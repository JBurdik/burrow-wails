/**
 * The client-side read model of the shell: workspaces, their tabs, agent
 * phases and chats. Filled by one `shell_snapshot` call and kept current by
 * the events the transport delivers live or replays after a reconnect.
 *
 * Deliberately NOT in here: chat transcript bodies and PTY scrollback. Both
 * already have their own replay paths (chat_stream + folded_ord via
 * LoadChatEventsSince; the daemon's ring on reattach), and a second one for
 * the same data is worse than none.
 *
 * Types are declared locally rather than imported from src/stores, because
 * src/runtime must stay importable by a client that has no stores at all
 * (src/runtime/boundary.test.ts enforces it).
 */
import type { Phase } from "./displayStatus";

export interface SnapshotWorkspace {
  id: number;
  name: string;
  path: string;
}

export interface ShellSnapshotData {
  seq: number;
  environment_id: string;
  workspaces: SnapshotWorkspace[];
  /** Tabs for EVERY workspace, keyed by workspace id — a phone opens on one
   *  the desktop never mounted. */
  tabs: Record<number, unknown[]>;
  /** Keyed exactly as the server keys a phase: `pty:<id>` or `chat:<id>`. */
  phases: Record<string, Phase>;
  chats: Record<string, unknown>[];
}

export interface ShellEvent {
  seq: number;
  name: string;
  payload?: unknown;
}

export function emptySnapshot(): ShellSnapshotData {
  return { seq: 0, environment_id: "", workspaces: [], tabs: {}, phases: {}, chats: [] };
}

/**
 * Apply one event. Returns a new object so a caller holding the old one for a
 * render diff still sees the old one.
 *
 * Unknown event names are ignored on purpose: the stream carries every bus
 * event the server has, and this model tracks part of it. An event that
 * matters to a component but not to this model reaches it through
 * `transport.listen` instead.
 */
export function applyShellEvent(state: ShellSnapshotData, ev: ShellEvent): ShellSnapshotData {
  // seq only ever moves forward. A duplicate delivery (live plus a resume's
  // deltas) must not rewind the position and make the client re-request a
  // gap it already has.
  const next: ShellSnapshotData = { ...state, seq: Math.max(state.seq, ev.seq) };

  if (ev.name.startsWith("phase-")) {
    next.phases = { ...state.phases, [ev.name.slice("phase-".length)]: ev.payload as Phase };
  }
  return next;
}

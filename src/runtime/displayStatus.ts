/**
 * The whole client-side status derivation.
 *
 * The backend owns the PHASE (see src-wails/internal/agentphase): what the
 * agent is doing, derivable with no client attached. What a client owns is
 * whether it has LOOKED yet — `review` and the transient `done` are read
 * receipts, not states, because the answer differs per device: the desktop may
 * be staring at the tab while the phone has never opened it.
 *
 * This file must not import Vue components, stores, or xterm.
 */
import type { TermStatus } from "../lib/terminalStatus";

export type PhaseState =
  | "idle"
  | "running"
  | "waiting_input"
  | "waiting_approval"
  | "done"
  | "failed"
  | "stale";

/** Wire shape of src-wails/internal/agentphase.Phase. */
export interface Phase {
  state: PhaseState;
  detail?: string;
  model?: string;
  title?: string;
  is_agent: boolean;
  turn_ended_at: number;
  updated_at: number;
}

/** How long a finished turn stays lime before it marks itself seen. */
export const DONE_AUTOCLEAR_MS = 4000;

function unseen(phase: Phase, seenAt: number): boolean {
  return phase.turn_ended_at > seenAt;
}

export function displayStatus(
  phase: Phase | undefined,
  seenAt: number,
  watching: boolean,
): TermStatus {
  if (!phase) return "idle";
  switch (phase.state) {
    case "running":
      return "running";
    case "waiting_input":
      return "waiting";
    case "waiting_approval":
      return "permission";
    case "failed":
      // A failed turn persists until seen whether or not anyone is watching:
      // the user must find out the turn died.
      return unseen(phase, seenAt) ? "error" : "idle";
    case "done":
      if (!unseen(phase, seenAt)) return "idle";
      return watching ? "done" : "review";
    case "idle":
      return "idle";
    // A stale PTY settles quietly — nothing failed, the process just went away.
    case "stale":
      return "idle";
    default: {
      // Compile-time exhaustiveness: if PhaseState gains a member and this
      // switch does not handle it, this assignment stops being valid.
      const _exhaustive: never = phase.state;
      void _exhaustive;
      return "idle";
    }
  }
}

/**
 * True when the client may mark this turn seen on its own (after
 * DONE_AUTOCLEAR_MS). A failed turn never auto-clears — only opening the tab
 * dismisses it.
 */
export function shouldMarkSeen(phase: Phase | undefined, watching: boolean): boolean {
  return !!phase && phase.state === "done" && watching;
}

/**
 * XState v5 state machine for terminal/chat status — the SINGLE owner of a dot.
 *
 * Three input channels feed it, all arbitrated here:
 *   1. agent hooks   (burrow status → pty-hook-{id})  → START/WAIT/PERMISSION_REQUEST/STOP/FAIL
 *   2. foreground poll (get_pty_foreground, 2 s)      → BUSY/NOT_BUSY/NEEDS_INPUT/SET_AGENT
 *   3. interrupt + dead-PTY watchdog                  → INTERRUPT
 *
 * The poll channel is guarded by `!context.isAgent`: for an agent leaf the hooks
 * are the sole authority and the poll can never fabricate a status. That guard is
 * the whole "stuck orange dot" rule, expressed once.
 *
 * States: idle → running ⇄ waiting/permission → done/review/error
 *
 * Side effects (sound, notify, ntfy, git refresh) are machine actions with no-op
 * defaults; callers inject real ones via `agentStatusMachine.provide({actions})`.
 */

import { setup, assign, and } from "xstate";
import type { TermStatus } from "../lib/terminalStatus";

// ── Types ──────────────────────────────────────────────────────────────────────

export type AgentStatusEvent =
  // channel 1 — agent hooks
  | { type: "START" }
  | { type: "WAIT" }
  | { type: "RESUME" }
  | { type: "PERMISSION_REQUEST" }
  | { type: "STOP"; watching: boolean }
  | { type: "FAIL"; detail?: string }
  // channel 2 — foreground poll (non-agent leaves only)
  | { type: "SET_AGENT"; isAgent: boolean }
  | { type: "BUSY" }
  | { type: "NOT_BUSY"; watching: boolean }
  | { type: "NEEDS_INPUT"; needs: boolean }
  // channel 3 — interrupt / watchdog
  | { type: "INTERRUPT" }
  // user opened the tab
  | { type: "MARK_SEEN" };

export interface AgentStatusContext {
  detail?: string;
  /** True while an agent process is in the foreground. Gates the poll channel. */
  isAgent: boolean;
}

export interface AgentStatusInput {
  isAgent?: boolean;
}

// ── Machine ────────────────────────────────────────────────────────────────────

export const agentStatusMachine = setup({
  types: {
    context: {} as AgentStatusContext,
    events: {} as AgentStatusEvent,
    input: {} as AgentStatusInput,
  },
  actions: {
    clearError: assign({ detail: undefined }),
    setDetail: assign({
      detail: ({ event }: { event: AgentStatusEvent }) =>
        event.type === "FAIL" ? event.detail : undefined,
    }),
    setAgent: assign({
      isAgent: ({ context, event }: { context: AgentStatusContext; event: AgentStatusEvent }) =>
        event.type === "SET_AGENT" ? event.isAgent : context.isAgent,
    }),
    // Injected by the caller via .provide(). No-ops by default so the machine
    // stays pure and unit-testable.
    playWaiting: () => {},
    onDone: () => {},
    onReview: () => {},
    onError: () => {},
  },
  guards: {
    // `watching` rides on the settling event (evaluated at transition time).
    isWatching: ({ event }: { event: AgentStatusEvent }) =>
      (event.type === "STOP" || event.type === "NOT_BUSY") && event.watching,
    // The poll must never drive an agent leaf's status.
    notAgent: ({ context }: { context: AgentStatusContext }) => !context.isAgent,
    needsInput: ({ event }: { event: AgentStatusEvent }) =>
      event.type === "NEEDS_INPUT" && event.needs,
    gotInput: ({ event }: { event: AgentStatusEvent }) =>
      event.type === "NEEDS_INPUT" && !event.needs,
  },
}).createMachine({
  id: "agentStatus",
  initial: "idle",
  context: ({ input }) => ({ detail: undefined, isAgent: input?.isAgent ?? false }),

  // SET_AGENT is accepted in every state — the poll flips it as processes come
  // and go, independent of where the status machine currently sits.
  on: {
    SET_AGENT: { actions: "setAgent" },
  },

  states: {
    idle: {
      on: {
        START: "running",
        BUSY: { guard: "notAgent", target: "running" },
      },
    },

    running: {
      on: {
        WAIT: "waiting",
        PERMISSION_REQUEST: "permission",
        STOP: [{ guard: "isWatching", target: "done" }, { target: "review" }],
        NOT_BUSY: [
          { guard: and(["notAgent", "isWatching"]), target: "done" },
          { guard: "notAgent", target: "review" },
        ],
        NEEDS_INPUT: { guard: and(["notAgent", "needsInput"]), target: "waiting" },
        FAIL: { target: "error", actions: "setDetail" },
        INTERRUPT: "idle",
      },
    },

    waiting: {
      entry: "playWaiting",
      on: {
        RESUME: "running",
        START: "running",
        PERMISSION_REQUEST: "permission",
        STOP: [{ guard: "isWatching", target: "done" }, { target: "review" }],
        NOT_BUSY: [
          { guard: and(["notAgent", "isWatching"]), target: "done" },
          { guard: "notAgent", target: "review" },
        ],
        // BUSY while waiting is a no-op: the command is still in the foreground,
        // it's just blocked on the user. Only `needs:false` resumes it.
        NEEDS_INPUT: { guard: and(["notAgent", "gotInput"]), target: "running" },
        FAIL: { target: "error", actions: "setDetail" },
        INTERRUPT: "idle",
      },
    },

    permission: {
      entry: "playWaiting",
      on: {
        RESUME: "running",
        START: "running",
        STOP: [{ guard: "isWatching", target: "done" }, { target: "review" }],
        FAIL: { target: "error", actions: "setDetail" },
        INTERRUPT: "idle",
      },
    },

    // Transient — auto-clears after 4 s (the user is watching, they saw it).
    done: {
      entry: "onDone",
      after: { 4000: "idle" },
      on: {
        MARK_SEEN: "idle",
        START: { target: "running", actions: "clearError" },
        BUSY: { guard: "notAgent", target: "running", actions: "clearError" },
      },
    },

    // Persists until the user opens the tab (markTabSeen).
    review: {
      entry: "onReview",
      on: {
        MARK_SEEN: "idle",
        START: { target: "running", actions: "clearError" },
        BUSY: { guard: "notAgent", target: "running", actions: "clearError" },
      },
    },

    // Persists until MARK_SEEN — never auto-clears (a failed turn must be seen).
    error: {
      entry: "onError",
      on: {
        MARK_SEEN: { target: "idle", actions: "clearError" },
        START: { target: "running", actions: "clearError" },
        BUSY: { guard: "notAgent", target: "running", actions: "clearError" },
      },
    },
  },
});

/** The machine's state ids ARE the TermStatus values — assert it at the type level. */
export type AgentStatusValue = Extract<TermStatus, "idle" | "running" | "waiting" | "permission" | "done" | "review" | "error">;

/** A leaf is "busy" (spinner-ish) exactly in the in-flight states. */
export function isBusyStatus(s: TermStatus): boolean {
  return s === "running" || s === "waiting" || s === "permission";
}

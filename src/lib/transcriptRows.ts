import type { ChatMessage } from "@/lib/chatTypes";

// What a transcript draws, as a pure projection of the loaded rows — the one
// rule, mirrored by ios/App/Transcript.swift and pinned for both by the shared
// fixtures in fixtures/transcript-rows.json.
//
// - message: one row.
// - tools: 2+ consecutive finished tool calls behind "Ran N commands"; a
//   trailing run at the very end whose last call still runs stays open, so
//   the live call shows. Open by default when one failed.
// - worked: a settled turn's thinking + tool rows behind one row, at the
//   position of its first work row. Folded by default except the last turn;
//   `foldOverride[userId]` is the user's choice.

export type TranscriptRow =
  | { kind: "message"; message: ChatMessage }
  | { kind: "tools"; id: string; items: ChatMessage[] }
  | { kind: "worked"; turn: number; tools: number; folded: boolean };

export interface TranscriptRowsOptions {
  /** The chat is not in flight, so its last turn is settled too. */
  settledLast: boolean;
  /** The user's fold choice per turn (its user row id). */
  foldOverride?: Record<number, boolean | undefined>;
}

// Rows without Go's `plumbing` flag were folded before it existed (old
// transcripts are not refolded): the same rule as chatfold.go isPlumbingCall.
const PLUMBING = new Set(["mcp__burrow__end_turn", "mcp__burrow__end_turn_pending", "ToolSearch"]);
const PLUMBING_TITLE = /^burrow[:\s]+end_turn(_pending)?$/i;

function isLegacyPlumbing(m: ChatMessage): boolean {
  if (m.role !== "tool" || m.toolFailed || m.toolOutput === undefined) return false;
  return m.toolRawName ? PLUMBING.has(m.text) : PLUMBING_TITLE.test(m.text.trim());
}

/** Rows the transcript never shows: legacy `queued` rows, plumbing calls, empty thinking. */
export function isVisibleRow(m: ChatMessage): boolean {
  if (m.role === "queued" || m.plumbing || isLegacyPlumbing(m)) return false;
  return !(m.role === "thinking" && !m.text);
}

export function toolRunning(m: ChatMessage): boolean {
  return !m.toolFailed && m.toolOutput === undefined;
}

/** A shell command, as opposed to any other tool. */
export function isCommandTool(m: ChatMessage): boolean {
  return m.toolRawName ? m.text === "Bash" : /^(run|execut|bash|\$)/i.test(m.text);
}

export function toolGroupLabel(items: ChatMessage[]): string {
  const n = items.length;
  const cmds = items.filter(isCommandTool).length;
  const s = (c: number) => (c === 1 ? "" : "s");
  if (cmds === n) return `Ran ${n} command${s(n)}`;
  if (cmds === 0) return `Used ${n} tool${s(n)}`;
  return `Used ${n} tools and ran ${cmds} command${s(cmds)}`;
}

const isWork = (m: ChatMessage) => m.role === "tool" || m.role === "thinking";

/** Groups runs of 2+ tool rows; `atEnd`: this slice ends the transcript (a live trailing run stays open). */
function grouped(msgs: ChatMessage[], atEnd: boolean): TranscriptRow[] {
  const out: TranscriptRow[] = [];
  let i = 0;
  while (i < msgs.length) {
    if (msgs[i].role !== "tool") { out.push({ kind: "message", message: msgs[i++] }); continue; }
    const start = i;
    while (i < msgs.length && msgs[i].role === "tool") i++;
    const run = msgs.slice(start, i);
    const live = atEnd && i === msgs.length && toolRunning(run[run.length - 1]);
    if (run.length < 2 || live) out.push(...run.map((message) => ({ kind: "message" as const, message })));
    else out.push({ kind: "tools", id: String(run[0].toolUseId ?? run[0].id), items: run });
  }
  return out;
}

export function transcriptRows(all: readonly ChatMessage[], opts: TranscriptRowsOptions): TranscriptRow[] {
  const msgs = all.filter(isVisibleRow);
  const out: TranscriptRow[] = [];
  let i = 0;
  // Rows before the first user message (a window starting mid-turn) are not folded.
  while (i < msgs.length && msgs[i].role !== "user") i++;
  out.push(...grouped(msgs.slice(0, i), i === msgs.length));
  while (i < msgs.length) {
    const user = msgs[i++];
    out.push({ kind: "message", message: user });
    const start = i;
    while (i < msgs.length && msgs[i].role !== "user") i++;
    const turn = msgs.slice(start, i);
    const hasLater = i < msgs.length;
    if (!(hasLater || opts.settledLast) || !turn.some(isWork)) { out.push(...grouped(turn, !hasLater)); continue; }
    const folded = opts.foldOverride?.[user.id] ?? hasLater;
    const tools = turn.filter((m) => m.role === "tool").length;
    let pending: ChatMessage[] = [];
    let emitted = false;
    for (const m of turn) {
      if (isWork(m)) {
        if (!emitted) {
          out.push(...grouped(pending, false));
          pending = [];
          out.push({ kind: "worked", turn: user.id, tools, folded });
          emitted = true;
        }
        if (folded) continue;
      }
      pending.push(m);
    }
    out.push(...grouped(pending, !hasLater));
  }
  return out;
}

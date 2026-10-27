// Background shell tasks of a Claude chat, derived from its raw stream-json
// lines (Go serves only the relevant ones: `load_chat_task_lines_since`).
//
// What the CLI (2.1.3xx) actually emits for a Bash task:
//   assistant  tool_use {id, name:"Bash", input:{command, description?, run_in_background:true}}
//   system     task_started {task_id, tool_use_id, description, is_backgrounded, task_type:"local_bash"}
//              — every Bash call gets one, foreground ones with is_backgrounded:false
//   system     task_updated {task_id, patch:{is_backgrounded:true}}
//              — a foreground Bash moved to the background (timeout / ctrl+b)
//   user       tool_result "…with ID: <id>. Output is being written to: <path>.output…",
//              tool_use_result.backgroundTaskId (+ timedOutAfterMs when moved)
//   system     background_tasks_changed {tasks:[{task_id,…}]} — the live set, a snapshot
//   system     task_updated {task_id, patch:{status:"completed"|"killed", end_time}}
//   system     task_notification {task_id, status:"completed"|"failed"|"stopped",
//              output_file, summary:"… completed (exit code 0)"}
// TaskStop / the `stop_task` control request ends as killed + stopped.

export type BgTaskStatus = "running" | "completed" | "failed" | "killed" | "ended";

export interface BgTask {
  id: string;
  toolUseId: string;
  description: string;
  command: string;
  outputFile: string;
  /** Codex: the process's working directory (no output file). */
  cwd?: string;
  status: BgTaskStatus;
  exitCode: number | null;
  summary: string;
  /** ms epoch, when known. */
  startedAt: number | null;
  endedAt: number | null;
  /** First ord the task was seen at — list order. */
  ord: number;
}

interface Entry extends BgTask {
  backgrounded: boolean;
  shell: boolean;
}

export interface BgTaskState {
  tasks: Map<string, Entry>;
  /** tool_use_id → command / start time from the assistant tool_use. */
  calls: Map<string, { command: string; description: string; at: number | null }>;
  /** tool_use_id → task id, for a tool_result that arrives before task_started. */
  byToolUse: Map<string, string>;
  lastOrd: number;
}

export function emptyBgTaskState(): BgTaskState {
  return { tasks: new Map(), calls: new Map(), byToolUse: new Map(), lastOrd: -1 };
}

const TERMINAL: ReadonlySet<BgTaskStatus> = new Set(["completed", "failed", "killed"]);

function entry(s: BgTaskState, id: string, ord: number): Entry {
  let e = s.tasks.get(id);
  if (!e) {
    e = {
      id, toolUseId: "", description: "", command: "", outputFile: "", status: "running",
      exitCode: null, summary: "", startedAt: null, endedAt: null, ord,
      backgrounded: false, shell: true,
    };
    s.tasks.set(id, e);
  }
  return e;
}

function link(s: BgTaskState, e: Entry, toolUseId: unknown) {
  if (typeof toolUseId !== "string" || !toolUseId) return;
  e.toolUseId = toolUseId;
  s.byToolUse.set(toolUseId, e.id);
  const call = s.calls.get(toolUseId);
  if (call) {
    if (!e.command) e.command = call.command;
    if (!e.description && call.description) e.description = call.description;
    if (e.startedAt == null) e.startedAt = call.at;
  }
}

function setStatus(e: Entry, st: BgTaskStatus, at?: unknown) {
  // A terminal status is final; a later "ended" guess never overrides it.
  if (TERMINAL.has(e.status) && !TERMINAL.has(st)) return;
  e.status = st;
  if (st !== "running" && e.endedAt == null && typeof at === "number") e.endedAt = at;
}

function ts(v: unknown): number | null {
  if (typeof v !== "string") return null;
  const t = Date.parse(v);
  return Number.isNaN(t) ? null : t;
}

function resultText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content.map((c) => (c && typeof c === "object" && typeof (c as { text?: unknown }).text === "string" ? (c as { text: string }).text : "")).join("\n");
  }
  return "";
}

const OUTPUT_RE = /Output is being written to: (\S+?\.output)\b/;
const EXIT_RE = /exit code (-?\d+)/;

/** Folds one raw stream line into the state. Unknown / non-JSON lines are ignored. */
export function applyBgTaskLine(s: BgTaskState, line: string, ord: number): void {
  if (ord > s.lastOrd) s.lastOrd = ord;
  let m: any;
  try { m = JSON.parse(line); } catch { return; }
  if (!m || typeof m !== "object") return;

  if (m.type === "assistant") {
    const at = ts(m.timestamp);
    for (const b of m.message?.content ?? []) {
      if (b?.type !== "tool_use" || b.name !== "Bash" || typeof b.id !== "string") continue;
      const command = typeof b.input?.command === "string" ? b.input.command : "";
      const description = typeof b.input?.description === "string" ? b.input.description : "";
      s.calls.set(b.id, { command, description, at });
      const known = s.byToolUse.get(b.id);
      if (known) link(s, s.tasks.get(known)!, b.id);
    }
    return;
  }

  if (m.type === "user") {
    const id = m.tool_use_result?.backgroundTaskId;
    if (typeof id !== "string" || !id) return;
    const e = entry(s, id, ord);
    e.backgrounded = true;
    for (const b of m.message?.content ?? []) {
      if (b?.type !== "tool_result") continue;
      link(s, e, b.tool_use_id);
      const path = OUTPUT_RE.exec(resultText(b.content))?.[1];
      if (path && !e.outputFile) e.outputFile = path;
    }
    if (e.startedAt == null) {
      const at = ts(m.timestamp);
      const moved = typeof m.tool_use_result?.timedOutAfterMs === "number" ? m.tool_use_result.timedOutAfterMs : 0;
      if (at != null) e.startedAt = at - moved;
    }
    return;
  }

  if (m.type !== "system") return;
  switch (m.subtype) {
    case "task_started": {
      if (typeof m.task_id !== "string") return;
      const e = entry(s, m.task_id, ord);
      if (m.task_type && m.task_type !== "local_bash") e.shell = false;
      if (m.is_backgrounded === true) e.backgrounded = true;
      if (typeof m.description === "string" && m.description) e.description = m.description;
      link(s, e, m.tool_use_id);
      return;
    }
    case "task_updated": {
      const e = typeof m.task_id === "string" ? s.tasks.get(m.task_id) : undefined;
      if (!e) return;
      const p = m.patch ?? {};
      if (p.is_backgrounded === true) {
        e.backgrounded = true;
        if (e.status === "ended") e.status = "running";
      }
      if (p.status === "completed" || p.status === "failed" || p.status === "killed") setStatus(e, p.status, p.end_time);
      else if (typeof p.end_time === "number" && e.endedAt == null) e.endedAt = p.end_time;
      return;
    }
    case "task_notification": {
      const e = typeof m.task_id === "string" ? s.tasks.get(m.task_id) : undefined;
      if (!e) return;
      if (typeof m.output_file === "string" && m.output_file && !e.outputFile) e.outputFile = m.output_file;
      if (typeof m.summary === "string") {
        e.summary = m.summary;
        const code = EXIT_RE.exec(m.summary);
        if (code) e.exitCode = Number(code[1]);
      }
      const st: BgTaskStatus =
        m.status === "completed" ? (e.exitCode != null && e.exitCode !== 0 ? "failed" : "completed")
          : m.status === "failed" ? "failed"
            : m.status === "stopped" ? "killed"
              : "ended";
      setStatus(e, st);
      return;
    }
    case "background_tasks_changed": {
      // The live set. A running task this snapshot no longer lists has ended,
      // even if its notification hasn't arrived (or never will — the CLI exited).
      const live = new Set<string>();
      for (const t of Array.isArray(m.tasks) ? m.tasks : []) if (typeof t?.task_id === "string") live.add(t.task_id);
      for (const e of s.tasks.values()) {
        if (e.backgrounded && e.status === "running" && e.ord < ord && !live.has(e.id)) setStatus(e, "ended");
      }
      return;
    }
  }
}

/** The background shell tasks, oldest first. */
export function listBgTasks(s: BgTaskState): BgTask[] {
  const out: BgTask[] = [];
  for (const e of s.tasks.values()) {
    if (!e.backgrounded || !e.shell) continue;
    const { backgrounded: _b, shell: _s, ...task } = e;
    out.push(task);
  }
  return out.sort((a, b) => a.ord - b.ord);
}

export function deriveBgTasks(lines: ReadonlyArray<{ ord: number; line: string }>): BgTask[] {
  const s = emptyBgTaskState();
  for (const l of lines) applyBgTaskLine(s, l.line, l.ord);
  return listBgTasks(s);
}

// ---- Codex ---------------------------------------------------------------
// A Codex chat's background work is the app-server's "background terminals":
// unified-exec processes that outlive the tool call and the turn (a dev
// server, a watcher). Nothing in the stream marks a process as backgrounded,
// so Go polls `thread/backgroundTerminals/list` (`chat_list_bg_terminals`) and
// stop is `thread/backgroundTerminals/terminate` (`chat_stop_task` with the
// processId). The list has no output, start time or exit code.

/** One row of `chat_list_bg_terminals` (Go's CodexBgTerminal). */
export interface CodexBgTerminal {
  itemId: string;
  processId: string;
  command: string;
  cwd: string;
}

/** `/bin/zsh -lc 'cmd'` → `cmd` — how Codex wraps every command it runs. */
export function unwrapShellCommand(command: string): string {
  const m = /^\S*\/(?:ba|z)?sh -l?c '(.*)'$/s.exec(command.trim());
  return m ? m[1].replace(/'\\''/g, "'") : command;
}

/**
 * Fold one poll of the live list into the rows shown: live terminals are
 * running (id = processId, what stop addresses); a row seen before that is no
 * longer listed has ended (exited, stopped, or the app-server is gone).
 */
export function mergeCodexTerminals(prev: ReadonlyArray<BgTask>, live: ReadonlyArray<CodexBgTerminal>): BgTask[] {
  const out = new Map<string, BgTask>();
  let ord = 0;
  for (const t of prev) {
    out.set(t.id, t.status === "running" ? { ...t, status: "ended" } : t);
    ord = Math.max(ord, t.ord + 1);
  }
  for (const t of live) {
    const was = out.get(t.processId);
    out.set(t.processId, {
      id: t.processId, toolUseId: t.itemId, description: "", command: unwrapShellCommand(t.command),
      outputFile: "", cwd: t.cwd, status: "running", exitCode: null, summary: "",
      startedAt: null, endedAt: null, ord: was ? was.ord : ord++,
    });
  }
  return [...out.values()].sort((a, b) => a.ord - b.ord);
}

/**
 * Command line for a terminal thread: the agent's interactive CLI, opened on a
 * prompt. The chat presets store *adapter* commands (`codex app-server`,
 * `gemini --acp`) — wire protocols, not TUIs — so the known agents are mapped
 * by kind and anything else falls back to its bare program.
 */
export interface TerminalLaunch {
  /** ChatAgent.kind — "claude" | "codex" | "gemini" | "custom" */
  kind: string;
  /** ChatAgent.command, used when the kind isn't one we know a TUI for */
  command: string;
  model?: string;
  /** Chat permission mode; only the ones Claude's CLI understands are passed on */
  permMode?: string;
}

const TERMINAL_PROGRAMS: Record<string, string> = {
  claude: "claude",
  codex: "codex",
  gemini: "gemini",
};

export function terminalProgramFor(a: Pick<TerminalLaunch, "kind" | "command">): string {
  return TERMINAL_PROGRAMS[a.kind] ?? a.command;
}

/** POSIX single-quoting: the only character that can escape is the quote itself. */
export function shellQuote(s: string): string {
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

export function buildTerminalCommand(a: TerminalLaunch, prompt: string): string {
  const parts = [terminalProgramFor(a)];
  if (a.kind === "claude") {
    if (a.model) parts.push("--model", a.model);
    // "auto" and "dontAsk" are Burrow-chat concepts with no CLI flag behind them.
    if (a.permMode === "bypassPermissions") parts.push("--dangerously-skip-permissions");
    else if (a.permMode === "acceptEdits" || a.permMode === "plan") parts.push("--permission-mode", a.permMode);
    // ponytail: effort is chat-side only; add a flag here when the CLI grows one.
  }
  // Claude, Codex and Gemini all take the first positional as the opening prompt.
  parts.push(shellQuote(prompt));
  return parts.join(" ");
}

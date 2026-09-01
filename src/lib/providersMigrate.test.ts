import { describe, it, expect } from "vitest";
import { migrateLegacyConfigs, type LegacyConfigs, type MigrationResult } from "./providersMigrate";

const EMPTY: LegacyConfigs = { agents: null, chatAgents: null, profiles: null };

/** The shipped defaults of each legacy store, as they'd sit in config.json. */
const LEGACY_TERMINAL_AGENTS = [
  { id: "claude", name: "Claude Code", command: "claude", args: "--dangerously-skip-permissions", shortcut: "⌘⇧1", color: "#d97757", icon: "claude" },
  { id: "codex", name: "Codex", command: "codex", args: "", shortcut: "⌘⇧2", color: "#34d399", icon: "openai" },
  { id: "gh-copilot", name: "GitHub Copilot", command: "copilot", args: "", shortcut: "⌘⇧3", color: "#8957e5", icon: "github-copilot" },
  { id: "aider", name: "Aider", command: "aider", args: "", shortcut: "⌘⇧4", color: "#fbbf24", icon: "robot" },
  { id: "cursor", name: "Cursor AI", command: "cursor-agent", args: "", shortcut: "⌘⇧5", color: "#f472b6", icon: "terminal" },
];

const LEGACY_CHAT_AGENTS = [
  { id: "claude", name: "Claude Code", transport: "claude-cli", command: "claude", args: [], env: {}, kind: "claude", color: "#d97757", icon: "claude", shortcut: "⌘⇧6", builtin: true },
  { id: "gemini", name: "Gemini", transport: "acp", command: "gemini", args: ["--acp"], env: {}, kind: "gemini", color: "#1a73e8", icon: "gemini", shortcut: "", builtin: true },
  { id: "codex", name: "Codex", transport: "codex-app-server", command: "codex", args: ["app-server"], env: {}, kind: "codex", color: "#74aa9c", icon: "openai", shortcut: "", builtin: true },
];

const LEGACY_PROFILES = [
  { id: "default", name: "Default", command: "claude", configDir: "", args: "", orgAccount: false },
  { id: "profile-1-20", name: "Work", command: "claude", configDir: "/Users/me/.claude-work", args: "--verbose", orgAccount: true },
];

const byId = (r: MigrationResult, id: string) => r.instances.find((a) => a.id === id);

describe("migrateLegacyConfigs — fresh install", () => {
  it("seeds the chat-capable providers when nothing is persisted", () => {
    const r = migrateLegacyConfigs(EMPTY);
    expect(r.instances.map((a) => a.id)).toEqual(["claude", "codex", "gemini", "opencode"]);
    expect(r.aliases).toEqual({});
    expect(r.instances.every((a) => a.enabled)).toBe(true);
  });

  it("survives garbage in every slot", () => {
    const r = migrateLegacyConfigs({ agents: "nope", chatAgents: 42, profiles: [null, "x"] });
    expect(r.instances.length).toBeGreaterThan(0);
  });
});

describe("migrateLegacyConfigs — the shipped defaults", () => {
  const r = migrateLegacyConfigs({
    agents: LEGACY_TERMINAL_AGENTS,
    chatAgents: LEGACY_CHAT_AGENTS,
    profiles: LEGACY_PROFILES,
  });

  it("collapses a terminal preset into the chat agent that runs the same program", () => {
    const claude = byId(r, "claude")!;
    expect(claude.transport).toBe("claude-cli");
    expect(claude.terminalArgs).toBe("--dangerously-skip-permissions");
    expect(claude.terminalShortcut).toBe("⌘⇧1");
    expect(claude.chatShortcut).toBe("⌘⇧6");
    // Only ONE Claude Code entry: the terminal preset did not become a second.
    expect(r.instances.filter((a) => a.name === "Claude Code")).toHaveLength(1);
  });

  it("keeps a terminal-only preset as a terminal-only instance", () => {
    const aider = byId(r, "aider")!;
    expect(aider.transport).toBe("none");
    expect(aider.terminalShortcut).toBe("⌘⇧4");
    expect(aider.providerId).toBe("aider");
  });

  it("upgrades retired icon keys", () => {
    expect(byId(r, "gh-copilot")!.icon).toBe("copilot");
  });

  it("stores a non-default binary as an override and leaves the default blank", () => {
    // "cursor-agent" IS the cursor catalog default, so no override is needed.
    expect(byId(r, "cursor")!.binary).toBe("");
    expect(byId(r, "claude")!.binary).toBe("");
  });

  it("turns a non-default Claude profile into its own instance, id intact", () => {
    const work = byId(r, "profile-1-20")!;
    expect(work.providerId).toBe("claude");
    expect(work.name).toBe("Work");
    expect(work.configDir).toBe("/Users/me/.claude-work");
    expect(work.args).toEqual(["--verbose"]);
    expect(work.orgAccount).toBe(true);
  });

  it("folds the default profile into the Claude instance and aliases its id", () => {
    expect(r.instances.some((a) => a.id === "default")).toBe(false);
    expect(r.aliases.default).toBe("claude");
  });

  it("groups instances of one provider together", () => {
    const claudeIdx = r.instances.map((a, i) => (a.providerId === "claude" ? i : -1)).filter((i) => i >= 0);
    expect(claudeIdx).toEqual([claudeIdx[0], claudeIdx[0] + 1]);
  });
});

describe("migrateLegacyConfigs — id safety", () => {
  it("suffixes a colliding legacy id and records the alias", () => {
    const r = migrateLegacyConfigs({
      agents: null,
      chatAgents: [LEGACY_CHAT_AGENTS[0]],
      // A profile that (pathologically) shares the chat agent's id.
      profiles: [{ id: "claude", name: "Second account", command: "claude", configDir: "/tmp/c2", args: "", orgAccount: false }],
    });
    expect(r.instances.map((a) => a.id)).toEqual(["claude", "claude-2"]);
    expect(r.aliases.claude).toBe("claude-2");
    expect(byId(r, "claude-2")!.configDir).toBe("/tmp/c2");
  });

  it("aliases a terminal preset id that got folded into another instance", () => {
    const r = migrateLegacyConfigs({
      agents: [{ id: "term-claude", name: "Claude", command: "claude", args: "-c", shortcut: "⌘⇧1", color: "#fff", icon: "claude" }],
      chatAgents: [LEGACY_CHAT_AGENTS[0]],
      profiles: null,
    });
    expect(r.instances).toHaveLength(1);
    expect(r.aliases["term-claude"]).toBe("claude");
    expect(r.instances[0].terminalArgs).toBe("-c");
  });

  it("matches on the program name, ignoring an absolute path", () => {
    const r = migrateLegacyConfigs({
      agents: [{ id: "t", name: "Claude", command: "/opt/homebrew/bin/claude", args: "-c", shortcut: "", color: "#fff", icon: "claude" }],
      chatAgents: [LEGACY_CHAT_AGENTS[0]],
      profiles: null,
    });
    expect(r.instances).toHaveLength(1);
    expect(r.instances[0].terminalArgs).toBe("-c");
  });
});

describe("migrateLegacyConfigs — determinism", () => {
  it("produces identical output when run twice", () => {
    const input: LegacyConfigs = {
      agents: LEGACY_TERMINAL_AGENTS,
      chatAgents: LEGACY_CHAT_AGENTS,
      profiles: LEGACY_PROFILES,
    };
    expect(migrateLegacyConfigs(input)).toEqual(migrateLegacyConfigs(input));
  });

  it("does not mutate the legacy input", () => {
    const input = JSON.parse(JSON.stringify({ agents: LEGACY_TERMINAL_AGENTS, chatAgents: LEGACY_CHAT_AGENTS, profiles: LEGACY_PROFILES }));
    const snapshot = JSON.stringify(input);
    migrateLegacyConfigs(input);
    expect(JSON.stringify(input)).toBe(snapshot);
  });
});

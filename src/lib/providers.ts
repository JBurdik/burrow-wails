/**
 * Provider catalog — the static list of agent CLIs Burrow knows how to launch.
 *
 * A *provider* is the program (claude, codex, gemini…). A *provider instance*
 * (see `stores/providers.ts`) is one configured way to run it: its own binary
 * path, config dir, env, colour and shortcuts. Several instances can share a
 * provider — that's how a work account and a personal account coexist.
 *
 * The catalog only supplies defaults for a NEW instance and per-provider
 * capabilities. Once an instance exists it owns its values; changing a catalog
 * default never rewrites configured instances.
 */

/**
 * How the embedded chat talks to the provider: Claude's native stream-json
 * CLI, Codex's native JSON-RPC app-server, or a generic ACP adapter.
 */
export type ChatTransport = "claude-cli" | "codex-app-server" | "acp";

export function transportLabel(transport: ChatTransport): string {
  if (transport === "claude-cli") return "Claude CLI";
  if (transport === "codex-app-server") return "Codex app-server";
  return "ACP";
}

/** Special-env injection selector, mirrored from the old chat-agent `kind`. */
export type ProviderKind = "claude" | "gemini" | "codex" | "custom";

export interface ProviderCatalogEntry {
  id: string;
  label: string;
  /** Default binary; empty for "custom", which has nothing to guess. */
  binary: string;
  icon: string;
  color: string;
  kind: ProviderKind;
  /** "none" = terminal-only provider (no embedded chat). */
  transport: ChatTransport | "none";
  transportArgs: string[];
  /** Whether the instance editor offers a config-dir field (CLAUDE_CONFIG_DIR). */
  supportsConfigDir: boolean;
  /** npm package to check for updates against; unset for CLIs not installed via npm. */
  npmPackage?: string;
}

export const PROVIDER_CATALOG: ProviderCatalogEntry[] = [
  { id: "claude", label: "Claude Code", binary: "claude", icon: "claude", color: "#d97757", kind: "claude", transport: "claude-cli", transportArgs: [], supportsConfigDir: true, npmPackage: "@anthropic-ai/claude-code" },
  { id: "codex", label: "Codex", binary: "codex", icon: "openai", color: "#74aa9c", kind: "codex", transport: "codex-app-server", transportArgs: ["app-server"], supportsConfigDir: false, npmPackage: "@openai/codex" },
  { id: "gemini", label: "Gemini", binary: "gemini", icon: "gemini", color: "#1a73e8", kind: "gemini", transport: "acp", transportArgs: ["--acp"], supportsConfigDir: false, npmPackage: "@google/gemini-cli" },
  { id: "opencode", label: "opencode", binary: "opencode", icon: "terminal", color: "#f59e0b", kind: "custom", transport: "acp", transportArgs: ["acp"], supportsConfigDir: false, npmPackage: "opencode-ai" },
  { id: "copilot", label: "GitHub Copilot", binary: "copilot", icon: "copilot", color: "#8957e5", kind: "custom", transport: "none", transportArgs: [], supportsConfigDir: false, npmPackage: "@github/copilot" },
  { id: "aider", label: "Aider", binary: "aider", icon: "robot", color: "#fbbf24", kind: "custom", transport: "none", transportArgs: [], supportsConfigDir: false },
  { id: "cursor", label: "Cursor AI", binary: "cursor-agent", icon: "terminal", color: "#f472b6", kind: "custom", transport: "none", transportArgs: [], supportsConfigDir: false },
  { id: "custom", label: "Custom", binary: "", icon: "robot", color: "#9ca3af", kind: "custom", transport: "none", transportArgs: [], supportsConfigDir: false },
];

const BY_ID = new Map(PROVIDER_CATALOG.map((p) => [p.id, p]));

/** Catalog entry for `id`, falling back to "custom" for unknown providers. */
export function providerFor(id: string): ProviderCatalogEntry {
  return BY_ID.get(id) ?? BY_ID.get("custom")!;
}

/**
 * Best-effort provider id for a bare command, used by the legacy-config
 * migration to attach old presets to a catalog entry. Matches on the program
 * name only, so `/opt/homebrew/bin/claude` still resolves to `claude`.
 */
export function providerIdForCommand(command: string): string {
  const program = command.trim().split(/[\s]+/)[0]?.split("/").pop() ?? "";
  if (!program) return "custom";
  const hit = PROVIDER_CATALOG.find((p) => p.binary && p.binary === program);
  return hit?.id ?? "custom";
}

/**
 * One configured way to run a provider. A terminal launch and a chat session
 * are two modes of the SAME instance, so the binary, env and config dir are
 * shared and only the mode-specific extras differ.
 */
export interface ProviderInstance {
  id: string;
  /** Catalog entry id. */
  providerId: string;
  name: string;
  enabled: boolean;
  color: string;
  icon: string;

  /** Binary to run; empty means "use the catalog default". */
  binary: string;
  /** Extra args shared by both launch modes. */
  args: string[];
  env: Record<string, string>;
  /** CLAUDE_CONFIG_DIR-style config dir; empty = the user's default. */
  configDir: string;
  /** Org/team accounts can't use the OAuth usage API — scan local JSONL instead. */
  orgAccount: boolean;

  /** Extra flags applied only to a terminal launch (e.g. --dangerously-skip-permissions). */
  terminalArgs: string;
  terminalShortcut: string;

  /** "none" = terminal-only instance; no embedded chat. */
  transport: ChatTransport | "none";
  /** Adapter args for the chat transport ("app-server", "--acp", …). */
  transportArgs: string[];
  chatShortcut: string;
  /** Drives special env injection in the Go backend. */
  kind: ProviderKind;

  /** Seeded from the catalog: resettable, not deletable. */
  builtin?: boolean;
}

export function newInstance(providerId: string, over: Partial<ProviderInstance> = {}): ProviderInstance {
  const p = providerFor(providerId);
  return {
    id: over.id ?? p.id,
    providerId: p.id,
    name: p.label,
    enabled: true,
    color: p.color,
    icon: p.icon,
    binary: "",
    args: [],
    env: {},
    configDir: "",
    orgAccount: false,
    terminalArgs: "",
    terminalShortcut: "",
    transport: p.transport,
    transportArgs: [...p.transportArgs],
    chatShortcut: "",
    kind: p.kind,
    ...over,
  };
}

/** Instances seeded on a fresh install — the chat-capable providers. */
export function seedInstances(): ProviderInstance[] {
  return ["claude", "codex", "gemini", "opencode"].map((id) => newInstance(id, { builtin: true }));
}

/** Binary this instance launches: its override, else the catalog default. */
export function binaryFor(a: Pick<ProviderInstance, "binary" | "providerId">): string {
  return a.binary.trim() || providerFor(a.providerId).binary;
}

/** Full command line for a terminal launch. */
export function commandLine(a: ProviderInstance): string {
  const extra = [...a.args, ...a.terminalArgs.trim().split(/\s+/)].filter(Boolean);
  return [binaryFor(a), ...extra].join(" ").trim();
}

/**
 * Chat transport to use for an instance that is about to back a chat session.
 * A terminal-only instance has no chat contract, so fall back to the one its
 * kind implies rather than leaving "none" to leak into the runtime.
 */
export function chatTransportFor(a: Pick<ProviderInstance, "transport" | "kind">): ChatTransport {
  if (a.transport !== "none") return a.transport;
  return a.kind === "claude" ? "claude-cli" : "acp";
}

/** Dotted-numeric version compare (ignores any -pre/+build suffix). true if `a` < `b`. */
export function versionLessThan(a: string, b: string): boolean {
  const pa = a.split(/[-+]/)[0].split(".").map(Number);
  const pb = b.split(/[-+]/)[0].split(".").map(Number);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? 0, y = pb[i] ?? 0;
    if (x !== y) return x < y;
  }
  return false;
}

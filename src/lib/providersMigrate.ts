/**
 * One-time fold of the three legacy agent configs into the unified provider
 * instance list.
 *
 *   agentPresets      → terminal quick-launch presets  (stores/agents.ts)
 *   chatAgentPresets  → embedded chat agents           (stores/chatAgents.ts)
 *   claudeProfiles    → Claude launch identities       (stores/profiles.ts)
 *
 * All three describe the same thing from different angles, so a terminal preset
 * and a chat agent that run the SAME program collapse into one instance carrying
 * both modes.
 *
 * Two invariants matter more than tidiness:
 *
 *   1. **Ids survive.** Chats persist `agentId`, tasks persist `profileId`. A
 *      migrated instance keeps its legacy id, and every legacy id that got
 *      folded into another instance is recorded in the alias map, so an old
 *      chat still resolves to the right config dir on `--resume`.
 *   2. **Deterministic.** No clocks, no counters — running it twice on the same
 *      input yields the same ids, so a partially-written config can't produce
 *      duplicates.
 *
 * Legacy keys are only read here; the caller leaves them on disk so an older
 * build can still be rolled back to.
 */
import {
  PROVIDER_CATALOG,
  providerFor,
  providerIdForCommand,
  newInstance,
  seedInstances,
  type ProviderInstance,
} from "./providers";

export interface LegacyConfigs {
  agents: unknown;
  chatAgents: unknown;
  profiles: unknown;
}

export interface MigrationResult {
  instances: ProviderInstance[];
  /** legacy id → instance id, for ids that did not survive as-is. */
  aliases: Record<string, string>;
}

/** Old terminal-preset icon keys that no longer exist in AGENT_ICONS. */
const ICON_ALIASES: Record<string, string> = {
  "github-copilot": "copilot",
  "git-branch": "robot",
};

function icon(key: unknown, fallback: string): string {
  const k = typeof key === "string" ? key : "";
  return ICON_ALIASES[k] ?? (k || fallback);
}

function argsToArray(v: unknown): string[] {
  if (Array.isArray(v)) return v.map(String).filter(Boolean);
  if (typeof v === "string") return v.trim().split(/\s+/).filter(Boolean);
  return [];
}

function asArray(v: unknown): Record<string, unknown>[] {
  return Array.isArray(v) ? (v.filter((x) => x && typeof x === "object") as Record<string, unknown>[]) : [];
}

/** Program name of a command line, ignoring args and directories. */
function program(command: unknown): string {
  return String(command ?? "").trim().split(/\s+/)[0]?.split("/").pop() ?? "";
}

/** Provider id for a legacy record: its own id if that's a catalog entry, else by command. */
function providerIdFor(id: unknown, command: unknown): string {
  const known = PROVIDER_CATALOG.find((p) => p.id === String(id ?? ""));
  if (known) return known.id;
  return providerIdForCommand(String(command ?? ""));
}

/** Store `command` as a binary override only when it differs from the catalog default. */
function binaryOverride(providerId: string, command: unknown): string {
  const cmd = String(command ?? "").trim();
  if (!cmd) return "";
  return cmd === providerFor(providerId).binary ? "" : cmd;
}

export function migrateLegacyConfigs(legacy: LegacyConfigs): MigrationResult {
  const aliases: Record<string, string> = {};
  const taken = new Set<string>();

  /** Keep the legacy id when free; otherwise suffix it and record the alias. */
  const claimId = (legacyId: string, fallback: string): string => {
    const base = legacyId || fallback;
    if (!taken.has(base)) {
      taken.add(base);
      return base;
    }
    let n = 2;
    while (taken.has(`${base}-${n}`)) n++;
    const id = `${base}-${n}`;
    taken.add(id);
    aliases[base] = id;
    return id;
  };

  const out: ProviderInstance[] = [];

  // --- Chat agents: the richest records, so they seed the list ---------------
  const chatRecords = asArray(legacy.chatAgents);
  if (chatRecords.length) {
    for (const c of chatRecords) {
      const providerId = providerIdFor(c.id, c.command);
      const cat = providerFor(providerId);
      const id = claimId(String(c.id ?? ""), providerId);
      out.push(newInstance(providerId, {
        id,
        name: String(c.name ?? cat.label),
        color: String(c.color ?? cat.color),
        icon: icon(c.icon, cat.icon),
        binary: binaryOverride(providerId, c.command),
        env: { ...((c.env as Record<string, string>) ?? {}) },
        transport: (c.transport as ProviderInstance["transport"]) ?? cat.transport,
        transportArgs: argsToArray(c.args),
        chatShortcut: String(c.shortcut ?? ""),
        kind: (c.kind as ProviderInstance["kind"]) ?? cat.kind,
        builtin: Boolean(c.builtin),
      }));
    }
  } else {
    // Nothing persisted (fresh install, or chat agents never touched): seed the
    // catalog's chat-capable providers so the list is never empty.
    for (const inst of seedInstances()) {
      taken.add(inst.id);
      out.push(inst);
    }
  }

  // --- Terminal presets: merge into a matching instance, else add ------------
  for (const t of asArray(legacy.agents)) {
    const legacyId = String(t.id ?? "");
    const prog = program(t.command);
    const match = prog ? out.find((a) => program(a.binary || providerFor(a.providerId).binary) === prog) : undefined;

    if (match) {
      match.terminalArgs = String(t.args ?? "");
      match.terminalShortcut = String(t.shortcut ?? "");
      if (legacyId && legacyId !== match.id) aliases[legacyId] = match.id;
      continue;
    }

    const providerId = providerIdFor(t.id, t.command);
    const cat = providerFor(providerId);
    out.push(newInstance(providerId, {
      id: claimId(legacyId, providerId),
      name: String(t.name ?? cat.label),
      color: String(t.color ?? cat.color),
      icon: icon(t.icon, cat.icon),
      binary: binaryOverride(providerId, t.command),
      terminalArgs: String(t.args ?? ""),
      terminalShortcut: String(t.shortcut ?? ""),
      // A preset Burrow only ever typed into a shell has no chat contract.
      transport: "none",
      transportArgs: [],
    }));
  }

  // --- Claude profiles: extra Claude instances ------------------------------
  const claudeInstance = out.find((a) => a.providerId === "claude");
  for (const p of asArray(legacy.profiles)) {
    const legacyId = String(p.id ?? "");
    const isDefault = legacyId === "default";

    if (isDefault && claudeInstance) {
      // The default profile IS the plain Claude launch — fold it in, but keep
      // any overrides the user put on it.
      if (p.configDir) claudeInstance.configDir = String(p.configDir);
      if (p.args) claudeInstance.args = argsToArray(p.args);
      if (p.orgAccount) claudeInstance.orgAccount = true;
      const bin = binaryOverride("claude", p.command);
      if (bin) claudeInstance.binary = bin;
      aliases[legacyId] = claudeInstance.id;
      continue;
    }

    out.push(newInstance("claude", {
      id: claimId(legacyId, "claude"),
      name: String(p.name ?? "Claude profile"),
      binary: binaryOverride("claude", p.command),
      args: argsToArray(p.args),
      configDir: String(p.configDir ?? ""),
      orgAccount: Boolean(p.orgAccount),
    }));
  }

  return { instances: sortByCatalog(out), aliases };
}

/** Group instances of the same provider together, in catalog order. */
function sortByCatalog(list: ProviderInstance[]): ProviderInstance[] {
  const rank = new Map(PROVIDER_CATALOG.map((p, i) => [p.id, i]));
  return list
    .map((a, i) => ({ a, i }))
    .sort((x, y) => (rank.get(x.a.providerId) ?? 99) - (rank.get(y.a.providerId) ?? 99) || x.i - y.i)
    .map((x) => x.a);
}

/** Typed authoring primitives for the Burrow extension API v1. */
export type Permission =
  | "workspace.read"
  | "workspace.write"
  | "secrets.read"
  | "secrets.write"
  | "settings.read"
  | "tasks.report"
  | "network.connect"
  | string;

export interface CommandContribution {
  id: string;
  title: string;
  command: string;
  args?: string[];
}

export interface WorkspacePulseSurface {
  id: string;
  title: string;
  description?: string;
  kind: "workspace-pulse";
}

export interface ExtensionManifest {
  apiVersion: 1;
  id: string;
  name: string;
  version: string;
  description?: string;
  permissions?: Permission[];
  commands?: CommandContribution[];
  surfaces?: WorkspacePulseSurface[];
}

export interface ExtensionDefinition {
  manifest: ExtensionManifest;
}

export interface ExtensionContext {
  extension: {
    id: string;
    directory: string;
  };
  workspace: {
    /** Undefined when Burrow has no active workspace. */
    cwd?: string;
  };
}

export type Environment = Readonly<Record<string, string | undefined>>;

export interface TaskUpdate {
  id: string;
  title: string;
  status?: "running" | "completed" | "failed";
  progress?: number;
}

export interface BurrowHost {
  workspace(): Promise<{ cwd: string }>;
  secrets: {
    get(name: string): Promise<string>;
    set(name: string, value: string): Promise<void>;
    delete(name: string): Promise<void>;
  };
  settings: { get(name: string): Promise<string> };
  tasks: { report(update: TaskUpdate): Promise<void> };
}

const idPattern = /^[a-z0-9][a-z0-9-]{1,62}$/;

/**
 * Declares an extension and validates the parts Burrow validates at install
 * time, so an author gets an error before packaging the extension.
 */
export function defineExtension(definition: ExtensionDefinition): ExtensionDefinition {
  const { manifest } = definition;
  if (manifest.apiVersion !== 1) throw new Error("Burrow SDK supports apiVersion: 1");
  if (!idPattern.test(manifest.id)) throw new Error("Extension id must be lowercase, hyphenated, and 2–63 characters");
  if (!manifest.name.trim() || !manifest.version.trim()) throw new Error("Extension name and version are required");

  for (const command of manifest.commands ?? []) {
    if (!command.id || !command.title || !command.command) throw new Error("Every command needs id, title, and command");
    if (command.command.includes("/") || command.command.includes("\\")) {
      throw new Error(`Command ${command.id} must use an executable available on PATH`);
    }
  }
  for (const surface of manifest.surfaces ?? []) {
    if (!surface.id || !surface.title || surface.kind !== "workspace-pulse") {
      throw new Error("v1 supports only a workspace-pulse surface with id and title");
    }
  }
  return definition;
}

/** Reads the stable v1 host context passed to a command process. */
export function getContext(environment: Environment): ExtensionContext {
  const id = environment.BURROW_EXTENSION_ID;
  const directory = environment.BURROW_EXTENSION_DIR;
  if (!id || !directory) throw new Error("This command must be launched by Burrow");
  const cwd = environment.BURROW_EXTENSION_CWD;
  return { extension: { id, directory }, workspace: { ...(cwd ? { cwd } : {}) } };
}

/** Returns the active workspace or a clear error suitable for command output. */
export function requireWorkspace(context: ExtensionContext): string {
  if (!context.workspace.cwd) throw new Error("Open a workspace before running this extension command");
  return context.workspace.cwd;
}

/**
 * Creates a capability-scoped client for the current command invocation.
 * Burrow starts this loopback bridge only for the command's lifetime; neither
 * its URL nor its token can be reused by a later invocation.
 */
export function createHost(environment: Environment): BurrowHost {
  const baseURL = environment.BURROW_EXTENSION_BRIDGE_URL;
  const token = environment.BURROW_EXTENSION_BRIDGE_TOKEN;
  if (!baseURL || !token) throw new Error("This command must be launched by Burrow with SDK host access");

  async function request<T>(path: string, options: { operation?: string; body?: unknown } = {}): Promise<T> {
    const response = await fetch(`${baseURL}${path}`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        ...(options.operation ? { "X-Burrow-Secret-Operation": options.operation } : {}),
        ...(options.body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      ...(options.body === undefined ? {} : { body: JSON.stringify(options.body) }),
    });
    if (!response.ok) throw new Error(`Burrow host request failed: ${await response.text()}`);
    return response.json() as Promise<T>;
  }

  return {
    workspace: () => request<{ cwd: string }>("/v1/workspace"),
    secrets: {
      get: async (name) => (await request<{ value: string }>(`/v1/secrets/${encodeURIComponent(name)}`, { operation: "get" })).value,
      set: async (name, value) => { await request(`/v1/secrets/${encodeURIComponent(name)}`, { operation: "set", body: { value } }); },
      delete: async (name) => { await request(`/v1/secrets/${encodeURIComponent(name)}`, { operation: "delete" }); },
    },
    settings: { get: async (name) => (await request<{ value: string }>(`/v1/settings/${encodeURIComponent(name)}`)).value },
    tasks: { report: async (update) => { await request("/v1/tasks/report", { body: update }); } },
  };
}

/**
 * Frontend half of the control API.
 *
 * Verbs that only the UI can perform (open a tab, focus a workspace, read a
 * terminal's scrollback) arrive as a `control:action` event; this dispatches
 * them and acks with a JSON result. The backend verb blocks until that ack, so
 * an agent gets the new tab's id — or a real error — rather than fire-and-forget.
 *
 * One listener for the whole app (not per Terminal): actions are addressed by
 * workspace/pty id, and the previous per-Terminal poll made "is that workspace
 * mounted?" the agent's problem.
 */
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { useWorkspaceStore } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useProvidersStore } from "@/stores/providers";
import { useUIStore } from "@/stores/ui";
import { useDiagram } from "@/composables/useDiagram";
import { buildTerminalCommand } from "@/lib/agentCommand";
import { readTermOutput } from "@/lib/termRegistry";

type ControlAction = { id: string; action: string; args: Record<string, unknown> };

const str = (v: unknown) => (typeof v === "string" ? v : v == null ? "" : String(v));
const num = (v: unknown) => (typeof v === "number" ? v : Number(str(v)) || 0);

export async function installControlBridge(): Promise<() => void> {
  return listen<ControlAction>("control:action", async (event) => {
    const { id, action, args } = event.payload;
    try {
      const result = await perform(action, args ?? {});
      await invoke("ack_control_action", { id, resultJson: JSON.stringify(result ?? {}), errMsg: "" });
    } catch (err) {
      await invoke("ack_control_action", { id, resultJson: "", errMsg: err instanceof Error ? err.message : String(err) });
    }
  });
}

async function perform(action: string, args: Record<string, unknown>): Promise<unknown> {
  switch (action) {
    case "focus_workspace":
      return focusWorkspace(num(args.workspaceId));
    case "focus_tab":
      return focusTab(num(args.ptyId));
    case "new_tab":
      return newTab(num(args.workspaceId), str(args.cmd));
    case "tab_rename":
      return renameTab(num(args.ptyId), str(args.title));
    case "tab_close":
      return closeTab(num(args.ptyId));
    case "workspace_create":
      return createWorkspace(str(args.path), str(args.name));
    case "workspaces_reload":
      await useWorkspaceStore().load();
      return { ok: true };
    case "spawn":
      return spawn(args);
    case "list_agents":
      return listAgents();
    case "agent_status":
      return agentStatus();
    case "tab_output":
      return tabOutput(num(args.ptyId), num(args.lines) || 80);
    case "diagram":
      useDiagram().showDiagram(str(args.content));
      return { ok: true };
    default:
      throw new Error(`the app has no handler for "${action}"`);
  }
}

/** Resolve a workspace by id, reloading the list first if it's not known yet
 *  (a worktree the backend just created won't be in the store). */
async function workspaceById(id: number) {
  const ws = useWorkspaceStore();
  let found = ws.workspaces.find((w) => w.id === id);
  if (!found) {
    await ws.load();
    found = ws.workspaces.find((w) => w.id === id);
  }
  return found;
}

async function focusWorkspace(id: number) {
  const found = await workspaceById(id);
  if (!found) throw new Error(`no workspace with id ${id}`);
  useWorkspaceStore().open(found);
  return { workspace_id: found.id, name: found.name, path: found.path };
}

/** Which workspace owns a pty id, per the tabs mirror. */
function ownerOf(ptyId: number): number | undefined {
  const tabs = useTerminalTabsStore();
  for (const [wsId, list] of Object.entries(tabs.tabsByWs)) {
    if (list.some((t) => t.id === ptyId)) return Number(wsId);
  }
  return undefined;
}

async function focusTab(ptyId: number) {
  const owner = ownerOf(ptyId);
  if (owner === undefined) throw new Error(`no open tab with pty id ${ptyId}`);
  const ws = useWorkspaceStore();
  if (ws.active?.id !== owner) {
    const target = await workspaceById(owner);
    if (target) ws.open(target);
  }
  useTerminalTabsStore().activate(owner, ptyId);
  useUIStore().mode = "terminal";
  return { pty_id: ptyId, workspace_id: owner };
}

async function newTab(workspaceId: number, cmd: string) {
  const ws = useWorkspaceStore();
  const target = workspaceId ? await workspaceById(workspaceId) : ws.active;
  if (!target) throw new Error("no workspace to open a tab in");
  ws.open(target);
  const ptyId = await useTerminalTabsStore().add(target.id, cmd || undefined);
  return { pty_id: ptyId, workspace_id: target.id };
}

function renameTab(ptyId: number, title: string) {
  const owner = ownerOf(ptyId);
  if (owner === undefined) throw new Error(`no open tab with pty id ${ptyId}`);
  useTerminalTabsStore().rename(owner, ptyId, title);
  return { pty_id: ptyId, title };
}

function closeTab(ptyId: number) {
  const owner = ownerOf(ptyId);
  if (owner === undefined) throw new Error(`no open tab with pty id ${ptyId}`);
  useTerminalTabsStore().close(owner, ptyId);
  return { pty_id: ptyId, closed: true };
}

async function createWorkspace(path: string, name: string) {
  if (!path) throw new Error("workspace_create needs a path");
  const ws = useWorkspaceStore();
  const created = await ws.create(name || path.split("/").filter(Boolean).pop() || path, path);
  ws.open(created);
  return { workspace_id: created.id, name: created.name, path: created.path };
}

/**
 * Open a sub-agent on the task. The caller names the WORK (and optionally which
 * configured agent and model); building the command line stays here, where the
 * provider registry lives — an agent shouldn't have to know that Codex isn't
 * launched like Claude, or invent flags.
 */
async function spawn(args: Record<string, unknown>) {
  const task = str(args.task);
  if (!task) throw new Error("spawn needs a task");
  const providers = useProvidersStore();
  const ui = useUIStore();
  const wsStore = useWorkspaceStore();

  const wanted = str(args.agent);
  const instance =
    (wanted && (providers.byId(wanted) ?? providers.instances.find((i) => i.name.toLowerCase() === wanted.toLowerCase()))) ||
    providers.byId(ui.defaultChatAgent) ||
    providers.instances.find((i) => i.enabled);
  if (!instance) throw new Error("no agent is configured in Settings > Providers");
  if (wanted && !providers.byId(wanted) && !providers.instances.some((i) => i.name.toLowerCase() === wanted.toLowerCase())) {
    throw new Error(`no agent named "${wanted}" — call list_agents to see the configured ones`);
  }

  const cwd = str(args.cwd);
  // A spawn into a worktree belongs to THAT workspace, so its tab nests under
  // the worktree in the sidebar rather than under the parent repo.
  const target = (cwd && wsStore.workspaces.find((w) => w.path === cwd)) || wsStore.active;
  if (!target) throw new Error("no workspace to spawn into");
  wsStore.open(target);

  // No explicit target → the user's Settings preference ("Spawn sub-agents as",
  // where "terminal" is this API's "tab").
  const openAs = str(args.target) || (ui.spawnMode === "chat" ? "chat" : "tab");
  if (openAs === "chat") {
    const chats = useClaudeChatsStore();
    const session = chats.create(target.id, { agentKind: instance.id });
    useTerminalTabsStore().openChat(target.id, session.id, instance.id, task);
    return { chat_id: session.id, workspace_id: target.id };
  }

  const cmd = buildTerminalCommand(
    { kind: instance.kind, command: providers.binaryFor(instance), model: str(args.model) || undefined },
    task,
  );
  const ptyId = await useTerminalTabsStore().add(target.id, cmd, {
    cwd: cwd || undefined,
    resultToken: str(args.token) || undefined,
    background: true,
  });
  if (ptyId === undefined) throw new Error("the workspace did not open a tab (is it still loading?)");
  return { pty_id: ptyId, workspace_id: target.id, agent: instance.name };
}

function listAgents() {
  return useProvidersStore()
    .instances.filter((i) => i.enabled)
    .map((i) => ({ id: i.id, name: i.name, kind: i.kind }));
}

/** Every agent in the app and what it's doing — tabs and chats in one list, the
 *  same two surfaces the Sidebar shows. */
function agentStatus() {
  const tabs = useTerminalTabsStore();
  const chats = useClaudeChatsStore();
  const ws = useWorkspaceStore();
  const nameOf = (id: number) => ws.workspaces.find((w) => w.id === id)?.name ?? String(id);

  const out: unknown[] = [];
  for (const [wsId, list] of Object.entries(tabs.tabsByWs)) {
    for (const tab of list) {
      if (!tab.isAgent && !tab.isChat) continue;
      out.push({
        kind: "tab",
        pty_id: tab.id,
        title: tab.title,
        status: tab.status,
        workspace: nameOf(Number(wsId)),
        workspace_id: Number(wsId),
      });
    }
  }
  for (const s of chats.sessions) {
    if (s.control) continue; // the Manager's own session
    out.push({
      kind: "chat",
      chat_id: s.id,
      title: s.title,
      status: s.status ?? (s.busy ? "running" : "idle"),
      workspace: nameOf(s.workspaceId),
      workspace_id: s.workspaceId,
    });
  }
  return out;
}

function tabOutput(ptyId: number, lines: number) {
  const text = readTermOutput(ptyId, lines);
  if (text === undefined) throw new Error(`pty ${ptyId} has no live terminal (closed, or its workspace isn't open)`);
  return { pty_id: ptyId, text };
}

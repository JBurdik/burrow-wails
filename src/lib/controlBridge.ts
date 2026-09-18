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
import { router } from "@/router";
import { useDiagram } from "@/composables/useDiagram";
import { buildTerminalCommand } from "@/lib/agentCommand";
import { readTermOutput } from "@/lib/termRegistry";
import { subagentMessage, type ChatMessage } from "@/lib/chatTypes";
import { childrenOf } from "@/stores/chatTree";
import { chatSession, liveChatSessionIds } from "@/lib/chatSession";

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

// Exported so a caller inside the app — e.g. RightPanel's manual spawn button
// — can go through the same verb dispatch a remote control-API caller would,
// rather than duplicating spawn()'s target/agent resolution.
export async function perform(action: string, args: Record<string, unknown>): Promise<unknown> {
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
    case "chat_send":
      return chatSendFollowUp(num(args.chatId), str(args.text));
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
  await router.push(`/ws/${found.id}`);
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
  // Navigating IS the focus: the route names the workspace and the tab, so a
  // control caller and the UI can no longer disagree about what is on screen.
  await router.push(`/ws/${owner}/tab/${ptyId}`);
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
  // ensureOpen, NOT open: a spawn is a background action (the tab branch below
  // passes `background: true` for the same reason), and the caller's cwd is
  // routinely a different workspace than the one on screen — an agent running
  // in a worktree, or in a project the user has since switched away from.
  // open() made that spawn steal the active workspace, and since a
  // just-mounted Terminal has not reported its tabs yet, App.vue read the
  // empty mirror as "this workspace has nothing to show" and bounced to the
  // welcome composer mid-thread. Mounting is all we need: the Terminal has to
  // exist to answer the `add` request, not to be in front.
  wsStore.ensureOpen(target);

  // No explicit target → the user's Settings preference ("Spawn sub-agents as",
  // where "terminal" is this API's "tab").
  const openAs = str(args.target) || (ui.spawnMode === "chat" ? "chat" : "tab");
  const parentChatId = num(args.parent_chat_id);
  if (openAs === "chat") {
    const chats = useClaudeChatsStore();
    // A sub-agent belongs to its thread, not the Sidebar, so it does NOT go
    // through openChat() — that is what puts a chat there. Its CLI starts
    // when SubAgentHost.vue mounts an AgentChat for it (a chat's process is
    // started on mount, not here — see AgentChat.vue's "Lazy runtime start"),
    // so the task text rides through `create()`'s `initialPrompt` for that
    // mount to pick up and send — queued THERE, before the new session is
    // pushed into `sessions.value`, so SubAgentHost's watcher can never
    // observe the session before its prompt is ready. See
    // pendingSubagentPrompts' comment in claudeChats.ts.
    const session = await chats.create(target.id, {
      agentKind: instance.id,
      parentChatId: parentChatId || undefined,
      initialPrompt: parentChatId ? task : undefined,
    });
    if (parentChatId) {
      // The parent's transcript gets a row recording the delegation.
      await appendSubagentMessage(parentChatId, session.id, task, instance.name);
      return { chat_id: session.id, workspace_id: target.id, parent_chat_id: parentChatId };
    }
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

/**
 * Record a spawn in the parent's transcript.
 *
 * Hazard: `AgentChat.vue` destructures `messages` straight off its
 * `chatSession(chatId)` (`const { messages, ... } = S`), so that Ref — not a
 * component-local copy — is the transcript, and it outlives an unmounted view
 * (see chatSession.ts). A blind load→push→save here would race the parent's
 * OWN next `saveMessages()` call, which serializes whatever is in that Ref at
 * the time and would silently drop our row if it lands between our load and
 * our save. `spawn` only ever runs mid-turn for the chat that called it, so
 * the parent's session already exists (`chatSession()` was created at mount);
 * pushing straight into that shared Ref, then saving from it, makes our
 * append indistinguishable from one the parent made itself — nothing to race.
 * Only a parent with no live session (spawned via the control API with no
 * view ever mounted for it) falls back to a direct read-modify-write.
 */
async function appendSubagentMessage(parentChatId: number, childChatId: number, task: string, agentName: string) {
  const msg = subagentMessage(childChatId, task, agentName);
  if (liveChatSessionIds().includes(parentChatId)) {
    const S = chatSession(parentChatId);
    S.messages.value.push(msg);
    // Mirrors AgentChat.vue's own saveMessages(): drop partials, cap history,
    // and hand foldedOrd = lastOrd + 1 so the chat_stream trim stays safe.
    const toSave = S.messages.value.filter((m) => !m.partial).slice(-2000);
    const foldedOrd = S.lastOrd >= 0 ? S.lastOrd + 1 : -1;
    await invoke("save_chat_messages", { chatId: parentChatId, messages: JSON.stringify(toSave), foldedOrd });
    return;
  }
  const raw = await invoke<string>("load_chat_messages", { chatId: parentChatId }).catch(() => "[]");
  const messages: ChatMessage[] = JSON.parse(raw || "[]");
  messages.push(msg);
  await invoke("save_chat_messages", { chatId: parentChatId, messages: JSON.stringify(messages), foldedOrd: -1 });
}

/** A follow-up into a child's session — the same call the composer makes, so a
 *  steered child is indistinguishable from one the user typed into. Goes
 *  straight to the provider bindings, matching AgentChat.vue's own send call
 *  for each transport, rather than through chatSession (which owns the
 *  transcript stream, not sending) — deliberately, since the whole point is
 *  reaching a sub-agent whose AgentChat view is NOT mounted. */
async function chatSendFollowUp(chatId: number, text: string) {
  if (!chatId) throw new Error("chat_send needs a chat_id");
  if (!text.trim()) throw new Error("chat_send needs text");
  const session = useClaudeChatsStore().sessions.find((s) => s.id === chatId);
  if (!session) throw new Error(`no chat with id ${chatId}`);
  if (session.transport === "claude-cli") {
    await invoke("claude_send", { id: chatId, text, sessionId: session.claudeSessionId || null, images: [] });
  } else if (session.transport === "codex-app-server") {
    await invoke("codex_send", { id: chatId, text, images: [] });
  } else {
    await invoke("acp_send", { id: chatId, text, images: [] });
  }
  return { chat_id: chatId, sent: true };
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
      parent_chat_id: s.parentChatId ?? 0,
      children: childrenOf(chats.sessions, s.id).map((c) => c.id),
    });
  }
  return out;
}

function tabOutput(ptyId: number, lines: number) {
  const text = readTermOutput(ptyId, lines);
  if (text === undefined) throw new Error(`pty ${ptyId} has no live terminal (closed, or its workspace isn't open)`);
  return { pty_id: ptyId, text };
}

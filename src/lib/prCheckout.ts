// Check out a pull request into a new worktree + chat (after t3code's
// PullRequestThreadDialog): read the PR, fetch its head into a worktree named
// after its branch (forge_pr_checkout → CreateWorktree, so setup scripts run),
// open a chat there linked to the PR (chat_link_pr) with a draft prompt the
// user sends themselves.
import { nextTick } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useNotificationsStore } from "@/stores/notifications";
import { useUIStore } from "@/stores/ui";
import { worktreePath } from "@/lib/projectSettings";
import { router } from "@/router";

/** Opens PR `number` of `ws`'s repository in a new worktree + chat. Reports progress and errors as a toast. */
export async function openPrInWorktree(ws: Workspace, number: number, url?: string): Promise<void> {
  const wsStore = useWorkspaceStore();
  const notif = useNotificationsStore();
  const ui = useUIStore();
  const root = (ws.parent_id && wsStore.workspaces.find((w) => w.id === ws.parent_id)) || ws;
  const toastId = notif.push({ type: "pending", title: `Checking out PR #${number}…`, source: "git" });
  try {
    const pr = await invoke<{ number: number; title: string; url: string; headRefName: string }>("forge_pr_view", { cwd: root.path, number });
    // A pasted URL must be this repository's PR, not just the same number.
    if (url && !url.toLowerCase().startsWith(pr.url.toLowerCase())) throw new Error(`${url} is not a pull request of ${root.name}`);
    const branch = pr.headRefName || `pr-${number}`;
    const wt = await invoke<Workspace>("forge_pr_checkout", {
      repoPath: root.path, number, branch, path: worktreePath(root, branch, ui.worktreesDir),
    });
    await wsStore.load();
    const chats = useClaudeChatsStore();
    const session = await chats.create(wt.id, { agentKind: ui.defaultChatAgent });
    await invoke("chat_link_pr", { chatId: session.id, ref: `#${number}` }).catch(() => {});
    // AgentChat's own draft key: the prompt waits in the composer, unsent.
    localStorage.setItem(`burrow.draft.chat.${session.id}`, `Review / continue PR #${number}: ${pr.title}`);
    const opened = wsStore.workspaces.find((w) => w.id === wt.id) ?? wt;
    wsStore.open(opened);
    await router.push(`/ws/${opened.id}`);
    await nextTick(); // a freshly-mounted Terminal attaches its request watcher on the next tick
    useTerminalTabsStore().openChat(opened.id, session.id);
    notif.resolve(toastId, { type: "done", title: `PR #${number} checked out`, body: branch, source: "git" });
  } catch (e) {
    notif.resolve(toastId, { type: "error", title: `Couldn't check out PR #${number}`, body: String(e), source: "git" });
  }
}

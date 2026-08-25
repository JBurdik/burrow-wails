import { defineStore } from "pinia";
import { ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type { TermStatus } from "@/lib/terminalStatus";
import { useWorkspaceStore } from "@/stores/workspace";
import { useUIStore } from "@/stores/ui";
import { useTerminalTabsStore } from "@/stores/terminalTabs";

export type BoardColumn = "backlog" | "todo" | "in_progress" | "for_review" | "done";

export const BOARD_COLUMNS: { id: BoardColumn; label: string }[] = [
  { id: "backlog", label: "Backlog" },
  { id: "todo", label: "Todo" },
  { id: "in_progress", label: "In Progress" },
  { id: "for_review", label: "For Review" },
  { id: "done", label: "Done" },
];

// Mirrors the Rust `MissionTask` struct (src-tauri/src/lib.rs — row_to_board_task /
// upsert_board_task). Field names match serde's default camelCase-free (snake_case)
// wire format used throughout this codebase's other Tauri structs.
export interface MissionTask {
  id: string;
  workspace_id: number;
  pty_id?: number | null;
  title: string;
  cwd?: string | null;
  model?: string | null;
  status?: string | null;
  turns?: number | null;
  created_at: number;
  handed_off?: number | null;
  profile_id?: string | null;
  repo_workspace_id: number;
  board_column: BoardColumn;
  description?: string | null;
  agent_kind?: string | null;
  transport?: "pty" | "acp" | "stream-json" | null;
  use_worktree?: number | null;
  worktree_branch?: string | null;
  task_workspace_id?: number | null;
  chat_id?: number | null;
  session_id?: string | null;
  board_order: number;
  updated_at?: number | null;
}

export interface TaskAttachment {
  id: number;
  task_id: string;
  ord: number;
  mime_type: string;
  file_path: string;
  created_at: number;
}

export interface AgentTurnChange {
  id: number;
  taskId: string;
  ptyId: number;
  startedAt: number;
  completedAt?: number | null;
  state: string;
  changesAvailable: boolean;
  changeError?: string | null;
  files: string[];
  additions: number;
  deletions: number;
}

/** New Backlog card id — same convention as other client-generated ids in this app. */
export function newTaskId(): string {
  return `task_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * Derive the card's live status dot. For a task whose agent is already running,
 * prefer the live terminal-tab / chat-session status (same TermStatus values +
 * status-dots.css classes used everywhere else) over the DB-persisted `status`
 * column, which only updates on explicit upsert/move.
 */
export function liveStatusForTask(task: MissionTask): TermStatus {
  const tabs = useTerminalTabsStore();
  for (const list of Object.values(tabs.tabsByWs)) {
    const t = list.find((x) => x.taskId === task.id);
    if (t) return t.status;
  }
  return (task.status as TermStatus) || "idle";
}

export const useBoardTasksStore = defineStore("boardTasks", () => {
  const tasksByRepo = ref<Record<number, MissionTask[]>>({});
  const attachmentsByTask = ref<Record<string, TaskAttachment[]>>({});
  let listening = false;

  async function load(repoId: number) {
    tasksByRepo.value[repoId] = await invoke<MissionTask[]>("list_board_tasks", { repoWorkspaceId: repoId });
  }

  function _put(task: MissionTask) {
    const list = tasksByRepo.value[task.repo_workspace_id] ?? [];
    const idx = list.findIndex((t) => t.id === task.id);
    if (idx >= 0) list.splice(idx, 1, task);
    else list.push(task);
    tasksByRepo.value[task.repo_workspace_id] = [...list];
  }

  async function upsert(task: MissionTask): Promise<MissionTask> {
    const saved = await invoke<MissionTask>("upsert_board_task", { task });
    _put(saved);
    return saved;
  }

  async function move(taskId: string, column: BoardColumn, order: number) {
    await invoke("move_board_task", { taskId, column, order });
    for (const repoId of Object.keys(tasksByRepo.value)) {
      const t = tasksByRepo.value[Number(repoId)]?.find((x) => x.id === taskId);
      if (t) { t.board_column = column; t.board_order = order; }
    }
  }

  async function remove(taskId: string) {
    await invoke("delete_board_task", { taskId });
    for (const repoId of Object.keys(tasksByRepo.value)) {
      tasksByRepo.value[Number(repoId)] = (tasksByRepo.value[Number(repoId)] || []).filter((t) => t.id !== taskId);
    }
    delete attachmentsByTask.value[taskId];
  }

  async function loadAttachments(taskId: string) {
    attachmentsByTask.value[taskId] = await invoke<TaskAttachment[]>("list_task_attachments", { taskId });
  }

  async function addAttachment(taskId: string, base64Data: string, mimeType: string) {
    await invoke<string>("write_task_attachment", { taskId, base64Data, mimeType });
    await loadAttachments(taskId);
  }

  async function removeAttachment(attachmentId: number, taskId: string) {
    await invoke("delete_task_attachment", { attachmentId });
    await loadAttachments(taskId);
  }

  async function readAttachmentBase64(attachmentId: number): Promise<{ base64: string; mime: string }> {
    const [base64, mime] = await invoke<[string, string]>("read_task_attachment_base64", { attachmentId });
    return { base64, mime };
  }

  async function listTurnChanges(taskId: string): Promise<AgentTurnChange[]> {
    return invoke<AgentTurnChange[]>("list_agent_turn_changes", { taskId });
  }

  async function getTurnDiff(turnId: number): Promise<string> {
    return invoke<string>("get_agent_turn_diff", { turnId });
  }

  // Listens for `board-task-moved`, emitted by `move_board_task`/`delete_board_task`
  // (UI drag-drop) AND by the pure-Rust `board-move` arm of `take_spawn_requests`
  // (agent/Manager `burrow board-move`) — so a card moved from a CLI/agent
  // context updates any open board live. Column-only patch; a full task list
  // refresh (new cards from `burrow board-create`) is left to KanbanBoard's
  // manual refresh, since the event carries no repo id to reconcile against.
  async function init() {
    if (listening) return;
    listening = true;
    await listen<{ taskId: string; column: string | null }>("board-task-moved", (ev) => {
      const { taskId, column } = ev.payload;
      for (const repoId of Object.keys(tasksByRepo.value)) {
        const list = tasksByRepo.value[Number(repoId)];
        if (!list) continue;
        if (column == null) {
          tasksByRepo.value[Number(repoId)] = list.filter((t) => t.id !== taskId);
          continue;
        }
        const t = list.find((x) => x.id === taskId);
        if (t) t.board_column = column as BoardColumn;
      }
    });
  }

  function slugify(s: string): string {
    return s.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 32) || "task";
  }

  // Single-quote a string for embedding in a shell command line.
  function shQuote(s: string): string {
    return `'${s.replace(/'/g, `'\\''`)}'`;
  }

  /**
   * Backlog → Todo transition: create the worktree (if requested), spawn a
   * real `claude` CLI PTY tab (stamped with this task's id) and type the
   * description + attachments as its first prompt, then move the card to
   * 'todo'. Shared by TaskDetail.vue's "Start" button AND Terminal.vue's
   * `take_spawn_requests` "board-move" handler (the frontend-pushed half of
   * `burrow board-move <id> todo`), so a Manager-driven start behaves
   * identically to a human clicking Start.
   */
  async function startTask(task: MissionTask): Promise<MissionTask> {
    const wsStore = useWorkspaceStore();
    const ui = useUIStore();
    const tabsStore = useTerminalTabsStore();

    const repo = wsStore.workspaces.find((w) => w.id === task.repo_workspace_id);
    if (!repo) throw new Error("repo workspace not found");
    const repoName = repo.path.split("/").filter(Boolean).pop() || "repo";

    let taskWorkspaceId = task.repo_workspace_id;
    let worktreeBranch: string | null = null;
    if (task.use_worktree) {
      const branch = `task/${slugify(task.title)}`;
      const path = `${ui.worktreesDir}/${repoName}/${branch}`;
      const wt = await wsStore.createWorktree(task.repo_workspace_id, branch, "HEAD", path);
      taskWorkspaceId = wt.id;
      worktreeBranch = branch;
    }

    const attachments = await invoke<TaskAttachment[]>("list_task_attachments", { taskId: task.id });
    const promptParts = [
      (task.description || "").trim(),
      attachments.length ? `Attached screenshots:\n${attachments.map((a) => `- ${a.file_path}`).join("\n")}` : "",
      `This is Kanban board task ${task.id}. When your work is ready for review, call the burrow MCP tool ` +
        `board_move({taskId: "${task.id}", column: "for_review"}) (or \`burrow board-move ${task.id} for_review\`) ` +
        `so the card moves off the board. Do not move it to "done" — only a human does that.`,
    ].filter(Boolean);
    const prompt = promptParts.join("\n\n");

    const modelFlag = task.model ? `--model ${task.model} ` : "";
    const cmd = prompt ? `claude ${modelFlag}${shQuote(prompt)}` : `claude ${modelFlag}`.trim();

    const next: MissionTask = { ...task };
    next.transport = "pty";
    next.chat_id = null;
    tabsStore.add(taskWorkspaceId, cmd, task.id);

    next.task_workspace_id = taskWorkspaceId;
    next.worktree_branch = worktreeBranch;
    next.board_column = "todo";
    next.board_order = Date.now();
    return upsert(next);
  }

  return {
    tasksByRepo,
    attachmentsByTask,
    load,
    upsert,
    move,
    remove,
    loadAttachments,
    addAttachment,
    removeAttachment,
    readAttachmentBase64,
    listTurnChanges,
    getTurnDiff,
    init,
    startTask,
    slugify,
  };
});

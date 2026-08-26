// Shim for "@tauri-apps/api/core"'s invoke(), backed by the Wails-generated
// Go bindings (burrow-wails/burrow/frontend/wailsjs/go/main/App). Vue
// call-sites pass a Tauri-style snake_case command name + named-args
// object; this dispatches to the corresponding Go method with positional
// args. Only commands implemented on the Go side so far are mapped — see
// docs/plans for the remaining src-tauri/src/lib.rs command surface.
import * as App from "../../../burrow-wails/burrow/frontend/wailsjs/go/main/App";

type Args = Record<string, any>;

export async function invoke(cmd: string, args: Args = {}): Promise<any> {
  switch (cmd) {
    // PTY
    case "create_pty":
      return App.CreatePty(args.shell, args.args ?? [], args.cwd ?? "", args.env ?? []);
    case "write_pty":
      return App.WritePty(args.id, args.data);
    case "resize_pty":
      return App.ResizePty(args.id, args.cols, args.rows);
    case "kill_pty":
      return App.KillPty(args.id);
    case "list_pty_sessions":
      return App.ListPtySessions();

    // Workspaces / tabs
    case "list_workspaces":
      return App.ListWorkspaces();
    case "create_workspace":
      return App.CreateWorkspace(args.name, args.path);
    case "delete_workspace":
      return App.DeleteWorkspace(args.id);
    case "rename_workspace":
      return App.RenameWorkspace(args.id, args.name);
    case "touch_workspace":
      return App.TouchWorkspace(args.id);
    case "set_workspace_icon":
      return App.SetWorkspaceIcon(args.id, args.icon);
    case "set_workspace_order":
      return App.SetWorkspaceOrder(args.ids);
    case "list_terminal_tabs":
      return App.ListTerminalTabs(args.workspaceId ?? args.workspace_id);
    case "save_terminal_tabs":
      return App.SaveTerminalTabs(args.workspaceId ?? args.workspace_id, args.tabs ?? []);

    // Worktrees
    case "create_worktree":
      return App.CreateWorktree(args.repoPath ?? args.repo_path, args.name, args.path, args.branch, args.baseRef ?? args.base_ref ?? "");
    case "remove_worktree":
      return App.RemoveWorktree(args.id, !!args.force);

    // Board
    case "list_board_tasks":
      return App.ListBoardTasks(args.repoWorkspaceId ?? args.repo_workspace_id);
    case "upsert_board_task":
      return App.UpsertBoardTask(args.task);
    case "move_board_task":
      return App.MoveBoardTask(args.taskId ?? args.task_id, args.column, args.order ?? 0);
    case "delete_board_task":
      return App.DeleteBoardTask(args.taskId ?? args.task_id);

    // Git / gh
    case "run_git":
      return App.RunGit(args.cwd, args.args ?? []);
    case "run_gh":
      return App.RunGh(args.cwd, args.args ?? []);

    // FS / misc
    case "write_text_file":
      return App.WriteTextFile(args.path, args.content);
    case "read_text_file":
    case "read_text_file_checked":
      return App.ReadTextFile(args.path);
    case "read_file_base64":
      return App.ReadFileBase64(args.path);
    case "read_dir_shallow":
      return App.ReadDirShallow(args.path);
    case "open_path_in":
      return App.OpenPathIn(args.path, args.target);
    case "get_app_version":
      return App.GetAppVersion();
    case "set_sleep_inhibit":
      return App.SetSleepInhibit(!!args.active);
    case "get_hook_server_port":
      return App.GetHookServerPort();
    case "set_http_enabled":
      return App.SetHttpEnabled(!!args.enabled);
    case "take_spawn_requests":
      return App.TakeSpawnRequests(args.cwd);

    // Claude Code
    case "claude_start":
      return App.ClaudeStart(args.cwd, args.args ?? []);
    case "claude_send":
      return App.ClaudeSend(args.id, args.text);
    case "claude_stop":
      return App.ClaudeStop(args.id);
    case "claude_abort":
      return App.ClaudeAbort(args.id);

    // ACP / Codex
    case "acp_start":
      return App.AcpStart(args.command, args.cwd, args.args ?? []);
    case "codex_start":
      return App.CodexStart(args.cwd, args.args ?? []);
    case "acp_send":
      return App.AcpSend(args.id, args.text);
    case "codex_send":
      return App.CodexSend(args.id, args.text);
    case "acp_stop":
      return App.AcpStop(args.id);
    case "codex_stop":
      return App.CodexStop(args.id);

    default:
      console.warn(`[wails-compat] invoke("${cmd}") has no Go binding yet`);
      throw new Error(`command not implemented in Go backend: ${cmd}`);
  }
}

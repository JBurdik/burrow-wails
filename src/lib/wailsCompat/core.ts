// Shim for "@tauri-apps/api/core"'s invoke(), backed by the Wails-generated
// Go bindings (burrow-wails/burrow/frontend/wailsjs/go/main/App). Vue
// call-sites pass a Tauri-style snake_case command name + named-args
// object; this dispatches to the corresponding Go method with positional
// args. Only commands implemented on the Go side so far are mapped — see
// docs/plans for the remaining src-tauri/src/lib.rs command surface.
import * as App from "../../../burrow-wails/burrow/frontend/wailsjs/go/main/App";

type Args = Record<string, any>;

export async function invoke<T = unknown>(cmd: string, args: Args = {}): Promise<T> {
  return dispatch(cmd, args) as Promise<T>;
}

async function dispatch(cmd: string, args: Args): Promise<any> {
  switch (cmd) {
    // PTY — id is the frontend's own numeric counter (props.ptyId), always
    // stringified for the Go/daemon side, which treats ids as opaque keys.
    case "create_pty":
      return App.CreatePty(String(args.id), args.cwd ?? "", args.cols, args.rows);
    case "write_pty":
      return App.WritePty(String(args.id), args.data);
    case "resize_pty":
      return App.ResizePty(String(args.id), args.cols, args.rows);
    case "kill_pty":
      return App.KillPty(String(args.id));
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

    // LSP
    case "lsp_start":
      return App.LspStart(args.id, args.command, args.args ?? [], args.cwd ?? "");
    case "lsp_send":
      return App.LspSend(args.id, args.message);
    case "lsp_stop":
      return App.LspStop(args.id);

    // Mission tasks / agent turns
    case "list_mission_tasks":
      return App.ListMissionTasks();
    case "upsert_mission_task":
      return App.UpsertMissionTask(args.task);
    case "delete_mission_task":
      return App.DeleteMissionTask(args.id);
    case "begin_agent_turn":
      return App.BeginAgentTurn(args.taskId ?? args.task_id, Number(args.ptyId ?? args.pty_id), args.worktreePath ?? args.worktree_path ?? "");
    case "complete_agent_turn":
      return App.CompleteAgentTurn(Number(args.ptyId ?? args.pty_id), args.state);
    case "list_agent_turn_changes":
      return App.ListAgentTurnChanges(args.taskId ?? args.task_id);

    // Skills / MCP servers
    case "list_skills":
      return App.ListSkills();
    case "set_skill_enabled":
      return App.SetSkillEnabled(args.dir, !!args.enabled);
    case "delete_skill":
      return App.DeleteSkill(args.dir);
    case "list_mcp_servers":
      return App.ListMcpServers();
    case "add_mcp_server":
      return App.AddMcpServer(args.name, args.config);
    case "remove_mcp_server":
      return App.RemoveMcpServer(args.name);

    // Claude session reading
    case "list_claude_sessions":
      return App.ListClaudeSessions(args.cwd);
    case "read_claude_transcript":
      return App.ReadClaudeTranscript(args.cwd, args.sessionId ?? args.session_id);
    case "read_claude_activity":
      return App.ReadClaudeActivity(args.cwd, args.sessionId ?? args.session_id);

    // Control/permission responses
    case "claude_respond_control":
      return App.ClaudeRespondControl(args.id, args.requestId ?? args.request_id, args.response);
    case "acp_respond_permission":
      return App.AcpRespondPermission(args.id, args.rpcId ?? args.rpc_id, args.optionId ?? args.option_id);
    case "acp_respond_user_input":
      return App.AcpRespondUserInput(args.id, args.rpcId ?? args.rpc_id, args.text);

    // Task attachments
    case "write_task_attachment":
      return App.WriteTaskAttachment(args.taskId ?? args.task_id, args.mimeType ?? args.mime_type, args.data, args.ext);
    case "list_task_attachments":
      return App.ListTaskAttachments(args.taskId ?? args.task_id);
    case "delete_task_attachment":
      return App.DeleteTaskAttachment(args.id);
    case "read_task_attachment_base64":
      return App.ReadTaskAttachmentBase64(args.id);

    // Misc
    case "system_stats":
      return App.SystemStats();
    case "get_config_dirs":
      return App.GetConfigDirs();
    case "set_config_dirs":
      return App.SetConfigDirs(args.claude, args.codex, args.copilot);
    case "save_temp_image":
      return App.SaveTempImage(args.b64 ?? args.data, args.ext);
    case "is_pid_alive":
      return App.IsPidAlive(args.pid);
    case "set_tab_live_status":
      return App.SetTabLiveStatus(args.ptyId ?? args.pty_id, args.status);
    case "set_max_agents":
      return App.SetMaxAgents(args.n ?? args.max);
    case "set_burrow_mcp_max_depth":
      return App.SetBurrowMcpMaxDepth(args.n ?? args.depth);

    // App config file
    case "read_config":
      return App.ReadConfig();
    case "write_config":
      return App.WriteConfig(args.content);

    // Float windows (no-op: Wails is single-window, see plan phase 7)
    case "open_float_window":
      return App.OpenFloatWindow(args.ptyId ?? args.pty_id);
    case "close_float_window":
      return App.CloseFloatWindow(args.ptyId ?? args.pty_id);
    case "set_float_corner":
      return App.SetFloatCorner(args.ptyId ?? args.pty_id, args.corner);
    case "snap_float_window":
      return App.SnapFloatWindow(args.ptyId ?? args.pty_id);
    case "sync_float_size":
      return App.SyncFloatSize(args.ptyId ?? args.pty_id);
    case "request_float_snapshot":
      return App.RequestFloatSnapshot(args.ptyId ?? args.pty_id);
    case "send_float_snapshot":
      return App.SendFloatSnapshot(args.ptyId ?? args.pty_id, args.data, args.cols, args.rows);
    case "notify_float_grid":
      return App.NotifyFloatGrid(args.ptyId ?? args.pty_id, args.cols, args.rows);
    case "open_git_panel_window":
      return App.OpenGitPanelWindow();
    case "register_tmux_win":
      return App.RegisterTmuxWin(args.winId ?? args.win_id, args.ptyId ?? args.pty_id);

    // Claude account/usage (stubbed — "unavailable" until reverse-engineered)
    case "claude_get_account":
      return App.ClaudeGetAccount(args.cwd);
    case "claude_plan_usage":
      return App.ClaudePlanUsage(args.configDir ?? args.config_dir, !!args.force);
    case "claude_usage_5h":
      return App.ClaudeUsage5h(args.configDir ?? args.config_dir);

    // Remote chat sync (stubbed — no transport yet)
    case "remote_sync_chat":
      return App.RemoteSyncChat(args.chat);
    case "remote_list_chats":
      return App.RemoteListChats();
    case "remote_create_chat":
      return App.RemoteCreateChat(args.cwd);

    // Daemon admin (stubbed)
    case "daemon_stats":
      return App.DaemonStats();
    case "clean_daemon":
      return App.CleanDaemon();
    case "kill_orphan_sessions":
      return App.KillOrphanSessions(args.keepIds ?? args.keep_ids ?? []);
    case "restart_daemon":
      return App.RestartDaemon();
    case "repair_agent_status":
      return App.RepairAgentStatus();
    case "format_source":
      return App.FormatSource(args.path, args.content, args.cwd);

    default:
      console.warn(`[wails-compat] invoke("${cmd}") has no Go binding yet`);
      throw new Error(`command not implemented in Go backend: ${cmd}`);
  }
}

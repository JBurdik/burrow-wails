// Shim for "@tauri-apps/api/core"'s invoke(), backed by the Wails-generated
// Go bindings (src-wails/frontend/wailsjs/go/main/App). Vue
// call-sites pass a Tauri-style snake_case command name + named-args
// object; this dispatches to the corresponding Go method with positional
// args. Only commands implemented on the Go side so far are mapped — see
// docs/plans for the remaining src-tauri/src/lib.rs command surface.
import * as App from "../../../src-wails/frontend/wailsjs/go/main/App";

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
      // The daemon binding exposes live IDs (`string[]`), while the legacy
      // Tauri UI contract expects session records. Normalize here so restored
      // terminal threads reattach to their existing PTY instead of allocating a
      // new one and consequently missing its status hooks.
      return App.ListPtySessions().then((ids) => ids
        .map((id) => Number(id))
        .filter((pty_id) => Number.isFinite(pty_id))
        .map((pty_id) => ({ pty_id, cwd: "", title: "", alive: true })));

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

    // Git / gh
    case "run_git":
      return App.RunGit(args.cwd, args.args ?? []);
    case "run_gh":
      return App.RunGh(args.cwd, args.args ?? []);

    // Checkpoints — pre-turn worktree snapshots (src-wails/checkpoints.go)
    case "create_checkpoint":
      return App.CreateCheckpoint(args.cwd, String(args.ptyId ?? ""), args.label ?? "");
    case "list_checkpoints":
      return App.ListCheckpoints(args.cwd, args.limit ?? 50);
    case "checkpoint_diff":
      return App.CheckpointDiff(args.cwd, args.commit);
    case "restore_checkpoint":
      return App.RestoreCheckpoint(args.cwd, args.commit);

    // Workspace search (⌘P)
    case "search_files":
      return App.SearchFiles(args.cwd, args.query, args.limit ?? 30);

    // FS / misc
    case "write_text_file":
      return App.WriteTextFile(args.path, args.content);
    case "read_text_file":
    case "read_text_file_checked":
      return App.ReadTextFile(args.path);
    case "read_file_base64":
      return App.ReadFileBase64(args.path);
    case "home_dir":
      return App.HomeDir();
    case "config_file_path":
      return App.ConfigFilePath();
    case "create_dir":
      return App.CreateDir(args.path);
    case "read_dir_shallow":
      return App.ReadDirShallow(args.path);
    case "open_path_in":
      return App.OpenPathIn(args.path, args.target);
    case "list_open_targets":
      return App.ListOpenTargets();
    case "get_app_version":
      return App.GetAppVersion();
    case "set_sleep_inhibit":
      return App.SetSleepInhibit(!!args.active);
    case "get_hook_server_port":
      return App.GetHookServerPort();
    case "set_http_enabled":
      return App.SetHttpEnabled(!!args.enabled);

    // Claude Code — `id` is the frontend's chat id; the Go side emits
    // `claude-data-<id>` under exactly that name, so it must round-trip.
    case "claude_start":
      return App.ClaudeStart(
        String(args.id),
        args.cwd ?? "",
        args.resumeSessionId ?? args.resume_session_id ?? "",
        args.permissionMode ?? args.permission_mode ?? "",
        args.appendSystemPrompt ?? args.append_system_prompt ?? "",
        args.model ?? "",
        args.effort ?? "",
        args.configDir ?? args.config_dir ?? "",
        args.profileCommand ?? args.profile_command ?? "",
        args.profileArgs ?? args.profile_args ?? "",
      );
    case "claude_send":
      return App.ClaudeSend(String(args.id), args.text ?? "", args.sessionId ?? args.session_id ?? "", args.images ?? []);
    case "claude_stop":
      return App.ClaudeStop(String(args.id));
    case "claude_abort":
      return App.ClaudeAbort(String(args.id));

    // ACP / Codex — the Go bridge owns the JSON-RPC handshake and emits
    // `acp-data-<id>` / `acp-req-<id>` under the frontend's chat id.
    case "acp_start":
      return App.AcpStart({
        id: String(args.id),
        cwd: args.cwd ?? "",
        command: args.command ?? "",
        args: args.args ?? [],
        env: args.env ?? {},
        kind: args.kind ?? "custom",
        configDir: args.configDir ?? args.config_dir ?? "",
        envFile: args.envFile ?? args.env_file ?? "",
        resumeSessionId: args.resumeSessionId ?? args.resume_session_id ?? "",
        emitHistory: !!(args.emitHistory ?? args.emit_history),
      } as any);
    case "codex_start":
      return App.CodexStart(String(args.id), args.cwd ?? "", args.env ?? {}, args.resumeSessionId ?? args.resume_session_id ?? "");
    case "acp_send":
      return App.AcpSend(String(args.id), args.text ?? "", args.images ?? []);
    case "codex_send":
      return App.CodexSend(String(args.id), args.text ?? "", args.images ?? []);
    case "acp_set_mode":
      return App.AcpSetMode(String(args.id), args.modeId ?? args.mode_id ?? "");
    case "acp_set_config":
      return App.AcpSetConfig(String(args.id), args.configId ?? args.config_id ?? "", args.value ?? "");
    case "acp_list_sessions":
      return App.AcpListSessions(String(args.id), args.cwd ?? "");
    case "acp_stop":
      return App.AcpStop(String(args.id));
    case "codex_stop":
      return App.CodexStop(String(args.id));
    case "codex_list_models":
      return App.CodexListModels(args.cwd ?? "");

    // LSP
    case "lsp_start":
      return App.LspStart(args.id, args.command, args.args ?? [], args.cwd ?? "");
    case "lsp_send":
      return App.LspSend(args.id, args.message);
    case "lsp_stop":
      return App.LspStop(args.id);

    // Providers
    case "probe_provider":
      return App.ProbeProvider(args.binary ?? "", args.cwd ?? "");

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
      return App.ClaudeRespondControl(String(args.id), args.requestId ?? args.request_id, args.response);
    case "acp_respond_permission":
      return App.AcpRespondPermission(String(args.id), args.rpcId ?? args.rpc_id, args.optionId ?? args.option_id);
    case "acp_respond_user_input":
      return App.AcpRespondUserInput(String(args.id), args.rpcId ?? args.rpc_id, args.text);

    // Misc
    case "system_stats":
      return App.SystemStats();
    case "list_fonts":
      return App.ListFonts();
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

    // Float bubble windows were removed (Wails v2 has no multi-window
    // support; see plan). request_float_snapshot/send_float_snapshot/
    // notify_float_grid stay — TaskLiveTerm.vue's task live-view reuses
    // the same event protocol and isn't a float window.
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
    case "ack_control_action":
      return App.AckControlAction(args.id, args.resultJson ?? "", args.errMsg ?? "");
    case "control_verbs":
      return App.ControlVerbs();
    case "repair_agent_status":
      return App.RepairAgentStatus();
    case "reinstall_status_hooks":
      return App.ReinstallStatusHooks();
    case "remove_status_hooks":
      return App.RemoveStatusHooks();
    case "format_source":
      return App.FormatSource(args.path, args.content, args.cwd);

    // Remote HTTP server / Tailscale
    case "get_http_server_status":
      return App.GetHttpServerStatus();
    case "regenerate_pair_code":
      return App.RegeneratePairCode();
    case "get_tailscale_status":
      return App.GetTailscaleStatus();
    case "set_tailscale_serve":
      return App.SetTailscaleServe(!!args.enabled, args.port);

    default:
      console.warn(`[wails-compat] invoke("${cmd}") has no Go binding yet`);
      throw new Error(`command not implemented in Go backend: ${cmd}`);
  }
}

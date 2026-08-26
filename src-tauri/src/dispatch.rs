//! Single command-dispatch router (jean pattern).
//!
//! Routes Burrow backend logic through one `dispatch_command(app, command, args)`
//! so the same command surface is reachable from Tauri IPC and (later) HTTP/WS/MCP.
//!
//! Scheme A: each arm deserializes named fields out of the JSON `args` blob and
//! calls the *same* `#[tauri::command]` fn the IPC layer exposes — no handler edits.

use serde_json::Value;
use tauri::{AppHandle, Manager};

/// Dispatch a command by name to the corresponding Rust handler.
/// Mirrors Tauri's invoke system but transport-agnostic.
pub async fn dispatch_command(
    app: &AppHandle,
    command: &str,
    args: Value,
) -> Result<Value, String> {
    match command {
        // =====================================================================
        // Batch 1 — FS + git (pure request/response)
        // =====================================================================
        "read_dir_shallow" => {
            let path: String = from_field(&args, "path")?;
            to_value(crate::read_dir_shallow(path)?)
        }
        "read_text_file" => {
            let path: String = from_field(&args, "path")?;
            to_value(crate::read_text_file(path))
        }
        "read_text_file_checked" => {
            let path: String = from_field(&args, "path")?;
            to_value(crate::read_text_file_checked(path)?)
        }
        "write_text_file" => {
            let path: String = from_field(&args, "path")?;
            let content: String = from_field(&args, "content")?;
            crate::write_text_file(path, content)?;
            Ok(Value::Null)
        }
        "read_file_base64" => {
            let path: String = from_field(&args, "path")?;
            to_value(crate::read_file_base64(path)?)
        }
        "run_git" => {
            let cwd: String = from_field(&args, "cwd")?;
            let git_args: Vec<String> = from_field(&args, "args")?;
            to_value(crate::run_git(cwd, git_args).await)
        }
        "run_gh" => {
            let cwd: String = from_field(&args, "cwd")?;
            let gh_args: Vec<String> = from_field(&args, "args")?;
            to_value(crate::run_gh(cwd, gh_args).await)
        }
        "format_source" => {
            let path: String = from_field(&args, "path")?;
            let content: String = from_field(&args, "content")?;
            let cwd: String = from_field(&args, "cwd")?;
            to_value(crate::format_source(path, content, cwd).await?)
        }
        "open_path_in" => {
            let path: String = from_field(&args, "path")?;
            let target: String = from_field(&args, "target")?;
            crate::open_path_in(path, target)?;
            Ok(Value::Null)
        }
        "save_temp_image" => {
            let b64: String = from_field(&args, "b64")?;
            let ext: String = from_field(&args, "ext")?;
            to_value(crate::save_temp_image(b64, ext)?)
        }
        "scaffold_burrow_dir" => {
            let workspace_path: String = field(&args, "workspacePath", "workspace_path")?;
            let default_manager_prompt: String =
                field(&args, "defaultManagerPrompt", "default_manager_prompt")?;
            crate::scaffold_burrow_dir(workspace_path, default_manager_prompt)?;
            Ok(Value::Null)
        }
        // =====================================================================
        // Batch 2 — Workspaces / DB
        // =====================================================================
        "list_workspaces" => {
            to_value(crate::list_workspaces(app.state::<crate::DbState>())?)
        }
        "create_workspace" => {
            let name: String = from_field(&args, "name")?;
            let path: String = from_field(&args, "path")?;
            to_value(crate::create_workspace(name, path, app.state::<crate::DbState>())?)
        }
        "delete_workspace" => {
            let id: i64 = from_field(&args, "id")?;
            crate::delete_workspace(id, app.state::<crate::DbState>())?;
            Ok(Value::Null)
        }
        "rename_workspace" => {
            let id: i64 = from_field(&args, "id")?;
            let name: String = from_field(&args, "name")?;
            crate::rename_workspace(id, name, app.state::<crate::DbState>())?;
            Ok(Value::Null)
        }
        "touch_workspace" => {
            let id: i64 = from_field(&args, "id")?;
            crate::touch_workspace(id, app.state::<crate::DbState>())?;
            Ok(Value::Null)
        }
        "create_worktree" => {
            let parent_id: i64 = field(&args, "parentId", "parent_id")?;
            let branch: String = from_field(&args, "branch")?;
            let base_ref: Option<String> = field_opt(&args, "baseRef", "base_ref")?;
            let path: String = from_field(&args, "path")?;
            to_value(crate::create_worktree(
                parent_id,
                branch,
                base_ref,
                path,
                app.clone(),
                app.state::<crate::DbState>(),
            )?)
        }
        "remove_worktree" => {
            let id: i64 = from_field(&args, "id")?;
            let force: bool = from_field(&args, "force")?;
            crate::remove_worktree(id, force, app.state::<crate::DbState>())?;
            Ok(Value::Null)
        }
        "list_terminal_tabs" => {
            let workspace_id: i64 = field(&args, "workspaceId", "workspace_id")?;
            to_value(crate::list_terminal_tabs(workspace_id, app.state::<crate::DbState>())?)
        }
        "save_terminal_tabs" => {
            let workspace_id: i64 = field(&args, "workspaceId", "workspace_id")?;
            let tabs: Vec<crate::TerminalTab> = from_field(&args, "tabs")?;
            crate::save_terminal_tabs(workspace_id, tabs, app.state::<crate::DbState>())?;
            Ok(Value::Null)
        }
        "list_mission_tasks" => {
            to_value(crate::list_mission_tasks(app.state::<crate::DbState>())?)
        }
        "upsert_mission_task" => {
            let task: crate::MissionTask = from_field(&args, "task")?;
            crate::upsert_mission_task(task, app.state::<crate::DbState>())?;
            Ok(Value::Null)
        }
        "delete_mission_task" => {
            let id: String = from_field(&args, "id")?;
            crate::delete_mission_task(id, app.state::<crate::DbState>())?;
            Ok(Value::Null)
        }

        // =====================================================================
        // Batch 3 — PTY control + daemon
        // =====================================================================
        "write_pty" => {
            let id: u32 = from_field(&args, "id")?;
            let data: Vec<u8> = from_field(&args, "data")?;
            crate::write_pty(id, data, app.state::<crate::DaemonState>())?;
            Ok(Value::Null)
        }
        "resize_pty" => {
            let id: u32 = from_field(&args, "id")?;
            let cols: u16 = from_field(&args, "cols")?;
            let rows: u16 = from_field(&args, "rows")?;
            crate::resize_pty(id, cols, rows, app.state::<crate::DaemonState>())?;
            Ok(Value::Null)
        }
        "kill_pty" => {
            let id: u32 = from_field(&args, "id")?;
            crate::kill_pty(id, app.state::<crate::DaemonState>());
            Ok(Value::Null)
        }
        "detach_pty" => {
            let id: u32 = from_field(&args, "id")?;
            crate::detach_pty(id, app.state::<crate::DaemonState>());
            Ok(Value::Null)
        }
        "list_pty_sessions" => {
            to_value(crate::list_pty_sessions(app.state::<crate::DaemonState>()))
        }
        "get_pty_foreground" => {
            let id: u32 = from_field(&args, "id")?;
            to_value(crate::get_pty_foreground(id, app.state::<crate::DaemonState>()))
        }
        "register_tmux_win" => {
            let win_id: String = field(&args, "winId", "win_id")?;
            let pty_id: u32 = field(&args, "ptyId", "pty_id")?;
            crate::register_tmux_win(win_id, pty_id, app.clone());
            Ok(Value::Null)
        }
        "is_pid_alive" => {
            let pid: u32 = from_field(&args, "pid")?;
            to_value(crate::is_pid_alive(pid))
        }
        "daemon_stats" => {
            to_value(crate::daemon_stats(app.state::<crate::DaemonState>(), app.clone()))
        }
        "clean_daemon" => {
            to_value(crate::clean_daemon(app.state::<crate::DaemonState>()))
        }
        "kill_orphan_sessions" => {
            let keep_ids: Vec<u32> = field(&args, "keepIds", "keep_ids")?;
            to_value(crate::kill_orphan_sessions(
                keep_ids,
                app.state::<crate::DaemonState>(),
                app.state::<crate::DbState>(),
            ))
        }
        "restart_daemon" => {
            to_value(crate::restart_daemon(app.state::<crate::DaemonState>(), app.clone())?)
        }
        "system_stats" => {
            to_value(crate::system_stats())
        }
        "set_max_agents" => {
            let n: u32 = from_field(&args, "n")?;
            crate::set_max_agents(n, app.clone());
            Ok(Value::Null)
        }

        // =====================================================================
        // Batch 4 — Agent IPC send-side (Claude / ACP / LSP)
        // =====================================================================
        "claude_send" => {
            let id: u32 = from_field(&args, "id")?;
            let text: String = from_field(&args, "text")?;
            let session_id: Option<String> = field_opt(&args, "sessionId", "session_id")?;
            let images: Option<Vec<String>> = from_field_opt(&args, "images")?;
            crate::claude_send(app.state::<crate::ClaudeState>(), id, text, session_id, images)?;
            Ok(Value::Null)
        }
        "claude_stop" => {
            let id: u32 = from_field(&args, "id")?;
            crate::claude_stop(app.state::<crate::ClaudeState>(), id);
            Ok(Value::Null)
        }
        "claude_abort" => {
            let id: u32 = from_field(&args, "id")?;
            crate::claude_abort(app.state::<crate::ClaudeState>(), id);
            Ok(Value::Null)
        }
        "claude_respond_control" => {
            let id: u32 = from_field(&args, "id")?;
            let request_id: String = field(&args, "requestId", "request_id")?;
            let response: Value = from_field(&args, "response")?;
            crate::claude_respond_control(app.state::<crate::ClaudeState>(), id, request_id, response)?;
            Ok(Value::Null)
        }
        "remote_sync_chat" => {
            let chat: crate::RemoteChat = from_field(&args, "chat")?;
            crate::remote_sync_chat(app.clone(), app.state::<crate::RemoteChatState>(), chat);
            Ok(Value::Null)
        }
        "remote_list_chats" => {
            to_value(crate::remote_list_chats(app.state::<crate::RemoteChatState>()))
        }
        "remote_create_chat" => {
            let workspace_id: i64 = field(&args, "workspaceId", "workspace_id")?;
            let agent_kind: String = field(&args, "agentKind", "agent_kind")?;
            to_value(crate::remote_create_chat(
                app.clone(),
                app.state::<crate::RemoteChatState>(),
                app.state::<crate::ClaudeState>(),
                app.state::<crate::AcpState>(),
                app.state::<crate::DbState>(),
                workspace_id,
                agent_kind,
            ).await?)
        }
        "acp_send" => {
            let id: u32 = from_field(&args, "id")?;
            let text: String = from_field(&args, "text")?;
            let images: Option<Vec<String>> = from_field_opt(&args, "images")?;
            to_value(crate::acp_send(app.state::<crate::AcpState>(), id, text, images)?)
        }
        "acp_set_mode" => {
            let id: u32 = from_field(&args, "id")?;
            let mode_id: String = field(&args, "modeId", "mode_id")?;
            to_value(crate::acp_set_mode(app.state::<crate::AcpState>(), id, mode_id)?)
        }
        "acp_set_config" => {
            let id: u32 = from_field(&args, "id")?;
            let config_id: String = field(&args, "configId", "config_id")?;
            let value: String = from_field(&args, "value")?;
            to_value(crate::acp_set_config(app.state::<crate::AcpState>(), id, config_id, value)?)
        }
        "acp_list_sessions" => {
            let id: u32 = from_field(&args, "id")?;
            let cwd: String = from_field(&args, "cwd")?;
            to_value(crate::acp_list_sessions(app.state::<crate::AcpState>(), id, cwd)?)
        }
        "acp_stop" => {
            let id: u32 = from_field(&args, "id")?;
            crate::acp_stop(app.state::<crate::AcpState>(), id);
            Ok(Value::Null)
        }
        "acp_respond_permission" => {
            let id: u32 = from_field(&args, "id")?;
            let rpc_id: u64 = field(&args, "rpcId", "rpc_id")?;
            let option_id: String = field(&args, "optionId", "option_id")?;
            crate::acp_respond_permission(app.state::<crate::AcpState>(), id, rpc_id, option_id)?;
            Ok(Value::Null)
        }
        "lsp_send" => {
            let id: u32 = from_field(&args, "id")?;
            let message: String = from_field(&args, "message")?;
            crate::lsp_send(app.state::<crate::LspState>(), id, message)?;
            Ok(Value::Null)
        }
        "lsp_stop" => {
            let id: u32 = from_field(&args, "id")?;
            crate::lsp_stop(app.state::<crate::LspState>(), id);
            Ok(Value::Null)
        }
        "claude_get_account" => {
            let cwd: String = from_field(&args, "cwd")?;
            to_value(crate::claude_get_account(app.state::<crate::AccountInfoCache>(), cwd).await?)
        }

        // =====================================================================
        // Batch 5 — Config / skills / MCP + Claude reads + misc
        // =====================================================================
        "get_config_dirs" => {
            to_value(crate::get_config_dirs(app.clone()))
        }
        "set_config_dirs" => {
            let claude: Vec<String> = from_field(&args, "claude")?;
            let codex: Vec<String> = from_field(&args, "codex")?;
            let copilot: Vec<String> = from_field(&args, "copilot")?;
            to_value(crate::set_config_dirs(app.clone(), claude, codex, copilot))
        }
        "list_skills" => {
            to_value(crate::list_skills(app.clone()))
        }
        "set_skill_enabled" => {
            let dir: String = from_field(&args, "dir")?;
            let enabled: bool = from_field(&args, "enabled")?;
            crate::set_skill_enabled(dir, enabled)?;
            Ok(Value::Null)
        }
        "delete_skill" => {
            let dir: String = from_field(&args, "dir")?;
            crate::delete_skill(dir)?;
            Ok(Value::Null)
        }
        "list_mcp_servers" => {
            to_value(crate::list_mcp_servers(app.clone())?)
        }
        "add_mcp_server" => {
            let name: String = from_field(&args, "name")?;
            let config: String = from_field(&args, "config")?;
            crate::add_mcp_server(app.clone(), name, config)?;
            Ok(Value::Null)
        }
        "remove_mcp_server" => {
            let name: String = from_field(&args, "name")?;
            crate::remove_mcp_server(app.clone(), name)?;
            Ok(Value::Null)
        }
        "reinstall_status_hooks" => {
            crate::reinstall_status_hooks(app.clone());
            Ok(Value::Null)
        }
        "remove_status_hooks" => {
            crate::remove_status_hooks(app.clone());
            Ok(Value::Null)
        }
        "repair_agent_status" => {
            to_value(crate::repair_agent_status(app.clone()))
        }
        "get_hook_server_port" => {
            to_value(crate::get_hook_server_port())
        }
        "set_sleep_inhibit" => {
            let active: bool = from_field(&args, "active")?;
            crate::set_sleep_inhibit(active)?;
            Ok(Value::Null)
        }
        "read_claude_result" => {
            let cwd: String = from_field(&args, "cwd")?;
            let session_id: String = field(&args, "sessionId", "session_id")?;
            let config_dir: Option<String> = field_opt(&args, "configDir", "config_dir")?;
            to_value(crate::read_claude_result(cwd, session_id, config_dir))
        }
        "read_claude_outcome" => {
            let cwd: String = from_field(&args, "cwd")?;
            let session_id: String = field(&args, "sessionId", "session_id")?;
            let config_dir: Option<String> = field_opt(&args, "configDir", "config_dir")?;
            to_value(crate::read_claude_outcome(cwd, session_id, config_dir))
        }
        "read_claude_activity" => {
            let cwd: String = from_field(&args, "cwd")?;
            let session_id: String = field(&args, "sessionId", "session_id")?;
            let config_dir: Option<String> = field_opt(&args, "configDir", "config_dir")?;
            to_value(crate::read_claude_activity(cwd, session_id, config_dir))
        }
        "list_claude_sessions" => {
            let cwd: String = from_field(&args, "cwd")?;
            let config_dir: Option<String> = field_opt(&args, "configDir", "config_dir")?;
            to_value(crate::list_claude_sessions(app.clone(), cwd, config_dir))
        }
        "read_claude_transcript" => {
            let cwd: String = from_field(&args, "cwd")?;
            let session_id: String = field(&args, "sessionId", "session_id")?;
            let config_dir: Option<String> = field_opt(&args, "configDir", "config_dir")?;
            to_value(crate::read_claude_transcript(cwd, session_id, config_dir))
        }
        "claude_plan_usage" => {
            let config_dir: Option<String> = field_opt(&args, "configDir", "config_dir")?;
            let force: Option<bool> = from_field_opt(&args, "force")?;
            to_value(crate::claude_plan_usage(app.clone(), config_dir, force))
        }
        "claude_usage_5h" => {
            let config_dir: Option<String> = field_opt(&args, "configDir", "config_dir")?;
            to_value(crate::claude_usage_5h(app.clone(), config_dir))
        }
        "get_app_version" => {
            to_value(crate::get_app_version())
        }
        "get_float_params" => {
            let label: String = from_field(&args, "label")?;
            to_value(crate::get_float_params(label, app.state::<crate::FloatParamsState>()))
        }

        _ => Err(format!("Unknown command: {command}")),
    }
}

// =============================================================================
// Helpers (verbatim from jean's http_server/dispatch.rs)
// =============================================================================

fn to_value<T: serde::Serialize>(val: T) -> Result<Value, String> {
    serde_json::to_value(val).map_err(|e| format!("Serialization error: {e}"))
}

fn from_field<T: serde::de::DeserializeOwned>(args: &Value, field: &str) -> Result<T, String> {
    args.get(field)
        .ok_or_else(|| format!("Missing field: {field}"))
        .and_then(|v| {
            serde_json::from_value(v.clone()).map_err(|e| format!("Invalid field '{field}': {e}"))
        })
}

#[allow(dead_code)]
fn from_field_opt<T: serde::de::DeserializeOwned>(
    args: &Value,
    field: &str,
) -> Result<Option<T>, String> {
    match args.get(field) {
        None | Some(Value::Null) => Ok(None),
        Some(v) => serde_json::from_value(v.clone())
            .map(Some)
            .map_err(|e| format!("Invalid field '{field}': {e}")),
    }
}

/// Try camelCase field first, then snake_case. For required fields.
fn field<T: serde::de::DeserializeOwned>(
    args: &Value,
    camel: &str,
    snake: &str,
) -> Result<T, String> {
    from_field(args, camel).or_else(|_| from_field(args, snake))
}

/// Try camelCase field first, then snake_case. For optional fields.
#[allow(dead_code)]
fn field_opt<T: serde::de::DeserializeOwned>(
    args: &Value,
    camel: &str,
    snake: &str,
) -> Result<Option<T>, String> {
    let camel_result = from_field_opt(args, camel)?;
    if camel_result.is_some() {
        return Ok(camel_result);
    }
    from_field_opt(args, snake)
}

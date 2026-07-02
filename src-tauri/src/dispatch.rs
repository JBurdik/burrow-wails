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

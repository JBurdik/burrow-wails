//! Single command-dispatch router (jean pattern).
//!
//! Routes Burrow backend logic through one `dispatch_command(app, command, args)`
//! so the same command surface is reachable from Tauri IPC and (later) HTTP/WS/MCP.
//!
//! Scheme A: each arm deserializes named fields out of the JSON `args` blob and
//! calls the *same* `#[tauri::command]` fn the IPC layer exposes — no handler edits.

use serde_json::Value;
use tauri::AppHandle;

/// Dispatch a command by name to the corresponding Rust handler.
/// Mirrors Tauri's invoke system but transport-agnostic.
pub async fn dispatch_command(
    _app: &AppHandle,
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

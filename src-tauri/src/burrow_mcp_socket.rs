//! Parent-side in-process Unix domain socket for the Burrow MCP server.
//!
//! Owns the live `AppHandle`, so tool logic runs in-process with full app
//! state. Per connection: read one newline-delimited JSON request, validate the
//! bearer token, then dispatch through `burrow_mcp_core::call_tool`. No HTTP
//! port. Windows is unsupported (decision §5.4) — a `cfg` stub returns an error.

use std::path::PathBuf;

use serde_json::{json, Value};
use tauri::{AppHandle, Manager};

use crate::burrow_mcp_core::{call_tool, extract_tool_call, jsonrpc_error, jsonrpc_ok};

pub const SOCKET_FILE: &str = "burrow-mcp.sock";
pub const TOKEN_FILE: &str = "burrow-mcp.token";

pub fn socket_path(app: &AppHandle) -> Option<PathBuf> {
    Some(app.path().app_data_dir().ok()?.join(SOCKET_FILE))
}

pub fn token_path(app: &AppHandle) -> Option<PathBuf> {
    Some(app.path().app_data_dir().ok()?.join(TOKEN_FILE))
}

/// The socket path + token that `build_burrow_mcp_config` bakes into a spawned
/// child's env. Token is read from the file written at startup.
pub fn socket_token(app: &AppHandle) -> Option<String> {
    std::fs::read_to_string(token_path(app)?).ok().map(|s| s.trim().to_string())
}

/// Cheap ephemeral bearer token for a local, 0600 socket. Not cryptographic
/// randomness — the socket file perms are the real access control; the token
/// just stops a stray connect from a different app instance.
fn gen_token() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    let pid = std::process::id();
    let stack = &nanos as *const _ as usize;
    format!("{nanos:x}{pid:x}{stack:x}")
}

/// Start the socket server (called once at Tauri setup). Generates + persists a
/// token, binds `<app_data>/burrow-mcp.sock` (chmod 0600), and serves in a
/// background task. Idempotent-ish: rebinds on each launch.
pub fn start(app: &AppHandle) {
    let Some(path) = socket_path(app) else { return };
    let Some(tok_path) = token_path(app) else { return };
    let token = gen_token();
    if let Some(parent) = tok_path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    let _ = std::fs::write(&tok_path, &token);
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        if let Err(e) = serve(app, path, token).await {
            log::warn!("Burrow MCP socket failed to start: {e}");
        }
    });
}

#[cfg(unix)]
async fn serve(app: AppHandle, path: PathBuf, token: String) -> Result<(), String> {
    use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
    use tokio::net::UnixListener;

    if let Some(parent) = path.parent() {
        tokio::fs::create_dir_all(parent)
            .await
            .map_err(|e| format!("Failed to create Burrow MCP socket dir: {e}"))?;
    }
    if path.exists() {
        let _ = tokio::fs::remove_file(&path).await;
    }

    let listener = UnixListener::bind(&path)
        .map_err(|e| format!("Failed to bind Burrow MCP socket {}: {e}", path.display()))?;
    {
        use std::os::unix::fs::PermissionsExt;
        let perms = std::fs::Permissions::from_mode(0o600);
        std::fs::set_permissions(&path, perms)
            .map_err(|e| format!("Failed to lock down Burrow MCP socket perms: {e}"))?;
    }
    log::info!("Burrow MCP socket listening at {}", path.display());

    loop {
        match listener.accept().await {
            Ok((stream, _addr)) => {
                let app = app.clone();
                let expected = token.clone();
                tokio::spawn(async move {
                    let (read_half, mut write_half) = stream.into_split();
                    let mut reader = BufReader::new(read_half);
                    let mut line = String::new();
                    let response = match tokio::time::timeout(
                        std::time::Duration::from_secs(30),
                        reader.read_line(&mut line),
                    )
                    .await
                    {
                        Ok(Ok(0)) => json!({"error":"empty request"}),
                        Ok(Ok(_)) => handle_socket_request(&app, &expected, &line).await,
                        Ok(Err(e)) => json!({"error": format!("read failed: {e}")}),
                        Err(_) => json!({"error":"read timeout"}),
                    };
                    if let Ok(encoded) = serde_json::to_string(&response) {
                        let _ = write_half.write_all(encoded.as_bytes()).await;
                        let _ = write_half.write_all(b"\n").await;
                        let _ = write_half.flush().await;
                    }
                });
            }
            Err(e) => log::warn!("Burrow MCP socket accept failed: {e}"),
        }
    }
}

#[cfg(not(unix))]
async fn serve(_app: AppHandle, _path: PathBuf, _token: String) -> Result<(), String> {
    Err("Burrow MCP local IPC is only supported on Unix".to_string())
}

fn tokens_eq(provided: &str, expected: &str) -> bool {
    // Length-checked byte compare. The 0600 socket perms are the real gate.
    provided.as_bytes() == expected.as_bytes()
}

async fn handle_socket_request(app: &AppHandle, expected_token: &str, line: &str) -> Value {
    let body: Value = match serde_json::from_str(line) {
        Ok(v) => v,
        Err(e) => return json!({"error": format!("invalid json: {e}")}),
    };
    let provided = body.get("token").and_then(|v| v.as_str()).unwrap_or("");
    if !tokens_eq(provided, expected_token) {
        return json!({"error":"unauthorized"});
    }

    let tool_call = match extract_tool_call(body.clone()) {
        Ok(tool_call) => tool_call,
        Err(e) => return jsonrpc_error(None, e.code, &e.message),
    };
    let source = body.get("source").and_then(|v| v.as_str()).unwrap_or("anon").to_string();
    let depth = body.get("depth").and_then(|v| v.as_u64()).unwrap_or(0) as u32;

    // call_tool is sync and may block (spawn wait: true); keep it off the runtime
    // worker so a long-running tool doesn't stall other connections.
    let app = app.clone();
    let name = tool_call.name;
    let args = tool_call.arguments;
    let result = tauri::async_runtime::spawn_blocking(move || {
        call_tool(&app, &name, args, &source, depth)
    })
    .await;

    match result {
        Ok(Ok(result)) => jsonrpc_ok(None, result),
        Ok(Err(e)) => jsonrpc_error(None, e.code, &e.message),
        Err(e) => jsonrpc_error(None, -32000, &format!("tool task join error: {e}")),
    }
}

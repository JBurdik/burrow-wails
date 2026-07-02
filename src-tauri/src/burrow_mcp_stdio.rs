//! Stdio MCP transport for Burrow.
//!
//! Launched by an MCP-client CLI (Claude/Codex) as `<burrow-app-exe>
//! --burrow-mcp-stdio`. Implements NO tool logic: `initialize`/`tools/list`/
//! `ping` answer locally from the static registry; `tools/call` is proxied over
//! Burrow's Unix domain socket to the already-running desktop app, which runs
//! the tool in-process with full app state. A thin, disposable proxy — the
//! socket server is the brain.

use std::io::{BufRead, Write};

use serde_json::{json, Value};

use crate::burrow_mcp_core::{
    handle_protocol_message, jsonrpc_error, ToolCallRequest, BURROW_MCP_DEPTH_ENV,
    BURROW_MCP_SESSION_ENV, BURROW_MCP_SOCKET_ENV, BURROW_MCP_TOKEN_ENV,
};

pub fn run_stdio_server() -> Result<(), String> {
    let stdin = std::io::stdin();
    let mut stdout = std::io::stdout();

    for line in stdin.lock().lines() {
        let line = line.map_err(|e| format!("Failed to read stdin: {e}"))?;
        if line.trim().is_empty() {
            continue;
        }
        let response = handle_message(&line);
        if let Some(response) = response {
            let encoded = serde_json::to_string(&response)
                .map_err(|e| format!("Failed to encode MCP response: {e}"))?;
            writeln!(stdout, "{encoded}").map_err(|e| format!("Failed to write stdout: {e}"))?;
            stdout.flush().map_err(|e| format!("Failed to flush stdout: {e}"))?;
        }
    }
    Ok(())
}

fn handle_message(line: &str) -> Option<Value> {
    let body: Value = match serde_json::from_str(line) {
        Ok(v) => v,
        Err(e) => return Some(jsonrpc_error(None, -32700, &format!("Parse error: {e}"))),
    };
    handle_protocol_message(body, proxy_tool_call)
}

fn proxy_tool_call(tool_call: ToolCallRequest) -> Result<Value, String> {
    let socket = std::env::var(BURROW_MCP_SOCKET_ENV)
        .map_err(|_| format!("Missing {BURROW_MCP_SOCKET_ENV}"))?;
    let token = std::env::var(BURROW_MCP_TOKEN_ENV)
        .map_err(|_| format!("Missing {BURROW_MCP_TOKEN_ENV}"))?;
    let source = std::env::var(BURROW_MCP_SESSION_ENV).unwrap_or_else(|_| "anon".to_string());
    let depth = std::env::var(BURROW_MCP_DEPTH_ENV)
        .ok()
        .and_then(|s| s.parse::<u32>().ok())
        .unwrap_or(0);

    proxy_to_parent(
        &socket,
        json!({
            "token": token,
            "source": source,
            "depth": depth,
            "name": tool_call.name,
            "arguments": tool_call.arguments,
        }),
    )
}

#[cfg(unix)]
fn proxy_to_parent(socket: &str, request: Value) -> Result<Value, String> {
    use std::io::BufReader;
    use std::os::unix::net::UnixStream;
    use std::time::Duration;

    let mut stream = UnixStream::connect(socket)
        .map_err(|e| format!("Failed to connect Burrow MCP socket {socket}: {e}"))?;
    stream
        .set_read_timeout(Some(Duration::from_secs(600)))
        .map_err(|e| format!("Failed to set Burrow MCP socket read timeout: {e}"))?;
    stream
        .set_write_timeout(Some(Duration::from_secs(30)))
        .map_err(|e| format!("Failed to set Burrow MCP socket write timeout: {e}"))?;
    let encoded = serde_json::to_string(&request)
        .map_err(|e| format!("Failed to encode Burrow MCP socket request: {e}"))?;
    writeln!(stream, "{encoded}").map_err(|e| format!("Failed to write Burrow MCP socket: {e}"))?;
    stream.flush().map_err(|e| format!("Failed to flush Burrow MCP socket: {e}"))?;

    let mut reader = BufReader::new(stream);
    let mut line = String::new();
    reader
        .read_line(&mut line)
        .map_err(|e| format!("Failed to read Burrow MCP socket response: {e}"))?;
    let response: Value = serde_json::from_str(&line)
        .map_err(|e| format!("Failed to parse Burrow MCP socket response: {e}"))?;
    if let Some(error) = parent_error_message(&response) {
        return Err(error);
    }
    Ok(response.get("result").cloned().unwrap_or(Value::Null))
}

// Windows → Unix socket only (decision §5.4). macOS-first; YAGNI.
#[cfg(not(unix))]
fn proxy_to_parent(_socket: &str, _request: Value) -> Result<Value, String> {
    Err("Burrow MCP local IPC is only supported on Unix".to_string())
}

fn parent_error_message(response: &Value) -> Option<String> {
    match response.get("error") {
        Some(Value::String(message)) => Some(message.clone()),
        Some(Value::Object(error)) => error
            .get("message")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned)
            .or_else(|| Some(Value::Object(error.clone()).to_string())),
        Some(other) => Some(other.to_string()),
        None => None,
    }
}

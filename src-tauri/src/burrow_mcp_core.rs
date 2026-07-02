//! Shared Burrow MCP tool registry / JSON-RPC router / dispatch.
//!
//! Transport-agnostic: the stdio proxy and the in-process socket server both
//! frame protocol messages through here. The actual tool bodies live in
//! `lib.rs` (`crate::mcp_run_tool`) so they can reach private app state (DB,
//! daemon, session dir) and reuse the exact request-dir transport the `burrow`
//! CLI already writes. This module owns only: the registry, the depth cap, and
//! the JSON-RPC envelope.

use serde_json::{json, Value};
use tauri::AppHandle;

pub const MCP_PROTOCOL_VERSION: &str = "2024-11-05";
pub const BURROW_MCP_STDIO_ARG: &str = "--burrow-mcp-stdio";
pub const BURROW_MCP_SOCKET_ENV: &str = "BURROW_MCP_SOCKET";
pub const BURROW_MCP_TOKEN_ENV: &str = "BURROW_MCP_TOKEN";
pub const BURROW_MCP_SESSION_ENV: &str = "BURROW_MCP_SESSION";
pub const BURROW_MCP_DEPTH_ENV: &str = "BURROW_MCP_DEPTH";

/// Tools that fan out (open a tab / worktree). Only these are subject to the
/// recursion depth cap — read/observe/route tools are exempt, mirroring jean's
/// RATE_LIMITED_TOOLS split.
const SPAWNING_TOOLS: &[&str] = &["spawn", "create_worktree", "new_tab"];

#[derive(Debug)]
pub struct ToolError {
    pub code: i32,
    pub message: String,
}

impl ToolError {
    pub fn invalid_params(msg: impl Into<String>) -> Self {
        Self { code: -32602, message: msg.into() }
    }
    pub fn internal(msg: impl Into<String>) -> Self {
        Self { code: -32000, message: msg.into() }
    }
}

pub fn current_depth() -> u32 {
    std::env::var(BURROW_MCP_DEPTH_ENV)
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(0)
}

pub fn next_depth() -> u32 {
    current_depth().saturating_add(1)
}

pub fn initialize_result() -> Value {
    json!({
        "protocolVersion": MCP_PROTOCOL_VERSION,
        "capabilities": { "tools": {} },
        "serverInfo": { "name": "burrow", "version": env!("CARGO_PKG_VERSION") },
    })
}

pub fn tools_list_result() -> Value {
    json!({ "tools": tool_registry() })
}

#[derive(Debug)]
pub struct ToolCallRequest {
    pub name: String,
    pub arguments: Value,
}

pub fn extract_tool_call(params: Value) -> Result<ToolCallRequest, ToolError> {
    let name = params
        .get("name")
        .and_then(|v| v.as_str())
        .map(ToOwned::to_owned)
        .ok_or_else(|| ToolError::invalid_params("missing 'name'"))?;
    let arguments = params.get("arguments").cloned().unwrap_or_else(|| json!({}));
    Ok(ToolCallRequest { name, arguments })
}

pub fn handle_protocol_message(
    body: Value,
    call_tool: impl FnMut(ToolCallRequest) -> Result<Value, String>,
) -> Option<Value> {
    let id = body.get("id").cloned();
    let method = body.get("method").and_then(|v| v.as_str()).unwrap_or("");
    let params = body.get("params").cloned().unwrap_or(Value::Null);

    match method {
        "initialize" => Some(jsonrpc_ok(id, initialize_result())),
        "notifications/initialized" => None,
        "tools/list" => Some(jsonrpc_ok(id, tools_list_result())),
        "tools/call" => Some(
            match extract_tool_call(params).map_err(|e| e.message).and_then(call_tool) {
                Ok(result) => jsonrpc_ok(id, result),
                Err(e) => jsonrpc_error(id, -32000, &e),
            },
        ),
        "ping" => Some(jsonrpc_ok(id, json!({}))),
        _ => Some(jsonrpc_error(id, -32601, &format!("Method not found: {method}"))),
    }
}

pub fn tool_registry() -> Value {
    json!([
        {"name":"spawn","description":"Delegate work to a sub-agent in a new Burrow tab in the current project. Pass wait:true to block and return the sub-agent's captured result, or omit it to fire-and-forget and later collect_results by token.","inputSchema":{"type":"object","properties":{"cmd":{"type":"string","description":"Command line to run in the new tab, e.g. \"claude 'fix the failing test'\"."},"cwd":{"type":"string","description":"Directory the new tab runs in. Defaults to the calling session's workspace."},"token":{"type":"string","description":"Optional result token; auto-generated when wait:true."},"wait":{"type":"boolean","default":false},"timeout":{"type":"integer","minimum":1,"description":"Seconds to block when wait:true (default 600)."}},"required":["cmd"],"additionalProperties":false}},
        {"name":"create_worktree","description":"Create a git worktree off the calling session's repo on a new/existing branch and open it as a workspace.","inputSchema":{"type":"object","properties":{"branch":{"type":"string"},"base":{"type":"string","description":"Base ref for a NEW branch (default HEAD)."},"path":{"type":"string","description":"On-disk location override."}},"required":["branch"],"additionalProperties":false}},
        {"name":"new_tab","description":"Open a new terminal tab in any workspace by id. A plain UI action (not sub-agent delegation).","inputSchema":{"type":"object","properties":{"ws":{"type":"string","description":"Target workspace id (default: current workspace)."},"cmd":{"type":"string","description":"Command to run (default: an empty shell)."}},"additionalProperties":false}},
        {"name":"list_workspaces","description":"List every workspace as {id,name,path}.","inputSchema":{"type":"object","properties":{},"additionalProperties":false}},
        {"name":"list_tabs","description":"List a workspace's tabs as {ptyId,title}. Defaults to the calling session's workspace.","inputSchema":{"type":"object","properties":{"ws":{"type":"string","description":"Target workspace id."}},"additionalProperties":false}},
        {"name":"send_to_tab","description":"Write text into an existing tab's PTY (drive a running sub-agent). Append a newline to submit.","inputSchema":{"type":"object","properties":{"tabid":{"type":"integer","description":"Target pty id."},"text":{"type":"string"}},"required":["tabid","text"],"additionalProperties":false}},
        {"name":"focus_tab","description":"Activate the tab with the given pty id, switching workspace if needed.","inputSchema":{"type":"object","properties":{"tabid":{"type":"integer"}},"required":["tabid"],"additionalProperties":false}},
        {"name":"focus_workspace","description":"Switch Burrow to (and open) the workspace with the given id.","inputSchema":{"type":"object","properties":{"wsid":{"type":"string"}},"required":["wsid"],"additionalProperties":false}},
        {"name":"wait_result","description":"Block until a spawned sub-agent's result token completes, then return its captured result.","inputSchema":{"type":"object","properties":{"token":{"type":"string"},"timeout":{"type":"integer","minimum":1,"description":"Seconds (default 600)."}},"required":["token"],"additionalProperties":false}},
        {"name":"collect_results","description":"Read the captured results for one or more sub-agent tokens without blocking. Returns per-token {done, result}.","inputSchema":{"type":"object","properties":{"tokens":{"type":"array","items":{"type":"string"}}},"required":["tokens"],"additionalProperties":false}},
        {"name":"worktree_remove","description":"Remove a worktree of the calling session's repo (git worktree remove + its Burrow row). Confirm with the user first.","inputSchema":{"type":"object","properties":{"branch":{"type":"string"},"path":{"type":"string"},"force":{"type":"boolean","default":false}},"additionalProperties":false}},
        {"name":"pr_create","description":"Create a GitHub PR via gh in the given cwd (a worktree dir) or the calling session's repo.","inputSchema":{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"},"base":{"type":"string"},"head":{"type":"string"},"cwd":{"type":"string"}},"required":["title","body"],"additionalProperties":false}},
        {"name":"pr_list","description":"List GitHub PRs via gh.","inputSchema":{"type":"object","properties":{"state":{"type":"string","enum":["open","closed","merged","all"]},"cwd":{"type":"string"}},"additionalProperties":false}},
        {"name":"pr_view","description":"View a GitHub PR via gh.","inputSchema":{"type":"object","properties":{"number":{"type":"integer"},"cwd":{"type":"string"}},"required":["number"],"additionalProperties":false}},
        {"name":"pr_merge","description":"Merge a GitHub PR via gh.","inputSchema":{"type":"object","properties":{"number":{"type":"integer"},"squash":{"type":"boolean","default":false},"cwd":{"type":"string"}},"required":["number"],"additionalProperties":false}}
    ])
}

/// Transport-neutral dispatch: enforce the depth cap on spawning tools, run the
/// tool body (in `lib.rs`, with full app state), and wrap the result in the MCP
/// `content` envelope. Sync — callers on async transports should offload via
/// spawn_blocking so a blocking `wait` doesn't stall a runtime worker.
pub fn call_tool(
    app: &AppHandle,
    name: &str,
    arguments: Value,
    source: &str,
    depth: u32,
) -> Result<Value, ToolError> {
    if SPAWNING_TOOLS.contains(&name) {
        let max = crate::mcp_max_depth(app);
        if depth > max {
            return Err(ToolError::internal(format!(
                "Burrow MCP recursion depth {depth} exceeds limit {max}"
            )));
        }
    }

    let result_json = crate::mcp_run_tool(app, name, arguments, source).map_err(ToolError::internal)?;
    Ok(json!({
        "content": [{
            "type": "text",
            "text": serde_json::to_string_pretty(&result_json).unwrap_or_else(|_| "null".to_string()),
        }],
        "isError": false,
    }))
}

pub fn jsonrpc_ok(id: Option<Value>, result: Value) -> Value {
    json!({ "jsonrpc": "2.0", "id": id.unwrap_or(Value::Null), "result": result })
}

pub fn jsonrpc_error(id: Option<Value>, code: i32, message: &str) -> Value {
    json!({ "jsonrpc": "2.0", "id": id.unwrap_or(Value::Null), "error": { "code": code, "message": message } })
}

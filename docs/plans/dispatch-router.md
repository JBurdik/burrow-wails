# Implementation Spec — Single Command-Dispatch Router (jean pattern)

**Status:** Plan for review. No production code yet.
**Author:** design doc, 2026-07-02.
**Goal:** Route Burrow's backend logic through one `dispatch_command(app, command, args)` function so the same command surface is reachable from Tauri IPC **and** (later) HTTP / WebSocket / MCP / headless — without rewriting handlers or breaking the frontend.

Reference implementation studied: jean at
`.../scratchpad/jean/src-tauri/src/http_server/{mod,server,websocket,dispatch}.rs` + `src-server/src/main.rs`.

---

## 1. How jean's router works, and why it unlocks HTTP/WS/MCP/headless for free

### 1.1 The one function

`dispatch.rs`:

```rust
pub async fn dispatch_command(
    app: &AppHandle,
    command: &str,
    args: Value,          // serde_json::Value — the invoke args object
) -> Result<Value, String> {
    match command {
        "load_preferences" => {
            let result = crate::load_preferences(app.clone()).await?;
            to_value(result)
        }
        "save_preferences" => {
            let preferences = from_field(&args, "preferences")?;
            crate::save_preferences(app.clone(), preferences).await?;
            emit_cache_invalidation(app, &["preferences"]);
            Ok(Value::Null)
        }
        "add_project" => {
            let path: String = from_field(&args, "path")?;
            let parent_id: Option<String> = field_opt(&args, "parentId", "parent_id")?;
            let result = crate::projects::add_project(app.clone(), path, parent_id).await?;
            to_value(result)
        }
        // ... one arm per command ...
        _ => Err(format!("Unknown command: {command}")),
    }
}
```

Each arm does exactly three things:
1. **Deserialize** named fields out of the JSON `args` blob (`from_field`/`field`/`field_opt`).
2. **Call the existing function** — `crate::foo(app.clone(), …)`. In jean these are the *same* functions the `#[tauri::command]` layer exposes; a Tauri command is just a normal `async fn`, so it can be called directly.
3. **Serialize** the return into a `Value` (`to_value`), or emit a `cache:invalidate` side-effect for mutations.

### 1.2 The helpers (all in `dispatch.rs`)

```rust
fn to_value<T: Serialize>(val: T) -> Result<Value, String>
fn from_field<T: DeserializeOwned>(args: &Value, field: &str) -> Result<T, String>
fn from_field_opt<T: DeserializeOwned>(args: &Value, field: &str) -> Result<Option<T>, String>
fn field<T>(args: &Value, camel: &str, snake: &str) -> Result<T, String>       // camel→snake fallback
fn field_opt<T>(args: &Value, camel: &str, snake: &str) -> Result<Option<T>, String>
fn emit_cache_invalidation(app: &AppHandle, keys: &[&str])                     // app.emit_all("cache:invalidate", …)
```

`field`/`field_opt` try camelCase first, then snake_case — so the router accepts args in whatever casing the transport uses (Tauri IPC sends the Rust param names; a web client may send camelCase). This is the whole "casing tolerance" layer.

### 1.3 The transports call `dispatch_command`, not the handlers

- **WebSocket** (`websocket.rs`): a client sends `{ type:"invoke", id, command, args }`. The loop calls
  `dispatch_command(&app, &command, args)` and wraps the result into
  `{ type:"response"|"error", id, data, error }`. A `command_should_run_on_blocking_pool(cmd)` gate routes sync git/CLI-heavy commands onto `spawn_blocking` so they don't starve the WS event loop.
- **HTTP** (`server.rs`): an axum `Router` with `/ws`, `/api/init`, `/api/auth`, `/api/files/*`, health routes. WebSocket upgrade at `/ws` feeds the same dispatch. REST-ish endpoints could call `dispatch_command` directly too.
- **Headless** (`src-server/src/main.rs`, 4 lines):
  ```rust
  fn main() {
      std::env::set_var("JEAN_HEADLESS", "1");
      jean_lib::run()
  }
  ```
  A separate binary that boots the *same* lib without a window. Because every capability is a string+JSON command behind `dispatch_command`, the headless server exposes the entire app over HTTP with zero per-command work.

### 1.4 The event-fanout trick — `EmitExt` (`mod.rs`)

Handlers don't call `app.emit(...)` directly; they call `app.emit_all(...)`:

```rust
pub trait EmitExt {
    fn emit_all<S: Serialize + Clone>(&self, event: &str, payload: &S) -> Result<(), String>;
    fn emit_all_owned<S: Serialize + Clone>(&self, event: &str, payload: S) -> Result<(), String>;
}
```

`emit_all` sends to **both** the Tauri webview (`self.emit`) *and* the `WsBroadcaster` (a `broadcast::channel` held as Tauri state, with per-session / per-terminal replay ring buffers for reconnect). One call, every connected client — native + web. **This is the piece that makes streaming (PTY output, chat chunks) work over the wire, not just the request/response commands.**

### 1.5 Why "for free"

Once logic lives behind `dispatch_command(command, args) -> Result<Value>` and events go through `emit_all`:
- **HTTP/WS** = a thin server that forwards `{command,args}` in and `{data|error}` out. ← the only new code.
- **MCP** = a shim that maps MCP tool calls → `dispatch_command`; the tool list is the command list.
- **Headless** = boot the lib with no window.
- **Web UI** = same frontend, `invoke()` swapped for a WS-transport shim (jean does exactly this).

The `#[tauri::command]` layer becomes one *client* of the router among several, instead of the only entry point.

---

## 2. Inventory of Burrow's current Tauri commands

All 87 live in `src-tauri/src/lib.rs`. Classified by routability.

### Global state structs (`State<…>`) — must be reachable from a non-IPC caller

`DaemonState` (PTY daemon IPC), `DbState` (sqlite `workspaces.db`), `LspState`, `ClaudeState`, `AcpState`, `AccountInfoCache`, `FloatParamsState`, `FloatLayoutState`.

> All are already registered via `app.manage(...)`, so `app.state::<T>()` retrieves them inside `dispatch_command` given only an `&AppHandle`. **No signature carries data the router can't reconstruct from `AppHandle`** — this is the key feasibility fact. (jean handlers take `app: AppHandle` and fetch state internally; a handler that takes `State<T>` as a param can be called from the router by passing `app.state::<T>()`.)

### 2A. PURE — request/response, no frontend dependency (ROUTABLE, do these first)

DB / git / FS / PTY-control / subprocess-IPC. ~65 commands:

- **Workspaces/DB:** `list_workspaces`, `create_workspace`, `delete_workspace`, `rename_workspace`, `touch_workspace`, `create_worktree`, `remove_worktree`, `list_terminal_tabs`, `save_terminal_tabs`, `list_mission_tasks`, `upsert_mission_task`, `delete_mission_task`.
- **PTY control:** `write_pty`, `resize_pty`, `kill_pty`, `detach_pty`, `list_pty_sessions`, `get_pty_foreground`, `register_tmux_win`, `is_pid_alive`.
- **Daemon:** `daemon_stats`, `clean_daemon`, `kill_orphan_sessions`, `restart_daemon`, `system_stats`, `set_max_agents`.
- **Git/GH/format:** `run_git`, `run_gh`, `format_source`.
- **FS:** `read_dir_shallow`, `read_text_file`, `read_text_file_checked`, `write_text_file`, `read_file_base64`, `save_temp_image`, `scaffold_burrow_dir`, `open_path_in`.
- **Agent subprocess IPC (send/control side):** `claude_send`, `claude_stop`, `claude_abort`, `claude_respond_control`, `acp_send`, `acp_set_mode`, `acp_set_config`, `acp_list_sessions`, `acp_stop`, `acp_respond_permission`, `lsp_send`, `lsp_stop`, `claude_get_account`.
- **Config/skills/MCP:** `get_config_dirs`, `set_config_dirs`, `list_skills`, `set_skill_enabled`, `delete_skill`, `list_mcp_servers`, `add_mcp_server`, `remove_mcp_server`, `reinstall_status_hooks`, `remove_status_hooks`, `repair_agent_status`, `get_hook_server_port`, `set_sleep_inhibit`.
- **Claude session reads:** `read_claude_result`, `read_claude_outcome`, `read_claude_activity`, `list_claude_sessions`, `read_claude_transcript`, `claude_plan_usage`, `claude_usage_5h`.
- **Misc:** `get_app_version`, `get_float_params` (consumes stored params — pure read).

### 2B. EVENT-EMITTER — spawn + stream output as `*-{id}` events (ROUTABLE for the *call*, streaming needs §4)

- `create_pty` → emits `pty-data-{id}` (+ drives `pty-hook-{id}`, `pty-flash-{id}` via hook server)
- `lsp_start` → `lsp-msg-{id}`
- `claude_start` → `claude-data-{id}`
- `acp_start` → `acp-data-{id}`
- `request_float_snapshot` → `float-snap-req-{id}`; `send_float_snapshot` → `float-snap-{id}`; `notify_float_grid` → `float-grid-{id}`
- `take_spawn_requests` → **hybrid**: emits `workspaces-changed` AND returns `Vec<SpawnRequest>` the frontend must act on (see 2C).

The **call** (spawn) is routable — it returns an id. The **stream** it starts only reaches a web client if the emit goes through an `emit_all`-style dual sink (§4).

### 2C. UI-ACTION — needs the renderer / native window (NOT purely routable)

- **Float windows (native Tauri window APIs):** `open_float_window`, `open_git_panel_window`, `set_window_size`, `set_float_corner`, `snap_float_window`, `sync_float_size`, `close_float_window`. These build/move/close OS webview windows — meaningless headless. Leave IPC-only.
- **`take_spawn_requests` return payload:** the `Vec<SpawnRequest>` (focus-workspace, focus-tab, new-tab, worktree-create) is consumed by `Terminal.vue`'s poll to mutate Pinia/UI state. The *read* half (list-workspaces/tabs, answered in Rust via result files) is pure; the *action* half is UI. Router can serve the read half; the action half stays a frontend poll.

### Frontend coupling (why nothing breaks if we keep IPC)

Every store/component calls these via `invoke("<name>", args)` — e.g. `workspace.ts` → `list_workspaces`/`create_workspace`/…; `git.ts` → `run_git`/`run_gh`; `XTerm.vue` → `write_pty`/`create_pty`/`resize_pty`; `ClaudeChat.vue` → `claude_start`/`acp_*`; `Terminal.vue` → `save_terminal_tabs`/`kill_pty`. Subscriptions use `listen(\`pty-data-${id}\`)`, `listen(\`claude-data-${id}\`)`, etc. **As long as `#[tauri::command]` names and event names are unchanged, the frontend is untouched.** The migration is purely additive on the backend.

---

## 3. Incremental migration plan

Principle: **extract logic into plain fns, add a router that calls them, make `#[tauri::command]` handlers delegate to the router.** Each command migrates independently; nothing breaks between steps.

### Step 0 — module scaffold (no behavior change)

New module `src-tauri/src/dispatch.rs` (mirrors jean's `http_server/dispatch.rs`, minus the server). Add `mod dispatch;` to `lib.rs`. Copy jean's helpers verbatim:

```rust
// dispatch.rs
use serde_json::Value;
use tauri::{AppHandle, Manager};

pub async fn dispatch_command(app: &AppHandle, command: &str, args: Value) -> Result<Value, String> {
    match command {
        _ => Err(format!("Unknown command: {command}")),
    }
}

fn to_value<T: serde::Serialize>(v: T) -> Result<Value, String> { … }
fn from_field<T: DeserializeOwned>(args: &Value, f: &str) -> Result<T, String> { … }
fn field<T>(args: &Value, camel: &str, snake: &str) -> Result<T, String> { … }
fn field_opt<T>(args: &Value, camel: &str, snake: &str) -> Result<Option<T>, String> { … }
```

Ship this alone — no arms yet, compiles, does nothing. (`Command enum` alternative discussed in §3.5; a `match &str` is simpler and is what jean uses — recommend starting there.)

### Step 1 — decide the delegation direction

Two orderings; both keep IPC working. **Recommend B.**

**A. Handler stays canonical, router calls handler** (jean's shape):
```rust
#[tauri::command]
async fn run_git(cwd: String, args: Vec<String>) -> GitOutput { … real logic … }

// dispatch arm:
"run_git" => {
    let cwd: String = from_field(&args, "cwd")?;
    let git_args: Vec<String> = field(&args, "args", "args")?;
    to_value(crate::run_git(cwd, git_args).await)
}
```
Zero handler edits. Router is a second caller. ← **least risk, do this first.** jean is entirely this shape.

**B. Extract core, both call it** (cleaner long-term for handlers needing `State<T>`):
```rust
// core fn — no Tauri attrs, takes AppHandle, fetches state itself
pub async fn run_git_core(app: &AppHandle, cwd: String, args: Vec<String>) -> Result<GitOutput, String> { … }

#[tauri::command]
async fn run_git(app: AppHandle, cwd: String, args: Vec<String>) -> Result<GitOutput, String> {
    run_git_core(&app, cwd, args).await
}
// dispatch arm calls run_git_core(app, …)
```

For **stateless** commands use A (call the handler). For commands taking `State<T>`, A still works — the router fetches state and passes it:
```rust
"write_pty" => {
    let id: u32 = from_field(&args, "id")?;
    let data: Vec<u8> = from_field(&args, "data")?;
    crate::write_pty(id, data, app.state::<DaemonState>())?;   // State<T> derefs from app
    Ok(Value::Null)
}
```
`app.state::<T>()` returns `State<'_, T>`, which is exactly what the handler param wants. So **A works for every command in 2A** without touching a single handler body. Adopt A globally; reserve B only if a handler’s signature proves awkward.

### Step 2 — migrate 2A commands in batches (each batch = one PR)

Add one `match` arm per command. Suggested batches (independent, low-risk first):
1. FS + git: `read_dir_shallow`, `read_text_file*`, `write_text_file`, `read_file_base64`, `run_git`, `run_gh`, `format_source`, `open_path_in`, `save_temp_image`, `scaffold_burrow_dir`.
2. Workspaces/DB: the 12 DB commands.
3. PTY control + daemon: the `*_pty` + daemon commands.
4. Agent IPC send-side: `claude_*` (send/stop/abort/respond), `acp_*`, `lsp_send`/`lsp_stop`.
5. Config/skills/MCP + Claude reads + misc.

Each arm mirrors the handler param list via `from_field`/`field`. **Verification per batch:** the arm and the handler call the same fn, so a unit test asserting `dispatch_command(app,"run_git",json!({"cwd":".","args":["status"]}))` equals the handler output is enough. jean ships exactly these (`websocket.rs` tests).

### Step 3 — spawn commands (2B): route the call, keep the stream on IPC for now

Add arms for `create_pty`/`lsp_start`/`claude_start`/`acp_start` that spawn and return the id. **Do not** touch their emit paths yet — they keep emitting `*-data-{id}` to the Tauri webview only. The router call works; streaming stays native. (§4 handles the wire.)

### Step 4 — introduce `emit_all` (enables §4, still no web client)

Copy jean's `EmitExt` + a `WsBroadcaster` stub into a new `src-tauri/src/emit.rs`. **Mechanical find-replace** `app.emit(` → `app.emit_all(` across `lib.rs` for the routable event emitters. With no server running, `try_state::<WsBroadcaster>()` is `None` → behaves identically to today. This is a no-op refactor that *prepares* for a transport.

### Step 5 — add a transport (separate PR, optional)

Only now add `server.rs` + `websocket.rs` (axum + `tokio-tungstenite` or `axum::ws`) that forward `{command,args}` → `dispatch_command` and subscribe to `WsBroadcaster` for events. Behind a feature flag / off by default. Headless binary (`src-server`) is the 4-line `main` from jean.

### 3.5 `Command enum` vs `match &str`

The task mentions "a Command enum + dispatch fn". jean uses a bare `match command: &str`. Trade-off:
- **`match &str`** (jean): least code, no enum to keep in sync, args stay `Value` and deserialize per-arm. **Recommended** — 87 arms is fine, and it's the proven shape.
- **`enum Command { RunGit{cwd,args}, … }` + `#[serde(tag="command")]`**: type-safe, self-documenting, but forces one variant + serde derive per command and a second match to execute. More ceremony, no real payoff here. Skip unless we want a typed client SDK later.

// ponytail: start with `match &str` — enum is speculative ceremony; add it only if a typed SDK ever needs it.

---

## 4. Risks — especially event-pushing commands

### R1. Streaming commands are not request/response (the core hazard)

`create_pty`, `claude_start`, `acp_start`, `lsp_start` return an id, then push an **unbounded stream** of `*-data-{id}` events. `dispatch_command` returns `Result<Value>` — a single value. A web/MCP caller invoking `create_pty` over HTTP gets the id but **never sees the output** unless a second channel carries the events.
- **Mitigation:** jean's `EmitExt::emit_all` + `WsBroadcaster` with per-terminal replay buffers (byte + count capped, dropped on `terminal:stopped`). The command call routes fine; the *stream* rides the broadcaster. Until §5 ships a transport, these stay IPC-only — **safe, because the frontend still uses `listen()` exactly as today.**
- **Do not** try to make `create_pty` "return the output" — it's inherently a stream. Keep the call/stream split.

### R2. `take_spawn_requests` — a poll that also emits, and returns UI actions

Genuinely hybrid: emits `workspaces-changed` *and* returns `Vec<SpawnRequest>` the frontend acts on (focus-tab, new-tab, open-workspace). The **read** commands answered inside it (`list-workspaces`, `list-tabs`) are pure and route cleanly. The **UI-action** requests can't become pure request/response — they *are* a message to the renderer.
- **Mitigation:** leave `take_spawn_requests` on the existing IPC poll. If a non-frontend caller ever needs to trigger a UI action (e.g. MCP "focus this tab"), model it as an `emit_all` of a `ui:action` event a connected renderer consumes — never as a router return value. Router can own the read half today; the action half stays frontend-mediated.

### R3. Float-window commands are OS-native, un-routable

The 7 float/git-panel window commands manipulate Tauri webview windows. Headless has no windows. Leave them IPC-only and **omit from the router** (or return `Err("not available headless")`). No mitigation needed — just don't migrate them.

### R4. `State<T>` reachability

Every stateful handler takes `State<DaemonState|DbState|…>`. The router only has `&AppHandle`.
- **Mitigation:** `app.state::<T>()` yields the same `State<'_, T>`. Verified feasible for all of 2A. The only requirement: state must be `app.manage`d before any dispatch — already true (managed at `setup`). No lifetime issues because the handler consumes `State` within the arm's scope.

### R5. Blocking commands starve an async event loop

`run_git`, `run_gh`, `create_worktree`, `restart_daemon` do synchronous process work. In IPC this is fine (Tauri runs commands on a threadpool). Over a single WS loop it would block everything.
- **Mitigation:** port jean's `command_should_run_on_blocking_pool(cmd)` gate → `spawn_blocking` for the git/CLI/daemon commands. Only relevant once §5's transport exists; irrelevant for pure-IPC delegation.

### R6. Arg-casing / shape mismatch

IPC sends Rust param names (snake or Tauri's rename); a future web client may send camelCase. A hand-written arm that reads the wrong field name silently 500s.
- **Mitigation:** use `field(args,camel,snake)` everywhere (jean's fallback). Add a per-batch test that round-trips the real frontend args object through the arm.

### R7. Double source of truth (drift)

With A-style delegation the handler and the arm both name the fn + fields. Renaming a param can desync them.
- **Mitigation:** arms call the *same* fn as the handler (not a copy), so only the field-extraction can drift — caught by R6's tests. Keep handler and arm adjacent or cross-referenced by a comment.

### R8. Auth / exposure

The moment a transport (§5) exists, every routed command is remotely invokable. jean gates `/ws` behind `/api/auth`.
- **Mitigation:** ship §5 behind auth from day one (jean's `auth.rs` token scheme), off by default, loopback-bound. Not a concern for §0–§4 (no network surface).

---

## Appendix — file touch map

| File | Change |
|------|--------|
| `src-tauri/src/dispatch.rs` (new) | `dispatch_command` + helpers |
| `src-tauri/src/emit.rs` (new, §4) | `EmitExt` + `WsBroadcaster` stub |
| `src-tauri/src/lib.rs` | `mod dispatch; mod emit;`; `app.emit(`→`app.emit_all(` (§4); handlers unchanged under scheme A |
| `src-tauri/src/http_server/{server,websocket}.rs` (new, §5) | axum transport, off by default |
| `src-server/src/main.rs` (new, §5) | 4-line headless binary |
| `src/**` frontend | **none** through §4 |

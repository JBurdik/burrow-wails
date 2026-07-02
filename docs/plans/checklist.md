# Jean-borrow implementation checklist

Two structural borrows from competitor `coollabsio/jean` (Apache-2.0). Specs:
`docs/plans/dispatch-router.md`, `docs/plans/mcp-server.md`.

## Tah 1 — Command-dispatch router

- [x] **Phase 0** — `dispatch.rs` scaffold: `dispatch_command(app,command,args)` + helpers (`to_value`/`from_field`/`field`/`field_opt`), `mod dispatch;` in lib.rs. Scheme A (handlers untouched). *(merged: ace405c)*
- [x] **Batch 1** — FS + git: `read_dir_shallow`, `read_text_file(_checked)`, `write_text_file`, `read_file_base64`, `run_git`, `run_gh`, `format_source`, `open_path_in`, `save_temp_image`, `scaffold_burrow_dir`.
- [ ] **Batch 2** — Workspaces/DB (12): `list_workspaces`, `create_workspace`, `delete_workspace`, `rename_workspace`, `touch_workspace`, `create_worktree`, `remove_worktree`, `list_terminal_tabs`, `save_terminal_tabs`, `list_mission_tasks`, `upsert_mission_task`, `delete_mission_task`.
- [ ] **Batch 3** — PTY control + daemon: `write_pty`, `resize_pty`, `kill_pty`, `detach_pty`, `list_pty_sessions`, `get_pty_foreground`, `register_tmux_win`, `is_pid_alive`, `daemon_stats`, `clean_daemon`, `kill_orphan_sessions`, `restart_daemon`, `system_stats`, `set_max_agents`.
- [ ] **Batch 4** — Agent IPC send-side: `claude_*` (send/stop/abort/respond), `acp_*`, `lsp_send`/`lsp_stop`.
- [ ] **Batch 5** — Config/skills/MCP + Claude reads + misc.
- [ ] **§4** — `emit.rs`: `EmitExt` + `WsBroadcaster` stub; mechanical `app.emit(`→`app.emit_all(`. No-op until a transport exists.
- [ ] **§5** — transport: `http_server/{server,websocket}.rs` (axum), off by default, behind auth. + `src-server` 4-line headless binary.
- [ ] **Fallout (free after §4/§5)** — headless server + Docker, WebSocket event replay ring buffers.

**Never migrate (UI/native):** float-window commands (`open_float_window`, `set_window_size`, `snap_float_window`, …), the `SpawnRequest` action half of `take_spawn_requests`.

## Tah 2 — MCP server

- [x] **Phase 0** — `burrow_mcp_{core,socket,stdio}.rs`: in-process Unix-socket server (`0o600` + bearer token), `--burrow-mcp-stdio` exe proxy, depth-capped SPAWNING tools `{spawn,create_worktree,new_tab}`, `send_to_tab`, `burrow_mcp_max_depth` pref (default 3). *(merged: 1724f2f)*
- [x] **Wire startup** — start the socket server at Tauri `setup` (alongside `start_hook_server`); write `burrow-mcp.sock` + `.token` in `BURROW_HOME_DIR` (= `app_data_dir`); `--burrow-mcp-stdio` branch dispatch at the top of `run()` before the Tauri builder.
- [x] **`--mcp-config` injection** — `build_burrow_mcp_config(app,ws_cwd,depth)` appended to newly spawned claude tabs in `take_spawn_requests`' spawn arm via `inject_burrow_mcp_config` (claude-only, non-destructive: Claude merges repeated `--mcp-config`); child `BURROW_MCP_DEPTH = depth+1`, depth defaults 0 (app can't read the spawning PTY env).
- [ ] **Live capture-parity check (§5.5)** — confirm Stop-hook `burrow capture` fires for MCP-spawned tabs so `spawn({wait})` returns a result. Blocker before Phase 1.
- [ ] **Phase 1** — update `BURROW_SKILL_MD` to teach MCP tools first, CLI as fallback.
- [ ] **Phase 2** (optional) — `bin/burrow` spawn/worktree arms read `BURROW_MCP_DEPTH`, refuse over limit.
- [ ] **Settings** — surface `burrow_mcp_max_depth` in the Settings UI.

**§5 decisions (locked):** app-exe stdio proxy · `send_to_tab` any depth-0 · depth default 3 · Unix-socket only (`cfg(windows)` stub) · capture-parity live-check before Phase 1.

## Verify each step
`cd src-tauri && cargo check` must pass. Additive only — frontend untouched until router §5. No commit without review.

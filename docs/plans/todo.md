# TODO — jean-borrow follow-ups

Deferred items from the router + MCP work (see `docs/plans/checklist.md` for the full history). Nothing here is blocking — main is clean, pushed, and runtime-verified.

- [ ] **Headless binary window-skip** — `src-server/` is currently a stub only. `run()` in `src-tauri/src/lib.rs` always builds a Tauri window; a real `BURROW_HEADLESS=1` skip needs to touch window-creation logic, judged too risky to bundle with the transport work. Needs its own careful session.
- [ ] **MCP Phase 2 (optional)** — `bin/burrow`'s `spawn`/`worktree` shell arms don't read `BURROW_MCP_DEPTH` yet, so the CLI fallback path has no depth cap (the MCP path already does). Small, low-risk shell guard — quick win whenever picked up.
- [x] **Mobile web UI over `/ws`** — merged. `src/mobile/` rebuilt against `/ws` (ConnectView/SessionsView/TerminalView, xterm.js live PTY, `tower-http` ServeDir at `/`, unauthed like `/healthz`). `cargo check` + `VITE_TARGET=mobile` build both pass on main. Untested on a real phone yet — see manual test steps below.
  - [ ] **Manual phone test** — enable HTTP toggle in Settings, run `pnpm build:mobile`, restart, `tailscale serve` the port, open from a phone browser, paste token from Settings status panel.
  - [ ] **Stale branch cleanup** — `feat/mobile-remote-web` (113 commits behind, old REST API) is now fully superseded. Safe to delete once confirmed nothing in it is still wanted.
- [ ] **WS event replay ring buffers** — `WsBroadcaster` (§4) is a live broadcast channel only, no replay buffer. A mobile client reconnecting after a network blip misses events in between. Noted as a future enhancement when §4 was built.

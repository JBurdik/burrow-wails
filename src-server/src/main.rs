//! Headless Burrow binary — placeholder (§5 fallout item, deferred).
//!
//! `agentic_ide_lib::run()` always builds a Tauri window (`tauri::Builder::default()...run()`
//! in `src-tauri/src/lib.rs`); it has no `BURROW_HEADLESS` conditional to skip window
//! creation. Wiring that in is out of scope for this pass (risk to the main app's
//! startup path outweighs the benefit here) — tracked as an open item in
//! `docs/plans/checklist.md`.
//!
//! For now: run the full desktop app with `http_enabled=1` (see
//! `docs/tailscale-remote-access.md`) and reach it over the HTTP/WS transport
//! from `src-tauri/src/http_server/`. A true no-window headless binary is a
//! follow-up.

fn main() {
    eprintln!(
        "burrow-server: headless (no-window) mode is not implemented yet.\n\
         Run the full Burrow.app with the `http_enabled` pref set to \"1\" instead —\n\
         see docs/tailscale-remote-access.md. This binary is a placeholder for a\n\
         future true headless mode (tracked in docs/plans/checklist.md)."
    );
    std::process::exit(1);
}

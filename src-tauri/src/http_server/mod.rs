//! HTTP/WebSocket transport for the dispatch router (§5).
//!
//! Off by default: only bound if `<BURROW_HOME_DIR>/http_enabled` contains
//! "1". Always binds `127.0.0.1` — never `0.0.0.0`. Remote (e.g. Tailscale)
//! access is expected to go through `tailscale serve`, which proxies a local
//! port to the tailnet without ever exposing the loopback bind itself.

pub mod auth;
pub mod server;
pub mod websocket;

use tauri::AppHandle;

const DEFAULT_PORT: u16 = 8420;

/// Pref file gating whether the server starts at all (mirrors the
/// `burrow_mcp_max_depth` pref-file pattern in `lib.rs`).
fn enabled_flag_path(app: &AppHandle) -> Option<std::path::PathBuf> {
    use tauri::Manager;
    Some(app.path().app_data_dir().ok()?.join("http_enabled"))
}

pub fn is_enabled(app: &AppHandle) -> bool {
    enabled_flag_path(app)
        .and_then(|p| std::fs::read_to_string(p).ok())
        .map(|s| s.trim() == "1")
        .unwrap_or(false)
}

pub fn set_enabled(app: &AppHandle, enabled: bool) {
    if let Some(path) = enabled_flag_path(app) {
        if let Some(parent) = path.parent() {
            let _ = std::fs::create_dir_all(parent);
        }
        let _ = std::fs::write(path, if enabled { "1" } else { "0" });
    }
}

pub fn port(app: &AppHandle) -> u16 {
    use tauri::Manager;
    app.path()
        .app_data_dir()
        .ok()
        .and_then(|d| std::fs::read_to_string(d.join("http_port")).ok())
        .and_then(|s| s.trim().parse::<u16>().ok())
        .unwrap_or(DEFAULT_PORT)
}

/// Tauri command: read-only status for Settings UI / smoke tests.
#[tauri::command]
pub fn get_http_server_status(app: AppHandle) -> serde_json::Value {
    use tauri::Manager;
    let token_path = app
        .path()
        .app_data_dir()
        .ok()
        .map(|d| d.join(auth::TOKEN_FILE).to_string_lossy().to_string())
        .unwrap_or_default();
    serde_json::json!({
        "enabled": is_enabled(&app),
        "port": port(&app),
        "tokenPath": token_path,
    })
}

/// Tauri command: writes the `http_enabled` pref file. Starting/stopping the
/// server live is out of scope for v1 — this takes effect on next app
/// restart (see `server::maybe_start` called from Tauri `setup`). TODO: live
/// start/stop without a restart.
#[tauri::command]
pub fn set_http_enabled(enabled: bool, app: AppHandle) {
    set_enabled(&app, enabled);
}

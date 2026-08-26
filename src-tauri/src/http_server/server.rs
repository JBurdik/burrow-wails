//! axum HTTP/WS server. Loopback-only, off unless the `http_enabled` pref
//! file says "1". Tailscale/remote access is expected via `tailscale serve`
//! proxying this loopback port onto the tailnet — we deliberately never bind
//! `0.0.0.0` ourselves; that decision belongs to the OS-level Tailscale
//! tooling, not this process.

use std::collections::HashMap;
use std::net::{IpAddr, Ipv4Addr, SocketAddr};
use std::sync::Arc;

use axum::{
    extract::{ws::WebSocketUpgrade, Query, State},
    http::{HeaderMap, StatusCode},
    response::IntoResponse,
    routing::get,
    Router,
};
use tauri::{AppHandle, Manager};
use tower_http::services::{ServeDir, ServeFile};

use super::{auth, websocket};
use crate::emit::WsBroadcaster;

#[derive(Clone)]
pub struct ServerState {
    pub app: AppHandle,
    pub token: Arc<String>,
    pub broadcaster: WsBroadcaster,
}

/// Called once from Tauri `setup`. No-op unless `http_enabled` pref is "1".
pub fn maybe_start(app: &AppHandle) {
    if !super::is_enabled(app) {
        return;
    }
    let Some(token) = auth::ensure_token(app) else {
        log::warn!("HTTP transport: could not create bearer token, not starting");
        return;
    };
    let port = super::port(app);

    let broadcaster = WsBroadcaster::new();
    app.manage(broadcaster.clone());

    let state = ServerState {
        app: app.clone(),
        token: Arc::new(token),
        broadcaster,
    };

    let app_handle = app.clone();
    tauri::async_runtime::spawn(async move {
        if let Err(e) = serve(state, port).await {
            log::warn!("HTTP transport failed to start: {e}");
        }
        let _ = app_handle; // keep AppHandle alive for the server's lifetime
    });
}

async fn serve(state: ServerState, port: u16) -> Result<(), String> {
    let router = build_router(state);
    // Loopback ONLY. Never 0.0.0.0 - remote access goes through `tailscale serve`.
    let addr = SocketAddr::new(IpAddr::V4(Ipv4Addr::LOCALHOST), port);
    let listener = tokio::net::TcpListener::bind(addr)
        .await
        .map_err(|e| format!("bind {addr}: {e}"))?;
    log::info!("HTTP transport listening on http://{addr}");
    axum::serve(listener, router)
        .await
        .map_err(|e| format!("serve: {e}"))
}

fn build_router(state: ServerState) -> Router {
    let router = Router::new()
        .route("/healthz", get(healthz))
        .route("/ws", get(ws_upgrade))
        .with_state(state.clone());

    // Serve the mobile web UI at `/`, unauthenticated (matches `/healthz` — a plain
    // browser GET can't attach an Authorization header; only `/ws` needs the token).
    // Only mounted when `dist-mobile/` actually exists on disk, so an app that never
    // ran `pnpm build:mobile` still serves `/ws` + `/healthz` fine.
    match dist_mobile_dir(&state.app) {
        Some(dir) if dir.join("mobile.html").is_file() => {
            log::info!("HTTP transport: serving mobile UI from {}", dir.display());
            let index = dir.join("mobile.html");
            let serve_dir = ServeDir::new(&dir).fallback(ServeFile::new(index));
            // Tailscale Serve exposes this same UI at `/burrow/` so it can
            // coexist with another app at `/`. Mount it locally too: this
            // makes asset requests work whether Tailscale keeps or strips the
            // configured path before proxying to us.
            router
                .nest_service("/burrow", serve_dir.clone())
                .fallback_service(serve_dir)
        }
        _ => {
            log::warn!("HTTP transport: dist-mobile/ not found, mobile UI not served (run `pnpm build:mobile`)");
            router
        }
    }
}

/// Resolve the built mobile bundle directory. Supports both `pnpm tauri:dev`
/// (repo root, two levels up from `src-tauri`) and a bundled app (Tauri
/// resource dir, mirroring `find_daemon_binary`'s resource-dir fallback).
fn dist_mobile_dir(app: &AppHandle) -> Option<std::path::PathBuf> {
    if let Ok(exe) = std::env::current_exe() {
        // src-tauri/target/debug/agentic-ide -> repo root is 3 parents up.
        if let Some(dev_root) = exe.parent().and_then(|p| p.parent()).and_then(|p| p.parent()).and_then(|p| p.parent()) {
            let candidate = dev_root.join("dist-mobile");
            if candidate.join("mobile.html").is_file() {
                return Some(candidate);
            }
        }
    }
    if let Ok(res) = app.path().resource_dir() {
        let candidate = res.join("dist-mobile");
        if candidate.join("mobile.html").is_file() {
            return Some(candidate);
        }
    }
    None
}

async fn healthz() -> impl IntoResponse {
    (StatusCode::OK, "ok")
}

async fn ws_upgrade(
    ws: WebSocketUpgrade,
    State(state): State<ServerState>,
    headers: HeaderMap,
    Query(params): Query<HashMap<String, String>>,
) -> impl IntoResponse {
    let authorized = headers
        .get("authorization")
        .and_then(|v| v.to_str().ok())
        .map(|v| auth::validate_credential(v, &state.token))
        .unwrap_or(false)
        || params
            .get("token")
            .map(|t| auth::validate_credential(t, &state.token))
            .unwrap_or(false);

    if !authorized {
        return (StatusCode::UNAUTHORIZED, "unauthorized").into_response();
    }

    ws.on_upgrade(move |socket| websocket::handle_socket(socket, state))
}

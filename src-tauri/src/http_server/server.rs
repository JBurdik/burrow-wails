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
    Router::new()
        .route("/healthz", get(healthz))
        .route("/ws", get(ws_upgrade))
        .with_state(state)
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
        .map(|v| auth::validate_token(v, &state.token))
        .unwrap_or(false)
        || params
            .get("token")
            .map(|t| auth::validate_token(t, &state.token))
            .unwrap_or(false);

    if !authorized {
        return (StatusCode::UNAUTHORIZED, "unauthorized").into_response();
    }

    ws.on_upgrade(move |socket| websocket::handle_socket(socket, state))
}

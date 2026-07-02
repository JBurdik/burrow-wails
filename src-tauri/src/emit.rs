//! Dual-sink event emission: Tauri webview + (optionally) a WebSocket broadcaster.
//!
//! `EmitExt::emit_all` is a drop-in replacement for `app.emit(...)` that also
//! fans an event out to any connected `/ws` clients (via `WsBroadcaster`, §5).
//! With no `WsBroadcaster` registered as managed state (the default until the
//! HTTP transport is enabled and started), `emit_all` behaves exactly like
//! today's `app.emit` — the broadcast side is a pure no-op.

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager};
use tokio::sync::broadcast;

/// Capacity of the broadcast channel. No replay buffer — a client that
/// connects after an event fires simply misses it (ring-buffer replay is a
/// stated future enhancement, not implemented here).
const CHANNEL_CAPACITY: usize = 256;

/// Live event broadcaster for connected WebSocket clients.
///
/// Held as Tauri managed state. Registered only when the HTTP transport
/// actually starts (see `http_server::server`); until then `app.try_state`
/// returns `None` and `emit_all` is a pure passthrough to `app.emit`.
#[derive(Clone)]
pub struct WsBroadcaster {
    sender: broadcast::Sender<(String, String)>,
}

impl WsBroadcaster {
    pub fn new() -> Self {
        let (sender, _rx) = broadcast::channel(CHANNEL_CAPACITY);
        Self { sender }
    }

    /// Subscribe a new WebSocket connection to the event stream.
    pub fn subscribe(&self) -> broadcast::Receiver<(String, String)> {
        self.sender.subscribe()
    }

    fn broadcast(&self, event: &str, payload_json: String) {
        // No receivers connected is not an error - just means no one is listening yet.
        let _ = self.sender.send((event.to_string(), payload_json));
    }
}

impl Default for WsBroadcaster {
    fn default() -> Self {
        Self::new()
    }
}

pub trait EmitExt {
    /// Emit `event` with `payload` to the Tauri webview (identical to
    /// `self.emit(event, payload)`) AND, if a `WsBroadcaster` is registered
    /// as managed state, broadcast it to connected WebSocket clients as
    /// `{event, payload}` JSON.
    fn emit_all<S: Serialize + Clone>(&self, event: &str, payload: S) -> tauri::Result<()>;
}

impl EmitExt for AppHandle {
    fn emit_all<S: Serialize + Clone>(&self, event: &str, payload: S) -> tauri::Result<()> {
        if let Some(broadcaster) = self.try_state::<WsBroadcaster>() {
            if let Ok(payload_json) = serde_json::to_string(&payload) {
                broadcaster.broadcast(event, payload_json);
            }
        }
        self.emit(event, payload)
    }
}

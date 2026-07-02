//! Per-connection WebSocket loop: JSON-RPC-ish `{id, command, args}` request/
//! response over `dispatch_command`, plus a simple event-subscribe channel
//! fed by `WsBroadcaster` (see `emit.rs`).

use std::collections::HashSet;

use axum::extract::ws::{Message, WebSocket};
use futures_util::{SinkExt, StreamExt};
use serde::Deserialize;
use serde_json::{json, Value};

use super::server::ServerState;

#[derive(Deserialize)]
#[serde(untagged)]
enum ClientMessage {
    Subscribe { subscribe: String },
    Call {
        id: Value,
        command: String,
        #[serde(default)]
        args: Value,
    },
}

pub async fn handle_socket(socket: WebSocket, state: ServerState) {
    let (mut sender, mut receiver) = socket.split();
    let mut events = state.broadcaster.subscribe();
    let mut subscribed: HashSet<String> = HashSet::new();

    loop {
        tokio::select! {
            incoming = receiver.next() => {
                let Some(Ok(msg)) = incoming else { break };
                let Message::Text(text) = msg else { continue };
                match serde_json::from_str::<ClientMessage>(&text) {
                    Ok(ClientMessage::Subscribe { subscribe }) => {
                        subscribed.insert(subscribe);
                    }
                    Ok(ClientMessage::Call { id, command, args }) => {
                        let result = crate::dispatch::dispatch_command(&state.app, &command, args).await;
                        let reply = match result {
                            Ok(value) => json!({ "id": id, "result": value }),
                            Err(error) => json!({ "id": id, "error": error }),
                        };
                        if sender.send(Message::Text(reply.to_string())).await.is_err() {
                            break;
                        }
                    }
                    Err(_) => {
                        let reply = json!({ "error": "invalid_message" });
                        if sender.send(Message::Text(reply.to_string())).await.is_err() {
                            break;
                        }
                    }
                }
            }
            event = events.recv() => {
                match event {
                    Ok((name, payload_json)) => {
                        if subscribed.contains(&name) {
                            let payload: Value = serde_json::from_str(&payload_json).unwrap_or(Value::Null);
                            let frame = json!({ "event": name, "payload": payload });
                            if sender.send(Message::Text(frame.to_string())).await.is_err() {
                                break;
                            }
                        }
                    }
                    Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => continue,
                    Err(tokio::sync::broadcast::error::RecvError::Closed) => break,
                }
            }
        }
    }
}

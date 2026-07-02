/// Synchronous client for the burrow-daemon Unix socket.
/// Used by Tauri commands in lib.rs — no tokio, just std threads + UnixStream.

use base64::{engine::general_purpose, Engine as _};
use serde_json::{json, Value};
use std::io::{BufRead, BufReader, BufWriter, Write};
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use crate::emit::EmitExt;
use tauri::AppHandle;

pub struct DaemonClient {
    socket_path: PathBuf,
    token: String,
    next_id: AtomicU64,
    pub app: AppHandle,
    // stop signals per streaming PTY: pty_id → Arc<AtomicBool>
    streams: Mutex<std::collections::HashMap<u32, Arc<AtomicBool>>>,
}

impl DaemonClient {
    pub fn new(socket_path: PathBuf, token: String, app: AppHandle) -> Self {
        Self {
            socket_path,
            token,
            next_id: AtomicU64::new(1),
            app,
            streams: Mutex::new(std::collections::HashMap::new()),
        }
    }

    fn next_id(&self) -> u64 {
        self.next_id.fetch_add(1, Ordering::Relaxed)
    }

    fn open(&self) -> Result<(BufReader<UnixStream>, BufWriter<UnixStream>), String> {
        let stream = UnixStream::connect(&self.socket_path)
            .map_err(|e| format!("daemon connect: {e}"))?;
        let r = BufReader::new(stream.try_clone().map_err(|e| e.to_string())?);
        let w = BufWriter::new(stream);
        Ok((r, w))
    }

    fn auth(
        &self,
        r: &mut BufReader<UnixStream>,
        w: &mut BufWriter<UnixStream>,
    ) -> Result<(), String> {
        let id = self.next_id();
        let msg = format!("{}\n", json!({"id": id, "cmd": "Auth", "token": self.token}));
        w.write_all(msg.as_bytes()).map_err(|e| e.to_string())?;
        w.flush().map_err(|e| e.to_string())?;
        let mut line = String::new();
        r.read_line(&mut line).map_err(|e| e.to_string())?;
        let resp: Value = serde_json::from_str(line.trim()).map_err(|e| e.to_string())?;
        if resp["ok"].as_bool() != Some(true) {
            return Err(resp["error"].as_str().unwrap_or("auth failed").to_string());
        }
        Ok(())
    }

    /// Send a single command and return the response. Opens/closes a connection per call.
    pub fn cmd(&self, mut payload: Value) -> Result<Value, String> {
        let id = self.next_id();
        payload["id"] = json!(id);

        let (mut r, mut w) = self.open()?;
        self.auth(&mut r, &mut w)?;

        let msg = format!("{payload}\n");
        w.write_all(msg.as_bytes()).map_err(|e| e.to_string())?;
        w.flush().map_err(|e| e.to_string())?;

        let mut line = String::new();
        r.read_line(&mut line).map_err(|e| e.to_string())?;
        serde_json::from_str(line.trim()).map_err(|e| e.to_string())
    }

    /// Open a persistent AttachPty stream, forwarding data as `pty-data-{id}`.
    pub fn start_stream(&self, pty_id: u32) {
        let stop = Arc::new(AtomicBool::new(false));
        self.streams.lock().unwrap().insert(pty_id, stop.clone());
        self.spawn_stream(pty_id, format!("pty-data-{pty_id}"), stop);
    }

    fn spawn_stream(&self, pty_id: u32, event_name: String, stop: Arc<AtomicBool>) {
        let socket_path = self.socket_path.clone();
        let token = self.token.clone();
        let app = self.app.clone();

        std::thread::spawn(move || {
            let stream = match UnixStream::connect(&socket_path) {
                Ok(s) => s,
                Err(e) => { eprintln!("[daemon_client] stream connect: {e}"); return; }
            };

            let mut r = BufReader::new(stream.try_clone().unwrap());
            let mut w = BufWriter::new(stream);

            // Auth
            let auth = format!("{}\n", json!({"id": 0, "cmd": "Auth", "token": token}));
            if w.write_all(auth.as_bytes()).is_err() { return; }
            if w.flush().is_err() { return; }
            let mut line = String::new();
            let _ = r.read_line(&mut line);

            // AttachPty
            line.clear();
            let attach = format!("{}\n", json!({"id": 1, "cmd": "AttachPty", "pty_id": pty_id}));
            if w.write_all(attach.as_bytes()).is_err() { return; }
            if w.flush().is_err() { return; }
            let _ = r.read_line(&mut line);
            let resp: Value = serde_json::from_str(line.trim()).unwrap_or(json!({"ok":false}));
            if resp["ok"].as_bool() != Some(true) {
                eprintln!("[daemon_client] AttachPty failed for pty {pty_id}");
                return;
            }

            // Stream events
            loop {
                if stop.load(Ordering::Relaxed) { break; }
                line.clear();
                match r.read_line(&mut line) {
                    Ok(0) | Err(_) => break,
                    Ok(_) => {}
                }
                let event: Value = match serde_json::from_str(line.trim()) {
                    Ok(v) => v,
                    Err(_) => continue,
                };
                match event["type"].as_str() {
                    Some("PtyData") => {
                        if let Some(enc) = event["data"].as_str() {
                            if let Ok(bytes) = general_purpose::STANDARD.decode(enc) {
                                let _ = app.emit_all(&event_name, bytes);
                            }
                        }
                    }
                    Some("PtyExit") => break,
                    _ => {}
                }
            }
        });
    }

    /// Signal the stream thread to exit (does not kill the daemon-side PTY).
    pub fn stop_stream(&self, pty_id: u32) {
        if let Some(stop) = self.streams.lock().unwrap().remove(&pty_id) {
            stop.store(true, Ordering::Relaxed);
        }
    }

    /// Verify connection works (used by daemon_ensure probe).
    pub fn probe(&self) -> bool {
        self.cmd(json!({"cmd": "ListSessions"}))
            .map(|r| r["ok"].as_bool() == Some(true))
            .unwrap_or(false)
    }

    /// The running daemon's build version, or None if it predates the Version
    /// command (an old daemon replies `unknown command`).
    pub fn version(&self) -> Option<String> {
        self.cmd(json!({"cmd": "Version"}))
            .ok()
            .and_then(|r| r.get("version").and_then(|v| v.as_str()).map(String::from))
    }
}

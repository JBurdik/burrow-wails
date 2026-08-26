//! Tailscale `serve` toggle — proxies the loopback HTTP/WS transport onto
//! the tailnet (never `funnel`, never public internet).

use serde::Serialize;
use std::process::Command;

/// Keep Burrow separate from any application already served from the device's
/// Tailscale root URL (for example t3code). Tailscale Serve combines handlers
/// by path, so this is safe to add and remove independently.
const BURROW_SERVE_PATH: &str = "/burrow";

#[derive(Debug, Serialize)]
pub struct TailscaleStatus {
    pub installed: bool,
    pub logged_in: bool,
    pub dns_name: Option<String>,
    pub serving: bool,
    pub serve_url: Option<String>,
}

pub fn tailscale_available() -> bool {
    Command::new("tailscale")
        .arg("version")
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}

fn dns_name() -> Option<(bool, Option<String>)> {
    let out = Command::new("tailscale")
        .args(["status", "--json"])
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let v: serde_json::Value = serde_json::from_slice(&out.stdout).ok()?;
    let logged_in = v.get("BackendState").and_then(|s| s.as_str()) == Some("Running");
    let dns = v
        .get("Self")
        .and_then(|s| s.get("DNSName"))
        .and_then(|s| s.as_str())
        .map(|s| s.trim_end_matches('.').to_string());
    Some((logged_in, dns))
}

fn serving_url() -> Option<String> {
    let out = match Command::new("tailscale")
        .args(["serve", "status", "--json"])
        .output()
    {
        Ok(o) if o.status.success() => o,
        _ => return None,
    };
    let v: serde_json::Value = match serde_json::from_slice(&out.stdout) {
        Ok(v) => v,
        Err(_) => return None,
    };
    v.get("Web")
        .and_then(|w| w.as_object())
        .and_then(|servers| {
            servers.iter().find_map(|(host, server)| {
                let has_burrow_handler = server
                    .get("Handlers")
                    .and_then(|handlers| handlers.as_object())
                    .is_some_and(|handlers| handlers.contains_key(BURROW_SERVE_PATH));
                has_burrow_handler.then(|| format!("https://{host}{BURROW_SERVE_PATH}/"))
            })
        })
}

pub fn tailscale_status() -> TailscaleStatus {
    let installed = tailscale_available();
    if !installed {
        return TailscaleStatus {
            installed: false,
            logged_in: false,
            dns_name: None,
            serving: false,
            serve_url: None,
        };
    }
    let (logged_in, dns_name) = dns_name().unwrap_or((false, None));
    let serve_url = logged_in.then(serving_url).flatten();
    let serving = serve_url.is_some();
    TailscaleStatus {
        installed,
        logged_in,
        dns_name,
        serving,
        serve_url,
    }
}

pub fn enable_serve(port: u16) -> Result<(), String> {
    let out = Command::new("tailscale")
        .args([
            "serve",
            "--bg",
            "--https=443",
            "--set-path=/burrow",
            &port.to_string(),
        ])
        .output()
        .map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(String::from_utf8_lossy(&out.stderr).trim().to_string());
    }
    Ok(())
}

pub fn disable_serve() -> Result<(), String> {
    let out = Command::new("tailscale")
        .args(["serve", "--https=443", "--set-path=/burrow", "off"])
        .output()
        .map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(String::from_utf8_lossy(&out.stderr).trim().to_string());
    }
    Ok(())
}

#[tauri::command]
pub fn get_tailscale_status() -> TailscaleStatus {
    tailscale_status()
}

#[tauri::command]
pub fn set_tailscale_serve(
    enabled: bool,
    port: u16,
    app: tauri::AppHandle,
) -> Result<TailscaleStatus, String> {
    if enabled && !super::is_enabled(&app) {
        return Err("Enable the HTTP/WS server first".to_string());
    }
    if enabled {
        enable_serve(port)?;
    } else {
        disable_serve()?;
    }
    Ok(tailscale_status())
}

//! Tailscale `serve` toggle — proxies the loopback HTTP/WS transport onto
//! the tailnet (never `funnel`, never public internet).

use serde::Serialize;
use std::process::Command;

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

fn serving_state() -> bool {
    let out = match Command::new("tailscale")
        .args(["serve", "status", "--json"])
        .output()
    {
        Ok(o) if o.status.success() => o,
        _ => return false,
    };
    let v: serde_json::Value = match serde_json::from_slice(&out.stdout) {
        Ok(v) => v,
        Err(_) => return false,
    };
    v.get("Web")
        .and_then(|w| w.as_object())
        .map(|m| !m.is_empty())
        .unwrap_or(false)
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
    let serving = logged_in && serving_state();
    let serve_url = if serving {
        dns_name.as_ref().map(|d| format!("https://{d}/"))
    } else {
        None
    };
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
        .args(["serve", "--bg", &port.to_string()])
        .output()
        .map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(String::from_utf8_lossy(&out.stderr).trim().to_string());
    }
    Ok(())
}

pub fn disable_serve() -> Result<(), String> {
    let out = Command::new("tailscale")
        .args(["serve", "--https=443", "off"])
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

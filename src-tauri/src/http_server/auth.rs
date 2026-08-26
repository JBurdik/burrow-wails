//! Bearer-token auth for the HTTP/WS transport.
//!
//! Mirrors `burrow_mcp_socket`'s token pattern: a token generated once at
//! startup, written to `<BURROW_HOME_DIR>/http.token` chmod 0600. The file
//! perms are the real access control (only this user can read it); the
//! token itself just stops an unauthenticated network peer from reaching
//! `/ws`.

use tauri::{AppHandle, Manager};

pub const TOKEN_FILE: &str = "http.token";

pub fn token_path(app: &AppHandle) -> Option<std::path::PathBuf> {
    Some(app.path().app_data_dir().ok()?.join(TOKEN_FILE))
}

fn gen_token() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    let pid = std::process::id();
    let stack = &nanos as *const _ as usize;
    format!("{nanos:x}{pid:x}{stack:x}")
}

/// Generate (or load, if one already exists) the bearer token and ensure the
/// file is chmod 0600.
pub fn ensure_token(app: &AppHandle) -> Option<String> {
    let path = token_path(app)?;
    if let Some(parent) = path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    if let Ok(existing) = std::fs::read_to_string(&path) {
        let existing = existing.trim();
        if !existing.is_empty() {
            lock_down(&path);
            return Some(existing.to_string());
        }
    }
    let token = gen_token();
    let _ = std::fs::write(&path, &token);
    lock_down(&path);
    Some(token)
}

#[cfg(unix)]
fn lock_down(path: &std::path::Path) {
    use std::os::unix::fs::PermissionsExt;
    let _ = std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600));
}

#[cfg(not(unix))]
fn lock_down(_path: &std::path::Path) {}

/// Validate a bearer token supplied either as an `Authorization: Bearer <t>`
/// header value or a raw `?token=<t>` query param.
pub fn validate_token(candidate: &str, expected: &str) -> bool {
    let candidate = candidate.strip_prefix("Bearer ").unwrap_or(candidate);
    constant_time_eq(candidate.as_bytes(), expected.as_bytes())
}

/// A short pairing code for a phone already inside the user's tailnet. The
/// original bearer token remains supported for automation and direct clients.
/// This intentionally is convenience authentication, not a replacement for a
/// publicly exposed password — the HTTP server itself remains loopback-only.
pub fn pairing_code(token: &str) -> String {
    let mut hash: u32 = 2_166_136_261;
    for byte in token.bytes() {
        hash ^= u32::from(byte);
        hash = hash.wrapping_mul(16_777_619);
    }
    format!("{:06}", hash % 1_000_000)
}

pub fn validate_credential(candidate: &str, expected: &str) -> bool {
    validate_token(candidate, expected)
        || constant_time_eq(candidate.trim().as_bytes(), pairing_code(expected).as_bytes())
}

fn constant_time_eq(left: &[u8], right: &[u8]) -> bool {
    let mut difference = left.len() ^ right.len();
    for index in 0..left.len().max(right.len()) {
        difference |= usize::from(
            left.get(index).copied().unwrap_or(0) ^ right.get(index).copied().unwrap_or(0),
        );
    }
    difference == 0
}

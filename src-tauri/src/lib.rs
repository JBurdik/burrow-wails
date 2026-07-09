mod burrow_mcp_core;
mod burrow_mcp_socket;
mod burrow_mcp_stdio;
mod daemon_client;
mod dispatch;
pub mod emit;
mod http_server;

use base64::{engine::general_purpose, Engine};
use daemon_client::DaemonClient;
use emit::EmitExt;
use rusqlite::Connection;
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, OnceLock};
use tauri::{AppHandle, Emitter, Manager, State};

#[cfg(target_os = "macos")]
mod sleep_inhibit {
    use std::sync::Mutex;
    use std::ffi::c_void;
    use libc::{c_char, c_uint};

    // IOKit types (not exposed by any crate we already have — declare manually)
    type IOPMAssertionID = u32;
    type IOReturn = c_uint;
    const kIOReturnSuccess: IOReturn = 0;

    // CoreFoundation: IOPMAssertionCreateWithName takes CFStringRef for the
    // type/name args, NOT `const char *`. Passing raw C strings made IOKit
    // CFRetain the char-pointer bytes as if they were CF objects and insert
    // them into its internal NSMutableDictionary -> objc_retain on a non-object
    // -> EXC_BAD_ACCESS (crash addr was the bytes of "PreventUserIdle..." read
    // as a pointer). Build real CFStrings and pass those instead.
    type CFStringRef = *const c_void;
    type CFAllocatorRef = *const c_void;
    const kCFStringEncodingUTF8: u32 = 0x0800_0100;

    #[link(name = "CoreFoundation", kind = "framework")]
    extern "C" {
        fn CFStringCreateWithCString(
            alloc: CFAllocatorRef,
            c_str: *const c_char,
            encoding: u32,
        ) -> CFStringRef;
        fn CFRelease(cf: *const c_void);
    }

    #[link(name = "IOKit", kind = "framework")]
    extern "C" {
        fn IOPMAssertionCreateWithName(
            assertion_type: CFStringRef,
            assertion_level: u32,
            assertion_name: CFStringRef,
            assertion_id: *mut IOPMAssertionID,
        ) -> IOReturn;
        fn IOPMAssertionRelease(assertion_id: IOPMAssertionID) -> IOReturn;
    }

    // kIOPMAssertionLevelOn = 255
    const kIOPMAssertionLevelOn: u32 = 255;

    static ASSERTION_ID: Mutex<Option<IOPMAssertionID>> = Mutex::new(None);

    pub fn set(active: bool) -> Result<(), String> {
        let mut guard = ASSERTION_ID.lock().unwrap();
        if active {
            if guard.is_some() { return Ok(()); } // already held
            let type_c = c"PreventUserIdleSystemSleep";
            let name_c = c"Burrow agent running";
            let mut id: IOPMAssertionID = 0;
            // CFStrings own a copy of the bytes; release them after the call.
            let ret = unsafe {
                let type_cf =
                    CFStringCreateWithCString(std::ptr::null(), type_c.as_ptr(), kCFStringEncodingUTF8);
                let name_cf =
                    CFStringCreateWithCString(std::ptr::null(), name_c.as_ptr(), kCFStringEncodingUTF8);
                if type_cf.is_null() || name_cf.is_null() {
                    if !type_cf.is_null() { CFRelease(type_cf); }
                    if !name_cf.is_null() { CFRelease(name_cf); }
                    return Err("CFStringCreateWithCString failed".into());
                }
                let r = IOPMAssertionCreateWithName(type_cf, kIOPMAssertionLevelOn, name_cf, &mut id);
                CFRelease(type_cf);
                CFRelease(name_cf);
                r
            };
            if ret == kIOReturnSuccess {
                *guard = Some(id);
                Ok(())
            } else {
                Err(format!("IOPMAssertionCreateWithName failed: {ret}"))
            }
        } else {
            if let Some(id) = guard.take() {
                unsafe { IOPMAssertionRelease(id) };
            }
            Ok(())
        }
    }

    #[cfg(test)]
    mod tests {
        // Exercises the real IOKit/CoreFoundation FFI. Before the CFStringRef
        // fix, set(true) passed raw char* where IOKit expected CF objects and
        // crashed with EXC_BAD_ACCESS — so this test segfaults on a regression
        // of the type signature rather than just asserting.
        #[test]
        fn set_round_trips_without_crashing() {
            super::set(true).expect("acquire assertion");
            super::set(true).expect("idempotent acquire");
            super::set(false).expect("release assertion");
            super::set(false).expect("release when none held");
        }
    }
}

#[tauri::command]
fn set_sleep_inhibit(active: bool) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    { sleep_inhibit::set(active) }
    #[cfg(not(target_os = "macos"))]
    { let _ = active; Ok(()) }
}

struct DaemonState {
    client: Mutex<Option<Arc<DaemonClient>>>,
}

impl DaemonState {
    /// Clone the Arc<DaemonClient> and release the mutex immediately. The client
    /// is itself thread-safe (each cmd opens its own connection), so holding the
    /// outer mutex across the network round-trip only serialized unrelated calls
    /// against each other — e.g. a 2s `get_pty_foreground` poll (which shells out
    /// to `ps` daemon-side) would block a fresh `create_pty`, making new terminals
    /// appear to hang on init. Cloning the Arc lets concurrent commands proceed.
    fn client(&self) -> Option<Arc<DaemonClient>> {
        self.client.lock().unwrap().clone()
    }
}

struct DbState {
    conn: Mutex<Connection>,
}

#[derive(Debug, Serialize, Clone)]
struct FloatParams {
    pty_id: u32,
    ws_id: i64,
    title: String,
}

struct FloatParamsState {
    map: Mutex<std::collections::HashMap<String, FloatParams>>,
}

/// token -> spawning workspace path, so /agent-done can scope its broadcast to
/// the Manager whose repo actually spawned that sub-agent.
#[derive(Default)]
struct SpawnTokenOrigin {
    map: Mutex<std::collections::HashMap<String, String>>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Corner {
    TopLeft,
    TopRight,
    BottomLeft,
    BottomRight,
}

impl Corner {
    fn from_str(s: &str) -> Corner {
        match s {
            "top-left" => Corner::TopLeft,
            "bottom-left" => Corner::BottomLeft,
            "bottom-right" => Corner::BottomRight,
            _ => Corner::TopRight,
        }
    }
}

/// One float window's layout slot. Sizes are LOGICAL px and tracked here (not
/// queried from the window) so re-stacking is deterministic even right after a
/// window is created, before its size is realized — that race was placing new
/// bubbles off-screen.
#[derive(Clone)]
struct FloatWin {
    label: String,
    w: f64,
    h: f64,
}

/// Float windows all snap to ONE user-chosen corner (a Setting, not where they're
/// dropped) and stack vertically there in insertion order. Dragging just returns
/// a window to that corner.
struct FloatLayoutState {
    corner: Mutex<Corner>,
    wins: Mutex<Vec<FloatWin>>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct Workspace {
    pub id: i64,
    pub name: String,
    pub path: String,
    pub created_at: i64,
    pub last_opened: Option<i64>,
    pub parent_id: Option<i64>,
    pub worktree_branch: Option<String>,
    /// Whether this workspace's directory is a git repo. Non-git folders are
    /// first-class workspaces but hide all git UI (branch, worktrees, diff panel).
    pub is_git: bool,
    pub icon: Option<String>,
    pub sort_order: i64,
}

/// Detect whether `path` lives inside a git work tree. Uses `git rev-parse` so it
/// correctly handles plain repos, submodules, and worktrees (where `.git` is a file).
fn is_git_repo(path: &str) -> bool {
    git_in(path, &["rev-parse", "--is-inside-work-tree"])
        .map(|out| out.trim() == "true")
        .unwrap_or(false)
}

// ── burrow CLI ────────────────────────────────────────────────────────────────

const BURROW_SCRIPT: &str = include_str!("../bin/burrow");
const TMUX_SHIM: &str = include_str!("../bin/tmux");

// Must stay identical to DAEMON_PROTO_VERSION in daemon_main.rs. Bumped only when
// daemon-side PTY behavior changes, so app-only updates don't needlessly restart
// the daemon (which would kill live PTY sessions). A mismatch on launch retires the
// stale daemon so the new behavior takes effect after an auto-update.
const DAEMON_PROTO_VERSION: &str = "3";

// Cached bin dir: written once per app session. Subsequent create_pty calls skip
// the file writes and chmod (2 writes + 2 fsyncs per tab was measurably slow).
static BURROW_BIN_DIR: OnceLock<PathBuf> = OnceLock::new();

fn ensure_burrow_bin(app: &AppHandle) -> Option<&'static PathBuf> {
    if let Some(dir) = BURROW_BIN_DIR.get() {
        return Some(dir);
    }

    let dir = app.path().app_data_dir().ok()?.join("bin");
    std::fs::create_dir_all(&dir).ok()?;

    let script = dir.join("burrow");
    std::fs::write(&script, BURROW_SCRIPT).ok()?;

    let tmux = dir.join("tmux");
    std::fs::write(&tmux, TMUX_SHIM).ok()?;

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let perms = std::fs::Permissions::from_mode(0o755);
        let _ = std::fs::set_permissions(&script, perms.clone());
        let _ = std::fs::set_permissions(&tmux, perms);
    }

    Some(BURROW_BIN_DIR.get_or_init(|| dir))
}

fn burrow_session_dir(app: &AppHandle) -> Option<std::path::PathBuf> {
    let dir = app.path().app_data_dir().ok()?.join("sessions");
    std::fs::create_dir_all(&dir).ok()?;
    Some(dir)
}

// ── Agent config directories ───────────────────────────────────────────────────
// Hooks + agent docs are installed into each agent's *config dir*. By default that's
// ~/.claude (Claude) and ~/.codex (Codex), but a user can point an agent elsewhere via
// CLAUDE_CONFIG_DIR / CODEX_HOME — and crucially can use a DIFFERENT dir per work folder.
// We can't reliably read that per-folder env from here (the shell sets it after spawn),
// so the set of dirs to target is an explicit, persisted list the user edits in Settings.
// It's seeded with the defaults plus whatever CLAUDE_CONFIG_DIR/CODEX_HOME the app itself
// inherited at launch.

#[derive(Debug, Serialize, Deserialize, Clone, Default)]
struct ConfigDirs {
    claude: Vec<String>,
    codex: Vec<String>,
    #[serde(default)]
    copilot: Vec<String>,
}

fn config_dirs_path(app: &AppHandle) -> Option<PathBuf> {
    Some(app.path().app_data_dir().ok()?.join("config-dirs.json"))
}

fn dedup(mut v: Vec<String>) -> Vec<String> {
    let mut seen = std::collections::HashSet::new();
    v.retain(|s| !s.trim().is_empty() && seen.insert(s.trim().to_string()));
    v.into_iter().map(|s| s.trim().to_string()).collect()
}

/// The dirs to install hooks/docs into. Reads the persisted list if present; otherwise
/// seeds from defaults (~/.claude, ~/.codex) + any CLAUDE_CONFIG_DIR/CODEX_HOME env.
fn load_config_dirs(app: &AppHandle) -> ConfigDirs {
    if let Some(path) = config_dirs_path(app) {
        if let Ok(s) = std::fs::read_to_string(&path) {
            if let Ok(cd) = serde_json::from_str::<ConfigDirs>(&s) {
                // Seed Copilot defaults for configs persisted before Copilot support existed.
                let copilot = if cd.copilot.is_empty() {
                    default_copilot_dirs(app)
                } else {
                    cd.copilot
                };
                return ConfigDirs {
                    claude: dedup(cd.claude),
                    codex: dedup(cd.codex),
                    copilot: dedup(copilot),
                };
            }
        }
    }
    // Seed defaults.
    let home = app.path().home_dir().ok();
    let mut claude = Vec::new();
    let mut codex = Vec::new();
    if let Some(h) = &home {
        claude.push(h.join(".claude").to_string_lossy().to_string());
        codex.push(h.join(".codex").to_string_lossy().to_string());
    }
    if let Ok(d) = std::env::var("CLAUDE_CONFIG_DIR") {
        claude.push(d);
    }
    if let Ok(d) = std::env::var("CODEX_HOME") {
        codex.push(d);
    }
    ConfigDirs {
        claude: dedup(claude),
        codex: dedup(codex),
        copilot: dedup(default_copilot_dirs(app)),
    }
}

/// Copilot CLI config dirs: ~/.copilot plus any $COPILOT_HOME the app inherited.
fn default_copilot_dirs(app: &AppHandle) -> Vec<String> {
    let mut copilot = Vec::new();
    if let Ok(h) = app.path().home_dir() {
        copilot.push(h.join(".copilot").to_string_lossy().to_string());
    }
    if let Ok(d) = std::env::var("COPILOT_HOME") {
        copilot.push(d);
    }
    copilot
}

fn save_config_dirs(app: &AppHandle, cd: &ConfigDirs) {
    let Some(path) = config_dirs_path(app) else { return };
    if let Some(parent) = path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    if let Ok(s) = serde_json::to_string_pretty(cd) {
        let _ = std::fs::write(path, s);
    }
}

fn install_agent_docs(app: &AppHandle) {
    let dirs = load_config_dirs(app);

    for claude_dir in &dirs.claude {
        let claude_dir = Path::new(claude_dir);
        let skill_dir = claude_dir.join("skills").join("burrow");
        if std::fs::create_dir_all(&skill_dir).is_ok() {
            let _ = std::fs::write(skill_dir.join("SKILL.md"), BURROW_SKILL_MD);
        }
        let kanban_skill_dir = claude_dir.join("skills").join("kanban");
        if std::fs::create_dir_all(&kanban_skill_dir).is_ok() {
            let _ = std::fs::write(kanban_skill_dir.join("SKILL.md"), KANBAN_SKILL_MD);
        }
        // Inject a brief always-in-context rule into ~/.claude/CLAUDE.md so Claude
        // never reaches for its built-in Agent/fork tool before loading the skill.
        let claude_md = claude_dir.join("CLAUDE.md");
        let existing = std::fs::read_to_string(&claude_md).unwrap_or_default();
        let block = format!("<!-- BURROW:BEGIN -->\n{BURROW_CLAUDE_MD_RULE}\n<!-- BURROW:END -->");
        let merged = match (existing.find("<!-- BURROW:BEGIN -->"), existing.find("<!-- BURROW:END -->")) {
            (Some(s), Some(e)) if e > s => {
                let end = e + "<!-- BURROW:END -->".len();
                format!("{}{}{}", &existing[..s], block, &existing[end..])
            }
            _ if existing.trim().is_empty() => block,
            _ => format!("{}\n\n{block}", existing.trim_end()),
        };
        let _ = std::fs::write(&claude_md, merged);
    }

    for codex_dir in &dirs.codex {
        let codex_dir = Path::new(codex_dir);
        if std::fs::create_dir_all(codex_dir).is_ok() {
            let agents = codex_dir.join("AGENTS.md");
            let existing = std::fs::read_to_string(&agents).unwrap_or_default();
            let block = format!("<!-- BURROW:BEGIN -->\n{BURROW_AGENT_DOC}\n<!-- BURROW:END -->");
            let merged = match (existing.find("<!-- BURROW:BEGIN -->"), existing.find("<!-- BURROW:END -->")) {
                (Some(s), Some(e)) if e > s => {
                    let end = e + "<!-- BURROW:END -->".len();
                    format!("{}{}{}", &existing[..s], block, &existing[end..])
                }
                _ if existing.trim().is_empty() => block,
                _ => format!("{}\n\n{block}", existing.trim_end()),
            };
            let _ = std::fs::write(&agents, merged);
        }
    }

    // Copilot CLI: same SKILL.md spec as Claude (it also reads CLAUDE.md), but
    // skills are discovered from dirs listed in `skillDirectories` in
    // <copilot-dir>/settings.json — there's no implicit `skills/` lookup. So we
    // write <copilot-dir>/skills/burrow/SKILL.md AND register <copilot-dir>/skills
    // in skillDirectories (non-destructive merge). This is what makes `/burrow`
    // resolve in a Copilot session.
    for copilot_dir in &dirs.copilot {
        let skills_root = Path::new(copilot_dir).join("skills");
        let skill_dir = skills_root.join("burrow");
        if std::fs::create_dir_all(&skill_dir).is_ok() {
            let _ = std::fs::write(skill_dir.join("SKILL.md"), BURROW_SKILL_MD);
        }
        let kanban_skill_dir = skills_root.join("kanban");
        if std::fs::create_dir_all(&kanban_skill_dir).is_ok() {
            let _ = std::fs::write(kanban_skill_dir.join("SKILL.md"), KANBAN_SKILL_MD);
        }
        register_copilot_skill_dir(
            &Path::new(copilot_dir).join("settings.json"),
            &skills_root.to_string_lossy(),
        );
    }
}

// Add `dir` to the `skillDirectories` array in Copilot's settings.json without
// clobbering the user's other settings. Absent/empty file → create it; unparseable
// → skip (never destroy a file we can't read).
fn register_copilot_skill_dir(path: &Path, dir: &str) {
    let existing = std::fs::read_to_string(path).unwrap_or_default();
    let mut root: serde_json::Value = if existing.trim().is_empty() {
        json!({})
    } else {
        match serde_json::from_str(&existing) {
            Ok(v) => v,
            Err(_) => return,
        }
    };
    let Some(obj) = root.as_object_mut() else { return };
    let arr = obj
        .entry("skillDirectories")
        .or_insert_with(|| json!([]));
    let Some(arr) = arr.as_array_mut() else { return };
    if arr.iter().any(|v| v.as_str() == Some(dir)) {
        return; // already registered
    }
    arr.push(json!(dir));
    if let Some(parent) = path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    if let Ok(s) = serde_json::to_string_pretty(&root) {
        let _ = std::fs::write(path, s);
    }
}

// Install a persistent status hook into an agent's global hook config (Claude's
// ~/.claude/settings.json, Codex's ~/.codex/hooks.json — same schema). The hook
// runs `burrow hook`, which no-ops unless BURROW_PTY_ID is set (i.e. outside a
// Burrow PTY). Non-destructive: parses existing JSON, only APPENDS our entry to
// each event array, skips if already present, and backs up the original first.
// This is what makes status work for manually-started and reattached agent tabs,
// the same way Superset registers one global notify hook.
fn merge_status_hooks(path: &Path, events: &[&str], cmd: &str, set_shift_enter: bool) {
    let existing = std::fs::read_to_string(path).unwrap_or_default();
    // Only proceed if the file is absent/empty or valid JSON — never clobber a file
    // we can't parse.
    let mut root: serde_json::Value = if existing.trim().is_empty() {
        json!({})
    } else {
        match serde_json::from_str(&existing) {
            Ok(v) => v,
            Err(_) => return,
        }
    };
    if !root.is_object() {
        return;
    }
    if !existing.trim().is_empty() {
        let _ = std::fs::write(path.with_extension("json.burrow-bak"), &existing);
    }

    let obj = root.as_object_mut().unwrap();
    let mut changed = false;

    if set_shift_enter && obj.get("shiftEnterKeyBindingInstalled") != Some(&json!(true)) {
        obj.insert("shiftEnterKeyBindingInstalled".into(), json!(true));
        changed = true;
    }

    let hooks = obj.entry("hooks").or_insert_with(|| json!({}));
    let Some(hooks) = hooks.as_object_mut() else { return };
    for ev in events {
        let arr = hooks.entry(*ev).or_insert_with(|| json!([]));
        let Some(arr) = arr.as_array_mut() else { continue };
        // Dedupe: our command is recognizable by mentioning BURROW_PTY_ID + "hook".
        let present = arr.iter().any(|grp| {
            grp.get("hooks")
                .and_then(|h| h.as_array())
                .is_some_and(|hs| {
                    hs.iter().any(|h| {
                        h.get("command")
                            .and_then(|c| c.as_str())
                            .is_some_and(|c| c.contains("BURROW_PTY_ID") && c.contains("hook"))
                    })
                })
        });
        if present {
            continue;
        }
        arr.push(json!({ "hooks": [ { "type": "command", "command": cmd } ] }));
        changed = true;
    }

    if changed {
        if let Some(parent) = path.parent() {
            let _ = std::fs::create_dir_all(parent);
        }
        if let Ok(s) = serde_json::to_string_pretty(&root) {
            let _ = std::fs::write(path, s);
        }
    }
}

fn install_status_hooks(app: &AppHandle) {
    let Ok(data) = app.path().app_data_dir() else { return };
    let burrow = data.join("bin").join("burrow");
    // Single-quote the path — the macOS app-data dir contains "Application Support".
    let cmd = format!("[ -n \"$BURROW_PTY_ID\" ] && '{}' hook || true", burrow.display());

    let dirs = load_config_dirs(app);

    // Claude: status events. `burrow hook` maps each event → a Burrow state:
    //   SessionStart   → session  (carries model/source/title; labels the tab)
    //   StopFailure    → error    (turn ended on an API error; detail=error_type)
    //   Notification   → permission|waiting for the actionable `type`s
    //                    (permission_prompt/idle_prompt), no-op otherwise.
    // Notification is registered again (it WAS dropped as pure telemetry) now that
    // `burrow hook` discriminates on the notification `type` rather than treating
    // every Notification as actionable.
    let claude_events = [
        "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "PermissionRequest",
        "SessionStart", "StopFailure", "Notification",
    ];
    for d in &dirs.claude {
        merge_status_hooks(&Path::new(d).join("settings.json"), &claude_events, &cmd, true);
    }

    // Codex: same hook schema, in <codex-dir>/hooks.json. SessionStart/StopFailure
    // and the Notification `type` discrimination are Claude-specific hook events —
    // Codex's hook surface doesn't define them, so we scope those to Claude only
    // and leave Codex on its existing lifecycle events (its notify path handles
    // approvals/turn-complete instead).
    let codex_events = ["UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop"];
    for d in &dirs.codex {
        merge_status_hooks(&Path::new(d).join("hooks.json"), &codex_events, &cmd, false);
    }

    // Copilot CLI: a *separate* schema — its own file per hook config at
    // <copilot-dir>/hooks/<name>.json (NOT merged into a shared settings file),
    // camelCase event names, and command objects keyed by "bash" not "command".
    // Because each event has its own array, we bake the target state straight into
    // the command (`burrow status <state>`) instead of routing through `burrow hook`
    // and parsing Copilot's stdin schema. We own the whole `burrow.json` file, so we
    // write/remove it wholesale rather than merging.
    for d in &dirs.copilot {
        write_copilot_hooks(&Path::new(d).join("hooks").join("burrow.json"), &burrow);
    }
}

// Build + write Copilot's dedicated hooks file. State is embedded per-event; the
// command no-ops outside a Burrow PTY (BURROW_PTY_ID unset).
fn write_copilot_hooks(path: &Path, burrow: &Path) {
    let bash = |state: &str| {
        json!([{
            "type": "command",
            "bash": format!("[ -n \"$BURROW_PTY_ID\" ] && '{}' status {} || true", burrow.display(), state),
            "timeoutSec": 5
        }])
    };
    let doc = json!({
        "version": 1,
        "hooks": {
            "userPromptSubmitted": bash("running"),
            "preToolUse": bash("running"),
            "postToolUse": bash("running"),
            // Copilot fires `notification` when it needs the user (permission/input
            // prompt) — verified empirically: it fires on a permission request but
            // NOT on a normal `--allow-all` turn. Map it to `waiting` so the tab gets
            // the blue need-input dot, mirroring Claude's Notification handling.
            "notification": bash("waiting"),
            "agentStop": bash("done"),
            "sessionEnd": bash("done"),
        }
    });
    if let Some(parent) = path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    if let Ok(s) = serde_json::to_string_pretty(&doc) {
        let _ = std::fs::write(path, s);
    }
}

// Reverse of merge_status_hooks: drop every hook group whose command is ours
// (recognizable by the BURROW_PTY_ID + "hook" marker), leaving any other hooks —
// including the user's own and Superset's — untouched.
fn unmerge_status_hooks(path: &Path) {
    let Ok(existing) = std::fs::read_to_string(path) else { return };
    let Ok(mut root) = serde_json::from_str::<serde_json::Value>(&existing) else { return };
    let Some(hooks) = root.get_mut("hooks").and_then(|h| h.as_object_mut()) else { return };

    let mut changed = false;
    for (_event, arr) in hooks.iter_mut() {
        let Some(arr) = arr.as_array_mut() else { continue };
        let before = arr.len();
        arr.retain(|grp| {
            !grp.get("hooks")
                .and_then(|h| h.as_array())
                .is_some_and(|hs| {
                    hs.iter().any(|h| {
                        h.get("command")
                            .and_then(|c| c.as_str())
                            .is_some_and(|c| c.contains("BURROW_PTY_ID") && c.contains("hook"))
                    })
                })
        });
        if arr.len() != before {
            changed = true;
        }
    }
    if changed {
        let _ = std::fs::write(path.with_extension("json.burrow-bak"), &existing);
        if let Ok(s) = serde_json::to_string_pretty(&root) {
            let _ = std::fs::write(path, s);
        }
    }
}

fn uninstall_status_hooks(app: &AppHandle) {
    let dirs = load_config_dirs(app);
    for d in &dirs.claude {
        unmerge_status_hooks(&Path::new(d).join("settings.json"));
    }
    for d in &dirs.codex {
        unmerge_status_hooks(&Path::new(d).join("hooks.json"));
    }
    // Copilot: we own the whole file, so just delete it.
    for d in &dirs.copilot {
        let _ = std::fs::remove_file(Path::new(d).join("hooks").join("burrow.json"));
    }
}

/// Re-install the global status hooks (idempotent). Exposed so the UI/CLI can
/// repair them without restarting the app.
#[tauri::command]
fn reinstall_status_hooks(app: AppHandle) {
    ensure_burrow_bin(&app);
    install_status_hooks(&app);
}

/// Remove the global status hooks from the Claude/Codex configs.
#[tauri::command]
fn remove_status_hooks(app: AppHandle) {
    uninstall_status_hooks(&app);
}

/// One-button repair for agent status dots. Force-reclaims hook.port to THIS
/// instance's live port (rescues revived/reattached PTYs whose baked
/// BURROW_HOOK_PORT is stale after a restart) and re-installs the burrow bin +
/// global status hooks. Returns the live hook-server port for UI feedback.
#[tauri::command]
fn repair_agent_status(app: AppHandle) -> u16 {
    let port = get_hook_server_port();
    if port != 0 {
        if let Ok(data) = app.path().app_data_dir() {
            let _ = std::fs::create_dir_all(&data);
            write_hook_port(&data, port);
        }
    }
    ensure_burrow_bin(&app);
    install_status_hooks(&app);
    port
}

/// The agent config dirs hooks/docs are installed into (seeded with defaults + env).
#[tauri::command]
fn get_config_dirs(app: AppHandle) -> ConfigDirs {
    load_config_dirs(&app)
}

/// Persist a new set of agent config dirs, then re-install hooks + docs into them.
/// Returns the cleaned/deduped list that was saved.
#[tauri::command]
fn set_config_dirs(app: AppHandle, claude: Vec<String>, codex: Vec<String>, copilot: Vec<String>) -> ConfigDirs {
    let cd = ConfigDirs { claude: dedup(claude), codex: dedup(codex), copilot: dedup(copilot) };
    save_config_dirs(&app, &cd);
    ensure_burrow_bin(&app);
    install_status_hooks(&app);
    install_agent_docs(&app);
    cd
}

const BURROW_CLAUDE_MD_RULE: &str = "## Burrow IDE — sub-agent delegation rule\n\
You are running inside **Burrow** IDE. When you want to delegate work to sub-agents or fan out tasks in parallel, use `burrow spawn` via Bash — NOT the built-in `Agent` tool or `fork`. The `Agent` tool runs in-process and its agents won't appear as Burrow tabs. Type `/burrow` to load the full delegation guide.";

const BURROW_AGENT_DOC: &str = "## Delegating to sub-agents (`burrow`)\n\
You are running inside Burrow, which gives you a `burrow` CLI to delegate work to sub-agents in new terminal tabs. Reach for it whenever the user wants to **delegate to agents**, **spawn an agent to** do something, run work **in parallel**, **fan out** subtasks, or **hand off** a task.\n\n\
- `burrow spawn <command...>` — open a new tab in this project running <command> (fire-and-forget, returns immediately). Example: `burrow spawn claude \"write tests for src/foo\"`.\n\
- `burrow spawn --token t1 claude \"...\"` then later `burrow collect t1` — delegate with a tracking token, keep working, and pull the sub-agent's final message whenever you want (non-blocking). `burrow collect` with no token returns every finished sub-agent.\n\
- `burrow sessions` — list the live sub-agent tabs (or `--count`).\n\
- `burrow worktree <branch> [--base-ref REF] [--path DIR]` — create a git worktree off this repo on a new/existing branch; it appears in the Sidebar nested under the repo.\n\
- `burrow spawn --cwd /path claude \"...\"` — run the new tab in a different directory.\n\
- `burrow set-status \"text\"` / `burrow set-status` — show/clear a status label in this tab's header.\n\
- `burrow trigger-flash` — briefly flash this tab as a visual ping to the user.\n\
- `burrow diff --last-turn` — git diff from HEAD at the start of the current agent turn.\n\
- `burrow top` — table of all live Burrow PTY sessions.\n\
- `burrow list-workspaces` / `burrow list-tabs [--ws ID]` — print workspaces / tabs (tab-separated, ids first).\n\
- `burrow focus-workspace ID` / `burrow focus-tab ID` — switch the UI to a workspace / tab.\n\
- `burrow new-tab [--ws ID] [--cmd CMD]` — open a new terminal tab (any workspace).\n\n\
Do NOT block waiting on sub-agents. Fan out the work, continue your own, then `burrow collect` for a recap. Respect the soft per-workspace concurrency limit `burrow spawn` reports. Sub-agents run interactively on the subscription (never use `claude -p`).";

const KANBAN_SKILL_MD: &str = "---\n\
name: kanban\n\
description: Read or manage this repo's Burrow Mission Control Kanban board (Backlog → Todo → In Progress → For Review → Done). Use when the user says \"/kanban\", \"add this to the board\", \"create a task\", \"what's on the board\", \"move this card\", or wants work tracked/started through the board instead of a plain spawned agent.\n\
---\n\n\
# Kanban board (Mission Control)\n\n\
Burrow's board backs its cards with `mission_tasks` rows. Board tasks run as **embedded ACP chat sessions only** — Claude, Codex, Gemini, opencode, whatever `agent` names — never a terminal tab. Starting one never opens a tab in the main view; only an explicit worktree creation shows up there (nested under its repo).\n\n\
## Prefer the Burrow MCP tools\n\n\
If you have MCP tools named `board_list`, `board_create`, `board_move` (served by the **burrow** MCP server), **use those** — typed, validated, structured results. The `burrow` shell CLI below is the **fallback** for shells and non-MCP contexts; every CLI subcommand maps to an equivalent tool.\n\n\
- `board_list({column?})` / `burrow board-list [--column backlog|todo|in_progress|for_review|done]` — list this repo's cards. **Always call this before creating a task**, to avoid duplicating one that already exists.\n\
- `board_create({title, description?, agent?, model?, worktree?})` / `burrow board-create --title T [--description D] [--agent claude|claude-acp|codex|gemini|opencode] [--model M] [--worktree|--no-worktree]` — create a card in **Backlog**. Omit `agent` to default to `claude-acp`. Returns/prints the new task id — report it back to the user. Backlog is the default: spawning is the human's/board's job unless the user explicitly says \"start it now\".\n\
- `board_move({taskId, column})` / `burrow board-move <taskId> <backlog|todo|in_progress|for_review|done>` — move a card. You may move a card up to `for_review` (e.g. when a sub-agent you spawned for that task finishes and you judge the work ready). You may **not** move a card to `done` — that's rejected outright; only a human marks a task done from the board UI.\n\n\
## Starting a task now\n\n\
If asked to start a Backlog task immediately: create it, then move it to `todo`. The actual worktree creation + agent spawn only happens once the app's own Backlog→Todo handler picks up the move (async — the call returns once it's requested, not once the agent is running) — confirm with the user that the card moved, and have them check the board if the agent doesn't appear to start.\n\n\
## When NOT to use this\n\n\
Ad-hoc work with no need for board tracking (a quick refactor, a one-off question) — just do it, or use `spawn`/`burrow spawn` for a fire-and-forget sub-agent. Reach for the board when the user wants the work visible/tracked as a card, or explicitly asks for \"/kanban\" / \"the board\".";

const BURROW_SKILL_MD: &str = "---\n\
name: burrow\n\
description: Delegate work to sub-agents by spawning new terminal tabs from inside the Burrow IDE. Use when the user asks to run work in parallel, hand a task to another agent, or when you want to fan out independent subtasks and collect their results without blocking.\n\
---\n\n\
# Delegating with `burrow`\n\n\
You are running inside **Burrow**. You can delegate work to sub-agents in new tabs. The model is **fire-and-forget + collect**: spawn agents, keep doing your own work, then pull their results when you want — never sit blocked waiting on them.\n\n\
## Prefer the Burrow MCP tools\n\
If you have MCP tools whose names are `spawn`, `create_worktree`, `list_workspaces`, `collect_results`, `send_to_tab`, `wait_result`, `pr_create`/`pr_list`/`pr_view`/`pr_merge`, `focus_tab`, `focus_workspace`, `new_tab`, `worktree_remove` (served by the **burrow** MCP server), **use those** — they are typed, validated, and return structured results. `spawn` with `wait: true` blocks and returns the sub-agent's captured result directly (no separate collect step); omit `wait` to fire-and-forget and pick results up later with `collect_results`. The `burrow` shell CLI below is the **fallback** for shells and non-MCP contexts, and every CLI subcommand maps to an equivalent tool.\n\n\
## Spawn sub-agents (fire-and-forget)\n\
```\n\
burrow spawn claude \"write unit tests for src/foo\"\n\
burrow spawn codex \"refactor the auth module\"\n\
```\n\
Opens a new tab in the **current project** and runs the command interactively. Returns immediately.\n\n\
## Fan out with tokens, then collect (non-blocking)\n\
```\n\
burrow spawn --token a claude \"audit src/auth for bugs\"\n\
burrow spawn --token b claude \"audit src/api for bugs\"\n\
# ...go do your own work in the meantime...\n\
burrow collect a b      # prints results of whichever have FINISHED, and only those\n\
burrow collect          # or: collect every finished sub-agent, no token list\n\
```\n\
`burrow collect` never blocks: it prints the final message of each finished token and **consumes** it, so a later `collect` returns only newly-finished ones. Tokens still running are reported as pending. Loop back and `collect` again later to pick them up — do useful work between calls, don't poll in a tight loop.\n\n\
## Recap pattern\n\
Spawn N agents up front → continue your task → near the end, `burrow collect` (optionally a few times as stragglers finish) → summarize what each returned for the user. You drive the recap; the sub-agents just drop their results for you.\n\n\
## Worktrees\n\
Create a git worktree off this repo on a new or existing branch — it shows up in the Sidebar nested under the repo, ready to open and run agents in.\n\
```\n\
burrow worktree feat/login                 # new branch off HEAD (or check out existing)\n\
burrow worktree hotfix --base-ref main      # new branch based on main\n\
burrow worktree feat/x --path ~/wt/x        # override the on-disk location\n\
```\n\
Use this to isolate a sub-task on its own branch/checkout instead of sharing the working tree. The worktree's disk path defaults to Burrow's configured worktrees dir.\n\n\
## Status labels, progress & logs\n\
```\n\
burrow set-status \"running tests...\"   # show a label next to this tab's status dot\n\
burrow set-status                        # clear the label\n\
burrow trigger-flash                     # briefly flash this tab (visual ping)\n\
burrow set-progress 0.4 --label \"Building\"  # show a progress bar in the tab (0.0–1.0)\n\
burrow set-progress                      # clear the progress bar\n\
burrow log -- \"Compiled 12 files\"       # append a timestamped log line below the tab bar\n\
burrow log --level warn -- \"Tests slow\" # levels: info (default), warn, error\n\
```\n\
Use these to communicate progress to the user without printing to the terminal.\n\n\
## Inspect what changed this turn\n\
```\n\
burrow diff --last-turn                  # git diff from HEAD at start of this turn\n\
```\n\
Shows exactly what files changed since the user submitted the prompt. Good for a quick sanity-check before reporting done.\n\n\
## Monitor all terminals\n\
```\n\
burrow top                               # table of all live Burrow PTY sessions\n\
```\n\n\
## Inspect / other dir\n\
```\n\
burrow sessions            # list live sub-agent tabs (--count for just the number)\n\
burrow spawn --cwd /path/to/other/project claude \"...\"\n\
```\n\n\
## Read & control the Burrow UI\n\
Inspect and drive the app itself from the terminal — list workspaces/tabs, switch focus, open tabs.\n\
```\n\
burrow list-workspaces             # print every workspace: <id>\\t<name>\\t<path>\n\
burrow list-tabs                   # print this workspace's tabs: <pty-id>\\t<title>\\t<status>\n\
                                    #   status: running/waiting/permission/done/review/error/idle — check it before assuming a tab hasn't finished\n\
burrow list-tabs --ws 3            # tabs of workspace 3\n\
burrow focus-workspace 3           # switch Burrow to (and open) workspace 3\n\
burrow focus-tab 42                # activate the tab with pty id 42 (switches workspace if needed)\n\
burrow new-tab                     # open an empty terminal tab in this workspace\n\
burrow new-tab --cmd \"npm test\"   # open a tab running a command\n\
burrow new-tab --ws 3 --cmd htop   # open a tab in another workspace\n\
```\n\
`list-workspaces` / `list-tabs` are READ commands: they print tab-separated rows to stdout (parse the ids for the focus/new-tab commands). `new-tab` differs from `spawn`: `spawn` is for delegating sub-agents in the current project, `new-tab` is a plain UI action that can target any workspace by id.\n\n\
## Limits & notes\n\
- **Soft concurrency limit** (per workspace, default 3, set in Burrow Settings): `burrow spawn` prints the current cap. Respect it — don't exceed it. It is advisory, not enforced, so it's on you.\n\
- Sub-agents run **interactively on the subscription**. Never pass `-p`/`--print`; never use the Agent SDK.\n\
- Result capture works for `claude` sub-agents (via its Stop hook). Other agents spawn fine but only return a collectable result once they emit a done signal.\n\
- `burrow wait <token>` still exists (blocks until one finishes) but prefer `collect` so you stay productive instead of blocked.\n\n\
## Rules — when to use sidebar feedback\n\n\
These rules apply to every task you run inside Burrow. Follow them unless the user explicitly says otherwise.\n\n\
**Status label (`burrow set-status`):**\n\
- Call `burrow set-status \"<phase>\"` at the start of any meaningful work phase (e.g. `\"analyzing\"`, `\"running tests\"`, `\"applying fixes\"`).\n\
- Update the label when the phase changes (e.g. switch from `\"analyzing\"` to `\"running tests\"`).\n\
- Clear it with `burrow set-status` (no arg) when your turn ends so the tab header returns to the agent status dot.\n\
- Keep labels short — one or two words. The user reads them at a glance.\n\n\
**Visual flash (`burrow trigger-flash`):**\n\
- Call `burrow trigger-flash` once, at the very end of a turn, when you have finished a significant task and want to draw the user's attention to this tab (e.g. tests passed, a long refactor completed).\n\
- Do NOT flash mid-turn or on trivial steps — it is a \"done\" signal, not a progress ping.\n\
- Do NOT flash if the turn ended in an error or requires immediate user action (the status dot already signals that).\n\n\
**Diff check (`burrow diff --last-turn`):**\n\
- Before reporting a multi-file change as complete, run `burrow diff --last-turn` internally as a sanity check to confirm the expected files changed.\n\
- You may skip this for single-file edits or when the user's request was purely read-only.\n\n\
**Progress bar (`burrow set-progress`):**\n\
- Use `burrow set-progress <0.0-1.0> --label \"<phase>\"` for tasks with measurable progress (running many tests, compiling many files, processing a list).\n\
- Clear with `burrow set-progress` (no arg) when the task ends.\n\
- Do NOT use for tasks where progress is not measurable — `set-status` suffices.\n\n\
**Log strip (`burrow log`):**\n\
- Use `burrow log -- \"message\"` to record key milestones that are worth keeping visible (e.g. \"Compiled 12 files\", \"3 tests failed\", \"Wrote auth.ts\").\n\
- Use `--level warn` or `--level error` for problems.\n\
- Do NOT log every step — only events the user would want to scroll back and read. Aim for 3-8 log lines per turn max.\n\n\
**Example turn lifecycle:**\n\
```\n\
burrow set-status \"analyzing\"\n\
burrow log -- \"Reading 8 files\"\n\
# ...read files, understand the problem...\n\
burrow set-status \"fixing\"\n\
burrow set-progress 0.0 --label \"Editing\"\n\
# ...make edits, update progress as files done...\n\
burrow set-progress 1.0 --label \"Editing\"\n\
burrow set-status \"testing\"\n\
burrow set-progress 0.0 --label \"Tests\"\n\
# ...run tests...\n\
burrow log -- \"All tests passed\"\n\
burrow set-progress          # clear\n\
burrow diff --last-turn      # quick sanity check\n\
burrow set-status            # clear — turn done\n\
burrow trigger-flash         # ping user: \"this tab finished\"\n\
```\n\n\
## Generating diagrams\n\
When asked to visualize architecture, flows, or data models, use Mermaid syntax wrapped in a `burrow diagram` call:\n\
```sh\n\
burrow diagram 'flowchart LR\n\
  A --> B\n\
  B --> C'\n\
```\n\
This renders an interactive SVG diagram in the Burrow UI. Pass the entire Mermaid source as a single argument (single-quoted). Any valid Mermaid diagram type works: flowchart, sequenceDiagram, classDiagram, erDiagram, gantt, etc.";

// ── Hook HTTP server ──────────────────────────────────────────────────────────

static HOOK_SERVER_PORT: OnceLock<u16> = OnceLock::new();

// Write hook.port atomically (temp + rename) so a reader never sees a half-written
// file. Format: line 1 = port, line 2 = owning pid — the pid lets readers and the
// reclaim loop detect a stale file left behind by a now-dead instance.
fn write_hook_port(data: &Path, port: u16) {
    let tmp = data.join("hook.port.tmp");
    let body = format!("{}\n{}", port, std::process::id());
    if std::fs::write(&tmp, body).is_ok() {
        let _ = std::fs::rename(&tmp, data.join("hook.port"));
    }
}

// Probe whether a pid is alive without signalling it (`kill -0`). Used by the
// hook.port reclaim loop to avoid two live instances fighting over the file, and
// by the frontend's PID-liveness sweep (via the is_pid_alive command) to clear a
// stuck agent dot the instant the agent process dies.
fn pid_alive(pid: u32) -> bool {
    std::process::Command::new("kill")
        .arg("-0")
        .arg(pid.to_string())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

/// Frontend PID-liveness sweep: is the agent process still alive? An in-flight
/// agent leaf whose pid is gone settles its status dot immediately instead of
/// waiting out the slower dead-PTY watchdog. Only ever called with a LOCAL pid
/// (the hook gates on hostname — see start_hook_server / `burrow status --pid`).
#[tauri::command]
fn is_pid_alive(pid: u32) -> bool {
    pid_alive(pid)
}

/// This machine's hostname, cached. Compared against the `host` a `burrow status
/// --pid` POST carries so we only forward a pid the agent reported from THIS host
/// — a pid from an agent running over SSH is a remote pid and must not drive the
/// local kill(pid,0) sweep. Uses the same `hostname` command the CLI calls, so the
/// two strings match exactly.
fn local_hostname() -> &'static str {
    static HOSTNAME: std::sync::OnceLock<String> = std::sync::OnceLock::new();
    HOSTNAME.get_or_init(|| {
        std::process::Command::new("hostname")
            .output()
            .ok()
            .and_then(|o| String::from_utf8(o.stdout).ok())
            .map(|s| s.trim().to_string())
            .unwrap_or_default()
    })
}

fn start_hook_server(app: AppHandle) {
    let server = tiny_http::Server::http("127.0.0.1:0").expect("hook server bind failed");
    let port = server.server_addr().to_ip().expect("hook server has no IP addr").port();
    let _ = HOOK_SERVER_PORT.set(port);
    // Publish the port to a stable file so globally-installed agent hooks can find
    // the CURRENT server after an app restart (the port is random each launch).
    if let Ok(data) = app.path().app_data_dir() {
        let _ = std::fs::create_dir_all(&data);
        write_hook_port(&data, port);
        // Self-reclaim: a transient instance (e.g. a `tauri:dev` run sharing this
        // app-data dir) overwrites hook.port with ITS port, then dies — leaving the
        // surviving app's PTYs pointing at a dead port (the documented stale-port
        // regression). Reclaim the file, but ONLY when its recorded pid is dead:
        // two LIVE instances must not fight over the single shared file. Live PTYs
        // use their baked BURROW_HOOK_PORT anyway; the file only matters for the
        // post-restart reattach path, where there's normally just one instance.
        let reclaim_dir = data.clone();
        std::thread::spawn(move || {
            let me = std::process::id();
            loop {
                std::thread::sleep(std::time::Duration::from_secs(3));
                let owned = std::fs::read_to_string(reclaim_dir.join("hook.port"))
                    .ok()
                    .and_then(|s| s.lines().nth(1).and_then(|p| p.trim().parse::<u32>().ok()));
                match owned {
                    Some(pid) if pid == me => {}              // already ours
                    Some(pid) if pid_alive(pid) => {}         // another live instance — leave it
                    _ => write_hook_port(&reclaim_dir, port), // stale / dead / legacy-format → reclaim
                }
            }
        });
    }

    std::thread::spawn(move || {
        for mut req in server.incoming_requests() {
            let url = req.url().to_string();
            let mut body = String::new();
            let _ = req.as_reader().read_to_string(&mut body);

            if let Ok(val) = serde_json::from_str::<serde_json::Value>(&body) {
                match url.as_str() {
                    "/hook" | "/" => {
                        // Agent lifecycle hook → status dot update.
                        if let (Some(pty_id), Some(state)) =
                            (val["ptyId"].as_u64(), val["state"].as_str())
                        {
                            // PID-liveness: forward the agent pid ONLY when the POST's
                            // `host` matches this machine — a pid from an SSH'd agent is
                            // a remote pid we must not kill(pid,0)-poll locally (supacode
                            // gating). A missing host/pid simply means no sweep → the
                            // dead-PTY watchdog still covers it.
                            let local_pid = match (val["pid"].as_u64(), val["host"].as_str()) {
                                (Some(pid), Some(host)) if host == local_hostname() => Some(pid),
                                _ => None,
                            };
                            // `error`/`session` carry extra metadata, and any state may
                            // now carry a pid, so emit an OBJECT {state, …} when there's
                            // anything beyond the bare state. The legacy bare-string shape
                            // (running/waiting/permission/done with no pid) is preserved
                            // for back-compat; XTerm.vue consumes both. Same channel.
                            if state == "error" || state == "session" || local_pid.is_some() {
                                let mut payload = serde_json::Map::new();
                                payload.insert("state".into(), serde_json::json!(state));
                                for k in ["detail", "model", "source", "title"] {
                                    if let Some(v) = val.get(k).and_then(|v| v.as_str()) {
                                        payload.insert(k.into(), serde_json::json!(v));
                                    }
                                }
                                if let Some(pid) = local_pid {
                                    payload.insert("pid".into(), serde_json::json!(pid));
                                }
                                let _ = app.emit(
                                    &format!("pty-hook-{pty_id}"),
                                    serde_json::Value::Object(payload),
                                );
                            } else {
                                let _ = app.emit_all(&format!("pty-hook-{pty_id}"), state.to_string());
                            }
                        }
                    }
                    "/write" => {
                        // tmux send-keys path: write raw bytes into a PTY.
                        // Frontend (XTerm.vue) listens to `pty-write-{id}` and
                        // forwards to the daemon via write_pty.
                        if let (Some(pty_id), Some(data)) =
                            (val["ptyId"].as_u64(), val["data"].as_str())
                        {
                            let _ = app.emit(
                                &format!("pty-write-{pty_id}"),
                                data.to_string(),
                            );
                        }
                    }
                    "/set-status" => {
                        // burrow set-status: custom label shown in the tab header.
                        if let Some(pty_id) = val["ptyId"].as_u64() {
                            let text = val["text"].as_str().unwrap_or("").to_string();
                            let _ = app.emit(
                                &format!("pty-status-text-{pty_id}"),
                                text,
                            );
                        }
                    }
                    "/flash" => {
                        // burrow trigger-flash: briefly highlight the tab.
                        if let Some(pty_id) = val["ptyId"].as_u64() {
                            let _ = app.emit(&format!("pty-flash-{pty_id}"), ());
                        }
                    }
                    "/open-diff" => {
                        // burrow diff --last-turn: open a diff tab in the terminal UI.
                        if let Some(pty_id) = val["ptyId"].as_u64() {
                            let diff = val["diff"].as_str().unwrap_or("").to_string();
                            let title = val["title"].as_str().unwrap_or("diff: last turn").to_string();
                            let _ = app.emit(
                                &format!("pty-open-diff-{pty_id}"),
                                serde_json::json!({ "diff": diff, "title": title }),
                            );
                        }
                    }
                    "/set-progress" => {
                        // burrow set-progress: show/clear a progress bar in the tab header.
                        if let Some(pty_id) = val["ptyId"].as_u64() {
                            let _ = app.emit(
                                &format!("pty-progress-{pty_id}"),
                                serde_json::json!({
                                    "progress": val["progress"],
                                    "label": val["label"].as_str().unwrap_or("")
                                }),
                            );
                        }
                    }
                    "/agent-done" => {
                        // burrow capture: sub-agent finished, result ready for collect.
                        // Scope the broadcast with the spawning workspace path so only
                        // the Manager whose repo actually spawned this token reacts —
                        // origin is recorded in take_spawn_requests when the token was
                        // first seen. Unknown origin (e.g. app restarted mid-spawn)
                        // still emits with originWs omitted, matching old behavior.
                        if let Some(token) = val["token"].as_str() {
                            let origin: State<SpawnTokenOrigin> = app.state();
                            let origin_ws = origin.map.lock().unwrap().remove(token);
                            let mut payload = serde_json::Map::new();
                            payload.insert("token".into(), serde_json::json!(token));
                            if let Some(ws) = origin_ws {
                                payload.insert("originWs".into(), serde_json::json!(ws));
                            }
                            let _ = app.emit("agent-done", serde_json::Value::Object(payload));
                        }
                    }
                    "/log" => {
                        // burrow log: append a timestamped log entry for this tab.
                        if let Some(pty_id) = val["ptyId"].as_u64() {
                            let level = val["level"].as_str().unwrap_or("info").to_string();
                            let message = val["message"].as_str().unwrap_or("").to_string();
                            let _ = app.emit(
                                &format!("pty-log-{pty_id}"),
                                serde_json::json!({ "level": level, "message": message }),
                            );
                        }
                    }
                    "/session-id" => {
                        // burrow hook (UserPromptSubmit): persist Claude session_id for resume.
                        if let Some(pty_id) = val["ptyId"].as_u64() {
                            if let Some(sid) = val["sessionId"].as_str() {
                                let _ = app.emit(
                                    &format!("pty-session-id-{pty_id}"),
                                    sid.to_string(),
                                );
                            }
                        }
                    }
                    _ => {}
                }
            }
            let _ = req.respond(tiny_http::Response::empty(200));
        }
    });
}

#[tauri::command]
fn get_hook_server_port() -> u16 {
    *HOOK_SERVER_PORT.get().unwrap_or(&0)
}

// Record the ptyId assigned to a tmux shim window ID (@N) so that subsequent
// `tmux send-keys -t @N` calls can look up the right PTY to write to. Called by
// Terminal.vue immediately after creating a tab that was spawned via the tmux shim.
#[tauri::command]
fn register_tmux_win(win_id: String, pty_id: u32, app: AppHandle) {
    let Ok(data) = app.path().app_data_dir() else { return };
    let wins_dir = data.join("tmux_wins");
    let _ = std::fs::create_dir_all(&wins_dir);
    let _ = std::fs::write(wins_dir.join(&win_id), pty_id.to_string());
}

// Publish the user's configured max-concurrent-sub-agents to a file the `burrow`
// CLI can read (localStorage lives in the frontend; the CLI can't see it). Same
// file-bridge pattern as hook.port. The limit is a SOFT cap surfaced to agents.
#[tauri::command]
fn set_max_agents(n: u32, app: AppHandle) {
    if let Ok(data) = app.path().app_data_dir() {
        let _ = std::fs::create_dir_all(&data);
        let _ = std::fs::write(data.join("max_agents"), n.max(1).to_string());
    }
}

// ── Daemon management ─────────────────────────────────────────────────────────

fn find_daemon_binary(app: &AppHandle) -> Result<std::path::PathBuf, String> {
    // Dev override
    if let Ok(p) = std::env::var("BURROW_DAEMON_BIN") {
        let path = std::path::PathBuf::from(p);
        if path.exists() { return Ok(path); }
    }
    // Alongside current executable (production bundle)
    if let Ok(exe) = std::env::current_exe() {
        let candidate = exe.parent().unwrap_or(Path::new(".")).join("burrow-daemon");
        if candidate.exists() { return Ok(candidate); }
    }
    // Tauri resource dir (bundled sidecar)
    if let Ok(res) = app.path().resource_dir() {
        for name in &["burrow-daemon", "burrow-daemon-aarch64-apple-darwin", "burrow-daemon-x86_64-apple-darwin"] {
            let c = res.join(name);
            if c.exists() { return Ok(c); }
        }
    }
    Err("burrow-daemon binary not found. Set BURROW_DAEMON_BIN env var in development.".into())
}

fn daemon_ensure(data_dir: &Path, app: &AppHandle) -> Result<Arc<DaemonClient>, String> {
    let socket_path = data_dir.join("daemon.sock");
    let token_path = data_dir.join("daemon.token");

    // Try existing daemon
    if let Ok(token) = std::fs::read_to_string(&token_path) {
        let token = token.trim().to_string();
        if socket_path.exists() {
            let client = Arc::new(DaemonClient::new(socket_path.clone(), token, app.clone()));
            if client.probe() {
                // Reuse only if it's THIS build. The daemon survives app restarts, so
                // after an auto-update the running daemon is the previous version and
                // lacks this build's PTY-level changes (e.g. the login-shell `-l`).
                // On a version mismatch, retire it (kill its published PID) and fall
                // through to spawn a fresh daemon, which removes + rebinds the socket.
                if client.version().as_deref() == Some(DAEMON_PROTO_VERSION) {
                    return Ok(client);
                }
                if let Ok(pid) = std::fs::read_to_string(data_dir.join("daemon.pid")) {
                    if let Ok(pid) = pid.trim().parse::<u32>() {
                        let _ = std::process::Command::new("kill")
                            .arg("-9")
                            .arg(pid.to_string())
                            .status();
                    }
                }
                std::thread::sleep(std::time::Duration::from_millis(150));
                let _ = std::fs::remove_file(&socket_path);
            }
        }
    }

    // Reaching here means no usable daemon (probe failed, socket missing, or a
    // version mismatch we already killed). The published PID — if still alive — is
    // therefore an unreachable orphan: it lost the socket but keeps running until
    // reboot, leaking a process + its dead PTYs every time this path is hit. Reap it
    // before spawning a replacement (kill is a no-op if it's already gone).
    if let Ok(pid) = std::fs::read_to_string(data_dir.join("daemon.pid")) {
        if let Ok(pid) = pid.trim().parse::<u32>() {
            let _ = std::process::Command::new("kill").arg("-9").arg(pid.to_string()).status();
        }
    }

    // Spawn new daemon
    let daemon_bin = find_daemon_binary(app)?;
    std::process::Command::new(&daemon_bin)
        .env("BURROW_DATA_DIR", data_dir)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .map_err(|e| format!("spawn daemon: {e}"))?;

    // Wait up to 3 s for socket + token
    for _ in 0..30 {
        std::thread::sleep(std::time::Duration::from_millis(100));
        if let Ok(token) = std::fs::read_to_string(&token_path) {
            let token = token.trim().to_string();
            if socket_path.exists() {
                let client = Arc::new(DaemonClient::new(socket_path.clone(), token, app.clone()));
                if client.probe() {
                    return Ok(client);
                }
            }
        }
    }
    Err("burrow-daemon did not start in time".into())
}

// ── PTY commands ──────────────────────────────────────────────────────────────

#[tauri::command]
fn create_pty(
    id: u32,
    cwd: String,
    cols: u16,
    rows: u16,
    daemon: State<DaemonState>,
    app: AppHandle,
) -> Result<(), String> {
    let client = daemon.client().ok_or("daemon not connected")?;

    // Build env for the shell: PATH (with burrow bin), BURROW_* vars
    let mut env = serde_json::Map::new();
    env.insert("TERM".into(), json!("xterm-256color"));
    env.insert("COLORTERM".into(), json!("truecolor"));
    // Some TUIs (e.g. GitHub Copilot CLI) gate their full-screen rendering on a
    // non-empty TERM_PROGRAM — an unset value reads as a dumb/non-interactive
    // terminal and they refuse to draw (blank screen). Real emulators always set
    // it (Apple_Terminal, WarpTerminal, vscode…), so we identify ourselves too.
    env.insert("TERM_PROGRAM".into(), json!("Burrow"));
    env.insert("TERM_PROGRAM_VERSION".into(), json!(env!("CARGO_PKG_VERSION")));

    if let Some(bin_dir) = ensure_burrow_bin(&app) {
        let existing = std::env::var("PATH").unwrap_or_default();
        env.insert("PATH".into(), json!(format!("{}:{}", bin_dir.display(), existing)));
    }
    if let Some(sess) = burrow_session_dir(&app) {
        env.insert("BURROW_SESSION_DIR".into(), json!(sess.to_string_lossy().to_string()));
    }
    env.insert("BURROW_CWD".into(), json!(&cwd));
    // Let any agent's hook/notify program report status back via `burrow status`.
    // BURROW_PTY_ID identifies this tab; the port is read live (env first, then the
    // hook.port file under BURROW_HOME_DIR) so it survives an app restart.
    env.insert("BURROW_PTY_ID".into(), json!(id.to_string()));
    // Enable Claude Code agent teams; the tmux shim in bin/ makes it functional.
    env.insert("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS".into(), json!("1"));
    if let Some(port) = HOOK_SERVER_PORT.get() {
        env.insert("BURROW_HOOK_PORT".into(), json!(port.to_string()));
    }
    if let Ok(data) = app.path().app_data_dir() {
        env.insert("BURROW_HOME_DIR".into(), json!(data.to_string_lossy().to_string()));
    }

    let resp = client.cmd(json!({
        "cmd": "CreatePty",
        "pty_id": id,
        "cwd": cwd,
        "cols": cols,
        "rows": rows,
        "env": env,
    }))?;

    if resp["ok"].as_bool() != Some(true) {
        return Err(resp["error"].as_str().unwrap_or("CreatePty failed").to_string());
    }

    // Open data stream: daemon → Tauri event pty-data-{id}
    client.start_stream(id);
    Ok(())
}

#[tauri::command]
fn write_pty(id: u32, data: Vec<u8>, daemon: State<DaemonState>) -> Result<(), String> {
    let client = daemon.client().ok_or("daemon not connected")?;
    let enc = general_purpose::STANDARD.encode(&data);
    client.cmd(json!({"cmd": "WritePty", "pty_id": id, "data": enc}))?;
    Ok(())
}

#[tauri::command]
fn resize_pty(id: u32, cols: u16, rows: u16, daemon: State<DaemonState>) -> Result<(), String> {
    let client = daemon.client().ok_or("daemon not connected")?;
    client.cmd(json!({"cmd": "ResizePty", "pty_id": id, "cols": cols, "rows": rows}))?;
    Ok(())
}

/// Kill the PTY in the daemon (called when the user explicitly closes a tab).
#[tauri::command]
fn kill_pty(id: u32, daemon: State<DaemonState>) {
    if let Some(client) = daemon.client() {
        let _ = client.cmd(json!({"cmd": "KillPty", "pty_id": id}));
        client.stop_stream(id);
    }
}

/// Detach the data stream without killing the PTY (called on app close).
/// The PTY keeps running in the daemon so it can be reattached next session.
#[tauri::command]
fn detach_pty(id: u32, daemon: State<DaemonState>) {
    if let Some(client) = daemon.client() {
        client.stop_stream(id);
    }
}

#[derive(Serialize)]
pub struct PtySessionInfo {
    pub pty_id: u32,
    pub cwd: String,
    pub title: String,
    pub alive: bool,
}

/// List PTY sessions known to the daemon — used by Terminal.vue to restore tabs.
#[tauri::command]
fn list_pty_sessions(daemon: State<DaemonState>) -> Vec<PtySessionInfo> {
    let Some(client) = daemon.client() else { return vec![] };
    let Ok(resp) = client.cmd(json!({"cmd": "ListSessions"})) else { return vec![] };
    resp["sessions"]
        .as_array()
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .filter_map(|s| Some(PtySessionInfo {
            pty_id: s["pty_id"].as_u64()? as u32,
            cwd: s["cwd"].as_str()?.to_string(),
            title: s["title"].as_str()?.to_string(),
            alive: s["alive"].as_bool()?,
        }))
        .collect()
}

#[tauri::command]
fn get_pty_foreground(id: u32, daemon: State<DaemonState>) -> String {
    let Some(client) = daemon.client() else { return String::new() };
    let Ok(resp) = client.cmd(json!({"cmd": "GetForeground", "pty_id": id})) else {
        return String::new()
    };
    resp["process"].as_str().unwrap_or("").to_string()
}

// ── System & daemon stats (title-bar dropdown) ────────────────────────────────

#[derive(Serialize)]
pub struct SystemStats {
    /// Aggregate CPU usage across all cores, 0–100.
    pub cpu_percent: f32,
    pub mem_used: u64,
    pub mem_total: u64,
}

/// CPU + RAM usage for the whole machine. CPU needs two samples spaced by the
/// refresh interval, so we sleep briefly between them.
#[tauri::command]
fn system_stats() -> SystemStats {
    use sysinfo::System;
    let mut sys = System::new();
    sys.refresh_cpu_usage();
    std::thread::sleep(sysinfo::MINIMUM_CPU_UPDATE_INTERVAL);
    sys.refresh_cpu_usage();
    sys.refresh_memory();
    let cpu_percent = sys.global_cpu_usage();
    SystemStats {
        cpu_percent,
        mem_used: sys.used_memory(),
        mem_total: sys.total_memory(),
    }
}

#[derive(Serialize)]
pub struct DaemonStats {
    pub connected: bool,
    pub pid: Option<u32>,
    pub total: usize,
    pub alive: usize,
}

/// Session counts + pid for the live daemon — drives the title-bar dropdown.
#[tauri::command]
fn daemon_stats(daemon: State<DaemonState>, app: AppHandle) -> DaemonStats {
    let pid = app
        .path()
        .app_data_dir()
        .ok()
        .and_then(|d| std::fs::read_to_string(d.join("daemon.pid")).ok())
        .and_then(|s| s.trim().parse::<u32>().ok());
    let Some(client) = daemon.client() else {
        return DaemonStats { connected: false, pid, total: 0, alive: 0 };
    };
    let Ok(resp) = client.cmd(json!({"cmd": "ListSessions"})) else {
        return DaemonStats { connected: false, pid, total: 0, alive: 0 };
    };
    let sessions = resp["sessions"].as_array().cloned().unwrap_or_default();
    let alive = sessions.iter().filter(|s| s["alive"].as_bool() == Some(true)).count();
    DaemonStats { connected: true, pid, total: sessions.len(), alive }
}

/// Kill every dead (non-alive) PTY the daemon still holds. Returns count reaped.
#[tauri::command]
fn clean_daemon(daemon: State<DaemonState>) -> usize {
    let Some(client) = daemon.client() else { return 0 };
    let Ok(resp) = client.cmd(json!({"cmd": "ListSessions"})) else { return 0 };
    let mut reaped = 0;
    for s in resp["sessions"].as_array().cloned().unwrap_or_default() {
        if s["alive"].as_bool() == Some(false) {
            if let Some(pid) = s["pty_id"].as_u64() {
                let _ = client.cmd(json!({"cmd": "KillPty", "pty_id": pid}));
                client.stop_stream(pid as u32);
                reaped += 1;
            }
        }
    }
    reaped
}

/// Kill ORPHANED PTY sessions: alive in the daemon but referenced by no tab —
/// the residue of a tab closed in a past run whose PTY was never reaped, which is
/// what lets a fresh tab collide onto a stale session ("old terminal reappears").
///
/// Safety: a session is killed only when ALL hold:
///  - it is `alive` (dead ones are handled by `clean_daemon`);
///  - `pty_id < 1_000_000` — never touch Mission Control's offset id-space;
///  - its id is in neither `keep_ids` (every live tab/pane id the frontend knows)
///    nor the `terminal_tabs` table (every persisted leaf, incl. split panes and
///    not-yet-opened workspaces). So a reattachable or just-created session stays.
#[tauri::command]
fn kill_orphan_sessions(keep_ids: Vec<u32>, daemon: State<DaemonState>, db: State<DbState>) -> usize {
    let Some(client) = daemon.client() else { return 0 };
    let mut keep: std::collections::HashSet<u32> = keep_ids.into_iter().collect();
    {
        let conn = db.conn.lock().unwrap();
        // `stmt` is a local declared after `conn`, so it drops before `conn` — avoids
        // the borrow outliving the lock guard (E0597 when using `if let` on the Result).
        let mut stmt = conn.prepare("SELECT pty_id FROM terminal_tabs WHERE pty_id IS NOT NULL").ok();
        if let Some(stmt) = stmt.as_mut() {
            if let Ok(rows) = stmt.query_map([], |r| r.get::<_, i64>(0)) {
                for pid in rows.flatten() { keep.insert(pid as u32); }
            }
        }
    }
    let Ok(resp) = client.cmd(json!({"cmd": "ListSessions"})) else { return 0 };
    let mut killed = 0;
    for s in resp["sessions"].as_array().cloned().unwrap_or_default() {
        if s["alive"].as_bool() != Some(true) { continue; }
        let Some(pid) = s["pty_id"].as_u64() else { continue };
        let pid = pid as u32;
        if pid >= 1_000_000 { continue; }      // Mission Control range — leave alone
        if keep.contains(&pid) { continue; }    // live or persisted tab — keep
        let _ = client.cmd(json!({"cmd": "KillPty", "pty_id": pid}));
        client.stop_stream(pid);
        killed += 1;
    }
    killed
}

/// Hard-restart the daemon: kill its process (taking all live PTYs with it) and
/// spawn a fresh one, swapping the connected client in place. Returns the new pid.
#[tauri::command]
fn restart_daemon(daemon: State<DaemonState>, app: AppHandle) -> Result<u32, String> {
    let data_dir = app.path().app_data_dir().map_err(|e| e.to_string())?;
    // Kill the published pid; daemon_ensure reaps any orphan too, but do it up
    // front so the version-match reuse branch can't re-adopt the old process.
    if let Ok(pid) = std::fs::read_to_string(data_dir.join("daemon.pid")) {
        if let Ok(pid) = pid.trim().parse::<u32>() {
            let _ = std::process::Command::new("kill").arg("-9").arg(pid.to_string()).status();
        }
    }
    std::thread::sleep(std::time::Duration::from_millis(150));
    let _ = std::fs::remove_file(data_dir.join("daemon.sock"));
    let client = daemon_ensure(&data_dir, &app)?;
    *daemon.client.lock().unwrap() = Some(client);
    std::fs::read_to_string(data_dir.join("daemon.pid"))
        .ok()
        .and_then(|s| s.trim().parse::<u32>().ok())
        .ok_or_else(|| "daemon restarted but pid unavailable".into())
}

// ── Spawn requests (burrow CLI) ───────────────────────────────────────────────

#[derive(Debug, Serialize)]
pub struct SpawnRequest {
    /// "spawn" (open a tab running `cmd`) or "worktree" (create a git worktree workspace).
    pub kind: String,
    pub cmd: String,
    pub token: String,
    pub cwd: String,
    /// worktree only: branch name + optional base ref for a new branch.
    pub branch: String,
    pub base: String,
    /// tmux shim: window ID (@N) assigned by the shim's new-window/split-window command.
    /// Frontend registers ptyId→winId via register_tmux_win so send-keys can find the PTY.
    pub tmux_win: String,
    /// Control commands (focus-workspace / new-tab): target workspace id (string,
    /// empty = the origin workspace). Single-word field name so it survives serde as-is.
    pub wsid: String,
    /// Control command (focus-tab): target tab/pty id (string, empty when unused).
    pub tabid: String,
    /// diagram kind: mermaid source to render in the UI.
    pub content: String,
}

/// Write a control read-command's answer where the `burrow` CLI is polling for it:
/// `<session>/<token>.result` (payload) + `<token>.done` (marker). Mirrors the
/// `burrow capture`/`burrow wait` convention. No-op when token is empty.
fn write_control_result(app: &AppHandle, token: &str, text: &str) {
    if token.is_empty() { return; }
    if let Some(dir) = burrow_session_dir(app) {
        let _ = std::fs::write(dir.join(format!("{token}.result")), text);
        let _ = std::fs::write(dir.join(format!("{token}.done")), "");
    }
}

/// Workspaces as `<id>\t<name>\t<path>\n` lines. Shared by the `burrow
/// list-workspaces` control command and the MCP `list_workspaces` tool.
fn query_workspaces_tsv(conn: &Connection) -> String {
    let mut s = String::new();
    if let Ok(mut stmt) = conn.prepare(
        "SELECT id, name, path FROM workspaces ORDER BY COALESCE(last_opened,0) DESC, created_at DESC",
    ) {
        if let Ok(rows) = stmt.query_map([], |r| {
            Ok((r.get::<_, i64>(0)?, r.get::<_, String>(1)?, r.get::<_, String>(2)?))
        }) {
            for row in rows.flatten() {
                s.push_str(&format!("{}\t{}\t{}\n", row.0, row.1, row.2));
            }
        }
    }
    s
}

/// Resolve the ROOT repo workspace id for a workspace path: if it's a worktree
/// (has a `parent_id`), returns the parent's id; otherwise returns its own id.
/// Worktree-of-a-worktree is disallowed elsewhere (`create_worktree` enforces
/// `parent_id IS NULL` on the parent), so this is at most one hop. Used to key
/// the Kanban board (`mission_tasks.repo_workspace_id`) the same way regardless
/// of which of a repo's worktrees the request originates from.
fn resolve_repo_workspace_id(conn: &Connection, ws_path: &str) -> Option<i64> {
    conn.query_row(
        "SELECT id, parent_id FROM workspaces WHERE path = ?1",
        rusqlite::params![ws_path],
        |r| Ok((r.get::<_, i64>(0)?, r.get::<_, Option<i64>>(1)?)),
    )
    .ok()
    .map(|(id, parent_id)| parent_id.unwrap_or(id))
}

/// Dir holding one file per pty id with its last-known live agent status
/// (`running`/`waiting`/`permission`/`done`/`review`/`error`/`idle`), written by
/// the frontend's status machine (`set_tab_live_status`). Lets `list_tabs` /
/// `burrow list-tabs` — pure Rust/DB reads with no frontend round-trip — answer
/// "did this agent finish?" instead of just returning pty id + title, which is
/// why callers checking over MCP/CLI kept assuming a finished agent was still
/// running: the status simply wasn't in that payload.
fn tab_status_dir(app: &AppHandle) -> Option<std::path::PathBuf> {
    let dir = burrow_session_dir(app)?.join("tab_status");
    std::fs::create_dir_all(&dir).ok()?;
    Some(dir)
}

#[tauri::command]
fn set_tab_live_status(pty_id: i64, status: String, app: AppHandle) {
    if let Some(dir) = tab_status_dir(&app) {
        let _ = std::fs::write(dir.join(pty_id.to_string()), status);
    }
}

fn read_tab_live_status(app: &AppHandle, pty_id: i64) -> String {
    tab_status_dir(app)
        .and_then(|d| std::fs::read_to_string(d.join(pty_id.to_string())).ok())
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "idle".to_string())
}

/// A workspace's tabs as `<pty_id>\t<title>\t<status>\n` lines. Shared by
/// `burrow list-tabs` and the MCP `list_tabs` tool. `status` is the live
/// agent status (see `tab_status_dir`) — `idle` if the tab never reported one
/// (plain shell, or Burrow restarted since).
fn query_tabs_tsv(app: &AppHandle, conn: &Connection, ws_id: i64) -> String {
    let mut s = String::new();
    if let Ok(mut stmt) = conn.prepare(
        "SELECT pty_id, COALESCE(title, default_title, '') FROM terminal_tabs WHERE workspace_id = ?1 ORDER BY ord ASC",
    ) {
        if let Ok(rows) = stmt.query_map(rusqlite::params![ws_id], |r| {
            Ok((r.get::<_, Option<i64>>(0)?, r.get::<_, String>(1)?))
        }) {
            for row in rows.flatten() {
                let pid = row.0;
                let pid_str = pid.map(|v| v.to_string()).unwrap_or_default();
                let status = pid.map(|v| read_tab_live_status(app, v)).unwrap_or_else(|| "idle".to_string());
                s.push_str(&format!("{}\t{}\t{}\n", pid_str, row.1, status));
            }
        }
    }
    s
}

/// Board tasks for a repo (optionally filtered to one column) as
/// `<id>\t<column>\t<title>\t<status>\n` lines. Shared by `burrow board-list`.
fn query_board_tasks_tsv(conn: &Connection, repo_workspace_id: i64, column: &str) -> String {
    let mut s = String::new();
    let sql = if column.is_empty() {
        "SELECT id, board_column, COALESCE(title,''), COALESCE(status,'') FROM mission_tasks \
         WHERE repo_workspace_id = ?1 ORDER BY board_column ASC, board_order ASC, created_at ASC"
    } else {
        "SELECT id, board_column, COALESCE(title,''), COALESCE(status,'') FROM mission_tasks \
         WHERE repo_workspace_id = ?1 AND board_column = ?2 ORDER BY board_order ASC, created_at ASC"
    };
    let Ok(mut stmt) = conn.prepare(sql) else { return s };
    let map_row = |r: &rusqlite::Row| -> rusqlite::Result<(String, String, String, String)> {
        Ok((r.get(0)?, r.get(1)?, r.get(2)?, r.get(3)?))
    };
    let rows: Vec<(String, String, String, String)> = if column.is_empty() {
        stmt.query_map(rusqlite::params![repo_workspace_id], map_row)
            .map(|it| it.filter_map(|x| x.ok()).collect())
            .unwrap_or_default()
    } else {
        stmt.query_map(rusqlite::params![repo_workspace_id, column], map_row)
            .map(|it| it.filter_map(|x| x.ok()).collect())
            .unwrap_or_default()
    };
    for (id, col, title, status) in rows {
        s.push_str(&format!("{id}\t{col}\t{title}\t{status}\n"));
    }
    s
}

// ── MCP server glue ────────────────────────────────────────────────────────────
// The MCP layer is a typed front door onto the SAME file-request-dir transport
// the `burrow` CLI writes (so Terminal.vue's poll claims MCP-spawned tabs
// identically) plus the same Rust-answered bodies the control commands use.
// Tool bodies live here (private app state); protocol/registry/depth live in
// burrow_mcp_core.

/// User-configurable recursion depth cap for spawning MCP tools. Stored as a
/// file under app-data (same file-bridge pattern as `max_agents`), default 3.
pub(crate) fn mcp_max_depth(app: &AppHandle) -> u32 {
    app.path()
        .app_data_dir()
        .ok()
        .and_then(|d| std::fs::read_to_string(d.join("burrow_mcp_max_depth")).ok())
        .and_then(|s| s.trim().parse::<u32>().ok())
        .unwrap_or(3)
}

/// Persist the recursion depth cap (Settings → published to a file the depth
/// check reads). Mirrors `set_max_agents`.
#[tauri::command]
fn set_burrow_mcp_max_depth(n: u32, app: AppHandle) {
    if let Ok(data) = app.path().app_data_dir() {
        let _ = std::fs::create_dir_all(&data);
        let _ = std::fs::write(data.join("burrow_mcp_max_depth"), n.to_string());
    }
}

/// Ephemeral result token when a caller wants `spawn({wait:true})` without
/// supplying one. Must be non-empty so the frontend wires up the capture hook.
fn mcp_gen_token() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0);
    format!("mcp{nanos:x}")
}

/// Write a request dir the frontend / Rust control path polls — the exact
/// files `bin/burrow` writes, so `take_spawn_requests` claims it identically.
/// `ready` is written LAST so a half-written request is never read.
fn mcp_write_request(app: &AppHandle, fields: &[(&str, &str)]) -> Result<(), String> {
    let reqdir = burrow_session_dir(app).ok_or("no session dir")?.join("requests");
    std::fs::create_dir_all(&reqdir).map_err(|e| e.to_string())?;
    let dir = reqdir.join(format!("req.{}", mcp_gen_token()));
    std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    for (k, v) in fields {
        std::fs::write(dir.join(k), v).map_err(|e| e.to_string())?;
    }
    std::fs::write(dir.join("ready"), "").map_err(|e| e.to_string())?;
    Ok(())
}

/// Block until `<session>/<token>.done` appears (or timeout), then return
/// `<token>.result`. Same convention as `burrow wait` / the Stop-hook capture.
fn mcp_block_on_done(app: &AppHandle, token: &str, timeout_secs: u64) -> Result<String, String> {
    let dir = burrow_session_dir(app).ok_or("no session dir")?;
    let done = dir.join(format!("{token}.done"));
    let result = dir.join(format!("{token}.result"));
    let mut waited = 0;
    while !done.exists() {
        std::thread::sleep(std::time::Duration::from_secs(1));
        waited += 1;
        if timeout_secs > 0 && waited >= timeout_secs {
            return Err(format!("timed out after {timeout_secs}s waiting for token {token}"));
        }
    }
    Ok(std::fs::read_to_string(&result).unwrap_or_default())
}

/// Reuse of `write_pty`'s daemon body for the `send_to_tab` tool.
fn mcp_write_pty(app: &AppHandle, id: u32, data: &[u8]) -> Result<(), String> {
    let daemon = app.state::<DaemonState>();
    let client = daemon.client().ok_or("daemon not connected")?;
    let enc = general_purpose::STANDARD.encode(data);
    client.cmd(json!({"cmd": "WritePty", "pty_id": id, "data": enc}))?;
    Ok(())
}

fn arg_str(args: &serde_json::Value, key: &str) -> Option<String> {
    args.get(key).and_then(|v| v.as_str()).map(|s| s.to_string())
}

/// A numeric-or-string arg (pty ids arrive as either) normalized to its string form.
fn arg_num_str(args: &serde_json::Value, key: &str) -> Option<String> {
    args.get(key)
        .and_then(|v| v.as_u64().map(|n| n.to_string()).or_else(|| v.as_str().map(|s| s.to_string())))
}

/// Dispatch an MCP tool to the same Rust logic the `burrow` CLI actions use.
/// `source` is the calling session's `BURROW_CWD` (routing key `ws`).
pub(crate) fn mcp_run_tool(
    app: &AppHandle,
    name: &str,
    args: serde_json::Value,
    source: &str,
) -> Result<serde_json::Value, String> {
    match name {
        "spawn" => {
            let cmd = arg_str(&args, "cmd").ok_or("missing 'cmd'")?;
            let cwd = arg_str(&args, "cwd").unwrap_or_else(|| source.to_string());
            let wait = args.get("wait").and_then(|v| v.as_bool()).unwrap_or(false);
            let mut token = arg_str(&args, "token").unwrap_or_default();
            if wait && token.is_empty() {
                token = mcp_gen_token();
            }
            mcp_write_request(app, &[
                ("cmd", &cmd),
                ("token", &token),
                ("cwd", &cwd),
                ("ws", source),
                ("tmux_win", ""),
            ])?;
            if wait {
                let timeout = args.get("timeout").and_then(|v| v.as_u64()).unwrap_or(600);
                let result = mcp_block_on_done(app, &token, timeout)?;
                Ok(json!({ "token": token, "status": "done", "result": result }))
            } else {
                Ok(json!({ "token": token, "status": "spawned" }))
            }
        }
        "create_worktree" => {
            let branch = arg_str(&args, "branch").ok_or("missing 'branch'")?;
            let base = arg_str(&args, "base").unwrap_or_default();
            let path = arg_str(&args, "path").unwrap_or_default();
            mcp_write_request(app, &[
                ("kind", "worktree"),
                ("branch", &branch),
                ("base", &base),
                ("path", &path),
                ("ws", source),
            ])?;
            Ok(json!({ "status": "requested", "branch": branch }))
        }
        "new_tab" => {
            let wsid = arg_str(&args, "ws").unwrap_or_default();
            let cmd = arg_str(&args, "cmd").unwrap_or_default();
            mcp_write_request(app, &[
                ("kind", "new-tab"),
                ("ws", source),
                ("wsid", &wsid),
                ("cmd", &cmd),
            ])?;
            Ok(json!({ "status": "requested" }))
        }
        "focus_tab" => {
            let tabid = arg_str(&args, "tabid")
                .or_else(|| args.get("tabid").and_then(|v| v.as_u64()).map(|n| n.to_string()))
                .ok_or("missing 'tabid'")?;
            mcp_write_request(app, &[("kind", "focus-tab"), ("ws", source), ("tabid", &tabid)])?;
            Ok(json!({ "status": "requested" }))
        }
        "focus_workspace" => {
            let wsid = arg_str(&args, "wsid").ok_or("missing 'wsid'")?;
            mcp_write_request(app, &[("kind", "focus-workspace"), ("ws", source), ("wsid", &wsid)])?;
            Ok(json!({ "status": "requested" }))
        }
        "list_workspaces" => {
            let db = app.state::<DbState>();
            let text = query_workspaces_tsv(&db.conn.lock().unwrap());
            let rows: Vec<serde_json::Value> = text
                .lines()
                .filter_map(|l| {
                    let mut p = l.splitn(3, '\t');
                    Some(json!({ "id": p.next()?, "name": p.next()?, "path": p.next()? }))
                })
                .collect();
            Ok(json!({ "workspaces": rows }))
        }
        "list_tabs" => {
            let db = app.state::<DbState>();
            let conn = db.conn.lock().unwrap();
            let id: Option<i64> = match arg_str(&args, "ws") {
                Some(w) => w.parse::<i64>().ok(),
                None => conn
                    .query_row("SELECT id FROM workspaces WHERE path = ?1", rusqlite::params![source], |r| r.get(0))
                    .ok(),
            };
            let text = id.map(|i| query_tabs_tsv(app, &conn, i)).unwrap_or_default();
            let rows: Vec<serde_json::Value> = text
                .lines()
                .filter_map(|l| {
                    let mut p = l.splitn(3, '\t');
                    Some(json!({
                        "ptyId": p.next()?,
                        "title": p.next().unwrap_or(""),
                        "status": p.next().unwrap_or("idle"),
                    }))
                })
                .collect();
            Ok(json!({ "tabs": rows }))
        }
        "send_to_tab" => {
            let tabid = args
                .get("tabid")
                .and_then(|v| v.as_u64().or_else(|| v.as_str().and_then(|s| s.parse().ok())))
                .ok_or("missing/invalid 'tabid'")? as u32;
            let text = arg_str(&args, "text").ok_or("missing 'text'")?;
            mcp_write_pty(app, tabid, text.as_bytes())?;
            Ok(json!({ "ok": true }))
        }
        "wait_result" => {
            let token = arg_str(&args, "token").ok_or("missing 'token'")?;
            let timeout = args.get("timeout").and_then(|v| v.as_u64()).unwrap_or(600);
            let result = mcp_block_on_done(app, &token, timeout)?;
            Ok(json!({ "token": token, "result": result }))
        }
        "collect_results" => {
            let dir = burrow_session_dir(app).ok_or("no session dir")?;
            let tokens = args.get("tokens").and_then(|v| v.as_array()).cloned().unwrap_or_default();
            let mut out = serde_json::Map::new();
            for t in tokens {
                let Some(tok) = t.as_str() else { continue };
                let done = dir.join(format!("{tok}.done")).exists();
                let result = std::fs::read_to_string(dir.join(format!("{tok}.result"))).unwrap_or_default();
                out.insert(tok.to_string(), json!({ "done": done, "result": result }));
            }
            Ok(json!({ "results": out }))
        }
        "worktree_remove" => {
            let branch = arg_str(&args, "branch").unwrap_or_default();
            let path = arg_str(&args, "path").unwrap_or_default();
            let force = args.get("force").and_then(|v| v.as_bool()).unwrap_or(false);
            let db = app.state::<DbState>();
            let msg = remove_worktree_by(&db, source, &branch, &path, force)?;
            let _ = app.emit("workspaces-changed", ());
            Ok(json!({ "status": "removed", "message": msg }))
        }
        "pr_create" | "pr_list" | "pr_view" | "pr_merge" => {
            let run_dir = arg_str(&args, "cwd").unwrap_or_else(|| source.to_string());
            let gh_args: Vec<String> = match name {
                "pr_create" => {
                    let mut a = vec![
                        "pr".into(), "create".into(),
                        "--title".into(), arg_str(&args, "title").ok_or("missing 'title'")?,
                        "--body".into(), arg_str(&args, "body").ok_or("missing 'body'")?,
                    ];
                    if let Some(b) = arg_str(&args, "base") { a.push("--base".into()); a.push(b); }
                    if let Some(h) = arg_str(&args, "head") { a.push("--head".into()); a.push(h); }
                    a
                }
                "pr_list" => {
                    let mut a = vec!["pr".into(), "list".into()];
                    if let Some(s) = arg_str(&args, "state") { a.push("--state".into()); a.push(s); }
                    a
                }
                "pr_view" => {
                    let n = args.get("number").and_then(|v| v.as_u64()).ok_or("missing 'number'")?;
                    vec!["pr".into(), "view".into(), n.to_string()]
                }
                _ => {
                    let n = args.get("number").and_then(|v| v.as_u64()).ok_or("missing 'number'")?;
                    let squash = args.get("squash").and_then(|v| v.as_bool()).unwrap_or(false);
                    vec!["pr".into(), "merge".into(), n.to_string(),
                         if squash { "--squash".into() } else { "--merge".into() }]
                }
            };
            let argref: Vec<&str> = gh_args.iter().map(|s| s.as_str()).collect();
            let out = gh_in(&run_dir, &argref)?;
            Ok(json!({ "output": out }))
        }
        "git_status" | "git_log" | "git_diff" => {
            let run_dir = arg_str(&args, "cwd").unwrap_or_else(|| source.to_string());
            let git_args: Vec<String> = match name {
                "git_status" => vec!["status".into()],
                "git_log" => {
                    let n = args.get("n").and_then(|v| v.as_u64()).unwrap_or(20);
                    vec!["log".into(), "--oneline".into(), format!("-{n}")]
                }
                _ => {
                    if args.get("staged").and_then(|v| v.as_bool()).unwrap_or(false) {
                        vec!["diff".into(), "--cached".into()]
                    } else {
                        vec!["diff".into()]
                    }
                }
            };
            let argref: Vec<&str> = git_args.iter().map(|s| s.as_str()).collect();
            Ok(json!({ "output": git_in(&run_dir, &argref)? }))
        }
        "run" => {
            let cmd = arg_str(&args, "cmd").ok_or("missing 'cmd'")?;
            let run_dir = arg_str(&args, "cwd").unwrap_or_else(|| source.to_string());
            let timeout = args.get("timeout").and_then(|v| v.as_u64()).unwrap_or(30);
            Ok(json!({ "output": run_shell_capture(&run_dir, &cmd, timeout) }))
        }
        "tab_rename" => {
            let tabid = arg_num_str(&args, "tabid").ok_or("missing 'tabid'")?;
            let name_arg = arg_str(&args, "name").ok_or("missing 'name'")?;
            mcp_write_request(app, &[
                ("kind", "tab-rename"), ("ws", source), ("tabid", &tabid), ("name", &name_arg),
            ])?;
            Ok(json!({ "status": "requested" }))
        }
        "tab_close" => {
            let tabid = arg_num_str(&args, "tabid").ok_or("missing 'tabid'")?;
            let force = if args.get("force").and_then(|v| v.as_bool()).unwrap_or(false) { "1" } else { "" };
            mcp_write_request(app, &[
                ("kind", "tab-close"), ("ws", source), ("tabid", &tabid), ("force", force),
            ])?;
            Ok(json!({ "status": "requested" }))
        }
        "workspace_create" => {
            let name_arg = arg_str(&args, "name").ok_or("missing 'name'")?;
            let path = arg_str(&args, "path").ok_or("missing 'path'")?;
            mcp_write_request(app, &[
                ("kind", "workspace-create"), ("ws", source), ("name", &name_arg), ("path", &path),
            ])?;
            Ok(json!({ "status": "requested", "path": path }))
        }
        "board_list" => {
            let db = app.state::<DbState>();
            let conn = db.conn.lock().unwrap();
            let rid = resolve_repo_workspace_id(&conn, source).ok_or("could not resolve this repo's workspace")?;
            let column = arg_str(&args, "column").unwrap_or_default();
            let text = query_board_tasks_tsv(&conn, rid, &column);
            let rows: Vec<serde_json::Value> = text
                .lines()
                .filter_map(|l| {
                    let mut p = l.splitn(4, '\t');
                    Some(json!({ "id": p.next()?, "column": p.next()?, "title": p.next()?, "status": p.next().unwrap_or("") }))
                })
                .collect();
            Ok(json!({ "tasks": rows }))
        }
        "board_create" => {
            let title = arg_str(&args, "title").ok_or("missing 'title'")?;
            let description = arg_str(&args, "description").unwrap_or_default();
            let agent = arg_str(&args, "agent").unwrap_or_default();
            let model = arg_str(&args, "model").unwrap_or_default();
            let use_worktree = if args.get("worktree").and_then(|v| v.as_bool()) == Some(false) { 0 } else { 1 };
            let db = app.state::<DbState>();
            let id = {
                let conn = db.conn.lock().unwrap();
                let rid = resolve_repo_workspace_id(&conn, source).ok_or("could not resolve this repo's workspace")?;
                let id = format!("task_{}", now_millis());
                let now = now_millis();
                conn.execute(
                    "INSERT INTO mission_tasks (
                        id, created_at, repo_workspace_id, board_column, title,
                        description, agent_kind, model, use_worktree, board_order, updated_at
                     ) VALUES (?1, ?2, ?3, 'backlog', ?4, ?5, ?6, ?7, ?8, 0, ?9)",
                    rusqlite::params![id, now, rid, title,
                        if description.is_empty() { None } else { Some(description) },
                        if agent.is_empty() { None } else { Some(agent) },
                        if model.is_empty() { None } else { Some(model) },
                        use_worktree, now],
                ).map_err(|e| e.to_string())?;
                id
            };
            let _ = app.emit("board-task-moved", json!({ "taskId": id, "column": "backlog" }));
            Ok(json!({ "taskId": id, "column": "backlog" }))
        }
        "board_move" => {
            let task_id = arg_str(&args, "taskId").ok_or("missing 'taskId'")?;
            let column = arg_str(&args, "column").ok_or("missing 'column'")?;
            if !BOARD_COLUMNS.contains(&column.as_str()) {
                return Err(format!("invalid column '{column}' (must be one of backlog|todo|in_progress|for_review|done)"));
            }
            if column == "done" {
                return Err("agents cannot move a task to 'done' — move it to 'for_review' and let a human/Manager mark it done".to_string());
            }
            if column == "todo" {
                // Backlog→Todo needs worktree creation + agent spawn, which only
                // exists client-side — route through the same request-dir the
                // `burrow board-move` CLI writes, so Terminal.vue's poll picks it
                // up identically (see the CLI's "board-move" arm in take_spawn_requests).
                mcp_write_request(app, &[("kind", "board-move"), ("ws", source), ("wsid", &task_id), ("tabid", &column)])?;
                Ok(json!({ "status": "requested", "taskId": task_id, "column": column }))
            } else {
                let db = app.state::<DbState>();
                {
                    let conn = db.conn.lock().unwrap();
                    move_board_task_db(&conn, &task_id, &column, 0.0)?;
                }
                let _ = app.emit("board-task-moved", json!({ "taskId": task_id.clone(), "column": column.clone() }));
                Ok(json!({ "status": "ok", "taskId": task_id, "column": column }))
            }
        }
        other => Err(format!("Unknown tool: {other}")),
    }
}

/// Build the `--mcp-config` JSON injected into a spawned agent so it gets the
/// Burrow MCP tools. The child's `BURROW_MCP_DEPTH` is `depth + 1` (the
/// increment site for the recursion cap).
pub(crate) fn build_burrow_mcp_config(app: &AppHandle, ws_cwd: &str, depth: u32) -> String {
    let command = std::env::current_exe()
        .ok()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_else(|| "burrow".to_string());
    let socket = burrow_mcp_socket::socket_path(app)
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_default();
    let token = burrow_mcp_socket::socket_token(app).unwrap_or_default();
    json!({ "mcpServers": { "burrow": {
        "type": "stdio",
        "command": command,
        "args": [burrow_mcp_core::BURROW_MCP_STDIO_ARG],
        "env": {
            burrow_mcp_core::BURROW_MCP_SOCKET_ENV: socket,
            burrow_mcp_core::BURROW_MCP_TOKEN_ENV: token,
            burrow_mcp_core::BURROW_MCP_SESSION_ENV: ws_cwd,
            burrow_mcp_core::BURROW_MCP_DEPTH_ENV: (depth + 1).to_string(),
        }
    } } })
    .to_string()
}

/// True when a spawn command line launches Claude Code (first token is `claude`
/// or `.../claude`). Only these tabs get the Burrow MCP `--mcp-config`.
fn is_claude_cmd(cmd: &str) -> bool {
    cmd.split_whitespace()
        .next()
        .map(|prog| prog.rsplit('/').next().unwrap_or(prog) == "claude")
        .unwrap_or(false)
}

/// Append Burrow's `--mcp-config` to a spawned claude command so the sub-agent
/// gets the MCP tools. Non-destructive: Claude Code merges repeated
/// `--mcp-config` flags, so an existing one is left intact and ours is added
/// alongside rather than clobbering it. `ws_cwd` is the tab's workspace (routing
/// key); depth defaults to 0 (the desktop app can't read the spawning PTY's env,
/// and `build_burrow_mcp_config` increments to depth+1 for the child).
fn inject_burrow_mcp_config(app: &AppHandle, cmd: &str, ws_cwd: &str) -> String {
    if !is_claude_cmd(cmd) {
        return cmd.to_string();
    }
    let depth = burrow_mcp_core::current_depth(); // app-process env → 0 in practice
    let cfg = build_burrow_mcp_config(app, ws_cwd, depth);
    // Single-quote the JSON for the shell; escape any embedded single quotes.
    let quoted = format!("'{}'", cfg.replace('\'', "'\\''"));
    format!("{cmd} --mcp-config {quoted}")
}

/// Frontend button-launch path: return the `--mcp-config '<json>'` flag to splice
/// into a claude launch's argv (XTerm prepends it alongside `--settings`). The
/// `take_spawn_requests` delegation path uses `inject_burrow_mcp_config` instead;
/// this covers agent-button and other in-app claude tab launches. depth 0 = a
/// human/button launch is the top of the chain (child gets depth 1).
/// Thin bridge onto the command-dispatch router. Lets any transport (or the
/// devtools console) exercise `dispatch_command` before the HTTP/WS layer exists —
/// `invoke('dispatch', {command, args})` should return the same result as calling
/// the underlying `#[tauri::command]` directly. This is the seam a future
/// HTTP/WS/MCP transport routes through.
#[tauri::command]
async fn dispatch(
    command: String,
    args: serde_json::Value,
    app: AppHandle,
) -> Result<serde_json::Value, String> {
    crate::dispatch::dispatch_command(&app, &command, args).await
}

#[tauri::command]
fn burrow_mcp_flag(cwd: String, app: AppHandle) -> String {
    let cfg = build_burrow_mcp_config(&app, &cwd, 0);
    let quoted = format!("'{}'", cfg.replace('\'', "'\\''"));
    format!("--mcp-config {quoted}")
}

/// Build the Burrow MCP server entry in **ACP format** for an `acp_start` session's
/// `session/new`|`session/load` `mcpServers` array. ACP uses a flat
/// `{name,command,args,env:[{name,value}]}` shape (env is an array of objects),
/// unlike Claude's `--mcp-config` object map. Returns `Null` when the socket isn't
/// available (browser-only dev) so the caller can filter it out. depth 0 = top of
/// the chain; the child proxy sees `BURROW_MCP_DEPTH=1`.
fn build_burrow_acp_server(app: &AppHandle, ws_cwd: &str) -> serde_json::Value {
    let socket = match burrow_mcp_socket::socket_path(app) {
        Some(p) => p.to_string_lossy().to_string(),
        None => return serde_json::Value::Null,
    };
    let command = std::env::current_exe()
        .ok()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_else(|| "burrow".to_string());
    let token = burrow_mcp_socket::socket_token(app).unwrap_or_default();
    json!({
        "name": "burrow",
        "command": command,
        "args": [burrow_mcp_core::BURROW_MCP_STDIO_ARG],
        "env": [
            { "name": burrow_mcp_core::BURROW_MCP_SOCKET_ENV, "value": socket },
            { "name": burrow_mcp_core::BURROW_MCP_TOKEN_ENV, "value": token },
            { "name": burrow_mcp_core::BURROW_MCP_SESSION_ENV, "value": ws_cwd },
            { "name": burrow_mcp_core::BURROW_MCP_DEPTH_ENV, "value": "1" },
        ]
    })
}

/// Merge Burrow's MCP server into a `{"mcpServers":{...}}` blob so a `claude_start`
/// session (Manager float chat + native ClaudeChat, both stream-json) gets the
/// spawn/worktree/pr tools alongside the user's own MCP servers. depth 0 = top of
/// the chain (spawned children get depth 1). Non-destructive: user servers kept.
fn merge_burrow_into_mcp(app: &AppHandle, base: String, ws_cwd: &str) -> String {
    let burrow = build_burrow_mcp_config(app, ws_cwd, 0);
    let mut v: serde_json::Value =
        serde_json::from_str(&base).unwrap_or_else(|_| serde_json::json!({ "mcpServers": {} }));
    let bv: serde_json::Value = match serde_json::from_str(&burrow) {
        Ok(x) => x,
        Err(_) => return base,
    };
    if let (Some(dst), Some(src)) = (
        v.get_mut("mcpServers").and_then(|m| m.as_object_mut()),
        bv.get("mcpServers").and_then(|m| m.as_object()),
    ) {
        for (k, val) in src {
            dst.insert(k.clone(), val.clone());
        }
    }
    serde_json::to_string(&v).unwrap_or(base)
}

#[tauri::command]
fn take_spawn_requests(cwd: String, app: AppHandle, db: State<DbState>) -> Vec<SpawnRequest> {
    let mut out = Vec::new();
    let Some(reqdir) = burrow_session_dir(&app).map(|d| d.join("requests")) else {
        return out;
    };
    let Ok(entries) = std::fs::read_dir(&reqdir) else { return out };
    for e in entries.flatten() {
        let d = e.path();
        if !d.is_dir() || !d.join("ready").exists() { continue; }
        let read = |name: &str| std::fs::read_to_string(d.join(name)).unwrap_or_default();
        let ws = read("ws");          // spawning workspace (request origin)

        // Control commands (list-workspaces/list-tabs/focus-workspace/focus-tab/new-tab)
        // all route to the ORIGIN workspace — the one the issuing agent runs in
        // (ws == cwd), which is by definition mounted and polling, so there's no
        // double-claim race and no "target not mounted" gap. READ commands are answered
        // right here in Rust (DB query → <token>.result file the CLI polls); UI ACTIONS
        // are pushed to the frontend for Terminal.vue to perform.
        let kind_peek = read("kind");
        if matches!(
            kind_peek.as_str(),
            "list-workspaces" | "list-tabs" | "focus-workspace" | "focus-tab" | "new-tab"
                | "worktree-remove" | "pr-create" | "pr-list" | "pr-view" | "pr-merge"
                | "tab-rename" | "tab-close" | "workspace-create"
                | "git-status" | "git-log" | "git-diff" | "run" | "tab-output"
                | "diagram"
                | "board-move" | "board-list" | "board-create" | "board-attach"
        ) {
            if ws != cwd { continue; }
            let token = read("token");
            let wsid = read("wsid");
            let tabid = read("tabid");
            let cmd = read("cmd");
            match kind_peek.as_str() {
                "list-workspaces" => {
                    let text = query_workspaces_tsv(&db.conn.lock().unwrap());
                    write_control_result(&app, &token, &text);
                }
                "list-tabs" => {
                    let text = {
                        let conn = db.conn.lock().unwrap();
                        // Resolve target workspace id: explicit --ws, else the origin ws (by path).
                        let target_id: Option<i64> = if !wsid.is_empty() {
                            wsid.parse::<i64>().ok()
                        } else {
                            conn.query_row(
                                "SELECT id FROM workspaces WHERE path = ?1",
                                rusqlite::params![ws],
                                |r| r.get::<_, i64>(0),
                            ).ok()
                        };
                        target_id.map(|id| query_tabs_tsv(&app, &conn, id)).unwrap_or_default()
                    };
                    write_control_result(&app, &token, &text);
                }
                "worktree-remove" => {
                    // Manager deletes a worktree (git worktree + Burrow row). Resolved
                    // within the origin repo by branch or path. Read command — answered
                    // in Rust, result polled by the CLI.
                    let branch = read("branch");
                    let path = read("path");
                    let force = read("force") == "1";
                    let text = match remove_worktree_by(&db, &ws, &branch, &path, force) {
                        Ok(m) => m,
                        Err(e) => format!("error: {e}"),
                    };
                    if !text.starts_with("error:") {
                        let _ = app.emit("workspaces-changed", ());
                    }
                    write_control_result(&app, &token, &text);
                }
                "pr-create" | "pr-list" | "pr-view" | "pr-merge" => {
                    // PR management via the `gh` CLI. Run in the optional --cwd dir
                    // (a worktree path) so the right branch is in context, else the
                    // origin repo. Read command — answered in Rust.
                    let path = read("path");
                    let run_dir = if path.is_empty() { ws.clone() } else { path };
                    let gh_args: Vec<String> = match kind_peek.as_str() {
                        "pr-create" => {
                            let title = read("title");
                            let body = read("body");
                            let base = read("base");
                            let head = read("branch");
                            let mut a = vec!["pr".into(), "create".into(),
                                "--title".into(), title, "--body".into(), body];
                            if !base.is_empty() { a.push("--base".into()); a.push(base); }
                            if !head.is_empty() { a.push("--head".into()); a.push(head); }
                            a
                        }
                        "pr-list" => {
                            let state = read("state");
                            let mut a = vec!["pr".into(), "list".into()];
                            if !state.is_empty() { a.push("--state".into()); a.push(state); }
                            a
                        }
                        "pr-view" => vec!["pr".into(), "view".into(), read("prnum")],
                        // pr-merge
                        _ => {
                            let mut a = vec!["pr".into(), "merge".into(), read("prnum")];
                            a.push(if read("squash") == "1" { "--squash".into() } else { "--merge".into() });
                            a
                        }
                    };
                    let argref: Vec<&str> = gh_args.iter().map(|s| s.as_str()).collect();
                    let text = match gh_in(&run_dir, &argref) {
                        Ok(o) => if o.trim().is_empty() { "ok".to_string() } else { o },
                        Err(e) => format!("error: {e}"),
                    };
                    write_control_result(&app, &token, &text);
                }
                "git-status" | "git-log" | "git-diff" => {
                    // Git read commands — run in the optional --cwd dir (a repo or
                    // worktree path), else the origin workspace. Answered in Rust.
                    let path = read("path");
                    let run_dir = if path.is_empty() { ws.clone() } else { path };
                    let git_args: Vec<String> = match kind_peek.as_str() {
                        "git-status" => vec!["status".into()],
                        "git-log" => {
                            // -N (default 20); reject a non-numeric --n so it can't
                            // smuggle extra git flags.
                            let n = read("n");
                            let n = if n.trim().chars().all(|c| c.is_ascii_digit()) && !n.trim().is_empty() {
                                n.trim().to_string()
                            } else { "20".to_string() };
                            vec!["log".into(), "--oneline".into(), format!("-{n}")]
                        }
                        // git-diff
                        _ => {
                            if read("staged") == "1" { vec!["diff".into(), "--cached".into()] }
                            else { vec!["diff".into()] }
                        }
                    };
                    let argref: Vec<&str> = git_args.iter().map(|s| s.as_str()).collect();
                    let text = match git_in(&run_dir, &argref) {
                        Ok(o) => if o.trim().is_empty() { "(no output)".to_string() } else { o },
                        Err(e) => format!("error: {e}"),
                    };
                    write_control_result(&app, &token, &text);
                }
                "run" => {
                    // Run an arbitrary shell command (read-only by convention) and
                    // capture stdout+stderr. 30s timeout. Lets the Manager grep/find/
                    // cat without spawning a whole agent tab.
                    let path = read("path");
                    let run_dir = if path.is_empty() { ws.clone() } else { path };
                    let text = if cmd.trim().is_empty() {
                        "error: no command".to_string()
                    } else {
                        run_shell_capture(&run_dir, &cmd, 30)
                    };
                    write_control_result(&app, &token, &text);
                }
                "tab-output" => {
                    // No addressable scrollback buffer is exposed to the CLI — the
                    // script already explains this and exits 1 before blocking, so
                    // this arm is just a safety net if a request still lands.
                    write_control_result(
                        &app, &token,
                        "error: tab-output not supported (no addressable PTY scrollback)",
                    );
                }
                "diagram" => {
                    let content = read("content");
                    out.push(SpawnRequest {
                        kind: kind_peek.clone(),
                        cmd: String::new(),
                        token: String::new(),
                        cwd: String::new(),
                        branch: String::new(),
                        base: String::new(),
                        tmux_win: String::new(),
                        wsid: String::new(),
                        tabid: String::new(),
                        content,
                    });
                }
                "board-list" => {
                    // Read command, answered entirely in Rust — pure DB read, no
                    // frontend involvement (same "no frontend" model as
                    // list-workspaces/git-status).
                    let column = read("column");
                    let text = {
                        let conn = db.conn.lock().unwrap();
                        match resolve_repo_workspace_id(&conn, &ws) {
                            Some(rid) => query_board_tasks_tsv(&conn, rid, &column),
                            None => "error: could not resolve this repo's workspace".to_string(),
                        }
                    };
                    write_control_result(&app, &token, &text);
                }
                "board-create" => {
                    // Creates a new Backlog-column task row. Pure DB write — task
                    // creation touches no PTY/UI, so (unlike board-move → todo) it
                    // needs no frontend round-trip.
                    let title = read("title");
                    let description = read("description");
                    let agent = read("agent");
                    let model = read("model");
                    let use_worktree = if read("no_worktree") == "1" { 0 } else { 1 };
                    let text = if title.trim().is_empty() {
                        "error: --title is required".to_string()
                    } else {
                        let conn = db.conn.lock().unwrap();
                        match resolve_repo_workspace_id(&conn, &ws) {
                            Some(rid) => {
                                let id = format!("task_{}", now_millis());
                                let now = now_millis();
                                let res = conn.execute(
                                    "INSERT INTO mission_tasks (
                                        id, created_at, repo_workspace_id, board_column, title,
                                        description, agent_kind, model, use_worktree, board_order, updated_at
                                     ) VALUES (?1, ?2, ?3, 'backlog', ?4, ?5, ?6, ?7, ?8, 0, ?9)",
                                    rusqlite::params![id, now, rid, title,
                                        if description.is_empty() { None } else { Some(description) },
                                        if agent.is_empty() { None } else { Some(agent) },
                                        if model.is_empty() { None } else { Some(model) },
                                        use_worktree, now],
                                );
                                match res {
                                    Ok(_) => id,
                                    Err(e) => format!("error: {e}"),
                                }
                            }
                            None => "error: could not resolve this repo's workspace".to_string(),
                        }
                    };
                    if !text.starts_with("error:") {
                        let _ = app.emit("board-task-moved", json!({ "taskId": text, "column": "backlog" }));
                    }
                    write_control_result(&app, &token, &text);
                }
                "board-move" => {
                    // Column-move policy split (docs/plans/mission-control-kanban.md
                    // §9.2, confirmed by the user): a move INTO 'todo' is the
                    // Backlog→Todo transition, which needs worktree creation +
                    // agent spawn — logic that only exists client-side — so it MUST
                    // go through the frontend (pushed as a SpawnRequest, like
                    // focus-tab). Every other destination column is a pure DB
                    // write, answered right here with no frontend involvement.
                    // Agents/CLI may never move a card to 'done' — that's reserved
                    // for a human/Manager via the UI's own `move_board_task` Tauri
                    // call (not reachable from this file-based CLI transport).
                    let task_id = read("task_id");
                    let column = read("column");
                    if task_id.is_empty() || column.is_empty() {
                        write_control_result(&app, &token, "error: task_id and column are required");
                    } else if !BOARD_COLUMNS.contains(&column.as_str()) {
                        write_control_result(&app, &token, &format!("error: invalid column '{column}' (must be one of backlog|todo|in_progress|for_review|done)"));
                    } else if column == "done" {
                        write_control_result(
                            &app, &token,
                            "error: agents cannot move a task to 'done' — move it to 'for_review' and let a human/Manager mark it done",
                        );
                    } else if column == "todo" {
                        write_control_result(&app, &token, "requested: frontend will create the worktree (if any) and spawn the agent, then move this card to 'todo'");
                        out.push(SpawnRequest {
                            kind: "board-move".to_string(),
                            cmd: String::new(),
                            token: String::new(),
                            cwd: String::new(),
                            branch: String::new(),
                            base: String::new(),
                            tmux_win: String::new(),
                            wsid: task_id,
                            tabid: column,
                            content: String::new(),
                        });
                    } else {
                        let text = {
                            let conn = db.conn.lock().unwrap();
                            match move_board_task_db(&conn, &task_id, &column, 0.0) {
                                Ok(()) => "ok".to_string(),
                                Err(e) => format!("error: {e}"),
                            }
                        };
                        if !text.starts_with("error:") {
                            let _ = app.emit("board-task-moved", json!({ "taskId": task_id, "column": column }));
                        }
                        write_control_result(&app, &token, &text);
                    }
                }
                "board-attach" => {
                    // Optional/phase-2 (docs/plans/mission-control-kanban.md §3):
                    // let an agent attach a file it produced (e.g. a screenshot)
                    // back onto its own card. Pure Rust — reads the file straight
                    // off disk (the agent already has it locally), no base64
                    // round-trip needed like the frontend's write_task_attachment.
                    let task_id = read("task_id");
                    let image_path = read("image_path");
                    let text = if task_id.is_empty() || image_path.is_empty() {
                        "error: task_id and image_path are required".to_string()
                    } else {
                        match std::fs::read(&image_path) {
                            Ok(bytes) => {
                                let mime = match std::path::Path::new(&image_path).extension().and_then(|e| e.to_str()) {
                                    Some("png") => "image/png",
                                    Some("jpg") | Some("jpeg") => "image/jpeg",
                                    Some("gif") => "image/gif",
                                    Some("webp") => "image/webp",
                                    Some("svg") => "image/svg+xml",
                                    _ => "application/octet-stream",
                                };
                                let data_dir = app.path().app_data_dir();
                                match data_dir {
                                    Ok(data_dir) => {
                                        let dir = data_dir.join("attachments").join(&task_id);
                                        match std::fs::create_dir_all(&dir) {
                                            Ok(()) => {
                                                let conn = db.conn.lock().unwrap();
                                                let next_ord: i64 = conn.query_row(
                                                    "SELECT COALESCE(MAX(ord), -1) + 1 FROM task_attachments WHERE task_id = ?1",
                                                    rusqlite::params![task_id], |r| r.get(0),
                                                ).unwrap_or(0);
                                                let ext = attachment_ext_for_mime(mime);
                                                let dest = dir.join(format!("{next_ord}.{ext}"));
                                                match std::fs::write(&dest, &bytes) {
                                                    Ok(()) => {
                                                        let dest_str = dest.to_string_lossy().to_string();
                                                        let created_at = now_millis();
                                                        match conn.execute(
                                                            "INSERT INTO task_attachments (task_id, ord, mime_type, file_path, created_at) VALUES (?1, ?2, ?3, ?4, ?5)",
                                                            rusqlite::params![task_id, next_ord, mime, dest_str, created_at],
                                                        ) {
                                                            Ok(_) => dest_str,
                                                            Err(e) => format!("error: {e}"),
                                                        }
                                                    }
                                                    Err(e) => format!("error: {e}"),
                                                }
                                            }
                                            Err(e) => format!("error: {e}"),
                                        }
                                    }
                                    Err(e) => format!("error: {e}"),
                                }
                            }
                            Err(e) => format!("error: cannot read {image_path}: {e}"),
                        }
                    };
                    write_control_result(&app, &token, &text);
                }
                "tab-rename" | "tab-close" | "workspace-create" => {
                    // UI actions carrying extra fields: tab-rename uses name→cmd +
                    // tabid; tab-close uses tabid + force→base; workspace-create uses
                    // name→cmd + path→cwd. Pushed to the frontend like focus-*.
                    let name = read("name");
                    let path = read("path");
                    let force = read("force");
                    out.push(SpawnRequest {
                        kind: kind_peek.clone(),
                        cmd: name,
                        token: String::new(),
                        cwd: path,
                        branch: String::new(),
                        base: force,
                        tmux_win: String::new(),
                        wsid,
                        tabid,
                        content: String::new(),
                    });
                }
                _ => {
                    // focus-workspace / focus-tab / new-tab → frontend UI action.
                    out.push(SpawnRequest {
                        kind: kind_peek.clone(),
                        cmd,
                        token: String::new(),
                        cwd: String::new(),
                        branch: String::new(),
                        base: String::new(),
                        tmux_win: String::new(),
                        wsid,
                        tabid,
                        content: String::new(),
                    });
                }
            }
            let _ = std::fs::remove_dir_all(&d);
            continue;
        }

        let newcwd = read("cwd");     // dir the new tab should run in (may be a worktree)
        // Route the tab to the workspace it will actually run in: prefer the target
        // dir `newcwd` when that dir is itself a workspace (e.g. a worktree), so the
        // tab nests under it; otherwise fall back to the spawning workspace `ws`
        // (covers `spawn --cwd <arbitrary dir>` where the dir is not its own
        // workspace, and `worktree` requests where newcwd is empty). The DB is the
        // arbiter so a single Terminal claims each request — no double-claim race.
        let target = if newcwd.is_empty() { ws.clone() } else { newcwd.clone() };
        let target_is_ws = {
            let conn = db.conn.lock().unwrap();
            conn.query_row(
                "SELECT 1 FROM workspaces WHERE path = ?1",
                rusqlite::params![target],
                |_| Ok(()),
            ).is_ok()
        };
        let claimant = if target_is_ws { &target } else { &ws };
        if *claimant != cwd { continue; }
        // `kind` is absent on legacy `spawn` requests → default to "spawn".
        let kind = { let k = read("kind"); if k.is_empty() { "spawn".to_string() } else { k } };
        let cmd = read("cmd");
        let token = read("token");
        let branch = read("branch");
        let base = read("base");
        let tmux_win = read("tmux_win");
        let _ = std::fs::remove_dir_all(&d);
        match kind.as_str() {
            "worktree" if !branch.is_empty() => {
                out.push(SpawnRequest { kind, cmd: String::new(), token: String::new(), cwd: newcwd, branch, base, tmux_win: String::new(), wsid: String::new(), tabid: String::new(), content: String::new() });
            }
            _ if !cmd.is_empty() => {
                if !token.is_empty() {
                    let origin: State<SpawnTokenOrigin> = app.state();
                    origin.map.lock().unwrap().insert(token.clone(), ws.clone());
                }
                // Give claude sub-agent tabs the Burrow MCP tools. Routing key is
                // the tab's own dir (newcwd if set, else the spawning workspace).
                let tab_cwd = if newcwd.is_empty() { ws.clone() } else { newcwd.clone() };
                let cmd = inject_burrow_mcp_config(&app, &cmd, &tab_cwd);
                out.push(SpawnRequest { kind: "spawn".to_string(), cmd, token, cwd: newcwd, branch: String::new(), base: String::new(), tmux_win, wsid: String::new(), tabid: String::new(), content: String::new() });
            }
            _ => {}
        }
    }
    out
}

// ── Git command ───────────────────────────────────────────────────────────────

#[derive(Serialize)]
struct GitOutput { stdout: String, stderr: String, code: i32 }

fn git_binary() -> &'static str {
    for p in &["/usr/bin/git", "/usr/local/bin/git", "/opt/homebrew/bin/git"] {
        if std::path::Path::new(p).exists() { return p; }
    }
    "/usr/bin/git"
}

// Async so the blocking `git` subprocess runs on a blocking-pool thread instead
// of a Tauri command worker (Tauri v2 runs sync command handlers on the main
// thread). Opening/switching a workspace fires git status/diff/log/branch via
// run_git; running them inline blocked the main thread → frozen UI + spinning
// beachball. spawn_blocking keeps git off the main thread.
#[tauri::command]
async fn run_git(cwd: String, args: Vec<String>) -> GitOutput {
    let git = git_binary();
    tauri::async_runtime::spawn_blocking(move || {
        match std::process::Command::new(git).args(&args).current_dir(&cwd).output() {
            Ok(out) => GitOutput {
                stdout: String::from_utf8_lossy(&out.stdout).into_owned(),
                stderr: String::from_utf8_lossy(&out.stderr).into_owned(),
                code: out.status.code().unwrap_or(-1),
            },
            Err(e) => GitOutput { stdout: String::new(), stderr: e.to_string(), code: -1 },
        }
    })
    .await
    .unwrap_or_else(|e| GitOutput { stdout: String::new(), stderr: e.to_string(), code: -1 })
}

// ── Format document ───────────────────────────────────────────────────────────

// Pipe `content` into `bin args...`'s stdin, return stdout if it exits 0. Used
// for both rustfmt and prettier — same stdin-in/stdout-out contract.
fn run_formatter(bin: &Path, args: &[&str], content: &str) -> Result<String, String> {
    use std::io::Write;
    let mut child = std::process::Command::new(bin)
        .args(args)
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .map_err(|e| e.to_string())?;
    child.stdin.take().unwrap().write_all(content.as_bytes()).map_err(|e| e.to_string())?;
    let out = child.wait_with_output().map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(String::from_utf8_lossy(&out.stderr).into_owned());
    }
    Ok(String::from_utf8_lossy(&out.stdout).into_owned())
}

// Project-local prettier first (picks up the project's config/plugins), else
// `npx --no-install` (already-installed global via npx cache), else a bare
// `prettier` on PATH. Mirrors resolve_lsp_bin's search-known-locations style.
fn resolve_prettier(cwd: &str) -> Option<(PathBuf, Vec<String>)> {
    let local = Path::new(cwd).join("node_modules/.bin/prettier");
    if local.exists() {
        return Some((local, vec![]));
    }
    if let Some(npx) = resolve_lsp_bin("npx", cwd) {
        return Some((npx, vec!["--no-install".into(), "prettier".into()]));
    }
    resolve_lsp_bin("prettier", cwd).map(|p| (p, vec![]))
}

// Formats `content` (the file at `path` in `cwd`) by extension, returning the
// formatted text WITHOUT writing to disk — the frontend decides whether/where
// to apply it. Unknown extension or missing formatter → returns content
// unchanged rather than erroring, so a stale toolchain never blocks the UI.
#[tauri::command]
async fn format_source(path: String, content: String, cwd: String) -> Result<String, String> {
    let ext = Path::new(&path).extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
    tauri::async_runtime::spawn_blocking(move || match ext.as_str() {
        "rs" => match resolve_lsp_bin("rustfmt", &cwd) {
            Some(bin) => run_formatter(&bin, &["--edition", "2021"], &content),
            None => Ok(content),
        },
        "js" | "jsx" | "ts" | "tsx" | "json" | "css" | "scss" | "html" | "vue" | "md" | "yaml" | "yml" => {
            match resolve_prettier(&cwd) {
                Some((bin, mut args)) => {
                    args.push("--stdin-filepath".into());
                    args.push(path.clone());
                    let arg_refs: Vec<&str> = args.iter().map(String::as_str).collect();
                    run_formatter(&bin, &arg_refs, &content)
                }
                None => Ok(content),
            }
        }
        _ => Ok(content),
    })
    .await
    .unwrap_or_else(|e| Err(e.to_string()))
}

// ── gh CLI (PR status) ────────────────────────────────────────────────────────
// Locate the GitHub CLI. Returns None when gh isn't installed so callers can
// degrade gracefully (no PR badge) instead of erroring.
fn gh_binary() -> Option<String> {
    for p in &["/opt/homebrew/bin/gh", "/usr/local/bin/gh", "/usr/bin/gh"] {
        if std::path::Path::new(p).exists() { return Some(p.to_string()); }
    }
    None
}

// Shell out to `gh` in `cwd`. Mirrors GitOutput so the frontend reuses the same
// shape. code = -1 + stderr "gh not found" when the CLI is missing; the store
// treats any non-zero code as "no PR info" and shows nothing.
// Async so the blocking `gh` subprocess (it also hits the network) runs on a
// blocking-pool thread instead of a Tauri command worker. At startup the Sidebar
// refreshes PR status for every workspace; running these inline saturated the
// command workers and stalled the real startup invokes → gray window. spawn_blocking
// keeps gh off that critical path.
#[tauri::command]
async fn run_gh(cwd: String, args: Vec<String>) -> GitOutput {
    let gh = match gh_binary() {
        Some(g) => g,
        None => return GitOutput { stdout: String::new(), stderr: "gh not found".into(), code: -1 },
    };
    tauri::async_runtime::spawn_blocking(move || {
        match std::process::Command::new(gh).args(&args).current_dir(&cwd).output() {
            Ok(out) => GitOutput {
                stdout: String::from_utf8_lossy(&out.stdout).into_owned(),
                stderr: String::from_utf8_lossy(&out.stderr).into_owned(),
                code: out.status.code().unwrap_or(-1),
            },
            Err(e) => GitOutput { stdout: String::new(), stderr: e.to_string(), code: -1 },
        }
    })
    .await
    .unwrap_or_else(|e| GitOutput { stdout: String::new(), stderr: e.to_string(), code: -1 })
}

// Synchronous `gh` shell-out for the Manager's PR commands (answered inline in
// take_spawn_requests). Mirrors git_in's Result contract.
fn gh_in(cwd: &str, args: &[&str]) -> Result<String, String> {
    let gh = gh_binary().ok_or_else(|| "gh not found (install the GitHub CLI)".to_string())?;
    match std::process::Command::new(gh).args(args).current_dir(cwd).output() {
        Ok(out) if out.status.success() => Ok(String::from_utf8_lossy(&out.stdout).into_owned()),
        Ok(out) => {
            let err = String::from_utf8_lossy(&out.stderr).into_owned();
            Err(if err.trim().is_empty() { format!("gh exited with {}", out.status.code().unwrap_or(-1)) } else { err })
        }
        Err(e) => Err(e.to_string()),
    }
}

// ── Open path in external app ─────────────────────────────────────────────────
// target: "finder" (reveal in Finder/Explorer), "vscode", "zed".
#[cfg(target_os = "macos")]
fn first_existing(paths: &[&str]) -> Option<String> {
    paths.iter().find(|p| std::path::Path::new(p).exists()).map(|p| p.to_string())
}

#[tauri::command]
fn open_path_in(path: String, target: String) -> Result<(), String> {
    // On macOS, `open -a App <folder>` just foregrounds an already-running editor
    // instead of opening the folder. Use the editor's own CLI (which opens the
    // path as a project/workspace), falling back to `open -a` if no CLI is found.
    #[cfg(target_os = "macos")]
    let mut cmd = match target.as_str() {
        "vscode" => {
            match first_existing(&[
                "/opt/homebrew/bin/code",
                "/usr/local/bin/code",
                "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
            ]) {
                Some(bin) => { let mut c = std::process::Command::new(bin); c.arg(&path); c }
                None => { let mut c = std::process::Command::new("open"); c.args(["-a", "Visual Studio Code", &path]); c }
            }
        }
        "zed" => {
            match first_existing(&[
                "/opt/homebrew/bin/zed",
                "/usr/local/bin/zed",
                "/Applications/Zed.app/Contents/MacOS/cli",
            ]) {
                Some(bin) => { let mut c = std::process::Command::new(bin); c.arg(&path); c }
                None => { let mut c = std::process::Command::new("open"); c.args(["-a", "Zed", &path]); c }
            }
        }
        _ => { let mut c = std::process::Command::new("open"); c.arg(&path); c }
    };
    #[cfg(target_os = "windows")]
    let mut cmd = match target.as_str() {
        "vscode" => { let mut c = std::process::Command::new("code"); c.arg(&path); c }
        "zed" => { let mut c = std::process::Command::new("zed"); c.arg(&path); c }
        _ => { let mut c = std::process::Command::new("explorer"); c.arg(&path); c }
    };
    #[cfg(all(unix, not(target_os = "macos")))]
    let mut cmd = match target.as_str() {
        "vscode" => { let mut c = std::process::Command::new("code"); c.arg(&path); c }
        "zed" => { let mut c = std::process::Command::new("zed"); c.arg(&path); c }
        _ => { let mut c = std::process::Command::new("xdg-open"); c.arg(&path); c }
    };
    cmd.spawn().map(|_| ()).map_err(|e| e.to_string())
}

// ── File system commands ──────────────────────────────────────────────────────

#[derive(Serialize)]
struct DirEntry { name: String, is_dir: bool }

#[tauri::command]
fn read_dir_shallow(path: String) -> Result<Vec<DirEntry>, String> {
    let entries = std::fs::read_dir(&path).map_err(|e| e.to_string())?;
    let mut result: Vec<DirEntry> = entries
        .filter_map(|e| e.ok())
        .map(|e| DirEntry {
            name: e.file_name().to_string_lossy().into_owned(),
            is_dir: e.file_type().map(|t| t.is_dir()).unwrap_or(false),
        })
        .collect();
    result.sort_by(|a, b| match (a.is_dir, b.is_dir) {
        (true, false) => std::cmp::Ordering::Less,
        (false, true) => std::cmp::Ordering::Greater,
        _ => a.name.to_lowercase().cmp(&b.name.to_lowercase()),
    });
    Ok(result)
}

// ── Workspace commands ────────────────────────────────────────────────────────

#[tauri::command]
fn list_workspaces(db: State<DbState>) -> Result<Vec<Workspace>, String> {
    let conn = db.conn.lock().unwrap();
    let mut stmt = conn
        .prepare("SELECT id, name, path, created_at, last_opened, parent_id, worktree_branch, is_git, icon, sort_order FROM workspaces ORDER BY COALESCE(last_opened, 0) DESC, created_at DESC")
        .map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map([], |row| Ok(Workspace {
            id: row.get(0)?,
            name: row.get(1)?,
            path: row.get(2)?,
            created_at: row.get(3)?,
            last_opened: row.get(4)?,
            parent_id: row.get(5)?,
            worktree_branch: row.get(6)?,
            is_git: row.get(7)?,
            icon: row.get(8)?,
            sort_order: row.get(9)?,
        }))
        .map_err(|e| e.to_string())?
        .filter_map(|r| r.ok())
        .collect();
    Ok(rows)
}

#[tauri::command]
fn create_workspace(name: String, path: String, db: State<DbState>) -> Result<Workspace, String> {
    let conn = db.conn.lock().unwrap();
    let now = unix_now();
    let is_git = is_git_repo(&path);
    conn.execute(
        "INSERT OR IGNORE INTO workspaces (name, path, created_at, is_git) VALUES (?1, ?2, ?3, ?4)",
        rusqlite::params![name, path, now, is_git],
    ).map_err(|e| e.to_string())?;
    let id = conn.last_insert_rowid();
    Ok(Workspace { id, name, path, created_at: now, last_opened: None, parent_id: None, worktree_branch: None, is_git, icon: None, sort_order: 0 })
}

#[tauri::command]
fn delete_workspace(id: i64, db: State<DbState>) -> Result<(), String> {
    db.conn.lock().unwrap()
        .execute("DELETE FROM workspaces WHERE id = ?1", rusqlite::params![id])
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn rename_workspace(id: i64, name: String, db: State<DbState>) -> Result<(), String> {
    db.conn.lock().unwrap()
        .execute("UPDATE workspaces SET name = ?1 WHERE id = ?2", rusqlite::params![name, id])
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn touch_workspace(id: i64, db: State<DbState>) -> Result<(), String> {
    db.conn.lock().unwrap()
        .execute("UPDATE workspaces SET last_opened = ?1 WHERE id = ?2", rusqlite::params![unix_now(), id])
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn set_workspace_icon(id: i64, icon: Option<String>, db: State<DbState>) -> Result<(), String> {
    db.conn.lock().unwrap()
        .execute("UPDATE workspaces SET icon = ?1 WHERE id = ?2", rusqlite::params![icon, id])
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn set_workspace_order(ids: Vec<i64>, db: State<DbState>) -> Result<(), String> {
    let conn = db.conn.lock().unwrap();
    for (i, id) in ids.iter().enumerate() {
        conn.execute(
            "UPDATE workspaces SET sort_order = ?1 WHERE id = ?2",
            rusqlite::params![i as i64, id],
        )
        .map_err(|e| e.to_string())?;
    }
    Ok(())
}

// ── Git worktrees ───────────────────────────────────────────────────────────────

fn expand_tilde(app: &AppHandle, p: &str) -> String {
    if let Some(rest) = p.strip_prefix("~") {
        if let Ok(home) = app.path().home_dir() {
            return format!("{}{}", home.display(), rest);
        }
    }
    p.to_string()
}

/// Run git in `repo` with `args`, returning Ok(stdout) on success or Err(stderr) on failure.
fn git_in(repo: &str, args: &[&str]) -> Result<String, String> {
    match std::process::Command::new(git_binary()).args(args).current_dir(repo).output() {
        Ok(out) if out.status.success() => Ok(String::from_utf8_lossy(&out.stdout).into_owned()),
        Ok(out) => {
            let err = String::from_utf8_lossy(&out.stderr).into_owned();
            Err(if err.trim().is_empty() { format!("git exited with {}", out.status.code().unwrap_or(-1)) } else { err })
        }
        Err(e) => Err(e.to_string()),
    }
}

// Run a shell command in `cwd`, capturing stdout+stderr, killing it after
// `timeout_secs`. Used by the Manager's `burrow run` (ad-hoc read-only commands).
// The child runs in a thread so we can time it out; on timeout we SIGTERM by pid
// (Child can't be killed after wait_with_output() moves it into the thread).
fn run_shell_capture(cwd: &str, cmd: &str, timeout_secs: u64) -> String {
    use std::process::{Command, Stdio};
    let child = match Command::new("/bin/sh")
        .arg("-c").arg(cmd)
        .current_dir(cwd)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
    {
        Ok(c) => c,
        Err(e) => return format!("error: failed to run: {e}"),
    };
    let pid = child.id();
    let (tx, rx) = std::sync::mpsc::channel();
    std::thread::spawn(move || {
        let _ = tx.send(child.wait_with_output());
    });
    match rx.recv_timeout(std::time::Duration::from_secs(timeout_secs)) {
        Ok(Ok(out)) => {
            let mut s = String::from_utf8_lossy(&out.stdout).into_owned();
            let err = String::from_utf8_lossy(&out.stderr);
            if !err.trim().is_empty() {
                if !s.is_empty() && !s.ends_with('\n') { s.push('\n'); }
                s.push_str(&err);
            }
            if s.trim().is_empty() {
                format!("(no output; exit {})", out.status.code().unwrap_or(-1))
            } else {
                s
            }
        }
        Ok(Err(e)) => format!("error: {e}"),
        Err(_) => {
            // Timed out — reap the runaway child by pid so it doesn't linger.
            let _ = Command::new("/bin/kill").arg("-TERM").arg(pid.to_string()).output();
            format!("error: command timed out after {timeout_secs}s")
        }
    }
}

#[tauri::command]
fn create_worktree(
    parent_id: i64,
    branch: String,
    base_ref: Option<String>,
    path: String,
    app: AppHandle,
    db: State<DbState>,
) -> Result<Workspace, String> {
    let branch = branch.trim().to_string();
    if branch.is_empty() {
        return Err("Branch name is required".into());
    }
    // Resolve the parent repo path; `parent_id IS NULL` enforces "no worktree of a worktree".
    let repo: String = {
        let conn = db.conn.lock().unwrap();
        conn.query_row(
            "SELECT path FROM workspaces WHERE id = ?1 AND parent_id IS NULL",
            rusqlite::params![parent_id],
            |row| row.get(0),
        )
        .map_err(|_| "Parent repo not found (or it is itself a worktree)".to_string())?
    };

    let wt_path = expand_tilde(&app, &path);
    if let Some(parent) = std::path::Path::new(&wt_path).parent() {
        std::fs::create_dir_all(parent).map_err(|e| e.to_string())?;
    }

    // New-vs-existing branch: probe whether the local branch already exists.
    let exists = git_in(&repo, &["rev-parse", "--verify", "--quiet", &format!("refs/heads/{}", branch)]).is_ok();
    if exists {
        git_in(&repo, &["worktree", "add", &wt_path, &branch])?;
    } else {
        let base = base_ref.as_deref().filter(|s| !s.trim().is_empty()).unwrap_or("HEAD");
        git_in(&repo, &["worktree", "add", "-b", &branch, &wt_path, base])?;
    }

    let now = unix_now();
    let id = {
        let conn = db.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO workspaces (name, path, created_at, parent_id, worktree_branch) VALUES (?1, ?2, ?3, ?4, ?5)",
            rusqlite::params![branch, wt_path, now, parent_id, branch],
        )
        .map_err(|e| {
            // Roll back the git worktree so a DB collision doesn't leave an orphan on disk.
            let _ = git_in(&repo, &["worktree", "remove", "--force", &wt_path]);
            e.to_string()
        })?;
        conn.last_insert_rowid()
    };

    Ok(Workspace {
        id,
        name: branch.clone(),
        path: wt_path,
        created_at: now,
        last_opened: None,
        parent_id: Some(parent_id),
        worktree_branch: Some(branch),
        is_git: true,
        icon: None,
        sort_order: 0,
    })
}

#[tauri::command]
fn remove_worktree(id: i64, force: bool, db: State<DbState>) -> Result<(), String> {
    let (wt_path, repo): (String, String) = {
        let conn = db.conn.lock().unwrap();
        conn.query_row(
            "SELECT w.path, p.path FROM workspaces w JOIN workspaces p ON w.parent_id = p.id WHERE w.id = ?1",
            rusqlite::params![id],
            |row| Ok((row.get(0)?, row.get(1)?)),
        )
        .map_err(|_| "Worktree not found".to_string())?
    };

    let dir_gone = !std::path::Path::new(&wt_path).exists();
    if dir_gone {
        // Directory removed out from under us — just reconcile git's registry.
        let _ = git_in(&repo, &["worktree", "prune"]);
    } else {
        let mut args = vec!["worktree", "remove", wt_path.as_str()];
        if force {
            args.push("--force");
        }
        if let Err(e) = git_in(&repo, &args) {
            // Surface the failure (e.g. uncommitted changes) so the UI can offer a force retry.
            return Err(e);
        }
    }

    db.conn.lock().unwrap()
        .execute("DELETE FROM workspaces WHERE id = ?1", rusqlite::params![id])
        .map_err(|e| e.to_string())?;
    Ok(())
}

// Resolve a worktree within `repo_path` by branch (preferred) or path, then remove
// it (git worktree + Burrow DB row). Used by the Manager's `burrow worktree-remove`.
fn remove_worktree_by(db: &State<DbState>, repo_path: &str, branch: &str, path: &str, force: bool) -> Result<String, String> {
    let (id, wt_path): (i64, String) = {
        let conn = db.conn.lock().unwrap();
        let repo_id: i64 = conn.query_row(
            "SELECT id FROM workspaces WHERE path = ?1",
            rusqlite::params![repo_path],
            |r| r.get(0),
        ).map_err(|_| "origin repo not found".to_string())?;
        // Match a child worktree of this repo by branch, else by path.
        let row = if !branch.is_empty() {
            conn.query_row(
                "SELECT id, path FROM workspaces WHERE parent_id = ?1 AND worktree_branch = ?2",
                rusqlite::params![repo_id, branch],
                |r| Ok((r.get::<_, i64>(0)?, r.get::<_, String>(1)?)),
            )
        } else {
            conn.query_row(
                "SELECT id, path FROM workspaces WHERE parent_id = ?1 AND path = ?2",
                rusqlite::params![repo_id, path],
                |r| Ok((r.get::<_, i64>(0)?, r.get::<_, String>(1)?)),
            )
        };
        row.map_err(|_| format!("worktree '{}' not found under this repo", if branch.is_empty() { path } else { branch }))?
    };

    let dir_gone = !std::path::Path::new(&wt_path).exists();
    if dir_gone {
        let _ = git_in(repo_path, &["worktree", "prune"]);
    } else {
        let mut args = vec!["worktree", "remove", wt_path.as_str()];
        if force { args.push("--force"); }
        git_in(repo_path, &args)?;
    }
    db.conn.lock().unwrap()
        .execute("DELETE FROM workspaces WHERE id = ?1", rusqlite::params![id])
        .map_err(|e| e.to_string())?;
    Ok(format!("removed worktree {}", if branch.is_empty() { wt_path } else { branch.to_string() }))
}

// ── Terminal tab persistence ───────────────────────────────────────────────────

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct TerminalTab {
    pub title: Option<String>,
    pub initial_cmd: Option<String>,
    pub pty_id: Option<u32>,
    pub cwd: Option<String>,
    /// The "Terminal N" fallback / user-renamed base title, separate from the
    /// live agent-set title stored in `title`. Added via idempotent migration.
    pub default_title: Option<String>,
    /// Claude Code session_id — set when a UserPromptSubmit hook fires. Used to
    /// auto-resume the conversation on app restart via `claude --resume <id>`.
    pub session_id: Option<String>,
}

#[tauri::command]
fn list_terminal_tabs(workspace_id: i64, db: State<DbState>) -> Result<Vec<TerminalTab>, String> {
    let conn = db.conn.lock().unwrap();
    let mut stmt = conn
        .prepare("SELECT title, initial_cmd, pty_id, cwd, default_title, session_id FROM terminal_tabs WHERE workspace_id = ?1 ORDER BY ord ASC")
        .map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map(rusqlite::params![workspace_id], |row| Ok(TerminalTab {
            title: row.get(0)?,
            initial_cmd: row.get(1)?,
            pty_id: row.get(2)?,
            cwd: row.get(3)?,
            default_title: row.get(4)?,
            session_id: row.get(5)?,
        }))
        .map_err(|e| e.to_string())?
        .filter_map(|r| r.ok())
        .collect();
    Ok(rows)
}

#[tauri::command]
fn save_terminal_tabs(
    workspace_id: i64,
    tabs: Vec<TerminalTab>,
    db: State<DbState>,
) -> Result<(), String> {
    let mut conn = db.conn.lock().unwrap();
    let tx = conn.transaction().map_err(|e| e.to_string())?;
    tx.execute("DELETE FROM terminal_tabs WHERE workspace_id = ?1", rusqlite::params![workspace_id])
        .map_err(|e| e.to_string())?;
    for (ord, tab) in tabs.iter().enumerate() {
        tx.execute(
            "INSERT INTO terminal_tabs (workspace_id, ord, title, initial_cmd, pty_id, cwd, default_title, session_id) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
            rusqlite::params![workspace_id, ord as i64, tab.title, tab.initial_cmd, tab.pty_id, tab.cwd, tab.default_title, tab.session_id],
        ).map_err(|e| e.to_string())?;
    }
    tx.commit().map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn get_app_version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

fn unix_now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs() as i64
}

#[tauri::command]
fn read_config(app: AppHandle) -> Result<String, String> {
    let path = app
        .path()
        .app_data_dir()
        .map_err(|e| e.to_string())?
        .join("config.json");
    match std::fs::read_to_string(&path) {
        Ok(s) => Ok(s),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok("{}".to_string()),
        Err(e) => Err(e.to_string()),
    }
}

#[tauri::command]
fn write_config(app: AppHandle, content: String) -> Result<(), String> {
    let dir = app.path().app_data_dir().map_err(|e| e.to_string())?;
    std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let path = dir.join("config.json");
    let tmp = dir.join("config.json.tmp");
    std::fs::write(&tmp, &content).map_err(|e| e.to_string())?;
    std::fs::rename(&tmp, &path).map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn write_text_file(path: String, content: String) -> Result<(), String> {
    std::fs::write(&path, content).map_err(|e| e.to_string())
}

#[tauri::command]
fn read_text_file(path: String) -> String {
    std::fs::read_to_string(&path).unwrap_or_default()
}

// Editor-grade read: distinguishes missing/empty/binary/too-large (unlike
// read_text_file which collapses every error to ""). Rejects binary (NUL byte or
// invalid UTF-8) and oversized files so the editor shows a placeholder instead of
// mounting garbage or freezing the renderer.
#[tauri::command]
fn read_text_file_checked(path: String) -> Result<String, String> {
    let bytes = std::fs::read(&path).map_err(|e| e.to_string())?;
    if bytes.len() > 5_000_000 {
        return Err("too-large".into());
    }
    if bytes.contains(&0) {
        return Err("binary".into());
    }
    String::from_utf8(bytes).map_err(|_| "binary".into())
}

#[tauri::command]
fn scaffold_burrow_dir(workspace_path: String, default_manager_prompt: String) -> Result<(), String> {
    let base = std::path::Path::new(&workspace_path);
    let burrow_dir = base.join(".burrow");
    std::fs::create_dir_all(&burrow_dir).map_err(|e| e.to_string())?;

    let manager_md = burrow_dir.join("manager.md");
    if !manager_md.exists() {
        std::fs::write(&manager_md, &default_manager_prompt).map_err(|e| e.to_string())?;
    }

    let config_toml = burrow_dir.join("config.toml");
    if !config_toml.exists() {
        let default_toml = "# Burrow project config\n# Uncomment and edit to add scripts:\n\n# [[scripts]]\n# id = \"dev\"\n# name = \"dev\"\n# steps = [\"pnpm dev\"]\n# continueOnError = false\n# color = \"#60a5fa\"\n";
        std::fs::write(&config_toml, default_toml).map_err(|e| e.to_string())?;
    }

    let skill_dir = base.join(".claude").join("skills").join("burrow");
    std::fs::create_dir_all(&skill_dir).map_err(|e| e.to_string())?;
    let skill_file = skill_dir.join("SKILL.md");
    std::fs::write(&skill_file, BURROW_SKILL_MD).map_err(|e| e.to_string())?;

    let kanban_skill_dir = base.join(".claude").join("skills").join("kanban");
    std::fs::create_dir_all(&kanban_skill_dir).map_err(|e| e.to_string())?;
    std::fs::write(kanban_skill_dir.join("SKILL.md"), KANBAN_SKILL_MD).map_err(|e| e.to_string())?;

    Ok(())
}

#[tauri::command]
fn read_file_base64(path: String) -> Result<String, String> {
    let bytes = std::fs::read(&path).map_err(|e| e.to_string())?;
    Ok(general_purpose::STANDARD.encode(&bytes))
}

// ── LSP bridge ────────────────────────────────────────────────────────────────
// The webview can't spawn processes, so a language server runs here as a child
// process and we bridge its stdio JSON-RPC to the frontend: stdout frames →
// `lsp-msg-{id}` events; `lsp_send` writes Content-Length-framed messages to
// stdin. The CodeMirror lsp-client drives the protocol (initialize/didOpen/etc).
struct LspProc {
    stdin: std::process::ChildStdin,
    child: std::process::Child,
}

#[derive(Default)]
struct LspState {
    procs: Mutex<std::collections::HashMap<u32, LspProc>>,
}

// A GUI app launched from Finder inherits a minimal PATH, so language servers
// (node CLIs in the project, rust-analyzer in ~/.cargo/bin, brew binaries) often
// aren't found via PATH alone. Search the usual locations explicitly.
fn resolve_lsp_bin(name: &str, root: &str) -> Option<PathBuf> {
    let p = Path::new(name);
    if p.is_absolute() {
        return if p.exists() { Some(p.to_path_buf()) } else { None };
    }
    let mut dirs: Vec<PathBuf> = vec![Path::new(root).join("node_modules/.bin")];
    if let Some(home) = std::env::var_os("HOME").map(PathBuf::from) {
        dirs.push(home.join(".cargo/bin"));
        dirs.push(home.join(".npm-global/bin"));
        dirs.push(home.join(".local/bin"));
        dirs.push(home.join(".volta/bin"));
    }
    dirs.push(PathBuf::from("/opt/homebrew/bin"));
    dirs.push(PathBuf::from("/usr/local/bin"));
    dirs.push(PathBuf::from("/usr/bin"));
    if let Ok(path) = std::env::var("PATH") {
        dirs.extend(path.split(':').map(PathBuf::from));
    }
    dirs.into_iter().map(|d| d.join(name)).find(|c| c.exists())
}

// A Finder-launched GUI app has a bare PATH, so a node-based server's
// `#!/usr/bin/env node` shebang (and rust-analyzer's tool lookups) can fail to
// find their runtime. Prepend the usual toolchain dirs to the child's PATH.
fn augmented_path(root: &str) -> String {
    let mut parts: Vec<String> = vec![format!("{root}/node_modules/.bin")];
    if let Some(home) = std::env::var_os("HOME").map(PathBuf::from) {
        for d in [".cargo/bin", ".volta/bin", ".local/bin", ".npm-global/bin"] {
            parts.push(home.join(d).to_string_lossy().into_owned());
        }
    }
    parts.extend(["/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"].map(String::from));
    if let Ok(existing) = std::env::var("PATH") {
        parts.push(existing);
    }
    parts.join(":")
}

// Cold `npx -y <pkg>` downloads the adapter on first use; for the big
// claude-agent-acp bundle that can exceed the handshake watchdog → the adapter
// gets killed mid-download and surfaces as "closed during handshake". Warm the
// npx cache in the background at startup so the real handshake is always fast.
// (--package <pkg> -- node -e "" downloads the package without launching the
// stdin-driven adapter.) Best-effort: failures are silent, the handshake still
// works on its own, just slower the first time.
fn prewarm_acp_adapters() {
    std::thread::spawn(|| {
        let root = std::env::var("HOME").unwrap_or_default();
        for pkg in ["@agentclientprotocol/claude-agent-acp", "@agentclientprotocol/codex-acp"] {
            let Some(npx) = resolve_lsp_bin("npx", &root) else { continue };
            let _ = std::process::Command::new(&npx)
                .args(["-y", "--package", pkg, "--", "node", "-e", ""])
                .env("PATH", augmented_path(&root))
                .stdin(std::process::Stdio::null())
                .stdout(std::process::Stdio::null())
                .stderr(std::process::Stdio::null())
                .status();
        }
    });
}

#[tauri::command]
fn lsp_start(
    app: AppHandle,
    state: State<LspState>,
    id: u32,
    name: String,
    args: Vec<String>,
    root_path: String,
) -> Result<(), String> {
    if state.procs.lock().unwrap().contains_key(&id) {
        return Ok(()); // already running for this id
    }
    let bin = resolve_lsp_bin(&name, &root_path)
        .ok_or_else(|| format!("language server '{name}' not found on PATH"))?;
    let mut child = std::process::Command::new(&bin)
        .args(&args)
        .current_dir(&root_path)
        .env("PATH", augmented_path(&root_path))
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .map_err(|e| e.to_string())?;
    let stdin = child.stdin.take().ok_or("no stdin")?;
    let stdout = child.stdout.take().ok_or("no stdout")?;
    let stderr = child.stderr.take().ok_or("no stderr")?;

    // Reader: parse Content-Length framed JSON-RPC, emit one event per message.
    let app2 = app.clone();
    std::thread::spawn(move || {
        use std::io::{BufRead, BufReader, Read};
        let mut reader = BufReader::new(stdout);
        loop {
            let mut content_len: usize = 0;
            loop {
                let mut line = String::new();
                match reader.read_line(&mut line) {
                    Ok(0) | Err(_) => return, // EOF or error
                    Ok(_) => {}
                }
                let t = line.trim_end();
                if t.is_empty() {
                    break; // blank line ends the header block
                }
                if let Some(v) = t.strip_prefix("Content-Length:") {
                    content_len = v.trim().parse().unwrap_or(0);
                }
            }
            if content_len == 0 {
                continue;
            }
            let mut buf = vec![0u8; content_len];
            if reader.read_exact(&mut buf).is_err() {
                return;
            }
            if let Ok(s) = String::from_utf8(buf) {
                let _ = app2.emit(&format!("lsp-msg-{id}"), s);
            }
        }
    });
    // Drain stderr so the server doesn't block on a full pipe.
    std::thread::spawn(move || {
        use std::io::Read;
        let mut s = stderr;
        let mut buf = [0u8; 4096];
        while let Ok(n) = s.read(&mut buf) {
            if n == 0 {
                break;
            }
        }
    });

    state.procs.lock().unwrap().insert(id, LspProc { stdin, child });
    Ok(())
}

#[tauri::command]
fn lsp_send(state: State<LspState>, id: u32, message: String) -> Result<(), String> {
    use std::io::Write;
    let mut guard = state.procs.lock().unwrap();
    let proc = guard.get_mut(&id).ok_or("lsp server not running")?;
    let header = format!("Content-Length: {}\r\n\r\n", message.len());
    proc.stdin
        .write_all(header.as_bytes())
        .and_then(|_| proc.stdin.write_all(message.as_bytes()))
        .and_then(|_| proc.stdin.flush())
        .map_err(|e| e.to_string())
}

#[tauri::command]
fn lsp_stop(state: State<LspState>, id: u32) {
    if let Some(mut proc) = state.procs.lock().unwrap().remove(&id) {
        let _ = proc.child.kill();
    }
}

// ── Claude chat bridge ────────────────────────────────────────────────────────
// Spawns `claude` in stream-json mode (the same mechanism as the VSCode extension,
// subscription-legal after the 2026-06-15 headless restriction). Each workspace
// gets its own persistent process (id = workspace_id); the session lives as long
// as the workspace is mounted. Modeled on the LSP bridge above.

// Read ~/.claude/settings.json and keep only stdio MCP servers.
// Remote servers (type=sse/ws/http) cause 30s+ hangs when spawned without a TTY
// because they try to connect to external endpoints that timeout. stdio servers
// spawn a local subprocess and are safe. Servers with no explicit type default to stdio.
fn build_mcp_config() -> String {
    let empty = r#"{"mcpServers":{}}"#.to_string();
    let config_dir = std::env::var("CLAUDE_CONFIG_DIR")
        .ok()
        .map(std::path::PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| std::path::PathBuf::from(h).join(".claude")));
    let settings_path = match config_dir {
        Some(d) => d.join("settings.json"),
        None => return empty,
    };
    let raw = match std::fs::read_to_string(&settings_path) {
        Ok(s) => s,
        Err(_) => return empty,
    };
    let v: serde_json::Value = match serde_json::from_str(&raw) {
        Ok(v) => v,
        Err(_) => return empty,
    };
    let servers = match v.get("mcpServers").and_then(|s| s.as_object()) {
        Some(m) => m,
        None => return empty,
    };
    // Keep only stdio (or untyped) servers — remote ones hang without a TTY.
    let local: serde_json::Map<String, serde_json::Value> = servers
        .iter()
        .filter(|(_, cfg)| {
            let t = cfg.get("type").and_then(|t| t.as_str()).unwrap_or("stdio");
            t == "stdio"
        })
        .map(|(k, v)| (k.clone(), v.clone()))
        .collect();
    serde_json::to_string(&serde_json::json!({ "mcpServers": local }))
        .unwrap_or(empty)
}

struct ClaudeProc {
    stdin: std::process::ChildStdin,
    child: std::process::Child,
}

#[derive(Default)]
struct ClaudeState {
    procs: Mutex<std::collections::HashMap<u32, ClaudeProc>>,
}

// ── ACP (Agent Client Protocol) bridge ──────────────────────────────────────
// Spawns an agent's ACP adapter as a subprocess speaking newline-delimited
// JSON-RPC on stdio (NOT Content-Length framed like LSP). Both the Tauri
// commands and the reader thread need stdin, hence Arc<Mutex<…>>.
struct AcpProc {
    stdin: Arc<std::sync::Mutex<std::process::ChildStdin>>,
    child: std::process::Child,
    next_id: Arc<std::sync::atomic::AtomicU64>,
    session_id: String,
}

#[derive(Default)]
struct AcpState {
    procs: std::sync::Mutex<std::collections::HashMap<u32, AcpProc>>,
}

// `async` is load-bearing: Tauri runs sync commands on the MAIN thread, so the
// blocking work here (binary probing, writing the burrow bin, and especially the
// `claude` fork/exec) would freeze the webview — the beachball when opening a
// chat. As an async command Tauri schedules it off the main thread instead.
// There are no `.await` points, so the brief MutexGuard is never held across one.
#[tauri::command]
async fn claude_start(
    app: AppHandle,
    state: State<'_, ClaudeState>,
    id: u32,
    cwd: String,
    resume_session_id: Option<String>,
    permission_mode: Option<String>,
    append_system_prompt: Option<String>,
    model: Option<String>,
    config_dir: Option<String>,
    profile_command: Option<String>,
    profile_args: Option<String>,
) -> Result<(), String> {
    if state.procs.lock().unwrap().contains_key(&id) {
        return Ok(());
    }
    let cmd_name = profile_command.as_deref().filter(|s| !s.trim().is_empty()).unwrap_or("claude");
    let bin = resolve_lsp_bin(cmd_name, &cwd)
        .ok_or_else(|| format!("{cmd_name} binary not found (checked ~/.local/bin, homebrew, PATH)"))?;

    let mcp_config = merge_burrow_into_mcp(&app, build_mcp_config(), &cwd);

    // User-chosen mode (header switch). Default `default` so edits surface as can_use_tool
    // → diff Accept/Reject (VS Code "review changes" parity). `acceptEdits` auto-applies
    // edits; `bypassPermissions` skips everything. Whitelisted to keep it off the argv as
    // arbitrary input.
    let perm_mode = match permission_mode.as_deref() {
        Some("acceptEdits") => "acceptEdits",
        Some("bypassPermissions") => "bypassPermissions",
        Some("plan") => "plan",
        Some("auto") => "auto",
        Some("dontAsk") => "dontAsk",
        _ => "default",
    };
    let mut args = vec![
        "--output-format".to_string(), "stream-json".to_string(),
        "--verbose".to_string(),
        "--input-format".to_string(), "stream-json".to_string(),
        "--include-partial-messages".to_string(),
        "--permission-mode".to_string(), perm_mode.to_string(),
        // Hidden flag (not in `claude --help`): routes every permission / blocking-tool
        // decision to us as a `can_use_tool` control_request on stdin instead of the CLI
        // auto-allowing/denying internally. This is exactly what the Agent SDK passes when
        // a `canUseTool` callback is set. Without it, Edit/Write/Bash/ExitPlanMode/
        // AskUserQuestion never reach the UI. We reply with claude_respond_control.
        "--permission-prompt-tool".to_string(), "stdio".to_string(),
        "--mcp-config".to_string(), mcp_config,
    ];
    if let Some(sid) = resume_session_id {
        args.push("--resume".to_string());
        args.push(sid);
    }
    // Mission-control primer (float chat). Teaches the session it can drive the
    // app via the `burrow` CLI. Only the float passes this; in-tab chats omit it.
    if let Some(sys) = append_system_prompt.as_deref().filter(|s| !s.trim().is_empty()) {
        args.push("--append-system-prompt".to_string());
        args.push(sys.to_string());
    }
    let allowed_models = ["claude-opus-4-8", "claude-sonnet-5", "claude-haiku-4-5-20251001"];
    if let Some(m) = model.as_deref().filter(|m| allowed_models.contains(m)) {
        args.push("--model".to_string());
        args.push(m.to_string());
    }

    // Profile extra args (inserted before any positional args).
    if let Some(pa) = profile_args.as_deref().filter(|s| !s.trim().is_empty()) {
        for flag in pa.split_whitespace() {
            args.push(flag.to_string());
        }
    }

    // Strip env to minimal set — bare GUI PATH + subscription auth via keychain.
    // ANTHROPIC_API_KEY intentionally empty so subscription OAuth is used.
    let mut env_map = std::collections::HashMap::new();
    for key in ["HOME", "USER", "TMPDIR", "LANG", "CLAUDE_CONFIG_DIR"] {
        if let Ok(v) = std::env::var(key) {
            env_map.insert(key.to_string(), v);
        }
    }
    // Profile config_dir overrides any inherited CLAUDE_CONFIG_DIR.
    if let Some(cd) = config_dir.as_deref().filter(|s| !s.trim().is_empty()) {
        let expanded = if cd.starts_with("~/") {
            if let Ok(home) = std::env::var("HOME") {
                format!("{}{}", home, &cd[1..])
            } else {
                cd.to_string()
            }
        } else {
            cd.to_string()
        };
        env_map.insert("CLAUDE_CONFIG_DIR".to_string(), expanded);
    }
    // PATH: prepend the burrow bin dir so the agent's Bash can run `burrow …`
    // control commands, then the usual augmented GUI PATH.
    let path = match ensure_burrow_bin(&app) {
        Some(bin_dir) => format!("{}:{}", bin_dir.display(), augmented_path(&cwd)),
        None => augmented_path(&cwd),
    };
    env_map.insert("PATH".to_string(), path);
    env_map.insert("ANTHROPIC_API_KEY".to_string(), std::env::var("ANTHROPIC_API_KEY").unwrap_or_default());

    // Burrow env so `burrow` control/read commands work from this session's Bash.
    // Mirrors the PTY-spawn exports. Deliberately NO BURROW_PTY_ID — the chat is
    // not a tab, so the global status hook stays a no-op for it. BURROW_CWD routes
    // read/control commands (list-workspaces, focus-*) to the origin workspace.
    if let Some(sess) = burrow_session_dir(&app) {
        env_map.insert("BURROW_SESSION_DIR".to_string(), sess.to_string_lossy().to_string());
    }
    env_map.insert("BURROW_CWD".to_string(), cwd.clone());
    if let Some(port) = HOOK_SERVER_PORT.get() {
        env_map.insert("BURROW_HOOK_PORT".to_string(), port.to_string());
    }
    if let Ok(data) = app.path().app_data_dir() {
        env_map.insert("BURROW_HOME_DIR".to_string(), data.to_string_lossy().to_string());
    }

    let mut child = std::process::Command::new(&bin)
        .args(&args)
        .current_dir(&cwd)
        .envs(&env_map)
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .map_err(|e| format!("failed to spawn claude: {e}"))?;

    let stdin = child.stdin.take().ok_or("no stdin")?;
    let stdout = child.stdout.take().ok_or("no stdout")?;
    let stderr = child.stderr.take().ok_or("no stderr")?;

    // Reader: one JSON line per event → emit claude-data-{id}
    let app2 = app.clone();
    std::thread::spawn(move || {
        use std::io::BufRead;
        let reader = std::io::BufReader::new(stdout);
        for line in reader.lines() {
            match line {
                Ok(l) => {
                    let t = l.trim().to_string();
                    if t.is_empty() { continue; }
                    let _ = app2.emit_all(&format!("claude-data-{id}"), t);
                }
                Err(_) => break,
            }
        }
        let _ = app2.emit_all(&format!("claude-data-{id}"), r#"{"type":"exit"}"#);
    });
    // Drain stderr to prevent pipe stall.
    std::thread::spawn(move || {
        use std::io::Read;
        let mut buf = [0u8; 4096];
        let mut s = stderr;
        while let Ok(n) = s.read(&mut buf) {
            if n == 0 { break; }
        }
    });

    state.procs.lock().unwrap().insert(id, ClaudeProc { stdin, child });
    Ok(())
}

#[tauri::command]
fn claude_send(state: State<ClaudeState>, id: u32, text: String, session_id: Option<String>, images: Option<Vec<String>>) -> Result<(), String> {
    use std::io::Write;
    let mut guard = state.procs.lock().unwrap();
    let proc = guard.get_mut(&id).ok_or("claude not running for this workspace")?;

    let mut content: Vec<serde_json::Value> = vec![];

    // Prepend image blocks — each entry is a data URI "data:<mime>;base64,<data>"
    for data_uri in images.unwrap_or_default() {
        if let Some(rest) = data_uri.strip_prefix("data:") {
            if let Some(comma) = rest.find(',') {
                let meta = &rest[..comma];
                let data = &rest[comma + 1..];
                let media_type = meta.split(';').next().unwrap_or("image/png");
                content.push(serde_json::json!({
                    "type": "image",
                    "source": { "type": "base64", "media_type": media_type, "data": data }
                }));
            }
        }
    }

    content.push(serde_json::json!({ "type": "text", "text": text }));

    let msg = serde_json::json!({
        "type": "user",
        "session_id": session_id.unwrap_or_default(),
        "message": { "role": "user", "content": content }
    });
    let line = msg.to_string() + "\n";
    proc.stdin.write_all(line.as_bytes()).and_then(|_| proc.stdin.flush()).map_err(|e| e.to_string())
}

#[tauri::command]
fn claude_stop(state: State<ClaudeState>, id: u32) {
    if let Some(mut proc) = state.procs.lock().unwrap().remove(&id) {
        let _ = proc.child.kill();
    }
}

// Abort the current turn by sending SIGINT — lets claude finalize gracefully
// (it emits a result event) rather than SIGKILL which just drops the pipe.
// The stdout reader thread will see EOF and emit the exit event normally.
#[tauri::command]
fn claude_abort(state: State<ClaudeState>, id: u32) {
    let guard = state.procs.lock().unwrap();
    if let Some(proc) = guard.get(&id) {
        let pid = proc.child.id();
        drop(guard);
        #[cfg(unix)]
        {
            std::process::Command::new("kill")
                .args(["-INT", &pid.to_string()])
                .spawn()
                .ok();
        }
    }
}

// Reply to a `can_use_tool` control_request on claude's stdin. The frontend builds the
// inner permission decision (`response`) so this one command serves every blocking tool:
//   permission   → {"behavior":"allow","updatedInput":{...}} | {"behavior":"deny","message":"..."}
//   ExitPlanMode → allow = approve plan; deny + message = keep planning with feedback
//   AskUserQuestion → allow with updatedInput.answers = {"<question text>":"<chosen label>"}
// Wire shape (double-nested `response.response`) confirmed against claude 2.1.181 / SDK 0.3.181.
#[tauri::command]
fn claude_respond_control(state: State<ClaudeState>, id: u32, request_id: String, response: serde_json::Value) -> Result<(), String> {
    use std::io::Write;
    let mut guard = state.procs.lock().unwrap();
    let proc = guard.get_mut(&id).ok_or("claude not running")?;
    let msg = serde_json::json!({
        "type": "control_response",
        "response": { "subtype": "success", "request_id": request_id, "response": response }
    });
    let line = msg.to_string() + "\n";
    proc.stdin.write_all(line.as_bytes()).and_then(|_| proc.stdin.flush()).map_err(|e| e.to_string())
}

// ── ACP commands ────────────────────────────────────────────────────────────
// Write one newline-delimited JSON-RPC message to the adapter's stdin.
fn acp_write(stdin: &Arc<std::sync::Mutex<std::process::ChildStdin>>, msg: &serde_json::Value) -> Result<(), String> {
    use std::io::Write;
    let line = msg.to_string() + "\n";
    let mut g = stdin.lock().unwrap();
    g.write_all(line.as_bytes()).and_then(|_| g.flush()).map_err(|e| e.to_string())
}

/// Load key=value pairs from a .env file (ignores comments and blank lines).
/// Called from acp_start to merge project env vars without crate dependencies.
fn load_dotenv_file(path: &std::path::Path) -> std::collections::HashMap<String, String> {
    let mut map = std::collections::HashMap::new();
    let Ok(content) = std::fs::read_to_string(path) else { return map };
    for line in content.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') { continue; }
        if let Some((k, v)) = line.split_once('=') {
            let v = v.trim().trim_matches('"').trim_matches('\'');
            map.insert(k.trim().to_string(), v.to_string());
        }
    }
    map
}

// Append a line to <app-data>/acp-debug.log. Always-on diagnostics: ACP adapters
// behave differently in a Finder-launched bundle than in `tauri:dev`, and the
// failure is otherwise invisible (silent "thinking", no model). Cheap, bounded
// by the few lines per session.
fn acp_dbg(path: &Option<std::path::PathBuf>, msg: &str) {
    if let Some(p) = path {
        use std::io::Write;
        if let Ok(mut f) = std::fs::OpenOptions::new().create(true).append(true).open(p) {
            let _ = writeln!(f, "{msg}");
        }
    }
}

#[tauri::command]
async fn acp_start(
    app: AppHandle,
    state: State<'_, AcpState>,
    id: u32,
    cwd: String,
    command: String,                                  // adapter program ("npx", "gemini", "codex", "opencode", …)
    args: Vec<String>,                                // adapter args (e.g. ["@agentclientprotocol/claude-agent-acp"])
    env: std::collections::HashMap<String, String>,   // user-defined env from the agent registry
    kind: Option<String>,                             // "claude"|"gemini"|"codex"|"custom" — drives special env injection
    config_dir: Option<String>,                       // override CLAUDE_CONFIG_DIR (from .burrow/config.toml [settings])
    env_file: Option<String>,                         // path to .env relative to cwd (default: .env)
    resume_session_id: Option<String>,                // resume a prior session via session/load instead of session/new
    emit_history: Option<bool>,                       // render the session/load replay (picker resume); else discard it
) -> Result<(), String> {
    if state.procs.lock().unwrap().contains_key(&id) {
        let dbg = app.path().app_data_dir().ok().map(|d| d.join("acp-debug.log"));
        acp_dbg(&dbg, &format!("acp_start id={id}: proc already running — early return (no session re-emit)"));
        return Ok(());
    }
    let emit_history = emit_history.unwrap_or(false);

    let kind = kind.unwrap_or_else(|| "custom".to_string());
    // Start from the user-defined env, then layer special per-kind injection on top.
    let mut extra_env = env;

    if kind == "claude" {
        // The claude adapter shells out to the `claude` binary, so point it at the
        // resolved path and blank ANTHROPIC_API_KEY so subscription OAuth flows.
        let claude_bin = resolve_lsp_bin("claude", &cwd)
            .ok_or_else(|| "claude binary not found (checked ~/.local/bin, homebrew, PATH)".to_string())?;
        extra_env.insert("CLAUDE_CODE_EXECUTABLE".to_string(), claude_bin.to_string_lossy().to_string());
        extra_env.insert("ANTHROPIC_API_KEY".to_string(), String::new());
        if let Some(cd) = config_dir.as_deref().filter(|s| !s.trim().is_empty()) {
            extra_env.insert("CLAUDE_CONFIG_DIR".to_string(), cd.to_string());
        }
    } else if kind == "codex" {
        // Forward Codex/OpenAI keys from the host env unless the agent already set them.
        for k in ["CODEX_API_KEY", "OPENAI_API_KEY", "OPEN_AI_API_KEY"] {
            if !extra_env.contains_key(k) {
                if let Ok(v) = std::env::var(k) { extra_env.insert(k.to_string(), v); }
            }
        }
    }

    let bin = resolve_lsp_bin(&command, &cwd)
        .ok_or_else(|| format!("{command} binary not found (checked ~/.local/bin, homebrew, PATH)"))?;

    // npx with no controlling tty + the JSON-RPC pipe on stdin: if the adapter package
    // isn't cached, npx would block on a confirmation prompt (reading our handshake as
    // the answer) and the session hangs forever. Force non-interactive install.
    let mut args = args;
    let is_npx = bin.file_name().and_then(|n| n.to_str()).map(|n| n == "npx" || n == "npx.cmd").unwrap_or(false);
    if is_npx && !args.iter().any(|a| a == "-y" || a == "--yes") {
        args.insert(0, "-y".to_string());
    }

    // Load .env file from project dir (env_file setting, default .env).
    // or_insert: never override env already set above.
    let dotenv_path = std::path::Path::new(&cwd).join(
        env_file.as_deref().unwrap_or(".env")
    );
    for (k, v) in load_dotenv_file(&dotenv_path) {
        extra_env.entry(k).or_insert(v);
    }

    // Minimal inherited base + augmented PATH (don't clobber explicit entries).
    for key in ["HOME", "USER", "TMPDIR", "LANG"] {
        if let Ok(v) = std::env::var(key) {
            extra_env.entry(key.to_string()).or_insert(v);
        }
    }
    extra_env.insert("PATH".to_string(), augmented_path(&cwd));

    let dbg_path = app.path().app_data_dir().ok().map(|d| d.join("acp-debug.log"));
    acp_dbg(&dbg_path, &format!(
        "\n=== acp_start id={id} kind={kind} ===\ncmd={command} bin={}\nargs={:?}\ncwd={cwd}\nPATH={}",
        bin.display(), args, extra_env.get("PATH").cloned().unwrap_or_default()
    ));

    let mut child = std::process::Command::new(&bin)
        .args(&args)
        .current_dir(&cwd)
        .envs(&extra_env)
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .map_err(|e| { let m = format!("failed to spawn acp adapter: {e}"); acp_dbg(&dbg_path, &m); m })?;
    acp_dbg(&dbg_path, &format!("spawned pid={}", child.id()));

    let stdin = child.stdin.take().ok_or("no stdin")?;
    let stdout = child.stdout.take().ok_or("no stdout")?;
    let stderr = child.stderr.take().ok_or("no stderr")?;

    // Drain stderr to prevent pipe stall, AND keep the tail (~8KB) so a failed
    // handshake can report WHY (node not found, npm registry error, auth, …).
    let stderr_log = Arc::new(std::sync::Mutex::new(String::new()));
    let stderr_log2 = stderr_log.clone();
    let dbg_err = dbg_path.clone();
    std::thread::spawn(move || {
        use std::io::Read;
        let mut buf = [0u8; 4096];
        let mut s = stderr;
        while let Ok(n) = s.read(&mut buf) {
            if n == 0 { break; }
            let chunk = String::from_utf8_lossy(&buf[..n]).to_string();
            acp_dbg(&dbg_err, &format!("STDERR {}", chunk.trim_end()));
            if let Ok(mut g) = stderr_log2.lock() {
                g.push_str(&chunk);
                if g.len() > 8192 { let cut = g.len() - 8192; g.drain(..cut); }
            }
        }
        acp_dbg(&dbg_err, "STDERR <eof>");
    });

    let stdin = Arc::new(std::sync::Mutex::new(stdin));
    let next_id = Arc::new(std::sync::atomic::AtomicU64::new(3)); // 0,1,2 reserved for handshake (initialize/load/new)

    // Handshake off the main thread: initialize (id 0) → session/new or session/load (id 1).
    // Returns the BufReader (positioned past the handshake responses) + sessionId +
    // the session result (modes + configOptions, surfaced to the frontend selectors).
    let cwd_hs = cwd.clone();
    let stdin_hs = stdin.clone();
    let resume_hs = resume_session_id.clone();
    // Burrow MCP server (ACP array format) so ACP agents (codex/gemini/…) get the
    // spawn/worktree/pr tools too. Empty array when the socket is unavailable.
    let burrow_mcp_hs: Vec<serde_json::Value> = match build_burrow_acp_server(&app, &cwd) {
        serde_json::Value::Null => vec![],
        v => vec![v],
    };
    let app_hs = app.clone();
    let emit_history_hs = emit_history;

    // Watchdog: if the handshake doesn't finish within 120s, kill the adapter so the
    // blocking read_line below unblocks (Ok(0) → "closed") instead of hanging the
    // session in a permanent "thinking" with no model and no error. 120s (not 30s)
    // tolerates a cold `npx -y` download of the adapter on first use.
    let hs_done = Arc::new(std::sync::atomic::AtomicBool::new(false));
    let hs_done_wd = hs_done.clone();
    let child_pid = child.id();
    std::thread::spawn(move || {
        for _ in 0..1200 {
            std::thread::sleep(std::time::Duration::from_millis(100));
            if hs_done_wd.load(std::sync::atomic::Ordering::SeqCst) { return; }
        }
        let _ = std::process::Command::new("kill").arg("-9").arg(child_pid.to_string()).status();
    });

    let (reader, session_id, session_result) = tauri::async_runtime::spawn_blocking(move || -> Result<(std::io::BufReader<std::process::ChildStdout>, String, serde_json::Value), String> {
        use std::io::BufRead;
        let mut reader = std::io::BufReader::new(stdout);

        // Read newline-delimited JSON until we see a response with the given id.
        let read_response = |reader: &mut std::io::BufReader<std::process::ChildStdout>, want: u64| -> Result<serde_json::Value, String> {
            loop {
                let mut line = String::new();
                match reader.read_line(&mut line) {
                    Ok(0) => return Err("acp adapter closed during handshake".to_string()),
                    Ok(_) => {}
                    Err(e) => return Err(e.to_string()),
                }
                let t = line.trim();
                if t.is_empty() { continue; }
                let v: serde_json::Value = match serde_json::from_str(t) {
                    Ok(v) => v,
                    Err(_) => continue,
                };
                // A response has an id matching `want` and no `method`.
                if v.get("method").is_none() && v.get("id").and_then(|i| i.as_u64()) == Some(want) {
                    return Ok(v);
                }
                // ignore notifications/requests during handshake
            }
        };

        acp_write(&stdin_hs, &serde_json::json!({
            "jsonrpc": "2.0",
            "id": 0,
            "method": "initialize",
            "params": {
                "protocolVersion": 1,
                "clientCapabilities": {
                    "fs": { "readTextFile": false, "writeTextFile": false },
                    "terminal": false
                },
                "clientInfo": { "name": "burrow", "title": "Burrow", "version": "2.16.0" }
            }
        }))?;
        let _ = read_response(&mut reader, 0)?;

        // Resume a prior conversation via session/load; fall back to session/new on error.
        let mut result = serde_json::Value::Null;
        let mut session_id = String::new();
        if let Some(sid) = resume_hs.as_deref().filter(|s| !s.trim().is_empty()) {
            acp_write(&stdin_hs, &serde_json::json!({
                "jsonrpc": "2.0", "id": 1, "method": "session/load",
                "params": { "sessionId": sid, "cwd": cwd_hs, "mcpServers": burrow_mcp_hs.clone() }
            }))?;
            // session/load REPLAYS the prior conversation as session/update notifications
            // before responding — forward those to the frontend so the picker renders
            // the old history. Loop here (instead of read_response) to emit while waiting.
            let resp = loop {
                let mut line = String::new();
                match reader.read_line(&mut line) {
                    Ok(0) => return Err("acp adapter closed during session/load".to_string()),
                    Ok(_) => {}
                    Err(e) => return Err(e.to_string()),
                }
                let t = line.trim();
                if t.is_empty() { continue; }
                let v: serde_json::Value = match serde_json::from_str(t) { Ok(v) => v, Err(_) => continue };
                if v.get("method").and_then(|m| m.as_str()) == Some("session/update") {
                    if emit_history_hs { let _ = app_hs.emit_all(&format!("acp-data-{id}"), t.to_string()); }
                    continue;
                }
                if v.get("method").is_none() && v.get("id").and_then(|i| i.as_u64()) == Some(1) {
                    break v;
                }
            };
            if resp.get("error").is_none() {
                session_id = sid.to_string();
                result = resp.get("result").cloned().unwrap_or(serde_json::Value::Null);
            }
            // else: load failed (stale id) — fall through to session/new below
        }
        if session_id.is_empty() {
            acp_write(&stdin_hs, &serde_json::json!({
                "jsonrpc": "2.0", "id": 2, "method": "session/new",
                "params": { "cwd": cwd_hs, "mcpServers": burrow_mcp_hs.clone() }
            }))?;
            let resp = read_response(&mut reader, 2)?;
            result = resp.get("result").cloned().unwrap_or(serde_json::Value::Null);
            session_id = result
                .get("sessionId")
                .and_then(|s| s.as_str())
                .ok_or("session/new response missing sessionId")?
                .to_string();
        }

        Ok((reader, session_id, result))
    })
    .await
    .map_err(|e| e.to_string())
    .and_then(|inner| inner.map_err(|e| {
        // Handshake failed/timed out — attach the adapter's stderr tail so the
        // frontend shows the actual cause instead of a silent stuck session.
        let logs = stderr_log.lock().map(|g| g.trim().to_string()).unwrap_or_default();
        let m = if logs.is_empty() { e } else { format!("{e}\n--- adapter stderr ---\n{logs}") };
        acp_dbg(&dbg_path, &format!("HANDSHAKE FAILED: {m}"));
        m
    }))?;
    hs_done.store(true, std::sync::atomic::Ordering::SeqCst);
    acp_dbg(&dbg_path, &format!(
        "handshake OK session_id={session_id} has_modes={} has_config={}",
        !session_result.get("modes").map(|v| v.is_null()).unwrap_or(true),
        !session_result.get("configOptions").map(|v| v.is_null()).unwrap_or(true),
    ));

    // Surface the session info (sessionId + modes + configOptions) to the frontend
    // so the model / permission-mode selectors can populate.
    let _ = app.emit_all(&format!("acp-data-{id}"), serde_json::json!({
        "_burrow": "session",
        "sessionId": session_id,
        "modes": session_result.get("modes").cloned().unwrap_or(serde_json::Value::Null),
        "configOptions": session_result.get("configOptions").cloned().unwrap_or(serde_json::Value::Null),
    }).to_string());

    // Reader thread: route adapter output to the frontend.
    let app2 = app.clone();
    let dbg_reader = dbg_path.clone();
    std::thread::spawn(move || {
        use std::io::BufRead;
        let mut reader = reader;
        loop {
            let mut line = String::new();
            match reader.read_line(&mut line) {
                Ok(0) | Err(_) => break, // EOF
                Ok(_) => {}
            }
            let t = line.trim();
            if t.is_empty() { continue; }
            // A message with a `method` AND an `id` is a server→client request
            // (e.g. session/request_permission). Everything else (notifications
            // like session/update, and our prompt responses) goes to acp-data.
            let is_request = serde_json::from_str::<serde_json::Value>(t)
                .ok()
                .map(|v| v.get("method").is_some() && v.get("id").is_some())
                .unwrap_or(false);
            let topic = if is_request { format!("acp-req-{id}") } else { format!("acp-data-{id}") };
            let _ = app2.emit(&topic, t.to_string());
        }
        acp_dbg(&dbg_reader, "reader <eof>");
        let _ = app2.emit_all(&format!("acp-data-{id}"), r#"{"_burrow":"exit"}"#);
    });

    state.procs.lock().unwrap().insert(id, AcpProc { stdin, child, next_id, session_id });
    Ok(())
}

/// Sends a prompt and returns the JSON-RPC id used, so the frontend can match the
/// turn-done response (other responses like set_mode share the acp-data channel).
#[tauri::command]
fn acp_send(state: State<AcpState>, id: u32, text: String, images: Option<Vec<String>>) -> Result<u64, String> {
    let guard = state.procs.lock().unwrap();
    let proc = guard.get(&id).ok_or("acp adapter not running")?;
    let rpc_id = proc.next_id.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
    // ACP ContentBlock: images go as { type:"image", mimeType, data(base64) }.
    // The frontend sends data URIs ("data:image/png;base64,XXXX") — split them.
    let mut prompt: Vec<serde_json::Value> = Vec::new();
    if !text.is_empty() {
        prompt.push(serde_json::json!({ "type": "text", "text": text }));
    }
    for uri in images.unwrap_or_default() {
        let (mime, data) = match uri.strip_prefix("data:").and_then(|r| r.split_once(";base64,")) {
            Some((m, d)) => (m.to_string(), d.to_string()),
            None => ("image/png".to_string(), uri.clone()),
        };
        prompt.push(serde_json::json!({ "type": "image", "mimeType": mime, "data": data }));
    }
    acp_write(&proc.stdin, &serde_json::json!({
        "jsonrpc": "2.0",
        "id": rpc_id,
        "method": "session/prompt",
        "params": { "sessionId": proc.session_id, "prompt": prompt }
    }))?;
    Ok(rpc_id)
}

/// Sets the session permission mode (session/set_mode). Returns the rpc id so the
/// frontend can refresh its selectors from the response (which carries modes).
#[tauri::command]
fn acp_set_mode(state: State<AcpState>, id: u32, mode_id: String) -> Result<u64, String> {
    let guard = state.procs.lock().unwrap();
    let proc = guard.get(&id).ok_or("acp adapter not running")?;
    let rpc_id = proc.next_id.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
    acp_write(&proc.stdin, &serde_json::json!({
        "jsonrpc": "2.0", "id": rpc_id, "method": "session/set_mode",
        "params": { "sessionId": proc.session_id, "modeId": mode_id }
    }))?;
    Ok(rpc_id)
}

/// Sets a session config option (session/set_config_option), e.g. model or effort.
/// Returns the rpc id; the response echoes the updated configOptions.
#[tauri::command]
fn acp_set_config(state: State<AcpState>, id: u32, config_id: String, value: String) -> Result<u64, String> {
    let guard = state.procs.lock().unwrap();
    let proc = guard.get(&id).ok_or("acp adapter not running")?;
    let rpc_id = proc.next_id.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
    acp_write(&proc.stdin, &serde_json::json!({
        "jsonrpc": "2.0", "id": rpc_id, "method": "session/set_config_option",
        "params": { "sessionId": proc.session_id, "configId": config_id, "value": value }
    }))?;
    Ok(rpc_id)
}

/// Lists prior sessions for `cwd` (session/list). Returns the rpc id; the response
/// (with `{sessions:[...]}`) is matched on acp-data and shown in the history picker.
#[tauri::command]
fn acp_list_sessions(state: State<AcpState>, id: u32, cwd: String) -> Result<u64, String> {
    let guard = state.procs.lock().unwrap();
    let proc = guard.get(&id).ok_or("acp adapter not running")?;
    let rpc_id = proc.next_id.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
    acp_write(&proc.stdin, &serde_json::json!({
        "jsonrpc": "2.0", "id": rpc_id, "method": "session/list",
        "params": { "cwd": cwd }
    }))?;
    Ok(rpc_id)
}

#[tauri::command]
fn acp_stop(state: State<AcpState>, id: u32) {
    if let Some(mut proc) = state.procs.lock().unwrap().remove(&id) {
        let _ = proc.child.kill();
    }
}

#[tauri::command]
fn acp_respond_permission(state: State<AcpState>, id: u32, rpc_id: u64, option_id: String) -> Result<(), String> {
    let guard = state.procs.lock().unwrap();
    let proc = guard.get(&id).ok_or("acp adapter not running")?;
    let outcome = if option_id.is_empty() {
        serde_json::json!({ "outcome": "cancelled" })
    } else {
        serde_json::json!({ "outcome": "selected", "optionId": option_id })
    };
    acp_write(&proc.stdin, &serde_json::json!({
        "jsonrpc": "2.0",
        "id": rpc_id,
        "result": { "outcome": outcome }
    }))
}

// ── Claude account info ───────────────────────────────────────────────────────

#[derive(Serialize, Clone)]
struct ClaudeAccountInfo {
    email: String,
    display_name: String,
    organization_type: String,   // e.g. "claude_max"
    rate_limit_tier: String,     // e.g. "default_claude_max_5x"
    status_text: String,         // raw stdout of `claude status` (for 5h window parsing)
}

#[derive(Default)]
struct AccountInfoCache(Mutex<Option<ClaudeAccountInfo>>);

// Async so it runs off the main thread: `rx.recv_timeout(4s)` below blocks the
// caller until `claude status` returns (up to 4s) — on the main thread that's a
// 4s beachball on first chat open, even though the frontend invokes it via .then().
#[tauri::command]
async fn claude_get_account(state: State<'_, AccountInfoCache>, cwd: String) -> Result<ClaudeAccountInfo, String> {
    // Return cached value if already fetched — avoids N concurrent `claude status` spawns.
    if let Some(cached) = state.0.lock().unwrap().clone() {
        return Ok(cached);
    }
    let home = std::env::var("HOME").unwrap_or_default();
    let path = std::path::Path::new(&home).join(".claude.json");
    let json: serde_json::Value = std::fs::read_to_string(&path)
        .ok()
        .and_then(|s| serde_json::from_str(&s).ok())
        .unwrap_or(json!({}));
    let acct = json.get("oauthAccount").cloned().unwrap_or(json!({}));
    let email = acct.get("emailAddress").and_then(|v| v.as_str()).unwrap_or("").to_string();
    let display_name = acct.get("displayName").and_then(|v| v.as_str()).unwrap_or("").to_string();
    let organization_type = acct.get("organizationType").and_then(|v| v.as_str()).unwrap_or("").to_string();
    let rate_limit_tier = acct.get("organizationRateLimitTier").and_then(|v| v.as_str()).unwrap_or("").to_string();

    // Run `claude status` with a 4s timeout — it can hang on network/TTY detection.
    let status_text = if let Some(bin) = resolve_lsp_bin("claude", &cwd) {
        let mut env_map = std::collections::HashMap::new();
        for key in &["HOME", "USER", "TMPDIR", "LANG"] {
            if let Ok(v) = std::env::var(key) { env_map.insert(key.to_string(), v); }
        }
        env_map.insert("PATH".to_string(), augmented_path(&cwd));
        env_map.insert("ANTHROPIC_API_KEY".to_string(), std::env::var("ANTHROPIC_API_KEY").unwrap_or_default());
        let (tx, rx) = std::sync::mpsc::channel::<String>();
        std::thread::spawn(move || {
            let out = std::process::Command::new(bin)
                .args(["status"])
                .envs(&env_map)
                .output()
                .map(|o| String::from_utf8_lossy(&o.stdout).to_string())
                .unwrap_or_default();
            let _ = tx.send(out);
        });
        rx.recv_timeout(std::time::Duration::from_secs(4)).unwrap_or_default()
    } else {
        String::new()
    };

    let info = ClaudeAccountInfo { email, display_name, organization_type, rate_limit_tier, status_text };
    *state.0.lock().unwrap() = Some(info.clone());
    Ok(info)
}

// ── Skills manager ────────────────────────────────────────────────────────────
// Claude skills live as <claude-dir>/skills/<name>/SKILL.md (YAML frontmatter with
// `name` + `description`). Disabling a skill renames its SKILL.md → SKILL.md.off so
// Claude stops loading it (the dir is ignored without a SKILL.md), reversibly.

#[derive(Serialize)]
struct SkillInfo {
    name: String,
    description: String,
    dir: String,
    enabled: bool,
}

/// Pull `name:`/`description:` out of a SKILL.md YAML frontmatter block. Plain line
/// scan — the frontmatter values are single-line in every skill we ship.
fn parse_skill_frontmatter(md: &str) -> (Option<String>, Option<String>) {
    let mut name = None;
    let mut desc = None;
    let mut in_fm = false;
    for (i, line) in md.lines().enumerate() {
        let t = line.trim();
        if t == "---" {
            if i == 0 { in_fm = true; continue; }
            if in_fm { break; }
        }
        if !in_fm { continue; }
        if let Some(v) = t.strip_prefix("name:") {
            name = Some(v.trim().trim_matches('"').to_string());
        } else if let Some(v) = t.strip_prefix("description:") {
            desc = Some(v.trim().trim_matches('"').to_string());
        }
    }
    (name, desc)
}

#[tauri::command]
fn list_skills(app: AppHandle) -> Vec<SkillInfo> {
    let mut out: Vec<SkillInfo> = Vec::new();
    let mut seen: std::collections::HashSet<String> = std::collections::HashSet::new();
    for cdir in &load_config_dirs(&app).claude {
        let skills_dir = Path::new(cdir).join("skills");
        let Ok(entries) = std::fs::read_dir(&skills_dir) else { continue };
        for e in entries.filter_map(|e| e.ok()) {
            if !e.file_type().map(|t| t.is_dir()).unwrap_or(false) { continue; }
            let dir = e.path();
            let on = dir.join("SKILL.md");
            let off = dir.join("SKILL.md.off");
            let (enabled, md_path) = if on.exists() {
                (true, on)
            } else if off.exists() {
                (false, off)
            } else {
                continue;
            };
            let dir_str = dir.to_string_lossy().into_owned();
            if !seen.insert(dir_str.clone()) { continue; }
            let md = std::fs::read_to_string(&md_path).unwrap_or_default();
            let (fm_name, fm_desc) = parse_skill_frontmatter(&md);
            out.push(SkillInfo {
                name: fm_name.unwrap_or_else(|| e.file_name().to_string_lossy().into_owned()),
                description: fm_desc.unwrap_or_default(),
                dir: dir_str,
                enabled,
            });
        }
    }
    out.sort_by(|a, b| a.name.to_lowercase().cmp(&b.name.to_lowercase()));
    out
}

#[tauri::command]
fn set_skill_enabled(dir: String, enabled: bool) -> Result<(), String> {
    let on = Path::new(&dir).join("SKILL.md");
    let off = Path::new(&dir).join("SKILL.md.off");
    if enabled {
        if off.exists() { std::fs::rename(&off, &on).map_err(|e| e.to_string())?; }
    } else if on.exists() {
        std::fs::rename(&on, &off).map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[tauri::command]
fn delete_skill(dir: String) -> Result<(), String> {
    // Guard: only remove a dir that actually looks like a skill.
    let p = Path::new(&dir);
    if !p.join("SKILL.md").exists() && !p.join("SKILL.md.off").exists() {
        return Err("not a skill directory".into());
    }
    std::fs::remove_dir_all(p).map_err(|e| e.to_string())
}

// ── MCP server manager ────────────────────────────────────────────────────────
// Claude Code stores MCP servers under the top-level `mcpServers` key of
// ~/.claude.json. serde_json's preserve_order feature keeps the rest of that large
// file byte-stable across the round-trip; we only touch `mcpServers`.

fn claude_json_path(app: &AppHandle) -> Result<std::path::PathBuf, String> {
    app.path().home_dir().map(|h| h.join(".claude.json")).map_err(|e| e.to_string())
}

fn read_claude_json(app: &AppHandle) -> Result<serde_json::Value, String> {
    let path = claude_json_path(app)?;
    let txt = std::fs::read_to_string(&path).unwrap_or_default();
    if txt.trim().is_empty() { return Ok(json!({})); }
    serde_json::from_str(&txt).map_err(|e| e.to_string())
}

fn write_claude_json(app: &AppHandle, root: &serde_json::Value) -> Result<(), String> {
    let path = claude_json_path(app)?;
    if let Ok(prev) = std::fs::read_to_string(&path) {
        if !prev.trim().is_empty() {
            let _ = std::fs::write(path.with_extension("json.burrow-bak"), &prev);
        }
    }
    let s = serde_json::to_string_pretty(root).map_err(|e| e.to_string())?;
    std::fs::write(&path, s).map_err(|e| e.to_string())
}

#[derive(Serialize)]
struct McpServer {
    name: String,
    /// The raw config object, re-serialized to a string for the frontend to display.
    config: String,
}

#[tauri::command]
fn list_mcp_servers(app: AppHandle) -> Result<Vec<McpServer>, String> {
    let root = read_claude_json(&app)?;
    let mut out = Vec::new();
    if let Some(servers) = root.get("mcpServers").and_then(|v| v.as_object()) {
        for (name, cfg) in servers {
            out.push(McpServer {
                name: name.clone(),
                config: serde_json::to_string_pretty(cfg).unwrap_or_default(),
            });
        }
    }
    Ok(out)
}

/// Add or replace an MCP server. `config` is the JSON object as a string (so the
/// frontend can hand over whatever shape the user typed — stdio command, http url…).
#[tauri::command]
fn add_mcp_server(app: AppHandle, name: String, config: String) -> Result<(), String> {
    let name = name.trim().to_string();
    if name.is_empty() { return Err("server name is required".into()); }
    let cfg: serde_json::Value = serde_json::from_str(config.trim())
        .map_err(|e| format!("config is not valid JSON: {e}"))?;
    if !cfg.is_object() { return Err("config must be a JSON object".into()); }

    let mut root = read_claude_json(&app)?;
    if !root.is_object() { return Err("~/.claude.json is not a JSON object".into()); }
    let obj = root.as_object_mut().unwrap();
    let servers = obj.entry("mcpServers").or_insert_with(|| json!({}));
    let Some(servers) = servers.as_object_mut() else {
        return Err("mcpServers is not an object".into());
    };
    servers.insert(name, cfg);
    write_claude_json(&app, &root)
}

#[tauri::command]
fn remove_mcp_server(app: AppHandle, name: String) -> Result<(), String> {
    let mut root = read_claude_json(&app)?;
    if let Some(servers) = root.get_mut("mcpServers").and_then(|v| v.as_object_mut()) {
        servers.remove(&name);
    }
    write_claude_json(&app, &root)
}

/// Save a base64-encoded image (pasted from the clipboard) to a temp file and
/// return its path, so the frontend can type that path into a PTY for an agent
/// (Claude Code et al.) to read. Drag-dropped files already have a real path, so
/// they skip this — only clipboard bytes need persisting.
#[tauri::command]
fn save_temp_image(b64: String, ext: String) -> Result<String, String> {
    let bytes = general_purpose::STANDARD
        .decode(b64.as_bytes())
        .map_err(|e| e.to_string())?;
    let nanos = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|e| e.to_string())?
        .as_nanos();
    let safe_ext: String = ext
        .chars()
        .filter(|c| c.is_ascii_alphanumeric())
        .take(5)
        .collect();
    let safe_ext = if safe_ext.is_empty() { "png".into() } else { safe_ext };
    let path = std::env::temp_dir().join(format!("burrow-paste-{nanos}.{safe_ext}"));
    std::fs::write(&path, &bytes).map_err(|e| e.to_string())?;
    Ok(path.to_string_lossy().to_string())
}

fn init_db(app: &AppHandle) -> Result<Connection, rusqlite::Error> {
    let data_dir = app.path().app_data_dir().expect("no app data dir");
    std::fs::create_dir_all(&data_dir).ok();
    let conn = Connection::open(data_dir.join("workspaces.db"))?;
    conn.execute_batch("PRAGMA foreign_keys = ON;")?;
    conn.execute_batch(
        "CREATE TABLE IF NOT EXISTS workspaces (
            id          INTEGER PRIMARY KEY AUTOINCREMENT,
            name        TEXT    NOT NULL,
            path        TEXT    NOT NULL UNIQUE,
            created_at  INTEGER NOT NULL,
            last_opened INTEGER
        );
        CREATE TABLE IF NOT EXISTS terminal_tabs (
            id           INTEGER PRIMARY KEY AUTOINCREMENT,
            workspace_id INTEGER NOT NULL,
            ord          INTEGER NOT NULL,
            title        TEXT,
            initial_cmd  TEXT,
            FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
        );
        CREATE TABLE IF NOT EXISTS mission_tasks (
            id           TEXT    PRIMARY KEY,
            workspace_id INTEGER,
            pty_id       INTEGER,
            title        TEXT,
            cwd          TEXT,
            model        TEXT,
            status       TEXT,
            turns        TEXT,
            created_at   INTEGER NOT NULL,
            FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
        );",
    )?;
    // Idempotent migrations: add new columns (ignored if already present)
    let _ = conn.execute_batch("ALTER TABLE terminal_tabs ADD COLUMN pty_id INTEGER");
    let _ = conn.execute_batch("ALTER TABLE terminal_tabs ADD COLUMN cwd TEXT");
    let _ = conn.execute_batch("ALTER TABLE workspaces ADD COLUMN parent_id INTEGER");
    let _ = conn.execute_batch("ALTER TABLE workspaces ADD COLUMN worktree_branch TEXT");
    // Existing rows predate folder-workspaces and were all git repos → default 1.
    let _ = conn.execute_batch("ALTER TABLE workspaces ADD COLUMN is_git INTEGER NOT NULL DEFAULT 1");
    let _ = conn.execute_batch("ALTER TABLE terminal_tabs ADD COLUMN default_title TEXT");
    let _ = conn.execute_batch("ALTER TABLE terminal_tabs ADD COLUMN session_id TEXT");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN handed_off INTEGER");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN profile_id TEXT");
    // Kanban board columns (docs/plans/mission-control-kanban.md §1.1) — mission_tasks
    // doubles as the board's task table rather than a parallel `board_tasks` table.
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN repo_workspace_id INTEGER");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN board_column TEXT NOT NULL DEFAULT 'backlog'");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN description TEXT");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN agent_kind TEXT");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN transport TEXT");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN use_worktree INTEGER NOT NULL DEFAULT 1");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN worktree_branch TEXT");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN task_workspace_id INTEGER");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN chat_id INTEGER");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN session_id TEXT");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN board_order REAL NOT NULL DEFAULT 0");
    let _ = conn.execute_batch("ALTER TABLE mission_tasks ADD COLUMN updated_at INTEGER");
    let _ = conn.execute_batch("ALTER TABLE workspaces ADD COLUMN icon TEXT");
    let _ = conn.execute_batch("ALTER TABLE workspaces ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0");
    conn.execute_batch(
        "CREATE TABLE IF NOT EXISTS task_attachments (
            id          INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id     TEXT NOT NULL,
            ord         INTEGER NOT NULL,
            mime_type   TEXT NOT NULL,
            file_path   TEXT NOT NULL,
            created_at  INTEGER NOT NULL,
            FOREIGN KEY(task_id) REFERENCES mission_tasks(id) ON DELETE CASCADE
        );",
    )?;
    Ok(conn)
}

// ── Git panel popout window ──────────────────────────────────────────────────

#[tauri::command]
fn open_git_panel_window(app: AppHandle) -> Result<(), String> {
    use tauri::{WebviewUrl, WebviewWindowBuilder};

    let label = "gitpanel";

    if let Some(win) = app.get_webview_window(label) {
        win.set_focus().map_err(|e| e.to_string())?;
        return Ok(());
    }

    let win = WebviewWindowBuilder::new(&app, label, WebviewUrl::App("index.html".into()))
        .title("Git")
        .inner_size(700.0, 520.0)
        .min_inner_size(500.0, 300.0)
        .resizable(true)
        .build()
        .map_err(|e| e.to_string())?;

    let _ = win;
    Ok(())
}

// ── Float window ─────────────────────────────────────────────────────────────

#[tauri::command]
fn open_float_window(
    app: AppHandle,
    float_params: State<FloatParamsState>,
    layout: State<FloatLayoutState>,
    pty_id: u32,
    title: String,
    ws_id: i64,
) -> Result<(), String> {
    use tauri::{WebviewUrl, WebviewWindowBuilder};

    let label = format!("float-{pty_id}");

    if let Some(win) = app.get_webview_window(&label) {
        win.set_focus().map_err(|e| e.to_string())?;
        return Ok(());
    }

    // Store params — float window retrieves via get_float_params on mount
    float_params.map.lock().unwrap().insert(
        label.clone(),
        FloatParams { pty_id, ws_id, title },
    );

    let win = WebviewWindowBuilder::new(&app, &label, WebviewUrl::App("index.html".into()))
        .title("")
        .inner_size(240.0, 36.0)
        .min_inner_size(180.0, 32.0)
        .always_on_top(true)
        .hidden_title(true)
        .title_bar_style(tauri::TitleBarStyle::Overlay)
        .transparent(true)
        .shadow(false)
        .resizable(true)
        .build()
        .map_err(|e| e.to_string())?;

    // Keep the window DECORATED (Overlay) so the standard window buttons exist —
    // tauri_plugin_decorum's on_window_ready installs a resize delegate that
    // re-runs position_traffic_lights() on every resize, and that derefs
    // close.superview() which is NULL on a borderless window → crash. With the
    // buttons present decorum is happy; we just hide them (setHidden is safe on a
    // non-null button, and decorum's delegate only repositions, never un-hides).
    #[cfg(target_os = "macos")]
    {
        use cocoa::appkit::{NSWindow, NSWindowButton, NSWindowCollectionBehavior};
        use cocoa::base::id;
        use objc::{msg_send, sel, sel_impl};
        if let Ok(ptr) = win.ns_window() {
            let ns_win = ptr as id;
            unsafe {
                for btn in [
                    NSWindowButton::NSWindowCloseButton,
                    NSWindowButton::NSWindowMiniaturizeButton,
                    NSWindowButton::NSWindowZoomButton,
                ] {
                    let b: id = ns_win.standardWindowButton_(btn);
                    if !b.is_null() {
                        let _: () = msg_send![b, setHidden: true];
                    }
                }
                // Float across ALL macOS Spaces (and stay visible in fullscreen
                // apps) — the bubble follows you to every desktop. Safe here: raw
                // NSWindow msg_send on the built window works (unlike decorum's
                // crashing traffic-light path).
                let behavior = ns_win.collectionBehavior()
                    | NSWindowCollectionBehavior::NSWindowCollectionBehaviorCanJoinAllSpaces
                    | NSWindowCollectionBehavior::NSWindowCollectionBehaviorFullScreenAuxiliary;
                ns_win.setCollectionBehavior_(behavior);
                // Kill the OS window shadow — a decorated window draws a square
                // shadow behind the round/transparent bubble. The pill/panel draw
                // their own CSS shadow instead.
                let _: () = msg_send![ns_win, setHasShadow: false];
            }
        }
    }

    // Register in the layout (collapsed-bar size) and stack/position it
    // deterministically at the configured corner — uses the tracked size, so no
    // off-screen race against the not-yet-realized window size.
    {
        let mut wins = layout.wins.lock().unwrap();
        if !wins.iter().any(|f| f.label == label) {
            wins.push(FloatWin { label: label.clone(), w: 240.0, h: 36.0 });
        }
    }
    reflow(&app, &layout);

    Ok(())
}

#[tauri::command]
fn set_window_size(
    app: AppHandle,
    label: String,
    width: f64,
    height: f64,
    layout: State<FloatLayoutState>,
) -> Result<(), String> {
    if let Some(win) = app.get_webview_window(&label) {
        // LOGICAL so it matches the layout math (Physical would halve on retina).
        win.set_size(tauri::Size::Logical(tauri::LogicalSize { width, height }))
            .map_err(|e| e.to_string())?;
    }
    // Track the new size + re-stack: a collapse/expand changes this window's
    // height, so everything below it shifts.
    {
        let mut wins = layout.wins.lock().unwrap();
        if let Some(e) = wins.iter_mut().find(|f| f.label == label) {
            e.w = width;
            e.h = height;
        }
    }
    reflow(&app, &layout);
    Ok(())
}

#[tauri::command]
fn get_float_params(
    label: String,
    float_params: State<FloatParamsState>,
) -> Option<FloatParams> {
    float_params.map.lock().unwrap().remove(&label)
}

// ── Float layout (corner snapping + vertical stacking) ───────────────────────

const FLOAT_SIDE_INSET: f64 = 12.0;
const FLOAT_TOP_INSET: f64 = 36.0; // clear the menu bar on the top corners
const FLOAT_BOTTOM_INSET: f64 = 14.0;
const FLOAT_GAP: f64 = 10.0;

/// The monitor a float layout should anchor to — the one the MAIN window is
/// currently on (not the system primary), so bubbles land on the screen the user
/// is actually looking at on a multi-monitor setup.
fn float_monitor(app: &AppHandle) -> Option<tauri::Monitor> {
    let w = app
        .get_webview_window("main")
        .or_else(|| app.webview_windows().into_values().next())?;
    w.current_monitor()
        .ok()
        .flatten()
        .or_else(|| w.primary_monitor().ok().flatten())
}

/// Re-stack every float window at the single configured corner, in insertion
/// order, offsetting by each window's tracked height + a gap. Uses the LOGICAL
/// sizes stored in FloatWin (not live queries) so it's correct the instant a
/// window is created.
fn reposition_floats(app: &AppHandle, corner: Corner, wins: &[FloatWin]) {
    let Some(mon) = float_monitor(app) else { return };
    let scale = mon.scale_factor();
    let msize = mon.size().to_logical::<f64>(scale);
    let mpos = mon.position().to_logical::<f64>(scale);

    let mut cum = 0.0;
    for fw in wins {
        let Some(win) = app.get_webview_window(&fw.label) else { continue };
        let (w, h) = (fw.w, fw.h);
        let x = match corner {
            Corner::TopLeft | Corner::BottomLeft => mpos.x + FLOAT_SIDE_INSET,
            Corner::TopRight | Corner::BottomRight => mpos.x + msize.width - w - FLOAT_SIDE_INSET,
        };
        let y = match corner {
            Corner::TopLeft | Corner::TopRight => mpos.y + FLOAT_TOP_INSET + cum,
            Corner::BottomLeft | Corner::BottomRight => {
                mpos.y + msize.height - FLOAT_BOTTOM_INSET - cum - h
            }
        };
        // Clamp fully on-screen (a manually-resized window can exceed its slot;
        // never let it spill off the monitor edge).
        let x = x.max(mpos.x).min((mpos.x + msize.width - w).max(mpos.x));
        let y = y.max(mpos.y).min((mpos.y + msize.height - h).max(mpos.y));
        let _ = win.set_position(tauri::LogicalPosition::new(x, y));
        cum += h + FLOAT_GAP;
    }
}

/// Re-stack using the currently-configured corner + tracked window list.
fn reflow(app: &AppHandle, layout: &FloatLayoutState) {
    let corner = *layout.corner.lock().unwrap();
    let wins = layout.wins.lock().unwrap().clone();
    reposition_floats(app, corner, &wins);
}

/// Setting changed (or initial sync): set the corner all floats snap to + re-stack.
#[tauri::command]
fn set_float_corner(app: AppHandle, corner: String, layout: State<FloatLayoutState>) {
    *layout.corner.lock().unwrap() = Corner::from_str(&corner);
    reflow(&app, &layout);
}

/// Drag-end: the corner is fixed by the Setting, so just return the window to its
/// stack slot at the configured corner.
#[tauri::command]
fn snap_float_window(app: AppHandle, label: String, layout: State<FloatLayoutState>) {
    let _ = label;
    reflow(&app, &layout);
}

/// Called on manual window resize: read the window's real size into the layout
/// (otherwise stacking uses a stale size → overlap/overflow) and re-stack.
#[tauri::command]
fn sync_float_size(app: AppHandle, label: String, layout: State<FloatLayoutState>) {
    let Some(win) = app.get_webview_window(&label) else { return };
    let scale = win.scale_factor().unwrap_or(1.0);
    let Ok(sz) = win.inner_size() else { return };
    let lsz = sz.to_logical::<f64>(scale);
    {
        let mut wins = layout.wins.lock().unwrap();
        if let Some(e) = wins.iter_mut().find(|f| f.label == label) {
            e.w = lsz.width;
            e.h = lsz.height;
        }
    }
    reflow(&app, &layout);
}

#[tauri::command]
fn close_float_window(app: AppHandle, label: String, layout: State<FloatLayoutState>) {
    layout.wins.lock().unwrap().retain(|f| f.label != label);
    if let Some(win) = app.get_webview_window(&label) {
        let _ = win.close();
    }
    reflow(&app, &layout);
}

/// Snapshot handshake routed through Rust app.emit (frontend→frontend `emit`
/// does NOT reliably cross windows; app.emit reaches every window — proven by the
/// pty-hook channel). Float asks; the main XTerm for this pty answers via
/// send_float_snapshot.
#[tauri::command]
fn request_float_snapshot(app: AppHandle, pty_id: u32) {
    let _ = app.emit(&format!("float-snap-req-{pty_id}"), ());
}

#[tauri::command]
fn send_float_snapshot(app: AppHandle, pty_id: u32, data: String, cols: u16, rows: u16) {
    let _ = app.emit(
        &format!("float-snap-{pty_id}"),
        json!({ "data": data, "cols": cols, "rows": rows }),
    );
}

/// Source terminal resized → tell its float mirror the new grid dims so it can
/// match (the shared PTY's resize already triggers the agent to repaint).
#[tauri::command]
fn notify_float_grid(app: AppHandle, pty_id: u32, cols: u16, rows: u16) {
    let _ = app.emit(&format!("float-grid-{pty_id}"), json!({ "cols": cols, "rows": rows }));
}

// ── Mission Control (tank-style task dashboard) ──────────────────────────────
// A separate Tauri window that drives headless `claude` PTYs as disposable
// "tasks": spawn from a prompt, watch status dots via the global hook server
// (pty-hook-{id}), attach a live terminal on demand, and read the final
// assistant response straight from Claude's JSONL transcript on Stop — the
// same model as the `tank` project, but on Burrow's native PTY instead of tmux.

// Mission Control is now an in-window view (ui.mode === 'mission'), not a
// separate window — so there's no window-builder command. Tasks persist in the
// shared workspaces.db (mission_tasks table), keyed to a workspace like every
// other Burrow feature.

#[derive(Debug, Default, Serialize, Deserialize, Clone)]
pub struct MissionTask {
    pub id: String,
    pub workspace_id: Option<i64>,
    pub pty_id: Option<i64>,
    pub title: Option<String>,
    pub cwd: Option<String>,
    pub model: Option<String>,
    pub status: Option<String>,
    /// JSON array of {role,text} turns — stored as text, parsed by the frontend.
    pub turns: Option<String>,
    pub created_at: i64,
    /// 1 if the task's live PTY was handed off to a real terminal tab (the tab now
    /// owns input; Mission Control keeps tracking status read-only). NULL/0 = no.
    #[serde(default)]
    pub handed_off: Option<i64>,
    /// Id of the Claude profile (config dir + binary + args) this task launched
    /// with — so `--resume` reuses the same CLAUDE_CONFIG_DIR (sessions are stored
    /// per config dir). NULL = the default profile.
    #[serde(default)]
    pub profile_id: Option<String>,
    // ── Kanban board fields (docs/plans/mission-control-kanban.md §1.1) ────────
    /// Root repo workspace id (parent_id climbed) — the board is keyed by this,
    /// not by `workspace_id`/`task_workspace_id`, so a task shows up regardless
    /// of which worktree it's currently running in.
    #[serde(default)]
    pub repo_workspace_id: Option<i64>,
    /// 'backlog' | 'todo' | 'in_progress' | 'for_review' | 'done'
    #[serde(default)]
    pub board_column: Option<String>,
    /// Markdown body shown on the Backlog card before any agent exists.
    #[serde(default)]
    pub description: Option<String>,
    /// 'claude' | 'codex' | 'aider' | ... (chatAgents registry id).
    #[serde(default)]
    pub agent_kind: Option<String>,
    /// 'pty' | 'acp' | 'stream-json' | NULL (no agent yet).
    #[serde(default)]
    pub transport: Option<String>,
    /// 0 = work directly on the current branch, no worktree. Defaults to 1.
    #[serde(default)]
    pub use_worktree: Option<i64>,
    #[serde(default)]
    pub worktree_branch: Option<String>,
    /// The workspace row the agent actually runs in (== repo_workspace_id if
    /// use_worktree=0, else the worktree's workspace id).
    #[serde(default)]
    pub task_workspace_id: Option<i64>,
    /// claudeChats.ts session id, when transport='acp'/'stream-json'.
    #[serde(default)]
    pub chat_id: Option<i64>,
    /// Agent-native session id, for --resume across transport switches.
    #[serde(default)]
    pub session_id: Option<String>,
    /// Float order for drag-reorder within a column.
    #[serde(default)]
    pub board_order: Option<f64>,
    #[serde(default)]
    pub updated_at: Option<i64>,
}

#[tauri::command]
fn list_mission_tasks(db: State<DbState>) -> Result<Vec<MissionTask>, String> {
    let conn = db.conn.lock().unwrap();
    let mut stmt = conn
        .prepare("SELECT id, workspace_id, pty_id, title, cwd, model, status, turns, created_at, handed_off, profile_id FROM mission_tasks ORDER BY created_at ASC")
        .map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map([], |row| Ok(MissionTask {
            id: row.get(0)?,
            workspace_id: row.get(1)?,
            pty_id: row.get(2)?,
            title: row.get(3)?,
            cwd: row.get(4)?,
            model: row.get(5)?,
            status: row.get(6)?,
            turns: row.get(7)?,
            created_at: row.get(8)?,
            handed_off: row.get(9)?,
            profile_id: row.get(10)?,
            ..Default::default()
        }))
        .map_err(|e| e.to_string())?
        .filter_map(|r| r.ok())
        .collect();
    Ok(rows)
}

#[tauri::command]
fn upsert_mission_task(task: MissionTask, db: State<DbState>) -> Result<(), String> {
    let conn = db.conn.lock().unwrap();
    conn.execute(
        "INSERT INTO mission_tasks (id, workspace_id, pty_id, title, cwd, model, status, turns, created_at, handed_off, profile_id)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11)
         ON CONFLICT(id) DO UPDATE SET
            workspace_id=excluded.workspace_id, pty_id=excluded.pty_id, title=excluded.title,
            cwd=excluded.cwd, model=excluded.model, status=excluded.status, turns=excluded.turns,
            handed_off=excluded.handed_off, profile_id=excluded.profile_id",
        rusqlite::params![task.id, task.workspace_id, task.pty_id, task.title, task.cwd, task.model, task.status, task.turns, task.created_at, task.handed_off, task.profile_id],
    ).map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn delete_mission_task(id: String, db: State<DbState>) -> Result<(), String> {
    let conn = db.conn.lock().unwrap();
    conn.execute("DELETE FROM mission_tasks WHERE id = ?1", rusqlite::params![id])
        .map_err(|e| e.to_string())?;
    Ok(())
}

// ── Kanban board (docs/plans/mission-control-kanban.md) ─────────────────────
// mission_tasks doubles as the board's task table (§0/§1.1) — these commands
// read/write the full extended row, keyed by repo_workspace_id rather than
// workspace_id so a task shows up regardless of which worktree it's running
// in. Column-move policy: `move_board_task` is a raw Tauri command, reachable
// ONLY from frontend JS (drag-drop / TaskDetail.vue) — there is no path for a
// CLI-driven agent to invoke a Tauri command directly, so no `done`-guard is
// needed here. The equivalent guard for the agent-facing path lives in
// `take_spawn_requests`'s "board-move" arm, which rejects `column == "done"`
// outright (agents may move a card up to `for_review`; only a human/Manager
// via the UI may mark it `done`).

const BOARD_TASK_COLS: &str = "id, workspace_id, pty_id, title, cwd, model, status, turns, created_at, handed_off, profile_id, repo_workspace_id, board_column, description, agent_kind, transport, use_worktree, worktree_branch, task_workspace_id, chat_id, session_id, board_order, updated_at";

fn row_to_board_task(row: &rusqlite::Row) -> rusqlite::Result<MissionTask> {
    Ok(MissionTask {
        id: row.get(0)?,
        workspace_id: row.get(1)?,
        pty_id: row.get(2)?,
        title: row.get(3)?,
        cwd: row.get(4)?,
        model: row.get(5)?,
        status: row.get(6)?,
        turns: row.get(7)?,
        created_at: row.get(8)?,
        handed_off: row.get(9)?,
        profile_id: row.get(10)?,
        repo_workspace_id: row.get(11)?,
        board_column: row.get(12)?,
        description: row.get(13)?,
        agent_kind: row.get(14)?,
        transport: row.get(15)?,
        use_worktree: row.get(16)?,
        worktree_branch: row.get(17)?,
        task_workspace_id: row.get(18)?,
        chat_id: row.get(19)?,
        session_id: row.get(20)?,
        board_order: row.get(21)?,
        updated_at: row.get(22)?,
    })
}

fn now_millis() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

/// Board columns in the fixed pipeline order (docs/plans/mission-control-kanban.md §1.1/§7).
const BOARD_COLUMNS: [&str; 5] = ["backlog", "todo", "in_progress", "for_review", "done"];

#[tauri::command]
fn list_board_tasks(repo_workspace_id: i64, db: State<DbState>) -> Result<Vec<MissionTask>, String> {
    let conn = db.conn.lock().unwrap();
    let sql = format!(
        "SELECT {BOARD_TASK_COLS} FROM mission_tasks WHERE repo_workspace_id = ?1 ORDER BY board_column ASC, board_order ASC, created_at ASC"
    );
    let mut stmt = conn.prepare(&sql).map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map(rusqlite::params![repo_workspace_id], row_to_board_task)
        .map_err(|e| e.to_string())?
        .filter_map(|r| r.ok())
        .collect();
    Ok(rows)
}

#[tauri::command]
fn upsert_board_task(task: MissionTask, db: State<DbState>) -> Result<MissionTask, String> {
    let conn = db.conn.lock().unwrap();
    let board_column = task.board_column.clone().unwrap_or_else(|| "backlog".to_string());
    let use_worktree = task.use_worktree.unwrap_or(1);
    let board_order = task.board_order.unwrap_or(0.0);
    let updated_at = now_millis();
    conn.execute(
        "INSERT INTO mission_tasks (
            id, workspace_id, pty_id, title, cwd, model, status, turns, created_at, handed_off, profile_id,
            repo_workspace_id, board_column, description, agent_kind, transport, use_worktree, worktree_branch,
            task_workspace_id, chat_id, session_id, board_order, updated_at
         )
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18, ?19, ?20, ?21, ?22, ?23)
         ON CONFLICT(id) DO UPDATE SET
            workspace_id=excluded.workspace_id, pty_id=excluded.pty_id, title=excluded.title,
            cwd=excluded.cwd, model=excluded.model, status=excluded.status, turns=excluded.turns,
            handed_off=excluded.handed_off, profile_id=excluded.profile_id,
            repo_workspace_id=excluded.repo_workspace_id, board_column=excluded.board_column,
            description=excluded.description, agent_kind=excluded.agent_kind, transport=excluded.transport,
            use_worktree=excluded.use_worktree, worktree_branch=excluded.worktree_branch,
            task_workspace_id=excluded.task_workspace_id, chat_id=excluded.chat_id,
            session_id=excluded.session_id, board_order=excluded.board_order, updated_at=excluded.updated_at",
        rusqlite::params![
            task.id, task.workspace_id, task.pty_id, task.title, task.cwd, task.model, task.status, task.turns,
            task.created_at, task.handed_off, task.profile_id, task.repo_workspace_id, board_column,
            task.description, task.agent_kind, task.transport, use_worktree, task.worktree_branch,
            task.task_workspace_id, task.chat_id, task.session_id, board_order, updated_at,
        ],
    ).map_err(|e| e.to_string())?;
    let sql = format!("SELECT {BOARD_TASK_COLS} FROM mission_tasks WHERE id = ?1");
    conn.query_row(&sql, rusqlite::params![task.id], row_to_board_task)
        .map_err(|e| e.to_string())
}

/// Narrow column-move used by both UI drag-drop (this Tauri command, called
/// directly from the frontend) and the pure-Rust "board-move" arm of
/// `take_spawn_requests` (which itself validates the column and enforces the
/// agent-can't-mark-done rule before ever calling into DB logic like this).
fn move_board_task_db(conn: &Connection, task_id: &str, column: &str, order: f64) -> Result<(), String> {
    if !BOARD_COLUMNS.contains(&column) {
        return Err(format!("invalid board column '{column}'"));
    }
    let updated_at = now_millis();
    conn.execute(
        "UPDATE mission_tasks SET board_column = ?1, board_order = ?2, updated_at = ?3 WHERE id = ?4",
        rusqlite::params![column, order, updated_at, task_id],
    ).map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
fn move_board_task(task_id: String, column: String, order: f64, db: State<DbState>, app: AppHandle) -> Result<(), String> {
    {
        let conn = db.conn.lock().unwrap();
        move_board_task_db(&conn, &task_id, &column, order)?;
    }
    let _ = app.emit("board-task-moved", json!({ "taskId": task_id, "column": column }));
    Ok(())
}

/// Unlinks/orphans the task's live pty/chat/worktree rather than killing or
/// deleting them — they keep existing as an ordinary standalone terminal tab
/// / chat / workspace the user can still deal with manually (docs/plans/
/// mission-control-kanban.md §9.5, "detach don't destroy"). Only the board
/// row (and its attachment rows/files) are removed.
#[tauri::command]
fn delete_board_task(task_id: String, db: State<DbState>, app: AppHandle) -> Result<(), String> {
    let attachments = {
        let conn = db.conn.lock().unwrap();
        let mut stmt = conn
            .prepare("SELECT file_path FROM task_attachments WHERE task_id = ?1")
            .map_err(|e| e.to_string())?;
        let paths: Vec<String> = stmt
            .query_map(rusqlite::params![task_id], |r| r.get::<_, String>(0))
            .map_err(|e| e.to_string())?
            .filter_map(|r| r.ok())
            .collect();
        conn.execute("DELETE FROM mission_tasks WHERE id = ?1", rusqlite::params![task_id])
            .map_err(|e| e.to_string())?;
        // FK ON DELETE CASCADE removes the task_attachments rows; still need to
        // clean up the files themselves (best-effort).
        paths
    };
    for p in attachments {
        let _ = std::fs::remove_file(p);
    }
    let _ = app.emit("board-task-moved", json!({ "taskId": task_id, "column": null }));
    Ok(())
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct TaskAttachment {
    pub id: i64,
    pub task_id: String,
    pub ord: i64,
    pub mime_type: String,
    pub file_path: String,
    pub created_at: i64,
}

fn attachment_ext_for_mime(mime: &str) -> &'static str {
    match mime {
        "image/png" => "png",
        "image/jpeg" | "image/jpg" => "jpg",
        "image/gif" => "gif",
        "image/webp" => "webp",
        "image/svg+xml" => "svg",
        _ => "bin",
    }
}

#[tauri::command]
fn write_task_attachment(task_id: String, base64_data: String, mime_type: String, app: AppHandle, db: State<DbState>) -> Result<String, String> {
    let bytes = general_purpose::STANDARD
        .decode(base64_data.as_bytes())
        .map_err(|e| e.to_string())?;
    let data_dir = app.path().app_data_dir().map_err(|e| e.to_string())?;
    let dir = data_dir.join("attachments").join(&task_id);
    std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;

    let conn = db.conn.lock().unwrap();
    let next_ord: i64 = conn
        .query_row(
            "SELECT COALESCE(MAX(ord), -1) + 1 FROM task_attachments WHERE task_id = ?1",
            rusqlite::params![task_id],
            |r| r.get(0),
        )
        .map_err(|e| e.to_string())?;
    let ext = attachment_ext_for_mime(&mime_type);
    let path = dir.join(format!("{next_ord}.{ext}"));
    std::fs::write(&path, &bytes).map_err(|e| e.to_string())?;
    let path_str = path.to_string_lossy().to_string();
    let created_at = now_millis();
    conn.execute(
        "INSERT INTO task_attachments (task_id, ord, mime_type, file_path, created_at) VALUES (?1, ?2, ?3, ?4, ?5)",
        rusqlite::params![task_id, next_ord, mime_type, path_str, created_at],
    ).map_err(|e| e.to_string())?;
    Ok(path_str)
}

#[tauri::command]
fn list_task_attachments(task_id: String, db: State<DbState>) -> Result<Vec<TaskAttachment>, String> {
    let conn = db.conn.lock().unwrap();
    let mut stmt = conn
        .prepare("SELECT id, task_id, ord, mime_type, file_path, created_at FROM task_attachments WHERE task_id = ?1 ORDER BY ord ASC")
        .map_err(|e| e.to_string())?;
    let rows = stmt
        .query_map(rusqlite::params![task_id], |row| Ok(TaskAttachment {
            id: row.get(0)?,
            task_id: row.get(1)?,
            ord: row.get(2)?,
            mime_type: row.get(3)?,
            file_path: row.get(4)?,
            created_at: row.get(5)?,
        }))
        .map_err(|e| e.to_string())?
        .filter_map(|r| r.ok())
        .collect();
    Ok(rows)
}

#[tauri::command]
fn delete_task_attachment(attachment_id: i64, db: State<DbState>) -> Result<(), String> {
    let conn = db.conn.lock().unwrap();
    let path: Option<String> = conn
        .query_row(
            "SELECT file_path FROM task_attachments WHERE id = ?1",
            rusqlite::params![attachment_id],
            |r| r.get(0),
        )
        .ok();
    conn.execute("DELETE FROM task_attachments WHERE id = ?1", rusqlite::params![attachment_id])
        .map_err(|e| e.to_string())?;
    if let Some(p) = path {
        let _ = std::fs::remove_file(p);
    }
    Ok(())
}

#[tauri::command]
fn read_task_attachment_base64(attachment_id: i64, db: State<DbState>) -> Result<(String, String), String> {
    let (path, mime): (String, String) = {
        let conn = db.conn.lock().unwrap();
        conn.query_row(
            "SELECT file_path, mime_type FROM task_attachments WHERE id = ?1",
            rusqlite::params![attachment_id],
            |r| Ok((r.get(0)?, r.get(1)?)),
        ).map_err(|e| e.to_string())?
    };
    let bytes = std::fs::read(&path).map_err(|e| e.to_string())?;
    Ok((general_purpose::STANDARD.encode(&bytes), mime))
}

/// Claude stores each session transcript at
/// `<config>/projects/<slug>/<session_id>.jsonl`, where `<config>` is `~/.claude`
/// by default (or a profile's CLAUDE_CONFIG_DIR) and `<slug>` is the cwd with `/`
/// and `.` replaced by `-`. Mirror that mapping. `config_dir` empty/None → default.
fn claude_transcript_path(cwd: &str, session_id: &str, config_dir: Option<&str>) -> Option<std::path::PathBuf> {
    let base = match config_dir {
        Some(d) if !d.trim().is_empty() => expand_tilde_home(d.trim()),
        _ => dirs_home()?.join(".claude"),
    };
    let slug: String = cwd
        .chars()
        .map(|c| if c == '/' || c == '.' { '-' } else { c })
        .collect();
    Some(base.join("projects").join(slug).join(format!("{session_id}.jsonl")))
}

fn dirs_home() -> Option<std::path::PathBuf> {
    std::env::var_os("HOME").map(std::path::PathBuf::from)
}

/// Expand a leading `~` to $HOME (profile config dirs are often typed `~/.claude-x`).
/// Distinct from `expand_tilde` (which needs an AppHandle + returns String).
fn expand_tilde_home(p: &str) -> std::path::PathBuf {
    if let Some(rest) = p.strip_prefix("~/") {
        if let Some(home) = dirs_home() {
            return home.join(rest);
        }
    }
    if p == "~" {
        if let Some(home) = dirs_home() {
            return home;
        }
    }
    std::path::PathBuf::from(p)
}

/// Read the last assistant text block from a task's Claude transcript — the
/// task's "result", captured when the Stop hook fires (mirrors tank's
/// read_last_assistant_text). Returns "" if not found yet.
#[tauri::command]
fn read_claude_result(cwd: String, session_id: String, config_dir: Option<String>) -> String {
    let Some(path) = claude_transcript_path(&cwd, &session_id, config_dir.as_deref()) else { return String::new() };
    let Ok(content) = std::fs::read_to_string(&path) else { return String::new() };
    for line in content.lines().rev() {
        let Ok(msg) = serde_json::from_str::<serde_json::Value>(line) else { continue };
        if msg.get("type").and_then(|v| v.as_str()) != Some("assistant") {
            continue;
        }
        let content = &msg["message"]["content"];
        if let Some(s) = content.as_str() {
            return s.to_string();
        }
        if let Some(arr) = content.as_array() {
            for block in arr {
                if block.get("type").and_then(|v| v.as_str()) == Some("text") {
                    return block.get("text").and_then(|v| v.as_str()).unwrap_or("").to_string();
                }
            }
        }
    }
    String::new()
}

#[derive(Debug, Serialize)]
pub struct ApiError {
    pub status: Option<i64>,
    pub message: String,
}

#[derive(Debug, Serialize)]
pub struct ClaudeOutcome {
    /// Last assistant text block (the task's result).
    pub text: String,
    /// Present when the last assistant message is a synthetic API-error record
    /// (claude tags `isApiErrorMessage` after exhausting retries on 429/529/etc).
    /// Mirrors tank's read_last_api_error — lets the UI flag a rate-limited task
    /// as `error` instead of falsely reporting it `done`.
    pub error: Option<ApiError>,
}

/// Read the last assistant message's text AND whether it's an API error — in one
/// pass over the transcript. Used by Mission Control on a task's `done`.
#[tauri::command]
fn read_claude_outcome(cwd: String, session_id: String, config_dir: Option<String>) -> ClaudeOutcome {
    let empty = ClaudeOutcome { text: String::new(), error: None };
    let Some(path) = claude_transcript_path(&cwd, &session_id, config_dir.as_deref()) else { return empty };
    let Ok(content) = std::fs::read_to_string(&path) else { return empty };
    for line in content.lines().rev() {
        let Ok(msg) = serde_json::from_str::<serde_json::Value>(line) else { continue };
        if msg.get("type").and_then(|v| v.as_str()) != Some("assistant") {
            continue;
        }
        // Extract the text (string content or first text block).
        let mut text = String::new();
        let body = &msg["message"]["content"];
        if let Some(s) = body.as_str() {
            text = s.to_string();
        } else if let Some(arr) = body.as_array() {
            for block in arr {
                if block.get("type").and_then(|v| v.as_str()) == Some("text") {
                    text = block.get("text").and_then(|v| v.as_str()).unwrap_or("").to_string();
                    break;
                }
            }
        }
        let error = if msg.get("isApiErrorMessage").and_then(|v| v.as_bool()) == Some(true) {
            Some(ApiError {
                status: msg.get("apiErrorStatus").and_then(|v| v.as_i64()),
                message: text.clone(),
            })
        } else {
            None
        };
        return ClaudeOutcome { text, error };
    }
    empty
}

#[derive(Debug, Serialize)]
pub struct ClaudeActivity {
    /// True if the transcript exists at all (claude got far enough to write it).
    pub exists: bool,
    /// ms since the transcript file was last modified (0 if unknown). A turn that's
    /// actively running keeps appending lines, so a large idle gap = claude is
    /// parked at its prompt.
    pub idle_ms: u64,
    /// True when the LAST transcript entry is an assistant message that ends the
    /// turn — a text reply with no trailing tool_use awaiting a result. Mid-turn the
    /// tail is a tool_use (assistant) or a tool_result (user), so this is false.
    pub turn_ended: bool,
    /// True when the tail is an assistant message whose pending tool call BLOCKS on
    /// the operator (AskUserQuestion / ExitPlanMode) — i.e. "waiting for you", not
    /// "running" and not "done". Mirrors tank's BLOCKING_TOOLS gate.
    pub awaiting_input: bool,
    /// True if the final assistant message is a synthetic API-error record.
    pub is_error: bool,
}

/// Hook-independent status fallback for Mission Control. The JSONL transcript is the
/// source of truth tank reads; a missed `Stop` hook (or a task that finished while
/// the UI wasn't listening, e.g. across a restart) would otherwise leave a task stuck
/// on `running`. We expose enough signal for the frontend to flip running→done once
/// the transcript has gone idle on a finished turn.
#[tauri::command]
fn read_claude_activity(cwd: String, session_id: String, config_dir: Option<String>) -> ClaudeActivity {
    let none = ClaudeActivity { exists: false, idle_ms: 0, turn_ended: false, awaiting_input: false, is_error: false };
    let Some(path) = claude_transcript_path(&cwd, &session_id, config_dir.as_deref()) else { return none };
    let Ok(meta) = std::fs::metadata(&path) else { return none };
    let idle_ms = meta
        .modified()
        .ok()
        .and_then(|m| m.elapsed().ok())
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0);
    let Ok(content) = std::fs::read_to_string(&path) else {
        return ClaudeActivity { exists: true, idle_ms, turn_ended: false, awaiting_input: false, is_error: false };
    };
    // Walk from the end to the last real message entry.
    let mut turn_ended = false;
    let mut awaiting_input = false;
    let mut is_error = false;
    for line in content.lines().rev() {
        let Ok(msg) = serde_json::from_str::<serde_json::Value>(line) else { continue };
        let ty = msg.get("type").and_then(|v| v.as_str()).unwrap_or("");
        if ty != "assistant" && ty != "user" {
            continue; // skip summaries/system/etc.
        }
        if ty == "assistant" {
            let body = &msg["message"]["content"];
            let mut has_text = false;
            let mut has_tool_use = false;
            if body.as_str().is_some() {
                has_text = true;
            } else if let Some(arr) = body.as_array() {
                for block in arr {
                    match block.get("type").and_then(|v| v.as_str()) {
                        Some("text") => has_text = true,
                        Some("tool_use") => {
                            has_tool_use = true;
                            // A pending BLOCKING tool call = waiting on the operator.
                            if matches!(
                                block.get("name").and_then(|v| v.as_str()),
                                Some("AskUserQuestion") | Some("ExitPlanMode")
                            ) {
                                awaiting_input = true;
                            }
                        }
                        _ => {}
                    }
                }
            }
            // Turn is over only when the tail is a text reply with no pending tool call.
            turn_ended = has_text && !has_tool_use;
            is_error = msg.get("isApiErrorMessage").and_then(|v| v.as_bool()) == Some(true);
        }
        // ty == "user" (a tool_result or a fresh prompt) → mid-turn, all flags stay false.
        break;
    }
    ClaudeActivity { exists: true, idle_ms, turn_ended, awaiting_input, is_error }
}

#[derive(serde::Serialize, Clone)]
struct ClaudeSessionInfo {
    session_id: String,
    first_message: String,
    updated_at: String, // ISO 8601
}

#[tauri::command]
fn list_claude_sessions(
    app: AppHandle,
    cwd: String,
    config_dir: Option<String>,
) -> Vec<ClaudeSessionInfo> {
    use std::io::{BufRead, BufReader};

    // Resolve projects dir (same logic as claude_usage_5h)
    let projects_dir = if let Some(cd) = config_dir.as_deref().filter(|s| !s.is_empty()) {
        let expanded = if cd.starts_with("~/") {
            app.path().home_dir().ok()
                .map(|h| format!("{}{}", h.display(), &cd[1..]))
                .unwrap_or_else(|| cd.to_string())
        } else { cd.to_string() };
        let base = std::path::Path::new(&expanded);
        let direct = base.join("projects");
        if direct.is_dir() { direct } else { base.join(".claude/projects") }
    } else {
        match app.path().home_dir() {
            Ok(h) => h.join(".claude/projects"),
            Err(_) => return vec![],
        }
    };

    // Derive the project slug from cwd (same convention Claude uses)
    let slug = cwd.replace(['/', '\\', ':'], "-").trim_start_matches('-').to_string();
    let project_dir = projects_dir.join(&slug);
    if !project_dir.is_dir() { return vec![]; }

    let Ok(entries) = std::fs::read_dir(&project_dir) else { return vec![]; };
    let mut results: Vec<ClaudeSessionInfo> = entries
        .flatten()
        .filter_map(|e| {
            let path = e.path();
            if path.extension().and_then(|x| x.to_str()) != Some("jsonl") { return None; }
            let session_id = path.file_stem()?.to_str()?.to_string();
            // mtime as updated_at
            let updated_at = path.metadata().ok()
                .and_then(|m| m.modified().ok())
                .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                .map(|d| {
                    // Simple ISO8601 from unix seconds — no chrono needed
                    let secs = d.as_secs();
                    let s = secs % 60; let m = (secs / 60) % 60;
                    let h = (secs / 3600) % 24; let days = secs / 86400;
                    // Approximate date calculation (good enough for display)
                    format!("{days}d {h:02}:{m:02}:{s:02}") // rough display
                })
                .unwrap_or_default();
            // Read first user message
            let f = std::fs::File::open(&path).ok()?;
            let first_message = BufReader::new(f).lines().flatten()
                .find_map(|line| {
                    let v: serde_json::Value = serde_json::from_str(&line).ok()?;
                    if v["role"].as_str() == Some("user") {
                        let content = &v["content"];
                        if let Some(arr) = content.as_array() {
                            return arr.iter().find_map(|c| {
                                if c["type"].as_str() == Some("text") {
                                    c["text"].as_str().map(|t| t.chars().take(80).collect::<String>())
                                } else { None }
                            });
                        }
                        if let Some(s) = content.as_str() {
                            return Some(s.chars().take(80).collect());
                        }
                    }
                    None
                })
                .unwrap_or_else(|| "(empty)".to_string());
            Some(ClaudeSessionInfo { session_id, first_message, updated_at })
        })
        .collect();

    // Sort by mtime desc — re-read metadata for sorting
    results.sort_by(|a, b| b.updated_at.cmp(&a.updated_at));
    results.truncate(20); // top 20 recent sessions
    results
}

/// One rendered line of a transcript: a single content block flattened out of the
/// JSONL so the frontend can render the FULL conversation (not just the captured
/// user/assistant turns Mission Control tracks live). `kind` distinguishes text,
/// thinking, tool calls and their results so the UI can style each differently.
#[derive(Debug, Serialize)]
pub struct TranscriptEntry {
    /// "user" | "assistant" | "system"
    pub role: String,
    /// "text" | "thinking" | "tool_use" | "tool_result" | "image"
    pub kind: String,
    /// The body: message text, thinking text, tool input (pretty JSON), or tool result.
    pub text: String,
    /// Tool name for `tool_use`; the result's tool name for `tool_result` (when known).
    pub tool: Option<String>,
    /// True for a failed tool_result or a synthetic API-error assistant message.
    #[serde(default)]
    pub is_error: bool,
    /// ISO timestamp of the source message, if present.
    pub ts: Option<String>,
}

/// Stringify a tool_result `content` (string, or array of text/image blocks).
fn stringify_result_content(v: &serde_json::Value) -> (String, bool) {
    if let Some(s) = v.as_str() {
        return (s.to_string(), false);
    }
    if let Some(arr) = v.as_array() {
        let mut out = String::new();
        let mut has_image = false;
        for block in arr {
            match block.get("type").and_then(|b| b.as_str()) {
                Some("text") => {
                    if !out.is_empty() { out.push('\n'); }
                    out.push_str(block.get("text").and_then(|t| t.as_str()).unwrap_or(""));
                }
                Some("image") => has_image = true,
                _ => {}
            }
        }
        if has_image && out.is_empty() {
            out.push_str("[image]");
        }
        return (out, false);
    }
    (String::new(), false)
}

/// Parse a session's full JSONL transcript into a flat, render-ready list of entries.
/// Used by Mission Control's "Full transcript" dialog. Returns [] if not found yet.
#[tauri::command]
fn read_claude_transcript(cwd: String, session_id: String, config_dir: Option<String>) -> Vec<TranscriptEntry> {
    let mut out: Vec<TranscriptEntry> = Vec::new();
    let Some(path) = claude_transcript_path(&cwd, &session_id, config_dir.as_deref()) else { return out };
    let Ok(content) = std::fs::read_to_string(&path) else { return out };
    for line in content.lines() {
        let Ok(msg) = serde_json::from_str::<serde_json::Value>(line) else { continue };
        let ty = msg.get("type").and_then(|v| v.as_str()).unwrap_or("");
        if ty != "user" && ty != "assistant" {
            continue; // skip summaries / system meta
        }
        // Meta entries (e.g. injected reminders) aren't real conversation.
        if msg.get("isMeta").and_then(|v| v.as_bool()) == Some(true) {
            continue;
        }
        let ts = msg.get("timestamp").and_then(|v| v.as_str()).map(|s| s.to_string());
        let is_api_err = msg.get("isApiErrorMessage").and_then(|v| v.as_bool()) == Some(true);
        let body = &msg["message"]["content"];
        // Plain-string content (most user prompts).
        if let Some(s) = body.as_str() {
            if !s.is_empty() {
                out.push(TranscriptEntry {
                    role: ty.to_string(), kind: "text".into(), text: s.to_string(),
                    tool: None, is_error: is_api_err, ts,
                });
            }
            continue;
        }
        let Some(arr) = body.as_array() else { continue };
        for block in arr {
            match block.get("type").and_then(|v| v.as_str()) {
                Some("text") => {
                    let t = block.get("text").and_then(|v| v.as_str()).unwrap_or("");
                    if !t.is_empty() {
                        out.push(TranscriptEntry {
                            role: ty.to_string(), kind: "text".into(), text: t.to_string(),
                            tool: None, is_error: is_api_err, ts: ts.clone(),
                        });
                    }
                }
                Some("thinking") => {
                    let t = block.get("thinking").and_then(|v| v.as_str()).unwrap_or("");
                    if !t.is_empty() {
                        out.push(TranscriptEntry {
                            role: ty.to_string(), kind: "thinking".into(), text: t.to_string(),
                            tool: None, is_error: false, ts: ts.clone(),
                        });
                    }
                }
                Some("tool_use") => {
                    let name = block.get("name").and_then(|v| v.as_str()).unwrap_or("tool").to_string();
                    let input = block.get("input")
                        .map(|i| serde_json::to_string_pretty(i).unwrap_or_default())
                        .unwrap_or_default();
                    out.push(TranscriptEntry {
                        role: ty.to_string(), kind: "tool_use".into(), text: input,
                        tool: Some(name), is_error: false, ts: ts.clone(),
                    });
                }
                Some("tool_result") => {
                    let (text, _) = stringify_result_content(block.get("content").unwrap_or(&serde_json::Value::Null));
                    let is_error = block.get("is_error").and_then(|v| v.as_bool()) == Some(true);
                    out.push(TranscriptEntry {
                        role: ty.to_string(), kind: "tool_result".into(), text,
                        tool: None, is_error, ts: ts.clone(),
                    });
                }
                _ => {}
            }
        }
    }
    out
}

// ── Claude 5h usage ───────────────────────────────────────────────────────────

/// Parse "2026-06-02T07:06:19.987Z" → ms since Unix epoch. No external crates.
fn iso_to_unix_ms(s: &str) -> Option<u64> {
    let s = s.strip_suffix('Z').unwrap_or(s);
    let (date, time_part) = s.split_once('T')?;
    let mut dp = date.splitn(3, '-');
    let year: u32 = dp.next()?.parse().ok()?;
    let month: u32 = dp.next()?.parse().ok()?;
    let day: u32 = dp.next()?.parse().ok()?;
    let (hms, frac_str) = time_part.split_once('.').unwrap_or((time_part, ""));
    let mut tp = hms.splitn(3, ':');
    let hour: u64 = tp.next()?.parse().ok()?;
    let min: u64 = tp.next()?.parse().ok()?;
    let sec: u64 = tp.next()?.parse().ok()?;

    let leap = |y: u32| (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
    let mut days: u64 = 0;
    for y in 1970..year {
        days += if leap(y) { 366 } else { 365 };
    }
    let mdays: [u32; 12] = [31, if leap(year) { 29 } else { 28 }, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    for m in 0..(month.saturating_sub(1)) as usize {
        days += mdays[m] as u64;
    }
    days += day.saturating_sub(1) as u64;

    let frac_ms: u64 = {
        let trimmed = &frac_str[..frac_str.len().min(3)];
        format!("{:0<3}", trimmed).parse().unwrap_or(0)
    };
    Some(days * 86_400_000 + hour * 3_600_000 + min * 60_000 + sec * 1_000 + frac_ms)
}

// ── Claude plan usage (real utilization %, the same data claude.ai's UI shows) ─
// Hits the OAuth usage endpoint with Claude Code's own access token. That token
// lives in ~/.claude/.credentials.json (Linux/WSL) or, on macOS, the login
// Keychain as a generic password (service "Claude Code-credentials") with NO
// file written — so we fall back to the `security` CLI there. Cached 60s: the
// upstream is rate-limited and only refreshes ~once a minute anyway. We shell
// out to `curl` to avoid pulling an HTTP-client crate into the build.
static PLAN_USAGE_CACHE: OnceLock<Mutex<std::collections::HashMap<String, (std::time::Instant, serde_json::Value)>>> = OnceLock::new();

/// Extract a usable accessToken from a creds JSON blob, rejecting expired ones.
/// Burrow reads the token statically off disk/keychain and never refreshes it
/// (the Claude CLI does that on launch via refreshToken). An expired token POSTed
/// to the usage endpoint comes back as rate_limit_error / authentication_error —
/// misleading. Treat expired as "no usable creds" so the caller falls back to the
/// profile's local JSONL scan instead of surfacing a bogus rate-limit notice.
fn token_if_valid(raw: &str) -> Option<String> {
    let v: serde_json::Value = serde_json::from_str(raw).ok()?;
    let oauth = &v["claudeAiOauth"];
    if let Some(exp_ms) = oauth["expiresAt"].as_u64() {
        let now_ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as u64;
        if exp_ms < now_ms {
            return None;
        }
    }
    oauth["accessToken"].as_str().map(|s| s.to_string())
}

/// Resolve the raw creds JSON blob for a profile — no expiry/parse, just the bytes.
/// An explicit profile config_dir reads ONLY that profile's creds (never falls back
/// to the default account / keychain, or a profile with no creds would silently
/// mirror the default account's usage — the "switch shows same values" bug). Probes
/// both `<cd>/.credentials.json` and `<cd>/.claude/.credentials.json`: claude with
/// CLAUDE_CONFIG_DIR=<dir> writes creds under <dir>/.claude/ even though `projects/`
/// lands directly in <dir>.
fn load_claude_creds_raw(app: &AppHandle, config_dir: Option<&str>) -> Option<String> {
    if let Some(cd) = config_dir.filter(|s| !s.is_empty()) {
        let expanded = if cd.starts_with("~/") {
            app.path().home_dir().ok()
                .map(|h| format!("{}{}", h.display(), &cd[1..]))
                .unwrap_or_else(|| cd.to_string())
        } else {
            cd.to_string()
        };
        let base = std::path::Path::new(&expanded);
        return std::fs::read_to_string(base.join(".credentials.json"))
            .or_else(|_| std::fs::read_to_string(base.join(".claude/.credentials.json")))
            .ok();
    }
    // Default profile (no config_dir): ~/.claude/.credentials.json then keychain.
    let mut raw = app.path().home_dir().ok()
        .and_then(|h| std::fs::read_to_string(h.join(".claude/.credentials.json")).ok());
    #[cfg(target_os = "macos")]
    if raw.is_none() {
        if let Ok(out) = std::process::Command::new("security")
            .args(["find-generic-password", "-s", "Claude Code-credentials", "-w"])
            .output()
        {
            if out.status.success() {
                let s = String::from_utf8_lossy(&out.stdout).trim().to_string();
                if !s.is_empty() {
                    raw = Some(s);
                }
            }
        }
    }
    raw
}

fn read_claude_oauth_token(app: &AppHandle, config_dir: Option<&str>) -> Option<String> {
    load_claude_creds_raw(app, config_dir).as_deref().and_then(token_if_valid)
}

/// True when creds DO exist (carry an accessToken) but the token's expiresAt has
/// passed — i.e. the profile is "logged out" / stale, distinct from no creds at
/// all. Lets the UI hint "run claude to refresh" instead of silently showing an
/// empty bar.
fn claude_creds_expired(app: &AppHandle, config_dir: Option<&str>) -> bool {
    let Some(raw) = load_claude_creds_raw(app, config_dir) else { return false };
    let Ok(v) = serde_json::from_str::<serde_json::Value>(&raw) else { return false };
    let oauth = &v["claudeAiOauth"];
    if oauth["accessToken"].as_str().is_none() {
        return false;
    }
    if let Some(exp_ms) = oauth["expiresAt"].as_u64() {
        let now_ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as u64;
        return exp_ms < now_ms;
    }
    false
}

/// Returns `{ ok: true, usage: {...} }` where `usage` is the raw upstream blob
/// (windows like `five_hour`, `seven_day`, `seven_day_sonnet`, each carrying
/// `utilization` 0..100 and `resets_at`), or `{ ok: false, error }`.
#[tauri::command]
fn claude_plan_usage(app: AppHandle, config_dir: Option<String>, force: Option<bool>) -> serde_json::Value {
    let cache_key = config_dir.as_deref().unwrap_or("").to_string();
    let cache = PLAN_USAGE_CACHE.get_or_init(|| Mutex::new(std::collections::HashMap::new()));
    if force != Some(true) {
        if let Ok(guard) = cache.lock() {
            if let Some((ts, val)) = guard.get(&cache_key) {
                if ts.elapsed().as_secs() < 60 && val.get("ok").and_then(|b| b.as_bool()) == Some(true) {
                    return val.clone();
                }
            }
        }
    }

    let token = match read_claude_oauth_token(&app, config_dir.as_deref()) {
        Some(t) => t,
        // Distinguish a stale (expired) login from no creds at all so the UI can
        // hint "run claude to refresh". Both still trigger the local JSONL fallback.
        None => {
            let err = if claude_creds_expired(&app, config_dir.as_deref()) {
                "token_expired"
            } else {
                "no_credentials"
            };
            return json!({ "ok": false, "error": err });
        }
    };

    let out = std::process::Command::new("curl")
        .args([
            "-s",
            "-m",
            "10",
            "-H",
            &format!("Authorization: Bearer {token}"),
            "-H",
            "anthropic-beta: oauth-2025-04-20",
            "https://api.anthropic.com/api/oauth/usage",
        ])
        .output();

    let body = match out {
        Ok(o) if o.status.success() => o.stdout,
        Ok(_) => return json!({ "ok": false, "error": "curl_failed" }),
        Err(e) => return json!({ "ok": false, "error": format!("spawn: {e}") }),
    };

    let usage: serde_json::Value = match serde_json::from_slice(&body) {
        Ok(v) => v,
        Err(_) => return json!({ "ok": false, "error": "bad_json" }),
    };
    // Org/team accounts: API responds 200 but sets rate_limits_available=false.
    // Map to permission_error so the frontend falls back to local JSONL scan.
    if usage.get("rate_limits_available").and_then(|v| v.as_bool()) == Some(false) {
        return json!({ "ok": false, "error": "permission_error", "message": "Plan limits unavailable for this account type" });
    }
    // Success shape has "five_hour"; error shape has "error.type" + "error.message".
    if usage.get("five_hour").is_none() {
        let err_type = usage["error"]["type"].as_str().unwrap_or("unknown");
        let err_msg = usage["error"]["message"].as_str().unwrap_or(err_type);
        let label = match err_type {
            "rate_limit_error" => "rate_limited",
            "authentication_error" | "permission_error" => "token_expired",
            _ => "api_error",
        };
        return json!({ "ok": false, "error": label, "message": err_msg });
    }

    let payload = json!({ "ok": true, "usage": usage });
    if let Ok(mut g) = cache.lock() {
        g.insert(cache_key, (std::time::Instant::now(), payload.clone()));
    }
    payload
}

/// Scan ~/.claude/projects/**/*.jsonl and aggregate assistant turn data from the
/// last 5 hours. Returns { outputTokens, turnCount } — no external crates needed.
#[tauri::command]
fn claude_usage_5h(app: AppHandle, config_dir: Option<String>) -> serde_json::Value {
    use std::io::{BufRead, BufReader};

    let projects_dir = if let Some(cd) = config_dir.filter(|s| !s.is_empty()) {
        // Expand a leading ~ (PathBuf::from would take it literally) and probe both
        // `<cd>/projects` and `<cd>/.claude/projects` — claude's transcripts land in
        // one or the other depending on how CLAUDE_CONFIG_DIR is laid out.
        let expanded = if cd.starts_with("~/") {
            app.path().home_dir().ok()
                .map(|h| format!("{}{}", h.display(), &cd[1..]))
                .unwrap_or_else(|| cd.to_string())
        } else {
            cd.to_string()
        };
        let base = std::path::Path::new(&expanded);
        let direct = base.join("projects");
        if direct.is_dir() { direct } else { base.join(".claude/projects") }
    } else {
        let home = match app.path().home_dir() {
            Ok(h) => h,
            Err(_) => return json!({ "outputTokens": 0, "turnCount": 0 }),
        };
        home.join(".claude/projects")
    };

    let now_ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64;
    let cutoff_ms = now_ms.saturating_sub(5 * 3600 * 1000);

    let mut output_tokens: u64 = 0;
    let mut turn_count: u32 = 0;

    let Ok(project_dirs) = std::fs::read_dir(&projects_dir) else {
        return json!({ "outputTokens": 0, "turnCount": 0 });
    };

    for project_entry in project_dirs.flatten() {
        if !project_entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
            continue;
        }
        // Skip files too old to contain recent entries (file mtime heuristic).
        if let Ok(meta) = project_entry.path().metadata() {
            if let Ok(modified) = meta.modified() {
                let mtime_ms = modified
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap_or_default()
                    .as_millis() as u64;
                if mtime_ms < cutoff_ms {
                    continue;
                }
            }
        }
        let Ok(jsonl_files) = std::fs::read_dir(project_entry.path()) else { continue };
        for file_entry in jsonl_files.flatten() {
            let path = file_entry.path();
            if path.extension().and_then(|e| e.to_str()) != Some("jsonl") {
                continue;
            }
            // Same mtime heuristic for individual files.
            if let Ok(meta) = path.metadata() {
                if let Ok(modified) = meta.modified() {
                    let mtime_ms = modified
                        .duration_since(std::time::UNIX_EPOCH)
                        .unwrap_or_default()
                        .as_millis() as u64;
                    if mtime_ms < cutoff_ms {
                        continue;
                    }
                }
            }
            let Ok(f) = std::fs::File::open(&path) else { continue };
            for line in BufReader::new(f).lines().flatten() {
                if !line.contains("\"assistant\"") {
                    continue;
                }
                let Ok(v) = serde_json::from_str::<serde_json::Value>(&line) else { continue };
                if v["type"].as_str() != Some("assistant") {
                    continue;
                }
                let ts_str = match v["timestamp"].as_str() {
                    Some(s) => s,
                    None => continue,
                };
                let ts_ms = match iso_to_unix_ms(ts_str) {
                    Some(ms) => ms,
                    None => continue,
                };
                if ts_ms < cutoff_ms {
                    continue;
                }
                if let Some(tokens) = v["message"]["usage"]["output_tokens"].as_u64() {
                    output_tokens += tokens;
                    turn_count += 1;
                }
            }
        }
    }

    json!({ "outputTokens": output_tokens, "turnCount": turn_count })
}

// ── Native menu ───────────────────────────────────────────────────────────────

fn build_menu(app: &tauri::AppHandle) -> tauri::Result<tauri::menu::Menu<tauri::Wry>> {
    use tauri::menu::{IsMenuItem, Menu, MenuItem, PredefinedMenuItem};

    // Start from Tauri's standard menu so the default Edit/View/Window
    // submenus (copy/paste/undo, minimize/zoom, …) are preserved, then
    // inject "Check for Updates…" into the app (first) submenu.
    let menu = Menu::default(app)?;

    if let Some(first) = menu.items()?.into_iter().next() {
        if let Some(app_menu) = first.as_submenu() {
            let check_update =
                MenuItem::with_id(app, "check-update", "Check for Updates…", true, None::<&str>)?;
            let separator = PredefinedMenuItem::separator(app)?;
            // Insert right after the "About" item (index 0) on macOS.
            let items: &[&dyn IsMenuItem<tauri::Wry>] = &[&separator, &check_update];
            app_menu.insert_items(items, 1)?;
        }
    }

    Ok(menu)
}

// ── Entry point ───────────────────────────────────────────────────────────────

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // Stdio MCP proxy mode: the app exe is launched by an MCP-client CLI as
    // `<exe> --burrow-mcp-stdio`. Run the JSON-RPC stdin/stdout loop (proxying
    // tools/call over the Unix socket to the running desktop app) and exit
    // BEFORE building the Tauri app — this process is a disposable proxy.
    if std::env::args().any(|a| a == burrow_mcp_core::BURROW_MCP_STDIO_ARG) {
        if let Err(e) = burrow_mcp_stdio::run_stdio_server() {
            eprintln!("[burrow] MCP stdio proxy error: {e}");
            std::process::exit(1);
        }
        return;
    }

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_fs::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_os::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_decorum::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_window_state::Builder::default().build())
        .setup(|app| {
            if let Ok(menu) = build_menu(app.handle()) {
                let _ = app.set_menu(menu);
            }
            let conn = init_db(app.handle()).expect("DB init failed");
            app.manage(DbState { conn: Mutex::new(conn) });
            app.manage(FloatParamsState { map: Mutex::new(std::collections::HashMap::new()) });
            app.manage(SpawnTokenOrigin::default());
            app.manage(FloatLayoutState {
                corner: Mutex::new(Corner::TopRight),
                wins: Mutex::new(Vec::new()),
            });

            // Vertically center the native traffic lights in the 36px titlebar.
            // (--titlebar-height = 36; button cluster ~16px tall → y ≈ 10.)
            //
            // OS vibrancy intentionally NOT applied: it caused system-wide
            // lag/freezes on this machine. All themes are opaque, so there is
            // nothing to frost anyway.
            #[cfg(target_os = "macos")]
            {
                use tauri_plugin_decorum::WebviewWindowExt;
                if let Some(win) = app.get_webview_window("main") {
                    let _ = win.set_traffic_lights_inset(13.0, 10.0);
                }
            }

            // Connect to (or spawn) burrow-daemon
            let data_dir = app.path().app_data_dir().expect("no app data dir");
            let client = daemon_ensure(&data_dir, app.handle())
                .map_err(|e| {
                    eprintln!("[burrow] daemon error: {e}");
                    e
                })
                .ok();
            app.manage(DaemonState { client: Mutex::new(client) });
            app.manage(LspState::default());
            app.manage(ClaudeState::default());
            app.manage(AcpState::default());
            app.manage(AccountInfoCache::default());

            start_hook_server(app.handle().clone());
            burrow_mcp_socket::start(app.handle());
            http_server::server::maybe_start(app.handle());
            install_agent_docs(app.handle());
            // Write the burrow bin now so the global hook command path is valid even
            // before the first PTY spawn, then register the persistent status hooks.
            ensure_burrow_bin(app.handle());
            install_status_hooks(app.handle());
            prewarm_acp_adapters();
            Ok(())
        })
        .on_menu_event(|app, event| {
            if event.id().as_ref() == "check-update" {
                let _ = app.emit("menu-check-update", ());
            }
        })
        .invoke_handler(tauri::generate_handler![
            create_pty,
            write_pty,
            resize_pty,
            kill_pty,
            detach_pty,
            list_pty_sessions,
            get_pty_foreground,
            list_claude_sessions,
            is_pid_alive,
            system_stats,
            daemon_stats,
            clean_daemon,
            kill_orphan_sessions,
            restart_daemon,
            take_spawn_requests,
            reinstall_status_hooks,
            set_sleep_inhibit,
            remove_status_hooks,
            get_config_dirs,
            set_config_dirs,
            run_git,
            run_gh,
            format_source,
            open_path_in,
            read_dir_shallow,
            list_workspaces,
            create_workspace,
            delete_workspace,
            rename_workspace,
            touch_workspace,
            set_workspace_icon,
            set_workspace_order,
            create_worktree,
            remove_worktree,
            list_terminal_tabs,
            save_terminal_tabs,
            get_app_version,
            write_text_file,
            read_text_file,
            read_text_file_checked,
            read_config,
            write_config,
            scaffold_burrow_dir,
            lsp_start,
            lsp_send,
            lsp_stop,
            claude_start,
            claude_send,
            claude_stop,
            claude_abort,
            claude_respond_control,
            acp_start,
            acp_send,
            acp_stop,
            acp_respond_permission,
            acp_set_mode,
            acp_set_config,
            acp_list_sessions,
            claude_get_account,
            read_file_base64,
            save_temp_image,
            get_hook_server_port,
            repair_agent_status,
            set_max_agents,
            set_burrow_mcp_max_depth,
            set_tab_live_status,
            http_server::get_http_server_status,
            http_server::set_http_enabled,
            http_server::tailscale::get_tailscale_status,
            http_server::tailscale::set_tailscale_serve,
            burrow_mcp_flag,
            dispatch,
            register_tmux_win,
            list_skills,
            set_skill_enabled,
            delete_skill,
            list_mcp_servers,
            add_mcp_server,
            remove_mcp_server,
            open_git_panel_window,
            open_float_window,
            get_float_params,
            set_window_size,
            snap_float_window,
            sync_float_size,
            close_float_window,
            request_float_snapshot,
            send_float_snapshot,
            notify_float_grid,
            set_float_corner,
            claude_usage_5h,
            claude_plan_usage,
            read_claude_result,
            read_claude_outcome,
            read_claude_activity,
            read_claude_transcript,
            list_mission_tasks,
            upsert_mission_task,
            delete_mission_task,
            list_board_tasks,
            upsert_board_task,
            move_board_task,
            delete_board_task,
            write_task_attachment,
            list_task_attachments,
            delete_task_attachment,
            read_task_attachment_base64,
        ])
        .run(tauri::generate_context!())
        .expect("error running tauri application");
}

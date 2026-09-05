package main

// Global (no-suffix) events, matching src-tauri/src/lib.rs's emit_all calls.
//
// This used to be a plain runtime.EventsEmit, which meant the mobile client
// never learned that the workspace list had changed. That class of bug is
// gone: there is no second door any more.
func emitWorkspacesChanged() { busEmit("workspaces-changed", nil) }

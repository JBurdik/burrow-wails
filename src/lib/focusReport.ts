import { invoke } from "@tauri-apps/api/core";

// Tell Go whether this native window has focus (src-wails/uifocus.go): on
// dev/beta the core deciding tray toasts runs in another process and can't
// ask AppKit. Desktop windows only — a phone's focus says nothing about the Mac.
export function startFocusReport(window_: string): void {
  const send = () => void invoke("ui_focus", { window: window_, focused: document.hasFocus() }).catch(() => {});
  window.addEventListener("focus", send);
  window.addEventListener("blur", send);
  // Heartbeat: Go forgets a "focused" report after 45 s (a window that died focused).
  setInterval(() => { if (document.hasFocus()) send(); }, 20_000);
  send();
}

// Stub for "@tauri-apps/api/webviewWindow" — multi-window control
// (float bubble -> focus main window) has no Wails port yet; Wails v2 is
// single-window by default (float bubbles need a redesign, see plan
// phase 7). Returns null so callers' `if (main)` guards no-op safely.
export class WebviewWindow {
  static async getByLabel(_label: string): Promise<null> {
    console.warn("[wails-compat] WebviewWindow.getByLabel is not ported yet (single-window)");
    return null;
  }
}

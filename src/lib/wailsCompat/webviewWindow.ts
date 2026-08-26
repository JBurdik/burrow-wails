// Stub for "@tauri-apps/api/webviewWindow" — multi-window control
// (float bubble -> focus main window) has no Wails port yet; Wails v2 is
// single-window by default (float bubbles need a redesign, see plan
// phase 7). Returns null so callers' `if (main)` guards no-op safely.
interface StubWindow {
  show(): Promise<void>;
  setFocus(): Promise<void>;
}

export class WebviewWindow {
  static async getByLabel(_label: string): Promise<StubWindow | null> {
    console.warn("[wails-compat] WebviewWindow.getByLabel is not ported yet (single-window)");
    return null;
  }
}

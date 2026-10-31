// Stub for "@tauri-apps/api/webviewWindow" — the Tauri float-bubble API
// (focus main window from a bubble). Real secondary windows are
// src/lib/windows.ts (Wails v3 OpenWindow); this shim stays a no-op for the
// leftover callers. Returns null so their `if (main)` guards no-op safely.
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

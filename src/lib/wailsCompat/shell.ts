// Shim for "@tauri-apps/plugin-shell"'s open() (open a URL in the default
// browser), backed by Wails' runtime.BrowserOpenURL.
import { BrowserOpenURL } from "../../../src-wails/frontend/wailsjs/runtime/runtime";

export async function open(url: string): Promise<void> {
  BrowserOpenURL(url);
}

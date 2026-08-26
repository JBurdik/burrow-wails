// Shim for "@tauri-apps/plugin-process"'s relaunch() — backed by Go's
// RelaunchApp (src-wails/updater.go), which re-opens the bundle and quits.
import * as App from "../../../src-wails/frontend/wailsjs/go/main/App";

export async function relaunch(): Promise<void> {
  await App.RelaunchApp();
}

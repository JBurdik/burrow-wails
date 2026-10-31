// Shim for "@tauri-apps/plugin-process"'s relaunch() — backed by Go's
// RelaunchApp (src-wails/updater.go), which re-opens the bundle and quits.
import * as App from "./bindings/burrow/desktopservice";

export async function relaunch(): Promise<void> {
  await App.RelaunchApp();
}

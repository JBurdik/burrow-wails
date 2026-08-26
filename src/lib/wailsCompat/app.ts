// Shim for "@tauri-apps/api/app"'s getVersion() — backed by the Go
// GetAppVersion binding already used elsewhere via invoke("get_app_version").
import * as App from "../../../src-wails/frontend/wailsjs/go/main/App";

export async function getVersion(): Promise<string> {
  return App.GetAppVersion();
}

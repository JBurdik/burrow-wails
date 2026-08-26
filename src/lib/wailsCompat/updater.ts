// Shim for "@tauri-apps/plugin-updater"'s check(). The version-check half
// is real (hits the same GitHub Releases latest.json tauri-plugin-updater
// used, via Go's CheckUpdate). downloadAndInstall isn't ported — no Go
// equivalent yet of the ed25519-signed artifact download/replace/relaunch
// flow (see plan phase 7); it rejects clearly instead of pretending to work.
import * as App from "../../../burrow-wails/burrow/frontend/wailsjs/go/main/App";

interface UpdateHandle {
  version: string;
  currentVersion: string;
  body?: string;
  downloadAndInstall: (cb?: (e: unknown) => void) => Promise<void>;
}

export async function check(): Promise<UpdateHandle | null> {
  const info = await App.CheckUpdate();
  if (!info.available) return null;
  return {
    version: info.version,
    currentVersion: info.current_version,
    body: info.notes,
    async downloadAndInstall() {
      throw new Error("Auto-update download/install isn't ported yet — update manually for now.");
    },
  };
}

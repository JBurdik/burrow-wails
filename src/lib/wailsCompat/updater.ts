// Shim for "@tauri-apps/plugin-updater"'s check(). Backed by Go's
// CheckUpdate/InstallUpdate (src-wails/updater.go), which hit the same
// GitHub Releases latest.json layout tauri-plugin-updater used.
import * as App from "../../../src-wails/frontend/wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../src-wails/frontend/wailsjs/runtime/runtime";

type DownloadEvent =
  | { event: "Started"; data: { contentLength?: number } }
  | { event: "Progress"; data: { chunkLength: number } }
  | { event: "Finished" };

interface UpdateHandle {
  version: string;
  currentVersion: string;
  body?: string;
  downloadAndInstall: (cb?: (e: DownloadEvent) => void) => Promise<void>;
}

export async function check(): Promise<UpdateHandle | null> {
  const info = await App.CheckUpdate();
  if (!info.available) return null;
  return {
    version: info.version,
    currentVersion: info.current_version,
    body: info.notes,
    async downloadAndInstall(cb) {
      // Go reports cumulative bytes; Tauri's callback wants a Started with the
      // total then per-chunk deltas, so translate on the way through.
      let started = false;
      let last = 0;
      if (cb) {
        EventsOn("update:progress", (p: { received: number; total: number }) => {
          if (!started) {
            started = true;
            cb({ event: "Started", data: { contentLength: p.total > 0 ? p.total : undefined } });
          }
          cb({ event: "Progress", data: { chunkLength: p.received - last } });
          last = p.received;
        });
      }
      try {
        await App.InstallUpdate(info.url, info.sha256);
        cb?.({ event: "Finished" });
      } finally {
        EventsOff("update:progress");
      }
    },
  };
}

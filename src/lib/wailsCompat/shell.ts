// Shim for "@tauri-apps/plugin-shell"'s open() (open a URL in the default
// browser), backed by Wails' Browser.OpenURL.
import { Browser } from "@wailsio/runtime";

export async function open(url: string): Promise<void> {
  await Browser.OpenURL(url);
}

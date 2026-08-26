// Shim for "@tauri-apps/plugin-dialog"'s open(), backed by Wails' native
// dialog runtime calls (PickDirectory/PickFile/PickFiles in dialog.go).
import * as App from "../../../burrow-wails/burrow/frontend/wailsjs/go/main/App";

interface OpenOptions {
  directory?: boolean;
  multiple?: boolean;
  filters?: { name: string; extensions: string[] }[];
  defaultPath?: string;
}

export async function open(opts: OpenOptions = {}): Promise<string | string[] | null> {
  try {
    if (opts.directory) {
      const dir = await App.PickDirectory();
      return dir || null;
    }
    const filter = opts.filters?.[0];
    const name = filter?.name ?? "";
    const exts = filter?.extensions ?? [];
    if (opts.multiple) {
      const files = await App.PickFiles(name, exts);
      return files && files.length > 0 ? files : null;
    }
    const file = await App.PickFile(name, exts);
    return file || null;
  } catch (err) {
    console.warn("[wails-compat] dialog open() failed:", err);
    return null;
  }
}

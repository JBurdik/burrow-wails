// Stub for "@tauri-apps/plugin-updater" — auto-update has no Wails port
// yet (needs a custom Go updater, see plan phase 7). check() always
// reports "no update available" so update.ts's UI stays quiet instead of
// erroring.
export async function check(): Promise<null> {
  console.warn("[wails-compat] updater check() is not ported yet");
  return null;
}

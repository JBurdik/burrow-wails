// Shim for "@tauri-apps/api/window"'s getCurrentWindow() — only the subset
// actually used: setTitle() (real, via Wails runtime) and onMoved() (stub,
// float-bubble corner-snapping is part of the float-window redesign that
// hasn't happened yet — see plan phase 7 / stubs.go).
import { WindowSetTitle } from "../../../burrow-wails/burrow/frontend/wailsjs/runtime/runtime";

export function getCurrentWindow() {
  return {
    async setTitle(title: string) {
      WindowSetTitle(title);
    },
    async onMoved(_handler: () => void) {
      console.warn("[wails-compat] window.onMoved is not ported yet (float windows)");
      return () => {};
    },
    async onResized(_handler: () => void) {
      console.warn("[wails-compat] window.onResized is not ported yet (float windows)");
      return () => {};
    },
  };
}

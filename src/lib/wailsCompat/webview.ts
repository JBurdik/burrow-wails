// Stub for "@tauri-apps/api/webview"'s getCurrentWebview().onDragDropEvent —
// native OS file drag-drop into the terminal isn't ported yet (Wails has
// its own OnFileDrop mechanism with a different payload shape). Returns a
// working no-op unlisten so XTerm.vue's setup doesn't throw.
export function getCurrentWebview() {
  return {
    async onDragDropEvent(_handler: (event: unknown) => void) {
      console.warn("[wails-compat] onDragDropEvent is not ported yet");
      return () => {};
    },
  };
}

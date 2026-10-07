import { onBeforeUnmount } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";

const IMAGE_MIME: Record<string, string> = { png: "png", jpg: "jpeg", jpeg: "jpeg", gif: "gif", webp: "webp" };

/**
 * Files dragged in from Finder (Go: `DragAndDrop.EnableFileDrop` → "file-drop").
 * Wails only forwards a drop whose target has `--wails-drop-target: drop`, so
 * `root` must carry the `drop-zone` class (composer.css), which also paints the
 * highlight while a drag is over it. Only the root under the cursor reacts.
 * Images go to `image` as data URIs (like a cmd+V paste); everything else
 * becomes an `@path` for `ref`.
 */
export function useFileDrop(
  root: () => Element | null | undefined,
  on: { image: (dataUri: string) => void; ref: (atPath: string) => void },
) {
  let unlisten: (() => void) | null = null;
  let gone = false;
  listen<{ x: number; y: number; paths: string[] }>("file-drop", async ({ payload }) => {
    const el = root();
    const hit = document.elementFromPoint(payload.x, payload.y);
    if (!el || !hit || !el.contains(hit)) return;
    for (const path of payload.paths) {
      const mime = IMAGE_MIME[path.split(".").pop()!.toLowerCase()];
      if (mime) {
        try {
          on.image(`data:image/${mime};base64,${await invoke<string>("read_file_base64", { path })}`);
          continue;
        } catch { /* unreadable as an image: fall through to a plain path */ }
      }
      on.ref(/\s/.test(path) ? `@"${path}"` : `@${path}`);
    }
  }).then((un) => { if (gone) un(); else unlisten = un; });
  onBeforeUnmount(() => { gone = true; unlisten?.(); });
}

/** Append `@path ` to composer text with sensible spacing. */
export function appendRef(text: string, atPath: string): string {
  return `${text}${text && !/\s$/.test(text) ? " " : ""}${atPath} `;
}

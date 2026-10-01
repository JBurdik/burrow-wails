import { onBeforeUnmount } from "vue";
import { listen } from "@tauri-apps/api/event";

export function useDiffCommentRefresh(workspace: () => number | undefined, refresh: () => Promise<void>) {
  let disposed = false;
  let stop: (() => void) | undefined;
  void listen<number>("diff-comments-changed", (event) => {
    if (!disposed && event.payload === workspace()) void refresh();
  }).then((unlisten) => { if (disposed) unlisten(); else stop = unlisten; }).catch(() => {});
  onBeforeUnmount(() => { disposed = true; stop?.(); });
}

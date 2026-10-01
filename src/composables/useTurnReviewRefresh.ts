import { watch, toValue, type MaybeRefOrGetter } from "vue";
import { listen } from "@tauri-apps/api/event";

export function useTurnReviewRefresh(subject: MaybeRefOrGetter<string>, refresh: () => void) {
  watch(() => toValue(subject), (id, _previous, onCleanup) => {
    if (!id) return;
    let disposed = false;
    let stop: (() => void) | undefined;
    onCleanup(() => { disposed = true; stop?.(); });
    void listen(`turn-receipt-${id}`, () => { if (!disposed) refresh(); }).then((unlisten) => {
      if (disposed) unlisten();
      else stop = unlisten;
    }).catch(() => {});
  }, { immediate: true });
}

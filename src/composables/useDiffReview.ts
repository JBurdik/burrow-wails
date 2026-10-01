import { computed, shallowRef, watch, type MaybeRefOrGetter, toValue } from "vue";
import { currentReceipts, diffRevision, type ReviewReceipts } from "@/lib/reviewReceipts";
import type { FileDiffMetadata } from "@pierre/diffs";

export function useDiffReview(context: MaybeRefOrGetter<string>) {
  const receipts = shallowRef<ReviewReceipts>({});
  const revisions = shallowRef<ReviewReceipts>({});
  const storageKey = computed(() => `burrow.diffSeen.v1:${toValue(context)}`);
  const seenCount = computed(() => Object.keys(revisions.value).filter(isSeen).length);
  const fileCount = computed(() => Object.keys(revisions.value).length);
  const storageError = shallowRef("");

  watch(storageKey, (key) => {
    revisions.value = {};
    storageError.value = "";
    try {
      const raw: unknown = JSON.parse(localStorage.getItem(key) ?? "{}");
      receipts.value = raw && typeof raw === "object" && !Array.isArray(raw)
        ? Object.fromEntries(Object.entries(raw).filter(([, v]) => typeof v === "string")) : {};
    } catch { receipts.value = {}; }
  }, { immediate: true });

  function persist() {
    try {
      localStorage.setItem(storageKey.value, JSON.stringify(receipts.value));
      storageError.value = "";
    } catch { storageError.value = "Seen marks could not be saved on this device."; }
  }

  function syncFiles(files: FileDiffMetadata[]) {
    revisions.value = Object.fromEntries(files.map((file) => [file.name, diffRevision(file)]));
    const next = currentReceipts(receipts.value, revisions.value);
    if (Object.keys(next).length !== Object.keys(receipts.value).length) {
      receipts.value = next;
      persist();
    }
  }

  function isSeen(name: string): boolean {
    return !!revisions.value[name] && receipts.value[name] === revisions.value[name];
  }

  function setSeen(name: string, seen: boolean) {
    const next = { ...receipts.value };
    if (seen && revisions.value[name]) next[name] = revisions.value[name];
    else delete next[name];
    receipts.value = next;
    persist();
  }

  return { seenCount, fileCount, storageError, syncFiles, isSeen, setSeen };
}

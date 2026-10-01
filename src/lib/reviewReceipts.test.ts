import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { effectScope, shallowRef, nextTick } from "vue";
import { parsePatchFiles } from "@pierre/diffs";
import { useDiffReview } from "@/composables/useDiffReview";

function file(value: string) {
  return parsePatchFiles(`diff --git a/app.ts b/app.ts\n--- a/app.ts\n+++ b/app.ts\n@@ -1 +1 @@\n-old\n+${value}\n`)[0].files[0];
}

describe("diff review receipts", () => {
  beforeEach(() => {
    const storage = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("persists seen state, clears it after edits, and cannot revive an old receipt", () => {
    const scope = effectScope();
    const review = scope.run(() => useDiffReview("project:1:workspace"))!;
    review.syncFiles([file("first")]);
    review.setSeen("app.ts", true);
    expect(review.seenCount.value).toBe(1);
    const secondScope = effectScope();
    const restored = secondScope.run(() => useDiffReview("project:1:workspace"))!;
    restored.syncFiles([file("first")]);
    expect(restored.isSeen("app.ts")).toBe(true);
    restored.syncFiles([file("second")]);
    expect(restored.isSeen("app.ts")).toBe(false);
    restored.syncFiles([file("first")]);
    expect(restored.isSeen("app.ts")).toBe(false);
    scope.stop();
    secondScope.stop();
  });

  it("keeps comparison scopes separate even for identical file names and contents", async () => {
    const scope = effectScope();
    const key = shallowRef("project:1:workspace");
    const review = scope.run(() => useDiffReview(key))!;
    review.syncFiles([file("first")]);
    review.setSeen("app.ts", true);
    key.value = "project:2:workspace";
    await nextTick();
    review.syncFiles([file("first")]);
    expect(review.isSeen("app.ts")).toBe(false);
    key.value = "project:1:workspace";
    await nextTick();
    review.syncFiles([file("first")]);
    expect(review.isSeen("app.ts")).toBe(true);
    scope.stop();
  });
});

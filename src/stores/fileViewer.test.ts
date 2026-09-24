import { beforeEach, describe, expect, it } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { useFileViewerStore } from "./fileViewer";

describe("fileViewer", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("recycles the single clean preview file", () => {
    const store = useFileViewerStore();
    store.openFile(1, "/repo/a.ts", "a.ts");
    store.openFile(1, "/repo/b.ts", "b.ts");
    expect(store.workspace(1).files.map((file) => file.path)).toEqual(["/repo/b.ts"]);
  });

  it("keeps pinned files when a new preview opens", () => {
    const store = useFileViewerStore();
    store.openFile(1, "/repo/a.ts", "a.ts", undefined, { pin: true });
    store.openFile(1, "/repo/b.ts", "b.ts");
    expect(store.workspace(1).files.map((file) => file.path)).toEqual(["/repo/a.ts", "/repo/b.ts"]);
  });

  it("automatically pins a preview on the first edit", () => {
    const store = useFileViewerStore();
    store.openFile(1, "/repo/a.ts", "a.ts");
    store.markDirty(1, "/repo/a.ts", true);
    store.openFile(1, "/repo/b.ts", "b.ts");
    const state = store.workspace(1);
    expect(state.files.map((file) => file.path)).toEqual(["/repo/a.ts", "/repo/b.ts"]);
    expect(state.files[0]).toMatchObject({ pinned: true, dirty: true });
  });

  it("keeps all state in memory without writing configuration", () => {
    const store = useFileViewerStore();
    store.openFile(1, "/repo/a.ts", "a.ts", 12);
    expect(store.workspace(1)).toMatchObject({ activePath: "/repo/a.ts", visible: true });
    expect(store.workspace(2).files).toEqual([]);
  });
});

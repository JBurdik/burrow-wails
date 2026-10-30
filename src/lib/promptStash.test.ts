import { describe, expect, it } from "vitest";
import { MAX_STASH_ENTRIES, MAX_STASH_IMAGE_CHARS, makeEntry, pushEntry, restoreEntry, type StashEntry } from "./promptStash";

const entry = (text: string, now: number) => makeEntry({ text, images: [] }, now);

describe("promptStash", () => {
  it("caps the stash and drops the oldest", () => {
    let entries: StashEntry[] = [];
    for (let i = 0; i <= MAX_STASH_ENTRIES; i++) entries = pushEntry(entries, entry(`p${i}`, i));
    expect(entries).toHaveLength(MAX_STASH_ENTRIES);
    expect(entries[0].text).toBe(`p${MAX_STASH_ENTRIES}`);
    expect(entries.some((e) => e.text === "p0")).toBe(false);
  });

  it("drops images past the budget and records them", () => {
    const big = "x".repeat(MAX_STASH_IMAGE_CHARS - 10);
    const e = makeEntry({ text: "", images: [big, "y".repeat(20), "z"] }, 1);
    expect(e.images).toEqual([big, "z"]);
    expect(e.droppedImages).toEqual(["image 2"]);
  });

  it("restore removes the entry and auto-stashes a non-empty composer", () => {
    const a = entry("a", 1);
    const { entries, restored } = restoreEntry([a], a.id, { text: "draft", images: [] }, 2);
    expect(restored).toBe(a);
    expect(entries.map((e) => e.text)).toEqual(["draft"]);
    expect(restoreEntry([a], a.id, { text: "  ", images: [] }).entries).toEqual([]);
  });
});

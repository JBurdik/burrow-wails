import { describe, expect, it } from "vitest";
import { parseConflicts, resolveConflict } from "./conflictMarkers";

const file = [
  "a",
  "<<<<<<< HEAD", "ours1", "=======", "theirs1", ">>>>>>> abc123 (msg)",
  "b",
  "<<<<<<< HEAD", "o2", "||||||| base", "b2", "=======", "t2", ">>>>>>> abc123",
  "",
].join("\n");

describe("conflictMarkers", () => {
  it("parses blocks incl. diff3 base", () => {
    const s = parseConflicts(file);
    expect(s.map((x) => x.kind)).toEqual(["text", "conflict", "text", "conflict", "text"]);
    expect(s[1]).toMatchObject({ ours: ["ours1"], theirs: ["theirs1"], base: null, oursLabel: "HEAD", theirsLabel: "abc123 (msg)" });
    expect(s[3]).toMatchObject({ ours: ["o2"], base: ["b2"], theirs: ["t2"] });
  });
  it("resolves one block, leaves the other intact", () => {
    const segs = parseConflicts(file);
    const once = resolveConflict(segs, 0, "both");
    expect(once.startsWith("a\nours1\ntheirs1\nb\n<<<<<<< HEAD")).toBe(true);
    const twice = resolveConflict(parseConflicts(once), 0, "theirs");
    expect(twice).toBe("a\nours1\ntheirs1\nb\nt2\n");
  });
  it("unterminated block stays text", () => {
    expect(parseConflicts("x\n<<<<<<< HEAD\ny").map((s) => s.kind)).toEqual(["text"]);
  });
});

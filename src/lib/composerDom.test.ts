import { describe, it, expect } from "vitest";
import {
  chipSignature, flatOffset, locateOffset, nodeLength, serializeNodes, tokenize,
  type FlatNode,
} from "./composerDom";

const text = (t: string): FlatNode => ({ kind: "text", text: t });
const skill = (n: string): FlatNode => ({ kind: "skill", name: n });
const command = (n: string): FlatNode => ({ kind: "command", name: n });

const KNOWN = { skills: ["burrow", "agent-browser", "caveman:caveman"], commands: ["compact", "pr"] };

// "fix /agent-browser now" — the model string the agent receives.
const NODES: FlatNode[] = [text("fix "), skill("agent-browser"), text(" now")];

describe("tokenize", () => {
  it("chips a known token at the start and keeps the rest as text", () => {
    expect(tokenize("/burrow fix it", KNOWN)).toEqual([skill("burrow"), text(" fix it")]);
  });

  it("chips mid-message and leaves the whitespace on the text run", () => {
    expect(tokenize("use /burrow now", KNOWN)).toEqual([
      text("use "), skill("burrow"), text(" now"),
    ]);
  });

  it("leaves an unknown /token as plain text", () => {
    // Only things that will actually resolve get to look resolved.
    expect(tokenize("use /nosuchthing now", KNOWN)).toEqual([text("use /nosuchthing now")]);
  });

  it("does not mistake a path for a token", () => {
    expect(tokenize("look at src/foo/bar.ts", KNOWN)).toEqual([text("look at src/foo/bar.ts")]);
  });

  it("handles a plugin-style name with a colon", () => {
    expect(tokenize("/caveman:caveman go", KNOWN)).toEqual([skill("caveman:caveman"), text(" go")]);
  });

  it("tells a built-in command apart from a skill", () => {
    expect(tokenize("/compact", KNOWN)).toEqual([command("compact")]);
    expect(tokenize("/burrow", KNOWN)).toEqual([skill("burrow")]);
  });

  it("lets a skill win a name collision with a command", () => {
    // The command list is a handful of built-ins; a skill was installed on purpose.
    expect(tokenize("/pr", { skills: ["pr"], commands: ["pr"] })).toEqual([skill("pr")]);
  });

  it("is empty for empty input, and all-text when nothing is known", () => {
    expect(tokenize("", KNOWN)).toEqual([]);
    expect(tokenize("/burrow", {})).toEqual([text("/burrow")]);
  });

  it("round-trips through serializeNodes", () => {
    const model = "use /burrow and /compact on src/x, not /unknown";
    expect(serializeNodes(tokenize(model, KNOWN))).toBe(model);
  });
});

describe("serializeNodes", () => {
  it("renders a chip back as its /invocation", () => {
    expect(serializeNodes(NODES)).toBe("fix /agent-browser now");
    // A command chip serializes the same way — the model never knows the kind.
    expect(serializeNodes([command("compact")])).toBe("/compact");
  });

  it("is empty for an empty list", () => {
    expect(serializeNodes([])).toBe("");
  });

  it("counts a chip as the characters it stands for, slash included", () => {
    expect(nodeLength(skill("burrow"))).toBe("/burrow".length);
    expect(nodeLength(text("hello"))).toBe(5);
  });
});

describe("locateOffset ↔ flatOffset round trip", () => {
  it("agrees at every offset in the model", () => {
    const model = serializeNodes(NODES);
    for (let off = 0; off <= model.length; off++) {
      const pos = locateOffset(NODES, off);
      const back = flatOffset(NODES, pos.index, pos.offset);
      // Inside a chip the caret snaps to an edge, so only those offsets move.
      const insideChip = off > 4 && off < 4 + nodeLength(skill("agent-browser"));
      if (insideChip) expect([4, 4 + nodeLength(skill("agent-browser"))]).toContain(back);
      else expect(back).toBe(off);
    }
  });

  it("puts a boundary offset at the end of the earlier node", () => {
    // Offset 4 is both "end of 'fix '" and "start of the chip". Landing in the
    // text node is what keeps a caret typed right after a chip from jumping
    // past the text that follows.
    expect(locateOffset(NODES, 4)).toEqual({ index: 0, offset: 4 });
  });

  it("never reports a position inside a chip", () => {
    const chipLen = nodeLength(skill("agent-browser"));
    for (let off = 5; off < 4 + chipLen; off++) {
      const pos = locateOffset(NODES, off);
      if (pos.index === 1) expect([0, chipLen]).toContain(pos.offset);
    }
  });

  it("snaps to the nearer chip edge", () => {
    const chipLen = nodeLength(skill("agent-browser")); // 14, chip spans 4..18
    // Just inside the chip resolves to "before it" — the same caret as the
    // boundary case above, expressed on the chip node instead of the text one.
    expect(locateOffset(NODES, 5)).toEqual({ index: 1, offset: 0 });
    expect(locateOffset(NODES, 10).offset).toBe(0);        // early half → before
    expect(locateOffset(NODES, 17).offset).toBe(chipLen);  // late half  → after
  });
});

describe("locateOffset edges", () => {
  it("clamps past the end, so a caret restored after a shortening edit survives", () => {
    expect(locateOffset(NODES, 999)).toEqual({ index: 2, offset: 4 });
  });

  it("clamps a negative offset to the start", () => {
    expect(locateOffset(NODES, -5)).toEqual({ index: 0, offset: 0 });
  });

  it("reports index -1 for an empty editor", () => {
    expect(locateOffset([], 0)).toEqual({ index: -1, offset: 0 });
  });

  it("handles a model that is nothing but a chip", () => {
    const only: FlatNode[] = [skill("burrow")];
    expect(serializeNodes(only)).toBe("/burrow");
    expect(locateOffset(only, 0)).toEqual({ index: 0, offset: 0 });
    expect(locateOffset(only, 7)).toEqual({ index: 0, offset: 7 });
    expect(flatOffset(only, 0, 7)).toBe(7);
  });

  it("keeps newlines in the model, so Shift+Enter survives a rebuild", () => {
    const multi: FlatNode[] = [text("one\ntwo "), skill("burrow"), text("\n")];
    expect(serializeNodes(multi)).toBe("one\ntwo /burrow\n");
    // The offset right after the chip is the last character of the model minus
    // the trailing newline.
    expect(locateOffset(multi, 15)).toEqual({ index: 1, offset: 7 });
  });
});

describe("flatOffset", () => {
  it("ignores an index past the list instead of walking off the end", () => {
    expect(flatOffset(NODES, 99, 0)).toBe("fix /agent-browser now".length);
  });
});

describe("chipSignature", () => {
  it("changes only when the chips do", () => {
    expect(chipSignature(NODES)).toBe("skill:agent-browser");
    // Editing the surrounding text must not trigger a DOM rebuild.
    expect(chipSignature([text("FIX "), skill("agent-browser"), text("!")])).toBe("skill:agent-browser");
    expect(chipSignature([text("fix "), text(" now")])).toBe("");
    // Order matters: two chips swapped is a different DOM.
    expect(chipSignature([skill("a"), skill("b")])).not.toBe(chipSignature([skill("b"), skill("a")]));
    // So does kind: a name that becomes a skill needs a rebuild to change look.
    expect(chipSignature([command("pr")])).not.toBe(chipSignature([skill("pr")]));
  });
});

import { describe, it, expect } from "vitest";
import { splitSkillTokens, hasSkillToken } from "./skillTokens";

const NAMES = ["burrow", "caveman:caveman"];

describe("splitSkillTokens", () => {
  it("pills a known skill at start", () => {
    expect(splitSkillTokens("/burrow fix it", NAMES)).toEqual([
      { pill: true, v: "/burrow" },
      { pill: false, v: " fix it" },
    ]);
  });

  it("pills mid-message after whitespace, ignores unknown and path-like slashes", () => {
    expect(splitSkillTokens("use /burrow on src/foo and /unknown", NAMES)).toEqual([
      { pill: false, v: "use " },
      { pill: true, v: "/burrow" },
      { pill: false, v: " on src/foo and /unknown" },
    ]);
  });

  it("handles plugin-scoped names and no-match text", () => {
    expect(hasSkillToken("run /caveman:caveman now", NAMES)).toBe(true);
    expect(hasSkillToken("plain text", NAMES)).toBe(false);
    expect(splitSkillTokens("", NAMES)).toEqual([{ pill: false, v: "" }]);
  });
});

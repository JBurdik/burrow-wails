import { describe, it, expect } from "vitest";
import { splitMentions } from "./mentionTokens";

describe("splitMentions", () => {
  it("splits a mention at start and mid-text", () => {
    expect(splitMentions("@src/app.ts and @README.md ok")).toEqual([
      { mention: true, v: "@src/app.ts" },
      { mention: false, v: " and " },
      { mention: true, v: "@README.md" },
      { mention: false, v: " ok" },
    ]);
  });

  it("ignores emails, bare @ and mid-word @", () => {
    for (const t of ["mail me@example.com", "just @ alone", "a@b"]) {
      expect(splitMentions(t)).toEqual([{ mention: false, v: t }]);
    }
  });

  it("returns the whole string when there is nothing to split", () => {
    expect(splitMentions("plain")).toEqual([{ mention: false, v: "plain" }]);
    expect(splitMentions("")).toEqual([{ mention: false, v: "" }]);
  });

  it("strips trailing sentence punctuation from the mention", () => {
    expect(splitMentions("check @src/app.ts.")).toEqual([
      { mention: false, v: "check " },
      { mention: true, v: "@src/app.ts" },
      { mention: false, v: "." },
    ]);
    expect(splitMentions("see @README.md, thanks")).toEqual([
      { mention: false, v: "see " },
      { mention: true, v: "@README.md" },
      { mention: false, v: ", thanks" },
    ]);
  });
});

import { describe, it, expect } from "vitest";
import { parsePrRef } from "./prLinkInput";

describe("parsePrRef", () => {
  it("reads numbers", () => {
    expect(parsePrRef("#12")).toEqual({ number: 12 });
    expect(parsePrRef(" 7 ")).toEqual({ number: 7 });
    expect(parsePrRef("#0")).toBeNull();
  });

  it("needs a # in free text", () => {
    expect(parsePrRef("123", false)).toBeNull();
    expect(parsePrRef("#123", false)).toEqual({ number: 123 });
  });

  it("reads every forge's PR URL", () => {
    expect(parsePrRef("https://github.com/a/b/pull/42")?.number).toBe(42);
    expect(parsePrRef("https://github.com/a/b/pull/42/files?w=1")?.number).toBe(42);
    expect(parsePrRef("https://gitlab.com/g/p/-/merge_requests/7")?.number).toBe(7);
    expect(parsePrRef("https://codeberg.org/a/b/pulls/3")?.number).toBe(3);
    expect(parsePrRef("https://dev.azure.com/o/p/_git/r/pullrequest/9")?.number).toBe(9);
    expect(parsePrRef("https://github.com/a/b/pull/42")?.url).toBe("https://github.com/a/b/pull/42");
  });

  it("rejects everything else", () => {
    expect(parsePrRef("https://github.com/a/b/issues/4")).toBeNull();
    expect(parsePrRef("fix #12 please")).toBeNull();
    expect(parsePrRef("")).toBeNull();
  });
});

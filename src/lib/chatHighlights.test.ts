import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@/lib/chatTypes";
import { highlightFiles, highlightLinks, isReviewFile, shortPath } from "./chatHighlights";

let id = 0;
const tool = (name: string, input: Record<string, unknown>) => ({ id: ++id, role: "tool", text: name, toolInput: input }) as ChatMessage;
const say = (text: string) => ({ id: ++id, role: "assistant", text }) as ChatMessage;

describe("chatHighlights", () => {
  it("lists written/edited files newest first, one row per path", () => {
    const msgs = [
      tool("Write", { file_path: "/r/a.ts" }),
      tool("Read", { file_path: "/r/b.ts" }),
      tool("Edit", { file_path: "/r/a.ts" }),
      tool("Edit", { file_path: "/r/c.ts" }),
    ];
    expect(highlightFiles(msgs)).toEqual([
      { path: "/r/c.ts", op: "edited" },
      { path: "/r/a.ts", op: "wrote" },
    ]);
  });

  it("finds links, trims punctuation, flags PRs", () => {
    const links = highlightLinks([
      say("See https://x.dev/docs."),
      say("Opened https://github.com/o/r/pull/42, and https://x.dev/docs again."),
    ]);
    expect(links).toEqual([
      { url: "https://github.com/o/r/pull/42", pr: true },
      { url: "https://x.dev/docs", pr: false },
    ]);
  });

  it("shortens paths inside the thread's folder", () => {
    expect(shortPath("/repo/src/a.ts", "/repo")).toBe("src/a.ts");
    expect(shortPath("/tmp/x", "/repo")).toBe("/tmp/x");
  });

  it("ignores links in code and hostless ones", () => {
    const links = highlightLinks([say("Run `curl https://api.example.com/x`:\n```bash\ncurl https://example.com/install.sh\n```\nDocs: https://docs.example.com and http://foo")]);
    expect(links.map((l) => l.url)).toEqual(["https://docs.example.com"]);
  });

  it("calls only documents/pictures review files", () => {
    expect(isReviewFile("/r/mock.html")).toBe(true);
    expect(isReviewFile("/r/src/main.go")).toBe(false);
  });
});

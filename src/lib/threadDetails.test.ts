import { describe, expect, it } from "vitest";
import { sumNumstat, visibleRows, type DetailsContext } from "./threadDetails";

const base: DetailsContext = { kind: "chat", desktop: true, branch: "main", hasPr: false, added: 0, removed: 0, servers: 0, scheduled: 0, lineage: 0 };

describe("visibleRows", () => {
  it("shows the always-on rows and hides the empty ones", () => {
    expect(visibleRows(base)).toEqual(["workspace", "openIn", "scripts", "branch", "commit"]);
  });

  it("drops every git row outside a repo and Open in on the web", () => {
    expect(visibleRows({ ...base, branch: "", hasPr: true, desktop: false })).toEqual(["workspace", "scripts"]);
  });

  it("shows rows that have something to show", () => {
    const rows = visibleRows({ ...base, hasPr: true, added: 3, servers: 1, scheduled: 2, lineage: 1 });
    expect(rows).toEqual(["workspace", "openIn", "scripts", "branch", "pr", "commit", "changes", "servers", "scheduled", "lineage"]);
  });

  it("keeps chat-only rows to chats", () => {
    for (const kind of ["tab", "workspace"] as const) {
      const rows = visibleRows({ ...base, kind, scheduled: 2, lineage: 1 });
      expect(rows).not.toContain("scheduled");
      expect(rows).not.toContain("lineage");
    }
  });
});

describe("sumNumstat", () => {
  it("totals lines and skips binary files", () => {
    expect(sumNumstat("3\t1\ta.ts\n-\t-\timg.png\n10\t0\tb.ts\n")).toEqual({ added: 13, removed: 1 });
    expect(sumNumstat("")).toEqual({ added: 0, removed: 0 });
  });
});

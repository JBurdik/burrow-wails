import { describe, expect, it } from "vitest";
import { branchTreeRows } from "./branchTree";

const names = ["main", "JB/fix/a", "JB/fix/b", "JB/task/c", "dev"];
const show = (rows: ReturnType<typeof branchTreeRows<string>>) =>
  rows.map((r) => `${"  ".repeat(r.depth)}${r.kind === "folder" ? `${r.label}/ (${r.count})` : r.label}`);

describe("branchTreeRows", () => {
  it("groups by slash, folders first", () => {
    expect(show(branchTreeRows(names, (s) => s, () => false))).toEqual([
      "JB/ (3)", "  fix/ (2)", "    a", "    b", "  task/ (1)", "    c", "main", "dev",
    ]);
  });
  it("hides collapsed folders unless expandAll", () => {
    expect(show(branchTreeRows(names, (s) => s, (k) => k === "JB/fix"))).toEqual([
      "JB/ (3)", "  fix/ (2)", "  task/ (1)", "    c", "main", "dev",
    ]);
    expect(branchTreeRows(names, (s) => s, (k) => k === "JB", true)).toHaveLength(8);
  });
});

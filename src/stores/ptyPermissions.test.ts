import { describe, expect, it } from "vitest";
import { describePermission, headOfQueue, type PtyPermission } from "./ptyPermissions";

const req = (id: string, at: number, tool_input: unknown = {}): PtyPermission => ({
  id, pty_id: "3", tool_name: "Bash", tool_input, at,
});

describe("headOfQueue", () => {
  it("shows the oldest request with a +N counter for the rest", () => {
    const q = headOfQueue([req("b", 20), req("a", 10), req("c", 30)]);
    expect(q?.head.id).toBe("a");
    expect(q?.more).toBe(2);
  });
  it("is null when nothing is pending", () => {
    expect(headOfQueue([])).toBeNull();
  });
});

describe("describePermission", () => {
  it("prefers the command, falls back to the raw input", () => {
    expect(describePermission(req("a", 1, { command: "npm test" }))).toBe("npm test");
    expect(describePermission(req("a", 1, { foo: 1 }))).toBe('{"foo":1}');
  });
});

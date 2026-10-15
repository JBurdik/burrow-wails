import { describe, expect, it } from "vitest";
import { addTerm, findServed, onlyShell, removeTerm, type RpTerms } from "./rpTerminals";

describe("rpTerminals", () => {
  it("dedups a served command per cwd, ignoring whitespace", () => {
    const s: RpTerms = { terms: [], active: null };
    addTerm(s, { id: 1, cwd: "/r", label: "Terminal" });
    addTerm(s, { id: 2, cwd: "/r", label: "pnpm dev", cmd: "pnpm dev" });
    expect(findServed(s, "  pnpm   dev ", "/r")?.id).toBe(2);
    expect(findServed(s, "pnpm dev", "/r/web")).toBeUndefined();
    // A scratch shell never matches, even with an empty command.
    expect(findServed(s, "", "/r")).toBeUndefined();
    expect(findServed(undefined, "pnpm dev", "/r")).toBeUndefined();
  });

  it("closing the active terminal activates its neighbour", () => {
    const s: RpTerms = { terms: [], active: null };
    [1, 2, 3].forEach((id) => addTerm(s, { id, cwd: "/r", label: String(id) }));
    s.active = 2;
    removeTerm(s, 2);
    expect(s.active).toBe(3);
    removeTerm(s, 3);
    expect(s.active).toBe(1);
    removeTerm(s, 1);
    expect(s).toEqual({ terms: [], active: null });
  });

  it("tells a stopped server (bare shell) from a running one", () => {
    expect(["zsh", "-zsh", "bash", ""].every(onlyShell)).toBe(true);
    expect(["node", "python3", "zshy"].some(onlyShell)).toBe(false);
  });
});

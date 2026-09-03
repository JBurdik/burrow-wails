import { describe, it, expect } from "vitest";
import { resolveWelcomeVisible } from "./ui";

describe("resolveWelcomeVisible", () => {
  it("auto (null): composer only when no live tab", () => {
    expect(resolveWelcomeVisible(null, 0)).toBe(true);
    expect(resolveWelcomeVisible(null, 1)).toBe(false);
  });

  it("explicit open/dismiss wins over the tab count", () => {
    expect(resolveWelcomeVisible(true, 3)).toBe(true);
    expect(resolveWelcomeVisible(false, 0)).toBe(false);
  });
});

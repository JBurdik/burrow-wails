import { describe, it, expect } from "vitest";
import { THEMES, THEME_FAMILIES, familyOf, variantFor, findFamily } from "./themes";

describe("theme families", () => {
  it("pairs a real light variant with a real dark variant", () => {
    for (const f of THEME_FAMILIES) {
      const light = THEMES.find((t) => t.key === f.light);
      const dark = THEMES.find((t) => t.key === f.dark);
      expect(light, `${f.key}.light`).toBeDefined();
      expect(dark, `${f.key}.dark`).toBeDefined();
      expect(light!.isDark, `${f.key}.light must be light`).toBe(false);
      expect(dark!.isDark, `${f.key}.dark must be dark`).toBe(true);
    }
  });

  it("claims every variant exactly once", () => {
    const claimed = THEME_FAMILIES.flatMap((f) => [f.light, f.dark]);
    expect(new Set(claimed).size).toBe(claimed.length);
    expect(new Set(claimed)).toEqual(new Set(THEMES.map((t) => t.key)));
  });

  it("resolves family × scheme back to the right variant", () => {
    for (const f of THEME_FAMILIES) {
      expect(variantFor(f, "light").key).toBe(f.light);
      expect(variantFor(f, "dark").key).toBe(f.dark);
      // familyOf is what migrates an old stored variant key to its family.
      expect(familyOf(f.light).key).toBe(f.key);
      expect(familyOf(f.dark).key).toBe(f.key);
    }
  });

  it("falls back to the first family for unknown keys", () => {
    expect(findFamily("nope").key).toBe(THEME_FAMILIES[0].key);
    expect(familyOf("nope").key).toBe(THEME_FAMILIES[0].key);
  });
});

import { describe, expect, it } from "vitest";
import { inlineLocalAssets, resolvePath } from "./htmlInline";

describe("htmlInline", () => {
  it("resolves relative paths", () => {
    expect(resolvePath("/a/b", "../c/d.png?x=1")).toBe("/a/c/d.png");
    expect(resolvePath("/a/b", "./my%20img.png")).toBe("/a/b/my img.png");
  });

  it("inlines stylesheets, their fonts, images; leaves remote alone", async () => {
    const files: Record<string, string> = {
      "/p/style.css": `@font-face{src:url("fonts/F.ttf")} body{color:red}`,
    };
    const html = `<link rel="stylesheet" href="style.css"><link rel="stylesheet" href="https://x/y.css"><img src="img/a.jpg"><img src="https://x/b.png">`;
    const out = await inlineLocalAssets(html, "/p/index.html",
      async (p) => { if (!(p in files)) throw new Error("nope"); return files[p]; },
      async (p) => `B64(${p})`);
    expect(out).toContain(`<style>@font-face{src:url("data:font/ttf;base64,B64(/p/fonts/F.ttf)")} body{color:red}</style>`);
    expect(out).toContain(`href="https://x/y.css"`);
    expect(out).toContain(`src="data:image/jpeg;base64,B64(/p/img/a.jpg)"`);
    expect(out).toContain(`src="https://x/b.png"`);
  });
});

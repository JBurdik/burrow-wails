import { describe, expect, it } from "vitest";
import { hasHiddenChars, renderChatMd } from "./chatBlocks";

describe("shell blocks", () => {
  it("adds Run + Copy to a closed bash fence", () => {
    const html = renderChatMd("```bash\nnpm test\n```\n");
    expect(html).toContain('data-sh-action="run"');
    expect(html).toContain(`data-shell-src="${encodeURIComponent("npm test")}"`);
  });

  it("leaves a still-streaming fence and other languages alone", () => {
    expect(renderChatMd("```bash\nnpm te")).not.toContain("data-sh-action");
    expect(renderChatMd("```ts\nconst a = 1\n```\n")).not.toContain("data-sh-action");
  });

  it("offers no Run for hidden characters, still Copy", () => {
    expect(hasHiddenChars("ls‮")).toBe(true);
    expect(hasHiddenChars("echo a​")).toBe(true);
    expect(hasHiddenChars("echo\x1b[2J")).toBe(true);
    expect(hasHiddenChars("for f in *; do\n\techo $f\ndone")).toBe(false);
    const html = renderChatMd("```sh\nrm -rf /tmp/x‮\n```\n");
    expect(html).not.toContain('data-sh-action="run"');
    expect(html).toContain('data-sh-action="copy"');
  });
});

import { describe, expect, it } from "vitest";
import { formatOptionsReply, mediaKind, optionLetter, parseOptions, renderOptions } from "./optionsBlock";
import { renderChatMd } from "./chatBlocks";

const spec = {
  question: "Which sidebar?",
  options: [
    { label: "Dense", note: "less padding", media: ["a.png", "a-drag.mp4"] },
    { label: "Airy", media: ["/abs/b.html"] },
    { label: "No media", media: [] },
    { label: "Bad ext", media: ["x.txt"] },
  ],
  current: ["now.png"],
};

describe("options block", () => {
  it("keeps options with a label and at least one known media file", () => {
    const s = parseOptions(JSON.stringify(spec))!;
    expect(s.question).toBe("Which sidebar?");
    expect(s.options.map((o) => o.label)).toEqual(["Dense", "Airy"]);
    expect(s.options[0]).toEqual({ label: "Dense", note: "less padding", media: ["a.png", "a-drag.mp4"] });
    expect(s.current).toEqual(["now.png"]);
  });

  it("needs two options, or one against current; bad JSON is null", () => {
    expect(parseOptions("{nope")).toBeNull();
    expect(parseOptions(JSON.stringify({ options: [{ label: "A", media: ["a.png"] }] }))).toBeNull();
    expect(parseOptions(JSON.stringify({ options: [{ label: "A", media: "a.png" }], current: "c.png" }))).not.toBeNull();
  });

  it("classifies media and letters options", () => {
    expect([mediaKind("x.PNG"), mediaKind("x.webm"), mediaKind("x.htm"), mediaKind("x.pdf")]).toEqual(["image", "video", "html", null]);
    expect([optionLetter(0), optionLetter(25), optionLetter(26)]).toEqual(["A", "Z", "AA"]);
  });

  it("formats the reply", () => {
    const s = parseOptions(JSON.stringify(spec))!;
    expect(formatOptionsReply(s, [1, 0], "  bigger  ")).toBe("Picked: A (Dense), B (Airy)\n\nbigger");
    expect(formatOptionsReply(s, [1], "")).toBe("Picked: B (Airy)");
    expect(formatOptionsReply(s, [], "neither works")).toBe("None of them: neither works");
    expect(formatOptionsReply(s, [], "")).toBe("None of them.");
  });

  it("renders a card with escaped labels and the spec, only for a closed fence", () => {
    const html = renderOptions(JSON.stringify({ options: [{ label: "<b>", media: ["a.png"] }, { label: "B", media: ["b.mp4"] }] }), false)!;
    expect(html).toContain("&lt;b&gt;");
    expect(html).not.toContain("<b><");
    expect(html).toContain('data-cv-image="a.png"');
    expect(html).toContain("Video");
    const block = "```options\n" + JSON.stringify(spec) + "\n```";
    expect(renderChatMd(block)).toContain('class="co"');
    expect(renderChatMd(block.slice(0, -3))).not.toContain('class="co"');
    expect(renderChatMd("```options\n{bad\n```")).toContain("<pre>");
  });
});

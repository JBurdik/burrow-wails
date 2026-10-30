import { describe, it, expect } from "vitest";
import { releaseNotesSince } from "./whatsNew";

const rel = (v: string) => `<div class="release"><span class="version">${v}</span></div>\n`;
const doc = `<html><style>x</style><div class="wrap"><h1>C</h1>${rel("Beta")}${rel("v0.7.0")}${rel("v0.6.0")}${rel("v0.5.2")}</div></body></html>`;

describe("releaseNotesSince", () => {
  it("picks releases in (seen, current], skipping unreleased beta", () => {
    const out = releaseNotesSince(doc, "0.7.0", "0.5.2");
    expect(out).toContain("v0.7.0");
    expect(out).toContain("v0.6.0");
    expect(out).not.toContain("v0.5.2");
    expect(out).not.toContain("Beta");
    expect(out).toContain("<style>x</style>");
  });
  it("is empty when the current version has no entry", () => {
    expect(releaseNotesSince(doc, "0.8.0", "0.7.0")).toBe("");
  });
});

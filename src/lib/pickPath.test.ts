import { describe, it, expect } from "vitest";
import { extOf, mimeFor, AUDIO_EXTS, IMAGE_EXTS } from "./pickPath";

describe("extOf", () => {
  it("reads the extension, lowercased", () => {
    expect(extOf("/a/b/Cover.PNG")).toBe("png");
    expect(extOf("beep.wav")).toBe("wav");
  });

  it("returns empty for an extensionless name", () => {
    expect(extOf("/Users/me/code")).toBe("");
    expect(extOf("Makefile")).toBe("");
  });

  it("ignores dots in parent dirs", () => {
    // A dotted folder must not make its extensionless child look like a file.
    expect(extOf("/Users/me/.config/settings")).toBe("");
  });
});

describe("mimeFor", () => {
  it("maps the formats the picker previews", () => {
    expect(mimeFor("a.svg")).toBe("image/svg+xml");
    expect(mimeFor("a.jpeg")).toBe("image/jpeg");
    expect(mimeFor("a.mp3")).toBe("audio/mpeg");
    expect(mimeFor("a.m4a")).toBe("audio/mp4");
  });

  it("gives every previewable extension a real MIME", () => {
    // A generic octet-stream would make <img>/<audio> reject the data URL.
    for (const ext of [...IMAGE_EXTS, ...AUDIO_EXTS]) {
      expect(mimeFor(`x.${ext}`), ext).not.toBe("application/octet-stream");
    }
  });

  it("falls back for anything else", () => {
    expect(mimeFor("notes.txt")).toBe("application/octet-stream");
  });
});

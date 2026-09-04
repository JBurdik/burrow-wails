import { describe, expect, it } from "vitest";
import { parseTextGenerationValue, textGenerationValue } from "./chatModels";

// The text-generation preference is one string that has changed shape three
// times; every past shape must stay readable, and the current one must survive
// a round-trip so a pinned effort can't leak onto another model.
describe("text generation selection", () => {
  it("round-trips with and without a pinned effort", () => {
    expect(parseTextGenerationValue(textGenerationValue("claude", "claude", "claude-opus-5", "xhigh"))).toEqual({
      kind: "claude", providerId: "claude", modelId: "claude-opus-5", effort: "xhigh",
    });
    expect(textGenerationValue("claude", "claude", "claude-opus-5")).toBe("claude::claude::claude-opus-5");
    expect(parseTextGenerationValue("claude::claude::claude-opus-5").effort).toBe("");
  });

  it("keeps reading the formats the preference used to hold", () => {
    expect(parseTextGenerationValue("claude-haiku-4-5")).toEqual({
      kind: "claude", providerId: "claude", modelId: "claude-haiku-4-5", effort: "",
    });
    expect(parseTextGenerationValue("codex::gpt-5.2-codex")).toEqual({
      kind: "codex", providerId: "codex", modelId: "gpt-5.2-codex", effort: "",
    });
  });

  it("treats the last field as the effort, never as part of the model id", () => {
    // A provider instance id and a model id are both free-form, so the parse
    // must not simply count separators from the left.
    expect(parseTextGenerationValue("codex::codex_personal::openai::gpt-5::low")).toEqual({
      kind: "codex", providerId: "codex_personal", modelId: "openai::gpt-5", effort: "low",
    });
  });
});

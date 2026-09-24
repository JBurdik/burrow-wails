import { beforeEach, describe, expect, it } from "vitest";
import {
  addCustomModel,
  hideAllModels,
  MODELS_BY_AGENT,
  modelsFor,
  moveModel,
  parseTextGenerationValue,
  removeCustomModel,
  resetModelPrefsForTest,
  setHidden,
  textGenerationValue,
  visibleModelsFor,
} from "./chatModels";

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

// Ordering, visibility and custom ids are what the Providers panel writes; the
// composer reads them back through visibleModelsFor. Every case below is one a
// user can reach with two clicks in that panel.
describe("model list preferences", () => {
  beforeEach(() => resetModelPrefsForTest());

  it("offers the shipped catalog untouched by default", () => {
    expect(modelsFor("claude")[0].id).toBe("claude-opus-5-5");
    expect(visibleModelsFor("claude")).toEqual(modelsFor("claude"));
  });

  it("appends custom ids and refuses ones the catalog already has", () => {
    expect(addCustomModel("claude", "claude-opus-9")).toBe(true);
    expect(addCustomModel("claude", "claude-opus-9")).toBe(false);
    expect(addCustomModel("claude", "claude-opus-5-5")).toBe(false);
    const all = modelsFor("claude");
    expect(all[all.length - 1]).toEqual({ id: "claude-opus-9", label: "claude-opus-9" });
    removeCustomModel("claude", "claude-opus-9");
    expect(modelsFor("claude").some((m) => m.id === "claude-opus-9")).toBe(false);
  });

  it("hides a model from the picker but keeps it in settings", () => {
    setHidden("claude", "claude-opus-5", true);
    expect(modelsFor("claude").some((m) => m.id === "claude-opus-5")).toBe(true);
    expect(visibleModelsFor("claude").some((m) => m.id === "claude-opus-5")).toBe(false);
  });

  it("never lets the last visible model be hidden", () => {
    hideAllModels("claude");
    expect(visibleModelsFor("claude")).toHaveLength(1);
    // The survivor is the one at the top, and it stays refusable.
    const last = visibleModelsFor("claude")[0];
    expect(setHidden("claude", last.id, true)).toBe(false);
  });

  it("reorders and keeps models a later release adds", () => {
    const before = modelsFor("claude").map((m) => m.id);
    moveModel("claude", before[2], -1);
    const after = modelsFor("claude").map((m) => m.id);
    expect(after[1]).toBe(before[2]);
    expect(after[2]).toBe(before[1]);
    expect(after.slice().sort()).toEqual(before.slice().sort());
  });

  it("drops stale ids from a saved order instead of losing the ordering", () => {
    addCustomModel("claude", "temp-model");
    moveModel("claude", "temp-model", -1); // writes an order that mentions it
    removeCustomModel("claude", "temp-model");
    const ids = modelsFor("claude").map((m) => m.id);
    expect(ids).not.toContain("temp-model");
    expect(ids).toHaveLength(MODELS_BY_AGENT.claude.length);
  });
});

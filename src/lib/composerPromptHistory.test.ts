import { describe, expect, it } from "vitest";
import type { ChatMessage } from "./chatTypes";
import { promptHistory, stepPromptHistory } from "./composerPromptHistory";

const msg = (id: number, role: ChatMessage["role"], text: string, displayText?: string): ChatMessage =>
  ({ id, role, text, displayText });

describe("composer prompt history", () => {
  it("keeps only what the user typed, folding repeats", () => {
    expect(promptHistory([
      msg(1, "user", "one"),
      msg(2, "assistant", "reply"),
      msg(3, "user", "Sub-agent Explore (#12) finished:\n\nok"),
      msg(4, "user", "Continue where you left off."),
      msg(5, "user", "two", "two [paste]"),
      msg(6, "user", "two", "two [paste]"),
      msg(7, "user", "  "),
    ])).toEqual(["one", "two [paste]"]);
  });

  it("walks back, forward, and returns the draft", () => {
    const entries = ["a", "b"];
    const up1 = stepPromptHistory(entries, null, "draft", "back")!;
    expect(up1.text).toBe("b");
    const up2 = stepPromptHistory(entries, up1.pos, up1.text, "back")!;
    expect(up2.text).toBe("a");
    expect(stepPromptHistory(entries, up2.pos, up2.text, "back")).toBeNull();
    const down1 = stepPromptHistory(entries, up2.pos, "a", "forward")!;
    expect(down1.text).toBe("b");
    expect(stepPromptHistory(entries, down1.pos, "b", "forward")).toEqual({ pos: null, text: "draft" });
  });

  it("an edited recall is a new draft", () => {
    const up = stepPromptHistory(["a", "b"], null, "", "back")!;
    expect(stepPromptHistory(["a", "b"], up.pos, "b!", "forward")).toBeNull();
    expect(stepPromptHistory(["a", "b"], up.pos, "b!", "back")!.pos!.draft).toBe("b!");
  });
});

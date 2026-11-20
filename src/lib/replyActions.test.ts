import { describe, expect, it } from "vitest";
import { renderChatMd } from "./chatBlocks";
import { parseAction, pressKey, withActions, type ActionPress } from "./replyActions";

const block = (o: unknown) => "```action\n" + JSON.stringify(o) + "\n```";
const go = { label: "Fill in the form", kind: "new_thread", brief: "Do the form" };
const send = { label: "Ask it", kind: "send", brief: "hi", chat_id: 7 };
const render = (text: string, presses: ActionPress[] = []) =>
  withActions(3, new Map(presses.map((p) => [pressKey(p.msg_index, p.action_index), p])), (id) => (id === 7 ? "Meeting" : ""), () => renderChatMd(text));

describe("parseAction", () => {
  it.each([
    [go, true],
    [send, true],
    [{ ...send, chat_id: undefined }, false],
    [{ ...go, kind: "rm" }, false],
    [{ ...go, brief: " " }, false],
  ])("%o → %s", (o, ok) => expect(!!parseAction(JSON.stringify(o))).toBe(ok));
  it("rejects non-JSON", () => expect(parseAction("{nope")).toBeNull());
});

describe("renderActionBlock", () => {
  it("numbers closed blocks in order, an invalid one too (as Go does)", () => {
    const html = render(`${block(go)}\ntext\n\`\`\`action\n{nope\n\`\`\`\n${block(send)}`);
    expect(html).toContain('data-ca-msg="3" data-ca-index="0"');
    expect(html).toContain('data-ca-index="2"');
    expect(html).not.toContain('data-ca-index="1"');
    expect(html).toContain("Sends to Meeting");
  });
  it("an unclosed (streaming) block stays code", () => {
    expect(render("```action\n" + JSON.stringify(go))).not.toContain('class="ca ');
  });
  it("shows press state", () => {
    const html = render(block(go), [{ chat_id: 1, msg_index: 3, action_index: 0, label: "x", state: "done", result_chat_id: 42 }]);
    expect(html).toContain("ca-done");
    expect(html).toContain('data-ca-result="42"');
  });
  it("outside withActions it is a code block", () => {
    expect(renderChatMd(block(go))).not.toContain('class="ca ');
  });
});

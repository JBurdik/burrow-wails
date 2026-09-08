import { describe, it, expect, vi } from "vitest";
import { nextTick, ref } from "vue";

const invoke = vi.hoisted(() => vi.fn(async (cmd: string) => {
  if (cmd === "list_skills") {
    return [
      { name: "burrow", description: "Delegate work", enabled: true },
      { name: "brainstorming", description: "Explore intent", enabled: true },
      { name: "off", description: "Disabled", enabled: false },
    ];
  }
  if (cmd === "run_git") return { stdout: "src/App.vue\nsrc/lib/deep/App.ts\nREADME.md\n" };
  throw new Error(`unexpected ${cmd}`);
}));
vi.mock("@tauri-apps/api/core", () => ({ invoke }));

import { useComposerCompletion } from "./composerCompletion";

// A bare object is enough: the composable only reads selectionStart and writes
// selectionStart/selectionEnd/focus back.
function fakeTextarea(caret: number) {
  return { selectionStart: caret, selectionEnd: caret, scrollTop: 0, focus() {} } as unknown as HTMLTextAreaElement;
}

function setup(initial: string, caret = initial.length) {
  const text = ref(initial);
  const el = fakeTextarea(caret);
  const c = useComposerCompletion({
    text,
    element: () => el,
    cwd: () => "/repo",
    commands: ref([{ name: "compact", description: "Compact history" }]),
  });
  return { text, el, c };
}

describe("composer completion triggers", () => {
  it("completes a repo path after @, ranking basename prefixes first", async () => {
    const { c } = setup("look at @App");
    await c.update();
    expect(c.suggestions.value.map((s) => s.insert)).toEqual(["@src/App.vue", "@src/lib/deep/App.ts"]);
    // Left column is the basename, right column the full path.
    expect(c.suggestions.value[0].label).toBe("@App.vue");
    expect(c.suggestions.value[0].hint).toBe("src/App.vue");
  });

  it("completes commands after / and skills after $, both inserting /name", async () => {
    const slash = setup("/comp");
    await slash.c.update();
    expect(slash.c.suggestions.value.map((s) => s.insert)).toEqual(["/compact"]);

    const dollar = setup("run $bu");
    // The shared skill fetch resolves on its own microtask.
    await nextTick();
    await dollar.c.update();
    expect(dollar.c.suggestions.value.map((s) => s.insert)).toEqual(["/burrow"]);
  });

  it("ignores a trigger glued to the end of a word", async () => {
    const { c } = setup("mail@example");
    await c.update();
    expect(c.suggestions.value).toEqual([]);
  });

  it("only offers enabled skills", async () => {
    const { c } = setup("$");
    await nextTick();
    await c.update();
    expect(c.suggestions.value.map((s) => s.insert)).not.toContain("/off");
  });
});

describe("applying a suggestion", () => {
  it("replaces the token in place, keeps the tail, and leaves the caret after it", async () => {
    const { text, el, c } = setup("see @App then fix it", 8); // caret right after "@App"
    await c.update();
    c.apply(c.suggestions.value[0]);
    expect(text.value).toBe("see @src/App.vue then fix it");
    await nextTick();
    expect(el.selectionStart).toBe("see @src/App.vue".length);
  });

  it("adds the separating space only when the tail does not start with one", async () => {
    const a = setup("$bu");
    await nextTick();
    await a.c.update();
    a.c.apply(a.c.suggestions.value[0]);
    expect(a.text.value).toBe("/burrow ");

    const b = setup("$bu rest", 3);
    await nextTick();
    await b.c.update();
    b.c.apply(b.c.suggestions.value[0]);
    expect(b.text.value).toBe("/burrow rest");
  });
});

describe("keyboard ownership", () => {
  it("claims Arrow/Enter/Escape only while the list is open", async () => {
    const { c } = setup("@App");
    const key = (k: string) => {
      const e = { key: k, shiftKey: false, preventDefault: () => {} } as KeyboardEvent;
      return c.handleKeydown(e);
    };
    // Closed: the host's Enter must still send the message.
    expect(key("Enter")).toBe(false);

    await c.update();
    expect(c.suggestions.value.length).toBe(2);
    expect(key("ArrowDown")).toBe(true);
    expect(c.activeIndex.value).toBe(1);
    expect(key("ArrowDown")).toBe(true);
    expect(c.activeIndex.value).toBe(1); // clamped at the end
    expect(key("Escape")).toBe(true);
    expect(c.suggestions.value).toEqual([]);
    expect(key("Enter")).toBe(false);
  });
});

describe("skill pills", () => {
  it("marks an installed /skill token and leaves an unknown one alone", async () => {
    const { text, c } = setup("/burrow now");
    await nextTick();
    expect(c.hasSkillPill.value).toBe(true);
    expect(c.skillParts.value.filter((p) => p.pill).map((p) => p.v)).toEqual(["/burrow"]);

    text.value = "/nosuchskill now";
    expect(c.hasSkillPill.value).toBe(false);
  });
});

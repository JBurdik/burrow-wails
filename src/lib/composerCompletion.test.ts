import { describe, it, expect, vi } from "vitest";
import { nextTick, ref } from "vue";

const SKILLS = [
  { dir: "agent-browser", name: "agent-browser", description: "Browser automation CLI.", source: "personal", enabled: true },
  { dir: "burrow", name: "burrow", description: "Delegate work.", source: "personal", enabled: true },
  { dir: "off", name: "off", description: "Disabled.", source: "personal", enabled: false },
  // Same name from two roots: the repo's own copy has to win, because that is
  // the one the agent would resolve.
  { dir: "burrow-release", name: "burrow", description: "Repo override.", source: "project", enabled: true },
];

const invoke = vi.hoisted(() => vi.fn(async (cmd: string) => {
  if (cmd === "list_skills") return SKILLS;
  if (cmd === "run_git") return { stdout: "src/App.vue\nsrc/lib/deep/App.ts\nREADME.md\n" };
  throw new Error(`unexpected ${cmd}`);
}));
vi.mock("@tauri-apps/api/core", () => ({ invoke }));

import { useComposerCompletion, skillLabel } from "./composerCompletion";

// ComposerTextInput's caret half, without a DOM. The offsets are into the
// plain-text model, which is all the composable ever deals in.
function fakeInput(caret: number) {
  let at = caret;
  return { focus() {}, caret: () => at, setCaret: (n: number) => { at = n; } };
}

let repo = 0;
function setup(initial: string, caret = initial.length) {
  const text = ref(initial);
  const el = fakeInput(caret);
  // A fresh cwd per test: the skill/file lists are cached per repo.
  const cwd = `/repo${repo++}`;
  const c = useComposerCompletion({
    text,
    input: () => el,
    cwd: () => cwd,
    commands: ref([{ name: "compact", description: "Compact history" }]),
  });
  return { text, el, c };
}

/** The skill list resolves on its own microtask chain. */
const settled = () => nextTick().then(nextTick);

describe("skillLabel", () => {
  it("title-cases the invocation name for display", () => {
    expect(skillLabel("agent-browser")).toBe("Agent Browser");
    expect(skillLabel("app-store-preflight-skills")).toBe("App Store Preflight Skills");
    expect(skillLabel("clean_gone")).toBe("Clean Gone");
    expect(skillLabel("bprod")).toBe("Bprod");
  });
});

describe("composer completion triggers", () => {
  it("completes a repo path after @, ranking basename prefixes first", async () => {
    const { c } = setup("look at @App");
    await c.update();
    expect(c.suggestions.value.map((s) => s.insert)).toEqual(["@src/App.vue", "@src/lib/deep/App.ts"]);
    expect(c.suggestions.value[0].label).toBe("@App.vue");
    expect(c.suggestions.value[0].hint).toBe("src/App.vue");
    expect(c.suggestions.value[0].badge).toBeUndefined();
  });

  it("lists skills after $ with their real description and origin badge", async () => {
    const { c } = setup("run $agent");
    await settled();
    await c.update();
    expect(c.suggestions.value).toHaveLength(1);
    const [row] = c.suggestions.value;
    expect(row.label).toBe("Agent Browser");
    expect(row.hint).toBe("Browser automation CLI.");
    expect(row.badge).toBe("personal");
    // `$` is the trigger; `/name` is what actually lands in the text.
    expect(row.insert).toBe("/agent-browser");
  });

  it("lets a project skill shadow a personal one of the same name", async () => {
    const { c } = setup("$burrow");
    await settled();
    await c.update();
    expect(c.suggestions.value).toHaveLength(1);
    expect(c.suggestions.value[0].badge).toBe("project");
    expect(c.suggestions.value[0].hint).toBe("Repo override.");
  });

  it("omits disabled skills", async () => {
    const { c } = setup("$");
    await settled();
    await c.update();
    expect(c.suggestions.value.map((s) => s.insert)).not.toContain("/off");
  });

  it("completes commands after / and badges them built-in", async () => {
    const { c } = setup("/comp");
    await c.update();
    expect(c.suggestions.value.map((s) => s.insert)).toEqual(["/compact"]);
    expect(c.suggestions.value[0].badge).toBe("command");
  });

  it("ignores a trigger glued to the end of a word", async () => {
    const { c } = setup("mail@example");
    await c.update();
    expect(c.suggestions.value).toEqual([]);
  });
});

describe("applying a suggestion", () => {
  it("splices a file mention in place and leaves the caret after it", async () => {
    const { text, el, c } = setup("see @App then fix it", 8); // caret right after "@App"
    await c.update();
    c.apply(c.suggestions.value[0]);
    expect(text.value).toBe("see @src/App.vue then fix it");
    await nextTick();
    expect(el.caret()).toBe("see @src/App.vue".length);
  });

  it("adds the separating space only when the tail does not start with one", async () => {
    const a = setup("/comp");
    await a.c.update();
    a.c.apply(a.c.suggestions.value[0]);
    expect(a.text.value).toBe("/compact ");

    const b = setup("/comp rest", 5);
    await b.c.update();
    b.c.apply(b.c.suggestions.value[0]);
    expect(b.text.value).toBe("/compact rest");
  });

  it("inserts /name inline where the $token was typed", async () => {
    const { text, c } = setup("$agent");
    await settled();
    await c.update();
    c.apply(c.suggestions.value[0]);
    expect(text.value).toBe("/agent-browser ");
  });

  it("keeps the surrounding text when the $token is mid-message", async () => {
    const { text, c } = setup("please $agent now", 13); // caret after "$agent"
    await settled();
    await c.update();
    c.apply(c.suggestions.value[0]);
    expect(text.value).toBe("please /agent-browser now");
  });
});

describe("keyboard ownership", () => {
  const press = (c: ReturnType<typeof useComposerCompletion>, k: string) =>
    c.handleKeydown({ key: k, shiftKey: false, preventDefault: () => {} } as KeyboardEvent);

  it("claims Arrow/Enter/Escape only while the list is open", async () => {
    const { c } = setup("@App");
    // Closed: the host's Enter must still send the message.
    expect(press(c, "Enter")).toBe(false);

    await c.update();
    expect(c.suggestions.value).toHaveLength(2);
    expect(press(c, "ArrowDown")).toBe(true);
    expect(c.activeIndex.value).toBe(1);
    expect(press(c, "ArrowDown")).toBe(true);
    expect(c.activeIndex.value).toBe(1); // clamped at the end
    expect(press(c, "Escape")).toBe(true);
    expect(c.suggestions.value).toEqual([]);
    expect(press(c, "Enter")).toBe(false);
  });

});

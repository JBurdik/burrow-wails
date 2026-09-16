<template>
  <div
    ref="rootEl"
    v-bind="$attrs"
    class="composer-editable"
    contenteditable="true"
    role="textbox"
    aria-multiline="true"
    spellcheck="false"
    :data-placeholder="placeholder"
    :data-empty="model.length === 0 ? 'true' : undefined"
    @input="onInput"
    @keydown="onKeydown"
    @paste="onPaste"
    @compositionstart="composing = true"
    @compositionend="onCompositionEnd"
  />
  <Teleport to="body">
    <div v-if="pasteDialogText !== null" class="fixed inset-0 z-[1000] flex items-center justify-center bg-black/70" @click.self="closePasteDialog">
      <div
        class="flex max-h-[90vh] w-[90vw] max-w-[900px] flex-col overflow-hidden rounded-lg shadow-[0_24px_64px_rgba(0,0,0,0.5)]"
        style="background: var(--bg-panel, #18181c); border: 1px solid var(--border, rgba(255,255,255,0.08));"
      >
        <div class="flex shrink-0 items-center justify-between border-b px-3 py-2" style="border-color: var(--border, rgba(255,255,255,0.08));">
          <span class="text-sm" style="color: var(--text-secondary, rgba(255,255,255,0.6));">Pasted text</span>
          <button class="flex items-center rounded p-1 hover:bg-white/10" style="color: var(--text-secondary, rgba(255,255,255,0.6));" @click="closePasteDialog">
            <PhX :size="16" />
          </button>
        </div>
        <pre
          class="m-0 flex-1 overflow-auto whitespace-pre-wrap p-3 font-mono text-sm"
          style="color: var(--text-primary, rgba(255,255,255,0.88));"
        >{{ pasteDialogText }}</pre>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from "vue";
import { PhX } from "@phosphor-icons/vue";
import {
  chipSignature, flatOffset, isChip, locateOffset, serializeNodes, tokenize, wrapPaste,
  type ChipKind, type FlatNode,
} from "@/lib/composerDom";

/** A pasted block collapses into a chip once it's longer than this many lines. */
const PASTE_COLLAPSE_LINES = 4;

defineOptions({ inheritAttrs: false });

/**
 * A contenteditable that behaves like a <textarea> to its host — one plain
 * string through v-model — but draws the known `/token`s in that string as
 * atomic inline chips (see .composer-editable in styles/composer.css).
 *
 * It was a real <textarea> with pills painted onto a transparent-text backdrop
 * behind it. That backdrop had to match the textarea's text metrics
 * character-for-character, so a pill could only recolor `/agent-browser` — no
 * icon, no display name. A chip is `contenteditable="false"`, so the browser
 * gives us the atomic behaviour for free: one Backspace deletes it whole,
 * arrows step over it, the caret never lands inside.
 */
const model = defineModel<string>({ default: "" });

const props = withDefaults(defineProps<{
  autofocus?: boolean;
  placeholder?: string;
  /** Installed skills to chip, with the display label to show. */
  skills?: readonly { name: string; label: string }[];
  /** Built-in commands to chip. Rendered as `/name`, since that IS the name. */
  commands?: readonly { name: string }[];
  /** Repo file paths to chip. Rendered with just the basename as the label. */
  files?: readonly string[];
}>(), { autofocus: false, placeholder: "", skills: () => [], commands: () => [], files: () => [] });

const emit = defineEmits<{
  input: [event: Event];
  keydown: [event: KeyboardEvent];
  paste: [event: ClipboardEvent];
}>();

const rootEl = useTemplateRef<HTMLDivElement>("rootEl");
// An IME candidate window is open: the DOM must not be rebuilt under it.
const composing = ref(false);

// Raw Phosphor bold-weight path data (PhTerminal / PhFile), the same icons
// ComposerSuggestions.vue and FileTreeNode.vue already use elsewhere. Chips
// are built with raw DOM calls rather than Vue's render tree, so the path
// data is inlined instead of mounting a live icon component per chip.
const CHIP_ICON_PATH: Partial<Record<ChipKind | "paste", string>> = {
  command: "M120,137,48,201A12,12,0,1,1,32,183l61.91-55L32,73A12,12,0,1,1,48,55l72,64A12,12,0,0,1,120,137Zm96,43H120a12,12,0,0,0,0,24h96a12,12,0,0,0,0-24Z",
  file: "M216.49,79.52l-56-56A12,12,0,0,0,152,20H56A20,20,0,0,0,36,40V216a20,20,0,0,0,20,20H200a20,20,0,0,0,20-20V88A12,12,0,0,0,216.49,79.52ZM160,57l23,23H160ZM60,212V44h76V92a12,12,0,0,0,12,12h48V212Z",
  // Ph "Clipboard Text" bold.
  paste: "M196,32H164.62a44,44,0,0,0-73.24,0H60A20,20,0,0,0,40,52V216a20,20,0,0,0,20,20H196a20,20,0,0,0,20-20V52A20,20,0,0,0,196,32Zm-4,180H64V56H84v8a12,12,0,0,0,12,12h64a12,12,0,0,0,12-12V56h20ZM88,132a12,12,0,0,1,12-12h56a12,12,0,0,1,0,24H100A12,12,0,0,1,88,132Zm0,40a12,12,0,0,1,12-12h56a12,12,0,0,1,0,24H100A12,12,0,0,1,88,172Z",
};
function chipIconSvg(kind: ChipKind | "paste"): string | null {
  const path = CHIP_ICON_PATH[kind];
  if (!path) return null;
  return `<svg class="composer-chip-icon" viewBox="0 0 256 256" aria-hidden="true"><path d="${path}"/></svg>`;
}

const known = () => ({
  skills: props.skills.map((s) => s.name),
  commands: props.commands.map((c) => c.name),
  files: props.files,
});

/** The editable's current children, as the model-shaped node list. */
function readNodes(): FlatNode[] {
  const root = rootEl.value;
  if (!root) return [];
  const out: FlatNode[] = [];
  const children = Array.from(root.childNodes);
  children.forEach((node, i) => {
    if (node.nodeType === Node.TEXT_NODE) {
      out.push({ kind: "text", text: node.nodeValue ?? "" });
      return;
    }
    const el = node as HTMLElement;
    if (el.dataset?.chip === "paste") {
      out.push({ kind: "paste", text: el.dataset.pasteText ?? "" });
      return;
    }
    const name = el.dataset?.skill;
    if (name) {
      const kind = el.dataset.chip === "command" ? "command" : el.dataset.chip === "file" ? "file" : "skill";
      out.push({ kind, name });
      return;
    }
    // A trailing <br> is the browser's own filler for an empty last line, not
    // a newline the user typed — counting it would append a phantom "\n" to
    // the model on every render.
    if (el.tagName === "BR") {
      if (i < children.length - 1) out.push({ kind: "text", text: "\n" });
      return;
    }
    // Anything else got in by a paste or a stray browser edit: flatten it.
    out.push({ kind: "text", text: el.textContent ?? "" });
  });
  return out;
}

/** A DOM (container, offset) pair as a model offset. */
function modelOffset(container: Node, offset: number): number {
  const root = rootEl.value;
  if (!root || !root.contains(container)) return model.value.length;
  const nodes = readNodes();
  // Selection anchored on the root itself addresses a child index, not text.
  if (container === root) return flatOffset(nodes, offset, 0);
  const index = Array.prototype.indexOf.call(root.childNodes, childOf(root, container));
  if (index < 0) return model.value.length;
  return flatOffset(nodes, index, offset);
}

/** Caret offset in the model, the way a textarea's selectionStart reads. */
function caret(): number {
  return selectionRange()[0];
}

/** [start, end] in the model — equal when the selection is collapsed. */
function selectionRange(): [number, number] {
  const sel = window.getSelection();
  if (!sel || sel.rangeCount === 0) return [model.value.length, model.value.length];
  const range = sel.getRangeAt(0);
  const from = modelOffset(range.startContainer, range.startOffset);
  const to = range.collapsed ? from : modelOffset(range.endContainer, range.endOffset);
  return from <= to ? [from, to] : [to, from];
}

/** The root's direct child that contains `node`. */
function childOf(root: HTMLElement, node: Node): Node {
  let cur = node;
  while (cur.parentNode && cur.parentNode !== root) cur = cur.parentNode;
  return cur;
}

function setCaret(offset: number) {
  const root = rootEl.value;
  if (!root) return;
  const pos = locateOffset(readNodes(), offset);
  const sel = window.getSelection();
  if (!sel) return;
  const range = document.createRange();
  if (pos.index < 0) {
    range.selectNodeContents(root);
    range.collapse(true);
  } else {
    const child = root.childNodes[pos.index];
    if (!child) return;
    if (child.nodeType === Node.TEXT_NODE) {
      range.setStart(child, Math.min(pos.offset, child.nodeValue?.length ?? 0));
    } else {
      // A chip: address the position before or after it on the root instead,
      // since there is no text position inside a contenteditable="false" node.
      range.setStart(root, pos.offset === 0 ? pos.index : pos.index + 1);
    }
    range.collapse(true);
  }
  sel.removeAllRanges();
  sel.addRange(range);
}

/** Rebuild the children from the model, keeping the caret where it was. */
function render(keepCaret = true) {
  const root = rootEl.value;
  if (!root) return;
  const offset = keepCaret && root.contains(window.getSelection()?.anchorNode ?? null) ? caret() : -1;
  const label = new Map(props.skills.map((s) => [s.name, s.label]));

  root.replaceChildren();
  for (const node of tokenize(model.value, known())) {
    if (!isChip(node)) {
      if (node.text) root.appendChild(document.createTextNode(node.text));
      continue;
    }
    const chip = document.createElement("span");
    chip.className = "composer-inline-chip";
    chip.contentEditable = "false";
    chip.dataset.chip = node.kind;
    let text: string;
    let icon: string | null;
    if (node.kind === "paste") {
      chip.dataset.pasteText = node.text;
      chip.title = "Click to view full text";
      chip.addEventListener("click", () => { pasteDialogText.value = node.text; });
      text = `Pasted text (${node.text.split("\n").length} lines)`;
      icon = chipIconSvg("paste");
    } else {
      chip.dataset.skill = node.name;
      // A command's name IS `/compact` — dropping the slash would make it read
      // like prose. A skill gets its display name, a file its basename.
      text = node.kind === "command" ? `/${node.name}`
        : node.kind === "file" ? node.name.slice(node.name.lastIndexOf("/") + 1)
        : (label.get(node.name) ?? node.name);
      icon = chipIconSvg(node.kind);
    }
    if (icon) chip.insertAdjacentHTML("afterbegin", icon);
    chip.appendChild(document.createTextNode(text));
    root.appendChild(chip);
  }
  // A text node ending in "\n" renders no visible last line, so the caret on a
  // freshly opened line would sit nowhere. The browser's own filler <br> is what
  // readNodes already ignores in last position.
  if (model.value.endsWith("\n")) root.appendChild(document.createElement("br"));
  if (offset >= 0) setCaret(offset);
}

// ── Undo, ours rather than the browser's ─────────────────────────────────────
// `render()` replaces the children outright, which the native undo stack knows
// nothing about: after inserting a skill, ⌘Z would rewind the *typing* it never
// saw superseded and leave the chip on screen — the caret jumped in front of it
// and nothing was removed. Mixing programmatic DOM surgery with contenteditable's
// own history does not work, so the history is a list of model snapshots and
// ⌘Z is handled here. That also makes undo behave the same for typing, Enter,
// paste and chips, which the native stack did not.
interface Snapshot { text: string; caret: number }
const history: Snapshot[] = [{ text: "", caret: 0 }];
let hIndex = 0;
let lastEditAt = 0;
// A run of plain typing collapses into one entry; a pause or a chip change
// starts a new one, so ⌘Z undoes a word-ish worth of work, not one letter.
const COALESCE_MS = 500;
const HISTORY_MAX = 100;
let applyingHistory = false;

function pushHistory(text: string, caretAt: number, coalesce: boolean) {
  if (history[hIndex]?.text === text) return;
  history.length = hIndex + 1; // a fresh edit drops whatever redo was ahead
  if (coalesce && hIndex > 0 && Date.now() - lastEditAt < COALESCE_MS) {
    history[hIndex] = { text, caret: caretAt };
  } else {
    history.push({ text, caret: caretAt });
    hIndex = history.length - 1;
    if (history.length > HISTORY_MAX) {
      history.shift();
      hIndex--;
    }
  }
  lastEditAt = Date.now();
}

function applySnapshot(snap: Snapshot) {
  applyingHistory = true;
  model.value = snap.text;
  // The model watcher rebuilds the DOM on the pre-flush tick, so the caret can
  // only be placed after that has happened.
  nextTick(() => {
    render(false);
    setCaret(snap.caret);
    applyingHistory = false;
  });
}

/** Replace the current selection with `text`, in the model. */
function insertText(text: string) {
  const [from, to] = selectionRange();
  const next = model.value.slice(0, from) + text + model.value.slice(to);
  const at = from + text.length;
  pushHistory(next, at, false);
  applySnapshot({ text: next, caret: at });
}

function undo(): boolean {
  if (hIndex <= 0) return false;
  hIndex--;
  applySnapshot(history[hIndex]);
  return true;
}

function redo(): boolean {
  if (hIndex >= history.length - 1) return false;
  hIndex++;
  applySnapshot(history[hIndex]);
  return true;
}

function onInput(e: Event) {
  const nodes = readNodes();
  model.value = serializeNodes(nodes);
  emit("input", e);
  if (composing.value || applyingHistory) return;
  // Only rebuild when the CHIPS changed — the user finished typing a known
  // name, or edited one until it stopped being one. Re-rendering on every
  // keystroke would fight the caret for no gain.
  const wanted = chipSignature(tokenize(model.value, known()));
  const chipsChanged = wanted !== chipSignature(nodes);
  if (chipsChanged) render();
  // A chip appearing or vanishing is its own undo step: it is the edit the
  // user will expect ⌘Z to take back first.
  pushHistory(model.value, caret(), !chipsChanged);
}

function onCompositionEnd(e: CompositionEvent) {
  composing.value = false;
  onInput(e);
}

function onKeydown(e: KeyboardEvent) {
  emit("keydown", e);
  if (e.defaultPrevented) return; // the host sent the message, or ran completion
  if ((e.metaKey || e.ctrlKey) && (e.key === "z" || e.key === "Z")) {
    e.preventDefault();
    // ⌘⇧Z is redo on macOS; ⌘Y is the Windows habit.
    if (e.shiftKey ? redo() : undo()) return;
    return;
  }
  if ((e.metaKey || e.ctrlKey) && e.key === "y") {
    e.preventDefault();
    redo();
    return;
  }
  if (e.key === "Enter") {
    // Newlines live in the model as "\n" (the element is white-space:pre-wrap),
    // never as <div>/<br> blocks. execCommand("insertText", "\n") used to do
    // this, but the browser decides for itself whether that becomes a "\n", a
    // <br> or a block split — and a <br> the editor then read back as nothing
    // is how every line break the user typed vanished from the sent message.
    // Inserting into the model and re-rendering leaves nothing to guess at.
    e.preventDefault();
    insertText("\n");
  }
}

function onPaste(e: ClipboardEvent) {
  emit("paste", e);
  if (e.defaultPrevented) return; // the host claimed it (an image)
  e.preventDefault();
  const plain = e.clipboardData?.getData("text/plain");
  if (!plain) return;
  const collapse = plain.split("\n").length > PASTE_COLLAPSE_LINES;
  insertText(collapse ? wrapPaste(plain) : plain);
}

const pasteDialogText = ref<string | null>(null);

function closePasteDialog() {
  pasteDialogText.value = null;
}

function focus() {
  rootEl.value?.focus();
}

// An external write — the host clearing the box after a send, ArrowUp recalling
// the last message, a restored draft. Skip when the model already matches what
// is on screen, or every keystroke would rebuild the DOM under the caret.
watch(model, (next) => {
  if (composing.value || applyingHistory) return;
  if (next === serializeNodes(readNodes())) return;
  render(false);
  if (document.activeElement === rootEl.value) setCaret(next.length);
  // An external set is a new baseline, not something to undo back through: a
  // ⌘Z after a send must not resurrect the message that was just sent.
  history.length = 0;
  history.push({ text: next, caret: next.length });
  hIndex = 0;
});

// The skill list arrives asynchronously (list_skills), so a draft restored from
// localStorage holding `/burrow` has to get its chip once the names are known.
watch(() => [props.skills, props.commands], () => render(), { deep: true });

function onDialogEscape(e: KeyboardEvent) {
  if (e.key === "Escape" && pasteDialogText.value !== null) closePasteDialog();
}

onMounted(() => {
  render(false);
  history[0] = { text: model.value, caret: model.value.length };
  if (props.autofocus) nextTick(focus);
  window.addEventListener("keydown", onDialogEscape);
});
onBeforeUnmount(() => window.removeEventListener("keydown", onDialogEscape));

defineExpose({ focus, caret, setCaret, element: rootEl });
</script>

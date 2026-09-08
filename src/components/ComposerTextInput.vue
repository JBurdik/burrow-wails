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
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, useTemplateRef, watch } from "vue";
import {
  chipSignature, flatOffset, isChip, locateOffset, serializeNodes, tokenize,
  type FlatNode,
} from "@/lib/composerDom";

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
}>(), { autofocus: false, placeholder: "", skills: () => [], commands: () => [] });

const emit = defineEmits<{
  input: [event: Event];
  keydown: [event: KeyboardEvent];
  paste: [event: ClipboardEvent];
}>();

const rootEl = useTemplateRef<HTMLDivElement>("rootEl");
// An IME candidate window is open: the DOM must not be rebuilt under it.
const composing = ref(false);

const known = () => ({
  skills: props.skills.map((s) => s.name),
  commands: props.commands.map((c) => c.name),
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
    const name = el.dataset?.skill;
    if (name) { out.push({ kind: el.dataset.chip === "command" ? "command" : "skill", name }); return; }
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

/** Caret offset in the model, the way a textarea's selectionStart reads. */
function caret(): number {
  const root = rootEl.value;
  const sel = window.getSelection();
  if (!root || !sel || sel.rangeCount === 0) return model.value.length;
  const range = sel.getRangeAt(0);
  if (!root.contains(range.startContainer)) return model.value.length;
  const nodes = readNodes();
  // Selection anchored on the root itself addresses a child index, not text.
  if (range.startContainer === root) return flatOffset(nodes, range.startOffset, 0);
  const index = Array.prototype.indexOf.call(root.childNodes, childOf(root, range.startContainer));
  if (index < 0) return model.value.length;
  return flatOffset(nodes, index, range.startOffset);
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
    chip.dataset.skill = node.name;
    chip.dataset.chip = node.kind;
    // A command's name IS `/compact` — dropping the slash would make it read
    // like prose. A skill gets its display name and an icon instead.
    chip.textContent = node.kind === "command" ? `/${node.name}` : (label.get(node.name) ?? node.name);
    root.appendChild(chip);
  }
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
    // never as <div>/<br> blocks — those would have to be flattened back out
    // on every read. ponytail: execCommand is deprecated but it is the only
    // insertion path that keeps the caret and selection handling native.
    e.preventDefault();
    document.execCommand("insertText", false, "\n");
  }
}

function onPaste(e: ClipboardEvent) {
  emit("paste", e);
  if (e.defaultPrevented) return; // the host claimed it (an image)
  e.preventDefault();
  const plain = e.clipboardData?.getData("text/plain");
  if (plain) document.execCommand("insertText", false, plain);
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

onMounted(() => {
  render(false);
  history[0] = { text: model.value, caret: model.value.length };
  if (props.autofocus) nextTick(focus);
});

defineExpose({ focus, caret, setCaret, element: rootEl });
</script>

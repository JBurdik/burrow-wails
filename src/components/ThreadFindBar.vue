<template>
  <div v-if="open" class="find-bar absolute right-4 top-2 z-20 flex items-center gap-1 rounded-md border border-border p-1 shadow-lg">
    <input
      ref="inputEl"
      v-model="query"
      class="w-44 bg-transparent px-1.5 text-[12.5px] text-foreground outline-none placeholder:text-muted-foreground"
      placeholder="Find in chat"
      aria-label="Find in chat"
      spellcheck="false"
      @keydown.enter.prevent="step($event.shiftKey ? -1 : 1)"
      @keydown.esc.stop.prevent="close"
    />
    <span class="min-w-[3.5em] text-right text-[11px] tabular-nums text-muted-foreground">{{ query ? formatFindCount(index, matches.length) : "" }}</span>
    <button class="find-btn" title="Previous (⇧↵)" aria-label="Previous match" :disabled="!matches.length" @click="step(-1)"><PhArrowUp :size="13" /></button>
    <button class="find-btn" title="Next (↵)" aria-label="Next match" :disabled="!matches.length" @click="step(1)"><PhArrowDown :size="13" /></button>
    <button class="find-btn" title="Close (Esc)" aria-label="Close find" @click="close"><PhX :size="13" /></button>
  </div>
</template>

<script lang="ts">
// Shared by every chat's bar. ponytail: one bar open app-wide, since the CSS
// highlight registry is global.
let closeOpenBar: (() => void) | null = null;
let lastPointerChat: Element | null = null;
</script>

<script setup lang="ts">
// ⌘F inside one chat transcript. Matching runs on the loaded ChatMessage[]
// (lib/threadFind.ts) so a match behind a fold still counts and can be
// unfolded (`reveal`); the highlight is painted over the rendered DOM with the
// CSS Custom Highlight API — Range objects only, no <mark> spliced into the
// markdown renderer's v-html, so nothing it re-renders fights the highlight.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { PhArrowDown, PhArrowUp, PhX } from "@phosphor-icons/vue";
import type { ChatMessage } from "@/lib/chatTypes";
import { findMatches, formatFindCount, occurrences, stepFindIndex } from "@/lib/threadFind";

const props = defineProps<{ messages: readonly ChatMessage[]; container: HTMLElement | null }>();
const emit = defineEmits<{ reveal: [id: number] }>();

const open = ref(false);
const query = ref("");
const index = ref(0);
const inputEl = ref<HTMLInputElement | null>(null);
const matches = computed(() => findMatches(props.messages, query.value));

const supported = typeof CSS !== "undefined" && "highlights" in CSS && typeof Highlight !== "undefined";

function chatRoot(): Element | null {
  return props.container?.closest(".claude-chat") ?? null;
}

/** ⌘F belongs to this chat: focus is in it, or nowhere editable and it's the visible chat last clicked. */
function ownsFind(): boolean {
  const root = chatRoot();
  if (!root) return false;
  const ae = document.activeElement;
  if (ae?.closest(".xterm, .cm-editor")) return false;
  const chat = ae?.closest(".claude-chat");
  if (chat) return chat === root;
  if (ae?.matches("input, textarea, select, [contenteditable]")) return false;
  const visible = [...document.querySelectorAll(".claude-chat")].filter((el) => el.getClientRects().length);
  return (lastPointerChat && visible.includes(lastPointerChat) ? lastPointerChat : visible[0]) === root;
}

function onKeydown(e: KeyboardEvent) {
  if (!(e.metaKey || e.ctrlKey) || e.altKey) return;
  const k = e.key.toLowerCase();
  if (k === "f" && !e.shiftKey && ownsFind()) {
    e.preventDefault();
    openBar();
  } else if (k === "g" && open.value && ownsFind()) {
    e.preventDefault();
    step(e.shiftKey ? -1 : 1);
  }
}
function onPointerDown(e: PointerEvent) {
  lastPointerChat = (e.target as Element | null)?.closest?.(".claude-chat") ?? lastPointerChat;
}

async function openBar() {
  if (closeOpenBar && closeOpenBar !== close) closeOpenBar();
  closeOpenBar = close;
  open.value = true;
  const sel = window.getSelection()?.toString().trim();
  if (sel && !sel.includes("\n") && props.container?.contains(window.getSelection()!.anchorNode)) query.value = sel;
  await nextTick();
  inputEl.value?.focus();
  inputEl.value?.select();
  observe();
  void navigate();
}

function close() {
  open.value = false;
  if (closeOpenBar === close) closeOpenBar = null;
  observer?.disconnect();
  observer = null;
  if (supported) {
    CSS.highlights.delete("thread-find");
    CSS.highlights.delete("thread-find-active");
  }
}

function step(delta: number) {
  if (!matches.value.length) return;
  index.value = stepFindIndex(index.value, matches.value.length, delta);
  void navigate();
}

watch(query, () => {
  index.value = 0;
  void navigate();
});
// The transcript grew or shrank under an open bar: keep the counter in range.
watch(() => matches.value.length, (n) => { if (index.value >= n) index.value = Math.max(0, n - 1); });

/** Unfold the current match's message, then highlight and scroll to it. */
async function navigate() {
  const m = matches.value[index.value];
  if (m) emit("reveal", m.id);
  await nextTick();
  paint(true);
}

// Text-node ranges per rendered message row, in DOM order. ponytail: a match
// split across two text nodes (`foo<b>bar</b>`) isn't painted; it still counts
// and scrolls to its message.
function domRanges(): Map<number, Range[]> {
  const out = new Map<number, Range[]>();
  const q = query.value;
  if (!props.container || !q) return out;
  for (const row of props.container.querySelectorAll<HTMLElement>("[data-message-id]")) {
    const id = Number(row.dataset.messageId);
    if (!Number.isFinite(id)) continue;
    const ranges: Range[] = [];
    const walker = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      for (const at of occurrences(node.nodeValue ?? "", q)) {
        const r = document.createRange();
        r.setStart(node, at);
        r.setEnd(node, at + q.length);
        ranges.push(r);
      }
    }
    if (ranges.length) out.set(id, ranges);
  }
  return out;
}

function paint(scroll: boolean) {
  if (!open.value) return;
  const byId = domRanges();
  const m = matches.value[index.value];
  // Rendered markdown can drop or add text vs the source, so the nth source
  // occurrence maps onto the nth painted one, clamped to what's there.
  const row = m ? byId.get(m.id) : undefined;
  const active = row?.[Math.min(m!.occurrence, row.length - 1)] ?? null;
  if (supported) {
    const all = new Highlight();
    for (const rs of byId.values()) for (const r of rs) if (r !== active) all.add(r);
    CSS.highlights.set("thread-find", all);
    CSS.highlights.set("thread-find-active", active ? new Highlight(active) : new Highlight());
  }
  if (!scroll || !m || !props.container) return;
  const target = active?.getBoundingClientRect() ?? props.container.querySelector(`[data-message-id="${m.id}"]`)?.getBoundingClientRect();
  if (!target) return;
  const box = props.container.getBoundingClientRect();
  if (target.top < box.top + 40 || target.bottom > box.bottom - 40) {
    props.container.scrollTo({ top: props.container.scrollTop + target.top - box.top - box.height / 2, behavior: "smooth" });
  }
}

// Streaming text, a fold toggled by hand, "Load earlier": repaint (never
// scroll) at most once a frame while the bar is open.
let observer: MutationObserver | null = null;
let frame = 0;
function observe() {
  if (observer || !props.container) return;
  observer = new MutationObserver(() => {
    if (frame) return;
    frame = requestAnimationFrame(() => { frame = 0; paint(false); });
  });
  observer.observe(props.container, { subtree: true, childList: true, characterData: true });
}

onMounted(() => {
  window.addEventListener("keydown", onKeydown);
  window.addEventListener("pointerdown", onPointerDown, true);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("pointerdown", onPointerDown, true);
  if (frame) cancelAnimationFrame(frame);
  if (open.value) close();
});
</script>

<style scoped>
.find-bar { background: var(--surface-elevated); }
.find-btn { display: flex; padding: 4px; border: none; border-radius: 5px; background: transparent; color: var(--chat-text-secondary); cursor: pointer; }
.find-btn:hover:not(:disabled) { color: var(--chat-text); background: color-mix(in srgb, var(--chat-text) 8%, transparent); }
.find-btn:disabled { opacity: 0.4; cursor: default; }
</style>

<style>
::highlight(thread-find) { background-color: color-mix(in srgb, var(--chat-accent) 28%, transparent); }
::highlight(thread-find-active) { background-color: color-mix(in srgb, var(--chat-accent) 60%, transparent); color: var(--chat-text); }
</style>

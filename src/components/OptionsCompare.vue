<template>
  <!-- Compare the looks a ```options block offers (src/lib/optionsBlock.ts).
       Hosted by WindowApp (kind "options") or, on the phone, by AgentChat as a
       full-screen overlay. ←/→ every shot of every option, ↑/↓ option to
       option, past the last shot all of them side by side. -->
  <div class="oc flex h-full min-h-0 flex-col bg-[var(--terminal-bg)] text-foreground md:flex-row">
    <section class="flex min-h-0 min-w-0 flex-1 flex-col">
      <div
        ref="stageEl"
        class="oc-stage relative min-h-0 flex-1 overflow-hidden"
        :class="scale > 1 && 'cursor-grab'"
        @wheel="onWheel"
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="drag = null"
        @pointercancel="drag = null"
        @dblclick="resetZoom"
      >
        <div v-if="showAll && !holding" class="grid h-full gap-3 p-3" :style="{ gridTemplateColumns: `repeat(${allCols}, minmax(0, 1fr))` }">
          <button
            v-for="(o, i) in spec.options"
            :key="i"
            type="button"
            class="flex min-h-0 flex-col gap-1.5 rounded-[var(--radius-nav)] border p-1.5 text-left hover:bg-hover"
            :class="picked.includes(i) ? 'border-[var(--accent)]' : 'border-border'"
            :title="`Open ${letter(i)}`"
            @click="goOption(i)"
          >
            <OptionMedia class="min-h-0 flex-1" :path="o.media[0]" static />
            <span class="truncate text-[12px]"><b>{{ letter(i) }}</b> {{ o.label }}</span>
          </button>
        </div>
        <div v-else class="absolute inset-0 origin-top-left" :style="{ transform: `translate(${tx}px, ${ty}px) scale(${scale})` }">
          <OptionMedia class="h-full w-full" :path="shownPath" />
        </div>
        <span v-if="holding" class="absolute left-3 top-3 rounded-[6px] bg-black/70 px-2 py-0.5 text-[11px] font-semibold text-white">Current</span>
        <span v-if="scale > 1" class="absolute right-3 top-3 rounded-[6px] bg-black/70 px-2 py-0.5 text-[11px] text-white">{{ Math.round(scale * 100) }}% · ⌘0 to reset</span>
      </div>

      <div class="flex shrink-0 items-center gap-2 border-t border-border px-3 py-2 text-[12px]">
        <button type="button" class="oc-btn" :disabled="pos === 0" title="Previous (←)" @click="step(-1)"><PhCaretLeft :size="13" /></button>
        <span class="min-w-0 flex-1 truncate">
          <template v-if="showAll">All options side by side</template>
          <template v-else>
            <b>{{ letter(cur.oi) }}</b> {{ spec.options[cur.oi].label }}
            <span v-if="spec.options[cur.oi].media.length > 1" class="text-muted-foreground"> · {{ cur.mi + 1 }}/{{ spec.options[cur.oi].media.length }}</span>
            <span v-if="spec.options[cur.oi].note" class="text-muted-foreground"> · {{ spec.options[cur.oi].note }}</span>
          </template>
        </span>
        <button
          v-if="spec.current"
          type="button"
          class="oc-btn"
          :class="holding && 'oc-on'"
          title="Hold (or hold Space) to see the current version"
          @pointerdown.prevent="holding = true"
          @pointerup="holding = false"
          @pointerleave="holding = false"
        >Current</button>
        <button type="button" class="oc-btn" :disabled="showAll" :title="pos === flat.length - 1 ? 'See them all (→)' : 'Next (→)'" @click="step(1)"><PhCaretRight :size="13" /></button>
      </div>
      <div class="flex shrink-0 gap-1 overflow-x-auto px-3 pb-2">
        <button
          v-for="(it, i) in flat"
          :key="i"
          type="button"
          class="h-1.5 shrink-0 rounded-[2px]"
          :class="[i === pos ? 'bg-[var(--accent)]' : 'bg-border', it.mi === 0 && i > 0 ? 'ml-2' : '']"
          :style="{ width: '18px' }"
          :title="`${letter(it.oi)} ${it.mi + 1}`"
          :aria-label="`${letter(it.oi)}, shot ${it.mi + 1}`"
          @click="go(i)"
        />
        <button type="button" class="ml-2 h-1.5 w-[18px] shrink-0 rounded-[2px]" :class="showAll ? 'bg-[var(--accent)]' : 'bg-border'" title="All" aria-label="All side by side" @click="go(flat.length)" />
      </div>
    </section>

    <aside class="flex max-h-[45%] shrink-0 flex-col gap-2 overflow-y-auto border-t border-border p-3 md:max-h-none md:w-[280px] md:border-l md:border-t-0">
      <div v-if="spec.question" class="text-[13px] font-semibold">{{ spec.question }}</div>
      <label v-for="(o, i) in spec.options" :key="i" class="oc-pick" :class="picked.includes(i) && 'oc-on'">
        <input type="checkbox" :checked="picked.includes(i)" :disabled="answered" @change="toggle(i)">
        <span class="min-w-0"><b>{{ letter(i) }}</b> {{ o.label }}<span v-if="o.note" class="block text-[11px] text-muted-foreground">{{ o.note }}</span></span>
      </label>
      <label class="oc-pick" :class="none && 'oc-on'">
        <input type="checkbox" :checked="none" :disabled="answered" @change="setNone">
        <span>None of them</span>
      </label>
      <textarea v-model="note" rows="3" :disabled="answered" class="w-full resize-y rounded-[7px] border border-border bg-panel px-2 py-1.5 text-[12px] focus:border-[var(--accent)] focus:outline-none" placeholder="Note (optional)" />
      <span v-if="answered" class="text-[11px] font-semibold text-[var(--green)]">Answered</span>
      <button v-else type="button" class="oc-send" :disabled="!canSend" title="Send (⌘↵)" @click="send">{{ sendLabel }}</button>
    </aside>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, onMounted, reactive, ref, watch, watchEffect } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhCaretLeft, PhCaretRight } from "@phosphor-icons/vue";
import { pageSrcdoc } from "@/lib/chatBlocks";
import { inlineLocalAssets } from "@/lib/htmlInline";
import { formatOptionsReply, mediaKind, mediaMime, optionLetter as letter, type OptionsSpec } from "@/lib/optionsBlock";

const props = defineProps<{ spec: OptionsSpec; cwd: string; answered?: boolean; start?: number }>();
const emit = defineEmits<{ send: [text: string]; close: [] }>();

// ── media: read through the same commands the RP / chat use for images and pages
type Loaded = { kind: "image" | "video" | "html"; src?: string; html?: string; error?: string };
const media = reactive(new Map<string, Loaded | null>());
const abs = (p: string) => (p.startsWith("/") ? p : `${props.cwd.replace(/\/+$/, "")}/${p}`);
async function load(path: string) {
  if (media.has(path)) return;
  media.set(path, null);
  const kind = mediaKind(path)!;
  const file = abs(path);
  try {
    if (kind === "html") {
      const html = await inlineLocalAssets(
        await invoke<string>("read_text_file", { path: file }), file,
        (p) => invoke<string>("read_text_file", { path: p }),
        (p) => invoke<string>("read_file_base64", { path: p }),
      );
      media.set(path, { kind, html: pageSrcdoc(html) });
      return;
    }
    const b64 = await invoke<string>("read_file_base64", { path: file });
    // ponytail: a video goes through base64 whole; fine for clips, a streamed
    // file URL if agents start sending long recordings.
    const src = kind === "video"
      ? URL.createObjectURL(new Blob([Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))], { type: mediaMime(path) }))
      : `data:${mediaMime(path)};base64,${b64}`;
    media.set(path, { kind, src });
  } catch (e) {
    media.set(path, { kind, error: `Could not read ${path}: ${e}` });
  }
}
onBeforeUnmount(() => media.forEach((m) => m?.kind === "video" && m.src && URL.revokeObjectURL(m.src)));

// `static`: a grid tile — a page can't take the pointer there (the tile is the button).
const OptionMedia = defineComponent({
  props: { path: { type: String, required: true }, static: Boolean },
  setup(p) {
    watchEffect(() => void load(p.path));
    return () => {
      const m = media.get(p.path);
      const fit = "h-full w-full object-contain";
      if (!m) return h("div", { class: "flex items-center justify-center text-[12px] text-muted-foreground" }, "Loading…");
      if (m.error) return h("div", { class: "flex items-center justify-center p-4 text-center text-[12px] text-[var(--red)]" }, m.error);
      if (m.kind === "image") return h("img", { class: fit, src: m.src, alt: p.path, draggable: false });
      if (m.kind === "video") return h("video", { class: fit, src: m.src, autoplay: true, loop: true, muted: true, playsinline: true });
      // Agent HTML: scripts in an opaque origin under pageSrcdoc's no-network CSP.
      return h("iframe", { class: `bg-white ${p.static ? "pointer-events-none" : ""}`, sandbox: "allow-scripts", referrerpolicy: "no-referrer", srcdoc: m.html, title: p.path });
    };
  },
});

// ── position: every shot of every option, then "all"
const flat = computed(() => props.spec.options.flatMap((o, oi) => o.media.map((path, mi) => ({ oi, mi, path }))));
const pos = ref(0);
const showAll = computed(() => pos.value >= flat.value.length);
const cur = computed(() => flat.value[Math.min(pos.value, flat.value.length - 1)]);
const allCols = computed(() => Math.min(props.spec.options.length, props.spec.options.length > 4 ? 3 : 4));
const holding = ref(false);
const shownPath = computed(() => {
  const current = props.spec.current;
  if (holding.value && current?.length) return current[Math.min(showAll.value ? 0 : cur.value.mi, current.length - 1)];
  return cur.value.path;
});

function go(i: number) { pos.value = Math.max(0, Math.min(i, flat.value.length)); }
function step(d: number) { go(pos.value + d); }
function goOption(oi: number) { go(flat.value.findIndex((it) => it.oi === oi)); }
if (props.start) goOption(Math.min(props.start, props.spec.options.length - 1));
function jumpOption(d: number) {
  if (showAll.value) { if (d < 0) goOption(props.spec.options.length - 1); return; }
  const target = cur.value.oi + d;
  if (target >= props.spec.options.length) go(flat.value.length);
  else goOption(Math.max(0, target));
}

// ── zoom at the pointer, drag to pan (not on a page: the iframe takes the pointer)
const stageEl = ref<HTMLElement | null>(null);
const scale = ref(1), tx = ref(0), ty = ref(0);
let drag: { x: number; y: number; tx: number; ty: number } | null = null;
function resetZoom() { scale.value = 1; tx.value = 0; ty.value = 0; }
watch([pos, holding], resetZoom);
function onWheel(e: WheelEvent) {
  if (showAll.value && !holding.value) return;
  e.preventDefault();
  const r = stageEl.value!.getBoundingClientRect();
  const px = e.clientX - r.left, py = e.clientY - r.top;
  const next = Math.min(8, Math.max(1, scale.value * Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.002))));
  const k = next / scale.value;
  scale.value = next;
  if (next === 1) { tx.value = 0; ty.value = 0; return; }
  tx.value = px - (px - tx.value) * k;
  ty.value = py - (py - ty.value) * k;
}
function onPointerDown(e: PointerEvent) {
  if (scale.value <= 1 || e.button !== 0) return;
  (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  drag = { x: e.clientX, y: e.clientY, tx: tx.value, ty: ty.value };
}
function onPointerMove(e: PointerEvent) {
  if (!drag) return;
  tx.value = drag.tx + e.clientX - drag.x;
  ty.value = drag.ty + e.clientY - drag.y;
}

// ── picks + send
const picked = ref<number[]>([]);
const none = ref(false);
const note = ref("");
function toggle(i: number) {
  none.value = false;
  picked.value = picked.value.includes(i) ? picked.value.filter((x) => x !== i) : [...picked.value, i];
}
function setNone() { none.value = !none.value; if (none.value) picked.value = []; }
const canSend = computed(() => !props.answered && (picked.value.length > 0 || none.value));
const sendLabel = computed(() => (none.value ? "Send: none of them" : picked.value.length ? `Send ${[...picked.value].sort((a, b) => a - b).map(letter).join(" + ")}` : "Pick one or None"));
function send() {
  if (canSend.value) emit("send", formatOptionsReply(props.spec, none.value ? [] : picked.value, note.value));
}

// ── keys
function onKeydown(e: KeyboardEvent) {
  if (e.metaKey && e.key === "Enter") { e.preventDefault(); send(); return; }
  const typing = (e.target as HTMLElement | null)?.closest?.("textarea, input[type=text]");
  if (typing) { if (e.key === "Escape") (typing as HTMLElement).blur(); return; }
  if (e.metaKey && e.key === "0") { e.preventDefault(); resetZoom(); return; }
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  const act: Record<string, () => void> = {
    ArrowRight: () => step(1), ArrowLeft: () => step(-1),
    ArrowDown: () => jumpOption(1), ArrowUp: () => jumpOption(-1),
    Escape: () => (scale.value > 1 ? resetZoom() : emit("close")),
    " ": () => { if (props.spec.current) holding.value = true; },
  };
  if (!act[e.key]) return;
  e.preventDefault();
  if (!e.repeat) act[e.key]();
}
function onKeyup(e: KeyboardEvent) { if (e.key === " ") holding.value = false; }
const onBlur = () => (holding.value = false);
onMounted(() => {
  window.addEventListener("keydown", onKeydown);
  window.addEventListener("keyup", onKeyup);
  window.addEventListener("blur", onBlur);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("keyup", onKeyup);
  window.removeEventListener("blur", onBlur);
});
</script>

<style scoped>
.oc-stage { touch-action: none; }
.oc-btn { display: inline-flex; align-items: center; gap: 4px; padding: 4px 9px; border-radius: 7px; border: 1px solid var(--border); background: transparent; color: var(--text-muted); font-size: 12px; user-select: none; }
.oc-btn:hover:not(:disabled) { color: var(--text-primary); background: var(--bg-hover); }
.oc-btn:disabled { opacity: 0.4; }
.oc-pick { display: flex; align-items: flex-start; gap: 8px; padding: 6px 8px; border-radius: 7px; border: 1px solid var(--border); font-size: 12.5px; cursor: pointer; }
.oc-pick input { margin-top: 2px; accent-color: var(--accent); }
.oc-on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); color: var(--text-primary); }
.oc-send { padding: 6px 12px; border-radius: 7px; border: 1px solid var(--accent); background: var(--accent); color: #fff; font-size: 12.5px; font-weight: 600; }
.oc-send:disabled { opacity: 0.45; }
</style>

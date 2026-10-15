<script setup lang="ts">
import { computed, inject, onBeforeUnmount, ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { PhArrowClockwise, PhPencilSimple, PhWarningCircle } from "@phosphor-icons/vue";
import { dirOf } from "@/lib/localLink";

const props = defineProps<{ path: string; cwd: string }>();
const activeTerm = inject<() => { openFileInTab(path: string, name: string, line?: number, pin?: boolean): void } | undefined>("activeTerm");

const raw = ref<string | null>(null);
const error = ref("");
const scroller = ref<HTMLElement | null>(null);

const relPath = computed(() => {
  const root = props.cwd.replace(/\/$/, "");
  return props.path.startsWith(`${root}/`) ? props.path.slice(root.length + 1) : props.path;
});
const crumbs = computed(() => relPath.value.split("/").filter(Boolean));

function slug(text: string): string {
  return text.toLowerCase().trim().replace(/[^\p{L}\p{N}\s-]/gu, "").replace(/\s+/g, "-");
}

// Headings get GitHub-style ids so both the outline and `[x](#goal)` links land.
const rendered = computed(() => {
  if (raw.value == null) return { html: "", outline: [] as { id: string; text: string; level: number }[] };
  const doc = new DOMParser().parseFromString(
    DOMPurify.sanitize(marked.parse(raw.value, { gfm: true }) as string),
    "text/html",
  );
  const seen = new Map<string, number>();
  const outline = [...doc.body.querySelectorAll("h1, h2, h3")].map((h) => {
    const base = slug(h.textContent ?? "") || "section";
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    h.id = n ? `${base}-${n}` : base;
    return { id: h.id, text: h.textContent ?? "", level: Number(h.tagName[1]) };
  });
  return { html: doc.body.innerHTML, outline };
});

async function load() {
  try {
    const text = await invoke<string>("read_text_file_checked", { path: props.path });
    if (text !== raw.value) raw.value = text;
    error.value = "";
  } catch (e) {
    error.value = String(e);
  }
}

// ponytail: 2 s poll so a plan the agent is still writing updates live; fs watch if it ever matters.
let timer: ReturnType<typeof setInterval> | undefined;
watch(() => props.path, () => {
  raw.value = null;
  scroller.value?.scrollTo({ top: 0 });
  void load();
  clearInterval(timer);
  timer = setInterval(load, 2000);
}, { immediate: true });
onBeforeUnmount(() => clearInterval(timer));

function jump(id: string) {
  scroller.value?.querySelector(`#${CSS.escape(id)}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
}

// In-page anchors would otherwise change the hash route. File links are
// handled app-wide (App.vue) against `data-link-base`.
function onClick(e: MouseEvent) {
  const href = (e.target as Element).closest?.("a[href]")?.getAttribute("href");
  if (!href?.startsWith("#")) return;
  e.preventDefault();
  jump(decodeURIComponent(href.slice(1)));
}

function edit() {
  activeTerm?.()?.openFileInTab(props.path, crumbs.value[crumbs.value.length - 1] ?? props.path, undefined, true);
}
</script>

<template>
  <div class="doc-surface flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1.5">
      <div class="flex min-w-0 flex-1 items-center gap-1 overflow-hidden text-[11px] text-muted-foreground" :title="path">
        <template v-for="(c, i) in crumbs" :key="i">
          <span v-if="i" class="shrink-0 opacity-60">/</span>
          <span class="truncate" :class="i === crumbs.length - 1 && 'shrink-0 text-secondary-foreground'">{{ c }}</span>
        </template>
      </div>
      <button class="flex h-6 w-6 shrink-0 items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground" title="Reload" aria-label="Reload" @click="load">
        <PhArrowClockwise :size="13" />
      </button>
      <button class="flex h-6 w-6 shrink-0 items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground" title="Edit in editor" aria-label="Edit in editor" @click="edit">
        <PhPencilSimple :size="13" />
      </button>
    </div>

    <div v-if="error && raw == null" class="flex flex-1 flex-col items-center justify-center gap-2 p-5 text-center text-[11px] text-muted-foreground">
      <PhWarningCircle :size="18" class="text-destructive" />
      <span>Could not read {{ relPath }}</span>
      <span class="max-w-full break-words opacity-70">{{ error }}</span>
    </div>

    <div v-else ref="scroller" class="doc-scroll min-h-0 flex-1 overflow-y-auto">
      <div class="doc-layout">
        <nav v-if="rendered.outline.length > 1" class="doc-outline" aria-label="Outline">
          <button
            v-for="h in rendered.outline"
            :key="h.id"
            class="doc-outline-item"
            :style="{ paddingLeft: `${(h.level - 1) * 10}px` }"
            :title="h.text"
            @click="jump(h.id)"
          >{{ h.text }}</button>
        </nav>
        <article class="md-doc" :data-link-base="dirOf(path)" @click="onClick" v-html="rendered.html" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.doc-scroll { container-type: inline-size; }
.doc-layout { display: flex; gap: 24px; padding: 20px 20px 48px; }
.doc-outline { display: none; }
@container (min-width: 620px) {
  .doc-outline {
    position: sticky; top: 0; align-self: flex-start;
    display: flex; flex-direction: column; gap: 2px;
    width: 160px; flex-shrink: 0; max-height: calc(100vh - 140px); overflow-y: auto;
  }
}
.doc-outline-item {
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: left;
  font-size: 11px; line-height: 1.6; color: var(--color-muted-foreground);
}
.doc-outline-item:hover { color: var(--color-foreground); }

.md-doc { min-width: 0; flex: 1; max-width: 760px; font-size: 13px; line-height: 1.65; color: var(--color-foreground); }
.md-doc :deep(h1) { font-size: 22px; font-weight: 700; margin: 0 0 12px; line-height: 1.25; }
.md-doc :deep(h2) { font-size: 17px; font-weight: 650; margin: 28px 0 10px; }
.md-doc :deep(h3) { font-size: 14px; font-weight: 650; margin: 22px 0 8px; }
.md-doc :deep(h4) { font-size: 13px; font-weight: 650; margin: 18px 0 6px; }
.md-doc :deep(:is(h1, h2, h3)) { scroll-margin-top: 12px; }
.md-doc :deep(p) { margin: 0 0 12px; }
.md-doc :deep(:is(ul, ol)) { margin: 0 0 12px; padding-left: 22px; }
.md-doc :deep(ul) { list-style: disc; }
.md-doc :deep(ol) { list-style: decimal; }
.md-doc :deep(li) { margin: 3px 0; }
.md-doc :deep(a) { color: var(--accent); text-decoration: underline; text-underline-offset: 2px; }
.md-doc :deep(code) { font-family: var(--font-mono); font-size: 11.5px; background: var(--color-hover); padding: 1px 5px; border-radius: 4px; }
.md-doc :deep(pre) { background: var(--terminal-bg); border: 1px solid var(--border); border-radius: 8px; padding: 12px 14px; overflow-x: auto; margin: 0 0 12px; }
.md-doc :deep(pre code) { background: none; padding: 0; }
.md-doc :deep(blockquote) { border-left: 3px solid var(--border); margin: 0 0 12px; padding-left: 12px; color: var(--color-muted-foreground); }
.md-doc :deep(hr) { border: 0; border-top: 1px solid var(--border); margin: 20px 0; }
.md-doc :deep(table) { display: block; overflow-x: auto; border-collapse: collapse; margin: 0 0 14px; font-size: 12px; }
.md-doc :deep(:is(th, td)) { border: 1px solid var(--border); padding: 6px 10px; text-align: left; vertical-align: top; }
.md-doc :deep(th) { background: var(--color-hover); font-weight: 600; }
.md-doc :deep(img) { max-width: 100%; }
.md-doc :deep(input[type="checkbox"]) { margin-right: 6px; }
</style>

<template>
  <div class="font-mono text-[12px] leading-[20px]">
    <div v-if="error" class="font-sans text-[12.5px] text-destructive">{{ error }}</div>
    <div v-else-if="text == null" class="h-24" />
    <template v-else>
      <div v-if="!conflictCount" class="mb-2 flex items-center gap-2 font-sans text-[12.5px] text-secondary-foreground">
        No conflict markers left.
        <button class="cr-btn cr-btn-solid" @click="git.markResolved(path)">Mark resolved</button>
      </div>
      <template v-for="(s, i) in segs" :key="i">
        <template v-if="s.kind === 'text'">
          <template v-if="!open.has(i) && s.lines.length > CTX * 2 + 2">
            <pre class="cr-line">{{ head(s, i).join("\n") }}</pre>
            <button class="cr-fold" @click="open.add(i)">{{ s.lines.length - fold(s, i) }} unchanged lines</button>
            <pre class="cr-line">{{ tail(s, i).join("\n") }}</pre>
          </template>
          <pre v-else class="cr-line">{{ s.lines.join("\n") }}</pre>
        </template>
        <div v-else class="my-1 overflow-hidden rounded-md border border-border">
          <div class="flex flex-wrap items-center gap-1 px-2 py-1 font-sans text-[11.5px]">
            <button class="cr-act" @click="pick(idx(i), 'ours')">Accept Current</button>
            <span class="text-muted-foreground">|</span>
            <button class="cr-act" @click="pick(idx(i), 'theirs')">Accept Incoming</button>
            <span class="text-muted-foreground">|</span>
            <button class="cr-act" @click="pick(idx(i), 'both')">Accept Both</button>
            <span class="text-muted-foreground">|</span>
            <button class="cr-act" @click="pick(idx(i), 'both-reversed')">Both (incoming first)</button>
          </div>
          <div class="cr-ours">
            <div class="cr-label">{{ sideLabel("ours") }} <span class="opacity-70">{{ s.oursLabel }}</span></div>
            <pre class="cr-line">{{ s.ours.join("\n") || " " }}</pre>
          </div>
          <div v-if="s.base" class="cr-base">
            <div class="cr-label">Common ancestor</div>
            <pre class="cr-line">{{ s.base.join("\n") || " " }}</pre>
          </div>
          <div class="cr-theirs">
            <div class="cr-label">{{ sideLabel("theirs") }} <span class="opacity-70">{{ s.theirsLabel }}</span></div>
            <pre class="cr-line">{{ s.theirs.join("\n") || " " }}</pre>
          </div>
        </div>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { useGitStore } from "@/stores/git";
import { parseConflicts, resolveConflict, type ConflictSegment, type Resolution } from "@/lib/conflictMarkers";

const props = defineProps<{ cwd: string; path: string }>();
const git = useGitStore();
const CTX = 3;

const text = ref<string | null>(null);
const error = ref("");
const open = reactive(new Set<number>());
const abs = computed(() => `${props.cwd.replace(/\/$/, "")}/${props.path}`);
const segs = computed(() => (text.value == null ? [] : parseConflicts(text.value)));
const conflictCount = computed(() => segs.value.filter((s) => s.kind === "conflict").length);

async function load() {
  error.value = "";
  try { text.value = await invoke<string>("read_text_file", { path: abs.value }); }
  catch (e) { error.value = `Can't read file: ${e}`; }
}
// status refreshes after every git op; re-read so an outside edit (editor, agent) shows up
watch(() => [abs.value, git.conflicted], load, { immediate: true });

/** Conflict number for segment `i` (resolveConflict counts conflicts only). */
const idx = (i: number) => segs.value.slice(0, i).filter((s) => s.kind === "conflict").length;

async function pick(index: number, how: Resolution) {
  const next = resolveConflict(segs.value, index, how);
  try {
    await invoke("write_text_file", { path: abs.value, content: next });
    text.value = next;
    open.clear();
  } catch (e) { error.value = `Can't write file: ${e}`; }
}

// In a rebase git swaps the sides: "ours" (HEAD) is the branch being rebased onto, "theirs" is your commit.
const sideLabel = (side: "ours" | "theirs") =>
  git.opState === "rebase"
    ? side === "ours" ? "Current (upstream)" : "Incoming (your commit)"
    : side === "ours" ? "Current (yours)" : "Incoming";

// Context around conflicts: keep CTX lines next to a neighbouring conflict, fold the rest.
type Text = Extract<ConflictSegment, { kind: "text" }>;
const first = (i: number) => i === 0;
const last = (i: number) => i === segs.value.length - 1;
const head = (s: Text, i: number) => (first(i) ? [] : s.lines.slice(0, CTX));
const tail = (s: Text, i: number) => (last(i) ? [] : s.lines.slice(-CTX));
const fold = (s: Text, i: number) => head(s, i).length + tail(s, i).length;
</script>

<style scoped>
.cr-line { margin: 0; padding: 0 10px; white-space: pre-wrap; word-break: break-all; color: var(--text-primary); }
.cr-label { padding: 2px 10px; font-family: var(--font-sans, system-ui); font-size: 11px; color: var(--text-secondary); }
.cr-ours { background: color-mix(in srgb, var(--green) 14%, transparent); border-left: 3px solid var(--green); }
.cr-theirs { background: color-mix(in srgb, var(--blue) 14%, transparent); border-left: 3px solid var(--blue); }
.cr-base { background: color-mix(in srgb, var(--border) 25%, transparent); border-left: 3px solid var(--border); }
.cr-act { color: var(--text-secondary); }
.cr-act:hover { color: var(--text-primary); text-decoration: underline; }
.cr-fold {
  display: block; width: 100%; margin: 2px 0; padding: 2px 10px; text-align: left; border-radius: 6px;
  font-family: var(--font-sans, system-ui); font-size: 11.5px; color: var(--text-secondary);
  background: color-mix(in srgb, var(--border) 30%, transparent);
}
.cr-fold:hover { color: var(--text-primary); }
.cr-btn { height: 24px; border-radius: 6px; padding: 0 8px; font-size: 11.5px; border: 1px solid var(--border); }
.cr-btn-solid { background: var(--accent); color: var(--bg-base); border-color: transparent; }
</style>

<template>
  <div class="gd select-text font-mono text-[12px] leading-[20px]">
    <div v-if="!files.length" class="grid min-h-24 place-items-center px-6 text-center text-[12.5px] text-muted-foreground">
      {{ text.trim() ? "No content changes." : "Empty diff." }}
    </div>
    <div v-for="(f, fi) in files" :key="fi" class="mb-3 last:mb-0">
      <div v-if="files.length > 1 || (!path && f.path)" class="mb-1.5 flex items-center gap-2 font-sans text-[12px]">
        <span class="truncate text-foreground" :title="f.path">{{ f.path }}</span>
        <span class="tabular-nums"><span class="gd-add-fg">+{{ f.adds }}</span><span class="gd-del-fg ml-1.5">−{{ f.dels }}</span></span>
      </div>
      <div v-if="f.binary" class="px-2 py-3 font-sans text-[12.5px] text-muted-foreground">Binary file, no preview.</div>
      <div v-else-if="!f.hunks.length" class="px-2 py-3 font-sans text-[12.5px] text-muted-foreground">
        {{ f.renamed ? "Renamed only, content unchanged." : "No content changes." }}
      </div>
      <template v-for="h in f.hunks" :key="h.index">
        <div v-if="!capped || h.start < LINE_CAP" class="gd-hunk mb-3 overflow-hidden rounded-lg border">
          <div class="gd-hunk-head flex h-8 items-center gap-2 border-b pl-3 pr-1.5 font-sans">
            <span class="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground">{{ h.title }}</span>
            <template v-if="hunkActions">
              <button v-if="staged" class="gd-btn" @click="emit('unstage-hunk', h.index)"><PhMinus :size="12" /> Unstage hunk</button>
              <template v-else>
                <button class="gd-btn" @click="emit('discard-hunk', h.index)"><PhArrowCounterClockwise :size="12" /> Discard</button>
                <button class="gd-btn" @click="emit('stage-hunk', h.index)"><PhPlus :size="12" /> Stage hunk</button>
              </template>
            </template>
          </div>
          <table class="w-full border-collapse">
            <tbody>
              <tr v-for="(l, li) in h.lines" :key="li" :class="l.kind === 'add' ? 'gd-add' : l.kind === 'del' ? 'gd-del' : ''">
                <td class="gd-no">{{ l.oldNo ?? "" }}</td>
                <td class="gd-no">{{ l.newNo ?? "" }}</td>
                <td class="w-4 select-none text-center" :class="l.kind === 'add' ? 'gd-add-fg' : l.kind === 'del' ? 'gd-del-fg' : 'text-transparent'">
                  {{ l.kind === "add" ? "+" : l.kind === "del" ? "−" : " " }}
                </td>
                <td class="whitespace-pre-wrap break-all pr-4" :class="l.kind === 'meta' ? 'italic text-muted-foreground' : 'text-foreground'">{{ l.text || "​" }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </div>
    <div v-if="capped" class="flex justify-center py-3 font-sans">
      <button class="gd-btn" @click="showAll = true">Show all {{ total }} lines</button>
    </div>
  </div>
</template>

<script setup lang="ts">
// Unified diff text → GitPaw-style rendering. Hunk indices are global and in
// order of `@@` headers, so for a one-file diff they line up with
// `splitHunks(text)[i]` from @/lib/gitHunks.
import { computed, ref, watch } from "vue";
import { PhArrowCounterClockwise, PhMinus, PhPlus } from "@phosphor-icons/vue";

const props = defineProps<{
  text: string;
  path?: string;
  hunkActions?: boolean;
  /** with hunkActions: the diff is of the index (`--cached`) → offer Unstage instead of Stage/Discard */
  staged?: boolean;
}>();
const emit = defineEmits<{
  "stage-hunk": [i: number];
  "unstage-hunk": [i: number];
  "discard-hunk": [i: number];
}>();

interface Line { kind: "add" | "del" | "ctx" | "meta"; text: string; oldNo?: number; newNo?: number }
interface Hunk { index: number; title: string; lines: Line[]; /** running line count before this hunk (cap) */ start: number }
interface FileDiff { path: string; hunks: Hunk[]; binary: boolean; renamed: boolean; adds: number; dels: number }

const LINE_CAP = 4000;
const showAll = ref(false);
watch(() => props.text, () => (showAll.value = false));

const unquote = (p: string) => p.replace(/^"(.*)"$/, "$1").replace(/^[ab]\//, "");

// Port of GitPaw's lib/diff.ts parseDiff (incl. combined `diff --cc` for conflicts).
const files = computed<FileDiff[]>(() => {
  const out: FileDiff[] = [];
  let file: FileDiff | null = null;
  let hunk: Hunk | null = null;
  let inHeader = false;
  let prefix = 1;
  let oldNo = 0;
  let newNo = 0;
  let hunkIdx = 0;
  let lineCount = 0;
  const newFile = (path: string): FileDiff => {
    const f = { path, hunks: [], binary: false, renamed: false, adds: 0, dels: 0 };
    out.push(f);
    return f;
  };
  for (const line of props.text.split("\n")) {
    if (line.startsWith("diff --git ") || line.startsWith("diff --cc ") || line.startsWith("diff --combined ")) {
      const m = line.match(/^diff --git (?:"?a\/)?(.+?)"? (?:"?b\/)?(.+?)"?$/);
      file = newFile(m ? m[2] : line.replace(/^diff --\w+ /, ""));
      hunk = null;
      inHeader = true;
      continue;
    }
    if (line.startsWith("@@")) {
      file ??= newFile(props.path ?? "");
      prefix = line.match(/^@+/)![0].length - 1;
      oldNo = Number(line.match(/ -(\d+)/)?.[1] ?? 0);
      newNo = Number(line.match(/ \+(\d+)/)?.[1] ?? 0);
      hunk = { index: hunkIdx++, title: line.replace(/^@@[^@]*@@+\s?/, "") || line, lines: [], start: lineCount };
      file.hunks.push(hunk);
      inHeader = false;
      continue;
    }
    if (!file) continue;
    if (inHeader) {
      if (line.startsWith("rename from ")) file.renamed = true;
      else if (line.startsWith("rename to ")) file.path = line.slice(10);
      else if (line.startsWith("Binary files")) file.binary = true;
      else if (line.startsWith("+++ ") && line !== "+++ /dev/null") file.path = unquote(line.slice(4));
      continue;
    }
    if (!hunk || line.length === 0) continue;
    lineCount++;
    if (line[0] === "\\") { hunk.lines.push({ kind: "meta", text: line }); continue; }
    const pre = line.slice(0, prefix);
    const text = line.slice(prefix);
    if (pre.includes("+")) { hunk.lines.push({ kind: "add", text, newNo: newNo++ }); file.adds++; }
    else if (pre.includes("-")) { hunk.lines.push({ kind: "del", text, oldNo: prefix === 1 ? oldNo++ : undefined }); file.dels++; }
    else hunk.lines.push({ kind: "ctx", text, oldNo: prefix === 1 ? oldNo++ : undefined, newNo: newNo++ });
  }
  return out;
});

const total = computed(() => files.value.reduce((n, f) => n + f.hunks.reduce((m, h) => m + h.lines.length, 0), 0));
// ponytail: caps whole hunks past LINE_CAP lines; virtualize if 4k-line hunks matter
const capped = computed(() => !showAll.value && total.value > LINE_CAP);
</script>

<style scoped>
.gd-hunk { border-color: color-mix(in srgb, var(--border) 70%, transparent); background: color-mix(in srgb, var(--bg-base) 40%, transparent); }
.gd-hunk-head { border-color: color-mix(in srgb, var(--border) 60%, transparent); background: color-mix(in srgb, var(--border) 18%, transparent); }
.gd-add { background: color-mix(in srgb, var(--green) 11%, transparent); }
.gd-del { background: color-mix(in srgb, var(--red) 11%, transparent); }
.gd-add-fg { color: var(--green); }
.gd-del-fg { color: var(--red); }
.gd-no {
  width: 1%; padding: 0 8px; text-align: right; white-space: nowrap; user-select: none;
  font-size: 11px; font-variant-numeric: tabular-nums; color: var(--text-muted); opacity: 0.7;
}
.gd-btn {
  display: inline-flex; align-items: center; gap: 4px; height: 24px; flex-shrink: 0;
  border-radius: 6px; padding: 0 8px; font-size: 11.5px; color: var(--text-secondary);
  background: color-mix(in srgb, var(--border) 30%, transparent);
}
.gd-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
</style>

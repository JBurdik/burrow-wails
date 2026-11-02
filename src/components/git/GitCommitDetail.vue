<template>
  <div v-if="err" class="flex flex-1 items-center justify-center text-xs text-muted-foreground">{{ err }}</div>
  <div v-else-if="!d" class="p-5"><div class="h-4 w-2/3 animate-pulse rounded bg-hover" /></div>
  <div v-else class="flex min-h-0 flex-1 flex-col">
    <div class="shrink-0 border-b border-border px-5 pb-4 pt-4">
      <h2 class="select-text text-[15px] font-semibold leading-snug tracking-[-0.01em] text-foreground">{{ subject }}</h2>
      <p v-if="body" class="mt-2 max-h-36 select-text overflow-auto whitespace-pre-wrap text-[12.5px] leading-relaxed text-secondary-foreground">{{ body }}</p>
      <div class="mt-3 flex items-center gap-2.5">
        <span
          class="grid size-8 shrink-0 place-items-center rounded-full text-[11.5px] font-semibold"
          :style="{ background: `color-mix(in oklab, ${avatarColor} 22%, transparent)`, color: avatarColor }"
        >{{ initials }}</span>
        <div class="min-w-0 flex-1">
          <div class="truncate text-[12.5px] text-foreground">{{ d.author }} <span class="text-muted-foreground">{{ d.email }}</span></div>
          <div class="text-[11.5px] text-muted-foreground">
            {{ new Date(d.date).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }) }}
            <template v-if="d.committer !== d.author">, committed by {{ d.committer }}</template>
          </div>
        </div>
        <button
          class="flex items-center gap-1.5 rounded-md px-2 py-1 font-mono text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground"
          title="Copy hash"
          @click="copy"
        >
          {{ d.hash.slice(0, 8) }}
          <PhCheck v-if="copied" :size="11" class="text-[var(--green)]" />
          <PhCopy v-else :size="11" />
        </button>
      </div>
      <div v-if="d.parents.length > 1" class="mt-2 text-[11.5px] text-muted-foreground">
        Merge of
        <template v-for="(p, i) in d.parents" :key="p">
          <span v-if="i">, </span>
          <button class="font-mono text-secondary-foreground hover:text-accent" @click="emit('select', p)">{{ p.slice(0, 7) }}</button>
        </template>. Diff is against the first parent.
      </div>
    </div>

    <div class="max-h-[32%] shrink-0 overflow-y-auto border-b border-border p-1.5">
      <button
        v-for="(f, i) in files"
        :key="i"
        class="flex h-[26px] w-full items-center gap-2 rounded-md px-2 text-left text-[12.5px]"
        :class="i === fileIdx ? 'bg-hover' : 'hover:bg-hover/60'"
        @click="fileIdx = i"
      >
        <span class="w-3 shrink-0 text-center font-mono text-[11px] font-semibold" :style="{ color: STATUS_COLOR[f.status] ?? 'var(--yellow)' }">{{ f.status }}</span>
        <span class="flex min-w-0 flex-1 truncate">
          <span class="truncate text-muted-foreground">{{ dir(f.path) }}</span>
          <span class="shrink-0 text-foreground">{{ base(f.path) }}</span>
        </span>
        <span class="shrink-0 text-[11px] tabular-nums">
          <span v-if="f.adds" class="text-[var(--green)]">+{{ f.adds }}</span>
          <span v-if="f.dels" class="ml-1.5 text-[var(--red)]">−{{ f.dels }}</span>
        </span>
      </button>
      <div v-if="!files.length" class="px-2 py-1.5 text-[12px] text-muted-foreground">Empty commit.</div>
    </div>

    <div class="flex min-h-0 flex-1 flex-col overflow-auto p-3">
      <GitDiff v-if="file" :key="fileIdx" :text="file.text" :path="file.path" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhCopy, PhCheck } from "@phosphor-icons/vue";
import { splitPatch, type PatchFile } from "@/lib/gitGraph";
import GitDiff from "./GitDiff.vue";

const props = defineProps<{ cwd: string; hash: string }>();
const emit = defineEmits<{ select: [hash: string] }>();

interface Detail { hash: string; author: string; email: string; date: string; committer: string; parents: string[]; message: string }
interface GitOutput { stdout: string; stderr: string; code: number }

const d = ref<Detail | null>(null);
const err = ref<string | null>(null);
const files = ref<PatchFile[]>([]);
const fileIdx = ref(0);
const copied = ref(false);

let gen = 0;
watch(() => [props.cwd, props.hash], async () => {
  const g = ++gen;
  d.value = null;
  err.value = null;
  files.value = [];
  fileIdx.value = 0;
  try {
    const [meta, patch] = await Promise.all([
      git(["show", "-s", "--format=%H%x1f%an%x1f%ae%x1f%aI%x1f%cn%x1f%P%x1f%B", props.hash]),
      git(["show", "--format=", "--diff-merges=first-parent", "-M", props.hash]),
    ]);
    if (g !== gen) return;
    const [hash, author, email, date, committer, parents, message] = meta.split("\x1f");
    d.value = { hash, author, email, date, committer, parents: parents.split(" ").filter(Boolean), message: message.trim() };
    files.value = splitPatch(patch);
  } catch (e) {
    if (g === gen) err.value = e instanceof Error ? e.message : String(e);
  }
}, { immediate: true });

async function git(args: string[]) {
  const out = await invoke<GitOutput>("run_git", { cwd: props.cwd, args });
  if (out.code !== 0) throw new Error(out.stderr || "git error");
  return out.stdout;
}

const subject = computed(() => d.value?.message.split("\n")[0] ?? "");
const body = computed(() => d.value?.message.split("\n").slice(1).join("\n").trim() ?? "");
const file = computed(() => files.value[fileIdx.value]);

// same palette as the graph lanes, hashed by author name (GitPaw's Avatar)
const AVATAR_COLORS = ["var(--accent)", "var(--green)", "var(--yellow)", "var(--blue)"];
const avatarColor = computed(() => {
  let h = 0;
  for (const ch of d.value?.author ?? "") h = (h * 31 + ch.charCodeAt(0)) | 0;
  return AVATAR_COLORS[Math.abs(h) % AVATAR_COLORS.length];
});
const initials = computed(() => (d.value?.author ?? "").split(/\s+/).map((p) => p[0]).slice(0, 2).join("").toUpperCase());

const STATUS_COLOR: Record<string, string> = { A: "var(--green)", D: "var(--red)", R: "var(--accent)", M: "var(--yellow)" };
const dir = (p: string) => p.slice(0, p.lastIndexOf("/") + 1);
const base = (p: string) => p.slice(p.lastIndexOf("/") + 1);

async function copy() {
  if (!d.value) return;
  await navigator.clipboard?.writeText(d.value.hash).catch(() => {});
  copied.value = true;
  setTimeout(() => (copied.value = false), 1200);
}
</script>

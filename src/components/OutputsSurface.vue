<script setup lang="ts">
import { computed, inject, ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhFileCode, PhFiles, PhPencilSimple } from "@phosphor-icons/vue";
import { chatSession } from "@/lib/chatSession";
import { chatOutputs, type ChatOutputFile, type OutputOp } from "@/lib/chatOutputs";

// Files the active chat created or modified, from its own transcript — not the
// workspace diff (that is the Diff surface). Read-only over the session.
const props = defineProps<{ chatId: number | null; cwd: string }>();

const activeTerm = inject<() => any>("activeTerm", () => undefined);
const openFileInPanel = inject<((path: string) => void) | undefined>("openFileInPanel", undefined);

const files = computed<ChatOutputFile[]>(() =>
  props.chatId ? chatOutputs(chatSession(props.chatId).messages.value) : [],
);

const OP_LABEL: Record<OutputOp, string> = { created: "Created", edited: "Edited", deleted: "Deleted" };
const OP_CLASS: Record<OutputOp, string> = {
  created: "border-success/40 text-success",
  edited: "border-warning/40 text-warning",
  deleted: "border-destructive/40 text-destructive",
};

// Codex can report a path relative to the thread's cwd.
function absolute(path: string): string {
  return path.startsWith("/") || !props.cwd ? path : `${props.cwd.replace(/\/$/, "")}/${path}`;
}
function basename(path: string): string {
  return path.split("/").pop() || path;
}
function relDir(path: string): string {
  const abs = absolute(path);
  const root = props.cwd.replace(/\/$/, "");
  const rel = root && abs.startsWith(root + "/") ? abs.slice(root.length + 1) : abs;
  const i = rel.lastIndexOf("/");
  return i > 0 ? rel.slice(0, i) : "";
}

const opening = ref("");

type GitOut = { stdout: string; stderr: string; code: number };
function git(cwd: string, args: string[]): Promise<GitOut> {
  return invoke<GitOut>("run_git", { cwd, args });
}

/** Working tree vs HEAD; a file git doesn't track diffs against /dev/null; a
 *  tracked file with no diff left (committed since) opens in the editor. */
async function openDiff(f: ChatOutputFile) {
  const abs = absolute(f.path);
  const dir = abs.slice(0, abs.lastIndexOf("/")) || props.cwd;
  opening.value = f.path;
  try {
    let diff = (await git(dir, ["diff", "HEAD", "--", abs]).catch(() => null))?.stdout ?? "";
    if (!diff && f.op !== "deleted") {
      const tracked = (await git(dir, ["ls-files", "--", abs]).catch(() => null))?.stdout.trim();
      // --no-index exits 1 when the files differ; stdout is still the diff.
      if (!tracked) diff = (await git(dir, ["diff", "--no-index", "--", "/dev/null", abs]).catch(() => null))?.stdout ?? "";
    }
    if (diff && activeTerm()?.openDiffInTab) activeTerm()!.openDiffInTab(basename(abs), false, diff);
    else if (f.op !== "deleted") openFileInPanel?.(abs);
  } finally {
    opening.value = "";
  }
}
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col overflow-hidden text-[11px] text-secondary-foreground">
    <header class="flex items-center justify-between border-b border-border p-2">
      <span class="flex items-center gap-1.5 font-semibold text-foreground"><PhFiles :size="14" /> Outputs</span>
      <span v-if="files.length" class="text-muted-foreground">{{ files.length }} file{{ files.length === 1 ? "" : "s" }}</span>
    </header>
    <div v-if="!props.chatId" class="p-4 text-center leading-relaxed text-muted-foreground">Open a chat to see the files it wrote.</div>
    <div v-else-if="files.length === 0" class="p-4 text-center leading-relaxed text-muted-foreground">This chat hasn't created or edited any files yet.</div>
    <div v-else class="flex-1 overflow-auto">
      <div
        v-for="f in files"
        :key="f.path"
        class="group flex cursor-pointer items-center gap-2 border-b border-border/65 px-2 py-1.5 hover:bg-hover"
        :class="{ 'opacity-60': opening === f.path }"
        :title="`${absolute(f.path)} — open diff`"
        @click="openDiff(f)"
      >
        <PhPencilSimple :size="12" class="shrink-0 text-muted-foreground" />
        <span class="min-w-0 flex-1 truncate">
          <span class="text-foreground" :class="{ 'line-through': f.op === 'deleted' }">{{ basename(f.path) }}</span>
          <span v-if="relDir(f.path)" class="ml-1.5 font-mono text-[10px] text-muted-foreground">{{ relDir(f.path) }}</span>
        </span>
        <span v-if="f.edits > 1" class="shrink-0 text-[10px] text-muted-foreground" :title="`${f.edits} tool calls`">×{{ f.edits }}</span>
        <span class="shrink-0 rounded border px-1 py-px text-[9px] font-semibold uppercase" :class="OP_CLASS[f.op]">{{ OP_LABEL[f.op] }}</span>
        <button
          v-if="f.op !== 'deleted' && openFileInPanel"
          class="inline-flex shrink-0 p-0.5 text-muted-foreground opacity-0 hover:rounded hover:bg-hover hover:text-foreground group-hover:opacity-100"
          title="Open in editor"
          @click.stop="openFileInPanel(absolute(f.path))"
        >
          <PhFileCode :size="13" />
        </button>
      </div>
    </div>
  </section>
</template>

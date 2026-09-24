<script setup lang="ts">
import { computed, nextTick, watch } from "vue";
import { PhArrowLeft, PhFileCode, PhPushPin, PhX } from "@phosphor-icons/vue";
import CodeEditor from "./CodeEditor.vue";
import { useFileViewerStore } from "@/stores/fileViewer";

const props = defineProps<{ workspaceId: number; cwd: string }>();
const emit = defineEmits<{
  closeFile: [path: string];
  saved: [];
  error: [message: string];
}>();

const viewer = useFileViewerStore();
const state = viewer.workspace(props.workspaceId);
const activeFile = computed(() => state.files.find((file) => file.path === state.activePath));
const editorRefs = new Map<string, InstanceType<typeof CodeEditor>>();

function setEditorRef(path: string, value: unknown) {
  if (value) editorRefs.set(path, value as InstanceType<typeof CodeEditor>);
  else editorRefs.delete(path);
}

watch(
  () => [activeFile.value?.path, activeFile.value?.revealKey] as const,
  async () => {
    await nextTick();
    const file = activeFile.value;
    if (!file) return;
    if (file.line) editorRefs.get(file.path)?.revealLine(file.line);
    editorRefs.get(file.path)?.focus();
  },
);
</script>

<template>
  <section
    v-show="state.visible && state.files.length"
    class="absolute inset-0 z-20 flex min-h-0 min-w-0 flex-col bg-[var(--terminal-bg)]"
    aria-label="Open files"
    @click.stop
    @mousedown.stop
  >
    <div class="flex h-[30px] shrink-0 items-stretch border-b border-border bg-[var(--surface)]">
      <button
        class="flex w-8 shrink-0 items-center justify-center border-r border-border text-muted-foreground transition-colors hover:bg-hover hover:text-foreground"
        title="Return to thread"
        aria-label="Return to thread"
        @click="viewer.hide(workspaceId)"
      >
        <PhArrowLeft :size="13" />
      </button>
      <div class="flex min-w-0 flex-1 overflow-x-auto">
        <div
          v-for="file in state.files"
          :key="file.path"
          class="group flex min-w-[110px] max-w-[220px] items-center gap-1.5 border-r border-border px-2.5 text-left text-[11px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground"
          :class="[
            state.activePath === file.path && 'bg-[var(--terminal-bg)] text-foreground',
            !file.pinned && !file.dirty && 'italic',
          ]"
          :title="file.path"
          role="tab"
          tabindex="0"
          :aria-selected="state.activePath === file.path"
          @click="viewer.activate(workspaceId, file.path)"
          @dblclick="viewer.pin(workspaceId, file.path)"
          @keydown.enter="viewer.activate(workspaceId, file.path)"
        >
          <PhFileCode :size="11" class="shrink-0" />
          <span class="min-w-0 flex-1 truncate">{{ file.name }}</span>
          <span v-if="file.dirty" class="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground" aria-label="Unsaved changes" />
          <PhPushPin v-else-if="file.pinned" :size="9" weight="fill" class="shrink-0 opacity-55" aria-label="Pinned file" />
          <button
            class="flex h-4 w-4 shrink-0 items-center justify-center rounded opacity-0 group-hover:opacity-70 hover:!bg-destructive/[0.15] hover:!text-destructive hover:!opacity-100"
            :aria-label="`Close ${file.name}`"
            @click.stop="emit('closeFile', file.path)"
            @keydown.enter.stop="emit('closeFile', file.path)"
          >
            <PhX :size="9" weight="bold" />
          </button>
        </div>
      </div>
    </div>

    <div class="relative flex min-h-0 flex-1 overflow-hidden">
      <CodeEditor
        v-for="file in state.files"
        v-show="state.activePath === file.path"
        :key="file.path"
        :leaf-id="file.id"
        :path="file.path"
        :cwd="cwd"
        :initial-line="file.line"
        :ref="(value) => setEditorRef(file.path, value)"
        @dirty="(dirty) => viewer.markDirty(workspaceId, file.path, dirty)"
        @saved="emit('saved')"
        @error="(message) => emit('error', message)"
      />
    </div>
  </section>
</template>

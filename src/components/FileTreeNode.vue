<template>
  <div>
    <div
      class="group mx-1 flex h-[22px] cursor-pointer items-center gap-1.5 whitespace-nowrap rounded text-xs text-foreground hover:bg-hover"
      :class="{ 'bg-selected': store.selectedId === node.id }"
      :style="{ paddingLeft: `${8 + depth * 12}px` }"
      @click="handleClick"
    >
      <PhSpinner    v-if="node.loading" class="w-2.5 shrink-0 animate-spin text-secondary-foreground" :size="10" />
      <PhCaretRight v-else-if="node.type === 'folder' && !node.expanded" class="w-2.5 shrink-0 text-secondary-foreground" :size="10" weight="bold" />
      <PhCaretDown  v-else-if="node.type === 'folder' && node.expanded" class="w-2.5 shrink-0 text-secondary-foreground" :size="10" weight="bold" />
      <span v-else class="w-2.5 shrink-0 opacity-0" />

      <PhFolderOpen v-if="node.type === 'folder' && node.expanded" class="shrink-0 text-blue-400" :size="14" weight="fill" />
      <PhFolder     v-else-if="node.type === 'folder'"             class="shrink-0 text-blue-400" :size="14" weight="fill" />
      <component    v-else :is="fileIconComponent(node.name)"      class="shrink-0 text-secondary-foreground" :size="14" weight="regular" />

      <span class="flex-1 truncate text-xs">{{ node.name }}</span>

      <button
        class="mr-1 hidden shrink-0 items-center justify-center rounded p-0.5 text-muted-foreground hover:bg-hover hover:text-accent group-hover:flex"
        title="Add to agent context (@path)"
        @click.stop="addToContext"
      >
        <PhAt :size="12" weight="bold" />
      </button>
    </div>

    <template v-if="node.type === 'folder' && node.expanded && node.children">
      <FileTreeNode v-for="child in node.children" :key="child.id" :node="child" :depth="depth + 1" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { inject } from "vue";
import {
  PhCaretRight, PhCaretDown,
  PhFolder, PhFolderOpen,
  PhFileVue, PhFileTs, PhFileJs, PhFileCode,
  PhGear, PhFile, PhSpinner, PhAt,
} from "@phosphor-icons/vue";
import { useFileTreeStore, type FileNode } from "@/stores/fileTree";

const props = defineProps<{ node: FileNode; depth: number }>();
const store = useFileTreeStore();
const activeTerm = inject<() => any>("activeTerm", () => undefined);

function handleClick() {
  if (props.node.type === "folder") {
    store.toggle(props.node.id);
  } else {
    store.select(props.node.id);
    activeTerm()?.openFileInTab(props.node.id, props.node.name);
  }
}

function addToContext() {
  activeTerm()?.insertContext(props.node.id);
}

function fileIconComponent(name: string) {
  if (name.endsWith(".vue"))  return PhFileVue;
  if (name.endsWith(".ts"))   return PhFileTs;
  if (name.endsWith(".js"))   return PhFileJs;
  if (name.endsWith(".rs"))   return PhFileCode;
  if (name.endsWith(".json") || name.endsWith(".toml")) return PhGear;
  return PhFile;
}
</script>

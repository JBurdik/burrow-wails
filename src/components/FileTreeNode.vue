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
      <component    v-else :is="fileIcon(node.name).icon" class="shrink-0" :class="fileIcon(node.name).color" :size="14" weight="regular" />

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
import { inject, type Component } from "vue";
import {
  PhCaretRight, PhCaretDown,
  PhFolder, PhFolderOpen,
  PhFileVue, PhFileTs, PhFileTsx, PhFileJs, PhFileJsx, PhFileCode,
  PhFileCss, PhFileHtml, PhFileMd, PhFilePy, PhFileRs, PhFileSql,
  PhFileImage, PhFileSvg, PhFileZip, PhFileLock, PhFileTxt, PhFileCsv,
  PhBracketsCurly, PhPackage, PhGitBranch, PhCube,
  PhFile, PhSpinner, PhAt,
} from "@phosphor-icons/vue";

// ponytail: extension/filename -> icon+color map, close enough to a vscode-icons theme without pulling one in
const EXT_ICONS: Record<string, { icon: Component; color: string }> = {
  vue: { icon: PhFileVue, color: "text-emerald-400" },
  ts: { icon: PhFileTs, color: "text-blue-400" },
  mts: { icon: PhFileTs, color: "text-blue-400" },
  tsx: { icon: PhFileTsx, color: "text-blue-400" },
  js: { icon: PhFileJs, color: "text-yellow-400" },
  mjs: { icon: PhFileJs, color: "text-yellow-400" },
  cjs: { icon: PhFileJs, color: "text-yellow-400" },
  jsx: { icon: PhFileJsx, color: "text-cyan-400" },
  json: { icon: PhBracketsCurly, color: "text-orange-400" },
  jsonc: { icon: PhBracketsCurly, color: "text-orange-400" },
  css: { icon: PhFileCss, color: "text-purple-400" },
  scss: { icon: PhFileCss, color: "text-pink-400" },
  html: { icon: PhFileHtml, color: "text-orange-400" },
  md: { icon: PhFileMd, color: "text-emerald-300" },
  mdx: { icon: PhFileMd, color: "text-emerald-300" },
  py: { icon: PhFilePy, color: "text-blue-400" },
  rs: { icon: PhFileRs, color: "text-orange-500" },
  go: { icon: PhFileCode, color: "text-cyan-400" },
  sql: { icon: PhFileSql, color: "text-teal-400" },
  svg: { icon: PhFileSvg, color: "text-orange-400" },
  png: { icon: PhFileImage, color: "text-pink-400" },
  jpg: { icon: PhFileImage, color: "text-pink-400" },
  jpeg: { icon: PhFileImage, color: "text-pink-400" },
  gif: { icon: PhFileImage, color: "text-pink-400" },
  webp: { icon: PhFileImage, color: "text-pink-400" },
  ico: { icon: PhFileImage, color: "text-pink-400" },
  zip: { icon: PhFileZip, color: "text-orange-400" },
  gz: { icon: PhFileZip, color: "text-orange-400" },
  tar: { icon: PhFileZip, color: "text-orange-400" },
  lock: { icon: PhFileLock, color: "text-muted-foreground" },
  yml: { icon: PhFileCode, color: "text-red-400" },
  yaml: { icon: PhFileCode, color: "text-red-400" },
  toml: { icon: PhFileCode, color: "text-orange-400" },
  txt: { icon: PhFileTxt, color: "text-muted-foreground" },
  csv: { icon: PhFileCsv, color: "text-emerald-400" },
  webmanifest: { icon: PhBracketsCurly, color: "text-muted-foreground" },
};

const NAME_ICONS: Record<string, { icon: Component; color: string }> = {
  "package.json": { icon: PhPackage, color: "text-red-400" },
  "package-lock.json": { icon: PhFileLock, color: "text-red-400" },
  "pnpm-lock.yaml": { icon: PhFileLock, color: "text-yellow-400" },
  "bun.lock": { icon: PhFileLock, color: "text-muted-foreground" },
  "bunfig.toml": { icon: PhFileCode, color: "text-orange-400" },
  ".gitignore": { icon: PhGitBranch, color: "text-orange-400" },
  ".dockerignore": { icon: PhCube, color: "text-blue-400" },
  dockerfile: { icon: PhCube, color: "text-blue-400" },
  justfile: { icon: PhFileCode, color: "text-yellow-400" },
};
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

function fileIcon(name: string) {
  const lower = name.toLowerCase();
  const ext = lower.includes(".") ? lower.slice(lower.lastIndexOf(".") + 1) : "";
  return NAME_ICONS[lower] ?? EXT_ICONS[ext] ?? { icon: PhFile, color: "text-secondary-foreground" };
}
</script>

import { defineStore } from "pinia";
import { ref } from "vue";
import { invoke } from "@tauri-apps/api/core";

export interface FileNode {
  id: string; // full absolute path
  name: string;
  type: "file" | "folder";
  children?: FileNode[];
  expanded?: boolean;
  loading?: boolean;
  error?: string;
}

interface RawEntry {
  name: string;
  isDir: boolean;
}

const HIDDEN = new Set([".git", "node_modules", "target", ".DS_Store"]);

async function fetchChildren(dirPath: string): Promise<FileNode[]> {
  const entries = await invoke<RawEntry[]>("read_dir_shallow", { path: dirPath });
  return entries
    .filter((e) => !HIDDEN.has(e.name))
    .map((e) => ({
      id: dirPath + "/" + e.name,
      name: e.name,
      type: e.isDir ? "folder" : "file",
      children: e.isDir ? [] : undefined,
      expanded: false,
    }));
}

function findNode(nodes: FileNode[], path: string): FileNode | null {
  for (const n of nodes) {
    if (n.id === path) return n;
    if (n.children) {
      const found = findNode(n.children, path);
      if (found) return found;
    }
  }
  return null;
}

export const useFileTreeStore = defineStore("fileTree", () => {
  const tree = ref<FileNode[]>([]);
  const selectedId = ref<string>("");
  const rootError = ref<string | null>(null);

  async function loadRoot(rootPath: string) {
    rootError.value = null;
    tree.value = [];
    try {
      tree.value = await fetchChildren(rootPath);
    } catch (e: unknown) {
      rootError.value = e instanceof Error ? e.message : "Failed to read directory";
    }
  }

  function clearTree() {
    tree.value = [];
    rootError.value = null;
  }

  async function expandNode(path: string) {
    const node = findNode(tree.value, path);
    if (!node || node.type !== "folder") return;
    if (node.children && node.children.length > 0) {
      node.expanded = true;
      return;
    }
    node.loading = true;
    node.error = undefined;
    try {
      node.children = await fetchChildren(path);
      node.expanded = true;
    } catch (e: unknown) {
      // ponytail: surface the failure instead of silently flipping to "expanded, 0 children" -
      // that state was indistinguishable from a genuinely empty folder and from "did nothing".
      node.error = e instanceof Error ? e.message : "Failed to read directory";
      console.error(`[fileTree] failed to read "${path}":`, e);
    } finally {
      node.loading = false;
    }
  }

  function collapseNode(path: string) {
    const node = findNode(tree.value, path);
    if (node) node.expanded = false;
  }

  function toggle(id: string) {
    const node = findNode(tree.value, id);
    if (!node || node.type !== "folder") return;
    if (node.expanded) collapseNode(id);
    else expandNode(id);
  }

  function select(id: string) {
    selectedId.value = id;
  }

  return { tree, selectedId, rootError, loadRoot, clearTree, toggle, select };
});

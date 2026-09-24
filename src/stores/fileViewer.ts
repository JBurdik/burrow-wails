import { defineStore } from "pinia";
import { reactive } from "vue";

export interface OpenEditorFile {
  id: number;
  path: string;
  name: string;
  line?: number;
  pinned: boolean;
  dirty: boolean;
  revealKey: number;
}

export interface FileViewerWorkspace {
  files: OpenEditorFile[];
  activePath: string | null;
  previewPath: string | null;
  visible: boolean;
}

export const useFileViewerStore = defineStore("fileViewer", () => {
  const byWorkspace = reactive<Record<number, FileViewerWorkspace>>({});
  let nextEditorId = -1;

  function workspace(workspaceId: number): FileViewerWorkspace {
    if (!byWorkspace[workspaceId]) {
      byWorkspace[workspaceId] = {
        files: [],
        activePath: null,
        previewPath: null,
        visible: false,
      };
    }
    // Read it back through the reactive record so the first caller receives
    // Vue's proxy too, not the raw right-hand side of an assignment.
    return byWorkspace[workspaceId];
  }

  function openFile(
    workspaceId: number,
    path: string,
    name: string,
    line?: number,
    options: { pin?: boolean } = {},
  ): OpenEditorFile {
    const state = workspace(workspaceId);
    const existing = state.files.find((file) => file.path === path);
    if (existing) {
      existing.line = line;
      existing.revealKey++;
      if (options.pin) {
        existing.pinned = true;
        if (state.previewPath === path) state.previewPath = null;
      }
      state.activePath = path;
      state.visible = true;
      return existing;
    }

    if (!options.pin && state.previewPath) {
      const preview = state.files.find((file) => file.path === state.previewPath);
      if (preview && !preview.pinned && !preview.dirty) {
        state.files = state.files.filter((file) => file.path !== preview.path);
      }
      state.previewPath = null;
    }

    const file: OpenEditorFile = {
      id: nextEditorId--,
      path,
      name,
      line,
      pinned: options.pin === true,
      dirty: false,
      revealKey: 0,
    };
    state.files.push(file);
    state.activePath = path;
    state.previewPath = file.pinned ? null : path;
    state.visible = true;
    return file;
  }

  function activate(workspaceId: number, path: string) {
    const state = workspace(workspaceId);
    if (!state.files.some((file) => file.path === path)) return;
    state.activePath = path;
    state.visible = true;
  }

  function pin(workspaceId: number, path: string) {
    const state = workspace(workspaceId);
    const file = state.files.find((candidate) => candidate.path === path);
    if (!file) return;
    file.pinned = true;
    if (state.previewPath === path) state.previewPath = null;
  }

  function markDirty(workspaceId: number, path: string, dirty: boolean) {
    const state = workspace(workspaceId);
    const file = state.files.find((candidate) => candidate.path === path);
    if (!file) return;
    file.dirty = dirty;
    if (dirty) pin(workspaceId, path);
  }

  function closeFile(workspaceId: number, path: string) {
    const state = workspace(workspaceId);
    const index = state.files.findIndex((file) => file.path === path);
    if (index < 0) return;
    state.files.splice(index, 1);
    if (state.previewPath === path) state.previewPath = null;
    if (state.activePath === path) {
      state.activePath = state.files[Math.min(index, state.files.length - 1)]?.path ?? null;
    }
    if (state.files.length === 0) state.visible = false;
  }

  function hide(workspaceId: number) {
    workspace(workspaceId).visible = false;
  }

  return { byWorkspace, workspace, openFile, activate, pin, markDirty, closeFile, hide };
});

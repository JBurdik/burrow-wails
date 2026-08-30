<template>
  <div
    v-if="node.type === 'leaf'"
    class="relative flex min-h-0 min-w-0 flex-1 overflow-hidden"
    :class="{ 'after:pointer-events-none after:absolute after:inset-0 after:z-[1] after:border after:border-accent after:opacity-35 after:content-[\'\']': focusedId === node.id }"
    @mousedown.capture="$emit('focus', (node as Leaf).id)"
  >
    <DiffTab
      v-if="(node as Leaf).leafType === 'diff'"
      :diff-file="(node as Leaf).diffFile!"
      :diff-staged="(node as Leaf).diffStaged ?? false"
      :diff="(node as Leaf).diff || ''"
    />
    <CodeEditor
      v-else-if="(node as Leaf).leafType === 'editor'"
      :leaf-id="(node as Leaf).id"
      :path="(node as Leaf).filePath!"
      :cwd="(node as Leaf).cwd ?? cwd"
      :initial-line="(node as Leaf).fileLine"
      :ref="(el: unknown) => registerRef((node as Leaf).id, el)"
      @title="(t: string) => $emit('title', (node as Leaf).id, t)"
      @dirty="(d: boolean) => $emit('dirty', (node as Leaf).id, d)"
      @saved="() => $emit('saved', (node as Leaf).id)"
    />
    <BrowserPane
      v-else-if="(node as Leaf).leafType === 'browser'"
      :initial-url="(node as Leaf).browserUrl"
    />
    <GitPanel
      v-else-if="(node as Leaf).leafType === 'git'"
      class="min-w-0 flex-1"
    />
    <XTerm
      v-else
      :pty-id="(node as Leaf).id"
      :cwd="cwd"
      :initial-cmd="(node as Leaf).initialCmd"
      :ref="(el: unknown) => registerRef((node as Leaf).id, el)"
      @title="(t: string) => $emit('title', (node as Leaf).id, t)"
      @busy="(b: boolean) => $emit('busy', (node as Leaf).id, b)"
    />
  </div>
  <div
    v-else
    class="flex min-h-0 min-w-0 flex-1 overflow-hidden"
    :class="node.direction === 'h' ? 'flex-row' : 'flex-col'"
  >
    <TerminalSplitView
      :node="node.first"
      :cwd="cwd"
      :focused-id="focusedId"
      @focus="$emit('focus', $event)"
      @title="(id, t) => $emit('title', id, t)"
      @busy="(id, b) => $emit('busy', id, b)"
      @dirty="(id, d) => $emit('dirty', id, d)"
      @saved="(id) => $emit('saved', id)"
    />
    <div class="shrink-0 bg-border" :class="node.direction === 'h' ? 'w-px' : 'h-px'" />
    <TerminalSplitView
      :node="node.second"
      :cwd="cwd"
      :focused-id="focusedId"
      @focus="$emit('focus', $event)"
      @title="(id, t) => $emit('title', id, t)"
      @busy="(id, b) => $emit('busy', id, b)"
      @dirty="(id, d) => $emit('dirty', id, d)"
      @saved="(id) => $emit('saved', id)"
    />
  </div>
</template>

<script setup lang="ts">
import { inject } from "vue";
import XTerm from "./XTerm.vue";
import DiffTab from "./DiffTab.vue";
import CodeEditor from "./CodeEditor.vue";
import BrowserPane from "./BrowserPane.vue";
import GitPanel from "./GitPanel.vue";
import TerminalSplitView from "./TerminalSplitView.vue";

export interface Leaf {
  type: "leaf";
  id: number;
  title: string;
  defaultTitle: string;
  isAgent: boolean;
  busy: boolean;
  status: import("@/lib/terminalStatus").TermStatus;
  initialCmd?: string;
  cwd?: string;          // per-tab cwd override (else workspace cwd)
  resultToken?: string;  // set on tabs spawned via `burrow spawn --token`
  leafType?: "terminal" | "diff" | "editor" | "chat" | "browser" | "git";  // default "terminal"
  browserUrl?: string; // set when leafType === "browser"
  round?: number;       // increments on each new user prompt submission (UserPromptSubmit)
  statusText?: string;  // set by `burrow set-status`; shown next to status dot
  statusDetail?: string; // error cause (rate_limit|overloaded|auth…) for an 'error' status; tooltip
  model?: string;        // agent model id from SessionStart metadata (e.g. claude-opus-4-8)
  sessionTitle?: string; // session title from SessionStart metadata
  progress?: number;    // 0.0–1.0; set by `burrow set-progress`
  progressLabel?: string;
  sessionId?: string;   // Claude session_id for cross-restart resume
  diffFile?: string;
  diffStaged?: boolean;
  diff?: string;
  diffOwnerPtyId?: number; // agent PTY that requested this diff, if any
  filePath?: string;  // set when leafType === "editor" (absolute path)
  fileLine?: number;  // editor: 1-based line to reveal on open (⌘P search hit)
  dirty?: boolean;    // editor: unsaved changes
  chatId?: number;    // set when leafType === "chat"
  initialPrompt?: string; // chat: first message to auto-send once its runtime is up
}

export interface SplitNode {
  type: "split";
  direction: "h" | "v";
  first: TreeNode;
  second: TreeNode;
  ratio?: number;   // fraction of space given to `first` (0..1), default 0.5
}

export type TreeNode = Leaf | SplitNode;

defineProps<{
  node: TreeNode;
  cwd: string;
  focusedId: number;
}>();

defineEmits<{
  focus: [id: number];
  title: [id: number, t: string];
  busy: [id: number, b: boolean];
  dirty: [id: number, d: boolean];
  saved: [id: number];
}>();

const registerRef = inject<(id: number, el: unknown) => void>("registerRef")!;
</script>

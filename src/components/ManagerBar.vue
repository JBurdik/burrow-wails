<template>
  <div class="relative z-30 flex min-h-0 flex-col bg-[var(--bg-base,#0d0d0d)] [background-image:linear-gradient(var(--bg-panel,#111),var(--bg-panel,#111))]" :class="props.rail ? 'h-full flex-1' : 'shrink-0 border-t border-border'">
    <!-- Drag handle (top border) — resize the expanded panel height -->
    <div
      v-show="!props.rail && expanded"
      class="-mt-[3px] h-[5px] shrink-0 cursor-row-resize hover:bg-accent/40"
      @mousedown="startResize"
    />

    <!-- Expanded panel: animated height wrapper keeps the chat mounted while it
         slides open/closed. Inner panel is fixed-height so content doesn't
         squish mid-animation. Only PAST MESSAGES live here — the composer is in
         the strip below. -->
    <div
      v-if="started"
      class="overflow-hidden"
      :class="props.rail ? 'flex min-h-0 flex-1' : 'shrink-0'"
      :style="props.rail ? undefined : { height: (expanded ? panelHeight : 0) + 'px', transition: isResizing ? 'none' : 'height 0.22s cubic-bezier(0.4,0,0.2,1)' }"
    >
      <div class="mb-panel flex min-h-0 flex-1 flex-col border-b border-border" :style="props.rail ? undefined : { height: panelHeight + 'px' }">
        <div class="mb-chat min-h-0 flex-1 bg-[var(--bg-base,#0d0d0d)]">
          <!-- One ClaudeChat per engaged repo, kept mounted and v-show'd. This is
               what lets a busy Manager keep streaming when you switch workspace:
               we flip visibility instead of unmounting (which would claude_stop). -->
          <AgentChat
            v-for="m in mountedManagers"
            v-show="m.repoId === rootId"
            :key="m.sessionId"
            :ref="(el) => setChatRef(m.repoId, el)"
            compact
            hide-composer
            model-key="burrow.manager.model"
            :default-model="managerModel"
            :chat-id="m.sessionId"
            :workspace-id="m.repoId"
            :cwd="m.cwd"
            :append-system-prompt="managerPrimer"
          />
        </div>
      </div>
    </div>

    <!-- Always-visible bottom strip — holds the one Manager composer -->
    <div class="shrink-0 border-t border-border" :class="props.rail ? 'p-2' : 'flex min-h-[38px] items-center gap-2 px-2.5 py-1.5'">
      <PhSparkle v-if="!props.rail" :size="15" weight="fill" class="shrink-0 text-accent" />
      <span v-if="!props.rail" class="shrink-0 text-xs font-semibold text-foreground">Manager</span>
      <span v-if="!props.rail" class="mb-status-dot h-2 w-2 shrink-0 rounded-full" :class="`mb-dot-${dotKind}`" :title="dotTitle" />
      <span v-if="!props.rail" class="shrink-0 max-w-[140px] overflow-hidden text-ellipsis whitespace-nowrap text-[10px] text-muted-foreground" :title="rootCwd">{{ rootName }}</span>

      <!-- Quick input with multiline + suggestions -->
      <ComposerBox class="mb-quick-wrap min-w-0 flex-1 overflow-hidden rounded-[14px] border border-white/10 bg-[#1a1a20] transition-colors focus-within:border-accent/50">
      <div class="relative">
        <!-- /command suggestions -->
        <div v-if="cmdSuggestions.length" class="absolute inset-x-0 bottom-[calc(100%+4px)] z-[80] max-h-[200px] overflow-y-auto rounded-lg border border-border bg-[var(--bg-dropdown,#18181c)] shadow-[0_-8px_24px_rgba(0,0,0,0.4)]">
          <div
            v-for="(s, i) in cmdSuggestions"
            :key="s.name"
            class="flex cursor-pointer items-baseline gap-2.5 px-2.5 py-1.5 transition-colors hover:bg-white/[0.06]"
            :class="i === cmdIdx && 'bg-white/[0.06]'"
            @mousedown.prevent="applyCmd(s.name)"
          >
            <span class="mb-sug-name min-w-[90px] shrink-0 font-mono text-xs font-semibold">/{{ s.name }}</span>
            <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-white/40">{{ s.description }}</span>
          </div>
        </div>
        <!-- @file suggestions -->
        <div v-if="atSuggestions.length" class="absolute inset-x-0 bottom-[calc(100%+4px)] z-[80] max-h-[200px] overflow-y-auto rounded-lg border border-border bg-[var(--bg-dropdown,#18181c)] shadow-[0_-8px_24px_rgba(0,0,0,0.4)]">
          <div
            v-for="(p, i) in atSuggestions"
            :key="p"
            class="flex cursor-pointer items-baseline gap-2.5 px-2.5 py-1.5 transition-colors hover:bg-white/[0.06]"
            :class="i === atIdx && 'bg-white/[0.06]'"
            @mousedown.prevent="applyAt(p)"
          >
            <span class="mb-sug-name min-w-[90px] shrink-0 font-mono text-xs font-semibold">@{{ p.slice(p.lastIndexOf('/') + 1) }}</span>
            <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-white/40">{{ p }}</span>
          </div>
        </div>
        <!-- Pasted image thumbnails -->
        <div v-if="pastedImages.length" class="flex flex-wrap gap-1.5 py-1">
          <div v-for="(img, i) in pastedImages" :key="i" class="relative shrink-0">
            <img :src="img" class="block h-12 w-12 rounded-md border border-white/10 object-cover" :alt="`Image ${i + 1}`" />
            <button
              class="absolute -right-[5px] -top-[5px] flex h-4 w-4 items-center justify-center rounded-full border-0 bg-black/70 p-0 text-[11px] leading-none text-white"
              title="Remove"
              @mousedown.prevent="pastedImages.splice(i, 1)"
            >×</button>
          </div>
        </div>
        <ComposerTextInput
          ref="quickEl"
          v-model="quickText"
          class="mb-quick box-border block max-h-[160px] min-h-[40px] w-full resize-none overflow-y-auto border-none bg-transparent px-3.5 pb-1.5 pt-3 font-sans text-[13px] leading-normal text-white/[0.88] outline-none placeholder:text-white/50"
          rows="1"
          :placeholder="busy ? 'Manager is working — queue a message…' : 'Message Manager… (Enter=send, Shift+Enter=newline, @file, /cmd)'"
          @focus="ensureStarted"
          @keydown="onQuickKeydown"
          @input="onQuickInput"
          @paste="onQuickPaste"
        />
      </div>
      <template v-if="props.rail" #toolbar>
        <div class="flex items-center gap-1.5 border-t border-white/[0.07] px-2 py-1.5">
          <div class="flex min-w-0 items-center gap-1.5 text-[11px] text-secondary-foreground">
            <PhSparkle :size="13" weight="fill" class="shrink-0 text-accent" />
            <span class="font-semibold">Manager</span>
            <span class="mb-status-dot h-1.5 w-1.5 shrink-0 rounded-full" :class="`mb-dot-${dotKind}`" :title="dotTitle" />
          </div>
          <div class="relative ml-auto flex items-center gap-1">
            <button class="flex h-7 items-center gap-1 rounded-md px-1.5 text-[11px] text-muted-foreground hover:bg-hover hover:text-foreground" title="Manager options" @click="railOptionsOpen = !railOptionsOpen">
              <PhSlidersHorizontal :size="14" />
              Options
            </button>
            <div v-if="railOptionsOpen" class="absolute bottom-[calc(100%+6px)] right-0 z-[80] w-[240px] rounded-[10px] border border-border bg-[var(--bg-dropdown,var(--bg-panel,#111))] p-1.5 shadow-[0_12px_32px_rgba(0,0,0,0.5)]">
              <div class="px-2 pb-1 pt-1 text-[10px] uppercase tracking-[0.07em] text-muted-foreground">Manager options</div>
              <div class="px-2 py-1 text-[10px] font-semibold text-muted-foreground">Model</div>
              <button v-for="m in MANAGER_MODELS" :key="m.id" class="mb-wt-item flex w-full items-center gap-2 rounded-lg border-0 bg-transparent p-2 text-left text-secondary-foreground hover:bg-white/[0.07]" :class="managerModel === m.id && 'mb-wt-item-on text-foreground'" @click="selectManagerModel(m.id); railOptionsOpen = false">
                <PhCpu :size="13" weight="bold" /><span class="flex-1 text-[11px]">{{ m.label }}</span><PhCheck v-if="managerModel === m.id" :size="13" class="text-accent" />
              </button>
              <div class="mt-1 border-t border-border/60 px-2 pb-1 pt-2 text-[10px] font-semibold text-muted-foreground">Agent workspace</div>
              <button class="mb-wt-item flex w-full items-center gap-2 rounded-lg border-0 bg-transparent p-2 text-left text-secondary-foreground hover:bg-white/[0.07]" @click="selectWorktreeMode(false); railOptionsOpen = false"><PhGitBranch :size="13" /><span class="flex-1 text-[11px]">Current branch</span><PhCheck v-if="!worktreeMode" :size="13" class="text-accent" /></button>
              <button class="mb-wt-item flex w-full items-center gap-2 rounded-lg border-0 bg-transparent p-2 text-left text-secondary-foreground hover:bg-white/[0.07]" @click="selectWorktreeMode(true); railOptionsOpen = false"><PhTree :size="13" /><span class="flex-1 text-[11px]">New worktree each</span><PhCheck v-if="worktreeMode" :size="13" class="text-accent" /></button>
              <div class="mt-1 border-t border-border/60 px-2 pb-1 pt-2 text-[10px] font-semibold text-muted-foreground">Permission</div>
              <button v-for="m in PERM_MODES" :key="m" class="mb-wt-item flex w-full items-center gap-2 rounded-lg border-0 bg-transparent p-2 text-left text-secondary-foreground hover:bg-white/[0.07]" :class="PERM_META[m].danger && 'text-destructive'" @click="selectPermMode(m); railOptionsOpen = false"><component :is="PERM_ICON[m]" :size="13" /><span class="flex-1 text-[11px]">{{ PERM_META[m].label }}</span><PhCheck v-if="activePermMode === m" :size="13" class="text-accent" /></button>
            </div>
            <button class="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-hover hover:text-foreground" title="Reset Manager session" @click="resetSession"><PhArrowCounterClockwise :size="14" /></button>
            <button class="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-hover hover:text-foreground" title="Project config" @click="emit('openProjectConfig')"><PhGear :size="14" /></button>
            <button class="flex h-7 w-7 items-center justify-center rounded-md bg-accent text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-40" title="Send to Manager" :disabled="!quickText.trim()" @click="quickSend"><PhArrowUp :size="14" weight="bold" /></button>
          </div>
        </div>
      </template>
      </ComposerBox>

      <!-- Model picker (Manager has its own model, default Sonnet) -->
      <div v-if="!props.rail" class="relative shrink-0">
        <button
          class="inline-flex h-[26px] shrink-0 items-center gap-[5px] whitespace-nowrap rounded-[7px] border border-border bg-transparent px-2 font-sans text-[11px] text-secondary-foreground hover:bg-hover hover:text-foreground"
          title="Manager model"
          @click="mdlMenuOpen = !mdlMenuOpen"
        >
          <PhCpu :size="13" weight="bold" />
          <span class="font-medium">{{ managerModelLabel }}</span>
          <PhCaretUp :size="9" weight="bold" class="opacity-60" />
        </button>
        <div v-if="mdlMenuOpen" class="absolute bottom-[calc(100%+6px)] right-0 z-[70] w-[230px] rounded-[10px] border border-border bg-[var(--bg-dropdown,var(--bg-panel,#111))] p-1.5 shadow-[0_12px_32px_rgba(0,0,0,0.5)]">
          <div class="px-2 pb-1.5 pt-1 text-[10px] uppercase tracking-[0.07em] text-muted-foreground">Manager model</div>
          <button
            v-for="m in MANAGER_MODELS"
            :key="m.id"
            class="mb-wt-item flex w-full items-center gap-[9px] rounded-lg border-0 bg-transparent p-2 text-left text-secondary-foreground hover:bg-white/[0.07]"
            :class="managerModel === m.id && 'mb-wt-item-on text-foreground'"
            @click="selectManagerModel(m.id)"
          >
            <PhCpu :size="14" weight="bold" />
            <div class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span class="text-xs font-semibold text-foreground">{{ m.label }}</span>
              <span class="text-[10px] leading-[1.3] text-muted-foreground">{{ m.note }}</span>
            </div>
            <PhCheck v-if="managerModel === m.id" :size="13" weight="bold" class="shrink-0 text-accent" />
          </button>
        </div>
      </div>

      <!-- Spawn-target picker: clear labeled dropdown (replaces the cryptic
           icon toggle). Tells you where the Manager puts new agents. -->
      <div v-if="!props.rail" class="relative shrink-0">
        <button
          class="inline-flex h-[26px] shrink-0 items-center gap-[5px] whitespace-nowrap rounded-[7px] border border-border bg-transparent px-2 font-sans text-[11px] text-secondary-foreground hover:bg-hover hover:text-foreground"
          :title="'Where the Manager spawns new agents'"
          @click="wtMenuOpen = !wtMenuOpen"
        >
          <PhTree v-if="worktreeMode" :size="13" weight="bold" />
          <PhGitBranch v-else :size="13" weight="bold" />
          <span class="font-medium">{{ worktreeMode ? 'New worktree' : 'Current branch' }}</span>
          <PhCaretUp :size="9" weight="bold" class="opacity-60" />
        </button>
        <div v-if="wtMenuOpen" class="absolute bottom-[calc(100%+6px)] right-0 z-[70] w-[260px] rounded-[10px] border border-border bg-[var(--bg-dropdown,var(--bg-panel,#111))] p-1.5 shadow-[0_12px_32px_rgba(0,0,0,0.5)]">
          <div class="px-2 pb-1.5 pt-1 text-[10px] uppercase tracking-[0.07em] text-muted-foreground">Spawn agents in…</div>
          <button
            class="mb-wt-item flex w-full items-center gap-[9px] rounded-lg border-0 bg-transparent p-2 text-left text-secondary-foreground hover:bg-white/[0.07]"
            :class="!worktreeMode && 'mb-wt-item-on text-foreground'"
            @click="selectWorktreeMode(false)"
          >
            <PhGitBranch :size="14" weight="bold" />
            <div class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span class="text-xs font-semibold text-foreground">Current branch</span>
              <span class="text-[10px] leading-[1.3] text-muted-foreground">Shared working tree — fast, agents see each other's edits</span>
            </div>
            <PhCheck v-if="!worktreeMode" :size="13" weight="bold" class="shrink-0 text-accent" />
          </button>
          <button
            class="mb-wt-item flex w-full items-center gap-[9px] rounded-lg border-0 bg-transparent p-2 text-left text-secondary-foreground hover:bg-white/[0.07]"
            :class="worktreeMode && 'mb-wt-item-on text-foreground'"
            @click="selectWorktreeMode(true)"
          >
            <PhTree :size="14" weight="bold" />
            <div class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span class="text-xs font-semibold text-foreground">New worktree each</span>
              <span class="text-[10px] leading-[1.3] text-muted-foreground">Isolated branch per agent — safe for parallel work</span>
            </div>
            <PhCheck v-if="worktreeMode" :size="13" weight="bold" class="shrink-0 text-accent" />
          </button>
        </div>
      </div>

      <!-- Permission mode switcher -->
      <div v-if="!props.rail" class="relative shrink-0">
        <button
          class="inline-flex h-[26px] shrink-0 items-center gap-[5px] whitespace-nowrap rounded-[7px] border border-border bg-transparent px-2 font-sans text-[11px] text-secondary-foreground hover:bg-hover hover:text-foreground"
          :class="activePermMeta.danger && 'border-destructive text-destructive hover:bg-destructive/12'"
          :title="activePermMeta.title"
          @click="permMenuOpen = !permMenuOpen"
        >
          <component :is="PERM_ICON[activePermMode]" :size="13" weight="bold" />
          <span class="font-medium">{{ activePermMeta.label }}</span>
        </button>
        <div v-if="permMenuOpen" class="absolute bottom-[calc(100%+6px)] right-0 z-[70] w-[260px] rounded-[10px] border border-border bg-[var(--bg-dropdown,var(--bg-panel,#111))] p-1.5 shadow-[0_12px_32px_rgba(0,0,0,0.5)]">
          <div class="px-2 pb-1.5 pt-1 text-[10px] uppercase tracking-[0.07em] text-muted-foreground">Permission mode</div>
          <button
            v-for="m in PERM_MODES"
            :key="m"
            class="mb-wt-item flex w-full items-center gap-[9px] rounded-lg border-0 bg-transparent p-2 text-left text-secondary-foreground hover:bg-white/[0.07]"
            :class="[activePermMode === m && 'mb-wt-item-on text-foreground', PERM_META[m].danger && 'mb-wt-item-danger text-destructive hover:bg-destructive/10']"
            :title="PERM_META[m].title"
            @click="selectPermMode(m)"
          >
            <component :is="PERM_ICON[m]" :size="14" weight="bold" />
            <div class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span class="text-xs font-semibold text-foreground">{{ PERM_META[m].label }}</span>
              <span class="text-[10px] leading-[1.3] text-muted-foreground">{{ PERM_META[m].title }}</span>
            </div>
          </button>
        </div>
      </div>

      <button
        v-if="!props.rail"
        class="flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-md border-0 bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground"
        :title="expanded ? 'Collapse Manager (⌘J)' : 'Expand Manager (⌘J)'"
        @click="toggleExpanded"
      >
        <PhCaretDown v-if="expanded" :size="15" weight="bold" />
        <PhCaretUp v-else :size="15" weight="bold" />
      </button>
      <button
        v-if="!props.rail"
        class="flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-md border-0 bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground"
        title="Reset Manager session (clears conversation history, starts fresh)"
        @click="resetSession"
      >
        <PhArrowCounterClockwise :size="15" weight="bold" />
      </button>
      <button
        v-if="!props.rail"
        class="flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-md border-0 bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground"
        title="Project config"
        @click="emit('openProjectConfig')"
      >
        <PhGear :size="15" weight="bold" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from "vue";
import { PhSparkle, PhGitBranch, PhTree, PhCaretDown, PhCaretUp, PhCheck, PhCpu, PhGear, PhArrowCounterClockwise, PhShieldWarning, PhPencilSimple, PhShieldCheck, PhListChecks, PhFastForward, PhSlidersHorizontal, PhArrowUp } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import AgentChat from "./AgentChat.vue";
import ComposerTextInput from "./ComposerTextInput.vue";
import ComposerBox from "./ComposerBox.vue";
import { useUIStore } from "@/stores/ui";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import { getDefaultManagerPrimer, SPAWN_MODE_WORKTREE, SPAWN_MODE_BRANCH } from "@/utils/managerPrimer";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";

const props = withDefaults(defineProps<{ cwd: string; wsId: number; rail?: boolean }>(), { rail: false });
const emit = defineEmits<{ openProjectConfig: [] }>();

const ui = useUIStore();
const chats = useClaudeChatsStore();
const wsStore = useWorkspaceStore();

// One live ClaudeChat instance per engaged repo (function refs keyed by repo id).
const chatRefs = new Map<number, InstanceType<typeof AgentChat>>();
function setChatRef(repoId: number, el: unknown) {
  if (el) chatRefs.set(repoId, el as InstanceType<typeof AgentChat>);
  else chatRefs.delete(repoId);
}
const quickEl = ref<InstanceType<typeof ComposerTextInput> | null>(null);
const quickTextarea = computed(() => quickEl.value?.element ?? null);
const DRAFT_KEY = computed(() => `burrow.draft.manager.${props.wsId}`);
const quickText = ref(localStorage.getItem(DRAFT_KEY.value) ?? "");
watch(quickText, (val) => {
  if (val) localStorage.setItem(DRAFT_KEY.value, val);
  else localStorage.removeItem(DRAFT_KEY.value);
});
const pastedImages = ref<string[]>([]);
const railOptionsOpen = ref(false);

// ── Suggestions ─────────────────────────────────────────────────────────────
interface Command { name: string; description: string }
const cmdSuggestions = ref<Command[]>([]);
const cmdIdx = ref(0);
const atSuggestions = ref<string[]>([]);
const atIdx = ref(0);
const fileList = ref<string[]>([]);
let fileListLoaded = false;

async function ensureFileList() {
  if (fileListLoaded) return;
  fileListLoaded = true;
  try {
    const out = await invoke<{ stdout: string }>("run_git", {
      cwd: rootCwd.value,
      args: ["ls-files", "--cached", "--others", "--exclude-standard"],
    });
    fileList.value = out.stdout.split("\n").map((s) => s.trim()).filter(Boolean).slice(0, 20000);
  } catch { fileList.value = []; }
}

function updateCmdSuggestions() {
  const m = quickText.value.match(/^\/(\S*)$/);
  if (!m) { cmdSuggestions.value = []; return; }
  const q = m[1].toLowerCase();
  const cmds = chatRefs.get(rootId.value)?.allCommands ?? [];
  cmdSuggestions.value = (cmds as Command[]).filter((c) => c.name.toLowerCase().startsWith(q));
  cmdIdx.value = 0;
}

function atQueryBeforeCursor(): string | null {
  const el = quickTextarea.value;
  const pos = el?.selectionStart ?? quickText.value.length;
  const upto = quickText.value.slice(0, pos);
  const m = upto.match(/(?:^|\s)@([^\s@]*)$/);
  return m ? m[1] : null;
}

async function updateAtSuggestions() {
  const q = atQueryBeforeCursor();
  if (q === null) { atSuggestions.value = []; return; }
  await ensureFileList();
  if (atQueryBeforeCursor() !== q) return;
  const ql = q.toLowerCase();
  atSuggestions.value = fileList.value
    .filter((p) => p.toLowerCase().includes(ql))
    .sort((a, b) => {
      const ab = a.slice(a.lastIndexOf("/") + 1).toLowerCase();
      const bb = b.slice(b.lastIndexOf("/") + 1).toLowerCase();
      return (Number(!ab.startsWith(ql)) - Number(!bb.startsWith(ql))) || a.length - b.length;
    })
    .slice(0, 8);
  atIdx.value = 0;
}

function applyCmd(name: string) {
  quickText.value = `/${name} `;
  cmdSuggestions.value = [];
  nextTick(() => { quickEl.value?.focus(); quickAutoResize(); });
}

function applyAt(path: string) {
  const el = quickTextarea.value;
  const pos = el?.selectionStart ?? quickText.value.length;
  const upto = quickText.value.slice(0, pos);
  const after = quickText.value.slice(pos);
  const m = upto.match(/@([^\s@]*)$/);
  if (!m) return;
  const base = upto.slice(0, upto.length - m[0].length);
  quickText.value = `${base}@${path} ${after}`;
  atSuggestions.value = [];
  nextTick(() => { quickEl.value?.focus(); quickAutoResize(); });
}

function quickAutoResize() {
  const el = quickTextarea.value;
  if (!el) return;
  el.style.height = "auto";
  el.style.height = Math.min(el.scrollHeight, 120) + "px";
}

function onQuickInput() {
  quickAutoResize();
  updateCmdSuggestions();
  updateAtSuggestions();
}

function onQuickPaste(e: ClipboardEvent) {
  const items = e.clipboardData?.items;
  if (!items) return;
  for (const item of Array.from(items)) {
    if (item.type.startsWith("image/")) {
      e.preventDefault();
      const file = item.getAsFile();
      if (!file) continue;
      const reader = new FileReader();
      reader.onload = (ev) => {
        const dataUrl = ev.target?.result as string;
        if (dataUrl) pastedImages.value.push(dataUrl);
      };
      reader.readAsDataURL(file);
    }
  }
}

function onQuickKeydown(e: KeyboardEvent) {
  if (atSuggestions.value.length > 0) {
    if (e.key === "ArrowDown") { e.preventDefault(); atIdx.value = Math.min(atIdx.value + 1, atSuggestions.value.length - 1); return; }
    if (e.key === "ArrowUp") { e.preventDefault(); atIdx.value = Math.max(atIdx.value - 1, 0); return; }
    if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && !e.metaKey)) { e.preventDefault(); applyAt(atSuggestions.value[atIdx.value]); return; }
    if (e.key === "Escape") { atSuggestions.value = []; return; }
  }
  if (cmdSuggestions.value.length > 0) {
    if (e.key === "ArrowDown") { e.preventDefault(); cmdIdx.value = Math.min(cmdIdx.value + 1, cmdSuggestions.value.length - 1); return; }
    if (e.key === "ArrowUp") { e.preventDefault(); cmdIdx.value = Math.max(cmdIdx.value - 1, 0); return; }
    if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && !e.metaKey)) { e.preventDefault(); applyCmd(cmdSuggestions.value[cmdIdx.value].name); return; }
    if (e.key === "Escape") { cmdSuggestions.value = []; return; }
  }
  // Shift+Enter or Cmd+Enter = newline
  if (e.key === "Enter" && (e.shiftKey || e.metaKey)) {
    e.preventDefault();
    const el = quickTextarea.value;
    if (!el) return;
    const s = el.selectionStart ?? quickText.value.length;
    const en = el.selectionEnd ?? s;
    quickText.value = quickText.value.slice(0, s) + "\n" + quickText.value.slice(en);
    nextTick(() => { el.selectionStart = el.selectionEnd = s + 1; quickAutoResize(); });
    return;
  }
  // plain Enter = send
  if (e.key === "Enter") { e.preventDefault(); quickSend(); }
}

// Expanded state is shared with the existing ui pref (floatChatOpen) so ⌘J and the
// persisted preference keep working unchanged. `started` gates the first claude
// spawn: we don't launch a Manager process until the user first opens or types.
const expanded = computed(() => props.rail || ui.floatChatOpen);
const started = ref(false);

function ensureStarted() {
  if (!started.value) started.value = true;
  if (typeof rootId.value === "number") ensureControlSession(rootId.value);
}
function toggleExpanded() {
  if (!props.rail) ui.toggleFloatChat();
}

// ── Active workspace anchoring ──
// Re-anchor immediately on every workspace switch. We do NOT defer while a task
// runs: each engaged repo keeps its own ClaudeChat mounted (see mountedManagers),
// so a busy Manager keeps streaming in the background — switching only flips
// which one is visible, it never unmounts/kills the running claude.
const activeWsId = ref<number>(props.wsId);
const activeCwd = ref<string>(props.cwd);

// The Manager is anchored to the ROOT repo, not a worktree. Worktrees are their
// own workspace rows (parent_id set); keying by root keeps one session alive
// across worktree switches instead of an empty one per worktree.
const root = computed(() => {
  const w = wsStore.workspaces.find((x) => x.id === activeWsId.value);
  if (w?.parent_id) {
    const parent = wsStore.workspaces.find((x) => x.id === w.parent_id);
    if (parent) return parent;
  }
  return w ?? null;
});
const rootId = computed(() => root.value?.id ?? activeWsId.value);
const rootCwd = computed(() => root.value?.path ?? activeCwd.value);
const rootName = computed(() => root.value?.name ?? "this repo");

// One persistent Manager session per ROOT repo, reused across open/collapse,
// worktree switches, and app restarts. Keyed by root repo id in localStorage.
const MAP_KEY = "burrow.floatchat.sessions";
function loadMap(): Record<number, number> {
  try { return JSON.parse(localStorage.getItem(MAP_KEY) || "{}"); } catch { return {}; }
}
function saveMap(m: Record<number, number>) {
  localStorage.setItem(MAP_KEY, JSON.stringify(m));
}

// Reactive map of root-repo id → its Manager session id. Drives which chats are
// mounted; seeded from the persisted map for sessions that still exist.
const sessionIdByRepo = ref<Record<number, number>>({});
{
  const map = loadMap();
  for (const [repo, sid] of Object.entries(map)) {
    if (chats.sessions.find((s) => s.id === sid)) sessionIdByRepo.value[Number(repo)] = sid;
  }
}

// One mounted ClaudeChat per engaged repo (kept alive, v-show'd) so switching
// workspaces never tears down a busy Manager.
const mountedManagers = computed(() =>
  Object.entries(sessionIdByRepo.value).map(([repo, sid]) => {
    const id = Number(repo);
    const ws = wsStore.workspaces.find((w) => w.id === id);
    return { repoId: id, sessionId: sid, cwd: ws?.path ?? rootCwd.value };
  }),
);

// The session id anchored to the currently active root repo (if any).
const activeSessionId = computed<number | null>(() =>
  typeof rootId.value === "number" ? sessionIdByRepo.value[rootId.value] ?? null : null,
);

const projectManagerPrompt = ref('');
watch(rootCwd, async (cwd) => {
  if (!cwd) return;
  try {
    await invoke('scaffold_burrow_dir', { workspacePath: cwd, defaultManagerPrompt: getDefaultManagerPrimer(false) });
  } catch { /* ignore — dir may already exist or path invalid */ }
  try {
    const content = await invoke<string>('read_text_file', { path: cwd + '/.burrow/manager.md' });
    const stripped = content.replace(/<!--[\s\S]*?-->/g, '').trim();
    const isPlaceholder = stripped === '# Project-specific Manager instructions' || stripped === '';
    projectManagerPrompt.value = isPlaceholder ? '' : stripped;
  } catch {
    projectManagerPrompt.value = '';
  }
}, { immediate: true });

// Worktree spawn preference (persisted globally) — reflected in the primer.
// NOTE: unlike the brief's assumption, this is a single global boolean (not
// keyed by repo) — the actual per-repo map at MAP_KEY above is the unrelated
// session-id-by-repo map, out of scope for this migration.
const WT_LS_KEY = "burrow.floatchat.worktreeMode";
const WT_CONFIG_KEY = "managerWorktreeMode";
const worktreeMode = ref<boolean>(false);
watch(worktreeMode, (v) => setConfig(WT_CONFIG_KEY, v));
const wtMenuOpen = ref(false);
function selectWorktreeMode(v: boolean) {
  worktreeMode.value = v;
  wtMenuOpen.value = false;
}

// Manager model — its own key, default Sonnet, switchable from the strip.
const MANAGER_MODEL_KEY = "burrow.manager.model";
const MANAGER_MODELS = [
  { id: "claude-sonnet-5", label: "Sonnet 5", note: "Recommended — balanced orchestration" },
  { id: "claude-opus-4-8", label: "Opus 4.8", note: "Strongest judgment — heavy multi-agent work" },
  { id: "claude-haiku-4-5-20251001", label: "Haiku 4.5", note: "Cheapest — simple dispatch only" },
] as const;
const DEFAULT_MANAGER_MODEL = "claude-sonnet-5";
const MANAGER_MODEL_CONFIG_KEY = "managerModel";
function loadManagerModel(): string {
  const v = getConfig<string | null>(MANAGER_MODEL_CONFIG_KEY, null);
  return MANAGER_MODELS.some((m) => m.id === v) ? (v as string) : DEFAULT_MANAGER_MODEL;
}
const managerModel = ref<string>(DEFAULT_MANAGER_MODEL);
const managerModelLabel = computed(
  () => MANAGER_MODELS.find((m) => m.id === managerModel.value)?.label ?? "Model",
);
const mdlMenuOpen = ref(false);
function selectManagerModel(id: string) {
  mdlMenuOpen.value = false;
  if (id === managerModel.value) return;
  managerModel.value = id;
  setConfig(MANAGER_MODEL_CONFIG_KEY, id);
  // Apply live to every mounted Manager chat (they share this model key).
  chatRefs.forEach((c) => (c as { selectModel?: (m: string) => void }).selectModel?.(id));
}

// Permission mode — shared with active chat. Mirrors `claude --permission-mode`.
type PermMode = "default" | "auto" | "acceptEdits" | "plan" | "dontAsk" | "bypassPermissions";
const PERM_META: Record<PermMode, { label: string; title: string; danger?: boolean }> = {
  default: { label: "Ask", title: "Ask before edits & commands" },
  auto: { label: "Auto", title: "Claude decides when to ask" },
  acceptEdits: { label: "Accept Edits", title: "Auto-accept file edits; still ask for other tools" },
  plan: { label: "Plan Mode", title: "Plan only — no edits or commands until you approve" },
  dontAsk: { label: "Don't Ask", title: "Run edits & commands without asking; still blocks dangerous ops" },
  bypassPermissions: { label: "Bypass", title: "Skip ALL permission checks", danger: true },
};
const PERM_MODES: PermMode[] = ["default", "auto", "acceptEdits", "plan", "dontAsk", "bypassPermissions"];
const PERM_ICON: Record<PermMode, unknown> = {
  default: PhShieldCheck,
  auto: PhSparkle,
  acceptEdits: PhPencilSimple,
  plan: PhListChecks,
  dontAsk: PhFastForward,
  bypassPermissions: PhShieldWarning,
};
const permMenuOpen = ref(false);
// config isn't reactive — mirror the active session's perm mode in a ref and
// re-read whenever the active session changes. Config key ("chatPermissionMode")
// and shape match ClaudeChat's exactly (same legacy keys: burrow.claude.permMode.<id> /
// .last), so a mode set here is picked up by the child on mount (loadPermMode)
// and vice-versa. ClaudeChat.vue's own onMounted migration already folds the
// legacy per-chat localStorage keys into this config key; the guard below makes
// this migration idempotent and order-independent with that one.
interface ChatPermissionModeConfig {
  byChat: Record<string, string>;
  last?: string;
  dangerousByChat: Record<string, boolean>;
}
const PERM_CONFIG_KEY = "chatPermissionMode";
const activePermMode = ref<PermMode>("default");
function refreshPermMode() {
  const sid = activeSessionId.value;
  const cfg = getConfig<ChatPermissionModeConfig>(PERM_CONFIG_KEY, { byChat: {}, dangerousByChat: {} });
  const v = sid ? cfg.byChat[String(sid)] : undefined;
  activePermMode.value = (v && (PERM_MODES as string[]).includes(v)) ? (v as PermMode) : "default";
}
watch(activeSessionId, refreshPermMode, { immediate: true });
const activePermMeta = computed(() => PERM_META[activePermMode.value]);

async function selectPermMode(m: PermMode) {
  permMenuOpen.value = false;
  if (m === activePermMode.value) return;
  activePermMode.value = m; // optimistic, reactive UI update
  // Manager owns persistence directly so it works even when the chat isn't mounted
  // (bar collapsed). The child reads the same key via loadPermMode on next mount.
  const sid = activeSessionId.value;
  if (sid) {
    const cfg = { ...getConfig<ChatPermissionModeConfig>(PERM_CONFIG_KEY, { byChat: {}, dangerousByChat: {} }) };
    cfg.byChat = { ...cfg.byChat, [String(sid)]: m };
    cfg.last = m;
    setConfig(PERM_CONFIG_KEY, cfg);
  }
  // If the chat is mounted & running, apply live (restarts claude with the new mode).
  await (chatRefs.get(rootId.value) as any)?.selectPermMode?.(m);
}

// Adopt the active workspace on every switch (no busy guard — the busy repo's
// chat stays mounted hidden, so re-anchoring can't kill it).
watch(
  () => [props.wsId, props.cwd] as const,
  ([wsId, cwd]) => {
    activeWsId.value = wsId;
    activeCwd.value = cwd;
  },
);

function ensureControlSession(repoId: number) {
  const existing = sessionIdByRepo.value[repoId];
  if (existing && chats.sessions.find((s) => s.id === existing)) return;
  const map = loadMap();
  const mapped = map[repoId];
  if (mapped && chats.sessions.find((s) => s.id === mapped)) {
    sessionIdByRepo.value[repoId] = mapped;
    return;
  }
  // create() flips the workspace's active chat; restore it so the in-tab Claude
  // pane isn't yanked to this hidden Manager session.
  const prevActive = chats.activeByWs[repoId];
  const sess = chats.create(repoId);
  chats.sync(sess.id, { title: "Manager", control: true });
  if (prevActive) chats.setActive(repoId, prevActive);
  map[repoId] = sess.id;
  saveMap(map);
  sessionIdByRepo.value[repoId] = sess.id;
}

function resetSession() {
  const repoId = rootId.value;
  if (typeof repoId !== "number") return;
  const map = loadMap();
  delete map[repoId];
  saveMap(map);
  delete sessionIdByRepo.value[repoId];
  ensureControlSession(repoId);
}

// Resolve a session for the active repo only when the Manager is actually
// engaged (bar expanded). Switching while collapsed does NOT spawn a claude.
watch(
  () => [expanded.value, rootId.value] as const,
  ([isOpen, repoId]) => {
    if (isOpen && typeof repoId === "number") {
      started.value = true;
      ensureControlSession(repoId);
    }
  },
  { immediate: true },
);

// The live Manager session row (status/busy mirror the in-tab chat model) for
// the currently active repo.
const session = computed(() =>
  activeSessionId.value === null
    ? null
    : chats.sessions.find((s) => s.id === activeSessionId.value) ?? null,
);
const busy = computed(() => !!session.value?.busy);

// Latch a turn that finished while collapsed so the strip dot flags "done".
const finishedWhileCollapsed = ref(false);
watch(
  () => session.value?.busy,
  (now, prev) => {
    if (prev && !now && !expanded.value) finishedWhileCollapsed.value = true;
    if (prev && !now) {
      // Adopt a workspace switch that was deferred while a task ran.
      activeWsId.value = props.wsId;
      activeCwd.value = props.cwd;
    }
  },
);
watch(expanded, (o) => { if (o) finishedWhileCollapsed.value = false; });

// Strip status dot: permission > waiting > busy > done > idle.
const dotKind = computed<"permission" | "waiting" | "busy" | "done" | "idle">(() => {
  const st = session.value?.status;
  if (st === "permission") return "permission";
  if (st === "waiting") return "waiting";
  if (busy.value) return "busy";
  if (finishedWhileCollapsed.value) return "done";
  return "idle";
});
const dotTitle = computed(() => {
  switch (dotKind.value) {
    case "permission": return "Manager needs a permission decision";
    case "waiting": return "Manager is waiting for your input";
    case "busy": return "Manager is working";
    case "done": return "Manager finished while you were away";
    default: return "Manager — idle";
  }
});

async function quickSend() {
  const text = quickText.value.trim();
  if (!text) return;
  quickText.value = "";
  cmdSuggestions.value = [];
  atSuggestions.value = [];
  const imgs = pastedImages.value.length ? [...pastedImages.value] : undefined;
  pastedImages.value = [];
  nextTick(() => { quickAutoResize(); });
  ensureStarted();
  const repoId = rootId.value;
  await nextTick();
  chatRefs.get(repoId)?.sendMessage(text, imgs);
}

// ── Resizable expanded panel height ──
const HEIGHT_LS_KEY = "burrow.manager.height";
const HEIGHT_CONFIG_KEY = "managerPanelHeight";
function clampHeight(v: number): number {
  return Math.min(Math.max(v || 360, 160), 900);
}
const panelHeight = ref<number>(360);
const isResizing = ref(false);
let startY = 0;
let startH = 0;
function startResize(e: MouseEvent) {
  isResizing.value = true;
  startY = e.clientY;
  startH = panelHeight.value;
  e.preventDefault();
}
function onResizeMove(e: MouseEvent) {
  if (!isResizing.value) return;
  const max = Math.round(window.innerHeight * 0.8);
  panelHeight.value = Math.min(Math.max(startH - (e.clientY - startY), 160), max);
}
function onResizeUp() {
  if (!isResizing.value) return;
  isResizing.value = false;
  setConfig(HEIGHT_CONFIG_KEY, panelHeight.value);
}

// Publish the always-visible strip height so the pet overlay walks ON TOP of
// the Manager row instead of behind it.
const STRIP_H = 38;
function onDocMouseDown(e: MouseEvent) {
  if ((wtMenuOpen.value || mdlMenuOpen.value || permMenuOpen.value) && !(e.target as HTMLElement)?.closest(".mb-wt")) {
    wtMenuOpen.value = false;
    mdlMenuOpen.value = false;
    permMenuOpen.value = false;
  }
}
// Sub-agent "done" auto-collect nudges removed: with the burrow MCP server now
// primary, a Manager drives its own result collection (spawn wait:true returns
// inline; collect_results otherwise), so the injected "Run: burrow collect …"
// message was redundant noise. The backend `agent-done` emit is now unconsumed.

onMounted(async () => {
  window.addEventListener("mousemove", onResizeMove);
  window.addEventListener("mouseup", onResizeUp);
  window.addEventListener("mousedown", onDocMouseDown);
  if (!props.rail) document.documentElement.style.setProperty("--manager-bar-h", `${STRIP_H}px`);
  if (expanded.value && typeof rootId.value === "number") {
    started.value = true;
    ensureControlSession(rootId.value);
  }

  // Config must be loaded (and legacy localStorage migrated) before any of the
  // config-backed refs below are trusted.
  await configReady;

  // managerWorktreeMode: single global boolean, legacy value was "1"/"0" strings
  // (not JSON), so migrate by hand rather than via migrateFromLocalStorage.
  const MISSING = Symbol("missing");
  if (getConfig<unknown>(WT_CONFIG_KEY, MISSING) === MISSING) {
    const raw = localStorage.getItem(WT_LS_KEY);
    if (raw !== null) {
      setConfig(WT_CONFIG_KEY, raw === "1");
      localStorage.removeItem(WT_LS_KEY);
    }
  }
  worktreeMode.value = getConfig<boolean>(WT_CONFIG_KEY, false);

  // managerModel: simple global scalar, 1:1 port.
  migrateFromLocalStorage(MANAGER_MODEL_KEY, MANAGER_MODEL_CONFIG_KEY);
  managerModel.value = loadManagerModel();

  // managerPanelHeight: simple global scalar, 1:1 port.
  migrateFromLocalStorage(HEIGHT_LS_KEY, HEIGHT_CONFIG_KEY);
  panelHeight.value = clampHeight(getConfig<number>(HEIGHT_CONFIG_KEY, 360));

  // chatPermissionMode: shared with ClaudeChat.vue — that component's own
  // onMounted migration already folds the legacy burrow.claude.permMode.<id> /
  // .last keys into this same config key. If no ClaudeChat has mounted yet,
  // run the identical migration here so the Manager still sees a migrated
  // value; the getConfig(...) === MISSING guard makes running it from both
  // places idempotent and order-independent.
  if (getConfig<unknown>(PERM_CONFIG_KEY, MISSING) === MISSING) {
    const rePerm = /^burrow\.claude\.permMode\.(\d+)$/;
    const reDanger = /^burrow\.claude\.dangerous\.(\d+)$/;
    const permLastKey = "burrow.claude.permMode.last";
    const byChat: Record<string, string> = {};
    const dangerousByChat: Record<string, boolean> = {};
    const keys = Object.keys(localStorage).filter(
      (k) => k !== permLastKey && (rePerm.test(k) || reDanger.test(k)),
    );
    for (const k of keys) {
      let m = k.match(rePerm);
      if (m) { const v = localStorage.getItem(k); if (v !== null) byChat[m[1]] = v; continue; }
      m = k.match(reDanger);
      if (m) { dangerousByChat[m[1]] = localStorage.getItem(k) === "1"; continue; }
    }
    const last = localStorage.getItem(permLastKey);
    const cfg: ChatPermissionModeConfig = { byChat, dangerousByChat };
    if (last !== null) cfg.last = last;
    setConfig(PERM_CONFIG_KEY, cfg);
    for (const k of keys) localStorage.removeItem(k);
    if (last !== null) localStorage.removeItem(permLastKey);
  }
  refreshPermMode();
});
onBeforeUnmount(() => {
  window.removeEventListener("mousemove", onResizeMove);
  window.removeEventListener("mouseup", onResizeUp);
  window.removeEventListener("mousedown", onDocMouseDown);
  if (!props.rail) document.documentElement.style.setProperty("--manager-bar-h", "0px");
});


const managerPrimer = computed(() => {
  if (projectManagerPrompt.value) {
    const spawnMode = worktreeMode.value ? SPAWN_MODE_WORKTREE : SPAWN_MODE_BRANCH;
    return projectManagerPrompt.value + '\n\n---\n\n' + spawnMode;
  }
  return getDefaultManagerPrimer(worktreeMode.value);
});
</script>

<style scoped>
.mb-chat :deep(.claude-chat) { background: transparent; backdrop-filter: none; }

.mb-quick::-webkit-scrollbar { display: none; }

.mb-sug-name { color: var(--accent); }

.mb-status-dot { }
.mb-dot-idle { background: var(--text-muted, #475569); }
.mb-dot-busy { background: #4ade80; animation: mb-pulse 1.4s ease-in-out infinite; }
.mb-dot-waiting { background: #3b82f6; }
.mb-dot-permission { background: #f59e0b; animation: mb-pulse 1.2s ease-in-out infinite; }
.mb-dot-done { background: #22c55e; }
@keyframes mb-pulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(74, 222, 128, 0); }
  50% { box-shadow: 0 0 0 4px rgba(74, 222, 128, 0.28); }
}

.mb-wt-item-on > svg:first-child { color: var(--accent); }
.mb-wt-item-danger > svg:first-child { color: var(--red, #ef4444); }
</style>

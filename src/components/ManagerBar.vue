<template>
  <div class="mb-root" :class="{ 'mb-open': expanded }">
    <!-- Drag handle (top border) — resize the expanded panel height -->
    <div
      v-show="expanded"
      class="mb-resize"
      @mousedown="startResize"
    />

    <!-- Expanded panel: animated height wrapper keeps the chat mounted while it
         slides open/closed. Inner panel is fixed-height so content doesn't
         squish mid-animation. Only PAST MESSAGES live here — the composer is in
         the strip below. -->
    <div
      v-if="started"
      class="mb-panel-wrap"
      :style="{ height: (expanded ? panelHeight : 0) + 'px', transition: isResizing ? 'none' : undefined }"
    >
      <div class="mb-panel" :style="{ height: panelHeight + 'px' }">
        <div class="mb-chat">
          <!-- One ClaudeChat per engaged repo, kept mounted and v-show'd. This is
               what lets a busy Manager keep streaming when you switch workspace:
               we flip visibility instead of unmounting (which would claude_stop). -->
          <ClaudeChat
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
    <div class="mb-strip">
      <PhSparkle :size="15" weight="fill" class="mb-strip-icon" />
      <span class="mb-strip-title">Manager</span>
      <span class="mb-status-dot" :class="`mb-dot-${dotKind}`" :title="dotTitle" />
      <span class="mb-strip-sub" :title="rootCwd">{{ rootName }}</span>

      <!-- Quick input with multiline + suggestions -->
      <div class="mb-quick-wrap">
        <!-- /command suggestions -->
        <div v-if="cmdSuggestions.length" class="mb-suggestions">
          <div
            v-for="(s, i) in cmdSuggestions"
            :key="s.name"
            class="mb-suggestion"
            :class="{ 'mb-sug-active': i === cmdIdx }"
            @mousedown.prevent="applyCmd(s.name)"
          >
            <span class="mb-sug-name">/{{ s.name }}</span>
            <span class="mb-sug-desc">{{ s.description }}</span>
          </div>
        </div>
        <!-- @file suggestions -->
        <div v-if="atSuggestions.length" class="mb-suggestions">
          <div
            v-for="(p, i) in atSuggestions"
            :key="p"
            class="mb-suggestion"
            :class="{ 'mb-sug-active': i === atIdx }"
            @mousedown.prevent="applyAt(p)"
          >
            <span class="mb-sug-name">@{{ p.slice(p.lastIndexOf('/') + 1) }}</span>
            <span class="mb-sug-desc">{{ p }}</span>
          </div>
        </div>
        <!-- Pasted image thumbnails -->
        <div v-if="pastedImages.length" class="mb-pasted-imgs">
          <div v-for="(img, i) in pastedImages" :key="i" class="mb-pasted-img-wrap">
            <img :src="img" class="mb-pasted-img" :alt="`Image ${i + 1}`" />
            <button class="mb-pasted-img-rm" title="Remove" @mousedown.prevent="pastedImages.splice(i, 1)">×</button>
          </div>
        </div>
        <textarea
          ref="quickEl"
          v-model="quickText"
          class="mb-quick"
          rows="1"
          :placeholder="busy ? 'Manager is working — queue a message…' : 'Message Manager… (Enter=send, Shift+Enter=newline, @file, /cmd)'"
          @focus="ensureStarted"
          @keydown="onQuickKeydown"
          @input="onQuickInput"
          @paste="onQuickPaste"
        />
      </div>

      <!-- Model picker (Manager has its own model, default Sonnet) -->
      <div class="mb-wt">
        <button
          class="mb-wt-btn"
          title="Manager model"
          @click="mdlMenuOpen = !mdlMenuOpen"
        >
          <PhCpu :size="13" weight="bold" />
          <span class="mb-wt-label">{{ managerModelLabel }}</span>
          <PhCaretUp :size="9" weight="bold" class="mb-wt-caret" />
        </button>
        <div v-if="mdlMenuOpen" class="mb-wt-menu mb-wt-menu-narrow">
          <div class="mb-wt-menu-head">Manager model</div>
          <button
            v-for="m in MANAGER_MODELS"
            :key="m.id"
            class="mb-wt-item"
            :class="{ 'mb-wt-item-on': managerModel === m.id }"
            @click="selectManagerModel(m.id)"
          >
            <PhCpu :size="14" weight="bold" />
            <div class="mb-wt-item-text">
              <span class="mb-wt-item-title">{{ m.label }}</span>
              <span class="mb-wt-item-sub">{{ m.note }}</span>
            </div>
            <PhCheck v-if="managerModel === m.id" :size="13" weight="bold" class="mb-wt-item-check" />
          </button>
        </div>
      </div>

      <!-- Spawn-target picker: clear labeled dropdown (replaces the cryptic
           icon toggle). Tells you where the Manager puts new agents. -->
      <div class="mb-wt">
        <button
          class="mb-wt-btn"
          :title="'Where the Manager spawns new agents'"
          @click="wtMenuOpen = !wtMenuOpen"
        >
          <PhTree v-if="worktreeMode" :size="13" weight="bold" />
          <PhGitBranch v-else :size="13" weight="bold" />
          <span class="mb-wt-label">{{ worktreeMode ? 'New worktree' : 'Current branch' }}</span>
          <PhCaretUp :size="9" weight="bold" class="mb-wt-caret" />
        </button>
        <div v-if="wtMenuOpen" class="mb-wt-menu">
          <div class="mb-wt-menu-head">Spawn agents in…</div>
          <button
            class="mb-wt-item"
            :class="{ 'mb-wt-item-on': !worktreeMode }"
            @click="selectWorktreeMode(false)"
          >
            <PhGitBranch :size="14" weight="bold" />
            <div class="mb-wt-item-text">
              <span class="mb-wt-item-title">Current branch</span>
              <span class="mb-wt-item-sub">Shared working tree — fast, agents see each other's edits</span>
            </div>
            <PhCheck v-if="!worktreeMode" :size="13" weight="bold" class="mb-wt-item-check" />
          </button>
          <button
            class="mb-wt-item"
            :class="{ 'mb-wt-item-on': worktreeMode }"
            @click="selectWorktreeMode(true)"
          >
            <PhTree :size="14" weight="bold" />
            <div class="mb-wt-item-text">
              <span class="mb-wt-item-title">New worktree each</span>
              <span class="mb-wt-item-sub">Isolated branch per agent — safe for parallel work</span>
            </div>
            <PhCheck v-if="worktreeMode" :size="13" weight="bold" class="mb-wt-item-check" />
          </button>
        </div>
      </div>

      <!-- Permission mode switcher -->
      <div class="mb-wt">
        <button
          class="mb-wt-btn"
          :class="{ 'mb-wt-btn-danger': activePermMeta.danger }"
          :title="activePermMeta.title"
          @click="permMenuOpen = !permMenuOpen"
        >
          <component :is="PERM_ICON[activePermMode]" :size="13" weight="bold" />
          <span class="mb-wt-label">{{ activePermMeta.label }}</span>
        </button>
        <div v-if="permMenuOpen" class="mb-wt-menu">
          <div class="mb-wt-menu-head">Permission mode</div>
          <button
            v-for="m in PERM_MODES"
            :key="m"
            class="mb-wt-item"
            :class="{ 'mb-wt-item-on': activePermMode === m, 'mb-wt-item-danger': PERM_META[m].danger }"
            :title="PERM_META[m].title"
            @click="selectPermMode(m)"
          >
            <component :is="PERM_ICON[m]" :size="14" weight="bold" />
            <div class="mb-wt-item-text">
              <span class="mb-wt-item-title">{{ PERM_META[m].label }}</span>
              <span class="mb-wt-item-sub">{{ PERM_META[m].title }}</span>
            </div>
          </button>
        </div>
      </div>

      <button
        class="mb-btn"
        :title="expanded ? 'Collapse Manager (⌘J)' : 'Expand Manager (⌘J)'"
        @click="toggleExpanded"
      >
        <PhCaretDown v-if="expanded" :size="15" weight="bold" />
        <PhCaretUp v-else :size="15" weight="bold" />
      </button>
      <button class="mb-btn" title="Reset Manager session (clears conversation history, starts fresh)" @click="resetSession">
        <PhArrowCounterClockwise :size="15" weight="bold" />
      </button>
      <button class="mb-btn" title="Project config" @click="emit('openProjectConfig')">
        <PhGear :size="15" weight="bold" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from "vue";
import { PhSparkle, PhGitBranch, PhTree, PhCaretDown, PhCaretUp, PhCheck, PhCpu, PhGear, PhArrowCounterClockwise, PhShieldWarning, PhPencilSimple, PhShieldCheck, PhListChecks, PhFastForward } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import ClaudeChat from "./ClaudeChat.vue";
import { useUIStore } from "@/stores/ui";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import { getDefaultManagerPrimer, SPAWN_MODE_WORKTREE, SPAWN_MODE_BRANCH } from "@/utils/managerPrimer";

const props = defineProps<{ cwd: string; wsId: number }>();
const emit = defineEmits<{ openProjectConfig: [] }>();

const ui = useUIStore();
const chats = useClaudeChatsStore();
const wsStore = useWorkspaceStore();

// One live ClaudeChat instance per engaged repo (function refs keyed by repo id).
const chatRefs = new Map<number, InstanceType<typeof ClaudeChat>>();
function setChatRef(repoId: number, el: unknown) {
  if (el) chatRefs.set(repoId, el as InstanceType<typeof ClaudeChat>);
  else chatRefs.delete(repoId);
}
const quickEl = ref<HTMLTextAreaElement | null>(null);
const DRAFT_KEY = computed(() => `burrow.draft.manager.${props.wsId}`);
const quickText = ref(localStorage.getItem(DRAFT_KEY.value) ?? "");
watch(quickText, (val) => {
  if (val) localStorage.setItem(DRAFT_KEY.value, val);
  else localStorage.removeItem(DRAFT_KEY.value);
});
const pastedImages = ref<string[]>([]);

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
  const el = quickEl.value;
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
  const el = quickEl.value;
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
  const el = quickEl.value;
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
    const el = quickEl.value!;
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
const expanded = computed(() => ui.floatChatOpen);
const started = ref(false);

function ensureStarted() {
  if (!started.value) started.value = true;
  if (typeof rootId.value === "number") ensureControlSession(rootId.value);
}
function toggleExpanded() {
  ui.toggleFloatChat();
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
const WT_KEY = "burrow.floatchat.worktreeMode";
const worktreeMode = ref<boolean>(localStorage.getItem(WT_KEY) === "1");
watch(worktreeMode, (v) => localStorage.setItem(WT_KEY, v ? "1" : "0"));
const wtMenuOpen = ref(false);
function selectWorktreeMode(v: boolean) {
  worktreeMode.value = v;
  wtMenuOpen.value = false;
}

// Manager model — its own key, default Sonnet, switchable from the strip.
const MANAGER_MODEL_KEY = "burrow.manager.model";
const MANAGER_MODELS = [
  { id: "claude-sonnet-4-6", label: "Sonnet 4.6", note: "Recommended — balanced orchestration" },
  { id: "claude-opus-4-8", label: "Opus 4.8", note: "Strongest judgment — heavy multi-agent work" },
  { id: "claude-haiku-4-5-20251001", label: "Haiku 4.5", note: "Cheapest — simple dispatch only" },
] as const;
const DEFAULT_MANAGER_MODEL = "claude-sonnet-4-6";
function loadManagerModel(): string {
  const v = localStorage.getItem(MANAGER_MODEL_KEY);
  return MANAGER_MODELS.some((m) => m.id === v) ? (v as string) : DEFAULT_MANAGER_MODEL;
}
const managerModel = ref<string>(loadManagerModel());
const managerModelLabel = computed(
  () => MANAGER_MODELS.find((m) => m.id === managerModel.value)?.label ?? "Model",
);
const mdlMenuOpen = ref(false);
function selectManagerModel(id: string) {
  mdlMenuOpen.value = false;
  if (id === managerModel.value) return;
  managerModel.value = id;
  localStorage.setItem(MANAGER_MODEL_KEY, id);
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
// localStorage isn't reactive — mirror the active session's perm mode in a ref and
// re-read whenever the active session changes. Keys match ClaudeChat's so a mode set
// here is picked up by the child on mount (loadPermMode) and vice-versa.
const PERM_KEY = (id: number) => `burrow.claude.permMode.${id}`;
const PERM_LAST_KEY = "burrow.claude.permMode.last";
const activePermMode = ref<PermMode>("default");
function refreshPermMode() {
  const sid = activeSessionId.value;
  const v = sid ? localStorage.getItem(PERM_KEY(sid)) : null;
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
    localStorage.setItem(PERM_KEY(sid), m);
    localStorage.setItem(PERM_LAST_KEY, m);
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
const HEIGHT_KEY = "burrow.manager.height";
const panelHeight = ref<number>(
  Math.min(Math.max(parseInt(localStorage.getItem(HEIGHT_KEY) ?? "360", 10) || 360, 160), 900),
);
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
  localStorage.setItem(HEIGHT_KEY, String(panelHeight.value));
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
// ── Sub-agent done notifications ─────────────────────────────────────────────
// When `burrow capture TOKEN` finishes, the hook server emits "agent-done".
// Collect tokens for 500 ms then inject a single message into the Manager so it
// can run `burrow collect` without the user having to prompt it.
const pendingDoneTokens: string[] = [];
let doneDebounce: ReturnType<typeof setTimeout> | null = null;
let unlistenAgentDone: (() => void) | null = null;

function flushDoneTokens() {
  const tokens = pendingDoneTokens.splice(0);
  if (!tokens.length) return;
  const repoId = rootId.value;
  if (typeof repoId !== "number") return;
  ensureStarted();
  const list = tokens.map((t) => `\`${t}\``).join(", ");
  const cmd = `burrow collect ${tokens.join(" ")}`;
  const msg = `Sub-agent${tokens.length > 1 ? "s" : ""} finished: ${list}. Run: \`${cmd}\``;
  chatRefs.get(repoId)?.sendMessage(msg);
}

// Resolve a spawning workspace's path to its ROOT repo id (same climb as
// `root` above), so a token only gets injected into the Manager whose repo
// actually spawned it — not every open Manager.
function rootIdForWsPath(path: string): number | null {
  const w = wsStore.workspaces.find((x) => x.path === path);
  if (!w) return null;
  if (w.parent_id) {
    const parent = wsStore.workspaces.find((x) => x.id === w.parent_id);
    if (parent) return parent.id;
  }
  return w.id;
}

listen<{ token: string; originWs?: string }>("agent-done", (e) => {
  const { token, originWs } = e.payload;
  // Unknown origin (e.g. token recorded before an app restart) still injects,
  // matching pre-fix behavior rather than silently dropping the notification.
  if (originWs != null && rootIdForWsPath(originWs) !== rootId.value) return;
  if (token && !pendingDoneTokens.includes(token)) pendingDoneTokens.push(token);
  if (doneDebounce) clearTimeout(doneDebounce);
  doneDebounce = setTimeout(flushDoneTokens, 500);
}).then((fn) => { unlistenAgentDone = fn; });

onMounted(() => {
  window.addEventListener("mousemove", onResizeMove);
  window.addEventListener("mouseup", onResizeUp);
  window.addEventListener("mousedown", onDocMouseDown);
  document.documentElement.style.setProperty("--manager-bar-h", `${STRIP_H}px`);
  if (expanded.value && typeof rootId.value === "number") {
    started.value = true;
    ensureControlSession(rootId.value);
  }
});
onBeforeUnmount(() => {
  window.removeEventListener("mousemove", onResizeMove);
  window.removeEventListener("mouseup", onResizeUp);
  window.removeEventListener("mousedown", onDocMouseDown);
  document.documentElement.style.setProperty("--manager-bar-h", "0px");
  unlistenAgentDone?.();
  if (doneDebounce) clearTimeout(doneDebounce);
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
.mb-root {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--border, rgba(255, 255, 255, 0.1));
  /* Opaque backing even under translucent themes. */
  background-color: var(--bg-base, #0d0d0d);
  background-image: linear-gradient(var(--bg-panel, #111111), var(--bg-panel, #111111));
  position: relative;
  z-index: 30;
}

/* ── Resize handle ── */
.mb-resize {
  height: 5px;
  margin-top: -3px;
  cursor: row-resize;
  flex-shrink: 0;
}
.mb-resize:hover { background: var(--accent, #3b82f6); opacity: 0.4; }

/* ── Expanded panel ── */
.mb-panel-wrap {
  flex-shrink: 0;
  overflow: hidden;
  transition: height 0.22s cubic-bezier(0.4, 0, 0.2, 1);
}
.mb-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border-bottom: 1px solid var(--border, rgba(255, 255, 255, 0.08));
}
.mb-chat {
  flex: 1;
  min-height: 0;
  background-color: var(--bg-base, #0d0d0d);
}
.mb-chat :deep(.claude-chat) { background: transparent; backdrop-filter: none; }

/* ── Bottom strip ── */
.mb-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 38px;
  padding: 6px 10px;
  flex-shrink: 0;
}
.mb-strip-icon { color: var(--accent, #3b82f6); flex-shrink: 0; }
.mb-strip-title { font-size: 12px; font-weight: 600; color: var(--text-primary, #e2e8f0); flex-shrink: 0; }
.mb-strip-sub {
  font-size: 10px;
  color: var(--text-muted, #64748b);
  flex-shrink: 0;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mb-spacer { flex: 1; }
.mb-quick-wrap {
  flex: 1;
  min-width: 0;
  position: relative;
  background: #1a1a20;
  border: 1px solid rgba(255,255,255,0.10);
  border-radius: 14px;
  overflow: hidden;
  transition: border-color .15s;
}
.mb-quick-wrap:focus-within { border-color: rgba(124,58,237,0.5); }
.mb-quick {
  display: block;
  width: 100%;
  background: transparent;
  border: none;
  color: rgba(255,255,255,0.88);
  font-family: var(--font-ui);
  font-size: 13px;
  line-height: 1.5;
  outline: none;
  padding: 12px 14px 6px;
  resize: none;
  min-height: 40px;
  max-height: 160px;
  overflow-y: auto;
  scrollbar-width: none;
  box-sizing: border-box;
}
.mb-quick::placeholder { color: rgba(255,255,255,0.5); }
.mb-quick::-webkit-scrollbar { display: none; }

/* Suggestions dropdown above the input */
.mb-suggestions {
  position: absolute;
  bottom: calc(100% + 4px);
  left: 0;
  right: 0;
  background: var(--bg-dropdown, #18181c);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
  border-radius: 8px;
  box-shadow: 0 -8px 24px rgba(0, 0, 0, 0.4);
  max-height: 200px;
  overflow-y: auto;
  z-index: 80;
}
.mb-suggestion {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 6px 10px;
  cursor: pointer;
  transition: background 0.1s;
}
.mb-suggestion:hover,
.mb-sug-active { background: rgba(255, 255, 255, 0.06); }
.mb-sug-name {
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 600;
  color: #a78bfa;
  flex-shrink: 0;
  min-width: 90px;
}
.mb-sug-desc {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.38);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Status dot ── */
.mb-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.mb-dot-idle { background: var(--text-muted, #475569); }
.mb-dot-busy { background: #4ade80; animation: mb-pulse 1.4s ease-in-out infinite; }
.mb-dot-waiting { background: #3b82f6; }
.mb-dot-permission { background: #f59e0b; animation: mb-pulse 1.2s ease-in-out infinite; }
.mb-dot-done { background: #22c55e; }
@keyframes mb-pulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(74, 222, 128, 0); }
  50% { box-shadow: 0 0 0 4px rgba(74, 222, 128, 0.28); }
}

/* ── Buttons ── */
.mb-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted, #94a3b8);
  cursor: pointer;
  flex-shrink: 0;
}
.mb-btn:hover { background: var(--bg-hover, rgba(255, 255, 255, 0.08)); color: var(--text-primary, #e2e8f0); }

/* ── Spawn-target picker ── */
.mb-wt { position: relative; flex-shrink: 0; }
.mb-wt-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 8px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
  border-radius: 7px;
  background: transparent;
  color: var(--text-secondary, #94a3b8);
  font-family: var(--font-ui);
  font-size: 11px;
  cursor: pointer;
  white-space: nowrap;
}
.mb-wt-btn:hover { background: var(--bg-hover, rgba(255, 255, 255, 0.08)); color: var(--text-primary, #e2e8f0); }
.mb-wt-label { font-weight: 500; }
.mb-wt-caret { opacity: 0.6; }
.mb-wt-btn-danger { color: #ef4444; }
.mb-wt-btn-danger:hover { background: rgba(239, 68, 68, 0.12); }
.mb-wt-item-danger { color: #ef4444; }
.mb-wt-item-danger:hover { background: rgba(239, 68, 68, 0.1); }
.mb-wt-item-danger > svg:first-child { color: #ef4444; }

.mb-wt-menu {
  position: absolute;
  right: 0;
  bottom: calc(100% + 6px);
  width: 260px;
  padding: 6px;
  background-color: var(--bg-dropdown, var(--bg-panel, #111));
  border: 1px solid var(--border, rgba(255, 255, 255, 0.14));
  border-radius: 10px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.5);
  z-index: 70;
}
.mb-wt-menu-narrow { width: 230px; }
.mb-wt-menu-head {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.07em;
  color: var(--text-muted, #64748b);
  padding: 4px 8px 6px;
}
.mb-wt-item {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 8px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-secondary, #94a3b8);
  cursor: pointer;
  text-align: left;
}
.mb-wt-item:hover { background: var(--bg-hover, rgba(255, 255, 255, 0.07)); }
.mb-wt-item-on { color: var(--text-primary, #e2e8f0); }
.mb-wt-item-on > svg:first-child { color: var(--accent, #3b82f6); }
.mb-wt-item-text { display: flex; flex-direction: column; gap: 2px; flex: 1; min-width: 0; }
.mb-wt-item-title { font-size: 12px; font-weight: 600; color: var(--text-primary, #e2e8f0); }
.mb-wt-item-sub { font-size: 10px; line-height: 1.3; color: var(--text-muted, #64748b); }
.mb-wt-item-check { color: var(--accent, #3b82f6); flex-shrink: 0; }

/* ── Pasted image thumbnails ── */
.mb-pasted-imgs {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  padding: 4px 0 4px;
}
.mb-pasted-img-wrap {
  position: relative;
  flex-shrink: 0;
}
.mb-pasted-img {
  display: block;
  width: 48px;
  height: 48px;
  object-fit: cover;
  border-radius: 6px;
  border: 1px solid var(--border, rgba(255,255,255,0.12));
}
.mb-pasted-img-rm {
  position: absolute;
  top: -5px;
  right: -5px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: rgba(0,0,0,0.7);
  border: none;
  color: #fff;
  font-size: 11px;
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
}
</style>

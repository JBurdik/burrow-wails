<template>
  <div class="welcome">
    <template v-if="target">
      <h1 class="welcome-title">
        What should we build in
        <DropdownMenuRoot>
          <DropdownMenuTrigger as-child>
            <button class="welcome-ws" type="button">
              <img v-if="store.icons[target.parent_id ?? target.id]" class="welcome-ws-icon" :src="store.icons[target.parent_id ?? target.id]" alt="" />
              <PhFolder v-else :size="15" weight="fill" />
              {{ target.worktree_branch || target.name }}
              <PhCaretDown :size="11" weight="bold" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="center" class="max-h-[300px] min-w-[200px] overflow-y-auto hide-scrollbar">
            <DropdownMenuItem
              v-for="repo in store.topLevel"
              :key="repo.id"
              class="text-[11.5px]"
              :class="{ 'text-foreground bg-accent/10': repo.id === target.id }"
              @select="pick(repo)"
            >
              <img v-if="store.icons[repo.id]" class="mr-1.5 h-3.5 w-3.5 shrink-0 rounded-sm object-cover" :src="store.icons[repo.id]" alt="" />
              <PhFolder v-else :size="12" weight="fill" class="mr-1.5 shrink-0 text-accent" />
              {{ repo.name }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenuRoot>?
      </h1>
      <ComposerBox class="welcome-compose">
        <!-- Floats above the box (styles/composer.css) — .welcome-compose is the
             positioned ancestor it anchors to. -->
        <ComposerSuggestions :items="suggestions" :active-index="activeIndex" @pick="completion.apply" />
        <ComposerTextInput
          ref="inputEl"
          v-model="text"
          class="welcome-input composer-input block w-full min-h-[60px]"
          placeholder="Ask for changes, send follow-ups, or attach images"
          :skills="completion.skills.value"
          autofocus
          @input="completion.update"
          @keydown="onComposerKeydown"
          @paste="onPaste"
        />
        <ComposerImages v-model="pendingImages" />
        <template #toolbar>
          <div class="composer-toolbar">
            <div class="composer-pillbar">
              <ModelPicker :agent-id="selectedAgentId" :model-id="selectedModel" :cwd="target.path" @select="onModelSelect" />
              <template v-if="isClaude">
                <ComposerPill
                  :label="selectedEffortLabel"
                  :items="CLAUDE_EFFORTS"
                  :active="selectedEffort"
                  title="Claude reasoning effort"
                  @select="pickEffort"
                />
                <ComposerPill
                  :icon="PERM_ICON[selectedPermMode]"
                  :label="permMeta.label"
                  :items="permItems"
                  :active="selectedPermMode"
                  detailed
                  @select="pickPermMode"
                />
              </template>
              <template v-else-if="isCodex">
                <ComposerPill
                  v-if="codexEfforts.length"
                  :label="codexEffort"
                  :items="codexEffortItems"
                  :active="codexEffort"
                  title="Codex reasoning effort"
                  @select="pickCodexEffort"
                />
                <ComposerPill
                  :icon="PhShieldCheck"
                  :label="codexPermLabel"
                  :items="CODEX_PERM_MODES"
                  :active="codexPermMode"
                  detailed
                  @select="pickCodexPermMode"
                />
              </template>
            </div>
            <div class="composer-sendgroup">
              <ComposerPill
                :icon="launchMode === 'chat' ? PhChatCenteredText : PhTerminal"
                :items="launchItems"
                :active="launchMode"
                :title="`Send as ${launchMode === 'chat' ? 'chat' : 'terminal'}`"
                align="end"
                @select="pickMode"
              />
              <button class="composer-send" type="button" :disabled="!text.trim() || worktreeBusy" @click="submit">
                <PhArrowUp :size="14" weight="bold" />
              </button>
            </div>
          </div>
        </template>
      </ComposerBox>
      <WorkspaceTargetPicker
        :mode="worktreeMode"
        :current-branch="currentBranch"
        :branches="switchableBranches"
        @switch-branch="switchBranch"
        @create-branch="createBranch"
        :detail="worktreeBusy ? 'Creating worktree…' : undefined"
        :base-branch="worktreeMode === 'new' ? currentBranch || 'HEAD' : undefined"
        appearance="attached"
        :disabled="worktreeBusy"
        :error="worktreeError"
        @select-mode="selectWorktreeMode"
      />
    </template>
    <template v-else>
      <PhFolderOpen :size="32" weight="thin" />
      <span>No workspace yet</span>
      <button class="welcome-open-btn" @click="emit('open-folder')">Open Folder…</button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, shallowRef, computed, watch, nextTick, onMounted } from "vue";
import { PhFolder, PhFolderOpen, PhCaretDown, PhArrowUp, PhShieldCheck, PhTerminal, PhChatCenteredText, PhSparkle, PhPencilSimple, PhListChecks, PhFastForward, PhShieldWarning } from "@phosphor-icons/vue";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import { useGitStore } from "@/stores/git";
import { useProvidersStore, binaryFor } from "@/stores/providers";
import { configReady, getConfig, setConfig } from "@/lib/config";
import { invoke } from "@tauri-apps/api/core";
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { modelsFor, effortsFor, defaultEffortFor, ensureModels } from "@/lib/chatModels";
import ModelPicker from "@/components/ModelPicker.vue";
import ComposerBox from "@/components/ComposerBox.vue";
import ComposerTextInput from "@/components/ComposerTextInput.vue";
import ComposerSuggestions from "@/components/composer/ComposerSuggestions.vue";
import ComposerImages from "@/components/composer/ComposerImages.vue";
import ComposerPill, { type ComposerPillItem } from "@/components/composer/ComposerPill.vue";
import { useComposerCompletion } from "@/lib/composerCompletion";
import { buildTerminalCommand, terminalProgramFor } from "@/lib/agentCommand";
import { getProjectSettings } from "@/lib/projectSettings";
import WorkspaceTargetPicker from "@/components/WorkspaceTargetPicker.vue";

const emit = defineEmits<{ (e: "open-folder"): void }>();

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
const git = useGitStore();
const providers = useProvidersStore();

const text = ref("");
const inputEl = ref<InstanceType<typeof ComposerTextInput>>();

// App.vue keeps this screen mounted behind v-show, so the textarea's own
// autofocus only fires once. App re-focuses it whenever the screen reappears.
// Rotate the launching provider (Claude → Codex → Gemini → …) from the
// keyboard, so switching doesn't need the ModelPicker popover. Bound to the
// rebindable "switchProvider" command; App.vue calls this.
function cycleProvider() {
  const list = providers.chatAgents;
  if (list.length < 2) return;
  const idx = list.findIndex((a) => a.id === selectedAgentId.value);
  const next = list[(idx + 1 + list.length) % list.length];
  // Empty model = let the new provider pick its own default (ClaudeChat.vue
  // resolves it once the session exists).
  onModelSelect(next.id, next.kind === "claude" ? getConfig<string>("chatLastUsedModel", modelsFor("claude")[0].id) : "");
}

// @file / $skill completion + skill pills — the same engine the chat composer
// uses (lib/composerCompletion.ts), so the two cannot drift.
const completion = useComposerCompletion({
  text,
  input: () => inputEl.value,
  cwd: () => target.value?.path ?? "",
});
const { suggestions, activeIndex } = completion;

function onComposerKeydown(e: KeyboardEvent) {
  // Completion first: Enter picks the highlighted suggestion instead of sending.
  if (completion.handleKeydown(e)) return;
  if (e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey && !e.isComposing) {
    e.preventDefault();
    submit();
  }
}

const pendingImages = ref<string[]>([]);

function attachImages(files: Iterable<File>) {
  for (const file of files) {
    if (!file.type.startsWith("image/")) continue;
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") pendingImages.value.push(reader.result);
    };
    reader.readAsDataURL(file);
  }
}

function onPaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.items ?? [])
    .filter((item) => item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter((file): file is File => file !== null);
  if (!files.length) return;
  event.preventDefault();
  attachImages(files);
}

async function persistTerminalImages(images: string[]): Promise<string[]> {
  return Promise.all(images.map(async (image) => {
    const match = /^data:image\/([a-z0-9.+-]+);base64,(.+)$/i.exec(image);
    if (!match) throw new Error("Unsupported image format");
    return invoke<string>("save_temp_image", { b64: match[2], ext: match[1] });
  }));
}

function promptWithImagePaths(prompt: string, paths: string[]): string {
  if (!paths.length) return prompt;
  const label = paths.length === 1 ? "image" : "images";
  const newline = String.fromCharCode(10);
  return "Please inspect the attached " + label + " before answering:" + newline + paths.join(newline) + newline + newline + prompt;
}

// Which agent (Claude/Codex/Gemini/…) launches the chat, like T3 Code's model
// switcher — starts on the user's configured default, overridable per-send.
const selectedAgentId = ref(ui.defaultChatAgent);
const selectedAgent = computed(() => providers.resolve(selectedAgentId.value));
const isClaude = computed(() => selectedAgent.value.kind === "claude");

// One popover picks provider + model together (see ModelPicker.vue). Effort and
// permission mode stay native-Claude only — ACP agents own those themselves.
// Config keys match ClaudeChat.vue's global defaults, so the chat we create
// picks the choice straight up.
const selectedModel = ref(getConfig<string>("chatLastUsedModel", modelsFor("claude")[0].id));
function onModelSelect(agentId: string, modelId: string) {
  selectedAgentId.value = agentId;
  selectedModel.value = modelId;
  if (!modelId) return;
  if (providers.resolve(agentId).kind === "claude") {
    setConfig("chatLastUsedModel", modelId);
  } else {
    // ACP / Codex models can only be applied once the session exists, so the
    // new chat picks this up when its selectors arrive (ClaudeChat.vue).
    setConfig("chatAcpLastModel", { ...getConfig<Record<string, string>>("chatAcpLastModel", {}), [agentId]: modelId });
  }
}

const CLAUDE_EFFORTS: ComposerPillItem[] = [
  { id: "low", label: "Low effort" },
  { id: "medium", label: "Medium effort" },
  { id: "high", label: "High effort" },
  { id: "xhigh", label: "Extra high" },
  { id: "max", label: "Max effort" },
];
const selectedEffort = ref(getConfig<string>("chatClaudeEffort", "high"));
const selectedEffortLabel = computed(() => CLAUDE_EFFORTS.find((e) => e.id === selectedEffort.value)?.label ?? "High effort");
function pickEffort(id: string) { selectedEffort.value = id; setConfig("chatClaudeEffort", id); }

type PermMode = "default" | "auto" | "acceptEdits" | "plan" | "dontAsk" | "bypassPermissions";
const PERM_MODES: PermMode[] = ["default", "auto", "acceptEdits", "plan", "dontAsk", "bypassPermissions"];
const PERM_META: Record<PermMode, { label: string; description: string }> = {
  default: { label: "Supervised", description: "Ask before commands and file changes." },
  auto: { label: "Auto", description: "Claude decides which routine actions can proceed." },
  acceptEdits: { label: "Auto-accept edits", description: "Auto-approve edits, ask before other actions." },
  plan: { label: "Plan mode", description: "Plan only until you approve implementation." },
  dontAsk: { label: "Don't ask", description: "Run edits and commands without routine prompts." },
  bypassPermissions: { label: "Full access", description: "Skip all permission checks." },
};
const PERM_ICON: Record<PermMode, unknown> = {
  default: PhShieldCheck,
  auto: PhSparkle,
  acceptEdits: PhPencilSimple,
  plan: PhListChecks,
  dontAsk: PhFastForward,
  bypassPermissions: PhShieldWarning,
};
const permItems = computed<ComposerPillItem[]>(() => PERM_MODES.map((m) => ({
  id: m,
  ...PERM_META[m],
  icon: PERM_ICON[m],
  danger: m === "bypassPermissions",
})));
interface ChatPermissionModeConfig { byChat: Record<string, string>; last?: string; dangerousByChat: Record<string, boolean> }
const selectedPermMode = ref<PermMode>((() => {
  const last = getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} }).last;
  return (PERM_MODES as string[]).includes(last ?? "") ? (last as PermMode) : "default";
})());
const permMeta = computed(() => PERM_META[selectedPermMode.value]);
function pickPermMode(mode: string) {
  selectedPermMode.value = mode as PermMode;
  // ClaudeChat.vue's loadPermMode() falls back to this "last used" value for
  // any chat id it hasn't seen before — the chat we're about to create included.
  const cfg = { ...getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} }) };
  cfg.last = mode;
  setConfig("chatPermissionMode", cfg);
}

// Codex publishes its own reasoning efforts per model and its own approval
// policies, so its pills are driven by the catalog rather than Claude's lists.
// Both are stashed in "chatAcpLast", which AgentChat.restoreAcpSelections()
// applies to the new chat as soon as its session comes up.
const isCodex = computed(() => selectedAgent.value.kind === "codex");
type AcpChatSettings = { mode?: string; model?: string; effort?: string };
function lastAcp(field: keyof AcpChatSettings): string | undefined {
  return getConfig<Record<string, AcpChatSettings>>("chatAcpLast", {})[selectedAgentId.value]?.[field];
}
function saveAcp(field: keyof AcpChatSettings, value: string) {
  const rec = { ...getConfig<Record<string, AcpChatSettings>>("chatAcpLast", {}) };
  rec[selectedAgentId.value] = { ...rec[selectedAgentId.value], [field]: value };
  setConfig("chatAcpLast", rec);
}

// Active workspace, else the most recently opened one, unless the user picked
// a different one from the dropdown.
const override = ref<Workspace | null>(null);
const target = computed<Workspace | null>(
  () => override.value ?? store.active ?? [...store.topLevel].sort((a, b) => (b.last_opened ?? 0) - (a.last_opened ?? 0))[0] ?? null,
);
function pick(repo: Workspace) { override.value = repo; }
// Someone else switching the active project (⌘⇧O's picker, the Sidebar) wins
// over a stale local pick — otherwise the composer kept showing whatever was
// last chosen here.
watch(() => store.active?.id, () => { override.value = null; });

// Exposed so App.vue can point the titlebar/right-panel git surfaces at
// whatever project this screen is targeting — picking a project here doesn't
// touch ws.active (sending the first message does, via store.open() below),
// so without this the branch/commit UI kept showing the previous real
// workspace instead of the one visibly selected in the picker.
defineExpose({ focus: () => inputEl.value?.focus(), cycleProvider, target });

type WorktreeMode = "current" | "new";
const worktreeMode = shallowRef<WorktreeMode>("current");
const worktreeBranch = shallowRef("");
const fetchedBranch = shallowRef("");
// ponytail: the git store already re-reads HEAD for the active cwd on every
// refresh; the one-shot fetch below is only for a target nobody is watching.
const currentBranch = computed(() => {
  const workspace = target.value;
  if (!workspace) return "";
  if (workspace.worktree_branch) return workspace.worktree_branch;
  if (git.cwd === workspace.path && git.branch) return git.branch;
  return fetchedBranch.value;
});
const worktreeBusy = shallowRef(false);
const worktreeError = shallowRef("");

function generatedWorktreeBranch(): string {
  const bytes = new Uint8Array(4);
  crypto.getRandomValues(bytes);
  return `burrow/${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

// A worktree branch named after the task instead of four random bytes, the way
// t3code names its own (generateBranchName). Best-effort: the random name is
// already in hand, so a missing model or a slow answer just keeps it.
async function namedWorktreeBranch(workspace: Workspace, message: string): Promise<string> {
  try {
    const slug = await invoke<string>("generate_branch_name", {
      cwd: workspace.path,
      model: ui.textGenerationModel,
      policy: ui.textGenerationPolicy,
      message,
    });
    return slug ? `burrow/${slug}` : "";
  } catch {
    return "";
  }
}

function selectWorktreeMode(mode: WorktreeMode) {
  worktreeMode.value = mode;
  worktreeError.value = "";
  if (mode === "new" && !worktreeBranch.value) worktreeBranch.value = generatedWorktreeBranch();
}

async function refreshCurrentBranch(workspace: Workspace | null) {
  if (!workspace || workspace.worktree_branch) {
    fetchedBranch.value = "";
    return;
  }
  try {
    const out = await invoke<{ stdout: string; code: number }>("run_git", { cwd: workspace.path, args: ["branch", "--show-current"] });
    fetchedBranch.value = out.code === 0 ? out.stdout.trim() : "";
  } catch {
    fetchedBranch.value = "";
  }
}

// Switching is only safe for the repo the git store points at — that is the
// checkout its switchBranch/createBranch would run in.
const switchableBranches = computed(() =>
  target.value && !target.value.worktree_branch && git.cwd === target.value.path ? git.branches : undefined,
);

async function switchBranch(name: string) {
  try { await git.switchBranch(name); }
  catch (e) { console.error("branch switch failed", e); }
}
async function createBranch(name: string) {
  try { await git.createBranch(name); }
  catch (e) { console.error("branch create failed", e); }
}

function worktreePath(workspace: Workspace, branch: string): string {
  const repo = workspace.path.split("/").filter(Boolean).pop() || "repo";
  const root = getProjectSettings(workspace.parent_id ?? workspace.id).worktreesDir || ui.worktreesDir;
  return `${root}/${repo}/${branch.replaceAll("/", "-")}`;
}

// git.branches (what the branch-switch dropdown lists) is otherwise only
// ever fetched by RightPanel's branch-diff-scope code — never for this
// screen's target — so the picker opened empty and switching a checkout
// silently did nothing.
watch(target, (workspace) => {
  void refreshCurrentBranch(workspace);
  if (workspace && !workspace.worktree_branch) {
    git.setCwd(workspace.path);
    void git.fetchBranches();
  }
}, { immediate: true });

// The catalog is what knows the efforts, and it is only fetched lazily — ask for
// it up front so the pill is there before the user opens the model picker.
watch([selectedAgentId, () => target.value?.path], () => {
  void ensureModels(selectedAgentId.value, selectedAgent.value.kind, target.value?.path ?? "");
}, { immediate: true });

const codexEfforts = computed(() => effortsFor(selectedAgentId.value, selectedModel.value));
const codexEffortItems = computed<ComposerPillItem[]>(() => codexEfforts.value.map((e) => ({ id: e, label: e })));
const codexEffort = computed(() =>
  lastAcp("effort") ?? defaultEffortFor(selectedAgentId.value, selectedModel.value) ?? codexEfforts.value[0] ?? ""
);
function pickCodexEffort(id: string) { saveAcp("effort", id); }

// Mirrors codexModes() in src-wails/acp.go — keep the ids in sync.
const CODEX_PERM_MODES: ComposerPillItem[] = [
  { id: "read-only", label: "Supervised", description: "Ask before commands and file changes.", icon: PhShieldCheck },
  { id: "auto-accept-edits", label: "Auto-accept edits", description: "Auto-approve edits, ask before other actions.", icon: PhPencilSimple },
  { id: "auto", label: "Auto", description: "Codex reviews routine actions automatically; risky actions still ask.", icon: PhSparkle },
  { id: "dontAsk", label: "Don't ask", description: "No approval prompts, still confined to the workspace.", icon: PhFastForward },
  { id: "full-access", label: "Full access", description: "Allow commands and edits without prompts.", icon: PhShieldWarning, danger: true },
];
const codexPermMode = computed(() => lastAcp("mode") ?? "auto");
const codexPermLabel = computed(() => CODEX_PERM_MODES.find((m) => m.id === codexPermMode.value)?.label ?? "Auto");
function pickCodexPermMode(id: string) { saveAcp("mode", id); }

// Chat UI (rich conversation) or a plain PTY running the agent's own CLI. The
// prompt is the same either way — only the surface it lands in differs.
type LaunchMode = "chat" | "terminal";
const launchMode = ref<LaunchMode>(getConfig<LaunchMode>("welcomeLaunchMode", "chat") === "terminal" ? "terminal" : "chat");
function pickMode(m: string) { launchMode.value = m as LaunchMode; setConfig("welcomeLaunchMode", m); }

// App.vue keeps this screen mounted from the very start of boot, before
// read_config resolves — every pill above was seeded from getConfig()'s
// fallback, not the persisted value. Re-sync once config lands.
onMounted(async () => {
  await configReady;
  selectedAgentId.value = ui.defaultChatAgent;
  selectedModel.value = getConfig<string>("chatLastUsedModel", modelsFor("claude")[0].id);
  selectedEffort.value = getConfig<string>("chatClaudeEffort", "high");
  const lastPerm = getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} }).last;
  if ((PERM_MODES as string[]).includes(lastPerm ?? "")) selectedPermMode.value = lastPerm as PermMode;
  launchMode.value = getConfig<LaunchMode>("welcomeLaunchMode", "chat") === "terminal" ? "terminal" : "chat";
});
const terminalProgram = computed(() => terminalProgramFor({ kind: selectedAgent.value.kind, command: binaryFor(selectedAgent.value) }));
const launchItems = computed<ComposerPillItem[]>(() => [
  { id: "chat", label: "Chat UI — rich conversation" },
  { id: "terminal", label: `Terminal — run ${terminalProgram.value} in a PTY` },
]);

// A project can pin its own agent/model (Project Settings → General); the
// app-wide default only applies where it hasn't.
// Keyed on id, not the `target` object itself: a background reload (e.g.
// `workspaces-changed` → store.load()) replaces every Workspace with a fresh
// object even when nothing the user picked actually changed, and watching
// the object would re-fire this and silently stomp the user's manual agent
// pick while they're still composing.
watch(() => target.value?.id, (id) => {
  if (id == null) return;
  const t = target.value;
  if (!t) return;
  const s = getProjectSettings(t.parent_id ?? t.id);
  selectedAgentId.value = s.agentId || ui.defaultChatAgent;
  if (s.modelId) selectedModel.value = s.modelId;
}, { immediate: true });


async function submit() {
  const prompt = text.value.trim();
  let t = target.value;
  if (!prompt || !t) return;
  if (worktreeMode.value === "new") {
    const branch = worktreeBranch.value.trim();
    if (!branch) return;
    worktreeBusy.value = true;
    worktreeError.value = "";
    try {
      // Mirrors t3code: create the worktree under the throwaway hex name
      // immediately (nothing here needs the LLM to have already answered),
      // then rename it to a task-derived name in the background once it's
      // ready — renaming never blocks getting the agent started.
      t = await store.createWorktree(t.id, branch, currentBranch.value || null, worktreePath(t, branch));
    } catch (err) {
      worktreeError.value = err instanceof Error ? err.message : String(err);
      return;
    } finally {
      worktreeBusy.value = false;
    }
    const worktreeId = t.id;
    void namedWorktreeBranch(t, prompt).then((named) => {
      if (named && named !== branch) return store.renameWorktreeBranch(worktreeId, branch, named);
    }).catch(() => {});
  }
  const images = [...pendingImages.value];
  const terminalPrompt = launchMode.value === "terminal" && images.length > 0
    ? promptWithImagePaths(prompt, await persistTerminalImages(images))
    : prompt;
  // Snapshot the user's picks now — store.open(t) below can change `target`'s
  // identity (e.g. after createWorktree's reload) and re-fire the
  // project-settings watch above, which would otherwise stomp these before
  // the deferred open() runs.
  const agentId = selectedAgentId.value;
  const agent = selectedAgent.value;
  const model = selectedModel.value;
  const permMode = selectedPermMode.value;
  const wasOpen = store.opened.some((w) => w.id === t.id);
  store.open(t);
  const open = launchMode.value === "terminal"
    ? () => termTabs.add(t.id, buildTerminalCommand(
        { kind: agent.kind, command: binaryFor(agent), model, permMode },
        terminalPrompt,
      ))
    : () => termTabs.openChat(t.id, undefined, agentId, prompt, images, model);
  wasOpen ? open() : nextTick(open); // freshly-mounted Terminal needs a tick to attach its request watcher
  text.value = "";
  pendingImages.value = [];
  worktreeMode.value = "current";
  worktreeBranch.value = "";
  // Leaving the composer for the tabs is the navigation that ends this screen —
  // it also covers the dashboard case the old explicit setMode("terminal") did.
  ui.closeWelcome();
}
</script>

<style scoped>
.welcome {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-secondary);
  padding: 24px;
  position: relative;
}

.welcome-crumb {
  display: flex;
  align-items: center;
  gap: 5px;
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11px;
  font-weight: 600;
  padding: 3px 6px;
  border-radius: 5px;
  margin-bottom: 6px;
}
.welcome-crumb:hover { color: var(--text-secondary); background: var(--bg-hover); }

.welcome-title {
  font-size: 20px;
  font-weight: 500;
  color: var(--text-primary);
  text-align: center;
  max-width: 560px;
  margin-bottom: 6px;
}
.welcome-title {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 6px;
}
.welcome-ws {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  padding: 2px 6px;
  border-radius: 7px;
  font: inherit;
  color: var(--accent);
  cursor: pointer;
}
.welcome-ws:hover { background: var(--bg-hover); }
.welcome-ws-icon { height: 17px; width: 17px; border-radius: 4px; object-fit: cover; }

.welcome-compose {
  position: relative;
  width: 100%;
  max-width: 560px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 10px 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  z-index: 1;
}

.welcome-input {
  background: none;
  border: none;
  outline: none;
  resize: none;
  color: var(--text-primary);
  font-size: 13px;
  font-family: var(--font-ui);
  line-height: 1.5;
}

.welcome-open-btn {
  background: var(--accent);
  border: none;
  border-radius: 6px;
  color: #fff;
  cursor: pointer;
  font-size: 12px;
  font-weight: 600;
  padding: 7px 14px;
}
.welcome-open-btn:hover { background: var(--accent-dim); }
</style>

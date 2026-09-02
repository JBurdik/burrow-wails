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
        <ComposerTextInput
          ref="inputEl"
          v-model="text"
          class="welcome-input composer-input"
          placeholder="Ask for changes, send follow-ups, or attach images"
          rows="3"
          autofocus
          @keydown.enter.exact.prevent="submit"
          @paste="onPaste"
        />
        <div v-if="pendingImages.length" class="welcome-image-previews">
          <div v-for="(image, index) in pendingImages" :key="image" class="welcome-image-preview">
            <img :src="image" :alt="'Attached image ' + (index + 1)" />
            <button type="button" class="welcome-image-remove" :aria-label="'Remove attached image ' + (index + 1)" @click="pendingImages.splice(index, 1)">
              <PhX :size="11" weight="bold" />
            </button>
          </div>
        </div>
        <template #toolbar>
        <div class="welcome-toolbar">
          <div class="welcome-pillbar">
            <ModelPicker :agent-id="selectedAgentId" :model-id="selectedModel" :cwd="target.path" @select="onModelSelect" />
            <template v-if="isClaude">
              <DropdownMenuRoot>
                <DropdownMenuTrigger as-child>
                  <button class="welcome-pill" type="button">
                    {{ selectedEffortLabel }}
                    <PhCaretDown :size="9" weight="bold" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" side="top" class="min-w-[170px]">
                  <DropdownMenuItem
                    v-for="e in CLAUDE_EFFORTS"
                    :key="e.id"
                    class="text-[11.5px]"
                    :class="{ 'text-foreground bg-accent/10': e.id === selectedEffort }"
                    @select="pickEffort(e.id)"
                  >{{ e.label }}</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenuRoot>
              <DropdownMenuRoot>
                <DropdownMenuTrigger as-child>
                  <button class="welcome-pill" type="button">
                    <PhShieldCheck :size="12" weight="bold" />
                    {{ permMeta.label }}
                    <PhCaretDown :size="9" weight="bold" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" side="top" class="min-w-[170px]">
                  <DropdownMenuItem
                    v-for="m in PERM_MODES"
                    :key="m"
                    class="text-[11.5px]"
                    :class="{ 'text-foreground bg-accent/10': m === selectedPermMode }"
                    @select="pickPermMode(m)"
                  >{{ PERM_META[m].label }}</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenuRoot>
            </template>
            <template v-else-if="isCodex">
              <DropdownMenuRoot v-if="codexEfforts.length">
                <DropdownMenuTrigger as-child>
                  <button class="welcome-pill" type="button">
                    {{ codexEffort }}
                    <PhCaretDown :size="9" weight="bold" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" side="top" class="min-w-[170px]">
                  <DropdownMenuItem
                    v-for="e in codexEfforts"
                    :key="e"
                    class="text-[11.5px]"
                    :class="{ 'text-foreground bg-accent/10': e === codexEffort }"
                    @select="pickCodexEffort(e)"
                  >{{ e }}</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenuRoot>
              <DropdownMenuRoot>
                <DropdownMenuTrigger as-child>
                  <button class="welcome-pill" type="button">
                    <PhShieldCheck :size="12" weight="bold" />
                    {{ codexPermLabel }}
                    <PhCaretDown :size="9" weight="bold" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" side="top" class="min-w-[170px]">
                  <DropdownMenuItem
                    v-for="m in CODEX_PERM_MODES"
                    :key="m.id"
                    class="text-[11.5px]"
                    :class="{ 'text-foreground bg-accent/10': m.id === codexPermMode }"
                    @select="pickCodexPermMode(m.id)"
                  >{{ m.label }}</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenuRoot>
            </template>
          </div>
          <div class="welcome-sendgroup">
            <DropdownMenuRoot>
              <DropdownMenuTrigger as-child>
                <button class="welcome-mode" type="button" :title="`Send as ${launchMode === 'chat' ? 'chat' : 'terminal'}`">
                  <PhChatCenteredText v-if="launchMode === 'chat'" :size="12" />
                  <PhTerminal v-else :size="12" />
                  <PhCaretDown :size="9" weight="bold" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" side="top" class="min-w-[210px]">
                <DropdownMenuItem
                  class="text-[11.5px]"
                  :class="{ 'text-foreground bg-accent/10': launchMode === 'chat' }"
                  @select="pickMode('chat')"
                >Chat UI — rich conversation</DropdownMenuItem>
                <DropdownMenuItem
                  class="text-[11.5px]"
                  :class="{ 'text-foreground bg-accent/10': launchMode === 'terminal' }"
                  @select="pickMode('terminal')"
                >Terminal — run {{ terminalProgram }} in a PTY</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenuRoot>
            <button class="welcome-send" type="button" :disabled="!text.trim()" @click="submit">
              <PhArrowUp :size="14" weight="bold" />
            </button>
          </div>
        </div>
        </template>
      </ComposerBox>
      <WorkspaceTargetPicker
        :mode="worktreeMode"
        :current-branch="currentBranch"
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
import { ref, shallowRef, computed, watch, nextTick } from "vue";
import { PhFolder, PhFolderOpen, PhCaretDown, PhArrowUp, PhShieldCheck, PhTerminal, PhChatCenteredText, PhX } from "@phosphor-icons/vue";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import { useProvidersStore, binaryFor } from "@/stores/providers";
import { getConfig, setConfig } from "@/lib/config";
import { invoke } from "@tauri-apps/api/core";
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { modelsFor, effortsFor, defaultEffortFor, ensureModels } from "@/lib/chatModels";
import ModelPicker from "@/components/ModelPicker.vue";
import ComposerBox from "@/components/ComposerBox.vue";
import ComposerTextInput from "@/components/ComposerTextInput.vue";
import { buildTerminalCommand, terminalProgramFor } from "@/lib/agentCommand";
import { getProjectSettings } from "@/lib/projectSettings";
import WorkspaceTargetPicker from "@/components/WorkspaceTargetPicker.vue";

const emit = defineEmits<{ (e: "open-folder"): void }>();

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
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

defineExpose({ focus: () => inputEl.value?.focus(), cycleProvider });
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

const CLAUDE_EFFORTS = [
  { id: "low", label: "Low effort" },
  { id: "medium", label: "Medium effort" },
  { id: "high", label: "High effort" },
  { id: "xhigh", label: "Extra high" },
  { id: "max", label: "Max effort" },
] as const;
const selectedEffort = ref(getConfig<string>("chatClaudeEffort", "high"));
const selectedEffortLabel = computed(() => CLAUDE_EFFORTS.find((e) => e.id === selectedEffort.value)?.label ?? "High effort");
function pickEffort(id: string) { selectedEffort.value = id; setConfig("chatClaudeEffort", id); }

type PermMode = "default" | "auto" | "acceptEdits" | "plan" | "dontAsk" | "bypassPermissions";
const PERM_MODES: PermMode[] = ["default", "auto", "acceptEdits", "plan", "dontAsk", "bypassPermissions"];
const PERM_META: Record<PermMode, { label: string }> = {
  default: { label: "Ask" },
  auto: { label: "Auto" },
  acceptEdits: { label: "Accept Edits" },
  plan: { label: "Plan Mode" },
  dontAsk: { label: "Don't Ask" },
  bypassPermissions: { label: "Bypass" },
};
interface ChatPermissionModeConfig { byChat: Record<string, string>; last?: string; dangerousByChat: Record<string, boolean> }
const selectedPermMode = ref<PermMode>((() => {
  const last = getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} }).last;
  return (PERM_MODES as string[]).includes(last ?? "") ? (last as PermMode) : "default";
})());
const permMeta = computed(() => PERM_META[selectedPermMode.value]);
function pickPermMode(mode: PermMode) {
  selectedPermMode.value = mode;
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

type WorktreeMode = "current" | "new";
const worktreeMode = shallowRef<WorktreeMode>("current");
const worktreeBranch = shallowRef("");
const currentBranch = shallowRef("");
const worktreeBusy = shallowRef(false);
const worktreeError = shallowRef("");

function generatedWorktreeBranch(): string {
  const bytes = new Uint8Array(4);
  crypto.getRandomValues(bytes);
  return `t3code/${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

function selectWorktreeMode(mode: WorktreeMode) {
  worktreeMode.value = mode;
  worktreeError.value = "";
  if (mode === "new" && !worktreeBranch.value) worktreeBranch.value = generatedWorktreeBranch();
}

async function refreshCurrentBranch(workspace: Workspace | null) {
  if (!workspace) {
    currentBranch.value = "";
    return;
  }
  if (workspace.worktree_branch) {
    currentBranch.value = workspace.worktree_branch;
    return;
  }
  try {
    const out = await invoke<{ stdout: string; code: number }>("run_git", { cwd: workspace.path, args: ["branch", "--show-current"] });
    currentBranch.value = out.code === 0 ? out.stdout.trim() : "";
  } catch {
    currentBranch.value = "";
  }
}

function worktreePath(workspace: Workspace, branch: string): string {
  const repo = workspace.path.split("/").filter(Boolean).pop() || "repo";
  const root = getProjectSettings(workspace.parent_id ?? workspace.id).worktreesDir || ui.worktreesDir;
  return `${root}/${repo}/${branch.replaceAll("/", "-")}`;
}

watch(target, (workspace) => { void refreshCurrentBranch(workspace); }, { immediate: true });

// The catalog is what knows the efforts, and it is only fetched lazily — ask for
// it up front so the pill is there before the user opens the model picker.
watch([selectedAgentId, () => target.value?.path], () => {
  void ensureModels(selectedAgentId.value, selectedAgent.value.kind, target.value?.path ?? "");
}, { immediate: true });

const codexEfforts = computed(() => effortsFor(selectedAgentId.value, selectedModel.value));
const codexEffort = computed(() =>
  lastAcp("effort") ?? defaultEffortFor(selectedAgentId.value, selectedModel.value) ?? codexEfforts.value[0] ?? ""
);
function pickCodexEffort(id: string) { saveAcp("effort", id); }

// Mirrors codexModes() in src-wails/acp.go — keep the ids in sync.
const CODEX_PERM_MODES = [
  { id: "read-only", label: "Read only" },
  { id: "auto", label: "Auto" },
  { id: "dontAsk", label: "Don't Ask" },
  { id: "full-access", label: "Full access" },
] as const;
const codexPermMode = computed(() => lastAcp("mode") ?? "auto");
const codexPermLabel = computed(() => CODEX_PERM_MODES.find((m) => m.id === codexPermMode.value)?.label ?? "Auto");
function pickCodexPermMode(id: string) { saveAcp("mode", id); }

// Chat UI (rich conversation) or a plain PTY running the agent's own CLI. The
// prompt is the same either way — only the surface it lands in differs.
type LaunchMode = "chat" | "terminal";
const launchMode = ref<LaunchMode>(getConfig<LaunchMode>("welcomeLaunchMode", "chat") === "terminal" ? "terminal" : "chat");
function pickMode(m: LaunchMode) { launchMode.value = m; setConfig("welcomeLaunchMode", m); }
const terminalProgram = computed(() => terminalProgramFor({ kind: selectedAgent.value.kind, command: binaryFor(selectedAgent.value) }));

// A project can pin its own agent/model (Project Settings → General); the
// app-wide default only applies where it hasn't.
watch(target, (t) => {
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
      t = await store.createWorktree(t.id, branch, currentBranch.value || null, worktreePath(t, branch));
    } catch (err) {
      worktreeError.value = err instanceof Error ? err.message : String(err);
      return;
    } finally {
      worktreeBusy.value = false;
    }
  }
  const images = [...pendingImages.value];
  const terminalPrompt = launchMode.value === "terminal" && images.length > 0
    ? promptWithImagePaths(prompt, await persistTerminalImages(images))
    : prompt;
  if (ui.mode !== "terminal") ui.setMode("terminal");
  const wasOpen = store.opened.some((w) => w.id === t.id);
  store.open(t);
  const open = launchMode.value === "terminal"
    ? () => termTabs.add(t.id, buildTerminalCommand(
        { kind: selectedAgent.value.kind, command: binaryFor(selectedAgent.value), model: selectedModel.value, permMode: selectedPermMode.value },
        terminalPrompt,
      ))
    : () => termTabs.openChat(t.id, undefined, selectedAgentId.value, prompt, images);
  wasOpen ? open() : nextTick(open); // freshly-mounted Terminal needs a tick to attach its request watcher
  text.value = "";
  pendingImages.value = [];
  worktreeMode.value = "current";
  worktreeBranch.value = "";
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
.welcome-input::placeholder { color: var(--text-muted); }

.welcome-image-previews {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.welcome-image-preview {
  position: relative;
  height: 64px;
  width: 64px;
}

.welcome-image-preview img {
  display: block;
  height: 100%;
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 7px;
  object-fit: cover;
}

.welcome-image-remove {
  position: absolute;
  top: -5px;
  right: -5px;
  display: grid;
  width: 18px;
  height: 18px;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 50%;
  background: var(--bg-panel);
  color: var(--text-secondary);
  cursor: pointer;
}

.welcome-image-remove:hover { color: var(--text-primary); background: var(--bg-hover); }

.welcome-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* Borderless ghost pills that wrap instead of scrolling — no frame, no scrollbar. */
.welcome-pillbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.welcome-pill {
  display: flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 11px;
  font-weight: 500;
  padding: 5px 7px;
  border-radius: 6px;
}
.welcome-pill:hover { color: var(--text-primary); background: var(--bg-hover); }

.welcome-sendgroup {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 2px;
}

.welcome-mode {
  display: flex;
  align-items: center;
  gap: 3px;
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  padding: 5px 6px;
  border-radius: 6px;
}
.welcome-mode:hover { color: var(--text-primary); background: var(--bg-hover); }

.welcome-send {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: var(--accent);
  border: none;
  color: #fff;
  cursor: pointer;
}
.welcome-send:hover:not(:disabled) { background: var(--accent-dim); }
.welcome-send:disabled { opacity: 0.4; cursor: default; }

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

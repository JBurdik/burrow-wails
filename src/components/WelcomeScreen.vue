<template>
  <div class="welcome">
    <template v-if="target">
      <DropdownMenuRoot>
        <DropdownMenuTrigger as-child>
          <button class="welcome-crumb" type="button">
            <PhFolder :size="11" weight="fill" />
            {{ target.name }}
            <PhCaretDown :size="9" weight="bold" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" class="max-h-[300px] min-w-[200px] overflow-y-auto hide-scrollbar">
          <DropdownMenuItem
            v-for="repo in store.topLevel"
            :key="repo.id"
            class="text-[11.5px]"
            :class="{ 'text-foreground bg-accent/10': repo.id === target.id }"
            @select="pick(repo)"
          >{{ repo.name }}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenuRoot>

      <h1 class="welcome-title">What should we build in <span class="welcome-ws">{{ target.worktree_branch || target.name }}</span>?</h1>
      <div class="welcome-compose">
        <textarea
          ref="inputEl"
          v-model="text"
          class="welcome-input composer-input"
          placeholder="Ask for changes, send follow-ups, or attach images"
          rows="3"
          @keydown.enter.exact.prevent="submit"
        />
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
      </div>
    </template>
    <template v-else>
      <PhFolderOpen :size="32" weight="thin" />
      <span>No workspace yet</span>
      <button class="welcome-open-btn" @click="emit('open-folder')">Open Folder…</button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted } from "vue";
import { PhFolder, PhFolderOpen, PhCaretDown, PhArrowUp, PhShieldCheck, PhTerminal, PhChatCenteredText } from "@phosphor-icons/vue";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import { useChatAgentsStore } from "@/stores/chatAgents";
import { getConfig, setConfig } from "@/lib/config";
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { modelsFor } from "@/lib/chatModels";
import ModelPicker from "@/components/ModelPicker.vue";
import { buildTerminalCommand, terminalProgramFor } from "@/lib/agentCommand";

const emit = defineEmits<{ (e: "open-folder"): void }>();

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
const chatAgents = useChatAgentsStore();

const text = ref("");
const inputEl = ref<HTMLTextAreaElement>();

// Which agent (Claude/Codex/Gemini/…) launches the chat, like T3 Code's model
// switcher — starts on the user's configured default, overridable per-send.
const selectedAgentId = ref(ui.defaultChatAgent);
const selectedAgent = computed(() => chatAgents.byId(selectedAgentId.value));
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
  if (chatAgents.byId(agentId).kind === "claude") {
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

// Chat UI (rich conversation) or a plain PTY running the agent's own CLI. The
// prompt is the same either way — only the surface it lands in differs.
type LaunchMode = "chat" | "terminal";
const launchMode = ref<LaunchMode>(getConfig<LaunchMode>("welcomeLaunchMode", "chat") === "terminal" ? "terminal" : "chat");
function pickMode(m: LaunchMode) { launchMode.value = m; setConfig("welcomeLaunchMode", m); }
const terminalProgram = computed(() => terminalProgramFor(selectedAgent.value));

// Active workspace, else the most recently opened one, unless the user picked
// a different one from the dropdown.
const override = ref<Workspace | null>(null);
const target = computed<Workspace | null>(
  () => override.value ?? store.active ?? [...store.topLevel].sort((a, b) => (b.last_opened ?? 0) - (a.last_opened ?? 0))[0] ?? null,
);
function pick(repo: Workspace) { override.value = repo; }

onMounted(() => nextTick(() => inputEl.value?.focus()));

function submit() {
  const prompt = text.value.trim();
  const t = target.value;
  if (!prompt || !t) return;
  if (ui.mode !== "terminal") ui.setMode("terminal");
  const wasOpen = store.opened.some((w) => w.id === t.id);
  store.open(t);
  const open = launchMode.value === "terminal"
    ? () => termTabs.add(t.id, buildTerminalCommand(
        { kind: selectedAgent.value.kind, command: selectedAgent.value.command, model: selectedModel.value, permMode: selectedPermMode.value },
        prompt,
      ))
    : () => termTabs.openChat(t.id, undefined, selectedAgentId.value, prompt);
  wasOpen ? open() : nextTick(open); // freshly-mounted Terminal needs a tick to attach its request watcher
  text.value = "";
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
.welcome-ws { color: var(--accent); }

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

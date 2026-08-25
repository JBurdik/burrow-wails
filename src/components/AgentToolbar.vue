<template>
  <div class="agent-toolbar">
    <div class="agent-btns">
      <button
        v-for="a in store.agents"
        :key="a.id"
        class="agent-btn"
        :style="{ '--agent-color': a.color }"
        :disabled="!a.command.trim()"
        :title="store.commandLine(a) || 'No command set'"
        @click="a.command.trim() && $emit('launch', store.commandLine(a))"
      >
        <component :is="iconFor(a.icon)" :size="12" :style="{ color: a.color }" />
        {{ a.name }}
        <span v-if="a.shortcut" class="agent-kbd">{{ a.shortcut }}</span>
      </button>
      <span v-if="store.agents.length === 0" class="no-agents">No agents configured</span>
    </div>
    <div class="toolbar-end">
      <div class="scripts-wrap">
        <button
          class="scripts-btn"
          :class="{ on: scriptsOpen }"
          title="Run a script"
          @click.stop="scriptsOpen = !scriptsOpen"
        >
          <PhPlayCircle :size="13" />
          <span>Scripts</span>
          <PhCaretDown :size="9" />
        </button>
        <div v-if="scriptsOpen" class="scripts-pop" @click.stop>
          <div class="sp-head">Run script</div>
          <button
            v-for="s in mergedScripts"
            :key="s.id"
            class="sp-row"
            :disabled="!scriptsStore.commandLine(s)"
            :title="scriptsStore.commandLine(s) || 'No steps'"
            @click="runScript(s)"
          >
            <span class="sp-dot" :style="{ background: s.color || '#60a5fa' }" />
            <span class="sp-name">{{ s.name }}</span>
            <code class="sp-cmd">{{ scriptsStore.commandLine(s) || "—" }}</code>
          </button>
          <div v-if="mergedScripts.length === 0" class="sp-empty">
            No scripts. Add some in Settings → Scripts.
          </div>
        </div>
      </div>
      <button class="browser-btn" title="Open browser tab" @click="$emit('open-browser')">
        <PhGlobe :size="13" />
        <span>Browser</span>
      </button>
      <button class="claude-ui-btn" title="Open a new conversation" @click="$emit('open-chat')">
        <PhChatCenteredText :size="13" />
        <span>Chat</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import ClaudeIcon from "@/components/icons/ClaudeIcon.vue";
import GitHubCopilotIcon from "@/components/icons/GitHubCopilotIcon.vue";
import OpenAIIcon from "@/components/icons/OpenAIIcon.vue";
import { useAgentsStore, type AgentIcon } from "@/stores/agents";
import { useScriptsStore, type Script } from "@/stores/scripts";
import { useWorkspaceStore } from "@/stores/workspace";
import { computed, ref, onBeforeUnmount } from "vue";
import { PhCode, PhGitBranch, PhRobot, PhSparkle, PhTerminal, PhGlobe, PhPlayCircle, PhCaretDown, PhChatCenteredText } from "@phosphor-icons/vue";

const iconMap: Record<AgentIcon, unknown> = {
  sparkle: PhSparkle,
  code: PhCode,
  "git-branch": PhGitBranch,
  robot: PhRobot,
  terminal: PhTerminal,
  claude: ClaudeIcon,
  openai: OpenAIIcon,
  "github-copilot": GitHubCopilotIcon,
};
function iconFor(icon: AgentIcon) {
  return iconMap[icon] ?? PhRobot;
}

const emit = defineEmits<{ launch: [cmd: string]; "open-chat": []; "open-browser": [] }>();

const store = useAgentsStore();
const scriptsStore = useScriptsStore();
const wsStore = useWorkspaceStore();

const scriptsOpen = ref(false);
const mergedScripts = computed(() => scriptsStore.scriptsFor(wsStore.active?.path));

function runScript(s: Script) {
  const cmd = scriptsStore.commandLine(s);
  if (cmd) emit("launch", cmd);
  scriptsOpen.value = false;
}

// Close the popover on any outside click.
function onDocClick() { scriptsOpen.value = false; }
document.addEventListener("click", onDocClick);
onBeforeUnmount(() => document.removeEventListener("click", onDocClick));
</script>

<style scoped>
.agent-toolbar {
  display: flex;
  align-items: center;
  height: 36px;
  padding: 0 10px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.agent-btns {
  display: flex;
  align-items: center;
  gap: 5px;
}

.agent-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  border-radius: 5px;
  border: 1px solid color-mix(in srgb, var(--agent-color, var(--accent)) 22%, var(--border));
  background: color-mix(in srgb, var(--agent-color, var(--accent)) 7%, transparent);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 11.5px;
  font-weight: 500;
  font-family: var(--font-ui);
  padding: 4px 9px;
  transition: background .12s, color .12s;
  white-space: nowrap;
}
.agent-btn:hover:not(:disabled) {
  background: color-mix(in srgb, var(--agent-color, var(--accent)) 14%, transparent);
  color: var(--text-primary);
}
.agent-btn:active:not(:disabled) { opacity: 0.75; }
.agent-btn:disabled { opacity: 0.35; cursor: default; }

.no-agents {
  font-size: 11px;
  color: var(--text-muted);
}

.agent-kbd {
  font-family: var(--font-ui);
  font-size: 9px;
  color: var(--text-muted);
  background: color-mix(in srgb, var(--agent-color, var(--accent)) 10%, rgba(255,255,255,0.05));
  border-radius: 3px;
  padding: 1px 4px;
  margin-left: 1px;
  flex-shrink: 0;
}

.toolbar-end {
  margin-left: auto;
  display: flex;
  align-items: center;
}

.toolbar-end {
  gap: 5px;
}

.scripts-wrap { position: relative; }
.scripts-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  border-radius: 5px;
  border: 1px solid color-mix(in srgb, #34d399 18%, var(--border));
  background: color-mix(in srgb, #34d399 6%, transparent);
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11.5px;
  font-weight: 500;
  font-family: var(--font-ui);
  padding: 4px 9px;
  transition: background .12s, color .12s;
}
.scripts-btn :deep(svg) { color: #34d399; }
.scripts-btn:hover, .scripts-btn.on {
  background: color-mix(in srgb, #34d399 13%, transparent);
  color: var(--text-primary);
}

.scripts-pop {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 240px;
  max-width: 420px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
  padding: 5px;
  z-index: 200;
}
.sp-head {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--text-muted);
  padding: 4px 8px 6px;
}
.sp-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: left;
  border: none;
  background: transparent;
  border-radius: 5px;
  padding: 6px 8px;
  cursor: pointer;
  color: var(--text-primary);
}
.sp-row:hover:not(:disabled) { background: color-mix(in srgb, var(--accent) 12%, transparent); }
.sp-row:disabled { opacity: 0.4; cursor: default; }
.sp-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.sp-name { font-size: 12px; font-weight: 500; flex-shrink: 0; }
.sp-cmd {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--text-muted);
  margin-left: auto;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sp-empty { font-size: 11px; color: var(--text-muted); padding: 6px 8px; }

.browser-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  border-radius: 5px;
  border: 1px solid color-mix(in srgb, #60a5fa 18%, var(--border));
  background: color-mix(in srgb, #60a5fa 6%, transparent);
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11.5px;
  font-weight: 500;
  font-family: var(--font-ui);
  padding: 4px 9px;
  transition: background .12s, color .12s;
}
.browser-btn :deep(svg) { color: #60a5fa; }
.browser-btn:hover {
  background: color-mix(in srgb, #60a5fa 13%, transparent);
  color: var(--text-primary);
}

.claude-ui-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  border-radius: 5px;
  border: 1px solid color-mix(in srgb, #d97706 18%, var(--border));
  background: color-mix(in srgb, #d97706 6%, transparent);
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11.5px;
  font-weight: 500;
  font-family: var(--font-ui);
  padding: 4px 9px;
  transition: background .12s, color .12s;
}
.claude-ui-btn :deep(svg) { color: #d97706; }
.claude-ui-btn:hover {
  background: color-mix(in srgb, #d97706 13%, transparent);
  color: var(--text-primary);
}
</style>

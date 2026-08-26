<template>
  <div class="flex h-9 shrink-0 items-center border-b border-border bg-panel px-2.5">
    <div class="flex items-center gap-1.5">
      <button
        v-for="a in store.agents"
        :key="a.id"
        class="agent-btn flex items-center gap-1.5 whitespace-nowrap rounded-[5px] px-2.5 py-1 font-sans text-[11.5px] font-medium text-secondary-foreground transition-colors active:opacity-75 disabled:cursor-default disabled:opacity-35"
        :style="{ '--agent-color': a.color }"
        :disabled="!a.command.trim()"
        :title="store.commandLine(a) || 'No command set'"
        @click="a.command.trim() && $emit('launch', store.commandLine(a))"
      >
        <component :is="iconFor(a.icon)" :size="12" :style="{ color: a.color }" />
        {{ a.name }}
        <span v-if="a.shortcut" class="agent-kbd ml-px shrink-0 rounded-[3px] px-1 py-px font-sans text-[9px] text-muted-foreground">{{ a.shortcut }}</span>
      </button>
      <span v-if="store.agents.length === 0" class="text-[11px] text-muted-foreground">No agents configured</span>
    </div>
    <div class="ml-auto flex items-center gap-1.5">
      <div class="relative">
        <button
          class="flex items-center gap-1.5 rounded-[5px] border border-emerald-400/18 bg-emerald-400/6 px-2.5 py-1 font-sans text-[11.5px] font-medium text-muted-foreground transition-colors [&_svg]:text-emerald-400 hover:bg-emerald-400/13 hover:text-foreground"
          :class="{ 'bg-emerald-400/13 text-foreground': scriptsOpen }"
          title="Run a script"
          @click.stop="scriptsOpen = !scriptsOpen"
        >
          <PhPlayCircle :size="13" />
          <span>Scripts</span>
          <PhCaretDown :size="9" />
        </button>
        <div v-if="scriptsOpen" class="absolute right-0 top-[calc(100%+6px)] z-[200] min-w-[240px] max-w-[420px] rounded-lg border border-border bg-panel p-1.5 shadow-[0_12px_32px_rgba(0,0,0,0.45)]" @click.stop>
          <div class="px-2 pb-1.5 pt-1 text-[10px] font-semibold tracking-wide text-muted-foreground">Run script</div>
          <button
            v-for="s in mergedScripts"
            :key="s.id"
            class="flex w-full items-center gap-2 rounded-[5px] bg-transparent px-2 py-1.5 text-left text-foreground hover:bg-accent/12 disabled:cursor-default disabled:opacity-40"
            :disabled="!scriptsStore.commandLine(s)"
            :title="scriptsStore.commandLine(s) || 'No steps'"
            @click="runScript(s)"
          >
            <span class="h-2 w-2 shrink-0 rounded-full" :style="{ background: s.color || '#60a5fa' }" />
            <span class="shrink-0 text-xs font-medium">{{ s.name }}</span>
            <code class="ml-auto truncate font-mono text-[10.5px] text-muted-foreground">{{ scriptsStore.commandLine(s) || "—" }}</code>
          </button>
          <div v-if="mergedScripts.length === 0" class="px-2 py-1.5 text-[11px] text-muted-foreground">
            No scripts. Add some in Settings → Scripts.
          </div>
        </div>
      </div>
      <button class="flex items-center gap-1.5 rounded-[5px] border border-blue-400/18 bg-blue-400/6 px-2.5 py-1 font-sans text-[11.5px] font-medium text-muted-foreground transition-colors [&_svg]:text-blue-400 hover:bg-blue-400/13 hover:text-foreground" title="Open browser tab" @click="$emit('open-browser')">
        <PhGlobe :size="13" />
        <span>Browser</span>
      </button>
      <button class="flex items-center gap-1.5 rounded-[5px] border border-amber-600/18 bg-amber-600/6 px-2.5 py-1 font-sans text-[11.5px] font-medium text-muted-foreground transition-colors [&_svg]:text-amber-600 hover:bg-amber-600/13 hover:text-foreground" title="Open a new conversation" @click="$emit('open-chat')">
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
/* Per-agent accent color is set at runtime via --agent-color (a.color) and
   mixed with the theme accent/border — genuinely dynamic per-instance
   theming that color-mix() can't express as static Tailwind classes. */
.agent-btn {
  border: 1px solid color-mix(in srgb, var(--agent-color, var(--accent)) 22%, var(--border));
  background: color-mix(in srgb, var(--agent-color, var(--accent)) 7%, transparent);
}
.agent-btn:hover:not(:disabled) {
  background: color-mix(in srgb, var(--agent-color, var(--accent)) 14%, transparent);
  color: var(--text-primary);
}
.agent-kbd {
  background: color-mix(in srgb, var(--agent-color, var(--accent)) 10%, rgba(255,255,255,0.05));
}
</style>

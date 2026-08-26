<template>
  <div class="welcome">
    <template v-if="target">
      <h1 class="welcome-title">What should we build in <span class="welcome-ws">{{ target.worktree_branch || target.name }}</span>?</h1>
      <div class="welcome-compose">
        <textarea
          ref="inputEl"
          v-model="text"
          class="welcome-input"
          placeholder="Ask for changes, send follow-ups, or attach images"
          rows="3"
          @keydown.enter.exact.prevent="submit"
        />
        <div class="welcome-toolbar">
          <button class="welcome-target" type="button" @click.stop="pickerOpen = !pickerOpen">
            <PhFolder :size="12" weight="fill" />
            {{ target.name }}
            <PhCaretDown :size="10" weight="bold" />
          </button>
          <button class="welcome-send" type="button" :disabled="!text.trim()" @click="submit">
            <PhArrowUp :size="14" weight="bold" />
          </button>
        </div>
        <div v-if="pickerOpen" class="welcome-picker" @click.stop>
          <button
            v-for="repo in store.topLevel"
            :key="repo.id"
            class="welcome-picker-row"
            :class="{ active: repo.id === target.id }"
            @click="pick(repo)"
          >{{ repo.name }}</button>
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
import { PhFolder, PhFolderOpen, PhCaretDown, PhArrowUp } from "@phosphor-icons/vue";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";

const emit = defineEmits<{ (e: "open-folder"): void }>();

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();

const text = ref("");
const pickerOpen = ref(false);
const inputEl = ref<HTMLTextAreaElement>();

// Active workspace, else the most recently opened one, unless the user picked
// a different one from the dropdown.
const override = ref<Workspace | null>(null);
const target = computed<Workspace | null>(
  () => override.value ?? store.active ?? [...store.topLevel].sort((a, b) => (b.last_opened ?? 0) - (a.last_opened ?? 0))[0] ?? null,
);
function pick(repo: Workspace) { override.value = repo; pickerOpen.value = false; }

onMounted(() => nextTick(() => inputEl.value?.focus()));

function submit() {
  const prompt = text.value.trim();
  const t = target.value;
  if (!prompt || !t) return;
  if (ui.mode !== "terminal") ui.setMode("terminal");
  const wasOpen = store.opened.some((w) => w.id === t.id);
  store.open(t);
  const open = () => termTabs.openChat(t.id, undefined, undefined, prompt);
  wasOpen ? open() : nextTick(open); // freshly-mounted Terminal needs a tick to attach its request watcher
  text.value = "";
}
</script>

<style scoped>
.welcome {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: var(--text-secondary);
  padding: 24px;
}

.welcome-title {
  font-size: 20px;
  font-weight: 500;
  color: var(--text-primary);
  text-align: center;
  max-width: 560px;
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
  gap: 6px;
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
  justify-content: space-between;
}

.welcome-target {
  display: flex;
  align-items: center;
  gap: 5px;
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11px;
  padding: 3px 4px;
  border-radius: 5px;
}
.welcome-target:hover { color: var(--text-secondary); background: var(--bg-hover); }

.welcome-send {
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

.welcome-picker {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 0;
  min-width: 200px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 4px;
  box-shadow: 0 14px 36px rgba(0, 0, 0, 0.55);
  z-index: 10;
}
.welcome-picker-row {
  display: block;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 11.5px;
  padding: 6px 8px;
  border-radius: 5px;
}
.welcome-picker-row:hover { background: var(--bg-hover); color: var(--text-primary); }
.welcome-picker-row.active { background: color-mix(in srgb, var(--accent) 10%, transparent); color: var(--text-primary); }

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

<template>
  <nav class="activity-bar">
    <button
      class="ab-btn"
      :class="{ active: ui.mode === 'dashboard' }"
      title="Dashboard"
      @click="ui.toggleDashboard()"
    >
      <PhSquaresFour :size="18" />
    </button>
    <div class="ab-sep" />
    <button
      class="ab-btn"
      title="New terminal (⌘T)"
      @click="newTerminal()"
    >
      <PhTerminal :size="18" />
    </button>
    <button
      class="ab-btn"
      :class="{ active: ui.mode === 'git' }"
      title="Git panel"
      @click="ui.toggleGitPanel()"
    >
      <PhGitBranch :size="18" />
    </button>
    <button
      class="ab-btn"
      :class="{ active: ui.boardRepoId === ws.active?.id }"
      title="Kanban board"
      :disabled="!ws.active"
      @click="ws.active && ui.openBoard(ws.active.id)"
    >
      <PhKanban :size="18" />
    </button>
  </nav>
</template>

<script setup lang="ts">
import { PhTerminal, PhGitBranch, PhKanban, PhSquaresFour } from "@phosphor-icons/vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";

const ws = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();

function newTerminal() {
  // From any non-terminal view (git/mission), the terminal icon returns to the
  // terminal first instead of silently adding a hidden tab.
  if (ui.mode !== 'terminal') { ui.setMode('terminal'); return; }
  if (ws.active) termTabs.add(ws.active.id);
}
</script>

<style scoped>
.activity-bar {
  width: 44px;
  flex: 0 0 44px;
  flex-shrink: 0;
  background: var(--bg-panel);
  backdrop-filter: var(--blur-panels, none);
  -webkit-backdrop-filter: var(--blur-panels, none);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 8px 0;
  gap: 2px;
}

.ab-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 8px;
  transition: color .12s, background .12s;
  position: relative;
}
.ab-sep {
  width: 22px;
  height: 1px;
  background: var(--border);
  margin: 4px 0;
}
.ab-btn:hover { color: var(--text-primary); background: var(--bg-hover); }
.ab-btn.active {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 12%, transparent);
}
</style>

<template>
  <nav
    class="flex w-11 shrink-0 flex-col items-center gap-0.5 border-r border-border bg-panel py-2 [backdrop-filter:var(--blur-panels,none)]"
  >
    <button
      class="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-hover hover:text-foreground"
      :class="{ 'bg-accent/12 text-accent hover:bg-accent/12 hover:text-accent': ui.mode === 'dashboard' }"
      title="Dashboard"
      @click="ui.toggleDashboard()"
    >
      <PhSquaresFour :size="18" />
    </button>
    <Separator class="my-1 w-[22px]" />
    <button
      class="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-hover hover:text-foreground"
      title="New terminal (⌘T)"
      @click="newTerminal()"
    >
      <PhTerminal :size="18" />
    </button>
    <button
      class="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-hover hover:text-foreground"
      :class="{ 'bg-accent/12 text-accent hover:bg-accent/12 hover:text-accent': ui.mode === 'git' }"
      title="Git panel"
      @click="ui.toggleGitPanel()"
    >
      <PhGitBranch :size="18" />
    </button>
    <button
      class="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
      :class="{ 'bg-accent/12 text-accent hover:bg-accent/12 hover:text-accent': ui.boardRepoId === ws.active?.id }"
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
import { Separator } from "@/components/ui/separator";
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

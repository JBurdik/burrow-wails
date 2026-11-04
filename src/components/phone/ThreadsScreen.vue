<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { PhChatCircle, PhMagnifyingGlass, PhPlus, PhTerminalWindow } from "@phosphor-icons/vue";
import { router } from "@/router";
import { useUIStore } from "@/stores/ui";
import { useWorkspaceStore } from "@/stores/workspace";
import { feedGroups, needsYou, permTarget, useThreadFeed, type FeedRow } from "@/lib/threadFeed";
import type { TermStatus } from "@/lib/terminalStatus";
import PhoneHeader from "./PhoneHeader.vue";
import { timeAgo } from "./timeAgo";

// The phone's home: every active thread of every project, grouped by what it
// needs from you (src/lib/threadFeed.ts — the tray panel's model). It replaced
// the desktop Sidebar here and the Inbox tab, which only saw the one mounted
// project. Settled threads live behind search.
const ui = useUIStore();
const ws = useWorkspaceStore();
const { rows, now, permsFor, answerPerm } = useThreadFeed();

const FILTER_KEY = "burrow.phone.project";
const filter = ref<number | null>(Number(localStorage.getItem(FILTER_KEY)) || null);
watch(filter, (id) => (id ? localStorage.setItem(FILTER_KEY, String(id)) : localStorage.removeItem(FILTER_KEY)));

/** Repos that have something on the list, by name; a stale filter shows "All". */
const projects = computed(() => {
  const ids = new Set(rows.value.map((r) => r.repoId));
  return ws.workspaces.filter((w) => ids.has(w.id)).sort((a, b) => a.name.localeCompare(b.name));
});
const activeFilter = computed(() => (projects.value.some((p) => p.id === filter.value) ? filter.value : null));
const groups = computed(() => {
  const shown = activeFilter.value == null ? rows.value : rows.value.filter((r) => r.repoId === activeFilter.value);
  return feedGroups(shown).filter((g) => g.rows.length);
});

function rightLabel(r: FeedRow): string {
  switch (r.status) {
    case "permission": return "Needs input";
    case "waiting": return "Waiting";
    case "error": return "Error";
    default: return timeAgo(r.at, now.value);
  }
}
const dot: Partial<Record<TermStatus, string>> = {
  running: "bg-accent animate-pulse",
  permission: "bg-[var(--yellow)]",
  waiting: "bg-[var(--yellow)]",
  error: "bg-destructive",
  review: "bg-[var(--blue)]",
  done: "bg-[var(--blue)]",
};
const subtitle = (r: FeedRow) => [activeFilter.value == null ? r.workspace : "", r.detail || r.recap].filter(Boolean).join(" · ");
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-base">
    <PhoneHeader title="Threads">
      <template #actions>
        <button class="phone-icon-btn" aria-label="Search threads" @click="ui.openSpotlight()"><PhMagnifyingGlass :size="21" /></button>
        <button class="phone-icon-btn" aria-label="New thread" @click="ui.openWelcome()"><PhPlus :size="22" /></button>
      </template>
    </PhoneHeader>

    <nav v-if="projects.length > 1" class="project-chips flex shrink-0 gap-1.5 overflow-x-auto border-b border-border px-3 py-2" aria-label="Project">
      <button class="chip" :class="activeFilter == null && 'chip--on'" @click="filter = null">All</button>
      <button v-for="p in projects" :key="p.id" class="chip" :class="activeFilter === p.id && 'chip--on'" @click="filter = p.id">
        <img v-if="ws.icons[p.id]" :src="ws.icons[p.id]" class="size-4 rounded-[4px] object-cover" alt="" />
        {{ p.name }}
      </button>
    </nav>

    <div class="flex-1 overflow-y-auto">
      <div v-if="!groups.length" class="px-6 py-16 text-center text-[14px] text-muted-foreground">
        No active threads.
        <button class="mt-3 block w-full text-accent" @click="ui.openSpotlight()">Search older threads</button>
      </div>
      <section v-for="g in groups" :key="g.key">
        <h2 class="px-4 pb-1 pt-4 text-[12px] font-semibold text-muted-foreground">{{ g.label }}</h2>
        <div v-for="r in g.rows" :key="r.key" class="border-b border-border/60">
          <button class="flex min-h-[60px] w-full items-center gap-3 px-4 py-2 text-left active:bg-hover" @click="router.push(r.route)">
            <span class="relative flex size-6 shrink-0 items-center justify-center">
              <img v-if="r.icon" :src="r.icon" class="size-6 rounded-[6px] object-cover" alt="" />
              <component :is="r.kind === 'chat' ? PhChatCircle : PhTerminalWindow" v-else :size="21" class="text-secondary-foreground" />
              <span v-if="dot[r.status]" class="absolute -right-0.5 -top-0.5 size-2.5 rounded-full ring-2 ring-[var(--bg-base)]" :class="dot[r.status]" />
            </span>
            <span class="flex min-w-0 flex-1 flex-col">
              <span class="truncate text-[15px] text-foreground">{{ r.title }}</span>
              <span v-if="subtitle(r)" class="truncate text-[12.5px] text-muted-foreground">{{ subtitle(r) }}</span>
            </span>
            <span class="shrink-0 text-[12px]" :class="needsYou(r.status) ? (r.status === 'error' ? 'text-destructive' : 'text-[var(--yellow)]') : 'text-muted-foreground'">{{ rightLabel(r) }}</span>
          </button>
          <!-- A terminal agent's tool permission, answered here (ptypermissions.go); a chat's opens the thread. -->
          <div v-for="req in permsFor(r)" :key="req.id" class="mx-4 mb-2 ml-[52px] rounded-lg border border-border px-3 py-2">
            <div class="truncate font-mono text-[12px] text-secondary-foreground">{{ req.tool_name }}<template v-if="permTarget(req)">: {{ permTarget(req) }}</template></div>
            <div class="mt-2 flex gap-2">
              <button class="perm-btn bg-accent text-accent-foreground" @click="answerPerm(req, 'allow')">Allow</button>
              <button class="perm-btn border border-border" @click="answerPerm(req, 'always')">Always</button>
              <button class="perm-btn border border-border text-destructive" @click="answerPerm(req, 'deny')">Deny</button>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.project-chips { scrollbar-width: none; }
.chip {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  font-size: 13px;
  color: var(--text-secondary);
  white-space: nowrap;
}
.chip--on { border-color: var(--accent); color: var(--text-primary); background: color-mix(in srgb, var(--accent) 14%, transparent); }
.perm-btn { flex: 1; height: 36px; border-radius: 8px; font-size: 14px; font-weight: 500; }
.perm-btn:active { opacity: 0.8; }
</style>

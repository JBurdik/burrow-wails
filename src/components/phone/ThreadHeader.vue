<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute } from "vue-router";
import { PhDotsThreeVertical } from "@phosphor-icons/vue";
import { router, routeId } from "@/router";
import { useWorkspaceStore } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { chatSession } from "@/lib/chatSession";
import PhoneHeader from "./PhoneHeader.vue";
import PhoneSheet from "./PhoneSheet.vue";

const route = useRoute();
const ws = useWorkspaceStore();
const tabs = useTerminalTabsStore();
const menu = ref(false);

const tab = computed(() => {
  const wsId = routeId(route.params.wsId);
  if (wsId === null) return null;
  const id = routeId(route.params.tabId) ?? tabs.activeByWs[wsId];
  return tabs.tabsByWs[wsId]?.find((t) => t.id === id) ?? null;
});
const title = computed(() => (route.name === "new" ? "New thread" : tab.value?.title ?? "Thread"));
const subtitle = computed(() => {
  const parts = [ws.active?.name, tab.value?.branch].filter(Boolean);
  return parts.join(" · ");
});

// Only chats have a session to drive; a terminal tab gets no menu.
function act(fn: "interrupt" | "restart" | "clear") {
  const id = tab.value?.chatId;
  menu.value = false;
  if (id == null) return;
  if (fn === "clear" && !confirm("Clear this thread's transcript and start a fresh session?")) return;
  void chatSession(id)[fn]();
}
</script>

<template>
  <PhoneHeader :title="title" :subtitle="subtitle" back>
    <template #actions>
      <button v-if="tab?.chatId != null" class="phone-icon-btn" aria-label="Thread actions" @click="menu = true">
        <PhDotsThreeVertical :size="22" weight="bold" />
      </button>
    </template>
  </PhoneHeader>
  <PhoneSheet v-if="menu" :title="title" @close="menu = false">
    <button class="sheet-row" @click="act('interrupt')">Stop the running turn</button>
    <button class="sheet-row" @click="act('restart')">Restart agent</button>
    <button class="sheet-row" @click="menu = false; router.push('/project/agents')">Sub-agents</button>
    <button class="sheet-row text-destructive" @click="act('clear')">Clear thread</button>
  </PhoneSheet>
</template>

<style scoped>
.sheet-row { display: flex; align-items: center; width: 100%; min-height: 50px; padding: 0 16px; font-size: 15px; text-align: left; }
.sheet-row:not(.text-destructive) { color: var(--text-primary); }
.sheet-row:active { background: var(--bg-hover); }
</style>

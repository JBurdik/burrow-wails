<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRemoteStore } from "../store";
import { Select } from "@/components/ui/select";
import { STATUS_PRIORITY } from "@/lib/terminalStatus";

const store = useRemoteStore();
const sortedChats = computed(() =>
  [...store.chats].sort((a, b) => {
    const pa = STATUS_PRIORITY.indexOf(store.chatStatus(a));
    const pb = STATUS_PRIORITY.indexOf(store.chatStatus(b));
    return pa !== pb ? pa - pb : b.id - a.id;
  })
);
const liveChats = computed(() => sortedChats.value.filter((c) => store.chatStatus(c) !== "idle"));
const settledChats = computed(() => sortedChats.value.filter((c) => store.chatStatus(c) === "idle"));
const showSettled = ref(false);
const creating = ref(false);
const workspaceId = ref<number | null>(null);
const createError = ref("");
const workspaceOptions = computed(() => store.workspaces.map((workspace) => ({ value: String(workspace.id), label: workspace.name })));
const workspaceIdModel = computed<string | undefined>({ get: () => workspaceId.value?.toString(), set: (id) => { workspaceId.value = id ? Number(id) : null; } });

async function createChat() {
  if (!workspaceId.value) return;
  creating.value = true; createError.value = "";
  try { await store.createChat(workspaceId.value, "claude"); }
  catch (error: any) { createError.value = error?.message ?? "Chat se nepodařilo vytvořit."; }
  finally { creating.value = false; }
}

onMounted(() => { if (!store.chats.length) store.loadChats(); });
</script>

<template>
  <header class="m-nav">
    <button class="m-nav-back" type="button" @click="store.showDashboard">‹ Přehled</button>
    <span class="m-nav-title">Konverzace</span>
    <button class="refresh" type="button" @click="store.loadChats">↻</button>
  </header>
  <main class="m-body chat-list">
    <section class="new-chat"><p class="eyebrow">NOVÁ KONVERZACE</p><div class="create-grid"><Select v-model="workspaceIdModel" class="mobile-select" :options="workspaceOptions" placeholder="Vyber projekt…" /><button type="button" :disabled="!workspaceId || creating" @click="createChat">{{ creating ? 'Spouštím…' : 'Nový chat' }}</button></div><p v-if="createError" class="create-error">{{ createError }}</p></section>
    <p class="eyebrow">ŽIVÉ CHATY</p>
    <button v-for="chat in liveChats" :key="chat.id" class="chat-row" type="button" @click="store.openChat(chat)">
      <span :class="['s-dot', store.chatStatus(chat)]" aria-hidden="true" />
      <span class="chat-row-main"><strong>{{ chat.title }}</strong><small><span v-if="chat.workspaceName" class="chat-ws">{{ chat.workspaceName }}</span>{{ chat.agentKind || chat.transport }} · {{ chat.messages.length }} zpráv</small></span>
      <span :class="['chat-status', { 'chat-status--busy': chat.busy }]">{{ chat.busy ? 'Pracuje' : store.chatStatus(chat) === 'review' ? 'Hotovo' : store.chatStatus(chat) === 'permission' ? 'Potřebuje tě' : store.chatStatus(chat) === 'error' ? 'Chyba' : 'Připraven' }}</span>
      <span class="chevron">›</span>
    </button>
    <section v-if="!liveChats.length && !settledChats.length" class="empty"><span>✦</span><strong>Žádná chatová relace</strong><p>Otevři nebo spusť chat v desktopovém Burrowu. Tady se objeví a půjde okamžitě ovládat.</p></section>

    <template v-if="settledChats.length">
      <button type="button" class="collapse-toggle" @click="showSettled = !showSettled">{{ showSettled ? '▾' : '▸' }} Ostatní ({{ settledChats.length }})</button>
      <template v-if="showSettled">
        <button v-for="chat in settledChats" :key="chat.id" class="chat-row chat-row--settled" type="button" @click="store.openChat(chat)">
          <span class="s-dot idle" aria-hidden="true" />
          <span class="chat-row-main"><strong>{{ chat.title }}</strong><small><span v-if="chat.workspaceName" class="chat-ws">{{ chat.workspaceName }}</span>{{ chat.agentKind || chat.transport }} · {{ chat.messages.length }} zpráv</small></span>
          <span class="chevron">›</span>
        </button>
      </template>
    </template>
  </main>
  <nav class="bottom-nav" aria-label="Hlavní navigace"><button class="nav-item" type="button" @click="store.showDashboard"><span>⊞</span>Přehled</button><button class="nav-item nav-item--active" type="button"><span>✦</span>Chaty</button><button class="nav-item" type="button" @click="store.showSessions"><span>›_</span>Terminály</button></nav>
</template>

<style scoped>
.refresh{border:0;background:transparent;color:var(--accent);font-size:20px}.chat-list{padding:20px 16px calc(84px + var(--safe-bottom))}.eyebrow{margin:20px 0 9px;color:var(--text-muted);font:700 10px/1 var(--font-mono);letter-spacing:.12em}.eyebrow:first-child{margin-top:0}.create-grid{display:grid;grid-template-columns:1fr 100px;gap:8px}.create-grid :deep(.mobile-select),.create-grid button{min-height:40px;border:1px solid var(--border);border-radius:8px;background:var(--bg-hover);color:var(--text-primary);padding:0 9px;font:12px var(--font-ui)}.create-grid button{grid-column:span 2;border-color:var(--accent);background:var(--accent);font-weight:700}.create-grid button:disabled{opacity:.45}.create-error{margin:8px 0 0;color:var(--red);font-size:11px}.chat-row{width:100%;min-height:75px;display:flex;align-items:center;gap:10px;padding:10px 2px;border:0;border-top:1px solid var(--border);background:transparent;color:inherit;text-align:left}.chat-row:last-of-type{border-bottom:1px solid var(--border)}.chat-orb{width:34px;height:34px;display:grid;place-items:center;border:1px solid var(--border);border-radius:50%;color:var(--text-secondary)}.chat-orb--busy{border-color:var(--yellow);color:var(--yellow);box-shadow:0 0 18px color-mix(in srgb,var(--yellow) 25%,transparent)}.chat-row-main{flex:1;min-width:0;display:grid;gap:4px}.chat-row-main strong,.chat-row-main small{overflow:hidden;white-space:nowrap;text-overflow:ellipsis}.chat-row-main strong{font:650 14px/1.2 var(--font-mono);color:var(--text-primary)}.chat-row-main small{color:var(--text-muted);font:11px/1.2 var(--font-mono)}.chat-status{color:var(--text-muted);font-size:10px}.chat-status--busy{color:var(--yellow)}.chevron{color:var(--text-muted);font-size:22px}.empty{min-height:310px;display:grid;place-content:center;gap:10px;text-align:center;color:var(--text-secondary)}.empty span{color:var(--accent);font-size:28px}.empty strong{color:var(--text-primary)}.empty p{max-width:260px;margin:0;font-size:12px}.bottom-nav{position:fixed;right:0;bottom:0;left:0;display:grid;grid-template-columns:repeat(3,1fr);padding:9px 10px calc(9px + var(--safe-bottom));border-top:1px solid var(--border);background:var(--bg-panel)}.nav-item{min-height:48px;display:grid;place-items:center;gap:3px;border:0;background:transparent;color:var(--text-muted);font-size:10px}.nav-item span{font:700 22px/.9 var(--font-mono)}.nav-item--active{color:var(--accent)}

/* Which project a chat belongs to is the first thing you need when several
   agents are running — lead the meta line with it. */
.chat-ws {
  color: var(--accent);
  margin-right: 6px;
}
.chat-ws::after {
  content: " ·";
  color: var(--text-muted);
}

.collapse-toggle { width: 100%; text-align: left; padding: 10px 2px; border: 0; background: transparent; color: var(--text-muted); font: 700 11px var(--font-mono); letter-spacing: .06em; }
.chat-row--settled { opacity: .65; }
</style>

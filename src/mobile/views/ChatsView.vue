<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Sparkles } from "lucide-vue-next";
import { agentIconComp } from "@/lib/agentIcons";
import { providerFor } from "@/lib/providers";
import { useRemoteStore } from "../store";
import { STATUS_PRIORITY } from "@/lib/terminalStatus";

const store = useRemoteStore();
// Live/settled split ported from desktop's Sidebar.vue: pending work always
// wins as "live"; a settled chat sorts by recency instead of status, same as
// the desktop shelf.
const liveChats = computed(() =>
  store.chats
    .filter((c) => !store.chatSettled(c))
    .sort((a, b) => {
      const pa = STATUS_PRIORITY.indexOf(store.chatStatus(a));
      const pb = STATUS_PRIORITY.indexOf(store.chatStatus(b));
      return pa !== pb ? pa - pb : b.id - a.id;
    })
);
const settledChats = computed(() =>
  store.chats
    .filter((c) => store.chatSettled(c))
    .sort((a, b) => (store.chatActivity[String(b.id)] ?? 0) - (store.chatActivity[String(a.id)] ?? 0))
);
const showSettled = ref(false);

onMounted(() => { if (!store.chats.length) store.loadChats(); });
</script>

<template>
  <header class="m-nav">
    <span class="m-nav-title">Konverzace</span>
    <button class="refresh" type="button" @click="store.loadChats">↻</button>
  </header>
  <main class="m-body chat-list">
    <section class="new-chat"><p class="eyebrow">NOVÁ KONVERZACE</p><button type="button" class="new-chat-cta" @click="store.showWelcome"><Sparkles :size="16" />Spustit nový chat</button></section>
    <p class="eyebrow">ŽIVÉ CHATY</p>
    <div v-for="chat in liveChats" :key="chat.id" class="chat-row">
      <button class="chat-row-hit" type="button" @click="store.openChat(chat)">
        <span class="chat-avatar" :style="{ color: providerFor(chat.agentKind ?? '').color }" aria-hidden="true"><component :is="agentIconComp(providerFor(chat.agentKind ?? '').icon)" :size="20" /></span>
        <span class="chat-row-main"><strong>{{ chat.title }}</strong><small><span v-if="chat.workspaceName" class="chat-ws">{{ chat.workspaceName }}</span>{{ chat.agentKind || chat.transport }} · {{ chat.messages.length }} zpráv</small></span>
        <span :class="['chat-status', store.chatStatus(chat), { 'chat-status--busy': chat.busy }]"><span class="chat-status-dot" aria-hidden="true" />{{ store.chatStatus(chat) === 'permission' ? 'Potřebuje tě' : chat.busy ? 'Pracuje' : store.chatStatus(chat) === 'review' ? 'Hotovo' : store.chatStatus(chat) === 'error' ? 'Chyba' : 'Připraven' }}</span>
      </button>
      <button class="settle-btn" type="button" title="Označit jako vyřízené" @click="store.setChatSettledOverride(chat.id, 'settled')">✓</button>
    </div>
    <section v-if="!liveChats.length && !settledChats.length" class="empty"><span>✦</span><strong>Žádná chatová relace</strong><p>Otevři nebo spusť chat v desktopovém Burrowu. Tady se objeví a půjde okamžitě ovládat.</p></section>

    <template v-if="settledChats.length">
      <button type="button" class="collapse-toggle" @click="showSettled = !showSettled">{{ showSettled ? '▾' : '▸' }} Ostatní ({{ settledChats.length }})</button>
      <template v-if="showSettled">
        <div v-for="chat in settledChats" :key="chat.id" class="chat-row chat-row--settled">
          <button class="chat-row-hit" type="button" @click="store.openChat(chat)">
            <span class="chat-avatar" :style="{ color: providerFor(chat.agentKind ?? '').color }" aria-hidden="true"><component :is="agentIconComp(providerFor(chat.agentKind ?? '').icon)" :size="20" /></span>
            <span class="chat-row-main"><strong>{{ chat.title }}</strong><small><span v-if="chat.workspaceName" class="chat-ws">{{ chat.workspaceName }}</span>{{ chat.agentKind || chat.transport }} · {{ chat.messages.length }} zpráv</small></span>
          </button>
          <button class="settle-btn" type="button" title="Vrátit mezi aktivní" @click="store.setChatSettledOverride(chat.id, 'active')">↩</button>
        </div>
      </template>
    </template>
  </main>
</template>

<style scoped>
.refresh{width:36px;height:36px;flex-shrink:0;display:grid;place-items:center;border:1px solid var(--border);border-radius:10px;background:var(--bg-panel);color:var(--text-primary);font-size:18px}
.chat-list{padding:20px 16px calc(20px + var(--safe-bottom))}
.eyebrow{margin:20px 0 9px;color:var(--text-muted);font:700 10px/1 var(--font-mono);letter-spacing:.12em}
.eyebrow:first-child{margin-top:0}
.new-chat-cta{display:flex;align-items:center;justify-content:center;gap:8px;width:100%;min-height:44px;border:0;border-radius:12px;background:var(--accent);color:#fff;font:600 13px var(--font-ui)}
.new-chat-cta:active{opacity:.85}

.chat-row{width:100%;min-height:75px;display:flex;align-items:stretch;gap:4px;padding:8px;border:1px solid var(--border);border-radius:16px;background:var(--bg-panel)}
.chat-row + .chat-row{margin-top:12px}
.chat-row-hit{flex:1;min-width:0;display:flex;align-items:center;gap:12px;padding:8px;border:0;background:transparent;color:inherit;text-align:left}
.chat-row-hit:active{background:var(--bg-hover);border-radius:10px}
.settle-btn{flex-shrink:0;align-self:center;width:32px;height:32px;display:grid;place-items:center;border:1px solid var(--border);border-radius:9px;background:var(--bg-hover);color:var(--text-secondary);font-size:14px}
.settle-btn:active{color:var(--accent);border-color:var(--accent)}
.chat-avatar{width:40px;height:40px;flex-shrink:0;display:grid;place-items:center;border:1px solid var(--border);border-radius:10px;background:var(--bg-hover);color:var(--text-secondary)}
.chat-row-main{flex:1;min-width:0;display:grid;gap:4px}
.chat-row-main strong,.chat-row-main small{overflow:hidden;white-space:nowrap;text-overflow:ellipsis}
.chat-row-main strong{font-family:var(--font-ui);font-size:15px;font-weight:600;color:var(--text-primary)}
.chat-row-main small{color:var(--text-muted);font:11px/1.2 var(--font-mono)}
.chat-status{display:inline-flex;align-items:center;gap:6px;flex-shrink:0;padding:5px 10px;border:1px solid var(--border);border-radius:20px;background:var(--bg-hover);color:var(--text-secondary);font:11px var(--font-mono);font-weight:600;white-space:nowrap}
.chat-status-dot{width:6px;height:6px;border-radius:50%;flex-shrink:0;background:var(--border)}
.chat-status.running .chat-status-dot,.chat-status--busy .chat-status-dot{background:var(--status-running)}
.chat-status.permission .chat-status-dot{background:var(--status-permission)}
.chat-status.review .chat-status-dot,.chat-status.done .chat-status-dot{background:var(--status-review)}
.chat-status.error .chat-status-dot{background:var(--status-error)}
.empty{min-height:310px;display:grid;place-content:center;gap:10px;text-align:center;color:var(--text-secondary)}
.empty span{color:var(--accent);font-size:28px}
.empty strong{color:var(--text-primary)}
.empty p{max-width:260px;margin:0;font-size:12px}
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

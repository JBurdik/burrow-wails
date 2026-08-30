<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useRemoteStore, type Tab, type TabStatus } from '../store';

const store = useRemoteStore();
const activeChats = computed(() => store.chats.filter((chat) => chat.busy));
const primaryChat = computed(() => activeChats.value[0] ?? store.chats[0] ?? null);
const allTabs = computed(() => store.workspaces.flatMap((workspace) => workspace.tabs));
const activeTabs = computed(() => allTabs.value.filter((tab) => ['running', 'waiting', 'permission'].includes(store.statusFor(tab.ptyId))));
const completedTabs = computed(() => allTabs.value.filter((tab) => store.statusFor(tab.ptyId) === 'done'));
const needsAttention = computed(() => activeTabs.value.filter((tab) => ['waiting', 'permission'].includes(store.statusFor(tab.ptyId))));

function statusLabel(status: TabStatus) {
  return { idle: 'Připraveno', running: 'Pracuje', waiting: 'Čeká', permission: 'Potřebuje tebe', done: 'Hotovo' }[status];
}
function shortPath(path: string) { return path.replace(/^\/Users\/[^/]+/, '~'); }
function openTab(tab: Tab) { store.openTerminal(tab); }
function openPrimaryChat() { if (primaryChat.value) store.openChat(primaryChat.value); else store.showChats(); }

onMounted(() => { if (!store.workspaces.length) store.loadSessions(); if (!store.chats.length) store.loadChats(); });
</script>

<template>
  <header class="dashboard-header">
    <div>
      <p class="dashboard-kicker">BURROW REMOTE</p>
      <h1 class="dashboard-title">Přehled práce</h1>
    </div>
    <button class="connection" type="button" :disabled="store.loading" @click="store.loadSessions" aria-label="Obnovit relace">
      <span class="connection-dot" aria-hidden="true" />{{ store.loading ? 'Obnovuji' : 'Připojeno' }}
    </button>
  </header>

  <main id="main-content" class="dashboard-body">
    <section class="summary" aria-labelledby="now-heading">
      <div>
        <p id="now-heading" class="section-label">TEĎ</p>
        <p class="summary-line"><strong>{{ activeChats.length }}</strong> {{ activeChats.length === 1 ? 'agent pracuje v chatu' : activeChats.length < 5 ? 'agenti pracují v chatu' : 'agentů pracuje v chatu' }}</p>
        <p v-if="needsAttention.length" class="attention-copy">{{ needsAttention.length }} {{ needsAttention.length === 1 ? 'relace čeká na zásah' : 'relace čekají na zásah' }}</p>
        <p v-else class="calm-copy">Vše je v pohybu, žádná odpověď není potřeba.</p>
      </div>
      <button class="primary-action" type="button" @click="openPrimaryChat"><span aria-hidden="true">✦</span>{{ primaryChat ? 'Otevřít chat' : 'Zobrazit chaty' }}</button>
    </section>

    <section v-if="store.loading && !allTabs.length" class="dashboard-state" aria-live="polite"><span class="state-mark" aria-hidden="true">···</span>Načítám relace z Burrowu</section>
    <section v-else-if="store.listError && !allTabs.length" class="dashboard-state dashboard-state--error" role="alert">Relace se nepodařilo načíst.<button class="text-action" type="button" @click="store.loadSessions">Zkusit znovu</button></section>
    <section v-else-if="!allTabs.length && !store.chats.length" class="dashboard-state"><span class="state-mark" aria-hidden="true">□</span><strong>Žádný otevřený agent</strong><span>Spusť agenta v desktopovém Burrowu, tady se objeví automaticky.</span></section>

    <template v-else>
      <section v-if="store.chats.length" class="session-section" aria-labelledby="chat-heading">
        <div class="section-heading"><p id="chat-heading" class="section-label">KONVERZACE</p><button class="text-action" type="button" @click="store.showChats">Všechny</button></div>
        <ul class="session-list"><li v-for="chat in store.chats.slice(0, 3)" :key="chat.id"><button class="session-row" type="button" @click="store.openChat(chat)"><span :class="['session-icon', { running: chat.busy }]" aria-hidden="true">✦</span><span class="session-main"><span class="session-title">{{ chat.title }}</span><span class="session-path">{{ chat.workspaceName ? chat.workspaceName + ' · ' : '' }}{{ chat.agentKind || chat.transport }}</span></span><span :class="['state-pill', { running: chat.busy }]">{{ chat.busy ? 'Pracuje' : 'Připraven' }}</span><span class="session-chevron" aria-hidden="true">›</span></button></li></ul>
      </section>
      <section v-if="activeTabs.length" class="session-section" aria-labelledby="active-heading">
        <div class="section-heading"><p id="active-heading" class="section-label">AKTIVNÍ RELACE</p><button class="text-action" type="button" @click="store.showSessions">Všechny</button></div>
        <ul class="session-list">
          <li v-for="tab in activeTabs" :key="tab.ptyId">
            <button class="session-row" type="button" @click="openTab(tab)">
              <span :class="['session-icon', store.statusFor(tab.ptyId)]" aria-hidden="true">›_</span>
              <span class="session-main"><span class="session-title">{{ tab.title }}</span><span class="session-path">{{ shortPath(tab.cwd) }}</span></span>
              <span class="session-state"><span :class="['state-pill', store.statusFor(tab.ptyId)]">{{ statusLabel(store.statusFor(tab.ptyId)) }}</span><span class="session-chevron" aria-hidden="true">›</span></span>
            </button>
          </li>
        </ul>
      </section>

      <section v-if="completedTabs.length" class="session-section session-section--completed" aria-labelledby="completed-heading">
        <div class="section-heading"><p id="completed-heading" class="section-label">NEDÁVNO HOTOVO</p><span class="section-count">{{ completedTabs.length }}</span></div>
        <ul class="session-list">
          <li v-for="tab in completedTabs.slice(0, 3)" :key="tab.ptyId">
            <button class="session-row session-row--done" type="button" @click="openTab(tab)"><span class="session-icon done" aria-hidden="true">✓</span><span class="session-main"><span class="session-title">{{ tab.title }}</span><span class="session-path">{{ shortPath(tab.cwd) }}</span></span><span class="session-chevron" aria-hidden="true">›</span></button>
          </li>
        </ul>
      </section>
    </template>
  </main>

  <nav class="bottom-nav" aria-label="Hlavní navigace">
    <button class="nav-item nav-item--active" type="button" aria-current="page" @click="store.showDashboard"><span aria-hidden="true">⊞</span>Přehled</button>
    <button class="nav-item" type="button" @click="store.showChats"><span aria-hidden="true">✦</span>Chaty</button>
    <button class="nav-item" type="button" @click="store.showSessions"><span aria-hidden="true">›_</span>Terminály</button>
  </nav>
</template>

<style scoped>
.dashboard-header { display:flex; align-items:flex-start; justify-content:space-between; padding:calc(var(--safe-top) + 24px) 20px 20px; border-bottom:1px solid var(--border); }.dashboard-kicker,.section-label{margin:0;color:var(--text-muted);font:700 10px/1.2 var(--font-mono);letter-spacing:.12em}.dashboard-title{margin:8px 0 0;color:var(--text-primary);font-size:28px;letter-spacing:-.04em;line-height:1}.connection{display:inline-flex;align-items:center;gap:7px;min-height:36px;padding:0 10px;border:1px solid var(--border);border-radius:9px;background:var(--bg-panel);color:var(--text-secondary);font-size:12px}.connection-dot{width:7px;height:7px;border-radius:50%;background:var(--accent)}.dashboard-body{flex:1;overflow-y:auto;padding:22px 20px calc(96px + var(--safe-bottom))}.summary{display:flex;align-items:flex-end;justify-content:space-between;gap:16px;padding-bottom:25px;border-bottom:1px solid var(--border)}.summary-line{margin:9px 0 4px;color:var(--text-primary);font-size:17px;letter-spacing:-.02em}.summary-line strong{color:var(--yellow);font:800 30px/.9 var(--font-mono);letter-spacing:-.08em}.attention-copy,.calm-copy{margin:0;color:var(--text-secondary);font-size:12px}.attention-copy{color:var(--yellow)}.primary-action{flex:0 0 auto;min-height:46px;display:inline-flex;align-items:center;gap:8px;padding:0 13px;border:1px solid var(--yellow);border-radius:10px;background:color-mix(in srgb,var(--yellow) 12%,var(--bg-panel));color:var(--text-primary);font-size:12px;font-weight:700}.primary-action span{color:var(--yellow);font:800 16px/1 var(--font-mono)}.session-section{margin-top:27px}.session-section--completed{margin-top:34px}.section-heading{display:flex;align-items:center;justify-content:space-between;margin-bottom:8px}.text-action{min-height:32px;border:0;padding:0;background:transparent;color:var(--accent);font-size:12px;font-weight:700}.section-count{color:var(--text-muted);font:700 12px/1 var(--font-mono)}.session-list{margin:0;padding:0;list-style:none;border-top:1px solid var(--border)}.session-row{width:100%;min-height:72px;display:flex;align-items:center;gap:12px;padding:10px 0;border:0;border-bottom:1px solid var(--border);background:transparent;color:inherit;text-align:left}.session-row:active{background:var(--bg-hover)}.session-icon{width:34px;height:34px;display:grid;place-items:center;border:1px solid var(--border);border-radius:50%;color:var(--text-secondary);font:700 15px/1 var(--font-mono)}.session-icon.running,.session-icon.permission{color:var(--yellow);border-color:var(--yellow)}.session-icon.waiting{color:var(--accent);border-color:var(--accent)}.session-icon.done{color:var(--green)}.session-main{flex:1;min-width:0;display:grid;gap:3px}.session-title,.session-path{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.session-title{color:var(--text-primary);font:650 14px/1.25 var(--font-mono)}.session-path{color:var(--text-muted);font:11px/1.25 var(--font-mono)}.session-state{display:flex;align-items:center;gap:11px}.state-pill{padding:5px 7px;border:1px solid var(--border);border-radius:6px;color:var(--text-secondary);font-size:10px;font-weight:700;white-space:nowrap}.state-pill.running,.state-pill.permission{border-color:var(--yellow);color:var(--yellow)}.state-pill.waiting{border-color:var(--accent);color:var(--accent)}.session-chevron{color:var(--text-muted);font-size:23px;line-height:1}.session-row--done{opacity:.72}.dashboard-state{min-height:260px;display:grid;place-content:center;gap:9px;color:var(--text-secondary);text-align:center}.dashboard-state--error{color:var(--red)}.state-mark{color:var(--text-muted);font:700 22px/1 var(--font-mono)}.bottom-nav{position:fixed;right:0;bottom:0;left:0;display:grid;grid-template-columns:repeat(3,1fr);padding:9px 10px calc(9px + var(--safe-bottom));border-top:1px solid var(--border);background:var(--bg-panel)}.nav-item{min-height:48px;display:grid;place-items:center;gap:3px;border:0;background:transparent;color:var(--text-muted);font-size:10px}.nav-item span{font:700 22px/.9 var(--font-mono)}.nav-item--active{color:var(--accent)}@media (min-width:680px){.dashboard-header,.dashboard-body{width:min(680px,100%);margin-inline:auto}.bottom-nav{left:50%;width:min(680px,100%);transform:translateX(-50%);border-right:1px solid var(--border);border-left:1px solid var(--border)}}
</style>

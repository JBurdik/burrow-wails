<template>
  <div class="m-root">
    <ConnectView v-if="store.view === 'connect'" />
    <DashboardView v-else-if="store.view === 'dashboard'" />
    <ChatsView v-else-if="store.view === 'chats'" />
    <ChatView v-else-if="store.view === 'chat'" />
    <SessionsView v-else-if="store.view === 'sessions'" />
    <TerminalView v-else-if="store.view === 'terminal'" />
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue';
import { useRemoteStore } from './store';
import ConnectView from './views/ConnectView.vue';
import DashboardView from './views/DashboardView.vue';
import ChatsView from './views/ChatsView.vue';
import ChatView from './views/ChatView.vue';
import SessionsView from './views/SessionsView.vue';
import TerminalView from './views/TerminalView.vue';

const store = useRemoteStore();

onMounted(() => {
  if (store.baseUrl && store.token) {
    store.connect(store.baseUrl, store.token).catch(() => {});
  }
});
</script>

<style>
/* Dark theme token baseline — same keys as desktop App.vue :root */
:root {
  --bg-base:        oklch(0.145 0.012 257);
  --bg-panel:       oklch(0.175 0.013 257);
  --bg-hover:       oklch(0.238 0.018 257);
  --bg-selected:    oklch(0.255 0.054 259);
  --border:         oklch(0.31 0.014 257);
  --text-primary:   oklch(0.94 0.012 85);
  --text-secondary: oklch(0.74 0.018 257);
  --text-muted:     oklch(0.57 0.018 257);
  --accent:         oklch(0.65 0.17 255);
  --accent-dim:     oklch(0.48 0.13 255);
  --green:          oklch(0.72 0.16 145);
  --yellow:         oklch(0.79 0.16 74);
  --red:            oklch(0.64 0.21 25);

  /* status dot tokens */
  --status-running:    #fb923c;
  --status-waiting:    #3b82f6;
  --status-permission: #f59e0b;
  --status-done:       #84cc16;
  --status-review:     #22c55e;
  --status-error:      #ef4444;

  --font-mono: 'SF Mono', 'Cascadia Code', 'Menlo', 'Consolas', monospace;
  --font-ui:   -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;

  --safe-top:    env(safe-area-inset-top, 0px);
  --safe-bottom: env(safe-area-inset-bottom, 0px);
}

*, *::before, *::after { box-sizing: border-box; }

html, body {
  margin: 0; padding: 0;
  background: var(--bg-base);
  color: var(--text-primary);
  font-family: var(--font-ui);
  font-size: 14px;
  line-height: 1.4;
  -webkit-font-smoothing: antialiased;
  overscroll-behavior: none;
}

.m-root {
  min-height: 100dvh;
  display: flex;
  flex-direction: column;
  background: radial-gradient(circle at 92% -12%, oklch(0.3 0.05 257 / 0.52), transparent 32rem), var(--bg-base);
}

button, input { font: inherit; }
button { -webkit-tap-highlight-color: transparent; }
button:focus-visible, input:focus-visible, [role="link"]:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

/* ── shared nav bar ── */
.m-nav {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: calc(var(--safe-top) + 8px) 16px 8px;
  background: color-mix(in oklch, var(--bg-panel) 92%, transparent);
  border-bottom: 1px solid var(--border);
  min-height: 56px;
}
.m-nav-title {
  font-family: var(--font-mono);
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.m-nav-back {
  background: none;
  border: none;
  color: var(--accent);
  font-size: 14px;
  padding: 4px 0;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

/* ── scrollable body ── */
.m-body {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding-bottom: calc(76px + var(--safe-bottom));
}

/* ── flat list rows ── */
.m-list { list-style: none; margin: 0; padding: 0; }
.m-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
  min-height: 48px;
  text-decoration: none;
  color: inherit;
}
.m-row:active { background: var(--bg-hover); }

/* ── status dot (mirrors desktop status-dots.css) ── */
.s-dot {
  width: 7px; height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.s-dot.idle       { background: var(--border); }
.s-dot.running    { background: var(--status-running); animation: pulse-orange 1s infinite; }
.s-dot.waiting    { background: var(--status-waiting); }
.s-dot.permission { background: var(--status-permission); animation: pulse-amber 1s infinite; }
.s-dot.done       { background: var(--status-done); }
.s-dot.review     { background: var(--status-review); animation: pulse-green 2s infinite; }
.s-dot.error      { background: var(--status-error); animation: pulse-red 1.4s infinite; }

@keyframes pulse-orange {
  0%, 100% { opacity: 1; } 50% { opacity: 0.4; }
}
@keyframes pulse-amber {
  0%, 100% { opacity: 1; } 50% { opacity: 0.5; }
}
@keyframes pulse-green {
  0%, 100% { opacity: 1; } 50% { opacity: 0.55; }
}
@keyframes pulse-red {
  0%, 100% { opacity: 1; } 50% { opacity: 0.4; }
}

/* ── state overlays ── */
.m-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 48px 24px;
  color: var(--text-muted);
  font-size: 13px;
  text-align: center;
  flex: 1;
}
.m-state-icon { font-size: 28px; opacity: 0.5; }
.m-state-msg  { color: var(--text-secondary); }
.m-state-detail { font-family: var(--font-mono); font-size: 11px; color: var(--red); }

/* ── form elements ── */
.m-input {
  width: 100%;
  background: var(--bg-hover);
  border: 1px solid var(--border);
  border-radius: 10px;
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 13px;
  padding: 10px 12px;
  outline: none;
  transition: border-color 0.15s;
}
.m-input:focus { border-color: var(--accent); }
.m-input::placeholder { color: var(--text-muted); }

.m-btn {
  width: 100%;
  background: var(--accent);
  color: #f8fafc;
  border: none;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 600;
  padding: 12px;
  cursor: pointer;
  transition: opacity 0.15s;
}
.m-btn:active  { opacity: 0.8; }
.m-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.m-btn-ghost {
  background: none;
  border: 1px solid var(--border);
  color: var(--text-secondary);
  border-radius: 9px;
  font-size: 13px;
  padding: 8px 12px;
  cursor: pointer;
}
</style>

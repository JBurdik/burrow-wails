<template>
  <div class="m-root">
    <ConnectView v-if="store.view === 'connect'" />
    <SessionsView v-else-if="store.view === 'sessions'" />
    <TerminalView v-else-if="store.view === 'terminal'" />
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue';
import { useRemoteStore } from './store';
import ConnectView from './views/ConnectView.vue';
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
  --bg-base:      #0d0d0d;
  --bg-panel:     #111111;
  --bg-hover:     #1a1a1a;
  --bg-selected:  #1e3a5f;
  --border:       #3a3a3a;
  --text-primary:   #f1f5f9;
  --text-secondary: #aab6c5;
  --text-muted:     #8b97a8;
  --accent:       #3b82f6;
  --accent-dim:   #1d4ed8;
  --green:        #22c55e;
  --yellow:       #eab308;
  --red:          #ef4444;

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
}

/* ── shared nav bar ── */
.m-nav {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: calc(var(--safe-top) + 8px) 16px 8px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border);
  min-height: 48px;
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
  padding-bottom: var(--safe-bottom);
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
  border-radius: 6px;
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
  border-radius: 6px;
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
  border-radius: 6px;
  font-size: 13px;
  padding: 8px 12px;
  cursor: pointer;
}
</style>

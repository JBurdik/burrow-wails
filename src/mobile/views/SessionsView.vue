<template>
  <header class="m-nav">
    <span class="m-nav-title">Burrow</span>
    <button class="m-btn-ghost" style="padding:4px 8px;font-size:12px" :disabled="store.loading" @click="store.loadSessions()">
      {{ store.loading ? '…' : '↺' }}
    </button>
    <button class="m-btn-ghost" @click="store.disconnect()">Disconnect</button>
  </header>

  <div v-if="store.loading && !store.workspaces.length" class="m-state">
    <span class="m-state-icon">⏳</span>
    <span class="m-state-msg">Loading sessions…</span>
  </div>

  <div v-else-if="store.listError && !store.workspaces.length" class="m-state">
    <span class="m-state-icon">✕</span>
    <span class="m-state-msg">Could not load sessions.</span>
    <span class="m-state-detail">{{ store.listError }}</span>
    <button class="m-btn-ghost" style="margin-top:8px" @click="store.loadSessions()">Retry</button>
  </div>

  <div v-else-if="!totalTabs" class="m-state">
    <span class="m-state-icon">□</span>
    <span class="m-state-msg">No open terminal sessions.</span>
  </div>

  <div v-else class="m-body" @touchstart="onTouchStart" @touchmove="onTouchMove" @touchend="onTouchEnd">
    <div v-for="ws in store.workspaces" :key="ws.id" class="ws-group">
      <div v-if="ws.tabs.length" class="ws-header">
        <span class="ws-name">{{ ws.name }}</span>
        <span class="ws-path">{{ shortPath(ws.path) }}</span>
      </div>

      <ul v-if="ws.tabs.length" class="m-list">
        <li
          v-for="tab in ws.tabs"
          :key="tab.ptyId"
          class="m-row"
          role="link"
          :aria-label="`${tab.title}, status: ${store.statusFor(tab.ptyId)}`"
          @click="store.openTerminal(tab)"
        >
          <span :class="['s-dot', store.statusFor(tab.ptyId)]" aria-hidden="true" />
          <div class="row-info">
            <span class="row-title">{{ tab.title }}</span>
            <span class="row-status">{{ store.statusFor(tab.ptyId) }}</span>
          </div>
          <span class="row-chevron" aria-hidden="true">›</span>
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useRemoteStore } from '../store';

const store = useRemoteStore();

const totalTabs = computed(() => store.workspaces.reduce((n, w) => n + w.tabs.length, 0));

function shortPath(p: string) {
  return p.replace(/^\/Users\/[^/]+/, '~');
}

// Pull-to-refresh: a plain touch gesture, no library — refresh once the user
// drags down >70px from the very top of the scroll body and releases.
let startY = 0;
let pulling = false;
function onTouchStart(e: TouchEvent) {
  if ((e.currentTarget as HTMLElement).scrollTop === 0) {
    startY = e.touches[0].clientY;
    pulling = true;
  }
}
function onTouchMove(e: TouchEvent) {
  if (!pulling) return;
  if (e.touches[0].clientY - startY < 0) pulling = false;
}
function onTouchEnd(e: TouchEvent) {
  if (pulling && e.changedTouches[0].clientY - startY > 70) store.loadSessions();
  pulling = false;
}

onMounted(() => {
  if (!store.workspaces.length) store.loadSessions();
});
</script>

<style scoped>
.ws-group { margin-bottom: 4px; }

.ws-header {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 10px 16px 6px;
  background: var(--bg-base);
  position: sticky;
  top: 0;
  z-index: 1;
  border-bottom: 1px solid var(--border);
}
.ws-name {
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 700;
  color: var(--text-primary);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.ws-path {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.row-title {
  font-family: var(--font-mono);
  font-size: 14px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.row-status {
  font-size: 11px;
  color: var(--text-muted);
}
.row-chevron {
  color: var(--text-muted);
  font-size: 18px;
  flex-shrink: 0;
}
</style>

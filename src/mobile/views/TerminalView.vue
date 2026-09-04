<template>
  <header class="m-nav">
    <button class="m-nav-back" @click="close" aria-label="Back">‹ Back</button>
    <span class="m-nav-title">{{ tab?.title }}</span>
    <span v-if="tab" :class="['s-dot', store.statusFor(tab.ptyId)]" style="margin-left:4px" aria-hidden="true" />
  </header>

  <div ref="termHost" class="term-host"></div>

  <div class="term-keys">
    <button class="term-key" @click="sendBytes([0x1b])">Esc</button>
    <button class="term-key" @click="sendBytes([0x09])">Tab</button>
    <button class="term-key" @click="sendBytes([0x03])">^C</button>
    <button class="term-key" @click="sendBytes([0x1b, 0x5b, 0x41])">↑</button>
    <button class="term-key" @click="sendBytes([0x1b, 0x5b, 0x42])">↓</button>
    <button class="term-key" @click="sendBytes([0x1b, 0x5b, 0x44])">←</button>
    <button class="term-key" @click="sendBytes([0x1b, 0x5b, 0x43])">→</button>
  </div>

  <form class="out-composer" @submit.prevent="send">
    <input
      v-model="draft"
      class="term-input"
      type="text"
      enterkeyhint="send"
      placeholder="Type a command…"
      autocomplete="off"
      autocorrect="off"
      autocapitalize="none"
      spellcheck="false"
    />
    <button class="out-send" type="submit" :disabled="!draft.trim()">Send</button>
  </form>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { useRemoteStore } from '../store';

const store = useRemoteStore();
const tab = store.activeTab!;

const termHost = ref<HTMLDivElement | null>(null);
const draft = ref('');
let term: Terminal;
let fitAddon: FitAddon;

function writeBytes(payload: number[]) {
  term.write(new Uint8Array(payload));
}

function sendBytes(bytes: number[]) {
  if (!tab) return;
  store.getClient().call('write_pty', { id: String(tab.ptyId), data: bytes }).catch(() => {});
}

function send() {
  const text = draft.value;
  if (!text || !tab) return;
  const bytes = Array.from(new TextEncoder().encode(text + '\r'));
  sendBytes(bytes);
  draft.value = '';
}

function close() {
  store.closeTerminal();
}

onMounted(() => {
  if (!tab || !termHost.value) return;

  term = new Terminal({
    fontFamily: "'SF Mono', 'Cascadia Code', 'Menlo', 'Consolas', monospace",
    fontSize: 13,
    lineHeight: 1.4,
    cursorBlink: true,
    allowProposedApi: true,
    scrollback: 3000,
    theme: {
      background: '#0d0d0d',
      foreground: '#f1f5f9',
      cursor: '#3b82f6',
    },
  });
  fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.open(termHost.value);
  fitAddon.fit();

  const client = store.getClient();
  // Vec<u8> serializes as a JSON array of byte numbers over WS — not base64/text.
  client.subscribe(`pty-data-${tab.ptyId}`, (payload: number[]) => writeBytes(payload));

  client.call('resize_pty', { id: String(tab.ptyId), cols: term.cols, rows: term.rows }).catch(() => {});

  window.addEventListener('resize', onResize);
});

function onResize() {
  fitAddon?.fit();
  if (tab && term) {
    store.getClient().call('resize_pty', { id: String(tab.ptyId), cols: term.cols, rows: term.rows }).catch(() => {});
  }
}

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize);
  if (tab) store.getClient().unsubscribe(`pty-data-${tab.ptyId}`);
  term?.dispose();
});
</script>

<style scoped>
.term-host {
  flex: 1;
  min-height: 0;
  background: var(--bg-base);
  padding: 6px 4px calc(var(--safe-bottom));
  overflow: hidden;
}

.term-keys {
  display: flex;
  gap: 6px;
  padding: 6px 8px;
  background: var(--bg-panel);
  border-top: 1px solid var(--border);
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}
.term-key {
  flex-shrink: 0;
  min-width: 42px;
  min-height: 36px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-hover);
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 700;
}
.term-key:active { background: var(--bg-selected); }

.out-composer {
  display: flex;
  gap: 8px;
  padding: 8px 10px calc(var(--safe-bottom) + 8px);
  border-top: 1px solid var(--border);
  background: var(--bg-panel);
}
.term-input {
  flex: 1;
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-hover);
  color: var(--text-primary);
  font: 16px/1.4 var(--font-ui);
  padding: 9px 10px;
  outline: none;
}
.term-input:focus { border-color: var(--accent); }

.out-send {
  min-height: 38px;
  padding: 0 14px;
  border-radius: 8px;
  border: 1px solid var(--accent);
  background: var(--accent);
  color: #f8fafc;
  font-size: 13px;
  font-weight: 700;
}
.out-send:disabled { opacity: 0.45; }
</style>

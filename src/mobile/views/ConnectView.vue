<template>
  <div class="pair-wrap">
    <div class="pair-logo">
      <span class="pair-mark" aria-hidden="true">B</span>
      <span class="pair-product">Burrow Remote</span>
    </div>

    <form class="pair-form" @submit.prevent="tryConnect">
      <p class="pair-target">{{ urlInput }}</p>
      <label class="pair-label">Přístupový token</label>
      <input
        v-model="tokenInput"
        class="m-input pair-token"
        type="text"
        inputmode="text"
        placeholder="48 znaků z Nastavení"
        autocomplete="off"
        autocorrect="off"
        autocapitalize="none"
        spellcheck="false"
        required
      />

      <div v-if="err" class="pair-error" :class="{ 'pair-error--network': isNetworkError }" role="alert">
        {{ err }}
      </div>

      <button class="m-btn" type="submit" :disabled="busy">
        {{ busy ? 'Connecting…' : 'Connect' }}
      </button>
    </form>

    <p class="pair-hint">
      Na desktopu v Nastavení → Remote access zkopíruj celou adresu i s tokenem —
      otevřením takového odkazu se pole vyplní samo.
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { useRemoteStore } from '../store';

const store = useRemoteStore();

function currentRemoteUrl() {
  const path = window.location.pathname.replace(/\/(?:mobile\.html)?$/, '') || '/';
  return `${window.location.origin}${path === '/' ? '' : path}`;
}
const urlInput   = ref(store.baseUrl || currentRemoteUrl());
const tokenInput = ref(store.token || '');
const busy = ref(false);
const err  = ref('');
const isNetworkError = ref(false);

async function tryConnect() {
  busy.value = true;
  err.value = '';
  isNetworkError.value = false;
  try {
    await store.connect(urlInput.value.trim(), tokenInput.value.trim());
  } catch (e: any) {
    const msg: string = e?.message ?? 'Connection failed';
    // A 401 from the /ws upgrade surfaces as a closed-before-open WebSocket error —
    // distinguish "reachable but wrong token" from "can't reach host at all" using
    // the healthCheck outcome that already ran inside store.connect().
    isNetworkError.value = !/token|unauthor/i.test(msg);
    err.value = isNetworkError.value
      ? `Could not reach ${urlInput.value}: ${msg}`
      : 'Connected to server, but the token was rejected (401).';
  } finally {
    busy.value = false;
  }
}
</script>

<style scoped>
.pair-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 100dvh;
  padding: 32px 24px calc(var(--safe-bottom) + 32px);
}

.pair-logo {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  margin-bottom: 32px;
}
.pair-mark {
  width: 56px;
  height: 56px;
  display: grid;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--bg-panel);
  color: var(--accent);
  font-family: var(--font-mono);
  font-size: 22px;
  font-weight: 800;
}
.pair-product {
  font-family: var(--font-mono);
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
  letter-spacing: 0.02em;
}

.pair-form {
  width: 100%;
  max-width: 360px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.pair-target {
  margin: 0 0 8px;
  overflow: hidden;
  color: var(--text-muted);
  font: 11px/1.4 var(--font-mono);
  text-align: center;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pair-label {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
  margin-bottom: -4px;
}

/* 48 hex chars has to stay readable on a phone. */
.pair-token {
  font-family: var(--font-mono);
  font-size: 12px;
  letter-spacing: 0.02em;
}

.pair-error {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--red);
  padding: 8px 10px;
  background: rgba(239, 68, 68, 0.08);
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 4px;
}
.pair-error--network {
  color: var(--yellow);
  background: rgba(234, 179, 8, 0.08);
  border-color: rgba(234, 179, 8, 0.3);
}

.pair-hint {
  margin-top: 20px;
  font-size: 12px;
  color: var(--text-muted);
  text-align: center;
  max-width: 300px;
  line-height: 1.6;
}
</style>

<template>
  <div class="pair-wrap">
    <div class="pair-logo">
      <span class="pair-mark" aria-hidden="true">B</span>
      <span class="pair-product">Burrow Remote</span>
    </div>

    <form class="pair-form" @submit.prevent="tryPair">
      <p class="pair-target">{{ urlInput }}</p>
      <label class="pair-label">Párovací kód</label>
      <PinInputRoot
        v-model="codeDigits"
        class="pin-root"
        type="number"
        placeholder="•"
        :disabled="busy"
        @complete="tryPair"
      >
        <PinInputInput v-for="i in 6" :key="i" :index="i - 1" class="pin-cell" />
      </PinInputRoot>

      <div v-if="err" class="pair-error" :class="{ 'pair-error--network': isNetworkError }" role="alert">
        {{ err }}
      </div>

      <button class="m-btn" type="submit" :disabled="busy || codeDigits.length < 6">
        {{ busy ? 'Připojuji…' : 'Připojit' }}
      </button>
    </form>

    <p class="pair-hint">
      Na desktopu v Nastavení → Remote access najdeš šestimístný kód.
      Platí na jedno použití — po spárování se telefon připojuje sám.
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { PinInputRoot, PinInputInput } from 'reka-ui';
import { useRemoteStore } from '../store';

const store = useRemoteStore();

function currentRemoteUrl() {
  const path = window.location.pathname.replace(/\/(?:mobile\.html)?$/, '') || '/';
  return `${window.location.origin}${path === '/' ? '' : path}`;
}
const urlInput = ref(store.baseUrl || currentRemoteUrl());
const codeDigits = ref<number[]>([]);
const busy = ref(false);
const err  = ref('');
const isNetworkError = ref(false);

async function tryPair() {
  const code = codeDigits.value.join('');
  if (busy.value || code.length < 6) return;
  busy.value = true;
  err.value = '';
  isNetworkError.value = false;
  try {
    await store.pair(urlInput.value.trim(), code);
  } catch (e: any) {
    const msg: string = e?.message ?? 'Připojení selhalo';
    // Anything from /pair itself is a server answer, so the host is reachable.
    // Only a transport failure (or a rejected /ws upgrade) is a network problem.
    isNetworkError.value = !/kód|zamčené|token|unauthor/i.test(msg);
    err.value = isNetworkError.value ? `Nedostupné ${urlInput.value}: ${msg}` : msg;
    codeDigits.value = [];
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

/* reka-ui PinInput, styled with the mobile theme instead of shadcn's
   tailwind skin — src/mobile/ carries no tailwind. */
.pin-root {
  display: flex;
  gap: 8px;
  justify-content: center;
}
.pin-cell {
  width: 44px;
  height: 56px;
  padding: 0;
  text-align: center;
  font-family: var(--font-mono);
  font-size: 22px;
  color: var(--text);
  background: var(--bg-input, var(--bg-panel));
  border: 1px solid var(--border);
  border-radius: 8px;
  outline: none;
  -moz-appearance: textfield;
}
.pin-cell::-webkit-outer-spin-button,
.pin-cell::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }
.pin-cell:focus { border-color: var(--accent); }
.pin-cell::placeholder { color: var(--text-muted); }

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

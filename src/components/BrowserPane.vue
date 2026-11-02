<template>
  <div ref="rootEl" class="flex h-full w-full flex-col overflow-hidden bg-panel">
    <div class="flex shrink-0 items-center gap-1 border-b border-border bg-panel px-1.5 py-1">
      <button class="flex shrink-0 items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="refresh" title="Refresh">
        <PhArrowClockwise :size="13" />
      </button>
      <input
        v-model="inputUrl"
        class="min-w-0 flex-1 rounded-[5px] border border-border bg-base px-2 py-1 font-sans text-xs text-foreground outline-none focus:border-accent"
        spellcheck="false"
        placeholder="Enter URL or localhost:3000…"
        @keydown.enter="navigate"
        @focus="($event.target as HTMLInputElement).select()"
      />
      <button class="flex shrink-0 items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="openExternal" title="Open in system browser">
        <PhArrowSquareOut :size="13" />
      </button>
    </div>
    <div class="relative flex-1 overflow-hidden">
      <iframe
        v-if="committedUrl && !nothingListening"
        :key="frameKey"
        ref="iframeEl"
        class="block h-full w-full border-0 bg-white"
        :src="committedUrl"
        sandbox="allow-downloads allow-forms allow-modals allow-same-origin allow-scripts"
        allow="clipboard-read; clipboard-write"
        @load="onLoad"
        @error="onError"
      />
      <div v-else-if="nothingListening" class="absolute inset-0 flex flex-col items-center justify-center gap-2.5 p-6 text-center text-[13px] text-muted-foreground">
        <PhPlugsConnected :size="32" class="mb-1 opacity-35" />
        <p class="text-foreground">Nothing listening on localhost:{{ port }}</p>
        <p class="text-[11px] leading-relaxed opacity-70">Start a dev server on this port in your session, or ask {{ props.agentName || "the agent" }} to start one.</p>
        <div class="mt-1.5 flex items-center gap-1.5">
          <button
            v-if="props.canAsk"
            class="rounded-[5px] border border-border bg-accent px-3.5 py-1 text-xs text-foreground transition-colors hover:brightness-110 disabled:opacity-50"
            :disabled="asked"
            @click="askToStart"
          >{{ asked ? "Asked" : `Ask ${props.agentName || "the agent"} to start it` }}</button>
          <button class="rounded-[5px] border border-border px-3.5 py-1 text-xs text-foreground transition-colors hover:bg-hover" @click="probe">Retry</button>
        </div>
      </div>
      <div v-else class="absolute inset-0 flex flex-col items-center justify-center gap-2.5 p-6 text-center text-[13px] text-muted-foreground">
        <PhGlobe :size="36" class="mb-1 opacity-35" />
        <p>Enter a URL above to browse</p>
        <p class="text-[11px] leading-relaxed opacity-60">Works best with localhost dev servers.<br />External sites may block embedding.</p>
      </div>
      <div v-if="blocked" class="absolute inset-0 flex flex-col items-center justify-center gap-2.5 bg-panel p-6 text-center text-[13px] text-muted-foreground">
        <PhProhibit :size="28" class="mb-1 opacity-35" />
        <p>This site blocked embedding (X-Frame-Options).</p>
        <button class="mt-1.5 rounded-[5px] border border-border bg-accent px-3.5 py-1 text-xs text-foreground transition-colors hover:brightness-110" @click="openExternal">Open in system browser</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { PhArrowClockwise, PhArrowSquareOut, PhGlobe, PhPlugsConnected, PhProhibit } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { open as shellOpen } from "@tauri-apps/plugin-shell";
import { localPort } from "@/lib/chatLinks";

const props = defineProps<{
  initialUrl?: string;
  /** A navigation pushed from outside (a chat link). `n` makes a repeat click on the same URL navigate again. */
  request?: { url: string; n: number };
  /** Offer "Ask … to start it" in the nothing-listening state (the host owns the send). */
  canAsk?: boolean;
  agentName?: string;
}>();
const emit = defineEmits<{ askStart: [port: number] }>();

const inputUrl = ref(props.initialUrl ?? "");
const committedUrl = ref(props.initialUrl ?? "");
const blocked = ref(false);
const iframeEl = ref<HTMLIFrameElement | null>(null);
const rootEl = ref<HTMLElement | null>(null);
const frameKey = ref(0);

watch(() => props.initialUrl, (u) => {
  if (u) {
    inputUrl.value = u;
    go(u);
  }
});
watch(() => props.request, (r) => { if (r) go(r.url); }, { immediate: true });

function normalizeUrl(raw: string): string {
  const s = raw.trim();
  if (!s) return s;
  if (/^https?:\/\//i.test(s)) return s;
  // bare host or localhost:port — assume http
  if (/^localhost(:\d+)?/i.test(s) || /^\d{1,3}(\.\d{1,3}){3}(:\d+)?/.test(s)) {
    return `http://${s}`;
  }
  return `https://${s}`;
}

function go(url: string) {
  blocked.value = false;
  committedUrl.value = normalizeUrl(url);
  inputUrl.value = committedUrl.value;
}

function navigate() {
  go(inputUrl.value);
}

function refresh() {
  if (port.value !== null && listening.value === false) { void probe(); return; }
  if (iframeEl.value?.contentWindow) {
    // A cross-origin frame's location.reload() throws: remount it instead.
    try { iframeEl.value.contentWindow.location.reload(); } catch { frameKey.value++; }
  } else if (committedUrl.value) {
    const u = committedUrl.value;
    committedUrl.value = "";
    setTimeout(() => { committedUrl.value = u; }, 0);
  }
}

function openExternal() {
  const url = committedUrl.value || normalizeUrl(inputUrl.value);
  if (url) shellOpen(url);
}

function onLoad() {
  // A local dev server is cross-origin to the app too, so its contentDocument
  // is always null — that says nothing about framing. Dev servers don't send
  // X-Frame-Options; only judge other sites by it.
  if (port.value !== null) return;
  // Try to detect X-Frame-Options block: cross-origin iframes still fire load
  // but contentDocument will be null or inaccessible.
  try {
    const doc = iframeEl.value?.contentDocument;
    if (doc === null) blocked.value = true;
  } catch {
    blocked.value = true;
  }
}

function onError() {
  blocked.value = true;
}

// ── "Nothing listening on localhost:N" ───────────────────────────────────────
// A local URL is probed (a loopback connect in Go) before it is framed, so a
// dead dev server shows why instead of WebKit's blank error page — and polled
// while this pane is on screen, so the page appears once the server is up.
const port = computed(() => localPort(committedUrl.value));
/** null = unknown (not probed yet, or not a local URL). */
const listening = ref<boolean | null>(null);
const nothingListening = computed(() => port.value !== null && listening.value === false);
const asked = ref(false);
let probeSeq = 0;

async function probe() {
  const p = port.value;
  if (p === null) { listening.value = null; return; }
  const seq = ++probeSeq;
  // No backend to ask (browser-only dev): frame it and let the page speak.
  const ok = await invoke<boolean>("port_listening", { port: p }).catch(() => true);
  if (seq !== probeSeq || p !== port.value) return;
  if (ok && listening.value === false) frameKey.value++; // came up: load it fresh
  listening.value = ok;
}

watch(committedUrl, () => { listening.value = null; asked.value = false; void probe(); }, { immediate: true });

// v-show hides an inactive pane (and the Right Panel when closed), which
// leaves it without an offsetParent — no polling for what nobody sees.
const timer = setInterval(() => {
  if (nothingListening.value && rootEl.value?.offsetParent && document.visibilityState === "visible") void probe();
}, 2000);
onBeforeUnmount(() => clearInterval(timer));

function askToStart() {
  if (port.value === null) return;
  asked.value = true;
  emit("askStart", port.value);
}
</script>

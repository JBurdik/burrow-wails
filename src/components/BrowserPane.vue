<template>
  <div class="flex h-full w-full flex-col overflow-hidden bg-panel">
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
        v-if="committedUrl"
        ref="iframeEl"
        class="block h-full w-full border-0 bg-white"
        :src="committedUrl"
        allow="clipboard-read; clipboard-write"
        @load="onLoad"
        @error="onError"
      />
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
import { ref, watch } from "vue";
import { PhArrowClockwise, PhArrowSquareOut, PhGlobe, PhProhibit } from "@phosphor-icons/vue";
import { open as shellOpen } from "@tauri-apps/plugin-shell";

const props = defineProps<{
  initialUrl?: string;
}>();

const inputUrl = ref(props.initialUrl ?? "");
const committedUrl = ref(props.initialUrl ?? "");
const blocked = ref(false);
const iframeEl = ref<HTMLIFrameElement | null>(null);

watch(() => props.initialUrl, (u) => {
  if (u) {
    inputUrl.value = u;
    go(u);
  }
});

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
  if (iframeEl.value?.contentWindow) {
    iframeEl.value.contentWindow.location.reload();
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
</script>

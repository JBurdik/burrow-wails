<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhArrowClockwise, PhArrowSquareOut, PhPlugsConnected, PhX } from "@phosphor-icons/vue";

const props = defineProps<{ cwd: string }>();

interface DevServer { pid: number; port: number; addr: string; command: string }

const servers = ref<DevServer[]>([]);
const loading = ref(false);
let timer: ReturnType<typeof setInterval> | undefined;

// Previews link straight to this Mac's tailnet IP — no proxy, no `tailscale
// serve`. A bare IP passes Vite's host check where a MagicDNS name doesn't.
const tailnetIp = ref<string | null>(null);
invoke<{ ip?: string | null }>("get_tailscale_status")
  .then((st) => { tailnetIp.value = st.ip ?? null; })
  .catch(() => {});

// lsof reports the bound address: `*:5173` / `100.x:5173` is reachable from
// the tailnet, `127.0.0.1:5173` / `[::1]:5173` only from this Mac.
function loopbackOnly(addr: string): boolean {
  return /^(127\.|\[::1\]|localhost)/.test(addr);
}

function previewUrl(s: DevServer): string {
  return `http://${tailnetIp.value}:${s.port}`;
}

async function refresh() {
  if (!props.cwd) { servers.value = []; return; }
  loading.value = true;
  try {
    servers.value = await invoke<DevServer[]>("list_dev_servers", { workspacePath: props.cwd });
  } finally {
    loading.value = false;
  }
}

async function kill(pid: number) {
  await invoke("kill_dev_server", { pid });
  await refresh();
}

watch(() => props.cwd, refresh, { immediate: true });
onMounted(() => { timer = setInterval(refresh, 3000); });
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col overflow-hidden text-[11px] text-secondary-foreground">
    <header class="flex items-center justify-between border-b border-border p-2">
      <span class="flex items-center gap-1.5 font-semibold text-foreground"><PhPlugsConnected :size="14" /> Dev servers</span>
      <button class="inline-flex p-0.5 text-muted-foreground hover:rounded hover:bg-hover hover:text-foreground disabled:opacity-40" :disabled="loading" title="Refresh" @click="refresh">
        <PhArrowClockwise :size="13" :class="{ 'animate-spin': loading }" />
      </button>
    </header>
    <div v-if="!props.cwd" class="p-4 text-center leading-relaxed text-muted-foreground">Open a workspace.</div>
    <div v-else-if="servers.length === 0" class="p-4 text-center leading-relaxed text-muted-foreground">No dev server is running in this workspace.</div>
    <div v-else class="flex-1 overflow-auto">
      <div v-for="s in servers" :key="s.pid" class="flex items-center gap-2 border-b border-border/65 px-2 py-1.5">
        <span class="font-mono text-accent">:{{ s.port }}</span>
        <span class="min-w-0 flex-1 truncate font-mono text-secondary-foreground" :title="s.command">{{ s.command }}</span>
        <span class="text-muted-foreground">pid {{ s.pid }}</span>
        <template v-if="tailnetIp">
          <span v-if="loopbackOnly(s.addr)" class="shrink-0 text-muted-foreground" title="Listens on localhost only, so other tailnet devices can't reach it. Start it with --host (Vite: server.host: true).">localhost only</span>
          <a v-else :href="previewUrl(s)" target="_blank" rel="noopener" class="inline-flex shrink-0 items-center gap-1 rounded border border-border px-1.5 py-0.5 text-foreground hover:bg-hover" :title="previewUrl(s)">
            <PhArrowSquareOut :size="11" /> Preview
          </a>
        </template>
        <button class="inline-flex shrink-0 items-center gap-1 rounded border border-border px-1.5 py-0.5 text-destructive hover:bg-hover" title="Kill" @click="kill(s.pid)">
          <PhX :size="11" /> Kill
        </button>
      </div>
    </div>
  </section>
</template>

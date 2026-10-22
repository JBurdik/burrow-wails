<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { PhCaretRight, PhGear, PhMagnifyingGlass, PhPlugs } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { useUIStore } from "@/stores/ui";
import { appTransport } from "@/lib/wailsCompat/core";
import { clearRemoteCredentials } from "@/runtime/remoteEndpoint";
import PhoneHeader from "./PhoneHeader.vue";

const ui = useUIStore();
const up = ref(false);
const version = ref("");
const off = appTransport().onState((v) => { up.value = v; });
onBeforeUnmount(off);
invoke<string>("get_app_version").then((v) => { version.value = v; }).catch(() => {});

function disconnect() {
  if (!confirm("Disconnect this device? You will need a new pairing code to connect again.")) return;
  clearRemoteCredentials();
  location.reload();
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-base">
    <PhoneHeader title="More" />
    <div class="flex-1 overflow-y-auto">
      <button class="more-row" @click="ui.openSettings()">
        <PhGear :size="20" class="text-secondary-foreground" /><span class="flex-1">Settings</span><PhCaretRight :size="15" class="text-muted-foreground" />
      </button>
      <button class="more-row" @click="ui.openSpotlight()">
        <PhMagnifyingGlass :size="20" class="text-secondary-foreground" /><span class="flex-1">Search</span><PhCaretRight :size="15" class="text-muted-foreground" />
      </button>
      <h2 class="px-4 pb-1 pt-5 text-[12px] font-semibold text-muted-foreground">Connection</h2>
      <div class="more-row">
        <PhPlugs :size="20" class="text-secondary-foreground" />
        <span class="flex-1">Desktop</span>
        <span class="flex items-center gap-1.5 text-[13px] text-muted-foreground">
          <span class="h-2 w-2 rounded-full" :class="up ? 'bg-success' : 'bg-destructive'" />{{ up ? "Connected" : "Reconnecting…" }}
        </span>
      </div>
      <button class="more-row !text-destructive" @click="disconnect"><span class="flex-1">Disconnect this device</span></button>
      <p v-if="version" class="px-4 py-6 text-center text-[12px] text-muted-foreground">Burrow {{ version }}</p>
    </div>
  </div>
</template>

<style scoped>
.more-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 52px;
  padding: 0 16px;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  font-size: 15px;
  color: var(--text-primary);
  text-align: left;
}
button.more-row:active { background: var(--bg-hover); }
</style>

<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { PhBell, PhCaretRight, PhGear, PhMagnifyingGlass, PhPlugs } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { useUIStore } from "@/stores/ui";
import { appTransport } from "@/lib/wailsCompat/core";
import { clearRemoteCredentials, loadRemoteCredentials } from "@/runtime/remoteEndpoint";
import { Switch } from "@/components/ui/switch";
import PhoneHeader from "./PhoneHeader.vue";

const ui = useUIStore();
const up = ref(false);
const version = ref("");
const off = appTransport().onState((v) => { up.value = v; });
onBeforeUnmount(off);
invoke<string>("get_app_version").then((v) => { version.value = v; }).catch(() => {});

// Web Push (src-wails/push.go). Only a paired device subscribes — the desktop
// has its own native notifications. iOS exposes Notification only to a PWA
// installed to the Home Screen, so its absence is the hint to install.
const paired = !!loadRemoteCredentials();
const canPush = typeof Notification !== "undefined" && "serviceWorker" in navigator && "PushManager" in window;
const pushOn = ref(false);
const pushBusy = ref(false);
const pushError = ref("");
if (paired && canPush) {
  navigator.serviceWorker.ready
    .then((r) => r.pushManager.getSubscription())
    .then((s) => { pushOn.value = !!s && Notification.permission === "granted"; })
    .catch(() => {});
}

function urlB64ToBytes(s: string): Uint8Array<ArrayBuffer> {
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (s.length % 4)) % 4));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

async function setPush(on: boolean) {
  pushBusy.value = true;
  pushError.value = "";
  try {
    const reg = await navigator.serviceWorker.ready;
    const old = await reg.pushManager.getSubscription();
    if (old) {
      await invoke("push_unsubscribe", { endpoint: old.endpoint }).catch(() => {});
      await old.unsubscribe();
    }
    if (on) {
      if ((await Notification.requestPermission()) !== "granted") throw new Error("Notifications are blocked for Burrow in this browser's settings.");
      const key = await invoke<string>("push_vapid_public_key");
      const sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: urlB64ToBytes(key) });
      await invoke("push_subscribe", { subscription: sub.toJSON() });
    }
    pushOn.value = on;
  } catch (e) {
    pushError.value = e instanceof Error ? e.message : String(e);
  } finally {
    pushBusy.value = false;
  }
}

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
      <template v-if="paired">
        <h2 class="px-4 pb-1 pt-5 text-[12px] font-semibold text-muted-foreground">Notifications</h2>
        <div v-if="canPush" class="more-row">
          <PhBell :size="20" class="text-secondary-foreground" />
          <span class="flex-1">Notify me when an agent needs me</span>
          <Switch :checked="pushOn" :disabled="pushBusy" @update:checked="setPush" />
        </div>
        <p v-else class="px-4 py-3 text-[13px] text-muted-foreground">To get notifications on iPhone, add Burrow to your Home Screen (Share → Add to Home Screen) and open it from there.</p>
        <p v-if="pushError" class="px-4 py-2 text-[13px] text-destructive">{{ pushError }}</p>
      </template>
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

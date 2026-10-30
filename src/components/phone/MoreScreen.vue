<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { PhCaretRight, PhGear, PhMagnifyingGlass, PhPencilSimple, PhPlugs, PhPlus, PhTrash } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { useUIStore } from "@/stores/ui";
import { appTransport, switchEnvironment } from "@/lib/wailsCompat/core";
import { activeEnvironmentId, listEnvironments, removeEnvironment, renameEnvironment } from "@/runtime/remoteEndpoint";
import PhoneHeader from "./PhoneHeader.vue";

const ui = useUIStore();
const up = ref(false);
const version = ref("");
const off = appTransport().onState((v) => { up.value = v; });
onBeforeUnmount(off);
invoke<string>("get_app_version").then((v) => { version.value = v; }).catch(() => {});

// One machine is connected at a time; tapping another one switches (reload).
const machines = ref(listEnvironments());
const activeId = activeEnvironmentId();
const hostLabel = (u: string) => u.replace(/^https?:\/\//, "");

function pick(id: string) {
  if (id !== activeId) switchEnvironment(id);
}

function rename(id: string, current: string) {
  const name = prompt("Machine name", current);
  if (name === null) return;
  renameEnvironment(id, name);
  machines.value = listEnvironments();
}

function forget(id: string, name: string) {
  if (!confirm(`Forget ${name}? You will need a new pairing code to connect to it again.`)) return;
  removeEnvironment(id);
  if (id === activeId) switchEnvironment(null);
  else machines.value = listEnvironments();
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
      <div v-for="m in machines" :key="m.environment_id" class="more-row">
        <button class="flex min-w-0 flex-1 items-center gap-3 self-stretch text-left" @click="pick(m.environment_id)">
          <PhPlugs :size="20" class="shrink-0 text-secondary-foreground" />
          <span class="flex min-w-0 flex-1 flex-col">
            <span class="truncate">{{ m.name }}</span>
            <span class="truncate text-[12px] text-muted-foreground">{{ hostLabel(m.baseUrl) }}</span>
          </span>
          <span v-if="m.environment_id === activeId" class="flex shrink-0 items-center gap-1.5 text-[13px] text-muted-foreground">
            <span class="h-2 w-2 rounded-full" :class="up ? 'bg-success' : 'bg-destructive'" />{{ up ? "Connected" : "Reconnecting…" }}
          </span>
        </button>
        <button class="p-1.5 text-muted-foreground" :aria-label="`Rename ${m.name}`" @click="rename(m.environment_id, m.name)"><PhPencilSimple :size="17" /></button>
        <button class="p-1.5 text-destructive" :aria-label="`Forget ${m.name}`" @click="forget(m.environment_id, m.name)"><PhTrash :size="17" /></button>
      </div>
      <button class="more-row" @click="switchEnvironment(null)">
        <PhPlus :size="20" class="text-secondary-foreground" /><span class="flex-1">Add machine</span><PhCaretRight :size="15" class="text-muted-foreground" />
      </button>
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

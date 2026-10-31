<template>
  <div class="window-app flex h-screen flex-col bg-[var(--terminal-bg)]">
    <!-- Hidden-inset titlebar: the traffic lights sit over this strip, which is
         also what drags the window. -->
    <div class="window-titlebar flex h-[30px] shrink-0 items-center justify-center text-[12px] text-muted-foreground" style="--wails-draggable: drag">
      {{ title }}
    </div>
    <div class="relative min-h-0 flex-1">
      <Settings v-if="kind === 'settings'" @close="closeSelf" />
      <XTerm v-else-if="kind === 'terminal'" :pty-id="leafId" :cwd="cwd" :initially-titled="true" />
      <AgentChat
        v-else-if="kind === 'chat' && chatId != null && ready"
        :chat-id="chatId"
        :workspace-id="wsId"
        :cwd="cwd"
        :is-watching="true"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import "@/styles/app.css";
import { defineAsyncComponent, onMounted, ref } from "vue";
import { Window } from "@wailsio/runtime";
import { hideSplash } from "@/lib/splash";
import { useUIStore } from "@/stores/ui";
import { useWorkspaceStore } from "@/stores/workspace";

// A secondary window: "#/window/<kind>/<id>?<params>" (src/lib/windows.ts).
const Settings = defineAsyncComponent(() => import("@/components/Settings.vue"));
const XTerm = defineAsyncComponent(() => import("@/components/XTerm.vue"));
const AgentChat = defineAsyncComponent(() => import("@/components/AgentChat.vue"));

const [path, query = ""] = location.hash.slice(1).split("?");
const parts = path.split("/").filter(Boolean); // ["window", kind, id?]
const kind = parts[1] ?? "";
const leafId = Number(parts[2] ?? 0);
const params = new URLSearchParams(query);
const wsId = Number(params.get("ws") ?? 0);
const chatId = params.has("chat") ? Number(params.get("chat")) : null;
const cwd = params.get("cwd") ?? "";
const title = kind === "settings" ? "Settings" : (params.get("title") ?? "Burrow");
const ready = ref(false);

const ui = useUIStore();
if (kind === "settings") {
  const section = params.get("section");
  if (section) ui.settingsSection = section;
  ui.settingsFocusId = params.get("focus") ?? "";
}

onMounted(async () => {
  document.title = title;
  await useWorkspaceStore().load().catch(() => {});
  ready.value = true;
  hideSplash();
});

function closeSelf() {
  void Window.Close();
}
</script>

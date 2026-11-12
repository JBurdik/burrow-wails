<template>
  <TrayPanel v-if="kind === 'tray'" />
  <TrayToast v-else-if="kind === 'traytoast'" />
  <div v-else class="window-app flex h-full flex-col bg-[var(--terminal-bg)]">
    <!-- Hidden-inset titlebar: the traffic lights sit over this strip, which is
         also what drags the window. -->
    <div v-if="kind !== 'settings'" class="window-titlebar relative flex h-[30px] shrink-0 items-center justify-center text-[12px] text-muted-foreground" style="--wails-draggable: drag">
      {{ title }}
      <!-- Mini chat (src/lib/miniChat.ts): keep on top + start a fresh chat. -->
      <div v-if="mini" class="absolute right-2 flex gap-0.5" style="--wails-draggable: no-drag">
        <button type="button" class="rounded p-1 hover:bg-hover hover:text-foreground" :class="onTop && 'text-foreground'" :title="onTop ? 'Keep on top: on' : 'Keep on top'" @click="setOnTop(!onTop)">
          <PhPushPin :size="13" :weight="onTop ? 'fill' : 'regular'" />
        </button>
        <button type="button" class="rounded p-1 hover:bg-hover hover:text-foreground" title="New mini chat" @click="newMiniChat">
          <PhPlus :size="13" />
        </button>
      </div>
    </div>
    <div class="relative min-h-0 flex-1">
      <Settings v-if="kind === 'settings'" windowed @close="closeSelf" />
      <XTerm v-else-if="kind === 'terminal'" :pty-id="leafId" :cwd="cwd" :initially-titled="true" />
      <!-- The whole Right Panel, following the main window's project and chat (rpWindow.ts). -->
      <RightPanel
        v-else-if="kind === 'rightpanel' && ready"
        class="absolute inset-0 !w-full"
        :cwd="follow.cwd"
        :workspace-id="follow.workspaceId"
        :is-git="follow.isGit"
        :active-chat="follow.chatId"
        :open="true"
        @close-panel="closeSelf"
        @open-panel="() => {}"
      />
      <GitWindow v-else-if="kind === 'git' && ready" :initial-ws-id="wsId || undefined" class="absolute inset-0" />
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
import { defineAsyncComponent, onBeforeUnmount, onMounted, ref } from "vue";
import { Window } from "@wailsio/runtime";
import { hideSplash } from "@/lib/splash";
import { useUIStore } from "@/stores/ui";
import { useWorkspaceStore } from "@/stores/workspace";
import { readFollow, RP_FOLLOW_KEY, type RpFollow } from "@/lib/rpWindow";
import { PhPlus, PhPushPin } from "@phosphor-icons/vue";
import { miniChat } from "@/lib/miniChat";

// A secondary window: "#/window/<kind>/<id>?<params>" (src/lib/windows.ts).
const Settings = defineAsyncComponent(() => import("@/components/Settings.vue"));
const XTerm = defineAsyncComponent(() => import("@/components/XTerm.vue"));
const AgentChat = defineAsyncComponent(() => import("@/components/AgentChat.vue"));
const GitWindow = defineAsyncComponent(() => import("@/components/git/GitWindow.vue"));
const RightPanel = defineAsyncComponent(() => import("@/components/RightPanel.vue"));
const TrayPanel = defineAsyncComponent(() => import("@/components/TrayPanel.vue"));
const TrayToast = defineAsyncComponent(() => import("@/components/TrayToast.vue"));

const [path, query = ""] = location.hash.slice(1).split("?");
const parts = path.split("/").filter(Boolean); // ["window", kind, id?]
const kind = parts[1] ?? "";
const leafId = Number(parts[2] ?? 0);
const params = new URLSearchParams(query);
const wsId = Number(params.get("ws") ?? 0);
const chatId = params.has("chat") ? Number(params.get("chat")) : null;
const cwd = params.get("cwd") ?? "";
const title = kind === "settings" ? "Settings" : kind === "git" ? "Git" : kind === "rightpanel" ? "Right panel" : (params.get("title") ?? "Burrow");

// kind "rightpanel": what the main window has active, kept current by its writes.
const follow = ref<RpFollow>(readFollow());
function onStorage(e: StorageEvent) {
  if (e.key === RP_FOLLOW_KEY) follow.value = readFollow();
}
if (kind === "rightpanel") window.addEventListener("storage", onStorage);
onBeforeUnmount(() => window.removeEventListener("storage", onStorage));
const ready = ref(false);

// Mini chat window: pin state is per device, re-applied on every open.
const mini = params.get("mini") === "1";
const ON_TOP_KEY = "burrow.miniChatOnTop";
const onTop = ref(mini && localStorage.getItem(ON_TOP_KEY) === "1");
function setOnTop(on: boolean) {
  onTop.value = on;
  localStorage.setItem(ON_TOP_KEY, on ? "1" : "0");
  void Window.SetAlwaysOnTop(on);
}
async function newMiniChat() {
  const m = await miniChat(true);
  location.hash = "#" + m.route;
  location.reload(); // ponytail: the window reads its route once at load
}

const ui = useUIStore();
if (kind === "settings") {
  const section = params.get("section");
  if (section) ui.settingsSection = section;
  ui.settingsFocusId = params.get("focus") ?? "";
}

// ⌘W closes any secondary window, Esc closes Settings. Not a native menu
// accelerator: that would also swallow ⌘W (close pane) in the main window.
function onKeydown(e: KeyboardEvent) {
  const cmdW = e.metaKey && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "w";
  const esc = kind === "settings" && e.key === "Escape" && !e.defaultPrevented;
  if (cmdW || esc) {
    e.preventDefault();
    closeSelf();
  }
}
onBeforeUnmount(() => window.removeEventListener("keydown", onKeydown));

onMounted(async () => {
  window.addEventListener("keydown", onKeydown);
  document.title = title;
  if (onTop.value) void Window.SetAlwaysOnTop(true);
  await useWorkspaceStore().load().catch(() => {});
  ready.value = true;
  hideSplash();
});

function closeSelf() {
  void Window.Close();
}
</script>

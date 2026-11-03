<script setup lang="ts">
// The macOS permissions Burrow asks for, in one list (onboarding + Settings).
// Modelled on Rex: each grant names the process macOS holds responsible — the
// core for chats, burrow-daemon for terminals — reads its state from that
// process (privacy_status; reading never prompts) and, for Full Disk Access,
// opens System Settings with the icon floating beside it to drag in.
import { computed, onMounted, onUnmounted, ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhBell, PhCheckCircle, PhHardDrives, PhPower, PhTerminalWindow } from "@phosphor-icons/vue";
import { useUIStore } from "@/stores/ui";
import { isDesktop } from "@/lib/wailsCompat/desktop";
import { CloseGrantHelper, CoreAtLogin, ExternalCore, OpenSystemSettings, ShowGrantHelper } from "@/lib/wailsCompat/bindings/burrow/desktopservice";

type Status = {
  notifications: string;
  bundle_id: string;
  chats_fda: string;
  chats_path: string;
  terminals_fda: string;
  terminals_path: string;
};

const FDA_PANE = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles";
const DRAG_HINT = "Drag this icon into the list in System Settings to grant Full Disk Access.";

const ui = useUIStore();
const st = ref<Status | null>(null);
const externalCore = ref(false);
const login = ref("");
const helperFor = ref<"" | "chats" | "terminals">("");
let timer = 0;

async function refresh() {
  try {
    st.value = await invoke<Status>("privacy_status");
  } catch {
    st.value = null;
  }
  if (externalCore.value) login.value = await CoreAtLogin("").catch(() => "");
  // Granted while the drag panel is up: it did its job.
  if (helperFor.value && st.value && st.value[`${helperFor.value}_fda` as "chats_fda" | "terminals_fda"] === "granted") {
    helperFor.value = "";
    void CloseGrantHelper().catch(() => {});
  }
}

onMounted(async () => {
  if (!isDesktop()) return;
  externalCore.value = await ExternalCore().catch(() => false);
  await refresh();
  timer = window.setInterval(refresh, 2000); // grants happen in System Settings
});
onUnmounted(() => {
  window.clearInterval(timer);
  if (helperFor.value) void CloseGrantHelper().catch(() => {});
});

async function allowNotifications() {
  if (st.value?.notifications === "denied") {
    const id = st.value.bundle_id ? `?id=${encodeURIComponent(st.value.bundle_id)}` : "";
    await OpenSystemSettings(`x-apple.systempreferences:com.apple.Notifications-Settings.extension${id}`);
    return;
  }
  await invoke("request_notifications").catch(() => {});
  await refresh();
}

function grant(which: "chats" | "terminals") {
  const path = which === "chats" ? st.value?.chats_path : st.value?.terminals_path;
  if (!path) return;
  helperFor.value = which;
  void ShowGrantHelper(path, FDA_PANE, "Full Disk Access", DRAG_HINT);
}

async function setLogin(on: boolean) {
  login.value = await CoreAtLogin(on ? "on" : "off").catch((e: unknown) => `error: ${e}`);
  ui.coreAtLogin = on && (login.value === "on" || login.value === "needs-approval");
  if (login.value === "needs-approval") await OpenSystemSettings("x-apple.systempreferences:com.apple.LoginItems-Settings.extension");
}

const rows = computed(() => {
  const s = st.value;
  if (!s) return [];
  const out: { key: string; icon: unknown; title: string; body: string; state: string; action?: string; run?: () => void }[] = [];
  if (s.notifications !== "webview") {
    out.push({
      key: "notifications",
      icon: PhBell,
      title: "Notifications",
      body: "Tells you when an agent needs an approval or finishes, even with the window closed.",
      state: s.notifications,
      action: s.notifications === "granted" ? undefined : s.notifications === "denied" ? "Open Settings" : "Allow",
      run: allowNotifications,
    });
  }
  out.push({
    key: "chats",
    icon: PhHardDrives,
    title: "Full Disk Access for chats",
    body: "Optional. Agents in chats can read and edit files in Documents, Desktop or iCloud without asking per folder.",
    state: s.chats_fda,
    action: s.chats_fda === "granted" ? undefined : "Grant…",
    run: () => grant("chats"),
  });
  out.push({
    key: "terminals",
    icon: PhTerminalWindow,
    title: "Full Disk Access for terminals",
    body: s.terminals_fda === "unknown"
      ? "Optional. Shown once the terminal service restarts onto this version."
      : "Optional. Terminal tabs run under burrow-daemon, which macOS asks about separately.",
    state: s.terminals_fda,
    action: s.terminals_fda === "denied" ? "Grant…" : undefined,
    run: () => grant("terminals"),
  });
  return out;
});

const loginHint = computed(() => {
  const l = login.value;
  if (l === "needs-approval") return "Allow it in System Settings → General → Login Items.";
  if (l === "unavailable") return "Not available in this build.";
  if (l === "unsupported") return "Needs macOS 13 or later.";
  return l.startsWith("error") ? l : "";
});
</script>

<template>
  <div v-if="rows.length" class="perm-list">
    <div v-for="r in rows" :key="r.key" class="perm-row">
      <span class="perm-icon"><component :is="r.icon" :size="17" /></span>
      <span class="perm-text">
        <strong>{{ r.title }}</strong>
        <small>{{ r.body }}</small>
      </span>
      <span v-if="r.state === 'granted'" class="perm-ok"><PhCheckCircle :size="15" weight="fill" /> On</span>
      <button v-else-if="r.action" class="perm-btn" type="button" @click="r.run?.()">{{ r.action }}</button>
    </div>
    <div v-if="externalCore" class="perm-row">
      <span class="perm-icon"><PhPower :size="17" /></span>
      <span class="perm-text">
        <strong>Start Burrow at login</strong>
        <small>Chats, schedules and the phone keep working from the moment you log in, before the window is opened.<template v-if="loginHint"> {{ loginHint }}</template></small>
      </span>
      <span v-if="login === 'on'" class="perm-ok"><PhCheckCircle :size="15" weight="fill" /> On</span>
      <button v-if="login === 'on' || login === 'needs-approval'" class="perm-btn quiet" type="button" @click="setLogin(false)">Turn off</button>
      <button v-else class="perm-btn" type="button" @click="setLogin(true)">Turn on</button>
    </div>
  </div>
</template>

<style scoped>
.perm-list { display: grid; gap: 8px; }
.perm-row { display: flex; align-items: center; gap: 12px; min-height: 58px; border: 1px solid var(--border); border-radius: 7px; background: var(--bg-panel); padding: 10px 12px; }
.perm-icon { display: grid; flex: none; width: 31px; height: 31px; place-items: center; border-radius: 6px; background: var(--bg-hover); color: var(--accent); }
.perm-text { display: grid; flex: 1; min-width: 0; gap: 2px; }
.perm-text strong { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.perm-text small { font-size: 11px; line-height: 1.45; color: var(--text-muted); }
.perm-ok { display: inline-flex; flex: none; align-items: center; gap: 5px; color: var(--green); font-size: 12px; font-weight: 600; }
.perm-btn { flex: none; border: 1px solid var(--accent); border-radius: 6px; background: var(--accent); color: var(--bg-solid, var(--bg-base)); cursor: pointer; font: inherit; font-size: 12px; font-weight: 600; padding: 6px 11px; }
.perm-btn:hover { filter: brightness(1.08); }
.perm-btn.quiet { border-color: var(--border); background: transparent; color: var(--text-secondary); }
</style>

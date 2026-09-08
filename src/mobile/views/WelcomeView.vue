<script setup lang="ts">
// The "start a new chat" entry screen — modeled on the pen.dev "Screen -
// Welcome (Mobile)" frame. This is what ChatsView.vue's cramped inline
// "NOVÁ KONVERZACE" form (a bare workspace <Select> + a button, agent
// hardcoded to "claude") is replaced by: a real composer that actually lets
// picking a model (which implies the agent/provider), a reasoning effort and
// a permission mode before the first message ever goes out.
//
// No Go changes needed: claude_start's remote-callable args already include
// model/effort/permissionMode (src-wails/remoteapi.go) — sendChat() in
// ../store.ts just used to hardcode them empty. This view is what finally
// fills them in, via RemoteChat.initialModel/initialEffort/initialPermissionMode.
import { computed, onMounted, ref, type Component } from "vue";
import { Bot, Sparkles, Gauge, ShieldCheck, ChevronDown, Paperclip, ArrowUp, Folder, Check } from "lucide-vue-next";
import { agentIconComp } from "@/lib/agentIcons";
import { providerFor } from "@/lib/providers";
import { modelsFor, effortLabel } from "@/lib/chatModels";
import { getConfig, setConfig, configReady } from "@/lib/config";
import { useRemoteStore } from "../store";

const store = useRemoteStore();

// ── permission mode ──────────────────────────────────────────────────────
// Mirrors AgentChat.vue's own PermMode type and PERM_META label/description
// text verbatim (not reinvented copy) — "Supervised" is the pen.dev mockup's
// example value and it corresponds to AgentChat.vue's "default" mode.
type PermMode = "default" | "auto" | "acceptEdits" | "plan" | "dontAsk" | "bypassPermissions";
const PERM_MODES: PermMode[] = ["default", "auto", "acceptEdits", "plan", "dontAsk", "bypassPermissions"];
const PERM_META: Record<PermMode, { label: string; description: string }> = {
  default: { label: "Supervised", description: "Ask before commands and file changes." },
  auto: { label: "Auto", description: "Claude decides which routine actions can proceed." },
  acceptEdits: { label: "Auto-accept edits", description: "Auto-approve edits, ask before other actions." },
  plan: { label: "Plan mode", description: "Plan only until you approve implementation." },
  dontAsk: { label: "Don't ask", description: "Run edits and commands without routine prompts." },
  bypassPermissions: { label: "Full access", description: "Skip all permission checks." },
};
function isPermMode(v: unknown): v is PermMode {
  return typeof v === "string" && (PERM_MODES as string[]).includes(v);
}
// Same config key + shape as AgentChat.vue's chatPermissionMode (byChat is
// per-chat and only meaningful once a chat exists; only `last` is read/written
// here, so a chat created from Welcome sets the SAME "last used mode" that a
// brand-new chat opened any other way already inherits from).
interface ChatPermissionModeConfig {
  byChat: Record<string, string>;
  last?: string;
  dangerousByChat: Record<string, boolean>;
}
function loadLastPermMode(): PermMode {
  const cfg = getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} });
  return isPermMode(cfg.last) ? cfg.last : "default";
}
function saveLastPermMode(mode: PermMode) {
  const cfg = { ...getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} }) };
  cfg.last = mode;
  setConfig("chatPermissionMode", cfg);
}

// ── picker state ─────────────────────────────────────────────────────────
const agentKind = ref<"claude" | "codex">("claude");
const modelId = ref(modelsFor("claude")[0]?.id ?? "");
const effort = ref(modelsFor("claude")[0]?.defaultEffort ?? "");
const permMode = ref<PermMode>("default");
const workspaceId = ref<number | null>(null);
const prompt = ref("");
const creating = ref(false);
const error = ref("");

const currentEfforts = computed(() => modelsFor(agentKind.value).find((m) => m.id === modelId.value)?.efforts ?? []);

// Model chip is really a provider+model picker (mobile can only start a chat
// as "codex" | "claude" — matches remote_create_chat's Go-side support), so
// its label carries the provider when the model itself doesn't ("Codex —
// Default"), and is just the model's own name when it already does
// ("Claude Opus 5").
const modelChipLabel = computed(() => {
  const m = modelsFor(agentKind.value).find((x) => x.id === modelId.value);
  if (!m) return "Model";
  return m.label === "Default" ? `${providerFor(agentKind.value).label} — Default` : m.label;
});

interface ModelRow { key: string; agentKind: "claude" | "codex"; modelId: string; label: string; sub: string; icon: Component; color: string; }
const modelRows = computed<ModelRow[]>(() => {
  const rows: ModelRow[] = [];
  for (const kind of ["claude", "codex"] as const) {
    const provider = providerFor(kind);
    for (const m of modelsFor(kind)) {
      rows.push({
        key: `${kind}:${m.id}`,
        agentKind: kind,
        modelId: m.id,
        label: m.label === "Default" ? `${provider.label} — Default` : m.label,
        sub: provider.label,
        icon: agentIconComp(provider.icon),
        color: provider.color,
      });
    }
  }
  return rows;
});

function selectModel(row: ModelRow) {
  agentKind.value = row.agentKind;
  modelId.value = row.modelId;
  // A model swap can invalidate the previously chosen effort (a different
  // model publishes a different effort list, or none at all) — reset to the
  // new model's own default rather than carrying over a stale value.
  effort.value = modelsFor(row.agentKind).find((m) => m.id === row.modelId)?.defaultEffort ?? "";
  sheet.value = null;
}

function selectPermMode(mode: PermMode) {
  permMode.value = mode;
  saveLastPermMode(mode);
  sheet.value = null;
}

// ── generic bottom sheet (model / effort / permission / workspace) ───────
// One small sheet rather than four integrations of Select.vue: three of
// these pickers need a provider icon + sub-label per row, which Select.vue's
// flat {value,label} shape has no slot for, and a phone screen only ever
// shows one of these four lists at a time anyway.
type SheetKind = "model" | "effort" | "permission" | "workspace" | null;
const sheet = ref<SheetKind>(null);
function openSheet(kind: Exclude<SheetKind, null>) {
  sheet.value = kind;
}

interface SheetRow { key: string; label: string; sub?: string; icon?: Component; color?: string; selected: boolean; pick: () => void; }

const sheetTitle = computed(() => ({
  model: "Model",
  effort: "Reasoning effort",
  permission: "Režim oprávnění",
  workspace: "Projekt",
}[sheet.value ?? "model"] ?? ""));

const sheetRows = computed<SheetRow[]>(() => {
  if (sheet.value === "model") {
    return modelRows.value.map((row) => ({
      key: row.key,
      label: row.label,
      sub: row.sub,
      icon: row.icon,
      color: row.color,
      selected: row.agentKind === agentKind.value && row.modelId === modelId.value,
      pick: () => selectModel(row),
    }));
  }
  if (sheet.value === "effort") {
    return currentEfforts.value.map((e) => ({
      key: e,
      label: effortLabel(e),
      selected: e === effort.value,
      pick: () => { effort.value = e; sheet.value = null; },
    }));
  }
  if (sheet.value === "permission") {
    return PERM_MODES.map((mode) => ({
      key: mode,
      label: PERM_META[mode].label,
      sub: PERM_META[mode].description,
      selected: mode === permMode.value,
      pick: () => selectPermMode(mode),
    }));
  }
  if (sheet.value === "workspace") {
    return store.workspaces.map((w) => ({
      key: String(w.id),
      label: w.name,
      sub: w.path,
      selected: w.id === workspaceId.value,
      pick: () => { workspaceId.value = w.id; sheet.value = null; },
    }));
  }
  return [];
});

// "Current checkout" is the pen.dev mockup's own placeholder copy (a design
// tool has no real workspace to show) — used here as the literal fallback
// before a workspace is picked, then replaced by the real name once one is.
const workspaceChipLabel = computed(() => store.workspaces.find((w) => w.id === workspaceId.value)?.name ?? "Current checkout");

// Branch is deliberately NOT rendered: the pen.dev footer also shows a branch
// name next to the workspace, but WorkspaceGroup (../store.ts) only carries
// id/name/path/tabs — no branch — and inventing one would be worse than
// omitting the element. See the report for this call.

const canSubmit = computed(() => !!workspaceId.value && !!prompt.value.trim() && !creating.value);

async function submit() {
  error.value = "";
  if (!workspaceId.value) { error.value = "Vyber projekt."; return; }
  const text = prompt.value.trim();
  if (!text) return;
  creating.value = true;
  try {
    const chat = await store.createChat(workspaceId.value, agentKind.value);
    chat.initialModel = modelId.value;
    chat.initialEffort = currentEfforts.value.length ? effort.value : "";
    chat.initialPermissionMode = permMode.value;
    store.openChat(chat);
    await store.sendChat(text);
  } catch (e: any) {
    error.value = e?.message ?? "Chat se nepodařilo vytvořit.";
  } finally {
    creating.value = false;
  }
}

onMounted(async () => {
  if (!store.workspaces.length) await store.loadSessions();
  if (workspaceId.value === null) workspaceId.value = store.workspaces[0]?.id ?? null;
  await configReady;
  permMode.value = loadLastPermMode();
});
</script>

<template>
  <div class="welcome-screen">
    <button class="welcome-back" type="button" @click="store.showChats" aria-label="Zpět na konverzace">‹</button>

    <div class="welcome-center">
      <div class="logo-lockup">
        <span class="logo-mark" aria-hidden="true"><Bot :size="15" /></span>
        <span class="logo-word">Burrow</span>
      </div>
      <h1 class="welcome-title">Vítej v Burrow</h1>
      <p class="welcome-subtitle">Vše, co potřebuješ ke spuštění a řízení agentů, je tady.</p>

      <form class="compose-box" @submit.prevent="submit">
        <textarea
          v-model="prompt"
          class="compose-input"
          rows="3"
          placeholder="Popiš požadované změny, pošli navazující zprávu nebo přilož obrázky"
        />

        <div class="compose-toolbar">
          <div class="toolbar-group">
            <button type="button" class="tb-chip" @click="openSheet('model')">
              <Sparkles :size="14" /><span>{{ modelChipLabel }}</span><ChevronDown :size="12" class="tb-chip-caret" />
            </button>
            <button v-if="currentEfforts.length" type="button" class="tb-chip" @click="openSheet('effort')">
              <Gauge :size="14" /><span>{{ effortLabel(effort) }} effort</span><ChevronDown :size="12" class="tb-chip-caret" />
            </button>
            <button type="button" class="tb-chip" @click="openSheet('permission')">
              <ShieldCheck :size="14" /><span>{{ PERM_META[permMode].label }}</span><ChevronDown :size="12" class="tb-chip-caret" />
            </button>
          </div>
          <div class="toolbar-group">
            <!-- Decorative only — mobile has no image-attach plumbing yet.
                 Rendered inert (no handler, unfocusable) rather than disabled,
                 so it doesn't pick up a greyed-out "disabled button" look it
                 wasn't given in the design. -->
            <span class="tb-chip tb-chip--inert" aria-hidden="true">
              <Paperclip :size="14" /><ChevronDown :size="12" class="tb-chip-caret" />
            </span>
            <button type="submit" class="submit-btn" :disabled="!canSubmit" aria-label="Odeslat">
              <ArrowUp :size="16" />
            </button>
          </div>
        </div>

        <div class="compose-divider" />

        <div class="compose-footer">
          <button type="button" class="footer-chip" @click="openSheet('workspace')">
            <Folder :size="12" /><span>{{ workspaceChipLabel }}</span><ChevronDown :size="12" />
          </button>
        </div>
      </form>

      <p v-if="error" class="welcome-error">{{ error }}</p>
    </div>

    <Teleport to="body">
      <div v-if="sheet" class="sheet-backdrop" @click="sheet = null">
        <div class="sheet" @click.stop>
          <p class="sheet-title">{{ sheetTitle }}</p>
          <div class="sheet-rows">
            <button v-for="row in sheetRows" :key="row.key" type="button" class="sheet-row" @click="row.pick">
              <span v-if="row.icon" class="sheet-row-icon" :style="row.color ? { color: row.color } : undefined" aria-hidden="true">
                <component :is="row.icon" :size="16" />
              </span>
              <span class="sheet-row-main">
                <strong>{{ row.label }}</strong>
                <small v-if="row.sub">{{ row.sub }}</small>
              </span>
              <Check v-if="row.selected" :size="14" class="sheet-row-check" />
            </button>
            <p v-if="!sheetRows.length" class="sheet-empty">Nic k výběru.</p>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.welcome-screen {
  position: relative;
  flex: 1;
  min-height: 100dvh;
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding: calc(var(--safe-top) + 16px) 20px calc(var(--safe-bottom) + 16px);
}
.welcome-back {
  position: absolute;
  top: calc(var(--safe-top) + 12px);
  left: 16px;
  width: 36px;
  height: 36px;
  display: grid;
  place-items: center;
  border: 0;
  background: none;
  color: var(--text-secondary);
  font-size: 20px;
}
.welcome-center { display: flex; flex-direction: column; align-items: center; gap: 14px; text-align: center; }

.logo-lockup { display: flex; align-items: center; gap: 8px; }
.logo-mark {
  width: 26px; height: 26px;
  flex-shrink: 0;
  display: grid; place-items: center;
  border-radius: 8px;
  background: var(--accent);
  color: #fff;
}
.logo-word { font-family: var(--font-ui); font-size: 14px; font-weight: 600; color: var(--text-primary); }

.welcome-title { margin: 6px 0 0; font-family: var(--font-ui); font-size: 22px; font-weight: 700; color: var(--text-primary); letter-spacing: -.01em; }
.welcome-subtitle { margin: 0; max-width: 300px; font-size: 13px; line-height: 1.4; color: var(--text-secondary); }

.compose-box {
  width: 100%;
  margin-top: 10px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 16px;
  background: var(--bg-panel);
  text-align: left;
}
.compose-input {
  width: 100%;
  resize: none;
  border: 0;
  background: transparent;
  color: var(--text-primary);
  font: 14px/1.4 var(--font-ui);
  padding: 0;
}
.compose-input::placeholder { color: var(--text-muted); }
.compose-input:focus { outline: none; }

.compose-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.toolbar-group { display: flex; align-items: center; gap: 14px; min-width: 0; }

/* Borderless, unlike AgentChat.vue's pill-chip toolbar — only the outer
   Compose Box card is bordered here, per the pen.dev spec. */
.tb-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 0;
  background: none;
  padding: 0;
  color: var(--text-secondary);
  font: 500 12px/1 var(--font-ui);
  white-space: nowrap;
  overflow: hidden;
}
.tb-chip span { overflow: hidden; text-overflow: ellipsis; }
.tb-chip-caret { color: var(--text-muted); flex-shrink: 0; }
.tb-chip--inert { color: var(--text-secondary); pointer-events: none; }
.tb-chip:active { opacity: .7; }

.submit-btn {
  width: 32px; height: 32px;
  flex-shrink: 0;
  display: grid; place-items: center;
  border: 0;
  border-radius: 50%;
  background: var(--accent);
  color: #fff;
}
.submit-btn:disabled { opacity: .4; }
.submit-btn:not(:disabled):active { opacity: .8; }

.compose-divider { height: 1px; background: var(--border); }

.compose-footer { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.footer-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 0;
  background: none;
  padding: 0;
  color: var(--text-muted);
  font: 12px var(--font-ui);
  min-width: 0;
}
.footer-chip span { max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.welcome-error { margin: 10px 0 0; color: var(--red); font-size: 12px; text-align: center; }

/* ── bottom sheet ── */
.sheet-backdrop {
  position: fixed; inset: 0;
  z-index: 1200;
  display: flex;
  align-items: flex-end;
  background: rgba(0, 0, 0, .5);
}
.sheet {
  width: 100%;
  max-height: 70vh;
  overflow-y: auto;
  padding: 14px 14px calc(14px + var(--safe-bottom));
  border-radius: 20px 20px 0 0;
  background: var(--bg-panel);
  border-top: 1px solid var(--border);
}
.sheet-title { margin: 0 0 10px; padding: 0 4px; color: var(--text-muted); font: 700 11px var(--font-mono); letter-spacing: .06em; text-transform: uppercase; }
.sheet-rows { display: flex; flex-direction: column; gap: 4px; }
.sheet-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 8px;
  border: 0;
  border-radius: 10px;
  background: none;
  color: var(--text-primary);
  text-align: left;
}
.sheet-row:active { background: var(--bg-hover); }
.sheet-row-icon {
  width: 28px; height: 28px;
  flex-shrink: 0;
  display: grid; place-items: center;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-hover);
}
.sheet-row-main { flex: 1; min-width: 0; display: grid; gap: 2px; }
.sheet-row-main strong { font-size: 14px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sheet-row-main small { font-size: 11px; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sheet-row-check { flex-shrink: 0; color: var(--accent); }
.sheet-empty { margin: 0; padding: 10px 8px; color: var(--text-muted); font-size: 12px; }
</style>

import { ref, computed, watch } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import {
  THEMES,
  THEME_FAMILIES,
  DEFAULT_THEME_KEY,
  DEFAULT_FAMILY_KEY,
  findTheme,
  findFamily,
  familyOf,
  variantFor,
} from "@/themes";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";
import { useWorkspaceStore } from "@/stores/workspace";
import { router, tabsOrWelcome, workspaceRoute } from "@/router";
import { useTerminalTabsStore } from "@/stores/terminalTabs";

function hexToRgba(hex: string, alpha: number): string {
  const h = hex.replace("#", "");
  if (h.length !== 6) return hex;
  const r = parseInt(h.substring(0, 2), 16);
  const g = parseInt(h.substring(2, 4), 16);
  const b = parseInt(h.substring(4, 6), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}

const PREFS_KEY = "agentic-ide.prefs"; // legacy localStorage key, migrated once into config.json
const CONFIG_KEY = "uiPrefs";

// Font family presets. `value` is the CSS font-family stack applied.
export interface FontPreset {
  label: string;
  value: string;
}

export const UI_FONTS: FontPreset[] = [
  { label: "System Default", value: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif' },
  { label: "Inter", value: 'Inter, -apple-system, sans-serif' },
  { label: "Roboto", value: 'Roboto, -apple-system, sans-serif' },
  { label: "Helvetica Neue", value: '"Helvetica Neue", Helvetica, Arial, sans-serif' },
  { label: "Georgia (serif)", value: 'Georgia, "Times New Roman", serif' },
];

export const TERMINAL_FONTS: FontPreset[] = [
  { label: "JetBrains Mono", value: '"JetBrains Mono", monospace' },
  { label: "Fira Code", value: '"Fira Code", monospace' },
  { label: "Cascadia Code", value: '"Cascadia Code", monospace' },
  { label: "SF Mono", value: '"SF Mono", ui-monospace, monospace' },
  { label: "Menlo", value: 'Menlo, Monaco, monospace' },
  { label: "Courier New", value: '"Courier New", monospace' },
];

interface Prefs {
  uiFont: string;
  uiFontSize: number;
  uiScale: number;
  terminalFont: string;
  terminalFontSize: number;
  swapPanels: boolean;
  rightPanelVisible: boolean;
  theme: string;
  themeMode: "system" | "light" | "dark"; // which half of the theme pair is live
  // Theme FAMILY keys (see themes.ts). The live variant is the one this family
  // provides for the resolved scheme, so "Toggle Dark/Light Mode" and "system"
  // stay inside the user's chosen designs.
  preferredDarkTheme: string;
  preferredLightTheme: string;
  soundEnabled: boolean;
  soundDoneEnabled: boolean;
  soundWaitingEnabled: boolean;
  soundDoneId: string; // builtin id or "custom"
  soundDoneCustomPath: string;
  soundWaitingId: string;
  soundWaitingCustomPath: string;
  soundVolume: number; // 0-100
  maxAgents: number; // soft per-workspace sub-agent cap for the /burrow skill
  mcpMaxDepth: number; // recursion depth cap for Burrow MCP spawning tools
  debugOverlay: boolean; // show the per-terminal diagnostic overlay (XTerm.vue)
  floatCorner: string; // which screen corner floating windows snap+stack to
  worktreesDir: string; // parent dir for git worktrees: <dir>/<repo>/<branch>
  mode: "terminal" | "claude" | "dashboard"; // active main-pane mode
  bgImagePath: string; // absolute path to user wallpaper (empty = none)
  bgOpacity: number; // 0–1 opacity of panels/terminal over the wallpaper
  // Per-element backdrop-blur radius in px (0 = off). Separate so each surface
  // tunes its own frosted-glass strength over the wallpaper.
  blurPanels: number; // sidebar, activity bar, right panel, title bar
  blurContent: number; // Dashboard cards
  blurTerminal: number; // terminal panes
  blurOverlay: number; // spotlight, settings, modal composers
  blurDropdown: number; // titlebar popup menus (notifications, stats, branch)
  // ── Integrations: ntfy.sh push notifications ──
  ntfyEnabled: boolean; // master toggle for the ntfy integration
  ntfyServer: string; // base server URL (default https://ntfy.sh)
  ntfyTopic: string; // topic to publish to
  ntfyToken: string; // optional access token (Bearer) for protected topics
  ntfyEvents: NtfyEvent[]; // which agent transitions push a notification
  ntfyOnlyWhenAway: boolean; // only push when the app window is unfocused
  // ── Plugins: Terminal Pets ──
  petsEnabled: boolean; // master toggle for the free-roam pixel critters overlay
  petsSpeech: boolean; // pets squeak status quips in speech bubbles
  petsLeveling: boolean; // pets grow + earn hats the more turns their agent finishes
  // ── Floating mission-control chat ──
  floatChatEnabled: boolean; // master toggle for the bottom-right control chat button
  floatChatOpen: boolean; // expanded (true) vs collapsed to a button (false)
  sidebarVisible: boolean; // left sidebar shown (default off — opens via ⌘B / titlebar toggle)
  sidebarWidth: number; // left sidebar panel width in px
  rightPanelWidth: number; // right panel width in px
  toastPosition: ToastPosition; // screen anchor for toast notifications
  defaultChatAgent: string; // default agent for new chat sessions (chatAgents store id)
  spawnMode: "terminal" | "chat"; // how `burrow spawn` sub-agents open: a terminal tab or an ACP chat
  // Background text generation (commit messages, PR content, branch names,
  // chat titles) — "kind::provider::model::effort", effort optional.
  textGenerationModel: string;
  textGenerationPolicy: TextGenerationPolicy; // house style folded into every generation prompt
}

// t3code's TextGenerationPolicyKind, minus "custom" — that one needs per-op
// instruction text, and nothing asks for it yet.
export type TextGenerationPolicy = "default" | "conventional_commits" | "repo_conventions";
export const TEXT_GENERATION_POLICIES: { id: TextGenerationPolicy; label: string; description: string }[] = [
  { id: "default", label: "Default", description: "Plain imperative subjects, no house style imposed." },
  { id: "conventional_commits", label: "Conventional Commits", description: "feat/fix/chore prefixes, scope only when the diff makes it obvious." },
  { id: "repo_conventions", label: "Match this repository", description: "Shows the model your last 20 commit subjects and asks it to follow them." },
];

// Screen anchor for the toast stack (ToastStack.vue).
export type ToastPosition =
  | "top-left" | "top-center" | "top-right"
  | "bottom-left" | "bottom-center" | "bottom-right";
export const TOAST_POSITIONS: { id: ToastPosition; label: string }[] = [
  { id: "top-left", label: "Top left" },
  { id: "top-center", label: "Top center" },
  { id: "top-right", label: "Top right" },
  { id: "bottom-left", label: "Bottom left" },
  { id: "bottom-center", label: "Bottom center" },
  { id: "bottom-right", label: "Bottom right" },
];

// Agent transitions that can trigger an ntfy push.
export type NtfyEvent = "done" | "waiting" | "permission" | "error";
export const NTFY_EVENTS: { id: NtfyEvent; label: string }[] = [
  { id: "done", label: "Task complete" },
  { id: "waiting", label: "Waiting for input" },
  { id: "permission", label: "Permission needed" },
  { id: "error", label: "Turn failed (error)" },
];

// The px sizes in the stylesheets are authored at this baseline. `zoom` scales
// the whole UI relative to it, so the default uiFontSize being above the
// baseline makes the default UI render slightly larger.
const BASE_FONT_SIZE = 13;

// Haiku is the cheap default t3code picks too (their
// DEFAULT_GIT_TEXT_GENERATION_MODEL_BY_PROVIDER), and it publishes no reasoning
// efforts, so the selection stays three-part.
export const DEFAULT_TEXT_GENERATION_MODEL = "claude::claude::claude-haiku-4-5-20251001";

const DEFAULT_PREFS: Prefs = {
  uiFont: UI_FONTS[0].value,
  uiFontSize: 16,
  uiScale: 1,
  terminalFont: TERMINAL_FONTS[0].value,
  terminalFontSize: 13,
  swapPanels: false,
  rightPanelVisible: true,
  theme: DEFAULT_THEME_KEY,
  themeMode: "dark",
  preferredDarkTheme: DEFAULT_FAMILY_KEY,
  preferredLightTheme: DEFAULT_FAMILY_KEY,
  soundEnabled: true,
  soundDoneEnabled: true,
  soundWaitingEnabled: true,
  soundDoneId: "soft-1",
  soundDoneCustomPath: "",
  soundWaitingId: "need-you-1",
  soundWaitingCustomPath: "",
  soundVolume: 70,
  maxAgents: 3,
  mcpMaxDepth: 3,
  debugOverlay: false,
  floatCorner: "top-right",
  worktreesDir: "~/burrow-worktrees",
  mode: "terminal",
  bgImagePath: "",
  bgOpacity: 0.82,
  blurPanels: 20,
  blurContent: 20,
  blurTerminal: 0,
  blurOverlay: 20,
  blurDropdown: 18,
  ntfyEnabled: false,
  ntfyServer: "https://ntfy.sh",
  ntfyTopic: "",
  ntfyToken: "",
  ntfyEvents: ["done", "permission", "error"],
  ntfyOnlyWhenAway: true,
  petsEnabled: false,
  petsSpeech: true,
  petsLeveling: true,
  floatChatEnabled: true,
  floatChatOpen: false,
  sidebarVisible: false,
  sidebarWidth: 220,
  rightPanelWidth: 300,
  toastPosition: "bottom-left",
  defaultChatAgent: "claude",
  spawnMode: "terminal",
  textGenerationModel: DEFAULT_TEXT_GENERATION_MODEL,
  textGenerationPolicy: "default",
};

function normalize(parsed: unknown): Prefs {
  if (parsed && typeof parsed === "object") {
    const stored = { ...DEFAULT_PREFS, ...(parsed as Partial<Prefs>) };
    // The pref was `commitMessageModel` while commit messages were the only
    // thing generated; it now covers every background writing job.
    const legacyModel = (parsed as { commitMessageModel?: string }).commitMessageModel;
    if (legacyModel && !(parsed as Partial<Prefs>).textGenerationModel) {
      stored.textGenerationModel = legacyModel;
    }
    // Until provider-aware text generation landed this held a bare Claude
    // model id. Keep existing preferences working and make them selectable.
    if (!stored.textGenerationModel.includes("::")) {
      stored.textGenerationModel = `claude::claude::${stored.textGenerationModel}`;
    } else if (stored.textGenerationModel.split("::").length === 2) {
      // Short-lived first provider-aware format: kind::model.
      const [kind, model] = stored.textGenerationModel.split("::");
      stored.textGenerationModel = `${kind}::${kind}::${model}`;
    }
    // Migrate installs saved below the current default up to it.
    if (stored.uiFontSize < DEFAULT_PREFS.uiFontSize) stored.uiFontSize = DEFAULT_PREFS.uiFontSize;
    // Installs saved before themeMode existed: infer it from their theme
    // pick rather than defaulting to "system", so nobody's theme changes underneath them.
    if (!(parsed as Partial<Prefs>).themeMode) {
      stored.themeMode = findTheme(stored.theme).isDark ? "dark" : "light";
    }
    // The preferred-theme slots used to hold single variant keys ("tide-light");
    // they now hold family keys ("tide"). Map anything that isn't a known family
    // through familyOf, so an upgrade keeps the user's design on both sides.
    const toFamily = (k: string, fallback: string) =>
      THEME_FAMILIES.some((f) => f.key === k)
        ? k
        : (THEMES.some((t) => t.key === k) ? familyOf(k).key : fallback);
    stored.preferredDarkTheme = toFamily(stored.preferredDarkTheme, DEFAULT_FAMILY_KEY);
    stored.preferredLightTheme = toFamily(stored.preferredLightTheme, DEFAULT_FAMILY_KEY);
    return stored;
  }
  return { ...DEFAULT_PREFS };
}

export const useUIStore = defineStore("ui", () => {
  const settingsOpen = ref(false);

  const loaded = { ...DEFAULT_PREFS };
  const uiFont = ref(loaded.uiFont);
  const uiFontSize = ref(loaded.uiFontSize);
  const uiScale = ref(loaded.uiScale);
  const terminalFont = ref(loaded.terminalFont);
  const terminalFontSize = ref(loaded.terminalFontSize);
  const swapPanels = ref(loaded.swapPanels);
  const rightPanelVisible = ref(loaded.rightPanelVisible);
  const theme = ref(loaded.theme);
  const themeMode = ref<"system" | "light" | "dark">(loaded.themeMode);
  const preferredDarkTheme = ref(loaded.preferredDarkTheme);
  const preferredLightTheme = ref(loaded.preferredLightTheme);
  const soundEnabled = ref(loaded.soundEnabled);
  const soundDoneEnabled = ref(loaded.soundDoneEnabled);
  const soundWaitingEnabled = ref(loaded.soundWaitingEnabled);
  const soundDoneId = ref(loaded.soundDoneId);
  const soundDoneCustomPath = ref(loaded.soundDoneCustomPath);
  const soundWaitingId = ref(loaded.soundWaitingId);
  const soundWaitingCustomPath = ref(loaded.soundWaitingCustomPath);
  const soundVolume = ref(loaded.soundVolume);
  const maxAgents = ref(loaded.maxAgents);
  const mcpMaxDepth = ref(loaded.mcpMaxDepth);
  const debugOverlay = ref(loaded.debugOverlay);
  const floatCorner = ref(loaded.floatCorner);
  const worktreesDir = ref(loaded.worktreesDir);
  // Which main surface is showing. Derived from the route, never assigned —
  // the URL is the view state (fáze 4, docs/plans/003-view-state-routes.md), so
  // this cannot drift from what is on screen the way a second ref could.
  // Still persisted, only so a restart reopens on the surface you left.
  const mode = computed<"terminal" | "claude" | "dashboard">(() =>
    router.currentRoute.value.name === "dashboard" ? "dashboard" : "terminal",
  );
  // A pref saved before git became a tab can still say "git" — fall back to terminal.
  const startupMode: "terminal" | "dashboard" =
    (loaded.mode as string) === "dashboard" ? "dashboard" : "terminal";
  const bgImagePath = ref(loaded.bgImagePath);
  const bgOpacity = ref(loaded.bgOpacity);
  const blurPanels = ref(loaded.blurPanels);
  const blurContent = ref(loaded.blurContent);
  const blurTerminal = ref(loaded.blurTerminal);
  const blurOverlay = ref(loaded.blurOverlay);
  const blurDropdown = ref(loaded.blurDropdown ?? 18);
  const ntfyEnabled = ref(loaded.ntfyEnabled);
  const ntfyServer = ref(loaded.ntfyServer);
  const ntfyTopic = ref(loaded.ntfyTopic);
  const ntfyToken = ref(loaded.ntfyToken);
  const ntfyEvents = ref<NtfyEvent[]>(loaded.ntfyEvents);
  const ntfyOnlyWhenAway = ref(loaded.ntfyOnlyWhenAway);
  const petsEnabled = ref(loaded.petsEnabled);
  const petsSpeech = ref(loaded.petsSpeech);
  const petsLeveling = ref(loaded.petsLeveling);
  const floatChatEnabled = ref(loaded.floatChatEnabled);
  const floatChatOpen = ref(loaded.floatChatOpen);

  // The compose screen is its own route now (`/`), so "is the composer up" is
  // not a ref anyone can forget to clear — see welcomeVisible below.
  const sidebarVisible = ref(loaded.sidebarVisible ?? false);
  const sidebarWidth = ref(loaded.sidebarWidth ?? 220);
  const rightPanelWidth = ref(loaded.rightPanelWidth ?? 300);
  const toastPosition = ref<ToastPosition>(loaded.toastPosition ?? "bottom-left");
  const defaultChatAgent = ref<string>(loaded.defaultChatAgent ?? 'claude');
  const spawnMode = ref<"terminal" | "chat">(loaded.spawnMode ?? "terminal");
  const textGenerationModel = ref<string>(loaded.textGenerationModel ?? DEFAULT_TEXT_GENERATION_MODEL);
  const textGenerationPolicy = ref<TextGenerationPolicy>(loaded.textGenerationPolicy ?? "default");
  // In-memory blob URL for the current wallpaper (not persisted).
  const bgImageUrl = ref<string>("");

  configReady.then(() => {
    migrateFromLocalStorage(PREFS_KEY, CONFIG_KEY);
    const p = normalize(getConfig<unknown>(CONFIG_KEY, DEFAULT_PREFS));
    uiFont.value = p.uiFont;
    uiFontSize.value = p.uiFontSize;
    uiScale.value = p.uiScale;
    terminalFont.value = p.terminalFont;
    terminalFontSize.value = p.terminalFontSize;
    swapPanels.value = p.swapPanels;
    rightPanelVisible.value = p.rightPanelVisible;
    theme.value = p.theme;
    themeMode.value = p.themeMode;
    preferredDarkTheme.value = p.preferredDarkTheme;
    preferredLightTheme.value = p.preferredLightTheme;
    // The live variant always follows (family × scheme), and "system" depends on
    // the OS setting at load time — so resolve it fresh rather than trusting the
    // persisted `theme`.
    resolveThemeMode();
    soundEnabled.value = p.soundEnabled;
    soundDoneEnabled.value = p.soundDoneEnabled;
    soundWaitingEnabled.value = p.soundWaitingEnabled;
    soundDoneId.value = p.soundDoneId;
    soundDoneCustomPath.value = p.soundDoneCustomPath;
    soundWaitingId.value = p.soundWaitingId;
    soundWaitingCustomPath.value = p.soundWaitingCustomPath;
    soundVolume.value = p.soundVolume;
    maxAgents.value = p.maxAgents;
    mcpMaxDepth.value = p.mcpMaxDepth;
    debugOverlay.value = p.debugOverlay;
    floatCorner.value = p.floatCorner;
    worktreesDir.value = p.worktreesDir;
    bgImagePath.value = p.bgImagePath;
    bgOpacity.value = p.bgOpacity;
    blurPanels.value = p.blurPanels;
    blurContent.value = p.blurContent;
    blurTerminal.value = p.blurTerminal;
    blurOverlay.value = p.blurOverlay;
    blurDropdown.value = p.blurDropdown ?? 18;
    ntfyEnabled.value = p.ntfyEnabled;
    ntfyServer.value = p.ntfyServer;
    ntfyTopic.value = p.ntfyTopic;
    ntfyToken.value = p.ntfyToken;
    ntfyEvents.value = p.ntfyEvents;
    ntfyOnlyWhenAway.value = p.ntfyOnlyWhenAway;
    petsEnabled.value = p.petsEnabled;
    petsSpeech.value = p.petsSpeech;
    petsLeveling.value = p.petsLeveling;
    floatChatEnabled.value = p.floatChatEnabled;
    floatChatOpen.value = p.floatChatOpen;
    sidebarVisible.value = p.sidebarVisible ?? false;
    sidebarWidth.value = p.sidebarWidth ?? 220;
    rightPanelWidth.value = p.rightPanelWidth ?? 300;
    toastPosition.value = p.toastPosition ?? "bottom-left";
    defaultChatAgent.value = p.defaultChatAgent ?? "claude";
    spawnMode.value = p.spawnMode ?? "terminal";
    textGenerationModel.value = p.textGenerationModel ?? DEFAULT_TEXT_GENERATION_MODEL;
    textGenerationPolicy.value = p.textGenerationPolicy ?? "default";
  });

  // Publish the soft sub-agent cap to a file the `burrow` CLI can read (it can't
  // see localStorage). No-op in browser-only dev where Tauri invoke is absent.
  watch(
    maxAgents,
    (n) => { invoke("set_max_agents", { n }).catch(() => {}); },
    { immediate: true },
  );

  // Publish the MCP recursion depth cap to the file mcp_max_depth() reads.
  watch(
    mcpMaxDepth,
    (n) => { invoke("set_burrow_mcp_max_depth", { n }).catch(() => {}); },
    { immediate: true },
  );

  // The full Theme object for the active key — consumed by xterm (XTerm.vue)
  // and the diff viewer (DiffTab.vue), which can't read CSS vars.
  const activeTheme = computed(() => findTheme(theme.value));

  // Whole-UI zoom factor applied to #app. The terminal must counter-zoom by 1/this
  // and scale its own font by this instead — CSS `zoom` on an ancestor breaks
  // xterm.js mouse-selection coordinate math (selection lands on the wrong rows).
  const effectiveScale = computed(() => uiScale.value * (uiFontSize.value / BASE_FONT_SIZE));

  // Apply the active theme's colors as CSS custom properties on :root, so all
  // chrome styled via var(--bg-base) etc. repaints. Font + layout vars are left
  // alone (they're not part of a theme).
  function applyTheme() {
    const t = findTheme(theme.value);
    const root = document.documentElement;
    for (const [k, v] of Object.entries(t.vars)) {
      root.style.setProperty(`--${k}`, v);
    }
    // Match the terminal frame/pane exactly to the xterm canvas background, so
    // there's no tonal "border" around the terminal content.
    if (t.xterm.background) root.style.setProperty("--terminal-bg", t.xterm.background);
    // Optional full-window meme wallpaper (joke themes); `none` clears it.
    root.style.setProperty("--bg-image", t.bgImage ?? "none");
    // Frosted-glass backdrop for translucent themes; else none. (No bundled theme
    // sets this — transparent/vibrancy themes were removed for causing lag.)
    root.style.setProperty("--backdrop-blur", t.backdropBlur ?? "none");
    // Per-element blur vars — each surface reads its own, independent of theme.
    const mkBlur = (n: number) => (n > 0 ? `blur(${n}px)` : "none");
    root.style.setProperty("--blur-panels", mkBlur(blurPanels.value));
    root.style.setProperty("--blur-content", mkBlur(blurContent.value));
    root.style.setProperty("--blur-terminal", mkBlur(blurTerminal.value));
    root.style.setProperty("--blur-overlay", mkBlur(blurOverlay.value));
    root.style.setProperty("--blur-dropdown", mkBlur(blurDropdown.value));
    root.style.colorScheme = t.isDark ? "dark" : "light";
    // When user has a wallpaper, make panels semi-transparent and enable blur.
    if (bgImageUrl.value) {
      const op = bgOpacity.value;
      root.style.setProperty("--bg-base", hexToRgba(t.vars["bg-base"], op));
      root.style.setProperty("--bg-panel", hexToRgba(t.vars["bg-panel"], op));
      root.style.setProperty("--bg-hover", hexToRgba(t.vars["bg-hover"], Math.min(1, op + 0.08)));
      root.style.setProperty("--terminal-bg", hexToRgba(t.xterm.background ?? t.vars["bg-base"], op));
      // Dropdowns float over content and need to stay legible — clamp opacity higher.
      root.style.setProperty("--bg-dropdown", hexToRgba(t.vars["bg-panel"], Math.min(1, Math.max(0.88, op))));
      if (!t.backdropBlur) root.style.setProperty("--backdrop-blur", "blur(20px)");
    } else {
      root.style.setProperty("--bg-dropdown", "var(--bg-panel)");
    }
  }

  // Load a wallpaper from disk (base64 → blob URL) and apply it.
  async function loadAndApplyBg(path: string) {
    if (!path) {
      bgImageUrl.value = "";
      document.body.style.backgroundImage = "none";
      document.body.style.backgroundSize = "";
      document.body.style.backgroundPosition = "";
      applyTheme();
      return;
    }
    try {
      const b64 = await invoke<string>("read_file_base64", { path });
      const bin = atob(b64);
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
      const ext = path.split(".").pop()?.toLowerCase() ?? "jpg";
      const mime = ext === "png" ? "image/png" : ext === "gif" ? "image/gif" : ext === "webp" ? "image/webp" : "image/jpeg";
      const url = URL.createObjectURL(new Blob([bytes], { type: mime }));
      bgImageUrl.value = url;
      document.body.style.backgroundImage = `url("${url}")`;
      document.body.style.backgroundSize = "cover";
      document.body.style.backgroundPosition = "center";
      applyTheme();
    } catch {
      bgImageUrl.value = "";
      document.body.style.backgroundImage = "none";
      applyTheme();
    }
  }

  // Reload the image when the path changes; just re-apply CSS when opacity changes.
  watch(bgImagePath, (path) => { loadAndApplyBg(path); saveBgPrefs(); });
  watch(bgOpacity, () => { applyTheme(); saveBgPrefs(); });
  watch([blurPanels, blurContent, blurTerminal, blurOverlay, blurDropdown], () => { applyTheme(); savePrefs(); });

  // Load wallpaper on store init (path already in prefs).
  if (bgImagePath.value) loadAndApplyBg(bgImagePath.value);

  function savePrefs() {
    setConfig(
      CONFIG_KEY,
      {
        uiFont: uiFont.value,
        uiFontSize: uiFontSize.value,
        uiScale: uiScale.value,
        terminalFont: terminalFont.value,
        terminalFontSize: terminalFontSize.value,
        swapPanels: swapPanels.value,
        rightPanelVisible: rightPanelVisible.value,
        theme: theme.value,
        themeMode: themeMode.value,
        preferredDarkTheme: preferredDarkTheme.value,
        preferredLightTheme: preferredLightTheme.value,
        soundEnabled: soundEnabled.value,
        soundDoneEnabled: soundDoneEnabled.value,
        soundWaitingEnabled: soundWaitingEnabled.value,
        soundDoneId: soundDoneId.value,
        soundDoneCustomPath: soundDoneCustomPath.value,
        soundWaitingId: soundWaitingId.value,
        soundWaitingCustomPath: soundWaitingCustomPath.value,
        soundVolume: soundVolume.value,
        maxAgents: maxAgents.value,
        mcpMaxDepth: mcpMaxDepth.value,
        debugOverlay: debugOverlay.value,
        floatCorner: floatCorner.value,
        worktreesDir: worktreesDir.value,
        mode: mode.value,
        bgImagePath: bgImagePath.value,
        bgOpacity: bgOpacity.value,
        blurPanels: blurPanels.value,
        blurContent: blurContent.value,
        blurTerminal: blurTerminal.value,
        blurOverlay: blurOverlay.value,
        blurDropdown: blurDropdown.value,
        ntfyEnabled: ntfyEnabled.value,
        ntfyServer: ntfyServer.value,
        ntfyTopic: ntfyTopic.value,
        ntfyToken: ntfyToken.value,
        ntfyEvents: ntfyEvents.value,
        ntfyOnlyWhenAway: ntfyOnlyWhenAway.value,
        petsEnabled: petsEnabled.value,
        petsSpeech: petsSpeech.value,
        petsLeveling: petsLeveling.value,
        floatChatEnabled: floatChatEnabled.value,
        floatChatOpen: floatChatOpen.value,
        sidebarVisible: sidebarVisible.value,
        sidebarWidth: sidebarWidth.value,
        rightPanelWidth: rightPanelWidth.value,
        toastPosition: toastPosition.value,
        defaultChatAgent: defaultChatAgent.value,
        spawnMode: spawnMode.value,
        textGenerationModel: textGenerationModel.value,
        textGenerationPolicy: textGenerationPolicy.value,
      } satisfies Prefs,
    );
  }

  function saveBgPrefs() {
    savePrefs();
  }

  // Persist + apply UI font, base font size and overall scale (zoom).
  watch(
    [uiFont, uiFontSize, uiScale, terminalFont, terminalFontSize, swapPanels, theme, themeMode,
     preferredDarkTheme, preferredLightTheme,
     soundEnabled, soundDoneEnabled, soundWaitingEnabled, soundDoneId, soundDoneCustomPath,
     soundWaitingId, soundWaitingCustomPath, soundVolume, rightPanelVisible, maxAgents, mcpMaxDepth, debugOverlay, floatCorner, worktreesDir, mode,
     ntfyEnabled, ntfyServer, ntfyTopic, ntfyToken, ntfyEvents, ntfyOnlyWhenAway,
     petsEnabled, petsSpeech, petsLeveling, floatChatEnabled, floatChatOpen,
     sidebarVisible, sidebarWidth, rightPanelWidth, toastPosition, defaultChatAgent, spawnMode, textGenerationModel, textGenerationPolicy],
    () => {
      savePrefs();
      applyTheme();
      document.documentElement.style.setProperty("--font-ui", uiFont.value);
      // The UI uses fixed px sizes, so the effective scale combines the explicit
      // scale with the font-size ratio (relative to the baseline). Use CSS `zoom`
      // (not `transform: scale`) so text re-rasterizes crisply at the real DPI —
      // `transform` scales a 1x bitmap and looks blurry on macOS WKWebView.
      applyAppScale();
    },
    { immediate: true },
  );

  // Counter-size #app so `zoom` lands exactly on the window. `zoom` magnifies layout,
  // so a plain 100vw box would overflow by `scale` on the right; we shrink it by
  // 1/scale first. Size in real CSS-px read from window.innerWidth/Height — NOT vw/vh
  // or %: a descendant's `zoom` leaves window.innerWidth untouched, so
  // `(innerWidth/scale)px * zoom(scale) === innerWidth` holds on every WebKit build.
  // Viewport/percentage units get re-evaluated against the *zoomed* viewport
  // inconsistently across macOS WKWebView versions, which left empty bands on the
  // right + bottom for some users. px must be recomputed on resize (vw/% wouldn't).
  function applyAppScale() {
    const scale = effectiveScale.value;
    const app = document.getElementById("app");
    if (!app) return;
    app.style.setProperty("zoom", scale === 1 ? "" : String(scale));
    app.style.width = scale === 1 ? "" : `${window.innerWidth / scale}px`;
    app.style.height = scale === 1 ? "" : `${window.innerHeight / scale}px`;
  }
  window.addEventListener("resize", applyAppScale);

  // Which Settings nav section to land on. Set by callers that deep-link
  // (palette → "Keybindings"/"Appearance"); Settings.vue reads it on mount.
  const settingsSection = ref("general");
  // Optional row to preselect inside that section (Providers: an instance id).
  const settingsFocusId = ref("");
  function openSettings(section?: string, focusId?: string) {
    if (section) settingsSection.value = section;
    settingsFocusId.value = focusId ?? "";
    settingsOpen.value = true;
  }
  function closeSettings() {
    settingsOpen.value = false;
  }
  function toggleSettings() {
    settingsOpen.value = !settingsOpen.value;
  }
  function toggleRightPanel() {
    rightPanelVisible.value = !rightPanelVisible.value;
  }

  // The scheme currently in force: "system" asks the OS, the other modes are
  // themselves.
  // Reactive mirror of the OS scheme — a bare matchMedia() read inside a computed
  // has no reactive dep, so the cache never invalidates when the OS flips.
  const systemDark = ref(window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? true);
  const resolvedScheme = computed<"light" | "dark">(() => {
    if (themeMode.value !== "system") return themeMode.value;
    return systemDark.value ? "dark" : "light";
  });

  // The families assigned to each scheme, and the one that is live now.
  const lightFamily = computed(() => findFamily(preferredLightTheme.value));
  const darkFamily = computed(() => findFamily(preferredDarkTheme.value));
  const activeFamily = computed(() =>
    resolvedScheme.value === "dark" ? darkFamily.value : lightFamily.value,
  );

  // Assign a family to one scheme (Settings' sun/moon buttons). Assigning to the
  // scheme that is live repaints immediately; the other side waits its turn.
  function setThemeFamilyFor(scheme: "light" | "dark", familyKey: string) {
    const key = findFamily(familyKey).key;
    if (scheme === "dark") preferredDarkTheme.value = key;
    else preferredLightTheme.value = key;
    resolveThemeMode();
  }

  // Use one family for both schemes (t3code's "use for both light and dark").
  function setThemeFamily(familyKey: string) {
    const key = findFamily(familyKey).key;
    preferredLightTheme.value = key;
    preferredDarkTheme.value = key;
    resolveThemeMode();
  }

  // Picking a concrete VARIANT (Onboarding, Spotlight): adopt its family for the
  // scheme that variant belongs to, and settle themeMode there so it shows now.
  function setTheme(key: string) {
    const scheme = findTheme(key).isDark ? "dark" : "light";
    setThemeFamilyFor(scheme, familyOf(key).key);
    themeMode.value = scheme;
    resolveThemeMode();
  }

  // Apply the live family's variant for the resolved scheme.
  function resolveThemeMode() {
    theme.value = variantFor(activeFamily.value, resolvedScheme.value).key;
  }

  function setThemeMode(mode: "system" | "light" | "dark") {
    themeMode.value = mode;
    resolveThemeMode();
  }

  // Follow OS theme changes live while in "system" mode.
  if (typeof window !== "undefined" && window.matchMedia) {
    window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", (e) => {
      systemDark.value = e.matches;
      if (themeMode.value === "system") resolveThemeMode();
    });
  }

  // Spotlight "Toggle Dark/Light Mode": flip the scheme; the theme follows from
  // whichever family that scheme is assigned. Doesn't rewrite the assignments.
  function toggleDarkLight() {
    setThemeMode(resolvedScheme.value === "dark" ? "light" : "dark");
  }

  // Route for a workspace's tabs, or the composer when there is no workspace
  // to show. One place decides that, so "go back to the tabs" is unambiguous.
  function tabsRoute(): string {
    return workspaceRoute(useWorkspaceStore().active?.id);
  }

  function setMode(m: "terminal" | "claude" | "dashboard") {
    void router.push(m === "dashboard" ? "/dashboard" : tabsRoute());
  }

  function toggleDashboard() {
    void router.push(mode.value === "dashboard" ? tabsRoute() : "/dashboard");
  }

  function toggleSidebar() {
    sidebarVisible.value = !sidebarVisible.value;
  }

  function resetFonts() {
    uiFont.value = DEFAULT_PREFS.uiFont;
    uiFontSize.value = DEFAULT_PREFS.uiFontSize;
    uiScale.value = DEFAULT_PREFS.uiScale;
    terminalFont.value = DEFAULT_PREFS.terminalFont;
    terminalFontSize.value = DEFAULT_PREFS.terminalFontSize;
  }

  function clearBgImage() {
    bgImagePath.value = "";
  }

  function toggleFloatChat() {
    floatChatOpen.value = !floatChatOpen.value;
  }

  // ── the active view ────────────────────────────────────────────────────────
  // Which surface the terminal host is actually showing, and the single source
  // of truth for "is the user looking at this tab". Everything that decides
  // watched/seen derives from `viewingTabs`, so a tab sitting behind the
  // welcome composer can never count as watched — that was the bug where a
  // turn finishing while you composed a new thread reported a transient `done`
  // instead of a persistent `review`, and window focus cleared its badge
  // unseen. t3code gets this for free (the composer is its own route, so
  // ChatView is simply unmounted); our terminals must stay mounted to keep the
  // PTY stream and the chat event listeners alive, so the state is explicit
  // instead of implied by the DOM.
  const welcomeVisible = computed(() => router.currentRoute.value.name === "welcome");
  const viewingTabs = computed(() => {
    const name = router.currentRoute.value.name;
    return name === "workspace" || name === "tab";
  });

  /** Number of the active workspace's tabs that still count as live. */
  function liveTabCount(): number {
    const wsStore = useWorkspaceStore();
    if (!wsStore.active) return 0;
    const all = useTerminalTabsStore().tabsByWs[wsStore.active.id] ?? [];
    return all.filter((t) => !t.settled).length;
  }

  /**
   * Go to a workspace's tabs — unless it has none live, in which case the
   * composer is the honest destination. This is the old tri-state `welcomeOpen`
   * auto branch, kept as a navigation decision instead of a stored one.
   */
  function showTabs() {
    void router.push(tabsOrWelcome(useWorkspaceStore().active?.id, liveTabCount()));
  }

  function openWelcome() {
    void router.push("/");
  }
  // Dismissing the composer is an explicit "show me the tabs", so it goes there
  // even when the workspace has nothing live yet — a tab being opened in the
  // same breath (a script, a new thread) has not reached the store yet, and
  // bouncing back to the composer would swallow it.
  function closeWelcome() {
    void router.push(tabsRoute());
  }

  // Spotlight lives in App.vue's tree, so callers elsewhere (Sidebar's "New
  // chat") reach it through this registered handle instead of prop-drilling a ref.
  const spotlightApi = ref<{ show: (opts?: { projectOnly?: boolean }) => void } | null>(null);
  function registerSpotlightApi(api: { show: (opts?: { projectOnly?: boolean }) => void }) {
    spotlightApi.value = api;
  }
  function pickProjectThenWelcome() {
    spotlightApi.value?.show({ projectOnly: true });
  }

  return {
    settingsOpen,
    settingsSection,
    settingsFocusId,
    uiFont,
    uiFontSize,
    uiScale,
    effectiveScale,
    terminalFont,
    terminalFontSize,
    swapPanels,
    rightPanelVisible,
    toggleRightPanel,
    theme,
    themeMode,
    setThemeMode,
    activeTheme,
    themes: THEMES,
    families: THEME_FAMILIES,
    resolvedScheme,
    lightFamily,
    darkFamily,
    activeFamily,
    setTheme,
    setThemeFamily,
    setThemeFamilyFor,
    preferredDarkTheme,
    preferredLightTheme,
    toggleDarkLight,
    soundEnabled,
    soundDoneEnabled,
    soundWaitingEnabled,
    soundDoneId,
    soundDoneCustomPath,
    soundWaitingId,
    soundWaitingCustomPath,
    soundVolume,
    maxAgents,
    mcpMaxDepth,
    debugOverlay,
    floatCorner,
    worktreesDir,
    mode,
    setMode,
    toggleDashboard,
    openSettings,
    closeSettings,
    toggleSettings,
    resetFonts,
    bgImagePath,
    bgOpacity,
    bgImageUrl,
    clearBgImage,
    blurPanels,
    blurContent,
    blurTerminal,
    blurOverlay,
    blurDropdown,
    ntfyEnabled,
    ntfyServer,
    ntfyTopic,
    ntfyToken,
    ntfyEvents,
    ntfyOnlyWhenAway,
    petsEnabled,
    petsSpeech,
    petsLeveling,
    floatChatEnabled,
    floatChatOpen,
    toggleFloatChat,
    showTabs,
    startupMode,
    welcomeVisible,
    viewingTabs,
    openWelcome,
    closeWelcome,
    registerSpotlightApi,
    pickProjectThenWelcome,
    sidebarVisible,
    toggleSidebar,
    sidebarWidth,
    rightPanelWidth,
    toastPosition,
    defaultChatAgent,
    spawnMode,
    textGenerationModel,
    textGenerationPolicy,
  };
});

import { ref, computed, watch } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import { THEMES, DEFAULT_THEME_KEY, findTheme } from "@/themes";

function hexToRgba(hex: string, alpha: number): string {
  const h = hex.replace("#", "");
  if (h.length !== 6) return hex;
  const r = parseInt(h.substring(0, 2), 16);
  const g = parseInt(h.substring(2, 4), 16);
  const b = parseInt(h.substring(4, 6), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}

const PREFS_KEY = "agentic-ide.prefs";

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
  soundEnabled: boolean;
  soundDoneEnabled: boolean;
  soundWaitingEnabled: boolean;
  soundDoneId: string; // builtin id or "custom"
  soundDoneCustomPath: string;
  soundWaitingId: string;
  soundWaitingCustomPath: string;
  soundVolume: number; // 0-100
  maxAgents: number; // soft per-workspace sub-agent cap for the /burrow skill
  debugOverlay: boolean; // show the per-terminal diagnostic overlay (XTerm.vue)
  floatCorner: string; // which screen corner floating windows snap+stack to
  worktreesDir: string; // parent dir for git worktrees: <dir>/<repo>/<branch>
  mode: "terminal" | "claude" | "git" | "mission" | "dashboard"; // active main-pane mode, switched via activity bar
  missionShowActivity: boolean; // MC detail feed: show thinking + tool_use/result entries (off = text messages only)
  bgImagePath: string; // absolute path to user wallpaper (empty = none)
  bgOpacity: number; // 0–1 opacity of panels/terminal over the wallpaper
  // Per-element backdrop-blur radius in px (0 = off). Separate so each surface
  // tunes its own frosted-glass strength over the wallpaper.
  blurPanels: number; // sidebar, activity bar, right panel, title bar
  blurContent: number; // Mission Control rail + Dashboard cards
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
  sidebarWidth: number; // left sidebar panel width in px
  rightPanelWidth: number; // right panel width in px
  toastPosition: ToastPosition; // screen anchor for toast notifications
  defaultChatAgent: string; // default agent for new chat sessions (chatAgents store id)
  spawnMode: "terminal" | "chat"; // how `burrow spawn` sub-agents open: a terminal tab or an ACP chat
}

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

const DEFAULT_PREFS: Prefs = {
  uiFont: UI_FONTS[0].value,
  uiFontSize: 16,
  uiScale: 1,
  terminalFont: TERMINAL_FONTS[0].value,
  terminalFontSize: 13,
  swapPanels: false,
  rightPanelVisible: true,
  theme: DEFAULT_THEME_KEY,
  soundEnabled: true,
  soundDoneEnabled: true,
  soundWaitingEnabled: true,
  soundDoneId: "soft-1",
  soundDoneCustomPath: "",
  soundWaitingId: "need-you-1",
  soundWaitingCustomPath: "",
  soundVolume: 70,
  maxAgents: 3,
  debugOverlay: false,
  floatCorner: "top-right",
  worktreesDir: "~/burrow-worktrees",
  mode: "terminal",
  missionShowActivity: true,
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
  sidebarWidth: 220,
  rightPanelWidth: 300,
  toastPosition: "bottom-left",
  defaultChatAgent: "claude",
  spawnMode: "terminal",
};

function loadPrefs(): Prefs {
  try {
    const raw = localStorage.getItem(PREFS_KEY);
    if (raw) {
      const stored = { ...DEFAULT_PREFS, ...JSON.parse(raw) };
      // Migrate installs saved below the current default up to it.
      if (stored.uiFontSize < DEFAULT_PREFS.uiFontSize) stored.uiFontSize = DEFAULT_PREFS.uiFontSize;
      return stored;
    }
  } catch {
    /* ignore */
  }
  return { ...DEFAULT_PREFS };
}

export const useUIStore = defineStore("ui", () => {
  const settingsOpen = ref(false);

  const loaded = loadPrefs();
  const uiFont = ref(loaded.uiFont);
  const uiFontSize = ref(loaded.uiFontSize);
  const uiScale = ref(loaded.uiScale);
  const terminalFont = ref(loaded.terminalFont);
  const terminalFontSize = ref(loaded.terminalFontSize);
  const swapPanels = ref(loaded.swapPanels);
  const rightPanelVisible = ref(loaded.rightPanelVisible);
  const theme = ref(loaded.theme);
  const soundEnabled = ref(loaded.soundEnabled);
  const soundDoneEnabled = ref(loaded.soundDoneEnabled);
  const soundWaitingEnabled = ref(loaded.soundWaitingEnabled);
  const soundDoneId = ref(loaded.soundDoneId);
  const soundDoneCustomPath = ref(loaded.soundDoneCustomPath);
  const soundWaitingId = ref(loaded.soundWaitingId);
  const soundWaitingCustomPath = ref(loaded.soundWaitingCustomPath);
  const soundVolume = ref(loaded.soundVolume);
  const maxAgents = ref(loaded.maxAgents);
  const debugOverlay = ref(loaded.debugOverlay);
  const floatCorner = ref(loaded.floatCorner);
  const worktreesDir = ref(loaded.worktreesDir);
  const mode = ref<"terminal" | "claude" | "git" | "mission" | "dashboard">(loaded.mode);
  const missionShowActivity = ref(loaded.missionShowActivity);
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
  const sidebarWidth = ref(loaded.sidebarWidth ?? 220);
  const rightPanelWidth = ref(loaded.rightPanelWidth ?? 300);
  const toastPosition = ref<ToastPosition>(loaded.toastPosition ?? "bottom-left");
  const defaultChatAgent = ref<string>(loaded.defaultChatAgent ?? 'claude');
  const spawnMode = ref<"terminal" | "chat">(loaded.spawnMode ?? "terminal");
  // In-memory blob URL for the current wallpaper (not persisted).
  const bgImageUrl = ref<string>("");
  const missionActiveCount = ref(0);

  // Push the float-window corner to Rust whenever it changes (and on load), so
  // every floating window snaps + stacks at the chosen corner.
  watch(
    floatCorner,
    (c) => { invoke("set_float_corner", { corner: c }).catch(() => {}); },
    { immediate: true },
  );

  // Publish the soft sub-agent cap to a file the `burrow` CLI can read (it can't
  // see localStorage). No-op in browser-only dev where Tauri invoke is absent.
  watch(
    maxAgents,
    (n) => { invoke("set_max_agents", { n }).catch(() => {}); },
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
    localStorage.setItem(
      PREFS_KEY,
      JSON.stringify({
        uiFont: uiFont.value,
        uiFontSize: uiFontSize.value,
        uiScale: uiScale.value,
        terminalFont: terminalFont.value,
        terminalFontSize: terminalFontSize.value,
        swapPanels: swapPanels.value,
        rightPanelVisible: rightPanelVisible.value,
        theme: theme.value,
        soundEnabled: soundEnabled.value,
        soundDoneEnabled: soundDoneEnabled.value,
        soundWaitingEnabled: soundWaitingEnabled.value,
        soundDoneId: soundDoneId.value,
        soundDoneCustomPath: soundDoneCustomPath.value,
        soundWaitingId: soundWaitingId.value,
        soundWaitingCustomPath: soundWaitingCustomPath.value,
        soundVolume: soundVolume.value,
        maxAgents: maxAgents.value,
        debugOverlay: debugOverlay.value,
        floatCorner: floatCorner.value,
        worktreesDir: worktreesDir.value,
        mode: mode.value,
        missionShowActivity: missionShowActivity.value,
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
        sidebarWidth: sidebarWidth.value,
        rightPanelWidth: rightPanelWidth.value,
        toastPosition: toastPosition.value,
        defaultChatAgent: defaultChatAgent.value,
        spawnMode: spawnMode.value,
      } satisfies Prefs),
    );
  }

  function saveBgPrefs() {
    savePrefs();
  }

  // Persist + apply UI font, base font size and overall scale (zoom).
  watch(
    [uiFont, uiFontSize, uiScale, terminalFont, terminalFontSize, swapPanels, theme,
     soundEnabled, soundDoneEnabled, soundWaitingEnabled, soundDoneId, soundDoneCustomPath,
     soundWaitingId, soundWaitingCustomPath, soundVolume, rightPanelVisible, maxAgents, debugOverlay, floatCorner, worktreesDir, mode, missionShowActivity,
     ntfyEnabled, ntfyServer, ntfyTopic, ntfyToken, ntfyEvents, ntfyOnlyWhenAway,
     petsEnabled, petsSpeech, petsLeveling, floatChatEnabled, floatChatOpen,
     sidebarWidth, rightPanelWidth, toastPosition, defaultChatAgent, spawnMode],
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

  function openSettings() {
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

  function setTheme(key: string) {
    theme.value = key;
  }

  function setMode(m: "terminal" | "claude" | "git" | "mission" | "dashboard") {
    mode.value = m;
  }

  function toggleDashboard() {
    mode.value = mode.value === "dashboard" ? "terminal" : "dashboard";
  }

  function toggleGitPanel() {
    mode.value = mode.value === "git" ? "terminal" : "git";
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

  return {
    settingsOpen,
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
    activeTheme,
    themes: THEMES,
    setTheme,
    soundEnabled,
    soundDoneEnabled,
    soundWaitingEnabled,
    soundDoneId,
    soundDoneCustomPath,
    soundWaitingId,
    soundWaitingCustomPath,
    soundVolume,
    maxAgents,
    debugOverlay,
    floatCorner,
    worktreesDir,
    mode,
    missionShowActivity,
    setMode,
    toggleDashboard,
    toggleGitPanel,
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
    missionActiveCount,
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
    sidebarWidth,
    rightPanelWidth,
    toastPosition,
    defaultChatAgent,
    spawnMode,
  };
});

<template>
  <div ref="hostEl" class="xterm-host flex-1 overflow-hidden p-2" />
  <div
    v-if="ui.debugOverlay"
    class="pointer-events-none absolute left-0.5 top-0.5 z-[9999] whitespace-pre border border-green-500 bg-black/80 px-[5px] py-[3px] font-mono text-[10px] leading-[1.3] text-green-500"
  >{{ dbg.text }}</div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import { registerTerm, unregisterTerm } from "@/lib/termRegistry";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { open as shellOpen } from "@tauri-apps/plugin-shell";
import { SerializeAddon } from "@xterm/addon-serialize";
import { attachRenderer } from "@/lib/termRenderer";
import type { ITerminalAddon } from "@xterm/xterm";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { getCurrentWebview } from "@tauri-apps/api/webview";
import { useUIStore } from "@/stores/ui";
import "@xterm/xterm/css/xterm.css";

function hexToRgba(hex: string, alpha: number): string {
  const h = hex.replace("#", "");
  if (h.length !== 6) return hex;
  const r = parseInt(h.substring(0, 2), 16);
  const g = parseInt(h.substring(2, 4), 16);
  const b = parseInt(h.substring(4, 6), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}

const props = defineProps<{ ptyId: number; cwd: string; initialCmd?: string; resultToken?: string; initiallyTitled?: boolean }>();
const emit = defineEmits<{ title: [t: string]; busy: [b: boolean]; needsInput: [b: boolean]; spawn: [req: { cmd: string; token: string; cwd: string }]; agentState: [s: string, detail?: string]; agentMeta: [meta: { model: string; source: string; title: string }]; agent: [b: boolean]; interrupt: []; cwd: [p: string] }>();

const ui = useUIStore();

// The whole UI is magnified by CSS `zoom: s` on #app (ui.ts). That breaks mouse
// selection in xterm: getCoords does `clientX - getBoundingClientRect().left`
// (ZOOMED/visual px) ÷ cell size (the canvas measureText metric, UNZOOMED layout
// px) — under an ancestor zoom the two disagree by `s`, so the selection lands a
// row/col off. Fix: counter-zoom the host to net-zoom-1 (zoom: 1/s) so rect and
// cell metric share one space, and re-grow the box so it still fills the pane.
//
// Crucially the re-grow is in PX (parent.clientWidth * s), NOT `s*100%`. A `%`
// width resolves against a containing block already inflated by the #app-zoom
// chain and compounds — measured at scale 1.23 it overflowed the pane by ~23%
// (host 1575px vs pane 1280px) and spilled off the window. PX from the parent's
// own layout width lands the zoomed footprint back exactly on the pane.
// Verified in a real WebKit-zoom browser: px → rect==offset (net-zoom-1) and
// host==pane (no overflow); % → 295px horizontal overflow.
// MUST round to an integer. A fractional font size (13 * 1.07 = 13.91px) gives a
// fractional cell height (× lineHeight 1.4); the canvas/WebGL renderer rounds each
// row independently, so the error accumulates down the grid and lower rows render
// on top of upper ones — the "doubled / strikethrough" scramble. Integer px keeps
// cell heights stable. Visible only when effectiveScale != 1 (counter-zoom active).
const scaledFontSize = () => Math.max(1, Math.round(ui.terminalFontSize * ui.effectiveScale));

const hostEl = ref<HTMLElement>();

// ── TEMP debug overlay ────────────────────────────────────────────────────────
// Visible readout to diagnose the prod-only blank terminal: does data arrive, does
// the xterm buffer fill, is the alt-screen active, is the host sized? Toggle with
// localStorage 'burrow-debug' = '1' (on by default here for the debug build).
let bytesRx = 0;
let writes = 0;
let txBack = 0;   // bytes xterm sent BACK to the pty (input + query responses)
let txBackN = 0;
// Raw capture of the first N bytes received, dumped to a file when the debug
// overlay is on — lets us inspect exactly what the agent emitted in a prod build
// (e.g. whether ?1049h alt-screen-enter arrives and how xterm reacted).
const dbg = ref({ text: "" });
function refreshDbg() {
  if (!ui.debugOverlay || !term) return;
  const host = hostEl.value;
  let bufLines = 0;
  let altType = "?";
  try {
    const b = term.buffer.active;
    altType = b.type;
    for (let i = 0; i < term.rows; i++) {
      const line = b.getLine(b.viewportY + i);
      if (line && line.translateToString(true).trim()) bufLines++;
    }
  } catch (e) { altType = "err:" + (e as Error).message; }
  dbg.value.text = [
    `pty ${props.ptyId}  ${term.cols}x${term.rows}`,
    `host ${host?.offsetWidth}x${host?.offsetHeight} scale ${ui.effectiveScale.toFixed(2)}`,
    `rx ${bytesRx}B writes ${writes}`,
    `txBack ${txBack}B / ${txBackN}`,
    `buf ${altType} lines ${bufLines}`,
    `age ${lastDataAt ? Math.round(performance.now() - lastDataAt) : "-"}ms`,
  ].join("\n");
}

function applyCounterZoom() {
  const el = hostEl.value;
  const parent = el?.parentElement;
  if (!el || !parent) return;
  const s = ui.effectiveScale;
  if (s === 1) {
    el.style.zoom = "";
    el.style.width = "";
    el.style.height = "";
    el.style.flex = "";
    return;
  }
  // Basis is the host's OWN natural flex slot, NOT parent.clientHeight: the parent
  // pane also holds a 26px titlebar when split, so parent.clientHeight overcounts
  // and the host spills past the pane bottom. Reset to flex first, read the laid-out
  // slot (flexbox already excludes the titlebar), then grow that by `s`.
  el.style.flex = "";
  el.style.zoom = "";
  el.style.width = "";
  el.style.height = "";
  const w = el.clientWidth;
  const h = el.clientHeight;
  el.style.flex = "none";
  el.style.zoom = String(1 / s);
  // Integer px: a sub-pixel box makes the renderer's backing canvas land on a
  // fractional device-pixel grid → blurry + drifting rows under the zoom.
  el.style.width = `${Math.round(w * s)}px`;
  el.style.height = `${Math.round(h * s)}px`;
}
let term: Terminal;
let fitAddon: FitAddon;
let serializeAddon: SerializeAddon;
let renderAddon: ITerminalAddon | null = null;
let unlisten: UnlistenFn | null = null;
let unlistenSnapReq: UnlistenFn | null = null;
let unlistenWrite: UnlistenFn | null = null;
let resizeObserver: ResizeObserver;
let resizeTimer: ReturnType<typeof setTimeout> | undefined;
let pollTimer: ReturnType<typeof setInterval>;
let dbgTimer: ReturnType<typeof setInterval>;

const CLAUDE_RE = /^claude$/i;
// Claude Code emits this exact OSC 0 title when idle/done (startup + after Stop).
// Block only THIS to prevent post-task idle reset. Allow all other titles (e.g. /rename).
const CLAUDE_IDLE_TITLE_RE = /^✳?\s*Claude\s+Code$/i;
const CODEX_RE = /^codex$/i;
const COPILOT_RE = /^copilot$/i;
const SHELL_RE = /^(zsh|bash|sh|fish|csh|tcsh|dash)$/;
// Legacy pattern-match fallback (used when hooks aren't active)
const NEEDS_INPUT_RE = /[›❯]|(\(y\/n\)|\[y\/n\]|\(Y\/n\)|\[Y\/n\])/i;
const ANSI_RE = /\x1b(?:\[[0-9;?]*[A-Za-z]|[^[])/g;
// OSC 9999 from the `burrow` CLI: \x1b]9999;spawn;<b64cmd>;<b64token>;<b64cwd>\x07
const SPAWN_RE = /\x1b\]9999;spawn;([A-Za-z0-9+/=]*);([A-Za-z0-9+/=]*);([A-Za-z0-9+/=]*)\x07/g;
// OSC 7: shell CWD hint — \e]7;file://hostname/path\a. Emitted by zsh/fish/bash
// after each `cd` when the user's shell config includes the osc7 hook. Lets us
// track live CWD without polling so `burrow spawn --cwd` always gets the right dir.
const OSC7_RE = /\x1b\]7;file:\/\/[^/]*(\/?[^\x07\x1b]*)\x07/g;
// OSC 133 shell integration markers — precise command boundary tracking without polling.
//   A=prompt-start  B=prompt-end  C=command-start  D;N=command-done(exit N)
// Supported by zsh (precmd/preexec hooks), bash (PS0/PROMPT_COMMAND), fish, iTerm2, etc.
const OSC133_RE = /\x1b\]133;([A-D])(?:;[^\x07]*)?\x07/g;
const b64decode = (s: string) =>
  s ? new TextDecoder().decode(Uint8Array.from(atob(s), (c) => c.charCodeAt(0))) : "";

// Last known foreground process name (from the poll) — gates OSC titles.
let foreground = "";
// True once the foreground agent has set its OWN title via OSC. After that the
// poll stops seeding "Claude" over it, so Claude Code's descriptive title sticks
// (and the tab tells you what that session was doing).
// Seeded from `initiallyTitled` on mount so a restored meaningful title is never
// overwritten by the initial "Claude" seed on reattach.
let agentTitled = false; // set in onMounted after props are available
// OSC title buffered before the first poll sets `foreground`. Non-cwd-like titles
// (agent titles with spaces/Unicode) take priority over shell cwd noise (bare words).
// Cleared on shell-returns and at trySend (pre-injection shell titles are discarded).
let pendingOscTitle: string | null = null;
// Last hook state — used to freeze the tab title after a turn ends so Claude's
// "Claude Code" idle-state title can't overwrite the task description.
let hookState: "idle" | "running" | "waiting" | "permission" | "done" | "error" = "idle";
// Agent process pid reported by the status hook (`burrow status --pid`), already
// gated to a LOCAL pid by Rust (a remote/SSH pid never reaches here). Drives the
// PID-liveness sweep in the poll below: when an in-flight agent's pid is gone we
// settle the stuck dot immediately, instead of waiting out the slower dead-PTY
// watchdog. null = no trusted local pid → only the watchdog applies.
let agentPid: number | null = null;

// Strip control/non-printable chars (mid-OSC replay garbage), trim, cap length.
function sanitizeTitle(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/[\x00-\x1f\x7f]/g, "").trim().slice(0, 80);
}

let outputBuffer = "";
let lastInterruptScanAt = 0;
// Timestamp of the last PTY output chunk — used to detect when the shell has
// finished its startup (sourcing .zprofile/.zshrc, printing the first prompt)
// and gone quiet, so we can inject the launch command without racing the init.
let lastDataAt = 0;
let hooksSettingsPath = "";
let unlistenHook: UnlistenFn | null = null;
let unlistenDrop: UnlistenFn | null = null;

// Image files an agent can read. Drag-dropped paths and clipboard image mimes are
// matched against this before we inject anything into the PTY.
const IMG_EXT_RE = /\.(png|jpe?g|gif|webp|bmp|svg|heic|heif|tiff?|avif)$/i;

// Type a filesystem path into THIS pty so the foreground agent (Claude Code,
// Copilot, …) picks it up as an image attachment — same as dragging a file into
// the agent's own terminal. Space-bearing paths are single-quoted so the shell /
// agent input parses them as one token; a trailing space ends the token.
function injectImagePath(path: string) {
  const quoted = /\s/.test(path) ? `'${path.replace(/'/g, "'\\''")}'` : path;
  const bytes = Array.from(new TextEncoder().encode(quoted + " "));
  invoke("write_pty", { id: props.ptyId, data: bytes });
}

// Cmd+V of an image: the DOM paste event carries the bitmap as a File. Persist it
// to a temp file via Rust, then inject the path. Text paste falls through to
// xterm's native handler untouched (no preventDefault on the non-image path).
async function onPaste(e: ClipboardEvent) {
  const items = e.clipboardData?.items;
  if (!items) return;
  for (const it of items) {
    if (it.kind !== "file" || !it.type.startsWith("image/")) continue;
    const file = it.getAsFile();
    if (!file) continue;
    e.preventDefault();
    // Claude Code reads the macOS clipboard itself on Ctrl+V (\x16) and inserts
    // an `[Image #N]` reference — far better than typing a temp path. The image
    // is still on the OS clipboard, so just forward \x16 and let Claude grab it.
    // Other agents (Copilot/Aider) lack clipboard-image support → temp-path path.
    if (CLAUDE_RE.test(foreground)) {
      invoke("write_pty", { id: props.ptyId, data: [0x16] });
      return;
    }
    const buf = new Uint8Array(await file.arrayBuffer());
    let bin = "";
    for (let i = 0; i < buf.length; i++) bin += String.fromCharCode(buf[i]);
    const b64 = btoa(bin);
    const ext = it.type.split("/")[1] || "png";
    try {
      const path = await invoke<string>("save_temp_image", { b64, ext });
      injectImagePath(path);
    } catch { /* clipboard write failed — leave the prompt alone */ }
    return;
  }
}

// Fit, then re-fit after layout and web-fonts settle. On restart the first fit
// can run before the surrounding panels are laid out or the mono web-font has
// loaded — xterm then measures the wrong cell width and picks too many cols/rows,
// so the terminal overflows the panel. The container size never changes again, so
// the ResizeObserver never corrects it. Re-fitting on the next frames + after
// fonts.ready re-measures with the real metrics and resizes the PTY to match.
let lastPtyCols = 0;
let lastPtyRows = 0;
// Viewport is "stuck to bottom" when the visible region is the live tail.
// While stuck, new output and reflows keep auto-scrolling; once the user scrolls
// up to read history, we leave the viewport alone until they return to the tail.
function isAtBottom(): boolean {
  if (!term) return true;
  const b = term.buffer.active;
  return b.viewportY >= b.baseY;
}

function safeFit(): boolean {
  if (!term || !fitAddon || !hostEl.value) return false;
  if (hostEl.value.offsetWidth === 0 || hostEl.value.offsetHeight === 0) return false;
  const stick = isAtBottom();
  fitAddon.fit();
  // A fit re-wraps scrollback (esp. when a hidden tab gets its real size on
  // reactivate), which can leave the viewport mid-buffer — re-pin to the tail.
  if (stick) term.scrollToBottom();
  // Only resize the PTY when the grid actually changed. Every resize_pty fires a
  // SIGWINCH which makes an alt-screen TUI (Claude Code, vim) repaint; firing it
  // on every observer tick spams repaints mid-stream and scrambles the screen.
  const changed = term.cols !== lastPtyCols || term.rows !== lastPtyRows;
  if (changed) {
    lastPtyCols = term.cols;
    lastPtyRows = term.rows;
    invoke("resize_pty", { id: props.ptyId, cols: term.cols, rows: term.rows });
  }
  notifyFloatGrid();
  return changed;
}

// Un-scramble a garbled screen: drop the renderer's glyph atlas and force a full
// redraw of every visible row from xterm's buffer. The scramble is a RENDER-side
// artifact (stale/overlapping glyphs the canvas never cleared), so the buffer is
// already correct — refresh() repaints it cleanly. No SIGWINCH/reflow here: a
// resize toggle re-wraps scrollback and can itself double lines.
function forceRepaint() {
  if (!term) return;
  try { (renderAddon as unknown as { clearTextureAtlas?: () => void })?.clearTextureAtlas?.(); } catch { /* DOM renderer: no atlas */ }
  term.refresh(0, term.rows - 1);
}

// Tell any floating mirror of this pty that the grid changed, so it can match
// cols/rows (the shared PTY's SIGWINCH already makes the agent repaint; the
// float just needs the new dims to render that repaint correctly).
let lastGridCols = 0;
let lastGridRows = 0;
function notifyFloatGrid() {
  if (!term) return;
  if (term.cols === lastGridCols && term.rows === lastGridRows) return;
  lastGridCols = term.cols;
  lastGridRows = term.rows;
  invoke("notify_float_grid", { ptyId: props.ptyId, cols: term.cols, rows: term.rows }).catch(() => {});
}
function deferredFit() {
  requestAnimationFrame(() => requestAnimationFrame(safeFit));
  document.fonts?.ready.then(safeFit).catch(() => {});
}

onMounted(async () => {
  // Seed from prop: if the restored leaf already has a non-default title, treat
  // the agent as "already titled" so the poll doesn't re-seed "Claude" over it.
  agentTitled = props.initiallyTitled ?? false;

  const initXtermTheme = ui.bgImageUrl
    ? { ...ui.activeTheme.xterm, background: hexToRgba(ui.activeTheme.xterm.background ?? "#0a0a0a", ui.bgOpacity) }
    : ui.activeTheme.xterm;

  term = new Terminal({
    theme: initXtermTheme,
    fontFamily: ui.terminalFont,
    fontSize: scaledFontSize(),
    lineHeight: 1.4,
    cursorBlink: true,
    cursorStyle: "bar",
    allowProposedApi: true,
    // Lets themes use rgba/transparent xterm backgrounds so the window
    // wallpaper / OS vibrancy shows through the terminal (meme + lime themes).
    allowTransparency: true,
    scrollback: 5000,
  });

  fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.loadAddon(new WebLinksAddon((e, uri) => {
    const isMac = /Mac/.test(navigator.platform || navigator.userAgent);
    if (isMac ? e.metaKey : e.ctrlKey) shellOpen(uri);
  }));
  // SerializeAddon lets a floating-bubble window request a snapshot of THIS
  // terminal's current screen (incl. alt-screen TUIs) to reconstruct it exactly
  // on expand — the daemon ring-buffer replay can't rebuild an alt-screen.
  serializeAddon = new SerializeAddon();
  term.loadAddon(serializeAddon);
  term.open(hostEl.value!);
  // GPU renderer (WebGL → Canvas → DOM). Must follow open(). Default DOM
  // renderer is the slowest; this is the big win for agent output floods.
  renderAddon = attachRenderer(term);

  applyCounterZoom();
  safeFit();
  deferredFit();
  // Ring-buffer replay from the daemon streams in after the rAF-deferred fit.
  // A further delayed repaint clears any stale atlas glyphs without a SIGWINCH.
  setTimeout(() => forceRepaint(), 350);

  // OSC title sequences set by the shell or programs (e.g. vim, tmux, Claude).
  // The interactive shell (zsh/bash) sets the OSC title to the cwd or last
  // command as cosmetics — junk for a tab name — so those are ignored. But an
  // AGENT's own title IS wanted: Claude Code sets a descriptive title (the task
  // it's on), which is exactly what tells you what each tab was doing. We accept
  // it and flag `agentTitled` so the poll stops re-seeding "Claude" over it.
  // (Truncation risk: on reattach the daemon ring buffer can replay a snapshot
  // starting mid-OSC; sanitizeTitle strips the control garbage.)
  term.onTitleChange((raw) => {
    const title = sanitizeTitle(raw);
    if (!title) return;
    if (!foreground) {
      // Foreground not yet known (first poll still in flight). Always buffer so
      // Claude's startup OSC title isn't lost, but don't let shell precmd/preexec
      // cwd noise (e.g. "work", "agentic-ide") overwrite a meaningful title that
      // arrived first. Shell cwd strings are bare single words; agent titles have
      // spaces or Unicode. Priority: non-cwd > cwd (last cwd still wins over cwd).
      const cwdBase = props.cwd?.replace(/.*\//, '') ?? '';
      const isCwdNoise = (t: string) =>
        t === cwdBase || /^[\w.-]+$/.test(t) || t.startsWith('/') || t.startsWith('~');
      if (!pendingOscTitle || isCwdNoise(pendingOscTitle) || !isCwdNoise(title)) {
        pendingOscTitle = title;
      }
      return;
    }
    if (SHELL_RE.test(foreground)) return;   // shell prompt cwd/cmd junk
    if (CLAUDE_RE.test(foreground) || CODEX_RE.test(foreground) || COPILOT_RE.test(foreground)) {
      // After Stop, Claude resets its OSC title to "✳ Claude Code" (idle state).
      // Block ONLY that specific idle reset — not all titles — so /rename still works
      // and future Claude versions that emit task-specific titles aren't blocked.
      if (hookState === "done" && CLAUDE_IDLE_TITLE_RE.test(title)) return;
      agentTitled = true;
    }
    emit("title", title);
  });

  // Agent status (running/waiting/done) is driven by hooks installed GLOBALLY in
  // each agent's config (~/.claude/settings.json, ~/.codex/hooks.json) by Rust at
  // startup. Those hooks run `burrow hook`, which POSTs to the local hook HTTP
  // server; Rust re-emits a `pty-hook-{id}` Tauri event that the listener below
  // turns into busy/needsInput/done. Because they're global + env-driven, they fire
  // for EVERY agent session in this PTY — launched-by-button, typed by hand, or
  // reattached after restart. The poll never fabricates "busy" for an agent
  // process, so these events are the sole source of truth (no stuck orange dot).
  const baseCmd = props.initialCmd?.trim().split(/\s+/)[0] ?? "";
  let launchArgs = "";

  // Per-tab result capture for `burrow collect <token>`. Two layers:
  // 1. --settings per-launch hook (works when --settings takes precedence over global)
  // 2. Token sidecar file (fallback: global burrow hook reads it on Stop, so capture
  //    works even if --settings hooks are overridden by ~/.claude/settings.json in
  //    newer Claude Code versions)
  if (baseCmd === "claude" && props.resultToken) {
    hooksSettingsPath = `/tmp/agentic-ide-hooks-${props.ptyId}.json`;
    const hooksJson = JSON.stringify({
      hooks: {
        Stop: [{ hooks: [{ type: "command", command: `burrow capture ${props.resultToken}` }] }],
      },
    });
    await invoke("write_text_file", { path: hooksSettingsPath, content: hooksJson });
    launchArgs = `--settings ${hooksSettingsPath}`;
    // Sidecar: global burrow hook Stop handler reads this file and calls burrow capture
    // when --settings hooks are silently ignored (Claude Code ≥ 2.1.195 behaviour).
    await invoke("write_text_file", {
      path: `/tmp/agentic-ide-token-${props.ptyId}`,
      content: props.resultToken,
    });
  }

  // Give button/in-app-launched claude tabs the Burrow MCP tools. The
  // `burrow spawn` delegation path already injects `--mcp-config` into the cmd
  // body (take_spawn_requests), so skip if it's already present to avoid a
  // duplicate. Hand-typed `claude` can't be covered here (no launch cmd).
  if (baseCmd === "claude" && !props.initialCmd!.includes("--mcp-config")) {
    try {
      const flag = await invoke<string>("burrow_mcp_flag", { cwd: props.cwd });
      if (flag) launchArgs = launchArgs ? `${launchArgs} ${flag}` : flag;
    } catch { /* MCP unavailable (browser-only dev) — launch without tools */ }
  }

  // Register all three listeners in parallel before creating the PTY — they are
  // independent and each round-trips to the Tauri IPC bridge, so sequencing them
  // added ~3× the latency for no reason.
  [unlistenHook, unlistenWrite, unlistenSnapReq] = await Promise.all([
    // Forward the agent's hook state straight through as ONE semantic event. A
    // single running|waiting|done event has no ordering hazard; Terminal.vue owns
    // the transition. The 2s poll never fabricates agent status, so these hooks
    // are the sole source of truth for an agent's running/waiting/done.
    // Payload is either a bare state string (legacy) or an object carrying the
    // state plus extras: `detail` for an `error`, and `{model,source,title}` for a
    // `session` (SessionStart metadata). Normalize, then fan out ONE event so there
    // is no ordering hazard — Terminal.vue owns the transition.
    listen<string | { state: string; detail?: string; model?: string; source?: string; title?: string; pid?: number }>(
      `pty-hook-${props.ptyId}`,
      (event) => {
        const p = event.payload;
        const obj = typeof p === "object" && p ? p : null;
        const state = obj ? obj.state : p;
        // Trusted local agent pid (Rust dropped any remote/SSH pid). Refresh it on
        // every event that carries one so the sweep always polls the live process.
        if (obj && typeof obj.pid === "number") agentPid = obj.pid;
        if (state === "session") {
          // Not a status — pure metadata (model/title from SessionStart). Surface it
          // up so Terminal.vue can store it; never touch the status dot.
          emit("agentMeta", {
            model: obj?.model ?? "",
            source: obj?.source ?? "",
            title: obj?.title ?? "",
          });
          return;
        }
        if (
          state === "running" || state === "waiting" || state === "permission" ||
          state === "done" || state === "error"
        ) {
          hookState = state as typeof hookState;
          // `error` carries a detail string (rate_limit|overloaded|…); pass it through.
          emit("agentState", state, state === "error" ? obj?.detail : undefined);
        }
      },
    ),
    // tmux send-keys path: the shim POSTs /write → hook server emits this event →
    // we forward to the daemon as regular PTY input.
    listen<string>(`pty-write-${props.ptyId}`, (event) => {
      const bytes = Array.from(new TextEncoder().encode(event.payload));
      invoke("write_pty", { id: props.ptyId, data: bytes });
    }),
    // Float-bubble snapshot responder: a floating window mirroring THIS pty asks
    // for the current screen on expand. SerializeAddon rebuilds the exact visible
    // state (alt-screen + modes) which the float writes into a fresh xterm. We
    // never tear this down per-tab visibility — main XTerms stay mounted (v-show),
    // so a hidden tab still answers. (tauriEmit, not the Vue `emit` above.)
    listen(`float-snap-req-${props.ptyId}`, async () => {
      try {
        // Send the grid dims too: the float must use the SAME cols/rows so the
        // serialized screen (and subsequent live bytes, laid out for this grid)
        // render identically — it font-scales to fit instead of reflowing.
        await invoke("send_float_snapshot", {
          ptyId: props.ptyId,
          data: serializeAddon.serialize(),
          cols: term.cols,
          rows: term.rows,
        });
      } catch { /* float falls back to live-only after its timeout */ }
    }),
  ]);

  // Create PTY
  await invoke("create_pty", {
    id: props.ptyId,
    cwd: props.cwd,
    cols: term.cols,
    rows: term.rows,
  });

  // Stream output from Rust → xterm
  unlisten = await listen<number[]>(`pty-data-${props.ptyId}`, (event) => {
    const bytes = new Uint8Array(event.payload);
    // Capture stick BEFORE the write grows the buffer; re-pin in the parse
    // callback so a fast agent flood can't leave the viewport stranded.
    const stick = isAtBottom();
    term.write(bytes, () => { if (stick) term.scrollToBottom(); });
    bytesRx += bytes.length;
    writes++;
    const text = new TextDecoder().decode(bytes);

    // `burrow spawn` requests: decode base64 fields → open a new tab.
    // Loop, since one chunk may carry several.
    SPAWN_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = SPAWN_RE.exec(text)) !== null) {
      try {
        const cmd = b64decode(m[1]).trim();
        if (cmd) emit("spawn", { cmd, token: b64decode(m[2]).trim(), cwd: b64decode(m[3]).trim() });
      } catch { /* ignore malformed payload */ }
    }

    // OSC 7: shell CWD hint. Passive — no-op if the user's shell doesn't emit it.
    OSC7_RE.lastIndex = 0;
    while ((m = OSC7_RE.exec(text)) !== null) {
      const p = decodeURIComponent(m[1]);
      if (p) emit("cwd", p);
    }

    // OSC 133 shell integration: C=command-start → busy, D=command-done → idle.
    // Only drives plain-shell busy; agent sessions ignore it (hooks are authoritative).
    if (!isAgentSession) {
      OSC133_RE.lastIndex = 0;
      while ((m = OSC133_RE.exec(text)) !== null) {
        if (m[1] === "C") emit("busy", true);
        else if (m[1] === "D") emit("busy", false);
      }
    }

    outputBuffer = (outputBuffer + text).slice(-500);
    lastDataAt = performance.now();

    // ponytail: output scan for Ctrl+C interrupted state — no hook fires in this case
    if (isAgentSession && hookState === "running") {
      const now = performance.now();
      if (now - lastInterruptScanAt > 2000) {
        const plain = outputBuffer.replace(ANSI_RE, "");
        if (plain.includes("Interrupted by user") || (plain.includes("Interrupted") && plain.includes("What should"))) {
          lastInterruptScanAt = now;
          hookState = "waiting";
          emit("agentState", "waiting");
        }
      }
    }
  });

  // Send initial command once the shell is actually ready (inject --settings for
  // claude). A fixed timeout raced slow startups (a login shell sourcing
  // .zprofile/.zshrc, an .zshrc that errors): the command was typed before the
  // prompt existed, so the newline got eaten and the command sat unrun — or input
  // interleaved with the prompt and zsh parsed the prompt text as a command.
  // Instead wait for the PTY to emit output and then fall quiet (prompt printed,
  // init done), with a hard cap so we never hang if the shell stays silent.
  if (props.initialCmd) {
    // Insert launchArgs right after the binary name so flags precede the
    // prompt positional arg — newer Claude Code requires flags before positional args.
    const cmd = launchArgs
      ? props.initialCmd!.replace(/^(\S+)/, `$1 ${launchArgs}`)
      : props.initialCmd!;
    const QUIET_MS = 250;   // silence that signals "prompt is ready"
    const MAX_WAIT_MS = 5000;
    const startedAt = performance.now();
    const trySend = () => {
      const now = performance.now();
      const ready = lastDataAt > 0 && now - lastDataAt >= QUIET_MS;
      if (ready || now - startedAt >= MAX_WAIT_MS) {
        // Discard any OSC title the shell set during its startup (cwd, last
        // command, etc.) — those arrived before the agent command was injected
        // and are noise for the tab name.
        pendingOscTitle = null;
        const bytes = Array.from(new TextEncoder().encode(cmd + "\n"));
        invoke("write_pty", { id: props.ptyId, data: bytes });
      } else {
        setTimeout(trySend, 100);
      }
    };
    setTimeout(trySend, 100);
  }

  // Custom key handling on top of xterm's defaults.
  term.attachCustomKeyEventHandler((e: KeyboardEvent) => {
    // Cmd+K → clear the terminal (iTerm-style: wipe scrollback + viewport, keep
    // the current prompt line). xterm's clear() drops every line above the cursor
    // row; we swallow the key so it never reaches the PTY.
    if (e.metaKey && !e.ctrlKey && !e.altKey && (e.key === "k" || e.key === "K")) {
      if (e.type === "keydown") term.clear();
      return false;
    }
    // Cmd+Shift+R → force a full repaint to un-scramble an alt-screen TUI that
    // got garbled (e.g. a mid-redraw resize during a handoff/compaction reprint).
    // Nudges SIGWINCH so the agent re-emits the whole screen. Swallowed.
    if (e.metaKey && e.shiftKey && !e.ctrlKey && !e.altKey && (e.key === "r" || e.key === "R")) {
      e.preventDefault(); // else the webview hard-reloads on this combo
      if (e.type === "keydown") forceRepaint();
      return false;
    }
    // Shift+Enter → CSI u escape (kitty protocol) so Claude Code inserts a newline.
    if (e.key === "Enter" && e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) {
      if (e.type === "keydown") send("\x1b[13;2u");
      return false; // prevent xterm from also sending \r
    }
    // Option/Alt + ←/→ → word-wise cursor movement. macOS terminals map Option
    // to readline's word-left/right (ESC b / ESC f); xterm.js doesn't do this on
    // its own, so emit the sequences ourselves and swallow the key.
    if (e.altKey && !e.ctrlKey && !e.metaKey && (e.key === "ArrowLeft" || e.key === "ArrowRight")) {
      if (e.type === "keydown") send(e.key === "ArrowLeft" ? "\x1bb" : "\x1bf");
      return false;
    }
    // Option/Alt + Backspace → delete the previous word (readline: ESC DEL).
    if (e.altKey && !e.ctrlKey && !e.metaKey && (e.key === "Backspace" || e.key === "Delete")) {
      if (e.type === "keydown") send("\x1b\x7f");
      return false;
    }
    return true;
  });

  function send(s: string) {
    invoke("write_pty", { id: props.ptyId, data: Array.from(new TextEncoder().encode(s)) });
  }

  // Send input from xterm → Rust PTY
  term.onData((data) => {
    // Interrupt detection: a bare ESC (single 0x1b — NOT an escape sequence like
    // arrows "\x1b[A") or Ctrl+C (0x03) cancels an agent's running turn. Agents
    // fire NO Stop hook on interrupt, and the foreground poll never clears an
    // agent's "running" (claude stays foreground at its prompt) → the dot would
    // stick orange forever. Forward as a semantic interrupt so Terminal can
    // settle the leaf back to idle. Generic = works for every agent (claude,
    // codex, aider…). No-op if nothing was running.
    if (data === "\x1b" || data === "\x03") emit("interrupt");
    const bytes = Array.from(new TextEncoder().encode(data));
    txBack += bytes.length;
    txBackN++;
    invoke("write_pty", { id: props.ptyId, data: bytes });
  });

  // Resize. Observe the PARENT, not the host: under a non-1 scale the host's own
  // size is driven by applyCounterZoom (explicit px), so watching the host would
  // miss pane/window resizes and risk a feedback loop. The parent reflects the
  // real available space — recompute the counter-zoom box, then refit.
  // Counter-zoom box is cheap + DOM-only, so keep it live for a smooth visual
  // drag. The PTY resize is debounced: a pane/window drag fires this dozens of
  // times, and each safeFit() → resize_pty → SIGWINCH; spamming SIGWINCH while
  // the agent is repainting is what scrambles the screen. Coalesce to one fit
  // (+ full repaint) once the drag settles.
  resizeObserver = new ResizeObserver(() => {
    applyCounterZoom();
    if (resizeTimer) clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => { if (safeFit()) forceRepaint(); }, 80);
  });
  resizeObserver.observe(hostEl.value!.parentElement ?? hostEl.value!);

  // Cmd+V image paste → temp file → path into the PTY (text paste untouched).
  term.textarea?.addEventListener("paste", onPaste);

  // Drag-and-drop image files. Tauri intercepts OS drops (dragDrop default-on), so
  // the bitmap never reaches the DOM — we listen to the window-level drop event
  // instead. It fires for ALL panes, so route by hit-testing the drop point
  // against THIS host's rect: only the pane under the cursor injects the path.
  // Dropped files already have a real path, so no temp copy is needed.
  unlistenDrop = await getCurrentWebview().onDragDropEvent((event) => {
    if (event.payload.type !== "drop") return;
    const rect = hostEl.value?.getBoundingClientRect();
    if (!rect) return;
    const dpr = window.devicePixelRatio || 1;
    const cx = event.payload.position.x / dpr;
    const cy = event.payload.position.y / dpr;
    if (cx < rect.left || cx > rect.right || cy < rect.top || cy > rect.bottom) return;
    for (const p of event.payload.paths) {
      if (IMG_EXT_RE.test(p)) injectImagePath(p);
    }
  });

  // Poll foreground process → auto-title. Runs once immediately (so tabs get a
  // correct name right after reload instead of waiting 2s) then every 2s.
  let lastProcess = "";
  // Sticky across polls: once an agent is seen foreground, the session stays
  // "agent" until the shell returns. Child processes the agent spawns (a pager,
  // git, node) then can't steal the tab name mid-conversation (the rename bug).
  let isAgentSession = false;
  // Consecutive empty-foreground polls. A single empty read is normal (daemon
  // race), but a sustained streak on an in-flight agent can mean a dead PTY whose
  // `done` hook never fired — the stuck-dot watchdog below acts only after this.
  let emptyForegroundStreak = 0;
  const poll = async () => {
    // PID-liveness sweep (supacode-style): if the agent reported a trusted local
    // pid and that process is gone while the leaf is still in-flight, the turn can
    // never finish — no Stop hook will ever fire. Settle the dot immediately rather
    // than waiting out the 3-empty-poll dead-PTY watchdog below. This catches a
    // crashed/killed agent even when its (defunct) process still lingers in the
    // foreground read. The watchdog stays as the fallback for the no-pid case.
    if (
      agentPid !== null &&
      (hookState === "running" || hookState === "waiting" || hookState === "permission")
    ) {
      const alive = await invoke<boolean>("is_pid_alive", { pid: agentPid }).catch(() => true);
      if (!alive) {
        hookState = "idle";
        agentPid = null;
        emit("interrupt"); // agent process gone, no clean `done` → settle the stuck dot
        return;
      }
    }

    const proc = await invoke<string>("get_pty_foreground", { id: props.ptyId });
    // Empty foreground = no non-shell process in the group: either a daemon
    // race/mid-conversation read (must NOT reset an agent's title/state) OR a
    // plain command just exited and only the shell remains. For an agent
    // session, skip — keep last known title/state (the "Terminal N" reset bug).
    // For a plain terminal that was busy, empty means the command finished and
    // we're back at the prompt → clear busy, else the orange dot sticks forever
    // (foreground_name returns "" for a bare shell, so SHELL_RE never fires).
    if (!proc) {
      emptyForegroundStreak++;
      // Stuck-state watchdog: an agent leaf still in-flight (per its last hook)
      // with foreground empty for several polls may be a genuinely dead PTY — the
      // process was killed/crashed and no Stop hook fired, so the dot would stick
      // forever. A single empty read is just a transient race, so we only act after
      // a streak AND confirm the PTY is actually dead in the daemon before settling.
      if (
        isAgentSession &&
        emptyForegroundStreak >= 3 &&
        (hookState === "running" || hookState === "waiting" || hookState === "permission")
      ) {
        const alive = await invoke<{ pty_id: number; alive: boolean }[]>("list_pty_sessions")
          .then((ss) => ss.find((s) => s.pty_id === props.ptyId)?.alive ?? true)
          .catch(() => true);
        if (!alive) {
          hookState = "idle";
          agentPid = null;
          emit("interrupt"); // dead PTY, no clean `done` → settle the stuck dot
        }
      }
      if (!isAgentSession && lastProcess && !SHELL_RE.test(lastProcess)) {
        lastProcess = "";
        emit("busy", false);
        // No title reset — names are fully sticky. A transient empty-foreground
        // read between commands must not wipe the last meaningful title.
      }
      return;
    }
    emptyForegroundStreak = 0;
    foreground = proc;
    if (proc === lastProcess) return;
    lastProcess = proc;

    const isClaude = CLAUDE_RE.test(proc);
    const isAgent = isClaude || CODEX_RE.test(proc) || COPILOT_RE.test(proc);

    if (SHELL_RE.test(proc)) {
      // Back at the shell prompt → whatever ran (agent or command) has exited.
      // Clear running state (rescues a stuck dot if an agent was interrupted with
      // no done hook). Names are fully sticky — do NOT reset the title here, so
      // a tab keeps its last meaningful name across turn boundaries and on restart.
      isAgentSession = false;
      agentPid = null;          // agent exited cleanly → stop sweeping its old pid
      pendingOscTitle = null;   // discard any pre-shell-exit buffered title
      // Keep agentTitled as-is: if an agent set a meaningful title, it persists.
      emit("agent", false);
      emit("busy", false);
    } else if (isAgent) {
      // An agent is the foreground process — but it stays foreground whether it's
      // THINKING or sitting idle at its own prompt. Presence is NOT "busy": the
      // poll must never fabricate a status here, or the spinner sticks forever.
      // running/waiting/done come ONLY from the agent's hooks (listener above).
      isAgentSession = true;
      emit("agent", true);        // mark the tab as an agent (robot icon)
      // Apply any OSC title buffered before foreground was known (early-start
      // race). Otherwise fall back to seeding "Claude" until the agent sets its own.
      if (!agentTitled && pendingOscTitle) {
        const cwdBase = props.cwd?.replace(/.*\//, '') ?? '';
        const isCwdNoise = (t: string) =>
          t === cwdBase || /^[\w.-]+$/.test(t) || t.startsWith('/') || t.startsWith('~');
        if (!isCwdNoise(pendingOscTitle)) {
          agentTitled = true;
          emit("title", pendingOscTitle);
        }
        pendingOscTitle = null;
      }
      if (!agentTitled) {
        emit("title", isClaude ? "Claude" : proc);
      }
    } else if (isAgentSession) {
      // A non-shell child process INSIDE a live agent session (the agent opened a
      // pager, ran git, spawned node…). Keep the agent's title and don't flip to
      // a plain-command "busy" — the agent's hooks remain the status source.
    } else {
      // Plain foreground command (npm test, vim, python…): presence == busy.
      emit("agent", false);
      emit("busy", true);
      const stripped = outputBuffer.replace(ANSI_RE, "");
      emit("needsInput", NEEDS_INPUT_RE.test(stripped.slice(-200)));
      emit("title", proc);        // e.g. "vim", "python3", "node"
    }
  };
  poll();
  pollTimer = setInterval(poll, 2000);
  dbgTimer = setInterval(refreshDbg, 500);

  // Let the control API read this tab's output (`burrow tab-output`), which is
  // how a Manager checks on an agent mid-task instead of waiting for its result.
  registerTerm(props.ptyId, { readOutput });
});

/**
 * The tail of the buffer as plain text. Trailing blank rows are dropped (an
 * agent's TUI pads the screen, and a Manager reading 80 lines of padding learns
 * nothing), and each row is right-trimmed.
 */
function readOutput(lines: number): string {
  if (!term) return "";
  const b = term.buffer.active;
  const rows: string[] = [];
  for (let y = 0; y < b.baseY + b.cursorY + 1; y++) {
    rows.push(b.getLine(y)?.translateToString(true).replace(/\s+$/, "") ?? "");
  }
  while (rows.length && rows[rows.length - 1] === "") rows.pop();
  return rows.slice(-lines).join("\n");
}

onBeforeUnmount(async () => {
  unregisterTerm(props.ptyId);
  clearInterval(pollTimer);
  clearInterval(dbgTimer);
  resizeObserver?.disconnect();
  if (resizeTimer) clearTimeout(resizeTimer);
  unlisten?.();
  unlistenHook?.();
  unlistenSnapReq?.();
  unlistenWrite?.();
  unlistenDrop?.();
  term?.textarea?.removeEventListener("paste", onPaste);
  // detach_pty closes the data stream but leaves the PTY alive in the daemon,
  // so it can be reattached after app restart.
  await invoke("detach_pty", { id: props.ptyId });
  renderAddon?.dispose();
  term?.dispose();
});

// Live-apply terminal font + UI-scale changes. The host counter-zooms to net-1,
// so the visual size comes from xterm's own fontSize (scaled by effectiveScale);
// a scale change also resizes the counter-zoom box, so recompute it then refit.
watch(
  () => [ui.terminalFont, ui.terminalFontSize, ui.effectiveScale],
  ([font]) => {
    if (!term) return;
    term.options.fontFamily = font as string;
    term.options.fontSize = scaledFontSize();
    applyCounterZoom();
    fitAddon?.fit();
    lastPtyCols = term.cols;
    lastPtyRows = term.rows;
    invoke("resize_pty", { id: props.ptyId, cols: term.cols, rows: term.rows });
    // Font/scale change resizes glyphs → the atlas still holds old-size glyphs.
    // Clear + full redraw so the new metrics render cleanly (no doubled rows).
    forceRepaint();
  },
);

// Live-apply theme changes to the running terminal.
watch(
  () => ui.activeTheme,
  (t) => {
    if (!term) return;
    term.options.theme = ui.bgImageUrl
      ? { ...t.xterm, background: hexToRgba(t.xterm.background ?? "#0a0a0a", ui.bgOpacity) }
      : t.xterm;
  },
);

// When wallpaper or opacity changes, re-tint the xterm canvas background.
watch(
  () => [ui.bgImageUrl, ui.bgOpacity] as const,
  ([url, op]) => {
    if (!term) return;
    const base = ui.activeTheme.xterm;
    term.options.theme = url
      ? { ...base, background: hexToRgba(base.background ?? "#0a0a0a", op) }
      : base;
  },
);

defineExpose({
  focus() { term?.focus(); },
  readOutput,
  refit() { safeFit(); deferredFit(); },
  // Force a full TUI redraw — un-scramble a garbled alt-screen agent.
  repaint() { forceRepaint(); },
  // Inject text into the PTY (no trailing newline — user reviews then hits Enter).
  sendText(text: string) {
    const bytes = Array.from(new TextEncoder().encode(text));
    invoke("write_pty", { id: props.ptyId, data: bytes });
    term?.focus();
  },
});
</script>

<style scoped>
.xterm-host :deep(.xterm) { height: 100%; }
.xterm-host :deep(.xterm-viewport) { background: transparent !important; }
</style>

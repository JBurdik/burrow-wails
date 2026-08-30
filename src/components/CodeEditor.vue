<template>
  <div class="flex min-h-0 min-w-0 flex-1 overflow-hidden">
    <div v-if="placeholder" class="flex flex-1 flex-col items-center justify-center gap-2.5 bg-terminal-bg text-xs text-muted-foreground">
      <PhFileX :size="34" weight="thin" />
      <p>{{ placeholder }}</p>
    </div>
    <div v-else ref="host" class="editor-host min-h-0 flex-1 overflow-hidden" />
  </div>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import { PhFileX } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { EditorState, Compartment, Prec } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
import { basicSetup } from "codemirror";
import { oneDark } from "@codemirror/theme-one-dark";
import { javascript } from "@codemirror/lang-javascript";
import { rust } from "@codemirror/lang-rust";
import { json } from "@codemirror/lang-json";
import { html } from "@codemirror/lang-html";
import { css } from "@codemirror/lang-css";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { lspExtension } from "@/lib/lsp";
import { jumpToDefinition } from "@codemirror/lsp-client";
import { useUIStore } from "@/stores/ui";
import { useEditorContextStore } from "@/stores/editorContext";

const props = defineProps<{ leafId: number; path: string; cwd: string; initialLine?: number }>();
const emit = defineEmits<{
  title: [t: string];
  dirty: [d: boolean];
  saved: [];
  error: [msg: string];
}>();

const ui = useUIStore();
const editorCtx = useEditorContextStore();
const host = ref<HTMLElement | null>(null);
const placeholder = ref<string>("");

// Publish the current selection (or clear it) to the shared editor-context store
// so the Claude chat can offer "share selection". Lines are 1-based.
function publishSelection(state: EditorState) {
  editorCtx.setActivePath(props.path);
  const range = state.selection.main;
  if (range.empty) { editorCtx.clear(); return; }
  const text = state.sliceDoc(range.from, range.to);
  editorCtx.setSelection({
    path: props.path,
    startLine: state.doc.lineAt(range.from).number,
    endLine: state.doc.lineAt(range.to).number,
    text,
  });
}

// CM owns its own DOM + state. NEVER wrap in ref/reactive — Vue's proxy corrupts
// CM internals. Bare module-local handle.
let view: EditorView | null = null;
let resizeObserver: ResizeObserver | undefined;
let savedDoc = "";
let lastDirty = false;
let saving = false;

const themeCompartment = new Compartment();

function basename(p: string): string {
  return p.split("/").pop() || p;
}

// Pick a CodeMirror language by file extension. Unknown → no language (plain).
function detectLanguage(p: string) {
  const ext = p.split(".").pop()?.toLowerCase() ?? "";
  switch (ext) {
    case "ts":
    case "tsx":
    case "mts":
    case "cts":
      return javascript({ typescript: true, jsx: ext === "tsx" });
    case "js":
    case "jsx":
    case "mjs":
    case "cjs":
      return javascript({ jsx: ext === "jsx" });
    case "rs":
      return rust();
    case "json":
      return json();
    case "html":
    case "htm":
    case "vue":
      return html();
    case "css":
    case "scss":
    case "less":
      return css();
    case "md":
    case "markdown":
      return markdown();
    case "py":
      return python();
    default:
      return [];
  }
}

function editorTheme() {
  return EditorView.theme({
    // Font is scaled by effectiveScale because the host counter-zooms to net-1
    // (see applyCounterZoom) — the visual size must come from the font, like XTerm.
    "&": { height: "100%", fontSize: `${ui.terminalFontSize * ui.effectiveScale}px` },
    ".cm-scroller": { fontFamily: ui.terminalFont, overflow: "auto" },
    ".cm-content": { fontFamily: ui.terminalFont },
    // ── LSP hover / signature tooltips (VS Code-style rich docs) ──
    ".cm-tooltip": {
      border: "1px solid #2a2a2a",
      borderRadius: "6px",
      background: "#1b1b1f",
      boxShadow: "0 8px 28px rgba(0,0,0,0.5)",
    },
    ".cm-tooltip.cm-tooltip-hover, .cm-tooltip-section": {
      maxWidth: "560px",
    },
    ".cm-lsp-documentation": {
      padding: "8px 12px",
      fontFamily: "var(--font-ui)",
      fontSize: "12px",
      lineHeight: "1.5",
      maxHeight: "360px",
      overflow: "auto",
    },
    ".cm-lsp-documentation pre, .cm-lsp-documentation code": {
      fontFamily: ui.terminalFont,
      fontSize: "12px",
    },
    ".cm-lsp-documentation pre": {
      margin: "6px 0",
      padding: "8px 10px",
      background: "#0d0d10",
      borderRadius: "5px",
      overflow: "auto",
      whiteSpace: "pre-wrap",
    },
    ".cm-lsp-documentation p": { margin: "4px 0" },
    ".cm-lsp-documentation a": { color: "var(--accent)" },
    ".cm-lsp-documentation hr": {
      border: "none",
      borderTop: "1px solid #2a2a2a",
      margin: "8px 0",
    },
  });
}

// The whole UI is magnified by CSS `zoom: s` on #app (ui.ts). That desyncs mouse
// coordinates from CodeMirror's getBoundingClientRect metrics, so mouse-driven
// features (hover tooltips, click-to-position) land off or fail entirely while
// keyboard-driven ones (completion) work. Same root cause + fix as XTerm: counter-
// zoom the host to net-zoom-1 (zoom: 1/s) so rects and mouse share one space, and
// re-grow the box in PX (not %, which compounds against the zoomed containing
// block) so it still fills the pane. Visual size then comes from the scaled font.
function applyCounterZoom() {
  const el = host.value;
  const parent = el?.parentElement;
  if (!el || !parent) return;
  const s = ui.effectiveScale;
  if (s === 1) {
    el.style.zoom = "";
    el.style.width = "";
    el.style.height = "";
    el.style.flex = "";
    view?.requestMeasure();
    return;
  }
  el.style.flex = "";
  el.style.zoom = "";
  el.style.width = "";
  el.style.height = "";
  const w = el.clientWidth;
  const h = el.clientHeight;
  el.style.flex = "none";
  el.style.zoom = String(1 / s);
  el.style.width = `${w * s}px`;
  el.style.height = `${h * s}px`;
  view?.requestMeasure();
}

function recomputeDirty() {
  if (!view) return;
  const dirty = view.state.doc.toString() !== savedDoc;
  if (dirty !== lastDirty) {
    lastDirty = dirty;
    emit("dirty", dirty);
  }
}

async function save() {
  if (!view || saving) return;
  saving = true;
  // Format before writing so the saved file matches what formatDocument would
  // produce. formatDocument never throws (reports via 'error' emit), so a
  // formatter failure can't block or corrupt the save.
  await formatDocument();
  const content = view.state.doc.toString();
  try {
    await invoke("write_text_file", { path: props.path, content });
    savedDoc = content;
    if (lastDirty) {
      lastDirty = false;
      emit("dirty", false);
    }
    emit("saved");
  } catch (e) {
    emit("error", String(e));
  } finally {
    saving = false;
  }
}

// Format via the backend (rustfmt/prettier by extension) and replace the doc
// in place. Resilient by design: on error or no-op change it just returns.
async function formatDocument() {
  if (!view) return;
  const content = view.state.doc.toString();
  let formatted: string;
  try {
    formatted = await invoke<string>("format_source", { path: props.path, content, cwd: props.cwd });
  } catch (e) {
    emit("error", String(e));
    return;
  }
  if (formatted === content) return;
  const cursor = Math.min(view.state.selection.main.head, formatted.length);
  view.dispatch({
    changes: { from: 0, to: view.state.doc.length, insert: formatted },
    selection: { anchor: cursor },
  });
}

function isDirty(): boolean {
  return lastDirty;
}

function focus() {
  view?.focus();
}

onMounted(async () => {
  emit("title", basename(props.path));

  let content: string;
  try {
    content = await invoke<string>("read_text_file_checked", { path: props.path });
  } catch (e) {
    const msg = String(e);
    if (msg.includes("binary")) placeholder.value = "Binary file — cannot edit";
    else if (msg.includes("too-large")) placeholder.value = "File too large to open";
    else {
      placeholder.value = "Could not open file";
      emit("error", msg);
    }
    return;
  }

  savedDoc = content;
  if (!host.value) return;

  // LSP (completion/hover/diagnostics/go-to-def) for supported languages. Async
  // server startup; resolves to [] when unsupported or the server is missing, so
  // the editor always mounts.
  const lspExt = await lspExtension(props.path, props.cwd);
  if (!host.value) return; // leaf closed while we awaited
  const hasLsp = !Array.isArray(lspExt) || lspExt.length > 0;

  const state = EditorState.create({
    doc: content,
    extensions: [
      basicSetup,
      detectLanguage(props.path),
      lspExt,
      // Cmd/Ctrl-click → go to definition (VS Code feel). F12 already bound by
      // languageServerSupport. ponytail: only when an LSP server is active.
      ...(hasLsp ? [EditorView.domEventHandlers({
        mousedown(e, view) {
          if (!(e.metaKey || e.ctrlKey)) return false;
          const pos = view.posAtCoords({ x: e.clientX, y: e.clientY });
          if (pos == null) return false;
          view.dispatch({ selection: { anchor: pos } });
          jumpToDefinition(view);
          e.preventDefault();
          return true;
        },
      })] : []),
      oneDark,
      themeCompartment.of(editorTheme()),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) recomputeDirty();
        if (u.selectionSet || u.focusChanged) publishSelection(u.state);
      }),
      // High precedence so ⌘S beats CM defaults and never reaches the OS
      // "save page" dialog (run returns true → preventDefault).
      Prec.highest(
        keymap.of([
          { key: "Mod-s", run: () => { void save(); return true; } },
          { key: "Shift-Alt-f", run: () => { void formatDocument(); return true; } },
        ]),
      ),
      keymap.of([indentWithTab]),
    ],
  });
  view = new EditorView({ state, parent: host.value });
  if (props.initialLine) revealLine(props.initialLine);

  // Counter-zoom now and whenever the pane resizes (incl. when this tab becomes
  // visible again after being display:none, which fires a 0→real size change).
  await nextTick();
  applyCounterZoom();
  resizeObserver = new ResizeObserver(() => applyCounterZoom());
  if (host.value.parentElement) resizeObserver.observe(host.value.parentElement);
});

// Live font/UI-scale changes — reconfigure the theme (scaled font) and re-grow
// the counter-zoom box, mirroring XTerm.
watch(
  () => [ui.terminalFont, ui.terminalFontSize, ui.effectiveScale],
  () => {
    view?.dispatch({ effects: themeCompartment.reconfigure(editorTheme()) });
    applyCounterZoom();
  },
);

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  view?.destroy();
  view = null;
});

// Put the caret on a 1-based line and scroll it into the middle of the pane —
// how a ⌘P search hit lands in the file.
function revealLine(line: number) {
  if (!view) return;
  const target = Math.min(Math.max(1, line), view.state.doc.lines);
  const pos = view.state.doc.line(target).from;
  view.dispatch({ selection: { anchor: pos }, effects: EditorView.scrollIntoView(pos, { y: "center" }) });
}

defineExpose({ focus, save, isDirty, formatDocument, revealLine });
</script>

<style scoped>
/* CodeMirror mounts its own DOM under .editor-host — reach it with :deep(). */
.editor-host :deep(.cm-editor) {
  height: 100%;
}
</style>

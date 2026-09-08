// LSP bridge for the CodeMirror editor.
//
// The webview can't spawn processes, so the Go side (`LspStart`/`LspSend`/
// `LspStop` in src-wails/lsp.go) runs each language server as a child process
// and bridges its stdio JSON-RPC: server→app via `lsp-msg-{id}` events, app→server
// via `lsp_send`. Here we wrap that as a CodeMirror lsp-client Transport and hand
// out per-file editor extensions. One server is shared per (workspace root, server
// kind) and kept alive for the session.

import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import {
  LSPClient,
  languageServerSupport,
  languageServerExtensions,
  type Transport,
} from "@codemirror/lsp-client";
import type { Extension } from "@codemirror/state";
import type { Language } from "@codemirror/language";
import { javascript } from "@codemirror/lang-javascript";
import { rust } from "@codemirror/lang-rust";
import { json } from "@codemirror/lang-json";
import { html } from "@codemirror/lang-html";
import { css } from "@codemirror/lang-css";
import { python } from "@codemirror/lang-python";

// Syntax-highlight fenced code inside hover/signature tooltips (the VS Code-style
// colored signatures). Maps a markdown code-fence language name → a CM Language.
function highlightLanguage(name: string): Language | null {
  switch (name.toLowerCase()) {
    case "typescript":
    case "ts":
    case "tsx":
      return javascript({ typescript: true, jsx: true }).language;
    case "javascript":
    case "js":
    case "jsx":
      return javascript({ jsx: true }).language;
    case "rust":
    case "rs":
      return rust().language;
    case "json":
      return json().language;
    case "html":
      return html().language;
    case "css":
      return css().language;
    case "python":
    case "py":
      return python().language;
    default:
      return null;
  }
}

// LSP languageId for a file (https://microsoft.github.io/language-server-protocol).
export function lspLanguageId(path: string): string | null {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  switch (ext) {
    case "ts": return "typescript";
    case "tsx": return "typescriptreact";
    case "mts": case "cts": return "typescript";
    case "js": case "mjs": case "cjs": return "javascript";
    case "jsx": return "javascriptreact";
    case "rs": return "rust";
    case "py": case "pyi": return "python";
    case "go": return "go";
    case "c": case "h": return "c";
    case "cpp": case "cc": case "cxx": case "hpp": case "hh": case "hxx": return "cpp";
    case "lua": return "lua";
    case "sh": case "bash": return "shellscript";
    default: return null;
  }
}

interface ServerDef { key: string; name: string; args: string[]; }

// Which server handles a languageId, and how to launch it. Binary is resolved on
// the Rust side (PATH + node_modules/.bin + brew/cargo dirs).
function serverFor(langId: string): ServerDef | null {
  switch (langId) {
    case "typescript":
    case "typescriptreact":
    case "javascript":
    case "javascriptreact":
      return { key: "typescript", name: "typescript-language-server", args: ["--stdio"] };
    case "rust":
      return { key: "rust", name: "rust-analyzer", args: [] };
    // Binaries resolved on the Rust side; a missing one fails gracefully → plain
    // highlighting. Install hint per server in comments.
    case "python":
      // pip install pyright  (or npm i -g pyright)
      return { key: "python", name: "pyright-langserver", args: ["--stdio"] };
    case "go":
      // go install golang.org/x/tools/gopls@latest
      return { key: "go", name: "gopls", args: [] };
    case "c":
    case "cpp":
      // brew install llvm / xcode clangd
      return { key: "clangd", name: "clangd", args: [] };
    case "lua":
      // brew install lua-language-server
      return { key: "lua", name: "lua-language-server", args: [] };
    case "shellscript":
      // npm i -g bash-language-server
      return { key: "bash", name: "bash-language-server", args: ["start"] };
    default:
      return null;
  }
}

// Absolute path → file:// URI. encodeURI keeps "/" so the path structure stays.
export function fileUri(path: string): string {
  return "file://" + encodeURI(path);
}

let serverIdCounter = 9000; // distinct id space from PTYs

interface ClientEntry { client: LSPClient; id: number; }
const clients = new Map<string, Promise<ClientEntry | null>>();

async function makeClient(root: string, server: ServerDef): Promise<ClientEntry | null> {
  const id = ++serverIdCounter;
  const handlers = new Set<(v: string) => void>();
  // Attach the message listener BEFORE starting the server so the initialize
  // response can't slip through before we're listening.
  await listen<string>(`lsp-msg-${id}`, (ev) => {
    for (const h of handlers) h(ev.payload);
  });
  try {
    await invoke("lsp_start", { id, command: server.name, args: server.args, cwd: root });
  } catch (e) {
    console.warn(`[lsp] ${server.name} failed to start:`, e);
    return null;
  }
  const transport: Transport = {
    send(message) { invoke("lsp_send", { id, message }).catch(() => {}); },
    subscribe(handler) { handlers.add(handler); },
    unsubscribe(handler) { handlers.delete(handler); },
  };
  const client = new LSPClient({
    rootUri: fileUri(root),
    highlightLanguage,
    extensions: languageServerExtensions(),
  }).connect(transport);
  return { client, id };
}

// CodeMirror editor extension giving a file LSP features (completion, hover,
// diagnostics, go-to-def). Returns [] when the language is unsupported or the
// server can't start — the editor still works as a plain highlighter.
export async function lspExtension(path: string, root: string): Promise<Extension> {
  const langId = lspLanguageId(path);
  if (!langId) return [];
  const server = serverFor(langId);
  if (!server) return [];

  const cacheKey = `${root}::${server.key}`;
  let entryP = clients.get(cacheKey);
  if (!entryP) {
    entryP = makeClient(root, server);
    clients.set(cacheKey, entryP);
  }
  const entry = await entryP;
  if (!entry) {
    clients.delete(cacheKey); // allow a later retry
    return [];
  }
  return languageServerSupport(entry.client, fileUri(path), langId);
}

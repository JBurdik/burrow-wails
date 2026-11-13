import type { ChatMessage } from "@/lib/chatTypes";

// What a thread made and mentioned, lifted from its transcript for the
// Highlights surface: files its tools wrote or edited, and the links in its
// replies. Pure — the session owns the transcript; this only projects it.

export interface HighlightFile {
  path: string;
  op: "wrote" | "edited";
}

export interface HighlightLink {
  url: string;
  pr: boolean;
}

const WRITE_TOOLS = new Set(["Write", "NotebookEdit"]);
const EDIT_TOOLS = new Set(["Edit", "MultiEdit"]);

// Files worth opening to look at (threadbear's "review" kinds): documents,
// pages, pictures, video, data. Source edits are Changes, not Files.
const REVIEW_EXT = /\.(md|markdown|html?|pdf|png|jpe?g|gif|webp|svg|mp4|mov|webm|csv|txt|json)$/i;
export function isReviewFile(path: string): boolean {
  return REVIEW_EXT.test(path);
}

/** Files written/edited by tool calls, newest first, one row per path ("wrote" wins). */
export function highlightFiles(messages: readonly ChatMessage[]): HighlightFile[] {
  const seen = new Map<string, HighlightFile>();
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (m.role !== "tool") continue;
    const op = WRITE_TOOLS.has(m.text) ? "wrote" : EDIT_TOOLS.has(m.text) ? "edited" : null;
    if (!op) continue;
    const raw = m.toolInput?.file_path ?? m.toolInput?.notebook_path ?? m.toolInput?.path;
    if (typeof raw !== "string" || !raw) continue;
    const prev = seen.get(raw);
    if (!prev) seen.set(raw, { path: raw, op });
    else if (op === "wrote") prev.op = "wrote";
  }
  return [...seen.values()];
}

const URL_RE = /https?:\/\/[^\s<>()"'`\]]+/g;

/** http(s) links in the agent's replies, newest first, deduped; trailing punctuation trimmed. */
export function highlightLinks(messages: readonly ChatMessage[]): HighlightLink[] {
  const out: HighlightLink[] = [];
  const seen = new Set<string>();
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (m.role !== "assistant") continue;
    // A URL inside a code block is an example or a command, not a link to open.
    const prose = m.text.replace(/```[\s\S]*?(```|$)/g, " ").replace(/`[^`\n]*`/g, " ");
    for (const hit of prose.match(URL_RE) ?? []) {
      const url = hit.replace(/[.,;:!?)*_]+$/, "");
      if (seen.has(url) || url.length > 2000 || !/^https?:\/\/([^/:]+\.[^/:]+|localhost|127\.0\.0\.1)(:\d+)?(\/|$)/.test(url)) continue;
      seen.add(url);
      out.push({ url, pr: /\/(pull|merge_requests|pullrequest)\/\d+/.test(url) });
    }
  }
  return out;
}

/** A path shown relative to the thread's folder when it lives there. */
export function shortPath(path: string, cwd: string): string {
  const base = cwd.replace(/\/+$/, "");
  return base && path.startsWith(base + "/") ? path.slice(base.length + 1) : path;
}

// Resolves an href (or inline-code path) from rendered markdown to a local
// file. Agents link files as `[plan](docs/plans/x.md)`, `/abs/x.md:12` or
// `file:///abs/x.md`; left alone, WKWebView navigates the app window to them.

export interface LocalLink { path: string; line?: number; lineEnd?: number; markdown: boolean }

const SCHEME = /^[a-z][a-z0-9+.-]*:/i;
const MD = /\.(md|markdown|mdx)$/i;

/** null = not a local file reference (external URL, in-page anchor, empty). */
export function resolveLocalLink(raw: string | null | undefined, baseDir: string): LocalLink | null {
  let href = (raw ?? "").trim();
  if (!href || href.startsWith("#")) return null;
  if (/^file:/i.test(href)) href = href.replace(/^file:\/\//i, "");
  else if (SCHEME.test(href)) return null;
  try { href = decodeURIComponent(href); } catch { /* keep as-is */ }

  let line: number | undefined;
  let lineEnd: number | undefined;
  const hash = href.match(/#L?(\d+)(?:-L?(\d+))?/);
  if (hash) { line = Number(hash[1]); if (hash[2]) lineEnd = Number(hash[2]); }
  href = href.replace(/[?#].*$/, "");
  const colon = href.match(/:(\d+)(?:-(\d+)|:\d+)?$/);
  if (colon) { line = Number(colon[1]); lineEnd = colon[2] ? Number(colon[2]) : undefined; href = href.slice(0, colon.index); }
  if (line !== undefined && lineEnd !== undefined && lineEnd < line) [line, lineEnd] = [lineEnd, line];
  if (!href) return null;

  const path = joinPath(href.startsWith("/") ? "" : baseDir, href.replace(/^~(?=\/)/, ""));
  return { path, line, ...(lineEnd !== undefined && lineEnd !== line ? { lineEnd } : {}), markdown: MD.test(path) };
}

/** Inline `code` is only a link when it looks like a markdown file path. */
export function looksLikeMdPath(text: string): boolean {
  return /^[\w.~/@-]+\.(md|markdown|mdx)(:\d+)?$/i.test(text.trim());
}

function joinPath(base: string, rel: string): string {
  const out: string[] = [];
  for (const part of `${base}/${rel}`.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") out.pop();
    else out.push(part);
  }
  return `/${out.join("/")}`;
}

export function dirOf(path: string): string {
  return path.replace(/\/[^/]*$/, "") || "/";
}

// ── file:line references in prose ───────────────────────────────────────────

const REF_BODY = String.raw`(?:\.{0,2}\/|~\/)?[\w@.-]+(?:\/[\w@.-]+)*\.[A-Za-z]\w{0,7}(?:#L\d+(?:-L?\d+)?|:\d+(?:-\d+)?(?::\d+)?)`;
const REF_IN_TEXT = new RegExp(String.raw`(?<![\w/.:@#~-])(${REF_BODY})(?![\w/])`, "g");
const REF_WHOLE = new RegExp(`^${REF_BODY}$`);

/** Inline `code` that is exactly a path with a line reference (`src/a.ts:12`). */
export function looksLikeCodeRef(text: string): boolean {
  return REF_WHOLE.test(text.trim());
}

/** Wrap `path:line` / `path#Lx-Ly` mentions in rendered chat HTML in
 *  `<a data-file-ref>` (no href — nothing navigates). Skips existing links,
 *  code blocks and form controls; a bare inline `code` is wrapped whole. A
 *  line reference is mandatory, so plain filenames are never touched. */
export function linkifyFileRefs(html: string): string {
  if (!/[:#]L?\d/.test(html)) return html;
  const parts = html.match(/<[^>]*>|[^<]+/g);
  if (!parts) return html;
  const SKIP = new Set(["a", "pre", "button", "textarea", "svg", "script", "style"]);
  const skip: Record<string, number> = {};
  let skipping = 0;
  const a = (ref: string, inner: string) => `<a class="file-ref" data-file-ref="${ref}">${inner}</a>`;
  const out: string[] = [];
  for (let i = 0; i < parts.length; i++) {
    const t = parts[i];
    if (t[0] === "<") {
      const m = t.match(/^<(\/?)([a-z][a-z0-9]*)/i);
      if (m) {
        const name = m[2].toLowerCase();
        if (SKIP.has(name) && !t.endsWith("/>")) { skip[name] = (skip[name] ?? 0) + (m[1] ? -1 : 1); skipping += m[1] ? -1 : 1; }
        if (!m[1] && name === "code" && t === "<code>" && !skipping && parts[i + 2] === "</code>" && parts[i + 1][0] !== "<" && looksLikeCodeRef(parts[i + 1])) {
          out.push(a(parts[i + 1].trim(), `<code>${parts[i + 1]}</code>`));
          i += 2;
          continue;
        }
      }
      out.push(t);
    } else out.push(skipping > 0 ? t : t.replace(REF_IN_TEXT, (r) => a(r, r)));
  }
  return out.join("");
}

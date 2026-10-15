// Resolves an href (or inline-code path) from rendered markdown to a local
// file. Agents link files as `[plan](docs/plans/x.md)`, `/abs/x.md:12` or
// `file:///abs/x.md`; left alone, WKWebView navigates the app window to them.

export interface LocalLink { path: string; line?: number; markdown: boolean }

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
  const hash = href.match(/#L?(\d+)/);
  if (hash) line = Number(hash[1]);
  href = href.replace(/[?#].*$/, "");
  const colon = href.match(/:(\d+)(?::\d+)?$/);
  if (colon) { line = Number(colon[1]); href = href.slice(0, colon.index); }
  if (!href) return null;

  const path = joinPath(href.startsWith("/") ? "" : baseDir, href.replace(/^~(?=\/)/, ""));
  return { path, line, markdown: MD.test(path) };
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

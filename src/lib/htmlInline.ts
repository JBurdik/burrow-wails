// An .html file shown in the Right Panel is drawn from srcdoc: no base URL, and
// pageSrcdoc's CSP allows no network. So its local stylesheets, images and
// fonts are inlined here — stylesheets as <style>, the rest as data: URLs.
// Remote URLs are left alone (the CSP still blocks them).

export type ReadText = (path: string) => Promise<string>;
export type ReadBase64 = (path: string) => Promise<string>;

const MIME: Record<string, string> = {
  png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp",
  svg: "image/svg+xml", avif: "image/avif", ico: "image/x-icon",
  ttf: "font/ttf", otf: "font/otf", woff: "font/woff", woff2: "font/woff2",
  mp4: "video/mp4", webm: "video/webm", mp3: "audio/mpeg", wav: "audio/wav", ogg: "audio/ogg",
};

function isLocal(url: string): boolean {
  return !!url && !/^([a-z][a-z0-9+.-]*:|\/\/|#)/i.test(url);
}

/** dir + a relative url → a normalized absolute path (query/hash dropped). */
export function resolvePath(dir: string, url: string): string {
  const rel = url.replace(/[?#].*$/, "");
  const parts = (rel.startsWith("/") ? rel : `${dir}/${rel}`).split("/");
  const out: string[] = [];
  for (const p of parts) {
    if (p === "..") out.pop();
    else if (p && p !== ".") out.push(p);
  }
  return `/${out.map(decodeURIComponent).join("/")}`;
}

const dirOf = (path: string) => path.replace(/\/[^/]*$/, "");

async function dataUrl(path: string, readB64: ReadBase64): Promise<string | null> {
  const mime = MIME[path.split(".").pop()?.toLowerCase() ?? ""];
  if (!mime) return null;
  try {
    return `data:${mime};base64,${await readB64(path)}`;
  } catch {
    return null;
  }
}

async function replaceAsync(s: string, re: RegExp, fn: (...m: string[]) => Promise<string>): Promise<string> {
  const jobs: Promise<string>[] = [];
  s.replace(re, (...m) => (jobs.push(fn(...(m as string[]))), ""));
  const done = await Promise.all(jobs);
  let i = 0;
  return s.replace(re, () => done[i++]);
}

async function inlineCssUrls(css: string, dir: string, readB64: ReadBase64): Promise<string> {
  return replaceAsync(css, /url\(\s*(["']?)([^"')]+)\1\s*\)/gi, async (m, _q, url) => {
    if (!isLocal(url)) return m;
    const d = await dataUrl(resolvePath(dir, url), readB64);
    return d ? `url("${d}")` : m;
  });
}

export async function inlineLocalAssets(html: string, htmlPath: string, readText: ReadText, readB64: ReadBase64): Promise<string> {
  const dir = dirOf(htmlPath);
  // <link rel="stylesheet" href="x.css"> → <style>
  let out = await replaceAsync(html, /<link\b[^>]*>/gi, async (tag) => {
    if (!/\brel\s*=\s*["']?stylesheet/i.test(tag)) return tag;
    const href = /\bhref\s*=\s*(["'])(.*?)\1/i.exec(tag)?.[2] ?? "";
    if (!isLocal(href)) return tag;
    const path = resolvePath(dir, href);
    try {
      const css = await inlineCssUrls(await readText(path), dirOf(path), readB64);
      return `<style>${css.replace(/<\/style/gi, "<\\/style")}</style>`;
    } catch {
      return tag;
    }
  });
  // src="img.png" / poster="…" on any element
  out = await replaceAsync(out, /\b(src|poster)\s*=\s*(["'])(.*?)\2/gi, async (m, attr, q, url) => {
    if (!isLocal(url)) return m;
    const d = await dataUrl(resolvePath(dir, url), readB64);
    return d ? `${attr}=${q}${d}${q}` : m;
  });
  // url(…) in inline <style> blocks and style="" attributes
  return inlineCssUrls(out, dir, readB64);
}

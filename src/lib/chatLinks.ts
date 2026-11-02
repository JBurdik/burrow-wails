// Where a link clicked in a chat transcript opens. Like Claude Desktop, a link
// opens BESIDE the chat (the Right Panel) when the panel can show it:
//   - a pull/merge request of the workspace's own repo → the PRs surface;
//   - a dev server on this machine (localhost / 127.0.0.1 / 0.0.0.0 / [::1]) →
//     the Browser surface.
// Everything else stays external (system browser): the Browser surface is an
// <iframe>, and GitHub, GitLab and most public sites forbid framing
// (X-Frame-Options / CSP frame-ancestors), so they would only ever show a
// "blocked" placeholder there. That includes another repo's PR.

export type ChatLinkTarget =
  | { kind: "pr"; number: number; url: string }
  | { kind: "browser"; url: string };

export interface PrRef { repo: string; number: number }

const LOCAL_HOSTS = new Set(["localhost", "127.0.0.1", "0.0.0.0", "[::1]"]);

function parseHttp(href: string): URL | null {
  try {
    const u = new URL(href);
    return u.protocol === "http:" || u.protocol === "https:" ? u : null;
  } catch {
    return null;
  }
}

/** `host/owner/repo` (lowercase, no `.git`) for a git remote URL in any of
 *  the usual shapes — https, `git@host:owner/repo.git`, `ssh://git@host/…`. */
export function repoKey(remote: string): string | null {
  const s = remote.trim();
  let host: string;
  let path: string;
  const scp = s.match(/^[\w.-]+@([^:/]+):(.+)$/);
  if (scp) {
    host = scp[1];
    path = scp[2];
  } else {
    try {
      const u = new URL(s);
      host = u.hostname;
      path = u.pathname;
    } catch {
      return null;
    }
  }
  path = path.replace(/^\/+|\/+$/g, "").replace(/\.git$/i, "");
  if (!host || !path.includes("/")) return null;
  return `${host}/${path}`.toLowerCase();
}

/** A GitHub `/<owner>/<repo>/pull/<n>` or GitLab `/<group…>/<repo>/-/merge_requests/<n>` link. */
export function prRef(href: string): PrRef | null {
  const u = parseHttp(href);
  if (!u) return null;
  const m =
    u.pathname.match(/^\/([^/]+\/[^/]+)\/pull\/(\d+)(?:\/|$)/) ??
    u.pathname.match(/^\/(.+?)\/-\/merge_requests\/(\d+)(?:\/|$)/);
  if (!m) return null;
  return { repo: `${u.hostname}/${m[1]}`.toLowerCase(), number: Number(m[2]) };
}

/** The URL to frame for a link to a server on this machine, or null. `0.0.0.0`
 *  is a bind address, not one to connect to — it becomes `localhost`. */
export function localUrl(href: string): string | null {
  const u = parseHttp(href);
  if (!u || !LOCAL_HOSTS.has(u.hostname)) return null;
  if (u.hostname === "0.0.0.0") u.hostname = "localhost";
  return u.href;
}

/** The port a local URL connects to (explicit, else the scheme's default). */
export function localPort(href: string): number | null {
  const u = parseHttp(href);
  if (!u || !LOCAL_HOSTS.has(u.hostname)) return null;
  return u.port ? Number(u.port) : u.protocol === "https:" ? 443 : 80;
}

/** null = not ours: leave the click to the default (system browser). */
export function classifyChatLink(href: string, repoKeys: readonly string[]): ChatLinkTarget | null {
  const pr = prRef(href);
  if (pr) return repoKeys.includes(pr.repo) ? { kind: "pr", number: pr.number, url: href } : null;
  const local = localUrl(href);
  return local ? { kind: "browser", url: local } : null;
}

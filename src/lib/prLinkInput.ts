// Pasted pull request references (after t3code's pullRequestLinkInput.ts):
// "#123", "123" or a GitHub / GitLab / Gitea / Azure DevOps PR URL. Pure, so the
// accepted shapes are tested without a forge. Go re-parses (parsePrRef in
// src-wails/chatpr.go) — this only decides what the UI offers to act on.

export interface PrRef {
  number: number;
  /** The pasted URL, when the reference was one. */
  url?: string;
}

const URL_RE = /^https?:\/\/\S+\/(?:pull|pulls|merge_requests|pullrequest)\/(\d+)(?:[/?#]\S*)?$/i;
const NUM_RE = /^#?(\d+)$/;

/** `bare` also accepts a plain "123" (a field that only takes PRs); free text needs "#123" or a URL. */
export function parsePrRef(input: string, bare = true): PrRef | null {
  const s = input.trim();
  const url = URL_RE.exec(s);
  if (url) {
    const n = Number(url[1]);
    return n > 0 ? { number: n, url: s } : null;
  }
  const num = NUM_RE.exec(s);
  if (!num || (!bare && !s.startsWith("#"))) return null;
  const n = Number(num[1]);
  return n > 0 ? { number: n } : null;
}

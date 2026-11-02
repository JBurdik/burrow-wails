// Thread details card (ThreadDetailsCard.vue, after t3code's ThreadDetailsCard):
// which rows a given context shows. Pure, so the visibility rules are tested
// without mounting anything. A row with nothing to show is left out.

export type DetailsRow =
  | "workspace" | "openIn" | "scripts"
  | "branch" | "pr" | "commit" | "changes" | "servers"
  | "scheduled" | "lineage";

/** The "where" group sits above the divider, the git group below it. */
export const WHERE_ROWS: readonly DetailsRow[] = ["workspace", "openIn", "scripts"];

export interface DetailsContext {
  /** A chat, a terminal tab, or the workspace itself with nothing focused. */
  kind: "chat" | "tab" | "workspace";
  /** "Open in…" launches a Mac app: no use to a browser client. */
  desktop: boolean;
  /** Current branch of the workspace; "" outside a git repo. */
  branch: string;
  hasPr: boolean;
  /** Chat only: a PR linked to the thread (chatpr.go), shown with or without a branch. */
  hasLinkedPr?: boolean;
  added: number;
  removed: number;
  servers: number;
  /** Chat only: pending scheduled prompts. */
  scheduled: number;
  /** Chat only: parent + side chats + sub-agents. */
  lineage: number;
}

export function visibleRows(c: DetailsContext): DetailsRow[] {
  const chat = c.kind === "chat";
  const show: Record<DetailsRow, boolean> = {
    workspace: true,
    openIn: c.desktop,
    // Always there: its "Add script" is the way to the first one.
    scripts: true,
    branch: !!c.branch,
    // A chat always gets the row: its menu is how a PR is linked.
    pr: !!c.hasLinkedPr || (!!c.branch && (c.hasPr || chat)),
    commit: !!c.branch,
    changes: c.added + c.removed > 0,
    servers: c.servers > 0,
    scheduled: chat && c.scheduled > 0,
    lineage: chat && c.lineage > 0,
  };
  return (Object.keys(show) as DetailsRow[]).filter((r) => show[r]);
}

/** `git diff --numstat` → line totals; binary files ("-") count as nothing. */
export function sumNumstat(out: string): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const line of out.split("\n")) {
    const [a, r] = line.split("\t");
    added += parseInt(a, 10) || 0;
    removed += parseInt(r, 10) || 0;
  }
  return { added, removed };
}

// Where the chat lane sits beside the card — t3code's resolveChatCanvasLayout
// (chatCanvasLayout.ts) without the preview player. The lane stays centred on
// the whole canvas while it clears the card; only when it would crowd the card
// does it shift left, easing in so a resize never makes it jump, and it shrinks
// only once shifting is not enough. Returns the right inset to give the chat
// (its lane is `mx-auto`, so centring it in `width - inset` puts it at `left`),
// or null when the lane would fall under minChatWidth — then the card hides.
export const DETAILS_CARD_WIDTH = 288;
export const DETAILS_CARD_GAP = 8;
const DETAILS_CARD_CLEARANCE = 32;

export function detailsLaneInset(
  width: number,
  { padding = 24, maxChatWidth = 760, minChatWidth = 560 } = {},
): number | null {
  const cardLeft = width - DETAILS_CARD_GAP - DETAILS_CARD_WIDTH;
  const laneRight = cardLeft - DETAILS_CARD_CLEARANCE;
  if (laneRight - padding < minChatWidth) return null;
  const centered = Math.max(0, Math.min(maxChatWidth, width - padding * 2));
  const laneWidth = Math.max(0, Math.min(centered, laneRight - padding));
  const centerLeft = (width - laneWidth) / 2;
  const half = (width - cardLeft) / 2;
  const shift = Math.min(half, Math.max(0, half - (laneRight - centerLeft - laneWidth)));
  const left = Math.max(padding, Math.min(centerLeft - shift, laneRight - laneWidth));
  return Math.max(0, width - left * 2 - laneWidth);
}

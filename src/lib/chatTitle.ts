// Shared by AgentChat.vue (desktop) and src/mobile/store.ts: a chat starts
// life titled "Chat N" (or "Chat N (phone)" for one the phone created), gets
// upgraded to a cheap local heuristic off the first prompt the instant it is
// sent, then — fire-and-forget — to a model-written title once generate_chat_title
// answers. Two callers now, so this lives once rather than drifting into two
// slightly different heuristics.

const FILLER_PREFIX =
  /^(can you |please |i want (you )?to |how (do i|to) |what (is|are) (the |a )?|could you |would you |help me |i need (you )?to )/i;

export function smartTitle(text: string): string {
  const clean = text.replace(FILLER_PREFIX, "").replace(/\s+/g, " ").trim();
  const words = clean.split(" ");
  const slug = words.slice(0, 6).join(" ");
  const title = slug.charAt(0).toUpperCase() + slug.slice(1);
  return title.length < clean.length ? title + "…" : title;
}

// Matches both the desktop's "Chat N" default and the phone's "Chat N (phone)"
// one — either is fair game to upgrade, neither is a title a person chose.
export function isDefaultTitle(title: string): boolean {
  return /^Chat(\s+\d+)?(\s+\(phone\))?$/.test(title.trim());
}

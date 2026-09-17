// Split message text into plain runs + `@path` mention tokens. Pure and
// DOM-free so it can be tested directly; AgentChat's pillifyMentions() applies
// it per text node of already-rendered markdown.

export interface MentionPart { mention: boolean; v: string }

// Trailing punctuation a sentence puts right after a mention ("check @foo.ts.",
// "see @foo.ts)") is not part of the path — strip it so the pill doesn't carry
// a trailing dot/paren. Keeps at least one char so a lone "@." isn't stripped bare.
const TRAILING_PUNCT = /[.,!?;:)\]}'"]+$/;

/** `@path` at the start of the text or after whitespace. A bare `@` and an
 *  address-like `a@b` are not mentions. */
export function splitMentions(text: string): MentionPart[] {
  const parts: MentionPart[] = [];
  const re = /(^|\s)(@[^\s@]+)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    const start = m.index + m[1].length;
    let mention = m[2];
    const trail = mention.match(TRAILING_PUNCT)?.[0] ?? "";
    if (trail && trail.length < mention.length) mention = mention.slice(0, -trail.length);
    if (start > last) parts.push({ mention: false, v: text.slice(last, start) });
    parts.push({ mention: true, v: mention });
    last = start + mention.length;
  }
  if (last < text.length || parts.length === 0) parts.push({ mention: false, v: text.slice(last) });
  return parts;
}

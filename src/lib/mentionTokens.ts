// Split message text into plain runs + `@path` mention tokens. Pure and
// DOM-free so it can be tested directly; AgentChat's pillifyMentions() applies
// it per text node of already-rendered markdown.

export interface MentionPart { mention: boolean; v: string }

/** `@path` at the start of the text or after whitespace. A bare `@` and an
 *  address-like `a@b` are not mentions. */
export function splitMentions(text: string): MentionPart[] {
  const parts: MentionPart[] = [];
  const re = /(^|\s)(@[^\s@]+)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    const start = m.index + m[1].length;
    if (start > last) parts.push({ mention: false, v: text.slice(last, start) });
    parts.push({ mention: true, v: m[2] });
    last = start + m[2].length;
  }
  if (last < text.length || parts.length === 0) parts.push({ mention: false, v: text.slice(last) });
  return parts;
}

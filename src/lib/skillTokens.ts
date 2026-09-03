// Split composer text into plain runs + `/skill-name` tokens (word-start only,
// matched against the known skill list) so the composer can render skills as
// pills via a highlight backdrop behind the textarea. The backdrop must keep
// the textarea's exact text metrics, so pills may only add color — never
// padding, margin, or font-weight.

export interface TextPart { pill: boolean; v: string }

export function splitSkillTokens(text: string, names: string[]): TextPart[] {
  if (!text || names.length === 0) return [{ pill: false, v: text }];
  const set = new Set(names);
  const parts: TextPart[] = [];
  const re = /(^|\s)\/([^\s/]+)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    if (!set.has(m[2])) continue;
    const start = m.index + m[1].length;
    if (start > last) parts.push({ pill: false, v: text.slice(last, start) });
    parts.push({ pill: true, v: `/${m[2]}` });
    last = start + m[2].length + 1;
  }
  if (last < text.length || parts.length === 0) parts.push({ pill: false, v: text.slice(last) });
  return parts;
}

export function hasSkillToken(text: string, names: string[]): boolean {
  return splitSkillTokens(text, names).some((p) => p.pill);
}

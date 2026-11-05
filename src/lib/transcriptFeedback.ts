export interface TranscriptQuote {
  quote: string;
  before: string;
  after: string;
}

export interface TranscriptNote extends TranscriptQuote {
  id: string;
  messageId: number;
  source: string;
  body: string;
  delivery?: string;
  resolvedAt?: number;
}

export interface FeedbackDelivery {
  text: string;
  sentAt: number;
}

export function composeTranscriptFeedback(notes: TranscriptNote[], request = ""): string {
  if (!notes.length) return request;
  const feedback = notes.map((note, i) => [
    `### Comment ${i + 1}`,
    "Quoted passage from your earlier response:",
    note.quote.split("\n").map((line) => `> ${line}`).join("\n"),
    note.before ? `Context before: ${note.before}` : "",
    note.after ? `Context after: ${note.after}` : "",
    `Feedback: ${note.body}`,
  ].filter(Boolean).join("\n\n")).join("\n\n");
  return `Please address these review comments on your earlier responses. Answer each comment in order and make any requested changes.\n\n${feedback}${request.trim() ? `\n\nAdditional request:\n${request}` : ""}`;
}

/** Read text offsets from the rendered response, preserving repeated quotes. */
export function selectedTranscriptQuote(container: HTMLElement, selection: Selection | null): TranscriptQuote | null {
  if (!selection || selection.isCollapsed || !selection.rangeCount) return null;
  const range = selection.getRangeAt(0);
  if (!container.contains(range.startContainer) || !container.contains(range.endContainer)) return null;
  const quote = range.toString();
  if (!quote.trim()) return null;
  const before = range.cloneRange();
  before.selectNodeContents(container);
  before.setEnd(range.startContainer, range.startOffset);
  const after = range.cloneRange();
  after.selectNodeContents(container);
  after.setStart(range.endContainer, range.endOffset);
  return { quote, before: before.toString().slice(-100), after: after.toString().slice(0, 100) };
}

/** Insert `quote` as a markdown blockquote into `text` at `caret`, on its own
 *  lines. `ask` leaves an empty line after it for the question. Returns the new
 *  text and where the caret goes (right after the quote / on the empty line). */
export function insertQuote(text: string, caret: number, quote: string, ask = false): { text: string; caret: number } {
  const at = Math.max(0, Math.min(caret, text.length));
  const before = text.slice(0, at);
  const after = text.slice(at);
  const block = quote.trim().split("\n").map((line) => line.trim() ? `> ${line}` : ">").join("\n");
  const head = before && !before.endsWith("\n") ? `${before}\n` : before;
  const insert = `${block}\n${ask ? "\n" : ""}`;
  const tail = after && !after.startsWith("\n") ? `\n${after}` : after;
  return { text: head + insert + tail, caret: head.length + insert.length };
}

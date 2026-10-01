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

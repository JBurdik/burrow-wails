/**
 * Live terminal handles, keyed by pty id.
 *
 * The control API's `tab_output` verb has to read a running agent's scrollback,
 * which only the xterm instance has. Walking the component tree from a plain
 * module isn't possible, so each XTerm registers itself here for its lifetime.
 * Deliberately not a Pinia store: this holds imperative handles, not state, and
 * nothing should render off it.
 */
export interface TermHandle {
  /** Last `lines` non-empty rows of the buffer, oldest first. */
  readOutput(lines: number): string;
}

const handles = new Map<number, TermHandle>();

export function registerTerm(ptyId: number, handle: TermHandle) {
  handles.set(ptyId, handle);
}

export function unregisterTerm(ptyId: number) {
  handles.delete(ptyId);
}

export function readTermOutput(ptyId: number, lines: number): string | undefined {
  return handles.get(ptyId)?.readOutput(lines);
}

// The model behind the composer's contenteditable input.
//
// The composer's model is a plain string — `"fix /agent-browser now"` — exactly
// what gets sent to the agent. What the user sees is that string with the known
// `/token`s replaced by atomic inline chips, which a <textarea> cannot do: the
// old implementation painted pills onto a transparent-text backdrop and so
// could only ever recolor the literal characters, never change their width or
// add an icon.
//
// A chip is `contenteditable="false"`, so the browser treats it as one
// indivisible character-like thing: one Backspace removes it whole, arrows step
// over it, and the caret can sit before or after it but never inside.
//
// Everything here works on a flat list of node descriptions rather than real
// DOM nodes, so the offset arithmetic — the part that actually breaks — is
// testable without a DOM environment. ComposerTextInput.vue maps its real child
// nodes onto this shape.

/** What a `/token` or `@token` in the model turned out to be. */
export type ChipKind = "skill" | "command" | "file";

/** One child of the editable root: a run of text, a named chip, or a pasted-text chip. */
export type FlatNode =
  | { kind: "text"; text: string }
  | { kind: ChipKind; name: string }
  | { kind: "paste"; text: string };

export function isChip(n: FlatNode): n is { kind: ChipKind; name: string } | { kind: "paste"; text: string } {
  return n.kind !== "text";
}

// Pasted blocks are wrapped in these invisible marker characters right inside
// the model string, so `tokenize()` can find the block again after any full
// re-render (undo, an external model write, the skills list arriving) without
// needing side-state that would drift out of sync with the string. They are
// not typable, so they never collide with real content.
const PASTE_START = "⁣";
const PASTE_END = "⁤";

export function wrapPaste(text: string): string {
  return `${PASTE_START}${text}${PASTE_END}`;
}

/**
 * Ph "Clipboard Text" bold path data — shared by the composer's own paste chip
 * (`ComposerTextInput.vue`) and the transcript's read-only one (`PasteChip.vue`),
 * so the two chips that mean "a pasted block collapsed here" stay visually
 * identical rather than drifting into two icons over time.
 */
export const PASTE_CHIP_ICON_PATH =
  "M196,32H164.62a44,44,0,0,0-73.24,0H60A20,20,0,0,0,40,52V216a20,20,0,0,0,20,20H196a20,20,0,0,0,20-20V52A20,20,0,0,0,196,32Zm-4,180H64V56H84v8a12,12,0,0,0,12,12h64a12,12,0,0,0,12-12V56h20ZM88,132a12,12,0,0,1,12-12h56a12,12,0,0,1,0,24H100A12,12,0,0,1,88,132Zm0,40a12,12,0,0,1,12-12h56a12,12,0,0,1,0,24H100A12,12,0,0,1,88,172Z";

/** How many lines a pasted block needs before it collapses into a chip. */
export const PASTE_COLLAPSE_LINES = 4;

/** Undo `wrapPaste`, for the text actually sent to the agent. */
export function stripPasteMarkers(text: string): string {
  return text.replaceAll(PASTE_START, "").replaceAll(PASTE_END, "");
}

/** Whether `text` still carries at least one `wrapPaste` block. */
export function hasPasteMarkers(text: string): boolean {
  return text.includes(PASTE_START);
}

function pasteMarkerRe(): RegExp {
  // A fresh RegExp per call: a module-level `g` regex keeps `lastIndex` across
  // calls, and a caller that re-enters mid-scan (this is used by both
  // `tokenize()` and the transcript renderer) would silently skip matches.
  return new RegExp(`${PASTE_START}([\\s\\S]*?)${PASTE_END}`, "g");
}

/** One run of a marked-up string: either plain text or a pasted block's raw text. */
export type PasteSegment = { paste: false; text: string } | { paste: true; text: string };

/**
 * Split model text that may still carry `wrapPaste` markers into plain-text
 * runs and pasted-block runs, without needing the skill/command/file tables
 * `tokenize()` requires. Used to render a sent message's own pasted block as
 * a collapsed chip in the transcript, the same way the composer collapses it
 * while still being typed.
 */
export function splitPasteSegments(text: string): PasteSegment[] {
  if (!text || !hasPasteMarkers(text)) return text ? [{ paste: false, text }] : [];
  const out: PasteSegment[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  const re = pasteMarkerRe();
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push({ paste: false, text: text.slice(last, m.index) });
    out.push({ paste: true, text: m[1] });
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push({ paste: false, text: text.slice(last) });
  return out;
}

/**
 * Split the model into plain runs and `/token`/`@token` chips, matched against
 * the known names. A token only counts at a word start, so a path like
 * `src/foo` is never mistaken for one, and an unknown `/whatever` or `@whatever`
 * stays plain text — which is the point: only things that will actually
 * resolve get to look resolved.
 *
 * Skills win over commands on a name collision: the command list is a handful
 * of built-ins, a skill is something the user installed on purpose.
 */
export function tokenize(
  text: string,
  known: { skills?: readonly string[]; commands?: readonly string[]; files?: readonly string[] },
): FlatNode[] {
  const skills = new Set(known.skills ?? []);
  const commands = new Set(known.commands ?? []);
  const files = new Set(known.files ?? []);
  if (!text) return [];

  interface Match { start: number; end: number; kind: ChipKind | "paste"; name: string }
  const matches: Match[] = [];
  let m: RegExpExecArray | null;

  const pasteRe = pasteMarkerRe();
  while ((m = pasteRe.exec(text)) !== null) {
    matches.push({ start: m.index, end: m.index + m[0].length, kind: "paste", name: m[1] });
  }

  if (skills.size > 0 || commands.size > 0) {
    // The leading group is start-of-string or the whitespace before the token —
    // it belongs to the preceding text run, not to the chip.
    const slashRe = /(^|\s)\/([^\s/]+)/g;
    while ((m = slashRe.exec(text)) !== null) {
      const name = m[2];
      const kind: ChipKind | null = skills.has(name) ? "skill" : commands.has(name) ? "command" : null;
      if (!kind) continue;
      const start = m.index + m[1].length;
      matches.push({ start, end: start + name.length + 1, kind, name }); // + the "/"
    }
  }
  if (files.size > 0) {
    // A file path can contain "/", unlike a skill or command name, so its body
    // only excludes whitespace.
    const atRe = /(^|\s)@([^\s]+)/g;
    while ((m = atRe.exec(text)) !== null) {
      const name = m[2];
      if (!files.has(name)) continue;
      const start = m.index + m[1].length;
      matches.push({ start, end: start + name.length + 1, kind: "file", name }); // + the "@"
    }
  }
  matches.sort((a, b) => a.start - b.start);

  const out: FlatNode[] = [];
  let last = 0;
  for (const match of matches) {
    if (match.start < last) continue; // overlapping match, first one wins
    if (match.start > last) out.push({ kind: "text", text: text.slice(last, match.start) });
    out.push(match.kind === "paste" ? { kind: "paste", text: match.name } : { kind: match.kind, name: match.name });
    last = match.end;
  }
  if (last < text.length) out.push({ kind: "text", text: text.slice(last) });
  return out;
}

/** The trigger character a chip's name is prefixed with in the model. */
function triggerChar(kind: ChipKind): "/" | "@" {
  return kind === "file" ? "@" : "/";
}

/** How many characters of the MODEL this node accounts for. */
export function nodeLength(node: FlatNode): number {
  if (node.kind === "paste") return node.text.length + 2; // + the two invisible markers
  return isChip(node) ? node.name.length + 1 : node.text.length; // + the trigger char
}

/** The plain-text model the host's v-model sees. */
export function serializeNodes(nodes: readonly FlatNode[]): string {
  let out = "";
  for (const n of nodes) {
    out += n.kind === "paste" ? wrapPaste(n.text) : isChip(n) ? `${triggerChar(n.kind)}${n.name}` : n.text;
  }
  return out;
}

/** Where a model offset lands in the node list. */
export interface NodePosition {
  /** Index into the node list. -1 when the list is empty. */
  index: number;
  /**
   * Offset within that node. For a chip this is only ever 0 (caret before it)
   * or its full length (caret after it) — a chip has no inside.
   */
  offset: number;
}

/**
 * Model offset → node position. Offsets past the end clamp to the end, which is
 * what a caret restored after an edit that shortened the text should do.
 */
export function locateOffset(nodes: readonly FlatNode[], offset: number): NodePosition {
  if (nodes.length === 0) return { index: -1, offset: 0 };
  const target = Math.max(0, offset);
  let acc = 0;
  for (let i = 0; i < nodes.length; i++) {
    const len = nodeLength(nodes[i]);
    // `<=` so an offset sitting exactly on a boundary lands in the EARLIER
    // node, at its end — the same place a textarea would put it, and the only
    // choice that keeps a caret typed right after a chip from jumping ahead of
    // whatever text follows.
    if (target <= acc + len) {
      const within = target - acc;
      if (isChip(nodes[i])) {
        // Snap to the nearer edge: there is no position inside a chip.
        return { index: i, offset: within > len / 2 ? len : 0 };
      }
      return { index: i, offset: within };
    }
    acc += len;
  }
  const last = nodes.length - 1;
  return { index: last, offset: nodeLength(nodes[last]) };
}

/** Node position → model offset. The inverse of locateOffset. */
export function flatOffset(nodes: readonly FlatNode[], index: number, offset: number): number {
  let acc = 0;
  for (let i = 0; i < Math.min(index, nodes.length); i++) acc += nodeLength(nodes[i]);
  return acc + offset;
}

/**
 * The ordered chips a node list holds. Comparing this against the same list
 * derived from the model is how the editor decides whether the DOM needs
 * rebuilding — re-rendering on every keystroke would fight the caret and break
 * IME composition, so it only happens when the *chips* actually changed.
 *
 * Kind is part of it: a name flipping from command to skill (a skill installed
 * mid-session) is a different chip and needs a rebuild.
 */
export function chipSignature(nodes: readonly FlatNode[]): string {
  return nodes.filter(isChip).map((n) => `${n.kind}:${n.kind === "paste" ? n.text : n.name}`).join(" ");
}

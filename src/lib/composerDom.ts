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

/** What a `/token` in the model turned out to be. */
export type ChipKind = "skill" | "command";

/** One child of the editable root: a run of text, or a chip. */
export type FlatNode =
  | { kind: "text"; text: string }
  | { kind: ChipKind; name: string };

export function isChip(n: FlatNode): n is { kind: ChipKind; name: string } {
  return n.kind !== "text";
}

/**
 * Split the model into plain runs and `/token` chips, matched against the known
 * names. A token only counts at a word start, so a path like `src/foo` is never
 * mistaken for one, and an unknown `/whatever` stays plain text — which is the
 * point: only things that will actually resolve get to look resolved.
 *
 * Skills win over commands on a name collision: the command list is a handful
 * of built-ins, a skill is something the user installed on purpose.
 */
export function tokenize(
  text: string,
  known: { skills?: readonly string[]; commands?: readonly string[] },
): FlatNode[] {
  const skills = new Set(known.skills ?? []);
  const commands = new Set(known.commands ?? []);
  if (!text) return [];
  if (skills.size === 0 && commands.size === 0) return [{ kind: "text", text }];

  const out: FlatNode[] = [];
  // The leading group is start-of-string or the whitespace before the token —
  // it belongs to the preceding text run, not to the chip.
  const re = /(^|\s)\/([^\s/]+)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    const name = m[2];
    const kind: ChipKind | null = skills.has(name) ? "skill" : commands.has(name) ? "command" : null;
    if (!kind) continue;
    const start = m.index + m[1].length;
    if (start > last) out.push({ kind: "text", text: text.slice(last, start) });
    out.push({ kind, name });
    last = start + name.length + 1; // + the "/"
  }
  if (last < text.length) out.push({ kind: "text", text: text.slice(last) });
  return out;
}

/** How many characters of the MODEL this node accounts for. */
export function nodeLength(node: FlatNode): number {
  return isChip(node) ? node.name.length + 1 : node.text.length; // + the "/"
}

/** The plain-text model the host's v-model sees. */
export function serializeNodes(nodes: readonly FlatNode[]): string {
  let out = "";
  for (const n of nodes) out += isChip(n) ? `/${n.name}` : n.text;
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
  return nodes.filter(isChip).map((n) => `${n.kind}:${n.name}`).join(" ");
}

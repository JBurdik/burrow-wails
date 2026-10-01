import type { FileDiffMetadata } from "@pierre/diffs";

export type ReviewReceipts = Record<string, string>;

/** A content identity, independent of the renderer's cache key and theme. */
export function diffRevision(file: FileDiffMetadata): string {
  const text = JSON.stringify([file.name, file.prevName, file.type, file.mode, file.prevMode,
    file.hunks, file.deletionLines, file.additionLines]);
  let a = 0x811c9dc5;
  let b = 0x9e3779b9;
  for (let i = 0; i < text.length; i++) {
    a = Math.imul(a ^ text.charCodeAt(i), 0x01000193);
    b = Math.imul(b ^ text.charCodeAt(i), 0x85ebca6b);
  }
  return `${text.length}:${a >>> 0}:${b >>> 0}`;
}

/** New edits clear a receipt; returning to an earlier diff cannot revive it. */
export function currentReceipts(receipts: ReviewReceipts, revisions: ReviewReceipts): ReviewReceipts {
  return Object.fromEntries(Object.entries(receipts).filter(([name, revision]) => revisions[name] === revision));
}

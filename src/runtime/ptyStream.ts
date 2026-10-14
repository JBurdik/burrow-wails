/**
 * Exactly-once PTY output for one terminal view, across a dropped /v2/ws.
 *
 * `pty-data-<id>` is not in the shell ring (bus.go's notRingable), so bytes
 * emitted while the socket was down never arrive live. Each chunk carries the
 * offset of its first byte in the PTY's whole output (the daemon numbers them);
 * this remembers how far the view has rendered and, on a reconnect or a hole
 * in the offsets, asks `pty_since` for exactly the missing bytes. Overlap
 * between that catch-up and live chunks is trimmed, so nothing renders twice.
 *
 * Must not import xterm (boundary.test.ts) — the view hands in `write`.
 */

/** The pty-data-<id> payload. `off` is null from a daemon that predates it. */
export interface PtyChunk {
  off: number | null;
  data: number[];
}

export interface PtyStream {
  /** Feed a live pty-data-<id> payload. */
  push(chunk: PtyChunk): void;
  /** Fetch whatever was missed since the last rendered byte (call on reconnect). */
  catchUp(): Promise<void>;
  /** Forget the position: the PTY behind this id exited / was replaced. */
  reset(): void;
}

export function createPtyStream(opts: {
  write: (bytes: number[]) => void;
  fetchSince: (since: number) => Promise<PtyChunk>;
  /** The daemon's ring no longer held `bytes` of the gap — they are gone. */
  onLost?: (bytes: number) => void;
}): PtyStream {
  // Offset of the next byte to render; null = no position yet (first chunk
  // sets it — attaching mid-stream starts wherever the stream is).
  let next: number | null = null;
  let catching = false;
  let held: PtyChunk[] = [];

  function apply(c: PtyChunk) {
    if (c.off == null) {
      opts.write(c.data); // old daemon: no offsets, nothing to dedupe on
      return;
    }
    if (next == null) next = c.off;
    if (c.off > next) {
      // A hole: something was lost in between. Hold this chunk behind a catch-up.
      held.push(c);
      void catchUp();
      return;
    }
    const skip = next - c.off;
    if (skip >= c.data.length) return; // already rendered
    opts.write(skip ? c.data.slice(skip) : c.data);
    next = c.off + c.data.length;
  }

  function push(c: PtyChunk) {
    if (catching) held.push(c);
    else apply(c);
  }

  async function catchUp() {
    if (catching || next == null) return;
    catching = true;
    try {
      const got = await opts.fetchSince(next);
      if (got.off != null && next != null) {
        if (got.off > next) {
          opts.onLost?.(got.off - next);
          next = got.off;
        } else if (got.off < next && got.data.length === 0) {
          next = got.off; // the PTY behind this id is behind us: a new one
        }
        apply(got);
      }
    } catch {
      // No catch-up possible (old daemon, socket down again): accept the hole
      // — what every reconnect did before — and restart from the next chunk.
      next = null;
    }
    catching = false;
    const h = held;
    held = [];
    for (const c of h) push(c);
  }

  return {
    push,
    catchUp,
    reset() {
      next = null;
      held = [];
    },
  };
}

/**
 * Jittered exponential backoff for reconnecting a transport.
 *
 * The jitter matters more than the exponent: without it, every client that
 * lost its connection to the same event (a laptop waking from sleep, the app
 * restarting) retries in lockstep. `jitter` is injectable so tests are
 * deterministic.
 */
export interface Backoff {
  /** Delay in ms for the next attempt, and count this attempt. */
  next(): number;
  /** Call after a successful connect. */
  reset(): void;
  attempts(): number;
}

export function createBackoff(opts: {
  baseMs?: number;
  maxMs?: number;
  /** Returns -1..1. Defaults to Math.random() mapped into that range. */
  jitter?: () => number;
} = {}): Backoff {
  const baseMs = opts.baseMs ?? 250;
  const maxMs = opts.maxMs ?? 10_000;
  const jitter = opts.jitter ?? (() => Math.random() * 2 - 1);

  let attempt = 0;

  return {
    next() {
      const step = Math.min(baseMs * 2 ** attempt, maxMs);
      attempt++;
      // Jitter is +/-20% of the step, clamped so it can neither exceed the
      // cap nor collapse to zero.
      const spread = step * 0.2 * jitter();
      return Math.max(1, Math.min(maxMs, Math.round(step + spread)));
    },
    reset() {
      attempt = 0;
    },
    attempts() {
      return attempt;
    },
  };
}

/**
 * Runs `fn` until it resolves, waiting `delaysMs[i]` after the i-th failure, and
 * rethrows the last error once the delays are used up. For reads that must not stay
 * empty because the socket was still reopening (wake from sleep, core restart).
 */
export async function retrying<T>(fn: () => Promise<T>, delaysMs: number[] = [1000, 2000, 4000, 8000, 15000, 30000]): Promise<T> {
  for (let i = 0; ; i++) {
    try {
      return await fn();
    } catch (e) {
      if (i >= delaysMs.length) throw e;
      await new Promise((r) => setTimeout(r, delaysMs[i]));
    }
  }
}

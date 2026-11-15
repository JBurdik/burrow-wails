/**
 * What the connection indicator shows, from the transport's raw up/down
 * (`Transport.onState`). Pure: timers are injected so the debounce is testable.
 *
 * A drop shows nothing for `SHOW_AFTER_MS` (a wake() probe or a quick
 * reconnect should not flash), then "reconnecting", then "offline" once the
 * socket has been down for `OFFLINE_AFTER_MS`. Coming back up clears at once.
 */
export type LinkState = "up" | "reconnecting" | "offline";

export const SHOW_AFTER_MS = 1500;
export const OFFLINE_AFTER_MS = 3000;

export interface Timers {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

const realTimers: Timers = {
  set: (fn, ms) => setTimeout(fn, ms),
  clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
};

export function createLinkStatus(emit: (s: LinkState) => void, timers: Timers = realTimers) {
  let pending: unknown[] = [];
  let down = false;
  return {
    set(up: boolean) {
      if (!up && down) return; // already counting
      for (const h of pending) timers.clear(h);
      pending = [];
      down = !up;
      if (up) {
        emit("up");
        return;
      }
      pending = [
        timers.set(() => emit("reconnecting"), SHOW_AFTER_MS),
        timers.set(() => emit("offline"), OFFLINE_AFTER_MS),
      ];
    },
  };
}

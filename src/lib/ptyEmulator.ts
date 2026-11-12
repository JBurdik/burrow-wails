import { ref } from "vue";
import { invoke } from "@tauri-apps/api/core";

/**
 * The daemon keeps a terminal emulator per PTY (plan 010, `vt` build tag), so a
 * terminal view can be unmounted while hidden and rebuilt from `pty_attach`
 * when shown. Only beta/dev daemons have one until Phase 3 ships to stable; a
 * stable app reads false and keeps every terminal mounted, as before.
 *
 * Asked once per window; false until the answer arrives (mounted = the safe side).
 */
export const ptyEmulator = ref(false);

let asked = false;
export function loadPtyEmulator(): void {
  if (asked) return;
  asked = true;
  // The window can boot before its core is up (a dev / cold start): a rejected
  // call is retried for a minute, then the answer stays "keep them mounted".
  const ask = (left: number) =>
    invoke<boolean>("pty_emulator")
      .then((v) => (ptyEmulator.value = v === true))
      .catch(() => { if (left > 0) setTimeout(() => ask(left - 1), 2000); });
  void ask(30);
}

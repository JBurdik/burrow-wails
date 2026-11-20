# 012 — wterm (+ libghostty core) as the xterm.js replacement: spike

Status: **proposed** (2026-11-12). Nothing built. Follows 010 (server-side VT) and 011 (ghostty-web research,
addendum = how wterm was found). Decide replace / don't replace only on the spike's numbers.

## Why

Since 010 the daemon runs Ghostty's emulator per PTY and xterm.js is only the view of what is on screen. The
remaining mismatches come from **two different emulators**: Ghostty in the daemon, xterm in the window (bold-bright
colour, a lone invalid UTF-8 byte, soft-wrap flags; 010 "Phase 0 results"). [vercel-labs/wterm](https://github.com/vercel-labs/wterm)
with [`@wterm/ghostty`](https://wterm.dev/ghostty) would put the same engine on both sides.

What it would bring (from its README, 2026-11-12; **not verified in Burrow**):

| | |
|---|---|
| Same emulator as the daemon | `@wterm/ghostty` = libghostty core → our `pty_attach` snapshot renders 1:1, if the Ghostty versions are compatible |
| DOM text rows | native selection, copy, ⌘F find, screen readers — fewer of `XTerm.vue`'s selection/zoom workarounds |
| Many terminals | optional `renderer: "webgl"` paints from **one shared** WebGL2 context (xterm: one per terminal, browsers cap them); `WorkerCore` parses off the main thread (an agent flood can't stall the UI); only a bounded window of history is in the DOM |
| Vue | official `@wterm/vue` (Vue 3) |
| Agents | `term.readText()`, optional WebMCP tools (read screen, type, wait for output) |
| Maintenance | active: v0.5.10 (2026-11-08), daily commits, ★3.7k, Apache-2.0 |

Costs / risks: it is a **"Labs experiment"** at 0.5 (API churn, Vercel's commitment unknown); xterm is proven
(VS Code); a port of `XTerm.vue` (~900 lines) + its callers is ~3–5 days (estimate).

## Spike (≈ 1 day, throwaway branch `spike/wterm`, no product code merged)

One dev-only toggle (`uiPrefs.wterm`, or a URL flag) makes `XTerm.vue`'s slot render a wterm view instead; same PTY
plumbing (`create_pty`, `pty-data-<id>`, `write_pty`, `resize_pty`, `pty_attach`). Measure:

1. **Ghostty version** `@wterm/ghostty` pins vs. our daemon's `f54340ad`; write a `pty_attach` snapshot (shell,
   `git log --color`, vim, top, claude — reuse the Phase 0 captures) and diff against feeding raw bytes. Goal: identical.
2. **Input:** Czech QWERTZ in WKWebView — dead keys (ˇ, ´), diacritics, ⌥ combos, IME; bracketed paste; mouse in
   a TUI (vim, Claude); Kitty keyboard if Claude negotiates it.
3. **Throughput:** `yes | head -c 1G` and the SGR flood with the DOM renderer and with `renderer: "webgl"` +
   `WorkerCore`; UI responsiveness (typing echo latency in another tab) and CPU. Compare with xterm + WebGL.
4. **Many terminals:** 20 tabs open with the 010 unmount on/off; webview memory before/after (the measurement
   010 Phase 3 still owes for xterm too).
5. **Feature map of `XTerm.vue`:** themes/palette + live switch, font/zoom (`applyCounterZoom`), phone mirror
   (fixed grid, CSS scale), links (⌘-click → `shellOpen`), ⌘K clear, ⌘⇧R repaint, title (OSC 0/2), OSC 7 cwd,
   image paste, `readOutput`, `frozen`. For each: 1:1 / adapter needed / missing.

## Exit / decision

- **Go** if: snapshot identical (1), Czech input works (2), flood ≥ xterm and the UI stays responsive (3), no
  missing item in (5) without a reasonable adapter. Then a plan 013 for the port, behind the same beta-only gating
  as 010's unmount (stable keeps xterm until the port has had its beta time).
- **No-go** otherwise; record the blocking item here and re-check on wterm's next minor release.

Not in the spike: removing xterm, the phone shell, the RP terminals, `frozen` / float relay cleanup.

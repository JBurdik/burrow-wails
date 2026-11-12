# 011 — ghostty-web as an xterm.js replacement (research)

Status: research only, 2026-11-12. Primary sources: the GitHub repo (cloned at `640db46`), npm registry, `gh` issue/PR lists.
Verdict up front: **not now.** Revisit when a release after 0.4.0 ships with the open fixes listed in §4 merged, or when Ghostty ships its own WASM distribution.

## 1. What it is

| | |
|---|---|
| Repo | https://github.com/coder/ghostty-web — owner **Coder** (they built it for their agent desktop app Mux) |
| License | MIT ([LICENSE](https://github.com/coder/ghostty-web/blob/main/LICENSE)) |
| npm | [`ghostty-web`](https://www.npmjs.com/package/ghostty-web): `latest` = **0.4.0, published 2025-12-09** (11 months ago); `next` = `0.4.0-next.21.g640db46` (2026-10-28) |
| Stars | 3,059 (2026-11-12) |
| Created | 2025-11-10 |
| Ghostty base | submodule `ghostty` @ [`5714ed07`](https://github.com/ghostty-org/ghostty/commit/5714ed07a1012573261b7b7e3ed2add9c1504496) (2025-12-01) plus a 65 KB / ~1,500-line [`patches/ghostty-wasm-api.patch`](https://github.com/coder/ghostty-web/blob/main/patches/ghostty-wasm-api.patch). Our daemon's ghostty `f54340ad` is **5,172 commits ahead** of that (GitHub compare). So the two are not the same emulator version. |

Activity over the last 3 months (2026-08-12 → 11-12): **1 commit on main** (`640db46`, a CI Node bump). The last code changes landed 2026-06-26. One release ever since Dec 2025: none (`gh release list` shows only v0.4.0). There are **~40 open PRs** (the oldest from April), including a Ghostty 1.3 upgrade (#162), IME placement (#190, #202) and kitty graphics (#197). None has been merged. Issue [#156](https://github.com/coder/ghostty-web/issues/156) "what's the intention for the repo going forward?" and [#137](https://github.com/coder/ghostty-web/issues/137) "publish a new npm release" have no maintainer reply. **In practice the project is stalled.**

## 2. API compatibility against what Burrow actually calls

Our real surface was grepped from `src/components/XTerm.vue`, `src/lib/termRenderer.ts` and `src/themes.ts`. Most of the APIs the brief expected are **not used by us**: `onBinary`, `onResize`, `registerOscHandler`/`parser`, `input()`, the selection API and `registerLinkProvider`. The OSC 9999 spawn parser is a regex over the data (`XTerm.vue:157`). The table checks what we do call against `lib/terminal.ts` and `lib/interfaces.ts` @ `640db46`.

| Burrow uses | ghostty-web | Note |
|---|---|---|
| `new Terminal({theme, fontFamily, fontSize, cursorBlink, cursorStyle:"bar", allowTransparency, scrollback})` | ✅ all in `ITerminalOptions` | `scrollback` reaches WASM as `scrollbackLimit`, which is interpreted as **bytes, not lines** ([#140](https://github.com/coder/ghostty-web/issues/140)) |
| `lineHeight: 1.4` | ❌ | open feature request [#216](https://github.com/coder/ghostty-web/issues/216) |
| `allowProposedApi` | ❌ (not needed) | — |
| `options.theme =` / `options.fontSize =` after open | ⚠️ | the setter is a proxy, but its own warning says "theme changes after open() are not yet fully supported" ([#125](https://github.com/coder/ghostty-web/issues/125)); our live theme / bg-opacity switch needs this |
| ANSI palette from `theme` | ❌ | the WASM side resolves colours to a hard-coded Tomorrow Night palette; the theme only covers bg, cursor and selection ([#121](https://github.com/coder/ghostty-web/issues/121)). Every Burrow theme would be wrong. |
| `open`, `write(data, cb)`, `writeln`, `resize`, `reset`, `clear`, `focus`, `dispose`, `scrollToBottom`, `loadAddon`, `onData`, `onTitleChange`, `cols`/`rows`, `textarea`, `element` | ✅ exist | `write('')` throws ([#199](https://github.com/coder/ghostty-web/issues/199)); titles are not detected in `Uint8Array` writes (fix PR #213 unmerged), and our pty stream writes bytes |
| `refresh(start, end)` | ❌ | no such method (its render loop repaints every frame anyway, [#208](https://github.com/coder/ghostty-web/issues/208)) |
| `attachCustomKeyEventHandler` | ⚠️ **inverted contract** | returning `true` *drops* the key in ghostty-web. Our handler (`XTerm.vue:649`) would swallow all typing ([#192](https://github.com/coder/ghostty-web/issues/192)) |
| `buffer.active.{baseY, viewportY, cursorY, getLine().translateToString(true)}` | ✅ in `lib/buffer.ts` | bugs: empty cells come back as `''`, which collapses cursor-positioned text ([#205](https://github.com/coder/ghostty-web/issues/205)); graphemes are truncated ([#200](https://github.com/coder/ghostty-web/issues/200)). `XTerm.vue:92`/`:817` read text this way. |
| `FitAddon` | ✅ `lib/addons/fit.ts` | the only shipped addon |
| `WebglAddon` / `CanvasAddon` (`termRenderer.ts`) | n/a | the renderer is built in, Canvas2D only |
| `WebLinksAddon` (⌘-click → `shellOpen`) | ⚠️ built in | `lib/providers/url-regex-provider.ts` + OSC 8 provider. A custom open handler (`linkHandler`) exists only in unmerged PR #194, and OSC 8 isn't in the npm 0.4.0 wasm (#137 comment) |
| `SerializeAddon` (float-bubble snapshot, `XTerm.vue:579`) | ❌ | no equivalent. We'd have to use the daemon's `pty_attach` VT snapshot instead (arguably better anyway) |

## 3. Renderer, size, input features

- **Renderer:** a Canvas 2D context (`renderer.ts:143`, `getContext('2d', {alpha:true})`) that draws cell by cell with `fillText`. There is **no WebGL**: [#155](https://github.com/coder/ghostty-web/issues/155) asks for one and is open. Known render issues: it redraws every animation frame even when idle or hidden (#208), it repaints and resizes every frame at a fractional DPR (#198), cells are 1 px too wide (#207), and box-drawing / Powerline glyphs show seams (#126, #184). For agent-output floods this is a regression from our WebGL addon. That is an inference from the code; I ran no benchmark.
- **Benchmarks:** [`bench/versus.ts`](https://github.com/coder/ghostty-web/blob/main/bench/versus.ts) (mitata, happy-dom) measures **parse/write throughput only** against `@xterm/xterm`. No numbers are published in the README or the repo. Unverified.
- **Bundle (npm 0.4.0, `npm pack --dry-run`):** `ghostty-vt.wasm` 423 KB. `dist/ghostty-web.js` is 682 KB because the wasm is also inlined as a `data:` URI ([#188](https://github.com/coder/ghostty-web/issues/188)). Unpacked total 2.2 MB. For comparison, xterm 6 + 4 addons is roughly 500–600 KB of JS (unverified, not measured here).
- **IME / composition:** listeners exist (`input-handler.ts:291`), but the file header still says "Does not handle IME/composition events (CJK input) – to be added later". Hangul IME is broken ([#119](https://github.com/coder/ghostty-web/issues/119)), and the composition is drawn in the wrong place (fixes #190/#202 unmerged). **Czech diacritics are untested.** Dead-key input (`ˇ`+`c`, the Czech QWERTZ number row) goes through composition/`input` events on macOS WebKit, so this needs a manual test before any adoption (unverified).
- **Mouse reporting:** yes on main ([#106](https://github.com/coder/ghostty-web/pull/106), SGR 1006). It is **not in npm 0.4.0**: it landed 2026-01-07.
- **Bracketed paste:** yes (`input-handler.ts`, fix #99 is in 0.4.0). Paste-sanitizing for CVE-2026-26982 is unmerged (PR #193).
- **Alt/Meta:** Alt+key sends no ESC prefix ([#203](https://github.com/coder/ghostty-web/issues/203)), so readline/tmux meta bindings break.
- **Accessibility:** only an `aria-label` on the hidden textarea. No screen-reader buffer ([#187](https://github.com/coder/ghostty-web/issues/187)). xterm has `screenReaderMode`.
- **Kitty keyboard:** the Ghostty key encoder has `setKittyFlags` (`ghostty.ts:183`), but nothing in `lib/` calls it, so the app's `CSI > u` push isn't wired (based on reading the code; unverified at runtime). **Kitty graphics:** PR #197 only, unmerged.

## 4. Known bugs that would hit Burrow specifically

1. [#192](https://github.com/coder/ghostty-web/issues/192): the inverted key-handler contract. The keyboard would be dead on day one.
2. [#121](https://github.com/coder/ghostty-web/issues/121): theme ANSI colours are ignored. All 16-colour output would use Tomorrow Night colours.
3. [#141](https://github.com/coder/ghostty-web/issues/141) / [#210](https://github.com/coder/ghostty-web/issues/210): WASM memory corruption after freeing a terminal (graphemes, a different width). We create and dispose many XTerm instances (tabs, splits, popouts, phone).
4. [#189](https://github.com/coder/ghostty-web/issues/189): one throw in `render()` kills the render loop permanently.
5. [#215](https://github.com/coder/ghostty-web/issues/215): alt-screen bugs after RIS. We call `reset()` on re-attach.
6. [#208](https://github.com/coder/ghostty-web/issues/208): a rAF loop per terminal even when hidden. We keep every opened workspace's terminals mounted but hidden.
7. [#100](https://github.com/coder/ghostty-web/issues/100): new instances steal focus. [#127](https://github.com/coder/ghostty-web/issues/127): forced scroll-to-bottom on write.
8. [#140](https://github.com/coder/ghostty-web/issues/140), [#205](https://github.com/coder/ghostty-web/issues/205), [#199](https://github.com/coder/ghostty-web/issues/199), [#203](https://github.com/coder/ghostty-web/issues/203): see above.
9. The CSP/`data:` wasm fetch (#188) is probably fine under the Wails asset scheme and our Vite build (unverified).

## 5. Recommendation

**Never in its current form. Revisit "later" when both of these hold:** (a) a release after 0.4.0 includes the Ghostty 1.3 bump, the #121, #192 and #141 fixes and IME, or (b) upstream Ghostty publishes its own libghostty WASM + JS package, which the ghostty-web README says it will move to. Today it is a stalled fork pinned to a Dec-2025 Ghostty with a large patch. It is Canvas2D only, it is missing lineHeight, theme palettes, serialize and refresh, and it has open memory-safety bugs. Switching would trade xterm's minor inaccuracies (one known: bold-bright) for many larger regressions.

**If we ever migrate, these files change:**
- `src/components/XTerm.vue` (920 lines): the import, constructor options, the inverted key handler, `refresh` calls, the serialize path (`:579`, replace it with a daemon snapshot), the web-links callback, async `await init()` before the first `new Terminal`, and the zoom/selection workaround (`:48–56`), which needs re-checking against its own `getCanvasOffset`.
- `src/lib/termRenderer.ts`: delete it (no WebGL or Canvas addons).
- `src/themes.ts`: the `ITheme` type import. Palette handling depends on #121.
- `package.json` (drop 6 `@xterm/*`, add `ghostty-web`) and `vite.config.ts` (wasm asset handling).
- Re-check other components that touch the terminal via `termRegistry.ts` / `Terminal.vue` / `ThreadFindBar.vue` (search over the buffer) / `PhoneKeys.vue` (phone input) / `RightPanel.vue`. Mostly via XTerm's exposed methods, but this hasn't been audited line by line.
- `src/runtime/ptyStream.ts`: the attach/`reset()` path, plus tests (`ptyStream.test.ts` mocks a term).

**Effort:** about 3–5 days for a feature-parity port against **upstream main + cherry-picked PRs** (we would have to vendor and build it: Zig + Bun, the patch, the submodule). Then ongoing maintenance of a fork. Using npm 0.4.0 is a non-starter: it lacks mouse tracking, OSC 8 and the resize-crash fix. This is a rough estimate, not scoped task by task.

**Would our server snapshot render identically?** Partly, and not because of a shared emulator.
- The bold-bright mismatch (plan 010 §results, diff 1) is an xterm *renderer* policy: `drawBoldTextInBrightColors` brightens only `30–37`, not `38;5;N`. ghostty-web resolves colours to RGB inside WASM and has no bold-brightening option at all (`renderer.ts:608` only switches the font to bold). So that particular diff would disappear in the sense that neither path brightens. That matches native Ghostty's default (`bold-is-bright = false`).
- **We can get the same fix in xterm today, with one line:** `drawBoldTextInBrightColors: false` in the `Terminal` options (`XTerm.vue:353`). Live output and the snapshot then render the same. The trade-off is that bold basic colours stop brightening on live output too. That matches Ghostty and most modern terminals.
- Everything else is not guaranteed identical: ghostty-web's Ghostty (Dec 2025 + patch) is 5k commits behind our daemon's `f54340ad`, and its palette bug (#121) would recolour the snapshot just like live output.

## Sources
Repo/README/source: https://github.com/coder/ghostty-web (tree @ `640db46`) · npm: https://www.npmjs.com/package/ghostty-web (`npm view ghostty-web time`) · issues/PRs: https://github.com/coder/ghostty-web/issues · Ghostty compare: https://github.com/ghostty-org/ghostty/compare/5714ed07...f54340ad

## Addendum (2026-11-12): official Ghostty WASM and newer libraries

**Ghostty upstream has no web terminal package, but it officially supports building the VT core as WASM.** This was verified in the source at the daemon's pin `f54340ad` (2026-10-21):
- `PACKAGING.md` → "WebAssembly (libghostty-vt)": `zig build -Demit-lib-vt -Dtarget=wasm32-freestanding -Doptimize=ReleaseSmall` produces `zig-out/bin/ghostty-vt.wasm`. SIMD is on by default. `-Dvt-features` drops unused areas; an interactive terminal needs `-all,+render-state,+input-encode,+selection,+color`, which is roughly half the size. ReleaseSmall is about 0.8 MB, and `wasm-opt` cuts about 10% more.
- Examples `example/wasm-vt`, `wasm-sgr` and `wasm-key-encode` cover only parse, format and encode. **There is no renderer, input handling or npm package from Ghostty itself.** "Official" here means the same engine and version as our daemon. Rendering, input and IME are the embedder's job.

**Libraries built on top of it** (activity checked with `gh api` on 2026-11-12):

| Project | Status | Notes |
|---|---|---|
| [vercel-labs/wterm](https://github.com/vercel-labs/wterm) | **active**: v0.5.10 released 2026-11-08, commits daily, ★3.7k, Apache-2.0, 21 open issues. Marked a "Labs experiment". | Renders rows as DOM text, so native selection, find and screen readers work. Optional `renderer: "webgl"` and an off-main-thread `WorkerCore`. **There is a Vue 3 package `@wterm/vue`.** It has its own ~49 KB Zig core, or [`@wterm/ghostty`](https://wterm.dev/ghostty) uses libghostty for full VT support (Kitty images, OSC 133, reflow). `term.readText()`. Not checked yet: its own Ghostty version, IME / Czech dead keys, flood performance, an xterm-like API for our `XTerm.vue` |
| [wiedymi/restty](https://github.com/wiedymi/restty) | slower: v0.3.0 released 2026-09-05, ★420, MIT | libghostty-vt + WebGPU, falling back to WebGL2 |
| coder/ghostty-web | stalled (see above) | — |

**Updated verdict:** xterm.js stays for now. **wterm + `@wterm/ghostty` is the first candidate worth a spike.** It would give the same emulator in the window as in the daemon, an official Vue binding, and active maintenance. A 1-day spike should check: which Ghostty commit `@wterm/ghostty` pins (so it matches our snapshot format), IME with Czech QWERTZ in WKWebView, a 1 GB flood with the WebGL renderer, a theme/palette API, and how much of `XTerm.vue` maps 1:1.

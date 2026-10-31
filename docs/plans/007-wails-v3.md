# 007 — Wails v2 → v3

Source: https://v3.wails.io/migration/v2-to-v3/ (raw: `wailsapp/wails` master,
`docs/mpress/content/migration/v2-to-v3.md`). v3.0.0 = stable for desktop
(macOS/Win/Linux); mobile experimental. v2 still gets critical fixes.

## Why it's cheap here

The desktop's data path already runs over `/v2/ws` (`remoteapi.go` reflects on
`App`, not on Wails bindings). Wails surface left on this branch:

| Area | Where | v2 API | v3 replacement |
|------|-------|--------|----------------|
| Bootstrap | `main.go` | `wails.Run(&options.App{…})` | `application.New` + `app.Window.NewWithOptions` + `app.Run()` |
| Assets | `main.go` | `assetserver.Options{Assets}` | `application.AssetOptions{Handler: application.AssetFileServerFS(assets)}` |
| Lifecycle | `main.go`, `app.go:192` | `OnStartup(ctx)`, `OnDomReady`, `OnBeforeClose` | service `ServiceStartup`/`ServiceShutdown`, window events (`WindowClosing` hook, `events.Common.WindowRuntimeReady`?) |
| Bindings | `Bind: []any{app}` — **226** exported methods bound, ~12 used from JS | `Services: []application.Service{…}` |
| Events (Go→JS) | 14× `runtime.EventsEmit` (menu-*, lsp-msg-*, float-*, extension-task:*, update:*) | `app.Event.Emit` |
| Window | `windowstate.go` (size/pos/maximise/fullscreen/minimised), `updater.go` Quit | `window.SetSize/SetPosition/Maximise/IsMaximised/…`, `app.Quit()` |
| Dialogs | `dialog.go` (dir, file, files + filters) | `app.Dialog.OpenFileWithOptions(…).PromptForSingleSelection()/PromptForMultipleSelection()` |
| Menu | `main.go` app/edit/window/help menus | `app.NewMenu()`, roles, `AddSubmenu`, `Add().OnClick` |
| Native menu hack | `appmenu_darwin.{go,m}` (Check for Updates… in app menu) | likely deletable — v3 menu can build the app menu itself |
| File drop | `runtime.OnFileDrop` + `DragAndDrop{EnableFileDrop, DisableWebViewDrop}` | window option `EnableFileDrop` + `WindowFilesDropped` event (verify x/y available) |
| Mac chrome | `mac.TitleBarHiddenInset()`, `BackgroundColour` | `MacWindow{TitleBar: application.MacTitleBarHiddenInset, Backdrop…}` |
| Frontend | `src/lib/wailsCompat/*` imports `src-wails/frontend/wailsjs/{go/main/App,runtime/runtime}` | `@wailsio/runtime` (`Events`, `Browser.OpenURL`, `Window.SetTitle`) + generated `bindings/…` |
| Build | `wails dev` / `wails build -s`, `wails.json` | `wails3 dev` / `wails3 build` (Taskfile), `build/config.yml` |
| Dev channel | `channel_dev.go` relies on `wails dev` adding the `dev` build tag | must re-wire tag/ldflag in Taskfile |

Untouched: everything behind `/v2/ws`, `remoteAllowed`, phase store, chat runtime,
headless build (`main_headless.go` — no Wails import), daemon, MCP sidecar.

## Decisions (grilled 2026-10-31)

1. **Why**: stay off maintenance-only v2; multi-window (floats, git panel) is the payoff — **separate PR** after this one.
2. **Branch** `wails-v3` off `beta`; ship via `just build-beta`, stable after ~1 week of daily use.
3. **Bindings**: thin `DesktopService` (~11 methods), not all 226 `App` methods.
4. **Build**: Taskfile owns compile + bundle + sign/notarize; **justfile stays** as the entry
   point (`just dev/build/build-beta/release` call `wails3 task …`), keeps bump, `latest.json`,
   `gh release`. `burrow-release` skill unchanged.
5. **App menu**: delete `appmenu_darwin.*` if the v3 menu can add an item to the app menu; else keep.
6. **Version** source of truth → `build/config.yml` (`info.version`); `just bump` rewrites it.
   **Bundle id must stay `com.agenticide.wails`** (beta override too) or the first v3 update
   lands as a different app.
7. **Bindings** generated into `src/lib/wailsCompat/bindings/`, committed.
8. **Window close = hide**, backend keeps running (phone, schedules); ⌘Q quits. Dock icon
   stays, click reopens. **System tray**: Open Burrow, Quit + count of chats/tabs in
   `waiting_*` (from `PhaseStore.onChange`, same source as push). No chat list in tray.
9. **Abort criteria** for the spike (~½ day): no drop coordinates or broken
   `MacTitleBarHiddenInset` ⇒ postpone, stay on v2.
10. **Order**: same branch, separate commits — 1:1 migration + smoke first, then
    hide-on-close + tray; beta release only with both.

## Steps

1. **Spike (branch `wails-v3`)** — `go get github.com/wailsapp/wails/v3@v3.0.0`, `wails3 init`
   a throwaway project to copy `build/` + `Taskfile.yml` shape. Verify: macOS 27 webview,
   `MacTitleBarHiddenInset`, file drop with coordinates, `go 1.25` OK.
2. **Desktop service** — new `desktop.go`: `DesktopService{app *App, wa *application.App}`
   exposing only what JS calls through Wails today: `LocalEndpoint`, `PickDirectory`,
   `PickFile`, `PickFiles`, `CheckUpdate`, `InstallUpdate`, `RelaunchApp`, `GetAppVersion`,
   `SendFloatSnapshot`, `NotifyFloatGrid`, `OpenGitPanelWindow`. Stop binding all of `App`
   (226 methods). `LocalEndpoint` stays the only in-process authorization.
3. **Replace `a.ctx`** — `App` gets `wa *application.App` + `win *application.WebviewWindow`;
   `startup(ctx)` keeps its own context (headless calls it the same). Swap the 14 emits,
   dialogs, window state, Quit.
4. **main.go** — `application.New` + one main window; menu via v3 API; try dropping
   `appmenu_darwin.*`.
5. **Close/shutdown** — `OnBeforeClose` → window-closing hook (save state) +
   `ServiceShutdown`/`OnShutdown` (cleanupOnShutdown). Check ⌘Q path runs both.
6. **Frontend compat** — `wailsCompat/*` from `wailsjs/...` → `@wailsio/runtime` + generated
   bindings; `event.ts` desktop-only prefixes unchanged; `commandSurface.test.ts` green.
7. **Build/release** — `justfile`: `dev`, `build`, `build-beta` (ldflags `buildChannel`),
   daemon + mcp copy into bundle, sign/notarize unchanged paths; `wails.json` version source
   → wherever v3 keeps `productVersion` (`build/config.yml`) + `just bump`. Info.plist /
   entitlements / bundle id beta override.
8. **Tests** — `events_test.go` allowlist (`EventsEmit(` → `Event.Emit(`),
   `TestRemoteSurfaceIsExhaustive` (new service methods), `go test ./...`, `pnpm test`.
9. **Manual smoke** — start, ⌘Q, window state restore, file drop on composer, dialogs,
   Help menu, Check for Updates…, auto-update install + relaunch (codesign team check),
   beta bundle side by side, phone pairing unaffected.
10. **Docs** — CLAUDE.md header/commands, `docs/architecture.md`, `docs/context.html`.
11. **Hide-on-close + tray** (separate commit) — `ApplicationShouldTerminateAfterLastWindowClosed:
    false`, window close hides, Dock reopen shows; `app.SystemTray` with waiting count;
    `cleanupOnShutdown` moves to real quit only. Smoke: close window → phone still works,
    schedule fires, tray count updates, ⌘Q cleans up.

## Later (not in this migration)

- Float bubbles / git panel window as real v3 windows (`webviewWindow.ts`, `window.ts` stubs).
- Tray list of waiting chats, Dock badge.

## Risks

- Updater swaps the running `.app`; bundle layout from `wails3 build` (Taskfile) must
  match what `updater.go` + `just pack` expect.
- `CLAUDE_CODE_PLUGIN_DIRS`/`BURROW_*` env unaffected, but `wails3 dev` runs a Vite dev
  server differently (frontenddir is `..`, not `./frontend`).
- File-drop x/y hit-testing in AgentChat.vue depends on coordinates v3 may not supply.

# BurrowSwift — brief for parallel agents

Experimental native SwiftUI client for Burrow (macOS 14+, Swift 6.4 toolchain, `swift-tools-version:5.9`).
It talks to the running Burrow core over the same `/v2/ws` surface as the Vue desktop and the phone —
**no Go backend changes**. If a feature needs a command that does not exist, report it; don't add it.

Six agents work in this directory **at the same time**. Read this whole file before editing anything.

## How the client works

- `Burrow.swift` — the transport + shared state (`@MainActor final class Burrow: ObservableObject`).
  - `try await b.call("<command>", [args])` → the reply's `result` as `Any?` (`JSON = [String: Any]`, helpers `.i("k")` / `.s("k")`).
  - `b.subscribe("<event-prefix>") { name, payload in … }` — every event (ringed `shell` frames and the
    unnumbered `pty-data-*` / chat channels). Use this instead of editing `onEvent`.
  - `b.snapshot` = last raw `shell_snapshot` (incl. `tabs` per workspace id, `phases`).
  - `b.selectedChat` / `b.selectedTab` (pty id) — detail pane selection; setting one clears the other.
  - `b.chats` (top-level, unarchived), `b.allChats` (every row), `b.workspaces[id]`, `b.projects`, `b.phases`,
    `b.state(id)`, `b.inFlight(id)`, `b.settled(chat)`, `b.rootWorkspace(wsID)`.
- The command surface and argument names are in `../src-wails/remoteapi.go` (`remoteAllowed`). Argument
  names there are the JSON keys. The Vue frontend (`../src/`) shows how each feature uses them — read
  the matching Vue component / store before building its Swift twin. `../CLAUDE.md` explains every subsystem.
- Probe the live core from the shell (the app is running on this Mac):
  `node tools/probe.mjs <command> '<json args>'` and `node tools/probe.mjs --listen <event-prefix> [seconds]`.
  Use it to learn real payload shapes before you write a parser. **Read-only commands only** unless the
  effect is harmless (don't send messages into the user's real chats, don't delete/archive their rows,
  don't push / merge / commit in their repos).

## Look and feel (keep it consistent)

- Colours: only `Theme.*` (Burrow's tokens: `base`, `sidebar`, `terminal`, `panel`, `hover`, `selected`,
  `border`, `text`, `text2`, `muted`, `accent`, `accentDim`, `green`, `yellow`, `red`, `Theme.status(state)`).
- Fonts: `.font(.ui(delta))` for prose, `.font(.brand(delta, weight:))` (0xProto) for chrome, labels, code.
  Base size follows ⌘+/⌘− — never hardcode point sizes except sidebar row titles.
- Radii: cards 10–16, composer 20, small chips 6–7. No capsule/pill-shaped buttons, no cream backgrounds.
- `Pet(state:size:)` is the mascot for empty / waiting / working states — use it, sparingly.
- Small, boring code. Mark deliberate shortcuts `// ponytail: <ceiling>, <upgrade path>`.

## Ownership — wave 2 (edit ONLY your files)

The work list is `PARITY.md` → "Prioritized work packages" (+ the per-area tables above it for detail).
Wave 1 (terminal, attention, threads, composer, git, shell) is done; its files now belong to these owners:

| Agent | Package(s) | Owns (may create more files with its prefix) |
|---|---|---|
| rp | 1 + 12 | `RightPanel.swift`, `RP*.swift`, `SubagentDetail.swift` |
| editor | 13 | `Editor*.swift`, `FilesView` (move it out of RightPanel.swift into `EditorFiles.swift` after rp agrees — or just build `EditorFiles.swift` and tell rp to switch) |
| transcript | 2 | `Transcript*.swift` |
| composer | 3 | `Composer*.swift` |
| attention | 4 | `Attention*.swift`, `Requests*.swift`; in `Burrow.swift` only `loadRequests` / `answer` |
| threads | 5 + 6 | `Sidebar*.swift`, `Thread*.swift`, `NewThread*.swift`, `Search*.swift`, `Schedule*.swift` |
| gitdiff | 7 | `DiffSurface*.swift`, `GitDiff*.swift`, `GitCheckpoint*.swift`, `Review*.swift` |
| gitwindow | 10 | `GitWindow*.swift`, `GitGraph*.swift`, `GitBranch*.swift`; `GitPanel.swift`, `GitLogic.swift`, `PRPanel.swift` |
| terminal | 8 | `Terminal*.swift`, `Package.swift`, `Sources/CGhosttyVT/`, `make-app.sh` |
| shell | 9 + 11 + 14 | `App.swift`, `Theme.swift`, `Projects*.swift`, `Spotlight*.swift`, `Settings*.swift`, `Remote*.swift`, `Window*.swift`, `ShellControl*.swift` |

- `Burrow.swift` is shared: new API goes in an `extension Burrow` **in your own file**; stored state in your own
  `ObservableObject`. Edit `Burrow.swift` itself only for an unavoidable one-line hook (re-read first, small Edit).
- A one-line insertion in someone else's file to place your view is fine — say so in your report.
- Theme: keep every existing `Theme` member name.

### Seams (already in the code — keep the signatures)

- Sub-agents and side chats NEVER take the main pane: `openSubagent(id)` (SubagentDetail.swift) shows them in the
  RP; `ChatFeed` is a per-chat transcript + requests store for a chat that is not `b.selectedChat`;
  `.environment(\.requestChatID, id)` makes `RequestCard`s answer that chat.
- `openRightPanelSurface(_ name:)` (RightPanel.swift) and `RPOpenRequests.shared` (Editor.swift: `.file`, `.surface`)
  are how the rest of the app asks the RP to show something. The **rp** agent makes the RP observe
  `RPOpenRequests` and open the right surface in the right pane.
- `openFileInRP(path, line:)` — clickable file links, Files tab, Spotlight. **editor** fills `EditorSurfaceSlot(path:)`.
- `DiffSurfaceSlot(chat:)` — **gitdiff** fills it; **rp** lists it as a surface ("diff").
- Terminal: `SidebarTabsSlot`, `TerminalDetailSlot`, `TerminalPanelSlot` (terminal). Attention: `AttentionSlot`,
  `PtyPermissionBanner`, `SecretRequestBanner`. Right-panel surfaces for terminals stay `TerminalPanelSlot`.
- In-app toasts: **attention** provides `toast(_ text:, kind:)` (global func) — others may call it once it exists;
  until then keep using `b.status`.

## Building

- Build with your own scratch path so builds don't fight over one lock:
  `swift build --scratch-path .build-<agent>` (from `swift-client/`).
- Everyone's files compile together. Keep yours compiling at every save (stub first, then fill in).
  If the build fails in a file you don't own, don't touch it — wait a minute and retry; if it persists
  for 10 minutes, mention it in your report.
- Don't run `make-app.sh`, don't kill or relaunch the running BurrowSwift app, don't commit. The
  coordinator rebuilds and relaunches when everyone is done. You cannot see the GUI: verify logic with
  the probe and with small `assert`-based checks where it matters (parsers, diff/hunk math).

## Report

End with: what works, what you skipped and why, which commands/events you used, any one-line edits you
made in files you don't own, and anything you could not verify.

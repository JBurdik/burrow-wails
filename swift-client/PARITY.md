# BurrowSwift ↔ Vue desktop — feature parity audit

Snapshot 2026-11-12. Vue = `src/` (desktop window, not `src/components/phone/`). Swift = `swift-client/Sources/BurrowSwift/` (~8.4k lines).
Status: **DONE** (behaviour present) · **PARTIAL** (exists, gaps listed) · **MISSING**. Size: S ≤ ½ day, M ≈ 1–2 days, L ≥ 3 days.
Each Swift claim was checked against the Swift source (calls / views), not file names.

## Sidebar

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Thread feed grouped by project (worktrees fold into repo), order = `sort_order` | `Sidebar.vue`, `lib/sidebarGroups.ts` | DONE | `Sidebar.swift` groups by `rootWorkspace`, sorts by `sort_order` | – |
| Needs-you section (waiting_*) | `Sidebar.vue` | DONE | | – |
| Pinned threads (`sidebarPinnedChats` config, followed live) | `lib/pinnedChats.ts` | DONE | `ThreadStore.togglePin`; no drag-reorder of pins | S |
| Working / Snoozed / Settled docked shelf, collapsible, `burrow.sidebar.workingExpanded`-style persistence | `Sidebar.vue` (shelf), `claudeChats.settledFor` | PARTIAL | Settled + Archived collapsible sections exist (capped 100); no separate **Snoozed** shelf, no **Working** shelf, expansion not persisted | S |
| "Delete all settled threads" bulk action | `Sidebar.vue` | MISSING | button on Settled header → confirm → `delete_chat` per row, keep pinned | S |
| Archived shelf | `Sidebar.vue` | DONE | | – |
| Collapsible project groups (persisted `COLLAPSED_KEY`) | `Sidebar.vue` | MISSING | DisclosureGroup per project header + UserDefaults set | S |
| Drag project header to reorder (`projectOrder`) | `lib/projectOrder.ts` | MISSING | `.draggable`/`.dropDestination` on headers → `set_workspace_order` (Swift only has ↑/↓ in Settings) | M |
| Pinned / archived **projects** | `lib/pinnedWorkspaces.ts`, `archivedWorkspaces.ts` | MISSING | project context menu: Pin / Archive (config keys) | S |
| Project header context menu: New chat, New terminal, New worktree…, Rename project…, Close project, Delete… | `Sidebar.vue` L420-480 | MISSING | header `.contextMenu`; new worktree dialog (branch + base) → `create_worktree`; rename → `rename_workspace`; delete with worktree-on-disk warning | M |
| "+ New thread in this project" per header | `Sidebar.vue` | MISSING | button preselecting project on NewThreadView | S |
| Row: status dot, unread bold, PR glyph, snooze clock, sleeping moon, subtitle (detail / branch / recap / relative time) | `Sidebar.vue` | DONE | `ChatRow` | – |
| Row hover card (thread details after delay: path, branch moved, session id, recap) | `Sidebar.vue` L360 | MISSING | `.popover` on hover with delay; reuse ThreadDetails view | M |
| Row context menu: Rename, Regenerate title, Pin, Settle/Unsettle, Mark unread, Snooze presets, Side chat, Review changes, Move/Leave worktree, Link/Unlink PR, Archive, Delete, Open in window | `Sidebar.vue`, `ThreadMenu` | PARTIAL | `ThreadMenu.swift` has all except **Regenerate title** (`generate_chat_title`) and **Open in window** (exists only as ⌘⌥O menu) | S |
| Undo window for delete (toast "Undo", delayed real delete) | `lib/pendingDeletes.ts` | MISSING | hide row, `delete_chat` after N s unless Undo | S |
| Terminal tabs listed with chats in one list (icon distinguishes) | `Sidebar.vue`, `terminalTabs` store | PARTIAL | `SidebarTabsSlot` lists tabs under the project; no status dot per tab from `phase-pty:`, no tab drag-reorder, settled flag for tabs (`settledTabs.ts`) missing | M |
| Tab menu: Rename tab…, Close tab, Open in window, Regenerate title | `Sidebar.vue` | PARTIAL | Rename + Close only | S |
| Search field: title filter + FTS "In chats" hits | `Sidebar.vue` | DONE | `.searchable` + `search_chats` | – |
| Scripts popover (Run a script per workspace) | `Sidebar.vue`, `stores/scripts.ts` | MISSING | list scripts from config, run into RP terminal (`serve`-like) | M |
| Bottom bar: settings, dashboard, git manager, open folder, connection dot | `Sidebar.vue` L304 | PARTIAL | connection dot + Scheduled button only; no Open folder…, Git manager, Dashboard buttons | S |
| Update banners + toasts docked in sidebar | `UpdateBanner.vue`, `ProviderUpdateBanner.vue`, `ToastStack.vue` | MISSING | see App shell (toasts) | M |
| ⌘B toggle sidebar | `lib/keymap.ts` | PARTIAL | NavigationSplitView has the system toggle; no ⌘B binding | S |

## Thread / transcript

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Live transcript (`chat-messages-changed-*` splice), paged older history on scroll-up | `AgentChat.vue`, `chatSession.ts` | DONE | `Burrow.splice`, `loadOlder` | – |
| Markdown (headings, lists, tables, code w/ copy) | `AgentChat.vue` (markdown-it) | PARTIAL | own `mdBlocks` parser: no syntax highlighting, no nested lists/blockquote fidelity, no inline HTML | M |
| Tool rows, grouped "Ran N commands" pill, expandable detail, Edit diff card | `AgentChat.vue` | DONE | `ToolRun`/`ToolRow`/`ToolDetail`/`EditDiff` | – |
| Settled turn folds thinking/tools behind one row; turn duration | `lib/chatTurns.ts` | DONE | `transcriptRows` (TranscriptLogic) + `TurnFoldRow`; turnMs else last row `at` − prompt `at` | – |
| "Files this turn edited" diffstat card → opens file diff | `AgentChat.vue` L214, `chatTurns.ts` | DONE | `ChangedFilesCard`: expand → hunk lines, name → `openFileInRP`, arrow → RP `diff` surface | – |
| Thinking blocks | `AgentChat.vue` | DONE | DisclosureGroup | – |
| Sub-agent spawn marker / surface marker notes (open RP surface) | `AgentChat.vue` L324-356 | PARTIAL | `NoteRow` "Open" sets `selectedChat = sub` (navigates away) instead of opening the sub-agent detail **in the RP**; surface notes call `openRightPanelSurface` (DONE) | S |
| Permission log / system info markers | `AgentChat.vue` L357-372 | PARTIAL | rendered as plain notes; no styled permission log | S |
| Images in transcript + lightbox | `ImageLightbox.vue` | DONE | in-app lightbox panel (`openImageLightbox`, TranscriptModals) | – |
| burrow-visuals: mermaid, visual (stat tiles, charts, tables, images, page cards), questions form | `chatBlocks.ts`, `DiagramModal.vue`, `HtmlPageModal.vue` | PARTIAL | page card → in-app no-network modal (`openHtmlPage`), mermaid zoom modal (`openDiagram`); mermaid still from CDN | S |
| Copy message button | `AgentChat.vue` | DONE | hover on user + assistant (`MessageView`) | – |
| Edit (put user message back in composer) | `AgentChat.vue` | DONE | hover pencil → `ComposerStore.setDraft` (+ data-URL images) | – |
| Rewind / Fork from here (Claude only) | `AgentChat.vue`, `chatrewind.go` | DONE | context menu on user msg | – |
| Working line (spinner verb + clock), stalled → restart | `AgentChat.vue` L418 | PARTIAL | Pet + phase detail; no turn clock, no stall detection → `chat_restart` | S |
| Usage-limit banner (Resume automatically / Dismiss) | `AgentChat.vue` L425 | DONE | `UsageLimitBanner` | – |
| Prompt minimap rail (dash per prompt, jump) | `AgentChat.vue` L436 | MISSING | thin rail with scrollTo per user msg | M |
| Jump to latest button when scrolled up; autoscroll pause | `AgentChat.vue` L457 | DONE | `TranscriptList`: wheel-up unpins, ≤64pt from bottom re-pins, "Jump to latest" button | – |
| "New" divider on reopen (visit watermark) | `lib/visitWatermark.ts` | DONE | `visitWatermark` + `NewDivider`, receipt captured when the chat opens | – |
| Find in chat ⌘F (count, next/prev, unfold) | `ThreadFindBar.vue`, `lib/threadFind.ts` | DONE | `TranscriptFind.swift`: ⌘F, Return/⌘G next, ⇧⌘G prev, Esc; highlights all, unfolds turn, scrolls | – |
| Transcript comments: select text → comment / quote into composer / ask (⌘⌥M) | `review/TranscriptComments.vue`, `lib/transcriptFeedback.ts` | PARTIAL | hover "quote" puts the WHOLE reply as `> quote` in the draft; no selection-based comments | M |
| Branch-changed banner (session branch vs current) | `AgentChat.vue` L472 | MISSING | compare chat.branch vs `run_git branch --show-current` | S |
| Where-this-chat-works line (cwd/branch under composer) | `AgentChat.vue` L809 | PARTIAL | header shows project + branch chips | – |
| Thread header: status, title, project, branch, PR chip, actions menu | `TitleBar.vue` / `AgentChat.vue` | DONE | `ThreadHeader` | – |
| Recap button ("Summarize where this chat is at") | `AgentChat.vue` | MISSING | button → recap command (check `remoteAllowed` name) + show `chats.recap` | S |
| Thread details card (Open in…, scripts, branch switch, PR link, commit/push, changes, running servers/bg tasks, scheduled prompts, lineage) | `ThreadDetailsCard.vue`, `lib/threadDetails.ts` | MISSING | popover from header; biggest single missing card | L |
| Sleeping indicator (slept_at) in composer | `AgentChat.vue` | PARTIAL | moon in sidebar only | S |
| ⌘K clear chat (`chat_clear`), `/clear` → clear | `AgentChat.vue` L2848 | MISSING | `/clear` is sent as text via `chat_send`; wire to `chat_clear` + ⌘K | S |
| Context report (`/context` breakdown) | `lib/contextReport.ts` | PARTIAL | `ContextRing` shows %; no category breakdown popover | S |
| Plan usage indicator (5h window etc.) | `PlanUsageIndicator.vue`, `lib/planUsage.ts` | MISSING | `plan_usage` → small meter in composer footer | S |
| Agent plan rail (editable steps) | `AgentPlanRail.vue`, `stores/agentPlans.ts` | MISSING | | M |
| Agent timeline / inspector | `AgentTimeline.vue`, `AgentInspector.vue` | MISSING | low value; skip unless asked | M |
| Clickable file links in transcript → open in RP / editor (`chatLinks`, `localLink`) | `lib/chatLinks.ts`, `localLink.ts` | DONE | `inline()` links `path:line` + file code spans (`burrow-file:`), md relative links; own-repo PR → RP `prs`, localhost → RP `browser:` | – |
| `@path` mention pills in rendered messages | `lib/mentionTokens.ts` | MISSING | style tokens in `inline()` | S |
| Pop-out thread window | `lib/windows.ts` (`OpenWindow chat`) | DONE | `ThreadWindow` (own socket) | – |

## Composer

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Send / queue while running / ⌘↩ steer / Esc interrupt | `AgentChat.vue`, `ComposerTextInput.vue` | DONE | `ComposerStore.submit` | – |
| Queued follow-ups list: edit, remove, reorder, send-now (interrupt+send) | `AgentChat.vue` L530 | DONE | `QueueList`; no collapse toggle | S |
| Per-chat drafts persisted | `ComposerTextInput.vue` | DONE | `composer.drafts` UserDefaults (Vue: config/localStorage — not shared) | – |
| @file / /command / $skill completion with fuzzy rank | `ComposerSuggestions.vue`, `lib/composerCompletion.ts` | DONE | | – |
| Inline token rendering (tokens styled in text, not chips) | `lib/composerDom.ts` | DONE | `composerTokenRanges` restyle | – |
| Image attach: picker, paste, drop, thumbnails | `ComposerImages.vue`, `chatImages.ts` | DONE | | – |
| Large paste → paste chip + view dialog | `PasteChip.vue`, `PasteTextDialog.vue` | MISSING | collapse pastes > N lines into a chip | S |
| Prompt history ↑/↓ | `lib/composerPromptHistory.ts` | MISSING | ↑ on empty/first line recalls previous user msgs | S |
| Prompt stash (park/restore, global) | `ComposerStash.vue`, `lib/promptStash.ts` | MISSING | stash menu; store in config like Vue (check key) | S |
| Undo/redo (⌘Z / ⌘Y) | `ComposerTextInput.vue` | DONE | NSTextView native undo | – |
| Launch bar: agent, model, effort, permission mode, profile | `ModelPicker.vue`, `AgentChat.vue` L652-735 | PARTIAL | Claude/Codex lists hardcoded-ish (`Launch`); ACP effort/mode from adapter `configOptions` / session modes missing; no model catalog prefs | M |
| Cost/usage badge + context ring | `AgentChat.vue` L737-800 | PARTIAL | context ring only; no cost badge | S |
| Side chat button | `AgentChat.vue` | PARTIAL | via thread menu only; opens as separate chat, not Side-chat surface | S |
| Codex user-input panel | `CodexUserInputPanel.vue` | DONE | `Requests.swift` codexInput card | – |
| ⌘L focus composer | `keymap.ts` | MISSING | | S |
| File drop from Finder (non-image → @path) | `FileDropOverlay.vue`, `useFileDrop.ts` | PARTIAL | images only | S |
| Welcome composer: project picker, worktree toggle, PR URL → checkout, send as chat **or terminal** | `WelcomeScreen.vue` | PARTIAL | `NewThreadView` has all except "send as terminal" (launchMode) and ⌘⇧A provider switch | S |

## Requests / attention

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Permission card Allow ⌘Y / Always / Deny | `AgentChat.vue` L16 | PARTIAL | Vue keys are plain **Y / N**; Swift ⌘Y only, no N | S |
| Always rules (`chatPermissionRules`) + auto-allow covered | `claudeChats.ts` | DONE | `AttentionStore.autoAllowCoveredAll` | – |
| File-edit diff preview Accept/Reject | `AgentChat.vue` L57 | PARTIAL | check `PendingRequest` kinds — edit shown as generic permission, no diff preview | S |
| Plan approval (approve / keep planning + feedback) | `AgentChat.vue` L75 | DONE | | – |
| AskUserQuestion stepped, multi-select, "other" | `AgentChat.vue` L580 | DONE | all-at-once card (not stepped) | – |
| ACP permission with adapter's option set | `AgentChat.vue` L95 | DONE | | – |
| Secret request card (provide / use saved / remember / decline) | `SecretRequestCard.vue` | DONE | | – |
| PTY permission banner above xterm (+N counter) | `PtyPermissionBanner.vue` | DONE | `PtyPermissionBanner` (check +N counter) | – |
| PTY permission toasts (answer from anywhere) | `PtyPermissionToasts.vue` | MISSING | floating toast stack with Allow/Always/Deny | S |
| Native notifications when unfocused (permission, waiting, done), click → thread | `stores/notifications.ts`, `chatAttention.ts` | DONE | `Notifier` | – |
| In-app toasts when focused | `ToastStack.vue` | MISSING | generic toast host (also needed for errors — Swift writes errors into `b.status`) | M |
| Notification center (bell, unread/read groups, mark all read, next unread) | `TitleBar.vue` L29-100, `stores/notifications.ts` | MISSING | | M |
| Sounds per event kind (bundled + custom) | `lib/sounds.ts`, Settings → Notifications | MISSING | NSSound per kind, prefs from `uiPrefs` | S |
| ntfy.sh push | `lib/ntfy.ts` | MISSING | POST on transitions if configured (core already does Web Push) | S |
| Dock badge = waiting count | `desktop.go` | DONE | | – |
| `chat_mark_active` while watched | `chatAttention.ts` | DONE | | – |
| Mark seen on view (chat + pty) | `chatAttention.ts`, `Terminal.vue` | DONE | | – |
| Sub-agent auto-archive sweep (30 min, collected) | `chatTree.ts` `subagentArchivable` | MISSING | only if Swift runs without the Vue main window; otherwise skip | S |
| ⌘⇧U jump to first unread | `keymap.ts` | MISSING | | S |

## Right Panel & surfaces

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Panel toggle ⌘⌥B, persisted | `ui.ts` | DONE | `rp.open` | – |
| **Two stacked panes** (top/bottom), split ratio drag, maximize/restore, close bottom pane | `lib/rpLayout.ts` (`burrow.rpLayout`) | MISSING | Swift = one tab strip, one surface. Port `RpLayout` (pure struct + tests) + VSplitView | L |
| Move surface between panes (hover arrow, right-click, drag & drop, drop-zone in lower 55 %) | `RightPanel.vue` L29-60 | MISSING | depends on split | M |
| "Open a surface" picker, close surface tab | `RightPanel.vue` | MISSING | Swift shows all 7 tabs fixed | S |
| Panel in its own window (↗) | `lib/rpWindow.ts` | MISSING | | M |
| Git surface (status, stage/unstage file + hunk, discard, commit w/ generate, push/pull/fetch, conflicts, recent commits) | `RightPanel.vue` Git tab, `git/GitChanges.vue` | DONE | `ChangesView`; per-file +/− numstat missing; recent commits not clickable (no commit detail) | S |
| PRs surface (list open/merged/closed, mine/assigned, detail w/ checks, merge squash, open in worktree, create w/ generate) | `PullRequestsPanel.vue` | DONE | | – |
| Files surface (tree, preview) | `RightPanel.vue` explorer, `FileTreeNode.vue` | PARTIAL | read-only text preview; no editor, no open-in-tab files (`file:` surfaces), no reveal/copy path menu | M |
| **Diff surface** (by turn / branch / worktree, checkpoints + restore) | `DiffTab.vue`, `lib/reviewDiff.ts`, `checkpoints.go` | MISSING | turn/branch diff viewer + checkpoint list + restore confirm | L |
| Diff review comments → send as feedback | `review/DiffReviewComments.vue`, `DiffFeedbackComposer.vue`, `reviewReceipts.ts` | MISSING | after Diff surface | M |
| Agents surface: sub-agent list, spawn dialog (profile picker), result preview | `RightPanel.vue` L451, `SubagentProfilePicker.vue` | PARTIAL | list + spawn + result exist; profile picker in spawn missing | S |
| **Sub-agent detail inside the RP** (child chat view, back, close = delete) | `RightPanel.vue` L453, `lib/subAgentView.ts` | MISSING | "Open" navigates main view to child. Embed `ThreadView(chat:)` in RP with back + close-deletes confirm. Needs per-chat transcript store (Burrow.messages is single) | M |
| Task-tool invocations list (in Agents surface) | `RightPanel.vue` L535 | MISSING | from transcript Task/Agent tool rows | S |
| Progress surface (todo list) | `ProgressSurface.vue` | DONE | | – |
| Background tasks surface (list, tail output, stop) | `BackgroundTasksSurface.vue` | DONE | | – |
| Terminal surface: several shells per workspace, `serve` processes, persisted (`burrow.rpTerms`) + restore filtered by live | `lib/rpTerminals.ts` | PARTIAL | shells + persistence (`rp.terms`) + prune done; agent `serve` shells not adopted (no control-bridge), no sub-tab close/rename | M |
| ⌘J toggle RP terminal (open in bottom pane focused) | `keymap.ts` | MISSING | | S |
| Side chat surface | `SideChatSurface.vue`, `lib/sideChat.ts` | MISSING | `chat_side_create` exists via menu; surface embeds child thread | M |
| Browser surface (dev-server preview) | `BrowserPane.vue` | MISSING | WKWebView + URL bar, kept mounted | M |
| Dev servers surface (list / kill) | `DevServersSurface.vue` | MISSING | | S |
| Markdown doc surface (rendered .md from chat link) | `MarkdownDocSurface.vue` | MISSING | reuse `Markdown` view | S |
| Workspace pulse surface | `WorkspacePulseSurface.vue` | MISSING | | S |
| History tab (pre-turn worktree snapshots) | `RightPanel.vue` L566 | MISSING | part of Diff/checkpoints package | – |
| Extension surfaces | `ExtensionNativeSurface.vue` | MISSING | skip (experimental) | L |

## Terminal

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| PTY render (server-side VT attach, offset resume, resync) | `XTerm.vue`, `runtime/ptyStream.ts` | DONE | libghostty-vt + `PtyStream` | – |
| Keyboard, IME, copy/paste, selection, scroll | `XTerm.vue` | DONE | `TerminalNSView` | – |
| New tab menu (Terminal / Claude / Codex) | `Sidebar.vue`, `Terminal.vue` | PARTIAL | only 3 hardcoded; Vue lists every agent preset (`stores/agents`) + ⌘⇧1-5 launch | S |
| ⌘T new tab, ⌘W close pane, ⌃1-9 switch tab | `keymap.ts` | MISSING | | S |
| Split panes ⌘D / ⌘⇧D, close pane | `TerminalSplitView.vue` | MISSING | split tree per tab | L |
| Pop-out pane ⌘⌥O (frozen in main) | `stores/popouts.ts` | MISSING | ⌘⌥O only for chats | M |
| Tab status dot + auto-title from foreground (`get_pty_foreground`) | `Terminal.vue`, `terminalStatus.ts` | PARTIAL | phase via snapshot; no foreground poll auto-title, no Regenerate title | S |
| Log strip (`burrow log`) | `Terminal.vue` L3 | MISSING | | S |
| Reopen closed tab/thread ⌘⇧T | `lib/closedViews.ts` | MISSING | | S |
| ⌘⇧R repaint | `keymap.ts` | MISSING | `pty_attach` re-paint | S |
| Font family / renderer prefs | Settings → Appearance | MISSING | terminal font from `uiPrefs` | S |
| Phone mirror sizing | `XTerm.vue` | n/a | desktop only | – |

## Git (full manager window)

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Git window (⌘1 history / ⌘2 changes), workspace selector | `git/GitWindow.vue`, `GitPanel.vue` | MISSING | separate WindowGroup | L |
| Commit graph history (lanes) + commit detail (files, copy hash) + context menu (checkout, revert, cherry-pick, reset…) | `git/GitHistory.vue`, `GitCommitDetail.vue`, `lib/gitGraph.ts` | MISSING | port `gitGraph.ts` lane layout | L |
| Branches tree (folders), remotes, tags, worktrees, stashes sidebar | `git/GitSidebar.vue`, `lib/branchTree.ts` | MISSING | | M |
| Toolbar shortcuts: fetch ⌘⇧F, pull ⌘⇧L, push ⌘⇧P, stash ⌘⇧S, new branch ⌘⇧B | `git/GitToolbar.vue` | PARTIAL | RP has pull/push/fetch buttons, no stash/new branch, no shortcuts | S |
| Stash list apply/pop/drop | `GitPanel.vue` L268 | MISSING | | S |
| Merge-conflict resolver (ours/theirs/both) | `git/ConflictResolver.vue` | DONE | `ConflictResolverView` | – |
| Stopped merge/rebase/cherry-pick banner continue/abort | `GitPanel.vue` L116 | DONE | `opBanner` | – |
| Commit dialog / Commit-push menu (commit, commit+push, amend) | `CommitDialog.vue`, `CommitPushMenu.vue` | PARTIAL | commit only; no amend, no commit+push combo | S |
| Branch switch dropdown | `GitPanel.vue` L74, `ThreadDetailsCard.vue` | MISSING | | S |
| Auto-refresh toggle | `AutoRefreshButton.vue` | MISSING | | S |

## PRs

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Forge detect / provider override / auth hint | `PullRequestsPanel.vue` | DONE | | – |
| PR → worktree + chat (`forge_pr_checkout`) | `lib/prCheckout.ts` | DONE | PRDetail + NewThread paste | – |
| PR from Spotlight (pasted URL / #123) | `Spotlight.vue` | MISSING | `parsePrRef` exists — add Spotlight row | S |
| Link/unlink PR to chat, state glyph | `ThreadDetailsCard.vue` | DONE | via menu | – |
| `chat_pr_refresh` on open / after push | `chatpr.go` callers | PARTIAL | after push only | S |
| PR comments / review threads | `PullRequestsPanel.vue` | PARTIAL | check detail: reviews shown, no inline comments | S |

## Files / editor

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Code editor (CodeMirror, save) | `CodeEditor.vue`, `stores/fileViewer.ts` | MISSING | NSTextView editor + `write_text_file`; syntax highlight optional | M |
| LSP (diagnostics, hover, go-to) | `lib/lsp.ts` | MISSING | `lsp_*` commands; skip until editor exists | L |
| File viewer "Return to thread" | `FileViewer.vue` | PARTIAL | FilePreview in RP only | S |
| Open in external app (last used per project, ⌘O) | `lib/openIn.ts` | MISSING | `list_open_targets` + NSWorkspace open | S |
| Path picker (in-app) | `PathPicker.vue`, `lib/pickPath.ts` | DONE (native) | NSOpenPanel is fine on native | – |

## Settings

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| General (auto-sleep, sub-agent deliver, recaps, float pop-outs, text-gen policy, worktree cleanup + preview/remove) | `Settings.vue` General | PARTIAL | text-gen **model** picker + **rules** textbox missing (check) | S |
| Permissions (chat permission rules list/edit) | `PermissionsPanel.vue` | MISSING | list + delete `chatPermissionRules` | S |
| Workspaces (order, icon, remove, saved secrets, add project) | `WorkspaceConfig.vue`, `SavedSecretsPanel.vue` | PARTIAL | per-project overrides (`projectSettings.ts`: env_file, claude_config_dir, setup scripts, forge provider) missing | M |
| Providers (list, enable, edit, OpenAI-compatible, update banner) | `ProvidersPanel.vue`, `OpenAIProviderSettings.vue` | PARTIAL | read-only list ("Edit in desktop app") | M |
| Sub-agent profiles | `agents/SubagentProfilesPanel.vue` | MISSING | | S |
| Scheduled tasks | `ScheduledTasksPanel.vue` | DONE | `ScheduleSheet` (sheet, not in Settings) | – |
| Scripts | `Settings.vue` Scripts | MISSING | | S |
| Skills / MCP servers | `Settings.vue` | MISSING | read-only lists first | S |
| Remote access (pair code, devices, revoke, connect to another Burrow) | `Settings.vue` remote | DONE | `RemoteSettings` | – |
| Appearance (theme light/dark/system, fonts, UI scale, terminal font) | `Settings.vue` Appearance | PARTIAL | own theme palette + text size; no font family pickers (`systemFonts.ts`) | S |
| Notifications (per-kind sounds, toast position, ntfy) | `Settings.vue` Notifications | MISSING | with sounds package | S |
| Integrations, Plugins, Extensions, About (version, update) | `Settings.vue` | MISSING | About + version easy; others skip | S |
| Keybindings editor + recorder + cheatsheet ⌘/ | `stores/keybindings.ts`, `ShortcutRecorder.vue` | PARTIAL | static list only; no rebinding, no tray hotkey recorder | M |

## App shell / windows / tray

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Spotlight ⌘P: actions, threads, projects, tabs, FTS hits | `Spotlight.vue` | PARTIAL | missing: agent launch, settings sections, PR ref, files, "pick project → new thread" ⌘⇧O | S |
| Back / forward ⌘[ ⌘] | `TitleBar.vue`, router | DONE | `Shell.go` | – |
| ⌘1-9 | `keymap.ts` FIXED: **jump to thread** | PARTIAL | Swift binds ⌘1-9 to **projects** — diverges from Vue | S |
| ⌘N new thread, ⌘⇧N new project, ⌘, settings | `keymap.ts` | PARTIAL | ⌘N, ⌘, done; ⌘⇧N new project missing | S |
| Title bar: system/daemon stats, kill orphan PTYs | `TitleBar.vue` | MISSING | | S |
| Dashboard / Mission Control | `Dashboard.vue` | MISSING | | M |
| Onboarding | `Onboarding.vue` | MISSING | | S |
| What's new after update | `lib/whatsNew.ts` | MISSING | | S |
| Auto-update banner / install | `stores/update.ts`, `UpdateBanner.vue` | MISSING | n/a until the Swift app ships | – |
| Tray panel / tray toasts / global hotkey | `TrayPanel.vue`, `TrayToast.vue` | MISSING | owned by Go core; Swift is a window client — skip | – |
| Dock menu recent threads | – | DONE (Swift-only) | | – |
| Pet overlay / animated backdrops | `PetOverlay.vue`, `AnimatedBackdrop.vue` | PARTIAL | `Pet` + `BurrowGlow` | – |
| Quit-hold overlay (⌘Q hold) | `QuitHoldOverlay.vue` | MISSING | | S |
| Protocol revision check / incompatible banner | `runtime/transport.ts` | PARTIAL | reads `minClientRev`, no `rev` compare, no banner | S |
| Resume from `seq` on reconnect | `runtime/transport.ts` | PARTIAL | always full refresh (ok per ponytail note) | – |
| Remote environments (switch machine) | `remoteEndpoint.ts` | DONE | `Remote.swift`, Keychain | – |
| `control:action` bridge (focus_tab, new_tab, tab_output, serve… from agents) | `lib/controlBridge.ts` | MISSING | Without it `burrow spawn`/`serve`/`focus_tab` never reach a Swift-only setup. Needs `ack_control_action` (`ui:ack` scope — only `LocalEndpoint` tickets have it) | M |
| Config live-follow (`config-changed`) | `lib/config.ts` | DONE | `ConfigStore`, `ThreadStore`, `AttentionStore` | – |

## Misc

| Feature | Vue source | Swift | Gap / what to build | Size |
|---|---|---|---|---|
| Docs screen | `DocsScreen.vue` | MISSING | skip | – |
| Workspace screen (`/ws/:id` all tabs view) | `WorkspaceScreen.vue` | MISSING | | M |
| Workspace target picker (new thread: repo vs worktree vs existing worktree) | `WorkspaceTargetPicker.vue` | PARTIAL | project menu + "New worktree" toggle; can't pick an existing worktree | S |
| Image lightbox | `ImageLightbox.vue` | DONE | | – |
| Agent history store | `stores/agentHistory.ts` | MISSING | low value | – |

## Prioritized work packages

Ordered by user value. File prefixes respect `AGENTS.md` ownership; each package names new files so agents don't collide.

1. **RP split layout + sub-agent detail in RP** (owner: git/RP) — `RightPanel.swift`, new `RPLayout.swift` (port `rpLayout.ts` as pure struct + `assert` self-check), `RPSubagent.swift`. Two panes, split drag, maximize, move surface (context menu + hover arrow + drag/drop), surface picker/close, persist `rp.layout`. Sub-agent opens in-panel (back / close→delete). Needs a per-chat transcript store (`TranscriptStore` keyed by chat id, extracted from `Burrow.messages`) — coordinate with composer owner. **L**
2. **Transcript reading UX** (composer) — `Transcript*.swift`: jump-to-latest + autoscroll pause, visit-watermark divider, copy/edit message hover actions, settled-turn folding + duration, files-edited card, find bar ⌘F (`TranscriptFind.swift`), clickable file/PR links → RP. **M–L**
3. **Composer power features** (composer) — `Composer*.swift`: prompt history ↑/↓, prompt stash, paste chip, `/clear`→`chat_clear` + ⌘K, ⌘L focus, recap button, cost badge + plan usage meter, ACP config options in LaunchBar, Finder file drop → @path. **M**
4. **Attention surfaces** (attention) — `Attention*.swift`, new `AttentionToasts.swift`, `AttentionCenter.swift`: in-app toast host (also replace `b.status` error strings), notification center, PTY permission toasts, sounds per kind, Y/N keys on permission card, edit-diff preview, ⌘⇧U next unread. **M**
5. **Sidebar completeness** (threads) — `Sidebar*.swift`, `Thread*.swift`: project header context menu + new-worktree dialog, per-project "+", collapsible + draggable project groups, pinned/archived projects, Working/Snoozed shelves, delete-all-settled, undo-delete, hover details card, Regenerate title, tab status dots/menu items, ⌘B. **M**
6. **Thread details card** (threads) — new `ThreadDetails.swift`: Open-in (`list_open_targets`, ⌘O), scripts, branch switch, PR link, commit/push menu, changes, running servers/bg tasks, scheduled prompts, lineage. **L**
7. **Diff surface + checkpoints + review comments** (git) — new `GitDiffSurface.swift`, `GitCheckpoints.swift`: turn/branch/worktree diff, checkpoint list + restore, diff review comments → feedback to chat. **L**
8. **Terminal parity** (terminal) — `Terminal*.swift`: ⌘T/⌘W/⌃1-9, agent preset list in New tab, split panes, pop-out pane, foreground auto-title, ⌘⇧T reopen, ⌘⇧R repaint, ⌘J RP terminal, terminal font prefs. **L**
9. **Control-bridge** (shell) — new `ShellControl.swift`: listen `control:action`, perform focus_tab / new_tab / serve / tab_output / close, `ack_control_action`. Verify the Swift ticket carries `ui:ack`; if not, report (Go change). **M**
10. **Git manager window** (git) — new `GitWindow.swift`, `GitGraph.swift` (port `gitGraph.ts` + checks), `GitBranches.swift`: history graph, commit detail, branches/tags/stashes/worktrees, toolbar shortcuts, amend / commit+push. **L**
11. **Settings fill-in** (shell) — `Settings*.swift`: Permissions rules, Sub-agent profiles, Scripts, Notifications (sounds/ntfy), text-gen model + rules, per-project overrides, providers edit, keybinding recorder, About/what's new. **M**
12. **RP extra surfaces** (git/RP) — `RPBrowser.swift`, `RPDevServers.swift`, `RPDoc.swift`, `RPSideChat.swift`: browser preview, dev servers, markdown doc, side-chat surface. **M**
13. **Files editor** (git/RP or new `Editor*.swift` owner) — editable file surface + save, open-file tabs in RP; LSP later. **M** (+L for LSP)
14. **Shell odds** (shell) — Spotlight: PR ref / agent launch / settings sections; fix ⌘1-9 to match Vue (threads) or document divergence; ⌘⇧N new project; protocol-rev banner; dashboard (optional). **S–M**

Not worth porting to Swift: tray panel / tray toasts / global hotkey (Go core owns them), auto-updater (until Swift ships), phone shell, extensions, docs screen, agent timeline/inspector.

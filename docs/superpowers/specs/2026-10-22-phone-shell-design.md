# Phone shell for the remote client — design

Date: 2026-10-22 · Status: approved in conversation, pending spec review

## Goal

On a phone-width remote client (`compact`, < 768 px, `src/lib/viewport.ts`) Burrow
should behave like a mobile app — bottom navigation, a stack of full-screen screens,
native back gestures — instead of the desktop layout squeezed by CSS (commit
`4955179`). iPad and desktop layouts do not change.

Success: on a phone you can see what needs you, open and answer a thread, check a
project's changes / PRs / dev servers, and get back with swipe-back or the back
arrow, without ever seeing desktop chrome (title bar, drawers, resize handles).

## Non-goals

- Inline Allow/Deny in the Inbox (answer inside the opened chat).
- Push notifications, swipe actions on rows.
- Any change to iPad or desktop layout.
- New Go commands — the shell uses only existing `invoke` surface.

## Architecture

When `compact` is true, `App.vue` renders `PhoneShell.vue` instead of the desktop
layout (TitleBar, Sidebar panel, resize handles, RightPanel side panel):

```
PhoneShell
  ├─ screen chosen by the route (Threads / Inbox / Project / More / detail)
  ├─ terminal host   ← stays mounted (v-show); chat leaves live inside it
  └─ BottomNav       ← root screens only
```

Global overlays stay where they are (Settings, Spotlight, ToastStack,
PtyPermissionToasts, lightbox, diagram modal). The terminal host must keep its
`v-show` mounting for the same reason as on desktop: re-attaching a PTY corrupts
scrollback (`src/router.ts` header comment).

Rejected: bolting a bottom nav onto the current layout with CSS (still a squeezed
desktop, breaks with every desktop change); a separate mobile app/store (the second
data path that was deliberately removed).

### Routes

Added to `src/router.ts`. Phone-only routes redirect to `/` when not `compact`.

| Route | Screen | Bottom nav |
|-------|--------|-----------|
| `/` | Threads (home) | yes |
| `/inbox` | Inbox | yes |
| `/project` | Project — list of sections | yes |
| `/project/:surface` | section detail; surface ∈ `git`, `pull-requests`, `dev-servers`, `agents` | no, back arrow |
| `/more` | More | yes |
| `/new` | new-thread composer (the existing WelcomeScreen in the terminal host) | no, back arrow |
| `/ws/:wsId/tab/:tabId` | chat / terminal, full screen | no, back arrow |

On a phone `/` is the Threads list, not the welcome composer; the `+` in the
Threads header pushes `/new`, which shows the existing WelcomeScreen full screen.
On desktop `/new` redirects to `/` (which already is the composer there).

### Navigation rules

- Bottom-nav tab switch = `router.replace` (no history entry).
- Opening a detail (thread, project section) = `router.push`, so iOS swipe-back and
  Android back work natively.
- Header back arrow = `router.back()` when there is in-app history, otherwise
  `router.replace(backTarget(route))` — the root of the detail's tab (thread → `/`,
  project section → `/project`, `/new` → `/`). Covers deep links.
- Transitions: push slides in from the right, back slides out to the right, tab
  switch has none. Plain CSS transitions, no library.

### Project detail reuses RightPanel

The git surface is inline in `RightPanel.vue`, so `/project/:surface` renders
`RightPanel` itself full screen with that one surface forced active and its tab
strip hidden in compact. Nothing is copied; desktop RightPanel changes reach the
phone automatically. RightPanel gets a prop for the forced surface; its normal
layout persistence is not written while forced.

## Screens

### Threads (`/`)

- Large title "Threads", `+` icon (new thread), search field below.
- New `PhoneThreadList.vue`, grouped by project. Built from the same stores and
  helpers as Sidebar (`terminalTabs`, `claudeChats`, `getAgentAttentionState`) —
  not the Sidebar component (hover cards, drag, resize are desktop-only).
- Row (~64 px): agent icon, title, preview of the last assistant message (already
  returned by Go, see `chats_lastassistant_test.go`), relative time, status dot.
- Tap opens the thread; long-press opens the existing thread menu.
- Archived threads collapsed at the bottom.
- No workspace: empty state; `+` opens the project picker.

### Inbox (`/inbox`)

- Sections, empty ones hidden: **Needs you** (permission / question pending),
  **Done, unread**, **Running**.
- Source: chats via `chatAttention` (`session.status` from `displayStatus`), PTY
  tabs via their phase / `terminalTabs` status. Grouping is a pure function
  `inboxSections()` in `src/lib/phoneNav.ts`.
- Bottom-nav badge = count of **Needs you**.
- Empty state: "Nothing needs you".
- Tap opens the thread.

### Project (`/project`)

- Header: project name + branch; tap opens a bottom sheet listing projects and
  worktrees (switches the active workspace).
- Rows, each pushing a detail: **Changes** (file count from the git store),
  **Pull requests**, **Dev servers**, **Sub-agents**.

### More (`/more`)

- Settings (existing Settings, full screen), Search (Spotlight), Connection (state,
  last connected, "Disconnect this device" — clears remote credentials and reloads
  into pairing), app version.

### Chat / terminal (`/ws/:wsId/tab/:tabId`)

- Header: back, title, subtitle with project and model, status dot, `⋯` sheet
  (interrupt, restart, clear, sub-agents → `/project/agents`).
- Composer pinned to the bottom with `safe-area-inset-bottom`. Height follows
  `visualViewport` (CSS variable updated on `resize`) so the iOS keyboard neither
  covers the composer nor pushes the header off screen.
- Terminal tabs keep today's xterm + keyboard input, with the same header.

## Visual rules

- Project tokens and components only (`--bg-base`, `--accent`, `--border`,
  `Button`, `Switch`, Phosphor icons). No new palette; animated themes keep working.
- Bottom nav: 4 items (icon + label), 56 px + `safe-area-inset-bottom`, active item
  in `--accent`, opaque background with a top border (no backdrop blur over
  animated themes).
- Headers: large title on root screens, compact 48 px bar with back arrow on
  details; all respect `safe-area-inset-top`.
- Touch targets ≥ 44 px; `:active` feedback instead of hover.
- `PhoneSheet.vue`: one small bottom-sheet component (project picker, `⋯` menu),
  closes on backdrop tap or swipe down.
- The "Mac unreachable" bar (`src/mobile/main.ts`) stays on top, below the safe area.

## Edge cases

- Deep link to a thread that no longer exists → `/`.
- Width changes live (rotation, iPad split view under 768 px): `compact` is
  reactive, the shell swaps, the route is kept; phone-only routes on a wide screen
  redirect to `/`.
- Connection drop: unreachable bar; screens keep showing last known state.

## New files

- `src/components/phone/PhoneShell.vue`
- `src/components/phone/BottomNav.vue`
- `src/components/phone/PhoneHeader.vue`
- `src/components/phone/PhoneSheet.vue`
- `src/components/phone/PhoneThreadList.vue`
- `src/components/phone/InboxScreen.vue`
- `src/components/phone/ProjectScreen.vue`
- `src/components/phone/MoreScreen.vue`
- `src/lib/phoneNav.ts` (+ `phoneNav.test.ts`)

Changed: `src/App.vue` (render switch), `src/router.ts` (routes + redirect),
`src/components/RightPanel.vue` (forced-surface prop, hidden strip in compact).

## Testing

- Vitest on `src/lib/phoneNav.ts`: `inboxSections()` + badge count, `backTarget()`,
  `showsBottomNav()`.
- `commandSurface.test.ts` keeps guarding that no `invoke` escapes the table.
- `pnpm build` (vue-tsc) and the existing suite stay green.
- Manual on a phone: tab switching, swipe-back, deep link, keyboard in chat,
  rotation, connection drop.

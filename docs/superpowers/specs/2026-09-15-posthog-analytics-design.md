# PostHog analytics + error tracking + report problem

## Goal

Basic product analytics and error tracking via PostHog (EU cloud, free tier), plus an
in-app "report problem" action, all reported into the same PostHog project. No email
delivery, no screenshots, no session replay — smallest useful slice.

## Provider

PostHog EU cloud (`eu.i.posthog.com`), account already created by the user. API key
supplied via env at build/run time, read through a new `App.PosthogApiKey()` Wails
binding (same pattern as other config surfaced to the frontend) — never hardcoded.

## Identity

`distinct_id` on both frontend and backend is the existing anonymous `environmentID()`
(`src-wails/environment.go`, already a random per-install id persisted in
`environment.json`). No email, name, or other PII is ever attached to an event.

## Backend (`src-wails/analytics.go`, new file)

- `posthog-go` SDK client, initialized once at app startup (`main.go`), EU host.
- `capture(event string, properties map[string]any)` — thin wrapper that always injects
  `distinct_id` from `environmentID()`.
- `capturePanic(where string)` — `recover()`-based helper. Called via `defer` in
  `main.go` and in the hot-path goroutines that already run detached (PTY read loop,
  phase poll ticker, `/v2/ws` per-connection dispatch goroutine — the same three
  documented in CLAUDE.md's websocket section). Sends a PostHog exception event with
  `where`, the recovered value, and a stack trace; does not re-panic (matches the
  existing per-call `recover` behavior on `/v2/ws`, which already turns a panic into a
  dropped connection rather than a crashed process).
- New `App` method `PosthogApiKey() string` returning the key from env, added to
  `remoteDenied` (local-only, never reachable from a paired remote client — same
  treatment as `LocalEndpoint`).

## Frontend (`src/main.ts` + new `src/lib/analytics.ts`)

- `posthog-js`, initialized once at boot after the api key + environment id are
  available.
- `autocapture: false`, no pageview/pageleave capture (hash-routed single-page terminal
  app — clicks and DOM structure are not meaningful signal here).
- `capture_exceptions: true` — built-in `window.onerror` / `unhandledrejection` capture,
  no custom code needed.
- `src/lib/analytics.ts` exports one function, `track(event, properties?)`, used by call
  sites below. Keeps the SDK import in one place.

## Events (base set)

| Event | Properties | Fired from |
|---|---|---|
| `app_opened` | `app_version`, `os` | frontend boot |
| `workspace_created` | — | `workspace` store, on create |
| `tab_opened` | `kind` (`terminal` \| `agent` \| `chat`) | `terminalTabs` store |
| `agent_spawned` | `agent_command` | wherever a PTY is created with an agent preset |
| `chat_message_sent` | `provider` (claude/acp/etc) | chat send path — **never message content** |
| `pr_created` | — | PR creation control verb |
| `problem_reported` | `description` | report-problem dialog (see below) |

No event carries PTY output, chat content, file contents, or filesystem paths.

## Report problem

New entry in Settings (or Help menu) opening a small dialog: a single textarea for a
description, a Send button. On send: `track('problem_reported', { description,
app_version: <appVersion>, os: <platform> })`. No screenshot, no log bundling, no email.
This can grow later (attach recent log lines, etc.) but that is out of scope here.

## Opt-out

`ui` store gets a new persisted boolean, `analyticsEnabled` (default `true`), shown as a
toggle in Settings → General ("Send anonymous usage data"). `track()` and backend
`capture()` both no-op when disabled — frontend checks the store flag before calling
`posthog.capture`; backend checks a value synced from the frontend via an existing
config-write path (`SetConfig`/`ui.analyticsEnabled` mirrored into `config.json`, read
by `analytics.go` at capture time) so a toggle takes effect without an app restart.

## Testing

One small Go test for `analytics.go`: a pure function that builds the event properties
map (given inputs, produces expected map) — no network call, verifies nothing panics
building a payload. No frontend test added; `track()` is a thin pass-through with no
branching logic worth a dedicated test beyond the opt-out check, which is a one-line
`if`.

## Out of scope (explicitly deferred)

- Email delivery / screenshot capture for report-problem.
- Session replay.
- Go SDK — using raw `posthog-go` client is fine, this refers to skipping any *extra*
  telemetry SDK beyond PostHog.
- Broader event coverage (spotlight usage, worktree actions, etc.) — this is the base
  set only.

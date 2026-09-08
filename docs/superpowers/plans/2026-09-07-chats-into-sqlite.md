# Chats into SQLite (a shared chat list, not a client-owned blob)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Both clients see the same chats, live. Today they cannot, and the desktop can silently delete a chat the phone just created.

**Spec:** none — this is a follow-up `src-wails/remote.go`'s `RemoteCreateChat` doc comment already names as required ("needs the desktop frontend to reload chat state after a remote-triggered creation (a larger change, tracked as a follow-up, not attempted here)").

## Root cause (investigated, not assumed)

Chats live in `config.json` under `chatSessions`. `src/lib/config.ts` reads the **whole file once at boot** into a module-level `cache`, and every `setConfig` writes **the whole cache back**. `App.WriteConfig` (`config.go`) is an atomic whole-file overwrite with **no merge**. Two independent writers — the desktop frontend and Go's `RemoteCreateChat` — therefore do read-modify-write on one blob, last writer wins on **every key**.

Measured consequences:

1. **No live sync.** Nothing emits a change event for chats, so the desktop learns about a phone-created chat only by being reloaded. (Workspaces have `workspaces-changed`; phases have `phase-pty:`/`phase-chat:`. The chat list was the one thing left with nothing.)
2. **Lost creation.** Any desktop `setConfig` between the phone's read and its own next write reverts the phone's session row **and** the `chatIdCounter` bump — a font preference is enough.
3. **Id reuse.** With the counter reverted, the next desktop chat takes the id the phone just used, attaching a fresh chat UI to the phone's still-running CLI process.
4. A dead subscription: `src/mobile/store.ts` used to listen for a `remote-chats` event. **Nothing has ever emitted that name** (zero occurrences in the tree), so cross-client creation never worked in either direction.

Not in scope, and worth saying because it is what actually hid the chat during investigation: a phone-created chat is titled `Chat <count+1>` — the same scheme the desktop uses — so it lands at the bottom of a 56-row list of near-identical names. That is a naming/discoverability problem, tracked separately in Task 7.

## Architecture

`chats` becomes a SQLite table that **Go owns**, alongside `workspaces` and `terminal_tabs`, with a `chats-changed` bus event. Two properties do the real work:

- **`INTEGER PRIMARY KEY AUTOINCREMENT`** — ids are allocated by the database and never reused, even after a delete. Consequence 3 becomes impossible rather than unlikely, and `chatIdCounter` stops existing.
- **Writes are per-row upserts that never delete.** `SaveChats` upserts the rows a client knows about and leaves the rest alone; removal is an explicit `DeleteChat`. Consequence 2 becomes impossible: a client that has never heard of row 87 cannot remove it.

What remains, named honestly: two clients editing **the same field of the same chat** at the same moment still resolve last-writer-wins. That is per-chat-per-field rather than per-file-per-anything, and `chats-changed` makes both converge within a round trip.

## Global Constraints

- **Comments in code in English.** Commit subject English, Conventional Commits.
- **`chatActiveByWs` stays client-side.** Which chat is selected is per-device, exactly like `burrow.seenAt` and for the same reason — the desktop being on chat 78 says nothing about what the phone should show. It stays in `config.json`.
- **`busy` and `status` are NOT columns.** `busy` was already persisted as `false` unconditionally, and `status` is the phase (`pty_phase`, key `chat:<id>`). A second copy of the phase in a second table is precisely the drift the phase work removed.
- **`chatTurns` and `chatPermissionRules` stay in `config.json`.** An activity log and a user preference; neither is the shared-list problem, and moving them is scope this does not need.
- **`chat_messages` and `chat_stream` are untouched.** They already key off the chat id and already live in SQLite; ids are preserved by the migration, so they keep resolving.
- Go: `cd src-wails && go test ./...`. Frontend: `pnpm test`, `pnpm build`, `pnpm build:mobile`. All: `just check`.

---

## File Structure

| file | responsibility |
|---|---|
| `src-wails/chats.go` (new) | `Chat`, the table, `ListChats`/`CreateChat`/`SaveChats`/`DeleteChat`, `chats-changed`, the config.json migration |
| `src-wails/chats_test.go` (new) | autoincrement never reuses, upsert never deletes a stranger's row, migration preserves ids |
| `src-wails/db.go` (modify) | register the schema |
| `src-wails/remote.go` (modify) | `RemoteListChats`/`RemoteCreateChat` onto the store; `remoteCreateChatSession` + its config surgery deleted |
| `src-wails/remoteapi.go` (modify) | `list_chats`, `create_chat`, `save_chats`, `delete_chat` |
| `src/stores/claudeChats.ts` (modify) | load/persist through the store; `nextId` deleted; `create`/`remove` async; `chats-changed` listener |
| `src/components/Terminal.vue`, `ManagerPanel.vue`, `src/lib/controlBridge.ts` (modify) | await the now-async `create` |
| `src/mobile/store.ts` (modify) | listen for `chats-changed` instead of never learning |
| `CLAUDE.md` (modify) | chats are SQLite now; what is still per-device |

---

### Task 1: the table and the store

**Files:** create `src-wails/chats.go`, `src-wails/chats_test.go`; modify `src-wails/db.go`

**Interfaces:**
```go
type Chat struct {
    ID              int64  `json:"id"`
    WorkspaceID     int64  `json:"workspace_id"`
    Title           string `json:"title"`
    PinnedTitle     bool   `json:"pinned_title"`
    ClaudeSessionID string `json:"claude_session_id"`
    MessageCount    int64  `json:"message_count"`
    Control         bool   `json:"control"`
    AgentKind       string `json:"agent_kind"`
    Transport       string `json:"transport"`
    Model           string `json:"model"`
    Branch          string `json:"branch"`
    SettledOverride string `json:"settled_override"`
    ArchivedAt      int64  `json:"archived_at"`
    LastActivityAt  int64  `json:"last_activity_at"`
}

func (a *App) ListChats() ([]Chat, error)
func (a *App) CreateChat(c Chat) (Chat, error)   // ignores c.ID; the DB assigns it
func (a *App) SaveChats(chats []Chat) error      // upsert by id, never deletes
func (a *App) DeleteChat(id int64) error
```

Schema:

```sql
CREATE TABLE IF NOT EXISTS chats (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id      INTEGER NOT NULL,
  title             TEXT    NOT NULL DEFAULT '',
  pinned_title      INTEGER NOT NULL DEFAULT 0,
  claude_session_id TEXT    NOT NULL DEFAULT '',
  message_count     INTEGER NOT NULL DEFAULT 0,
  control           INTEGER NOT NULL DEFAULT 0,
  agent_kind        TEXT    NOT NULL DEFAULT '',
  transport         TEXT    NOT NULL DEFAULT '',
  model             TEXT    NOT NULL DEFAULT '',
  branch            TEXT    NOT NULL DEFAULT '',
  settled_override  TEXT    NOT NULL DEFAULT '',
  archived_at       INTEGER NOT NULL DEFAULT 0,
  last_activity_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS chats_workspace ON chats(workspace_id);
```

`AUTOINCREMENT` is load-bearing and not decoration: plain `INTEGER PRIMARY KEY` reuses the highest freed rowid after a delete, which would hand a new chat the id of a deleted one whose `chat_stream` rows still exist.

Every mutation ends in `busEmit("chats-changed", nil)` — no payload, because the event says "your list is stale" and the list is one cheap call. A payload would be a second representation of the same rows to keep in step.

- [ ] **Step 1: Write the failing test**

```go
func TestChatIdsAreNeverReused(t *testing.T)
// create, delete, create again — the second id must be higher, because
// chat_stream and pty_phase rows for the deleted id may still exist.

func TestSaveChatsNeverDeletesARowItDoesNotKnowAbout(t *testing.T)
// THE bug. A client whose list predates another client's creation saves its
// own rows; the newer row must survive. This is the test that fails today by
// construction, since today's writer is a whole-file overwrite.

func TestSaveChatsUpdatesInPlace(t *testing.T)
func TestArchiveIsJustAColumn(t *testing.T)     // archived rows still listed
func TestMutationsEmitChatsChanged(t *testing.T) // one event per mutation, none on a read
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestChat`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run tests + race**

Run: `cd src-wails && go test ./... && go test -race ./... -run TestChat`

- [ ] **Step 5: Commit** — `feat(chats): a chats table Go owns, with ids that are never reused`

---

### Task 2: migrate config.json → SQLite, once

**Files:** modify `src-wails/chats.go`; test in `src-wails/chats_test.go`

Runs from `startup()`, after `openDB` and before anything serves a client.

**Ids must be preserved** — `chat_stream(chat_id)`, `chat_messages`, and `pty_phase`'s `chat:<id>` keys all reference them, so a renumbering migration would orphan every transcript in the app. Insert with explicit ids, then advance `sqlite_sequence` past the maximum so the next `AUTOINCREMENT` cannot collide with a migrated row.

Idempotent: skip when the table already has rows. On success **remove `chatSessions` and `chatIdCounter` from `config.json`**, so there is one source of truth rather than a stale copy that looks authoritative. (A desktop frontend still holding a pre-migration cache can write those keys back once; harmless, because nothing reads them again.)

- [ ] **Step 1: Write the failing test**

```go
func TestMigrationPreservesChatIds(t *testing.T)
// A renumbering migration orphans every chat_stream row in the app.

func TestMigrationAdvancesTheAutoincrementPastMigratedIds(t *testing.T)
// Without this the first new chat takes an id a migrated chat already holds,
// and inherits its transcript.

func TestMigrationIsIdempotent(t *testing.T)
func TestMigrationTolerdatesAMissingOrGarbageKey(t *testing.T)
// config.json is hand-editable; a broken key must leave an empty list, not
// abort startup.
```

- [ ] **Step 2–4:** implement, run.
- [ ] **Step 5: Commit** — `feat(chats): migrate the config.json chat list into SQLite, once`

---

### Task 3: `remote.go` onto the store

**Files:** modify `src-wails/remote.go`, `src-wails/remoteapi.go`; test `src-wails/remote_test.go`

`RemoteListChats` keeps its wire shape (the phone reads `workspaceName`/`workspacePath` off it) but reads `ListChats` instead of parsing `config.json`. `RemoteCreateChat` calls `CreateChat` then `ClaudeStart`. **`remoteCreateChatSession`, `remoteCreateMu` and the whole read-modify-write of `config.json` are deleted** — the mutex existed only to serialize two `RemoteCreateChat` calls against a hazard the database now handles, and its own doc comment says it never closed the real one.

New wire names: `list_chats` (`scopeOrchRead`), `create_chat` / `save_chats` / `delete_chat` (`scopeOrchOperate`).

- [ ] **Step 1: Write the failing test** — `RemoteCreateChat` returns a row that `ListChats` then contains; two creations get distinct ids; the reply keeps the fields the phone reads.
- [ ] **Step 2–4:** implement, run.
- [ ] **Step 5: Commit** — `refactor(remote): chat creation through the store, not through config.json`

---

### Task 4: `claudeChats.ts` on the store

**Files:** modify `src/stores/claudeChats.ts`

- load: `list_chats` (mapped from snake_case) instead of `getConfig(SESSIONS_KEY)`
- `persist()`: `save_chats` with the rows this client holds
- `create()`: **async**, id from `create_chat` — the client stops inventing ids
- `remove()`: `delete_chat`
- `nextId`, `COUNTER_KEY`, `SESSIONS_KEY` and their legacy-migration lines: deleted
- `chatActiveByWs` and `chatPermissionRules`: unchanged, still `config.json` (Global Constraints)

`spawnActor` still runs per loaded session, and `busy`/`status` are still client-side —
they are simply no longer written to disk.

- [ ] **Step 1: Write the failing test** — `src/stores/claudeChats.test.ts`: a reload merge keeps live actors and drops rows the server no longer has; `create` returns the server's id.
- [ ] **Step 2–4:** implement, `pnpm test`, `pnpm build`.
- [ ] **Step 5: Commit** — `refactor(chats): the desktop reads and writes the chat store, not config.json`

---

### Task 5: await the async `create`

**Files:** modify `src/components/Terminal.vue`, `src/components/ManagerPanel.vue`, `src/lib/controlBridge.ts`

Five call sites, all in fire-and-forget handlers or already-async functions:
`Terminal.openClaudeChat` (×2 branches), `Terminal.makeChatLeaf` → `splitFocused`,
`ManagerPanel`'s new-thread handler, `controlBridge`'s `spawn`-as-chat.

The ripple stops at the handlers — nothing reads a return value except
`controlBridge` (already `async`) and `ManagerPanel` (awaits fine).

- [ ] **Step 1–3:** implement, `pnpm build`, `pnpm test`.
- [ ] **Step 4: Commit** — `refactor(chats): await chat creation now that the id comes from the server`

---

### Task 6: live sync on both clients

**Files:** modify `src/stores/claudeChats.ts`, `src/mobile/store.ts`

Both listen for `chats-changed` and reload their list. This is the part that makes
the original report impossible: a chat created anywhere appears everywhere without a
reload, because the event and the transport that carries it already exist.

The desktop's reload **merges** rather than replaces — it must not discard a running
actor or an in-flight `busy` for a chat that is only being re-read.

- [ ] **Step 1: Write the failing test** — a `chats-changed` event triggers exactly one reload and preserves the live fields.
- [ ] **Step 2–4:** implement, run `just check`.
- [ ] **Step 5: Commit** — `feat(chats): both clients follow chats-changed`

---

### Task 7: name a new chat so it can be found

**Files:** modify `src-wails/chats.go` or `src/stores/claudeChats.ts`

`Chat <count+1>` was fine when one client made chats in a list of five. With 56
chats in one workspace and both clients using the same scheme, a new chat is
indistinguishable from the 55 above it — which is why the chat in the original
report was on screen and still could not be found.

Minimum: a phone-created chat says where it came from, and the Sidebar can sort or
mark by `last_activity_at`. Deliberately kept small and last, because it is a
labelling fix and not the data bug.

- [ ] **Step 1–3:** implement, run.
- [ ] **Step 4: Commit** — `fix(chats): a new chat is findable among fifty others`

---

### Task 8: docs

**Files:** modify `CLAUDE.md`

- [ ] Chats are a SQLite table Go owns, with `chats-changed`; ids are `AUTOINCREMENT` and never reused, and why that matters given `chat_stream`; writes are upserts that never delete, and what race is left; what is still per-device (`chatActiveByWs`, `seenAt`) and why; that `config.json` is no longer a shared-state store.
- [ ] **Commit**

---

## Self-review

**Coverage:**

| root-cause consequence | task |
|---|---|
| no live sync | 1 (event), 6 (listeners) |
| lost creation on a racing write | 1 (upsert never deletes) |
| id reuse via a reverted counter | 1 (`AUTOINCREMENT`) |
| the dead `remote-chats` subscription | 6 (a real event with real listeners) |
| a new chat cannot be spotted | 7 |

**Deliberately out of scope:** `chatTurns`, `chatPermissionRules`, `chatActiveByWs`
(Global Constraints, with reasons). Field-level last-writer-wins on one chat edited
simultaneously from both clients — named in Architecture rather than solved, because
solving it means per-field versioning for a conflict two humans on one account will
essentially never hit.

**Type consistency:** Go `Chat` JSON (`snake_case`) ↔ `ClaudeSession` (`camelCase`) —
Task 4 owns the mapping in one place, and the test that `create` returns the server's
id is what keeps the two from drifting.

**Placeholders:** none. The root cause was verified against the running install
(config.json mtime after the phone's write, with the phone's row surviving, proving
the whole-file overwrite and the desktop's re-read) rather than inferred.

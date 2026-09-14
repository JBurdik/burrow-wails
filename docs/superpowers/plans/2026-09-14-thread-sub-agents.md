# Thread Sub-Agents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent running in a chat thread can spawn sub-agents that belong to that thread — read and driven from the Right Panel, watched and steered by the parent, and deleted or archived along with it.

**Architecture:** One new column, `chats.parent_chat_id`, carries the relationship. `BURROW_CHAT_ID` in a chat agent's environment tells the control API which thread a `spawn` came from. The Sidebar filters children out; a new Right Panel section mounts each child's `AgentChat` the way `ManagerPanel` already does. Three verbs let the parent watch (`agent_status`), steer (`chat_send`) and collect (`wait_result`) its children.

**Tech Stack:** Go 1.x + Wails v2 (`src-wails/`), SQLite, Vue 3 + Pinia + TypeScript (`src/`), vitest, `go test`.

**Spec:** `docs/superpowers/specs/2026-09-14-thread-sub-agents-design.md`

## Global Constraints

- Depth is capped at **one level**: a chat with `parent_chat_id != 0` may not spawn. The error string is exactly `sub-agent cannot spawn sub-agents`.
- `parent_chat_id = 0` means top-level. Never NULL — the column is `NOT NULL DEFAULT 0`.
- `ListChats` returns children alongside top-level chats. Filtering is the **client's** job.
- Every new `App` method MUST get an entry in `remoteAllowed` or `remoteDenied` in `src-wails/remoteapi.go`, or `TestRemoteSurfaceIsExhaustive` fails. That failure is the design working.
- Every new `invoke("...")` wire name MUST exist in `remoteAllowed` or `CLIENT_SIDE_COMMANDS`, or `src/lib/wailsCompat/commandSurface.test.ts` fails.
- Go tests run with `cd src-wails && go test ./...`. Frontend checks run with `pnpm test` and `pnpm build` (the latter is `vue-tsc` + vite, so it is the type check).
- Commit after every task. Commit messages in English, imperative mood.

---

### Task 1: The `parent_chat_id` column

**Files:**
- Modify: `src-wails/chats.go` (`Chat` struct ~line 40, `chatsSchema()` ~line 61, `chatColumns` ~line 83, `scanChat` ~line 87, `CreateChat` ~line 122, `SaveChats` ~line 154)
- Modify: `src-wails/db.go:136-150` (the `ALTER TABLE` migration block)
- Test: `src-wails/chats_test.go`

**Interfaces:**
- Consumes: nothing — this is the first task.
- Produces: `Chat.ParentChatID int64` (JSON `parent_chat_id`), persisted by `CreateChat` and `SaveChats`, returned by `ListChats`.

- [ ] **Step 1: Write the failing test**

Append to `src-wails/chats_test.go`:

```go
// A child chat's parent must survive the round trip, or the Right Panel has no
// way to tell a sub-agent from a thread.
func TestParentChatIDRoundTrips(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	parent, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	if parent.ParentChatID != 0 {
		t.Errorf("a top-level chat got parent %d, want 0", parent.ParentChatID)
	}
	child, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "child", ParentChatID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentChatID != parent.ID {
		t.Fatalf("CreateChat returned parent %d, want %d", child.ParentChatID, parent.ID)
	}

	list, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	var got int64 = -1
	for _, c := range list {
		if c.ID == child.ID {
			got = c.ParentChatID
		}
	}
	if got != parent.ID {
		t.Fatalf("ListChats reported parent %d, want %d", got, parent.ID)
	}
}

// SaveChats is the only path a client has for editing a row, so it has to carry
// the parent too — otherwise the first save after a spawn orphans the child.
func TestSaveChatsKeepsParent(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	parent, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	child, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "child", ParentChatID: parent.ID})

	child.Title = "renamed"
	if err := a.SaveChats([]Chat{child}); err != nil {
		t.Fatal(err)
	}
	list, _ := a.ListChats()
	for _, c := range list {
		if c.ID == child.ID && c.ParentChatID != parent.ID {
			t.Fatalf("parent became %d after SaveChats, want %d", c.ParentChatID, parent.ID)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd src-wails && go test ./... -run 'TestParentChatIDRoundTrips|TestSaveChatsKeepsParent'`
Expected: compile error — `unknown field ParentChatID in struct literal`.

- [ ] **Step 3: Add the field, the column and the migration**

In `src-wails/chats.go`, add to the `Chat` struct after `LastActivityAt`:

```go
	// 0 means top-level. A sub-agent spawned by a thread carries that thread's
	// id here: the Sidebar filters on it, the Right Panel scopes on it, and
	// DeleteChat cascades on it. A column rather than a title tag, because all
	// three of those would otherwise rest on a string.
	ParentChatID int64 `json:"parent_chat_id"`
```

In `chatsSchema()`, add `parent_chat_id INTEGER NOT NULL DEFAULT 0` as the last column of the `CREATE TABLE`, and a second statement after the existing index:

```go
		`CREATE INDEX IF NOT EXISTS chats_parent ON chats(parent_chat_id)`,
```

In `src-wails/db.go`, append to the `ALTER TABLE` list (these run best-effort; a duplicate-column error on an existing DB is expected and ignored, same as every line above it):

```go
		`ALTER TABLE chats ADD COLUMN parent_chat_id INTEGER NOT NULL DEFAULT 0`,
```

Update `chatColumns` to end with `, parent_chat_id`, `scanChat` to scan `&c.ParentChatID` last, `CreateChat`'s INSERT to name `parent_chat_id` with one more `?` and pass `c.ParentChatID`, and `SaveChats`' UPDATE to set `parent_chat_id=?` with `c.ParentChatID` passed before `c.ID`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd src-wails && go test ./... -run 'TestParentChatIDRoundTrips|TestSaveChatsKeepsParent' -v`
Expected: PASS.

- [ ] **Step 5: Run the whole Go suite**

Run: `cd src-wails && go test ./...`
Expected: PASS — in particular `TestChatIdsAreNeverReused` and the migration tests in `dbmigrate_test.go`.

- [ ] **Step 6: Commit**

```bash
git add src-wails/chats.go src-wails/db.go src-wails/chats_test.go
git commit -m "Give a chat a parent chat"
```

---

### Task 2: Delete cascades to children

**Files:**
- Modify: `src-wails/chats.go` (`DeleteChat` ~line 190)
- Test: `src-wails/chats_test.go`

**Interfaces:**
- Consumes: `Chat.ParentChatID` from Task 1.
- Produces: `DeleteChat(id int64) error` — unchanged signature, now also deletes rows whose `parent_chat_id = id`.

- [ ] **Step 1: Write the failing test**

Append to `src-wails/chats_test.go`:

```go
// Deleting a thread takes its sub-agents with it: they exist only under it, so
// leaving them behind leaves rows nothing in the UI can reach.
func TestDeleteChatCascadesToChildren(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	parent, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	childA, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "a", ParentChatID: parent.ID})
	childB, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "b", ParentChatID: parent.ID})
	bystander, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "unrelated"})

	if err := a.DeleteChat(parent.ID); err != nil {
		t.Fatal(err)
	}

	list, _ := a.ListChats()
	alive := map[int64]bool{}
	for _, c := range list {
		alive[c.ID] = true
	}
	for _, gone := range []int64{parent.ID, childA.ID, childB.ID} {
		if alive[gone] {
			t.Errorf("chat %d survived the cascade", gone)
		}
	}
	if !alive[bystander.ID] {
		t.Error("the cascade took an unrelated chat")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src-wails && go test ./... -run TestDeleteChatCascadesToChildren`
Expected: FAIL — `chat <id> survived the cascade` for both children.

- [ ] **Step 3: Make the delete cascade**

Replace the body of `DeleteChat` in `src-wails/chats.go`:

```go
// DeleteChat removes a chat and, with it, every sub-agent spawned under it. A
// child exists only underneath its thread, so leaving it behind leaves a row no
// surface can reach. One statement each rather than a foreign key: the table is
// created without one on existing installs, and ON DELETE CASCADE needs
// PRAGMA foreign_keys per connection to fire at all.
func (a *App) DeleteChat(id int64) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	if _, err := a.db.Exec(`DELETE FROM chats WHERE parent_chat_id = ?`, id); err != nil {
		return err
	}
	res, err := a.db.Exec(`DELETE FROM chats WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	busEmit("chats-changed", nil)
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd src-wails && go test ./... -run TestDeleteChatCascadesToChildren -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src-wails/chats.go src-wails/chats_test.go
git commit -m "Delete a thread's sub-agents with it"
```

---

### Task 3: `BURROW_CHAT_ID` reaches the control API

**Files:**
- Modify: `src-wails/controlapi.go` (`addBurrowEnv` ~line 368)
- Modify: `src-wails/claudechat.go` (`ClaudeStart`, where the process environment is assembled)
- Modify: `src-wails/bin/burrow` (the CLI script — where `$BURROW_CWD` is added to the request body)
- Test: `src-wails/controlapi_test.go`, `src-wails/burrowcli_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `BURROW_CHAT_ID` in a chat agent's environment, and a `parent_chat_id` field in every `POST /v1/<verb>` body sent by a CLI running inside a chat.

- [ ] **Step 1: Read how `cwd` gets there today**

Run: `grep -n 'BURROW_CWD' src-wails/bin/burrow src-wails/controlapi.go`
This is the pattern to copy exactly — the new value rides the same road.

- [ ] **Step 2: Write the failing test**

Append to `src-wails/controlapi_test.go`:

```go
// A chat agent has no BURROW_PTY_ID (a chat is not a tab), so the chat id is
// the only thing that can tell the control API which thread a spawn came from.
// burrow-mcp is a child of the CLI process and inherits this, so both doors
// agree without a second mechanism.
func TestAddBurrowEnvCarriesChatID(t *testing.T) {
	a := &App{}
	env := map[string]string{}
	a.addBurrowEnvForChat(env, t.TempDir(), 42)
	if env["BURROW_CHAT_ID"] != "42" {
		t.Fatalf("BURROW_CHAT_ID = %q, want \"42\"", env["BURROW_CHAT_ID"])
	}
}

// A chat id of 0 means "not a chat" — exporting it would make every tab look
// like a child of chat 0.
func TestAddBurrowEnvOmitsZeroChatID(t *testing.T) {
	a := &App{}
	env := map[string]string{}
	a.addBurrowEnv(env, t.TempDir())
	if _, ok := env["BURROW_CHAT_ID"]; ok {
		t.Fatal("BURROW_CHAT_ID was exported with no chat")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestAddBurrowEnv'`
Expected: compile error — `a.addBurrowEnvForChat undefined`.

- [ ] **Step 4: Export the chat id**

In `src-wails/controlapi.go`, keep `addBurrowEnv` as-is and add beneath it:

```go
// addBurrowEnvForChat is addBurrowEnv plus the chat id, so a `spawn` made from
// inside this chat can be attributed to it. Zero is left unset rather than
// exported as "0" — an absent variable says "not a chat", a zero says "child of
// chat 0", and only one of those is true.
func (a *App) addBurrowEnvForChat(env map[string]string, cwd string, chatID int64) {
	a.addBurrowEnv(env, cwd)
	if chatID > 0 {
		env["BURROW_CHAT_ID"] = strconv.FormatInt(chatID, 10)
	}
}
```

Add `"strconv"` to the imports if it isn't there. Then change every chat-process launch site to call it: in `claudechat.go`'s `ClaudeStart` and in `acp.go`'s session start, replace `a.addBurrowEnv(env, cwd)` with `a.addBurrowEnvForChat(env, cwd, chatIDOf(id))`, where `id` is the chat id the function already has as a string — add this helper next to `addBurrowEnvForChat`:

```go
// chatIDOf parses the string chat id the chat managers key on. A non-numeric id
// yields 0, i.e. "not a chat", which is the safe direction.
func chatIDOf(id string) int64 {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd src-wails && go test ./... -run 'TestAddBurrowEnv' -v`
Expected: PASS.

- [ ] **Step 6: Send it from the CLI**

In `src-wails/bin/burrow`, find where `cwd` is injected into the JSON body (it uses `$BURROW_CWD`) and add the same treatment for `BURROW_CHAT_ID` as `parent_chat_id`, omitting the field entirely when the variable is unset. The script may use only `curl` and `sed` — no `python3`, no `node`, no tty — so follow the existing quoting helper rather than introducing one.

- [ ] **Step 7: Verify the CLI still parses**

Run: `cd src-wails && go test ./... -run TestBurrowCLI -v && sh -n bin/burrow`
Expected: PASS and no output from `sh -n`.

- [ ] **Step 8: Commit**

```bash
git add src-wails/controlapi.go src-wails/claudechat.go src-wails/acp.go src-wails/bin/burrow src-wails/controlapi_test.go
git commit -m "Tell the control API which chat a call came from"
```

---

### Task 4: `spawn` attributes the parent and refuses to nest

**Files:**
- Modify: `src-wails/internal/control/verbs_delegate.go` (`spawn` verb definition ~line 38, `Core.spawn` ~line 110)
- Test: `src-wails/internal/control/control_test.go`

**Interfaces:**
- Consumes: the `parent_chat_id` request field from Task 3.
- Produces: the UI verb `spawn` now receives `parent_chat_id` (int64) in its args map, and `target` is forced to `"chat"` whenever it is non-zero.

- [ ] **Step 1: Write the failing test**

Append to `src-wails/internal/control/control_test.go` (follow the file's existing fake-`UIBridge` helper; if it has none, add one that records the args map it was called with and returns a canned `SpawnResult`):

```go
// A spawn made from inside a thread produces a sub-agent OF that thread, and a
// sub-agent is a chat: a terminal tab would put it back in the Sidebar, which
// is the arrangement this feature exists to replace.
func TestSpawnFromChatForcesChatTargetAndCarriesParent(t *testing.T) {
	ui := &fakeUI{result: SpawnResult{ChatID: 9, Target: "chat"}}
	c := New(Deps{UI: ui})

	if _, err := c.Dispatch(context.Background(), "spawn", Params{
		"task":           "investigate the cache bug",
		"target":         "tab",
		"parent_chat_id": float64(7),
	}); err != nil {
		t.Fatal(err)
	}
	if ui.args["target"] != "chat" {
		t.Errorf("target = %v, want chat", ui.args["target"])
	}
	if ui.args["parent_chat_id"] != int64(7) {
		t.Errorf("parent_chat_id = %v, want 7", ui.args["parent_chat_id"])
	}
}

// Depth is capped at one level: recursive agent trees run away in cost and the
// panel that shows them is a flat list.
func TestSubAgentCannotSpawn(t *testing.T) {
	ui := &fakeUI{result: SpawnResult{ChatID: 9, Target: "chat"}}
	c := New(Deps{UI: ui})

	_, err := c.Dispatch(context.Background(), "spawn", Params{
		"task":              "do more work",
		"parent_chat_id":    float64(7),
		"caller_is_subagent": true,
	})
	if err == nil || !strings.Contains(err.Error(), "sub-agent cannot spawn sub-agents") {
		t.Fatalf("err = %v, want the sub-agent guard", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src-wails && go test ./internal/control/ -run 'TestSpawnFromChat|TestSubAgentCannotSpawn'`
Expected: FAIL — `target = tab, want chat`.

- [ ] **Step 3: Carry the parent through**

In `verbs_delegate.go`, add two args to the `spawn` verb's `Args` list:

```go
			{Name: "parent_chat_id", Type: "integer", Desc: "Set automatically from BURROW_CHAT_ID — the thread this sub-agent belongs to"},
			{Name: "caller_is_subagent", Type: "boolean", Desc: "Set automatically — a sub-agent may not spawn further sub-agents"},
```

and at the top of `Core.spawn`, before the existing target validation:

```go
	if p.Bool("caller_is_subagent") {
		return nil, fmt.Errorf("sub-agent cannot spawn sub-agents")
	}
	parent := p.Int("parent_chat_id")
	// A sub-agent that belongs to a thread IS a chat: a terminal tab would put
	// it back in the Sidebar as a peer, which is the arrangement this replaces.
	if parent > 0 {
		p["target"] = "chat"
	}
```

and add `"parent_chat_id": parent` to the `args` map handed to `c.ui`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd src-wails && go test ./internal/control/ -run 'TestSpawnFromChat|TestSubAgentCannotSpawn' -v`
Expected: PASS.

- [ ] **Step 5: Set `caller_is_subagent` server-side**

The flag must not be forgeable by the prompt, so the HTTP layer sets it, not the caller. In `src-wails/controlapi.go`'s handler, after decoding the body and before dispatching: look the `parent_chat_id` of the *calling* chat up in the `chats` table and set `caller_is_subagent` when that row itself has a non-zero parent. Add to `controlapi_test.go`:

```go
// The guard has to be server-side: a prompt can say anything, so the app is
// what decides whether the caller is already somebody's sub-agent.
func TestCallerIsSubagentIsServerDerived(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()
	parent, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	child, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "child", ParentChatID: parent.ID})

	if a.chatIsSubagent(parent.ID) {
		t.Error("a top-level chat was called a sub-agent")
	}
	if !a.chatIsSubagent(child.ID) {
		t.Error("a child chat was not recognised as a sub-agent")
	}
}
```

Implement `chatIsSubagent(id int64) bool` in `chats.go` as a single `SELECT parent_chat_id FROM chats WHERE id = ?` returning `parent > 0` (false on any error — an unknown chat is not a sub-agent).

- [ ] **Step 6: Run the suite**

Run: `cd src-wails && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add src-wails/internal/control/verbs_delegate.go src-wails/internal/control/control_test.go src-wails/controlapi.go src-wails/chats.go src-wails/controlapi_test.go
git commit -m "Attribute a spawn to the thread that made it"
```

---

### Task 5: `wait_result` and `collect_results` understand chats

**Files:**
- Modify: `src-wails/internal/control/control.go` (`Deps` ~line 132)
- Modify: `src-wails/internal/control/verbs_delegate.go` (`wait_result` verb ~line 92, `Core.waitResult` ~line 164, `Core.collectResults` ~line 187)
- Modify: `src-wails/controlapi.go` (where `control.Deps{...}` is built — wire the new `Phases` dependency)
- Modify: `src-wails/chats.go` (add `collected_at` handling)
- Modify: `src-wails/db.go` (the `ALTER TABLE` block)
- Test: `src-wails/internal/control/control_test.go`

**Interfaces:**
- Consumes: `Chat.ParentChatID` (Task 1).
- Produces:
  - `control.Phases` interface: `Phase(key string) (state string, turnEndedAt int64)`, keyed `"chat:<id>"` / `"pty:<id>"`.
  - `control.ChatReader` interface: `LastAssistantMessage(chatID int64) (string, error)` and `UncollectedChildren(parentChatID int64) ([]int64, error)` and `MarkCollected(chatID int64) error`.
  - `wait_result` accepts `chat_id` as an alternative to `token`.

- [ ] **Step 1: Write the failing test**

Append to `src-wails/internal/control/control_test.go`:

```go
type fakePhases struct{ state string; endedAt int64 }

func (f *fakePhases) Phase(key string) (string, int64) { return f.state, f.endedAt }

type fakeChats struct{ last string }

func (f *fakeChats) LastAssistantMessage(int64) (string, error)     { return f.last, nil }
func (f *fakeChats) UncollectedChildren(int64) ([]int64, error)     { return nil, nil }
func (f *fakeChats) MarkCollected(int64) error                      { return nil }

// A chat sub-agent writes no capture files, so waiting on one has to read the
// phase Go already derives — which also means waiting works with no view of the
// child mounted anywhere.
func TestWaitResultResolvesFromChatPhase(t *testing.T) {
	c := New(Deps{Phases: &fakePhases{state: "done", endedAt: 123}, Chats: &fakeChats{last: "found it: an off-by-one"}})

	out, err := c.Dispatch(context.Background(), "wait_result", Params{"chat_id": float64(9), "timeout": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(Result).Text; got != "found it: an off-by-one" {
		t.Fatalf("text = %q", got)
	}
}

// A failed turn still ends the wait: the caller wants to know, and blocking
// until the timeout tells it nothing it can act on.
func TestWaitResultReturnsOnFailedPhase(t *testing.T) {
	c := New(Deps{Phases: &fakePhases{state: "failed", endedAt: 1}, Chats: &fakeChats{last: "could not build"}})
	if _, err := c.Dispatch(context.Background(), "wait_result", Params{"chat_id": float64(9), "timeout": float64(5)}); err != nil {
		t.Fatal(err)
	}
}

// Neither a token nor a chat id is a caller error, not a ten-minute block.
func TestWaitResultNeedsATarget(t *testing.T) {
	c := New(Deps{})
	_, err := c.Dispatch(context.Background(), "wait_result", Params{})
	if err == nil || !strings.Contains(err.Error(), "needs a token or a chat_id") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src-wails && go test ./internal/control/ -run TestWaitResult`
Expected: compile error — `unknown field Phases in struct literal of type Deps`.

- [ ] **Step 3: Add the dependencies and the chat branch**

In `control.go`, add to `Deps`:

```go
	// Phases reports the derived agent phase for a key ("chat:<id>", "pty:<id>").
	// An interface, not the store: this package takes capabilities, never the app.
	Phases Phases
	// Chats reads a chat's transcript tail and tracks which children have been
	// collected — the chat equivalent of the <token>.result/.done files.
	Chats ChatReader
```

and beside the other capability interfaces:

```go
type Phases interface {
	Phase(key string) (state string, turnEndedAt int64)
}

type ChatReader interface {
	LastAssistantMessage(chatID int64) (string, error)
	UncollectedChildren(parentChatID int64) ([]int64, error)
	MarkCollected(chatID int64) error
}
```

In `verbs_delegate.go`, add to the `wait_result` verb's args:

```go
			{Name: "chat_id", Type: "integer", Desc: "Chat sub-agent to wait on, instead of a token"},
```

and rewrite `Core.waitResult`'s head:

```go
func (c *Core) waitResult(ctx context.Context, p Params) (any, error) {
	token := p.Str("token")
	chatID := p.Int("chat_id")
	if token == "" && chatID <= 0 {
		return nil, fmt.Errorf("wait_result needs a token or a chat_id")
	}
	timeout := time.Duration(p.Int("timeout")) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	deadline := time.Now().Add(timeout)

	for {
		if chatID > 0 {
			// A chat writes no capture files; the phase Go derives IS the
			// completion signal, and it is derived with no client attached.
			if state, _ := c.deps.Phases.Phase(fmt.Sprintf("chat:%d", chatID)); state == "done" || state == "failed" || state == "stale" {
				text, err := c.deps.Chats.LastAssistantMessage(chatID)
				if err != nil {
					return nil, err
				}
				_ = c.deps.Chats.MarkCollected(chatID)
				return Result{Token: fmt.Sprintf("chat:%d", chatID), Text: text}, nil
			}
		} else if text, ok := c.takeResult(token); ok {
			return Result{Token: token, Text: text}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("wait_result: %s did not finish within %s (it may still be working — check agent_status)", firstNonEmpty(token, fmt.Sprintf("chat:%d", chatID)), timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd src-wails && go test ./internal/control/ -run TestWaitResult -v`
Expected: PASS.

- [ ] **Step 5: Implement the app-side adapters**

Add `collected_at INTEGER NOT NULL DEFAULT 0` to `chatsSchema()`'s `CREATE TABLE` and as another `ALTER TABLE chats ADD COLUMN collected_at INTEGER NOT NULL DEFAULT 0` line in `db.go`. Do **not** add it to `Chat`/`chatColumns` — it is bookkeeping for `collect_results`, not client state, and adding it to the struct would put it in `SaveChats`' last-writer-wins path.

In `src-wails/chats.go`, add the three methods the interface needs, on `*App`:

```go
// LastAssistantMessage is a finished sub-agent's answer: the last assistant
// entry in its transcript. The transcript is JSON payloads, so this decodes
// rather than querying a column that does not exist.
func (a *App) LastAssistantMessage(chatID int64) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("no database")
	}
	rows, err := a.db.Query(`SELECT payload_json FROM chat_messages WHERE chat_id = ? ORDER BY ord DESC LIMIT 50`, chatID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		var msg struct {
			Role string `json:"role"`
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(raw), &msg) == nil && msg.Role == "assistant" && strings.TrimSpace(msg.Text) != "" {
			return msg.Text, nil
		}
	}
	return "", rows.Err()
}

// UncollectedChildren is collect_results' chat half: finished sub-agents of this
// thread that nobody has taken yet. collected_at plays the role the .done files
// play for a PTY capture — without it collect_results returns the same answer
// forever.
func (a *App) UncollectedChildren(parentChatID int64) ([]int64, error) {
	out := []int64{}
	if a.db == nil {
		return out, nil
	}
	rows, err := a.db.Query(`SELECT id FROM chats WHERE parent_chat_id = ? AND collected_at = 0 ORDER BY id`, parentChatID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (a *App) MarkCollected(chatID int64) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	_, err := a.db.Exec(`UPDATE chats SET collected_at = ? WHERE id = ?`, time.Now().UnixMilli(), chatID)
	return err
}
```

Add a `phasesAdapter` in `controlapi.go` that wraps the app's `PhaseStore`:

```go
// phasesAdapter is PhaseStore behind control's Phases interface — the control
// package takes capabilities, so the store itself never crosses the boundary.
type phasesAdapter struct{ s *PhaseStore }

func (p phasesAdapter) Phase(key string) (string, int64) {
	if p.s == nil {
		return "idle", 0
	}
	ph := p.s.Get(key)
	return string(ph.State), ph.TurnEndedAt
}
```

(Check the actual field names on `agentphase.Phase` with `grep -n 'type Phase struct' -A 20 src-wails/internal/agentphase/phase.go` and use those — `State` and `TurnEndedAt` are the expected names.)

Wire both into the `control.Deps{...}` literal: `Phases: phasesAdapter{s: a.phases}, Chats: a`.

- [ ] **Step 6: Make `collect_results` sweep chat children too**

`collectResults` today reads `.done` files only, so a parent whose children are
all chats gets an empty list forever. Add the chat sweep at the top of
`Core.collectResults` — it needs the caller's own chat id, so add
`parent_chat_id` to the `collect_results` verb's args (set from
`BURROW_CHAT_ID` by the same server-side path as `spawn`):

```go
func (c *Core) collectResults(p Params) (any, error) {
	out := []Result{}
	// Chat sub-agents first: they write no .done file, so collected_at is what
	// stops this from returning the same answer on every call.
	if parent := p.Int("parent_chat_id"); parent > 0 && c.deps.Chats != nil {
		ids, err := c.deps.Chats.UncollectedChildren(parent)
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			state, _ := c.deps.Phases.Phase(fmt.Sprintf("chat:%d", id))
			if state != "done" && state != "failed" && state != "stale" {
				continue // still working — not a result yet
			}
			text, err := c.deps.Chats.LastAssistantMessage(id)
			if err != nil {
				return out, err
			}
			_ = c.deps.Chats.MarkCollected(id)
			out = append(out, Result{Token: fmt.Sprintf("chat:%d", id), Text: text})
		}
	}
	// ...then the existing .done file sweep, appending into the same `out`.
```

Change the verb's `Fn` to `func(ctx context.Context, p Params) (any, error) { return c.collectResults(p) }`.

- [ ] **Step 7: Test the chat sweep**

Append to `src-wails/internal/control/control_test.go`:

```go
// Without collected_at, every call would hand back the same finished child —
// which is how a supervising loop turns into an infinite one.
func TestCollectResultsTakesEachChildOnce(t *testing.T) {
	chats := &countingChats{children: []int64{5}, last: "done deal"}
	c := New(Deps{Phases: &fakePhases{state: "done", endedAt: 1}, Chats: chats})

	first, err := c.Dispatch(context.Background(), "collect_results", Params{"parent_chat_id": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.([]Result)) != 1 {
		t.Fatalf("first sweep returned %d results, want 1", len(first.([]Result)))
	}
	if chats.marked != 1 {
		t.Fatalf("MarkCollected called %d times, want 1", chats.marked)
	}
}
```

with `countingChats` a `ChatReader` whose `UncollectedChildren` returns its
`children` slice until `MarkCollected` empties it, incrementing `marked`.

Run: `cd src-wails && go test ./internal/control/ -run TestCollectResults -v`
Expected: PASS.

- [ ] **Step 8: Run the whole suite**

Run: `cd src-wails && go test ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add src-wails/internal/control src-wails/controlapi.go src-wails/chats.go src-wails/db.go
git commit -m "Let a parent wait on and collect chat sub-agents"
```

---

### Task 6: `chat_send` — the parent steers a child

**Files:**
- Modify: `src-wails/internal/control/verbs_delegate.go`
- Modify: `src/lib/controlBridge.ts` (the `perform` switch ~line 59)
- Test: `src-wails/internal/control/control_test.go`

**Interfaces:**
- Consumes: the `UIBridge` pattern used by `tab_output` and `list_agents`.
- Produces: verb `chat_send` with args `chat_id` (integer, required) and `text` (string, required), dispatched to the UI action name `chat_send` with args `{chatId, text}`.

- [ ] **Step 1: Write the failing test**

Append to `src-wails/internal/control/control_test.go`:

```go
// send_to_tab types into a PTY, which a chat sub-agent does not have. Without
// this verb a parent can only start a child and wait — it cannot correct one
// that is heading the wrong way.
func TestChatSendReachesTheUI(t *testing.T) {
	ui := &fakeUI{}
	c := New(Deps{UI: ui})

	if _, err := c.Dispatch(context.Background(), "chat_send", Params{"chat_id": float64(9), "text": "stop and summarise"}); err != nil {
		t.Fatal(err)
	}
	if ui.action != "chat_send" {
		t.Fatalf("action = %q", ui.action)
	}
	if ui.args["chatId"] != int64(9) || ui.args["text"] != "stop and summarise" {
		t.Fatalf("args = %v", ui.args)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src-wails && go test ./internal/control/ -run TestChatSendReachesTheUI`
Expected: FAIL with `unknown verb "chat_send"`.

- [ ] **Step 3: Register the verb**

Append to the slice returned by `delegationVerbs`:

```go
	}, {
		Name:    "chat_send",
		Summary: "Send a follow-up message to a chat sub-agent and submit it",
		Args: []Arg{
			{Name: "chat_id", Type: "integer", Desc: "Chat to send to — a sub-agent id from agent_status", Required: true},
			{Name: "text", Type: "string", Desc: "Message to send", Required: true},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			if p.Int("chat_id") <= 0 {
				return nil, fmt.Errorf("chat_send needs a chat_id")
			}
			if strings.TrimSpace(p.Str("text")) == "" {
				return nil, fmt.Errorf("chat_send needs text")
			}
			var out any
			return out, c.ui(ctx, "chat_send", map[string]any{"chatId": p.Int("chat_id"), "text": p.Str("text")}, &out)
		},
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd src-wails && go test ./internal/control/ -run TestChatSendReachesTheUI -v`
Expected: PASS.

- [ ] **Step 5: Handle it in the UI bridge**

In `src/lib/controlBridge.ts`, add to the `perform` switch:

```ts
    case "chat_send":
      return chatSendFollowUp(num(args.chatId), str(args.text));
```

and the implementation next to `spawn()`:

```ts
/** A follow-up into a child's session — the same call the composer makes, so a
 *  steered child is indistinguishable from one the user typed into. */
async function chatSendFollowUp(chatId: number, text: string) {
  if (!chatId) throw new Error("chat_send needs a chat_id");
  if (!text.trim()) throw new Error("chat_send needs text");
  const session = chatSession(chatId);
  await session.send(text);
  return { chat_id: chatId, sent: true };
}
```

Import `chatSession` from `@/lib/chatSession`. **Check the send method's real name first** — run `grep -n 'send' src/lib/chatSession.ts | head -20` and use what `ChatSession` actually exposes; if sending lives on the mounted component rather than the session, route through the store's actor instead and adjust this function, keeping the exported name `chatSendFollowUp`.

- [ ] **Step 6: Type-check**

Run: `pnpm build`
Expected: no `vue-tsc` errors.

- [ ] **Step 7: Commit**

```bash
git add src-wails/internal/control src/lib/controlBridge.ts
git commit -m "Let a parent send a follow-up to a chat sub-agent"
```

---

### Task 7: The store knows about children

**Files:**
- Modify: `src/stores/claudeChats.ts` (`ClaudeSession` ~line 16, `ChatRow` ~line 91, `sessionFromRow` ~line 111, `rowFromSession` ~line 131, `create` ~line 321, `remove` ~line 370, `archive` ~line 399)
- Modify: `src/components/Sidebar.vue` (the chat list)
- Test: `src/stores/claudeChats.test.ts` (create it)

**Interfaces:**
- Consumes: `parent_chat_id` on the wire (Task 1).
- Produces:
  - `ClaudeSession.parentChatId?: number`
  - `create(workspaceId, opts?: { agentKind?: string; parentChatId?: number })`
  - `childrenOf(parentChatId: number): ClaudeSession[]`
  - `topLevelForWs(workspaceId: number): ClaudeSession[]`

- [ ] **Step 1: Write the failing test**

Create `src/stores/claudeChats.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { childrenOf, topLevel } from "./chatTree";

const sessions = [
  { id: 1, workspaceId: 1, parentChatId: undefined },
  { id: 2, workspaceId: 1, parentChatId: 1 },
  { id: 3, workspaceId: 1, parentChatId: 1 },
  { id: 4, workspaceId: 1, parentChatId: undefined },
  { id: 5, workspaceId: 2, parentChatId: 4 },
] as any[];

describe("chat tree", () => {
  it("lists a thread's children in id order", () => {
    expect(childrenOf(sessions, 1).map((s) => s.id)).toEqual([2, 3]);
  });

  it("keeps sub-agents out of the top-level list", () => {
    // This is what stops a thread that spawned three helpers from showing four
    // sibling entries in the Sidebar.
    expect(topLevel(sessions, 1).map((s) => s.id)).toEqual([1, 4]);
  });

  it("treats an unknown parent as no children", () => {
    expect(childrenOf(sessions, 99)).toEqual([]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test src/stores/claudeChats.test.ts`
Expected: FAIL — cannot resolve `./chatTree`.

- [ ] **Step 3: Write the pure helpers**

Create `src/stores/chatTree.ts`:

```ts
import type { ClaudeSession } from "./claudeChats";

/** Sub-agents of one thread, oldest first. Pure so the Sidebar/panel filter is
 *  testable without a store, a transport or a mounted component. */
export function childrenOf(sessions: ClaudeSession[], parentChatId: number): ClaudeSession[] {
  return sessions.filter((s) => s.parentChatId === parentChatId).sort((a, b) => a.id - b.id);
}

/** Threads of one workspace: a sub-agent lives in the Right Panel, never in the
 *  Sidebar, so it is filtered out here rather than at each call site. */
export function topLevel(sessions: ClaudeSession[], workspaceId: number): ClaudeSession[] {
  return sessions.filter((s) => s.workspaceId === workspaceId && !s.parentChatId);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test src/stores/claudeChats.test.ts`
Expected: PASS, 3 tests.

- [ ] **Step 5: Thread the field through the store**

In `src/stores/claudeChats.ts`:
- add `parentChatId?: number;` to `ClaudeSession` with a comment: *"The thread that spawned this sub-agent. Undefined for a normal thread; a sub-agent lives in the Right Panel and is filtered out of the Sidebar."*
- add `parent_chat_id: number;` to `ChatRow`
- map it both ways: `parentChatId: r.parent_chat_id || undefined` in `sessionFromRow`, `parent_chat_id: s.parentChatId ?? 0` in `rowFromSession`
- widen `create`'s opts to `{ agentKind?: string; parentChatId?: number }` and pass `parentChatId: opts?.parentChatId` into the `rowFromSession({...})` literal
- export `childrenOf`/`topLevel` bound to `sessions.value` from the store's return object, so components don't import the pure module and the store separately

In `remove(id)`, before stopping the chat itself, recurse into children so their CLI processes actually die (Go's cascade removes the rows, but only the client knows which stop verb each transport uses):

```ts
    // A thread's sub-agents go with it. Go cascades the ROWS; the processes are
    // ours to stop, because which stop verb applies depends on the transport.
    for (const child of childrenOf(sessions.value, id)) await remove(child.id);
```

In `archive(id)`, archive children in the same pass (same reasoning — an archived thread whose helpers keep running is not archived).

- [ ] **Step 6: Keep a child out of the active-chat slot**

`activeByWs` records which THREAD a workspace is showing, and a sub-agent is not
a thread — if one ever lands there the workspace's main view switches to a chat
the Sidebar does not even list. Guard `setActive`:

```ts
  function setActive(workspaceId: number, sessionId: number) {
    // A sub-agent lives in the Right Panel; it is never the workspace's chat.
    if (sessions.value.find((s) => s.id === sessionId)?.parentChatId) return;
    activeByWs.value[workspaceId] = sessionId;
    persist();
  }
```

Also check `ensureSession`/`activeSession` (around line 355) so a workspace whose
only chats are children still creates a real thread rather than adopting one.

- [ ] **Step 7: Filter the Sidebar**

In `src/components/Sidebar.vue`, the chat list for a workspace must use `topLevel(...)` instead of filtering on `workspaceId` alone. Find it with `grep -n 'sessions' src/components/Sidebar.vue`.

- [ ] **Step 8: Verify**

Run: `pnpm test && pnpm build`
Expected: all tests pass, no type errors.

- [ ] **Step 9: Commit**

```bash
git add src/stores/chatTree.ts src/stores/claudeChats.test.ts src/stores/claudeChats.ts src/components/Sidebar.vue
git commit -m "Keep sub-agents out of the Sidebar's thread list"
```

---

### Task 8: `spawn` creates a child and announces it

**Files:**
- Modify: `src/lib/controlBridge.ts` (`spawn` ~line 156, `agentStatus` ~line 211)
- Modify: `src/lib/chatTypes.ts:8-22` (the `ChatMessage` interface)
- Test: `src/lib/controlBridge.test.ts` (create it, covering the pure message builder only)

**Interfaces:**
- Consumes: `create(workspaceId, { agentKind, parentChatId })` and `childrenOf` (Task 7); `parent_chat_id` in the spawn args (Task 4).
- Produces:
  - `ChatMessage.subagentChatId?: number` and `ChatMessage.subagentAgent?: string`, carried on a `role: "system-info"` message.
  - `subagentMessage(chatId: number, title: string, agentName: string): ChatMessage` exported from `src/lib/chatTypes.ts`.
  - `agent_status` output gains `parent_chat_id` and `children: number[]` per chat.

- [ ] **Step 1: Write the failing test**

Create `src/lib/controlBridge.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { subagentMessage } from "./chatTypes";

describe("subagentMessage", () => {
  it("is a system-info row that names the child chat", () => {
    // The transcript row is how a thread records that it delegated, and the id
    // is what makes it clickable — without it the row is just prose.
    const m = subagentMessage(42, "investigate the cache bug", "codex");
    expect(m.role).toBe("system-info");
    expect(m.subagentChatId).toBe(42);
    expect(m.subagentAgent).toBe("codex");
    expect(m.text).toContain("investigate the cache bug");
  });

  it("falls back to a generic label with no task text", () => {
    expect(subagentMessage(7, "", "claude").text).toContain("Sub-agent");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test src/lib/controlBridge.test.ts`
Expected: FAIL — `subagentMessage` is not exported.

- [ ] **Step 3: Add the message shape**

In `src/lib/chatTypes.ts`, add to `ChatMessage`:

```ts
  // Set on a "system-info" row that records a spawn: the thread delegated, and
  // this is the child it created. The id is what makes the row clickable —
  // clicking opens the Right Panel on that sub-agent.
  subagentChatId?: number;
  subagentAgent?: string;
```

and below the interface:

```ts
let subagentMsgSeq = 0;

/** The transcript row a thread gets when it spawns a sub-agent. */
export function subagentMessage(chatId: number, title: string, agentName: string): ChatMessage {
  const label = title.split("\n")[0].trim();
  return {
    id: Date.now() * 1000 + (subagentMsgSeq++ % 1000),
    role: "system-info",
    text: label ? `Spawned sub-agent: ${label}` : "Spawned Sub-agent",
    subagentChatId: chatId,
    subagentAgent: agentName,
  };
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test src/lib/controlBridge.test.ts`
Expected: PASS, 2 tests.

- [ ] **Step 5: Branch `spawn` on the parent**

In `src/lib/controlBridge.ts`, replace the `openAs === "chat"` branch:

```ts
  const parentChatId = num(args.parent_chat_id);
  if (openAs === "chat") {
    const chats = useClaudeChatsStore();
    const session = await chats.create(target.id, { agentKind: instance.id, parentChatId: parentChatId || undefined });
    if (parentChatId) {
      // A sub-agent belongs to its thread, so it does NOT go through
      // openChat() — that call is what puts a chat in the Sidebar. The parent's
      // transcript gets a row instead, and chats-changed repaints the panel.
      await appendSubagentMessage(parentChatId, session.id, task, instance.name);
      chatSession(session.id).send(task);
      return { chat_id: session.id, workspace_id: target.id, parent_chat_id: parentChatId };
    }
    useTerminalTabsStore().openChat(target.id, session.id, instance.id, task);
    return { chat_id: session.id, workspace_id: target.id };
  }
```

and add the append helper, which reads the parent's transcript, pushes the row and saves it back through the existing persistence call:

```ts
async function appendSubagentMessage(parentChatId: number, childChatId: number, task: string, agentName: string) {
  const raw = await invoke<string>("load_chat_messages", { chatId: parentChatId }).catch(() => "[]");
  const messages: ChatMessage[] = JSON.parse(raw || "[]");
  messages.push(subagentMessage(childChatId, task, agentName));
  await invoke("save_chat_messages", { chatId: parentChatId, messagesJson: JSON.stringify(messages), foldedOrd: -1 });
}
```

Check the exact wire names and argument names for `load_chat_messages` / `save_chat_messages` in `src-wails/remoteapi.go` before writing this — the table, not the Go signature, is authoritative. If `AgentChat.vue` is mounted for the parent it owns the transcript in memory; appending underneath it is acceptable here because the component reloads on `chats-changed`, but verify that manually in Task 10.

- [ ] **Step 6: Report children in `agent_status`**

In `agentStatus()`, for each chat push `parent_chat_id: s.parentChatId ?? 0` and `children: childrenOf(chats.sessions, s.id).map((c) => c.id)`.

- [ ] **Step 7: Verify**

Run: `pnpm test && pnpm build`
Expected: PASS, including `commandSurface.test.ts`.

- [ ] **Step 8: Commit**

```bash
git add src/lib/chatTypes.ts src/lib/controlBridge.ts src/lib/controlBridge.test.ts
git commit -m "Spawn a sub-agent under the thread that asked for it"
```

---

### Task 9: The Right Panel surface

**Files:**
- Modify: `src/components/RightPanel.vue` (the `agents` tab block ~line 324, `tabs` ~line 558, `subagentList` ~line 492)
- Modify: `src/components/AgentChat.vue` (the message renderer, for `role === "system-info"` with `subagentChatId`)
- Modify: `src/stores/ui.ts` (a way to open the panel on a surface)

**Interfaces:**
- Consumes: `childrenOf` (Task 7), `subagentChatId` on `ChatMessage` (Task 8), `phase-chat:{id}` events.
- Produces: `ui.openRightPanelSurface(tabId: string)` — makes the panel visible and selects a surface.

- [ ] **Step 1: Add the panel opener**

In `src/stores/ui.ts`, next to `toggleRightPanel`:

```ts
  // The panel and which surface it shows are separate pieces of state (the
  // surface lives per-workspace in RightPanel). This is the one call a caller
  // outside the panel needs: show it, and say what to show.
  const pendingRightPanelSurface = ref<string | null>(null);
  function openRightPanelSurface(tabId: string) {
    rightPanelVisible.value = true;
    pendingRightPanelSurface.value = tabId;
  }
```

Export both. `RightPanel.vue` watches `pendingRightPanelSurface`, and on a non-null value opens that surface (adding it to `openedTabIds` if absent), sets `activeTab`, and clears the ref back to `null`.

- [ ] **Step 2: Rewrite the `agents` surface**

Replace the `activeTab === 'agents'` block in `RightPanel.vue` with two sections and a detail view. Sketch — match the file's existing class conventions:

```vue
    <div v-else-if="activeTab === 'agents'" class="flex min-h-0 flex-1 flex-col">
      <!-- Detail: one child's full chat, mounted the way ManagerPanel is. -->
      <template v-if="openChildId">
        <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
          <button class="rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground" aria-label="Back to sub-agents" @click="openChildId = null"><PhCaretLeft :size="12" /></button>
          <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] font-semibold text-foreground">{{ chatTitle(openChildId) }}</span>
        </div>
        <!-- Kept mounted per engaged child: a busy sub-agent keeps streaming
             while you look at another one, the same reason ManagerPanel uses
             v-show rather than v-if. -->
        <AgentChat
          v-for="id in engagedChildIds"
          :key="id"
          v-show="id === openChildId"
          :chat-id="id"
          :workspace-id="props.workspaceId!"
          :cwd="props.cwd"
          compact
          class="min-h-0 flex-1"
        />
      </template>

      <template v-else>
        <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
          <span class="flex-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Sub-agents</span>
          <button class="rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Spawn a sub-agent under this thread" aria-label="Spawn a sub-agent" @click="spawnChildManually"><PhPlus :size="12" /></button>
        </div>
        <div v-if="!activeChatId" class="m-2 rounded-[var(--radius-card)] border border-dashed border-border/60 px-4 py-6 text-center text-[11px] leading-[1.7] text-muted-foreground">
          Open a chat to see its sub-agents.
        </div>
        <div v-else-if="!childList.length" class="m-2 rounded-[var(--radius-card)] border border-dashed border-border/60 px-4 py-6 text-center text-[11px] leading-[1.7] text-muted-foreground">
          No sub-agents yet.<br />This thread's agent can spawn one, or use +.
        </div>
        <div
          v-for="child in childList"
          :key="child.id"
          class="flex cursor-pointer items-center gap-1.5 border-b border-border/40 px-2 py-[6px] transition-colors hover:bg-hover"
          @click="openChild(child.id)"
        >
          <PhRobot :size="12" class="shrink-0 text-muted-foreground" />
          <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] text-secondary-foreground">{{ child.title }}</span>
          <span class="shrink-0 text-[9px] text-muted-foreground">{{ childPhase[child.id] ?? "idle" }}</span>
        </div>

        <!-- The existing Task-tool list, narrowed from workspace-wide to this
             thread: both sections answer "what did THIS thread delegate". -->
        <div class="flex shrink-0 items-center gap-1.5 border-y border-border px-2 py-1.5">
          <span class="flex-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Task tool</span>
        </div>
        <!-- ...unchanged v-for over subagentList... -->
      </template>
    </div>
```

Script additions:

```ts
const openChildId = ref<number | null>(null);
const engagedChildIds = ref<number[]>([]);
const activeChatId = computed(() => (props.workspaceId ? chats.activeSession(props.workspaceId)?.id ?? null : null));
const childList = computed(() => (activeChatId.value ? childrenOf(chats.sessions, activeChatId.value) : []));

function openChild(id: number) {
  if (!engagedChildIds.value.includes(id)) engagedChildIds.value.push(id);
  openChildId.value = id;
}

// Phase per child, straight off the bus — the same event Terminal.vue listens
// to for tabs. This is the first frontend consumer of phase-chat:.
const childPhase = reactive<Record<number, string>>({});
const phaseUnsubs: Array<() => void> = [];
watch(childList, (list) => {
  for (const child of list) {
    if (child.id in childPhase) continue;
    childPhase[child.id] = "idle";
    listen(`phase-chat:${child.id}`, (p: any) => { childPhase[child.id] = p?.state ?? "idle"; }).then((un) => phaseUnsubs.push(un));
  }
}, { immediate: true });
onUnmounted(() => phaseUnsubs.forEach((un) => un()));
```

A child's `AgentChat` is a second consumer of the same chat-id-keyed session
registry the main view uses. The registry only evicts idle sessions, so a busy
child survives an unmount — but the component must still `release()` its
handlers when the panel drops it, or a finished child streams into a view that
no longer exists. `AgentChat` does this in its own `onUnmounted`; the thing to
get right here is that `engagedChildIds` is **per workspace key** like
`terminalPtyByWs`, so switching projects does not leave another project's child
mounted. Store it in `WsUiState` alongside `openedTabIds` rather than as a bare
`ref`, and make `openChildId` a `WsUiState` field too.

Use the file's existing event-listener import (check with `grep -n 'listen\|EventsOn' src/components/RightPanel.vue src/components/Terminal.vue | head`) and the real payload field name for the phase state (`grep -n 'applyPhase' -A 10 src/components/Terminal.vue`).

`spawnChildManually` prompts for a task and calls the same path as the verb:

```ts
async function spawnChildManually() {
  if (!activeChatId.value || !props.workspaceId) return;
  const task = window.prompt("What should the sub-agent do?");
  if (!task?.trim()) return;
  // Same door as the agent's own spawn: one implementation, two callers.
  await perform("spawn", { task, target: "chat", parent_chat_id: activeChatId.value, cwd: props.cwd });
}
```

Update `subagentList` to scope to `activeChatId` instead of every chat in the workspace, and change its empty-state copy accordingly. Update the surface's description in `tabs` to `"Sub-agents spawned by this chat."`.

- [ ] **Step 3: Render the transcript row**

In `AgentChat.vue`, where `role === "system-info"` is rendered, add a branch for `msg.subagentChatId`: a clickable row with a robot icon, `msg.text`, the agent name, and a live dot from `phase-chat:{msg.subagentChatId}`. Its click handler:

```ts
function openSubagentFromTranscript(chatId: number) {
  // The panel is where a sub-agent lives; the transcript row is a pointer to it.
  useUIStore().openRightPanelSurface("agents");
  subagentToOpen.value = chatId;
}
```

Route `subagentToOpen` to the panel the same way `openRightPanelGitTab` is provided/injected in `App.vue:387-391` — follow that pattern rather than inventing a second one.

- [ ] **Step 4: Verify**

Run: `pnpm test && pnpm build`
Expected: PASS, no type errors.

- [ ] **Step 5: Commit**

```bash
git add src/components/RightPanel.vue src/components/AgentChat.vue src/stores/ui.ts src/App.vue
git commit -m "Show and drive a thread's sub-agents in the right panel"
```

---

### Task 10: Docs and manual verification

**Files:**
- Modify: `src-wails/agentdocs/skills/burrow/SKILL.md`
- Modify: `docs/context.html`
- Modify: `docs/burrow.html`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: everything above.
- Produces: no code.

- [ ] **Step 1: Teach the agents the new shape**

In `src-wails/agentdocs/skills/burrow/SKILL.md`, add a section:

```markdown
## Sub-agents of your thread

A `spawn` you make from a chat creates a sub-agent that belongs to **your
thread**: it does not appear in the sidebar, it lives in the right panel, and it
is deleted when your thread is. You do not pass the relationship — the app knows
which chat you are.

- `burrow agent_status` — your children are the `children` ids on your own row.
- `burrow chat_send <chat_id> --text "..."` — correct a child mid-task.
- `burrow wait_result --chat-id <id>` — block until it finishes and read its
  answer.

A sub-agent may not spawn sub-agents of its own.
```

- [ ] **Step 2: Update the HTML references**

`docs/context.html`: add `chats.parent_chat_id`, the `chat_send` verb and the new Right Panel surface behaviour. `docs/burrow.html`: add `chat_send`, `wait_result --chat-id`, and the `BURROW_CHAT_ID` environment variable to the env list.

- [ ] **Step 3: Update CLAUDE.md**

In the "The chat list is shared state" section, add a paragraph: `parent_chat_id` is what makes a sub-agent belong to a thread rather than sit beside it; the Sidebar filters on it, the Right Panel scopes on it, and `DeleteChat` cascades on it. Note the one-level depth cap and why.

- [ ] **Step 4: Run everything**

Run: `cd src-wails && go test ./... && cd .. && pnpm test && pnpm build`
Expected: all green.

- [ ] **Step 5: Manual verification (`just dev`)**

Nothing above proves the feature works in the app — no test in this repo drives a real agent. Check, in order:

1. Open a chat, ask the agent to `burrow spawn --task "list the files in src/stores"`. A row appears in the transcript **and** in the panel's Sub-agents section.
2. Click the transcript row → the right panel opens on Sub-agents with that child selected, showing its live stream.
3. Go back to the list, switch to another thread → the section changes to that thread's children.
4. From the parent, `burrow agent_status` → the child is listed under `children`; `burrow chat_send` → the message lands in the child's stream.
5. Delete the parent thread → the child disappears from the panel and its CLI process is gone (`ps aux | grep claude`).
6. Ask the **child** to spawn → it gets `sub-agent cannot spawn sub-agents`.

- [ ] **Step 6: Commit**

```bash
git add src-wails/agentdocs docs CLAUDE.md
git commit -m "Document thread sub-agents"
```

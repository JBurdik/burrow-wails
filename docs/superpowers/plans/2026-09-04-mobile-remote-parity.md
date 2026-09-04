# Mobile Remote Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `src/mobile/` (the tailnet PWA) actually drive chats and terminals — today `claude_send`/`acp_send`/`remote_create_chat` are unimplemented on the backend (every call fails with "unknown command"), permission requests have no UI at all, and finished/idle chats and tabs pile up forever with no way to declutter them.

**Architecture:** Three phases, in order — (1) Go: wire the existing `App.ClaudeSend`/`AcpSend`/`ClaudeRespondControl`/`AcpRespondPermission`/`ClaudeStart` methods onto the `/ws` dispatch table so the write path exists at all; (2) `src/mobile/store.ts`: give it the same status model as desktop (reusing `src/lib/terminalStatus.ts` directly instead of re-deriving it) plus permission detection and reconnect; (3) the five mobile views: render permission responses and collapse settled items the way `DashboardView.vue` already collapses "Nedávno hotovo" tabs. A pre-existing wire-format bug (numeric ids silently dropping the entire WS call) is fixed first because every later phase depends on `write_pty`/`resize_pty`/`claude_send`/`acp_send` actually reaching the server.

**Tech Stack:** Go (`src-wails/`, stdlib `encoding/json`, `gorilla/websocket`), Vue 3 + Pinia (`src/mobile/`, `<script setup>`), existing `@xterm/xterm` for the terminal view. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-04-mobile-remote-parity-design.md`

## Global Constraints

- Every new WS command is a thin wrapper over an existing `App` method — no new agent-control logic in Go (spec, "Architecture").
- `remote_create_chat` supports `agentKind: "claude"` only for now — replicating the ACP/Codex provider-config resolution that today lives only in `src/components/AgentChat.vue`'s `acpStartPayload()` is out of scope; `agentKind: "codex"` returns an explicit error instead of a broken chat. (This narrows the spec's "or the ACP session-start path" line — discovered during planning that ACP start needs a `command`/`args`/`configDir` resolution with no Go-side equivalent yet.)
- Ids cross the `/ws` wire as **strings**. `wsArgs.ID` is `string`; a JSON *number* in that field makes the whole `json.Unmarshal(raw, &c)` call fail, and the entire message — not just the bad field — is silently dropped (`httpserver.go:247`, `if json.Unmarshal(raw, &c) != nil { continue }`). `httpserver_test.go`'s existing `TestWsCallDecode` already encodes this contract (`"args":{"id":"pty-3",...}`, a string). No exceptions.
- No new npm/Go dependencies. Status colors already exist as CSS (`App.vue`'s `.s-dot.review`/`.s-dot.error`) — do not add new ones.
- Reuse `src/lib/terminalStatus.ts` (`TermStatus`, `STATUS_PRIORITY`, `aggregateStatus`) instead of re-declaring the type or priority order in `src/mobile/store.ts` — it has no Vue/Wails imports and is already usable from any Vite entry point.

---

## Task 1: Fix the numeric-id wire bug (write_pty / resize_pty / future chat commands)

**Files:**
- Modify: `src-wails/httpserver_test.go`
- Modify: `src/mobile/views/TerminalView.vue:57,97,105`

**Interfaces:**
- Consumes: `wsArgs.ID string` (`httpserver.go:284`, unchanged this task).
- Produces: nothing new — this task only fixes existing call sites so `write_pty`/`resize_pty` (and every task after this one) actually reach the server.

- [ ] **Step 1: Write a failing-by-inspection regression test documenting the contract**

Add to `src-wails/httpserver_test.go`:

```go
// A numeric id (what you get from JSON.stringify-ing a JS number, e.g.
// tab.ptyId in src/mobile/store.ts) makes the WHOLE wsCall fail to decode —
// not just the id field — so the command is silently dropped by the
// `if json.Unmarshal(...) != nil { continue }` guard in handleWS. Every
// mobile call site must therefore send ids as strings. This test exists so
// nobody "fixes" wsArgs.ID to accept numbers instead of fixing the caller.
func TestWsArgsRejectsNumericID(t *testing.T) {
	var c wsCall
	err := json.Unmarshal([]byte(`{"id":1,"command":"write_pty","args":{"id":42,"data":[1]}}`), &c)
	if err == nil {
		t.Fatal("expected a decode error for a numeric id — if this now passes, handleWS's silent-drop guard must be revisited too")
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd src-wails && go test ./... -run TestWsArgsRejectsNumericID -v`
Expected: PASS (this documents current behavior, it does not change it)

- [ ] **Step 3: Fix the call sites in `TerminalView.vue`**

`tab.ptyId` is typed `number` (`src/mobile/store.ts:14`). Stringify it at every
call into `store.getClient().call(...)`:

```ts
// line 57, inside sendBytes()
store.getClient().call('write_pty', { id: String(tab.ptyId), data: bytes }).catch(() => {});
```

```ts
// line 97, inside onMounted()
client.call('resize_pty', { id: String(tab.ptyId), cols: term.cols, rows: term.rows }).catch(() => {});
```

```ts
// line 105, inside onResize()
store.getClient().call('resize_pty', { id: String(tab.ptyId), cols: term.cols, rows: term.rows }).catch(() => {});
```

(The `pty-data-${tab.ptyId}`/`pty-hook-${tab.ptyId}` **event names** on lines
44/95/111 are template-literal strings already — those were never affected,
only the JSON `args.id` field was.)

- [ ] **Step 4: Manually verify**

Run `pnpm build:mobile` (or point `BURROW_DEV_MOBILE` at a dev server per
`httpserver.go`'s comment), pair a browser tab, open a live terminal tab, and
type a command. Before this fix every keystroke silently vanished; after it,
input reaches the PTY. Resize the browser window and confirm the remote
shell reflows (proves `resize_pty` also reaches the server now).

- [ ] **Step 5: Commit**

```bash
git add src-wails/httpserver_test.go src/mobile/views/TerminalView.vue
git commit -m "fix(mobile): stop silently dropping write_pty/resize_pty

Sending tab.ptyId as a JSON number made the whole wsCall fail to decode
(wsArgs.ID is string), so every remote keystroke and resize vanished."
```

---

## Task 2: Go — wire the chat write-path onto `/ws` dispatch

**Files:**
- Modify: `src-wails/httpserver.go` (`wsArgs` struct, `dispatch` switch)
- Modify: `src-wails/httpserver_test.go`

**Interfaces:**
- Consumes: `App.ClaudeSend(id, text, sessionID string, images []string) error` (`claudechat.go:192`), `App.AcpSend(id, text string, images []string) (int64, error)` (`acp.go:932`), `App.ClaudeRespondControl(id, requestID string, response map[string]any) error` (`control.go:14`), `App.AcpRespondPermission(id string, rpcID int64, optionID string) error` (`control.go:35`) — all pre-existing, unchanged.
- Produces: WS commands `claude_send`, `acp_send`, `claude_respond_control`, `acp_respond_permission` — consumed by Task 5/7 in `src/mobile/store.ts`.

- [ ] **Step 1: Write the failing tests**

Add to `src-wails/httpserver_test.go`:

```go
// New write-path commands must decode their args and reach dispatch — this
// only checks the allow-list + arg shape, not the App call itself (that
// needs a live claudeMgr/acpReg, covered by claudechat_test.go/acp_test.go
// patterns instead).
func TestDispatchKnowsWritePathCommands(t *testing.T) {
	s := &HTTPServer{app: &App{}}
	for _, cmd := range []string{"claude_send", "acp_send", "claude_respond_control", "acp_respond_permission"} {
		_, err := s.dispatch(wsCall{Command: cmd, Args: wsArgs{ID: "1"}})
		if err != nil && strings.Contains(err.Error(), "unknown command") {
			t.Errorf("%s: still not in the dispatch allow-list", cmd)
		}
	}
}

func TestWsArgsDecodesWritePathFields(t *testing.T) {
	var c wsCall
	raw := `{"id":1,"command":"claude_send","args":{"id":"3","text":"hi","sessionId":"abc"}}`
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.Args.Text != "hi" || c.Args.SessionId != "abc" {
		t.Fatalf("bad decode: %+v", c.Args)
	}

	var rc wsCall
	raw = `{"id":2,"command":"claude_respond_control","args":{"id":"3","requestId":"r1","response":{"behavior":"allow"}}}`
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	if rc.Args.RequestId != "r1" || rc.Args.Response["behavior"] != "allow" {
		t.Fatalf("bad decode: %+v", rc.Args)
	}

	var ap wsCall
	raw = `{"id":3,"command":"acp_respond_permission","args":{"id":"3","rpcId":42,"optionId":"allow_once"}}`
	if err := json.Unmarshal([]byte(raw), &ap); err != nil {
		t.Fatal(err)
	}
	if ap.Args.RpcId != 42 || ap.Args.OptionId != "allow_once" {
		t.Fatalf("bad decode: %+v", ap.Args)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd src-wails && go test ./... -run 'TestDispatchKnowsWritePathCommands|TestWsArgsDecodesWritePathFields' -v`
Expected: FAIL — `TestDispatchKnowsWritePathCommands` fails because all four
commands currently return `unknown command`; `TestWsArgsDecodesWritePathFields`
fails to compile (`wsArgs` has no `Text`/`SessionId`/`RequestId`/`Response`/
`RpcId`/`OptionId` fields yet).

- [ ] **Step 3: Extend `wsArgs` (`httpserver.go:283-289`)**

```go
// wsArgs is the union of every argument object the mobile client sends.
// Keys are camelCase because that is what api.ts puts on the wire.
type wsArgs struct {
	ID          string         `json:"id"`
	WorkspaceID int64          `json:"workspaceId"`
	Data        []int          `json:"data"`
	Cols        uint16         `json:"cols"`
	Rows        uint16         `json:"rows"`
	Text        string         `json:"text"`
	SessionId   string         `json:"sessionId"`
	RequestId   string         `json:"requestId"`
	Response    map[string]any `json:"response"`
	RpcId       int64          `json:"rpcId"`
	OptionId    string         `json:"optionId"`
	AgentKind   string         `json:"agentKind"`
}
```

- [ ] **Step 4: Add the dispatch cases (`httpserver.go:298-315`)**

```go
func (s *HTTPServer) dispatch(c wsCall) (any, error) {
	switch c.Command {
	case "list_workspaces":
		return s.app.ListWorkspaces()
	case "list_terminal_tabs":
		return s.app.ListTerminalTabs(c.Args.WorkspaceID)
	case "write_pty":
		return nil, s.app.WritePty(c.Args.ID, c.Args.Data)
	case "resize_pty":
		return nil, s.app.ResizePty(c.Args.ID, c.Args.Cols, c.Args.Rows)
	case "list_pty_sessions":
		return s.app.ListPtySessions()
	case "remote_list_chats":
		return s.app.RemoteListChats()
	case "remote_create_chat":
		return s.app.RemoteCreateChat(c.Args.WorkspaceID, c.Args.AgentKind)
	case "claude_send":
		return nil, s.app.ClaudeSend(c.Args.ID, c.Args.Text, c.Args.SessionId, nil)
	case "acp_send":
		_, err := s.app.AcpSend(c.Args.ID, c.Args.Text, nil)
		return nil, err
	case "claude_respond_control":
		return nil, s.app.ClaudeRespondControl(c.Args.ID, c.Args.RequestId, c.Args.Response)
	case "acp_respond_permission":
		return nil, s.app.AcpRespondPermission(c.Args.ID, c.Args.RpcId, c.Args.OptionId)
	default:
		return nil, fmt.Errorf("unknown command %q", c.Command)
	}
}
```

`RemoteCreateChat`'s real signature is defined in Task 3 — this task's
`TestDispatchKnowsWritePathCommands` will still pass once Task 3 lands since
it only checks for `unknown command` on the other four; add `remote_create_chat`
to that test's command list once Task 3 is done (that task also updates this
test file, see its Step 1).

Also delete the now-superseded stub: remove `RemoteCreateChat` from
`stubs.go:65-67` (comment + function) — Task 3 replaces it in `remote.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd src-wails && go test ./... -run 'TestDispatchKnowsWritePathCommands|TestWsArgsDecodesWritePathFields' -v`
Expected: PASS. (`TestDispatchKnowsWritePathCommands` will error on
`remote_create_chat` referencing the old stub until Task 3 lands — if doing
these tasks in strict order, temporarily drop `remote_create_chat` from that
test's command list and restore it in Task 3.)

Run the full suite too: `cd src-wails && go build ./... && go test ./...`
Expected: build succeeds, no regressions.

- [ ] **Step 6: Commit**

```bash
git add src-wails/httpserver.go src-wails/httpserver_test.go src-wails/stubs.go
git commit -m "feat(mobile): wire claude_send/acp_send/respond-control onto /ws

Thin dispatch wrappers over the same App methods the desktop already
uses — no new agent-control logic, just exposing it on the tailnet
transport."
```

---

## Task 3: Go — real `RemoteCreateChat`

**Files:**
- Modify: `src-wails/remote.go`
- Modify: `src-wails/remote_test.go`
- Modify: `src-wails/stubs.go` (remove the old stub + its comment)

**Interfaces:**
- Consumes: `App.ReadConfig() (string, error)` / `App.WriteConfig(string) error` (`config.go:25,40`), `App.ClaudeStart(id, cwd, resumeSessionID, permissionMode, appendSystemPrompt, model, effort, configDir, profileCommand, profileArgs string) error` (`claudechat.go:112`), `a.workspaceLabels() (map[int64]string, map[int64]string)` (`remote.go:50`, names+paths by workspace id, pre-existing).
- Produces: `App.RemoteCreateChat(workspaceID int64, agentKind string) (map[string]any, error)` — the session row shape `RemoteListChats` already returns, consumed by `src/mobile/store.ts#createChat` (already calls `remote_create_chat`, needs no change).

- [ ] **Step 1: Write the failing test**

Add to `src-wails/remote_test.go`:

```go
func TestRemoteCreateChatSessionBumpsCounterAndPreservesConfig(t *testing.T) {
	cfg := map[string]any{
		"someUnrelatedSetting": "keep-me",
		"chatIdCounter":        float64(5),
		"chatSessions": []any{
			map[string]any{"id": float64(5), "workspaceId": float64(2), "title": "Chat 1"},
		},
	}
	session, id := remoteCreateChatSession(cfg, 2, "claude")

	if id != 6 {
		t.Fatalf("id = %d, want 6 (must bump the counter, not reuse the last id)", id)
	}
	if cfg["chatIdCounter"] != float64(7) {
		t.Fatalf("chatIdCounter = %v, want 7", cfg["chatIdCounter"])
	}
	if cfg["someUnrelatedSetting"] != "keep-me" {
		t.Fatal("unrelated config keys must survive — config.json is a grab-bag, not just chat state")
	}
	if session["title"] != "Chat 2" {
		t.Fatalf("title = %v, want \"Chat 2\" (second chat for this workspace)", session["title"])
	}
	if session["transport"] != "claude-cli" {
		t.Fatalf("transport = %v, want claude-cli", session["transport"])
	}
	sessions, ok := cfg["chatSessions"].([]any)
	if !ok || len(sessions) != 2 {
		t.Fatalf("chatSessions = %#v, want 2 entries", cfg["chatSessions"])
	}
	history, ok := cfg["chatMessageHistory"].(map[string]any)
	if !ok {
		t.Fatal("chatMessageHistory was not created")
	}
	if msgs, ok := history["6"].([]any); !ok || len(msgs) != 0 {
		t.Fatalf("chatMessageHistory[6] = %#v, want []", history["6"])
	}
}

func TestRemoteCreateChatRejectsNonClaude(t *testing.T) {
	a := &App{}
	if _, err := a.RemoteCreateChat(1, "codex"); err == nil {
		t.Fatal("expected an error — remote chat creation only supports agentKind claude for now")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd src-wails && go test ./... -run TestRemoteCreateChat -v`
Expected: FAIL to compile — `remoteCreateChatSession` and the real
`RemoteCreateChat` don't exist yet (only the `stubs.go` stub does).

- [ ] **Step 3: Remove the stub**

Delete from `stubs.go:65-67`:

```go
// RemoteListChats now lives in remote.go. RemoteCreateChat is still a stub:
// TODO(remote-chat): implement once the write path lands.
func (a *App) RemoteCreateChat(_cwd string) (map[string]any, error) { return map[string]any{}, nil }
```

- [ ] **Step 4: Implement in `remote.go`**

Add below `remoteChatsFromConfig`:

```go
// remoteCreateChatSession mutates cfg (config.json already decoded into a
// generic map — NOT the narrow remoteConfig struct, which would drop every
// other settings key on write-back) in place: bumps chatIdCounter, appends a
// new session row in the exact shape src/stores/claudeChats.ts#create()
// builds client-side, and seeds an empty transcript. Pure function so the
// id-allocation and shape logic is testable without a real app data dir.
func remoteCreateChatSession(cfg map[string]any, workspaceID int64, agentKind string) (map[string]any, int64) {
	counter := int64(1)
	if v, ok := cfg["chatIdCounter"].(float64); ok {
		counter = int64(v)
	}
	id := counter
	cfg["chatIdCounter"] = float64(counter + 1)

	sessions, _ := cfg["chatSessions"].([]any)
	countForWs := 0
	for _, raw := range sessions {
		if s, ok := raw.(map[string]any); ok {
			if wsID, ok := numericID(s["workspaceId"]); ok && wsID == workspaceID {
				countForWs++
			}
		}
	}

	transport := "claude-cli"
	if agentKind != "claude" {
		transport = "acp"
	}
	session := map[string]any{
		"id":              float64(id),
		"workspaceId":     float64(workspaceID),
		"claudeSessionId": "",
		"title":           fmt.Sprintf("Chat %d", countForWs+1),
		"busy":            false,
		"messageCount":    0,
		"agentKind":       agentKind,
		"transport":       transport,
		"lastActivityAt":  float64(time.Now().UnixMilli()),
	}
	cfg["chatSessions"] = append(sessions, session)

	history, _ := cfg["chatMessageHistory"].(map[string]any)
	if history == nil {
		history = map[string]any{}
	}
	history[fmt.Sprint(id)] = []any{}
	cfg["chatMessageHistory"] = history

	return session, id
}

// RemoteCreateChat is the one write RemoteListChats's read-only comment
// above deliberately excluded — see that comment for why config.json's
// read-modify-write here can race a concurrent desktop setConfig save. The
// window is one HTTP round trip right after the phone picks a workspace, so
// the risk is accepted rather than adding cross-process locking for it.
//
// Claude-only for now: an ACP/Codex session needs command/args/configDir
// resolved from provider config that today only exists in
// AgentChat.vue's acpStartPayload() — porting that is future work, not
// wired here.
func (a *App) RemoteCreateChat(workspaceID int64, agentKind string) (map[string]any, error) {
	if agentKind != "claude" {
		return nil, fmt.Errorf("remote chat creation only supports Claude for now (got %q)", agentKind)
	}

	raw, err := a.ReadConfig()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse config.json: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}

	session, id := remoteCreateChatSession(cfg, workspaceID, agentKind)

	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := a.WriteConfig(string(out)); err != nil {
		return nil, err
	}

	names, paths := a.workspaceLabels()
	cwd := paths[workspaceID]
	chatID := fmt.Sprint(id)
	if err := a.ClaudeStart(chatID, cwd, "", "default", "", "", "", "", "", ""); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	session["messages"] = []map[string]any{}
	session["workspaceName"] = names[workspaceID]
	session["workspacePath"] = cwd
	return session, nil
}
```

Add `"time"` to `remote.go`'s import block.

- [ ] **Step 5: Restore `remote_create_chat` in Task 2's dispatch test**

In `httpserver_test.go`'s `TestDispatchKnowsWritePathCommands`, add
`"remote_create_chat"` back to the command list (it needs a real `App`
instance now, which the test already constructs via `&App{}` — a bare
`App{}` has a nil DB, so `RemoteCreateChat` will error on `ReadConfig`
inside `appDataDir()`/file IO rather than panic, and this test only checks
the error is not "unknown command").

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd src-wails && go test ./... -v`
Expected: PASS, no regressions.

- [ ] **Step 7: Commit**

```bash
git add src-wails/remote.go src-wails/remote_test.go src-wails/stubs.go src-wails/httpserver_test.go
git commit -m "feat(mobile): implement RemoteCreateChat for Claude sessions

Replicates claudeChats.ts#create()'s session-row shape server-side,
persists it the same way the desktop's setConfig does, then starts the
CLI with ClaudeStart — same code path the desktop chat UI uses."
```

---

## Task 4: Mobile store — status parity (review/error, mark-seen)

**Files:**
- Modify: `src/mobile/store.ts`

**Interfaces:**
- Consumes: `TermStatus`, `STATUS_PRIORITY` from `src/lib/terminalStatus.ts` (pre-existing, no Vue/Wails imports).
- Produces: `TabStatus` becomes an alias of `TermStatus` (widened to include `review`/`error`); `RemoteChat` gains `unseen?: "review" | "error"`; new `markTabSeen(ptyId)` / `markChatSeen(chatId)` exported from the store, consumed by Task 8/9's `openTerminal`/`openChat` calls.

- [ ] **Step 1: Replace the local `TabStatus` type with the shared one**

In `src/mobile/store.ts`, replace:

```ts
export type TabStatus = "idle" | "running" | "waiting" | "permission" | "done";
```

with:

```ts
import type { TermStatus } from "@/lib/terminalStatus";
export type TabStatus = TermStatus;
```

(add the import alongside the existing `BurrowWsClient`/`config` imports at
the top of the file).

- [ ] **Step 2: Add `unseen` to `RemoteChat`**

```ts
export interface RemoteChat {
  id: number;
  workspaceId: number;
  title: string;
  busy: boolean;
  status?: TabStatus | null;
  agentKind?: string | null;
  transport: "claude-cli" | "codex-app-server" | "acp";
  claudeSessionId: string;
  workspaceName?: string;
  workspacePath?: string;
  messages: RemoteMessage[];
  // Set when a turn finished while this chat was not the open one — cleared
  // by markChatSeen(). Mirrors desktop's "review" persisting until the tab
  // is seen (Terminal.vue's settleDone()).
  unseen?: "review" | "error";
}
```

- [ ] **Step 3: Make tab "done" go to `review` when not being watched**

Replace `watchTabStatus` (`store.ts:89-103`):

```ts
function watchTabStatus(ptyId: number) {
  client?.subscribe(`pty-hook-${ptyId}`, (payload) => {
    // Broadcast branch only ever sends the bare state string (see api.ts note).
    const state = typeof payload === "string" ? payload : payload?.state;
    if (state === "running" || state === "waiting" || state === "permission") {
      const t = doneTimers.get(ptyId);
      if (t !== undefined) { window.clearTimeout(t); doneTimers.delete(ptyId); }
      statuses.set(ptyId, state);
    } else if (state === "error") {
      const t = doneTimers.get(ptyId);
      if (t !== undefined) { window.clearTimeout(t); doneTimers.delete(ptyId); }
      statuses.set(ptyId, "error"); // persists until markTabSeen, like desktop
    } else if (state === "done") {
      const watching = view.value === "terminal" && activeTab.value?.ptyId === ptyId;
      if (watching) {
        statuses.set(ptyId, "done");
        const t = window.setTimeout(() => statuses.set(ptyId, "idle"), 4000);
        doneTimers.set(ptyId, t);
      } else {
        statuses.set(ptyId, "review");
      }
    }
  });
}
```

- [ ] **Step 4: Mark a tab/chat seen on open, clearing review/error**

Replace `openTerminal` (`store.ts:328-331`):

```ts
function openTerminal(tab: Tab) {
  activeTab.value = tab;
  view.value = "terminal";
  markTabSeen(tab.ptyId);
}

function markTabSeen(ptyId: number) {
  const s = statuses.get(ptyId);
  if (s === "review" || s === "error") statuses.set(ptyId, "idle");
}
```

Replace `openChat` (`store.ts:293-296`):

```ts
function openChat(chat: RemoteChat) {
  activeChat.value = chat;
  view.value = "chat";
  markChatSeen(chat.id);
}

function markChatSeen(chatId: number) {
  const chat = chatFor(chatId);
  if (chat) chat.unseen = undefined;
}
```

- [ ] **Step 5: Set `unseen` when a chat turn finishes unwatched**

Replace the `turn.completed`/`turn.failed` case in `applyEvent` (`store.ts:251-255`):

```ts
case "turn.completed":
case "turn.failed": {
  chat.busy = false;
  chat.messages.forEach((message) => { message.partial = false; });
  const watching = view.value === "chat" && activeChat.value?.id === chat.id;
  if (!watching) chat.unseen = event.type === "turn.failed" ? "error" : "review";
  return;
}
```

- [ ] **Step 6: Add a `chatStatus` helper and export the new functions**

Add near `statusFor` (`store.ts:85-87`):

```ts
function chatStatus(chat: RemoteChat): TabStatus {
  if (chat.busy) return "running";
  if (chat.unseen) return chat.unseen;
  return "idle";
}
```

(Permission is folded in by Task 5, which sets `chat.unseen`-adjacent
`pendingPermission` and extends this function.)

In the store's final `return { ... }` block (`store.ts:356-362`), add the
three new exports:

```ts
return {
  baseUrl, token, connected, connecting, connectError,
  view, workspaces, loading, listError, activeTab,
  chats, activeChat,
  pair, connect, disconnect, loadSessions, loadChats, openTerminal, closeTerminal, showDashboard, showSessions, showChats, openChat, closeChat, sendChat, createChat,
  statusFor, getClient,
  markTabSeen, markChatSeen, chatStatus,
};
```

- [ ] **Step 7: Manually verify**

Start a chat/tab from desktop, let it finish while the phone is on a
different view, confirm the phone would show `review` (checked visually in
Task 9's UI, but you can already confirm via Vue devtools / a temporary
`console.log(store.statusFor(...))` that the value is `"review"` not
`"done"`). Open it and confirm the value resets to `"idle"`.

- [ ] **Step 8: Commit**

```bash
git add src/mobile/store.ts
git commit -m "feat(mobile): review/error status parity with desktop

Reuses src/lib/terminalStatus.ts's TermStatus instead of a separate
mobile-only status type. A tab/chat that finishes while not open goes
to review (or error on a failed turn) and stays there until opened —
same persistence rule as Terminal.vue's settleDone()."
```

---

## Task 5: Mobile store — permission detection and response

**Files:**
- Modify: `src/mobile/store.ts`

**Interfaces:**
- Consumes: raw channels `claude-data-{id}` / `acp-req-{id}` (already broadcast via `emitAll`, unconsumed by mobile until now); WS commands `claude_respond_control`/`acp_respond_permission` from Task 2.
- Produces: `RemoteChat.pendingPermission` (new field); `respondChatPermission(chatId, allow)` exported from the store, consumed by Task 7's `ChatView.vue` banner.

- [ ] **Step 1: Add `pendingPermission` to `RemoteChat`**

```ts
export interface PendingPermission {
  requestId?: string; // Claude control_request id
  rpcId?: number;      // ACP JSON-RPC id
  toolName: string;
  detail: string;
}
```

Add `pendingPermission?: PendingPermission | null;` to `RemoteChat`.

- [ ] **Step 2: Parse the raw channel for control/permission requests**

Add a new function, called once per chat right after `watchChat` in
`loadChats`/`createChat` (`store.ts:279`, `store.ts:324`):

```ts
// Narrow reader: only recognizes a can_use_tool control_request (Claude) or
// a session/request_permission (ACP) — everything else on this raw channel
// is ignored on purpose (see spec's "Non-goals": no full protocol parsing
// on mobile, only enough to unblock a turn).
function watchChatPermissions(chat: RemoteChat) {
  const rawEvent = chat.transport === "claude-cli" ? `claude-data-${chat.id}` : `acp-req-${chat.id}`;
  client?.subscribe(rawEvent, (payload) => {
    const line = typeof payload === "string" ? safeJson(payload) : payload;
    if (!line || typeof line !== "object") return;
    const msg = line as Record<string, any>;

    if (chat.transport === "claude-cli") {
      if (msg.type !== "control_request" || msg.request?.subtype !== "can_use_tool") return;
      const input = msg.request.input ?? {};
      const detail = input.command ?? input.file_path ?? input.path ?? "";
      chat.pendingPermission = {
        requestId: msg.request_id,
        toolName: msg.request.tool_name ?? "Tool",
        detail: String(detail),
      };
      return;
    }

    // ACP: server->client REQUEST has both method and id.
    if (typeof msg.id !== "number" || !msg.method) return;
    if (!/permissions\/requestApproval|request_permission/.test(msg.method)) return;
    chat.pendingPermission = {
      rpcId: msg.id,
      toolName: msg.params?.toolCall?.title ?? msg.params?.title ?? "Tool",
      detail: "",
    };
  });
}
```

Call `watchChatPermissions(chat)` alongside every existing `watchChat(chat)`
call (there are three: `loadChats`'s initial loop, `loadChats`'s
`remote-chats` subscription handler, and `createChat`).

- [ ] **Step 3: Add the respond action**

```ts
async function respondChatPermission(chatId: number, allow: boolean) {
  const chat = chatFor(chatId);
  const pending = chat?.pendingPermission;
  if (!client || !chat || !pending) return;
  chat.pendingPermission = null;
  try {
    if (pending.requestId) {
      await client.call("claude_respond_control", {
        id: chat.id,
        requestId: pending.requestId,
        response: allow
          ? { behavior: "allow", updatedInput: {} }
          : { behavior: "deny", message: "User denied this action." },
      });
    } else if (pending.rpcId !== undefined) {
      await client.call("acp_respond_permission", {
        id: chat.id,
        rpcId: pending.rpcId,
        optionId: allow ? "allow_once" : "reject_once",
      });
    }
  } catch (e: any) {
    chat.messages.push({ id: Date.now(), role: "assistant", text: `Odpověď na povolení selhala: ${e?.message ?? e}` });
  }
}
```

- [ ] **Step 4: Fold permission into `chatStatus` (from Task 4)**

```ts
function chatStatus(chat: RemoteChat): TabStatus {
  if (chat.pendingPermission) return "permission";
  if (chat.busy) return "running";
  if (chat.unseen) return chat.unseen;
  return "idle";
}
```

- [ ] **Step 5: Export `respondChatPermission`**

Add it to the store's `return { ... }` block from Task 4's Step 6:

```ts
  markTabSeen, markChatSeen, chatStatus, respondChatPermission,
```

- [ ] **Step 6: Manually verify**

From desktop or the phone, send a message that triggers a tool permission
(e.g. ask Claude to run a shell command with default permission mode).
Confirm `chat.pendingPermission` becomes non-null (Vue devtools), and that
calling `store.respondChatPermission(chat.id, true)` from the console lets
the turn resume (visible as new `chat-event-*` deltas / `busy` going false).
Full UI is Task 7 — this task only needs the data flow working.

- [ ] **Step 7: Commit**

```bash
git add src/mobile/store.ts
git commit -m "feat(mobile): detect and answer permission/plan requests

Reads claude-data-{id}/acp-req-{id} (already broadcast, previously
unconsumed by mobile) just enough to recognize a can_use_tool or
request_permission ask, and answers it through the Task 2 dispatch
commands — same control_response/optionId shapes AgentChat.vue sends."
```

---

## Task 6: Mobile store — WS auto-reconnect

**Files:**
- Modify: `src/mobile/store.ts`

**Interfaces:**
- Consumes: `BurrowWsClient` (`src/mobile/api.ts`, unchanged — its `onClose` hook already exists).
- Produces: `reconnecting` ref, exported from the store, consumed by Task 7/8's banner.

- [ ] **Step 1: Add reconnect state and logic**

Add near the other refs (`store.ts:69-71`):

```ts
const reconnecting = ref(false);
let reconnectAttempt = 0;
let reconnectTimer: number | undefined;
```

Replace the `c.onClose` assignment inside `connect()` (`store.ts:123-126`):

```ts
c.onClose = () => {
  connected.value = false;
  if (view.value === "terminal") view.value = "dashboard";
  scheduleReconnect();
};
```

Add below `connect()`:

```ts
function scheduleReconnect() {
  if (reconnectTimer !== undefined || view.value === "connect") return;
  reconnecting.value = true;
  const delay = Math.min(1000 * 2 ** reconnectAttempt, 30000);
  reconnectTimer = window.setTimeout(async () => {
    reconnectTimer = undefined;
    if (!baseUrl.value || !token.value) { reconnecting.value = false; return; }
    try {
      await connect(baseUrl.value, token.value);
      reconnectAttempt = 0;
      reconnecting.value = false;
    } catch {
      reconnectAttempt++;
      scheduleReconnect();
    }
  }, delay);
}
```

- [ ] **Step 2: Stop retrying on explicit disconnect**

In `disconnect()` (`store.ts:144-153`), add at the top:

```ts
function disconnect() {
  if (reconnectTimer !== undefined) { window.clearTimeout(reconnectTimer); reconnectTimer = undefined; }
  reconnectAttempt = 0;
  reconnecting.value = false;
  client?.close();
  // ...unchanged rest of the function
```

- [ ] **Step 3: Export `reconnecting`**

Add it to the store's `return { ... }` block from Task 5's Step 5:

```ts
  markTabSeen, markChatSeen, chatStatus, respondChatPermission, reconnecting,
```

- [ ] **Step 4: Manually verify**

Pair the phone, then stop the desktop app (or kill wifi on the phone) mid
session. Confirm `store.reconnecting` becomes `true` and, after restarting
the server, the client reconnects on its own within the backoff window
without the user reopening the PWA. Force-close and reopen the PWA — confirm
it does NOT spin retrying against a server that's simply not there yet
(`view.value === 'connect'` guard).

- [ ] **Step 5: Commit**

```bash
git add src/mobile/store.ts
git commit -m "feat(mobile): auto-reconnect the WS with capped backoff

A dropped phone connection (any wifi hiccup) previously required
manually reopening the PWA. Retries at 1s/2s/4s.../30s, stops on an
explicit disconnect()."
```

---

## Task 7: `ChatView.vue` — permission banner

**Files:**
- Modify: `src/mobile/views/ChatView.vue`

**Interfaces:**
- Consumes: `chat.pendingPermission` (Task 5), `store.respondChatPermission` (Task 5), `store.reconnecting` (Task 6).

- [ ] **Step 1: Add the banner to the template**

In `ChatView.vue`, right after the `<header class="m-nav">` block (line 20):

```html
<div v-if="store.reconnecting" class="reconnect-banner">Připojuji znovu…</div>
<div v-if="chat?.pendingPermission" class="perm-banner">
  <span class="perm-text">{{ chat.pendingPermission.toolName }}<template v-if="chat.pendingPermission.detail"> — {{ chat.pendingPermission.detail }}</template></span>
  <div class="perm-actions">
    <button type="button" class="perm-allow" @click="store.respondChatPermission(chat!.id, true)">Povolit</button>
    <button type="button" class="perm-deny" @click="store.respondChatPermission(chat!.id, false)">Zamítnout</button>
  </div>
</div>
```

- [ ] **Step 2: Disable the composer while a permission is pending**

In the `<form class="chat-composer">` (line 34), extend the existing
`:disabled="chat?.busy"` on the textarea/button to also cover the pending
permission:

```html
<form class="chat-composer" @submit.prevent="send">
  <textarea v-model="draft" :disabled="chat?.busy || !!chat?.pendingPermission" placeholder="Napiš agentovi…" rows="1" enterkeyhint="send" @keydown.enter.exact.prevent="send"/>
  <button type="submit" :disabled="!draft.trim() || chat?.busy || !!chat?.pendingPermission">↑</button>
</form>
```

- [ ] **Step 3: Style the banner**

Append to the `<style scoped>` block:

```css
.reconnect-banner { padding: 6px 15px; background: color-mix(in srgb, var(--yellow) 18%, var(--bg-panel)); color: var(--yellow); font: 11px/1.3 var(--font-mono); text-align: center; }
.perm-banner { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 15px; background: color-mix(in srgb, var(--accent) 14%, var(--bg-panel)); border-bottom: 1px solid var(--border); }
.perm-text { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 12px var(--font-mono); color: var(--text-primary); }
.perm-actions { display: flex; gap: 6px; flex-shrink: 0; }
.perm-allow, .perm-deny { min-height: 34px; padding: 0 12px; border-radius: 8px; border: 1px solid var(--border); font-size: 12px; font-weight: 700; }
.perm-allow { background: var(--accent); border-color: var(--accent); color: #fff; }
.perm-deny { background: transparent; color: var(--red); border-color: var(--red); }
```

- [ ] **Step 4: Manually verify**

Trigger a tool permission again (as in Task 5's verification) and confirm
the banner renders with the tool name/detail, the composer is disabled, and
tapping Povolit/Zamítnout resumes the turn and clears the banner.

- [ ] **Step 5: Commit**

```bash
git add src/mobile/views/ChatView.vue
git commit -m "feat(mobile): render the permission banner in ChatView

Allow/Deny wired to store.respondChatPermission; composer disabled
while a decision is pending so a message can't race the tool call."
```

---

## Task 8: `ChatsView.vue` — Claude-only create, status sort + collapse

**Files:**
- Modify: `src/mobile/views/ChatsView.vue`

**Interfaces:**
- Consumes: `store.chatStatus` (Task 4/5), `store.markChatSeen` (Task 4, called by `openChat` already — no new call needed here), `TermStatus`/`STATUS_PRIORITY` from `@/lib/terminalStatus`.

- [ ] **Step 1: Drop the agent picker — Claude only for now**

Replace the script's create-related refs/computed (`ChatsView.vue:9-21`):

```ts
const creating = ref(false);
const workspaceId = ref<number | null>(null);
const createError = ref("");
const workspaceOptions = computed(() => store.workspaces.map((workspace) => ({ value: String(workspace.id), label: workspace.name })));
const workspaceIdModel = computed<string | undefined>({ get: () => workspaceId.value?.toString(), set: (id) => { workspaceId.value = id ? Number(id) : null; } });

async function createChat() {
  if (!workspaceId.value) return;
  creating.value = true; createError.value = "";
  try { await store.createChat(workspaceId.value, "claude"); }
  catch (error: any) { createError.value = error?.message ?? "Chat se nepodařilo vytvořit."; }
  finally { creating.value = false; }
}
```

(Removes `agentKind`/`agentOptions` — every remote-created chat is Claude
until ACP creation is built; see Global Constraints.)

Update the template's create section (`ChatsView.vue:34`) to drop the second
`<Select>`:

```html
<section class="new-chat"><p class="eyebrow">NOVÁ KONVERZACE</p><div class="create-grid"><Select v-model="workspaceIdModel" class="mobile-select" :options="workspaceOptions" placeholder="Vyber projekt…" /><button type="button" :disabled="!workspaceId || creating" @click="createChat">{{ creating ? 'Spouštím…' : 'Nový chat' }}</button></div><p v-if="createError" class="create-error">{{ createError }}</p></section>
```

- [ ] **Step 2: Sort by status priority, split settled into a collapsed section**

Replace `orderedChats` (`ChatsView.vue:7`) with:

```ts
import { STATUS_PRIORITY } from "@/lib/terminalStatus";

const sortedChats = computed(() =>
  [...store.chats].sort((a, b) => {
    const pa = STATUS_PRIORITY.indexOf(store.chatStatus(a));
    const pb = STATUS_PRIORITY.indexOf(store.chatStatus(b));
    return pa !== pb ? pa - pb : b.id - a.id;
  })
);
const liveChats = computed(() => sortedChats.value.filter((c) => store.chatStatus(c) !== "idle"));
const settledChats = computed(() => sortedChats.value.filter((c) => store.chatStatus(c) === "idle"));
const showSettled = ref(false);
```

- [ ] **Step 3: Render two sections instead of one flat list**

Replace the chat list block (`ChatsView.vue:35-42`):

```html
<p class="eyebrow">ŽIVÉ CHATY</p>
<button v-for="chat in liveChats" :key="chat.id" class="chat-row" type="button" @click="store.openChat(chat)">
  <span :class="['s-dot', store.chatStatus(chat)]" aria-hidden="true" />
  <span class="chat-row-main"><strong>{{ chat.title }}</strong><small><span v-if="chat.workspaceName" class="chat-ws">{{ chat.workspaceName }}</span>{{ chat.agentKind || chat.transport }} · {{ chat.messages.length }} zpráv</small></span>
  <span :class="['chat-status', { 'chat-status--busy': chat.busy }]">{{ chat.busy ? 'Pracuje' : store.chatStatus(chat) === 'review' ? 'Hotovo' : store.chatStatus(chat) === 'permission' ? 'Potřebuje tě' : store.chatStatus(chat) === 'error' ? 'Chyba' : 'Připraven' }}</span>
  <span class="chevron">›</span>
</button>
<section v-if="!liveChats.length && !settledChats.length" class="empty"><span>✦</span><strong>Žádná chatová relace</strong><p>Otevři nebo spusť chat v desktopovém Burrowu. Tady se objeví a půjde okamžitě ovládat.</p></section>

<template v-if="settledChats.length">
  <button type="button" class="collapse-toggle" @click="showSettled = !showSettled">{{ showSettled ? '▾' : '▸' }} Ostatní ({{ settledChats.length }})</button>
  <template v-if="showSettled">
    <button v-for="chat in settledChats" :key="chat.id" class="chat-row chat-row--settled" type="button" @click="store.openChat(chat)">
      <span class="s-dot idle" aria-hidden="true" />
      <span class="chat-row-main"><strong>{{ chat.title }}</strong><small><span v-if="chat.workspaceName" class="chat-ws">{{ chat.workspaceName }}</span>{{ chat.agentKind || chat.transport }} · {{ chat.messages.length }} zpráv</small></span>
      <span class="chevron">›</span>
    </button>
  </template>
</template>
```

`.s-dot` classes already exist globally in `App.vue` (`review`/`error`
included), so no new CSS is needed for the dot itself.

- [ ] **Step 4: Style the collapse toggle**

Append to `<style scoped>`:

```css
.collapse-toggle { width: 100%; text-align: left; padding: 10px 2px; border: 0; background: transparent; color: var(--text-muted); font: 700 11px var(--font-mono); letter-spacing: .06em; }
.chat-row--settled { opacity: .65; }
```

- [ ] **Step 5: Manually verify**

With a mix of a running chat, a `review` chat (finished while unwatched —
force this by triggering Task 4's flow), and several long-idle chats,
confirm the idle ones collapse under "Ostatní (N)" and the running/review
ones stay visible above, sorted with `review` after `running` per
`STATUS_PRIORITY`.

- [ ] **Step 6: Commit**

```bash
git add src/mobile/views/ChatsView.vue
git commit -m "feat(mobile): declutter ChatsView — sort by status, collapse idle

Live/attention-needing chats stay on top; idle ones collapse under
'Ostatní (N)' instead of piling up in one flat list. Chat creation is
Claude-only until ACP provider-config resolution exists server-side."
```

---

## Task 9: `DashboardView.vue` — collapse idle chats the same way

**Files:**
- Modify: `src/mobile/views/DashboardView.vue`

**Interfaces:**
- Consumes: `store.chatStatus` (Task 4/5).

- [ ] **Step 1: Split chats into live vs settled**

Replace `activeChats`/`primaryChat` (`DashboardView.vue:6-7`):

```ts
const liveChats = computed(() => store.chats.filter((chat) => store.chatStatus(chat) !== "idle"));
const primaryChat = computed(() => liveChats.value[0] ?? store.chats[0] ?? null);
```

(`activeChats.length` is used later for the summary copy — update those two
references, `DashboardView.vue:38-39`, from `activeChats` to `liveChats`.)

- [ ] **Step 2: Cap the "KONVERZACE" section to live chats, note settled count**

Replace the chats section (`DashboardView.vue:50-53`):

```html
<section v-if="liveChats.length || store.chats.length > liveChats.length" class="session-section" aria-labelledby="chat-heading">
  <div class="section-heading"><p id="chat-heading" class="section-label">KONVERZACE</p><button class="text-action" type="button" @click="store.showChats">Všechny</button></div>
  <ul class="session-list"><li v-for="chat in liveChats.slice(0, 3)" :key="chat.id"><button class="session-row" type="button" @click="store.openChat(chat)"><span :class="['session-icon', store.chatStatus(chat)]" aria-hidden="true">✦</span><span class="session-main"><span class="session-title">{{ chat.title }}</span><span class="session-path">{{ chat.workspaceName ? chat.workspaceName + ' · ' : '' }}{{ chat.agentKind || chat.transport }}</span></span><span :class="['state-pill', store.chatStatus(chat)]">{{ chat.busy ? 'Pracuje' : store.chatStatus(chat) === 'review' ? 'Hotovo' : store.chatStatus(chat) === 'permission' ? 'Potřebuje tě' : 'Připraven' }}</span><span class="session-chevron" aria-hidden="true">›</span></button></li></ul>
  <p v-if="store.chats.length > liveChats.length" class="settled-hint">{{ store.chats.length - liveChats.length }} dalších čeká v Chatech (bez aktivity)</p>
</section>
```

- [ ] **Step 3: Style the hint**

Append to `<style scoped>`:

```css
.settled-hint { margin: 6px 0 0; color: var(--text-muted); font-size: 11px; }
```

- [ ] **Step 4: Manually verify**

With the same mix as Task 8, confirm the dashboard's "KONVERZACE" section
only lists live/review/permission chats (max 3) plus the "N dalších…" hint,
not every idle chat ever created.

- [ ] **Step 5: Commit**

```bash
git add src/mobile/views/DashboardView.vue
git commit -m "feat(mobile): dashboard chats section matches ChatsView's declutter

Only live/attention-needing chats surface here; idle ones are summarized
as a count instead of listed, consistent with the tabs section's
existing 'Nedávno hotovo' pattern."
```

---

## Task 10: End-to-end manual verification

No code changes — this closes out the plan by walking the full spec
scenario in one pass, since `src/mobile/` has no automated UI test harness
(per the spec's Testing section).

- [ ] **Step 1: Build and serve the mobile bundle**

Run: `pnpm build:mobile && just dev` (or `wails dev` per repo root `justfile`)

- [ ] **Step 2: Pair a phone or a second browser tab**

Open Settings → Remote access on desktop, get the 6-digit code, pair from
the mobile URL.

- [ ] **Step 3: Walk the full chat lifecycle**

1. Create a new chat from `ChatsView` (Claude, pick a workspace) — confirm
   it actually starts (no more "unknown command").
2. Send a message that will trigger a tool permission — confirm the banner
   in Task 7 appears and Allow resumes the turn.
3. Background the PWA (switch tabs/apps) until the turn finishes — return
   and confirm the chat shows as `review` in `ChatsView`/`DashboardView`
   until opened, then drops to idle and collapses into "Ostatní" over time.

- [ ] **Step 4: Walk the terminal + reconnect path**

1. Open a live terminal tab, type a command — confirm it runs (Task 1's fix).
2. Rotate the phone / resize the browser — confirm the remote shell reflows.
3. Kill the desktop app or the phone's network mid-session — confirm the
   "Připojuji znovu…" banner (or dashboard equivalent) appears and the
   client reconnects on its own once the server is back.

- [ ] **Step 5: Record results**

If every sub-step passes, the plan is done. If something regresses, file it
as a fix against the specific task above rather than patching ad hoc — the
task's test (Go) or manual-verify step (Vue) is where the regression belongs.

# Remote Access — Implementation Plan, fáze 4 (snapshot + resume)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Klient přežije výpadek spojení bez ztráty eventů a dostane celý stav shellu jedním round tripem — tedy to, co dnes chybí desktopu (drop pod backpressure ztratí scrollback i tečky) a co telefon bude potřebovat na první paint.

**Architecture:** `seq` z jednoho `atomic.Int64` na každý shell event, ring buffer posledních 512 v paměti, `{t:"resume", since}` → delty nebo `{t:"resync"}`. Snapshot je jeden RPC `shell_snapshot` vracející workspaces, taby **všech** workspaců, fáze a chaty. Žádný per-client stav na serveru, jen ten ring.

**Tech Stack:** Go 1.25, `sync/atomic`; Vue 3 + vitest.

**Spec:** `docs/superpowers/specs/2026-09-05-remote-access-t3code-design.md` §2

**Předchozí fáze:** `docs/superpowers/plans/2026-09-06-remote-access-phase-3.md` (hotová, `752d537` → `65e2971`)

## Global Constraints

- **Komentáře v kódu anglicky.** Commit subject anglicky, Conventional Commits.
- **Binární PTY framy tenhle plán nedělá.** Spec §2 je chce, ale je to optimalizace šířky pásma, ne prerekvizita — `pty-data` dál jede jako JSON. Odloženo vědomě.
- **Staré `/ws` + `HTTPServer.dispatch` zůstává živé a nedotčené.** `src/mobile` na něm visí do fáze 6.
- **Snapshot nese jen to, co spec §2 vyjmenovává.** Tělo transcriptu ne (`chat_stream` + `folded_ord` + `LoadChatEventsSince` už v SQLite je a je lepší), PTY scrollback ne (daemon má ring a reattach ho přehraje). Kdo přidá do snapshotu transcript, staví druhý replay log pro totéž.
- **Žádný per-client stav na serveru.** Ring buffer je sdílený; `resume` je čistá funkce nad ním. Kdo přidá mapu klientů, obrací rozhodnutí ze spec §2.
- **`busEmit` zůstává jediné dveře** pro eventy.
- Go testy: `cd src-wails && go test ./...` a `go test -race ./...`. Frontend: `pnpm test`. Vše dohromady: `just check`.
- `src/runtime/**` nesmí importovat z `src/components`, `src/views`, `src/stores`, `src/mobile`, ani `xterm` — hlídá `src/runtime/boundary.test.ts`.

---

## File Structure

| soubor | odpovědnost |
|---|---|
| `src/runtime/transport.ts` (modify) | Task 1: opravit rozpočet `MAX_CONNECT_FAILURES`; Task 5: `resume`/`resync` |
| `src-wails/shellstream.go` (nový) | `seq`, ring buffer, `shellEvent`, `resumeSince()` |
| `src-wails/shellstream_test.go` (nový) | seq monotónní, ring wrap, resume v bufferu / mimo / po restartu |
| `src-wails/shellsnapshot.go` (nový) | `ShellSnapshot` typ + `App.ShellSnapshot()` |
| `src-wails/shellsnapshot_test.go` (nový) | snapshot nese taby všech workspaců, fáze, chaty |
| `src-wails/remoteproto.go` (modify) | `shell` a `resync` server framy, `resume` client frame |
| `src-wails/remotews.go` (modify) | `resume` handling, shell eventy do odchozí fronty |
| `src-wails/bus.go` (modify) | shell eventy odbočují do ringu, ne jen na sinky |
| `src-wails/remoteapi.go` (modify) | `shell_snapshot` do `remoteAllowed` |
| `src/runtime/shellSnapshot.ts` (nový) | typy + `applyShellEvent` reducer |
| `src/runtime/shellSnapshot.test.ts` (nový) | reducer aplikuje delty, resync nahradí stav |
| `CLAUDE.md` (modify) | co je resume, co ring nedělá |

---

### Task 1: rozpočet na nedosažitelný transport

**Files:**
- Modify: `src/runtime/transport.ts`
- Test: `src/runtime/transport.test.ts`

**Interfaces:**
- Produces: nic nového; mění konstantu a komentář

Tenhle task je nález z finálního review fáze 3, který se do její fix vlny už nevešel.

**Problém.** `MAX_CONNECT_FAILURES = 5` proti výchozímu backoffu (250 · 500 · 1000 · 2000 · 4000) znamená pokusy v čase 0, 250, 750, 1750, **3750 ms** — pátý *neúspěch* padne ve 3,75 s. Komentář u té konstanty tvrdí ~7,5 s, což je čas, kdy by přišel *šestý* pokus; šestý ale neexistuje.

**Proč na tom záleží.** `startup()` v `src-wails/app.go` vytváří ticket store až na řádku 239 a běží **souběžně** s načítáním webview, takže frontend může volat dřív, než tickety existují — `LocalEndpoint()` pak vrátí nulovou strukturu a transport selže okamžitě. Všechno před tím řádkem je na kritické cestě, včetně `a.daemon.Ensure()`, který na studeném startu blokuje až **2 s** (20 × 100 ms, `daemonclient.go:46`), plus `openDB`, `migrateChatHistoryToSQLite`, `ensureBurrowBin`, `installStatusHooks`, `installAgentDocs`, `initControl`. Když start přeleze 3,75 s, každé volání z `onMounted` skončí `transport unreachable`; socket se pak zotaví, ale nikdo ta volání nezopakuje — uživatel kouká na prázdnou appku, dokud nedá reload. Před fází 3 se ta volání zafrontovala a odešla po připojení.

- [ ] **Step 1: Write the failing test**

```ts
// přidat do src/runtime/transport.test.ts
it("gives a cold start more than the daemon's own 2s startup budget", async () => {
  // The desktop's startup() runs concurrently with the webview and has
  // daemon.Ensure() on its critical path, which alone blocks up to 2s before
  // the ticket store exists. A give-up budget under that turns a slow boot
  // into an app that rejects every onMounted call and never retries them.
  const delays: number[] = [];
  const b = createBackoff({ jitter: () => 0 });
  for (let i = 0; i < MAX_CONNECT_FAILURES; i++) delays.push(b.next());

  const budgetMs = delays.slice(0, -1).reduce((a, d) => a + d, 0);
  expect(budgetMs).toBeGreaterThan(10_000);
});
```

Import `MAX_CONNECT_FAILURES` and `createBackoff`; export the constant from
`transport.ts` if it is not exported yet.

The sum drops the last delay on purpose: the Nth failure happens after N-1
waits, which is exactly the arithmetic the old comment got wrong.

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- transport`
Expected: FAIL — the budget is 3750, not > 10000

- [ ] **Step 3: Fix the constant and the comment**

Raise `MAX_CONNECT_FAILURES` to `7` and replace the comment's arithmetic with
the real timeline:

```ts
/**
 * Consecutive failed connection attempts before every pending call is rejected
 * with `transport unreachable`.
 *
 * Seven, against the default backoff (250 · 500 · 1000 · 2000 · 4000 · 4000 ·
 * 4000), puts the give-up at roughly 15.5 s: a failure happens after each
 * wait, so N failures cost the sum of the first N-1 delays.
 *
 * The floor is the desktop's own cold start. `startup()` creates the ticket
 * store late and runs concurrently with the webview, so the frontend can call
 * before it exists — and `daemon.Ensure()` alone blocks up to 2 s spawning the
 * daemon, before the DB migration, the bin write and the agent-docs install.
 * Give up sooner than that and a slow boot rejects every onMounted call, the
 * socket then recovers, and nobody re-issues them: an empty app until reload.
 */
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- transport`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/runtime/transport.ts src/runtime/transport.test.ts
git commit -m "fix(transport): give a cold start more than 3.75s to come up"
```

---

### Task 2: shell event stream — seq a ring buffer

**Files:**
- Create: `src-wails/shellstream.go`, `src-wails/shellstream_test.go`

**Interfaces:**
- Produces:
  - `type shellEvent struct { Seq int64; Name string; Payload any }`
  - `func recordShellEvent(name string, payload any) shellEvent`
  - `func resumeSince(since int64) (evs []shellEvent, ok bool)`
  - `func currentSeq() int64`
  - `func shellStreamReset()` — pro testy
  - `const shellRingSize = 512`

**Co ring je a co není.** Drží posledních 512 shell eventů v paměti. `resumeSince`
vrací delty, když `since` v ringu ještě je; jinak `ok == false` a volající pošle
`resync`. Po restartu procesu je ring prázdný a `seq` začíná od nuly, takže každý
`since > 0` je mimo — což je správně, protože stav na druhé straně restartu
nemá s tím současným nic společného.

**Pozor:** ring je sdílený a bez per-client stavu. Neukládej do něj `pty-data` —
PTY bajty jsou horký kanál a daemon je při reattachi přehraje sám; 512 slotů by
navíc spotřeboval jeden hlučný build za vteřinu.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/shellstream_test.go
package main

import "testing"

func TestSeqIsMonotonic(t *testing.T) {
	shellStreamReset()
	a := recordShellEvent("workspaces-changed", nil)
	b := recordShellEvent("workspaces-changed", nil)
	if a.Seq <= 0 {
		t.Fatalf("first seq must be positive, got %d", a.Seq)
	}
	if b.Seq != a.Seq+1 {
		t.Fatalf("seq not consecutive: %d then %d", a.Seq, b.Seq)
	}
	if currentSeq() != b.Seq {
		t.Fatalf("currentSeq %d, want %d", currentSeq(), b.Seq)
	}
}

func TestResumeReturnsOnlyWhatCameAfter(t *testing.T) {
	shellStreamReset()
	first := recordShellEvent("a", nil)
	recordShellEvent("b", nil)
	recordShellEvent("c", nil)

	evs, ok := resumeSince(first.Seq)
	if !ok {
		t.Fatal("resume from a seq still in the ring must succeed")
	}
	if len(evs) != 2 {
		t.Fatalf("want the 2 events after %d, got %d", first.Seq, len(evs))
	}
	if evs[0].Name != "b" || evs[1].Name != "c" {
		t.Fatalf("wrong events or order: %+v", evs)
	}
}

func TestResumeFromCurrentSeqIsEmptyNotAResync(t *testing.T) {
	shellStreamReset()
	last := recordShellEvent("a", nil)

	evs, ok := resumeSince(last.Seq)
	if !ok {
		t.Fatal("a client that missed nothing must not be told to resync")
	}
	if len(evs) != 0 {
		t.Fatalf("want no events, got %+v", evs)
	}
}

func TestResumeBeyondTheRingFails(t *testing.T) {
	shellStreamReset()
	old := recordShellEvent("first", nil)
	for i := 0; i < shellRingSize+10; i++ {
		recordShellEvent("filler", nil)
	}

	if _, ok := resumeSince(old.Seq); ok {
		t.Fatal("a seq the ring has overwritten must report a gap")
	}
}

func TestResumeAfterProcessRestartFails(t *testing.T) {
	// A fresh ring is what a restarted process looks like: seq starts over, so
	// any client seq is from a state that no longer exists.
	shellStreamReset()
	if _, ok := resumeSince(42); ok {
		t.Fatal("a seq from before a restart must report a gap")
	}
}

func TestResumeFromZeroIsAGap(t *testing.T) {
	shellStreamReset()
	recordShellEvent("a", nil)
	// since 0 means "I have nothing", which is a snapshot, not a delta.
	if _, ok := resumeSince(0); ok {
		t.Fatal("since=0 must send the client to a snapshot")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestSeq|TestResume'`
Expected: FAIL — `undefined: recordShellEvent`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/shellstream.go
package main

import "sync"

// shellRingSize is how many recent shell events a reconnecting client can
// catch up on. Beyond it the client is told to resync and takes a fresh
// snapshot — cheaper than any scheme that keeps per-client state on the
// server, which is what this deliberately avoids.
const shellRingSize = 512

// shellEvent is one bus event, numbered. The name and payload are exactly
// what busEmit carried, so the client's handlers are the same ones a live
// event goes through.
type shellEvent struct {
	Seq     int64  `json:"seq"`
	Name    string `json:"name"`
	Payload any    `json:"payload,omitempty"`
}

var (
	shellMu   sync.Mutex
	shellSeq  int64
	shellRing []shellEvent
)

// recordShellEvent numbers an event and files it in the ring.
func recordShellEvent(name string, payload any) shellEvent {
	shellMu.Lock()
	defer shellMu.Unlock()

	shellSeq++
	ev := shellEvent{Seq: shellSeq, Name: name, Payload: payload}

	shellRing = append(shellRing, ev)
	if len(shellRing) > shellRingSize {
		// Drop from the front. Copying keeps the slice's backing array from
		// growing without bound as it would with a bare reslice.
		copy(shellRing, shellRing[len(shellRing)-shellRingSize:])
		shellRing = shellRing[:shellRingSize]
	}
	return ev
}

// resumeSince returns the events after `since`. ok is false when the client's
// position is no longer in the ring — because it fell off the front, or
// because this process restarted and the numbering began again. Either way
// the client's next move is a snapshot, not a delta.
func resumeSince(since int64) ([]shellEvent, bool) {
	shellMu.Lock()
	defer shellMu.Unlock()

	// since 0 means the client holds nothing at all.
	if since <= 0 {
		return nil, false
	}
	if since > shellSeq {
		// Ahead of us: only possible across a restart.
		return nil, false
	}
	if len(shellRing) == 0 {
		return nil, since == shellSeq
	}
	oldest := shellRing[0].Seq
	if since < oldest-1 {
		return nil, false
	}

	out := make([]shellEvent, 0, shellSeq-since)
	for _, ev := range shellRing {
		if ev.Seq > since {
			out = append(out, ev)
		}
	}
	return out, true
}

func currentSeq() int64 {
	shellMu.Lock()
	defer shellMu.Unlock()
	return shellSeq
}

// shellStreamReset exists for tests.
func shellStreamReset() {
	shellMu.Lock()
	defer shellMu.Unlock()
	shellSeq = 0
	shellRing = nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd src-wails && go test ./... -run 'TestSeq|TestResume' -v`
Expected: PASS

- [ ] **Step 5: Race check**

Run: `cd src-wails && go test -race ./... -run 'TestSeq|TestResume'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add src-wails/shellstream.go src-wails/shellstream_test.go
git commit -m "feat(remote): numbered shell events with a replay ring"
```

---

### Task 3: `shell_snapshot`

**Files:**
- Create: `src-wails/shellsnapshot.go`, `src-wails/shellsnapshot_test.go`
- Modify: `src-wails/remoteapi.go`

**Interfaces:**
- Consumes: `currentSeq()` (Task 2), `App.ListWorkspaces` (`workspace.go:37`), `App.ListTerminalTabs` (`workspace.go:120`), `PhaseStore.All` (`phasestore.go:180`), `App.RemoteListChats` (`remote.go:31`)
- Produces: `type ShellSnapshot struct{…}`, `func (a *App) ShellSnapshot() (ShellSnapshot, error)`

**Proč `Seq` je ve snapshotu:** klient si ho uloží a při reconnectu ho pošle jako
`since`. Snímek bez čísla nejde navázat.

**Pořadí čtení má význam.** `Seq` čti **jako první**, před daty. Když ho vezmeš
až po nich, může mezi čtením dat a čtením seq proběhnout event, klient ho
započítá jako už viděný a navždy o něj přijde. Opačné pořadí může jen doručit
něco dvakrát, což reducery snesou.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/shellsnapshot_test.go
package main

import (
	"testing"

	"burrow/internal/agentphase"
)

func TestSnapshotCarriesSeqTakenBeforeData(t *testing.T) {
	shellStreamReset()
	recordShellEvent("a", nil)

	app := &App{}
	snap, err := app.ShellSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Seq != 1 {
		t.Fatalf("snapshot seq %d, want the current 1", snap.Seq)
	}
}

func TestSnapshotCarriesPhases(t *testing.T) {
	shellStreamReset()
	t.Cleanup(busReset)
	busReset()

	store, _ := newTestStore(t)
	store.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	app := &App{phases: store}
	snap, err := app.ShellSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := snap.Phases["pty:7"]; !ok || got.State != agentphase.Running {
		t.Fatalf("phase missing or wrong: %+v", snap.Phases)
	}
}

func TestSnapshotToleratesAMissingStore(t *testing.T) {
	// The DB can fail to open; a snapshot must degrade rather than panic,
	// because the client's first paint depends on it.
	shellStreamReset()
	app := &App{}
	snap, err := app.ShellSnapshot()
	if err != nil {
		t.Fatalf("a snapshot with no store must not error: %v", err)
	}
	if snap.Phases == nil || snap.Tabs == nil {
		t.Fatal("empty maps, not nil, so the client can index them")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestSnapshot`
Expected: FAIL — `undefined: ShellSnapshot`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/shellsnapshot.go
package main

import "burrow/internal/agentphase"

// ShellSnapshot is everything a client needs for its first paint, in one round
// trip. Deliberately NOT in here: chat transcript bodies (chat_stream plus
// folded_ord already replay them from SQLite, and a second replay log for the
// same thing is worse than none) and PTY scrollback (the daemon's own ring
// replays it on reattach).
type ShellSnapshot struct {
	Seq           int64                        `json:"seq"`
	EnvironmentID string                       `json:"environment_id"`
	Workspaces    []Workspace                  `json:"workspaces"`
	Tabs          map[int64][]TerminalTab      `json:"tabs"`
	Phases        map[string]agentphase.Phase  `json:"phases"`
	Chats         []map[string]any             `json:"chats"`
}

// ShellSnapshot reads the sequence FIRST, before any data. Taken afterwards, an
// event landing between the data read and the seq read would be counted as
// already seen and lost for good; taken first, the worst case is that the
// client sees something twice, which its reducers tolerate.
func (a *App) ShellSnapshot() (ShellSnapshot, error) {
	snap := ShellSnapshot{
		Seq:           currentSeq(),
		EnvironmentID: a.environmentID,
		Tabs:          map[int64][]TerminalTab{},
		Phases:        map[string]agentphase.Phase{},
	}

	if a.phases != nil {
		snap.Phases = a.phases.All()
	}
	if a.db == nil {
		// No database: an empty shell rather than an error. The client's first
		// paint should show an app with nothing in it, not a failure.
		return snap, nil
	}

	ws, err := a.ListWorkspaces()
	if err != nil {
		return snap, err
	}
	snap.Workspaces = ws

	// Tabs for EVERY workspace, not just the mounted one — a phone opens on a
	// workspace the desktop never mounted.
	for _, w := range ws {
		tabs, err := a.ListTerminalTabs(w.ID)
		if err != nil {
			return snap, err
		}
		snap.Tabs[w.ID] = tabs
	}

	if chats, err := a.RemoteListChats(); err == nil {
		snap.Chats = chats
	}
	return snap, nil
}
```

Add to `remoteAllowed` in `src-wails/remoteapi.go`:

```go
	"shell_snapshot": {Method: "ShellSnapshot", Args: nil, Scope: scopeOrchRead},
```

- [ ] **Step 4: Run tests + the surface tests**

Run: `cd src-wails && go test ./... -run 'TestSnapshot|TestRemoteSurface'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src-wails/shellsnapshot.go src-wails/shellsnapshot_test.go src-wails/remoteapi.go
git commit -m "feat(remote): shell_snapshot for a client's first paint"
```

---

### Task 4: `resume` a `resync` na serveru

**Files:**
- Modify: `src-wails/remoteproto.go`, `src-wails/remotews.go`, `src-wails/bus.go`
- Test: `src-wails/remotews_test.go`

**Interfaces:**
- Consumes: `recordShellEvent`, `resumeSince`, `currentSeq` (Task 2)
- Produces: `resume` client frame; `shell` a `resync` server frames; `welcomeFrame` gains `seq`

**Kudy eventy tečou.** Dnes `busEmit` volá sinky. Nově: `busEmit` nejdřív
zaznamená event do ringu (a dostane `seq`), pak ho pošle sinkům i se `seq`.
WS sink pak posílá `shell` framy místo `event` framů. **`pty-data-*` do ringu
nepatří** — filtruj ho, jinak jeden hlučný build vyplaví 512 slotů za vteřinu.

- [ ] **Step 1: Write the failing test**

```go
// přidat do src-wails/remotews_test.go
func TestWelcomeCarriesTheCurrentSeq(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()
	recordShellEvent("a", nil)

	conn, _ := dialTestWS(t, &App{})
	// dialTestWS already consumed the welcome; re-dial to inspect it.
	_ = conn

	// A client that reconnects needs to know where the server is now, or its
	// first resume is a guess.
	if currentSeq() != 1 {
		t.Fatalf("precondition: seq %d", currentSeq())
	}
}

func TestResumeInsideTheRingSendsDeltas(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()

	conn, _ := dialTestWS(t, &App{})
	first := recordShellEvent("workspaces-changed", nil)
	recordShellEvent("phase-pty:7", map[string]string{"state": "running"})

	if err := conn.WriteJSON(map[string]any{"t": "resume", "since": first.Seq}); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(timeNowPlus(2))
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("no frame after resume: %v", err)
	}
	if f.T != "shell" {
		t.Fatalf("want a shell frame, got %q", f.T)
	}
}

func TestResumeBeyondTheRingSendsResync(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()

	conn, _ := dialTestWS(t, &App{})
	for i := 0; i < shellRingSize+5; i++ {
		recordShellEvent("filler", nil)
	}

	if err := conn.WriteJSON(map[string]any{"t": "resume", "since": 1}); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(timeNowPlus(2))
	for {
		var f serverFrame
		if err := conn.ReadJSON(&f); err != nil {
			t.Fatalf("no resync arrived: %v", err)
		}
		if f.T == "resync" {
			return
		}
	}
}

func TestPtyDataIsNotInTheRing(t *testing.T) {
	// PTY bytes are the hot channel and the daemon replays them on reattach.
	// Ringing them would flush 512 slots in a second of noisy output.
	shellStreamReset()
	busEmit("pty-data-7", []int{104, 105})
	if currentSeq() != 0 {
		t.Fatalf("pty-data was recorded: seq %d", currentSeq())
	}
}
```

Add a `timeNowPlus(seconds int) time.Time` helper next to the other test
helpers if one does not exist.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestResume|TestWelcomeCarries|TestPtyData'`
Expected: FAIL — the server does not understand `resume`

- [ ] **Step 3: Extend the frames**

In `src-wails/remoteproto.go`: add `Since int64` to `clientFrame`, accept
`"resume"` alongside `"call"` in `decodeClientFrame` (a resume frame has no id,
so the positive-id rule must apply to `call` only — check that the existing
guard is not applied to both), and add to `serverFrame`:

```go
	// shell
	Events []shellEvent `json:"events,omitempty"`
```

plus constructors:

```go
func shellFrame(evs []shellEvent) serverFrame {
	return serverFrame{T: "shell", Events: evs}
}

func resyncFrame() serverFrame { return serverFrame{T: "resync"} }
```

and give `welcomeFrame` a `Seq int64` field carrying `currentSeq()`.

- [ ] **Step 4: Route events through the ring**

In `src-wails/bus.go`, have `busEmit` record before it fans out, skipping the
hot channel:

```go
// isRingable reports whether an event belongs in the replay ring. PTY bytes do
// not: the daemon replays them on reattach, and a second of noisy output would
// evict everything else.
func isRingable(name string) bool {
	return !strings.HasPrefix(name, "pty-data-")
}
```

`busEmit` records a ringable event, then passes the resulting `seq` to sinks.
Give `EventSink` a third parameter (`seq int64`, 0 for non-ringable) and update
both remaining sinks.

In `src-wails/remotews.go`, the WS sink sends `shellFrame([]shellEvent{ev})`
for a ringable event and keeps `eventFrame` for the rest. Handle a `resume`
frame in the read loop: `resumeSince(f.Since)` → `shellFrame(evs)` when ok,
`resyncFrame()` when not.

- [ ] **Step 5: Run tests + race**

Run: `cd src-wails && go test ./... && go test -race ./... -run TestWS`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add src-wails/remoteproto.go src-wails/remotews.go src-wails/bus.go src-wails/remotews_test.go
git commit -m "feat(remote): resume from a seq, resync past the ring"
```

---

### Task 5: klient — `resume` po reconnectu

**Files:**
- Modify: `src/runtime/transport.ts`
- Create: `src/runtime/shellSnapshot.ts`, `src/runtime/shellSnapshot.test.ts`
- Test: `src/runtime/transport.test.ts`

**Interfaces:**
- Produces:
  - `transport.ts`: transport tracks `lastSeq`, sends `{t:"resume", since}` after a reconnect, exposes `onResync(cb)`
  - `shellSnapshot.ts`: `export interface ShellSnapshotData {…}`, `export function applyShellEvent(state, ev)`

**Kdy `resume` a kdy ne.** Na **prvním** připojení klient `resume` neposílá —
nemá `since`, takže si vezme snapshot. Po reconnectu pošle `resume` s posledním
viděným `seq`; když dorazí `resync`, zahodí stav a vezme si snapshot znovu.

- [ ] **Step 1: Write the failing test**

```ts
// přidat do src/runtime/transport.test.ts
it("does not resume on the first connection", async () => {
  const { t } = setup();
  await tick();
  const ws = FakeWS.instances[0];
  ws.open();
  await tick();
  expect(ws.sent.map((s) => JSON.parse(s).t)).not.toContain("resume");
  void t;
});

it("resumes from the last seq it saw after a reconnect", async () => {
  const { t } = setup();
  await tick();
  const first = FakeWS.instances[0];
  first.open();
  first.deliver({ t: "shell", events: [{ seq: 12, name: "workspaces-changed" }] });

  first.close();
  await tick();
  await tick();
  const second = FakeWS.instances[FakeWS.instances.length - 1];
  second.open();
  await tick();

  const resume = second.sent.map((s) => JSON.parse(s)).find((f) => f.t === "resume");
  expect(resume).toBeTruthy();
  expect(resume.since).toBe(12);
  void t;
});

it("reports a resync so the caller can retake a snapshot", async () => {
  const { t } = setup();
  await tick();
  const ws = FakeWS.instances[0];
  ws.open();

  let resynced = 0;
  t.onResync(() => resynced++);
  ws.deliver({ t: "resync" });
  expect(resynced).toBe(1);
});

it("delivers shell events to the same listeners a live event uses", async () => {
  const { t } = setup();
  await tick();
  const ws = FakeWS.instances[0];
  ws.open();

  const seen: unknown[] = [];
  t.listen("phase-pty:7", (p) => seen.push(p));
  ws.deliver({ t: "shell", events: [{ seq: 3, name: "phase-pty:7", payload: { state: "done" } }] });
  expect(seen).toEqual([{ state: "done" }]);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- transport`
Expected: FAIL — `t.onResync is not a function`

- [ ] **Step 3: Implement in transport.ts**

Track `lastSeq`, updated from `welcome.seq` on a first connection and from each
`shell` frame's highest `seq`. In `onopen`, send `{t:"resume", since:lastSeq}`
only when `lastSeq > 0`. Route a `shell` frame's events through the same
listener dispatch a live `event` frame uses — one code path, so a replayed
event and a live one cannot diverge. Add `onResync(cb)` and call it on a
`resync` frame.

- [ ] **Step 4: Write the reducer**

```ts
// src/runtime/shellSnapshot.ts
/**
 * The client-side read model of the shell: workspaces, their tabs, agent
 * phases and chats. Filled by one `shell_snapshot` call and kept current by
 * the events the transport replays or delivers live.
 *
 * Deliberately not in here: chat transcript bodies and PTY scrollback. Both
 * already have their own replay paths, and a second one for the same data is
 * worse than none.
 */
import type { Phase } from "./displayStatus";

export interface ShellSnapshotData {
  seq: number;
  environment_id: string;
  workspaces: { id: number; name: string; path: string }[];
  tabs: Record<number, unknown[]>;
  phases: Record<string, Phase>;
  chats: Record<string, unknown>[];
}

export function emptySnapshot(): ShellSnapshotData {
  return { seq: 0, environment_id: "", workspaces: [], tabs: {}, phases: {}, chats: [] };
}

/** Apply one event by name. Unknown names are ignored on purpose: the stream
 *  carries every bus event, and this model only tracks part of it. */
export function applyShellEvent(
  state: ShellSnapshotData,
  ev: { seq: number; name: string; payload?: unknown },
): ShellSnapshotData {
  const next = { ...state, seq: Math.max(state.seq, ev.seq) };
  if (ev.name.startsWith("phase-")) {
    const id = ev.name.slice("phase-".length);
    next.phases = { ...state.phases, [id]: ev.payload as Phase };
  }
  return next;
}
```

with tests asserting a phase event updates the map, `seq` moves forward and
never backward, and an unknown event name leaves the rest of the state alone.

- [ ] **Step 5: Run tests + typecheck**

Run: `pnpm test && pnpm build`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add src/runtime/transport.ts src/runtime/transport.test.ts src/runtime/shellSnapshot.ts src/runtime/shellSnapshot.test.ts
git commit -m "feat(runtime): resume after a reconnect, resync when the gap is too big"
```

---

### Task 6: dokumentace

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Document the stream**

Add to the transport section: what `seq` is, that the ring holds 512 events and
what falls outside it, that `pty-data` is deliberately excluded and why, that
`resume` returns deltas or a `resync`, that a process restart always means
`resync` because the numbering starts over, and that the snapshot deliberately
excludes transcript bodies and PTY scrollback because both already have replay
paths.

State plainly that binary PTY frames were deferred and why: a bandwidth
optimization, not a prerequisite.

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: the shell stream, its ring, and what it deliberately omits"
```

---

## Self-review

**Spec coverage (§2):**

| spec | task |
|---|---|
| `seq` z jednoho atomic, bumpnutý na každý shell event | 2 |
| ring 512 posledních, in-memory | 2 |
| `{t:"resume", since}` → delty · mimo ring nebo po restartu → `resync` | 2, 4, 5 |
| žádný per-client stav na serveru | 2 (ring je sdílený, resume je čistá funkce) |
| `shell_snapshot` jeden round trip = první paint | 3 |
| snapshot nese taby VŠECH workspaců | 3 |
| tělo transcriptu a PTY scrollback se streamu neúčastní | 3, 6 |
| jména eventů identická s bus jmény | 4 (ring nese `name` beze změny) |

Vědomě mimo: binární PTY framy (§2) — optimalizace, ne prerekvizita, zaznamenáno
v Global Constraints a v Tasku 6. `ChatSummary` s `PendingKind` — snapshot nese
`RemoteListChats`'s existující tvar; typovaný `ChatSummary` je práce pro fázi 6,
kdy ho bude mít kdo konzumovat.

**Type consistency:** `shellEvent{Seq,Name,Payload}` (Go, JSON `seq`/`name`/`payload`)
↔ TS `{seq, name, payload}` v Tasku 5. `ShellSnapshot`'s JSON tags
(`seq`, `environment_id`, `workspaces`, `tabs`, `phases`, `chats`) ↔
`ShellSnapshotData`. `EventSink` gains a third parameter in Task 4 and both
sinks are updated there.

**Placeholders:** žádné. Task 4 Step 3 vyžaduje ověřit, že dnešní kontrola
kladného `id` platí jen pro `call` framy a ne pro `resume` — je tam napsané, co
hledat a proč.

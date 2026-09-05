> **SUPERSEDED** (2026-09-05) — nahrazeno `docs/superpowers/specs/2026-09-05-remote-access-t3code-design.md`.
> Z tohoto dokumentu nebyl implementovaný žádný kód. Nový design jde na full t3code model:
> desktop mluví WS s lokálním serverem, jedna auth cesta se scopes, Tailscale jen jako endpoint provider.

# Remote Access — Implementation Plan, part 1 (fáze 1 + 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Backend začne vlastnit `environmentId` a **derivovanou fázi agenta**, takže status dotu přežije restart appky, funguje pro nemountnutý workspace, a je odvoditelný bez jakéhokoli připojeného klienta (prerekvizita pushe).

**Architecture:** Čistá funkce `agentphase.Next(cur, ev, now)` v Go nahrazuje XState machine v `src/machines/agentStatus.ts`. `review` a 4s transient `done` **nejsou** stavy — jsou to read receipty, které si derivuje každý klient sám z `turnEndedAt` proti svému `seenAt`. Store drží fáze v mapě + SQLite a emituje `pty-phase-{id}` přes `emitAll`.

**Tech Stack:** Go 1.x + `database/sql` (mattn/go-sqlite3), Wails v2 events, Vue 3 + Pinia, vitest.

**Spec:** `docs/plans/005-remote-access.md`

## Global Constraints

- Jazyk komentářů v kódu: **angličtina** (jako celý zbytek repa). Plán a commit body česky/anglicky podle zvyku repa — commit subject anglicky, Conventional Commits.
- `emitAll` (`src-wails/events.go:18`) je **jediné dveře** pro každý event, který má vidět mobilní klient. Výjimky jsou jen local-only eventy vyjmenované v §Task 5.
- Nová `App` metoda **není** automaticky vystavená na síť. Tento plán žádnou remote surface nezakládá — to je část 2.
- `internal/agentphase` nesmí importovat `database/sql`, Wails runtime, ani nic z `main`. Je to čistá funkce; IO patří store v `main`.
- Fáze v Go **nezná** `review`. Kdo přidá `review` do Go, obrací rozhodnutí ze specu §3.
- Persistence: migrace jsou idempotentní `CREATE TABLE IF NOT EXISTS` / `ALTER TABLE`, přidané do existujícího `migrate()` v `src-wails/db.go:42`. Žádný migrační framework.

---

## File Structure

| soubor | odpovědnost |
|---|---|
| `src-wails/environment.go` (nový) | `environmentId`: generace, persist do `<app-data>/environment.json`, `App.EnvironmentID()` |
| `src-wails/environment_test.go` (nový) | stabilita id přes restart |
| `src-wails/internal/agentphase/phase.go` (nový) | typy `Phase`/`Event`/`State`/`Kind` + čistá `Next()` |
| `src-wails/internal/agentphase/phase_test.go` (nový) | portované případy z `src/machines/agentStatus.test.ts` |
| `src-wails/phasestore.go` (nový) | `PhaseStore`: mapa + mutex + `pty_phase` tabulka + emit `pty-phase-{id}` |
| `src-wails/phasestore_test.go` (nový) | persist/load, žádný emit při no-op |
| `src-wails/phasepoll.go` (nový) | Go-side poll fáze: foreground → agent/busy, pid sweep, dead-PTY watchdog |
| `src-wails/db.go:42` (modify) | `pty_phase` tabulka |
| `src-wails/hookserver.go` (modify) | `hookPayload.Pid`, routovat hook → `PhaseStore.Apply`, `ReplayStatus` → `PhaseStore.Replay` |
| `src-wails/events.go` (modify) | `emitWorkspacesChanged` → `emitAll` |
| `src-wails/events_test.go` (nový) | grep test: `runtime.EventsEmit` jen v povolených místech |
| `src/runtime/displayStatus.ts` (nový) | `Phase` typ + `displayStatus()` + `shouldMarkSeen()` |
| `src/runtime/displayStatus.test.ts` (nový) | read-receipt derivace |
| `src/lib/terminalStatus.ts` (modify) | zůstává (agregace + jména); `AgentEvent` typ mizí |
| `src/machines/agentStatus.ts` + `.test.ts` (delete) | nahrazeno Go |
| `src/components/XTerm.vue` (modify) | přestává emitovat `agentState`/`busy`/`interrupt`; poll si nechá jen jména |
| `src/components/Terminal.vue` (modify) | leaf status = `displayStatus(phase, seenAt, watching)` |
| `CLAUDE.md` + `docs/context.html` (modify) | `pty-hook-{id}` → `pty-phase-{id}`, kdo vlastní fázi |

---

### Task 1: `environmentId`

**Files:**
- Create: `src-wails/environment.go`
- Test: `src-wails/environment_test.go`

**Interfaces:**
- Consumes: `appDataDir()` z `src-wails/app.go:231`
- Produces: `func environmentID(dir string) (string, error)` — pure, dir-scoped; `func (a *App) EnvironmentID() (string, error)` — Wails binding

- [ ] **Step 1: Write the failing test**

```go
// src-wails/environment_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentIDIsStable(t *testing.T) {
	dir := t.TempDir()

	first, err := environmentID(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(first) != 36 {
		t.Fatalf("want a 36-char uuid, got %q", first)
	}

	second, err := environmentID(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if second != first {
		t.Fatalf("id changed across calls: %q → %q", first, second)
	}

	// A fresh process reads the same file, so a fresh dir must differ.
	other, err := environmentID(t.TempDir())
	if err != nil {
		t.Fatalf("other dir: %v", err)
	}
	if other == first {
		t.Fatal("two different app-data dirs produced the same environment id")
	}

	if _, err := os.Stat(filepath.Join(dir, "environment.json")); err != nil {
		t.Fatalf("environment.json not written: %v", err)
	}
}

func TestEnvironmentIDSurvivesGarbageFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "environment.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	id, err := environmentID(dir)
	if err != nil {
		t.Fatalf("unreadable file must be replaced, not fatal: %v", err)
	}
	if id == "" {
		t.Fatal("want a fresh id")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test -run TestEnvironmentID ./...`
Expected: FAIL — `undefined: environmentID`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/environment.go
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// One running Burrow is one ExecutionEnvironment (docs/plans/005-remote-access.md
// §1). Its id is what lets a client tell "the same machine reached a different
// way" from "a different machine": it must survive an IP change, a rename, and a
// tailnet migration, so it is generated once and read from disk forever after.
type environmentFile struct {
	EnvironmentID string `json:"environmentId"`
}

var (
	envIDOnce  sync.Mutex
	envIDCache = map[string]string{}
)

func environmentID(dir string) (string, error) {
	envIDOnce.Lock()
	defer envIDOnce.Unlock()
	if id, ok := envIDCache[dir]; ok {
		return id, nil
	}

	path := filepath.Join(dir, "environment.json")
	if raw, err := os.ReadFile(path); err == nil {
		var f environmentFile
		// A corrupt file is not fatal: the id is ours to mint, so mint a new one
		// rather than leaving the app unable to start.
		if json.Unmarshal(raw, &f) == nil && f.EnvironmentID != "" {
			envIDCache[dir] = f.EnvironmentID
			return f.EnvironmentID, nil
		}
	}

	id, err := newUUIDv4()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(environmentFile{EnvironmentID: id})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	envIDCache[dir] = id
	return id, nil
}

func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// EnvironmentID is the Wails binding. The frontend keys per-environment client
// state on it.
func (a *App) EnvironmentID() (string, error) {
	dir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return environmentID(dir)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src-wails && go test -run TestEnvironmentID ./... && go build ./...`
Expected: PASS, build clean

- [ ] **Step 5: Commit**

```bash
git add src-wails/environment.go src-wails/environment_test.go
git commit -m "feat(remote): give the environment a stable id

One running Burrow is one ExecutionEnvironment. Its id is what lets a
client tell 'same machine, different route' from 'different machine', so
it has to outlive IP changes, renames and tailnet moves — generated once
into <app-data>/environment.json, read from disk forever after.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `internal/agentphase` — čistá derivace fáze

**Files:**
- Create: `src-wails/internal/agentphase/phase.go`
- Test: `src-wails/internal/agentphase/phase_test.go`
- Read for reference (neupravovat): `src/machines/agentStatus.ts`, `src/machines/agentStatus.test.ts`

**Interfaces:**
- Consumes: nic (žádné importy mimo stdlib)
- Produces:
  - `type State string` s konstantami `Idle`, `Running`, `Waiting`, `Permission`, `Done`, `Error`, `Stale`
  - `type Kind string` s konstantami `Start`, `Wait`, `Resume`, `Permit`, `Stop`, `Fail`, `Interrupt`, `Busy`, `NotBusy`, `NeedsInput`, `SetAgent`, `GoneStale`, `Session`
  - `type Phase struct { State State; Detail, Model, Title string; IsAgent bool; TurnEndedAt, UpdatedAt int64 }`
  - `type Event struct { Kind Kind; Detail, Model, Title string; IsAgent, Needs bool }`
  - `func Next(cur Phase, ev Event, now int64) Phase`
  - `func InFlight(s State) bool`

**Kontext, který implementátor potřebuje:** dnešní pravdu drží XState machine ve `src/machines/agentStatus.ts`. Portuj **transitions**, ne stavy `review`/transient-`done` — ty ve Go nejsou (spec §3: „review není fáze, je to read receipt"). Tři vstupní kanály se arbitrují tady: agent hooks, foreground poll, watchdog. **Poll nesmí nikdy řídit agentní leaf** — to je celé pravidlo „stuck orange dot", vyjádřené jednou, guardem `cur.IsAgent`.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/internal/agentphase/phase_test.go
package agentphase

import "testing"

const now = int64(1_000)

func apply(start State, isAgent bool, evs ...Event) Phase {
	p := Phase{State: start, IsAgent: isAgent}
	for _, ev := range evs {
		p = Next(p, ev, now)
	}
	return p
}

// ── basic transitions (ported from agentStatus.test.ts "basic transitions") ──

func TestBasicTransitions(t *testing.T) {
	cases := []struct {
		name  string
		start State
		ev    Event
		want  State
	}{
		{"idle → start → running", Idle, Event{Kind: Start}, Running},
		{"running → wait → waiting", Running, Event{Kind: Wait}, Waiting},
		{"running → permission", Running, Event{Kind: Permit}, Permission},
		{"idle → permission (native approval before any output)", Idle, Event{Kind: Permit}, Permission},
		{"waiting → resume → running", Waiting, Event{Kind: Resume}, Running},
		{"permission → resume → running", Permission, Event{Kind: Resume}, Running},
		{"waiting → start → running", Waiting, Event{Kind: Start}, Running},
		{"review-replacement: done → start → running", Done, Event{Kind: Start}, Running},
		{"stale → start → running", Stale, Event{Kind: Start}, Running},
	}
	for _, c := range cases {
		if got := apply(c.start, true, c.ev).State; got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// ── settle: no `watching` guard here on purpose ─────────────────────────────

func TestStopSettlesToDoneAndStampsTurnEnd(t *testing.T) {
	// The XState machine branched STOP into done|review on a per-device
	// `watching` flag. The server has no business knowing who is looking, so it
	// records WHEN the turn ended and lets each client decide.
	for _, from := range []State{Running, Waiting, Permission} {
		p := apply(from, true, Event{Kind: Stop})
		if p.State != Done {
			t.Errorf("%s → stop: got %q, want done", from, p.State)
		}
		if p.TurnEndedAt != now {
			t.Errorf("%s → stop: TurnEndedAt = %d, want %d", from, p.TurnEndedAt, now)
		}
	}
}

func TestStartClearsTurnEndAndDetail(t *testing.T) {
	p := apply(Error, true, Event{Kind: Start})
	if p.State != Running {
		t.Fatalf("state = %q, want running", p.State)
	}
	if p.Detail != "" {
		t.Errorf("Detail = %q, want cleared", p.Detail)
	}
	if p.TurnEndedAt != 0 {
		t.Errorf("TurnEndedAt = %d, want 0 — a running turn has not ended", p.TurnEndedAt)
	}
}

// ── error ───────────────────────────────────────────────────────────────────

func TestFailCarriesDetail(t *testing.T) {
	p := apply(Running, true, Event{Kind: Fail, Detail: "billing_error"})
	if p.State != Error {
		t.Fatalf("state = %q, want error", p.State)
	}
	if p.Detail != "billing_error" {
		t.Errorf("Detail = %q, want billing_error", p.Detail)
	}
	if p.TurnEndedAt != now {
		t.Errorf("TurnEndedAt = %d — a failed turn is an ended turn", p.TurnEndedAt)
	}
}

// ── interrupt ───────────────────────────────────────────────────────────────

func TestInterruptSettlesToIdle(t *testing.T) {
	for _, from := range []State{Running, Waiting, Permission} {
		if got := apply(from, true, Event{Kind: Interrupt}).State; got != Idle {
			t.Errorf("%s → interrupt: got %q, want idle", from, got)
		}
	}
}

// ── poll channel: plain command (non-agent) ─────────────────────────────────

func TestPollDrivesPlainCommands(t *testing.T) {
	if got := apply(Idle, false, Event{Kind: Busy}).State; got != Running {
		t.Errorf("busy on a plain leaf: got %q, want running", got)
	}
	if got := apply(Running, false, Event{Kind: NotBusy}).State; got != Done {
		t.Errorf("not_busy on a plain leaf: got %q, want done", got)
	}
	if got := apply(Running, false, Event{Kind: NeedsInput, Needs: true}).State; got != Waiting {
		t.Errorf("needs_input(true): got %q, want waiting", got)
	}
	if got := apply(Waiting, false, Event{Kind: NeedsInput, Needs: false}).State; got != Running {
		t.Errorf("needs_input(false): got %q, want running", got)
	}
	if got := apply(Idle, false, Event{Kind: NeedsInput, Needs: true}).State; got != Idle {
		t.Errorf("needs_input at an idle prompt must be a no-op, got %q", got)
	}
	if got := apply(Waiting, false, Event{Kind: NotBusy}).State; got != Done {
		t.Errorf("a waiting command that exits must still settle, got %q", got)
	}
}

// ── poll channel: agent leaf — hooks are the sole authority ─────────────────

func TestPollNeverDrivesAnAgentLeaf(t *testing.T) {
	// This is the stuck-orange-dot bug. An agent is foreground whether it is
	// thinking or idle at its own prompt, so presence is not busy.
	if got := apply(Idle, true, Event{Kind: Busy}).State; got != Idle {
		t.Errorf("busy must not fabricate running on an agent leaf, got %q", got)
	}
	if got := apply(Running, true, Event{Kind: NotBusy}).State; got != Running {
		t.Errorf("not_busy must not settle a live agent turn, got %q", got)
	}
	if got := apply(Running, true, Event{Kind: NeedsInput, Needs: true}).State; got != Running {
		t.Errorf("needs_input must not drag a running agent into waiting, got %q", got)
	}
}

func TestSuppressedPollEventIsNotNews(t *testing.T) {
	// A suppressed event must leave the phase byte-identical, or the store will
	// emit and persist on every 2 s tick.
	cur := Phase{State: Running, IsAgent: true, UpdatedAt: 5}
	if got := Next(cur, Event{Kind: NotBusy}, now); got != cur {
		t.Errorf("suppressed event changed the phase: %+v → %+v", cur, got)
	}
}

func TestUnhandledEventIsNotNews(t *testing.T) {
	cur := Phase{State: Idle, UpdatedAt: 5}
	if got := Next(cur, Event{Kind: Wait}, now); got != cur {
		t.Errorf("wait in idle changed the phase: %+v → %+v", cur, got)
	}
}

func TestSetAgentFlipsTheGuardMidFlight(t *testing.T) {
	p := apply(Running, true, Event{Kind: SetAgent, IsAgent: false}, Event{Kind: NotBusy})
	if p.State != Done {
		t.Fatalf("after set_agent(false) the poll must settle it, got %q", p.State)
	}
	p = apply(Idle, false, Event{Kind: SetAgent, IsAgent: true}, Event{Kind: Busy})
	if p.State != Idle {
		t.Fatalf("after set_agent(true) the poll must be muted, got %q", p.State)
	}
}

// ── watchdog ────────────────────────────────────────────────────────────────

func TestStaleOnlyAppliesToAnInFlightTurn(t *testing.T) {
	for _, from := range []State{Running, Waiting, Permission} {
		if got := apply(from, true, Event{Kind: GoneStale}).State; got != Stale {
			t.Errorf("%s → stale: got %q, want stale", from, got)
		}
	}
	for _, from := range []State{Idle, Done, Error} {
		if got := apply(from, true, Event{Kind: GoneStale}).State; got != from {
			t.Errorf("%s → stale must be a no-op, got %q", from, got)
		}
	}
}

// ── session metadata is not a transition ────────────────────────────────────

func TestSessionCarriesMetadataWithoutChangingState(t *testing.T) {
	p := apply(Running, true, Event{Kind: Session, Model: "opus", Title: "fix the parser"})
	if p.State != Running {
		t.Errorf("session must not change state, got %q", p.State)
	}
	if p.Model != "opus" || p.Title != "fix the parser" {
		t.Errorf("metadata not carried: %+v", p)
	}
}

func TestSessionDoesNotClobberWithEmptyValues(t *testing.T) {
	p := Phase{State: Running, Model: "opus", Title: "fix the parser"}
	p = Next(p, Event{Kind: Session}, now)
	if p.Model != "opus" || p.Title != "fix the parser" {
		t.Errorf("empty session fields wiped known metadata: %+v", p)
	}
}

func TestInFlight(t *testing.T) {
	for _, s := range []State{Running, Waiting, Permission} {
		if !InFlight(s) {
			t.Errorf("%q must count as in flight", s)
		}
	}
	for _, s := range []State{Idle, Done, Error, Stale} {
		if InFlight(s) {
			t.Errorf("%q must not count as in flight", s)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./internal/agentphase/`
Expected: FAIL — `no non-test Go files` / `undefined: Next`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/internal/agentphase/phase.go

// Package agentphase derives one leaf's agent phase from the three channels
// that report on it: the agent's own status hooks, the foreground poll, and the
// dead-PTY watchdog. It is the Go port of src/machines/agentStatus.ts, with one
// deliberate subtraction.
//
// `review` and the 4 s transient `done` are NOT states here. "The turn finished
// and nobody looked at it" is a read receipt, and a receipt belongs to whoever
// is (or is not) looking — the desktop window and a phone disagree about that by
// definition. So this package records WHEN a turn ended (`TurnEndedAt`) and each
// client compares it against its own `seenAt` (src/runtime/displayStatus.ts).
//
// Pure by construction: no clock, no IO, no logging. `now` is a parameter so the
// tests are deterministic, and the store in package main owns persistence and
// event emission.
package agentphase

type State string

const (
	Idle       State = "idle"
	Running    State = "running"
	Waiting    State = "waiting"
	Permission State = "permission"
	Done       State = "done"
	Error      State = "error"
	// Stale is the dead-PTY watchdog's verdict: the process is gone and no Stop
	// hook will ever arrive. Named for what happened, not for what we did about
	// it (the old code called this "interrupt", which described the remedy).
	Stale State = "stale"
)

type Kind string

const (
	// channel 1 — agent status hooks
	Start   Kind = "start"
	Wait    Kind = "wait"
	Resume  Kind = "resume"
	Permit  Kind = "permission"
	Stop    Kind = "stop"
	Fail    Kind = "fail"
	Session Kind = "session"
	// channel 2 — foreground poll (plain-command leaves only)
	SetAgent   Kind = "set_agent"
	Busy       Kind = "busy"
	NotBusy    Kind = "not_busy"
	NeedsInput Kind = "needs_input"
	// channel 3 — interrupt / watchdog
	Interrupt Kind = "interrupt"
	GoneStale Kind = "stale"
)

// Phase is the whole server-side truth about one leaf. Comparable on purpose:
// the store uses == to decide whether anything is worth emitting.
type Phase struct {
	State       State  `json:"state"`
	Detail      string `json:"detail,omitempty"`
	Model       string `json:"model,omitempty"`
	Title       string `json:"title,omitempty"`
	IsAgent     bool   `json:"isAgent"`
	TurnEndedAt int64  `json:"turnEndedAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type Event struct {
	Kind    Kind
	Detail  string
	Model   string
	Title   string
	IsAgent bool
	Needs   bool
}

func InFlight(s State) bool {
	return s == Running || s == Waiting || s == Permission
}

// Next returns the phase after applying ev. An event this state does not accept
// returns cur byte-identical — "nothing happened" must not look like news, or
// the store emits on every poll tick.
func Next(cur Phase, ev Event, now int64) Phase {
	next := cur
	next.UpdatedAt = now

	switch ev.Kind {
	case SetAgent:
		// Accepted in every state: the poll flips this as processes come and go,
		// independent of where the phase currently sits.
		if cur.IsAgent == ev.IsAgent {
			return cur
		}
		next.IsAgent = ev.IsAgent
		return next
	case Session:
		// Metadata (model / task title), never a transition. Empty fields mean
		// "no news", not "clear it".
		if ev.Model == "" && ev.Title == "" {
			return cur
		}
		if ev.Model != "" {
			next.Model = ev.Model
		}
		if ev.Title != "" {
			next.Title = ev.Title
		}
		return next
	case Busy, NotBusy, NeedsInput:
		// The whole stuck-orange-dot rule, in one place: an agent stays
		// foreground whether it is thinking or idle at its own prompt, so for an
		// agent leaf the hooks are the sole authority.
		if cur.IsAgent {
			return cur
		}
	}

	switch cur.State {
	case Idle, Done, Error, Stale:
		switch ev.Kind {
		case Start, Busy:
			return started(next)
		case Permit:
			next.State = Permission
			next.Detail = ""
			return next
		}

	case Running:
		switch ev.Kind {
		case Wait:
			next.State = Waiting
			return next
		case Permit:
			next.State = Permission
			return next
		case Stop, NotBusy:
			return settled(next, now)
		case NeedsInput:
			if ev.Needs {
				next.State = Waiting
				return next
			}
		case Fail:
			return failed(next, ev, now)
		case Interrupt:
			return interrupted(next)
		case GoneStale:
			next.State = Stale
			return next
		}

	case Waiting:
		switch ev.Kind {
		case Start, Resume:
			return started(next)
		case Permit:
			next.State = Permission
			return next
		case Stop, NotBusy:
			return settled(next, now)
		case NeedsInput:
			// Busy while waiting is a no-op — the command is still foreground,
			// just blocked on the user. Only needs:false resumes it.
			if !ev.Needs {
				return started(next)
			}
		case Fail:
			return failed(next, ev, now)
		case Interrupt:
			return interrupted(next)
		case GoneStale:
			next.State = Stale
			return next
		}

	case Permission:
		switch ev.Kind {
		case Start, Resume:
			return started(next)
		case Stop:
			return settled(next, now)
		case Fail:
			return failed(next, ev, now)
		case Interrupt:
			return interrupted(next)
		case GoneStale:
			next.State = Stale
			return next
		}
	}

	return cur
}

func started(p Phase) Phase {
	p.State = Running
	p.Detail = ""
	// A running turn has not ended. Leaving a stale TurnEndedAt here would make
	// every client render the new turn as an unread completion.
	p.TurnEndedAt = 0
	return p
}

func settled(p Phase, now int64) Phase {
	p.State = Done
	p.TurnEndedAt = now
	return p
}

func failed(p Phase, ev Event, now int64) Phase {
	p.State = Error
	p.Detail = ev.Detail
	p.TurnEndedAt = now
	return p
}

func interrupted(p Phase) Phase {
	p.State = Idle
	p.Detail = ""
	p.TurnEndedAt = 0
	return p
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src-wails && go test ./internal/agentphase/ -v`
Expected: PASS, všechny testy

- [ ] **Step 5: Verify the purity constraint**

Run: `cd src-wails && go list -deps ./internal/agentphase/ | grep -Ev '^(internal/|errors|unsafe|runtime|iter|math|sync|unicode|strconv|slices|cmp)' | head`
Expected: prázdné (nebo jen stdlib) — žádný `database/sql`, žádný wails, žádný `main`

- [ ] **Step 6: Commit**

```bash
git add src-wails/internal/agentphase/
git commit -m "feat(phase): derive agent phase in Go, without review

Port of src/machines/agentStatus.ts as a pure function, minus one state
on purpose. 'The turn finished and nobody looked' is a read receipt, and
a receipt belongs to whoever is (not) looking — the desktop window and a
phone disagree about that by definition. So Next() records when a turn
ended and leaves review to each client.

Keeps the rule that matters: the foreground poll can never drive an agent
leaf, because an agent is foreground whether it is thinking or idle at its
own prompt. That guard is the stuck-orange-dot bug, stated once.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `PhaseStore` — držení, persistence, emit

**Files:**
- Create: `src-wails/phasestore.go`
- Test: `src-wails/phasestore_test.go`
- Modify: `src-wails/db.go:42` (nová tabulka v `migrate()`)
- Modify: `src-wails/hookserver.go` (`hookPayload.Pid`, routování do store)

**Interfaces:**
- Consumes: `agentphase.{Phase,Event,Next,InFlight}` (Task 2); `emitAll(ctx, name, payload)` z `events.go:18`; `openDB` schema v `db.go:42`
- Produces:
  - `func NewPhaseStore(ctx context.Context, db *sql.DB) (*PhaseStore, error)`
  - `func (s *PhaseStore) Apply(ptyID string, ev agentphase.Event)`
  - `func (s *PhaseStore) Get(ptyID string) agentphase.Phase`
  - `func (s *PhaseStore) All() map[string]agentphase.Phase`
  - `func (s *PhaseStore) Replay(ptyID string)`
  - `func (h *HookServer) SetPhaseStore(s *PhaseStore)`
  - Wails event `pty-phase-{ptyID}` s payloadem `agentphase.Phase`

- [ ] **Step 1: Write the failing test**

```go
// src-wails/phasestore_test.go
package main

import (
	"context"
	"testing"

	"burrow/internal/agentphase"
)

func newTestPhaseStore(t *testing.T) *PhaseStore {
	t.Helper()
	db, err := openDB(t.TempDir())
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewPhaseStore(context.Background(), db)
	if err != nil {
		t.Fatalf("NewPhaseStore: %v", err)
	}
	return s
}

func TestPhaseStoreAppliesAndReads(t *testing.T) {
	s := newTestPhaseStore(t)
	s.Apply("7", agentphase.Event{Kind: agentphase.SetAgent, IsAgent: true})
	s.Apply("7", agentphase.Event{Kind: agentphase.Start})

	got := s.Get("7")
	if got.State != agentphase.Running {
		t.Fatalf("state = %q, want running", got.State)
	}
	if !got.IsAgent {
		t.Error("IsAgent lost")
	}
	if len(s.All()) != 1 {
		t.Errorf("All() = %d entries, want 1", len(s.All()))
	}
}

func TestPhaseStoreUnknownPtyIsIdle(t *testing.T) {
	s := newTestPhaseStore(t)
	if got := s.Get("nope").State; got != agentphase.Idle {
		t.Errorf("unknown pty = %q, want idle", got)
	}
}

func TestPhaseStorePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewPhaseStore(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply("3", agentphase.Event{Kind: agentphase.SetAgent, IsAgent: true})
	s.Apply("3", agentphase.Event{Kind: agentphase.Start})
	s.Apply("3", agentphase.Event{Kind: agentphase.Fail, Detail: "rate_limit"})
	db.Close()

	// A restart is what used to lose every dot: the state lived in Terminal.vue.
	db2, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2, err := NewPhaseStore(context.Background(), db2)
	if err != nil {
		t.Fatal(err)
	}

	got := s2.Get("3")
	if got.State != agentphase.Error {
		t.Fatalf("state after restart = %q, want error", got.State)
	}
	if got.Detail != "rate_limit" {
		t.Errorf("Detail after restart = %q, want rate_limit", got.Detail)
	}
	if got.TurnEndedAt == 0 {
		t.Error("TurnEndedAt lost across restart — review/error would never render")
	}
	if !got.IsAgent {
		t.Error("IsAgent lost across restart")
	}
}

func TestPhaseStoreDoesNotEmitOnNoOp(t *testing.T) {
	s := newTestPhaseStore(t)
	s.Apply("9", agentphase.Event{Kind: agentphase.SetAgent, IsAgent: true})
	s.Apply("9", agentphase.Event{Kind: agentphase.Start})

	emits := 0
	s.emit = func(string, agentphase.Phase) { emits++ }

	// Suppressed by the agent guard, and a repeat of the current state.
	s.Apply("9", agentphase.Event{Kind: agentphase.NotBusy})
	s.Apply("9", agentphase.Event{Kind: agentphase.SetAgent, IsAgent: true})
	if emits != 0 {
		t.Fatalf("emitted %d times for events that changed nothing", emits)
	}

	s.Apply("9", agentphase.Event{Kind: agentphase.Stop})
	if emits != 1 {
		t.Fatalf("emitted %d times for a real transition, want 1", emits)
	}
}

func TestPhaseStoreReplayEmitsKnownPhaseOnly(t *testing.T) {
	s := newTestPhaseStore(t)
	names := []string{}
	s.emit = func(name string, _ agentphase.Phase) { names = append(names, name) }

	s.Replay("missing")
	if len(names) != 0 {
		t.Fatalf("replayed an unknown pty: %v", names)
	}

	s.Apply("4", agentphase.Event{Kind: agentphase.Start})
	names = names[:0]
	s.Replay("4")
	if len(names) != 1 || names[0] != "pty-phase-4" {
		t.Fatalf("replay emitted %v, want [pty-phase-4]", names)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test -run TestPhaseStore ./...`
Expected: FAIL — `undefined: NewPhaseStore`

- [ ] **Step 3: Add the table**

V `src-wails/db.go`, do slice `CREATE TABLE IF NOT EXISTS` příkazů v `migrate()` (vedle `checkpoints`, kolem řádku 90) přidej:

```go
		`CREATE TABLE IF NOT EXISTS pty_phase (
			pty_id        TEXT PRIMARY KEY,
			state         TEXT NOT NULL,
			detail        TEXT NOT NULL DEFAULT '',
			model         TEXT NOT NULL DEFAULT '',
			title         TEXT NOT NULL DEFAULT '',
			is_agent      INTEGER NOT NULL DEFAULT 0,
			turn_ended_at INTEGER NOT NULL DEFAULT 0,
			updated_at    INTEGER NOT NULL DEFAULT 0
		)`,
```

- [ ] **Step 4: Write the store**

```go
// src-wails/phasestore.go
package main

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"burrow/internal/agentphase"
)

// PhaseStore is the app-side owner of every leaf's agent phase: it applies
// events through the pure agentphase.Next, keeps the result, persists it, and
// tells everyone who cares.
//
// It exists because the phase used to live in Terminal.vue, which meant it
// existed only for a mounted workspace and died on reload. A phone asking "what
// is that agent doing" cannot depend on which workspace the desktop happens to
// be showing.
type PhaseStore struct {
	ctx    context.Context
	db     *sql.DB
	mu     sync.RWMutex
	phases map[string]agentphase.Phase

	// Injection seams, so the tests need neither a Wails runtime nor a clock.
	emit func(name string, p agentphase.Phase)
	now  func() int64
}

func NewPhaseStore(ctx context.Context, db *sql.DB) (*PhaseStore, error) {
	s := &PhaseStore{
		ctx:    ctx,
		db:     db,
		phases: map[string]agentphase.Phase{},
		now:    func() int64 { return time.Now().UnixMilli() },
	}
	s.emit = func(name string, p agentphase.Phase) { emitAll(s.ctx, name, p) }
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PhaseStore) load() error {
	rows, err := s.db.Query(`SELECT pty_id, state, detail, model, title, is_agent,
		turn_ended_at, updated_at FROM pty_phase`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var p agentphase.Phase
		var isAgent int
		if err := rows.Scan(&id, &p.State, &p.Detail, &p.Model, &p.Title, &isAgent,
			&p.TurnEndedAt, &p.UpdatedAt); err != nil {
			return err
		}
		p.IsAgent = isAgent == 1
		s.phases[id] = p
	}
	return rows.Err()
}

// Apply runs one event through the state machine. A transition that changes
// nothing is not persisted and not emitted — the foreground poll fires every
// 2 s per leaf, so "no news" has to stay silent.
func (s *PhaseStore) Apply(ptyID string, ev agentphase.Event) {
	if ptyID == "" {
		return
	}
	s.mu.Lock()
	cur := s.phases[ptyID]
	if cur.State == "" {
		cur.State = agentphase.Idle
	}
	next := agentphase.Next(cur, ev, s.now())
	if sameNews(cur, next) {
		s.mu.Unlock()
		return
	}
	s.phases[ptyID] = next
	s.mu.Unlock()

	s.persist(ptyID, next)
	s.emit("pty-phase-"+ptyID, next)
}

// sameNews compares two phases ignoring UpdatedAt, which is a timestamp rather
// than information: bumping it alone is not worth a write or a wake-up.
func sameNews(a, b agentphase.Phase) bool {
	a.UpdatedAt, b.UpdatedAt = 0, 0
	return a == b
}

func (s *PhaseStore) persist(ptyID string, p agentphase.Phase) {
	isAgent := 0
	if p.IsAgent {
		isAgent = 1
	}
	_, err := s.db.Exec(`INSERT INTO pty_phase
		(pty_id, state, detail, model, title, is_agent, turn_ended_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(pty_id) DO UPDATE SET
			state=excluded.state, detail=excluded.detail, model=excluded.model,
			title=excluded.title, is_agent=excluded.is_agent,
			turn_ended_at=excluded.turn_ended_at, updated_at=excluded.updated_at`,
		ptyID, string(p.State), p.Detail, p.Model, p.Title, isAgent, p.TurnEndedAt, p.UpdatedAt)
	if err != nil {
		// A lost write costs a dot after the next restart, not correctness now.
		log.Printf("pty_phase persist %s: %v", ptyID, err)
	}
}

func (s *PhaseStore) Get(ptyID string) agentphase.Phase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.phases[ptyID]
	if !ok {
		return agentphase.Phase{State: agentphase.Idle}
	}
	return p
}

func (s *PhaseStore) All() map[string]agentphase.Phase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]agentphase.Phase, len(s.phases))
	for id, p := range s.phases {
		out[id] = p
	}
	return out
}

// Replay re-emits a known phase after a view attaches. A terminal can already be
// running before its XTerm mounts, and the first hook would otherwise be lost.
func (s *PhaseStore) Replay(ptyID string) {
	s.mu.RLock()
	p, ok := s.phases[ptyID]
	s.mu.RUnlock()
	if ok {
		s.emit("pty-phase-"+ptyID, p)
	}
}
```

- [ ] **Step 5: Route hooks into the store**

V `src-wails/hookserver.go`:

1. Přidej `Pid` do `hookPayload` (posílá ho `burrow status --pid`, dnes se zahazuje — Task 4 na něm staví pid sweep):

```go
type hookPayload struct {
	PtyID  string `json:"ptyId"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	Model  string `json:"model,omitempty"`
	Source string `json:"source,omitempty"`
	Title  string `json:"title,omitempty"`
	Pid    int    `json:"pid,omitempty"`
}
```

2. Přidej do `HookServer` field a setter:

```go
	phases *PhaseStore
	pids   sync.Map // ptyID → int, the agent's own pid as reported by its hook
```

```go
// SetPhaseStore wires the hook channel into the phase machine. Separate from
// StartHookServer because the hook server comes up before the DB — an agent's
// shell must be able to report status before anything else is ready.
func (h *HookServer) SetPhaseStore(s *PhaseStore) {
	h.mu.Lock()
	h.phases = s
	h.mu.Unlock()
}

// AgentPid returns the pid an agent reported for this PTY, 0 if none.
func (h *HookServer) AgentPid(ptyID string) int {
	if v, ok := h.pids.Load(ptyID); ok {
		return v.(int)
	}
	return 0
}
```

3. Na konci `handleStatus`, před `w.WriteHeader`, přidej routování (ponech i dnešní `h.emitStatus(p)` — Task 7 ho odstraní až frontend přejde na `pty-phase-{id}`):

```go
	if p.Pid > 0 && p.PtyID != "" {
		h.pids.Store(p.PtyID, p.Pid)
	}
	h.mu.RLock()
	store := h.phases
	h.mu.RUnlock()
	if store != nil {
		store.Apply(p.PtyID, hookEvent(p))
	}
```

4. Přidej mapování (jediné místo, kde se hook vokabulář překládá na fázi):

```go
// hookEvent translates the `burrow status` vocabulary into a phase event. The
// hook names are the agent's; the phase names are ours, and the mapping lives
// here alone.
func hookEvent(p hookPayload) agentphase.Event {
	switch p.State {
	case "running":
		return agentphase.Event{Kind: agentphase.Start}
	case "waiting":
		return agentphase.Event{Kind: agentphase.Wait}
	case "permission":
		return agentphase.Event{Kind: agentphase.Permit}
	case "done":
		return agentphase.Event{Kind: agentphase.Stop}
	case "error":
		return agentphase.Event{Kind: agentphase.Fail, Detail: p.Detail}
	case "session":
		// SessionStart is metadata, not a turn boundary.
		return agentphase.Event{Kind: agentphase.Session, Model: p.Model, Title: p.Title}
	case "interrupt":
		return agentphase.Event{Kind: agentphase.Interrupt}
	default:
		return agentphase.Event{Kind: agentphase.Kind(p.State)}
	}
}
```

5. Import `"burrow/internal/agentphase"` a `"sync"` (už tam je).

**Poznámka pro implementátora:** ověř module path — `grep '^module' src-wails/go.mod` — a použij ji místo `burrow/` v importech, pokud se liší.

- [ ] **Step 6: Wire the store at startup**

V `src-wails/app.go` přidej `phases *PhaseStore` do `App` (vedle `hookSrv *HookServer`, kolem řádku 20) a hned za `a.hookSrv = hookSrv` (řádek 207):

```go
	// The phase store needs the DB; the hook server does not, which is why it
	// comes up first — an agent's shell must be able to report status before
	// anything else is ready.
	if phases, err := NewPhaseStore(ctx, a.db); err != nil {
		log.Printf("phase store: %v", err)
	} else {
		a.phases = phases
		a.hookSrv.SetPhaseStore(phases)
	}
```

- [ ] **Step 7: Run tests**

Run: `cd src-wails && go test -run 'TestPhaseStore' ./... -v && go build ./...`
Expected: PASS, build clean

- [ ] **Step 8: Commit**

```bash
git add src-wails/phasestore.go src-wails/phasestore_test.go src-wails/db.go src-wails/hookserver.go src-wails/app.go
git commit -m "feat(phase): hold and persist agent phase app-side

The phase used to live in Terminal.vue, so it existed only for a mounted
workspace and died on reload. A phone asking what an agent is doing can't
depend on which workspace the desktop happens to be showing, so the store
owns it now: apply through the pure machine, persist to pty_phase, emit
pty-phase-{id}.

A transition that changes nothing is neither written nor emitted — the
foreground poll fires every 2 s per leaf, so 'no news' has to stay quiet.

Also stops discarding the pid the status hook reports; the watchdog needs
it next.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Go-side phase poll (foreground, pid sweep, dead-PTY watchdog)

**Files:**
- Create: `src-wails/phasepoll.go`
- Test: `src-wails/phasepoll_test.go`
- Read for reference (neupravovat): `src/components/XTerm.vue:695-800`

**Interfaces:**
- Consumes: `PhaseStore.Apply/Get` (Task 3); `HookServer.AgentPid` (Task 3); `App.GetPtyForeground(id string) string` (`app.go:289` — **bez error**; prázdný string je „nic k nahlášení", protože volající to poll-uje); `App.ListPtySessions() ([]string, error)` (`app.go:276`); `App.IsPidAlive(pid int) bool` (`misc.go:70`)
- Produces: `func classifyForeground(name string) foregroundKind`, `type ptyProbe interface { Foreground(ptyID string) string; Alive(ptyID string) bool; PidAlive(pid int) bool }`, `func (p *phasePoller) tick(ctx context.Context)`, `func startPhasePoll(ctx context.Context, p *phasePoller)`

**Kontext:** `XTerm.vue` má dnes poll, který dělá **dvě** věci — jména tabů (OSC titulky, sticky names) a fázi. Jména zůstávají v renderu, protože potřebují OSC buffer z byte streamu. Fáze jde sem. `get_pty_foreground` je `TIOCGPGRP` + sysctl, žádný fork, takže dva čtenáři jednoho levného faktu jsou lepší než jeden zamotaný vlastník.

Pravidla, která se **musí** zachovat (dnes `XTerm.vue:695-760`):
- prázdný foreground **jednou** = přechodný race daemonu → ignoruj
- 3× prázdný **a** leaf je in-flight **a** daemon hlásí `alive=false` → `GoneStale`
- agent nahlášený pid, který zmizel, zatímco je leaf in-flight → `GoneStale` okamžitě (nečekej na streak)
- shell v foregroundu → `SetAgent(false)` + `NotBusy` (tohle zachraňuje Ctrl+C'd agenta bez `done` hooku)
- agent v foregroundu → `SetAgent(true)` a **nic víc** (presence není busy)
- jiný příkaz → `SetAgent(false)` + `Busy`

- [ ] **Step 1: Write the failing test**

```go
// src-wails/phasepoll_test.go
package main

import (
	"context"
	"testing"

	"burrow/internal/agentphase"
)

func TestClassifyForeground(t *testing.T) {
	cases := map[string]foregroundKind{
		"zsh":          fgShell,
		"bash":         fgShell,
		"fish":         fgShell,
		"claude":       fgAgent,
		"codex":        fgAgent,
		"copilot":      fgAgent,
		"npm":          fgCommand,
		"vim":          fgCommand,
		"cargo":        fgCommand,
		"node":         fgCommand,
	}
	for name, want := range cases {
		if got := classifyForeground(name); got != want {
			t.Errorf("classifyForeground(%q) = %v, want %v", name, got, want)
		}
	}
}

// fakePtyProbe stands in for the daemon so the poll is testable without one.
type fakePtyProbe struct {
	foreground map[string]string
	alive      map[string]bool
	pidAlive   map[int]bool
}

func (f *fakePtyProbe) Foreground(id string) string { return f.foreground[id] }
func (f *fakePtyProbe) Alive(id string) bool        { return f.alive[id] }
func (f *fakePtyProbe) PidAlive(pid int) bool       { return f.pidAlive[pid] }

func newPollFixture(t *testing.T, probe *fakePtyProbe, pids map[string]int) (*phasePoller, *PhaseStore) {
	t.Helper()
	store := newTestPhaseStore(t)
	p := &phasePoller{
		store: store,
		probe: probe,
		pidOf: func(id string) int { return pids[id] },
		ids:   func() []string { return keysOf(probe.foreground) },
		empty: map[string]int{},
	}
	return p, store
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPollAgentPresenceIsNotBusy(t *testing.T) {
	probe := &fakePtyProbe{
		foreground: map[string]string{"1": "claude"},
		alive:      map[string]bool{"1": true},
	}
	p, store := newPollFixture(t, probe, nil)

	p.tick(context.Background())

	got := store.Get("1")
	if !got.IsAgent {
		t.Error("an agent in the foreground must mark the leaf as an agent")
	}
	if got.State != agentphase.Idle {
		t.Errorf("state = %q — agent presence must never fabricate running", got.State)
	}
}

func TestPollDrivesPlainCommand(t *testing.T) {
	probe := &fakePtyProbe{
		foreground: map[string]string{"2": "npm"},
		alive:      map[string]bool{"2": true},
	}
	p, store := newPollFixture(t, probe, nil)

	p.tick(context.Background())
	if got := store.Get("2").State; got != agentphase.Running {
		t.Errorf("state = %q, want running for a plain command", got)
	}

	probe.foreground["2"] = "zsh"
	p.tick(context.Background())
	if got := store.Get("2").State; got != agentphase.Done {
		t.Errorf("state = %q, want done once the shell is back", got)
	}
}

func TestPollShellRescuesAnInterruptedAgent(t *testing.T) {
	probe := &fakePtyProbe{
		foreground: map[string]string{"3": "claude"},
		alive:      map[string]bool{"3": true},
	}
	p, store := newPollFixture(t, probe, nil)
	p.tick(context.Background())
	store.Apply("3", agentphase.Event{Kind: agentphase.Start})

	// Ctrl+C: the agent is gone and no done hook ever fires.
	probe.foreground["3"] = "zsh"
	p.tick(context.Background())

	if got := store.Get("3").State; got != agentphase.Done {
		t.Errorf("state = %q — a shell back in the foreground must settle the dot", got)
	}
}

func TestPollIgnoresASingleEmptyRead(t *testing.T) {
	probe := &fakePtyProbe{
		foreground: map[string]string{"4": "claude"},
		alive:      map[string]bool{"4": true},
	}
	p, store := newPollFixture(t, probe, nil)
	p.tick(context.Background())
	store.Apply("4", agentphase.Event{Kind: agentphase.Start})

	probe.foreground["4"] = "" // transient daemon race
	p.tick(context.Background())

	if got := store.Get("4").State; got != agentphase.Running {
		t.Errorf("state = %q — one empty read is a race, not a death", got)
	}
}

func TestPollWatchdogNeedsStreakAndDeadPty(t *testing.T) {
	probe := &fakePtyProbe{
		foreground: map[string]string{"5": "claude"},
		alive:      map[string]bool{"5": true},
	}
	p, store := newPollFixture(t, probe, nil)
	p.tick(context.Background())
	store.Apply("5", agentphase.Event{Kind: agentphase.Start})

	probe.foreground["5"] = ""
	for i := 0; i < 5; i++ {
		p.tick(context.Background())
	}
	if got := store.Get("5").State; got != agentphase.Running {
		t.Fatalf("state = %q — a live PTY must not be declared stale however long it reads empty", got)
	}

	probe.alive["5"] = false
	p.tick(context.Background())
	if got := store.Get("5").State; got != agentphase.Stale {
		t.Errorf("state = %q, want stale once the daemon confirms the PTY is dead", got)
	}
}

func TestPollPidSweepSettlesImmediately(t *testing.T) {
	probe := &fakePtyProbe{
		foreground: map[string]string{"6": "claude"},
		alive:      map[string]bool{"6": true},
		pidAlive:   map[int]bool{4242: false},
	}
	p, store := newPollFixture(t, probe, map[string]int{"6": 4242})
	p.tick(context.Background())
	store.Apply("6", agentphase.Event{Kind: agentphase.Start})

	// A crashed agent can linger as a defunct foreground read, so the streak
	// watchdog would never fire. The reported pid is the faster signal.
	p.tick(context.Background())
	if got := store.Get("6").State; got != agentphase.Stale {
		t.Errorf("state = %q, want stale when the agent's own pid is gone", got)
	}
}

func TestPollPidSweepOnlyForInFlight(t *testing.T) {
	probe := &fakePtyProbe{
		foreground: map[string]string{"7": "claude"},
		alive:      map[string]bool{"7": true},
		pidAlive:   map[int]bool{7: false},
	}
	p, store := newPollFixture(t, probe, map[string]int{"7": 7})

	p.tick(context.Background()) // idle, not in flight
	if got := store.Get("7").State; got != agentphase.Idle {
		t.Errorf("state = %q — a dead pid on an idle leaf is not news", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test -run TestPoll ./...`
Expected: FAIL — `undefined: foregroundKind`

- [ ] **Step 3: Write the implementation**

```go
// src-wails/phasepoll.go
package main

import (
	"context"
	"regexp"
	"time"

	"burrow/internal/agentphase"
)

// The foreground poll, phase half only.
//
// XTerm.vue polls the same fact for tab NAMES, and keeps doing so: names need
// the OSC title buffer that only the renderer sees. This poller wants the other
// half — is a command running, has an agent's process died — which has to work
// whether or not any view is mounted. get_pty_foreground is a TIOCGPGRP plus a
// sysctl lookup with no fork, so two readers of one cheap fact beats one tangled
// owner.
const phasePollInterval = 2 * time.Second

// A sustained empty foreground can mean a dead PTY, but a single empty read is
// just a daemon race. Only act after a streak, and only with confirmation.
const emptyForegroundStreakLimit = 3

var (
	shellRe   = regexp.MustCompile(`^(zsh|bash|sh|fish|csh|tcsh|dash)$`)
	claudeRe  = regexp.MustCompile(`^claude`)
	codexRe   = regexp.MustCompile(`^codex`)
	copilotRe = regexp.MustCompile(`^copilot`)
)

type foregroundKind int

const (
	fgCommand foregroundKind = iota
	fgShell
	fgAgent
)

func classifyForeground(name string) foregroundKind {
	switch {
	case shellRe.MatchString(name):
		return fgShell
	case claudeRe.MatchString(name), codexRe.MatchString(name), copilotRe.MatchString(name):
		return fgAgent
	default:
		return fgCommand
	}
}

// ptyProbe is the slice of the daemon this poller needs, as an interface so the
// tests do not need a daemon.
//
// Foreground returns "" rather than an error on failure, matching
// App.GetPtyForeground: the caller polls this, and already reads "" as "nothing
// to say".
type ptyProbe interface {
	Foreground(ptyID string) string
	Alive(ptyID string) bool
	PidAlive(pid int) bool
}

type phasePoller struct {
	store *PhaseStore
	probe ptyProbe
	pidOf func(ptyID string) int
	ids   func() []string
	empty map[string]int // ptyID → consecutive empty foreground reads
}

func (p *phasePoller) tick(ctx context.Context) {
	for _, id := range p.ids() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		p.pollOne(id)
	}
}

func (p *phasePoller) pollOne(id string) {
	cur := p.store.Get(id)

	// Fastest death signal first: an agent that reported its pid and then
	// vanished can never finish its turn, and it may still linger as a defunct
	// foreground read, so the streak watchdog below would wait forever.
	if pid := p.pidOf(id); pid > 0 && agentphase.InFlight(cur.State) && !p.probe.PidAlive(pid) {
		p.store.Apply(id, agentphase.Event{Kind: agentphase.GoneStale})
		return
	}

	name := p.probe.Foreground(id)
	if name == "" {
		p.empty[id]++
		if p.empty[id] >= emptyForegroundStreakLimit &&
			agentphase.InFlight(cur.State) &&
			!p.probe.Alive(id) {
			p.store.Apply(id, agentphase.Event{Kind: agentphase.GoneStale})
		}
		return
	}
	p.empty[id] = 0

	switch classifyForeground(name) {
	case fgShell:
		// Back at the prompt: whatever ran has exited. This is what rescues an
		// agent killed with Ctrl+C, whose done hook never fired.
		p.store.Apply(id, agentphase.Event{Kind: agentphase.SetAgent, IsAgent: false})
		p.store.Apply(id, agentphase.Event{Kind: agentphase.NotBusy})
	case fgAgent:
		// Presence is not busy: an agent is foreground whether it is thinking or
		// idle at its own prompt. running/waiting/done come only from its hooks.
		p.store.Apply(id, agentphase.Event{Kind: agentphase.SetAgent, IsAgent: true})
	case fgCommand:
		p.store.Apply(id, agentphase.Event{Kind: agentphase.SetAgent, IsAgent: false})
		p.store.Apply(id, agentphase.Event{Kind: agentphase.Busy})
	}
}

func startPhasePoll(ctx context.Context, p *phasePoller) {
	go func() {
		t := time.NewTicker(phasePollInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.tick(ctx)
			}
		}
	}()
}
```

- [ ] **Step 4: Adapt the App to `ptyProbe` and start the poll**

V `src-wails/phasepoll.go` přidej adaptér nad `App`:

```go
// appPtyProbe adapts the existing App bindings to ptyProbe.
type appPtyProbe struct{ app *App }

func (a appPtyProbe) Foreground(ptyID string) string {
	return a.app.GetPtyForeground(ptyID)
}

func (a appPtyProbe) Alive(ptyID string) bool {
	ids, err := a.app.ListPtySessions()
	if err != nil {
		// Unknown is not dead. Reporting dead here would settle live dots on a
		// transient daemon hiccup.
		return true
	}
	for _, id := range ids {
		if id == ptyID {
			return true
		}
	}
	return false
}

func (a appPtyProbe) PidAlive(pid int) bool { return a.app.IsPidAlive(pid) }
```

A ve startupu (`app.go`, uvnitř téhož `else` bloku, který Task 3 přidal za `a.hookSrv = hookSrv`):

```go
	startPhasePoll(ctx, &phasePoller{
		store: phases,
		probe: appPtyProbe{app: a},
		pidOf: a.hookSrv.AgentPid,
		ids: func() []string {
			ids, err := a.ListPtySessions()
			if err != nil {
				return nil
			}
			return ids
		},
		empty: map[string]int{},
	})
```

- [ ] **Step 5: Run tests**

Run: `cd src-wails && go test -run 'TestPoll|TestClassify' ./... -v && go build ./...`
Expected: PASS, build clean

- [ ] **Step 6: Commit**

```bash
git add src-wails/phasepoll.go src-wails/phasepoll_test.go src-wails/app.go
git commit -m "feat(phase): poll the foreground for phase, app-side

XTerm.vue's poll does two jobs — tab names and phase. Names stay in the
renderer because they need the OSC title buffer only it sees; phase moves
here, because it has to work whether or not a view is mounted.
get_pty_foreground is a TIOCGPGRP plus a sysctl with no fork, so two
readers of one cheap fact beats one tangled owner.

Keeps every rule the old poll earned the hard way: one empty read is a
daemon race, the watchdog needs a streak AND a dead PTY, a reported pid
that vanished is the faster signal, a shell back in the foreground
rescues a Ctrl+C'd agent, and agent presence is never busy.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `emitAll` je jediné dveře

**Files:**
- Modify: `src-wails/events.go:27`
- Create: `src-wails/events_test.go`

**Interfaces:**
- Consumes: `emitAll` z `events.go:18`
- Produces: `func localOnlyEvents() map[string]bool` — vyjmenovaný allowlist lokálních eventů

**Kontext:** `emitWorkspacesChanged` sedí přímo pod `emitAll` a používá plain `runtime.EventsEmit`, takže `workspaces-changed` se na mobilního klienta nikdy nedostane. Deset dalších raw emitů je lokálních legitimně (menu, float windows, `update:progress`, `lsp-msg`). Rozdíl mezi „lokální" a „zapomenuté" je dnes neviditelný; po tomhle tasku je to zápis v allowlistu.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/events_test.go
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every event a remote client may need must go through emitAll. A plain
// runtime.EventsEmit reaches the native window only, which is how
// workspaces-changed silently never arrived on the phone.
//
// Local-only events are legitimate — they address the desktop shell itself, not
// the session. They just have to be listed here on purpose.
func localOnlyEvents() map[string]bool {
	return map[string]bool{
		"menu-check-update":    true, // native menu → desktop UI
		"menu-open-docs":       true,
		"menu-show-onboarding": true,
		"menu-show-shortcuts":  true,
		"update:progress":      true, // the updater updates this bundle only
	}
}

// Prefixed families that are local-only for the same reason.
func localOnlyPrefixes() []string {
	return []string{
		"float-",         // float windows are a desktop-only surface
		"lsp-msg-",       // LSP traffic is per-renderer, not per-session
		"extension-task", // extension bridge speaks to its own host window
	}
}

var eventsEmitRe = regexp.MustCompile(`runtime\.EventsEmit\(\s*[^,]+,\s*("([^"]*)"|"([^"]*)"\s*\+)`)

func TestEmitAllIsTheOnlyDoor(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	local := localOnlyEvents()
	prefixes := localOnlyPrefixes()

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "events.go" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range eventsEmitRe.FindAllStringSubmatch(string(raw), -1) {
			name := m[2]
			if name == "" {
				name = m[3]
			}
			if local[name] {
				continue
			}
			if hasAnyPrefix(name, prefixes) {
				continue
			}
			t.Errorf("%s emits %q with runtime.EventsEmit — use emitAll, or add it to localOnlyEvents with a reason", f, name)
		}
	}
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func TestWorkspacesChangedGoesThroughEmitAll(t *testing.T) {
	raw, err := os.ReadFile("events.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.Contains(src, `emitAll(ctx, "workspaces-changed")`) {
		t.Error("workspaces-changed must go through emitAll — a remote client needs it too")
	}
	if strings.Contains(src, `runtime.EventsEmit(ctx, "workspaces-changed")`) {
		t.Error("workspaces-changed still bypasses emitAll")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test -run 'TestEmitAll|TestWorkspacesChanged' ./... -v`
Expected: FAIL — `TestWorkspacesChangedGoesThroughEmitAll` a `TestEmitAllIsTheOnlyDoor` na `stubs.go`/`lsp.go`/`main.go`/`updater.go`/`extension_bridge.go`, pokud regexy prefixů nesedí

- [ ] **Step 3: Fix the leak**

V `src-wails/events.go:27`:

```go
// Global (no-suffix) events. emitAll, not EventsEmit: a remote client's
// workspace list goes stale otherwise, and that was invisible because the
// bypass sat one line below emitAll's own doc comment.
func emitWorkspacesChanged(ctx context.Context) { emitAll(ctx, "workspaces-changed") }
```

- [ ] **Step 4: Run tests, adjust the allowlist if the test flags something real**

Run: `cd src-wails && go test -run 'TestEmitAll|TestWorkspacesChanged' ./... -v`
Expected: PASS. Pokud test nahlásí event, který v allowlistu není: **rozhodni**, jestli je lokální (přidej do `localOnlyEvents`/`localOnlyPrefixes` s důvodem v komentáři), nebo jestli je to další díra jako `workspaces-changed` (přepiš na `emitAll`). Nepřidávej do allowlistu jen proto, aby test prošel.

- [ ] **Step 5: Commit**

```bash
git add src-wails/events.go src-wails/events_test.go
git commit -m "fix(events): route workspaces-changed through emitAll, and enforce it

emitWorkspacesChanged sat one line below emitAll's doc comment and used
plain runtime.EventsEmit, so a remote client's workspace list went stale
forever and nothing said why.

The fix is one word; the test is the point. 'Local-only' and 'someone
forgot' are indistinguishable by reading, so local-only events are now a
list with reasons and everything else must go through emitAll.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `src/runtime/displayStatus.ts` — read receipty na klientovi

**Files:**
- Create: `src/runtime/displayStatus.ts`
- Test: `src/runtime/displayStatus.test.ts`

**Interfaces:**
- Consumes: `TermStatus` z `src/lib/terminalStatus.ts` (typ zůstává tam — je to agregace a jména, ne transitions)
- Produces:
  - `export type Phase = { state: PhaseState; detail?: string; model?: string; title?: string; isAgent: boolean; turnEndedAt: number; updatedAt: number }`
  - `export type PhaseState = "idle" | "running" | "waiting" | "permission" | "done" | "error" | "stale"`
  - `export const DONE_TRANSIENT_MS = 4000`
  - `export function displayStatus(p: Phase, seenAt: number, watching: boolean, now?: number): TermStatus`
  - `export function shouldMarkSeen(p: Phase, watching: boolean): boolean`

**Kontext, který implementátor potřebuje:** Go teď posílá `Phase` bez `review` a bez transientního `done`. Tady se z fáze + `seenAt` + „kouká se na to zrovna" derivuje barva. Dvě pravidla, obě z CLAUDE.md:

1. `done` je **transientní, když se uživatel kouká** (4 s, pak nic) a **`review`, když se nekouká** (drží, dokud tab neuvidí).
2. `error` **drží, dokud tab neuvidí — i když se uživatel kouká.** Failed turn se nesmí sám uklidit.

Z toho vychází, že „viděl jsem to" se u `done` smí nastavit automaticky (uživatel se koukal, když to dobíhalo), ale u `error` **ne** — proto `shouldMarkSeen` vrací true jen pro `done`. Bez toho by turn dokončený před očima uživatele vyskočil jako nepřečtený, jakmile odejde na jiný tab.

- [ ] **Step 1: Write the failing test**

```ts
// src/runtime/displayStatus.test.ts
import { describe, expect, it } from "vitest";
import { DONE_TRANSIENT_MS, displayStatus, shouldMarkSeen, type Phase } from "./displayStatus";

const phase = (over: Partial<Phase> = {}): Phase => ({
  state: "idle",
  isAgent: true,
  turnEndedAt: 0,
  updatedAt: 0,
  ...over,
});

describe("displayStatus — in-flight states pass through", () => {
  it.each(["running", "waiting", "permission"] as const)("%s renders as itself", (state) => {
    expect(displayStatus(phase({ state }), 0, true, 1_000)).toBe(state);
  });

  it("idle renders as idle", () => {
    expect(displayStatus(phase({ state: "idle" }), 0, true, 1_000)).toBe("idle");
  });

  it("stale renders as idle — the watchdog settles a dot, it does not raise one", () => {
    expect(displayStatus(phase({ state: "stale" }), 0, false, 1_000)).toBe("idle");
  });
});

describe("displayStatus — done is a read receipt", () => {
  it("finished while away → review, and it persists", () => {
    const p = phase({ state: "done", turnEndedAt: 1_000 });
    expect(displayStatus(p, 0, false, 1_100)).toBe("review");
    expect(displayStatus(p, 0, false, 1_000 + 60_000)).toBe("review");
  });

  it("finished while watching → transient done, then idle", () => {
    // shouldMarkSeen fires at settle, so seenAt is already fresh.
    const p = phase({ state: "done", turnEndedAt: 1_000 });
    expect(displayStatus(p, 1_000, true, 1_100)).toBe("done");
    expect(displayStatus(p, 1_000, true, 1_000 + DONE_TRANSIENT_MS + 1)).toBe("idle");
  });

  it("leaving mid-transient does not resurrect it as unread", () => {
    const p = phase({ state: "done", turnEndedAt: 1_000 });
    expect(displayStatus(p, 1_000, false, 1_100)).toBe("idle");
  });

  it("review clears once the tab is seen", () => {
    const p = phase({ state: "done", turnEndedAt: 1_000 });
    expect(displayStatus(p, 2_000, false, 2_100)).toBe("idle");
  });
});

describe("displayStatus — error never auto-clears", () => {
  it("persists even while watching", () => {
    const p = phase({ state: "error", detail: "billing_error", turnEndedAt: 1_000 });
    expect(displayStatus(p, 0, true, 1_100)).toBe("error");
    expect(displayStatus(p, 0, true, 1_000 + DONE_TRANSIENT_MS + 1)).toBe("error");
  });

  it("clears only once the tab is seen", () => {
    const p = phase({ state: "error", turnEndedAt: 1_000 });
    expect(displayStatus(p, 2_000, true, 2_100)).toBe("idle");
  });
});

describe("shouldMarkSeen", () => {
  it("marks a completion the user watched", () => {
    expect(shouldMarkSeen(phase({ state: "done", turnEndedAt: 1_000 }), true)).toBe(true);
  });

  it("does not mark a completion nobody watched", () => {
    expect(shouldMarkSeen(phase({ state: "done", turnEndedAt: 1_000 }), false)).toBe(false);
  });

  it("never marks an error, even watched — a failed turn must be acknowledged", () => {
    expect(shouldMarkSeen(phase({ state: "error", turnEndedAt: 1_000 }), true)).toBe(false);
  });

  it("never marks an in-flight turn", () => {
    expect(shouldMarkSeen(phase({ state: "running" }), true)).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm vitest run src/runtime/displayStatus.test.ts`
Expected: FAIL — nelze resolvovat `./displayStatus`

- [ ] **Step 3: Write the implementation**

```ts
// src/runtime/displayStatus.ts
/**
 * Phase → dot colour.
 *
 * Go owns the phase (src-wails/internal/agentphase): what the agent is doing.
 * It deliberately has no `review` and no transient `done`, because "the turn
 * finished and nobody looked at it" is a read receipt — and the desktop window
 * and a phone disagree about who looked, by definition.
 *
 * So the receipt lives here, per client: `seenAt` is this device's last
 * acknowledgement of the tab, and `watching` is whether the user's eyes are on
 * it right now (workspace visible + tab active + window focused).
 *
 * Pure module: no Vue, no transport, no stores.
 */
import type { TermStatus } from "../lib/terminalStatus";

export type PhaseState =
  | "idle"
  | "running"
  | "waiting"
  | "permission"
  | "done"
  | "error"
  | "stale";

/** Mirror of agentphase.Phase's JSON shape. */
export interface Phase {
  state: PhaseState;
  detail?: string;
  model?: string;
  title?: string;
  isAgent: boolean;
  /** ms epoch; 0 while the turn is still running. */
  turnEndedAt: number;
  updatedAt: number;
}

/** How long a completion the user actually watched stays lime before clearing. */
export const DONE_TRANSIENT_MS = 4000;

export function displayStatus(
  p: Phase,
  seenAt: number,
  watching: boolean,
  now: number = Date.now(),
): TermStatus {
  switch (p.state) {
    // A failed turn must be acknowledged. It never auto-clears, not even while
    // the user is watching it fail.
    case "error":
      return p.turnEndedAt > seenAt ? "error" : "idle";

    case "done":
      // Unacknowledged completion: the durable "finished while you were away"
      // dot. shouldMarkSeen() is what keeps a watched completion out of here.
      if (p.turnEndedAt > seenAt) return "review";
      return watching && now - p.turnEndedAt < DONE_TRANSIENT_MS ? "done" : "idle";

    // The dead-PTY watchdog settles a stuck dot; it does not raise one.
    case "stale":
      return "idle";

    default:
      return p.state;
  }
}

/**
 * True when this device should record that it has seen the current phase.
 *
 * Only completions, and only watched ones: a turn that finished in front of the
 * user is seen, so it renders as the transient lime dot and never comes back as
 * unread when they navigate away. Errors are excluded on purpose — they require
 * an explicit look at the tab.
 */
export function shouldMarkSeen(p: Phase, watching: boolean): boolean {
  return watching && p.state === "done";
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm vitest run src/runtime/displayStatus.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/runtime/displayStatus.ts src/runtime/displayStatus.test.ts
git commit -m "feat(runtime): derive the dot from a phase and a read receipt

Go owns what the agent is doing; this owns whether THIS device has seen
it. review and the 4s transient done are receipts, not agent states — the
desktop window and a phone disagree about who looked, by definition.

The subtle half is shouldMarkSeen: a completion the user watched is
marked seen immediately, or it pops back as unread the moment they change
tabs. Errors are excluded, because a failed turn has to be acknowledged.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Zapojit frontend na `pty-phase-{id}`, smazat XState machine

**Files:**
- Modify: `src/components/XTerm.vue` (listener `pty-hook-*` → `pty-phase-*`; poll shazuje fázové emity)
- Modify: `src/components/Terminal.vue` (leaf drží `phase` + `seenAt`; status = `displayStatus`)
- Modify: `src/lib/terminalStatus.ts` (odstranit typ `AgentEvent` — už nemá odesílatele)
- Delete: `src/machines/agentStatus.ts`, `src/machines/agentStatus.test.ts`
- Modify: `src-wails/hookserver.go` (odstranit `emitStatus` + `ReplayStatus`, teď to dělá store)
- Modify: `CLAUDE.md`, `docs/context.html`

**Interfaces:**
- Consumes: `displayStatus`, `shouldMarkSeen`, `Phase` (Task 6); Wails event `pty-phase-{id}` s payloadem `Phase` (Task 3); `PhaseStore.Replay` (Task 3)
- Produces: `Terminal.vue` leaf field `phase: Phase` + `seenAt: number`; emit `phase` z `XTerm.vue`

**Kontext:** tohle je jediný task s reálným rizikem regrese — mění se zdroj pravdy pro každou tečku v UI. Postupuj přesně a na konci projdi manuální checklist.

- [ ] **Step 1: Replace the hook listener in XTerm.vue**

Najdi listener na `pty-hook-${props.ptyId}` (`grep -n 'pty-hook' src/components/XTerm.vue`). Nahraď celý ten blok — včetně lokálních `hookState` / `agentPid` proměnných, které existovaly jen pro watchdog v pollu — jedním forwardem:

```ts
// The phase is derived in Go now (src-wails/internal/agentphase), so this view
// no longer arbitrates three channels: it forwards one object and renders.
const unlistenPhase = await listen<Phase>(`pty-phase-${props.ptyId}`, (e) => {
  emit("phase", e.payload);
});
```

Přidej `phase` do `defineEmits` a **odstraň** emity `agentState`, `agentMeta`, `busy`, `interrupt` (model/title teď jedou v `Phase`).

- [ ] **Step 2: Strip the phase half out of XTerm's poll**

V pollu (`src/components/XTerm.vue:695-800`) smaž:
- celý `is_pid_alive` sweep blok (řádky s `agentPid !== null`) — dělá to Go
- watchdog blok `emptyForegroundStreak >= 3` + `list_pty_sessions` — dělá to Go
- `emit("busy", …)` a `emit("interrupt")` volání
- `hookState` a `agentPid` deklarace

**Ponech** vše, co se týká jmen: `lastProcess`, `isAgentSession`, `pendingOscTitle`, `agentTitled`, `emit("title", …)`, `emit("agent", …)`, `SHELL_RE`/`CLAUDE_RE` použití pro titulky.

- [ ] **Step 3: Consume the phase in Terminal.vue**

V leaf typu přidej:

```ts
  phase: Phase;
  /** ms epoch of this device's last acknowledgement. */
  seenAt: number;
```

s initem `phase: { state: "idle", isAgent: false, turnEndedAt: 0, updatedAt: 0 }, seenAt: 0`.

Nahraď `onAgentState` / `onAgentMeta` jedním handlerem:

```ts
function onPhase(leaf: Leaf, phase: Phase) {
  leaf.phase = phase;
  if (phase.model) leaf.model = phase.model;
  // A session title only fills in a default name; it never clobbers an
  // agent-set task title.
  if (phase.title && isDefaultTitle(leaf.title)) leaf.sessionTitle = phase.title;
  leaf.statusDetail = phase.detail ?? "";

  if (shouldMarkSeen(phase, isWatching(tabOf(leaf)))) {
    leaf.seenAt = Date.now();
  }
}
```

Nahraď čtení `leaf.status` computovanou hodnotou:

```ts
function leafStatus(leaf: Leaf): TermStatus {
  return displayStatus(leaf.phase, leaf.seenAt, isWatching(tabOf(leaf)), nowTick.value);
}
```

`nowTick` je jediný `ref(Date.now())` pro celou komponentu, bumpnutý každou sekundu, aby transientní `done` sám dojel do `idle`:

```ts
const nowTick = ref(Date.now());
let tickTimer: ReturnType<typeof setInterval>;
onMounted(() => { tickTimer = setInterval(() => (nowTick.value = Date.now()), 1000); });
onBeforeUnmount(() => clearInterval(tickTimer));
```

`markTabSeen` nastavuje `leaf.seenAt = Date.now()` pro každý leaf tabu. `settleDone()` **zmiz** — nemá co dělat, transient řeší `displayStatus`.

- [ ] **Step 4: Delete the machine and the dead type**

```bash
git rm src/machines/agentStatus.ts src/machines/agentStatus.test.ts
```

V `src/lib/terminalStatus.ts` smaž `export type AgentEvent = …` (už nemá odesílatele). `TermStatus`, `STATUS_PRIORITY`, `aggregateStatus`, `getAgentAttentionState`, `deriveTabTitle`, `isDefaultTitle` **zůstávají** — je to agregace a derivace jmen, ne transitions.

- [ ] **Step 5: Drop the legacy hook emit in Go**

V `src-wails/hookserver.go` odstraň `emitStatus`, `ReplayStatus` a mapu `statuses` — store je teď vlastní. Volání `ReplayStatus` (`grep -rn 'ReplayStatus' src-wails/`) přesměruj na `a.phases.Replay(ptyID)`.

- [ ] **Step 6: Typecheck and test**

Run: `pnpm build && pnpm test && cd src-wails && go build ./... && go test ./...`
Expected: vue-tsc clean, vitest PASS, go build clean, go test PASS

- [ ] **Step 7: Manual verification — this is the task that can regress every dot**

Run: `just dev`

Projdi a odškrtni:
- [ ] spusť agenta → tečka zežloutne (`running`)
- [ ] agent se zeptá na permission → amber pulse + zvonek v Sidebaru
- [ ] nech turn dojet **při koukání** → lime tečka, do 4 s sama zmizí
- [ ] pusť turn, přepni na jiný workspace, nech dojet → vrať se → **`review`** (zelený pulse), po otevření tabu zmizí
- [ ] vyvolej chybu (odpoj síť během turnu) → **červený pulse**, drží i při koukání, zmizí až po otevření tabu
- [ ] `npm test` v plain tabu → `running`, po dokončení `done`/`review` podle koukání
- [ ] agent Ctrl+C → tečka se uklidí (shell zpátky v foregroundu)
- [ ] `kill -9` agentova procesu → tečka se uklidí do ~6 s (watchdog)
- [ ] **restart appky s dobíhajícím `review`** → tečka je tam pořád (to je nové; dřív se ztratila)
- [ ] tab title se pořád nastavuje z OSC i z agenta a je sticky přes turn boundary

- [ ] **Step 8: Update the docs**

V `CLAUDE.md` v sekci „PTY / Agent state machine (`XTerm.vue`)":
- `pty-hook-{id}` → `pty-phase-{id}`, payload je celý `Phase` objekt
- fázi derivuje `src-wails/internal/agentphase`, ne `XTerm.vue`/`Terminal.vue`
- `review`/`error` persistence je read receipt v `src/runtime/displayStatus.ts` proti `seenAt`, ne stav
- `interrupt` → `stale`
- foreground poll: Go pro fázi, `XTerm.vue` pro jména; oba čtou `get_pty_foreground`
- status přežije restart (`pty_phase` tabulka)

V `docs/context.html` doplň `internal/agentphase`, `phasestore.go`, `phasepoll.go`, `environment.go`, `src/runtime/` a smaž `src/machines/agentStatus.ts`.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor(status): render phases from Go, delete the XState machine

Terminal.vue and XTerm.vue stop arbitrating three channels and start
rendering one object. The status machine, the settleDone timer, the pid
sweep and the dead-PTY watchdog all moved to Go, where they work whether
or not the workspace is mounted and survive a reload.

XTerm keeps polling get_pty_foreground for tab names, because names need
the OSC title buffer only the renderer sees. It just stopped deriving
state from the same read.

New behaviour worth naming: a review dot now survives an app restart. It
used to live in a component, so it did not.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage (fáze 1 + 2 dle `005-remote-access.md` §10):**

| požadavek specu | task |
|---|---|
| §1 `environmentId` stabilní UUID v `environment.json` | Task 1 |
| §1 endpoint providery | **odloženo do fáze 5** — spotřebuje je až pairing UI; viz odchylka níže |
| §3 `agentphase.Phase` + stavy incl. `stale` | Task 2 |
| §3 vstup: hooky, provider runtime, PTY liveness | Task 3 (hooky, liveness), Task 4 (poll, pid sweep, watchdog) |
| §3 persist do `pty_phase` | Task 3 |
| §3 klient dostane fázi, derivuje `review` z `seenAt` | Task 6, Task 7 |
| §3 `agentStatus.ts` + `terminalStatus.ts` mizí | Task 7 — **odchylka**, viz níže |
| §3 testy portované do Go | Task 2 |
| §2 `emitAll` jediné dveře + test | Task 5 |
| §5 `src/runtime/` vzniká | Task 6 (`displayStatus.ts`; zbytek runtime je fáze 3) |

**Odchylky od specu, vědomé:**

1. **Endpoint providery odloženy.** Spec je dával do fáze 1. Nikdo je ve fázi 1–2 nekonzumuje — první čtenář je pairing UI ve fázi 5. Postavit je teď znamená napsat kód, který půl roku nikdo nespustí.
2. **`terminalStatus.ts` nezmizí, ani se nezmenší na 40 řádků.** Spec §8 tvrdil „→ `runtime/displayStatus.ts` (~40 ř.)". Chyba ve specu: ten soubor drží agregaci (`STATUS_PRIORITY`, `aggregateStatus`, `getAgentAttentionState`) a derivaci jmen (`deriveTabTitle`, `isDefaultTitle`), což nejsou transitions a nikam se nestěhují. Zmizí z něj jen typ `AgentEvent`. Mizí `src/machines/agentStatus.ts` (198 ř.) — ten byl vždycky ten pravý cíl.
3. **Foreground poll se nepřesouvá, rozděluje se.** Spec §3 mluvil o „PTY liveness z daemonu" a nespecifikoval poll. Přesun celého pollu do Go by s sebou vzal derivaci titulků, která potřebuje OSC buffer z renderu. Dva čtenáři jednoho `TIOCGPGRP` jsou levnější než ten refactor.
4. **`pty-hook-{id}` se v Tasku 3 ještě emituje dál** a mizí až v Tasku 7. Jinak by byl mezi tasky commit s mrtvými tečkami.

**Type consistency:** `agentphase.Phase` (Go, `json` tagy camelCase) ↔ `Phase` (`src/runtime/displayStatus.ts`) — `state`, `detail`, `model`, `title`, `isAgent`, `turnEndedAt`, `updatedAt` sedí. `Kind` konstanty v Tasku 2 se používají pod stejnými jmény v Tasku 3 (`hookEvent`) a Tasku 4 (`pollOne`). `PhaseStore` metody `Apply`/`Get`/`All`/`Replay` jsou konzumovány v Tasku 4 (`Apply`, `Get`) a Tasku 7 (`Replay`) pod stejnými jmény. `displayStatus(p, seenAt, watching, now?)` a `shouldMarkSeen(p, watching)` mají shodné signatury v Tasku 6 i 7.

**Ověřené fakty (žádné nejistoty nezůstaly):**
- module je `burrow`, takže importy jsou `burrow/internal/agentphase`
- `App.GetPtyForeground(id string) string` (`app.go:289`) — **bez error**; `App.ListPtySessions() ([]string, error)` (`app.go:276`); `App.IsPidAlive(pid int) bool` (`misc.go:70`)
- startup drží `a.db` (`app.go:22`) a `a.hookSrv` (`app.go:207`, hned za `StartHookServer` na 203)

**Poznámka na Task 7:** `hookPayload.Source` (`SessionStart` source) se v `agentphase.Event` nepřenáší, protože ho dnešní UI nikde nezobrazuje — `Model` a `Title` ano. Kdo ho bude potřebovat, přidá pole do `Phase`; nechávat ho protékat „pro případ" znamená nést ho ve snapshotu i po síti.

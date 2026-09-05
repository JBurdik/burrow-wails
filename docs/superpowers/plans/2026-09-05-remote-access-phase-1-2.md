# Remote Access — Implementation Plan, fáze 1 + 2

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Backend začne vlastnit `environmentId`, seznam dosažitelných endpointů a **derivovanou fázi agenta**, takže status tečky přežije restart appky, funguje pro nemountnutý workspace a je odvoditelný bez připojeného klienta.

**Architecture:** Čistá funkce `agentphase.Next(cur, ev, now)` v Go nahrazuje XState machine v `src/machines/agentStatus.ts`. `review` a 4s transient `done` **nejsou stavy** — jsou to read receipty, které si každý klient derivuje sám z `turnEndedAt` proti svému `seenAt`. `PhaseStore` drží fáze v mapě + SQLite a emituje `phase-{id}` přes nový `bus`. Endpointy vznikají z `EndpointProvider` registru, ne z větvení podle Tailscale.

**Tech Stack:** Go 1.x + `database/sql` (modernc.org/sqlite), Wails v2 events, Vue 3 + Pinia, vitest.

**Spec:** `docs/superpowers/specs/2026-09-05-remote-access-t3code-design.md`

## Global Constraints

- **Komentáře v kódu anglicky.** Plán a commit body podle zvyku repa; commit subject anglicky, Conventional Commits.
- **`bus.Emit` je jediné dveře** pro každý event, který má vidět mobilní klient. Výjimky jsou jen local-only eventy vyjmenované v Tasku 6.
- **`internal/agentphase` nesmí importovat** `database/sql`, Wails runtime, ani nic z `main`. Je to čistá funkce; IO patří `PhaseStore` v `main`.
- **Fáze v Go nezná `review`.** Kdo přidá `review` do Go, obrací rozhodnutí ze spec §3.
- **Stavy:** `idle | running | waiting_input | waiting_approval | done | failed | stale`. Žádné `starting` (Burrow nemá launch fázi odlišnou od `running`).
- **Tento plán nezakládá žádnou síťovou surface.** `RemoteEndpoints()` je Wails binding, ne HTTP endpoint. WS server je fáze 4.
- **Migrace:** idempotentní `CREATE TABLE IF NOT EXISTS` přidané do `migrate()` v `src-wails/db.go:42`. Žádný migrační framework.
- Go testy: `cd src-wails && go test ./...`. Frontend testy: `pnpm test`. Typecheck: `pnpm build`.

---

## File Structure

| soubor | odpovědnost |
|---|---|
| `src-wails/environment.go` (nový) | `environmentID`: generace, persist do `<app-data>/environment.json`, `App.EnvironmentID()` |
| `src-wails/environment_test.go` (nový) | stabilita id přes restart, regenerace po poškození |
| `src-wails/endpoints.go` (nový) | `AdvertisedEndpoint`, `EndpointProvider`, `loopbackProvider`, `selectEndpoint` |
| `src-wails/endpoints_test.go` (nový) | selection order (t3code, verbatim) |
| `src-wails/endpoints_tailscale.go` (nový) | `tailscaleProvider` nad `GetTailscaleStatus` |
| `src-wails/endpoints_tailscale_test.go` (nový) | provider s podvrženým statusem |
| `src-wails/bus.go` (nový) | `EventBus`: `Subscribe`/`Emit`, package-level `bus` |
| `src-wails/bus_test.go` (nový) | fanout, žádný sink → žádná panika |
| `src-wails/wailssink.go` (nový) | jediné místo, kde bus teče do `runtime.EventsEmit` |
| `src-wails/events.go` (modify) | `emitAll` mizí; `emitWorkspacesChanged` → `bus.Emit` |
| `src-wails/events_test.go` (nový) | grep test: `runtime.EventsEmit` jen v allowlistu |
| `src-wails/internal/agentphase/phase.go` (nový) | `State`/`Phase`/`Event`/`Kind` + čistá `Next()` |
| `src-wails/internal/agentphase/phase_test.go` (nový) | portované případy z `agentStatus.test.ts` + `stale` |
| `src-wails/phasestore.go` (nový) | `PhaseStore`: mapa + mutex + `pty_phase` tabulka + emit `phase-{id}` |
| `src-wails/phasestore_test.go` (nový) | persist/load, žádný emit při no-op |
| `src-wails/phasepoll.go` (nový) | Go-side poll: foreground → agent/busy, dead-PTY watchdog → `stale` |
| `src-wails/phasepoll_test.go` (nový) | watchdog až po 3 prázdných čteních |
| `src-wails/db.go:42` (modify) | `pty_phase` tabulka |
| `src-wails/hookserver.go` (modify) | hook → `PhaseStore.Apply`; `ReplayStatus` → `PhaseStore.Replay` |
| `src-wails/providerruntime.go` (modify) | `turn.completed`/`turn.failed` → `PhaseStore.Apply("chat:"+id, …)` |
| `src-wails/app.go` (modify) | `startup`: `environmentID`, `NewPhaseStore`, `startPhasePoll`, sink registrace |
| `src/runtime/displayStatus.ts` (nový) | `Phase` typ + `displayStatus()` + `shouldMarkSeen()` |
| `src/runtime/displayStatus.test.ts` (nový) | read-receipt derivace |
| `src/components/Terminal.vue` (modify) | `leafActors` → `leafPhases`; status z `displayStatus()` |
| `src/components/XTerm.vue` (modify) | přestává emitovat `agentState`/`busy`/`interrupt`/`needsInput`; poll si nechá jen jména |
| `src/machines/agentStatus.ts` + `.test.ts` (delete) | nahrazeno Go |
| `src/lib/terminalStatus.ts` (modify) | zůstává (agregace + jména); `AgentEvent` typ mizí |
| `CLAUDE.md` + `docs/context.html` (modify) | `pty-hook-{id}` → `phase-{id}`, kdo vlastní fázi |

---

# FÁZE 1 — environment + endpointy

### Task 1: `environmentId`

**Files:**
- Create: `src-wails/environment.go`
- Test: `src-wails/environment_test.go`
- Modify: `src-wails/app.go:158` (`startup`)

**Interfaces:**
- Consumes: `appDataDir()` z `src-wails/app.go:231`
- Produces: `func environmentID(dir string) (string, error)` — pure, dir-scoped; `func (a *App) EnvironmentID() string` — Wails binding, vrací cache

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
	if first == "" {
		t.Fatal("empty id")
	}
	second, err := environmentID(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != second {
		t.Fatalf("id changed across calls: %q != %q", first, second)
	}
}

func TestEnvironmentIDDiffersPerDir(t *testing.T) {
	a, _ := environmentID(t.TempDir())
	b, _ := environmentID(t.TempDir())
	if a == b {
		t.Fatal("two installs share an id")
	}
}

func TestEnvironmentIDRegeneratesOnCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := environmentID(dir)
	if err != nil {
		t.Fatalf("corrupt file should not be fatal: %v", err)
	}
	if id == "" {
		t.Fatal("empty id after regeneration")
	}
	again, _ := environmentID(dir)
	if id != again {
		t.Fatal("regenerated id was not persisted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestEnvironmentID`
Expected: FAIL — `undefined: environmentID`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/environment.go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// environmentFile is the on-disk shape of <app-data>/environment.json.
type environmentFile struct {
	EnvironmentID string `json:"environmentId"`
}

// environmentID returns the stable identity of this Burrow install, creating
// it on first call. Every client-side record (known environments, endpoint
// preferences, seen-at receipts) is keyed by it, so it must survive changes of
// IP, hostname and tailnet — which is why it is a stored random id rather than
// anything derived from the machine.
func environmentID(dir string) (string, error) {
	path := filepath.Join(dir, "environment.json")
	if b, err := os.ReadFile(path); err == nil {
		var f environmentFile
		// A corrupt or truncated file regenerates rather than failing: an
		// unreadable id must not be able to keep the app from starting.
		if json.Unmarshal(b, &f) == nil && f.EnvironmentID != "" {
			return f.EnvironmentID, nil
		}
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)

	b, err := json.Marshal(environmentFile{EnvironmentID: id})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

// EnvironmentID is the Wails binding. The value is resolved once at startup.
func (a *App) EnvironmentID() string { return a.environmentID }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src-wails && go test ./... -run TestEnvironmentID`
Expected: PASS

- [ ] **Step 5: Wire it into startup**

V `src-wails/app.go` přidej pole do `App` (za `sessionDir string` na řádku 37):

```go
	environmentID string
```

A v `startup` hned za úspěšný `appDataDir()` (`app.go:165`):

```go
	if id, err := environmentID(dataDir); err != nil {
		log.Printf("environment id: %v", err)
	} else {
		a.environmentID = id
	}
```

- [ ] **Step 6: Verify build**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add src-wails/environment.go src-wails/environment_test.go src-wails/app.go
git commit -m "feat(remote): stable environment id for this install"
```

---

### Task 2: `EndpointProvider` + loopback + selection order

**Files:**
- Create: `src-wails/endpoints.go`
- Test: `src-wails/endpoints_test.go`

**Interfaces:**
- Produces:
  - `type AdvertisedEndpoint struct{ Kind, HTTPBase, WSBase, Reachability string; HostedHTTPSCompatible, Default, Available bool }`
  - `type EndpointProvider interface{ Name() string; Endpoints() []AdvertisedEndpoint }`
  - `func newLoopbackProvider(port int) EndpointProvider`
  - `func selectEndpoint(eps []AdvertisedEndpoint, preferredKind string, sameMachine bool) *AdvertisedEndpoint`

- [ ] **Step 1: Write the failing test**

```go
// src-wails/endpoints_test.go
package main

import "testing"

func ep(kind string, hosted, def bool) AdvertisedEndpoint {
	reach := "private"
	if kind == "loopback" {
		reach = "loopback"
	}
	return AdvertisedEndpoint{Kind: kind, Reachability: reach, HostedHTTPSCompatible: hosted, Default: def, Available: true}
}

func TestSelectEndpointPrefersUserKind(t *testing.T) {
	eps := []AdvertisedEndpoint{ep("tailscale-magicdns", true, false), ep("loopback", false, true)}
	got := selectEndpoint(eps, "loopback", true)
	if got == nil || got.Kind != "loopback" {
		t.Fatalf("user preference ignored: %+v", got)
	}
}

func TestSelectEndpointIgnoresUnavailablePreference(t *testing.T) {
	unavailable := ep("tailscale-magicdns", true, false)
	unavailable.Available = false
	eps := []AdvertisedEndpoint{unavailable, ep("loopback", false, true)}
	got := selectEndpoint(eps, "tailscale-magicdns", true)
	if got == nil || got.Kind != "loopback" {
		t.Fatalf("fell back wrong: %+v", got)
	}
}

func TestSelectEndpointOrder(t *testing.T) {
	hosted := ep("tailscale-magicdns", true, false)
	dflt := ep("ssh-forward", false, true)
	plain := ep("lan", false, false)
	loop := ep("loopback", false, false)

	if got := selectEndpoint([]AdvertisedEndpoint{loop, plain, dflt, hosted}, "", false); got.Kind != "tailscale-magicdns" {
		t.Fatalf("hosted-https must win: %+v", got)
	}
	if got := selectEndpoint([]AdvertisedEndpoint{loop, plain, dflt}, "", false); got.Kind != "ssh-forward" {
		t.Fatalf("default must win over plain: %+v", got)
	}
	if got := selectEndpoint([]AdvertisedEndpoint{loop, plain}, "", false); got.Kind != "lan" {
		t.Fatalf("non-loopback must win: %+v", got)
	}
}

func TestSelectEndpointLoopbackOnlySameMachine(t *testing.T) {
	eps := []AdvertisedEndpoint{ep("loopback", false, true)}
	if got := selectEndpoint(eps, "", false); got != nil {
		t.Fatalf("loopback offered to a remote client: %+v", got)
	}
	if got := selectEndpoint(eps, "", true); got == nil {
		t.Fatal("loopback withheld from a same-machine client")
	}
}

func TestLoopbackProvider(t *testing.T) {
	eps := newLoopbackProvider(4242).Endpoints()
	if len(eps) != 1 {
		t.Fatalf("want 1 endpoint, got %d", len(eps))
	}
	if eps[0].WSBase != "ws://127.0.0.1:4242" {
		t.Fatalf("bad ws base: %q", eps[0].WSBase)
	}
	if got := newLoopbackProvider(0).Endpoints(); len(got) != 0 {
		t.Fatalf("port 0 must advertise nothing, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestSelectEndpoint|TestLoopbackProvider'`
Expected: FAIL — `undefined: AdvertisedEndpoint`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/endpoints.go
package main

import "fmt"

// AdvertisedEndpoint is a server-authored candidate route to this environment.
// It is a HINT, not proof: only a successful connection from the client's own
// network decides whether the route works.
type AdvertisedEndpoint struct {
	Kind                  string `json:"kind"` // loopback | tailscale-magicdns | ssh-forward | ...
	HTTPBase              string `json:"http_base"`
	WSBase                string `json:"ws_base"`
	Reachability          string `json:"reachability"` // loopback | lan | private | public | tunnel
	HostedHTTPSCompatible bool   `json:"hosted_https_compatible"`
	Default               bool   `json:"default"`
	Available             bool   `json:"available"`
}

// EndpointProvider contributes candidate endpoints. Providers live OUTSIDE the
// core environment model on purpose: Tailscale is the first one, not a branch
// in the model, so a future tunnel plugs into the same shape.
type EndpointProvider interface {
	Name() string
	// Endpoints is best-effort. A provider that cannot answer returns an empty
	// slice rather than an error — a broken provider must not break the list.
	Endpoints() []AdvertisedEndpoint
}

type loopbackProvider struct{ port int }

func newLoopbackProvider(port int) EndpointProvider { return loopbackProvider{port: port} }

func (p loopbackProvider) Name() string { return "loopback" }

func (p loopbackProvider) Endpoints() []AdvertisedEndpoint {
	if p.port == 0 {
		return nil
	}
	authority := fmt.Sprintf("127.0.0.1:%d", p.port)
	return []AdvertisedEndpoint{{
		Kind:         "loopback",
		HTTPBase:     "http://" + authority,
		WSBase:       "ws://" + authority,
		Reachability: "loopback",
		Default:      true,
		Available:    true,
	}}
}

// selectEndpoint implements t3code's order verbatim (docs/architecture/remote.md):
// user preference by KIND (never by URL — a tailnet address changes), then
// hosted-HTTPS compatible, then explicitly default, then non-loopback, and
// loopback only for a client on this same machine.
func selectEndpoint(eps []AdvertisedEndpoint, preferredKind string, sameMachine bool) *AdvertisedEndpoint {
	usable := make([]AdvertisedEndpoint, 0, len(eps))
	for _, e := range eps {
		if !e.Available {
			continue
		}
		if e.Reachability == "loopback" && !sameMachine {
			continue
		}
		usable = append(usable, e)
	}

	pick := func(match func(AdvertisedEndpoint) bool) *AdvertisedEndpoint {
		for i := range usable {
			if match(usable[i]) {
				return &usable[i]
			}
		}
		return nil
	}

	if preferredKind != "" {
		if e := pick(func(e AdvertisedEndpoint) bool { return e.Kind == preferredKind }); e != nil {
			return e
		}
	}
	if e := pick(func(e AdvertisedEndpoint) bool { return e.HostedHTTPSCompatible }); e != nil {
		return e
	}
	if e := pick(func(e AdvertisedEndpoint) bool { return e.Default }); e != nil {
		return e
	}
	if e := pick(func(e AdvertisedEndpoint) bool { return e.Reachability != "loopback" }); e != nil {
		return e
	}
	return pick(func(AdvertisedEndpoint) bool { return true })
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src-wails && go test ./... -run 'TestSelectEndpoint|TestLoopbackProvider'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src-wails/endpoints.go src-wails/endpoints_test.go
git commit -m "feat(remote): endpoint provider model with t3code selection order"
```

---

### Task 3: Tailscale endpoint provider

**Files:**
- Create: `src-wails/endpoints_tailscale.go`
- Test: `src-wails/endpoints_tailscale_test.go`

**Interfaces:**
- Consumes: `TailscaleStatus` z `src-wails/tailscale.go:74`, `tailscaleServePath` z `tailscale.go:46`, `AdvertisedEndpoint` z Tasku 2
- Produces: `func newTailscaleProvider(status func() TailscaleStatus) EndpointProvider`

Provider bere `func() TailscaleStatus`, ne `*App` — jinak by test musel postavit celou appku, aby ověřil dvě podmínky.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/endpoints_tailscale_test.go
package main

import "testing"

func strp(s string) *string { return &s }

func TestTailscaleProviderAdvertisesMagicDNS(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus {
		return TailscaleStatus{Installed: true, LoggedIn: true, DNSName: strp("mac-mini.tail1234.ts.net"), Serving: true}
	})
	eps := p.Endpoints()
	if len(eps) != 1 {
		t.Fatalf("want 1 endpoint, got %d", len(eps))
	}
	e := eps[0]
	if e.Kind != "tailscale-magicdns" {
		t.Fatalf("bad kind: %q", e.Kind)
	}
	if e.WSBase != "wss://mac-mini.tail1234.ts.net/burrow" {
		t.Fatalf("bad ws base: %q", e.WSBase)
	}
	if !e.HostedHTTPSCompatible {
		t.Fatal("MagicDNS HTTPS must be hosted-compatible")
	}
	if e.Reachability != "private" {
		t.Fatalf("bad reachability: %q", e.Reachability)
	}
	if !e.Available {
		t.Fatal("serving node must be available")
	}
}

func TestTailscaleProviderUnavailableWhenNotServing(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus {
		return TailscaleStatus{Installed: true, LoggedIn: true, DNSName: strp("mac-mini.tail1234.ts.net"), Serving: false}
	})
	eps := p.Endpoints()
	if len(eps) != 1 {
		t.Fatalf("want the endpoint listed but unavailable, got %d", len(eps))
	}
	if eps[0].Available {
		t.Fatal("a node that is not serving must not be advertised as available")
	}
}

func TestTailscaleProviderSilentWhenLoggedOut(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus { return TailscaleStatus{Installed: true} })
	if got := p.Endpoints(); len(got) != 0 {
		t.Fatalf("logged-out node advertised %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestTailscaleProvider`
Expected: FAIL — `undefined: newTailscaleProvider`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/endpoints_tailscale.go
package main

// tailscaleProvider turns the local tailnet node into endpoint candidates.
// It takes a status function rather than the App so it stays testable without
// a running app or a real tailnet.
type tailscaleProvider struct{ status func() TailscaleStatus }

func newTailscaleProvider(status func() TailscaleStatus) EndpointProvider {
	return tailscaleProvider{status: status}
}

func (p tailscaleProvider) Name() string { return "tailscale" }

func (p tailscaleProvider) Endpoints() []AdvertisedEndpoint {
	s := p.status()
	if !s.Installed || !s.LoggedIn || s.DNSName == nil || *s.DNSName == "" {
		return nil
	}
	// The path must match what TailscaleServe mounts; serving at "/" would
	// clobber whatever else this node already publishes.
	authority := *s.DNSName + tailscaleServePath
	return []AdvertisedEndpoint{{
		Kind:     "tailscale-magicdns",
		HTTPBase: "https://" + authority,
		WSBase:   "wss://" + authority,
		// Private, not public: reachable inside the tailnet only. Funnel is
		// refused outright (see phase 5), so this never becomes "public".
		Reachability: "private",
		// MagicDNS terminates real HTTPS, which is what makes the PWA a secure
		// context — service worker, install prompt and (later) push depend on it.
		HostedHTTPSCompatible: true,
		// Listed but unavailable when the node is not serving our path, so the
		// UI can say "Tailscale is there, turn sharing on" instead of hiding it.
		Available: s.Serving,
	}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src-wails && go test ./... -run TestTailscaleProvider`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src-wails/endpoints_tailscale.go src-wails/endpoints_tailscale_test.go
git commit -m "feat(remote): tailscale endpoint provider"
```

---

### Task 4: `RemoteEndpoints()` binding

**Files:**
- Modify: `src-wails/endpoints.go`
- Modify: `src-wails/app.go` (pole `endpointProviders` + init v `startup`)
- Test: `src-wails/endpoints_test.go`

**Interfaces:**
- Consumes: `newLoopbackProvider`, `newTailscaleProvider` (Tasky 2–3), `httpServerPort` z `src-wails/httpserver.go`
- Produces: `func collectEndpoints(providers []EndpointProvider) []AdvertisedEndpoint`; `func (a *App) RemoteEndpoints() []AdvertisedEndpoint`

- [ ] **Step 1: Write the failing test**

Přidej do `src-wails/endpoints_test.go`:

```go
type fakeProvider struct {
	name string
	eps  []AdvertisedEndpoint
}

func (f fakeProvider) Name() string                    { return f.name }
func (f fakeProvider) Endpoints() []AdvertisedEndpoint { return f.eps }

func TestCollectEndpointsConcatenates(t *testing.T) {
	got := collectEndpoints([]EndpointProvider{
		fakeProvider{name: "a", eps: []AdvertisedEndpoint{ep("loopback", false, true)}},
		fakeProvider{name: "b", eps: nil},
		fakeProvider{name: "c", eps: []AdvertisedEndpoint{ep("lan", false, false)}},
	})
	if len(got) != 2 {
		t.Fatalf("want 2 endpoints, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "loopback" || got[1].Kind != "lan" {
		t.Fatalf("provider order not preserved: %+v", got)
	}
}

func TestCollectEndpointsNeverNil(t *testing.T) {
	got := collectEndpoints(nil)
	if got == nil {
		t.Fatal("must return an empty slice, not nil — the binding is JSON-marshalled")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestCollectEndpoints`
Expected: FAIL — `undefined: collectEndpoints`

- [ ] **Step 3: Write minimal implementation**

Připoj na konec `src-wails/endpoints.go`:

```go
// collectEndpoints asks every provider in order. A provider that returns
// nothing is normal, not an error.
func collectEndpoints(providers []EndpointProvider) []AdvertisedEndpoint {
	out := make([]AdvertisedEndpoint, 0, len(providers))
	for _, p := range providers {
		out = append(out, p.Endpoints()...)
	}
	return out
}

// RemoteEndpoints is a DESKTOP binding, deliberately not an HTTP route: the
// list of ways to reach this machine is recon information and nobody needs it
// before they are connected. Settings renders it; pairing (phase 5) uses it.
func (a *App) RemoteEndpoints() []AdvertisedEndpoint {
	return collectEndpoints(a.endpointProviders)
}
```

- [ ] **Step 4: Wire providers into startup**

V `src-wails/app.go` přidej pole do `App`:

```go
	endpointProviders []EndpointProvider
```

A v `startup`, za blok s `environmentID`:

```go
	a.endpointProviders = []EndpointProvider{
		newLoopbackProvider(httpServerPort),
		newTailscaleProvider(a.GetTailscaleStatus),
	}
```

- [ ] **Step 5: Run tests + build**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 6: Regenerate Wails bindings and typecheck the frontend**

Run: `cd src-wails && wails generate module && cd .. && pnpm build`
Expected: build passes; `RemoteEndpoints` a `EnvironmentID` jsou v `src-wails/frontend/wailsjs/go/main/App.d.ts`

- [ ] **Step 7: Commit**

```bash
git add src-wails/endpoints.go src-wails/endpoints_test.go src-wails/app.go src-wails/frontend/wailsjs
git commit -m "feat(remote): RemoteEndpoints binding over the provider registry"
```

---

# FÁZE 2 — fáze agenta na backendu

### Task 5: `internal/agentphase` — čistá `Next()`

**Files:**
- Create: `src-wails/internal/agentphase/phase.go`
- Test: `src-wails/internal/agentphase/phase_test.go`

**Interfaces:**
- Produces: `State`, `Phase`, `Kind`, `Event`, `func Next(cur Phase, ev Event, now int64) Phase`, `func (Phase) InFlight() bool`

Tenhle balíček **nesmí importovat nic** kromě stdlib. Žádné `database/sql`, žádný Wails, nic z `main`.

**Mapa pravidel** (převzatá 1:1 z `src/machines/agentStatus.ts`, ať je zřejmé, co se zachovává):

| dnešní XState | nový `Kind` | výsledek |
|---|---|---|
| `START` | `HookRunning` | `Running`, `Detail` vymazán |
| `WAIT` | `HookWaiting` | `WaitingInput` |
| `PERMISSION_REQUEST` | `HookPermission` | `WaitingApproval` (i z `Idle`) |
| `STOP` | `HookDone` | `Done`, `TurnEndedAt = now` |
| `FAIL` | `HookError` | `Failed`, `Detail`, `TurnEndedAt = now` |
| — | `HookSession` | jen metadata (`Model`/`Title`), stav beze změny |
| `SET_AGENT` | `PollAgent` | nastaví `IsAgent`, stav beze změny |
| `BUSY` | `PollBusy` | `Running` — **jen když `!IsAgent`** |
| `NOT_BUSY` | `PollNotBusy` | `Done` + `TurnEndedAt` — jen když `!IsAgent` a běželo |
| `NEEDS_INPUT{true}` | `PollNeedsInput` | `WaitingInput` — jen `!IsAgent` a z `Running` |
| `NEEDS_INPUT{false}` | `PollGotInput` | `Running` — jen `!IsAgent` a z `WaitingInput` |
| `INTERRUPT` | `Interrupt` | `Idle` |
| watchdog | `Dead` | `Stale` — jen když je fáze in-flight |
| `MARK_SEEN` | **neexistuje** | read receipt je klientský |

- [ ] **Step 1: Write the failing test**

```go
// src-wails/internal/agentphase/phase_test.go
package agentphase

import "testing"

const now = int64(1_000)

func apply(p Phase, evs ...Event) Phase {
	for _, ev := range evs {
		p = Next(p, ev, now)
	}
	return p
}

func TestStartsIdle(t *testing.T) {
	var p Phase
	if p.State != "" && p.State != Idle {
		t.Fatalf("zero value is not idle: %q", p.State)
	}
	if got := Next(p, Event{Kind: HookRunning}, now); got.State != Running {
		t.Fatalf("idle → running failed: %q", got.State)
	}
}

func TestHookTransitions(t *testing.T) {
	p := apply(Phase{}, Event{Kind: HookRunning})
	if p.State != Running {
		t.Fatalf("want running, got %q", p.State)
	}
	if p = apply(p, Event{Kind: HookWaiting}); p.State != WaitingInput {
		t.Fatalf("want waiting_input, got %q", p.State)
	}
	if p = apply(p, Event{Kind: HookPermission}); p.State != WaitingApproval {
		t.Fatalf("want waiting_approval, got %q", p.State)
	}
	if p = apply(p, Event{Kind: HookRunning}); p.State != Running {
		t.Fatalf("resume failed, got %q", p.State)
	}
}

func TestPermissionFromIdle(t *testing.T) {
	// A native app-server agent can deliver an approval RPC before its first
	// visible output; it is still actionable.
	if got := apply(Phase{}, Event{Kind: HookPermission}); got.State != WaitingApproval {
		t.Fatalf("want waiting_approval, got %q", got.State)
	}
}

func TestDoneRecordsTurnEnd(t *testing.T) {
	p := apply(Phase{}, Event{Kind: HookRunning}, Event{Kind: HookDone})
	if p.State != Done {
		t.Fatalf("want done, got %q", p.State)
	}
	if p.TurnEndedAt != now {
		t.Fatalf("turn end not recorded: %d", p.TurnEndedAt)
	}
}

func TestFailCarriesDetailAndClearsOnNewTurn(t *testing.T) {
	p := apply(Phase{}, Event{Kind: HookRunning}, Event{Kind: HookError, Detail: "billing_error"})
	if p.State != Failed || p.Detail != "billing_error" {
		t.Fatalf("want failed/billing_error, got %q/%q", p.State, p.Detail)
	}
	if p.TurnEndedAt != now {
		t.Fatalf("failed turn must record its end: %d", p.TurnEndedAt)
	}
	p = apply(p, Event{Kind: HookRunning})
	if p.State != Running || p.Detail != "" {
		t.Fatalf("new turn must clear detail: %q/%q", p.State, p.Detail)
	}
}

func TestSessionIsMetadataNotStatus(t *testing.T) {
	p := apply(Phase{}, Event{Kind: HookRunning})
	got := apply(p, Event{Kind: HookSession, Model: "opus", Title: "Fix the parser"})
	if got.State != Running {
		t.Fatalf("session must not change state: %q", got.State)
	}
	if got.Model != "opus" || got.Title != "Fix the parser" {
		t.Fatalf("session metadata lost: %+v", got)
	}
}

func TestPollNeverDrivesAnAgent(t *testing.T) {
	// The whole "stuck orange dot" rule: an agent is foreground whether it is
	// thinking or sitting at its prompt, so presence is not busy.
	agent := apply(Phase{}, Event{Kind: PollAgent, Bool: true})
	if got := apply(agent, Event{Kind: PollBusy}); got.State == Running {
		t.Fatal("poll fabricated running for an agent leaf")
	}
	live := apply(agent, Event{Kind: HookRunning})
	if got := apply(live, Event{Kind: PollNotBusy}); got.State != Running {
		t.Fatalf("poll settled a live agent turn: %q", got.State)
	}
	if got := apply(live, Event{Kind: PollNeedsInput}); got.State != Running {
		t.Fatalf("poll dragged a running agent into waiting: %q", got.State)
	}
}

func TestPollDrivesPlainCommands(t *testing.T) {
	p := apply(Phase{}, Event{Kind: PollBusy})
	if p.State != Running {
		t.Fatalf("want running, got %q", p.State)
	}
	if got := apply(p, Event{Kind: PollNeedsInput}); got.State != WaitingInput {
		t.Fatalf("want waiting_input, got %q", got.State)
	}
	waiting := apply(p, Event{Kind: PollNeedsInput})
	if got := apply(waiting, Event{Kind: PollGotInput}); got.State != Running {
		t.Fatalf("want running, got %q", got.State)
	}
	if got := apply(waiting, Event{Kind: PollNotBusy}); got.State != Done {
		t.Fatalf("a waiting command that exits must still settle: %q", got.State)
	}
}

func TestNeedsInputAtIdlePromptIsNoop(t *testing.T) {
	if got := apply(Phase{}, Event{Kind: PollNeedsInput}); got.State != Idle && got.State != "" {
		t.Fatalf("idle prompt produced a dot: %q", got.State)
	}
}

func TestSetAgentFlipsTheGuardMidFlight(t *testing.T) {
	p := apply(Phase{}, Event{Kind: PollBusy})
	p = apply(p, Event{Kind: PollAgent, Bool: true})
	if got := apply(p, Event{Kind: PollNotBusy}); got.State != Running {
		t.Fatalf("guard did not flip: %q", got.State)
	}
}

func TestInterruptSettlesToIdle(t *testing.T) {
	for _, start := range []Kind{HookRunning, HookWaiting, HookPermission} {
		p := apply(Phase{}, Event{Kind: start})
		if got := apply(p, Event{Kind: Interrupt}); got.State != Idle {
			t.Fatalf("interrupt from %q left %q", start, got.State)
		}
	}
}

func TestDeadOnlySettlesInFlight(t *testing.T) {
	live := apply(Phase{}, Event{Kind: HookRunning})
	if got := apply(live, Event{Kind: Dead}); got.State != Stale {
		t.Fatalf("want stale, got %q", got.State)
	}
	finished := apply(Phase{}, Event{Kind: HookRunning}, Event{Kind: HookDone})
	if got := apply(finished, Event{Kind: Dead}); got.State != Done {
		t.Fatalf("dead must not overwrite a finished turn: %q", got.State)
	}
}

func TestNoopReturnsAnUnchangedPhase(t *testing.T) {
	// The store relies on this to skip a write and an emit.
	p := apply(Phase{}, Event{Kind: HookRunning})
	if got := Next(p, Event{Kind: HookRunning}, now+5); got != p {
		t.Fatalf("repeated event produced a change: %+v vs %+v", got, p)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./internal/agentphase/...`
Expected: FAIL — `undefined: Phase`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/internal/agentphase/phase.go

// Package agentphase derives an agent's phase from the facts the backend
// already has: status hooks, provider runtime events and PTY liveness.
//
// It is a pure function on purpose. The phase must be derivable with NO client
// connected — the phone is asleep, the PWA is closed, and the answer still has
// to exist. That is also why `review` is not a phase here: whether a finished
// turn still needs looking at is per-device, so it is a read receipt the client
// derives from TurnEndedAt against its own seenAt.
//
// This package must not import database/sql, the Wails runtime, or anything
// from main. IO belongs to the store.
package agentphase

type State string

const (
	Idle            State = "idle"
	Running         State = "running"
	WaitingInput    State = "waiting_input"
	WaitingApproval State = "waiting_approval"
	Done            State = "done"
	Failed          State = "failed"
	// Stale is the dead-PTY watchdog: the turn never ended, the process is
	// gone. "interrupt" named what we did about it; this names what happened.
	Stale State = "stale"
)

// Phase is comparable on purpose — the store skips writes and emits when
// Next() returns an unchanged value.
type Phase struct {
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"` // error_type, blocking tool name
	Model  string `json:"model,omitempty"`
	Title  string `json:"title,omitempty"`
	// IsAgent gates the poll channel. An agent stays foreground whether it is
	// thinking or idle at its prompt, so the poll must never speak for it.
	IsAgent     bool  `json:"is_agent"`
	TurnEndedAt int64 `json:"turn_ended_at"` // 0 while a turn is in flight
	UpdatedAt   int64 `json:"updated_at"`
}

// InFlight reports whether a turn is still open.
func (p Phase) InFlight() bool {
	return p.State == Running || p.State == WaitingInput || p.State == WaitingApproval
}

type Kind string

const (
	HookRunning    Kind = "hook_running"
	HookWaiting    Kind = "hook_waiting"
	HookPermission Kind = "hook_permission"
	HookDone       Kind = "hook_done"
	HookError      Kind = "hook_error"
	HookSession    Kind = "hook_session"

	PollAgent      Kind = "poll_agent" // Bool = isAgent
	PollBusy       Kind = "poll_busy"
	PollNotBusy    Kind = "poll_not_busy"
	PollNeedsInput Kind = "poll_needs_input"
	PollGotInput   Kind = "poll_got_input"

	Interrupt Kind = "interrupt" // Ctrl+C
	Dead      Kind = "dead"      // watchdog confirmed the PTY is gone
)

type Event struct {
	Kind   Kind
	Detail string
	Model  string
	Source string
	Title  string
	Bool   bool
}

// Next returns the phase after ev. It returns cur unchanged (UpdatedAt
// included) when nothing happened, so callers can treat equality as "no news".
func Next(cur Phase, ev Event, now int64) Phase {
	if cur.State == "" {
		cur.State = Idle
	}
	next := cur

	switch ev.Kind {
	case HookRunning:
		next.State = Running
		next.Detail = ""
		next.TurnEndedAt = 0
	case HookWaiting:
		next.State = WaitingInput
	case HookPermission:
		next.State = WaitingApproval
	case HookDone:
		next.State = Done
		next.TurnEndedAt = now
	case HookError:
		next.State = Failed
		next.Detail = ev.Detail
		next.TurnEndedAt = now
	case HookSession:
		// Metadata, not a status: SessionStart labels the tab, it does not
		// start a turn.
		if ev.Model != "" {
			next.Model = ev.Model
		}
		if ev.Title != "" {
			next.Title = ev.Title
		}
	case PollAgent:
		next.IsAgent = ev.Bool
	case PollBusy:
		if !cur.IsAgent {
			next.State = Running
			next.Detail = ""
			next.TurnEndedAt = 0
		}
	case PollNotBusy:
		if !cur.IsAgent && cur.InFlight() {
			next.State = Done
			next.TurnEndedAt = now
		}
	case PollNeedsInput:
		if !cur.IsAgent && cur.State == Running {
			next.State = WaitingInput
		}
	case PollGotInput:
		if !cur.IsAgent && cur.State == WaitingInput {
			next.State = Running
		}
	case Interrupt:
		next.State = Idle
		next.Detail = ""
		next.TurnEndedAt = 0
	case Dead:
		if cur.InFlight() {
			next.State = Stale
			next.TurnEndedAt = now
		}
	}

	if next == cur {
		return cur
	}
	next.UpdatedAt = now
	return next
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src-wails && go test ./internal/agentphase/... -v`
Expected: PASS

- [ ] **Step 5: Assert the package has no forbidden imports**

Run: `cd src-wails && go list -deps ./internal/agentphase | grep -E 'wails|database/sql'`
Expected: žádný výstup (exit 1 z grepu je v pořádku)

- [ ] **Step 6: Commit**

```bash
git add src-wails/internal/agentphase
git commit -m "feat(phase): pure agent phase derivation in Go"
```

---

### Task 6: `bus` — jediné dveře pro eventy

**Files:**
- Create: `src-wails/bus.go`, `src-wails/bus_test.go`, `src-wails/wailssink.go`, `src-wails/events_test.go`
- Modify: `src-wails/events.go`, `src-wails/app.go:158` (`startup`), 11 volání `emitAll(`

**Interfaces:**
- Produces: `type EventSink func(name string, payload any)`; `func busSubscribe(s EventSink)`; `func busEmit(name string, payload any)`
- Nahrazuje: `emitAll(ctx, name, payload)` → `busEmit(name, payload)`

`bus` je package-level, ne pole na `App`.

```go
// ponytail: package-level bus. There is one app per process, and threading a
// handle through 11 call sites plus the daemon and hook goroutines buys
// nothing today. Make it injected when internal/server splits out.
```

- [ ] **Step 1: Write the failing tests**

```go
// src-wails/bus_test.go
package main

import "testing"

func TestBusFansOutToEverySink(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	var a, b []string
	busSubscribe(func(name string, _ any) { a = append(a, name) })
	busSubscribe(func(name string, _ any) { b = append(b, name) })

	busEmit("workspaces-changed", nil)

	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("fanout failed: %v / %v", a, b)
	}
}

func TestBusWithNoSinksDoesNotPanic(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	busEmit("pty-data-1", []byte("hi"))
}
```

```go
// src-wails/events_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Local-only events: the native window is the only consumer, so they may call
// the Wails runtime directly. Everything else goes through busEmit, or the
// mobile client silently never sees it — which is exactly how
// emitWorkspacesChanged went missing.
var wailsRuntimeAllowlist = map[string]string{
	"main.go":             "menu items",
	"wailssink.go":        "the bus → window sink itself",
	"updater.go":          "update:progress, desktop-only",
	"stubs.go":            "float window snapshots, desktop-only",
	"lsp.go":              "lsp-msg, desktop-only",
	"extension_bridge.go": "extension-task, desktop-only",
}

func TestWailsRuntimeEmitIsConfined(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "EventsEmit(") {
			continue
		}
		if _, ok := wailsRuntimeAllowlist[name]; !ok {
			t.Errorf("%s calls EventsEmit directly; use busEmit so the event reaches remote clients too "+
				"(or add it to wailsRuntimeAllowlist with a reason if it is genuinely desktop-only)", name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd src-wails && go test ./... -run 'TestBus|TestWailsRuntimeEmit'`
Expected: FAIL — `undefined: busEmit`

- [ ] **Step 3: Write the bus**

```go
// src-wails/bus.go
package main

import "sync"

// EventSink receives every app event. The native window is one sink; the WS
// server (phase 4) becomes another.
type EventSink func(name string, payload any)

// ponytail: package-level bus. There is one app per process, and threading a
// handle through 11 call sites plus the daemon and hook goroutines buys
// nothing today. Make it injected when internal/server splits out.
var (
	busMu    sync.RWMutex
	busSinks []EventSink
)

func busSubscribe(s EventSink) {
	busMu.Lock()
	defer busMu.Unlock()
	busSinks = append(busSinks, s)
}

// busEmit is the SINGLE door for any event a client may care about. There is
// deliberately no second path: the old emitAll had one, and the one call site
// that forgot it (emitWorkspacesChanged) meant the mobile client never learned
// that the workspace list had changed.
func busEmit(name string, payload any) {
	busMu.RLock()
	sinks := make([]EventSink, len(busSinks))
	copy(sinks, busSinks)
	busMu.RUnlock()
	for _, s := range sinks {
		s(name, payload)
	}
}

// busReset exists for tests.
func busReset() {
	busMu.Lock()
	defer busMu.Unlock()
	busSinks = nil
}
```

```go
// src-wails/wailssink.go
package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// installWailsSink is the ONLY place the bus meets the Wails runtime. Keeping
// it in its own file is what lets events_test.go assert the boundary.
func installWailsSink(ctx context.Context) {
	busSubscribe(func(name string, payload any) {
		runtime.EventsEmit(ctx, name, payload)
	})
}
```

- [ ] **Step 4: Replace `emitAll` and fix `emitWorkspacesChanged`**

Nahraď celý obsah `src-wails/events.go`:

```go
package main

// Global (no-suffix) events, matching src-tauri/src/lib.rs's emit_all calls.
//
// This used to be a plain runtime.EventsEmit, which meant the mobile client
// never learned the workspace list had changed. That class of bug is gone:
// there is no second door any more.
func emitWorkspacesChanged() { busEmit("workspaces-changed", nil) }
```

Pak přepiš zbylých 10 volání `emitAll(...)`:

Run: `cd src-wails && grep -rn "emitAll(" *.go`

Každé `emitAll(ctx, "name", payload)` → `busEmit("name", payload)`. Volání `emitWorkspacesChanged(ctx)` → `emitWorkspacesChanged()`. `HTTPServer.Broadcast` se z `events.go` odstraňuje — `httpserver.go` si místo toho v `SetHttpEnabled` zaregistruje sink:

```go
	busSubscribe(func(name string, payload any) { s.Broadcast(name, payload) })
```

- [ ] **Step 5: Register the window sink at startup**

V `src-wails/app.go`, jako **první** řádek `startup` za `a.ctx = ctx`:

```go
	installWailsSink(ctx)
```

- [ ] **Step 6: Run tests + build**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS. `TestWailsRuntimeEmitIsConfined` prochází.

- [ ] **Step 7: Manual check**

Run: `just dev`
Ověř: vytvoř workspace → objeví se v Sidebaru bez reloadu; otevři terminál → teče do něj výstup.

- [ ] **Step 8: Commit**

```bash
git add src-wails
git commit -m "refactor(events): single event bus, confine Wails runtime emits

emitWorkspacesChanged used runtime.EventsEmit directly, so the mobile
client never received workspaces-changed. Rather than fix that one call,
remove the second door: busEmit is the only path, and a test pins which
files may still talk to the Wails runtime."
```

---

### Task 7: `PhaseStore` — mapa, SQLite, emit

**Files:**
- Create: `src-wails/phasestore.go`, `src-wails/phasestore_test.go`
- Modify: `src-wails/db.go:42` (`migrate`), `src-wails/app.go` (`startup`)

**Interfaces:**
- Consumes: `agentphase.Next` (Task 5), `busEmit` (Task 6), `openDB` z `db.go`
- Produces:
  - `func NewPhaseStore(db *sql.DB) (*PhaseStore, error)`
  - `func (s *PhaseStore) Apply(id string, ev agentphase.Event)`
  - `func (s *PhaseStore) Get(id string) agentphase.Phase`
  - `func (s *PhaseStore) All() map[string]agentphase.Phase`
  - `func (s *PhaseStore) Replay(id string)`

Klíč `id` je `pty:<ptyID>` nebo `chat:<chatID>` — jeden store, dva zdroje, **jeden typ fáze**. Event se jmenuje `phase-<id>`, tj. `phase-pty:7`.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/phasestore_test.go
package main

import (
	"testing"

	"burrow/internal/agentphase"
)

func newTestStore(t *testing.T) (*PhaseStore, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewPhaseStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestPhaseStoreEmitsOnChange(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	var events []string
	busSubscribe(func(name string, _ any) { events = append(events, name) })

	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	if len(events) != 1 || events[0] != "phase-pty:7" {
		t.Fatalf("bad emit: %v", events)
	}
	if s.Get("pty:7").State != agentphase.Running {
		t.Fatalf("state not stored: %+v", s.Get("pty:7"))
	}
}

func TestPhaseStoreSilentOnNoop(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	var count int
	busSubscribe(func(string, any) { count++ })

	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	if count != 1 {
		t.Fatalf("a repeated event emitted %d times", count)
	}
}

func TestPhaseStoreSurvivesRestart(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewPhaseStore(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookError, Detail: "rate_limit"})
	db.Close()

	db2, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2, err := NewPhaseStore(db2)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Get("pty:7")
	if got.State != agentphase.Failed || got.Detail != "rate_limit" {
		t.Fatalf("phase did not survive restart: %+v", got)
	}
	if got.TurnEndedAt == 0 {
		t.Fatal("turn end lost across restart — the read receipt needs it")
	}
}

func TestPhaseStoreReplayReemits(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	var replayed int
	busSubscribe(func(name string, _ any) {
		if name == "phase-pty:7" {
			replayed++
		}
	})
	s.Replay("pty:7")
	if replayed != 1 {
		t.Fatalf("replay emitted %d times", replayed)
	}
	s.Replay("pty:999")
	if replayed != 1 {
		t.Fatal("replay of an unknown id emitted something")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestPhaseStore`
Expected: FAIL — `undefined: NewPhaseStore`

- [ ] **Step 3: Add the table**

V `src-wails/db.go`, do slice `stmts` v `migrate()`:

```go
		// One row per PTY or chat. The phase used to live in Terminal.vue, so
		// it existed only for a mounted workspace and an app restart threw it
		// away. Here it survives both.
		`CREATE TABLE IF NOT EXISTS pty_phase (
			id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			is_agent INTEGER NOT NULL DEFAULT 0,
			turn_ended_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
```

- [ ] **Step 4: Write the store**

```go
// src-wails/phasestore.go
package main

import (
	"database/sql"
	"log"
	"sync"
	"time"

	"burrow/internal/agentphase"
)

// PhaseStore owns every agent phase in this environment: PTYs keyed
// "pty:<id>", chats keyed "chat:<id>". One store and one phase type for both,
// because two derivations of the same thing is exactly how the mobile client's
// chat dots drifted from its terminal dots.
type PhaseStore struct {
	mu     sync.Mutex
	db     *sql.DB
	phases map[string]agentphase.Phase
}

func NewPhaseStore(db *sql.DB) (*PhaseStore, error) {
	s := &PhaseStore{db: db, phases: make(map[string]agentphase.Phase)}
	rows, err := db.Query(`SELECT id, state, detail, model, title, is_agent, turn_ended_at, updated_at FROM pty_phase`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var p agentphase.Phase
		if err := rows.Scan(&id, &p.State, &p.Detail, &p.Model, &p.Title, &p.IsAgent, &p.TurnEndedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		s.phases[id] = p
	}
	return s, rows.Err()
}

// Apply advances one phase. A no-op event writes nothing and emits nothing —
// the pure Next() returning an unchanged value is what makes that cheap.
func (s *PhaseStore) Apply(id string, ev agentphase.Event) {
	s.mu.Lock()
	cur := s.phases[id]
	next := agentphase.Next(cur, ev, time.Now().UnixMilli())
	if next == cur {
		s.mu.Unlock()
		return
	}
	s.phases[id] = next
	s.mu.Unlock()

	s.persist(id, next)
	busEmit("phase-"+id, next)
}

func (s *PhaseStore) persist(id string, p agentphase.Phase) {
	_, err := s.db.Exec(
		`INSERT INTO pty_phase (id, state, detail, model, title, is_agent, turn_ended_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   state=excluded.state, detail=excluded.detail, model=excluded.model,
		   title=excluded.title, is_agent=excluded.is_agent,
		   turn_ended_at=excluded.turn_ended_at, updated_at=excluded.updated_at`,
		id, string(p.State), p.Detail, p.Model, p.Title, p.IsAgent, p.TurnEndedAt, p.UpdatedAt,
	)
	if err != nil {
		log.Printf("persist phase %s: %v", id, err)
	}
}

func (s *PhaseStore) Get(id string) agentphase.Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phases[id]
}

// All is what the snapshot RPC will hand a connecting client (phase 4).
func (s *PhaseStore) All() map[string]agentphase.Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]agentphase.Phase, len(s.phases))
	for k, v := range s.phases {
		out[k] = v
	}
	return out
}

// Replay re-emits a known phase after a view attaches. A terminal thread can
// already be running before its XTerm mounts; without this its first hook is
// lost and the dot stays idle until the next one arrives.
func (s *PhaseStore) Replay(id string) {
	s.mu.Lock()
	p, ok := s.phases[id]
	s.mu.Unlock()
	if ok {
		busEmit("phase-"+id, p)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd src-wails && go test ./... -run TestPhaseStore -v`
Expected: PASS

- [ ] **Step 6: Wire into startup**

V `src-wails/app.go` přidej pole do `App`:

```go
	phases *PhaseStore
```

A v `startup` hned za `a.db = db`:

```go
	if ps, err := NewPhaseStore(db); err != nil {
		log.Printf("phase store: %v", err)
	} else {
		a.phases = ps
	}
```

- [ ] **Step 7: Move `set_tab_live_status` to Go**

`SetTabLiveStatus` (`src-wails/misc.go:84`) je dnes volaný z `Terminal.vue` při každé změně statusu — mirror do DB pro `burrow list-tabs`. Go teď fázi zná sám, takže ten round-trip je zbytečný. V `PhaseStore.Apply`, hned za `busEmit`:

```go
	// Mirror for `burrow list-tabs` / MCP list_tabs, which read the DB with no
	// frontend round-trip. The frontend used to make this call itself.
	if ptyID, ok := strings.CutPrefix(id, "pty:"); ok {
		setTabLiveStatus(s.db, ptyID, string(next.State))
	}
```

Vytáhni tělo `App.SetTabLiveStatus` do `func setTabLiveStatus(db *sql.DB, ptyID, status string)` v `misc.go` a nech `App.SetTabLiveStatus` volat ji (binding zatím zůstává; ruší se v Tasku 12).

- [ ] **Step 8: Run tests + build**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add src-wails
git commit -m "feat(phase): persist agent phases in SQLite and emit phase-{id}"
```

---

### Task 8: hooky krmí `PhaseStore`

**Files:**
- Modify: `src-wails/hookserver.go`
- Test: `src-wails/hookserver_test.go` (nový)

**Interfaces:**
- Consumes: `PhaseStore.Apply`/`Replay` (Task 7)
- Produces: `func hookEvent(p hookPayload) (agentphase.Event, bool)` — čistý překlad hook payloadu na event

`pty-hook-{id}` emit v tomhle tasku **zůstává** — frontend na něm pořád visí a padne až v Tasku 12. Nový `phase-{id}` jede vedle něj.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/hookserver_test.go
package main

import (
	"testing"

	"burrow/internal/agentphase"
)

func TestHookEventMapping(t *testing.T) {
	cases := []struct {
		state string
		want  agentphase.Kind
	}{
		{"running", agentphase.HookRunning},
		{"waiting", agentphase.HookWaiting},
		{"permission", agentphase.HookPermission},
		{"done", agentphase.HookDone},
		{"error", agentphase.HookError},
		{"session", agentphase.HookSession},
	}
	for _, c := range cases {
		ev, ok := hookEvent(hookPayload{PtyID: "7", State: c.state})
		if !ok {
			t.Fatalf("%q was dropped", c.state)
		}
		if ev.Kind != c.want {
			t.Fatalf("%q → %q, want %q", c.state, ev.Kind, c.want)
		}
	}
}

func TestHookEventDropsUnknownState(t *testing.T) {
	if _, ok := hookEvent(hookPayload{PtyID: "7", State: "sparkles"}); ok {
		t.Fatal("an unknown hook state must not move the phase")
	}
}

func TestHookEventCarriesDetailAndSessionMetadata(t *testing.T) {
	ev, _ := hookEvent(hookPayload{PtyID: "7", State: "error", Detail: "billing_error"})
	if ev.Detail != "billing_error" {
		t.Fatalf("detail lost: %q", ev.Detail)
	}
	ev, _ = hookEvent(hookPayload{PtyID: "7", State: "session", Model: "opus", Title: "Fix parser"})
	if ev.Model != "opus" || ev.Title != "Fix parser" {
		t.Fatalf("session metadata lost: %+v", ev)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestHookEvent`
Expected: FAIL — `undefined: hookEvent`

- [ ] **Step 3: Write minimal implementation**

Do `src-wails/hookserver.go`:

```go
// hookEvent translates one `burrow status` POST into a phase event. An unknown
// state returns false: a hook nobody planned for must not move the dot.
func hookEvent(p hookPayload) (agentphase.Event, bool) {
	switch p.State {
	case "running":
		return agentphase.Event{Kind: agentphase.HookRunning}, true
	case "waiting":
		return agentphase.Event{Kind: agentphase.HookWaiting}, true
	case "permission":
		return agentphase.Event{Kind: agentphase.HookPermission}, true
	case "done":
		return agentphase.Event{Kind: agentphase.HookDone}, true
	case "error":
		return agentphase.Event{Kind: agentphase.HookError, Detail: p.Detail}, true
	case "session":
		return agentphase.Event{Kind: agentphase.HookSession, Model: p.Model, Source: p.Source, Title: p.Title}, true
	}
	return agentphase.Event{}, false
}
```

Přidej `phases *PhaseStore` do `HookServer` a parametr do `StartHookServer`. V `handleStatus`, za existující `h.emitStatus(p)`:

```go
	if h.phases != nil && p.PtyID != "" {
		if ev, ok := hookEvent(p); ok {
			h.phases.Apply("pty:"+p.PtyID, ev)
		}
	}
```

A v `ReplayStatus`, za `h.emitStatus(p)`:

```go
	if h.phases != nil {
		h.phases.Replay("pty:" + ptyID)
	}
```

Volání `StartHookServer` v `app.go` doplň o `a.phases`.

- [ ] **Step 4: Run tests + build**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Manual check**

Run: `just dev`, otevři terminál, spusť `claude`, pošli prompt.
Ověř v konzoli devtools: `phase-pty:<id>` eventy chodí a `pty-hook-<id>` chodí pořád taky.

- [ ] **Step 6: Commit**

```bash
git add src-wails
git commit -m "feat(phase): route status hooks into the phase store"
```

---

### Task 9: Go-side poll + dead-PTY watchdog

**Files:**
- Create: `src-wails/phasepoll.go`, `src-wails/phasepoll_test.go`
- Modify: `src-wails/app.go` (`startup`)

**Interfaces:**
- Consumes: `App.ListPtySessions` (`app.go:276`), `App.GetPtyForeground` (`app.go:289`), `PhaseStore.Apply`
- Produces: `type phasePoller struct{…}`; `func (p *phasePoller) tick()`; `func startPhasePoll(ctx context.Context, ps *PhaseStore, list func() ([]string, error), fg func(string) string)`

Watchdog: prázdný foreground **3× po sobě** a PTY není v `ListPtySessions` → `Dead`. Jedno prázdné čtení je přechodný závod s daemonem a ignoruje se.

`SHELL_RE` a rozpoznání agenta zůstávají — přesouvají se sem z `XTerm.vue`.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/phasepoll_test.go
package main

import (
	"testing"

	"burrow/internal/agentphase"
)

type fakePty struct {
	sessions []string
	fg       map[string]string
}

func (f *fakePty) list() ([]string, error) { return f.sessions, nil }
func (f *fakePty) foreground(id string) string {
	return f.fg[id]
}

func TestPollMarksAgentLeaves(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	f := &fakePty{sessions: []string{"7"}, fg: map[string]string{"7": "claude"}}
	p := newPhasePoller(s, f.list, f.foreground)

	p.tick()

	if !s.Get("pty:7").IsAgent {
		t.Fatal("agent foreground did not set IsAgent")
	}
	if s.Get("pty:7").State == agentphase.Running {
		t.Fatal("the poll fabricated running for an agent — hooks are the sole authority")
	}
}

func TestPollDrivesPlainCommands(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	f := &fakePty{sessions: []string{"7"}, fg: map[string]string{"7": "npm"}}
	p := newPhasePoller(s, f.list, f.foreground)

	p.tick()
	if s.Get("pty:7").State != agentphase.Running {
		t.Fatalf("plain command did not go running: %+v", s.Get("pty:7"))
	}

	f.fg["7"] = "zsh" // back at the prompt
	p.tick()
	if s.Get("pty:7").State != agentphase.Done {
		t.Fatalf("returning to the shell did not settle: %+v", s.Get("pty:7"))
	}
}

func TestWatchdogNeedsThreeEmptyReadsAndADeadPty(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.PollAgent, Bool: true})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	f := &fakePty{sessions: []string{"7"}, fg: map[string]string{"7": ""}}
	p := newPhasePoller(s, f.list, f.foreground)

	for i := 0; i < 5; i++ {
		p.tick()
	}
	if s.Get("pty:7").State != agentphase.Running {
		t.Fatalf("an empty foreground on a LIVE pty settled the dot: %+v", s.Get("pty:7"))
	}

	f.sessions = nil // the daemon confirms it is gone
	p.tick()
	if s.Get("pty:7").State != agentphase.Stale {
		t.Fatalf("dead pty did not go stale: %+v", s.Get("pty:7"))
	}
}

func TestWatchdogIgnoresASingleEmptyRead(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.PollAgent, Bool: true})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	f := &fakePty{sessions: nil, fg: map[string]string{"7": ""}}
	p := newPhasePoller(s, f.list, f.foreground)
	p.tick()

	if s.Get("pty:7").State != agentphase.Running {
		t.Fatalf("one empty read is a daemon race, not a death: %+v", s.Get("pty:7"))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestPoll|TestWatchdog'`
Expected: FAIL — `undefined: newPhasePoller`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/phasepoll.go
package main

import (
	"context"
	"regexp"
	"time"

	"burrow/internal/agentphase"
)

// shellRE matches an interactive shell sitting at its prompt. The daemon
// reports the shell BY NAME when it is foreground — that is how we learn a
// command or agent has exited.
var shellRE = regexp.MustCompile(`^(zsh|bash|sh|fish|csh|tcsh|dash)$`)

// agentRE matches the CLIs we treat as agents. For an agent leaf the poll only
// ever sets IsAgent: an agent is foreground whether it is thinking or idle at
// its prompt, so presence is not busy (the old stuck-orange-dot bug).
var agentRE = regexp.MustCompile(`^(claude|codex|aider|gemini|opencode|amp|goose)$`)

// emptyReadsBeforeDead is the watchdog's patience. One empty foreground read is
// a transient race with the daemon; three plus a pty the daemon no longer
// lists is a process that died without a Stop hook.
const emptyReadsBeforeDead = 3

type phasePoller struct {
	phases *PhaseStore
	list   func() ([]string, error)
	fg     func(string) string
	empty  map[string]int
}

func newPhasePoller(ps *PhaseStore, list func() ([]string, error), fg func(string) string) *phasePoller {
	return &phasePoller{phases: ps, list: list, fg: fg, empty: make(map[string]int)}
}

func (p *phasePoller) tick() {
	ids, err := p.list()
	if err != nil {
		return
	}
	alive := make(map[string]bool, len(ids))
	for _, id := range ids {
		alive[id] = true
	}

	// Every pty the store knows about, not just the live ones: a pty that just
	// died is exactly the case the watchdog exists for.
	seen := make(map[string]bool)
	for key := range p.phases.All() {
		id, ok := cutPtyKey(key)
		if !ok {
			continue
		}
		seen[id] = true
		p.pollOne(id, alive[id])
	}
	for _, id := range ids {
		if !seen[id] {
			p.pollOne(id, true)
		}
	}
}

func (p *phasePoller) pollOne(id string, alive bool) {
	key := "pty:" + id
	name := p.fg(id)

	if name == "" {
		p.empty[id]++
		if p.empty[id] >= emptyReadsBeforeDead && !alive {
			p.phases.Apply(key, agentphase.Event{Kind: agentphase.Dead})
		}
		return
	}
	p.empty[id] = 0

	switch {
	case agentRE.MatchString(name):
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollAgent, Bool: true})
	case shellRE.MatchString(name):
		// Back at the prompt: whatever ran is over. This also rescues an agent
		// the user Ctrl+C'd, which fires no Stop hook.
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollAgent, Bool: false})
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollNotBusy})
	default:
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollAgent, Bool: false})
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollBusy})
	}
}

func cutPtyKey(key string) (string, bool) {
	const prefix = "pty:"
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		return key[len(prefix):], true
	}
	return "", false
}

// startPhasePoll runs the poll for the life of the app. It lives on the server
// side because the phase must be derivable with no client attached.
func startPhasePoll(ctx context.Context, ps *PhaseStore, list func() ([]string, error), fg func(string) string) {
	p := newPhasePoller(ps, list, fg)
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.tick()
			}
		}
	}()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd src-wails && go test ./... -run 'TestPoll|TestWatchdog' -v`
Expected: PASS

- [ ] **Step 5: Wire into startup**

V `src-wails/app.go`, na konci `startup` (po vytvoření daemona i phase store):

```go
	if a.phases != nil {
		startPhasePoll(ctx, a.phases, a.ListPtySessions, a.GetPtyForeground)
	}
```

- [ ] **Step 6: Run tests + build**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add src-wails
git commit -m "feat(phase): server-side foreground poll and dead-PTY watchdog"
```

---

### Task 10: chat fáze z provider runtime

**Files:**
- Modify: `src-wails/providerruntime.go`
- Test: `src-wails/providerruntime_test.go` (existující soubor, přidej případy)

**Interfaces:**
- Consumes: `ProviderRuntimeEvent` z `providerruntime.go`, `PhaseStore.Apply`
- Produces: `func chatPhaseEvent(ev ProviderRuntimeEvent) (agentphase.Event, bool)`

Chat dostává **tutéž** `agentphase.Phase` pod klíčem `chat:<id>`. Mapa: `user.delta`/`text.delta` → `HookRunning`, `turn.completed` → `HookDone`, `turn.failed` → `HookError`, `session.title` → `HookSession`. Ostatní eventy fázi nehýbou.

- [ ] **Step 1: Write the failing test**

```go
// přidat do src-wails/providerruntime_test.go
func TestChatPhaseEventMapping(t *testing.T) {
	cases := []struct {
		in   string
		want agentphase.Kind
		ok   bool
	}{
		{"text.delta", agentphase.HookRunning, true},
		{"user.delta", agentphase.HookRunning, true},
		{"turn.completed", agentphase.HookDone, true},
		{"turn.failed", agentphase.HookError, true},
		{"session.title", agentphase.HookSession, true},
		{"tool.started", "", false},
		{"thinking.delta", "", false},
	}
	for _, c := range cases {
		ev, ok := chatPhaseEvent(ProviderRuntimeEvent{Type: c.in})
		if ok != c.ok {
			t.Fatalf("%q: ok=%v, want %v", c.in, ok, c.ok)
		}
		if ok && ev.Kind != c.want {
			t.Fatalf("%q → %q, want %q", c.in, ev.Kind, c.want)
		}
	}
}
```

Pole jsou ověřená proti `src-wails/providerruntime.go:31`: `Type`, `Message` (nese důvod u `turn.failed`), `Title`. Vocabulary konstanty (`EvtTextDelta`, `EvtTurnCompleted`, …) jsou tamtéž od řádku 57 — použij je místo string literálů.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestChatPhaseEvent`
Expected: FAIL — `undefined: chatPhaseEvent`

- [ ] **Step 3: Write minimal implementation**

Do `src-wails/providerruntime.go`:

```go
// chatPhaseEvent maps a provider runtime event onto a phase event. A chat and a
// PTY carry the SAME phase type: two derivations of "is this agent busy" is how
// the mobile client's chat dots drifted from its terminal dots.
func chatPhaseEvent(ev ProviderRuntimeEvent) (agentphase.Event, bool) {
	switch ev.Type {
	case EvtTextDelta, EvtUserDelta:
		return agentphase.Event{Kind: agentphase.HookRunning}, true
	case EvtTurnCompleted:
		return agentphase.Event{Kind: agentphase.HookDone}, true
	case EvtTurnFailed:
		return agentphase.Event{Kind: agentphase.HookError, Detail: ev.Message}, true
	case EvtSessionTitle:
		return agentphase.Event{Kind: agentphase.HookSession, Title: ev.Title}, true
	}
	return agentphase.Event{}, false
}
```

V místě, kde se `ProviderRuntimeEvent`y emitují na `chat-event-{chatId}`, přidej za emit:

```go
	if phases != nil {
		if pev, ok := chatPhaseEvent(e); ok {
			phases.Apply("chat:"+chatID, pev)
		}
	}
```

(`phases` doruč stejnou cestou, jakou se sem dostává `chatID` — konstruktorem toho typu, který runtime eventy vydává.)

- [ ] **Step 4: Run tests + build**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src-wails
git commit -m "feat(phase): derive chat phases from provider runtime events"
```

---

### Task 11: `displayStatus()` — read receipt na klientovi

**Files:**
- Create: `src/runtime/displayStatus.ts`, `src/runtime/displayStatus.test.ts`

**Interfaces:**
- Consumes: `TermStatus` z `src/lib/terminalStatus.ts`
- Produces:
  - `export interface Phase { state: PhaseState; detail?: string; model?: string; title?: string; is_agent: boolean; turn_ended_at: number; updated_at: number }`
  - `export function displayStatus(phase: Phase | undefined, seenAt: number, watching: boolean): TermStatus`
  - `export function shouldMarkSeen(phase: Phase | undefined, watching: boolean): boolean`
  - `export const DONE_AUTOCLEAR_MS = 4000`

- [ ] **Step 1: Write the failing test**

```ts
// src/runtime/displayStatus.test.ts
import { describe, it, expect } from "vitest";
import { displayStatus, shouldMarkSeen, type Phase } from "./displayStatus";

const phase = (p: Partial<Phase>): Phase => ({
  state: "idle",
  is_agent: false,
  turn_ended_at: 0,
  updated_at: 0,
  ...p,
});

describe("displayStatus", () => {
  it("maps in-flight phases straight through", () => {
    expect(displayStatus(phase({ state: "running" }), 0, true)).toBe("running");
    expect(displayStatus(phase({ state: "waiting_input" }), 0, true)).toBe("waiting");
    expect(displayStatus(phase({ state: "waiting_approval" }), 0, true)).toBe("permission");
  });

  it("shows done while watching and review while away", () => {
    const p = phase({ state: "done", turn_ended_at: 100 });
    expect(displayStatus(p, 0, true)).toBe("done");
    expect(displayStatus(p, 0, false)).toBe("review");
  });

  it("clears once the turn has been seen", () => {
    const p = phase({ state: "done", turn_ended_at: 100 });
    expect(displayStatus(p, 100, false)).toBe("idle");
    expect(displayStatus(p, 200, false)).toBe("idle");
  });

  it("keeps a failed turn until it is seen, watching or not", () => {
    const p = phase({ state: "failed", detail: "billing_error", turn_ended_at: 100 });
    expect(displayStatus(p, 0, true)).toBe("error");
    expect(displayStatus(p, 0, false)).toBe("error");
    expect(displayStatus(p, 100, true)).toBe("idle");
  });

  it("treats a newer turn as unseen again", () => {
    const p = phase({ state: "done", turn_ended_at: 300 });
    expect(displayStatus(p, 100, false)).toBe("review");
  });

  it("settles a stale pty without shouting about it", () => {
    expect(displayStatus(phase({ state: "stale", turn_ended_at: 100 }), 0, false)).toBe("idle");
  });

  it("is idle for an unknown id", () => {
    expect(displayStatus(undefined, 0, false)).toBe("idle");
  });
});

describe("shouldMarkSeen", () => {
  it("marks a finished turn seen only while watching", () => {
    const p = phase({ state: "done", turn_ended_at: 100 });
    expect(shouldMarkSeen(p, true)).toBe(true);
    expect(shouldMarkSeen(p, false)).toBe(false);
  });

  it("never auto-marks a failed turn", () => {
    const p = phase({ state: "failed", turn_ended_at: 100 });
    expect(shouldMarkSeen(p, true)).toBe(false);
  });

  it("never marks an in-flight turn", () => {
    expect(shouldMarkSeen(phase({ state: "running" }), true)).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- displayStatus`
Expected: FAIL — module not found

- [ ] **Step 3: Write minimal implementation**

```ts
// src/runtime/displayStatus.ts
/**
 * The whole client-side status derivation.
 *
 * The backend owns the PHASE (see src-wails/internal/agentphase): what the
 * agent is doing, derivable with no client attached. What a client owns is
 * whether it has LOOKED yet — `review` and the transient `done` are read
 * receipts, not states, because the answer differs per device: the desktop may
 * be staring at the tab while the phone has never opened it.
 *
 * This file must not import Vue components, stores, or xterm.
 */
import type { TermStatus } from "../lib/terminalStatus";

export type PhaseState =
  | "idle"
  | "running"
  | "waiting_input"
  | "waiting_approval"
  | "done"
  | "failed"
  | "stale";

/** Wire shape of src-wails/internal/agentphase.Phase. */
export interface Phase {
  state: PhaseState;
  detail?: string;
  model?: string;
  title?: string;
  is_agent: boolean;
  turn_ended_at: number;
  updated_at: number;
}

/** How long a finished turn stays lime before it marks itself seen. */
export const DONE_AUTOCLEAR_MS = 4000;

function unseen(phase: Phase, seenAt: number): boolean {
  return phase.turn_ended_at > seenAt;
}

export function displayStatus(
  phase: Phase | undefined,
  seenAt: number,
  watching: boolean,
): TermStatus {
  if (!phase) return "idle";
  switch (phase.state) {
    case "running":
      return "running";
    case "waiting_input":
      return "waiting";
    case "waiting_approval":
      return "permission";
    case "failed":
      // A failed turn persists until seen whether or not anyone is watching:
      // the user must find out the turn died.
      return unseen(phase, seenAt) ? "error" : "idle";
    case "done":
      if (!unseen(phase, seenAt)) return "idle";
      return watching ? "done" : "review";
    // A stale PTY settles quietly — nothing failed, the process just went away.
    case "stale":
    case "idle":
    default:
      return "idle";
  }
}

/**
 * True when the client may mark this turn seen on its own (after
 * DONE_AUTOCLEAR_MS). A failed turn never auto-clears — only opening the tab
 * dismisses it.
 */
export function shouldMarkSeen(phase: Phase | undefined, watching: boolean): boolean {
  return !!phase && phase.state === "done" && watching;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- displayStatus`
Expected: PASS

- [ ] **Step 5: Add the import-boundary test**

```ts
// src/runtime/boundary.test.ts
import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// src/runtime is shared by the desktop shell and the PWA. If it can reach into
// components or stores, the two clients stop being able to share it — which is
// how src/mobile ended up with its own copy of the status logic.
const FORBIDDEN = ["@/components", "@/views", "@/stores", "@/mobile", "../components", "../stores", "xterm"];

describe("src/runtime import boundary", () => {
  it("imports nothing from the UI layer", () => {
    const dir = join(__dirname);
    for (const f of readdirSync(dir)) {
      if (!f.endsWith(".ts") || f.endsWith(".test.ts")) continue;
      const src = readFileSync(join(dir, f), "utf8");
      for (const bad of FORBIDDEN) {
        expect(src.includes(`from "${bad}`), `${f} imports ${bad}`).toBe(false);
      }
    }
  });
});
```

- [ ] **Step 6: Run all frontend tests**

Run: `pnpm test`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add src/runtime
git commit -m "feat(runtime): client-side display status as a read receipt"
```

---

### Task 12: `Terminal.vue` na fáze; smrt `agentStatus.ts`

**Files:**
- Modify: `src/components/Terminal.vue`, `src/components/XTerm.vue`, `src/lib/terminalStatus.ts`, `src/lib/wailsCompat/core.ts`
- Delete: `src/machines/agentStatus.ts`, `src/machines/agentStatus.test.ts`

**Interfaces:**
- Consumes: `displayStatus`, `shouldMarkSeen`, `DONE_AUTOCLEAR_MS`, `Phase` (Task 11); Wails event `phase-pty:{id}` (Task 7)
- Produces: nic nového — tohle je swap

Tenhle task je jediný, kde se maže starý kanál. Dělej ho celý, nebo vůbec.

- [ ] **Step 1: Replace the actor registry with a phase registry**

V `src/components/Terminal.vue` nahraď import (`:188-189`):

```ts
import { displayStatus, shouldMarkSeen, DONE_AUTOCLEAR_MS, type Phase } from "@/runtime/displayStatus";
```

a `leafActors` (`:570`):

```ts
// Server-owned phase per leaf, keyed by pty id. The status shown is a pure
// function of (phase, seenAt, watching) — there is no client state machine
// any more, so "I looked at it" can no longer race a transition.
const leafPhases = reactive(new Map<number, Phase>());
const leafSeenAt = reactive(new Map<number, number>());
const doneTimers = new Map<number, ReturnType<typeof setTimeout>>();
```

`seenAt` přežívá restart v `localStorage` pod klíčem `burrow.seenAt`:

```ts
function loadSeenAt() {
  try {
    const raw = JSON.parse(localStorage.getItem("burrow.seenAt") ?? "{}") as Record<string, number>;
    for (const [k, v] of Object.entries(raw)) leafSeenAt.set(Number(k), v);
  } catch { /* a corrupt receipt file just means everything looks unread */ }
}

function persistSeenAt() {
  localStorage.setItem("burrow.seenAt", JSON.stringify(Object.fromEntries(leafSeenAt)));
}
```

- [ ] **Step 2: Replace registerLeafListeners' actor block**

Nahraď blok `createActor(...)` + `actor.subscribe(...)` (`:600-627`) tímto:

```ts
  const applyPhase = (leafId: number, phase: Phase) => {
    const prevStatus = locateLeaf(leafId)?.leaf.status;
    leafPhases.set(leafId, phase);
    const found = locateLeaf(leafId);
    if (!found) return;
    const { tab, leaf } = found;
    const watching = isWatching(tab);
    const status = displayStatus(phase, leafSeenAt.get(leafId) ?? 0, watching);

    leaf.status = status;
    leaf.statusDetail = phase.detail || undefined;
    leaf.busy = status === "running" || status === "waiting" || status === "permission";
    leaf.isAgent = phase.is_agent;
    if (phase.model) leaf.model = phase.model;
    if (phase.title && isDefaultTitle(leaf.title)) leaf.title = phase.title;

    // Side effects fire on ENTERING a status, exactly as the machine's entry
    // actions did — comparing against the previous status is what keeps a
    // repeated phase event from re-notifying.
    if (status !== prevStatus) {
      if (status === "waiting" || status === "permission") playSound("waiting");
      if (status === "done") onTurnSettled(leafId);
      if (status === "review") { playSound("done"); onTurnSettled(leafId); }
      if (status === "error") maybeNtfy("error", leaf.title);
      if (status === "running" && prevStatus !== "running") {
        leaf.round = (leaf.round ?? 0) + 1;
        invoke("create_checkpoint", { cwd: leaf.cwd ?? props.cwd, label: `turn ${leaf.round}` }).catch(() => {});
      }
    }

    // A finished turn the user is watching marks itself seen after 4 s. This is
    // the transient `done` from the old machine, expressed as a receipt.
    clearTimeout(doneTimers.get(leafId));
    if (shouldMarkSeen(phase, watching)) {
      doneTimers.set(leafId, setTimeout(() => markLeafSeen(leafId), DONE_AUTOCLEAR_MS));
    }
  };
```

A do `Promise.all([...])` listenerů přidej:

```ts
    listen<Phase>(`phase-pty:${leafId}`, (ev) => applyPhase(leafId, ev.payload)),
```

Import `isDefaultTitle` z `@/lib/terminalStatus`, pokud tam ještě není.

- [ ] **Step 3: Replace markTabSeen**

```ts
function markLeafSeen(leafId: number) {
  const phase = leafPhases.get(leafId);
  if (!phase) return;
  leafSeenAt.set(leafId, Math.max(leafSeenAt.get(leafId) ?? 0, phase.turn_ended_at, Date.now()));
  persistSeenAt();
  applyPhase(leafId, phase); // recompute the dot with the new receipt
}

// Mark every finished TERMINAL leaf in a tab as seen. Chats mount only when on
// screen, so they mark themselves.
function markTabSeen(tab: Tab) {
  for (const leaf of getAllLeaves(tab.root)) {
    if (leaf.leafType === "chat") continue;
    markLeafSeen(leaf.id);
  }
}
```

`applyPhase` musí být deklarovaná v module scope (ne uvnitř `registerLeafListeners`), aby ji `markLeafSeen` viděla — vytáhni ji ven a předej `leafId` parametrem, jak je výše.

- [ ] **Step 4: Delete the dead handlers**

Smaž z `Terminal.vue`: `onAgentState` (`:790`), `onLeafBusy` (`:751`), `onLeafAgent`'s `send` (`:734` — funkce zůstává jen kvůli `leaf.isAgent`, ale ta se teď plní z fáze, takže celá mizí), `onLeafInterrupt` (`:881`), `onLeafNeedsInput` (`:887`), a odpovídající `@agent-state` / `@busy` / `@agent` / `@interrupt` / `@needs-input` atributy z `<XTerm>` v šabloně (`:127` a okolí).

Smaž volání `invoke("set_tab_live_status", …)` — Go ho teď píše sám (Task 7, Step 7). Smaž i binding `App.SetTabLiveStatus` z `misc.go` a jeho case v `wailsCompat/core.ts`.

- [ ] **Step 5: Strip status emits from XTerm.vue**

V `src/components/XTerm.vue`:
- z `defineEmits` (`:35`) odstraň `busy`, `needsInput`, `agentState`, `agentMeta`, `agent`, `interrupt`
- smaž listener `pty-hook-${props.ptyId}` (`:450-475`)
- v pollu (`:726-811`) nech **jen** odvození titulku z názvu procesu; smaž každé `emit("busy" | "interrupt" | "agentState" | "agent")`
- smaž `emit("interrupt")` na Ctrl+C (`:651`) a `emit("agentState","waiting")` z output scanu (`:562`)
- `SHELL_RE` zůstává — používá se pro titulek

- [ ] **Step 6: Delete the machine**

```bash
git rm src/machines/agentStatus.ts src/machines/agentStatus.test.ts
```

Z `src/lib/terminalStatus.ts` smaž `export type AgentEvent` (nikdo ho už neposílá).

- [ ] **Step 7: Typecheck and test**

Run: `pnpm build && pnpm test`
Expected: PASS. Pokud `vue-tsc` hlásí nepoužitý import `xstate`, odstraň ho i z `package.json`, jestli ho nic jiného nepoužívá (`rg "from \"xstate\"" src/`).

- [ ] **Step 8: Manual verification — the whole point of the phase**

Run: `just dev`

1. Spusť agenta v tabu, přepni na jiný workspace, počkej, až doběhne → tečka je **zelená (`review`)**, ne lime.
2. Klikni na tab → tečka zmizí.
3. Spusť agenta, dívej se na něj → po doběhnutí lime tečka, po 4 s zmizí.
4. Spusť agenta, **zavři a znovu spusť appku** → tečka je pořád tam (tohle dřív nešlo).
5. Spusť `npm test` v terminálu → oranžová po dobu běhu, po návratu k promptu se usadí.
6. Spusť agenta a `kill -9` jeho proces → do ~6 s se tečka usadí (`stale`), nezůstane viset.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor(status): derive terminal status from the server-owned phase

Deletes the XState machine. The phase now comes from Go, and review /
transient done are read receipts computed from turn_ended_at against a
per-device seenAt — so the dot survives a restart, exists for an unmounted
workspace, and 'I looked at it' can no longer race a transition."
```

---

### Task 13: dokumentace

**Files:**
- Modify: `CLAUDE.md`, `docs/context.html`

- [ ] **Step 1: Update CLAUDE.md**

V sekci **PTY / Agent state machine (`XTerm.vue`)**:
- přejmenuj na **PTY / Agent phase (`internal/agentphase`)**
- `pty-hook-{id}` → `phase-pty:{id}`, payload je celá `Phase`
- fáze žije v Go, persistuje v `pty_phase`, přežije restart
- foreground poll je v `phasepoll.go`, ne v `XTerm.vue`
- `review` a transient `done` jsou read receipty (`src/runtime/displayStatus.ts`), ne stavy
- `interrupt` → `stale`

V sekci **Backend** přidej `bus.go`, `phasestore.go`, `phasepoll.go`, `environment.go`, `endpoints.go` do seznamu souborů a poznámku, že `busEmit` je jediné dveře pro eventy.

- [ ] **Step 2: Update docs/context.html**

Stejné změny v tabulce Go bindings (`EnvironmentID`, `RemoteEndpoints` přibyly; `SetTabLiveStatus` zmizel) a v popisu status dotů.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md docs/context.html
git commit -m "docs: agent phase is owned by Go, not XTerm.vue"
```

---

## Self-review

**Spec coverage (fáze 1–2):**

| spec | task |
|---|---|
| §4 `environmentId` | 1 |
| §4 `EndpointProvider`, loopback, selection order | 2 |
| §4 tailscale provider | 3 |
| §4 `remote_endpoints` jako desktop RPC | 4 |
| §3 `agentphase` čistá funkce, stavy vč. `stale` | 5 |
| §1 event bus jako jediné dveře + grep test | 6 |
| §3 persist do SQLite, emit `phase-{id}` | 7 |
| §3 vstup: hooky | 8 |
| §3 vstup: poll + dead-PTY watchdog | 9 |
| §2 chat i PTY nesou týž typ fáze | 10 |
| §3 `review`/`done` jako read receipt | 11 |
| §5 smrt `agentStatus.ts`, `terminalStatus.ts` osekaný | 12 |
| §7 testy 2, 3, 4 | 6, 11, 5 |

Mimo tento plán, vědomě: §1 boot přes WS, §2 protokol/snapshot/resume, §4 auth, §5 `src/mobile` — to jsou fáze 3–7.

**Type consistency:** `agentphase.Phase` (Go, snake_case JSON) ↔ `Phase` (TS) — pole se shodují včetně `is_agent`, `turn_ended_at`, `updated_at`. Klíč eventu je `phase-pty:{id}` / `phase-chat:{id}` ve všech taskách (7, 8, 10, 12). `busEmit`/`busSubscribe`/`busReset` jednotně (6, 7, 9).

**Placeholders:** žádné. Jediné místo, kde plán říká „uprav podle skutečnosti", je Task 10 Step 1 (jméno pole na `ProviderRuntimeEvent`) — a je tam napsané, které jméno se hledá a proč.

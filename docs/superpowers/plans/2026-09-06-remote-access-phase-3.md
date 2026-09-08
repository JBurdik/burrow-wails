# Remote Access — Implementation Plan, fáze 3 (desktop na WS)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Desktopový Vue frontend přestane volat Wails bindings pro data a začne mluvit WebSocketem s lokálním Go serverem — jeden dispatch, jeden event fanout, jedna auth cesta, takže remote nemůže driftovat od desktopu, protože je to týž kód.

**Architecture:** Nový `/v2/ws` endpoint namountovaný na always-on loopback mux hook serveru. Rámce jsou JSON (`call`/`reply`/`event`/`welcome`). Co je vystavené, určuje explicitní tabulka `remoteAllowed` v `remoteapi.go` — wire cmd → `App` metoda + jména argumentů + scope; reflexe dělá **jen** konverzi typů. Na klientovi se `wailsCompat/core.ts` a `event.ts` z shimů nad Wails bindings stanou shimy nad WS transportem, takže ~130 call-sitů v `src/` se nedotkne ani jeden.

**Tech Stack:** Go 1.25 + `gorilla/websocket` (už v go.mod), `reflect`, `encoding/json`; Vue 3 + vitest.

**Spec:** `docs/superpowers/specs/2026-09-05-remote-access-t3code-design.md`

**Předchozí fáze:** `docs/superpowers/plans/2026-09-05-remote-access-phase-1-2.md` (hotová, commity `d87933b` → `29bcff5`)

## Global Constraints

- **Komentáře v kódu anglicky.** Commit subject anglicky, Conventional Commits.
- **Snapshot, resume ani binární PTY framy tenhle plán nedělá.** To je fáze 4. Desktop si stav dál natahuje jednotlivými `invoke` calls, jak to dělá dnes.
- **Staré `/ws` + `HTTPServer.dispatch` zůstává živé a nedotčené.** `src/mobile` na něm visí do fáze 6. Kdo ho smaže tady, rozbije telefon.
- **`/v2/ws` vyžaduje jednorázový ticket.** Loopback origin sám o sobě není autorizace — jinak by appku řídil libovolný lokální proces. Ticket vydává jen in-process Wails binding.
- **`remoteAllowed` je bezpečnostní hranice.** Reflexe smí dělat marshalling a nic jiného. Nová `App` metoda je mimo síť, dokud ji tam někdo vědomě nenapíše; vynuceno testem.
- **`busEmit` zůstává jediné dveře** pro eventy. Nový WS sink je druhý konzument busu, ne druhý emitor.
- **Nemodifikovat `src-wails/internal/agentphase`** — je to čistá funkce z fáze 2, tenhle plán se jí netýká.
- Go testy: `cd src-wails && go test ./...` a `go test -race ./...`. Frontend: `pnpm test`. Typecheck: `pnpm build`.
- `src/runtime/**` nesmí importovat z `src/components`, `src/views`, `src/stores`, `src/mobile`, ani `xterm` — hlídá `src/runtime/boundary.test.ts` z fáze 2. Neoslabovat ho, aby něco prošlo.

---

## File Structure

| soubor | odpovědnost |
|---|---|
| `src-wails/remoteapi.go` (nový) | `remoteCmd`, `remoteAllowed`, `remoteDenied`, `callApp()` — co je vystavené a konverze argumentů |
| `src-wails/remoteapi_test.go` (nový) | úplnost tabulky proti `App` metodám, konverze typů, chybějící/špatný argument |
| `src-wails/remoteproto.go` (nový) | tvary rámců + `encodeFrame`/`decodeFrame` |
| `src-wails/remoteproto_test.go` (nový) | round-trip, neznámý tag, poškozený JSON |
| `src-wails/remotews.go` (nový) | ticket store + `/v2/ws` handler: reader goroutine, jedna odchozí fronta, bus sink |
| `src-wails/remotews_test.go` (nový) | ticket jednorázový/expirující, call→reply, event fanout, pořadí |
| `src-wails/app.go` (modify) | `LocalEndpoint()` binding, mount `/v2/ws` do hook serveru |
| `src/runtime/reconnectBackoff.ts` (nový) | jitterovaný exponenciální backoff (portovaný z t3code) |
| `src/runtime/reconnectBackoff.test.ts` (nový) | monotonie, cap, reset |
| `src/runtime/transport.ts` (nový) | WS klient: `invoke`, `listen`, reconnect, re-ticket |
| `src/runtime/transport.test.ts` (nový) | proti fake WebSocketu: reply routing, error, reconnect, listen |
| `src/lib/wailsCompat/core.ts` (rewrite) | switch přes ~130 commandů → `transport.invoke` |
| `src/lib/wailsCompat/event.ts` (rewrite) | `EventsOn` → `transport.listen` |
| `CLAUDE.md` + `docs/context.html` (modify) | kudy teče desktop, co je `/v2/ws`, co ticket |

---

### Task 1: `remoteapi.go` — tabulka a konverze argumentů

**Files:**
- Create: `src-wails/remoteapi.go`
- Test: `src-wails/remoteapi_test.go`

**Interfaces:**
- Produces:
  - `type remoteScope string` s konstantami `scopeOrchRead`, `scopeOrchOperate`, `scopeTerminal`, `scopeAccessRead`, `scopeAccessWrite`
  - `type remoteCmd struct { Method string; Args []string; Scope remoteScope }`
  - `var remoteAllowed map[string]remoteCmd`
  - `var remoteDenied map[string]string` — jméno metody → důvod
  - `func callApp(recv any, c remoteCmd, args map[string]json.RawMessage) (any, error)`

**Pozor — dvě věci, které jinak spálí implementaci:**

1. **Jména parametrů v runtime neexistují.** Wails generuje `arg1, arg2, arg3` a Go `reflect` jména parametrů nevystavuje. Proto je nese `remoteCmd.Args` — poziční seznam wire jmen. Zdroj pravdy pro tu mapu je **současný switch v `src/lib/wailsCompat/core.ts`**: každý `case "x": return App.Y(args.a, args.b)` je jeden řádek tabulky. Přepiš to odtud, nevymýšlej.
2. **Frontend posílá `id` jako číslo, ale `CreatePty(id string, …)` chce string.** `core.ts` to dnes dělá `String(args.id)`. `json.Unmarshal` čísla do `*string` **selže**, takže konverze musí číslo na string umět sama. Bez toho nefunguje ani jeden PTY call.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/remoteapi_test.go
package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestRemoteSurfaceIsExhaustive is the security gate: a new App method is off
// the network until somebody puts it in one list or the other on purpose.
func TestRemoteSurfaceIsExhaustive(t *testing.T) {
	allowedMethods := make(map[string]bool, len(remoteAllowed))
	for wire, c := range remoteAllowed {
		if c.Method == "" {
			t.Errorf("remoteAllowed[%q] has no method", wire)
		}
		allowedMethods[c.Method] = true
	}

	appType := reflect.TypeOf(&App{})
	for i := 0; i < appType.NumMethod(); i++ {
		name := appType.Method(i).Name
		if allowedMethods[name] {
			continue
		}
		if _, ok := remoteDenied[name]; ok {
			continue
		}
		t.Errorf("App.%s is in neither remoteAllowed nor remoteDenied — decide "+
			"whether it belongs on the wire, then add it to one of them", name)
	}

	// A denied entry for a method that no longer exists is rot.
	for name := range remoteDenied {
		if _, ok := appType.MethodByName(name); !ok {
			t.Errorf("remoteDenied names App.%s, which does not exist", name)
		}
	}
}

func TestRemoteAllowedArityMatchesMethods(t *testing.T) {
	appType := reflect.TypeOf(&App{})
	for wire, c := range remoteAllowed {
		m, ok := appType.MethodByName(c.Method)
		if !ok {
			t.Errorf("%q: App.%s does not exist", wire, c.Method)
			continue
		}
		// m.Type includes the receiver, the args list does not.
		if want := m.Type.NumIn() - 1; want != len(c.Args) {
			t.Errorf("%q: App.%s takes %d args, table names %d (%v)",
				wire, c.Method, want, len(c.Args), c.Args)
		}
	}
}

func TestRemoteAllowedEveryCommandHasAScope(t *testing.T) {
	for wire, c := range remoteAllowed {
		if c.Scope == "" {
			t.Errorf("%q has no scope; pick the narrowest one that works", wire)
		}
	}
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeRecv carries one method of each return shape the App surface uses, so
// the marshalling can be tested without a daemon or a database.
//
// Do NOT reach for real App methods here: App.GetPtyForeground and
// App.ListPtySessions both dereference a.daemon with no nil guard, so calling
// them on a zero &App{} panics rather than returning an error.
type fakeRecv struct {
	gotString string
	gotInt    int
}

func (f *fakeRecv) TakeString(s string) string          { f.gotString = s; return s }
func (f *fakeRecv) TakeInt(n int)                       { f.gotInt = n }
func (f *fakeRecv) NoArgsWithError() ([]string, error)  { return nil, errors.New("boom") }
func (f *fakeRecv) NoArgsNoError() int                  { return 7 }

func TestCallAppCoercesNumberToString(t *testing.T) {
	// The frontend's pty id is its own numeric counter, and every Go PTY
	// method takes it as an opaque string key. core.ts used to String() it;
	// now the conversion has to.
	f := &fakeRecv{}
	got, err := callApp(f, remoteCmd{Method: "TakeString", Args: []string{"id"}},
		map[string]json.RawMessage{"id": raw(t, 7)})
	if err != nil {
		t.Fatalf("numeric id was not coerced to string: %v", err)
	}
	if got != "7" || f.gotString != "7" {
		t.Fatalf(`want "7", got %q (method saw %q)`, got, f.gotString)
	}
}

func TestCallAppKeepsARealString(t *testing.T) {
	f := &fakeRecv{}
	got, err := callApp(f, remoteCmd{Method: "TakeString", Args: []string{"id"}},
		map[string]json.RawMessage{"id": raw(t, "abc")})
	if err != nil || got != "abc" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCallAppMissingArgIsZeroValue(t *testing.T) {
	// core.ts passed `args.cwd ?? ""` — an absent optional arg is the zero
	// value, not an error.
	f := &fakeRecv{}
	got, err := callApp(f, remoteCmd{Method: "TakeString", Args: []string{"id"}},
		map[string]json.RawMessage{})
	if err != nil {
		t.Fatalf("missing arg should be the zero value: %v", err)
	}
	if got != "" {
		t.Fatalf("want the zero value, got %q", got)
	}
}

func TestCallAppNullArgIsZeroValue(t *testing.T) {
	f := &fakeRecv{}
	if _, err := callApp(f, remoteCmd{Method: "TakeInt", Args: []string{"n"}},
		map[string]json.RawMessage{"n": json.RawMessage("null")}); err != nil {
		t.Fatalf("explicit null should be the zero value: %v", err)
	}
	if f.gotInt != 0 {
		t.Fatalf("want 0, method saw %d", f.gotInt)
	}
}

func TestCallAppRejectsWrongType(t *testing.T) {
	f := &fakeRecv{}
	_, err := callApp(f, remoteCmd{Method: "TakeInt", Args: []string{"n"}},
		map[string]json.RawMessage{"n": raw(t, "not a number")})
	if err == nil {
		t.Fatal("a string where an int is wanted must be an error")
	}
	if !strings.Contains(err.Error(), "n") {
		t.Errorf("error should name the offending arg, got %q", err)
	}
}

func TestCallAppRejectsUnknownMethod(t *testing.T) {
	if _, err := callApp(&fakeRecv{}, remoteCmd{Method: "NoSuchMethod"}, nil); err == nil {
		t.Fatal("unknown method must be an error")
	}
}

func TestCallAppRejectsArityMismatch(t *testing.T) {
	if _, err := callApp(&fakeRecv{}, remoteCmd{Method: "TakeString", Args: nil}, nil); err == nil {
		t.Fatal("a table entry naming the wrong number of args must be an error")
	}
}

func TestCallAppSplitsTrailingError(t *testing.T) {
	// (T, error): the error becomes the call's error, never a result value.
	got, err := callApp(&fakeRecv{}, remoteCmd{Method: "NoArgsWithError", Args: nil}, nil)
	if err == nil {
		t.Fatal("a method's trailing error must become the call error")
	}
	if got != nil {
		t.Fatalf("a failed call must carry no result, got %v", got)
	}
}

func TestCallAppReturnsAResultWithNoError(t *testing.T) {
	got, err := callApp(&fakeRecv{}, remoteCmd{Method: "NoArgsNoError", Args: nil}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("want 7, got %v", got)
	}
}

func TestCallAppHandlesAVoidMethod(t *testing.T) {
	got, err := callApp(&fakeRecv{}, remoteCmd{Method: "TakeInt", Args: []string{"n"}},
		map[string]json.RawMessage{"n": raw(t, 3)})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("a void method must reply with a null result, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestRemote|TestCallApp'`
Expected: FAIL — `undefined: remoteAllowed`

- [ ] **Step 3: Write the reflection half**

```go
// src-wails/remoteapi.go
package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// remoteScope is the capability a command needs. Scopes are enforced per
// command, not per connection: holding a ticket is not authorization to call
// everything (t3code's environment-auth profile, same reasoning).
type remoteScope string

const (
	scopeOrchRead    remoteScope = "orchestration:read"
	scopeOrchOperate remoteScope = "orchestration:operate"
	scopeTerminal    remoteScope = "terminal:operate"
	scopeAccessRead  remoteScope = "access:read"
	scopeAccessWrite remoteScope = "access:write"
)

// remoteCmd is one exposed App method.
//
// Args names the wire keys POSITIONALLY, because parameter names do not exist
// at runtime: Go's reflect does not carry them and Wails generates arg1..argN.
// This table is therefore the only place that knows an argument's name — which
// is fine, because it is also the only place that decides what is reachable at
// all.
type remoteCmd struct {
	Method string
	Args   []string
	Scope  remoteScope
}

// callApp invokes an allowed method with JSON arguments. It does marshalling
// and nothing else: no defaulting beyond the zero value, no name guessing, no
// fallback to a method the table did not name.
//
// recv is `any` rather than *App so the marshalling can be unit-tested against
// a fake carrying every return shape — several real App methods dereference
// a.daemon with no nil guard and would panic on a zero value. Real callers
// pass the *App; that the Method strings name something real is what
// TestRemoteAllowedArityMatchesMethods checks.
func callApp(recv any, c remoteCmd, args map[string]json.RawMessage) (any, error) {
	m := reflect.ValueOf(recv).MethodByName(c.Method)
	if !m.IsValid() {
		return nil, fmt.Errorf("no such method %q", c.Method)
	}
	mt := m.Type()
	if mt.NumIn() != len(c.Args) {
		return nil, fmt.Errorf("%s takes %d args, table names %d", c.Method, mt.NumIn(), len(c.Args))
	}

	in := make([]reflect.Value, mt.NumIn())
	for i, name := range c.Args {
		pv := reflect.New(mt.In(i))
		if rawArg, ok := args[name]; ok && len(rawArg) > 0 && string(rawArg) != "null" {
			if err := unmarshalArg(rawArg, pv); err != nil {
				return nil, fmt.Errorf("arg %q: %w", name, err)
			}
		}
		// An absent or null argument stays the zero value — core.ts's
		// `args.cwd ?? ""` behaviour, preserved.
		in[i] = pv.Elem()
	}

	out := m.Call(in)
	return splitResult(out)
}

// unmarshalArg decodes one argument, with one deliberate coercion: a JSON
// number into a string parameter. The frontend's pty id is its own numeric
// counter while every Go PTY method takes it as an opaque string key, and
// core.ts used to bridge that with String(args.id). Without this, no PTY call
// works at all.
func unmarshalArg(rawArg json.RawMessage, pv reflect.Value) error {
	if pv.Elem().Kind() == reflect.String {
		var n json.Number
		if json.Unmarshal(rawArg, &n) == nil && len(rawArg) > 0 && rawArg[0] != '"' {
			pv.Elem().SetString(n.String())
			return nil
		}
	}
	return json.Unmarshal(rawArg, pv.Interface())
}

// splitResult turns a method's return values into (result, error). Go methods
// here come in four shapes: (), (error), (T), (T, error).
func splitResult(out []reflect.Value) (any, error) {
	errType := reflect.TypeOf((*error)(nil)).Elem()
	var err error
	if n := len(out); n > 0 && out[n-1].Type().Implements(errType) {
		if e := out[n-1].Interface(); e != nil {
			err = e.(error)
		}
		out = out[:n-1]
	}
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out[0].Interface(), nil
}

// itoa is used by the table's callers for id keys; kept here so the coercion
// and its inverse live together.
func itoa(i int64) string { return strconv.FormatInt(i, 10) }
```

- [ ] **Step 4: Write the table**

The table is a mechanical port. Open `src/lib/wailsCompat/core.ts` and walk its
`switch` top to bottom. Each `case` becomes one entry:

```ts
// core.ts
case "create_pty":
  return App.CreatePty(String(args.id), args.cwd ?? "", args.cols, args.rows);
```
becomes
```go
"create_pty": {Method: "CreatePty", Args: []string{"id", "cwd", "cols", "rows"}, Scope: scopeTerminal},
```

Rules for the port:
- Argument order in `Args` is the Go method's parameter order, and each name is
  the property `core.ts` reads off its `args` object.
- A `case` that returns `Promise.resolve()` without calling Go (like
  `detach_pty`) has no method — leave it out of the table entirely and handle
  it client-side, as Task 7 does.
- A `case` that post-processes the Go result in TS (like `list_pty_sessions`
  mapping ids into session records) keeps that post-processing on the client.
  The table exposes the raw method; Task 7 keeps the mapping.
- Scope: reads get `scopeOrchRead`; anything that mutates workspaces, tabs,
  chats or files gets `scopeOrchOperate`; PTY create/write/resize/kill get
  `scopeTerminal`; anything that lists or mutates pairing/devices gets
  `scopeAccessRead`/`scopeAccessWrite`.

Start the table like this and continue until `TestRemoteSurfaceIsExhaustive`
passes:

```go
// remoteAllowed is the whole remote surface. Ported one-for-one from the
// switch that used to live in src/lib/wailsCompat/core.ts, which is where the
// wire-name-to-method mapping has always been written down.
var remoteAllowed = map[string]remoteCmd{
	// PTY
	"create_pty":          {Method: "CreatePty", Args: []string{"id", "cwd", "cols", "rows"}, Scope: scopeTerminal},
	"write_pty":           {Method: "WritePty", Args: []string{"id", "data"}, Scope: scopeTerminal},
	"resize_pty":          {Method: "ResizePty", Args: []string{"id", "cols", "rows"}, Scope: scopeTerminal},
	"kill_pty":            {Method: "KillPty", Args: []string{"id"}, Scope: scopeTerminal},
	"get_pty_foreground":  {Method: "GetPtyForeground", Args: []string{"id"}, Scope: scopeOrchRead},
	"list_pty_sessions":   {Method: "ListPtySessions", Args: nil, Scope: scopeOrchRead},

	// Environment. environment_id is also what the WS handler test calls,
	// because it is safe on a bare &App{} — most commands are not.
	"environment_id":   {Method: "EnvironmentID", Args: nil, Scope: scopeOrchRead},
	"remote_endpoints": {Method: "RemoteEndpoints", Args: nil, Scope: scopeAccessRead},

	// Workspaces
	"list_workspaces":   {Method: "ListWorkspaces", Args: nil, Scope: scopeOrchRead},
	"create_workspace":  {Method: "CreateWorkspace", Args: []string{"name", "path"}, Scope: scopeOrchOperate},
	"delete_workspace":  {Method: "DeleteWorkspace", Args: []string{"id"}, Scope: scopeOrchOperate},
	"rename_workspace":  {Method: "RenameWorkspace", Args: []string{"id", "name"}, Scope: scopeOrchOperate},
	// … continue for every case in core.ts's switch
}

// remoteDenied names the App methods that are deliberately NOT reachable over
// the wire, each with the reason. An entry here is a decision, not a TODO.
var remoteDenied = map[string]string{
	"LocalEndpoint": "issues the ticket that authorizes a connection; reachable only in-process",
	// … see Step 5
}
```

- [ ] **Step 5: Fill `remoteDenied` and let the test tell you what is left**

Run `cd src-wails && go test ./... -run TestRemoteSurfaceIsExhaustive` and work
the failures. Every name it prints is a decision you must make. Deny anything in
these families, with the reason as the map value:

- **Window / native chrome** — `SaveWindowState`, `RestoreWindowState`, dialogs:
  `"desktop window chrome; meaningless to a remote client"`
- **Updater** — `CheckUpdate`, `InstallUpdate`, `RelaunchApp`:
  `"swaps the running .app bundle; must not be triggerable over the network"`
- **Ticket / auth** — `LocalEndpoint`:
  `"issues the ticket that authorizes a connection; reachable only in-process"`
- **Hook/control plumbing** — `AckControlAction`:
  `"the UI's ack channel for a UI-performed verb; not a client call"`

If a method does not obviously belong in a family, put it in `remoteAllowed`
with the narrowest scope that works. Being reachable is the normal case; the
deny list is for things whose reachability would be a bug.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd src-wails && go test ./... -run 'TestRemote|TestCallApp' -v`
Expected: PASS

- [ ] **Step 7: Full suite + commit**

Run: `cd src-wails && go build ./... && go test ./...`

```bash
git add src-wails/remoteapi.go src-wails/remoteapi_test.go
git commit -m "feat(remote): explicit command table over App methods

Parameter names do not exist at runtime — reflect does not carry them and
Wails generates arg1..argN — so the table names them positionally. That
also makes it the single place deciding what is reachable, which a test
pins against every App method."
```

---

### Task 2: rámce protokolu

**Files:**
- Create: `src-wails/remoteproto.go`
- Test: `src-wails/remoteproto_test.go`

**Interfaces:**
- Produces:
  - `type clientFrame struct { T string; ID int64; Cmd string; Args map[string]json.RawMessage }`
  - `type serverFrame struct { T string; ID int64; Result any; Error *frameError; Name string; Payload any; EnvironmentID string; Scopes []remoteScope }`
  - `type frameError struct { Code, Message string }`
  - `func decodeClientFrame(b []byte) (clientFrame, error)`
  - `func replyFrame(id int64, result any) serverFrame`, `func errorFrame(id int64, code, msg string) serverFrame`, `func eventFrame(name string, payload any) serverFrame`, `func welcomeFrame(envID string, scopes []remoteScope) serverFrame`

Snapshot a resume rámce (`shell`, `resume`, `resync`) tenhle plán **nedělá** — jsou fáze 4. Nezakládej pro ně prázdné tvary; YAGNI.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/remoteproto_test.go
package main

import (
	"encoding/json"
	"testing"
)

func TestDecodeClientFrameCall(t *testing.T) {
	f, err := decodeClientFrame([]byte(`{"t":"call","id":3,"cmd":"list_workspaces","args":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if f.T != "call" || f.ID != 3 || f.Cmd != "list_workspaces" {
		t.Fatalf("bad decode: %+v", f)
	}
}

func TestDecodeClientFrameKeepsArgsRaw(t *testing.T) {
	// Args stay json.RawMessage so callApp can decode each one straight into
	// its parameter's own type.
	f, err := decodeClientFrame([]byte(`{"t":"call","id":1,"cmd":"write_pty","args":{"id":7,"data":[3]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Args["id"]) != "7" {
		t.Fatalf("id arg not raw: %q", f.Args["id"])
	}
	if string(f.Args["data"]) != "[3]" {
		t.Fatalf("data arg not raw: %q", f.Args["data"])
	}
}

func TestDecodeClientFrameRejectsGarbage(t *testing.T) {
	if _, err := decodeClientFrame([]byte(`{not json`)); err == nil {
		t.Fatal("garbage must not decode")
	}
}

func TestDecodeClientFrameRejectsUnknownTag(t *testing.T) {
	if _, err := decodeClientFrame([]byte(`{"t":"telepathy"}`)); err == nil {
		t.Fatal("an unknown frame tag must be rejected, not ignored")
	}
}

func TestServerFramesOmitEmptyFields(t *testing.T) {
	// A reply must not carry an `error` key, and an event must not carry an
	// `id` — the client routes on their presence.
	b, err := json.Marshal(replyFrame(4, map[string]int{"n": 1}))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["error"]; ok {
		t.Errorf("reply carries an error key: %s", b)
	}
	if m["t"] != "reply" || m["id"] != float64(4) {
		t.Errorf("bad reply shape: %s", b)
	}

	b, _ = json.Marshal(eventFrame("phase-pty:7", nil))
	m = map[string]any{}
	_ = json.Unmarshal(b, &m)
	if _, ok := m["id"]; ok {
		t.Errorf("event carries an id: %s", b)
	}
	if m["t"] != "event" || m["name"] != "phase-pty:7" {
		t.Errorf("bad event shape: %s", b)
	}
}

func TestErrorFrameCarriesCodeAndMessage(t *testing.T) {
	f := errorFrame(9, "unknown_command", `no such command "telepathy"`)
	if f.Error == nil || f.Error.Code != "unknown_command" {
		t.Fatalf("bad error frame: %+v", f)
	}
	if f.ID != 9 {
		t.Fatalf("error frame lost its id: %+v", f)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestDecodeClientFrame|TestServerFrames|TestErrorFrame'`
Expected: FAIL — `undefined: decodeClientFrame`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/remoteproto.go
package main

import (
	"encoding/json"
	"fmt"
)

// The /v2/ws wire format. Frames are tagged by `t` so the channel can carry
// calls, replies and events without the client guessing from shape.
//
// Event NAMES are identical to the bus event names (`pty-data-7`,
// `phase-pty:7`, `chat-event-12`) on purpose: no translation table means no
// translation table to forget an entry in.

type clientFrame struct {
	T    string                     `json:"t"`
	ID   int64                      `json:"id,omitempty"`
	Cmd  string                     `json:"cmd,omitempty"`
	Args map[string]json.RawMessage `json:"args,omitempty"`
}

type frameError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type serverFrame struct {
	T     string      `json:"t"`
	ID    int64       `json:"id,omitempty"`
	Result any         `json:"result,omitempty"`
	Error *frameError `json:"error,omitempty"`

	// event
	Name    string `json:"name,omitempty"`
	Payload any    `json:"payload,omitempty"`

	// welcome
	EnvironmentID string        `json:"environmentId,omitempty"`
	Scopes        []remoteScope `json:"scopes,omitempty"`
}

func decodeClientFrame(b []byte) (clientFrame, error) {
	var f clientFrame
	if err := json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	// An unknown tag is rejected rather than ignored: silently dropping a
	// frame a future client sends is how a protocol mismatch turns into a
	// hang instead of an error.
	if f.T != "call" {
		return f, fmt.Errorf("unknown frame type %q", f.T)
	}
	return f, nil
}

func replyFrame(id int64, result any) serverFrame {
	return serverFrame{T: "reply", ID: id, Result: result}
}

func errorFrame(id int64, code, msg string) serverFrame {
	return serverFrame{T: "reply", ID: id, Error: &frameError{Code: code, Message: msg}}
}

func eventFrame(name string, payload any) serverFrame {
	return serverFrame{T: "event", Name: name, Payload: payload}
}

func welcomeFrame(envID string, scopes []remoteScope) serverFrame {
	return serverFrame{T: "welcome", EnvironmentID: envID, Scopes: scopes}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src-wails && go test ./... -run 'TestDecodeClientFrame|TestServerFrames|TestErrorFrame' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src-wails/remoteproto.go src-wails/remoteproto_test.go
git commit -m "feat(remote): tagged frame protocol for /v2/ws"
```

---

### Task 3: ticket store + `/v2/ws` handler

**Files:**
- Create: `src-wails/remotews.go`
- Test: `src-wails/remotews_test.go`

**Interfaces:**
- Consumes: `remoteAllowed`/`callApp` (Task 1), rámce (Task 2), `busSubscribe` z `src-wails/bus.go`
- Produces:
  - `type ticketStore struct{…}` s `func newTicketStore() *ticketStore`, `func (s *ticketStore) issue(scopes []remoteScope) string`, `func (s *ticketStore) redeem(t string) ([]remoteScope, bool)`
  - `type remoteWS struct{…}` s `func newRemoteWS(app *App, tickets *ticketStore) *remoteWS`, `func (h *remoteWS) register(mux *http.ServeMux)`

**Konstrukce, na které záleží:**

- **Jedna odchozí fronta na spojení.** Reader goroutine parsuje rámce a posílá je do bufferovaného kanálu; jediná writer goroutine z něj čte a zapisuje. Dvě goroutiny nikdy nezapisují do stejného WS spojení — `gorilla/websocket` to nedovoluje a stará `Broadcast` to obcházela mutexem přes celý server.
- **Pořadí.** Reply na `create_pty` a `pty-data-7` event pro totéž PTY musí dojít v tom pořadí, v jakém vznikly. Jedna fronta to garantuje; dnešní `handleWS` + `Broadcast` ne.
- **Pomalý klient nesmí zablokovat bus.** Když je fronta plná, spojení se **zavře**, nezablokuje se zápis. `busEmit` běží pod `emitMu` v `PhaseStore.Apply` (fáze 2), takže blokující sink by zastavil každou změnu fáze v appce.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/remotews_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTicketIsSingleUse(t *testing.T) {
	s := newTicketStore()
	tok := s.issue([]remoteScope{scopeOrchRead})
	if _, ok := s.redeem(tok); !ok {
		t.Fatal("fresh ticket did not redeem")
	}
	if _, ok := s.redeem(tok); ok {
		t.Fatal("ticket redeemed twice")
	}
}

func TestTicketExpires(t *testing.T) {
	s := newTicketStore()
	s.ttl = 10 * time.Millisecond
	tok := s.issue([]remoteScope{scopeOrchRead})
	time.Sleep(30 * time.Millisecond)
	if _, ok := s.redeem(tok); ok {
		t.Fatal("expired ticket redeemed")
	}
}

func TestTicketRejectsUnknown(t *testing.T) {
	s := newTicketStore()
	if _, ok := s.redeem("not-a-ticket"); ok {
		t.Fatal("unknown ticket redeemed")
	}
}

// dialTestWS starts the handler on a test server and returns a connection
// that has already consumed the welcome frame.
func dialTestWS(t *testing.T, app *App) (*websocket.Conn, *httptest.Server) {
	t.Helper()
	tickets := newTicketStore()
	h := newRemoteWS(app, tickets)
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tok := tickets.issue([]remoteScope{scopeOrchRead, scopeOrchOperate, scopeTerminal})
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	var welcome serverFrame
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatalf("welcome: %v", err)
	}
	if welcome.T != "welcome" {
		t.Fatalf("first frame was %q, want welcome", welcome.T)
	}
	return conn, srv
}

func TestWSRejectsMissingTicket(t *testing.T) {
	tickets := newTicketStore()
	h := newRemoteWS(&App{}, tickets)
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/v2/ws"
	if _, resp, err := websocket.DefaultDialer.Dial(url, nil); err == nil {
		t.Fatal("connected with no ticket")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %v (%v)", resp, err)
	}
}

func TestWSCallGetsAReply(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	conn, _ := dialTestWS(t, &App{environmentID: "env-test"})

	// environment_id is safe on a bare &App{}: it reads a field. Do not use
	// get_pty_foreground or list_pty_sessions here — both dereference
	// a.daemon with no nil guard and would panic instead of erroring.
	if err := conn.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "environment_id"}); err != nil {
		t.Fatal(err)
	}
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatal(err)
	}
	if f.T != "reply" || f.ID != 1 {
		t.Fatalf("bad reply: %+v", f)
	}
	if f.Result != "env-test" {
		t.Fatalf("reply carried %v, want the environment id", f.Result)
	}
}

func TestWSUnknownCommandIsAnErrorReplyNotADrop(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	conn, _ := dialTestWS(t, &App{})

	if err := conn.WriteJSON(clientFrame{T: "call", ID: 2, Cmd: "telepathy"}); err != nil {
		t.Fatal(err)
	}
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatal(err)
	}
	if f.Error == nil || f.ID != 2 {
		t.Fatalf("unknown command must reply with an error carrying the id: %+v", f)
	}
}

func TestWSForwardsBusEvents(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	conn, _ := dialTestWS(t, &App{})

	// Give the sink a moment to register before emitting.
	time.Sleep(20 * time.Millisecond)
	busEmit("phase-pty:7", map[string]string{"state": "running"})

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("no event frame arrived: %v", err)
	}
	if f.T != "event" || f.Name != "phase-pty:7" {
		t.Fatalf("bad event frame: %+v", f)
	}
}

func TestWSScopeIsEnforcedPerCommand(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	tickets := newTicketStore()
	h := newRemoteWS(&App{}, tickets)
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Read-only ticket: a terminal:operate command must be refused even
	// though the connection is authenticated.
	tok := tickets.issue([]remoteScope{scopeOrchRead})
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var welcome serverFrame
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatal(err)
	}

	if err := conn.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "kill_pty"}); err != nil {
		t.Fatal(err)
	}
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatal(err)
	}
	if f.Error == nil || f.Error.Code != "forbidden" {
		t.Fatalf("read-only ticket was allowed to kill a pty: %+v", f)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestTicket|TestWS'`
Expected: FAIL — `undefined: newTicketStore`

- [ ] **Step 3: Write minimal implementation**

```go
// src-wails/remotews.go
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"sync"
	"time"
)

// outboundQueue is how many frames may wait for a slow client before its
// connection is dropped. Dropping is deliberate: busEmit runs under
// PhaseStore's emitMu, so a sink that blocks would stall every phase change
// in the app, not just this client's view of it.
const outboundQueue = 256

// ticketTTL is short because a ticket is only ever carried from LocalEndpoint()
// straight into a dial. It exists so a token never has to travel in a URL —
// browsers cannot set headers on a WS handshake, so the handshake credential
// is in the query string, and a single-use 30-second value is safe there in a
// way a long-lived token is not.
const ticketTTL = 30 * time.Second

type ticket struct {
	scopes  []remoteScope
	expires time.Time
}

type ticketStore struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]ticket
}

func newTicketStore() *ticketStore {
	return &ticketStore{ttl: ticketTTL, m: make(map[string]ticket)}
}

func (s *ticketStore) issue(scopes []remoteScope) string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	tok := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Opportunistic sweep: the map only ever holds tickets issued in the last
	// 30 seconds, so it needs no background goroutine.
	now := time.Now()
	for k, v := range s.m {
		if now.After(v.expires) {
			delete(s.m, k)
		}
	}
	s.m[tok] = ticket{scopes: scopes, expires: now.Add(s.ttl)}
	return tok
}

// redeem consumes a ticket. Single use: a replayed handshake credential is
// worthless even if it leaks into a proxy log.
func (s *ticketStore) redeem(tok string) ([]remoteScope, bool) {
	if tok == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if subtle.ConstantTimeCompare([]byte(k), []byte(tok)) != 1 {
			continue
		}
		delete(s.m, k)
		if time.Now().After(v.expires) {
			return nil, false
		}
		return v.scopes, true
	}
	return nil, false
}

type remoteWS struct {
	app     *App
	tickets *ticketStore
}

func newRemoteWS(app *App, tickets *ticketStore) *remoteWS {
	return &remoteWS{app: app, tickets: tickets}
}

func (h *remoteWS) register(mux *http.ServeMux) {
	mux.HandleFunc("/v2/ws", h.handle)
}

func (h *remoteWS) handle(w http.ResponseWriter, r *http.Request) {
	scopes, ok := h.tickets.redeem(r.URL.Query().Get("ticket"))
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	granted := make(map[remoteScope]bool, len(scopes))
	for _, s := range scopes {
		granted[s] = true
	}

	// One outbound queue, one writer. Two goroutines must never write the
	// same websocket, and a single queue is also what makes ordering hold:
	// a reply and an event produced in that order arrive in that order.
	out := make(chan serverFrame, outboundQueue)
	done := make(chan struct{})
	var closeOnce sync.Once
	shutdown := func() { closeOnce.Do(func() { close(done) }) }

	unsubscribe := busSubscribe(func(name string, payload any) {
		select {
		case out <- eventFrame(name, payload):
		case <-done:
		default:
			// Queue full: drop the client rather than block the bus.
			shutdown()
		}
	})
	defer unsubscribe()

	go func() {
		for {
			select {
			case f := <-out:
				if err := conn.WriteJSON(f); err != nil {
					shutdown()
					return
				}
			case <-done:
				return
			}
		}
	}()

	envID := ""
	if h.app != nil {
		envID = h.app.environmentID
	}
	out <- welcomeFrame(envID, scopes)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			shutdown()
			return
		}
		f, err := decodeClientFrame(raw)
		if err != nil {
			// A malformed frame with no id cannot be replied to; log and
			// carry on rather than killing a working connection.
			log.Printf("remote ws: %v", err)
			continue
		}
		h.serve(f, granted, out, done)
	}
}

func (h *remoteWS) serve(f clientFrame, granted map[remoteScope]bool, out chan<- serverFrame, done <-chan struct{}) {
	send := func(fr serverFrame) {
		select {
		case out <- fr:
		case <-done:
		}
	}

	cmd, ok := remoteAllowed[f.Cmd]
	if !ok {
		send(errorFrame(f.ID, "unknown_command", "no such command "+f.Cmd))
		return
	}
	// Scope is checked per command, not once per connection: holding a ticket
	// is not authorization to call everything it could reach.
	if !granted[cmd.Scope] {
		send(errorFrame(f.ID, "forbidden", string(cmd.Scope)+" required"))
		return
	}

	result, err := callApp(h.app, cmd, f.Args)
	if err != nil {
		send(errorFrame(f.ID, "call_failed", err.Error()))
		return
	}
	send(replyFrame(f.ID, result))
}
```

- [ ] **Step 4: Make `busSubscribe` return an unsubscribe function**

`busSubscribe` (fáze 2, `src-wails/bus.go`) dnes nic nevrací a sink nelze odebrat —
každé WS spojení by trvale přidalo sink do slice, který nikdy neubývá. Změň
podpis na `func busSubscribe(s EventSink) (unsubscribe func())`:

```go
// src-wails/bus.go
type busEntry struct {
	id   uint64
	sink EventSink
}

var (
	busMu     sync.RWMutex
	busSinks  []busEntry
	busNextID uint64
)

// busSubscribe registers a sink and returns a function that removes it. The
// window sink is registered once for the app's lifetime and never removed; a
// remote connection's sink must be, or every connect leaks one.
func busSubscribe(s EventSink) func() {
	busMu.Lock()
	busNextID++
	id := busNextID
	busSinks = append(busSinks, busEntry{id: id, sink: s})
	busMu.Unlock()

	return func() {
		busMu.Lock()
		defer busMu.Unlock()
		for i, e := range busSinks {
			if e.id == id {
				busSinks = append(busSinks[:i], busSinks[i+1:]...)
				return
			}
		}
	}
}
```

`busEmit` iteruje kopii, takže se nemění. Existující volání
(`installWailsSink`, `installWSSink`) ignorují návratovou hodnotu — to je v Go
v pořádku a je to správné: ty dva sinky žijí po celý běh appky.

Přidej test, že unsubscribe funguje:

```go
// přidat do src-wails/bus_test.go
func TestBusUnsubscribeRemovesTheSink(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	var got []string
	unsub := busSubscribe(func(name string, _ any) { got = append(got, name) })
	busEmit("a", nil)
	unsub()
	busEmit("b", nil)

	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("unsubscribe did not take effect: %v", got)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd src-wails && go test ./... -run 'TestTicket|TestWS|TestBus' -v`
Expected: PASS

- [ ] **Step 6: Race check**

Run: `cd src-wails && go test -race ./... -run 'TestWS'`
Expected: PASS, no race reported. The reader goroutine, the writer goroutine
and the bus sink all touch `out` and `done`.

- [ ] **Step 7: Commit**

```bash
git add src-wails/remotews.go src-wails/remotews_test.go src-wails/bus.go src-wails/bus_test.go
git commit -m "feat(remote): /v2/ws with single-use tickets and per-command scopes

One outbound queue per connection, so a reply and an event produced in
order arrive in order — and a full queue drops the client instead of
blocking busEmit, which runs under PhaseStore's emitMu."
```

---

### Task 4: `LocalEndpoint()` a namountování

**Files:**
- Modify: `src-wails/app.go`
- Test: `src-wails/remotews_test.go` (přidat)

**Interfaces:**
- Consumes: `newTicketStore`, `newRemoteWS` (Task 3), `StartHookServer` z `src-wails/hookserver.go`
- Produces: `type LocalEndpointInfo struct { WSURL string; Ticket string; EnvironmentID string }` a `func (a *App) LocalEndpoint() LocalEndpointInfo`

`/v2/ws` jde na **mux hook serveru**, ne na nový listener: ten mux už poslouchá
na loopbacku po celý běh appky a `StartHookServer` bere registrátory rout právě
proto (control API tím jede). Server na 37892 se zapíná a vypíná přepínačem
remote accessu, takže by desktopu zmizel pod rukama.

- [ ] **Step 1: Write the failing test**

```go
// přidat do src-wails/remotews_test.go
func TestLocalEndpointIssuesAUsableTicket(t *testing.T) {
	app := &App{environmentID: "env-test"}
	app.tickets = newTicketStore()
	app.hookPort = 1234

	info := app.LocalEndpoint()
	if info.EnvironmentID != "env-test" {
		t.Errorf("environment id not carried: %+v", info)
	}
	if !strings.Contains(info.WSURL, "127.0.0.1:1234/v2/ws") {
		t.Errorf("bad ws url: %q", info.WSURL)
	}
	if info.Ticket == "" {
		t.Fatal("no ticket issued")
	}
	scopes, ok := app.tickets.redeem(info.Ticket)
	if !ok {
		t.Fatal("issued ticket does not redeem")
	}
	if len(scopes) == 0 {
		t.Fatal("desktop ticket carries no scopes")
	}
}

func TestLocalEndpointTicketsAreDistinct(t *testing.T) {
	app := &App{tickets: newTicketStore(), hookPort: 1}
	if app.LocalEndpoint().Ticket == app.LocalEndpoint().Ticket {
		t.Fatal("two calls returned the same single-use ticket")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run TestLocalEndpoint`
Expected: FAIL — `app.tickets undefined`

- [ ] **Step 3: Write minimal implementation**

Do `App` struct (`src-wails/app.go`, za `endpointProviders`):

```go
	tickets *ticketStore
	// hookPort mirrors hookSrv.port, assigned once at startup. It exists as
	// its own field so LocalEndpoint is testable without standing up a real
	// hook server; hookSrv stays the source of truth everywhere else.
	hookPort int
```

A na konec `app.go`:

```go
// LocalEndpointInfo is the desktop's bootstrap: where to connect and the
// one-shot credential to connect with.
type LocalEndpointInfo struct {
	WSURL         string `json:"ws_url"`
	Ticket        string `json:"ticket"`
	EnvironmentID string `json:"environment_id"`
}

// LocalEndpoint hands the desktop frontend a fresh single-use ticket for
// /v2/ws. This is the one thing the desktop still needs a Wails binding for,
// and the reason it needs one: being in-process IS the desktop's
// authorization, and that is not a claim anything on the network can make.
// The frontend calls this again on every reconnect, since a ticket is spent
// by the handshake that uses it.
func (a *App) LocalEndpoint() LocalEndpointInfo {
	if a.tickets == nil {
		return LocalEndpointInfo{}
	}
	// The desktop is the app; it gets every scope.
	all := []remoteScope{scopeOrchRead, scopeOrchOperate, scopeTerminal, scopeAccessRead, scopeAccessWrite}
	return LocalEndpointInfo{
		WSURL:         fmt.Sprintf("ws://127.0.0.1:%d/v2/ws", a.hookPort),
		Ticket:        a.tickets.issue(all),
		EnvironmentID: a.environmentID,
	}
}
```

- [ ] **Step 4: Wire it into startup**

V `startup`, **před** voláním `StartHookServer`:

```go
	a.tickets = newTicketStore()
```

Do `StartHookServer`'s registrátorů rout přidej `newRemoteWS(a, a.tickets).register`
vedle toho, který už mountuje control API. A hned za úspěšný start:

```go
	a.hookPort = a.hookSrv.port
```

`hookSrv.port` je dnes neexportované pole na `HookServer` — čte se ve `startup`
už teď (pro `BURROW_HOOK_PORT`), takže žádná změna viditelnosti není potřeba.

- [ ] **Step 5: Run tests + build**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 6: Regenerate bindings**

Run: `cd src-wails && wails generate module && cd .. && pnpm build`
Expected: `LocalEndpoint` je v `src-wails/frontend/wailsjs/go/main/App.d.ts`,
`LocalEndpointInfo` v `models.ts`

- [ ] **Step 7: Commit**

```bash
git add src-wails/app.go src-wails/remotews_test.go src-wails/frontend/wailsjs
git commit -m "feat(remote): LocalEndpoint binding mounts /v2/ws on the hook server"
```

---

### Task 5: `reconnectBackoff.ts`

**Files:**
- Create: `src/runtime/reconnectBackoff.ts`
- Test: `src/runtime/reconnectBackoff.test.ts`

**Interfaces:**
- Produces: `export function createBackoff(opts?: { baseMs?: number; maxMs?: number; jitter?: () => number }): { next(): number; reset(): void; attempts(): number }`

Jitter je injektovatelný, aby test byl deterministický. Bez jitteru se po
probuzení stroje ze sleepu všichni klienti připojují v tomtéž okamžiku.

- [ ] **Step 1: Write the failing test**

```ts
// src/runtime/reconnectBackoff.test.ts
import { describe, it, expect } from "vitest";
import { createBackoff } from "./reconnectBackoff";

describe("createBackoff", () => {
  it("grows and then caps", () => {
    const b = createBackoff({ baseMs: 100, maxMs: 800, jitter: () => 0 });
    expect(b.next()).toBe(100);
    expect(b.next()).toBe(200);
    expect(b.next()).toBe(400);
    expect(b.next()).toBe(800);
    expect(b.next()).toBe(800);
  });

  it("reset returns to the base delay", () => {
    const b = createBackoff({ baseMs: 100, maxMs: 800, jitter: () => 0 });
    b.next();
    b.next();
    b.reset();
    expect(b.next()).toBe(100);
    expect(b.attempts()).toBe(1);
  });

  it("applies jitter within the current step", () => {
    // jitter() = 1 must never push the delay past the cap.
    const b = createBackoff({ baseMs: 100, maxMs: 150, jitter: () => 1 });
    for (let i = 0; i < 5; i++) expect(b.next()).toBeLessThanOrEqual(150);
  });

  it("never returns a negative or zero delay", () => {
    const b = createBackoff({ baseMs: 100, maxMs: 800, jitter: () => -1 });
    for (let i = 0; i < 5; i++) expect(b.next()).toBeGreaterThan(0);
  });

  it("counts attempts", () => {
    const b = createBackoff({ jitter: () => 0 });
    expect(b.attempts()).toBe(0);
    b.next();
    b.next();
    expect(b.attempts()).toBe(2);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- reconnectBackoff`
Expected: FAIL — module not found

- [ ] **Step 3: Write minimal implementation**

```ts
// src/runtime/reconnectBackoff.ts
/**
 * Jittered exponential backoff for reconnecting a transport.
 *
 * The jitter matters more than the exponent: without it, every client that
 * lost its connection to the same event (a laptop waking from sleep, the app
 * restarting) retries in lockstep. `jitter` is injectable so tests are
 * deterministic.
 */
export interface Backoff {
  /** Delay in ms for the next attempt, and count this attempt. */
  next(): number;
  /** Call after a successful connect. */
  reset(): void;
  attempts(): number;
}

export function createBackoff(opts: {
  baseMs?: number;
  maxMs?: number;
  /** Returns -1..1. Defaults to Math.random() mapped into that range. */
  jitter?: () => number;
} = {}): Backoff {
  const baseMs = opts.baseMs ?? 250;
  const maxMs = opts.maxMs ?? 10_000;
  const jitter = opts.jitter ?? (() => Math.random() * 2 - 1);

  let attempt = 0;

  return {
    next() {
      const step = Math.min(baseMs * 2 ** attempt, maxMs);
      attempt++;
      // Jitter is +/-20% of the step, clamped so it can neither exceed the
      // cap nor collapse to zero.
      const spread = step * 0.2 * jitter();
      return Math.max(1, Math.min(maxMs, Math.round(step + spread)));
    },
    reset() {
      attempt = 0;
    },
    attempts() {
      return attempt;
    },
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- reconnectBackoff`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/runtime/reconnectBackoff.ts src/runtime/reconnectBackoff.test.ts
git commit -m "feat(runtime): jittered reconnect backoff"
```

---

### Task 6: `transport.ts` — WS klient

**Files:**
- Create: `src/runtime/transport.ts`
- Test: `src/runtime/transport.test.ts`

**Interfaces:**
- Consumes: `createBackoff` (Task 5)
- Produces:
  - `export interface EndpointSource { (): Promise<{ wsUrl: string; ticket: string }> }`
  - `export interface Transport { invoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T>; listen<T>(event: string, handler: (payload: T) => void): () => void; close(): void }`
  - `export function createTransport(getEndpoint: EndpointSource, opts?: { WebSocketImpl?: typeof WebSocket; backoff?: Backoff }): Transport`

**Proč `EndpointSource` jako callback:** ticket je jednorázový, takže reconnect
si musí říct o nový. Na desktopu ho dodá Wails binding; ve fázi 5 ho remote
klient dodá z uloženého device tokenu. Transport nezná ani jeden — dostane
funkci.

**Co musí platit:**
- `invoke` před otevřením spojení **čeká**, neselže. Frontend volá `invoke` z
  `onMounted` a spojení se v tu chvíli ještě navazuje.
- `listen` je odběr **klientský**: server fanoutuje všechno každému. Registrace
  přežije reconnect — jinak by po každém výpadku přestaly chodit `pty-data`.
- Reply routing podle `id`; `error` rámec `invoke` odmítne.
- Nedokončené `invoke` při zavření spojení **odmítnout**, ne nechat viset.

- [ ] **Step 1: Write the failing test**

```ts
// src/runtime/transport.test.ts
import { describe, it, expect, vi } from "vitest";
import { createTransport } from "./transport";
import { createBackoff } from "./reconnectBackoff";

/** Minimal scriptable WebSocket stand-in. */
class FakeWS {
  static instances: FakeWS[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  sent: string[] = [];
  readyState = 0;

  constructor(public url: string) {
    FakeWS.instances.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
    this.onclose?.();
  }

  // test helpers
  open() {
    this.readyState = 1;
    this.onopen?.();
    this.deliver({ t: "welcome", environmentId: "env", scopes: [] });
  }
  deliver(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
  lastCall() {
    return JSON.parse(this.sent[this.sent.length - 1]);
  }
}

function setup() {
  FakeWS.instances = [];
  const getEndpoint = vi.fn(async () => ({ wsUrl: "ws://x/v2/ws", ticket: "t" + FakeWS.instances.length }));
  const t = createTransport(getEndpoint, {
    WebSocketImpl: FakeWS as unknown as typeof WebSocket,
    backoff: createBackoff({ baseMs: 1, maxMs: 1, jitter: () => 0 }),
  });
  return { t, getEndpoint };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

describe("createTransport", () => {
  it("puts the ticket in the url", async () => {
    setup();
    await tick();
    expect(FakeWS.instances[0].url).toContain("ticket=t0");
  });

  it("resolves invoke with the reply result", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];
    ws.open();

    const p = t.invoke<number>("answer");
    await tick();
    const call = ws.lastCall();
    expect(call.t).toBe("call");
    expect(call.cmd).toBe("answer");
    ws.deliver({ t: "reply", id: call.id, result: 42 });
    await expect(p).resolves.toBe(42);
  });

  it("queues an invoke made before the socket opens", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];

    const p = t.invoke("early");
    await tick();
    expect(ws.sent).toHaveLength(0); // nothing sent yet

    ws.open();
    await tick();
    const call = ws.lastCall();
    expect(call.cmd).toBe("early");
    ws.deliver({ t: "reply", id: call.id, result: "ok" });
    await expect(p).resolves.toBe("ok");
  });

  it("rejects invoke on an error reply", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];
    ws.open();

    const p = t.invoke("boom");
    await tick();
    ws.deliver({ t: "reply", id: ws.lastCall().id, error: { code: "call_failed", message: "no" } });
    await expect(p).rejects.toThrow("no");
  });

  it("rejects in-flight invokes when the socket closes", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];
    ws.open();

    const p = t.invoke("orphan");
    await tick();
    ws.close();
    await expect(p).rejects.toThrow(/disconnected/i);
  });

  it("routes events to listeners and honours unlisten", async () => {
    const { t } = setup();
    await tick();
    const ws = FakeWS.instances[0];
    ws.open();

    const seen: unknown[] = [];
    const off = t.listen("phase-pty:7", (p) => seen.push(p));
    ws.deliver({ t: "event", name: "phase-pty:7", payload: { state: "running" } });
    expect(seen).toHaveLength(1);

    off();
    ws.deliver({ t: "event", name: "phase-pty:7", payload: { state: "done" } });
    expect(seen).toHaveLength(1);
  });

  it("keeps listeners registered across a reconnect", async () => {
    const { t } = setup();
    await tick();
    const first = FakeWS.instances[0];
    first.open();

    const seen: unknown[] = [];
    t.listen("pty-data-1", (p) => seen.push(p));

    first.close();
    await tick();
    await tick();
    expect(FakeWS.instances.length).toBeGreaterThan(1);

    const second = FakeWS.instances[FakeWS.instances.length - 1];
    second.open();
    second.deliver({ t: "event", name: "pty-data-1", payload: "hi" });
    expect(seen).toEqual(["hi"]);
  });

  it("asks for a fresh ticket on reconnect", async () => {
    const { t, getEndpoint } = setup();
    await tick();
    FakeWS.instances[0].open();
    FakeWS.instances[0].close();
    await tick();
    await tick();
    expect(getEndpoint.mock.calls.length).toBeGreaterThan(1);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- transport`
Expected: FAIL — module not found

- [ ] **Step 3: Write minimal implementation**

```ts
// src/runtime/transport.ts
/**
 * The client half of the /v2/ws protocol (src-wails/remoteproto.go).
 *
 * This is the ONLY place the desktop and the remote client differ: both use
 * this transport, and what varies is the EndpointSource they hand it. Remote
 * access is therefore not a feature — it is a different URL.
 *
 * Must not import from src/components, src/views, src/stores, src/mobile or
 * xterm (src/runtime/boundary.test.ts enforces it).
 */
import { createBackoff, type Backoff } from "./reconnectBackoff";

export interface EndpointSource {
  (): Promise<{ wsUrl: string; ticket: string }>;
}

export interface Transport {
  invoke<T = unknown>(cmd: string, args?: Record<string, unknown>): Promise<T>;
  /** Returns an unlisten function. Survives reconnects. */
  listen<T = unknown>(event: string, handler: (payload: T) => void): () => void;
  close(): void;
}

interface Pending {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
}

export function createTransport(
  getEndpoint: EndpointSource,
  opts: { WebSocketImpl?: typeof WebSocket; backoff?: Backoff } = {},
): Transport {
  const WS = opts.WebSocketImpl ?? WebSocket;
  const backoff = opts.backoff ?? createBackoff();

  let ws: WebSocket | null = null;
  let closed = false;
  let nextId = 1;
  const pending = new Map<number, Pending>();
  // Frames written before the socket opened. The frontend calls invoke() from
  // onMounted while the connection is still being made, and failing those
  // would make startup order matter.
  let outbox: string[] = [];
  // Listeners are client-side: the server fans every event out to every
  // connection, so this map is the routing table and it must outlive a
  // socket — otherwise a reconnect silently stops delivering pty bytes.
  const listeners = new Map<string, Set<(payload: unknown) => void>>();

  function flush() {
    if (!ws || ws.readyState !== 1) return;
    for (const frame of outbox) ws.send(frame);
    outbox = [];
  }

  function failPending(reason: string) {
    for (const p of pending.values()) p.reject(new Error(reason));
    pending.clear();
  }

  async function connect() {
    if (closed) return;
    let endpoint: { wsUrl: string; ticket: string };
    try {
      // A ticket is single-use, so every attempt needs a fresh one.
      endpoint = await getEndpoint();
    } catch {
      scheduleReconnect();
      return;
    }
    if (closed) return;

    const url = `${endpoint.wsUrl}?ticket=${encodeURIComponent(endpoint.ticket)}`;
    const socket = new WS(url);
    ws = socket;

    socket.onopen = () => {
      backoff.reset();
      flush();
    };
    socket.onmessage = (ev: MessageEvent) => handleFrame(String(ev.data));
    socket.onclose = () => {
      if (ws === socket) ws = null;
      failPending("disconnected");
      scheduleReconnect();
    };
    socket.onerror = () => {
      // onclose always follows; reconnect is scheduled there so it cannot be
      // scheduled twice for one socket.
    };
  }

  function scheduleReconnect() {
    if (closed) return;
    setTimeout(connect, backoff.next());
  }

  function handleFrame(data: string) {
    let frame: any;
    try {
      frame = JSON.parse(data);
    } catch {
      return;
    }
    if (frame?.t === "reply") {
      const p = pending.get(frame.id);
      if (!p) return;
      pending.delete(frame.id);
      if (frame.error) p.reject(new Error(frame.error.message || frame.error.code || "call failed"));
      else p.resolve(frame.result);
      return;
    }
    if (frame?.t === "event") {
      const set = listeners.get(frame.name);
      if (!set) return;
      for (const h of set) h(frame.payload);
      return;
    }
    // `welcome` needs no handling yet — phase 4 uses its seq to resume.
  }

  return {
    invoke<T>(cmd: string, args: Record<string, unknown> = {}): Promise<T> {
      const id = nextId++;
      const frame = JSON.stringify({ t: "call", id, cmd, args });
      return new Promise<T>((resolve, reject) => {
        pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
        if (ws && ws.readyState === 1) ws.send(frame);
        else outbox.push(frame);
      });
    },

    listen<T>(event: string, handler: (payload: T) => void): () => void {
      const h = handler as (payload: unknown) => void;
      let set = listeners.get(event);
      if (!set) {
        set = new Set();
        listeners.set(event, set);
      }
      set.add(h);
      return () => {
        set!.delete(h);
        if (set!.size === 0) listeners.delete(event);
      };
    },

    close() {
      closed = true;
      failPending("transport closed");
      ws?.close();
      ws = null;
    },
  };
}
```

Přidej start spojení na konec `createTransport` těsně před `return` — jinak se
nikdy nepřipojí:

```ts
  void connect();
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- transport`
Expected: PASS

- [ ] **Step 5: Boundary check + commit**

Run: `pnpm test -- boundary && pnpm test`
Expected: PASS (`src/runtime` nesmí importovat UI vrstvu)

```bash
git add src/runtime/transport.ts src/runtime/transport.test.ts
git commit -m "feat(runtime): websocket transport with reconnect and re-ticketing"
```

---

### Task 7: přepnutí desktopu — `core.ts` a `event.ts`

**Files:**
- Modify: `src/lib/wailsCompat/core.ts` (rewrite)
- Modify: `src/lib/wailsCompat/event.ts` (rewrite)

**Interfaces:**
- Consumes: `createTransport` (Task 6), `App.LocalEndpoint` (Task 4), `remoteAllowed` wire jména (Task 1)
- Produces: `invoke()` a `listen()` se stejnými podpisy jako dnes — call-sity se nemění

**Tohle je ten flip.** Dělej ho celý, nebo vůbec.

- [ ] **Step 1: Read what the switch does beyond dispatching**

Než cokoli smažeš, projdi `src/lib/wailsCompat/core.ts` a vypiš si **každý
`case`, který dělá víc než volání metody**. Jsou minimálně tři druhy a všechny
tři musí zůstat na klientovi:

1. `detach_pty` → `Promise.resolve()`. Nevolá Go vůbec. `XTerm.onBeforeUnmount`
   na něj čeká před `dispose()`, takže **nesmí** spadnout do defaultu, který
   hází — to leakovalo xterm instanci s WebGL kontextem na každém zavřeném
   terminálu.
2. `list_pty_sessions` → mapuje `string[]` z Go na záznamy
   `{pty_id, cwd, title, alive}`, které čeká legacy UI kontrakt.
3. `create_pty` a spol. → `String(args.id)`. Tuhle konverzi teď dělá Go
   (`unmarshalArg`), takže tady mizí.

Zapiš si je; Step 3 je musí obsahovat.

- [ ] **Step 2: Rewrite core.ts**

```ts
// src/lib/wailsCompat/core.ts
// Shim for "@tauri-apps/api/core"'s invoke(), backed by the /v2/ws transport.
//
// This used to be a switch over ~130 commands calling Wails-generated
// bindings. It is now a thin pass-through: the command table lives in Go
// (src-wails/remoteapi.go), which is also what decides what a remote client
// may reach. One dispatch, one place to audit.
//
// What stays here is the handful of cases that are client-side decisions
// rather than backend calls.
import { LocalEndpoint } from "../../../src-wails/frontend/wailsjs/go/main/App";
import { createTransport, type Transport } from "@/runtime/transport";

type Args = Record<string, any>;

let transport: Transport | null = null;

/** The desktop's authorization is that it is in-process: it asks the binding
 *  for a fresh single-use ticket, including on every reconnect. */
export function desktopTransport(): Transport {
  if (!transport) {
    transport = createTransport(async () => {
      const info = await LocalEndpoint();
      return { wsUrl: info.ws_url, ticket: info.ticket };
    });
  }
  return transport;
}

export async function invoke<T = unknown>(cmd: string, args: Args = {}): Promise<T> {
  switch (cmd) {
    // Nothing to detach. The Go daemon broadcasts frames to every attached
    // client and a closed XTerm simply stops listening, while the PTY keeps
    // running for the next reattach. Deliberately NOT left to fall through:
    // XTerm.onBeforeUnmount awaits this before disposing, so a throw here
    // skipped renderAddon.dispose() + term.dispose() and leaked an xterm
    // instance (with its WebGL context) on every closed terminal.
    case "detach_pty":
      return undefined as T;

    // The daemon binding exposes live ids (string[]), while the legacy UI
    // contract expects session records. Normalize here so restored terminal
    // threads reattach to their existing PTY instead of allocating a new one
    // and consequently missing its status hooks.
    case "list_pty_sessions": {
      const ids = await desktopTransport().invoke<string[]>("list_pty_sessions");
      return ids
        .map((id) => Number(id))
        .filter((pty_id) => Number.isFinite(pty_id))
        .map((pty_id) => ({ pty_id, cwd: "", title: "", alive: true })) as T;
    }
  }

  return desktopTransport().invoke<T>(cmd, args);
}
```

- [ ] **Step 3: Rewrite event.ts**

```ts
// src/lib/wailsCompat/event.ts
// Shim for "@tauri-apps/api/event"'s listen(), backed by the /v2/ws
// transport. Event names on the wire are identical to the bus names in Go, so
// there is no translation table here to forget an entry in.
import { desktopTransport } from "./core";

export type UnlistenFn = () => void;

export async function listen<T = unknown>(
  event: string,
  handler: (event: { event: string; payload: T }) => void,
): Promise<UnlistenFn> {
  // Tauri's payload shape is {event, payload}; the wire hands us the raw
  // payload, so wrap it back into the shape call-sites already expect.
  return desktopTransport().listen<T>(event, (payload) => handler({ event, payload }));
}

export async function emit(event: string, _payload?: unknown): Promise<void> {
  // There is no client->client emit: app-originated events only. Kept as a
  // no-op so call-sites that emit UI-local signals don't throw.
  console.warn(`[wails-compat] emit("${event}") is a no-op — not ported`);
}
```

- [ ] **Step 4: Find the commands the table is missing**

Spusť `pnpm build` a pak appku (`just dev`). Každé `invoke`, které tabulka
nezná, se ozve jako odmítnutý promise s `unknown_command`. Otevři devtools
konzoli, projdi appku (workspace, terminál, chat, git panel, settings) a
posbírej jména, která spadnou. Každé přidej do `remoteAllowed` v
`src-wails/remoteapi.go`.

`TestRemoteSurfaceIsExhaustive` hlídá, že žádná `App` metoda nezůstane
nezařazená — **nehlídá** ale, že každé wire jméno, které frontend volá, v
tabulce je. Ten směr musí projít rukou; proto ten průchod appkou.

- [ ] **Step 5: Typecheck + tests**

Run: `pnpm build && pnpm test`
Expected: PASS. Pokud `vue-tsc` hlásí nepoužité importy generovaných bindings,
odstraň je — `core.ts` už z `App` importuje jen `LocalEndpoint`.

- [ ] **Step 6: Manual verification — celá appka jede přes WS**

Run: `just dev`

1. Appka nabootuje, sidebar má workspaces (byly načtené `invoke`em přes WS).
2. Otevři terminál, napiš `ls` → výstup teče (`pty-data-{id}` došel jako event frame).
3. Spusť agenta, pošli prompt → tečka jde oranžová a po doběhnutí se usadí.
4. Otevři chat, pošli zprávu → odpověď streamuje.
5. Git panel ukáže změny.
6. V devtools konzoli **žádné** `unknown_command`.
7. Zabij WS spojení (devtools → Network → WS → zavři) → appka se do pár sekund
   sama připojí a terminál dál teče.

- [ ] **Step 7: Commit**

```bash
git add src/lib/wailsCompat/core.ts src/lib/wailsCompat/event.ts src-wails/remoteapi.go
git commit -m "refactor(transport): desktop talks to the app over /v2/ws

The 130-case switch that mapped wire names onto Wails bindings is gone;
the mapping lives in Go, where it also decides what a remote client can
reach. Remote access stops being a second API surface that can drift."
```

---

### Task 8: dokumentace

**Files:**
- Modify: `CLAUDE.md`, `docs/context.html`

- [ ] **Step 1: Update CLAUDE.md**

Přidej sekci o transportu (za „Control API + `burrow` CLI"), a v sekci **Backend**
doplň `remoteapi.go`, `remoteproto.go`, `remotews.go`.

Co musí být pravda a jasné:
- desktop mluví s appkou přes `ws://127.0.0.1:<hookPort>/v2/ws`, ne přes Wails bindings
- `wailsCompat/core.ts` je pass-through, ne switch; jediná Wails binding, kterou
  na datové cestě potřebuje, je `LocalEndpoint()`
- `remoteAllowed` je bezpečnostní hranice a `TestRemoteSurfaceIsExhaustive` ji hlídá
- jména parametrů nese tabulka, protože v runtime neexistují (reflect je nemá,
  Wails generuje `arg1..argN`)
- ticket je jednorázový a 30s; loopback origin sám autorizace není
- jedna odchozí fronta na spojení → pořadí reply vs event; plná fronta klienta
  odpojí, aby nezablokovala `busEmit` pod `emitMu`
- staré `/ws` + `dispatch` **žije** pro `src/mobile` do fáze 6
- **snapshot a resume tady nejsou** — fáze 4

- [ ] **Step 2: Update docs/context.html**

V tabulce Go bindings: `LocalEndpoint()` přibylo. Doplň poznámku, že ostatní
commandy už nejsou Wails bindings volané z frontendu, ale wire jména v
`remoteAllowed`.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md docs/context.html
git commit -m "docs: the desktop's data path is /v2/ws"
```

---

## Self-review

**Spec coverage (fáze 3):**

| spec | task |
|---|---|
| §2 `Transport` interface, `invoke`/`listen` | 6 |
| §2 rámce `call`/`reply`/`event`/`welcome` | 2 |
| §2 jména eventů identická s bus jmény | 2, 7 |
| §2 `remoteAllowed` jako explicitní hranice + reflexe jen na marshalling | 1 |
| §2 test „každá `App` metoda v allow nebo deny" | 1 |
| §2 ordering: jedna odchozí fronta | 3 |
| §1 `core.ts` jako re-export transportu, nula změn v call-sitech | 7 |
| §1 `App.LocalEndpoint()` vrací `{wsURL, ticket}` | 4 |
| §4/§6.2 jednorázový ticket, token nikdy jako dlouhoživoucí query param | 3 |
| §4 scopes vynucené per RPC metoda | 1, 3 |
| §5 `reconnectBackoff.ts` | 5 |
| §5 `src/runtime/` import boundary | 6 (test z fáze 2) |

Vědomě mimo tenhle plán: §2 snapshot/`shell_snapshot`/resume/resync a binární
PTY framy (fáze 4); §4 pairing, device tokeny, endpoint výběr na klientovi,
funnel check, bind invarianty (fáze 5); §5 `src/mobile` na sdílených stores
(fáze 6); §6 push (fáze 7). §1 boot readiness gate není potřeba, protože
`/v2/ws` sedí na hook serveru, který startuje před frontendem — `invoke` navíc
frontuje, takže volání během navazování spojení nespadne.

**Type consistency:** `remoteCmd{Method, Args, Scope}` používají Tasky 1 a 3
shodně. `serverFrame`/`clientFrame` tagy (`t`, `id`, `cmd`, `args`, `result`,
`error`, `name`, `payload`, `environmentId`, `scopes`) se v Tasku 2 (Go),
Tasku 3 (testy) a Tasku 6 (TS klient) shodují. `LocalEndpointInfo` má JSON tagy
`ws_url`/`ticket`/`environment_id` a Task 7 je čte v tom tvaru.
`busSubscribe` vrací `func()` v Tasku 3 a všechna existující volání to ignorují.

**Placeholders:** žádné. Dva kroky vyžadují ruční průchod a je u nich napsané
co a proč: Task 1 Step 5 (zařadit metody, které test vypíše) a Task 7 Step 4
(najít wire jména, která tabulka nezná — ten směr žádný test nehlídá).

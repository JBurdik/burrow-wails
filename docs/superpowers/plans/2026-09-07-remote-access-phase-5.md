# Remote Access — Implementation Plan, fáze 5 (pairing, device tokens, scopes na síti)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Telefon se spáruje jednou, dostane **vlastní** odvolatelný token a přes něj se dostane na `/v2/ws` z tailnetu. Sdílený `http.token` v query parametru přestane být cesta dovnitř. Startup odmítne nastartovat, když by handler skončil na veřejném internetu.

**Architecture:** `POST /v2/pair` (6 číslic, budget, rotace při úspěchu) → per-device token uložený jako SHA-256 v SQLite → `POST /v2/ws-ticket` (Bearer) → jednorázový 30s ticket → `wss://…/v2/ws?ticket=…`. Ticket nese scopes **a device id**, takže revoke shodí i živá spojení. `/v2/ws` se registruje na oba muxy (hook server = loopback desktop, 37892 = to, co `tailscale serve` proxuje).

**Tech Stack:** Go 1.25, SQLite; Vue 3 + vitest.

**Spec:** `docs/superpowers/specs/2026-09-05-remote-access-t3code-design.md` §4, invarianty 1–3, testy 5–8

**Předchozí fáze:** `docs/superpowers/plans/2026-09-06-remote-access-phase-4.md` (hotová, `5e45245` → `cd180c8`)

## Global Constraints

- **Komentáře v kódu anglicky.** Commit subject anglicky, Conventional Commits.
- **`http.token` a staré `/ws` v téhle fázi NEUMÍRAJÍ.** Spec §4 chce hard cutover; ten patří do fáze 6, kdy nový klient existuje. Zabít token teď = telefon nefunkční mezi fází 5 a 6, a fáze mají být shipnutelné jednotlivě. Fáze 6 to smaže spolu s `store.ts`/`api.ts`.
- **Scopes nejsou sandbox a tahle fáze to nezmění.** Viz LOAD-BEARING NOTE v `remoteapi.go`: `terminal:operate` je interaktivní shell, `orchestration:operate` je exec jinou cestou, `orchestration:read` je neomezené čtení hostu, a **eventy scopes ignorují úplně**. Spárované zařízení má autoritu nad strojem. Task 5 to napíše nahlas do kódu i do CLAUDE.md místo toho, aby se to tvářilo jinak. Kdo v téhle fázi přidá path guard do `fs.go`, zavře jednu ze čtyř děr a rozbije desktopu file tree a editor — je to samostatná práce s vlastním rozhodnutím.
- **Token nikdy v query parametru.** Ticket ano (jednorázový, 30 s). Bearer header pro všechno ostatní. Test to hlídá.
- **Fail closed.** Funnel zapnutý → server nenastartuje. Non-loopback adresa → server nenastartuje. Ne warning do logu.
- Go testy: `cd src-wails && go test ./...` a `go test -race ./...`. Frontend: `pnpm test`. Vše: `just check`.

---

## File Structure

| soubor | odpovědnost |
|---|---|
| `src-wails/remoteguard.go` (nový) | `funnelEnabledIn()`, `assertLoopbackAddr()` — čisté funkce, žádný exec |
| `src-wails/remoteguard_test.go` (nový) | funnel on/off z reálného JSONu, tailnet IP odmítnuta |
| `src-wails/remotedevices.go` (nový) | `remote_devices` tabulka, `pairDevice`, `deviceForToken`, `RemoteDevices`, `RevokeRemoteDevice` |
| `src-wails/remotedevices_test.go` (nový) | pair→lookup, revoke→401, list nikdy nenese token |
| `src-wails/remoteauth.go` (nový) | `POST /v2/pair`, `POST /v2/ws-ticket` |
| `src-wails/remoteauth_test.go` (nový) | spec testy 7 a 8 |
| `src-wails/remotews.go` (modify) | ticket nese `deviceID`; spojení se registruje pod ním; revoke ho shodí |
| `src-wails/httpserver.go` (modify) | mount `/v2/pair`, `/v2/ws-ticket`, `/v2/ws` na tailnet mux |
| `src-wails/app.go` (modify) | `setHttpEnabled` prochází guardy |
| `src-wails/db.go` (modify) | migrace tabulky |
| `src-wails/remoteapi.go` (modify) | `remote_devices` / `revoke_remote_device`; přepsaná LOAD-BEARING NOTE |
| `src/components/Settings.vue` (modify) | seznam zařízení + revoke |
| `CLAUDE.md` (modify) | co pairing je, co scopes nejsou |

---

### Task 1: fail closed — funnel a bind adresa

**Files:**
- Create: `src-wails/remoteguard.go`, `src-wails/remoteguard_test.go`
- Modify: `src-wails/app.go`, `src-wails/tailscale.go`

**Interfaces:**
- Produces: `func funnelEnabledIn(serveStatusJSON []byte) bool`, `func assertLoopbackAddr(addr string) error`

**Proč.** `tailscale funnel` vystaví **týž** handler na veřejný internet — stejný host, stejný port 443, stejná `/burrow` cesta. Pairing s šesti číslicemi je proti tailnetu rozumný a proti internetu ne. A přímý listen na tailnet IP je zakázaný nejen kvůli auth: plain HTTP na private IP **není secure context**, takže by nešel service worker, a tedy ani instalovatelná PWA.

Reálný tvar `tailscale serve status --json` (změřeno na tomhle stroji, funnel vypnutý — klíč `AllowFunnel` **chybí**, není `false`):

```json
{"TCP":{"443":{"HTTPS":true}},
 "Web":{"mac-mini.tailnet.ts.net:443":{"Handlers":{"/burrow":{"Proxy":"http://127.0.0.1:37892"}}}}}
```

Se zapnutým funnelem přibude `"AllowFunnel":{"mac-mini.tailnet.ts.net:443":true}`.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/remoteguard_test.go
func TestFunnelOffInRealServeStatus(t *testing.T) {
	// Measured shape with funnel off: the AllowFunnel key is ABSENT, not
	// false. A checker that looked for `== false` would read "off" as "on"
	// and refuse to start for everyone.
	const off = `{"TCP":{"443":{"HTTPS":true}},"Web":{"h:443":{"Handlers":{"/burrow":{"Proxy":"http://127.0.0.1:37892"}}}}}`
	if funnelEnabledIn([]byte(off)) {
		t.Fatal("funnel reported on when the key is absent")
	}
}

func TestFunnelOnRefusesStart(t *testing.T) {
	const on = `{"AllowFunnel":{"h:443":true},"Web":{"h:443":{"Handlers":{"/burrow":{"Proxy":"http://127.0.0.1:37892"}}}}}`
	if !funnelEnabledIn([]byte(on)) {
		t.Fatal("funnel on was not detected")
	}
}

func TestUnparseableServeStatusIsTreatedAsFunnelOn(t *testing.T) {
	// Fail closed: if we cannot read the config we cannot claim the handler
	// is private.
	if !funnelEnabledIn([]byte("not json")) {
		t.Fatal("unreadable serve config must fail closed")
	}
}

func TestOnlyLoopbackMayBeBound(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:37892", "localhost:37892"} {
		if err := assertLoopbackAddr(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	// A tailnet IP, a LAN IP and a wildcard are all the same mistake: the
	// only way in from the tailnet is `tailscale serve`, which terminates
	// HTTPS and forwards to loopback.
	for _, bad := range []string{"100.64.0.1:37892", "0.0.0.0:37892", ":37892", "192.168.1.5:37892"} {
		if err := assertLoopbackAddr(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestFunnel|TestOnlyLoopback'`
Expected: FAIL — undefined functions

- [ ] **Step 3: Implement `remoteguard.go`**

`funnelEnabledIn` unmarshals into `struct{ AllowFunnel map[string]bool }` and
returns true on **any** true entry **or** an unmarshal error. Do not try to match
the hostport against our own node: funnel is granted per host:port and our
serve sits on that same 443, so any funnel there publishes our path.

`assertLoopbackAddr` splits host/port and accepts only `127.0.0.1`,
`::1` and `localhost`. An empty host (`":37892"`) is a wildcard bind, which
is a reject, not a default.

Add `App.funnelEnabled() bool` in `tailscale.go` — shells out to
`tailscale serve status --json` and delegates to the pure function. **Not
installed / no serve config = funnel off**, because there is nothing published
at all in that case (distinct from "cannot read a config that exists").

- [ ] **Step 4: Gate the listener**

In `app.go`'s `setHttpEnabled(true)`: build the addr, `assertLoopbackAddr` it,
then refuse when `a.funnelEnabled()`. Both return an error from
`SetHttpEnabled` so Settings shows it — the pref file must NOT be written when
the start was refused, or the next launch retries a config the user cannot see
failing.

- [ ] **Step 5: Run tests**

Run: `cd src-wails && go test ./... && go vet ./...`

- [ ] **Step 6: Commit**

```bash
git add src-wails/remoteguard.go src-wails/remoteguard_test.go src-wails/app.go src-wails/tailscale.go
git commit -m "feat(remote): refuse to start behind funnel or off loopback"
```

---

### Task 2: per-device tokens

**Files:**
- Create: `src-wails/remotedevices.go`, `src-wails/remotedevices_test.go`
- Modify: `src-wails/db.go`

**Interfaces:**
- Produces:
  ```go
  type RemoteDevice struct {
      ID       string `json:"id"`
      Name     string `json:"name"`
      Kind     string `json:"kind"`
      Scopes   []remoteScope `json:"scopes"`
      AddedAt  int64  `json:"added_at"`
      LastSeen int64  `json:"last_seen"`
  }
  func (a *App) pairDevice(name, kind string) (token string, dev RemoteDevice, err error)
  func (a *App) deviceForToken(token string) (RemoteDevice, bool)
  func (a *App) touchDevice(id string)
  func (a *App) RemoteDevices() ([]RemoteDevice, error)
  func (a *App) RevokeRemoteDevice(id string) error
  ```

**Co se ukládá.** Token je 32 náhodných bajtů hex; v DB leží **jen jeho
SHA-256**. Není to obrana proti někomu, kdo už čte disk (tam leží i
`control.token` v plaintextu), ale je to obrana proti tomu, aby se dal
použitelný token vynést z backupu DB nebo z chybového výpisu, a stojí to čtyři
řádky. `RemoteDevice` **nemá** pole pro token ani pro hash, aby ho `RemoteDevices()`
nemohla vrátit ani omylem.

Scopes se ukládají per-device (comma-joined), ne konstanta v kódu: spárovaný
telefon dnes dostane `orchestration:read`, `orchestration:operate`,
`terminal:operate`, a když je později bude chtít UI zúžit, je to řádek v DB, ne
migrace.

**Nikdy `access:write`, nikdy `ui:ack`.** `access:write` je bootstrap párování
(zařízení nesmí párovat další zařízení) a `ui:ack` je identita desktopového UI
(viz `remoteapi.go`). Že si spárovaný telefon může přes `create_pty` spustit
shell a obejít obojí lokálně, je pravda a je to v Tasku 5 napsané nahlas — což
není důvod ta jména vydávat, je to důvod netvrdit, že něco brání.

- [ ] **Step 1: Write the failing test**

```go
// src-wails/remotedevices_test.go
func TestPairedDeviceIsFoundByItsToken(t *testing.T) { /* pairDevice → deviceForToken ok */ }

func TestRevokedDeviceIsNotFound(t *testing.T) {
	// Revocation has to be the end of it: this is the only lever the user
	// has after a phone is lost.
}

func TestADeviceListNeverCarriesATokenOrItsHash(t *testing.T) {
	// Settings renders this list; a token in it is a token in a screenshot.
	// Asserted on the marshalled JSON, not on the struct, because a field
	// added later without a json:"-" tag is exactly the mistake.
}

func TestTwoDevicesGetDifferentTokens(t *testing.T) {}

func TestTokenIsNotStoredInPlaintext(t *testing.T) {
	// Read the raw column and assert the token is not in it.
}
```

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Implement**

Table (migrate in `db.go` alongside the others):

```sql
CREATE TABLE IF NOT EXISTS remote_devices (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  scopes     TEXT NOT NULL,
  added_at   INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL
);
```

`deviceForToken` hashes the presented token and looks the hash up — a lookup by
key, so no constant-time compare is needed or possible; the hash is what
prevents a timing oracle over the stored value.

- [ ] **Step 4: Run tests + race**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(remote): per-device tokens, hashed at rest and revocable"
```

---

### Task 3: `/v2/pair` a `/v2/ws-ticket`

**Files:**
- Create: `src-wails/remoteauth.go`, `src-wails/remoteauth_test.go`
- Modify: `src-wails/httpserver.go`, `src-wails/remotews.go`

**Interfaces:**
- Consumes: Task 2's device store, `ticketStore` (`remotews.go`)
- Produces: `type remoteAuth struct{...}` with `register(mux)`; `ticket` gains `deviceID string`

**Tvar.**

```
POST /v2/pair       {code, name, kind}      → {device_token, environment_id, scopes}
POST /v2/ws-ticket  Authorization: Bearer   → {ticket}
GET  /v2/ws?ticket=…
```

`/v2/pair` je nutně neautentizované — to je smysl párování. Rozpočet na
pokusy (`pairMaxFailures`, už existuje) a rotace kódu při úspěchu zůstávají;
kód dostane **TTL 3 minuty** (spec §4), což dnes nemá, takže kód zobrazený
ráno v Settings není v platnosti večer.

`/v2/ws-ticket` bere Bearer device token, aktualizuje `last_seen` a vydá ticket
se **scopes toho zařízení** a s jeho `deviceID`. Desktop dál používá
`LocalEndpoint()`, který razí ticket s `deviceID: ""` a se všemi scopes — být
in-process JE jeho autorizace a nic na síti to o sobě tvrdit nemůže.

- [ ] **Step 1: Write the failing test**

Spec §7 items 7 and 8, plus the query-param invariant:

```go
func TestExpiredPairCodeIsRejected(t *testing.T)
func TestPairCodeIsSingleUse(t *testing.T)            // a success rotates it
func TestPairLocksOutAfterTheBudget(t *testing.T)
func TestWsTicketRequiresABearerDeviceToken(t *testing.T)
func TestRevokedDeviceCannotGetATicket(t *testing.T)
func TestADeviceTokenInTheQueryStringIsNotAccepted(t *testing.T)
// ^ the invariant that survives a refactor: /v2/ws takes a `ticket`, and a
//   device token presented the way the OLD /ws took one must fail.
func TestPairedDeviceDoesNotGetAccessWriteOrUiAck(t *testing.T)  // spec test 8
```

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Implement `remoteauth.go`**

- [ ] **Step 4: Mount on the tailnet server**

In `httpserver.go`'s `ListenAndServe`: register `/v2/pair`, `/v2/ws-ticket`
and — the point of the whole phase — `/v2/ws` itself, using the **app's own
`remoteWS` and its shared ticket store**, not a new one. A second ticket store
would mean a ticket minted by `/v2/ws-ticket` is unknown to the handler that
redeems it.

Old `/ws`, `/rpc/`, `/pair` stay mounted (Global Constraints).

- [ ] **Step 5: Run tests + race**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(remote): pair for a device token, trade it for a ws ticket"
```

---

### Task 4: revoke shodí živá spojení

**Files:**
- Modify: `src-wails/remotews.go`, `src-wails/remotedevices.go`
- Test: `src-wails/remotews_test.go`

**Proč.** Revoke, který nechá běžet spojení otevřené od včera, není revoke.
Ztracený telefon má živý socket a ten socket je celá appka.

- [ ] **Step 1: Write the failing test**

```go
func TestRevokingADeviceDropsItsLiveConnection(t *testing.T)
func TestRevokingADeviceLeavesOtherConnectionsAlone(t *testing.T)
// ^ including the desktop's own (deviceID "").
```

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Implement**

A small registry on `remoteWS`: `map[string]map[uint64]func()` of shutdown
funcs by device id, registered after the upgrade and removed by the same defer
that unsubscribes from the bus. `RevokeRemoteDevice` deletes the row and then
calls them. Keyed by a per-connection id so two connections from one device
unregister independently.

Connections with `deviceID == ""` (the desktop) are never in the registry —
there is no row to revoke, and being reachable there would let a revoke drop
the window.

- [ ] **Step 4: Run tests + race**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(remote): a revoke closes the device's open sockets"
```

---

### Task 5: Settings + řekni pravdu o scopes

**Files:**
- Modify: `src-wails/remoteapi.go`, `src/components/Settings.vue`

- [ ] **Step 1: Expose the two verbs**

`remote_devices` → `RemoteDevices` (`scopeAccessRead`), `revoke_remote_device`
→ `RevokeRemoteDevice` (`scopeAccessWrite`). Both therefore desktop-only, since
a paired device never gets `access:write` — and `remote_devices` at
`access:read` rather than `orchestration:read` keeps the device inventory off
the read scope every paired phone holds.

- [ ] **Step 2: Rewrite the LOAD-BEARING NOTE**

It currently says the honest model is a prerequisite "before any phase exposes
a scoped-but-not-fully-trusted session". This phase resolves that question by
answering it rather than by building the containment: **a paired device is the
owner's own device and holds authority over the machine, by design** — t3code
does the same, a phone that can drive a terminal can drive anything. Record:

- what a `Scope` therefore IS: a record of which door a call came through, and
  the forcing function that makes a NEW verb decide whether it joins the
  network surface at all;
- what it is NOT: containment. Keep all four bullets (terminal:operate,
  orchestration:operate, orchestration:read, events ignore scopes) — they are
  still true and they are the reason the sentence above has to be said out loud;
- that the real boundary is **paired or not paired**, which is why the pairing
  budget, the code TTL, the funnel refusal and revoke-drops-sockets are where
  the security work of this phase went;
- that `fs.go`'s path guard, per-connection event filtering and an exec
  admission check are what a genuinely limited device role would need, and that
  none of them is on the path to shipping this one.

- [ ] **Step 3: Settings section**

Under the existing remote-access block: the pair code with its remaining TTL, a
regenerate button, and the device list — name, kind, `lastSeen` as relative
time, revoke per row. An empty list says "no devices paired" rather than
rendering nothing, because "nothing paired" and "the list failed to load" must
not look the same.

- [ ] **Step 4: Run `just check`**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(settings): paired devices, and an honest note about scopes"
```

---

### Task 6: dokumentace

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1** — the pairing chain end to end; that a device token never
travels in a URL and a ticket may because it is single-use and 30 s; that the
funnel check and the loopback assert are fail-closed and why (secure context,
not only auth); that revoke closes sockets; that scopes are not containment and
what the real boundary is; that `http.token` and the old `/ws` are still alive
and die in phase 6.

- [ ] **Step 2: Commit**

---

## Self-review

**Spec coverage (§4, §7):**

| spec | task |
|---|---|
| pairing grant 6 číslic, TTL 3 min, jednorázový | 3 |
| `POST /pair` → sessionToken/environmentId/scopes | 3 |
| `POST /ws-ticket` → ticket TTL 30 s jednorázový | 3 |
| telefon dostane první tři scopes, nikdy `access:write` | 2, 3 |
| enforcement per RPC, ne jen při vydání ticketu | už hotové (fáze 3), test v 3 |
| Settings `{deviceName, deviceType, lastSeen, addedAt}` + revoke | 2, 5 |
| invariant 1 — bind jen `127.0.0.1` | 1 |
| invariant 2 — funnel = fail closed | 1 |
| invariant 3 — token nikdy v query | 3 |
| test 5 — listen na non-loopback odmítne start | 1 |
| test 6 — funnel odmítne start | 1 |
| test 7 — expirovaný / dvakrát použitý grant, revokované zařízení → 401 | 3 |
| test 8 — session bez `access:write` na pairing verb → 403 | 3 |

**Vědomě mimo:** hard cutover `http.token` (Global Constraints — patří do fáze
6, kdy existuje nový klient). Path guard v `fs.go`, per-connection filtrování
eventů a admission check na exec — Task 5 je pojmenuje jako to, co by chtěla
skutečně omezená role zařízení, a fáze 5 je nedělá, protože spárované zařízení
omezená role není. QR kód v Settings — párovací kód se zobrazuje textem; QR je
kosmetika nad tímtéž šestičíslím.

**Type consistency:** `RemoteDevice` JSON (`id`/`name`/`kind`/`scopes`/
`added_at`/`last_seen`) ↔ Settings.vue. `ticket` gains `deviceID` in Task 3 and
Task 4 reads it; `LocalEndpoint` keeps minting `deviceID: ""`.

**Placeholders:** žádné. Task 1 nese změřený tvar `tailscale serve status
--json` z tohohle stroje, včetně toho, že `AllowFunnel` při vypnutém funnelu
**chybí** místo aby bylo `false` — což je právě ta chyba, kterou by naivní
checker udělal.

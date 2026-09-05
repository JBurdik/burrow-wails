# Remote access — design (t3code model, PWA-only)

> **Supersedes** `docs/plans/005-remote-access.md` a `docs/plans/005-remote-access-impl-1.md`.
> Z těch dvou nebyl implementovaný žádný kód (`internal/agentphase` neexistuje), takže
> tohle není změna směru za pochodu — je to jejich náhrada před prvním commitem.

Referenční implementace: `pingdotgg/t3code`
(`~/.opensrc/repos/github.com/pingdotgg/t3code/main`), zejména
`docs/architecture/remote.md`, `docs/architecture/overview.md`,
`docs/cloud/environment-auth.md`, `packages/client-runtime/`,
`apps/server/src/auth/`.

## Cíl

Remote access **není feature**. Je to „ukaž stejnému klientovi jinou URL".

Jediný rozdíl proti t3code: **nemáme native app**. Roli mobilního klienta hraje
PWA, servírovaná tímtéž Go serverem.

## Problém, který to řeší

Burrow má dnes **tři paralelní API surfaces a dva klienty**:

| surface | tvar | konzument |
|---|---|---|
| `wailsCompat/core.ts` `invoke()` | switch přes ~130 snake_case commandů | plný `src/` desktop |
| `httpserver.go` `dispatch()` | 11 hand-typed commandů, jeden `wsArgs` god-struct | `src/mobile/` (vlastní store, 616 ř.) |
| `internal/control` verby | registry, 25 verbů, `ScopeRemote` | CLI / MCP / mobil |

Měřitelné důsledky v repu dnes:

- `src/mobile/store.ts` má vlastní `statusFor` (:118), `chatStatus` (:122),
  `watchTabStatus` (:129 — vlastní kopie 4000 ms auto-clearu ze `settleDone()`)
  a vlastní `applyEvent` (:310) duplikující `src/lib/chatProjection.ts`.
- `events.go:27` `emitWorkspacesChanged` používá `runtime.EventsEmit` místo
  `emitAll` — `workspaces-changed` se na telefon **nikdy nedostane**.
- `httpserver.go:221` přiznává v komentáři, že token jede jako query param.
  Jeden dlouhoživoucí `http.token`: leak = plný přístup ke stroji, a nelze
  odvolat jedno zařízení.
- Derivovaný status (`review` vs `done`) žije ve `Terminal.vue`. Existuje jen
  pro mountnuté workspaces a **restart appky ho zahodí**.

## Rozhodnutí (a co z nich plyne)

| rozhodnutí | důsledek |
|---|---|
| **Full t3code transport**: i desktop mluví WS s lokálním serverem | jeden dispatch, jeden event fanout, jedna auth cesta — remote nemůže driftovat od desktopu, protože je to týž kód |
| Server **bez Wails závislosti**, ale zatím uvnitř appky | headless `burrow serve` je později přesun souborů, ne redesign |
| Wire = **dnešní `invoke` kontrakt** (snake_case cmd + named args) | 130 call-sitů v `src/` se nedotkne ani jeden |
| Auth: **scopes + pairing + WS ticket**, bez OAuth ceremonie | spárovaný telefon nemůže párovat další zařízení; žádný refresh/DPoP kód |
| Tailscale = **endpoint provider, ne identita** | jedna auth větev místo dvou |
| PWA v1: dashboard · chaty + permission · spawn · git diff | Web Push je fáze 7, odříznutelná |

**Nekupuju z t3code**: RFC 8693 token-exchange tvar, DPoP, expirující access
tokeny s refreshem, relay/cloud vrstvu, snapshot/patch pro *všechen* stav.
Jeden uživatel — druhá vrstva ceremonie bez druhého uživatele je jen kód, který
se dá špatně nakonfigurovat.

**Zůstává mimo** (t3code non-goals, přebírám): provider auth se nesynchronizuje
(Claude/Codex login je per-machine) a `workspace` zůstává environment-local —
lokální a remote klon téhož repa jsou dva workspacy.

---

## 1. Procesní hranice a boot

Server poslouchá na `127.0.0.1:<ephemeral>`, port do `<app-data>/server.port`.
Vlastní všechno, co dnes vlastní `App`: DB, PTY, chaty, git, FS, LSP, extensions,
fáze. Startuje z `main.go` **před** `wails.Run`.

### Co zůstává na Wails bindings

| shim | proč nejde přes WS |
|---|---|
| `wailsCompat/window.ts` | pozice/velikost okna, fullscreen |
| `wailsCompat/dialog.ts` | native file picker |
| `wailsCompat/notification.ts` | native OS notifikace |
| `wailsCompat/updater.ts` + `process.ts` | swap `.app` bundlu, relaunch |
| menu callbacky v `main.go` | `runtime.EventsEmit` pro `menu-*` |
| **`App.LocalEndpoint()`** (nová) | vrátí `{wsURL, ticket}` |

`LocalEndpoint()` je bootstrap. Desktop je autentizovaný tím, že je **v procesu** —
nepároval se, jen si přes binding řekne o jednorázový ticket se všemi scopes.
Na síti ta cesta neexistuje.

### Boot sequence

```
main() → server.Start()          // listener + DB migrace + daemon + hook server
       → readiness gate
       → wails.Run()
       → frontend → App.LocalEndpoint() → ws://127.0.0.1:PORT
       → server: {t:"welcome", environmentId, seq, scopes}
       → klient: shell_snapshot → první paint
```

`wails dev` funguje beze změny — Vite na :34115 si port zjistí týmž bindingem.

### Hranice bez přesunu balíčku

130 metod se **nestěhuje** do `internal/server`. Je to velký mechanický diff,
který sám o sobě nic neumí. Hranice se vynutí **grep testem**:
`wails/v2/pkg/runtime` smí importovat jen `main.go`, `window.go`, `dialog.go`,
`updater.go`.

```go
// ponytail: boundary enforced by import grep, not by package split.
// Split into internal/server when headless `burrow serve` actually ships.
```

### Event bus

`emitAll` umírá jako koncept. Vzniká `bus.Emit(name, payload)` — jediné dveře,
žádná druhá větev, kterou lze zapomenout. Dnešní bug `emitWorkspacesChanged`
tím přestane být **možný**, ne jen opravený.

Local-only eventy (menu, float-window, `update:progress`, `lsp-msg`) zůstávají
na `runtime.EventsEmit` a jsou vyjmenované v allowlistu grep testu.

---

## 2. Protokol, snapshot, resume

```
klient → server   {t:"call",   id, cmd, args}
klient → server   {t:"resume", since}
server → klient   {t:"welcome", environmentId, seq, scopes}
server → klient   {t:"reply",  id, result} | {t:"reply", id, error:{code,message}}
server → klient   {t:"event",  name, payload}
server → klient   {t:"shell",  seq, ev}
server → klient   {t:"resync"}
```

Jména eventů jsou **identická** s dnešními Wails eventy (`pty-data-{id}`,
`chat-event-{id}`, `acp-req-{id}`, …). Žádný překlad = žádná mapa, která může
zapadnout.

**PTY bajty jdou binárně.** Dnes `wsArgs.Data []int` — JSON pole intů, ~3×
overhead proti base64, ~4× proti raw. Frame: `[1][4B ptyID][payload]`. Ostatní
kanály jsou low-rate, zůstávají JSON.

**Ordering.** Jeden goroutine na spojení, jedna odchozí fronta. Reply a event
pro tentýž objekt nemůžou přijít přeházené — což je dnes u `pty-data` vs
`create_pty` reply nezaručené.

### Go dispatch (`remoteapi.go`)

```go
type remoteCmd struct {
    method string
    scope  Scope
}

var remoteAllowed = map[string]remoteCmd{
    "list_workspaces": {method: "ListWorkspaces", scope: scopeOrchRead},
    "write_pty":       {method: "WritePty",       scope: scopeTerminal},
    "create_chat":     {method: "CreateChat",     scope: scopeOrchOperate},
    // ...
}
```

Reflexe dělá **jen** marshalling: args JSON object → poziční `reflect.Value`
podle jmen parametrů z Wails-generovaných bindings. Rozhodnutí *co* je vystavené
a *pod jakým scope* zůstává ten explicitní map literal. Tím mizí `wsArgs`
god-struct i hand-typed `dispatch`.

### Snapshot

Jeden RPC `shell_snapshot` → jeden round trip = první paint.

```go
type ShellSnapshot struct {
    Seq           int64
    EnvironmentID string
    Workspaces    []Workspace
    Tabs          map[int64][]TerminalTab      // VŠECHNY workspaces, ne jen mounted
    Phases        map[string]agentphase.Phase
    Chats         []ChatSummary
}

type ChatSummary struct {
    ID          int64
    WorkspaceID int64
    Title       string
    AgentKind   string
    Phase       agentphase.Phase // TÝŽ typ jako u PTY
    PendingKind string           // "" | "permission" | "question" | "plan"
}
```

Chat i PTY nesou **stejnou** `agentphase.Phase`. Jinak vzniknou dvě derivace
téhož a `chatStatus()` se rozejde s `tabStatus()` — což se mobilu už jednou
stalo (`store.ts:122`).

`PendingKind` je **jen tvar tečky**, ne payload. Samotný control/permission
protokol zůstává na raw kanálu, jak dnes — je to UI rozhodnutí, ne transcript.

### Stream + resume

`seq` z jednoho `atomic.Int64`, bumpne na každý shell event. `ev` je tagged
union: `{k:"phase", id, phase}` · `{k:"tabs", workspaceID, tabs}` ·
`{k:"workspaces"}` · `{k:"chat", chatID, …}`.

Server drží **ring 512 posledních** shell eventů in-memory. Klient po reconnectu
pošle `{t:"resume", since}`:

- `since` v ringu → dojedou delty
- mimo ring nebo po restartu procesu → `{t:"resync"}` → klient si vezme snapshot znovu

**Žádný per-client stav na serveru**, jen ten ring. Reconnect backoff je vlastní
modul (`src/runtime/reconnectBackoff.ts`, portovaný z t3code).

### Co se streamu záměrně neúčastní

- **Tělo transcriptu.** `chat_stream(chat_id, ord, kind, line)` + `folded_ord` +
  `LoadChatEventsSince` je persistentní v SQLite a lepší, než co bych postavil.
  Snapshot nese jen `Phase`/`PendingKind`; tělo dojede přes `replayChatStream()`.
  Nedělám druhý replay log pro totéž.
- **PTY scrollback.** Daemon má ring buffer a reattach ho přehraje.

---

## 3. Fáze agenta patří serveru

Dnes fáze žije ve `Terminal.vue` + `agentStatus.ts` (XState, 198 ř.). Existuje
jen pro mountnutý workspace, restart appky ji zahodí, a mobil si ji musel odvodit
znovu.

```go
// src-wails/internal/agentphase
type State string // starting|running|waiting_input|waiting_approval|done|failed|stale

type Phase struct {
    State       State
    Detail      string // error_type, jméno blocking toolu
    Model       string
    Title       string
    TurnEndedAt int64  // 0 = běží
    UpdatedAt   int64
}

func Next(cur Phase, ev Event, now int64) Phase // pure, žádné IO
```

Jména z t3code. `stale` je jméno pro dnešní dead-PTY watchdog — `interrupt`
říká, co se udělalo, ne co se stalo.

**Vstupy**, všechny už v Go: hook eventy (`hookserver.go` drží latest state per
PTY + `ReplayStatus`), provider runtime eventy (`turn.completed`/`turn.failed`
z `providerruntime.go`), PTY liveness z daemonu, foreground poll.

`internal/agentphase` nesmí importovat `database/sql`, Wails runtime, ani nic
z `main`. IO patří store v `main`.

**Persist** do SQLite `pty_phase` / `chat_phase`, řádek per id. Emit `phase-{id}`
přes bus.

### `review` a transient `done` nejsou stavy

Jsou to **read receipty**. Závisí na `isWatching`, což je per-device — telefon a
desktop mají na tutéž fázi různou odpověď. Celá klientská derivace:

```ts
// src/runtime/displayStatus.ts
review = phase.state === "done" && phase.turnEndedAt > seenAt
```

Kdo přidá `review` do Go, obrací tohle rozhodnutí.

Vedlejší úklid zdarma: `markTabSeen` přestane soutěžit se status transitions.
Dnes je `review` *stav*, takže „viděl jsem to" musí stav přepsat — race. Jako
read receipt je to `seenAt = now`, monotónní, bez závodu. `STATUS_PRIORITY`
ztratí `review` slot.

Fáze 2 má hodnotu, i kdyby remote access nikdy nedojel: status přežije restart
a funguje pro nemountnutý workspace.

---

## 4. Endpointy, párování, auth

### Doménový model (t3code, verbatim)

| typ | kdo autoří | kde žije |
|---|---|---|
| `Environment` | server o sobě | `<app-data>/environment.json` |
| `KnownEnvironment` | klient | `localStorage` PWA |
| `AccessEndpoint` | klient (vybraný) | součást `KnownEnvironment` |
| `AdvertisedEndpoint` | provider | z `remote_endpoints` RPC |

`environmentId` = stabilní UUID z prvního spuštění. Klíčuje klientský stav a
přežije změnu IP, hostname i tailnetu. Bez něj nelze poznat „tohle je ten samý
stroj jinou cestou".

```go
type EndpointProvider interface {
    Name() string
    Endpoints() []AdvertisedEndpoint // best-effort; chyba = prázdný slice
}

type AdvertisedEndpoint struct {
    Kind                  string // "loopback" | "tailscale-magicdns" | "ssh-forward" | ...
    HTTPBase              string
    WSBase                string
    Reachability          string // loopback | lan | private | public | tunnel
    HostedHTTPSCompatible bool
    Default               bool
    Available             bool
}
```

Den 1 dva providery: `loopback` (vždy) a `tailscale` (wrapper nad existujícím
`GetTailscaleStatus`/`TailscaleServe`). SSH-forward přijde jako **další
provider**, ne jako nová větev.

Preference se ukládá **podle `Kind`**, ne podle URL (`"tailscale-magicdns"`, ne
`"https://mac-mini.tail1234.ts.net"`) — tailnet IP se mění a MagicDNS jméno taky
při přejmenování stroje.

Selection order, když uživatel nic nevybral (t3code, verbatim):

1. `HostedHTTPSCompatible`
2. `Default`
3. non-loopback
4. loopback jen pro same-machine klienty

`remote_endpoints` je **desktop RPC přes Wails binding**, ne pre-auth endpoint na
síti. Vyjmenované adresy stroje jsou recon informace; nikdo je nepotřebuje před
spojením. QR kód pro párování se vykresluje v Settings na desktopu.

**Topologie:** UI v1 shipuje jen phone → desktop. Model je multi-environment od
začátku, takže desktop environment switcher přijde bez rewrite.

### Auth — jeden model pro všechny endpointy

Tailscale je **provider adres, ne identita**. Odpadá tím druhá auth větev
(`Tailscale-User-Login` header trust): míň kódu a menší plocha na chybu.

```
pairing grant (6 číslic, TTL 3 min, jednorázový)
   → POST /pair       → { sessionToken, environmentId, scopes }
   → POST /ws-ticket  (Bearer) → ticket (TTL 30 s, jednorázový)
   → wss://…/ws?ticket=…
```

Scopes: `orchestration:read` · `orchestration:operate` · `terminal:operate` ·
`access:read` · `access:write`.

Spárovaný telefon dostane první tři. **Nemůže párovat další zařízení** —
`access:write` má jen desktop bootstrap. Enforcement je **per RPC metoda**
(`remoteAllowed` v §2), ne jen při vydání ticketu; ticket není povolení volat
všechno.

Settings ukáže `{deviceName, deviceType, lastSeen, addedAt}` a umí odvolat jedno
zařízení.

### Bezpečnostní invarianty

Každý z nich musí být v kódu jako komentář **i jako test**, protože každý se dá
porušit tichou změnou konfigurace.

**1. Listener bindí výhradně na `127.0.0.1`.** Jediná cesta dovnitř z tailnetu je
`tailscale serve`, který terminuje HTTPS a přeposílá na loopback. Přímý listen na
tailnet IP je zakázaný nejen kvůli auth, ale i proto, že plain HTTP na private IP
**není secure context** — nešel by service worker, a tedy ani instalovatelná PWA
ani push.

**2. `tailscale funnel` musí být na naší cestě vypnutý.** Funnel vystaví týž
handler na veřejný internet. Startup check: pokud je funnel zapnutý, remote
access **odmítne nastartovat** a nahlásí to. Fail closed, ne warning do logu.

**3. Token nikdy v query parametru.** Dnešní `httpserver.go:221` to dělá a
přiznává to v komentáři. Nahrazeno WS ticketem — jednorázový a 30 s živý, takže
i když skončí v logu proxy, je mrtvý.

### Migrace: hard cutover

`http.token` se při prvním startu **smaže** a nahradí prázdným seznamem zařízení.
Telefon se musí spárovat znovu. Staré credentials se nemapují na nové, protože
jejich model přístupu je jiný — sdílený token bez scopes nelze poctivě přeložit
na scoped per-device session. Stejně to udělal t3code v migraci `031`.

---

## 5. `src/runtime/` a co umře

```
src/runtime/
  transport.ts         WS klient, invoke/listen, reconnect
  environment.ts       KnownEnvironment, endpoint selection
  reconnectBackoff.ts  ← portovaný z t3code
  shellSnapshot.ts     typy + applyShellEvent + resume/resync
  displayStatus.ts     fáze + seenAt → barva (~40 ř.)
  chatProjection.ts    ← přesun z src/lib/
  chatSession.ts       ← přesun z src/lib/
```

Hranice vynucená testem: `src/runtime/**` nesmí importovat z `src/components`,
`src/views`, `src/mobile`, `src/stores`, ani `xterm`. Povoleno: `vue` (jen
`ref`/`reactive`/`computed`) a pure TS.

Pinia stores zůstávají v `src/stores/` a **sdílí je oba klienti** — jsou
UI-agnostic a už dnes testovatelné (router má memory history mimo browser).
`src/mobile/` po tomhle: jen views + mobilní layout + router. `mobile.html` +
`VITE_TARGET=mobile` + `dist-mobile/app` + `go:embed` zůstávají beze změny.

### Co se maže

| soubor | ř. | proč |
|---|---|---|
| `src/mobile/store.ts` | 616 | duplikuje stores + `applyEvent` + `statusFor`/`watchTabStatus` |
| `src/mobile/api.ts` | 128 | → `runtime/transport.ts` |
| `src-wails/httpserver.go` | 411 | → `remoteserver.go` |
| `src-wails/remote.go` (+test) | 404 | druhý read model nad `config.json` |
| `src/machines/agentStatus.ts` (+test) | 198 | → `internal/agentphase` |
| `src/lib/terminalStatus.ts` | 104 | → `displayStatus.ts` (~40 ř.) |
| `switch` v `wailsCompat/core.ts` | ~330 | → ~30 ř. WS klienta |
| `ScopeRemote` v `internal/control` | 14 refs | mobil jede přes `invoke`, ne přes verby |

Nové Go: `environment.go` · `remoteapi.go` · `remoteserver.go` · `remoteauth.go` ·
`internal/agentphase/` · `bus.go` · *(fáze 7)* `push.go`.

**`internal/control` verby zůstávají a nesjednocují se s `invoke`.** Překryv jsou
3 jména ze 155, granularita je jiná (verby jsou hrubozrnné, agent-facing, část
jde přes `UIBridge` a čeká na ack frontendu). Sloučení by znamenalo přepsat 130
commandů na verby — velký diff bez vztahu k remote access.

### PWA v1

Dashboard stavů · chaty včetně follow-upů a permission · spawn nového
chatu/agenta · git diff.

`TerminalView.vue` zůstává, přepnutý na sdílený transport — je to pár řádků, když
už PTY kanál existuje, a zachraňuje „agent čeká na y/n přímo v PTY".

**Web Push je mimo v1**, sedí jako fáze 7 a je odříznutelná. Nahlas, co to
znamená: bez pushe je remote pollovací nástroj — musíš se podívat, abys věděl, že
se něco děje.

---

## 6. Fáze

Každá shipnutelná samostatně.

1. `environmentId` + endpoint providery + `remote_endpoints` RPC
2. `internal/agentphase` + persist + event bus *(desktop profituje hned)*
3. `src/runtime/` + WS transport; **desktop přepnutý na WS** *(chování identické, zatím jen loopback)*
4. `remoteserver.go` + `remoteapi.go` + snapshot/resume + binární PTY frames
5. `remoteauth.go` — pairing, scopes, tickety, revoke; `http.token` umírá
6. `src/mobile` na sdílených stores; `store.ts`/`api.ts`/`remote.go` mazané; PWA v1 surface
7. *(odříznuté)* Web Push — permission + error

Fáze 2 a 3 mají hodnotu, i kdyby remote access nikdy nedojel. To je záměr, ne
náhoda.

### Fáze 7 — Web Push, až se pro ni rozhodneme

Budí **jen `waiting_approval` a `failed`**. Obojí znamená *zablokováno nebo
rozbito*. `done` nebudí: agenti dobíhají celý den a `done` se auto-clearuje, když
se člověk kouká — budit na něj je nejrychlejší cesta k vypnutí notifikací úplně.

VAPID keypair v app-data, `push_subscriptions` v SQLite, `webpush-go`, `push`
handler v service workeru. `push_subscriptions` je klíčovaná **vlastním
`deviceId`** (klientem generované UUID v `localStorage`), ne session tokenem —
subscription musí přežít re-pairing. Revoke zařízení maže i jeho subscription.

Háček: iOS pošle push do PWA jen když je appka přidaná na home screen (iOS 16.4+).
UX prerekvizita, ne kód — MagicDNS HTTPS secure context už dodá.

---

## 7. Testy

Existují proti hnilobě, ne kvůli pokrytí.

1. Každá `App` metoda je v `remoteAllowed` **nebo** `remoteDenied` → nová metoda shodí build
2. Import grep: `wails/v2/pkg/runtime` jen v `main.go` / `window.go` / `dialog.go` / `updater.go` + local-only allowlist
3. `src/runtime/**` import boundary
4. `agentphase`: portované případy z `agentStatus.test.ts` + `stale` + interim-stop (`background_tasks`)
5. Listen na non-loopback adrese → server odmítne start
6. `tailscale funnel` zapnutý na naší cestě → server odmítne start
7. Pairing: expirovaný grant, dvakrát použitý grant, revokované zařízení → 401; ticket použitý dvakrát → 401
8. Scope: session bez `access:write` volá `create_pairing_grant` → 403
9. Snapshot/resume: `since` v ringu → delty; `since` mimo ring → `resync`; restart procesu → `resync`

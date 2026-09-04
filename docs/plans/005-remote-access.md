# Plán: Remote access na session (t3code model)

## Cíl

Jeden klientský runtime, jeden protokol, N klientů. Mobilní PWA má vlastní UI,
ale **nesmí mít vlastní parser, vlastní store ani vlastní derivaci statusu** —
protože přesně to dnes má, a proto se rozjíždí.

Zároveň: „agent se zasekl na permission" má dojít na telefon, který spí. To je
rozhodnutí, které nemůže padnout v klientovi, protože v tu chvíli žádný klient
neexistuje.

Referenční implementace: `pingdotgg/t3code`
(`~/.opensrc/repos/github.com/pingdotgg/t3code/main`), zejména
`docs/architecture/remote.md`, `packages/client-runtime/`,
`packages/shared/src/agentAwareness.ts`, `apps/server/src/auth/`.

## Problém

Burrow má **tři paralelní API surfaces a dva klienty**:

| surface | tvar | konzument |
|---|---|---|
| `wailsCompat/core.ts` `invoke()` | 121 snake_case commandů, named args | plný `src/` desktop |
| `httpserver.go` `dispatch()` | 11 hand-typed commandů, jeden `wsArgs` god-struct | `src/mobile/` (2 529 ř., vlastní store) |
| `internal/control` verby | registry, 25 verbů, `ScopeRemote` | CLI / MCP / mobil |

`remote.go` (241 ř.) navíc čte `config.json` + SQLite do shapů šitých na
`src/mobile/store.ts` — druhý read model transcriptu.

Důsledky, které jsou v repu měřitelné dnes:

- `src/mobile/store.ts` má vlastní `statusFor` (:118), `chatStatus` (:122),
  `watchTabStatus` (:129 — s vlastní kopií 4000 ms auto-clearu ze
  `settleDone()`) a **vlastní `applyEvent` (:310)** duplikující
  `src/lib/chatProjection.ts`.
- `events.go:27` `emitWorkspacesChanged` používá plain `runtime.EventsEmit`
  místo `emitAll` — takže `workspaces-changed` se na telefon **nikdy
  nedostane**. Z 12 raw emitů je 10 legitimně lokálních (menu, float,
  `update:progress`, `lsp-msg`); tenhle jeden je díra.
- `httpserver.go:221` přiznává v komentáři, že token jede jako query param.
  Jeden dlouhoživoucí `http.token` v souboru: leak = plný přístup ke stroji
  (včetně spawn agenta a čtení FS) a nelze odvolat jedno zařízení.
- Derivovaný status (`review` vs `done`) žije ve `Terminal.vue`. Existuje jen
  pro mountnuté workspaces a **restart appky ho zahodí**. Telefon proto vidí
  `review` ve významu „desktop se na to nekoukal", což je nesmysl.

## Jak to řeší t3code

Klient↔server hranice je **vždycky** HTTP/WS. Remote není feature; je to „ukaž
stejnému klientovi jinou URL". Domain model:

- `ExecutionEnvironment` — jedna běžící instance serveru, stabilní
  `environmentId`. Vlastní providery, projekty, thready, terminály, git, FS.
- `KnownEnvironment` — klientský záznam „umím se sem dostat". Není
  server-authored, žije per-device.
- `AccessEndpoint` — jedna konkrétní cesta k environmentu. Environment se
  nemění, mění se jen cesta.
- `AdvertisedEndpoint` — server/provider-authored kandidát. Nese reachability
  hint a `hostedHTTPSCompatible`. Je to **hint, ne důkaz** — o dosažitelnosti
  rozhodne až pokus o spojení.
- Endpoint **providery** jsou add-on mimo core. Tailscale je první z nich, ne
  součást modelu.

A striktní oddělení **access** (jak klient mluví WS) od **launch** (jak server
vznikl). Zed je reference pro launch discipline, ne pro transport.

Stav se drží jako **snapshot + reducer**: `OrchestrationShellSnapshot` +
`applyShellStreamEvent` v `packages/client-runtime`, konzumované web/desktop/
mobile. Reconnect má vlastní modul (`reconnectBackoff.ts`).

Derivaci fáze agenta dělá **server** (`projectThreadAwareness()` volaná z
`AgentAwarenessRelay`), fáze jsou
`starting|running|waiting_for_approval|waiting_for_input|completed|failed|stale`.
**`review` u nich není fáze** — je to `completed` + client-side read/unread proti
`updatedAt`.

## Co si z toho beru a co ne

Beru: domain model, oddělení access/launch, endpoint providery, snapshot+reducer,
shared client-runtime, server-owned fázi, pairing grant → per-device session,
WS ticket, `stale` jako jméno pro dead-PTY watchdog.

Nekupuju: token exchange (RFC 8693) tvar, DPoP, 8 scopes, expirující access
tokeny s refreshem, relay/cloud vrstvu, snapshot/patch pro *všechen* stav.
Jsem jeden uživatel; deny-by-default už mám na správné úrovni (viz `remoteAllowed`
níže) a druhá vrstva scopes bez druhého uživatele je jen kód, který se dá špatně
nakonfigurovat.

Zůstává úmyslně mimo (t3code non-goals, přebírám): **provider auth se
nesynchronizuje** (Claude/Codex login je per-machine) a `workspace` zůstává
environment-local — lokální a remote klon téhož repa jsou dva workspacy.

---

## 1. Domain model

| typ | kdo autoří | kde žije |
|---|---|---|
| `Environment` | server o sobě | `<app-data>/environment.json` |
| `KnownEnvironment` | klient | `localStorage` telefonu / SQLite desktopu |
| `AccessEndpoint` | klient (vybraný) | součást `KnownEnvironment` |
| `AdvertisedEndpoint` | server / provider | vrácený z `remote_endpoints` |

`environmentId` je stabilní UUID generovaný při prvním spuštění. Klíčuje všechen
klientský stav a přežije změnu IP, hostname i tailnetu. Bez něj nelze poznat
„tohle je ten samý stroj jinou cestou".

```go
type EndpointProvider interface {
    Name() string
    Endpoints() []AdvertisedEndpoint  // best-effort; chyba = prázdný slice
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

Den 1 dva providery: `loopbackProvider` (vždy) a `tailscaleProvider` (wrapper nad
existujícím `GetTailscaleStatus` / `TailscaleServe`). SSH-forward endpointy
přidá desktop v multi-env fázi — **jako provider, ne jako nová větev**.

Uživatelská preference se ukládá **podle `Kind`, ne podle URL**
(`"tailscale-magicdns"`, ne `"https://mac-mini.tail1234.ts.net"`) — tailnet IP se
mění a MagicDNS jméno taky při přejmenování stroje.

`remote_endpoints` je **desktop-side RPC** (jde přes `wailsTransport`), ne
pre-auth endpoint na síti: seznam adres a QR kód pro párování se vykresluje
v Settings na desktopu. Nezveřejňuje se — vyjmenované adresy stroje jsou
recon informace a nikdo je nepotřebuje před spojením.

Selection order, když uživatel nic nevybral (t3code, verbatim):
1. `HostedHTTPSCompatible`
2. `Default`
3. non-loopback
4. loopback jen pro same-machine klienty

**Topologie:** UI v1 shipuje jen phone → desktop. Model je ale multi-environment
od začátku, takže desktop environment switcher přijde bez rewrite.

## 2. Transport + protokol

```ts
// src/runtime/transport.ts
export interface Transport {
  invoke<T>(cmd: string, args?: Record<string, any>): Promise<T>
  listen<T>(event: string, h: (p: T) => void): Promise<() => void>
}
```

`wailsTransport` = dnešní `wailsCompat/core.ts` + `event.ts` beze změny chování.
`wsTransport` = nová, WS proti `KnownEnvironment`. `wailsCompat/core.ts` se
z 121-case switche stane **re-export aktivního transportu** → nula změn v
call-sitech `src/`.

Framy:

```
klient → server   {t:"call",   id, cmd, args}
klient → server   {t:"resume", since}
server → klient   {t:"reply",  id, result} | {t:"reply", id, error}
server → klient   {t:"event",  name, payload}
server → klient   {t:"shell",  seq, ev}
server → klient   {t:"resync"}
```

Jména eventů jsou **identická** s Wails eventy. Žádný překlad = žádná mapa,
která může zapadnout.

**PTY bajty**: `pty-data-{id}` je horký kanál a dnes jede jako JSON array intů
(`wsArgs.Data []int`) — přes tailnet ~3× overhead proti base64 a ~4× proti
binárnímu framu. Jde jako **binární WS frame** (header `ptyID` + raw bajty).
Ostatní kanály jsou low-rate, zůstávají JSON.

### Go dispatch

Generovaný reflexí nad `App` metodami, ale **opt-in allow-list**:

```go
// remoteapi.go
var remoteAllowed = map[string]string{  // wire cmd → App method
    "list_workspaces": "ListWorkspaces",
    "write_pty":       "WritePty",
    // ...
}
```

Reflexe dělá **jen marshalling** (args JSON object → poziční `reflect.Value`
podle jmen parametrů z Wails-generovaných bindings). Rozhodnutí *co* je
vystavené zůstává ten explicitní map literal — nová `App` metoda je mimo síť,
dokud ji tam někdo vědomě nenapíše. Tím mizí `wsArgs` god-struct i hand-typed
`dispatch`.

### Event fanout

`emitAll` se stane **jediné dveře**; `emitWorkspacesChanged` se opraví.
Vynuceno testem (viz §7.2).

## 3. Fáze agenta patří backendu

`review` vs `done` závisí na `isWatching`, což je per-device. To ale **není vstup
do fáze** — je to read receipt. Rozhodující argument pro backend je push: telefon
spí, PWA není otevřená, žádný klient neexistuje, a přesto musí padnout
rozhodnutí „tohle stojí za vzbuzením". Stejný důvod, jaký už jednou vyhrál
v `providerruntime.go`.

Nový package `src-wails/internal/agentphase`:

```go
type Phase struct {
    State       string // running | waiting | permission | done | error | stale
    Detail      string // error_type, jméno blocking toolu
    Model       string
    Title       string
    TurnEndedAt int64  // 0 = běží
    UpdatedAt   int64
}
```

Vstup: hook eventy (`hookserver.go:89` už drží latest state per PTY +
`ReplayStatus`), provider runtime eventy (`turn.completed` / `turn.failed`), PTY
liveness z daemonu. Čistá funkce nad fakty, která Go už má.

`stale` je jméno pro dnešní dead-PTY watchdog — kradeno z t3code, protože
`interrupt` říká, co se udělalo, ne co se stalo.

Persist do SQLite (`pty_phase`, jeden řádek per pty). Dnes stav žije ve
`Terminal.vue` a restart appky ho ztratí; po tomhle přežije.

**Klient nedostane status, dostane fázi.** `seenAt` je jediné, co si drží sám:

```ts
// src/runtime/displayStatus.ts — celá derivace, ne state machine
review = phase.state === "done" && phase.turnEndedAt > seenAt
```

Z `agentStatus.ts` (198 ř.) + `terminalStatus.ts` (104 ř.) + mobilního
`statusFor`/`watchTabStatus` zbyde jeden soubor pod 40 řádků. Testy z
`agentStatus.test.ts` se portují do Go.

Vedlejší úklid zdarma: `markTabSeen` přestane soutěžit se status transitions
(dnes je `review` *stav*, takže „viděl jsem to" musí stav přepsat — race), a
`STATUS_PRIORITY` ztratí `review` slot.

## 4. Snapshot + stream

```go
type ShellSnapshot struct {
    Seq           int64
    EnvironmentID string
    Workspaces    []Workspace
    Tabs          map[int64][]TerminalTab // per workspace, VŠECHNY, ne jen mounted
    PtyPhases     map[string]agentphase.Phase
    Chats         []ChatSummary
}

type ChatSummary struct {
    ID          int64
    WorkspaceID int64
    Title       string
    AgentKind   string
    Phase       agentphase.Phase // TÝŽ typ jako u PTY — jedna derivace, dva zdroje
    PendingKind string           // "" | "permission" | "question" | "plan"
}
```

Chat i PTY nesou **stejný `agentphase.Phase`**. Jinak by existovaly dvě
derivace téhož a `chatStatus()` by se rozešel s `tabStatus()` — což se dnes
mobilu už stalo (`store.ts:122`).

`PendingKind` je **jen tvar tečky**, ne payload. Samotný control/permission
protokol zůstává na raw kanálu, jak dnes — je to UI rozhodnutí, ne transcript
(viz CLAUDE.md, „Still on the raw channel, deliberately").

Jeden RPC `shell_snapshot` → jeden round trip = první paint.

Stream: `{t:"shell", seq, ev}`, `seq` z jednoho `atomic.Int64` bumpnutého na
každý shell event. `ev` je tagged union: `{k:"pty_phase", ptyID, phase}` ·
`{k:"tabs", workspaceID, tabs}` · `{k:"workspaces"}` · `{k:"chat", chatID, …}`.

Reconnect: klient pošle `{t:"resume", since}`. Server drží **ring buffer
posledních 512 shell eventů** (in-memory, ne SQLite — shell eventy jsou levné a
snapshot je vždy fallback). `since` v bufferu → delty; mimo buffer nebo po
restartu procesu → `{t:"resync"}` a klient si vezme snapshot znovu. **Žádný
per-client stav na serveru**, jen ten ring.

Co se streamu **neúčastní** a je to záměr:

- **Tělo transcriptu.** `chat_stream` + `ord` + `folded_ord` +
  `LoadChatEventsSince` je persistentní v SQLite a lepší, než co bych postavil.
  Snapshot nese jen `turnState`/`pendingRequest`; tělo dojede přes existující
  `replayChatStream()`. Nedělám druhý replay log pro to samé.
- **PTY scrollback.** Daemon má ring buffer a reattach ho přehraje.

## 5. `src/runtime/`

```
src/runtime/
  transport.ts        interface + wailsTransport + wsTransport, boot-time select
  environment.ts      KnownEnvironment, endpoint selection, reconnectBackoff
  shellSnapshot.ts    typy + applyShellEvent + resume/resync
  displayStatus.ts    fáze + seenAt → barva (~40 ř.)
  chatProjection.ts   ← přesun z src/lib/
  chatSession.ts      ← přesun z src/lib/
```

`agentStatus.ts` a `terminalStatus.ts` se **nepřesouvají, mažou se** (§3).

Hranice vynucená testem (§7.3): `src/runtime/**` nesmí importovat z
`src/components`, `src/views`, `src/mobile`, `src/stores`, ani `xterm`.
Povoleno: `vue` (jen `ref`/`reactive`/`computed`) a pure TS.

Pinia stores zůstávají v `src/stores/` a jsou sdílené — jsou UI-agnostic a už
dnes testovatelné (router má memory history mimo browser). Nepřesouvám je;
`src/mobile` je jen začne importovat místo vlastního `store.ts`.

`src/mobile/` po tomhle: **jen views + mobilní layout + router.** `mobile.html`
+ `VITE_TARGET=mobile` + `dist-mobile/app` + `go:embed` zůstávají beze změny —
oddělený bundle je přesně to, co „vlastní UI, sdílený runtime" potřebuje.

## 6. Auth + pairing

Dvě cesty dovnitř, protože dva druhy endpointů.

### 6.1 Tailnet endpoint → Tailscale je identita

```
telefon → tailscale serve (HTTPS 443, MagicDNS) → 127.0.0.1:PORT (Go)
```

Go listener **bind výhradně na `127.0.0.1`**. Header `Tailscale-User-Login` se
věří **jen a pouze** když `RemoteAddr` je loopback.

> **Bezpečnostní invariant** (musí být v kódu jako komentář i jako test):
> header-trust je vázaný na loopback origin. `tailscale serve` je jediná cesta
> dovnitř tailnetem. Když se bind změní na `0.0.0.0`, header se přestane věřit
> úplně — ne že se začne věřit komukoli. **Fail closed.**
>
> Přímý listen na tailnet IP je zakázaný nejen kvůli header spoofingu, ale i
> proto, že plain HTTP na private IP **není secure context** → nešel by service
> worker ani push.
>
> **Funnel musí být explicitně vypnutý.** `tailscale funnel` vystaví ten samý
> handler na veřejný internet a Tailscale u funnel requestů identity headery
> **neposílá** — prošly by tedy jako „loopback bez identity". Startup check:
> když je funnel na naší cestě zapnutý, remote access **odmítne nastartovat** a
> nahlásí to.

Whitelist loginů: default = ten, co appku spustil (z `tailscale status --json`
vlastní node). Přidání dalšího je vědomý krok v Settings.

### 6.2 Endpointy bez identity → per-device token

Pro SSH-forward a budoucí LAN/relay.

- **Pairing grant**: desktop vygeneruje 6místný kód, TTL 3 min, jednorázový.
  Klient ho vymění na `POST /pair` za `{deviceToken, environmentId, deviceName}`.
  Grant se maže při použití i při expiraci.
- **Token per zařízení**, ne shared. Settings zobrazí
  `{deviceName, deviceType, lastSeen, addedAt}` a umí **revoke jednoho**.
- **Token nikdy v query.** `POST /ws-ticket` (Bearer) → jednorázový ticket,
  TTL 30 s → `wss://…/ws?ticket=…`. Řeší přiznanou vadu v `httpserver.go:221`.
  Ticket je jednorázový a krátký, takže i když skončí v logu, je mrtvý.

### 6.3 Migrace

`http.token` se při prvním startu smaže a nahradí prázdným device seznamem.
Telefon musí spárovat znovu. **Hard cutover**, jako t3code migrace `031` — staré
credentials se nemapují na nové, protože jejich model přístupu je jiný.

## 7. Testy

Existují proto, že bez nich to zhnije — ne kvůli pokrytí.

1. `remoteAllowed` vs `App` metody: každá metoda musí být v allow **nebo** deny
   listu. Spadne na nové neuvážené.
2. `runtime.EventsEmit` grep: jen `events.go` + explicitní local-only allowlist
   (menu, float, `update:progress`, `lsp-msg`).
3. `src/runtime/**` import boundary.
4. `agentphase`: portované případy z `agentStatus.test.ts` + `stale` +
   interim-stop (`background_tasks`).
5. Podvržený `Tailscale-User-Login` z non-loopback → 401.
6. Funnel zapnutý → remote access odmítne start.
7. Pairing: expirovaný grant, dvakrát použitý grant, revokovaný device → 401.
   Ticket použitý dvakrát → 401.
8. Snapshot/resume: `since` v bufferu → delty; `since` mimo → `resync`; restart
   procesu → `resync`.

## 8. Co umře

| soubor | ř. | proč |
|---|---|---|
| `src/mobile/store.ts` | 616 | duplikuje stores + `applyEvent` + `statusFor`/`watchTabStatus` |
| `src/mobile/api.ts` | 128 | nahrazeno `runtime/transport.ts` |
| `src-wails/remote.go` + test | 404 | druhý read model nad `config.json` |
| `src/machines/agentStatus.ts` | 198 | portováno do `internal/agentphase` |
| `src/lib/terminalStatus.ts` | 104 | → `runtime/displayStatus.ts` (~40 ř.) |
| `ScopeRemote` v `internal/control` | 14 refs | mobil jede přes `invoke`, ne přes verby |

Přepisované: `httpserver.go` (411 ř.) → `remoteserver.go`.

Nové Go: `environment.go` · `remoteapi.go` · `remoteserver.go` · `remoteauth.go` ·
`internal/agentphase/` · `push.go`.

**`internal/control` verby zůstávají a nesjednocují se s `invoke`.** Překryv je
3 jména ze 146 (25 verbů vs 121 commandů), granularita je jiná (verby jsou
hrubozrnné, agent-facing, část jde přes `UIBridge` a čeká na ack frontendu), a
sloučení by znamenalo přepsat 121 invoke commandů na verby — velký diff bez
vztahu k remote access.

## 9. Push (poslední fáze, odříznutelná)

Web Push, budí **jen `permission` a `error`**. Obě znamenají *zablokováno nebo
rozbito*. `done` nebudí: agenti dobíhají celý den a `done` se auto-clearuje, když
se člověk kouká — budit na něj je nejrychlejší cesta k vypnutí notifikací úplně.

VAPID keypair v app-data, `push_subscriptions` v SQLite, `webpush-go`, `push`
handler v service workeru. ~150 ř. Go.

`push_subscriptions` je klíčovaná **vlastním `deviceId`** (klientem generované
UUID v `localStorage`), ne device tokenem ani Tailscale loginem — subscription
musí přežít oba auth módy z §6 i re-pairing. Řádek nese
`{deviceId, endpoint, p256dh, auth, label, lastSeen}`. Revoke zařízení v §6.2
maže i jeho subscription.

**Háček**: iOS pošle push do PWA jen když je appka přidaná na home screen
(iOS 16.4+). UX prerekvizita, ne kód — MagicDNS HTTPS secure context už dodá.

Bez pushe je remote access pollovací hračka: hodnota není „můžu se podívat
z gauče", ale „vím, že se agent zasekl". Proto je to v plánu, ne mimo něj.
Nastavení, per-workspace mute, quiet hours a `done` notifikace jsou vlastní
feature s vlastním designem — sem nepatří.

## 10. Fáze

Každá shipnutelná samostatně.

1. `environmentId` + endpoint providery + `remote_endpoints` RPC. Nic se
   nerozbije, nic nového neumí.
2. `internal/agentphase` + persist + `emitAll` fix. **Desktop okamžitě
   profituje** — status přežije restart. Testovatelné bez remote.
3. `src/runtime/` + `transport.ts`; desktop přepnutý na `wailsTransport`. Čistě
   refactor, chování identické.
4. `remoteserver.go` + `remoteapi.go` + snapshot/resume. Mobil ještě na starém.
5. `remoteauth.go` — TS identita, grants, device tokeny, tickety. Starý
   `http.token` umírá.
6. `src/mobile` přepsaný na sdílené stores; `store.ts` / `api.ts` / `remote.go`
   mazané.
7. Web Push (permission + error).

Fáze 2 a 3 mají hodnotu i kdyby remote access nikdy nedojel. To je záměr, ne
náhoda.

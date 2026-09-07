# Remote Access — Implementation Plan, fáze 6 (telefon na sdílený runtime)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Telefon jede na `/v2/ws` — spáruje se šesti číslicemi, dostane vlastní token, první paint vezme z `shell_snapshot`, tečky čte z fází derivovaných na serveru, po výpadku se resumne. **To je první fáze, kterou je vidět na telefonu.** Staré `/ws`, `/rpc/`, `/pair` a sdílený `http.token` v tomtéž kroku umírají.

**Architecture:** Od fáze 3 je `invoke()` transport-agnostické — `core.ts` je pass-through na `src/runtime/transport.ts`, jediné, čím se desktop a telefon liší, je `EndpointSource`. Fáze 6 tedy nepíše žádnou druhou datovou cestu: přidá **remote** `EndpointSource` (uložené `{baseUrl, deviceToken}` → `POST /v2/ws-ticket` → ticket), sundá z `invoke` podmínku „musí být Wails runtime", a mobilní store přepíše z `BurrowWsClient` na ten samý `Transport`. Tím je celá `remoteAllowed` tabulka dostupná z telefonu — bez nové položky kdekoli.

**Tech Stack:** Vue 3 + Pinia + vitest; Go 1.25 (mazání).

**Spec:** `docs/superpowers/specs/2026-09-05-remote-access-t3code-design.md` §5 (co umře, PWA v1), §6 fáze 6

**Předchozí fáze:** `docs/superpowers/plans/2026-09-07-remote-access-phase-5.md` (hotová, `be70ba0` → `4b64a83`)

## Global Constraints

- **Komentáře v kódu anglicky.** Commit subject anglicky, Conventional Commits.
- **`src/mobile/store.ts` se NEMAŽE a mobil se NEPŘEPÍNÁ na desktopové Pinia stores.** Spec §5 to chce; je to refaktor bez přírůstku schopností. Mobilní views jsou napsané proti `WorkspaceGroup`/`RemoteChat` s vnořenými taby, desktopové stores mají jiný tvar a jinou životnost (`terminalTabs` je zrcadlo, jehož pravdou je mountnutá `Terminal.vue`). Co z `store.ts` v téhle fázi **skutečně** umře, je duplikace, která má náhradu: `api.ts` celý, `watchTabStatus` + `doneTimers` (nahrazeno fázemi + `displayStatus`), `scheduleReconnect` + `connectGeneration` (vlastní transport), N+1 načítání seznamů (nahrazeno `shell_snapshot`).
- **`remote.go` zůstává.** Je to read model chatů nad `config.json` a mobil na něm visí (`remote_list_chats`, `remote_create_chat`). Zabít ho znamená postavit chaty z desktopového tvaru — samostatná práce, ne součást přesunu transportu.
- **Chatové permission zůstávají na raw kanálu.** `claude-data-*` / `acp-req-*` a `watchChatPermissions` se nemění: control/permission protokol je vědomě mimo `ProviderRuntimeEvent` (viz CLAUDE.md), protože je to UI rozhodnutí, ne transcript.
- **Tečky terminálů jdou přes fáze, tečky chatů ne.** `phase-pty:{id}` + `displayStatus` nahrazuje `pty-hook-*`. Chat status dál plyne z `busy`/`pendingPermission`, protože permission na fázi není a `phase-chat:` nemá zatím konzumenta ani na desktopu.
- Frontend: `pnpm test`, `pnpm build` (typecheck). Go: `cd src-wails && go test ./...`. Vše: `just check`.
- `src/runtime/**` nesmí importovat z `src/components`, `src/views`, `src/stores`, `src/mobile`, ani `xterm` — hlídá `src/runtime/boundary.test.ts`.

---

## File Structure

| soubor | odpovědnost |
|---|---|
| `src/runtime/remoteEndpoint.ts` (nový) | uložené credentials, `pairDevice()`, `remoteEndpointSource()` |
| `src/runtime/remoteEndpoint.test.ts` (nový) | pairing chyby, ticket na každý pokus, token nikdy v URL |
| `src/runtime/transport.ts` (modify) | `onState(cb)` — UI musí umět říct „odpojeno" |
| `src/lib/wailsCompat/core.ts` (modify) | `remoteTransport()`; `invoke` už neházi mimo Wails, když jsou credentials |
| `src/mobile/api.ts` | **smazat** |
| `src/mobile/store.ts` (modify) | transport místo klienta; fáze místo `pty-hook`; snapshot místo N+1 |
| `src/mobile/views/ConnectView.vue` (modify) | párování proti `/v2/pair` |
| `src/mobile/views/DiffView.vue` (nový) | PWA v1 chybějící kus: git diff |
| `src-wails/httpserver.go` (modify) | `/ws`, `/rpc/`, `/pair`, `http.token`, `Broadcast` — smazat |
| `src-wails/remoteapi.go` (modify) | odstranit verby, které přežily jen pro staré `/ws` |
| `CLAUDE.md` (modify) | telefon jede na `/v2/ws`; co bylo smazáno |

---

### Task 1: remote `EndpointSource` — `invoke` funguje na telefonu

**Files:**
- Create: `src/runtime/remoteEndpoint.ts`, `src/runtime/remoteEndpoint.test.ts`
- Modify: `src/lib/wailsCompat/core.ts`

**Interfaces:**
- Produces:
  ```ts
  export interface RemoteCredentials { baseUrl: string; deviceToken: string; environmentId: string }
  export function loadRemoteCredentials(): RemoteCredentials | null
  export function saveRemoteCredentials(c: RemoteCredentials): void
  export function clearRemoteCredentials(): void
  export async function pairDevice(baseUrl: string, code: string, name: string, kind: string): Promise<RemoteCredentials>
  export function remoteEndpointSource(get: () => RemoteCredentials | null): EndpointSource
  ```

**Tohle je celá fáze v jednom tasku.** `invoke()` dnes končí `throw` bez Wails
runtime; jak se to zvedne, je z telefonu dostupná celá `remoteAllowed` tabulka,
protože `core.ts` je od fáze 3 pass-through. Žádná druhá datová cesta se nepíše.

Credentials leží v `localStorage` (ne v `@/lib/config`, což je `read_config`
přes `invoke` — tedy kruh: config by potřeboval transport a transport
credentials). Klíč nese `environmentId`, ne hostname, protože tailnet IP i
MagicDNS jméno se mění.

- [ ] **Step 1: Write the failing test**

```ts
// src/runtime/remoteEndpoint.test.ts
it("asks for a fresh ticket on every attempt", async () => {
  // A ticket is single-use, so a source that cached one would work exactly
  // once and then reconnect-loop forever.
});
it("never puts the device token in the ws url", async () => {
  // Spec §4 invariant 3, asserted from the client side too: the server
  // refusing it is one half, not sending it is the other.
});
it("reports a wrong pairing code distinctly from an unreachable host", async () => {
  // Both are 'it did not work' to the transport and two different things to
  // fix for the user.
});
it("gives up rather than looping when the stored token is revoked", async () => {
  // A revoked token gets 401 from /v2/ws-ticket forever. The source has to
  // surface that, or the phone spins with no way to re-pair.
});
```

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Implement `remoteEndpoint.ts`**

`remoteEndpointSource` returns `async () => ({wsUrl, ticket})`: POSTs
`/v2/ws-ticket` with `Authorization: Bearer <deviceToken>`, derives `wsUrl` from
`baseUrl` (`http`→`ws`, `https`→`wss`, `+ "/v2/ws"`). A **401 clears the stored
credentials** and throws a distinguishable error, so the UI falls back to the
pairing screen instead of retrying a revoked token until the user force-quits.

- [ ] **Step 4: `core.ts` — remote path**

`remoteTransport()` mirrors `desktopTransport()` with that source. `invoke`
picks: Wails runtime → desktop; stored credentials → remote; neither → the
existing throw (which is what `@/lib/config` still relies on before pairing).
The three `float-*` / `open_git_panel_window` cases stay Wails-only and must
now throw a *named* error on the phone rather than calling an undefined
binding.

- [ ] **Step 5: Run `pnpm test && pnpm build`**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(runtime): a remote endpoint source, so invoke works on the phone"
```

---

### Task 2: `onState` a mobilní store na sdíleném transportu

**Files:**
- Modify: `src/runtime/transport.ts`, `src/mobile/store.ts`
- Delete: `src/mobile/api.ts`
- Test: `src/runtime/transport.test.ts`

**Interfaces:**
- Produces: `Transport.onState(cb: (up: boolean) => void): () => void`

**Proč `onState`.** Mobilní UI musí umět říct „odpojeno / obnovuji spojení" a
transport je jediný, kdo to ví. Bez toho by store držel vlastní socket jen pro
ten indikátor — a byl by to druhý socket na tomtéž spojení.

Co ze `store.ts` odchází: `BurrowWsClient`, `scheduleReconnect`,
`reconnectAttempt`/`reconnectTimer`, `connectGeneration` (transport má vlastní
backoff a vlastní generation guard), `getClient`, `healthCheck` (spojení samo
je health check), `client.unsubscribe` (`listen` vrací unlisten).

- [ ] **Step 1: Write the failing test**

```ts
it("reports going down and coming back up", async () => {
  // onState is what a UI showing "reconnecting" reads; without it the store
  // would open a second socket just to know.
});
it("keeps reporting state across several reconnects", async () => {});
```

- [ ] **Step 2–5**: implement, delete `api.ts`, run tests.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(mobile): one transport for both clients, api.ts deleted"
```

---

### Task 3: tečky z fází, ne z `pty-hook`

**Files:**
- Modify: `src/mobile/store.ts`

Nahradit `watchTabStatus` + `doneTimers` (~35 ř.) tím, co dělá desktopová
`Terminal.vue`: poslouchat `phase-pty:{id}`, uložit `Phase`, a stav renderovat
`displayStatus(phase, seenAt, watching)` z `src/runtime/displayStatus.ts`.
`seenAt` per leaf v `localStorage` pod `burrow.seenAt.mobile` — **ne** pod
desktopovým klíčem: read receipt je per-device, což je celý důvod, proč `review`
není fáze.

Tím zmizí i mobilní verze bugu, kvůli kterému `done` bez mountnutého view
zůstávalo `review` navždy: `displayStatus` to počítá z `turn_ended_at` proti
`seenAt`, ne z pořadí eventů.

- [ ] **Step 1: Write the failing test** — `src/mobile/status.test.ts`: fáze
  `done` s `turn_ended_at` novějším než `seenAt` → `review`; po `markTabSeen`
  → `idle`; `waiting_approval` → `permission`.
- [ ] **Step 2–4**: implement, run.
- [ ] **Step 5: Commit** — `fix(mobile): read the server's phase instead of the legacy hook channel`

---

### Task 4: první paint ze `shell_snapshot`, resume po výpadku

**Files:**
- Modify: `src/mobile/store.ts`

`loadSessions()` dnes udělá `list_workspaces` a pak `list_terminal_tabs` **na
každý workspace** — N+1 round tripů na mobilní síti, každý s vlastní latencí.
Nahradit jedním `shell_snapshot`, který nese workspaces, taby všech workspaců,
fáze i chaty, a jeho `seq` ohlásit `transport.noteSeq()`.

`transport.onResync()` → vzít snapshot znovu a přepsat stav. To je ta půlka
resume, která na klientovi chyběla: server delty umí od fáze 4, ale nikdo si o
ně neřekl.

Orphan PTY (živé v daemonu, chybějící v SQLite) zůstávají — `shell_snapshot`
čte `terminal_tabs`, takže `list_pty_sessions` je pro ně dál potřeba.

- [ ] **Step 1: Write the failing test** — snapshot naplní workspaces i fáze
  jedním voláním; `resync` vyvolá nový snapshot; `noteSeq` dostane `snapshot.seq`.
- [ ] **Step 2–4**: implement, run.
- [ ] **Step 5: Commit** — `perf(mobile): one snapshot for the first paint, and resume after a drop`

---

### Task 5: párování v `ConnectView`

**Files:**
- Modify: `src/mobile/views/ConnectView.vue`, `src/mobile/store.ts`

Šest číslic proti `/v2/pair`, s `name` (výchozí z `navigator.userAgent` — pak
Settings ukáže „iPhone", ne „Paired device") a `kind`. Chybové stavy: špatný
kód, zamčené párování, nedosažitelný host — tři různé věty, protože každá se
opravuje jinak. Odvolaný token → zpět na tuhle obrazovku s vysvětlením, ne
nekonečné „connecting".

- [ ] **Step 1–4**: implement, `pnpm build`, run.
- [ ] **Step 5: Commit** — `feat(mobile): pair with a code, not a pasted token`

---

### Task 6: git diff (chybějící kus PWA v1)

**Files:**
- Create: `src/mobile/views/DiffView.vue`
- Modify: `src/mobile/App.vue`, `src/mobile/store.ts`

Spec §5 vyjmenovává PWA v1 jako „dashboard stavů · chaty s follow-upy a
permission · spawn nového chatu/agenta · **git diff**". První tři existují,
diff ne. `run_git` je v `remoteAllowed` pod `orchestration:operate`, které
spárované zařízení má.

`git status --porcelain=v1` na seznam a `git diff -- <path>` na obsah, per
workspace path. Read-only view: žádné stage, žádný commit — to je desktopová
práce a spec pro v1 nic z toho nechce.

- [ ] **Step 1–4**: implement, run.
- [ ] **Step 5: Commit** — `feat(mobile): git diff for a workspace`

---

### Task 7: hard cutover — staré `/ws` a `http.token` umírají

**Files:**
- Modify: `src-wails/httpserver.go`, `src-wails/app.go`, `src-wails/remoteapi.go`, `src/components/Settings.vue`
- Test: `src-wails/httpserver_test.go`

Teď — a ne ve fázi 5 — protože **teď existuje klient, který to nepotřebuje**.
Maže se: `handleWS`, `dispatch`, `wsCall`/`wsArgs`, `/rpc/` + `handleRPC`,
`/pair` + `handlePair` + `PairCode`/`RegeneratePairCode`, `loadOrCreateHTTPToken`,
`authorized`, `Broadcast` + `clients` + `installWSSink` + `wsBroadcaster`, a
token z `get_http_server_status`. `http.token` na disku se při startu **smaže**
(spec §4: staré credentials se na nové nemapují, sdílený token bez scopes nelze
poctivě přeložit na scoped per-device session).

Zůstává: `/healthz`, `handleAssets` (to je ten `go:embed` PWA bundle) a `/v2/*`.

`EventSink` po smazání `installWSSink` má **jediného** odběratele (`remotews.go`).
Bus tím neztrácí smysl — je to dál jediné dveře — ale komentář o „dvou sincích"
v `bus.go` a v CLAUDE.md přestane být pravda.

- [ ] **Step 1: Write the failing test** — `/ws` a `/rpc/x` vracejí 404;
  `/healthz` a `/v2/ws` dál žijí; `http.token` po startu na disku není.
- [ ] **Step 2–4**: implement, run `just check`.
- [ ] **Step 5: Commit** — `refactor(remote): delete the v1 tailnet surface and http.token`

---

### Task 8: dokumentace

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1** — telefon jede na `/v2/ws` se sdíleným transportem; `invoke`
  vybírá endpoint podle kontextu a to je celý rozdíl mezi klienty; tečky na
  mobilu jsou fáze + per-device `seenAt`; první paint je snapshot a výpadek je
  resume; co bylo smazáno a proč teprve teď; že `EventSink` má jediného
  odběratele; a **co je a není manuálně vyzkoušené**.

- [ ] **Step 2: Commit**

---

## Self-review

**Spec coverage (§5, §6):**

| spec | task |
|---|---|
| `src/mobile` na sdíleném transportu | 1, 2 |
| `api.ts` smazané | 2 |
| `httpserver.go`'s v1 surface smazané | 7 |
| `http.token` umírá, hard cutover | 7 |
| PWA v1: dashboard stavů | 3, 4 |
| PWA v1: chaty s follow-upy a permission | už funguje, přenesené na nový transport v 2 |
| PWA v1: spawn nového chatu/agenta | už funguje (`remote_create_chat`), přenesené v 2 |
| PWA v1: git diff | 6 |
| `TerminalView` na sdíleném transportu | 2 |

**Vědomě mimo:** `store.ts` a přechod na desktopové Pinia stores (Global
Constraints — refaktor bez přírůstku schopností, a mobilní views jsou napsané
proti jinému tvaru). `remote.go` (Global Constraints). `src/machines/agentStatus.ts`
a `src/lib/terminalStatus.ts` — spec §5 je chce smazat, ale drží je **desktopové**
chaty, ne mobil; je to práce fáze 7 nebo samostatná. `ScopeRemote` v
`internal/control` — po Tasku 7 je bez odběratele, ale mazání verbového scope je
změna v jiném subsystému. Web Push (fáze 7, mimo v1 podle výběru uživatele).

**Type consistency:** `ShellSnapshot` JSON (fáze 4) ↔ `ShellSnapshotData`
(`src/runtime/shellSnapshot.ts`) ↔ mobilní `WorkspaceGroup`/`Tab` mapování
v Tasku 4. `Phase` (`displayStatus.ts`) ↔ `phase-pty:{id}` payload. `PairStatus`
(fáze 5) se telefonu netýká — ten čte jen `/v2/pair`'s odpověď.

**Placeholders:** žádné. Task 1 nese poznámku, proč credentials nejdou do
`@/lib/config` (kruh: config čte přes `invoke`, `invoke` potřebuje credentials).

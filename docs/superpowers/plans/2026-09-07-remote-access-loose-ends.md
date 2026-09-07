# Remote access — Implementation Plan, zbytky po fázích 1–6

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Zavřít to, co po fázích 1–6 zůstalo otevřené, a hlavně **poprvé ten kód proklikat rukou** — protože každá chyba, kterou v něm někdo našel, byla nalezena používáním, ne testem.

**Není to feature plán.** Je to seznam dluhů, každý s vlastním důkazem z kódu.

**Předchozí fáze:** `docs/superpowers/plans/2026-09-05-remote-access-phase-1-2.md` → `2026-09-07-remote-access-phase-6.md` (všechny hotové, `d87933b` → `3aadb04`)

## Než začneš — kontext, který není v kódu

- **Branch: `feat/remote-access-phase-1-2`, worktree `.worktrees/remote-access-phase-1-2`, NEPUSHNUTO** (83+ commitů, branch nemá upstream). Všechno níž stojí na commitech, které nikde jinde nejsou.
- **Paralelně běží druhý agent** na `docs/superpowers/plans/2026-09-07-chat-transcript-phase-7.md` a je uvnitř `chatstream.go` / `chatstore.go` / `chatfold.go`. **Task 4 níž sahá do `chatstream.go`** — viz jeho varování, nedělej ho současně.
- Ledger si veď v `.superpowers/sdd/2026-09-07-remote-access-loose-ends/progress.md`.
- Go: `cd src-wails && go test ./...` + `go test -race ./...`. Frontend: `pnpm test`, `pnpm build`, `pnpm build:mobile`. Vše: `just check`.

---

### Task 1: proklikat fáze 1–6 rukou

**Files:** žádné. Výstupem je ledger a případné nové tasky.

**Tohle je nejdůležitější task v tomhle dokumentu** a jediný, který nemůže udělat agent sám.

Fáze 1–6 mají zelený `just check` (211 testů / 28 souborů) a **nula manuálních ověření**. Za dvě minuty prvního reálného použití z toho vypadly dvě skutečné chyby, ani jedna nepadala v testech:

- chat vytvořený z telefonu byl v sidebaru neviditelný — filtr, který nerozeznal nový chat od opuštěného (`42111d2`)
- `PhaseStore` vůbec nenaběhl, `no such column: id`, takže tečky stavu byly mrtvé na obou klientech (`e73b1dc`)

To není smůla, to je základní rychlost hledání chyb používáním versus testem. Dokud tohle neproběhne, „hotovo" u fází 1–6 znamená „zacommitované a zelené", ne „funguje".

**Postup.** Postav a spusť (`pnpm build:mobile && just build`, pak `open src-wails/build/bin/Burrow.app`) nebo `just dev`. Ke každému bodu si do ledgeru zapiš **co jsi viděl**, ne „ok" — pozorování je celá hodnota tohohle tasku.

- [ ] **Párování.** Settings → Remote access → zapnout. Kód má TTL 3 min. Na telefonu `https://<magicdns>/burrow/`, zadat kód. Očekávej: telefon se připojí, a v Settings **přibude řádek zařízení** se jménem („iPhone") a „last seen".
- [ ] **Špatný kód.** Zadej 000000. Očekávej: zpráva o špatném/expirovaném kódu, **ne** „nedostupné" — ty dvě věci se opravují jinak a rozlišují se podle typu chyby, ne textu.
- [ ] **Lockout.** 5× špatně. Očekávej: Settings hlásí zamčeno a nabízí nový kód; správný kód už neprojde, dokud negeneruješ nový.
- [ ] **Expirace kódu.** Nech kód ležet >3 min. Očekávej: Settings ho **nezobrazuje** (ne mrtvé číslice).
- [ ] **Revoke.** Settings → Revoke u telefonu. Očekávej: telefon **okamžitě** spadne na párovací obrazovku, ne nekonečné „připojuji".
- [ ] **Funnel fail-closed.** `tailscale funnel 443 on`, pak zkus zapnout remote access. Očekávej: **odmítne** a řekne proč (`tailscale funnel off`). Pak zpátky vypnout.
- [ ] **Snapshot / první paint.** Na telefonu otevři dashboard. Očekávej: workspaces i taby **všech** workspaců, ne jen mountnutého; jeden round trip, ne postupné dosypávání.
- [ ] **Fáze a tečky.** Spusť agenta v terminálu. Očekávej: tečka se hýbe na desktopu **i** na telefonu. Nech turn dojet bez koukání → **review** tečka; otevři tab → zhasne. Na druhém klientovi zůstane review, dokud tam nekoukneš (per-device receipt).
- [ ] **ESC uprostřed turnu.** Očekávej: tečka se usadí na idle **bez** review badge (zrušený turn nemá receipt).
- [ ] **Zabití socketu uprostřed turnu.** Vypni/zapni Wi-Fi na telefonu během streamu. Očekávej: „Odpojeno" → „Obnovuji spojení" → dojede a **tečky doskočí**, ne zmrznou. Terminálový scrollback v mezeře **chybět bude** — to je vědomé (`pty-data` není v ringu).
- [ ] **Backpressure.** `cat` velkého souboru v terminálu, s otevřeným telefonem. Očekávej: appka nezamrzne; nejhorší přijatelný výsledek je drop klienta a reconnect.
- [ ] **`burrow spawn` round trip.** Z agenta `burrow spawn ...`. Očekávej: nový tab se otevře a verb vrátí `pty_id`.
- [ ] **PWA install.** Na iOS „Add to Home Screen". Očekávej: jde to (MagicDNS je secure context) a appka se po otevření sama připojí uloženým tokenem.
- [ ] **Step: zapsat výsledky** do ledgeru a **z každého nálezu udělat task** — buď sem, nebo do vlastního plánu, podle velikosti.

---

### Task 2: `workspaces-changed` nikdo neemituje

**Files:**
- Modify: `src-wails/workspace.go`
- Test: `src-wails/workspace_test.go`

**Důkaz.** `emitWorkspacesChanged()` existuje v `events.go:8` a **nemá jediného volajícího**:

```
$ grep -rn "emitWorkspacesChanged" src-wails/*.go | grep -v _test
src-wails/bus.go:66:   // that forgot it (emitWorkspacesChanged) meant the mobile client never learned
src-wails/events.go:8: func emitWorkspacesChanged() { busEmit("workspaces-changed", nil) }
```

Ta zmínka v `bus.go` je komentář o tom, jak `emitAll` kdysi tuhle chybu způsobil — a chyba je pořád tady, jen jinak: funkce zůstala, volání ne.

**Následek.** Vytvoření, přejmenování, smazání, ikona ani přeuspořádání workspace **neoznámí druhému klientovi nic**. Telefon má zastaralý seznam až do reconnectu nebo `resync`. Je to přesně ta samá třída chyby jako neviditelný chat z telefonu, jen o jednu úroveň výš, a `shell_snapshot` ji maskuje tím, že při každém připojení vyrobí správný obrázek.

Mutace, které mají emitovat (`workspace.go`): `CreateWorkspace`, `DeleteWorkspace`, `RenameWorkspace`, `SetWorkspaceIcon`, `SetWorkspaceOrder`. **`TouchWorkspace` ne** — je to `last_opened` timestamp, ne změna seznamu, a emit na něj by tekl při každém přepnutí.

- [ ] **Step 1: Write the failing test** — každá z těch pěti metod emitne `workspaces-changed` právě jednou; `TouchWorkspace` neemitne nic. Vzor: `bus_test.go`'s sink.
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Implement** — emit **po** úspěšném commitu, nikdy před ním; chybová cesta neemituje.
- [ ] **Step 4: Zkontroluj konzumenty.** `App.vue:473` na `workspaces-changed` už poslouchá; mobilní store ne — přidej mu reload (nebo `refresh()`), jinak polovina opravy nic nedělá.
- [ ] **Step 5: `just check`**
- [ ] **Step 6: Commit** — `fix(workspaces): emit workspaces-changed, which nothing ever did`

---

### Task 3: `lsp_start` posílá jiné argumenty, než tabulka jmenuje

**Files:**
- Modify: `src/lib/lsp.ts` **nebo** `src-wails/remoteapi.go` (rozhodni v tasku)
- Test: `src/lib/wailsCompat/commandSurface.test.ts` (odstranit výjimku)

**Důkaz.** Tabulka jmenuje `["id","command","args","cwd"]` (`remoteapi.go:369`), call site posílá `{id, name, args, rootPath}` (`src/lib/lsp.ts:131`). `callApp` plní **jen** argumenty, které tabulka jmenuje, takže `command` a `cwd` dorazí jako prázdný string → **LSP server se na téhle branchi nikdy nespustil.**

Není to díra v testu: `commandSurface.test.ts:349` to hlásí a je to tam vedené jako

```
lsp_start: "KNOWN BUG: sends name/rootPath, table names command/cwd",
```

s poznámkou, že oprava je změna chování LSP cesty a byla mimo rozsah transportní práce. Tenhle task ten odklad platí.

**Rozhodnutí, které task musí udělat:** je `name` totéž co `command`? `LspStart(id, command, args, cwd)` chce spustitelný soubor a pracovní adresář; `server.name` je nejspíš jméno presetu, ne binárka. Tedy **nepřejmenovávej to naslepo** — dohledej, co `server` v `lsp.ts` je, a přenes to, co ta Go metoda skutečně potřebuje.

- [ ] **Step 1: Write the failing test** — smaž výjimku z `commandSurface.test.ts` a nech test padnout, to je červená.
- [ ] **Step 2** — dohledej `server` shape v `lsp.ts` a `LspStart` v `lsp.go`; zapiš do ledgeru, co je čím.
- [ ] **Step 3: Implement** správné mapování.
- [ ] **Step 4** — ověř rukou, že se server **opravdu spustí** (jinak jsi jen srovnal jména). Bez toho task není hotový.
- [ ] **Step 5: Commit** — `fix(lsp): pass the arguments the command table actually names`

---

### Task 4: `chatStreamHardKeep` je strop NAD ochranou foldu

> **KONFLIKT:** `chatstream.go` má v ruce agent od fáze 7. Nedělej tenhle task, dokud nedoběhne — nebo ho nech udělat jemu, protože po jeho Tasku 2 se fold děje synchronně a část argumentu se mění.

**Files:**
- Modify: `src-wails/chatstream.go`
- Test: `src-wails/chatstream_test.go`

**Důkaz** (`chatstream.go:187-194`):

```go
cutoff := latestOrd - chatStreamKeep              // 20 000
if folded := w.foldedOrd(chatID) - 1; folded < cutoff {
    cutoff = folded                                // ochrana: nemaž nesložené
}
if hard := latestOrd - chatStreamHardKeep; hard > cutoff {
    cutoff = hard                                  // ...kterou tohle přebije
}
```

Komentář nad `trim` říká *„an unfolded line is the only copy of that part of the transcript, so age alone must never delete it"* — a `chatStreamHardKeep` (200 000) to právě dělá: zvedne cutoff **nad** hranici složeného. Je to strop proti neomezenému růstu, ale je napsaný tak, že vyhraje nad tou jedinou ochranou, kterou tam ta funkce má.

Reálně to potřebuje 200 000 řádků v jednom chatu, takže je to daleko — ale je to napsané naopak, než jak komentář slibuje.

- [ ] **Step 1: Write the failing test** — chat s `folded_ord` daleko za sebou a >200 000 řádky: `trim` **nesmí** smazat nic nesloženého.
- [ ] **Step 2–3** — rozhodni a implementuj: buď hard cap **nikdy** nesmí sáhnout na nesložené (a neomezený růst se řeší jinde), nebo smí, ale pak to musí být **hlasité** (log, který pojmenuje, co se zahazuje) a komentář nad `trim` se musí přepsat, aby nelhal.
- [ ] **Step 4: Commit** — `fix(chatstream): stop the hard cap from deleting unfolded lines`

---

### Task 5: rozhodnout o integraci branche

**Files:** žádné.

83+ commitů, `main` nedotčený, branch bez upstreamu. **Až po Tasku 1** — mergovat neproklikaný kód je to, co tenhle plán zpochybňuje.

Použij `superpowers:finishing-a-development-branch`.

- [ ] **Step 1** — nabídnout uživateli: merge lokálně / push + PR / nechat.
- [ ] **Step 2** — provést jeho volbu. Pozor: na `main` má uživatel **nezacommitovanou práci** (extensions/SDK); nic tam nepřepiš.

---

## Vědomě mimo tenhle plán

| věc | proč |
|---|---|
| **Web Push** (spec remote access §6, fáze 7) | Uživatel ji sám vyřadil z v1 scope. Vlastní práce, vlastní dokument. Pozor: „fáze 7" jsou dvě různé věci — tahle a `2026-09-07-chat-transcript-phase-7.md`. |
| **Binární PTY framy** (spec §2) | Optimalizace šířky pásma, ne prerekvizita. `pty-data` jede jako JSON a funguje. |
| **`fs.go` path guard, per-connection filtrování eventů, admission check na exec** | `remoteapi.go`'s LOAD-BEARING NOTE je vede jako to, co by chtěla **skutečně omezená** role zařízení. Fáze 5 rozhodla, že spárované zařízení je vlastní telefon uživatele a autoritu nad strojem má záměrně. Tenhle seznam je prerekvizita, **až** kdyby se mělo pustit dovnitř zařízení, které není uživatelovo. |
| **`src/machines/agentStatus.ts`, `src/lib/terminalStatus.ts`** | Spec §5 je chce smazat, ale drží je **desktopové chaty**, ne mobil. Až fáze 7 přesune fold do Go, uvidí se, co z nich zbyde. |
| **`store.ts` → desktopové Pinia stores** | Vědomá deviace fáze 6: refaktor bez přírůstku schopností. |

## Self-review

**Pořadí není libovolné.** Task 1 je první, protože může přidat další tasky a protože Task 5 na něm závisí. Tasky 2–4 jsou navzájem nezávislé a malé.

**Každý task nese důkaz z kódu**, ne domněnku: `grep` výstup u Tasku 2, čísla řádků u 3 a 4, dva commity jako precedens u Tasku 1.

**Task 3 má past** a plán ji pojmenovává: srovnat jména argumentů je snadné a **nestačí** — `name` skoro jistě není `command`. Task není hotový bez ručního ověření, že se server spustí.

**Placeholders:** žádné.

# Remote access — Implementation Plan, zbytky po fázích 1–6

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Zavřít tři konkrétní defekty, které po fázích 1–6 zůstaly otevřené. Každý je ověřený v kódu, ne odhadnutý.

**Není to feature plán.** Je to seznam dluhů. Tasky jsou navzájem nezávislé — dají se dělat v jakémkoli pořadí a klidně paralelně, s jednou výjimkou (Task 3, viz jeho varování o konfliktu).

**Předchozí fáze:** `docs/superpowers/plans/2026-09-05-remote-access-phase-1-2.md` → `2026-09-07-remote-access-phase-6.md` (všechny hotové, `d87933b` → `3aadb04`)

## Než začneš — kontext, který není v kódu

- **Branch: `feat/remote-access-phase-1-2`, worktree `.worktrees/remote-access-phase-1-2`, NEPUSHNUTO** (83+ commitů, branch nemá upstream). Všechno níž stojí na commitech, které nikde jinde nejsou; ve fresh worktree z `main` to nedává smysl.
- **Paralelně běží druhý agent** na `docs/superpowers/plans/2026-09-07-chat-transcript-phase-7.md` a je uvnitř `chatstream.go` / `chatstore.go` / `chattranscript.go` / `chatfold.go`. **Task 3 níž sahá do `chatstream.go`** — viz jeho varování.
- Ledger si veď v `.superpowers/sdd/2026-09-07-remote-access-loose-ends/progress.md`.
- Go: `cd src-wails && go test ./...` + `go test -race ./...`. Frontend: `pnpm test`, `pnpm build`, `pnpm build:mobile`. Vše: `just check`.
- **Manuální ověření fází 1–6 sem nepatří** a je pro člověka:
  `docs/superpowers/2026-09-07-manual-verification-remote-access.md`. Task 2 níž
  na něj předává jeden bod, který jinak nelze uzavřít.

---

## File Structure

| soubor | odpovědnost |
|---|---|
| `src-wails/workspace.go` (modify) | Task 1: emitovat `workspaces-changed` |
| `src-wails/workspace_test.go` (modify/nový) | Task 1: každá mutace emitne právě jednou |
| `src/mobile/store.ts` (modify) | Task 1: konzumovat ten event |
| `src/lib/lsp.ts` **nebo** `src-wails/remoteapi.go` (modify) | Task 2: srovnat argumenty |
| `src/lib/wailsCompat/commandSurface.test.ts` (modify) | Task 2: odstranit výjimku |
| `src-wails/chatstream.go` (modify) | Task 3: hard cap nesmí mazat nesložené |
| `src-wails/chatstream_test.go` (modify) | Task 3 |

---

### Task 1: `workspaces-changed` nikdo neemituje

**Files:**
- Modify: `src-wails/workspace.go`, `src/mobile/store.ts`
- Test: `src-wails/workspace_test.go`

**Interfaces:**
- Produces: nic nového; zapojuje existující `emitWorkspacesChanged()`

**Důkaz.** `emitWorkspacesChanged()` existuje v `events.go:8` a **nemá jediného volajícího**:

```
$ grep -rn "emitWorkspacesChanged" src-wails/*.go | grep -v _test
src-wails/bus.go:66:   // that forgot it (emitWorkspacesChanged) meant the mobile client never learned
src-wails/events.go:8: func emitWorkspacesChanged() { busEmit("workspaces-changed", nil) }
```

Ta zmínka v `bus.go` je komentář o tom, jak `emitAll` tuhle chybu kdysi způsobil — a chyba je pořád tady, jen jinak: funkce zůstala, volání ne.

**Následek.** Vytvoření, přejmenování, smazání, ikona ani přeuspořádání workspace **neoznámí druhému klientovi nic**. Telefon má zastaralý seznam až do reconnectu nebo `resync`. Je to stejná třída chyby jako neviditelný chat z telefonu (`42111d2`), jen o úroveň výš, a `shell_snapshot` ji maskuje tím, že při každém *připojení* vyrobí správný obrázek — takže se to projeví jen u klienta, který už připojený je.

Mutace, které mají emitovat (`workspace.go`): `CreateWorkspace`, `DeleteWorkspace`, `RenameWorkspace`, `SetWorkspaceIcon`, `SetWorkspaceOrder`.

**`TouchWorkspace` NE** — je to `last_opened` timestamp, ne změna seznamu, a emit na něj by tekl při každém přepnutí workspace.

- [ ] **Step 1: Write the failing test**

Do `src-wails/workspace_test.go`. Vzor odběratele je `bus_test.go` (`busSubscribe(func(ev shellEvent) { ... })` + `t.Cleanup(busReset)`).

```go
func TestWorkspaceMutationsEmitWorkspacesChanged(t *testing.T) {
	// Every mutation of the LIST notifies; a client that is already connected
	// has no other way to learn (shell_snapshot only runs on connect).
	// Exactly once, not twice: a doubled emit is a doubled reload on the phone.
}

func TestTouchWorkspaceDoesNotEmit(t *testing.T) {
	// last_opened is not a change to the list, and it fires on every
	// workspace switch — emitting there would flood both clients.
}

func TestAFailedMutationDoesNotEmit(t *testing.T) {
	// e.g. rename of an id that does not exist. The event claims the list
	// changed; if it did not, every client does a pointless round trip and
	// learns nothing.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./... -run 'TestWorkspaceMutations|TestTouchWorkspace|TestAFailedMutation'`
Expected: FAIL — nic neemituje

- [ ] **Step 3: Implement**

Emit **po** úspěšném zápisu, nikdy před ním; chybová cesta neemituje.

- [ ] **Step 4: Zapojit konzumenta na mobilu**

`App.vue:473` na `workspaces-changed` už poslouchá. **Mobilní store ne** — přidej mu odběr, který zavolá `refresh()`. Bez toho je polovina opravy k ničemu: server bude křičet do prázdna.

Pozor na echo: `refresh()` je celý snapshot, takže tady **nedělej** `selfWrites`-style filtr jako u chatů — telefon workspaces nemění, jen je čte.

- [ ] **Step 5: Run `just check`**

- [ ] **Step 6: Commit**

```bash
git commit -m "fix(workspaces): emit workspaces-changed, which nothing ever did"
```

---

### Task 2: `lsp_start` posílá jiné argumenty, než tabulka jmenuje

**Files:**
- Modify: `src/lib/lsp.ts` **nebo** `src-wails/remoteapi.go` (rozhodni v tasku)
- Test: `src/lib/wailsCompat/commandSurface.test.ts`

**Důkaz.** Tabulka jmenuje `["id","command","args","cwd"]` (`remoteapi.go:369`), call site posílá `{id, name, args, rootPath}` (`src/lib/lsp.ts:131`). `callApp` plní **jen** argumenty, které tabulka jmenuje, takže `command` a `cwd` dorazí jako prázdný string → **LSP server se na téhle branchi nikdy nespustil.**

Není to díra v testu. `commandSurface.test.ts:349` to hlásí a je to tam vedené jako:

```ts
lsp_start: "KNOWN BUG: sends name/rootPath, table names command/cwd",
```

s poznámkou, že oprava je změna chování LSP cesty a byla mimo rozsah transportní práce. Tenhle task ten odklad platí.

**Past, kterou task musí obejít.** Je `name` totéž co `command`? `LspStart(id, command, args, cwd)` chce **spustitelný soubor** a **pracovní adresář**; `server.name` je skoro jistě jméno presetu, ne binárka. **Nepřejmenovávej to naslepo** — srovnat jména je snadné a nestačí. Dohledej, co `server` v `lsp.ts` skutečně je a odkud se bere, a přenes to, co ta Go metoda potřebuje.

- [ ] **Step 1: Write the failing test**

Smaž řádek `lsp_start:` z výjimek v `commandSurface.test.ts` a nech test padnout — to je červená, a je to zároveň jediná automatická kontrola, kterou na tohle máme.

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- commandSurface`
Expected: FAIL — `lsp_start` posílá argumenty, které tabulka nejmenuje

- [ ] **Step 3: Zjistit, co je čím**

Přečti `server` shape v `src/lib/lsp.ts` a `LspStart` v `src-wails/lsp.go`. Do ledgeru zapiš: co je `command`, co je `cwd`, a odkud je call site má vzít. **Bez tohohle kroku je Step 4 hádání.**

- [ ] **Step 4: Implement** správné mapování a nech `commandSurface` zezelenat.

- [ ] **Step 5: Run `just check` + `pnpm build`**

- [ ] **Step 6: Commit**

```bash
git commit -m "fix(lsp): pass the arguments the command table actually names"
```

- [ ] **Step 7: Předat ruční ověření**

Že se server **opravdu spustí**, agent ověřit nemůže. Zapiš do ledgeru, že to
zbývá, a je to už zapsané jako bod v
`docs/superpowers/2026-09-07-manual-verification-remote-access.md`. Task je
hotový bez toho — ale **neříkej, že LSP funguje**, dokud to někdo neviděl.

---

### Task 3: `chatStreamHardKeep` je strop NAD ochranou foldu

> **KONFLIKT — přečti první.** `chatstream.go` má v ruce agent od fáze 7
> (`docs/superpowers/plans/2026-09-07-chat-transcript-phase-7.md`, právě
> commituje do `chatstream.go`/`chattranscript.go`). **Nedělej tenhle task
> současně s ním.** Buď počkej, až doběhne, nebo ho nech udělat jemu — po jeho
> Tasku 2 se fold děje synchronně s emitem a část argumentu níž se mění.
> Ověř `git log` před začátkem.

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

Komentář nad `trim` říká: *„an unfolded line is the only copy of that part of the transcript, so age alone must never delete it"* — a `chatStreamHardKeep` (200 000) přesně to dělá: zvedne cutoff **nad** hranici složeného. Je to strop proti neomezenému růstu, ale je napsaný tak, že vyhraje nad tou jedinou ochranou, kterou ta funkce má.

Reálně to potřebuje 200 000 řádků v jednom chatu, takže je to daleko. Ale je to napsané naopak, než jak komentář slibuje, a komentář je to, podle čeho se příště někdo rozhodne.

- [ ] **Step 1: Write the failing test**

```go
func TestHardCapDoesNotDeleteUnfoldedLines(t *testing.T) {
	// A chat with folded_ord far behind and more than chatStreamHardKeep
	// lines. The hard cap is a bound on growth, not a licence to delete the
	// only copy of a transcript nobody has folded yet.
}
```

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Rozhodnout a implementovat**

Dvě legitimní odpovědi, vyber jednu a napiš proč do ledgeru:

- **hard cap nikdy nesáhne na nesložené** — neomezený růst se pak řeší jinde (a je potřeba říct kde), nebo
- **smí, ale hlasitě** — log, který pojmenuje chat a kolik řádků zahazuje, **a přepsat komentář nad `trim`**, aby nelhal.

Nenechávej kód a komentář v rozporu, ať vybereš cokoli.

- [ ] **Step 4: Run `cd src-wails && go test ./... && go test -race ./...`**

- [ ] **Step 5: Commit**

```bash
git commit -m "fix(chatstream): stop the hard cap from deleting unfolded lines"
```

---

## Vědomě mimo tenhle plán

| věc | proč |
|---|---|
| **Manuální ověření fází 1–6** | Agent nespustí GUI. `docs/superpowers/2026-09-07-manual-verification-remote-access.md`. |
| **Integrace branche** (merge / push + PR) | Rozhodnutí uživatele, a má být **až po** tom ověření. `superpowers:finishing-a-development-branch`. |
| **Web Push** (spec remote access §6, fáze 7) | Uživatel ji sám vyřadil z v1 scope. Pozor: „fáze 7" jmenuje **dva různé dokumenty** — tuhle a `2026-09-07-chat-transcript-phase-7.md`. |
| **Binární PTY framy** (spec §2) | Optimalizace šířky pásma, ne prerekvizita. `pty-data` jede jako JSON a funguje. |
| **`fs.go` path guard, per-connection filtrování eventů, admission check na exec** | `remoteapi.go`'s LOAD-BEARING NOTE je vede jako to, co by chtěla **skutečně omezená** role zařízení. Fáze 5 rozhodla, že spárované zařízení je vlastní telefon uživatele a autoritu nad strojem má záměrně. Prerekvizita **až** kdyby se mělo pustit dovnitř zařízení, které není uživatelovo. |
| **`src/machines/agentStatus.ts`, `src/lib/terminalStatus.ts`** | Spec §5 je chce smazat, ale drží je **desktopové chaty**, ne mobil. Až fáze 7 přesune fold do Go, uvidí se, co z nich zbyde. |
| **`store.ts` → desktopové Pinia stores** | Vědomá deviace fáze 6: refaktor bez přírůstku schopností. |

## Self-review

**Každý task nese důkaz z kódu**, ne domněnku: `grep` výstup u Tasku 1, čísla řádků a citace výjimky u Tasku 2, citovaný úryvek `trim` u Tasku 3.

**Tasky jsou nezávislé** a mohou jít paralelně — kromě Tasku 3, který má konflikt s běžícím agentem od fáze 7 a nese o tom varování jako první věc.

**Task 1 má poloviční past:** emitovat na Go straně a nezapojit mobilního konzumenta znamená opravu, která nic nedělá. Step 4 to hlídá.

**Task 2 má past** a plán ji pojmenovává: srovnat jména argumentů je snadné a **nestačí**. Step 3 existuje proto, aby Step 4 nebyl hádání, a Step 7 přiznává, co agent uzavřít nemůže.

**Task 3 nemá jednu správnou odpověď** — plán nabízí dvě legitimní a žádá zápis rozhodnutí do ledgeru, místo aby předstíral, že to je zjevné.

**Placeholders:** žádné.

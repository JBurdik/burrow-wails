# Co převzít z stablyai/orca

Zdroj: `github.com/stablyai/orca` (MIT, Electron + React). Prošlý stav k 2026-09-22.
Většina jejich feature setu Burrow už má (worktrees, mobil, control CLI, fázové notifikace,
PR panel, scrollback přes restart). Tenhle dokument je jen o tom, co **nemá**.

---

## Závazné pravidlo pro všechno níž: ChatUI je první občan

Orca je **terminál-centrická**. Worker je PTY, adresuje se `--terminal <handle>`, výsledek se
tahá ze scrollbacku. Burrow má vedle terminálů plnohodnotné chaty (`chats.go`, `parent_chat_id`,
`chat_send`, `spawn --target chat`) a ty jsou pro strukturovanou práci lepší — mají durable
transcript, neutrální eventy a permission gate.

**Žádná z těchhle featur se nesmí navrhnout tak, že funguje jen s terminálem.** Konkrétně:

- Cokoli, co adresuje agenta, používá **klíč `chat:<id>` nebo `pty:<id>`** — přesně ten
  jeden typ, který už dnes drží `phasestore.go`. Ne handle terminálu.
- Cokoli, co čte výstup agenta, čte z `chat_stream` (chat) **nebo** ze scrollbacku (tab).
  Ne ze scrollbacku vždycky.
- Cokoli, co agentovi něco posílá, jde přes `chat_send` **nebo** `send_to_tab`.
- Nová funkce, která umí jen jedno z toho, je nedodělaná. Terminál není fallback ChatUI
  ani naopak.

Verb registry to už takhle umí (`spawn` má `target: tab | chat`) — držet se toho.

---

## 1. Review loop — z one-shot na batch

### Stav v Burrow
`DiffFeedbackComposer.vue` (50 ř.) + `DiffTab.vue` už umí: vyber řádky diffu, napiš komentář,
`Send to agent`. Je to **jednorázové** — jeden výběr, jeden komentář, hned odchází.

### Co má orca navíc
`src/main/folder-workspace-diff-comments.ts` — komentáře jsou **perzistentní entita per workspace**.
Reviewer projde celý diff, nasype 8 poznámek na různé soubory a řádky, teprve pak je pošle
jako jednu zprávu. To je způsob, jak lidi doopravdy review dělají; Burrowův one-shot nutí
posílat agentovi osm zpráv a rozbíjí mu kontext.

### Návrh
- Nová tabulka v `db.go`: `diff_comments(id, ws_id, file, line, side, body, created_at, sent_at)`.
  SQLite, ne localStorage — přežije restart a čte se i z mobilu přes existující `/v2/ws`.
- `DiffTab.vue`: výběr řádků → `Add note` (místo `Send`). Poznámky inline v diffu jako gutter
  marker + rozbalitelný blok.
- Jedno tlačítko `Send N notes` → složí markdown (`file:line` + citovaný hunk + text).
- **Cíl odeslání je chat i tab.** Výchozí je chat workspace, pokud nějaký je — tam má review
  smysl, protože agent odpoví strukturovaně. Fallback na aktivní tab.
- `sent_at` místo mazání: po odeslání poznámky zešednou, ale zůstanou — je vidět, co agent dostal.

**Nedělat**: threading, resolve/unresolve, komentáře na commity, sync do GitHub review.

**Kontrola**: go test na složení markdownu z N poznámek ve dvou souborech.

---

## 2. Usage a cena

### Co má orca
Dva oddělené subsystémy, které stojí za to nezaměnit:

**a) `src/main/claude-usage/`** — historická spotřeba. `scanner.ts` prochází transcript JSONL,
`claude-model-pricing.ts` počítá cenu (rozlišuje cache-write 5min vs 1h TTL a long-context tier
nad 200k tokenů), `worktree-attribution.ts` přiřazuje spotřebu ke konkrétnímu worktree.

**b) `src/main/rate-limits/`** — *živé* okno a čas resetu. Přes 100 souborů, osm providerů.
Tahá to z OAuth credentials, a když to nejde, `claude-pty-usage-parser.ts` spustí **skrytou PTY**,
pošle do ní `/usage` a parsuje výstup. K tomu `claude-accounts/` s keychainem a hot-swapem účtů.

### Co z toho vzít
Jen **(a)**. Bod (b) je ledovec: skrytá PTY, keychain, OAuth refresh, osm providerů — a hot-swap
účtů řeší problém, který Burrow nemá.

### Klíčový rozdíl proti orce: chaty už data mají
`providerruntime.go` emituje `context.usage` (`EvtContextUsage`) s `input_tokens`,
`output_tokens`, `cache_read_input_tokens` a `cache_creation_input_tokens`, a celý stream se
ukládá do `chat_stream`. **Pro chaty se tedy nic neparsuje** — jen se agreguje, co už teče.
Orca tohle nemůže, protože nemá neutrální provider vrstvu; musí číst cizí JSONL.

Takže dvě různě drahé poloviny:

- **Chaty (levné, dělat první):** agregovat `context.usage` per chat. Zdroj pravdy je
  `chat_stream`, takže to jde dopočítat i zpětně a přežije to restart.
- **Terminály (dražší, až potom):** tam Burrow do agenta nevidí, takže se nedá vyhnout
  procházení `~/.claude/projects/**/*.jsonl` orcovým způsobem. Atribuce přes `cwd` v transcriptu
  → `workspaces.path`; worktree zdědí kořen šplháním po `parent_id`, jak to už dělá
  `forge_provider`. Cache: soubory jsou append-only, stačí offset + mtime.

### Ceník
Jedna mapa konstant v Go. Orcův `claude-model-pricing.ts` je dobrá reference na **hranice verzí** —
jejich testy ukazují past: `claude-opus-4.9` ≠ `claude-opus-4.1`, `claude-sonnet-50` není
`sonnet-5`. Match musí respektovat hranici verze, ne prefix.

**Nedělat**: živé rate-limit okno, přepínání účtů, keychain, ostatní providery, grafy.

**Kontrola**: go test na `estimateCost` s hraničními jmény modelů (převzít jejich případy)
a jeden na inkrementální scan (druhý průchod nesmí dvakrát započítat stejné řádky).

---

## 3. Verzovaný skill servírovaný binárkou

### Co má orca
`skills/orchestration/SKILL.md` je **záměrně prázdný stub**, který říká jen "tohle není návod".
Plný, verzí sladěný guide vytiskne až `orca skills get orchestration`. Jejich odůvodnění přímo
v souboru: *"kept out of this file on purpose so it can never drift from the binary that will
actually run your commands."* K tomu `--reference references/<file>.md` pro podmíněné kapitoly,
takže agent načítá jen to, co pro daný krok potřebuje, místo celého bloku do kontextu.

### Proč to Burrow chce
`agentdocs.go` dnes instaluje **statický** `agentdocs/skills/burrow/SKILL.md` do `~/.claude/`.
Ten soubor popisuje verby. Verby se mění. Nainstalovaná kopie se nemění, dokud uživatel
neupdatuje appku — a i pak přepisujeme soubor, který si mohl upravit. Klasický drift.

Přitom Burrow už **umí generovat popis verbů z registry** — `managerPrimer.ts` to dělá pro
Manager primer z `control_verbs`. Chybí jen druhý výstup téhož zdroje.

### Návrh
- Nový non-verb subcommand `burrow skills get <name>` (stejná kategorie jako `burrow hook`,
  `burrow status` — plumbing, ne control verb). Tiskne guide složený z živé verb registry.
- `agentdocs.go` instaluje **stub**: co to je, kdy to zapnout, a jedna věta „plný návod dostaneš
  přes `burrow skills get burrow`". Stub se skoro nemění, takže přepis nikoho nebolí.
- `burrow skills get burrow --reference <name>` pro delší kapitoly (orchestration, worktrees),
  ať nelezou do kontextu vždycky.
- Guide musí popisovat **obě cesty** — chat i tab (viz závazné pravidlo nahoře).

**Nedělat**: verzování guidu, cache, vlastní formát. Je to jen `fmt.Fprintf` z registry.

**Kontrola**: go test, že `skills get burrow` vyjmenuje každý verb z registry — tentýž typ
ochrany jako `TestRemoteSurfaceIsExhaustive`.

**Cena**: nejmenší položka v dokumentu, největší poměr hodnota/práce.

---

## 4. Orchestration state machine

### Co má orca
Nejlepší kus celého repa, `skill-guides/orchestration.md` + sedm referencí. Jádro:

- **Tři entity.** *Run* je durable namespace a schránka koordinátora. *Task* je práce.
  *Dispatch* je **jeden autoritativní pokus** o Task. Oddělení Task od Dispatch je to, co dělá
  retry čitelným — druhý pokus je nový Dispatch téhož Tasku, ne nový Task.
- **Autorita plyne z aktivního Dispatche**, ne z titulku terminálu, zkopírovaného ID nebo
  viditelného panelu.
- **`worker_done` právě jednou**, z dispatchnutého terminálu, s explicitním
  `--outcome succeeded|failed`. Jejich pravidlo: *"Never encode failure only in prose."*
  Selhání v próze je přesně to, co Burrowův `collect_results` dnes dělá.
- **Vrstvená liveness**: `live | unverifiable | exited`, a tvrdé pravidlo
  *"absence never authorizes stop, abandon, retry, or release"* — timeout je checkpoint,
  ne selhání. Rozlišují „terminál žije" od „agent v něm žije".
- **Completion accounting**: po přijatém výsledku musí koordinátor udělat právě jedno —
  reuse, `worker-retain`, nebo `worker-release`. Turn nesmí skončit, dokud
  `worker-list --terminal-state reclaimable` vrací řádky.
- **Blokující `ask`/`reply`** — worker se zeptá koordinátora a čeká. Nikdy neotevře lokální
  TUI otázku, na kterou koordinátor nemůže odpovědět.

### Stav v Burrow
`spawn` + `agent_status` + `collect_results` + `wait_result`. Žádný durable stav: neexistuje
entita „pokus", výsledek je text, a nic nehlídá, že na každého spawnutého agenta padlo
rozhodnutí. Manager tak umí ztratit workera a nepozná rozdíl mezi „ještě dělá" a „umřel".

### Návrh — a tady je ChatUI úplně zásadní
Orcův model je postavený na terminálech. Burrow ho musí postavit na **klíči, který má obojí**:

- Nová tabulka `orch_task(id, run_id, spec, status)` a `orch_dispatch(id, task_id, target_key,
  outcome, started_at, settled_at)`, kde **`target_key` je `chat:<id>` nebo `pty:<id>`** —
  tentýž tvar, jaký už používá `phasestore.go`. Žádné terminálové handle.
- **Liveness se nemusí vymýšlet.** Burrow už derivuje fázi v Go a má ji i bez připojeného
  klienta. Mapa je přímá: `running`/`waiting_*` → `live`, `stale` → `unverifiable`,
  PTY pryč z daemona nebo chat ukončený → `exited`. Tohle je důvod, proč je tahle feature
  pro Burrow levnější než pro orcu.
- `worker_done` jako nový verb s povinným `outcome: succeeded|failed`. Pro chat target ho může
  Burrow odvodit i z `turn.completed` / `turn.failed`, což terminál neumí — další bod pro chaty.
- `ask` / `reply` jako verby. Pro chat je odpověď prostý `chat_send`; pro tab `send_to_tab`.
- Manager primer (a tedy i `skills get` z bodu 3) dostane sekci s koordinátorskou smyčkou.

**Nedělat v první verzi**: DAG závislosti, remote placement, depth limity, `worker-abandon`
vs `worker-stop` rozlišení, adopce cizích Runů. Orca má na každé z toho vlastní referenci —
to je zralost, ne startovní bod. Začít s: Run, Task, Dispatch, outcome, release accounting.

**Kontrola**: go test na přechody stavů — dvojitý `worker_done` musí být odmítnut, a
`unverifiable` nesmí povolit `release`.

---

## 5. Design Mode — a proč to u nás vypadá jinak

### Co má orca
Klikneš na element ve stránce a agentovi se pošle jeho HTML, CSS a oříznutý screenshot.
Pro frontend práci největší diferenciátor v celé appce.

### Proč to Burrow nemůže zkopírovat rovnou
Burrow **browser má** (`BrowserPane.vue`, RP surface „Browser"), ale je to `<iframe>` uvnitř
Wails WKWebView. Orca má Electron `<webview>` s `executeJavaScript`, což je něco úplně jiného.
Z cross-origin iframu nepřečteš `contentDocument` ani neuděláš screenshot — `BrowserPane.vue`
si na ř. 101–104 sám detekuje, že mu prohlížeč přístup odepřel. Takže „klikni na element"
přes dnešní pane prostě nejde a přechod na WebKit na tom nic nezmění; není to o enginu,
je to o same-origin policy.

### Cesta, která sedí na to, co Burrow už má
Injektovat skript **do stránky** místo čtení zvenčí. Burrow už provozuje loopback mux
(hook server), takže:

- Browser pane nemíří na `localhost:3000` napřímo, ale na Burrowí proxy na hook portu, která
  HTML odpovědi prostřihne o `<script>`.
- Ten skript je pak **same-origin s pane** → klik na element, `outerHTML`, `getComputedStyle`,
  a `postMessage` ven. Žádné CDP, žádný druhý prohlížeč, žádná nová závislost.
- Výsledek jde do promptu jako text (soubor:selektor + HTML + relevantní CSS).
  **Cíl je chat i tab**, viz pravidlo nahoře.

**Screenshot vynechat z v1.** Je to nejdražší třetina (canvas taint, crop, přenos obrázku do
promptu) a nejmenší část hodnoty — HTML + computed styles + selektor agentovi stačí. Přidat,
až bude jasné, že chybí.

**Riziko**: proxy musí nechat projít websocket (HMR) beze změny, jinak rozbije dev server
uživatele. To je ta věc, co tuhle položku drží až za body 1–4.

---

## Příloha: zásobník

| Věc | Proč | Cena |
|---|---|---|
| **Mark unread** | Ruční „vrátím se k tomu". Burrow má `seenAt`, chybí toggle opačným směrem. Musí fungovat pro chaty i taby. | Malá |
| **Quick open přes worktrees + agenty + příkazy** | Spotlight existuje, jen hledá úzce. | Malá |
| **Fan-out jednoho promptu na N agentů → compare → merge vítěze** | Burrow má spawn i worktree; chybí compare/pick UI. Postavit až na bodu 4 — bez Task/Dispatch nemáš co porovnávat. | Velká |
| **SSH / remote worktrees, ephemeral VM** | Agenti na silném stroji. Celý `ephemeral-vm-*` subsystém. | Velká |

**Zamítnuto:** cloud relay pro mobil (`cloud/`) — Burrow má loopback + tailnet záměrně,
relay je horší security model za lepší UX pro lidi bez tailscale. Computer-use skill.

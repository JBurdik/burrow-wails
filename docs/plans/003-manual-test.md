# Ruční test: fáze 3, 4 a 6 (plán 003)

Tři změny sáhly na render chatu, navigaci a remote klienta a nemá je za sebou
nikdo, kdo by na to koukal. `vue-tsc` + 226 unit testů prošlo; tohle je ta část,
kterou testy nepokryjí.

Pořadí je schválně od nejlevnějšího k nejdražšímu — když spadne §1, nemá cenu
zkoušet §6.

Hlásit lze číslem kroku (např. „spadlo 4c").

## Výsledky prvního průchodu (agent-browser, 2026-09-04)

Prošlo v `wails dev` na `localhost:34115`, s reálnými daty a živým Claude:

- **§1a/§1c** boot na `#/` + composer, konzole bez erroru.
- **§2** dashboard tam a zpět (`#/dashboard` ⇄ `#/ws/2`), `selectTab` ze Sidebaru
  s tabem v URL, zpět/vpřed přes tři taby, `#/ws/999999` i `#/nope/1/2` → `#/`,
  **deep link `#/ws/2` otevřel workspace, aktivoval tab a dorovnal URL na
  `#/ws/2/tab/195`** — celý round trip route ⇄ store.
- **§2g** odeslání z composeru → `#/ws/2`, tab vznikl, agent nastartoval.
- **§3** živý turn: text v jedné bublině, tool karta s `toolOutput`,
  `toolRawName=1`, nic nezůstalo `partial`, `session.title` doplnil jméno
  threadu, **`folded_ord = max(ord)+1`** přesně.
- **§3f/§8e** čistý cyklus `running → done → idle` (auto-clear po 4 s).
- **§4a** nejdůležitější test plánu: turn se **třemi** Bash tooly dojel a složil
  se na disk s **odmountovaným view**, včetně automatického flushe frontované
  zprávy (§3j). `folded_ord` dorovnané.
- Doménové eventy chodí ve správném tvaru — ověřeno probem na
  `chat-event-{id}`: `{ord:49, events:[{type:"text.delta",…}]}`.

Neověřeno tímto průchodem: §5 (ACP/Codex), §6 (permissions), §7 (crash
recovery), §9 (Manager), §10 (mobil) — dotestováno druhým průchodem níž.

### Co to našlo

1. **`props.isWatching` je po unmountu zamrzlé** → turn dokončený za zavřeným
   view se hlásil jako sledovaný a usadil se na transientní `done`, který sám
   zmizí. To je původní bug v nové podobě. Opraveno: `ChatSession.isWatched()`
   (refCount) místo propu, +2 testy.
2. **Startovní navigace předbíhala obnovu tabů** — rozhodnutí padlo jednou v
   `onMounted`, kdy je `tabsByWs` ještě prázdné. Přepsáno na jednorázový watcher
   na první neprázdný seznam (s 10s stropem). *Neprojevilo se to jako regrese* —
   tahle instalace nemá v `terminal_tabs` žádné řádky, takže `#/` je správný
   cíl i podle chování před fází 4.
3. **Pre-existing, opraveno:** `session.status` se persistuje a po startu se
   nesrovnal s actorem — `subscribe()` fajruje až na dalším přechodu, takže
   `running` zapsané při zavření appky uprostřed turnu tam viselo navěky
   (`busy:false` + `running`). `ensureActor` teď stav actoru adoptuje hned.
4. **Pre-existing, opraveno:** float verby (`notify_float_grid`,
   `request_float_snapshot`, `send_float_snapshot`, `register_tmux_win`,
   `set_tab_live_status`) berou pty id jako **string**, ale volající posílá
   číselné leaf id → Wails bridge to odmítal na každém resize terminálu:
   `json: cannot unmarshal number into Go value of type string`. Shim to teď
   stringifikuje. Topic si Go staví stejnou konverzí, takže jména sedí dál.
5. **Pre-existing, opraveno:** tři zapomenuté `[UNREAD-DEBUG]` logy
   (`claudeChats.ts`, `terminalTabs.ts` ×2) — jeden z nich sypal `new Error().stack`
   při každém updatu tabů.
6. **Pre-existing, opraveno:** `get_pty_foreground` neexistoval — chyběl celý
   druhý kanál status dotů. Doplněno: `ptycore.Foreground()` čte `TIOCGPGRP`
   (tcgetpgrp) na master fd, tedy terminálovou představu o foregroundu — přesně
   to, komu by kernel doručil Ctrl-C — a jméno procesu bere z `p_comm` přes
   sysctl, ne forkem `ps` (poll běží každé 2 s na každý terminál). Protaženo
   daemon protokolem (`kind:"foreground"`), protože master fd drží daemon.
   Shell se **hlásí jménem**, když je ve foregroundu — tím se volající dozví,
   že příkaz nebo agent skončil (`SHELL_RE` v `XTerm.vue`).

   **Potřebuje rebuild daemona.** Běžící `burrow-daemon` ten verb nezná a vrátí
   `unknown request kind: foreground`; `GetPtyForeground` to spolkne a vrátí
   prázdno, takže se to degraduje tiše, ale status dots pro ne-agentní příkazy
   nezačnou fungovat, dokud daemon nepojede z nového buildu. V `wails dev` se
   spouští přes `go run ./cmd/burrow-daemon`, takže stačí **zabít starý proces**
   (`pkill -f burrow-daemon`) a nechat ho nastartovat znovu.

## Výsledky druhého průchodu — §5–§10 (agent-browser, 2026-09-04)

Testováno z izolovaného worktree na tipu branche (`/tmp/burrow-mt003`, commit
9074afa), protože v hlavním working tree paralelně pracoval jiný agent a jeho
Go editace restartovaly backend uprostřed turnu — což samo vyrobilo jeden falešný
nález (Codex approval, který umřel na restart adapteru, ne na bug).

- **§5 ACP/Codex:** 5a–5d, 5f–5h prošlo. Text, thinking group („Worked for 8s"),
  tool karty s výstupem i failed ikonou, notifikace, `modes`/`configOptions`
  z handshake (Supervised / Auto-accept edits / Auto / Don't ask / Full access),
  volba modelu i modu přežije reopen. 5g: kill adapteru mid-turn usadil spinner
  bez „finished" notifikace. 5h prošlo **až po opravě** (nález 10).
- **§5e není v tomhle buildu čím otestovat:** `acp_list_sessions` má shim ve
  `wailsCompat/core.ts` i Go binding, ale **žádné call site v UI** — resume
  picker neexistuje. Resume jako takový funguje (`resumeSessionId` v
  `startRpcRuntime`, threadId přežil restart appky) a §5f (chat je po resume
  `idle`, ne zaseknutý na „running") drží.
- **§6 permissions:** 6a–6h prošlo, 6e/6f/6h **až po opravě** (nález 7 a 8).
  6a diff dialog + `✏️` marker, 6c `❓` + `waiting` (modrý dot), 6e `⚡` +
  `permission` (amber + bell), 6g „Claude proposed a plan" s Approve/Keep
  planning + `📋`, 6f dialog po přepnutí pryč a zpět **jednou**, ne zdvojený
  ani zmizelý — a přežije i celý reload stránky, protože request drží Go.
- **§7 crash recovery:** prošlo. Zabito uprostřed 1500slovné eseje s
  `folded_ord = 1920` a `max(ord) = 3009`; po restartu je v transcriptu i ta
  část, co dojela po posledním uložení, a na hranici replaye není nic dvakrát
  (`Job control is the Unix` právě 1×).
- **§8 nepřečteno:** 8a–8f prošlo, včetně **8f, což je ten původní bug, kvůli
  kterému plán vznikl** (`running → review` s composerem přes běžící tab).
  8e transientní `done` zmizel sám po ~4 s, 8c/8d review dot drží i po návratu
  focusu do okna na *jiný* tab.
- **§9 Manager:** 9a–9c prošlo. Odpovídá, tool karty se renderují, `burrow run`
  i `burrow spawn` fungují (vznikl tab, Manager nahlásil `pty_id`), a po
  přepnutí projektu tam a zpět **vlákno běží dál** a transcript je celý.
- **§10 mobil:** 10a–10f prošlo, 10d/10f **až po opravě** (nález 13).
  Mobilní bundle je `//go:embed`-nutý, takže testování chce
  `BURROW_DEV_MOBILE=<repo>/src-wails/dist-mobile/app` + `VITE_TARGET=mobile
  vite build`, jinak server vrací na `/` 404.

### Co to našlo (druhý průchod)

7. **Codex approval request se nikdy nedostal do UI.** `pumpCodexLine` nemá case
   pro `item/commandExecution/requestApproval` / `item/fileChange/requestApproval`
   / `item/permissions/requestApproval`, takže spadly do `default`, kde
   `rejectUnsupportedCodexRequest` odpoví JSON-RPC chybou. Turn pod *Supervised*
   umřel na „the environment rejected the command approval request" a žádný
   dialog se neukázal. Vtipné je, že komentář u `default` tvrdí „the known
   approval requests still travel through acp-req above this default" — jenže ta
   větev tam nikdy nebyla, a `parseAcpPermRequest` na frontendu přesně ty tři
   metody už umí. Doplněn case → `acp-req`. **§6e/§6f/§6h.**
8. **Regrese z fáze 2′/3: `acpPermRpcId` zůstal component-local.** `acpPermReq`
   se přestěhoval do session, jeho rpcId ne (`AgentChat.vue:832`). Po remountu
   tedy banner dál renderuje ze session, ale `serverRequest/resolved` se
   porovnává s *lokálním* `acpPermRpcId`, který je v nové instanci `null` →
   dialog už nikdy nezmizí. Horší je druhá polovina: `isIdle()` bere
   `acpPermReq !== null` jako „nezahazuj session", takže session s viset
   zůstalým requestem **nikdy nezidleuje** a listenery se nikdy neodpojí.
   Přesunuto do session. *(Stejná třída bugu čeká na `codexUserInput` —
   `item/tool/requestUserInput` drží celý request lokálně, takže po remountu
   panel zmizí a Codex čeká navěky. Neověřeno, nesahal jsem na to.)*
9. **Regrese z fáze 3: chat se otevírá odscrollovaný na začátek historie.**
   Scroll na konec dělal jen `watch(() => chats.activeByWs[props.workspaceId])`
   — bez `immediate` a bez ekvivalentu v `onMounted`. Dokud byl chat pořád
   mountnutý, přepnutí tabu ten watcher fajrovalo; teď se komponenta vytváří
   znovu a aktivní id je nastavené **dřív**, než nová instance mountne, takže
   watcher nefajruje vůbec. Doplněn `scrollToBottom(true)` v `onMounted` za
   naplnění transcriptu.
10. **§5h: mrtvá ACP/Codex session zůstala v registry.** `pump()` po EOF ohlásí
    `{_burrow:"exit"}`, ale session nechá v `acpReg`. `AcpStart` i `CodexStart`
    se zkratkují na „pro tohle id už session žije", takže další prompt šel do
    zavřeného stdin: `Error: write |1: broken pipe`. Přidán `dropIf(chatID, sess)`
    (drží jen když je to pořád *ta* session, aby dobíhající reader nesmazal
    svého nástupce) volaný **před** emitem exitu.
11. **Zaseknutý daemon položí celý workspace.** `daemonserver.broadcast` držel
    `s.mu` po celou dobu zápisu **všem** klientům, a `handleConn` si pod tímtéž
    lockem registruje nové spojení. Jeden klient, který přestal čítat (zabitý
    dev build, co nechal socket otevřený), tak zablokoval daemona pro všechny
    — i pro čerstvě připojené. Důsledek na frontendu: `list_pty_sessions` se
    **nikdy nevrátí** (ne error, ne timeout — visící promise), `Terminal.onMounted`
    uvízne na `Promise.all` a všechno pod ním (obnova chat threadů, `syncStore`)
    se neprovede → workspace bez jediného tabu a bez jediného threadu, bez
    hlášky. Tohle je nejspíš i to „tahle instalace nemá v `terminal_tabs`
    žádné řádky" z prvního průchodu. Opraveno dvakrát: snapshot klientů mimo
    lock + `SetWriteDeadline` na jeden send (a close při chybě, aby dekodér
    klienta odregistroval).
12. **`list_terminal_tabs` bez `.catch`.** Jeho sourozenec `list_pty_sessions`
    ho má, tenhle ne — a odmítnutí uvnitř `Promise.all` shodí celý mount hook
    se vším pod ním (viz nález 11). Doplněn catch s `console.warn`: rozbitý
    bridge pak stojí obnovu terminálů, ne obnovu chatů a sidebaru.
13. **Mobil: fáze 6 doparsovala tool výstupy, ale nikdo je nerenderoval.**
    `src/mobile/store.ts` plní `toolOutput` i `toolFailed` (řádky 246–247),
    `views/ChatView.vue` ale vykresloval jen `⌘ {{ message.text }}`. Failed
    tool byl na telefonu k nerozeznání od úspěšného a výstup nebyl vidět
    vůbec — tedy přesně to, co fáze 6 v plánu inzeruje jako nově umělé.
    Doplněn `⚠` marker, obarvený titulek a blok s výstupem. **§10d/§10f.**

### Nálezy druhého průchodu, které jsem NEopravil

14. **Odeslání z composeru nedá nový tab do URL.** Skončí to na `#/ws/:id`,
    ne `#/ws/:id/tab/:pty` — tab se otevře a vykreslí, ale není adresovatelný,
    dokud na něj člověk neklikne. `activateTab` naviguje jen
    `if (wsStore.active?.id === props.workspaceId && uiStore.viewingTabs)`, a
    `viewingTabs` je v tu chvíli ještě `false`, protože `WelcomeScreen.submit`
    volá `ui.closeWelcome()` až **za** `openChat`. Přeřazení nestačí
    (`router.push` se usazuje asynchronně) a ani `await ui.closeWelcome()` před
    `open()` to nespravilo — takže tam je ještě něco dalšího, co po
    `activateTab` adresu přepíše zpátky na `/ws/:id`. Zkusil jsem obojí a
    revertoval; chce to doměřit, ne hádat. **§2g/§5a.**

### Co se ukázalo jako prostředí, ne bug

- **`claude` spouštěný appkou občas hlásí „Failed to authenticate: OAuth session
  expired and could not be refreshed".** Z shellu `claude -p` jede (dědí auth
  po nadřazeném agentovi), s prázdným env a `ANTHROPIC_API_KEY=` — což je přesně
  to, co `ClaudeStart` posílá — hlásí „Not logged in". Chce to `claude /login`.
  Chvíli to vrátilo turny (Manager i spawnutý chat 73 běžely), takže je to spíš
  vypršelý token než konfigurace.
- **§6a nešlo napoprvé vidět**, protože v `config.json` leží uložené pravidlo
  `chatPermissionRules: ["Bash:burrow", "Edit"]` z dřívějšího „Always allow" —
  Edit se schválil sám. S `Write` (bez pravidla) dialog naskočil normálně.
- **Přepnutí providera uprostřed vlákna vlákno rozbije.** Přehodil jsem Codex
  chatu model na Claude Opus; `claudeSessionId` v něm zůstal Codexí `threadId`,
  takže `claude --resume <cizí id>` selhal. Chat 72 je tím pádem nepoužitelný.
  Nehlásím jako bug (nikdo to normálně nedělá), ale guard by tam být mohl.
- **Replay po restartu neposune `folded_ord`.** Transcript se vykreslí správně,
  ale `chat_messages` i značka zůstanou, kde byly, takže každý další start
  replayuje týž konec znovu. Idempotentní to je (ověřeno — nic dvakrát) a trim
  nic nesmaže, protože maže jen pod značkou. Podle plánu je to v pořádku,
  jen ať se to ví.
- **Mobil nemá permission UI** (control protokol zůstal na raw kanálu záměrně),
  takže turn pod *Supervised* rozjetý z desktopu se na telefonu jen zasekne na
  „READY" a čeká, dokud to člověk neschválí na desktopu. A seznam chatů hlásí
  u všech „0 zpráv" a otevřený existující chat je prázdný — historii mobil
  neskládá, jen živé eventy. Obojí čeká na fázi 7.
- **`wails dev` v externím prohlížeči má vlastní past:** bindingy volané dřív,
  než se usadí runtime websocket, se **nevrátí vůbec** (visící promise, ne
  reject). S opraveným nálezem 12 to už neshodí obnovu chatů, ale terminály
  po takovém loadu chybí, dokud stránku nereloadneš.

**Pozor na HMR při testování.** Editace `chatSession.ts` za běhu vyrobí nový
registry session, takže `busy` se čte z čerstvé session, kdežto `status` drží
starou hodnotu actoru. Vypadá to jako bug, není. Po každé změně kódu **reload
stránky**, jinak si vyrobíš falešné nálezy — mně se to stalo dvakrát.

## Příprava

```bash
just dev
```

Wails dev printne i devserver URL (obvykle `http://localhost:34115`). **Otevři
ji v Chrome** — dostaneš tu samou appku i s bindingy, ale navíc **URL bar a
devtools**. Bez toho routing testovat nejde a chybu v konzoli neuvidíš.

Konzoli měj otevřenou celou dobu. Cokoli, co se v ní objeví s `[chat-diag]`
nebo `chat stream:`, je relevantní.

---

## §1 — Boot (smoke)

| | krok | čekej |
|---|---|---|
| 1a | Spusť appku | Nabootuje na URL `#/` nebo `#/ws/<id>`, ne na prázdný shell |
| 1b | Když byl minule otevřený workspace s živými taby | `#/ws/<id>`, taby vidíš |
| 1c | Když živý tab nebyl žádný | `#/` a welcome composer |
| 1d | Minule jsi skončil na dashboardu | `#/dashboard` |

Spadne → `App.vue` `onMounted` (startovní `router.replace`) a `ui.startupMode`.

## §2 — Router (fáze 4)

| | krok | čekej |
|---|---|---|
| 2a | Klik na workspace v Sidebaru | URL `#/ws/<id>`; workspace bez živého tabu → `#/` |
| 2b | Klik na tab v tab baru | URL `#/ws/<id>/tab/<ptyId>` |
| 2c | Proklikej 5 tabů za sebou, pak **zpět** | Jedno zpět tě vrátí *před* tu sérii, ne o jeden tab (klik na tab je `replace`) |
| 2d | Klik na thread v Sidebaru (jiný workspace) | Přepne workspace **i** tab, URL má oba |
| 2e | Dashboard → klik na řádek aktivity | Jedna navigace, tab je hned aktivní (dřív to byl `setTimeout(…, 60)`) |
| 2f | „New thread" v Sidebaru | `#/`, composer, focus v inputu |
| 2g | Odešli z composeru prompt | Odejde na taby a tab se otevře — **nesmí** to skočit zpět na composer |
| 2h | Zavři poslední živý tab | Skočí na `#/` s composerem |
| 2i | TitleBar zpět (zavřít workspace) | `#/` |
| 2j | Ručně přepiš URL na `#/ws/999999` | Skočí na `#/`, ne prázdný shell |
| 2k | Ručně `#/ws/<platné id>/tab/<platné pty>` a Enter | Otevře přesně ten workspace a tab (deep link) |
| 2l | Zpět/vpřed 5× tam a zpět | Stav odpovídá URL, appka se nezasekne |

Spadne 2a–2i → volající (`Sidebar.selectWs/selectTab`, `Dashboard.goToTab`,
`ui.showTabs`/`closeWelcome`). Spadne 2j–2l → oba watchery route ⇄ store v
`App.vue`. **Zasekne se / bliká** → smyčka route → store → route, koukej na ty
dva watchery a jejich guardy.

## §3 — Render chatu, nativní transport (fáze 6) ← nejrizikovější

Nový chat s Claude. Prompt, který donutí agenta mluvit *a* použít tooly, např.:
*„Přečti package.json, pak README, a řekni mi jednou větou co ten projekt je."*

| | krok | čekej |
|---|---|---|
| 3a | Během streamování | Text roste **v jedné bublině**, ne nová bublina po každém chunku |
| 3b | | Thinking je vlastní (šedá) bublina, taky se skládá do jedné |
| 3c | | Tool karty se objeví s názvem (`Read`) a ikonou, ne s holým textem |
| 3d | Rozbal tool kartu po dokončení | Má **výstup** (dřív mohl zůstat prázdný) |
| 3e | Nech agenta selhat toolem (`Read /nope/x`) | Karta je označená jako failed (červeně) |
| 3f | Turn dokončí | Spinner zmizí, žádná bublina nezůstane „partial" (nesmí blikat kurzor) |
| 3g | | Pod turnem tokeny + cena |
| 3h | | Sidebar dostane vygenerovaný title threadu |
| 3i | Přejmenuj tab ručně, pošli další prompt | Tvoje jméno **zůstane** (guard `claudeGeneratedTitle`) |
| 3j | Pošli 2 prompty rychle za sebou | Druhý je `queued` (šedý), po prvním turnu se sám odešle a placeholder zmizí |
| 3k | Během turnu klikni abort | Turn skončí, **žádná** notifikace „finished" (`suppressNextDone`) |
| 3l | Reload appky (⌘R v Chrome) | Transcript je celý — text, tooly, výstupy |

Spadne 3a–3e → `src/lib/chatProjection.ts` (kryté testy, takže spíš dispatch v
`AgentChat.onEvents`). Spadne 3f–3k → `finishTurn()`. Spadne 3l →
`saveMessages` / `foldedOrd`.

## §4 — Unmount chatu (fáze 3)

| | krok | čekej |
|---|---|---|
| 4a | Pusť dlouhý turn, přepni na **jiný tab**, počkej až dojede, vrať se | Transcript je **celý**, nic nechybí v místě, kde jsi odešel |
| 4b | Totéž, ale přepni na jiný **workspace** | Totéž |
| 4c | Totéž, ale odejdi na **composer** (`#/`) | Totéž |
| 4d | Během turnu přepni pryč a hned zpět 5× | Žádné duplicitní zprávy, žádné dvojité bubliny |
| 4e | Přepni pryč od **idle** chatu a zpět | Transcript se načte z DB, je stejný |
| 4f | `burrow spawn` chat s promptem do **neaktivního** workspace | Agent nastartuje a odpoví, i když ses na to nekoukal |
| 4g | Po 4f otevři ten tab | Odpověď tam je, a **je označená nepřečtená** (zelený dot), ne přečtená |

Spadne 4a–4d → `lib/chatSession.ts` (`isIdle`/`release` — session se odpojila,
i když měla běžet). **Duplicity ve 4d** → `loadMessages` se volá i když session
není prázdná. Spadne 4f–4g → `Terminal.shouldMountChat` a to
`if (props.isWatching ?? true)` u `markSeen`.

## §5 — ACP / Codex

Nový chat s Codexem (nebo jiným ACP adapterem).

| | krok | čekej |
|---|---|---|
| 5a | Prompt | Text + thinking + tool karty jako u Claude |
| 5b | Turn dokončí | Spinner zmizí, notifikace přijde |
| 5c | Model / permission-mode picker | Nabídne volby (`modes`/`configOptions` z handshake) |
| 5d | Přepni model a pošli prompt | Tvoje volba zůstala (`restoreAcpSelections`) |
| 5e | Resume existujícího ACP threadu (picker) | Historie se vyrenderuje včetně **tvých** starých promptů |
| 5f | Po 5e | Chat je **idle**, ne zaseknutý na „running" ← přesně ta regrese, co jsem opravoval |
| 5g | Zabij adapter (`kill` procesu) mid-turn | Spinner se usadí, **žádná** notifikace „finished" |
| 5h | Po 5g pošli další prompt | Nastartuje nový proces, nezapisuje do mrtvé pipe |

Spadne 5a–5b → `NormalizeAcpLine` v `providerruntime.go`. Spadne 5e–5f →
`markAgentActive` podmíněný `!usesRpcRuntime`. Spadne 5c–5d, 5g–5h → raw ACP
cesta v `onAcpData` (handshake, `acpPromptRpcId`, `session.exited`).

## §6 — Permissions

| | krok | čekej |
|---|---|---|
| 6a | Nech agenta chtít `Edit` na souboru | Diff dialog + `✏️` marker ve feedu |
| 6b | Allow | Dialog i marker zmizí, turn jede dál |
| 6c | Nech agenta zavolat `AskUserQuestion` | `❓` marker, dot je `waiting` (modrý), zvuk |
| 6d | Odpověz | Marker zmizí |
| 6e | Nech agenta chtít `Bash` bez pravidla | `⚡` marker, dot `permission` (amber), bell v Sidebaru |
| 6f | Během čekání na permission **přepni pryč a zpět** | Dialog je pořád tam, ne zmizelý ani zdvojený |
| 6g | ExitPlanMode | `📋` marker, plán k review |
| 6h | Codex approval (ACP) | Allow once / Always / Deny fungují |

6f je ten důležitý — testuje, že `isIdle()` bere pending permission jako
„nezahazuj session". Spadne → `isIdle` v `chatSession.ts`.

## §7 — Crash recovery (fáze 1b + 2′)

| | krok | čekej |
|---|---|---|
| 7a | Pusť dlouhý turn a **zabij appku** (⌘Q / kill) uprostřed | — |
| 7b | Spusť znovu, otevři ten chat | Transcript má i tu část, co dojela **po** posledním uložení |
| 7c | Zkontroluj, že nic není dvakrát | Žádné duplicitní bubliny na hranici replaye |

Spadne → `replayChatStream` a `folded_ord`. Duplicity = `folded_ord` je pozadu
proti tomu, co je v `chat_messages`.

Kontrola v DB, když něco nesedí:

```bash
sqlite3 ~/Library/Application\ Support/burrow/workspaces.db \
  "select chat_id, folded_ord from chat_stream_state;
   select chat_id, count(*), min(ord), max(ord) from chat_stream group by 1;"
```

## §8 — Nepřečteno (fáze 3)

| | krok | čekej |
|---|---|---|
| 8a | Pusť turn, přepni na jiný tab, nech dojet | Zelený **review** dot, drží dokud tab neotevřeš |
| 8b | Otevři ten tab | Dot zmizí |
| 8c | Pusť turn, **odfokusuj okno** (jiná appka), nech dojet | Review dot + notifikace |
| 8d | Vrať focus do okna, ale **na jiný tab** | Dot **pořád** drží ← dřív ho návrat focusu smazal nepřečtený |
| 8e | Pusť turn, koukej na ten tab, nech dojet | Transientní `done` (limetka), zmizí sám za 4 s |
| 8f | Pusť turn, otevři **composer** přes něj, nech dojet | Review dot ← **původní bug**, tohle je ten důvod, proč celý plán existuje |

## §9 — Manager panel

Manager v pravém panelu jede přes `AgentChat`, takže ho fáze 6 taky přepsala.

| | krok | čekej |
|---|---|---|
| 9a | Otevři Manager, dej mu úkol | Odpovídá, tooly se renderují |
| 9b | Přepni repo a zpět | Vlákno běží dál, transcript celý |
| 9c | Nech ho spawnout sub-agenta (`burrow spawn`) | Vznikne tab; Manager vidí `Task` kartu |

9c testuje `subagents.started/completed` — přesunul jsem je z
`applyRuntimeEvent` do `onEvents`.

## §10 — Remote klient (mobil)

Smazal jsem mu vlastní parser, takže je to celé nové.

| | krok | čekej |
|---|---|---|
| 10a | Zapni remote access, spáruj telefon / otevři PWA | Seznam chatů |
| 10b | Otevři chat, pusť turn z desktopu | Na mobilu roste text v jedné bublině |
| 10c | | Thinking bublina |
| 10d | | Tool karty **s výstupem** ← to dřív remote klient neumel vůbec |
| 10e | Turn dokončí | Spinner zmizí |
| 10f | Failed tool | Označený jako failed |

Spadne → `applyEvent` v `src/mobile/store.ts` nebo to, že `chat-event-*` neteče
přes WS (`emitAll` → `wsBroadcaster`).

---

## Co určitě nefunguje (není bug, není hotové)

- **Chat, který nikdo neotevřel, nemá transcript.** Roste jen `chat_stream`.
  Čeká na fázi 7 (serverová projekce).
- **Dva klienti na tomtéž chatu si `chat_messages` přepisují** (replace-all).
  Taky fáze 7.
- **Nepřečteno nepřežije restart appky** — drží to XState actor v paměti.
  Čeká na timestampy v SQLite, tedy na druhého klienta.
- **`/settings/*` route není** — Settings je overlay, záměrně.

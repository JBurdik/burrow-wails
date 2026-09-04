# Plán: Route-model pohledů (t3code model)

## Cíl

Udělat z „co je uživatel právě vidí“ strukturálně vynucený stav, ne konvenci.
Cílem je stav, ve kterém skrytý thread **nemůže** být považován za sledovaný,
protože v tu chvíli neexistuje v DOMu — ne proto, že si na to někdo vzpomněl
napsat podmínku.

Referenční implementace: `pingdotgg/t3code`, `apps/web`
(v `~/.opensrc/repos/github.com/pingdotgg/t3code/main`).

## Problém

Burrow drží všechny thready mountnuté a přepíná je přes `v-show`. Viditelnost
tedy žije v DOMu, kam se logika nemůže zeptat, takže každé rozhodnutí typu
„kouká se na to uživatel?“ si ji musí dovodit z náhradních příznaků.

To už jednou selhalo: welcome composer je overlay nad `terminal-host`, ale
`ui.mode` zůstává `"terminal"` a `activeTabId` se nemění, takže tab pod ním
vycházel jako sledovaný. Turn dokončený za composerem hlásil transientní `done`
místo persistentního `review`, nezazvonil, a návrat focusu do okna ho označil za
přečtený, aniž ho kdo viděl.

Opraveno explicitním view-statem (`ui.welcomeVisible` / `ui.viewingTabs`,
`Terminal.seeActiveTab`) — viz *Co už je hotové*. Ta oprava ale kupuje jen
správné chování, ne garanci: nový kód se `viewingTabs` nemusí zeptat a bug se
vrátí v jiné podobě.

## Jak to řeší t3code

- Welcome/empty state je **vlastní route** `_chat.index.tsx`, nový nezapsaný
  thread `_chat.draft.$draftId.tsx`, existující thread
  `_chat.$environmentId.$threadId.tsx`. URL *je* view-state.
- `ChatView` je mountnutý jen pro thread v URL. Odejdeš na `/` → unmount.
- Přečteno není boolean, ale timestamp: `threadLastVisitedAtById[threadKey]`
  (`uiStateStore.ts`). Značí se v effectu uvnitř `ChatView` — tedy jen pro
  thread, který je opravdu na obrazovce (`ChatView.tsx`, `markThreadVisited`).
- Unread = porovnání časů: `hasUnseenCompletion` v `Sidebar.logic.ts`
  (`latestTurn.completedAt > lastVisitedAt`). K tomu ruční `markThreadUnread`,
  které posune `visitedAt` na `completedAt − 1 ms`.
- **Focus okna do read/unread logiky nevstupuje vůbec.** Žádné
  `document.hasFocus()`.

Klíč není router jako takový — je to fakt, že odchod z threadu ten thread
skutečně odmountuje, takže effect značící přečteno nemá kde běžet.

### Jak jim přitom nespadne stream (tohle je ta důležitá část)

Odmountovat thread si můžou dovolit ze dvou nezávislých důvodů:

1. **Data jsou serverová.** Raw výstup providera parsuje server
   (`ProviderRuntimeIngestion.ts`, 1721 ř.) na doménové eventy → append-only
   `orchestration_events` (`sequence`) → projektory (`ProjectionPipeline.ts`,
   1602 ř.) je foldují do projekčních tabulek. `projection_state.
   last_applied_sequence` drží, kam který projektor došel. Klient volá
   `subscribeThread(threadId)` a dostane **snapshot + živé eventy**; raw řádky
   nikdy nevidí a „replay podle ord" u nich neexistuje.
2. **Subscription nevlastní komponenta.** V
   `environments/runtime/service.ts` je refcountovaná mapa
   `threadDetailSubscriptions`; `shouldEvictThreadDetailSubscription` ji drží
   naživu i při `refCount === 0`, dokud je thread non-idle (pending approval /
   user input / plán). Unmount `ChatView` tedy stream nezabíjí — jen pustí
   referenci.

Bod 2 kopírujeme hned — je levnější a bezpečnější než replay, protože neuteče
nic, co by se pak muselo dohánět (fáze 2′).

Bod 1 jsme původně zamítli s tím, že je to „druhá kopie reduceru v Go". Ten
argument neobstál: kopie parseru už na frontendu byly **tři** a jen jedna
úplná. Ingestion half je proto v Go hotová (fáze 6), serverová projekce do
`chat_messages` zbývá.

## Proč to dnes nejde přímo

1. **Ztráta chat streamu.** `AgentChat.onBeforeUnmount` odregistruje listener
   `claude-data-{chatId}`. Backend proces schválně žije dál (jinak by se agent
   zabil při každém přemountování hostu), ale `emitAll` byl fire-and-forget bez
   jakéhokoli záznamu. Odmountovaný chat zahodil deltas vysílané mezitím →
   **díra v transcriptu**. *Fáze 1 to řeší napůl:* řádky se teď zapisují do
   `chat_stream`, takže existují. Chybí druhá půlka — někdo je musí přijímat,
   i když komponenta nekouká (fáze 2′), a trim je nesmí smazat dřív, než je
   někdo složí (fáze 1b).
2. **Rozsypaný terminál.** `XTerm.onBeforeUnmount` volá `detach_pty` a
   `term.dispose()`. PTY v daemonu žije dál (přežije i restart appky), ale
   návrat replayuje ring buffer do nově fitnutého terminálu → poškozený buffer.
   Přesně proto jsou dnes terminály mountnuté napořád (komentář v `App.vue`).
3. **Stav threadu v komponentě.** `Terminal.vue` drží `tabs`, `leafActors`
   (XState status actors) a countery dead-PTY watchdogu. Unmount = reset status
   dots a ztráta hook stavu.

t3code tyhle problémy nemá ze dvou důvodů — serverový thread state *a*
subscription, která nepatří komponentě (viz výše). My bereme druhý důvod;
první je pro single-user desktop příliš drahý.

## Principy

- Pořadí je dané: **nejdřív přežití dat, pak unmount, pak router.** Obrácené
  pořadí každý krok riskuje transcript.
- Zdroj pravdy o obsahu threadu se přesouvá z komponenty do storu a storage.
  Komponenta je view nad ním, ne jeho vlastník.
- Stream vlastní vrstva, která přežije unmount. Replay je pak nouzová cesta po
  restartu appky, ne běžný provoz — každý replay je příležitost transcript
  poškodit.
- PTY se neodmountovává nikdy. Terminál a chat mají různé nároky a řeší se
  odděleně — chat je odmountovatelný, terminál (zatím) ne.
- Přečteno je timestamp, ne boolean. Umožní to „mark unread“ a je to idempotentní
  vůči pořadí eventů.
- Focus okna přestává být vstupem read/unread logiky. Je to vstup jen pro
  notifikace a zvuk.
- Každá fáze je samostatně nasaditelná a sama o sobě přínosná.

## Fáze

### Fáze 1 — Live stream do storage (backend, bez UI změn) — **HOTOVO**

Chat deltas se zapisují průběžně, ne až na konci turnu, a jdou replaynout.

Implementace (`chatstream.go`): vlastní tabulka `chat_stream(chat_id, ord, kind,
line)`, ne append do `chat_messages` — ta drží *vykreslené* `ChatMessage`
objekty z frontendu, kdežto tady leží syrové řádky transportu. Sdílet jednu
tabulku by znamenalo naparsovat stream-json v Go, tedy druhou kopii reduceru,
která se rozejde. Je to spodní půlka t3code (`orchestration_events` +
`sequence`) bez jeho serverového projektoru — projektor zůstává frontend.

- `kind` = topic bez chat id (`claude-data` / `acp-data` / `acp-req`). Chat
  poslouchá na víc kanálech a replay musí každý řádek vrátit na ten svůj;
  `ord` je společné, takže se zachová i jejich vzájemné pořadí.
- `emitChatLine(chatID, kind, line)` je jediné dveře pro výstup agenta:
  persistuj, pak emituj. Prošly jím obě cesty — `claudechat.go` i všech sedm
  emitů v `acp.go` — takže Claude i Codex se chovají stejně.
- Zápis neblokuje stream: `ord` se přiděluje synchronně (aby ho fáze 2 mohla
  poslat s řádkem a deduplikovat podle něj), samotný INSERT jede na vlastní
  goroutině. Plná fronta řádek zahodí a zaloguje — díra v replayi je
  zotavitelná, zaseknutý stream ne.
- Log je omezený na posledních 20 000 řádků na chat, trim jednou za 1000 zápisů.
- Binding `LoadChatStreamSince(chatID, since)` → `[]ChatStreamLine{ord,kind,line}`.
- `DeleteChatMessages` maže i stream, aby frontend měl dál jedno volání.
- **Vedlejší nutná oprava:** `openDB` teď otevírá s `journal_mode(WAL)` +
  `busy_timeout(5000)`. Do teď byl jediný zapisovatel UI thread; s writer goroutinou
  je souběžný zápis okamžitý `SQLITE_BUSY` na defaultním rollback journalu.
  Odhalil to test, ne produkce.
- Test: `chatstream_test.go` (pořadí + `kind` + `since`, a `ord` pokračující
  přes restart appky).

### Fáze 1b — `folded_ord` (trim nesmí sežrat nesložené řádky) — **HOTOVO**

Fáze 1 trimuje `chat_stream` na posledních 20 000 řádků na chat. To je slepý
řez: může smazat řádky, které ještě nikdo nesložil do `chat_messages`, a pak
po restartu appky v transcriptu chybí kus turnu. t3code tenhle problém nemá,
protože ví, kam jeho projektor došel (`projection_state.last_applied_sequence`).

Vezmeme si přesně tenhle jeden sloupec, ne celý projektor:

- `chat_stream_state(chat_id, folded_ord)` — „frontend složil stream do
  `chat_messages` až sem".
- `SaveChatMessages` dostane volitelné `foldedOrd`; zapíše se ve stejné
  transakci jako zpráv, aby nemohl tvrdit víc, než co je uložené.
- Trim maže jen `ord < min(folded_ord, latest − keep)`. Chat, který nikdo
  neskládá, tedy roste — ale roste ohraničeně, protože ho nikdo neskládá jen
  když je zavřený, a při otevření se složí.
- Bez téhle fáze je fáze 3 (unmount) tichá ztráta dat, ne jen riziko.

Implementováno přesně takhle, plus:
- `ON CONFLICT … SET folded_ord = MAX(folded_ord, excluded.folded_ord)` — zpožděný
  zápis nesmí značku vrátit zpět a znovu „odemknout" už smazané řádky.
- `chatStreamHardKeep = 200000` jako pojistka proti chatu, který se nikdy
  neskládá: bez ní by DB rostla donekonečna. Je to backstop proti bugu, ne
  běžný trim.
- Binding `ChatFoldedOrd(chatID)` = odkud po restartu appky replaynout.
- Test `TestTrimKeepsUnfoldedLines` (včetně regrese značky).

### Fáze 2′ — Vlastnictví streamu mimo komponentu — **HOTOVO** (nahrazuje původní „replay při mountu")

**Původní fáze 2 byla replay po mountu podle poslední známé `ord`.** Zrušená:
plán sám ji označil za jediné místo, kde jde tiše poškodit transcript, a
t3code ukazuje, že ta práce vůbec nemusí vzniknout. Když stream nevlastní
komponenta, neuteče nic, a replay je potřeba jen po restartu appky — což
`chat_stream` z fáze 1 už umí.

Cíl: `messages` + reducer `onLine` přestanou být component-local ve
`AgentChat.vue` (3958 ř.) a přestěhují se do `claudeChats` storu. Listener
`claude-data-{id}` / `acp-data-{id}` / `acp-req-{id}` registruje **store**,
ne komponenta.

- Store drží per-chat `{messages, lastOrd, listeners}` a refcount mountů, po
  vzoru `threadDetailSubscriptions`.
- Odregistrovat listener smí až když je chat idle A refcount 0 — non-idle chat
  (běžící turn, čekající permission) si drží stream sám, i když ho nikdo
  nevidí. To je `shouldEvictThreadDetailSubscription`.
- `AgentChat.vue` se stává view nad storem: čte `messages`, posílá vstup.
- Replay (`LoadChatStreamSince`) se volá jen při startu appky pro chaty s
  `folded_ord < max(ord)`, ne při každém mountu.
- Až tady je unmount chatu bezpečný.

**Stav: hotová nosná část.**
- `src/lib/chatTypes.ts` — typy (`ChatMessage`, `CanUseToolReq`, ACP tvary) ven
  z SFC, aby na ně šlo sáhnout zvenčí.
- `src/lib/chatSession.ts` — registry `Map<chatId, ChatSession>`: transcript,
  stav turnu, blokující requesty obou transportů, listenery, refcount.
  `release()` odpojí listenery jen když je session idle — `isIdle()` je
  `shouldEvictThreadDetailSubscription`.
- Listener se drží `s.handlers`, ne konkrétní closure. Remount tedy jen
  **přepne reducer** (`setHandlers`), nikdy neodpojuje a nepřipojuje znovu —
  právě mezi tím se ztrácely řádky.
- `AgentChat.vue` refy destrukturuje ze session, takže zbylých ~3900 řádků
  zůstalo beze změny. V DOMu žijící stav (scroll element, menu, draft, timery)
  vědomě zůstal v komponentě.
- `loadMessages` se volá **jen když je session prázdná**. Neprázdná session je
  živější než DB; přiřadit přes ni by zahodilo právě to, kvůli čemu tenhle plán
  vznikl.
- Session zaniká jen s chatem: `Terminal.stopChatSession` a
  `claudeChats.remove` volají `dropChatSession`.
- `<AgentChat>` má v `Terminal.vue` `:key`, aby handle na session nemohl
  zestárnout (ManagerPanel klíčoval už dřív).
- Test `src/lib/chatSession.test.ts` — pravidlo eviction (idle padne, busy a
  blokovaný na uživateli přežijí, refcount nejde pod nulu).

**Dnes je to změna bez viditelného chování:** chat leaf je pořád `v-show`, takže
se nic neodmountovává. To je záměr — nosná konstrukce jde nasadit a odžít
dřív, než se na ni pověsí fáze 3.

**Dokončeno i zbytek 2′:**
- Event payload je teď `{ord, kind, line}` místo holého řádku. Odvodit
  „složeno až sem" na serveru nejde — znamenalo by to předpokládat, že každý
  odeslaný řádek byl i zpracovaný, což přes crash neplatí a trim by pak mazal
  nevyrenderované řádky.
- Rozbaluje to **session**, ne komponenta: `feed()` si zapíše `lastOrd` a
  reduceru předá jen `line`. `AgentChat.vue` o číslování neví.
- `saveMessages` posílá `foldedOrd = lastOrd + 1`.
- `replayChatStream(chatId)` = `chat_folded_ord` → `load_chat_stream_since` →
  reducer podle `kind`. Volá se jen při **prvním** naplnění session (tj. po
  startu appky), ne při každém mountu — a jen když značka existuje, jinak by
  „replay od 0" zduplikoval historii, kterou už `chat_messages` drží.
- `src/mobile/store.ts` obálku rozbaluje taky, jinak by remote klient přestal
  renderovat.
- Testy: dispatch podle `kind`, idempotence replaye, chat bez značky.

### Fáze 3 — Unmount chat threadů — **HOTOVO**

Chat leaf se přepne z `v-show` na `v-if`. Terminálové leafy zůstávají.

- Read/unread se přepíše na timestampy: `chatLastVisitedAt[chatId]` v ui storu,
  unread = `lastTurnCompletedAt > lastVisitedAt`. Nahrazuje `MARK_SEEN` cestu
  pro chaty.
- Značení přečteno se přesune **do** `AgentChat` (běží jen když je mountnutý),
  čímž z `Terminal.vue` mizí `seeActiveTab` i `markTabSeen` pro chat leafy.
  Pozor: po fázi 2′ je mountnutá jen *view*, ne stream — značit přečteno smí
  view, ne store, jinak se vrací původní bug v nové podobě.
- `document.hasFocus()` vypadává z read/unread; zůstává jen v `notifyDone` /
  `maybeNtfy`.
- Po téhle fázi je bug strukturálně nemožný pro chaty.

**Jak to dopadlo:**
- `Terminal.isChatVisible(tab)` = tab viditelný **a** workspace aktivní **a**
  `viewingTabs`. Chat leaf se renderuje přes `v-if`, ne `v-show`. Terminály
  zůstávají mountnuté (fáze 5).
- `markTabSeen` chaty přeskakuje. Značí se sám chat při mountu — a mountne se
  jen když je vidět. Predikát „kouká se na to uživatel?" tím zmizel z volajícího.
- **Výjimka:** chat spawnutý s promptem se mountne i neviditelný
  (`shouldMountChat`), jinak by `burrow spawn` do jiného workspace nikdy
  nenastartoval agenta. Takový chat mountne nesledovaný, proto je `markSeen`
  podmíněný `isWatching`.

**Odchylka od plánu: `document.hasFocus()` z read/unread nevypadlo.** t3code ho
nepotřebuje, protože je to webová appka — otevřený tab tam prakticky znamená
„dívá se". Burrow je desktop: okno na pozadí je opravdu „pryč", a právě případ
„agent dokončil, když jsem byl jinde" je hlavní feature. Bez focusu by turn
dokončený v odfokusovaném okně skončil jako transientní `done` (zmizí za 4 s)
místo persistentního `review`. Focus tedy zůstává vstupem `STOP { watching }`.

**Odloženo:** read/unread jako timestamp v SQLite. Dnes to drží XState actor
(`review` do `MARK_SEEN`) a druhý zdroj pravdy vedle něj by byl horší než
současný stav. Přijde s druhým klientem — viz *Další klienti*.

### Fáze 4 — Router — **HOTOVO**

Přidat `vue-router` a udělat z URL zdroj pravdy view-statu.

- Routy: `/` (welcome composer), `/draft/:draftId`, `/ws/:wsId/tab/:tabId`,
  `/dashboard`, `/settings/*`.
- `ui.mode`, `ui.welcomeOpen`, `welcomeVisible` a `viewingTabs` se ruší — nahradí
  je aktuální route.
- Přepsat volající: `Sidebar` (`openWelcome` / `closeWelcome` / aktivace tabu),
  `Spotlight`, `WelcomeScreen.submit`, `PetOverlay`, `controlBridge`
  (`focus_tab` / `focus_workspace` verby).
- Terminálové leafy zůstanou mountnuté mimo route view (skrytý host), dokud
  nebude vyřešená fáze 5.
- Přínos: deep linky, zpět/vpřed, jeden čitelný stav pro control API.

**Jak to dopadlo:**
- `src/router.ts`: `/`, `/ws/:wsId`, `/ws/:wsId/tab/:tabId`, `/dashboard`,
  catch-all → `/`. Hash history (appka běží z Wails asset schématu, kde není
  server, který by cestu přepsal na index.html); memory history mimo prohlížeč,
  aby import routeru neshodil store testy.
- **Žádný `<router-view>`.** Shell v `App.vue` route čte a pohled ukazuje sám,
  protože terminálový host musí zůstat mountnutý přes všechny routy —
  routovaná komponenta by se při navigaci odmountovala a reattach PTY rozsype
  scrollback (fáze 5). Router tu kupuje jeden čitelný stav, deep linky a
  zpět/vpřed, ne přestavbu DOMu.
- `ui.mode` je computed z routy (nejde do něj přiřadit), `welcomeOpen` zmizel
  úplně, `welcomeVisible`/`viewingTabs` čtou routu. Setters (`setMode`,
  `openWelcome`, `closeWelcome`, `toggleDashboard`) navigují.
- Tri-state `welcomeOpen` se rozpadl na dvě rozhodnutí: `showTabs()` použije
  `tabsOrWelcome()` (nic živého → composer), `closeWelcome()` jde na taby vždy
  (uživatel právě otevírá tab, který ještě není ve storu — bounce zpět na
  composer by ho spolkl).
- `App.vue` má dva hlídané watchery route ⇄ store, každý zajištěný proti tomu,
  aby odpovídal tomu druhému. Neznámé `wsId` = `replace("/")`, ne „ukaž co
  zrovna je".
- Přepsaní volající: `Sidebar` (`selectWs`/`selectTab`/`pickProject`),
  `WelcomeScreen.submit`, `Dashboard` (`goToTab` je teď jedna navigace místo
  „přepni mód a za 60 ms aktivuj tab"), `PetOverlay`, `Terminal.activateTab`
  (klik na tab je navigace, `replace` — proklikávání tabů nemá plnit historii),
  `controlBridge` (`focus_tab` → `/ws/:id/tab/:pty`, `focus_workspace` →
  `/ws/:id`).
- `resolveWelcomeVisible()` zrušena; její pravidlo i test žijí dál jako
  `tabsOrWelcome()` v `router.ts`.

**Odchylka:** `/settings/*` route nevznikla. Settings je overlay nad vším
(⌘,), ne obsah hlavního panelu — udělat z něj routu by znamenalo změnit UX,
ne jen adresu. Stejně tak Spotlight a docs.

**Neověřeno v běhu.** Na stroji není nainstalovaný prohlížeč ani
Playwright/Puppeteer a instalovat je bez vyžádání nebudu, takže tahle fáze má
za sebou `vue-tsc` + unit testy, ne skutečné proklikání. Než se to pošle dál,
chce to ručně projít: přepnutí workspace, klik na tab, dashboard tam a zpět,
zavření posledního tabu, `burrow focus_tab`, a zpět/vpřed.

### Fáze 6 — Ingestion do backendu — **HOTOVO**

Původně *Mimo rozsah* („druhá kopie reduceru v Go"). Ten argument neobstál:
kopie parseru **už byly tři**, jen jedna z nich úplná.

- `onLine` v `AgentChat.vue` — úplný (transcript, tooly, permissions, result).
- `lib/providerRuntime.ts` — částečný normalizér, běží *vedle* `onLine`.
- `mobile/store.ts` — vlastní, nejchudší, pro remote klienta.

Druhý klient z takového uspořádání nemůže dostat správný transcript — musí si
protokol doimplementovat do jiné hloubky než desktop. Parse patří tomu, kdo
vlastní proces, ne tomu, kdo zrovna renderuje.

**Hotovo:**
- `src-wails/providerruntime.go` — `ProviderRuntimeEvent` + normalizace obou
  transportů. Port `providerRuntime.ts` + `acpParser.ts`, rozšířený o to, co
  `onLine` řešil inline: thinking, `tool_result`, hranice turnu s usage/cost,
  vygenerovaný `session_title`, ACP markery `_burrow` (`session` → `session.id`,
  `exit` → `turn.completed`).
- 30 table-driven testů; první tři zrcadlí `providerRuntime.test.ts`, aby se
  port nemohl rozejít s TypeScriptem, který nahrazuje.
- `emitChatLine` publikuje navíc `chat-event-{id}` s `{ord, events}` — **vedle**
  raw kanálu, ne místo něj. Adopce po jednom konzumentovi, ne jedním překlopením.
- `LoadChatEventsSince(chatID, since)` = stejný catch-up jako
  `LoadChatStreamSince`, jen pro klienta, který nechce znát žádný wire format.
- **`src/mobile/store.ts` přepnutý.** Dva parsery → jeden `applyEvent`. Remote
  klient teď navíc umí tool výstupy a `failed`, což předtím neuměl.

**Co záměrně nenormalizuju:** odpověď JSON-RPC na `session/prompt` (končí turn,
ale korelace `acpPromptRpcId` patří tomu, kdo request poslal — hádat „každá
odpověď končí turn" by turn ukončilo na nesouvisející odpovědi) a
`control_request` (permission = rozhodnutí UI, ne transcript; má vlastní kanál).

**Dokončeno i zbytek:**
- Doplněné eventy: `user.delta` (uživatelský turn z ACP `session/load` replaye)
  a `session.exited` (proces je mrtvý — další send musí spawnout nový, na rozdíl
  od `turn.completed`, které nechává proces žít).
  - Claude `exit` → `[turn.completed, session.exited]`, protože frontend ho vždy
    bral jako result včetně notifikace.
  - ACP `_burrow:"exit"` → **jen** `session.exited`. ACP turn končí odpovědí na
    vlastní `session/prompt`; emitovat tam `turn.completed` by zazvonilo za turn,
    který nikdo nedokončil.
- `src/lib/chatProjection.ts` — pravidla transcriptu (append do partial, párování
  tool výsledků, `settleTranscript`) vytažená z SFC, 11 testů. Právě tady
  transcript tiše chybne, a dosud to šlo otestovat jen mountnutím 4000řádkové
  komponenty.
- `AgentChat.vue`: `onEvents(batch)` místo `applyRuntimeEvent`. Z `onLine` zmizely
  branche `assistant` / `user` / `result` / `exit` / `session_title`, z
  `onAcpData` celý `session/update` blok, `user_message_chunk` i `_burrow:"exit"`.
  Turn boundary je jedna funkce `finishTurn()` pro oba transporty.
- Na raw kanálu zůstalo jen to, co doménový event nemá: control (permission)
  protokol, Claude `system/init`, ACP handshake s `modes`/`configOptions`,
  `serverRequest/resolved` a korelace `acpPromptRpcId`. Všechno to jsou
  rozhodnutí UI, ne transcript.
- `replayChatStream` jede přes `load_chat_events_since`. Replay má obnovit
  transcript a nic víc — raw by znovu otevřel permission requesty, na které
  uživatel odpověděl před restartem.
- **Smazáno:** `lib/providerRuntime.ts` (+ test). `lib/acpParser.ts` zredukovaný
  na `parseAcpPermRequest` — `parseAcpUpdate` byl transcript, ten čte Go.

**Dvě regrese, které odhalila kontrola po sobě** (obě opravené):
- `markAgentActive()` se volal na každý transcript event. Jeho vlastní komentář
  varuje, že ACP `session/load` přehrává celou historii tímtéž feedem bez
  turn-done — chat by uvízl na „running". Podmíněno `!usesRpcRuntime`.
- `session.title` se aplikoval bez guardu `claudeGeneratedTitle`. Opakovaný
  title z dalšího resultu by přepsal jméno, které si uživatel mezitím změnil.

### Fáze 7 — Serverová projekce do `chat_messages` (nezačato, má předpoklad)

Cíl: Go jediný zapisovatel `chat_messages`. Vyřešilo by to tři věci, které
fáze 6 nechává otevřené — chat, který nikdo neotevřel, se neprojektuje;
replace-all souběh dvou klientů; a mobil by transcript nemusel skládat z eventů.

**Předpoklad, na který to narazilo:** transcript není jen výstup agenta. Je to
směs:
1. **stream-derived** — to, co teď dělá `chatProjection.ts`;
2. **client-authored** — uživatelská bublina (`AgentChat.vue:2512`, pushnutá
   lokálně při sendu) a UI markery (`❓ Question`, `📋 Plan ready`,
   `✏️ Edit: file`, `⚡ … wants permission`, `queued` placeholdery), které se
   navíc při vyřešení z feedu **mažou** (`removeFeedMarker`).

Dva zapisovatelé do jedné tabulky nejde. Návrh, který to řeší:
- `ClaudeSend` a ACP prompt jsou **už dnes** Go bindingy, takže uživatelskou
  bublinu umí zaprojektovat server. To je ta část, co jde.
- UI markery **nemají** jít do Go (emoji a zkracování cest v Go = špatná
  hranice). Zůstaly by klientské a efemérní; po reloadu je nahradí replaynutý
  `control_request`, ze kterého vznikly.
- Zbývá to nejtěžší: frontendový `messages` se stane **mergem** serverových
  řádků a lokálních markerů, a musí být jasné, kam marker patří v pořadí. To je
  vlastní design, ne dopsání funkce.

Nedělat to hned po fázích 4 a 6, které nikdo neproklikal. Mění se tím
persistence transcriptu — nejdražší věc, co v appce jde rozbít.

### Fáze 5 — Unmount terminálů (volitelné, možná nikdy)

Vyžaduje, aby reattach obnovil buffer bezchybně — tj. `attach_pty` se
snapshotem, který se nefituje znovu, nebo serializovaný stav terminálu na straně
daemona. Bez toho fázi nedělat; přínos je malý (konzistence) a riziko vysoké.

## Co už je hotové

Přípravný krok, který dělá bug neškodným už teď:

- `stores/ui.ts`: `welcomeVisible` a `viewingTabs` jako computed ve storu, plus
  pure `resolveWelcomeVisible(welcomeOpen, liveTabCount)`. Predikát se
  přestěhoval z privátního scope `App.vue`, kde ho `Terminal.vue` nemohl
  přečíst — to byl původ bugu. **Fáze 4 tohle zrušila:** `welcomeOpen` už
  neexistuje, predikát žije dál jako `tabsOrWelcome()` v `router.ts`.
- `Terminal.vue`: `isWatching()` gatuje na `viewingTabs`; tři rozsypané
  `markTabSeen` cesty (watch na workspace, watch na mode, `onWindowFocus`)
  sloučené do jedné `seeActiveTab()`.
- `Sidebar.vue`: highlight „New thread“ na `welcomeVisible` místo `welcomeOpen`.

Fáze 3 a 4 tenhle kód zrušily, jak bylo v plánu. Zbyl jen `Terminal.isWatching`
— a i ten už jen pro terminálové leafy a notifikace.

## Rizika

- Fáze 1 v horké cestě streamu. Pomalý zápis = zadrhávající stream. Měřit,
  bufferovat, nikdy neblokovat emit.
- Fáze 1b je to, co dělá trim bezpečným. Bez ní fáze 3 tiše maže data.
- Fáze 2′ je nejdražší krok plánu (refaktor `AgentChat.vue`). Zato ruší
  původní fázi 2 i s jejím rizikem replay/dedup. Test: nechat běžet dlouhý
  turn, přepnout pryč a zpět, porovnat transcript s `chat_stream` v DB.
- Fáze 4 se dotkla hodně call sites. `focus_tab`/`focus_workspace` teď
  navigují místo psaní do `ui.mode`; `spawn` se jich nedotýká. Hlavní zbylé
  riziko je smyčka route → store → route — obě strany jsou proto porovnávané
  před navigací a používají `replace`.
- ACP runtime (`acp.go`) má vlastní event cestu — fáze 1 a 2 musí pokrýt oba
  transporty, jinak se chová jinak Claude a jinak Codex.

## Další klienti (mobil)

Do budoucna se počítá s dalším klientem (mobilní app / PWA). Tvar zatím
nerozhodnutý, ale plán se ho dotýká na dvou místech:

- **Fáze 3 (read/unread).** Timestamp `chatLastVisitedAt` půjde do SQLite, ne do
  ui storu / localStorage. Stejná práce, jiné úložiště — a druhý klient jinak
  má vlastní unread stav, který se s desktopem nikdy nesejde. Čtení/zápis pak
  přes control verb se `ScopeRemote`.
- **Fáze 4 (router).** Pokud bude klient PWA nad stávajícím `src/`, dostane deep
  linky zadarmo a je to argument dělat fázi 4 dřív. Pokud nativní app nad
  tailnet API, router řeší jen desktop.

Fáze 1 a 2 jsou na tomhle rozhodnutí nezávislé — `chat_stream` je serverový
append-only log, ze kterého se umí dotáhnout jakýkoli klient.

## Mimo rozsah

- ~~Serverový thread state jako v t3code~~ — **přehodnoceno, viz fáze 6.**
  Argument „druhá kopie reduceru" byl špatně: kopie už byly tři. Ingestion je
  v Go hotová, serverová projekce do `chat_messages` zbývá.
- Odmountovávání terminálů (fáze 5, podmíněná).
- Sjednocení statusů chatu a terminálu do jednoho modelu — souvisí, ale je to
  vlastní plán.

# Plán: Živé podagenty v chatu (t3code model)

## Cíl

Claude Code umí nativně spawnout podagenty přes vestavěný nástroj `Task`
(`general-purpose` aj.) — probíhá to uvnitř JEDNOHO `claude` procesu, žádný nový
PTY. Burrow to dnes vidí jen jako obyčejný tool call: spinner "Task" dokud
nedoběhne, pak jeden blok výstupu. t3code stejný proud rozparsuje na
per-podagent živé řádky — model, effort, tokeny, počet nástrojů, label
aktuálního kroku — viz screenshot (`Kicked off 4 subagents`, `1 working Σ 531k`,
rozbalitelný `Implement Task 4: … general-purpose … 107k tok · 22 tools`).

Cíl je totéž v Burrow: Claude sám dělá multi-agent orchestraci (review vlny,
fix waves, paralelní implementace), Burrow tomu jen dá tvář.

Referenční implementace: `pingdotgg/t3code`
(`~/.opensrc/repos/github.com/pingdotgg/t3code/main`).

**Mimo scope tohle NENÍ:** `burrow spawn` / Manager panel / nové PTY taby —
to je Burrow-řízená orchestrace (agent běžící v samostatném procesu, viditelný
jako tab). Tady řešíme podagenty, které si Claude spustí SÁM, uvnitř svého
vlastního tool-use, a které dnes v transkriptu prakticky mizí.

## Problém

`src-wails/providerruntime.go` (`normalizeClaudeEvent`, řádek 136) rozparsuje
ze `system` zpráv jen `subtype: "session_title"`. Claude CLI ale v
stream-json při použití `Task` nástroje posílá i:

- `system/task_started` — `{task_id, description, task_type}`
- `system/task_progress` — `{task_id, description, summary?, usage?, last_tool_name?}`
- `system/task_notification` — `{task_id, status, summary?, usage?}`

Všechny tři dnes padají do `return nil` (žádný z `case` větví je nezachytí).
Podagent samotný je vidět jen jako:

1. `tool_use` blok `name: "Task"` → `tool.started` s `input` (`description`,
   `prompt`, `subagent_type`) → v UI obyčejná tool karta se spinnerem.
2. `tool_result` až po dokončení → `tool.completed` s celým finálním textem.

Mezitím — klidně 2 minuty u review vlny s 4 podagenty — uživatel nevidí nic:
žádný token count, žádný "na čem teď dělá", žádné odlišení "běží" vs "visí".
U vnořených podagentů (review→fix→re-review, jak je na screenshotu) je to
obzvlášť slepé, protože vnořený `Task` je jen další anonymní spinner uvnitř
jiného spinneru.

## Jak to řeší t3code

- **Server (`apps/server/src/provider/Layers/ClaudeAdapter.ts`):** stejné tři
  `system` subtypy se mapují na vlastní runtime eventy `task.started` /
  `task.progress` / `task.completed`, nesoucí `taskId` (`RuntimeTaskId`),
  `description`/`summary`, `usage`, `lastToolName`, `status`. `task_progress` a
  `task_notification` navíc protlačí `usage` do stavu vlákna přes
  `emitThreadTokenUsage` (`normalizeClaudeTaskProgressTokenUsage`), takže
  token counter u podagenta je vždy živý souhrn, ne jen poslední delta.
- **Klient (`apps/web/src/session-logic.ts`, `deriveWorkLogEntries`):**
  `task.started` se v work logu úplně **skipuje** (řádek 635) — nekreslí se
  jako vlastní entry, protože ho reprezentuje už `tool.started` toho `Task`
  tool_use. `task.progress`/`task.completed` (`isTaskActivity`, řádek 686) se
  naopak renderují jako aktualizace TÉ SAMÉ entry: label je `summary` (nebo
  `detail` jako fallback), tone `task.progress` → `"thinking"` (odlišný vizuál
  od dokončeného kroku). Klíčová věc: **`taskId` je identita**, `tool.started`
  „Task" karta a jeho `task.*` updaty se spárují a karta se přepisuje na
  místě, ne že by přibývaly nové řádky.
- Odtud plyne UI ze screenshotu: karta podagenta má stálé umístění v transkriptu
  (dané `tool_use.id` toho `Task` volání), ale její obsah (label, tokeny, počet
  nástrojů, stav) žije z `task.progress` proudu dokud nedorazí `task.completed`
  / `tool_result`.

## Jak to udělat v Burrow

### 1. Go: rozšířit vokabulář (`src-wails/providerruntime.go`)

Nové konstanty vedle `EvtToolStarted` atd.:

```go
EvtTaskStarted   = "task.started"
EvtTaskProgress  = "task.progress"
EvtTaskCompleted = "task.completed"
```

Pole na `ProviderRuntimeEvent`: `TaskID string`, `Summary string`,
`LastToolName string`, plus reuse `InputTokens`/`OutputTokens` pro usage.

V `normalizeClaudeEvent`, case `"system"`, doplnit vedle `session_title`:

```go
case "task_started":
    return []ProviderRuntimeEvent{{Type: EvtTaskStarted, TaskID: strField(event["task_id"]), ...}}
case "task_progress":
    return []ProviderRuntimeEvent{{Type: EvtTaskProgress, TaskID: ..., Summary: ..., ...usage}}
case "task_notification":
    return []ProviderRuntimeEvent{{Type: EvtTaskCompleted, TaskID: ..., ...}}
```

`NormalizeChatLine`/testy v `providerruntime_test.go` (pokud existuje —
ověřit) rozšířit o fixture řádky z reálného stream-json (dá se natáhnout
z `~/.opensrc/.../ClaudeAdapter.test.ts`, mají tam fixtures pro `task_progress`).

**Korelace na `Task` tool_use:** `task_started`/`task_progress` v Claude
stream-json nenesou `tool_use_id`, jen `task_id`. Potřeba ověřit na reálném
provozu (spustit review-vlnu, zalogovat raw stream-json), jestli `task_id`
odpovídá `tool_use.id` toho `Task` volání 1:1, nebo je nutné je párovat podle
pořadí/timing jako to dělá t3code (`ClaudeTaskState` map — mrknout, jak tam
klíčují). Tohle je jediné reálné riziko celého plánu — bez správné korelace
se karta neaktualizuje na místě, ale duplikuje.

### 2. Frontend: `chatProjection.ts` + `chatTypes.ts`

`ChatMessage` (chatTypes.ts) dostane pole pro live-task kartu:

```ts
taskId?: string;        // task_id, žije na tool-card se stejným toolUseId
taskLabel?: string;     // poslední summary/detail z task.progress
taskTokens?: number;    // input+output tokens za podagenta
taskToolCount?: number; // kolik nástrojů podagent zatím použil
taskStatus?: "running" | "done" | "failed";
```

`applyChatEvent` (chatProjection.ts):

- `task.started`: no-op na transkript (jako t3code) — `tool.started` s
  `name: "Task"` už kartu založil. Případně jen zapsat `taskId` na
  odpovídající tool zprávu, aby šlo párovat pozdější `task.progress`.
- `task.progress`: najít tool kartu **podle `taskId`** (ne `toolCallId` —
  ten patří `Task` tool_use, `taskId` je jiný korelační klíč, viz riziko
  výše), přepsat `taskLabel`/`taskTokens`/`taskToolCount`, `taskStatus =
  "running"`. Vrátit `true` (re-render), ale bez auto-scrollu na každou
  dílčí aktualizaci — jinak review vlna se 4 podagenty rozjebe scroll.
- `task.completed`: `taskStatus = "done"|"failed"`, necháme `tool.completed`
  (přijde zvlášť jako `tool_result`) dopsat finální `toolOutput`.

`PROJECTED` set rozšířit o všechny tři, `isProjectedEvent` beze změny logiky.

### 3. UI karta (`AgentChat.vue`)

Dnešní tool-card větev (`msg.role === 'tool'`, ~řádek 212) při
`msg.text === 'Task'` (raw native tool name) vykreslí rozšířenou variantu:

- Neexpandovaná: `▸ Task: {taskLabel ?? subagent_type z toolInput} · {taskTokens} tok · {taskToolCount} tools · [spinner|✓|✗]`
  — přesně řádek jako na screenshotu (`Implement Task 4: … general-purpose … 107k tok · 22 tools`).
- Expandovaná: běžný JSON `toolInput` (prompt, subagent_type) + `toolOutput`
  jako dřív.
- Vnořený `Task` (podagent spawnující podagenta, review→fix vlny) se řeší
  samo od sebe — je to jen další tool card ve stejném transkriptu, vykreslí
  se pod tou vnější, žádné speciální nesting není potřeba (na rozdíl od
  t3code, kde je vpravo samostatný "Agents" panel se stromem — to by šlo
  jako fáze 2, ne nutná podmínka pro živé statusy).

### 4. Sidebar / tab dot (mimo scope fáze 1)

t3code screenshot ukazuje i souhrn nad vláknem (`1 working Σ 531k`). To by v
Burrow odpovídalo něčemu na `Terminal.vue`/`Sidebar` úrovni chatu — **záměrně
odloženo**: nejdřív ověřit, že se `task.*` eventy vůbec dají spolehlivě
korelovat a vykreslit v samotném transkriptu (fáze 1–3), pak případně
agregovat do sidebar summary jako fáze 4.

## Fáze

1. **Go parsing** — `task_started`/`task_progress`/`task_notification` →
   nové `ProviderRuntimeEvent`. Ověřit `task_id` ↔ `tool_use.id` korelaci na
   reálném `claude` procesu (spustit prompt co dělá `Task`, zalogovat raw
   stream-json řádky do souboru, porovnat).
2. **Projection** — `chatProjection.ts`/`chatTypes.ts` rozšíření + testy
   (repo má vitest jen pro `agentStatus.ts` dosud — přidat
   `chatProjection.test.ts` pokud čas dovolí, logika je čistá funkce).
3. **UI karta** — `AgentChat.vue` tool-card větev pro `Task`, živé
   tokeny/label/tool count, bez auto-scrollu na progress update.
4. *(volitelné, mimo první dodávku)* — souhrn na úrovni vlákna/tabu
   ("N working, Σ tokens"), případně boční panel jako t3code — až fáze 1–3
   prokážou, že je co agregovat.

## Rizika

- **`task_id` korelace** (viz výše) — jediná neznámá, kterou nejde odvodit
  ze zdrojáku bez живého provozu; ověřit dřív, než se píše UI kód.
- Claude CLI musí `task_progress` posílat i v defaultní stream-json
  konfiguraci Burrow (bez extra flagů) — pokud ne, potřeba dohledat flag
  (t3code s Effect providerem nemusí spawnovat CLI identicky jako Burrow's
  `claudechat.go`).
- ACP transport (`NormalizeAcpLine`) nemá ekvivalent `Task` nástroje —
  tohle je čistě Claude-native feature, ACP agenti (pokud nějaký umí
  subagenty) by potřebovali vlastní mapping, mimo scope.

## Mimo rozsah

- Burrow-řízené spawnování (`burrow spawn`, Manager panel, nové PTY taby) —
  nezávislý mechanismus, funguje už dnes.
- Boční "Agents" panel jako v t3code screenshotu (samostatná plocha vedle
  chatu) — možná fáze 4, ne podmínka funkčnosti.
- Mobilní klient (`src/mobile`) — dostane stejné eventy přes stejný Go parser
  zadarmo, ale live-task UI pro mobil je samostatná práce.

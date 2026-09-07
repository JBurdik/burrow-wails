# Chat transcript — Implementation Plan, fáze 7 (fold patří Go)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `chat_messages` přestane být per-klient cache psaná frontendem a stane se tím, co Go odvozuje z `chat_stream`. Tím zmizí poslední sdílený stav, o který se dva klienti přetahují, telefon **poprvé** dostane historii chatu, a `chatProjection.ts` se smaže.

**Architecture:** `chat_stream` už je jediná pravda (append-only, `ord`, Go ho vlastní). Fáze 7 přesune **fold** — `events → ChatMessage[]` — z `chatProjection.ts` do Go, takže `chat_messages` je čistě derivovaný pohled a `folded_ord` je Go vlastní účetnictví, ne dohoda mezi klienty. Klienti pak jen čtou.

**Tech Stack:** Go 1.25; Vue 3 + vitest (mazání).

**Prerekvizita — SPLNĚNÁ.** CLAUDE.md ji vedla jako *„the transcript mixes stream-derived messages with client-authored ones"*. Commit `3412f1d` publikuje lidský prompt přes `emitChatLine`, takže transcript je od té chvíle **100 % odvozený ze streamu**. Bez toho by Go fold nemohl existovat: neměl by odkud vzít user bubliny.

## Co je dnes změřeno, ne odhadnuto

| fakt | kde |
|---|---|
| `chat_messages` je `(chat_id, ord, payload_json)` — řádek na zprávu, ne blob | `chatstore.go:28` |
| ale `SaveChatMessages` dělá `DELETE WHERE chat_id` + reinsert celé pole | `chatstore.go:94` |
| `folded_ord` **nejde dozadu** — `MAX(folded_ord, excluded.folded_ord)` | `chatstore.go:110` |
| `trim` maže jen složené řádky, **ale `chatStreamHardKeep` ten strop přebije** | `chatstream.go:187-194` |
| fold je 140 řádků, z toho ~90 reálné logiky | `src/lib/chatProjection.ts` |
| `chat_messages` čte/píše **jen `AgentChat.vue`**, tři call sites | `AgentChat.vue:1532,1550,1554` |
| **mobil transcript nepersistuje vůbec** a nikdy nevolá `LoadChatEventsSince` | `src/mobile/store.ts` |

**Skutečná cesta ke ztrátě dat**, popsaná přesně (dřívější formulace „marker jde dozadu" byla nesprávná — `MAX` to hlídá):

1. Desktop složí do ordu 50, uloží svůj seznam, `folded_ord = 50`.
2. Mobil má kratší transcript (třeba jen to, co viděl živě), složí do 30 → `DELETE` + reinsert **jeho** kratšího seznamu. `folded_ord` zůstane 50, protože `MAX`.
3. `chat_messages` teď obsahuje mobilní kratší verzi, ale marker tvrdí, že je složeno až po 50.
4. `trim` na to má právo a řádky ≤ 49 z `chat_stream` smaže. **Ta část transcriptu už není nikde.**

Druhá, nezávislá: `chatStreamHardKeep` může smazat i **nesložené** řádky, protože je to strop nad `folded_ord`, ne pod ním. Dnes to zachraňuje jen to, že desktop skládá průběžně.

## Global Constraints

- **Komentáře v kódu anglicky.** Commit subject anglicky, Conventional Commits.
- **`chat_stream` je a zůstává jediná pravda.** `chat_messages` je od téhle fáze *cache, kterou lze kdykoli zahodit a přepočítat*. Kdo do ní začne psát z klienta, obrací celou fázi.
- **Fold musí být deterministický a čistý.** Stejný vstup → stejný výstup, žádné hodiny, žádné náhodné id. Jinak dva klienti nedostanou tentýž transcript a jsme zpátky na začátku.
- **`ChatMessage` JSON tvar se NEMĚNÍ.** Klienti ho renderují dnes; fáze 7 mění, *kdo ho vyrobí*, ne *jak vypadá*. Změna tvaru je samostatná práce.
- **Migrace nesmí nic přepočítat destruktivně.** Existující `chat_messages` jsou v mnoha chatech jediná kopie částí transcriptu, které `trim` už ze streamu smazal. Přepočet ze streamu by je zahodil. Viz Task 5.
- Go: `cd src-wails && go test ./...` + `go test -race ./...`. Frontend: `pnpm test`, `pnpm build`, `pnpm build:mobile`. Vše: `just check`.

---

## File Structure

| soubor | odpovědnost |
|---|---|
| `src/lib/chatProjection.ts` (modify → delete) | Task 0 opraví idempotenci; Task 6 smaže |
| `src-wails/chatfold.go` (nový) | port foldu: `foldEvents([]ProviderRuntimeEvent) []ChatMessage` |
| `src-wails/chatfold_test.go` (nový) | portované případy z `chatProjection.test.ts` + idempotence |
| `src-wails/chatstore.go` (modify) | Go píše `chat_messages`; `SaveChatMessages` mizí z drátu |
| `src-wails/chatstream.go` (modify) | fold navazuje na `emitChatLine`; `chat-messages-changed` |
| `src-wails/remoteapi.go` (modify) | `list_chat_messages` in, `save_chat_messages` out |
| `src/components/AgentChat.vue` (modify) | čte místo skládání |
| `src/mobile/store.ts` (modify) | poprvé dostane historii |
| `CLAUDE.md` (modify) | kdo transcript vlastní |

---

### Task 0: fold není idempotentní — a já tvrdil, že je

**Files:**
- Modify: `src/lib/chatProjection.ts`
- Test: `src/lib/chatProjection.test.ts`

**Tohle je moje chyba z commitu `3412f1d`** a patří na začátek, protože ji fáze 7 zdědí do Go.

Ten commit tvrdí: *„a client that saw it live recognises the replay instead of drawing it twice"*. **Nerozpozná.** `appendChunk` s `partial: false` a `acp:`-id dohledá existující bublinu a udělá `last.text += text` — takže **stejný `user.delta` doručený dvakrát text zdvojí** („okok"), což je horší než druhá bublina.

Dnes to nebolí jen shodou okolností: `chat-event-*` je v `notRingable`, takže nemá `seq` a transport ho nededuplikuje; sender si echo hlídá `pendingSends`; replay jde od `folded_ord`, takže složené řádky nechodí. Až Go začne skládat, tenhle předpoklad padá.

- [ ] **Step 1: Write the failing test**

```ts
it("does not double a non-partial message delivered twice", () => {
  // A settled message identified by id is a COMPLETE bubble, so a second
  // delivery of the same event is a duplicate, not a continuation. Appending
  // turns "ok" into "okok" — a corrupted transcript rather than a repeated one.
  const s = { messages: [], nextMsgId: 1 };
  const ev = { type: "user.delta", messageId: "acp:user:7", text: "ok" };
  applyChatEvent(s, ev);
  applyChatEvent(s, ev);
  expect(s.messages).toHaveLength(1);
  expect(s.messages[0].text).toBe("ok");
});

it("still concatenates a PARTIAL message's chunks", () => {
  // The opposite case, and the reason this cannot be fixed by refusing every
  // repeat id: streamed assistant text arrives as many events under one id.
  const s = { messages: [], nextMsgId: 1 };
  applyChatEvent(s, { type: "text.delta", messageId: "acp:a1", text: "he" });
  applyChatEvent(s, { type: "text.delta", messageId: "acp:a1", text: "llo" });
  expect(s.messages[0].text).toBe("hello");
});
```

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Fix `appendChunk`**

Non-partial + id'd → **replace, do not append**: `last.text = text`. A settled
bubble carries its whole text in one event (`normalizeUserPrompt` emits the
prompt once), so assignment is both idempotent and correct. Partial chunks keep
appending. Do NOT extend this to the id-less (native Claude, position-matched)
path — there a repeat genuinely is a continuation.

- [ ] **Step 4: Run `pnpm test`**

- [ ] **Step 5: Commit** — `fix(chat): a settled message delivered twice must not double its text`

---

### Task 1: port the fold to Go

**Files:**
- Create: `src-wails/chatfold.go`, `src-wails/chatfold_test.go`

**Interfaces:**
- Produces:
  ```go
  type ChatMessage struct { /* the shape AgentChat renders — see chatTypes.ts */ }
  // foldEvents is PURE: same events in, same messages out, no clock, no ids
  // from anywhere but the sequence itself.
  func foldEvents(events []ProviderRuntimeEvent) []ChatMessage
  ```

Port `applyChatEvent` + `appendChunk` + `settleTranscript` verbatim in behaviour,
including the two rules the TS comments call load-bearing:

- **`acp:`-prefixed id → match BY id; no id → match by position.** Getting it
  backwards merges two agents' sentences into one bubble.
- **`tool.completed` matches the LAST call with that id**, not the first: a tool
  is called repeatedly with the same name and only ids disambiguate.

`nextMsgId` becomes the index in the output slice — the id was only ever a
client-side render key, and deriving it from position is what makes the fold
deterministic across clients.

- [ ] **Step 1: Write the failing tests** — port every case from
  `src/lib/chatProjection.test.ts` (it exists and covers the ACP/native split),
  plus Task 0's idempotence pair, plus: a `tool.completed` with no matching
  call is dropped rather than creating a headless tool row.
- [ ] **Step 2–4**: implement, `go test -race`.
- [ ] **Step 5: Commit** — `feat(chat): fold events into messages in Go`

---

### Task 2: Go writes `chat_messages`

**Files:**
- Modify: `src-wails/chatstore.go`, `src-wails/chatstream.go`

`emitChatLine` already persists the line and knows the `ord`. After that, fold
and write — under the **same transaction** as the `folded_ord` bump, which is
the invariant the current code gets right and must keep: *the marker and the
messages move together, or the trim gets permission to delete something the
stored transcript does not contain.*

**Do not fold the whole stream per line.** Keep the folded tail in memory per
chat (the writer already exists per chat) and append; recompute from the stream
only on first touch after a restart. A chat with 4000 lines re-folded on every
token is a live-typing hang.

Emit **`chat-messages-changed-<chatID>`** — additive, and in `notRingable`
(same reasoning as the other chat channels: it has a durable log).

- [ ] **Step 1: Write the failing test** — after two `emitChatLine` calls,
  `LoadChatMessages` returns the folded pair; `folded_ord` advanced in the same
  transaction; a rollback leaves neither moved.
- [ ] **Step 2–4**: implement, run.
- [ ] **Step 5: Commit** — `feat(chat): Go owns the folded transcript`

---

### Task 3: `save_chat_messages` leaves the wire

**Files:**
- Modify: `src-wails/remoteapi.go`, `src-wails/chatstore.go`

Delete the write path from the client surface. `SaveChatMessages` goes to
`remoteDenied` (or away entirely — `TestRemoteSurfaceIsExhaustive` forces the
decision). `load_chat_messages` stays; `list_chat_messages` is the same thing
under a name that does not imply a matching save.

**One call site is NOT a fold** and must survive: `AgentChat.vue:3060`, the
one-time localStorage→SQLite transcript migration. It imports history that has
no stream behind it at all. Keep it as an explicit `import_chat_messages`
(`scopeOrchOperate`), or run it in Go — decide in the task, do not delete it by
accident.

- [ ] **Step 1–5**: as above. Commit — `refactor(chat): the transcript is read-only on the wire`

---

### Task 4: clients read instead of folding

**Files:**
- Modify: `src/components/AgentChat.vue`, `src/mobile/store.ts`

Desktop: `load_chat_messages` on mount (already does), `chat-messages-changed`
→ reload, and **no local fold**. `onEvents` keeps only what is genuinely the
view's: scroll, notifications, sub-agent bookkeeping, usage accounting.

Mobile: gains transcript history **for the first time** — today `RemoteChat.messages`
is memory-only and reopening a chat on the phone shows nothing. This is the
task that makes the phone's chat view honest, and it is why fáze 7 is worth
doing beyond the data-loss fix.

- [ ] **Step 1: Write the failing test** — mobile store: a chat opened with no
  live events still renders history from `list_chat_messages`.
- [ ] **Step 2–4**: implement, `pnpm build` + `pnpm build:mobile`.
- [ ] **Step 5: Commit** — `feat(mobile): the phone finally has chat history`

---

### Task 5: migration, and what must NOT be recomputed

**Files:**
- Modify: `src-wails/chatstore.go`

**The dangerous task.** Existing `chat_messages` rows are, for many chats, the
only surviving copy of a transcript whose stream lines `trim` already deleted.
A migration that recomputes from `chat_stream` would silently truncate years of
history to whatever is still in the last 500 lines.

So: **keep every existing row untouched.** Go's fold takes over from
`folded_ord` forward, appending. The first fold after this ships reads the
stored tail as its starting state rather than starting empty.

- [ ] **Step 1: Write the failing test** — a chat with stored messages and a
  `folded_ord` beyond the oldest surviving stream line keeps its stored
  messages and appends the new fold after them.
- [ ] **Step 2–4**: implement, run.
- [ ] **Step 5: Commit** — `feat(chat): adopt stored transcripts instead of recomputing them`

---

### Task 6: delete the client fold

**Files:**
- Delete: `src/lib/chatProjection.ts`, `src/lib/chatProjection.test.ts`
- Modify: `src/lib/chatSession.ts` (the `foldedOrd` bookkeeping), `src/components/AgentChat.vue`

Only after Task 4 is verified by hand — this is the point of no return, and
`chatProjection.test.ts` is the reference the Go port was measured against.

- [ ] **Step 1–3**: delete, `just check`, commit — `refactor(chat): delete the client-side fold`

---

### Task 7: dokumentace

- [ ] **Step 1** — CLAUDE.md: `chat_stream` je pravda, `chat_messages` je
  derivovaná cache Go, `folded_ord` je Go účetnictví a ne dohoda klientů,
  fold je čistá funkce v `chatfold.go`, telefon má historii, a co se smazalo.
  Zrušit i tu větu o „fáze 7 v plánu" v sekci o provider protokolu.
- [ ] **Step 2: Commit**

---

## Self-review

**Co tahle fáze zavírá:**

| problém | task |
|---|---|
| dvě kopie transcriptu, jedna přepisuje druhou celou | 2, 3 |
| `folded_ord` tvrdí složeno, `chat_messages` to neobsahuje → `trim` maže nenávratně | 2 (jedna transakce) |
| telefon nemá historii chatu vůbec | 4 |
| settled zpráva doručená dvakrát zdvojí text | 0 |
| dvě implementace foldu (dnes jen jedna, ale klientská) | 1, 6 |

**Vědomě mimo:** `chatStreamHardKeep` může smazat nesložené řádky — po Tasku 2
se fold děje synchronně s emitem, takže na to okno není kde vzniknout, ale
**strop sám je pořád špatně napsaný** (je nad `folded_ord`, ne pod ním) a
zaslouží si vlastní opravu s vlastním testem. `ChatMessage` tvar. Web Push
(to je fáze 7 ve *spec* remote accessu — jiná věc, jiný dokument, uživatelem
odložená).

**Risk, nahlas:** Task 4 a 6 přepisují datovou cestu hlavního chatového UI.
`chatProjection.test.ts` je jediná existující záchranná síť a je to net na TS
straně, kterou mažeme. Proto je Task 1 měřený **proti němu** a Task 6 je až za
manuálním ověřením — ne dřív.

**Placeholders:** žádné. Každé číslo a každý řádek výše je z kódu, ne z paměti;
dvě moje dřívější tvrzení byla při psaní tohoto plánu opravena (`folded_ord`
nejde dozadu; replay není idempotentní).

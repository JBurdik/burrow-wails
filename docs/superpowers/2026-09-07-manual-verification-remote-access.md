# Manuální ověření remote accessu (fáze 1–6)

**Není to plán a není to pro agenta.** Agent tohle udělat nemůže — nespustí GUI
a neuvidí telefon. Je to checklist pro člověka.

**Proč to existuje.** Fáze 1–6 mají zelený `just check` a **nula manuálních
ověření**. Za dvě minuty prvního reálného použití z toho vypadly dvě skutečné
chyby a ani jedna nepadala v testech:

- chat vytvořený z telefonu byl v sidebaru neviditelný — filtr, který
  nerozeznal nový chat od opuštěného (`42111d2`)
- `PhaseStore` vůbec nenaběhl, `no such column: id`, takže tečky stavu byly
  mrtvé na obou klientech (`e73b1dc`)

To není smůla. Dokud tenhle průchod neproběhne, „hotovo" u fází 1–6 znamená
„zacommitované a zelené", ne „funguje".

**Jak spustit.** `pnpm build:mobile && just build`, pak
`open src-wails/build/bin/Burrow.app` — nebo `just dev`. Ke každému bodu si
zapiš **co jsi viděl**, ne „ok": pozorování je celá hodnota tohohle cvičení.
Z každého nálezu udělej task (do plánu zbytků, nebo vlastní, podle velikosti).

## Párování a zařízení

- [ ] **Spárovat.** Settings → Remote access → zapnout. Na telefonu
  `https://<magicdns>/burrow/`, zadat kód. Očekávej: telefon se připojí a
  v Settings **přibude řádek zařízení** se jménem („iPhone") a „last seen".
- [ ] **Špatný kód.** Zadej `000000`. Očekávej: zpráva o špatném/expirovaném
  kódu, **ne** „nedostupné" — ty dvě věci se opravují jinak a rozlišují se
  podle typu chyby, ne podle textu.
- [ ] **Lockout.** 5× špatně. Očekávej: Settings hlásí zamčeno a nabízí nový
  kód; správný kód už neprojde, dokud negeneruješ nový.
- [ ] **Expirace.** Nech kód ležet >3 min. Očekávej: Settings ho
  **nezobrazuje** (ne mrtvé číslice, které nebudou fungovat).
- [ ] **Revoke.** Settings → Revoke u telefonu. Očekávej: telefon **okamžitě**
  spadne na párovací obrazovku, ne nekonečné „připojuji".
- [ ] **Druhé zařízení.** Spáruj i prohlížeč. Revoke jednoho **nesmí** odpojit
  druhé.

## Fail-closed hlídače

- [ ] **Funnel.** `tailscale funnel 443 on`, pak zkus zapnout remote access.
  Očekávej: **odmítne** a řekne proč (`tailscale funnel off`). Pak funnel
  vypnout a ověřit, že už to jde.

## Data a stav

- [ ] **První paint.** Na telefonu dashboard. Očekávej: workspaces i taby
  **všech** workspaců, ne jen mountnutého; jeden round trip, ne postupné
  dosypávání.
- [ ] **Tečky přes oba klienty.** Spusť agenta v terminálu. Očekávej: tečka se
  hýbe na desktopu **i** na telefonu.
- [ ] **Read receipt je per-device.** Nech turn dojet bez koukání → **review**
  tečka. Otevři tab na desktopu → zhasne. Na telefonu **zůstane** review,
  dokud tam nekoukneš.
- [ ] **ESC uprostřed turnu.** Očekávej: tečka se usadí na idle **bez** review
  badge (zrušený turn nemá receipt).
- [ ] **Chat z telefonu.** Vytvoř chat na telefonu. Očekávej: objeví se
  v desktopovém sidebaru **bez restartu**.
- [ ] **Zpráva z telefonu.** Napiš na telefonu. Očekávej: bublina se objeví
  i na desktopu, a naopak.

## Odolnost

- [ ] **Zabití socketu uprostřed turnu.** Vypni/zapni Wi-Fi na telefonu během
  streamu. Očekávej: „Odpojeno" → „Obnovuji spojení" → dojede a **tečky
  doskočí**, ne zmrznou. Terminálový scrollback v té mezeře **chybět bude** —
  to je vědomé, `pty-data` není v replay ringu.
- [ ] **Backpressure.** `cat` velkého souboru v terminálu s otevřeným
  telefonem. Očekávej: appka nezamrzne; nejhorší přijatelný výsledek je drop
  klienta a reconnect.
- [ ] **Restart appky s běžícím agentem.** Očekávej: fáze se obnoví z SQLite,
  tečka není prázdná.

## Zbytek

- [ ] **`burrow spawn` round trip.** Z agenta `burrow spawn ...`. Očekávej:
  nový tab se otevře a verb vrátí `pty_id`.
- [ ] **PWA install.** Na iOS „Add to Home Screen". Očekávej: jde to (MagicDNS
  je secure context) a appka se po otevření sama připojí uloženým tokenem.
- [ ] **LSP.** Až bude Task 2 plánu zbytků hotový: ověř, že se server
  **opravdu spustí** — srovnat jména argumentů nestačí.

## Až tohle projde

Teprve pak má smysl rozhodovat o integraci branche (merge lokálně / push + PR /
nechat). `main` je nedotčený a má na sobě nezacommitovanou práci
(extensions/SDK) — nic tam nepřepisovat. Použij
`superpowers:finishing-a-development-branch`.

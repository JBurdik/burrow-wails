# Plán: Šablony threadů

## Cíl

Urychlit založení opakujících se druhů práce, aniž by se z Burrowu stal rigidní workflow nástroj. Šablona je editovatelný výchozí bod pro thread: nastaví užitečný kontext, doporučené spuštění a očekávaný výstup. Uživatel může vše změnit před spuštěním.

## Problém

Prázdný prompt je flexibilní, ale uživatel i agent pokaždé znovu rozhodují, zda použít worktree, co má agent udělat před editací, které ověření spustit a jak má vypadat hotový výsledek. To vede k nejednotným výsledkům a pomalejšímu zadávání běžných úloh.

## Principy

- Šablona předvyplňuje, nikoli zamyká.
- Výběr šablony otevře normálně editovatelný formulář threadu.
- V1 neprovádí automatické akce jen kvůli výběru šablony; worktree vznikne až při spuštění threadu.
- Šablona vyjadřuje záměr a očekávaný výsledek, ne skrytý magický prompt.
- Vestavěné šablony fungují i bez projektové konfigurace.

## První sada vestavěných šablon

### Opravit bug

Agent má reprodukovat nebo ověřit problém, najít pravděpodobnou příčinu, implementovat nejmenší bezpečnou opravu a přidat či spustit regresní ověření. Výsledek obsahuje příčinu, změněné oblasti, testy a omezení.

### Implementovat feature

Agent nejprve navrhne krátký plán, identifikuje dopad, implementuje změnu a ověří ji relevantními testy. Šablona doporučí worktree, ale uživatel jej může vypnout. Výstup obsahuje shrnutí, testovací postup a omezení.

### Prozkoumat codebase

Read-only orientovaná šablona. Agent zmapuje relevantní části kódu, tok dat a závislosti, určí rizika a nabídne konkrétní implementační plán — bez automatické editace.

### PR review

Agent projde diff a relevantní testy. Nálezy vrátí podle závažnosti s konkrétním odůvodněním a odkazy na soubory; pokud nenajde chyby, uvede zbývající rizika nebo mezery v ověření.

### Bezpečný refaktor

Agent nejprve popíše dotčené chování a závislosti, zachová veřejné chování, upraví testy podle potřeby a doloží ověření.

## Návrh dat šablony

```text
id, název, popis, ikona
výchozí agent a model (volitelné)
doporučené spuštění: aktuální workspace | worktree
promptová kostra
checklist očekávaného výsledku
volitelná doporučená příloha nebo kontext
```

`Doporučené` znamená viditelně předvyplněné, nikdy vynucené. Pokud vybraný provider není k dispozici, formulář nabídne kompatibilní výchozí hodnotu.

## UX tok

```text
Nový thread
  → Vybrat „Od nuly“ nebo šablonu
  → Upravit cíl, prompt, agent/model a workspace/worktree
  → Volitelně připojit soubory či obrázky
  → Spustit
  → Thread si nese id šablony v kontextu a historii
```

V seznamu threadů lze nenápadně zobrazit použitou šablonu. Skutečný cíl threadu zůstává dominantní.

## Projektové šablony — pozdější fáze

Po ověření vestavěných šablon lze přidat verzované šablony v `.burrow/templates/` nebo `.burrow/burrow.json`. Projektová šablona může dodat testovací příkazy, architektonická pravidla nebo povinnou strukturu výsledku. Načtení projektové konfigurace musí projít trust branou, která ukáže, co konfigurace přidává, a projektovou akci nepustí bez jednorázového schválení.

## Mimo rozsah V1

- Automatické spuštění agenta po výběru šablony.
- Povinné workflow kroky nebo blokování ručních úprav.
- Marketplace/sdílení šablon mezi uživateli.
- Hodnocení kvality pouze podle checklistu.

## Rozhodnutí k potvrzení

1. Mají být vestavěné šablony plně upravitelné a kopírovatelné už v první verzi?
2. Má se id šablony ukládat jen pro historii, nebo také pro analýzu úspěšnosti typu práce?
3. Mají projektové šablony vestavěné přepsat, nebo je jen doplnit?

## Kritéria úspěchu

- Běžný thread lze založit rychleji než z prázdného promptu.
- Uživatel vždy vidí a může upravit instrukce pro agenta.
- Výstupy stejného typu práce obsahují konzistentnější shrnutí a ověření.
- Šablony přirozeně poskytují data pro PR cockpit.

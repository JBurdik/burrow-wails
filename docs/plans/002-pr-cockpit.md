# Plán: PR cockpit

## Cíl

Proměnit dokončený thread ve snadno kontrolovatelný a kvalitně připravený pull request. PR cockpit není náhradou GitHubu nebo jiné forge; je to závěrečná vrstva Burrowu, která spojí záměr threadu, jeho diff, ověření a následný review feedback.

## Problém

„Agent skončil“ neznamená „změna je připravená k mergi“. Mezi dokončením práce a otevřením PR se ztrácí kontext: co thread řešil, co se změnilo, jaké testy agent spustil, co zůstává rizikem a kam vrátit feedback z code review.

## Principy

- PR se nikdy nevytváří automaticky; Burrow připraví návrh, uživatel jej schválí.
- Zdroj pravdy pro stav PR, komentáře a CI zůstává forge.
- Tvrzení agenta o testech a rizicích jsou odlišitelná od dat načtených z Gitu/forge.
- Cockpit funguje i bez napojení na GitHub: alespoň jako diff review a exportovatelný popis PR.
- Dostupný je až z threadu s konkrétním výsledkem, typicky z review/checkpoint stavu.

## Stavový tok

```text
Thread s výsledkem
  → Zkontrolovat změny
  → Připravit PR
  → Vytvořit draft nebo „ready for review“ PR
  → Sledovat CI a review
  → Poslat feedback do stejného threadu
  → Merge / uzavřít thread
```

Thread se nemá označit jako skutečně hotový pouze proto, že agent skončil. Rozlišujme alespoň `čeká na review`, `PR otevřené`, `změny vyžádány`, `merged` a `uzavřeno bez merge`.

## Obrazovka 1: Zkontrolovat změny

Cockpit před přípravou PR ukáže:

- cílovou větev a větev/worktree threadu;
- souhrn diffu a seznam změněných souborů;
- checkpoint, ze kterého diff vychází;
- shrnutí agenta;
- vykonané testy a jejich známý výsledek;
- agentem deklarovaná rizika či omezení.

Uživatel může otevřít plný diff, vrátit se do threadu s feedbackem nebo přejít k přípravě PR. Nejde o náhradu code editoru: cílem je rychle posoudit připravenost výsledku.

## Obrazovka 2: Připravit PR

Formulář obsahuje:

```text
Název PR
Popis PR
Cílová větev
Draft × ready for review
Volitelní reviewers / labels (až po připojení forge)
```

Burrow nabídne editovatelný návrh názvu a popisu. Popis staví na konkrétních datech threadu, ne jen na obecné textové generaci:

```markdown
## Co se změnilo

## Proč

## Ověření

## Rizika a omezení

## Kontext
Odkaz na Burrow thread / checkpoint
```

Použil-li thread šablonu, její checklist doplní relevantní sekce. „Opravit bug“ například přidá příčinu a regresní test, zatímco „Implementovat feature“ zvýrazní uživatelský dopad a omezení.

Před potvrzením Burrow zkontroluje minimální předpoklady: změny existují, cílová větev je známá, aktuální větev je publikovatelná a uživatel viděl návrh. `Push` i vytvoření PR jsou vždy výslovné akce.

## Obrazovka 3: Sledovat PR

Po vytvoření se v detailu threadu zobrazí kompaktní karta:

- odkaz, číslo, autor a stav PR;
- stav CI;
- review stav a počet nevyřešených komentářů;
- poslední aktivita;
- akce: otevřít ve forge, obnovit stav, poslat feedback do threadu.

V první verzi je rozumné číst jen souhrnný stav a na detail komentářů odkazovat do forge. Později lze zvolený review komentář přeměnit na follow-up ve stejném threadu s odkazem na soubor a řádek.

## Napojení na worktrees a Git

- Thread ve worktree je ideální případ: branch a diff jsou jasně určené.
- Thread v aktuálním workspace musí jasně ukázat, z jaké větve PR vzniká, aby nezahrnul cizí lokální změny.
- Pokud jsou změny necommitnuté, cockpit je nesmí tiše commitnout. Vrátí uživatele do threadu/terminálu s vysvětlením.
- Úklid worktree zůstává samostatná explicitní akce až po merge nebo uzavření PR.

## Fázování

### Fáze 1 — lokální příprava

Diff review, strukturovaný editovatelný návrh popisu, export/zkopírování textu a propojení s threadem/checkpointem. Nevyžaduje účet ani síť.

### Fáze 2 — vytvoření PR

Podpora jedné forge, nejspíše GitHub přes `gh`: ověření přihlášení, explicitní push, vytvoření draft/ready PR a uložení odkazu do threadu.

### Fáze 3 — život po otevření PR

Obnova CI/review stavu, notifikace a převod vybraného review feedbacku na follow-up threadu.

## Mimo rozsah V1

- Kompletní GitHub klient v Burrowu.
- Automatický merge, force-push nebo automatická oprava review komentářů.
- Povinné napojení na jednu forge.
- Tiché commitování/pushování uživatelských změn.

## Rozhodnutí k potvrzení

1. Má Fáze 1 pracovat jen s checkpointem, nebo i s běžným pracovním diffem?
2. Je GitHub přes `gh` správná první integrace, nebo má být forge vrstva od začátku abstraktní?
3. Má být PR karta dostupná jen v detailu threadu, nebo i v agregované review frontě projektu?
4. Má Burrow po merge nabídnout úklid worktree a lokální branche, vždy se samostatným potvrzením?

## Kritéria úspěchu

- Uživatel připraví srozumitelné PR z hotového worktree threadu bez ztráty kontextu.
- Návrh PR je editovatelný a transparentní ve svých zdrojích.
- Review feedback se rychle vrátí do původního threadu.
- Žádná síťová ani destruktivní Git akce neproběhne bez explicitního potvrzení.

# TUI

`total-recall tui` öffnet das Archiv in einer Vollbild-Oberfläche im Terminal: Volltextsuche mit Vorschau beim Tippen, eine Detailansicht je Session, eine Statistik der Aktivität als Chart, Tabelle und Heatmap und die erste und letzte Aktivität je Tag. Die Datenbank wird nur lesend geöffnet.

!!! note "Spike"
    Die TUI ist das Ergebnis eines Spikes. Sie ist als eigenständiges Unterkommando gebaut, das sich wieder entfernen lässt. Entwurf, Abweichungen, Messwerte und Empfehlung stehen in [Spike: TUI](spike-tui.md#ergebnis). Die Oberfläche ist Englisch wie die übrige CLI-Ausgabe; alle Bildschirme auf dieser Seite sind erfundene Beispiele.

## Aufruf

```sh
total-recall tui                          # startet in der Suche
total-recall tui stats                    # startet in der Statistik
total-recall tui activity                 # startet in der Aktivität je Tag
total-recall tui --query "goreleaser"     # Suche mit vorbelegtem Suchtext
total-recall tui stats --graphics text    # Chart als Text, kein Bild
```

| Argument / Option | Bedeutung |
|---|---|
| `search` \| `stats` \| `activity` | Ansicht beim Start, Default `search` |
| `--query TEXT` | Suchtext vorbelegen; die Suche läuft sofort beim Start |
| `--graphics auto\|text\|kitty` | Chart-Darstellung in der Statistik, Default `auto`; siehe [Grafikmodi](#grafikmodi) |
| `--no-mouse` | Maus dem Terminal überlassen: kein Mausrad, keine Klicks, dafür Textauswahl durch einfaches Ziehen; siehe [Maus](#maus) |

Die [globalen Optionen](befehle.md#globale-optionen) `--db`, `--tz` und `--config` gelten wie überall; `--tz` bzw. `timezone` aus der Config bestimmt Tages-, Wochen- und Monatsgrenzen, `day_start` die Tagesgrenze der Aktivitätsansicht. Beenden mit `q` oder ++ctrl+c++ (++ctrl+c++ immer, `q` nur, solange kein Eingabefeld den Fokus hat).

Die Oberfläche braucht mindestens 80×24 Zeichen; die Beispiele hier sind 100 Zeichen breit. Bei weniger Platz werden Fußzeile und Tabellen abgeschnitten (`…`), die Heatmap wechselt in ein kompaktes Layout und blendet sich darunter aus.

## Aufbau

Alle Ansichten teilen denselben Rahmen:

- **Kopfzeile:** links der Programmname, rechts die Ansichten als Tabs. Die aktive steht in eckigen Klammern (`[1 search]  2 stats  3 activity`), damit sie auch ohne Farbe erkennbar ist. Ist eine Session geöffnet, hängt ihr Name an der Suche: `[1 search] › session`.
- **Fußzeile:** die wichtigsten Tasten der aktuellen Ansicht, dann die globalen (`1 search • 2 stats • 3 activity • ? help • q quit`). Hat ein Eingabefeld den Fokus, steht dort nur noch `ctrl+c quit`, weil alle anderen Tasten in das Feld gehen.
- **Hilfe:** `?` legt ein Overlay mit allen Tasten der aktuellen Ansicht und den globalen Tasten über den Inhalt. ++esc++ oder `?` schließt es. Das Overlay wird aus derselben Tastenbelegung erzeugt wie die Fußzeile.
- **Maus:** ein Klick auf einen Tab in der Kopfzeile wechselt die Ansicht, das Mausrad blättert im Inhalt; siehe [Maus](#maus).

```text
 total-recall                                                                   1 search  [2 stats]
────────────────────────────────────────────────────────────────────────────────────────────────────

                ╭──────────────────────────────────────────────────────────────────╮
                │ keys: stats                                                      │
                │                                                                  │
                │ d days      p by project      [ earlier     1 search    ?   help │
                │ w weeks     a by agent        ] later       2 stats     esc back │
                │ m months    v by directory    - zoom out                q   quit │
                │ e metric    h heatmap         + zoom in                          │
                │                               g graphics                         │
                │                                                                  │
                │ esc or ? closes this help                                        │
                ╰──────────────────────────────────────────────────────────────────╯

────────────────────────────────────────────────────────────────────────────────────────────────────
 esc back • q quit
```

## Maus

Die TUI fordert Mausmeldungen vom Terminal an (Cell Motion: Klicks und Rad, keine Bewegung). Damit gilt überall:

| Maus | Wirkung |
|---|---|
| Rad | blättert, was unter dem Zeiger liegt: die Trefferliste (bewegt die Markierung wie ++up++/++down++), die Vorschau, den Verlauf einer Session, die Tage der Aktivität |
| Klick auf einen Tab in der Kopfzeile | wechselt die Ansicht wie `1`, `2`, `3`; schließt eine offene Session |
| Klick auf einen Treffer | markiert ihn und gibt der Liste den Fokus; ein zweiter Klick auf den markierten Treffer öffnet die Session wie ++enter++ |
| Klick auf die Eingabezeile der Suche | gibt der Eingabe den Fokus |

Klicks anderswo (Vorschau, Statistik, Aktivität, Fußzeile, Hilfe) tun nichts. Das Mausrad in der Vorschau verschiebt den Austausch zeilenweise; mit dem nächsten Treffer springt die Vorschau wieder an die automatische Position mit dem Treffer im oberen Drittel.

**Textauswahl:** Solange die Maus an das Programm geht, wählt einfaches Ziehen keinen Text mehr aus. Die Terminals erlauben die Auswahl trotzdem mit einer Zusatztaste: ++shift++ und Ziehen in Ghostty und kitty, ++option++ und Ziehen in Terminal.app und iTerm2. In herdr und tmux kommen die Mausmeldungen beim Programm an, die Auswahl mit Zusatztaste geht an das äußere Terminal. Wer ganz ohne Maus arbeiten will, startet mit `--no-mouse`; dann bleibt die Maus beim Terminal und die Bedienung ist rein über die Tastatur. Eine Einstellung in der Konfiguration folgt mit dem Abschnitt `[tui]` ([#49](https://github.com/seletz/total-recall/issues/49)).

## Suche

```text
 total-recall                                                                   [1 search]  2 stats
────────────────────────────────────────────────────────────────────────────────────────────────────
 > goreleaser cask                                                   actor: all  since: 90d  repo: *
─────────────────────────────────────────────────┬──────────────────────────────────────────────────
▸2026-10-31 22:14  Release v0.1.0           human│ Release v0.1.0
  Please also upload the cask to the homebrew ta…│ claude:9a1b2c3d-0000-4000-8…  seletz/total-recall
 2026-10-31 22:16  Release v0.1.0           agent│ 2026-10-31 22:10 – 23:02 (52m)  develop
  goreleaser skips the cask upload when the tap …│──────────────────────────────────────────────────
 2026-10-28 09:02  CI pipeline              human│── 22:14  human ◄
  can we build the cask in goreleaser and push i…│Please also upload the cask to the homebrew tap
 2026-10-28 09:05  CI pipeline              agent│with goreleaser, the token is in the repo secrets.
  goreleaser needs HOMEBREW_TAP_GITHUB_TOKEN in …│
 2026-10-12 15:40  Packaging                agent│── 22:16  agent
  copied .goreleaser.yml from the notes project …│goreleaser skips the cask upload when the tap
                                                 │token is missing. I added a check that fails the
                                                 │release early instead.
                                                 │
 5 hits (12 ms)
────────────────────────────────────────────────────────────────────────────────────────────────────
 ↑↓ move • enter open • a actor • t time • r repo • ^r raw fts • tab/esc input • 1 search • 2 stats…
```

- **Eingabe** oben, mit dem Fokus beim Start. Jede Änderung startet nach 150 ms Ruhe eine Suche wie `search`: ohne Rohsyntax wird jedes Wort in Anführungszeichen gesetzt, alle Wörter müssen vorkommen. Höchstens 200 Treffer werden geholt; die Statuszeile sagt `200 hits, limit reached`, wenn es mehr gäbe. Rechts in der Eingabezeile stehen die aktiven Filter.
- **Trefferliste** links, zwei Zeilen je Treffer: Zeit, Session-Name und Actor, darunter der Textausschnitt mit hervorgehobenen Suchwörtern. Sortierung wie `search`: Rang, dann Zeit. Der markierte Treffer trägt `▸`; hat die Liste den Fokus, ist er zusätzlich invertiert.
- **Vorschau** rechts, folgt der Markierung (nach 100 ms Ruhe): der Session-Kopf (Name, ID und Repo, Zeitraum und Branch), dann der Austausch um den Treffer, also die menschliche Nachricht davor und die Antwort des Agenten danach. Der Treffer-Event ist mit `◄` markiert, die Suchwörter sind unterstrichen. Trifft die Suche einen Tool-Aufruf, lädt die Vorschau den vollen Verlauf inklusive Tools.
- **Fokus:** ++tab++ wechselt zwischen Eingabe und Liste, siehe [Fokus](#fokus). In der Liste bewegen ++up++/++down++ und `k`/`j`, ++enter++ öffnet die markierte Session (auch direkt aus der Eingabe). Mit der [Maus](#maus): das Rad über der Liste bewegt die Markierung, ein Klick markiert den Treffer, ein zweiter Klick öffnet ihn.
- **Statuszeile:** Trefferzahl und Dauer der Abfrage (`5 hits (12 ms)`), sonst `type to search`, `searching…`, `no hits` oder die Fehlermeldung.

### Fokus

Innerhalb der Suche können zwei Elemente den Fokus haben: die **Eingabe** und die **Trefferliste**. Die Vorschau ist reine Anzeige und folgt der Markierung. Der Repo-Filter ist ein modales Feld, das `r` aus der Liste öffnet; er gehört nicht zur Reihenfolge.

- ++tab++ und ++shift+tab++ wechseln zwischen Eingabe und Liste, in beide Richtungen, und bleiben dabei in der Suche. Die Ansichten wechseln nur `1`, `2` und `3`; ++tab++ ist dafür nicht belegt.
- ++esc++ geht eine Ebene zurück: aus dem Repo-Filter in die Liste (die Änderung wird verworfen), aus der Liste in die Eingabe. In der Eingabe tut ++esc++ nichts.
- Hat die **Eingabe** den Fokus, sind alle Tasten Suchtext. Ausnahmen sind nur ++tab++/++shift+tab++ (Fokus), ++enter++ (markierten Treffer öffnen), ++ctrl+r++ (Rohsyntax) und ++ctrl+c++ (beenden). `a`, `t`, `r`, `q`, `?`, `1`, `2` und `3` landen im Suchtext und lösen nichts aus.
- Hat die **Liste** den Fokus, gelten ihre Tasten (bewegen, öffnen, Filter) und die globalen Tasten der App: `1`, `2`, `3`, `?`, `q`.
- Im **Repo-Filter** übernehmen ++enter++ und ++tab++ den Wert, ++esc++ verwirft ihn; alles andere ist Text.
- Die **Maus** folgt derselben Ordnung: ein Klick auf einen Treffer gibt der Liste den Fokus, ein Klick auf die Eingabezeile der Eingabe; das Rad ändert den Fokus nicht. Solange der Repo-Filter offen ist, tut die Maus nichts.
- Sichtbar ist der Fokus am Cursor und an der Markierung: in der Eingabe und im Repo-Filter steht der Cursor des Terminals im Feld; hat die Liste den Fokus, ist der markierte Treffer invertiert. Das `▸` vor dem markierten Treffer bleibt in jedem Fall, weil ++enter++ ihn aus beiden Elementen öffnet und die Vorschau ihn zeigt. Die Fußzeile nennt die Tasten des fokussierten Elements.

Der Fokus bleibt beim Wechsel der Ansicht und beim Öffnen einer Session erhalten; ++esc++ aus der Session führt dorthin zurück, wo man war.

### Filter

Die Filter werden in der **Liste** umgeschaltet (in der Eingabe wären die Buchstaben Suchtext). Jede Änderung sucht sofort neu.

| Taste | Filter | Werte |
|---|---|---|
| `a` | Actor | `all` → `human` → `agent` → `peer` → `all` |
| `t` | Zeitraum (Zeit des Events) | `all` → `7d` → `30d` → `90d` → `all` |
| `r` | Repo | Teilstring von Remote-URL, Git-Root oder Arbeitsverzeichnis. Öffnet ein Eingabefeld in der Statuszeile: ++enter++ oder ++tab++ übernimmt, ++esc++ verwirft. Leer löscht den Filter |
| ++ctrl+r++ | Rohsyntax | FTS5-Syntax unverändert durchreichen (`AND`, `OR`, `NOT`, `"Phrase"`, `präfix*`), wie `search --raw`. Geht auch aus der Eingabe |

Die Hervorhebung in Vorschau und Detailansicht nimmt die Wörter der Eingabe; in Rohsyntax ohne Operatoren, Anführungszeichen und Sterne.

## Session

++enter++ auf einem Treffer öffnet die Session als Ebene über der Suche. Die Suche bleibt darunter erhalten: ++esc++ führt mit Eingabe, Treffern, Markierung und Fokus zurück. `1` und `2` schließen die Detailansicht ebenfalls.

```text
 total-recall                                                         [1 search] › session  2 stats
────────────────────────────────────────────────────────────────────────────────────────────────────
 Release v0.1.0                                         claude:9a1b2c3d-0000-4000-8000-000000000000
 seletz/total-recall  develop  2026-10-31 22:10 – 23:02 (52m)  3 human  14 agent
── 3/17 events · hit 1/4 · tools ───────────────────────────────────────────────────────────────────
── 2026-10-31 22:14:02  human ◄
Please also upload the cask to the homebrew tap with goreleaser, the token is in the repo
secrets.

── 2026-10-31 22:14:30  tool_use Read
.goreleaser.yml

── 2026-10-31 22:16:11  agent
goreleaser skips the cask upload when the tap token is missing. I added a check that fails the
release early instead.

── 2026-10-31 22:17:40  human
Good. Tag v0.1.0 and run the release.

────────────────────────────────────────────────────────────────────────────────────────────────────
 ↑↓ scroll • n/N next/prev hit • T tools on/off • y copy session id • esc back • 1 search • …
```

- **Kopf:** Name und volle Session-ID, darunter Repo, Branch, Zeitraum mit Dauer, Anzahl menschlicher und Agenten-Events, bei Subagent-Sessions die Eltern-ID. In schmalen Fenstern gewinnt der Name, die ID wird gekürzt.
- **Trennlinie mit Status:** der oberste sichtbare Event (`3/17 events`), der gewählte Treffer (`hit 1/4`), `tools`, wenn Tool-Events eingeblendet sind, und kurz `copied` nach dem Kopieren.
- **Verlauf** wie `show`: eine Trennzeile je Event mit Zeit und Actor, Tool-Aufrufe mit dem Werkzeugnamen, Text am Wortende umbrochen. Die Suchwörter sind unterstrichen, der gewählte Treffer ist invertiert, der markierte Event trägt `◄`. Beim Öffnen steht der Treffer-Event oben; ist er ein Tool-Event, werden Tools automatisch eingeblendet.
- **Blättern** mit ++up++/++down++, ++pgup++/++pgdn++ oder dem Mausrad (drei Zeilen je Schritt).
- `n`/`N` springen zum nächsten/vorigen Treffer, am Ende wieder von vorn. `T` blendet Tool-Aufrufe, Ergebnisse und Automatik ein und aus (wie `show --all`); die Position bleibt am markierten Event. `y` kopiert die Session-ID über OSC 52 in die Zwischenablage, für `show`, `sql` oder Agenten.

## Statistik

```text
 total-recall                                                                   1 search  [2 stats]
────────────────────────────────────────────────────────────────────────────────────────────────────
 period: [day] week month  by: [project] agent directory                     2026-10-05 – 2026-11-01
 events
  4000 ┤                                                    ··
       │                                                 ·· ░░             ·· ··
  3000 ┤                                              ·· ░░ ▒▒ ··          ░░ ░░ ··
       │                            ·· ··             ░░ ▒▒ ▓▓ ░░       ·· ▒▒ ▒▒ ░░
  2000 ┤                         ▓▓ ▓▓ ▓▓ ▓▓       ▓▓ ▒▒ ▓▓ ▓▓ ▒▒       ▓▓ ▓▓ ▓▓ ▒▒
       │       ▓▓             ▓▓ ██ ▓▓ ▓▓ ██       ██ ▓▓ ▓▓ ██ ▓▓       ▓▓ ▓▓ ▓▓ ▓▓ ▓▓
  1000 ┤    ▓▓ ██ ▓▓          ██ ██ ██ ██ ██       ██ ▓▓ ██ ██ ▓▓       ██ ██ ██ ▓▓ ██
       │ ██ ██ ██ ██ ██       ██ ██ ██ ██ ██       ██ ██ ██ ██ ██ ██ ██ ██ ██ ██ ██ ██ ██
     0 ┼────────────────────────────────────────────────────────────────────────────────────
        05 06 07 08 09 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 01
 ██ webshop  ▓▓ infra  ▒▒ total-recall  ░░ notes  ·· other
────────────────────────────────────────────────────────────────────────────────────────────────────
 project                                     sessions    session-days      human      agent    share
 webshop                                           31              56      1 204     21 870     37 %
 infra                                             34              56        980     18 310     31 %
 total-recall                                      12              21        310      6 120     10 %
 notes                                              9              14        402      5 990     10 %
 other                                             30              42        515      7 212     12 %
 text chart (kitty: not detected) · 28 days · 312 ms
────────────────────────────────────────────────────────────────────────────────────────────────────
 d/w/m period • p/a/v by • [ ] -/+ range • 1 search • 2 stats • ? help • q quit
```

- **Umschalter** oben: Periode (`d` Tage, `w` Wochen, `m` Monate) und Dimension (`p` Projekt, `a` Agent, `v` Verzeichnis), der aktive Wert in Klammern. Rechts der gezeigte Zeitraum.
- **Zeitfenster:** Standard sind die letzten 28 Tage, 16 Wochen oder 12 Monate bis heute. `[` und `]` verschieben um ein ganzes Fenster, `-` und `+` halbieren bzw. verdoppeln die Anzahl Perioden (mindestens eine, höchstens das Achtfache des Standardfensters). Ein Periodenwechsel springt auf das Standardfenster, das die zuletzt gezeigte Periode enthält.
- **Kennzahl:** `e` wechselt zwischen `events` (menschliche plus Agenten-Events), `human` und `sessions`. Der Titel über dem Chart nennt sie.
- **Chart:** eine gestapelte Säule je Periode, Segmente sind die sechs größten Werte der Dimension über das ganze Fenster plus `other`. Die Legende steht darunter; die Farben bleiben beim Umschalten stabil. Im Terminal unterscheiden Farben die Segmente, oben sind sie als Schraffur gezeigt. Perioden ohne Aktivität bleiben leer, die Achse ist lückenlos.
- **Tabelle:** dieselben Reihen mit zwei Session-Zahlen, menschlichen und Agenten-Events und Anteil an allen Events. `sessions` zählt die eindeutigen Sessions im ganzen Zeitfenster, jede einmal. `session-days`, `session-weeks` bzw. `session-months` (je nach Periode) summiert die Sessions je Periode, so wie das Chart sie zeigt: eine Session, die an drei Tagen aktiv war, zählt bei Tagen dreimal. Die Differenz der beiden Spalten zeigt, wie viele Sessions über Periodengrenzen liefen.
- **Statuszeile:** Grafikmodus, Anzahl Perioden, Dauer der Abfrage, Zustand der Heatmap. Ergebnisse werden je Kombination gecacht; Zurückschalten ist sofort.

Die Dimensionen:

| Dimension | Schlüssel | Anzeige |
|---|---|---|
| `project` | Git-Root der Session, sonst ihr Arbeitsverzeichnis; Subagent-Sessions zählen zur Eltern-Session, Worktrees zum Hauptrepo | Basisname des Pfads. Gleiche Basisnamen bekommen das Elternverzeichnis dazu (`a/tools`, `b/tools`) |
| `agent` | `claude main` (Session ohne Eltern) oder `claude subagent` | unverändert |
| `directory` | Arbeitsverzeichnis der Session, bei Subagents das der Eltern-Session | voller Pfad |

### Heatmap

`h` blendet unter dem Chart die Heatmap Wochentag × Stunde für das Zeitfenster ein (Daten wie `stats`, Kennzahl wie das Chart). Je dunkler die Zelle, desto näher am Maximum, das der Titel nennt; leere Zellen bleiben frei.

```text
 events by weekday × hour · max 412
 Mon│                        ░░░▒▒▒▓▓▓███▓▓▓▒▒▒▓▓▓▓▓▓▒▒▒░░░         ░░░
 Tue│                     ░░░▒▒▒▓▓▓▓▓▓▒▒▒   ▓▓▓███▓▓▓▒▒▒░░░
 Wed│                           ▒▒▒▓▓▓▓▓▓   ▒▒▒▒▒▒▓▓▓▒▒▒         ░░░▒▒▒░░░
 Thu│                        ░░░▒▒▒▒▒▒▓▓▓      ▓▓▓███▓▓▓▒▒▒░░░
 Fri│                           ░░░▒▒▒▒▒▒░░░   ▒▒▒▒▒▒░░░
 Sat│                              ░░░░░░         ░░░▒▒▒░░░
 Sun│                                                ░░░░░░      ▒▒▒░░░
    ┼────────────────────────────────────────────────────────────────────────
     00 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15 16 17 18 19 20 21 22 23
```

Die Heatmap braucht zehn Zeilen. Bleibt dem Chart daneben weniger als seine Mindesthöhe von acht Zeilen, wechselt sie in ein **kompaktes Layout** mit neun Zeilen: die Stundenbeschriftung rückt in die Achsenzeile, die Legende der Schattierungen von leer bis Maximum (`░▒▓█`) steht im Titel. Bei 80×24 passen so Chart und Heatmap untereinander; die Tabelle bleibt dafür ausgeblendet. In beiden Layouts werden die Zellen schmaler, bis jede Stunde eine Spalte hat, dann steht nur noch jede zweite Stunde dran.

```text
 events by weekday × hour · ░▒▓█ · max 412
 Mon│                        ░░░▒▒▒▓▓▓███▓▓▓▒▒▒▓▓▓▓▓▓▒▒▒░░░         ░░░
 Tue│                     ░░░▒▒▒▓▓▓▓▓▓▒▒▒   ▓▓▓███▓▓▓▒▒▒░░░
 Wed│                           ▒▒▒▓▓▓▓▓▓   ▒▒▒▒▒▒▓▓▓▒▒▒         ░░░▒▒▒░░░
 Thu│                        ░░░▒▒▒▒▒▒▓▓▓      ▓▓▓███▓▓▓▒▒▒░░░
 Fri│                           ░░░▒▒▒▒▒▒░░░   ▒▒▒▒▒▒░░░
 Sat│                              ░░░░░░         ░░░▒▒▒░░░
 Sun│                                                ░░░░░░      ▒▒▒░░░
    ╵00 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15 16 17 18 19 20 21 22 23
```

Reichen auch neun Zeilen nicht (Terminal unter 24 Zeilen), bleibt die Heatmap ausgeblendet und die Statuszeile sagt `heatmap needs more rows`.

## Aktivität

Die dritte Ansicht (`3`, `total-recall tui activity`) zeigt je Tag die **erste und letzte Aktivität**: ein Notnagel für vergessenes Ein- und Ausstempeln. Dieselben Daten liefert [`stats --workday`](befehle.md#stats-workday) als Text und JSON.

```text
 total-recall                                                      1 search   2 stats  [3 activity]
────────────────────────────────────────────────────────────────────────────────────────────────────
 period: day [week] month  count: [all] human        this week · 2026-W44 · 2026-10-26 – 2026-11-01
 day             first  last          span     diff  04     08      12      16     20      00
 Mon 2026-10-26  08:12  18:40       10h28m   +2h28m  ········▓▓███████████████▓▓▓▓·················
 Tue 2026-10-27  09:03  01:30 (+1)  16h27m   +8h27m  ·········████████████████▓▓▓▓▓▓▓▓▓▓▓▓▓▒▒▒▒····
 Wed 2026-10-28  -      -                -        -  ·········│──────────────│·····················
 Thu 2026-10-29  07:55  17:10        9h15m   +1h15m  ·······▓▓▓███████████████▓····················
 Fri 2026-10-30  10:20  15:45        5h25m   -2h35m  ·········│──███████████─│·····················
 Sat 2026-10-31  -      -                -        -  ··············································
 Sun 2026-11-01  21:00  21:05           5m   +0h05m  ································█·············

────────────────────────────────────────────────────────────────────────────────────────────────────
 total: 5 days with activity, 41h40m · target 32h00m (4 workdays × 8h00m) · balance +9h40m
 7 days · 4 ms
```

- **Umschalter** oben: Zeitfenster (`d` ein Tag, `w` eine ISO-Woche ab Montag, `m` ein Monat) und was zählt (`all`: eigene Nachrichten und Agent-Events wie in der Statistik; `h` schaltet auf `human`, nur eigene Nachrichten). Rechts das Fenster: `this week`/`last week` (bzw. `today`/`yesterday`, `this month`/`last month`), Kalenderwoche oder Monat und der erste und letzte Tag.
- **Zeitfenster:** Start ist die aktuelle Woche. `[` und `]` verschieben um einen Tag, eine Woche oder einen Monat. `c` springt in die aktuelle Periode (heute, diese Woche, dieser Monat), `l` in die vorige (gestern, letzte Woche, letzter Monat). Ein Periodenwechsel behält den zuletzt gezeigten Tag bei: aus der letzten Woche führt `m` in den Monat, der ihren letzten Tag enthält.
- **Zeilen:** je Tag Wochentag und Datum, erste und letzte Aktivität als Uhrzeit, die Spanne dazwischen, die Differenz zum Tagessoll und ein Balken auf einer Zeitachse. Tage ohne Aktivität stehen mit `-` und leerem Balken da, damit Lücken auffallen.
- **Normalarbeitszeit:** Kernzeit und Tagessoll kommen aus `[work]` in der [Konfiguration](konfiguration.md#work), Standard 09:00–17:00 und 8 h. Die Kernzeit liegt als Band mit Linien an Anfang und Ende auf der Achse jedes Werktags (`│───│`, farbig hinterlegt). Der Balken ist innerhalb der Kernzeit blau (`█`), davor und danach orange (`▓`), nach Mitternacht rot (`▒`); am Wochenende violett ohne Band. Ohne Farben (`NO_COLOR`) unterscheiden die Zeichen.
- **Spanne und Differenz:** Die Spanne eines Werktags wird gegen `daily_low`/`daily_high` bewertet und mit der Differenz zum Soll (`+1h15m`, `-2h35m`) gefärbt: türkis unter, grün im Rahmen, orange deutlich über Soll. Das Wochenende hat kein Soll, seine ganze Spanne ist die Differenz; ein Tag ohne Aktivität hat keine (`-`) und zählt nicht als Arbeitstag, also auch Urlaub oder Feiertag nicht.
- **Tagesgrenze:** Ein Tag beginnt um `day_start` aus der [Konfiguration](konfiguration.md#day_start), Standard 04:00 Ortszeit. Aktivität davor zählt zum Vortag und wird als `01:30 (+1)` angezeigt; die Zeitachse läuft entsprechend von 04 bis 04 Uhr. Die Spanne ist die tatsächlich verstrichene Zeit, auch über eine Zeitumstellung hinweg.
- **Summenzeile** bei Woche und Monat: Anzahl Tage mit Aktivität, Summe der Spannen, das Soll (Tagessoll × Werktage mit Aktivität) und der Saldo dagegen, gefärbt mit der um die Werktage skalierten Tagestoleranz. Ein einzelner Tag braucht sie nicht.
- **Farben** folgen dem Terminalhintergrund (hell oder dunkel, Abfrage beim Start) und kommen aus der Palette der Statistik-Charts.
- **Statuszeile:** Anzahl Tage und Dauer der Abfrage. Passt ein Monat nicht in die Höhe, zeigt sie die sichtbaren Zeilen (`rows 1–21 of 31`); ++up++/++down++ bzw. `k`/`j` und das Mausrad blättern. Ergebnisse werden je Fenster und Zählweise gecacht.

Der Balken ist immer Text (Blockzeichen), unabhängig vom [Grafikmodus](#grafikmodi); bei weniger als etwa 65 Spalten entfällt er, die Uhrzeiten, Spanne und Differenz bleiben.

## Tasten

### Überall

| Taste | Wirkung |
|---|---|
| `1` / `2` / `3` | Suche / Statistik / Aktivität; schließt eine offene Session |
| ++tab++ / ++shift+tab++ | Fokus innerhalb der Ansicht wechseln: in der Suche Eingabe ↔ Liste. Wechselt nie die Ansicht |
| `?` | Hilfe ein/aus |
| ++esc++ | eine Ebene zurück: Session → Suche, Hilfe schließen, Liste → Eingabe, Repo-Filter abbrechen |
| `q` | beenden (nicht, solange Eingabe oder Repo-Filter den Fokus haben) |
| ++ctrl+c++ | beenden, immer |
| Maus | Rad blättert, Klick auf einen Tab wechselt die Ansicht; siehe [Maus](#maus) |

### Suche

| Taste | Eingabe hat den Fokus | Liste hat den Fokus |
|---|---|---|
| ++tab++ / ++shift+tab++ | in die Liste | in die Eingabe |
| ++esc++ | – | in die Eingabe |
| ++enter++ | markierten Treffer öffnen | markierten Treffer öffnen |
| ++up++ / `k`, ++down++ / `j` | – | Markierung bewegen |
| `a`, `t`, `r` | Suchtext | Filter Actor, Zeitraum, Repo |
| ++ctrl+r++ | Rohsyntax ein/aus | Rohsyntax ein/aus |

Im Repo-Filter: ++enter++ oder ++tab++ übernimmt, ++esc++ verwirft; siehe [Fokus](#fokus).

### Session

| Taste | Wirkung |
|---|---|
| ++up++ / `k`, ++down++ / `j` | eine Zeile |
| `b` / ++pgup++, `f` / ++pgdn++ / ++space++ | eine Seite |
| `u` / ++ctrl+u++, `d` / ++ctrl+d++ | eine halbe Seite |
| `g` / ++home++, `G` / ++end++ | Anfang / Ende |
| `n` / `N` | nächster / voriger Treffer |
| `T` | Tool-Events ein/aus |
| `y` | Session-ID kopieren |
| ++esc++ | zurück zur Suche |

### Statistik

| Taste | Wirkung |
|---|---|
| `d` / `w` / `m` | Periode Tag / Woche / Monat |
| `p` / `a` / `v` | Dimension Projekt / Agent / Verzeichnis |
| `[` / `]` | ein Fenster zurück / vor |
| `-` / `+` | Fenster halbieren / verdoppeln (`=` wie `+`) |
| `e` | Kennzahl: events → human → sessions |
| `h` | Heatmap ein/aus |
| `g` | Grafikmodus: auto → text → kitty → auto |

### Aktivität

| Taste | Wirkung |
|---|---|
| `d` / `w` / `m` | Fenster ein Tag / eine Woche / ein Monat |
| `[` / `]` | eine Periode zurück / vor |
| `c` | heute / diese Woche / dieser Monat |
| `l` | gestern / letzte Woche / letzter Monat |
| `h` | nur eigene Nachrichten zählen ein/aus |
| ++up++ / `k`, ++down++ / `j` | blättern, wenn nicht alle Tage passen |

## Grafikmodi

Das Chart der Statistik gibt es als Text (Blockzeichen, überall) und als Bild über das Kitty-Graphics-Protokoll. Umschalter, Legende, Tabelle und Heatmap bleiben in jedem Fall Text.

| Modus | Verhalten |
|---|---|
| `auto` (Default) | Beim Start wird das Terminal einmal sondiert. Ein Bild gibt es nur, wenn die Sondierung beantwortet wird **und** die Umgebung ein Terminal nennt, das Bilder über Unicode-Platzhalter darstellen kann: Ghostty, kitty oder herdr. Bis dahin und sonst: Text. |
| `text` | Nie sondieren, nie ein Bild. Es geht keine Abfrage ans Terminal. |
| `kitty` | Bild erzwingen, ohne Sondierung. Für Terminals, die die Erkennung nicht überstehen (etwa hinter ssh oder tmux), zeigt in anderen Terminals aber nur Platzhalterzeichen. |

`g` wechselt zur Laufzeit `auto` → `text` → `kitty` → `auto`. Die Statuszeile nennt den Modus und bei `auto` den Grund, wenn kein Bild gezeigt wird:

| Statuszeile | Bedeutung |
|---|---|
| `image chart (kitty)` | Bild wird gezeigt |
| `image chart (kitty, forced)` | Bild erzwungen (`--graphics kitty` oder `g`) |
| `text chart` | Modus `text` |
| `text chart (kitty: probing…)` | Sondierung läuft |
| `text chart (kitty: rendering…)` | Bild entschieden, aber noch nicht platziert |
| `text chart (kitty: not detected)` | Umgebung nennt kein Kitty-fähiges Terminal; es wurde nichts gesendet |
| `text chart (kitty: no answer)` | Abfrage ohne Antwort innerhalb von 250 ms |
| `text chart (kitty: no answer; tmux allow-passthrough?)` | dasselbe in tmux: meist fehlt `allow-passthrough` |
| `text chart (kitty: no answer; herdr kitty_graphics off?)` | dasselbe in herdr: `terminal.kitty_graphics` ist aus oder das äußere Terminal kann keine Kitty Graphics |
| `text chart (kitty: outer terminal unknown)` | tmux hat geantwortet, aber nichts weist auf Ghostty hin |
| `text chart (kitty: no placeholders in …)` | das Terminal antwortet, kann aber keine Unicode-Platzhalter (WezTerm, Konsole) |
| `text chart (kitty: ghostty 1.3.x in tmux needs 1.4)` | Ghostty vor 1.4 zeigt Platzhalter in tmux fehlerhaft |
| `text chart (kitty: not probed)` | zurück auf `auto`, aber die Sondierung kann in diesem Prozess nicht mehr laufen |
| `text chart (kitty: render error)` | das Bild konnte nicht gerendert werden |

### Voraussetzungen

| Umgebung | Ergebnis |
|---|---|
| Ghostty ≥ 1.0, nativ | Bild |
| kitty | Bild |
| Ghostty in tmux ≥ 3.3 | Bild, wenn `allow-passthrough` an ist, der tmux-Server aus Ghostty gestartet wurde und Ghostty ≥ 1.4 ist; sonst Text mit Grund |
| herdr in Ghostty | Bild, wenn herdr die Sondierung beantwortet: `terminal.kitty_graphics` an und ein äußeres Terminal mit Kitty Graphics; sonst Text mit Grund |
| tmux ohne Passthrough, Zellij | Text, Hinweis in der Statuszeile |
| WezTerm, Konsole, Warp | Text: sie beantworten die Sondierung, zeigen Platzhalter aber als Zeichensalat |
| Terminal.app, iTerm2, Alacritty, Windows Terminal | Text |
| ssh | Bild, wenn die entfernte Shell `TERM=xterm-ghostty` sieht (ssh reicht `TERM` standardmäßig durch) und `COLORTERM=truecolor` gesetzt ist; sonst Text, `g` `g` erzwingt |

**tmux.** Die Bildsequenzen werden in DCS-Passthrough verpackt; dafür muss tmux ≥ 3.3 sie durchlassen:

```text
# ~/.tmux.conf
set -g allow-passthrough on
```

tmux ersetzt in seinen Panes `TERM` und `TERM_PROGRAM`, daher erkennt die TUI Ghostty dort an `GHOSTTY_RESOURCES_DIR` bzw. `GHOSTTY_BIN_DIR`. Die Variablen hat nur ein tmux-Server, der aus Ghostty heraus gestartet wurde (`tmux show-environment -g | grep GHOSTTY`). Fehlen sie, meldet die Statuszeile `outer terminal unknown`; dann hilft ein Neustart des tmux-Servers aus Ghostty, die Variable beim Aufruf (`GHOSTTY_RESOURCES_DIR=1 total-recall tui stats`) oder `g` `g` zum Erzwingen.

**herdr.** herdr setzt in seinen Panes `TERM_PROGRAM=herdr` und `TERM=xterm-256color` und reicht die Ghostty-Variablen nicht durch. Die Chart-Bibliothek würde dort gar nicht sondieren, deshalb schickt die TUI die Kitty-Abfrage (`a=q`) selbst und wartet 250 ms. herdr zeichnet Platzhalter selbst und reicht das Bild an das äußere Terminal weiter; es antwortet nur, solange `terminal.kitty_graphics` an ist und das äußere Terminal Kitty Graphics kann. Bleibt die Antwort aus, meldet die Statuszeile `no answer; herdr kitty_graphics off?`, `g` `g` erzwingt. `NTCHARTS_KITTY=supported|unsupported` gilt auch in herdr und ersetzt die Abfrage.

**ssh.** Die Sondierung läuft durch ssh hindurch, die Umgebungsvariablen nicht alle: `TERM` kommt an, `TERM_PROGRAM` und `COLORTERM` normalerweise nicht. Ohne Truecolor gehen die Platzhalter kaputt (die Bild-ID steckt in der Vordergrundfarbe), also auf der Gegenseite `COLORTERM=truecolor` setzen oder per `SendEnv`/`AcceptEnv` durchreichen.

**Wie das Bild entsteht.** Das Chart wird außerhalb der Hauptschleife als PNG gerendert, einmal je Änderung an das Terminal übertragen und über Unicode-Platzhalterzellen platziert; der Hintergrund ist transparent, Terminalfarben scheinen durch. Die Bildgröße folgt der Zellenzahl des Chart-Bereichs mal Zellgröße in Pixeln, die das Terminal auf Anfrage meldet (Rückfall 10×20 px). Ein Wechsel von Periode, Dimension, Kennzahl, Fenster oder Größe ersetzt das Bild; die Heatmap verkleinert den Chart-Bereich.

!!! warning "Noch nicht in echten Terminals geprüft"
    Der Bildpfad ist bisher nur in einem Pseudo-Terminal mit nachgestellten Ghostty-Antworten getestet. Die manuelle Prüfung in Ghostty (nativ, in tmux mit und ohne Passthrough), Terminal.app und bei 80×24 steht aus; siehe [Spike: TUI](spike-tui.md#nicht-gepruft-oder-nicht-umgesetzt).

## Bekannte Grenzen

- **Spike.** Die TUI kann in einer späteren Version wieder entfernt werden; siehe die Empfehlung im [Spike](spike-tui.md#empfehlung).
- **Suche:** höchstens 200 Treffer je Abfrage, sortiert nach Rang. Der Zeitfilter startet bei `all`, nicht bei 90 Tagen. Die Filter `a`, `t`, `r` wirken nur, wenn die Liste den Fokus hat.
- **Projektnamen** kommen aus einer Heuristik: Repositories heißen wie der Basisname ihres Git-Roots (bei gleichen Namen mit so vielen übergeordneten Verzeichnissen wie nötig), Verzeichnisse ohne Repository erscheinen als Pfad, das Home-Verzeichnis darin als `~` (`~/notes`). Die Projekt-Ebene aus [#4](https://github.com/seletz/total-recall/issues/4) ersetzt das später.
- **Heatmap** nur ab etwa 25 Terminalzeilen.
- **Aktivität:** der Balken ist nur Text, kein Bild; die Tagesgrenze kommt aus der Config, nicht aus der Ansicht; kein Sprung aus einem Tag in die Session.
- **Bilder** nur in Ghostty, kitty und herdr; andere Terminals mit Kitty Graphics, aber ohne Platzhalter, bekommen Text. Sixel und das iTerm2-Protokoll werden nicht angeboten.
- **Maus** nur für Rad, Tabs und Treffer: kein Klick in Vorschau, Statistik und Aktivität, kein Ziehen in Listen. Textauswahl nur mit Zusatztaste des Terminals oder mit `--no-mouse`.
- **Zwischenablage** über OSC 52; das Terminal muss es erlauben (in tmux `set -g set-clipboard on`).
- **Keine Konfiguration.** Grafikmodus und Startansicht nur als Flag bzw. Argument, kein Abschnitt `[tui]` in der Config.
- **Große Sessions:** Vorschau und Detail laden den Verlauf vollständig; bei Sessions mit mehreren zehntausend Events dauert das Einblenden der Tools einen Moment (gemessen unter 0,1 s).

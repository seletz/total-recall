# TUI

`total-recall tui` öffnet das Archiv in einer Vollbild-Oberfläche im Terminal: Volltextsuche mit Vorschau beim Tippen, eine Detailansicht je Session und eine Statistik der Aktivität als Chart, Tabelle und Heatmap. Die Datenbank wird nur lesend geöffnet.

!!! note "Spike"
    Die TUI ist das Ergebnis eines Spikes. Sie ist als eigenständiges Unterkommando gebaut, das sich wieder entfernen lässt. Entwurf, Abweichungen, Messwerte und Empfehlung stehen in [Spike: TUI](spike-tui.md#ergebnis). Die Oberfläche ist Englisch wie die übrige CLI-Ausgabe; alle Bildschirme auf dieser Seite sind erfundene Beispiele.

## Aufruf

```sh
total-recall tui                          # startet in der Suche
total-recall tui stats                    # startet in der Statistik
total-recall tui --query "goreleaser"     # Suche mit vorbelegtem Suchtext
total-recall tui stats --graphics text    # Chart als Text, kein Bild
```

| Argument / Option | Bedeutung |
|---|---|
| `search` \| `stats` | Ansicht beim Start, Default `search` |
| `--query TEXT` | Suchtext vorbelegen; die Suche läuft sofort beim Start |
| `--graphics auto\|text\|kitty` | Chart-Darstellung in der Statistik, Default `auto`; siehe [Grafikmodi](#grafikmodi) |

Die [globalen Optionen](befehle.md#globale-optionen) `--db`, `--tz` und `--config` gelten wie überall; `--tz` bzw. `timezone` aus der Config bestimmt Tages-, Wochen- und Monatsgrenzen. Beenden mit `q` oder ++ctrl+c++ (++ctrl+c++ immer, `q` nur, solange kein Eingabefeld den Fokus hat).

Die Oberfläche braucht mindestens 80×24 Zeichen; die Beispiele hier sind 100 Zeichen breit. Bei weniger Platz werden Fußzeile und Tabellen abgeschnitten (`…`), die Heatmap blendet sich aus.

## Aufbau

Alle Ansichten teilen denselben Rahmen:

- **Kopfzeile:** links der Programmname, rechts die Ansichten als Tabs. Die aktive steht in eckigen Klammern (`[1 search]  2 stats`), damit sie auch ohne Farbe erkennbar ist. Ist eine Session geöffnet, hängt ihr Name an der Suche: `[1 search] › session`.
- **Fußzeile:** die wichtigsten Tasten der aktuellen Ansicht, dann die globalen (`1 search • 2 stats • tab next view • ? help • q quit`). Hat ein Eingabefeld den Fokus, steht dort nur noch `ctrl+c quit`, weil alle anderen Tasten in das Feld gehen.
- **Hilfe:** `?` legt ein Overlay mit allen Tasten der aktuellen Ansicht und den globalen Tasten über den Inhalt. ++esc++ oder `?` schließt es. Das Overlay wird aus derselben Tastenbelegung erzeugt wie die Fußzeile.

```text
 total-recall                                                                   1 search  [2 stats]
────────────────────────────────────────────────────────────────────────────────────────────────────

             ╭───────────────────────────────────────────────────────────────────────╮
             │ keys: stats                                                           │
             │                                                                       │
             │ d days      p by project      [ earlier     1   search       ?   help │
             │ w weeks     a by agent        ] later       2   stats        esc back │
             │ m months    v by directory    - zoom out    tab next view    q   quit │
             │ e metric    h heatmap         + zoom in                               │
             │                               g graphics                              │
             │                                                                       │
             │ esc or ? closes this help                                             │
             ╰───────────────────────────────────────────────────────────────────────╯

────────────────────────────────────────────────────────────────────────────────────────────────────
 esc back • q quit
```

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
 ↑↓ move • enter open • a actor • t time • r repo • ^r raw fts • esc input • 1 search • 2 stats • …
```

- **Eingabe** oben, mit dem Fokus beim Start. Jede Änderung startet nach 150 ms Ruhe eine Suche wie `search`: ohne Rohsyntax wird jedes Wort in Anführungszeichen gesetzt, alle Wörter müssen vorkommen. Höchstens 200 Treffer werden geholt; die Statuszeile sagt `200 hits, limit reached`, wenn es mehr gäbe. Rechts in der Eingabezeile stehen die aktiven Filter.
- **Trefferliste** links, zwei Zeilen je Treffer: Zeit, Session-Name und Actor, darunter der Textausschnitt mit hervorgehobenen Suchwörtern. Sortierung wie `search`: Rang, dann Zeit. Der markierte Treffer ist mit `▸` und invertiert dargestellt.
- **Vorschau** rechts, folgt der Markierung (nach 100 ms Ruhe): der Session-Kopf (Name, ID und Repo, Zeitraum und Branch), dann der Austausch um den Treffer, also die menschliche Nachricht davor und die Antwort des Agenten danach. Der Treffer-Event ist mit `◄` markiert, die Suchwörter sind unterstrichen. Trifft die Suche einen Tool-Aufruf, lädt die Vorschau den vollen Verlauf inklusive Tools.
- **Fokus:** ++tab++ wechselt von der Eingabe in die Liste, ++esc++ zurück in die Eingabe. In der Liste bewegen ++up++/++down++ und `k`/`j`, ++enter++ öffnet die markierte Session (auch direkt aus der Eingabe). Solange die Eingabe den Fokus hat, gehen Buchstaben in den Suchtext: `q` beendet dann nicht, `1`/`2`/++tab++ wechseln nicht die Ansicht.
- **Statuszeile:** Trefferzahl und Dauer der Abfrage (`5 hits (12 ms)`), sonst `type to search`, `searching…`, `no hits` oder die Fehlermeldung.

### Filter

Die Filter werden in der **Liste** umgeschaltet (in der Eingabe wären die Buchstaben Suchtext). Jede Änderung sucht sofort neu.

| Taste | Filter | Werte |
|---|---|---|
| `a` | Actor | `all` → `human` → `agent` → `peer` → `all` |
| `t` | Zeitraum (Zeit des Events) | `all` → `7d` → `30d` → `90d` → `all` |
| `r` | Repo | Teilstring von Remote-URL, Git-Root oder Arbeitsverzeichnis. Öffnet ein Eingabefeld in der Statuszeile: ++enter++ übernimmt, ++esc++ verwirft. Leer löscht den Filter |
| ++ctrl+r++ | Rohsyntax | FTS5-Syntax unverändert durchreichen (`AND`, `OR`, `NOT`, `"Phrase"`, `präfix*`), wie `search --raw`. Geht auch aus der Eingabe |

Die Hervorhebung in Vorschau und Detailansicht nimmt die Wörter der Eingabe; in Rohsyntax ohne Operatoren, Anführungszeichen und Sterne.

## Session

++enter++ auf einem Treffer öffnet die Session als Ebene über der Suche. Die Suche bleibt darunter erhalten: ++esc++ führt mit Eingabe, Treffern und Markierung zurück. `1`, `2` und ++tab++ schließen die Detailansicht ebenfalls.

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
 project                                                     sessions      human      agent    share
 webshop                                                           56      1 204     21 870     37 %
 infra                                                             56        980     18 310     31 %
 total-recall                                                      21        310      6 120     10 %
 notes                                                             14        402      5 990     10 %
 other                                                             42        515      7 212     12 %
 text chart (kitty: not detected) · 28 days · 312 ms
────────────────────────────────────────────────────────────────────────────────────────────────────
 d/w/m period • p/a/v by • [ ] -/+ range • 1 search • 2 stats • tab next view • ? help • q quit
```

- **Umschalter** oben: Periode (`d` Tage, `w` Wochen, `m` Monate) und Dimension (`p` Projekt, `a` Agent, `v` Verzeichnis), der aktive Wert in Klammern. Rechts der gezeigte Zeitraum.
- **Zeitfenster:** Standard sind die letzten 28 Tage, 16 Wochen oder 12 Monate bis heute. `[` und `]` verschieben um ein ganzes Fenster, `-` und `+` halbieren bzw. verdoppeln die Anzahl Perioden (mindestens eine, höchstens das Achtfache des Standardfensters). Ein Periodenwechsel springt auf das Standardfenster, das die zuletzt gezeigte Periode enthält.
- **Kennzahl:** `e` wechselt zwischen `events` (menschliche plus Agenten-Events), `human` und `sessions`. Der Titel über dem Chart nennt sie.
- **Chart:** eine gestapelte Säule je Periode, Segmente sind die sechs größten Werte der Dimension über das ganze Fenster plus `other`. Die Legende steht darunter; die Farben bleiben beim Umschalten stabil. Im Terminal unterscheiden Farben die Segmente, oben sind sie als Schraffur gezeigt. Perioden ohne Aktivität bleiben leer, die Achse ist lückenlos.
- **Tabelle:** dieselben Reihen mit Sessions, menschlichen und Agenten-Events und Anteil an allen Events. Die Spalte `sessions` summiert die Sessions je Periode: eine Session, die an drei Tagen aktiv war, zählt bei Tagen dreimal.
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

Die Heatmap braucht zehn Zeilen. Bleibt dem Chart daneben weniger als seine Mindesthöhe (Terminal unter 25 Zeilen, etwa 80×24), bleibt sie ausgeblendet und die Statuszeile sagt `heatmap needs more rows`.

## Tasten

### Überall

| Taste | Wirkung |
|---|---|
| `1` / `2` | Suche / Statistik; schließt eine offene Session |
| ++tab++ | nächste Ansicht (in der Suche: aus der Eingabe erst in die Liste) |
| `?` | Hilfe ein/aus |
| ++esc++ | eine Ebene zurück: Session → Suche, Hilfe schließen, Liste → Eingabe, Repo-Filter abbrechen |
| `q` | beenden (nicht, solange Eingabe oder Repo-Filter den Fokus haben) |
| ++ctrl+c++ | beenden, immer |

### Suche

| Taste | Eingabe hat den Fokus | Liste hat den Fokus |
|---|---|---|
| ++tab++ | in die Liste | nächste Ansicht |
| ++esc++ | – | in die Eingabe |
| ++enter++ | markierten Treffer öffnen | markierten Treffer öffnen |
| ++up++ / `k`, ++down++ / `j` | – | Markierung bewegen |
| `a`, `t`, `r` | Suchtext | Filter Actor, Zeitraum, Repo |
| ++ctrl+r++ | Rohsyntax ein/aus | Rohsyntax ein/aus |

Im Repo-Filter: ++enter++ übernimmt, ++esc++ verwirft.

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

## Grafikmodi

Das Chart der Statistik gibt es als Text (Blockzeichen, überall) und als Bild über das Kitty-Graphics-Protokoll. Umschalter, Legende, Tabelle und Heatmap bleiben in jedem Fall Text.

| Modus | Verhalten |
|---|---|
| `auto` (Default) | Beim Start wird das Terminal einmal sondiert. Ein Bild gibt es nur, wenn die Sondierung beantwortet wird **und** die Umgebung ein Terminal nennt, das Bilder über Unicode-Platzhalter darstellen kann: Ghostty oder kitty. Bis dahin und sonst: Text. |
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

**ssh.** Die Sondierung läuft durch ssh hindurch, die Umgebungsvariablen nicht alle: `TERM` kommt an, `TERM_PROGRAM` und `COLORTERM` normalerweise nicht. Ohne Truecolor gehen die Platzhalter kaputt (die Bild-ID steckt in der Vordergrundfarbe), also auf der Gegenseite `COLORTERM=truecolor` setzen oder per `SendEnv`/`AcceptEnv` durchreichen.

**Wie das Bild entsteht.** Das Chart wird außerhalb der Hauptschleife als PNG gerendert, einmal je Änderung an das Terminal übertragen und über Unicode-Platzhalterzellen platziert; der Hintergrund ist transparent, Terminalfarben scheinen durch. Die Bildgröße folgt der Zellenzahl des Chart-Bereichs mal Zellgröße in Pixeln, die das Terminal auf Anfrage meldet (Rückfall 10×20 px). Ein Wechsel von Periode, Dimension, Kennzahl, Fenster oder Größe ersetzt das Bild; die Heatmap verkleinert den Chart-Bereich.

!!! warning "Noch nicht in echten Terminals geprüft"
    Der Bildpfad ist bisher nur in einem Pseudo-Terminal mit nachgestellten Ghostty-Antworten getestet. Die manuelle Prüfung in Ghostty (nativ, in tmux mit und ohne Passthrough), Terminal.app und bei 80×24 steht aus; siehe [Spike: TUI](spike-tui.md#nicht-gepruft-oder-nicht-umgesetzt).

## Bekannte Grenzen

- **Spike.** Die TUI kann in einer späteren Version wieder entfernt werden; siehe die Empfehlung im [Spike](spike-tui.md#empfehlung).
- **Suche:** höchstens 200 Treffer je Abfrage, sortiert nach Rang. Der Zeitfilter startet bei `all`, nicht bei 90 Tagen. Die Filter `a`, `t`, `r` wirken nur, wenn die Liste den Fokus hat.
- **Projektnamen** kommen aus einer Heuristik über Git-Root und Verzeichnis. Verzeichnisse ohne Repository sind nicht als solche markiert (kein `~`, anders als im Entwurf), weil der Schlüssel allein nicht sagt, ob er ein Git-Root ist. Die Projekt-Ebene aus [#4](https://github.com/seletz/total-recall/issues/4) ersetzt das später.
- **Tabelle:** `sessions` ist über die Perioden summiert, nicht über das Fenster eindeutig gezählt.
- **Heatmap** nur ab etwa 25 Terminalzeilen.
- **Bilder** nur in Ghostty und kitty; andere Terminals mit Kitty Graphics, aber ohne Platzhalter, bekommen Text. Sixel und das iTerm2-Protokoll werden nicht angeboten.
- **Keine Maus.** Bedienung nur über die Tastatur.
- **Zwischenablage** über OSC 52; das Terminal muss es erlauben (in tmux `set -g set-clipboard on`).
- **Keine Konfiguration.** Grafikmodus und Startansicht nur als Flag bzw. Argument, kein Abschnitt `[tui]` in der Config.
- **Große Sessions:** Vorschau und Detail laden den Verlauf vollständig; bei Sessions mit mehreren zehntausend Events dauert das Einblenden der Tools einen Moment (gemessen unter 0,1 s).

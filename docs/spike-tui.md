# Spike: TUI

Entwurf für eine Terminal-Oberfläche `total-recall tui`: interaktive Volltextsuche mit Vorschau und grafische Auswertung der Aktivität über die Zeit. Stand der Recherche: 1. November 2026. Ob der Spike übernommen wird, ist offen; er bleibt deshalb ein eigenständiges Unterkommando, das sich wieder entfernen lässt. Der Spike ist umgesetzt; das [Ergebnis](#ergebnis) mit Messwerten und Empfehlung steht am Ende, die Bedienung in [TUI](tui.md).

## Ziele

- Suchen wie mit `search`, aber live: tippen, Treffer sehen, Vorschau lesen, in die Session springen.
- Aktivität über die Zeit sehen: je Tag, Woche oder Monat, aufgeschlüsselt nach Projekt, Agent-Typ (Haupt-Session oder Subagent) oder Verzeichnis.
- Charts als Text (Blockzeichen) überall, als echte Bilder über das Kitty-Graphics-Protokoll, wo das Terminal es kann (Ghostty).

## Festgelegt

Diese Punkte sind entschieden und werden hier nicht neu verhandelt:

- Der Spike wird als Unterkommando `total-recall tui` direkt nach `develop` gemergt. Alles Neue liegt unter `internal/tui/`, der Rest des Programms wird nur minimal berührt (siehe [Architektur](#architektur)).
- Grafik hybrid: Textcharts als Basis, Bilder über Kitty Graphics, wenn erkannt; sauberer Rückfall.
- „Projekt“ ist im Spike eine Heuristik aus Git-Root und Verzeichnis, nur lesend. Keine Schema-Migration. Die Projekt-Ebene aus [#4](https://github.com/seletz/total-recall/issues/4) bleibt davon unabhängig; die TUI bekommt sie später als Dimension dazu.
- Die TUI öffnet die Datenbank nur lesend (`store.OpenReadOnly`, wie alle lesenden Befehle).
- `CGO_ENABLED=0` bleibt. Alle Bibliotheken sind reines Go.

## Bibliotheken

### Vergleich

Stand 1. November 2026, Versionen von pkg.go.dev und GitHub.

**TUI-Frameworks**

| Bibliothek | Version | Lizenz | Pflege | CGO | Suche mit Liste + Vorschau | Charts / Kitty |
|---|---|---|---|---|---|---|
| Bubble Tea v2 (`charm.land/bubbletea/v2`) + Bubbles v2 + Lip Gloss v2 | v2.1.1 (28.10.2026), stabil seit v2.0.0 (24.02.2026) | MIT | sehr aktiv, Releases monatlich | nein | Beste Widget-Auswahl (`textinput`, `list`, `viewport`, `table`), Elm-Modell gut testbar (`teatest`) | ntcharts ist dafür gebaut; Renderer (Ultraviolet) verwirft rohe Kitty-Sequenzen im View, Bilder brauchen Unicode-Platzhalter |
| tview + tcell v2 | tview v0.42.0 (27.08.2025), tcell v2.8.1 | MIT | ein Maintainer, Schübe; tcell v3 (v3.6.0, 10.2026) noch nicht übernommen | nein | Schnell zusammengesteckt, aber imperativ und schwer zu testen | Keine Chart-Bibliothek; Bilder nur über `Screen.Tty()` von Hand, Neuzeichnen selbst verwalten |
| gocui (jesseduffield / awesome-gocui) | keine Releases bzw. v1.1.0 (2022) | BSD-3 | Fork nur für lazygit gepflegt | nein | Sehr kleine API | nichts |
| vaxis (`go.rockorager.dev/vaxis`) | v0.17.1 (10.2026) | Apache-2.0 | ein Autor, stetig | nein | Eigene Widgets, wenig Ökosystem | Einziges Framework mit eingebauten Bildern (Kitty, Sixel) |

**Charts und Bilder**

| Bibliothek | Zweck | Version | Lizenz | Pflege | CGO |
|---|---|---|---|---|---|
| ntcharts v2 (`github.com/NimbleMarkets/ntcharts/v2`) | Textcharts für Bubble Tea: gestapelte Balken, Zeitreihen, Heatmap, Sparkline | v2.9.0 (29.10.2026) | MIT; eine Datei (`picture/glyph.go`) MPL-2.0 | sehr aktiv (sechs Releases Sep./Okt. 2026); README: API kann sich noch ändern | nein |
| chartpicture (`github.com/NimbleMarkets/ntcharts/picture/chartpicture/v2`) | go-analyze/charts-Bilder im Terminal: Kitty Graphics mit Glyphen-Rückfall, Erkennung, tmux-Passthrough, Unicode-Platzhalter | v2.9.0 | MIT | wie ntcharts | nein |
| go-analyze/charts | Charts als PNG/SVG, Schriften eingebettet; Nachfolger von go-chart und vicanso/go-charts (beide archiviert) | v0.6.4 (15.10.2026) | MIT | aktiv | nein |
| asciigraph | Linien-Charts als Text, frameworkfrei | v0.10.0 (06.2026) | BSD-3 | aktiv | nein |
| gonum/plot | wissenschaftliche Plots als PNG | v0.17.0 (09.2025) | BSD-3 | langsam | nein |
| rasterm | Kitty-/Sixel-/iTerm2-Encoder, minimal | v1.1.2 (01.2025) | MIT | ruhig | nein |
| go-termimg | Bilder im Terminal, viele Protokolle; Platzhalter laut README „unfertig“ | v0.1.26 (02.2026) | MIT | mäßig | nein |
| termui | Textcharts | v3.1.0 (2019) | MIT | tot | nein |

**Ghostty** (v1.4.0, 27.10.2026): Kitty Graphics vollständig inklusive Unicode-Platzhalter, Löschen, z-Index; ab 1.4.0 auch Animation und relative Platzierung. Kein Sixel (ausdrücklich nicht geplant), kein iTerm2-Protokoll. Kitty-Keyboard-Protokoll, synchronisierte Ausgabe (DEC 2026), Truecolor, OSC 52. Die Abfrage `a=q` wird beantwortet. Umgebung: `TERM=xterm-ghostty`, `TERM_PROGRAM=ghostty`, `TERM_PROGRAM_VERSION`, `GHOSTTY_RESOURCES_DIR`.

### Entscheidung

**Bubble Tea v2 + Bubbles v2 + Lip Gloss v2**, Charts mit **ntcharts v2** (Text) und **chartpicture + go-analyze/charts** (Bilder über Kitty Graphics), Tests mit **teatest v2** und `x/exp/golden`.

Warum:

- Die Suchansicht besteht aus Eingabe, Liste und Viewport. Bubbles liefert alle drei, inklusive Soft-Wrap und Hervorhebung im Viewport. Bei tview wäre das ähnlich schnell, bei vaxis und gocui Handarbeit.
- Das Elm-Modell (Model, Update, View) lässt sich ohne Terminal testen. `teatest` liefert Golden-Tests gegen eine feste Terminalgröße. Das passt zur TDD-Regel des Repos.
- ntcharts ist die einzige gepflegte Chart-Bibliothek für Go-TUIs und bringt genau die drei Chart-Typen mit, die gebraucht werden: gestapelte Balken, Zeitreihe, Heatmap.
- Der Kitty-Pfad ist in chartpicture fertig: Erkennung per `a=q`-Abfrage mit Timeout, Unicode-Platzhalter (damit der Bubble-Tea-Renderer die Bildzellen nicht verwirft), tmux-Passthrough, Glyphen-Rückfall, Rendering außerhalb der Hauptschleife. Ein eigener Encoder wäre etwa 150 Zeilen plus 100 Zeilen Platzhalter und bleibt als Plan B (siehe [Risiken](#risiken)).
- `chartpicture.BarChartOptionFromNT` erzeugt aus einem ntcharts-Balkendiagramm die Bildvariante. Text- und Bild-Chart kommen also aus einer Datenstruktur.
- Alles MIT (eine Datei MPL-2.0, dateiweise Copyleft, ohne Folgen für uns). Keine CGO. Ein Probe-Build mit allen Abhängigkeiten und `CGO_ENABLED=0` ist gelungen (arm64, etwa 5 MiB zusätzlich).

Was dagegen spricht und in Kauf genommen wird: Bubble Tea v2 setzt Go 1.26 voraus (das Repo steht auf 1.27), der Renderer Ultraviolet hat noch kein Tag, und ntcharts warnt selbst vor API-Änderungen. Für einen Spike ist das vertretbar; vor einer Übernahme werden die Versionen gepinnt und geprüft.

Nicht gewählt: tview (keine Charts, Bilder von Hand, Tests schwer), vaxis (Bilder eingebaut, aber kein Chart- und Widget-Ökosystem), eigener Kitty-Encoder (unnötig, solange chartpicture trägt).

## Architektur

```
internal/cli/tui.go          Unterkommando `tui`: Config laden, Store read-only öffnen, Programm starten
internal/tui                 App-Model: Ansichten, Umschalten, Keymap, Statuszeile, Hilfe
internal/tui/search          Suchansicht: Eingabe, Trefferliste, Vorschau
internal/tui/session         Session-Detailansicht (Viewport über Events)
internal/tui/stats           Statistikansicht: Umschalter, Tabelle, Chart-Container
internal/tui/chart           Chart-Spezifikation aus Aggregaten; Text-Rendering (ntcharts); Bild-Rendering (go-analyze/charts, chartpicture)
internal/project             Projekt-Heuristik (reine Funktion; seit `stats --by project` außerhalb der TUI)
internal/store/activity.go   Aggregation Periode × Dimension (neu, nur lesend, additive Datei)
```

Berührt wird bestehender Code an drei Stellen: `internal/cli/root.go` hängt den Befehl ein, `go.mod` bekommt die Abhängigkeiten, `internal/store` bekommt eine neue Datei mit Tests. Zum Entfernen: `internal/tui` löschen, `tui.go` und die Zeile in `root.go` entfernen, `go mod tidy`. `activity.go` kann bleiben; `stats` kann es später selbst nutzen.

Datenfluss:

```
Tastatur ──► tea.Program ──► App.Update ──► Ansicht.Update ──► tea.Cmd (Abfrage im Hintergrund)
                                                 ▲                      │
                                                 └──── Ergebnis-Msg ◄───┘ (mit Sequenznummer; veraltete werden verworfen)
Ansicht.View ──► Lip Gloss-Layout ──► Renderer
```

Jede Abfrage läuft als `tea.Cmd` mit `context.Context`; eine neue Eingabe bricht die vorige ab. Ergebnisse tragen eine Sequenznummer, das Model nimmt nur das jüngste.

### Datenschicht

Wiederverwendet aus `internal/store`:

| Zweck | Vorhanden |
|---|---|
| Treffer mit Snippet, Rang, Session-Name, Repo | `Store.Search` (FTS5, `bm25`, `snippet`), Filter `SearchFilter` (Zeit, Repo, Harness, Name, Actor, Raw) |
| Session-Kopf, Zähler | `Store.Session`, `Store.FindSession`, `SessionInfo.Repo()` |
| Verlauf einer Session | `Store.Events(id, all)` |
| Sessionliste | `Store.Sessions` mit `SessionFilter` (TopLevel, HideEmpty, Limit) |
| Kennzahlen je Tag, Repo, Wochentag×Stunde | `Store.Stats` |

Fehlt und kommt neu in `internal/store/activity.go`:

```go
type Period int    // PeriodDay, PeriodWeek (ISO, Montag), PeriodMonth
type Dimension int // DimProject, DimAgent, DimDirectory

type ActivityFilter struct {
    Since, Until time.Time
    Location     *time.Location
    Period       Period
    Dimension    Dimension
    TopN         int // Werte je Periode, Rest als "andere"; 0 = alle
}

type ActivityCell struct {
    Key string // Projekt, "claude main"/"claude subagent", Verzeichnis
    Activity   // Sessions, Human, Agent (aus Store.Stats)
}

type ActivityBucket struct {
    Start time.Time // Periodenanfang in Location
    Label string    // 2026-10-05, 2026-W41, 2026-10
    Cells []ActivityCell // sortiert nach Key-Reihenfolge von Keys
}

type ActivitySeries struct {
    Buckets []ActivityBucket // lückenlos, auch leere Perioden
    Keys    []string         // Dimensionswerte, nach Gesamtvolumen absteigend
}

func (s *Store) ActivitySeries(ctx context.Context, f ActivityFilter) (ActivitySeries, error)
```

Die Abfrage liest wie `Store.Stats` nur `ts`, `actor`, `session_id` plus die Dimensionsspalten (`git_root`, `cwd`, `parent_id`, `harness`) und bucketet in Go in der konfigurierten Zeitzone. Das ist bewusst dieselbe Technik wie bei `Stats`: SQLite kennt keine Zeitzonen, und `events.ts_local` steht fest auf der Zeitzone beim Einlesen. Lücken werden aufgefüllt, damit Charts eine durchgehende Zeitachse haben. Gemessen an der echten Datenbank (etwa 60.000 human/agent-Events im Oktober 2026) liegt `stats` heute bei unter 0,4 s inklusive Prozessstart; das reicht für interaktive Umschalter, und das Ergebnis wird je Filterkombination gecacht.

Dimension `DimAgent` liefert `<harness> main` für Sessions ohne `parent_id` und `<harness> subagent` für Sessions mit `parent_id`. Dimension `DimDirectory` liefert `cwd`. Dimension `DimProject` liefert den Rohschlüssel (`git_root`, sonst `cwd`); die Abbildung auf einen Projektnamen macht `internal/project`, damit der Store keine Darstellungsregeln kennt. Kennzahlen je Zelle: Sessions (distinct), human-Events, agent-Events. Weitere Kennzahlen (Tokens, aktive Minuten) sind [offen](#offene-fragen).

### Projekt-Heuristik

Nur lesend, reine Funktion `project.Name(gitRoot, cwd string) string`, tabellengetrieben getestet:

1. `git_root` gesetzt: Basisname des Git-Roots. `gitinfo` löst Worktrees bereits auf das Hauptrepo auf (`git rev-parse --git-common-dir`, Layout `<repo>__worktrees/<name>`), daher landen Worktree-Sessions beim Repo.
2. Kein Git-Root: Basisname des `cwd`, mit Präfix `~` markiert, zum Beispiel `~vault`.
3. Subagent-Sessions erben den Schlüssel der Eltern-Session (sie teilen ohnehin das `cwd`).
4. Kollision (zwei Git-Roots mit gleichem Basisnamen): Elternverzeichnis dazu, `dipa/dipa` statt `dipa`. Die Kollisionsprüfung macht `project.Names(keys []string) map[string]string` über alle Schlüssel einer Serie.

In der echten Datenbank ergibt das heute rund 24 Projekte aus 214 Verzeichnissen. Wenn [#4](https://github.com/seletz/total-recall/issues/4) kommt, ersetzt die dort definierte Zuordnung Schritt 1 bis 4; die Schnittstelle (Schlüssel → Name) bleibt.

## Bildschirme

Alle Mockups 100×30 Zeichen. Die Oberfläche ist Englisch wie die CLI; Hilfetexte in der Doku Deutsch.

### Suche

```
┌ total-recall ───────────────────────────────── 1 search  2 stats  ? help  q quit ┐
│ > goreleaser cask_                                   actor: all  since: 90d  repo: * │
├────────────────────────────────────────────┬─────────────────────────────────────────┤
│ 2026-10-31 22:14  Release v0.1.0      human │ Release v0.1.0                          │
│   …upload the cask to the homebrew tap wi… │ claude:f2b299a1-…  seletz/total-recall  │
│ 2026-10-31 22:16  Release v0.1.0      agent │ 2026-10-31 22:10 – 23:02 (52m)  develop  │
│   …goreleaser skips the cask when the tap… │─────────────────────────────────────────│
│ 2026-10-28 09:02  CI pipeline         human │ ── 22:14 human                           │
│   …can we build the cask in goreleaser…    │ Please also upload the cask to the       │
│ 2026-10-28 09:05  CI pipeline         agent │ homebrew tap with goreleaser, the token  │
│   …goreleaser needs HOMEBREW_TAP_GITHUB_T… │ is in the repo secrets.                  │
│ 2026-10-12 15:40  odoo-work-cli       agent │                                          │
│   …copied .goreleaser.yml from odoo-work-… │ ── 22:16 agent                           │
│                                            │ goreleaser skips the cask upload when    │
│                                            │ the tap token is missing. I added …      │
│                                            │                                          │
│                                            │                                          │
│                                            │                                          │
├────────────────────────────────────────────┴─────────────────────────────────────────┤
│ 5 hits (12 ms)   ↑↓ move  enter open  tab list/input  a actor  t time  r repo  ^r raw │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

- Eingabe oben, Fokus beim Start. Jeder Tastendruck startet nach 150 ms Ruhe eine Suche (`Store.Search`, Limit 200). Ohne Eingabe: leere Liste und Hinweis.
- Links die Treffer: Zeit, Session-Name, Actor, Snippet (zweizeilig, Treffer hervorgehoben). Sortierung wie `search`: Rang, dann Zeit.
- Rechts die Vorschau zum markierten Treffer: Session-Kopf (Name, ID, Repo, Zeitraum, Branch), dann der Austausch um den Treffer: die menschliche Nachricht davor und die Antwort danach (`Store.Events` der Session, Fenster um den Treffer, Treffer hervorgehoben).
- Filter als Umschalter in der Fußzeile: ++a++ Actor (all, human, agent, peer), ++t++ Zeitraum (7d, 30d, 90d, all), ++r++ Repo-Teilstring (kleine Eingabe), ++ctrl+r++ FTS5-Rohsyntax. Filter stehen rechts in der Kopfzeile.
- ++tab++ wechselt zwischen Eingabe und Liste; in der Liste bewegen ++j++/++k++ und Pfeile, ++enter++ öffnet die Session, ++esc++ zurück zur Eingabe.

### Session

```
┌ total-recall ───────────────────────────────── 1 search  2 stats  ? help  q quit ┐
│ Release v0.1.0                                           claude:f2b299a1-b3c1-4cf8… │
│ seletz/total-recall  develop  2026-10-31 22:10 – 23:02 (52m)  3 human  14 agent      │
├───────────────────────────────────────────────────────────────────────────────────────┤
│ ── 22:10 human                                                                        │
│ Let's cut v0.1.0. Bump the version, tag, and run goreleaser.                          │
│                                                                                       │
│ ── 22:11 agent                                                                        │
│ I'll bump the version in …                                                            │
│                                                                                       │
│ ── 22:14 human                                                                   ◄── │
│ Please also upload the cask to the homebrew tap with [goreleaser], the token is in   │
│ the repo secrets.                                                                     │
│                                                                                       │
│ ── 22:16 agent                                                                        │
│ goreleaser skips the [cask] upload when the tap token is missing. I added …          │
│ ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒ │
├───────────────────────────────────────────────────────────────────────────────────────┤
│ 23/41 events   ↑↓ scroll  n/N next/prev hit  T tools  y copy id  esc back              │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

- Viewport (Bubbles) über `Store.Events`, Soft-Wrap, Markierung der Treffer der aktuellen Suche (Begriffe werden im Text hervorgehoben, ++n++/++shift+n++ springen).
- ++t++ (groß) blendet Tool-Aufrufe und -Ergebnisse ein (`all = true`), wie `show --all`.
- ++y++ kopiert die Session-ID (OSC 52, von Bubble Tea v2 unterstützt; Ghostty kann es). Für `show`, `sql` und Agenten.

### Statistik

```
┌ total-recall ───────────────────────────────── 1 search  2 stats  ? help  q quit ┐
│ period: [day] week month    by: [project] agent directory    range: 2026-10-05 – 11-01 │
├───────────────────────────────────────────────────────────────────────────────────────┤
│ events                                                                                │
│ 4000 ┤                                           ███                                  │
│      │                                       ███ ███                                  │
│ 3000 ┤                                   ███ ███ ███ ███                              │
│      │                               ▒▒▒ ███ ▒▒▒ ███ ███                              │
│ 2000 ┤                       ░░░ ▒▒▒ ▒▒▒ ▒▒▒ ▒▒▒ ▒▒▒ ███ ░░░                          │
│      │               ███     ░░░ ░░░ ▒▒▒ ░░░ ░░░ ▒▒▒ ▒▒▒ ░░░ ▒▒▒                      │
│ 1000 ┤       ▒▒▒ ░░░ ▒▒▒ ███ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ███                  │
│      │ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░ ░░░            │
│    0 ┼─────────────────────────────────────────────────────────────────────────────── │
│       05  07  09  11  13  15  17  19  21  23  25  27  29  31                            │
│                                                                                       │
│ ███ dipa  ▒▒▒ dg-k8s  ░░░ sichtung  ▓▓▓ total-recall  ··· other                      │
├───────────────────────────────────────────────────────────────────────────────────────┤
│ project         sessions   human    agent   share                                     │
│ dipa                  56    1 204   21 870   37 %                                     │
│ dg-k8s                56      980   18 310   31 %                                     │
│ sichtung              21      310    6 120   10 %                                     │
│ total-recall          14      402    5 990   10 %                                     │
│ other                 42      515    7 212   12 %                                     │
├───────────────────────────────────────────────────────────────────────────────────────┤
│ text chart (kitty: not detected)   d/w/m period  p/a/v by  [ ] shift  - + zoom  g gfx │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

- Oben die Umschalter: ++d++/++w++/++m++ Periode (Tag, Woche, Monat), ++p++/++a++/++v++ Dimension (Projekt, Agent, Verzeichnis). Der Zeitraum folgt der Periode: zuletzt 28 Tage, 16 Wochen, 12 Monate; `[`/`]` verschieben um ein Fenster, `-`/`+` halbieren und verdoppeln. ++e++ wechselt die Kennzahl (Events, human-Events, Sessions).
- Mitte: ein gestapeltes Balkendiagramm (ntcharts `barchart`, `BarData` mit mehreren `BarValue`), eine Säule je Periode, Segmente = die sechs größten Dimensionswerte plus „other“. Legende darunter. Farben aus einer festen Palette, die in Text- und Bildvariante gleich ist.
- Unten die Tabelle zur Serie (Bubbles `table`), sortiert nach Volumen, mit Anteil.
- ++g++ schaltet Grafik um: `auto` (erkannt → Bild), `text`, `kitty` (erzwingen). Die Fußzeile zeigt den Modus und bei `auto` den Grund, wenn kein Bild gezeigt wird (`ToggleBlocked()`: Erkennung läuft, nicht erkannt, Timeout, tmux ohne Passthrough).
- ++h++ zeigt als zweites Chart die Heatmap Wochentag × Stunde für den Zeitraum (ntcharts `heatmap`, Daten aus `Store.Stats`). Optional, siehe Issues.

Im Bildmodus ersetzt `chartpicture.Model` den Chart-Bereich. Die Bildgröße folgt der Zellenzahl des Bereichs mal Zellgröße in Pixeln (`picture.RequestCellSize`, Antwort auf `CSI 16 t`; Rückfall 10×20 px). Tabelle, Legende und Umschalter bleiben Text.

### Navigation und Tasten

| Taste | Überall |
|---|---|
| ++1++ / ++2++ | Suche / Statistik |
| ++tab++ | nächste Ansicht (in der Suche: Eingabe ↔ Liste) |
| `?` | Hilfe ein/aus (Overlay mit allen Tasten der aktuellen Ansicht) |
| ++esc++ | eine Ebene zurück (Detail → Suche, Hilfe schließen, Filter abbrechen) |
| ++q++ / ++ctrl+c++ | beenden (++q++ nicht, solange die Sucheingabe den Fokus hat) |

Die Tastenbelegung ist je Ansicht ein `key.KeyMap` (Bubbles) und wird für die Hilfe daraus erzeugt, nicht doppelt gepflegt. Maus: Scrollen im Viewport und in der Liste, mehr nicht.

Aufruf:

```sh
total-recall tui                       # startet in der Suche
total-recall tui stats                 # startet in der Statistik
total-recall tui --query "goreleaser"  # startet mit Suchbegriff
total-recall tui --graphics text       # auto | text | kitty
```

Globale Flags (`--db`, `--tz`, `--config`) gelten wie überall. Die TUI respektiert `timezone` aus der Config für Tagesgrenzen.

## Kitty Graphics: Erkennung und Rückfall

Reihenfolge beim Start, umgesetzt von `picture` (ntcharts) und im Spike nur konfiguriert:

1. `--graphics text`: nie sondieren, Textcharts. `--graphics kitty`: `picture.ForceKittyCapability(KittyCapabilitySupported)`, ungeprüft.
2. `auto`: Beim `Init` schickt `picture.QueryKittySupport` die Abfrage `ESC _ G i=31,s=1,v=1,a=q,t=d,f=24;AAAA ESC \` gefolgt von `CSI c` (DA1). Kommt `i=31;OK` vor der DA1-Antwort, ist Kitty da; kommt nur DA1 oder nichts bis zum Timeout, bleibt es bei Text. Umgebungsvariablen (`TERM_PROGRAM=ghostty`, `TERM=xterm-kitty`, `KITTY_WINDOW_ID`) sind nur Hinweis für die Fußzeile, nicht Entscheidung: sie überleben weder ssh noch tmux zuverlässig.
3. Unter tmux (`$TMUX` gesetzt) wickelt die Bibliothek alle Sequenzen in DCS-Passthrough. Ohne `set -g allow-passthrough on` (tmux ≥ 3.3) kommt keine Antwort, Ergebnis Timeout, Rückfall Text, Grund in der Fußzeile.
4. Bis die Sondierung entschieden ist, zeigt die Ansicht das Textchart; danach wird bei Erfolg einmal umgeschaltet. Kein Flackern, kein Warten.

Warum Unicode-Platzhalter: Der Renderer von Bubble Tea v2 (Ultraviolet) hält ein Zellraster und zeichnet nur Differenzen. Rohe Kitty-Sequenzen im View-String werden seit v2.0.8 verworfen. Beim Platzhalter-Verfahren wird das Bild einmal außerhalb des Views übertragen (`a=T,U=1,i=<id>`, PNG `f=100`, Chunks à 4096 Byte Base64), und die Bildzellen sind gewöhnliche Zeichen (U+10EEEE mit Zeilen-/Spalten-Diakritika und der Bild-ID in der Vordergrundfarbe). Die überlebt jeden Diff, scrollt mit und funktioniert auch durch tmux. Voraussetzung ist Truecolor, was Ghostty setzt (`COLORTERM=truecolor`).

Fallmatrix:

| Terminal | Verhalten |
|---|---|
| Ghostty ≥ 1.0, kitty | Bild (Platzhalter) |
| herdr in Ghostty | Bild, wenn herdr die `a=q`-Abfrage beantwortet. chartpicture sondiert dort nicht (`TERM_PROGRAM=herdr` ist ihm unbekannt), die TUI schickt die Abfrage selbst; siehe [#41](https://github.com/seletz/total-recall/issues/41) |
| WezTerm, Konsole, Warp | antworten auf `a=q`, können aber keine Platzhalter: Bild käme kaputt. Im Spike: Bildmodus nur, wenn `TERM_PROGRAM` ghostty oder `TERM` xterm-kitty ist, sonst Text trotz positiver Sondierung. Offen für später. |
| Alacritty, Terminal.app, iTerm2 stabil, Windows Terminal | Text |
| tmux ohne Passthrough, Zellij | Text, Hinweis in der Fußzeile |
| ssh | Bild, sofern das entfernte Terminal Ghostty ist und `TERM_PROGRAM` weitergereicht wird; sonst Text |

Sixel und iTerm2-Protokoll werden nicht angeboten: Ghostty kann beides nicht, und für andere Terminals reicht Text.

## Teststrategie

TDD wie im Repo: erst der rote Test, dann Code. Tabellengetrieben. `go test ./...` und `go vet ./...` grün. Keine echten Transkripte, keine echten Session-Inhalte. Alle Fixtures kommen aus `internal/testfixture` und sind erfunden.

| Ebene | Technik | Was wird geprüft |
|---|---|---|
| `store.ActivitySeries` | Temp-DB, befüllt über die Write-API mit synthetischen Sessions/Events; Tabelle je Periode × Dimension | Bucketgrenzen in `Europe/Berlin` (Tageswechsel, ISO-Woche über Jahreswechsel, Monat), lückenloses Auffüllen, TopN + „other“, Subagent-Zuordnung, leere Zeiträume |
| `project.Name/Names` | reine Funktionen | Git-Root, Worktree-Pfad, kein Repo, Kollision, leere Werte |
| `chart` Spezifikation | reine Funktionen auf `ActivitySeries` | Farbzuordnung stabil über Umschalter, Reihenfolge, Labels je Periode, „other“ zuletzt |
| Textcharts | `golden.RequireEqual` auf die gerenderte Zeichenfläche bei fester Größe, Farbprofil ohne Farbe | Balkenhöhen, Legende, Achsen |
| Bildcharts | PNG rendern, dekodieren: Maße stimmen, Bild nicht leer, bekannte Palettenfarben enthalten. Kein Pixel-Golden (Schriftrendering darf sich ändern) | Rendering läuft ohne Terminal, Größe folgt Zellen × Zellgröße |
| Ansichten | `teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))`, Tasten per `Send`/`Type`, `WaitFor` auf Text, `RequireEqualOutput` gegen `testdata/*.golden`, Aktualisieren mit `-update` | Suche: Debounce, veraltete Ergebnisse verworfen, Vorschau folgt der Markierung, Filter. Statistik: Umschalter, Zeitraumfenster, Fußzeile. Detail: Sprung zum Treffer, Tool-Umschalter |
| `tui`-Befehl | `cli`-Test in-process wie die anderen Befehle: `--help`, fehlende DB, `--graphics` validiert | Einhängen und Fehlerpfade |

Für deterministische Golden-Dateien: Terminalgröße fest, Farbprofil in Tests auf „keine Farbe“ (Programmoption `tea.WithColorProfile`), Kitty-Sondierung in Tests erzwungen auf „nicht unterstützt“ (`picture.ForceKittyCapability`), Uhr injiziert (wie `App.Now`), Locale fest. Die Golden-Dateien enthalten Escape-Sequenzen des Renderers; sie werden mit `-update` erneuert, und ein Bubble-Tea-Update, das sie ändert, ist ein bewusster Commit.

Neue Fixtures: `testfixture.Activity(t, dir)` schreibt ein kleines Projektverzeichnis mit drei Projekten, einem Worktree, einer Subagent-Session und Events über sechs Wochen, damit Tag/Woche/Monat und alle drei Dimensionen etwas zu zeigen haben. Bestehende Tests benutzen weiter `testfixture.Demo`.

Manuell vor jedem Merge (Checkliste in der PR, lesend gegen die echte DB): Ghostty nativ, Ghostty in tmux mit und ohne Passthrough, Terminal.app, Fenster verkleinern auf 80×24.

## Risiken

| Risiko | Folge | Umgang |
|---|---|---|
| Bubble Tea v2 / Ultraviolet ändern sich (kein Tag für Ultraviolet, Performance-Issue #1481 offen) | Build bricht bei Update, Golden-Dateien ändern sich | Versionen pinnen, `go.sum` im Repo, Updates bewusst |
| ntcharts-API ändert sich (README-Warnung) | wie oben | Chart-Code hinter `internal/tui/chart` kapseln, nur `barchart`, `heatmap`, `picture`, `chartpicture` nutzen |
| chartpicture zeigt in der Praxis kein Bild (Platzhalter, Zellgröße, Race zwischen Upload und Zeichnen) | Bildmodus unbrauchbar | Erster Schritt im Grafik-Epic ist ein Durchstich mit einem Platzhalterbild. Plan B: eigener Encoder (rasterm-artig, etwa 150 Zeilen) plus Platzhalterzeilen, Upload per `tea.Raw`, `tea.Sequence` für Upload-dann-Zeichnen |
| Ghostty < 1.4 hat Fehler mit Platzhaltern in tmux (#13056) | kaputte Bilder in alten Versionen | Text in tmux, solange `TERM_PROGRAM_VERSION` < 1.4.0 |
| Große Ergebnisse (Suche nach häufigen Wörtern) | träge Liste | Limit 200, Snippets kurz, Vorschau lädt nur die Events eines Fensters |
| Binärgröße und Abhängigkeiten (+ ~5 MiB, ~40 Module) | Release größer, mehr Angriffsfläche | Für einen Spike hinnehmen; bei Übernahme prüfen, ob Glamour u. a. nötig sind |
| MPL-2.0 in `picture/glyph.go` | dateiweise Copyleft | kein Problem, Datei wird nicht geändert; in der Doku nennen |
| Go-Mindestversion 1.26 | niemand sonst betroffen (Repo auf 1.27) | — |

## Offene Fragen

- **Kennzahl „Dauer“.** Sessions haben Start und Ende, aber lange Pausen zählen mit. Vorschlag für später: aktive Minuten = Summe der Abstände zwischen Events, gedeckelt bei 15 Minuten. Im Spike nur Events und Sessions.
- **Tokens.** `tokens_out` je Event ist da und ließe sich stapeln. Lohnt sich das als Kennzahl, oder verwirrt es neben Events?
- **Sprache der Oberfläche.** Vorschlag: Englisch wie die CLI-Ausgabe. Alternative: Deutsch wie die Doku.
- **Andere Terminals mit Kitty Graphics, aber ohne Platzhalter** (WezTerm, Konsole): im Spike Text. Später könnte eine direkte Platzierung ohne Platzhalter nachgerüstet werden, kostet aber eigene Renderer-Logik.
- **Kitty-Keyboard-Protokoll.** Bubble Tea v2 kann es anfragen (`KeyboardEnhancements`); nötig ist es erst, wenn Tastenkombinationen wie ++shift+enter++ gebraucht werden. Im Spike aus.
- **`stats` erweitern.** `Store.ActivitySeries` könnte `stats --period week --by project` versorgen und damit `monthly_stats.py` im Vault ersetzen. Kleiner Folgeschritt, nicht Teil des Spikes.
- **Config.** Ein Abschnitt `[tui]` (`graphics = "auto"`, Standard-Zeitraum) ist leicht, aber vor der Übernahme unnötig. Im Spike nur Flags.

## Ergebnis

Stand 1. November 2026, nach dem Merge aller Teil-Issues des Meilensteins „Spike: TUI“ in `develop`. Benutzerdoku: [TUI](tui.md), Befehlsreferenz: [`tui`](befehle.md#tui).

### Was funktioniert

- **Stack wie entschieden.** Bubble Tea v2.1.1, Bubbles v2.2.1, Lip Gloss v2.0.6, ntcharts v2.9.0, chartpicture v2.9.0, go-analyze/charts v0.6.4, teatest v2 (Pseudo-Version, es gibt kein Tag). Alles reines Go, der statische Build mit `CGO_ENABLED=0` funktioniert. Ultraviolet (Renderer) ist als direkte Abhängigkeit dazugekommen, weil der Zellgrößen-Event dort definiert ist.
- **Suche** mit Debounce (150 ms), Abbruch und Sequenznummern, Limit 200, Trefferliste mit Snippets, Vorschau des Austauschs um den Treffer (eigene Debounce 100 ms), Filter Actor, Zeitraum, Repo und Rohsyntax, Sprung in die Session.
- **Session** als Viewport über `Store.Events`: Umbruch am Wortende, Hervorhebung der Suchwörter, `n`/`N`, Tool-Events ein- und ausblenden mit stabiler Position, Session-ID kopieren (OSC 52). Treffer auf Tool-Events blenden Tools automatisch ein.
- **Statistik:** `Store.ActivitySeries` mit einer Abfrage und Bucketing in Go, Periode × Dimension, Zeitfenster verschieben und zoomen, drei Kennzahlen, gestapeltes Textchart mit stabilen Farben, Tabelle, Heatmap Wochentag × Stunde aus `Store.Stats`. Ergebnisse je Filterkombination gecacht.
- **Kitty Graphics** über chartpicture: Sondierung, Zellgröße, PNG-Übertragung in 4096-Byte-Chunks, Unicode-Platzhalter, Löschen beim Umschalten, Entscheidungstabelle `Decide` als reine Funktion. Im Pseudo-Terminal mit nachgestellten Ghostty-Antworten läuft der ganze Pfad durch (siehe [Nicht geprüft](#nicht-gepruft-oder-nicht-umgesetzt)). Ein eigener Encoder (Plan B) war nicht nötig.
- **Tests:** `go test ./...` und `go vet ./...` grün; Golden-Tests je Ansicht, Entscheidungstabellen, PNG-Dekodierung, Programmläufe mit teatest, CLI-Tests für `--help`, ungültige Flags und fehlende DB. `TestMain` in `internal/tui` und `internal/tui/stats` pinnt den prozessweiten Zustand des `picture`-Pakets, damit Goldens nicht vom Terminal abhängen, das `go test` ausführt.

### Nicht geprüft oder nicht umgesetzt

- **Manuelle Prüfung in echten Terminals steht aus.** Ghostty nativ, Ghostty in tmux mit und ohne Passthrough, Terminal.app, Fenster auf 80×24: die Checkliste aus PR [#38](https://github.com/seletz/total-recall/pull/38) ist offen. Der Bildpfad ist damit nur gegen nachgestellte Terminalantworten belegt, nicht gegen Ghostty selbst. Das ist die wichtigste Lücke vor einer Übernahme.
- **ssh** und **kitty** (das Terminal) sind nicht ausprobiert. Über ssh fehlt normalerweise `COLORTERM`, ohne Truecolor gehen Platzhalter kaputt; dokumentiert, nicht getestet.
- **Andere Terminals mit Kitty Graphics, aber ohne Platzhalter** (WezTerm, Konsole) bekommen Text, wie im Entwurf offen gelassen.
- **Maus:** der Mausmodus ist nicht eingeschaltet; die Detailansicht würde Rad-Events verarbeiten, bekommt aber keine.
- **Offene Fragen aus dem Entwurf** bleiben offen: Kennzahlen Dauer und Tokens, Config-Abschnitt `[tui]`, Kitty-Keyboard-Protokoll, Sprache der Oberfläche (Englisch wie vorgeschlagen).

### Abweichungen vom Entwurf

- **Fokus in der Suche.** Entwurf: ++tab++ wechselt immer zwischen Eingabe und Liste. Umgesetzt: ++tab++ geht Eingabe → Liste, ++esc++ Liste → Eingabe, und ++tab++ aus der Liste wechselt die Ansicht. Grund: die App fragt eine Ansicht nur mit einem `Focused() bool`, ob sie die Tastatur hält; ein Umschalter in beide Richtungen bräuchte einen reicheren Vertrag.
- **Zeitfilter** der Suche startet bei `all`, nicht bei 90 Tagen. **Repo-Filter** wird mit ++enter++ übernommen, nicht beim Tippen.
- **Detailansicht** ist eine Ebene über den Ansichten (die Suche bleibt darunter erhalten), kein dritter Tab. Status und Position stehen in der Trennlinie, nicht in der Fußzeile.
- **Umbruch und Hervorhebung** macht die Session-Ansicht selbst: der Soft-Wrap des Bubbles-Viewports trennt mitten im Wort.
- **Heatmap** nimmt die Kennzahl des Charts (events, human, sessions) und blendet sich aus, wenn der Körper zu niedrig ist; bei 80×24 fehlt der Platz. Die Statuszeile sagt es.
- **Kitty:** Sondierung mit 250 ms Timeout statt DA1 als Abschluss (Verhalten der Bibliothek), Abfrage-ID der Bibliothek statt 31. Die Umgebungsvariablen entscheiden mit, nicht nur die Sondierung: Bild nur bei Ghostty oder kitty, weil andere Terminals zwar antworten, aber keine Platzhalter zeichnen. tmux ersetzt `TERM` und `TERM_PROGRAM`, deshalb zählen zusätzlich `TERM=xterm-ghostty` sowie `GHOSTTY_RESOURCES_DIR`/`GHOSTTY_BIN_DIR` im tmux-Server als Ghostty-Hinweis. Zwei Umwege statt Änderungen an chartpicture: das Bildmodell wird bei der Zellgrößen-Antwort neu gebaut (chartpicture nimmt die Zellgröße nur im Konstruktor), und die Setter des Chart-Containers geben `tea.Cmd` zurück, weil chartpicture außerhalb der Hauptschleife rendert.
- **Projektnamen:** `project.Names` kann Verzeichnisse ohne Repository nicht mit `~` markieren; der Rohschlüssel sagt nicht, ob er Git-Root oder Arbeitsverzeichnis ist. Behoben wird das durch die Projekt-Ebene aus [#4](https://github.com/seletz/total-recall/issues/4).
- **TopN** wird über das ganze Fenster berechnet, nicht je Periode (stabile Legende). Leere Zellen sind enthalten. Die Spalte `sessions` der Tabelle summiert die je Periode eindeutigen Sessions.

### Messwerte

Alle Messungen lesend gegen die echte Datenbank, darwin/arm64, Go 1.27.3, `CGO_ENABLED=0`; nur Zahlen, keine Inhalte. Datenbank: 680 MiB, 314 Sessions, 179.707 Events, Schema v3. Alle Abfragen mit `Europe/Berlin`. Angaben als Minimum und Median aus fünf Läufen, Startzeiten aus einem Pseudo-Terminal (`expect`) mit `--graphics text`.

**Start der TUI**

| Messung | Wert |
|---|---|
| Prozessstart bis erster Frame | 30 bis 36 ms |
| Start mit `--query` (häufiges Wort) bis erste Treffer | 80 bis 120 ms, davon Abfrage 60 bis 90 ms |
| Start in `stats` bis erstes Chart | 280 bis 300 ms, davon Abfrage 255 bis 280 ms |

**`Store.Search`** (Limit 200, alle Treffer bei 200 abgeschnitten)

| Begriff | ohne Zeitfilter (min / median) | letzte 90 Tage |
|---|---|---|
| häufiges Fachwort (z.B. `release`) | 76 / 176 ms | 71 / 96 ms |
| sehr häufiges Wort (z.B. `test`) | 238 / 289 ms | 158 / 170 ms |
| `error` | 62 / 145 ms | – |
| `git` | 167 / 184 ms | – |
| `config` | 110 / 116 ms | – |
| Stoppwort (`the`) | 312 / 344 ms | – |

**`Store.ActivitySeries`** (TopN 6)

| Fenster | Buckets | min / median |
|---|---|---|
| Tag × Projekt, 28 Tage | 28 | 316 / 368 ms |
| Woche × Agent, 16 Wochen | 16 | 341 / 424 ms |
| Monat × Verzeichnis, 12 Monate | 12 | 366 / 443 ms |
| Tag × Projekt, 224 Tage (achtfaches Fenster) | 224 | 369 / 454 ms |
| Monat × Projekt, gesamter Bestand | 6 | 317 / 335 ms |

Die Dauer hängt kaum vom Fenster ab: die Abfrage liest alle Events im Zeitraum, und der Bestand ist noch klein genug, dass ein Monat und ein Jahr ähnlich lange dauern. Der Entwurf hatte „unter 0,4 s“ angenommen; das trifft zu, aber ohne viel Luft. Die TUI zeigt die gemessene Dauer in der Statuszeile und cacht je Kombination.

**`Store.Stats`** (Heatmap): 28 Tage 268 / 271 ms, 16 Wochen 300 / 306 ms, 12 Monate 300 / 320 ms.

**`Store.Events`** (Vorschau und Detail): Sessions mit 8 bis 17 Nachrichten unter 1 ms. Größte Session im Bestand: 5.508 Nachrichten in 35 ms, mit Tools 27.978 Events in 71 ms.

**Binärgröße** (`go build ./cmd/total-recall`, darwin/arm64)

| Build | v0.1.1 | develop | Differenz |
|---|---|---|---|
| mit `-s -w` (wie Release und `mise run build`) | 8,55 MiB | 12,05 MiB | +3,50 MiB |
| ohne Strip | 12,67 MiB | 17,21 MiB | +4,54 MiB |
| Module in `go list -m all` | 34 | 69 | +35 |

Der Bildpfad (chartpicture, go-analyze/charts mit eingebetteten Schriften) macht davon etwa ein Drittel aus.

### Erkenntnisse aus den Tests

- Golden-Tests mit teatest sind unzuverlässig, sobald sie auf Nachrichten oder auf Text in Bildschirmbereichen warten, die sich nicht ändern. Stabil ist: das Model direkt treiben, `tea.Cmd` synchron ausführen (Statistik und Session nach [#35](https://github.com/seletz/total-recall/issues/35)/[#36](https://github.com/seletz/total-recall/pull/36)) oder den Model-Zustand abfragen statt den Bildschirm.
- `TestOutput` in `internal/tui` schlägt nur unter `-race` fehl (Timing des Renderers); CI läuft ohne `-race`.
- Session- und Statistik-Tests haben je eine eigene Kopie eines `run`/`drive`-Helfers, der Kommandos synchron abarbeitet. Gehört nach `internal/tui/tuitest`.

### Empfehlung

**Übernehmen, mit Folgearbeiten.** Die Suche mit Vorschau und Sprung in die Session ist der Teil, den die CLI nicht ersetzen kann, und er ist fertig, schnell (Treffer in 100 bis 350 ms, Session-Detail unter 100 ms) und gut getestet. Die Statistik liefert in 0,3 bis 0,45 s, was `stats` heute nicht kann (Zeitreihe nach Projekt oder Agent), und `ActivitySeries` ist unabhängig von der TUI nützlich. Die Architektur hält, was der Entwurf versprach: alles unter `internal/tui`, der Rest des Programms an drei Stellen berührt, Ansichten in eigenen Paketen, die Grafikentscheidung als reine Funktion. Kosten: 3,5 MiB und 35 Module mehr, zwei Abhängigkeiten ohne Tag (Ultraviolet, teatest), eine Bibliothek mit API-Warnung (ntcharts).

Die Übernahme sollte an zwei Bedingungen hängen: Erstens die manuelle Kitty-Checkliste aus PR #38 in Ghostty durchgehen. Scheitert der Bildpfad in der Praxis und lässt er sich nicht mit vertretbarem Aufwand reparieren, ist die Konsequenz nicht, die TUI zu verwerfen, sondern den Bildpfad: chartpicture, go-analyze/charts und `kitty.go` entfernen, Textcharts behalten, etwa 1 MiB sparen. Zweitens die Versionen gepinnt lassen und Updates der Charm-Module bewusst einspielen, weil Golden-Dateien sich mit dem Renderer ändern.

Verwerfen wäre richtig, wenn die TUI nicht benutzt wird. Das lässt sich nach ein paar Wochen Gebrauch entscheiden; die [Entfernungsanleitung](#entfernungsanleitung) bleibt gültig.

### Entfernungsanleitung

Falls der Spike wieder herausgenommen wird:

1. `internal/tui` komplett löschen, dazu `internal/cli/tui.go`.
2. In `internal/cli/root.go` die Zeile `e.tuiCmd(),` aus der Befehlsliste entfernen; in `internal/cli/cli_test.go` die Tests für `tui --help` und die ungültigen `tui`-Aufrufe.
3. `go mod tidy`. Damit verschwinden Bubble Tea, Bubbles, Lip Gloss, Ultraviolet, `x/ansi`, `x/exp/golden`, teatest, ntcharts, chartpicture, go-analyze/charts und colorprofile; kein anderes Paket benutzt sie.
4. `docs/tui.md` löschen, den Eintrag in `zensical.toml` und den Abschnitt `tui` in `docs/befehle.md` entfernen. Diese Seite bleibt als Protokoll.
5. Bleiben können `internal/store/activity.go` samt Tests und `internal/testfixture` (Aktivitäts-Fixture, `fixturedb`): additive Dateien ohne Abhängigkeiten nach außen, Grundlage für `stats --period/--by`. Wird die Projekt-Heuristik dafür gebraucht, `internal/tui/project` vorher nach `internal/project` verschieben.
6. `go test ./...`, `go vet ./...`, `mise run docs:build`.

### Vorschlag: `stats --period/--by`

`Store.ActivitySeries` ist fertig und gemessen; `stats` kann es ohne TUI nutzen und damit `monthly_stats.py` im Vault ersetzen. Vorschlag:

```sh
total-recall stats --period week --by project --since 2026-08-01 --until 2026-10-31
total-recall stats --period month --by agent --top 6 --json
total-recall stats --month 2026-10 --period day --by directory
```

| Option | Bedeutung |
|---|---|
| `--period day\|week\|month` | Perioden statt der heutigen Tagesliste; ohne `--by` eine Zeile je Periode mit Sessions, human, agent |
| `--by project\|agent\|directory` | zusätzlich je Periode nach Dimension aufschlüsseln; setzt `--period` voraus (Default `day`) |
| `--top N` | die N größten Werte der Dimension über den ganzen Zeitraum, der Rest als `other`; Default 0 = alle |

Ohne die neuen Flags bleibt `stats` wie heute. Textausgabe im Langformat `PERIOD  KEY  SESSIONS  HUMAN  AGENT` (eine Zeile je Periode und Schlüssel, leere Zellen ausgelassen, `other` zuletzt), damit sie mit `sort` und `awk` weiterverarbeitet werden kann. JSON spiegelt `ActivitySeries`:

```json
{
  "period": "week",
  "by": "project",
  "timezone": "Europe/Berlin",
  "keys": ["webshop", "infra", "other"],
  "buckets": [
    {"start": "2026-10-04T22:00:00.000Z", "label": "2026-W41",
     "cells": [{"key": "webshop", "sessions": 12, "human": 240, "agent": 4100}]}
  ]
}
```

Für `--by project` braucht `stats` die Namensbildung aus `internal/tui/project`, die dazu nach `internal/project` wandert; mit [#4](https://github.com/seletz/total-recall/issues/4) wird sie durch die dort definierte Zuordnung ersetzt, die Schnittstelle (Schlüssel → Name) bleibt. Für die Tabelle wäre zusätzlich eine über den Zeitraum eindeutige Session-Zählung je Schlüssel nützlich; das ist eine zweite Abfrage oder eine Erweiterung von `ActivitySeries`.

### Folge-Issues (Vorschlag)

Nicht angelegt, nur gelistet:

1. **Kitty-Checkliste aus PR #38** in Ghostty (nativ, tmux mit/ohne Passthrough), Terminal.app und bei 80×24 durchgehen; Ergebnis hier nachtragen. Entscheidet über den Bildpfad.
2. **`tuitest`-Helfer:** den doppelten `run`/`drive`-Helfer aus Session- und Statistik-Tests nach `internal/tui/tuitest` heben.
3. **`TestOutput` unter `-race`** deterministisch machen oder CI mit `-race` laufen lassen.
4. **Fokus-Vertrag:** ++tab++ in der Suche wie im Entwurf in beide Richtungen, dafür `Focused` um die Richtung erweitern; oder das heutige Verhalten als endgültig festschreiben.
5. **Projektnamen mit `~`** für Verzeichnisse ohne Repository, sobald [#4](https://github.com/seletz/total-recall/issues/4) die Projekt-Ebene liefert; TUI als Konsument.
6. **`stats --period/--by/--top`** wie oben, inklusive Ablösung von `monthly_stats.py`.
7. **Eindeutige Sessions je Fenster** in der Statistik-Tabelle (Store-Erweiterung).
8. **Config `[tui]`:** `graphics`, Standard-Zeitfilter der Suche (`all` oder 90 Tage), Startansicht.
9. **Abhängigkeiten:** Ultraviolet und teatest auf getaggte Versionen heben, sobald es sie gibt; Charm-Updates mit Golden-Refresh als eigener Commit.
10. **Heatmap bei 80×24:** statt ausblenden Chart und Heatmap abwechselnd zeigen oder den Körper scrollbar machen.

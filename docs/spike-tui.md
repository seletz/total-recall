# Spike: TUI

Entwurf für eine Terminal-Oberfläche `total-recall tui`: interaktive Volltextsuche mit Vorschau und grafische Auswertung der Aktivität über die Zeit. Stand der Recherche: 1. November 2026. Ob der Spike übernommen wird, ist offen; er bleibt deshalb ein eigenständiges Unterkommando, das sich wieder entfernen lässt.

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
internal/tui/project         Projekt-Heuristik (reine Funktion)
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

Dimension `DimAgent` liefert `<harness> main` für Sessions ohne `parent_id` und `<harness> subagent` für Sessions mit `parent_id`. Dimension `DimDirectory` liefert `cwd`. Dimension `DimProject` liefert den Rohschlüssel (`git_root`, sonst `cwd`); die Abbildung auf einen Projektnamen macht `internal/tui/project`, damit der Store keine Darstellungsregeln kennt. Kennzahlen je Zelle: Sessions (distinct), human-Events, agent-Events. Weitere Kennzahlen (Tokens, aktive Minuten) sind [offen](#offene-fragen).

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

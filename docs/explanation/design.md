---
title: "Design (Spezifikation)"
icon: lucide/drafting-compass
---
# Design

`total-recall` archiviert die Sessions von Coding-Agenten (Claude Code zuerst, später weitere Harnesses) in eine lokale SQLite-Datenbank, damit sie nicht verloren gehen und sich auswerten lassen. Es ist harness-neutral: Jede Harness bekommt einen dünnen Adapter, alles andere ist gemeinsam.

## Ziele

- Nichts geht verloren, auch wenn die Harness ihre Transkripte löscht (Claude Code: `cleanupPeriodDays`).
- Je Session: Name, Anfragen und Antworten, Zeiten, Verzeichnis, Git-Repo-URL und Branch.
- Auswertbar von Menschen (CLI, Doku) und von Agenten (`total-recall llm --skill` gibt einen Skill aus, `total-recall sql` erlaubt Read-only-SQL).
- Kein Kopierjob, kein Daemon. Eingelesen wird über Hooks der Harness, inkrementell, plus Nachlesen beim Session-Start.

## Nicht-Ziele (v0.1)

- Keine Kontext-Injektion in laufende Sessions (anders als claude-recall).
- Keine semantische Suche, keine Embeddings. Volltextsuche über SQLite FTS5 reicht.
- Keine weiteren Harnesses außer Claude Code; die Adapter-Schnittstelle ist aber fertig.

## Technik

- Go, statisch gebaut (`CGO_ENABLED=0`). SQLite über `modernc.org/sqlite` (pure Go, FTS5 enthalten).
- CLI mit `github.com/spf13/cobra`. Jeder lesende Befehl hat `--json`.
- Releases über goreleaser, Homebrew-Cask im Tap `seletz/homebrew-tap`. Die Release Notes je Version stehen in `docs/reference/90-aenderungen.md`; der Release-Workflow stellt den Abschnitt des Tags (`scripts/release-notes.sh`, `--release-header`) vor den generierten Commit-Changelog. `scripts/release.sh` taggt nur, wenn der Abschnitt existiert. Jedes Archiv hat eine SBOM (CycloneDX 1.6, JSON) aus `cyclonedx-gomod bin` gegen das fertige Binary, erzeugt im `sboms:`-Block von goreleaser über `tools/sbom` (`internal/sbom`): Hauptkomponente `total-recall` ohne `v`, purls mit `GOOS`/`GOARCH` der Zielplattform, `GOPROXY=off`; alle SBOMs stehen in `checksums-<tag>.txt`. `tools/sbom check` prüft Dateinamen, Kopf, Hauptkomponente, Plattform, Modulhashes gegen `go.sum` und die Checksummen; der Release-Lauf prüft jede SBOM vor Prüfsummen und Upload, die CI jeden Pull Request am Snapshot (`mise run sbom:check`). Die darwin-Binaries signiert und notarisiert der Block `notarize.macos` (quill, auf dem Linux-Runner) in der Build-Phase von goreleaser, also vor Archiv, SBOM und Prüfsummen; aktiv nur, wenn `release.yml` die Secrets `MACOS_SIGN_P12`, `MACOS_SIGN_PASSWORD`, `MACOS_NOTARY_KEY`, `MACOS_NOTARY_KEY_ID` und `MACOS_NOTARY_ISSUER_ID` liefert, sonst bleiben die Binaries unsigniert (Snapshot, CI, Forks). Der Cask verlässt sich seit v1.3.0 auf die Notarisierung und entfernt das Quarantäne-Attribut nicht mehr (#161). Beschreibung für Benutzer: `docs/reference/50-release.md`.
- Doku mit Zensical unter `docs/`, auf Deutsch, veröffentlicht auf GitHub Pages bei jedem Push nach `develop` (`.github/workflows/docs.yml`).
- Tasks über `mise` (`mise run build|test|lint|demo:db|docs:serve|docs:build|docs:screenshots|docs:screenshots:check|docs:charts|docs:charts:check|release|release:snapshot|sbom:check`).
- **Demo-Archiv.** `mise run demo:db` erzeugt mit `tools/demo` das synthetische Demo-Archiv `build/demo.db` (`internal/demo`): eine erfundene Entwicklerin, fünf erfundene Projekte und ein Worktree, Sessions mit Subagenten von Mai bis Oktober 2026 mit Kernzeit, Abenden, Nächten über Mitternacht, Wochenenden, Feiertagen in Baden-Württemberg (Fronleichnam gearbeitet) und einem Urlaub; die Woche ab 2026-06-01 ist für die Screenshots von Hand gelegt, die Sessions im Oktober zum Release von kompass ausformuliert. Die Erzeugung ist deterministisch (eigener Zufallsgenerator mit festem Seed, feste Uhr `demo.Now`). Das Archiv ist die gemeinsame große Fixture: `fixturedb.Demo` öffnet die mit `TOTAL_RECALL_DEMO_DB` benannte Datei (die mise-Tasks `test` und `test:race` sowie die CI setzen sie) und baut sonst eine eigene Kopie im Temp-Verzeichnis; kleine gezielte Fixtures (`testfixture.Small`, `testfixture.Activity`) bleiben.
- **Screenshots.** `mise run docs:screenshots` erzeugt mit `tools/screenshots` die Bilder der TUI unter `docs/assets/screenshots/` aus dem Demo-Archiv (`internal/screenshot`): das Werkzeug steuert die TUI kopflos mit fester Größe und fester Uhr, zeichnet die Frames selbst als PNG (Zelle für Zelle, eingebettete Schrift Go Mono) und schreibt die Text-Frames als Goldens nach `internal/screenshot/testdata/frames/`. `TestFramesGolden` vergleicht die Frames mit den Goldens, `mise run docs:screenshots:check` (`scripts/screenshots-check.sh`) rendert in ein temporäres Verzeichnis und vergleicht Bilder und Goldens byteweise mit den eingecheckten; die CI ruft es auf. **Wer die TUI ändert, führt `mise run docs:screenshots` aus und checkt Bilder und Goldens ein**, sonst schlagen Test und Prüfung fehl. Das Rendering ist deterministisch (eingebettete Schrift, feste Uhren, reines Go), auf derselben Plattform bis auf das Byte; zwischen arm64 und amd64 rundet der Schrift-Rasterizer einzelne Pixel anders, deshalb vergleicht die CI nur die Goldens (`--goldens-only`), und wer nicht auf einem arm64-Mac arbeitet, nutzt die Option ebenfalls. Zeigt die Doku eine Ansicht oder einen Zustand der TUI, gibt es dafür einen `Shot` in `internal/screenshot/frames.go`; Skizzen von Hand gibt es nur noch im [Spike](spike-tui.md#bildschirme) als Entwurf. `tools/anonymize` kopiert optional ein echtes Archiv ohne Namen und Inhalte, um die eigene Statistik anzusehen; die Doku nutzt es nicht.
- **Grafiken.** `total-recall chart` zeichnet die Grafiken eines Monatsberichts (`internal/figure`: Kalender, Anteil je Projekt, Punchcard, Arbeitszeit je Woche) über eine kleine renderer-neutrale Szene (`internal/canvas`: Rechtecke, Linien, Kreise, Text in CSS-Pixeln), die als SVG geschrieben (eigener Writer, Zahlen auf zwei Stellen gerundet, Text damit stabil für Golden-Tests) oder in reinem Go gerastert wird (`golang.org/x/image/vector` für die Formen, eingebettete Go Sans für den Text, keine neue Abhängigkeit). Die Figuren sind reine Funktionen über einfache Eingabetypen, die CLI holt die Daten aus `store.Stats`, `store.ActivitySeries` und `worktime.Compute`. Beschriftungen kommen aus einer kleinen Tabelle je Sprache (`figure.Lang`, `en` und `de`: Titel, Legenden, Wochentage, Monatsnamen, Datums- und Zahlenformat, keine i18n-Bibliothek); `[chart] lang` bzw. `--lang` wählt sie, `--title` ersetzt die Überschrift, Dateinamen sind sprachunabhängig. `mise run docs:charts` erzeugt mit `tools/charts` die Beispielbilder der Doku unter `docs/assets/charts/` (Englisch) und `docs/assets/charts/de/` (Deutsch) aus dem Demo-Archiv (Oktober 2026, feste Uhr, Kategorien mit Farben); `mise run docs:charts:check` (`scripts/charts-check.sh`) rendert in ein temporäres Verzeichnis und vergleicht byteweise, die CI ruft es auf. Die SVG-Dateien sind plattformunabhängig stabil; PNGs prüfen die Tests nur über Maße und Stichproben, weil der Schrift-Rasterizer je Plattform einzelne Pixel anders rundet (siehe Screenshots). **Wer eine Grafik ändert, führt `mise run docs:charts` aus und checkt `docs/assets/charts/` ein.**
- TDD, tabellengetriebene Tests. Testdaten sind synthetisch. **Nie echte Transkripte ins Repo.**

## Pfade

| Was | Default | Override |
|---|---|---|
| Datenbank | `~/Library/Application Support/total-recall/archive.db` (macOS), `$XDG_DATA_HOME/total-recall/archive.db` (Linux) | `--db`, `TOTAL_RECALL_DB` |
| Backups | `~/Documents/total-recall/` (iCloud) | `--backup-dir`, `TOTAL_RECALL_BACKUP_DIR` |
| Logs (Hook-Fehler) | neben der DB, `total-recall.log`; über 5 MiB wird es nach `total-recall.log.1` rotiert (eine alte Generation) | `log` |
| Claude-Code-Transkripte | `~/.claude/projects/**/*.jsonl` (inkl. `<session>/subagents/**`) | `CLAUDE_CONFIG_DIR` |
| Claude-Code-Datei-Historie | `~/.claude/file-history/<session>/<hash>@v<N>` (nur gelesen) | neben `projects_dir`, also `CLAUDE_CONFIG_DIR` |

Die laufende DB liegt bewusst **nicht** in iCloud: SQLite-WAL-Dateien und iCloud-Sync vertragen sich nicht. `backup` schreibt per `VACUUM INTO` eine konsistente Kopie in eine Temporärdatei neben der DB, komprimiert sie mit zstd (`klauspost/compress`, Stufe `SpeedBetterCompression`, höchstens 4 Threads) nach `~/Documents/total-recall/archive-YYYY-MM-DD.db.zst` (erst `.tmp`, dann atomar unter den endgültigen Namen, ohne eine vorhandene Datei zu ersetzen: harter Link, ohne harte Links eine reservierte leere Datei und `rename`). Ein neues Backup ersetzt nie ein vorhandenes. Hat der Tag schon eine Kopie in irgendeinem Format, heißt die neue `archive-YYYY-MM-DD-HHMM.db.zst`, in derselben Minute `archive-YYYY-MM-DD-HHMM-2.db.zst`; ein Backup, das parallel denselben Namen nimmt, weicht auf den nächsten aus. Der Zeitpunkt einer Kopie steht an genau einer Stelle (`store.backupTime`, [#209](https://github.com/seletz/total-recall/issues/209)): Datum und Uhrzeit aus dem Namen in der konfigurierten Zeitzone, bei einem reinen Tagesnamen die Änderungszeit der Datei. Liegt die Änderungszeit nicht auf dem Tag des Namens, wurde die Datei später kopiert oder wiederhergestellt; dann zählt der Beginn dieses Tages, eine Kopie verlässt ihren Tag also nie. `status`, Config-Seite, Setup, `--if-older` und die Aufbewahrung ordnen danach (`backupFiles`), auf die Minute genau, der Genauigkeit der Namen; innerhalb einer Minute gilt der Tagesname als erste Kopie, dann die Nummer. Der Tagesname hat Vorrang nur bei der Vergabe (`store.NextBackupName`): Neue Kopien bekommen ihn nur als erste des Tages, in älteren Ordnern kann er aber auch die späteste sein. Namen, die kein gültiges Datum tragen, gelten nicht als Kopie. Gelöscht wird nur über die Aufbewahrung, die ältesten zuerst, jede Kopie mit einer Zeile im Log: alle Kopien der letzten 7 Tage mit einer Kopie plus die jeweils letzte Kopie jedes Monats. `keep_daily` zählt Tage, nicht Dateien, sonst würden mehrere Backups an einem Tag die Vortage verdrängen. Die gzip-Kopien `archive-YYYY-MM-DD.db.gz` von bis v1.2 und unkomprimierte `archive-YYYY-MM-DD.db` noch älterer Versionen zählen mit. zstd statt gzip seit v1.3.0: Auf einer Kopie eines echten Archivs (444 MiB nach `VACUUM INTO`) schrieb gzip 245 MiB in 6,3 s, zstd in dieser Stufe 220 MiB in 1,7 bis 2,5 s; das Entpacken ging von 2,0 s auf 0,4 s. `restore BACKUP_FILE [--to PATH] [--force]` entpackt eine Kopie in eine Datenbankdatei (Default: die konfigurierte DB), prüft sie (Schema-Version, `PRAGMA quick_check`) und überschreibt ein bestehendes Ziel nur mit `--force`.

## Konfiguration

Eine TOML-Datei für alles: `~/.config/total-recall/config.toml` (`$XDG_CONFIG_HOME` wird beachtet), Override per `--config` oder `TOTAL_RECALL_CONFIG`. Fehlt sie, gelten die Defaults. Rangfolge: Flag > Umgebungsvariable > Config-Datei > Default. `total-recall config show` zeigt die wirksame Konfiguration mit Herkunft je Wert, `total-recall config init` schreibt eine kommentierte Vorlage (überschreibt nie eine bestehende Datei). Die Datei wird über die dotfiles verwaltet.

Die TUI (Ansicht Config, Seiten Settings und Projects) schreibt einzelne Schlüssel und die Projektdefinitionen in die Datei. Dafür gibt es `internal/config/tomledit`, einen zeilenbasierten Editor, der die Datei nicht neu serialisiert, sondern nur die betroffene Zeile ändert: `Set` ersetzt den Wert eines Schlüssels, aktiviert einen auskommentierten (`# key = …`, auch einen auskommentierten Tabellenkopf der Vorlage), hängt einen fehlenden Schlüssel an seine Tabelle oder legt die Tabelle an; `Unset` kommentiert aus; `AppendTable`, `SetAt`, `UnsetAt`, `RemoveTable`, `MoveTable` und `Count` bedienen Tabellen-Arrays (`[[project]]`, `[[worktime.category]]`); `Diff` vergleicht zwei Dateien als flache Schlüsselmengen, `Save` schreibt `<datei>.bak` und dann atomar. `config.SetValue`/`Reset` parsen den Text nach dem Typ des Schlüssels (`config.Catalog`: Art, erlaubte Werte, Hilfetext je Schlüssel), lassen den Editor schreiben, prüfen mit `Diff`, dass sich genau dieser Schlüssel geändert hat, laden den Text wie `Load` und lehnen ab, was einen neuen Fehler erzeugt (`Problems`, nur neue). `config.AddProject`/`SetProject`/`RemoveProject`/`MoveProject` schreiben eine Definition `[[project]]` als Ganzes auf demselben Weg (der `Diff` darf nur `project` nennen; eine geleerte Beschreibung oder Farbe wird auskommentiert, ein Muster als Text, mehrere als Liste). `[accounts]` und `[[worktime.category]]` ändert die TUI nicht, sie öffnet `$EDITOR`.

```toml
# ~/.config/total-recall/config.toml

db = "~/Library/Application Support/total-recall/archive.db"
timezone = "Europe/Berlin"      # für Auswertungen (stats, Tagesgrenzen)
day_start = "04:00"              # Tagesgrenze für die Aktivität je Tag (stats --workday, TUI)
log = ""                         # leer = neben der DB

[backup]
dir = "~/Documents/total-recall"
keep_daily = 7
keep_monthly = 0                 # 0 = alle Monatskopien behalten
min_interval = "24h"             # für `backup --if-older`

[redact]
enabled = true
extra_patterns = []              # zusätzliche Go-Regexe
allow = []                       # Regexe, die nie maskiert werden

[work]
hours = "09:00-17:00"            # Kernzeit (stats --workday, TUI activity)
target = "8h"                    # Tagessoll an Arbeitstagen, Wochenende und Feiertage ohne Soll
daily_low = "6h"                 # Spanne darunter: unter Soll
daily_high = "9h"                # Spanne darüber: deutlich über Soll
weekly_low = "32h"               # Wochenarbeitszeit (5 Arbeitstage) darunter: unter Soll
weekly_high = "48h"              # darüber: deutlich über Soll
bundesland = "Baden-Württemberg" # gesetzliche Feiertage (internal/holiday); unbekannt/leer = bundesweit
count = "human"                  # was die Aktivität je Tag zählt: "human" oder "all"

[worktime]
max_gap = "20m"                  # Lücke zwischen eigenen Nachrichten, die voll zählt (stats --worktime)
min_credit = "3m"                # was eine längere Lücke zählt
grid = "15m"                     # Rundungsraster, 0 = keine Rundung
rounding = "up"                  # up | down | nearest
scope = "category"               # category | project
summary_max_chars = 200          # Länge des Buchungstexts je Tag und Kategorie (stats --worktime --summary), 0 = ohne Grenze
# [[worktime.category]]          # Regeln in Reihenfolge: name, match (Glob oder Liste) auf Projektname, Git-Root (auch mit ~), Remote-URL
# name = "kunde-a"
# match = ["https://github.com/kunde-a/*"]
# color = "blue"                 # optional, Farbe in `chart worktime` (project.ParseColor)

[ingest]
tool_result_max_bytes = 4096
raw_max_bytes = 0                # 0 = unbegrenzt (Default); größere Zeilen: raw = NULL, raw_truncated = 1
store_blobs = true               # Bilder und Dokumente aus den Zeilen als Blobs speichern (false: nur der Verweis bleibt)
store_memory = true              # Projekt-Gedächtnis der Harness versioniert archivieren (Claude Code: memory/*.md je Projekt)
store_file_history = true        # Sicherungskopien bearbeiteter Dateien (Claude Code: file-history) als Blobs, Text maskiert
store_raw = true

[harness.claude]
enabled = true
projects_dir = "~/.claude/projects"   # Default: $CLAUDE_CONFIG_DIR/projects bzw. ~/.claude/projects
skip_record_types = []                # zusätzlich zu den Standard-Ausschlüssen; "attachment/<subtyp>" für einen Attachment-Subtyp

[harness.claude.actors]
# zusätzliche Präfixe für die Actor-Klassifikation
peer_prefixes = []
peer_paste_headers = []          # erste Zeile eines allein eingefügten Blocks, z. B. ["# Task:", "Auftrag:"]
automation_prefixes = ["Hourly ", "Weekend watch"]

[git]
resolve = true

# [[project]]                    # Projekte in Reihenfolge, die erste passende Definition gewinnt (internal/project)
# name = "kompass"               # Pflicht, eindeutig; so heißt das Projekt in stats --by project, --worktime, TUI
# description = "Wanderkarten"   # optional
# color = "blue"                 # optional: Palettenname oder "#rrggbb", sonst automatische Farbe
# match = ["https://github.com/mara/kompass*", "~/src/kompass-*"]   # Glob oder Liste auf Git-Root (auch mit ~), Remote-URL, abgeleiteten Namen

[accounts]                       # Namen für Account-/Organisations-IDs (sessions.account_id, organization_id)
# "aaaaaaaa-0000-4000-8000-000000000001" = "work"

[tui]
view = "search"                  # Ansicht beim Start; Argument geht vor
graphics = "auto"                # auto | text | kitty; --graphics geht vor
mouse = true                     # false = --no-mouse; --no-mouse=false geht vor
stats_period = "day"             # day | week | month
stats_dimension = "project"      # project | agent | directory
activity_window = "week"         # day | week | month
activity_projects = false        # Balken der Aktivität in Projektfarben (p)
activity_newest = "bottom"       # bottom | top: neuester Tag der Aktivität unten oder oben (o)
reload = "5s"                    # Archiv auf Änderungen prüfen, sichtbare Ansicht neu laden; "0" = aus; --reload geht vor

[chart]
format = "svg"                   # svg | png
scale = 1.0                      # Größenfaktor; --width/--height setzen die Größe
title = true
legend = true
transparent = false
top = 12                         # Projekte in `chart projects`, Rest als other; 0 = alle
heatmap = ["#c6dbef", "#9ecae1", "#4292c6", "#08519c"]   # Stufen des Kalenders, hell nach dunkel
lang = "en"                      # Sprache der Beschriftungen: en | de

[keys]
# Aktion = Taste oder Liste von Tasten; nur genannte Aktionen ändern sich.
# Aktionsnamen mit Kontext-Präfix (global_, search_, session_, stats_,
# activity_, config_), Katalog in internal/tui/keys. Tastennamen wie Bubble Tea sie
# meldet (a, G, ?, up, enter, ctrl+r, shift+tab); ctrl+c ist reserviert.
# Je Kontext gehört eine Taste zu einer Aktion; globale Tasten sind in
# keinem Kontext erneut erlaubt. Verstöße sind ein Fehler beim Laden.
# global_quit = ["q", "ctrl+q"]
```

`~` und Umgebungsvariablen werden in Pfaden expandiert. Unbekannte Schlüssel sind ein Fehler (Tippfehler sollen auffallen).

Zwei Stufen der Prüfung: Die Kerneinstellungen (Pfade, Zeitzone, `day_start`, `[backup]`, `[ingest]`, `[redact]`, `[accounts]`, Schlüssel und Typen) prüft `config.Load` für jeden Befehl. Die Tabellen `[work]`, `[worktime]`, `[tui]`, `[chart]` (samt `color` der Kategorien), `[keys]` und die Liste `[[project]]` werden nur geparst und erst beim Gebrauch geprüft (`WorkHours`, `WorktimeSettings`, `ValidateTUI`, `ChartSettings`, `KeyMap`, `ProjectResolver` liefern den Fehler; `Problems` alle sechs für `config show` und `doctor`). Grund: Hooks laden die Config bei jedem Lauf; ein Fehler in einer Tabelle, die nur die TUI, `stats --by project`, `stats --workday` oder `stats --worktime` braucht, darf die Archivierung nicht stoppen.

**Projekte** (`[[project]]`, `internal/project`): Der rohe Schlüssel eines Projekts ist der Git-Root der Session, sonst ihr Arbeitsverzeichnis (Subagenten über die Eltern-Session). Eine Definition hat `name` (Pflicht, eindeutig), `description`, `color` (Palettenname oder `#rrggbb`) und `match` (Glob-Muster wie `[[worktime.category]]`, als Ganzes gegen den Schlüssel als Pfad und mit `~`, die Remote-URL und den abgeleiteten Namen). `project.Resolver` wendet sie an: `Key` liefert je Schlüssel den Namen der ersten passenden Definition, sonst den Schlüssel; mehrere Schlüssel mit demselben Namen sind damit ein Projekt. Die Zuordnung ist berechnet, nicht gespeichert (keine Schemaänderung) und wirkt überall, wo ein Projektname entsteht: `store.ActivitySeries` bekommt sie als Funktion `ActivityFilter.Project` und wendet sie vor dem Zählen und vor `--top` an, `worktime.Compute` über `Options.Projects` vor den Kategorie-Regeln (die den neuen Namen sehen), die TUI-Statistik über `stats.Options.Projects`. Schlüssel ohne Treffer behalten den abgeleiteten Namen (`project.Names`): Basisname des Git-Roots, bei Kollisionen mit übergeordneten Verzeichnissen, Verzeichnisse ohne Repository als Pfad mit `~`; fällt ein abgeleiteter Name mit einem konfigurierten zusammen, erscheint er als Pfad. Farben: `project.Palette` ist die gemeinsame Palette von Charts und Konfiguration; `chart.Colors.Fix` pinnt die konfigurierte Farbe eines Projekts, die automatischen Farben lassen gepinnte aus. Die Aktivitätsansicht übernimmt dieselbe Zuordnung mit #148, die Pflege in der TUI kommt mit dem Konfigurationsreiter (#146).

## Architektur

```
cmd/total-recall          CLI (cobra), dünn
internal/model            gemeinsame Typen: Session, Event, Record
internal/harness          Adapter-Interface + Registry
internal/harness/claude   Adapter Claude Code (JSONL)
internal/gitinfo          cwd -> git root, remote URL, branch (mit Cache)
internal/redact           Maskieren von Geheimnissen
internal/store            SQLite: Schema, Migrationen, Upserts, Abfragen, Backup
internal/store/rawz       zstd-Kompression von events.raw mit eingebetteten Wörterbüchern (tools/rawdict trainiert sie aus synthetischen Zeilen)
internal/blobref          Verweise `blob:sha256:<hex>` in einer JSON-Zeile: Base64-Anhänge herausnehmen (byte-erhaltend) und wieder einsetzen, JSON-Werte herausheben (`Lift`), Verweise auf Daten außerhalb der Zeile ergänzen (`Annotate`), harness-neutral
internal/project          Projekte: Definitionen aus `[[project]]` (Glob-Muster, Farben, Palette) und ihr Resolver, sonst Projektname aus Git-Root bzw. Verzeichnis, Home als `~` (reine Funktionen; `stats --by project`, `--worktime`, TUI)
internal/holiday          gesetzliche Feiertage je Bundesland (github.com/wlbr/feiertage), TUI-unabhängig; erfüllt `store.Holidays`
internal/canvas           renderer-neutrale Szene (Rechtecke, Linien, Kreise, Text) mit SVG-Writer und PNG-Rasterizer in reinem Go (`chart`)
internal/figure           die Grafiken von `chart` als reine Funktionen über Szenen: Kalender, Anteil je Projekt, Punchcard, Arbeitszeit je Woche
internal/worktime         Arbeitszeit je Tag, Kategorie und Projekt aus den Lücken zwischen eigenen Nachrichten (reine Funktionen; `stats --worktime`): Gutschrift, Kategorie-Regeln (Glob), Rundung
internal/ingest           Orchestrierung: Quellen finden, Offsets, Locking, Store schreiben
internal/skill            eingebetteter SKILL.md für `llm --skill`
internal/config           TOML laden, Defaults, Env/Flag-Overrides, Pfad-Expansion; Catalog (Art und Hilfetext je Schlüssel), SetValue/Reset schreiben einzelne Schlüssel, AddProject/SetProject/RemoveProject/MoveProject die Definitionen
internal/config/tomledit  TOML-Datei zeilenweise ändern, Kommentare bleiben: Set/Unset, Tabellen-Arrays (anhängen, setzen, auskommentieren, entfernen, verschieben), Diff, Save mit .bak und atomar
internal/logfile          Log-Datei der Hooks und Befehle: Logger mit Rotation, ERROR-Zeilen lesen (status, TUI)
internal/ops              Verdrahtung der schreibenden Abläufe für CLI und TUI: Store mit Wartungs-Lock öffnen, ingest.Deps, Adapter, Wartungsoptionen, Hook-Block, Formatierung der Zusammenfassungen
internal/tui              TUI: App-Shell, Keymap, Maus; Views search, session, stats, activity, configview (Status, Settings, Projects, Actions, Setup) sowie chart, theme, window, zone, tuitest in Unterpaketen
internal/tui/keys         Katalog der Tasten-Aktionen (Name, Kontext, Default, Hilfetext), Validierung von [keys]
internal/version          Version via ldflags
```

Datenfluss: `ingest` fragt den Adapter nach Quelldateien, liest je Datei ab dem gespeicherten Byte-Offset, der Adapter liefert `model.Record`s (Session-Fakten, Events und [Meta-Records](#session_meta); `raw` ist die ganze Zeile, Bild- und Dokumentdaten schon als Anhänge herausgelöst, `internal/blobref`), `redact` maskiert Text und `raw` (nicht die Anhänge), `store` komprimiert `raw` und schreibt in einer Transaktion Events + Meta-Records + Blobs + Session-Upsert + neuen Offset. Daneben, ohne Offsets: das [Projekt-Gedächtnis](#memory), je Projekt die geänderten Dateien, maskiert, als Fassungen.

## Datenmodell

Alle Zeiten UTC, ISO 8601 (`2026-10-31T22:21:58.123Z`) als TEXT.

### `sources`

Eine Zeile je eingelesener Datei.

| Spalte | Typ | |
|---|---|---|
| `id` | INTEGER PK | Ziel von `events.source_id` (Migration 5) |
| `path` | TEXT UNIQUE | absoluter Pfad |
| `harness` | TEXT | `claude` |
| `size` | INTEGER | Größe beim letzten Lesen |
| `mtime` | TEXT | |
| `offset` | INTEGER | bis hierhin gelesen (Ende der letzten vollständigen Zeile) |
| `ingested_at` | TEXT | |

Ist eine Datei kleiner als `offset` (ersetzt/gekürzt), wird ab 0 neu gelesen; Events sind idempotent. Ein Event, dessen Datei noch keine Zeile hat (Tests, Altbestand aus der Zeit vor Migration 5), legt eine Zeile nur mit `path` und `harness` an (`offset` 0); der nächste Ingest dieser Datei füllt sie.

### `sessions`

| Spalte | Typ | |
|---|---|---|
| `id` | TEXT PK | `<harness>:<native id>`, z.B. `claude:7d1e4c20-…` |
| `harness` | TEXT | |
| `harness_version` | TEXT | zuletzt gesehene Version |
| `native_id` | TEXT | |
| `parent_id` | TEXT NULL | Subagent → Eltern-Session |
| `name` | TEXT NULL | eigener Titel (Claude: `custom-title`), maßgeblich |
| `ai_title` | TEXT NULL | von der Harness erzeugter Titel (Claude: `ai-title`, Legacy `summary`) |
| `agent_name` | TEXT NULL | |
| `display_name` | TEXT, generiert | `COALESCE(name, ai_title, agent_name)`; das zeigen `sessions`, `show`, `search` |
| `cwd` | TEXT | erstes gesehenes Arbeitsverzeichnis |
| `git_root` | TEXT NULL | |
| `git_remote_url` | TEXT NULL | normalisiert, ohne Credentials |
| `git_branch` | TEXT NULL | zuletzt gesehen |
| `model` | TEXT NULL | zuletzt gesehenes Modell |
| `started_at` | TEXT | min(ts) |
| `ended_at` | TEXT | max(ts) |
| `continued_in` | TEXT NULL | Session, in der diese fortgesetzt wurde (Claude: `continued-in`), volle ID mit Präfix; Index, kein Fremdschlüssel, weil die Zielsession später oder nie eingelesen wird (Migration 4) |
| `account_id` | TEXT NULL | Account, auf dem die Session lief (Claude: `session-owner.accountUuid`); Index (Migration 4) |
| `organization_id` | TEXT NULL | Organisation dieses Accounts (Claude: `session-owner.organizationUuid`) (Migration 4) |

Upsert-Regel: `started_at` = Minimum, `ended_at` = Maximum, leere Werte überschreiben nie gesetzte, „zuletzt gesehen“-Felder nehmen den Wert des jüngsten Events; `continued_in`, `account_id`, `organization_id`: der letzte nicht-leere Wert gewinnt. Die Gegenrichtung der Fortsetzung (`continued_from`) ist keine Spalte, sondern eine Abfrage über `continued_in` (`store.SessionInfo.ContinuedFrom`, `sessions --json`); bei mehreren Kandidaten die zuletzt gestartete Session. Beide Records erzeugen kein Event, deshalb lassen sich die Felder nicht aus `events.raw` nachfüllen: ein Archiv aus der Zeit vor Migration 4 bekommt sie durch `ingest --all --reread`, das jede Quelle ab Offset 0 liest (Events idempotent, Sessions gemerged).

### `events`

| Spalte | Typ | |
|---|---|---|
| `seq` | INTEGER PK | Einfügereihenfolge; zugleich `rowid` des Volltextindex (`content_rowid`), deshalb muss der nach einer Migration nicht neu gebaut werden |
| `id` | TEXT UNIQUE | `<harness>:<native event id>`; ohne native ID: `<harness>:<sha1(path)>:<offset>` |
| `session_id` | TEXT FK | |
| `ts` | TEXT | UTC, ISO 8601 mit `T` und `Z` |
| `ts_local` | TEXT | derselbe Zeitpunkt in der Zone `timezone` mit Offset; Tagesgrenzen (`daily_activity`, `stats`) rechnen damit, weil SQLite keine Zeitzonen-Datenbank hat |
| `kind` | TEXT | `message`, `tool_use`, `tool_result`, `system`, `meta` |
| `role` | TEXT | `user`, `assistant`, `system`, `tool` |
| `actor` | TEXT | `human`, `agent`, `peer`, `automation`, `tool` |
| `text` | TEXT | lesbarer Text (redigiert); bei Tool-Ergebnissen gekürzt auf 4 KiB |
| `tool_name` | TEXT NULL | |
| `model` | TEXT NULL | |
| `tokens_in` / `tokens_out` | INTEGER NULL | |
| `source_id` / `source_offset` | INTEGER FK / INTEGER | Rückverweis: `sources.id` (Migration 5, vorher `source_path` TEXT) und Byte-Offset der Zeile |
| `raw` | BLOB NULL | die vollständige Original-Zeile als zstd-Frame (siehe [`raw`](#raw)), redigiert, Bild- und Dokumentdaten durch `blob:sha256:<hex>` ersetzt (siehe [`blobs`](#blobs)); NULL mit `store_raw = false` oder über `ingest.raw_max_bytes` (Default 0 = unbegrenzt) |
| `raw_truncated` | INTEGER | 1, wenn `raw` wegen der Größe weggelassen wurde (Migration 3); `text` bleibt wie sonst. Seit v1.3.0 nur noch Altlast: `ingest --all --reread --rewrite-raw` trägt die Zeile nach, solange das Transkript existiert |

`events_fts`: FTS5 über `text` (external content auf `events` mit `seq` als `rowid`, Trigger für insert, delete und update von `text`).

Die Lesepfade (`store.Events`, `EventWindow`, `Search`, also `show`, `search`, `sessions`, `stats` und die TUI) laden `raw` und `blobs.data` nicht: kein Lesebefehl gibt die Spalten aus, und sie sind mit Abstand die größten. `model.Event.Raw` ist aus dem Store deshalb immer leer. Nur `store.UserMessages` (`reindex --actors`) und `store.RawLine` lesen `raw`, nur `store.Blob` (`total-recall blob`) liest `blobs.data`; `backup` kopiert die Datei. Den Pfad der Quelle holen die Lesepfade über `LEFT JOIN sources` (`store.eventCols`, `eventFrom`).

### `raw` { #raw }

`raw` ist die **vollständige Transkriptzeile**, redigiert und **komprimiert**; nur die Daten von Bildern und Dokumenten stehen nicht in der Zeile, sondern in [`blobs`](#blobs), und die Zeile trägt an ihrer Stelle einen Verweis. Das Archiv steht für sich: wer `raw` und die Blobs hat, kann die Zeile bis aufs Byte wiederherstellen (`store.RawLine`), ohne Transkript. Von Migration 5 bis v1.2 war `raw` ein **Auszug** (Allowlist im Adapter: Record-Felder, `message.content`, `toolUseResult` ohne Diffs, ohne `usage`, `sessionId`, `requestId`, …), und größere Zeilen fielen ganz weg (`raw_max_bytes = 16384`). Beides ist seit v1.3.0 (#180) abgeschafft: das Transkript ist kein Sicherheitsnetz, Claude Code löscht es nach `cleanupPeriodDays`, und was der Auszug wegließ, wäre für immer weg gewesen. `raw_max_bytes` ist 0 (unbegrenzt) und bleibt nur als Notbremse; `raw_truncated` behält die Zeilen, die ein altes Limit verworfen hat, bis `ingest --all --reread --rewrite-raw` sie aus den Transkripten nachträgt (solange die noch da sind). Das Interface `harness.RawTrimmer` und `internal/harness/claude/trim.go` sind entfernt; Migration 5 komprimiert nur noch, sie beschneidet nicht mehr.

**Anhänge (Adapter, `internal/blobref`).** Bilder und Dokumente stehen in der Zeile als Base64 in einem `source`-Objekt der Form `{"type":"base64","media_type":"image/png","data":"…"}`: in `message.content[i].source` (vom Nutzer eingefügt) und in `message.content[i].content[j].source` (Tool-Ergebnis), und Claude Code wiederholt das Bild des Read-Werkzeugs in `toolUseResult.file` (`{"base64":…,"type":"image/png"}`) auf derselben Zeile. `blobref.Extract` läuft mit `json.Decoder` über die Token der Zeile und ersetzt in jedem Objekt, das die Regel des Adapters erkennt (`claude.attachmentRule`: ein `base64`- bzw. `data`-Feld neben einem Medientyp), nur den Datenstring durch `"blob:sha256:<hex>"`; alles andere bleibt Byte für Byte, Whitespace, Escapes und Schlüsselreihenfolge eingeschlossen, deshalb ist `blobref.Restore` (Verweis → Daten, wieder Base64) exakt invers, und `Extract` auf einer schon ersetzten Zeile ist idempotent. Der Hash ist der SHA-256 der dekodierten Daten (zweimal dasselbe Bild auf einer Zeile → ein Blob, zwei Verweise). Dokumente mit Textquelle (`{"type":"text"}`) bleiben in der Zeile und werden wie Text redigiert. Der Adapter hängt die Anhänge als `model.Attachment` (Hash, Medientyp, Daten) an das Event, zu dem der Block gehört (Blockindex → Event `#i`, `toolUseResult.*` → das Tool-Ergebnis), und das Event bekommt als Text einen Platzhalter `[image image/png 34,2 KiB]`; vor v1.3.0 ignorierte der Adapter `image`- und `document`-Blöcke, ein `--reread` legt diese Events deshalb neu an. Der Adapter extrahiert vor dem Parse und vor der Redaction: die Anhänge werden **nicht redigiert** (siehe [Redaction](#redaction)), die Verweise überleben die Redaction (Hex ist nie „hohe Entropie“, der Doppelpunkt macht den Verweis zum Mehrfach-Token).

**Kompression (Store).** `internal/store/rawz` ist die eine Stelle, die `raw` schreibt und liest: ein zstd-Frame (`klauspost/compress`, pure Go) mit **Dictionary-ID im Frame-Header**, ohne Prüfsumme, mit Frame-Content-Size (die unkomprimierte Größe steht im Header, `status` schätzt daraus das Verhältnis ohne zu entpacken), Fenster 1 MiB (eine größere Zeile wird trotzdem komprimiert, nur ohne weiter zurückliegende Treffer). Die Wörterbücher sind in das Binary eingebettet (`internal/store/rawz/dict/`): `raw-v1.zdict` (`0x54520001`), trainiert auf den Auszügen von Migration 5, und `raw-v2.zdict` (`0x54520002`), trainiert auf vollständigen Zeilen mit der Buchhaltung der Harness (`sessionId`, `cwd`, `version`, `usage`, `requestId`, Thinking-Blöcke). Der Decoder kennt **alle** je eingebetteten Wörterbücher (`rawz.Dicts`) und Frames ohne Wörterbuch, der Encoder nimmt das letzte. Ein neues Wörterbuch wird angehängt, nie ersetzt: alte Zeilen bleiben lesbar. Trainiert wird **nur aus synthetischen Zeilen** (`mise run dict:train`, `tools/rawdict`: die Zeilen des Demo-Archivs plus generierte Records in den Formen echter Transkripte, deterministisch); nichts aus `~/.claude/projects` kommt ins Repo. `tools/rawdict measure` misst die Kompression für ein Archiv, nur Zahlen. Die Trainingszeilen sind bei jedem Lauf dieselben (`tools/rawdict samples`); der Wörterbuch-Builder von `klauspost/compress` bricht Gleichstände aber in Map-Reihenfolge, zwei Läufe liefern deshalb byteweise verschiedene, gleichwertige Wörterbücher. Maßgeblich ist das eingecheckte; ein neu trainiertes wird mit neuer ID angehängt, nie an die Stelle des alten gesetzt.

In SQL (`sql`, Tests) entpackt die Funktion `raw_text(raw)` (registriert im Treiber beim Laden von `internal/store`); ein Wert, der kein Frame ist (Textspalte eines älteren Archivs), kommt unverändert zurück. Die Verweise bleiben darin stehen; die vollständige Zeile mit Daten liefert nur `store.RawLine` (Go), das die Blobs einsetzt.

### `blobs` und `event_blobs` { #blobs }

Anhänge liegen seit Migration 6 (#181) einmal je Inhalt in `blobs`, die Zuordnung zu Events in `event_blobs`.

| Tabelle | Spalte | Typ | |
|---|---|---|---|
| `blobs` | `sha256` | TEXT PK | Hex-SHA-256 der Daten, wie sie geliefert wurden; derselbe Wert wie im Verweis `blob:sha256:<hex>` in `raw` |
| | `media_type` | TEXT | `image/png`, `image/jpeg`, `application/pdf`, … aus dem `source`-Objekt |
| | `bytes` | INTEGER | Größe der Daten, wie geliefert |
| | `encoding` | TEXT | `''`: `data` ist wie geliefert (PNG, JPEG, GIF, WebP sind schon komprimiert); `zstd`: `data` ist ein zstd-Frame ohne Wörterbuch (PDF, Text, alles andere) |
| | `data` | BLOB | die Daten |
| `event_blobs` | `event_id` | TEXT | `events.id`, ohne Fremdschlüssel: künftige Umbauten von `events` sollen die Tabelle nicht mitziehen; `--rewrite-raw` räumt die Zeilen eines Events auf |
| | `position` | INTEGER | Reihenfolge der Verweise in der Zeile; `(event_id, position)` ist der Primärschlüssel, WITHOUT ROWID |
| | `sha256` | TEXT FK | → `blobs.sha256`, Index `event_blobs_sha256` |

Der Store (`internal/store/blob.go`) schreibt Blobs in derselben Transaktion wie die Events: `INSERT … ON CONFLICT DO NOTHING` auf `blobs` (Dedup über den Hash, `WriteResult.Blobs`/`BlobBytes` zählen nur neue), die Verweise des Events in `event_blobs`; beim Einfügen eines Events und bei `--rewrite-raw` (das die alten Verweise des Events löscht und neu schreibt; Blobs ohne Verweis bleiben stehen). Vor dem Schreiben rechnet der Store den SHA-256 nach und lehnt einen Anhang ab, dessen Hash nicht zu den Daten passt. `[ingest] store_blobs = false` lässt den Ingest die Daten fallen; `raw` behält den Verweis, der Platzhalter im Text bleibt, `blob` meldet dann „not found“. Lesen: `store.Blob` (voller Hash oder eindeutiges Präfix; `ErrNotFound`, `ErrAmbiguous`), `BlobEvents`, `SessionAttachments` (nur Metadaten, eine Abfrage je Session für `show`), `BlobStats` (`status`, TUI), `RawLine`. Kein Lesebefehl außer `blob` lädt `blobs.data`, und `blob` schreibt Binärdaten nur in `--out DATEI` oder eine Pipe, nie auf ein Terminal; `TestReadPathsDoNotLoadRaw` (#135) prüft auch die Spalte `data`. `backup` kopiert die Datei samt Blobs; `maintenance` baut sie mit um und vergleicht die Zahl der Blobs vor und nach dem Umbau. Die TUI zeigt nur den Platzhalter im Text; einen Betrachter gibt es nicht (geplant für v1.4.0), die Lesefunktionen des Stores sind dafür da.

### `session_meta` { #session_meta }

Harness-Records, die keine Nachricht sind, aber zu einer Session gehören (Migration 7, [#182](https://github.com/seletz/total-recall/issues/182)): Kontext, den die Harness in eine Runde eingefügt hat, Text, der während der Arbeit des Agenten getippt wurde, Zustand der Session, Index der Datei-Historie, Workflow-Journal, und jeder Record-Typ, den der Adapter nicht kennt. Das Archiv soll für sich stehen: Ist ein Transkript weg, fehlt nichts, was man nachlesen wollte. Diese Records erzeugen **kein Event**, stehen nicht im Volltextindex und zählen in keiner Statistik (`stats`, `daily_activity`, `exchanges`, Arbeitszeit); die Lesebefehle laden sie nicht, nur `sql` und `status` sehen sie.

| Spalte | Typ | |
|---|---|---|
| `seq` | INTEGER PK | Einfügereihenfolge |
| `id` | TEXT UNIQUE | `<harness>:<native id>`; ohne native ID `<harness>:<sha1(session, typ, inhalt)>` |
| `session_id` | TEXT FK | `sessions.id`; ein Meta-Record einer noch unbekannten Session legt eine leere Session-Zeile an und verändert nie `started_at`/`ended_at` |
| `ts` | TEXT NULL | eigener Zeitstempel des Records, sonst der des zuletzt im selben `Parse`-Lauf gesehenen Records mit Zeitstempel, sonst NULL |
| `type` | TEXT | Record-Typ der Harness (`attachment`, `pr-link`, …); die Tabelle kennt keine Typnamen, nur der Adapter |
| `source_id` / `source_offset` | INTEGER FK / INTEGER | wie bei `events` |
| `raw` | BLOB | die **ganze** Zeile, redigiert mit `Redactor.JSON`, komprimiert wie `events.raw` (`rawz`); `raw_text(raw)` entpackt. Große Werte, die der Adapter herausgenommen hat, stehen als `blob:sha256:<hex>` darin (siehe unten) |

Index `session_meta_session(session_id, type)`. Der Adapter liefert sie als `model.Meta` in `model.Record.Meta`; der Store schreibt sie in derselben Transaktion wie die Events (`store.WriteOptions.Meta`, `INSERT … ON CONFLICT(id) DO NOTHING`), `ingest --reread --rewrite-raw` ersetzt auch ihr `raw`. Sie folgen `ingest.store_raw` (aus: keine Meta-Records) und `ingest.raw_max_bytes` (eine größere Zeile wird mit einer Logzeile verworfen, denn die Zeile ist alles, was ein Meta-Record hat).

**Große Werte als Blobs, einmal über alle Sessions.** Ein Meta-Record kann `model.Attachment`s tragen: JSON-Werte, die der Adapter mit `blobref.Lift` aus der Zeile nimmt (ein Objekt oder Array an einem benannten Pfad, als kompaktes JSON, Medientyp `application/json`), und an deren Stelle in der Zeile der Verweis `blob:sha256:<hex>` steht. Der Store legt sie wie die Anhänge der Events in `blobs` ab, einmal je Hash, und verknüpft sie in `meta_blobs(meta_id, position, sha256)` (wie `event_blobs`). Bei Claude Code sind das `shape` des `api-request-shape`-Records (System-Prompt und Tool-Definitionen einer Request-Form) und `attachment.systemPrompt` des `prompt_snapshot`-Attachments: Beide schreibt die Harness jede Runde neu, lokal je rund 100 MB roh bei ein paar hundert verschiedenen Werten. Die JSON-Blobs laufen durch `Redactor.JSON` (System-Prompts enthalten die geladenen Instruktionsdateien, Umgebung, Pfade); da die Maskierung den Hash ändert, ersetzt `ingest` danach den Verweis in der Zeile. **`ingest.store_blobs = false` lässt sie nicht aus:** Der Schalter verwirft die unmaskierbaren Bilder und Dokumente der Events; die Blobs der Meta-Records sind maskierter Text und der einzige Ort, an dem dieser Inhalt steht. Wer System-Prompts nicht will, nimmt `skip_record_types = ["api-request-shape", "attachment/prompt_snapshot"]`. `blob HASH` gibt den Wert als JSON aus, `blob --json` nennt unter `meta_records` die Records, die ihn tragen. Eine Wiederherstellung der Zeile mit dem Wert an Ort und Stelle gibt es (anders als `RawLine` für Events) nicht; die Zeile mit Verweis und der Blob sind zusammen vollständig.

**Einmal je Inhalt und Session.** Claude Code schreibt Zustands-Records (`mode`, `permission-mode`, `atis-latch`, `pr-link`, `bridge-session`) in jeder Runde neu, auf einem lokalen Archiv 170 000 Zeilen mit ein paar hundert verschiedenen Inhalten. Ein Record ohne `uuid` bekommt deshalb als ID den SHA-1 über Session, Typ und die Zeile als kompaktes JSON mit sortierten Schlüsseln ohne `uuid`, `timestamp`, `parentUuid` (`metaHash`): gleicher Inhalt in derselben Session ist dieselbe Zeile, `ts` ist der des ersten Auftretens. Records mit `uuid` (Attachments) heißen `claude:<uuid>` wie Events. Ein Reread ist damit idempotent.

**Welche Claude-Records** (gemessen auf einem lokalen Archiv, 530 Dateien, 859 000 Zeilen, 3,1 GB; „komprimiert“ nach der Dedup je Session mit dem Archiv-Wörterbuch):

| Typ | Zeilen | roh | komprimiert | Entscheidung |
|---|---|---|---|---|
| `api-request` | 66 160 | 971 MB | 104 MB | übersprungen: Request-Cache der Harness (Hashes der Prompt-Blöcke, API-Parameter), ohne die Blobs wertlos |
| `api-request-blob` | 69 263 | 286 MB | 161 MB | übersprungen: Nachrichteninhalte je Hash, wiederholen die `user`/`assistant`-Events |
| `api-request-shape` | 1 073 | 98 MB | 30 MB | aufgenommen: System-Prompt und Tool-Definitionen je Request-Form; `shape` wird Blob, einmal je Inhalt über alle Sessions (Stefans Entscheidung, Zahlen nach Dedup unten) |
| `attachment` | 123 440 | 307 MB | 89 MB | aufgenommen bis auf sieben Subtypen (40 MB): `total_tokens_reminder`, `bash_output_audience_note`, `deferred_tools_record`, `deferred_tools_delta`, `agent_listing_delta`, `skill_listing`, `mcp_instructions_delta` (Kataloge und Erinnerungen der Harness). `prompt_snapshot` (1 479 Zeilen, 102 MB roh, 23 MB komprimiert) kommt mit, `attachment.systemPrompt` wird Blob wie `shape`. Bleiben daneben ~26 MB in ~50 000 Zeilen: `instructions` (die `CLAUDE.md`-Dateien, wie geladen), `file` (@-erwähnte Dateien), `queued_command` (Text, der während der Arbeit des Agenten eingegeben wurde), `hook_success` (Ausgaben der Hooks), `edited_text_file`, `environment`, `invoked_skills`, `task_status`, `remote_session_change`, `date`, `model`, … Ein unbekannter Subtyp wird aufgenommen |
| `queue-operation` | 26 761 | 17 MB | 8,7 MB | aufgenommen: `enqueue`/`dequeue`/`remove` mit Text; ein entferntes Kommando steht sonst nirgends |
| `last-prompt` | 41 523 | 10 MB | 7,8 MB | übersprungen: wiederholt die letzte eigene Nachricht (Resume-Picker) |
| `file-history-snapshot`, `file-history-delta` | 5 074 + 1 396 | 32 MB | 6,2 MB | aufgenommen: Index der Datei-Historie; die Sicherungskopien, auf die er verweist, kommen als Blobs dazu ([#183](https://github.com/seletz/total-recall/issues/183), siehe unten) |
| `pr-link` | 28 293 | 6,3 MB | 146 KB (865 Zeilen) | aufgenommen: Session ↔ Pull Request |
| `bridge-session`, `permission-mode`, `atis-latch`, `mode` | je ~40 000 | 17 MB | 130 KB (~1 300 Zeilen) | aufgenommen, nach Dedup eine Handvoll je Session |
| `cost-state`, `tools-used`, `history-suppression`, `started`, `result` | 2 000 | 0,9 MB | 0,5 MB | aufgenommen: Kosten und Dauer je Modell, benutzte Tools, Workflow-Journal (`subagents/workflows/*/journal.jsonl`, hängt an der Session des Workflow-Agenten `agent-<agentId>`) |
| unbekannt | | | | aufgenommen; `doctor` meldet den Typ weiter, damit die Listen gepflegt werden |

Summe, gemessen nach dem Reread auf einer Kopie des Archivs: 49 MiB komprimiert in 76 766 Zeilen plus 13 MiB JSON-Blobs (517 Werte: 356 verschiedene Request-Formen aus 951 `api-request-shape`-Records, 161 verschiedene System-Prompts und Tool-Listen aus 1 491 `prompt_snapshot`-Attachments; 38 MiB roh, 200 MB in den Transkripten) kommen ins Archiv, 1,27 GB roh bleiben draußen. Der Reread aller 535 Transkripte (240 000 Events) dauerte 4 min 36 s, siehe [Änderungen](../reference/90-aenderungen.md). Die drei Listen (`ingestedTypes`, `metaTypes`, `skippedTypes` samt `skippedAttachmentTypes`) und die Blob-Pfade (`metaLifts`) stehen an einer Stelle im Adapter (`internal/harness/claude/record.go`); `skip_record_types` ergänzt die Ausschlüsse, auch als `attachment/<subtyp>`.

**Datei-Historie** ([#183](https://github.com/seletz/total-recall/issues/183)). Bevor Claude Code eine Datei bearbeitet, kopiert es sie nach `$CLAUDE_CONFIG_DIR/file-history/<session>/<backupFileName>` (`backupFileName` ist `<16 Hex-Zeichen>@v<N>`, ein Hash des Pfads und die Version) und vermerkt die Kopie im Transkript: `file-history-snapshot` trägt den Index aller Kopien bis dahin (`snapshot.trackedFileBackups`, Schlüssel ist der Pfad der Datei, jede Runde wiederholt und erweitert), `file-history-delta` eine neue Kopie (`backup`). Die Kopien liegen außerhalb der Transkripte, und Claude Code löscht sie mit ihnen; deshalb liest sie der normale Ingest, nicht erst ein Reread. Der Adapter (`internal/harness/claude/filehistory.go`) liest beim Parsen eines dieser Records jede genannte Kopie aus dem Verzeichnis der Session (Name ohne Pfadbestandteile, sonst wird er nicht verfolgt), ergänzt im Objekt der Kopie das Feld `"blob":"blob:sha256:<hex>"` (`blobref.Annotate`: fügt vor der schließenden Klammer ein, alles andere bleibt Byte für Byte) und hängt den Inhalt als `model.Attachment` an den Meta-Record, Medientyp `text/plain` (gültiges UTF-8 ohne NUL) oder `application/octet-stream`. Der Store legt ihn wie die JSON-Blobs ab: einmal je Hash in `blobs`, zstd-komprimiert, verknüpft über `meta_blobs`. Nur der erste Record eines `Parse`-Laufs, der eine Kopie nennt, trägt sie; die Wiederholungen in späteren Snapshots bleiben, wie sie sind (sonst läse jede Runde dieselben Dateien neu). Die ID des Records wird vor dem Ergänzen berechnet: ob die Kopie noch da ist, ändert nicht, welche Zeile er ist. **Text geht durch `Redactor.Text`**, vor dem Hash, damit gleicher maskierter Inhalt gleich gehasht wird; `ingest` ersetzt dann den Verweis in der Zeile wie bei den JSON-Blobs. **Binärdateien** lassen sich nicht maskieren und folgen `ingest.store_blobs` wie Bilder (aus: der Verweis bleibt in der Zeile, die Daten nicht). Kopien über 16 MiB (`claude.DefaultFileHistoryMaxBytes`) bleiben draußen, mit einer Logzeile. Fehlt eine Kopie schon, bleibt der Record ohne Verweis, ohne Fehler; der Adapter zählt sie (`Stats.BackupsMissing`) und schreibt je Transkript eine Logzeile. Damit ein späteres `ingest --reread --rewrite-raw` nicht wegwirft, was das Archiv schon hat, ersetzt der Store die Zeile eines Meta-Records nur, wenn die neue mindestens so viele Blob-Verweise trägt wie die gespeicherte (`keepsMoreRefs`; sonst bleibt der Record, wie er ist, `WriteResult.MetaKept`, in der Ingest-Zusammenfassung „N meta records kept“). Der Pfad der Kopien folgt aus der Konfiguration der Harness: das Verzeichnis neben `harness.claude.projects_dir`, also `$CLAUDE_CONFIG_DIR/file-history`; nur der Adapter kennt ihn und das Format, der Store bleibt harness-neutral (keine Schemaänderung). `[ingest] store_file_history = false` schaltet es ab, ebenso `store_raw = false` (ohne Zeile kein Meta-Record). `status` zählt die Blobs getrennt nach Anhängen der Events, JSON-Werten und übrigen Daten der Meta-Records (`files`, JSON `blob_kinds`); `blob HASH` gibt eine Kopie aus, Text auch auf ein Terminal.

Gemessen lokal (nur lesend, 152 Session-Verzeichnisse): 3 431 Kopien mit 210 MiB, davon 10 binär (0,5 MiB); 3 184 verschiedene Inhalte mit 199 MiB, mit zstd (Stufe 3, ohne Wörterbuch) 71 MiB. Die größte Kopie hat 0,9 MiB, die Grenze von 16 MiB greift lokal nie. Die Records nennen 4 069 Kopien je Transkript, 638 davon sind schon gelöscht; jede Kopie auf der Platte wird von einem Record genannt. Auf einer Kopie des Archivs (549 Transkripte) brachte `ingest --all --reread --rewrite-raw` 3 207 Kopien als Blobs, 199 MiB wie geliefert, 71 MiB gespeichert; die Datei wuchs nach `maintenance` von 762 auf 914 MiB, das Backup von 476 auf 596 MiB, der Reread dauerte 6 min ([Änderungen](../reference/90-aenderungen.md)).

**`usage`**, gemessen an 84 752 Assistant-Antworten: `input_tokens` 1,3 M, `cache_creation_input_tokens` 449 M (davon `ephemeral_1h` 395 M, `ephemeral_5m` 54 M), `cache_read_input_tokens` 23,8 G, `output_tokens` 68 M, davon `thinking_tokens` 15,3 M. `tokens_in` ist die Summe der drei Eingabezahlen und damit zu 99,9 % Cache-Reads; getrennte Spalten für Cache-Lesen und -Schreiben (und Thinking) kommen mit einem eigenen Issue nach [#180](https://github.com/seletz/total-recall/issues/180), wenn `raw` die `usage` wieder enthält und sich die Spalten daraus nachfüllen lassen; `tokens_in` bleibt dann die Summe, damit bestehende Zahlen stabil bleiben.

### Projekt-Gedächtnis { #memory }

Claude Code führt je Projekt ein Gedächtnis aus Markdown-Dateien (`<projects_dir>/<projekt>/memory/*.md` mit dem Index `MEMORY.md`), außerhalb der Transkripte; der Agent ändert und löscht sie laufend. Seit Migration 8 ([#192](https://github.com/seletz/total-recall/issues/192)) archiviert total-recall sie **versioniert**: Ohne Archiv wären frühere Stände und gelöschte Einträge weg. Gemessen lokal: 424 Projektverzeichnisse, 35 davon mit Gedächtnis, 905 Dateien, alle `.md`, keine Unterordner, 1,9 MiB, die größte 31 KiB, Median 1,7 KiB; nach Änderungszeit 31 Dateien jünger als ein Tag, 135 jünger als eine Woche, 282 älter als 90 Tage. 11 der 35 Projekte haben keine Transkripte mehr.

| Tabelle | Spalte | |
|---|---|---|
| `memory_projects` | `id` | INTEGER PK |
| | `harness`, `key` | UNIQUE; `key` ist der native Projektschlüssel des Adapters (Claude Code: der Verzeichnisname) |
| | `cwd` | Arbeitsverzeichnis des Projekts, NULL solange unbekannt; der Git-Root kommt über `dirs` |
| `memory_files` | `id` | INTEGER PK, zugleich `rowid` von `memory_fts` |
| | `project_id`, `path` | UNIQUE; `path` relativ zum Gedächtnis-Verzeichnis, mit `/`, ohne `.`/`..` |
| | `sha256` | aktueller Inhalt (→ `blobs`), NULL = gelöscht |
| | `size`, `mtime` | Stand auf der Platte beim letzten Lauf (mtime auf Millisekunden wie alle Zeiten) |
| `memory_versions` | `seq` | INTEGER PK, Reihenfolge |
| | `file_id` | → `memory_files.id`; Index `(file_id, seq)` |
| | `ts` | Änderungszeit der Datei; bei einer Löschung die Zeit des Laufs |
| | `observed_at` | Zeit des Laufs, der die Fassung aufnahm |
| | `sha256`, `bytes` | Inhalt (→ `blobs`, Index) und Größe; NULL/0 bei einer Löschung |
| `memory_fts` | `text` | FTS5 (`unicode61 remove_diacritics 2` wie `events_fts`) über den aktuellen Inhalt jeder vorhandenen Datei; eigene Inhaltstabelle, kein External Content, weil der Inhalt nur komprimiert in `blobs` liegt |

**Harness-neutral im Store, Pfadwissen im Adapter** (#186). Der Store kennt nur Harness, Projektschlüssel und Pfade relativ zum Gedächtnis-Verzeichnis. Ein Adapter mit Gedächtnis erfüllt `harness.MemorySource`: `Memory()` listet je Projekt die Dateien (Pfad, absoluter Pfad, Größe, mtime) und sagt mit `ok = false`, dass sein Datenverzeichnis ganz fehlt; `MemoryCwd(key, candidates)` liefert das Arbeitsverzeichnis. Claude Code: jedes Verzeichnis unter `projects_dir` mit einem Unterordner `memory`, darin jede reguläre `*.md`-Datei, auch in Unterordnern, ohne versteckte Dateien und symbolische Links; das Verzeichnis kommt aus dem Feld `cwd` eines Transkripts im Projektverzeichnis (die ersten 1 MiB von bis zu drei Dateien), sonst aus den Arbeitsverzeichnissen der Sessions im Archiv, deren Schlüssel passt (`claude.ProjectKey`: jedes Zeichen außer ASCII-Buchstaben und Ziffern wird `-`). Ein gefundenes Verzeichnis wird wie das einer Session über `dirs` zu Git-Root und Remote aufgelöst; die Projektnamen von `memory` entstehen daraus wie bei `stats --by project`.

**Einlesen** (`ingest.Memory`, aus `ingest.All` bei `ingest.store_memory` und aus jedem Hook nach dem Transkript): je Harness ein Lock (`memory:<harness>`, wie die Quellen; gesperrt zählt als `locked`), der gespeicherte Stand (`store.MemoryState`) gegen die Liste des Adapters. Gelesen wird nur eine Datei, deren Größe oder mtime sich geändert hat; ihr Inhalt läuft durch `Redactor.Text` und geht dann an `store.WriteMemory`, das den Hash bildet: gleicher Hash ändert nur `size`/`mtime`, ein neuer schreibt Blob (`text/markdown`, zstd, `ON CONFLICT DO NOTHING`), Version und Index. Eine gespeicherte Datei, die nicht mehr auf der Platte liegt, bekommt eine Löschung, ebenso alle Dateien eines Projekts, dessen Gedächtnis-Verzeichnis fehlt; fehlt das ganze Datenverzeichnis des Adapters, wird nichts gelöscht. Eine Transaktion je Projekt. Gemessen auf einer Kopie des Archivs: der erste Lauf über 905 Dateien 0,63 s, ein Lauf ohne Änderung 10 ms. Frühere Stände vor dem ersten Lauf gibt es nicht.

**Lesen** (für `memory` und die geplante TUI-Ansicht): `MemoryProjects` (mit Git-Root aus `dirs`, Zahl der Dateien, Löschungen, Fassungen, letzte Änderung), `MemoryFiles` (Stand jetzt oder zu `At`: je Datei die Fassung mit der höchsten `seq` und `ts <= At`; optional mit gelöschten), `MemoryVersions` (je Datei, älteste zuerst), `MemoryContent` (Inhalt einer Datei zu einem Zeitpunkt), `SearchMemory` (FTS über die aktuellen Fassungen, je Projekt einschränkbar; die spätere typisierte Suche über alle Objekttypen hängt sich hier ein), `MemoryStats` (`status`), `BlobMemory` (`blob --json`). Keine Zeile des Gedächtnisses zählt in `stats`, `daily_activity`, `exchanges` oder der Arbeitszeit, `search` sieht es nicht.

### Views

- `exchanges`: je menschlicher Nachricht (`actor='human'`) die Anfrage plus die letzte Agenten-Antwort (`role='assistant'`, `kind='message'`) vor der nächsten menschlichen Nachricht derselben Session.
- `daily_activity`: Tag (lokale Zeit Europe/Berlin), Session, Repo, Anzahl human/agent-Events.

### Actor-Klassifikation (Claude Code)

| Fall | actor |
|---|---|
| `type=user`, Text vom Menschen getippt | `human` |
| Text beginnt mit „Another Claude session“, `<agent-message`, `[Cross-session` oder einem Präfix aus `peer_prefixes`; `origin.kind = "peer"` (Nachrichten anderer Sessions über den Socket, `<cross-session-message …>`); Subagent-Hand-back | `peer` |
| Nachricht besteht nur aus einem `<pasted_content …>`-Block, dessen erste nicht leere Zeile mit einem Eintrag aus `peer_paste_headers` beginnt (ein in das Terminal eingefügter Auftrag eines Koordinators) | `peer` |
| `<task-notification>`, geplante Prompts („Hourly …“, Scheduled), `isMeta`, `<command-…>`/`<local-command-…>`, `This session is being continued`, `Base directory for this skill`, `<system-reminder>` | `automation` |
| `<bash-input>` | `human` (der Benutzer hat `!cmd` getippt) |
| `<pasted_content …>` sonst | `human` |
| `type=user` mit `tool_result`-Blöcken | `tool` (kind `tool_result`) |
| `type=assistant` | `agent` (Text → kind `message`, `tool_use`-Block → kind `tool_use`) |

Ein Assistant-Eintrag mit mehreren Content-Blöcken ergibt mehrere Events (`<uuid>#<block index>`).

Reihenfolge: zuerst die Textregeln (Peer-Präfixe, Peer-Paste-Kopfzeilen, Human-Präfixe, Automation-Präfixe), dann `origin.kind`, dann `isMeta`/`isCompactSummary`/`scheduledTaskId`, dann Subagent-Transkript, sonst `human`. Eine Nachricht, die ein Koordinator per Terminal-Multiplexer in die Session tippt oder einfügt, hat keine harness-seitige Markierung (`origin.kind = "human"`, `promptSource = "typed"`); nur die konfigurierten Präfixe und Kopfzeilen unterscheiden sie von eigener Eingabe. `reindex --actors` wendet die aktuellen Regeln auf gespeicherte User-Nachrichten an: Textregeln auf `text`, die Flags aus `raw`; ohne `raw` (nicht gespeichert oder wegen der Größe weggelassen) entscheiden nur die Textregeln, sonst bleibt der Actor stehen.

### Welche Claude-Records

Eingelesen: `user`, `assistant`, `system`, `custom-title`, `ai-title`, `agent-name`, `summary`, `session-owner`, `continued-in`. Von den Content-Blöcken einer User-Nachricht werden `text`, `tool_result`, `image` und `document` verarbeitet; `image` und `document` werden zu einem `message`-Event mit Platzhalter (`[image image/png 34,2 KiB]`), ihre Daten zu Anhängen (siehe [`raw`](#raw), [`blobs`](#blobs)), auch innerhalb eines `tool_result`. Bewusst übersprungen (`claude.SkippedRecordTypes`): `api-request`, `api-request-blob`, `last-prompt`, dazu sieben Attachment-Subtypen (`claude.SkippedAttachmentTypes`); der Offset läuft trotzdem weiter. Alle anderen Typen, bekannte (`claude.MetaRecordTypes`: `attachment`, `api-request-shape`, `queue-operation`, `file-history-*`, `pr-link`, `mode`, `permission-mode`, `cost-state`, …) wie unbekannte, werden als [Meta-Records](#session_meta) aufbewahrt; `doctor` meldet Typen, die in keiner der drei Listen stehen. `custom-title`/`ai-title`/`agent-name`/`summary`/`session-owner`/`continued-in` erzeugen kein Event, sondern setzen Felder der Session (`summary` und `ai-title` → `ai_title`, damit ein eigener Titel nie überschrieben wird; `session-owner` → `account_id`, `organization_id`; `continued-in` → `continued_in` als `claude:<continuedInSessionId>`, ohne den Zeitstempel des Records zu übernehmen, denn er stammt vom Zeitpunkt der Fortsetzung und würde `ended_at` verfälschen).

## Neue Harness anbinden

Eine Harness bekommt ein Paket `internal/harness/<name>` mit einem Typ, der `harness.Adapter` erfüllt (`Name`, `Sources`, `Parse`), und wird in `internal/ops` (`Adapters`) samt eigenem Config-Abschnitt `[harness.<name>]` eingehängt. Was keine Nachricht ist, aber zur Session gehört, liefert der Adapter als `model.Meta` ([`session_meta`](#session_meta)): ganze Zeile, Typname der Harness, ID oder Inhalts-Hash, große Inhalte als Anhänge; der Store kennt keine Typnamen. Optional erfüllt er `harness.Reclassifier` (`reindex --actors`) und `harness.MemorySource`, wenn die Harness ein Gedächtnis je Projekt außerhalb der Transkripte führt ([Projekt-Gedächtnis](#memory)). `model.Event.Raw` ist die ganze Zeile; trägt die Harness Bilder oder Dokumente als Base64 in ihren Zeilen, nimmt der Adapter sie mit `blobref.Extract` und einer eigenen Regel (welche Felder Daten und Medientyp tragen) heraus und hängt sie als `model.Attachment` an die Events (siehe [`raw`](#raw)). Alles andere (Redaction, Git-Info, Store, Locking, Offsets, Kompression, Blobs) ist gemeinsam.

Den Vertrag prüft die Konformitäts-Suite `internal/harness/harnesstest`. Jeder Adapter ruft sie mit synthetischen Fixtures auf:

```go
func TestConformance(t *testing.T) {
	harnesstest.Run(t, func(dir string) harness.Adapter { return New(dir) }, "testdata/projects")
}
```

Die Suite kopiert die Fixtures je Prüfung in ein Temp-Verzeichnis und prüft:

| Prüfung | Vertrag |
|---|---|
| Quellen | `Sources` ist deterministisch (gleiche Liste bei jedem Aufruf und jeder neuen Instanz), absolute Pfade, existierende Dateien, keine Duplikate |
| Felder | jedes Event hat `ID` und `SessionID` mit Präfix `<Name()>:`, einen Zeitstempel, `Kind`, `Role` und `Actor` aus den Mengen in `internal/model`; Session- und Parent-IDs der Records tragen dasselbe Präfix |
| IDs | Event-IDs sind über alle Quellen eindeutig und stabil über Läufe und Instanzen |
| Offsets | `Parse` ab 0 liefert die Dateigröße; erneutes `Parse` ab diesem Offset liefert nichts und denselben Offset |
| Halbe Zeile | eine unvollständige letzte Zeile wird nicht verbraucht (Offset = Anfang der Zeile) |
| Zwei Hälften | Datei an einer Zeilengrenze oder mitten in einer Zeile kürzen, parsen, wieder vervollständigen, ab dem gelieferten Offset weiterparsen: zusammen dieselben Events wie in einem Durchgang |
| Abbruch | mit abgebrochenem Kontext gibt `Parse` `context.Canceled` zurück und emittiert nichts; wird mitten im Lauf abgebrochen oder liefert `emit` einen Fehler, kommt der Fehler zurück, und ab dem gelieferten Offset geht nichts verloren und nichts doppelt |

Token-Zahlen werden beim Hälften-Vergleich nicht verglichen: Claude Code wiederholt die Usage einer API-Antwort in jeder Block-Zeile, der Adapter zählt sie je `Parse`-Aufruf nur einmal. Wird zwischen zwei Blöcken derselben Antwort geteilt, kann die Usage doppelt erscheinen.

Die Fixtures brauchen mindestens eine Quelle mit zwei Records, die Events erzeugen, und jede Quelle endet mit einem Zeilenumbruch. Die Suite testet sich selbst an einem Spielzeug-Adapter mit absichtlich eingebauten Fehlern.

Für die Format-Drift-Prüfung (`total-recall doctor`) implementiert ein Adapter zusätzlich `harness.FormatChecker`; die bekannten Record- und Block-Typen exportiert er, damit Parser und Prüfung dieselben Listen benutzen.

## Redaction

Vor dem Schreiben werden `text` und `raw` maskiert (`«redacted:<art>»`), dazu die JSON-Blobs der Meta-Records (`Redactor.JSON`) und die Text-Kopien der Datei-Historie (`Redactor.Text`); die Anhänge (Bilder, Dokumente in [`blobs`](#blobs)) **nicht**: Ein Bild lässt sich nicht redigieren, ohne es zu zerstören, und sein Hash muss zu den Daten passen. Wer in Screenshots Geheimnisse zeigt, archiviert sie; `[ingest] store_blobs = false` lässt die Daten weg. Maskiert werden: Private-Key-Blöcke, AWS-/GitHub-/Slack-/OpenAI-/Anthropic-Token-Formate, JWTs, `op://`-Referenzen bleiben (sind keine Geheimnisse), `password=…`/`passwort: …`/`token: …`-Zuweisungen, Basic-Auth in URLs, hochentropische Strings ≥ 32 Zeichen in Zuweisungen. Redaction ist nicht umkehrbar. Tests mit Positiv- und Negativfällen (z.B. Git-SHAs und UUIDs dürfen nicht maskiert werden).

`text` läuft durch `Redactor.Text`: Die Regeln greifen tokenweise, ein Treffer ersetzt nur den Treffer, die Umgebung bleibt lesbar. `raw` (auch das der Meta-Records) läuft durch `Redactor.JSON`, das die Zeile als JSON liest und nur String-Werte anfasst (Schlüssel, Zahlen, Reihenfolge und Leerraum bleiben byteweise erhalten). Je String-Wert gilt die Reihenfolge wie in `Text`: Steht der Wert unter einem sensiblen Schlüssel (`password`, `secret`, `token`, `api_key`, `authorization`, … wie in den Zuweisungsregeln) und sieht nach einem Geheimnis aus (kein Platzhalter, keine Referenz, kein Typ), wird der ganze Wert `«redacted:secret»`. Sonst gelten die Textregeln auf den Wert, tokenweise. Nur wenn die keinen Treffer liefern und der Wert selbst **ein einziges Token** ist (≥ 32 Zeichen, ohne Leerraum, aus `[A-Za-z0-9+/=_.-]`) mit hoher Entropie, wird er als Ganzes `«redacted:high_entropy»`. Prosa, Shell-Befehle, Diffs, Tool-Ausgaben und Pfade enthalten Leerraum oder andere Zeichen und werden darum nie als Ganzes maskiert; das war bis v1.0.2 anders ([#130](https://github.com/seletz/total-recall/issues/130): `JSON` prüfte jeden Wert als Ganzes auf Entropie, lange Werte ab 32 Zeichen verschwanden komplett). Das Regressionsmuster steht in `internal/redact/redact_test.go` (`TestJSONTranscriptLineRegression`). Weil `raw` jetzt jeden Wert durch alle Textregeln schickt, hat jede Regel eine billige Vorprüfung (Literal wie `ghp_`, `AKIA`, `eyJ`, `://`, ein Schlüsselwort wie `token` in der Kleinschreibung, oder eine Folge von 32 Token-Zeichen für `high_entropy`); der reguläre Ausdruck läuft nur, wenn sie zutrifft. Auf echten Transkripten ist `JSON` damit etwa doppelt so schnell wie vor dem Fix.

String-Elemente in **Arrays** haben keinen eigenen Schlüssel. Sie bekommen seit v1.3.0 dieselbe Prüfung wie Objektwerte, mit dem Schlüssel des Arrays als ihrem Schlüssel (durch geschachtelte Arrays hindurch vererbt; `JSON` führt dafür einen Stapel der offenen Container): Unter einem sensiblen Schlüssel (`"passwords": [...]`) wird jedes Element, das nach einem Geheimnis aussieht, `«redacted:secret»`; sonst gelten die Textregeln tokenweise; findet die nichts und ist das Element ein einziges Token ≥ 32 Zeichen mit hoher Entropie, wird es als Ganzes `«redacted:high_entropy»`, außer der Schlüssel des Arrays benennt Bezeichner (`messageIds`, `ids`, `signatures`, `paths`, …, `identifierValue`, jetzt auch mit Endung `ids`). Zusätzlich zählt in einem Array das vorangehende Element als Schlüssel, wenn es eine Option mit sensiblem Namen ist (`--token`, `--api-key`, `--password`, `-passwd`, …, `flagKey`: beginnt mit `-`, danach nur ein Optionsname aus `[A-Za-z0-9_-]`, Name wie bei den Zuweisungsregeln): dann wird das folgende Element als Ganzes `«redacted:secret»`, auch wenn es kurz ist, sofern es nach einem Geheimnis aussieht (`plausibleSecret`: kein Platzhalter wie `${TOKEN}` oder `<pw>`, keine `op://`-Referenz). Ein bloßes Wort `token` ohne Bindestrich zählt nicht (Prosa in einer Wortliste), ebenso wenig ein Wortfetzen mit Bindestrich, der `secret` nur enthält. Der Vorgänger wird nur innerhalb desselben Arrays gemerkt: eine Zahl oder ein Objekt dazwischen löscht ihn, ein `--token` am Ende eines Arrays wirkt nicht auf den nächsten Objektwert. Vor v1.3.0 liefen Array-Elemente nur durch die Textregeln; ein Geheimnis ohne bekanntes Format in `["--api-key", "<token>"]` blieb in `raw` stehen ([#162](https://github.com/seletz/total-recall/issues/162)). Prosa, Befehle, Pfade, Diffs, SHAs, UUIDs und lange Bezeichner in Arrays enthalten Leerraum, beginnen mit `/`, sind Hex oder haben keine Ziffern und bleiben darum erhalten; Werkzeugnamen der Form `mcp__server__tool` schließt `highEntropy` jetzt über den doppelten Unterstrich aus, auch in `text` (`TestJSONArrayElements`). Gemessen an den Rohzeilen echter Transkripte ändert die Regel nur eine Handvoll von Millionen Array-Elementen, im gespeicherten Auszug praktisch keines. Ein Archiv zieht den Bestand mit `ingest --all --reread --rewrite-raw` nach.

Reparatur eines Archivs, dessen `raw` so beschnitten wurde, oder das vor v1.3.0 nur Auszüge und keine Blobs hat: `ingest --all --reread --rewrite-raw` liest jede Quelle ab Offset 0, redigiert jede Zeile neu (die ganze Zeile, siehe [`raw`](#raw)) und schreibt bei bestehenden Events nur `raw` und `raw_truncated` neu (`store.WriteOptions.RewriteRaw`: ein `UPDATE` dieser zwei Spalten vor dem `INSERT … ON CONFLICT DO NOTHING`), dazu die Anhänge der Zeile in `blobs` und die Verweise des Events in `event_blobs` (alte Verweise des Events werden ersetzt). Auch ein Event ohne `raw` (altes Limit, `raw_truncated = 1`) bekommt die Zeile. `text`, `actor` und die FTS-Tabelle bleiben unberührt; der Trigger `events_fts_au` reagiert nur auf `UPDATE OF text`, und `reindex --actors` kollidiert nicht. Session-Zahlen bleiben gleich; Events kommen nur hinzu, wo der Adapter Zeilen jetzt anders liest (Bild-Blöcke, die vor v1.3.0 übersprungen wurden). `Summary.RawRewritten` zählt die neu geschriebenen Zeilen, `Blobs`/`BlobBytes` die neuen Blobs. Der Lauf ist abbrechbar und wiederholbar: Jede Charge (500 Events oder 64 MiB Anhänge, `ingest.DefaultBatchBytes`) ist eine Transaktion, der Offset der Quelle wird mit ihr gespeichert, ein zweiter Lauf schreibt dieselben Zeilen noch einmal und ändert nichts. Für gelöschte Quellen bleibt alles, wie es ist: das Nachtragen geht nur, solange die Transkripte existieren.

## Git-Info

`gitinfo.Resolve(cwd)`: existiert `cwd`, dann `git -C cwd rev-parse --show-toplevel`, `git remote get-url origin`, `git branch --show-current`. Für Worktrees wird der Hauptrepo-Root über `git rev-parse --git-common-dir` bestimmt. Existiert `cwd` nicht mehr (gelöschter Worktree), wird zum nächsten existierenden Elternverzeichnis gegangen, das ein Git-Repo ist. Ist auch das kein Repo und liegt `cwd` unter `<pfad>/<repo>__worktrees/` (Worktree-Layout von worktrunk), wird `<pfad>/<repo>` aufgelöst. Ergebnis wird in Tabelle `dirs(cwd PK, git_root, git_remote_url, resolved_at)` gecacht, damit es nach dem Löschen eines Worktrees erhalten bleibt. Ein Treffer gilt für immer, „kein Repo“ wird nach 7 Tagen neu geprüft. Je Lauf wird jedes `cwd` höchstens einmal aufgelöst. Remote-URLs werden normalisiert (`git@github.com:o/r.git` → `https://github.com/o/r`), Credentials entfernt.

## Ingest, Hooks, Nebenläufigkeit

- `ingest [PATH…]`: einzelne Dateien. `ingest --all`: alle Quellen aller Adapter (= Nachlesen). `ingest --regit`: Git-Info für das `cwd` jeder Session neu auflösen (am `dirs`-Cache vorbei), `dirs` und `sessions.git_root`/`git_remote_url` aktualisieren; löst sich ein `cwd` nicht mehr auf, bleibt der gecachte Treffer. `ingest --reread` (mit `PATH…` oder `--all`): die Quellen ab Offset 0 lesen statt ab dem gespeicherten Lesestand (`ingest.Deps.Reread`); Events sind idempotent, Sessions werden gemerged, der neue Offset ist wieder das Dateiende. Für Altbestand nach einer Migration, die Session-Felder oder [Meta-Records](#session_meta) aus bisher übersprungenen Records ergänzt. `ingest --reread --rewrite-raw`: zusätzlich `raw` und `raw_truncated` bestehender Events (und `raw` bestehender Meta-Records) aus der neu redigierten Zeile ersetzen und ihre Anhänge als Blobs nachtragen (`ingest.Deps.RewriteRaw`, siehe [Redaction](#redaction)); nur mit `--reread` und nur bei `ingest.store_raw = true`, sonst würde `raw` geleert. `ingest --hook`: liest Hook-JSON von stdin (`transcript_path`, `session_id`, `cwd`, `hook_event_name`), liest diese Datei ein, und bei `SessionStart` zusätzlich `--all`.
- `--detach`: startet sich selbst als losgelösten Hintergrundprozess (neue Session-ID, stdin-Daten als Argument/Tempdatei) und kehrt sofort mit Exit 0 zurück. Hooks dürfen die Harness nie bremsen oder blockieren; Fehler gehen ins Log, Exit-Code im Hook-Modus ist immer 0.
- SQLite im WAL-Modus, `busy_timeout` 10 s. Je Quelldatei ein Lock (Lockfile in `locks/` neben der DB, `flock` ohne Warten: eine gesperrte Datei wird übersprungen), damit zwei parallele Läufe dieselbe Datei nicht doppelt lesen. Events sind ohnehin idempotent (`INSERT … ON CONFLICT DO NOTHING`).
- Geschrieben wird in Batches (500 Events) je Transaktion, zusammen mit dem Offset der ersten noch nicht geschriebenen Zeile. Ein Absturz verliert und verdoppelt daher nichts.
- `--detach` übergibt das Hook-JSON in einer Tempdatei (`--hook-payload`, intern), der Kindprozess löscht sie nach dem Lesen. Stdout/stderr des Kindprozesses gehen ins Log.
- Jeder Hook liest nach den Transkripten das [Projekt-Gedächtnis](#memory) (bei `ingest.store_memory`), `SessionStart` über `--all`. Ohne Änderung kostet das ein `stat` je Datei und eine Abfrage.
- Im Hook-Modus schreibt `ingest` nichts auf stdout (bei `SessionStart` landete das im Kontext der Session).
- Nur vollständige Zeilen lesen; eine halb geschriebene letzte Zeile bleibt für den nächsten Lauf.
- **Wartung und Dateitausch.** `maintenance` baut die Datei mit neuer Seitengröße auf einer Kopie neu auf (`PRAGMA page_size` + `VACUUM INTO`, geht auch aus dem WAL-Modus) und tauscht sie per `rename` aus. Ein Prozess, der die alte Datei schon offen hat, würde nach dem Tausch in deren Inode weiterschreiben; deshalb hält jedes schreibende Öffnen über die CLI (`store.WithLockDir`: `ingest`, `backup`, `reindex`) ein geteiltes `flock` auf `locks/maintenance.lock` bis zum Schließen, und `maintenance` nimmt dasselbe Lock exklusiv (Polling bis `--wait`, sonst `ErrWritersActive`); `migrate` nimmt es auf demselben Weg. Weil `flock` Schreiber nicht nachrangig behandelt und laufend neu öffnende Hooks die Wartung aushungern könnten, kündigt sie sich vorher mit einem exklusiven `flock` auf `locks/maintenance.pending` an, das sie bis zum Ende hält; ein Schreiber nimmt sein geteiltes Lock nur, wenn er die Ankündigung frei vorfindet. Sobald die Wartung wartet, startet also kein neuer Schreiber mehr, sie muss nur die schon offenen überdauern. Ein Schreiber, der auf die Ankündigung oder das exklusive Lock trifft, wartet nicht, sondern scheitert sofort mit `ErrMaintenance`; im Hook-Modus ist das eine `INFO`-Zeile und Exit 0, der nächste Hook liest ab dem gespeicherten Offset weiter. Lesende Befehle brauchen kein Lock. Unter dem Lock: `PRAGMA wal_checkpoint(TRUNCATE)` (Abbruch mit `ErrReadersActive`, wenn Leser es verhindern, sonst würde die alte `-wal` zur neuen Datei gepaart), Kopie bauen, `PRAGMA integrity_check` und Zeilenzahlen vergleichen, `-wal`/`-shm` entfernen, `rename`. Vorher: Platzprüfung (etwa 2× Dateigröße neben der DB, halbe im Backup-Ordner, `syscall.Statfs`), Backup wie `backup`, Reste `archive.db.maint-*` früherer Läufe entfernen. Jeder Fehler vor dem `rename` lässt das Original unverändert und löscht die Kopie (auch bei `SIGINT`, der Befehl bricht über den Kontext ab). `--dry-run` baut und misst die Kopie ohne Lock und Backup. `status` zeigt `page_size` und einen Hinweis unter 16384.

Hooks für Claude Code (`total-recall hooks print --harness claude` gibt diesen Block aus, eingetragen wird er manuell bzw. über die dotfiles):

| Event | Befehl |
|---|---|
| `Stop`, `SubagentStop` | `total-recall ingest --hook --detach` |
| `SessionStart` | `total-recall ingest --hook --detach` (liest zusätzlich alles nach) |
| `SessionEnd` | `total-recall ingest --hook --detach` und `total-recall backup --if-older 24h --detach` |

## Schema-Versionierung

- Die Tabelle `schema_version` hält die Version der Datenbank. Jede Migration ist ein SQL-Block in `internal/store/schema.go`, Version = Position in der Liste. Daneben, parallel und ebenfalls nur anhängend: `migrationNotes` (je Migration eine Zeile Beschreibung und der zusätzliche Platzbedarf während der Migration als Faktor der Dateigröße; ein Test prüft, dass beide Listen gleich lang sind) und `migrationSteps` (der Go-Teil einer Migration, die nicht mit SQL allein auskommt, etwa weil sie alle Events umschreibt; er läuft in derselben Transaktion nach dem SQL-Block, vor dem Setzen von `schema_version`, und meldet seinen Fortschritt über `report(done, total)`).
- **Migrationen sind vorwärts-only. Eine bestehende Migration wird nie geändert, neue werden nur angehängt.** Ein neues Schema ist eine Minor-Version, weil der Nutzer die Migration selbst auslöst; was sonst zur stabilen Schnittstelle zählt, steht in der Referenz unter [Versionierung](../reference/60-versionierung.md).
- **Kein Befehl migriert von selbst.** `store.Open` legt eine neue, leere DB direkt in der neuesten Version an; eine bestehende DB mit älterem Schema lehnt es mit `ErrSchemaOutdated` ab, eine mit neuerem wie bisher mit `ErrSchemaTooNew`. Grund: Eine Migration kann alle Events umschreiben und Minuten dauern; so etwas darf nicht unbemerkt in einem Hook passieren. Der Nutzer sichert vorher und entscheidet, wann.
- `migrate [--dry-run] [--yes] [--wait DAUER] [--json]` (`store.PlanMigration`, `store.Migrate`): zeigt Version der DB und des Programms, je ausstehender Migration ihre Beschreibung, den Pfad der Kopie und den nötigen freien Platz (Kopie + größter Platzfaktor × Dateigröße + ein Viertel Reserve); fragt nach Bestätigung (`--yes` für Skripte, `--dry-run` zeigt nur den Plan). Dann: Platzprüfung (`ErrDiskSpace`), exklusives Lock wie `maintenance` (`locks/maintenance.pending`, dann `maintenance.lock`; wartet bis `--wait` auf schon offene Schreiber, sonst `ErrWritersActive`; Hooks, die währenddessen starten, überspringen ihren Lauf), Öffnen mit `store.AllowOutdated()`, Kopie `<db>.pre-v<N>.bak` per `VACUUM INTO` neben die DB (N = alte Version), dann jede Migration in eigener Transaktion mit `wal_checkpoint(TRUNCATE)` danach, damit die WAL einer großen Migration nicht stehen bleibt. Ein Fehler lässt die DB auf der letzten vollständigen Version, die Kopie bleibt liegen. Fortschritt kommt über `MigrateOptions.Progress` (`store.Progress{Version, Done, Total}`); die CLI gibt ihn höchstens alle zwei Sekunden und am Ende aus.
- **Veraltete DB (älter als das Programm):**
  - Schreibende Befehle (`ingest`, `reindex`, `maintenance`) scheitern mit `ErrSchemaOutdated`; die Meldung nennt den Weg: `total-recall backup`, dann `total-recall migrate`. Geschrieben wird nichts.
  - Im Hook-Modus ist das kein Fehler: eine `WARN`-Zeile im Log, höchstens einmal je Meldung und Tag (Marker `locks/schema-warned`), Exit 0, Lesestand (`sources.offset`) unverändert. Der erste Hook nach der Migration liest nach (`SessionStart` liest alles), es geht nichts verloren.
  - `backup` öffnet mit `store.AllowOutdated()` und funktioniert ohne Migration: Es ist der empfohlene Schritt davor.
  - Lesende Befehle öffnen read-only, warnen auf stderr (`store.SchemaWarning`) und laufen, soweit sie keine neuen Spalten brauchen; scheitert eine Abfrage an einer fehlenden Spalte oder Tabelle, trägt der Fehler dieselbe Anleitung. `status` zeigt `MIGRATION PENDING` in der Zeile `schema` und `migration_pending` im JSON, `doctor` meldet die ausstehende Migration als Befund (Exit 2), die TUI zeigt in der Kopfzeile `migration pending: total-recall migrate` statt der Ladezeit und bemerkt die Migration beim nächsten `Poll` (`store.MigrationPending`).
- Ist die DB neuer als das Programm: `ErrSchemaTooNew`, auch lesend; im Hook-Modus geloggt (`ERROR`), Exit 0, nichts geschrieben.
- `TestMigrateFromEveryVersion` baut die DB in jeder historischen Version (`store.CreateAtVersion`, auch für Tests anderer Pakete), füllt sie, prüft, dass `Open` sie ablehnt, und migriert mit `Migrate` auf den neuesten Stand. Neue Migrationen sind damit automatisch gegen alle älteren Stände getestet; wer Daten anfasst, ergänzt dort Beispielzeilen. Weitere Tests: kein Automigrieren durch `ingest`, Hook, `backup`, `maintenance` und `reindex`; ein Hook auf einer veralteten DB verliert nichts; `--dry-run` und ein Abbruch ändern nichts.
- Eine Migration ist ein SQL-Block (`migrations`), dazu je Version ein Eintrag `migrationNotes` (`Summary`: was sie tut, bei langen Migrationen die Dauer; `Space`: zusätzlicher Platz als Faktor der Dateigröße, 0 für ALTER TABLE, 1 oder mehr, wenn jede Zeile neu geschrieben wird) und optional ein Go-Schritt `migrationSteps[v]` (`func(ctx, tx, report)`, gebaut aus dem migrierenden Store, damit er dessen Optionen kennt), der in derselben Transaktion nach dem SQL und vor dem Setzen von `schema_version` läuft und seinen Fortschritt über `report(done, total)` meldet. Bricht er ab, bleibt die DB auf der alten Version.
- **Migration 5** (`raw` als zstd-BLOB, `source_id`): Das SQL legt `sources` mit `id` neu an (Pfad → ID, fehlende Pfade aus `events.source_path` bekommen eine Zeile) und die Tabelle `events_v5`; der Go-Schritt (`migrateRawV5`) kopiert die Events in Chargen von 1000 unter Beibehaltung von `seq`, komprimiert `raw` (bis v1.2 entschlackte er es dabei zum Auszug; seit #180 nicht mehr, ein v4-Archiv behält beim Migrieren seine ganzen Zeilen), bildet `source_path` auf `source_id` ab, tauscht die Tabellen (die Views werden dafür aus `sqlite_master` gesichert, gelöscht und wieder angelegt, weil SQLite eine Tabelle nicht umbenennt, solange eine View eine fehlende nennt), legt Indizes und FTS-Trigger neu an (`eventsIndexSQL`) und lässt FTS5 den Index gegen die neue Tabelle prüfen (`integrity-check`); weil `seq` die `rowid` des externen Volltextindex ist, muss der nicht neu gebaut werden. `Space` ist 0,5: Die WAL wächst während der Migration auf gut ein Drittel der Datei (gemessen 0,36; nur die neue Tabelle wird geschrieben, die alten Seiten wandern auf die Freiliste), mit Kopie und Reserve verlangt `migrate` damit knapp das Doppelte. Auf einer Kopie eines echten Archivs (233 198 Events, 1,1 GiB): 25 s; die Datei ist danach bis zu `maintenance` größer als vorher (Freiliste), danach mit 16-KiB-Seiten 438 MiB.
- **Migration 6** (`blobs`, `event_blobs`, #181): nur SQL, legt die beiden Tabellen und den Index an, bewegt keine Daten (`Space` 0, Sekunden). Was sie nicht tut: Zeilen vervollständigen und Anhänge aus bestehenden `raw`-Auszügen holen; die Auszüge von Migration 5 enthalten die Bilder gar nicht mehr. Dafür liest `ingest --all --reread --rewrite-raw` die Transkripte neu (siehe [Redaction](#redaction)), als eigener Schritt nach `migrate`, abbrechbar und wiederholbar; die Migration selbst bleibt kurz und braucht keinen Platz.
- **Migration 8** ([Projekt-Gedächtnis](#memory), #192): legt `memory_projects`, `memory_files`, `memory_versions`, `memory_fts` und zwei Indizes an (`Space` 0). Auf einer Kopie des Archivs (803 MB) 0,9 s, die Datei wächst um 0,2 MB. Daten bewegt sie keine: Der nächste `ingest --all` (oder Hook) nimmt das Gedächtnis auf, wie es auf der Platte liegt; der erste Lauf über 905 Dateien dauerte 0,63 s, die Datei wuchs um 4,8 MiB (Inhalte 1,9 MiB, als Blobs 1,1 MiB, der Volltextindex mit seiner Kopie des Texts und Seitenverschnitt den Rest).
- **Migration 7** ([`session_meta`](#session_meta), #182): legt nur die Tabelle und ihren Index an (`Space` 0, Sekundenbruchteile). Die Meta-Records der vorhandenen Transkripte holt derselbe Lauf `ingest --all --reread --rewrite-raw`; was die Harness schon gelöscht hat, ist weg.

## CLI

| Befehl | Zweck |
|---|---|
| `ingest` | siehe oben |
| `reindex --actors` | Actor jeder gespeicherten User-Nachricht mit den aktuellen Regeln neu bestimmen (`harness.Reclassifier`, `ingest.Reclassify`), ohne die Transkripte zu lesen; idempotent, `--dry-run` zählt nur |
| `status` | DB-Pfad, Größe, Seitengröße (mit Hinweis auf `maintenance` unter 16384), Anzahl Sessions/Events, Größe von `raw` (komprimiert, Anzahl Events mit und ohne `raw`, Kompressionsverhältnis aus den Frame-Headern einer Stichprobe von 2000 Zeilen; `store.RawStats`), Anzahl und Größe der Blobs (geliefert und gespeichert; `store.BlobStats`), Projekt-Gedächtnis (Dateien, Projekte, Löschungen, Fassungen, Größe; `store.MemoryStats`), letzter Ingest je Harness, Fehler aus dem Log der letzten 24 h (ältere nur als Anzahl, `--all-errors`; fehlende Transkripte zählen nie), letztes Backup |
| `sessions` | Liste; Filter `--since`, `--until`, `--repo`, `--harness`, `--name`, `--account` (Name aus `[accounts]`, Account-ID oder Präfix), `--limit`, `--subagents` (sonst nur Haupt-Sessions), `--all-sessions` (sonst ohne Sessions ohne Events und Startzeit); Spalten Start, Dauer, ID-Präfix, Name, Repo, Branch, #human, #agent; JSON zusätzlich `continued_in`, `continued_from`, `account_id`, `organization_id`, `account` (Name), jeweils `null` ohne Wert |
| `show SESSION` | Verlauf einer Session (Anfragen/Antworten, `--all` inkl. Tools); Kopf mit `parent`, `continued from`/`continued in`, `account`/`organization` (Name aus `[accounts]`), soweit bekannt; Fenster über `--since`/`--until` (Semantik und Zeitformate wie `search`, lokale Zeit), `--limit`/`--offset` zum Blättern, `--from-event ID` ab einem Event (z. B. Treffer aus `search`); ohne Grenzen vollständig, ab 2000 Events mit Hinweis auf stderr; je Event seine Anhänge (Hash, Medientyp, Größe; JSON `attachments`, immer vorhanden) aus `store.SessionAttachments`, nur Metadaten |
| `memory [--project P] [--at ZEIT] [--deleted] [--history DATEI] [--grep WÖRTER [--raw] [--limit N]]` | das archivierte [Projekt-Gedächtnis](#memory): je Datei die Fassung jetzt oder zu `--at` (Projektname wie `stats --by project`, `--project` genau, sonst als Teil von Name, Pfad, Schlüssel), `--history` die Fassungen einer Datei (`ambiguous`, wenn sie in mehreren Projekten liegt), `--grep` die Volltextsuche über den aktuellen Inhalt (`store.SearchMemory`). Kein Flag an `search`: die Suche über Objekttypen kommt mit einem eigenen Epic als `search --type` |
| `blob HASH [--out DATEI]` | einen Anhang ausgeben (`store.Blob`: voller SHA-256 oder eindeutiges Präfix, `not_found`/`ambiguous`): in `--out DATEI` oder auf stdout, Binärdaten aber nie auf ein Terminal (`usage`-Fehler mit Hinweis auf `--out`); `--json` nur Metadaten (`sha256`, `media_type`, `bytes`, `stored_bytes`, `events` über `store.BlobEvents`, `meta_records`, `memory` über `store.BlobMemory`, `out`) |
| `search QUERY` | FTS5-Suche mit Snippets; Filter wie `sessions`, `--actor`, `--session ID` (eindeutiges Präfix erlaubt, mehrdeutig = Fehler), `--raw` (FTS5-Syntax, sonst werden die Wörter quotiert). Mit `--by-session` eine Zeile je Session (Subagenten eingeschlossen) aus `store.SearchBySession`: `first_hit`, `last_hit`, `hits`, `snippet` des ersten Treffers; sortiert nach `first_hit` (Zeitleiste, älteste zuerst) oder mit `--order hits` nach Trefferzahl; dieselben Filter, `--limit` zählt Sessions. Das ist die Kernabfrage 1 des Skills, vorher eine SQL-Abfrage |
| `stats` | Kennzahlen für einen Zeitraum (`--month YYYY-MM` oder `--since/--until`): je Tag, je Wochentag×Stunde (lokale Zeit), je Repo: Sessions, human-, agent-Events. Mit `--period day\|week\|month`, `--by project\|agent\|directory\|account` (`account`: `sessions.account_id`, Subagenten über die Eltern-Session, ohne Account `unknown`, Namen aus `[accounts]`), `--top N` stattdessen eine Zeitreihe Periode × Schlüssel aus `store.ActivitySeries` (Text im Langformat, JSON) samt Summen über den Zeitraum, in denen jede Session einmal zählt (in den Perioden zählt sie je Periode mit Aktivität), Ersatz für `monthly_stats.py` im Vault; Projektnamen aus `internal/project`: zuerst die Definitionen `[[project]]` (erste passende gewinnt, mehrere Repositories werden ein Schlüssel, vor `--top`), sonst der abgeleitete Name. Mit `--workday` je Tag die erste und letzte Aktivität und die Spanne (`store.Workday`), Zeitraum `--range today\|this-week\|last-week\|this-month\|last-month` oder `--since/--until`/`--month`; gezählt werden eigene Nachrichten (`[work] count`, `--all-events` für alle; `--human-only` veraltet, erzwingt eigene Nachrichten auch bei `count = "all"`); ein Tag beginnt um `day_start` (Default 04:00 Ortszeit), Aktivität davor zählt zum Vortag (`01:30 (+1)`). Je Tag die Differenz der Spanne zum Tagessoll aus `[work]` (`store.WorkHours`: Soll an Arbeitstagen mit Aktivität, Wochenende und Feiertage aus `internal/holiday` ohne Soll, Spalte `HOLIDAY`), in der Summe Soll × Arbeitstage des Zeitraums (Mo–Fr ohne Feiertage, bis heute, mit oder ohne Aktivität) und Saldo; JSON in Sekunden. Dieselben Daten zeigt die TUI im Reiter `activity`, dort mit der Kernzeit als Band auf der Zeitachse, Balkensegmenten innerhalb/außerhalb/nach Mitternacht, Farben nach Bewertung gegen `daily_low`/`daily_high`, einer Kopfzeile je Woche/Monat (Arbeitszeit gegen `weekly_low`/`weekly_high` anteilig nach Arbeitstagen, Soll, Saldo, Feiertage) und Kennzahlen (`store.Summarize`: Median, Mittel, Min/Max von Beginn, Ende, Spanne). Mit `--worktime` je Tag und Kategorie die geschätzte Arbeitszeit, roh und gerundet (`store.HumanEvents` liefert die eigenen Nachrichten mit Session-Titel und Projekt, `internal/worktime` rechnet): die Lücke zwischen zwei aufeinanderfolgenden eigenen Nachrichten über alle Sessions zählt für das Projekt der späteren, voll bis `max_gap`, sonst `min_credit`; Projekte heißen wie in `--by project` (Definitionen `[[project]]` zuerst) und werden über die Glob-Regeln `[[worktime.category]]` Kategorien zugeordnet (erste passende Regel, die Regeln sehen den konfigurierten Namen; ohne Regel heißt die Kategorie wie das Projekt); die Summe je Kategorie und Tag (oder je Projekt, `scope`) wird auf `grid` gerundet (`rounding`). Zeitraum wie `--workday`, `--sessions` listet je Zelle Projekte und Sessions mit Titel. `--summary` ergänzt je Zelle die Issue-/PR-Referenzen (`#31`, `PR #32`, `owner/repo#123`, auch aus Issue-/Pull-URLs; `worktime.FindRefs`) aus den Session-Titeln und den eigenen Nachrichten (`store.HumanEvents` liefert den Text; nie aus Agent- oder Tool-Ausgaben), je Session, Projekt und Zelle dedupliziert und sortiert, und einen Buchungstext `<projekt>: <titel> (<refs>), …; <projekt>: …` (`worktime.Summary`), gekürzt auf `summary_max_chars`; JSON enthält `refs` und `text` immer. Meetings und Arbeit ohne Agent fehlen: eine Untergrenze. |
| `chart <calendar\|projects\|punchcard\|worktime\|all>` | Grafiken für Monatsberichte als SVG (eigener Writer) oder PNG (reines Go) aus `internal/figure`: Kalender der eigenen Nachrichten je Tag (`store.Stats.ByDay`, Monatsraster, Stufen aus `[chart] heatmap`), Anteil je Projekt (`store.ActivitySeries` je Monat, `--top`, Farben aus `[[project]]` bzw. Palette in Volumenreihenfolge, `other` grau), Punchcard Wochentag × Stunde (`store.Stats.ByWeekdayHour`, eigene Nachrichten über Agenten-Events), Arbeitszeit je ISO-Woche und Kategorie (`worktime.Compute` wie `stats --worktime`, gerundete Zeit, Soll je Woche = `[work] target` × Arbeitstage der Woche im Zeitraum bis heute, Farben aus `color` der Kategorie bzw. freie Palettenfarben in Regelreihenfolge). Zeitraum `--month`, `--since/--until`, `--range` wie `stats`; `calendar`/`projects`/`punchcard` ab Mitternacht, `worktime` ab `day_start`. `--format svg\|png`, `--scale`, `--width`/`--height` (beide: zentriert im Rahmen), `--out DIR` mit `<YYYY-MM>-<grafik>.<format>` bzw. `<von>_<bis>-<grafik>`, ohne `--out` eine Grafik nach stdout; `--no-title`, `--no-legend`, `--transparent`, `--top`, `--lang en\|de` (Beschriftungen, Datums- und Zahlenformat), `--title TEXT`; Defaults aus `[chart]`. Gezählt wie `stats` (`actor = human` ohne `peer`). |
| `tui [search\|stats\|activity\|config]` | Terminal-Oberfläche, lesend (`store.OpenReadOnly`): Suche mit Vorschau und Session-Detail (Kopf mit `continued from`/`continued in`), Statistik aus `store.ActivitySeries` und `store.Stats` (Textchart, Kitty Graphics mit Rückfall, Tabelle, Heatmap), Aktivität je Tag aus `store.Workday`. Die Ansicht Config (`internal/tui/configview`) ist die Ausnahme: Settings schreibt Schlüssel der Config-Datei (`config.SetValue`, siehe Konfiguration), Projects pflegt `[[project]]` (`config.AddProject`/`SetProject`/`RemoveProject`/`MoveProject`): Liste der Definitionen mit dem, was jede im Archiv trifft (`store.ProjectKeys`: Git-Root bzw. Arbeitsverzeichnis je Session der obersten Ebene mit Remote-URL, Sessionzahl und letzter Aktivität, durch `project.Resolver.Key` zugeordnet), darunter die Pfade ohne Definition; ein Formular je Definition mit Entwurf, der erst mit `y` geschrieben wird, Farbwahl aus `project.Palette` und einer Vorschau, die den Entwurf mit `project.Targets` und einem Resolver aus den übrigen Definitionen gegen die Pfade rechnet (erste passende gewinnt); `E` öffnet die Datei, `Changed` nach dem Editor. Actions führt `backup`, `maintenance`, `migrate`, `reindex --actors`, `ingest --all` und `doctor` im Prozess aus, über dieselben Funktionen wie die Befehle (`internal/ops`: `OpenWrite` mit dem Wartungs-Lock, `Deps`, `MaintainOptions`, `Migrate` um `store.Migrate` mit Lock und Raw-Trimmern, `PlanLines` für den Plan aus `store.PlanMigration`, den die View bei anstehender Migration mit dem Status lädt und vor der Bestätigung zeigt; solange sie ansteht, ist außer `backup`, das mit `store.AllowOutdated` öffnet, keine schreibende Aktion anwählbar), schreibende nur nach Bestätigung, in einer Goroutine mit Fortschrittszeilen als Nachrichten und `context`-Abbruch; währenddessen meldet die View `Busy`, die App setzt das Neuladen aus und fragt danach `store.Poll` sofort (ausgetauschte Datei → `Reopen`). Setup prüft Config, Hooks in `settings.json` (nur lesen), Binary im PATH, Backup-Verzeichnis, Transkripte, Archiv und bietet nur das Schreiben der eigenen Vorlage und des Backup-Verzeichnisses an; den Hook-Block kopiert `tea.SetClipboard` (OSC 52). Eine geänderte Konfiguration erreicht die App als `Changed`; sie übernimmt `reload` und `mouse` sofort und gibt geänderte Projektdefinitionen (`config.ProjectResolver`, verglichen mit `Resolver.Equal`) an Statistik und Aktivität weiter (`SetProjects`: Resolver tauschen, `chart.Colors.Reset`, `Reload`), alles andere gilt beim nächsten Start. `--query`, `--graphics auto\|text\|kitty`, `--no-mouse`, `--reload <dauer>`; Defaults aus `[tui]`, Tastenbelegung aus `[keys]`. Neuladen: alle `reload` (Default 5 s, `0` = aus, mindestens 1 s) fragt die App `store.Poll` (ein `PRAGMA data_version` auf einer dafür festgehaltenen Verbindung plus `stat` der Datei; keine Lesetransaktion zwischen den Prüfungen, keine weitere Abfrage ohne Änderung) und lädt bei einer Änderung nur die sichtbare Ansicht neu (`View.Reload`: gleiche Abfrage, gleiches Fenster, gleiche Session; Markierung, Scrollposition, Fokus und Eingabe bleiben, eine Session am Ende läuft mit), die anderen gelten als veraltet und laden beim nächsten Anzeigen; eine ausgetauschte Datei (`maintenance`) meldet `Poll` als `Replaced`, `store.Reopen` öffnet sie neu und nimmt sofort die neue Basis. Die Kopfzeile zeigt `updated HH:MM:SS`, `R` (`global_reload`) lädt sofort. Bedienung in [TUI](../reference/30-tui.md), Entwurf und Messwerte in [Spike: TUI](spike-tui.md). |
| `sql QUERY` | Read-only-SQL (DB mit `mode=ro` und `PRAGMA query_only`), Ausgabe Tabelle/JSON/JSONL/CSV; `raw_text(raw)` entpackt die Rohzeile (mit den Blob-Verweisen) |
| `backup` | `VACUUM INTO` + zstd + Aufbewahrung; `--if-older DURATION` |
| `restore BACKUP_FILE` | Backup (`.db.zst`, ältere `.db.gz` oder `.db`) in eine DB-Datei entpacken; `--to PATH`, `--force` |
| `maintenance` | Datei mit neuer Seitengröße (`--page-size`, Default 16384) auf einer Kopie neu aufbauen, prüfen und atomar austauschen; Backup vorher, Lock gegen Hooks, `--dry-run` misst nur, `--wait`, `--json`; siehe „Wartung und Dateitausch“ oben |
| `migrate` | Schema der DB auf den Stand des Programms bringen, der einzige Weg dahin: Plan (Versionen, ausstehende Migrationen, Kopie, Platz), Bestätigung oder `--yes`, `--dry-run` zeigt nur, `--wait`, `--json`; Platzprüfung, exklusives Lock wie `maintenance`, Kopie `<db>.pre-v<N>.bak`, Fortschritt; siehe „Schema-Versionierung“ oben |
| `hooks print --harness claude` | Hook-Konfiguration als JSON |
| `doctor [--harness claude] [--json]` | Format-Drift prüfen, nur lesend, ohne DB: unbekannte Record-Typen (weder eingelesen noch bekannt übersprungen), unbekannte Content-Block-Typen, fehlende erwartete Felder (`timestamp`, `sessionId`, `uuid`, `cwd` je Typ), kaputte Zeilen, gesehene Harness-Versionen (neueste zuerst). Exit 0 = sauber, 2 = Befunde. Die bekannten Listen exportiert der Adapter; Parser und Prüfung teilen sie. |
| `llm --skill` | gibt den eingebetteten `SKILL.md` aus (Schema, Befehle, Beispiel-SQL), damit Agenten die Daten auswerten können |
| `config show` / `config init` | wirksame Konfiguration / Vorlage schreiben |
| `version` | Version ausgeben; `--version`/`-v` auf dem Hauptbefehl gibt dieselbe Zeile aus |

Globale Flags: `--config`, `--db`, `--tz` (Default `Europe/Berlin` für Auswertungen), `--backup-dir`. `backup.min_interval` ist derzeit ungenutzt; `--if-older` braucht eine ausdrückliche Dauer.

### Ausgabevertrag der Lesebefehle

Gilt für `status`, `sessions`, `show`, `blob`, `search`, `stats`, `sql`, `doctor` und `config show`; umgesetzt in `internal/cli/output.go` (`outputFlags`, `output`, `NullString`, `ErrorJSON`), das jeder Lesebefehl über `env.readRun` benutzt.

- **Formate.** Jeder Lesebefehl hat `--format table|json|jsonl` (`sql` zusätzlich `csv`), `--json` ist die Kurzform für `--format json` und gewinnt. `table` ist der Standard und meint die lesbare Form (Tabelle oder Text). `json` ist ein eingerücktes Dokument, `jsonl` ein kompaktes Objekt je Zeile, ungepuffert geschrieben; ein Einzelobjekt (`status`, `stats`) ist dann eine Zeile, `show` schreibt nur die Events (den Session-Kopf liefert `sessions`). Ein leeres Ergebnis ist bei `json` `[]`, bei `jsonl` keine Zeile.
- **Fehler.** Bei `json` oder `jsonl` geht ein Fehler als ein Objekt auf stdout: `{"error":{"kind":"<art>","message":"<meldung>"}}`. Der Exit-Code bleibt 1, die Meldung steht zusätzlich auf stderr. `kind` ist eine stabile Menge: `usage` (Flags, Argumente, ihre Kombinationen), `config`, `db` (Archiv nicht zu öffnen), `sql` (SQLite lehnt die Abfrage ab, auch `search --raw`), `not_found`, `ambiguous` (Session-Referenz), `internal` (alles andere). Das gilt auch für Fehler vor dem Befehl selbst (unbekanntes Flag), erkannt an `--json` bzw. `--format json|jsonl` in den Argumenten.
- **Schlüssel.** Jeder dokumentierte Schlüssel ist immer vorhanden. Ein String ohne Wert ist `null` (Typ `NullString`), Zahlen sind nie `null` (0), Listen und Maps nie `null` (`[]`, `{}`). Die Ausgabetypen haben kein `omitempty`; Typen in `store` und `harness` dürfen eigene Tags behalten, `doctor --json` gibt `harness.FormatReport` unverändert aus.
- **Kürzung.** `--format table` kürzt Zellen nur, wenn stdout ein Terminal ist: die Spalten werden auf die Terminalbreite eingepasst (die breitesten geben zuerst ab, keine unter 4 Zeichen), eine gekürzte Zelle endet mit `…`, und unter der Ausgabe steht auf stderr `N cells truncated, --no-truncate for full values`. In eine Pipe oder Datei wird nie gekürzt, `--no-truncate` erzwingt volle Zellen auch im Terminal. Zellen mit Zeilenumbrüchen oder Tabs werden in jedem Fall auf eine Zeile gezogen (Leerraum zu einem Leerzeichen), damit eine Zeile eine Tabellenzeile bleibt; `csv` und JSON behalten den Text unverändert. Betroffen sind die Tabellen von `sessions`, `sql`, `status`, `config show` sowie Name und Textausschnitt von `search`; die Zahlentabellen von `stats` werden nicht gekürzt.

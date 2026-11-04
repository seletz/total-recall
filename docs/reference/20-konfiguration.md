---
title: Konfiguration
icon: lucide/settings
---
# Konfiguration

Alles steht in einer TOML-Datei. Du brauchst sie nicht: Fehlt sie, gelten die Defaults.

## Wo liegt die Datei

| Reihenfolge | Ort |
|---|---|
| 1 | `--config PFAD` |
| 2 | Umgebungsvariable `TOTAL_RECALL_CONFIG` |
| 3 | `$XDG_CONFIG_HOME/total-recall/config.toml` |
| 4 | `~/.config/total-recall/config.toml` |

Die Datei wird über die dotfiles verwaltet.

## Rangfolge der Werte

Für jeden Wert gilt:

**Flag > Umgebungsvariable > Config-Datei > Default**

Beispiel: `--db /tmp/test.db` schlägt `TOTAL_RECALL_DB`, und das schlägt `db = …` in der Datei.

`total-recall config show` zeigt die wirksame Konfiguration. Zu jedem Wert steht, woher er kommt.

`total-recall config init` schreibt eine kommentierte Vorlage. Eine bestehende Datei wird nie überschrieben.

## Regeln

- `~` und Umgebungsvariablen (`$HOME`, `${VAR}`) werden in Pfaden aufgelöst.
- Unbekannte Schlüssel sind ein Fehler. So fallen Tippfehler sofort auf.

## Prüfung { #pruefung }

Beim Laden prüft jeder Befehl die Kerneinstellungen streng: Pfade, `timezone`, `day_start`, `[backup]`, `[ingest]`, `[redact]`, unbekannte Schlüssel und falsche Typen. Ein Fehler dort bricht jeden Befehl ab.

Die Abschnitte `[work]`, `[worktime]`, `[tui]` und `[keys]` werden erst geprüft, wenn ein Befehl sie braucht: `stats --workday`, `stats --worktime` und `tui` brechen mit der Fehlermeldung ab, `config show` listet die Probleme unter der Tabelle und endet mit Exit-Code 1, `doctor` meldet sie als Befund (Exit-Code 2). Hooks, `ingest`, `search` und alle anderen Befehle laufen trotzdem. Ein Tippfehler in der Tastenbelegung oder der Arbeitszeit stoppt also nie die Archivierung.

## Vollständige Vorlage

```toml
# ~/.config/total-recall/config.toml

db = "~/Library/Application Support/total-recall/archive.db"
timezone = "Europe/Berlin"      # für Auswertungen (stats, Tagesgrenzen)
day_start = "04:00"              # Tagesgrenze für die Aktivität je Tag
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
bundesland = "Baden-Württemberg" # gesetzliche Feiertage; unbekannt/leer = bundesweit
count = "human"                  # was zählt: "human" (eigene Nachrichten) oder "all"

[worktime]
max_gap = "20m"                  # Lücke zwischen eigenen Nachrichten, die voll zählt
min_credit = "3m"                # was eine längere Lücke zählt
grid = "15m"                     # Rundungsraster, 0 = keine Rundung
rounding = "up"                  # up | down | nearest
scope = "category"               # category | project: was gerundet wird
# [[worktime.category]]          # Regeln in Reihenfolge, die erste passende gewinnt
# name = "kunde-a"
# match = ["https://github.com/kunde-a/*", "~/src/kunde-a-*"]

[ingest]
tool_result_max_bytes = 4096
raw_max_bytes = 16384
store_raw = true

[harness.claude]
enabled = true
projects_dir = "~/.claude/projects"   # Default: $CLAUDE_CONFIG_DIR/projects bzw. ~/.claude/projects
skip_record_types = []                # zusätzlich zu den Standard-Ausschlüssen

[harness.claude.actors]
# zusätzliche Präfixe für die Actor-Klassifikation
peer_prefixes = []
automation_prefixes = ["Hourly ", "Weekend watch"]

[git]
resolve = true

# [accounts]                     # Namen für Account-IDs (sessions --account, stats --by account, show)
# "aaaaaaaa-0000-4000-8000-000000000001" = "work"

[tui]
view = "search"                  # Ansicht beim Start: search, stats, activity
graphics = "auto"                # Chart: auto, text, kitty (--graphics)
mouse = true                     # false = --no-mouse
stats_period = "day"             # Statistik beim Start: day, week, month
stats_dimension = "project"      # project, agent, directory
activity_window = "week"         # Aktivität beim Start: day, week, month

[keys]
# Tastenbelegung der TUI, Aktion = Taste oder Liste; siehe TUI, „Tastenbelegung anpassen“
# global_quit = ["q", "ctrl+q"]
# stats_earlier = "<"
```

## Die Schlüssel im Einzelnen

### Oberste Ebene

`db`
:   Pfad zur SQLite-Datenbank. Default auf macOS: `~/Library/Application Support/total-recall/archive.db`. Auf Linux: `$XDG_DATA_HOME/total-recall/archive.db`. Override: `--db`, `TOTAL_RECALL_DB`. Die Datei gehört **nicht** in iCloud, siehe [Datenschutz](../explanation/datenschutz.md).

`timezone`
:   Zeitzone für Auswertungen. Bestimmt, wo ein Tag anfängt und aufhört, etwa in `stats` und in der View `daily_activity`. In der Datenbank selbst stehen alle Zeiten in UTC. Default: `Europe/Berlin`. Override: `--tz`.

`day_start` { #day_start }
:   Uhrzeit (`HH:MM`, Ortszeit), zu der ein Tag für die erste und letzte Aktivität je Tag beginnt ([`stats --workday`](10-befehle.md#stats-workday), TUI-Ansicht „activity“). Aktivität davor zählt zum Vortag und wird als `01:30 (+1)` angezeigt. `00:00` ist die Kalendermitternacht. Default: `04:00`. Wirkt nicht auf `stats` ohne `--workday` und nicht auf `daily_activity`.

`log`
:   Logdatei für Fehler aus Hook-Läufen und Hintergrundprozessen (`--detach`) sowie Hinweise wie Backups vor einer Schema-Migration. Leer heißt: `total-recall.log` neben der Datenbank. Wird die Datei größer als 5 MiB, wird sie in `<log>.1` umbenannt (eine ältere Generation bleibt, eine noch ältere wird ersetzt).

### `[backup]`

`dir`
:   Zielordner für Backups. Default: `~/Documents/total-recall`, also iCloud. Override: `--backup-dir`, `TOTAL_RECALL_BACKUP_DIR`.

`keep_daily`
:   So viele Tageskopien bleiben erhalten. Default: 7.

`keep_monthly`
:   So viele Monatskopien bleiben erhalten. Eine Monatskopie ist die letzte Kopie eines Monats. `0` heißt: alle behalten.

`min_interval`
:   Reserviert. Derzeit ungenutzt: `backup --if-older` braucht eine ausdrückliche Dauer, etwa `--if-older 24h` wie im Hook.

### `[redact]`

`enabled`
:   Geheimnisse vor dem Speichern maskieren. Default: `true`. Abschalten ist nur für Tests sinnvoll.

`extra_patterns`
:   Zusätzliche reguläre Ausdrücke (Go-Syntax). Treffer werden maskiert.

`allow`
:   Reguläre Ausdrücke für Text, der nie maskiert werden darf. Hilft bei Fehlalarmen.

### `[ingest]`

`tool_result_max_bytes`
:   Tool-Ergebnisse (etwa die Ausgabe eines Shell-Befehls) werden im Feld `text` auf diese Länge gekürzt. Default: 4096 Bytes.

`raw_max_bytes`
:   Obergrenze für die Original-JSON-Zeile. Ist eine Zeile größer, bleibt `raw` leer (NULL) und `raw_truncated` wird 1. Der lesbare Text (`text`) wird trotzdem gespeichert. Große Zeilen sind fast immer Tool-Aufrufe und Tool-Ergebnisse (Dateiinhalte, Befehlsausgaben); sie machen sonst den Großteil der Datenbank aus. `0` heißt: keine Grenze. Default: 16384 Bytes.

`store_raw`
:   Die Original-JSON-Zeile jedes Events (maskiert) in der Spalte `raw` speichern. Kostet Platz, erlaubt aber spätere Neuauswertung. Default: `true`.

### `[harness.claude]`

`enabled`
:   Claude Code einlesen. Default: `true`.

`projects_dir`
:   Wo die Transkripte liegen. Default: `$CLAUDE_CONFIG_DIR/projects`, sonst `~/.claude/projects`.

`skip_record_types`
:   Weitere Record-Typen, die übersprungen werden. Die Standard-Ausschlüsse (`attachment`, `api-request*`, `file-history-*` usw.) gelten immer.

### `[harness.claude.actors]`

Jedes Event bekommt einen `actor`: Wer hat das geschrieben? Mensch, Agent, ein anderer Agent oder eine Automatik. Die Regeln stehen im [Design](../explanation/design.md#actor-klassifikation-claude-code). Hier ergänzt du eigene Präfixe.

`peer_prefixes`
:   Nachrichten, die mit einem dieser Texte beginnen, gelten als `peer` (ein anderer Agent).

`automation_prefixes`
:   Nachrichten, die mit einem dieser Texte beginnen, gelten als `automation`. Beispiel: geplante Prompts wie „Hourly …“.

### `[accounts]` { #accounts }

Namen für die Account- und Organisations-IDs, die Claude Code seit 2.1.317 in jede Session schreibt (`session-owner`-Record; Spalten `account_id` und `organization_id`). Schlüssel ist die ID, Wert der Name. Wer zwischen zwei Accounts wechselt, sieht mit den Namen auf einen Blick, welche Session wo lief.

```toml
[accounts]
"aaaaaaaa-0000-4000-8000-000000000001" = "work"
"bbbbbbbb-0000-4000-8000-000000000002" = "privat"
```

Die Namen nutzen [`sessions --account`](10-befehle.md#sessions) und [`search --account`](10-befehle.md#search) als Filter, [`stats --by account`](10-befehle.md#stats-period) als Schlüssel, `show` im Kopf und `sessions --json` im Feld `account`. IDs ohne Eintrag erscheinen unverändert. Der Abschnitt ist optional und wird wie die Kerneinstellungen beim Laden geprüft (nur Strings).

### `[work]` { #work }

Normalarbeitszeit und Feiertage für die Aktivität je Tag ([`stats --workday`](10-befehle.md#stats-workday), TUI-Ansicht „activity“; die Feiertage auch in der Statistik-Ansicht).

`hours`
:   Kernzeit als `HH:MM-HH:MM` (Ortszeit des Kalendertags, Beginn vor Ende, nicht vor `day_start`). Die TUI zeigt sie als Band auf der Zeitachse und färbt Aktivität davor und danach als früh bzw. spät. Default: `09:00-17:00`.

`target`
:   Tagessoll an Arbeitstagen (Montag bis Freitag ohne Feiertage); Wochenende und Feiertage haben keines. Die Spanne eines Tags mit Aktivität wird daran gemessen, die Woche oder der Monat gegen Soll × Arbeitstage der Periode (bis heute, mit oder ohne Aktivität). Default: `8h`.

`daily_low`, `daily_high`
:   Eine Spanne unter `daily_low` gilt als unter Soll, über `daily_high` als deutlich darüber, dazwischen als im Rahmen; danach färbt die TUI Spanne, Differenz und die Kennzahlen der Spanne. Default: `6h` und `9h`.

`weekly_low`, `weekly_high`
:   Rahmen für die Arbeitszeit einer Woche mit fünf Arbeitstagen: darunter unter Soll, darüber deutlich darüber; danach färbt die TUI die Kopfzeile von Woche und Monat. Eine Periode mit anderer Zahl von Arbeitstagen (Feiertag, laufender Monat) skaliert den Rahmen anteilig: vier Arbeitstage 25h36m–38h24m, ein Monat mit 21 Arbeitstagen 134h24m–201h36m. Default: `32h` und `48h`.

`bundesland`
:   Bundesland für die gesetzlichen Feiertage: `Baden-Württemberg`, `Bayern`, `Berlin`, `Brandenburg`, `Bremen`, `Hamburg`, `Hessen`, `Mecklenburg-Vorpommern`, `Niedersachsen`, `Nordrhein-Westfalen`, `Rheinland-Pfalz`, `Saarland`, `Sachsen`, `Sachsen-Anhalt`, `Schleswig-Holstein`, `Thüringen` (Schreibweise ohne Bindestrich und Groß-/Kleinschreibung egal) oder `Deutschland` für nur die bundesweiten. Ein unbekannter oder leerer Name ist kein Fehler und bedeutet `Deutschland`. Default: `Baden-Württemberg`.

`count`
:   Was die Aktivität je Tag zählt: `human` (nur eigene Nachrichten; Agent-Events laufen nachts weiter und dehnen die Spanne) oder `all` (eigene Nachrichten und Agent-Events wie die Aktivitätsstatistik). `stats --workday --all-events` und die Taste `h` in der TUI schalten für einen Lauf um. Default: `human`.

### `[worktime]` { #worktime }

Arbeitszeit je Projekt und Tag aus deinen eigenen Nachrichten ([`stats --worktime`](10-befehle.md#stats-worktime)), als Grundlage für Stundenzettel. Die Lücke zwischen zwei aufeinanderfolgenden eigenen Nachrichten, über alle Sessions hinweg, zählt für das Projekt der späteren. Meetings und Arbeit ohne Agent hinterlassen keine Nachricht: die Werte sind eine Untergrenze.

`max_gap`
:   Längste Lücke, die voll zählt. Default: `20m`.

`min_credit`
:   Was eine längere Lücke zählt (Pause, Meeting, erste Nachricht des Tages). Zwischen 0 und `max_gap`. Default: `3m`.

`grid`
:   Rundungsraster für die Summe; `0m` rundet nicht. Default: `15m`.

`rounding`
:   Richtung: `up` (aufrunden), `down` (abrunden) oder `nearest` (kaufmännisch, halbe Raster aufwärts). Default: `up`.

`scope`
:   Was gerundet wird: `category` rundet die Summe einer Kategorie je Tag einmal; `project` rundet jedes Projekt je Tag, die Kategorie ist die Summe der gerundeten Projekte (rundet bei mehreren Projekten je Kategorie stärker auf). Default: `category`.

`summary_max_chars`
:   Länge des Buchungstexts je Kategorie und Tag, den [`stats --worktime --summary`](10-befehle.md#stats-worktime) aus den Session-Titeln und den Issue-/PR-Referenzen deiner Nachrichten baut, in Zeichen; ein längerer Text endet mit `…`. `0` schaltet die Grenze ab, negativ ist ein Fehler. Default: `200`.

`[[worktime.category]]`
:   Regeln, die Projekte Kategorien zuordnen, in der Reihenfolge der Datei; die erste passende gewinnt. Je Regel `name` (die Kategorie) und `match` (ein Muster oder eine Liste). Ein Muster wird als Ganzes gegen den Projektnamen (wie bei `stats --by project`), den Git-Root bzw. das Arbeitsverzeichnis (als Pfad und mit `~` für das Home-Verzeichnis) und die Remote-URL geprüft; `*` steht für beliebigen Text, auch über `/` hinweg, `?` für ein Zeichen, alles andere ist wörtlich. Ein Projekt ohne passende Regel ist seine eigene Kategorie und heißt wie das Projekt. Eine letzte Regel mit `match = "*"` sammelt den Rest. Gleiche Namen zweimal, eine Regel ohne Namen oder ohne Muster sind Fehler. Default: keine Regeln.

```toml
[[worktime.category]]
name = "kunde-a"
match = ["https://github.com/kunde-a/*", "~/src/kunde-a-*"]

[[worktime.category]]
name = "intern"
match = ["total-recall", "dotfiles"]

[[worktime.category]]
name = "sonstiges"
match = "*"
```

### `[git]`

`resolve`
:   Zu jedem Arbeitsverzeichnis Git-Root und Remote-URL ermitteln. Default: `true`. Der Branch kommt aus dem Transkript. Das Ergebnis wird in der Tabelle `dirs` gespeichert, damit es nach dem Löschen eines Worktrees erhalten bleibt. Ein gelöschter Worktree unter `<repo>__worktrees/<name>` wird dem Repo `<repo>` daneben zugeordnet. „Kein Git-Repo“ wird nach 7 Tagen neu geprüft.

### `[tui]` { #tui }

Defaults für [`total-recall tui`](30-tui.md). Flags und das Argument der Ansicht gehen vor: **Flag > Config > Default**.

`view`
:   Ansicht beim Start: `search`, `stats` oder `activity`. Das Argument `total-recall tui stats` geht vor. Default: `search`.

`graphics`
:   Darstellung des Charts in der Statistik: `auto`, `text` oder `kitty`, siehe [Grafikmodi](30-tui.md#grafikmodi). Override: `--graphics`. Default: `auto`.

`mouse`
:   `true`: Mausrad und Klicks gehen an die TUI. `false`: die Maus bleibt beim Terminal, Text lässt sich durch einfaches Ziehen auswählen, siehe [Maus](30-tui.md#maus). Override: `--no-mouse` bzw. `--no-mouse=false`. Default: `true`.

`stats_period`, `stats_dimension`
:   Periode (`day`, `week`, `month`) und Dimension (`project`, `agent`, `directory`), mit denen die Statistik startet. Default: `day` und `project`.

`activity_window`
:   Fenster, mit dem die Aktivität je Tag startet: `day`, `week` oder `month`, jeweils die Periode, die heute enthält. Default: `week`.

### `[keys]` { #keys }

Tastenbelegung der TUI: je Aktion eine Taste (`global_quit = "x"`) oder eine Liste (`global_quit = ["q", "ctrl+q"]`). Nur genannte Aktionen ändern sich. Die Aktionen, ihre Standardbelegung, die erlaubten Tastennamen und die Regeln für Konflikte stehen unter [Tastenbelegung anpassen](30-tui.md#tastenbelegung-anpassen). Ein unbekannter Aktionsname, eine unbekannte Taste oder ein Konflikt ist ein Fehler für `tui`; `config show` und `doctor` melden ihn, alle anderen Befehle laufen weiter (siehe [Prüfung](#pruefung)). `config show` zeigt jede Aktion als `keys.<aktion>` mit Belegung und Herkunft.

## Umgebungsvariablen

| Variable | Wirkt auf |
|---|---|
| `TOTAL_RECALL_CONFIG` | Pfad der Config-Datei |
| `TOTAL_RECALL_DB` | `db` |
| `TOTAL_RECALL_BACKUP_DIR` | `backup.dir` |
| `CLAUDE_CONFIG_DIR` | Default von `harness.claude.projects_dir` |
| `XDG_CONFIG_HOME`, `XDG_DATA_HOME` | Default-Pfade |

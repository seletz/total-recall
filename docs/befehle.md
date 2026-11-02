# Befehle

Überblick über alle Befehle. Alle Beispielausgaben sind erfunden.

## Globale Optionen

| Option | Bedeutung |
|---|---|
| `--config PFAD` | andere Config-Datei (sonst `TOTAL_RECALL_CONFIG` bzw. `~/.config/total-recall/config.toml`) |
| `--db PFAD` | andere Datenbank (sonst `TOTAL_RECALL_DB` bzw. `db` aus der Config) |
| `--tz ZONE` | Zeitzone für Anzeige und Tagesgrenzen, Default `Europe/Berlin` |
| `--backup-dir PFAD` | anderer Backup-Ordner (sonst `TOTAL_RECALL_BACKUP_DIR` bzw. `backup.dir`) |

Alle lesenden Befehle (`status`, `sessions`, `show`, `search`, `stats`, `sql`, `config show`) haben `--json`.

Zeiten in der Ausgabe sind in lokaler Zeit (`--tz`). In der Datenbank und in der JSON-Ausgabe stehen sie in UTC (`2026-10-31T22:21:58.123Z`).

Zeitangaben für `--since` und `--until` sind lokale Zeit: `2026-10-01`, `2026-10-01T14:30` oder RFC 3339. Ein reines Datum bei `--until` schließt den ganzen Tag ein: `--until 2026-10-31` heißt „bis einschließlich 31. Oktober“.

Die lesenden Befehle öffnen die Datenbank nur lesend. Ist ihr Schema älter als das Programm, gibt es eine Warnung; ein schreibender Befehl wie `ingest` migriert sie (siehe [Design](design.md#schema-versionierung)).

## `ingest` { #ingest }

Liest Transkripte in die Datenbank ein. Gelesen wird nur, was seit dem letzten Lauf dazugekommen ist, und nur vollständige Zeilen.

```sh
total-recall ingest ~/.claude/projects/-Users-me-repo/abc.jsonl   # einzelne Datei
total-recall ingest --all                                         # alle Quellen nachlesen
total-recall ingest --hook --detach < hook.json                   # aus einem Hook
```

| Option | Bedeutung |
|---|---|
| `--all` | alle Quellen aller aktiven Harnesses |
| `--hook` | Hook-JSON von stdin lesen (`transcript_path`, `agent_transcript_path`, `session_id`, `cwd`, `hook_event_name`) und diese Datei(en) einlesen. Bei `SessionStart` zusätzlich alles wie `--all`. Exit-Code immer 0, Fehler ins Log, nichts auf stdout |
| `--regit` | Git-Root und Remote-URL für das Arbeitsverzeichnis jeder Session neu bestimmen (am Cache vorbei) und die Sessions aktualisieren. Für veraltete Git-Infos, etwa nach einem umbenannten Remote. Ein Verzeichnis, das sich nicht mehr auflösen lässt (gelöschter Worktree), behält seinen gespeicherten Wert. Allein oder zusammen mit `--all`; braucht `git.resolve = true` |
| `--detach` | als losgelöster Hintergrundprozess weiterlaufen und sofort zurückkehren. Ausgabe und Fehler gehen ins Log |

Ausgabe:

```text
scanned 291 files: 4 changed, 287 unchanged, 0 locked; 22 new events, 3 sessions touched (55ms)
```

- `changed`: Dateien mit neuen Zeilen. `unchanged`: nichts Neues. `locked`: gerade von einem anderen Lauf gelesen, übersprungen.
- Fehler je Datei stehen auf stderr; die übrigen Dateien werden trotzdem gelesen. Gab es Fehler, ist der Exit-Code 1.
- Mit `--regit` kommt eine Zeile dazu: `regit: 57 directories, 12 sessions updated (1.2s)`.

Mehr dazu: [Hooks einrichten](hooks.md).

## `status` { #status }

Zeigt den Zustand des Archivs.

```sh
total-recall status
total-recall status --json
total-recall status --errors 20     # mehr Fehlerzeilen aus dem Log
```

```text
database              /Users/me/Library/Application Support/total-recall/archive.db
size                  612.4 MiB
schema                v2 (binary supports v2)
sessions              274
events                158203
sources               280
last ingest (claude)  2026-11-01 14:59  (2m ago)
last backup           /Users/me/Documents/total-recall/archive-2026-11-01.db.gz  (5h12m ago)
log                   /Users/me/Library/Application Support/total-recall/total-recall.log

recent errors:
  2026-10-30T08:12:01Z ERROR hook Stop: /Users/me/.claude/projects/x/y.jsonl: permission denied
```

`schema` zeigt die Schema-Version der Datenbank und die, die das Programm kennt. Die JSON-Ausgabe enthält dieselben Werte (`schema_version`, `binary_schema_version`, `db_size_bytes`, `last_ingest`, `last_backup`, `recent_errors`, …).

## `sessions` { #sessions }

Listet Sessions, neueste zuerst. Subagent-Sessions sind ausgeblendet, außer mit `--subagents`. Ebenso Sessions ohne Events und ohne Startzeit (etwa wenn von einer Session nur ein Titel-Record gesehen wurde), außer mit `--all-sessions`.

```sh
total-recall sessions --since 2026-10-01 --until 2026-10-31
total-recall sessions --repo seletz/total-recall
total-recall sessions --name release --json
total-recall sessions --limit 10
```

```text
START             DUR    ID        NAME                        REPO                 BRANCH      HUMAN  AGENT
2026-10-31 19:38  2h59m  9a1b2c3d  Release-Pipeline aufsetzen  seletz/total-recall  develop     14     212
2026-10-31 09:05  45m    4e5f6a7b  Backup-Retention prüfen     seletz/total-recall  feature/bk  6      58
```

| Option | Bedeutung |
|---|---|
| `--since`, `--until` | Sessions, die im Zeitraum aktiv waren |
| `--repo TEXT` | Teilstring von Remote-URL, Git-Root oder Arbeitsverzeichnis |
| `--harness NAME` | z.B. `claude` |
| `--name TEXT` | Teilstring des Namens (eigener Titel, KI-Titel oder Agent-Name) |
| `--limit N` | höchstens N Zeilen (0 = alle, Default) |
| `--subagents` | Subagent-Sessions mit anzeigen |
| `--all-sessions` | auch leere Sessions (keine Events, keine Startzeit) anzeigen |

Spalten: `ID` ist ein Präfix der nativen Session-ID, das `show` versteht. `NAME` ist der eigene Titel (`/rename`), sonst der von Claude Code erzeugte Titel, sonst der Agent-Name. `REPO` ist `owner/repo` der Remote-URL, sonst der Ordnername. `HUMAN`/`AGENT` zählen Events nach Actor. `DUR` ist Ende minus Start, inklusive Pausen.

## `show` { #show }

Zeigt den Verlauf einer Session: deine Anfragen und die Antworten des Agenten.

```sh
total-recall show 9a1b2c3d                      # Präfix aus `sessions`
total-recall show claude:9a1b2c3d-…             # volle ID
total-recall show 9a1b2c3d --all                # inklusive Tool-Aufrufe, Ergebnisse, Automatik
total-recall show 9a1b2c3d --json
```

```text
Release-Pipeline aufsetzen
claude:9a1b2c3d-0000-4000-8000-000000000000
time    2026-10-31 19:38 – 2026-10-31 22:37  (2h59m)
repo    https://github.com/seletz/total-recall
branch  develop
cwd     /Users/me/develop/total-recall

── 2026-10-31 19:38:02  human
Bitte richte goreleaser mit einem Homebrew-Cask ein.

── 2026-10-31 19:41:15  agent
Erledigt: .goreleaser.yml angelegt, Cask im Tap konfiguriert.
```

Die Session kann über die volle ID (`claude:<uuid>`), die native ID oder ein eindeutiges Präfix davon angegeben werden. Ohne `--all` zeigt `show` nur Nachrichten von Mensch, Agent und anderen Agenten (`peer`).

## `search` { #search }

Volltextsuche (SQLite FTS5) mit Textausschnitten, beste Treffer zuerst.

```sh
total-recall search goreleaser
total-recall search backup retention --since 2026-10-01      # beide Wörter müssen vorkommen
total-recall search --raw 'cask AND (homebrew OR tap)'       # FTS5-Syntax
total-recall search redaction --actor human --repo seletz/total-recall --json
```

```text
2026-10-31 19:38  Release-Pipeline aufsetzen  seletz/total-recall  claude:9a1b2c3d-…  [human]
    Bitte richte [goreleaser] mit einem Homebrew-Cask ein.
```

| Option | Bedeutung |
|---|---|
| Filter wie `sessions` | `--since`/`--until` gelten hier für die Zeit des Events |
| `--actor A` | nur `human`, `agent`, `peer`, `automation` oder `tool` |
| `--raw` | Anfrage unverändert an FTS5: `AND`, `OR`, `NOT`, `"genaue Phrase"`, `präfix*` |
| `--limit N` | höchstens N Treffer (Default 20, 0 = alle) |

Ohne `--raw` wird jedes Wort in Anführungszeichen gesetzt. Dann müssen alle Wörter vorkommen, und Sonderzeichen sind harmlos.

## `stats` { #stats }

Kennzahlen für einen Zeitraum: insgesamt, je Tag, je Repo und je Wochentag × Stunde; mit [`--period`/`--by`](#stats-period) eine Zeitreihe, mit [`--workday`](#stats-workday) die erste und letzte Aktivität je Tag. Gezählt werden Sessions (mit mindestens einem menschlichen oder Agenten-Event im Bereich), menschliche und Agenten-Events. Tage und Stunden in lokaler Zeit.

```sh
total-recall stats                                   # aktueller Monat
total-recall stats --month 2026-10
total-recall stats --since 2026-10-01 --until 2026-10-15 --json
```

```text
2026-10-01 00:00 – 2026-10-31 23:59 (Europe/Berlin)
total: 120 sessions, 1310 human, 30544 agent events

DAY         SESSIONS  HUMAN  AGENT
2026-10-01  6         41     902
…

REPO                 SESSIONS  HUMAN  AGENT
seletz/total-recall  31        402    9120
…

human events per weekday × hour
     00  01  …  09  10  11  …
Mon  .   .   …  12  30  25  …
```

Die JSON-Ausgabe ist die Grundlage für `monthly_stats.py` im Vault:

```json
{
  "since": "2026-09-30T22:00:00.000Z",
  "until": "2026-10-31T23:00:00.000Z",
  "timezone": "Europe/Berlin",
  "total": {"sessions": 120, "human": 1310, "agent": 30544},
  "by_day": [{"day": "2026-10-01", "sessions": 6, "human": 41, "agent": 902}],
  "by_repo": [{"repo": "https://github.com/seletz/total-recall", "sessions": 31, "human": 402, "agent": 9120}],
  "by_weekday_hour": [{"weekday": 0, "day": "Mon", "hour": 0, "sessions": 0, "human": 0, "agent": 0}]
}
```

`by_weekday_hour` enthält immer alle 7 × 24 Zellen, `weekday` 0 ist Montag. `repo` ist die Remote-URL, sonst der Git-Root, sonst das Arbeitsverzeichnis.

### Zeitreihe: `--period`, `--by`, `--top` { #stats-period }

Mit `--period` oder `--by` liefert `stats` statt der drei Tabellen eine Zeitreihe: eine Zeile je Periode und, mit `--by`, je Projekt, Agent-Typ oder Arbeitsverzeichnis. Der Zeitraum kommt wie oben aus `--month` oder `--since`/`--until`; Periodengrenzen liegen in lokaler Zeit, Wochen sind ISO-Wochen ab Montag. Das ersetzt `monthly_stats.py` im Vault.

```sh
total-recall stats --period week --by project --since 2026-08-01 --until 2026-10-31
total-recall stats --period month --by agent --top 6 --json
total-recall stats --month 2026-10 --period day --by directory
total-recall stats --period month --since 2026-01-01          # nur je Monat, ohne Aufschlüsselung
```

| Option | Bedeutung |
|---|---|
| `--period day\|week\|month` | Breite der Perioden. Ohne `--by` eine Zeile je Periode mit Sessions, human- und agent-Events, leere Perioden mit 0, zuletzt eine Zeile `total` für den ganzen Zeitraum |
| `--by project\|agent\|directory` | zusätzlich je Periode nach Schlüssel aufschlüsseln; ohne `--period` gilt `day`. `project`: Projektname (Basisname des Git-Roots; bei gleichen Namen so viele übergeordnete Verzeichnisse wie nötig), Worktrees zählen zum Hauptrepository. Verzeichnisse ohne Repository erscheinen als Pfad, im Text mit `~` für das Home-Verzeichnis (`~/notes`), in JSON als voller Pfad (`/Users/me/notes`), damit Skripte nicht vom Home des Aufrufers abhängen. `agent`: `claude main` oder `claude subagent`. `directory`: Arbeitsverzeichnis. Subagent-Sessions zählen bei `project` und `directory` zu ihrer Eltern-Session |
| `--top N` | nur mit `--by`: die N größten Schlüssel über den ganzen Zeitraum, der Rest je Periode als `other` (immer zuletzt). Default 0 = alle |

Die Textausgabe ist ein Langformat ohne Kopfzeilen über der Tabelle, damit sie sich mit `sort`, `awk` oder `column` weiterverarbeiten lässt. Mit `--by` erscheint je Periode nur, was Aktivität hat; Schlüssel sind nach Gesamtvolumen sortiert, `other` zuletzt. Den Schluss bilden Zeilen mit der Periode `total`: ohne `--by` eine für den Zeitraum, mit `--by` eine je Schlüssel und eine über alle Schlüssel (KEY `*`).

`SESSIONS` zählt zweierlei: In einer Periodenzeile zählt jede Session, die in der Periode aktiv war; eine Session über drei Tage steht bei `--period day` in drei Zeilen. In den `total`-Zeilen zählt jede Session im Zeitraum genau einmal. Die Summe der Periodenzeilen ist deshalb meist größer als `total`. `HUMAN` und `AGENT` sind in beiden Fällen einfache Summen.

```text
PERIOD    KEY           SESSIONS  HUMAN  AGENT
2026-W40  total-recall  12        240    4100
2026-W40  infra         3         31     410
2026-W40  ~/notes       2         18     95
2026-W41  total-recall  9         180    3300
total     total-recall  18        420    7400
total     infra         3         31     410
total     ~/notes       2         18     95
total     *             23        469    7905
```

JSON hat immer dieselben Felder. `since`/`until` sind der gefilterte Zeitraum in UTC, `start` der Periodenbeginn in UTC, `label` die Periode in lokaler Zeit (`2026-10-05`, `2026-W41`, `2026-10`). Jede Periode des Zeitraums ist enthalten, auch leere; `sessions`, `human`, `agent` auf Periodenebene sind die Summe über alle Schlüssel. `cells` enthält nur Schlüssel mit Aktivität. `total` ist der ganze Zeitraum, `by_key` dasselbe je Schlüssel in der Reihenfolge von `keys`; dort zählt `sessions` jede Session einmal, in den Perioden einmal je Periode mit Aktivität. Ohne `--by` sind `by` leer, `keys`, `by_key` und `cells` leere Listen.

```json
{
  "since": "2026-09-30T22:00:00.000Z",
  "until": "2026-10-31T23:00:00.000Z",
  "timezone": "Europe/Berlin",
  "period": "week",
  "by": "project",
  "keys": ["total-recall", "infra", "other"],
  "total": {"sessions": 23, "human": 469, "agent": 7905},
  "by_key": [
    {"key": "total-recall", "sessions": 18, "human": 420, "agent": 7400},
    {"key": "infra", "sessions": 3, "human": 31, "agent": 410},
    {"key": "other", "sessions": 2, "human": 18, "agent": 95}
  ],
  "buckets": [
    {
      "start": "2026-10-04T22:00:00.000Z",
      "label": "2026-W41",
      "sessions": 12, "human": 240, "agent": 4100,
      "cells": [
        {"key": "total-recall", "sessions": 9, "human": 180, "agent": 3300},
        {"key": "infra", "sessions": 3, "human": 60, "agent": 800}
      ]
    }
  ]
}
```

### Arbeitszeit: `--workday` { #stats-workday }

Mit `--workday` zeigt `stats` je Tag die **erste und letzte Aktivität** und die Spanne dazwischen: ein Notnagel, wenn das Ein- oder Ausstempeln vergessen wurde. Dieselbe Ansicht hat die [TUI](tui.md#aktivitat) als Reiter „activity“.

```sh
total-recall stats --workday                          # diese Woche
total-recall stats --workday --range last-week
total-recall stats --workday --range last-month --json
total-recall stats --workday --since 2026-10-01 --until 2026-10-15 --human-only
```

```text
2026-10-26 04:00 – 2026-11-02 03:59 (Europe/Berlin, day starts 04:00, human and agent events)

DATE        DAY  FIRST  LAST        SPAN
2026-10-26  Mon  08:12  18:40       10h28m
2026-10-27  Tue  09:03  01:30 (+1)  16h27m
2026-10-28  Wed  -      -           -
2026-10-29  Thu  07:55  17:10       9h15m
…

total: 5 days with activity, 41h40m
```

| Option | Bedeutung |
|---|---|
| `--range today\|this-week\|last-week\|this-month\|last-month` | Zeitraum relativ zu jetzt. Wochen sind ISO-Wochen ab Montag. Nur mit `--workday`, nicht zusammen mit `--since`/`--until`/`--month` |
| `--since`, `--until`, `--month` | wie oben; ein Datum meint den ganzen Tag ab der Tagesgrenze, `--until` einschließlich. Ohne Zeitraum gilt `this-week` |
| `--human-only` | nur eigene Nachrichten zählen (sonst wie die Aktivitätsstatistik: eigene Nachrichten und Agent-Events) |

**Tagesgrenze.** Ein Tag beginnt um `day_start` aus der [Konfiguration](konfiguration.md#day_start), Standard 04:00 Ortszeit. Aktivität davor zählt zum Vortag und steht als `01:30 (+1)` in der Spalte; deshalb beginnt auch der Zeitraum um 04:00. `today` kurz nach Mitternacht ist noch der Vortag. Die Spanne ist die verstrichene Zeit, auch über eine Zeitumstellung hinweg. Alle Tage des Zeitraums erscheinen, Tage ohne Aktivität mit `-`.

JSON hat je Tag die Zeiten in UTC (`first`, `last`) und als Ortszeit (`first_local`, `last_local`, mit `(+1)`), die Spanne in Sekunden und die Zähler. Ein Tag ohne Aktivität hat leere Zeiten und Nullen.

```json
{
  "since": "2026-10-26T03:00:00.000Z",
  "until": "2026-11-02T03:00:00.000Z",
  "timezone": "Europe/Berlin",
  "day_start": "04:00",
  "human_only": false,
  "days": [
    {
      "day": "2026-10-27", "weekday": "Tue",
      "first": "2026-10-27T08:03:00.000Z", "last": "2026-10-28T00:30:00.000Z",
      "first_local": "09:03", "last_local": "01:30 (+1)",
      "span_seconds": 59220,
      "sessions": 3, "human": 41, "agent": 902
    }
  ],
  "total": {"days": 5, "span_seconds": 150000}
}
```

`--workday` lässt sich nicht mit `--period`, `--by` oder `--top` kombinieren.

## `tui` { #tui }

Öffnet das Archiv in einer Terminal-Oberfläche: Volltextsuche mit Vorschau beim Tippen, Detailansicht je Session, Aktivität als Chart, Tabelle und Heatmap, erste und letzte Aktivität je Tag. Nur lesend. Bedienung, Tasten und Grafikmodi: [TUI](tui.md).

```sh
total-recall tui                              # Suche
total-recall tui stats                        # Statistik
total-recall tui activity                     # erste und letzte Aktivität je Tag
total-recall tui --query "goreleaser cask"    # Suche mit vorbelegtem Text
total-recall tui stats --graphics text        # Chart als Text, keine Terminal-Abfrage
```

| Argument / Option | Bedeutung |
|---|---|
| `search` \| `stats` \| `activity` | Ansicht beim Start, Default `search` |
| `--query TEXT` | Suchtext vorbelegen; die Suche läuft sofort |
| `--graphics auto\|text\|kitty` | Darstellung des Charts in der Statistik. `auto` sondiert das Terminal und zeigt ein Bild nur in Ghostty und kitty, sonst Text. `text` zeigt immer Blockzeichen und schickt keine Abfrage ans Terminal. `kitty` erzwingt das Bild ohne Sondierung. Default `auto` |

Ein unbekannter Modus oder eine unbekannte Ansicht ist ein Fehler. Beenden mit `q` oder ++ctrl+c++. Kein `--json`; die TUI ist ein Spike, siehe [Spike: TUI](spike-tui.md#ergebnis).

## `sql` { #sql }

Führt eine SQL-Abfrage aus, nur lesend. Die Datenbank wird mit `mode=ro` und `PRAGMA query_only` geöffnet. Ändern ist nicht möglich.

```sh
total-recall sql "SELECT count(*) FROM sessions"
total-recall sql --json "SELECT id, display_name FROM sessions LIMIT 5"
total-recall sql --format csv "SELECT * FROM daily_activity" > aktivitaet.csv
```

| Option | Bedeutung |
|---|---|
| `--format table\|json\|csv` | Ausgabeformat, Default `table` |
| `--json` | Kurzform für `--format json` (Liste von Objekten, Spalten in Abfrage-Reihenfolge) |

Schema und Beispiele: [Auswertung](auswertung.md) und `total-recall llm --skill`.

## `backup` { #backup }

Schreibt eine konsistente, mit gzip komprimierte Kopie der Datenbank nach `<backup.dir>/archive-YYYY-MM-DD.db.gz`. Das Datum ist in der konfigurierten Zeitzone. Eine Kopie vom selben Tag wird ersetzt, auch eine unkomprimierte `archive-YYYY-MM-DD.db` älterer Versionen. Danach werden alte Kopien nach den Regeln in `[backup]` aufgeräumt; komprimierte und alte unkomprimierte Kopien zählen dabei gleich.

Die Kopie entsteht per `VACUUM INTO` zuerst als Temporärdatei neben der Datenbank (nicht im Backup-Ordner, der von iCloud synchronisiert wird), wird dann komprimiert und erst zum Schluss unter ihrem endgültigen Namen abgelegt. Im Backup-Ordner liegt also nie eine halbe Kopie unter dem richtigen Namen.

```sh
total-recall backup
total-recall backup --if-older 24h            # nur, wenn das letzte Backup älter ist
total-recall backup --if-older 24h --detach   # aus einem Hook
```

```text
backup written: /Users/me/Documents/total-recall/archive-2026-11-01.db.gz
```

| Option | Bedeutung |
|---|---|
| `--if-older DAUER` | nichts tun, wenn die neueste Kopie jünger ist (Go-Dauer: `24h`, `90m`) |
| `--detach` | im Hintergrund laufen, sofort mit 0 zurückkehren; Ausgabe und Fehler ins Log |

## `restore` { #restore }

Stellt eine Backup-Kopie wieder her: entpackt `archive-YYYY-MM-DD.db.gz` (oder kopiert eine alte, unkomprimierte `.db`) in eine Datenbankdatei.

```sh
total-recall restore ~/Documents/total-recall/archive-2026-11-01.db.gz --to /tmp/archive.db
total-recall --db /tmp/archive.db sessions --limit 5     # Inhalt ansehen
total-recall restore ~/Documents/total-recall/archive-2026-11-01.db.gz --force   # laufende DB ersetzen
```

```text
restored /Users/me/Documents/total-recall/archive-2026-11-01.db.gz to /tmp/archive.db
```

| Option | Bedeutung |
|---|---|
| `--to PFAD` | Ziel; Default ist die konfigurierte Datenbank |
| `--force` | eine bestehende Zieldatei ersetzen (samt `-wal`/`-shm`) |

Ein bestehendes Ziel wird ohne `--force` nie überschrieben. Die Daten landen zuerst in einer Temporärdatei neben dem Ziel und werden geprüft (Schema-Version, `PRAGMA quick_check`); erst dann wird das Ziel ersetzt. Vor `--force` auf die laufende Datenbank: sicherstellen, dass gerade kein `ingest` läuft. Was seit dem Backup dazukam, liest das nächste `ingest --all` wieder ein, solange die Transkripte noch da sind.

## `doctor` { #doctor }

Prüft die Transkripte einer Harness auf Format-Drift: Hat Claude Code etwas Neues eingeführt, das `total-recall` noch nicht kennt? Gelesen wird nur, die Datenbank wird nicht geöffnet.

```sh
total-recall doctor                  # Claude Code
total-recall doctor --json
```

```text
harness claude: 291 sources, 650112 lines
versions (newest first): 2.1.316 (8492), 2.1.314 (1826), 2.1.312 (60947), …

unknown record types:
  hologram-state                           12  first: /Users/me/.claude/projects/-Users-me-repo/9a1b….jsonl:41

findings: the adapter may need an update (see docs: Hooks, "Nach einem Claude-Code-Update")
```

Gemeldet werden:

| Befund | Bedeutung |
|---|---|
| unbekannte Record-Typen | weder eingelesen noch als bewusst übersprungen bekannt (`skip_record_types` aus der Config zählt als bekannt) |
| unbekannte Content-Block-Typen | in `user`-/`assistant`-Nachrichten oder im Inhalt eines `tool_result`, z.B. `assistant/hologram` |
| fehlende Felder | `timestamp`, `sessionId`, `uuid`, `cwd` bei `user`/`assistant`/`system`; `sessionId` bei Titel-Records |
| kaputte Zeilen | vollständige Zeilen, die kein JSON-Objekt sind (eine halb geschriebene letzte Zeile zählt nicht) |
| Versionen | die Claude-Code-Versionen in den Transkripten, neueste zuerst, mit Anzahl Zeilen |

Je Befund steht die erste Fundstelle (`Datei:Zeile`) dabei. Inhalte werden nicht ausgegeben.

| Option | Bedeutung |
|---|---|
| `--harness NAME` | zu prüfende Harness, Default `claude` |
| `--json` | Liste von Berichten (`unknown_record_types`, `unknown_block_types`, `missing_fields`, `malformed`, `versions`) |

Exit-Code 0: nichts Unbekanntes. Exit-Code 2: Befunde. Siehe [Nach einem Claude-Code-Update](hooks.md#nach-einem-claude-code-update).

## `hooks print` { #hooks-print }

Gibt die Hook-Konfiguration für `~/.claude/settings.json` als JSON aus.

```sh
total-recall hooks print --harness claude
total-recall hooks print --binary "$(command -v total-recall)"   # mit absolutem Pfad
```

| Option | Bedeutung |
|---|---|
| `--harness NAME` | derzeit nur `claude` (Default) |
| `--binary BEFEHL` | Befehl in den Hooks, Default `total-recall` |

Den Block und was jeder Hook tut: [Hooks einrichten](hooks.md).

## `llm --skill` { #llm }

Gibt die eingebettete Anleitung für Agenten aus (`SKILL.md`). Siehe [Für Agenten](agenten.md).

```sh
total-recall llm --skill
```

## `config show` / `config init` { #config }

```sh
total-recall config show          # wirksame Konfiguration mit Herkunft je Wert
total-recall config show --json
total-recall config init          # kommentierte Vorlage schreiben (überschreibt nie)
```

```text
config file: /Users/me/.config/total-recall/config.toml (default, loaded)

KEY              VALUE                                                          ORIGIN
db               /Users/me/Library/Application Support/total-recall/archive.db  default
timezone         Europe/Berlin                                                  default
backup.dir       /Users/me/Documents/total-recall                               file
…
log (effective)  /Users/me/Library/Application Support/total-recall/total-recall.log  default
```

`ORIGIN` ist `default`, `file`, `env` oder `flag`. `config init` schreibt an den Ort, den `--config` bzw. `TOTAL_RECALL_CONFIG` nennt, sonst nach `~/.config/total-recall/config.toml`. Siehe [Konfiguration](konfiguration.md).

## `version` { #version }

Gibt die Version aus.

```sh
total-recall version
```

## `completion` { #completion }

Erzeugt Shell-Vervollständigung (bash, zsh, fish, powershell). Bei der Installation per Homebrew ist das schon erledigt.

```sh
total-recall completion zsh > "${fpath[1]}/_total-recall"
```

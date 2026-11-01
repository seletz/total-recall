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
| `--detach` | als losgelöster Hintergrundprozess weiterlaufen und sofort zurückkehren. Ausgabe und Fehler gehen ins Log |

Ausgabe:

```text
scanned 291 files: 4 changed, 287 unchanged, 0 locked; 22 new events, 3 sessions touched (55ms)
```

- `changed`: Dateien mit neuen Zeilen. `unchanged`: nichts Neues. `locked`: gerade von einem anderen Lauf gelesen, übersprungen.
- Fehler je Datei stehen auf stderr; die übrigen Dateien werden trotzdem gelesen. Gab es Fehler, ist der Exit-Code 1.

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
last backup           /Users/me/Documents/total-recall/archive-2026-11-01.db  (5h12m ago)
log                   /Users/me/Library/Application Support/total-recall/total-recall.log

recent errors:
  2026-10-30T08:12:01Z ERROR hook Stop: /Users/me/.claude/projects/x/y.jsonl: permission denied
```

`schema` zeigt die Schema-Version der Datenbank und die, die das Programm kennt. Die JSON-Ausgabe enthält dieselben Werte (`schema_version`, `binary_schema_version`, `db_size_bytes`, `last_ingest`, `last_backup`, `recent_errors`, …).

## `sessions` { #sessions }

Listet Sessions, neueste zuerst. Subagent-Sessions sind ausgeblendet, außer mit `--subagents`.

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

Kennzahlen für einen Zeitraum: insgesamt, je Tag, je Repo und je Wochentag × Stunde. Gezählt werden Sessions (mit mindestens einem menschlichen oder Agenten-Event im Bereich), menschliche und Agenten-Events. Tage und Stunden in lokaler Zeit.

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

Schreibt eine konsistente Kopie der Datenbank (`VACUUM INTO`) nach `<backup.dir>/archive-YYYY-MM-DD.db`. Das Datum ist in der konfigurierten Zeitzone. Eine Kopie vom selben Tag wird ersetzt. Danach werden alte Kopien nach den Regeln in `[backup]` aufgeräumt.

```sh
total-recall backup
total-recall backup --if-older 24h            # nur, wenn das letzte Backup älter ist
total-recall backup --if-older 24h --detach   # aus einem Hook
```

```text
backup written: /Users/me/Documents/total-recall/archive-2026-11-01.db
```

| Option | Bedeutung |
|---|---|
| `--if-older DAUER` | nichts tun, wenn die neueste Kopie jünger ist (Go-Dauer: `24h`, `90m`) |
| `--detach` | im Hintergrund laufen, sofort mit 0 zurückkehren; Ausgabe und Fehler ins Log |

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

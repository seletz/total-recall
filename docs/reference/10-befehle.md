---
title: Befehle
icon: lucide/terminal
---
# Befehle

Überblick über alle Befehle. Alle Beispielausgaben sind erfunden.

## Globale Optionen

| Option | Bedeutung |
|---|---|
| `--config PFAD` | andere Config-Datei (sonst `TOTAL_RECALL_CONFIG` bzw. `~/.config/total-recall/config.toml`) |
| `--db PFAD` | andere Datenbank (sonst `TOTAL_RECALL_DB` bzw. `db` aus der Config) |
| `--tz ZONE` | Zeitzone für Anzeige und Tagesgrenzen, Default `Europe/Berlin` |
| `--backup-dir PFAD` | anderer Backup-Ordner (sonst `TOTAL_RECALL_BACKUP_DIR` bzw. `backup.dir`) |
| `--version`, `-v` | Version ausgeben und beenden, wie [`version`](#version) |

## Ausgabe der lesenden Befehle { #ausgabe }

Alle lesenden Befehle (`status`, `sessions`, `show`, `search`, `stats`, `sql`, `doctor`, `config show`) haben dieselben Ausgabeoptionen:

| Option | Bedeutung |
|---|---|
| `--format table\|json\|jsonl` | Ausgabeformat, Default `table` (Tabelle oder Text). `sql` kennt zusätzlich `csv`. |
| `--json` | Kurzform für `--format json` |
| `--no-truncate` | Tabellenzellen auch im Terminal nie kürzen |

`json` ist ein eingerücktes Dokument, `jsonl` ein kompaktes Objekt je Zeile, ungepuffert, zum Streamen großer Ergebnisse. Bei `status` und `stats` ist das eine Zeile; `show --format jsonl` schreibt nur die Events, eines je Zeile (den Session-Kopf liefert `sessions`). Ein leeres Ergebnis ist `[]` (`json`) bzw. keine Zeile (`jsonl`).

**Fehler.** Mit `--json` oder `--format json|jsonl` geht ein Fehler als ein Objekt auf stdout, der Exit-Code bleibt 1, die Meldung steht zusätzlich auf stderr. Ein Skript muss also nur stdout lesen:

```sh
$ total-recall sql --json "select nosuchcol from sessions"
{"error":{"kind":"sql","message":"SQL logic error: no such column: nosuchcol (1)"}}
```

`kind` ist stabil: `usage` (Flags, Argumente, ihre Kombinationen), `config` (Konfiguration nicht ladbar), `db` (Archiv nicht zu öffnen), `sql` (SQLite lehnt die Abfrage ab, auch bei `search --raw`), `not_found` (Session oder Event unbekannt), `ambiguous` (Präfix passt auf mehrere Sessions), `internal` (alles andere).

**Schlüssel.** Jeder dokumentierte Schlüssel ist in der JSON-Ausgabe immer vorhanden. Ein String ohne Wert ist `null`, zum Beispiel `parent_id` einer Haupt-Session oder `holiday` an einem gewöhnlichen Tag; Zahlen sind nie `null` (0), Listen nie `null` (`[]`).

**Kürzung.** `--format table` kürzt Zellen nur, wenn stdout ein Terminal ist, und dann auf die Terminalbreite: die breitesten Spalten geben zuerst ab. Eine gekürzte Zelle endet mit `…`, und auf stderr steht `N cells truncated, --no-truncate for full values`. In eine Pipe oder Datei (`| less`, `> datei`, aus einem Skript) wird nie gekürzt. Zellen mit Zeilenumbrüchen stehen in der Tabelle immer auf einer Zeile (Leerraum zu einem Leerzeichen); `csv` und JSON behalten den Text unverändert.

Zeiten in der Ausgabe sind in lokaler Zeit (`--tz`). In der Datenbank und in der JSON-Ausgabe stehen sie in UTC (`2026-10-31T22:21:58.123Z`).

Zeitangaben für `--since` und `--until` sind lokale Zeit: `2026-10-01`, `2026-10-01T14:30` oder RFC 3339. Ein reines Datum bei `--until` schließt den ganzen Tag ein: `--until 2026-10-31` heißt „bis einschließlich 31. Oktober“.

Die lesenden Befehle öffnen die Datenbank nur lesend. Ist ihr Schema älter als das Programm, gibt es eine Warnung; ein schreibender Befehl wie `ingest` migriert sie (siehe [Design](../explanation/design.md#schema-versionierung)).

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
| `--reread` | die angegebenen Dateien bzw. mit `--all` alle Quellen von Anfang an lesen statt ab dem gespeicherten Lesestand. Events sind idempotent (nichts wird doppelt), Sessions werden zusammengeführt; so bekommen Sessions nachträglich, was ältere Versionen noch nicht gespeichert haben, etwa Fortsetzung und Account (Schema v4). Nur mit `PATH…` oder `--all` |
| `--rewrite-raw` | nur zusammen mit `--reread`: die gespeicherte Rohzeile (`raw`, `raw_truncated`) jedes bereits vorhandenen Events durch die neu gelesene und neu maskierte ersetzen. `text`, `actor` und der Volltextindex bleiben unverändert, nichts wird doppelt. Repariert Archive, in denen frühere Versionen ganze Werte in `raw` maskiert haben ([#130](https://github.com/seletz/total-recall/issues/130)). Braucht `ingest.store_raw = true`, sonst Fehler |
| `--detach` | als losgelöster Hintergrundprozess weiterlaufen und sofort zurückkehren. Ausgabe und Fehler gehen ins Log |

Ausgabe:

```text
scanned 291 files: 4 changed, 287 unchanged, 0 locked; 22 new events, 3 sessions touched (55ms)
```

- `changed`: Dateien mit neuen Zeilen. `unchanged`: nichts Neues. `locked`: gerade von einem anderen Lauf gelesen, übersprungen.
- Fehler je Datei stehen auf stderr; die übrigen Dateien werden trotzdem gelesen. Gab es Fehler, ist der Exit-Code 1.
- Mit `--regit` kommt eine Zeile dazu: `regit: 57 directories, 12 sessions updated (1.2s)`.
- Mit `--reread` zählen alle gelesenen Dateien als `changed`, obwohl `new events` 0 bleibt.
- Mit `--rewrite-raw` steht hinter `new events` zusätzlich `N raw rewritten`: so viele vorhandene Events haben eine neue Rohzeile bekommen.

**Altbestand nach einem Schema-Update.** Neue Session-Felder (seit Schema v4: `continued_in`, `account_id`, `organization_id`) stammen aus Records, die beim ersten Einlesen übersprungen wurden. Bereits gelesene Transkripte liefern sie erst nach einem einmaligen `total-recall ingest --all --reread`; das dauert so lange wie das erste Einlesen und ändert an den Events nichts.

**Rohzeilen reparieren.** Bis v1.0.2 hat die Maskierung in `raw` ganze Werte ab 32 Zeichen durch `«redacted:high_entropy»` ersetzt (Befehle, Tool-Ausgaben, Nachrichtentexte; `text` war nie betroffen). Einmalig, vorher ein [Backup](#backup):

```sh
total-recall backup
total-recall ingest --all --reread --rewrite-raw
```

Danach enthält `raw` wieder die vollständige, nur tokenweise maskierte Zeile und ist etwa doppelt so groß wie vorher. Transkripte, die es nicht mehr gibt, bleiben wie sie sind.

Mehr dazu: [Hooks einrichten](../how-to/20-hooks-einrichten.md).

## `reindex` { #reindex }

Bestimmt abgeleitete Felder gespeicherter Events neu, ohne die Transkripte zu lesen.

```sh
total-recall reindex --actors --dry-run   # zeigen, was sich ändern würde
total-recall reindex --actors             # anwenden
```

| Option | Bedeutung |
|---|---|
| `--actors` | den Actor jeder gespeicherten User-Nachricht mit den aktuellen Regeln neu bestimmen (eingebaute Regeln plus [`[harness.claude.actors]`](20-konfiguration.md#harnessclaudeactors)) und die Events aktualisieren, deren Actor sich ändert. Idempotent: ein zweiter Lauf mit denselben Regeln ändert nichts |
| `--dry-run` | nur zählen, nichts schreiben |

Ausgabe:

```text
reindex --actors: 1234 user messages, 17 changed (human -> peer: 17) (0.4s)
reindex --actors (dry run): 1234 user messages, 17 would change (human -> peer: 17) (0.4s)
```

- Die Textregeln (Präfixe, Kopfzeilen eingefügter Blöcke) laufen über den gespeicherten Text; die Flags des Records (`origin.kind`, `isMeta`, geplante Prompts) kommen aus `raw`. Fehlt `raw` (`ingest.store_raw = false` oder Zeile größer als `ingest.raw_max_bytes`) und trifft keine Textregel, bleibt der Actor stehen; die Zahl steht als `undecided` in der Ausgabe.
- Typischer Anlass: ein Koordinator hat Aufträge in Sessions eingefügt, die als `human` gezählt wurden, siehe [Stundenzettel füllen](../how-to/45-stundenzettel.md#mehrere-agenten).

## `status` { #status }

Zeigt den Zustand des Archivs.

```sh
total-recall status
total-recall status --json
total-recall status --all-errors        # alle Fehler des Logs, unabhängig vom Alter
total-recall status --errors 20         # bis zu 20 Fehlerzeilen statt 5
```

```text
database              /Users/me/Library/Application Support/total-recall/archive.db
size                  612.4 MiB
page size             4096  (total-recall maintenance rebuilds with 16384 and saves space)
schema                v2 (binary supports v2)
sessions              274
events                158203
sources               280
last ingest (claude)  2026-11-01 14:59  (2m ago)
last backup           /Users/me/Documents/total-recall/archive-2026-11-01.db.gz  (5h12m ago)
log                   /Users/me/Library/Application Support/total-recall/total-recall.log

recent errors (last 24h, since 2026-10-31 15:01): 1
  2026-11-01 08:12  (6h49m ago)  hook Stop: /Users/me/.claude/projects/x/y.jsonl: permission denied
older errors: 3 (not shown; status --all-errors lists them, log: /Users/me/Library/Application Support/total-recall/total-recall.log)
```

**Fehler.** Gezeigt werden die `ERROR`-Zeilen des Logs aus den letzten 24 Stunden, mit Zeitstempel in lokaler Zeit und Alter. Das Fenster ist fest, damit die Ausgabe nicht davon abhängt, ob seither ein Hook gelaufen ist. Ältere Fehler erscheinen nur als Anzahl; `--all-errors` listet sie alle (ohne Begrenzung, sofern `--errors` nicht gesetzt ist). Zeilen über Transkripte, die beim Hook noch nicht existierten (`no such file or directory`), zählen nie als Fehler, auch nicht aus älteren Versionen, die den Fall noch als `ERROR` protokolliert haben.

`schema` zeigt die Schema-Version der Datenbank und die, die das Programm kennt. `page size` ist die Seitengröße der Datei in Bytes; ist sie kleiner als 16384, steht daneben der Hinweis auf [`maintenance`](#maintenance), das die Datei mit größeren Seiten kompakter neu aufbaut. Die JSON-Ausgabe enthält dieselben Werte (`schema_version`, `binary_schema_version`, `db_size_bytes`, `page_size`, `last_ingest`, `last_backup`, …). Die Fehler stehen in `recent_errors` als Objekte mit `at` (UTC, `null` ohne Zeitstempel) und `message`, dazu `errors_window` (`"24h"`, mit `--all-errors` `null`), `errors_since` (Beginn des Fensters, UTC), `recent_errors_total` (Anzahl im Fenster, auch über die Begrenzung hinaus) und `older_errors` (Anzahl davor).

## `sessions` { #sessions }

Listet Sessions, neueste zuerst. Subagent-Sessions sind ausgeblendet, außer mit `--subagents`. Ebenso Sessions ohne Events und ohne Startzeit (etwa wenn von einer Session nur ein Titel-Record gesehen wurde), außer mit `--all-sessions`.

```sh
total-recall sessions --since 2026-10-01 --until 2026-10-31
total-recall sessions --repo seletz/total-recall
total-recall sessions --name release --json
total-recall sessions --account work                  # Name aus [accounts], ID oder ID-Präfix
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
| `--account A` | Sessions dieses Accounts: ein Name aus [`[accounts]`](20-konfiguration.md#accounts), die Account-ID oder ein Präfix davon |
| `--limit N` | höchstens N Zeilen (0 = alle, Default) |
| `--subagents` | Subagent-Sessions mit anzeigen |
| `--all-sessions` | auch leere Sessions (keine Events, keine Startzeit) anzeigen |

Spalten: `ID` ist ein Präfix der nativen Session-ID, das `show` versteht. `NAME` ist der eigene Titel (`/rename`), sonst der von Claude Code erzeugte Titel, sonst der Agent-Name. `REPO` ist `owner/repo` der Remote-URL, sonst der Ordnername. `HUMAN`/`AGENT` zählen Events nach Actor. `DUR` ist Ende minus Start, inklusive Pausen.

Die JSON-Ausgabe hat zusätzlich je Session `continued_in` (die Session, in der diese fortgesetzt wurde, volle ID; sie muss nicht archiviert sein), `continued_from` (die Session, die in dieser fortgesetzt wurde), `account_id` und `organization_id` (Account und Organisation, auf denen die Session lief; Claude Code schreibt sie seit 2.1.317) sowie `account` (der Name aus `[accounts]`). Alle fünf sind `null`, wenn nichts bekannt ist. Eine fortgesetzte Session ist eine neue Session mit eigener ID; der Verlauf einer Arbeit über mehrere Sessions folgt der Kette `continued_in`.

## `show` { #show }

Zeigt den Verlauf einer Session: deine Anfragen und die Antworten des Agenten.

```sh
total-recall show 9a1b2c3d                      # Präfix aus `sessions`
total-recall show claude:9a1b2c3d-…             # volle ID
total-recall show 9a1b2c3d --all                # inklusive Tool-Aufrufe, Ergebnisse, Automatik
total-recall show 9a1b2c3d --json
total-recall show 9a1b2c3d --since 2026-10-31T20:00 --until 2026-10-31T21:00   # Zeitfenster, lokale Zeit
total-recall show 9a1b2c3d --limit 50 --offset 100                             # blättern
total-recall show 9a1b2c3d --from-event claude:7c0d…#0 --limit 20              # ab einem Treffer aus `search`
```

```text
Release-Pipeline aufsetzen
claude:9a1b2c3d-0000-4000-8000-000000000000
time            2026-10-31 19:38 – 2026-10-31 22:37  (2h59m)
repo            https://github.com/seletz/total-recall
branch          develop
cwd             /Users/me/develop/total-recall
continued from  claude:4e5f6a7b-0000-4000-8000-000000000000
account         work (aaaaaaaa-0000-4000-8000-000000000001)
organization    cccccccc-0000-4000-8000-00000000000c

── 2026-10-31 19:38:02  human
Bitte richte goreleaser mit einem Homebrew-Cask ein.

── 2026-10-31 19:41:15  agent
Erledigt: .goreleaser.yml angelegt, Cask im Tap konfiguriert.
```

| Option | Bedeutung |
|---|---|
| `--all` | auch Tool-Aufrufe und -Ergebnisse, Automatik, System- und Meta-Events |
| `--since T` / `--until T` | nur Events in diesem Zeitfenster; Formate und Semantik wie bei `search`: `YYYY-MM-DD`, `YYYY-MM-DDTHH:MM`, `YYYY-MM-DD HH:MM` oder RFC 3339, lokale Zeit gemäß `timezone`; ein reines Datum bei `--until` schließt den ganzen Tag ein |
| `--limit N` | höchstens N Events (0 = alle, Default) |
| `--offset N` | die ersten N Events des Fensters überspringen (Blättern mit `--limit`) |
| `--from-event ID` | ab diesem Event (einschließlich), z. B. `event_id` eines Treffers aus `search` |

Die Session kann über die volle ID (`claude:<uuid>`), die native ID oder ein eindeutiges Präfix davon angegeben werden. Ohne `--all` zeigt `show` nur Nachrichten von Mensch, Agent und anderen Agenten (`peer`).

Der Kopf nennt, was bekannt ist: bei Subagent-Sessions `parent`, bei Fortsetzungen `continued from` (die Session, die hier fortgesetzt wurde) und `continued in` (die Session, in der es weiterging), dazu `account` und `organization` mit dem Namen aus [`[accounts]`](20-konfiguration.md#accounts), sofern einer konfiguriert ist. Die JSON-Ausgabe hat dieselben Felder wie `sessions --json`.

Die Filter wirken in dieser Reihenfolge: erst `--all`, dann das Zeitfenster und `--from-event`, dann `--offset` und `--limit`. Ohne Grenzen bleibt die Ausgabe vollständig; bei mehr als 2000 Events steht ein Hinweis auf stderr, wie sich die Ausgabe begrenzen lässt. Mit `--from-event` und einem Event, das ohne `--all` nicht sichtbar ist (etwa ein Tool-Ergebnis), beginnt die Ausgabe bei der nächsten Nachricht. Ein leeres Fenster liefert mit `--json` eine leere Liste `"events": []`.

## `search` { #search }

Volltextsuche (SQLite FTS5) mit Textausschnitten, beste Treffer zuerst.

```sh
total-recall search goreleaser
total-recall search backup retention --since 2026-10-01      # beide Wörter müssen vorkommen
total-recall search --raw 'cask AND (homebrew OR tap)'       # FTS5-Syntax
total-recall search redaction --actor human --repo seletz/total-recall --json
total-recall search backup --session 9a1b2c3d                # nur in dieser Session
total-recall search goreleaser --by-session                  # eine Zeile je Session, Zeitleiste
```

```text
2026-10-31 19:38  Release-Pipeline aufsetzen  seletz/total-recall  claude:9a1b2c3d-…  [human]
    Bitte richte [goreleaser] mit einem Homebrew-Cask ein.
```

Mit `--by-session` werden die Treffer je Session zusammengefasst, Subagenten-Sessions eingeschlossen: erster und letzter Treffer (lokale Zeit; in JSON UTC als `first_hit` und `last_hit`), Anzahl der Treffer, ID, Name, Repo und der Textausschnitt des ersten Treffers. Sortiert nach dem ersten Treffer, älteste Session zuerst, also als Zeitleiste; `--order hits` sortiert nach Trefferzahl, meiste zuerst. `--limit` zählt dann Sessions statt Treffer und behält in der Zeitleiste die Sessions mit den jüngsten Treffern (bei `--order hits` die mit den meisten), sodass eine Suche über das ganze Archiv die neuesten Sessions zeigt. Die Filter wirken wie bei der einfachen Suche, `--actor human` etwa liefert die Sessions, in denen du selbst das Thema angesprochen hast.

```text
FIRST             LAST              HITS  ID                  NAME                         REPO                 SNIPPET
2026-10-31 19:38  2026-10-31 21:02  7     claude:9a1b2c3d-…   Release-Pipeline aufsetzen   seletz/total-recall  Bitte richte [goreleaser] mit einem Homebrew-Cask ein.
2026-11-02 09:15  2026-11-02 09:16  2     claude:4f7a0b11-…   Release v0.3.0               seletz/total-recall  …der [goreleaser]-Lauf ist durch…
```

| Option | Bedeutung |
|---|---|
| Filter wie `sessions` | `--since`/`--until` gelten hier für die Zeit des Events |
| `--actor A` | nur `human`, `agent`, `peer`, `automation` oder `tool` |
| `--session ID` | nur in dieser Session; volle ID, native ID oder ein eindeutiges Präfix wie bei `show`, ein mehrdeutiges Präfix ist ein Fehler |
| `--raw` | Anfrage unverändert an FTS5: `AND`, `OR`, `NOT`, `"genaue Phrase"`, `präfix*` |
| `--limit N` | höchstens N Treffer (Default 20, 0 = alle); mit `--by-session` die N Sessions mit den jüngsten Treffern, bei `--order hits` die mit den meisten |
| `--by-session` | eine Zeile je Session statt je Treffer: `first_hit`, `last_hit`, `hits`, `snippet` des ersten Treffers |
| `--order first_hit\|hits` | Sortierung mit `--by-session`: nach erstem Treffer, älteste zuerst (Default), oder nach Trefferzahl, meiste zuerst |

Ohne `--raw` wird jedes Wort in Anführungszeichen gesetzt. Dann müssen alle Wörter vorkommen, und Sonderzeichen sind harmlos.

## `stats` { #stats }

Kennzahlen für einen Zeitraum: insgesamt, je Tag, je Repo und je Wochentag × Stunde; mit [`--period`/`--by`](#stats-period) eine Zeitreihe, mit [`--workday`](#stats-workday) die erste und letzte Aktivität je Tag, mit [`--worktime`](#stats-worktime) die geschätzte Arbeitszeit je Tag und Kategorie. Gezählt werden Sessions (mit mindestens einem menschlichen oder Agenten-Event im Bereich), menschliche und Agenten-Events. Tage und Stunden in lokaler Zeit.

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

Mit `--period` oder `--by` liefert `stats` statt der drei Tabellen eine Zeitreihe: eine Zeile je Periode und, mit `--by`, je Projekt, Agent-Typ, Arbeitsverzeichnis oder Account. Der Zeitraum kommt wie oben aus `--month` oder `--since`/`--until`; Periodengrenzen liegen in lokaler Zeit, Wochen sind ISO-Wochen ab Montag. Das ersetzt `monthly_stats.py` im Vault.

```sh
total-recall stats --period week --by project --since 2026-08-01 --until 2026-10-31
total-recall stats --period month --by agent --top 6 --json
total-recall stats --month 2026-10 --period day --by directory
total-recall stats --period month --by account --since 2026-09-01
total-recall stats --period month --since 2026-01-01          # nur je Monat, ohne Aufschlüsselung
```

| Option | Bedeutung |
|---|---|
| `--period day\|week\|month` | Breite der Perioden. Ohne `--by` eine Zeile je Periode mit Sessions, human- und agent-Events, leere Perioden mit 0, zuletzt eine Zeile `total` für den ganzen Zeitraum |
| `--by project\|agent\|directory\|account` | zusätzlich je Periode nach Schlüssel aufschlüsseln; ohne `--period` gilt `day`. `project`: Projektname, zuerst aus den Definitionen [`[[project]]`](20-konfiguration.md#project) (die erste passende gewinnt; mehrere Repositories mit derselben Definition sind ein Schlüssel, auch für `--top`), sonst der Basisname des Git-Roots (bei gleichen Namen so viele übergeordnete Verzeichnisse wie nötig), Worktrees zählen zum Hauptrepository. Verzeichnisse ohne Repository erscheinen als Pfad, im Text mit `~` für das Home-Verzeichnis (`~/notes`), in JSON als voller Pfad (`/Users/me/notes`), damit Skripte nicht vom Home des Aufrufers abhängen. `agent`: `claude main` oder `claude subagent`. `directory`: Arbeitsverzeichnis. `account`: der Account, auf dem die Session lief, mit dem Namen aus [`[accounts]`](20-konfiguration.md#accounts), sonst die ID; Sessions ohne Account (vor Claude Code 2.1.317 bzw. vor einem `ingest --reread`) stehen unter `unknown`. Subagent-Sessions zählen bei `project`, `directory` und `account` zu ihrer Eltern-Session |
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

Mit `--workday` zeigt `stats` je Tag die **erste und letzte Aktivität**, die Spanne dazwischen und die Differenz zum Tagessoll: ein Notnagel, wenn das Ein- oder Ausstempeln vergessen wurde. Dieselbe Ansicht hat die [TUI](30-tui.md#aktivitat) als Reiter „activity“.

```sh
total-recall stats --workday                          # diese Woche
total-recall stats --workday --range last-week
total-recall stats --workday --range last-month --json
total-recall stats --workday --since 2026-10-01 --until 2026-10-15 --all-events
```

```text
2026-10-26 04:00 – 2026-11-02 03:59 (Europe/Berlin, day starts 04:00, human events only)
core hours 09:00-17:00, target 8h00m per workday, holidays Baden-Württemberg

DATE        DAY  FIRST  LAST        SPAN    DIFF    HOLIDAY
2026-10-26  Mon  08:12  18:40       10h28m  +2h28m  -
2026-10-27  Tue  09:03  01:30 (+1)  16h27m  +8h27m  -
2026-10-28  Wed  -      -           -       -       -
2026-10-29  Thu  07:55  17:10       9h15m   +1h15m  -
…
2026-11-01  Sun  21:00  21:05       5m      +0h05m  Allerheiligen

total: 5 days with activity, 41h40m · target 40h00m (5 workdays × 8h00m) · balance +1h40m · 1 holiday
```

| Option | Bedeutung |
|---|---|
| `--range today\|this-week\|last-week\|this-month\|last-month` | Zeitraum relativ zu jetzt. Wochen sind ISO-Wochen ab Montag. Nur mit `--workday`, nicht zusammen mit `--since`/`--until`/`--month` |
| `--since`, `--until`, `--month` | wie oben; ein Datum meint den ganzen Tag ab der Tagesgrenze, `--until` einschließlich. Ohne Zeitraum gilt `this-week` |
| `--all-events` | eigene Nachrichten und Agent-Events zählen wie die Aktivitätsstatistik. Standard sind nur eigene Nachrichten (`human events only`): Agent-Events laufen nachts weiter und dehnen die Spanne. Den Standard setzt `count` in [`[work]`](20-konfiguration.md#work) |
| `--human-only` | veraltet, da Standard; wird noch angenommen (mit Hinweis auf stderr) und setzt auch bei `count = "all"` auf eigene Nachrichten |

**Tagesgrenze.** Ein Tag beginnt um `day_start` aus der [Konfiguration](20-konfiguration.md#day_start), Standard 04:00 Ortszeit. Aktivität davor zählt zum Vortag und steht als `01:30 (+1)` in der Spalte; deshalb beginnt auch der Zeitraum um 04:00. `today` kurz nach Mitternacht ist noch der Vortag. Die Spanne ist die verstrichene Zeit, auch über eine Zeitumstellung hinweg. Alle Tage des Zeitraums erscheinen, Tage ohne Aktivität mit `-`.

**Soll.** Kernzeit und Tagessoll kommen aus [`[work]`](20-konfiguration.md#work), Standard 09:00–17:00 und 8 h. `DIFF` ist die Spanne minus Soll an Arbeitstagen mit Aktivität; Wochenende und Feiertage haben kein Soll, dort ist die ganze Spanne die Differenz. Die Summenzeile nennt das Soll als Tagessoll × **Arbeitstage des Zeitraums**: Montag bis Freitag ohne Feiertage, mit oder ohne Aktivität, aber nur bis heute (im laufenden Monat zählen künftige Tage noch nicht). Ein Tag ohne Aktivität an einem Arbeitstag drückt den Saldo also um ein Tagessoll (Urlaub und ein vergessener Tag sind nicht zu unterscheiden), ein Feiertag nicht. Die Kernzeit ist in der Textausgabe nur eine Angabe; wo die Aktivität liegt, zeigt die TUI.

**Feiertage.** Gesetzliche Feiertage kommen aus `bundesland` in [`[work]`](20-konfiguration.md#work), Standard Baden-Württemberg; die Kopfzeile nennt die Region, die Spalte `HOLIDAY` den Namen, die Summenzeile die Anzahl. Ein unbekanntes Bundesland fällt auf die bundesweiten Feiertage (`Deutschland`) zurück.

JSON hat je Tag die Zeiten in UTC (`first`, `last`) und als Ortszeit (`first_local`, `last_local`, mit `(+1)`), die Spanne in Sekunden, `weekend`, `holiday` (Name oder `""`), Soll und Differenz in Sekunden (`target_seconds`, `diff_seconds`, beide 0 ohne Aktivität) und die Zähler. Oben stehen `human_only`, `work_hours`, `target_seconds` und `holiday_region`, in `total` die Arbeitstage des Zeitraums (`workdays`), die Feiertage (`holidays`), das Soll und der Saldo (`balance_seconds`). Ein Tag ohne Aktivität hat leere Zeiten und Nullen.

```json
{
  "since": "2026-10-26T03:00:00.000Z",
  "until": "2026-11-02T03:00:00.000Z",
  "timezone": "Europe/Berlin",
  "day_start": "04:00",
  "human_only": true,
  "work_hours": "09:00-17:00",
  "target_seconds": 28800,
  "holiday_region": "Baden-Württemberg",
  "days": [
    {
      "day": "2026-10-27", "weekday": "Tue",
      "first": "2026-10-27T08:03:00.000Z", "last": "2026-10-28T00:30:00.000Z",
      "first_local": "09:03", "last_local": "01:30 (+1)",
      "span_seconds": 59220, "weekend": false, "holiday": "",
      "target_seconds": 28800, "diff_seconds": 30420,
      "sessions": 3, "human": 41, "agent": 0
    }
  ],
  "total": {"days": 5, "workdays": 5, "holidays": 1, "span_seconds": 150000, "target_seconds": 144000, "balance_seconds": 6000}
}
```

`--workday` lässt sich nicht mit `--period`, `--by`, `--top` oder `--worktime` kombinieren.

### Arbeitszeit je Projekt: `--worktime` { #stats-worktime }

Mit `--worktime` schätzt `stats` die **aktive Arbeitszeit je Tag und Kategorie** aus deinen eigenen Nachrichten, als Grundlage für Stundenzettel: roh und gerundet, mit den beteiligten Projekten und Sessions. Das Rezept dazu: [Stundenzettel füllen](../how-to/45-stundenzettel.md).

```sh
total-recall stats --worktime                         # diese Woche
total-recall stats --worktime --range last-week
total-recall stats --worktime --range last-month --sessions
total-recall stats --worktime --range last-week --summary
total-recall stats --worktime --since 2026-10-26 --until 2026-10-30 --json
```

```text
2026-10-26 04:00 – 2026-10-31 03:59 (Europe/Berlin, day starts 04:00, human events only)
gaps up to 20m count in full, longer ones 3m · rounded up to 15m per category and day · holidays Baden-Württemberg
a lower bound: meetings and work without an agent leave no messages

DATE        DAY  CATEGORY     RAW    ROUNDED  HOLIDAY
2026-10-26  Mon  fahrtenbuch  45m    45m      -
2026-10-26  Mon  kompass      2h32m  2h45m    -
2026-10-26  Mon  wetterwarte  26m    30m      -
2026-10-27  Tue  fahrtenbuch  55m    1h00m    -
2026-10-27  Tue  kompass      3h13m  3h15m    -
2026-10-28  Wed  kompass      2h52m  3h00m    -
2026-10-28  Wed  wetterwarte  53m    1h00m    -
2026-10-29  Thu  -            -      -        -
2026-10-30  Fri  fahrtenbuch  1h25m  1h30m    -
2026-10-30  Fri  kompass      2h50m  3h00m    -

CATEGORY     RAW     ROUNDED  DAYS
fahrtenbuch  3h05m   3h15m    3
kompass      11h29m  12h00m   4
wetterwarte  1h19m   1h30m    2
total        15h53m  16h45m   4
```

**Methode.** Grundlage sind nur deine eigenen Nachrichten (`actor = 'human'`), über alle Sessions hinweg in einer Zeitlinie. Die Lücke zwischen zwei aufeinanderfolgenden Nachrichten zählt für das Projekt der späteren: bis `max_gap` (Default 20 min) voll, darüber (Pause, Meeting, erste Nachricht des Tages) nur `min_credit` (Default 3 min). Die Zeit nach der letzten Nachricht eines Tages zählt nicht. Die Summe je Kategorie und Tag wird auf `grid` gerundet (Default 15 min aufwärts). Alles kommt aus [`[worktime]`](20-konfiguration.md#worktime).

**Untergrenze.** Meetings, Lesen, Denken und alles, was ohne Agent passiert, hinterlässt keine Nachricht und fehlt. Die Werte sind eine Untergrenze, kein Ersatz für eine Zeiterfassung; `--workday` liefert die Anwesenheit dazu.

**Kategorien.** Projekte (Git-Root, sonst Arbeitsverzeichnis, Namen wie bei `--by project`, also zuerst aus [`[[project]]`](20-konfiguration.md#project)) werden über die Regeln `[[worktime.category]]` zugeordnet, die erste passende gewinnt; die Regeln sehen den konfigurierten Projektnamen. Ein Projekt ohne Regel ist seine eigene Kategorie und heißt wie das Projekt, Verzeichnisse ohne Repository als Pfad mit `~`. Eine letzte Regel mit `match = "*"` sammelt den Rest.

| Option | Bedeutung |
|---|---|
| `--range`, `--since`, `--until`, `--month` | wie bei [`--workday`](#stats-workday): Tage ab der Tagesgrenze, `--until` einschließlich, ohne Zeitraum `this-week` |
| `--sessions` | in der Textausgabe je Tag und Kategorie die Sessions mit Titel (Vorlage für Buchungstexte) und, bei mehr als einem Projekt je Kategorie, die Projekte; Tage ohne Aktivität entfallen dort. JSON enthält beides immer |
| `--summary` | in der Textausgabe je Tag und Kategorie die Zeile `refs:` mit den Issue- und PR-Referenzen je Projekt und `text:` mit einem Vorschlag für den Buchungstext (siehe unten); kombinierbar mit `--sessions`, Tage ohne Aktivität entfallen. JSON enthält `refs` und `text` immer |

```text
2026-10-26 Mon: 3h43m raw, 4h00m rounded
  fahrtenbuch: 45m raw, 45m rounded
       45m  7d1e02c0  Fahrten zusammenfassen
  kompass: 2h32m raw, 2h45m rounded
       45m  7d1e02bb  GPX-Export
        9m  7d1e02bc  Tiles-Cache nach Neustart leer
       44m  7d1e02bd  Wegpunkte stabil sortieren
       53m  7d1e02bf  Umlaute in Ortsnamen
  wetterwarte: 26m raw, 30m rounded
       26m  7d1e02be  Frostwarnung
```

```text
2026-10-26 Mon: 3h43m raw, 4h00m rounded
  fahrtenbuch: 45m raw, 45m rounded
    refs: -
    text: fahrtenbuch: Fahrten zusammenfassen
  kompass: 2h32m raw, 2h45m rounded
    refs: kompass #31, #48, PR #52
    text: kompass: GPX-Export (#31/PR #52), Tiles-Cache nach Neustart leer (#31), Wegpunkte stabil sortieren (#48), Umlaute in Ortsnamen (#48)
  wetterwarte: 26m raw, 30m rounded
    refs: wetterwarte mara/wetterwarte#238
    text: wetterwarte: Frostwarnung (mara/wetterwarte#238)
```

**Referenzen und Buchungstext.** `--summary` liest die Issue- und PR-Referenzen aus den Session-Titeln und deinen eigenen Nachrichten (`actor = 'human'`), nie aus Antworten des Agenten oder Werkzeugausgaben. Formen: `#31`, `PR #32` (auch `pull request #32`, `MR #32`), `owner/repo#123`, `PR owner/repo#124` sowie Issue-, Pull- und Merge-Request-URLs (`https://…/owner/repo/issues/123`, `…/pull/124`, `…/-/merge_requests/8`), die zur Kurzform werden. Nicht gezählt: ein `#` hinter einem Buchstaben, einer Ziffer oder `&` (`C#1`, `&#39;`), eine Nummer mit Buchstaben dahinter (`#1a2b3c`), ein Repository hinter `/` oder `.` (ein Pfad). `refs` ist je Zelle, Projekt und Session dedupliziert (eine Nummer, die einmal als PR steht, ist ein PR) und nach Repository und Nummer sortiert, das eigene Repository zuerst. `text` ist `<projekt>: <session>, <session>; <projekt>: …`, Projekte nach Name, Sessions in Reihenfolge der ersten Nachricht: der Titel, dahinter in Klammern die Referenzen der Nachrichten, die der Titel nicht nennt (`Konfig (#21)`, `Neue Lint-Regeln (#40) (PR #52)`); ohne Titel die Referenzen (`#19/#26`), ohne beides die erste Zeile der ersten Nachricht des Tages (höchstens 60 Zeichen), sonst `(untitled)`; gleiche Einträge eines Projekts nur einmal. Der Text wird auf `summary_max_chars` Zeichen gekürzt und endet dann mit `…` (Default 200, `0` ohne Grenze); das Rezept: [Stundenzettel füllen](../how-to/45-stundenzettel.md#referenzen-und-ein-vorschlag-fur-den-text).

**Tagesgrenze und Feiertage.** Wie bei `--workday`: ein Tag beginnt um `day_start`, Nachrichten davor zählen zum Vortag (die Lücke läuft über Mitternacht weiter). Alle Tage des Zeitraums erscheinen, Feiertage aus `bundesland` in `[work]` mit Namen; ein Feiertag oder Wochenende ändert an der Zählung nichts.

JSON hat oben die wirksamen Einstellungen (`max_gap_seconds`, `min_credit_seconds`, `grid_seconds`, `rounding`, `rounding_scope`, `summary_max_chars`, `holiday_region`), je Tag `raw_seconds`, `rounded_seconds`, `weekend`, `holiday` und die Liste `categories` mit je Kategorie den Projekten (`project`, `key`, `remote`, roh und einzeln gerundet, `refs`), Sessions (`id`, `title`, `raw_seconds`, `refs`, in Reihenfolge der ersten Nachricht), den Referenzen der Zelle (`refs`, Liste von Strings, `[]` ohne) und dem Buchungstext (`text`), dann `categories` mit den Summen je Kategorie über den Zeitraum und `total`. Gerundete Summen sind Summen der gerundeten Tageszellen. Ein Tag ohne Aktivität hat eine leere Liste und Nullen.

```json
{
  "since": "2026-10-26T03:00:00.000Z",
  "until": "2026-10-27T03:00:00.000Z",
  "timezone": "Europe/Berlin",
  "day_start": "04:00",
  "max_gap_seconds": 1200, "min_credit_seconds": 180,
  "grid_seconds": 900, "rounding": "up", "rounding_scope": "category",
  "summary_max_chars": 200,
  "holiday_region": "Baden-Württemberg",
  "days": [
    {
      "day": "2026-10-26", "weekday": "Mon", "weekend": false, "holiday": null,
      "raw_seconds": 13402, "rounded_seconds": 14400,
      "categories": [
        {
          "category": "kompass", "raw_seconds": 9142, "rounded_seconds": 9900,
          "projects": [{"project": "kompass", "key": "/home/mara/src/kompass", "remote": "https://git.invalid/mara/kompass", "raw_seconds": 9142, "rounded_seconds": 9900, "refs": ["#31", "#48", "PR #52"]}],
          "sessions": [{"id": "claude:7d1e02bb-02bb-4000-8000-0000000002bb", "title": "GPX-Export", "raw_seconds": 2712, "refs": ["#31", "PR #52"]}],
          "refs": ["#31", "#48", "PR #52"],
          "text": "kompass: GPX-Export (#31/PR #52), Tiles-Cache nach Neustart leer (#31), Wegpunkte stabil sortieren (#48), Umlaute in Ortsnamen (#48)"
        }
      ]
    }
  ],
  "categories": [{"category": "kompass", "raw_seconds": 9142, "rounded_seconds": 9900, "days": 1}],
  "total": {"raw_seconds": 13402, "rounded_seconds": 14400, "days": 1}
}
```

`--worktime` lässt sich nicht mit `--workday`, `--all-events`, `--period`, `--by` oder `--top` kombinieren; `--sessions` und `--summary` brauchen `--worktime`. Ein Fehler in `[worktime]` bricht nur diesen Befehl ab (`kind` `config`).

## `tui` { #tui }

Öffnet das Archiv in einer Terminal-Oberfläche: Volltextsuche mit Vorschau beim Tippen, Detailansicht je Session, Aktivität als Chart, Tabelle und Heatmap, erste und letzte Aktivität je Tag. Nur lesend. Bedienung, Tasten und Grafikmodi: [TUI](30-tui.md).

```sh
total-recall tui                              # Suche
total-recall tui stats                        # Statistik
total-recall tui activity                     # erste und letzte Aktivität je Tag
total-recall tui --query "goreleaser cask"    # Suche mit vorbelegtem Text
total-recall tui stats --graphics text        # Chart als Text, keine Terminal-Abfrage
```

| Argument / Option | Bedeutung |
|---|---|
| `search` \| `stats` \| `activity` | Ansicht beim Start, Default `tui.view` aus der Config, sonst `search` |
| `--query TEXT` | Suchtext vorbelegen; die Suche läuft sofort |
| `--graphics auto\|text\|kitty` | Darstellung des Charts in der Statistik. `auto` sondiert das Terminal und zeigt ein Bild nur in Ghostty, kitty und herdr, sonst Text. `text` zeigt immer Blockzeichen und schickt keine Abfrage ans Terminal. `kitty` erzwingt das Bild ohne Sondierung. Default `tui.graphics` aus der Config, sonst `auto` |
| `--no-mouse` | Maus dem Terminal überlassen (Textauswahl durch Ziehen). Default `tui.mouse` aus der Config; `--no-mouse=false` schaltet die Maus für einen Aufruf ein |
| `--reload DAUER` | Intervall, in dem die TUI das Archiv auf Änderungen prüft und die sichtbare Ansicht neu lädt; `0` schaltet ab, mindestens `1s`. Default `tui.reload` aus der Config, sonst `5s`; siehe [Neuladen](30-tui.md#neuladen) |

Die Defaults der Ansicht, des Grafikmodus, der Maus, des Neuladens und des Startzustands von Statistik und Aktivität stehen im Abschnitt [`[tui]`](20-konfiguration.md#tui) der Konfiguration, die Tastenbelegung in [`[keys]`](20-konfiguration.md#keys); Flags und Argument gehen vor. Ein unbekannter Modus oder eine unbekannte Ansicht ist ein Fehler. Beenden mit `q` oder ++ctrl+c++. Kein `--json`. Entwurf, Messwerte und Entscheidung zur Übernahme: [Spike: TUI](../explanation/spike-tui.md).

## `sql` { #sql }

Führt eine SQL-Abfrage aus, nur lesend. Die Datenbank wird mit `mode=ro` und `PRAGMA query_only` geöffnet. Ändern ist nicht möglich.

```sh
total-recall sql "SELECT count(*) FROM sessions"
total-recall sql --json "SELECT id, display_name FROM sessions LIMIT 5"
total-recall sql --format csv "SELECT * FROM daily_activity" > aktivitaet.csv
```

| Option | Bedeutung |
|---|---|
| `--format table\|json\|jsonl\|csv` | Ausgabeformat, Default `table`; siehe [Ausgabe](#ausgabe) |
| `--json` | Kurzform für `--format json` (Liste von Objekten, Spalten in Abfrage-Reihenfolge, SQL-`NULL` als `null`) |
| `--no-truncate` | Zellen auch im Terminal nie kürzen |

`csv` und JSON geben Werte vollständig aus. Die Tabelle zieht mehrzeilige Werte auf eine Zeile und kürzt nur im Terminal.

Schema: [Schema](40-schema.md); Beispiele: [Archiv auswerten](../how-to/30-auswerten.md) und `total-recall llm --skill`.

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

## `maintenance` { #maintenance }

Baut die Datenbankdatei kompakt mit einer neuen Seitengröße neu auf und tauscht sie atomar aus. Die Zeilen des Archivs sind meist 1 bis 4 KiB groß; bei SQLites Standard von 4 KiB je Seite bleiben dadurch viele Seitenreste ungenutzt, mit 16 KiB je Seite ist die Datei um rund ein Siebtel kleiner, ohne dass sich am Schema etwas ändert. Wann und wie: [Archiv warten](../how-to/60-wartung.md).

```sh
total-recall maintenance --dry-run          # nur messen: wie groß würde die Datei?
total-recall maintenance                    # Backup, Neuaufbau mit 16 KiB-Seiten, Austausch
total-recall maintenance --page-size 8192   # andere Seitengröße
total-recall maintenance --json
```

```text
backup written: /Users/me/Documents/total-recall/archive-2026-11-04.db.gz
rebuilding with page size 16384 into /Users/me/Library/Application Support/total-recall/archive.db.maint-2199473120
archive replaced: /Users/me/Library/Application Support/total-recall/archive.db
                BEFORE     AFTER
size            858.3 MiB  738.1 MiB  (-14.0 %)
page size       4096       16384
pages           219724     47238
freelist pages  131        0
sessions        465        465
events          220295     220295
sources         471        471
```

| Option | Bedeutung |
|---|---|
| `--page-size N` | Seitengröße der neuen Datei, Zweierpotenz von 512 bis 65536 (Default 16384) |
| `--dry-run` | nur messen: die Kopie wird gebaut, geprüft und wieder gelöscht; kein Backup, kein Lock, das Archiv bleibt unverändert |
| `--wait DAUER` | so lange auf laufende Hooks warten, die das Archiv noch offen haben (Default `60s`) |
| `--format`, `--json` | wie bei den [lesenden Befehlen](#ausgabe); JSON hat `db`, `dry_run`, `swapped`, `page_size`, `backup` (`null` ohne Backup), `before` und `after` (je `size_bytes`, `page_size`, `page_count`, `freelist_pages`, `sessions`, `events`, `sources`), `saved_bytes`, `duration_ms` |

Ablauf, jeder Schritt bricht bei einem Fehler ab und lässt das Archiv unverändert:

1. Prüfen, ob neben der Datenbank etwa die doppelte Dateigröße frei ist (Backup-Kopie und neue Kopie) und im Backup-Ordner die halbe; sonst Abbruch mit den Zahlen.
2. Backup wie [`backup`](#backup) (gleicher Ordner, gleiche Aufbewahrung). Wie `backup` migriert der Befehl ein älteres Schema vorher.
3. Exklusives Lock gegen Hooks (`locks/maintenance.lock` neben der Datenbank). Hält ein `ingest` oder `backup` das Archiv noch offen, wartet der Befehl bis `--wait` und bricht dann mit `writers active` ab; schon beim Warten kündigt er sich an (`locks/maintenance.pending`), damit kein neuer Schreiber mehr startet. Hooks, die währenddessen starten, überspringen ihren Lauf (eine `INFO`-Zeile im Log, kein Fehler) und lesen beim nächsten Hook derselben Session oder beim nächsten `SessionStart` ab dem gespeicherten Offset weiter; es geht nichts verloren.
4. WAL-Checkpoint. Hält ein lesender Prozess gerade eine Lesetransaktion offen (eine laufende Abfrage von `sql`, `status` oder der TUI), bricht der Befehl mit `readers active` ab: den Prozess schließen oder kurz warten und noch einmal starten. Eine TUI im Leerlauf hält keine offen; sie bemerkt die ausgetauschte Datei bei der nächsten Prüfung und öffnet sie neu, siehe [Neuladen](30-tui.md#neuladen).
5. `PRAGMA page_size` und `VACUUM INTO` in eine Temporärdatei `archive.db.maint-*` neben der Datenbank.
6. `PRAGMA integrity_check` auf der Kopie und Vergleich der Zeilenzahlen von `sessions`, `events`, `sources` mit dem Original.
7. Austausch: `-wal`/`-shm` des Originals entfernen, Kopie per `rename` an die Stelle der Datenbank. Danach öffnet der Befehl die neue Datei einmal (WAL-Modus wieder an) und zeigt die Werte nachher.

Ein Abbruch, auch mit `Ctrl-C`, löscht die Temporärdatei; Reste einer abgestürzten Wartung räumt der nächste Lauf weg. Hat die Wartung die Datei einmal ausgetauscht, ist sie fertig: Das Backup aus Schritt 2 ist nur eine Absicherung, [`restore`](#restore) spielt es bei Bedarf zurück.

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
| `--json` | Liste von Berichten (`unknown_record_types`, `unknown_block_types`, `missing_fields`, `malformed`, `versions`); Konfigurationsprobleme gehen dann auf stderr |

Exit-Code 0: nichts Unbekanntes. Exit-Code 2: Befunde. Siehe [Nach einem Claude-Code-Update](../how-to/20-hooks-einrichten.md#nach-einem-claude-code-update).

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

Den Block und was jeder Hook tut: [Hooks einrichten](../how-to/20-hooks-einrichten.md).

## `llm --skill` { #llm }

Gibt die eingebettete Anleitung für Agenten aus (`SKILL.md`). Siehe [Agenten anbinden](../how-to/50-agenten-anbinden.md).

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

`ORIGIN` ist `default`, `file`, `env` oder `flag`. `config init` schreibt an den Ort, den `--config` bzw. `TOTAL_RECALL_CONFIG` nennt, sonst nach `~/.config/total-recall/config.toml`. Siehe [Konfiguration](20-konfiguration.md).

## `version` { #version }

Gibt die Version aus, eine Zeile, sonst nichts. `total-recall --version` (kurz `-v`) tut dasselbe.

```sh
$ total-recall version
1.0.0
$ total-recall --version
1.0.0
```

Ein Release-Build zeigt die Version ohne `v`. Ein Build aus dem Quellcode zeigt die Ausgabe von `git describe` (etwa `v1.0.0-3-gabc1234`) oder `dev`, wenn beim Bauen keine Version gesetzt wurde.

## `completion` { #completion }

Erzeugt Shell-Vervollständigung (bash, zsh, fish, powershell). Bei der Installation per Homebrew ist das schon erledigt.

```sh
total-recall completion zsh > "${fpath[1]}/_total-recall"
```

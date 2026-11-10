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

Die lesenden Befehle öffnen die Datenbank nur lesend. Ist ihr Schema älter als das Programm, gibt es eine Warnung auf stderr; die Befehle laufen, soweit sie keine neuen Spalten brauchen, sonst nennt der Fehler den Weg: `total-recall backup`, dann [`total-recall migrate`](#migrate). Kein Befehl migriert von selbst (siehe [Design](../explanation/design.md#schema-versionierung)).

## `ingest` { #ingest }

Liest Transkripte in die Datenbank ein. Gelesen wird nur, was seit dem letzten Lauf dazugekommen ist, und nur vollständige Zeilen.

```sh
total-recall ingest ~/.claude/projects/-Users-me-repo/abc.jsonl   # einzelne Datei
total-recall ingest --all                                         # alle Quellen nachlesen
total-recall ingest --hook --detach < hook.json                   # aus einem Hook
```

| Option | Bedeutung |
|---|---|
| `--all` | alle Quellen aller aktiven Harnesses, dazu ihr [Projekt-Gedächtnis](#memory) (`ingest.store_memory`) |
| `--hook` | Hook-JSON von stdin lesen (`transcript_path`, `agent_transcript_path`, `session_id`, `cwd`, `hook_event_name`) und diese Datei(en) einlesen, danach das [Projekt-Gedächtnis](#memory). Bei `SessionStart` zusätzlich alles wie `--all`. Exit-Code immer 0, Fehler ins Log, nichts auf stdout |
| `--regit` | Git-Root und Remote-URL für das Arbeitsverzeichnis jeder Session neu bestimmen (am Cache vorbei) und die Sessions aktualisieren. Für veraltete Git-Infos, etwa nach einem umbenannten Remote. Ein Verzeichnis, das sich nicht mehr auflösen lässt (gelöschter Worktree), behält seinen gespeicherten Wert. Allein oder zusammen mit `--all`; braucht `git.resolve = true` |
| `--reread` | die angegebenen Dateien bzw. mit `--all` alle Quellen von Anfang an lesen statt ab dem gespeicherten Lesestand. Events sind idempotent (nichts wird doppelt), Sessions werden zusammengeführt; so bekommen Sessions nachträglich, was ältere Versionen noch nicht gespeichert haben, etwa Fortsetzung und Account (Schema v4). Nur mit `PATH…` oder `--all` |
| `--rewrite-raw` | nur zusammen mit `--reread`: die gespeicherte Rohzeile (`raw`, `raw_truncated`) jedes bereits vorhandenen Events durch die neu gelesene, neu maskierte [vollständige Zeile](40-schema.md#raw) ersetzen und ihre Bilder und Dokumente als [Blobs](40-schema.md#blobs) nachtragen; auch Events, die bisher keine Rohzeile hatten (altes Limit), bekommen sie. `text`, `actor` und der Volltextindex bleiben unverändert, nichts wird doppelt. Bringt Archive aus der Zeit vor v1.3.0 (Auszüge, keine Blobs) auf den heutigen Stand und repariert solche, in denen frühere Versionen ganze Werte in `raw` maskiert ([#130](https://github.com/seletz/total-recall/issues/130)) oder Geheimnisse in Arrays nicht erkannt haben ([#162](https://github.com/seletz/total-recall/issues/162)). Braucht `ingest.store_raw = true`, sonst Fehler |
| `--detach` | als losgelöster Hintergrundprozess weiterlaufen und sofort zurückkehren. Ausgabe und Fehler gehen ins Log |

Ausgabe:

```text
scanned 291 files: 4 changed, 287 unchanged, 0 locked; 22 new events, 9 meta records, 3 sessions touched (55ms)
```

- `changed`: Dateien mit neuen Zeilen. `unchanged`: nichts Neues. `locked`: gerade von einem anderen Lauf gelesen, übersprungen.
- Fehler je Datei stehen auf stderr; die übrigen Dateien werden trotzdem gelesen. Gab es Fehler, ist der Exit-Code 1.
- Mit `--regit` kommt eine Zeile dazu: `regit: 57 directories, 12 sessions updated (1.2s)`.
- `meta records`: neu gespeicherte [Meta-Records](40-schema.md) (Anhänge, Modus, Pull Request, Datei-Historie, …); gleicher Inhalt je Session zählt nur beim ersten Mal.
- Mit `--reread` zählen alle gelesenen Dateien als `changed`, obwohl `new events` 0 bleibt.
- Mit `--rewrite-raw` steht hinter `meta records` zusätzlich `N events and M meta records raw rewritten`: so viele vorhandene Events und Meta-Records haben eine neue Rohzeile bekommen. Gezählt werden Zeilen der Tabellen, jede einmal je Lauf, auch wenn ihr Record im Transkript mehrfach vorkommt; was derselbe Lauf neu angelegt hat, steht nur bei `new events` oder `meta records`. Dahinter steht gegebenenfalls `(N meta records kept: …)`: Meta-Records, deren gespeicherte Zeile auf mehr Blobs verweist als die neue, etwa weil Claude Code die Sicherungskopien einer Datei inzwischen gelöscht hat; sie bleiben, wie sie sind.
- Wurden Blobs gespeichert (Bilder, Dokumente, System-Prompts, Sicherungskopien der Datei-Historie), folgt `N attachments (X MiB)`: so viele neue Blobs, mit ihrer Größe, wie geliefert. Kopien, die ein Record nennt, die aber nicht mehr auf der Platte liegen, stehen je Transkript als `INFO`-Zeile im Log (`backup files no longer on disk`).
- Hat sich das [Projekt-Gedächtnis](#memory) geändert, folgt `memory: N new versions, M deleted`: so viele neue Fassungen von Gedächtnis-Dateien und so viele gelöschte. Gelesen werden nur Dateien, deren Größe oder Änderungszeit sich geändert hat; ein Lauf ohne Änderung kostet ein paar Millisekunden.

**Altbestand nach einem Schema-Update.** Neue Session-Felder (seit Schema v4: `continued_in`, `account_id`, `organization_id`) stammen aus Records, die beim ersten Einlesen übersprungen wurden. Bereits gelesene Transkripte liefern sie erst nach einem einmaligen `total-recall ingest --all --reread`; das dauert so lange wie das erste Einlesen und ändert an den Events nichts.

**Rohzeilen vervollständigen.** Bis v1.2 war `raw` ein Auszug der Zeile, Zeilen über 16 KiB fehlten ganz, und Bilder wurden nicht gespeichert; bis v1.0.2 hat außerdem die Maskierung in `raw` ganze Werte ab 32 Zeichen durch `«redacted:high_entropy»` ersetzt (Befehle, Tool-Ausgaben, Nachrichtentexte; `text` war nie betroffen). Ein Lauf bringt alles auf den Stand von v1.3.0, einmalig, nach dem [Update](../how-to/10-installieren.md#aktualisieren) und vorher ein [Backup](#backup):

```sh
total-recall backup
total-recall ingest --all --reread --rewrite-raw
total-recall maintenance
```

Danach enthält `raw` die ganze, nur tokenweise maskierte Zeile, Bilder und Dokumente liegen als Blobs im Archiv, und für jedes vom Nutzer eingefügte Bild gibt es ein Event mit Platzhalter (`[image image/png 34.2 KiB]`), das ältere Versionen übersprungen hatten. Der Lauf dauert etwa so lange wie das erste Einlesen, lässt sich mit `Ctrl-C` abbrechen und später wiederholen (jede Charge ist eine Transaktion, ein zweiter Lauf ändert nichts mehr). Transkripte, die es nicht mehr gibt, bleiben wie sie sind: **Nachtragen geht nur, solange Claude Code die Transkripte noch hat** (`cleanupPeriodDays`), deshalb bald nach dem Update. `maintenance` danach gibt den Platz der ersetzten Zeilen frei.

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

`--actors` setzt außerdem [`input_kind`](40-schema.md#input_kind) der menschlichen Nachrichten neu (aus Text und `raw`) und lässt ihn bei Nachrichten weg, die nicht mehr `human` sind; die Zahl steht als `input kinds set` in der Ausgabe, wenn sie nicht 0 ist.

`--actors` erkennt auch Eingaben, die ein Koordinator mit einem Befehl in die Session eines Workers getippt hat (`herdr agent prompt`, `tmux send-keys`): Sie werden `automation`, wenn eine andere Session sie kurz vorher als Argument eines Tool-Aufrufs verschickt hat ([`prompt_window`, `prompt_commands`](20-konfiguration.md#harnessclaudeactors)). Dazu gehört die erste Eingabe eines Workers, den ein Koordinator gestartet und dessen Auftrag er aus einer Datei oder Variablen geschickt hat ([`spawn_commands`](20-konfiguration.md#harnessclaudeactors)). Die Zahl steht in der Ausgabe als `of them were typed in by another session`; sie ist ein Teil der geänderten Nachrichten.

Ausgabe:

```text
reindex --actors: 1234 user messages, 17 changed (human -> peer: 14, human -> automation: 3), 3 of them were typed in by another session (0.4s)
reindex --actors (dry run): 1234 user messages, 17 would change (human -> peer: 14, human -> automation: 3), 3 of them would be typed in by another session (0.4s)
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
schema                v5 (binary supports v5)
sessions              274
events                158203
sources               280
raw                   61.4 MiB in 152012 events, compressed 3.3x (estimated from 2000 rows); 6191 events without raw: line too long
blobs                 4935 blobs, 425.0 MiB (191.4 MiB stored): 1234 attachments 120.1 MiB, 517 JSON values 13.0 MiB, 3184 files 58.3 MiB
meta records          38.1 MiB in 79412 records
memory                905 files in 35 projects (3 deleted), 948 versions, 1.9 MiB (1.1 MiB stored)
last ingest (claude)  2026-11-01 14:59  (2m ago)
last backup           /Users/me/Documents/total-recall/archive-2026-11-01.db.zst  (5h12m ago)
log                   /Users/me/Library/Application Support/total-recall/total-recall.log

recent errors (last 24h, since 2026-10-31 15:01): 1
  2026-11-01 08:12  (6h49m ago)  hook Stop: /Users/me/.claude/projects/x/y.jsonl: permission denied
older errors: 3 (not shown; status --all-errors lists them, log: /Users/me/Library/Application Support/total-recall/total-recall.log)
```

**Letztes Backup.** `last backup` hat drei Zustände. Mit Backup steht der Pfad mit Alter da (`ok`). Ist das Verzeichnis lesbar und leer oder fehlt es, steht `none in <Verzeichnis>` (`none`). Kann `status` das Verzeichnis nicht lesen, etwa weil es unter `~/Documents` liegt und der aufrufenden App die macOS-Freigabe fehlt (`operation not permitted`), ist der Zustand `unknown`, und das sagt **nichts** über die Backups: Die Zeile nennt den Grund, darunter steht ein Hinweis (`hint:`). Die Abhilfe: der App (Terminal) unter Datenschutz & Sicherheit → Dateien und Ordner den Zugriff auf „Dokumente“ geben und **danach das Terminal bzw. die App neu starten**, denn laufende Prozesse behalten die alten Rechte; oder `[backup] dir` außerhalb von `~/Documents` legen. Die Zeit des letzten Backups kennt `status` trotzdem: Jedes Backup schreibt neben die Datenbank die kleine Statusdatei `<db>.backup.json` (Zeit, Datei, Größe), und `status` liest sie, wenn das Verzeichnis nicht lesbar ist (auch ein übersprungener Lauf von `backup --if-older` legt sie an, wenn sie fehlt). Ein lesbares Verzeichnis gilt immer mehr als die Statusdatei. Im JSON stehen dafür `last_backup_state` (`ok`, `none` oder `unknown`), bei `unknown` `last_backup_reason` (`no access: operation not permitted`) und `last_backup_hint`, sonst `null`; `last_backup` und `last_backup_at` bleiben wie bisher (bei `unknown` aus der Statusdatei oder `null`). Der Exit-Code ändert sich nicht.

**Fehler.** Gezeigt werden die `ERROR`-Zeilen des Logs aus den letzten 24 Stunden, mit Zeitstempel in lokaler Zeit und Alter. Das Fenster ist fest, damit die Ausgabe nicht davon abhängt, ob seither ein Hook gelaufen ist. Ältere Fehler erscheinen nur als Anzahl; `--all-errors` listet sie alle (ohne Begrenzung, sofern `--errors` nicht gesetzt ist). Zeilen über Transkripte, die beim Hook noch nicht existierten (`no such file or directory`), zählen nie als Fehler, auch nicht aus älteren Versionen, die den Fall noch als `ERROR` protokolliert haben.

`raw` zeigt die komprimierte Größe der Rohzeilen und die Events ohne; `blobs` die Zahl der gespeicherten Blobs ([Schema](40-schema.md#blobs)) mit ihrer Größe, wie geliefert, und in Klammern, wie sie im Archiv liegen (PDF und Text zstd-komprimiert; JSON `blobs`, `blob_bytes`, `blob_stored_bytes`); gibt es mehr als Bilder und Dokumente, folgt die Aufteilung mit der gespeicherten Größe je Art: `attachments` (Bilder und Dokumente der Events), `JSON values` (System-Prompts und Tool-Definitionen der Meta-Records), `files` (Sicherungskopien bearbeiteter Dateien), im JSON `blob_kinds` mit `attachments`, `json`, `files`, je `count`, `bytes`, `stored_bytes`. `schema` zeigt die Schema-Version der Datenbank und die, die das Programm kennt; ist die Datenbank älter, steht daneben `MIGRATION PENDING` mit der Anleitung (`total-recall backup`, dann [`total-recall migrate`](#migrate)), im JSON ist `migration_pending` dann `true`. Solange schreiben die Hooks nichts. `page size` ist die Seitengröße der Datei in Bytes; ist sie kleiner als 16384, steht daneben der Hinweis auf [`maintenance`](#maintenance), das die Datei mit größeren Seiten kompakter neu aufbaut. `raw` ist die Größe der gespeicherten Rohzeilen (`events.raw`, komprimiert, siehe [Schema](40-schema.md#raw)) und die Zahl der Events, die eine haben; der Faktor dahinter ist das Kompressionsverhältnis, geschätzt aus den Kopfdaten einer Stichprobe von Zeilen (nichts wird dafür entpackt); am Ende die Zahl der Events, deren Zeile wegen `ingest.raw_max_bytes` nicht gespeichert wurde. `meta records` ist die Zahl und komprimierte Größe der [Meta-Records](40-schema.md#session_meta) (`session_meta`). `memory` zählt das archivierte [Projekt-Gedächtnis](#memory): vorhandene Dateien, Projekte, gelöschte Dateien, alle Fassungen, die Größe der verschiedenen Inhalte und in Klammern, wie sie komprimiert im Archiv liegen (JSON `memory_projects`, `memory_files`, `memory_deleted`, `memory_versions`, `memory_bytes`, `memory_stored_bytes`; die Inhalte zählen auch bei `blobs` mit). Die JSON-Ausgabe enthält dieselben Werte (`schema_version`, `binary_schema_version`, `db_size_bytes`, `page_size`, `raw_rows`, `raw_bytes`, `raw_ratio`, `raw_truncated`, `meta_rows`, `meta_bytes`, `last_ingest`, `last_backup`, …). Die Fehler stehen in `recent_errors` als Objekte mit `at` (UTC, `null` ohne Zeitstempel) und `message`, dazu `errors_window` (`"24h"`, mit `--all-errors` `null`), `errors_since` (Beginn des Fensters, UTC), `recent_errors_total` (Anzahl im Fenster, auch über die Begrenzung hinaus) und `older_errors` (Anzahl davor).

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

── 2026-10-31 19:38:02  human (typed)
Bitte richte goreleaser mit einem Homebrew-Cask ein.

── 2026-10-31 19:41:15  agent
Erledigt: .goreleaser.yml angelegt, Cask im Tap konfiguriert.

── 2026-10-31 19:52:40  human (pasted)
[image image/png 84.3 KiB]
   attachment 3b7c2e1f0a9d8c7b6a5f4e3d2c1b0a9f8e7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b  image/png  84.3 KiB
```

| Option | Bedeutung |
|---|---|
| `--all` | auch Tool-Aufrufe und -Ergebnisse, Automatik, System- und Meta-Events |
| `--since T` / `--until T` | nur Events in diesem Zeitfenster; Formate und Semantik wie bei `search`: `YYYY-MM-DD`, `YYYY-MM-DDTHH:MM`, `YYYY-MM-DD HH:MM` oder RFC 3339, lokale Zeit gemäß `timezone`; ein reines Datum bei `--until` schließt den ganzen Tag ein |
| `--limit N` | höchstens N Events (0 = alle, Default) |
| `--offset N` | die ersten N Events des Fensters überspringen (Blättern mit `--limit`) |
| `--from-event ID` | ab diesem Event (einschließlich), z. B. `event_id` eines Treffers aus `search` |

Bei menschlichen Eingaben steht hinter `human` in Klammern, wie sie ankamen: `typed`, `pasted`, `bash` oder `remote` ([Schema](40-schema.md#input_kind)); ist das nicht bekannt, steht nur `human`. Ein eingefügtes Bild oder Dokument (Platzhalter wie `[Image #2]`) ist `pasted`; Diktat ins Eingabefeld ist `typed`, weil der Harness es nicht vom Tippen unterscheidet. Im JSON hat jedes Event das Feld `input_kind` (String oder `null`, seit v1.6.0).

Die Session kann über die volle ID (`claude:<uuid>`), die native ID oder ein eindeutiges Präfix davon angegeben werden. Ohne `--all` zeigt `show` nur Nachrichten von Mensch, Agent und anderen Agenten (`peer`).

Der Kopf nennt, was bekannt ist: bei Subagent-Sessions `parent`, bei Fortsetzungen `continued from` (die Session, die hier fortgesetzt wurde) und `continued in` (die Session, in der es weiterging), dazu `account` und `organization` mit dem Namen aus [`[accounts]`](20-konfiguration.md#accounts), sofern einer konfiguriert ist. Die JSON-Ausgabe hat dieselben Felder wie `sessions --json`.

Bilder und Dokumente, die in der Session eingefügt oder von Tools geliefert wurden, erscheinen als Event mit Platzhalter (`[image image/png 84.3 KiB]`) und darunter je Anhang eine Zeile `attachment` mit Hash, Medientyp und Größe; [`blob HASH --out DATEI`](#blob) schreibt die Datei. Im JSON hat jedes Event `attachments` (immer vorhanden, `[]` ohne) mit `sha256`, `media_type`, `bytes` und `position`. Anhänge in Tool-Ergebnissen sind nur mit `--all` zu sehen.

Die Filter wirken in dieser Reihenfolge: erst `--all`, dann das Zeitfenster und `--from-event`, dann `--offset` und `--limit`. Ohne Grenzen bleibt die Ausgabe vollständig; bei mehr als 2000 Events steht ein Hinweis auf stderr, wie sich die Ausgabe begrenzen lässt. Mit `--from-event` und einem Event, das ohne `--all` nicht sichtbar ist (etwa ein Tool-Ergebnis), beginnt die Ausgabe bei der nächsten Nachricht. Ein leeres Fenster liefert mit `--json` eine leere Liste `"events": []`.

## `blob` { #blob }

Gibt ein Bild oder Dokument aus dem Archiv aus ([Schema](40-schema.md#blobs)), oder einen ausgelagerten Wert eines [Meta-Records](40-schema.md#session_meta) wie den System-Prompt einer Runde (JSON) eine Sicherungskopie einer bearbeiteten Datei (Datei-Historie, Text maskiert) oder eine Fassung einer Gedächtnis-Datei (Markdown, den Hash nennt [`memory`](#memory)). Den Hash nennt [`show`](#show) in der Zeile `attachment` bzw. unter `attachments` im JSON, bei Meta-Records steht er als `blob:sha256:<hex>` in `raw`; ein eindeutiges Präfix genügt.

```sh
total-recall blob 3b7c2e1f --out screenshot.png      # in eine Datei
total-recall blob 3b7c2e1f | open -f -a Preview      # in eine Pipe
total-recall blob 3b7c2e1f --json                    # nur Metadaten
```

```text
wrote screenshot.png (image/png, 84.3 KiB)
```

| Option | Bedeutung |
|---|---|
| `--out DATEI` | die Daten in diese Datei schreiben (wird überschrieben) statt nach stdout |
| `--format`, `--json` | wie bei den [lesenden Befehlen](#ausgabe); mit `--json` werden nur die Metadaten ausgegeben: `sha256`, `media_type`, `bytes` (wie geliefert), `stored_bytes` (im Archiv), `events` (IDs der Events, deren Zeile den Anhang trägt), `meta_records` (IDs der Meta-Records, die ihn tragen), `memory` (Gedächtnis-Dateien mit diesem Inhalt: `project`, `project_key`, `harness`, `path`, `ts` der ersten Fassung), `out` (die geschriebene Datei, sonst `null`). Mit `--out` wird die Datei trotzdem geschrieben |

Ohne `--out` gehen die Bytes nach stdout, Binärdaten aber nur in eine Pipe oder Datei: Auf ein Terminal schreibt der Befehl sie nicht (`usage`-Fehler mit Hinweis auf `--out`); Text und JSON schon. Ein unbekannter Hash ist `not_found`, ein Präfix, das auf mehrere Anhänge passt, `ambiguous`. Die Daten kommen so zurück, wie sie im Transkript standen (PDF und Text werden entpackt). Der Hash ist der SHA-256 der Daten; `shasum -a 256 screenshot.png` prüft ihn.

## `memory` { #memory }

Zeigt das Projekt-Gedächtnis der Harness, wie es im Archiv liegt. Claude Code führt je Projekt Notizen als Markdown-Dateien (`~/.claude/projects/<projekt>/memory/*.md` mit dem Index `MEMORY.md`), die der Agent laufend schreibt, ändert und löscht. Sie stehen nicht in den Transkripten; `ingest --all` und jeder Hook archivieren sie **versioniert**: eine neue Fassung, wenn sich der Inhalt ändert, ein eigener Eintrag, wenn eine Datei verschwindet. Der Inhalt wird vorher [maskiert](../explanation/datenschutz.md#gedaechtnis). Frühere Stände gibt es ab dem ersten Lauf mit v1.4.0; was vorher geändert oder gelöscht wurde, ist nicht im Archiv.

```sh
total-recall memory                                # alle Dateien aller Projekte, wie sie jetzt sind
total-recall memory --project kompass              # nur ein Projekt
total-recall memory --at 2026-10-01                # wie sie am Ende dieses Tages waren
total-recall memory --deleted                      # gelöschte Dateien mit anzeigen
total-recall memory --history MEMORY.md --project kompass   # alle Fassungen einer Datei
total-recall memory --grep "release workflow"      # Volltextsuche im aktuellen Inhalt
total-recall blob 5e0c9a41d2b7                     # Inhalt einer Fassung
```

```text
PROJECT  FILE                       CHANGED           SIZE     VERSIONS
kompass  MEMORY.md                  2026-10-28 16:02  1.2 KiB  7
kompass  trail-markers.md           2026-10-12 09:40  412 B    1
iris     MEMORY.md                  2026-10-30 11:15  864 B    3
```

| Option | Bedeutung |
|---|---|
| `--project P` | nur Projekte, deren Name, Pfad oder Schlüssel der Harness `P` ist; ohne genauen Treffer die, die `P` enthalten. Der Name ist derselbe wie bei [`stats --by project`](#stats-period) (Definitionen `[[project]]` zuerst); ein Projekt, dessen Verzeichnis unbekannt ist, heißt wie der Schlüssel der Harness (bei Claude Code der Verzeichnisname, etwa `-Users-me-src-kompass`). Kein Treffer ist `not_found` |
| `--at ZEIT` | der Stand zu dieser Zeit (lokale Zeit, `YYYY-MM-DD` meint das Ende des Tages, sonst `YYYY-MM-DDTHH:MM`): je Datei die letzte Fassung bis dahin |
| `--deleted` | gelöschte Dateien mit anzeigen (`SIZE` `deleted`, JSON `deleted: true`, `sha256: null`) |
| `--history DATEI` | alle Fassungen einer Datei, älteste zuerst, Löschungen eingeschlossen; `DATEI` ist der Pfad im Gedächtnis-Verzeichnis. Liegt die Datei in mehreren Projekten (`MEMORY.md` fast immer), ist das ohne `--project` ein `ambiguous`-Fehler, der die Projekte nennt. Mit `--at` nur die Fassungen bis dahin |
| `--grep WÖRTER` | Volltextsuche (FTS5) im aktuellen Inhalt der vorhandenen Dateien, beste Treffer zuerst, mit Textausschnitt; alle Wörter müssen vorkommen. Ein eigener Index, getrennt von dem der Events; dieselbe Suche ist [`search --type memory`](#search) |
| `--raw`, `--limit N` | mit `--grep`: Anfrage unverändert an FTS5 wie bei `search --raw`; höchstens N Treffer (0 = alle) |
| `--format`, `--json` | wie bei den [lesenden Befehlen](#ausgabe) |

**Zeiten.** `CHANGED` (JSON `changed`, bei `--history` `ts`) ist die Änderungszeit der Datei, als das Archiv die Fassung sah; bei einer Löschung die Zeit des Laufs, der sie bemerkte. `--history` zeigt daneben `OBSERVED` (`observed_at`), wann der Lauf die Fassung aufgenommen hat. Ändert der Agent eine Datei zweimal zwischen zwei Läufen, kennt das Archiv nur die zweite Fassung; da jeder Hook nachsieht, ist das selten.

**JSON.** Ohne Option je Datei ein Objekt: `project`, `project_key` (Schlüssel der Harness), `harness`, `cwd` und `git_root` des Projekts (`null`, solange unbekannt), `path`, `sha256` (`null` bei gelöschten), `bytes`, `deleted`, `changed` (UTC), `versions` (Fassungen bis dahin, Löschungen mitgezählt). `--history`: je Fassung `project`, `project_key`, `harness`, `path`, `ts`, `observed_at`, `sha256` (`null` bei einer Löschung), `bytes`, `deleted`. `--grep`: je Treffer `project`, `project_key`, `harness`, `path`, `sha256`, `changed`, `snippet` (Treffer in `[…]`).

**Projektzuordnung.** Das Archiv kennt das Projekt über den Schlüssel der Harness. Das Verzeichnis dazu liest der Adapter aus einem Transkript des Projekts; gibt es keins mehr, sucht er unter den Sessions im Archiv ein Arbeitsverzeichnis, das zu dem Schlüssel passt. Findet er keins, bleibt `cwd` leer, und das Projekt heißt wie sein Schlüssel; ein späterer Lauf trägt das Verzeichnis nach, sobald eine Session dafür auftaucht.

## `search` { #search }

Volltextsuche (SQLite FTS5) mit Textausschnitten, beste Treffer zuerst. Ohne `--type` sucht sie in den Events, also in Nachrichten, Tool-Aufrufen und Tool-Ergebnissen.

```sh
total-recall search goreleaser
total-recall search backup retention --since 2026-10-01      # beide Wörter müssen vorkommen
total-recall search --raw 'cask AND (homebrew OR tap)'       # FTS5-Syntax
total-recall search redaction --actor human --repo seletz/total-recall --json
total-recall search backup --session 9a1b2c3d                # nur in dieser Session
total-recall search goreleaser --by-session                  # eine Zeile je Session, Zeitleiste
total-recall search kompass --type session,memory            # Sessions nach Titel, Projekt-Gedächtnis
total-recall search release --project kompass --type event,memory
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
| `--type T[,T…]` | was durchsucht wird, siehe [Typen](#search-types); Default `event` |
| Filter wie `sessions` | `--since`/`--until` gelten hier für die Zeit des Events (bei `session` für die Zeit, in der die Session lief, bei `memory` für die Zeit der aktuellen Fassung) |
| `--project P` | nur dieses Projekt, bei jedem Typ: der Name wie bei [`stats --by project`](#stats-period) (Definitionen `[[project]]` zuerst), ein Pfad oder der Schlüssel der Harness eines Gedächtnis-Projekts; ohne genauen Treffer alle, deren Name, Pfad oder Remote-URL `P` enthält. Kein Treffer ist `not_found` |
| `--actor A` | nur `human`, `agent`, `peer`, `automation` oder `tool` |
| `--session ID` | nur in dieser Session; volle ID, native ID oder ein eindeutiges Präfix wie bei `show`, ein mehrdeutiges Präfix ist ein Fehler |
| `--raw` | Anfrage unverändert an FTS5: `AND`, `OR`, `NOT`, `"genaue Phrase"`, `präfix*` |
| `--limit N` | höchstens N Treffer je Typ (Default 20, 0 = alle); mit `--by-session` die N Sessions mit den jüngsten Treffern, bei `--order hits` die mit den meisten |
| `--by-session` | eine Zeile je Session statt je Treffer: `first_hit`, `last_hit`, `hits`, `snippet` des ersten Treffers; nur für den Typ `event` |
| `--order first_hit\|hits` | Sortierung mit `--by-session`: nach erstem Treffer, älteste zuerst (Default), oder nach Trefferzahl, meiste zuerst |

Ohne `--raw` wird jedes Wort in Anführungszeichen gesetzt. Dann müssen alle Wörter vorkommen, und Sonderzeichen sind harmlos.

### Typen { #search-types }

Jeder Typ hat seinen eigenen Index; die Namen sind dieselben in CLI, JSON und TUI und gelten für jede Harness.

| Typ | findet | Reihenfolge | gilt mit |
|---|---|---|---|
| `event` | Nachrichten, Tool-Aufrufe und Tool-Ergebnisse (JSON `kind`) | beste zuerst | allen Filtern |
| `session` | Sessions, deren Name, Titel oder Agentenname alle Wörter enthält (Groß-/Kleinschreibung egal) | neueste zuerst | `--since`, `--until`, `--repo`, `--harness`, `--name`, `--account`, `--project`, `--session` |
| `memory` | Dateien des [Projekt-Gedächtnisses](#memory) nach ihrem aktuellen Inhalt | beste zuerst | `--since`, `--until`, `--repo`, `--harness`, `--project`, `--raw` |

`attachment`, `meta` und `file` sind reserviert und noch nicht durchsuchbar; `--type file` ist ein `usage`-Fehler. Mehrere Typen ergeben je Typ eine Gruppe in der Reihenfolge von `--type`; einen Rang über Typen hinweg gibt es nicht, weil die Indizes nicht vergleichbar sind. Ein Filter, der für einen gewählten Typ nicht gilt, ist ein `usage`-Fehler (`--actor does not apply to type memory`), nie ein stilles Weglassen.

In der Textausgabe beginnt jede Zeile mit dem Typ, sobald `--type` etwas anderes als nur `event` wählt; ohne `--type` bleibt sie, wie sie war. Schneidet `--limit` einen Typ ab, steht auf stderr, wie viele Treffer er hätte:

```text
session  2026-10-31 19:38  Release-Pipeline aufsetzen  kompass  claude:9a1b2c3d-…
    [Release]-Pipeline aufsetzen
memory  2026-11-02 08:10  kompass  release.md
    Das [Release] läuft über goreleaser…
memory: 20 of 31 hits, --limit 0 for all
```

**JSON.** Ein flaches Objekt je Treffer. Jeder Treffer hat die Kernfelder `type`, `session_id`, `session_name`, `repo`, `project` (Name wie bei `stats --by project`), `ts` (UTC), `snippet` (Treffer in `[…]`) und `rank`, dazu die Felder seines Typs; innerhalb eines Typs ist jeder Schlüssel immer vorhanden. Skripte verzweigen über `type`.

| Typ | eigene Felder | Anmerkungen |
|---|---|---|
| `event` | `event_id`, `actor`, `input_kind`, `kind` | `rank` FTS5, kleiner ist besser; ein Subagent zählt beim Projekt zu seiner Eltern-Session |
| `session` | `harness`, `ended_at`, `events` | `ts` ist der Start, `snippet` der passende Name, Titel oder Agentenname, `rank` immer `0` |
| `memory` | `project_key`, `path`, `sha256` | `ts` ist die Zeit der aktuellen Fassung, `session_id` und `session_name` sind `null`; den Inhalt gibt `blob SHA256` aus |

Die Zeilen von `--by-session` fassen Event-Treffer zusammen und haben kein `type`.

## `timeline` { #timeline }

Die Events eines Zeitfensters über alle Sessions, älteste zuerst. Ohne weitere Option sind es die Nachrichten des Menschen: Der Befehl beantwortet „War der Abend nach 20:00 noch Arbeit, und woran?“ in einer Ausgabe, ohne Suchbegriff und ohne SQL.

```sh
total-recall timeline --since 2026-10-20T20:00 --until 2026-10-21T06:00
total-recall timeline --since 2026-10-20 --project kompass --json
total-recall timeline --since 2026-10-20 --until 2026-10-20 --actor all --all   # alles, auch Tool-Aufrufe
```

```text
LOCAL                UTC        SESSION   NAME                   PROJECT  TEXT
2026-10-20 20:31:00  18:31:00Z  7d1e02a6  Höhenprofil der Route  kompass  Benenne das Paket route/cache in route/tiles um.
2026-10-20 21:30:00  19:30:00Z  7d1e02a7  Flaky route cache test kompass  Zeig mir, wo die Steigung berechnet wird.
```

| Option | Bedeutung |
|---|---|
| `--since`, `--until` | Zeitfenster, lokale Zeit: `YYYY-MM-DD` oder `YYYY-MM-DDTHH:MM`. `--since` ist inklusiv, `--until` exklusiv; ein Datum als `--until` schließt den ganzen Tag ein. `--since` muss vor `--until` liegen, sonst ist es ein `usage`-Fehler (auch im MCP-Tool) |
| `--project P` | nur dieses Projekt: der Name wie bei [`stats --by project`](#stats-period), sonst ein Teil von Name, Pfad oder Remote-URL; Subagenten gehören zum Projekt ihrer Eltern-Session |
| `--repo R`, `--harness H`, `--name N`, `--account A` | Filter wie bei [`sessions`](#sessions) |
| `--actor A` | wessen Events: `human` (Default), `agent`, `peer`, `automation`, `tool` oder `all`; ein anderer Wert ist ein `usage`-Fehler mit den erlaubten Werten |
| `--all` | auch Tool-Aufrufe und -Ergebnisse, System- und Meta-Events; Default nur Nachrichten. Ohne ausdrückliches `--actor` gilt dann `--actor all` (der Default `human` hätte keine Tool-Aufrufe); ein gesetztes `--actor`, auch `human`, gewinnt |
| `--text-width N` | Zeichen des Textes je Event (Default 160, 0 = ganzer Text) |
| `--fields F` | nur diese Schlüssel der JSON-Events, kommagetrennt (Default und `all`: alle). Ein doppelter Name zählt einmal, Leerzeichen um die Namen zählen nicht, und ein leerer Wert (`--fields=`) ergibt alle Schlüssel, also die Ausgabe ohne `--fields`. Die Reihenfolge ist immer die des ganzen Objekts, ein unbekannter Name ist ein `usage`-Fehler mit den erlaubten. Wirkt nur im JSON |
| `--limit N` | höchstens die ältesten N Events (Default 0 = alle); ein abgeschnittenes Fenster steht auf stderr |

**JSON.** Eine Liste mit einem Objekt je Event, alle Schlüssel immer vorhanden: `event_id`, `ts` (UTC), `ts_local` (RFC 3339 in der konfigurierten Zeitzone), `session_id`, `session_name`, `project` (Name wie bei `stats --by project`), `repo`, `actor`, `input_kind` (bei menschlichen Eingaben `typed`, `pasted`, `bash` oder `remote`, sonst `null`, seit v1.6.0; [Schema](40-schema.md#input_kind)), `kind`, `text` (einzeilig, gekürzt), `text_chars` (Länge des ganzen Textes) und `text_truncated`. Gekürzt wird also nie unbemerkt.

**Die Zahlen stimmen.** Im selben Fenster listet `timeline` genau die Events, die [`stats`](#stats) als `human` zählt, insgesamt und je Projekt (`stats --period day --by project`). Ein Test prüft das gegen das Demo-Archiv, auch über die Seiten des MCP-Tools.

**MCP.** Das Tool `timeline` hat dieselben Parameter (`text_width` statt `--text-width`) und liefert die Liste in Seiten, siehe [MCP anbinden](../how-to/55-mcp-anbinden.md#seiten). Anders als die CLI schickt es als Default `fields=event_id,ts_local,session_id,project,actor,input_kind,kind,text,text_truncated`: Ohne `ts` (`ts_local` hat den Offset), `session_name`, `repo` und `text_chars`, die zusammen rund 40 % der Bytes eines Events ausmachen, trägt eine Seite etwa 45 % mehr Events, ein Tag mit `all` braucht entsprechend weniger Seiten ([#259](https://github.com/seletz/total-recall/issues/259)). `fields=all` bringt alle Schlüssel zurück; die Seiten sind dann byte-gleich die CLI-Ausgabe ohne `--fields`. Für einen Überblick über einen ganzen Tag empfiehlt sich `fields: "ts_local,actor,text"` mit `text_width: 80`: das sind die wenigsten Seiten. Sonst helfen weniger Felder, ein kleineres `text_width` oder ein engeres Fenster; der Hinweis (`hint`) jeder Seite sagt das. Im MCP gilt wie in der CLI: Ein doppelter Name zählt einmal, die Reihenfolge der Namen ist egal, ein leerer Wert (`fields: ""`) ergibt den MCP-Default (die kompakten Schlüssel oben, nicht alle) und `all` alle Schlüssel.

## `stats` { #stats }

Kennzahlen für einen Zeitraum: insgesamt, je Tag, je Repo und je Wochentag × Stunde; mit [`--period`/`--by`](#stats-period) eine Zeitreihe, mit [`--workday`](#stats-workday) die erste und letzte Aktivität je Tag, mit [`--worktime`](#stats-worktime) die geschätzte Arbeitszeit je Tag und Kategorie, mit [`--tools`](#stats-tools) die Tool-Aufrufe der Agenten. Gezählt werden Sessions (mit mindestens einem menschlichen oder Agenten-Event im Bereich), menschliche und Agenten-Events. Tage und Stunden in lokaler Zeit.

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

#### Token: `--tokens` { #stats-tokens }

`tokens_in` eines Events ist die Summe aus frischer Eingabe, Cache-Schreiben und Cache-Lesen; die Preise dafür unterscheiden sich um den Faktor 10 bis 20. Seit Schema v9 ([#185](https://github.com/seletz/total-recall/issues/185)) stehen die Teile einzeln im Archiv ([Schema](40-schema.md#tabellen-und-views)). Die JSON-Ausgabe von `stats` hat sie immer, unter `tokens`; die Tabellen zeigen sie mit `--tokens`: nach den drei Tabellen eine Zeile `total` und eine je Tag, dann eine je Modell, mit den Spalten `INPUT`, `FRESH`, `CACHE WRITE`, `CACHE READ`, `UNSPLIT` und `OUTPUT` (kompakt: 1,2K, 3,4M, 5,6G). `--tokens` gehört zu den Tabellen und lässt sich nicht mit `--workday`, `--worktime`, `--period`, `--by` oder `--top` kombinieren.

```json
"tokens": {
  "events": 88416, "input": 25331165720, "fresh": 1675644, "cache_read": 24497809921,
  "cache_write": 542155856, "unsplit": 0, "output": 80000000,
  "by_day": [{"day": "2026-10-01", "events": 310, "input": 90000000, "fresh": 5000, "cache_read": 88000000, "cache_write": 2000000, "unsplit": 0, "output": 300000}],
  "by_model": [{"model": "claude-opus-5-5", "events": 4000, "input": 9700000000, "fresh": 76200, "cache_read": 9500000000, "cache_write": 100300000, "unsplit": 57300000, "output": 24100000}]
}
```

`input` ist immer `fresh + cache_read + cache_write + unsplit`. `unsplit` ist die Eingabe von Events, deren Aufteilung unbekannt ist, weil ihre Rohzeile fehlt (zu groß für `ingest.raw_max_bytes`, oder vor v1.3.0 gekürzt); `ingest --all --reread --rewrite-raw` holt sie aus den Transkripten nach, soweit die noch da sind. `output` enthält die Thinking-Tokens des Modells. Die Zahlen gelten für alle Events mit Token-Angaben, nicht nur für menschliche und Agenten-Events wie die Aktivitäts-Tabellen; `by_day` rechnet in lokaler Zeit, `by_model` zeigt `null` für Events ohne Modell. Eine Kostenschätzung gibt es nicht: Preise ändern sich je Modell und Vertrag.

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

### Tool-Nutzung: `--tools`, `--by`, `--path` { #stats-tools }

Mit `--tools` zählt `stats` statt der Aktivität die `tool_use`-Events: wie oft die Agenten welches Tool aufgerufen haben (`Bash`, `Read`, `Edit`, MCP-Tools, …). Der Zeitraum kommt wie oben aus `--month` oder `--since`/`--until`. `--by day|session|repo|project` gruppiert (`repo` und `project` wie bei den anderen Statistiken, Subagenten zählen bei `project` für ihre Hauptsession); `--top N` fasst die kleinsten Gruppen in `other` zusammen; ohne `--by` kürzt es `by_tool` auf die N meistgenutzten Tools, der Rest steht als ein Eintrag `other` (zuletzt, nur wenn es mehr als N Tools gibt). **Die Gruppen ergeben immer die Gesamtzahl**, ebenso die Tools einschließlich `other`: nichts fällt weg, auch mit `--top` nicht. `--tools` lässt sich nicht mit `--workday`, `--worktime`, `--tokens` oder `--period` kombinieren; `--by` hat hier die Werte `day`, `session`, `repo`, `project` (nicht die der Zeitreihe).

```sh
total-recall stats --tools --month 2026-10                      # Aufrufe je Tool
total-recall stats --tools --by day --since 2026-10-01           # je Tag und Tool
total-recall stats --tools --by project --top 5 --json
total-recall stats --tools --path '*/internal/store/*' --json    # nur Aufrufe auf dieses Verzeichnis
total-recall stats --tools --path '*/internal/store/*' --path-in all   # auch Nachrichten und Prompts mit dem Pfad
```

```json
{
  "since": "2026-09-30T22:00:00.000Z", "until": "2026-10-31T23:00:00.000Z", "timezone": "Europe/Berlin",
  "by": "day", "path": null, "path_in": null, "total": 412,
  "by_tool": [{"tool": "Bash", "count": 230}, {"tool": "Read", "count": 120}, {"tool": "Edit", "count": 62}],
  "groups": [{"key": "2026-10-01", "count": 40, "by_tool": [{"tool": "Bash", "count": 25}, {"tool": "Edit", "count": 15}]}]
}
```

`by`, `path` und `path_in` sind `null` ohne die Flags (`path_in` ist mit `--path` immer `"paths"` oder `"all"`), `groups` ist dann leer. Tage laufen chronologisch, alle anderen Gruppen nach Anzahl absteigend, `other` zuletzt; die Tools einer Liste ebenso nach Anzahl, dann nach Name. Ein `tool_use` ohne Toolnamen zählt als `unknown`.

**`--path GLOB` und seine Grenzen.** Der Filter prüft die *Argumente* des Aufrufs, so wie das Archiv sie speichert: im Event steht von der Eingabe des Tools nur ein Text (`events.text`), nicht die ganze Eingabe. Es ist das erste vorhandene Feld aus `command`, `file_path`, `path`, `url`, `query`, `pattern`, `description`, `prompt`, `message`, `skill`, sonst das JSON der Eingabe, **auf 1 KiB gekürzt**. Daraus folgt:

- **Welche Tools geprüft werden: `--path-in`.** Standard ist `--path-in paths`: der Glob wird nur auf Aufrufe von Tools geprüft, deren Argument ein Pfad, ein Pfadmuster oder ein Befehl ist: `Bash` (`command`), `Read`, `Edit`, `MultiEdit`, `Write` (`file_path`), `Glob`, `Grep` (`path`, sonst `pattern`) und `LS` (`path`). Eine Nachricht an eine andere Session (`SendMessage`), ein Prompt für einen Sub-Agenten (`Task`), eine URL (`WebFetch`), eine Suchanfrage oder ein MCP-Tool nennen einen Pfad oft nur im Text und zählen deshalb **nicht** als Arbeit an dieser Datei. `--path-in all` prüft wie in v1.6.0 jeden Aufruf. Die Liste steht an einer Stelle im Code (`store.PathTools`). Weil das Archiv nur den *einen* Text je Aufruf speichert, nicht den Namen des Feldes, entscheidet der Toolname, nicht das Feld: ein `Grep` ohne `path` zählt mit seinem Muster, `NotebookEdit` fehlt (sein Pfad steht nicht unter den gespeicherten Feldern), und ein MCP-Tool, das wirklich Dateien bearbeitet, braucht `--path-in all`. `--path-in` verlangt `--path`; ohne `--path` zählt `--tools` alle Aufrufe.
- **Datei-Tools** (`Read`, `Edit`, `Write`, …): der Glob muss auf den **ganzen** Pfad passen, also `*/internal/store/*` oder `/home/me/proj/*.go`, nicht `internal/store`. SQLite-GLOB: `*`, `?` und `[a-z]` (auch über `/` hinweg), Groß- und Kleinschreibung zählt.
- **Bash**: der Glob darf **irgendwo im Befehl** passen (der Filter setzt `*` davor und dahinter). `--path /w/app` findet `ls /w/app/src` und `git -C /w/app status`, aber auch `echo /w/app`. Ob der Pfad Ziel oder nur Teil eines Arguments ist, erkennt der Filter nicht; Pfade in Variablen, Shell-Expansion (`$HOME`, `~`, relative Pfade nach einem `cd`) und Pipes über mehrere Verzeichnisse werden nicht aufgelöst.
- **Lange Befehle**: ein Pfad hinter der 1-KiB-Grenze wird nicht gefunden. Wer das Original braucht, liest es mit `sql` aus `raw_text(raw)` (nur mit `store_raw`).
- **Andere Tools** (nur mit `--path-in all`: `WebFetch`, `Task`, MCP-Tools, …) werden auf ihr erstes bekanntes Feld oder ihr JSON als Ganzes geprüft; ein Glob wie `*pfad*` trifft dort zuverlässiger als ein genauer Pfad.
- Die Pfade stehen so im Archiv, wie der Agent sie geschrieben hat (maskiert wie jeder Text); es gibt keine Normalisierung zwischen absolut und relativ.

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
| `--blocks` | je Tag die **Aktivitätsblöcke** statt nur erster und letzter Aktivität, siehe unten. Nur mit `--workday` |

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

**Blöcke.** `--blocks` zerlegt die Spanne jedes Tags in Blöcke, etwa für eine Anwesenheitsbuchung: 07:48–09:05 und 11:24–12:40 statt 07:48–12:40. Eine Lücke zwischen zwei gezählten Events **über** `block_gap` in [`[work]`](20-konfiguration.md#work) (Standard `30m`) beendet einen Block; eine Lücke von genau `block_gap` tut es nicht. Gezählt wird wie sonst bei `--workday` (`count`, `--all-events`), die Tagesgrenze gilt auch hier: Blöcke über Mitternacht gehören zum Tag davor, bis `day_start`. Ein einzelnes Event ist ein Block von 0 s. Die Tabelle bekommt die Spalten `BLOCKS` (`08:12-09:05 11:24-12:40`), `WORK` (Summe der Blöcke) und `BREAKS` (Summe der Pausen dazwischen); `WORK` plus `BREAKS` ist immer die Spanne, und der Zeitraum wird nie gekürzt.

```json
"blocks": [
  {"start": "2026-10-27T06:48:00.000Z", "end": "2026-10-27T08:05:00.000Z", "start_local": "07:48", "end_local": "09:05", "seconds": 4620, "events": 18, "single": false},
  {"start": "2026-10-27T10:24:00.000Z", "end": "2026-10-27T11:40:00.000Z", "start_local": "11:24", "end_local": "12:40", "seconds": 4560, "events": 23, "single": false}
],
"block_seconds": 9180, "break_seconds": 8340
```

Im JSON hat jeder Tag mit `--blocks` `blocks` (leer ohne Aktivität), `block_seconds` und `break_seconds`; oben steht `block_gap_seconds`, in `total` die Summen `block_seconds` und `break_seconds` über alle Tage. Ohne `--blocks` fehlen diese Felder. Im [MCP-Tool](#mcp) `stats` heißt der Parameter `blocks`.

**Einzelne Nachrichten.** Eine einzelne Nachricht nach einer langen Pause ergibt einen Block von 0 Sekunden: für eine Anwesenheitsbuchung Rauschen. Damit der Aufrufer ihn erkennt, ohne Events nachzuzählen, hat jeder Block `events` (die Zahl der gezählten Events darin) und `single` (`true`, wenn es genau eins ist). Die Sekunden bleiben unverändert, es gibt keinen Mindestwert, nichts fällt weg. Zwei Events mit derselben Zeit sind ein Block von 0 Sekunden mit `events: 2`, kein einzelner. Es gilt immer: Pro Tag ergeben die `events` aller Blöcke genau `human` plus `agent` des Tags, und `block_seconds` plus `break_seconds` die Spanne. Alle Sekunden sind ganze Sekunden: Die Blockgrenzen werden nach der ersten Aktivität des Tags auf ganze Sekunden abgerundet, bevor die Dauern gebildet werden, daher ist `block_seconds` exakt die Summe der `seconds` der Blöcke und `block_seconds` plus `break_seconds` exakt `span_seconds` (auch bei Zeitstempeln mit Millisekunden), in `total` genauso. Die Tabelle zeigt einen einzelnen Block nur mit der Uhrzeit und einem `*` (`15:00*`) und erklärt das Zeichen unter der Tabelle.

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

**Projekte ohne Kategorie.** Weil so ein Projekt still als eigene Kategorie läuft, fällt ein neues Repository sonst erst beim Buchen auf. Das JSON hat dafür `uncategorized`: die Projekte mit Zeit im Zeitraum, auf die keine Regel passt, mit `project`, `key`, `remote` und `raw_seconds` (Summe über den Zeitraum), die größten zuerst; `[]`, wenn jedes Projekt eine Kategorie hat. Die Liste ist ein **Teil** der Zeit, kein Zusatz: dieselben Sekunden stehen auch in `days`, `categories` und `total`, jedes Projekt als Kategorie mit seinem Namen. Die Tabellenausgabe meldet sie nach der Tabelle **auf stderr** (`stats: 2 projects without a category …`), damit stdout unverändert bleibt. Die Meldung erscheint nur, wenn überhaupt Kategorien konfiguriert sind; ohne `[[worktime.category]]` ist „jedes Projekt eine Kategorie“ gewollt. [`doctor`](#doctor) weist auf Repositories hin, die schon aktiv sind, bevor man eine Buchung macht.

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
  "uncategorized": [],
  "total": {"raw_seconds": 13402, "rounded_seconds": 14400, "days": 1}
}
```

`--worktime` lässt sich nicht mit `--workday`, `--all-events`, `--period`, `--by` oder `--top` kombinieren; `--sessions` und `--summary` brauchen `--worktime`. Ein Fehler in `[worktime]` bricht nur diesen Befehl ab (`kind` `config`).

## `chart` { #chart }

Zeichnet die Grafiken eines Monatsberichts als SVG (Default) oder PNG: eine Grafik nach stdout oder mit `--out DIR` als Dateien, `all` alle fünf. Rezept mit Beispielbildern: [Grafiken für einen Monatsbericht](../how-to/47-monatsbericht.md).

```sh
total-recall chart <calendar|projects|punchcard|worktime|workday|all> [--month YYYY-MM | --since/--until | --range NAME]
                   [--format svg|png] [--scale N | --width PX | --height PX] [--out DIR]
                   [--no-title] [--no-legend] [--background COLOR | --transparent] [--top N] [--lang en|de] [--title TEXT]
```

| Grafik | Inhalt | Richtwert |
|---|---|---|
| `calendar` | Monatsraster, ISO-Wochen als Spalten, Mo–So als Zeilen; Zellfarbe = eigene Nachrichten je Tag in vier Stufen relativ zum Maximum, Tageszahl in der Zelle, Legende mit Maximum und Summe | 360 × 290 |
| `projects` | je Projekt zwei horizontale Balken: Anteil an den Agenten-Events und Anteil an den eigenen Nachrichten, in Prozent des Zeitraums; die `--top` größten Projekte (Default 12 aus `[chart]`), der Rest als `other` | 610 × 370 |
| `punchcard` | Wochentag × Stunde (7 × 24): großer heller Kreis = Agenten-Events, kleiner dunkler = eigene Nachrichten, Radius nach Wurzel des Anteils am jeweiligen Maximum | 760 × 290 |
| `worktime` | gestapelte Balken je ISO-Woche und Kategorie aus [`--worktime`](#stats-worktime) (gerundete Zeit), Strich je Woche beim Soll: Tagessoll aus [`[work]`](20-konfiguration.md#work) × Arbeitstage der Woche im Zeitraum (Mo–Fr ohne Feiertag, bis heute) | 560 × 260 |
| `workday` | die Arbeitstage der TUI: eine Zeile je Tag mit Wochentag und Datum, Balken von der ersten bis zur letzten Aktivität auf einer Zeitachse ab `day_start` (blau in der Kernzeit, orange davor und danach, dunkleres Orange nach Mitternacht mit `(+1)` im Text), Kernzeit als Band, Soll als gestrichelte Linie, Wochenende grau, Feiertage rot mit Namen; rechts erste und letzte Aktivität, Spanne und Abweichung vom Soll; Kopf mit Summe, Soll und Saldo. Zählweise und Grenzen wie [`stats --workday`](#stats) (`[work]`) | 720 × (60 + 17 je Tag) |

Die Beispiele zeigen jede Grafik für Oktober 2026, gerendert aus dem synthetischen Demo-Archiv (`mise run docs:charts`):

![calendar](../assets/charts/2026-10-calendar.svg)

![projects](../assets/charts/2026-10-projects.svg)

![punchcard](../assets/charts/2026-10-punchcard.svg)

![worktime](../assets/charts/2026-10-worktime.svg)

![workday](../assets/charts/2026-10-workday.svg)

Der Zeitraum ist der laufende Monat, `--month YYYY-MM`, `--since`/`--until` (ein Datum schließt den Tag ein) oder `--range today|this-week|last-week|this-month|last-month`. Gezählt wird wie bei [`stats`](#stats): eigene Nachrichten sind Events mit `actor = human`, Aufträge anderer Agenten (`peer`) und Automatisches zählen nicht; `calendar`, `projects` und `punchcard` nehmen Kalendertage ab Mitternacht, `worktime` und `workday` die Tagesgrenze `day_start` wie `stats --worktime` bzw. `stats --workday`.

Der Hintergrund ist transparent, die Grafik passt in Notizen mit hellem und dunklem Grund; `--background #rrggbb` setzt eine Farbe, `--transparent` erzwingt keine. Jedes Element trägt seine Werte als Tooltip (`<title>`).

Dateien heißen `<YYYY-MM>-<grafik>.<format>` für einen ganzen Monat, sonst `<von>_<bis>-<grafik>.<format>` mit dem ersten und letzten Tag (`2026-10-05_2026-10-11-calendar.svg`); die Pfade werden ausgegeben. `all` braucht `--out`.

`--format svg|png`
:   SVG wird selbst erzeugt (Schrift: System-Sans des Betrachters, Tooltips auf Zellen, Balken und Kreisen), PNG in reinem Go gerastert (Go Sans). Default aus [`[chart]`](20-konfiguration.md#chart): `svg`.

`--scale N`, `--width PX`, `--height PX`
:   `--scale` multipliziert die natürliche Größe (Default aus `[chart]`: 1). `--width` oder `--height` setzen die Größe in Pixeln, die andere Seite folgt; beide zusammen bilden einen Rahmen, in dem die Grafik zentriert sitzt. Ein SVG trägt die Größe als `width`/`height`, die `viewBox` bleibt die natürliche Größe.

`--no-title`, `--no-legend`, `--transparent`
:   Überschrift (`<Grafik> · Oktober 2026`) und Legende weg. Defaults aus `[chart]`: `title`, `legend`.

`--lang en|de`
:   Sprache der Beschriftungen: Titel, Legende, Achsen, Wochentage und Monatsnamen, dazu Datums- und Zahlenformat (`W41` / `KW 41`, `2026-10-05` / `05.10.2026`, `7.5h` / `7,5 h`, `other` / `Sonstige`). Die Dateinamen bleiben gleich. Default aus `[chart]`: `en`.

`--title TEXT`
:   Eigene Überschrift statt `<Grafik> · <Zeitraum>`; `--no-title` geht vor.

`--top N`
:   `projects`: die N größten Projekte, der Rest als `other`; `0` = alle. Default aus `[chart]`: 12.

Farben: ein Projekt mit `color` in [`[[project]]`](20-konfiguration.md#project) behält sie, die anderen bekommen die Reihenfarben in der Reihenfolge des Volumens, `other` grau; eine Kategorie mit `color` in [`[[worktime.category]]`](20-konfiguration.md#worktime) ebenso, die anderen die freien Reihenfarben in der Reihenfolge der Regeln, Projekte ohne Regel danach alphabetisch; die Stufen des Kalenders stehen in `scale_orange` (oder `heatmap`), die der Arbeitstage in `scale_blue`/`scale_orange`, die Palette der Projekte in `palette` von `[chart]`. Ein Fehler in `[chart]` oder in einer Farbe bricht `chart` ab, keinen anderen Befehl ([Prüfung](20-konfiguration.md#pruefung)).

## `tui` { #tui }

Öffnet das Archiv in einer Terminal-Oberfläche: Volltextsuche mit Vorschau beim Tippen, Detailansicht je Session, Aktivität als Chart, Tabelle und Heatmap, erste und letzte Aktivität je Tag, dazu die Ansicht [Config](30-tui.md#config) mit Status, Einstellungen, Wartungsaktionen und Prüfung der Installation. Lesend, bis auf die Aktionen und das Ändern von Einstellungen in Config, die nach Bestätigung schreiben wie die gleichnamigen Befehle. Bedienung, Tasten und Grafikmodi: [TUI](30-tui.md).

```sh
total-recall tui                              # Suche
total-recall tui stats                        # Statistik
total-recall tui activity                     # erste und letzte Aktivität je Tag
total-recall tui config                       # Status, Einstellungen, Aktionen, Setup
total-recall tui --query "goreleaser cask"    # Suche mit vorbelegtem Text
total-recall tui stats --graphics text        # Chart als Text, keine Terminal-Abfrage
```

| Argument / Option | Bedeutung |
|---|---|
| `search` \| `stats` \| `activity` \| `config` | Ansicht beim Start, Default `tui.view` aus der Config, sonst `search` |
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

Schreibt eine konsistente, mit zstd komprimierte Kopie der Datenbank nach `<backup.dir>/archive-YYYY-MM-DD.db.zst`. Datum und Uhrzeit sind in der konfigurierten Zeitzone. Ein neues Backup ersetzt nie ein vorhandenes: Hat der Tag schon eine Kopie, auch eine `archive-YYYY-MM-DD.db.gz` (bis v1.2) oder eine unkomprimierte `archive-YYYY-MM-DD.db` noch älterer Versionen, heißt die neue `archive-YYYY-MM-DD-HHMM.db.zst`, in derselben Minute `archive-YYYY-MM-DD-HHMM-2.db.zst` und so weiter. Als Zeitpunkt einer Kopie gelten Datum und Uhrzeit im Namen, ohne Uhrzeit im Namen die Änderungszeit der Datei. Danach werden alte Kopien nach den Regeln in [`[backup]`](20-konfiguration.md#backup) aufgeräumt, die ältesten zuerst; jede gelöschte Kopie steht als `backup: pruned …` im Log. Alle Namen und Formate zählen dabei gleich, und `status`, `backup --if-older` und die TUI finden die neueste Kopie unter jedem Namen.

zstd statt gzip seit v1.3.0: Auf einer Kopie eines echten Archivs (444 MiB) schrieb gzip 245 MiB in gut 6 Sekunden, zstd 220 MiB in rund 2; das Entpacken dauert 0,4 statt 2 Sekunden. Die Stufe ist fest (`SpeedBetterCompression`, höchstens 4 Threads, etwa 30 MiB Arbeitsspeicher).

Die Kopie entsteht per `VACUUM INTO` zuerst als Temporärdatei neben der Datenbank (nicht im Backup-Ordner, der von iCloud synchronisiert wird), wird dann komprimiert und erst zum Schluss unter ihrem endgültigen Namen abgelegt. Im Backup-Ordner liegt also nie eine halbe Kopie unter dem richtigen Namen.

```sh
total-recall backup
total-recall backup --if-older 24h            # nur, wenn das letzte Backup älter ist
total-recall backup --if-older 24h --detach   # aus einem Hook
```

```text
backup written: /Users/me/Documents/total-recall/archive-2026-11-01.db.zst
```

| Option | Bedeutung |
|---|---|
| `--if-older DAUER` | nichts tun, wenn die neueste Kopie jünger ist (Go-Dauer: `24h`, `90m`) |
| `--detach` | im Hintergrund laufen, sofort mit 0 zurückkehren; Ausgabe und Fehler ins Log |

## `restore` { #restore }

Stellt eine Backup-Kopie wieder her: entpackt `archive-YYYY-MM-DD.db.zst` oder `archive-YYYY-MM-DD-HHMM.db.zst` (oder eine `.db.gz` von bis v1.2, oder kopiert eine alte, unkomprimierte `.db`) in eine Datenbankdatei. Das Format erkennt der Befehl an der Endung.

```sh
total-recall restore ~/Documents/total-recall/archive-2026-11-01.db.zst --to /tmp/archive.db
total-recall --db /tmp/archive.db sessions --limit 5     # Inhalt ansehen
total-recall restore ~/Documents/total-recall/archive-2026-11-01.db.zst --force   # laufende DB ersetzen
```

```text
restored /Users/me/Documents/total-recall/archive-2026-11-01.db.zst to /tmp/archive.db
```

| Option | Bedeutung |
|---|---|
| `--to PFAD` | Ziel; Default ist die konfigurierte Datenbank |
| `--force` | eine bestehende Zieldatei ersetzen (samt `-wal`/`-shm`) |

Ein bestehendes Ziel wird ohne `--force` nie überschrieben. Die Daten landen zuerst in einer Temporärdatei neben dem Ziel und werden geprüft (Schema-Version, `PRAGMA quick_check`); erst dann wird das Ziel ersetzt. Vor `--force` auf die laufende Datenbank: sicherstellen, dass gerade kein `ingest` läuft. Was seit dem Backup dazukam, liest das nächste `ingest --all` wieder ein, solange die Transkripte noch da sind. Ohne das Programm: `zstd -d archive-2026-11-01.db.zst -o archive.db` (für `.db.gz`: `gunzip -k`), siehe [Backup wiederherstellen](../how-to/60-wartung.md#backup-wiederherstellen).

## `maintenance` { #maintenance }

Baut die Datenbankdatei kompakt mit einer neuen Seitengröße neu auf und tauscht sie atomar aus. Die Zeilen des Archivs sind meist 1 bis 4 KiB groß; bei SQLites Standard von 4 KiB je Seite bleiben dadurch viele Seitenreste ungenutzt, mit 16 KiB je Seite ist die Datei um rund ein Siebtel kleiner, ohne dass sich am Schema etwas ändert. Wann und wie: [Archiv warten](../how-to/60-wartung.md).

```sh
total-recall maintenance --dry-run          # nur messen: wie groß würde die Datei?
total-recall maintenance                    # Backup, Neuaufbau mit 16 KiB-Seiten, Austausch
total-recall maintenance --page-size 8192   # andere Seitengröße
total-recall maintenance --json
```

```text
backup written: /Users/me/Documents/total-recall/archive-2026-11-04.db.zst
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
2. Backup wie [`backup`](#backup) (gleicher Ordner, gleiche Aufbewahrung). Ein älteres Schema migriert der Befehl nicht; er bricht mit dem Hinweis auf [`migrate`](#migrate) ab.
3. Exklusives Lock gegen Hooks (`locks/maintenance.lock` neben der Datenbank). Hält ein `ingest` oder `backup` das Archiv noch offen, wartet der Befehl bis `--wait` und bricht dann mit `writers active` ab; schon beim Warten kündigt er sich an (`locks/maintenance.pending`), damit kein neuer Schreiber mehr startet. Hooks, die währenddessen starten, überspringen ihren Lauf (eine `INFO`-Zeile im Log, kein Fehler) und lesen beim nächsten Hook derselben Session oder beim nächsten `SessionStart` ab dem gespeicherten Offset weiter; es geht nichts verloren.
4. WAL-Checkpoint. Hält ein lesender Prozess gerade eine Lesetransaktion offen (eine laufende Abfrage von `sql`, `status` oder der TUI), bricht der Befehl mit `readers active` ab: den Prozess schließen oder kurz warten und noch einmal starten. Eine TUI im Leerlauf hält keine offen; sie bemerkt die ausgetauschte Datei bei der nächsten Prüfung und öffnet sie neu, siehe [Neuladen](30-tui.md#neuladen).
5. `PRAGMA page_size` und `VACUUM INTO` in eine Temporärdatei `archive.db.maint-*` neben der Datenbank.
6. `PRAGMA integrity_check` auf der Kopie und Vergleich der Zeilenzahlen von `sessions`, `events`, `sources` mit dem Original.
7. Austausch: `-wal`/`-shm` des Originals entfernen, Kopie per `rename` an die Stelle der Datenbank. Danach öffnet der Befehl die neue Datei einmal (WAL-Modus wieder an) und zeigt die Werte nachher.

Ein Abbruch, auch mit `Ctrl-C`, löscht die Temporärdatei; Reste einer abgestürzten Wartung räumt der nächste Lauf weg. Hat die Wartung die Datei einmal ausgetauscht, ist sie fertig: Das Backup aus Schritt 2 ist nur eine Absicherung, [`restore`](#restore) spielt es bei Bedarf zurück.

## `migrate` { #migrate }

Bringt die Datenbank auf das Schema dieser Programmversion. Das ist der einzige Weg dahin: Kein anderer Befehl migriert, auch kein Hook. Nach einem Update auf eine Version mit neuem Schema schreiben die Hooks nichts, bis dieser Befehl gelaufen ist; [`status`](#status), [`doctor`](#doctor) und die [TUI](30-tui.md) weisen darauf hin. Rezept: [Aktualisieren](../how-to/10-installieren.md#aktualisieren).

```sh
total-recall backup                 # zuerst sichern
total-recall migrate --dry-run      # nur den Plan zeigen
total-recall migrate                # Plan zeigen, nachfragen, migrieren
total-recall migrate --yes          # ohne Nachfrage, für Skripte
total-recall migrate --json
```

```text
database  /Users/me/Library/Application Support/total-recall/archive.db  (1145.6 MiB)
schema    v4 -> v5, 1 pending migration:
  v5    events: raw zstd-compressed (BLOB), source_id instead of source_path; rewrites every event, about a minute per 100,000 events, and needs free disk space of about twice the archive size next to it (copy and WAL)
backup    /Users/me/Library/Application Support/total-recall/archive.db.pre-v4.bak
space     about 2004.8 MiB free needed next to the database (copy and migration)
Migrate now? Hooks that run meanwhile skip and catch up afterwards. [y/N] y
store: schema v4 -> v5: backup written to /Users/me/Library/Application Support/total-recall/archive.db.pre-v4.bak
migration v5: 209000/233198 (89 %)
migration v5: 233198/233198 (100 %)
store: migration v5 applied (25.077s)
migrated v4 -> v5 (27.15s); the copy /Users/me/Library/Application Support/total-recall/archive.db.pre-v4.bak can be removed once everything works
```

Die Zahlen stammen von einer Kopie eines echten Archivs mit 233 198 Events (1,1 GiB). Die Migration auf v5 schreibt jedes Event neu ([`raw`](40-schema.md#raw) komprimiert, `source_id`); danach ist die Datei zunächst **größer** als vorher (1,5 GiB, die alten Seiten stehen auf der Freiliste), erst [`maintenance`](#maintenance) baut sie klein (438 MiB). Deshalb nach `migrate` einmal `maintenance`, siehe [Aktualisieren](../how-to/10-installieren.md#aktualisieren).

Die Migration auf **v6** (v1.3.0) legt nur die Tabellen für [Bilder und Dokumente](40-schema.md#blobs) an und dauert Sekunden; sie füllt sie nicht. Das macht danach [`ingest --all --reread --rewrite-raw`](#ingest), das die Transkripte neu liest, solange es sie noch gibt.

| Option | Bedeutung |
|---|---|
| `--dry-run` | nur den Plan zeigen: Versionen, ausstehende Migrationen, Pfad der Kopie, nötiger Platz; nichts ändern |
| `--yes` | nicht nachfragen |
| `--wait DAUER` | so lange auf laufende Hooks warten, die das Archiv noch offen haben (Default `60s`) |
| `--format`, `--json` | wie bei den [lesenden Befehlen](#ausgabe); JSON hat `db`, `dry_run`, `from`, `to`, `pending`, `steps` (je `version`, `summary`, `space_factor`), `size_bytes`, `need_bytes`, `backup` (`null`, wenn nichts aussteht), `migrated`, `duration_ms`. Ohne `--yes` fragt auch `--json` nach |

Ablauf, jeder Schritt bricht bei einem Fehler ab:

1. Plan: Version der Datenbank und des Programms, je ausstehender Migration eine Zeile. Ist die Datenbank aktuell, endet der Befehl hier (`is current, nothing to migrate`).
2. Nachfrage, sofern nicht `--yes`. Alles außer `y` oder `yes` bricht ab (`migration cancelled, the archive is unchanged`, Exit 1).
3. Prüfen, ob neben der Datenbank genug frei ist: die Kopie (etwa Dateigröße), dazu der Platzbedarf der größten Migration (als Faktor der Dateigröße für die WAL; v5 deklariert 0,5, gemessen wächst sie auf gut ein Drittel der Datei) und ein Viertel Reserve. Für v5 also knapp das Doppelte der Dateigröße.
4. Exklusives Lock gegen Hooks wie bei [`maintenance`](#maintenance): Hält ein `ingest` oder `backup` das Archiv noch offen, wartet der Befehl bis `--wait` und bricht dann mit `writers active` ab. Hooks, die währenddessen starten, überspringen ihren Lauf und lesen danach nach. Die TUI darf offen bleiben; sie zeigt die Migration nach der nächsten Prüfung.
5. Kopie `archive.db.pre-v<N>.bak` neben der Datenbank (N = bisherige Version), per `VACUUM INTO`.
6. Die Migrationen, jede in eigener Transaktion, danach ein WAL-Checkpoint. Bei langen Migrationen zeigt der Befehl den Fortschritt, höchstens alle zwei Sekunden.

Ein Fehler lässt die Datenbank auf der letzten vollständig angewandten Version; die Kopie bleibt liegen. Sie lässt sich wie jede Datenbankdatei öffnen (`total-recall --db archive.db.pre-v4.bak status`, mit dem alten Binary) oder zurückkopieren. Läuft alles, kann sie weg.

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
| unbekannte Record-Typen | weder eingelesen noch als Meta-Record noch als bewusst übersprungen bekannt (`skip_record_types` aus der Config zählt als bekannt); sie werden trotzdem als [Meta-Record](40-schema.md) aufbewahrt, der Befund sagt nur, dass der Adapter die Liste nachziehen sollte |
| unbekannte Content-Block-Typen | in `user`-/`assistant`-Nachrichten oder im Inhalt eines `tool_result`, z.B. `assistant/hologram` |
| fehlende Felder | `timestamp`, `sessionId`, `uuid`, `cwd` bei `user`/`assistant`/`system`; `sessionId` bei Titel-Records |
| kaputte Zeilen | vollständige Zeilen, die kein JSON-Objekt sind (eine halb geschriebene letzte Zeile zählt nicht) |
| Versionen | die Claude-Code-Versionen in den Transkripten, neueste zuerst, mit Anzahl Zeilen |

Je Befund steht die erste Fundstelle (`Datei:Zeile`) dabei. Inhalte werden nicht ausgegeben.

**Hinweis auf nicht eingerichtete Projekte.** Ist das Archiv da, nennt `doctor` zusätzlich die Projekte mit eigenen Nachrichten in den letzten `--days` Tagen (Default 30), auf die kein [`[[project]]`](20-konfiguration.md#project) passt oder die keine `[[worktime.category]]` abdeckt:

```text
hint: 2 projects with activity in the last 30 days not fully set up in the config:
  dotfiles: no [[project]]
  wetterwarte: no [[project]], no category
```

Jede Prüfung gilt nur, wenn die Tabelle Einträge hat: ohne `[[project]]` gibt es kein „kein Projekt“, ohne Kategorien kein „keine Kategorie“. Die Kategorie wird wie bei [`stats --worktime`](#stats-worktime) bestimmt. Der Hinweis ist kein Befund: **Der Exit-Code bleibt 0**, auch wenn es nur Hinweise gibt (Exit-Code 2 bedeutet ausschließlich Befunde). Im Text stehen die Zeilen auf stdout. Mit `--json` stehen sie als Feld `hints` in **jedem** Bericht der Liste, getrennt von den Befunden (deren Felder bleiben, wie sie sind), und zusätzlich als Text auf stderr. `--days 0` schaltet den Hinweis ab, `hints` ist dann `[]`.

```json
"hints": [
  {"project": "wetterwarte", "key": "/home/mara/src/wetterwarte", "missing": ["project", "category"], "last_active": "2026-10-28T16:57:00Z"}
]
```

`project` ist der Anzeigename, `key` der Schlüssel des Projekts (wie bei `stats --worktime`), `missing` nennt `project` (kein `[[project]]` passt) und/oder `category` (keine Kategorie deckt es ab), `last_active` die Zeit der letzten eigenen Nachricht (UTC). Ohne Hinweise ist `hints` `[]`.

| Option | Bedeutung |
|---|---|
| `--harness NAME` | zu prüfende Harness, Default `claude` |
| `--days N` | Zeitfenster des Hinweises auf nicht eingerichtete Projekte, Default 30; 0 schaltet ihn ab |
| `--json` | Liste von Berichten (`unknown_record_types`, `unknown_block_types`, `missing_fields`, `malformed`, `versions`, dazu `hints`); Konfigurationsprobleme gehen dann auf stderr |

Exit-Code 0: nichts Unbekanntes (Hinweise zählen nicht). Exit-Code 2: Befunde. Siehe [Nach einem Claude-Code-Update](../how-to/20-hooks-einrichten.md#nach-einem-claude-code-update).

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

## `mcp` { #mcp }

Startet den lesenden MCP-Server auf stdin/stdout. Claude Code startet ihn als Unterprozess; du rufst ihn selten selbst auf. Kein Port, kein Daemon, nur lesend. Einrichten und Tools: [Archiv per MCP anbinden](../how-to/55-mcp-anbinden.md).

```sh
total-recall mcp
total-recall mcp print                       # der Eintrag als JSON
total-recall mcp print --command             # die Zeile `claude mcp add …`
total-recall mcp print --binary "$(command -v total-recall)"
```

Der Server bietet die Tools `search`, `sessions`, `show`, `stats`, `memory` und `status`. Jedes führt den gleichnamigen Befehl mit `--json` aus; Ausgabe und [Fehlerobjekt](#ausgabe) sind die der CLI. Ein Ergebnis ist bei jedem Tool auf 32 KiB begrenzt, ohne dass etwas fehlt: Ist die CLI-Ausgabe größer, kommt sie in **Seiten**. Der erste Inhalt ist eine vollständige Seite (gültiges CLI-JSON mit ganzen Einträgen der Seitenliste, alle Summen und übrigen Felder auf jeder Seite), ein zweiter nennt `page` und `next.cursor`. Das Tool mit denselben Argumenten und `cursor` holt die nächste Seite ([Seiten](../how-to/55-mcp-anbinden.md#seiten)). Jeder Aufruf öffnet das Archiv neu und schreibgeschützt. Die persistenten Optionen `--config`, `--db` und `--tz` gelten auch für die Tools.

| `mcp print` | Bedeutung |
|---|---|
| `--harness NAME` | derzeit nur `claude` (Default) |
| `--binary BEFEHL` | Befehl im Eintrag, Default `total-recall` |
| `--command` | statt des JSON die Zeile `claude mcp add --scope user total-recall -- BEFEHL mcp` |

`mcp print` schreibt nichts in die Einstellungen von Claude Code.

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

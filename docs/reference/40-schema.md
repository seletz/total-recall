---
title: Schema
icon: lucide/database
---
# Schema

Das Wichtigste zum Schema der Datenbank für eigene Abfragen mit [`sql`](10-befehle.md#sql). Beispiele stehen im How-To [Archiv auswerten](../how-to/30-auswerten.md), die vollständige Definition aller Tabellen und Views in der Spezifikation unter [Datenmodell](../explanation/design.md#datenmodell). Dieselbe Kurzfassung bekommen Agenten mit `total-recall llm --skill`.

## Tabellen und Views

| Tabelle / View | Inhalt |
|---|---|
| `sessions` | eine Zeile je Session: Name, Repo, Branch, Start, Ende. `display_name` ist der beste Titel: eigener Titel (`name`), sonst KI-Titel (`ai_title`), sonst `agent_name`. `parent_id` zeigt bei Subagenten auf die Eltern-Session, `continued_in` auf die Session, in der diese fortgesetzt wurde (volle ID, Ziel muss nicht archiviert sein), `account_id` und `organization_id` nennen den Account, auf dem sie lief (seit Schema v4) |
| `events` | eine Zeile je Nachricht, Tool-Aufruf oder Tool-Ergebnis |
| `events_fts` | Volltextindex über `events.text` |
| `exchanges` | je menschlicher Nachricht: die Anfrage und die letzte Antwort des Agenten |
| `daily_activity` | je Tag (lokale Zeit) und Session: Repo, Anzahl human-/agent-Events |
| `session_meta` | Harness-Records, die keine Nachricht sind, aber zur Session gehören (seit Schema v6): von der Harness eingefügter Kontext (Claude Code: `attachment` mit den `CLAUDE.md`-Dateien, @-erwähnten Dateien, Hook-Ausgaben, …), während der Arbeit des Agenten getippter Text (`queue-operation`), Modus, Pull Request (`pr-link`), Kosten (`cost-state`), Datei-Historie, Workflow-Journal und unbekannte Typen. Je Record eine Zeile, gleicher Inhalt je Session einmal; kein Event, nicht im Volltextindex, in keiner Statistik. Spalten: `seq`, `id`, `session_id`, `ts` (NULL ohne Zeitstempel), `type` (Typname der Harness), `source_id`, `source_offset`, `raw` (ganze Zeile, maskiert, komprimiert; `raw_text(raw)` entpackt) |
| `sources` | eingelesene Dateien und Lesestand; `events.source_id` verweist auf `id` (seit Schema v5) |
| `dirs` | Cache: Verzeichnis → Git-Root und Remote |

Wichtige Spalten in `events`: `seq` (Einfügereihenfolge, Primärschlüssel), `id`, `session_id`, `ts` (UTC) und `ts_local` (Ortszeit mit Offset, für Tagesgrenzen), `kind` (`message`, `tool_use`, `tool_result`, `system`, `meta`), `role`, `actor`, `text`, `tool_name`, `model`, `tokens_in`, `tokens_out`. Den Pfad der Quelldatei liefert `JOIN sources s ON s.id = e.source_id`; `source_offset` ist der Byte-Offset der Zeile darin. `raw` und `raw_truncated` beschreibt der nächste Abschnitt.

## `raw` { #raw }

`events.raw` ist seit Schema v5 ein **komprimierter Auszug** der Transkriptzeile, kein wortgetreues Abbild: ein zstd-BLOB mit eingebettetem Wörterbuch, darin nur die Felder, die das Programm zum erneuten Einlesen braucht (Typ, IDs, Zeitstempel, die Flags der Actor-Klassifikation, `message` mit `content` und `model`, `toolUseResult` ohne Diffs). Buchhaltung der Harness und alles, was schon in Spalten steht (Token-Zahlen, `cwd`, Session-ID), fehlt. Die Transkripte bleiben die Quelle; [`ingest --reread --rewrite-raw`](10-befehle.md#ingest) schreibt `raw` aus ihnen neu.

In SQL entpackt `raw_text(raw)`:

```sh
total-recall sql "SELECT id, raw_text(raw) FROM events WHERE id = 'claude:…' LIMIT 1"
```

`raw` ist `NULL`, wenn der Auszug größer als `ingest.raw_max_bytes` war (`raw_truncated = 1`) oder `ingest.store_raw = false` gilt. In Listenabfragen `raw` nie mitselektieren; [`status`](10-befehle.md#status) zeigt die Größe aller Rohzeilen und das Kompressionsverhältnis. Details: [Design](../explanation/design.md#raw).

`session_meta.raw` ist dagegen die **ganze** Zeile des Records, ebenso maskiert und komprimiert. Erst nach `type` und `session_id` filtern, dann wenige Zeilen entpacken, zum Beispiel die Pull Requests einer Session:

```sh
total-recall sql "SELECT DISTINCT json_extract(raw_text(raw), '$.prUrl') FROM session_meta WHERE type = 'pr-link' AND session_id = 'claude:…'"
```

Welche Record-Typen von Claude Code als Meta-Record landen und welche bewusst draußen bleiben, steht im [Design](../explanation/design.md#session_meta).

Die Schema-Version steht in `schema_version` und bei [`status`](10-befehle.md#status). Ein Programm-Update migriert nie von selbst; das macht [`migrate`](10-befehle.md#migrate) nach einem Backup, siehe [Aktualisieren](../how-to/10-installieren.md#aktualisieren).

Eine Fortsetzungskette liest sich über `continued_in`; die Gegenrichtung ist eine Abfrage: `SELECT id FROM sessions WHERE continued_in = 'claude:…'`. Sessions aus der Zeit vor Schema v4 haben die drei neuen Spalten erst nach [`ingest --all --reread`](10-befehle.md#ingest).

## Actor

Der `actor` sagt, wer etwas geschrieben hat:

| actor | Bedeutung |
|---|---|
| `human` | du hast es getippt |
| `agent` | der Agent hat es geschrieben |
| `peer` | ein anderer Agent (andere Session, Subagent) |
| `automation` | Automatik: geplante Prompts, Systemhinweise, Slash-Commands |
| `tool` | Ergebnis eines Tool-Aufrufs |

## Zeiten

Alle Zeiten stehen in **UTC** im Format `2026-10-31T22:21:58.123Z`. Für lokale Zeit nutze `datetime(ts, 'localtime')`. Das rechnet mit der Zeitzone deines Rechners.

Das vollständige Schema steht im [Design](../explanation/design.md#datenmodell).

!!! note "Spalten der Views"
    Die Spaltennamen der Views `exchanges` (`session_id`, `ts`, `request`, `response`, `request_id`, `response_id`, `response_ts`) und `daily_activity` (`day`, `session_id`, `repo`, `human_events`, `agent_events`) sind vorläufig. Maßgeblich ist das Schema in der Datenbank: `total-recall sql "SELECT sql FROM sqlite_master WHERE type = 'view'"`.


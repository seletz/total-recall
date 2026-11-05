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
| `sources` | eingelesene Dateien und Lesestand |
| `dirs` | Cache: Verzeichnis → Git-Root und Remote |

Wichtige Spalten in `events`: `session_id`, `ts`, `kind` (`message`, `tool_use`, `tool_result`, `system`, `meta`), `role`, `actor`, `text`, `tool_name`, `model`.

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
    Die Spaltennamen der Views `exchanges` (`session_id`, `ts`, `request`, `response`) und `daily_activity` (`day`, …) sind vorläufig. Maßgeblich ist das Schema in der Datenbank: `total-recall sql "SELECT sql FROM sqlite_master WHERE type = 'view'"`.


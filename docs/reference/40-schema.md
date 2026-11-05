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
| `sources` | eingelesene Dateien und Lesestand; `events.source_id` verweist auf `id` (seit Schema v5) |
| `dirs` | Cache: Verzeichnis → Git-Root und Remote |
| `blobs` | Bilder und Dokumente aus den Transkripten, einmal je Inhalt (SHA-256), seit Schema v6; siehe [unten](#blobs) |
| `event_blobs` | welche Events welche Blobs tragen, in welcher Reihenfolge |

Wichtige Spalten in `events`: `seq` (Einfügereihenfolge, Primärschlüssel), `id`, `session_id`, `ts` (UTC) und `ts_local` (Ortszeit mit Offset, für Tagesgrenzen), `kind` (`message`, `tool_use`, `tool_result`, `system`, `meta`), `role`, `actor`, `text`, `tool_name`, `model`, `tokens_in`, `tokens_out`. Den Pfad der Quelldatei liefert `JOIN sources s ON s.id = e.source_id`; `source_offset` ist der Byte-Offset der Zeile darin. `raw` und `raw_truncated` beschreibt der nächste Abschnitt.

## `raw` { #raw }

`events.raw` ist die **vollständige Transkriptzeile**, maskiert und als zstd-BLOB mit eingebettetem Wörterbuch komprimiert. Nur eines fehlt: die Daten von Bildern und Dokumenten. Sie liegen in [`blobs`](#blobs), und in der Zeile steht an ihrer Stelle `blob:sha256:<hex>`; aus `raw` und den Blobs lässt sich die Zeile Byte für Byte wiederherstellen. Von v1.2.0 bis v1.2.1 (Schema v5) war `raw` nur ein Auszug (ohne Buchhaltung der Harness, Token-Zahlen, Diffs), und Zeilen über 16 KiB fehlten; seit v1.3.0 ist die Zeile vollständig und unbegrenzt, denn das Archiv soll ohne die Transkripte auskommen, die Claude Code nach einigen Wochen löscht. Ältere Archive holen das mit [`ingest --all --reread --rewrite-raw`](10-befehle.md#ingest) nach, solange die Transkripte noch da sind.

In SQL entpackt `raw_text(raw)`:

```sh
total-recall sql "SELECT id, raw_text(raw) FROM events WHERE id = 'claude:…' LIMIT 1"
```

`raw` ist `NULL` mit `ingest.store_raw = false`, über `ingest.raw_max_bytes` (Default 0 = keine Grenze) oder bei Zeilen, die ein älteres Limit verworfen hat (`raw_truncated = 1`). In Listenabfragen `raw` nie mitselektieren; [`status`](10-befehle.md#status) zeigt die Größe aller Rohzeilen und das Kompressionsverhältnis. Details: [Design](../explanation/design.md#raw).

## `blobs` und `event_blobs` { #blobs }

Bilder, die du in eine Session einfügst, und Bilder oder Dokumente, die ein Werkzeug liefert, stehen im Transkript als Base64 in der Zeile. Seit Schema v6 (v1.3.0) nimmt total-recall sie heraus und speichert sie einmal je Inhalt in `blobs`:

| Spalte | Inhalt |
|---|---|
| `sha256` | SHA-256 der Daten als Hex, Primärschlüssel; derselbe Wert wie im Verweis in `raw` und bei [`show`](10-befehle.md#show) |
| `media_type` | `image/png`, `image/jpeg`, `application/pdf`, … |
| `bytes` | Größe der Daten, wie geliefert |
| `encoding` | leer: `data` ist wie geliefert (PNG, JPEG sind schon komprimiert); `zstd`: `data` ist zstd-komprimiert (PDF, Text) |
| `data` | die Daten; in Listenabfragen nie mitselektieren |

`event_blobs(event_id, position, sha256)` sagt, welche Events den Anhang tragen und in welcher Reihenfolge er in der Zeile stand. Dasselbe Bild zweimal (Claude Code wiederholt ein gelesenes Bild in der Zeile des Tool-Ergebnisses) ist ein Blob mit zwei Verweisen. Der Text des Events (`events.text`) trägt einen Platzhalter wie `[image image/png 84.3 KiB]`; die Daten selbst gibt [`blob`](10-befehle.md#blob) aus. Mit `ingest.store_blobs = false` bleiben die Verweise und Platzhalter, die Daten werden nicht gespeichert. Bilder werden **nicht maskiert** ([Datenschutz](../explanation/datenschutz.md)).

```sh
total-recall sql "SELECT e.id, b.media_type, b.bytes FROM event_blobs eb JOIN blobs b ON b.sha256 = eb.sha256 JOIN events e ON e.id = eb.event_id ORDER BY e.ts DESC LIMIT 20"
```

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


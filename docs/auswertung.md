# Auswertung

Drei Wege führen zu Antworten:

1. `stats` für fertige Kennzahlen.
2. `search` für die Volltextsuche.
3. `sql` für alles andere.

## Kennzahlen mit `stats`

Ein Monat im Überblick:

```sh
total-recall stats --month 2026-10
```

Du bekommst drei Tabellen:

- **je Tag:** Sessions, menschliche Nachrichten, Agenten-Events.
- **je Wochentag × Stunde:** wann du arbeitest (lokale Zeit).
- **je Repo:** wo du arbeitest.

Für Skripte:

```sh
total-recall stats --month 2026-10 --json > oktober.json
```

## Suchen mit `search`

```sh
# Wo ging es um goreleaser?
total-recall search goreleaser

# Nur was ich selbst geschrieben habe
total-recall search "iCloud" --actor human

# Eine Phrase, eingeschränkt auf ein Repo und einen Zeitraum
total-recall search --raw '"VACUUM INTO"' --repo github.com/seletz/total-recall --since 2026-10-01
```

Die Treffer zeigen Session, Zeit und einen Textausschnitt. Mit der Session-ID geht es weiter zu `show`.

## Eigene Abfragen mit `sql`

`sql` öffnet die Datenbank nur lesend. Du kannst also nichts kaputt machen.

### Das Wichtigste zum Schema

| Tabelle / View | Inhalt |
|---|---|
| `sessions` | eine Zeile je Session: Name, Repo, Branch, Start, Ende. `display_name` ist der beste Titel: eigener Titel (`name`), sonst KI-Titel (`ai_title`), sonst `agent_name` |
| `events` | eine Zeile je Nachricht, Tool-Aufruf oder Tool-Ergebnis |
| `events_fts` | Volltextindex über `events.text` |
| `exchanges` | je menschlicher Nachricht: die Anfrage und die letzte Antwort des Agenten |
| `daily_activity` | je Tag (lokale Zeit) und Session: Repo, Anzahl human-/agent-Events |
| `sources` | eingelesene Dateien und Lesestand |
| `dirs` | Cache: Verzeichnis → Git-Root und Remote |

Wichtige Spalten in `events`: `session_id`, `ts`, `kind` (`message`, `tool_use`, `tool_result`, `system`, `meta`), `role`, `actor`, `text`, `tool_name`, `model`.

Der `actor` sagt, wer etwas geschrieben hat:

| actor | Bedeutung |
|---|---|
| `human` | du hast es getippt |
| `agent` | der Agent hat es geschrieben |
| `peer` | ein anderer Agent (andere Session, Subagent) |
| `automation` | Automatik: geplante Prompts, Systemhinweise, Slash-Commands |
| `tool` | Ergebnis eines Tool-Aufrufs |

Alle Zeiten stehen in **UTC** im Format `2026-10-31T22:21:58.123Z`. Für lokale Zeit nutze `datetime(ts, 'localtime')`. Das rechnet mit der Zeitzone deines Rechners.

Das vollständige Schema steht im [Design](design.md#datenmodell).

### Beispiele

Sessions je Repo und Monat:

```sql
SELECT strftime('%Y-%m', started_at, 'localtime') AS monat,
       coalesce(git_remote_url, cwd)             AS repo,
       count(*)                                  AS sessions
FROM sessions
WHERE parent_id IS NULL
GROUP BY monat, repo
ORDER BY monat DESC, sessions DESC;
```

Deine Nachrichten je Tag:

```sql
SELECT date(ts, 'localtime') AS tag, count(*) AS nachrichten
FROM events
WHERE actor = 'human' AND kind = 'message'
GROUP BY tag
ORDER BY tag DESC
LIMIT 30;
```

Sessions, in denen ein Begriff vorkommt:

```sql
SELECT s.id, s.display_name, s.started_at, count(*) AS treffer
FROM events_fts f
JOIN events e   ON e.rowid = f.rowid
JOIN sessions s ON s.id = e.session_id
WHERE events_fts MATCH 'goreleaser'
GROUP BY s.id
ORDER BY s.started_at DESC;
```

Anfragen und Antworten einer Session:

```sql
SELECT ts, request, response
FROM exchanges
WHERE session_id = 'claude:7d1e4c20-0001-4000-8000-000000000001'
ORDER BY ts;
```

Wie lange liefen die Sessions der letzten Woche?

```sql
SELECT display_name,
       round((julianday(ended_at) - julianday(started_at)) * 24 * 60) AS minuten
FROM sessions
WHERE started_at >= strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-7 days')
ORDER BY minuten DESC;
```

Zu welchen Stunden arbeite ich?

```sql
SELECT strftime('%H', ts, 'localtime') AS stunde, count(*) AS nachrichten
FROM events
WHERE actor = 'human'
GROUP BY stunde
ORDER BY stunde;
```

Welche Tools nutzt der Agent am meisten?

```sql
SELECT tool_name, count(*) AS aufrufe
FROM events
WHERE kind = 'tool_use'
GROUP BY tool_name
ORDER BY aufrufe DESC
LIMIT 15;
```

Als JSON oder CSV weiterverarbeiten:

```sh
total-recall sql --json "SELECT * FROM daily_activity WHERE day >= '2026-10-01'" | jq .
```

!!! note "Spalten der Views"
    Die Spaltennamen der Views `exchanges` (`session_id`, `ts`, `request`, `response`) und `daily_activity` (`day`, …) sind vorläufig. Maßgeblich ist das Schema in der Datenbank: `total-recall sql "SELECT sql FROM sqlite_master WHERE type = 'view'"`.

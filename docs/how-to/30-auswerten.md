---
title: Archiv auswerten
icon: lucide/chart-column
---
# Archiv auswerten

Drei Wege führen zu Antworten:

1. `stats` für fertige Kennzahlen.
2. `search` für die Volltextsuche.
3. `sql` für alles andere.

Dasselbe interaktiv zeigt die [TUI](../reference/30-tui.md); Grafiken für einen Bericht zeichnet [`chart`](47-monatsbericht.md).

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

Als Zeitreihe, je Woche und Projekt (Details: [`stats --period`](../reference/10-befehle.md#stats-period)):

```sh
total-recall stats --period week --by project --since 2026-08-01
total-recall stats --period month --by agent --top 5 --json
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

`sql` öffnet die Datenbank nur lesend. Du kannst also nichts kaputt machen. Tabellen, Views, der `actor` und die Zeitangaben stehen in der Referenz unter [Schema](../reference/40-schema.md); das vollständige Schema in der Spezifikation unter [Datenmodell](../explanation/design.md#datenmodell).

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

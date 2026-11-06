---
title: Archiv per MCP anbinden
icon: lucide/plug
---
# Archiv per MCP anbinden

Neben der CLI bietet `total-recall` einen lokalen **MCP-Server** an. Der Agent ruft das Archiv dann über Tools auf (`search`, `show`, …) statt über Shell-Befehle. Die Tools liefern dieselben JSON-Daten wie die CLI.

Der Server läuft **nur lesend** und **nur lokal**: Claude Code startet `total-recall mcp` beim Beginn der Session als Unterprozess und spricht über stdin/stdout mit ihm (Transport stdio). Es gibt keinen Port und keinen Daemon; mit der Session endet der Prozess.

## Eintragen

`total-recall` schreibt nichts in die Einstellungen von Claude Code. Du trägst den Server selbst ein, am einfachsten mit dem Befehl von Claude Code:

```sh
claude mcp add --scope user total-recall -- total-recall mcp
```

`total-recall mcp print --command` gibt genau diese Zeile aus, mit `--binary PFAD` für einen absoluten Pfad. Als JSON für die `.mcp.json` eines Projekts (oder den Schlüssel `mcpServers` in `~/.claude.json`) gibt [`total-recall mcp print`](../reference/10-befehle.md#mcp) den Eintrag aus:

```json
{
  "mcpServers": {
    "total-recall": {
      "type": "stdio",
      "command": "total-recall",
      "args": ["mcp"]
    }
  }
}
```

Startet Claude Code mit einem anderen `PATH` als deine Shell, nimm den absoluten Pfad: `total-recall mcp print --binary "$(command -v total-recall)"`. Die Optionen `--config`, `--db` und `--tz` kommen in `args` vor `mcp`, falls der Server ein anderes Archiv lesen soll.

Die Seite **setup** der [TUI](../reference/30-tui.md#setup) hat dafür die Zeile `mcp`. Sie liest den Schlüssel `mcpServers` aus `~/.claude.json` (bzw. `$CLAUDE_CONFIG_DIR/.claude.json`) und aus der `.mcp.json` des Verzeichnisses, in dem die TUI läuft, und prüft, dass der Eintrag auf ein vorhandenes `total-recall` zeigt. Alles andere in diesen Dateien wird weder gelesen noch angezeigt, und sie werden nie geschrieben. Ist die Zeile markiert, zeigt die Seite den Befehl und den JSON-Eintrag, ++c++ kopiert den Befehl.

Prüfen in Claude Code: `/mcp` listet den Server `total-recall` mit seinen Tools.

## Die Tools

Alle Tools sind schreibgeschützt und geben das JSON des gleichnamigen Befehls zurück, so wie `--json` es ausgibt ([Befehle](../reference/10-befehle.md)). Die Parameter sind die Flags des Befehls, `by_session` ist `--by-session`.

| Tool | Befehl | Parameter (`*` = Pflicht) |
|---|---|---|
| `search` | [`search`](../reference/10-befehle.md#search) | `query*`, `type`, `project`, `actor`, `session`, `since`, `until`, `repo`, `harness`, `by_session`, `order`, `raw`, `limit` (Default 20, höchstens 200; je Typ) |
| `sessions` | [`sessions`](../reference/10-befehle.md#sessions) | `since`, `until`, `repo`, `harness`, `name`, `account`, `subagents`, `limit` (Default 50, höchstens 500) |
| `show` | [`show`](../reference/10-befehle.md#show) | `session*`, `since`, `until`, `from_event`, `offset`, `all`, `limit` (Default 200, höchstens 1000) |
| `stats` | [`stats`](../reference/10-befehle.md#stats) | `month`, `since`, `until`, `period`, `by`, `top`, `workday`, `worktime`, `sessions`, `summary`, `range`, `all_events` |
| `memory` | [`memory`](../reference/10-befehle.md#memory) | `project`, `at`, `history`, `grep`, `deleted`, `raw`, `limit` |
| `status` | [`status`](../reference/10-befehle.md#status) | keine |

`type` ist wie `--type` ([`search`](../reference/10-befehle.md#search)): `event` (Default), `session` oder `memory`, als Array oder kommagetrennte Liste. Die Treffer kommen nach Typ gruppiert in der angegebenen Reihenfolge, **`limit` gilt je Typ**, und jeder Treffer hat das Feld `type`. `project` ist `--project`.

Es gibt bewusst keine Tools zum Schreiben, Migrieren, Sichern oder für `sql`: Wer gruppieren, zählen oder verknüpfen will, nimmt dafür die CLI (`total-recall sql`). Der Skill (`total-recall llm --skill`) sagt dem Agenten, wann er welchen Weg nimmt.

## Begrenzte Ausgabe

Ein Tool-Ergebnis ist höchstens 64 KiB groß. Der erste Inhalt ist **immer genau die CLI-Ausgabe**. Ist sie größer, wird die Liste (oder bei `show` die Liste `events`) nach ganzen Einträgen gekürzt und bleibt gültiges JSON. Dann steht in einem zweiten Text-Inhalt, was passiert ist:

```json
{"truncated":true,"next":{"offset":120},"hint":"output cut after 120 of 200 items; call again with offset=120 to continue, or narrow the window (since, until, limit)"}
```

`next.offset` gibt es dort, wo das Tool `offset` kennt (`show`). Bei den anderen Tools engt der Agent mit `since`, `until` oder einem kleineren `limit` ein. Die Zeilenzahl begrenzen außerdem `limit` und sein Maximum; `show` ohne `limit` liefert höchstens 200 Ereignisse. `stats` ist ein Objekt und lässt sich nicht kürzen; ein sehr großes Ergebnis kommt unverändert zurück, dann hilft ein engerer Zeitraum.

## Fehler

Ein fehlgeschlagener Aufruf trägt `isError: true` und das Fehlerobjekt der CLI als Text, `{"error":{"kind":"not_found","message":"…"}}`, mit denselben `kind`-Werten (`usage`, `db`, `sql`, `not_found`, `ambiguous`, …). Ungültige Parameter sind ein `usage`-Fehler.

- **Archiv fehlt oder hat ein älteres Schema.** Der Server öffnet das Archiv bei **jedem Aufruf neu**, schreibgeschützt. Ein älteres Schema (nach einem Update des Binarys, vor `total-recall migrate`) gibt, wo eine Abfrage eine neue Tabelle braucht, einen Fehler mit dem Hinweis auf `migrate`; `status` zeigt `migration_pending`. Der Server migriert nie selbst ([Aktualisieren](10-installieren.md#aktualisieren)).
- **`maintenance` oder `migrate` tauscht die Datei.** Weil jeder Aufruf neu öffnet, braucht der Server keinen Neustart; läuft gerade der Austausch, kommt ein Fehler, und der nächste Aufruf klappt.
- **Update des Binarys.** Claude Code startet den Server beim Beginn einer Session; eine laufende Session hält die alte Version, bis sie neu startet.

## Datenschutz

Was ein Tool zurückgibt, steht danach im Kontext der Session und geht an den Modellanbieter der Harness. Mit dem Server kann **jede** Session, in der er eingetragen ist, das ganze Archiv lesen, auch Sessions aus anderen Repositories. Das ist der Zweck, aber gut zu wissen: Willst du das für ein Repository nicht, trag den Server nicht im User-Scope, sondern nur in den `.mcp.json` der Projekte ein, in denen du ihn willst. Mehr dazu in [Datenschutz](../explanation/datenschutz.md#mcp).

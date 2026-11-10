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
| `search` | [`search`](../reference/10-befehle.md#search) | `query*`, `cursor`, `type`, `project`, `actor`, `session`, `since`, `until`, `repo`, `harness`, `by_session`, `order`, `raw`, `limit` (Default 20, höchstens 200; je Typ) |
| `sessions` | [`sessions`](../reference/10-befehle.md#sessions) | `cursor`, `since`, `until`, `repo`, `harness`, `name`, `account`, `subagents`, `limit` (Default 50, höchstens 500) |
| `show` | [`show`](../reference/10-befehle.md#show) | `session*`, `cursor`, `since`, `until`, `from_event`, `offset`, `all`, `limit` (Default 200, höchstens 1000) |
| `stats` | [`stats`](../reference/10-befehle.md#stats) | `cursor`, `month`, `since`, `until`, `period`, `by`, `top`, `tools`, `path`, `path_in`, `workday`, `blocks`, `worktime`, `sessions`, `summary`, `range`, `all_events` |
| `memory` | [`memory`](../reference/10-befehle.md#memory) | `cursor`, `project`, `at`, `history`, `grep`, `deleted`, `raw`, `limit` |
| `status` | [`status`](../reference/10-befehle.md#status) | keine |
| `timeline` | [`timeline`](../reference/10-befehle.md#timeline) | `cursor`, `since`, `until`, `project`, `repo`, `actor` (Default `human`, oder `all`; mit `all=true` ohne `actor` gilt `all`), `all`, `text_width` (Default 160), `fields` (Default die kompakten Schlüssel ohne `ts`, `session_name`, `repo`, `text_chars`; `all` = alle), `limit` (ohne Angabe alle; große Fenster kommen in Seiten) |

`type` ist wie `--type` ([`search`](../reference/10-befehle.md#search)): `event` (Default), `session` oder `memory`, als Array oder kommagetrennte Liste. Die Treffer kommen nach Typ gruppiert in der angegebenen Reihenfolge, **`limit` gilt je Typ**, und jeder Treffer hat das Feld `type`. `project` ist `--project`.

Es gibt bewusst keine Tools zum Schreiben, Migrieren, Sichern oder für `sql`: Wer gruppieren, zählen oder verknüpfen will, nimmt dafür die CLI (`total-recall sql`). Der Skill (`total-recall llm --skill`) sagt dem Agenten, wann er welchen Weg nimmt.

## Seiten statt Kürzung { #seiten }

Der erste Inhalt eines Tool-Ergebnisses ist höchstens **32 KiB** groß, bei **jedem** Tool. Die Grenze liegt mit Abstand unter dem, was Clients annehmen: Claude Code begrenzt MCP-Antworten auf etwa 25 000 Tokens, und JSON mit IDs und Pfaden kostet bis zu etwa 2 Bytes je Token, 32 KiB sind also im ungünstigen Fall rund 16 000 Tokens.

Der Grundsatz: **Die Zahlen müssen stimmen.** Ein Tool lässt nie still Teile einer Liste weg, über die eine Summe gebildet wird. Ist die CLI-Ausgabe größer als die Grenze, kommt sie in **Seiten**, und der Agent holt den Rest mit Folgeaufrufen. Passt sie, ändert sich nichts: Der erste Inhalt ist die CLI-Ausgabe, ein zweiter fehlt.

- **Eine Seite** ist gültiges CLI-JSON mit denselben Feldern. Sie enthält **ganze Einträge** einer Liste, der Seitenliste: `days` bei `stats` mit `worktime` oder `workday`, `buckets` bei `period`/`by`, `groups` bei `tools`, sonst `by_day`; `events` bei `show`; die Zeilen bei `search`, `sessions` und `memory`. Bei `stats` mit Arbeitszeit ist ein Tag also nie mitten in seinen Kategorien geteilt: Jeder Tag kommt mit allen Kategorien, Projekten, Referenzen und Sessions, bei `workday` mit `blocks` mit allen Blöcken: Die Blöcke eines Tags bleiben zusammen, und `block_seconds` plus `break_seconds` ergeben auf jeder Seite die Spanne des Tags (die Summen im Kopf stehen auf jeder Seite). Bei `worktime` steht die kleine Liste `uncategorized` (Projekte ohne Kategorie) wie die Summen unverändert auf jeder Seite.
- **Die Eingabeart** (`input_kind` bei `show`, `search` und `timeline`, seit v1.6.0) steht in jedem Event der Seite. Über alle Seiten ergeben `typed`, `pasted`, `bash`, `remote` und `null` bei menschlichen Events genau deren Anzahl; Events anderer Actor haben immer `null`.
- **Alles andere steht vollständig auf jeder Seite**, vor allem `total` und die Summen je Kategorie über den ganzen Zeitraum. Auf jeder Seite ist ein Tag die Summe seiner Kategorien, und die Summe der Tage aller Seiten ist der Gesamtwert.
- **Alle Seiten zusammen** sind byte-gleich die kompakte CLI-Ausgabe: Seite 1 mit den Einträgen der Seitenliste aller Seiten.

Der zweite Inhalt sagt, wo man ist und wie es weitergeht. **`page`, `next` und `hint` stehen im zweiten Content-Block des Ergebnisses, nicht im Daten-JSON des ersten**; im ersten ist nur das CLI-JSON, in dem es diese Felder nicht gibt. Die Beschreibung jedes Tools in `tools/list` sagt das ausdrücklich. Der zweite Block ist ein eigener Text mit diesem JSON:

```json
{"page":{"index":2,"of":5,"unit":"days","from":8,"to":16,"total":31},"next":{"cursor":"16-9dc0292a4f5d"},"hint":"page 2 of 5 (days 9 to 16 of 31); totals and all other fields are complete on every page; call again with cursor=\"16-9dc0292a4f5d\" for the next page; or narrow the query first: …"}
```

`from` und `to` zählen ab 0 (`[from, to)`), `total` ist die Zahl der Einträge der Seitenliste. Für die nächste Seite ruft der Agent **dasselbe Tool mit denselben Argumenten** und zusätzlich `cursor` = `next.cursor` auf, unverändert; die letzte Seite hat kein `next`. Der Cursor ist ein undurchsichtiger Text. Er hält keinen Zustand im Server: Jeder Aufruf führt den Befehl neu aus und teilt wieder auf. Hat sich das Archiv zwischen zwei Seiten geändert, beginnt der Agent ohne `cursor` von vorn, statt Seiten zu mischen, die nicht zusammenpassen. Ein Cursor, der nicht passt, ist immer ein `usage`-Fehler, und die Meldung sagt, warum:

| Meldung | Bedeutung | Was tun |
|---|---|---|
| `invalid cursor` | der Text ist kein Cursor oder nicht der einer Seite dieses Ergebnisses | `next.cursor` unverändert übergeben |
| `cursor belongs to a different query` | die anderen Argumente (`since`, `month`, `limit`, …) sind nicht die des Aufrufs, der den Cursor ausgegeben hat, oder es ist ein anderes Tool | dieselben Argumente nehmen, oder ohne `cursor` neu beginnen |
| `the result changed since this cursor was issued` | gleiche Argumente, aber das Archiv hat sich zwischen den Seiten geändert | ohne `cursor` neu beginnen |

Dazu trägt der Cursor einen Hash der Argumente (ohne `cursor`) und einen des Ergebnisses neben dem Eintrag, bei dem die Seite beginnt. Ein `cursor` bei einem Ergebnis ohne Seiten ist ebenfalls ein `usage`-Fehler (`this result has no pages`), bei `status` ein unbekanntes Argument.

**Vorab eingrenzen** spart Seiten: Jede Seite trägt die Summen und alle anderen Felder neu, mehr Seiten kosten also mehr Tokens. Der Hinweis nennt, wie man eingrenzt: ein kleinerer Zeitraum (`month`, `since`, `until`, `range`), `sessions` oder `summary` weglassen, `period`/`by` mit `top`, ein kleineres `limit`. Wer nur Summen braucht, kommt damit oft mit einer Seite aus.

**Tool-Nutzung.** `stats` mit `tools: true` liefert dasselbe JSON wie [`stats --tools`](../reference/10-befehle.md#stats-tools); `by` ist dann `day`, `session`, `repo` oder `project`, `top` fasst die kleinsten Gruppen in `other` zusammen, ohne `by` die am seltensten genutzten Tools in `by_tool`, `path` filtert auf die Argumente der Aufrufe (Datei-Tools: ganzer Pfad, Bash: irgendwo im Befehl; Grenzen in der Befehlsreferenz), standardmäßig nur bei Tools mit Pfad oder Befehl; `path_in: "all"` prüft jeden Aufruf, auch Nachrichten und Prompts. Die Seitenliste ist `groups`; `total` und `by_tool` stehen auf jeder Seite, und die `count`-Werte der Gruppen aller Seiten ergeben `total`.

**Die eine Ausnahme.** Ein einzelner Eintrag der Seitenliste, der allein über der Grenze liegt (zum Beispiel ein Tag mit sehr vielen Sessions und Referenzen), lässt sich nicht teilen, ohne seine Kategorien zu zerreißen. Seine Seite verliert dann als Letztes nur **Detail-Listen** (bei `stats` die `refs` und `sessions`, die jeweils größte zuerst) und lange Texte; Listen, die Summen tragen (`categories`, `projects`, `blocks`, `buckets`, `by_day`), und alle Zahlen bleiben unberührt. Der Hinweis sagt, was fehlt: `"truncated":true` und `cut` mit Pfad, Zahl der Listen und den Einträgen, die blieben (ggf. `shortened_strings`). Reicht das nicht, ist das Ergebnis ein `usage`-Fehler mit dem Hinweis zum Eingrenzen (kürzerer Zeitraum, `sessions` oder `summary` weglassen), nie eine falsche Teilsumme.

**Umstellen von v1.4.** Bis v1.4.0 kürzte der Server und meldete `next.offset`. Das ist mit v1.5.0 weg: `next.cursor` ersetzt es bei allen Tools, auch bei `show`; die Schritte stehen im [Changelog](../reference/90-aenderungen.md#bruch).

## Fehler

Ein fehlgeschlagener Aufruf trägt `isError: true` und das Fehlerobjekt der CLI als Text, `{"error":{"kind":"not_found","message":"…"}}`, mit denselben `kind`-Werten (`usage`, `db`, `sql`, `not_found`, `ambiguous`, …). Ungültige Parameter sind ein `usage`-Fehler.

- **Archiv fehlt oder hat ein älteres Schema.** Der Server öffnet das Archiv bei **jedem Aufruf neu**, schreibgeschützt. Ein älteres Schema (nach einem Update des Binarys, vor `total-recall migrate`) gibt, wo eine Abfrage eine neue Tabelle braucht, einen Fehler mit dem Hinweis auf `migrate`; `status` zeigt `migration_pending`. Der Server migriert nie selbst ([Aktualisieren](10-installieren.md#aktualisieren)).
- **`maintenance` oder `migrate` tauscht die Datei.** Weil jeder Aufruf neu öffnet, braucht der Server keinen Neustart; läuft gerade der Austausch, kommt ein Fehler, und der nächste Aufruf klappt.
- **Update des Binarys.** Claude Code startet den Server beim Beginn einer Session; eine laufende Session hält die alte Version, bis sie neu startet.

## Datenschutz

Was ein Tool zurückgibt, steht danach im Kontext der Session und geht an den Modellanbieter der Harness. Mit dem Server kann **jede** Session, in der er eingetragen ist, das ganze Archiv lesen, auch Sessions aus anderen Repositories. Das ist der Zweck, aber gut zu wissen: Willst du das für ein Repository nicht, trag den Server nicht im User-Scope, sondern nur in den `.mcp.json` der Projekte ein, in denen du ihn willst. Mehr dazu in [Datenschutz](../explanation/datenschutz.md#mcp).

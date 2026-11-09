---
title: Änderungen
icon: lucide/history
---
# Änderungen

Was sich je Version geändert hat, aus Sicht der Benutzer. Die vollständige Liste der Commits steht bei jedem [Release auf GitHub](https://github.com/seletz/total-recall/releases); der Abschnitt zur jeweiligen Version dieser Seite bildet dort den Kopf der Release Notes.

## v1.6.0 (unveröffentlicht)

### Tool-Nutzung in `stats`

- **`stats --tools`** ([Befehle](10-befehle.md#stats-tools), #239): Zahl der `tool_use`-Events je Tool, gruppierbar mit `--by day|session|repo|project` und `--top N`. Die Gruppen ergeben immer die Gesamtzahl. `--path GLOB` filtert auf die Argumente der Aufrufe (Dateipfad der Datei-Tools als Ganzes, Bash-Befehl an beliebiger Stelle); die Grenzen der Pfaderkennung stehen in der Befehlsreferenz. Neue Flags und neue JSON-Felder, nichts Bestehendes ändert sich.
- **MCP:** das Tool `stats` hat die Parameter `tools` und `path`, `by` dazu die Werte `day`, `session` und `repo` ([MCP anbinden](../how-to/55-mcp-anbinden.md)). Das Ergebnis kommt wie alle großen Ergebnisse seitenweise (Seitenliste `groups`), `total` und `by_tool` stehen auf jeder Seite.

## v1.5.0 (unveröffentlicht)

### Bruch

**Die Ergebnisse der MCP-Tools werden nicht mehr gekürzt, sondern in Seiten geliefert** ([#230](https://github.com/seletz/total-recall/issues/230)). Dieser Bruch kommt ausnahmsweise in einer Minor-Version, weil es noch keine externen Nutzer des MCP-Servers gibt (wie bei v1.3.0). Seit v1.4.0 ([#226](https://github.com/seletz/total-recall/issues/226)) schnitt jedes Tool bei 32 KiB Listen ab; bei `stats` mit `worktime` und `sessions` für einen Monat blieben so 62 von 66 Kategorien und 37 von 117 Projekten übrig, und die Summen passten nicht mehr zu ihren Teilen. Ein MCP-Tool lässt jetzt nie mehr still Teile einer Liste weg, über die eine Summe gebildet wird.

Was sich an den Ergebnissen ändert:

- Ist das Ergebnis größer als 32 KiB, ist der erste Inhalt **eine Seite**: gültiges CLI-JSON mit denselben Feldern, aber nur mit einem Teil der Seitenliste (`days` bei `stats` mit `worktime` oder `workday`, `buckets` bei `period`/`by`, sonst `by_day`; `events` bei `show`; die Zeilen bei `search`, `sessions`, `memory`). Eine Seite enthält ganze Einträge: bei `stats` ganze Tage mit allen Kategorien, Projekten, Referenzen und Sessions.
- **Alles andere steht vollständig auf jeder Seite**, vor allem `total` und die Summen je Kategorie über den Zeitraum. Alle Seiten zusammen sind byte-gleich die kompakte CLI-Ausgabe.
- Der zweite Inhalt ist der Hinweis `{"page":{"index":2,"of":5,"unit":"days","from":8,"to":16,"total":31},"next":{"cursor":"…"},"hint":"…"}`. Die letzte Seite hat kein `next`.
- **`next.offset` entfällt.** Auch `show` meldet nur noch `next.cursor`. Der Parameter `offset` von `show` bleibt, er wählt wie `--offset` das Fenster der CLI.
- **Neuer Parameter `cursor`** bei `search`, `sessions`, `show`, `stats` und `memory`. `truncated` steht nur noch im Hinweis, wenn wirklich etwas fehlt (siehe unten); `cut` und `shortened_strings` gibt es nur noch dann.
- Nur ein **einzelner Eintrag**, der allein über 32 KiB liegt (zum Beispiel ein Tag mit sehr vielen Sessions), verliert noch Einträge der Detail-Listen `refs` und `sessions` und lange Texte; `categories`, `projects` und alle Zahlen bleiben vollständig. Der Hinweis hat dann `truncated: true` und `cut`. Reicht das nicht, kommt ein `usage`-Fehler mit dem Hinweis, den Zeitraum zu verkleinern oder `sessions`/`summary` wegzulassen, statt einer falschen Teilsumme.

**So stellt ein Agent um:**

1. Bei jedem Ergebnis den zweiten Inhalt lesen. Hat er `next`, ist das Ergebnis nicht vollständig.
2. Dasselbe Tool mit **denselben Argumenten** und zusätzlich `cursor` = `next.cursor` (unverändert) aufrufen, bis `next` fehlt.
3. Summen über Seiten nie aus den Seitensummen bilden, sondern aus den Einträgen der Seitenliste (`total` steht ohnehin auf jeder Seite vollständig).
4. Ein Cursor-Fehler (`usage`, „the result changed …“) heißt: Das Archiv hat sich zwischen zwei Seiten geändert. Ohne `cursor` neu beginnen.
5. Wer `offset` aus `next.offset` von `show` weitergegeben hat, nimmt jetzt `cursor`.

Die Seiten sparen keine Tokens: Jede trägt die Summen und alle anderen Felder neu. Wer nur die Summen braucht, grenzt vorher ein (kleinerer Zeitraum, `sessions`/`summary` aus) und kommt oft mit einer Seite aus. Die Anleitung für Agenten (`total-recall llm --skill`) und das [How-to](../how-to/55-mcp-anbinden.md#seiten) beschreiben das neue Verhalten.

### Memory in der TUI

- **Memory-Viewer:** ++enter++ auf einem Memory-Treffer der Suche öffnet das Gedächtnis des Projekts ([Memory](30-tui.md#memory), #193): links die Dateien mit `MEMORY.md` als Index, gelöschte zuletzt, rechts der Inhalt als lesbares Markdown, die Datei des Treffers markiert. ++esc++ führt zur Suche zurück.
- **Verlauf je Datei:** ++enter++ auf einer Datei listet ihre Versionen mit Zeitpunkt und Größe, eine Löschung als eigene Zeile, und zeigt einen früheren Stand. Maus und Tasten wie im Rest der TUI; die Tasten sind als `memory_*` in `[keys]` änderbar.
- **Diff:** `d` im Verlauf zeigt den Zeilendiff gegen die Version davor, hinzugefügte Zeilen grün, entfernte rot; mit `c` wird eine Basis gemerkt, so lassen sich zwei beliebige Versionen vergleichen (#194).
- **Zeitleiste:** `t` zeigt je Projekt, was wann hinzukam, sich änderte oder gelöscht wurde, mit dem Diff der markierten Änderung rechts; ++enter++ springt in den Verlauf der Datei. Gilt ab dem Zeitpunkt, ab dem total-recall das Gedächtnis archiviert.
- **Demo-Archiv:** das erfundene Gedächtnis von `kompass` hat jetzt mehrere Versionen, einen wechselnden Index und eine gelöschte Datei, damit Verlauf und Screenshots etwas zu zeigen haben.

## v1.4.0

### Was dieses Update mit dem Archiv macht

Dieses Release bringt die Schemas **v8** und **v9**. v8 ist eine kurze Migration ohne Datenbewegung, v9 füllt neue Spalten aus den Rohzeilen (Sekunden, siehe unten). Neu im Archiv ist das **Projekt-Gedächtnis** von Claude Code: die Markdown-Dateien, die der Agent je Projekt unter `~/.claude/projects/<projekt>/memory/` anlegt, ändert und löscht. Bisher standen sie nirgends im Archiv; wer wissen wollte, was der Agent sich zu einem Projekt gemerkt hatte, sah nur den Stand auf der Platte.

- **Was gespeichert wird:** jede Fassung jeder Gedächtnis-Datei, **maskiert** wie der Text der Events, als Blob einmal je Inhalt; eine Löschung als eigener Eintrag. Nichts davon ist ein Event: Suche, Statistik und Arbeitszeit bleiben, wie sie sind.
- **Ab wann:** ab dem ersten `ingest --all` (oder Hook) nach der Migration. Frühere Stände gibt es nicht, auch nicht aus den Transkripten.
- **Datenschutz:** Das Gedächtnis enthält oft persönliche Notizen über dich, deine Arbeit und andere Menschen. Was der Agent löscht, bleibt im Archiv und in den Backups. Abschaltbar mit `ingest.store_memory = false` ([Datenschutz](../explanation/datenschutz.md#gedaechtnis)).

**Was das kostet**, gemessen auf einer Kopie eines echten Archivs (Stand v7, 803 MB, 35 Projekte mit Gedächtnis, 905 Dateien, 1,9 MiB):

| | `migrate` | erster `ingest --all` | Lauf ohne Änderung |
|---|---|---|---|
| Dauer | 0,9 s | 0,63 s für das Gedächtnis | 10 ms je Hook |
| Datei | + 0,2 MB | + 4,8 MiB | |

Die Inhalte liegen komprimiert mit 1,1 MiB in `blobs`, den Rest belegt der Volltextindex mit seiner Kopie des Texts. In 42 der 905 Dateien hat die Maskierung etwas ersetzt. 11 der 35 Projekte haben keine Transkripte mehr und keine passende Session im Archiv; sie heißen in `memory` wie ihr Verzeichnis bei Claude Code, bis eine Session dafür auftaucht.

**Die Schritte** ([Aktualisieren](../how-to/10-installieren.md#aktualisieren)):

```sh
total-recall backup      # 1. sichern
total-recall migrate     # 2. Sekunden: legt die Tabellen für das Gedächtnis an und füllt die Token-Spalten
total-recall ingest --all  # 3. das Gedächtnis aufnehmen, wie es jetzt ist (sonst beim nächsten Hook)
```

Außerdem **teilt das Archiv die Eingabe-Token auf** ([#185](https://github.com/seletz/total-recall/issues/185)): `events.tokens_in` war die Summe aus frischer Eingabe, Cache-Schreiben und Cache-Lesen, in einem echten Archiv zu 97 % Cache-Lesen, und taugte so nicht für Kostenabschätzungen. Neu sind die Spalten `tokens_cache_read` und `tokens_cache_write` (Schema v9). `tokens_in` bleibt die Summe, bestehende Zahlen ändern sich nicht. `migrate` füllt die Spalten aus `usage` in `raw`; Events ohne Rohzeile bleiben `NULL` (unbekannt). Neue Events bekommen sie beim Ingest. `stats --json` hat die Aufteilung immer (Feld `tokens`, nur ein neues Feld), `stats --tokens` zeigt sie in der Tabelle ([Befehlsreferenz](10-befehle.md#stats-tokens)). Thinking-Tokens sind in `output_tokens` enthalten und nicht einzeln gespeichert.

**Was das kostet**, gemessen auf einer Kopie eines echten Archivs (Stand v7, 249 041 Events, 88 416 mit Token-Angaben):

| | `migrate` (v8 und v9) | davon v9 |
|---|---|---|
| Dauer | 4,8 s | 3,9 s |
| Datei | + 4 MB (786 MiB auf 790 MiB) | |

87 539 der 88 416 Events hatten eine Rohzeile mit `usage` und sind aufgeteilt; 877 bleiben `NULL` und zählen in `stats` als `unsplit`. Von 25,3 G Eingabe-Token waren 24,5 G Cache-Lesen, 0,54 G Cache-Schreiben und 1,7 M frische Eingabe.

Außerdem neu im Archiv sind die **Sicherungskopien bearbeiteter Dateien**: Bevor Claude Code eine Datei ändert, legt es eine Kopie unter `~/.claude/file-history` ab, und es löscht sie zusammen mit den Transkripten. Der normale Ingest liest sie jetzt mit und legt sie einmal je Inhalt als Blob ab; Textdateien werden dabei maskiert, Binärdateien folgen `ingest.store_blobs` ([#183](https://github.com/seletz/total-recall/issues/183), [Datenschutz](../explanation/datenschutz.md)). Wer das nicht will, setzt `ingest.store_file_history = false`.

Für den Bestand gilt dasselbe wie bei v1.3.0: Was Claude Code schon gelöscht hat, ist weg. Die Kopien, die noch da sind, holt ein Reread:

```sh
total-recall backup                               # 1. sichern
total-recall ingest --all --reread --rewrite-raw  # 2. Sicherungskopien aus den noch vorhandenen Transkripten nachtragen
total-recall maintenance                          # 3. Platz der ersetzten Zeilen freigeben
```

Schritt 2 lässt sich abbrechen und wiederholen. Ein späterer Reread verliert nichts, wenn die Kopien inzwischen gelöscht sind: Ein Meta-Record, dessen gespeicherte Zeile auf mehr Blobs verweist als die neue, bleibt, wie er ist (`N meta records kept` in der Zusammenfassung).

**Was die Datei-Historie kostet**, gemessen auf einer Kopie eines echten Archivs (Schema v7, 544 Sessions, 242 141 Events, 549 Transkripte):

| | vorher | nach Reread | nach `maintenance` |
|---|---|---|---|
| Datei | 762 MiB | 942 MiB | 914 MiB |
| Backup (`.db.zst`) | 476 MiB | | 596 MiB |
| Dauer | | 6 min 0 s | 8 s |

Dazu kamen 3 207 Sicherungskopien als Blobs, 199 MiB wie geliefert, 71 MiB gespeichert (zstd). 638 Kopien in 12 Transkripten waren schon gelöscht; ihre Records bleiben ohne Verweis, je Transkript steht eine Zeile im Log. Keine Kopie lag über der Grenze von 16 MiB. Die Zahlen enthalten auch, was seit dem letzten Ingest des Archivs neu war (345 Events, 1 648 Meta-Records).

### Grafiken

- **Neue Optik** ([#213](https://github.com/seletz/total-recall/issues/213)): `total-recall chart` zeichnet mit transparentem Hintergrund und mittelgrauem Text (`#8b8b8b`), kompakter, mit leicht gerundeten Ecken und Werten als Tooltip; die Grafiken passen in helle und dunkle Notizen. Neu in `[chart]`: `background`, `text`, `scale_orange`, `scale_blue`, `palette`; neu `--background`. `heatmap` ist jetzt ein optionaler Override (Default: leer, der Kalender nimmt `scale_orange`). Der Hintergrund ist nicht mehr weiß: wer ihn weiß will, setzt `background = "#ffffff"`.
- **`chart workday`** ([#214](https://github.com/seletz/total-recall/issues/214)): die Arbeitstage der TUI als Grafik, mit Kernzeit, Soll, Feiertagen und Überlauf nach Mitternacht; auch Teil von `chart all`.

### Agenten

- **MCP-Server `total-recall mcp`** ([#219](https://github.com/seletz/total-recall/issues/219)). Ein lokaler MCP-Server über stdio, den Claude Code als Unterprozess startet (kein Port, kein Daemon, nur lesend). Tools: `search` (mit `type` und `project` wie [`search`](10-befehle.md#search), `limit` gilt je Typ), `sessions`, `show`, `stats`, `memory`, `status`; jedes liefert die `--json`-Ausgabe des gleichnamigen Befehls, Fehler als dasselbe Fehlerobjekt mit `isError`. Die Ausgabe ist bei **jedem** Tool auf 32 KiB begrenzt (Claude Code nimmt etwa 25 000 Tokens an): Listen werden nach ganzen Einträgen gekürzt, bei Objekten wie `stats` zuerst die verschachtelten Listen (die Sessions je Tag und Kategorie), dann lange Texte; der erste Inhalt bleibt gültiges CLI-JSON, ein zweiter nennt die Kürzung (`cut`, `next.offset`) und wie man eingrenzt. Jeder Aufruf öffnet das Archiv neu, schreibgeschützt; ein älteres Schema meldet den Hinweis auf `migrate`, eine mit `maintenance` ausgetauschte Datei braucht keinen Neustart. Die Umsetzung ist klein und ohne neue Abhängigkeit. [`mcp print`](10-befehle.md#mcp) gibt den Eintrag aus (JSON oder die Zeile `claude mcp add`), die Seite Setup der TUI prüft ihn und zeigt ihn, geschrieben wird nichts. `llm --skill` beschreibt den Server. Die Tool-Namen und Parameter sind Teil der [stabilen Schnittstelle](60-versionierung.md). Anleitung: [Archiv per MCP anbinden](../how-to/55-mcp-anbinden.md), [Datenschutz](../explanation/datenschutz.md#mcp).

### Archiv

- **Datei-Historie von Claude Code** ([#183](https://github.com/seletz/total-recall/issues/183)). Die Records `file-history-snapshot` und `file-history-delta` verweisen jetzt mit `"blob":"blob:sha256:<hex>"` auf den Inhalt der Kopie, der einmal je Inhalt in `blobs` liegt (`text/plain` maskiert, sonst `application/octet-stream`), verknüpft über `meta_blobs` ([Schema](40-schema.md#session_meta)). Neuer Schlüssel `ingest.store_file_history` (Default `true`, [Konfiguration](20-konfiguration.md)). [`blob HASH`](10-befehle.md#blob) gibt eine Kopie aus, Text auch auf ein Terminal. [`status`](10-befehle.md#status) teilt die Blobs nach Art auf (`attachments`, `JSON values`, `files`; JSON `blob_kinds`), die TUI nennt die Kopien auf der Seite *status*.

- **[`memory`](10-befehle.md#memory)** zeigt das archivierte Projekt-Gedächtnis: je Projekt die Dateien, wie sie jetzt sind oder mit `--at DATUM` waren, mit `--deleted` auch gelöschte; `--history DATEI` listet alle Fassungen einer Datei, `--grep WÖRTER` sucht im aktuellen Inhalt (eigener Volltextindex, getrennt von `search`). `--project` schränkt auf Projekte ein, Namen wie bei `stats --by project`. Den Inhalt einer Fassung gibt [`blob`](10-befehle.md#blob) aus, `blob --json` nennt unter `memory` die Dateien, die ihn hatten ([#192](https://github.com/seletz/total-recall/issues/192)).
- [`status`](10-befehle.md#status) hat eine Zeile `memory` (Dateien, Projekte, Löschungen, Fassungen, Größe), JSON `memory_projects`, `memory_files`, `memory_deleted`, `memory_versions`, `memory_bytes`, `memory_stored_bytes`. Die [Ingest-Zusammenfassung](10-befehle.md#ingest) meldet `memory: N new versions, M deleted`, wenn sich etwas geändert hat.
- Neuer Schalter [`ingest.store_memory`](20-konfiguration.md#ingest) (Default `true`).

### Suche

- **Suche über Objekttypen** ([#196](https://github.com/seletz/total-recall/issues/196), Teil von [#195](https://github.com/seletz/total-recall/issues/195)). [`search --type T[,T…]`](10-befehle.md#search) sucht außer in Events (`event`, weiter der Default) auch in Sessions (`session`: Name, Titel, Agentenname) und im Projekt-Gedächtnis (`memory`: aktueller Inhalt, dieselben Dateien wie `memory --grep`). Die Treffer kommen nach Typ gruppiert in der Reihenfolge von `--type`; `--limit` gilt je Typ, und die Textausgabe meldet auf stderr, wie viele Treffer ein Typ hätte (`event: 20 of 134 hits`). `attachment`, `meta` und `file` sind als Namen reserviert und noch nicht durchsuchbar. Ein Filter, der für einen gewählten Typ nicht gilt (etwa `--actor` bei `memory`, `--raw` bei `session`), ist ein `usage`-Fehler. Ohne `--type` bleibt die Textausgabe, wie sie war.
- **`search --project P`** schränkt jeden Typ auf ein Projekt ein; der Name ist derselbe wie bei `stats --by project` (Definitionen `[[project]]` zuerst), sonst zählt ein Teil von Name, Pfad oder Remote-URL. Wirkt auch mit `--by-session`.
- **JSON:** Neue Felder an den Event-Treffern von `search --json`: `type` (immer `event`) und `project` (Projektname der Session, ein Subagent zählt zu seiner Eltern-Session; `null` ohne Verzeichnis). `rank` gab es schon. Die Treffer der Typen `session` und `memory` haben dieselben Kernfelder (`type`, `session_id`, `session_name`, `repo`, `project`, `ts`, `snippet`, `rank`) und eigene dazu, beschrieben in der [Befehlsreferenz](10-befehle.md#search). Die Zeilen von `--by-session` bleiben unverändert und haben kein `type`: Sie fassen Treffer zusammen, sie sind keine.
- Keine Migration: Jeder Typ sucht in seinem eigenen Index (`events_fts`, `memory_fts`, die Namensspalten von `sessions`). Gemessen auf einer Kopie eines echten Archivs (248 552 Events, 911 Gedächtnis-Dateien) war ein gemeinsamer Index nicht schneller (Top 20 über Events und Gedächtnis je 4–40 ms, getrennt ebenso), bei einem Typfilter langsamer (nur Gedächtnis: 0,1–0,5 ms getrennt, 3–30 ms gemeinsam) und hätte einen Neuaufbau als Migration (19 s, + 72 MB) gekostet.

### TUI

- **Suche über Objekttypen in der TUI** ([#197](https://github.com/seletz/total-recall/issues/197), Teil von [#195](https://github.com/seletz/total-recall/issues/195)). Die Suche hat einen **Typ-Umschalter** in der Linie unter der Eingabe: `event` (Default), `session` und `memory`, einzeln oder zusammen, per Klick oder mit `e`, `s`, `m` (`search_type_event`, `search_type_session`, `search_type_memory`). Mit mehreren Typen steht vor jeder Gruppe eine Kopfzeile mit der Zahl der Treffer, bei einem Treffer-Limit `200 of 1234`; jeder Typ hat sein eigenes Format (Event: Zeit, Session, Actor; Session: Name, Zahl der Events, Projekt; Memory: Projekt und Datei). Mit dem Default sieht die Liste aus wie bisher, nur die Linie trägt jetzt den Umschalter.
- **Projekt-Pin.** Rechts in derselben Linie steht `project: all`. `p` pinnt das Projekt des markierten Treffers (nochmal `p` hebt den Pin auf), `P` oder ein Klick auf das Wort öffnet die Liste der Projekte (`all projects` hebt auf), das `✕` löscht den Pin. Der Pin gilt für jeden Typ, mit demselben Projektnamen wie `search --project`. Filter, die ein Typ nicht anwendet (Actor bei Sessions und Memory, Rohsyntax bei Sessions), lässt die TUI für diesen Typ weg, statt einen Fehler zu zeigen.
- **Viewer in der Vorschau.** Die Vorschau wählt den Viewer nach dem Typ des markierten Treffers: den Austausch um eine Nachricht wie bisher; bei einem Tool-Aufruf oder einer Tool-Ausgabe **Eingabe und Ausgabe getrennt**, JSON eingerückt; bei einer Session den Kopf und die ersten Nachrichten; bei einer Memory-Datei den Inhalt als **lesbares Markdown** mit Projekt und Datei im Kopf. Die Viewer teilen eine Schnittstelle (`internal/tui/viewer`), in die spätere Viewer für Bilder und Diffs einhängen. Eine Session öffnet ++enter++ wie einen Event-Treffer; eine Memory-Datei hat noch keine Detailansicht.
- **Setup-Zeile `mcp`** ([#219](https://github.com/seletz/total-recall/issues/219)). Die Seite Setup der Ansicht Config prüft, ob der MCP-Server bei Claude Code eingetragen ist (Schlüssel `mcpServers` in `~/.claude.json` und `.mcp.json`, nur gelesen), und zeigt bei markierter Zeile den Befehl und den JSON-Eintrag; ++c++ kopiert den Befehl. Die Taste heißt jetzt „copy hook or MCP block“.
- **Projektnamen in der Projektfarbe** ([#210](https://github.com/seletz/total-recall/issues/210)). Die Seite Projects der Ansicht Config zeigt die Namen in Liste und Formular in derselben Farbe wie Statistik und Aktivität: die konfigurierte Farbe, sonst die automatische. Die markierte Zeile hat ihren Hintergrund jetzt über die ganze Breite, und zu dunkle oder zu helle Farben bleiben auf ihm lesbar. Ohne Farben (`NO_COLOR`) bleibt der Name normal.

### Behoben

- **Letztes Backup nach Zeitpunkt statt nach Dateiname** ([#209](https://github.com/seletz/total-recall/issues/209)). Lagen an einem Tag `archive-YYYY-MM-DD-1505.db.gz` (15:05) und ein späteres `archive-YYYY-MM-DD.db.zst` (22:03), galt das ältere als neuestes. `status`, Config-Seite, Setup, `backup --if-older` und die Aufbewahrung (die letzte Kopie eines Monats) bestimmen den Zeitpunkt jetzt einheitlich, in jedem Format: Datum und Uhrzeit aus dem Namen, ohne Uhrzeit im Namen die Änderungszeit der Datei. Liegt sie nicht auf dem Tag des Namens, etwa nach dem Kopieren auf einen neuen Rechner, zählt der Beginn dieses Tages. Dateien mit unmöglichem Datum im Namen (`archive-2026-13-01.db.zst`) gelten nicht mehr als Backup; die Aufbewahrung lässt sie liegen.
- **Text der Aktion `backup`** in der Config-Ansicht: Er nennt jetzt die Backups, und `keep_monthly = 0` heißt dort richtig `all monthly backups` statt `0 monthly`, denn `0` behält alle Monatskopien.
- **Markierte Zeile auf Settings, Actions und Setup** ([#216](https://github.com/seletz/total-recall/issues/216)). Der Hintergrund reißt hinter farbigem oder fettem Text nicht mehr ab, sondern läuft über die ganze Zeile, und farbige Schrift auf der Markierung bleibt lesbar.

## v1.3.1

### Behoben

- [`ingest`](10-befehle.md#ingest) meldet Meta-Records wieder richtig: Die Zusammenfassung zeigte immer `0 meta records`, die Records selbst wurden gespeichert ([#201](https://github.com/seletz/total-recall/issues/201)).
- `ingest --reread --rewrite-raw` zählt Zeilen der Tabellen statt Vorkommen im Transkript und trennt Events von Meta-Records: `N events and M meta records raw rewritten` statt `N raw rewritten`. Bisher zählte jeder Zustands-Record, den Claude Code jede Runde neu schreibt, bei jedem Vorkommen, auch wenn derselbe Lauf die Zeile eben erst angelegt hatte ([#201](https://github.com/seletz/total-recall/issues/201)).
- **`backup` ersetzt kein vorhandenes Backup mehr** ([#200](https://github.com/seletz/total-recall/issues/200)). Seit v1.3.0 löschte ein zweites Backup am selben Tag die `.db.gz` oder `.db` dieses Tages, frühere Versionen überschrieben die Kopie des Tages. Jetzt bleiben alle Kopien liegen: Hat der Tag schon eine, bekommt die neue die Uhrzeit in den Namen (`archive-YYYY-MM-DD-HHMM.db.zst`), in derselben Minute zusätzlich eine laufende Nummer. Das gilt auch für parallele Läufe und für das Backup von `maintenance`. `restore`, `status`, `backup --if-older`, die Config-Seite und das Setup der TUI kennen die neuen Namen.
- **Aufbewahrung nachvollziehbar.** Gelöscht wird nur noch über `keep_daily` und `keep_monthly`, die ältesten Kopien zuerst, und jede gelöschte Kopie steht als `backup: pruned …` im Log, auch beim Lauf von `maintenance`. `keep_daily` zählt jetzt Tage statt Dateien: Von den letzten sieben Tagen mit Backup bleiben alle Kopien, mehrere Backups an einem Tag verdrängen keine älteren Tage. Im Backup-Ordner können daher mehr Dateien liegen als `keep_daily` angibt.
- **Anonymisierte Kopie ohne Bilder und System-Prompts** ([#206](https://github.com/seletz/total-recall/issues/206)). `tools/anonymize` leerte seit v1.3.0 die Meta-Records, ließ aber `blobs` und `event_blobs` stehen: Eine anonymisierte Kopie enthielt weiter jeden Screenshot, jedes Dokument und jeden System-Prompt des Archivs. Jetzt sind alle Blobs und ihre Verknüpfungen in der Kopie leer. Das Archiv selbst war nie betroffen.

## v1.3.0

### Was dieses Update mit dem Archiv macht

Dieses Release bringt Schema **v7** (zwei kurze Migrationen, v6 und v7) und ändert, was im Archiv liegt. Grundsatz: Das Archiv soll ohne die Transkripte auskommen, die Claude Code nach einigen Wochen löscht; „lässt sich aus dem Transkript neu schreiben“ gilt nicht mehr als Sicherheitsnetz.

**Eine Ausnahme von der Regel, dass fertige Migrationen nicht geändert werden.** Migration 5 (v1.2.0) hat `raw` beim Umbau zum komprimierten BLOB auf einen Auszug entschlackt (ohne Buchhaltung der Harness, Token-Zahlen, Diffs). Dieser Schritt ist nachträglich entfernt worden: Migration 5 komprimiert jetzt nur noch. Das bricht die Forward-only-Regel einmal und mit Absicht, denn ein Archiv, das noch auf v4 steht, behält beim Update so alle Felder, statt sie erst zu verlieren und dann aus den Transkripten zurückzuholen. Es gibt noch keine fremden Benutzer, die ein v4-Archiv mit v1.2 migriert hätten; dokumentiert bleibt es trotzdem.

**Was je nach Ausgangslage passiert:**

- **Archiv auf v4** (bis v1.1.x): `migrate` komprimiert `raw` und behält alle Felder. Was damals wegen `raw_max_bytes` (16 KiB) nicht gespeichert wurde, holt der Reread aus den Transkripten.
- **Archiv auf v5** (v1.2.x): `raw` bleibt der Auszug von v1.2, bis `ingest --all --reread --rewrite-raw` gelaufen ist. Danach ist jede Zeile vollständig, deren Transkript noch da ist.
- **Transkript schon gelöscht:** Der Auszug (oder die leere Zeile) bleibt, wie er ist. Was weg ist, bleibt weg; deshalb den Reread bald nach dem Update ausführen (Claude Code löscht nach `cleanupPeriodDays`, standardmäßig 30 Tagen).

**Was neu gespeichert wird:**

- **Vollständige Rohzeilen**, ohne Größengrenze (`raw_max_bytes` ist jetzt 0): jede Transkriptzeile, wie sie war, maskiert und komprimiert ([#180](https://github.com/seletz/total-recall/issues/180)).
- **Bilder und Dokumente als Blobs**, einmal je Inhalt. Sie werden **nicht maskiert**, ein Screenshot mit einem Token darin liegt unverändert im Archiv; `ingest.store_blobs = false` verwirft sie ([#181](https://github.com/seletz/total-recall/issues/181), [Datenschutz](../explanation/datenschutz.md)).
- **Meta-Records**: Records, die keine Nachricht sind, aber zur Session gehören, darunter der **System-Prompt und die Tool-Definitionen** jeder Runde, die geladenen `CLAUDE.md`-Dateien, Hook-Ausgaben, während der Arbeit getippter Text, Modus, Pull Request, Kosten, Datei-Historie ([#182](https://github.com/seletz/total-recall/issues/182), [Datenschutz](../explanation/datenschutz.md)). System-Prompts und Tool-Definitionen liegen maskiert und einmal je Inhalt über alle Sessions als Blob; `store_blobs = false` lässt sie **nicht** aus, abschalten geht nur über `skip_record_types`.

**Was das kostet**, gemessen auf einer Kopie eines echten Archivs (Stand v5, 532 Sessions, 239 791 Events, 534 Transkripte, alle noch vorhanden):

| | vorher (v5) | nach `migrate` | nach Reread | nach `maintenance` |
|---|---|---|---|---|
| Datei | 453 MiB | 469 MiB | 854 MiB | 830 MiB |
| Backup (`.db.zst`) | 224 MiB | | | 526 MiB |
| Dauer | | 0,6 s | 4 min 36 s | 8 s |

Davon `raw` 271 MiB in 240 061 Events (komprimiert, Faktor 3,3), Bilder und Dokumente 904 Blobs mit 108 MiB, Meta-Records 49 MiB in 76 766 Zeilen, darin System-Prompts und Tool-Definitionen als 517 JSON-Blobs, 38 MiB roh, 13 MiB gespeichert (356 verschiedene Request-Formen aus 951 Records, 161 verschiedene System-Prompts und Tool-Listen aus 1 491 Snapshots). `migrate` braucht neben der Datenbank Platz für die Kopie `archive.db.pre-v5.bak` (566 MiB in dem Beispiel, es prüft das vorher); die Kopie kann weg, sobald alles läuft. Der Reread schreibt jede Zeile neu, die Datei wächst dabei bis `maintenance` auf ihre Endgröße plus Freiliste.

**Die Schritte, in dieser Reihenfolge** ([Aktualisieren](../how-to/10-installieren.md#aktualisieren)):

```sh
total-recall backup                               # 1. sichern
total-recall migrate                              # 2. Sekunden: legt vier Tabellen an
total-recall ingest --all --reread --rewrite-raw  # 3. Rohzeilen, Bilder und Meta-Records aus den Transkripten nachtragen
total-recall maintenance                          # 4. Platz der ersetzten Zeilen freigeben
```

Schritt 3 lässt sich abbrechen und wiederholen und funktioniert nur für Transkripte, die noch da sind; die Hooks füllen danach alles Neue von selbst.

Neu festgelegt ist, was bei total-recall im Sinne von Semantic Versioning stabil ist: Befehle, Flags, JSON-Ausgabe, Config-Schlüssel, Exit-Codes und Hooks. Schema, Views, `raw`, Backup-Format und TUI sind es nicht, und ein neues Schema ist eine Minor-Version, weil die Migration nur von Hand läuft; v1.2.0 hielt sich bereits daran. Die Seite [Versionierung](60-versionierung.md) sagt, was das für eigene Skripte heißt ([#190](https://github.com/seletz/total-recall/issues/190)).

### Archiv

- **Meta-Records (Schema v7).** Records der Harness, die keine Nachricht sind, aber zur Session gehören, landen in der neuen Tabelle `session_meta`: bei Claude Code der eingefügte Kontext (`attachment`: die geladenen `CLAUDE.md`-Dateien, @-erwähnte Dateien, Ausgaben der Hooks, der System-Prompt als `prompt_snapshot`, …), während der Arbeit getippter Text (`queue-operation`, auch entfernter), Modus und Pull Request (`pr-link`), Kosten je Modell (`cost-state`), System-Prompt und Tool-Definitionen je Request-Form (`api-request-shape`), der Index der Datei-Historie, das Workflow-Journal, und jeder Typ, den der Adapter nicht kennt. Je Record die ganze Zeile, maskiert und komprimiert wie `raw`; gleicher Inhalt je Session einmal; kein Event, nicht in Suche und Statistik. System-Prompts und Tool-Definitionen stehen nicht in der Zeile, sondern als JSON-Blob einmal je Inhalt über alle Sessions (`blobs`, `meta_blobs`; `blob HASH` gibt sie aus). Draußen bleiben der API-Request-Cache (`api-request`, `api-request-blob`: 1,25 GB auf einem lokalen Archiv, wiederholt nur die Nachrichten), `last-prompt` und sieben Attachment-Subtypen (Kataloge, Erinnerungen); `skip_record_types` nimmt jetzt auch `attachment/<subtyp>`. `status` zeigt Zahl und Größe (`meta records`), die Ingest-Zusammenfassung `N meta records`; die Zeile `blobs` zählt die JSON-Blobs mit. Messung und Entscheidung je Typ im [Design](../explanation/design.md#session_meta), was neu gespeichert wird unter [Datenschutz](../explanation/datenschutz.md) ([#182](https://github.com/seletz/total-recall/issues/182)).
- **Rohzeilen wieder vollständig, ohne Limit** ([#180](https://github.com/seletz/total-recall/issues/180)). `events.raw` ist die ganze Transkriptzeile, nicht mehr der Auszug von v1.2.0 (der Buchhaltung, Token-Zahlen und Diffs wegließ), und `ingest.raw_max_bytes` ist jetzt `0` (keine Grenze; bis v1.2 wurden Zeilen über 16 KiB ganz verworfen). Dafür gibt es ein zweites Kompressions-Wörterbuch, trainiert auf vollständigen Zeilen; Archive mit dem alten bleiben lesbar. Migration 5 beschneidet die Zeilen eines v4-Archivs nicht mehr.
- **Bilder und Dokumente als Blobs** ([#181](https://github.com/seletz/total-recall/issues/181)). Screenshots, die du in eine Session einfügst, und Bilder oder PDFs, die ein Werkzeug liest, kommen einmal je Inhalt (SHA-256) in die neue Tabelle `blobs`; in der Rohzeile steht an ihrer Stelle ein Verweis mit dem Hash, aus beidem lässt sich die Zeile Byte für Byte wiederherstellen ([Schema](40-schema.md#blobs)). PNG und JPEG liegen wie geliefert, PDF und Text zstd-komprimiert. `ingest.store_blobs = false` schaltet das ab. Bilder werden **nicht maskiert** ([Datenschutz](../explanation/datenschutz.md)).
- Eingefügte Bilder sind jetzt ein Event mit Platzhalter (`[image image/png 84.3 KiB]`), auch im Text eines Tool-Ergebnisses; frühere Versionen übersprangen diese Blöcke, `--reread` legt die Events nach.

### Behoben

- **Geheimnisse in JSON-Arrays wurden in `raw` nicht erkannt.** Ein String in einem Array hat keinen eigenen Schlüssel und bekam deshalb nur die Textregeln, nicht die Prüfung auf ein ganzes hochentropisches Token; ein Geheimnis ohne bekanntes Format hinter `"--api-key"` in einer Argumentliste blieb stehen. Jetzt gilt für Array-Elemente dieselbe Prüfung wie für Objektwerte, mit dem Schlüssel des Arrays: unter `"passwords": [...]` wird jedes Element maskiert, ein einzelnes hochentropisches Token ≥ 32 Zeichen wird `«redacted:high_entropy»` (nicht unter Bezeichner-Schlüsseln wie `messageIds`), und steht davor eine Option mit sensiblem Namen (`--token`, `--api-key`, `--password`, …), wird das Element auch dann `«redacted:secret»`, wenn es kurz ist. Prosa, Befehle, Pfade, Diffs, SHAs, UUIDs und Werkzeugnamen wie `mcp__server__tool` in Arrays bleiben erhalten; auf den Rohzeilen echter Transkripte änderte sich ein winziger Bruchteil der Array-Elemente, im gespeicherten Auszug praktisch keines. `text` war nie betroffen. Bestehende Archive bekommen die neu maskierten Rohzeilen mit dem Lauf `ingest --all --reread --rewrite-raw`, der ohnehin zu den Update-Schritten dieser Version gehört ([#179](https://github.com/seletz/total-recall/issues/179), siehe [`ingest`](10-befehle.md#ingest)); Transkripte, die inzwischen gelöscht sind, behalten ihre alte Rohzeile ([#162](https://github.com/seletz/total-recall/issues/162), [Design](../explanation/design.md#redaction)).

### TUI

- **Einstellungen wirken sofort.** Die Seite [Settings](30-tui.md#settings) der Ansicht Config übernimmt mehr Werte ohne Neustart: die Tastenbelegung `[keys]` (Tasten, Fußzeile und Hilfe aller Ansichten), `day_start` und die Tabelle `[work]` (Kernzeiten, Ziel und Grenzen zeichnen die Aktivität neu; Tagesgrenze und Bundesland laden das Fenster neu und markieren die Feiertage der Statistik neu; `count` schaltet die Zählweise um) sowie `tui.activity_projects`. Fenster, Markierung, Scrollposition und Eingabe bleiben dabei erhalten, wie schon bei `reload`, `mouse` und den Projektdefinitionen. Die Zeile `default` sagt zu jedem Schlüssel, was eine Änderung tut: `applied at once`, `takes effect at the next start` (`db`, `timezone` und die Startzustände der Ansichten) oder `used by the next command or action` (alles, was die TUI nicht selbst nutzt); die Meldung nach dem Schreiben wiederholt es. Eine im Editor geänderte Tabelle mit einem Fehler lässt die bisherigen Werte in Kraft ([#165](https://github.com/seletz/total-recall/issues/165)).

- **Die Aktivität je Tag füllt die Höhe.** Bisher zeigte die Ansicht genau die Tage der Periode, sieben bei einer Woche, und ließ den Rest des Bildschirms leer. Jetzt stehen immer so viele Tageszeilen, wie zwischen Kopf- und Fußzeile passen: vor der Periode die Tage davor, gedämpft, die Tage der Periode mit `▎` markiert. `d`, `w` und `m` bestimmen nur noch, welche Tage Kopfzeile, Soll, Saldo, Kennzahlen und Legende zusammenfassen; nach einer Größenänderung passt sich die Liste sofort an, ist die Periode länger als der Platz, scrollt sie wie bisher. Neu ist `o`: der neueste Tag unten (Default, wie ein Protokoll) oder oben, auch per Klick auf `day` im Spaltenkopf, dessen Pfeil die Richtung zeigt; ++up++ und ++down++ zeigen immer die Zeile darüber bzw. darunter. Start mit `activity_newest` in [`[tui]`](20-konfiguration.md#tui) ([Aktivität](30-tui.md#aktivitat), [#173](https://github.com/seletz/total-recall/issues/173)).

### Neu in der CLI

- **`blob HASH [--out DATEI]`** gibt ein Bild oder Dokument aus dem Archiv aus, `--json` nur die Metadaten samt Events; Binärdaten nie auf ein Terminal ([`blob`](10-befehle.md#blob)).
- **`show`** nennt unter jedem Event seine Anhänge mit Hash, Typ und Größe, im JSON `attachments` je Event. **`status`** zeigt Anzahl und Größe der Blobs (JSON `blobs`, `blob_bytes`, `blob_stored_bytes`), die TUI auf der Seite *status* ebenso. `ingest` meldet neue Anhänge (`N attachments (X MiB)`).
- **`ingest --all --reread --rewrite-raw`** schreibt jetzt auch Rohzeilen für Events, die bisher keine hatten, und trägt die Blobs nach; abbrechbar, wiederholbar, idempotent.
- Gemessen auf einer Kopie eines echten Archivs (530 Sessions, 239 000 Events, 451 MiB in Schema v5, 8 330 Events ohne Rohzeile): `migrate` auf v6 0,6 s, Datei unverändert. `ingest --all --reread --rewrite-raw` 3 min 50 s, 239 769 Rohzeilen neu geschrieben, 136 neue Events (eingefügte Bilder), keines mehr ohne Rohzeile; `raw` wächst komprimiert von 121 auf 269 MiB (Verhältnis 3,2), dazu 903 Blobs mit 108 MiB (864 PNG, 39 JPEG; 1 890 Verweise, 883 Blobs stehen mehrmals in den Zeilen, ohne Dedup wären es 229 MiB). Ein zweiter Lauf dauert gleich lang und ändert nichts. Nach `maintenance` (8 s) ist die Datei 740 MiB groß, das Backup (`.db.zst`) 467 MiB.

- **Deutsche Beschriftungen in `chart`.** `--lang de` bzw. `lang = "de"` in `[chart]` schaltet Titel, Legende, Achsen, Wochentage und Monatsnamen der Grafiken auf Deutsch um, samt Datums- und Zahlenformat (`KW 41`, `05.10.2026`, `7,5 h`, `Sonstige` statt `other`); die Dateinamen bleiben gleich. `--title TEXT` setzt eine eigene Überschrift statt `<Grafik> · <Zeitraum>`. Rezept: [Grafiken für einen Monatsbericht](../how-to/47-monatsbericht.md#deutsche-beschriftung-eigener-titel) ([#160](https://github.com/seletz/total-recall/issues/160)).

### Backup

- **Backups sind zstd-komprimiert** (`archive-YYYY-MM-DD.db.zst` statt `.db.gz`). Seit Schema v5 sind die Rohzeilen schon zstd-Blöcke, an denen gzip nichts mehr spart; auf einer Kopie eines echten Archivs (444 MiB) schrieb gzip 245 MiB in 6,3 s, zstd 220 MiB in rund 2 s, und das Entpacken dauert 0,4 statt 2 s. Alte `.db.gz` bleiben lesbar (`restore` erkennt das Format an der Endung) und zählen bei der Aufbewahrung weiter mit; `status`, `backup --if-older` und die Config-Seite der TUI nehmen die neueste Kopie in jedem Format. **Eigene Skripte, die das neueste Backup suchen oder mit `gunzip` entpacken, müssen angepasst werden**: auf `archive-*.db.zst` statt `archive-*.db.gz`, zum Entpacken `restore` oder `zstd -d`. Name und Format der Backups gehören nicht zur stabilen Schnittstelle ([Versionierung](60-versionierung.md)). Rezept: [Backup wiederherstellen](../how-to/60-wartung.md#backup-wiederherstellen) ([#163](https://github.com/seletz/total-recall/issues/163)).

### Installation

- Der Homebrew-Cask entfernt das Quarantäne-Attribut nicht mehr. Die macOS-Binaries sind seit v1.0.2 signiert und notarisiert, Gatekeeper startet sie auch mit gesetztem Attribut ohne Dialog; der `postflight`-Schritt war die Übergangslösung bis zur ersten verifizierten Notarisierung ([Installation](../how-to/10-installieren.md#mit-homebrew), [#161](https://github.com/seletz/total-recall/issues/161)).

### Intern

- `scripts/release-notes.sh`, das den Kopf der GitHub-Release-Notes aus dieser Seite baut, hat einen Test gegen einen synthetischen Changelog: Abschnitt finden, relative Links und Anker auf die veröffentlichte Doku umschreiben, fehlende Version abweisen. Neu abgewiesen wird auch ein Abschnitt, der noch „(unveröffentlicht)“ heißt; bisher nahm das Skript ihn als Release Notes an. `mise run release:notes:check`, in der CI über `go test` ([#164](https://github.com/seletz/total-recall/issues/164)).

## v1.2.1

### Behoben

- Claude Code schreibt in seltenen Fällen den neuen Record-Typ `history-suppression` (Grund und Account, gegen den die Unterdrückung entschieden wurde; `ts` statt `timestamp`). `doctor` meldete ihn als unbekannt und endete mit Exit 2. Der Adapter kennt ihn jetzt als Meta-Record: `doctor` meldet ihn nicht mehr, Ingest überspringt ihn ohne Meldung. Gespeichert wird nichts davon ([#167](https://github.com/seletz/total-recall/issues/167)).

## v1.2.0

Ab dieser Version **migriert kein Befehl die Datenbank mehr von selbst**, auch kein Hook. Bringt ein Update ein neues Schema, zeigt `total-recall status` in der Zeile `schema` ein `MIGRATION PENDING`; die Hooks schreiben dann nichts (eine `WARN`-Zeile im Log am Tag), bis du migriert hast, lesende Befehle und die TUI laufen weiter. Es geht nichts verloren.

Dieses Release bringt so ein neues Schema, **v5**: Die Rohzeilen (`events.raw`) werden zu einem entschlackten, komprimierten Auszug, und Events verweisen auf ihre Quelldatei per ID. Das Archiv wird dadurch etwa 60 % kleiner; die Migration schreibt dafür jedes Event einmal um.

**Nach dem Update einmal, in dieser Reihenfolge:**

1. `total-recall backup`: Sicherung vor allem anderen.
2. `total-recall migrate`: zeigt die Versionen und jede ausstehende Migration, fragt nach, legt `archive.db.pre-v<N>.bak` neben die Datenbank und migriert; der erste Hook danach liest alles nach. Für v5: rund eine Minute je 100 000 Events; frei sein muss etwa das Doppelte der Dateigröße neben der Datenbank (Kopie und WAL), `migrate` prüft das vorher. Rezept: [Aktualisieren](../how-to/10-installieren.md#aktualisieren).
3. `total-recall maintenance`: baut die Datei neu auf. Direkt nach der Migration ist sie **größer** als vorher (die alten Seiten stehen auf der Freiliste); erst der Neuaufbau macht sie klein, auf einer Kopie eines echten Archivs von 1,1 GiB auf 438 MiB.

### Archiv

- **`raw` ist ein Auszug, keine wortgetreue Kopie mehr.** Von der Transkriptzeile bleiben nur die Felder, die das Programm zum erneuten Einlesen und für [`reindex --actors`](10-befehle.md#reindex) braucht: Typ, IDs, Zeitstempel, die Flags der Actor-Klassifikation, `message` mit `content` und `model`, `toolUseResult` ohne die Diffs. Buchhaltung der Harness (`wire*`, `serverClassifier*`, Request-IDs) und alles, was schon in Spalten steht (Token-Zahlen, `cwd`, Session-ID), fehlt. Die Transkripte bleiben die Quelle; [`ingest --reread --rewrite-raw`](10-befehle.md#ingest) schreibt `raw` aus ihnen neu, auch im neuen Format. `ingest.raw_max_bytes` misst jetzt den Auszug. Auf einer Kopie eines echten Archivs halbierte das allein `raw` ([Schema](40-schema.md#raw), [#132](https://github.com/seletz/total-recall/issues/132)).
- **`raw` ist komprimiert**: ein zstd-BLOB mit einem eingebetteten, aus synthetischen Zeilen trainierten Wörterbuch (Dictionary-ID im Frame, ein späteres Wörterbuch liest alte Zeilen weiter). In eigenen SQL-Abfragen entpackt `raw_text(raw)`; `sql "SELECT raw …"` liefert sonst Binärdaten. Gemessen auf der Kopie: `raw` 645 → 118 MiB, Datei nach `maintenance` 1146 → 438 MiB, Backup 187 → 244 MiB (das Backup ist gzip-komprimiert, zstd-Blöcke lassen sich nicht weiter packen); `show`, `search`, `sessions` und `stats` liefern vorher und nachher dieselbe Ausgabe ([Design](../explanation/design.md#raw)).
- **`events.source_id` statt `source_path`**: Events verweisen auf `sources.id`, der Pfad steht einmal je Datei. Eigene Abfragen holen ihn mit `JOIN sources s ON s.id = e.source_id`.
- [`status`](10-befehle.md#status) zeigt in der Zeile `raw` die komprimierte Größe aller Rohzeilen, die Zahl der Events mit und ohne `raw` und das Kompressionsverhältnis (geschätzt aus einer Stichprobe, nichts wird entpackt); JSON: `raw_rows`, `raw_bytes`, `raw_truncated`, `raw_ratio`.
- Für Entwickler: `mise run dict:train` trainiert das Wörterbuch neu (nur aus dem Demo-Archiv und generierten Zeilen), `go run ./tools/rawdict measure -db …` misst die Kompression eines Archivs in Zahlen.

### Behoben

- **Flackern beim Blättern durch die Zeit in der Aktivität.** Jeder Tagesschritt (++up++/++down++, Mausrad) zeichnete bis zur Antwort seiner Abfrage einen Ladezustand ohne Zeilen, Kopfzeile und Kennzahlen; jetzt bleibt der alte Inhalt stehen, bis das neue Fenster vollständig da ist, die Tage werden einzeln gecacht und mit einer Periode (mindestens sieben Tagen) Vorlauf und Nachlauf abgefragt, Schritte innerhalb der laufenden Abfrage warten auf deren Antwort, und eine Antwort von vor einem Neuladen wird verworfen ([#147](https://github.com/seletz/total-recall/issues/147)).
- **`view = "config"` in `[tui]` war ein Konfigurationsfehler**, obwohl `total-recall tui config` die Ansicht kennt; der Wert ist jetzt erlaubt. Die Release-Notes auf GitHub verlinken Anker innerhalb der Änderungsseite (`#behoben`) jetzt absolut.

### Neu in der CLI

- **Neuer Befehl [`chart`](10-befehle.md#chart): Grafiken für Monatsberichte als SVG oder PNG.** `total-recall chart <calendar|projects|punchcard|worktime|all>` zeichnet den Kalender der eigenen Nachrichten je Tag (Monatsraster, vier Stufen), den Anteil je Projekt (zwei Balken: Agenten-Arbeit und eigene Aufmerksamkeit, Top 12), die Uhrzeiten als Punchcard (Wochentag × Stunde, eigene Nachrichten über der Agenten-Aktivität) und die Arbeitszeit je Woche und Kategorie (gestapelt, mit Wochensoll aus `[work]`). Zeitraum wie `stats` (`--month`, `--since`/`--until`, `--range`), gezählt wie `stats` (eingespeiste Aufträge anderer Agenten zählen nicht). SVG wird selbst erzeugt, PNG in reinem Go gerastert; `--format`, `--scale`, `--width`/`--height`, `--out DIR` mit den Dateinamen `<YYYY-MM>-<grafik>.<format>`, `--no-title`, `--no-legend`, `--transparent`, `--top`. Rezept mit Beispielbildern aus dem Demo-Archiv: [Grafiken für einen Monatsbericht](../how-to/47-monatsbericht.md) ([#155](https://github.com/seletz/total-recall/issues/155)).
- **Neuer Befehl [`migrate`](10-befehle.md#migrate), Schema-Migrationen laufen nicht mehr automatisch.** Bisher migrierte der erste schreibende Befehl nach einem Update die Datenbank unbemerkt, auch ein Hook. Eine Migration kann aber alle Events umschreiben und Minuten dauern; das entscheidet jetzt der Nutzer. `migrate` zeigt die Version der Datenbank und des Programms, je ausstehender Migration eine Beschreibung, den Pfad der Kopie und den nötigen freien Platz, fragt nach (`--yes` für Skripte, `--dry-run` zeigt nur), prüft den Platz, sperrt das Archiv wie `maintenance` gegen Hooks, schreibt wie bisher `archive.db.pre-v<N>.bak` und migriert mit Fortschrittsanzeige; `--json` für Skripte. Eine neue, leere Datenbank entsteht weiterhin direkt in der neuesten Version. Bei einer älteren Datenbank schreiben `ingest`, `reindex` und `maintenance` nichts und nennen den Weg (`total-recall backup`, dann `total-recall migrate`); im Hook ist das kein Fehler, sondern eine `WARN`-Zeile im Log, einmal am Tag, der Lesestand bleibt, der erste Hook nach der Migration liest nach. `backup` funktioniert ohne Migration. Lesende Befehle warnen auf stderr und laufen, soweit das Schema reicht; `status` zeigt `MIGRATION PENDING` und `migration_pending` im JSON, `doctor` meldet die ausstehende Migration als Befund, die [TUI](30-tui.md) zeigt `migration pending` in der Kopfzeile ([#145](https://github.com/seletz/total-recall/issues/145)).

### Konfiguration

- **`[chart]` und Farben der Kategorien.** Der neue Abschnitt [`[chart]`](20-konfiguration.md#chart) hält die Defaults von `chart`: `format`, `scale`, `title`, `legend`, `transparent`, `top` und die Farbstufen des Kalenders `heatmap`. Eine Regel in [`[[worktime.category]]`](20-konfiguration.md#worktime) bekommt optional `color` (Palettenname wie bei `[[project]]` oder `#rrggbb`) für `chart worktime`; ohne Farbe die nächste freie Reihenfarbe. Geprüft erst beim Gebrauch wie `[worktime]`: ein Fehler stoppt `chart`, nie die Hooks; `config show` und `doctor` melden ihn ([#155](https://github.com/seletz/total-recall/issues/155)).
- **Projekte mit Namen und Farbe.** Die neue Liste [`[[project]]`](20-konfiguration.md#project) definiert Projekte: `name` (Pflicht, eindeutig), `description`, `color` (Palettenname `blue`, `orange`, `green`, `red`, `purple`, `teal` oder `#rrggbb`) und `match`, Glob-Muster wie in `[[worktime.category]]` gegen den Git-Root bzw. das Arbeitsverzeichnis (als Pfad und mit `~`), die Remote-URL und den bisherigen abgeleiteten Namen. Die erste passende Definition gewinnt; Sessions ohne Treffer heißen wie bisher, ohne Konfiguration ändert sich nichts. Die Zuordnung wird bei jeder Abfrage berechnet, nicht gespeichert, und wirkt deshalb sofort auch rückwirkend: in [`stats --by project`](10-befehle.md#stats-period) (mehrere Repositories mit derselben Definition sind ein Schlüssel, auch vor `--top`), in [`stats --worktime`](10-befehle.md#stats-worktime) (die Kategorie-Regeln sehen den konfigurierten Namen) und in der Statistik der [TUI](30-tui.md#statistik), wo ein Projekt seine konfigurierte Farbe trägt und die automatischen Farben sie auslassen. Fehler in der Liste stoppen nur diese Befehle, nie die Hooks; `config show` zeigt die Definitionen unter `project`, `doctor` die Fehler. Die Pflege in der TUI: [Seite Projects](30-tui.md#projects), Rezept [Projekte definieren](../how-to/67-projekte-definieren.md) ([#4](https://github.com/seletz/total-recall/issues/4), [#146](https://github.com/seletz/total-recall/issues/146), [#148](https://github.com/seletz/total-recall/issues/148)).

### TUI

- **Ansicht Config** (`4`, `total-recall tui config`) mit fünf Seiten: **status** zeigt, was `status` ausgibt, mit der Zeile `raw` (komprimierte Größe der Rohzeilen, Events mit und ohne `raw`), dazu `migration pending`, wenn die Datenbank hinter dem Schema der Binary liegt, die Probleme der Konfiguration und die Fehler des Logs der letzten 24 Stunden; **settings** listet jeden Schlüssel mit Wert, Herkunft, Hilfetext und Default und ändert ihn in der Datei (++enter++: Wahrheitswert kippt, Aufzählung weiter, sonst Eingabezeile; `d` zurück auf den Default), mit erhaltenen Kommentaren, `config.toml.bak`, atomarem Schreiben und derselben Prüfung wie beim Laden; **projects** pflegt die Projektdefinitionen aus `[[project]]` (siehe unten); **actions** startet `backup`, `maintenance` (auch als Probelauf), `migrate`, `reindex --actors` (auch als Probelauf), `ingest --all` und `doctor` im Prozess der TUI, schreibende nur nach `y`, mit Fortschrittszeilen, Abbruch per ++esc++ und denselben Sperren wie in der CLI; bei anstehender Migration schreibt außer `backup` nichts (`migrate first`), und `migrate` zeigt vor der Bestätigung den Plan des Befehls (Versionen, Schritte, Kopie, nötiger Platz) und danach seinen Fortschritt; **setup** prüft Config-Datei, Hooks in `~/.claude/settings.json`, Binary im `PATH` samt Version, Backup-Verzeichnis, Transkript-Verzeichnis und Archiv, bietet für Vorlage und Backup-Verzeichnis eine Korrektur an und zeigt den Hook-Block zum Kopieren (`c`); in die Einstellungen von Claude Code schreibt sie nicht. Tasten mit Präfix `config_`: [Config](30-tui.md#config), Rezept: [Einstellungen in der TUI ändern](../how-to/65-einstellungen-tui.md) ([#146](https://github.com/seletz/total-recall/issues/146)).
- **Projekte in der TUI pflegen.** Die Seite [Projects](30-tui.md#projects) der Ansicht Config legt Definitionen `[[project]]` an, ändert, löscht und sortiert sie in der Config-Datei. Die Liste zeigt je Definition Farbe, Beschreibung, die Zahl der Sessions, die sie trifft, und die Muster; darunter unter `UNMATCHED` die Git-Roots und Verzeichnisse des Archivs ohne Definition mit Sessionzahl und letzter Aktivität: `a` macht aus einem Pfad ein Projekt (Name wie abgeleitet, Muster die Remote-URL, die auch Worktrees trifft), `m` ein Muster einer bestehenden Definition. Das Formular (++enter++) bearbeitet Name, Beschreibung, Farbe (Palette mit Farbfeld, `custom #rrggbb`, zurück auf automatisch) und die Muster; eine Vorschau sagt zum markierten oder gerade getippten Muster, welche Pfade und wie viele Sessions es trifft und welche frühere Definition einen Pfad wegnimmt, denn die erste passende gewinnt (`K`/`J` ändern die Reihenfolge). `y` schreibt, `x` löscht nach Rückfrage. Geschrieben wird wie auf der Seite Settings: Kommentare bleiben, `config.toml.bak`, atomar, nur was die Prüfung besteht; ein abgelehnter Entwurf lässt die Datei unverändert. Statistik und Aktivität übernehmen die Definitionen sofort und laden neu, ohne Neustart. Neue Tasten `config_add` (`a`), `config_remove` (`x`), `config_move_up` (`K`), `config_move_down` (`J`), `config_pattern` (`m`); Rezept: [Projekte definieren](../how-to/67-projekte-definieren.md) ([#4](https://github.com/seletz/total-recall/issues/4)).
- **Aktivität in Projektfarben.** In der Ansicht [Aktivität](30-tui.md#aktivitat) färbt `p` (`activity_projects`) die Balken nach Projekt statt nach Kernzeit: jede Zelle der Zeitachse zeigt das Projekt mit den meisten gezählten Events in dieser Zeit (`human` oder `all` wie die Ansicht), in derselben Farbe und mit derselben Zuordnung wie in der Statistik (Definitionen [`[[project]]`](20-konfiguration.md#project) mit `color` zuerst); Zellen ohne Event bleiben als gedämpfte Lücke stehen. Darunter eine Legende mit den Projekten des Fensters, Farbe, Zeichen und Zahl der Events; mehr als sechs Projekte werden grau als `other` zusammengefasst. Ohne Farben (`NO_COLOR`) unterscheidet je Projekt ein eigenes Zeichen. Der Umschalter `bars: hours projects` steht oben neben Fenster und Zählweise und ist klickbar; `activity_projects = true` in [`[tui]`](20-konfiguration.md#tui) startet so. Die Projektdaten sind Teil der einen Abfrage je Fenster und wie die Tage je Tag gecacht; beim Blättern bleiben die alten Balken bis zur Antwort stehen. Wird die obere Zeile zu schmal, verkürzt sich das Fenster rechts stufenweise, statt abgeschnitten zu werden ([#148](https://github.com/seletz/total-recall/issues/148)).
- **Mausklicks in allen Ansichten.** Klickbar ist, was es per Taste gibt, mit derselben Wirkung: die Tastenhinweise der Fußzeile (bei `d/w/m period` jede Taste einzeln, Abgeschnittenes nicht), Periode und Dimension der Statistik, Fenster, Zählweise und Kennzahlen der Aktivität, die Vorschau der Suche (öffnet die Session wie ++enter++) und ein Klick neben das Hilfe-Fenster (schließt es). Rad, Tabs, Trefferliste und Eingabezeile wie bisher; `--no-mouse` und `mouse = false` unverändert: [Maus](30-tui.md#maus) ([#140](https://github.com/seletz/total-recall/issues/140)).
- **Automatisches Neuladen.** Die [TUI](30-tui.md#neuladen) lädt ihre Daten neu, während die Hooks schreiben: Alle fünf Sekunden (`reload` in [`[tui]`](20-konfiguration.md#tui), `--reload`, `"0"` schaltet ab, mindestens `1s`) prüft sie mit einer einzigen Abfrage, ob sich das Archiv geändert hat, und lädt nur dann und nur die sichtbare Ansicht neu: Suche mit aktueller Abfrage, Statistik, Aktivität oder die offene Session. Markierung, Scrollposition, Fokus und Eingabe bleiben erhalten; in der Session kommen neue Events unten hinzu, am Ende läuft die Ansicht mit. Die Kopfzeile zeigt `updated 14:03:12`, `R` (`global_reload`) lädt sofort. Tauscht `maintenance` die Datei aus, öffnet die TUI sie von selbst neu; zwischen den Prüfungen hält sie keine Lesetransaktion offen, der WAL-Checkpoint der Wartung bleibt frei ([#141](https://github.com/seletz/total-recall/issues/141)).

## v1.1.0

Dieses Release behebt einen **Datenverlust** in der Rohzeile `raw` (siehe [Behoben](#behoben)) und hebt die Datenbank auf Schema v4: Fortsetzung und Account je Session. Ein schreibender Befehl (`ingest`, `backup`, `maintenance`) migriert beim ersten Lauf und legt vorher `archive.db.pre-v3.bak` an.

**Nach dem Update einmal, in dieser Reihenfolge:**

1. `total-recall backup`: Sicherung vor allem anderen.
2. `total-recall ingest --all --reread --rewrite-raw`: migriert auf Schema v4, liest alle noch vorhandenen Transkripte neu, füllt Fortsetzung und Account der bestehenden Sessions und schreibt die beschädigten Rohzeilen neu. Events werden dabei nicht verdoppelt; `raw` wird etwa doppelt so groß.
3. Optional, wer mit koordinierenden Agenten arbeitet: `peer_prefixes` bzw. `peer_paste_headers` in der [Konfiguration](20-konfiguration.md) setzen, dann `total-recall reindex --actors`, damit eingespeiste Aufträge nicht mehr als eigene Arbeitszeit zählen.
4. Optional: `total-recall maintenance` baut die Datei mit 16-KiB-Seiten neu und spart rund 14 %.

### Behoben

- **Datenverlust in `raw`.** Seit v0.1.0 hat die Maskierung beim Einlesen ganze Werte der Rohzeile (`events.raw`) durch `«redacted:high_entropy»` ersetzt, sobald sie 32 Zeichen oder länger waren: Shell-Befehle, Tool-Ausgaben und Nachrichtentexte. In einem realen Archiv fehlte so in fast der Hälfte aller Agent-Nachrichten der komplette Text in `raw`. **Nicht betroffen** ist die Spalte `text`, und damit `show`, `search`, `stats` und die TUI. Jetzt maskiert die Rohzeile wie der Text nur noch einzelne Tokens (bekannte Token-Formate, Private Keys, Zuweisungen, Bearer-Header); ganze Werte nur unter sensiblen Schlüsseln wie `password`, `token`, `secret`, `api_key` oder `authorization`, oder wenn der Wert selbst ein einzelnes zufälliges Token ist. **Reparatur:** zuerst ein Backup (`total-recall backup`), dann einmal `total-recall ingest --all --reread --rewrite-raw`. Das liest alle Transkripte neu und ersetzt nur `raw` der vorhandenen Events; Events, Volltextindex und Sessions bleiben zahlenmäßig gleich. Für Transkripte, die inzwischen gelöscht sind, ist der Verlust endgültig. `raw` wird danach etwa doppelt so groß ([#136](https://github.com/seletz/total-recall/issues/136), [#130](https://github.com/seletz/total-recall/issues/130)).

### Neu in der CLI

- Fortgesetzte Sessions sind verkettet: Claude Code schreibt seit 2.1.318 am Ende eines Transkripts, in welcher Session es weiterging (`continued-in`). Das Archiv speichert die Verkettung in `sessions.continued_in`; `sessions --json` und `show --json` haben `continued_in` und `continued_from`, der Kopf von `show` und der Session-Ansicht der [TUI](30-tui.md#session) nennt beide Richtungen. Die Zielsession darf später oder nie eingelesen werden, der Ingest scheitert daran nicht ([#113](https://github.com/seletz/total-recall/issues/113), Schritt 2).
- Account und Organisation je Session (Claude Code seit 2.1.317, Record `session-owner`) stehen in `sessions.account_id` und `organization_id`. `sessions --account` und `search --account` filtern danach (Name, ID oder ID-Präfix), [`stats --by account`](10-befehle.md#stats-period) zählt je Account (`unknown` für Sessions ohne), `show` zeigt beide im Kopf, JSON hat `account_id`, `organization_id` und `account` ([#97](https://github.com/seletz/total-recall/issues/97), Schritt 2).
- [`ingest --reread`](10-befehle.md#ingest) liest die angegebenen Dateien bzw. mit `--all` alle Quellen von Anfang an; Events bleiben eindeutig, Sessions bekommen die Felder, die ältere Versionen nicht gespeichert haben.
- [`ingest --reread --rewrite-raw`](10-befehle.md#ingest) ersetzt dabei zusätzlich die Rohzeile (`raw`, `raw_truncated`) vorhandener Events durch die neu maskierte; `text`, `actor` und der Volltextindex bleiben unverändert. Die Ausgabe nennt `N raw rewritten`. Braucht `ingest.store_raw = true` ([#130](https://github.com/seletz/total-recall/issues/130)).
- `stats --worktime --summary` liefert je Tag und Kategorie die Issue- und PR-Referenzen aus den Session-Titeln und deinen eigenen Nachrichten (`#31`, `PR #32`, `owner/repo#123`, auch aus Issue- und Pull-URLs; nie aus Antworten des Agenten oder Werkzeugausgaben), je Projekt gruppiert und dedupliziert, sowie einen Vorschlag für den Buchungstext `<projekt>: <titel> (<refs>), …`, gekürzt auf `summary_max_chars` in `[worktime]` (Default 200). Kombinierbar mit `--sessions`; `--json` enthält `refs` und `text` immer, auch je Projekt und Session. Rezept: [Stundenzettel füllen](../how-to/45-stundenzettel.md#referenzen-und-ein-vorschlag-fur-den-text) ([#108](https://github.com/seletz/total-recall/issues/108)).
- `search WORDS --by-session` fasst die Treffer je Session zusammen, Subagenten-Sessions eingeschlossen: eine Zeile mit `session_id`, `display_name`, `repo`, `first_hit`, `last_hit` (in JSON UTC, in der Tabelle lokale Zeit), `hits` und dem `snippet` des ersten Treffers, sortiert nach dem ersten Treffer als Zeitleiste oder mit `--order hits` nach Trefferzahl. Die Filter `--since`, `--until`, `--repo`, `--actor`, `--session`, `--raw` und die Ausgabeformate gelten wie bisher, `--limit` behält die Sessions mit den jüngsten Treffern. Die Kernabfrage 1 des Skills verweist jetzt auf diesen Befehl statt auf eine SQL-Abfrage: [`search`](10-befehle.md#search) ([#109](https://github.com/seletz/total-recall/issues/109)).
- Aufträge, die ein koordinierender Agent in eine Session tippt oder als Block einfügt, zählen nicht mehr als eigene Nachrichten. Neben `peer_prefixes` gibt es in `[harness.claude.actors]` die Liste `peer_paste_headers`: Eine Nachricht, die nur aus einem eingefügten Block besteht und deren erste Zeile mit einem der Einträge beginnt (etwa `# Task:` oder `Auftrag:`), bekommt `actor = 'peer'`. `stats --workday` und `stats --worktime` zählen weiterhin nur `human`, die Arbeitszeit endet damit wieder mit der eigenen letzten Nachricht ([#111](https://github.com/seletz/total-recall/issues/111)). Beispielkonfiguration: [Stundenzettel füllen](../how-to/45-stundenzettel.md#mehrere-agenten), Regeln: [`[harness.claude.actors]`](20-konfiguration.md#harnessclaudeactors).
- Neuer Befehl [`reindex --actors`](10-befehle.md#reindex): wendet die aktuellen Actor-Regeln auf alle gespeicherten User-Nachrichten an, ohne die Transkripte zu lesen; idempotent, `--dry-run` zeigt nur, wie viele Events sich ändern würden ([#111](https://github.com/seletz/total-recall/issues/111)).
- Neuer Befehl [`maintenance`](10-befehle.md#maintenance): baut die Datenbankdatei nach einem Backup mit größeren Seiten (`--page-size`, Default 16384) kompakt neu auf, prüft die Kopie (`PRAGMA integrity_check`, gleiche Zeilenzahlen) und tauscht sie atomar aus; bei Zeilen von 1 bis 4 KiB spart das rund ein Siebtel der Dateigröße ohne Schemaänderung. `--dry-run` misst nur. Laufende Hooks werden abgewartet (`--wait`), Hooks während des Austauschs überspringen ihren Lauf und lesen beim nächsten Hook weiter; jeder Fehler vor dem Austausch lässt das Archiv unverändert. `status` zeigt die Seitengröße (`page_size`) und einen Hinweis, solange sie unter 16384 liegt. Wann und wie: [Archiv warten](../how-to/60-wartung.md) ([#134](https://github.com/seletz/total-recall/issues/134)).
- `show`, `search`, `sessions`, `stats` und die TUI laden die Spalte `raw` der Events nicht mehr, die kein Lesebefehl ausgibt und die den Großteil des Archivs ausmacht; `show` einer großen Session ist damit um ein Vielfaches schneller. Nur `reindex --actors` und `backup` fassen `raw` weiter an ([#131](https://github.com/seletz/total-recall/issues/131)).

### TUI

- In der Ansicht **Aktivität** blättern ++up++/++down++ (`k`/`j`) und das Mausrad durch die Zeit: Passen nicht alle Tage auf den Bildschirm, scrollen sie wie bisher die Zeilen, am Rand rückt das Fenster um einen Tag zurück bzw. vor statt stehenzubleiben. Woche und Monat werden so zu einem gleitenden Fenster gleicher Länge; Kopfzeile, Soll, Saldo und Kennzahlen gelten für die sichtbaren Tage, Kalenderwoche oder Monat stehen nur bei einem ganzen. `c`, `l`, `d`, `w`, `m` richten wieder am Kalender aus. ++left++/++right++ springen weiter um eine ganze Periode, aber nicht mehr über die laufende hinaus; auch ++down++ endet dort. Die Hilfe nennt die Tasten `day back (scroll)` und `day forward (scroll)`: [TUI](30-tui.md#aktivitat) ([#125](https://github.com/seletz/total-recall/issues/125)).

### Konfiguration

- Neu: [`[accounts]`](20-konfiguration.md#accounts) ordnet Account- und Organisations-IDs Namen zu, die `sessions`, `show` und `stats --by account` anzeigen und als Filter annehmen.

### Release

- Der Release-Lauf prüft den Download von goreleaser jetzt kryptografisch: Vor jedem goreleaser-Schritt in `ci.yml`, `release.yml` und `signed-snapshot.yml` installiert `sigstore/cosign-installer` (v4.1.2) cosign, womit `goreleaser-action` die Sigstore-Signatur der Checksummen-Datei des goreleaser-Releases prüft statt nur die Prüfsumme; eine fehlgeschlagene Prüfung bricht den Lauf ab. goreleaser ist in allen Workflows auf v2.19.0 gepinnt, die Version steht in `mise.toml`, und `mise run tools:check` (auch in der CI) findet Abweichungen in den Workflows: [Release-Assets](50-release.md#werkzeuge-des-release-laufs) ([#121](https://github.com/seletz/total-recall/issues/121)).

### Dokumentation

- [Schema](40-schema.md) und [Design](../explanation/design.md#sessions) beschreiben die drei neuen Spalten, der [Skill](../how-to/50-agenten-anbinden.md) Fortsetzungsketten und Accounts.

## v1.0.2

Die macOS-Binaries sind ab dieser Version signiert und notarisiert. Sonst ändert sich nichts; die Datenbank bleibt bei Schema v3.

### Release

- Die macOS-Binaries (amd64, arm64) sind mit einer Developer-ID signiert (Hardened Runtime, Apple-Zeitstempel) und von Apple notarisiert. Gatekeeper startet sie ohne Dialog, auch aus einem über den Browser geladenen Archiv mit Quarantäne-Attribut; der Fehler „Apple konnte nicht überprüfen …“ nach `brew upgrade --cask total-recall`, wenn ein Hook das Binary vor dem `postflight`-Schritt des Casks aufrief, entfällt. Signiert wird im Release-Lauf vor Archiv, SBOM und Prüfsummen, sodass alle drei das ausgelieferte Binary beschreiben; ohne die Secrets im Repository (Snapshot, CI, Forks) bleibt das Binary wie bisher unsigniert. Prüfung mit `codesign` und `spctl` sowie die Einrichtung der Secrets: [Release-Assets](50-release.md#signatur-der-macos-binaries) ([#116](https://github.com/seletz/total-recall/issues/116)).
- Der manuell startbare Workflow `signed-snapshot.yml` baut einen signierten und notarisierten Snapshot der darwin-Binaries und hängt die Archive sieben Tage als Workflow-Artefakt an den Lauf, ohne Tag, Release oder Cask. So lassen sich die Secrets und die Signatur prüfen, bevor ein Release sie braucht: [Release-Assets](50-release.md#signatur-ohne-release-testen) ([#116](https://github.com/seletz/total-recall/issues/116)).

## v1.0.1

### Behoben

- Claude Code 2.1.318 schreibt den neuen Record-Typ `continued-in` (die Session, in der diese fortgesetzt wurde; einmal am Ende des Transkripts). Der Adapter kennt ihn jetzt als Meta-Record: `doctor` meldet ihn nicht mehr als unbekannt, Ingest überspringt ihn wie bisher ohne Meldung. Die Verkettung selbst wird noch nicht gespeichert, das folgt in v1.1.0 ([#113](https://github.com/seletz/total-recall/issues/113), Schritt 1).

## v1.0.0

Das erste öffentliche Release: das Repository ist ab dieser Version öffentlich, die Releases tragen eine SBOM je Plattform, die Installation geht über das Release-Archiv oder den Homebrew-Cask. Funktional ändert sich gegenüber v0.3.1 wenig; die Datenbank bleibt bei Schema v3, ein Update braucht keine Migration.

### Neu in der CLI

- `total-recall --version` (kurz `-v`) gibt die Version aus, gleich wie [`version`](10-befehle.md#version) ([#103](https://github.com/seletz/total-recall/issues/103)).

### Release

- Jedes Release trägt je Plattform eine SBOM im Format CycloneDX 1.6 als Asset neben dem Archiv (`total-recall-<tag>-<os>-<arch>.cdx.json`): alle in das Binary gelinkten Go-Module mit Version und Hash wie in `go.sum`, die Go-Toolchain und die Build-Einstellungen. Die SBOMs stehen in `checksums-<tag>.txt`; die CI prüft sie bei jedem Pull Request, der Release-Lauf vor dem Upload. Aufbau und Prüfung: [Release-Assets](50-release.md) ([#106](https://github.com/seletz/total-recall/issues/106)).
- Der Homebrew-Cask `seletz/tap/total-recall` wird ab dieser Version mit jedem Release aktualisiert; vorher konnte Homebrew die Archive des privaten Repositorys nicht laden.

### Dokumentation

- Die [TUI-Referenz](30-tui.md) zeigt alle Ansichten als Screenshots aus dem Demo-Archiv statt als ASCII-Skizzen; neu dazu das Hilfe-Overlay und das kompakte Layout der Heatmap bei 80×24. Die Entwurfsskizzen im [Spike](../explanation/spike-tui.md#bildschirme) sind als solche markiert. `mise run docs:screenshots:check` prüft, ob die eingecheckten Screenshots und Goldens zur TUI passen; die CI ruft es auf ([#99](https://github.com/seletz/total-recall/issues/99)).
- [Installieren](../how-to/10-installieren.md) beschreibt den Weg über das Release-Archiv mit Prüfsumme und SBOM; die README erklärt Zweck, Installation und Lizenz für Leser, die das Repository neu finden.

## v0.3.1

### Behoben

- Claude Code 2.1.317 schreibt den neuen Record-Typ `session-owner` (Account und Organisation der Session, mehrfach je Transkript, ohne Zeitstempel). Der Adapter kennt ihn jetzt als Meta-Record: `doctor` meldet ihn nicht mehr als unbekannt, Ingest überspringt ihn wie bisher ohne Meldung. Gespeichert wird nichts davon ([#97](https://github.com/seletz/total-recall/issues/97), Schritt 1).

## v0.3.0

### Neu in der CLI

- [`stats --worktime`](10-befehle.md#stats-worktime): die aktive Arbeitszeit je Tag und Kategorie, geschätzt aus den Lücken zwischen den eigenen Nachrichten über alle Sessions (bis 20 min voll, längere 3 min), je Kategorie und Tag auf 15 min aufgerundet; Zeitraum wie `--workday`, `--sessions` listet je Zelle die Sessions mit Titel als Vorlage für Buchungstexte, JSON mit Projekten und Sessions. Ersatz für das Skript über `sql`, das bisher den Stundenzettel gefüllt hat. Meetings und Arbeit ohne Agent sind nicht erfasst: die Werte sind eine Untergrenze. Rezept: [Stundenzettel füllen](../how-to/45-stundenzettel.md).

### Konfiguration

- Neu: [`[worktime]`](20-konfiguration.md#worktime) mit `max_gap`, `min_credit`, `grid`, `rounding` (`up`, `down`, `nearest`), `scope` (`category`, `project`) und den Regeln `[[worktime.category]]`, die Projekte per Glob auf Name, Git-Root oder Remote-URL Kategorien zuordnen. Wie `[work]` wird der Abschnitt erst beim Gebrauch geprüft; `config show` und `doctor` melden Fehler, Hooks laufen weiter.
- Der [Skill](../how-to/50-agenten-anbinden.md) beschreibt `stats --worktime` und seine JSON-Felder.

## v0.2.0

Die Terminal-Oberfläche aus dem [Spike](../explanation/spike-tui.md) ist übernommen, dazu Arbeitszeit und Feiertage in CLI und TUI, Zeitreihen in `stats`, ein fester Ausgabevertrag der lesenden Befehle für Skripte und Agenten, Zeitfenster in `show` und drei neue Config-Abschnitte. Die Datenbank bleibt bei Schema v3; ein Update braucht keine Migration.

### Neu: `total-recall tui`

Eine Vollbild-Oberfläche im Terminal, nur lesend. Bedienung: [TUI](30-tui.md).

- **Suche** mit Trefferliste beim Tippen, Vorschau des Austauschs um den Treffer, Filtern nach Actor, Zeitraum und Repo sowie Rohsyntax (++ctrl+r++). ++enter++ öffnet die Session.
- **Session** als Detailansicht: Verlauf mit Hervorhebung der Suchwörter, `n`/`N` von Treffer zu Treffer, `T` blendet Tool-Aufrufe ein und aus, `y` kopiert die Session-ID.
- **Statistik:** gestapeltes Chart je Tag, Woche oder Monat nach Projekt, Agent-Typ oder Verzeichnis, Kennzahlen Events, Mensch oder Sessions, Tabelle mit eindeutigen Sessions je Zeitfenster, Heatmap Wochentag × Stunde (auch bei 80×24). Das Chart gibt es als Text überall und als Bild über Kitty Graphics in Ghostty, kitty und herdr, mit Rückfall auf Text und Grund in der Statuszeile.
- **Aktivität:** erste und letzte Aktivität je Tag mit Kernzeit als Band, Tagessoll und Differenz, Arbeitszeit der Woche oder des Monats mit Soll und Saldo, Feiertage nach Bundesland, Kennzahlen (Median, Mittelwert, Minimum, Maximum).
- **Maus:** Rad blättert, Klick wechselt Tabs und wählt Treffer; `--no-mouse` lässt die Maus beim Terminal.
- **Farben:** ein gemeinsames Theme für alle Ansichten, folgt dem Terminalhintergrund, bleibt mit `NO_COLOR` lesbar.
- **Tasten:** Standardbelegung für ein deutsches Mac-Layout, jede Aktion über `[keys]` umbelegbar; `?` zeigt die wirksame Belegung.
- **Projektnamen:** Verzeichnisse ohne Repository erscheinen als Pfad mit `~` für das Home-Verzeichnis.

### Neu in der CLI

- [`stats --period day|week|month --by project|agent|directory --top N`](10-befehle.md#stats-period): eine Zeitreihe als Text oder JSON, Ersatz für externe Auswertungsskripte.
- [`stats --workday`](10-befehle.md#stats-workday): erste und letzte Aktivität je Tag mit Spanne, Soll, Differenz, Feiertagen und Saldo; `--range today|this-week|last-week|this-month|last-month`, `--all-events`, JSON.
- [Ausgabe der lesenden Befehle](10-befehle.md#ausgabe): `status`, `sessions`, `show`, `search`, `stats`, `sql`, `doctor` und `config show` haben `--format table|json|jsonl` (`sql` auch `csv`), `--json` als Kurzform und `--no-truncate`. `jsonl` schreibt ein kompaktes Objekt je Zeile, ungepuffert; `show --format jsonl` schreibt nur die Events. Mit JSON-Ausgabe geht ein Fehler als `{"error":{"kind":…,"message":…}}` auf stdout, Exit-Code 1; `kind` ist stabil (`usage`, `config`, `db`, `sql`, `not_found`, `ambiguous`, `internal`). Jeder dokumentierte Schlüssel ist immer vorhanden, ein leerer String ist `null`.
- [`show --since/--until/--limit/--offset/--from-event`](10-befehle.md#show): Zeitfenster in lokaler Zeit wie bei `search`, Blättern und Einstieg an einem Event, etwa der `event_id` eines Suchtreffers. Ohne Grenzen bleibt die Ausgabe vollständig; ab 2000 Events steht ein Hinweis auf stderr, wie sie sich begrenzen lässt.
- [`search --session ID`](10-befehle.md#search): nur in einer Session suchen; volle ID, native ID oder eindeutiges Präfix.
- [`llm --skill`](../how-to/50-agenten-anbinden.md): der Skill für Agenten ist für maschinelle Nutzung umgebaut: zuerst die vier Kernabfragen als Copy-Paste-Blöcke für `sql --json`, dann der Ausgabevertrag, Kostenhinweise, Schema und Fallstricke (etwa das Idiom für ein lokales Zeitfenster auf `ts`). Ein Test prüft die genannten Spalten gegen das Schema und die JSON-Felder gegen die echte Ausgabe.
- `config show` listet die Tastenbelegung als `keys.<aktion>` und meldet Probleme in `[work]`, `[tui]` und `[keys]`; `doctor` meldet sie als Befund.
- [`status`](10-befehle.md#status) zeigt nur noch Fehler der letzten 24 Stunden, mit lokaler Zeit und Alter; ältere erscheinen als Anzahl, `--all-errors` listet sie. Hook-Zeilen über noch nicht vorhandene Transkripte zählen nicht mehr als Fehler. In der JSON-Ausgabe ist `recent_errors` jetzt eine Liste von Objekten (`at`, `message`), neu sind `errors_window`, `errors_since`, `recent_errors_total` und `older_errors`.

### Konfiguration

Neu in der [Konfiguration](20-konfiguration.md): `day_start` (Tagesgrenze der Aktivität je Tag), `[work]` (Kernzeit, Tagessoll, Rahmen je Tag und Woche, Bundesland, Zählweise), `[tui]` (Startansicht, Grafikmodus, Maus, Startzustand von Statistik und Aktivität) und `[keys]` (Tastenbelegung). `config init` schreibt sie kommentiert in die Vorlage.

### Geändertes Verhalten

- **Nur eigene Nachrichten:** `stats --workday` und die Aktivitätsansicht zählen standardmäßig nur menschliche Nachrichten, weil Agent-Events nachts weiterlaufen und die Spanne dehnen. `--all-events`, die Taste `h` oder `count = "all"` in `[work]` zählen alles. `--human-only` ist veraltet und wird noch angenommen.
- **Fokus statt Ansicht:** ++tab++ und ++shift+tab++ wechseln in der Suche nur noch zwischen Eingabe und Liste. Die Ansichten wechseln `1`, `2` und `3`.
- **Prüfung der Config:** `[work]`, `[tui]` und `[keys]` werden erst geprüft, wenn ein Befehl sie braucht. Ein Fehler dort bricht `tui` und `stats --workday` ab, stoppt aber nie Hooks und `ingest`.
- **JSON-Fehler auf stdout:** Mit `--json` oder `--format json|jsonl` steht ein Fehler jetzt als Objekt auf stdout, nicht mehr nur als Text auf stderr. Ein Skript muss nur stdout lesen; der Exit-Code bleibt 1.
- **`null` statt fehlender Schlüssel:** Schlüssel ohne Wert fehlten in der JSON-Ausgabe bisher, jetzt stehen sie mit `null` darin, zum Beispiel `parent_id` einer Haupt-Session oder `holiday` an einem gewöhnlichen Tag. Zahlen und Listen sind nie `null`.
- **Kürzung nur im Terminal:** Tabellenzellen werden nur gekürzt, wenn stdout ein Terminal ist, und dann auf die Terminalbreite mit `…` und einem Hinweis auf stderr. In eine Pipe oder Datei geht immer der volle Text; `--no-truncate` erzwingt ihn auch im Terminal.

### Sonstiges

- Die Dokumentation liegt auf GitHub Pages: <https://seletz.github.io/total-recall/>. Sie ist nach Diátaxis gegliedert in Tutorials, How-To, Referenz und Erklärungen, mit Screenshots aller TUI-Ansichten.
- Ein synthetisches Demo-Archiv (`mise run demo:db`) ist die gemeinsame Grundlage von Tests und Screenshots; die Screenshots prüft die CI gegen eingecheckte Frames.
- Lizenz: MIT.
- Das Binary ist durch die TUI etwa 3,5 MiB größer.

### Bekannt

- Die TUI ist in herdr und über die gerenderten Screenshots geprüft. Andere Terminals (Ghostty, tmux, Terminal.app) sollten funktionieren, sind aber nicht einzeln getestet; Fehlerberichte sind willkommen.
- Feiertage gibt es nur für Deutschland; Urlaub und Krankheit kennt die Aktivitätsansicht nicht.

## v0.1.1 (2026-11-01)

- Hooks: eine vom Harness gemeldete, aber (noch) nicht vorhandene Transkript-Datei ist eine INFO-Zeile im Log, kein Fehler.

## v0.1.0 (2026-11-01)

Erste Version.

- Einlesen der Claude-Code-Transkripte über Hooks (`ingest --hook --detach`), inkrementell und mit Nachlesen beim Session-Start.
- SQLite-Archiv mit Volltextsuche, Schema-Versionierung und Backup vor jeder Migration.
- Befehle `status`, `sessions`, `show`, `search`, `stats`, `sql`, `backup`, `restore`, `doctor`, `hooks print`, `llm --skill`, `config show|init`, `version`, `completion`.
- Maskierung von Geheimnissen vor dem Speichern.
- Git-Root, Remote-URL und Branch je Session, auch für gelöschte Worktrees.
- Releases über goreleaser, Homebrew-Cask im Tap `seletz/homebrew-tap`.

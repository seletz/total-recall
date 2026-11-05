---
title: Einstellungen in der TUI ändern
icon: lucide/sliders-horizontal
---
# Einstellungen in der TUI ändern

Die Ansicht [Config](../reference/30-tui.md#config) der TUI zeigt den Zustand des Archivs, ändert einzelne Werte der Konfiguration in der Datei, startet die Wartungsbefehle und prüft die Installation. Alles, was sie schreibt, fragt vorher nach; die Datei behält ihre Kommentare, und vor jedem Schreiben entsteht `config.toml.bak`.

```sh
total-recall tui config
```

Oder in einer laufenden TUI `4` drücken. Die Seiten wechseln mit ++tab++ und ++shift+tab++.

## Einen Wert ändern

1. Seite **settings** (++tab++ einmal). Mit ++down++ / `j` zum Schlüssel, etwa `reload` unter `[tui]`. Unter der Linie stehen Hilfetext, erlaubte Werte und Default des markierten Schlüssels.
2. ++enter++. Ein Wahrheitswert wie `mouse` kippt sofort, eine Aufzählung wie `view` nimmt den nächsten Wert; bei allem anderen öffnet sich die Eingabezeile mit dem aktuellen Wert.
3. Wert tippen, wie er in der Datei stünde: `10s`, `1h30m`, `04:00`, `~/Backups/total-recall`, eine Liste als `["a", "b"]` oder `a, b`. ++enter++ schreibt, ++esc++ verwirft.
4. Die Meldung sagt, was passiert ist: `tui.reload written to ~/.config/total-recall/config.toml · applied` oder `· takes effect at the next start`. Ein abgelehnter Wert (`want a duration like "20m"`) ändert nichts; die Eingabezeile bleibt offen zum Korrigieren.

`d` setzt den markierten Schlüssel auf den Default zurück: Die Zeile wird in der Datei auskommentiert, nicht gelöscht. Ein Wert mit Herkunft `env` oder `flag` lässt sich hier nicht ändern, weil die Datei dagegen wirkungslos wäre; die Meldung nennt die Quelle.

Für `[accounts]` und `[[worktime.category]]` öffnet `E` die Datei in `$EDITOR`; nach dem Schließen liest die TUI die Konfiguration neu. Fehlt die Datei noch, legt die TUI sie beim ersten Schreiben aus der Vorlage an.

## Eine Aktion ausführen

Seite **actions**: ++down++ zur Aktion, ++enter++. Was liest (`maintenance (dry run)`, `reindex actors (dry run)`, `doctor`), läuft sofort. Was schreibt (`backup`, `maintenance`, `reindex actors`, `ingest all`), zeigt zuerst, was es tun wird, und wartet auf `y`; ++esc++ bricht ab. Die Fortschrittszeilen stehen unter der Liste, ++esc++ bricht eine laufende Aktion ab, am Ende steht die Zusammenfassung mit Dauer. Solange eine Aktion läuft, lädt die TUI nichts nach; danach lädt sie den Status neu und öffnet ein von `maintenance` ausgetauschtes Archiv von selbst.

Zeigt der Status `migration pending`, liegt die Datenbank hinter dem Schema der Binary. Erst `backup`, dann `migrate`; die Aktion `migrate` ist nur dann anwählbar und in dieser Version noch nicht verfügbar.

## Die Installation prüfen

Seite **setup** (++shift+tab++ vom Status aus): sechs Prüfungen, `√`, `!` oder `×` mit Text. Bei zwei bietet ++enter++ eine Korrektur an, nach `y`: die Vorlage der Config-Datei schreiben, das Backup-Verzeichnis anlegen. Fehlen Hooks in `~/.claude/settings.json`, nennt die Zeile die Ereignisse; `c` kopiert den Hook-Block in die Zwischenablage, eingetragen wird er von Hand, siehe [Hooks einrichten](20-hooks-einrichten.md). Die TUI ändert die Einstellungen von Claude Code nie.

---
title: Änderungen
icon: lucide/history
---
# Änderungen

Was sich je Version geändert hat, aus Sicht der Benutzer. Die vollständige Liste der Commits steht bei jedem [Release auf GitHub](https://github.com/seletz/total-recall/releases); der Abschnitt zur jeweiligen Version dieser Seite bildet dort den Kopf der Release Notes.

## v0.2.0

Die Terminal-Oberfläche aus dem [Spike](../explanation/spike-tui.md) ist übernommen, dazu Arbeitszeit und Feiertage in CLI und TUI, Zeitreihen in `stats` und drei neue Config-Abschnitte. Die Datenbank bleibt bei Schema v3; ein Update braucht keine Migration.

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
- `config show` listet die Tastenbelegung als `keys.<aktion>` und meldet Probleme in `[work]`, `[tui]` und `[keys]`; `doctor` meldet sie als Befund.
- [`status`](10-befehle.md#status) zeigt nur noch Fehler der letzten 24 Stunden, mit lokaler Zeit und Alter; ältere erscheinen als Anzahl, `--all-errors` listet sie. Hook-Zeilen über noch nicht vorhandene Transkripte zählen nicht mehr als Fehler. In der JSON-Ausgabe ist `recent_errors` jetzt eine Liste von Objekten (`at`, `message`), neu sind `errors_window`, `errors_since`, `recent_errors_total` und `older_errors`.

### Konfiguration

Neu in der [Konfiguration](20-konfiguration.md): `day_start` (Tagesgrenze der Aktivität je Tag), `[work]` (Kernzeit, Tagessoll, Rahmen je Tag und Woche, Bundesland, Zählweise), `[tui]` (Startansicht, Grafikmodus, Maus, Startzustand von Statistik und Aktivität) und `[keys]` (Tastenbelegung). `config init` schreibt sie kommentiert in die Vorlage.

### Geändertes Verhalten

- **Nur eigene Nachrichten:** `stats --workday` und die Aktivitätsansicht zählen standardmäßig nur menschliche Nachrichten, weil Agent-Events nachts weiterlaufen und die Spanne dehnen. `--all-events`, die Taste `h` oder `count = "all"` in `[work]` zählen alles. `--human-only` ist veraltet und wird noch angenommen.
- **Fokus statt Ansicht:** ++tab++ und ++shift+tab++ wechseln in der Suche nur noch zwischen Eingabe und Liste. Die Ansichten wechseln `1`, `2` und `3`.
- **Prüfung der Config:** `[work]`, `[tui]` und `[keys]` werden erst geprüft, wenn ein Befehl sie braucht. Ein Fehler dort bricht `tui` und `stats --workday` ab, stoppt aber nie Hooks und `ingest`.

### Sonstiges

- Die Dokumentation liegt auf GitHub Pages: <https://seletz.github.io/total-recall/>.
- Lizenz: MIT.
- Das Binary ist durch die TUI etwa 3,5 MiB größer.

### Bekannt

- Die Kitty-Grafik der Statistik ist in echten Terminals noch nicht vollständig geprüft ([#40](https://github.com/seletz/total-recall/issues/40)); der Textpfad ist davon unabhängig.
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

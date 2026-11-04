---
title: Änderungen
icon: lucide/history
---
# Änderungen

Was sich je Version geändert hat, aus Sicht der Benutzer. Die vollständige Liste der Commits steht bei jedem [Release auf GitHub](https://github.com/seletz/total-recall/releases); der Abschnitt zur jeweiligen Version dieser Seite bildet dort den Kopf der Release Notes.

## v1.1.0 (unveröffentlicht)

### TUI

- In der Ansicht **Aktivität** blättern ++up++/++down++ (`k`/`j`) und das Mausrad durch die Zeit: Passen nicht alle Tage auf den Bildschirm, scrollen sie wie bisher die Zeilen, am Rand rückt das Fenster um einen Tag zurück bzw. vor statt stehenzubleiben. Woche und Monat werden so zu einem gleitenden Fenster gleicher Länge; Kopfzeile, Soll, Saldo und Kennzahlen gelten für die sichtbaren Tage, Kalenderwoche oder Monat stehen nur bei einem ganzen. `c`, `l`, `d`, `w`, `m` richten wieder am Kalender aus. ++left++/++right++ springen weiter um eine ganze Periode, aber nicht mehr über die laufende hinaus; auch ++down++ endet dort. Die Hilfe nennt die Tasten `day back (scroll)` und `day forward (scroll)`: [TUI](30-tui.md#aktivitat) ([#125](https://github.com/seletz/total-recall/issues/125)).

### Release

- Der Release-Lauf prüft den Download von goreleaser jetzt kryptografisch: Vor jedem goreleaser-Schritt in `ci.yml`, `release.yml` und `signed-snapshot.yml` installiert `sigstore/cosign-installer` (v4.1.2) cosign, womit `goreleaser-action` die Sigstore-Signatur der Checksummen-Datei des goreleaser-Releases prüft statt nur die Prüfsumme; eine fehlgeschlagene Prüfung bricht den Lauf ab. goreleaser ist in allen Workflows auf v2.19.0 gepinnt, die Version steht in `mise.toml`, und `mise run tools:check` (auch in der CI) findet Abweichungen in den Workflows: [Release-Assets](50-release.md#werkzeuge-des-release-laufs) ([#121](https://github.com/seletz/total-recall/issues/121)).

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

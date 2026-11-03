# total-recall

Archiviert Sessions von Coding-Agenten (zuerst Claude Code) in eine lokale SQLite-Datenbank und macht sie auswertbar, für Menschen und für Agenten. Claude Code löscht alte Transkripte; `total-recall` liest sie über Hooks laufend ein und hebt sie auf.

![total-recall tui: Volltextsuche mit Vorschau](docs/assets/screenshots/search.png)

Dokumentation: <https://seletz.github.io/total-recall/> (Quelle unter [docs/](docs/), nach [Diátaxis](https://diataxis.fr/) gegliedert; Spezifikation in [docs/explanation/design.md](docs/explanation/design.md), Änderungen je Version in [docs/reference/90-aenderungen.md](docs/reference/90-aenderungen.md)).

## Was es kann

- **Einlesen über Hooks**, inkrementell und ohne Daemon; beim Session-Start wird alles nachgelesen, was fehlt. Geheimnisse werden vor dem Speichern maskiert, die Datenbank bleibt auf dem Rechner.
- **Befehle:** `sessions`, `show`, `search` (Volltext über FTS5), `stats` (je Tag, Repo und Wochentag × Stunde; Zeitreihen mit `--period`/`--by`; Arbeitszeit je Tag mit `--workday`; Arbeitszeit je Projekt für Stundenzettel mit `--worktime`), `sql` (nur lesend), `backup`/`restore`, `doctor` für Format-Drift. Alle lesenden Befehle haben `--json`.
- **TUI** (`total-recall tui`): Suche mit Vorschau beim Tippen und Sprung in die Session, Statistik als Chart (Text oder Kitty Graphics), Tabelle und Heatmap, erste und letzte Aktivität je Tag mit Kernzeit, Soll und Feiertagen. Mausbedienung, Theme nach Terminalhintergrund, Tasten für ein deutsches Mac-Layout und über `[keys]` umbelegbar.
- **Für Agenten:** `total-recall llm --skill` gibt eine Anleitung mit Schema und Beispiel-SQL aus.

## Installation

```sh
brew install --cask seletz/tap/total-recall
```

Solange das Repository privat ist, geht nur der Weg aus dem Quellcode: `mise run build` baut `./total-recall`. Details, Hooks und erstes Einlesen: [Installieren](https://seletz.github.io/total-recall/how-to/10-installieren/) und [Hooks einrichten](https://seletz.github.io/total-recall/how-to/20-hooks-einrichten/); einmal von vorn bis hinten: [Erste Schritte](https://seletz.github.io/total-recall/tutorials/erste-schritte/).

## Entwicklung

```sh
mise run build        # Binary bauen
mise run test         # go test ./...
mise run lint         # go vet und golangci-lint
mise run docs:serve   # Doku lokal mit Zensical
mise run docs:screenshots        # Screenshots der TUI neu erzeugen (nach jeder TUI-Änderung)
mise run docs:screenshots:check  # prüft, ob die eingecheckten Screenshots aktuell sind (läuft in der CI)
```

## Lizenz

MIT, siehe [LICENSE](LICENSE).

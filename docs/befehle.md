# Befehle

Überblick über alle Befehle. Was noch nicht fertig ist, ist als **geplant** markiert. Optionen und Ausgabe können sich bis zur Fertigstellung noch ändern.

## Globale Optionen

| Option | Bedeutung |
|---|---|
| `--config PFAD` | andere Config-Datei |
| `--db PFAD` | andere Datenbank |
| `--json` | Ausgabe als JSON (bei allen lesenden Befehlen) |
| `--tz ZONE` | Zeitzone für Auswertungen, Default `Europe/Berlin` |

Zeiten in der Ausgabe sind in lokaler Zeit (`--tz`). In der Datenbank stehen sie in UTC.

## `ingest` { #ingest }

!!! info "geplant"

Liest Transkripte in die Datenbank ein.

```sh
total-recall ingest ~/.claude/projects/-Users-me-repo/abc.jsonl   # einzelne Datei
total-recall ingest --all                                         # alle Quellen nachlesen
total-recall ingest --hook --detach < hook.json                   # aus einem Hook
```

- `--all`: alle Quellen aller Harnesses. Nur Neues wird gelesen.
- `--hook`: Hook-JSON von stdin. Bei `SessionStart` zusätzlich `--all`.
- `--detach`: im Hintergrund weiterlaufen, sofort mit 0 zurückkehren.

Mehr dazu: [Hooks einrichten](hooks.md).

## `status` { #status }

!!! info "geplant"

Zeigt den Zustand des Archivs: DB-Pfad und Größe, Anzahl Sessions und Events, letzter Ingest je Harness, letzte Fehler aus dem Log, letztes Backup.

```sh
total-recall status
total-recall status --json
```

## `sessions` { #sessions }

!!! info "geplant"

Listet Sessions. Spalten: Start, Dauer, Name, Repo, Branch, Anzahl menschlicher und Agenten-Events.

```sh
total-recall sessions --since 2026-10-01 --until 2026-10-31
total-recall sessions --repo github.com/seletz/total-recall
total-recall sessions --name release --json
```

Filter: `--since`, `--until`, `--repo`, `--harness`, `--name`.

## `show` { #show }

!!! info "geplant"

Zeigt den Verlauf einer Session: deine Anfragen und die Antworten des Agenten.

```sh
total-recall show claude:f2b299a1-b3c1-4cf8-b1d7-8c4840eab467
total-recall show claude:f2b299a1-… --all     # inklusive Tool-Aufrufe und -Ergebnisse
```

Die Session-ID hat die Form `<harness>:<native id>`. Du findest sie mit `sessions`.

## `search` { #search }

!!! info "geplant"

Volltextsuche (SQLite FTS5) mit Textausschnitten.

```sh
total-recall search "goreleaser"
total-recall search "cask AND homebrew" --since 2026-10-01
total-recall search "redaction" --actor human --repo github.com/seletz/total-recall
```

Filter wie bei `sessions`, dazu `--actor` (`human`, `agent`, `peer`, `automation`, `tool`). Die Suchsyntax ist die von FTS5: `AND`, `OR`, `NOT`, `"genaue Phrase"`, `präfix*`.

## `stats` { #stats }

!!! info "geplant"

Kennzahlen für einen Zeitraum: je Tag, je Wochentag × Stunde und je Repo. Gezählt werden Sessions, menschliche und Agenten-Events. Tage und Stunden in lokaler Zeit.

```sh
total-recall stats --month 2026-10
total-recall stats --since 2026-10-01 --until 2026-10-15 --json
```

Die JSON-Ausgabe ist die Grundlage für `monthly_stats.py` im Vault.

## `sql` { #sql }

!!! info "geplant"

Führt eine SQL-Abfrage aus, nur lesend. Die Datenbank wird mit `mode=ro` und `PRAGMA query_only` geöffnet. Ändern ist nicht möglich.

```sh
total-recall sql "SELECT count(*) FROM sessions"
total-recall sql --json "SELECT id, name FROM sessions LIMIT 5"
```

Ausgabe als Tabelle, JSON oder CSV. Beispiele: [Auswertung](auswertung.md).

## `backup` { #backup }

!!! info "geplant"

Schreibt eine konsistente Kopie der Datenbank (`VACUUM INTO`) nach `~/Documents/total-recall/archive-YYYY-MM-DD.db`. Danach werden alte Kopien nach den Regeln in `[backup]` aufgeräumt.

```sh
total-recall backup
total-recall backup --if-older 24h       # nur, wenn das letzte Backup älter ist
```

## `hooks print` { #hooks-print }

!!! info "geplant"

Gibt die Hook-Konfiguration als JSON aus.

```sh
total-recall hooks print --harness claude
```

## `llm --skill` { #llm }

!!! info "geplant"

Gibt die eingebettete Anleitung für Agenten aus (`SKILL.md`). Siehe [Für Agenten](agenten.md).

```sh
total-recall llm --skill
```

## `config show` / `config init` { #config }

!!! info "geplant"

```sh
total-recall config show     # wirksame Konfiguration mit Herkunft je Wert
total-recall config init     # kommentierte Vorlage schreiben (überschreibt nie)
```

Siehe [Konfiguration](konfiguration.md).

## `version` { #version }

Gibt die Version aus.

```sh
total-recall version
```

## `completion` { #completion }

Erzeugt Shell-Vervollständigung (bash, zsh, fish, powershell). Bei der Installation per Homebrew ist das schon erledigt.

```sh
total-recall completion zsh > "${fpath[1]}/_total-recall"
```

---
title: Analyse-Tool
icon: lucide/chart-network
---
# Analyse-Tool

`total-recall-analytics` ist ein eigenes Programm neben `total-recall`. Es wertet die Archive eines oder mehrerer Hosts mit [DuckDB](https://duckdb.org/) aus: Es liest die Archive schreibgeschützt, baut daraus eine Analyse-Datenbank (Star-Schema) und beantwortet Fragen über alle Hosts eines Agenten-Pools. Die Archive selbst ändert es nie.

Diese Seite wächst mit dem Meilenstein v1.11.0. Bisher gibt es das Programm selbst mit dem Befehl [`version`](#version); die Befehle zum Bauen und Abfragen der Analyse-Datenbank folgen. Installieren: [Installieren, Analyse-Tool](../how-to/10-installieren.md#analyse-tool).

## Aufbau

- **Eigenes Binary.** DuckDB kommt über den Go-Treiber [`github.com/duckdb/duckdb-go/v2`](https://github.com/duckdb/duckdb-go) (DuckDB v2.0.1), der eine C-Bibliothek einbindet und deshalb cgo braucht. `total-recall` bleibt statisch und cgo-frei; das Analyse-Tool ist ein eigenes Go-Modul im Verzeichnis `analytics/` des Repos und hat eigene Build- und Release-Regeln (siehe [Release-Assets](50-release.md#analyse-tool)).
- **Archive über die SQLite-Erweiterung.** DuckDB liest SQLite-Dateien über seine Erweiterung `sqlite_scanner` und hängt jedes Archiv mit `ATTACH … (TYPE sqlite, READ_ONLY)` an. Schreiben in ein angehängtes Archiv lehnt DuckDB ab.
- **Kein Netz.** Die Erweiterung ist nicht Teil der DuckDB-Bibliothek, DuckDB würde sie sonst beim ersten Gebrauch von `extensions.duckdb.org` laden. Das Analyse-Tool trägt sie stattdessen im Binary: Beim Bauen lädt `scripts/analytics-ext.sh` die Erweiterung für die Zielplattform und prüft sie gegen die SHA-256-Summen in `analytics/internal/duck/ext.lock`; `go build` bettet sie ein. Zur Laufzeit schreibt das Programm sie einmal in sein Cache-Verzeichnis (unten) und lädt sie von dort. Bei jedem Start vergleicht es die Datei mit der festgelegten Prüfsumme und schreibt sie neu, wenn sie fehlt oder abweicht; DuckDB prüft beim Laden zusätzlich die Signatur der Erweiterung. Automatisches Installieren und Laden von Erweiterungen ist abgeschaltet, nur signierte Erweiterungen sind erlaubt, und DuckDB sucht Erweiterungen nur im Cache-Verzeichnis. Ein `~/.duckdb`, das eine DuckDB-CLI angelegt hat, spielt keine Rolle.
- **Cache-Verzeichnis** der Erweiterung: `~/Library/Caches/total-recall-analytics/duckdb-<version>/<plattform>/` auf macOS, `~/.cache/total-recall-analytics/duckdb-<version>/<plattform>/` auf Linux (`$XDG_CACHE_HOME`, falls gesetzt). Es kann jederzeit gelöscht werden; der nächste Start schreibt die Erweiterung neu (etwa 40 MB).
- **Plattformen:** macOS arm64 und amd64 (signiert und notarisiert), Linux amd64 (gegen glibc ab 2.35 und libstdc++ gelinkt; Alpine/musl geht nicht).
- **Ausgabe** wie bei `total-recall`: `--json` gibt ein eingerücktes Dokument mit allen dokumentierten Schlüsseln aus, ein Fehler im JSON-Modus ist ein Objekt `{"error":{"kind":"…","message":"…"}}` auf stdout, die Meldung steht zusätzlich auf stderr, Exit-Code 1. `kind` ist `usage` (Flags, Argumente), `db` (Datenbank oder Archiv nicht zu öffnen), `sql` (DuckDB lehnt eine Abfrage ab) oder `internal`. Siehe [Ausgabevertrag](../explanation/design.md#ausgabevertrag-der-lesebefehle).

## Befehle

### version

```
total-recall-analytics version [--json]
```

Zeigt die Version des Programms, die gelinkte DuckDB-Version mit Plattform und die geladenen Erweiterungen. Weil der Befehl DuckDB öffnet und die eingebettete Erweiterung lädt, ist er zugleich die Probe, ob das Programm auf dem Rechner läuft.

| Option | Bedeutung |
|---|---|
| `--json` | Ausgabe als JSON |

Beispiel (Linux, Benutzer `mara`):

```console
$ total-recall-analytics version
total-recall-analytics  1.11.0
DuckDB                  v2.0.1 (linux_amd64)
extension               sqlite_scanner ce0987fda4, embedded, loaded from /home/mara/.cache/total-recall-analytics/duckdb-v2.0.1/linux_amd64/sqlite_scanner.duckdb_extension
```

```console
$ total-recall-analytics version --json
{
  "version": "1.11.0",
  "duckdb": "v2.0.1",
  "platform": "linux_amd64",
  "extensions": [
    {
      "name": "sqlite_scanner",
      "version": "ce0987fda4",
      "path": "/home/mara/.cache/total-recall-analytics/duckdb-v2.0.1/linux_amd64/sqlite_scanner.duckdb_extension",
      "embedded": true
    }
  ]
}
```

| Schlüssel | Inhalt |
|---|---|
| `version` | Version des Programms (`dev` bei einem Build ohne Release) |
| `duckdb` | Version der gelinkten DuckDB-Bibliothek |
| `platform` | DuckDB-Plattform (`osx_arm64`, `osx_amd64`, `linux_amd64`) |
| `extensions` | geladene Erweiterungen, die nicht fest in DuckDB stecken; nie `null` |
| `extensions[].name` | Name der Erweiterung |
| `extensions[].version` | Version der Erweiterung (Commit von DuckDB) |
| `extensions[].path` | Datei, aus der sie geladen wurde |
| `extensions[].embedded` | `true`, wenn sie aus dem Binary stammt |

Ein Fehler im JSON-Modus:

```console
$ total-recall-analytics version --json --bogus
total-recall-analytics: unknown flag: --bogus
{"error":{"kind":"usage","message":"unknown flag: --bogus"}}
```

## TUI

Das Analyse-Tool hat noch keine Bedienung im Terminal; sie kommt mit [#366](https://github.com/seletz/total-recall/issues/366) im selben Meilenstein. Die TUI von `total-recall` zeigt auf der Einstellungsseite nur ihre eigene Version, nicht die des Analyse-Tools; dessen Version zeigt `total-recall-analytics version`.

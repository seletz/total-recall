---
title: Analyse-Tool
icon: lucide/chart-network
---
# Analyse-Tool

`total-recall-analytics` ist ein eigenes Programm neben `total-recall`. Es wertet die Archive eines oder mehrerer Hosts mit [DuckDB](https://duckdb.org/) aus: Es liest die Archive schreibgeschützt, baut daraus eine Analyse-Datenbank (Star-Schema) und beantwortet Fragen über alle Hosts eines Agenten-Pools. Die Archive selbst ändert es nie.

Diese Seite wächst mit dem Meilenstein v1.11.0. Bisher gibt es das Programm selbst mit dem Befehl [`version`](#version); die Befehle zum Bauen und Abfragen der Analyse-Datenbank folgen. Installieren: [Installieren, Analyse-Tool](../how-to/10-installieren.md#analyse-tool).

## Aufbau

- **Eigenes Binary.** DuckDB kommt über den Go-Treiber [`github.com/duckdb/duckdb-go/v2`](https://github.com/duckdb/duckdb-go) (DuckDB v2.0.1), der eine C++-Bibliothek einbindet und deshalb cgo braucht. `total-recall` bleibt statisch und cgo-frei; das Analyse-Tool ist ein eigenes Go-Modul im Verzeichnis `analytics/` des Repos und hat eigene Build- und Release-Regeln (siehe [Release-Assets](50-release.md#analyse-tool)).
- **Archive über die SQLite-Erweiterung.** DuckDB liest SQLite-Dateien über seine Erweiterung `sqlite_scanner` und hängt jedes Archiv mit `ATTACH … (TYPE sqlite, READ_ONLY)` an. Schreiben in ein angehängtes Archiv lehnt DuckDB ab.
- **Erweiterungen eingebaut, nichts wird nachgeladen.** `sqlite_scanner` gehört nicht zur fertigen DuckDB-Bibliothek; DuckDB würde sie sonst beim ersten Gebrauch von `extensions.duckdb.org` laden. Das Analyse-Tool linkt stattdessen ein DuckDB, das beim Bauen aus dem Quellcode entsteht (`scripts/analytics-duckdb.sh`), mit `sqlite_scanner`, `parquet`, `json`, `icu` (Zeitzonen) und `core_functions` fest eingebaut. Version und Commit stehen in `analytics/duckdb/duckdb.lock`, die Erweiterungen samt Commit von `sqlite_scanner` in `analytics/duckdb/extensions.cmake`. Dieses DuckDB kann zur Laufzeit keine Erweiterung laden oder installieren: `LOAD` und `INSTALL` enden mit `Permission Error: … this build does not link the loadable_extensions library`. Es gibt also keinen Download, keine Erweiterungsdatei auf der Platte und kein `dlopen`; ein `~/.duckdb` einer DuckDB-CLI spielt keine Rolle. Zusätzlich ist automatisches Installieren und Laden abgeschaltet.
- **Plattformen:** macOS arm64 und amd64 (signiert und notarisiert, Hardened Runtime ohne Ausnahmen), Linux amd64 (gegen glibc und libstdc++ von Debian 12 gelinkt; Alpine/musl geht nicht).
- **Ausgabe** wie bei `total-recall`: `--json` gibt ein eingerücktes Dokument mit allen dokumentierten Schlüsseln aus, ein Fehler im JSON-Modus ist ein Objekt `{"error":{"kind":"…","message":"…"}}` auf stdout, die Meldung steht zusätzlich auf stderr, Exit-Code 1. `kind` ist `usage` (Flags, Argumente), `db` (Datenbank oder Archiv nicht zu öffnen), `sql` (DuckDB lehnt eine Abfrage ab) oder `internal`. Siehe [Ausgabevertrag](../explanation/design.md#ausgabevertrag-der-lesebefehle).

## Befehle

### version

```
total-recall-analytics version [--json]
```

Zeigt die Version des Programms, die gelinkte DuckDB-Version mit Commit und Plattform und die eingebauten Erweiterungen. Weil der Befehl DuckDB öffnet, ist er zugleich die Probe, ob das Programm auf dem Rechner läuft.

| Option | Bedeutung |
|---|---|
| `--json` | Ausgabe als JSON |

Beispiel (Linux, Snapshot-Build, in einem Container ohne Netz):

```console
$ total-recall-analytics version
total-recall-analytics  1.9.1-SNAPSHOT-1120ad0
DuckDB                  v2.0.1 (8d78114e90, linux_amd64)
extensions              core_functions, icu, json, parquet, sqlite_scanner (compiled in)
```

```console
$ total-recall-analytics version --json
{
  "version": "1.9.1-SNAPSHOT-1120ad0",
  "duckdb": "v2.0.1",
  "duckdb_source": "8d78114e90",
  "platform": "linux_amd64",
  "extensions": [
    {
      "name": "core_functions",
      "version": "v2.0.1",
      "static": true
    },
    {
      "name": "icu",
      "version": "v2.0.1",
      "static": true
    },
    {
      "name": "json",
      "version": "v2.0.1",
      "static": true
    },
    {
      "name": "parquet",
      "version": "v2.0.1",
      "static": true
    },
    {
      "name": "sqlite_scanner",
      "version": "ce0987fda4",
      "static": true
    }
  ]
}
```

| Schlüssel | Inhalt |
|---|---|
| `version` | Version des Programms (`dev` bei einem Build ohne Release) |
| `duckdb` | Version der gelinkten DuckDB |
| `duckdb_source` | Commit von DuckDB, aus dem sie gebaut ist (gekürzt) |
| `platform` | DuckDB-Plattform (`osx_arm64`, `osx_amd64`, `linux_amd64`) |
| `extensions` | geladene Erweiterungen; nie `null` |
| `extensions[].name` | Name der Erweiterung |
| `extensions[].version` | Version der Erweiterung: die DuckDB-Version bei denen aus dem DuckDB-Repository, sonst der Commit (`sqlite_scanner`) |
| `extensions[].static` | `true`: fest eingebaut; dieses Programm lädt keine anderen |

Ein Fehler im JSON-Modus:

```console
$ total-recall-analytics version --json --bogus
total-recall-analytics: unknown flag: --bogus
{"error":{"kind":"usage","message":"unknown flag: --bogus"}}
```

## TUI

Das Analyse-Tool hat noch keine Bedienung im Terminal; sie kommt mit [#366](https://github.com/seletz/total-recall/issues/366) im selben Meilenstein. Die TUI von `total-recall` zeigt auf der Einstellungsseite nur ihre eigene Version, nicht die des Analyse-Tools; dessen Version zeigt `total-recall-analytics version`.

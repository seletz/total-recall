---
title: Analyse-Tool
icon: lucide/chart-network
---
# Analyse-Tool

`total-recall-analytics` ist ein eigenes Programm neben `total-recall`. Es wertet die Archive eines oder mehrerer Hosts mit [DuckDB](https://duckdb.org/) aus: Es liest die Archive schreibgeschützt, baut daraus eine Analyse-Datenbank (Star-Schema) und beantwortet Fragen über alle Hosts eines Agenten-Pools. Die Archive selbst ändert es nie.

Diese Seite wächst mit dem Meilenstein v1.11.0. Bisher gibt es das Programm selbst mit dem Befehl [`version`](#version) und das Schema der Analyse-Datenbank mit seinen [Views](#views-vertrag-v1); die Befehle zum Bauen und Abfragen folgen. Installieren: [Installieren, Analyse-Tool](../how-to/10-installieren.md#analyse-tool).

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

## Views (Vertrag v1)

Die Views `analytics_*_v1` sind die einzige Schnittstelle der Analyse-Datenbank für Abfragen, Notebooks und BI-Werkzeuge; die Tabellen dahinter (Dimensionen, Fakten, Marts) können sich ändern. Spalten und Typen stehen in `analytics/internal/star/project/contract.txt` und werden nach jedem Lauf geprüft: Weicht eine View ab, bricht der Lauf ab und die Datenbank bleibt, wie sie war. Spalten kommen nur dazu; ein Bruch ergibt eine View `_v2` neben `_v1`. Zeiten sind UTC (`TIMESTAMP`), lokale Tage und Stunden in der primären Zone des Laufs; andere Zonen über `analytics_time_slot_v1` und `slot_key` (Unix-Sekunden / 900). Warum das Schema so aussieht: [Analyse-Schema](../explanation/analyse-schema.md).

### `analytics_daily_v1`

lokaler Tag × Host × Projekt.

| Spalte | Typ |
|---|---|
| `day_local` | `DATE` |
| `host` | `VARCHAR` |
| `project` | `VARCHAR` |
| `sessions` | `BIGINT` |
| `human_prompts` | `BIGINT` |
| `agent_events` | `BIGINT` |
| `tool_calls` | `BIGINT` |
| `tokens_in` | `BIGINT` |
| `tokens_out` | `BIGINT` |

### `analytics_events_v1`

eine Zeile je Event (Fakt `fact_event`), lokale Zeit in der primären Zone.

| Spalte | Typ |
|---|---|
| `event_id` | `VARCHAR` |
| `session_id` | `VARCHAR` |
| `host` | `VARCHAR` |
| `project` | `VARCHAR` |
| `repo` | `VARCHAR` |
| `is_subagent` | `BOOLEAN` |
| `ts` | `TIMESTAMP` |
| `day_local` | `DATE` |
| `hour_local` | `INTEGER` |
| `weekday_local` | `INTEGER` |
| `slot_key` | `BIGINT` |
| `kind` | `VARCHAR` |
| `role` | `VARCHAR` |
| `actor` | `VARCHAR` |
| `input_kind` | `VARCHAR` |
| `tool_name` | `VARCHAR` |
| `tool_kind` | `VARCHAR` |
| `mcp_server` | `VARCHAR` |
| `model` | `VARCHAR` |
| `model_family` | `VARCHAR` |
| `harness` | `VARCHAR` |
| `account_name` | `VARCHAR` |
| `account_group` | `VARCHAR` |
| `tokens_in` | `BIGINT` |
| `tokens_out` | `BIGINT` |
| `tokens_cache_read` | `BIGINT` |
| `tokens_cache_write` | `BIGINT` |
| `tokens_fresh` | `BIGINT` |
| `tokens_unsplit` | `BIGINT` |

### `analytics_project_week_v1`

ISO-Woche × Projekt.

| Spalte | Typ |
|---|---|
| `iso_year` | `INTEGER` |
| `iso_week` | `INTEGER` |
| `project` | `VARCHAR` |
| `sessions` | `BIGINT` |
| `active_days` | `BIGINT` |
| `human_events` | `BIGINT` |
| `agent_events` | `BIGINT` |
| `tool_calls` | `BIGINT` |
| `tokens_in` | `BIGINT` |
| `tokens_out` | `BIGINT` |

### `analytics_session_month_v1`

lokaler Monat des Session-Beginns × Projekt.

| Spalte | Typ |
|---|---|
| `month` | `VARCHAR` |
| `project` | `VARCHAR` |
| `sessions` | `BIGINT` |
| `subagent_sessions` | `BIGINT` |
| `avg_duration_min` | `DOUBLE` |
| `total_duration_h` | `DOUBLE` |
| `human_events` | `BIGINT` |
| `agent_events` | `BIGINT` |
| `agent_per_human` | `DOUBLE` |
| `model_switch_sessions` | `BIGINT` |

### `analytics_sessions_v1`

eine Zeile je Session (Fakt `fact_session`).

| Spalte | Typ |
|---|---|
| `session_id` | `VARCHAR` |
| `host` | `VARCHAR` |
| `harness` | `VARCHAR` |
| `harness_version` | `VARCHAR` |
| `project` | `VARCHAR` |
| `repo` | `VARCHAR` |
| `git_branch` | `VARCHAR` |
| `model` | `VARCHAR` |
| `account_id` | `VARCHAR` |
| `account_name` | `VARCHAR` |
| `account_group` | `VARCHAR` |
| `parent_id` | `VARCHAR` |
| `is_subagent` | `BOOLEAN` |
| `started_at` | `TIMESTAMP` |
| `ended_at` | `TIMESTAMP` |
| `duration_s` | `BIGINT` |
| `events` | `BIGINT` |
| `human_events` | `BIGINT` |
| `agent_events` | `BIGINT` |
| `tool_calls` | `BIGINT` |
| `distinct_models` | `BIGINT` |
| `model_switches` | `BIGINT` |
| `tokens_in` | `BIGINT` |
| `tokens_out` | `BIGINT` |

### `analytics_time_slot_v1`

die Zeitzonen-Brücke: Zone × UTC-Viertelstunde → lokaler Tag, Stunde, Wochentag.

| Spalte | Typ |
|---|---|
| `tz` | `VARCHAR` |
| `slot_key` | `BIGINT` |
| `local_date` | `DATE` |
| `local_hour` | `INTEGER` |
| `iso_weekday` | `INTEGER` |

### `analytics_tokens_day_v1`

lokaler Tag × Modell, nur Events mit Token-Angaben.

| Spalte | Typ |
|---|---|
| `day_local` | `DATE` |
| `model` | `VARCHAR` |
| `model_family` | `VARCHAR` |
| `events` | `BIGINT` |
| `tokens_in` | `BIGINT` |
| `tokens_fresh` | `BIGINT` |
| `tokens_cache_read` | `BIGINT` |
| `tokens_cache_write` | `BIGINT` |
| `tokens_unsplit` | `BIGINT` |
| `tokens_out` | `BIGINT` |

### `analytics_tool_month_v1`

lokaler Monat × Projekt × Tool.

| Spalte | Typ |
|---|---|
| `month` | `VARCHAR` |
| `project` | `VARCHAR` |
| `tool_name` | `VARCHAR` |
| `tool_kind` | `VARCHAR` |
| `mcp_server` | `VARCHAR` |
| `calls` | `BIGINT` |

### `analytics_weekhour_v1`

Wochentag × Stunde (lokal), human- und agent-Events.

| Spalte | Typ |
|---|---|
| `iso_weekday` | `INTEGER` |
| `local_hour` | `INTEGER` |
| `human_events` | `BIGINT` |
| `agent_events` | `BIGINT` |

## TUI

Das Analyse-Tool hat noch keine Bedienung im Terminal; sie kommt mit [#366](https://github.com/seletz/total-recall/issues/366) im selben Meilenstein. Die TUI von `total-recall` zeigt auf der Einstellungsseite nur ihre eigene Version, nicht die des Analyse-Tools; dessen Version zeigt `total-recall-analytics version`.

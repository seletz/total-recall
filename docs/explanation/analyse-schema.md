---
title: Analyse-Schema
icon: lucide/chart-network
---
# Analyse-Schema

Wie das [Analyse-Tool](../reference/15-analyse.md) `total-recall-analytics` die Archive eines Agenten-Pools für Auswertungen aufbereitet und warum so. Die Spalten der Schnittstelle stehen in der Referenz unter [Views](../reference/15-analyse.md#views-vertrag-v1).

## Das Archiv bleibt die Quelle

Die Analyse-Datenbank ist eine DuckDB-Datei neben den Archiven, keine zweite Wahrheit. Jedes Archiv wird nur gelesen (`ATTACH … READ_ONLY`), und alles in der Analyse-Datenbank lässt sich jederzeit mit einem vollständigen Lauf neu bauen. Deshalb gibt es keine Historie von Dimensionen (kein SCD 2) und keine Sicherung der Analyse-Datenbank: Das Archiv ist der Audit-Trail.

## Korn

| Fakt | Korn | natürlicher Schlüssel | Kennzahlen |
|---|---|---|---|
| `fact_event` | ein Event des Archivs (Nachricht, Tool-Aufruf, Tool-Ergebnis) | `events.id` | Anzahl, Tokens: Input, frisch, Cache-Lesen, Cache-Schreiben, ohne Aufteilung, Output |
| `fact_session` | eine Session | `sessions.id` | Beginn und Ende (erstes und letztes Event, UTC), Dauer, Events, human/agent, Tool-Aufrufe, Modelle, Modellwechsel, Tokens, Subagent |

`fact_session` wird für jede Session, die ein Lauf berührt, aus `fact_event` neu gerechnet; beide Fakten stimmen deshalb immer überein (Test `relationships_fact_session`).

## Arbeitszeit

Die Arbeitszeit ist keine Summe über Events, sondern eine Schätzung aus den Lücken zwischen eigenen Prompts, mit Höchstlücke, Mindestgutschrift, Rundung und Kategorien aus `[worktime]`. Sie wird nicht in SQL nachgebaut: Jeder Lauf ruft dieselbe Go-Funktion wie `total-recall stats --worktime` (`internal/worktime`) mit den human-Events aus `fact_event` auf und schreibt zwei weitere Fakten:

| Fakt | Korn | Kennzahlen |
|---|---|---|
| `fact_work_slice` | Arbeitstag × Kategorie × Session | gutgeschriebene Sekunden |
| `fact_work_day` | Arbeitstag × Kategorie | gutgeschriebene und zu buchende (gerundete) Sekunden |

Gerechnet wird über **alle Quellen zusammen**, nicht je Host: Eine Lücke gehört zur Person. Wer zwischen zwei Rechnern wechselt oder auf beiden gleichzeitig Agenten laufen lässt, bekommt die Zeit dazwischen einmal gutgeschrieben, nicht je Host. Je Host aufteilen lässt sich die Zeit trotzdem: Jede Scheibe gehört zu einer Session und damit zu deren Host und Projekt. Die Summe je Host im Pool ist deshalb nicht dieselbe wie `stats --worktime` auf dem Archiv eines einzelnen Hosts, wenn sich die Arbeit zeitlich überlappt. Gerundet wird wie in `stats` je Arbeitstag und Kategorie; je Projekt oder Host gibt es nur die gutgeschriebene Zeit. Die Fakten werden bei jedem Lauf ganz neu gerechnet, weil eine neue Quelle die Lücken aller anderen verändern kann.

## Modell

```
                     dim_host      dim_harness     dim_account (Name, Gruppe aus der Konfiguration)
                         \              |              /
 bridge_time_slot ─── fact_event ─── dim_model (Familie)
 (Zone × UTC-¼h →     /  |  |  \
  lokaler Tag,       /   |  |   dim_tool (builtin | mcp, MCP-Server)
  Stunde, Wochentag)/    |  |
   │      dim_session ───┘  |  dim_actor (human, agent, peer, automation, tool)
 dim_date (Eltern, Fortsetzung, dim_event_type (kind × role × input_kind)
           Subagent, Branch) |
                       fact_session ─── dim_project (Name wie stats --by project) ── dim_repo
                             |
 Marts:  mart_daily · mart_project_week · mart_tool_month · mart_session_month · mart_tokens_day · mart_weekhour
                             |
 Views (Vertrag): analytics_{sessions,events,daily,project_week,tool_month,session_month,tokens_day,weekhour,time_slot}_v1
```

- **Gemeinsame Dimensionen:** Beide Fakten teilen sich die Dimensionen, deshalb lassen sich Session- und Event-Kennzahlen nach denselben Merkmalen schneiden (Host, Projekt, Modell, Konto …).
- **Schlüssel sind Hashes**, keine fortlaufenden Nummern: die ersten 8 Byte von MD5 über Dimension und natürlichen Schlüssel (`tr_key`). Jeder Lauf, jeder Host und jede Datenbank (DuckDB, PostgreSQL mit demselben Ausdruck) erzeugt dieselben Schlüssel, ohne beim Einfügen nachzuschlagen.
- **Joins sind LEFT**, und jede Dimension hat ein Mitglied „unbekannt“ (leerer natürlicher Schlüssel): Kein Fakt fällt aus einer Auswertung, weil ein Merkmal fehlt. Der Test `relationships_fact_event` findet trotzdem jeden hängenden Schlüssel.
- **Dimensionen wachsen nur.** Ein Lauf trägt die Schlüssel ein, die er sieht, und löscht nie welche; ein Tool, das nicht mehr vorkommt, behält seinen Schlüssel, und ältere Fakten bleiben gültig. Attribute werden überschrieben.
- **Projekt wie `stats --by project`:** Schlüssel ist der Git-Root der Session (Subagenten: der Eltern-Session), sonst das Arbeitsverzeichnis, mit dem Home-Verzeichnis als `~`. Der Name kommt aus den [Projekt-Definitionen](../how-to/67-projekte-definieren.md) oder wird abgeleitet wie in `stats`; er wird bei jedem Lauf neu berechnet, eine neue Definition benennt also um, ohne ein Fakt anzufassen. **Repo** ist wie `stats` `by_repo` die Remote-URL, sonst der Git-Root, sonst das Arbeitsverzeichnis der Session selbst.

## Zeit

Fakten tragen nur UTC: den Zeitpunkt und die UTC-Viertelstunde (`slot_key`). Lokale Tage, Stunden und Wochentage kommen aus der Brücke `bridge_time_slot`, eine Zeile je Zeitzone und Viertelstunde. Jeder reale Zonenversatz ist ein Vielfaches von 15 Minuten, deshalb sind die Tagesgrenzen exakt, auch über die Sommerzeit. Hosts in verschiedenen Zonen verfälschen so keine Tagessummen: Ausgewertet wird in einer Zone, die primäre Zone der Marts und Views ist die des Laufs, weitere Zonen stehen in der Brücke.

## Laden

Ein Lauf lädt ein Archiv (eine *Quelle*) in einer Transaktion:

1. **Stage:** alle Events jeder Session, die ein Event über dem Wasserstand der Quelle hat (`events.seq`). Die ganze berührte Session wird neu geladen, damit nachträglich geänderte Session-Attribute (Git-Root aufgelöst, Login nachgetragen, Eltern-Session später archiviert) alle ihre Events erreichen.
2. **Dimensionen, Namen, Fakten:** Dimensionen und `fact_event` werden auf ihren Schlüssel ersetzt (löschen und einfügen). Ein erneut geladenes Event, auch dasselbe von einem zweiten Host, ersetzt seine Zeile: Jeder Lauf ist idempotent. Projekt- und Login-Namen rechnet Go aus der Konfiguration.
3. **Kalender, Marts, Views:** werden bei jedem Lauf ganz neu gebaut; sie sind nie die einzige Kopie von etwas.
4. **Prüfen:** Vertrag der Views und Datentests (eindeutige und gesetzte Schlüssel, Beziehungen, erlaubte Werte, Token-Aufteilung, Abdeckung der Brücke, Marts = Fakten). Schlägt etwas fehl, wird die ganze Transaktion verworfen.
5. **Wasserstand** der Quelle und Batch-Zeile (`etl_batch`, `etl_watermark`).

Jede Quelle hat ihren eigenen Wasserstand, weil `seq` je Archiv zählt; mehrere Hosts laden in eine Datenbank. Ein vollständiger Lauf ersetzt nur die Fakten seiner Quelle. Was ein inkrementeller Lauf nicht sieht: Änderungen an Sessions ohne neue Events und ein `reindex --actors`; dafür gibt es den vollständigen Lauf.

Geprüft an einer Kopie des Demo-Archivs (936 Sessions, 21 942 Events): Ein zweiter Lauf ohne neue Events lädt nichts und ändert nichts; ein Archiv, das zwischen zwei Läufen wächst, endet genau dort, wo ein vollständiger Lauf endet; aufgeteilt auf zwei Hosts (207 und 729 Sessions) ergeben beide zusammen dasselbe wie das ganze Archiv.

## Summe gleich Teile

Die Views liefern dieselben Zahlen wie `total-recall stats --json` auf demselben Archiv: Sessions, human- und agent-Events gesamt, je Tag, je Repo, je Wochentag und Stunde; Tokens gesamt, je Tag und je Modell in allen Teilen (frisch, Cache-Lesen, Cache-Schreiben, ohne Aufteilung); Tool-Aufrufe je Tool und je Projekt; Arbeitszeit gesamt, je Tag und Kategorie (gutgeschrieben und gerundet) und je Tag und Projekt wie `stats --worktime --json`. Ein Test vergleicht das bei jedem Build. Eine gewollte Abweichung: `stats --json` gibt ein Projekt ohne Repository mit vollem Pfad aus, die Analyse-Datenbank mit `~`.

## Modelle als SQL-Dateien

Die Modelle sind einfache SQL-Dateien (`analytics/internal/star/project/models/`), je Datei eine Abfrage, in der Schreibweise von [dbt](https://www.getdbt.com/): `{{ config(materialized=…) }}`, `{{ ref('…') }}`, `{{ source('tr', '…') }}`, `{{ var('…') }}`. Das Analyse-Tool baut sie selbst, ohne Python und ohne dbt: Es ordnet die Modelle nach ihren `ref`, ersetzt nur diese vier Konstrukte und lehnt alles andere (Makros, `{% %}`) ab, statt stilles Fehl-SQL zu erzeugen. Datentests sind Abfragen in `tests/`, die die verletzenden Zeilen liefern. `dbt_project.yml` legt dieselben Dateien als dbt-Projekt an, für Lineage und generierte Doku mit dbt-duckdb.

## Datenschutz

Die Analyse-Datenbank enthält keinen Text: keine Nachrichten, keine Titel, keine Tool-Argumente, keine Rohzeilen, keine Anhänge, und keine Volltextsuche. Die einzigen Zeichenketten sind IDs, Namen aus der Konfiguration, Modell- und Tool-Namen, Branch, Remote-URL und Pfade mit `~` statt des Home-Verzeichnisses. Wer Text sucht, nutzt `total-recall search` auf dem Archiv.

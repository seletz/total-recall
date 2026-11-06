---
title: Versionierung
icon: lucide/tag
---
# Versionierung

total-recall folgt [Semantic Versioning](https://semver.org/lang/de/): `MAJOR.MINOR.PATCH`. Ob eine Änderung ein Bruch ist, hängt davon ab, was zur öffentlichen Schnittstelle zählt. Das legt diese Seite fest; die Änderungen je Version stehen in den [Änderungen](90-aenderungen.md).

## Stabil

Ein Bruch an einer dieser Stellen bedeutet eine neue Major-Version:

- **CLI-Befehle und Flags.** Ein Befehl oder ein Flag verschwindet nicht und ändert seine Bedeutung nicht; neue kommen in einer Minor-Version dazu.
- **JSON- und jsonl-Ausgabe.** Die Feldnamen der dokumentierten Befehle ([Befehle](10-befehle.md), Abschnitt *JSON*) und der [Ausgabevertrag](10-befehle.md#ausgabe): jeder dokumentierte Schlüssel ist immer vorhanden, `null` nur bei Strings, Fehler als ein Objekt auf stdout mit stabilem `kind`. Neue Felder kommen in einer Minor-Version dazu; Skripte sollten unbekannte Felder ignorieren.
- **Typen der Suche.** Die Namen der [Typen](10-befehle.md#search-types) (`event`, `session`, `memory` und die reservierten `attachment`, `meta`, `file`), das Flag `--type` mit dem Default `event` und das Feld `type` jedes Treffers. Je Typ gelten seine Felder wie oben: immer vorhanden, neue nur dazu. Ein neuer Typ oder ein reservierter, der durchsuchbar wird, ist Minor; Skripte sollten Treffer eines unbekannten `type` überspringen. Welche Filter für welchen Typ gelten, kann in einer Minor-Version nur wachsen.
- **Config-Schlüssel.** Ein Schlüssel in der [Konfiguration](20-konfiguration.md) wird nicht umbenannt oder entfernt und ändert seine Bedeutung nicht; eine bestehende Datei lädt mit jeder späteren Version derselben Major-Version. Neue Schlüssel sind Minor, ein geänderter Default steht in den Änderungen.
- **Exit-Codes.** `0` Erfolg, `1` Fehler, `2` Befunde bei `doctor`; Hooks enden immer mit `0`.
- **Hooks.** Was [`hooks print`](10-befehle.md#hooks-print) ausgibt, bleibt gültig: Ein eingetragener Hook-Block funktioniert mit jeder späteren Version derselben Major-Version weiter.

## Nicht stabil

Diese Dinge ändern sich auch in einer Minor-Version. Die Änderung steht in den Release Notes, aber sie macht keine neue Major-Version:

- **Datenbankschema, SQL-Views, Format von `raw` und der Blobs.** Eigene Abfragen mit [`sql`](10-befehle.md#sql) gegen die Tabellen und Views aus dem [Schema](40-schema.md) sind eigenes Risiko: Eine Spalte kann wandern (`source_path` wurde in v1.2.0 `source_id`), eine Kodierung kann wechseln (`raw` ist seit v1.2.0 ein komprimierter BLOB). Wer sich darauf verlassen will, nimmt die Befehle mit `--json`.
- **Dateinamen und Format der Backups.** Was `backup` schreibt und wie es heißt; `restore` liest immer alle Formate, die `backup` je geschrieben hat.
- **Bedienung und Layout der TUI, Tastenbelegung.** Ansichten, Seiten, Tasten und Spalten der [TUI](30-tui.md).
- **Interne Go-Pakete.** Alles unter `internal/`; das Modul ist keine Bibliothek.

## Migrationen

Ein neues Schema ist eine **Minor-Version**. Die Begründung: Eine Migration läuft nie automatisch, der Nutzer führt [`total-recall migrate`](10-befehle.md#migrate) selbst aus, nach einem Backup, und sieht vorher den Plan. Wer migriert, hat damit die Release Notes vor sich und entscheidet bewusst; ein Update des Binarys allein ändert am Archiv nichts, und die Hooks schreiben nichts, bis migriert ist ([Design](../explanation/design.md#schema-versionierung)). Die Release Notes sagen, was die Migration tut und wie lange sie dauert. Migrationen sind vorwärts gerichtet: Eine ältere Version öffnet ein neueres Archiv nicht; zurück geht es nur mit der Kopie `archive.db.pre-v<N>.bak` und dem alten Binary.

v1.2.0 nahm diese Regel schon vorweg: `source_path` entfiel, `raw` wurde ein BLOB, die Migration lief zum ersten Mal nur noch von Hand, und die Version blieb Minor.

## Was das für Skripte heißt

- Skripte rufen die Befehle mit `--json` oder `--format jsonl` auf und lesen nur stdout. Diese Ausgabe ändert sich innerhalb einer Major-Version nur durch neue Felder.
- Wer `search` mit mehreren Typen aufruft, verzweigt über `type` und übergeht unbekannte Typen.
- `sql` gehört in ein Skript nur, wenn ein Schemawechsel verkraftbar ist. Vor einem Update die Änderungen lesen; `total-recall sql "SELECT name, sql FROM sqlite_master"` zeigt das aktuelle Schema.
- Skripte, die Backups weiterverarbeiten, sollten die Endung nicht fest annehmen und zum Entpacken [`restore`](10-befehle.md#restore) nehmen statt eines externen Werkzeugs.
- Nach jedem Update `total-recall status` ansehen; `MIGRATION PENDING` heißt, dass eine Minor-Version ein neues Schema gebracht hat, siehe [Aktualisieren](../how-to/10-installieren.md#aktualisieren).

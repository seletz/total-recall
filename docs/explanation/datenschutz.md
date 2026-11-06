---
title: Datenschutz
icon: lucide/shield-check
---
# Datenschutz

Das Archiv enthält alles, was du mit deinen Agenten besprochen hast. Es bleibt auf deinem Rechner. Diese Seite erklärt, was gespeichert wird, was maskiert wird und wo die Dateien liegen.

## Was gespeichert wird

Je **Session**:

- ID, Harness und Version, Name bzw. Titel,
- Arbeitsverzeichnis, Git-Root, Remote-URL (ohne Zugangsdaten) und Branch,
- verwendetes Modell,
- Start und Ende.

Je **Event** (Nachricht, Tool-Aufruf, Tool-Ergebnis):

- Zeitpunkt, Art, Rolle, Actor,
- der lesbare Text. Tool-Ergebnisse werden auf 4 KiB gekürzt.
- Tool-Name, Modell, Token-Zahlen,
- die vollständige Original-JSON-Zeile (`raw`), ebenfalls maskiert und komprimiert gespeichert ([Schema](../reference/40-schema.md#raw)); seit v1.3.0 ohne Größengrenze, damit das Archiv ohne die Transkripte auskommt. Abschaltbar mit `ingest.store_raw = false`, begrenzbar mit `ingest.raw_max_bytes`.
- Bilder und Dokumente, die du in eine Session einfügst oder die ein Werkzeug liest (Screenshots, PDFs), als [Blobs](../reference/40-schema.md#blobs), einmal je Inhalt. **Sie werden nicht maskiert**: Ein Bild lässt sich nicht nach Geheimnissen durchsuchen, ohne es zu zerstören. Ein Screenshot mit einem Passwort oder Token darin liegt also unverändert im Archiv. Abschaltbar mit `ingest.store_blobs = false`; dann bleibt nur der Hash als Verweis.

Je **Meta-Record** (seit Schema v7, Tabelle [`session_meta`](../reference/40-schema.md)): Records der Harness, die keine Nachricht sind, aber zur Session gehören, als ganze JSON-Zeile, maskiert und komprimiert. Bei Claude Code sind das vor allem: der Kontext, den die Harness in eine Runde eingefügt hat (die geladenen `CLAUDE.md`-Dateien, mit `@` erwähnte Dateien, Ausgaben der Hooks, Hinweise auf außerhalb geänderte Dateien, Umgebung), Text, den du getippt hast, während der Agent arbeitete (auch wieder entfernter), Modus und Pull Request der Session, Kosten und Dauer je Modell, der Index der Datei-Historie und das Workflow-Journal. Dateiinhalte stehen damit so im Archiv, wie der Agent sie gesehen hat. Dazu der **System-Prompt und die Tool-Definitionen** jeder Runde (`api-request-shape`, Attachment `prompt_snapshot`): einmal je Inhalt über alle Sessions als [Blob](../reference/40-schema.md#blobs), **maskiert** wie die Zeile (anders als Bilder; `ingest.store_blobs = false` lässt sie deshalb nicht aus). Der System-Prompt enthält, was die Harness beim Start geladen hat: Instruktionsdateien, Umgebung, Pfade, Git-Zustand. Abschaltbar mit `ingest.store_raw = false` (dann gar keine Meta-Records) oder je Typ mit `harness.claude.skip_record_types` (`"api-request-shape"`, `"attachment/prompt_snapshot"`).

**Sicherungskopien bearbeiteter Dateien** (seit v1.3.0): Bevor Claude Code eine Datei ändert, legt es eine Kopie unter `~/.claude/file-history` ab und löscht sie später mit dem Transkript. `total-recall` liest diese Kopien beim Ingest und legt sie einmal je Inhalt als [Blob](../reference/40-schema.md#session_meta) ab. Damit steht der **Quelltext jeder Datei, die ein Agent bearbeitet hat**, im Stand vor der Änderung im Archiv, auch Konfigurationsdateien, `.env`-Dateien und alles, was ein Agent anfasst. Textdateien werden **maskiert** wie der übrige Text (bekannte Token-Formate, Zuweisungen wie `password=…`, private Schlüssel); ein Geheimnis ohne erkennbares Format bleibt stehen. Binärdateien werden nicht maskiert und nur mit `ingest.store_blobs = true` gespeichert. Abschaltbar mit `ingest.store_file_history = false`; dann bleibt nur der Index (Pfad, Version, Zeitpunkt). `total-recall` schreibt nie in `~/.claude`.

Nicht gespeichert werden der API-Request-Cache der Harness (`api-request`, `api-request-blob`: wiederholt nur die Nachrichten), `last-prompt` und die Kataloge und Erinnerungen der Harness (sieben Attachment-Subtypen, siehe [Design](design.md#session_meta)).

## Maskierung von Geheimnissen

Vor dem Schreiben werden `text` und `raw` durchsucht, auch das `raw` der Meta-Records, ihre JSON-Blobs und die Text-Kopien der Datei-Historie (die Bilder, Dokumente und binären Kopien nicht, siehe oben). Treffer werden ersetzt durch `«redacted:<art>»`. Das betrifft unter anderem:

- private Schlüssel (PEM-Blöcke),
- Tokens von AWS, GitHub, Slack, OpenAI, Anthropic,
- JWTs,
- Zuweisungen wie `password=…`, `passwort: …`, `token: …`,
- Zugangsdaten in URLs (`https://user:pass@…`),
- lange, zufällig aussehende Zeichenketten in Zuweisungen.

Nicht maskiert werden zum Beispiel Git-SHAs, UUIDs und `op://`-Referenzen. Die sind keine Geheimnisse.

!!! warning "Grenzen"
    Die Maskierung ist nicht umkehrbar. Sie fängt typische Formate, aber nicht alles. Ein Passwort in Fließtext („das Passwort ist Sommer2026“) erkennt sie nicht. Behandle die Datenbank deshalb wie vertrauliche Daten.

Eigene Muster und Ausnahmen: `[redact]` in der [Konfiguration](../reference/20-konfiguration.md#redact).

## Wo die Dateien liegen

| Was | Ort (macOS) |
|---|---|
| Datenbank | `~/Library/Application Support/total-recall/archive.db` |
| Log | `total-recall.log` neben der Datenbank |
| Backups | `~/Documents/total-recall/archive-YYYY-MM-DD.db.zst` |
| Config | `~/.config/total-recall/config.toml` |

Auf Linux liegt die Datenbank unter `$XDG_DATA_HOME/total-recall/`.

## Warum die Datenbank nicht in iCloud liegt

SQLite schreibt im WAL-Modus. Neben `archive.db` liegen dann `archive.db-wal` und `archive.db-shm`. iCloud synchronisiert diese Dateien unabhängig voneinander und zu beliebigen Zeiten. Das kann die Datenbank beschädigen. Deshalb liegt die laufende Datenbank lokal in `Application Support`.

## Backups in iCloud

`total-recall backup` schreibt mit `VACUUM INTO` eine **konsistente** Kopie, komprimiert sie mit zstd und legt sie als einzelne Datei ab. Die ist sicher für iCloud. Zurückholen mit [`total-recall restore`](../reference/10-befehle.md#restore). Der Default-Ordner `~/Documents/total-recall` wird über iCloud Drive synchronisiert, wenn „Schreibtisch- & Dokumente-Ordner“ aktiv ist.

Aufbewahrt werden die letzten 7 Tageskopien und die letzte Kopie jedes Monats.

Beachte: Damit liegen die Backups auch bei Apple. iCloud Drive ist nur Ende-zu-Ende-verschlüsselt, wenn der „Erweiterte Datenschutz“ aktiv ist. Willst du das nicht, setze `backup.dir` auf einen lokalen Ordner.

## Löschen

Es gibt keinen Löschbefehl. Willst du alles entfernen, lösche die Datenbank, das Log und die Backups. `sql` ist nur lesend und kann nichts löschen.

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
- die Original-JSON-Zeile (`raw`), ebenfalls maskiert. Abschaltbar mit `ingest.store_raw = false`.

Nicht gespeichert werden technische Records wie Anhänge, API-Requests oder Datei-Snapshots.

## Maskierung von Geheimnissen

Vor dem Schreiben werden `text` und `raw` durchsucht. Treffer werden ersetzt durch `«redacted:<art>»`. Das betrifft unter anderem:

- private Schlüssel (PEM-Blöcke),
- Tokens von AWS, GitHub, Slack, OpenAI, Anthropic,
- JWTs,
- Zuweisungen wie `password=…`, `passwort: …`, `token: …`,
- Zugangsdaten in URLs (`https://user:pass@…`),
- lange, zufällig aussehende Zeichenketten in Zuweisungen.

Nicht maskiert werden zum Beispiel Git-SHAs, UUIDs und `op://`-Referenzen. Die sind keine Geheimnisse.

!!! warning "Grenzen"
    Die Maskierung ist nicht umkehrbar. Sie fängt typische Formate, aber nicht alles. Ein Passwort in Fließtext („das Passwort ist Sommer2026“) erkennt sie nicht. Behandle die Datenbank deshalb wie vertrauliche Daten.

Eigene Muster und Ausnahmen: `[redact]` in der [Konfiguration](konfiguration.md#redact).

## Wo die Dateien liegen

| Was | Ort (macOS) |
|---|---|
| Datenbank | `~/Library/Application Support/total-recall/archive.db` |
| Log | `total-recall.log` neben der Datenbank |
| Backups | `~/Documents/total-recall/archive-YYYY-MM-DD.db` |
| Config | `~/.config/total-recall/config.toml` |

Auf Linux liegt die Datenbank unter `$XDG_DATA_HOME/total-recall/`.

## Warum die Datenbank nicht in iCloud liegt

SQLite schreibt im WAL-Modus. Neben `archive.db` liegen dann `archive.db-wal` und `archive.db-shm`. iCloud synchronisiert diese Dateien unabhängig voneinander und zu beliebigen Zeiten. Das kann die Datenbank beschädigen. Deshalb liegt die laufende Datenbank lokal in `Application Support`.

## Backups in iCloud

`total-recall backup` schreibt mit `VACUUM INTO` eine **konsistente** Kopie in eine einzelne Datei. Die ist sicher für iCloud. Der Default-Ordner `~/Documents/total-recall` wird über iCloud Drive synchronisiert, wenn „Schreibtisch- & Dokumente-Ordner“ aktiv ist.

Aufbewahrt werden die letzten 7 Tageskopien und die letzte Kopie jedes Monats.

Beachte: Damit liegen die Backups auch bei Apple. iCloud Drive ist nur Ende-zu-Ende-verschlüsselt, wenn der „Erweiterte Datenschutz“ aktiv ist. Willst du das nicht, setze `backup.dir` auf einen lokalen Ordner.

## Löschen

Es gibt keinen Löschbefehl. Willst du alles entfernen, lösche die Datenbank, das Log und die Backups. `sql` ist nur lesend und kann nichts löschen.

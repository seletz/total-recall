---
title: Archiv warten
icon: lucide/wrench
---
# Archiv warten

Das Archiv wächst mit jeder Session, bei täglicher Arbeit um einige hundert MiB im Monat. Die Datei kompakter zu machen, kostet nichts an Daten: `maintenance` baut sie mit größeren Seiten neu auf. Hintergrund: Die Zeilen in `events` sind meist 1 bis 4 KiB groß; mit SQLites Standard von 4 KiB je Seite bleibt am Ende vieler Seiten Platz ungenutzt, mit 16 KiB je Seite ist die Datei um rund ein Siebtel kleiner. Schema und Inhalt bleiben gleich.

## Wann

- Einmal nach [`migrate`](../reference/10-befehle.md#migrate) auf Schema v5 (v1.2.0): Die Migration schreibt jedes Event neu, die alten Seiten bleiben als Freiliste in der Datei, die dadurch zunächst größer ist als vorher. Auf einer Kopie eines echten Archivs: 1146 MiB vor der Migration, 1533 MiB danach, 438 MiB nach `maintenance` (`freelist pages` in `maintenance --dry-run` zeigt den Anteil). Rezept: [Aktualisieren](10-installieren.md#aktualisieren).
- Einmal nach dem Update auf eine Version mit `maintenance`: Archive, die vorher angelegt wurden, haben 4 KiB-Seiten. [`status`](../reference/10-befehle.md#status) zeigt die Seitengröße und weist darauf hin, solange sie unter 16384 liegt.
- Danach selten. Die Seitengröße bleibt; ein weiterer Lauf lohnt nur, wenn viel gelöscht wurde (`freelist pages` in `maintenance --dry-run`).
- Zu einer ruhigen Zeit, etwa morgens vor der ersten Session: Der Befehl hält das Archiv für die Dauer des Neuaufbaus exklusiv, bei 1,5 GiB rund zehn Sekunden plus das Backup. Hooks, die in dieser Zeit laufen, überspringen ihren Lauf und holen das beim nächsten Hook derselben Session oder beim nächsten `SessionStart` vom gespeicherten Offset nach; eine `INFO`-Zeile im Log (`maintenance in progress, skipped`) ist dann kein Fehler, und es geht nichts verloren.

## Wie

Erst messen, dann machen. Der Probelauf baut die Kopie, prüft sie und löscht sie wieder, ohne Backup und ohne Lock:

```sh
total-recall maintenance --dry-run
```

```text
dry run: the archive was not changed; the rebuilt copy was measured and deleted
                BEFORE     AFTER
size            858.3 MiB  738.1 MiB  (-14.0 %)
page size       4096       16384
...
```

Lohnt es sich, dann TUI und andere lesende Befehle beenden (sie blockieren den Checkpoint vor dem Austausch) und:

```sh
total-recall maintenance
```

Der Befehl schreibt zuerst ein Backup wie [`backup`](../reference/10-befehle.md#backup), baut dann die Kopie, prüft sie (`PRAGMA integrity_check`, gleiche Zeilenzahlen) und tauscht die Datei atomar aus. Jeder Fehler vorher lässt das Archiv unverändert, auch ein `Ctrl-C`. Danach zeigt `status` die neue Seitengröße ohne Hinweis.

## `status` zeigt „last backup: unknown“ { #backup-unbekannt }

Liegt `[backup] dir` unter `~/Documents` und darf der lesende Prozess den Ordner nicht lesen, zeigen [`status`](../reference/10-befehle.md#status) und die TUI `unknown` statt `none`, mit Grund (`no access: operation not permitted`) und Hinweis. Das heißt nicht, dass es keine Backups gibt; der Hinweis sagt „die Backups sind in Ordnung“ nur, wenn die Statusdatei ein Backup der letzten 48 Stunden belegt. Liegt das Verzeichnis nicht unter `~/Documents`, `~/Desktop` oder `~/Downloads`, oder lautet der Fehler `permission denied` statt `operation not permitted`, ist es kein Datenschutz-Problem und der Hinweis lautet „Dateirechte prüfen“: Rechte des Verzeichnisses für den Benutzer, der `total-recall` aufruft. Unter macOS liegt es sonst am Datenschutz für den Ordner (hier „Dokumente“; bei `~/Desktop` und `~/Downloads` entsprechend):

1. Der App, aus der du `total-recall` startest (Terminal, IDE), unter Systemeinstellungen → Datenschutz & Sicherheit → Dateien und Ordner den Zugriff auf „Dokumente“ geben.
2. **Das Terminal bzw. die App danach neu starten.** Laufende Prozesse behalten die alten Rechte; ein Terminal, das seit Tagen läuft, bekommt die Freigabe erst nach dem Neustart.
3. Oder `[backup] dir` außerhalb von `~/Documents` legen.

Die Zeit des letzten Backups zeigt `status` auch ohne Leserechte, aus der Statusdatei `<db>.backup.json` neben der Datenbank, die jedes Backup schreibt.

## Backups und iCloud { #backup-icloud }

Der Standard für `[backup] dir` ist `~/Documents/total-recall`. Liegt `~/Documents` in iCloud Drive, wird jedes Backup mitsynchronisiert, bei einem großen Archiv mehrere hundert MiB pro Tag; das kostet iCloud-Speicher und Bandbreite. [`doctor`](../reference/10-befehle.md#doctor) weist darauf hin. Wer das nicht will, legt das Verzeichnis außerhalb, zum Beispiel in der Konfiguration:

```toml
[backup]
dir = "~/Library/Application Support/total-recall/backups"
```

Bestehende Backups lassen sich einfach dorthin verschieben (`archive-*.db.zst`). Der Standardpfad ändert sich nicht von selbst.

## Backup wiederherstellen { #backup-wiederherstellen }

`backup` schreibt seit v1.3.0 `archive-YYYY-MM-DD.db.zst` (zstd); bis v1.2 schrieben `archive-YYYY-MM-DD.db.gz` (gzip), Vorabversionen davor unkomprimierte `archive-YYYY-MM-DD.db`. Alle drei liest [`restore`](../reference/10-befehle.md#restore), alle drei zählen bei der Aufbewahrung (`keep_daily`, `keep_monthly`) gleich, und `status`, `backup --if-older`, die Config-Seite und das Setup der TUI nehmen die jeweils neueste Kopie, egal in welchem Format. Alte `.db.gz` musst du also weder umwandeln noch löschen; sie fallen mit der Zeit aus der Aufbewahrung.

Ein neues Backup ersetzt nie ein vorhandenes. Hat der Tag schon eine Kopie, egal in welchem Format, bekommt die neue die Uhrzeit in den Namen: `archive-YYYY-MM-DD-HHMM.db.zst`, in derselben Minute zusätzlich eine laufende Nummer (`archive-YYYY-MM-DD-HHMM-2.db.zst`). Gelöscht wird nur über die [Aufbewahrung](../reference/20-konfiguration.md#backup), die ältesten Kopien zuerst; jede gelöschte Kopie steht als `INFO`-Zeile `backup: pruned …` im Log.

Erst neben die laufende Datenbank, dann ansehen:

```sh
total-recall restore ~/Documents/total-recall/archive-2026-11-01.db.zst --to /tmp/archive.db
total-recall --db /tmp/archive.db status
```

Die laufende Datenbank ersetzen (vorher TUI und Hooks beenden, kein `ingest` darf laufen):

```sh
total-recall restore ~/Documents/total-recall/archive-2026-11-01.db.zst --force
```

`restore` prüft die Kopie (Schema-Version, `PRAGMA quick_check`), bevor es das Ziel anfasst. Ohne das Programm geht es auch, etwa auf einem Rechner ohne total-recall: `zstd -d archive-2026-11-01.db.zst -o archive.db` (Homebrew: `brew install zstd`), für eine alte Kopie `gunzip -k archive-2026-11-01.db.gz`. Heraus kommt jeweils eine gewöhnliche SQLite-Datei.

## Wenn es nicht klappt

| Meldung | Bedeutung |
|---|---|
| `not enough free disk space` | Neben der Datenbank muss etwa die doppelte Dateigröße frei sein (Backup-Kopie und neue Kopie), im Backup-Ordner die halbe. Platz schaffen oder alte Backups löschen. |
| `writers active` | Ein `ingest` oder `backup` hatte das Archiv noch offen, als `--wait` (Default 60 s) ablief. Kurz warten und noch einmal starten, oder `--wait 5m`. |
| `readers active` | Ein lesender Prozess (TUI, `sql`, ein anderes Terminal) hält die Datei offen. Schließen, noch einmal starten. |
| `older than this binary` | Die Datenbank hat ein älteres Schema als das Programm. `maintenance` migriert nicht: erst `total-recall backup`, dann [`total-recall migrate`](../reference/10-befehle.md#migrate), siehe [Aktualisieren](10-installieren.md#aktualisieren). |
| `copy rejected` | Die Kopie hat die Prüfung nicht bestanden; das Archiv ist unverändert. Das sollte nicht vorkommen: Log und [`doctor`](../reference/10-befehle.md#doctor) ansehen, Issue aufmachen. |

Referenz: [`maintenance`](../reference/10-befehle.md#maintenance).

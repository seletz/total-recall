---
title: Installieren
icon: lucide/download
---
# Installieren

Unterstützt werden macOS und Linux auf amd64 und arm64. Das Binary ist statisch gebaut und braucht keine weiteren Bibliotheken. Drei Wege: das Release-Archiv von GitHub, Homebrew oder der Quellcode.

## Release-Archiv

Jedes [Release auf GitHub](https://github.com/seletz/total-recall/releases) trägt je Plattform ein Archiv `total-recall-<tag>-<os>-<arch>.tar.gz` mit dem Binary und den Shell-Vervollständigungen, dazu eine SBOM und die Datei `checksums-<tag>.txt` mit den SHA-256-Summen aller Dateien. Was die Dateien enthalten und wie man die SBOM prüft, steht unter [Release-Assets](../reference/50-release.md).

Archiv und Checksummen laden, prüfen, entpacken und das Binary an einen Ort im `PATH` legen. Beispiel für macOS auf Apple Silicon (`darwin-arm64`; für Linux `linux-amd64` oder `linux-arm64`):

```sh
tag=v1.2.0
curl -LO https://github.com/seletz/total-recall/releases/download/$tag/total-recall-$tag-darwin-arm64.tar.gz
curl -LO https://github.com/seletz/total-recall/releases/download/$tag/checksums-$tag.txt
shasum -a 256 --ignore-missing -c checksums-$tag.txt
tar -xzf total-recall-$tag-darwin-arm64.tar.gz
install -m 0755 total-recall ~/.local/bin/
```

Auf Linux heißt der Prüfbefehl `sha256sum --ignore-missing -c checksums-$tag.txt`. Prüfen:

```sh
total-recall --version
```

Das macOS-Binary ist ab v1.0.2 mit einer Developer-ID signiert und von Apple notarisiert; Gatekeeper lässt es auch dann ohne Nachfrage starten, wenn das Archiv aus dem Browser kam und die Quarantäne gesetzt ist. Beim ersten Start holt macOS das Notarisierungs-Ticket bei Apple, dafür braucht es kurz Netz. Prüfen lässt sich die Signatur mit `codesign` und `spctl`, siehe [Release-Assets](../reference/50-release.md#signatur-der-macos-binaries). Ältere Releases sind nicht signiert; dort blockiert macOS den ersten Start einer aus dem Browser geladenen Datei (bei `curl` nicht). Abhilfe: `xattr -d com.apple.quarantine ~/.local/bin/total-recall`.

Die Vervollständigungen liegen im Archiv unter `completions/` (`total-recall.bash`, `_total-recall`, `total-recall.fish`); sie lassen sich auch jederzeit mit [`total-recall completion`](../reference/10-befehle.md#completion) erzeugen.

## Mit Homebrew

Ab v1.0.0 kommt `total-recall` als Cask aus dem Tap `seletz/homebrew-tap`:

```sh
brew install --cask seletz/tap/total-recall
```

Der Cask installiert die Shell-Vervollständigung für bash, zsh und fish und wird mit jedem Release aktualisiert. Er verlässt sich auf die Signatur und Notarisierung des Binarys: Das Quarantäne-Attribut, das Homebrew beim Laden setzt, bleibt stehen, und Gatekeeper startet das Binary trotzdem ohne Dialog. Bis v1.2.1 entfernte der Cask das Attribut beim Installieren zusätzlich; das war die Übergangslösung aus der Zeit vor der Signatur.

```sh
brew upgrade --cask total-recall
```

## Aus dem Quellcode

Voraussetzungen: Go (Version laut `go.mod`) und [mise](https://mise.jdx.dev/).

```sh
git clone https://github.com/seletz/total-recall.git
cd total-recall
mise install
mise run build
```

Das Ergebnis ist `./total-recall` im Repo. Kopiere es an einen Ort in deinem `PATH`, zum Beispiel:

```sh
install -m 0755 total-recall ~/.local/bin/
```

Ohne mise geht es auch direkt mit Go:

```sh
CGO_ENABLED=0 go install github.com/seletz/total-recall/cmd/total-recall@latest
```

Die Version zeigt dann `dev`, weil sie beim Release-Build gesetzt wird.

## Aktualisieren

Ein neues Binary einspielen wie beim Installieren (`brew upgrade --cask total-recall`, Release-Archiv oder Build). Innerhalb einer Major-Version bleiben Befehle, Flags, JSON-Ausgabe, Config-Schlüssel und Hooks gültig; ein neues Datenbankschema kann auch eine Minor-Version bringen, weil die Migration ohnehin nur von Hand läuft ([Versionierung](../reference/60-versionierung.md)). Deshalb nach jedem Update prüfen, ob eines anliegt:

```sh
total-recall status
```

Steht in der Zeile `schema` ein `MIGRATION PENDING`, migriert nichts von selbst: Die Hooks schreiben nichts mehr (eine `WARN`-Zeile im Log, einmal am Tag), bis du migriert hast; lesende Befehle und die TUI laufen weiter, soweit sie das alte Schema erlauben, die TUI zeigt `migration pending` in der Kopfzeile. Es geht nichts verloren, der erste Hook nach der Migration liest alles nach. In dieser Reihenfolge:

```sh
total-recall backup                               # 1. sichern
total-recall migrate                              # 2. Plan ansehen, bestätigen, migrieren
total-recall ingest --all --reread --rewrite-raw  # 3. nach v1.3.0: Rohzeilen und Bilder nachtragen, siehe unten
total-recall maintenance                          # 4. Datei neu aufbauen (nach v1.2.0 und v1.3.0 nötig, siehe unten)
total-recall status                               # 5. prüfen: schema ohne Hinweis
```

**v1.3.0 (Schema v6)** legt nur zwei Tabellen an, `migrate` dauert Sekunden. Was das Release ändert, steckt im dritten Schritt: `raw` wird wieder die **vollständige** Transkriptzeile ohne Größengrenze, und eingefügte Bilder und Dokumente kommen als Blobs ins Archiv ([Änderungen](../reference/90-aenderungen.md)). Beides holt `ingest --all --reread --rewrite-raw` für den Altbestand aus den Transkripten, und zwar **nur, solange Claude Code die noch hat** (`cleanupPeriodDays`, standardmäßig 30 Tage): Was schon gelöscht ist, bleibt der Auszug von v1.2. Deshalb den Schritt bald nach dem Update ausführen; er dauert etwa so lange wie das erste Einlesen, lässt sich abbrechen und wiederholen. Zahlen von einer Kopie eines echten Archivs stehen in den [Änderungen](../reference/90-aenderungen.md). `maintenance` danach gibt den Platz der ersetzten Zeilen frei.

**v1.2.0 (Schema v5)** schreibt jedes Event neu: `raw` wird ein komprimierter Auszug, Events verweisen per ID auf ihre Quelle ([Änderungen](../reference/90-aenderungen.md)). Auf einer Kopie eines echten Archivs mit 233 000 Events (1,1 GiB) dauerte `migrate` 27 Sekunden und brauchte neben der Datenbank rund 2 GiB frei (Kopie und WAL; `migrate` prüft das vorher). Direkt danach ist die Datei **größer** als vorher (1,5 GiB, die alten Seiten stehen auf der Freiliste); erst [`maintenance`](60-wartung.md) baut sie neu auf, in dem Beispiel in 11 Sekunden auf 438 MiB. Dafür muss neben der Datenbank noch einmal etwa das Doppelte der Dateigröße frei sein und im Backup-Ordner die Hälfte. Die lesenden Befehle liefern vorher und nachher dieselben Ergebnisse.

Zwei Dinge fallen nach dem Update auf: Die Kopie `archive.db.pre-v4.bak` liegt unkomprimiert neben der Datenbank, bis du sie löschst. Und die gzip-Backups in `~/Documents/total-recall` werden **größer**, obwohl die Datenbank kleiner ist, in dem Beispiel von 187 auf 244 MiB: `raw` ist jetzt schon mit zstd komprimiert, und gzip kann diese Blöcke nicht weiter packen. Ab v1.3.0 schreibt `backup` deshalb `.db.zst` statt `.db.gz` (kleiner und deutlich schneller); die alten `.db.gz` bleiben lesbar und zählen bei der Aufbewahrung mit, siehe [Backup wiederherstellen](60-wartung.md#backup-wiederherstellen).

`migrate` zeigt vorher, was es tut: die Versionen, jede ausstehende Migration mit einer Zeile Beschreibung, wo die Kopie `archive.db.pre-v<N>.bak` liegt und wie viel Platz frei sein muss. `--dry-run` zeigt nur den Plan, `--yes` fragt nicht nach. Bei einer langen Migration läuft eine Fortschrittsanzeige; Hooks, die währenddessen feuern, überspringen ihren Lauf und holen danach nach. Referenz: [`migrate`](../reference/10-befehle.md#migrate). Was sich je Version ändert und ob ein Update migrieren muss, steht in den [Änderungen](../reference/90-aenderungen.md).

Die Kopie neben der Datenbank ist die Rückfalloption: Läuft nach der Migration alles, kann sie weg. Wer zurück will, kopiert sie über die Datenbank und installiert das alte Binary.

Dasselbe geht in der TUI: Die Ansicht [Config](../reference/30-tui.md#config) (`4`) zeigt auf der Seite **status** `migration pending`, die Seite **actions** führt `backup`, `migrate` (mit demselben Plan vor der Bestätigung) und `maintenance` aus, und die Seite **setup** prüft nach dem Einspielen eines neuen Binarys, ob das im `PATH` liegende dieselbe Version ist wie das laufende und ob die Hooks noch vollständig sind. Rezept: [Die Datenbank migrieren](65-einstellungen-tui.md#die-datenbank-migrieren).

## Nächste Schritte

1. [Hooks einrichten](20-hooks-einrichten.md) und einmal alles nachlesen: `total-recall ingest --all`.
2. Optional eine [Konfiguration](../reference/20-konfiguration.md) anlegen; die Defaults passen meist.

Wer das Ganze einmal von vorn bis hinten durchspielen will, nimmt das Tutorial [Erste Schritte](../tutorials/erste-schritte.md).

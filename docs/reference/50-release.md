---
title: Release-Assets
icon: lucide/package
---
# Release-Assets

Was ein [Release auf GitHub](https://github.com/seletz/total-recall/releases) als Dateien trägt und wie man sie prüft. Die Dateinamen folgen alle dem Schema `total-recall-<tag>-<os>-<arch>`; `<os>` ist `darwin` oder `linux`, `<arch>` `amd64` oder `arm64`.

| Datei | Inhalt |
|---|---|
| `total-recall-<tag>-<os>-<arch>.tar.gz` | Das statische Binary `total-recall` und die Shell-Vervollständigungen für bash, zsh und fish. Das darwin-Binary ist ab v1.1.0 [signiert und notarisiert](#signatur-der-macos-binaries). |
| `total-recall-<tag>-<os>-<arch>.cdx.json` | Die [SBOM](#sbom) des Binarys aus demselben Archiv. |
| `checksums-<tag>.txt` | SHA-256 aller Archive und SBOMs im Format von `sha256sum`. |

Alle Dateien eines Releases entstehen in einem Lauf von goreleaser aus dem getaggten Commit (`.goreleaser.yml`, Workflow `release.yml`). `mise run release:snapshot` baut dieselben Dateien lokal nach `dist/`, ohne etwas zu veröffentlichen; die Version heißt dann `<letzter Tag>-SNAPSHOT-<commit>`. Der Snapshot signiert nicht, weil die [Secrets](#secrets-des-release-laufs) lokal fehlen; einen signierten Snapshot baut der Workflow [`signed-snapshot.yml`](#signatur-ohne-release-testen).

## Checksummen prüfen

Archiv, SBOM und Checksummen-Datei in ein Verzeichnis laden, dann:

```sh
sha256sum --ignore-missing -c checksums-v1.0.0.txt
```

Auf macOS ohne coreutils: `shasum -a 256 --ignore-missing -c checksums-v1.0.0.txt`.

## SBOM

Jedes Archiv hat eine Software Bill of Materials im Format [CycloneDX](https://cyclonedx.org/) 1.6 als JSON: die Liste aller Go-Module, die in genau dieses Binary gelinkt sind, mit Version und Hash, dazu die Go-Toolchain und die Build-Einstellungen. Die Module unterscheiden sich je Plattform (Linux und macOS linken verschiedene Systemschichten), deshalb gibt es eine SBOM je Archiv.

Erzeugt wird sie mit [cyclonedx-gomod](https://github.com/CycloneDX/cyclonedx-gomod) im Modus `bin` aus dem fertigen Binary, nicht aus dem Quellcode: Die Modulliste ist die, die der Go-Linker in das Binary geschrieben hat. Die Version des Werkzeugs ist in `mise.toml` festgelegt und in der CI dieselbe.

Aufbau der Datei:

- `metadata.component`: die Hauptkomponente, Name `total-recall`, Version ohne `v` (`1.0.0`), purl mit Modulpfad, Version und Plattform (`pkg:golang/github.com/seletz/total-recall@v1.0.0?goarch=arm64&goos=darwin&type=module#cmd/total-recall`), Verweis auf das Repository.
- `metadata.properties`: Hashes des Binarys, Compiler, `CGO_ENABLED`, `GOOS`, `GOARCH` und die übrigen Build-Einstellungen aus den Build-Infos des Binarys (`cdx:gomod:build:…`).
- `components`: je Modul Name, Version, SHA-256-Hash (derselbe Wert wie der `h1:`-Hash in `go.sum`, nur hexadezimal statt Base64), purl mit Plattform und, soweit erkannt, die Lizenz als `evidence`. Die Lizenzerkennung ist heuristisch; wo sie eine veraltete SPDX-Kennung liefert (etwa `GPL-2.0+` für `github.com/golang/freetype`, das wahlweise unter FreeType-Lizenz oder GPL steht), steht die Kennung als Lizenzname statt als `id`, damit die Datei dem Schema entspricht. Die Go-Standardbibliothek steht als Modul `std` mit der Go-Version.
- `dependencies`: der Abhängigkeitsgraph über die purls.

Serial Number und Zeitstempel sind je Lauf neu; zwei Läufe ergeben deshalb keine byte-gleichen Dateien. Maßgeblich ist die Datei, die im Release hängt und in `checksums-<tag>.txt` steht.

### SBOM prüfen

Formal gegen das CycloneDX-Schema mit der [CycloneDX CLI](https://github.com/CycloneDX/cyclonedx-cli):

```sh
cyclonedx validate --input-file total-recall-v1.0.0-darwin-arm64.cdx.json --input-version v1_6 --fail-on-errors
```

Inhaltlich gegen das Repository mit dem Prüfwerkzeug der Releases. Es braucht den Quellcode des getaggten Commits, weil es die Modulhashes mit `go.sum` vergleicht:

```sh
git clone https://github.com/seletz/total-recall.git
cd total-recall
git checkout v1.0.0
go run ./tools/sbom check -version 1.0.0 /pfad/zu/total-recall-v1.0.0-darwin-arm64.cdx.json
```

Geprüft wird: Dateiname nach dem Schema, gültiges JSON, `bomFormat` `CycloneDX` und `specVersion` `1.6`, Hauptkomponente `total-recall` in der angegebenen Version mit Repository-Verweis, Plattform in jeder purl gleich der im Dateinamen, jedes Modul mit Version und Hash genau so in `go.sum`, Standardbibliothek vorhanden. Bei Erfolg eine Zeile je Datei:

```
total-recall-v1.0.0-darwin-arm64.cdx.json: darwin/arm64, 39 modules, go1.27.3
```

Ein ganzes `dist/`-Verzeichnis aus `mise run release:snapshot` prüft `go run ./tools/sbom check dist` zusätzlich darauf, dass jedes Archiv eine SBOM hat und jede SBOM mit passender Prüfsumme in der Checksummen-Datei steht; `mise run sbom:check` baut den Snapshot und prüft ihn in einem Schritt. Die CI tut dasselbe bei jedem Pull Request, und der Release-Lauf prüft jede SBOM, bevor goreleaser Prüfsummen bildet und hochlädt.

## Signatur der macOS-Binaries

Die darwin-Binaries (amd64 und arm64) sind ab v1.1.0 mit einem Developer-ID-Application-Zertifikat signiert, mit Hardened Runtime und Apple-Zeitstempel, und von Apples Notary Service notarisiert. macOS startet sie deshalb ohne Gatekeeper-Dialog, auch aus einem Archiv, das der Browser mit Quarantäne-Attribut abgelegt hat. Die Linux-Binaries sind unverändert unsigniert.

Signiert wird im Release-Lauf auf dem Linux-Runner mit [quill](https://github.com/anchore/quill) über den Block `notarize.macos` in `.goreleaser.yml`. Der Schritt gehört zur Build-Phase von goreleaser: Er schreibt die Signatur in die Binaries in `dist/`, bevor Archive, SBOMs und Prüfsummen entstehen. Archiv, SBOM (`cdx:gomod:binary:hash:…`) und `checksums-<tag>.txt` beschreiben also das signierte Binary, das auch ausgeliefert wird. Der Lauf wartet auf Apples Antwort (höchstens 20 Minuten); eine abgelehnte Einreichung bricht das Release ab, bevor etwas hochgeladen wird.

Ein Notarisierungs-Ticket lässt sich nur an Bundles, Disk-Images und Installer-Pakete heften (`stapler`), nicht an ein nacktes Mach-O-Binary. Beim ersten Start holt macOS das Ticket deshalb bei Apple; ohne Netz prüft Gatekeeper nur die Signatur.

### Signatur prüfen

Nach dem Entpacken des Archivs:

```sh
codesign -dv --verbose=2 ./total-recall
```

Erwartet sind in der Ausgabe eine Zeile `Authority=Developer ID Application: …`, darunter `Authority=Developer ID Certification Authority` und `Authority=Apple Root CA`, sowie `flags=0x10000(runtime)` für die Hardened Runtime und ein `Timestamp=…`. Ein unsigniertes Binary meldet `code object is not signed at all`; ein nur ad-hoc signiertes zeigt `Signature=adhoc` und keine `Authority`-Zeilen.

Die Gatekeeper-Bewertung samt Notarisierung:

```sh
spctl -a -vv -t exec ./total-recall
```

Erwartet: `./total-recall: accepted` mit `source=Notarized Developer ID`. Nur `source=Developer ID` heißt signiert, aber nicht notarisiert (oder das Ticket war offline nicht erreichbar); `rejected` heißt, dass die Datei so nicht startet. Wer den Quarantäne-Fall nachstellen will, lädt das Archiv über den Browser statt mit `curl`, entpackt es und ruft `./total-recall --version` auf; mit einem notarisierten Binary erscheint kein Dialog.

### Signatur ohne Release testen

Der Workflow `signed-snapshot.yml` prüft Signatur und Notarisierung, ohne ein Release zu veröffentlichen: etwa nach dem Einrichten oder Erneuern der [Secrets](#secrets-des-release-laufs) oder nach einer Änderung an `notarize.macos`. Er läuft nur auf Anstoß (`workflow_dispatch`), mit demselben Setup und derselben goreleaser-Version wie `release.yml`, und ruft `goreleaser release --snapshot --clean` mit den fünf Secrets auf. goreleaser signiert und notarisiert dabei wie im Release-Lauf, denn der Notarisierungs-Schritt gehört zur Build-Phase und wird im Snapshot-Modus nicht übersprungen; `--snapshot` lässt nur Veröffentlichung und Validierung aus. Der Lauf legt keinen Tag an, erzeugt kein GitHub-Release, hat nur Leserechte auf das Repository und bekommt den Token für den Homebrew-Tap nicht.

Der Lauf bricht ab, wenn eines der fünf Secrets fehlt, statt unsigniert zu bauen. Nach dem Bau liest er die Signatur jedes darwin-Binarys mit `quill describe` zurück und schlägt fehl, wenn ein Binary unsigniert oder nur ad hoc signiert ist, kein Team-ID trägt oder die Hardened Runtime fehlt; die Antwort von Apple hat goreleaser zuvor abgewartet (`wait: true`), eine Ablehnung bricht den Lauf schon dort ab. Die darwin-Archive, ihre SBOMs und die Checksummen-Datei hängen sieben Tage als Workflow-Artefakt `signed-snapshot-darwin` am Lauf. Die Archive heißen wie im Release nach dem letzten Tag (`total-recall-<tag>-darwin-<arch>.tar.gz`), das Binary meldet aber `<letzter Tag>-SNAPSHOT-<commit>`.

Starten, auf dem Default-Branch oder einem anderen Branch (`--ref`), und auf das Ende warten:

```sh
gh workflow run signed-snapshot.yml --ref develop
gh run watch "$(gh run list --workflow signed-snapshot.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
```

Das Artefakt holen und auf dem Mac prüfen:

```sh
gh run download --name signed-snapshot-darwin --dir snapshot
cd snapshot
tar -xzf total-recall-*-darwin-arm64.tar.gz
codesign -dv --verbose=2 ./total-recall
spctl -a -vv -t exec ./total-recall
./total-recall --version
```

Erwartet ist dieselbe Ausgabe wie bei einem Release, siehe [Signatur prüfen](#signatur-prufen): die Developer-ID in den `Authority`-Zeilen, `flags=0x10000(runtime)`, ein `Timestamp` und `accepted` mit `source=Notarized Developer ID`. Für amd64 entsprechend das zweite Archiv. `gh run download` ohne `--name` lädt die Artefakte des zuletzt gewählten Laufs; mit `gh run download <run-id>` einen bestimmten.

Ein `workflow_dispatch`-Workflow lässt sich erst starten, wenn er im Default-Branch liegt; `--ref` auf einen anderen Branch geht danach.

### Secrets des Release-Laufs

Der Workflow `release.yml` reicht fünf Repository-Secrets als Umgebungsvariablen an goreleaser durch. Fehlt `MACOS_SIGN_P12` oder ist es leer, überspringt goreleaser den Schritt und die darwin-Binaries bleiben unsigniert; alles andere am Release läuft wie bisher. Snapshot-Builds, die CI und Forks ohne Secrets sind davon nicht betroffen.

| Secret | Inhalt |
|---|---|
| `MACOS_SIGN_P12` | Das Developer-ID-Application-Zertifikat samt privatem Schlüssel als PKCS#12-Datei (`.p12`), base64-kodiert. |
| `MACOS_SIGN_PASSWORD` | Das Passwort der `.p12`-Datei. |
| `MACOS_NOTARY_KEY` | Der private Schlüssel des App-Store-Connect-API-Keys (`.p8`), base64-kodiert. |
| `MACOS_NOTARY_KEY_ID` | Die Key-ID des API-Keys (zehn Zeichen, steht in App Store Connect neben dem Key). |
| `MACOS_NOTARY_ISSUER_ID` | Die Issuer-ID des Teams (UUID, steht auf derselben Seite über der Key-Liste). |

Einrichten, einmalig durch den Maintainer:

1. **Zertifikat.** In der Apple-Developer-Konsole ein Zertifikat vom Typ „Developer ID Application" ausstellen und in der Schlüsselbund-Verwaltung installieren. Dort das Zertifikat zusammen mit dem privaten Schlüssel als `.p12` exportieren und ein Passwort vergeben. Kodieren und als Secret setzen:

    ```sh
    base64 -i developer-id-application.p12 | tr -d '\n' | gh secret set MACOS_SIGN_P12
    gh secret set MACOS_SIGN_PASSWORD
    ```

2. **API-Key.** In App Store Connect unter „Users and Access“ → „Integrations“ → „App Store Connect API“ einen Team-Key mit der Rolle „Developer“ anlegen. Die `.p8`-Datei gibt es nur einmal zum Download. Kodieren und mit Key-ID und Issuer-ID als Secrets setzen:

    ```sh
    base64 -i AuthKey_XXXXXXXXXX.p8 | tr -d '\n' | gh secret set MACOS_NOTARY_KEY
    gh secret set MACOS_NOTARY_KEY_ID
    gh secret set MACOS_NOTARY_ISSUER_ID
    ```

3. **Prüfen.** `gh secret list` zeigt die fünf Namen. Ein Lauf von [`signed-snapshot.yml`](#signatur-ohne-release-testen) zeigt, ob die Secrets stimmen, ohne ein Release zu machen. Das nächste Release signiert; die Schritte `signing` und `notarizing` erscheinen im Log des Workflows, und die [Prüfung](#signatur-prufen) am heruntergeladenen Archiv muss die Developer-ID zeigen.

Das `-i` von `base64` ist die macOS-Schreibweise; auf Linux `base64 -w0 datei`. Die kodierten Dateien nie ins Repository legen; `gh secret set` liest den Wert von der Standardeingabe, sodass er nicht in der Shell-History landet.

Das Developer-ID-Zertifikat gilt fünf Jahre, der Zeitstempel hält bestehende Signaturen darüber hinaus gültig. Ein neues Zertifikat heißt nur `MACOS_SIGN_P12` und `MACOS_SIGN_PASSWORD` neu setzen; die API-Keys verfallen nicht, lassen sich aber in App Store Connect widerrufen.

## Release durchführen

1. Abschnitt `## <tag>` in [Änderungen](90-aenderungen.md) anlegen oder den Zusatz „(unveröffentlicht)“ entfernen; `scripts/release.sh` taggt sonst nicht, und der Workflow bricht ohne Abschnitt ab.
2. `mise run sbom:check` lokal: baut den Snapshot und prüft die SBOMs so, wie es die CI tut.
3. `mise run release`: wählt die Versionsnummer, taggt und pusht den Tag; der Workflow baut, signiert, notarisiert, erzeugt SBOMs und Prüfsummen, legt das GitHub-Release an und aktualisiert den Cask.
4. Nach dem Lauf: das darwin-Archiv laden und die [Signatur prüfen](#signatur-prufen); `brew upgrade --cask total-recall` auf einem Mac und `total-recall --version`.

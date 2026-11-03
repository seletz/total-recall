---
title: Release-Assets
icon: lucide/package
---
# Release-Assets

Was ein [Release auf GitHub](https://github.com/seletz/total-recall/releases) als Dateien trägt und wie man sie prüft. Die Dateinamen folgen alle dem Schema `total-recall-<tag>-<os>-<arch>`; `<os>` ist `darwin` oder `linux`, `<arch>` `amd64` oder `arm64`.

| Datei | Inhalt |
|---|---|
| `total-recall-<tag>-<os>-<arch>.tar.gz` | Das statische Binary `total-recall` und die Shell-Vervollständigungen für bash, zsh und fish. |
| `total-recall-<tag>-<os>-<arch>.cdx.json` | Die [SBOM](#sbom) des Binarys aus demselben Archiv. |
| `checksums-<tag>.txt` | SHA-256 aller Archive und SBOMs im Format von `sha256sum`. |

Alle Dateien eines Releases entstehen in einem Lauf von goreleaser aus dem getaggten Commit (`.goreleaser.yml`, Workflow `release.yml`). `mise run release:snapshot` baut dieselben Dateien lokal nach `dist/`, ohne etwas zu veröffentlichen; die Version heißt dann `<letzter Tag>-SNAPSHOT-<commit>`.

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

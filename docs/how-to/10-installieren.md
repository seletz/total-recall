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
tag=v1.0.0
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

Das Binary ist nicht von Apple signiert. macOS blockiert den ersten Start einer aus dem Browser geladenen Datei; bei `curl` passiert das nicht. Falls doch: `xattr -d com.apple.quarantine ~/.local/bin/total-recall`.

Die Vervollständigungen liegen im Archiv unter `completions/` (`total-recall.bash`, `_total-recall`, `total-recall.fish`); sie lassen sich auch jederzeit mit [`total-recall completion`](../reference/10-befehle.md#completion) erzeugen.

## Mit Homebrew

Ab v1.0.0 kommt `total-recall` als Cask aus dem Tap `seletz/homebrew-tap`:

```sh
brew install --cask seletz/tap/total-recall
```

Der Cask entfernt beim Installieren das Quarantäne-Attribut, installiert die Shell-Vervollständigung für bash, zsh und fish und wird mit jedem Release aktualisiert:

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

## Nächste Schritte

1. [Hooks einrichten](20-hooks-einrichten.md) und einmal alles nachlesen: `total-recall ingest --all`.
2. Optional eine [Konfiguration](../reference/20-konfiguration.md) anlegen; die Defaults passen meist.

Wer das Ganze einmal von vorn bis hinten durchspielen will, nimmt das Tutorial [Erste Schritte](../tutorials/erste-schritte.md).

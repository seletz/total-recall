# Installation

## Mit Homebrew

`total-recall` kommt als Cask aus dem Tap `seletz/homebrew-tap`.

```sh
brew install --cask seletz/tap/total-recall
```

Prüfen:

```sh
total-recall version
```

Das Binary ist nicht von Apple signiert. Der Cask entfernt deshalb beim Installieren das Quarantäne-Attribut. Ohne das würde macOS beim ersten Start blockieren.

Shell-Vervollständigung für bash, zsh und fish installiert der Cask mit.

Aktualisieren:

```sh
brew upgrade --cask total-recall
```

!!! warning "Privates Repository"
    Solange das Repository privat ist, kann Homebrew die Release-Archive nicht ohne Anmeldung laden. Dann bleibt nur die Installation aus dem Quellcode.

## Aus dem Quellcode

Voraussetzungen: Go (Version laut `go.mod`) und [mise](https://mise.jdx.dev/).

```sh
git clone git@github.com:seletz/total-recall.git
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

1. [Konfiguration](konfiguration.md) anlegen (optional, die Defaults passen meist).
2. [Hooks einrichten](hooks.md).
3. Einmal alles nachlesen: `total-recall ingest --all`.

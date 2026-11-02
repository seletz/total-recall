---
title: Erste Schritte
icon: lucide/footprints
---
# Erste Schritte

In diesem Tutorial installierst du `total-recall`, trägst die Hooks für Claude Code ein, liest deine bisherigen Sessions ein und findest eine davon wieder: erst auf der Kommandozeile, dann in der TUI. Du brauchst macOS oder Linux, Claude Code und ein Terminal. Rechne mit einer Viertelstunde.

Alle Ausgaben auf dieser Seite sind erfundene Beispiele. Bei dir stehen deine Sessions.

## 1. Installieren

```sh
brew install --cask seletz/tap/total-recall
total-recall version
```

Geht Homebrew nicht, etwa weil das Repository privat ist, baue aus dem Quellcode: `mise run build` erzeugt `./total-recall`. Beide Wege im Einzelnen: [Installieren](../how-to/10-installieren.md).

## 2. Hooks eintragen

`total-recall` läuft nicht im Hintergrund. Claude Code ruft es über Hooks auf. Den Block dafür gibt das Programm selbst aus:

```sh
total-recall hooks print --harness claude
```

Trage ihn in `~/.claude/settings.json` ein. Hast du dort schon Hooks, ergänze die Einträge, statt die Sektion zu ersetzen. Findet Claude Code `total-recall` nicht im `PATH`, nimm `total-recall hooks print --binary "$(command -v total-recall)"`. Was jeder Hook tut: [Hooks einrichten](../how-to/20-hooks-einrichten.md).

## 3. Alles nachlesen

Einmal von Hand, damit auch die Sessions von vor der Installation im Archiv landen:

```sh
total-recall ingest --all
```

```text
scanned 291 files: 291 changed, 0 unchanged, 0 locked; 158203 new events, 274 sessions touched (14.2s)
```

Ab jetzt lesen die Hooks nach jeder Antwort nur noch das Neue ein. Beim Start jeder Session wird nachgelesen, was fehlt.

## 4. Nachsehen, was da ist

```sh
total-recall status
```

```text
database              /Users/me/Library/Application Support/total-recall/archive.db
size                  612.4 MiB
schema                v3 (binary supports v3)
sessions              274
events                158203
sources               291
last ingest (claude)  2026-11-02 09:14  (1m ago)
last backup           -
log                   /Users/me/Library/Application Support/total-recall/total-recall.log
```

Die neuesten Sessions:

```sh
total-recall sessions --limit 3
```

```text
START             DUR    ID        NAME                        REPO                 BRANCH      HUMAN  AGENT
2026-10-31 19:38  2h59m  9a1b2c3d  Release-Pipeline aufsetzen  seletz/total-recall  develop     14     212
2026-10-31 09:05  45m    4e5f6a7b  Backup-Retention prüfen     seletz/total-recall  feature/bk  6      58
2026-10-30 14:20  1h12m  7c8d9e0f  CI pipeline                 seletz/total-recall  develop     9      131
```

## 5. Eine Session wiederfinden

Worum ging es, als goreleaser Thema war?

```sh
total-recall search goreleaser
```

```text
2026-10-31 19:38  Release-Pipeline aufsetzen  seletz/total-recall  claude:9a1b2c3d-…  [human]
    Bitte richte [goreleaser] mit einem Homebrew-Cask ein.
```

Das Präfix der Session-ID reicht, um den Verlauf zu lesen:

```sh
total-recall show 9a1b2c3d
```

```text
Release-Pipeline aufsetzen
claude:9a1b2c3d-0000-4000-8000-000000000000
time    2026-10-31 19:38 – 2026-10-31 22:37  (2h59m)
repo    https://github.com/seletz/total-recall
branch  develop
cwd     /Users/me/develop/total-recall

── 2026-10-31 19:38:02  human
Bitte richte goreleaser mit einem Homebrew-Cask ein.

── 2026-10-31 19:41:15  agent
Erledigt: .goreleaser.yml angelegt, Cask im Tap konfiguriert.
```

Ohne `--all` siehst du nur die Nachrichten von Mensch und Agent, keine Tool-Aufrufe.

## 6. Die TUI öffnen

Dasselbe geht interaktiv:

```sh
total-recall tui
```

Tippe `goreleaser`. Die Trefferliste füllt sich beim Tippen, rechts steht der Austausch um den markierten Treffer. ++tab++ wechselt in die Liste, ++up++ und ++down++ bewegen die Markierung, ++enter++ öffnet die Session, ++esc++ führt zurück. `2` zeigt die Statistik der Aktivität, `3` die erste und letzte Aktivität je Tag, `?` alle Tasten, `q` beendet.

Damit hast du das Archiv einmal von vorn bis hinten gesehen: Es füllt sich ab jetzt von selbst.

## Wie es weitergeht

- Kennzahlen, Suche und eigene SQL-Abfragen: [Archiv auswerten](../how-to/30-auswerten.md).
- Erste und letzte Aktivität je Tag mit Soll und Saldo: [Arbeitszeit nachvollziehen](../how-to/40-arbeitszeit.md).
- Agenten das Archiv selbst auswerten lassen: [Agenten anbinden](../how-to/50-agenten-anbinden.md).
- Die Defaults passen meist. Wenn nicht, schreibt `total-recall config init` eine kommentierte Vorlage; alle Schlüssel stehen in der [Konfiguration](../reference/20-konfiguration.md).
- Nach einem Update von Claude Code prüft `total-recall doctor`, ob das Transkript-Format noch bekannt ist: [Nach einem Claude-Code-Update](../how-to/20-hooks-einrichten.md#nach-einem-claude-code-update).

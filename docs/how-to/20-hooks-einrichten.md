---
title: Hooks einrichten
icon: lucide/webhook
---
# Hooks einrichten

`total-recall` läuft nicht ständig. Claude Code startet es über Hooks. Ein Hook ist ein Befehl, den Claude Code bei bestimmten Ereignissen ausführt. Was ein Hook-Lauf im Einzelnen tut und warum er sich sofort löst, erklärt [Einlesen über Hooks](../explanation/hooks.md).

## Der Block für Claude Code

Diesen Block gibt `total-recall hooks print --harness claude` aus. Er gehört in `~/.claude/settings.json`. Bei Stefan wird er über die dotfiles eingetragen.

Mit `--binary` steht ein anderer Befehl in den Hooks, etwa ein absoluter Pfad: `total-recall hooks print --binary "$(command -v total-recall)"`. Das hilft, wenn Claude Code `total-recall` nicht im `PATH` findet.

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "command": "total-recall ingest --hook --detach" }] }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "command": "total-recall ingest --hook --detach" }] }
    ],
    "SubagentStop": [
      { "hooks": [{ "type": "command", "command": "total-recall ingest --hook --detach" }] }
    ],
    "SessionEnd": [
      {
        "hooks": [
          { "type": "command", "command": "total-recall ingest --hook --detach" },
          { "type": "command", "command": "total-recall backup --if-older 24h --detach" }
        ]
      }
    ]
  }
}
```

Hast du schon andere Hooks, ergänze die Einträge. Ersetze nicht die ganze `hooks`-Sektion.

## Was jeder Hook tut

| Ereignis | Wann | Was passiert |
|---|---|---|
| `SessionStart` | Session startet oder wird fortgesetzt | Liest die aktuelle Session ein **und** liest alle Quellen nach |
| `Stop` | Agent ist mit einer Antwort fertig | Liest die neuen Zeilen der aktuellen Session ein |
| `SubagentStop` | Ein Subagent ist fertig | Liest die neuen Zeilen der Session und des Subagent-Transkripts (`agent_transcript_path`) ein |
| `SessionEnd` | Session endet | Liest den Rest ein und macht ein Backup, falls das letzte älter als 24 h ist |

Alle vier rufen `total-recall ingest --hook --detach` auf: `--hook` liest das Hook-JSON von stdin, `--detach` lässt den Hook sofort zurückkehren. Gelesen wird nur ab der Stelle, an der der letzte Lauf aufgehört hat. Ein Lauf dauert deshalb meist nur Millisekunden. Welche Felder des JSON gelesen werden, wie das Nachlesen beim Start Lücken auffängt und was bei Fehlern passiert: [Einlesen über Hooks](../explanation/hooks.md).

## Erstes Einlesen

Einmal nach der Installation:

```sh
total-recall ingest --all
```

Damit landet alles in der Datenbank, was Claude Code noch nicht gelöscht hat.

## Prüfen

Nach ein paar Antworten in einer neuen Session:

```sh
total-recall status
total-recall sessions --since 2026-11-01
```

Steht dort nichts, schau ins Log neben der Datenbank.

## Nach einem Claude-Code-Update { #nach-einem-claude-code-update }

Claude Code ändert sein Transkript-Format ohne Ankündigung: neue Record-Typen, neue Content-Blöcke, umbenannte Felder. `total-recall` liest dann trotzdem weiter, übersieht aber womöglich Neues. Nach einem Update deshalb:

```sh
total-recall doctor
```

- Exit-Code 0: alles bekannt.
- Exit-Code 2: Es gibt Unbekanntes. Die Ausgabe nennt Typ, Anzahl und erste Fundstelle. Ein Blick in die genannte Zeile zeigt, ob darin etwas Archivierenswertes steht. Wenn ja, braucht der Adapter ein Update; wenn nein, gehört der Typ in die Liste der bewusst übersprungenen Typen im Adapter (bis dahin hilft `harness.claude.skip_record_types` in der Config).

Die Versionsliste zeigt, ab welcher Claude-Code-Version etwas Neues auftaucht. Alle Befunde und Optionen: [`doctor`](../reference/10-befehle.md#doctor).

# Hooks einrichten

`total-recall` läuft nicht ständig. Claude Code startet es über Hooks. Ein Hook ist ein Befehl, den Claude Code bei bestimmten Ereignissen ausführt.

## Der Block für Claude Code

Diesen Block gibt `total-recall hooks print --harness claude` aus (**geplant**). Er gehört in `~/.claude/settings.json`. Bei Stefan wird er über die dotfiles eingetragen.

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
| `SubagentStop` | Ein Subagent ist fertig | Liest die neuen Zeilen ein, inklusive Subagent-Transkript |
| `SessionEnd` | Session endet | Liest den Rest ein und macht ein Backup, falls das letzte älter als 24 h ist |

Mit `--hook` liest `total-recall` das Hook-JSON von stdin. Daraus nimmt es `transcript_path`, `session_id`, `cwd` und `hook_event_name`. Gelesen wird nur ab der Stelle, an der der letzte Lauf aufgehört hat. Ein Lauf dauert deshalb meist nur Millisekunden.

## Warum `--detach`

Ein Hook darf Claude Code nie bremsen. `--detach` startet `total-recall` als losgelösten Hintergrundprozess. Der Hook kehrt sofort mit Exit-Code 0 zurück.

- Claude Code wartet nicht auf das Einlesen.
- Ein Fehler stoppt nie deine Session. Im Hook-Modus ist der Exit-Code immer 0.
- Fehler landen im Log `total-recall.log` neben der Datenbank. `total-recall status` zeigt die letzten.

Laufen zwei Hooks gleichzeitig, ist das kein Problem. Je Quelldatei gibt es ein Lock. Events sind idempotent, doppelt eingelesen wird nichts.

## Das Nachlesen beim Start

Beim `SessionStart` liest `total-recall` zusätzlich alles nach (wie `ingest --all`). Das fängt Lücken auf:

- Sessions, die vor der Installation entstanden sind.
- Hooks, die ausgefallen sind, etwa nach einem Absturz.
- Sessions ohne `SessionEnd`, zum Beispiel weil das Terminal geschlossen wurde.

Weil nur Neues gelesen wird, ist das auch bei vielen Transkripten schnell.

## Prüfen

Nach ein paar Antworten in einer neuen Session:

```sh
total-recall status
total-recall sessions --since 2026-11-01
```

Steht dort nichts, schau ins Log neben der Datenbank.

## Erstes Einlesen

Einmal nach der Installation:

```sh
total-recall ingest --all
```

Damit landet alles in der Datenbank, was Claude Code noch nicht gelöscht hat.

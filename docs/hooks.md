# Hooks einrichten

`total-recall` läuft nicht ständig. Claude Code startet es über Hooks. Ein Hook ist ein Befehl, den Claude Code bei bestimmten Ereignissen ausführt.

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

Mit `--hook` liest `total-recall` das Hook-JSON von stdin. Daraus nimmt es `transcript_path`, `agent_transcript_path` (nur bei `SubagentStop`), `session_id`, `cwd` und `hook_event_name`. Andere Felder werden ignoriert. Gelesen wird nur ab der Stelle, an der der letzte Lauf aufgehört hat. Ein Lauf dauert deshalb meist nur Millisekunden.

Ein Beispiel für das JSON, das Claude Code bei `Stop` schickt:

```json
{
  "session_id": "9a1b2c3d-0000-4000-8000-000000000000",
  "transcript_path": "/Users/me/.claude/projects/-Users-me-develop-repo/9a1b2c3d-0000-4000-8000-000000000000.jsonl",
  "cwd": "/Users/me/develop/repo",
  "hook_event_name": "Stop",
  "stop_hook_active": false
}
```

Bei `SubagentStop` kommen unter anderem `agent_id`, `agent_type` und `agent_transcript_path` dazu. `transcript_path` ist dann das Transkript der Haupt-Session, `agent_transcript_path` das des Subagents (`<session>/subagents/agent-<id>.jsonl`); `total-recall` liest beide. Die Feldnamen folgen der [Hooks-Referenz von Claude Code](https://code.claude.com/docs/en/hooks) (geprüft am 2026-11-01).

Zum Ausprobieren ohne Claude Code: `total-recall ingest --hook < stop.json`.

## Warum `--detach`

Ein Hook darf Claude Code nie bremsen. `--detach` startet `total-recall` als losgelösten Hintergrundprozess. Der Hook kehrt sofort mit Exit-Code 0 zurück.

- Claude Code wartet nicht auf das Einlesen.
- Ein Fehler stoppt nie deine Session. Im Hook-Modus ist der Exit-Code immer 0.
- Im Hook-Modus schreibt `total-recall` nichts auf stdout. Claude Code würde die Ausgabe eines `SessionStart`-Hooks sonst in den Kontext der Session übernehmen.
- Fehler landen im Log `total-recall.log` neben der Datenbank, eine Zeile je Fehler: `2026-10-30T08:12:01Z ERROR hook Stop: …`. `total-recall status` zeigt die letzten.
- Meldet Claude Code eine Transkript-Datei, die (noch) nicht existiert, ist das kein Fehler. Das passiert beim SessionStart einer neuen Session und beim SubagentStop interner Agenten ohne Transkript. Der Hook schreibt dann eine INFO-Zeile und liest den Rest. Taucht die Datei später auf, liest der nächste Hook sie ein.
- Kaputtes oder unvollständiges Hook-JSON, eine fehlende Transkript-Datei oder eine Datenbank aus einer neueren `total-recall`-Version: alles landet als Fehler im Log, geschrieben wird dann nichts.

Mit `--detach` übergibt der Hook das JSON in einer temporären Datei an den Hintergrundprozess. Der liest sie und löscht sie gleich wieder.

Laufen zwei Hooks gleichzeitig, ist das kein Problem. Je Quelldatei gibt es ein Lock (Lockdateien im Ordner `locks/` neben der Datenbank). Ist eine Datei gerade gesperrt, überspringt der zweite Lauf sie, statt zu warten; der nächste Hook holt den Rest. Events sind idempotent, doppelt eingelesen wird nichts.

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

## Nach einem Claude-Code-Update { #nach-einem-claude-code-update }

Claude Code ändert sein Transkript-Format ohne Ankündigung: neue Record-Typen, neue Content-Blöcke, umbenannte Felder. `total-recall` liest dann trotzdem weiter, übersieht aber womöglich Neues. Nach einem Update deshalb:

```sh
total-recall doctor
```

- Exit-Code 0: alles bekannt.
- Exit-Code 2: Es gibt Unbekanntes. Die Ausgabe nennt Typ, Anzahl und erste Fundstelle. Ein Blick in die genannte Zeile zeigt, ob darin etwas Archivierenswertes steht. Wenn ja, braucht der Adapter ein Update; wenn nein, gehört der Typ in die Liste der bewusst übersprungenen Typen im Adapter (bis dahin hilft `harness.claude.skip_record_types` in der Config).

Die Versionsliste zeigt, ab welcher Claude-Code-Version etwas Neues auftaucht.

## Erstes Einlesen

Einmal nach der Installation:

```sh
total-recall ingest --all
```

Damit landet alles in der Datenbank, was Claude Code noch nicht gelöscht hat.

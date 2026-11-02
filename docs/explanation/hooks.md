---
title: Einlesen über Hooks
icon: lucide/workflow
---
# Einlesen über Hooks

Claude Code ruft `total-recall` über Hooks auf; es läuft kein Hintergrunddienst. Diese Seite erklärt, was ein Hook-Lauf liest, warum er sich sofort löst und wie Lücken aufgefangen werden. Den Block zum Eintragen und die Schritte danach zeigt [Hooks einrichten](../how-to/20-hooks-einrichten.md).

## Das Hook-JSON

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

Die Einzelheiten zu Offsets, Locks, Batches und dem Hook-Modus stehen in der Spezifikation unter [Ingest, Hooks, Nebenläufigkeit](design.md#ingest-hooks-nebenlaufigkeit).

# Für Agenten

Agenten sollen das Archiv selbst auswerten können. Etwa: „Woran habe ich letzte Woche gearbeitet?“ oder „Was haben wir damals zu X entschieden?“

Dafür bringt `total-recall` eine Anleitung mit, einen Skill im üblichen `SKILL.md`-Format.

```sh
total-recall llm --skill
```

## Was drinsteht

Der Skill ist auf Englisch, weil er für Agenten gedacht ist. Er ist für maschinelle Nutzung aufgebaut: zuerst das, was ein Agent kopieren kann, danach die Erklärung.

1. **Die vier Kernabfragen** als Copy-Paste-Blöcke für `total-recall sql --json`: Welche Sessions sprechen über ein Thema und wann (Volltextsuche über `events_fts`, gruppiert nach Session, mit Repo und Zeitraum), was der Mensch dazu gesagt hat (`actor = 'human'`), die Konversation eines Zeitfensters und die Zeitleiste eines Themas über Sessions und Repos hinweg.
2. **Der Ausgabevertrag:** immer `--json`, Fehler als `{"error":{"kind":…,"message":…}}` auf stdout mit Exit-Code 1, jeder Schlüssel immer vorhanden (`null` statt fehlend), `[]` bei leerem Ergebnis, Kürzung nur bei `--format table` im Terminal, `--format jsonl` für große Lesungen.
3. **Kostenhinweise:** `LIMIT` in Erkundungsabfragen, Tool-Ergebnisse sind auf 4 KiB gekürzt, `show` mit `--since/--until`, `--limit/--offset` oder `--from-event` begrenzen.
4. **Schema und Fallstricke:** das Zeitstempelformat in der Datenbank und das Idiom, um ein lokales Zeitfenster darauf abzubilden, die Werte von `actor`, Subagenten, welche Felder von `sessions --json` berechnet werden und keine Spalten sind, alle Tabellen und Views mit ihren Spalten, die JSON-Felder jedes Lesebefehls.
5. **Abgrenzung:** Das Archiv hält Konversation, keine Artefakte. Dateien und Design-Dokumente liest ein Agent im Repo.

Die Positionierung steht am Anfang: So erfährt ein Agent in Session A, was in Session B entschieden wurde, auch wenn B in einem anderen Repo lief. `sql` mit FTS5 steht deshalb vor `search`: `search` liefert Treffer, `sql` liefert Antworten.

Ein Test prüft die im Skill genannten Spalten gegen das echte Schema (`PRAGMA table_xinfo`) und die genannten JSON-Felder gegen die echte Ausgabe der Befehle auf einer synthetischen Datenbank. Der Skill beschreibt also kein Feld, das es nicht gibt.

## Als Skill installieren

Der Text ist ins Binary eingebettet. Er passt also immer zur installierten Version. So legst du ihn für Claude Code als persönlichen Skill ab:

```sh
mkdir -p ~/.claude/skills/total-recall
total-recall llm --skill > ~/.claude/skills/total-recall/SKILL.md
```

Nach einem Update von `total-recall` den Befehl wiederholen.

Alternativ reicht ein Hinweis in der `CLAUDE.md`: „Für Fragen zu früheren Sessions: `total-recall llm --skill` lesen.“

## Sicherheit

- Agenten brauchen nur `sql` und die lesenden Befehle. `sql` kann nichts ändern.
- Geheimnisse sind schon beim Einlesen maskiert. Siehe [Datenschutz](datenschutz.md).

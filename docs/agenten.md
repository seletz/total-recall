# Für Agenten

Agenten sollen das Archiv selbst auswerten können. Etwa: „Woran habe ich letzte Woche gearbeitet?“ oder „Was haben wir damals zu X entschieden?“

Dafür bringt `total-recall` eine Anleitung mit, einen Skill im üblichen `SKILL.md`-Format.

```sh
total-recall llm --skill
```

## Was drinsteht

Der Skill ist auf Englisch, weil er für Agenten gedacht ist. Er enthält:

- wann der Skill passt (Fragen zu früheren Sessions, Arbeitsverlauf, Zeitaufwand, frühere Entscheidungen),
- was das Archiv enthält,
- die Befehle mit `--json`,
- das Schema: Tabellen, Spalten, Views,
- Beispiel-SQL,
- Fallstricke: UTC-Zeiten, Bedeutung von `actor`, maskierte Geheimnisse, Datenlücken vor dem ersten Einlesen.

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

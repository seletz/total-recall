# Konfiguration

Alles steht in einer TOML-Datei. Du brauchst sie nicht: Fehlt sie, gelten die Defaults.

## Wo liegt die Datei

| Reihenfolge | Ort |
|---|---|
| 1 | `--config PFAD` |
| 2 | Umgebungsvariable `TOTAL_RECALL_CONFIG` |
| 3 | `$XDG_CONFIG_HOME/total-recall/config.toml` |
| 4 | `~/.config/total-recall/config.toml` |

Die Datei wird über die dotfiles verwaltet.

## Rangfolge der Werte

Für jeden Wert gilt:

**Flag > Umgebungsvariable > Config-Datei > Default**

Beispiel: `--db /tmp/test.db` schlägt `TOTAL_RECALL_DB`, und das schlägt `db = …` in der Datei.

`total-recall config show` zeigt die wirksame Konfiguration. Zu jedem Wert steht, woher er kommt.

`total-recall config init` schreibt eine kommentierte Vorlage. Eine bestehende Datei wird nie überschrieben.

## Regeln

- `~` und Umgebungsvariablen (`$HOME`, `${VAR}`) werden in Pfaden aufgelöst.
- Unbekannte Schlüssel sind ein Fehler. So fallen Tippfehler sofort auf.

## Vollständige Vorlage

```toml
# ~/.config/total-recall/config.toml

db = "~/Library/Application Support/total-recall/archive.db"
timezone = "Europe/Berlin"      # für Auswertungen (stats, Tagesgrenzen)
log = ""                         # leer = neben der DB

[backup]
dir = "~/Documents/total-recall"
keep_daily = 7
keep_monthly = 0                 # 0 = alle Monatskopien behalten
min_interval = "24h"             # für `backup --if-older`

[redact]
enabled = true
extra_patterns = []              # zusätzliche Go-Regexe
allow = []                       # Regexe, die nie maskiert werden

[ingest]
tool_result_max_bytes = 4096
store_raw = true

[harness.claude]
enabled = true
projects_dir = "~/.claude/projects"   # Default: $CLAUDE_CONFIG_DIR/projects bzw. ~/.claude/projects
skip_record_types = []                # zusätzlich zu den Standard-Ausschlüssen

[harness.claude.actors]
# zusätzliche Präfixe für die Actor-Klassifikation
peer_prefixes = []
automation_prefixes = ["Hourly ", "Weekend watch"]

[git]
resolve = true
```

## Die Schlüssel im Einzelnen

### Oberste Ebene

`db`
:   Pfad zur SQLite-Datenbank. Default auf macOS: `~/Library/Application Support/total-recall/archive.db`. Auf Linux: `$XDG_DATA_HOME/total-recall/archive.db`. Override: `--db`, `TOTAL_RECALL_DB`. Die Datei gehört **nicht** in iCloud, siehe [Datenschutz](datenschutz.md).

`timezone`
:   Zeitzone für Auswertungen. Bestimmt, wo ein Tag anfängt und aufhört, etwa in `stats` und in der View `daily_activity`. In der Datenbank selbst stehen alle Zeiten in UTC. Default: `Europe/Berlin`. Override: `--tz`.

`log`
:   Logdatei für Fehler aus Hook-Läufen und Hintergrundprozessen (`--detach`) sowie Hinweise wie Backups vor einer Schema-Migration. Leer heißt: `total-recall.log` neben der Datenbank. Die Datei wächst ohne Begrenzung; Fehler sollten selten sein.

### `[backup]`

`dir`
:   Zielordner für Backups. Default: `~/Documents/total-recall`, also iCloud. Override: `--backup-dir`, `TOTAL_RECALL_BACKUP_DIR`.

`keep_daily`
:   So viele Tageskopien bleiben erhalten. Default: 7.

`keep_monthly`
:   So viele Monatskopien bleiben erhalten. Eine Monatskopie ist die letzte Kopie eines Monats. `0` heißt: alle behalten.

`min_interval`
:   Reserviert. Derzeit ungenutzt: `backup --if-older` braucht eine ausdrückliche Dauer, etwa `--if-older 24h` wie im Hook.

### `[redact]`

`enabled`
:   Geheimnisse vor dem Speichern maskieren. Default: `true`. Abschalten ist nur für Tests sinnvoll.

`extra_patterns`
:   Zusätzliche reguläre Ausdrücke (Go-Syntax). Treffer werden maskiert.

`allow`
:   Reguläre Ausdrücke für Text, der nie maskiert werden darf. Hilft bei Fehlalarmen.

### `[ingest]`

`tool_result_max_bytes`
:   Tool-Ergebnisse (etwa die Ausgabe eines Shell-Befehls) werden im Feld `text` auf diese Länge gekürzt. Default: 4096 Bytes.

`store_raw`
:   Die Original-JSON-Zeile jedes Events (maskiert) in der Spalte `raw` speichern. Kostet Platz, erlaubt aber spätere Neuauswertung. Default: `true`.

### `[harness.claude]`

`enabled`
:   Claude Code einlesen. Default: `true`.

`projects_dir`
:   Wo die Transkripte liegen. Default: `$CLAUDE_CONFIG_DIR/projects`, sonst `~/.claude/projects`.

`skip_record_types`
:   Weitere Record-Typen, die übersprungen werden. Die Standard-Ausschlüsse (`attachment`, `api-request*`, `file-history-*` usw.) gelten immer.

### `[harness.claude.actors]`

Jedes Event bekommt einen `actor`: Wer hat das geschrieben? Mensch, Agent, ein anderer Agent oder eine Automatik. Die Regeln stehen im [Design](design.md#actor-klassifikation-claude-code). Hier ergänzt du eigene Präfixe.

`peer_prefixes`
:   Nachrichten, die mit einem dieser Texte beginnen, gelten als `peer` (ein anderer Agent).

`automation_prefixes`
:   Nachrichten, die mit einem dieser Texte beginnen, gelten als `automation`. Beispiel: geplante Prompts wie „Hourly …“.

### `[git]`

`resolve`
:   Zu jedem Arbeitsverzeichnis Git-Root und Remote-URL ermitteln. Default: `true`. Der Branch kommt aus dem Transkript. Das Ergebnis wird in der Tabelle `dirs` gespeichert, damit es nach dem Löschen eines Worktrees erhalten bleibt. Ein gelöschter Worktree unter `<repo>__worktrees/<name>` wird dem Repo `<repo>` daneben zugeordnet. „Kein Git-Repo“ wird nach 7 Tagen neu geprüft.

## Umgebungsvariablen

| Variable | Wirkt auf |
|---|---|
| `TOTAL_RECALL_CONFIG` | Pfad der Config-Datei |
| `TOTAL_RECALL_DB` | `db` |
| `TOTAL_RECALL_BACKUP_DIR` | `backup.dir` |
| `CLAUDE_CONFIG_DIR` | Default von `harness.claude.projects_dir` |
| `XDG_CONFIG_HOME`, `XDG_DATA_HOME` | Default-Pfade |

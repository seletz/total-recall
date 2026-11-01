# Design

`total-recall` archiviert die Sessions von Coding-Agenten (Claude Code zuerst, später weitere Harnesses) in eine lokale SQLite-Datenbank, damit sie nicht verloren gehen und sich auswerten lassen. Es ist harness-neutral: Jede Harness bekommt einen dünnen Adapter, alles andere ist gemeinsam.

## Ziele

- Nichts geht verloren, auch wenn die Harness ihre Transkripte löscht (Claude Code: `cleanupPeriodDays`).
- Je Session: Name, Anfragen und Antworten, Zeiten, Verzeichnis, Git-Repo-URL und Branch.
- Auswertbar von Menschen (CLI, Doku) und von Agenten (`total-recall llm --skill` gibt einen Skill aus, `total-recall sql` erlaubt Read-only-SQL).
- Kein Kopierjob, kein Daemon. Eingelesen wird über Hooks der Harness, inkrementell, plus Nachlesen beim Session-Start.

## Nicht-Ziele (v0.1)

- Keine Kontext-Injektion in laufende Sessions (anders als claude-recall).
- Keine semantische Suche, keine Embeddings. Volltextsuche über SQLite FTS5 reicht.
- Keine weiteren Harnesses außer Claude Code; die Adapter-Schnittstelle ist aber fertig.

## Technik

- Go, statisch gebaut (`CGO_ENABLED=0`). SQLite über `modernc.org/sqlite` (pure Go, FTS5 enthalten).
- CLI mit `github.com/spf13/cobra`. Jeder lesende Befehl hat `--json`.
- Releases über goreleaser, Homebrew-Cask im Tap `seletz/homebrew-tap` (Vorbild: `~/develop/odoo-work-cli/.goreleaser.yml`).
- Doku mit Zensical unter `docs/`, auf Deutsch (Vorbild für `zensical.toml`: `~/develop/digitalgedacht/dg-k8s/sichtung/zensical.toml`).
- Tasks über `mise` (`mise run build|test|lint|docs:serve|release`).
- TDD, tabellengetriebene Tests. Testdaten sind synthetisch. **Nie echte Transkripte ins Repo.**

## Pfade

| Was | Default | Override |
|---|---|---|
| Datenbank | `~/Library/Application Support/total-recall/archive.db` (macOS), `$XDG_DATA_HOME/total-recall/archive.db` (Linux) | `--db`, `TOTAL_RECALL_DB` |
| Backups | `~/Documents/total-recall/` (iCloud) | `--backup-dir`, `TOTAL_RECALL_BACKUP_DIR` |
| Logs (Hook-Fehler) | neben der DB, `total-recall.log` | |
| Claude-Code-Transkripte | `~/.claude/projects/**/*.jsonl` (inkl. `<session>/subagents/**`) | `CLAUDE_CONFIG_DIR` |

Die laufende DB liegt bewusst **nicht** in iCloud: SQLite-WAL-Dateien und iCloud-Sync vertragen sich nicht. `backup` schreibt per `VACUUM INTO` eine konsistente Kopie nach `~/Documents/total-recall/archive-YYYY-MM-DD.db`. Aufbewahrung: die letzten 7 Tageskopien plus die jeweils letzte Kopie jedes Monats.

## Konfiguration

Eine TOML-Datei für alles: `~/.config/total-recall/config.toml` (`$XDG_CONFIG_HOME` wird beachtet), Override per `--config` oder `TOTAL_RECALL_CONFIG`. Fehlt sie, gelten die Defaults. Rangfolge: Flag > Umgebungsvariable > Config-Datei > Default. `total-recall config show` zeigt die wirksame Konfiguration mit Herkunft je Wert, `total-recall config init` schreibt eine kommentierte Vorlage (überschreibt nie eine bestehende Datei). Die Datei wird über die dotfiles verwaltet.

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
raw_max_bytes = 16384            # größere Zeilen: raw = NULL, raw_truncated = 1; 0 = unbegrenzt
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

`~` und Umgebungsvariablen werden in Pfaden expandiert. Unbekannte Schlüssel sind ein Fehler (Tippfehler sollen auffallen).

## Architektur

```
cmd/total-recall          CLI (cobra), dünn
internal/model            gemeinsame Typen: Session, Event, Record
internal/harness          Adapter-Interface + Registry
internal/harness/claude   Adapter Claude Code (JSONL)
internal/gitinfo          cwd -> git root, remote URL, branch (mit Cache)
internal/redact           Maskieren von Geheimnissen
internal/store            SQLite: Schema, Migrationen, Upserts, Abfragen, Backup
internal/ingest           Orchestrierung: Quellen finden, Offsets, Locking, Store schreiben
internal/skill            eingebetteter SKILL.md für `llm --skill`
internal/config           TOML laden, Defaults, Env/Flag-Overrides, Pfad-Expansion
internal/version          Version via ldflags
```

Datenfluss: `ingest` fragt den Adapter nach Quelldateien, liest je Datei ab dem gespeicherten Byte-Offset, der Adapter liefert `model.Record`s, `redact` maskiert, `store` schreibt in einer Transaktion Events + Session-Upsert + neuen Offset.

## Datenmodell

Alle Zeiten UTC, ISO 8601 (`2026-10-31T22:21:58.123Z`) als TEXT.

### `sources`

Eine Zeile je eingelesener Datei.

| Spalte | Typ | |
|---|---|---|
| `path` | TEXT PK | absoluter Pfad |
| `harness` | TEXT | `claude` |
| `size` | INTEGER | Größe beim letzten Lesen |
| `mtime` | TEXT | |
| `offset` | INTEGER | bis hierhin gelesen (Ende der letzten vollständigen Zeile) |
| `ingested_at` | TEXT | |

Ist eine Datei kleiner als `offset` (ersetzt/gekürzt), wird ab 0 neu gelesen; Events sind idempotent.

### `sessions`

| Spalte | Typ | |
|---|---|---|
| `id` | TEXT PK | `<harness>:<native id>`, z.B. `claude:f2b299a1-…` |
| `harness` | TEXT | |
| `harness_version` | TEXT | zuletzt gesehene Version |
| `native_id` | TEXT | |
| `parent_id` | TEXT NULL | Subagent → Eltern-Session |
| `name` | TEXT NULL | eigener Titel (Claude: `custom-title`), maßgeblich |
| `ai_title` | TEXT NULL | von der Harness erzeugter Titel (Claude: `ai-title`, Legacy `summary`) |
| `agent_name` | TEXT NULL | |
| `display_name` | TEXT, generiert | `COALESCE(name, ai_title, agent_name)`; das zeigen `sessions`, `show`, `search` |
| `cwd` | TEXT | erstes gesehenes Arbeitsverzeichnis |
| `git_root` | TEXT NULL | |
| `git_remote_url` | TEXT NULL | normalisiert, ohne Credentials |
| `git_branch` | TEXT NULL | zuletzt gesehen |
| `model` | TEXT NULL | zuletzt gesehenes Modell |
| `started_at` | TEXT | min(ts) |
| `ended_at` | TEXT | max(ts) |

Upsert-Regel: `started_at` = Minimum, `ended_at` = Maximum, leere Werte überschreiben nie gesetzte, „zuletzt gesehen“-Felder nehmen den Wert des jüngsten Events.

### `events`

| Spalte | Typ | |
|---|---|---|
| `id` | TEXT PK | `<harness>:<native event id>`; ohne native ID: `<harness>:<sha1(path)>:<offset>` |
| `session_id` | TEXT FK | |
| `ts` | TEXT | |
| `kind` | TEXT | `message`, `tool_use`, `tool_result`, `system`, `meta` |
| `role` | TEXT | `user`, `assistant`, `system`, `tool` |
| `actor` | TEXT | `human`, `agent`, `peer`, `automation`, `tool` |
| `text` | TEXT | lesbarer Text (redigiert); bei Tool-Ergebnissen gekürzt auf 4 KiB |
| `tool_name` | TEXT NULL | |
| `model` | TEXT NULL | |
| `tokens_in` / `tokens_out` | INTEGER NULL | |
| `source_path` / `source_offset` | TEXT / INTEGER | Rückverweis |
| `raw` | TEXT NULL | Original-JSON der Zeile, redigiert (siehe unten); NULL, wenn die Zeile größer als `ingest.raw_max_bytes` ist |
| `raw_truncated` | INTEGER | 1, wenn `raw` wegen der Größe weggelassen wurde (Migration 3); `text` bleibt wie sonst |

`events_fts`: FTS5 über `text` (external content auf `events`, Trigger für insert).

### Views

- `exchanges`: je menschlicher Nachricht (`actor='human'`) die Anfrage plus die letzte Agenten-Antwort (`role='assistant'`, `kind='message'`) vor der nächsten menschlichen Nachricht derselben Session.
- `daily_activity`: Tag (lokale Zeit Europe/Berlin), Session, Repo, Anzahl human/agent-Events.

### Actor-Klassifikation (Claude Code)

| Fall | actor |
|---|---|
| `type=user`, Text vom Menschen getippt | `human` |
| Text beginnt mit „Another Claude session“, `<agent-message`, `[Cross-session`, Subagent-Hand-back | `peer` |
| `<task-notification>`, geplante Prompts („Hourly …“, Scheduled), `isMeta`, `<command-…>`/`<local-command-…>`, `This session is being continued`, `Base directory for this skill`, `<system-reminder>` | `automation` |
| `<bash-input>` | `human` (Stefan hat `!cmd` getippt) |
| `<pasted_content …>` | `human` |
| `type=user` mit `tool_result`-Blöcken | `tool` (kind `tool_result`) |
| `type=assistant` | `agent` (Text → kind `message`, `tool_use`-Block → kind `tool_use`) |

Ein Assistant-Eintrag mit mehreren Content-Blöcken ergibt mehrere Events (`<uuid>#<block index>`).

### Welche Claude-Records

Eingelesen: `user`, `assistant`, `system`, `custom-title`, `ai-title`, `agent-name`, `summary`. Alle anderen (`attachment`, `api-request*`, `bridge-session`, `file-history-*`, `queue-operation`, `mode`, …) werden übersprungen, der Offset läuft trotzdem weiter. `custom-title`/`ai-title`/`agent-name`/`summary` erzeugen kein Event, sondern setzen Felder der Session (`summary` und `ai-title` → `ai_title`, damit ein eigener Titel nie überschrieben wird).

## Redaction

Vor dem Schreiben werden `text` und `raw` maskiert (`«redacted:<art>»`): Private-Key-Blöcke, AWS-/GitHub-/Slack-/OpenAI-/Anthropic-Token-Formate, JWTs, `op://`-Referenzen bleiben (sind keine Geheimnisse), `password=…`/`passwort: …`/`token: …`-Zuweisungen, Basic-Auth in URLs, hochentropische Strings ≥ 32 Zeichen in Zuweisungen. Redaction ist nicht umkehrbar. Tests mit Positiv- und Negativfällen (z.B. Git-SHAs und UUIDs dürfen nicht maskiert werden).

## Git-Info

`gitinfo.Resolve(cwd)`: existiert `cwd`, dann `git -C cwd rev-parse --show-toplevel`, `git remote get-url origin`, `git branch --show-current`. Für Worktrees wird der Hauptrepo-Root über `git rev-parse --git-common-dir` bestimmt. Existiert `cwd` nicht mehr (gelöschter Worktree), wird zum nächsten existierenden Elternverzeichnis gegangen, das ein Git-Repo ist. Ist auch das kein Repo und liegt `cwd` unter `<pfad>/<repo>__worktrees/` (Worktree-Layout von worktrunk), wird `<pfad>/<repo>` aufgelöst. Ergebnis wird in Tabelle `dirs(cwd PK, git_root, git_remote_url, resolved_at)` gecacht, damit es nach dem Löschen eines Worktrees erhalten bleibt. Ein Treffer gilt für immer, „kein Repo“ wird nach 7 Tagen neu geprüft. Je Lauf wird jedes `cwd` höchstens einmal aufgelöst. Remote-URLs werden normalisiert (`git@github.com:o/r.git` → `https://github.com/o/r`), Credentials entfernt.

## Ingest, Hooks, Nebenläufigkeit

- `ingest [PATH…]`: einzelne Dateien. `ingest --all`: alle Quellen aller Adapter (= Nachlesen). `ingest --hook`: liest Hook-JSON von stdin (`transcript_path`, `session_id`, `cwd`, `hook_event_name`), liest diese Datei ein, und bei `SessionStart` zusätzlich `--all`.
- `--detach`: startet sich selbst als losgelösten Hintergrundprozess (neue Session-ID, stdin-Daten als Argument/Tempdatei) und kehrt sofort mit Exit 0 zurück. Hooks dürfen die Harness nie bremsen oder blockieren; Fehler gehen ins Log, Exit-Code im Hook-Modus ist immer 0.
- SQLite im WAL-Modus, `busy_timeout` 10 s. Je Quelldatei ein Lock (Lockfile in `locks/` neben der DB, `flock` ohne Warten: eine gesperrte Datei wird übersprungen), damit zwei parallele Läufe dieselbe Datei nicht doppelt lesen. Events sind ohnehin idempotent (`INSERT … ON CONFLICT DO NOTHING`).
- Geschrieben wird in Batches (500 Events) je Transaktion, zusammen mit dem Offset der ersten noch nicht geschriebenen Zeile. Ein Absturz verliert und verdoppelt daher nichts.
- `--detach` übergibt das Hook-JSON in einer Tempdatei (`--hook-payload`, intern), der Kindprozess löscht sie nach dem Lesen. Stdout/stderr des Kindprozesses gehen ins Log.
- Im Hook-Modus schreibt `ingest` nichts auf stdout (bei `SessionStart` landete das im Kontext der Session).
- Nur vollständige Zeilen lesen; eine halb geschriebene letzte Zeile bleibt für den nächsten Lauf.

Hooks für Claude Code (`total-recall hooks print --harness claude` gibt diesen Block aus, eingetragen wird er manuell bzw. über die dotfiles):

| Event | Befehl |
|---|---|
| `Stop`, `SubagentStop` | `total-recall ingest --hook --detach` |
| `SessionStart` | `total-recall ingest --hook --detach` (liest zusätzlich alles nach) |
| `SessionEnd` | `total-recall ingest --hook --detach` und `total-recall backup --if-older 24h --detach` |

## Schema-Versionierung

- Die Tabelle `schema_version` hält die Version der Datenbank. Jede Migration ist ein SQL-Block in `internal/store/schema.go`, Version = Position in der Liste.
- **Migrationen sind vorwärts-only. Eine bestehende Migration wird nie geändert, neue werden nur angehängt.**
- Ein schreibendes Öffnen (`ingest`, `backup`) wendet fehlende Migrationen an. Vorher schreibt es per `VACUUM INTO` eine Kopie `<db>.pre-v<N>.bak` neben die DB (N = alte Version) und loggt das. Für eine neue, leere DB entfällt die Kopie.
- Ist die DB neuer als das Programm, ist das ein Fehler (`ErrSchemaTooNew`); im Hook-Modus wird er geloggt, Exit 0, nichts geschrieben.
- Lesende Befehle öffnen read-only und migrieren nie. Neuer → Fehler. Älter → Warnung mit dem Hinweis, einen schreibenden Befehl auszuführen; Abfragen laufen, soweit sie keine neuen Spalten brauchen.
- `status` zeigt Version der DB und des Programms.
- Ein Test baut die DB in jeder historischen Version (Migrationen 1..k), füllt sie und migriert auf den neuesten Stand. Neue Migrationen sind damit automatisch gegen alle älteren Stände getestet.

## CLI

| Befehl | Zweck |
|---|---|
| `ingest` | siehe oben |
| `status` | DB-Pfad, Größe, Anzahl Sessions/Events, letzter Ingest je Harness, letzte Fehler aus dem Log, letztes Backup |
| `sessions` | Liste; Filter `--since`, `--until`, `--repo`, `--harness`, `--name`, `--limit`, `--subagents` (sonst nur Haupt-Sessions); Spalten Start, Dauer, ID-Präfix, Name, Repo, Branch, #human, #agent |
| `show SESSION` | Verlauf einer Session (Anfragen/Antworten, `--all` inkl. Tools) |
| `search QUERY` | FTS5-Suche mit Snippets; Filter wie `sessions`, `--actor`, `--raw` (FTS5-Syntax, sonst werden die Wörter quotiert) |
| `stats` | Kennzahlen für einen Zeitraum (`--month YYYY-MM` oder `--since/--until`): je Tag, je Wochentag×Stunde (lokale Zeit), je Repo: Sessions, human-, agent-Events. Basis für `monthly_stats.py` im Vault. |
| `sql QUERY` | Read-only-SQL (DB mit `mode=ro` und `PRAGMA query_only`), Ausgabe Tabelle/JSON/CSV |
| `backup` | `VACUUM INTO` + Aufbewahrung; `--if-older DURATION` |
| `hooks print --harness claude` | Hook-Konfiguration als JSON |
| `llm --skill` | gibt den eingebetteten `SKILL.md` aus (Schema, Befehle, Beispiel-SQL), damit Agenten die Daten auswerten können |
| `config show` / `config init` | wirksame Konfiguration / Vorlage schreiben |
| `version` | |

Globale Flags: `--config`, `--db`, `--tz` (Default `Europe/Berlin` für Auswertungen), `--backup-dir`. `--json` haben die lesenden Befehle einzeln. `backup.min_interval` ist derzeit ungenutzt; `--if-older` braucht eine ausdrückliche Dauer.

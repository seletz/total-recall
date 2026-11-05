---
title: "Design (Spezifikation)"
icon: lucide/drafting-compass
---
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
- Releases über goreleaser, Homebrew-Cask im Tap `seletz/homebrew-tap`. Die Release Notes je Version stehen in `docs/reference/90-aenderungen.md`; der Release-Workflow stellt den Abschnitt des Tags (`scripts/release-notes.sh`, `--release-header`) vor den generierten Commit-Changelog. `scripts/release.sh` taggt nur, wenn der Abschnitt existiert. Jedes Archiv hat eine SBOM (CycloneDX 1.6, JSON) aus `cyclonedx-gomod bin` gegen das fertige Binary, erzeugt im `sboms:`-Block von goreleaser über `tools/sbom` (`internal/sbom`): Hauptkomponente `total-recall` ohne `v`, purls mit `GOOS`/`GOARCH` der Zielplattform, `GOPROXY=off`; alle SBOMs stehen in `checksums-<tag>.txt`. `tools/sbom check` prüft Dateinamen, Kopf, Hauptkomponente, Plattform, Modulhashes gegen `go.sum` und die Checksummen; der Release-Lauf prüft jede SBOM vor Prüfsummen und Upload, die CI jeden Pull Request am Snapshot (`mise run sbom:check`). Die darwin-Binaries signiert und notarisiert der Block `notarize.macos` (quill, auf dem Linux-Runner) in der Build-Phase von goreleaser, also vor Archiv, SBOM und Prüfsummen; aktiv nur, wenn `release.yml` die Secrets `MACOS_SIGN_P12`, `MACOS_SIGN_PASSWORD`, `MACOS_NOTARY_KEY`, `MACOS_NOTARY_KEY_ID` und `MACOS_NOTARY_ISSUER_ID` liefert, sonst bleiben die Binaries unsigniert (Snapshot, CI, Forks). Der `postflight`-Schritt des Casks (xattr) bleibt als Absicherung, bis ein notarisiertes Release verifiziert ist. Beschreibung für Benutzer: `docs/reference/50-release.md`.
- Doku mit Zensical unter `docs/`, auf Deutsch, veröffentlicht auf GitHub Pages bei jedem Push nach `develop` (`.github/workflows/docs.yml`).
- Tasks über `mise` (`mise run build|test|lint|demo:db|docs:serve|docs:build|docs:screenshots|docs:screenshots:check|release|release:snapshot|sbom:check`).
- **Demo-Archiv.** `mise run demo:db` erzeugt mit `tools/demo` das synthetische Demo-Archiv `build/demo.db` (`internal/demo`): eine erfundene Entwicklerin, fünf erfundene Projekte und ein Worktree, Sessions mit Subagenten von Mai bis Oktober 2026 mit Kernzeit, Abenden, Nächten über Mitternacht, Wochenenden, Feiertagen in Baden-Württemberg (Fronleichnam gearbeitet) und einem Urlaub; die Woche ab 2026-06-01 ist für die Screenshots von Hand gelegt, die Sessions im Oktober zum Release von kompass ausformuliert. Die Erzeugung ist deterministisch (eigener Zufallsgenerator mit festem Seed, feste Uhr `demo.Now`). Das Archiv ist die gemeinsame große Fixture: `fixturedb.Demo` öffnet die mit `TOTAL_RECALL_DEMO_DB` benannte Datei (die mise-Tasks `test` und `test:race` sowie die CI setzen sie) und baut sonst eine eigene Kopie im Temp-Verzeichnis; kleine gezielte Fixtures (`testfixture.Small`, `testfixture.Activity`) bleiben.
- **Screenshots.** `mise run docs:screenshots` erzeugt mit `tools/screenshots` die Bilder der TUI unter `docs/assets/screenshots/` aus dem Demo-Archiv (`internal/screenshot`): das Werkzeug steuert die TUI kopflos mit fester Größe und fester Uhr, zeichnet die Frames selbst als PNG (Zelle für Zelle, eingebettete Schrift Go Mono) und schreibt die Text-Frames als Goldens nach `internal/screenshot/testdata/frames/`. `TestFramesGolden` vergleicht die Frames mit den Goldens, `mise run docs:screenshots:check` (`scripts/screenshots-check.sh`) rendert in ein temporäres Verzeichnis und vergleicht Bilder und Goldens byteweise mit den eingecheckten; die CI ruft es auf. **Wer die TUI ändert, führt `mise run docs:screenshots` aus und checkt Bilder und Goldens ein**, sonst schlagen Test und Prüfung fehl. Das Rendering ist deterministisch (eingebettete Schrift, feste Uhren, reines Go), auf derselben Plattform bis auf das Byte; zwischen arm64 und amd64 rundet der Schrift-Rasterizer einzelne Pixel anders, deshalb vergleicht die CI nur die Goldens (`--goldens-only`), und wer nicht auf einem arm64-Mac arbeitet, nutzt die Option ebenfalls. Zeigt die Doku eine Ansicht oder einen Zustand der TUI, gibt es dafür einen `Shot` in `internal/screenshot/frames.go`; Skizzen von Hand gibt es nur noch im [Spike](spike-tui.md#bildschirme) als Entwurf. `tools/anonymize` kopiert optional ein echtes Archiv ohne Namen und Inhalte, um die eigene Statistik anzusehen; die Doku nutzt es nicht.
- TDD, tabellengetriebene Tests. Testdaten sind synthetisch. **Nie echte Transkripte ins Repo.**

## Pfade

| Was | Default | Override |
|---|---|---|
| Datenbank | `~/Library/Application Support/total-recall/archive.db` (macOS), `$XDG_DATA_HOME/total-recall/archive.db` (Linux) | `--db`, `TOTAL_RECALL_DB` |
| Backups | `~/Documents/total-recall/` (iCloud) | `--backup-dir`, `TOTAL_RECALL_BACKUP_DIR` |
| Logs (Hook-Fehler) | neben der DB, `total-recall.log`; über 5 MiB wird es nach `total-recall.log.1` rotiert (eine alte Generation) | `log` |
| Claude-Code-Transkripte | `~/.claude/projects/**/*.jsonl` (inkl. `<session>/subagents/**`) | `CLAUDE_CONFIG_DIR` |

Die laufende DB liegt bewusst **nicht** in iCloud: SQLite-WAL-Dateien und iCloud-Sync vertragen sich nicht. `backup` schreibt per `VACUUM INTO` eine konsistente Kopie in eine Temporärdatei neben der DB, komprimiert sie mit gzip nach `~/Documents/total-recall/archive-YYYY-MM-DD.db.gz` (erst `.tmp`, dann atomares Umbenennen). Aufbewahrung: die letzten 7 Tageskopien plus die jeweils letzte Kopie jedes Monats; unkomprimierte `archive-YYYY-MM-DD.db` älterer Versionen zählen mit, eine vom selben Tag wird durch die `.gz` ersetzt. `restore BACKUP_FILE [--to PATH] [--force]` entpackt eine Kopie in eine Datenbankdatei (Default: die konfigurierte DB), prüft sie (Schema-Version, `PRAGMA quick_check`) und überschreibt ein bestehendes Ziel nur mit `--force`.

## Konfiguration

Eine TOML-Datei für alles: `~/.config/total-recall/config.toml` (`$XDG_CONFIG_HOME` wird beachtet), Override per `--config` oder `TOTAL_RECALL_CONFIG`. Fehlt sie, gelten die Defaults. Rangfolge: Flag > Umgebungsvariable > Config-Datei > Default. `total-recall config show` zeigt die wirksame Konfiguration mit Herkunft je Wert, `total-recall config init` schreibt eine kommentierte Vorlage (überschreibt nie eine bestehende Datei). Die Datei wird über die dotfiles verwaltet.

```toml
# ~/.config/total-recall/config.toml

db = "~/Library/Application Support/total-recall/archive.db"
timezone = "Europe/Berlin"      # für Auswertungen (stats, Tagesgrenzen)
day_start = "04:00"              # Tagesgrenze für die Aktivität je Tag (stats --workday, TUI)
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

[work]
hours = "09:00-17:00"            # Kernzeit (stats --workday, TUI activity)
target = "8h"                    # Tagessoll an Arbeitstagen, Wochenende und Feiertage ohne Soll
daily_low = "6h"                 # Spanne darunter: unter Soll
daily_high = "9h"                # Spanne darüber: deutlich über Soll
weekly_low = "32h"               # Wochenarbeitszeit (5 Arbeitstage) darunter: unter Soll
weekly_high = "48h"              # darüber: deutlich über Soll
bundesland = "Baden-Württemberg" # gesetzliche Feiertage (internal/holiday); unbekannt/leer = bundesweit
count = "human"                  # was die Aktivität je Tag zählt: "human" oder "all"

[worktime]
max_gap = "20m"                  # Lücke zwischen eigenen Nachrichten, die voll zählt (stats --worktime)
min_credit = "3m"                # was eine längere Lücke zählt
grid = "15m"                     # Rundungsraster, 0 = keine Rundung
rounding = "up"                  # up | down | nearest
scope = "category"               # category | project
summary_max_chars = 200          # Länge des Buchungstexts je Tag und Kategorie (stats --worktime --summary), 0 = ohne Grenze
# [[worktime.category]]          # Regeln in Reihenfolge: name, match (Glob oder Liste) auf Projektname, Git-Root (auch mit ~), Remote-URL
# name = "kunde-a"
# match = ["https://github.com/kunde-a/*"]

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
peer_paste_headers = []          # erste Zeile eines allein eingefügten Blocks, z. B. ["# Task:", "Auftrag:"]
automation_prefixes = ["Hourly ", "Weekend watch"]

[git]
resolve = true

[accounts]                       # Namen für Account-/Organisations-IDs (sessions.account_id, organization_id)
# "aaaaaaaa-0000-4000-8000-000000000001" = "work"

[tui]
view = "search"                  # Ansicht beim Start; Argument geht vor
graphics = "auto"                # auto | text | kitty; --graphics geht vor
mouse = true                     # false = --no-mouse; --no-mouse=false geht vor
stats_period = "day"             # day | week | month
stats_dimension = "project"      # project | agent | directory
activity_window = "week"         # day | week | month
reload = "5s"                    # Archiv auf Änderungen prüfen, sichtbare Ansicht neu laden; "0" = aus; --reload geht vor

[keys]
# Aktion = Taste oder Liste von Tasten; nur genannte Aktionen ändern sich.
# Aktionsnamen mit Kontext-Präfix (global_, search_, session_, stats_,
# activity_), Katalog in internal/tui/keys. Tastennamen wie Bubble Tea sie
# meldet (a, G, ?, up, enter, ctrl+r, shift+tab); ctrl+c ist reserviert.
# Je Kontext gehört eine Taste zu einer Aktion; globale Tasten sind in
# keinem Kontext erneut erlaubt. Verstöße sind ein Fehler beim Laden.
# global_quit = ["q", "ctrl+q"]
```

`~` und Umgebungsvariablen werden in Pfaden expandiert. Unbekannte Schlüssel sind ein Fehler (Tippfehler sollen auffallen).

Zwei Stufen der Prüfung: Die Kerneinstellungen (Pfade, Zeitzone, `day_start`, `[backup]`, `[ingest]`, `[redact]`, `[accounts]`, Schlüssel und Typen) prüft `config.Load` für jeden Befehl. Die Tabellen `[work]`, `[worktime]`, `[tui]` und `[keys]` werden nur geparst und erst beim Gebrauch geprüft (`WorkHours`, `WorktimeSettings`, `ValidateTUI`, `KeyMap` liefern den Fehler; `Problems` alle vier für `config show` und `doctor`). Grund: Hooks laden die Config bei jedem Lauf; ein Fehler in einer Tabelle, die nur die TUI, `stats --workday` oder `stats --worktime` braucht, darf die Archivierung nicht stoppen.

## Architektur

```
cmd/total-recall          CLI (cobra), dünn
internal/model            gemeinsame Typen: Session, Event, Record
internal/harness          Adapter-Interface + Registry
internal/harness/claude   Adapter Claude Code (JSONL)
internal/gitinfo          cwd -> git root, remote URL, branch (mit Cache)
internal/redact           Maskieren von Geheimnissen
internal/store            SQLite: Schema, Migrationen, Upserts, Abfragen, Backup
internal/store/rawz       zstd-Kompression von events.raw mit eingebettetem Wörterbuch (tools/rawdict trainiert es aus synthetischen Zeilen)
internal/project          Projektname aus Git-Root bzw. Verzeichnis, Home als `~` (reine Funktionen; `stats --by project`, TUI)
internal/holiday          gesetzliche Feiertage je Bundesland (github.com/wlbr/feiertage), TUI-unabhängig; erfüllt `store.Holidays`
internal/worktime         Arbeitszeit je Tag, Kategorie und Projekt aus den Lücken zwischen eigenen Nachrichten (reine Funktionen; `stats --worktime`): Gutschrift, Kategorie-Regeln (Glob), Rundung
internal/ingest           Orchestrierung: Quellen finden, Offsets, Locking, Store schreiben
internal/skill            eingebetteter SKILL.md für `llm --skill`
internal/config           TOML laden, Defaults, Env/Flag-Overrides, Pfad-Expansion
internal/tui              TUI: App-Shell, Keymap, Maus; Views search, session, stats, activity sowie chart, theme, window, tuitest in Unterpaketen
internal/tui/keys         Katalog der Tasten-Aktionen (Name, Kontext, Default, Hilfetext), Validierung von [keys]
internal/version          Version via ldflags
```

Datenfluss: `ingest` fragt den Adapter nach Quelldateien, liest je Datei ab dem gespeicherten Byte-Offset, der Adapter liefert `model.Record`s (mit `raw` schon als Auszug, `harness.RawTrimmer`), `redact` maskiert, `store` komprimiert `raw` und schreibt in einer Transaktion Events + Session-Upsert + neuen Offset.

## Datenmodell

Alle Zeiten UTC, ISO 8601 (`2026-10-31T22:21:58.123Z`) als TEXT.

### `sources`

Eine Zeile je eingelesener Datei.

| Spalte | Typ | |
|---|---|---|
| `id` | INTEGER PK | Ziel von `events.source_id` (Migration 5) |
| `path` | TEXT UNIQUE | absoluter Pfad |
| `harness` | TEXT | `claude` |
| `size` | INTEGER | Größe beim letzten Lesen |
| `mtime` | TEXT | |
| `offset` | INTEGER | bis hierhin gelesen (Ende der letzten vollständigen Zeile) |
| `ingested_at` | TEXT | |

Ist eine Datei kleiner als `offset` (ersetzt/gekürzt), wird ab 0 neu gelesen; Events sind idempotent. Ein Event, dessen Datei noch keine Zeile hat (Tests, Altbestand aus der Zeit vor Migration 5), legt eine Zeile nur mit `path` und `harness` an (`offset` 0); der nächste Ingest dieser Datei füllt sie.

### `sessions`

| Spalte | Typ | |
|---|---|---|
| `id` | TEXT PK | `<harness>:<native id>`, z.B. `claude:7d1e4c20-…` |
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
| `continued_in` | TEXT NULL | Session, in der diese fortgesetzt wurde (Claude: `continued-in`), volle ID mit Präfix; Index, kein Fremdschlüssel, weil die Zielsession später oder nie eingelesen wird (Migration 4) |
| `account_id` | TEXT NULL | Account, auf dem die Session lief (Claude: `session-owner.accountUuid`); Index (Migration 4) |
| `organization_id` | TEXT NULL | Organisation dieses Accounts (Claude: `session-owner.organizationUuid`) (Migration 4) |

Upsert-Regel: `started_at` = Minimum, `ended_at` = Maximum, leere Werte überschreiben nie gesetzte, „zuletzt gesehen“-Felder nehmen den Wert des jüngsten Events; `continued_in`, `account_id`, `organization_id`: der letzte nicht-leere Wert gewinnt. Die Gegenrichtung der Fortsetzung (`continued_from`) ist keine Spalte, sondern eine Abfrage über `continued_in` (`store.SessionInfo.ContinuedFrom`, `sessions --json`); bei mehreren Kandidaten die zuletzt gestartete Session. Beide Records erzeugen kein Event, deshalb lassen sich die Felder nicht aus `events.raw` nachfüllen: ein Archiv aus der Zeit vor Migration 4 bekommt sie durch `ingest --all --reread`, das jede Quelle ab Offset 0 liest (Events idempotent, Sessions gemerged).

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
| `source_id` / `source_offset` | INTEGER FK / INTEGER | Rückverweis: `sources.id` (Migration 5, vorher `source_path` TEXT) und Byte-Offset der Zeile |
| `raw` | BLOB NULL | Auszug der Original-Zeile als zstd-Frame (siehe [`raw`](#raw)), redigiert; NULL, wenn der Auszug größer als `ingest.raw_max_bytes` ist |
| `raw_truncated` | INTEGER | 1, wenn `raw` wegen der Größe weggelassen wurde (Migration 3); `text` bleibt wie sonst |

`events_fts`: FTS5 über `text` (external content auf `events`, Trigger für insert).

Die Lesepfade (`store.Events`, `EventWindow`, `Search`, also `show`, `search`, `sessions`, `stats` und die TUI) laden `raw` nicht: kein Lesebefehl gibt die Spalte aus, und sie ist mit Abstand die größte. `model.Event.Raw` ist aus dem Store deshalb immer leer. Nur `store.UserMessages` (`reindex --actors`) liest `raw`; `backup` kopiert die Datei. Den Pfad der Quelle holen die Lesepfade über `LEFT JOIN sources` (`store.eventCols`, `eventFrom`).

### `raw` { #raw }

`raw` ist seit Migration 5 kein wortgetreues Abbild der Transkriptzeile mehr, sondern ein **Auszug**, **komprimiert**. Die Transkripte bleiben die Quelle; `ingest --reread --rewrite-raw` schreibt `raw` aus ihnen neu.

**Auszug (Adapter).** Was in `raw` bleibt, entscheidet der Adapter, nicht der Store: das optionale Interface `harness.RawTrimmer` (`TrimRaw(raw []byte) ([]byte, error)`), das der Claude-Adapter mit einer Allowlist an einer Stelle (`internal/harness/claude/trim.go`) erfüllt. Es bleiben die Felder, die `Parse` und `Reclassify` lesen, sonst nichts:

| Ebene | bleibt | geht |
|---|---|---|
| Record | `type`, `uuid`, `parentUuid`, `timestamp`, `isMeta`, `isCompactSummary`, `scheduledTaskId`, `origin`, `promptSource`, `subtype`, `content` (System-Records), `message`, `toolUseResult` | `sessionId`, `cwd`, `gitBranch`, `version`, `agentId`, `userType`, `isSidechain`, `requestId`, `slug`, `permissionDecision`, `wire*`, `serverClassifier*`, … (alles, was in Spalten von `sessions`/`events` steht oder Buchhaltung der Harness ist) |
| `message` | `model`, `content` | `usage` (Token-Zahlen sind Spalten), `id`, `role`, `stop_reason`, `type`, … |
| `toolUseResult` | alles andere | `bashEditDiff`, `structuredPatch` (die Diffs wiederholen, was `message.content` trägt); ein `toolUseResult`, das kein Objekt ist (String, Liste), bleibt unverändert |

Der Adapter wendet `TrimRaw` beim Parse an (`model.Event.Raw` ist schon der Auszug, mit festen Schlüsselreihenfolge, idempotent); `ingest.raw_max_bytes` misst damit den Auszug, nicht die Zeile. Auf einer Kopie eines echten Archivs halbierte der Auszug `raw` (645 → 312 MiB). Niemand liest `message.id` oder `requestId` aus `raw`: die Token-Zuordnung je `message.id` passiert beim Parse auf der Transkriptzeile, `--rewrite-raw` findet Events über ihre ID.

**Kompression (Store).** `internal/store/rawz` ist die eine Stelle, die `raw` schreibt und liest: ein zstd-Frame (`klauspost/compress`, pure Go) mit **Dictionary-ID im Frame-Header** (`0x54520001` für das erste Wörterbuch), ohne Prüfsumme, mit Frame-Content-Size (die unkomprimierte Größe steht im Header, `status` schätzt daraus das Verhältnis ohne zu entpacken). Das Wörterbuch ist in das Binary eingebettet (`internal/store/rawz/dict/raw-v1.zdict`); der Decoder kennt **alle** je eingebetteten Wörterbücher (`rawz.Dicts`) und Frames ohne Wörterbuch, der Encoder nimmt das letzte. Ein neues Wörterbuch wird angehängt, nie ersetzt: alte Zeilen bleiben lesbar. Trainiert wird **nur aus synthetischen Zeilen** (`mise run dict:train`, `tools/rawdict`: die entschlackten Zeilen des Demo-Archivs plus generierte Records in den Formen echter Transkripte, deterministisch); nichts aus `~/.claude/projects` kommt ins Repo. Gemessen auf einer Kopie eines echten Archivs: zstd ohne Wörterbuch 43,5 % des Auszugs, mit dem synthetisch trainierten Wörterbuch 37,6 %, mit einem (nur lokal, zum Vergleich) aus echten Zeilen trainierten 31,1 %. `tools/rawdict measure` misst das für ein Archiv, nur Zahlen. Die Trainingszeilen sind bei jedem Lauf dieselben (`tools/rawdict samples`); der Wörterbuch-Builder von `klauspost/compress` bricht Gleichstände aber in Map-Reihenfolge, zwei Läufe liefern deshalb byteweise verschiedene, gleichwertige Wörterbücher. Maßgeblich ist das eingecheckte; ein neu trainiertes wird mit neuer ID angehängt, nie an die Stelle des alten gesetzt.

In SQL (`sql`, Tests) entpackt die Funktion `raw_text(raw)` (registriert im Treiber beim Laden von `internal/store`); ein Wert, der kein Frame ist (Textspalte eines älteren Archivs), kommt unverändert zurück.

### Views

- `exchanges`: je menschlicher Nachricht (`actor='human'`) die Anfrage plus die letzte Agenten-Antwort (`role='assistant'`, `kind='message'`) vor der nächsten menschlichen Nachricht derselben Session.
- `daily_activity`: Tag (lokale Zeit Europe/Berlin), Session, Repo, Anzahl human/agent-Events.

### Actor-Klassifikation (Claude Code)

| Fall | actor |
|---|---|
| `type=user`, Text vom Menschen getippt | `human` |
| Text beginnt mit „Another Claude session“, `<agent-message`, `[Cross-session` oder einem Präfix aus `peer_prefixes`; `origin.kind = "peer"` (Nachrichten anderer Sessions über den Socket, `<cross-session-message …>`); Subagent-Hand-back | `peer` |
| Nachricht besteht nur aus einem `<pasted_content …>`-Block, dessen erste nicht leere Zeile mit einem Eintrag aus `peer_paste_headers` beginnt (ein in das Terminal eingefügter Auftrag eines Koordinators) | `peer` |
| `<task-notification>`, geplante Prompts („Hourly …“, Scheduled), `isMeta`, `<command-…>`/`<local-command-…>`, `This session is being continued`, `Base directory for this skill`, `<system-reminder>` | `automation` |
| `<bash-input>` | `human` (Stefan hat `!cmd` getippt) |
| `<pasted_content …>` sonst | `human` |
| `type=user` mit `tool_result`-Blöcken | `tool` (kind `tool_result`) |
| `type=assistant` | `agent` (Text → kind `message`, `tool_use`-Block → kind `tool_use`) |

Ein Assistant-Eintrag mit mehreren Content-Blöcken ergibt mehrere Events (`<uuid>#<block index>`).

Reihenfolge: zuerst die Textregeln (Peer-Präfixe, Peer-Paste-Kopfzeilen, Human-Präfixe, Automation-Präfixe), dann `origin.kind`, dann `isMeta`/`isCompactSummary`/`scheduledTaskId`, dann Subagent-Transkript, sonst `human`. Eine Nachricht, die ein Koordinator per Terminal-Multiplexer in die Session tippt oder einfügt, hat keine harness-seitige Markierung (`origin.kind = "human"`, `promptSource = "typed"`); nur die konfigurierten Präfixe und Kopfzeilen unterscheiden sie von eigener Eingabe. `reindex --actors` wendet die aktuellen Regeln auf gespeicherte User-Nachrichten an: Textregeln auf `text`, die Flags aus `raw`; ohne `raw` (nicht gespeichert oder wegen der Größe weggelassen) entscheiden nur die Textregeln, sonst bleibt der Actor stehen.

### Welche Claude-Records

Eingelesen: `user`, `assistant`, `system`, `custom-title`, `ai-title`, `agent-name`, `summary`, `session-owner`, `continued-in`. Alle anderen (`attachment`, `api-request*`, `bridge-session`, `file-history-*`, `queue-operation`, `mode`, …) werden übersprungen, der Offset läuft trotzdem weiter. Die Liste der bekannten, bewusst übersprungenen Typen steht im Adapter (`claude.SkippedRecordTypes`); `doctor` meldet Typen, die in keiner der beiden Listen stehen. `custom-title`/`ai-title`/`agent-name`/`summary`/`session-owner`/`continued-in` erzeugen kein Event, sondern setzen Felder der Session (`summary` und `ai-title` → `ai_title`, damit ein eigener Titel nie überschrieben wird; `session-owner` → `account_id`, `organization_id`; `continued-in` → `continued_in` als `claude:<continuedInSessionId>`, ohne den Zeitstempel des Records zu übernehmen, denn er stammt vom Zeitpunkt der Fortsetzung und würde `ended_at` verfälschen).

## Neue Harness anbinden

Eine Harness bekommt ein Paket `internal/harness/<name>` mit einem Typ, der `harness.Adapter` erfüllt (`Name`, `Sources`, `Parse`), und wird in `internal/cli/ingest.go` (`deps`) samt eigenem Config-Abschnitt `[harness.<name>]` eingehängt. Optional erfüllt er `harness.Reclassifier` (`reindex --actors`) und `harness.RawTrimmer` (Auszug für `raw`, siehe [`raw`](#raw); ohne Trimmer wird die ganze Zeile gespeichert). Alles andere (Redaction, Git-Info, Store, Locking, Offsets, Kompression) ist gemeinsam.

Den Vertrag prüft die Konformitäts-Suite `internal/harness/harnesstest`. Jeder Adapter ruft sie mit synthetischen Fixtures auf:

```go
func TestConformance(t *testing.T) {
	harnesstest.Run(t, func(dir string) harness.Adapter { return New(dir) }, "testdata/projects")
}
```

Die Suite kopiert die Fixtures je Prüfung in ein Temp-Verzeichnis und prüft:

| Prüfung | Vertrag |
|---|---|
| Quellen | `Sources` ist deterministisch (gleiche Liste bei jedem Aufruf und jeder neuen Instanz), absolute Pfade, existierende Dateien, keine Duplikate |
| Felder | jedes Event hat `ID` und `SessionID` mit Präfix `<Name()>:`, einen Zeitstempel, `Kind`, `Role` und `Actor` aus den Mengen in `internal/model`; Session- und Parent-IDs der Records tragen dasselbe Präfix |
| IDs | Event-IDs sind über alle Quellen eindeutig und stabil über Läufe und Instanzen |
| Offsets | `Parse` ab 0 liefert die Dateigröße; erneutes `Parse` ab diesem Offset liefert nichts und denselben Offset |
| Halbe Zeile | eine unvollständige letzte Zeile wird nicht verbraucht (Offset = Anfang der Zeile) |
| Zwei Hälften | Datei an einer Zeilengrenze oder mitten in einer Zeile kürzen, parsen, wieder vervollständigen, ab dem gelieferten Offset weiterparsen: zusammen dieselben Events wie in einem Durchgang |
| Abbruch | mit abgebrochenem Kontext gibt `Parse` `context.Canceled` zurück und emittiert nichts; wird mitten im Lauf abgebrochen oder liefert `emit` einen Fehler, kommt der Fehler zurück, und ab dem gelieferten Offset geht nichts verloren und nichts doppelt |

Token-Zahlen werden beim Hälften-Vergleich nicht verglichen: Claude Code wiederholt die Usage einer API-Antwort in jeder Block-Zeile, der Adapter zählt sie je `Parse`-Aufruf nur einmal. Wird zwischen zwei Blöcken derselben Antwort geteilt, kann die Usage doppelt erscheinen.

Die Fixtures brauchen mindestens eine Quelle mit zwei Records, die Events erzeugen, und jede Quelle endet mit einem Zeilenumbruch. Die Suite testet sich selbst an einem Spielzeug-Adapter mit absichtlich eingebauten Fehlern.

Für die Format-Drift-Prüfung (`total-recall doctor`) implementiert ein Adapter zusätzlich `harness.FormatChecker`; die bekannten Record- und Block-Typen exportiert er, damit Parser und Prüfung dieselben Listen benutzen.

## Redaction

Vor dem Schreiben werden `text` und `raw` maskiert (`«redacted:<art>»`): Private-Key-Blöcke, AWS-/GitHub-/Slack-/OpenAI-/Anthropic-Token-Formate, JWTs, `op://`-Referenzen bleiben (sind keine Geheimnisse), `password=…`/`passwort: …`/`token: …`-Zuweisungen, Basic-Auth in URLs, hochentropische Strings ≥ 32 Zeichen in Zuweisungen. Redaction ist nicht umkehrbar. Tests mit Positiv- und Negativfällen (z.B. Git-SHAs und UUIDs dürfen nicht maskiert werden).

`text` läuft durch `Redactor.Text`: Die Regeln greifen tokenweise, ein Treffer ersetzt nur den Treffer, die Umgebung bleibt lesbar. `raw` läuft durch `Redactor.JSON`, das die Zeile als JSON liest und nur String-Werte anfasst (Schlüssel, Zahlen, Reihenfolge und Leerraum bleiben byteweise erhalten). Je String-Wert gilt die Reihenfolge wie in `Text`: Steht der Wert unter einem sensiblen Schlüssel (`password`, `secret`, `token`, `api_key`, `authorization`, … wie in den Zuweisungsregeln) und sieht nach einem Geheimnis aus (kein Platzhalter, keine Referenz, kein Typ), wird der ganze Wert `«redacted:secret»`. Sonst gelten die Textregeln auf den Wert, tokenweise. Nur wenn die keinen Treffer liefern und der Wert selbst **ein einziges Token** ist (≥ 32 Zeichen, ohne Leerraum, aus `[A-Za-z0-9+/=_.-]`) mit hoher Entropie, wird er als Ganzes `«redacted:high_entropy»`. Prosa, Shell-Befehle, Diffs, Tool-Ausgaben und Pfade enthalten Leerraum oder andere Zeichen und werden darum nie als Ganzes maskiert; das war bis v1.0.2 anders ([#130](https://github.com/seletz/total-recall/issues/130): `JSON` prüfte jeden Wert als Ganzes auf Entropie, lange Werte ab 32 Zeichen verschwanden komplett). Das Regressionsmuster steht in `internal/redact/redact_test.go` (`TestJSONTranscriptLineRegression`). Weil `raw` jetzt jeden Wert durch alle Textregeln schickt, hat jede Regel eine billige Vorprüfung (Literal wie `ghp_`, `AKIA`, `eyJ`, `://`, ein Schlüsselwort wie `token` in der Kleinschreibung, oder eine Folge von 32 Token-Zeichen für `high_entropy`); der reguläre Ausdruck läuft nur, wenn sie zutrifft. Auf echten Transkripten ist `JSON` damit etwa doppelt so schnell wie vor dem Fix.

Reparatur eines Archivs, dessen `raw` so beschnitten wurde: `ingest --reread --rewrite-raw` liest jede Quelle ab Offset 0, redigiert jede Zeile neu (seit Migration 5 den Auszug, siehe [`raw`](#raw)) und schreibt bei bestehenden Events nur `raw` und `raw_truncated` neu (`store.WriteOptions.RewriteRaw`: ein `UPDATE` dieser zwei Spalten vor dem `INSERT … ON CONFLICT DO NOTHING`). `text`, `actor` und die FTS-Tabelle bleiben unberührt; der Trigger `events_fts_au` reagiert nur auf `UPDATE OF text`, und `reindex --actors` kollidiert nicht. Event-, FTS- und Session-Zahlen bleiben gleich; `Summary.RawRewritten` zählt die neu geschriebenen Zeilen. Für gelöschte Quellen bleibt `raw` wie es ist.

## Git-Info

`gitinfo.Resolve(cwd)`: existiert `cwd`, dann `git -C cwd rev-parse --show-toplevel`, `git remote get-url origin`, `git branch --show-current`. Für Worktrees wird der Hauptrepo-Root über `git rev-parse --git-common-dir` bestimmt. Existiert `cwd` nicht mehr (gelöschter Worktree), wird zum nächsten existierenden Elternverzeichnis gegangen, das ein Git-Repo ist. Ist auch das kein Repo und liegt `cwd` unter `<pfad>/<repo>__worktrees/` (Worktree-Layout von worktrunk), wird `<pfad>/<repo>` aufgelöst. Ergebnis wird in Tabelle `dirs(cwd PK, git_root, git_remote_url, resolved_at)` gecacht, damit es nach dem Löschen eines Worktrees erhalten bleibt. Ein Treffer gilt für immer, „kein Repo“ wird nach 7 Tagen neu geprüft. Je Lauf wird jedes `cwd` höchstens einmal aufgelöst. Remote-URLs werden normalisiert (`git@github.com:o/r.git` → `https://github.com/o/r`), Credentials entfernt.

## Ingest, Hooks, Nebenläufigkeit

- `ingest [PATH…]`: einzelne Dateien. `ingest --all`: alle Quellen aller Adapter (= Nachlesen). `ingest --regit`: Git-Info für das `cwd` jeder Session neu auflösen (am `dirs`-Cache vorbei), `dirs` und `sessions.git_root`/`git_remote_url` aktualisieren; löst sich ein `cwd` nicht mehr auf, bleibt der gecachte Treffer. `ingest --reread` (mit `PATH…` oder `--all`): die Quellen ab Offset 0 lesen statt ab dem gespeicherten Lesestand (`ingest.Deps.Reread`); Events sind idempotent, Sessions werden gemerged, der neue Offset ist wieder das Dateiende. Für Altbestand nach einer Migration, die Session-Felder aus bisher übersprungenen Records ergänzt. `ingest --reread --rewrite-raw`: zusätzlich `raw` und `raw_truncated` bestehender Events aus der neu redigierten Zeile ersetzen (`ingest.Deps.RewriteRaw`, siehe [Redaction](#redaction)); nur mit `--reread` und nur bei `ingest.store_raw = true`, sonst würde `raw` geleert. `ingest --hook`: liest Hook-JSON von stdin (`transcript_path`, `session_id`, `cwd`, `hook_event_name`), liest diese Datei ein, und bei `SessionStart` zusätzlich `--all`.
- `--detach`: startet sich selbst als losgelösten Hintergrundprozess (neue Session-ID, stdin-Daten als Argument/Tempdatei) und kehrt sofort mit Exit 0 zurück. Hooks dürfen die Harness nie bremsen oder blockieren; Fehler gehen ins Log, Exit-Code im Hook-Modus ist immer 0.
- SQLite im WAL-Modus, `busy_timeout` 10 s. Je Quelldatei ein Lock (Lockfile in `locks/` neben der DB, `flock` ohne Warten: eine gesperrte Datei wird übersprungen), damit zwei parallele Läufe dieselbe Datei nicht doppelt lesen. Events sind ohnehin idempotent (`INSERT … ON CONFLICT DO NOTHING`).
- Geschrieben wird in Batches (500 Events) je Transaktion, zusammen mit dem Offset der ersten noch nicht geschriebenen Zeile. Ein Absturz verliert und verdoppelt daher nichts.
- `--detach` übergibt das Hook-JSON in einer Tempdatei (`--hook-payload`, intern), der Kindprozess löscht sie nach dem Lesen. Stdout/stderr des Kindprozesses gehen ins Log.
- Im Hook-Modus schreibt `ingest` nichts auf stdout (bei `SessionStart` landete das im Kontext der Session).
- Nur vollständige Zeilen lesen; eine halb geschriebene letzte Zeile bleibt für den nächsten Lauf.
- **Wartung und Dateitausch.** `maintenance` baut die Datei mit neuer Seitengröße auf einer Kopie neu auf (`PRAGMA page_size` + `VACUUM INTO`, geht auch aus dem WAL-Modus) und tauscht sie per `rename` aus. Ein Prozess, der die alte Datei schon offen hat, würde nach dem Tausch in deren Inode weiterschreiben; deshalb hält jedes schreibende Öffnen über die CLI (`store.WithLockDir`: `ingest`, `backup`, `reindex`) ein geteiltes `flock` auf `locks/maintenance.lock` bis zum Schließen, und `maintenance` nimmt dasselbe Lock exklusiv (Polling bis `--wait`, sonst `ErrWritersActive`); `migrate` nimmt es auf demselben Weg. Weil `flock` Schreiber nicht nachrangig behandelt und laufend neu öffnende Hooks die Wartung aushungern könnten, kündigt sie sich vorher mit einem exklusiven `flock` auf `locks/maintenance.pending` an, das sie bis zum Ende hält; ein Schreiber nimmt sein geteiltes Lock nur, wenn er die Ankündigung frei vorfindet. Sobald die Wartung wartet, startet also kein neuer Schreiber mehr, sie muss nur die schon offenen überdauern. Ein Schreiber, der auf die Ankündigung oder das exklusive Lock trifft, wartet nicht, sondern scheitert sofort mit `ErrMaintenance`; im Hook-Modus ist das eine `INFO`-Zeile und Exit 0, der nächste Hook liest ab dem gespeicherten Offset weiter. Lesende Befehle brauchen kein Lock. Unter dem Lock: `PRAGMA wal_checkpoint(TRUNCATE)` (Abbruch mit `ErrReadersActive`, wenn Leser es verhindern, sonst würde die alte `-wal` zur neuen Datei gepaart), Kopie bauen, `PRAGMA integrity_check` und Zeilenzahlen vergleichen, `-wal`/`-shm` entfernen, `rename`. Vorher: Platzprüfung (etwa 2× Dateigröße neben der DB, halbe im Backup-Ordner, `syscall.Statfs`), Backup wie `backup`, Reste `archive.db.maint-*` früherer Läufe entfernen. Jeder Fehler vor dem `rename` lässt das Original unverändert und löscht die Kopie (auch bei `SIGINT`, der Befehl bricht über den Kontext ab). `--dry-run` baut und misst die Kopie ohne Lock und Backup. `status` zeigt `page_size` und einen Hinweis unter 16384.

Hooks für Claude Code (`total-recall hooks print --harness claude` gibt diesen Block aus, eingetragen wird er manuell bzw. über die dotfiles):

| Event | Befehl |
|---|---|
| `Stop`, `SubagentStop` | `total-recall ingest --hook --detach` |
| `SessionStart` | `total-recall ingest --hook --detach` (liest zusätzlich alles nach) |
| `SessionEnd` | `total-recall ingest --hook --detach` und `total-recall backup --if-older 24h --detach` |

## Schema-Versionierung

- Die Tabelle `schema_version` hält die Version der Datenbank. Jede Migration ist ein SQL-Block in `internal/store/schema.go`, Version = Position in der Liste. Daneben, parallel und ebenfalls nur anhängend: `migrationNotes` (je Migration eine Zeile Beschreibung und der zusätzliche Platzbedarf während der Migration als Faktor der Dateigröße; ein Test prüft, dass beide Listen gleich lang sind) und `migrationSteps` (der Go-Teil einer Migration, die nicht mit SQL allein auskommt, etwa weil sie alle Events umschreibt; er läuft in derselben Transaktion nach dem SQL-Block, vor dem Setzen von `schema_version`, und meldet seinen Fortschritt über `report(done, total)`).
- **Migrationen sind vorwärts-only. Eine bestehende Migration wird nie geändert, neue werden nur angehängt.**
- **Kein Befehl migriert von selbst.** `store.Open` legt eine neue, leere DB direkt in der neuesten Version an; eine bestehende DB mit älterem Schema lehnt es mit `ErrSchemaOutdated` ab, eine mit neuerem wie bisher mit `ErrSchemaTooNew`. Grund: Eine Migration kann alle Events umschreiben und Minuten dauern; so etwas darf nicht unbemerkt in einem Hook passieren. Der Nutzer sichert vorher und entscheidet, wann.
- `migrate [--dry-run] [--yes] [--wait DAUER] [--json]` (`store.PlanMigration`, `store.Migrate`): zeigt Version der DB und des Programms, je ausstehender Migration ihre Beschreibung, den Pfad der Kopie und den nötigen freien Platz (Kopie + größter Platzfaktor × Dateigröße + ein Viertel Reserve); fragt nach Bestätigung (`--yes` für Skripte, `--dry-run` zeigt nur den Plan). Dann: Platzprüfung (`ErrDiskSpace`), exklusives Lock wie `maintenance` (`locks/maintenance.pending`, dann `maintenance.lock`; wartet bis `--wait` auf schon offene Schreiber, sonst `ErrWritersActive`; Hooks, die währenddessen starten, überspringen ihren Lauf), Öffnen mit `store.AllowOutdated()`, Kopie `<db>.pre-v<N>.bak` per `VACUUM INTO` neben die DB (N = alte Version), dann jede Migration in eigener Transaktion mit `wal_checkpoint(TRUNCATE)` danach, damit die WAL einer großen Migration nicht stehen bleibt. Ein Fehler lässt die DB auf der letzten vollständigen Version, die Kopie bleibt liegen. Fortschritt kommt über `MigrateOptions.Progress` (`store.Progress{Version, Done, Total}`); die CLI gibt ihn höchstens alle zwei Sekunden und am Ende aus.
- **Veraltete DB (älter als das Programm):**
  - Schreibende Befehle (`ingest`, `reindex`, `maintenance`) scheitern mit `ErrSchemaOutdated`; die Meldung nennt den Weg: `total-recall backup`, dann `total-recall migrate`. Geschrieben wird nichts.
  - Im Hook-Modus ist das kein Fehler: eine `WARN`-Zeile im Log, höchstens einmal je Meldung und Tag (Marker `locks/schema-warned`), Exit 0, Lesestand (`sources.offset`) unverändert. Der erste Hook nach der Migration liest nach (`SessionStart` liest alles), es geht nichts verloren.
  - `backup` öffnet mit `store.AllowOutdated()` und funktioniert ohne Migration: Es ist der empfohlene Schritt davor.
  - Lesende Befehle öffnen read-only, warnen auf stderr (`store.SchemaWarning`) und laufen, soweit sie keine neuen Spalten brauchen; scheitert eine Abfrage an einer fehlenden Spalte oder Tabelle, trägt der Fehler dieselbe Anleitung. `status` zeigt `MIGRATION PENDING` in der Zeile `schema` und `migration_pending` im JSON, `doctor` meldet die ausstehende Migration als Befund (Exit 2), die TUI zeigt in der Kopfzeile `migration pending: total-recall migrate` statt der Ladezeit und bemerkt die Migration beim nächsten `Poll` (`store.MigrationPending`).
- Ist die DB neuer als das Programm: `ErrSchemaTooNew`, auch lesend; im Hook-Modus geloggt (`ERROR`), Exit 0, nichts geschrieben.
- `TestMigrateFromEveryVersion` baut die DB in jeder historischen Version (`store.CreateAtVersion`, auch für Tests anderer Pakete), füllt sie, prüft, dass `Open` sie ablehnt, und migriert mit `Migrate` auf den neuesten Stand. Neue Migrationen sind damit automatisch gegen alle älteren Stände getestet; wer Daten anfasst, ergänzt dort Beispielzeilen. Weitere Tests: kein Automigrieren durch `ingest`, Hook, `backup`, `maintenance` und `reindex`; ein Hook auf einer veralteten DB verliert nichts; `--dry-run` und ein Abbruch ändern nichts.
- Eine Migration ist ein SQL-Block (`migrations`), dazu je Version ein Eintrag `migrationNotes` (`Summary`: was sie tut, bei langen Migrationen die Dauer; `Space`: zusätzlicher Platz als Faktor der Dateigröße, 0 für ALTER TABLE, 1 oder mehr, wenn jede Zeile neu geschrieben wird) und optional ein Go-Schritt `migrationSteps[v]` (`func(ctx, tx, report)`, gebaut aus dem migrierenden Store, damit er dessen Optionen kennt), der in derselben Transaktion nach dem SQL und vor dem Setzen von `schema_version` läuft und seinen Fortschritt über `report(done, total)` meldet. Bricht er ab, bleibt die DB auf der alten Version.
- **Migration 5** (`raw` als Auszug und zstd-BLOB, `source_id`): Das SQL legt `sources` mit `id` neu an (Pfad → ID, fehlende Pfade aus `events.source_path` bekommen eine Zeile) und die Tabelle `events_v5`; der Go-Schritt (`migrateRawV5`) kopiert die Events in Chargen von 1000 unter Beibehaltung von `seq`, entschlackt `raw` mit dem `RawTrimmer` der Harness (`MigrateOptions.RawTrimmers`, `store.WithRawTrimmers`; `migrate` füllt sie aus den Adaptern, eine Harness ohne Trimmer wird nur komprimiert), komprimiert, bildet `source_path` auf `source_id` ab, tauscht die Tabellen (die Views werden dafür aus `sqlite_master` gesichert, gelöscht und wieder angelegt, weil SQLite eine Tabelle nicht umbenennt, solange eine View eine fehlende nennt), legt Indizes und FTS-Trigger neu an (`eventsIndexSQL`) und lässt FTS5 den Index gegen die neue Tabelle prüfen (`integrity-check`); weil `seq` die `rowid` des externen Volltextindex ist, muss der nicht neu gebaut werden. `Space` ist 1,5: Die WAL wächst während der Migration auf etwa das Anderthalbfache der Datei, mit Kopie und Reserve verlangt `migrate` damit rund das Dreifache. Auf einer Kopie eines echten Archivs (231 681 Events, 1,1 GiB): 14 s; die Datei ist danach bis zu `maintenance` größer als vorher (die alten Seiten stehen auf der Freiliste), danach mit 16-KiB-Seiten 435 MiB.

## CLI

| Befehl | Zweck |
|---|---|
| `ingest` | siehe oben |
| `reindex --actors` | Actor jeder gespeicherten User-Nachricht mit den aktuellen Regeln neu bestimmen (`harness.Reclassifier`, `ingest.Reclassify`), ohne die Transkripte zu lesen; idempotent, `--dry-run` zählt nur |
| `status` | DB-Pfad, Größe, Seitengröße (mit Hinweis auf `maintenance` unter 16384), Anzahl Sessions/Events, Größe von `raw` (komprimiert, Anzahl Events mit und ohne `raw`, Kompressionsverhältnis aus den Frame-Headern einer Stichprobe von 2000 Zeilen; `store.RawStats`), letzter Ingest je Harness, Fehler aus dem Log der letzten 24 h (ältere nur als Anzahl, `--all-errors`; fehlende Transkripte zählen nie), letztes Backup |
| `sessions` | Liste; Filter `--since`, `--until`, `--repo`, `--harness`, `--name`, `--account` (Name aus `[accounts]`, Account-ID oder Präfix), `--limit`, `--subagents` (sonst nur Haupt-Sessions), `--all-sessions` (sonst ohne Sessions ohne Events und Startzeit); Spalten Start, Dauer, ID-Präfix, Name, Repo, Branch, #human, #agent; JSON zusätzlich `continued_in`, `continued_from`, `account_id`, `organization_id`, `account` (Name), jeweils `null` ohne Wert |
| `show SESSION` | Verlauf einer Session (Anfragen/Antworten, `--all` inkl. Tools); Kopf mit `parent`, `continued from`/`continued in`, `account`/`organization` (Name aus `[accounts]`), soweit bekannt; Fenster über `--since`/`--until` (Semantik und Zeitformate wie `search`, lokale Zeit), `--limit`/`--offset` zum Blättern, `--from-event ID` ab einem Event (z. B. Treffer aus `search`); ohne Grenzen vollständig, ab 2000 Events mit Hinweis auf stderr |
| `search QUERY` | FTS5-Suche mit Snippets; Filter wie `sessions`, `--actor`, `--session ID` (eindeutiges Präfix erlaubt, mehrdeutig = Fehler), `--raw` (FTS5-Syntax, sonst werden die Wörter quotiert). Mit `--by-session` eine Zeile je Session (Subagenten eingeschlossen) aus `store.SearchBySession`: `first_hit`, `last_hit`, `hits`, `snippet` des ersten Treffers; sortiert nach `first_hit` (Zeitleiste, älteste zuerst) oder mit `--order hits` nach Trefferzahl; dieselben Filter, `--limit` zählt Sessions. Das ist die Kernabfrage 1 des Skills, vorher eine SQL-Abfrage |
| `stats` | Kennzahlen für einen Zeitraum (`--month YYYY-MM` oder `--since/--until`): je Tag, je Wochentag×Stunde (lokale Zeit), je Repo: Sessions, human-, agent-Events. Mit `--period day\|week\|month`, `--by project\|agent\|directory\|account` (`account`: `sessions.account_id`, Subagenten über die Eltern-Session, ohne Account `unknown`, Namen aus `[accounts]`), `--top N` stattdessen eine Zeitreihe Periode × Schlüssel aus `store.ActivitySeries` (Text im Langformat, JSON) samt Summen über den Zeitraum, in denen jede Session einmal zählt (in den Perioden zählt sie je Periode mit Aktivität), Ersatz für `monthly_stats.py` im Vault; Projektnamen aus `internal/project`. Mit `--workday` je Tag die erste und letzte Aktivität und die Spanne (`store.Workday`), Zeitraum `--range today\|this-week\|last-week\|this-month\|last-month` oder `--since/--until`/`--month`; gezählt werden eigene Nachrichten (`[work] count`, `--all-events` für alle; `--human-only` veraltet, erzwingt eigene Nachrichten auch bei `count = "all"`); ein Tag beginnt um `day_start` (Default 04:00 Ortszeit), Aktivität davor zählt zum Vortag (`01:30 (+1)`). Je Tag die Differenz der Spanne zum Tagessoll aus `[work]` (`store.WorkHours`: Soll an Arbeitstagen mit Aktivität, Wochenende und Feiertage aus `internal/holiday` ohne Soll, Spalte `HOLIDAY`), in der Summe Soll × Arbeitstage des Zeitraums (Mo–Fr ohne Feiertage, bis heute, mit oder ohne Aktivität) und Saldo; JSON in Sekunden. Dieselben Daten zeigt die TUI im Reiter `activity`, dort mit der Kernzeit als Band auf der Zeitachse, Balkensegmenten innerhalb/außerhalb/nach Mitternacht, Farben nach Bewertung gegen `daily_low`/`daily_high`, einer Kopfzeile je Woche/Monat (Arbeitszeit gegen `weekly_low`/`weekly_high` anteilig nach Arbeitstagen, Soll, Saldo, Feiertage) und Kennzahlen (`store.Summarize`: Median, Mittel, Min/Max von Beginn, Ende, Spanne). Mit `--worktime` je Tag und Kategorie die geschätzte Arbeitszeit, roh und gerundet (`store.HumanEvents` liefert die eigenen Nachrichten mit Session-Titel und Projekt, `internal/worktime` rechnet): die Lücke zwischen zwei aufeinanderfolgenden eigenen Nachrichten über alle Sessions zählt für das Projekt der späteren, voll bis `max_gap`, sonst `min_credit`; Projekte werden über die Glob-Regeln `[[worktime.category]]` Kategorien zugeordnet (erste passende Regel; ohne Regel heißt die Kategorie wie das Projekt); die Summe je Kategorie und Tag (oder je Projekt, `scope`) wird auf `grid` gerundet (`rounding`). Zeitraum wie `--workday`, `--sessions` listet je Zelle Projekte und Sessions mit Titel. `--summary` ergänzt je Zelle die Issue-/PR-Referenzen (`#31`, `PR #32`, `owner/repo#123`, auch aus Issue-/Pull-URLs; `worktime.FindRefs`) aus den Session-Titeln und den eigenen Nachrichten (`store.HumanEvents` liefert den Text; nie aus Agent- oder Tool-Ausgaben), je Session, Projekt und Zelle dedupliziert und sortiert, und einen Buchungstext `<projekt>: <titel> (<refs>), …; <projekt>: …` (`worktime.Summary`), gekürzt auf `summary_max_chars`; JSON enthält `refs` und `text` immer. Meetings und Arbeit ohne Agent fehlen: eine Untergrenze. |
| `tui [search\|stats\|activity]` | Terminal-Oberfläche, nur lesend (`store.OpenReadOnly`): Suche mit Vorschau und Session-Detail (Kopf mit `continued from`/`continued in`), Statistik aus `store.ActivitySeries` und `store.Stats` (Textchart, Kitty Graphics mit Rückfall, Tabelle, Heatmap), Aktivität je Tag aus `store.Workday`. `--query`, `--graphics auto\|text\|kitty`, `--no-mouse`, `--reload <dauer>`; Defaults aus `[tui]`, Tastenbelegung aus `[keys]`. Neuladen: alle `reload` (Default 5 s, `0` = aus, mindestens 1 s) fragt die App `store.Poll` (ein `PRAGMA data_version` auf einer dafür festgehaltenen Verbindung plus `stat` der Datei; keine Lesetransaktion zwischen den Prüfungen, keine weitere Abfrage ohne Änderung) und lädt bei einer Änderung nur die sichtbare Ansicht neu (`View.Reload`: gleiche Abfrage, gleiches Fenster, gleiche Session; Markierung, Scrollposition, Fokus und Eingabe bleiben, eine Session am Ende läuft mit), die anderen gelten als veraltet und laden beim nächsten Anzeigen; eine ausgetauschte Datei (`maintenance`) meldet `Poll` als `Replaced`, `store.Reopen` öffnet sie neu und nimmt sofort die neue Basis. Die Kopfzeile zeigt `updated HH:MM:SS`, `R` (`global_reload`) lädt sofort. Bedienung in [TUI](../reference/30-tui.md), Entwurf und Messwerte in [Spike: TUI](spike-tui.md). |
| `sql QUERY` | Read-only-SQL (DB mit `mode=ro` und `PRAGMA query_only`), Ausgabe Tabelle/JSON/JSONL/CSV; `raw_text(raw)` entpackt die Rohzeile |
| `backup` | `VACUUM INTO` + gzip + Aufbewahrung; `--if-older DURATION` |
| `restore BACKUP_FILE` | Backup (`.db.gz` oder alte `.db`) in eine DB-Datei entpacken; `--to PATH`, `--force` |
| `maintenance` | Datei mit neuer Seitengröße (`--page-size`, Default 16384) auf einer Kopie neu aufbauen, prüfen und atomar austauschen; Backup vorher, Lock gegen Hooks, `--dry-run` misst nur, `--wait`, `--json`; siehe „Wartung und Dateitausch“ oben |
| `migrate` | Schema der DB auf den Stand des Programms bringen, der einzige Weg dahin: Plan (Versionen, ausstehende Migrationen, Kopie, Platz), Bestätigung oder `--yes`, `--dry-run` zeigt nur, `--wait`, `--json`; Platzprüfung, exklusives Lock wie `maintenance`, Kopie `<db>.pre-v<N>.bak`, Fortschritt; siehe „Schema-Versionierung“ oben |
| `hooks print --harness claude` | Hook-Konfiguration als JSON |
| `doctor [--harness claude] [--json]` | Format-Drift prüfen, nur lesend, ohne DB: unbekannte Record-Typen (weder eingelesen noch bekannt übersprungen), unbekannte Content-Block-Typen, fehlende erwartete Felder (`timestamp`, `sessionId`, `uuid`, `cwd` je Typ), kaputte Zeilen, gesehene Harness-Versionen (neueste zuerst). Exit 0 = sauber, 2 = Befunde. Die bekannten Listen exportiert der Adapter; Parser und Prüfung teilen sie. |
| `llm --skill` | gibt den eingebetteten `SKILL.md` aus (Schema, Befehle, Beispiel-SQL), damit Agenten die Daten auswerten können |
| `config show` / `config init` | wirksame Konfiguration / Vorlage schreiben |
| `version` | Version ausgeben; `--version`/`-v` auf dem Hauptbefehl gibt dieselbe Zeile aus |

Globale Flags: `--config`, `--db`, `--tz` (Default `Europe/Berlin` für Auswertungen), `--backup-dir`. `backup.min_interval` ist derzeit ungenutzt; `--if-older` braucht eine ausdrückliche Dauer.

### Ausgabevertrag der Lesebefehle

Gilt für `status`, `sessions`, `show`, `search`, `stats`, `sql`, `doctor` und `config show`; umgesetzt in `internal/cli/output.go` (`outputFlags`, `output`, `NullString`, `ErrorJSON`), das jeder Lesebefehl über `env.readRun` benutzt.

- **Formate.** Jeder Lesebefehl hat `--format table|json|jsonl` (`sql` zusätzlich `csv`), `--json` ist die Kurzform für `--format json` und gewinnt. `table` ist der Standard und meint die lesbare Form (Tabelle oder Text). `json` ist ein eingerücktes Dokument, `jsonl` ein kompaktes Objekt je Zeile, ungepuffert geschrieben; ein Einzelobjekt (`status`, `stats`) ist dann eine Zeile, `show` schreibt nur die Events (den Session-Kopf liefert `sessions`). Ein leeres Ergebnis ist bei `json` `[]`, bei `jsonl` keine Zeile.
- **Fehler.** Bei `json` oder `jsonl` geht ein Fehler als ein Objekt auf stdout: `{"error":{"kind":"<art>","message":"<meldung>"}}`. Der Exit-Code bleibt 1, die Meldung steht zusätzlich auf stderr. `kind` ist eine stabile Menge: `usage` (Flags, Argumente, ihre Kombinationen), `config`, `db` (Archiv nicht zu öffnen), `sql` (SQLite lehnt die Abfrage ab, auch `search --raw`), `not_found`, `ambiguous` (Session-Referenz), `internal` (alles andere). Das gilt auch für Fehler vor dem Befehl selbst (unbekanntes Flag), erkannt an `--json` bzw. `--format json|jsonl` in den Argumenten.
- **Schlüssel.** Jeder dokumentierte Schlüssel ist immer vorhanden. Ein String ohne Wert ist `null` (Typ `NullString`), Zahlen sind nie `null` (0), Listen und Maps nie `null` (`[]`, `{}`). Die Ausgabetypen haben kein `omitempty`; Typen in `store` und `harness` dürfen eigene Tags behalten, `doctor --json` gibt `harness.FormatReport` unverändert aus.
- **Kürzung.** `--format table` kürzt Zellen nur, wenn stdout ein Terminal ist: die Spalten werden auf die Terminalbreite eingepasst (die breitesten geben zuerst ab, keine unter 4 Zeichen), eine gekürzte Zelle endet mit `…`, und unter der Ausgabe steht auf stderr `N cells truncated, --no-truncate for full values`. In eine Pipe oder Datei wird nie gekürzt, `--no-truncate` erzwingt volle Zellen auch im Terminal. Zellen mit Zeilenumbrüchen oder Tabs werden in jedem Fall auf eine Zeile gezogen (Leerraum zu einem Leerzeichen), damit eine Zeile eine Tabellenzeile bleibt; `csv` und JSON behalten den Text unverändert. Betroffen sind die Tabellen von `sessions`, `sql`, `status`, `config show` sowie Name und Textausschnitt von `search`; die Zahlentabellen von `stats` werden nicht gekürzt.

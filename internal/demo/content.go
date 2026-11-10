package demo

import (
	"time"

	"github.com/seletz/total-recall/internal/testfixture"
)

// project is one invented project of the fictional developer with the
// vocabulary its conversations are made of. The filler text never mentions
// the words of Query, so the search screenshot shows the conversations
// written out below.
type project struct {
	key    string
	cwd    string
	root   string // git root; empty means no repository
	remote string
	branch string
	weight int // share of the sessions
	titles []string
	// prompts are what the developer types, replies what the agent answers;
	// both are drawn at random, so the conversations are plausible but not
	// coherent.
	prompts, replies []string
	tools            []tool
}

const remoteBase = "https://git.invalid/mara/"

// The projects: a route-planning CLI (the main one, also in a worktree), a
// weather-station dashboard, a trip log, dotfiles and a notes directory
// without a repository.
var (
	kompass = &project{
		key: "kompass", cwd: Home + "/src/kompass", root: Home + "/src/kompass", remote: remoteBase + "kompass", branch: "main", weight: 38,
		titles: []string{
			"Höhenprofil der Route", "Flaky route cache test", "GPX-Export", "Tiles-Cache nach Neustart leer",
			"Wegpunkte stabil sortieren", "Umlaute in Ortsnamen", "Benchmarks für den Routing-Kern",
			"Doku der Befehle", "Neue Lint-Regeln (#40)", "Kartenkacheln offline", "Rennrad-Profil", "Streaming-Parser",
		},
		prompts: []string{
			"Füge einen Test für die Wegpunkt-Sortierung hinzu, siehe #31.",
			"Warum ist der Tiles-Cache nach einem Neustart leer?",
			"Schreib die Doku für den Befehl `kompass route`.",
			"the elevation profile is off by a few metres on long climbs, have a look",
			"Benenne das Paket route/cache in route/tiles um.",
			"Mach die Ausgabe von `kompass plan` auch als JSON verfügbar.",
			"Die Umlaute in Ortsnamen kommen kaputt an, wenn die Datei Latin-1 ist (#48).",
			"add a benchmark for the routing core (PR #52) and tell me where the time goes",
			"Der Offline-Modus soll die letzten 200 Kacheln behalten.",
			"Zeig mir, wo die Steigung berechnet wird.",
		},
		replies: []string{
			"Done. The sort is stable now and the test covers duplicate waypoints.",
			"Der Cache wurde beim Start mit einem leeren Verzeichnis überschrieben; ich lese ihn jetzt vor dem Anlegen ein.",
			"Ich habe die Doku ergänzt und die Beispiele gegen die aktuelle Ausgabe geprüft.",
			"The profile used the raw GPS altitude; it now takes the smoothed series, which removes the offset.",
			"Umbenannt, Importe angepasst, Tests laufen.",
			"`kompass plan --json` gibt jetzt die Route als Objekt mit Wegpunkten und Abschnitten aus.",
			"Die Datei wird nun über den erkannten Zeichensatz gelesen; ein Test mit Latin-1 liegt bei.",
			"Most of the time is spent in the heap of the shortest-path search; the benchmark is in route/bench_test.go.",
			"Die Steigung steht in route/grade.go; sie mittelt über drei Punkte.",
		},
		tools: []tool{
			{"Bash", "go test ./...", "ok  \tkompass/route\t0.812s\nok  \tkompass/internal/gpx\t0.144s\n"},
			{"Read", "route/cache.go", "package route\n\n// Cache keeps the tiles of the last routes.\n"},
			{"Edit", "route/sort.go", "The file has been updated.\n"},
			{"Grep", "Waypoint", "route/sort.go:12\nroute/plan.go:48\ninternal/gpx/parse.go:31\n"},
			{"Bash", "golangci-lint run", "0 issues.\n"},
			{"Bash", "git commit -m 'fix(route): stable waypoint sort'", "[main 3f2a1c9] fix(route): stable waypoint sort\n"},
			{"Bash", "go build ./... && ./kompass plan --from Freiburg --to Basel", "route: 72.4 km, 540 m climb\n"},
		},
	}
	kompassWorktree = &project{
		key: "kompass-gpx", cwd: Home + "/src/kompass__worktrees/gpx", root: Home + "/src/kompass", remote: remoteBase + "kompass", branch: "gpx", weight: 6,
		titles:  []string{"GPX-Import", "Tracks mit Zeitstempeln", "GPX-Fixtures", "Streaming-Parser für große Dateien"},
		prompts: kompass.prompts, replies: kompass.replies, tools: kompass.tools,
	}
	wetterwarte = &project{
		key: "wetterwarte", cwd: Home + "/src/wetterwarte", root: Home + "/src/wetterwarte", remote: remoteBase + "wetterwarte", branch: "main", weight: 24,
		titles: []string{
			"Sensorwerte glätten", "Dashboard-Layout", "MQTT reconnect", "Wochenchart", "Taupunkt-Berechnung",
			"Frostwarnung", "CSV-Export", "Zeitzone im Chart", "Regenmesser kalibrieren", "Lücken in den Messreihen",
		},
		prompts: []string{
			"Die Temperaturkurve zappelt, glätte die Sensorwerte über fünf Minuten.",
			"the MQTT client never reconnects after the broker restarts, see mara/wetterwarte#238",
			"Baue ein Wochenchart mit Minimum und Maximum je Tag.",
			"Rechne den Taupunkt aus Temperatur und Luftfeuchte und zeig ihn im Dashboard.",
			"Schick eine Warnung, wenn die Temperatur in der Nacht unter zwei Grad fällt.",
			"Exportiere die Messwerte eines Monats als CSV.",
			"Das Chart zeigt UTC statt Ortszeit, #239.",
			"Warum fehlen in der Nacht Messwerte?",
			"the rain gauge counts twice per tip, check the debounce",
		},
		replies: []string{
			"Die Werte laufen jetzt durch ein gleitendes Mittel über fünf Minuten; die Kurve ist ruhig.",
			"The client now reconnects with backoff and resubscribes; I added a test with a restarting broker.",
			"Das Wochenchart steht; Minimum und Maximum je Tag kommen aus einer eigenen Abfrage.",
			"Der Taupunkt wird nach der Magnus-Formel berechnet und als eigene Kachel gezeigt.",
			"Die Frostwarnung geht per E-Mail raus, höchstens einmal je Nacht.",
			"`wetterwarte export --month 2026-09` schreibt die CSV-Datei mit einer Zeile je Messung.",
			"Die Achse nimmt jetzt die konfigurierte Zeitzone; der Test prüft den Übergang zur Winterzeit.",
			"Der Sensor geht bei niedriger Spannung in den Schlafmodus; ich logge das jetzt, statt stumm zu bleiben.",
			"Debounce raised to 50 ms; the gauge counts one tip now.",
		},
		tools: []tool{
			{"Bash", "go test ./...", "ok  \twetterwarte/sensor\t0.231s\nok  \twetterwarte/web\t1.044s\n"},
			{"Read", "sensor/smooth.go", "package sensor\n\n// Smooth averages readings over a window.\n"},
			{"Edit", "web/chart.go", "The file has been updated.\n"},
			{"Grep", "Reconnect", "mqtt/client.go:88\nmqtt/client_test.go:40\n"},
			{"Bash", "curl -s localhost:8080/api/latest", "{\"temp\":12.4,\"humidity\":81,\"dewpoint\":9.2}\n"},
			{"Write", "sensor/dewpoint_test.go", "File created.\n"},
		},
	}
	fahrtenbuch = &project{
		key: "fahrtenbuch", cwd: Home + "/src/fahrtenbuch", root: Home + "/src/fahrtenbuch", remote: remoteBase + "fahrtenbuch", branch: "main", weight: 14,
		titles: []string{
			"Import aus der Tacho-App", "Kilometerstand prüfen", "PDF-Export", "Monatsübersicht", "Fahrten zusammenfassen",
			"Adressen vervollständigen", "Datenbank-Migration", "Backup der Fahrten",
		},
		prompts: []string{
			"Importiere die Fahrten aus dem CSV-Export der Tacho-App.",
			"Der Kilometerstand darf nie kleiner werden als bei der letzten Fahrt, prüf das beim Speichern.",
			"Erzeuge eine Monatsübersicht als PDF.",
			"Fasse Fahrten mit weniger als fünf Minuten Pause zu einer zusammen.",
			"add a migration for the new column purpose (#17) and keep the old rows",
			"Wie viele Fahrten hat der September?",
		},
		replies: []string{
			"Der Import liest die CSV-Datei und legt je Zeile eine Fahrt an; doppelte Fahrten werden übersprungen.",
			"Die Prüfung sitzt im Speichern und lehnt einen kleineren Kilometerstand mit einer klaren Meldung ab.",
			"Das PDF listet die Fahrten des Monats mit Summe je Zweck.",
			"Fahrten mit kurzer Pause werden beim Import zusammengefasst; die Einzelfahrten bleiben in der Tabelle.",
			"Migration 4 adds the column with a default; the test migrates a filled database.",
			"Der September hat 41 Fahrten mit 1 180 km.",
		},
		tools: []tool{
			{"Bash", "go test ./...", "ok  \tfahrtenbuch/store\t0.388s\n"},
			{"Read", "store/trip.go", "package store\n\n// Trip is one recorded drive.\n"},
			{"Bash", "sqlite3 fahrten.db 'SELECT count(*) FROM trips'", "412\n"},
			{"Edit", "store/import.go", "The file has been updated.\n"},
		},
	}
	dotfiles = &project{
		key: "dotfiles", cwd: Home + "/dotfiles", root: Home + "/dotfiles", remote: remoteBase + "dotfiles", branch: "main", weight: 6,
		titles: []string{"Shell-Aliase aufräumen", "Neovim-Konfiguration", "tmux-Statusleiste", "Git-Aliase", "Prompt beschleunigen"},
		prompts: []string{
			"Räum die Shell-Aliase auf, einige zeigen auf Programme, die es nicht mehr gibt.",
			"Die tmux-Statusleiste soll den Akkustand zeigen.",
			"Warum braucht der Prompt eine halbe Sekunde?",
			"move the git aliases into their own file",
		},
		replies: []string{
			"Sieben Aliase entfernt, die restlichen nach Thema sortiert.",
			"Die Statusleiste zeigt den Akkustand aus `pmset`, aktualisiert jede Minute.",
			"Der Prompt rief bei jedem Zeichen `git status` auf; das passiert jetzt nur noch beim Verzeichniswechsel.",
			"The aliases live in git/aliases now and are included from the main config.",
		},
		tools: []tool{
			{"Read", "zsh/aliases.zsh", "alias ll='ls -la'\nalias gs='git status'\n"},
			{"Edit", "tmux/tmux.conf", "The file has been updated.\n"},
			{"Bash", "zsh -ic 'time (exit)'", "0.48s user 0.12s system\n"},
		},
	}
	notizen = &project{
		key: "notizen", cwd: Home + "/notizen", weight: 7,
		titles: []string{"Wochenrückblick", "Lesenotizen sortieren", "Ideen für den Winter", "Artikel zusammenfassen", "Packliste"},
		prompts: []string{
			"Fasse die Notizen dieser Woche in fünf Punkten zusammen.",
			"Sortiere die Lesenotizen nach Thema und lege je Thema eine Datei an.",
			"Welche Ideen aus dem Ordner ideen/ sind noch offen?",
			"Mach aus der Packliste eine Checkliste mit Kästchen.",
		},
		replies: []string{
			"Die Woche in fünf Punkten steht in wochen/2026-W23.md.",
			"Die Notizen liegen jetzt in lesen/<thema>.md; die alten Dateien habe ich gelöscht.",
			"Drei Ideen sind offen; ich habe sie in ideen/offen.md gesammelt.",
			"Die Packliste ist eine Checkliste mit `- [ ]` je Zeile.",
		},
		tools: []tool{
			{"Grep", "TODO", "ideen/garten.md:4\nideen/werkstatt.md:9\n"},
			{"Read", "wochen/2026-W22.md", "# Woche 22\n\n- Dashboard fertig\n"},
			{"Write", "wochen/2026-W23.md", "File created.\n"},
		},
	}
)

// projects in the order their weights are drawn.
var projects = []*project{kompass, kompassWorktree, wetterwarte, fahrtenbuch, dotfiles, notizen}

// genericPrompts are prompts of any project.
var genericPrompts = []string{
	"run the tests and fix what fails",
	"lint and vet, then commit",
	"Was hast du geändert? Kurz bitte.",
	"commit it",
	"Mach weiter.",
	"Schau dir den Fehler an.",
	"ok, and now the docs",
}

// agentKind is a type of subagent with what it is asked and what it reports.
type agentKind struct {
	name             string
	prompts, replies []string
}

var agentKinds = []agentKind{
	{"explore", []string{
		"Research task: find where the configuration is loaded and list the callers.",
		"Finde alle Stellen, an denen die Zeitzone gesetzt wird.",
	}, []string{
		"The configuration is loaded in config/load.go; three callers, all in cmd/.",
		"Die Zeitzone wird an zwei Stellen gesetzt: beim Start und im Export.",
	}},
	{"reviewer", []string{
		"Review the diff of the current branch for bugs and report them.",
		"Prüfe die Änderungen auf Fehler bei der Rundung.",
	}, []string{
		"One finding: the loop skips the last element when the slice is empty. Everything else looks fine.",
		"Keine Fehler bei der Rundung; eine Variable ist ungenutzt.",
	}},
	{"tests", []string{
		"Write table-driven tests for the parser and run them.",
		"Schreib Tests für die Zusammenfassung der Fahrten.",
	}, []string{
		"Added six cases; all pass.",
		"Vier Fälle, alle grün.",
	}},
	{"docs", []string{
		"Update the command reference for the new flag.",
		"Ergänze die Doku um das neue Chart.",
	}, []string{
		"The reference lists the flag with an example.",
		"Die Doku beschreibt das Chart mit einem Beispiel.",
	}},
}

// at returns an instant of the conversations below.
func at(month time.Month, day, hour, min int) time.Time {
	return time.Date(2026, month, day, hour, min, 0, 0, Location)
}

// featured are the conversations written out by hand: invented developer
// chat about the release of kompass in German and English; "Packaging" was
// continued in "Homebrew-Tap einrichten" and "Signierte Releases" in
// "Release v0.3.0", the session of the screenshot. Several mention
// goreleaser and the Homebrew cask, the Query of the search screenshot.
func featured() []session {
	return []session{
		{
			id: "7d1e4c20-0001-4000-8000-000000000001", cwd: kompass.cwd, title: "Release v0.3.0",
			turns: []turn{
				{at: at(time.October, 31, 22, 14),
					prompt: "Lade den Homebrew-Cask mit goreleaser in den Tap hoch.",
					tools: []tool{{"Read", ".goreleaser.yml",
						"casks:\n  - name: kompass\n    repository:\n      owner: mara\n      name: homebrew-tap\n"}},
					reply:   "goreleaser skips the cask upload when the tap token is missing. I added a check that fails the release early instead, so a missing HOMEBREW_TAP_GITHUB_TOKEN no longer leaves a half-published release.",
					replyAt: 2 * time.Minute},
				{at: at(time.October, 31, 22, 17),
					prompt: "Gut. Tagge v0.3.0 und starte das Release.", remote: true,
					tools: []tool{{"Bash", "git tag -s v0.3.0 -m 'v0.3.0' && git push origin v0.3.0",
						"To git.invalid:mara/kompass\n * [new tag]         v0.3.0 -> v0.3.0\n"}},
					reply:   "Tagged v0.3.0 and pushed the tag. The release workflow is running; goreleaser builds the binaries and publishes the cask to the tap.",
					replyAt: 3 * time.Minute},
			},
		},
		{
			id: "7d1e4c20-0002-4000-8000-000000000002", cwd: kompass.cwd, ai: "CI pipeline",
			turns: []turn{
				{at: at(time.October, 28, 9, 2),
					prompt: "can we build the cask in goreleaser and push it to the tap from CI?",
					tools: []tool{{"Bash", "brew info --cask --json=v2 mara/tap/kompass",
						`{"casks":[{"token":"kompass","tap":"mara/tap","version":"0.2.1","desc":"Route planner","homepage":"https://git.invalid/mara/kompass","url":"https://git.invalid/mara/kompass/releases/download/v0.2.1/kompass_0.2.1_darwin_arm64.tar.gz","note":"cask generated by goreleaser","artifacts":[{"binary":["kompass"]}]}]}` + "\n"}},
					reply:   "goreleaser needs HOMEBREW_TAP_GITHUB_TOKEN in the workflow secrets and a cask entry in .goreleaser.yml. I added both; the cask is published on every tag.",
					replyAt: 3 * time.Minute},
				{at: at(time.October, 28, 9, 12),
					prompt: "Und der Lint-Job? Der schlägt seit gestern fehl.\n<pasted_content id=\"1\">route/cache.go:42:18: unused-parameter: parameter 'ctx' seems to be unused (revive)</pasted_content id=\"1\">",
					tools: []tool{{"Bash", "golangci-lint run ./...",
						"route/cache.go:42:18: unused-parameter: parameter 'ctx' seems to be unused (revive)\n"}},
					reply:   "The lint job fails on a new golangci-lint rule: an unused parameter in route/cache.go. I removed the parameter and the job is green again.",
					replyAt: 4 * time.Minute},
			},
		},
		{
			id: "7d1e4c20-0003-4000-8000-000000000003", cwd: kompass.cwd, ai: "Packaging",
			continuedIn: "7d1e4c20-0005-4000-8000-000000000005",
			turns: []turn{
				{at: at(time.October, 12, 15, 40),
					prompt:  "Übernimm die .goreleaser.yml aus dem Notizen-Projekt und passe sie für kompass an.",
					reply:   "copied .goreleaser.yml from the sister project kompass-web, renamed the binary to kompass and set the cask name. goreleaser builds darwin/arm64 and linux/amd64.",
					replyAt: 5 * time.Minute},
			},
		},
		{
			id: "7d1e4c20-0004-4000-8000-000000000004", cwd: kompass.cwd, ai: "Flaky route cache test",
			turns: []turn{
				{at: at(time.October, 20, 10, 15),
					prompt: "TestRouteCache schlägt gelegentlich fehl, bitte schau dir das an.",
					tools: []tool{{"Bash", "go test -run TestRouteCache -count=200 ./route/",
						"--- FAIL: TestRouteCache (0.01s)\n    cache_test.go:57: waypoints out of order\nFAIL\n"}},
					reply:   "The test depended on map iteration order. I sort the waypoints before comparing them; 200 runs pass now.",
					replyAt: 6 * time.Minute},
			},
		},
		{
			id: "7d1e4c20-0005-4000-8000-000000000005", cwd: kompass.cwd, ai: "Homebrew-Tap einrichten",
			turns: []turn{
				{at: at(time.October, 15, 11, 5),
					prompt: "Richte den Homebrew-Tap ein, damit goreleaser den Cask dorthin pusht.",
					tools: []tool{{"Bash", "gh repo create mara/homebrew-tap --public",
						"https://git.invalid/mara/homebrew-tap\n"}},
					reply:   "Created the tap repository and a deploy key. goreleaser publishes the cask there; users install it with brew install --cask mara/tap/kompass.",
					replyAt: 7 * time.Minute},
			},
		},
		{
			id: "7d1e4c20-0006-4000-8000-000000000006", cwd: kompass.cwd, title: "Signierte Releases",
			continuedIn: "7d1e4c20-0001-4000-8000-000000000001",
			turns: []turn{
				{at: at(time.October, 22, 17, 30),
					prompt:  "Können wir die Release-Artefakte mit cosign signieren?",
					reply:   "Yes: goreleaser signs the checksums file with cosign after the build. The cask is unaffected, it still points at the signed archives.",
					replyAt: 4 * time.Minute},
			},
		},
		{
			id: "7d1e4c20-0007-4000-8000-000000000007", cwd: kompassWorktree.cwd, title: "GPX-Import",
			turns: []turn{
				{at: at(time.October, 24, 16, 0),
					prompt: "Füge einen GPX-Import hinzu: Wegpunkte und Tracks, Streaming-Parser.",
					tools: []tool{{"Bash", "go test ./internal/gpx/...",
						"ok  \tkompass/internal/gpx\t0.412s\n"}},
					reply:   "Added internal/gpx with a streaming parser for waypoints and tracks, plus tests with a small synthetic file.",
					replyAt: 9 * time.Minute},
			},
		},
		{
			id: "7d1e4c20-0008-4000-8000-000000000008", cwd: kompass.cwd, title: "Höhenprofil prüfen",
			turns: []turn{
				{at: at(time.October, 28, 9, 6),
					prompt: "Das Höhenprofil kippt nach Kilometer 40, siehe Screenshot und die Notizen zum Format im Anhang.",
					attached: []testfixture.Attached{
						{Kind: "image", MediaType: "image/png", Data: profileImage()},
						{Kind: "document", MediaType: "application/pdf", Data: formatNotes()},
					},
					reply:   "The profile showed the raw altitude of the last segment. I now smooth the series before the climb is summed up; the picture matches the route again.",
					replyAt: 4 * time.Minute},
			},
		},
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// Synthetic transcript lines for the dictionary. The demo archive has
// conversations but few of the shapes a real Claude Code transcript is
// made of: tool calls with their inputs, tool results with the harness's
// toolUseResult object, system records, hook and command wrappers,
// thinking blocks, and on every line the harness's bookkeeping (session,
// cwd, branch, version, request ID, usage with cache tokens). The
// generator below writes those shapes with invented content (the fictional
// developer mara and her projects, see internal/demo), deterministically
// (fixed seed). A dictionary learns the recurring JSON structure and
// vocabulary from them; the content itself never came from a real
// transcript.

// synthLines returns about n whole raw lines in the shapes of Claude Code
// records, sorted and deduplicated.
func synthLines(n int) [][]byte {
	g := &synth{r: rand.New(rand.NewSource(132)), t: time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)}
	seen := map[string]bool{}
	for len(seen) < n {
		for _, l := range g.turn() {
			seen[l] = true
		}
	}
	out := make([]string, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	res := make([][]byte, len(out))
	for i, l := range out {
		res[i] = []byte(l)
	}
	return res
}

type synth struct {
	r       *rand.Rand
	t       time.Time
	parent  string
	session string
	cwd     string
	branch  string
	version string
}

func (g *synth) pick(items ...string) string { return items[g.r.Intn(len(items))] }

func (g *synth) uuid() string {
	b := make([]byte, 16)
	g.r.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (g *synth) toolID() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789"
	b := make([]byte, 22)
	for i := range b {
		b[i] = alphabet[g.r.Intn(len(alphabet))]
	}
	return "toolu_01" + string(b)
}

func (g *synth) stamp() string {
	g.t = g.t.Add(time.Duration(2+g.r.Intn(40)) * time.Second)
	return g.t.Format("2006-01-02T15:04:05.000Z")
}

func (g *synth) requestID() string {
	const alphabet = "0123456789abcdefghijkmnopqrstuvwxyz"
	b := make([]byte, 25)
	for i := range b {
		b[i] = alphabet[g.r.Intn(len(alphabet))]
	}
	return "req_" + string(b)
}

// newSession starts a session: every record of it repeats these fields.
func (g *synth) newSession() {
	g.session = g.uuid()
	g.cwd = g.pick(dirs...)
	g.branch = g.pick("main", "develop", "feat/31-waypoint-sort", "fix/cache-reload", "docs/route-command")
	g.version = g.pick("2.1.300", "2.1.312", "2.1.317", "2.1.320", "2.1.325")
	g.parent = ""
}

// record writes a line in the key order of Claude Code: the parent and
// session bookkeeping first, then the record's own fields, then the
// trailing uuid and timestamp.
func (g *synth) record(typ string, fields ...any) string {
	if g.session == "" || g.r.Intn(60) == 0 {
		g.newSession()
	}
	var b strings.Builder
	b.WriteString(`{"parentUuid":`)
	if g.parent == "" {
		b.WriteString("null")
	} else {
		b.WriteString(`"` + g.parent + `"`)
	}
	b.WriteString(`,"isSidechain":false,"userType":"external","cwd":"` + g.cwd + `","sessionId":"` + g.session + `","version":"` + g.version + `","gitBranch":"` + g.branch + `"`)
	if typ == "assistant" {
		b.WriteString(`,"slug":"` + g.pick("stable-waypoint-sort", "tiles-cache-reload", "gpx-export-segments", "route-docs") + `"`)
	}
	b.WriteString(`,"type":"` + typ + `"`)
	for i := 0; i+1 < len(fields); i += 2 {
		v, _ := marshal(fields[i+1])
		b.WriteString(`,"` + fields[i].(string) + `":` + v)
	}
	if typ == "assistant" {
		b.WriteString(`,"requestId":"` + g.requestID() + `"`)
	}
	id := g.uuid()
	b.WriteString(`,"uuid":"` + id + `","timestamp":"` + g.stamp() + `"}`)
	g.parent = id
	return b.String()
}

// usage is the API usage Claude Code repeats on every assistant line of
// a response.
func (g *synth) usage() obj {
	return obj{"input_tokens": 1 + g.r.Intn(12), "cache_creation_input_tokens": g.r.Intn(4000), "cache_read_input_tokens": 20000 + g.r.Intn(90000),
		"cache_creation": obj{"ephemeral_5m_input_tokens": g.r.Intn(4000), "ephemeral_1h_input_tokens": 0}, "output_tokens": 1 + g.r.Intn(600),
		"service_tier": "standard", "inference_geo": "unknown"}
}

// assistant writes an assistant line with one content block and the
// message envelope (id, role, usage, stop_reason) of the API.
func (g *synth) assistant(model string, block obj) string {
	return g.record("assistant", "message", obj{"id": "msg_01" + g.toolID()[8:], "type": "message", "role": "assistant", "model": model,
		"content": []obj{block}, "stop_reason": nil, "stop_sequence": nil, "usage": g.usage()})
}

// thinking is a short thinking block, as assistant lines carry them.
func (g *synth) thinking() obj {
	return obj{"type": "thinking", "thinking": g.pick(thoughts...), "signature": g.toolID() + g.toolID() + g.toolID()}
}

func marshal(v any) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type obj = map[string]any

var (
	models   = []string{"claude-opus-4-1-20250805", "claude-sonnet-4-5-20250929", "claude-opus-4-5-20251101", "claude-haiku-4-5-20251001"}
	files    = []string{"route/grade.go", "route/cache_test.go", "cmd/kompass/main.go", "internal/tiles/store.go", "docs/reference/befehle.md", "mise.toml", "go.mod", "README.md", "internal/gpx/export.go", "internal/gpx/export_test.go", "station/reader.go", "web/dashboard.go", "trips/log.go", ".config/zsh/aliases.zsh", "notes/2026-09-07.md"}
	dirs     = []string{"/home/mara/src/kompass", "/home/mara/src/kompass__worktrees/gpx", "/home/mara/src/wetterwarte", "/home/mara/src/fahrtenbuch", "/home/mara/dotfiles", "/home/mara/notes"}
	cmds     = []string{"go test ./... 2>&1 | tail -20", "go build ./... && go vet ./...", "git status --short", "git diff --stat", "git log --oneline -10", "gh pr view 52 --json title,body,state", "gh issue view 31", "mise run lint", "mise run test", "rg -n 'func Grade' route/", "ls -la internal/tiles", "go test ./route/ -run TestGrade -v", "git add -A && git commit -m 'fix(route): stable waypoint sort'", "gofmt -l .", "go mod tidy && git diff go.mod", "cat mise.toml", "wc -l route/*.go", "golangci-lint run ./...", "git checkout -b feat/31-waypoint-sort", "go run ./cmd/kompass plan --json testdata/alps.gpx | head -40"}
	descs    = []string{"Run the tests", "Build and vet the module", "Show the working tree status", "Show recent commits", "Read the pull request", "Search for the grade function", "List the tiles package", "Run the lint task", "Commit the fix", "Format check", "Tidy modules"}
	prompts  = []string{"Füge einen Test für die Wegpunkt-Sortierung hinzu, siehe #31.", "Warum ist der Tiles-Cache nach einem Neustart leer?", "the elevation profile is off by a few metres on long climbs, have a look", "Benenne das Paket route/cache in route/tiles um.", "add a benchmark for the routing core (PR #52) and tell me where the time goes", "Zeig mir, wo die Steigung berechnet wird.", "run the tests and fix what fails", "Schreib die Doku für den Befehl `kompass route`.", "ok, weiter", "ja", "Please also update the changelog.", "What does the dashboard do when the station is offline?", "Erstelle einen PR gegen develop mit `Closes #31`.", "look at the failing CI job and tell me why it is red"}
	replies  = []string{"Done. The sort is stable now and the test covers duplicate waypoints.", "Der Cache wurde beim Start mit einem leeren Verzeichnis überschrieben; ich lese ihn jetzt vor dem Anlegen ein.", "The profile used the raw GPS altitude; it now takes the smoothed series, which removes the offset.", "Umbenannt, Importe angepasst, Tests laufen.", "Most of the time is spent in the heap of the shortest-path search; the benchmark is in route/bench_test.go.", "Die Steigung steht in route/grade.go; sie mittelt über drei Punkte.", "I'll start by reading the failing test, then fix the sort.", "Let me look at the cache initialisation first.", "Die Tests sind grün:\n\n```\nok  \tkompass/route\t0.412s\n```\n\nIch committe das jetzt.", "## Summary\n\n- **Fixed** the waypoint sort (`route/sort.go`)\n- **Added** a table-driven test\n- **Updated** the changelog\n\nNext: open the PR."}
	goCode   = []string{"package route\n\nimport (\n\t\"sort\"\n\t\"testing\"\n)\n\n// Grade returns the slope between two points in percent.\nfunc Grade(a, b Point) float64 {\n\tif a.Dist == b.Dist {\n\t\treturn 0\n\t}\n\treturn (b.Ele - a.Ele) / (b.Dist - a.Dist) * 100\n}\n", "func TestSortWaypoints(t *testing.T) {\n\ttests := []struct {\n\t\tname string\n\t\tin   []Waypoint\n\t\twant []string\n\t}{\n\t\t{\"empty\", nil, nil},\n\t\t{\"duplicates keep order\", []Waypoint{{Name: \"a\"}, {Name: \"a\"}}, []string{\"a\", \"a\"}},\n\t}\n\tfor _, tt := range tests {\n\t\tt.Run(tt.name, func(t *testing.T) {\n\t\t\tgot := Sort(tt.in)\n\t\t\tif !reflect.DeepEqual(names(got), tt.want) {\n\t\t\t\tt.Errorf(\"Sort() = %v, want %v\", got, tt.want)\n\t\t\t}\n\t\t})\n\t}\n}\n", "func (c *Cache) Load(dir string) error {\n\tentries, err := os.ReadDir(dir)\n\tif err != nil {\n\t\treturn fmt.Errorf(\"cache: %w\", err)\n\t}\n\tfor _, e := range entries {\n\t\tif e.IsDir() {\n\t\t\tcontinue\n\t\t}\n\t\tc.add(e.Name())\n\t}\n\treturn nil\n}\n", "if err != nil {\n\treturn nil, fmt.Errorf(\"store: %w\", err)\n}\ndefer func() { _ = rows.Close() }()\n"}
	outputs  = []string{"ok  \tkompass/route\t0.412s\nok  \tkompass/internal/tiles\t0.088s\nok  \tkompass/internal/gpx\t1.204s\n", "--- FAIL: TestSortWaypoints (0.00s)\n    sort_test.go:42: Sort() = [b a a], want [a a b]\nFAIL\nFAIL\tkompass/route\t0.015s\nFAIL\n", " M route/grade.go\n M route/grade_test.go\n?? route/sort.go\n", "a1b2c3d fix(route): stable waypoint sort (#31)\n9f8e7d6 feat(gpx): export segments\n0c1d2e3 docs: describe kompass plan --json\n", "", "route/grade.go:12:func Grade(a, b Point) float64 {\nroute/grade.go:21:func gradeAt(pts []Point, i int) float64 {\n", "[feat/31-waypoint-sort 4d5e6f7] fix(route): stable waypoint sort\n 3 files changed, 48 insertions(+), 6 deletions(-)\n"}
	todos    = []string{"Read the failing test", "Fix the waypoint sort", "Run go test ./...", "Update the changelog", "Open the pull request", "Check the CI job"}
	hooks    = []string{"SessionStart:startup hook success: total-recall ingest --hook --detach", "Stop hook success: total-recall ingest --hook --detach", "PreToolUse:Bash hook success: scripts/guard.sh"}
	thoughts = []string{"The failing test expects a stable sort; sort.SliceStable keeps duplicates in order. Let me check the call site first.", "The cache is written before the directory exists. Reading it on startup and creating the directory lazily fixes both symptoms.", "Three files changed; the changelog entry belongs under Behoben. I will run the tests before committing.", "The user wants the docs in German. The command reference has a section per command; I add one for kompass route."}
)

func (g *synth) path() string { return g.pick(dirs...) + "/" + g.pick(files...) }

// turn writes one exchange: a user prompt, a few tool calls with results,
// and a reply; now and then a system record or a wrapped command.
func (g *synth) turn() []string {
	var out []string
	switch g.r.Intn(12) {
	case 0:
		out = append(out, g.record("user", "message", obj{"role": "user", "content": "<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>"}, "isMeta", true))
		out = append(out, g.record("user", "message", obj{"role": "user", "content": "<local-command-stdout></local-command-stdout>"}, "isMeta", true))
	case 1:
		out = append(out, g.record("user", "message", obj{"role": "user", "content": "<task-notification>\n<task-id>a1b2c3</task-id>\n<status>completed</status>\n<summary>Agent \"Review the sort\" completed</summary>\n</task-notification>"}, "origin", obj{"kind": "task-notification"}))
	case 2:
		out = append(out, g.record("system", "subtype", "turn_duration", "durationMs", 3000+g.r.Intn(300000), "content", fmt.Sprintf("Turn took %ds", 3+g.r.Intn(300)), "isMeta", false, "level", "info"))
	case 3:
		out = append(out, g.record("system", "subtype", "stop_hook_summary", "content", g.pick(hooks...), "isMeta", true, "level", "info"))
	case 4:
		out = append(out, g.record("user", "message", obj{"role": "user", "content": "This session is being continued from a previous conversation that ran out of context. The conversation is summarized below:\n\n## Summary\n\n1. **Primary request:** " + g.pick(prompts...) + "\n2. **Key files:** `" + g.path() + "`\n"}, "isCompactSummary", true, "isVisibleInTranscriptOnly", true))
	case 5:
		out = append(out, g.record("user", "message", obj{"role": "user", "content": "Another Claude session sent a message:\n<cross-session-message from=\"uds:/tmp/cc-socks/1234.sock\" from-name=\"Koordinator\">\n" + g.pick(prompts...) + "\n</cross-session-message>"}, "origin", obj{"kind": "peer"}, "promptSource", "typed"))
	default:
		content := g.pick(prompts...)
		if g.r.Intn(3) == 0 {
			content += "\n\n<system-reminder>\nThe user opened the file " + g.path() + " in the IDE. This may or may not be related to the current task.\n</system-reminder>"
		}
		out = append(out, g.record("user", "message", obj{"role": "user", "content": content}, "origin", obj{"kind": "human"}, "promptSource", "typed", "permissionMode", g.pick("default", "acceptEdits", "plan")))
	}
	model := g.pick(models...)
	if g.r.Intn(3) == 0 {
		out = append(out, g.assistant(model, g.thinking()))
	}
	if g.r.Intn(2) == 0 {
		out = append(out, g.assistant(model, obj{"type": "text", "text": g.pick(replies...)}))
	}
	for i := g.r.Intn(4); i > 0; i-- {
		out = append(out, g.toolCall(model)...)
	}
	out = append(out, g.assistant(model, obj{"type": "text", "text": g.pick(replies...)}))
	return out
}

// toolCall writes one tool_use record and its tool_result record with the
// toolUseResult object Claude Code adds.
func (g *synth) toolCall(model string) []string {
	id := g.toolID()
	var name string
	var input obj
	var result any
	var text string
	switch g.r.Intn(9) {
	case 0, 1, 2:
		name, text = "Bash", g.pick(outputs...)
		input = obj{"command": g.pick(cmds...), "description": g.pick(descs...)}
		result = obj{"stdout": text, "stderr": "", "interrupted": false, "isImage": false, "noOutputExpected": false}
	case 3:
		name = "Read"
		p, code := g.path(), g.pick(goCode...)
		lines := strings.Split(code, "\n")
		var b strings.Builder
		for i, l := range lines {
			fmt.Fprintf(&b, "%6d\t%s\n", i+1, l)
		}
		text = b.String()
		input = obj{"file_path": p}
		result = obj{"type": "text", "file": obj{"filePath": p, "content": code, "numLines": len(lines), "startLine": 1, "totalLines": len(lines)}}
	case 4:
		name = "Edit"
		p := g.path()
		old, new := "return 0", "return math.NaN()"
		text = "The file " + p + " has been updated successfully."
		input = obj{"file_path": p, "old_string": old, "new_string": new}
		result = obj{"filePath": p, "oldString": old, "newString": new, "originalFile": g.pick(goCode...), "replaceAll": false, "userModified": false}
	case 5:
		name = "Write"
		p, code := g.path(), g.pick(goCode...)
		text = "File created successfully at: " + p
		input = obj{"file_path": p, "content": code}
		result = obj{"type": "create", "filePath": p, "content": code, "structuredPatch": []any{}}
	case 6:
		name = "Grep"
		text = g.pick(outputs...)
		input = obj{"pattern": "func Grade", "path": g.pick(dirs...), "output_mode": "content", "-n": true}
		result = obj{"mode": "content", "numFiles": 2, "filenames": []string{g.path(), g.path()}, "content": text, "numLines": 2}
	case 7:
		name = "Glob"
		fs := []string{g.path(), g.path(), g.path()}
		text = strings.Join(fs, "\n")
		input = obj{"pattern": "**/*_test.go", "path": g.pick(dirs...)}
		result = obj{"filenames": fs, "durationMs": 5 + g.r.Intn(80), "numFiles": len(fs), "truncated": false}
	default:
		name = "TodoWrite"
		var list []obj
		for i, td := range todos[:2+g.r.Intn(4)] {
			st := "pending"
			switch i {
			case 0:
				st = "completed"
			case 1:
				st = "in_progress"
			}
			list = append(list, obj{"content": td, "status": st, "activeForm": td + "…"})
		}
		text = "Todos have been modified successfully. Ensure that you continue to use the todo list to track your progress. Please proceed with the current tasks if applicable"
		input = obj{"todos": list}
		result = obj{"oldTodos": list, "newTodos": list}
	}
	use := g.assistant(model, obj{"type": "tool_use", "id": id, "name": name, "input": input})
	block := obj{"tool_use_id": id, "type": "tool_result", "content": text}
	if g.r.Intn(10) == 0 {
		block["is_error"] = true
		block["content"] = "Exit code 1\n" + text
	}
	res := g.record("user", "message", obj{"role": "user", "content": []obj{block}}, "toolUseResult", result,
		"sourceToolAssistantUUID", g.parent)
	return []string{use, res}
}

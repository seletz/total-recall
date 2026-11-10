package cli

import (
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// TestTimeFlagErrorsNameTheFlag: an invalid time value names the flag it
// came from, in the CLI (--since) and in MCP (argument "since"), for every
// command with a time flag.
func TestTimeFlagErrorsNameTheFlag(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	s := &mcpSession{t: t, e: e, args: []string{"--db", db}}

	const bad = "garbage"
	for _, tc := range []struct {
		name string
		cli  []string // CLI args without --db and --json
		tool string   // MCP tool, "" = CLI only
		args map[string]any
		flag string // flag name without dashes
		want string // text after the flag name in the message
	}{
		{"sessions since", []string{"sessions", "--since", bad}, "sessions", map[string]any{"since": bad}, "since", `invalid time "garbage"`},
		{"sessions until", []string{"sessions", "--until", bad}, "sessions", map[string]any{"until": bad}, "until", `invalid time "garbage"`},
		{"search since", []string{"search", "x", "--since", bad}, "search", map[string]any{"query": "x", "since": bad}, "since", `invalid time "garbage"`},
		{"search until", []string{"search", "x", "--until", bad}, "search", map[string]any{"query": "x", "until": bad}, "until", `invalid time "garbage"`},
		{"timeline since", []string{"timeline", "--since", bad}, "timeline", map[string]any{"since": bad}, "since", `invalid time "garbage"`},
		{"timeline until", []string{"timeline", "--until", bad}, "timeline", map[string]any{"until": bad}, "until", `invalid time "garbage"`},
		{"show since", []string{"show", "claude:x", "--since", bad}, "show", map[string]any{"session": "claude:x", "since": bad}, "since", `invalid time "garbage"`},
		{"show until", []string{"show", "claude:x", "--until", bad}, "show", map[string]any{"session": "claude:x", "until": bad}, "until", `invalid time "garbage"`},
		{"stats since", []string{"stats", "--since", bad}, "stats", map[string]any{"since": bad}, "since", `invalid time "garbage"`},
		{"stats until", []string{"stats", "--until", bad}, "stats", map[string]any{"until": bad}, "until", `invalid time "garbage"`},
		{"stats month", []string{"stats", "--month", bad}, "stats", map[string]any{"month": bad}, "month", `invalid --month`},
		{"stats workday since", []string{"stats", "--workday", "--since", bad}, "stats", map[string]any{"workday": true, "since": bad}, "since", `invalid time "garbage"`},
		{"stats worktime until", []string{"stats", "--worktime", "--until", bad}, "stats", map[string]any{"worktime": true, "until": bad}, "until", `invalid time "garbage"`},
		{"stats workday range", []string{"stats", "--workday", "--range", bad}, "stats", map[string]any{"workday": true, "range": bad}, "range", `invalid --range`},
		{"memory at", []string{"memory", "--at", bad}, "memory", map[string]any{"at": bad}, "at", `invalid time "garbage"`},
		{"chart since", []string{"chart", "all", "--out", "x", "--since", bad}, "", nil, "since", `invalid time "garbage"`},
		{"chart until", []string{"chart", "all", "--out", "x", "--until", bad}, "", nil, "until", `invalid time "garbage"`},
		{"chart month", []string{"chart", "all", "--out", "x", "--month", bad}, "", nil, "month", `invalid --month`},
		{"chart range", []string{"chart", "all", "--out", "x", "--range", bad}, "", nil, "range", `invalid --range`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--db", db}, tc.cli...)
			if tc.tool != "" {
				args = append(args, "--json")
			}
			r := e.run(t, "", args...)
			if r.code != 1 {
				t.Fatalf("exit %d, want 1: %s %s", r.code, r.stdout, r.stderr)
			}
			var er ErrorBody
			if tc.tool != "" {
				er = decode[ErrorJSON](t, r.stdout).Error
			} else { // chart has no --json: the text error on stderr
				er = ErrorBody{Kind: "usage", Message: strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(r.stderr), "Error: "), "total-recall: ")}
			}
			if er.Kind != "usage" || !strings.Contains(er.Message, "--"+tc.flag) || !strings.Contains(er.Message, tc.want) {
				t.Errorf("CLI error = %+v, want usage naming --%s and %q", er, tc.flag, tc.want)
			}
			if strings.Contains(tc.flag+tc.want, "invalid time") && !strings.HasPrefix(er.Message, "--"+tc.flag+": ") {
				t.Errorf("CLI message %q does not start with %q", er.Message, "--"+tc.flag+": ")
			}
			if tc.tool == "" {
				return
			}
			got, isErr := s.call(tc.tool, tc.args)
			if !isErr {
				t.Fatalf("MCP: no error: %v", got)
			}
			m := decode[ErrorJSON](t, got[0]).Error
			if m.Kind != "usage" || !strings.Contains(m.Message, `argument "`+tc.flag+`"`) || strings.Contains(m.Message, "--") {
				t.Errorf("MCP error = %+v, want usage naming argument %q without --", m, tc.flag)
			}
		})
	}
}

package dispatch

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 3, 20, 0, 0, 0, time.UTC)

func TestSent(t *testing.T) {
	long := "Bitte develop in den Branch mergen, die Konflikte im Changelog loesen und danach go test ausfuehren"
	tests := []struct {
		name    string
		calls   []Call
		session string
		after   time.Duration // worker input this long after t0
		text    string
		want    bool
	}{
		{"herdr prompt 3 s earlier", []Call{{"A", t0, `herdr agent prompt w1 "` + long + `"`}}, "B", 3 * time.Second, long, true},
		{"same session never matches", []Call{{"B", t0, `herdr agent prompt w1 "` + long + `"`}}, "B", 3 * time.Second, long, false},
		{"other text", []Call{{"A", t0, `herdr agent prompt w1 "` + long + `"`}}, "B", 3 * time.Second, "Bitte den Release v1.7.0 taggen und veroeffentlichen", false},
		{"outside the window", []Call{{"A", t0, `herdr agent prompt w1 "` + long + `"`}}, "B", 121 * time.Second, long, false},
		{"inside the window", []Call{{"A", t0, `herdr agent prompt w1 "` + long + `"`}}, "B", 120 * time.Second, long, true},
		{"input before the call", []Call{{"A", t0.Add(5 * time.Second), `herdr agent prompt w1 "` + long + `"`}}, "B", 0, long, false},
		{"whitespace and quotes are ignored", []Call{{"A", t0, `tmux send-keys -t w1 'Bitte develop   in den Branch mergen,` + "\n" + `die Konflikte im Changelog loesen und danach go test ausfuehren' Enter`}}, "B", time.Second, long, true},
		{"escaped newlines in the command", []Call{{"A", t0, `herdr agent prompt w1 "Zeile eins der Aufgabe\nZeile zwei der Aufgabe"`}}, "B", time.Second, "Zeile eins der Aufgabe\nZeile zwei der Aufgabe", true},
		{"pasted wrapper is stripped", []Call{{"A", t0, `herdr agent prompt w1 "` + long + `"`}}, "B", time.Second, "<pasted_content id=\"1\">\n" + long + "\n</pasted_content id=\"1\">", true},
		{"long text compares its start", []Call{{"A", t0, `herdr agent prompt w1 "` + long + long + long + `"`}}, "B", time.Second, long + long + long + " und ein ganz anderes Ende", true},
		{"command text cut at its end", []Call{{"A", t0, `herdr agent prompt w1 "` + long[:60]}}, "B", time.Second, long, false},
		{"short text without a prompt command", []Call{{"A", t0, `echo "weiter"`}}, "B", time.Second, "weiter", false},
		{"short text to a prompt command", []Call{{"A", t0, `herdr agent prompt w1 "weiter"`}}, "B", time.Second, "weiter", true},
		{"any tool argument counts", []Call{{"A", t0, long}}, "B", time.Second, long, true},
		{"empty text", []Call{{"A", t0, `herdr agent prompt w1 ""`}}, "B", time.Second, "  ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix := NewIndex(tt.calls, Config{})
			if got := ix.Sent(tt.session, t0.Add(tt.after), tt.text); got != tt.want {
				t.Errorf("Sent = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig(t *testing.T) {
	calls := []Call{{"A", t0, `mysend w1 "ok"`}}
	if NewIndex(calls, Config{}).Sent("B", t0.Add(time.Second), "ok") {
		t.Error("an unknown command must not match a short text")
	}
	if !NewIndex(calls, Config{Commands: []string{"mysend"}}).Sent("B", t0.Add(time.Second), "ok") {
		t.Error("a configured prompt command must match a short text")
	}
	if NewIndex([]Call{{"A", t0, "A long enough text for the generic match"}}, Config{Window: 5 * time.Second}).Sent("B", t0.Add(6*time.Second), "A long enough text for the generic match") {
		t.Error("the window is configurable")
	}
	if NewIndex([]Call{{"A", t0, "A long enough text for the generic match"}}, Config{Window: -1}).Sent("B", t0.Add(time.Second), "A long enough text for the generic match") {
		t.Error("a negative window turns the detection off")
	}
}

func TestSpawned(t *testing.T) {
	const (
		cwd    = "/home/u/src/proj__worktrees/feat-x"
		branch = "feat/x-spawn"
	)
	start := Call{"A", t0, `herdr agent start w2 --cwd ` + cwd + ` --branch ` + branch}
	prompt := func(after time.Duration) Call {
		return Call{"A", t0.Add(after), `herdr agent prompt w2 "$(cat /tmp/p.md)"`}
	}
	tests := []struct {
		name     string
		calls    []Call
		session  string
		after    time.Duration
		branch   string
		cwd      string
		wantSent bool
	}{
		{"start naming the cwd, prompt delivered", []Call{start, prompt(2 * time.Second)}, "B", 5 * time.Second, "", cwd, true},
		{"start naming the branch, prompt delivered", []Call{start, prompt(2 * time.Second)}, "B", 5 * time.Second, branch, "/elsewhere", true},
		{"loop over the worker directories", []Call{
			{"A", t0, `for d in /home/u/src/p/a /home/u/src/p/b /home/u/src/p/c; do herdr agent start w --cwd $d; herdr agent prompt w "$(cat $d/PROMPT.md)"; done`},
		}, "B", time.Second, "", "/home/u/src/p/b", true},
		{"a human opens the session, no spawn", nil, "B", 5 * time.Second, branch, cwd, false},
		{"start without a prompt delivered", []Call{start}, "B", 5 * time.Second, branch, cwd, false},
		{"prompt delivered before the start", []Call{prompt(0), {"A", t0.Add(time.Second), start.Text}}, "B", 5 * time.Second, branch, cwd, false},
		{"start of another session's branch", []Call{start, prompt(2 * time.Second)}, "B", 5 * time.Second, "feat/other", "/home/u/src/other", false},
		{"default branch is no reference", []Call{{"A", t0, `herdr agent start w2 --branch main`}, prompt(time.Second)}, "B", 5 * time.Second, "main", "", false},
		{"outside the window", []Call{start, prompt(2 * time.Second)}, "B", 200 * time.Second, branch, cwd, false},
		{"same session", []Call{start, prompt(2 * time.Second)}, "A", 5 * time.Second, branch, cwd, false},
		{"unrelated command names the cwd", []Call{{"A", t0, `ls ` + cwd}, prompt(time.Second)}, "B", 5 * time.Second, branch, cwd, false},
		{"prompt from another session does not deliver", []Call{start, {"C", t0.Add(time.Second), `herdr agent prompt w9 "x"`}}, "B", 5 * time.Second, branch, cwd, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix := NewIndex(tt.calls, Config{})
			if got := ix.Spawned(tt.session, t0.Add(tt.after), tt.branch, tt.cwd); got != tt.wantSent {
				t.Errorf("Spawned = %v, want %v", got, tt.wantSent)
			}
		})
	}
	if NewIndex([]Call{start, prompt(time.Second)}, Config{SpawnCommands: []string{"mystart"}}).Spawned("B", t0.Add(5*time.Second), branch, cwd) {
		t.Error("with another spawn command list, herdr agent start no longer counts")
	}
	if NewIndex([]Call{start, prompt(time.Second)}, Config{Window: -1}).Spawned("B", t0.Add(5*time.Second), branch, cwd) {
		t.Error("a negative window turns the detection off")
	}
}

func TestSpawnedThroughFunctions(t *testing.T) {
	const def = `spawn(){ herdr agent start w --cwd "$1"; herdr agent prompt w "$(cat $2)"; }`
	invoke := `COMMON='x'; spawn /home/u/src/p/feat-y /tmp/a.md; spawn /home/u/src/p/feat-z /tmp/b.md`
	tests := []struct {
		name    string
		calls   []Call
		session string
		cwd     string
		after   time.Duration // the first input this long after t0
		want    bool
	}{
		{"called in a later call of the same session", []Call{{"A", t0, def}, {"A", t0.Add(45 * time.Second), invoke}}, "B", "/home/u/src/p/feat-y", 46 * time.Second, true},
		{"second target of the same call", []Call{{"A", t0, def}, {"A", t0.Add(45 * time.Second), invoke}}, "B", "/home/u/src/p/feat-z", 71 * time.Second, true},
		{"function keyword form", []Call{{"A", t0, `function spawn { herdr agent start w --cwd "$1"; herdr agent prompt w x; }`}, {"A", t0.Add(time.Minute), invoke}}, "B", "/home/u/src/p/feat-y", 61 * time.Second, true},
		{"defined and called in one call", []Call{{"A", t0, def + "; " + invoke}}, "B", "/home/u/src/p/feat-y", 3 * time.Second, true},
		{"defined by another session", []Call{{"C", t0, def}, {"A", t0.Add(45 * time.Second), invoke}}, "B", "/home/u/src/p/feat-y", 46 * time.Second, false},
		{"definition too long ago", []Call{{"A", t0, def}, {"A", t0.Add(11 * time.Minute), invoke}}, "B", "/home/u/src/p/feat-y", 11*time.Minute + time.Second, false},
		{"function without spawn or prompt command", []Call{{"A", t0, `spawn(){ echo "$1"; }`}, {"A", t0.Add(time.Second), invoke}}, "B", "/home/u/src/p/feat-y", 2 * time.Second, false},
		{"the name is only part of a word", []Call{{"A", t0, def}, {"A", t0.Add(time.Second), `respawn /home/u/src/p/feat-y /tmp/a.md`}}, "B", "/home/u/src/p/feat-y", 2 * time.Second, false},
		{"call names another directory", []Call{{"A", t0, def}, {"A", t0.Add(time.Second), invoke}}, "B", "/home/u/src/p/feat-other", 2 * time.Second, false},
		{"called before the definition", []Call{{"A", t0, invoke}, {"A", t0.Add(time.Second), def}}, "B", "/home/u/src/p/feat-y", 2 * time.Second, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix := NewIndex(tt.calls, Config{})
			if got := ix.Spawned(tt.session, t0.Add(tt.after), "", tt.cwd); got != tt.want {
				t.Errorf("Spawned = %v, want %v", got, tt.want)
			}
		})
	}
}

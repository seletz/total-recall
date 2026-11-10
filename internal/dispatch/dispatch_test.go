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

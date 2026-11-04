package worktime

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// summaryEvents is one day with two projects: kompass with three
// sessions (one titled with a ref, one titled without, one untitled with
// refs only) and wetterwarte with one untitled session without refs.
func summaryEvents() []Event {
	k := func(min int, session, title, text string) Event {
		return Event{Time: tm(2026, 11, 3, 9, min), Session: session, Title: title, Project: "/w/kompass", Remote: "https://example.test/o/kompass", Text: text}
	}
	w := func(min int, session, title, text string) Event {
		return Event{Time: tm(2026, 11, 3, 9, min), Session: session, Title: title, Project: "/w/wetterwarte", Remote: "https://example.test/o/wetterwarte", Text: text}
	}
	return []Event{
		k(0, "s1", "SBOM (#31)", "Start with #31, the SBOM. See also PR #32."),
		k(5, "s1", "SBOM (#31)", "ok and #31 again"),
		k(10, "s2", "Konfig", "Lies #21 und mach weiter."),
		k(15, "s3", "", "Fix #19 and #26."),
		k(20, "s3", "", "also owner/repo#7"),
		w(25, "s4", "", "  Frostwarnung bauen,\nzweite Zeile  "),
		w(30, "s5", "", ""),
		w(35, "s6", "Konfig", "#21 noch einmal, aber in wetterwarte"),
	}
}

func TestComputeSummary(t *testing.T) {
	rep := Compute(summaryEvents(), Settings{MaxGap: 20 * time.Minute, MinCredit: 3 * time.Minute, Grid: 15 * time.Minute, SummaryMaxChars: 200}, Options{})
	if len(rep.Days) != 1 || len(rep.Days[0].Categories) != 2 {
		t.Fatalf("days = %+v", rep.Days)
	}
	kompass, wetter := rep.Days[0].Categories[0], rep.Days[0].Categories[1]
	if kompass.Name != "kompass" || wetter.Name != "wetterwarte" {
		t.Fatalf("categories = %q, %q", kompass.Name, wetter.Name)
	}
	if got, want := refStrings(kompass.Refs), []string{"#19", "#21", "#26", "#31", "PR #32", "owner/repo#7"}; !reflect.DeepEqual(got, want) {
		t.Errorf("kompass refs = %v, want %v", got, want)
	}
	if len(kompass.Projects) != 1 || !reflect.DeepEqual(refStrings(kompass.Projects[0].Refs), refStrings(kompass.Refs)) {
		t.Errorf("kompass project refs = %+v", kompass.Projects)
	}
	if len(kompass.Sessions) != 3 {
		t.Fatalf("kompass sessions = %+v", kompass.Sessions)
	}
	for i, want := range [][]string{{"#31", "PR #32"}, {"#21"}, {"#19", "#26", "owner/repo#7"}} {
		if got := refStrings(kompass.Sessions[i].Refs); !reflect.DeepEqual(got, want) {
			t.Errorf("session %d refs = %v, want %v", i, got, want)
		}
	}
	if kompass.Sessions[0].Project != "/w/kompass" || kompass.Sessions[0].Prompt != "Start with #31, the SBOM. See also PR #32." {
		t.Errorf("session 0 = %+v", kompass.Sessions[0])
	}
	// The title keeps its own ref; refs from the messages are appended;
	// an untitled session shows its refs.
	if want := "kompass: SBOM (#31) (PR #32), Konfig (#21), #19/#26/owner/repo#7"; kompass.Text != want {
		t.Errorf("kompass text = %q, want %q", kompass.Text, want)
	}
	// wetterwarte: the untitled session without refs falls back to the
	// first line of its first message; one without any text is untitled.
	if got, want := refStrings(wetter.Refs), []string{"#21"}; !reflect.DeepEqual(got, want) {
		t.Errorf("wetterwarte refs = %v, want %v", got, want)
	}
	if want := "wetterwarte: Frostwarnung bauen,, (untitled), Konfig (#21)"; wetter.Text != want {
		t.Errorf("wetterwarte text = %q, want %q", wetter.Text, want)
	}
}

func TestComputeSummaryTwoProjects(t *testing.T) {
	events := summaryEvents()
	rules := []Rule{mustRule(t, "kunde", []string{"https://example.test/o/*"})}
	rep := Compute(events, Settings{MaxGap: 20 * time.Minute, MinCredit: 3 * time.Minute, Rules: rules, SummaryMaxChars: 200}, Options{})
	c := rep.Days[0].Categories[0]
	if c.Name != "kunde" || len(c.Projects) != 2 {
		t.Fatalf("category = %+v", c)
	}
	if got, want := refStrings(c.Refs), []string{"#19", "#21", "#26", "#31", "PR #32", "owner/repo#7"}; !reflect.DeepEqual(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
	if got, want := refStrings(c.Projects[1].Refs), []string{"#21"}; c.Projects[1].Name != "wetterwarte" || !reflect.DeepEqual(got, want) {
		t.Errorf("wetterwarte project = %+v", c.Projects[1])
	}
	// Sessions are listed once per project, in order of their first message.
	want := "kompass: SBOM (#31) (PR #32), Konfig (#21), #19/#26/owner/repo#7; wetterwarte: Frostwarnung bauen,, (untitled), Konfig (#21)"
	if c.Text != want {
		t.Errorf("text = %q, want %q", c.Text, want)
	}
	// Truncation keeps the text within the limit, in runes.
	rep = Compute(events, Settings{MaxGap: 20 * time.Minute, MinCredit: 3 * time.Minute, Rules: rules, SummaryMaxChars: 30}, Options{})
	if got := rep.Days[0].Categories[0].Text; len([]rune(got)) != 30 || !strings.HasSuffix(got, "…") || !strings.HasPrefix(want, strings.TrimSuffix(got, "…")) {
		t.Errorf("truncated text = %q", got)
	}
	// Zero means no limit.
	rep = Compute(events, Settings{MaxGap: 20 * time.Minute, MinCredit: 3 * time.Minute, Rules: rules}, Options{})
	if got := rep.Days[0].Categories[0].Text; got != want {
		t.Errorf("unlimited text = %q", got)
	}
}

// TestComputeSummaryDuplicateTitles: two sessions with the same label are
// named once; a ref that is already in the title is not appended.
func TestComputeSummaryDuplicateTitles(t *testing.T) {
	events := []Event{
		{Time: tm(2026, 11, 3, 9, 0), Session: "a", Title: "Fix #5", Project: "/w/p", Text: "fix #5"},
		{Time: tm(2026, 11, 3, 9, 5), Session: "b", Title: "Fix #5", Project: "/w/p", Text: "again #5"},
		{Time: tm(2026, 11, 3, 9, 10), Session: "c", Title: "", Project: "/w/p", Text: ""},
	}
	rep := Compute(events, Settings{MaxGap: 20 * time.Minute, MinCredit: 3 * time.Minute}, Options{})
	c := rep.Days[0].Categories[0]
	if want := "p: Fix #5, (untitled)"; c.Text != want {
		t.Errorf("text = %q, want %q", c.Text, want)
	}
	if got := refStrings(c.Refs); !reflect.DeepEqual(got, []string{"#5"}) {
		t.Errorf("refs = %v", got)
	}
	if got := refStrings(rep.Days[0].Categories[0].Sessions[2].Refs); got != nil {
		t.Errorf("session without refs = %v, want none", got)
	}
}

func refStrings(refs []Ref) []string {
	var out []string
	for _, r := range refs {
		out = append(out, r.String())
	}
	return out
}

// tm is a UTC instant of the summary tests.
func tm(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

func mustRule(t *testing.T, name string, patterns []string) Rule {
	t.Helper()
	r, err := NewRule(name, patterns)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

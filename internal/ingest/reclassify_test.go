package ingest

import (
	"database/sql"
	"testing"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/claude"
	tf "github.com/seletz/total-recall/internal/testfixture"
)

const taskPaste = "<pasted_content id=\"4a2b\">\n# Task: rebase the branch\nThen open the PR.\n</pasted_content id=\"4a2b\">\n"

// writeReclassifyTree writes a session with a typed message, a pasted task
// block and a subagent prompt, and ingests it with the default rules.
func writeReclassifyTree(t *testing.T, e *env) {
	t.Helper()
	tf.WriteFile(t, e.projects, "-w-alpha/"+sid+".jsonl", tf.Join(
		tf.User(sid, "r1", "2026-10-05T08:00:00.000Z", "/w/alpha", "please list the files"),
		tf.Assistant(sid, "r2", "2026-10-05T08:00:02.000Z", "/w/alpha", "Done."),
		tf.User(sid, "r3", "2026-10-05T23:00:00.000Z", "/w/alpha", taskPaste),
	))
	tf.WriteFile(t, e.projects, "-w-alpha/"+sid+"/subagents/agent-r01.jsonl",
		tf.Line{"type": "user", "uuid": "r4", "timestamp": "2026-10-05T08:00:10.000Z", "sessionId": sid, "agentId": "r01",
			"cwd": "/w/alpha", "message": map[string]any{"role": "user", "content": "Research task: count."}}.JSON())
	if _, err := All(ctx, e.deps); err != nil {
		t.Fatal(err)
	}
}

func TestReclassify(t *testing.T) {
	e := newEnv(t)
	writeReclassifyTree(t, e)
	actor := func(id string) string { return e.str(t, "SELECT actor FROM events WHERE id = 'claude:"+id+"'") }
	if a := actor("r3"); a != "human" {
		t.Fatalf("pasted task ingested as %q, want human with default rules", a)
	}

	// Same rules: nothing changes.
	sum, err := Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Examined != 3 || sum.Changed != 0 || sum.Undecided != 0 || len(sum.Transitions) != 0 {
		t.Errorf("unchanged rules: %+v", sum)
	}

	// New rule, dry run: counted, not written.
	e.deps.Adapters = []harness.Adapter{claude.New(e.projects, claude.WithPeerPasteHeaders("# Task:"))}
	sum, err = Reclassify(ctx, e.deps, true)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Examined != 3 || sum.Changed != 1 || !sum.DryRun || sum.Transitions["human -> peer"] != 1 {
		t.Errorf("dry run: %+v", sum)
	}
	if a := actor("r3"); a != "human" {
		t.Errorf("dry run wrote actor %q", a)
	}

	// Apply, then again: idempotent.
	sum, err = Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Changed != 1 || sum.DryRun {
		t.Errorf("apply: %+v", sum)
	}
	if a, b, c := actor("r1"), actor("r3"), actor("r4"); a != "human" || b != "peer" || c != "peer" {
		t.Errorf("actors after apply = %s %s %s, want human peer peer", a, b, c)
	}
	sum, err = Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Changed != 0 {
		t.Errorf("second apply: %+v", sum)
	}

	// Rule removed again: the paste goes back to human.
	e.deps.Adapters = []harness.Adapter{claude.New(e.projects)}
	sum, err = Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Changed != 1 || sum.Transitions["peer -> human"] != 1 || actor("r3") != "human" {
		t.Errorf("rule removed: %+v, actor %s", sum, actor("r3"))
	}
}

// Without a stored raw record only the text rules can decide; other
// messages keep their actor and are reported as undecided.
func TestReclassifyWithoutRaw(t *testing.T) {
	e := newEnv(t)
	e.deps.StoreRaw = false
	writeReclassifyTree(t, e)
	e.deps.Adapters = []harness.Adapter{claude.New(e.projects, claude.WithPeerPasteHeaders("# Task:"))}
	sum, err := Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Examined != 3 || sum.Changed != 1 || sum.Undecided != 2 {
		t.Errorf("without raw: %+v", sum)
	}
	if n := e.count(t, "SELECT count(*) FROM events WHERE actor = 'peer'"); n != 2 {
		t.Errorf("peer events = %d, want 2 (paste and subagent prompt)", n)
	}
}

// An adapter that cannot reclassify is skipped, not an error.
type plainAdapter struct{ harness.Adapter }

func (plainAdapter) Name() string { return "plain" }

func TestReclassifySkipsAdaptersWithoutSupport(t *testing.T) {
	e := newEnv(t)
	writeReclassifyTree(t, e)
	e.deps.Adapters = []harness.Adapter{plainAdapter{}}
	sum, err := Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Examined != 0 || sum.Changed != 0 {
		t.Errorf("unsupported adapter: %+v", sum)
	}
}

// Reclassify fills input_kind of an archive that has none (events
// ingested before schema v10 and not covered by the migration), and clears
// it when the actor stops being human.
func TestReclassifyInputKind(t *testing.T) {
	e := newEnv(t)
	writeReclassifyTree(t, e)
	kind := func(id string) string {
		return e.str(t, "SELECT COALESCE(input_kind, '-') FROM events WHERE id = 'claude:"+id+"'")
	}
	if a, b, c := kind("r1"), kind("r3"), kind("r4"); a != "typed" || b != "pasted" || c != "-" {
		t.Fatalf("input kinds at ingest = %s %s %s, want typed pasted -", a, b, c)
	}
	db, err := sql.Open("sqlite", "file:"+e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE events SET input_kind = NULL"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	sum, err := Reclassify(ctx, e.deps, true)
	if err != nil || sum.InputKinds != 2 || kind("r1") != "-" {
		t.Fatalf("dry run: %+v (%v), r1 = %s", sum, err, kind("r1"))
	}
	sum, err = Reclassify(ctx, e.deps, false)
	if err != nil || sum.InputKinds != 2 || sum.Changed != 0 {
		t.Fatalf("apply: %+v (%v)", sum, err)
	}
	if a, b, c := kind("r1"), kind("r3"), kind("r4"); a != "typed" || b != "pasted" || c != "-" {
		t.Errorf("input kinds after apply = %s %s %s", a, b, c)
	}
	if sum, _ = Reclassify(ctx, e.deps, false); sum.InputKinds != 0 {
		t.Errorf("second apply: %+v", sum)
	}

	// A pasted task becomes a peer message: no input kind any more.
	e.deps.Adapters = []harness.Adapter{claude.New(e.projects, claude.WithPeerPasteHeaders("# Task:"))}
	sum, err = Reclassify(ctx, e.deps, false)
	if err != nil || sum.Changed != 1 || kind("r3") != "-" {
		t.Errorf("peer rule: %+v (%v), r3 = %s", sum, err, kind("r3"))
	}
}

// ingest --reread --rewrite-raw fills input_kind of events archived before
// it existed, from the transcript.
func TestRereadFillsInputKind(t *testing.T) {
	e := newEnv(t)
	writeReclassifyTree(t, e)
	db, err := sql.Open("sqlite", "file:"+e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE events SET input_kind = NULL"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	e.deps.Reread, e.deps.RewriteRaw = true, true
	if _, err := All(ctx, e.deps); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "SELECT count(*) FROM events WHERE input_kind IN ('typed', 'pasted')"); n != 2 {
		t.Errorf("events with input_kind after reread = %d, want 2", n)
	}
	if n := e.count(t, "SELECT count(*) FROM events WHERE input_kind IS NOT NULL AND actor <> 'human'"); n != 0 {
		t.Errorf("non-human events with input_kind = %d", n)
	}
}

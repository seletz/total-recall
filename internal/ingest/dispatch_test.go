package ingest

import (
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/dispatch"
	tf "github.com/seletz/total-recall/internal/testfixture"
)

const (
	coord  = "c0000000-0000-4000-8000-000000000001"
	worker = "c0000000-0000-4000-8000-000000000002"
	task   = "Bitte develop in den Branch mergen und die Tests laufen lassen"
)

// writeDispatchTree writes a coordinator session that sends task to a worker
// with herdr, a worker that gets it 3 s later, and the cases that must stay
// human: another text, the same text outside the window, the person's own
// input in the coordinator.
func writeDispatchTree(t *testing.T, e *env) {
	t.Helper()
	tf.WriteFile(t, e.projects, "-w-alpha/"+coord+".jsonl", tf.Join(
		tf.User(coord, "c1", "2026-10-05T20:00:00.000Z", "/w/alpha", "dispatch the work"),
		tf.ToolUse(coord, "c2", "2026-10-05T20:01:00.000Z", "/w/alpha", "Bash", `herdr agent prompt w1 "`+task+`"`),
		tf.User(coord, "c3", "2026-10-05T20:01:03.000Z", "/w/alpha", task), // same session: the person's own input
	))
	tf.WriteFile(t, e.projects, "-w-beta/"+worker+".jsonl", tf.Join(
		tf.User(worker, "w1", "2026-10-05T20:01:03.000Z", "/w/beta", task),
		tf.Assistant(worker, "w2", "2026-10-05T20:01:20.000Z", "/w/beta", "On it."),
		tf.User(worker, "w3", "2026-10-05T20:02:00.000Z", "/w/beta", "Und jetzt etwas ganz anderes bitte"),
		tf.User(worker, "w4", "2026-10-05T20:05:00.000Z", "/w/beta", task), // outside the window
	))
}

func TestIngestMarksDispatchedPrompts(t *testing.T) {
	for _, order := range []string{"coordinator first", "worker first"} {
		t.Run(order, func(t *testing.T) {
			e := newEnv(t)
			writeDispatchTree(t, e)
			first, second := "-w-alpha/"+coord+".jsonl", "-w-beta/"+worker+".jsonl"
			if order == "worker first" {
				first, second = second, first
			}
			run(t, e, e.projects+"/"+first)
			run(t, e, e.projects+"/"+second)
			actor := func(id string) string { return e.str(t, "SELECT actor FROM events WHERE id = 'claude:"+id+"'") }
			for id, want := range map[string]string{"w1": "automation", "w3": "human", "w4": "human", "c1": "human", "c3": "human"} {
				if got := actor(id); got != want {
					t.Errorf("%s: actor %q, want %q", id, got, want)
				}
			}
			if k := e.str(t, "SELECT COALESCE(input_kind, 'NULL') FROM events WHERE id = 'claude:w1'"); k != "NULL" {
				t.Errorf("a dispatched prompt keeps input_kind %q", k)
			}
		})
	}
}

func TestIngestDispatchOff(t *testing.T) {
	e := newEnv(t)
	e.deps.Dispatch = dispatch.Config{Window: -1}
	writeDispatchTree(t, e)
	if _, err := All(ctx, e.deps); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "SELECT count(*) FROM events WHERE actor = 'automation'"); n != 0 {
		t.Errorf("%d automation events with the detection off", n)
	}
}

// TestReclassifyDispatch: reindex --actors finds what an older ingest left
// human, is idempotent, and keeps what ingest marked.
func TestReclassifyDispatch(t *testing.T) {
	e := newEnv(t)
	e.deps.Dispatch = dispatch.Config{Window: -1} // as an archive from before
	writeDispatchTree(t, e)
	if _, err := All(ctx, e.deps); err != nil {
		t.Fatal(err)
	}
	e.deps.Dispatch = dispatch.Config{}
	dry, err := Reclassify(ctx, e.deps, true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Dispatched != 1 || dry.Changed != 1 || dry.Transitions["human -> automation"] != 1 {
		t.Errorf("dry run: %+v", dry)
	}
	if n := e.count(t, "SELECT count(*) FROM events WHERE actor = 'automation'"); n != 0 {
		t.Errorf("dry run wrote %d events", n)
	}
	sum, err := Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Dispatched != 1 || sum.Changed != 1 {
		t.Errorf("run: %+v", sum)
	}
	if a := e.str(t, "SELECT actor FROM events WHERE id = 'claude:w1'"); a != "automation" {
		t.Errorf("w1 actor %q", a)
	}
	// A second run changes nothing: the marked event stays automation.
	again, err := Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed != 0 || again.InputKinds != 0 {
		t.Errorf("second run: %+v", again)
	}
	// The window is as configured: 2 s is too short for the 3 s gap.
	e.deps.Dispatch = dispatch.Config{Window: 2 * time.Second}
	back, err := Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if back.Transitions["automation -> human"] != 1 {
		t.Errorf("shorter window: %+v", back)
	}
}

const (
	spawner = "c0000000-0000-4000-8000-000000000003"
	spawned = "c0000000-0000-4000-8000-000000000004"
	opened  = "c0000000-0000-4000-8000-000000000005"
	looped  = "c0000000-0000-4000-8000-000000000006"
)

// writeSpawnTree: a coordinator starts a worker in /w/spawned-worker with
// herdr and hands it a prompt from a file (the text is in no argument); a
// loop does the same for two directories; the person opens a session in
// /w/opened-by-hand without any command.
func writeSpawnTree(t *testing.T, e *env) {
	t.Helper()
	tf.WriteFile(t, e.projects, "-w-alpha/"+spawner+".jsonl", tf.Join(
		tf.User(spawner, "s1", "2026-10-06T20:00:00.000Z", "/w/alpha", "start the worker"),
		tf.ToolUse(spawner, "s2", "2026-10-06T20:01:00.000Z", "/w/alpha", "Bash", `herdr agent start w2 --cwd /w/spawned-worker && herdr agent prompt w2 "$(cat /tmp/prompt.md)"`),
		tf.ToolUse(spawner, "s3", "2026-10-06T20:03:00.000Z", "/w/alpha", "Bash",
			`for d in /w/looped-one /w/looped-two; do herdr agent start w --cwd $d; herdr agent prompt w "$(cat $d/PROMPT.md)"; done`),
	))
	tf.WriteFile(t, e.projects, "-w-spawned-worker/"+spawned+".jsonl", tf.Join(
		tf.User(spawned, "p1", "2026-10-06T20:01:05.000Z", "/w/spawned-worker", "Implementiere das Feature aus dem Ticket und teste es"),
		tf.Assistant(spawned, "p2", "2026-10-06T20:01:30.000Z", "/w/spawned-worker", "On it."),
		tf.User(spawned, "p3", "2026-10-06T20:02:00.000Z", "/w/spawned-worker", "Danke, bitte noch die Doku"),
	))
	tf.WriteFile(t, e.projects, "-w-opened-by-hand/"+opened+".jsonl", tf.Join(
		tf.User(opened, "o1", "2026-10-06T20:01:05.000Z", "/w/opened-by-hand", "Ich oeffne das hier selbst und tippe sofort"),
	))
	tf.WriteFile(t, e.projects, "-w-looped-two/"+looped+".jsonl", tf.Join(
		tf.User(looped, "l1", "2026-10-06T20:03:02.000Z", "/w/looped-two", "Dritter Worker, Auftrag aus der Datei"),
	))
}

func TestIngestMarksSpawnedSessions(t *testing.T) {
	for _, order := range []string{"coordinator first", "workers first"} {
		t.Run(order, func(t *testing.T) {
			e := newEnv(t)
			writeSpawnTree(t, e)
			files := []string{"-w-alpha/" + spawner, "-w-spawned-worker/" + spawned, "-w-opened-by-hand/" + opened, "-w-looped-two/" + looped}
			if order == "workers first" {
				files = []string{files[1], files[2], files[3], files[0]}
			}
			for _, f := range files {
				run(t, e, e.projects+"/"+f+".jsonl")
			}
			actor := func(id string) string { return e.str(t, "SELECT actor FROM events WHERE id = 'claude:"+id+"'") }
			for id, want := range map[string]string{
				"p1": "automation", // spawned, prompt from a file
				"p3": "human",      // the person's own later input
				"o1": "human",      // opened by hand: no spawn command names it
				"l1": "automation", // loop over directories
				"s1": "human",
			} {
				if got := actor(id); got != want {
					t.Errorf("%s: actor %q, want %q", id, got, want)
				}
			}
		})
	}
}

func TestReclassifySpawned(t *testing.T) {
	e := newEnv(t)
	e.deps.Dispatch = dispatch.Config{Window: -1}
	writeSpawnTree(t, e)
	if _, err := All(ctx, e.deps); err != nil {
		t.Fatal(err)
	}
	e.deps.Dispatch = dispatch.Config{}
	sum, err := Reclassify(ctx, e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Dispatched != 2 || sum.Changed != 2 || sum.Transitions["human -> automation"] != 2 {
		t.Errorf("run: %+v", sum)
	}
	again, err := Reclassify(ctx, e.deps, false)
	if err != nil || again.Changed != 0 {
		t.Errorf("second run: %+v, %v", again, err)
	}
	if a := e.str(t, "SELECT actor FROM events WHERE id = 'claude:p3'"); a != "human" {
		t.Errorf("the later input is %q, want human", a)
	}
}

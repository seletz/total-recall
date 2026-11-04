package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestPoll: the first call is the baseline, commits of another connection
// are reported once, and polling leaves no read transaction behind that
// would block a WAL checkpoint.
func TestPoll(t *testing.T) {
	w, p := openTestStore(t)
	fillStore(t, w, "a", 2)
	r, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	poll := func(want Change) {
		t.Helper()
		got, err := r.Poll(ctx)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if got != want {
			t.Fatalf("Poll = %v, want %v", got, want)
		}
	}
	poll(Unchanged) // baseline
	poll(Unchanged)
	fillStore(t, w, "b", 1)
	poll(Modified)
	poll(Unchanged)
	evs, err := r.Events(ctx, "claude:b", true)
	if err != nil || len(evs) != 1 {
		t.Fatalf("Events after change = %d, %v; want 1", len(evs), err)
	}
	poll(Unchanged) // a read of the reader itself is no change
	if err := w.checkpointTruncate(ctx); err != nil {
		t.Fatalf("checkpoint with an idle poller: %v", err)
	}
}

// TestPollReplaced: a maintenance run swaps the file; Poll reports it,
// Reopen reads the new file and the next Poll starts a new baseline.
func TestPollReplaced(t *testing.T) {
	w, p := openTestStore(t)
	fillStore(t, w, "a", 3)
	r, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if _, err := r.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	res, err := Maintain(ctx, p, maintainOpts(filepath.Dir(p)))
	if err != nil || !res.Swapped {
		t.Fatalf("Maintain = %+v, %v", res, err)
	}
	got, err := r.Poll(ctx)
	if err != nil || got != Replaced {
		t.Fatalf("Poll after swap = %v, %v; want replaced", got, err)
	}
	if err := r.Reopen(); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	got, err = r.Poll(ctx)
	if err != nil || got != Unchanged {
		t.Fatalf("Poll after reopen = %v, %v; want unchanged", got, err)
	}
	evs, err := r.Events(ctx, "claude:a", true)
	if err != nil || len(evs) != 3 {
		t.Fatalf("Events after reopen = %d, %v; want 3", len(evs), err)
	}
	w2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w2.Close() })
	fillStore(t, w2, "c", 1)
	got, err = r.Poll(ctx)
	if err != nil || got != Modified {
		t.Fatalf("Poll after a write to the new file = %v, %v; want modified", got, err)
	}
	if _, _, err := r.Query(ctx, "INSERT INTO sessions(id, harness) VALUES ('x', 'y')"); err == nil {
		t.Error("the reopened store is writable")
	}
}

func TestReopenReadWrite(t *testing.T) {
	w, _ := openTestStore(t)
	if err := w.Reopen(); err == nil {
		t.Error("Reopen of a read-write store must fail")
	}
}

func TestPollMissingFile(t *testing.T) {
	_, p := openTestStore(t)
	r, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r2, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r2.Poll(ctx); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Poll on a missing file = %v, want a not-exist error", err)
	}
}

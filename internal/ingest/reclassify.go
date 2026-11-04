package ingest

import (
	"context"
	"time"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
)

// ReclassifySummary describes a Reclassify run.
type ReclassifySummary struct {
	Examined    int            // stored user messages looked at
	Changed     int            // messages whose actor differs under the current rules (written unless DryRun)
	Undecided   int            // messages the harness could not decide (no text rule matched, raw record missing)
	Transitions map[string]int // "<old> -> <new>" → count of changed messages
	DryRun      bool
	Duration    time.Duration
}

// Reclassify applies the current actor rules of every adapter that
// implements harness.Reclassifier to the stored user messages of its
// harness and updates the actors that differ. With dryRun it only counts.
// It is idempotent: a second run with the same rules changes nothing.
func Reclassify(ctx context.Context, d Deps, dryRun bool) (ReclassifySummary, error) {
	start := time.Now()
	sum := ReclassifySummary{Transitions: map[string]int{}, DryRun: dryRun}
	for _, a := range d.Adapters {
		rc, ok := a.(harness.Reclassifier)
		if !ok {
			continue
		}
		var changes []store.ActorChange
		err := d.Store.UserMessages(ctx, a.Name(), func(m store.UserMessage) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			sum.Examined++
			actor, ok := rc.Reclassify(toEvent(m), m.Subagent)
			if !ok {
				sum.Undecided++
				return nil
			}
			if actor != m.Actor {
				sum.Changed++
				sum.Transitions[string(m.Actor)+" -> "+string(actor)]++
				changes = append(changes, store.ActorChange{ID: m.ID, Actor: actor})
			}
			return nil
		})
		if err != nil {
			return sum, err
		}
		if dryRun || len(changes) == 0 {
			continue
		}
		if _, err := d.Store.SetEventActors(ctx, changes); err != nil {
			return sum, err
		}
	}
	sum.Duration = time.Since(start)
	return sum, nil
}

func toEvent(m store.UserMessage) model.Event {
	return model.Event{ID: m.ID, Actor: m.Actor, Text: m.Text, Raw: m.Raw}
}

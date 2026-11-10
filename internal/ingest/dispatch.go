package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/seletz/total-recall/internal/dispatch"
	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store"
)

// dispatchIndex indexes the tool calls of the archive from..to for the
// recognition of inputs another session typed in.
func dispatchIndex(ctx context.Context, d Deps, from, to time.Time) (*dispatch.Index, error) {
	if d.Dispatch.Window < 0 {
		return dispatch.NewIndex(nil, d.Dispatch), nil
	}
	calls, err := d.Store.ToolCalls(ctx, from, to)
	if err != nil {
		return nil, err
	}
	in := make([]dispatch.Call, len(calls))
	for i, c := range calls {
		in[i] = dispatch.Call{Session: c.SessionID, TS: c.TS, Text: c.Text}
	}
	return dispatch.NewIndex(in, d.Dispatch), nil
}

// markDispatched sets the actor automation (and no input kind) on the human
// inputs, within the time range this run wrote and the window after it, that
// another session sent. It looks both ways: a new input may match an old
// call, a new call an old input, as the order of the files is arbitrary.
func (r *runner) markDispatched(ctx context.Context) {
	d := r.deps
	if d.Dispatch.Window < 0 || r.hi.IsZero() {
		return
	}
	w := d.Dispatch.Window
	if w == 0 {
		w = dispatch.DefaultWindow
	}
	ix, err := dispatchIndex(ctx, d, r.lo.Add(-w), r.hi.Add(w))
	if err != nil {
		r.fail("dispatch", err)
		return
	}
	for _, a := range d.Adapters {
		var changes []store.ActorChange
		err := d.Store.UserMessagesBetween(ctx, a.Name(), r.lo, r.hi.Add(w), func(m store.UserMessage) error {
			if m.Actor == model.ActorHuman && ix.Sent(m.SessionID, m.TS, m.Text) {
				changes = append(changes, store.ActorChange{ID: m.ID, Actor: model.ActorAutomation})
			}
			return nil
		})
		if err == nil && len(changes) > 0 {
			var n int64
			n, err = d.Store.SetEventActors(ctx, changes)
			r.sum.Dispatched += int(n)
		}
		if err != nil {
			r.fail("dispatch", fmt.Errorf("%s: %w", a.Name(), err))
			return
		}
	}
}

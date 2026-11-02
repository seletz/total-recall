package tuitest

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Synchronous driving.
//
// Golden tests that wait for a program's output are racy: a frame may be
// captured before or after an asynchronous command lands. The helpers below
// avoid the program altogether. They call Update directly and run every
// returned command to completion on the spot, so after each step the model
// is in the state a program would reach once all its commands have run.

// Update handles one message and returns the command to run next. It is the
// step of whatever is under test: Run and Drive build it from a tea.Model;
// a component with a different Update signature is wrapped in a closure.
type Update func(tea.Msg) tea.Cmd

// Exec runs cmd to completion like a program would, but synchronously: a
// batch or sequence is unpacked in order, a raw terminal write (tea.RawMsg)
// is collected instead of delivered, every other message goes to update and
// the command it returns runs in turn. It returns the terminal writes in the
// order they happened. A nil cmd or a nil message does nothing.
func Exec(tb testing.TB, update Update, cmd tea.Cmd) []string {
	tb.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch m := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		return execAll(tb, update, m)
	case tea.RawMsg:
		s, ok := m.Msg.(string)
		if !ok {
			tb.Fatalf("tuitest.Exec: raw message of type %T", m.Msg)
		}
		return []string{s}
	}
	// tea.Sequence yields an unexported slice of commands.
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		cmds := make([]tea.Cmd, v.Len())
		for i := range cmds {
			cmds[i], _ = v.Index(i).Interface().(tea.Cmd)
		}
		return execAll(tb, update, cmds)
	}
	return Exec(tb, update, update(msg))
}

func execAll(tb testing.TB, update Update, cmds []tea.Cmd) []string {
	tb.Helper()
	var out []string
	for _, c := range cmds {
		out = append(out, Exec(tb, update, c)...)
	}
	return out
}

// Run executes cmd synchronously against m and feeds every message it
// produces back into m.Update, following the commands to the end: a load
// yields its result right away, a load whose result triggers another is
// followed, batches and sequences are unpacked, raw terminal writes are
// dropped. It returns the model after the last message.
func Run(tb testing.TB, m tea.Model, cmd tea.Cmd) tea.Model {
	tb.Helper()
	Exec(tb, func(msg tea.Msg) tea.Cmd {
		var next tea.Cmd
		m, next = m.Update(msg)
		return next
	}, cmd)
	return m
}

// Send hands msg to m.Update and runs the returned command to completion
// (see Run).
func Send(tb testing.TB, m tea.Model, msg tea.Msg) tea.Model {
	tb.Helper()
	m, cmd := m.Update(msg)
	return Run(tb, m, cmd)
}

// Drive starts m (Init, run to completion) and sends msgs in order, running
// the command of each to completion before the next one is sent (see Send).
// Key presses are built with Key or Keys. It returns the final model.
func Drive(tb testing.TB, m tea.Model, msgs ...tea.Msg) tea.Model {
	tb.Helper()
	m = Run(tb, m, m.Init())
	for _, msg := range msgs {
		m = Send(tb, m, msg)
	}
	return m
}

// Keys builds the key presses for names (see Key) as messages for Drive.
func Keys(names ...string) []tea.Msg {
	msgs := make([]tea.Msg, 0, len(names))
	for _, n := range names {
		msgs = append(msgs, Key(n))
	}
	return msgs
}

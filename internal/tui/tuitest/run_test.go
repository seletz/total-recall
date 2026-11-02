package tuitest_test

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// logModel records every message it handles. A "load" message answers with
// a "loaded" message, like a view whose load result triggers another load;
// a "write" message answers with a raw terminal write.
type logModel struct {
	log []string
}

func (m logModel) Init() tea.Cmd { return func() tea.Msg { return "init" } }

func (m logModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case string:
		m.log = append(m.log, msg)
		switch msg {
		case "load":
			return m, func() tea.Msg { return "loaded" }
		case "write":
			return m, func() tea.Msg { return tea.RawMsg{Msg: "\x1b[?25l"} }
		}
	case tea.KeyPressMsg:
		m.log = append(m.log, "key:"+msg.String())
	case tea.WindowSizeMsg:
		m.log = append(m.log, "size")
	}
	return m, nil
}

func (m logModel) View() tea.View { return tea.NewView(strings.Join(m.log, "\n")) }

// step adapts m to an Update and hands the log back through got.
func step(m *logModel) tuitest.Update {
	return func(msg tea.Msg) tea.Cmd {
		nm, cmd := m.Update(msg)
		*m = nm.(logModel)
		return cmd
	}
}

func msgCmd(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func TestExec(t *testing.T) {
	tests := []struct {
		name   string
		cmd    tea.Cmd
		log    []string
		writes []string
	}{
		{"nil command", nil, nil, nil},
		{"nil message", func() tea.Msg { return nil }, nil, nil},
		{"single message", msgCmd("a"), []string{"a"}, nil},
		{"follows the chain", msgCmd("load"), []string{"load", "loaded"}, nil},
		{"raw write collected", msgCmd(tea.RawMsg{Msg: "\x1b[2J"}), nil, []string{"\x1b[2J"}},
		{"write from an update", msgCmd("write"), []string{"write"}, []string{"\x1b[?25l"}},
		{"batch unpacked in order", tea.Batch(msgCmd("a"), nil, msgCmd("load"), msgCmd("b")),
			[]string{"a", "load", "loaded", "b"}, nil},
		{"sequence unpacked in order", tea.Sequence(msgCmd(tea.RawMsg{Msg: "1"}), msgCmd("load"), msgCmd(tea.RawMsg{Msg: "2"})),
			[]string{"load", "loaded"}, []string{"1", "2"}},
		{"nested batch and sequence", tea.Sequence(tea.Batch(msgCmd("a"), msgCmd("write")), msgCmd("b")),
			[]string{"a", "write", "b"}, []string{"\x1b[?25l"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &logModel{}
			writes := tuitest.Exec(t, step(m), tt.cmd)
			if !slices.Equal(m.log, tt.log) {
				t.Errorf("messages = %q, want %q", m.log, tt.log)
			}
			if !slices.Equal(writes, tt.writes) {
				t.Errorf("writes = %q, want %q", writes, tt.writes)
			}
		})
	}
}

func TestRun(t *testing.T) {
	m := tuitest.Run(t, logModel{}, tea.Batch(msgCmd("load"), msgCmd("write")))
	want := []string{"load", "loaded", "write"}
	if got := m.(logModel).log; !slices.Equal(got, want) {
		t.Errorf("messages = %q, want %q", got, want)
	}
	if m := tuitest.Run(t, logModel{log: []string{"x"}}, nil); !slices.Equal(m.(logModel).log, []string{"x"}) {
		t.Errorf("nil command changed the model: %q", m.(logModel).log)
	}
}

func TestSend(t *testing.T) {
	m := tuitest.Send(t, logModel{}, "load")
	want := []string{"load", "loaded"}
	if got := m.(logModel).log; !slices.Equal(got, want) {
		t.Errorf("messages = %q, want %q", got, want)
	}
}

func TestDrive(t *testing.T) {
	m := tuitest.Drive(t, logModel{}, tea.WindowSizeMsg{Width: 10, Height: 5}, tuitest.Key("q"), "load")
	want := []string{"init", "size", "key:q", "load", "loaded"}
	if got := m.(logModel).log; !slices.Equal(got, want) {
		t.Errorf("messages = %q, want %q", got, want)
	}
}

func TestKeys(t *testing.T) {
	got := tuitest.Keys("a", "enter")
	want := []tea.Msg{tuitest.Key("a"), tuitest.Key("enter")}
	if !slices.Equal(got, want) {
		t.Errorf("Keys = %v, want %v", got, want)
	}
	if got := tuitest.Keys(); len(got) != 0 {
		t.Errorf("Keys() = %v, want none", got)
	}
}

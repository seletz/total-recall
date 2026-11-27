package session

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/tui/tuitest"
)

// TestMemoryKey: m asks for the memory of the project of the session: the
// git root with its remote, else the working directory; without either it
// only says so. The app answers a project without memory with a Notice.
func TestMemoryKey(t *testing.T) {
	tests := []struct {
		name              string
		cwd, root, remote string
		want              OpenMemory
		wantNotice        string
		wantNoCmd         bool
	}{
		{"git root", "/w/kompass/sub", "/w/kompass", "git@example.org:k.git", OpenMemory{Key: "/w/kompass", Remote: "git@example.org:k.git"}, "", false},
		{"directory only", "/w/fahrtenbuch", "", "", OpenMemory{Key: "/w/fahrtenbuch", Dir: true}, "", false},
		{"no directory", "", "", "", OpenMemory{}, "no project for this session", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := openAttached(t, attachedStore(t, false))
			m.session.Cwd, m.session.GitRoot, m.session.GitRemoteURL = tt.cwd, tt.root, tt.remote
			next, cmd := m.Update(tuitest.Key("M"))
			if tt.wantNoCmd {
				if cmd != nil {
					t.Fatalf("command %v", cmd())
				}
				if got := ansi.Strip(next.(Model).View().Content); !strings.Contains(got, tt.wantNotice) {
					t.Errorf("no notice %q:\n%s", tt.wantNotice, got)
				}
				return
			}
			if cmd == nil {
				t.Fatal("no command")
			}
			if got, ok := cmd().(OpenMemory); !ok || got != tt.want {
				t.Errorf("M sends %+v, want %+v", got, tt.want)
			}
		})
	}
	m := openAttached(t, attachedStore(t, false))
	next, _ := m.Update(Notice{Text: "no memory for kompass"})
	if got := ansi.Strip(next.(Model).View().Content); !strings.Contains(got, "no memory for kompass") {
		t.Errorf("notice missing:\n%s", got)
	}
}

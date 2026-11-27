package demo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/testfixture"
)

// fileEdit is a backup copy of a file that the agent edits in a session:
// Claude Code copies the file to file-history/<session>/<name> before the
// edit and names the copy in a file-history-delta record.
type fileEdit struct {
	path    string // the edited file
	name    string // backup file name, <hash>@v<N>
	version int
	content string
	at      time.Time
}

// fileHistoryDir is the directory next to the projects directory where
// the demo keeps the backup copies, like $CLAUDE_CONFIG_DIR/file-history.
func fileHistoryDir(projects string) string {
	return filepath.Join(filepath.Dir(projects), "file-history")
}

// elevationBefore and elevationAfter are the two versions of the invented
// source file of the session "Höhenprofil prüfen": the first backup is the
// file before the fix, the second the one after the first edit.
const (
	elevationBefore = `package route

// Climb sums up the ascent of a profile in meters.
func Climb(profile []float64) float64 {
	var up float64
	for i := 1; i < len(profile); i++ {
		if d := profile[i] - profile[i-1]; d > 0 {
			up += d
		}
	}
	return up
}
`
	elevationAfter = `package route

// Climb sums up the ascent of a smoothed profile in meters: the raw
// altitude of the last segment jumps, so the series is smoothed first.
func Climb(profile []float64) float64 {
	smooth := smoothElevation(profile, 5)
	var up float64
	for i := 1; i < len(smooth); i++ {
		if d := smooth[i] - smooth[i-1]; d > 0 {
			up += d
		}
	}
	return up
}
`
)

// writeFileHistory writes the backup copies of the session's edits below
// the file-history directory and returns the delta records that name them.
func writeFileHistory(projects string, s session) ([]testfixture.Line, error) {
	var out []testfixture.Line
	for i, e := range s.edits {
		p := filepath.Join(fileHistoryDir(projects), s.id, e.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, fmt.Errorf("demo: %w", err)
		}
		if err := os.WriteFile(p, []byte(e.content), 0o644); err != nil {
			return nil, fmt.Errorf("demo: %w", err)
		}
		msg := fmt.Sprintf("%s-fh%d", strings.TrimSuffix(s.id, s.id[len(s.id)-4:]), i)
		out = append(out, testfixture.FileHistoryDelta(s.id, msg, stamp(e.at), e.path, e.name))
	}
	return out, nil
}

package demo

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// memoryFile is one invented file of the project memory of a demo
// project: Claude Code keeps them in <project dir>/memory next to the
// transcripts.
type memoryFile struct {
	path    string
	content string
	mtime   time.Time
}

// memoryOf are the memory files of the demo projects that have any. The
// text is invented, like everything of the demo; kompass has the files
// the search screenshot finds with Query.
var memoryOf = map[*project][]memoryFile{
	kompass: {
		{"MEMORY.md", `# Memory

- [Release process](release-process.md) — how a kompass release is cut and published
- [Conventions](conventions.md) — code style and test rules of the repository
`, at(time.October, 31, 22, 40)},
		{"release-process.md", `---
name: release-process
description: How a kompass release is cut and published
metadata:
  type: project
---

A release is a signed tag on ` + "`main`" + `. The release workflow then runs
**goreleaser**, which builds the binaries for darwin/arm64 and
linux/amd64 and publishes the Homebrew cask to the tap.

## Steps

1. Update the changelog under the next version.
2. Tag with ` + "`git tag -s vX.Y.Z`" + ` and push the tag.
3. Wait for the workflow; the cask lands in ` + "`mara/homebrew-tap`" + `.

**Why:** a missing ` + "`HOMEBREW_TAP_GITHUB_TOKEN`" + ` used to leave a
half-published release. The workflow now fails early.

**How to apply:** never publish the cask by hand; if the workflow fails,
fix the cause and re-run it.
`, at(time.October, 31, 22, 35)},
		{"conventions.md", `---
name: conventions
description: Code style and test rules of the kompass repository
metadata:
  type: feedback
---

- Tests are table-driven; fixtures are small and invented.
- ` + "`golangci-lint run ./...`" + ` must be green before a commit.
- Sort waypoints before comparing them: map iteration order is random.
`, at(time.October, 28, 9, 30)},
	},
	wetterwarte: {
		{"MEMORY.md", `# Memory

- [Sensors](sensors.md) — which sensors report what, and how they are smoothed
`, at(time.October, 14, 18, 5)},
		{"sensors.md", `---
name: sensors
description: Which sensors report what, and how they are smoothed
metadata:
  type: project
---

The temperature sensor reports every ten seconds; the chart smooths the
values over a five-minute window. The rain gauge counts tips, one tip is
0.2 mm. The MQTT client must reconnect after a broker restart.
`, at(time.October, 14, 18, 0)},
	},
	fahrtenbuch: {
		{"MEMORY.md", `# Memory

The odometer must never decrease from one trip to the next; check it when
a trip is saved.
`, at(time.September, 9, 20, 15)},
	},
}

// writeMemory writes the memory files of every project below the
// project directory of its working directory.
func writeMemory(projects string) error {
	for pr, files := range memoryOf {
		dir := filepath.Join(projects, projectDir(pr.cwd), "memory")
		for _, f := range files {
			p := filepath.Join(dir, filepath.FromSlash(f.path))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return fmt.Errorf("demo: %w", err)
			}
			if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
				return fmt.Errorf("demo: %w", err)
			}
			if err := os.Chtimes(p, f.mtime, f.mtime); err != nil {
				return fmt.Errorf("demo: %w", err)
			}
		}
	}
	return nil
}

package demo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seletz/total-recall/internal/ingest"
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
		{"conventions.md", memoryConventions, at(time.October, 28, 9, 30)},
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

// memoryStage is an earlier state of one project's memory: the complete
// set of files at the time a scan saw them (a file that is not listed is
// gone), so the archive has several versions and a deletion to show.
type memoryStage struct {
	observed time.Time
	files    []memoryFile
}

// memoryHistory are the earlier states of the memory of the demo projects,
// oldest first; the state in memoryOf comes after the last one. kompass
// has a release-process file that grew, an index that changed twice and a
// note that was dropped again.
var memoryHistory = map[*project][]memoryStage{
	kompass: {
		{at(time.October, 12, 22, 0), []memoryFile{
			{"MEMORY.md", `# Memory

- [Release process](release-process.md) — release steps
`, at(time.October, 12, 21, 50)},
			{"release-process.md", `---
name: release-process
description: How a kompass release is cut
metadata:
  type: project
---

A release is a tag on ` + "`main`" + `. The release workflow builds the
binaries and publishes them.
`, at(time.October, 12, 21, 45)},
		}},
		{at(time.October, 15, 18, 0), []memoryFile{
			{"MEMORY.md", `# Memory

- [Release process](release-process.md) — release steps
- [Old notes](old-notes.md) — scratch notes on the first route format
`, at(time.October, 15, 17, 58)},
			{"release-process.md", `---
name: release-process
description: How a kompass release is cut
metadata:
  type: project
---

A release is a tag on ` + "`main`" + `. The release workflow builds the
binaries and publishes them.
`, at(time.October, 12, 21, 45)},
			{"old-notes.md", `---
name: old-notes
description: Scratch notes on the first route format
metadata:
  type: project
---

Routes were stored as plain GPX. The planned switch to a compact binary
format is on hold.
`, at(time.October, 15, 17, 55)},
		}},
		{at(time.October, 24, 10, 0), []memoryFile{
			{"MEMORY.md", `# Memory

- [Release process](release-process.md) — release steps
- [Old notes](old-notes.md) — scratch notes on the first route format
`, at(time.October, 15, 17, 58)},
			{"release-process.md", `---
name: release-process
description: How a kompass release is cut and published
metadata:
  type: project
---

A release is a signed tag on ` + "`main`" + `. The release workflow then runs
goreleaser, which builds the binaries for darwin/arm64 and linux/amd64.

## Steps

1. Update the changelog under the next version.
2. Tag with ` + "`git tag -s vX.Y.Z`" + ` and push the tag.
`, at(time.October, 24, 9, 50)},
			{"old-notes.md", `---
name: old-notes
description: Scratch notes on the first route format
metadata:
  type: project
---

Routes were stored as plain GPX. The planned switch to a compact binary
format is on hold.
`, at(time.October, 15, 17, 55)},
		}},
		{at(time.October, 28, 10, 0), []memoryFile{
			{"MEMORY.md", `# Memory

- [Release process](release-process.md) — release steps
- [Conventions](conventions.md) — code style and test rules
`, at(time.October, 28, 9, 35)},
			{"release-process.md", `---
name: release-process
description: How a kompass release is cut and published
metadata:
  type: project
---

A release is a signed tag on ` + "`main`" + `. The release workflow then runs
goreleaser, which builds the binaries for darwin/arm64 and linux/amd64.

## Steps

1. Update the changelog under the next version.
2. Tag with ` + "`git tag -s vX.Y.Z`" + ` and push the tag.
`, at(time.October, 24, 9, 50)},
			{"conventions.md", memoryConventions, at(time.October, 28, 9, 30)},
		}},
	},
}

// memoryConventions is a text shared by the last stage and the final state,
// so the file has one version.
const memoryConventions = `---
name: conventions
description: Code style and test rules of the kompass repository
metadata:
  type: feedback
---

- Tests are table-driven; fixtures are small and invented.
- ` + "`golangci-lint run ./...`" + ` must be green before a commit.
- Sort waypoints before comparing them: map iteration order is random.
`

// replayMemory lets the archive see the earlier states of the memory: each
// stage replaces the memory directory of its project and is scanned at the
// time it names, set on the clock first. The final state is written again
// by the caller.
func replayMemory(ctx context.Context, projects string, deps ingest.Deps, clock *time.Time) error {
	for pr, stages := range memoryHistory {
		dir := filepath.Join(projects, projectDir(pr.cwd), "memory")
		for _, st := range stages {
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("demo: %w", err)
			}
			if err := writeMemoryFiles(dir, st.files); err != nil {
				return err
			}
			*clock = st.observed
			sum, err := ingest.Memory(ctx, deps)
			if err != nil {
				return fmt.Errorf("demo: memory: %w", err)
			}
			if len(sum.Errors) > 0 {
				return fmt.Errorf("demo: memory %s: %w", sum.Errors[0].Path, sum.Errors[0].Err)
			}
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("demo: %w", err)
		}
	}
	return nil
}

// writeMemory writes the memory files of every project below the
// project directory of its working directory.
func writeMemory(projects string) error {
	for pr, files := range memoryOf {
		if err := writeMemoryFiles(filepath.Join(projects, projectDir(pr.cwd), "memory"), files); err != nil {
			return err
		}
	}
	return nil
}

// writeMemoryFiles writes files below dir with their modification times.
func writeMemoryFiles(dir string, files []memoryFile) error {
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
	return nil
}

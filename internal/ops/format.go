package ops

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/ingest"
)

// FormatIngest renders the summary line of an ingest run, e.g.
// "scanned 3 files: 1 changed, 2 unchanged, 0 locked; 12 new events, 4
// meta records, 2 attachments (1.3 MiB), 1 sessions touched (0.2s)";
// rewriteRaw adds the rows whose raw line was replaced ("240 events and 75
// meta records raw rewritten"), attachments appear when some were stored,
// memory when the project memory changed.
func FormatIngest(s ingest.Summary, rewriteRaw bool) string {
	rewritten := ""
	if rewriteRaw {
		rewritten = fmt.Sprintf(", %d events and %d meta records raw rewritten", s.RawRewritten-s.MetaRewritten, s.MetaRewritten)
		if s.MetaKept > 0 {
			rewritten += fmt.Sprintf(" (%d meta records kept: they refer to data no longer on disk)", s.MetaKept)
		}
	}
	blobs := ""
	if s.Blobs > 0 {
		blobs = fmt.Sprintf(", %d attachments (%.1f MiB)", s.Blobs, float64(s.BlobBytes)/(1<<20))
	}
	memory := ""
	if s.MemoryVersions > 0 || s.MemoryDeleted > 0 {
		memory = fmt.Sprintf(", memory: %d new versions, %d deleted", s.MemoryVersions, s.MemoryDeleted)
	}
	return fmt.Sprintf("scanned %d files: %d changed, %d unchanged, %d locked; %d new events, %d meta records%s%s, %d sessions touched%s (%s)",
		s.Scanned, s.Changed, s.Unchanged, s.Locked, s.Events, s.Meta, rewritten, blobs, s.Sessions, memory, s.Duration.Round(time.Millisecond))
}

// FormatReclassify renders one summary line, e.g.
// "reindex --actors: 1234 user messages, 17 changed (human -> peer: 17), 3 undecided (0.4s)".
func FormatReclassify(s ingest.ReclassifySummary) string {
	var b strings.Builder
	b.WriteString("reindex --actors")
	if s.DryRun {
		b.WriteString(" (dry run)")
	}
	verb := "changed"
	if s.DryRun {
		verb = "would change"
	}
	fmt.Fprintf(&b, ": %d user messages, %d %s", s.Examined, s.Changed, verb)
	if len(s.Transitions) > 0 {
		keys := make([]string, 0, len(s.Transitions))
		for k := range s.Transitions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s: %d", k, s.Transitions[k]))
		}
		fmt.Fprintf(&b, " (%s)", strings.Join(parts, ", "))
	}
	if s.Undecided > 0 {
		fmt.Fprintf(&b, ", %d undecided (no raw record)", s.Undecided)
	}
	fmt.Fprintf(&b, " (%s)", s.Duration.Round(time.Millisecond))
	return b.String()
}

// FormatReport writes the readable form of a doctor report, as the
// command prints it.
func FormatReport(w io.Writer, r harness.FormatReport) {
	p := func(f string, a ...any) { _, _ = fmt.Fprintf(w, f, a...) }
	p("harness %s: %d sources, %d lines\n", r.Harness, r.Sources, r.Lines)
	vs := make([]string, 0, len(r.Versions))
	for i, v := range r.Versions {
		if i == 8 {
			vs = append(vs, fmt.Sprintf("… (%d more)", len(r.Versions)-i))
			break
		}
		vs = append(vs, v.Version+" ("+strconv.FormatInt(v.Count, 10)+")")
	}
	if len(vs) == 0 {
		vs = []string{"none"}
	}
	p("versions (newest first): %s\n", strings.Join(vs, ", "))
	section := func(title string, m map[string]*harness.Finding) {
		if len(m) == 0 {
			return
		}
		p("\n%s:\n", title)
		for _, k := range sortedKeys(m) {
			p("  %-32s %8d  first: %s\n", k, m[k].Count, m[k].Example)
		}
	}
	section("unknown record types", r.UnknownRecordTypes)
	section("unknown content block types", r.UnknownBlockTypes)
	if len(r.MissingFields) > 0 {
		flat := map[string]*harness.Finding{}
		for typ, fields := range r.MissingFields {
			for f, v := range fields {
				flat[typ+"."+f] = v
			}
		}
		section("missing fields (record type.field)", flat)
	}
	if r.Malformed.Count > 0 {
		p("\nmalformed lines: %d  first: %s\n", r.Malformed.Count, r.Malformed.Example)
	}
	if r.Clean() {
		p("no unknown record types, content blocks or missing fields\n")
	} else {
		p("\nfindings: the adapter may need an update (see docs: Hooks, \"Nach einem Claude-Code-Update\")\n")
	}
}

func sortedKeys(m map[string]*harness.Finding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

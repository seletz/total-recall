package harness

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// FormatChecker is implemented by adapters that can check their sources for
// format drift (`total-recall doctor`): record types, content blocks or
// fields the adapter does not know. It reads the sources only.
type FormatChecker interface {
	CheckFormat(ctx context.Context) (FormatReport, error)
}

// Finding counts occurrences of one unexpected thing and names the first
// place it was seen ("<path>:<line number>").
type Finding struct {
	Count   int64  `json:"count"`
	Example string `json:"example"`
}

// Add counts one occurrence at where; the first location is kept.
func (f *Finding) Add(where string) {
	if f.Count == 0 {
		f.Example = where
	}
	f.Count++
}

// VersionCount is how many lines a harness version wrote.
type VersionCount struct {
	Version string `json:"version"`
	Count   int64  `json:"count"`
}

// FormatReport is the result of a format check.
type FormatReport struct {
	Harness string `json:"harness"`
	Sources int    `json:"sources"`
	Lines   int64  `json:"lines"`
	// UnknownRecordTypes are record types neither ingested nor known to be skipped.
	UnknownRecordTypes map[string]*Finding `json:"unknown_record_types"`
	// UnknownBlockTypes are content block types the adapter does not know,
	// keyed "<record or container type>/<block type>".
	UnknownBlockTypes map[string]*Finding `json:"unknown_block_types"`
	// MissingFields maps record type -> expected field -> lines without it.
	MissingFields map[string]map[string]*Finding `json:"missing_fields"`
	// Malformed counts complete lines that are not JSON objects.
	Malformed Finding `json:"malformed"`
	// Versions are the harness versions seen, newest first.
	Versions []VersionCount `json:"versions"`
}

// NewFormatReport returns an empty report for harness name.
func NewFormatReport(name string) FormatReport {
	return FormatReport{Harness: name, UnknownRecordTypes: map[string]*Finding{},
		UnknownBlockTypes: map[string]*Finding{}, MissingFields: map[string]map[string]*Finding{},
		Versions: []VersionCount{}}
}

// Clean reports whether nothing unexpected was found.
func (r FormatReport) Clean() bool {
	return len(r.UnknownRecordTypes) == 0 && len(r.UnknownBlockTypes) == 0 &&
		len(r.MissingFields) == 0 && r.Malformed.Count == 0
}

// SetVersions stores counts sorted newest first (numeric dot-separated
// comparison, e.g. 2.1.10 > 2.1.9).
func (r *FormatReport) SetVersions(counts map[string]int64) {
	r.Versions = make([]VersionCount, 0, len(counts))
	for v, n := range counts {
		r.Versions = append(r.Versions, VersionCount{Version: v, Count: n})
	}
	sort.Slice(r.Versions, func(i, j int) bool { return CompareVersions(r.Versions[i].Version, r.Versions[j].Version) > 0 })
}

// CompareVersions compares dot-separated versions part by part, numerically
// where both parts are numbers, else as strings. It returns -1, 0 or 1.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil && nx != ny:
			if nx < ny {
				return -1
			}
			return 1
		case (ex != nil || ey != nil) && x != y:
			return strings.Compare(x, y)
		}
	}
	return 0
}

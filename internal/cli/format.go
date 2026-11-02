package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
)

// writeJSON writes v as indented JSON.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// jsonMarshal is json.Marshal without HTML escaping.
func jsonMarshal(v any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(b.String(), "\n")), nil
}

// localTime formats t in loc as "2006-01-02 15:04"; empty for the zero time.
func localTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

// isoTime formats t as UTC RFC 3339 with milliseconds; empty for zero.
func isoTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(store.TimeFormat)
}

// duration formats d compactly: "45s", "12m", "3h05m", "2d04h".
func duration(d time.Duration) string {
	switch {
	case d < 0:
		return ""
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
}

// age formats how long ago t was.
func age(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return duration(now.Sub(t)) + " ago"
}

// shortRepo returns "owner/repo" for a remote URL, else the base name of a path.
func shortRepo(repo string) string {
	if repo == "" {
		return ""
	}
	if u, err := url.Parse(repo); err == nil && u.Host != "" {
		p := strings.Trim(u.Path, "/")
		if p != "" {
			return p
		}
		return u.Host
	}
	return filepath.Base(repo)
}

// parseTimeArg parses a --since/--until value in loc: "YYYY-MM-DD",
// "YYYY-MM-DDTHH:MM", "YYYY-MM-DD HH:MM" or RFC 3339. A date-only --until
// (endOfDay) means the end of that day, i.e. the start of the next one.
func parseTimeArg(s string, loc *time.Location, endOfDay bool) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		if endOfDay {
			t = t.AddDate(0, 0, 1)
		}
		return t, nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, usagef("invalid time %q (use YYYY-MM-DD, YYYY-MM-DDTHH:MM or RFC 3339)", s)
}

// openRead opens the archive read-only and prints a schema warning to stderr.
// Its errors are of kind db.
func (e *env) openRead(cfg *config.Config) (*store.Store, error) {
	st, err := store.OpenReadOnly(cfg.DB, store.WithLocation(cfg.Location()))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errKind(ErrKindDB, fmt.Errorf("no archive at %s (run `total-recall ingest --all` to create it)", cfg.DB))
	}
	if err != nil {
		return nil, errKind(ErrKindDB, err)
	}
	if w := st.SchemaWarning(); w != "" {
		_, _ = fmt.Fprintln(e.app.Stderr, "warning:", w)
	}
	return st, nil
}

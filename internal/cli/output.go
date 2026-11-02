package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/store"
)

// This file is the output contract of the read commands (see
// docs/design.md, CLI): --format table|json|jsonl (sql also csv), --json as
// a shortcut, --no-truncate; every documented JSON key is always present
// and empty strings are null; errors in a JSON format are one JSON object on
// stdout; table cells are cut only on a terminal.

// Format is an output format of a read command.
type Format string

// The output formats. FormatTable is the human-readable default (a table
// or text), FormatJSON one indented document, FormatJSONL one compact JSON
// object per line, FormatCSV comma-separated values (sql only).
const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatCSV   Format = "csv"
)

// IsJSON reports whether f is json or jsonl.
func (f Format) IsJSON() bool { return f == FormatJSON || f == FormatJSONL }

// NullString is a string in JSON output whose empty value is null, so that
// every key is present and "no value" is distinguishable from "".
type NullString string

// MarshalJSON writes null for the empty string.
func (s NullString) MarshalJSON() ([]byte, error) {
	if s == "" {
		return []byte("null"), nil
	}
	return jsonMarshal(string(s))
}

// UnmarshalJSON accepts null and strings.
func (s *NullString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*s = ""
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*s = NullString(v)
	return nil
}

// ErrorJSON is the error object a read command writes to stdout in a JSON
// format: {"error":{"kind":"…","message":"…"}}.
type ErrorJSON struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody is the content of ErrorJSON. Kind is one of the ErrKind
// constants, Message the text that also goes to stderr.
type ErrorBody struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// The error kinds. They are stable; scripts may switch on them.
const (
	ErrKindUsage     = "usage"     // flags, arguments, their combinations
	ErrKindConfig    = "config"    // the configuration could not be loaded
	ErrKindDB        = "db"        // the archive could not be opened
	ErrKindSQL       = "sql"       // SQLite rejected the query (sql, search --raw)
	ErrKindNotFound  = "not_found" // the referenced session does not exist
	ErrKindAmbiguous = "ambiguous" // the reference matches more than one session
	ErrKindInternal  = "internal"  // everything else
)

// kindError carries the error kind of the JSON error object.
type kindError struct {
	kind string
	err  error
}

func (e kindError) Error() string { return e.err.Error() }
func (e kindError) Unwrap() error { return e.err }

// errKind tags err with a kind; nil stays nil.
func errKind(kind string, err error) error {
	if err == nil {
		return nil
	}
	return kindError{kind: kind, err: err}
}

// usage tags err as a usage error.
func usage(err error) error { return errKind(ErrKindUsage, err) }

// usagef formats a usage error.
func usagef(format string, a ...any) error { return usage(fmt.Errorf(format, a...)) }

// errorKind classifies err for the JSON error object.
func errorKind(err error) string {
	var ke kindError
	switch {
	case errors.As(err, &ke):
		return ke.kind
	case errors.Is(err, store.ErrNotFound):
		return ErrKindNotFound
	case errors.Is(err, store.ErrAmbiguous):
		return ErrKindAmbiguous
	}
	return ErrKindInternal
}

// writeJSONError writes the error object for err as one line.
func writeJSONError(w io.Writer, err error) {
	b, _ := jsonMarshal(ErrorJSON{Error: ErrorBody{Kind: errorKind(err), Message: err.Error()}})
	_, _ = fmt.Fprintln(w, string(b))
}

// jsonRequested reports whether args ask for a JSON format (--json,
// --format json|jsonl). It is the fallback for errors raised before a
// command resolved its flags, e.g. an unknown flag.
func jsonRequested(args []string) bool {
	for i, a := range args {
		switch {
		case a == "--":
			return false
		case a == "--json" || a == "--json=true" || a == "--format=json" || a == "--format=jsonl":
			return true
		case a == "--format" && i+1 < len(args) && (args[i+1] == "json" || args[i+1] == "jsonl"):
			return true
		}
	}
	return false
}

// usageArgs makes the errors of a cobra argument validator usage errors.
func usageArgs(p cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return usage(p(cmd, args)) }
}

// outputFlags are the output flags of a read command: --format, --json
// and --no-truncate.
type outputFlags struct {
	format     string
	json       bool
	noTruncate bool
	formats    []Format
}

// register adds the flags to c. Every read command accepts table, json and
// jsonl; extra formats (csv) are added by the command.
func (f *outputFlags) register(c *cobra.Command, extra ...Format) {
	f.formats = append([]Format{FormatTable, FormatJSON, FormatJSONL}, extra...)
	names := make([]string, len(f.formats))
	for i, x := range f.formats {
		names[i] = string(x)
	}
	fl := c.Flags()
	fl.StringVar(&f.format, "format", string(FormatTable), "output format: "+strings.Join(names, ", "))
	fl.BoolVar(&f.json, "json", false, "shortcut for --format json")
	fl.BoolVar(&f.noTruncate, "no-truncate", false, "never cut table cells to the terminal width")
}

// resolve returns the requested format; --json wins over --format.
func (f *outputFlags) resolve() (Format, error) {
	if f.json {
		return FormatJSON, nil
	}
	for _, x := range f.formats {
		if string(x) == f.format {
			return x, nil
		}
	}
	names := make([]string, len(f.formats))
	for i, x := range f.formats {
		names[i] = string(x)
	}
	return "", usagef("invalid --format %q (%s)", f.format, strings.Join(names, ", "))
}

// output writes the result of one read command in the resolved format.
type output struct {
	w      io.Writer
	errw   io.Writer
	format Format
	// width is the terminal width table cells are cut to; 0 never cuts
	// (stdout is not a terminal, or --no-truncate).
	width     int
	truncated int // cells cut so far; reported by finish
}

// readRun wraps the RunE of a read command: it resolves the output flags,
// records that errors must be JSON, runs fn and prints the truncation hint.
func (e *env) readRun(f *outputFlags, fn func(cmd *cobra.Command, args []string, o *output) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		format, err := f.resolve()
		if err != nil {
			return err
		}
		e.jsonErrors = format.IsJSON()
		o := &output{w: cmd.OutOrStdout(), errw: cmd.ErrOrStderr(), format: format}
		if format == FormatTable && !f.noTruncate {
			o.width = e.app.TerminalWidth()
		}
		if err := fn(cmd, args, o); err != nil {
			return err
		}
		o.finish()
		return nil
	}
}

// terminalWidth returns the width of the terminal w is attached to, or 0
// when w is not a terminal. COLUMNS overrides a size the terminal does not
// report.
func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return 0
	}
	if cols, _, err := term.GetSize(f.Fd()); err == nil && cols > 0 {
		return cols
	}
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		return n
	}
	return 80
}

// object writes one value: indented with json, one compact line with jsonl.
func (o *output) object(v any) error {
	if o.format == FormatJSONL {
		b, err := jsonMarshal(v)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(o.w, string(b))
		return err
	}
	return writeJSON(o.w, v)
}

// writeList writes items as a JSON array (json) or one object per line
// (jsonl), item by item, so a consumer can stream jsonl.
func writeList[T any](o *output, items []T) error {
	if o.format == FormatJSONL {
		for _, it := range items {
			if err := o.object(it); err != nil {
				return err
			}
		}
		return nil
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(o.w, "[]")
		return err
	}
	if _, err := io.WriteString(o.w, "[\n"); err != nil {
		return err
	}
	for i, it := range items {
		b, err := jsonIndent(it, "  ")
		if err != nil {
			return err
		}
		sep := ",\n"
		if i == len(items)-1 {
			sep = "\n"
		}
		if _, err := io.WriteString(o.w, "  "+string(b)+sep); err != nil {
			return err
		}
	}
	_, err := io.WriteString(o.w, "]\n")
	return err
}

// jsonIndent marshals v indented by two spaces, every line but the first
// prefixed with prefix, without a trailing newline and HTML escaping.
func jsonIndent(v any, prefix string) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetIndent(prefix, "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(b.String(), "\n")), nil
}

// table writes rows aligned in columns, one line per row. Cells with line
// breaks or tabs are collapsed to one line. On a terminal the columns are
// fitted to its width (fitColumns); a cut cell ends with "…".
func (o *output) table(header []string, rows [][]string) error {
	n := len(header)
	for _, r := range rows {
		n = max(n, len(r))
	}
	widths := make([]int, n)
	for i, h := range header {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range rows {
		for i := range r {
			r[i] = oneLine(r[i])
			widths[i] = max(widths[i], utf8.RuneCountInString(r[i]))
		}
	}
	if o.width > 0 {
		fitted := fitColumns(append([]int(nil), widths...), o.width)
		for _, r := range rows {
			for i := range r {
				if utf8.RuneCountInString(r[i]) > fitted[i] {
					r[i] = cut(r[i], fitted[i])
					o.truncated++
				}
			}
		}
	}
	return table(o.w, header, rows)
}

// cell returns s on one line, cut to limit runes on a terminal (0 = no
// limit beyond the terminal width). It is for text output that is no table.
func (o *output) cell(s string, limit int) string {
	s = oneLine(s)
	if o.width == 0 {
		return s
	}
	limit = min(limit, o.width)
	if limit <= 0 {
		limit = o.width
	}
	if utf8.RuneCountInString(s) > limit {
		o.truncated++
		return cut(s, limit)
	}
	return s
}

// finish prints the truncation hint to stderr.
func (o *output) finish() {
	if o.truncated == 0 {
		return
	}
	_, _ = fmt.Fprintf(o.errw, "%s truncated, --no-truncate for full values\n", plural(o.truncated, "cell"))
}

// oneLine collapses the whitespace of s to single spaces when it spans
// lines or contains tabs; other strings are returned unchanged.
func oneLine(s string) string {
	if strings.ContainsAny(s, "\n\r\t") {
		return strings.Join(strings.Fields(s), " ")
	}
	return s
}

// cut shortens s to n runes, the last one "…".
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// tablePadding is the space tabwriter puts between columns.
const tablePadding = 2

// minColumnWidth is how narrow fitColumns makes a column at most.
const minColumnWidth = 4

// fitColumns shrinks the widest columns until the row fits limit (0 = no
// limit): each step takes one rune from the widest column, so wide columns
// share the space evenly and narrow ones keep their width. Columns are not
// made narrower than minColumnWidth; the result may then still exceed the
// limit. widths is modified and returned.
func fitColumns(widths []int, limit int) []int {
	if limit <= 0 || len(widths) == 0 {
		return widths
	}
	total := func() int {
		t := tablePadding * (len(widths) - 1)
		for _, w := range widths {
			t += w
		}
		return t
	}
	for total() > limit {
		widest := -1
		for i, w := range widths {
			if w > minColumnWidth && (widest < 0 || w > widths[widest]) {
				widest = i
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
	}
	return widths
}

// table writes rows aligned in columns without any cutting.
func table(w io.Writer, header []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, tablePadding, ' ', 0)
	if len(header) > 0 {
		_, _ = fmt.Fprintln(tw, strings.Join(header, "\t"))
	}
	for _, r := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}

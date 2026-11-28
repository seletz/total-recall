package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// This file follows the output contract of total-recall's read commands
// (docs/explanation/design.md, "Ausgabevertrag der Lesebefehle"): --json
// writes one indented document, every documented key is always present, and
// an error in JSON mode is one object {"error":{"kind":…,"message":…}} on
// stdout, with the message also on stderr and exit code 1.

// The error kinds of the JSON error object. They are stable.
const (
	ErrKindUsage    = "usage"    // flags, arguments, their combinations
	ErrKindDB       = "db"       // a database or archive could not be opened
	ErrKindSQL      = "sql"      // DuckDB rejected a query
	ErrKindInternal = "internal" // everything else
)

// ErrorJSON is the error object of the JSON output.
type ErrorJSON struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody is the content of ErrorJSON.
type ErrorBody struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

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

func usagef(format string, a ...any) error {
	return errKind(ErrKindUsage, fmt.Errorf(format, a...))
}

func errorKind(err error) string {
	var ke kindError
	if errors.As(err, &ke) {
		return ke.kind
	}
	return ErrKindInternal
}

func writeJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

func writeJSONError(w io.Writer, err error) {
	b, _ := json.Marshal(ErrorJSON{Error: ErrorBody{Kind: errorKind(err), Message: err.Error()}})
	_, _ = fmt.Fprintln(w, string(b))
}

// jsonRequested reports whether args ask for JSON output; the fallback for
// errors raised before a command parsed its flags.
func jsonRequested(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "--json=true" {
			return true
		}
	}
	return false
}

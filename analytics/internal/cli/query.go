package cli

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/analytics/internal/duck"
)

// The formats of query.
const (
	formatTable = "table"
	formatJSON  = "json"
	formatJSONL = "jsonl"
	formatCSV   = "csv"
)

func newQueryCmd(a *App) *cobra.Command {
	var asJSON bool
	var format string
	cmd := &cobra.Command{
		Use:   "query SQL",
		Short: "Run read-only SQL against the analytics database",
		Long: `Runs one SQL query against the analytics database, opened read-only and
without access to files: the views analytics_*_v1 are the interface (see
views). "-" reads the query from stdin.`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usagef("query takes one SQL argument, got %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON {
				format = formatJSON
			}
			switch format {
			case formatTable, formatJSON, formatJSONL, formatCSV:
			default:
				return usagef("--format %s: want table, json, jsonl or csv", format)
			}
			q := args[0]
			if q == "-" {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				q = string(b)
			}
			ctx := cmd.Context()
			cfg, err := a.config()
			if err != nil {
				return err
			}
			db, _, err := a.openAnalytics(ctx, cfg, true)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			conn, err := lockedConn(ctx, db)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			rows, err := conn.QueryContext(ctx, q)
			if err != nil {
				return errKind(ErrKindSQL, err)
			}
			defer func() { _ = rows.Close() }()
			return writeRows(a.Stdout, rows, format)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&asJSON, "json", false, "shortcut for --format json")
	f.StringVar(&format, "format", formatTable, "table, json, jsonl or csv")
	return cmd
}

// lockedConn returns a connection without file access whose configuration
// cannot be changed: a query reads the analytics database and nothing else.
func lockedConn(ctx context.Context, db *duck.DB) (*sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, errKind(ErrKindDB, err)
	}
	for _, s := range []string{"SET enable_external_access = false", "SET lock_configuration = true"} {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			_ = conn.Close()
			return nil, errKind(ErrKindDB, err)
		}
	}
	return conn, nil
}

// writeRows writes a result set in a format. JSON keeps numbers as numbers,
// dates as YYYY-MM-DD and timestamps as RFC 3339 (UTC); NULL is null.
func writeRows(w io.Writer, rows *sql.Rows, format string) error {
	cols, err := rows.Columns()
	if err != nil {
		return errKind(ErrKindSQL, err)
	}
	types, _ := rows.ColumnTypes()
	var table [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return errKind(ErrKindSQL, err)
		}
		for i, v := range vals {
			vals[i] = jsonValue(v, types[i].DatabaseTypeName())
		}
		if format == formatJSONL {
			if err := writeJSONLine(w, cols, vals); err != nil {
				return err
			}
			continue
		}
		table = append(table, vals)
	}
	if err := rows.Err(); err != nil {
		return errKind(ErrKindSQL, err)
	}
	switch format {
	case formatJSON:
		out := make([]orderedRow, len(table))
		for i, r := range table {
			out[i] = orderedRow{cols: cols, vals: r}
		}
		return writeJSON(w, out)
	case formatCSV:
		cw := csv.NewWriter(w)
		_ = cw.Write(cols)
		for _, r := range table {
			rec := make([]string, len(r))
			for i, v := range r {
				rec[i] = cell(v)
			}
			_ = cw.Write(rec)
		}
		cw.Flush()
		return cw.Error()
	case formatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, strings.Join(cols, "\t"))
		for _, r := range table {
			cells := make([]string, len(r))
			for i, v := range r {
				cells[i] = strings.Join(strings.Fields(cell(v)), " ")
			}
			_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
		}
		return tw.Flush()
	}
	return nil
}

// orderedRow marshals as a JSON object with the columns in query order.
type orderedRow struct {
	cols []string
	vals []any
}

func (r orderedRow) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, c := range r.cols {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(c)
		v, err := json.Marshal(r.vals[i])
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func writeJSONLine(w io.Writer, cols []string, vals []any) error {
	b, err := json.Marshal(orderedRow{cols: cols, vals: vals})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// jsonValue normalises a scanned DuckDB value for output.
func jsonValue(v any, dbType string) any {
	switch x := v.(type) {
	case time.Time:
		if dbType == "DATE" {
			return x.Format(time.DateOnly)
		}
		return x.UTC().Format(time.RFC3339Nano)
	case *big.Int:
		return json.Number(x.String())
	case []byte:
		return string(x)
	}
	return v
}

// cell renders a value for table and CSV output; NULL is empty.
func cell(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// Package duck opens the DuckDB database of total-recall-analytics and
// attaches total-recall archives (SQLite files) to it.
//
// DuckDB reads SQLite through its sqlite_scanner extension. The binary links
// a DuckDB built from source with that extension compiled in
// (scripts/analytics-duckdb.sh, build tag duckdb_use_static_lib, cgo_*.go)
// and without the capability to load extensions at run time: there is no
// dlopen, no download and no extension file. The configuration below also
// switches off automatic installing and loading.
package duck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// SQLiteExtension is the name of DuckDB's SQLite extension.
const SQLiteExtension = "sqlite_scanner"

// settings are DuckDB configuration options applied when the database is
// created: no extension is installed or loaded implicitly.
var settings = [][2]string{
	{"autoinstall_known_extensions", "false"},
	{"autoload_known_extensions", "false"},
	{"allow_community_extensions", "false"},
	{"allow_unsigned_extensions", "false"},
}

// Options configure Open.
type Options struct {
	// Path is the DuckDB database file; empty means an in-memory database.
	Path string
	// ReadOnly opens Path read-only.
	ReadOnly bool
}

// DB is an open DuckDB database.
type DB struct {
	*sql.DB
	connector *duckdb.Connector
}

// ErrNotStatic means the binary was built without the build tag
// duckdb_use_static_lib and so without the sqlite extension.
var ErrNotStatic = errors.New("built without the static DuckDB that contains the sqlite extension (scripts/analytics-duckdb.sh, then go build -tags duckdb_use_static_lib)")

// Open opens (or creates) the DuckDB database.
func Open(ctx context.Context, o Options) (*DB, error) {
	if !staticBuild {
		return nil, ErrNotStatic
	}
	q := url.Values{}
	for _, s := range settings {
		q.Set(s[0], s[1])
	}
	if o.ReadOnly {
		q.Set("access_mode", "READ_ONLY")
	}
	connector, err := duckdb.NewConnector(o.Path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("open duckdb %q: %w", o.Path, err)
	}
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		_ = connector.Close()
		return nil, fmt.Errorf("open duckdb %q: %w", o.Path, err)
	}
	return &DB{DB: db, connector: connector}, nil
}

// Close closes the database and the underlying DuckDB instance.
func (d *DB) Close() error {
	err := d.DB.Close()
	if cerr := d.connector.Close(); err == nil {
		err = cerr
	}
	return err
}

var aliasRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// AttachSQLite attaches the SQLite file at path read-only under alias. The
// alias must be a plain identifier.
func (d *DB) AttachSQLite(ctx context.Context, path, alias string) error {
	if !aliasRE.MatchString(alias) {
		return fmt.Errorf("invalid alias %q: letters, digits and _ only", alias)
	}
	q := "ATTACH " + quoteString(path) + " AS " + alias + " (TYPE sqlite, READ_ONLY)"
	if _, err := d.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("attach %s: %w", path, err)
	}
	return nil
}

// Info describes the linked DuckDB and its extensions.
type Info struct {
	DuckDBVersion string
	SourceID      string
	Platform      string
	Extensions    []Extension
}

// Extension is a loaded DuckDB extension. Static means compiled into the
// binary.
type Extension struct {
	Name    string
	Version string
	Static  bool
}

// Info reads the DuckDB version, platform and loaded extensions.
func (d *DB) Info(ctx context.Context) (Info, error) {
	var i Info
	if err := d.QueryRowContext(ctx, "SELECT library_version, source_id, (SELECT platform FROM pragma_platform()) FROM pragma_version()").Scan(&i.DuckDBVersion, &i.SourceID, &i.Platform); err != nil {
		return i, err
	}
	rows, err := d.QueryContext(ctx, "SELECT extension_name, coalesce(extension_version, ''), install_mode = 'STATICALLY_LINKED' FROM duckdb_extensions() WHERE loaded ORDER BY 1")
	if err != nil {
		return i, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e Extension
		if err := rows.Scan(&e.Name, &e.Version, &e.Static); err != nil {
			return i, err
		}
		i.Extensions = append(i.Extensions, e)
	}
	return i, rows.Err()
}

// quoteString quotes s as an SQL string literal.
func quoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

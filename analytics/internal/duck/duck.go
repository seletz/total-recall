// Package duck opens the DuckDB database of total-recall-analytics and
// attaches total-recall archives (SQLite files) to it.
//
// DuckDB reads SQLite through its sqlite_scanner extension, which is not
// part of the linked library. The binary embeds the extension for its
// platform (ext_<os>_<arch>.go, pinned in ext.lock), writes it once to a cache
// directory and loads it from there. Automatic installation and loading of
// extensions is switched off, so DuckDB never fetches anything from the
// network.
package duck

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// SQLiteExtension is the name of DuckDB's SQLite extension.
const SQLiteExtension = "sqlite_scanner"

// settings are DuckDB configuration options applied when the database is
// created. They keep DuckDB offline: no extension is installed or loaded
// implicitly, only signed ones are accepted.
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
	// ExtDir is the directory the embedded extension is written to; empty
	// means DefaultExtDir.
	ExtDir string
	// ReadOnly opens Path read-only.
	ReadOnly bool
}

// DB is an open DuckDB database with the SQLite extension loaded.
type DB struct {
	*sql.DB
	connector *duckdb.Connector
	extPath   string
}

// ErrNoExtension means the binary was built without the embedded extension
// for its platform.
var ErrNoExtension = errors.New("built without the embedded DuckDB sqlite extension for this platform (run scripts/analytics-ext.sh before go build)")

// DefaultExtDir is the cache directory of the extension:
// <user cache dir>/total-recall-analytics/duckdb-<version>/<platform>.
func DefaultExtDir() (string, error) {
	lock, err := ParseLock(lockFile)
	if err != nil {
		return "", err
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache directory: %w", err)
	}
	return filepath.Join(base, "total-recall-analytics", "duckdb-"+lock.DuckDB, Platform), nil
}

// Open opens (or creates) the DuckDB database and loads the embedded SQLite
// extension on every connection.
func Open(ctx context.Context, o Options) (*DB, error) {
	if len(sqliteExtGz) == 0 {
		return nil, ErrNoExtension
	}
	lock, err := ParseLock(lockFile)
	if err != nil {
		return nil, err
	}
	entry, ok := lock.Find(SQLiteExtension, Platform)
	if !ok {
		return nil, fmt.Errorf("ext.lock has no %s for %s", SQLiteExtension, Platform)
	}
	dir := o.ExtDir
	if dir == "" {
		if dir, err = DefaultExtDir(); err != nil {
			return nil, err
		}
	}
	extPath, err := materialize(dir, entry)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, s := range settings {
		q.Set(s[0], s[1])
	}
	// DuckDB looks for extensions only in the cache directory, never in a
	// ~/.duckdb a DuckDB CLI may have filled.
	q.Set("extension_directory", dir)
	if o.ReadOnly {
		q.Set("access_mode", "READ_ONLY")
	}
	dsn := o.Path + "?" + q.Encode()
	load := "LOAD " + quoteString(extPath)
	connector, err := duckdb.NewConnector(dsn, func(execer driver.ExecerContext) error {
		_, err := execer.ExecContext(ctx, load, nil)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("open duckdb %q: %w", o.Path, err)
	}
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		_ = connector.Close()
		return nil, fmt.Errorf("open duckdb %q: %w", o.Path, err)
	}
	return &DB{DB: db, connector: connector, extPath: extPath}, nil
}

// Close closes the database and the underlying DuckDB instance.
func (d *DB) Close() error {
	err := d.DB.Close()
	if cerr := d.connector.Close(); err == nil {
		err = cerr
	}
	return err
}

// ExtensionPath is the file the SQLite extension was loaded from.
func (d *DB) ExtensionPath() string { return d.extPath }

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

// Info describes the linked DuckDB and the loaded extensions.
type Info struct {
	DuckDBVersion string
	Platform      string
	Extensions    []Extension
}

// Extension is a loaded DuckDB extension. Embedded means the binary carries
// it and loaded it from Path, its cache file.
type Extension struct {
	Name     string
	Version  string
	Path     string
	Embedded bool
}

// Info reads the DuckDB version, platform and loaded extensions.
func (d *DB) Info(ctx context.Context) (Info, error) {
	var i Info
	if err := d.QueryRowContext(ctx, "SELECT version(), (SELECT platform FROM pragma_platform())").Scan(&i.DuckDBVersion, &i.Platform); err != nil {
		return i, err
	}
	rows, err := d.QueryContext(ctx, "SELECT extension_name, coalesce(extension_version, '') FROM duckdb_extensions() WHERE loaded AND install_mode <> 'STATICALLY_LINKED' ORDER BY 1")
	if err != nil {
		return i, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e Extension
		if err := rows.Scan(&e.Name, &e.Version); err != nil {
			return i, err
		}
		if e.Name == SQLiteExtension {
			e.Path, e.Embedded = d.extPath, true
		}
		i.Extensions = append(i.Extensions, e)
	}
	return i, rows.Err()
}

// quoteString quotes s as an SQL string literal.
func quoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

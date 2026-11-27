package star

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// This file is the model runner: it builds the models of the project
// directory the way dbt would, for the subset of dbt the models may use.
//
//   - {{ config(materialized='view'|'table'|'incremental', unique_key='col') }}
//     view (default): CREATE OR REPLACE VIEW; table: CREATE OR REPLACE TABLE;
//     incremental: CREATE TABLE on the first build, afterwards delete+insert
//     on unique_key (dbt's delete+insert strategy), so the table only grows
//     by the keys a run selects and never loses others.
//   - {{ ref('model') }}  another model (or a seed); defines the build order.
//   - {{ source('tr', 'table') }}  a table of the attached archive.
//   - {{ var('name') }}  a run variable (dbt: --vars).
//
// Anything else in braces (macros, {% %} control flow, comments) is an error
// rather than silently wrong SQL. Data tests (tests/*.sql) are singular dbt
// tests: a query that returns the rows violating the test.

var (
	reConfig  = regexp.MustCompile(`(?s)\{\{\s*config\((.*?)\)\s*\}\}`)
	reConfArg = regexp.MustCompile(`(\w+)\s*=\s*['"]([^'"]*)['"]`)
	reRef     = regexp.MustCompile(`\{\{\s*ref\(\s*['"](\w+)['"]\s*\)\s*\}\}`)
	reSource  = regexp.MustCompile(`\{\{\s*source\(\s*['"](\w+)['"]\s*,\s*['"](\w+)['"]\s*\)\s*\}\}`)
	reVar     = regexp.MustCompile(`\{\{\s*var\(\s*['"](\w+)['"]\s*\)\s*\}\}`)
	reJinja   = regexp.MustCompile(`\{\{|\{%|\{#`)
)

// Model is one model file.
type Model struct {
	Name         string
	Path         string
	Materialized string // view, table or incremental
	UniqueKey    string // incremental only
	Refs         []string
	Text         string
}

// Project is the parsed project: models in build order and data tests.
type Project struct {
	Models []Model
	Tests  []Model
}

// RenderContext resolves source() and var().
type RenderContext struct {
	// Sources maps a source name to the schema (attached database) of its
	// tables.
	Sources map[string]string
	Vars    map[string]string
}

// ParseModel reads the config and the refs of a model.
func ParseModel(name, text string) (Model, error) {
	m := Model{Name: name, Materialized: "view", Text: text}
	if c := reConfig.FindStringSubmatch(text); c != nil {
		for _, a := range reConfArg.FindAllStringSubmatch(c[1], -1) {
			switch a[1] {
			case "materialized":
				m.Materialized = a[2]
			case "unique_key":
				m.UniqueKey = a[2]
			default:
				return m, fmt.Errorf("model %s: config %s is not supported", name, a[1])
			}
		}
	}
	switch m.Materialized {
	case "view", "table":
	case "incremental":
		if m.UniqueKey == "" {
			return m, fmt.Errorf("model %s: incremental needs unique_key", name)
		}
	default:
		return m, fmt.Errorf("model %s: materialized=%q is not supported", name, m.Materialized)
	}
	seen := map[string]bool{}
	for _, r := range reRef.FindAllStringSubmatch(text, -1) {
		if !seen[r[1]] {
			seen[r[1]] = true
			m.Refs = append(m.Refs, r[1])
		}
	}
	return m, nil
}

// Render turns a model into SQL for DuckDB.
func Render(text string, c RenderContext) (string, error) {
	out := reConfig.ReplaceAllString(text, "")
	out = reRef.ReplaceAllString(out, "$1")
	var errs []string
	out = reSource.ReplaceAllStringFunc(out, func(s string) string {
		g := reSource.FindStringSubmatch(s)
		schema, ok := c.Sources[g[1]]
		if !ok {
			errs = append(errs, "unknown source "+g[1])
		}
		return schema + "." + g[2]
	})
	out = reVar.ReplaceAllStringFunc(out, func(s string) string {
		name := reVar.FindStringSubmatch(s)[1]
		v, ok := c.Vars[name]
		if !ok {
			errs = append(errs, "unknown var "+name)
		}
		return v
	})
	if len(errs) > 0 {
		return "", fmt.Errorf("%s", strings.Join(errs, ", "))
	}
	if loc := reJinja.FindStringIndex(out); loc != nil {
		end := min(loc[0]+40, len(out))
		return "", fmt.Errorf("dbt syntax outside the supported subset: %q", out[loc[0]:end])
	}
	return out, nil
}

// Seed materialization: a table the caller loads (dbt: a seed), placed in the
// build order after the models it is computed from.
const Seed = "seed"

// LoadProject parses models/**/*.sql and tests/*.sql of fsys and orders the
// models by their refs. seeds names the tables loaded by Go (dbt: seeds)
// with the models they are computed from; they appear in Models with
// Materialized == Seed at their place in the order.
func LoadProject(fsys fs.FS, seeds map[string][]string) (Project, error) {
	var p Project
	byName := map[string]Model{}
	err := fs.WalkDir(fsys, "models", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".sql" {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(path.Base(p), ".sql")
		if _, dup := byName[name]; dup {
			return fmt.Errorf("model %s is defined twice", name)
		}
		m, err := ParseModel(name, string(b))
		if err != nil {
			return err
		}
		m.Path = p
		byName[name] = m
		return nil
	})
	if err != nil {
		return p, err
	}
	for name, deps := range seeds {
		if _, dup := byName[name]; dup {
			return p, fmt.Errorf("model %s is defined twice (also a seed)", name)
		}
		byName[name] = Model{Name: name, Materialized: Seed, Refs: deps}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(n string, from string) error
	visit = func(n, from string) error {
		switch state[n] {
		case 1:
			return fmt.Errorf("models: ref cycle through %s", n)
		case 2:
			return nil
		}
		m, ok := byName[n]
		if !ok {
			return fmt.Errorf("model %s: ref('%s') is no model", from, n)
		}
		state[n] = 1
		for _, r := range m.Refs {
			if err := visit(r, n); err != nil {
				return err
			}
		}
		state[n] = 2
		p.Models = append(p.Models, m)
		return nil
	}
	for _, n := range names {
		if err := visit(n, ""); err != nil {
			return p, err
		}
	}
	tests, _ := fs.Glob(fsys, "tests/*.sql")
	for _, f := range tests {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return p, err
		}
		m, err := ParseModel(strings.TrimSuffix(path.Base(f), ".sql"), string(b))
		if err != nil {
			return p, err
		}
		for _, r := range m.Refs {
			if _, ok := byName[r]; !ok {
				return p, fmt.Errorf("test %s: ref('%s') is no model", m.Name, r)
			}
		}
		m.Path = f
		p.Tests = append(p.Tests, m)
	}
	return p, nil
}

// execer is a connection or a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Build materializes one model.
func Build(ctx context.Context, db execer, m Model, c RenderContext) error {
	body, err := Render(m.Text, c)
	if err != nil {
		return fmt.Errorf("model %s: %w", m.Name, err)
	}
	body = strings.TrimRight(strings.TrimSpace(body), ";")
	var stmts []string
	switch m.Materialized {
	case "view":
		stmts = []string{"CREATE OR REPLACE VIEW " + m.Name + " AS\n" + body}
	case "table":
		stmts = []string{"CREATE OR REPLACE TABLE " + m.Name + " AS\n" + body}
	case Seed:
		return fmt.Errorf("model %s: a seed is loaded by the caller", m.Name)
	case "incremental":
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_tables() WHERE database_name = current_database() AND schema_name = current_schema() AND table_name = ?", m.Name).Scan(&n); err != nil {
			return fmt.Errorf("model %s: %w", m.Name, err)
		}
		if n == 0 {
			stmts = []string{"CREATE TABLE " + m.Name + " AS\n" + body}
		} else {
			tmp := "__new_" + m.Name
			stmts = []string{
				"CREATE OR REPLACE TEMP TABLE " + tmp + " AS\n" + body,
				"DELETE FROM " + m.Name + " WHERE " + m.UniqueKey + " IN (SELECT " + m.UniqueKey + " FROM " + tmp + ")",
				"INSERT INTO " + m.Name + " BY NAME SELECT * FROM " + tmp,
				"DROP TABLE " + tmp,
			}
		}
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("model %s (%s): %w", m.Name, m.Path, err)
		}
	}
	return nil
}

// TestFailures runs a data test and returns the number of violating rows.
func TestFailures(ctx context.Context, db execer, t Model, c RenderContext) (int, error) {
	body, err := Render(t.Text, c)
	if err != nil {
		return 0, fmt.Errorf("test %s: %w", t.Name, err)
	}
	var n int
	q := "SELECT count(*) FROM (" + strings.TrimRight(strings.TrimSpace(body), ";") + ")"
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("test %s: %w", t.Name, err)
	}
	return n, nil
}

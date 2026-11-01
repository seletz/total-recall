// Package config loads the total-recall TOML configuration, applies defaults,
// environment and flag overrides, expands paths and records where each
// effective value came from (see docs/design.md, section "Konfiguration").
//
// Precedence: flag > environment variable > config file > default.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // static builds must not depend on the system zoneinfo

	"github.com/BurntSushi/toml"
)

// Environment variables understood by Load.
const (
	EnvConfig    = "TOTAL_RECALL_CONFIG"
	EnvDB        = "TOTAL_RECALL_DB"
	EnvBackupDir = "TOTAL_RECALL_BACKUP_DIR"
)

// Template is the commented configuration written by `config init`.
// Loading it yields the same effective values as the built-in defaults.
//
//go:embed template.toml
var Template string

// Origin says where an effective configuration value came from.
type Origin string

// Possible origins, in increasing precedence.
const (
	OriginDefault Origin = "default"
	OriginFile    Origin = "file"
	OriginEnv     Origin = "env"
	OriginFlag    Origin = "flag"
)

// Duration is a time.Duration that decodes from a TOML string such as "24h".
type Duration struct{ time.Duration }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// Config is the effective configuration.
type Config struct {
	DB       string `toml:"db"`
	Timezone string `toml:"timezone"`
	Log      string `toml:"log"`
	Backup   Backup `toml:"backup"`
	Redact   Redact `toml:"redact"`
	Ingest   Ingest `toml:"ingest"`
	Harness  struct {
		Claude Claude `toml:"claude"`
	} `toml:"harness"`
	Git Git `toml:"git"`

	// Path is the config file that was considered (it may not exist).
	Path string `toml:"-"`
	// PathOrigin says how Path was chosen (default, env or flag).
	PathOrigin Origin `toml:"-"`
	// Loaded is true when Path existed and was read.
	Loaded bool `toml:"-"`

	origins map[string]Origin
	loc     *time.Location
}

// Backup configures `total-recall backup`.
type Backup struct {
	Dir         string   `toml:"dir"`
	KeepDaily   int      `toml:"keep_daily"`
	KeepMonthly int      `toml:"keep_monthly"`
	MinInterval Duration `toml:"min_interval"`
}

// Redact configures secret masking.
type Redact struct {
	Enabled       bool     `toml:"enabled"`
	ExtraPatterns []string `toml:"extra_patterns"`
	Allow         []string `toml:"allow"`
}

// Ingest configures reading transcripts.
type Ingest struct {
	ToolResultMaxBytes int  `toml:"tool_result_max_bytes"`
	StoreRaw           bool `toml:"store_raw"`
}

// Claude configures the Claude Code adapter.
type Claude struct {
	Enabled         bool     `toml:"enabled"`
	ProjectsDir     string   `toml:"projects_dir"`
	SkipRecordTypes []string `toml:"skip_record_types"`
	Actors          Actors   `toml:"actors"`
}

// Actors holds extra prefixes for actor classification.
type Actors struct {
	PeerPrefixes       []string `toml:"peer_prefixes"`
	AutomationPrefixes []string `toml:"automation_prefixes"`
}

// Git configures git info resolution.
type Git struct {
	Resolve bool `toml:"resolve"`
}

// Flags are command-line overrides. Empty fields are not set.
type Flags struct {
	DB        string
	BackupDir string
	Timezone  string
}

// Options control Load. Zero values use the real environment.
type Options struct {
	// ConfigFile is the --config flag value.
	ConfigFile string
	// Flags are the remaining flag overrides.
	Flags Flags
	// GOOS selects platform defaults; default runtime.GOOS.
	GOOS string
	// Home is the home directory; default os.UserHomeDir().
	Home string
	// Getenv looks up environment variables; default os.Getenv.
	Getenv func(string) string
}

// Entry is one effective value for `config show`.
type Entry struct {
	Key    string
	Value  string
	Origin Origin
}

// keys lists every configuration key with a formatter, in display order.
var keys = []struct {
	key    string
	format func(*Config) string
}{
	{"db", func(c *Config) string { return c.DB }},
	{"timezone", func(c *Config) string { return c.Timezone }},
	{"log", func(c *Config) string { return c.Log }},
	{"backup.dir", func(c *Config) string { return c.Backup.Dir }},
	{"backup.keep_daily", func(c *Config) string { return strconv.Itoa(c.Backup.KeepDaily) }},
	{"backup.keep_monthly", func(c *Config) string { return strconv.Itoa(c.Backup.KeepMonthly) }},
	{"backup.min_interval", func(c *Config) string { return c.Backup.MinInterval.String() }},
	{"redact.enabled", func(c *Config) string { return strconv.FormatBool(c.Redact.Enabled) }},
	{"redact.extra_patterns", func(c *Config) string { return formatList(c.Redact.ExtraPatterns) }},
	{"redact.allow", func(c *Config) string { return formatList(c.Redact.Allow) }},
	{"ingest.tool_result_max_bytes", func(c *Config) string { return strconv.Itoa(c.Ingest.ToolResultMaxBytes) }},
	{"ingest.store_raw", func(c *Config) string { return strconv.FormatBool(c.Ingest.StoreRaw) }},
	{"harness.claude.enabled", func(c *Config) string { return strconv.FormatBool(c.Harness.Claude.Enabled) }},
	{"harness.claude.projects_dir", func(c *Config) string { return c.Harness.Claude.ProjectsDir }},
	{"harness.claude.skip_record_types", func(c *Config) string { return formatList(c.Harness.Claude.SkipRecordTypes) }},
	{"harness.claude.actors.peer_prefixes", func(c *Config) string { return formatList(c.Harness.Claude.Actors.PeerPrefixes) }},
	{"harness.claude.actors.automation_prefixes", func(c *Config) string { return formatList(c.Harness.Claude.Actors.AutomationPrefixes) }},
	{"git.resolve", func(c *Config) string { return strconv.FormatBool(c.Git.Resolve) }},
}

func formatList(l []string) string {
	q := make([]string, len(l))
	for i, s := range l {
		q[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// defaults returns the built-in configuration for the given platform.
func defaults(goos, home string, getenv func(string) string) *Config {
	c := &Config{Timezone: "Europe/Berlin"}
	if goos == "darwin" {
		c.DB = filepath.Join(home, "Library", "Application Support", "total-recall", "archive.db")
	} else {
		data := getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		c.DB = filepath.Join(data, "total-recall", "archive.db")
	}
	c.Backup = Backup{Dir: filepath.Join(home, "Documents", "total-recall"), KeepDaily: 7, MinInterval: Duration{24 * time.Hour}}
	c.Redact = Redact{Enabled: true, ExtraPatterns: []string{}, Allow: []string{}}
	c.Ingest = Ingest{ToolResultMaxBytes: 4096, StoreRaw: true}
	claudeDir := getenv("CLAUDE_CONFIG_DIR")
	if claudeDir == "" {
		claudeDir = filepath.Join(home, ".claude")
	}
	c.Harness.Claude = Claude{
		Enabled:         true,
		ProjectsDir:     filepath.Join(claudeDir, "projects"),
		SkipRecordTypes: []string{},
		Actors:          Actors{PeerPrefixes: []string{}, AutomationPrefixes: []string{"Hourly ", "Weekend watch"}},
	}
	c.Git.Resolve = true
	return c
}

// DefaultPath returns the default config file location.
func DefaultPath(home string, getenv func(string) string) string {
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "total-recall", "config.toml")
}

// Load builds the effective configuration.
// A missing default config file is not an error; a missing file named by
// --config or TOTAL_RECALL_CONFIG is.
func Load(opts Options) (*Config, error) {
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("config: home directory: %w", err)
		}
		opts.Home = h
	}
	expand := func(p string) string { return ExpandPath(p, opts.Home, opts.Getenv) }

	c := defaults(opts.GOOS, opts.Home, opts.Getenv)
	c.origins = map[string]Origin{}

	switch {
	case opts.ConfigFile != "":
		c.Path, c.PathOrigin = expand(opts.ConfigFile), OriginFlag
	case opts.Getenv(EnvConfig) != "":
		c.Path, c.PathOrigin = expand(opts.Getenv(EnvConfig)), OriginEnv
	default:
		c.Path, c.PathOrigin = DefaultPath(opts.Home, opts.Getenv), OriginDefault
	}

	if err := c.readFile(expand); err != nil {
		return nil, err
	}

	if v := opts.Getenv(EnvDB); v != "" {
		c.set("db", &c.DB, expand(v), OriginEnv)
	}
	if v := opts.Getenv(EnvBackupDir); v != "" {
		c.set("backup.dir", &c.Backup.Dir, expand(v), OriginEnv)
	}
	if v := opts.Flags.DB; v != "" {
		c.set("db", &c.DB, expand(v), OriginFlag)
	}
	if v := opts.Flags.BackupDir; v != "" {
		c.set("backup.dir", &c.Backup.Dir, expand(v), OriginFlag)
	}
	if v := opts.Flags.Timezone; v != "" {
		c.set("timezone", &c.Timezone, v, OriginFlag)
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) set(key string, dst *string, v string, o Origin) {
	*dst = v
	c.origins[key] = o
}

func (c *Config) readFile(expand func(string) string) error {
	b, err := os.ReadFile(c.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && c.PathOrigin == OriginDefault {
			return nil
		}
		return fmt.Errorf("config: %w", err)
	}
	md, err := toml.Decode(string(b), c)
	if err != nil {
		return fmt.Errorf("config %s: %w", c.Path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		names := make([]string, len(und))
		for i, k := range und {
			names[i] = k.String()
		}
		return fmt.Errorf("config %s: unknown keys: %s", c.Path, strings.Join(names, ", "))
	}
	c.Loaded = true
	for _, k := range keys {
		if md.IsDefined(strings.Split(k.key, ".")...) {
			c.origins[k.key] = OriginFile
		}
	}
	for _, p := range []*string{&c.DB, &c.Log, &c.Backup.Dir, &c.Harness.Claude.ProjectsDir} {
		*p = expand(*p)
	}
	return nil
}

func (c *Config) validate() error {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return fmt.Errorf("config: timezone %q: %w", c.Timezone, err)
	}
	c.loc = loc
	if c.Backup.KeepDaily < 0 {
		return fmt.Errorf("config: backup.keep_daily must be >= 0")
	}
	if c.Backup.KeepMonthly < 0 {
		return fmt.Errorf("config: backup.keep_monthly must be >= 0")
	}
	if c.Ingest.ToolResultMaxBytes < 0 {
		return fmt.Errorf("config: ingest.tool_result_max_bytes must be >= 0")
	}
	for _, l := range []struct {
		key  string
		pats []string
	}{{"redact.extra_patterns", c.Redact.ExtraPatterns}, {"redact.allow", c.Redact.Allow}} {
		for _, p := range l.pats {
			if _, err := regexp.Compile(p); err != nil {
				return fmt.Errorf("config: %s: %w", l.key, err)
			}
		}
	}
	return nil
}

// Origin returns where the value of key (dotted, e.g. "backup.dir") came from.
func (c *Config) Origin(key string) Origin {
	if o, ok := c.origins[key]; ok {
		return o
	}
	return OriginDefault
}

// Entries returns all effective values with their origin, in a stable order.
func (c *Config) Entries() []Entry {
	out := make([]Entry, len(keys))
	for i, k := range keys {
		out[i] = Entry{Key: k.key, Value: k.format(c), Origin: c.Origin(k.key)}
	}
	return out
}

// Location returns the configured time zone.
func (c *Config) Location() *time.Location {
	if c.loc == nil {
		if loc, err := time.LoadLocation(c.Timezone); err == nil {
			c.loc = loc
		} else {
			return time.UTC
		}
	}
	return c.loc
}

// LogPath returns the effective log file: Log, or total-recall.log next to the DB.
func (c *Config) LogPath() string {
	if c.Log != "" {
		return c.Log
	}
	return filepath.Join(filepath.Dir(c.DB), "total-recall.log")
}

// ExpandPath expands a leading "~" or "~/" to home and $VAR / ${VAR} using getenv.
// "~user" forms are left alone.
func ExpandPath(p, home string, getenv func(string) string) string {
	if p == "" {
		return ""
	}
	if p == "~" {
		p = home
	} else if strings.HasPrefix(p, "~/") {
		p = home + p[1:]
	}
	return os.Expand(p, getenv)
}

// WriteTemplate writes Template to path, creating parent directories.
// It never overwrites an existing file; in that case the returned error
// satisfies os.IsExist / errors.Is(err, fs.ErrExist).
func WriteTemplate(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(Template); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

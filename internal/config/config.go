// Package config loads the total-recall TOML configuration, applies defaults,
// environment and flag overrides, expands paths and records where each
// effective value came from (see docs/explanation/design.md, section "Konfiguration").
//
// Precedence: flag > environment variable > config file > default.
//
// Load validates the core settings (paths, time zone, day boundary, backup,
// ingest, redaction). The tables [work], [worktime], [tui], [keys] and the
// list [[project]] are only parsed: their values are checked when a command
// uses them (WorkHours, WorktimeSettings, KeyMap, ValidateTUI,
// ProjectResolver), so a mistake there never stops hooks and ingest.
// Problems lists every such error for `config show` and `doctor`.
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
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // static builds must not depend on the system zoneinfo

	"github.com/BurntSushi/toml"

	"github.com/seletz/total-recall/internal/figure"
	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/worktime"
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

// DefaultRawMaxBytes is the default of ingest.raw_max_bytes: 0, no limit.
// The archive keeps every line whole since #180; the option stays for
// archives that must stay small.
const DefaultRawMaxBytes = 0

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
	// DayStart is the wall-clock time ("HH:MM") at which a day begins for
	// the first/last activity per day (stats --workday, TUI activity):
	// events before it count for the previous day.
	DayStart string `toml:"day_start"`
	Log      string `toml:"log"`
	Backup   Backup `toml:"backup"`
	Redact   Redact `toml:"redact"`
	Ingest   Ingest `toml:"ingest"`
	Harness  struct {
		Claude Claude `toml:"claude"`
	} `toml:"harness"`
	Git Git `toml:"git"`
	// Work is the normal working time the activity per day is measured
	// against (stats --workday, TUI activity).
	Work Work `toml:"work"`
	// Worktime holds the parameters of the working time per project
	// (stats --worktime).
	Worktime Worktime `toml:"worktime"`
	// TUI holds the start-up defaults of `total-recall tui`.
	TUI TUI `toml:"tui"`
	// Chart holds the defaults of `total-recall chart`.
	Chart Chart `toml:"chart"`
	// Keys overrides key bindings of the TUI: action name → keys; see
	// package keys for the names. Only listed actions change.
	Keys map[string]KeyList `toml:"keys"`
	// Accounts names the account and organization IDs the sessions carry
	// (sessions.account_id, sessions.organization_id): ID → name. Display
	// code shows the name where it has one; `sessions --account` and
	// `stats --by account` accept the names.
	Accounts map[string]string `toml:"accounts"`
	// Projects are the configured projects ([[project]]) in file order:
	// name, description, colour and the patterns that assign sessions to
	// them. ProjectResolver validates and compiles them.
	Projects []Project `toml:"project"`

	// Path is the config file that was considered (it may not exist).
	Path string `toml:"-"`
	// PathOrigin says how Path was chosen (default, env or flag).
	PathOrigin Origin `toml:"-"`
	// Loaded is true when Path existed and was read.
	Loaded bool `toml:"-"`
	// Home is the home directory the paths were expanded with (Options.Home
	// or os.UserHomeDir()); display code abbreviates it to "~".
	Home string `toml:"-"`

	origins  map[string]Origin
	loc      *time.Location
	dayStart time.Duration
	// opts are the options the configuration was loaded with; SetValue
	// loads the edited file with the same ones.
	opts Options
	// defaultValues are the built-in values of every key as Entries
	// formats them (Default); fileKeys are the keys the file sets, even
	// those an environment variable or a flag overrides (Reset).
	defaultValues map[string]string
	fileKeys      map[string]struct{}
}

// StringList is a TOML value that is a string or a list of strings.
type StringList []string

// KeyList is the value of one [keys] entry: a key name or a list of them.
type KeyList = StringList

// UnmarshalTOML implements toml.Unmarshaler: a string or an array of strings.
func (l *StringList) UnmarshalTOML(v any) error {
	switch v := v.(type) {
	case string:
		*l = StringList{v}
	case []any:
		out := make(StringList, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("want a string, got %v", e)
			}
			out = append(out, s)
		}
		*l = out
	default:
		return fmt.Errorf("want a string or a list of strings, got %v", v)
	}
	return nil
}

// Worktime configures the working time per project and day that
// `stats --worktime` estimates from the user's own messages (see
// internal/worktime for the method).
type Worktime struct {
	// MaxGap is the longest gap between two own messages that counts in
	// full; MinCredit is what a longer gap (a break, a meeting, the first
	// message of a day) counts.
	MaxGap    Duration `toml:"max_gap"`
	MinCredit Duration `toml:"min_credit"`
	// Grid is the rounding grid (0 = no rounding), Rounding the direction
	// (up, down, nearest) and Scope what is rounded: the sum per category
	// and day (category) or every project per day (project).
	Grid     Duration `toml:"grid"`
	Rounding string   `toml:"rounding"`
	Scope    string   `toml:"scope"`
	// SummaryMaxChars is the length of the booking text per category and
	// day (stats --worktime --summary) in characters; 0 means no limit.
	SummaryMaxChars int `toml:"summary_max_chars"`
	// Categories are the ordered rules that map projects to categories
	// ([[worktime.category]]); the first match wins, a project without a
	// match is its own category.
	Categories []Category `toml:"category"`
}

// Category is one [[worktime.category]] rule: the category name, the
// glob patterns matched against the project name, its git root (as a
// path and with "~") and its remote URL, and an optional colour for the
// charts (a name of project.Palette or #rrggbb; validated by ChartSettings).
type Category struct {
	Name  string     `toml:"name"`
	Match StringList `toml:"match"`
	Color string     `toml:"color"`
}

// Project is one [[project]] definition: the name (required, unique), an
// optional description and colour (a project.Palette name or #rrggbb;
// empty means the automatic colour) and the glob patterns (same syntax as
// Category) matched against the git root or working directory as a path
// and with "~", the remote URL and the derived project name. The first
// definition with a match wins; see project.Resolver.
type Project struct {
	Name        string     `toml:"name"`
	Description string     `toml:"description"`
	Color       string     `toml:"color"`
	Match       StringList `toml:"match"`
}

// Built-in working time per project: gaps up to 20 minutes count in full,
// longer ones 3 minutes, the sum per category and day is rounded up to
// 15 minutes, the booking text is cut at 200 characters.
const (
	DefaultWorktimeMaxGap          = 20 * time.Minute
	DefaultWorktimeMinCredit       = 3 * time.Minute
	DefaultWorktimeGrid            = 15 * time.Minute
	DefaultWorktimeRounding        = "up"
	DefaultWorktimeScope           = "category"
	DefaultWorktimeSummaryMaxChars = 200
)

// TUI configures `total-recall tui`. Flags override every value.
type TUI struct {
	// View is the view shown first: search, stats, activity or config.
	View string `toml:"view"`
	// Graphics is the chart mode: auto, text or kitty (--graphics).
	Graphics string `toml:"graphics"`
	// Mouse enables wheel scrolling and clicks (--no-mouse turns it off).
	Mouse bool `toml:"mouse"`
	// StatsPeriod and StatsDimension are the period (day, week, month) and
	// the dimension (project, agent, directory) the stats view starts with.
	StatsPeriod    string `toml:"stats_period"`
	StatsDimension string `toml:"stats_dimension"`
	// ActivityWindow is the window the activity view starts with: day,
	// week or month.
	ActivityWindow string `toml:"activity_window"`
	// ActivityProjects starts the activity view with the bars coloured by
	// project (the key p switches).
	ActivityProjects bool `toml:"activity_projects"`
	// ActivityNewest is where the activity view puts the newest day:
	// bottom or top (the key o switches).
	ActivityNewest string `toml:"activity_newest"`
	// Reload is how often the TUI checks the archive for changes and
	// reloads the visible view when there are any (--reload); 0 switches
	// the reload off, anything else is at least MinReload.
	Reload Duration `toml:"reload"`
}

// Reload defaults and bounds of [tui] reload.
const (
	DefaultReload = 5 * time.Second
	MinReload     = time.Second
)

// Chart configures `total-recall chart`; flags override every value.
type Chart struct {
	// Format is the output format: svg or png (--format).
	Format string `toml:"format"`
	// Scale multiplies the natural size of a figure (--scale); --width
	// and --height set the size instead.
	Scale float64 `toml:"scale"`
	// Title and Legend draw the heading and the legend (--no-title,
	// --no-legend switch them off for one run).
	Title  bool `toml:"title"`
	Legend bool `toml:"legend"`
	// Transparent leaves the white background out (--transparent).
	Transparent bool `toml:"transparent"`
	// Top is how many projects the project chart shows; the rest is
	// folded into "other" (--top, 0 = all).
	Top int `toml:"top"`
	// Heatmap is the colour scale of the calendar, light to dark: names
	// of project.Palette or #rrggbb values, one per level.
	Heatmap []string `toml:"heatmap"`
	// Lang is the language of the labels: en or de (--lang).
	Lang string `toml:"lang"`
}

// Defaults of [chart].
const (
	DefaultChartFormat = "svg"
	DefaultChartScale  = 1.0
	DefaultChartTop    = 12
	DefaultChartLang   = string(figure.EN)
)

// ChartFormats are the allowed values of [chart] format.
var ChartFormats = []string{"svg", "png"}

// ChartLangs are the allowed values of [chart] lang.
var ChartLangs = figure.Langs

// ChartSettings is the validated [chart] table with the category colours
// of [[worktime.category]], as `total-recall chart` uses them.
type ChartSettings struct {
	Format      string
	Scale       float64
	Title       bool
	Legend      bool
	Transparent bool
	Top         int
	// Lang is the language of the labels.
	Lang figure.Lang
	// Heatmap holds the colours of the calendar scale as #rrggbb.
	Heatmap []string
	// CategoryColors maps a category name to its colour as #rrggbb;
	// categories without a colour are absent.
	CategoryColors map[string]string
}

// ValidReload checks a reload interval of the TUI: 0 (off) or at least
// MinReload. The CLI applies it to --reload as well.
func ValidReload(d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("reload %s: must not be negative", d)
	}
	if d != 0 && d < MinReload {
		return fmt.Errorf("reload %s: use 0 to switch the reload off or at least %s", d, MinReload)
	}
	return nil
}

// Allowed values of the [tui] table.
var (
	TUIViews      = []string{"search", "stats", "activity", "config"}
	TUIGraphics   = []string{"auto", "text", "kitty"}
	TUIPeriods    = []string{"day", "week", "month"}
	TUINewest     = []string{"bottom", "top"}
	TUIDimensions = []string{"project", "agent", "directory"}
)

// DefaultDayStart is the built-in day boundary for the activity per day.
const DefaultDayStart = "04:00"

// Work configures the normal working time of the activity per day.
type Work struct {
	// Hours are the core hours as "HH:MM-HH:MM" (local wall-clock time of
	// the calendar day, start before end, not before day_start).
	Hours string `toml:"hours"`
	// Target is the daily target on weekdays; weekends have none.
	Target Duration `toml:"target"`
	// DailyLow and DailyHigh bound the span of a day that counts as on
	// target: below DailyLow it is under, above DailyHigh over.
	DailyLow  Duration `toml:"daily_low"`
	DailyHigh Duration `toml:"daily_high"`
	// WeeklyLow and WeeklyHigh bound the working time of a week with five
	// workdays that counts as on target; a week or a month with fewer
	// workdays (holidays, a running period) scales them.
	WeeklyLow  Duration `toml:"weekly_low"`
	WeeklyHigh Duration `toml:"weekly_high"`
	// Bundesland selects the public holidays (see internal/holiday); an
	// unknown or empty name means the nationwide holidays only.
	Bundesland string `toml:"bundesland"`
	// Count says which events the activity per day counts by default:
	// CountHuman (own messages only) or CountAll (human and agent events).
	Count string `toml:"count"`
}

// Values of Work.Count.
const (
	CountHuman = "human"
	CountAll   = "all"
)

// HumanOnly reports whether the activity per day counts own messages
// only by default.
func (w Work) HumanOnly() bool { return w.Count != CountAll }

// Built-in working time: core hours 09:00–17:00, 8h per weekday, a span of
// 6h to 9h per day and 32h to 48h per week is on target, holidays of
// Baden-Württemberg.
const (
	DefaultWorkHours      = "09:00-17:00"
	DefaultWorkTarget     = 8 * time.Hour
	DefaultWorkDailyLow   = 6 * time.Hour
	DefaultWorkDailyHigh  = 9 * time.Hour
	DefaultWorkWeeklyLow  = 32 * time.Hour
	DefaultWorkWeeklyHigh = 48 * time.Hour
	DefaultWorkBundesland = "Baden-Württemberg"
	DefaultWorkCount      = CountHuman
)

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
	RawMaxBytes        int  `toml:"raw_max_bytes"`
	StoreRaw           bool `toml:"store_raw"`
	StoreBlobs         bool `toml:"store_blobs"`
	// StoreMemory archives the project memory of the harness (versioned,
	// redacted) on ingest --all and in the hooks.
	StoreMemory bool `toml:"store_memory"`
	// StoreFileHistory archives the backup copies of edited files the
	// harness keeps next to its transcripts (Claude Code: file-history).
	StoreFileHistory bool `toml:"store_file_history"`
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
	PeerPasteHeaders   []string `toml:"peer_paste_headers"`
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

// fields lists every configuration key with a formatter, in display order.
var fields = []struct {
	key    string
	format func(*Config) string
}{
	{"db", func(c *Config) string { return c.DB }},
	{"timezone", func(c *Config) string { return c.Timezone }},
	{"day_start", func(c *Config) string { return c.DayStart }},
	{"log", func(c *Config) string { return c.Log }},
	{"backup.dir", func(c *Config) string { return c.Backup.Dir }},
	{"backup.keep_daily", func(c *Config) string { return strconv.Itoa(c.Backup.KeepDaily) }},
	{"backup.keep_monthly", func(c *Config) string { return strconv.Itoa(c.Backup.KeepMonthly) }},
	{"backup.min_interval", func(c *Config) string { return c.Backup.MinInterval.String() }},
	{"redact.enabled", func(c *Config) string { return strconv.FormatBool(c.Redact.Enabled) }},
	{"redact.extra_patterns", func(c *Config) string { return formatList(c.Redact.ExtraPatterns) }},
	{"redact.allow", func(c *Config) string { return formatList(c.Redact.Allow) }},
	{"ingest.tool_result_max_bytes", func(c *Config) string { return strconv.Itoa(c.Ingest.ToolResultMaxBytes) }},
	{"ingest.raw_max_bytes", func(c *Config) string { return strconv.Itoa(c.Ingest.RawMaxBytes) }},
	{"ingest.store_raw", func(c *Config) string { return strconv.FormatBool(c.Ingest.StoreRaw) }},
	{"ingest.store_blobs", func(c *Config) string { return strconv.FormatBool(c.Ingest.StoreBlobs) }},
	{"ingest.store_memory", func(c *Config) string { return strconv.FormatBool(c.Ingest.StoreMemory) }},
	{"ingest.store_file_history", func(c *Config) string { return strconv.FormatBool(c.Ingest.StoreFileHistory) }},
	{"harness.claude.enabled", func(c *Config) string { return strconv.FormatBool(c.Harness.Claude.Enabled) }},
	{"harness.claude.projects_dir", func(c *Config) string { return c.Harness.Claude.ProjectsDir }},
	{"harness.claude.skip_record_types", func(c *Config) string { return formatList(c.Harness.Claude.SkipRecordTypes) }},
	{"harness.claude.actors.peer_prefixes", func(c *Config) string { return formatList(c.Harness.Claude.Actors.PeerPrefixes) }},
	{"harness.claude.actors.peer_paste_headers", func(c *Config) string { return formatList(c.Harness.Claude.Actors.PeerPasteHeaders) }},
	{"harness.claude.actors.automation_prefixes", func(c *Config) string { return formatList(c.Harness.Claude.Actors.AutomationPrefixes) }},
	{"git.resolve", func(c *Config) string { return strconv.FormatBool(c.Git.Resolve) }},
	{"work.hours", func(c *Config) string { return c.Work.Hours }},
	{"work.target", func(c *Config) string { return c.Work.Target.String() }},
	{"work.daily_low", func(c *Config) string { return c.Work.DailyLow.String() }},
	{"work.daily_high", func(c *Config) string { return c.Work.DailyHigh.String() }},
	{"work.weekly_low", func(c *Config) string { return c.Work.WeeklyLow.String() }},
	{"work.weekly_high", func(c *Config) string { return c.Work.WeeklyHigh.String() }},
	{"work.bundesland", func(c *Config) string { return c.Work.Bundesland }},
	{"work.count", func(c *Config) string { return c.Work.Count }},
	{"worktime.max_gap", func(c *Config) string { return c.Worktime.MaxGap.String() }},
	{"worktime.min_credit", func(c *Config) string { return c.Worktime.MinCredit.String() }},
	{"worktime.grid", func(c *Config) string { return c.Worktime.Grid.String() }},
	{"worktime.rounding", func(c *Config) string { return c.Worktime.Rounding }},
	{"worktime.scope", func(c *Config) string { return c.Worktime.Scope }},
	{"worktime.summary_max_chars", func(c *Config) string { return strconv.Itoa(c.Worktime.SummaryMaxChars) }},
	{"worktime.category", func(c *Config) string { return formatCategories(c.Worktime.Categories) }},
	{"tui.view", func(c *Config) string { return c.TUI.View }},
	{"tui.graphics", func(c *Config) string { return c.TUI.Graphics }},
	{"tui.mouse", func(c *Config) string { return strconv.FormatBool(c.TUI.Mouse) }},
	{"tui.stats_period", func(c *Config) string { return c.TUI.StatsPeriod }},
	{"tui.stats_dimension", func(c *Config) string { return c.TUI.StatsDimension }},
	{"tui.activity_window", func(c *Config) string { return c.TUI.ActivityWindow }},
	{"tui.activity_projects", func(c *Config) string { return strconv.FormatBool(c.TUI.ActivityProjects) }},
	{"tui.activity_newest", func(c *Config) string { return c.TUI.ActivityNewest }},
	{"tui.reload", func(c *Config) string { return c.TUI.Reload.String() }},
	{"chart.format", func(c *Config) string { return c.Chart.Format }},
	{"chart.scale", func(c *Config) string { return strconv.FormatFloat(c.Chart.Scale, 'f', -1, 64) }},
	{"chart.title", func(c *Config) string { return strconv.FormatBool(c.Chart.Title) }},
	{"chart.legend", func(c *Config) string { return strconv.FormatBool(c.Chart.Legend) }},
	{"chart.transparent", func(c *Config) string { return strconv.FormatBool(c.Chart.Transparent) }},
	{"chart.top", func(c *Config) string { return strconv.Itoa(c.Chart.Top) }},
	{"chart.heatmap", func(c *Config) string { return formatList(c.Chart.Heatmap) }},
	{"chart.lang", func(c *Config) string { return c.Chart.Lang }},
	{"accounts", func(c *Config) string { return formatAccounts(c.Accounts) }},
	{"project", func(c *Config) string { return formatProjects(c.Projects) }},
}

// formatProjects formats the project definitions as `[name (#rrggbb) =
// ["glob", …], …]`; the colour is left out when there is none.
func formatProjects(ps []Project) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		name := p.Name
		if hex, err := project.ParseColor(p.Color); err == nil && hex != "" {
			name += " (" + hex + ")"
		} else if p.Color != "" {
			name += " (" + strconv.Quote(p.Color) + ")"
		}
		parts[i] = name + " = " + formatList(p.Match)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// formatAccounts formats the account names as `["id" = "name", …]`, by ID.
func formatAccounts(m map[string]string) string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Quote(id) + " = " + strconv.Quote(m[id])
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// formatCategories formats the category rules as `[name = ["glob", …], …]`,
// a rule with a colour as `name = ["glob"] (blue)`.
func formatCategories(cs []Category) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = c.Name + " = " + formatList(c.Match)
		if c.Color != "" {
			parts[i] += " (" + c.Color + ")"
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
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
	c := &Config{Timezone: "Europe/Berlin", DayStart: DefaultDayStart}
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
	c.Ingest = Ingest{ToolResultMaxBytes: 4096, RawMaxBytes: DefaultRawMaxBytes, StoreRaw: true, StoreBlobs: true, StoreMemory: true, StoreFileHistory: true}
	claudeDir := getenv("CLAUDE_CONFIG_DIR")
	if claudeDir == "" {
		claudeDir = filepath.Join(home, ".claude")
	}
	c.Harness.Claude = Claude{
		Enabled:         true,
		ProjectsDir:     filepath.Join(claudeDir, "projects"),
		SkipRecordTypes: []string{},
		Actors:          Actors{PeerPrefixes: []string{}, PeerPasteHeaders: []string{}, AutomationPrefixes: []string{"Hourly ", "Weekend watch"}},
	}
	c.Git.Resolve = true
	c.Work = Work{
		Hours: DefaultWorkHours, Target: Duration{DefaultWorkTarget},
		DailyLow: Duration{DefaultWorkDailyLow}, DailyHigh: Duration{DefaultWorkDailyHigh},
		WeeklyLow: Duration{DefaultWorkWeeklyLow}, WeeklyHigh: Duration{DefaultWorkWeeklyHigh},
		Bundesland: DefaultWorkBundesland, Count: DefaultWorkCount,
	}
	c.Worktime = Worktime{
		MaxGap: Duration{DefaultWorktimeMaxGap}, MinCredit: Duration{DefaultWorktimeMinCredit}, Grid: Duration{DefaultWorktimeGrid},
		Rounding: DefaultWorktimeRounding, Scope: DefaultWorktimeScope, SummaryMaxChars: DefaultWorktimeSummaryMaxChars, Categories: []Category{},
	}
	c.TUI = TUI{View: "search", Graphics: "auto", Mouse: true, StatsPeriod: "day", StatsDimension: "project", ActivityWindow: "week", ActivityNewest: "bottom", Reload: Duration{DefaultReload}}
	c.Chart = Chart{Format: DefaultChartFormat, Scale: DefaultChartScale, Title: true, Legend: true, Top: DefaultChartTop, Heatmap: slices.Clone(figure.DefaultHeatmap), Lang: DefaultChartLang}
	c.Keys = map[string]KeyList{}
	c.Accounts = map[string]string{}
	c.Projects = []Project{}
	return c
}

// AccountName returns the configured name of an account or organization
// ID, or "" if none is configured.
func (c *Config) AccountName(id string) string { return c.Accounts[id] }

// ResolveAccount returns the ID a configured account name stands for; any
// other reference (an ID or a prefix of one) is returned unchanged.
func (c *Config) ResolveAccount(ref string) string {
	ids := make([]string, 0, len(c.Accounts))
	for id, name := range c.Accounts {
		if name == ref {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return ref
	}
	sort.Strings(ids)
	return ids[0]
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
func Load(opts Options) (*Config, error) { return load(opts, nil) }

// load builds the configuration; text, when not nil, stands in for the
// content of the config file (SetValue validates an edit with it).
func load(opts Options, text []byte) (*Config, error) {
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
	c.Home = opts.Home
	c.opts = opts
	c.origins = map[string]Origin{}
	c.fileKeys = map[string]struct{}{}
	c.defaultValues = map[string]string{}
	for _, e := range c.Entries() {
		c.defaultValues[e.Key] = e.Value
	}

	switch {
	case opts.ConfigFile != "":
		c.Path, c.PathOrigin = expand(opts.ConfigFile), OriginFlag
	case opts.Getenv(EnvConfig) != "":
		c.Path, c.PathOrigin = expand(opts.Getenv(EnvConfig)), OriginEnv
	default:
		c.Path, c.PathOrigin = DefaultPath(opts.Home, opts.Getenv), OriginDefault
	}

	if err := c.readFile(expand, text); err != nil {
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

// readFile decodes the config file, or text when it is not nil.
func (c *Config) readFile(expand func(string) string, text []byte) error {
	b := text
	if b == nil {
		var err error
		b, err = os.ReadFile(c.Path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && c.PathOrigin == OriginDefault {
				return nil
			}
			return fmt.Errorf("config: %w", err)
		}
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
	for _, k := range fields {
		if md.IsDefined(strings.Split(k.key, ".")...) {
			c.origins[k.key] = OriginFile
			c.fileKeys[k.key] = struct{}{}
		}
	}
	for name := range c.Keys {
		c.origins["keys."+name] = OriginFile
		c.fileKeys["keys."+name] = struct{}{}
	}
	for id := range c.Accounts {
		c.fileKeys["accounts."+id] = struct{}{}
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
	d, err := parseClock(c.DayStart)
	if err != nil {
		return fmt.Errorf("config: day_start %q: %w", c.DayStart, err)
	}
	c.dayStart = d
	if c.Backup.KeepDaily < 0 {
		return fmt.Errorf("config: backup.keep_daily must be >= 0")
	}
	if c.Backup.KeepMonthly < 0 {
		return fmt.Errorf("config: backup.keep_monthly must be >= 0")
	}
	if c.Ingest.ToolResultMaxBytes < 0 {
		return fmt.Errorf("config: ingest.tool_result_max_bytes must be >= 0")
	}
	if c.Ingest.RawMaxBytes < 0 {
		return fmt.Errorf("config: ingest.raw_max_bytes must be >= 0")
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

// WorkHours validates the [work] table against day_start and returns the
// core hours as offsets from local midnight (09:00–17:00 = 9h, 17h). The
// error names the first bad value; commands that do not use the working
// time never see it.
func (c *Config) WorkHours() (start, end time.Duration, err error) {
	from, to, ok := strings.Cut(c.Work.Hours, "-")
	start, err1 := parseClock(from)
	end, err2 := parseClock(to)
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("config: work.hours %q: want HH:MM-HH:MM", c.Work.Hours)
	}
	if start >= end {
		return 0, 0, fmt.Errorf("config: work.hours %q: start must be before end", c.Work.Hours)
	}
	if start < c.dayStart {
		return 0, 0, fmt.Errorf("config: work.hours %q: must not start before day_start %s", c.Work.Hours, c.DayStart)
	}
	if err := c.validateWorkBounds(); err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

// validateWorkBounds checks the target, the bounds and the count of [work].
func (c *Config) validateWorkBounds() error {
	if t := c.Work.Target.Duration; t <= 0 || t > 24*time.Hour {
		return fmt.Errorf("config: work.target %s: want more than 0 and at most 24h", t)
	}
	if c.Work.DailyLow.Duration < 0 {
		return fmt.Errorf("config: work.daily_low must be >= 0")
	}
	if c.Work.DailyHigh.Duration < c.Work.DailyLow.Duration {
		return fmt.Errorf("config: work.daily_high must not be below work.daily_low")
	}
	if c.Work.WeeklyLow.Duration < 0 {
		return fmt.Errorf("config: work.weekly_low must be >= 0")
	}
	if c.Work.WeeklyHigh.Duration < c.Work.WeeklyLow.Duration {
		return fmt.Errorf("config: work.weekly_high must not be below work.weekly_low")
	}
	if c.Work.Count != CountHuman && c.Work.Count != CountAll {
		return fmt.Errorf("config: work.count %q: want %s or %s", c.Work.Count, CountHuman, CountAll)
	}
	return nil
}

// WorktimeSettings validates the [worktime] table and returns it as the settings
// of the working-time estimate, the category rules compiled. The error
// names the first bad value; commands that do not estimate working time
// never see it.
func (c *Config) WorktimeSettings() (worktime.Settings, error) {
	w := c.Worktime
	s := worktime.Settings{MaxGap: w.MaxGap.Duration, MinCredit: w.MinCredit.Duration, Grid: w.Grid.Duration, SummaryMaxChars: w.SummaryMaxChars}
	if s.MaxGap <= 0 {
		return s, fmt.Errorf("config: worktime.max_gap %s: want more than 0", s.MaxGap)
	}
	if s.MinCredit < 0 || s.MinCredit > s.MaxGap {
		return s, fmt.Errorf("config: worktime.min_credit %s: want between 0 and worktime.max_gap (%s)", s.MinCredit, s.MaxGap)
	}
	if s.Grid < 0 || s.Grid > 24*time.Hour {
		return s, fmt.Errorf("config: worktime.grid %s: want between 0 (no rounding) and 24h", s.Grid)
	}
	if s.SummaryMaxChars < 0 {
		return s, fmt.Errorf("config: worktime.summary_max_chars %d: want 0 (no limit) or more", s.SummaryMaxChars)
	}
	var err error
	if s.Rounding, err = worktime.ParseRounding(w.Rounding); err != nil {
		return s, fmt.Errorf("config: worktime.rounding %q: use %s", w.Rounding, orList(worktime.Roundings))
	}
	if s.Scope, err = worktime.ParseScope(w.Scope); err != nil {
		return s, fmt.Errorf("config: worktime.scope %q: use %s", w.Scope, orList(worktime.Scopes))
	}
	for i, cat := range w.Categories {
		r, err := worktime.NewRule(cat.Name, cat.Match)
		if err != nil {
			return s, fmt.Errorf("config: worktime.category[%d]: %w", i, err)
		}
		s.Rules = append(s.Rules, r)
	}
	if err := worktime.ValidateRules(s.Rules); err != nil {
		return s, fmt.Errorf("config: worktime.category: %w", err)
	}
	return s, nil
}

// ProjectResolver validates the [[project]] list and compiles it into the
// resolver that assigns sessions to the configured projects, with Home as
// "~" in the patterns. A definition without a name or pattern, an empty
// pattern, an unknown colour or two definitions with the same name are
// errors; commands that never name a project do not see them. Without
// definitions the resolver leaves every key as it is.
func (c *Config) ProjectResolver() (project.Resolver, error) {
	defs := make([]project.Definition, 0, len(c.Projects))
	for i, p := range c.Projects {
		d, err := project.NewDefinition(p.Name, p.Description, p.Color, p.Match)
		if err != nil {
			return project.Resolver{}, fmt.Errorf("config: project[%d]: %w", i, err)
		}
		defs = append(defs, d)
	}
	r, err := project.NewResolver(defs, c.Home)
	if err != nil {
		return project.Resolver{}, fmt.Errorf("config: %w", err)
	}
	return r, nil
}

// ValidateTUI checks the values of the [tui] table.
func (c *Config) ValidateTUI() error {
	for _, v := range []struct {
		key, value string
		allowed    []string
	}{
		{"tui.view", c.TUI.View, TUIViews},
		{"tui.graphics", c.TUI.Graphics, TUIGraphics},
		{"tui.stats_period", c.TUI.StatsPeriod, TUIPeriods},
		{"tui.stats_dimension", c.TUI.StatsDimension, TUIDimensions},
		{"tui.activity_window", c.TUI.ActivityWindow, TUIPeriods},
		{"tui.activity_newest", c.TUI.ActivityNewest, TUINewest},
	} {
		if !slices.Contains(v.allowed, v.value) {
			return fmt.Errorf("config: %s %q: use %s", v.key, v.value, orList(v.allowed))
		}
	}
	if err := ValidReload(c.TUI.Reload.Duration); err != nil {
		return fmt.Errorf("config: tui.%w", err)
	}
	return nil
}

// ChartSettings validates the [chart] table and the colours of the
// [[worktime.category]] rules and returns them as `total-recall chart`
// uses them; colours are normalized to #rrggbb. The error names the first
// bad value; commands that draw no charts never see it.
func (c *Config) ChartSettings() (ChartSettings, error) {
	ch := c.Chart
	s := ChartSettings{Format: ch.Format, Scale: ch.Scale, Title: ch.Title, Legend: ch.Legend, Transparent: ch.Transparent, Top: ch.Top, CategoryColors: map[string]string{}}
	if !slices.Contains(ChartFormats, ch.Format) {
		return s, fmt.Errorf("config: chart.format %q: use %s", ch.Format, orList(ChartFormats))
	}
	if ch.Scale <= 0 || ch.Scale > 16 {
		return s, fmt.Errorf("config: chart.scale %g: want between 0 and 16", ch.Scale)
	}
	if ch.Top < 0 {
		return s, fmt.Errorf("config: chart.top %d: want 0 (all projects) or more", ch.Top)
	}
	lang, err := figure.ParseLang(ch.Lang)
	if err != nil {
		return s, fmt.Errorf("config: chart.lang %q: use %s", ch.Lang, orList(ChartLangs))
	}
	s.Lang = lang
	if len(ch.Heatmap) == 0 {
		return s, fmt.Errorf("config: chart.heatmap: want at least one colour")
	}
	for i, h := range ch.Heatmap {
		p, err := project.ParseColor(h)
		if err != nil || p == "" {
			return s, fmt.Errorf("config: chart.heatmap[%d]: %s", i, colorError(h, err))
		}
		s.Heatmap = append(s.Heatmap, p)
	}
	for i, cat := range c.Worktime.Categories {
		p, err := project.ParseColor(cat.Color)
		if err != nil {
			return s, fmt.Errorf("config: worktime.category[%d]: %w", i, err)
		}
		if p != "" {
			s.CategoryColors[cat.Name] = p
		}
	}
	return s, nil
}

// colorError words the error of an empty or bad colour.
func colorError(s string, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("color %q: want a colour", s)
}

// KeyMap resolves the [keys] table against the defaults and validates it:
// unknown actions, bad key names and conflicts are errors (see package
// keys). Commands that do not use the key bindings never see them.
func (c *Config) KeyMap() (keys.Map, error) {
	overrides := make(map[string][]string, len(c.Keys))
	for name, l := range c.Keys {
		overrides[name] = l
	}
	m, err := keys.Resolve(overrides)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return m, nil
}

// Problems returns the errors of the tables that Load does not validate
// ([work], [worktime], [tui], [chart] with the category colours, [keys],
// [[project]]), in that order; nil when they are all fine.
func (c *Config) Problems() []error {
	var out []error
	if _, _, err := c.WorkHours(); err != nil {
		out = append(out, err)
	}
	if _, err := c.WorktimeSettings(); err != nil {
		out = append(out, err)
	}
	if err := c.ValidateTUI(); err != nil {
		out = append(out, err)
	}
	if _, err := c.ChartSettings(); err != nil {
		out = append(out, err)
	}
	if _, err := c.KeyMap(); err != nil {
		out = append(out, err)
	}
	if _, err := c.ProjectResolver(); err != nil {
		out = append(out, err)
	}
	return out
}

// orList joins "a, b or c".
func orList(l []string) string {
	if len(l) < 2 {
		return strings.Join(l, "")
	}
	return strings.Join(l[:len(l)-1], ", ") + " or " + l[len(l)-1]
}

// Origin returns where the value of key (dotted, e.g. "backup.dir") came from.
func (c *Config) Origin(key string) Origin {
	if o, ok := c.origins[key]; ok {
		return o
	}
	return OriginDefault
}

// Entries returns all effective values with their origin, in a stable
// order: the fields, then every key-bound action of the TUI with the keys
// as configured (or the default), whether or not they resolve.
func (c *Config) Entries() []Entry {
	out := make([]Entry, 0, len(fields)+len(keys.Actions))
	for _, k := range fields {
		out = append(out, Entry{Key: k.key, Value: k.format(c), Origin: c.Origin(k.key)})
	}
	for _, a := range keys.Actions {
		k := "keys." + a.Name
		ks := a.Keys
		if l, ok := c.Keys[a.Name]; ok {
			ks = l
		}
		out = append(out, Entry{Key: k, Value: formatList(ks), Origin: c.Origin(k)})
	}
	return out
}

// parseClock parses "HH:MM" (hour 0-23, minute 0-59) as an offset from
// midnight.
func parseClock(s string) (time.Duration, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || strings.Count(s, ":") != 1 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("want HH:MM between 00:00 and 23:59")
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

// DayStartOffset returns the configured day boundary as an offset from
// local midnight (04:00 = 4h).
func (c *Config) DayStartOffset() time.Duration { return c.dayStart }

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

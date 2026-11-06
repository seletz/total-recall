package config

import (
	"strings"

	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/tui/keys"
	"github.com/seletz/total-recall/internal/worktime"
)

// Kind is the type of a configuration value, which decides how the TUI
// edits it and how SetValue parses the text.
type Kind string

// Kinds of values.
const (
	// KindString is free text.
	KindString Kind = "string"
	// KindPath is a path; "~" and environment variables are expanded.
	KindPath Kind = "path"
	// KindClock is a wall-clock time "HH:MM".
	KindClock Kind = "clock"
	// KindBool is true or false.
	KindBool Kind = "bool"
	// KindInt is a non-negative integer.
	KindInt Kind = "int"
	// KindFloat is a decimal number such as "1.5".
	KindFloat Kind = "float"
	// KindDuration is a duration such as "20m" or "24h"; "0" is allowed.
	KindDuration Kind = "duration"
	// KindEnum is one of Key.Enum.
	KindEnum Kind = "enum"
	// KindList is a list of strings: a TOML array or words separated by
	// commas.
	KindList Kind = "list"
	// KindTable is a table or an array of tables ([accounts],
	// [[worktime.category]]): shown as a whole, edited in the file.
	KindTable Kind = "table"
)

// Key describes one configuration key for the settings editor.
type Key struct {
	Name string
	Kind Kind
	// Enum lists the allowed values of a KindEnum key.
	Enum []string
	// Help is a one-line description.
	Help string
}

// Catalog describes every key of fields, in the same order. The key
// bindings ("keys.<action>") are not listed; Lookup resolves them from the
// action catalogue.
var Catalog = []Key{
	{"db", KindPath, nil, "Path of the archive database; keep it out of cloud-synced folders"},
	{"timezone", KindString, nil, "Time zone for evaluations (stats, day boundaries), e.g. Europe/Berlin"},
	{"day_start", KindClock, nil, "Wall-clock time at which a day begins for the activity per day"},
	{"log", KindPath, nil, "Log file for hook errors; empty = total-recall.log next to the database"},
	{"backup.dir", KindPath, nil, "Directory of the backups (archive-YYYY-MM-DD.db.zst)"},
	{"backup.keep_daily", KindInt, nil, "Number of most recent days whose copies are kept"},
	{"backup.keep_monthly", KindInt, nil, "Number of monthly copies to keep; 0 = keep all"},
	{"backup.min_interval", KindDuration, nil, "Minimum age of the last backup for `backup --if-older` (currently unused)"},
	{"redact.enabled", KindBool, nil, "Mask secrets in text and raw JSON before they are stored"},
	{"redact.extra_patterns", KindList, nil, "Additional Go regular expressions to mask (group 1 if present)"},
	{"redact.allow", KindList, nil, "Regular expressions; a candidate secret matching one is never masked"},
	{"ingest.tool_result_max_bytes", KindInt, nil, "Tool results are truncated to this many bytes"},
	{"ingest.raw_max_bytes", KindInt, nil, "Longer raw lines are not kept (raw = NULL, raw_truncated = 1); 0 = no limit (default)"},
	{"ingest.store_raw", KindBool, nil, "Keep the (redacted) original JSON line of every event"},
	{"ingest.store_blobs", KindBool, nil, "Keep images and documents of the lines as blobs (once per hash); they cannot be redacted"},
	{"ingest.store_memory", KindBool, nil, "Archive the project memory of the harness (Claude Code: memory/*.md), versioned and redacted"},
	{"harness.claude.enabled", KindBool, nil, "Read Claude Code transcripts"},
	{"harness.claude.projects_dir", KindPath, nil, "Transcript directory; default $CLAUDE_CONFIG_DIR/projects or ~/.claude/projects"},
	{"harness.claude.skip_record_types", KindList, nil, "Record types to skip in addition to the built-in exclusions; attachment/<subtype> skips one attachment subtype"},
	{"harness.claude.actors.peer_prefixes", KindList, nil, "Message prefixes of tasks from other agents (actor peer)"},
	{"harness.claude.actors.peer_paste_headers", KindList, nil, "First lines of a pasted block that make it a task from another agent"},
	{"harness.claude.actors.automation_prefixes", KindList, nil, "Message prefixes of scheduled automations (actor automation)"},
	{"git.resolve", KindBool, nil, "Resolve git root, remote URL and branch of the session directories"},
	{"work.hours", KindString, nil, "Core hours HH:MM-HH:MM; activity outside them is marked early or late"},
	{"work.target", KindDuration, nil, "Daily target on weekdays; weekends and holidays have none"},
	{"work.daily_low", KindDuration, nil, "A day's span below this counts as under the target"},
	{"work.daily_high", KindDuration, nil, "A day's span above this counts as over the target"},
	{"work.weekly_low", KindDuration, nil, "A week's working time below this counts as under (scaled by workdays)"},
	{"work.weekly_high", KindDuration, nil, "A week's working time above this counts as over (scaled by workdays)"},
	{"work.bundesland", KindEnum, holiday.Regions(), "Bundesland for the public holidays"},
	{"work.count", KindEnum, []string{CountHuman, CountAll}, "Which events the activity per day counts by default"},
	{"worktime.max_gap", KindDuration, nil, "Longest gap between two own messages that counts in full"},
	{"worktime.min_credit", KindDuration, nil, "What a longer gap (break, meeting, first message of a day) counts"},
	{"worktime.grid", KindDuration, nil, "Rounding grid of the working time per category and day; 0 = no rounding"},
	{"worktime.rounding", KindEnum, worktime.Roundings, "Rounding direction"},
	{"worktime.scope", KindEnum, worktime.Scopes, "What is rounded: the category per day or every project per day"},
	{"worktime.summary_max_chars", KindInt, nil, "Length of the booking text per category and day; 0 = no limit"},
	{"worktime.category", KindTable, nil, "Category rules [[worktime.category]] with name, patterns and colour, first match wins; edit them in the file"},
	{"tui.view", KindEnum, TUIViews, "View shown first"},
	{"tui.graphics", KindEnum, TUIGraphics, "Chart rendering in the stats view"},
	{"tui.mouse", KindBool, nil, "Wheel scrolling and clicks; false leaves the mouse to the terminal"},
	{"tui.stats_period", KindEnum, TUIPeriods, "Period the stats view starts with"},
	{"tui.stats_dimension", KindEnum, TUIDimensions, "Dimension the stats view starts with"},
	{"tui.activity_window", KindEnum, TUIPeriods, "Window the activity view starts with"},
	{"tui.activity_projects", KindBool, nil, "Activity view starts with the bars in project colours"},
	{"tui.activity_newest", KindEnum, TUINewest, "Where the activity view puts the newest day"},
	{"tui.reload", KindDuration, nil, "How often the TUI checks the archive for changes; 0 = off, at least 1s"},
	{"chart.format", KindEnum, ChartFormats, "Output format of chart"},
	{"chart.scale", KindFloat, nil, "Multiplier of the natural size of a figure"},
	{"chart.title", KindBool, nil, "Draw the heading of a figure"},
	{"chart.legend", KindBool, nil, "Draw the legend of a figure"},
	{"chart.transparent", KindBool, nil, "Leave the white background out"},
	{"chart.top", KindInt, nil, "Projects the project chart shows, the rest is other; 0 = all"},
	{"chart.heatmap", KindList, nil, "Colour scale of the calendar, light to dark: palette names or #rrggbb"},
	{"chart.lang", KindEnum, ChartLangs, "Language of the labels of a figure: en or de"},
	{"accounts", KindTable, nil, "Names for account and organization IDs; edit them in the file"},
	{"project", KindTable, nil, "Project definitions [[project]]: name, colour, patterns; see the projects page, edit them in the file"},
}

// Lookup returns the description of key: a Catalog entry, a key binding
// ("keys.<action>", a list) or an account name ("accounts.<id>", a string).
func Lookup(key string) (Key, bool) {
	for _, k := range Catalog {
		if k.Name == key {
			return k, true
		}
	}
	if name, ok := strings.CutPrefix(key, "keys."); ok {
		a, ok := keys.Lookup(name)
		if !ok {
			return Key{}, false
		}
		return Key{Name: key, Kind: KindList, Help: "Keys of " + a.Help + " (" + string(a.Context) + ")"}, true
	}
	if id, ok := strings.CutPrefix(key, "accounts."); ok && id != "" {
		return Key{Name: key, Kind: KindString, Help: "Name shown for the account or organization ID " + id}, true
	}
	return Key{}, false
}

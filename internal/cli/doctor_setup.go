package cli

import (
	"fmt"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/ops"
	"github.com/seletz/total-recall/internal/version"
)

// DoctorSetupJSON is the JSON output of doctor --setup: the checks of the
// setup page of the TUI. OK is true when every check is ok.
type DoctorSetupJSON struct {
	OK     bool             `json:"ok"`
	Checks []SetupCheckJSON `json:"checks"`
}

// SetupCheckJSON is one check. OK is a tick, Warn a hint on an ok check;
// neither is a failure. Text is the line the setup page shows, with "~" for
// the home directory. Fix is null, or names what the TUI offers to do
// about it (doctor --setup never writes).
type SetupCheckJSON struct {
	Name string     `json:"name"`
	OK   bool       `json:"ok"`
	Warn bool       `json:"warn"`
	Text string     `json:"text"`
	Fix  NullString `json:"fix"`
}

// doctorSetup runs the setup checks of the config page and reports them.
func (e *env) doctorSetup(cfg *config.Config, o *output) error {
	checks := ops.SetupChecks(ops.SetupEnv{Config: cfg, Home: e.app.Home, Version: version.Version, Getenv: e.app.Getenv}, e.app.Now())
	out := DoctorSetupJSON{OK: true, Checks: make([]SetupCheckJSON, 0, len(checks))}
	for _, c := range checks {
		out.OK = out.OK && c.OK
		out.Checks = append(out.Checks, SetupCheckJSON{Name: c.Name, OK: c.OK, Warn: c.Warn, Text: c.Text, Fix: NullString(c.Fix)})
	}
	if o.format.IsJSON() {
		return o.object(out)
	}
	rows := make([][]string, 0, len(out.Checks))
	for _, c := range out.Checks {
		state := "ok"
		switch {
		case !c.OK:
			state = "fail"
		case c.Warn:
			state = "warn"
		}
		text := c.Text
		if c.Fix != "" {
			text += fmt.Sprintf(" (TUI fix: %s)", c.Fix)
		}
		rows = append(rows, []string{c.Name, state, text})
	}
	return o.table([]string{"CHECK", "STATE", "TEXT"}, rows)
}

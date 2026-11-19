package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/tui/keys"
)

// ConfigChangeJSON is the JSON output of config set and config unset: the
// key as it is effective afterwards. Every key is always present; Value
// and Default are null when empty. Changed is false when the file already
// had the value (set) or did not set the key (unset), then nothing was
// written.
type ConfigChangeJSON struct {
	File    string     `json:"file"`
	Key     string     `json:"key"`
	Value   NullString `json:"value"`
	Origin  string     `json:"origin"`
	Default NullString `json:"default"`
	Changed bool       `json:"changed"`
}

// configEdit is the shared body of config set and unset: it loads the
// configuration, applies edit, and reports the key afterwards. Only the
// config file is written; the archive is not opened. Edits the file
// functions refuse are validation errors, file errors stay internal.
func (e *env) configEdit(o *output, key string, edit func(*config.Config) (*config.Config, error)) error {
	cfg, err := e.loadConfig()
	if err != nil {
		return err
	}
	before := configValue(cfg, key)
	n, err := edit(cfg)
	if err != nil {
		if config.IsInvalid(err) {
			return errKind(ErrKindValidation, err)
		}
		return err
	}
	out := ConfigChangeJSON{
		File:    n.Path,
		Key:     key,
		Value:   NullString(configValue(n, key)),
		Origin:  string(n.Origin(key)),
		Default: NullString(n.Default(key)),
	}
	out.Changed = out.Value != NullString(before) || n.Origin(key) != cfg.Origin(key)
	if o.format.IsJSON() {
		return o.object(out)
	}
	_, err = fmt.Fprintf(o.w, "%s = %s (%s)\n", out.Key, out.Value, out.Origin)
	return err
}

// configValue returns the effective value of key as config show prints it.
func configValue(c *config.Config, key string) string {
	for _, en := range c.Entries() {
		if en.Key == key {
			return en.Value
		}
	}
	return ""
}

func (e *env) configSetCmd() *cobra.Command {
	var of outputFlags
	c := &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set a configuration key in the config file (comments and order are kept)",
		Long: `Write one key into the config file, like the settings page of the TUI: the
value is checked against the kind of the key (see config show --json, catalog)
and the result must load without a new problem. The previous file is kept as
<file>.bak. Only the config file changes, never the archive.`,
		Args: usageArgs(cobra.ExactArgs(2)),
	}
	c.RunE = e.readRun(&of, func(_ *cobra.Command, args []string, o *output) error {
		return e.configEdit(o, args[0], func(cfg *config.Config) (*config.Config, error) {
			return cfg.SetValue(args[0], args[1])
		})
	})
	of.register(c)
	return c
}

func (e *env) configUnsetCmd() *cobra.Command {
	var of outputFlags
	c := &cobra.Command{
		Use:   "unset KEY",
		Short: "Remove a configuration key from the config file (back to the default)",
		Long: `Comment the key out in the config file, so the default (or an environment
variable) applies again. A key the file does not set is left alone.`,
		Args: usageArgs(cobra.ExactArgs(1)),
	}
	c.RunE = e.readRun(&of, func(_ *cobra.Command, args []string, o *output) error {
		return e.configEdit(o, args[0], func(cfg *config.Config) (*config.Config, error) {
			return cfg.Reset(args[0])
		})
	})
	of.register(c)
	return c
}

// configCatalog lists the keys of config.Catalog and then every key
// binding, as the settings page of the TUI offers them.
func configCatalog(cfg *config.Config) []ConfigKeyJSON {
	out := make([]ConfigKeyJSON, 0, len(config.Catalog))
	add := func(k config.Key) {
		enum := append([]string{}, k.Enum...)
		def := cfg.Default(k.Name)
		if k.Kind == config.KindTable {
			def = ""
		}
		out = append(out, ConfigKeyJSON{Key: k.Name, Kind: string(k.Kind), Enum: enum, Default: NullString(def), Help: k.Help})
	}
	for _, k := range config.Catalog {
		add(k)
	}
	for _, a := range keys.Actions {
		if k, ok := config.Lookup("keys." + a.Name); ok {
			add(k)
		}
	}
	return out
}

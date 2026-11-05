package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/seletz/total-recall/internal/config/tomledit"
)

// SetValue changes one key of the configuration file: text is parsed by
// the key's Kind (see Lookup), the value is written into the file with
// its comments and order kept (tomledit), a missing file is created from
// the template first. Before anything is written, the result is loaded
// like Load and must not have a problem the current configuration does
// not have; the returned Config is the new effective configuration. The
// previous file is kept as "<file>.bak". A value that an environment
// variable or a flag overrides is written all the same; the returned
// configuration still shows the override.
func (c *Config) SetValue(key, text string) (*Config, error) {
	k, ok := Lookup(key)
	if !ok {
		return nil, fmt.Errorf("config: unknown key %q", key)
	}
	if k.Kind == KindTable {
		return nil, fmt.Errorf("config: %s is a table; edit it in the file", key)
	}
	v, err := parseValue(k, text)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", key, err)
	}
	return c.edit(key, func(d *tomledit.Document) error { return d.Set(key, v) })
}

// Reset removes key from the file (its line is commented out), so the
// default, the environment or a flag applies again. A key the file does
// not set is left alone.
func (c *Config) Reset(key string) (*Config, error) {
	k, ok := Lookup(key)
	if !ok {
		return nil, fmt.Errorf("config: unknown key %q", key)
	}
	if k.Kind == KindTable {
		return nil, fmt.Errorf("config: %s is a table; edit it in the file", key)
	}
	if c.Origin(key) != OriginFile && !c.fileSets(key) {
		return c, nil
	}
	return c.edit(key, func(d *tomledit.Document) error { return d.Unset(key) })
}

// fileSets reports whether the file sets key, whatever overrides it.
func (c *Config) fileSets(key string) bool {
	_, ok := c.fileKeys[key]
	return ok
}

// edit applies fn to the file text (or the template), verifies that
// exactly key changed, validates the result and writes it.
func (c *Config) edit(key string, fn func(*tomledit.Document) error) (*Config, error) {
	old, err := os.ReadFile(c.Path)
	if errors.Is(err, fs.ErrNotExist) {
		old = []byte(Template)
	} else if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	d := tomledit.Parse(string(old))
	if err := fn(d); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	text := d.String()
	diff, err := tomledit.Diff(string(old), text)
	if err != nil {
		return nil, fmt.Errorf("config: edit of %s produced invalid TOML: %w", key, err)
	}
	// Nothing but key may differ; nothing at all is fine (the value was
	// already there, perhaps spelled differently).
	if len(diff) > 1 || len(diff) == 1 && diff[0] != key {
		return nil, fmt.Errorf("config: edit of %s would change %v; the file was not written", key, diff)
	}
	n, err := load(c.opts, []byte(text))
	if err != nil {
		return nil, err
	}
	if p := newProblems(c.Problems(), n.Problems()); len(p) > 0 {
		return nil, p[0]
	}
	if err := tomledit.Save(c.Path, text); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return n, nil
}

// newProblems returns the problems of after that before did not have.
func newProblems(before, after []error) []error {
	seen := map[string]bool{}
	for _, e := range before {
		seen[e.Error()] = true
	}
	var out []error
	for _, e := range after {
		if !seen[e.Error()] {
			out = append(out, e)
		}
	}
	return out
}

// parseValue converts the text of a value of kind k into what tomledit
// writes.
func parseValue(k Key, text string) (any, error) {
	text = strings.TrimSpace(text)
	switch k.Kind {
	case KindBool:
		switch text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%q: want true or false", text)
	case KindInt:
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, fmt.Errorf("%q: want an integer", text)
		}
		return n, nil
	case KindDuration:
		d, err := time.ParseDuration(text)
		if err != nil {
			return nil, fmt.Errorf("%q: want a duration such as 20m, 1h30m or 0", text)
		}
		return FormatDuration(d), nil
	case KindList:
		return parseList(text)
	case KindEnum:
		for _, e := range k.Enum {
			if e == text {
				return text, nil
			}
		}
		return nil, fmt.Errorf("%q: use %s", text, orList(k.Enum))
	}
	return text, nil
}

// parseList reads a TOML array of strings, or words separated by commas.
func parseList(text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	if strings.HasPrefix(text, "[") {
		var v struct{ V []string }
		if _, err := toml.Decode("V = "+text, &v); err != nil {
			return nil, fmt.Errorf("%q: want a TOML list of strings", text)
		}
		if v.V == nil {
			v.V = []string{}
		}
		return v.V, nil
	}
	var out []string
	for _, w := range strings.Split(text, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out, nil
}

// FormatDuration writes d the short way the template does: "24h" rather
// than "24h0m0s", "1h30m", "20m", "0" for zero.
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0"
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// Reload loads the configuration again with the options it was loaded
// with, after the file was edited by other means.
func (c *Config) Reload() (*Config, error) { return load(c.opts, nil) }

// Default returns the built-in value of key as Entries formats it, "" for
// an unknown key.
func (c *Config) Default(key string) string {
	return c.defaultValues[key]
}

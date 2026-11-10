// Package dispatch recognizes human inputs that another session typed into
// this one: a coordinator runs a command such as `herdr agent prompt w1 "…"`
// or `tmux send-keys`, and the text arrives in the worker's terminal, where
// it looks like the person typed it. The recognition matches the input with
// the argument of a tool call of another session shortly before it.
package dispatch

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultWindow is how long before the input the tool call may be.
	DefaultWindow = 120 * time.Second
	// MinChars is the length (normalized) a text needs to be matched against
	// any tool call: short words like "ok" occur in too many arguments.
	MinChars = 12
	// maxCompare is how much of a long text is compared; the tool input is
	// cut when stored, so the end of a long prompt may be missing.
	maxCompare = 200
)

// DefaultCommands are the commands known to send text to another session.
// A text shorter than MinChars is only matched against calls that contain
// one of them.
var DefaultCommands = []string{"herdr agent prompt", "tmux send-keys"}

// DefaultSpawnCommands are the commands known to start a worker session.
var DefaultSpawnCommands = []string{"herdr agent start"}

// minBranch is the shortest branch name that counts as a reference to a
// session; the default branches never do.
const minBranch = 4

var defaultBranches = map[string]bool{"main": true, "master": true, "develop": true, "HEAD": true}

// Config tunes the matching. The zero value takes the defaults.
type Config struct {
	// Window is the time a call may precede the input; 0 = DefaultWindow,
	// negative turns the detection off.
	Window time.Duration
	// Commands are substrings of tool arguments that send text to another
	// session; nil = DefaultCommands.
	Commands []string
	// SpawnCommands are substrings of tool arguments that start a worker
	// session (naming its working directory or branch); nil =
	// DefaultSpawnCommands.
	SpawnCommands []string
}

// Call is a tool call of a session with its argument text.
type Call struct {
	Session string
	TS      time.Time
	Text    string
}

type entry struct {
	Call
	norm   string
	prompt bool // the argument contains one of the configured commands
	spawn  bool // the argument contains one of the spawn commands
}

// Index holds the tool calls of a time range for lookups by time.
type Index struct {
	cfg   Config
	calls []entry // by TS
}

// NewIndex indexes calls under cfg.
func NewIndex(calls []Call, cfg Config) *Index {
	if cfg.Window == 0 {
		cfg.Window = DefaultWindow
	}
	if cfg.Commands == nil {
		cfg.Commands = DefaultCommands
	}
	if cfg.SpawnCommands == nil {
		cfg.SpawnCommands = DefaultSpawnCommands
	}
	ix := &Index{cfg: cfg}
	if cfg.Window < 0 {
		return ix
	}
	for _, c := range calls {
		e := entry{Call: c, norm: Normalize(c.Text), prompt: containsAny(c.Text, cfg.Commands), spawn: containsAny(c.Text, cfg.SpawnCommands)}
		ix.calls = append(ix.calls, e)
	}
	sort.SliceStable(ix.calls, func(i, j int) bool { return ix.calls[i].TS.Before(ix.calls[j].TS) })
	return ix
}

// Sent reports whether the input text in session at ts was sent by a tool
// call of another session: that call is at most the window before ts and its
// argument contains the text, ignoring white space and quotes.
func (ix *Index) Sent(session string, ts time.Time, text string) bool {
	if len(ix.calls) == 0 {
		return false
	}
	n := Normalize(text)
	if len(n) > maxCompare {
		n = n[:maxCompare]
	}
	if n == "" {
		return false
	}
	from := ts.Add(-ix.cfg.Window)
	i := sort.Search(len(ix.calls), func(i int) bool { return !ix.calls[i].TS.Before(from) })
	for ; i < len(ix.calls) && !ix.calls[i].TS.After(ts); i++ {
		c := ix.calls[i]
		if c.Session == session || (len(n) < MinChars && !c.prompt) {
			continue
		}
		if strings.Contains(c.norm, n) {
			return true
		}
	}
	return false
}

var (
	pasteTag = regexp.MustCompile(`</?pasted_content[^>]*>`)
	escapes  = strings.NewReplacer(`\n`, "", `\t`, "", `\r`, "")
)

// Normalize prepares a text for comparison: pasted-block tags, white space,
// quotes and backslashes are removed, so a prompt compares equal in a shell
// argument and in the terminal.
func Normalize(s string) string {
	s = pasteTag.ReplaceAllString(s, "")
	s = escapes.Replace(s)
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '"', '\'', '`', '\\':
			return -1
		}
		return r
	}, s)
}

func containsAny(text string, subs []string) bool {
	for _, s := range subs {
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// Spawned reports whether the session was started by another session and
// its first prompt delivered by it, which the text comparison (Sent) cannot
// see when the prompt came from a file, a variable or a loop: within the
// window before ts, a tool call of another session ran a spawn or prompt
// command that names the session's working directory or its branch, and the
// same session then ran a prompt command (the delivery, at or after the
// naming call, not after ts). branch and cwd are the session's own; the
// default branches and very short names are no reference. The caller asks
// this for the first human input of a session only: a person who types into
// a worker later stays human.
func (ix *Index) Spawned(session string, ts time.Time, branch, cwd string) bool {
	if len(ix.calls) == 0 {
		return false
	}
	var names []string
	if len(cwd) > 8 {
		names = append(names, cwd)
	}
	if len(branch) >= minBranch && !defaultBranches[branch] {
		names = append(names, branch)
	}
	if len(names) == 0 {
		return false
	}
	from := ts.Add(-ix.cfg.Window)
	lo := sort.Search(len(ix.calls), func(i int) bool { return !ix.calls[i].TS.Before(from) })
	hi := sort.Search(len(ix.calls), func(i int) bool { return ix.calls[i].TS.After(ts) })
	for i := lo; i < hi; i++ {
		c := ix.calls[i]
		if c.Session == session || (!c.spawn && !c.prompt) || !containsAny(c.Text, names) {
			continue
		}
		for j := i; j < hi; j++ { // the delivery: a prompt command of the same session, at or after this call
			if d := ix.calls[j]; d.Session == c.Session && d.prompt {
				return true
			}
		}
	}
	return false
}

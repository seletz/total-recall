package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
)

// defaultTextWidth is the default of timeline --text-width: runes of the
// text kept per event.
const defaultTextWidth = 160

// timelineFieldNames are the keys of a timeline event in output order, the
// names --fields takes.
var timelineFieldNames = []string{"event_id", "ts", "ts_local", "session_id", "session_name", "project", "repo",
	"actor", "input_kind", "kind", "text", "text_chars", "text_truncated"}

// fieldsAll is the --fields value for every field, the default of the CLI.
const fieldsAll = "all"

// mcpTimelineFields is the --fields default of the MCP tool timeline: what
// tells when, who and what, without the UTC time (ts_local has the offset),
// repo, session_name and text_chars, which are about 40 % of the bytes of an
// event. A page carries about 45 % more events (#259); fields=all brings
// the rest back.
const mcpTimelineFields = "event_id,ts_local,session_id,project,actor,input_kind,kind,text,text_truncated"

// parseTimelineFields resolves --fields to the set of keys to keep; nil
// keeps every key. Names are comma separated, spaces and repeats are
// ignored, "all" or empty means every key.
func parseTimelineFields(arg string) (map[string]bool, error) {
	keep := map[string]bool{}
	for _, name := range strings.Split(arg, ",") {
		name = strings.TrimSpace(name)
		switch {
		case name == "":
		case name == fieldsAll:
			return nil, nil
		case slices.Contains(timelineFieldNames, name):
			keep[name] = true
		default:
			return nil, usagef("invalid --fields %q (%s, or %s)", name, strings.Join(timelineFieldNames, ", "), fieldsAll)
		}
	}
	if len(keep) == 0 || len(keep) == len(timelineFieldNames) {
		return nil, nil
	}
	return keep, nil
}

// timelineSubset is a timeline event reduced to some keys, in the order of
// the whole object.
type timelineSubset struct {
	ev   TimelineEventJSON
	keep map[string]bool
}

// MarshalJSON writes the kept keys with the values of the whole event.
func (s timelineSubset) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(s.ev)
	if err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for _, k := range timelineFieldNames {
		if !s.keep[k] {
			continue
		}
		if out.Len() > 1 {
			out.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		out.Write(kb)
		out.WriteByte(':')
		out.Write(all[k])
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// actorAll is the --actor value that takes events of every actor.
const actorAll = "all"

// timelineActors are the values of --actor; the first is the default.
var timelineActors = []string{"human", "agent", "peer", "automation", "tool", actorAll}

// checkTimelineArgs rejects what would only give an empty list: an
// unknown actor and a window that does not start before it ends. A date
// as --until includes its whole day (parseTimeArg), so a single day is a
// valid window.
func checkTimelineArgs(actor string, since, until time.Time) error {
	if !slices.Contains(timelineActors, actor) {
		return usagef("invalid --actor %q (%s)", actor, strings.Join(timelineActors, ", "))
	}
	if !since.IsZero() && !until.IsZero() && !since.Before(until) {
		return usagef("--since (%s) must be before --until (%s)", since.Format("2006-01-02 15:04"), until.Format("2006-01-02 15:04"))
	}
	return nil
}

// TimelineEventJSON is one event of timeline --json. Every key is always
// present; a NullString without a value is null. Text is on one line and
// at most --text-width runes; TextChars is the length of the whole text
// in runes and TextTruncated says it was cut, so nothing is cut silently.
type TimelineEventJSON struct {
	EventID     string     `json:"event_id"`
	TS          string     `json:"ts"`       // UTC
	TSLocal     string     `json:"ts_local"` // RFC 3339 in the configured time zone
	SessionID   string     `json:"session_id"`
	SessionName NullString `json:"session_name"`
	Project     NullString `json:"project"` // name as in stats --by project
	Repo        NullString `json:"repo"`
	Actor       string     `json:"actor"`
	// InputKind says how a human input arrived (typed, pasted, bash,
	// remote); null for other actors and when unknown (since v1.6.0).
	InputKind     NullString `json:"input_kind"`
	Kind          string     `json:"kind"`
	Text          NullString `json:"text"`
	TextChars     int        `json:"text_chars"`
	TextTruncated bool       `json:"text_truncated"`
}

// timelineJSON converts an event; width 0 keeps the whole text.
func timelineJSON(h store.TimelineEvent, cat *project.Catalog, loc *time.Location, width int) TimelineEventJSON {
	text := oneLine(h.Event.Text)
	n := utf8.RuneCountInString(text)
	trunc := width > 0 && n > width
	if trunc {
		text = cut(text, width)
	}
	return TimelineEventJSON{EventID: h.Event.ID, TS: isoTime(h.Event.TS), TSLocal: h.Event.TS.In(loc).Format(time.RFC3339),
		SessionID: h.Event.SessionID, SessionName: NullString(h.SessionName), Project: NullString(cat.Name(h.Project)),
		Repo: NullString(h.Repo), Actor: string(h.Event.Actor), InputKind: NullString(h.Event.InputKind), Kind: string(h.Event.Kind),
		Text: NullString(text), TextChars: n, TextTruncated: trunc}
}

func (e *env) timelineCmd() *cobra.Command {
	var f filterFlags
	var of outputFlags
	var actor, projectRef string
	var allKinds bool
	var textWidth int
	var fields string
	c := &cobra.Command{
		Use:   "timeline",
		Short: "Events of a time window across all sessions, oldest first",
		Long: `The timeline of what happened in a time window across all sessions:
one row per event with local and UTC time, session, project and the start
of the text. By default the messages of the human, which answers "was the
evening still work, and on what?". --since and --until take a time
(YYYY-MM-DDTHH:MM, local time) or a date; --until is exclusive, a date
includes the whole day. --project is a name as in stats --by project, else
a part of name, path or remote URL.

--since must be before --until. --actor selects whose events (human,
agent, peer, automation, tool, or all; default human), --all adds tool
calls and results, system and meta events and, without an explicit
--actor, takes every actor. The text
is cut to --text-width runes (0 = whole text); in JSON text_truncated and
text_chars say so. --fields keeps only the named keys of the JSON events
(all by default), e.g. --fields event_id,ts_local,project,text. --limit keeps the oldest events and says so on stderr.`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		if textWidth < 0 {
			return usage(errors.New("--text-width must not be negative"))
		}
		// --all without an explicit --actor means every actor: the default
		// human has no tool calls to add.
		if allKinds && !cmd.Flags().Changed("actor") {
			actor = actorAll
		}
		keep, err := parseTimelineFields(fields)
		if err != nil {
			return err
		}
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		sf, err := f.filter(cfg)
		if err != nil {
			return err
		}
		if err := checkTimelineArgs(actor, sf.Since, sf.Until); err != nil {
			return err
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		ctx := cmd.Context()
		// JSON names a directory without repository by its full path, like
		// stats --by project --json; text abbreviates the home directory.
		catCfg := *cfg
		if o.format.IsJSON() {
			catCfg.Home = ""
		}
		cat, err := loadProjectCatalog(ctx, st, &catCfg)
		if err != nil {
			return err
		}
		tf := store.TimelineFilter{SessionFilter: sf, AllKinds: allKinds, Actor: actor}
		if actor == actorAll {
			tf.Actor = ""
		}
		if projectRef != "" {
			scope, err := cat.Scope(projectRef)
			if err != nil {
				return errKind(ErrKindNotFound, err)
			}
			tf.ProjectKeys = scope.Keys
		}
		loc := cfg.Location()
		var list []store.TimelineEvent
		more := false
		if projectRef == "" || len(tf.ProjectKeys) > 0 {
			if tf.Limit > 0 {
				tf.Limit++ // one more tells that the limit cut the list
			}
			if list, err = st.Timeline(ctx, tf); err != nil {
				return errKind(ErrKindSQL, err)
			}
			if sf.Limit > 0 && len(list) > sf.Limit {
				list, more = list[:sf.Limit], true
			}
		}
		if o.format.IsJSON() {
			var werr error
			if keep == nil {
				out := make([]TimelineEventJSON, 0, len(list))
				for _, h := range list {
					out = append(out, timelineJSON(h, cat, loc, textWidth))
				}
				werr = writeList(o, out)
			} else {
				out := make([]timelineSubset, 0, len(list))
				for _, h := range list {
					out = append(out, timelineSubset{ev: timelineJSON(h, cat, loc, textWidth), keep: keep})
				}
				werr = writeList(o, out)
			}
			if werr != nil {
				return werr
			}
		} else {
			rows := make([][]string, 0, len(list))
			for _, h := range list {
				j := timelineJSON(h, cat, loc, textWidth)
				rows = append(rows, []string{h.Event.TS.In(loc).Format("2006-01-02 15:04:05"), h.Event.TS.UTC().Format("15:04:05Z"),
					shortSessionID(h.Event.SessionID), string(j.SessionName), string(j.Project), string(j.Text)})
			}
			if err := o.table([]string{"LOCAL", "UTC", "SESSION", "NAME", "PROJECT", "TEXT"}, rows); err != nil {
				return err
			}
			if len(list) == 0 {
				_, _ = fmt.Fprintln(e.app.Stderr, "no events")
			}
		}
		if more {
			_, _ = fmt.Fprintf(e.app.Stderr, "timeline: the first %d events, more in the window: --limit 0 for all\n", sf.Limit)
		}
		return nil
	})
	f.register(c, 0)
	of.register(c)
	c.Flags().StringVar(&actor, "actor", "human", "whose events: human, agent, peer, automation, tool or all (default human; all with --all)")
	c.Flags().StringVar(&projectRef, "project", "", "only this project: name as in stats --by project, else a part of name, path or remote URL")
	c.Flags().BoolVar(&allKinds, "all", false, "include tool calls and results, system and meta events, and every actor unless --actor is set (default: messages only)")
	c.Flags().IntVar(&textWidth, "text-width", defaultTextWidth, "runes of the text per event (0 = whole text)")
	c.Flags().StringVar(&fields, "fields", fieldsAll, "keys of the JSON events, comma separated: "+strings.Join(timelineFieldNames, ", ")+" (default all)")
	return c
}

// shortSessionID is the part of a session ID after the harness prefix,
// at most 8 characters: enough to pass to show.
func shortSessionID(id string) string {
	if _, rest, ok := strings.Cut(id, ":"); ok {
		id = rest
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

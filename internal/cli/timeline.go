package cli

import (
	"errors"
	"fmt"
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

// actorAll is the --actor value that takes events of every actor.
const actorAll = "all"

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

--actor selects whose events (human, agent, peer, automation, tool, or
all), --all adds tool calls and results, system and meta events. The text
is cut to --text-width runes (0 = whole text); in JSON text_truncated and
text_chars say so. --limit keeps the oldest events and says so on stderr.`,
		Args: usageArgs(cobra.NoArgs),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, _ []string, o *output) error {
		if textWidth < 0 {
			return usage(errors.New("--text-width must not be negative"))
		}
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		sf, err := f.filter(cfg)
		if err != nil {
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
			out := make([]TimelineEventJSON, 0, len(list))
			for _, h := range list {
				out = append(out, timelineJSON(h, cat, loc, textWidth))
			}
			if err := writeList(o, out); err != nil {
				return err
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
	c.Flags().StringVar(&actor, "actor", "human", "whose events: human, agent, peer, automation, tool or all")
	c.Flags().StringVar(&projectRef, "project", "", "only this project: name as in stats --by project, else a part of name, path or remote URL")
	c.Flags().BoolVar(&allKinds, "all", false, "include tool calls and results, system and meta events (default: messages only)")
	c.Flags().IntVar(&textWidth, "text-width", defaultTextWidth, "runes of the text per event (0 = whole text)")
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

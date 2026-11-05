package configview

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/seletz/total-recall/internal/ops"
)

// statusPage shows the shared state as a table of labelled rows, then the
// problems of the configuration and the errors of the log. It has no
// state of its own: the view loads the status.
type statusPage struct{}

func newStatusPage() statusPage { return statusPage{} }

func (statusPage) title() string                            { return "status" }
func (p statusPage) update(*Model, tea.Msg) (page, tea.Cmd) { return p, nil }
func (p statusPage) reload(*Model) (page, tea.Cmd)          { return p, nil }
func (statusPage) focused() bool                            { return false }
func (statusPage) shortHelp(*Model) []key.Binding           { return nil }
func (statusPage) fullHelp(*Model) [][]key.Binding          { return nil }
func (statusPage) view(m *Model, width, height int) string  { return m.statusView(width, height) }

// statusView renders the status rows; the label column is in the accent
// colour, a pending migration and errors in the warning colour.
func (m Model) statusView(width, height int) string {
	cfg, s := m.opts.Config, m.state
	if cfg == nil {
		return " " + m.th.Muted.Render("no configuration")
	}
	warn := m.th.Error.Render
	var rows [][2]string
	add := func(label, value string) { rows = append(rows, [2]string{label, value}) }
	switch {
	case !s.loaded:
		add("archive", m.tilde(cfg.DB))
		add("", m.th.Muted.Render("loading…"))
	case s.noArchive:
		add("archive", m.tilde(cfg.DB)+"  "+warn("missing: run ingest all (actions)"))
	default:
		add("archive", m.tilde(cfg.DB))
		add("size", mib(s.size)+"  "+m.th.Muted.Render(fmt.Sprintf("%d pages, %d free", s.pages.PageCount, s.pages.FreelistPages)))
		ps := strconv.Itoa(s.pages.PageSize)
		if s.pages.PageSize < ops.RecommendedPageSize {
			ps += "  " + warn(fmt.Sprintf("maintenance rebuilds with %d and saves space", ops.RecommendedPageSize))
		}
		add("page size", ps)
		schema := fmt.Sprintf("v%d, binary v%d", s.schema, s.latest)
		if s.pending() {
			schema += "  " + warn("migration pending: back up, then run migrate (actions)")
		}
		add("schema", schema)
		add("raw", rawCell(s, m.th.Muted.Render))
		add("blobs", blobCell(s, m.th.Muted.Render))
		add("meta records", metaCell(s, m.th.Muted.Render))
		add("sessions", strconv.Itoa(s.status.Sessions))
		add("events", strconv.Itoa(s.status.Events))
		add("sources", strconv.Itoa(s.status.Sources))
		harnesses := make([]string, 0, len(s.status.LastIngest))
		for h := range s.status.LastIngest {
			harnesses = append(harnesses, h)
		}
		sortStrings(harnesses)
		for _, h := range harnesses {
			t := s.status.LastIngest[h]
			if t.IsZero() {
				add("last ingest ("+h+")", m.th.Muted.Render("never"))
				continue
			}
			add("last ingest ("+h+")", t.In(m.opts.Location).Format("2006-01-02 15:04")+"  "+m.th.Muted.Render(age(m.now(), t)))
		}
	}
	if s.loaded {
		if s.backup == "" {
			add("last backup", warn("none")+"  "+m.th.Muted.Render("in "+m.tilde(cfg.Backup.Dir)))
		} else {
			add("last backup", m.tilde(s.backup)+"  "+m.th.Muted.Render(age(m.now(), s.backupAt)))
		}
	}
	add("log", m.tilde(cfg.LogPath()))
	state := "not found, defaults apply"
	if cfg.Loaded {
		state = "loaded"
	}
	add("config", m.tilde(cfg.Path)+"  "+m.th.Muted.Render(state))
	if s.err != nil {
		add("error", warn(s.err.Error()))
	}
	label := 0
	for _, r := range rows {
		label = max(label, lipgloss.Width(r[0]))
	}
	lines := make([]string, 0, len(rows)+8)
	for _, r := range rows {
		lines = append(lines, m.line(" "+m.th.Label.Render(pad(r[0], label))+"  "+r[1], width))
	}
	lines = append(lines, "")
	if len(s.problems) == 0 {
		lines = append(lines, m.line(" "+m.th.Label.Render(pad("problems", label))+"  "+m.th.Muted.Render("none"), width))
	} else {
		lines = append(lines, m.line(" "+m.th.Label.Render(pad("problems", label))+"  "+warn(plural(len(s.problems), "problem")+" (hooks and ingest are not affected)"), width))
		for _, p := range s.problems {
			lines = append(lines, m.line("   "+p.Error(), width))
		}
	}
	head := " " + m.th.Label.Render(pad("errors", label)) + "  "
	if !s.loaded {
		lines = append(lines, head+m.th.Muted.Render("loading…"))
	} else if len(s.errs) == 0 {
		lines = append(lines, head+m.th.Muted.Render("none in the last 24h"))
		if s.older > 0 {
			lines = append(lines, m.line(fmt.Sprintf("   %s older, see status --all-errors", plural(s.older, "error")), width))
		}
	} else {
		lines = append(lines, m.line(head+warn(plural(len(s.errs), "error")+" in the last 24h"), width))
		errs := s.errs
		// Newest first, as many as fit.
		avail := max(height-len(lines)-1, 1)
		if len(errs) > avail {
			errs = errs[len(errs)-avail:]
		}
		for i := len(errs) - 1; i >= 0; i-- {
			e := errs[i]
			stamp := "(no timestamp)"
			if !e.At.IsZero() {
				stamp = e.At.In(m.opts.Location).Format("2006-01-02 15:04")
			}
			lines = append(lines, m.line("   "+m.th.Muted.Render(stamp)+"  "+e.Message, width))
		}
		if s.older > 0 {
			lines = append(lines, m.line(fmt.Sprintf("   %s older, see status --all-errors", plural(s.older, "error")), width))
		}
	}
	return strings.Join(lines, "\n")
}

// rawCell is the raw row: the compressed size of the stored raw lines
// and their count, the events without one; "unknown" while the migration
// is pending. The compression ratio of `status` is estimated from a random
// sample and left out here, so the row reads the same on every load.
func rawCell(s state, muted func(...string) string) string {
	if s.pending() {
		return muted("unknown until the migration ran")
	}
	cell := "none"
	if s.raw.Rows > 0 {
		cell = fmt.Sprintf("%s in %d events", mib(s.raw.Bytes), s.raw.Rows)
	}
	if s.raw.Truncated > 0 {
		cell += "  " + muted(fmt.Sprintf("%d without raw: line too long", s.raw.Truncated))
	}
	return cell
}

// blobCell is the blobs row: the number of stored attachments and their
// size as delivered; "unknown" while the migration is pending.
func blobCell(s state, muted func(...string) string) string {
	if s.pending() {
		return muted("unknown until the migration ran")
	}
	if s.blobs.Count == 0 {
		return "none"
	}
	return fmt.Sprintf("%d attachments, %s", s.blobs.Count, mib(s.blobs.Bytes))
}

// metaCell is the meta records row: rows and compressed size of
// session_meta; "unknown" while the migration is pending.
func metaCell(s state, muted func(...string) string) string {
	if s.pending() {
		return muted("unknown until the migration ran")
	}
	if s.meta.Rows == 0 {
		return "none"
	}
	return fmt.Sprintf("%s in %d records", mib(s.meta.Bytes), s.meta.Rows)
}

// sortStrings sorts in place (a tiny wrapper, so the page reads without
// an import of sort).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func (statusPage) cursor(int) *tea.Cursor { return nil }

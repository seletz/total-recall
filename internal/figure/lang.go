package figure

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Lang is the language of the labels of a figure: the titles, legends,
// axes, weekdays and months and the date and number formats. File names
// are language-independent.
type Lang string

// Languages of the figures.
const (
	EN Lang = "en"
	DE Lang = "de"
)

// Langs lists the languages ParseLang accepts, in the order of the docs.
var Langs = []string{string(EN), string(DE)}

// ParseLang returns the Lang named by s: en or de. An empty name is EN.
func ParseLang(s string) (Lang, error) {
	switch Lang(s) {
	case "", EN:
		return EN, nil
	case DE:
		return DE, nil
	}
	return "", fmt.Errorf("invalid language %q (%s)", s, strings.Join(Langs, ", "))
}

// texts are the labels of the figures in one language. Tooltips use the
// same words as the legends.
type texts struct {
	calendarTitle, maxPerDay, total        string
	projectsTitle, agentWork, ownAttention string
	agentEvents, ownMessages               string
	punchcardTitle, agentActivity          string
	worktimeTitle, target                  string
	other                                  string
	week                                   string // format of a calendar week, "W%02d"
	date, month                            string // time layouts of a day and of a month
	decimal                                string // the decimal separator
	hours                                  string // suffix of a number of hours
	weekdays                               [7]string
}

var table = map[Lang]texts{
	EN: {
		calendarTitle: "Own messages per day", maxPerDay: "max %d/day", total: "total %d",
		projectsTitle: "Share per project", agentWork: "agent work (agent events)", ownAttention: "own attention (own messages)",
		agentEvents: "agent events", ownMessages: "own messages",
		punchcardTitle: "Messages per weekday and hour", agentActivity: "agent activity",
		worktimeTitle: "Working time per week", target: "target",
		other:    "other",
		week:     "W%02d",
		date:     "2006-01-02",
		month:    "January 2006",
		decimal:  ".",
		hours:    "h",
		weekdays: [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
	},
	DE: {
		calendarTitle: "Eigene Nachrichten je Tag", maxPerDay: "max. %d/Tag", total: "gesamt %d",
		projectsTitle: "Anteil je Projekt", agentWork: "Agenten-Arbeit (Agenten-Events)", ownAttention: "eigene Aufmerksamkeit (Nachrichten)",
		agentEvents: "Agenten-Events", ownMessages: "eigene Nachrichten",
		punchcardTitle: "Nachrichten je Wochentag und Stunde", agentActivity: "Agenten-Aktivität",
		worktimeTitle: "Arbeitszeit je Woche", target: "Soll",
		other:    "Sonstige",
		week:     "KW %02d",
		date:     "02.01.2006",
		month:    "January 2006",
		decimal:  ",",
		hours:    " h",
		weekdays: [7]string{"Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"},
	},
}

// germanMonths are the month names of DE; time.Format knows only the
// English ones.
var germanMonths = [...]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}

// text returns the labels of l; an unknown or empty language falls back
// to EN.
func (l Lang) text() texts {
	if t, ok := table[l]; ok {
		return t
	}
	return table[EN]
}

// Week labels a calendar week: "W41" in EN, "KW 41" in DE.
func (l Lang) Week(n int) string { return fmt.Sprintf(l.text().week, n) }

// Other names the folded projects of the project chart: "other" in EN,
// "Sonstige" in DE.
func (l Lang) Other() string { return l.text().other }

// Weekday names a weekday, 0 = Monday, as the tooltips use it ("Mon",
// "Mo"); the row labels of the heatmaps are its first two letters.
func (l Lang) Weekday(i int) string { return l.text().weekdays[i%7] }

// day formats a date: 2006-01-02 in EN, 02.01.2006 in DE.
func (l Lang) day(t time.Time) string { return t.Format(l.text().date) }

// monthName formats a month with its year: "October 2026", "Oktober 2026".
func (l Lang) monthName(t time.Time) string {
	if l == DE {
		return germanMonths[t.Month()-1] + " " + strconv.Itoa(t.Year())
	}
	return t.Format(l.text().month)
}

// hours formats a duration in hours with at most one decimal: "7.5h" in
// EN, "7,5 h" in DE.
func (l Lang) hours(h float64) string {
	t := l.text()
	s := strconv.FormatFloat(math.Round(h*10)/10, 'f', -1, 64)
	return strings.Replace(s, ".", t.decimal, 1) + t.hours
}

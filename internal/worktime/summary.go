package worktime

import "strings"

// promptMaxChars is the length of the excerpt of the first message that
// stands in for a missing title in the booking text.
const promptMaxChars = 60

// Summary builds the booking text of a category on one day:
// "<project>: <session>, <session>; <project>: …" with the projects by
// name and the sessions in order of their first message. A session is
// its title, followed in parentheses by the references of its messages
// that the title does not already name ("Konfig (#21)"); without a title
// it is its references ("#19/#26"), without those the first line of its
// first message, else "(untitled)". Equal labels within a project appear
// once. The text is cut to max runes, ending in "…"; 0 means no limit.
func Summary(cat Category, max int) string {
	var parts []string
	for _, p := range cat.Projects {
		var labels []string
		seen := map[string]bool{}
		for _, sess := range cat.Sessions {
			if sess.Project != p.Key {
				continue
			}
			l := sessionLabel(sess)
			if seen[l] {
				continue
			}
			seen[l] = true
			labels = append(labels, l)
		}
		if len(labels) == 0 {
			continue
		}
		parts = append(parts, p.Name+": "+strings.Join(labels, ", "))
	}
	return Truncate(strings.Join(parts, "; "), max)
}

// sessionLabel names a session in the booking text; see Summary.
func sessionLabel(sess Session) string {
	if sess.Title != "" {
		var extra []string
		known := FindRefs(sess.Title)
		for _, r := range sess.Refs {
			if !containsRef(known, r) {
				extra = append(extra, r.String())
			}
		}
		if len(extra) == 0 {
			return sess.Title
		}
		return sess.Title + " (" + strings.Join(extra, "/") + ")"
	}
	if len(sess.Refs) > 0 {
		return joinRefs(sess.Refs, "/")
	}
	if line := firstLine(sess.Prompt); line != "" {
		return Truncate(line, promptMaxChars)
	}
	return "(untitled)"
}

// containsRef reports whether refs name the same repository and number.
func containsRef(refs []Ref, r Ref) bool {
	for _, o := range refs {
		if o.Repo == r.Repo && o.Number == r.Number {
			return true
		}
	}
	return false
}

// joinRefs writes the references separated by sep.
func joinRefs(refs []Ref, sep string) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.String()
	}
	return strings.Join(parts, sep)
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

package analytics

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	jinjaConfig = regexp.MustCompile(`(?s)\{\{\s*config\(.*?\)\s*\}\}`)
	jinjaSource = regexp.MustCompile(`\{\{\s*source\(\s*['"](\w+)['"]\s*,\s*['"](\w+)['"]\s*\)\s*\}\}`)
	jinjaVar    = regexp.MustCompile(`\{\{\s*var\(\s*['"](\w+)['"]\s*\)\s*\}\}`)
	jinjaAny    = regexp.MustCompile(`\{\{|\{%|\{#`)
)

// RenderModel turns a model file of the dbt project (tools/analytics/dbt)
// into plain SQL for SQLite. It understands only the subset the models may
// use, so the same file runs in dbt and in total-recall: config(...) is
// dropped, source('schema', 'table') becomes the table name (SQLite has no
// schemas), var('name') the value of vars. Anything else in braces (ref,
// macros, control flow) is an error rather than silently wrong SQL.
func RenderModel(text string, vars map[string]string) (string, error) {
	out := jinjaConfig.ReplaceAllString(text, "")
	out = jinjaSource.ReplaceAllString(out, "$2")
	var missing []string
	out = jinjaVar.ReplaceAllStringFunc(out, func(m string) string {
		name := jinjaVar.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("analytics: model uses unknown vars %s", strings.Join(missing, ", "))
	}
	if loc := jinjaAny.FindStringIndex(out); loc != nil {
		end := min(loc[0]+40, len(out))
		return "", fmt.Errorf("analytics: model uses dbt syntax outside the supported subset: %q", out[loc[0]:end])
	}
	return out, nil
}

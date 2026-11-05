// Package tomledit edits a TOML file as text, so that comments, blank
// lines, key order and the spelling of everything it does not touch stay
// as they are. The TOML library decodes a file into values and loses the
// comments; this package instead scans the lines for table headers and
// key/value entries (including values that span lines) and changes only
// the lines of the entry or table it is asked to change.
//
// Setting a key replaces the value of its line, activates a commented-out
// line "# key = …" of the same key in the same table (the config template
// is full of those), or inserts the key after the last entry of its table;
// a missing table is created, a commented-out header "# [table]" is
// activated. Arrays of tables ("[[project]]") are addressed by index and
// can be appended and removed as a whole.
//
// The scanner is deliberately small; it understands what a configuration
// file contains, not every corner of TOML. Diff decodes two documents with
// the TOML library and names the keys whose values differ, so a caller can
// verify that an edit changed exactly what it meant to change before it
// writes the file. Save writes atomically and keeps the previous content
// as "<file>.bak".
package tomledit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
)

// Document is the text of a TOML file, line by line.
type Document struct {
	lines []string
	// eol is whether the text ended with a newline; edits always add one.
	eol bool
}

// Pair is one key and its value for AppendTable.
type Pair struct {
	Key   string
	Value any
}

// Raw is a value that is already TOML text and is written verbatim.
type Raw string

// Parse splits text into lines. It never fails: lines it does not
// understand are kept verbatim.
func Parse(text string) *Document {
	lines := strings.Split(text, "\n")
	eol := strings.HasSuffix(text, "\n")
	if eol {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 1 && lines[0] == "" && !eol {
		lines = nil
	}
	return &Document{lines: lines, eol: eol}
}

// String returns the text of the document.
func (d *Document) String() string {
	if len(d.lines) == 0 {
		return ""
	}
	s := strings.Join(d.lines, "\n")
	if d.eol {
		s += "\n"
	}
	return s
}

// ---- scanning ----

// header is a table header line "[a.b]" or "[[a.b]]".
type header struct {
	line  int
	path  []string
	array bool
}

// entry is one key/value: its table (index into the headers, -1 for the
// top level), the key relative to the table, the lines it takes and the
// columns of the value on the first and the last of them.
type entry struct {
	table      int
	key        []string
	start, end int
	valueCol   int // column in lines[start] where the value starts
	valueEnd   int // column in lines[end] after the value
}

// scan is the structure of the document: headers in file order, active
// entries, commented-out entries ("# key = value", one line) and
// commented-out headers ("# [a.b]").
type scan struct {
	headers   []header
	entries   []entry
	commented []entry
	cheaders  []header
}

func (d *Document) scan() scan {
	var s scan
	table := -1
	for i := 0; i < len(d.lines); i++ {
		t := strings.TrimSpace(d.lines[i])
		switch {
		case t == "":
		case t[0] == '#':
			rest := strings.TrimSpace(t[1:])
			if strings.HasPrefix(rest, "[") {
				if h, ok := parseHeader(rest); ok {
					h.line = i
					s.cheaders = append(s.cheaders, h)
				}
				continue
			}
			if e, ok := d.parseEntry(i, strings.Index(d.lines[i], rest)); ok && e.end == i && plausibleValue(d.lines[i][e.valueCol:e.valueEnd]) {
				e.table = table
				s.commented = append(s.commented, e)
			}
		case t[0] == '[':
			if h, ok := parseHeader(t); ok {
				h.line = i
				s.headers = append(s.headers, h)
				table = len(s.headers) - 1
			}
		default:
			if e, ok := d.parseEntry(i, 0); ok {
				e.table = table
				s.entries = append(s.entries, e)
				i = e.end
			}
		}
	}
	return s
}

// parseHeader parses "[a.b]" or "[[a.b]]" (with optional trailing comment).
func parseHeader(t string) (header, bool) {
	array := strings.HasPrefix(t, "[[")
	t = strings.TrimPrefix(strings.TrimPrefix(t, "["), "[")
	close := "]"
	if array {
		close = "]]"
	}
	end := strings.Index(t, close)
	if end < 0 {
		return header{}, false
	}
	path, rest, ok := parseKey(t[:end])
	if !ok || strings.TrimSpace(rest) != "" || len(path) == 0 {
		return header{}, false
	}
	after := strings.TrimSpace(t[end+len(close):])
	if after != "" && after[0] != '#' {
		return header{}, false
	}
	return header{path: path, array: array}, true
}

// parseEntry parses "key = value" starting at column col of line i; the
// value may continue on the following lines.
func (d *Document) parseEntry(i, col int) (entry, bool) {
	line := d.lines[i]
	key, rest, ok := parseKey(line[col:])
	if !ok || len(key) == 0 {
		return entry{}, false
	}
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "=") {
		return entry{}, false
	}
	valueCol := len(line) - len(rest) + 1
	for valueCol < len(line) && (line[valueCol] == ' ' || line[valueCol] == '\t') {
		valueCol++
	}
	end, valueEnd, ok := d.scanValue(i, valueCol)
	if !ok {
		return entry{}, false
	}
	return entry{key: key, start: i, end: end, valueCol: valueCol, valueEnd: valueEnd}, true
}

// scanValue finds the end of the value that starts at (line, col): the
// line it ends on and the column after its last character, before any
// trailing comment. Strings and nested brackets are honoured; an array
// may continue on following lines.
func (d *Document) scanValue(line, col int) (endLine, endCol int, ok bool) {
	s := d.lines[line][col:]
	for _, q := range []string{`"""`, `'''`} {
		if strings.HasPrefix(s, q) {
			return d.scanMultilineString(line, col+3, q)
		}
	}
	depth := 0
	lastNonSpace := -1
	for l := line; l < len(d.lines); l++ {
		text := d.lines[l]
		c := 0
		if l == line {
			c = col
		}
		for c < len(text) {
			ch := text[c]
			switch {
			case ch == '"' || ch == '\'':
				n, ok := skipString(text, c)
				if !ok {
					return 0, 0, false
				}
				c = n
				lastNonSpace = c
				continue
			case ch == '[' || ch == '{':
				depth++
			case ch == ']' || ch == '}':
				depth--
				if depth < 0 {
					return 0, 0, false
				}
			case ch == '#' && depth == 0:
				return l, lastNonSpace, lastNonSpace >= 0
			}
			c++
			if ch != ' ' && ch != '\t' {
				lastNonSpace = c
			}
		}
		if depth == 0 {
			return l, lastNonSpace, lastNonSpace >= 0
		}
		// The value continues on the next line; the end column restarts
		// there.
		lastNonSpace = -1
	}
	return 0, 0, false
}

// scanMultilineString finds the closing delimiter q of a multi-line
// string whose content starts at (line, col).
func (d *Document) scanMultilineString(line, col int, q string) (int, int, bool) {
	for l := line; l < len(d.lines); l++ {
		text := d.lines[l]
		start := 0
		if l == line {
			start = col
		}
		if i := strings.Index(text[start:], q); i >= 0 {
			return l, start + i + len(q), true
		}
	}
	return 0, 0, false
}

// skipString returns the index after the single-line string that starts
// at text[i] (a quote).
func skipString(text string, i int) (int, bool) {
	q := text[i]
	for j := i + 1; j < len(text); j++ {
		switch {
		case q == '"' && text[j] == '\\':
			j++
		case text[j] == q:
			return j + 1, true
		}
	}
	return 0, false
}

// plausibleValue reports whether v looks like a TOML value; a prose
// comment that happens to contain "=" is no entry.
func plausibleValue(v string) bool {
	if v == "" {
		return false
	}
	switch v {
	case "true", "false", "inf", "nan", "+inf", "-inf":
		return true
	}
	return strings.ContainsRune(`"'[{+-0123456789`, rune(v[0]))
}

// parseKey parses a dotted key (bare or quoted segments) at the start of
// s and returns the segments and the rest of s.
func parseKey(s string) (segs []string, rest string, ok bool) {
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return nil, "", false
		}
		switch s[0] {
		case '"', '\'':
			n, ok := skipString(s, 0)
			if !ok {
				return nil, "", false
			}
			seg := s[1 : n-1]
			if s[0] == '"' {
				u, err := strconv.Unquote(s[:n])
				if err != nil {
					return nil, "", false
				}
				seg = u
			}
			segs = append(segs, seg)
			s = s[n:]
		default:
			n := 0
			for n < len(s) && isBare(s[n]) {
				n++
			}
			if n == 0 {
				return nil, "", false
			}
			segs = append(segs, s[:n])
			s = s[n:]
		}
		s = strings.TrimLeft(s, " \t")
		if !strings.HasPrefix(s, ".") {
			return segs, s, true
		}
		s = s[1:]
	}
}

func isBare(c byte) bool {
	return c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// bareKey reports whether seg can be written without quotes.
func bareKey(seg string) bool {
	if seg == "" {
		return false
	}
	for i := 0; i < len(seg); i++ {
		if !isBare(seg[i]) {
			return false
		}
	}
	return true
}

// formatKey writes segments as a dotted key, quoting what is not bare.
func formatKey(segs []string) string {
	out := make([]string, len(segs))
	for i, s := range segs {
		if bareKey(s) {
			out[i] = s
		} else {
			out[i] = strconv.Quote(s)
		}
	}
	return strings.Join(out, ".")
}

func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && samePath(path[:len(prefix)], prefix)
}

// fullKey is the key of e with its table in front.
func (s scan) fullKey(e entry) []string {
	if e.table < 0 {
		return e.key
	}
	return append(append([]string{}, s.headers[e.table].path...), e.key...)
}

// regionEnd is the line after the region of header h (the index into
// headers): the line of the next header, or the end of the document.
func (d *Document) regionEnd(s scan, h int) int {
	if h+1 < len(s.headers) {
		return s.headers[h+1].line
	}
	return len(d.lines)
}

// lastEntry returns the index of the last entry of table h (-1 for the
// top level), or -1.
func (s scan) lastEntry(h int) int {
	last := -1
	for i, e := range s.entries {
		if e.table == h {
			last = i
		}
	}
	return last
}

// ---- edits ----

// Set gives key (dotted, e.g. "tui.reload" or `accounts."id"`) the value
// v, formatted with Format. The key's table is every segment but the
// last; a key in an array of tables needs SetAt.
func (d *Document) Set(key string, v any) error {
	segs, err := splitKey(key)
	if err != nil {
		return err
	}
	val, err := Format(v)
	if err != nil {
		return fmt.Errorf("tomledit: %s: %w", key, err)
	}
	s := d.scan()
	for _, h := range s.headers {
		if h.array && hasPrefix(segs, h.path) {
			return fmt.Errorf("tomledit: %s lies in the array of tables [[%s]]; use SetAt", key, formatKey(h.path))
		}
	}
	// 1. An active entry with this key: replace the value.
	for _, e := range s.entries {
		if samePath(s.fullKey(e), segs) {
			d.replaceValue(e, val)
			return nil
		}
	}
	table, name := segs[:len(segs)-1], segs[len(segs)-1:]
	h := -1
	for i, hd := range s.headers {
		if !hd.array && samePath(hd.path, table) {
			h = i
			break
		}
	}
	if h < 0 && len(table) > 0 {
		// 2. A commented-out header of the table: activate it.
		for _, ch := range s.cheaders {
			if !ch.array && samePath(ch.path, table) {
				d.lines[ch.line] = "[" + formatKey(table) + "]"
				s = d.scan()
				for i, hd := range s.headers {
					if hd.line == ch.line {
						h = i
					}
				}
				break
			}
		}
	}
	if h < 0 && len(table) > 0 {
		// 3. No table at all: create it after its closest parent, or at
		// the end.
		pos := d.tableInsertPos(s, table)
		d.insert(pos, "["+formatKey(table)+"]", formatKey(name)+" = "+val)
		return nil
	}
	return d.setInTable(s, h, name, val)
}

// setInTable sets the key name (relative to table h, -1 for the top
// level) to the formatted value val: an active entry is replaced, a
// commented-out one activated, otherwise the key is inserted after the
// last entry of the table.
func (d *Document) setInTable(s scan, h int, name []string, val string) error {
	for _, e := range s.entries {
		if e.table == h && samePath(e.key, name) {
			d.replaceValue(e, val)
			return nil
		}
	}
	for _, e := range s.commented {
		if e.table == h && samePath(e.key, name) {
			line := d.lines[e.start]
			rest := strings.TrimSpace(strings.TrimSpace(line)[1:])
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			col := strings.Index(line, rest)
			// Keep the key as spelled, replace the value.
			d.lines[e.start] = indent + line[col:e.valueCol] + val + line[e.valueEnd:]
			return nil
		}
	}
	entryLine := formatKey(name) + " = " + val
	if last := s.lastEntry(h); last >= 0 {
		d.insertLines(s.entries[last].end+1, entryLine)
		return nil
	}
	if h >= 0 {
		d.insertLines(s.headers[h].line+1, entryLine)
		return nil
	}
	// Top level without entries: before the first header, else at the end.
	if len(s.headers) > 0 {
		pos := s.headers[0].line
		d.insertLines(pos, entryLine, "")
		return nil
	}
	d.insertLines(len(d.lines), entryLine)
	return nil
}

// replaceValue puts val in place of the value of e, collapsing a value
// that spanned lines onto the first one; the text after the value (a
// trailing comment) is kept.
func (d *Document) replaceValue(e entry, val string) {
	line := d.lines[e.start][:e.valueCol] + val + d.lines[e.end][e.valueEnd:]
	d.lines = append(d.lines[:e.start], append([]string{line}, d.lines[e.end+1:]...)...)
	d.eol = true
}

// insertLines inserts lines before index pos.
func (d *Document) insertLines(pos int, lines ...string) {
	out := make([]string, 0, len(d.lines)+len(lines))
	out = append(out, d.lines[:pos]...)
	out = append(out, lines...)
	out = append(out, d.lines[pos:]...)
	d.lines = out
	d.eol = true
}

// insert places a block (a header with its entries) before index pos,
// separated from what is around it by one blank line.
func (d *Document) insert(pos int, lines ...string) {
	block := lines
	if pos > 0 && strings.TrimSpace(d.lines[pos-1]) != "" {
		block = append([]string{""}, block...)
	}
	if pos < len(d.lines) && strings.TrimSpace(d.lines[pos]) != "" {
		block = append(block, "")
	}
	d.insertLines(pos, block...)
}

// tableInsertPos is where a new table with path goes: after the last
// table of the same path (arrays), else after the region of the longest
// existing table whose path is a prefix, else at the end.
func (d *Document) tableInsertPos(s scan, path []string) int {
	last := -1
	for i, h := range s.headers {
		if samePath(h.path, path) {
			last = i
		}
	}
	if last < 0 {
		best := -1
		for i, h := range s.headers {
			if !h.array && len(h.path) < len(path) && hasPrefix(path, h.path) && (best < 0 || len(h.path) > len(s.headers[best].path)) {
				best = i
			}
		}
		last = best
	}
	if last < 0 {
		return len(d.lines)
	}
	// After the last entry of that region, so trailing blank lines and the
	// comments of the next table stay where they are.
	if e := s.lastEntry(last); e >= 0 {
		return s.entries[e].end + 1
	}
	return s.headers[last].line + 1
}

// Unset comments the entry of key out ("# key = value"), so the default
// applies again and the former value stays readable. A key without an
// active entry is an error.
func (d *Document) Unset(key string) error {
	segs, err := splitKey(key)
	if err != nil {
		return err
	}
	s := d.scan()
	for _, e := range s.entries {
		if samePath(s.fullKey(e), segs) {
			for i := e.start; i <= e.end; i++ {
				d.lines[i] = "# " + d.lines[i]
			}
			d.eol = true
			return nil
		}
	}
	return fmt.Errorf("tomledit: %s is not set", key)
}

// Count returns how many tables of the array path ("[[path]]") there are.
func (d *Document) Count(path string) int {
	segs, err := splitKey(path)
	if err != nil {
		return 0
	}
	return len(d.scan().arrayTables(segs))
}

// arrayTables returns the header indices of the array of tables path.
func (s scan) arrayTables(path []string) []int {
	var out []int
	for i, h := range s.headers {
		if h.array && samePath(h.path, path) {
			out = append(out, i)
		}
	}
	return out
}

// SetAt sets key (relative to the table) in the index-th table of the
// array path.
func (d *Document) SetAt(path string, index int, key string, v any) error {
	segs, err := splitKey(path)
	if err != nil {
		return err
	}
	name, err := splitKey(key)
	if err != nil {
		return err
	}
	val, err := Format(v)
	if err != nil {
		return fmt.Errorf("tomledit: %s: %w", key, err)
	}
	s := d.scan()
	tables := s.arrayTables(segs)
	if index < 0 || index >= len(tables) {
		return fmt.Errorf("tomledit: [[%s]] has %d tables, no index %d", path, len(tables), index)
	}
	return d.setInTable(s, tables[index], name, val)
}

// AppendTable adds a table "[[path]]" with the pairs after the last table
// of that array (or after the parent table, or at the end).
func (d *Document) AppendTable(path string, pairs []Pair) error {
	segs, err := splitKey(path)
	if err != nil {
		return err
	}
	lines := []string{"[[" + formatKey(segs) + "]]"}
	for _, p := range pairs {
		name, err := splitKey(p.Key)
		if err != nil {
			return err
		}
		val, err := Format(p.Value)
		if err != nil {
			return fmt.Errorf("tomledit: %s: %w", p.Key, err)
		}
		lines = append(lines, formatKey(name)+" = "+val)
	}
	s := d.scan()
	d.insert(d.tableInsertPos(s, segs), lines...)
	return nil
}

// RemoveTable deletes the index-th table of the array path with its
// entries and the blank lines up to the next header.
func (d *Document) RemoveTable(path string, index int) error {
	segs, err := splitKey(path)
	if err != nil {
		return err
	}
	s := d.scan()
	tables := s.arrayTables(segs)
	if index < 0 || index >= len(tables) {
		return fmt.Errorf("tomledit: [[%s]] has %d tables, no index %d", path, len(tables), index)
	}
	h := tables[index]
	start, end := s.headers[h].line, d.regionEnd(s, h)
	d.lines = append(d.lines[:start], d.lines[end:]...)
	d.eol = true
	return nil
}

// splitKey parses a dotted key argument.
func splitKey(key string) ([]string, error) {
	segs, rest, ok := parseKey(key)
	if !ok || strings.TrimSpace(rest) != "" || len(segs) == 0 {
		return nil, fmt.Errorf("tomledit: invalid key %q", key)
	}
	return segs, nil
}

// ---- values ----

// Format renders v as TOML: string (quoted), bool, int, int64, float64,
// time.Duration (its String, quoted), []string (an inline array) or Raw.
func Format(v any) (string, error) {
	switch x := v.(type) {
	case Raw:
		return string(x), nil
	case string:
		return quote(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		s := strconv.FormatFloat(x, 'f', -1, 64)
		if !strings.ContainsAny(s, ".e") {
			s += ".0"
		}
		return s, nil
	case time.Duration:
		return quote(x.String()), nil
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	return "", fmt.Errorf("cannot format a %T as TOML", v)
}

// quote writes s as a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f || !unicode.IsPrint(r) && r > 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ---- verification ----

// Diff decodes both texts and returns the dotted keys (bare segments where
// possible) whose values differ, were added or removed, sorted. An array
// of tables counts as one value under its path.
func Diff(a, b string) ([]string, error) {
	fa, err := flatten(a)
	if err != nil {
		return nil, err
	}
	fb, err := flatten(b)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for k, va := range fa {
		seen[k] = true
		if vb, ok := fb[k]; !ok || !reflect.DeepEqual(va, vb) {
			out = append(out, k)
		}
	}
	for k := range fb {
		if !seen[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// flatten decodes text and maps every leaf to its dotted key.
func flatten(text string) (map[string]any, error) {
	var m map[string]any
	if _, err := toml.Decode(text, &m); err != nil {
		return nil, fmt.Errorf("tomledit: %w", err)
	}
	out := map[string]any{}
	var walk func(prefix []string, v any)
	walk = func(prefix []string, v any) {
		if sub, ok := v.(map[string]any); ok {
			// An empty table is no value: filling it adds keys, not a
			// difference of its own.
			for k, x := range sub {
				walk(append(append([]string{}, prefix...), k), x)
			}
			return
		}
		out[formatKey(prefix)] = v
	}
	for k, v := range m {
		walk([]string{k}, v)
	}
	return out, nil
}

// ---- files ----

// Save writes text to path atomically (a temporary file in the same
// directory, then rename) and keeps the previous content, if there was
// one, as "<path>.bak". Parent directories are created.
func Save(path, text string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("tomledit: %w", err)
	}
	mode := os.FileMode(0o644)
	if old, err := os.ReadFile(path); err == nil {
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(path+".bak", old, mode); err != nil {
			return fmt.Errorf("tomledit: backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tomledit: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("tomledit: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("tomledit: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("tomledit: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("tomledit: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("tomledit: %w", err)
	}
	return nil
}

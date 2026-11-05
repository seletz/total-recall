// Package redact masks secrets in text and raw JSON lines before they are
// stored (see docs/explanation/design.md, section "Redaction"). Masked spans are replaced
// with «redacted:<kind>»; masking is not reversible.
//
// Detected:
//   - private key blocks (PEM/OpenSSH, also truncated ones),
//   - GitHub (ghp_/gho_/ghu_/ghs_/ghr_, github_pat_), AWS access key IDs,
//     Slack tokens and webhooks, OpenAI (sk-…), Anthropic (sk-ant-…),
//     Google API keys (AIza…), Stripe-style keys, JWTs, Bearer tokens,
//   - basic-auth passwords in URLs (scheme://user:pass@host; the user is kept),
//   - assignments to secret-looking keys (password, passwort, kennwort, pwd,
//     secret, token, api_key, access_key, private_key, auth_key, credential)
//     in key=value, key: value, key := value, key => value and JSON form
//     (JSON keys also: authorization),
//   - prose like "the password is X" / "das Passwort lautet X" when X
//     contains a digit or symbol,
//   - high-entropy values ≥ 32 characters assigned to any key; the value is
//     one bare token (no whitespace), never a whole sentence or command,
//   - in JSON: string values under secret-looking keys (also elements of an
//     array stored under one), bare high-entropy tokens as whole values or
//     array elements, and the value after a --password/--token/--api-key
//     flag in an argument array.
//
// Not masked on purpose: op:// references, $VAR/${VAR} and template
// references, git SHAs and other hex digests, UUIDs, paths, code
// expressions (calls, field access), type annotations, numbers assigned to
// keys such as max_tokens.
//
// Known limits: secrets without a recognizable format or key (e.g. a bare
// password on its own line, `mysql -pSecret`, `-p secret` flags) are not
// found; low-entropy values in generic assignments are kept; a secret-looking
// key with an innocent short value (password = pw) is masked. Regex-based
// redaction is best effort, not a guarantee.
package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
)

// Options configure a Redactor.
type Options struct {
	// ExtraPatterns are additional Go regexps; with a capture group only group 1 is masked.
	ExtraPatterns []string
	// Allow are Go regexps; a candidate secret matching any of them is kept.
	Allow []string
}

// Redactor masks secrets. It is safe for concurrent use.
type Redactor struct {
	rules []rule
	allow []*regexp.Regexp
}

// rule finds secrets. span returns the byte range to mask for one match
// (indices as returned by FindAllStringSubmatchIndex) or ok=false to skip.
// need lists literals of which at least one must occur in the input (in
// its lowercased form when fold is set), needRun demands a run of at least
// 32 token characters: cheap prechecks that spare the regexp on the bulk
// of ordinary text.
type rule struct {
	kind    string
	re      *regexp.Regexp
	span    func(s string, m []int) (start, end int, ok bool)
	need    []string
	fold    bool
	needRun bool
}

// applies reports whether the prechecks of ru hold for s; lower returns
// the lowercased s on demand.
func (ru rule) applies(s string, lower func() string) bool {
	if ru.needRun && !hasTokenRun(s, 32) {
		return false
	}
	if len(ru.need) == 0 {
		return true
	}
	h := s
	if ru.fold {
		h = lower()
	}
	for _, n := range ru.need {
		if strings.Contains(h, n) {
			return true
		}
	}
	return false
}

// hasTokenRun reports whether s contains n consecutive token characters
// ([A-Za-z0-9+/=_.-]), the shape of a value the high_entropy rule can mask.
func hasTokenRun(s string, n int) bool {
	run := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' || c == '_' || c == '.' || c == '-' {
			if run++; run >= n {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}

// Marker returns the replacement text for kind.
func Marker(kind string) string { return "«redacted:" + kind + "»" }

// group masks capture group g (0 = whole match) if it participated.
func group(g int) func(string, []int) (int, int, bool) {
	return func(_ string, m []int) (int, int, bool) {
		if m[2*g] < 0 {
			return 0, 0, false
		}
		return m[2*g], m[2*g+1], true
	}
}

// groupWithDigit masks group g only if it contains a digit.
func groupWithDigit(g int) func(string, []int) (int, int, bool) {
	return func(s string, m []int) (int, int, bool) {
		if m[2*g] < 0 || !strings.ContainsAny(s[m[2*g]:m[2*g+1]], "0123456789") {
			return 0, 0, false
		}
		return m[2*g], m[2*g+1], true
	}
}

var (
	reHex      = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	reCodeRef  = regexp.MustCompile(`^[A-Za-z_]\w*(\.[A-Za-z_]\w*)+$`)
	reDigits   = regexp.MustCompile(`^[0-9]+$`)
	reAllSame  = regexp.MustCompile(`^(?:\*+|x+|X+|\.+|-+|_+|#+)$`)
	notSecrets = map[string]bool{
		"string": true, "str": true, "bool": true, "boolean": true, "int": true, "number": true,
		"null": true, "nil": true, "none": true, "true": true, "false": true, "undefined": true,
		"optional": true, "any": true, "required": true, "redacted": true, "secret": true,
		"password": true, "token": true, "bytes": true, "text": true,
	}
	// key substrings that make a secret-looking key harmless (counts, references, metadata).
	keyDeny = []string{"tokens", "tokenizer", "tokentype", "count", "length", "max", "min", "file", "path",
		"name", "ref", "url", "uri", "expir", "ttl", "policy", "prompt", "hint", "field", "env",
		"required", "reset", "changed", "manager", "store", "rotation", "header", "label", "kind", "type"}
	keyAllow = []string{"password", "passwort", "passwd", "kennwort", "passphrase", "secret", "token",
		"apikey", "accesskey", "privatekey", "authkey", "credential", "authorization"}
	// identifierKey marks keys whose values are identifiers, signatures or
	// locations (tool_use_id, messageIds, signature, displayPath): a single
	// random-looking token there is not a secret, the whole-value
	// high_entropy rule skips it.
	identifierKey = []string{"signature", "path", "file", "name", "url", "uri", "hash", "digest"}
	// reToken matches a value that is one bare token, the only shape the
	// high_entropy rule masks as a whole (same character class as in Text).
	reToken = regexp.MustCompile(`^[A-Za-z0-9+/=_.-]{32,}$`)
	// reOptionName matches the name of a command-line option without its
	// dashes (api-key, token, passwd); anything else is not a flag.
	reOptionName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
)

// sensitiveKey reports whether a key name likely holds a secret.
func sensitiveKey(k string) bool {
	n := strings.ToLower(k)
	n = strings.NewReplacer("_", "", "-", "", ".", "").Replace(n)
	for _, d := range keyDeny {
		if strings.Contains(n, d) {
			return false
		}
	}
	if n == "pass" || n == "pwd" || n == "pw" || strings.HasSuffix(n, "pwd") {
		return true
	}
	for _, a := range keyAllow {
		if strings.Contains(n, a) {
			return true
		}
	}
	return false
}

// identifierValue reports whether key names an identifier-like field.
func identifierValue(key string) bool {
	n := strings.ToLower(key)
	if strings.HasSuffix(n, "id") || strings.HasSuffix(n, "ids") {
		return true
	}
	for _, d := range identifierKey {
		if strings.Contains(n, d) {
			return true
		}
	}
	return false
}

// plausibleSecret reports whether v, assigned to a secret-looking key, is a
// literal secret rather than a reference, type, placeholder or code.
func plausibleSecret(v string, quoted bool) bool {
	v = strings.TrimSpace(v)
	if len(v) < 3 {
		return false
	}
	for _, p := range []string{"«", "$", "{{", "<", "%", "op://", "{", "[", "/", "~/", "*", "="} {
		if strings.HasPrefix(v, p) {
			return false
		}
	}
	if notSecrets[strings.ToLower(v)] || reAllSame.MatchString(v) {
		return false
	}
	if quoted {
		return true
	}
	if strings.ContainsAny(v, "([") || reCodeRef.MatchString(v) || reDigits.MatchString(v) {
		return false
	}
	return true
}

// entropy returns the Shannon entropy in bits per character.
func entropy(s string) float64 {
	freq := map[rune]float64{}
	n := 0.0
	for _, r := range s {
		freq[r]++
		n++
	}
	h := 0.0
	for _, c := range freq {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}

// highEntropy reports whether v looks like a random secret: ≥ 32 chars,
// mixed case and digits, not hex/UUID, not a path, not a dunder identifier
// (mcp__server__tool), high Shannon entropy.
func highEntropy(v string) bool {
	if len(v) < 32 || strings.HasPrefix(v, "/") || strings.HasPrefix(v, "«") || strings.Contains(v, "__") {
		return false
	}
	if reHex.MatchString(strings.ReplaceAll(v, "-", "")) {
		return false
	}
	var up, lo, dig bool
	for _, r := range v {
		switch {
		case unicode.IsUpper(r):
			up = true
		case unicode.IsLower(r):
			lo = true
		case unicode.IsDigit(r):
			dig = true
		}
	}
	// Threshold scales with length: random alphanumerics of length n reach
	// roughly log2(min(n, 62)) bits; 75 % of that separates them from words,
	// identifiers and padded base64 data.
	return up && lo && dig && entropy(v) >= 0.75*math.Log2(math.Min(float64(len(v)), 62))
}

// assignment value: double-quoted, single-quoted or bare.
const assignValue = `(?:"([^"\n]*)"|'([^'\n]*)'|([^\s,;'"\)\]\}]+))`

func assignSpan(s string, m []int) (int, int, bool) {
	if !sensitiveKey(s[m[4]:m[5]]) {
		return 0, 0, false
	}
	for g, quoted := range map[int]bool{3: true, 4: true, 5: false} {
		if m[2*g] >= 0 {
			if !plausibleSecret(s[m[2*g]:m[2*g+1]], quoted) {
				return 0, 0, false
			}
			return m[2*g], m[2*g+1], true
		}
	}
	return 0, 0, false
}

func proseSpan(s string, m []int) (int, int, bool) {
	start, end := m[4], m[5]
	end = start + len(strings.TrimRight(s[start:end], ".,;:)"))
	v := s[start:end]
	if len(v) < 6 || strings.HasPrefix(v, "«") || strings.HasPrefix(v, "op://") || strings.HasPrefix(v, "$") {
		return 0, 0, false
	}
	if !strings.ContainsFunc(v, func(r rune) bool { return !unicode.IsLetter(r) }) {
		return 0, 0, false
	}
	return start, end, true
}

func entropySpan(s string, m []int) (int, int, bool) {
	v := s[m[6]:m[7]]
	if !highEntropy(v) {
		return 0, 0, false
	}
	return m[6], m[7], true
}

func builtinRules() []rule {
	mk := func(kind, re string, span func(string, []int) (int, int, bool), need ...string) rule {
		return rule{kind: kind, re: regexp.MustCompile(re), span: span, need: need}
	}
	fold := func(r rule) rule { r.fold = true; return r }
	secretWords := []string{"pass", "pwd", "pw", "kennwort", "secret", "token", "api", "access", "private", "auth", "credential"}
	proseWords := []string{"password", "passwort", "kennwort", "passphrase", "token", "secret", "pin"}
	entropy := mk("high_entropy", `(^|[^A-Za-z0-9_.-])([A-Za-z_][A-Za-z0-9_.-]*)["']?[ \t]*(?::=|=|:)[ \t]*["']?([A-Za-z0-9+/=_.-]{32,})`, entropySpan)
	entropy.needRun = true
	return []rule{
		mk("private_key", `-----BEGIN[A-Z0-9 ]*PRIVATE KEY[A-Z ]*-----(?s:.*?)(?:-----END[A-Z0-9 ]*PRIVATE KEY[A-Z ]*-----|\z)`, group(0), "PRIVATE KEY"),
		mk("github_token", `\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`, group(0), "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"),
		mk("aws_access_key", `\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`, group(0), "AKIA", "ASIA"),
		mk("slack_webhook", `https://hooks\.slack\.com/(?:services|workflows)/[A-Za-z0-9/_-]+`, group(0), "hooks.slack.com"),
		mk("slack_token", `\bxox[abposr]-[A-Za-z0-9-]{10,}`, group(0), "xox"),
		mk("anthropic_key", `\bsk-ant-[A-Za-z0-9_-]{20,}`, group(0), "sk-ant-"),
		mk("openai_key", `\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}`, groupWithDigit(0), "sk-"),
		mk("stripe_key", `\b[rs]k_(?:live|test)_[A-Za-z0-9]{16,}`, group(0), "k_live_", "k_test_"),
		mk("google_api_key", `\bAIza[0-9A-Za-z_-]{35}\b`, group(0), "AIza"),
		mk("jwt", `\beyJ[A-Za-z0-9_-]{5,}\.eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{10,}`, group(0), "eyJ"),
		mk("basic_auth", `\b[A-Za-z][A-Za-z0-9+.-]*://[^/\s:@]+:([^/\s@]+)@`, group(1), "://"),
		fold(mk("bearer", `(?i)\bbearer[ \t]+([A-Za-z0-9._~+/=-]{16,})`, groupWithDigit(1), "bearer")),
		fold(mk("secret", `(?i)(^|[^A-Za-z0-9_.-])([A-Za-z0-9_.-]*(?:pass(?:word|wort|wd|phrase)?|pwd|pw|kennwort|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|auth[_-]?key|credentials?)[A-Za-z0-9_.-]*)["']?[ \t]*(?::=|=>|=|:)[ \t]*`+assignValue, assignSpan, secretWords...)),
		fold(mk("secret", `(?i)\b(password|passwort|kennwort|passphrase|token|secret|pin)\s+(?:is|ist|lautet|was|war)\s*:?\s+["']?(\S+)`, proseSpan, proseWords...)),
		entropy,
	}
}

// New builds a Redactor with the built-in rules plus opts.
func New(opts Options) (*Redactor, error) {
	r := &Redactor{rules: builtinRules()}
	for _, p := range opts.ExtraPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("redact: extra pattern %q: %w", p, err)
		}
		g := 0
		if re.NumSubexp() > 0 {
			g = 1
		}
		r.rules = append(r.rules, rule{kind: "custom", re: re, span: group(g)})
	}
	for _, p := range opts.Allow {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("redact: allow pattern %q: %w", p, err)
		}
		r.allow = append(r.allow, re)
	}
	return r, nil
}

func (r *Redactor) allowed(secret string) bool {
	for _, a := range r.allow {
		if a.MatchString(secret) {
			return true
		}
	}
	return false
}

// Text masks secrets in s.
func (r *Redactor) Text(s string) string {
	lower, lowered := "", false
	lf := func() string {
		if !lowered {
			lower, lowered = strings.ToLower(s), true
		}
		return lower
	}
	for _, ru := range r.rules {
		if !ru.applies(s, lf) {
			continue
		}
		if t := r.applyRule(ru, s); t != s {
			s, lowered = t, false
		}
	}
	return s
}

func (r *Redactor) applyRule(ru rule, s string) string {
	ms := ru.re.FindAllStringSubmatchIndex(s, -1)
	if ms == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range ms {
		start, end, ok := ru.span(s, m)
		if !ok || start < last || start == end || r.allowed(s[start:end]) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(Marker(ru.kind))
		last = end
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// JSON masks secrets in one raw JSON document (e.g. a transcript line) and
// keeps it valid JSON. Only string literals are rewritten; everything else
// (key order, whitespace, numbers) is preserved byte for byte. A string value
// whose key looks secret is masked entirely, so is a value that is one bare
// high-entropy token; every other string (prose, commands, tool output) is
// masked with Text, token by token, and otherwise kept (#130). An array
// element has no key: it gets the same checks as a value under a harmless
// key, and the element before it stands in for the key when it is a flag
// such as --api-key or --token (#162). If line is not valid JSON, Text(line)
// is returned with an error.
func (r *Redactor) JSON(line string) (string, error) {
	if !json.Valid([]byte(line)) {
		return r.Text(line), fmt.Errorf("redact: invalid JSON")
	}
	var b strings.Builder
	last := 0
	lastKey := ""
	// frames mirrors the open containers; the top frame tells whether a
	// string is an object value or an array element and remembers the
	// previous element of the array.
	var frames []jsonFrame
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case '[':
			f := jsonFrame{array: true}
			if n := len(frames); n > 0 {
				// the array's own key, inherited through nested arrays
				f.key = lastKey
				if frames[n-1].array {
					f.key = frames[n-1].key
				}
			}
			frames = append(frames, f)
			continue
		case '{':
			frames = append(frames, jsonFrame{})
			continue
		case ']', '}':
			if len(frames) > 0 {
				frames = frames[:len(frames)-1]
			}
			continue
		case ',':
			if n := len(frames); n > 0 && frames[n-1].array {
				frames[n-1].prev, frames[n-1].cur = frames[n-1].cur, ""
			}
			continue
		case '"':
		default:
			continue
		}
		j := i + 1
		for ; j < len(line); j++ {
			if line[j] == '\\' {
				j++
			} else if line[j] == '"' {
				break
			}
		}
		lit := line[i : j+1]
		var str string
		if err := json.Unmarshal([]byte(lit), &str); err != nil {
			return r.Text(line), fmt.Errorf("redact: %w", err)
		}
		top := len(frames) - 1
		switch {
		case top >= 0 && !frames[top].array && isKey(line, j+1):
			lastKey = str
			i = j
			continue
		case top >= 0 && frames[top].array:
			frames[top].cur = str
			if red := r.jsonValue(frames[top].key, frames[top].prev, str); red != str {
				b.WriteString(line[last:i])
				b.WriteString(encodeString(red))
				last = j + 1
			}
		default:
			key := ""
			if top >= 0 {
				key = lastKey
			}
			if red := r.jsonValue(key, "", str); red != str {
				b.WriteString(line[last:i])
				b.WriteString(encodeString(red))
				last = j + 1
			}
		}
		i = j
	}
	if last == 0 {
		return line, nil
	}
	b.WriteString(line[last:])
	return b.String(), nil
}

// jsonFrame is one open JSON container while JSON scans a line. For an
// array, key is the object key the array (or its enclosing array) is stored
// under, cur the string element being read (empty if the current element is
// not a string) and prev the one before it.
type jsonFrame struct {
	array bool
	key   string
	prev  string
	cur   string
}

// jsonValue masks one string value stored under key: the object key of the
// value or of the array that holds it ("" at the top level). For an array
// element prev is the preceding element ("" if none or not a string); a flag
// like --token or --api-key there counts as a sensitive key too. Order as in
// Text: the specific rules first (sensitive key, known token formats,
// assignments inside the value), the generic high-entropy rule last and
// only for a value that is one bare token under a key that does not name
// identifiers.
func (r *Redactor) jsonValue(key, prev, v string) string {
	if r.allowed(v) {
		return r.Text(v)
	}
	if (sensitiveKey(key) || flagKey(prev)) && plausibleSecret(v, true) {
		return Marker("secret")
	}
	if t := r.Text(v); t != v {
		return t
	}
	if !identifierValue(key) && reToken.MatchString(v) && highEntropy(v) {
		return Marker("high_entropy")
	}
	return v
}

// flagKey reports whether an array element names a secret-taking option:
// a command-line flag (-passwd, --api-key, --token) whose name is a sensitive
// key. A bare word without the dash is not enough ("token" in a word list
// is prose, "--token" in an argument list takes a value), neither is a word
// that merely contains one (-- followed by a sentence fragment).
func flagKey(s string) bool {
	if len(s) > 64 || !strings.HasPrefix(s, "-") {
		return false
	}
	name := strings.TrimLeft(s, "-")
	return reOptionName.MatchString(name) && sensitiveKey(name)
}

// isKey reports whether the string literal ending before pos is an object key.
func isKey(s string, pos int) bool {
	for ; pos < len(s); pos++ {
		switch s[pos] {
		case ' ', '\t', '\n', '\r':
			continue
		case ':':
			return true
		default:
			return false
		}
	}
	return false
}

func encodeString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

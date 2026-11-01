// Package redact masks secrets in text and raw JSON lines before they are
// stored (see docs/design.md, section "Redaction"). Masked spans are replaced
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
//     in key=value, key: value, key := value, key => value and JSON form,
//   - prose like "the password is X" / "das Passwort lautet X" when X
//     contains a digit or symbol,
//   - high-entropy values ≥ 32 characters assigned to any key.
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
type rule struct {
	kind string
	re   *regexp.Regexp
	span func(s string, m []int) (start, end int, ok bool)
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
		"apikey", "accesskey", "privatekey", "authkey", "credential"}
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
// mixed case and digits, not hex/UUID, not a path, high Shannon entropy.
func highEntropy(v string) bool {
	if len(v) < 32 || strings.HasPrefix(v, "/") || strings.HasPrefix(v, "«") {
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
	mk := func(kind, re string, span func(string, []int) (int, int, bool)) rule {
		return rule{kind: kind, re: regexp.MustCompile(re), span: span}
	}
	return []rule{
		mk("private_key", `-----BEGIN[A-Z0-9 ]*PRIVATE KEY[A-Z ]*-----(?s:.*?)(?:-----END[A-Z0-9 ]*PRIVATE KEY[A-Z ]*-----|\z)`, group(0)),
		mk("github_token", `\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`, group(0)),
		mk("aws_access_key", `\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`, group(0)),
		mk("slack_webhook", `https://hooks\.slack\.com/(?:services|workflows)/[A-Za-z0-9/_-]+`, group(0)),
		mk("slack_token", `\bxox[abposr]-[A-Za-z0-9-]{10,}`, group(0)),
		mk("anthropic_key", `\bsk-ant-[A-Za-z0-9_-]{20,}`, group(0)),
		mk("openai_key", `\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}`, groupWithDigit(0)),
		mk("stripe_key", `\b[rs]k_(?:live|test)_[A-Za-z0-9]{16,}`, group(0)),
		mk("google_api_key", `\bAIza[0-9A-Za-z_-]{35}\b`, group(0)),
		mk("jwt", `\beyJ[A-Za-z0-9_-]{5,}\.eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{10,}`, group(0)),
		mk("basic_auth", `\b[A-Za-z][A-Za-z0-9+.-]*://[^/\s:@]+:([^/\s@]+)@`, group(1)),
		mk("bearer", `(?i)\bbearer[ \t]+([A-Za-z0-9._~+/=-]{16,})`, groupWithDigit(1)),
		mk("secret", `(?i)(^|[^A-Za-z0-9_.-])([A-Za-z0-9_.-]*(?:pass(?:word|wort|wd|phrase)?|pwd|pw|kennwort|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|auth[_-]?key|credentials?)[A-Za-z0-9_.-]*)["']?[ \t]*(?::=|=>|=|:)[ \t]*`+assignValue, assignSpan),
		mk("secret", `(?i)\b(password|passwort|kennwort|passphrase|token|secret|pin)\s+(?:is|ist|lautet|was|war)\s*:?\s+["']?(\S+)`, proseSpan),
		mk("high_entropy", `(^|[^A-Za-z0-9_.-])([A-Za-z_][A-Za-z0-9_.-]*)["']?[ \t]*(?::=|=|:)[ \t]*["']?([A-Za-z0-9+/=_.-]{32,})`, entropySpan),
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
	for _, ru := range r.rules {
		s = r.applyRule(ru, s)
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
// whose key looks secret is masked entirely, every other string is masked
// with Text. If line is not valid JSON, Text(line) is returned with an error.
func (r *Redactor) JSON(line string) (string, error) {
	if !json.Valid([]byte(line)) {
		return r.Text(line), fmt.Errorf("redact: invalid JSON")
	}
	var b strings.Builder
	last := 0
	lastKey := ""
	afterColon := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case ':':
			afterColon = true
			continue
		case ',', '[', '{', '}', ']':
			afterColon = false
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
		if isKey(line, j+1) {
			lastKey = str
		} else {
			key := ""
			if afterColon {
				key = lastKey
			}
			if red := r.jsonValue(key, str); red != str {
				b.WriteString(line[last:i])
				b.WriteString(encodeString(red))
				last = j + 1
			}
		}
		afterColon = false
		i = j
	}
	if last == 0 {
		return line, nil
	}
	b.WriteString(line[last:])
	return b.String(), nil
}

func (r *Redactor) jsonValue(key, v string) string {
	if key != "" && !r.allowed(v) {
		if sensitiveKey(key) && plausibleSecret(v, true) {
			return Marker("secret")
		}
		if highEntropy(v) {
			return Marker("high_entropy")
		}
	}
	return r.Text(v)
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

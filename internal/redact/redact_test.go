package redact

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode"
)

// Synthetic, non-functional secrets (built at runtime so scanners do not trip on the source).
var (
	ghp       = "ghp_" + strings.Repeat("aB3dE5", 6)
	ghPAT     = "github_pat_11ABCDEFG0" + strings.Repeat("xY7z", 15)
	awsKey    = "AKIA" + "IOSFODNN7EXAMPLE"
	awsSecret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	slackTok  = "xoxb-" + "123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx"
	slackHook = "https://hooks.slack.com/services/" + "T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
	openaiKey = "sk-proj-" + "Abc123Def456Ghi789Jkl012Mno345"
	openaiOld = "sk-" + "4bC9dE2fG7hJ1kL3mN5pQ8rS0tU6vW"
	antKey    = "sk-ant-api03-" + "Abc123Def456Ghi789Jkl012Mno345-_xyz"
	jwt       = "eyJhbGciOiJIUzI1NiJ9." + "eyJzdWIiOiIxMjM0NTY3ODkwIn0." + "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	privKey   = "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ\nAAAAAAAAAAEAAAAzAAAAC3NzaC1lZDI1NTE5\n-----END OPENSSH PRIVATE KEY-----"
	rsaKey    = "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----"
	entropic  = "Zm9vYmFyQmF6UXV4MTIzNDU2Nzg5MGFiY2RlZmdo"
)

func mustNew(t *testing.T, o Options) *Redactor {
	t.Helper()
	r, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestTextPositive(t *testing.T) {
	r := mustNew(t, Options{})
	tests := []struct {
		name   string
		in     string
		secret string // must not appear in output
		kind   string // expected marker kind
		keep   string // must still appear in output
	}{
		{"openssh private key", "key:\n" + privKey + "\ndone", "b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ", "private_key", "done"},
		{"rsa private key", rsaKey, "MIIEpAIBAAKCAQEA1234", "private_key", ""},
		{"truncated private key", "-----BEGIN EC PRIVATE KEY-----\nMHcCAQEEIabc", "MHcCAQEEIabc", "private_key", ""},
		{"github ghp", "use " + ghp + " please", ghp, "github_token", "please"},
		{"github ghs", "token ghs_" + strings.Repeat("Q1w2E3", 6), "ghs_" + strings.Repeat("Q1w2E3", 6), "github_token", ""},
		{"github pat", "GH=" + ghPAT, ghPAT, "github_token", ""},
		{"aws access key", "id " + awsKey + " ok", awsKey, "aws_access_key", "ok"},
		{"aws secret assignment", "aws_secret_access_key = " + awsSecret, awsSecret, "secret", "aws_secret_access_key"},
		{"slack token", "slack " + slackTok, slackTok, "slack_token", ""},
		{"slack webhook", "post to " + slackHook, "XXXXXXXXXXXXXXXXXXXXXXXX", "slack_webhook", "post to"},
		{"openai project key", "OPENAI " + openaiKey, openaiKey, "openai_key", ""},
		{"openai legacy key", "key " + openaiOld + ".", openaiOld, "openai_key", ""},
		{"anthropic key", "export X=" + antKey, antKey, "anthropic_key", "export X="},
		{"jwt", "Cookie: session=" + jwt, jwt, "jwt", "Cookie"},
		{"bearer header", "Authorization: Bearer abcDEF123456ghiJKL789", "abcDEF123456ghiJKL789", "bearer", "Authorization"},
		{"password key=value", "password=hunter2 next", "hunter2", "secret", "next"},
		{"password key: value", "password: s3cr3t!", "s3cr3t!", "secret", "password"},
		{"passwort german", "Passwort: Geheim123", "Geheim123", "secret", "Passwort"},
		{"db_password env", "DB_PASSWORD=\"correct horse battery\"", "correct horse battery", "secret", "DB_PASSWORD"},
		{"token yaml", "  token: abcdef123456", "abcdef123456", "secret", "token"},
		{"api_key colon-equals", "apiKey := \"q9w8e7r6t5\"", "q9w8e7r6t5", "secret", "apiKey"},
		{"client secret", "CLIENT_SECRET='Zz9_yy8-xx7'", "Zz9_yy8-xx7", "secret", ""},
		{"json in text", `{"user":"bob","password":"hunter2"}`, "hunter2", "secret", `"user":"bob"`},
		{"prose password is", "the password is Hunter2!", "Hunter2!", "secret", "the password is"},
		{"prose passwort lautet", "Das Passwort lautet geheim42", "geheim42", "secret", "Das Passwort lautet"},
		{"basic auth url", "git clone https://bob:pa55w0rd@example.com/r.git", "pa55w0rd", "basic_auth", "https://bob:"},
		{"basic auth postgres", "postgres://app:Xy9secret@db:5432/app", "Xy9secret", "basic_auth", "@db:5432/app"},
		{"high entropy assignment", "SOME_VALUE=" + entropic, entropic, "high_entropy", "SOME_VALUE="},
		{"google api key", "key AIza" + "SyA1234567890abcdefghijklmnopqrstuv", "AIzaSyA1234567890abcdefghijklmnopqrstuv", "google_api_key", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Text(tt.in)
			if strings.Contains(got, tt.secret) {
				t.Errorf("secret not masked:\n in: %q\nout: %q", tt.in, got)
			}
			if !strings.Contains(got, "«redacted:"+tt.kind+"»") {
				t.Errorf("missing marker %q in %q", tt.kind, got)
			}
			if tt.keep != "" && !strings.Contains(got, tt.keep) {
				t.Errorf("lost context %q in %q", tt.keep, got)
			}
			if again := r.Text(got); again != got {
				t.Errorf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestTextNegative(t *testing.T) {
	r := mustNew(t, Options{})
	tests := []struct{ name, in string }{
		{"git sha", "commit 3f9a1c0e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a3f fixed it"},
		{"short sha", "see 3f9a1c0"},
		{"sha assignment", "GIT_SHA=3f9a1c0e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a3f"},
		{"sha256 digest", "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
		{"digest assignment", "digest: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
		{"uuid", "session 7d1e4c20-0001-4000-8000-000000000001 started"},
		{"uuid assignment", "session_id=7d1e4c20-0001-4000-8000-000000000001"},
		{"op reference", "password: op://Private/db/password"},
		{"op reference eq", "TOKEN=op://vault/item/field"},
		{"file path", "/Users/bob/develop/total-recall/internal/redact/redact.go"},
		{"path assignment", "path: /Users/bob/Library/Application Support/total-recall/archive.db"},
		{"long word", "Donaudampfschifffahrtsgesellschaftskapitänsmütze"},
		{"long identifier assignment", "name: TestWriteBatchIdempotencyWithDuplicateEvents"},
		{"go type token", "type T struct { token string }"},
		{"ts type annotation", "password: string;"},
		{"env var ref", "password: ${DB_PASSWORD}"},
		{"env var ref plain", "TOKEN=$GITHUB_TOKEN"},
		{"function call", "token := getToken(ctx)"},
		{"field access", "password = cfg.Password"},
		{"max tokens", "max_tokens: 4096"},
		{"tokens json", `"input_tokens": 1234, "output_tokens": 99`},
		{"password file", "password_file: /run/secrets/db"},
		{"secret ref k8s", "secretName: my-app-tls"},
		{"prose no secret", "the token is expired, refresh the password before login"},
		{"redacted marker", "password: «redacted:secret»"},
		{"ssh git url", "git@github.com:seletz/total-recall.git"},
		{"https url no auth", "https://github.com/seletz/total-recall"},
		{"url with port", "http://localhost:8080/path"},
		{"email", "mail bob@example.com"},
		{"task id", "task-1234567890abcdefghijklmnop"},
		{"risk words", "risk-assessment-framework-document"},
		{"public key", "-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqh\n-----END PUBLIC KEY-----"},
		{"base64 image in code", "img := \"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ\""},
		{"placeholder", "password: <your-password>"},
		{"empty value", "password:"},
		{"no value prose", "Please enter your password."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Text(tt.in); got != tt.in {
				t.Errorf("unexpected change:\n in: %q\nout: %q", tt.in, got)
			}
		})
	}
}

func TestOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		in   string
		want string
	}{
		{"extra pattern whole match", Options{ExtraPatterns: []string{`ACME-[0-9]{6}`}}, "id ACME-123456 end", "id «redacted:custom» end"},
		{"extra pattern group", Options{ExtraPatterns: []string{`pin=([0-9]+)`}}, "pin=1234", "pin=«redacted:custom»"},
		{"allow keeps token", Options{Allow: []string{`^ghp_aB3dE5`}}, "x " + ghp, "x " + ghp},
		{"allow keeps password", Options{Allow: []string{`^hunter2$`}}, "password=hunter2", "password=hunter2"},
		{"allow does not affect others", Options{Allow: []string{`^nope$`}}, "password=hunter2", "password=«redacted:secret»"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := mustNew(t, tt.opts)
			if got := r.Text(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewInvalidPattern(t *testing.T) {
	if _, err := New(Options{ExtraPatterns: []string{"("}}); err == nil {
		t.Error("expected error for bad extra pattern")
	}
	if _, err := New(Options{Allow: []string{"["}}); err == nil {
		t.Error("expected error for bad allow pattern")
	}
}

func TestJSON(t *testing.T) {
	r := mustNew(t, Options{})
	tests := []struct {
		name    string
		in      string
		secrets []string
		keep    []string
	}{
		{"password value", `{"type":"user","password":"hunter2","n":1}`, []string{"hunter2"}, []string{`"type":"user"`, `"n":1`}},
		{"nested token key", `{"a":{"b":[{"api_key":"q9w8e7r6t5"}]}}`, []string{"q9w8e7r6t5"}, []string{`"api_key":`}},
		{"token in message text", `{"message":{"content":[{"type":"text","text":"use ` + ghp + ` now"}]}}`, []string{ghp}, []string{`"type":"text"`}},
		{"escaped json in string", `{"input":"{\"password\":\"hunter2\"}"}`, []string{"hunter2"}, []string{`\"password\"`}},
		{"private key with escaped newlines", `{"text":"` + strings.ReplaceAll(privKey, "\n", `\n`) + `"}`, []string{"b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ"}, nil},
		{"unicode preserved", `{"text":"Grüße «x» ä <b>"}`, nil, []string{`Grüße «x» ä <b>`}},
		{"untouched sha and uuid", `{"sha":"3f9a1c0e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a3f","uuid":"7d1e4c20-0001-4000-8000-000000000001"}`, nil, []string{`{"sha":"3f9a1c0e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a3f","uuid":"7d1e4c20-0001-4000-8000-000000000001"}`}},
		{"numeric token counts", `{"usage":{"input_tokens":12,"output_tokens":3}}`, nil, []string{`{"usage":{"input_tokens":12,"output_tokens":3}}`}},
		{"whitespace preserved", "{ \"a\" : \"b\" ,\n \"secret\" : \"Zz9_yy8-xx7\" }", []string{"Zz9_yy8-xx7"}, []string{"{ \"a\" : \"b\" ,\n \"secret\" : "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.JSON(tt.in)
			if err != nil {
				t.Fatalf("JSON: %v", err)
			}
			if !json.Valid([]byte(got)) {
				t.Fatalf("invalid JSON: %s", got)
			}
			for _, s := range tt.secrets {
				if strings.Contains(got, s) {
					t.Errorf("secret %q not masked: %s", s, got)
				}
			}
			for _, k := range tt.keep {
				if !strings.Contains(got, k) {
					t.Errorf("lost %q: %s", k, got)
				}
			}
			if len(tt.secrets) == 0 && got != tt.in {
				t.Errorf("unexpected change:\n in: %s\nout: %s", tt.in, got)
			}
		})
	}
}

func TestJSONInvalid(t *testing.T) {
	r := mustNew(t, Options{})
	got, err := r.JSON(`{"password":"hunter2"`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if strings.Contains(got, "hunter2") {
		t.Errorf("invalid JSON must still be text-redacted: %s", got)
	}
}

func TestHighEntropyRandom(t *testing.T) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	rng := rand.New(rand.NewPCG(1, 2))
	missed := 0
	for i := 0; i < 1000; i++ {
		b := make([]byte, 32+rng.IntN(33))
		for j := range b {
			b[j] = alphabet[rng.IntN(len(alphabet))]
		}
		v := string(b)
		if strings.ContainsAny(v, "0123456789") && strings.ContainsFunc(v, unicode.IsUpper) && strings.ContainsFunc(v, unicode.IsLower) && !highEntropy(v) {
			missed++
		}
	}
	if missed > 5 {
		t.Errorf("highEntropy missed %d/1000 random strings", missed)
	}
}

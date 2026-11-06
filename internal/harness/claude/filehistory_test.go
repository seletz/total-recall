package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/blobref"
)

// historyTree writes a synthetic transcript and backup files below a temp
// dir laid out like $CLAUDE_CONFIG_DIR and returns the transcript path and
// the file-history dir.
func historyTree(t *testing.T, lines string, files map[string][]byte) (string, string) {
	t.Helper()
	root := t.TempDir()
	hist := filepath.Join(root, "file-history")
	for name, data := range files {
		p := filepath.Join(hist, sid, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr := filepath.Join(root, "projects", "-w-demo", sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(tr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tr, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	return tr, hist
}

func snapshotLine(msg string, backups map[string]string) string {
	var b strings.Builder
	first := true
	for path, name := range backups {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(`"` + path + `":{"backupFileName":"` + name + `","version":1,"backupTime":"2026-10-01T10:00:00Z"}`)
	}
	return `{"type":"file-history-snapshot","messageId":"` + msg + `","snapshot":{"messageId":"` + msg + `","trackedFileBackups":{` + b.String() + `},"timestamp":"2026-10-01T10:00:00Z"},"isSnapshotUpdate":false}`
}

func deltaLine(name string) string {
	return `{"type":"file-history-delta","messageId":"m-3","snapshotMessageId":"m-1","trackingPath":"/w/demo/c.go","backup":{"backupFileName":"` + name + `","version":2,"backupTime":"2026-10-01T10:02:00Z","realParentDir":"/w/demo"},"timestamp":"2026-10-01T10:02:00Z","sessionId":"` + sid + `"}`
}

func TestFileHistory(t *testing.T) {
	text := []byte("package demo\n\nconst greeting = \"hallo\"\n")
	bin := []byte("\x89PNG\r\n\x1a\n\x00\x00synthetic")
	delta := []byte("package demo // v2\n")
	files := map[string][]byte{"aaaa000000000001@v1": text, "bbbb000000000002@v1": bin, "cccc000000000003@v2": delta}
	lines := snapshotLine("m-1", map[string]string{"/w/demo/a.go": "aaaa000000000001@v1"}) + "\n" +
		snapshotLine("m-2", map[string]string{"/w/demo/logo.png": "bbbb000000000002@v1"}) + "\n" +
		// m-1's file again, plus one that is gone: neither is read.
		snapshotLine("m-2b", map[string]string{"/w/demo/a.go": "aaaa000000000001@v1"}) + "\n" +
		snapshotLine("m-4", map[string]string{"/w/demo/gone.go": "dddd000000000004@v1"}) + "\n" +
		deltaLine("cccc000000000003@v2") + "\n"

	t.Run("off by default", func(t *testing.T) {
		tr, _ := historyTree(t, lines, files)
		recs, _ := parseFile(t, New(""), tr, 0)
		for _, r := range recs {
			if len(r.Meta[0].Attachments) != 0 || strings.Contains(r.Meta[0].Raw, "blob:sha256:") {
				t.Errorf("without a dir: %+v", r.Meta[0])
			}
		}
	})
	t.Run("archived", func(t *testing.T) {
		tr, hist := historyTree(t, lines, files)
		var logged []string
		a := New("", WithFileHistoryDir(hist), WithLogf(func(f string, args ...any) { logged = append(logged, f) }))
		recs, _ := parseFile(t, a, tr, 0)
		if len(recs) != 5 {
			t.Fatalf("records = %d", len(recs))
		}
		want := []struct {
			media string
			data  []byte
		}{{"text/plain", text}, {"application/octet-stream", bin}, {}, {}, {"text/plain", delta}}
		for i, w := range want {
			m := recs[i].Meta[0]
			if w.data == nil {
				if len(m.Attachments) != 0 || strings.Contains(m.Raw, "blob:sha256:") {
					t.Errorf("record %d: %+v", i, m)
				}
				continue
			}
			h := blobref.Hash(w.data)
			if len(m.Attachments) != 1 || m.Attachments[0].MediaType != w.media || m.Attachments[0].SHA256 != h || string(m.Attachments[0].Data) != string(w.data) {
				t.Errorf("record %d attachments = %+v", i, m.Attachments)
			}
			if !strings.Contains(m.Raw, `,"blob":"blob:sha256:`+h+`"}`) {
				t.Errorf("record %d raw = %s", i, m.Raw)
			}
		}
		// The ID does not depend on whether the copy is on disk.
		plain, _ := parseFile(t, New(""), tr, 0)
		for i := range recs {
			if recs[i].Meta[0].ID != plain[i].Meta[0].ID {
				t.Errorf("record %d: id %s with file history, %s without", i, recs[i].Meta[0].ID, plain[i].Meta[0].ID)
			}
		}
		st := a.Stats()
		if st.Backups != 3 || st.BackupBytes != int64(len(text)+len(bin)+len(delta)) || st.BackupsMissing != 1 || st.BackupsOversized != 0 {
			t.Errorf("stats = %+v", st)
		}
		if len(logged) != 1 || !strings.Contains(logged[0], "no longer on disk") {
			t.Errorf("log = %q", logged)
		}
	})
	t.Run("size cap", func(t *testing.T) {
		tr, hist := historyTree(t, lines, files)
		var logged []string
		a := New("", WithFileHistoryDir(hist), WithFileHistoryMaxBytes(int64(len(bin))), WithLogf(func(f string, args ...any) { logged = append(logged, f) }))
		recs, _ := parseFile(t, a, tr, 0)
		if len(recs[0].Meta[0].Attachments) != 0 || len(recs[1].Meta[0].Attachments) != 1 {
			t.Errorf("cap: %+v / %+v", recs[0].Meta[0].Attachments, recs[1].Meta[0].Attachments)
		}
		if st := a.Stats(); st.BackupsOversized != 1 {
			t.Errorf("stats = %+v", st)
		}
		if !strings.Contains(strings.Join(logged, "\n"), "over the cap") {
			t.Errorf("log = %q", logged)
		}
	})
	t.Run("names with path elements are not followed", func(t *testing.T) {
		tr, hist := historyTree(t, snapshotLine("m-9", map[string]string{"/etc/x": "../escape@v1"})+"\n", map[string][]byte{"../escape@v1": []byte("outside")})
		recs, _ := parseFile(t, New("", WithFileHistoryDir(hist)), tr, 0)
		if len(recs[0].Meta[0].Attachments) != 0 {
			t.Errorf("followed a path: %+v", recs[0].Meta[0])
		}
	})
}

func TestDefaultFileHistoryDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/c")
	if d, err := DefaultFileHistoryDir(); err != nil || d != "/c/file-history" {
		t.Errorf("DefaultFileHistoryDir = %q, %v", d, err)
	}
}

func TestIsText(t *testing.T) {
	for in, want := range map[string]bool{"plain\n": true, "Grüße": true, "": true, "a\x00b": false, "\xff\xfe": false} {
		if got := isText([]byte(in)); got != want {
			t.Errorf("isText(%q) = %v", in, got)
		}
	}
}

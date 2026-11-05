package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/seletz/total-recall/internal/harness"
)

// writeTree writes files (relative path -> content) below a new temp dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func drUserLine(uuid, version, content string) string {
	return fmt.Sprintf(`{"type":"user","uuid":%q,"timestamp":"2026-10-01T10:00:00Z","sessionId":"s","cwd":"/w","version":%q,"message":{"role":"user","content":%s}}`,
		uuid, version, content)
}

func drAssistantLine(uuid, version, content string) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-10-01T10:00:01Z","sessionId":"s","cwd":"/w","version":%q,"message":{"id":"m","model":"x","role":"assistant","content":%s}}`,
		uuid, version, content)
}

func TestCheckFormatClean(t *testing.T) {
	a := New("testdata/projects")
	r, err := a.CheckFormat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Clean() {
		t.Errorf("demo fixtures not clean: %+v", r)
	}
	if r.Harness != "claude" || r.Sources == 0 || r.Lines == 0 {
		t.Errorf("report = %+v", r)
	}
}

func TestCheckFormatFindsDrift(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"-w/s.jsonl": drUserLine("u1", "2.1.9", `"hi"`) + "\n" +
			drAssistantLine("u2", "2.1.10", `[{"type":"text","text":"a"},{"type":"hologram"},{"type":"thinking","thinking":"x"}]`) + "\n" +
			drUserLine("u3", "2.1.10", `[{"type":"tool_result","tool_use_id":"t","content":[{"type":"text","text":"ok"},{"type":"smell"}]},{"type":"image"}]`) + "\n" +
			`{"type":"brand-new","sessionId":"s"}` + "\n" +
			`{"type":"brand-new"}` + "\n" +
			`{"type":"attachment","sessionId":"s"}` + "\n" +
			`{"type":"user","uuid":"u4","sessionId":"s","message":{"role":"user","content":"no time, no cwd"}}` + "\n" +
			`{"type":"custom-title","customTitle":"t"}` + "\n" +
			"not json\n" +
			`{"type":"user","partial`, // trailing partial line: not counted
	})
	a := New(dir)
	r, err := a.CheckFormat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Clean() {
		t.Fatal("drift not detected")
	}
	if got := r.UnknownRecordTypes["brand-new"].Count; got != 2 {
		t.Errorf("unknown record type count = %d, want 2", got)
	}
	if len(r.UnknownRecordTypes) != 1 {
		t.Errorf("unknown record types = %v", r.UnknownRecordTypes)
	}
	if ex := r.UnknownRecordTypes["brand-new"].Example; ex == "" {
		t.Errorf("no example location")
	}
	wantBlocks := map[string]int64{"assistant/hologram": 1, "tool_result/smell": 1}
	gotBlocks := map[string]int64{}
	for k, v := range r.UnknownBlockTypes {
		gotBlocks[k] = v.Count
	}
	if !reflect.DeepEqual(gotBlocks, wantBlocks) {
		t.Errorf("unknown blocks = %v, want %v", gotBlocks, wantBlocks)
	}
	wantMissing := map[string]map[string]int64{
		"user":         {"timestamp": 1, "cwd": 1},
		"custom-title": {"sessionId": 1},
	}
	gotMissing := map[string]map[string]int64{}
	for typ, fields := range r.MissingFields {
		gotMissing[typ] = map[string]int64{}
		for f, v := range fields {
			gotMissing[typ][f] = v.Count
		}
	}
	if !reflect.DeepEqual(gotMissing, wantMissing) {
		t.Errorf("missing fields = %v, want %v", gotMissing, wantMissing)
	}
	if r.Malformed.Count != 1 {
		t.Errorf("malformed = %d, want 1", r.Malformed.Count)
	}
	if r.Lines != 9 {
		t.Errorf("lines = %d, want 9 (partial line excluded)", r.Lines)
	}
	wantVersions := []harness.VersionCount{{Version: "2.1.10", Count: 2}, {Version: "2.1.9", Count: 1}}
	if !reflect.DeepEqual(r.Versions, wantVersions) {
		t.Errorf("versions = %v, want %v", r.Versions, wantVersions)
	}
}

func TestCheckFormatHonoursSkipRecordTypes(t *testing.T) {
	dir := writeTree(t, map[string]string{"-w/s.jsonl": `{"type":"brand-new","sessionId":"s"}` + "\n"})
	r, err := New(dir, WithSkipRecordTypes("brand-new")).CheckFormat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Clean() {
		t.Errorf("configured skip type reported: %+v", r.UnknownRecordTypes)
	}
}

func TestCheckFormatCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New("testdata/projects").CheckFormat(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// TestKnownBlockTypesMatchParser keeps the exported lists and the parser in
// sync: handled block types produce events, ignored ones do not.
func TestKnownBlockTypesMatchParser(t *testing.T) {
	a := New("")
	block := func(typ string) string {
		switch typ {
		case "tool_use":
			return `{"type":"tool_use","id":"t","name":"Bash","input":{"command":"ls"}}`
		case "tool_result":
			return `{"type":"tool_result","tool_use_id":"t","content":"ok"}`
		}
		return fmt.Sprintf(`{"type":%q,"text":"x"}`, typ)
	}
	for _, tc := range []struct {
		record  string
		handled []string
		ignored []string
		line    func(string) string
	}{
		{"user", HandledUserBlockTypes(), IgnoredUserBlockTypes(), func(b string) string { return drUserLine("u", "1", "["+b+"]") }},
		{"assistant", HandledAssistantBlockTypes(), IgnoredAssistantBlockTypes(), func(b string) string { return drAssistantLine("u", "1", "["+b+"]") }},
	} {
		for _, typ := range tc.handled {
			if evs := parseLine(t, a, tc.line(block(typ))); len(evs) != 1 {
				t.Errorf("%s block %q is listed as handled but produced %d events", tc.record, typ, len(evs))
			}
		}
		for _, typ := range tc.ignored {
			if evs := parseLine(t, a, tc.line(block(typ))); len(evs) != 0 {
				t.Errorf("%s block %q is listed as ignored but produced %d events", tc.record, typ, len(evs))
			}
		}
	}
	for _, typ := range IngestedRecordTypes() {
		if !ingested[typ] {
			t.Errorf("ingested type %q not used by the parser", typ)
		}
	}
	for _, typ := range SkippedRecordTypes() {
		if ingested[typ] {
			t.Errorf("type %q is both ingested and skipped", typ)
		}
	}
}

// TestCheckFormatSessionOwner: Claude Code 2.1.317 writes session-owner
// records (account and organization, no timestamp) several times per
// session. They are known meta records and must not count as drift.
func TestCheckFormatSessionOwner(t *testing.T) {
	owner := `{"type":"session-owner","accountUuid":"acc00000-0000-4000-8000-000000000001","organizationUuid":"org00000-0000-4000-8000-000000000001","sessionId":"s"}`
	dir := writeTree(t, map[string]string{
		"-w/s.jsonl": owner + "\n" + drUserLine("u1", "2.1.317", `"hi"`) + "\n" + owner + "\n",
	})
	r, err := New(dir).CheckFormat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Clean() {
		t.Errorf("session-owner reported as drift: %+v", r)
	}
	if r.Lines != 3 {
		t.Errorf("lines = %d, want 3", r.Lines)
	}
}

// TestCheckFormatContinuedIn: Claude Code 2.1.318 writes a continued-in
// record (the session this one was continued in) once at the end of a
// transcript. It is a known meta record and must not count as drift.
func TestCheckFormatContinuedIn(t *testing.T) {
	cont := `{"type":"continued-in","timestamp":"2026-11-04T07:00:00.000Z","sessionId":"s","continuedInSessionId":"bbbbbbbb-0000-4000-8000-000000000002"}`
	dir := writeTree(t, map[string]string{
		"-w/s.jsonl": drUserLine("u1", "2.1.318", `"hi"`) + "\n" + cont + "\n",
	})
	r, err := New(dir).CheckFormat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Clean() {
		t.Errorf("continued-in reported as drift: %+v", r)
	}
	if r.Lines != 2 {
		t.Errorf("lines = %d, want 2", r.Lines)
	}
}

// TestCheckFormatHistorySuppression: Claude Code writes a
// history-suppression record (a cause and the account it was vetoed
// against, with a ts field instead of timestamp) in rare cases. It is a
// known meta record and must not count as drift.
func TestCheckFormatHistorySuppression(t *testing.T) {
	hs := `{"type":"history-suppression","sessionId":"s","cause":"chokepoint_veto","vetoedAgainstAccountUuid":"bbbbbbbb-0000-4000-8000-000000000002","ts":"2026-11-05T07:00:00.000Z"}`
	dir := writeTree(t, map[string]string{
		"-w/s.jsonl": drUserLine("u1", "2.1.318", `"hi"`) + "\n" + hs + "\n",
	})
	r, err := New(dir).CheckFormat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Clean() {
		t.Errorf("history-suppression reported as drift: %+v", r)
	}
	if r.Lines != 2 {
		t.Errorf("lines = %d, want 2", r.Lines)
	}
}

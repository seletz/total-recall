package blobref

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// apiRule accepts the Anthropic API source shape and a harness-specific
// {"base64": …, "type": "<media type>"} shape, like the Claude adapter.
func apiRule(f Fields) (dataKey, mediaType string, ok bool) {
	if f["type"] == "base64" && f["media_type"] != "" && f["data"] != "" {
		return "data", f["media_type"], true
	}
	if f["base64"] != "" && strings.Contains(f["type"], "/") {
		return "base64", f["type"], true
	}
	return "", "", false
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func hashOf(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestRef(t *testing.T) {
	h := hashOf([]byte("x"))
	r := Ref{SHA256: h}
	if got := r.String(); got != "blob:sha256:"+h {
		t.Errorf("String = %q", got)
	}
	tests := []struct {
		in string
		ok bool
	}{
		{"blob:sha256:" + h, true},
		{"blob:sha256:" + strings.ToUpper(h), false},
		{"blob:sha256:" + h[:63], false},
		{"blob:sha256:" + h + "0", false},
		{"blob:md5:" + h, false},
		{h, false},
		{"", false},
	}
	for _, tt := range tests {
		got, ok := Parse(tt.in)
		if ok != tt.ok || (ok && got.SHA256 != h) {
			t.Errorf("Parse(%q) = %+v, %v; want ok %v", tt.in, got, ok, tt.ok)
		}
	}
}

func TestExtractAndRestore(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfake image bytes")
	jpg := []byte("\xff\xd8\xff fake jpeg")
	line := `{"type":"user","message":{"content":[{"type":"text","text":"see \"this\": {\"data\":\"not an object\"}"},` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + b64(png) + `"}},` +
		`{"type":"tool_result","content":[{"type":"text","text":"ok"},{"type":"image","source":{"media_type":"image/jpeg","data":"` + b64(jpg) + `","type":"base64"}}]}]},` +
		`"toolUseResult":{"type":"image","file":{"base64":"` + b64(jpg) + `","type":"image/jpeg","originalSize":123}},` +
		`"other":{"type":"base64","media_type":"image/png","data":""},"nested":[[{"data":"x","type":"text","media_type":"text/plain"}]]}`
	out, atts, err := Extract([]byte(line), apiRule)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 3 {
		t.Fatalf("attachments = %d, want 3: %+v", len(atts), atts)
	}
	want := []Attachment{
		{Path: "message.content[1].source", MediaType: "image/png", SHA256: hashOf(png), Data: png},
		{Path: "message.content[2].content[1].source", MediaType: "image/jpeg", SHA256: hashOf(jpg), Data: jpg},
		{Path: "toolUseResult.file", MediaType: "image/jpeg", SHA256: hashOf(jpg), Data: jpg},
	}
	for i, a := range atts {
		if a.Path != want[i].Path || a.MediaType != want[i].MediaType || a.SHA256 != want[i].SHA256 || !bytes.Equal(a.Data, want[i].Data) {
			t.Errorf("attachment %d = %+v, want %+v", i, a, want[i])
		}
	}
	if !json.Valid(out) {
		t.Fatalf("rewritten line is not JSON: %s", out)
	}
	if bytes.Contains(out, []byte(b64(png))) || bytes.Contains(out, []byte(b64(jpg))) {
		t.Errorf("base64 data still in the line: %s", out)
	}
	if n := bytes.Count(out, []byte("blob:sha256:")); n != 3 {
		t.Errorf("references = %d, want 3: %s", n, out)
	}
	// Everything else is byte for byte as it was: the text block with its
	// decoy, the empty data, the nested text source, the key order.
	for _, keep := range []string{`"text":"see \"this\": {\"data\":\"not an object\"}"`, `"data":""`, `{"data":"x","type":"text","media_type":"text/plain"}`,
		`{"media_type":"image/jpeg","data":"blob:sha256:` + hashOf(jpg) + `","type":"base64"}`, `"originalSize":123`} {
		if !bytes.Contains(out, []byte(keep)) {
			t.Errorf("rewritten line lost %s: %s", keep, out)
		}
	}
	// Extract is idempotent on its own output.
	again, atts2, err := Extract(out, apiRule)
	if err != nil || len(atts2) != 0 || !bytes.Equal(again, out) {
		t.Errorf("Extract on rewritten line: %d attachments, %v, changed %v", len(atts2), err, !bytes.Equal(again, out))
	}
	// Restore gives the original line back, byte for byte.
	blobs := map[string][]byte{hashOf(png): png, hashOf(jpg): jpg}
	lookups := 0
	restored, err := Restore(out, func(sha string) ([]byte, error) {
		lookups++
		b, ok := blobs[sha]
		if !ok {
			return nil, fmt.Errorf("no blob %s", sha)
		}
		return b, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != line {
		t.Errorf("Restore:\n got %s\nwant %s", restored, line)
	}
	if lookups != 3 {
		t.Errorf("lookups = %d, want 3", lookups)
	}
}

func TestExtractLeavesLinesAlone(t *testing.T) {
	tests := []struct {
		name, line string
	}{
		{"no base64 word", `{"type":"user","message":{"content":"hello"}}`},
		{"base64 in prose", `{"type":"user","message":{"content":"decode the base64 string please"}}`},
		{"invalid base64 data", `{"source":{"type":"base64","media_type":"image/png","data":"not base64!!"}}`},
		{"rule does not match", `{"type":"base64","data":"` + b64([]byte("abc")) + `"}`},
		{"not an object", `[1, 2, "base64"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, atts, err := Extract([]byte(tt.line), apiRule)
			if err != nil || len(atts) != 0 || string(out) != tt.line {
				t.Errorf("Extract = %s, %d attachments, %v", out, len(atts), err)
			}
		})
	}
	if _, _, err := Extract([]byte(`{"type":"base64","data":"`+b64([]byte("abc"))+`","media_type":"x/y"`), apiRule); err == nil {
		t.Error("truncated JSON: no error")
	}
}

func TestRestoreUnknownBlob(t *testing.T) {
	h := hashOf([]byte("gone"))
	line := []byte(`{"source":{"type":"base64","media_type":"image/png","data":"blob:sha256:` + h + `"}}`)
	_, err := Restore(line, func(string) ([]byte, error) { return nil, fmt.Errorf("missing") })
	if err == nil || !strings.Contains(err.Error(), h) {
		t.Errorf("Restore with a missing blob: %v", err)
	}
	// A line without references is returned as it is, without lookups.
	plain := []byte(`{"a":"b"}`)
	out, err := Restore(plain, func(string) ([]byte, error) { t.Fatal("lookup called"); return nil, nil })
	if err != nil || !bytes.Equal(out, plain) {
		t.Errorf("Restore(plain) = %s, %v", out, err)
	}
}

func TestExtractWhitespaceAndEscapes(t *testing.T) {
	png := []byte{1, 2, 3, 250, 251}
	line := "{ \"message\" : { \"content\" : [ { \"type\" : \"image\" , \"source\" : { \"type\" : \"base64\" ,\n \"media_type\" : \"image\\/png\" , \"data\" : \"" + b64(png) + "\" } } ] } }"
	out, atts, err := Extract([]byte(line), apiRule)
	if err != nil || len(atts) != 1 || atts[0].MediaType != "image/png" || atts[0].Path != "message.content[0].source" {
		t.Fatalf("Extract = %+v, %v", atts, err)
	}
	if !strings.Contains(string(out), "\"data\" : \"blob:sha256:"+hashOf(png)+"\" }") {
		t.Errorf("rewritten: %s", out)
	}
	restored, err := Restore(out, func(string) ([]byte, error) { return png, nil })
	if err != nil || string(restored) != line {
		t.Errorf("Restore = %s, %v", restored, err)
	}
}

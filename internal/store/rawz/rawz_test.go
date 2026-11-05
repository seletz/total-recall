package rawz

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/dict"
)

var sample = `{"type":"user","uuid":"u-1","parentUuid":"p-1","timestamp":"2026-10-01T10:00:00.000Z","origin":{"kind":"human"},"message":{"content":"hello world, this is a message of the usual length for a transcript line"}}`

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"line", sample},
		{"unicode", `{"message":{"content":"Grüße «redacted:secret» 日本語  "}}`},
		{"large", `{"message":{"content":"` + strings.Repeat("abcdefghij", 20000) + `"}}`},
		{"beyond the window", `{"message":{"content":"` + varied(3<<20) + `"}}`},
		{"not json", "plain text\nwith newline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Encode(tt.in)
			if (tt.in == "") != (b == nil) {
				t.Fatalf("Encode(%q) = %d bytes, nil only for empty", tt.in, len(b))
			}
			got, err := Decode(b)
			if err != nil || got != tt.in {
				t.Fatalf("Decode = %q, %v", got, err)
			}
			if tt.in == "" {
				return
			}
			h, ok := Inspect(b)
			if !ok || h.Size != uint64(len(tt.in)) || h.DictID != DictIDV2 {
				t.Errorf("Inspect = %+v, %v; want size %d dict %#x", h, ok, len(tt.in), DictIDV2)
			}
			if !IsFrame(b) {
				t.Error("IsFrame false")
			}
		})
	}
}

func TestDictionaryHelps(t *testing.T) {
	plain, err := New()
	if err != nil {
		t.Fatal(err)
	}
	with, without := len(Encode(sample)), len(plain.Encode(sample))
	if with >= without {
		t.Errorf("with dictionary %d bytes, without %d: the dictionary does not help on a typical line", with, without)
	}
	t.Logf("line %d bytes: %d with dictionary, %d without", len(sample), with, without)
}

// TestOlderDictionaryStillDecodes: a codec with a newer dictionary appended
// decodes frames written by the older one and frames without dictionary.
func TestOlderDictionaryStillDecodes(t *testing.T) {
	var samples [][]byte
	for i := 0; i < 64; i++ {
		samples = append(samples, []byte(strings.Replace(sample, "u-1", "u-"+strings.Repeat("x", i), 1)))
	}
	newer, err := dict.BuildZstdDict(samples, dict.Options{MaxDictSize: 8 << 10, HashBytes: 6, ZstdDictID: DictIDV2 + 1})
	if err != nil {
		t.Fatal(err)
	}
	old, _ := New(Dicts...)
	plain, _ := New()
	next, err := New(append(append([][]byte{}, Dicts...), newer)...)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"old dictionary": old.Encode(sample), "no dictionary": plain.Encode(sample), "new dictionary": next.Encode(sample)} {
		got, err := next.Decode(b)
		if err != nil || got != sample {
			t.Errorf("%s: Decode = %q, %v", name, got, err)
		}
	}
	if h, _ := Inspect(next.Encode(sample)); h.DictID != DictIDV2+1 {
		t.Errorf("new codec writes dict %#x, want %#x", h.DictID, DictIDV2+1)
	}
	// The old codec does not know the new dictionary: a clear error, no garbage.
	if _, err := old.Decode(next.Encode(sample)); err == nil {
		t.Error("old codec decoded a frame of an unknown dictionary")
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	for _, b := range [][]byte{[]byte("{}"), []byte("not a frame"), {0x28, 0xb5, 0x2f, 0xfd, 0, 0, 0}} {
		_, err := Decode(b)
		if err == nil {
			t.Errorf("Decode(%q) succeeded", b)
		}
		if !IsFrame(b) && !errors.Is(err, ErrNotZstd) {
			t.Errorf("Decode(%q) = %v, want ErrNotZstd", b, err)
		}
	}
}

// varied returns n bytes that do not repeat within the encoder's window,
// the shape of a large Write tool call with real content.
func varied(n int) string {
	var b strings.Builder
	b.Grow(n + 32)
	for i := 0; b.Len() < n; i++ {
		b.WriteString(strconv.Itoa(i*7919) + " line of a generated file with number " + strconv.Itoa(i) + "\\n")
	}
	return b.String()[:n]
}

// TestFirstDictionaryStillDecodes: rows written with dictionary v1 (the
// extracts of schema v5) decode with the default codec, which encodes
// with v2.
func TestFirstDictionaryStillDecodes(t *testing.T) {
	v1, err := New(Dicts[0])
	if err != nil {
		t.Fatal(err)
	}
	b := v1.Encode(sample)
	if h, _ := Inspect(b); h.DictID != DictIDV1 {
		t.Fatalf("v1 codec writes dict %#x", h.DictID)
	}
	got, err := Decode(b)
	if err != nil || got != sample {
		t.Errorf("Decode(v1 frame) = %q, %v", got, err)
	}
	if h, _ := Inspect(Encode(sample)); h.DictID != DictIDV2 {
		t.Errorf("default codec writes dict %#x, want v2", h.DictID)
	}
}

func TestEmbeddedDictionary(t *testing.T) {
	if len(Dicts) == 0 || len(Dicts[len(Dicts)-1]) < 1024 {
		t.Fatal("no embedded dictionary")
	}
	if _, err := Default(); err != nil {
		t.Fatal(err)
	}
}

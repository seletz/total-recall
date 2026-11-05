// Package blobref moves attachment data (images, documents delivered as
// base64 in a transcript line) out of the line and back in. It is
// harness-neutral: a Rule supplied by the adapter decides which JSON
// objects carry data; the package knows the reference that replaces the
// data in the stored line ("blob:sha256:<hex>", see Ref), finds and decodes
// the data without re-serialising the line (every other byte of the line
// stays as it was), and restores a line from its blobs (Restore). The
// store keeps the blobs by their SHA-256 (tables blobs and event_blobs,
// schema v6), so a line with references is small and reconstructable.
package blobref

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// Prefix starts every reference string.
const Prefix = "blob:sha256:"

// Ref is a reference to a stored blob, written in place of the base64 data
// of an attachment: the string "blob:sha256:<64 hex digits>". The field
// that held the base64 text holds the reference; Restore puts the base64
// of the blob back.
type Ref struct {
	// SHA256 is the lower-case hex digest of the attachment's bytes.
	SHA256 string
}

// String returns the reference as it is stored.
func (r Ref) String() string { return Prefix + r.SHA256 }

// Parse recognises a reference string. ok is false for anything else.
func Parse(s string) (Ref, bool) {
	if len(s) != len(Prefix)+sha256.Size*2 || s[:len(Prefix)] != Prefix {
		return Ref{}, false
	}
	h := s[len(Prefix):]
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return Ref{}, false
		}
	}
	return Ref{SHA256: h}, true
}

// Hash returns the hex SHA-256 of data, the key of a blob.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// Fields are the string-valued direct members of one JSON object, decoded.
type Fields map[string]string

// Rule decides whether an object carries attachment data. It sees the
// object's string fields and returns the key of the field that holds the
// base64 data and the media type of the attachment. A rule should accept
// only objects whose shape it knows (the adapter knows its format).
type Rule func(f Fields) (dataKey, mediaType string, ok bool)

// Attachment is one piece of data Extract took out of a line.
type Attachment struct {
	// Path is the JSON path of the object that held the data, with array
	// indexes, e.g. "message.content[1].source"; the adapter maps it to
	// its events.
	Path      string
	MediaType string
	// SHA256 is Hash(Data).
	SHA256 string
	// Data is the decoded attachment as it was delivered.
	Data []byte
}

// Marker is the substring every line with attachment data of a known
// shape contains ("base64": the API source type or the data field name).
// Callers skip Extract for lines without it.
const Marker = "base64"

// Extract finds the objects of raw that rule accepts, decodes their base64
// data and returns the line with every data string replaced by its Ref,
// plus the attachments in document order. The rest of the line is copied
// byte for byte (keys, order, whitespace, escapes). A data string that is
// not base64 stays. A line that is not valid JSON is an error, raw comes
// back unchanged.
func Extract(raw []byte, rule Rule) ([]byte, []Attachment, error) {
	var atts []Attachment
	var repl []replacement
	err := walk(raw, func(f *frame, obj Fields, spans map[string]span) {
		key, media, ok := rule(obj)
		if !ok {
			return
		}
		sp, ok := spans[key]
		if !ok {
			return
		}
		data, ok := decodeBase64(obj[key])
		if !ok {
			return
		}
		h := Hash(data)
		atts = append(atts, Attachment{Path: f.path, MediaType: media, SHA256: h, Data: data})
		repl = append(repl, replacement{span: sp, text: strconv.Quote(Ref{SHA256: h}.String())})
	}, nil)
	if err != nil {
		return raw, nil, err
	}
	if len(repl) == 0 {
		return raw, nil, nil
	}
	return splice(raw, repl), atts, nil
}

// Restore replaces every reference string in raw by the base64 of the
// blob lookup returns for its hash and gives the line back as it was
// before Extract. A line without references is returned as it is, without
// a lookup. An error of lookup is returned with the hash.
func Restore(raw []byte, lookup func(sha256 string) ([]byte, error)) ([]byte, error) {
	if !bytes.Contains(raw, []byte(Prefix)) {
		return raw, nil
	}
	var repl []replacement
	var lerr error
	err := walk(raw, nil, func(value string, sp span) {
		if lerr != nil {
			return
		}
		ref, ok := Parse(value)
		if !ok {
			return
		}
		data, err := lookup(ref.SHA256)
		if err != nil {
			lerr = fmt.Errorf("blob %s: %w", ref.SHA256, err)
			return
		}
		repl = append(repl, replacement{span: sp, text: `"` + base64.StdEncoding.EncodeToString(data) + `"`})
	})
	if err != nil {
		return raw, err
	}
	if lerr != nil {
		return raw, lerr
	}
	return splice(raw, repl), nil
}

// decodeBase64 decodes standard base64 with or without padding.
func decodeBase64(s string) ([]byte, bool) {
	if s == "" {
		return nil, false
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, true
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, true
	}
	return nil, false
}

// span is a byte range of raw, start inclusive, end exclusive; for a
// string value it covers the quotes.
type span struct{ start, end int }

type replacement struct {
	span
	text string
}

// splice applies the replacements, which must not overlap.
func splice(raw []byte, repl []replacement) []byte {
	sort.Slice(repl, func(i, j int) bool { return repl[i].start < repl[j].start })
	var out bytes.Buffer
	out.Grow(len(raw))
	pos := 0
	for _, r := range repl {
		out.Write(raw[pos:r.start])
		out.WriteString(r.text)
		pos = r.end
	}
	out.Write(raw[pos:])
	return out.Bytes()
}

// frame is one open container of the walk.
type frame struct {
	object    bool
	expectKey bool   // object: the next string token is a key
	key       string // object: the key of the value being read
	index     int    // array: the index of the next element
	path      string // the container's own path
	fields    Fields
	spans     map[string]span // object: spans of its string values, by key
}

// childPath is the path of the value the frame is about to read.
func (f *frame) childPath() string {
	if f == nil {
		return ""
	}
	if f.object {
		if f.path == "" {
			return f.key
		}
		return f.path + "." + f.key
	}
	return f.path + "[" + strconv.Itoa(f.index) + "]"
}

// advance notes that the frame has read one value.
func (f *frame) advance() {
	if f == nil {
		return
	}
	if f.object {
		f.expectKey = true
	} else {
		f.index++
	}
}

// walk tokenises raw and calls onObject when an object closes (with its
// string fields and their spans) and onString for every string value
// (never for keys). Either callback may be nil.
func walk(raw []byte, onObject func(f *frame, obj Fields, spans map[string]span), onString func(value string, sp span)) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var stack []*frame
	top := func() *frame {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}
	for {
		before := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			if len(stack) > 0 {
				return fmt.Errorf("blobref: unexpected end of JSON")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("blobref: %w", err)
		}
		after := dec.InputOffset()
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				parent := top()
				f := &frame{object: v == '{', expectKey: v == '{', path: parent.childPath()}
				if f.object {
					f.fields, f.spans = Fields{}, map[string]span{}
				}
				parent.advance()
				stack = append(stack, f)
			case '}', ']':
				f := top()
				stack = stack[:len(stack)-1]
				if f.object && onObject != nil {
					onObject(f, f.fields, f.spans)
				}
			}
		case string:
			f := top()
			if f != nil && f.object && f.expectKey {
				f.key, f.expectKey = v, false
				continue
			}
			sp := span{start: stringStart(raw, int(before)), end: int(after)}
			if f != nil && f.object {
				f.fields[f.key] = v
				f.spans[f.key] = sp
			}
			if onString != nil {
				onString(v, sp)
			}
			f.advance()
		default:
			top().advance()
		}
	}
}

// stringStart finds the opening quote of the string token that ends at
// the decoder's position: from the end of the previous token only
// whitespace, ',' and ':' can precede it.
func stringStart(raw []byte, from int) int {
	for i := from; i < len(raw); i++ {
		switch raw[i] {
		case ' ', '\t', '\r', '\n', ',', ':':
			continue
		}
		return i
	}
	return from
}

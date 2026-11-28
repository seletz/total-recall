package cli

import (
	"strings"
	"testing"
)

// kindHits runs a search --json over the demo archive and returns the
// hits per type; --limit 0 keeps all of them.
func kindHits(t *testing.T, e *testEnv, base []string, args ...string) map[string][]typedHit {
	t.Helper()
	out := map[string][]typedHit{}
	for _, h := range decode[[]typedHit](t, e.json(t, base, append([]string{"search", "--limit", "0"}, args...)...)) {
		out[h.s("type")] = append(out[h.s("type")], h)
	}
	return out
}

// TestSearchKindsAddUp: against the real --json output of the demo
// archive, the events of a word are the sum of the kinds: input, answer,
// tool_call and tool_result, plus the messages of peers and automation
// (which no kind type takes). Image and document add up to the events
// with an attachment, and every group of a multi-type search has as many
// hits as the same type searched alone.
func TestSearchKindsAddUp(t *testing.T) {
	t.Parallel()
	e, base := attachEnv(t)
	seen := map[string]int{}
	for _, word := range []string{"release", "test", "goreleaser", "cask", "route", "the"} {
		all := kindHits(t, e, base, word)["event"]
		if len(all) == 0 {
			t.Fatalf("%q: no event hits", word)
		}
		parts := kindHits(t, e, base, word, "--type", "input,answer,tool_call,tool_result")
		sum := 0
		for _, ty := range []string{"input", "answer", "tool_call", "tool_result"} {
			sum += len(parts[ty])
			seen[ty] += len(parts[ty])
		}
		for _, actor := range []string{"peer", "automation"} {
			sum += len(kindHits(t, e, base, word, "--actor", actor)["event"])
		}
		if sum != len(all) {
			t.Errorf("%q: kinds + peer + automation = %d, event = %d", word, sum, len(all))
		}
		// The same hits, one by one: every id of a kind is an event with
		// that actor and kind.
		ids := map[string]bool{}
		for _, h := range all {
			ids[h.s("event_id")] = true
		}
		for ty, hs := range parts {
			for _, h := range hs {
				if !ids[h.s("event_id")] {
					t.Errorf("%q: %s hit %s is no event hit", word, ty, h.s("event_id"))
				}
				switch ty {
				case "input":
					if h.s("actor") != "human" || h.s("kind") != "message" {
						t.Errorf("input hit %+v", h)
					}
				case "answer":
					if h.s("actor") != "agent" || h.s("kind") != "message" {
						t.Errorf("answer hit %+v", h)
					}
				case "tool_call":
					if h.s("kind") != "tool_use" {
						t.Errorf("tool_call hit %+v", h)
					}
				case "tool_result":
					if h.s("kind") != "tool_result" {
						t.Errorf("tool_result hit %+v", h)
					}
				}
			}
		}
	}
	for _, ty := range []string{"input", "answer", "tool_call", "tool_result"} {
		if seen[ty] == 0 {
			t.Errorf("no %s hit for any word: the sum proves nothing", ty)
		}
	}
	// The word-less listing: the image and the document of the demo
	// archive, and together the events of --has-attachment.
	kinds := kindHits(t, e, base, "--type", "image,document")
	any := kindHits(t, e, base, "--has-attachment")["event"]
	img := kindHits(t, e, base, "--has-attachment=image")["event"]
	doc := kindHits(t, e, base, "--has-attachment=document")["event"]
	if len(kinds["image"]) != len(img) || len(kinds["document"]) != len(doc) || len(img) == 0 || len(doc) == 0 {
		t.Errorf("image %d (%d), document %d (%d)", len(kinds["image"]), len(img), len(kinds["document"]), len(doc))
	}
	if len(any) < len(img) || len(any) < len(doc) || len(any) > len(img)+len(doc) {
		t.Errorf("attachments %d, images %d, documents %d", len(any), len(img), len(doc))
	}
}

// TestSearchKindsUnchanged: a call without --type is the call with
// --type event, byte for byte.
func TestSearchKindsUnchanged(t *testing.T) {
	t.Parallel()
	e, base := attachEnv(t)
	for _, args := range [][]string{{"release"}, {"release", "--actor", "human"}, {"--has-attachment"}, {"cask", "--limit", "3"}} {
		plain := e.json(t, base, append([]string{"search"}, args...)...)
		typed := e.json(t, base, append([]string{"search", "--type", "event"}, args...)...)
		if plain != typed {
			t.Errorf("%v: output differs from --type event", args)
		}
		if strings.Contains(plain, `"type": "input"`) || !strings.Contains(plain, `"type":`) {
			t.Errorf("%v: types in the default output", args)
		}
	}
}

// TestSearchToolAndFile: --tool narrows tool calls, the type file finds
// the backup copies of the demo archive by content, and filters that do
// not apply are usage errors.
func TestSearchToolAndFile(t *testing.T) {
	t.Parallel()
	e, base := attachEnv(t)
	bash := kindHits(t, e, base, "go", "test", "--type", "tool_call", "--tool", "Bash")["tool_call"]
	all := kindHits(t, e, base, "go", "test", "--type", "tool_call")["tool_call"]
	if len(bash) == 0 || len(bash) > len(all) {
		t.Errorf("Bash tool calls %d of %d", len(bash), len(all))
	}
	if none := kindHits(t, e, base, "go", "test", "--type", "tool_call", "--tool", "NoSuchTool")["tool_call"]; len(none) != 0 {
		t.Errorf("unknown tool: %d hits", len(none))
	}
	files := kindHits(t, e, base, "smoothed", "--type", "file")["file"]
	if len(files) != 1 || !strings.HasSuffix(files[0].s("path"), "/route/elevation.go") || !strings.Contains(files[0].s("snippet"), "[smoothed]") ||
		files[0].s("media_type") != "text/plain" || len(files[0].s("sha256")) != 64 {
		t.Fatalf("file hits = %+v", files)
	}
	both := kindHits(t, e, base, "climb", "--type", "file")["file"]
	if len(both) != 2 {
		t.Errorf("both versions of the file: %d hits", len(both))
	}
	for _, tc := range []struct {
		args []string
		msg  string
	}{
		{[]string{"x", "--type", "input", "--actor", "human"}, "--actor does not apply to type input"},
		{[]string{"x", "--type", "file", "--raw"}, "--raw does not apply to type file"},
		{[]string{"x", "--type", "event", "--tool", "Bash"}, "--tool does not apply to type event"},
		{[]string{"x", "--type", "image", "--has-attachment"}, "--has-attachment does not apply to type image"},
	} {
		r := e.run(t, "", append(append(append([]string{}, base...), "search", "--json"), tc.args...)...)
		if r.code == 0 || !strings.Contains(r.stdout, tc.msg) {
			t.Errorf("%v: %+v", tc.args, r)
		}
	}
	// A limit counts per type, and the cut group says how many it has.
	r := e.run(t, "", append(append([]string{}, base...), "search", "the", "--type", "input,answer", "--limit", "2")...)
	if r.code != 0 || !strings.Contains(r.stderr, "input: 2 of ") || !strings.Contains(r.stderr, "answer: 2 of ") {
		t.Errorf("limit note: %+v", r)
	}
}

// TestSearchToolName (#374): --tool compares without case, the JSON of
// every event hit has tool_name (the tool of a tool call, else null), and
// --tool without --type means --type tool_call.
func TestSearchToolName(t *testing.T) {
	t.Parallel()
	e, base := attachEnv(t)
	exact := kindHits(t, e, base, "go", "test", "--type", "tool_call", "--tool", "Bash")["tool_call"]
	if len(exact) == 0 {
		t.Fatal("no Bash tool calls in the demo archive")
	}
	for _, tool := range []string{"bash", "BASH"} {
		if got := kindHits(t, e, base, "go", "test", "--type", "tool_call", "--tool", tool)["tool_call"]; len(got) != len(exact) {
			t.Errorf("--tool %s: %d hits, --tool Bash: %d", tool, len(got), len(exact))
		}
	}
	// Without --type the type is tool_call.
	plain := kindHits(t, e, base, "go", "test", "--tool", "bash")
	if len(plain["tool_call"]) != len(exact) || len(plain) != 1 {
		t.Errorf("--tool without --type: %d tool_call hits of %d, groups %d", len(plain["tool_call"]), len(exact), len(plain))
	}
	// tool_name: always present; the tool of a call, null for the rest.
	for _, h := range exact {
		if v, ok := h["tool_name"]; !ok || v != "Bash" {
			t.Errorf("tool_call hit tool_name = %v (present %v)", v, ok)
		}
	}
	for ty, hs := range kindHits(t, e, base, "the", "--type", "input,answer,tool_result") {
		for _, h := range hs {
			if v, ok := h["tool_name"]; !ok || v != nil {
				t.Errorf("%s hit tool_name = %v (present %v), want null", ty, v, ok)
			}
		}
	}
	// --tool with another type is still a usage error.
	r := e.run(t, "", append(append([]string{}, base...), "search", "x", "--type", "event", "--tool", "Bash", "--json")...)
	if r.code == 0 || !strings.Contains(r.stdout, "--tool does not apply to type event") {
		t.Errorf("--type event --tool: %+v", r)
	}
}

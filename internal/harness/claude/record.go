package claude

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/seletz/total-recall/internal/model"
)

// truncMark is appended to text that was cut.
const truncMark = " …[truncated]"

// Known record and content block types. The parser and the format check
// (CheckFormat, `total-recall doctor`) share these lists; when Claude Code
// adds a type, add it here (and handle it in the parser if it carries data).
var (
	ingestedTypes = []string{"user", "assistant", "system", "custom-title", "ai-title", "agent-name", "summary"}
	skippedTypes  = []string{
		"attachment", "api-request", "api-request-blob", "api-request-shape", "last-prompt", "mode",
		"permission-mode", "atis-latch", "pr-link", "queue-operation", "bridge-session",
		"file-history-snapshot", "file-history-delta", "cost-state", "tools-used",
		"session-owner",     // account and organization of the session (2.1.317+), no timestamp
		"started", "result", // workflow journal.jsonl
	}
	handledUserBlocks      = []string{"text", "tool_result"}
	ignoredUserBlocks      = []string{"image", "document"}
	handledAssistantBlocks = []string{"text", "tool_use"}
	ignoredAssistantBlocks = []string{"thinking", "redacted_thinking"}
	// toolResultBlocks may appear in a tool_result's content array: text is
	// kept, the others become a "[type]" placeholder.
	toolResultBlocks = []string{"text", "image", "document", "tool_reference"}
	// expectedFields are the top-level fields every record of a type should have.
	expectedFields = map[string][]string{
		"user":          {"timestamp", "sessionId", "uuid", "cwd"},
		"assistant":     {"timestamp", "sessionId", "uuid", "cwd"},
		"system":        {"timestamp", "sessionId", "uuid", "cwd"},
		"custom-title":  {"sessionId"},
		"ai-title":      {"sessionId"},
		"agent-name":    {"sessionId"},
		"session-owner": {"sessionId"},
	}
)

// IngestedRecordTypes returns the record types the adapter reads.
func IngestedRecordTypes() []string { return slices.Clone(ingestedTypes) }

// SkippedRecordTypes returns record types known to carry nothing worth
// archiving; they are skipped on purpose.
func SkippedRecordTypes() []string { return slices.Clone(skippedTypes) }

// HandledUserBlockTypes returns user content block types that become events.
func HandledUserBlockTypes() []string { return slices.Clone(handledUserBlocks) }

// IgnoredUserBlockTypes returns user content block types skipped on purpose.
func IgnoredUserBlockTypes() []string { return slices.Clone(ignoredUserBlocks) }

// HandledAssistantBlockTypes returns assistant content block types that become events.
func HandledAssistantBlockTypes() []string { return slices.Clone(handledAssistantBlocks) }

// IgnoredAssistantBlockTypes returns assistant content block types skipped on purpose.
func IgnoredAssistantBlockTypes() []string { return slices.Clone(ignoredAssistantBlocks) }

// ToolResultBlockTypes returns the block types known inside tool_result content.
func ToolResultBlockTypes() []string { return slices.Clone(toolResultBlocks) }

// ExpectedFields returns the top-level fields every record of type typ
// should have (nil if none are required).
func ExpectedFields(typ string) []string { return slices.Clone(expectedFields[typ]) }

// ingested is the lookup set of ingestedTypes.
var ingested = setOf(ingestedTypes)

func setOf(l ...[]string) map[string]bool {
	m := map[string]bool{}
	for _, x := range l {
		for _, s := range x {
			m[s] = true
		}
	}
	return m
}

// rawRecord is the subset of a transcript line the adapter looks at.
type rawRecord struct {
	Type             string          `json:"type"`
	UUID             string          `json:"uuid"`
	Timestamp        string          `json:"timestamp"`
	SessionID        string          `json:"sessionId"`
	AgentID          string          `json:"agentId"`
	Cwd              string          `json:"cwd"`
	GitBranch        string          `json:"gitBranch"`
	Version          string          `json:"version"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	ScheduledTaskID  string          `json:"scheduledTaskId"`
	Origin           json.RawMessage `json:"origin"`
	Message          *rawMessage     `json:"message"`
	Subtype          string          `json:"subtype"`
	Content          json.RawMessage `json:"content"` // system records
	CustomTitle      string          `json:"customTitle"`
	AgentName        string          `json:"agentName"`
	AITitle          string          `json:"aiTitle"`
	Summary          string          `json:"summary"`
}

type rawMessage struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *rawUsage       `json:"usage"`
}

type rawUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

type rawBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

// fileInfo is what the transcript path tells about its session.
type fileInfo struct {
	base       string // file name without .jsonl
	parentUUID string // set for files below <parent-uuid>/subagents/
}

func pathInfo(path string) fileInfo {
	fi := fileInfo{base: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := len(parts) - 2; i > 0; i-- {
		if parts[i] == "subagents" {
			fi.parentUUID = parts[i-1]
			break
		}
	}
	return fi
}

// parser holds per-Parse-call state.
type parser struct {
	a        *Adapter
	path     string
	pathHash string
	file     fileInfo
	seenMsg  map[string]bool // assistant message ids whose usage was already attributed
}

func newParser(a *Adapter, path string) *parser {
	h := sha1.Sum([]byte(path))
	return &parser{a: a, path: path, pathHash: hex.EncodeToString(h[:]), file: pathInfo(path), seenMsg: map[string]bool{}}
}

// parseLine maps one JSONL line. It returns the record (nil if skipped), the
// record type, and ok=false if the line is not a JSON object.
func (p *parser) parseLine(line []byte, offset int64) (*model.Record, string, bool) {
	var r rawRecord
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, "", false
	}
	if !ingested[r.Type] || p.a.skip[r.Type] {
		return nil, r.Type, true
	}
	ts := parseTime(r.Timestamp)
	sess := p.session(&r, ts)
	rec := &model.Record{Session: sess}
	raw := string(line)
	base := model.Event{SessionID: sess.ID, TS: ts, SourcePath: p.path, SourceOffset: offset, Raw: raw}
	nativeID := r.UUID
	if nativeID == "" {
		nativeID = fmt.Sprintf("%s:%d", p.pathHash, offset)
	}
	evID := HarnessName + ":" + nativeID

	switch r.Type {
	case "custom-title":
		rec.Session.Name = r.CustomTitle
	case "agent-name":
		rec.Session.AgentName = r.AgentName
	case "ai-title":
		rec.Session.AITitle = r.AITitle
	case "summary":
		// Legacy auto-generated title: same rank as ai-title, never overrides custom-title.
		rec.Session.AITitle = r.Summary
	case "system":
		e := base
		e.ID, e.Kind, e.Role, e.Actor = evID, model.KindSystem, "system", model.ActorAutomation
		e.Text = stringOrEmpty(r.Content)
		if e.Text == "" {
			e.Text = r.Subtype
		}
		rec.Events = append(rec.Events, e)
	case "user":
		rec.Events = p.userEvents(&r, base, evID)
	case "assistant":
		rec.Events = p.assistantEvents(&r, base, evID)
		if r.Message != nil && r.Message.Model != "" && r.Message.Model != "<synthetic>" {
			rec.Session.Model = r.Message.Model
		}
	}
	return rec, r.Type, true
}

func (p *parser) session(r *rawRecord, ts time.Time) model.Session {
	s := model.Session{
		Harness:        HarnessName,
		HarnessVersion: r.Version,
		Cwd:            r.Cwd,
		GitBranch:      r.GitBranch,
		StartedAt:      ts,
		EndedAt:        ts,
	}
	if p.file.parentUUID != "" {
		agent := r.AgentID
		if agent == "" {
			agent = strings.TrimPrefix(p.file.base, "agent-")
		}
		s.NativeID = "agent-" + agent
		s.ParentID = HarnessName + ":" + p.file.parentUUID
	} else {
		s.NativeID = r.SessionID
		if s.NativeID == "" {
			s.NativeID = p.file.base
		}
	}
	s.ID = HarnessName + ":" + s.NativeID
	return s
}

func (p *parser) userEvents(r *rawRecord, base model.Event, evID string) []model.Event {
	if r.Message == nil {
		return nil
	}
	flags := userFlags{
		originKind:    originKind(r.Origin),
		isMeta:        r.IsMeta,
		compact:       r.IsCompactSummary,
		scheduled:     r.ScheduledTaskID != "",
		subagentInput: p.file.parentUUID != "",
	}
	text := func(t string) model.Event {
		e := base
		e.Kind, e.Role, e.Actor, e.Text = model.KindMessage, "user", p.a.classifier.Classify(t, flags), t
		return e
	}
	if s, ok := asString(r.Message.Content); ok {
		e := text(s)
		e.ID = evID
		return []model.Event{e}
	}
	var blocks []rawBlock
	if err := json.Unmarshal(r.Message.Content, &blocks); err != nil {
		return nil
	}
	var out []model.Event
	for i, b := range blocks {
		var e model.Event
		switch b.Type {
		case "text":
			e = text(b.Text)
		case "tool_result":
			e = base
			e.Kind, e.Role, e.Actor = model.KindToolResult, "tool", model.ActorTool
			t := resultText(b.Content)
			if b.IsError {
				t = "[error] " + t
			}
			e.Text = truncate(t, p.a.toolResultMax)
		default: // images, documents: nothing searchable
			continue
		}
		e.ID = fmt.Sprintf("%s#%d", evID, i)
		out = append(out, e)
	}
	return out
}

func (p *parser) assistantEvents(r *rawRecord, base model.Event, evID string) []model.Event {
	m := r.Message
	if m == nil {
		return nil
	}
	if m.Model != "<synthetic>" {
		base.Model = m.Model
	}
	base.Role, base.Actor = "assistant", model.ActorAgent
	var blocks []rawBlock
	if s, ok := asString(m.Content); ok {
		blocks = []rawBlock{{Type: "text", Text: s}}
	} else if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return nil
	}
	// Claude Code writes one line per content block, each repeating the API
	// message's usage. Attribute usage only to the first event seen per message id.
	withUsage := m.Usage != nil && (m.ID == "" || !p.seenMsg[m.ID])
	var out []model.Event
	for i, b := range blocks {
		e := base
		switch b.Type {
		case "text":
			e.Kind, e.Text = model.KindMessage, b.Text
		case "tool_use":
			e.Kind, e.ToolName, e.Text = model.KindToolUse, b.Name, truncate(toolInputText(b.Input), p.a.toolInputMax)
		default: // thinking, redacted_thinking, ...
			continue
		}
		e.ID = fmt.Sprintf("%s#%d", evID, i)
		if withUsage {
			u := m.Usage
			e.TokensIn = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
			e.TokensOut = u.OutputTokens
			withUsage = false
			if m.ID != "" {
				p.seenMsg[m.ID] = true
			}
		}
		out = append(out, e)
	}
	return out
}

// toolInputKeys are input fields that summarize a tool call well, in priority order.
var toolInputKeys = []string{"command", "file_path", "path", "url", "query", "pattern", "description", "prompt", "message", "skill"}

// toolInputText summarizes a tool_use input: the first well-known string field,
// else the compact JSON of the input.
func toolInputText(in json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(in, &m); err == nil {
		for _, k := range toolInputKeys {
			if s, ok := asString(m[k]); ok && s != "" {
				return s
			}
		}
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, in); err != nil {
		return string(in)
	}
	return buf.String()
}

// resultText extracts readable text from tool_result content (string or block array).
func resultText(c json.RawMessage) string {
	if s, ok := asString(c); ok {
		return s
	}
	var blocks []rawBlock
	if err := json.Unmarshal(c, &blocks); err != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "":
		default:
			parts = append(parts, "["+b.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}

func originKind(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if s, ok := asString(raw); ok {
		return s
	}
	var o struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.Kind
}

func asString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func stringOrEmpty(raw json.RawMessage) string {
	s, _ := asString(raw)
	return s
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// truncate cuts s to at most max bytes on a rune boundary and appends truncMark.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + truncMark
}

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

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/inputkind"
	"github.com/seletz/total-recall/internal/model"
)

// truncMark is appended to text that was cut.
const truncMark = " …[truncated]"

// Known record and content block types. The parser and the format check
// (CheckFormat, `total-recall doctor`) share these lists; when Claude Code
// adds a type, add it here (and handle it in the parser if it carries data).
//
// Every record type falls into one of three groups: ingestedTypes become
// events or session fields; skippedTypes are dropped on purpose (the lists
// say why); everything else, metaTypes and types the adapter has never
// seen, is kept as a meta record (model.Meta: the whole line, redacted and
// compressed, no event). doctor still reports the unknown ones.
var (
	ingestedTypes = []string{
		"user", "assistant", "system", "custom-title", "ai-title", "agent-name", "summary",
		"session-owner", // account and organization of the session (2.1.317+), no timestamp
		"continued-in",  // session this one was continued in (2.1.318+), once at the end
	}
	// metaTypes are kept as meta records. Measured on a local archive
	// (#182): together about 40 MiB compressed; the same content is stored
	// once per session, which collapses the per-turn state records (mode,
	// permission-mode, atis-latch, pr-link, bridge-session) to a few rows
	// per session.
	metaTypes = []string{
		"attachment",            // context the harness injected: instructions, files, hook output, queued commands, system prompt, ... (subtypes below)
		"api-request-shape",     // system prompt and tool definitions of a request shape; the shape leaves the line as a blob (metaLifts)
		"queue-operation",       // text typed while the agent worked: enqueue, dequeue, remove; a removed command is nowhere else
		"file-history-snapshot", // index of the file history (tracked file backups), no timestamp of its own
		"file-history-delta",    // one backup of one file
		"pr-link",               // pull request of the session, repeated every turn
		"bridge-session",        // remote bridge of the session, no timestamp
		"permission-mode",       // permission mode of the session, no timestamp
		"mode",                  // mode of the session, no timestamp
		"atis-latch",            // no timestamp
		"cost-state",            // cost, duration and lines per model and session
		"tools-used",            // tools used so far, no timestamp
		"history-suppression",   // cause and account a history suppression was vetoed against; ts, no timestamp
		"started", "result",     // workflow journal.jsonl: start and result of a workflow agent
	}
	// skippedTypes carry nothing the archive needs: the API request cache
	// (hashes of prompt blocks and the message content behind them, which
	// repeats the events; 1.25 GiB of 1.7 GiB skipped lines locally) and
	// the last prompt, which repeats the latest user message.
	// skip_record_types in the config adds to the list.
	skippedTypes = []string{
		"api-request", "api-request-blob", "last-prompt",
	}
	// skippedAttachmentTypes are the attachment subtypes dropped on
	// purpose: the harness's catalogs of tools, skills, agents and MCP
	// instructions, and the reminders it sends itself. Locally they were
	// a third of the attachment bytes. A subtype not in this list is
	// kept, whether known or new; skip_record_types takes
	// "attachment/<subtype>" entries.
	skippedAttachmentTypes = []string{
		"total_tokens_reminder", "bash_output_audience_note",
		"deferred_tools_record", "deferred_tools_delta", "agent_listing_delta", "skill_listing", "mcp_instructions_delta",
	}
	// metaLifts names, per record type (attachment subtypes as
	// AttachmentSkipKey), the JSON values that leave a meta record's line
	// as a blob (blobref.Lift, media type application/json): the system
	// prompt and tool definitions of a request shape, and the system
	// prompt and (newer versions) the tool definitions of a prompt
	// snapshot. They repeat per session and across sessions (locally 100
	// MB each, a few hundred distinct values); the store keeps every value
	// once per hash.
	metaLifts = map[string][]string{
		"api-request-shape":                  {"shape"},
		AttachmentSkipKey("prompt_snapshot"): {"attachment.systemPrompt", "attachment.tools"},
	}
	// image and document blocks become events with a placeholder text;
	// their data goes to the blobs (see attachments.go).
	handledUserBlocks      = []string{"text", "tool_result", "image", "document"}
	ignoredUserBlocks      = []string{}
	handledAssistantBlocks = []string{"text", "tool_use"}
	ignoredAssistantBlocks = []string{"thinking", "redacted_thinking"}
	// toolResultBlocks may appear in a tool_result's content array: text is
	// kept, the others become a "[type]" placeholder.
	toolResultBlocks = []string{"text", "image", "document", "tool_reference"}
	// expectedFields are the top-level fields every record of a type should have.
	expectedFields = map[string][]string{
		"user":                {"timestamp", "sessionId", "uuid", "cwd"},
		"assistant":           {"timestamp", "sessionId", "uuid", "cwd"},
		"system":              {"timestamp", "sessionId", "uuid", "cwd"},
		"custom-title":        {"sessionId"},
		"ai-title":            {"sessionId"},
		"agent-name":          {"sessionId"},
		"session-owner":       {"sessionId"},
		"continued-in":        {"timestamp", "sessionId", "continuedInSessionId"},
		"history-suppression": {"sessionId"},
	}
)

// IngestedRecordTypes returns the record types the adapter reads.
func IngestedRecordTypes() []string { return slices.Clone(ingestedTypes) }

// MetaRecordTypes returns the record types the adapter keeps as meta
// records (model.Meta). Types in no list are kept as well.
func MetaRecordTypes() []string { return slices.Clone(metaTypes) }

// SkippedRecordTypes returns record types known to carry nothing worth
// archiving; they are skipped on purpose.
func SkippedRecordTypes() []string { return slices.Clone(skippedTypes) }

// SkippedAttachmentTypes returns the attachment subtypes skipped on
// purpose; every other subtype is kept as a meta record.
func SkippedAttachmentTypes() []string { return slices.Clone(skippedAttachmentTypes) }

// AttachmentSkipKey is the skip_record_types entry for an attachment subtype.
func AttachmentSkipKey(subtype string) string { return "attachment/" + subtype }

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

// ingested and skipped are the lookup sets of ingestedTypes and
// skippedTypes; skippedAttachments holds the AttachmentSkipKey of every
// skippedAttachmentTypes entry.
var (
	ingested           = setOf(ingestedTypes)
	skipped            = setOf(skippedTypes)
	skippedAttachments = map[string]bool{}
)

func init() {
	for _, t := range skippedAttachmentTypes {
		skippedAttachments[AttachmentSkipKey(t)] = true
	}
}

// metaVolatileFields are the fields left out when a meta record without a
// uuid is hashed for its ID: the same content written again in another
// turn gets the same ID and is stored once per session.
var metaVolatileFields = []string{"uuid", "timestamp", "parentUuid"}

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
	Entrypoint       string          `json:"entrypoint"`
	Message          *rawMessage     `json:"message"`
	Subtype          string          `json:"subtype"`
	Content          json.RawMessage `json:"content"` // system records
	CustomTitle      string          `json:"customTitle"`
	AgentName        string          `json:"agentName"`
	AITitle          string          `json:"aiTitle"`
	Summary          string          `json:"summary"`
	AccountUUID      string          `json:"accountUuid"`          // session-owner
	OrganizationUUID string          `json:"organizationUuid"`     // session-owner
	ContinuedIn      string          `json:"continuedInSessionId"` // continued-in
	Attachment       *struct {
		Type string `json:"type"`
	} `json:"attachment"` // attachment
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
	Source  json.RawMessage `json:"source"` // image, document
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
	lastTS   time.Time       // time stamp of the last record that had one (meta records without one borrow it)
	// backups are the backup file names already read in this call (the
	// first record that names one carries it); missing and oversized
	// count the ones that could not be read.
	backups            map[string]bool
	missing, oversized int64
}

func newParser(a *Adapter, path string) *parser {
	h := sha1.Sum([]byte(path))
	return &parser{a: a, path: path, pathHash: hex.EncodeToString(h[:]), file: pathInfo(path), seenMsg: map[string]bool{}, backups: map[string]bool{}}
}

// parseLine maps one JSONL line. It returns the record (nil if skipped), the
// record type, and ok=false if the line is not a JSON object.
func (p *parser) parseLine(line []byte, offset int64) (*model.Record, string, bool) {
	// Attachments leave the line first: what the record parser and the
	// events see is the line with references, which is what raw stores.
	line, atts := extractAttachments(line)
	var r rawRecord
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, "", false
	}
	if p.skips(&r) {
		return nil, r.Type, true
	}
	ts := parseTime(r.Timestamp)
	if !ts.IsZero() {
		p.lastTS = ts
	}
	if !ingested[r.Type] {
		return p.metaRecord(&r, line, offset), r.Type, true
	}
	if r.Type == "continued-in" {
		// Written when the session is resumed, possibly days later: it must
		// not stretch the session's end.
		ts = time.Time{}
	}
	sess := p.session(&r, ts)
	rec := &model.Record{Session: sess}
	// raw is the whole line (#180): the archive stands on its own when
	// the transcript is gone. Only attachment data is referenced, not
	// inlined (#181).
	base := model.Event{SessionID: sess.ID, TS: ts, SourcePath: p.path, SourceOffset: offset, Raw: string(line)}
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
	case "session-owner":
		rec.Session.AccountID, rec.Session.OrganizationID = r.AccountUUID, r.OrganizationUUID
	case "continued-in":
		if r.ContinuedIn != "" {
			rec.Session.ContinuedIn = HarnessName + ":" + r.ContinuedIn
		}
	case "system":
		e := base
		e.ID, e.Kind, e.Role, e.Actor = evID, model.KindSystem, "system", model.ActorAutomation
		e.Text = stringOrEmpty(r.Content)
		if e.Text == "" {
			e.Text = r.Subtype
		}
		rec.Events = append(rec.Events, e)
	case "user":
		rec.Events = p.userEvents(&r, base, evID, atts)
		attach(rec.Events, atts)
	case "assistant":
		rec.Events = p.assistantEvents(&r, base, evID)
		if r.Message != nil && r.Message.Model != "" && r.Message.Model != "<synthetic>" {
			rec.Session.Model = r.Message.Model
		}
	}
	return rec, r.Type, true
}

// skips reports whether the record is dropped on purpose: its type is in
// skippedTypes or configured in skip_record_types, or it is an attachment
// whose subtype is in skippedAttachmentTypes or configured as
// "attachment/<subtype>".
func (p *parser) skips(r *rawRecord) bool {
	if skipped[r.Type] || p.a.skip[r.Type] {
		return true
	}
	if r.Type == "attachment" && r.Attachment != nil {
		k := AttachmentSkipKey(r.Attachment.Type)
		return skippedAttachments[k] || p.a.skip[k]
	}
	return false
}

// metaRecord builds the record for a line that is kept as a meta record:
// the session it belongs to (without time stamps, a meta record never
// widens a session) and one model.Meta with the whole line. The ID is the
// record's uuid; a record without one is identified by a hash of session,
// type and content without metaVolatileFields, so the same content is
// stored once per session.
func (p *parser) metaRecord(r *rawRecord, line []byte, offset int64) *model.Record {
	sess := p.session(r, time.Time{})
	line, atts := liftMeta(r, line)
	m := model.Meta{SessionID: sess.ID, TS: parseTime(r.Timestamp), Type: r.Type, SourcePath: p.path, SourceOffset: offset, Raw: string(line), Attachments: atts}
	if m.TS.IsZero() {
		m.TS = p.lastTS
	}
	if r.UUID != "" {
		m.ID = HarnessName + ":" + r.UUID
	} else {
		m.ID = HarnessName + ":" + metaHash(sess.NativeID, r.Type, line)
	}
	// The backups of edited files join the record after its ID is fixed:
	// whether a copy is still on disk must not change which row the
	// record is.
	if out, fh := p.fileHistory(r.Type, line); len(fh) > 0 {
		m.Raw = string(out)
		for _, a := range fh {
			m.Attachments = append(m.Attachments, model.Attachment{SHA256: a.SHA256, MediaType: a.MediaType, Data: a.Data})
		}
	}
	return &model.Record{Session: sess, Meta: []model.Meta{m}}
}

// liftMeta takes the values named in metaLifts for the record's type out
// of the line (blobref.Lift) and returns the line with references and the
// attachments; a type without an entry, or a line without the values,
// comes back as it is.
func liftMeta(r *rawRecord, line []byte) ([]byte, []model.Attachment) {
	key := r.Type
	if r.Type == "attachment" && r.Attachment != nil {
		key = AttachmentSkipKey(r.Attachment.Type)
	}
	paths := metaLifts[key]
	if len(paths) == 0 {
		return line, nil
	}
	out, lifted, err := blobref.Lift(line, func(path string) (string, bool) {
		return "application/json", slices.Contains(paths, path)
	})
	if err != nil || len(lifted) == 0 {
		return line, nil
	}
	atts := make([]model.Attachment, len(lifted))
	for i, a := range lifted {
		atts[i] = model.Attachment{SHA256: a.SHA256, MediaType: a.MediaType, Data: a.Data}
	}
	return out, atts
}

// metaHash hashes a meta record's content for its ID: sha1 over session,
// type and the line as compact JSON with sorted keys, without
// metaVolatileFields. A line that is not an object is hashed as it is.
func metaHash(session, typ string, line []byte) string {
	content := line
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err == nil && obj != nil {
		for _, k := range metaVolatileFields {
			delete(obj, k)
		}
		if b, err := json.Marshal(obj); err == nil {
			content = b
		}
	}
	h := sha1.New()
	h.Write([]byte(session))
	h.Write([]byte{0})
	h.Write([]byte(typ))
	h.Write([]byte{0})
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
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

func (p *parser) userEvents(r *rawRecord, base model.Event, evID string, atts []blobref.Attachment) []model.Event {
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
	remote := inputkind.RemoteSignal(r.Entrypoint, flags.originKind)
	text := func(t string) model.Event {
		e := base
		e.Kind, e.Role, e.Actor, e.Text = model.KindMessage, "user", p.a.classifier.Classify(t, flags), t
		if e.Actor == model.ActorHuman {
			e.InputKind = inputkind.Of(t, remote)
		}
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
	sizes := attachmentSizes(atts)
	var out []model.Event
	for i, b := range blocks {
		var e model.Event
		switch b.Type {
		case "text":
			e = text(b.Text)
		case "tool_result":
			e = base
			e.Kind, e.Role, e.Actor = model.KindToolResult, "tool", model.ActorTool
			t := resultText(b.Content, sizes)
			if b.IsError {
				t = "[error] " + t
			}
			e.Text = truncate(t, p.a.toolResultMax)
		case "image", "document":
			// A pasted image or document: an event of its own, so the
			// line and its blob are archived; the text is a placeholder.
			e = text(placeholder(b, sizes))
			if e.Actor == model.ActorHuman && e.InputKind != model.InputRemote {
				e.InputKind = model.InputPasted
			}
		default:
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
			e.TokensCacheRead, e.TokensCacheWrite = u.CacheReadInputTokens, u.CacheCreationInputTokens
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

// resultText extracts readable text from tool_result content (string or
// block array); images and documents become placeholders.
func resultText(c json.RawMessage, sizes map[string]int) string {
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
		case "image", "document":
			parts = append(parts, placeholder(b, sizes))
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

// Package model holds the harness-neutral types shared by adapters, ingest and store.
package model

import "time"

// Actor classifies who produced an event.
type Actor string

const (
	ActorHuman      Actor = "human"      // typed by the person
	ActorAgent      Actor = "agent"      // produced by the model
	ActorPeer       Actor = "peer"       // message from another agent session
	ActorAutomation Actor = "automation" // harness/system injected, scheduled prompts, notifications
	ActorTool       Actor = "tool"       // tool results
)

// Valid reports whether a is one of the defined actors.
func (a Actor) Valid() bool {
	switch a {
	case ActorHuman, ActorAgent, ActorPeer, ActorAutomation, ActorTool:
		return true
	}
	return false
}

// Kind classifies what an event is.
type Kind string

const (
	KindMessage    Kind = "message"
	KindToolUse    Kind = "tool_use"
	KindToolResult Kind = "tool_result"
	KindSystem     Kind = "system"
	KindMeta       Kind = "meta"
)

// Valid reports whether k is one of the defined kinds.
func (k Kind) Valid() bool {
	switch k {
	case KindMessage, KindToolUse, KindToolResult, KindSystem, KindMeta:
		return true
	}
	return false
}

// Event roles.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
	RoleTool      = "tool"
)

// ValidRole reports whether r is one of the defined event roles.
func ValidRole(r string) bool {
	switch r {
	case RoleUser, RoleAssistant, RoleSystem, RoleTool:
		return true
	}
	return false
}

// Session is a (partial) session description. Zero values mean "unknown" and never
// overwrite known values in the store (see docs/explanation/design.md, upsert rule).
type Session struct {
	ID             string // "<harness>:<native id>"
	Harness        string
	HarnessVersion string
	NativeID       string
	ParentID       string
	Name           string // explicit title (Claude: custom-title); authoritative
	AITitle        string // harness-generated title (Claude: ai-title, legacy summary)
	AgentName      string
	Cwd            string
	GitRoot        string
	GitRemoteURL   string
	GitBranch      string
	Model          string
	// ContinuedIn is the session this one was continued in (Claude:
	// continued-in record), as a full session ID; empty if none is known.
	// The target session may be ingested later or never.
	ContinuedIn string
	// AccountID and OrganizationID identify the account the session ran on
	// (Claude: session-owner record); empty if unknown.
	AccountID      string
	OrganizationID string
	StartedAt      time.Time
	EndedAt        time.Time
}

// DisplayName returns the best human-readable title: Name, else AITitle,
// else AgentName (empty if none is known).
func (s Session) DisplayName() string {
	switch {
	case s.Name != "":
		return s.Name
	case s.AITitle != "":
		return s.AITitle
	}
	return s.AgentName
}

// Event is one normalized entry of a session transcript.
type Event struct {
	ID           string // "<harness>:<native event id>[#<block>]"
	SessionID    string
	TS           time.Time
	Kind         Kind
	Role         string // user, assistant, system, tool
	Actor        Actor
	Text         string
	ToolName     string
	Model        string
	TokensIn     int64
	TokensOut    int64
	SourcePath   string
	SourceOffset int64
	// Raw is the original JSON line (redacted before storing), with the
	// data of every attachment replaced by a blobref reference. It is set
	// by the adapters on ingest; the store's read queries leave it empty.
	Raw string
	// RawTruncated is set when Raw was dropped because the line exceeded the
	// configured size cap (ingest.raw_max_bytes).
	RawTruncated bool
	// Attachments are the binary parts (images, documents) the adapter
	// took out of Raw, in the order of their references; the store keeps
	// them as blobs. Read queries leave the list empty.
	Attachments []Attachment
}

// Attachment is one binary part of a transcript line (an image or a
// document delivered as base64) that lives outside Raw: the line refers to
// it by SHA256 (see internal/blobref), the store keeps the bytes once per
// hash.
type Attachment struct {
	// SHA256 is the hex digest of Data, the key of the stored blob.
	SHA256 string
	// MediaType is the type the harness delivered, e.g. "image/png".
	MediaType string
	// Data is the attachment as delivered, decoded.
	Data []byte
}

// Record is what an adapter emits per transcript line: session facts and zero or more events.
type Record struct {
	Session Session
	Events  []Event
}

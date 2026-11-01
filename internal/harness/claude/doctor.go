package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/seletz/total-recall/internal/harness"
)

var _ harness.FormatChecker = (*Adapter)(nil)

// CheckFormat implements harness.FormatChecker: it reads all sources
// (read-only, complete lines only) and reports record types, content block
// types and missing fields the adapter does not expect, malformed lines and
// the Claude Code versions seen. Record types configured with
// WithSkipRecordTypes count as known.
func (a *Adapter) CheckFormat(ctx context.Context) (harness.FormatReport, error) {
	r := harness.NewFormatReport(a.Name())
	srcs, err := a.Sources()
	if err != nil {
		return r, err
	}
	c := &checker{
		r:        &r,
		known:    setOf(ingestedTypes, skippedTypes),
		user:     setOf(handledUserBlocks, ignoredUserBlocks),
		asst:     setOf(handledAssistantBlocks, ignoredAssistantBlocks),
		result:   setOf(toolResultBlocks),
		versions: map[string]int64{},
	}
	for t := range a.skip {
		c.known[t] = true
	}
	r.Sources = len(srcs)
	for _, p := range srcs {
		if err := c.file(ctx, p); err != nil {
			return r, err
		}
	}
	r.SetVersions(c.versions)
	return r, nil
}

type checker struct {
	r                         *harness.FormatReport
	known, user, asst, result map[string]bool
	versions                  map[string]int64
}

func (c *checker) file(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	rd := bufio.NewReaderSize(f, 1<<16)
	for n := 1; ; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := rd.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return nil // a partial trailing line is still being written
		}
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		c.r.Lines++
		c.line(line, fmt.Sprintf("%s:%d", path, n))
	}
}

func (c *checker) line(line []byte, where string) {
	var rec map[string]json.RawMessage
	if err := json.Unmarshal(line, &rec); err != nil {
		c.r.Malformed.Add(where)
		return
	}
	typ, _ := asString(rec["type"])
	if v, ok := asString(rec["version"]); ok && v != "" {
		c.versions[v]++
	}
	if !c.known[typ] {
		add(c.r.UnknownRecordTypes, typ, where)
	}
	for _, f := range expectedFields[typ] {
		if v, ok := rec[f]; !ok || string(v) == "null" || string(v) == `""` {
			m := c.r.MissingFields[typ]
			if m == nil {
				m = map[string]*harness.Finding{}
				c.r.MissingFields[typ] = m
			}
			add(m, f, where)
		}
	}
	switch typ {
	case "user":
		c.blocks(rec["message"], typ, c.user, where)
	case "assistant":
		c.blocks(rec["message"], typ, c.asst, where)
	}
}

// blocks checks the content block types of a message.
func (c *checker) blocks(msg json.RawMessage, typ string, known map[string]bool, where string) {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if len(msg) == 0 || json.Unmarshal(msg, &m) != nil {
		return
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(m.Content, &blocks) != nil {
		return // string content or none
	}
	for _, b := range blocks {
		if !known[b.Type] {
			add(c.r.UnknownBlockTypes, typ+"/"+b.Type, where)
		}
		if b.Type != "tool_result" {
			continue
		}
		var inner []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b.Content, &inner) != nil {
			continue
		}
		for _, ib := range inner {
			if !c.result[ib.Type] {
				add(c.r.UnknownBlockTypes, "tool_result/"+ib.Type, where)
			}
		}
	}
}

func add(m map[string]*harness.Finding, key, where string) {
	f := m[key]
	if f == nil {
		f = &harness.Finding{}
		m[key] = f
	}
	f.Add(where)
}

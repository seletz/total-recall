package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/model"
)

// Attachments (#181). Claude Code writes images and documents into a
// transcript line as base64 in two shapes: the API's content source
// ({"type":"base64","media_type":…,"data":…} below an image or document
// block of a user message or of a tool_result's content) and, for the
// Read tool, a copy in toolUseResult.file ({"base64":…,"type":"image/png",
// …}). Both leave the line before anything else looks at it: raw keeps a
// reference (blobref.Ref) and the store keeps the bytes once per hash.
// The events get the attachments of their own block (the tool_result
// event also those of toolUseResult) and a placeholder in their text.

// attachmentRule is the blobref.Rule for the two shapes.
func attachmentRule(f blobref.Fields) (dataKey, mediaType string, ok bool) {
	if f["type"] == "base64" && f["media_type"] != "" && f["data"] != "" {
		return "data", f["media_type"], true
	}
	if f["base64"] != "" && strings.Contains(f["type"], "/") {
		return "base64", f["type"], true
	}
	return "", "", false
}

// extractAttachments returns line with references in place of the
// attachment data, and the attachments. A line without the marker is
// returned as it is; a line that is not JSON too (parseLine reports it).
func extractAttachments(line []byte) ([]byte, []blobref.Attachment) {
	if !bytes.Contains(line, []byte(blobref.Marker)) {
		return line, nil
	}
	out, atts, err := blobref.Extract(line, attachmentRule)
	if err != nil {
		return line, nil
	}
	return out, atts
}

// attachmentSizes maps the hash of every attachment to its size, for the
// placeholders.
func attachmentSizes(atts []blobref.Attachment) map[string]int {
	if len(atts) == 0 {
		return nil
	}
	m := make(map[string]int, len(atts))
	for _, a := range atts {
		m[a.SHA256] = len(a.Data)
	}
	return m
}

// placeholder is the text of an image or document block: "[image
// image/png 34.2 KiB]" when the data was extracted, "[image image/png]"
// for a source of another kind with a media type, else "[image]".
func placeholder(b rawBlock, sizes map[string]int) string {
	var src struct {
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	_ = json.Unmarshal(b.Source, &src)
	if src.MediaType == "" {
		return "[" + b.Type + "]"
	}
	if ref, ok := blobref.Parse(src.Data); ok {
		if n, ok := sizes[ref.SHA256]; ok {
			return "[" + b.Type + " " + src.MediaType + " " + sizeLabel(n) + "]"
		}
	}
	return "[" + b.Type + " " + src.MediaType + "]"
}

// sizeLabel formats a byte count for a placeholder: "29 B", "1.2 KiB", "3.4 MiB".
func sizeLabel(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return strconv.Itoa(n) + " B"
}

// attach distributes the attachments of a line to its events by their
// path: data below message.content[i] goes to the event of block i
// (its ID ends in "#i"), data elsewhere (toolUseResult) to the first
// tool_result event; without such an event, to the first event.
func attach(events []model.Event, atts []blobref.Attachment) {
	if len(events) == 0 || len(atts) == 0 {
		return
	}
	for _, a := range atts {
		i := 0
		if block, ok := blockIndex(a.Path); ok {
			if j := eventOfBlock(events, block); j >= 0 {
				i = j
			}
		} else if j := firstOfKind(events, model.KindToolResult); j >= 0 {
			i = j
		}
		events[i].Attachments = append(events[i].Attachments, model.Attachment{SHA256: a.SHA256, MediaType: a.MediaType, Data: a.Data})
	}
}

// blockIndex returns the content block index of a path below
// message.content.
func blockIndex(path string) (int, bool) {
	const prefix = "message.content["
	if !strings.HasPrefix(path, prefix) {
		return 0, false
	}
	rest := path[len(prefix):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	return n, err == nil
}

func eventOfBlock(events []model.Event, block int) int {
	suffix := "#" + strconv.Itoa(block)
	for i, e := range events {
		if strings.HasSuffix(e.ID, suffix) {
			return i
		}
	}
	return -1
}

func firstOfKind(events []model.Event, k model.Kind) int {
	for i, e := range events {
		if e.Kind == k {
			return i
		}
	}
	return -1
}

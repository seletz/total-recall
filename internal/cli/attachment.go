package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/store"
)

// --has-attachment[=any|image|document] (#272) of search and sessions:
// what carries an attachment, an image or a document of event_blobs.

// hasAttachmentFlag is the flag name.
const hasAttachmentFlag = "has-attachment"

// attachmentActors are the values of sessions --actor.
var attachmentActors = []string{"human", "agent", "peer", "automation", "tool"}

// registerHasAttachment adds --has-attachment to c. The value is optional
// and must follow with "=": a bare --has-attachment means any kind.
func registerHasAttachment(c *cobra.Command, kind *string, what string) {
	c.Flags().StringVar(kind, hasAttachmentFlag, "", "only "+what+" with an attachment, of the kind any (default), image or document: --has-attachment=image")
	c.Flags().Lookup(hasAttachmentFlag).NoOptDefVal = "any"
}

// attachmentFilter resolves --has-attachment: nil when the flag is not
// set. actor is the actor of the event with the attachment.
func attachmentFilter(cmd *cobra.Command, kind, actor string) (*store.AttachmentFilter, error) {
	if !cmd.Flags().Changed(hasAttachmentFlag) {
		return nil, nil
	}
	k, err := store.ParseAttachmentKind(kind)
	if err != nil {
		return nil, usagef("invalid --%s %q (any, image, document)", hasAttachmentFlag, kind)
	}
	return &store.AttachmentFilter{Kind: k, Actor: actor}, nil
}

// checkAttachmentActor rejects a sessions --actor that is unknown or
// that has no --has-attachment to belong to.
func checkAttachmentActor(actor string, hasFilter bool) error {
	switch {
	case actor == "":
		return nil
	case !slices.Contains(attachmentActors, actor):
		return usagef("invalid --actor %q (%s)", actor, strings.Join(attachmentActors, ", "))
	case !hasFilter:
		return usagef("--actor needs --%s", hasAttachmentFlag)
	}
	return nil
}

// attachmentJSON converts the attachments of an event; never nil.
func attachmentJSON(list []store.EventAttachment) []AttachmentJSON {
	out := make([]AttachmentJSON, 0, len(list))
	for _, a := range list {
		out = append(out, AttachmentJSON{SHA256: a.SHA256, MediaType: a.MediaType, Bytes: a.Bytes, Position: a.Position})
	}
	return out
}

// attachmentLabel is the text-output summary of the attachments of an
// event: "image/png 956 B, application/pdf 342 B".
func attachmentLabel(list []store.EventAttachment) string {
	parts := make([]string, len(list))
	for i, a := range list {
		parts[i] = a.MediaType + " " + sizeLabel(a.Bytes)
	}
	return strings.Join(parts, ", ")
}

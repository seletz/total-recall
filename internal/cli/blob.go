package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/seletz/total-recall/internal/config"
	"github.com/seletz/total-recall/internal/store"
)

// BlobJSON is the JSON output of blob: the metadata of the attachment and
// the events and meta records whose raw line refers to it; Out is the
// file it was written to, null without --out. The data itself is never
// part of the JSON.
type BlobJSON struct {
	SHA256      string   `json:"sha256"`
	MediaType   string   `json:"media_type"`
	Bytes       int64    `json:"bytes"`
	StoredBytes int64    `json:"stored_bytes"`
	Events      []string `json:"events"`
	MetaRecords []string `json:"meta_records"`
	// Memory lists the memory files that had this blob as content.
	Memory []MemoryUseJSON `json:"memory"`
	Out    NullString      `json:"out"`
}

func (e *env) blobCmd() *cobra.Command {
	var of outputFlags
	var out string
	c := &cobra.Command{
		Use:   "blob HASH",
		Short: "Write an attachment (image, document, system prompt) of the archive to a file or stdout",
		Long: `Write an attachment of the archive, identified by its SHA-256 (full digest
or a unique prefix, as show lists it), to --out FILE or to stdout: an image
or document of a message, or a part of a meta record such as a system
prompt (application/json), or a version of a memory file (text/markdown,
see memory --history). Binary data is not written to a terminal: use
--out or a pipe. With --json only the metadata is printed (type, size, the
events, meta records and memory files that refer to it).`,
		Args: usageArgs(cobra.ExactArgs(1)),
	}
	c.RunE = e.readRun(&of, func(cmd *cobra.Command, args []string, o *output) error {
		cfg, err := e.loadConfig()
		if err != nil {
			return err
		}
		st, err := e.openRead(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		ref := strings.ToLower(strings.TrimSpace(args[0]))
		info, data, err := st.Blob(cmd.Context(), ref)
		switch {
		case errors.Is(err, store.ErrNotFound):
			return errKind(ErrKindNotFound, fmt.Errorf("no attachment with hash %q", args[0]))
		case errors.Is(err, store.ErrAmbiguous):
			return errKind(ErrKindAmbiguous, fmt.Errorf("%q matches more than one attachment; use a longer prefix", args[0]))
		case err != nil:
			return err
		}
		if out != "" {
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return err
			}
		}
		if o.format.IsJSON() {
			events, err := st.BlobEvents(cmd.Context(), info.SHA256)
			if err != nil {
				return err
			}
			metas, err := st.BlobMeta(cmd.Context(), info.SHA256)
			if err != nil {
				return err
			}
			mem, err := blobMemory(cmd.Context(), st, cfg, info.SHA256)
			if err != nil {
				return err
			}
			return o.object(BlobJSON{SHA256: info.SHA256, MediaType: info.MediaType, Bytes: info.Bytes, StoredBytes: info.StoredBytes,
				Events: events, MetaRecords: metas, Memory: mem, Out: NullString(out)})
		}
		if out != "" {
			_, _ = fmt.Fprintf(o.w, "wrote %s (%s, %s)\n", out, info.MediaType, sizeLabel(info.Bytes))
			return nil
		}
		if e.app.TerminalWidth() > 0 && !strings.HasPrefix(info.MediaType, "text/") && info.MediaType != "application/json" {
			return usagef("refusing to write %s (%s) to a terminal; use --out FILE or a pipe", info.MediaType, sizeLabel(info.Bytes))
		}
		_, err = o.w.Write(data)
		return err
	})
	of.register(c)
	c.Flags().StringVar(&out, "out", "", "write the attachment to this file instead of stdout")
	return c
}

// blobMemory lists the memory files that had the blob as content, with
// the project names of the memory command.
func blobMemory(ctx context.Context, st *store.Store, cfg *config.Config, sha string) ([]MemoryUseJSON, error) {
	uses, err := st.BlobMemory(ctx, sha)
	if err != nil || len(uses) == 0 {
		return []MemoryUseJSON{}, err
	}
	ps, err := memoryProjects(ctx, st, cfg)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(ps))
	for _, p := range ps {
		names[p.ID] = p.Name
	}
	out := make([]MemoryUseJSON, 0, len(uses))
	for _, u := range uses {
		out = append(out, MemoryUseJSON{Project: names[u.ProjectID], ProjectKey: u.Key, Harness: u.Harness, Path: u.Path, TS: isoTime(u.TS)})
	}
	return out, nil
}

// sizeLabel formats a byte count: "29 B", "1.2 KiB", "3.4 MiB".
func sizeLabel(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

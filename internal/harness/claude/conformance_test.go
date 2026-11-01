package claude

import (
	"testing"

	"github.com/seletz/total-recall/internal/harness"
	"github.com/seletz/total-recall/internal/harness/harnesstest"
)

func TestConformance(t *testing.T) {
	harnesstest.Run(t, func(dir string) harness.Adapter { return New(dir) }, "testdata/projects")
}

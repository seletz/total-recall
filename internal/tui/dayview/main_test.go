package dayview

import (
	"os"
	"testing"

	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// TestMain builds the demo archive once for the tests that read it.
func TestMain(m *testing.M) { os.Exit(fixturedb.DemoMain(m)) }

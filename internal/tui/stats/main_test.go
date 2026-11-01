package stats

import (
	"os"
	"testing"

	"github.com/NimbleMarkets/ntcharts/v2/picture"
)

// TestMain pins the process-wide Kitty Graphics state the picture package
// keeps: the probe is forced to "unsupported" and consumed, so no test ever
// queries the terminal running the tests, and tmux passthrough is off, so
// the escape sequences the tests inspect are not wrapped when the tests run
// inside tmux. Tests inject the probe outcome through KittyOptions.Probe.
func TestMain(m *testing.M) {
	picture.ForceKittyCapability(picture.KittyCapabilityUnsupported)
	_ = picture.QueryKittySupport()
	picture.SetTmuxPassthrough(false)
	os.Exit(m.Run())
}

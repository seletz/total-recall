package tui

import (
	"os"
	"testing"

	"github.com/NimbleMarkets/ntcharts/v2/picture"
)

// TestMain pins the Kitty Graphics probe to "unsupported" for the whole
// package: the stats view would otherwise probe the terminal running the
// tests, and the raw output and the status line would depend on it.
func TestMain(m *testing.M) {
	picture.ForceKittyCapability(picture.KittyCapabilityUnsupported)
	os.Exit(m.Run())
}

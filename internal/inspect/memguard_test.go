package inspect

import (
	"os"
	"testing"

	"github.com/dani-gouken/lumen/internal/memguard"
)

// TestMain guards the tests: they run programs, and a program that takes
// memory without end must end the tests, not the machine they run on.
func TestMain(m *testing.M) {
	memguard.EnforceInTests()
	os.Exit(m.Run())
}

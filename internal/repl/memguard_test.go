package repl

import (
	"os"
	"testing"

	"github.com/dani-gouken/lumen/internal/memguard"
)

func TestMain(m *testing.M) {
	memguard.EnforceInTests()
	os.Exit(m.Run())
}

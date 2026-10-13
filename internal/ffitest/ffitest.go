// Package ffitest gives the tests a C library to call: probe.c, compiled
// by the C compiler of the machine the first time a test asks for it. The
// tests that need it are skipped where there is no compiler.
package ffitest

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/dani-gouken/lumen/internal/vm"
)

//go:embed testdata/probe.c
var source []byte

var built struct {
	once sync.Once
	path string
	why  string
}

// Library returns the path of the compiled library, or skips the test.
func Library(t testing.TB) string {
	t.Helper()
	if !vm.ForeignSupported {
		t.Skip("this build of lumen cannot call the functions of C libraries")
	}
	built.once.Do(build)
	if built.path == "" {
		t.Skip("the C library of the tests could not be compiled: " + built.why)
	}
	return built.path
}

// Installed returns the path a library is loaded by if the machine has it,
// or skips the test: for the tests that call a real library.
func Installed(t testing.TB, name string, folders ...string) string {
	t.Helper()
	if !vm.ForeignSupported {
		t.Skip("this build of lumen cannot call the functions of C libraries")
	}
	extension := ".so"
	if runtime.GOOS == "darwin" {
		extension = ".dylib"
	}
	folders = append(folders, "/opt/homebrew/lib", "/usr/local/lib", "/usr/lib",
		"/usr/lib/x86_64-linux-gnu", "/usr/lib/aarch64-linux-gnu")
	for _, folder := range folders {
		file := filepath.Join(folder, "lib"+name+extension)
		if _, err := os.Stat(file); err == nil {
			return file
		}
	}
	t.Skip("lib" + name + " is not installed")
	return ""
}

func build() {
	compiler, err := exec.LookPath("cc")
	if err != nil {
		built.why = "no C compiler (cc)"
		return
	}
	// Kept for the whole run of the tests of a package: nothing says when
	// the last of them is done with it
	folder, err := os.MkdirTemp("", "lumen-ffi-")
	if err != nil {
		built.why = err.Error()
		return
	}
	file := filepath.Join(folder, "probe.c")
	if err := os.WriteFile(file, source, 0o644); err != nil {
		built.why = err.Error()
		return
	}
	library := filepath.Join(folder, "libprobe.so")
	if runtime.GOOS == "darwin" {
		library = filepath.Join(folder, "libprobe.dylib")
	}
	if out, err := exec.Command(compiler, "-shared", "-fPIC", "-O1", "-o", library, file).CombinedOutput(); err != nil {
		built.why = err.Error() + ": " + string(out)
		return
	}
	built.path = library
}

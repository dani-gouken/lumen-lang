//go:build !((darwin || linux) && (arm64 || amd64))

package vm

import "fmt"

// ForeignSupported reports whether this build of lumen can call the
// functions of C libraries.
const ForeignSupported = false

var errNoForeign = fmt.Errorf("this build of lumen cannot call the functions of C libraries (macOS and Linux, on arm64 and amd64, can)")

func (vm *VM) foreignFunction(f *Foreign) (ExternFunc, error) { return nil, errNoForeign }

func cAlloc(bytes uintptr) (uintptr, error)    { return 0, errNoForeign }
func cFree(address uintptr) error              { return errNoForeign }
func cLength(address uintptr) (uintptr, error) { return 0, errNoForeign }

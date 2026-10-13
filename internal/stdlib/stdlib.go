// Package stdlib holds the standard library: Lumen modules compiled into
// the lumen binary, so that `import std.strings;` works in any program
// without installing anything.
//
// The modules are ordinary Lumen source, in lib/. What they cannot do in
// Lumen they declare with `extern fn`; those functions are the natives of
// the virtual machine (internal/vm/native_*.go).
package stdlib

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Package is the name the library is imported by: import std.math;
const Package = "std"

// Root is the folder the library's files appear to be in. It is not a
// folder on disk: paths under it are served from the binary by Read. It
// is what diagnostics and runtime errors show for a line of the library.
const Root = "<std>"

//go:embed lib/*.lum
var files embed.FS

// IsPath reports whether a path names a file of the library.
func IsPath(file string) bool {
	return strings.HasPrefix(file, Root+"/")
}

// Read returns the source of a file of the library, given as Root/name.lum.
func Read(file string) (string, error) {
	// The loader builds the path with the separator of the system
	name := strings.ReplaceAll(strings.TrimPrefix(file, Root+"/"), "\\", "/")
	data, err := files.ReadFile(path.Join("lib", name))
	if !IsPath(file) || strings.Contains(name, "/") {
		err = fs.ErrNotExist
	}
	if err != nil {
		return "", &fs.PathError{Op: "read", Path: file, Err: fs.ErrNotExist}
	}
	return string(data), nil
}

// Modules lists the modules of the library, sorted: "std.math", ...
func Modules() []string {
	entries, _ := files.ReadDir("lib")
	var names []string
	for _, entry := range entries {
		names = append(names, Package+"."+strings.TrimSuffix(entry.Name(), ".lum"))
	}
	sort.Strings(names)
	return names
}

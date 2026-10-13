// Package project reads lumen.json, the file that describes a Lumen
// program made of more than a single file: where it starts, where its
// modules are, which packages it uses and the limits it runs with.
//
//	{
//	    "name": "game",
//	    "entry": "src/main.lum",
//	    "root": "src",
//	    "packages": { "std": "vendor/std" },
//	    "heap_mb": 512,
//	    "max_depth": 20000
//	}
//
// Every key is optional. Paths are relative to the folder of the file.
package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the name of the project file.
const FileName = "lumen.json"

// Config is the content of a project file.
type Config struct {
	// Name of the program. Informative.
	Name string `json:"name"`

	// Entry is the file the program starts in, run when no file is given
	// on the command line.
	Entry string `json:"entry"`

	// Root is the folder module paths start at. Default: the folder of
	// the project file.
	Root string `json:"root"`

	// Packages maps the first part of a module path to the folder that
	// holds its modules: with "std": "vendor/std", `import std.list;`
	// reads vendor/std/list.lum.
	Packages map[string]string `json:"packages"`

	// Limits of the virtual machine; 0 leaves the default.
	HeapMB   int `json:"heap_mb"`
	MaxDepth int `json:"max_depth"`

	// Path is the project file, Dir its folder.
	Path string `json:"-"`
	Dir  string `json:"-"`
}

// Find looks for a project file in dir and then in each folder above it.
// It returns nil when there is none.
func Find(dir string) (*Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for {
		path := filepath.Join(abs, FileName)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return Load(display(path))
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return nil, nil
		}
		abs = parent
	}
}

// display shortens an absolute path to one relative to the current
// directory when it is inside it, so that messages show the path the way
// the user would write it.
func display(abs string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

// Load reads and checks a project file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = path
	cfg.Dir = filepath.Dir(path)
	return cfg, nil
}

// Parse reads the content of a project file.
func Parse(data []byte) (*Config, error) {
	cfg := &Config{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, describe(err, data)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected text after the closing '}'")
	}
	return cfg, cfg.check()
}

// describe turns a decoding error into a message about the file.
func describe(err error, data []byte) error {
	var syntax *json.SyntaxError
	var mismatch *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		// The offset counts the character that was not expected
		line, col := position(data, syntax.Offset-1)
		return fmt.Errorf("line %d, column %d: not valid JSON: %s", line, col, syntax.Error())
	case errors.As(err, &mismatch):
		line, _ := position(data, mismatch.Offset)
		return fmt.Errorf("line %d: %q must be %s", line, mismatch.Field, expected(mismatch.Field))
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return errors.New("the file is empty or ends too early; the smallest project file is {}")
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		key := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return fmt.Errorf("unknown key %q; the keys are %s", key, strings.Join(keys, ", "))
	}
	return err
}

var keys = []string{"name", "entry", "root", "packages", "heap_mb", "max_depth"}

func expected(field string) string {
	switch strings.SplitN(field, ".", 2)[0] {
	case "heap_mb", "max_depth":
		return "a whole number"
	case "packages":
		return `an object of names and folders, as in {"std": "vendor/std"}`
	}
	return "a string"
}

// position converts a byte offset into a line and a column, both 1-based.
func position(data []byte, offset int64) (line, col int) {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	if offset < 0 {
		offset = 0
	}
	before := data[:offset]
	line = bytes.Count(before, []byte("\n")) + 1
	col = len(before) - bytes.LastIndexByte(before, '\n')
	return line, col
}

func (c *Config) check() error {
	if c.Entry != "" && !strings.HasSuffix(c.Entry, ".lum") {
		return fmt.Errorf(`"entry" must be a .lum file, got %q`, c.Entry)
	}
	if c.HeapMB < 0 {
		return fmt.Errorf(`"heap_mb" must be positive, got %d`, c.HeapMB)
	}
	if c.MaxDepth < 0 {
		return fmt.Errorf(`"max_depth" must be positive, got %d`, c.MaxDepth)
	}
	for _, name := range c.packageNames() {
		if !isIdentifier(name) {
			return fmt.Errorf(`package name %q is not a name that can be imported: use letters, digits and '_', not starting with a digit`, name)
		}
		if c.Packages[name] == "" {
			return fmt.Errorf(`package %q has no folder`, name)
		}
	}
	return nil
}

func (c *Config) packageNames() []string {
	names := make([]string, 0, len(c.Packages))
	for name := range c.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isIdentifier(s string) bool {
	for i, r := range s {
		letter := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return s != ""
}

// resolve returns a path of the project file relative to its folder.
func (c *Config) resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(c.Dir, path)
}

// RootDir is the folder module paths start at.
func (c *Config) RootDir() string {
	return c.resolve(c.Root)
}

// EntryFile is the file the program starts in.
func (c *Config) EntryFile() (string, error) {
	if c.Entry == "" {
		return "", fmt.Errorf(`%s does not say where the program starts; add "entry": "main.lum"`, c.Path)
	}
	return c.resolve(c.Entry), nil
}

// PackageDirs returns the folder of each package.
func (c *Config) PackageDirs() map[string]string {
	dirs := make(map[string]string, len(c.Packages))
	for name, dir := range c.Packages {
		dirs[name] = c.resolve(dir)
	}
	return dirs
}

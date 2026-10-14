// Package modules turns a program written as several files into the single
// program the rest of the compiler works on.
//
// A module is one file. Its name is its path from the root of the program,
// with dots: physics/body.lum is the module physics.body. Folders only
// group modules; they are not modules themselves. The root is the folder
// of the entry file unless Options says otherwise.
//
// Loading has three steps:
//
//  1. Read and parse every file reachable through imports.
//  2. Work out, for each module, what its imports bring into scope.
//  3. Give every top-level declaration outside the entry file a name that
//     is unique in the program (new in physics.body becomes
//     "physics.body.new") and rewrite every reference to it, then put all
//     declarations in one program.
//
// After that no later stage needs to know about modules, except the type
// checker, which enforces the visibility of methods.
package modules

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/parser"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/stdlib"
	"github.com/dani-gouken/lumen/internal/token"
)

// Error codes for modules
const (
	ErrModuleNotFound = "E8001" // No file for an imported module
	ErrUnknownName    = "E8002" // Module does not declare the name asked for
	ErrPrivate        = "E8003" // Name is not exported with 'pub'
	ErrNameConflict   = "E8004" // Import clashes with another name in scope
	ErrNotAValue      = "E8005" // Module or type used where a value is expected, or the reverse
	ErrInvalidImport  = "E8006" // Import that cannot work: the entry file, a re-exported module
)

// Extension is the suffix of Lumen source files.
const Extension = ".lum"

// ReadFile returns the text of a source file.
type ReadFile func(path string) (string, error)

// Options says where the modules of a program are.
type Options struct {
	// Read returns the text of an imported file.
	Read ReadFile

	// Root is the folder module paths start at. Empty: the folder of the
	// entry file.
	Root string

	// Packages maps the first part of a module path to a folder outside
	// the root: with "std" in "vendor/std", the module std.list is the
	// file vendor/std/list.lum and the module std is vendor/std.lum.
	Packages map[string]string

	// EveryMethod brings the modules that give the builtin types their
	// methods (MethodModules) into the program whether it calls one or
	// not: for an editor, which suggests them after a dot before any is
	// written.
	EveryMethod bool
}

// methodModule is a module that gives the builtin types methods, as far
// as it is known before it is part of a program.
type methodModule struct {
	src   string
	names map[string]bool
}

// item is a top-level declaration of a module.
type item struct {
	name      string // As written
	qualified string // Unique in the program
	kind      string // "function", "struct", "trait", "enum"
	isType    bool
	pub       bool
	module    *module
}

// reexport is a name a module passes on with `pub import other.{name}`.
type reexport struct {
	from *module
	tok  token.Token
}

type module struct {
	name string // Dotted path; "" for the entry file
	file string
	prog *ast.Program

	values    map[string]*item
	types     map[string]*item
	reexports map[string]reexport

	// What the imports bring into scope
	aliases        map[string]*module
	importedValues map[string]*item
	importedTypes  map[string]*item
}

// display names a module in messages.
func (m *module) display() string {
	if m.name == "" {
		return "the program's entry file"
	}
	return "module '" + m.name + "'"
}

type loader struct {
	errs        *errors.Handler
	read        ReadFile
	rootDir     string
	rootSet     bool // The root was given, not taken from the entry file
	packages    map[string]string
	everyMethod bool // See Options.EveryMethod
	// The method modules the program's library does not have
	noMethods map[string]bool
	// Those it has and that are not part of the program yet: their text,
	// and the names of the methods they declare
	methods map[string]*methodModule

	root    *module
	modules map[string]*module // By dotted path; nil for one that failed to load
	order   []*module
}

// Load parses the entry file and every module it imports, directly or not,
// and returns them as one program.
// When an error was reported the returned program is not fit for the next
// stages.
func Load(src, file string, opts Options, errs *errors.Handler) *ast.Program {
	l := &loader{
		errs:        errs,
		read:        opts.Read,
		rootDir:     opts.Root,
		rootSet:     opts.Root != "",
		packages:    opts.Packages,
		everyMethod: opts.EveryMethod,
		modules:     make(map[string]*module),
	}
	if !l.rootSet {
		l.rootDir = filepath.Dir(file)
	}

	l.root = l.parse("", file, src)
	for i := 0; i < len(l.order); i++ {
		l.loadImports(l.order[i])
	}
	if errs.HasErrors() {
		return l.root.prog
	}

	for _, m := range l.order {
		l.collectReexports(m)
	}
	for _, m := range l.order {
		l.bindImports(m)
	}
	l.checkModuleNames()

	// Rewriting goes ahead even when an import was wrong, so that misuses
	// of the modules that did load are reported in the same run
	merged := &ast.Program{BaseNode: l.root.prog.BaseNode}
	for _, m := range l.order {
		(&rewriter{loader: l, m: m}).rewrite()
		for _, decl := range m.prog.Decls {
			// A function the runtime provides has nothing to compile; it is
			// kept apart, for its name and signature
			if fn, ok := decl.(*ast.FnDecl); ok && fn.Extern {
				merged.Externs = append(merged.Externs, fn)
				continue
			}
			merged.Decls = append(merged.Decls, decl)
		}
	}
	return merged
}

// parse parses one file and records its declarations.
func (l *loader) parse(name, file, src string) *module {
	l.errs.AddSource(file, parser.SourceLines(src))

	m := &module{
		name:           name,
		file:           file,
		prog:           parser.Parse(src, file, l.errs),
		values:         make(map[string]*item),
		types:          make(map[string]*item),
		reexports:      make(map[string]reexport),
		aliases:        make(map[string]*module),
		importedValues: make(map[string]*item),
		importedTypes:  make(map[string]*item),
	}
	l.order = append(l.order, m)

	declare := func(table map[string]*item, declName, kind string, pub bool) {
		if _, dup := table[declName]; dup {
			return // Reported by name resolution
		}
		table[declName] = &item{
			name:      declName,
			qualified: m.qualify(declName),
			kind:      kind,
			isType:    kind != "function" && kind != "constant",
			pub:       pub,
			module:    m,
		}
	}
	for _, decl := range m.prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			declare(m.values, d.Name, "function", d.Pub)
		case *ast.ConstDecl:
			declare(m.values, d.Name, "constant", d.Pub)
		case *ast.StructDecl:
			declare(m.types, d.Name, "struct", d.Pub)
		case *ast.TraitDecl:
			declare(m.types, d.Name, "trait", d.Pub)
		case *ast.EnumDecl:
			declare(m.types, d.Name, "enum", d.Pub)
		}
	}
	l.importsResult(m, src)
	return m
}

// qualify returns the program-wide name of a declaration of the module.
// Names of the entry file are kept as written.
func (m *module) qualify(name string) string {
	if m.name == "" {
		return name
	}
	return m.name + "." + name
}

// path returns the file of a module given by its dotted path. A path
// whose first part names a package is looked up in the package's folder.
func (l *loader) path(dotted string) string {
	base, rest := l.rootDir, dotted
	if first, after, _ := strings.Cut(dotted, "."); l.packages[first] != "" {
		if after == "" {
			return l.packages[first] + Extension
		}
		base, rest = l.packages[first], after
	}
	parts := append([]string{base}, strings.Split(rest, ".")...)
	return filepath.Join(parts...) + Extension
}

// sameFile reports whether two paths name one file.
func sameFile(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return absA == absB
}

// loadImports reads the modules a module imports that are not loaded yet.
// RangeModule is the module of the standard library that has Range, the
// struct a range kept as a value is: let r = 0..10;
const (
	RangeModule = "std.ops"
	RangeStruct = RangeModule + ".Range"
)

// MethodModules are the modules of the standard library that give the
// builtin types their methods. Every program has them.
var MethodModules = []string{"std.arrays", "std.strings", "std.math"}

// The names of the standard library that every file can say without
// importing them: fn load() -> Result<Config, String>, and the Template
// a function of commands or queries takes. A file that says one of them,
// and neither declares one nor imports one, is read as if it began with
// `import std.result.{Result};` or `import std.ops.{Template};`.
const (
	ResultModule   = "std.result"
	ResultName     = "Result"
	TemplateModule = "std.ops"
	TemplateName   = "Template"
)

var (
	saysResult   = regexp.MustCompile(`\bResult\b`)
	saysTemplate = regexp.MustCompile(`\bTemplate\b`)
)

// importsResult gives a file the imports of Result and Template it did not
// write, when it names them and has none of its own.
func (l *loader) importsResult(m *module, src string) {
	l.importsImplicitly(m, src, ResultModule, ResultName, saysResult)
	l.importsImplicitly(m, src, TemplateModule, TemplateName, saysTemplate)
}

func (l *loader) importsImplicitly(m *module, src, module, name string, says *regexp.Regexp) {
	first, _, _ := strings.Cut(module, ".")
	if l.packages[first] == "" || m.name == module || m.types[name] != nil ||
		!strings.Contains(src, name) || !says.MatchString(src) {
		return
	}
	for _, imp := range m.prog.Imports {
		for _, imported := range imp.Names {
			if imported.Name == name {
				return
			}
		}
		if imp.Alias == name {
			return
		}
	}
	at := m.prog.BaseNode
	m.prog.Imports = append(m.prog.Imports, &ast.ImportDecl{
		BaseNode: at,
		Path:     module,
		Names:    []*ast.ImportName{{BaseNode: at, Name: name}},
		Implicit: true,
	})
}

// methodNames finds the methods a module of the library declares: the
// functions written inside its impls, which are the indented ones. The
// text is gone through line by line: this is done for every program, on a
// few thousand lines, before anything of the module is known to be needed.
func methodNames(src string) map[string]bool {
	found := make(map[string]bool)
	for len(src) > 0 {
		line := src
		if end := strings.IndexByte(src, '\n'); end >= 0 {
			line, src = src[:end], src[end+1:]
		} else {
			src = ""
		}
		if len(line) == 0 || (line[0] != ' ' && line[0] != '\t') {
			continue
		}
		line = strings.TrimLeft(line, " \t")
		line = strings.TrimPrefix(line, "pub ")
		if !strings.HasPrefix(line, "fn ") {
			continue
		}
		name := line[len("fn "):]
		if end := strings.IndexAny(name, "(<"); end > 0 {
			found[name[:end]] = true
		}
	}
	return found
}

// callsMethod reports whether a file writes, after a dot, one of some
// names of methods.
func callsMethod(prog *ast.Program, methods map[string]bool) bool {
	if prog == nil {
		return false
	}
	// The shorter of the two is gone through
	if len(prog.Members) < len(methods) {
		for name := range prog.Members {
			if methods[name] {
				return true
			}
		}
		return false
	}
	for name := range methods {
		if prog.Members[name] {
			return true
		}
	}
	return false
}

// LibraryMethods lists, sorted, the methods that a module of MethodModules
// declares in the standard library of the lumen binary. A program that
// calls none of them does not have the module, and a method it misspells
// is compared with these.
func LibraryMethods(module string) []string {
	src, err := stdlib.Read(stdlib.Root + "/" + strings.TrimPrefix(module, stdlib.Package+".") + ".lum")
	if err != nil {
		return nil
	}
	var names []string
	for name := range methodNames(src) {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (l *loader) loadImports(m *module) {
	// A range kept as a value is a Range of std.ops, which the file need
	// not have imported. Without the library there is none, and the
	// range stays what it was: not a value, as the checker will say.
	if _, seen := l.modules[RangeModule]; !seen && m.prog.KeepsRanges && m.name != RangeModule {
		file := l.path(RangeModule)
		if src, err := l.read(file); err == nil {
			l.modules[RangeModule] = l.parse(RangeModule, file, src)
		}
	}
	// The modules that give arrays, texts and numbers their methods are
	// part of every program that calls one: items.len() and name.trim()
	// work without an import. Their names are not: arrays.range(0, 3)
	// still wants `import std.arrays;`. A module is only read into the
	// program when a name written after a dot is one of its methods, so
	// that a program that calls none starts as fast as it did. A program
	// that brings a library of its own without them has no such methods.
	for _, name := range MethodModules {
		// Only where the library is a package of the program: a folder
		// called std next to it is not the library
		first, _, _ := strings.Cut(name, ".")
		if _, seen := l.modules[name]; seen || m.name == name || l.packages[first] == "" || l.noMethods[name] {
			continue
		}
		if !l.everyMethod && len(m.prog.Members) == 0 {
			continue
		}
		// Read once for the program, however many of its files are asked
		file := l.path(name)
		known, read := l.methods[name]
		if !read {
			src, err := l.read(file)
			if err != nil {
				// A library of the program's own, without this module
				if l.noMethods == nil {
					l.noMethods = make(map[string]bool)
				}
				l.noMethods[name] = true
				continue
			}
			known = &methodModule{src: src, names: methodNames(src)}
			if l.methods == nil {
				l.methods = make(map[string]*methodModule)
			}
			l.methods[name] = known
		}
		if l.everyMethod || callsMethod(m.prog, known.names) {
			l.modules[name] = l.parse(name, file, known.src)
		}
	}
	for _, imp := range m.prog.Imports {
		if _, seen := l.modules[imp.Path]; seen {
			continue
		}

		file := l.path(imp.Path)
		if sameFile(file, l.root.file) {
			l.modules[imp.Path] = nil
			l.report(imp.Token(), ErrInvalidImport,
				"Move what other modules need into a module of its own",
				"cannot import '%s': it is the program's entry file", imp.Path)
			continue
		}

		src, err := l.read(file)
		if err != nil {
			l.modules[imp.Path] = nil
			l.report(imp.Token(), ErrModuleNotFound, l.notFoundSuggestion(imp, file),
				"module '%s' not found", imp.Path)
			continue
		}
		l.modules[imp.Path] = l.parse(imp.Path, file, src)
	}
}

// notFoundSuggestion explains where a module was looked for. Writing
// `import shapes.Circle;` for a name of the module shapes is an easy
// mistake, so that case gets the import it should have been.
func (l *loader) notFoundSuggestion(imp *ast.ImportDecl, file string) string {
	if dot := strings.LastIndex(imp.Path, "."); dot >= 0 && len(imp.Names) == 0 {
		parent, name := imp.Path[:dot], imp.Path[dot+1:]
		if src, err := l.read(l.path(parent)); err == nil && declares(src, name) {
			return fmt.Sprintf("There is no file %s. To import '%s' from the module '%s', write: import %s.{%s};",
				file, name, parent, parent, name)
		}
	}
	first, _, _ := strings.Cut(imp.Path, ".")
	switch {
	case l.packages[first] != "":
		return fmt.Sprintf("Expected the file %s: '%s' is a package in the folder %s", file, first, l.packages[first])
	case l.rootSet:
		return fmt.Sprintf("Expected the file %s. Module paths start at the root of the project, %s", file, l.rootDir)
	}
	return fmt.Sprintf("Expected the file %s. Module paths start at the folder of the program's entry file", file)
}

// declares reports whether source text has a top-level declaration, or a
// re-export, of the given name.
func declares(src, name string) bool {
	prog := parser.Parse(src, "", errors.NewHandler())
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			if d.Name == name {
				return true
			}
		case *ast.ConstDecl:
			if d.Name == name {
				return true
			}
		case *ast.StructDecl:
			if d.Name == name {
				return true
			}
		case *ast.EnumDecl:
			if d.Name == name {
				return true
			}
		case *ast.TraitDecl:
			if d.Name == name {
				return true
			}
		}
	}
	for _, imp := range prog.Imports {
		for _, n := range imp.Names {
			if imp.Pub && n.Name == name {
				return true
			}
		}
	}
	return false
}

// collectReexports records the names a module passes on. It runs for every
// module before any import is bound, so a chain of re-exports can be
// followed whatever the order of the files.
func (l *loader) collectReexports(m *module) {
	for _, imp := range m.prog.Imports {
		target := l.modules[imp.Path]
		if !imp.Pub || target == nil {
			continue
		}
		if len(imp.Names) == 0 {
			l.report(imp.Token(), ErrInvalidImport,
				fmt.Sprintf("List the names to pass on: pub import %s.{Name};", imp.Path),
				"a module cannot be exported again as a whole")
			continue
		}
		for _, name := range imp.Names {
			if _, own := m.lookupOwn(name.Name); !own {
				m.reexports[name.Name] = reexport{from: target, tok: name.Token()}
			}
		}
	}
}

func (m *module) lookupOwn(name string) (*item, bool) {
	if it, ok := m.values[name]; ok {
		return it, true
	}
	it, ok := m.types[name]
	return it, ok
}

// lookup finds a name a module offers: a declaration of its own, or one it
// passes on from another module. visible reports whether other modules may
// use it.
func (m *module) lookup(name string, visiting map[*module]bool) (it *item, visible bool) {
	if it, ok := m.lookupOwn(name); ok {
		return it, it.pub
	}
	if re, ok := m.reexports[name]; ok && !visiting[m] {
		visiting[m] = true
		if it, _ := re.from.lookup(name, visiting); it != nil {
			return it, true
		}
	}
	return nil, false
}

// find looks a name up in another module on behalf of m and reports what
// is wrong when m cannot use it.
func (l *loader) find(m, target *module, name string, tok token.Token) *item {
	it, visible := target.lookup(name, make(map[*module]bool))
	if it == nil {
		suggestion := ""
		if similar := resolve.ClosestName(name, target.exported()); similar != "" {
			suggestion = "Did you mean '" + similar + "'?"
		}
		l.report(tok, ErrUnknownName, suggestion, "%s has no '%s'", target.display(), name)
		return nil
	}
	if !visible && target != m {
		l.report(tok, ErrPrivate,
			fmt.Sprintf("Declare it with 'pub' in %s to use it from other modules", target.file),
			"%s '%s' of %s is private", it.kind, name, target.display())
		return nil
	}
	return it
}

// exported lists the names other modules may use, sorted.
func (m *module) exported() []string {
	var names []string
	for name, it := range m.values {
		if it.pub {
			names = append(names, name)
		}
	}
	for name, it := range m.types {
		if it.pub {
			names = append(names, name)
		}
	}
	for name := range m.reexports {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// bindImports works out what each import of a module brings into scope.
func (l *loader) bindImports(m *module) {
	for _, imp := range m.prog.Imports {
		target := l.modules[imp.Path]
		if target == nil {
			continue
		}

		if len(imp.Names) == 0 {
			if imp.Pub {
				continue // Reported with the re-exports
			}
			l.bindAlias(m, imp, target)
			continue
		}
		for _, name := range imp.Names {
			l.bindName(m, target, name)
		}
	}
}

func (l *loader) bindAlias(m *module, imp *ast.ImportDecl, target *module) {
	alias := imp.Alias
	if alias == "" {
		alias = imp.Path[strings.LastIndex(imp.Path, ".")+1:]
	}
	rename := fmt.Sprintf("Give the module another name: import %s as other;", imp.Path)

	if existing, ok := m.aliases[alias]; ok {
		if existing != target {
			l.report(imp.Token(), ErrNameConflict, rename,
				"'%s' already names the module '%s'", alias, existing.name)
		}
		return
	}
	if it, ok := m.lookupOwn(alias); ok {
		l.report(imp.Token(), ErrNameConflict, rename,
			"'%s' is both an imported module and a %s of this file", alias, it.kind)
		return
	}
	if it := m.imported(alias); it != nil {
		l.report(imp.Token(), ErrNameConflict, rename,
			"'%s' is both an imported module and a %s imported from '%s'", alias, it.kind, it.module.name)
		return
	}
	m.aliases[alias] = target
}

func (m *module) imported(name string) *item {
	if it, ok := m.importedValues[name]; ok {
		return it
	}
	return m.importedTypes[name]
}

func (l *loader) bindName(m, target *module, name *ast.ImportName) {
	it := l.find(m, target, name.Name, name.Token())
	if it == nil {
		return
	}
	useModule := fmt.Sprintf("Import the module and write %s.%s instead", target.name[strings.LastIndex(target.name, ".")+1:], name.Name)

	if own, ok := m.lookupOwn(name.Name); ok {
		if own != it {
			l.report(name.Token(), ErrNameConflict, useModule,
				"'%s' is already a %s of this file", name.Name, own.kind)
		}
		return
	}
	if existing := m.imported(name.Name); existing != nil {
		if existing != it {
			l.report(name.Token(), ErrNameConflict, useModule,
				"'%s' is already imported from '%s'", name.Name, existing.module.name)
		}
		return
	}
	if existing, ok := m.aliases[name.Name]; ok {
		l.report(name.Token(), ErrNameConflict, useModule,
			"'%s' already names the module '%s'", name.Name, existing.name)
		return
	}

	if it.isType {
		m.importedTypes[name.Name] = it
	} else {
		m.importedValues[name.Name] = it
	}
}

// checkModuleNames rejects a struct whose program-wide name is also the
// name of a module: the method m of struct a.B and the function m of module
// a.B would both be "a.B.m".
func (l *loader) checkModuleNames() {
	for _, m := range l.order {
		for _, decl := range m.prog.Decls {
			d, ok := decl.(*ast.StructDecl)
			if !ok {
				continue
			}
			if other := l.modules[m.qualify(d.Name)]; other != nil && m.name != "" {
				l.report(d.Token(), ErrNameConflict, "Rename the struct or the file "+other.file,
					"struct '%s' of module '%s' has the same name as the module '%s'", d.Name, m.name, other.name)
			}
		}
	}
}

func (l *loader) report(tok token.Token, code, suggestion, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if suggestion != "" {
		l.errs.ReportErrorWithSuggestion(code, msg, suggestion, tok, nil)
		return
	}
	l.errs.ReportErrorFromToken(code, msg, tok, nil)
}

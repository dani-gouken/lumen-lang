// Package repl is Lumen typed in a line at a time: `lumen repl`.
//
// Lumen compiles whole programs, and the REPL does not change that. Each
// input becomes a program of its own, made of everything declared so far
// and a main that runs what was typed, and the programs run one after the
// other on the same virtual machine:
//
//	> let count = 0;
//	> fn bump() -> Int { return 1; }
//	> count += bump();
//
// The third input is compiled as
//
//	fn bump() -> Int { return 1; }
//	fn main() {
//	    let count: Int = __repl_get_count();    // the variable of before
//	    __repl_link("count", () -> count);
//	    {
//	        count += bump();
//	    }
//	}
//
// A variable is given back to each program with its value, and as the
// very same variable: see internal/vm/repl.go. Declarations are compiled
// again each time, which is what makes one that is typed again replace
// the one before.
package repl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/parser"
	"github.com/dani-gouken/lumen/internal/token"
	"github.com/dani-gouken/lumen/internal/types"
	"github.com/dani-gouken/lumen/internal/vm"
)

// Options says what a session runs with. The zero value is a session in
// the current folder, writing to the output and the error output.
type Options struct {
	// Dir is the folder import paths start at. Default: the current one.
	Dir string

	// Packages maps the first part of a module path to a folder, as the
	// "packages" of a project file do.
	Packages map[string]string

	// Out receives what programs print and the values of expressions,
	// Err the diagnostics. Default: os.Stdout and os.Stderr.
	Out io.Writer
	Err io.Writer

	// In is what programs read (std.io.readLine). Default: os.Stdin.
	In io.Reader

	// Machine makes the virtual machine. Default: vm.NewVM.
	Machine func() *vm.VM
}

// Session is one conversation: what was declared, and the variables.
type Session struct {
	opts    Options
	file    string // The name every program of the session is compiled under
	machine *vm.VM

	state
}

// state is the part of a session an input changes, and that is put back
// as it was when the input turns out not to compile.
type state struct {
	entries []*entry
	vars    []*variable
}

// entry is one input. Its text stays where it was in the programs that
// follow, so that a line keeps its number for as long as the session
// lasts: an error in a function says which input it was typed in. What
// is no longer part of the program, a statement that has run or a
// declaration that was replaced, is blanked out.
type entry struct {
	text  string
	lines int
	items []*item
}

// item is one declaration or import of an entry that is still part of
// the program.
type item struct {
	key      string // What a later declaration of the same thing replaces
	name     string
	kind     string
	from, to int // Where it is in the entry's text
	head     string
}

// variable is a variable programs are given back.
type variable struct {
	name string
	typ  *ast.Type
}

// Outcome says how an input went.
type Outcome struct {
	// Exit is set when the program asked to end (std.os.exit): the REPL
	// ends with it.
	Exit *int

	// Failed reports that the input did not compile, or failed when run.
	Failed bool
}

// New starts a session.
func New(opts Options) *Session {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Err == nil {
		opts.Err = os.Stderr
	}
	if opts.Dir == "" {
		opts.Dir = "."
	}
	if opts.Machine == nil {
		opts.Machine = vm.NewVM
	}
	s := &Session{opts: opts, file: filepath.Join(opts.Dir, sessionFile)}
	s.start()
	return s
}

// sessionFile is the name of the file the programs of a session are
// compiled as. There is no such file; positions in it are shown by the
// number of the input instead (see place).
const sessionFile = "repl.lum"

func (s *Session) start() {
	s.machine = s.opts.Machine()
	s.machine.KeepVariables()
	s.machine.SetOutput(s.opts.Out)
	if s.opts.In != nil {
		s.machine.SetInput(s.opts.In)
	}
	s.machine.SetProgram(s.file)
	s.state = state{}
}

// Machine is the virtual machine of the session, for whoever has to stop
// a program that runs (VM.Interrupt).
func (s *Session) Machine() *vm.VM { return s.machine }

// Reset forgets everything: declarations, variables, and what the
// programs left in memory.
func (s *Session) Reset() {
	s.start()
}

// Number is the number the next input will have, from 1.
func (s *Session) Number() int { return len(s.entries) + 1 }

// --- The program an input is compiled as ---

// layout says where the pieces of a generated program are, to tell what a
// position in it means to whoever typed the pieces.
type layout struct {
	starts []int // The line before the first line of each entry

	// The line before the input being taken: what is found questionable
	// above it was said when it was typed
	fresh int

	// What was put in front of the last statement of the current input
	// to show its value: on which line, from which column, how long
	shownLine, shownCol, shownLen int
	original                      string // That line as it was typed
}

// header is the first line of every program: the functions the virtual
// machine provides to a session, and the modules the types of the
// variables come from.
func (st *state) header() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "extern fn %s(name: String, read: fn() -> Any); ", vm.KeepFunction)
	fmt.Fprintf(&sb, "extern fn %s(name: String, read: fn() -> Any); ", vm.LinkFunction)
	fmt.Fprintf(&sb, "extern fn %s(value: Any); ", vm.ShowFunction)

	modules := map[string]bool{}
	for _, v := range st.vars {
		fmt.Fprintf(&sb, "extern fn %s%s() -> %s; ", vm.GetPrefix, v.name, typeSource(v.typ, modules))
	}
	names := make([]string, 0, len(modules))
	for module := range modules {
		names = append(names, module)
	}
	sort.Strings(names)
	for _, module := range names {
		fmt.Fprintf(&sb, "import %s as %s; ", module, moduleAlias(module))
	}
	return sb.String()
}

// restores is the start of main: each variable declared again, with the
// value it has and as the variable it was.
func (st *state) restores() string {
	var sb strings.Builder
	for _, v := range st.vars {
		fmt.Fprintf(&sb, "let %s: %s = %s%s(); %s(%q, () -> %s); ",
			v.name, typeSource(v.typ, map[string]bool{}), vm.GetPrefix, v.name, vm.LinkFunction, v.name, v.name)
	}
	return sb.String()
}

// program writes the program for a state: its entries, then main. The
// statements of the input being run, if any, go in a block of their own
// inside main, where they may declare a variable of an earlier name
// again; after is what follows them there.
func (st *state) program(statements, after string) (string, *layout) {
	var sb strings.Builder
	lay := &layout{}
	sb.WriteString(st.header())
	sb.WriteString("\n")
	line := 2
	for _, e := range st.entries {
		lay.starts = append(lay.starts, line)
		sb.WriteString("\n")
		sb.WriteString(e.text)
		sb.WriteString("\n")
		line += e.lines + 1
	}
	lay.starts = append(lay.starts, line)
	lay.fresh = line
	sb.WriteString("fn main() { ")
	sb.WriteString(st.restores())
	sb.WriteString("{\n")
	sb.WriteString(statements)
	sb.WriteString("\n")
	sb.WriteString(after)
	sb.WriteString("} }\n")
	return sb.String(), lay
}

func (s *Session) compilerOptions() compiler.Options {
	return compiler.Options{Root: s.opts.Dir, Packages: s.opts.Packages, Hosted: vm.IsSessionFunction}
}

// --- Positions ---

// place turns a position in a generated program into what it means in
// the session: the number of the input, and the line in it.
func (s *Session) place(lay *layout, pos token.Position) (token.Position, bool) {
	if pos.File != s.file || lay == nil {
		return pos, false
	}
	for i := len(lay.starts) - 1; i >= 0; i-- {
		if pos.Line > lay.starts[i] {
			current := i == len(lay.starts)-1
			pos.File = fmt.Sprintf("[%d]", i+1)
			pos.Line -= lay.starts[i]
			return pos, current && lay.shownLen > 0 && pos.Line == lay.shownLine
		}
	}
	pos.File = "[repl]"
	return pos, false
}

// shift takes what was put in front of a shown expression out of a
// column of its line.
func (lay *layout) shift(col int) int {
	if col >= lay.shownCol+lay.shownLen {
		return col - lay.shownLen
	}
	if col > lay.shownCol {
		return lay.shownCol
	}
	return col
}

// entryPosition is a position of a generated program as one of the
// current input: the line and column in what was typed.
func (s *Session) entryPosition(lay *layout, pos token.Position) token.Position {
	pos.Line -= lay.starts[len(lay.starts)-1]
	return pos
}

// relabel rewrites the positions a compiled program carries, which are
// what a failure is reported with when it runs: by input, as the
// positions of what does not compile are. It is done once, here, because
// a function may fail long after the program it was compiled in: called
// from a later input, through a closure that was kept.
func (s *Session) relabel(chunk *vm.Chunk, lay *layout) {
	done := map[*vm.DebugInfo]bool{}
	for _, info := range chunk.DebugInfo {
		if info == nil || done[info] {
			continue
		}
		done[info] = true
		pos, shown := s.place(lay, token.Position{File: info.FilePath, Line: info.Line, Col: info.Column})
		if shown {
			pos.Col = lay.shift(pos.Col)
			info.SourceLine = lay.original
		}
		info.FilePath, info.Line, info.Column = pos.File, pos.Line, pos.Col
	}
}

// leftmost is where the text of an expression starts, as far as its
// parts say: the position of an expression is that of its operator.
func leftmost(expr ast.Expr) token.Position {
	first := expr.Position()
	types.WalkExprs(&ast.ExprStmt{Expr: expr}, func(part ast.Expr) {
		pos := part.Position()
		if pos.Line == 0 {
			return
		}
		if pos.Line < first.Line || (pos.Line == first.Line && pos.Col < first.Col) {
			first = pos
		}
	})
	return first
}

// statementStart is where the statement around a position of a text
// starts: what stands before a part of an expression, an opening
// parenthesis say, is not a part itself.
func statementStart(text string, within token.Position) token.Position {
	start := token.Position{Line: 1, Col: 1}
	toks := parser.NewLexer(text, "").Tokenize()
	for i, tok := range toks {
		if tok.Pos.Line > within.Line || (tok.Pos.Line == within.Line && tok.Pos.Col >= within.Col) {
			break
		}
		switch tok.Type {
		case token.SEMICOLON, token.LBRACE, token.RBRACE:
			if i+1 < len(toks) {
				start = toks[i+1].Pos
			}
		}
	}
	return token.Position{Line: start.Line, Col: start.Col}
}

func (s *Session) report(lay *layout, d errors.Diagnostic) {
	pos, shown := s.place(lay, d.Pos)
	if d.Severity != errors.SeverityError && d.Pos.File == s.file && lay != nil && d.Pos.Line <= lay.fresh {
		// A warning about an earlier input was given when it was typed
		return
	}
	d.Pos = pos
	if shown {
		d.Pos.Col = lay.shift(d.Pos.Col)
		d.Context = lay.original
		for i := range d.Highlights {
			d.Highlights[i].StartCol = lay.shift(d.Highlights[i].StartCol)
			d.Highlights[i].EndCol = lay.shift(d.Highlights[i].EndCol)
		}
	}
	fmt.Fprint(s.opts.Err, d.String())
}

func (s *Session) reportAll(lay *layout, errs *errors.Handler) {
	for _, d := range errs.Diagnostics {
		s.report(lay, d)
	}
}

func (s *Session) note(format string, args ...interface{}) {
	fmt.Fprintf(s.opts.Err, format+"\n", args...)
}

// --- Inputs ---

// Eval takes one input: declarations, or statements to run.
func (s *Session) Eval(input string) Outcome {
	text := strings.TrimRight(input, " \t\r\n")
	if strings.TrimSpace(text) == "" {
		return Outcome{}
	}
	if declares(text) {
		return s.declare(text)
	}
	return s.run(text)
}

// declares reports whether an input is declarations rather than
// statements, by how it starts.
func declares(text string) bool {
	toks := parser.NewLexer(text, "").Tokenize()
	if len(toks) < 2 {
		return false
	}
	switch toks[0].Type {
	case token.IMPORT, token.STRUCT, token.ENUM, token.TRAIT, token.IMPL, token.PUB, token.EXTERN:
		return true
	case token.FN:
		// `fn name(` declares; `fn(` is a function written as a value
		return toks[1].Type == token.IDENT
	case token.CONST:
		// A constant of the program, not of one input: it is there for
		// the functions as well
		return true
	}
	return false
}

// syntaxOnly reports whether what was found wrong is the way the input is
// written, which a missing semicolon at its end can explain.
func syntaxOnly(errs *errors.Handler) bool {
	found := false
	for _, d := range errs.Diagnostics {
		if d.Severity != errors.SeverityError {
			continue
		}
		if !strings.HasPrefix(d.Code, "E0") {
			return false
		}
		found = true
	}
	return found
}

// probe is what the checker found in the statements of an input.
type probe struct {
	text     string // The statements, with the semicolon they may have lacked
	lay      *layout
	errs     *errors.Handler
	analysis *compiler.Analysis
	block    *ast.BlockStmt // The statements, in main
}

// check type-checks statements as the next input.
func (s *Session) check(text string) *probe {
	try := func(text string) *probe {
		src, lay := s.program(text, "")
		errs := errors.NewHandler()
		analysis := compiler.Analyze(src, s.file, s.compilerOptions(), errs)
		return &probe{text: text, lay: lay, errs: errs, analysis: analysis}
	}
	p := try(text)
	if syntaxOnly(p.errs) && !strings.HasSuffix(text, ";") {
		// `1 + 2`, `let x = 3`: the semicolon is not what one is here to
		// type. On a line of its own, after a comment that may end the input.
		if with := try(text + "\n;"); !syntaxOnly(with.errs) {
			p = with
		}
	}
	if p.errs.HasErrors() || p.analysis.Program == nil {
		return p
	}
	for _, decl := range p.analysis.Program.Decls {
		fn, ok := decl.(*ast.FnDecl)
		if !ok || fn.Name != "main" || fn.Body == nil || len(fn.Body.Stmts) == 0 {
			continue
		}
		p.block, _ = fn.Body.Stmts[len(fn.Body.Stmts)-1].(*ast.BlockStmt)
	}
	return p
}

// declared lists the variables the statements declare, with their types.
// One whose type cannot be written is left out, and named in unkept.
func (p *probe) declared() (vars []*variable, unkept []string) {
	if p.block == nil {
		return nil, nil
	}
	at := map[string]int{}
	for _, stmt := range p.block.Stmts {
		let, ok := stmt.(*ast.LetStmt)
		if !ok || strings.HasPrefix(let.Name, "__") || let.Name == "_" {
			continue
		}
		typ := let.Type
		if typ == nil && p.analysis.Info != nil {
			typ = types.TypeToAST(p.analysis.Info.Types[let.Value], let.BaseNode)
		}
		if typ == nil || !writable(typ) {
			unkept = append(unkept, let.Name)
			continue
		}
		v := &variable{name: let.Name, typ: typ}
		if i, again := at[let.Name]; again {
			vars[i] = v
			continue
		}
		at[let.Name] = len(vars)
		vars = append(vars, v)
	}
	return vars, unkept
}

// shown returns the last statement if it is an expression with a value
// to show, and the type of that value.
func (p *probe) shown() (ast.Expr, types.Type) {
	if p.block == nil || len(p.block.Stmts) == 0 || p.analysis.Info == nil {
		return nil, nil
	}
	stmt, ok := p.block.Stmts[len(p.block.Stmts)-1].(*ast.ExprStmt)
	if !ok {
		return nil, nil
	}
	if binary, ok := stmt.Expr.(*ast.BinaryExpr); ok && ast.IsAssignment(binary.Op) {
		return nil, nil
	}
	t := p.analysis.Info.Types[stmt.Expr]
	if t == nil {
		return nil, nil
	}
	if prim, ok := t.(*types.PrimitiveType); ok {
		switch prim.Name {
		case types.PrimitiveVoid, types.PrimitiveUnknown, types.PrimitiveNull:
			return nil, nil
		}
	}
	return stmt.Expr, t
}

// shownName is the variable the value of an expression is put in, to be
// shown once the statements have run.
const shownName = "__repl_value"

// run compiles statements as the next input, and runs them.
func (s *Session) run(text string) Outcome {
	p := s.check(text)
	if p.errs.HasErrors() {
		s.reportAll(p.lay, p.errs)
		return Outcome{Failed: true}
	}
	vars, unkept := p.declared()

	// The program to run: the statements, the value of the last one put
	// aside if it has one, and the new variables handed over to be kept
	statements, after := p.text, ""
	var shownAt token.Position
	expr, _ := p.shown()
	if expr != nil {
		shownAt = statementStart(p.text, s.entryPosition(p.lay, leftmost(expr)))
		if offset, ok := offsetOf(p.text, shownAt.Line, shownAt.Col); ok {
			prefix := "let " + shownName + " = "
			statements = p.text[:offset] + prefix + p.text[offset:]
			after += fmt.Sprintf("%s(%s); ", vm.ShowFunction, shownName)
		} else {
			expr = nil
		}
	}
	for _, v := range vars {
		after += fmt.Sprintf("%s(%q, () -> %s); ", vm.KeepFunction, v.name, v.name)
	}

	src, lay := s.program(statements, after)
	if expr != nil {
		lay.shownLine, lay.shownCol, lay.shownLen = shownAt.Line, shownAt.Col, len("let "+shownName+" = ")
		lay.original = strings.Split(p.text, "\n")[shownAt.Line-1]
	}
	errs := errors.NewHandler()
	result := compiler.CompileOptions(src, s.file, s.compilerOptions(), errs)
	if errs.HasErrors() && expr != nil {
		// The statements compiled as they were typed: it is putting the
		// value aside that did not. They run without it.
		after = strings.TrimPrefix(after, fmt.Sprintf("%s(%s); ", vm.ShowFunction, shownName))
		src, lay = s.program(p.text, after)
		errs = errors.NewHandler()
		result = compiler.CompileOptions(src, s.file, s.compilerOptions(), errs)
	}
	s.reportAll(lay, errs)
	if errs.HasErrors() || result.Chunk == nil {
		return Outcome{Failed: true}
	}
	s.relabel(result.Chunk, lay)

	// The input takes its place among the entries whether it fails or
	// not: closures it made may live on, and say where they are from
	s.entries = append(s.entries, blankEntry(p.text))

	_, err := s.machine.Exec(result.Chunk)
	s.machine.SettleVariables()
	value, hasValue := s.machine.TakeShown()
	if err != nil {
		return s.failed(err)
	}

	for _, v := range vars {
		s.setVariable(v)
	}
	for _, name := range unkept {
		s.note("note: '%s' is not kept: its type has no written form. Give it one: let %s: Type = ...;", name, name)
	}
	s.dropUnusable(vars)
	if hasValue {
		fmt.Fprintln(s.opts.Out, value)
	}
	return Outcome{}
}

// failed reports how a program failed when it ran.
func (s *Session) failed(err error) Outcome {
	if status, asked := vm.ExitCode(err); asked {
		return Outcome{Exit: &status}
	}
	failure, ok := err.(*vm.RuntimeError)
	if !ok {
		fmt.Fprintf(s.opts.Err, "Error: %v\n", err)
		return Outcome{Failed: true}
	}
	fmt.Fprint(s.opts.Err, failure.Diagnostic().String())
	return Outcome{Failed: true}
}

func (st *state) setVariable(v *variable) {
	for i, known := range st.vars {
		if known.name == v.name {
			st.vars[i] = v
			return
		}
	}
	st.vars = append(st.vars, v)
}

func (s *Session) forget(name string) bool {
	for i, v := range s.vars {
		if v.name == name {
			s.vars = append(s.vars[:i:i], s.vars[i+1:]...)
			s.machine.ForgetVariable(name)
			return true
		}
	}
	return false
}

// dropUnusable makes sure the next program compiles with the variables
// just declared. A type can be one the session has no way to write: that
// of a value a module makes and does not export. Such a variable is not
// kept, rather than left to break every input after it.
func (s *Session) dropUnusable(added []*variable) {
	for i := len(added) - 1; i >= 0; i-- {
		src, _ := s.program("", "")
		errs := errors.NewHandler()
		compiler.Analyze(src, s.file, s.compilerOptions(), errs)
		if !errs.HasErrors() {
			return
		}
		if s.forget(added[i].name) {
			s.note("note: '%s' is not kept: its type, %s, cannot be written here", added[i].name, typeDisplay(added[i].typ))
		}
	}
}

// blankEntry is an entry of which nothing is part of the program any
// more: statements, once they have run.
func blankEntry(text string) *entry {
	lines := strings.Count(text, "\n") + 1
	return &entry{text: strings.Repeat("\n", lines-1), lines: lines}
}

// offsetOf gives where a line and column of a text are among its bytes.
func offsetOf(text string, line, col int) (int, bool) {
	offset := 0
	for l := 1; l < line; l++ {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			return 0, false
		}
		offset += next + 1
	}
	for c := 1; c < col; c++ {
		if offset >= len(text) || text[offset] == '\n' {
			return 0, false
		}
		_, size := decodeRune(text[offset:])
		offset += size
	}
	return offset, true
}

// --- Declarations ---

// declare takes declarations and imports as the next input. Each
// replaces what was declared under the same name before.
func (s *Session) declare(text string) Outcome {
	e, problem := s.parseEntry(text)
	if problem {
		return Outcome{Failed: true}
	}

	// What the session would be with them, to be kept if it compiles
	next := s.state.clone()
	var replaced, notes []string
	for _, it := range e.items {
		if next.remove(it.key) && it.kind != "import" {
			replaced = append(replaced, it.name)
		}
	}
	for _, it := range e.items {
		if !isTypeKind(it.kind) || !contains(replaced, it.name) {
			continue
		}
		// A value of the type as it was is not one of the type as it is
		for i := len(next.vars) - 1; i >= 0; i-- {
			if v := next.vars[i]; mentions(v.typ, it.name) {
				next.vars = append(next.vars[:i:i], next.vars[i+1:]...)
				notes = append(notes, fmt.Sprintf("note: '%s' is forgotten: it was a value of %s as declared before", v.name, it.name))
			}
		}
	}
	next.entries = append(next.entries, e)

	src, lay := next.program("", "")
	lay.fresh = lay.starts[len(lay.starts)-2]
	errs := errors.NewHandler()
	compiler.CompileOptions(src, s.file, s.compilerOptions(), errs)
	s.reportAll(lay, errs)
	if errs.HasErrors() {
		if len(replaced) > 0 {
			s.note("note: nothing was changed: %s %s as declared before. What uses it can be declared again in the same input.",
				strings.Join(replaced, ", "), map[bool]string{true: "is", false: "are"}[len(replaced) == 1])
		}
		return Outcome{Failed: true}
	}

	for _, v := range s.vars {
		if !next.has(v.name) {
			s.machine.ForgetVariable(v.name)
		}
	}
	s.state = *next
	for _, note := range notes {
		s.note("%s", note)
	}
	return Outcome{}
}

func (st *state) clone() *state {
	next := &state{vars: append([]*variable(nil), st.vars...)}
	for _, e := range st.entries {
		copied := *e
		copied.items = append([]*item(nil), e.items...)
		next.entries = append(next.entries, &copied)
	}
	return next
}

func (st *state) has(variable string) bool {
	for _, v := range st.vars {
		if v.name == variable {
			return true
		}
	}
	return false
}

// remove takes the declaration of a key out of the program, and reports
// whether there was one.
func (st *state) remove(key string) bool {
	found := false
	for _, e := range st.entries {
		kept := e.items[:0:0]
		for _, it := range e.items {
			if it.key != key {
				kept = append(kept, it)
				continue
			}
			found = true
			e.text = e.text[:it.from] + blank(e.text[it.from:it.to]) + e.text[it.to:]
		}
		e.items = kept
	}
	return found
}

// blank writes a text out as spaces, its line ends kept: what comes after
// it stays on its line.
func blank(text string) string {
	out := []byte(text)
	for i, b := range out {
		if b != '\n' {
			out[i] = ' '
		}
	}
	return string(out)
}

func isTypeKind(kind string) bool {
	return kind == "struct" || kind == "enum" || kind == "trait"
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// parseEntry reads an input of declarations: what each declares, and
// where it is in the text. Mistakes in how it is written are reported.
func (s *Session) parseEntry(text string) (*entry, bool) {
	number := s.Number()
	errs := errors.NewHandler()
	prog := parser.Parse(text, s.file, errs)
	if errs.HasErrors() {
		for _, d := range errs.Diagnostics {
			d.Pos.File = fmt.Sprintf("[%d]", number)
			fmt.Fprint(s.opts.Err, d.String())
		}
		return nil, true
	}
	if len(prog.StmtStarts) > 0 {
		s.note("Error: [%d]:%d: declarations and statements are typed as inputs of their own: declare first, then run",
			number, prog.StmtStarts[0].Line)
		return nil, true
	}

	e := &entry{text: text, lines: strings.Count(text, "\n") + 1}
	add := func(node ast.Node, kind, name, key string) {
		pos := node.Position()
		from, _ := offsetOf(text, pos.Line, pos.Col)
		// An import is placed at its path and a constant at its name,
		// and `pub` is written before what a declaration starts with
		for _, word := range []string{"import", "const", "pub"} {
			if before := strings.TrimRight(text[:from], " \t\r\n"); strings.HasSuffix(before, word) {
				from = len(before) - len(word)
			}
		}
		head := text[from:]
		if end := strings.IndexAny(head, "{;\n"); end >= 0 {
			head = head[:end]
		}
		e.items = append(e.items, &item{key: key, name: name, kind: kind, from: from, head: strings.TrimSpace(head)})
	}
	for _, imp := range prog.Imports {
		add(imp, "import", imp.Path, "import "+imp.String())
	}
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			if d.Name == "main" && !d.Extern {
				s.note("Error: there is no main to write here: what is typed outside a function runs at once. Give the function another name, and call it.")
				return nil, true
			}
			add(d, "fn", d.Name, "name "+d.Name)
		case *ast.StructDecl:
			add(d, "struct", d.Name, "name "+d.Name)
		case *ast.EnumDecl:
			add(d, "enum", d.Name, "name "+d.Name)
		case *ast.TraitDecl:
			add(d, "trait", d.Name, "name "+d.Name)
		case *ast.ConstDecl:
			add(d, "const", d.Name, "name "+d.Name)
		case *ast.ImplDecl:
			name := "impl " + d.StructName
			if d.TraitName != "" {
				name = "impl " + d.TraitName + " for " + d.StructName
			}
			add(d, "impl", name, name)
		}
	}
	sort.SliceStable(e.items, func(i, j int) bool { return e.items[i].from < e.items[j].from })
	for i, it := range e.items {
		it.to = len(text)
		if i+1 < len(e.items) {
			it.to = e.items[i+1].from
		}
	}
	// Said twice in one input, the last one stands
	seen := map[string]bool{}
	for i := len(e.items) - 1; i >= 0; i-- {
		it := e.items[i]
		if it.kind == "import" && seen[it.key] {
			e.text = e.text[:it.from] + blank(e.text[it.from:it.to]) + e.text[it.to:]
			e.items = append(e.items[:i:i], e.items[i+1:]...)
		}
		seen[it.key] = true
	}
	return e, false
}

// --- Questions about the session ---

// Variable is a variable of the session, as shown by `:vars`.
type Variable struct {
	Name, Type, Value string
}

// Variables lists the variables, in the order they were declared.
func (s *Session) Variables() []Variable {
	var out []Variable
	for _, v := range s.vars {
		value, _ := s.machine.Variable(v.name)
		out = append(out, Variable{Name: v.name, Type: typeDisplay(v.typ), Value: summary(vm.FormatLiteral(value))})
	}
	return out
}

// summary keeps a value to one line of a reasonable length.
func summary(text string) string {
	text = strings.ReplaceAll(text, "\n", "\\n")
	if strings.HasPrefix(text, "<fn ") && strings.HasSuffix(text, ">") {
		// The name the compiler gave a function written as a value says
		// nothing; its type, shown beside it, says what it is
		return "<fn>"
	}
	const most = 80
	if runes := []rune(text); len(runes) > most {
		return string(runes[:most-3]) + "..."
	}
	return text
}

// Declarations lists what is declared, in the order it was: the first
// line of each declaration.
func (s *Session) Declarations() []string {
	var out []string
	for _, e := range s.entries {
		for _, it := range e.items {
			out = append(out, it.head)
		}
	}
	return out
}

// TypeOf says what type an expression has, without running it.
func (s *Session) TypeOf(expression string) (string, bool) {
	p := s.check(strings.TrimSpace(expression))
	if p.errs.HasErrors() {
		s.reportAll(p.lay, p.errs)
		return "", false
	}
	if p.block == nil || len(p.block.Stmts) == 0 {
		return "", false
	}
	stmt, ok := p.block.Stmts[len(p.block.Stmts)-1].(*ast.ExprStmt)
	if !ok || p.analysis.Info == nil {
		s.note("Error: ':type' takes an expression")
		return "", false
	}
	t := p.analysis.Info.Types[stmt.Expr]
	if node := types.TypeToAST(t, stmt.BaseNode); node != nil {
		return typeDisplay(node), true
	}
	if t == nil {
		return "", false
	}
	return t.String(), true
}

// Forget stops keeping a variable, or takes a declaration out of the
// program. It reports whether there was something of that name, and
// whether it could go: a declaration the others still use stays.
func (s *Session) Forget(name string) (found, gone bool) {
	if s.forget(name) {
		return true, true
	}
	next := s.state.clone()
	if !next.remove("name " + name) {
		return false, false
	}
	// A type takes its methods with it, and the values it has
	for _, e := range s.entries {
		for _, it := range e.items {
			if it.kind == "impl" && (it.name == "impl "+name || strings.HasSuffix(it.name, " for "+name)) {
				next.remove(it.key)
			}
		}
	}
	for i := len(next.vars) - 1; i >= 0; i-- {
		if mentions(next.vars[i].typ, name) {
			next.vars = append(next.vars[:i:i], next.vars[i+1:]...)
		}
	}
	src, lay := next.program("", "")
	errs := errors.NewHandler()
	compiler.CompileOptions(src, s.file, s.compilerOptions(), errs)
	if errs.HasErrors() {
		s.note("Error: '%s' is still used:", name)
		s.reportAll(lay, errs)
		return true, false
	}
	for _, v := range s.vars {
		if !next.has(v.name) {
			s.machine.ForgetVariable(v.name)
		}
	}
	s.state = *next
	return true, true
}

// Load takes the declarations of a file as an input. Its main, if it has
// one, is left out: the session is what runs. The statements of a script
// are run after, as an input of their own.
func (s *Session) Load(path string) Outcome {
	data, err := os.ReadFile(path)
	if err != nil {
		s.note("Error: cannot read %s: %v", path, err)
		return Outcome{Failed: true}
	}
	text := strings.TrimRight(string(data), " \t\r\n")
	if strings.HasPrefix(text, "#!") {
		// For the system, and a comment only where a file starts
		end := strings.IndexByte(text+"\n", '\n')
		text = blank(text[:end]) + text[end:]
	}

	// Find main by parsing the file as it is, and blank it out
	prog := parser.Parse(text, path, errors.NewHandler())
	var starts, stmts []int
	for _, pos := range prog.StmtStarts {
		from, _ := offsetOf(text, pos.Line, pos.Col)
		stmts = append(stmts, from)
	}
	starts = append(starts, stmts...)
	main := -1
	nodes := make([]ast.Node, 0, len(prog.Imports)+len(prog.Decls))
	for _, imp := range prog.Imports {
		nodes = append(nodes, imp)
	}
	for _, decl := range prog.Decls {
		nodes = append(nodes, decl)
	}
	for _, node := range nodes {
		pos := node.Position()
		from, _ := offsetOf(text, pos.Line, pos.Col)
		if fn, ok := node.(*ast.FnDecl); ok && fn.Name == "main" && !fn.Extern {
			main = from
		}
		starts = append(starts, from)
	}
	if main >= 0 {
		sort.Ints(starts)
		end := len(text)
		for _, start := range starts {
			if start > main {
				end = start
				break
			}
		}
		text = text[:main] + blank(text[main:end]) + text[end:]
		s.note("note: the main of %s is left out; call what it calls", filepath.Base(path))
	}

	// The statements go from the declarations into a text of their own,
	// each on the line it was written on
	sort.Ints(starts)
	script := blank(text)
	for _, from := range stmts {
		end := len(text)
		if i := sort.SearchInts(starts, from+1); i < len(starts) {
			end = starts[i]
		}
		script = script[:from] + text[from:end] + script[end:]
		text = text[:from] + blank(text[from:end]) + text[end:]
	}
	if strings.TrimSpace(text) != "" {
		if out := s.declare(text); out.Failed || strings.TrimSpace(script) == "" {
			return out
		}
	}
	if strings.TrimSpace(script) == "" {
		return Outcome{}
	}
	return s.run(script)
}

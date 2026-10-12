// Package compiler orchestrates the Lumen compilation pipeline, from source
// text to luvm bytecode.
package compiler

import (
	"fmt"
	"os"

	"github.com/dani-gouken/lumen/internal/analysis"
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/codegen"
	"github.com/dani-gouken/lumen/internal/desugar"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/ir"
	"github.com/dani-gouken/lumen/internal/irgen"
	"github.com/dani-gouken/lumen/internal/monomorph"
	"github.com/dani-gouken/lumen/internal/parser"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/types"
	"github.com/dani-gouken/lumen/internal/vm"
)

// Error codes for the compiler driver
const (
	ErrUnsupportedFeature = "E7001" // Language feature the pipeline cannot compile yet
)

// Result holds what each stage of a compilation produced. Stages that did
// not run because an earlier one reported errors leave their field nil.
type Result struct {
	// AST is the program as parsed.
	AST *ast.Program

	// Lowered is the program after monomorphization and desugaring.
	Lowered *ast.Program

	// IR is the intermediate representation.
	IR *ir.Program

	// Chunk is the executable bytecode. It is nil unless the whole
	// pipeline succeeded.
	Chunk *vm.Chunk
}

// CompileFile reads and compiles a Lumen source file.
func CompileFile(path string, errs *errors.Handler) (*Result, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return Compile(string(src), path, errs), nil
}

// Compile runs the pipeline on source text:
//
//	parse -> resolve -> type check -> monomorphize -> desugar
//	      -> closure analysis -> IR generation -> bytecode generation
//
// Diagnostics go to errs. The pipeline stops after the first stage that
// reports an error, so later stages never see a broken program.
func Compile(src, file string, errs *errors.Handler) *Result {
	result := &Result{}
	lines := parser.SourceLines(src)
	failed := errs.HasErrors

	// 1. Parsing
	prog := parser.Parse(src, file, errs)
	result.AST = prog
	if failed() {
		return result
	}
	checkSupported(prog, errs, lines)
	if failed() {
		return result
	}

	// 2. Name resolution
	resolver := resolve.NewResolver(errs)
	resolver.SetSourceLines(lines)
	table := resolver.Resolve(prog)
	if failed() {
		return result
	}

	// 3. Type checking
	checker := types.NewChecker(prog, table, errs)
	checker.SetSourceLines(lines)
	typeInfo := checker.Check()
	if failed() {
		return result
	}

	// 4. Monomorphization. It adds declarations and rewrites call sites, so
	// names and types are computed again for the new program.
	mono := monomorph.NewMonomorphizer(prog, typeInfo, table, errs)
	mono.SetSourceLines(lines)
	prog, _ = mono.Monomorphize()
	if failed() {
		return result
	}
	table, typeInfo = reanalyze(prog)

	// 5. Desugaring
	prog, _ = desugar.NewDesugarer(prog, typeInfo, table, errs, lines).Desugar()
	result.Lowered = prog
	if failed() {
		return result
	}
	table, typeInfo = reanalyze(prog)

	// 6. Closure analysis
	analyzer := analysis.NewAnalyzer(prog, table, typeInfo, errs)
	analyzer.SetSourceLines(lines)
	captures := analyzer.Analyze()
	if failed() {
		return result
	}

	// 7. IR generation
	gen := irgen.NewIRGenerator(prog, typeInfo, table, captures, errs)
	gen.SetSourceLines(lines)
	result.IR = gen.Generate()
	if failed() {
		return result
	}

	// 8. Bytecode generation
	backend := codegen.NewGenerator(result.IR, builtinNames(), errs)
	backend.SetSourceLines(lines)
	chunk := backend.Generate()
	if failed() {
		return result
	}

	result.Chunk = chunk
	return result
}

// reanalyze recomputes names and types for a program produced by an earlier
// stage. The source program already passed both checks, so diagnostics here
// would only describe compiler-generated code and are discarded; anything
// that is really unusable is caught again by IR generation.
func reanalyze(prog *ast.Program) (*resolve.Table, *types.Info) {
	scratch := errors.NewHandler()
	table := resolve.NewResolver(scratch).Resolve(prog)
	typeInfo := types.NewChecker(prog, table, scratch).Check()
	return table, typeInfo
}

func builtinNames() []string {
	var names []string
	for _, fn := range resolve.Builtins() {
		names = append(names, fn.Name)
	}
	return names
}

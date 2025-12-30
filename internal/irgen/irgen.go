package irgen

import (
	"github.com/dani-gouken/lumen/internal/analysis"
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/ir"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/types"
)

// Error codes for IR generation
const (
	ErrUnsupportedConstruct = "E5001" // Language feature not yet supported in IR
)

// IRGenerator converts AST to IR
type IRGenerator struct {
	prog         *ast.Program
	typeInfo     *types.Info
	resolveTable *resolve.Table
	captureInfo  *analysis.CaptureInfo
	errs         *errors.Handler
	sourceLines  map[int]string

	// Output IR program
	irProg *ir.Program

	// Current function being compiled
	currentFunc *FunctionCompiler

	// Lambda counter for generating unique names
	lambdaCount int
}

// NewIRGenerator creates a new IR generator
func NewIRGenerator(
	prog *ast.Program,
	typeInfo *types.Info,
	resolveTable *resolve.Table,
	captureInfo *analysis.CaptureInfo,
	errs *errors.Handler,
) *IRGenerator {
	return &IRGenerator{
		prog:         prog,
		typeInfo:     typeInfo,
		resolveTable: resolveTable,
		captureInfo:  captureInfo,
		errs:         errs,
		sourceLines:  make(map[int]string),
		irProg:       &ir.Program{Functions: make([]*ir.Function, 0)},
		lambdaCount:  0,
	}
}

// SetSourceLines sets source lines for error reporting
func (g *IRGenerator) SetSourceLines(lines map[int]string) {
	g.sourceLines = lines
}

// Generate converts the entire program to IR
func (g *IRGenerator) Generate() *ir.Program {
	// Generate IR for each top-level declaration
	for _, decl := range g.prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			g.compileFunction(d)
		case *ast.ImplDecl:
			for _, method := range d.Methods {
				g.compileFunction(method)
			}
		}
	}

	return g.irProg
}

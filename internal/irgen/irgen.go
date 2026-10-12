package irgen

import (
	"sort"

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
	ErrNotAValue            = "E5002" // Name cannot be used as a value
	ErrInvalidAssignment    = "E5003" // Target of an assignment cannot be written
	ErrAmbiguousMember      = "E5004" // Name is both a struct field and a method
	ErrLoopControl          = "E5005" // break/continue outside of a loop
)

// method describes one impl method of a struct.
type method struct {
	name   string // Name used at call sites: p.name(...)
	irName string // Name of the compiled function: Struct.name
}

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

	// Top-level functions by name (user declarations and builtins)
	funcs map[string]*ast.FnDecl

	// Methods attached to each struct, and every method/field name in use
	structMethods map[string][]method
	methodNames   map[string]bool
	fieldNames    map[string]bool
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
		prog:          prog,
		typeInfo:      typeInfo,
		resolveTable:  resolveTable,
		captureInfo:   captureInfo,
		errs:          errs,
		sourceLines:   make(map[int]string),
		irProg:        &ir.Program{Functions: make([]*ir.Function, 0)},
		lambdaCount:   0,
		funcs:         make(map[string]*ast.FnDecl),
		structMethods: make(map[string][]method),
		methodNames:   make(map[string]bool),
		fieldNames:    make(map[string]bool),
	}
}

// SetSourceLines sets source lines for error reporting
func (g *IRGenerator) SetSourceLines(lines map[int]string) {
	g.sourceLines = lines
}

// Generate converts the entire program to IR
func (g *IRGenerator) Generate() *ir.Program {
	g.collectDecls()

	// Generate IR for each top-level declaration
	for _, decl := range g.prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			g.compileFunction(d, d.Name, false)
		case *ast.ImplDecl:
			for _, m := range d.Methods {
				g.compileFunction(m, methodIRName(d.StructName, m.Name), true)
			}
		}
	}

	return g.irProg
}

// collectDecls indexes functions, struct fields and methods so that call
// sites can be compiled before the declarations they refer to.
func (g *IRGenerator) collectDecls() {
	for _, fn := range resolve.Builtins() {
		g.funcs[fn.Name] = fn
	}

	for _, decl := range g.prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			g.funcs[d.Name] = d
		case *ast.StructDecl:
			for _, field := range d.Fields {
				g.fieldNames[field.Name] = true
			}
		case *ast.ImplDecl:
			for _, m := range d.Methods {
				if m.Body == nil {
					continue
				}
				g.methodNames[m.Name] = true
				g.structMethods[d.StructName] = append(g.structMethods[d.StructName], method{
					name:   m.Name,
					irName: methodIRName(d.StructName, m.Name),
				})
			}
		}
	}

	for _, methods := range g.structMethods {
		sort.Slice(methods, func(i, j int) bool { return methods[i].name < methods[j].name })
	}
}

func methodIRName(structName, methodName string) string {
	return structName + "." + methodName
}

// errorAt reports an IR generation error at the given node.
func (g *IRGenerator) errorAt(node ast.Node, code, msg, suggestion string) {
	if suggestion != "" {
		g.errs.ReportErrorWithSuggestion(code, msg, suggestion, node.Token(), g.sourceLines)
		return
	}
	g.errs.ReportErrorFromToken(code, msg, node.Token(), g.sourceLines)
}

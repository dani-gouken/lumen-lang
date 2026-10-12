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
	ErrLoopControl          = "E5005" // break/continue outside of a loop
)

// CastIntrinsic is the host function that implements `value as Type`. It
// takes the value and a description of the type and returns the value, or
// fails when the value is something else.
const CastIntrinsic = "__cast"

// TypeTestIntrinsic is the host function that implements `value is Type`.
// It takes the value and a description of the type and returns a Bool.
const TypeTestIntrinsic = "__is"

// method describes one impl method of a struct.
type method struct {
	name   string // Name used at call sites: p.name(...)
	irName string // Name of the compiled function: Struct.name

	// A method reachable through a trait is also stored on each value of
	// the struct, so it can be found when only the trait is known.
	fromTrait bool
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

	// Methods of each struct, and for each method name the structs having it
	structMethods map[string][]method
	methodOwners  map[string][]string
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
		methodOwners:  make(map[string][]string),
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
		case *ast.ImplDecl:
			for _, m := range d.Methods {
				if m.Body == nil {
					continue
				}
				g.methodOwners[m.Name] = append(g.methodOwners[m.Name], d.StructName)
				g.structMethods[d.StructName] = append(g.structMethods[d.StructName], method{
					name:      m.Name,
					irName:    methodIRName(d.StructName, m.Name),
					fromTrait: d.TraitName != "",
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

// hasMethod reports whether a struct has an impl method of that name.
func (g *IRGenerator) hasMethod(structName, name string) bool {
	for _, m := range g.structMethods[structName] {
		if m.name == name {
			return true
		}
	}
	return false
}

// dispatch says how a call `receiver.name(...)` reaches its target.
type dispatch int

const (
	dispatchField   dispatch = iota // Not a method: call the function stored in the field
	dispatchStatic                  // The struct is known: call its function directly
	dispatchDynamic                 // Only a trait is known: look the method up on the value
)

// resolveMethod decides how `receiver.name(...)` is called, from the static
// type of the receiver. For a static call it also returns the struct.
func (g *IRGenerator) resolveMethod(receiver ast.Expr, name string) (dispatch, string) {
	recvType := g.typeInfo.Types[receiver]
	if union, ok := recvType.(*types.UnionType); ok {
		// An optional receiver: dispatch on what it is when it is not null
		var value types.Type
		for _, member := range union.Types {
			if !member.Equals(types.Null) {
				if value != nil {
					value = nil
					break
				}
				value = member
			}
		}
		recvType = value
	}

	switch t := recvType.(type) {
	case *types.StructType:
		if g.hasMethod(t.Name, name) {
			return dispatchStatic, t.Name
		}
		return dispatchField, ""
	case *types.BehaviorType:
		return dispatchDynamic, ""
	}

	// The receiver's type is not known (its elements came from an untyped
	// `[]`, for instance). The name still identifies the method when only
	// one struct defines it.
	switch owners := g.methodOwners[name]; len(owners) {
	case 0:
		return dispatchField, ""
	case 1:
		return dispatchStatic, owners[0]
	}
	return dispatchDynamic, ""
}

// storedOnValue reports whether a method has to be stored on each value of
// its struct: when it can be called through a trait, or when several
// structs share the name and a call with an unknown receiver type has to
// find the right one at runtime.
func (g *IRGenerator) storedOnValue(m method) bool {
	return m.fromTrait || len(g.methodOwners[m.name]) > 1
}

// errorAt reports an IR generation error at the given node.
func (g *IRGenerator) errorAt(node ast.Node, code, msg, suggestion string) {
	if suggestion != "" {
		g.errs.ReportErrorWithSuggestion(code, msg, suggestion, node.Token(), g.sourceLines)
		return
	}
	g.errs.ReportErrorFromToken(code, msg, node.Token(), g.sourceLines)
}

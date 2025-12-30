package irgen

import (
	"github.com/dani-gouken/lumen/internal/analysis"
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/ir"
)

// FunctionCompiler compiles a single function
type FunctionCompiler struct {
	gen      *IRGenerator
	function ast.Node // FnDecl or FnExpr
	builder  *ir.Builder

	// Variable to register mapping
	locals map[string]*ir.Register

	// Upvalue mapping (for closures)
	upvalues map[string]int // variable name -> upvalue index

	// Loop context stack (for break/continue)
	loopStack []*loopContext
}

// loopContext tracks loop blocks for break/continue
type loopContext struct {
	breakTarget    *ir.BasicBlock
	continueTarget *ir.BasicBlock
}

// compileFunction compiles a function declaration
func (g *IRGenerator) compileFunction(fn *ast.FnDecl) {
	if fn.Body == nil {
		return // External function declaration
	}

	// Create function compiler
	fc := &FunctionCompiler{
		gen:       g,
		function:  fn,
		builder:   ir.NewBuilder(fn.Name),
		locals:    make(map[string]*ir.Register),
		upvalues:  make(map[string]int),
		loopStack: make([]*loopContext, 0),
	}

	g.currentFunc = fc

	// Allocate registers for parameters
	for _, param := range fn.Params {
		reg := fc.builder.AddParam()
		fc.locals[param.Name] = reg
	}

	// Check if this function captures variables
	if captures := g.captureInfo.GetCaptures(fn); captures != nil && len(captures.CapturedVars) > 0 {
		fc.setupUpvalues(captures)
	}

	// Compile function body
	fc.compileBlock(fn.Body)

	// Add implicit return if needed
	fc.addImplicitReturn()

	// Add to program
	g.irProg.Functions = append(g.irProg.Functions, fc.builder.Func)
}

// setupUpvalues creates the upvalue mapping from capture info
func (fc *FunctionCompiler) setupUpvalues(captures *analysis.FunctionCaptures) {
	for i, cv := range captures.CapturedVars {
		fc.upvalues[cv.Name] = i
	}
}

// addImplicitReturn adds a void return if the function doesn't end with one
func (fc *FunctionCompiler) addImplicitReturn() {
	// Check if last instruction is already a return
	if len(fc.builder.CurrentBlock.Instructions) > 0 {
		last := fc.builder.CurrentBlock.Instructions[len(fc.builder.CurrentBlock.Instructions)-1]
		if last.Op == ir.OpRet {
			return
		}
	}

	// Add void return
	fc.builder.CreateRet(nil)
}

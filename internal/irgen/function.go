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

	// Variable to register mapping, one map per lexical block (innermost last)
	scopes []map[string]*ir.Register

	// Registers that hold a named variable. Expression results that are not
	// in this set are temporaries and can be adopted by a `let`.
	varRegs map[int]bool

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

func (g *IRGenerator) newFunctionCompiler(fn ast.Node, name string) *FunctionCompiler {
	return &FunctionCompiler{
		gen:       g,
		function:  fn,
		builder:   ir.NewBuilder(name),
		scopes:    []map[string]*ir.Register{make(map[string]*ir.Register)},
		varRegs:   make(map[int]bool),
		upvalues:  make(map[string]int),
		loopStack: make([]*loopContext, 0),
	}
}

// compileFunction compiles a function declaration under the given IR name.
// Methods take the receiver as an implicit first parameter named `self`.
func (g *IRGenerator) compileFunction(fn *ast.FnDecl, name string, isMethod bool) {
	if fn.Body == nil {
		return // External function declaration
	}

	fc := g.newFunctionCompiler(fn, name)
	if isMethod {
		fc.declare("self", fc.builder.AddParam())
	}
	fc.compileBody(fn.Params, fn.Body)
}

// compileBody compiles parameters and body, then adds the finished function
// to the program. Nested lambdas are added before their enclosing function.
func (fc *FunctionCompiler) compileBody(params []*ast.Param, body *ast.BlockStmt) *ir.Function {
	g := fc.gen
	parent := g.currentFunc
	g.currentFunc = fc
	defer func() { g.currentFunc = parent }()

	fc.setPos(fc.function)

	// Allocate registers for parameters
	for _, param := range params {
		fc.declare(param.Name, fc.builder.AddParam())
	}

	// Check if this function captures variables
	if captures := g.captureInfo.GetCaptures(fc.function); captures != nil && len(captures.CapturedVars) > 0 {
		fc.setupUpvalues(captures)
	}

	// Compile function body
	fc.compileBlock(body)

	// Add implicit return if needed
	fc.addImplicitReturn()

	// Add to program
	g.irProg.Functions = append(g.irProg.Functions, fc.builder.Func)
	return fc.builder.Func
}

// setupUpvalues creates the upvalue mapping from capture info
func (fc *FunctionCompiler) setupUpvalues(captures *analysis.FunctionCaptures) {
	for i, cv := range captures.CapturedVars {
		fc.upvalues[cv.Name] = i
	}
	fc.builder.Func.UpvalueCount = len(captures.CapturedVars)
}

// addImplicitReturn adds a void return if the function doesn't end with one
func (fc *FunctionCompiler) addImplicitReturn() {
	if fc.blockEndsWithTerminator() {
		return
	}

	// Add void return
	fc.builder.CreateRet(nil)
}

// --- Scopes ---

func (fc *FunctionCompiler) pushScope() {
	fc.scopes = append(fc.scopes, make(map[string]*ir.Register))
}

func (fc *FunctionCompiler) popScope() {
	fc.scopes = fc.scopes[:len(fc.scopes)-1]
}

// declare binds a variable name to a register in the innermost scope.
func (fc *FunctionCompiler) declare(name string, reg *ir.Register) {
	fc.scopes[len(fc.scopes)-1][name] = reg
	fc.varRegs[reg.ID] = true
}

// lookup finds the register of a local variable, innermost scope first.
func (fc *FunctionCompiler) lookup(name string) (*ir.Register, bool) {
	for i := len(fc.scopes) - 1; i >= 0; i-- {
		if reg, ok := fc.scopes[i][name]; ok {
			return reg, true
		}
	}
	return nil, false
}

// setPos records the source position for the instructions emitted next.
func (fc *FunctionCompiler) setPos(node ast.Node) {
	if node == nil {
		return
	}
	if pos := node.Position(); pos.Line > 0 {
		fc.builder.Pos = pos
	}
}

// startDeadBlock switches to a fresh block after a terminator, so code that
// follows a return/break/continue does not land in a finished block.
func (fc *FunctionCompiler) startDeadBlock() {
	fc.builder.SetBlock(fc.builder.NewBlock())
}

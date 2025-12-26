package irgen

import (
	"fmt"

	"github.com/yourname/lumen/internal/analysis"
	"github.com/yourname/lumen/internal/ast"
	"github.com/yourname/lumen/internal/ir"
)

// createClosure creates a closure with captured variables
func (fc *FunctionCompiler) createClosure(
	fn *ir.Function,
	captures *analysis.FunctionCaptures,
) *ir.Register {
	// Gather captured values
	capturedVals := make([]ir.Value, len(captures.CapturedVars))

	for i, cv := range captures.CapturedVars {
		if cv.FromUpvalue {
			// Capture from our upvalue
			upvalIdx, ok := fc.upvalues[cv.Name]
			if ok {
				capturedVals[i] = fc.builder.CreateLoadUpvalue(upvalIdx)
			} else {
				// Fallback: try to get from locals
				if reg, ok := fc.locals[cv.Name]; ok {
					capturedVals[i] = reg
				}
			}
		} else {
			// Capture from our local
			if reg, ok := fc.locals[cv.Name]; ok {
				capturedVals[i] = reg
			} else {
				// Try upvalue as fallback
				if upvalIdx, ok := fc.upvalues[cv.Name]; ok {
					capturedVals[i] = fc.builder.CreateLoadUpvalue(upvalIdx)
				}
			}
		}
	}

	// Emit OpMakeClosure instruction
	funcRef := ir.Constant{Val: fn.Name, Type: "FuncRef"}
	return fc.builder.CreateMakeClosure(funcRef, capturedVals...)
}

// compileLambda compiles a lambda expression (FnExpr)
func (fc *FunctionCompiler) compileLambda(lambda *ast.FnExpr) *ir.Register {
	// First, compile the lambda as a separate function
	lambdaFunc := fc.gen.compileLambdaFunction(lambda)

	// Get capture info
	captures := fc.gen.captureInfo.GetCaptures(lambda)
	if captures == nil || len(captures.CapturedVars) == 0 {
		// No captures - just reference the function
		return fc.builder.CreateMov(ir.Constant{
			Val:  lambdaFunc.Name,
			Type: "FuncRef",
		})
	}

	// Has captures - create closure
	return fc.createClosure(lambdaFunc, captures)
}

// compileLambdaFunction compiles a lambda expression as a separate IR function
func (g *IRGenerator) compileLambdaFunction(lambda *ast.FnExpr) *ir.Function {
	// Generate unique name
	name := fmt.Sprintf("lambda_%d", g.lambdaCount)
	g.lambdaCount++

	fc := &FunctionCompiler{
		gen:       g,
		function:  lambda,
		builder:   ir.NewBuilder(name),
		locals:    make(map[string]*ir.Register),
		upvalues:  make(map[string]int),
		loopStack: make([]*loopContext, 0),
	}

	// Allocate parameters
	for _, param := range lambda.Params {
		reg := fc.builder.AddParam()
		fc.locals[param.Name] = reg
	}

	// Setup upvalues if any
	if captures := g.captureInfo.GetCaptures(lambda); captures != nil {
		fc.setupUpvalues(captures)
	}

	// Compile body
	fc.compileBlock(lambda.Body)
	fc.addImplicitReturn()

	// Add to program
	g.irProg.Functions = append(g.irProg.Functions, fc.builder.Func)

	return fc.builder.Func
}

// compileNestedFunction handles nested function declarations
// This is called when we encounter a function declaration inside another function
func (g *IRGenerator) compileNestedFunction(fn *ast.FnDecl, parentFC *FunctionCompiler) *ir.Register {
	// Compile as a separate function
	g.compileFunction(fn)

	// Get the function from the program (last added)
	compiledFunc := g.irProg.Functions[len(g.irProg.Functions)-1]

	// Check if it captures anything
	captures := g.captureInfo.GetCaptures(fn)
	if captures == nil || len(captures.CapturedVars) == 0 {
		// No captures - just return function reference
		return parentFC.builder.CreateMov(ir.Constant{
			Val:  compiledFunc.Name,
			Type: "FuncRef",
		})
	}

	// Has captures - create closure
	return parentFC.createClosure(compiledFunc, captures)
}

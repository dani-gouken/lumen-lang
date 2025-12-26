package irgen

import (
	"github.com/yourname/lumen/internal/ast"
	"github.com/yourname/lumen/internal/ir"
)

// compileBlock compiles a block of statements
func (fc *FunctionCompiler) compileBlock(block *ast.BlockStmt) {
	if block == nil {
		return
	}

	for _, stmt := range block.Stmts {
		fc.compileStmt(stmt)
	}
}

// compileStmt compiles a single statement
func (fc *FunctionCompiler) compileStmt(stmt ast.Stmt) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.LetStmt:
		fc.compileLetStmt(s)
	case *ast.ReturnStmt:
		fc.compileReturnStmt(s)
	case *ast.IfStmt:
		fc.compileIfStmt(s)
	case *ast.WhileStmt:
		fc.compileWhileStmt(s)
	case *ast.ForStmt:
		fc.compileForStmt(s)
	case *ast.BlockStmt:
		fc.compileBlock(s)
	case *ast.ExprStmt:
		fc.compileExpr(s.Expr)
	case *ast.BreakStmt:
		fc.compileBreakStmt()
	case *ast.ContinueStmt:
		fc.compileContinueStmt()
	case *ast.MatchStmt:
		fc.compileMatchStmt(s)
	default:
		fc.gen.errs.ReportErrorFromToken(
			ErrUnsupportedConstruct,
			"Unsupported statement type in IR generation",
			stmt.Token(),
			fc.gen.sourceLines,
		)
	}
}

// compileLetStmt compiles a variable declaration
func (fc *FunctionCompiler) compileLetStmt(stmt *ast.LetStmt) {
	// Compile initializer
	var reg *ir.Register
	if stmt.Value != nil {
		reg = fc.compileExpr(stmt.Value)
	} else {
		// No initializer - load null/zero
		reg = fc.builder.CreateMov(ir.Constant{Val: nil, Type: "Null"})
	}

	// Store in locals map
	fc.locals[stmt.Name] = reg

	// If this variable is captured, we might need to handle it specially
	// For now, we just store it as a local
}

// compileReturnStmt compiles a return statement
func (fc *FunctionCompiler) compileReturnStmt(stmt *ast.ReturnStmt) {
	var val ir.Value
	if stmt.Value != nil {
		val = fc.compileExpr(stmt.Value)
	}
	fc.builder.CreateRet(val)
}

package irgen

import (
	"github.com/yourname/lumen/internal/ast"
	"github.com/yourname/lumen/internal/ir"
)

// compileIfStmt compiles an if statement
func (fc *FunctionCompiler) compileIfStmt(stmt *ast.IfStmt) {
	// Compile condition
	cond := fc.compileExpr(stmt.Cond)

	// Create basic blocks
	thenBlock := fc.builder.NewBlock()
	mergeBlock := fc.builder.NewBlock()
	var elseBlock *ir.BasicBlock

	// Branch
	if stmt.Else != nil {
		elseBlock = fc.builder.NewBlock()
		fc.builder.CreateBr(cond, thenBlock, elseBlock)
	} else {
		fc.builder.CreateBr(cond, thenBlock, mergeBlock)
	}

	// Then block
	fc.builder.SetBlock(thenBlock)
	fc.compileStmt(stmt.Then)
	// Only add jump if the block doesn't already end with a return
	if !fc.blockEndsWithTerminator() {
		fc.builder.CreateJmp(mergeBlock)
	}

	// Else block (if present)
	if stmt.Else != nil {
		fc.builder.SetBlock(elseBlock)
		fc.compileStmt(stmt.Else)
		if !fc.blockEndsWithTerminator() {
			fc.builder.CreateJmp(mergeBlock)
		}
	}

	// Merge block
	fc.builder.SetBlock(mergeBlock)
}

// compileWhileStmt compiles a while loop
func (fc *FunctionCompiler) compileWhileStmt(stmt *ast.WhileStmt) {
	// Create basic blocks
	condBlock := fc.builder.NewBlock()
	bodyBlock := fc.builder.NewBlock()
	exitBlock := fc.builder.NewBlock()

	// Jump to condition
	fc.builder.CreateJmp(condBlock)

	// Condition block
	fc.builder.SetBlock(condBlock)
	cond := fc.compileExpr(stmt.Cond)
	fc.builder.CreateBr(cond, bodyBlock, exitBlock)

	// Push loop context
	fc.loopStack = append(fc.loopStack, &loopContext{
		breakTarget:    exitBlock,
		continueTarget: condBlock,
	})

	// Body block
	fc.builder.SetBlock(bodyBlock)
	fc.compileStmt(stmt.Body)
	if !fc.blockEndsWithTerminator() {
		fc.builder.CreateJmp(condBlock)
	}

	// Pop loop context
	fc.loopStack = fc.loopStack[:len(fc.loopStack)-1]

	// Exit block
	fc.builder.SetBlock(exitBlock)
}

// compileForStmt compiles a for-in loop
func (fc *FunctionCompiler) compileForStmt(stmt *ast.ForStmt) {
	// For-in loop: for x in iterable { body }
	// This is simplified - a real implementation would need iterator protocol

	// Evaluate iterable
	iterable := fc.compileExpr(stmt.Iterable)

	// Create basic blocks
	condBlock := fc.builder.NewBlock()
	bodyBlock := fc.builder.NewBlock()
	exitBlock := fc.builder.NewBlock()

	// For now, treat as simple iteration
	// TODO: Implement proper iterator protocol
	fc.builder.CreateJmp(bodyBlock)

	// Push loop context
	fc.loopStack = append(fc.loopStack, &loopContext{
		breakTarget:    exitBlock,
		continueTarget: condBlock,
	})

	// Body block
	fc.builder.SetBlock(bodyBlock)
	fc.compileStmt(stmt.Body)
	if !fc.blockEndsWithTerminator() {
		fc.builder.CreateJmp(exitBlock)
	}

	// Pop loop context
	fc.loopStack = fc.loopStack[:len(fc.loopStack)-1]

	// Exit block
	fc.builder.SetBlock(exitBlock)

	_ = iterable // Suppress unused warning
}

// compileBreakStmt compiles a break statement
func (fc *FunctionCompiler) compileBreakStmt() {
	if len(fc.loopStack) == 0 {
		// Error: break outside loop
		return
	}
	ctx := fc.loopStack[len(fc.loopStack)-1]
	fc.builder.CreateJmp(ctx.breakTarget)
}

// compileContinueStmt compiles a continue statement
func (fc *FunctionCompiler) compileContinueStmt() {
	if len(fc.loopStack) == 0 {
		// Error: continue outside loop
		return
	}
	ctx := fc.loopStack[len(fc.loopStack)-1]
	fc.builder.CreateJmp(ctx.continueTarget)
}

// compileMatchStmt compiles a match statement (should be desugared to if-else)
func (fc *FunctionCompiler) compileMatchStmt(stmt *ast.MatchStmt) {
	// Match should be desugared to if-else chains
	// This is a fallback implementation
	matchExpr := fc.compileExpr(stmt.Cond)

	mergeBlock := fc.builder.NewBlock()
	var nextBlock *ir.BasicBlock

	for i, arm := range stmt.Cases {
		armBlock := fc.builder.NewBlock()
		nextBlock = fc.builder.NewBlock()

		// For now, skip pattern matching and just execute each arm
		// A real implementation would need pattern matching logic
		fc.builder.CreateJmp(armBlock)

		fc.builder.SetBlock(armBlock)
		// arm.Body is a Node, which can be BlockStmt or Expr
		if body, ok := arm.Body.(*ast.BlockStmt); ok {
			fc.compileStmt(body)
		} else if expr, ok := arm.Body.(ast.Expr); ok {
			fc.compileExpr(expr)
		}
		if !fc.blockEndsWithTerminator() {
			fc.builder.CreateJmp(mergeBlock)
		}

		fc.builder.SetBlock(nextBlock)

		// Last arm - jump to merge
		if i == len(stmt.Cases)-1 {
			fc.builder.CreateJmp(mergeBlock)
		}
	}

	fc.builder.SetBlock(mergeBlock)
	_ = matchExpr // Suppress unused warning
}

// blockEndsWithTerminator checks if the current block ends with a terminator
func (fc *FunctionCompiler) blockEndsWithTerminator() bool {
	if len(fc.builder.CurrentBlock.Instructions) == 0 {
		return false
	}
	last := fc.builder.CurrentBlock.Instructions[len(fc.builder.CurrentBlock.Instructions)-1]
	return last.Op == ir.OpRet || last.Op == ir.OpJmp || last.Op == ir.OpBr
}

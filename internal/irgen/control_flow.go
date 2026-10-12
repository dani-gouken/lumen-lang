package irgen

import (
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/ir"
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

// compileForStmt compiles a for-in loop over an array or string.
// Desugaring normally rewrites these into while loops before IR generation;
// this handles any that reach it directly.
//
//	index = 0; length = len(iterable)
//	cond: br index < length, body, exit
//	body: item = iterable[index]; index = index + 1; ...; jmp cond
func (fc *FunctionCompiler) compileForStmt(stmt *ast.ForStmt) {
	iterable := fc.compileExpr(stmt.Iterable)
	fc.setPos(stmt)

	index := fc.builder.CreateMov(ir.Constant{Val: int64(0), Type: "Int"})
	fc.varRegs[index.ID] = true
	one := fc.builder.CreateMov(ir.Constant{Val: int64(1), Type: "Int"})
	length := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpLen, length, iterable)

	condBlock := fc.builder.NewBlock()
	bodyBlock := fc.builder.NewBlock()
	exitBlock := fc.builder.NewBlock()

	fc.builder.CreateJmp(condBlock)

	fc.builder.SetBlock(condBlock)
	inRange := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpLt, inRange, index, length)
	fc.builder.CreateBr(inRange, bodyBlock, exitBlock)

	fc.loopStack = append(fc.loopStack, &loopContext{
		breakTarget:    exitBlock,
		continueTarget: condBlock,
	})

	fc.builder.SetBlock(bodyBlock)
	fc.pushScope()
	item := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpGetIndex, item, iterable, index)
	fc.declare(stmt.Var, item)
	// Advance before the body so `continue` cannot skip it
	fc.builder.Emit(ir.OpAdd, index, index, one)
	fc.compileStmt(stmt.Body)
	fc.popScope()
	if !fc.blockEndsWithTerminator() {
		fc.builder.CreateJmp(condBlock)
	}

	fc.loopStack = fc.loopStack[:len(fc.loopStack)-1]

	fc.builder.SetBlock(exitBlock)
}

// compileBreakStmt compiles a break statement
func (fc *FunctionCompiler) compileBreakStmt(stmt *ast.BreakStmt) {
	if len(fc.loopStack) == 0 {
		fc.gen.errorAt(stmt, ErrLoopControl, "'break' outside of a loop", "")
		return
	}
	ctx := fc.loopStack[len(fc.loopStack)-1]
	fc.builder.CreateJmp(ctx.breakTarget)
	fc.startDeadBlock()
}

// compileContinueStmt compiles a continue statement
func (fc *FunctionCompiler) compileContinueStmt(stmt *ast.ContinueStmt) {
	if len(fc.loopStack) == 0 {
		fc.gen.errorAt(stmt, ErrLoopControl, "'continue' outside of a loop", "")
		return
	}
	ctx := fc.loopStack[len(fc.loopStack)-1]
	fc.builder.CreateJmp(ctx.continueTarget)
	fc.startDeadBlock()
}

// compileMatchStmt rejects match statements: desugaring turns every match it
// supports into an if-else chain, so one reaching IR generation uses a
// pattern that cannot be lowered yet.
func (fc *FunctionCompiler) compileMatchStmt(stmt *ast.MatchStmt) {
	fc.gen.errorAt(stmt, ErrUnsupportedConstruct,
		"this match statement cannot be compiled yet",
		"Match on literal values, or use if/else")
}

// blockEndsWithTerminator checks if the current block ends with a terminator
func (fc *FunctionCompiler) blockEndsWithTerminator() bool {
	if len(fc.builder.CurrentBlock.Instructions) == 0 {
		return false
	}
	last := fc.builder.CurrentBlock.Instructions[len(fc.builder.CurrentBlock.Instructions)-1]
	return last.Op == ir.OpRet || last.Op == ir.OpJmp || last.Op == ir.OpBr
}

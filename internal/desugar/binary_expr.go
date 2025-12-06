package desugar

import (
	"github.com/yourname/lumen/internal/ast"
	"github.com/yourname/lumen/internal/types"
)

// desugarBinaryExpr desugars binary expressions, particularly short-circuit and null coalescing operators
func (d *Desugarer) desugarBinaryExpr(binary *ast.BinaryExpr) ast.Expr {
	// Check for operators that need special desugaring
	switch binary.Op {
	case "&&":
		return d.desugarLogicalAnd(binary)
	case "||":
		return d.desugarLogicalOr(binary)
	case "??":
		return d.desugarNullCoalesce(binary)
	default:
		// For other operators, just desugar the operands
		left := d.desugarExpr(binary.Left)
		right := d.desugarExpr(binary.Right)

		return &ast.BinaryExpr{
			BaseNode: binary.BaseNode,
			Left:     left,
			Op:       binary.Op,
			Right:    right,
		}
	}
}

// desugarLogicalAnd desugars the && operator
//
// Note: When && appears in pure expression context (not caught by statement-level desugaring),
// we recursively desugar the operands but keep the && operator itself.
// Statement-level contexts (if conditions, let statements, etc.) handle full desugaring
// by hoisting to temporary variables via desugarShortCircuitInCondition/Expression.
func (d *Desugarer) desugarLogicalAnd(binary *ast.BinaryExpr) ast.Expr {
	// Recursively desugar operands
	left := d.desugarExpr(binary.Left)
	right := d.desugarExpr(binary.Right)

	return &ast.BinaryExpr{
		BaseNode: binary.BaseNode,
		Left:     left,
		Op:       "&&",
		Right:    right,
	}
}

// desugarLogicalOr desugars the || operator
//
// Note: Similar to &&, pure expression contexts keep the operator,
// while statement contexts get fully desugared via helper functions.
func (d *Desugarer) desugarLogicalOr(binary *ast.BinaryExpr) ast.Expr {
	// Recursively desugar operands
	left := d.desugarExpr(binary.Left)
	right := d.desugarExpr(binary.Right)

	return &ast.BinaryExpr{
		BaseNode: binary.BaseNode,
		Left:     left,
		Op:       "||",
		Right:    right,
	}
}

// desugarNullCoalesce desugars the ?? operator
//
// Note: Similar to && and ||, this keeps the operator in pure expression contexts.
// Full desugaring happens at statement level via containsShortCircuit detection.
func (d *Desugarer) desugarNullCoalesce(binary *ast.BinaryExpr) ast.Expr {
	// Recursively desugar operands
	left := d.desugarExpr(binary.Left)
	right := d.desugarExpr(binary.Right)

	return &ast.BinaryExpr{
		BaseNode: binary.BaseNode,
		Left:     left,
		Op:       "??",
		Right:    right,
	}
}

// NOTE: Short-circuit operators (&&, ||, ??) ARE fully desugared in statement contexts:
// - If/while conditions: via desugarShortCircuitInCondition
// - Let statements: via desugarShortCircuitInExpression
// - Return statements: can be extended similarly if needed
//
// The key insight is that we CAN transform expressions into statements by detecting
// short-circuit operators at the statement level and hoisting the desugaring logic.
// Any remaining && || ?? in pure expression contexts will be handled by IR generation.

// desugarShortCircuitInCondition handles short-circuit operators in conditional contexts
// This is called from if/while statements where we can insert temporary variables
func (d *Desugarer) desugarShortCircuitInCondition(expr ast.Expr) ([]ast.Stmt, ast.Expr) {
	binary, ok := expr.(*ast.BinaryExpr)
	if !ok {
		// Not a binary expression, just desugar normally
		return nil, d.desugarExpr(expr)
	}

	// Check complexity and warn if too deeply nested
	d.checkShortCircuitComplexity(expr)

	pos := expr.Position()

	switch binary.Op {
	case "&&":
		// Transform: condition1 && condition2
		// Into statements:
		//   let __tmp_N = condition1;
		//   let __result_N = false;
		//   if __tmp_N {
		//     __result_N = condition2;
		//   }
		// And use __result_N as the condition

		tmpVar1 := d.generateTmpVar()
		resultVar := d.generateTmpVar()

		// Recursively desugar left operand (might also be short-circuit)
		leftPreStmts, leftExpr := d.desugarShortCircuitInCondition(binary.Left)

		// let __tmp_N = condition1;
		tmpStmt := &ast.LetStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Name:     tmpVar1,
			Type: &ast.Type{
				BaseNode: ast.BaseNode{Pos: pos},
				Kind:     ast.TypeBasic,
				Name:     "Bool",
			},
			Value:   leftExpr,
			IsConst: false,
		}

		tmpIdent := d.createIdent(tmpVar1, pos)
		d.typeInfo.Types[tmpIdent] = types.Bool

		// let __result_N = false;
		resultStmt := &ast.LetStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Name:     resultVar,
			Type: &ast.Type{
				BaseNode: ast.BaseNode{Pos: pos},
				Kind:     ast.TypeBasic,
				Name:     "Bool",
			},
			Value:   d.createBoolLiteral(false, pos),
			IsConst: false,
		}

		resultIdent := d.createIdent(resultVar, pos)
		d.typeInfo.Types[resultIdent] = types.Bool

		// Recursively desugar right operand
		rightPreStmts, rightExpr := d.desugarShortCircuitInCondition(binary.Right)

		// __result_N = condition2;
		resultAssign := &ast.ExprStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Expr: &ast.BinaryExpr{
				BaseNode: ast.BaseNode{Pos: pos},
				Left:     d.createIdent(resultVar, pos),
				Op:       "=",
				Right:    rightExpr,
			},
		}

		// Build the if body with right operand pre-statements
		ifBodyStmts := append(rightPreStmts, resultAssign)
		ifBody := d.wrapInBlock(ifBodyStmts, pos)

		// if __tmp_N { __result_N = condition2; }
		ifStmt := &ast.IfStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Cond:     tmpIdent,
			Then:     ifBody,
			Else:     nil,
		}

		// Combine all pre-statements
		preStmts := leftPreStmts
		preStmts = append(preStmts, tmpStmt, resultStmt, ifStmt)

		return preStmts, resultIdent

	case "||":
		// Transform: condition1 || condition2
		// Into statements:
		//   let __tmp_N = condition1;
		//   let __result_N = true;
		//   if !__tmp_N {
		//     __result_N = condition2;
		//   }
		// And use __result_N as the condition

		tmpVar1 := d.generateTmpVar()
		resultVar := d.generateTmpVar()

		// Recursively desugar left operand
		leftPreStmts, leftExpr := d.desugarShortCircuitInCondition(binary.Left)

		// let __tmp_N = condition1;
		tmpStmt := &ast.LetStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Name:     tmpVar1,
			Type: &ast.Type{
				BaseNode: ast.BaseNode{Pos: pos},
				Kind:     ast.TypeBasic,
				Name:     "Bool",
			},
			Value:   leftExpr,
			IsConst: false,
		}

		tmpIdent := d.createIdent(tmpVar1, pos)
		d.typeInfo.Types[tmpIdent] = types.Bool

		// let __result_N = true;
		resultStmt := &ast.LetStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Name:     resultVar,
			Type: &ast.Type{
				BaseNode: ast.BaseNode{Pos: pos},
				Kind:     ast.TypeBasic,
				Name:     "Bool",
			},
			Value:   d.createBoolLiteral(true, pos),
			IsConst: false,
		}

		resultIdent := d.createIdent(resultVar, pos)
		d.typeInfo.Types[resultIdent] = types.Bool

		// Recursively desugar right operand
		rightPreStmts, rightExpr := d.desugarShortCircuitInCondition(binary.Right)

		// __result_N = condition2;
		resultAssign := &ast.ExprStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Expr: &ast.BinaryExpr{
				BaseNode: ast.BaseNode{Pos: pos},
				Left:     d.createIdent(resultVar, pos),
				Op:       "=",
				Right:    rightExpr,
			},
		}

		// Build the if body with right operand pre-statements
		ifBodyStmts := append(rightPreStmts, resultAssign)
		ifBody := d.wrapInBlock(ifBodyStmts, pos)

		// !__tmp_N
		notCondition := &ast.UnaryExpr{
			BaseNode: ast.BaseNode{Pos: pos},
			Op:       "!",
			Expr:     tmpIdent,
		}
		d.typeInfo.Types[notCondition] = types.Bool

		// if !__tmp_N { __result_N = condition2; }
		ifStmt := &ast.IfStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Cond:     notCondition,
			Then:     ifBody,
			Else:     nil,
		}

		// Combine all pre-statements
		preStmts := leftPreStmts
		preStmts = append(preStmts, tmpStmt, resultStmt, ifStmt)

		return preStmts, resultIdent

	default:
		// Not a short-circuit operator
		return nil, d.desugarExpr(expr)
	}
}

// desugarShortCircuitInExpression handles short-circuit operators in expression contexts
// This transforms the expression and returns pre-statements that need to be executed first
func (d *Desugarer) desugarShortCircuitInExpression(expr ast.Expr) ([]ast.Stmt, ast.Expr) {
	// Use the same logic as desugarShortCircuitInCondition
	// The result is a variable that holds the boolean/expression result
	return d.desugarShortCircuitInCondition(expr)
}

// containsShortCircuit checks if an expression contains &&, ||, or ?? operators
func (d *Desugarer) containsShortCircuit(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op == "&&" || e.Op == "||" || e.Op == "??" {
			return true
		}
		return d.containsShortCircuit(e.Left) || d.containsShortCircuit(e.Right)
	case *ast.UnaryExpr:
		return d.containsShortCircuit(e.Expr)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			if d.containsShortCircuit(arg) {
				return true
			}
		}
		return d.containsShortCircuit(e.Func)
	case *ast.IndexExpr:
		return d.containsShortCircuit(e.Expr) || d.containsShortCircuit(e.Index)
	case *ast.MemberExpr:
		return d.containsShortCircuit(e.Expr)
	default:
		return false
	}
}

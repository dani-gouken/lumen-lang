package desugar

import (
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/token"
	"github.com/dani-gouken/lumen/internal/types"
)

// desugarMatchStmt transforms a match statement into nested if-else statements
//
// Simple Value Matching:
//
//	match x {
//	  case 1: { action1(); }
//	  case 2: { action2(); }
//	  default: { defaultAction(); }
//	}
//
// Becomes:
//
//	if x == 1 {
//	  action1();
//	} else if x == 2 {
//	  action2();
//	} else {
//	  defaultAction();
//	}
//
// Type Matching:
//
//	match value {
//	  case Int: { handleInt(); }
//	  case String: { handleString(); }
//	}
//
// Becomes:
//
//	{
//	  let __match_tmp_N = value;
//	  if __is_type(__match_tmp_N, Int) {
//	    handleInt();
//	  } else if __is_type(__match_tmp_N, String) {
//	    handleString();
//	  }
//	}
func (d *Desugarer) desugarMatchStmt(match *ast.MatchStmt) ast.Stmt {
	pos := match.Position()

	// Desugar the matched expression
	matchedExpr := d.desugarExpr(match.Cond)

	// If we have no cases, return an empty block
	if len(match.Cases) == 0 {
		return d.wrapInBlock([]ast.Stmt{}, pos)
	}

	// Validate all patterns
	for _, caseStmt := range match.Cases {
		if !d.validateMatchPattern(caseStmt.Pattern) {
			// Continue processing other cases even if one fails
			// This allows reporting multiple errors at once
		}
	}

	// Check if we need a temporary variable
	// We use a temporary if:
	// 1. The expression is complex (not just an identifier or literal)
	// 2. We have type matching or enum variant matching
	// A plain variable is tested directly, also for type patterns: the test
	// `value is Int` then tells the type checker that `value` is an Int
	// inside the case, which a copy in a temporary would not.
	needsTemp := d.isComplexExpr(matchedExpr)

	var tmpVar string
	var tmpVarExpr ast.Expr
	var stmts []ast.Stmt

	if needsTemp {
		// Generate temporary variable: let __match_tmp_N = matched_expr;
		tmpVar = d.generateMatchVar()
		tmpVarType := d.inferTypeFromExpr(match.Cond, pos)

		tmpInit := &ast.LetStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Name:     tmpVar,
			Type:     tmpVarType,
			Value:    matchedExpr,
			IsConst:  false,
		}

		tmpVarExpr = d.createIdent(tmpVar, pos)
		if exprType, ok := d.typeInfo.Types[match.Cond]; ok {
			d.typeInfo.Types[tmpVarExpr] = exprType
		}

		stmts = append(stmts, tmpInit)
	} else {
		tmpVarExpr = matchedExpr
	}

	// Build the if-else chain
	var ifChain ast.Stmt
	for i := len(match.Cases) - 1; i >= 0; i-- {
		caseStmt := match.Cases[i]
		// When the cases cover every variant of an enum, the last one needs
		// no test: nothing else is left. Written as the final `else`, it
		// also shows the stages below that one of the cases always runs.
		last := i == len(match.Cases)-1 && match.Exhaustive
		ifChain = d.buildMatchCase(tmpVarExpr, caseStmt, ifChain, pos, last)
	}

	if needsTemp {
		stmts = append(stmts, ifChain)
		block := d.wrapInBlock(stmts, pos)
		d.info.Mapping[match] = block
		return block
	}

	d.info.Mapping[match] = ifChain
	return ifChain
}

// buildMatchCase builds a single case in the if-else chain
func (d *Desugarer) buildMatchCase(matchedExpr ast.Expr, caseStmt *ast.MatchCase, elseStmt ast.Stmt, pos token.Position, unconditional bool) ast.Stmt {
	// Desugar the case body
	var caseBody *ast.BlockStmt
	switch body := caseStmt.Body.(type) {
	case *ast.BlockStmt:
		caseBody = d.desugarStmt(body).(*ast.BlockStmt)
	case ast.Expr:
		// Convert expression to expression statement in block
		exprStmt := &ast.ExprStmt{
			BaseNode: ast.BaseNode{Pos: body.Position()},
			Expr:     d.desugarExpr(body),
		}
		caseBody = d.wrapInBlock([]ast.Stmt{exprStmt}, body.Position())
	case ast.Stmt:
		caseBody = d.wrapInBlock([]ast.Stmt{d.desugarStmt(body)}, body.Position())
	default:
		caseBody = d.wrapInBlock([]ast.Stmt{}, pos)
	}

	// Build the condition based on the pattern type
	var condition ast.Expr

	switch pattern := caseStmt.Pattern.(type) {
	case *ast.VariantPattern:
		// A variant of an enum: test for it, then name its values at the
		// start of the case
		tests, names := d.lowerPattern(matchedExpr, pattern, pos)
		condition = d.allOf(tests, pos)
		caseBody = d.wrapInBlock(append(names, caseBody.Stmts...), caseBody.Position())
		if unconditional {
			return caseBody
		}

	case *ast.Type:
		// Type matching: __is_type(matched_expr, Type)
		condition = d.createTypeCheckCall(matchedExpr, pattern, pos)

	case ast.Expr:
		// Value matching: matched_expr == pattern
		patternExpr := d.desugarExpr(pattern)
		condition = d.createBinaryExpr(matchedExpr, "==", patternExpr, pos)
		d.typeInfo.Types[condition] = types.Bool

	default:
		// Default case or unsupported pattern - always true
		condition = d.createBoolLiteral(true, pos)
		d.typeInfo.Types[condition] = types.Bool
	}

	// Build the if statement
	ifStmt := &ast.IfStmt{
		BaseNode: ast.BaseNode{Pos: pos},
		Cond:     condition,
		Then:     caseBody,
		Else:     elseStmt,
	}

	return ifStmt
}

// createTypeCheckCall creates the test `expr is Type` for a type pattern
func (d *Desugarer) createTypeCheckCall(expr ast.Expr, typ *ast.Type, pos token.Position) ast.Expr {
	test := &ast.TypeTestExpr{
		BaseNode: ast.BaseNode{Pos: pos, Tok: typ.Tok},
		Expr:     expr,
		Type:     typ,
	}

	d.typeInfo.Types[test] = types.Bool
	return test
}

// isComplexExpr determines if an expression is complex enough to need a temporary variable
func (d *Desugarer) isComplexExpr(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.Ident, *ast.Literal:
		return false
	default:
		return true
	}
}

// hasTypeOrEnumMatching checks if any cases use type or enum matching
func (d *Desugarer) hasTypeOrEnumMatching(cases []*ast.MatchCase) bool {
	for _, c := range cases {
		if _, isType := c.Pattern.(*ast.Type); isType {
			return true
		}
		// Check for enum variant matching (CallExpr pattern)
		if _, isCall := c.Pattern.(*ast.CallExpr); isCall {
			return true
		}
	}
	return false
}

// inferTypeFromExpr infers the AST type from an expression
func (d *Desugarer) inferTypeFromExpr(expr ast.Expr, pos token.Position) *ast.Type {
	if exprType, ok := d.typeInfo.Types[expr]; ok {
		return d.typeToASTType(exprType, pos)
	}

	// Unknown: leave the declaration without an annotation
	return nil
}

package compiler

import (
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
)

// checkSupported reports language features that parse but cannot be
// compiled yet, so they fail with a clear message instead of a confusing
// error from a later stage.
func checkSupported(prog *ast.Program, errs *errors.Handler, lines map[int]string) {
	unsupported := func(node ast.Node, what, suggestion string) {
		errs.ReportErrorWithSuggestion(ErrUnsupportedFeature, what+" not supported yet", suggestion, node.Token(), lines)
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.EnumDecl:
			unsupported(d, "enums are", "Use Int constants or a struct instead")
		case *ast.FnDecl:
			checkBlockSupported(d.Body, unsupported)
		case *ast.ImplDecl:
			for _, m := range d.Methods {
				checkBlockSupported(m.Body, unsupported)
			}
		}
	}
}

func checkBlockSupported(block *ast.BlockStmt, unsupported func(ast.Node, string, string)) {
	if block == nil {
		return
	}
	for _, stmt := range block.Stmts {
		checkStmtSupported(stmt, unsupported)
	}
}

func checkStmtSupported(stmt ast.Stmt, unsupported func(ast.Node, string, string)) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		checkBlockSupported(s, unsupported)
	case *ast.IfStmt:
		checkBlockSupported(s.Then, unsupported)
		if s.Else != nil {
			checkStmtSupported(s.Else, unsupported)
		}
	case *ast.WhileStmt:
		checkBlockSupported(s.Body, unsupported)
	case *ast.ForStmt:
		checkBlockSupported(s.Body, unsupported)
	case *ast.MatchStmt:
		for _, c := range s.Cases {
			switch c.Pattern.(type) {
			case nil, *ast.Literal, *ast.UnaryExpr, *ast.Type:
				// Value patterns, type patterns and `default`
			default:
				unsupported(c, "this kind of match pattern is", "Match on literal values or on types")
			}
			if body, ok := c.Body.(*ast.BlockStmt); ok {
				checkBlockSupported(body, unsupported)
			}
		}
	}
}

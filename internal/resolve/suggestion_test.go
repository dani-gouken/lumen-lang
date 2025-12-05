package resolve

import (
	"strings"
	"testing"

	"github.com/yourname/lumen/internal/ast"
	"github.com/yourname/lumen/internal/errors"
	"github.com/yourname/lumen/internal/token"
)

func TestResolveSuggestion(t *testing.T) {
	// let count = 1;
	// let x = cont; // Typo: cont -> count

	countDecl := &ast.LetStmt{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 1}, EndPos: token.Position{Line: 1, Col: 14}}},
		Name:     "count",
		Value:    &ast.Literal{Value: "1", Kind: "int"},
	}

	contRef := &ast.Ident{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 2, Col: 9}, EndPos: token.Position{Line: 2, Col: 13}}},
		Name:     "cont",
	}
	xDecl := &ast.LetStmt{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 2, Col: 1}, EndPos: token.Position{Line: 2, Col: 14}}},
		Name:     "x",
		Value:    contRef,
	}

	prog := &ast.Program{
		Decls: []ast.Decl{
			&ast.FnDecl{
				Name: "main",
				Body: &ast.BlockStmt{
					Stmts: []ast.Stmt{countDecl, xDecl},
				},
			},
		},
	}

	errs := errors.NewHandler()
	resolver := NewResolver(errs)
	resolver.Resolve(prog)

	if !errs.HasErrors() {
		t.Fatal("Expected resolution errors, but got none")
	}

	foundSuggestion := false
	for _, diag := range errs.Diagnostics {
		if strings.Contains(diag.Message, "undefined identifier 'cont'") {
			if strings.Contains(diag.Suggestion, "Did you mean 'count'?") {
				foundSuggestion = true
			}
		}
	}

	if !foundSuggestion {
		t.Errorf("Expected suggestion 'Did you mean 'count'?', but got errors: %v", errs.Diagnostics)
	}
}

func TestResolveTypeSuggestion(t *testing.T) {
	// let x: Intger = 1; // Typo: Intger -> Int

	xDecl := &ast.LetStmt{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 1}, EndPos: token.Position{Line: 1, Col: 18}}},
		Name:     "x",
		Type:     &ast.Type{Kind: ast.TypeBasic, Name: "Intger", BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 8}, EndPos: token.Position{Line: 1, Col: 14}}}},
		Value:    &ast.Literal{Value: "1", Kind: "int"},
	}

	prog := &ast.Program{
		Decls: []ast.Decl{
			&ast.FnDecl{
				Name: "main",
				Body: &ast.BlockStmt{
					Stmts: []ast.Stmt{xDecl},
				},
			},
		},
	}

	errs := errors.NewHandler()
	resolver := NewResolver(errs)
	resolver.Resolve(prog)

	if !errs.HasErrors() {
		t.Fatal("Expected resolution errors, but got none")
	}

	foundSuggestion := false
	for _, diag := range errs.Diagnostics {
		if strings.Contains(diag.Message, "undefined type 'Intger'") {
			if strings.Contains(diag.Suggestion, "Did you mean 'Int'?") {
				foundSuggestion = true
			}
		}
	}

	if !foundSuggestion {
		t.Errorf("Expected suggestion 'Did you mean 'Int'?', but got errors: %v", errs.Diagnostics)
	}
}

func TestPatternMatchingVariableBinding(t *testing.T) {
	// match x { case y: y + 1 }
	// y should be bound as a new variable in the match case scope

	xIdent := &ast.Ident{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 7}, EndPos: token.Position{Line: 1, Col: 8}}},
		Name:     "x",
	}

	// Pattern: identifier y (should bind y)
	patternY := &ast.Ident{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 16}, EndPos: token.Position{Line: 1, Col: 17}}},
		Name:     "y",
	}

	// Body: y + 1 (should resolve y to the bound variable)
	bodyYRef := &ast.Ident{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 19}, EndPos: token.Position{Line: 1, Col: 20}}},
		Name:     "y",
	}

	matchStmt := &ast.MatchStmt{
		Cond: xIdent,
		Cases: []*ast.MatchCase{
			{
				Pattern: patternY,
				Body: &ast.BinaryExpr{
					Left:  bodyYRef,
					Op:    "+",
					Right: &ast.Literal{Value: "1", Kind: "int"},
				},
			},
		},
	}

	// Declare x in outer scope
	xDecl := &ast.LetStmt{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 1}, EndPos: token.Position{Line: 1, Col: 11}}},
		Name:     "x",
		Value:    &ast.Literal{Value: "5", Kind: "int"},
	}

	prog := &ast.Program{
		Decls: []ast.Decl{
			&ast.FnDecl{
				Name: "main",
				Body: &ast.BlockStmt{
					Stmts: []ast.Stmt{xDecl, matchStmt},
				},
			},
		},
	}

	errs := errors.NewHandler()
	resolver := NewResolver(errs)
	table := resolver.Resolve(prog)

	if errs.HasErrors() {
		t.Errorf("Expected no errors, but got: %v", errs.Diagnostics)
	}

	// Verify that y in the body resolves to the pattern-bound variable
	if sym := table.Defs[bodyYRef]; sym == nil {
		t.Error("Expected y in body to be resolved")
	} else if sym.Decl != patternY {
		t.Error("Expected y in body to resolve to the pattern binding")
	}
}

func TestPatternMatchingWildcard(t *testing.T) {
	// match x { case _: 1 }
	// _ is a wildcard and should not bind a variable

	xIdent := &ast.Ident{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 7}, EndPos: token.Position{Line: 1, Col: 8}}},
		Name:     "x",
	}

	// Pattern: wildcard _
	wildcardPattern := &ast.Ident{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 16}, EndPos: token.Position{Line: 1, Col: 17}}},
		Name:     "_",
	}

	matchStmt := &ast.MatchStmt{
		Cond: xIdent,
		Cases: []*ast.MatchCase{
			{
				Pattern: wildcardPattern,
				Body:    &ast.Literal{Value: "1", Kind: "int"},
			},
		},
	}

	xDecl := &ast.LetStmt{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 1}, EndPos: token.Position{Line: 1, Col: 11}}},
		Name:     "x",
		Value:    &ast.Literal{Value: "5", Kind: "int"},
	}

	prog := &ast.Program{
		Decls: []ast.Decl{
			&ast.FnDecl{
				Name: "main",
				Body: &ast.BlockStmt{
					Stmts: []ast.Stmt{xDecl, matchStmt},
				},
			},
		},
	}

	errs := errors.NewHandler()
	resolver := NewResolver(errs)
	resolver.Resolve(prog)

	// Should have no errors - wildcard doesn't try to redeclare _
	if errs.HasErrors() {
		t.Errorf("Expected no errors for wildcard pattern, but got: %v", errs.Diagnostics)
	}
}

func TestPatternMatchingLiteral(t *testing.T) {
	// match x { case 42: true }
	// Literal patterns don't bind variables

	xIdent := &ast.Ident{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 7}, EndPos: token.Position{Line: 1, Col: 8}}},
		Name:     "x",
	}

	matchStmt := &ast.MatchStmt{
		Cond: xIdent,
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.Literal{Value: "42", Kind: "int"},
				Body:    &ast.Literal{Value: "true", Kind: "bool"},
			},
		},
	}

	xDecl := &ast.LetStmt{
		BaseNode: ast.BaseNode{Tok: token.Token{Pos: token.Position{Line: 1, Col: 1}, EndPos: token.Position{Line: 1, Col: 11}}},
		Name:     "x",
		Value:    &ast.Literal{Value: "5", Kind: "int"},
	}

	prog := &ast.Program{
		Decls: []ast.Decl{
			&ast.FnDecl{
				Name: "main",
				Body: &ast.BlockStmt{
					Stmts: []ast.Stmt{xDecl, matchStmt},
				},
			},
		},
	}

	errs := errors.NewHandler()
	resolver := NewResolver(errs)
	resolver.Resolve(prog)

	if errs.HasErrors() {
		t.Errorf("Expected no errors for literal pattern, but got: %v", errs.Diagnostics)
	}
}

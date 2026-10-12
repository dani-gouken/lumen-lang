package parser

import "github.com/dani-gouken/lumen/internal/ast"

// classifyPatterns turns a match pattern that is the bare name of a struct
// or trait into a type pattern. While parsing, `case Point:` cannot be told
// from a name used as a value; once the whole file is parsed the
// declarations are known.
func classifyPatterns(prog *ast.Program) {
	typeNames := make(map[string]bool)
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			typeNames[d.Name] = true
		case *ast.TraitDecl:
			typeNames[d.Name] = true
		}
	}

	var visitBlock func(block *ast.BlockStmt)
	visitStmt := func(stmt ast.Stmt) {}
	visitStmt = func(stmt ast.Stmt) {
		switch s := stmt.(type) {
		case *ast.BlockStmt:
			visitBlock(s)
		case *ast.IfStmt:
			visitBlock(s.Then)
			if s.Else != nil {
				visitStmt(s.Else)
			}
		case *ast.WhileStmt:
			visitBlock(s.Body)
		case *ast.ForStmt:
			visitBlock(s.Body)
		case *ast.MatchStmt:
			for _, c := range s.Cases {
				if ident, ok := c.Pattern.(*ast.Ident); ok && typeNames[ident.Name] {
					c.Pattern = &ast.Type{
						BaseNode: ident.BaseNode,
						Kind:     ast.TypeBasic,
						Name:     ident.Name,
						Generics: ident.TypeArgs,
					}
				}
				if body, ok := c.Body.(*ast.BlockStmt); ok {
					visitBlock(body)
				}
			}
		}
	}
	visitBlock = func(block *ast.BlockStmt) {
		if block == nil {
			return
		}
		for _, stmt := range block.Stmts {
			visitStmt(stmt)
		}
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			visitBlock(d.Body)
		case *ast.ImplDecl:
			for _, m := range d.Methods {
				visitBlock(m.Body)
			}
		}
	}
}

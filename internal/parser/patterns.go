package parser

import "github.com/dani-gouken/lumen/internal/ast"

// classifyPatterns turns a match pattern that is the bare name of a struct,
// an enum or a trait into a type pattern. While parsing, `case Point:` cannot be told
// from a name used as a value; once the whole file is parsed the
// declarations are known.
func classifyPatterns(prog *ast.Program) {
	typeNames := make(map[string]bool)
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			typeNames[d.Name] = true
		case *ast.EnumDecl:
			typeNames[d.Name] = true
		case *ast.TraitDecl:
			typeNames[d.Name] = true
		}
	}

	classify := func(pattern ast.Node) ast.Node {
		if ident, ok := pattern.(*ast.Ident); ok && typeNames[ident.Name] {
			return &ast.Type{
				BaseNode: ident.BaseNode,
				Kind:     ast.TypeBasic,
				Name:     ident.Name,
				Generics: ident.TypeArgs,
			}
		}
		return pattern
	}
	visitBlock := func(block *ast.BlockStmt) {
		if block == nil {
			return
		}
		ast.Walk(block, func(ast.Expr) {}, func(stmt ast.Stmt) {
			match, ok := stmt.(*ast.MatchStmt)
			if !ok {
				return
			}
			for _, c := range match.Cases {
				c.Pattern = classify(c.Pattern)
				for i, alt := range c.Alternatives {
					c.Alternatives[i] = classify(alt)
				}
			}
		})
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

// variantPattern turns the expression parsed for a match case into a
// pattern on an enum variant, when it has the shape of one:
//
//	Shape.Empty            a name with a dot
//	Shape.Rect(w, h)       the same, with a pattern for each value
//
// As values these expressions cannot be match patterns anyway (only
// literals can), so nothing is lost by reading them this way. Whether the
// name really is a variant is for the later stages to say. Anything else
// is returned unchanged.
//
// top is false for a pattern inside another, where a bare name is a
// variable to bind rather than something to compare with.
func (p *Parser) variantPattern(expr ast.Expr, top bool) ast.Node {
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if isPath(e) {
			return &ast.VariantPattern{BaseNode: pathBase(e), Target: e}
		}
	case *ast.CallExpr:
		target, ok := e.Func.(*ast.MemberExpr)
		if !ok || !isPath(target) {
			break
		}
		pattern := &ast.VariantPattern{BaseNode: pathBase(target), Target: target, HasArgs: true}
		for _, arg := range e.Args {
			pattern.Args = append(pattern.Args, p.subPattern(arg))
		}
		return pattern
	case *ast.Ident:
		if !top && len(e.TypeArgs) == 0 {
			return &ast.BindingPattern{BaseNode: e.BaseNode, Name: e.Name}
		}
	}
	return expr
}

// subPattern reads an argument of a variant pattern: a name to bind, a
// literal to compare with, or a pattern on another enum value.
func (p *Parser) subPattern(expr ast.Expr) ast.Node {
	switch e := expr.(type) {
	case *ast.Literal:
		return e
	case *ast.UnaryExpr:
		if lit, ok := e.Expr.(*ast.Literal); ok && e.Op == "-" && (lit.Kind == "int" || lit.Kind == "float") {
			return e
		}
	case *ast.Ident, *ast.MemberExpr, *ast.CallExpr:
		if pattern := p.variantPattern(expr, false); pattern != ast.Node(expr) {
			return pattern
		}
	}

	p.errs.ReportErrorWithSuggestion(ErrUnexpectedToken,
		"this cannot be matched against a value of a variant",
		"Use a name to give the value a name, '_' to ignore it, a literal, or the pattern of another variant",
		expr.Token(), p.sourceLines)
	return &ast.BindingPattern{BaseNode: ast.BaseNode{Pos: expr.Position(), Tok: expr.Token()}, Name: "_"}
}

// isPath reports whether an expression is names joined by dots.
func isPath(e *ast.MemberExpr) bool {
	if len(e.TypeArgs) > 0 {
		return false
	}
	switch inner := e.Expr.(type) {
	case *ast.Ident:
		return true
	case *ast.MemberExpr:
		return isPath(inner)
	}
	return false
}

// pathBase positions a pattern at the start of its path, with a token that
// spans all of it.
func pathBase(e *ast.MemberExpr) ast.BaseNode {
	first := ast.Expr(e)
	text := ""
	for {
		member, ok := first.(*ast.MemberExpr)
		if !ok {
			break
		}
		text = "." + member.Name + text
		first = member.Expr
	}
	tok := first.Token()
	tok.Literal += text
	tok.EndPos = e.Tok.EndPos
	tok.EndPos.Col += len(e.Name)
	return ast.BaseNode{Pos: tok.Pos, Tok: tok}
}

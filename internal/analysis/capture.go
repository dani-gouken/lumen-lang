package analysis

import (
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/resolve"
)

// buildCaptureGraph traverses the AST and identifies captures
func (a *Analyzer) buildCaptureGraph(prog *ast.Program) {
	for _, decl := range prog.Decls {
		a.visitDecl(decl)
	}
}

func (a *Analyzer) visitDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FnDecl:
		if d.Body != nil {
			a.visitFunction(d, d.Body, d.Params)
		}
	case *ast.StructDecl, *ast.EnumDecl, *ast.TraitDecl:
		// No closure analysis needed for type declarations
	case *ast.ImplDecl:
		for _, method := range d.Methods {
			if method.Body != nil {
				a.visitFunction(method, method.Body, method.Params)
			}
		}
	}
}

func (a *Analyzer) visitFunction(fn ast.Node, body *ast.BlockStmt, params []*ast.Param) {
	// Create new scope for this function
	scope := &functionScope{
		function: fn,
		locals:   make(map[string]*resolve.Symbol),
		depth:    len(a.scopeStack),
	}

	// Register parameters as locals
	fnScope := a.resolveTable.Scopes[fn]
	if fnScope != nil {
		for _, param := range params {
			if sym := fnScope.Lookup(param.Name); sym != nil {
				scope.locals[param.Name] = sym
			}
		}
	}

	// Push function onto stack
	a.funcStack = append(a.funcStack, fn)
	a.scopeStack = append(a.scopeStack, scope)

	// Create capture entry
	a.info.Captures[fn] = &FunctionCaptures{
		Function:     fn,
		CapturedVars: make([]*CapturedVar, 0),
		Depth:        scope.depth,
		Parent:       a.getParentFunction(),
	}

	// Visit body
	a.visitStmt(body, scope)

	// Pop function from stack
	a.funcStack = a.funcStack[:len(a.funcStack)-1]
	a.scopeStack = a.scopeStack[:len(a.scopeStack)-1]
}

func (a *Analyzer) visitStmt(stmt ast.Stmt, scope *functionScope) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.BlockStmt:
		for _, st := range s.Stmts {
			a.visitStmt(st, scope)
		}
	case *ast.LetStmt:
		// Register local variable
		stmtScope := a.resolveTable.Scopes[scope.function]
		if stmtScope != nil {
			if sym := stmtScope.Lookup(s.Name); sym != nil {
				scope.locals[s.Name] = sym
			}
		}
		if s.Value != nil {
			a.visitExpr(s.Value, scope)
		}
	case *ast.ReturnStmt:
		if s.Value != nil {
			a.visitExpr(s.Value, scope)
		}
	case *ast.IfStmt:
		a.visitExpr(s.Cond, scope)
		a.visitStmt(s.Then, scope)
		if s.Else != nil {
			a.visitStmt(s.Else, scope)
		}
	case *ast.WhileStmt:
		a.visitExpr(s.Cond, scope)
		a.visitStmt(s.Body, scope)
	case *ast.ForStmt:
		// For-in loop: for x in iterable { ... }
		// Loop variable is local
		a.visitExpr(s.Iterable, scope)
		a.visitStmt(s.Body, scope)
	case *ast.MatchStmt:
		a.visitExpr(s.Cond, scope)
		for _, matchCase := range s.Cases {
			// Pattern variables are local to the arm
			if body, ok := matchCase.Body.(*ast.BlockStmt); ok {
				a.visitStmt(body, scope)
			}
		}
	case *ast.ExprStmt:
		a.visitExpr(s.Expr, scope)
	case *ast.BreakStmt, *ast.ContinueStmt:
		// No expressions to visit
	}
}

func (a *Analyzer) visitExpr(expr ast.Expr, scope *functionScope) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.Ident:
		a.checkCapture(e, scope)
	case *ast.BinaryExpr:
		a.visitExpr(e.Left, scope)
		a.visitExpr(e.Right, scope)
	case *ast.UnaryExpr:
		a.visitExpr(e.Expr, scope)
	case *ast.CallExpr:
		a.visitExpr(e.Func, scope)
		for _, arg := range e.Args {
			a.visitExpr(arg, scope)
		}
	case *ast.IndexExpr:
		a.visitExpr(e.Expr, scope)
		a.visitExpr(e.Index, scope)
	case *ast.MemberExpr:
		a.visitExpr(e.Expr, scope)
	case *ast.ArrayLiteralExpr:
		for _, elem := range e.Elements {
			a.visitExpr(elem, scope)
		}
	case *ast.StructInitExpr:
		for _, fieldValue := range e.Fields {
			a.visitExpr(fieldValue, scope)
		}
	case *ast.FnExpr:
		// Nested lambda - recurse
		a.visitFunction(e, e.Body, e.Params)
	case *ast.Literal:
		// No variables to check
	}
}

// checkCapture determines if an identifier is a captured variable
func (a *Analyzer) checkCapture(ident *ast.Ident, scope *functionScope) {
	// Check if this is a local variable in current scope
	if _, isLocal := scope.locals[ident.Name]; isLocal {
		return // Not a capture
	}

	// Look up in resolve table
	sym := a.resolveTable.Defs[ident]
	if sym == nil {
		return // Not resolved (error should be reported by resolver)
	}

	// Only capture variables (not functions, types, etc.)
	if sym.Kind != resolve.SymbolVar {
		return
	}

	// Check if it's defined in a parent scope
	scopeDistance := 0
	fromUpvalue := false

	// Search through parent scopes
	for i := len(a.scopeStack) - 2; i >= 0; i-- {
		parentScope := a.scopeStack[i]
		scopeDistance++

		if _, found := parentScope.locals[ident.Name]; found {
			// Found in ancestor scope - this is a capture!
			capturedVar := &CapturedVar{
				Symbol:        sym,
				Name:          ident.Name,
				ScopeDistance: scopeDistance,
				FromUpvalue:   fromUpvalue,
				Decl:          sym.Decl,
			}

			// Check if we already captured this variable
			captures := a.info.Captures[scope.function]
			alreadyCaptured := false
			for _, cv := range captures.CapturedVars {
				if cv.Symbol == sym {
					alreadyCaptured = true
					break
				}
			}

			// Add to current function's captures if not already captured
			if !alreadyCaptured {
				a.info.AddCapture(scope.function, capturedVar)
			}

			return
		}

		// Check if parent already captures this variable
		parentCaptures := a.info.Captures[parentScope.function]
		for _, cv := range parentCaptures.CapturedVars {
			if cv.Symbol == sym {
				fromUpvalue = true
				break
			}
		}
	}
}

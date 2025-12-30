package monomorph

import (
	"github.com/dani-gouken/lumen/internal/ast"
)

// rewriteCallSites rewrites all call sites to use monomorphized function names.
func (m *Monomorphizer) rewriteCallSites() {
	for _, decl := range m.prog.Decls {
		m.rewriteDeclCallSites(decl)
	}
}

func (m *Monomorphizer) rewriteDeclCallSites(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FnDecl:
		if d.Body != nil {
			m.rewriteStmtCallSites(d.Body)
		}
	case *ast.ImplDecl:
		for _, method := range d.Methods {
			if method.Body != nil {
				m.rewriteStmtCallSites(method.Body)
			}
		}
	}
}

func (m *Monomorphizer) rewriteStmtCallSites(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		for _, st := range s.Stmts {
			m.rewriteStmtCallSites(st)
		}
	case *ast.LetStmt:
		if s.Value != nil {
			m.rewriteExprCallSites(s.Value)
		}
	case *ast.ReturnStmt:
		if s.Value != nil {
			m.rewriteExprCallSites(s.Value)
		}
	case *ast.IfStmt:
		m.rewriteExprCallSites(s.Cond)
		m.rewriteStmtCallSites(s.Then)
		if s.Else != nil {
			m.rewriteStmtCallSites(s.Else)
		}
	case *ast.WhileStmt:
		m.rewriteExprCallSites(s.Cond)
		m.rewriteStmtCallSites(s.Body)
	case *ast.ForStmt:
		m.rewriteExprCallSites(s.Iterable)
		m.rewriteStmtCallSites(s.Body)
	case *ast.ExprStmt:
		m.rewriteExprCallSites(s.Expr)
	}
}

func (m *Monomorphizer) rewriteExprCallSites(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.CallExpr:
		// Check if this is a call to a generic function
		if ident, ok := e.Func.(*ast.Ident); ok {
			if _, isGeneric := m.generics[ident.Name]; isGeneric {
				// Get the type arguments
				typeArgs := m.extractTypeArgsFromIdent(ident, m.generics[ident.Name])
				if typeArgs != nil {
					key := MakeInstantiationKey(ident.Name, typeArgs)
					// Rewrite the function name to the mangled name
					ident.Name = m.mangleName(key)
					// Remove type arguments
					ident.TypeArgs = nil
				}
			}
		}

		m.rewriteExprCallSites(e.Func)
		for _, arg := range e.Args {
			m.rewriteExprCallSites(arg)
		}

	case *ast.BinaryExpr:
		m.rewriteExprCallSites(e.Left)
		m.rewriteExprCallSites(e.Right)
	case *ast.UnaryExpr:
		m.rewriteExprCallSites(e.Expr)
	case *ast.MemberExpr:
		m.rewriteExprCallSites(e.Expr)
	case *ast.IndexExpr:
		m.rewriteExprCallSites(e.Expr)
		m.rewriteExprCallSites(e.Index)
	case *ast.ArrayLiteralExpr:
		for _, elem := range e.Elements {
			m.rewriteExprCallSites(elem)
		}
	case *ast.StructInitExpr:
		for _, fieldExpr := range e.Fields {
			m.rewriteExprCallSites(fieldExpr)
		}
		// Also handle generic struct instantiation
		if len(e.TypeArgs) > 0 {
			if _, isGeneric := m.generics[e.Name]; isGeneric {
				typeArgs := m.convertASTTypesToTypes(e.TypeArgs)
				key := MakeInstantiationKey(e.Name, typeArgs)
				// Rewrite the struct name to the mangled name
				e.Name = m.mangleName(key)
				// Remove type arguments
				e.TypeArgs = nil
			}
		}
	}
}

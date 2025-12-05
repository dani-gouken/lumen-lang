package resolve

import (
	"fmt"

	"github.com/yourname/lumen/internal/ast"
	"github.com/yourname/lumen/internal/errors"
	"github.com/yourname/lumen/internal/token"
)

type Resolver struct {
	table       *Table
	current     *Scope
	errs        *errors.Handler
	sourceLines map[int]string // Source lines for error context
}

func NewResolver(errs *errors.Handler) *Resolver {
	return &Resolver{
		table:       NewTable(),
		current:     NewScope(nil), // Global scope
		errs:        errs,
		sourceLines: make(map[int]string),
	}
}

// SetSourceLines sets the source lines for error reporting.
func (r *Resolver) SetSourceLines(lines map[int]string) {
	r.sourceLines = lines
}

func (r *Resolver) Resolve(prog *ast.Program) *Table {
	// Pass 1: Top-level declarations
	r.resolveTopLevel(prog)

	// Pass 2: Function bodies and other executable code
	r.resolveBody(prog)

	return r.table
}

func (r *Resolver) resolveTopLevel(prog *ast.Program) {
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			r.declare(d.Name, SymbolFunc, d)
		case *ast.StructDecl:
			r.declare(d.Name, SymbolStruct, d)
		case *ast.EnumDecl:
			r.declare(d.Name, SymbolEnum, d)
		case *ast.TraitDecl:
			r.declare(d.Name, SymbolTrait, d)
			// ImplDecl doesn't introduce a new name in the global scope in the same way,
			// but we might need to resolve the struct and trait names it refers to.
			// For now, we just register the named entities.
		}
	}
}

func (r *Resolver) declare(name string, kind SymbolKind, node ast.Node) {
	sym := &Symbol{
		Name: name,
		Kind: kind,
		Decl: node,
	}
	if err := r.current.Insert(name, sym); err != nil {
		if baseNode, ok := node.(interface{ Token() token.Token }); ok {
			r.errs.ReportErrorFromToken("E1002", err.Error(), baseNode.Token(), r.sourceLines)
		} else {
			r.errs.ReportError(node.Position(), "E1002", err.Error())
		}
	}
}

func (r *Resolver) resolveBody(prog *ast.Program) {
	for _, decl := range prog.Decls {
		r.visitDecl(decl)
	}
}

func (r *Resolver) visitDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FnDecl:
		r.visitFnDecl(d)
	case *ast.StructDecl:
		// Struct fields types need resolution? Yes, but types are resolved in type checking mostly.
		// However, if we had type aliases or constants, we'd resolve them here.
		// For now, we might want to check if field types refer to valid types.
		// But the spec says "Name Resolution links identifiers to declarations".
		// So we should resolve type names in struct fields too.
		r.visitStructDecl(d)
	case *ast.EnumDecl:
		r.visitEnumDecl(d)
	case *ast.TraitDecl:
		r.visitTraitDecl(d)
	case *ast.ImplDecl:
		r.visitImplDecl(d)
	}
}

func (r *Resolver) visitFnDecl(d *ast.FnDecl) {
	// Create new scope for function
	r.pushScope(d)
	defer r.popScope()

	// Register type parameters
	for _, tp := range d.TypeParams {
		// Type parameters are declared in the function scope.
		// Since we don't have a specific AST node for the type parameter name,
		// we use the FnDecl node for error reporting context.
		sym := &Symbol{Name: tp, Kind: SymbolTypeParam, Decl: d}
		r.current.Insert(tp, sym)
	}

	// Register parameters
	for _, param := range d.Params {
		r.visitType(param.Type)
		r.declare(param.Name, SymbolVar, param)
	}

	// Resolve return type
	if d.ReturnType != nil {
		r.visitType(d.ReturnType)
	}

	// Resolve body
	if d.Body != nil {
		r.visitBlockStmt(d.Body)
	}
}

func (r *Resolver) visitStructDecl(d *ast.StructDecl) {
	// Resolve field types
	for _, f := range d.Fields {
		r.visitType(f.Type)
	}
}

func (r *Resolver) visitEnumDecl(d *ast.EnumDecl) {
	for _, v := range d.Variants {
		for _, t := range v.Values {
			r.visitType(t)
		}
	}
}

func (r *Resolver) visitTraitDecl(d *ast.TraitDecl) {
	// Trait methods
	for _, m := range d.Methods {
		r.visitFnDecl(m)
	}
}

func (r *Resolver) visitImplDecl(d *ast.ImplDecl) {
	// Verify that the struct and trait names exist in the scope.
	// Note: Since these are stored as strings in the AST, we cannot link them
	// in the resolution table (which requires *ast.Ident).
	if sym := r.current.Lookup(d.StructName); sym == nil {
		// Check if it's a built-in type (e.g. impl MyTrait for Int)
		if !isBuiltinType(d.StructName) {
			if tok := d.Token(); tok.Type != 0 {
				r.errs.ReportErrorFromToken("E1001", fmt.Sprintf("undefined type '%s'", d.StructName), tok, r.sourceLines)
			} else {
				r.errs.ReportError(d.Position(), "E1001", fmt.Sprintf("undefined type '%s'", d.StructName))
			}
		}
	}

	if d.TraitName != "" {
		if sym := r.current.Lookup(d.TraitName); sym == nil {
			if tok := d.Token(); tok.Type != 0 {
				r.errs.ReportErrorFromToken("E1001", fmt.Sprintf("undefined trait '%s'", d.TraitName), tok, r.sourceLines)
			} else {
				r.errs.ReportError(d.Position(), "E1001", fmt.Sprintf("undefined trait '%s'", d.TraitName))
			}
		}
	}

	for _, m := range d.Methods {
		r.visitFnDecl(m)
	}
}

func (r *Resolver) visitBlockStmt(b *ast.BlockStmt) {
	r.pushScope(b)
	defer r.popScope()

	for _, stmt := range b.Stmts {
		r.visitStmt(stmt)
	}
}

func (r *Resolver) visitStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		r.visitBlockStmt(s)
	case *ast.LetStmt:
		r.visitLetStmt(s)
	case *ast.ReturnStmt:
		if s.Value != nil {
			r.visitExpr(s.Value)
		}
	case *ast.IfStmt:
		r.visitExpr(s.Cond)
		r.visitBlockStmt(s.Then)
		if s.Else != nil {
			r.visitStmt(s.Else)
		}
	case *ast.WhileStmt:
		r.visitExpr(s.Cond)
		r.visitBlockStmt(s.Body)
	case *ast.ForStmt:
		r.visitForStmt(s)
	case *ast.MatchStmt:
		r.visitMatchStmt(s)
	case *ast.ExprStmt:
		r.visitExpr(s.Expr)
	case *ast.BreakStmt, *ast.ContinueStmt:
		// Nothing to resolve
	}
}

func (r *Resolver) visitLetStmt(s *ast.LetStmt) {
	if s.Type != nil {
		r.visitType(s.Type)
	}
	if s.Value != nil {
		r.visitExpr(s.Value)
	}
	// Declare variable AFTER resolving initializer
	r.declare(s.Name, SymbolVar, s)
}

func (r *Resolver) visitForStmt(s *ast.ForStmt) {
	r.visitExpr(s.Iterable)

	// For loop creates a scope for the loop variable
	r.pushScope(s)
	defer r.popScope()

	// Declare loop variable
	// We don't have a separate node for the variable name, so we use the ForStmt
	sym := &Symbol{Name: s.Var, Kind: SymbolVar, Decl: s}
	if err := r.current.Insert(s.Var, sym); err != nil {
		if tok := s.Token(); tok.Type != 0 {
			r.errs.ReportErrorFromToken("E1002", err.Error(), tok, r.sourceLines)
		} else {
			r.errs.ReportError(s.Position(), "E1002", err.Error())
		}
	}

	r.visitBlockStmt(s.Body)
}

func (r *Resolver) visitMatchStmt(s *ast.MatchStmt) {
	r.visitExpr(s.Cond)
	for _, c := range s.Cases {
		r.visitMatchCase(c)
	}
}

func (r *Resolver) visitMatchCase(c *ast.MatchCase) {
	// Match case creates a scope
	r.pushScope(c)
	defer r.popScope()

	// TODO: Implement pattern matching variable binding.
	// Currently, patterns are visited as expressions, which means identifiers
	// are looked up rather than declared.
	if expr, ok := c.Pattern.(ast.Expr); ok {
		r.visitExpr(expr)
	}

	// Body can be either BlockStmt or Expr
	switch body := c.Body.(type) {
	case *ast.BlockStmt:
		r.visitBlockStmt(body)
	case ast.Expr:
		r.visitExpr(body)
	}
}

func (r *Resolver) visitExpr(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		r.visitExpr(e.Left)
		r.visitExpr(e.Right)
	case *ast.UnaryExpr:
		r.visitExpr(e.Expr)
	case *ast.CallExpr:
		r.visitExpr(e.Func)
		for _, arg := range e.Args {
			r.visitExpr(arg)
		}
	case *ast.MemberExpr:
		r.visitExpr(e.Expr)
		// e.Name is a property name, not a variable, so we don't resolve it here.
	case *ast.IndexExpr:
		r.visitExpr(e.Expr)
		r.visitExpr(e.Index)
	case *ast.StructInitExpr:
		// Resolve struct name? It is a string in AST.
		// Fields
		for _, v := range e.Fields {
			r.visitExpr(v)
		}
	case *ast.Ident:
		r.resolveIdent(e)
	case *ast.Literal:
		// Nothing to resolve
	}
}

func (r *Resolver) resolveIdent(id *ast.Ident) {
	sym := r.current.Lookup(id.Name)
	if sym == nil {
		if tok := id.Token(); tok.Type != 0 {
			r.errs.ReportErrorFromToken("E1001", fmt.Sprintf("undefined identifier '%s'", id.Name), tok, r.sourceLines)
		} else {
			r.errs.ReportError(id.Position(), "E1001", fmt.Sprintf("undefined identifier '%s'", id.Name))
		}
		return
	}
	r.table.Defs[id] = sym
}

func (r *Resolver) visitType(t *ast.Type) {
	if t == nil {
		return
	}
	switch t.Kind {
	case ast.TypeBasic:
		// Check if the type exists in the current scope.
		sym := r.current.Lookup(t.Name)
		if sym == nil {
			// Is it a built-in type?
			if !isBuiltinType(t.Name) {
				if tok := t.Token(); tok.Type != 0 {
					r.errs.ReportErrorFromToken("E1001", fmt.Sprintf("undefined type '%s'", t.Name), tok, r.sourceLines)
				} else {
					r.errs.ReportError(t.Position(), "E1001", fmt.Sprintf("undefined type '%s'", t.Name))
				}
			}
		}
		for _, g := range t.Generics {
			r.visitType(g)
		}
	case ast.TypeFunction:
		for _, p := range t.ParamTypes {
			r.visitType(p)
		}
		r.visitType(t.ReturnType)
	}
}

func (r *Resolver) pushScope(node ast.Node) {
	scope := NewScope(r.current)
	r.table.Scopes[node] = scope
	r.current = scope
}

func (r *Resolver) popScope() {
	r.current = r.current.Parent
}

func isBuiltinType(name string) bool {
	switch name {
	case "Int", "Float", "String", "Bool", "Void", "Any":
		return true
	}
	return false
}

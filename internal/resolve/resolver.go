package resolve

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
)

// Error codes for name resolution
const (
	ErrUndefined     = "E1001" // Undefined identifier, type, or trait
	ErrRedeclaration = "E1002" // Redeclaration of a name in the same scope
)

type Resolver struct {
	table       *Table
	current     *Scope
	errs        *errors.Handler
	sourceLines map[int]string // Source lines for error context
}

func NewResolver(errs *errors.Handler) *Resolver {
	r := &Resolver{
		table:       NewTable(),
		current:     NewScope(nil), // Global scope
		errs:        errs,
		sourceLines: make(map[int]string),
	}

	// Host-provided functions live in the global scope
	for _, fn := range Builtins() {
		r.current.Insert(fn.Name, &Symbol{Name: fn.Name, Kind: SymbolFunc, Decl: fn})
	}
	return r
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
		r.errs.ReportErrorFromToken(ErrRedeclaration, err.Error(), node.Token(), r.sourceLines)
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
	// Type parameters are visible in the field types
	r.pushScope(d)
	defer r.popScope()
	for _, tp := range d.TypeParams {
		r.current.Insert(tp, &Symbol{Name: tp, Kind: SymbolTypeParam, Decl: d})
	}

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
			suggestion := r.findSimilarName(d.StructName)
			msg := fmt.Sprintf("undefined type '%s'", d.StructName)
			if suggestion != "" {
				r.errs.ReportErrorWithSuggestion(ErrUndefined, msg, fmt.Sprintf("Did you mean '%s'?", suggestion), d.Token(), r.sourceLines)
			} else {
				r.errs.ReportErrorFromToken(ErrUndefined, msg, d.Token(), r.sourceLines)
			}
		}
	}

	if d.TraitName != "" {
		if sym := r.current.Lookup(d.TraitName); sym == nil {
			suggestion := r.findSimilarName(d.TraitName)
			msg := fmt.Sprintf("undefined trait '%s'", d.TraitName)
			if suggestion != "" {
				r.errs.ReportErrorWithSuggestion(ErrUndefined, msg, fmt.Sprintf("Did you mean '%s'?", suggestion), d.Token(), r.sourceLines)
			} else {
				r.errs.ReportErrorFromToken(ErrUndefined, msg, d.Token(), r.sourceLines)
			}
		}
	}

	for _, m := range d.Methods {
		r.visitMethodDecl(d, m)
	}
}

// visitMethodDecl resolves an impl method, which has an implicit `self`.
func (r *Resolver) visitMethodDecl(impl *ast.ImplDecl, m *ast.FnDecl) {
	r.pushScope(impl)
	defer r.popScope()

	r.current.Insert("self", &Symbol{Name: "self", Kind: SymbolVar, Decl: impl})
	for _, tp := range impl.TypeParams {
		r.current.Insert(tp, &Symbol{Name: tp, Kind: SymbolTypeParam, Decl: impl})
	}
	r.visitFnDecl(m)
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
		r.errs.ReportErrorFromToken(ErrRedeclaration, err.Error(), s.Token(), r.sourceLines)
	}
	if s.Index != nil {
		r.declare(s.Index.Name, SymbolVar, s.Index)
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

	// Visit pattern - this handles pattern matching variable binding
	r.visitPattern(c.Pattern)

	// Body can be either BlockStmt or Expr
	switch body := c.Body.(type) {
	case *ast.BlockStmt:
		r.visitBlockStmt(body)
	case ast.Expr:
		r.visitExpr(body)
	}
}

// visitPattern handles pattern matching, including variable binding.
// Currently supports:
// - Literal patterns (e.g., 1, "hello")
// - Identifier patterns (e.g., x - binds a new variable)
// - Enum variant patterns (e.g., Some(x) - not fully implemented)
// - Struct patterns (e.g., Point { x, y } - not fully implemented)
func (r *Resolver) visitPattern(pattern ast.Node) {
	switch p := pattern.(type) {
	case *ast.Ident:
		// Special case: underscore is a wildcard, not a binding
		if p.Name == "_" {
			return
		}
		// This is a variable binding pattern - declare a new variable
		// instead of looking it up
		r.declare(p.Name, SymbolVar, p)

	case *ast.Literal:
		// Literal patterns don't bind variables, just match values
		// Nothing to resolve

	case *ast.CallExpr:
		// This could be an enum variant pattern like Some(x)
		// The function name should be resolved (it's the variant name)
		r.visitExpr(p.Func)
		// Arguments could be patterns that bind variables
		for _, arg := range p.Args {
			r.visitPattern(arg)
		}

	case *ast.StructInitExpr:
		// This could be a struct pattern like Point { x, y }
		// Field values could be patterns that bind variables
		for _, field := range p.Fields {
			r.visitPattern(field)
		}

	case ast.Expr:
		// Fallback: treat as expression (for backwards compatibility)
		// This handles other expression types that might appear in patterns
		r.visitExpr(p)

	default:
		// Unknown pattern type - ignore
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
		for _, arg := range e.TypeArgs {
			r.visitType(arg)
		}
	case *ast.IndexExpr:
		r.visitExpr(e.Expr)
		r.visitExpr(e.Index)
	case *ast.StructInitExpr:
		// Resolve struct name? It is a string in AST.
		// Fields
		for _, v := range e.Fields {
			r.visitExpr(v)
		}
	case *ast.ArrayLiteralExpr:
		for _, elem := range e.Elements {
			r.visitExpr(elem)
		}
	case *ast.FnExpr:
		r.visitFnExpr(e)
	case *ast.CastExpr:
		r.visitExpr(e.Expr)
		r.visitType(e.Type)
	case *ast.TypeTestExpr:
		r.visitExpr(e.Expr)
		r.visitType(e.Type)
	case *ast.Ident:
		r.resolveIdent(e)
	case *ast.Literal:
		// Nothing to resolve
	}
}

func (r *Resolver) visitFnExpr(e *ast.FnExpr) {
	r.pushScope(e)
	defer r.popScope()

	for _, param := range e.Params {
		r.visitType(param.Type)
		r.declare(param.Name, SymbolVar, param)
	}
	if e.ReturnType != nil {
		r.visitType(e.ReturnType)
	}
	if e.Body != nil {
		r.visitBlockStmt(e.Body)
	}
}

func (r *Resolver) resolveIdent(id *ast.Ident) {
	sym := r.current.Lookup(id.Name)
	if sym == nil {
		suggestion := r.findSimilarName(id.Name)
		msg := fmt.Sprintf("undefined identifier '%s'", id.Name)
		if suggestion != "" {
			r.errs.ReportErrorWithSuggestion(ErrUndefined, msg, fmt.Sprintf("Did you mean '%s'?", suggestion), id.Token(), r.sourceLines)
		} else {
			r.errs.ReportErrorFromToken(ErrUndefined, msg, id.Token(), r.sourceLines)
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
				suggestion := r.findSimilarName(t.Name)
				msg := fmt.Sprintf("undefined type '%s'", t.Name)
				if suggestion != "" {
					r.errs.ReportErrorWithSuggestion(ErrUndefined, msg, fmt.Sprintf("Did you mean '%s'?", suggestion), t.Token(), r.sourceLines)
				} else {
					r.errs.ReportErrorFromToken(ErrUndefined, msg, t.Token(), r.sourceLines)
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
	case ast.TypeArray, ast.TypeOption, ast.TypeVariadic:
		r.visitType(t.ElemType)
	case ast.TypeUnion:
		for _, u := range t.UnionTypes {
			r.visitType(u)
		}
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
	case "Int", "Float", "String", "Bool", "Void", "Any", "Self":
		return true
	}
	return false
}

// findSimilarName finds a similar name in scope using Levenshtein distance.
func (r *Resolver) findSimilarName(target string) string {
	bestDist := 1000
	bestName := ""

	// Walk scopes
	curr := r.current
	seen := make(map[string]bool)

	for curr != nil {
		for name := range curr.Symbols {
			if seen[name] {
				continue
			}
			seen[name] = true
			dist := levenshtein(target, name)
			if dist < bestDist {
				bestDist = dist
				bestName = name
			}
		}
		curr = curr.Parent
	}

	// Check built-ins too
	builtins := []string{"Int", "Float", "String", "Bool", "Void", "Any"}
	for _, name := range builtins {
		dist := levenshtein(target, name)
		if dist < bestDist {
			bestDist = dist
			bestName = name
		}
	}

	// Threshold: distance <= 3 or distance is less than half the target length
	if bestDist <= 3 || (bestDist < len(target)/2 && bestDist < 5) {
		return bestName
	}

	return ""
}

// levenshtein computes the Levenshtein distance between two strings.
func levenshtein(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	n, m := len(r1), len(r2)

	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	row := make([]int, m+1)
	for i := 0; i <= m; i++ {
		row[i] = i
	}

	for i := 1; i <= n; i++ {
		prev := i
		var current int
		for j := 1; j <= m; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			current = min(min(row[j]+1, prev+1), row[j-1]+cost)
			row[j-1] = prev
			prev = current
		}
		row[m] = prev
	}

	return row[m]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

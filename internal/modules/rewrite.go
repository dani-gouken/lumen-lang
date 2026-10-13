package modules

import (
	"sort"
	"strings"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/token"
)

// rewriter replaces, in one module, every name that refers to a top-level
// declaration by that declaration's program-wide name: its own
// declarations, names brought in by `import m.{name}`, and members of
// imported modules (`body.new`, `body.Body`).
//
// It follows the scoping of the language as far as needed to tell such a
// name from a local one: a variable or parameter hides a function and a
// module of the same name, a type parameter hides a type.
type rewriter struct {
	*loader
	m *module

	locals     []map[string]bool
	typeParams []map[string]bool
}

func (r *rewriter) rewrite() {
	for _, decl := range r.m.prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			d.Name = r.m.qualify(d.Name)
			d.Module = r.m.name
			r.fn(d)

		case *ast.ConstDecl:
			d.Name = r.m.qualify(d.Name)
			d.Module = r.m.name
			r.typ(d.Type)
			d.Value = r.expr(d.Value)

		case *ast.StructDecl:
			d.Name = r.m.qualify(d.Name)
			r.withTypeParams(d.TypeParams, func() {
				for _, field := range d.Fields {
					r.typ(field.Type)
				}
			})

		case *ast.EnumDecl:
			d.Name = r.m.qualify(d.Name)
			r.withTypeParams(d.TypeParams, func() {
				for _, variant := range d.Variants {
					for _, value := range variant.Values {
						r.typ(value)
					}
				}
			})

		case *ast.TraitDecl:
			d.Name = r.m.qualify(d.Name)
			for _, method := range d.Methods {
				// What a trait promises is as visible as the trait
				method.Pub = true
				method.Module = r.m.name
				r.fn(method)
			}

		case *ast.ImplDecl:
			d.StructName = r.typeName(d.StructName, d.Token())
			if d.TraitName != "" {
				d.TraitName = r.typeName(d.TraitName, d.Token())
			}
			r.withTypeParams(d.TypeParams, func() {
				for _, method := range d.Methods {
					method.Module = r.m.name
					if d.TraitName != "" {
						method.Pub = true
					}
					r.fn(method)
				}
			})
		}
	}
	r.m.prog.Imports = nil
}

// --- Scopes ---

func (r *rewriter) withTypeParams(names []string, body func()) {
	scope := make(map[string]bool, len(names))
	for _, name := range names {
		scope[name] = true
	}
	r.typeParams = append(r.typeParams, scope)
	body()
	r.typeParams = r.typeParams[:len(r.typeParams)-1]
}

func (r *rewriter) isTypeParam(name string) bool {
	for _, scope := range r.typeParams {
		if scope[name] {
			return true
		}
	}
	return false
}

func (r *rewriter) scoped(body func()) {
	r.locals = append(r.locals, make(map[string]bool))
	body()
	r.locals = r.locals[:len(r.locals)-1]
}

func (r *rewriter) declare(name string) {
	if len(r.locals) > 0 {
		r.locals[len(r.locals)-1][name] = true
	}
}

func (r *rewriter) isLocal(name string) bool {
	for _, scope := range r.locals {
		if scope[name] {
			return true
		}
	}
	return false
}

// --- Names ---

// value returns the declaration a bare name refers to as a value, if it is
// a function of this module or one imported by name.
func (r *rewriter) value(name string) *item {
	if it, ok := r.m.values[name]; ok {
		return it
	}
	return r.m.importedValues[name]
}

// typeItem is value for types.
func (r *rewriter) typeItem(name string) *item {
	if it, ok := r.m.types[name]; ok {
		return it
	}
	return r.m.importedTypes[name]
}

// typeName returns the program-wide name of a type as written: `Body`, or
// `body.Body` for a type of an imported module. Names it does not know
// (builtin types, type parameters, mistakes) are returned unchanged for the
// type checker to deal with.
func (r *rewriter) typeName(name string, tok token.Token) string {
	alias, member, qualified := strings.Cut(name, ".")
	if !qualified {
		if r.isTypeParam(name) {
			return name
		}
		if it := r.typeItem(name); it != nil {
			return it.qualified
		}
		if _, isModule := r.m.aliases[name]; isModule {
			r.report(tok, ErrNotAValue, "Name a type of the module: "+name+".Name",
				"'%s' is a module, not a type", name)
		}
		return name
	}

	target, ok := r.m.aliases[alias]
	if !ok {
		r.report(tok, ErrModuleNotFound, r.importSuggestion(alias),
			"'%s' is not an imported module", alias)
		return name
	}
	it := r.find(r.m, target, member, tok)
	if it == nil {
		return name
	}
	if !it.isType {
		r.report(tok, ErrNotAValue, "", "'%s' is a function, not a type", name)
		return name
	}
	return it.qualified
}

// importSuggestion proposes the import that would make `alias.name` work.
func (r *rewriter) importSuggestion(alias string) string {
	var known []string
	for name := range r.m.aliases {
		known = append(known, name)
	}
	if similar := resolve.ClosestName(alias, known); similar != "" {
		return "Did you mean '" + similar + "'?"
	}
	var paths []string
	for path, m := range r.modules {
		if m != nil && (path == alias || strings.HasSuffix(path, "."+alias)) {
			paths = append(paths, path)
		}
	}
	if len(paths) > 0 {
		sort.Strings(paths)
		return "Add: import " + paths[0] + ";"
	}
	// Not loaded by anything yet, but there may be a file of that name
	if _, err := r.read(r.path(alias)); err == nil {
		return "Add: import " + alias + ";"
	}
	return ""
}

// moduleOf returns the module an expression names, when it is the bare
// name of an imported module that no local variable hides.
func (r *rewriter) moduleOf(expr ast.Expr) (*module, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok || len(ident.TypeArgs) > 0 || r.isLocal(ident.Name) || r.value(ident.Name) != nil {
		return nil, false
	}
	target, ok := r.m.aliases[ident.Name]
	return target, ok
}

// --- Types ---

func (r *rewriter) typ(t *ast.Type) {
	if t == nil {
		return
	}
	switch t.Kind {
	case ast.TypeBasic:
		t.Name = r.typeName(t.Name, t.Token())
		r.types(t.Generics)
	case ast.TypeArray, ast.TypeOption, ast.TypeVariadic:
		r.typ(t.ElemType)
	case ast.TypeUnion:
		r.types(t.UnionTypes)
	case ast.TypeFunction:
		r.types(t.ParamTypes)
		r.typ(t.ReturnType)
	}
}

func (r *rewriter) types(list []*ast.Type) {
	for _, t := range list {
		r.typ(t)
	}
}

// --- Declarations and statements ---

func (r *rewriter) fn(d *ast.FnDecl) {
	r.withTypeParams(d.TypeParams, func() {
		r.scoped(func() {
			r.params(d.Params)
			r.typ(d.ReturnType)
			r.block(d.Body)
		})
	})
}

func (r *rewriter) params(params []*ast.Param) {
	for _, param := range params {
		r.typ(param.Type)
		r.declare(param.Name)
	}
}

func (r *rewriter) block(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	r.scoped(func() {
		for _, stmt := range b.Stmts {
			r.stmt(stmt)
		}
	})
}

func (r *rewriter) stmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		r.block(s)
	case *ast.LetStmt:
		r.typ(s.Type)
		if s.Value != nil {
			s.Value = r.expr(s.Value)
		}
		r.declare(s.Name)
	case *ast.ReturnStmt:
		if s.Value != nil {
			s.Value = r.expr(s.Value)
		}
	case *ast.IfStmt:
		s.Cond = r.expr(s.Cond)
		r.block(s.Then)
		if s.Else != nil {
			r.stmt(s.Else)
		}
	case *ast.WhileStmt:
		s.Cond = r.expr(s.Cond)
		r.block(s.Body)
	case *ast.ForStmt:
		s.Iterable = r.expr(s.Iterable)
		r.scoped(func() {
			r.declare(s.Var)
			if s.Index != nil {
				r.declare(s.Index.Name)
			}
			r.block(s.Body)
		})
	case *ast.MatchStmt:
		s.Cond = r.expr(s.Cond)
		for _, c := range s.Cases {
			// The names a pattern binds are variables of its case
			r.scoped(func() {
				c.Pattern = r.pattern(c.Pattern)
				switch body := c.Body.(type) {
				case *ast.BlockStmt:
					r.block(body)
				case ast.Expr:
					c.Body = r.expr(body)
				}
			})
		}
	case *ast.ExprStmt:
		s.Expr = r.expr(s.Expr)
	}
}

// pattern rewrites the pattern of a match case. A name that turns out to
// be a type of another module becomes a type pattern: the parser could only
// recognize the types declared in the file it was reading.
func (r *rewriter) pattern(pattern ast.Node) ast.Node {
	switch p := pattern.(type) {
	case nil:
		return nil
	case *ast.Type:
		r.typ(p)
		return p

	case *ast.Ident:
		if !r.isLocal(p.Name) && r.value(p.Name) == nil {
			if it := r.typeItem(p.Name); it != nil {
				r.types(p.TypeArgs)
				return &ast.Type{BaseNode: p.BaseNode, Kind: ast.TypeBasic, Name: it.qualified, Generics: p.TypeArgs}
			}
		}
		return r.expr(p)

	case *ast.VariantPattern:
		// `module.Type` reads like `Enum.Variant`; only here is it known
		// which of the two it is
		if member, ok := p.Target.(*ast.MemberExpr); ok && !p.HasArgs {
			if target, ok := r.moduleOf(member.Expr); ok {
				tok := memberToken(member)
				if it, _ := target.lookup(member.Name, make(map[*module]bool)); it != nil && it.isType {
					if it = r.find(r.m, target, member.Name, tok); it == nil {
						return p
					}
					return &ast.Type{BaseNode: ast.BaseNode{Pos: tok.Pos, Tok: tok}, Kind: ast.TypeBasic, Name: it.qualified}
				}
			}
		}
		p.Target = r.expr(p.Target)
		for i, arg := range p.Args {
			p.Args[i] = r.pattern(arg)
		}
		return p

	case *ast.BindingPattern:
		if !p.Ignored() {
			r.declare(p.Name)
		}
		return p

	case *ast.Literal, *ast.UnaryExpr:
		return p

	case ast.Expr:
		return r.expr(p)
	}
	return pattern
}

// memberToken spans `module.name`, so a diagnostic underlines all of it.
func memberToken(e *ast.MemberExpr) token.Token {
	tok := e.Expr.Token()
	tok.Literal += "." + e.Name
	tok.EndPos = e.Tok.EndPos
	tok.EndPos.Col += len(e.Name)
	return tok
}

// --- Expressions ---

func (r *rewriter) exprs(list []ast.Expr) {
	for i, e := range list {
		list[i] = r.expr(e)
	}
}

func (r *rewriter) expr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.Ident:
		r.types(e.TypeArgs)
		if r.isLocal(e.Name) {
			return e
		}
		if it := r.value(e.Name); it != nil {
			e.Name = it.qualified
			return e
		}
		// An enum is named in expressions to reach its variants: Shape.Circle
		if it := r.typeItem(e.Name); it != nil && it.kind == "enum" {
			e.Name = it.qualified
			return e
		}
		if _, isModule := r.m.aliases[e.Name]; isModule {
			r.report(e.Token(), ErrNotAValue, "Use something the module declares: "+e.Name+".name",
				"'%s' is a module and cannot be used as a value", e.Name)
		}
		return e

	case *ast.MemberExpr:
		r.types(e.TypeArgs)
		if target, ok := r.moduleOf(e.Expr); ok {
			return r.moduleMember(e, target)
		}
		e.Expr = r.expr(e.Expr)
		return e

	case *ast.CallExpr:
		e.Func = r.expr(e.Func)
		r.exprs(e.Args)
	case *ast.BinaryExpr:
		e.Left = r.expr(e.Left)
		e.Right = r.expr(e.Right)
	case *ast.UnaryExpr:
		e.Expr = r.expr(e.Expr)
	case *ast.CastExpr:
		e.Expr = r.expr(e.Expr)
		r.typ(e.Type)
	case *ast.TypeTestExpr:
		e.Expr = r.expr(e.Expr)
		r.typ(e.Type)
	case *ast.IndexExpr:
		e.Expr = r.expr(e.Expr)
		e.Index = r.expr(e.Index)
	case *ast.ArrayLiteralExpr:
		r.exprs(e.Elements)

	case *ast.StructInitExpr:
		e.Name = r.typeName(e.Name, e.Token())
		r.types(e.TypeArgs)
		// In the order of the names, so that diagnostics come out the same
		// way on every run
		fields := make([]string, 0, len(e.Fields))
		for name := range e.Fields {
			fields = append(fields, name)
		}
		sort.Strings(fields)
		for _, name := range fields {
			e.Fields[name] = r.expr(e.Fields[name])
		}

	case *ast.FnExpr:
		r.scoped(func() {
			r.params(e.Params)
			r.typ(e.ReturnType)
			r.block(e.Body)
		})
	}
	return expr
}

// moduleMember turns `module.name` into a reference to the declaration.
func (r *rewriter) moduleMember(e *ast.MemberExpr, target *module) ast.Expr {
	tok := memberToken(e)
	it := r.find(r.m, target, e.Name, tok)
	if it == nil {
		return e
	}
	if it.isType && it.kind != "enum" {
		r.report(tok, ErrNotAValue, "A struct is created with "+tok.Literal+" { field: value }",
			"'%s' is a %s and cannot be used as a value", tok.Literal, it.kind)
		return e
	}
	return &ast.Ident{
		BaseNode: ast.BaseNode{Pos: tok.Pos, Tok: tok},
		Name:     it.qualified,
		TypeArgs: e.TypeArgs,
	}
}

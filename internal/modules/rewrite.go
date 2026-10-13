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
				r.bounds(d.Bounds)
				for _, field := range d.Fields {
					r.typ(field.Type)
					if field.Default != nil {
						r.scoped(func() { field.Default = r.expr(field.Default) })
					}
				}
			})

		case *ast.EnumDecl:
			d.Name = r.m.qualify(d.Name)
			r.withTypeParams(d.TypeParams, func() {
				r.bounds(d.Bounds)
				for _, variant := range d.Variants {
					for _, value := range variant.Values {
						r.typ(value)
					}
				}
			})

		case *ast.TraitDecl:
			d.Name = r.m.qualify(d.Name)
			r.withTypeParams(d.TypeParams, func() {
				for _, method := range d.Methods {
					// What a trait promises is as visible as the trait
					method.Pub = true
					method.Module = r.m.name
					r.method(method)
				}
			})

		case *ast.ImplDecl:
			d.StructName = r.typeName(d.StructName, d.Token())
			if d.TraitName != "" {
				d.TraitName = r.typeName(d.TraitName, d.Token())
			}
			r.withTypeParams(d.TypeParams, func() {
				r.bounds(d.Bounds)
				r.types(d.TraitArgs)
				for _, method := range d.Methods {
					method.Module = r.m.name
					if d.TraitName != "" {
						method.Pub = true
					}
					r.method(method)
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

// bounds rewrites the traits type parameters are required to implement.
func (r *rewriter) bounds(bounds map[string][]*ast.Type) {
	for _, traits := range bounds {
		r.types(traits)
	}
}

// method rewrites a method: a function in which `self` is a variable.
func (r *rewriter) method(d *ast.FnDecl) {
	r.scoped(func() {
		r.declare("self")
		r.fn(d)
	})
}

func (r *rewriter) fn(d *ast.FnDecl) {
	r.withTypeParams(d.TypeParams, func() {
		r.bounds(d.Bounds)
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
		// The names of `if value is Shape.Circle(r)` are variables of the
		// block that follows, not of the `else`
		r.scoped(func() {
			s.Cond = r.condition(s.Cond)
			r.block(s.Then)
		})
		if s.Else != nil {
			r.stmt(s.Else)
		}
	case *ast.WhileStmt:
		r.scoped(func() {
			s.Cond = r.condition(s.Cond)
			r.block(s.Body)
		})
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
				for i, alternative := range c.Alternatives {
					c.Alternatives[i] = r.pattern(alternative)
				}
				if c.Guard != nil {
					c.Guard = r.expr(c.Guard)
				}
				switch body := c.Body.(type) {
				case *ast.BlockStmt:
					r.block(body)
				case ast.Expr:
					c.Body = r.expr(body)
				}
			})
		}
	case *ast.DeferStmt:
		r.block(s.Body)
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

// condition rewrites what an `if` or a `while` tests, or a chain of `&&`.
// The names of its pattern tests are declared in the scope the caller
// opened, where what the condition guards is rewritten next.
func (r *rewriter) condition(cond ast.Expr) ast.Expr {
	switch e := cond.(type) {
	case *ast.BinaryExpr:
		if e.Op == "&&" {
			e.Left = r.condition(e.Left)
			e.Right = r.condition(e.Right)
			return e
		}
	case *ast.PatternTestExpr:
		return r.patternTest(e)
	}
	return r.expr(cond)
}

// patternTest rewrites `value is Pattern`. Two things written as patterns
// turn out here to be tests for a type, as they were before `is` took
// patterns: `value is geo.Shape`, a type of another module, and
// `value is (Point, Label)`, a tuple of types that read like names.
func (r *rewriter) patternTest(e *ast.PatternTestExpr) ast.Expr {
	e.Expr = r.expr(e.Expr)

	if tuple := r.tupleOfTypes(e.Pattern); tuple != nil {
		r.typ(tuple)
		return &ast.TypeTestExpr{BaseNode: e.BaseNode, Expr: e.Expr, Type: tuple}
	}
	switch pattern := r.pattern(e.Pattern).(type) {
	case *ast.Type:
		return &ast.TypeTestExpr{BaseNode: e.BaseNode, Expr: e.Expr, Type: pattern}
	case *ast.VariantPattern:
		e.Pattern = pattern
	}
	return e
}

// tupleOfTypes gives the tuple type a pattern stands for when it is a
// tuple of names that are all types, and nil otherwise.
func (r *rewriter) tupleOfTypes(p *ast.VariantPattern) *ast.Type {
	target, ok := p.Target.(*ast.MemberExpr)
	if !ok || len(p.Args) == 0 {
		return nil
	}
	enum, ok := target.Expr.(*ast.Ident)
	if !ok || !ast.IsTupleName(enum.Name) {
		return nil
	}

	tuple := &ast.Type{BaseNode: p.BaseNode, Kind: ast.TypeBasic, Name: enum.Name}
	for _, arg := range p.Args {
		name, ok := arg.(*ast.BindingPattern)
		if !ok || name.Ignored() || r.isLocal(name.Name) || r.value(name.Name) != nil {
			return nil
		}
		if !r.isTypeParam(name.Name) && r.typeItem(name.Name) == nil {
			return nil
		}
		tuple.Generics = append(tuple.Generics, &ast.Type{BaseNode: name.BaseNode, Kind: ast.TypeBasic, Name: name.Name})
	}
	return tuple
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
		if e.Op == "&&" {
			// What a pattern test on the left names is seen on the right
			r.scoped(func() { expr = r.condition(e) })
			return expr
		}
		e.Left = r.expr(e.Left)
		e.Right = r.expr(e.Right)
	case *ast.PatternTestExpr:
		r.scoped(func() { expr = r.patternTest(e) })
		return expr
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
	case *ast.MatchExpr:
		r.stmt(e.Match)
	case *ast.RangeExpr:
		e.Low = r.expr(e.Low)
		e.High = r.expr(e.High)
		if !e.Counted && r.loader.modules[RangeModule] != nil {
			// Kept as a value: a struct like any other from here on
			yes := "false"
			if e.Inclusive {
				yes = "true"
			}
			literal := func(kind, text string) ast.Expr {
				tok := e.Token()
				tok.Literal = text
				return &ast.Literal{BaseNode: ast.BaseNode{Pos: e.Position(), Tok: tok}, Kind: kind, Value: text}
			}
			return &ast.StructInitExpr{BaseNode: e.BaseNode, Name: RangeStruct, Range: e, Fields: map[string]ast.Expr{
				"from": e.Low, "to": e.High, "inclusive": literal("bool", yes), "by": literal("int", "1"),
			}}
		}

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

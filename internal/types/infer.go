package types

import (
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/resolve"
)

// Type argument inference: `first([1, 2])` is read as `first<Int>([1, 2])`.
//
// The type parameters of a generic function or method are found by matching
// the declared parameter types against the types of the arguments:
// `xs: [T]` against `[int]` gives T = int. The result is written into the
// AST as explicit type arguments, so that everything after the checker
// (monomorphization in particular) sees the call as if they had been typed.
//
// A parameter that no argument determines (`fn none<T>() -> [T]`) is taken
// from the type the call's result is expected to have, when there is one:
// `let xs: [Int] = none();`, `return none();` in a function returning [Int],
// `takesInts(none())`. Without either, the call needs its type arguments.

// inferFunctionTypeArgs fills in the type arguments of a call to a generic
// function that was written without any.
func (c *Checker) inferFunctionTypeArgs(e *ast.CallExpr, expected Type) {
	ident, ok := e.Func.(*ast.Ident)
	if !ok || len(ident.TypeArgs) > 0 {
		return
	}
	sym := c.table.Defs[ident]
	if sym == nil || sym.Kind != resolve.SymbolFunc {
		return
	}
	fn, ok := sym.Decl.(*ast.FnDecl)
	if !ok || len(fn.TypeParams) == 0 {
		return
	}

	// Parameter types with the function's type parameters left open
	open := make(map[string]Type, len(fn.TypeParams))
	for _, name := range fn.TypeParams {
		open[name] = NewTypeParam(name)
	}
	restore := c.bind(open)
	signature := genericSignature{
		names:    fn.TypeParams,
		params:   c.parameterTypes(fn),
		variadic: isVariadicDecl(fn),
		result:   c.resolveType(fn.ReturnType),
		bounds:   c.openBounds(fn.Bounds),
	}
	restore()

	if inferred := c.inferTypeArgs(signature, e.Args, expected, ident.BaseNode); inferred != nil {
		ident.TypeArgs = inferred
	}
}

// inferMethodTypeArgs does the same for a method with type parameters of
// its own: `stack.map(toString)` is read as `stack.map<String>(toString)`.
func (c *Checker) inferMethodTypeArgs(recv Type, member *ast.MemberExpr, e *ast.CallExpr, expected Type) {
	decl := c.methodDecl(recv, member.Name)
	if decl == nil || len(decl.TypeParams) == 0 || len(member.TypeArgs) > 0 {
		return
	}

	// The struct's parameters are known from the receiver; the method's
	// own are left open
	bindings := make(map[string]Type)
	if impl := c.impls[decl]; impl != nil {
		var args []Type
		if structType, ok := recv.(*StructType); ok {
			args = structType.TypeArgs
		}
		if array, ok := recv.(*ArrayType); ok {
			// In `impl<T> [T]`, T is what the array holds
			args = []Type{array.ElementType}
		}
		for i, name := range impl.TypeParams {
			bindings[name] = Unknown
			if i < len(args) {
				bindings[name] = args[i]
			}
		}
	}
	for _, name := range decl.TypeParams {
		bindings[name] = NewTypeParam(name)
	}

	signature := genericSignature{names: decl.TypeParams, variadic: isVariadicDecl(decl)}
	restore := c.bind(bindings)
	c.withSelf(recv, func() {
		signature.params = c.parameterTypes(decl)
		signature.result = c.resolveType(decl.ReturnType)
		signature.bounds = c.openBounds(decl.Bounds)
	})
	restore()

	if inferred := c.inferTypeArgs(signature, e.Args, expected, member.BaseNode); inferred != nil {
		member.TypeArgs = inferred
	}
}

// parameterTypes resolves the declared parameter types of a function. A
// variadic parameter is given as its element type.
func (c *Checker) parameterTypes(fn *ast.FnDecl) []Type {
	params := make([]Type, len(fn.Params))
	for i, param := range fn.Params {
		if isVariadicDecl(fn) && i == len(fn.Params)-1 {
			params[i] = c.resolveType(param.Type.ElemType)
		} else {
			params[i] = c.resolveType(param.Type)
		}
	}
	return params
}

func isVariadicDecl(fn *ast.FnDecl) bool {
	if len(fn.Params) == 0 {
		return false
	}
	last := fn.Params[len(fn.Params)-1].Type
	return last != nil && last.Kind == ast.TypeVariadic
}

// genericSignature is the signature of a generic function or method with
// its type parameters left open.
type genericSignature struct {
	names    []string // the type parameters to find
	params   []Type
	variadic bool
	result   Type

	// What each type parameter is required to implement. In
	// <T, S: Source<T>>, an S that is a Counter says what T is.
	bounds map[string][]Type
}

// openBounds resolves the requirements of type parameters, for a
// signature being matched: to be called while the parameters are open.
func (c *Checker) openBounds(bounds map[string][]*ast.Type) map[string][]Type {
	if len(bounds) == 0 {
		return nil
	}
	out := make(map[string][]Type, len(bounds))
	for name, traits := range bounds {
		for _, trait := range traits {
			out[name] = append(out[name], c.resolveType(trait))
		}
	}
	return out
}

// inferTypeArgs matches parameter types against arguments, then the result
// type against the type expected of the call. It returns the type arguments
// in declaration order, or nil if any of them could not be determined.
func (c *Checker) inferTypeArgs(sig genericSignature, args []ast.Expr, expected Type, at ast.BaseNode) []*ast.Type {
	names, params := sig.names, sig.params
	isParam := make(map[string]bool, len(names))
	for _, name := range names {
		isParam[name] = true
	}

	found := make(map[string]Type)
	for i, argType := range c.typesOf(args) {
		var param Type
		switch {
		case i < len(params):
			param = params[i]
		case sig.variadic && len(params) > 0:
			param = params[len(params)-1]
		default:
			continue
		}
		c.unify(param, argType, isParam, found)
	}

	// A function written with `->` has no type until its parameters do.
	// Once the other arguments say what those are, its body says what it
	// gives: in arrays.map(people, p -> p.name), T is Person from the
	// first argument, and U is String from the body.
	fromShortFns := func() {
		for i, arg := range args {
			fn, ok := arg.(*ast.FnExpr)
			if !ok || !fn.Short || i >= len(params) {
				continue
			}
			want, ok := params[i].(*FunctionType)
			if !ok || len(want.Params) != len(fn.Params) {
				continue
			}
			known := make([]Type, len(want.Params))
			complete := true
			for j, param := range want.Params {
				// Asked of the parameter as declared: what was found for
				// T may itself be a type parameter called T, the caller's
				if mentionsOpen(param, isParam, found) {
					complete = false
				}
				known[j] = Instantiate(param, found)
			}
			if !complete {
				continue
			}
			if result := c.shortFnResult(fn, known); result != nil {
				c.unify(want.ReturnType, result, isParam, found)
			} else if !fn.Typed && c.annotateShortFn(fn, known, Unknown) {
				// Its body does not say what it gives: there is a mistake
				// in it. Check it now, with what is known of its
				// parameters, for the mistake to be the error reported.
				c.info.Types[fn] = c.checkFnExpr(fn)
			}
		}
	}
	fromShortFns()

	// A parameter that was found says what the others are, through what
	// it is required to implement; and those in turn
	fromBounds := func() {
		for range names {
			for _, name := range names {
				if typ, ok := found[name]; ok {
					for _, bound := range sig.bounds[name] {
						c.unify(bound, typ, isParam, found)
					}
				}
			}
		}
	}
	fromBounds()

	// What the arguments say comes first; the expected type only fills in
	// what they leave open
	if expected != nil && !expected.Equals(Any) {
		c.unify(sig.result, expected, isParam, found)
		fromBounds()
	}

	result := make([]*ast.Type, len(names))
	for i, name := range names {
		typ, ok := found[name]
		if !ok {
			return nil
		}
		if result[i] = typeToAST(typ, at); result[i] == nil {
			return nil
		}
	}
	return result
}

// typesOf types expressions without reporting anything: the arguments are
// checked again, against the inferred signature, and mistakes in them are
// reported then.
func (c *Checker) typesOf(exprs []ast.Expr) []Type {
	reported := c.errs
	c.errs = errors.NewHandler()
	defer func() { c.errs = reported }()

	types := make([]Type, len(exprs))
	for i, expr := range exprs {
		types[i] = c.checkExpr(expr)
	}
	return types
}

// unify matches a parameter type against the type of an argument and
// records what each type parameter has to be. The first argument that
// determines a parameter decides; if a later one disagrees, the ordinary
// argument check reports it against that decision.
func (c *Checker) unify(param, arg Type, isParam map[string]bool, found map[string]Type) {
	if param == nil || arg == nil || isUnknown(arg) {
		return
	}

	switch p := param.(type) {
	case *TypeParam:
		if !isParam[p.Name] {
			return
		}
		if _, done := found[p.Name]; !done && !arg.Equals(Null) {
			found[p.Name] = arg
		}

	case *ArrayType:
		if a, ok := arg.(*ArrayType); ok {
			c.unify(p.ElementType, a.ElementType, isParam, found)
		}

	case *FunctionType:
		a, ok := arg.(*FunctionType)
		if !ok || len(a.Params) != len(p.Params) {
			return
		}
		for i := range p.Params {
			c.unify(p.Params[i], a.Params[i], isParam, found)
		}
		c.unify(p.ReturnType, a.ReturnType, isParam, found)

	case *BehaviorType:
		// Source<T> against a Counter that implements Source<Int>
		if args, ok := c.traitArgsOf(arg, p.Name); ok {
			for i := range p.TypeArgs {
				if i < len(args) {
					c.unify(p.TypeArgs[i], args[i], isParam, found)
				}
			}
		}

	case *StructType:
		a, ok := arg.(*StructType)
		if !ok || a.Name != p.Name || len(a.TypeArgs) != len(p.TypeArgs) {
			return
		}
		for i := range p.TypeArgs {
			c.unify(p.TypeArgs[i], a.TypeArgs[i], isParam, found)
		}

	case *UnionType:
		// T? against a value or another optional: match what is not null
		if !hasNull(p) {
			return
		}
		if value, argValue := withoutNull(p), withoutNull(arg); value != nil && argValue != nil {
			c.unify(value, argValue, isParam, found)
		}
	}
}

// typeToAST writes a type the way it would appear in source, or returns nil
// for a type that has no source form.
func typeToAST(t Type, at ast.BaseNode) *ast.Type {
	basic := func(name string) *ast.Type {
		return &ast.Type{BaseNode: at, Kind: ast.TypeBasic, Name: name}
	}

	switch t := t.(type) {
	case *PrimitiveType:
		switch t.Name {
		case PrimitiveInt:
			return basic("Int")
		case PrimitiveFloat:
			return basic("Float")
		case PrimitiveBool:
			return basic("Bool")
		case PrimitiveString:
			return basic("String")
		case PrimitiveAny:
			return basic("Any")
		case PrimitiveVoid:
			return basic("Void")
		}

	case *TypeParam:
		return basic(t.Name)

	case *BehaviorType:
		result := basic(t.Name)
		for _, arg := range t.TypeArgs {
			argType := typeToAST(arg, at)
			if argType == nil {
				return nil
			}
			result.Generics = append(result.Generics, argType)
		}
		return result

	case *StructType:
		result := basic(t.Name)
		for _, arg := range t.TypeArgs {
			argType := typeToAST(arg, at)
			if argType == nil {
				return nil
			}
			result.Generics = append(result.Generics, argType)
		}
		return result

	case *ArrayType:
		if elem := typeToAST(t.ElementType, at); elem != nil {
			return &ast.Type{BaseNode: at, Kind: ast.TypeArray, ElemType: elem}
		}

	case *UnionType:
		value := withoutNull(t)
		if !hasNull(t) || value == nil {
			return nil
		}
		if _, nested := value.(*UnionType); nested {
			return nil
		}
		if elem := typeToAST(value, at); elem != nil {
			return &ast.Type{BaseNode: at, Kind: ast.TypeOption, ElemType: elem}
		}

	case *FunctionType:
		if t.IsVariadic {
			return nil
		}
		result := &ast.Type{BaseNode: at, Kind: ast.TypeFunction}
		for _, param := range t.Params {
			paramType := typeToAST(param, at)
			if paramType == nil {
				return nil
			}
			result.ParamTypes = append(result.ParamTypes, paramType)
		}
		if result.ReturnType = typeToAST(t.ReturnType, at); result.ReturnType != nil {
			return result
		}
	}
	return nil
}

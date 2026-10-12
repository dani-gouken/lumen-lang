package types

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/resolve"
)

// collectDecls indexes structs, traits and methods before any body is
// checked, so a method can be called above the impl block that defines it.
func (c *Checker) collectDecls() {
	for _, decl := range c.prog.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			c.structs[d.Name] = d
		case *ast.TraitDecl:
			c.traits[d.Name] = d
		}
	}

	for _, decl := range c.prog.Decls {
		impl, ok := decl.(*ast.ImplDecl)
		if !ok {
			continue
		}
		if impl.TraitName != "" {
			c.registry.Register(impl.StructName, impl.TraitName)
		}
		if c.methods[impl.StructName] == nil {
			c.methods[impl.StructName] = make(map[string]*ast.FnDecl)
		}
		for _, m := range impl.Methods {
			if _, dup := c.methods[impl.StructName][m.Name]; dup {
				c.errorf(m.Token(), ErrDuplicateMethod, "method '%s' is already defined for %s", m.Name, impl.StructName)
				continue
			}
			c.methods[impl.StructName][m.Name] = m
			c.impls[m] = impl
		}
	}
}

// withSelf runs fn with `Self` standing for the given type.
func (c *Checker) withSelf(self Type, fn func()) {
	prev := c.selfType
	c.selfType = self
	defer func() { c.selfType = prev }()
	fn()
}

// bindStructParams brings the type parameters of a generic struct into
// scope, bound to the type arguments of one use of it. It returns a function
// that restores the previous scope.
func (c *Checker) bindStructParams(decl *ast.StructDecl, args []Type) func() {
	bindings := make(map[string]Type, len(decl.TypeParams))
	for i, name := range decl.TypeParams {
		if i < len(args) {
			bindings[name] = args[i]
		} else {
			bindings[name] = Unknown
		}
	}
	return c.bind(bindings)
}

// bind adds type parameter bindings and returns a function that removes
// them again. A nil binding marks a parameter that is in scope but not yet
// bound to a concrete type.
func (c *Checker) bind(bindings map[string]Type) func() {
	type saved struct {
		typ   Type
		found bool
	}
	previous := make(map[string]saved, len(bindings))
	for name, typ := range bindings {
		old, found := c.typeParams[name]
		previous[name] = saved{old, found}
		c.typeParams[name] = typ
	}
	return func() {
		for name, old := range previous {
			if old.found {
				c.typeParams[name] = old.typ
			} else {
				delete(c.typeParams, name)
			}
		}
	}
}

// fieldType returns the type of a struct field.
func (c *Checker) fieldType(recv *StructType, name string) (Type, bool) {
	decl := c.structs[recv.Name]
	if decl == nil {
		return Unknown, true // Monomorphized or unknown struct: nothing to check against
	}
	field := findField(decl, name)
	if field == nil {
		return nil, false
	}

	defer c.bindStructParams(decl, recv.TypeArgs)()
	return c.resolveType(field.Type), true
}

// methodDecl finds the declaration of a method of a struct, or of a
// method a trait requires.
func (c *Checker) methodDecl(recv Type, name string) *ast.FnDecl {
	switch t := recv.(type) {
	case *StructType:
		return c.methods[t.Name][name]
	case *BehaviorType:
		if trait := c.traits[t.Name]; trait != nil {
			for _, m := range trait.Methods {
				if m.Name == name {
					return m
				}
			}
		}
	}
	return nil
}

// methodType returns the signature of a method as seen on a receiver of the
// given type: a struct with an impl method of that name, or a trait that
// declares it. `Self` in the signature is the receiver's type, the type
// parameters of the impl are the receiver's type arguments, and the
// method's own type parameters are the given type arguments.
func (c *Checker) methodType(recv Type, name string, typeArgs []Type) (*FunctionType, bool) {
	decl := c.methodDecl(recv, name)
	if decl == nil {
		return nil, false
	}

	bindings := make(map[string]Type)

	// In `impl<T> Box<T>`, T is whatever the receiver has in that position
	if impl := c.impls[decl]; impl != nil {
		var args []Type
		if structType, ok := recv.(*StructType); ok {
			args = structType.TypeArgs
		}
		for i, param := range impl.TypeParams {
			bindings[param] = Unknown
			if i < len(args) {
				bindings[param] = args[i]
			}
		}
	}

	// In `fn map<U>(...)`, U comes from the call: stack.map<String>(...)
	for i, param := range decl.TypeParams {
		bindings[param] = Unknown
		if i < len(typeArgs) {
			bindings[param] = typeArgs[i]
		}
	}
	defer c.bind(bindings)()

	var ft *FunctionType
	c.withSelf(recv, func() { ft, _ = c.resolveFnType(decl).(*FunctionType) })
	return ft, ft != nil
}

// methodTypeArgs resolves the type arguments written at a method call and
// checks that the method takes that many.
func (c *Checker) methodTypeArgs(recv Type, member *ast.MemberExpr, e *ast.CallExpr, expected Type) []Type {
	c.inferMethodTypeArgs(recv, member, e, expected)

	var args []Type
	for _, arg := range member.TypeArgs {
		args = append(args, c.resolveType(arg))
	}

	decl := c.methodDecl(recv, member.Name)
	if decl == nil || len(args) == len(decl.TypeParams) {
		return args
	}

	params := strings.Join(decl.TypeParams, ", ")
	switch {
	case len(decl.TypeParams) == 0:
		c.errorf(member.Token(), ErrMethodTypeArgs, "method '%s' takes no type arguments", member.Name)
	case len(args) == 0:
		c.errorfWithSuggestion(member.Token(), ErrMethodTypeArgs,
			fmt.Sprintf("Write the types for <%s>: %s<...>(...)", params, member.Name),
			"cannot infer the type arguments of method '%s'", member.Name)
	default:
		c.errorf(member.Token(), ErrMethodTypeArgs, "method '%s' takes %d type arguments (<%s>), got %d",
			member.Name, len(decl.TypeParams), params, len(args))
	}
	return nil // Unbound parameters become unknown, avoiding follow-up errors
}

// checkMethodCall types `receiver.name(args)`: a method of the receiver's
// struct or trait, or a call of a function stored in a field.
func (c *Checker) checkMethodCall(e *ast.CallExpr, member *ast.MemberExpr, expected Type) Type {
	recv := c.checkExpr(member.Expr)

	fail := func(format string, args ...interface{}) Type {
		c.errorf(member.Token(), ErrUnknownMethod, format, args...)
		c.checkArgsOnly(e)
		return Unknown
	}

	switch t := recv.(type) {
	case nil:
		c.checkArgsOnly(e)
		return Unknown

	case *StructType:
		if ft, ok := c.methodType(t, member.Name, c.methodTypeArgs(t, member, e, expected)); ok {
			c.info.Types[member] = ft
			return c.checkArgs(e, ft)
		}
		if c.structs[t.Name] == nil {
			break // Unknown struct, reported where it was named
		}
		if fieldType, ok := c.fieldType(t, member.Name); ok {
			// A field that holds a function
			c.info.Types[member] = fieldType
			return c.callValue(e, fieldType)
		}
		if similar := c.similarMember(t, member.Name); similar != "" {
			c.errorfWithSuggestion(member.Token(), ErrUnknownMethod, "Did you mean '"+similar+"'?",
				"%s has no method '%s'", t, member.Name)
			c.checkArgsOnly(e)
			return Unknown
		}
		return fail("%s has no method '%s'", t, member.Name)

	case *BehaviorType:
		if ft, ok := c.methodType(t, member.Name, c.methodTypeArgs(t, member, e, expected)); ok {
			c.info.Types[member] = ft
			return c.checkArgs(e, ft)
		}
		return fail("trait %s has no method '%s'", t, member.Name)

	case *UnionType:
		if hasNull(t) {
			c.errorfWithSuggestion(member.Token(), ErrPossiblyNull, "Test it against null first, or provide a fallback with '??'",
				"cannot call '%s' on %s: the value may be null", member.Name, t)
			c.checkArgsOnly(e)
			return Unknown
		}

	case *PrimitiveType:
		if isUnknown(t) {
			c.checkArgsOnly(e)
			return Unknown
		}
	}

	return fail("type %s has no method '%s'", recv, member.Name)
}

// similarMember suggests a field or method of a struct close to name.
func (c *Checker) similarMember(recv *StructType, name string) string {
	var candidates []string
	for method := range c.methods[recv.Name] {
		candidates = append(candidates, method)
	}
	if decl := c.structs[recv.Name]; decl != nil {
		for _, field := range decl.Fields {
			candidates = append(candidates, field.Name)
		}
	}
	sort.Strings(candidates)

	for _, candidate := range candidates {
		if strings.EqualFold(candidate, name) || editDistanceAtMostOne(candidate, name) {
			return candidate
		}
	}
	return ""
}

// editDistanceAtMostOne reports whether b is a by one inserted, deleted or
// replaced character.
func editDistanceAtMostOne(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	if i == len(a) {
		return true
	}
	if len(a) == len(b) {
		return a[i+1:] == b[i+1:]
	}
	return a[i:] == b[i+1:]
}

// callValue types a call of a value that should be a function.
func (c *Checker) callValue(e *ast.CallExpr, callee Type) Type {
	if callee == nil || isUnknown(callee) {
		c.checkArgsOnly(e)
		return Unknown
	}

	ft, ok := callee.(*FunctionType)
	if !ok {
		c.errorf(e.Func.Token(), ErrNotFunction, "cannot call non-function type %s", callee)
		c.checkArgsOnly(e)
		return Unknown
	}
	return c.checkArgs(e, ft)
}

// checkArgsOnly checks the argument expressions of a call whose callee
// could not be typed, so mistakes inside them are still reported.
func (c *Checker) checkArgsOnly(e *ast.CallExpr) {
	for _, arg := range e.Args {
		c.checkExpr(arg)
	}
}

// checkArgs checks the arguments of a call against a signature and returns
// the call's type.
func (c *Checker) checkArgs(e *ast.CallExpr, ft *FunctionType) Type {
	// Check argument count
	if len(e.Args) != len(ft.Params) {
		if !ft.IsVariadic || len(e.Args) < len(ft.Params)-1 {
			c.errorf(e.Token(), ErrArgCount, "expected %d arguments, got %d", len(ft.Params), len(e.Args))
		}
	}

	// Check argument types
	for i, arg := range e.Args {
		var paramType Type
		if ft.IsVariadic && i >= len(ft.Params)-1 {
			paramType = ft.Params[len(ft.Params)-1]
		} else if i < len(ft.Params) {
			paramType = ft.Params[i]
		} else {
			// Extra arguments for non-variadic function (already reported error)
			c.checkExpr(arg)
			continue
		}

		argType := c.checkExprAs(arg, paramType)
		if argType == nil {
			continue
		}

		if !c.assignable(paramType, argType) {
			c.errorf(arg.Token(), ErrArgType, "cannot use type %s as argument of type %s", argType, paramType)
		}
	}

	return ft.ReturnType
}

// checkExprAs checks an expression in a place where a type is expected.
// An array literal then takes its element type from that place instead of
// from its first element, so `let shapes: [Shape] = [rect, square];` and
// `let names: [String] = [];` have the declared type. A call of a generic
// function takes from it the type arguments it could not infer otherwise.
func (c *Checker) checkExprAs(expr ast.Expr, expected Type) Type {
	if _, isCall := expr.(*ast.CallExpr); isCall {
		// A generic call may take the type arguments its own arguments
		// leave open from here: let xs: [Int] = none();
		c.expected = expected
		typ := c.checkExpr(expr)
		c.expected = nil
		return typ
	}

	literal, isLiteral := expr.(*ast.ArrayLiteralExpr)
	want, isArray := expected.(*ArrayType)
	if !isLiteral || !isArray || isUnknown(want.ElementType) {
		return c.checkExpr(expr)
	}

	for _, elem := range literal.Elements {
		elemType := c.checkExprAs(elem, want.ElementType)
		if elemType != nil && !c.assignable(want.ElementType, elemType) {
			c.errorfWithSuggestion(elem.Token(), ErrTypeMismatch,
				"All array elements should be of type "+describe(want.ElementType),
				"array element has type %s, expected %s", elemType, want.ElementType)
		}
	}

	c.info.Types[expr] = want
	return want
}

// checkPush applies the one rule the signature of the builtin `push` cannot
// express: the value must fit the element type of the array.
func (c *Checker) checkPush(e *ast.CallExpr) {
	if len(e.Args) != 2 {
		return
	}
	arrType, valType := c.info.Types[e.Args[0]], c.info.Types[e.Args[1]]
	if arrType == nil || valType == nil || isUnknown(arrType) {
		return
	}

	arr, ok := arrType.(*ArrayType)
	if !ok {
		c.errorf(e.Args[0].Token(), ErrArgType, "push expects an array, got %s", arrType)
		return
	}
	if !c.assignable(arr.ElementType, valType) {
		c.errorf(e.Args[1].Token(), ErrArgType, "cannot push type %s onto %s", valType, arrType)
	}
}

// isBuiltinCall reports whether a call goes to the named host function.
func (c *Checker) isBuiltinCall(e *ast.CallExpr, name string) bool {
	ident, ok := e.Func.(*ast.Ident)
	if !ok || ident.Name != name {
		return false
	}
	sym := c.table.Defs[ident]
	if sym == nil || sym.Kind != resolve.SymbolFunc {
		return false
	}
	decl, ok := sym.Decl.(*ast.FnDecl)
	return ok && decl.Body == nil && resolve.IsBuiltin(name)
}

// --- Assignability ---

// assignable reports whether a value of type source can be stored in a
// place of type target.
func (c *Checker) assignable(target, source Type) bool {
	if target == nil || source == nil || isUnknown(target) || isUnknown(source) {
		return true
	}
	if target.Equals(Any) {
		return true
	}

	// Structs are the same type when their type arguments agree too:
	// a Box<Int> is not a Box<String>
	if t, ok := target.(*StructType); ok {
		src, ok := source.(*StructType)
		if !ok || t.Name != src.Name {
			return false
		}
		if len(t.TypeArgs) != len(src.TypeArgs) {
			return len(t.TypeArgs) == 0 || len(src.TypeArgs) == 0
		}
		for i := range t.TypeArgs {
			if !c.sameType(t.TypeArgs[i], src.TypeArgs[i]) {
				return false
			}
		}
		return true
	}

	if target.Equals(source) {
		return true
	}
	// `Self` outside of an impl (a trait's default method) is not bound yet
	if target.Equals(Self) || source.Equals(Self) {
		return true
	}

	// A union source fits when every alternative fits
	if src, ok := source.(*UnionType); ok {
		for _, member := range src.Types {
			if !c.assignable(target, member) {
				return false
			}
		}
		return true
	}

	switch t := target.(type) {
	case *UnionType:
		for _, member := range t.Types {
			if c.assignable(member, source) {
				return true
			}
		}

	case *ArrayType:
		if src, ok := source.(*ArrayType); ok {
			return c.assignable(t.ElementType, src.ElementType)
		}

	case *FunctionType:
		src, ok := source.(*FunctionType)
		if !ok || len(t.Params) != len(src.Params) || t.IsVariadic != src.IsVariadic {
			return false
		}
		// The function will be called with the target's parameter types,
		// so it must accept them (parameters go the other way round), and
		// what it returns must fit what the target promises.
		for i := range t.Params {
			if !c.assignable(src.Params[i], t.Params[i]) {
				return false
			}
		}
		return c.assignable(t.ReturnType, src.ReturnType)

	case *BehaviorType:
		// A struct fits a trait it implements
		if src, ok := source.(*StructType); ok {
			return c.registry.Implements(src.Name, t.Name)
		}
	}
	return false
}

// sameType reports whether two types are interchangeable.
func (c *Checker) sameType(a, b Type) bool {
	return c.assignable(a, b) && c.assignable(b, a)
}

// checkCastExpr types `value as Type`. A cast is accepted when the value
// may be of that type: the type is more specific than the value's (out of
// Any, out of an optional or union, from a trait to a struct implementing
// it) or already fits. Whether it really is, is checked when it runs,
// including the elements of arrays and the fields of generic structs.
func (c *Checker) checkCastExpr(e *ast.CastExpr) Type {
	source := c.checkExpr(e.Expr)
	target := c.resolveType(e.Type)
	if source == nil || isUnknown(source) || isUnknown(target) {
		return target
	}

	switch {
	case c.assignable(target, source):
		// Always succeeds: nothing to check at runtime
	case c.assignable(source, target):
		descriptor, ok := c.castDescriptor(target)
		if !ok {
			c.errorf(e.Type.Token(), ErrInvalidCast, "a cast to %s cannot be checked", target)
			return target
		}
		c.info.Casts[e] = descriptor
	default:
		c.errorf(e.Token(), ErrInvalidCast, "cannot cast %s to %s: a value of that type can never be one", source, target)
	}
	return target
}

// checkTypeTest types `value is Type`, a question answered when the
// program runs. Inside a branch taken when it is true, the tested variable
// or field has that type (see flow.go).
func (c *Checker) checkTypeTest(e *ast.TypeTestExpr) Type {
	source := c.checkExpr(e.Expr)
	target := c.resolveType(e.Type)
	if isUnknown(target) {
		return Bool
	}

	if source != nil && !isUnknown(source) && !c.assignable(target, source) && !c.assignable(source, target) {
		c.errorf(e.Token(), ErrInvalidCast, "%s can never be %s", source, target)
		return Bool
	}

	descriptor, ok := c.castDescriptor(target)
	if !ok {
		c.errorf(e.Type.Token(), ErrInvalidCast, "a test for %s cannot be made", target)
		return Bool
	}
	c.info.Tests[e] = descriptor
	c.info.TestedTypes[e] = target
	return Bool
}

// castDescriptor encodes what a cast has to verify at runtime, in the form
// the VM's cast function understands:
//
//	int float bool string null   that kind of value
//	any                          anything
//	fn(D,D)->D                   a function usable with that signature
//	[D]                          an array whose elements all match D
//	?D                           null, or what D says
//	{A|B}                        a value of struct A or B
//	{A:f=D,g=D}                  a value of struct A whose fields f and g match
//
// A value does not record the type arguments of a generic struct, so a cast
// to Box<Int> is verified through the fields whose type depends on them:
// {Box:value=int}. A function type is compared with the signature recorded
// for the function value when it was compiled.
func (c *Checker) castDescriptor(target Type) (string, bool) {
	return c.describeCast(target, make(map[string]bool))
}

// describeCast builds a descriptor. visiting holds the generic struct types
// being described, so a struct that contains itself (a linked list) is
// verified one level deep instead of forever.
func (c *Checker) describeCast(target Type, visiting map[string]bool) (string, bool) {
	switch t := target.(type) {
	case *PrimitiveType:
		switch t.Name {
		case PrimitiveInt, PrimitiveFloat, PrimitiveBool, PrimitiveString, PrimitiveNull, PrimitiveAny:
			return string(t.Name), true
		case PrimitiveUnknown:
			return "any", true
		}

	case *TypeParam:
		// Not known until the generic function is used; nothing to verify
		return "any", true

	case *ArrayType:
		elem, ok := c.describeCast(t.ElementType, visiting)
		return "[" + elem + "]", ok

	case *FunctionType:
		return c.describeFunctionCast(t, visiting)

	case *StructType:
		return c.describeStructCast(t, visiting)

	case *BehaviorType:
		var names []string
		for name := range c.structs {
			if c.registry.Implements(name, t.Name) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		return "{" + strings.Join(names, "|") + "}", true

	case *UnionType:
		if value := withoutNull(t); hasNull(t) && value != nil {
			if _, nested := value.(*UnionType); !nested {
				inner, ok := c.describeCast(value, visiting)
				return "?" + inner, ok
			}
		}
	}
	return "", false
}

// describeFunctionCast describes a function type: fn(D,D)->D, with ...D
// for a variadic parameter and void for no result. The same form records
// the type of every compiled function, so that a cast to a function type
// can compare the two.
func (c *Checker) describeFunctionCast(t *FunctionType, visiting map[string]bool) (string, bool) {
	params := make([]string, len(t.Params))
	for i, param := range t.Params {
		descriptor, ok := c.describeCast(param, visiting)
		if !ok {
			return "", false
		}
		params[i] = descriptor
	}
	if t.IsVariadic && len(params) > 0 {
		params[len(params)-1] = "..." + params[len(params)-1]
	}

	result := "void"
	if t.ReturnType != nil && !t.ReturnType.Equals(Void) {
		descriptor, ok := c.describeCast(t.ReturnType, visiting)
		if !ok {
			return "", false
		}
		result = descriptor
	}
	return "fn(" + strings.Join(params, ",") + ")->" + result, true
}

// recordSignature stores the type of a function for the code generator,
// which attaches it to the compiled function.
func (c *Checker) recordSignature(node ast.Node, fn *ast.FnDecl) {
	ft, ok := c.resolveFnType(fn).(*FunctionType)
	if !ok {
		return
	}
	if descriptor, ok := c.castDescriptor(ft); ok {
		c.info.Signatures[node] = descriptor
	}
}

// describeStructCast describes a struct type. For a generic struct it adds
// the fields whose type involves a type parameter.
func (c *Checker) describeStructCast(t *StructType, visiting map[string]bool) (string, bool) {
	decl := c.structs[t.Name]
	key := describe(t)
	if decl == nil || len(decl.TypeParams) == 0 || len(t.TypeArgs) == 0 || visiting[key] {
		return "{" + t.Name + "}", true
	}

	visiting[key] = true
	defer delete(visiting, key)

	var fields []string
	for _, field := range decl.Fields {
		if !mentionsAny(field.Type, decl.TypeParams) {
			continue
		}
		fieldType, _ := c.fieldType(t, field.Name)
		descriptor, ok := c.describeCast(fieldType, visiting)
		if !ok {
			return "", false
		}
		fields = append(fields, field.Name+"="+descriptor)
	}

	if len(fields) == 0 {
		return "{" + t.Name + "}", true
	}
	return "{" + t.Name + ":" + strings.Join(fields, ",") + "}", true
}

// mentionsAny reports whether a type annotation uses one of the names.
func mentionsAny(t *ast.Type, names []string) bool {
	if t == nil {
		return false
	}
	for _, name := range names {
		if t.Kind == ast.TypeBasic && t.Name == name {
			return true
		}
	}
	for _, sub := range append(append(append([]*ast.Type{t.ElemType, t.ReturnType}, t.Generics...), t.UnionTypes...), t.ParamTypes...) {
		if mentionsAny(sub, names) {
			return true
		}
	}
	return false
}

// comparable reports whether == and != make sense between two types.
func (c *Checker) comparable(left, right Type) bool {
	// Anything may be tested against null: a variable known to be non-null
	// at this point is often still tested by code written before it was
	if left.Equals(Null) || right.Equals(Null) {
		return true
	}
	return c.assignable(left, right) || c.assignable(right, left)
}

func isUnknown(t Type) bool {
	return t != nil && t.Equals(Unknown)
}

// hasNull reports whether a union admits null.
func hasNull(u *UnionType) bool {
	for _, member := range u.Types {
		if member.Equals(Null) {
			return true
		}
	}
	return false
}

// optional returns the type T?, that is T | null.
func optional(t Type) Type {
	if t.Equals(Null) || isUnknown(t) || t.Equals(Any) {
		return t
	}
	if u, ok := t.(*UnionType); ok {
		if hasNull(u) {
			return u
		}
		return NewUnion(append(append([]Type{}, u.Types...), Null)...)
	}
	return NewUnion(t, Null)
}

// withoutNull returns what remains of a type once null is ruled out, or nil
// if nothing does (the type was just null).
func withoutNull(t Type) Type {
	if t.Equals(Null) {
		return nil
	}
	u, ok := t.(*UnionType)
	if !ok {
		return t
	}

	var rest []Type
	for _, member := range u.Types {
		if !member.Equals(Null) {
			rest = append(rest, member)
		}
	}
	switch len(rest) {
	case 0:
		return nil
	case 1:
		return rest[0]
	}
	return NewUnion(rest...)
}

// describe renders a type the way it is written in source, for messages.
func describe(t Type) string {
	switch t := t.(type) {
	case *StructType:
		if len(t.TypeArgs) == 0 {
			return t.Name
		}
		args := make([]string, len(t.TypeArgs))
		for i, arg := range t.TypeArgs {
			args[i] = describe(arg)
		}
		return t.Name + "<" + strings.Join(args, ", ") + ">"

	case *BehaviorType:
		return t.Name

	case *ArrayType:
		return "[" + describe(t.ElementType) + "]"

	case *UnionType:
		if rest := withoutNull(t); rest != nil && hasNull(t) {
			if _, nested := rest.(*UnionType); !nested {
				return describe(rest) + "?"
			}
		}
		parts := make([]string, len(t.Types))
		for i, member := range t.Types {
			parts[i] = describe(member)
		}
		return strings.Join(parts, " | ")

	case *FunctionType:
		params := make([]string, len(t.Params))
		for i, param := range t.Params {
			params[i] = describe(param)
		}
		if t.IsVariadic && len(params) > 0 {
			params[len(params)-1] = "..." + params[len(params)-1]
		}
		return "fn(" + strings.Join(params, ", ") + ") -> " + describe(t.ReturnType)
	}
	return t.String()
}

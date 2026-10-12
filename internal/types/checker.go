package types

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/token"
)

const (
	ErrTypeMismatch      = "E2001"
	ErrReturnOutsideFn   = "E2002"
	ErrReturnMismatch    = "E2003"
	ErrMissingReturn     = "E2004"
	ErrInvalidCondition  = "E2005"
	ErrInvalidOperation  = "E2006"
	ErrInvalidUnary      = "E2007"
	ErrNotFunction       = "E2008"
	ErrArgCount          = "E2009"
	ErrArgType           = "E2010"
	ErrNotIterable       = "E2011"
	ErrMissingMethod     = "E2012"
	ErrSignatureMismatch = "E2013"
	ErrUnknownField      = "E2014"
)

type Checker struct {
	prog        *ast.Program
	table       *resolve.Table
	errs        *errors.Handler
	info        *Info
	sourceLines map[int]string

	// Context for return type checking
	currentFn *ast.FnDecl

	// Type parameters in scope. They are only bound by monomorphization,
	// so until then they are treated as Any.
	typeParams map[string]bool
}

func NewChecker(prog *ast.Program, table *resolve.Table, errs *errors.Handler) *Checker {
	return &Checker{
		prog:        prog,
		table:       table,
		errs:        errs,
		info:        NewInfo(),
		sourceLines: make(map[int]string),
		typeParams:  make(map[string]bool),
	}
}

func (c *Checker) SetSourceLines(lines map[int]string) {
	c.sourceLines = lines
}

func (c *Checker) Check() *Info {
	for _, decl := range c.prog.Decls {
		c.checkDecl(decl)
	}
	return c.info
}

func (c *Checker) checkDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FnDecl:
		c.checkFnDecl(d)
	case *ast.StructDecl:
		c.checkStructDecl(d)
	case *ast.EnumDecl:
		c.checkEnumDecl(d)
	case *ast.TraitDecl:
		c.checkTraitDecl(d)
	case *ast.ImplDecl:
		c.checkImplDecl(d)
	}
}

func (c *Checker) checkFnDecl(d *ast.FnDecl) {
	prevFn := c.currentFn
	c.currentFn = d
	defer func() { c.currentFn = prevFn }()
	defer c.bindTypeParams(d.TypeParams)()

	if d.Body != nil {
		c.checkBlockStmt(d.Body)
	}
}

func (c *Checker) checkStructDecl(d *ast.StructDecl) {
	// Check that all field types are valid
	for _, field := range d.Fields {
		if field.Type != nil {
			c.resolveType(field.Type)
			// The resolveType will report errors if the type is undefined
		}
	}
}

func (c *Checker) checkEnumDecl(d *ast.EnumDecl) {
	// Check that all variant types are valid
	for _, variant := range d.Variants {
		for _, valType := range variant.Values {
			if valType != nil {
				c.resolveType(valType)
				// The resolveType will report errors if the type is undefined
			}
		}
	}
}

func (c *Checker) checkTraitDecl(d *ast.TraitDecl) {
	// Check all trait methods
	for _, method := range d.Methods {
		// Trait methods can be abstract (no body) or have default implementations
		// We check the signature is valid
		for _, param := range method.Params {
			if param.Type != nil {
				c.resolveType(param.Type)
			}
		}
		if method.ReturnType != nil {
			c.resolveType(method.ReturnType)
		}
		// If the method has a body, check it
		if method.Body != nil {
			c.checkFnDecl(method)
		}
	}
}

func (c *Checker) checkImplDecl(d *ast.ImplDecl) {
	// Check that all impl methods are valid functions
	for _, method := range d.Methods {
		c.checkFnDecl(method)
	}

	// If this is a trait implementation (not just inherent impl), verify completeness
	if d.TraitName != "" {
		traitDecl := c.findTraitDecl(d.TraitName)
		if traitDecl != nil {
			c.verifyTraitImpl(d, traitDecl)
		}
	}
}

// findTraitDecl finds the trait declaration by name in the program
func (c *Checker) findTraitDecl(name string) *ast.TraitDecl {
	for _, decl := range c.prog.Decls {
		if traitDecl, ok := decl.(*ast.TraitDecl); ok {
			if traitDecl.Name == name {
				return traitDecl
			}
		}
	}
	return nil
}

// verifyTraitImpl verifies that an impl provides all required methods and signatures match
func (c *Checker) verifyTraitImpl(implDecl *ast.ImplDecl, traitDecl *ast.TraitDecl) {
	// Build a map of implemented methods
	implMethods := make(map[string]*ast.FnDecl)
	for _, method := range implDecl.Methods {
		implMethods[method.Name] = method
	}

	// Check each trait method
	for _, traitMethod := range traitDecl.Methods {
		implMethod, ok := implMethods[traitMethod.Name]
		if !ok {
			c.errorfWithSuggestion(
				implDecl.Token(),
				ErrMissingMethod,
				fmt.Sprintf("Implement method '%s' for trait '%s'", traitMethod.Name, traitDecl.Name),
				"implementation missing required method '%s' from trait '%s'",
				traitMethod.Name,
				traitDecl.Name,
			)
			continue
		}

		// Verify method signatures match
		c.verifyMethodSignature(implMethod, traitMethod, traitDecl.Name)
	}
}

// verifyMethodSignature checks that an impl method signature matches the trait method
func (c *Checker) verifyMethodSignature(implMethod, traitMethod *ast.FnDecl, traitName string) {
	// Check parameter count
	if len(implMethod.Params) != len(traitMethod.Params) {
		c.errorfWithSuggestion(
			implMethod.Token(),
			ErrSignatureMismatch,
			fmt.Sprintf("Method should have %d parameters to match trait '%s'", len(traitMethod.Params), traitName),
			"method '%s' has %d parameters, trait requires %d",
			implMethod.Name,
			len(implMethod.Params),
			len(traitMethod.Params),
		)
		return
	}

	// Check each parameter type
	for i := 0; i < len(implMethod.Params); i++ {
		implParamType := c.resolveType(implMethod.Params[i].Type)
		traitParamType := c.resolveType(traitMethod.Params[i].Type)

		if !implParamType.Equals(traitParamType) {
			c.errorfWithSuggestion(
				implMethod.Params[i].Token(),
				ErrSignatureMismatch,
				fmt.Sprintf("Parameter type should be %s to match trait '%s'", traitParamType, traitName),
				"parameter '%s' has type %s, trait requires %s",
				implMethod.Params[i].Name,
				implParamType,
				traitParamType,
			)
		}
	}

	// Check return type
	implReturnType := c.resolveType(implMethod.ReturnType)
	traitReturnType := c.resolveType(traitMethod.ReturnType)

	if !implReturnType.Equals(traitReturnType) {
		c.errorfWithSuggestion(
			implMethod.Token(),
			ErrSignatureMismatch,
			fmt.Sprintf("Return type should be %s to match trait '%s'", traitReturnType, traitName),
			"method '%s' returns %s, trait requires %s",
			implMethod.Name,
			implReturnType,
			traitReturnType,
		)
	}
}

func (c *Checker) checkStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		c.checkBlockStmt(s)
	case *ast.LetStmt:
		c.checkLetStmt(s)
	case *ast.ReturnStmt:
		c.checkReturnStmt(s)
	case *ast.IfStmt:
		c.checkIfStmt(s)
	case *ast.WhileStmt:
		c.checkWhileStmt(s)
	case *ast.ForStmt:
		c.checkForStmt(s)
	case *ast.ExprStmt:
		c.checkExpr(s.Expr)
	case *ast.MatchStmt:
		c.checkMatchStmt(s)
	}
}

func (c *Checker) checkBlockStmt(s *ast.BlockStmt) {
	for _, stmt := range s.Stmts {
		c.checkStmt(stmt)
	}
}

func (c *Checker) checkLetStmt(s *ast.LetStmt) {
	valType := c.checkExpr(s.Value)
	if valType == nil {
		return
	}

	if s.Type != nil {
		declType := c.resolveType(s.Type)
		if !c.assignable(declType, valType) {
			c.errorfWithSuggestion(s.Value.Token(), ErrTypeMismatch, fmt.Sprintf("Ensure the value is of type %s", declType), "cannot assign type %s to %s", valType, declType)
		}
	}
}

func (c *Checker) checkReturnStmt(s *ast.ReturnStmt) {
	if c.currentFn == nil {
		c.errorf(s.Token(), ErrReturnOutsideFn, "return statement outside of function")
		return
	}

	var returnType Type = Void
	if c.currentFn.ReturnType != nil {
		returnType = c.resolveType(c.currentFn.ReturnType)
	}

	if s.Value != nil {
		valType := c.checkExpr(s.Value)
		if valType != nil {
			if !c.assignable(returnType, valType) {
				c.errorfWithSuggestion(s.Value.Token(), ErrReturnMismatch, fmt.Sprintf("Ensure the return value is of type %s", returnType), "cannot return type %s from function returning %s", valType, returnType)
			}
		}
	} else {
		if !returnType.Equals(Void) {
			c.errorfWithSuggestion(s.Token(), ErrMissingReturn, fmt.Sprintf("Add a return statement with a value of type %s", returnType), "missing return value in function returning %s", returnType)
		}
	}
}

func (c *Checker) checkIfStmt(s *ast.IfStmt) {
	condType := c.checkExpr(s.Cond)
	if condType != nil && !condType.Equals(Bool) && !isAny(condType) {
		c.errorfWithSuggestion(s.Cond.Token(), ErrInvalidCondition, "Condition must evaluate to a boolean", "condition must be bool, got %s", condType)
	}

	c.checkBlockStmt(s.Then)
	if s.Else != nil {
		if elseBlock, ok := s.Else.(*ast.BlockStmt); ok {
			c.checkBlockStmt(elseBlock)
		} else if elseIf, ok := s.Else.(*ast.IfStmt); ok {
			c.checkIfStmt(elseIf)
		}
	}
}

func (c *Checker) checkWhileStmt(s *ast.WhileStmt) {
	condType := c.checkExpr(s.Cond)
	if condType != nil && !condType.Equals(Bool) && !isAny(condType) {
		c.errorfWithSuggestion(s.Cond.Token(), ErrInvalidCondition, "Condition must evaluate to a boolean", "condition must be bool, got %s", condType)
	}
	c.checkBlockStmt(s.Body)
}

func (c *Checker) checkForStmt(s *ast.ForStmt) {
	// Check that the iterable is actually iterable (array or string)
	iterType := c.checkExpr(s.Iterable)
	if iterType != nil {
		switch t := iterType.(type) {
		case *ArrayType:
			// Valid: can iterate over arrays
			// The loop variable will have the element type
		case *PrimitiveType:
			// Only String is iterable among primitives (Any is checked at runtime)
			if !t.Equals(String) && !isAny(t) {
				c.errorfWithSuggestion(s.Iterable.Token(), ErrNotIterable, "Use an array or string in for loops", "cannot iterate over type %s", iterType)
			}
		default:
			c.errorfWithSuggestion(s.Iterable.Token(), ErrNotIterable, "Use an array or string in for loops", "cannot iterate over type %s", iterType)
		}
	}
	c.checkBlockStmt(s.Body)
}

func (c *Checker) checkMatchStmt(s *ast.MatchStmt) {
	c.checkExpr(s.Cond)

	for _, matchCase := range s.Cases {
		// Check the pattern (if it's an expression)
		if pattern, ok := matchCase.Pattern.(ast.Expr); ok {
			c.checkExpr(pattern)
		}

		// Check the body - can be either BlockStmt or Expr
		switch body := matchCase.Body.(type) {
		case *ast.BlockStmt:
			c.checkBlockStmt(body)
		case ast.Expr:
			c.checkExpr(body)
		}
	}
}

func (c *Checker) checkExpr(expr ast.Expr) Type {
	if expr == nil {
		return nil
	}

	var t Type
	switch e := expr.(type) {
	case *ast.Literal:
		t = c.checkLiteral(e)
	case *ast.Ident:
		t = c.checkIdent(e)
	case *ast.BinaryExpr:
		t = c.checkBinaryExpr(e)
	case *ast.UnaryExpr:
		t = c.checkUnaryExpr(e)
	case *ast.CallExpr:
		t = c.checkCallExpr(e)
	case *ast.StructInitExpr:
		t = c.checkStructInitExpr(e)
	case *ast.ArrayLiteralExpr:
		t = c.checkArrayLiteralExpr(e)
	case *ast.MemberExpr:
		t = c.checkMemberExpr(e)
	case *ast.IndexExpr:
		t = c.checkIndexExpr(e)
	case *ast.FnExpr:
		t = c.checkFnExpr(e)
	default:
		t = Any
	}

	if t != nil {
		c.info.Types[expr] = t
	}
	return t
}

func (c *Checker) checkLiteral(l *ast.Literal) Type {
	switch l.Kind {
	case "int":
		return Int
	case "float":
		return Float
	case "string":
		return String
	case "bool":
		return Bool
	case "null":
		return Any
	}
	return Any
}

func (c *Checker) checkIdent(id *ast.Ident) Type {
	sym := c.table.Defs[id]
	if sym == nil {
		// Error should have been reported by resolver, but we can report again or just return Any
		return Any
	}

	switch sym.Kind {
	case resolve.SymbolVar:
		// Find the declaration
		if decl, ok := sym.Decl.(*ast.LetStmt); ok {
			if decl.Type != nil {
				return c.resolveType(decl.Type)
			}
			if t, ok := c.info.Types[decl.Value]; ok {
				return t
			}
		} else if param, ok := sym.Decl.(*ast.Param); ok {
			return c.resolveType(param.Type)
		} else if loop, ok := sym.Decl.(*ast.ForStmt); ok {
			// Loop variable: element type of the iterable
			switch iter := c.info.Types[loop.Iterable].(type) {
			case *ArrayType:
				return iter.ElementType
			case *PrimitiveType:
				if iter.Equals(String) {
					return String
				}
			}
		} else if impl, ok := sym.Decl.(*ast.ImplDecl); ok {
			// Implicit `self` of an impl method
			return c.resolveType(&ast.Type{Kind: ast.TypeBasic, Name: impl.StructName})
		}
	case resolve.SymbolFunc:
		if fn, ok := sym.Decl.(*ast.FnDecl); ok {
			// Construct function type
			// Note: Generic functions are represented with type parameters.
			// For now, we construct a monomorphic function type.
			// Full generic type inference would require tracking type arguments
			// at call sites and performing substitution.
			return c.resolveFnType(fn)
		}
	}
	return Any
}

func (c *Checker) checkBinaryExpr(e *ast.BinaryExpr) Type {
	left := c.checkExpr(e.Left)
	right := c.checkExpr(e.Right)

	if left == nil || right == nil {
		return Any
	}

	switch e.Op {
	case "=":
		if !c.assignable(left, right) {
			c.errorfWithSuggestion(e.Right.Token(), ErrTypeMismatch, fmt.Sprintf("Ensure the value is of type %s", left), "cannot assign type %s to %s", right, left)
		}
		return Void
	case "??":
		return right
	}

	// Operands whose type is only known at runtime are checked by the VM
	if isAny(left) || isAny(right) {
		switch e.Op {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return Bool
		}
		return Any
	}

	// Basic arithmetic
	if left.Equals(Int) && right.Equals(Int) {
		switch e.Op {
		case "+", "-", "*", "/", "%":
			return Int
		case "==", "!=", "<", "<=", ">", ">=":
			return Bool
		}
	}

	if left.Equals(Float) && right.Equals(Float) {
		switch e.Op {
		case "+", "-", "*", "/":
			return Float
		case "==", "!=", "<", "<=", ">", ">=":
			return Bool
		}
	}

	if left.Equals(String) && right.Equals(String) {
		if e.Op == "+" {
			return String
		}
		switch e.Op {
		case "==", "!=", "<", "<=", ">", ">=":
			return Bool
		}
	}

	if (e.Op == "==" || e.Op == "!=") && left.Equals(right) {
		return Bool
	}

	if left.Equals(Bool) && right.Equals(Bool) {
		if e.Op == "&&" || e.Op == "||" || e.Op == "==" || e.Op == "!=" {
			return Bool
		}
	}

	c.errorf(e.Token(), ErrInvalidOperation, "operator %s not defined for types %s and %s", e.Op, left, right)
	return Any
}

func (c *Checker) checkUnaryExpr(e *ast.UnaryExpr) Type {
	operand := c.checkExpr(e.Expr)
	if operand == nil || isAny(operand) {
		return Any
	}

	if e.Op == "-" {
		if operand.Equals(Int) || operand.Equals(Float) {
			return operand
		}
	} else if e.Op == "!" {
		if operand.Equals(Bool) {
			return Bool
		}
	}

	c.errorf(e.Token(), ErrInvalidUnary, "operator %s not defined for type %s", e.Op, operand)
	return Any
}

func (c *Checker) checkCallExpr(e *ast.CallExpr) Type {
	funcType := c.checkExpr(e.Func)
	if funcType == nil {
		return Any
	}

	if funcType.Equals(Any) {
		// If the function type is Any (e.g. error in resolution), we can't check arguments.
		// But we should check the arguments themselves to ensure they are valid expressions.
		for _, arg := range e.Args {
			c.checkExpr(arg)
		}
		return Any
	}

	ft, ok := funcType.(*FunctionType)
	if !ok {
		c.errorf(e.Func.Token(), ErrNotFunction, "cannot call non-function type %s", funcType)
		return Any
	}

	// Check argument count
	if len(e.Args) != len(ft.Params) {
		if !ft.IsVariadic || len(e.Args) < len(ft.Params)-1 {
			c.errorf(e.Token(), ErrArgCount, "expected %d arguments, got %d", len(ft.Params), len(e.Args))
		}
	}

	// Check argument types
	for i, arg := range e.Args {
		argType := c.checkExpr(arg)
		if argType == nil {
			continue
		}

		var paramType Type
		if ft.IsVariadic && i >= len(ft.Params)-1 {
			paramType = ft.Params[len(ft.Params)-1]
		} else if i < len(ft.Params) {
			paramType = ft.Params[i]
		} else {
			// Extra arguments for non-variadic function (already reported error)
			continue
		}

		if !c.assignable(paramType, argType) {
			c.errorf(arg.Token(), ErrArgType, "cannot use type %s as argument of type %s", argType, paramType)
		}
	}

	return ft.ReturnType
}

func (c *Checker) checkStructInitExpr(e *ast.StructInitExpr) Type {
	decl := c.findStructDecl(e.Name)
	for name, value := range e.Fields {
		valType := c.checkExpr(value)
		if decl == nil || valType == nil || len(decl.TypeParams) > 0 {
			continue
		}
		field := findField(decl, name)
		if field == nil {
			c.errorf(value.Token(), ErrUnknownField, "struct %s has no field '%s'", e.Name, name)
			continue
		}
		if fieldType := c.resolveType(field.Type); !c.assignable(fieldType, valType) {
			c.errorf(value.Token(), ErrTypeMismatch, "cannot use type %s for field '%s' of type %s", valType, name, fieldType)
		}
	}
	return &StructType{Name: e.Name}
}

// checkMemberExpr types `expr.name`: the length of an array or string, or a
// struct field. Anything else (methods, generic structs) is left to runtime.
func (c *Checker) checkMemberExpr(e *ast.MemberExpr) Type {
	switch recv := c.checkExpr(e.Expr).(type) {
	case *ArrayType:
		if e.Name == "length" {
			return Int
		}
	case *PrimitiveType:
		if recv.Equals(String) && e.Name == "length" {
			return Int
		}
	case *StructType:
		decl := c.findStructDecl(recv.Name)
		if decl == nil || len(decl.TypeParams) > 0 {
			return Any
		}
		if field := findField(decl, e.Name); field != nil {
			return c.resolveType(field.Type)
		}
	}
	return Any
}

// checkIndexExpr types `expr[index]`.
func (c *Checker) checkIndexExpr(e *ast.IndexExpr) Type {
	recv := c.checkExpr(e.Expr)
	index := c.checkExpr(e.Index)
	if index != nil && !index.Equals(Int) && !isAny(index) {
		c.errorfWithSuggestion(e.Index.Token(), ErrTypeMismatch, "Use an Int as index", "index must be int, got %s", index)
	}

	switch t := recv.(type) {
	case *ArrayType:
		return t.ElementType
	case *PrimitiveType:
		if t.Equals(String) {
			return String
		}
	}
	return Any
}

// checkFnExpr types an anonymous function and checks its body.
func (c *Checker) checkFnExpr(e *ast.FnExpr) Type {
	// Reuse the FnDecl machinery so `return` is checked against the lambda
	fn := &ast.FnDecl{BaseNode: e.BaseNode, Params: e.Params, ReturnType: e.ReturnType, Body: e.Body}
	c.checkFnDecl(fn)
	return c.resolveFnType(fn)
}

func (c *Checker) findStructDecl(name string) *ast.StructDecl {
	for _, decl := range c.prog.Decls {
		if structDecl, ok := decl.(*ast.StructDecl); ok && structDecl.Name == name {
			return structDecl
		}
	}
	return nil
}

func findField(decl *ast.StructDecl, name string) *ast.Field {
	for _, field := range decl.Fields {
		if field.Name == name {
			return field
		}
	}
	return nil
}

func (c *Checker) checkArrayLiteralExpr(e *ast.ArrayLiteralExpr) Type {
	// Check all elements and infer the array type
	if len(e.Elements) == 0 {
		// Empty array - we can't infer the type, return array of Any
		return NewArray(Any)
	}

	// Check the first element to determine the array type
	firstType := c.checkExpr(e.Elements[0])
	if firstType == nil {
		return NewArray(Any)
	}

	// Check that all other elements have compatible types
	for i := 1; i < len(e.Elements); i++ {
		elemType := c.checkExpr(e.Elements[i])
		if elemType != nil && !c.assignable(firstType, elemType) {
			c.errorfWithSuggestion(
				e.Elements[i].Token(),
				ErrTypeMismatch,
				fmt.Sprintf("All array elements should be of type %s", firstType),
				"array element has type %s, expected %s",
				elemType,
				firstType,
			)
		}
	}

	return NewArray(firstType)
}

// Helpers

func (c *Checker) resolveType(n *ast.Type) Type {
	if n == nil {
		return Void
	}
	switch n.Kind {
	case ast.TypeBasic:
		if c.typeParams[n.Name] {
			return Any
		}
		switch n.Name {
		case "Int", "int":
			return Int
		case "Float", "float":
			return Float
		case "Bool", "bool":
			return Bool
		case "String", "string":
			return String
		case "Void", "void":
			return Void
		case "Any", "any":
			return Any
		case "Self":
			return Self
		default:
			if c.findTraitDecl(n.Name) != nil {
				// Any struct implementing the trait fits; methods are
				// looked up on the value at runtime.
				return Any
			}
			// Look up type name in table?
			// Again, `ast.Type` has `Name string`. No `Ident`.
			// This is a limitation.
			return &StructType{Name: n.Name}
		}
	case ast.TypeArray, ast.TypeVariadic:
		// A variadic parameter is seen as an array inside the function
		elem := c.resolveType(n.ElemType)
		return NewArray(elem)
	case ast.TypeOption:
		// T? also admits null, which only the runtime can tell apart
		return Any
	case ast.TypeFunction:
		var params []Type
		for _, p := range n.ParamTypes {
			params = append(params, c.resolveType(p))
		}
		var ret Type = Void
		if n.ReturnType != nil {
			ret = c.resolveType(n.ReturnType)
		}
		return NewFunction(params, ret)
	case ast.TypeUnion:
		var types []Type
		for _, t := range n.UnionTypes {
			types = append(types, c.resolveType(t))
		}
		return NewUnion(types...)
	}
	return Any
}

func (c *Checker) resolveFnType(fn *ast.FnDecl) Type {
	defer c.bindTypeParams(fn.TypeParams)()

	variadic := false
	var params []Type
	for i, p := range fn.Params {
		if p.Type != nil && p.Type.Kind == ast.TypeVariadic && i == len(fn.Params)-1 {
			// Callers pass the elements one by one
			variadic = true
			params = append(params, c.resolveType(p.Type.ElemType))
			continue
		}
		params = append(params, c.resolveType(p.Type))
	}

	var ret Type = Void
	if fn.ReturnType != nil {
		ret = c.resolveType(fn.ReturnType)
	}

	if variadic {
		return NewVariadicFunction(params, ret)
	}
	return NewFunction(params, ret)
}

// bindTypeParams brings type parameters into scope and returns a function
// that restores the previous scope.
func (c *Checker) bindTypeParams(names []string) func() {
	var added []string
	for _, name := range names {
		if !c.typeParams[name] {
			c.typeParams[name] = true
			added = append(added, name)
		}
	}
	return func() {
		for _, name := range added {
			delete(c.typeParams, name)
		}
	}
}

// assignable reports whether a value of type source can be stored in target.
// A source of type Any is only known at runtime, so it is accepted here.
func (c *Checker) assignable(target, source Type) bool {
	if isAny(source) || isAny(target) || target.Equals(Self) || source.Equals(Self) {
		return true
	}

	// Any may also sit inside a type: [] is an [any] that fits every array,
	// and a generic function has Any where its type parameters were.
	switch t := target.(type) {
	case *ArrayType:
		if src, ok := source.(*ArrayType); ok {
			return c.assignable(t.ElementType, src.ElementType)
		}
	case *FunctionType:
		src, ok := source.(*FunctionType)
		if ok && len(t.Params) == len(src.Params) && t.IsVariadic == src.IsVariadic {
			for i := range t.Params {
				if !c.assignable(t.Params[i], src.Params[i]) {
					return false
				}
			}
			return c.assignable(t.ReturnType, src.ReturnType)
		}
	}
	return IsAssignable(target, source, nil)
}

func isAny(t Type) bool {
	return t != nil && t.Equals(Any)
}

func (c *Checker) errorf(tok token.Token, code string, format string, args ...interface{}) {
	c.errs.ReportErrorFromToken(code, fmt.Sprintf(format, args...), tok, c.sourceLines)
}

func (c *Checker) errorfWithSuggestion(tok token.Token, code string, suggestion string, format string, args ...interface{}) {
	c.errs.ReportErrorWithSuggestion(code, fmt.Sprintf(format, args...), suggestion, tok, c.sourceLines)
}

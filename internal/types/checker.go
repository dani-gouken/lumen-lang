package types

import (
	"fmt"
	"sort"
	"strings"

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
	ErrUnknownMethod     = "E2015"
	ErrUndefinedStruct   = "E2016"
	ErrPossiblyNull      = "E2017"
	ErrMissingField      = "E2018"
	ErrDuplicateMethod   = "E2019"
	ErrUntypedEmptyArray = "E2020"
	ErrInvalidCast       = "E2021"
	ErrAssignToConst     = "E2022"
	ErrUnreachableCode   = "E2023"
	ErrMethodTypeArgs    = "E2024"
)

type Checker struct {
	prog        *ast.Program
	table       *resolve.Table
	errs        *errors.Handler
	info        *Info
	sourceLines map[int]string

	// Context for return type checking
	currentFn *ast.FnDecl

	// Type parameters in scope, with the type each one is bound to. Inside
	// a generic declaration a parameter is in scope but unbound (nil): it is
	// an opaque type that only fits itself. At a use with explicit type
	// arguments, the parameters are bound to those arguments.
	typeParams map[string]Type

	// What `Self` stands for: the struct inside an impl, nil elsewhere
	selfType Type

	// Declarations indexed by name, and which structs implement which traits
	structs  map[string]*ast.StructDecl
	traits   map[string]*ast.TraitDecl
	methods  map[string]map[string]*ast.FnDecl // struct -> method -> declaration
	impls    map[*ast.FnDecl]*ast.ImplDecl     // method -> the impl block it is in
	registry *ImplRegistry

	// Flow typing (see flow.go): variables currently known to be non-null,
	// and how often each variable has been assigned so far
	narrowed      map[*resolve.Symbol]Type
	narrowedPaths map[string]pathFact // field paths such as `node.next`
	pathEpoch     int                 // advanced whenever field facts are dropped
	writes        map[*resolve.Symbol]int
	shared        map[*resolve.Symbol]bool // assigned by a lambda that does not own them

	// Which functions may assign a field when called (see findFieldWriters)
	fieldWriters map[*ast.FnDecl]bool
	methodsNamed map[string][]*ast.FnDecl
	fieldNames   map[string]bool

	// The type the surrounding code expects of the call being checked,
	// used to infer type arguments the call's own arguments leave open
	expected Type
}

func NewChecker(prog *ast.Program, table *resolve.Table, errs *errors.Handler) *Checker {
	return &Checker{
		prog:          prog,
		table:         table,
		errs:          errs,
		info:          NewInfo(),
		sourceLines:   make(map[int]string),
		typeParams:    make(map[string]Type),
		structs:       make(map[string]*ast.StructDecl),
		traits:        make(map[string]*ast.TraitDecl),
		methods:       make(map[string]map[string]*ast.FnDecl),
		impls:         make(map[*ast.FnDecl]*ast.ImplDecl),
		registry:      NewImplRegistry(),
		narrowed:      make(map[*resolve.Symbol]Type),
		narrowedPaths: make(map[string]pathFact),
		writes:        make(map[*resolve.Symbol]int),
		shared:        make(map[*resolve.Symbol]bool),

		fieldWriters: make(map[*ast.FnDecl]bool),
		methodsNamed: make(map[string][]*ast.FnDecl),
		fieldNames:   make(map[string]bool),
	}
}

func (c *Checker) SetSourceLines(lines map[int]string) {
	c.sourceLines = lines
}

func (c *Checker) Check() *Info {
	c.collectDecls()
	c.findFieldWriters()

	for _, decl := range c.prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			c.findSharedVariables(d.Body)
		case *ast.ImplDecl:
			for _, method := range d.Methods {
				c.findSharedVariables(method.Body)
			}
		}
	}

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

	if d.Body == nil {
		return
	}
	c.recordSignature(d, d)

	// A function body may run at any later time, so it cannot rely on what
	// is known about outer variables here; and what it assigns is no longer
	// known afterwards.
	outer := c.flowSnapshot()
	c.narrowed = make(map[*resolve.Symbol]Type)
	c.narrowedPaths = make(map[string]pathFact)
	c.checkBlockStmt(d.Body)
	c.flowRestore(outer)

	if d.ReturnType != nil && !alwaysReturns(d.Body) {
		if returnType := c.resolveType(d.ReturnType); !returnType.Equals(Void) && !isUnknown(returnType) {
			what := "function"
			if d.Name != "" {
				what = "function '" + d.Name + "'"
			}
			c.errorfWithSuggestion(d.Token(), ErrMissingReturn,
				fmt.Sprintf("Return a value of type %s at the end of every path", describe(returnType)),
				"%s can reach its end without returning a value", what)
		}
	}
}

func (c *Checker) checkStructDecl(d *ast.StructDecl) {
	defer c.bindTypeParams(d.TypeParams)()

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
	// The struct's type parameters are in scope for the methods
	defer c.bindTypeParams(d.TypeParams)()

	// Inside the impl, `Self` and `self` refer to the struct
	c.withSelf(c.implSelfType(d), func() {
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
	})
}

// implSelfType returns the type of `self` in an impl block: the struct,
// with the impl's type parameters as its type arguments.
func (c *Checker) implSelfType(d *ast.ImplDecl) Type {
	self := &StructType{Name: d.StructName, BaseName: d.StructName}
	for _, name := range d.TypeParams {
		self.TypeArgs = append(self.TypeArgs, c.resolveType(&ast.Type{Kind: ast.TypeBasic, Name: name}))
	}
	if _, isStruct := c.structs[d.StructName]; !isStruct {
		// impl for a builtin type
		return c.resolveType(&ast.Type{Kind: ast.TypeBasic, Name: d.StructName})
	}
	return self
}

// declaredTargetType types the target of an assignment to a field or an
// element. Like for variables, the declared type counts: a field known to
// be non-null here can be set back to null.
func (c *Checker) declaredTargetType(target ast.Expr) Type {
	key, _, isPath := c.fieldPath(target)
	fact, narrowed := c.narrowedPaths[key]
	if !isPath || !narrowed {
		return c.checkExpr(target)
	}

	delete(c.narrowedPaths, key)
	typ := c.checkExpr(target)
	c.narrowedPaths[key] = fact
	return typ
}

// checkAssignment types `target = value`.
func (c *Checker) checkAssignment(e *ast.BinaryExpr) Type {
	var left Type
	var sym *resolve.Symbol

	if ident, ok := e.Left.(*ast.Ident); ok {
		// Assigning is checked against the declared type: a variable known
		// to be non-null here may still be set back to null
		left = c.declaredIdentType(ident)
		c.info.Types[ident] = left
		sym = c.table.Defs[ident]

		if sym != nil {
			if decl, ok := sym.Decl.(*ast.LetStmt); ok && decl.IsConst {
				c.errorfWithSuggestion(ident.Token(), ErrAssignToConst,
					fmt.Sprintf("Declare it with 'let %s' to make it assignable", ident.Name),
					"cannot assign to constant '%s'", ident.Name)
			}
		}
	} else {
		left = c.declaredTargetType(e.Left)
	}

	var right Type
	if left != nil {
		right = c.checkExprAs(e.Right, left)
	} else {
		right = c.checkExpr(e.Right)
	}

	if _, toField := e.Left.(*ast.MemberExpr); toField {
		// A field changed: the same value may be reachable through other
		// paths
		c.forgetFields()
	}

	if sym != nil && sym.Kind == resolve.SymbolVar {
		c.recordWrite(sym)
		// A value that cannot be null makes an optional variable non-null
		if left != nil && right != nil && !isUnknown(right) && !c.shared[sym] {
			if value := withoutNull(left); value != nil && !value.Equals(left) && c.assignable(value, right) {
				c.narrowed[sym] = value
			}
		}
	}

	if left != nil && right != nil && !c.assignable(left, right) {
		c.errorfWithSuggestion(e.Right.Token(), ErrTypeMismatch, fmt.Sprintf("Ensure the value is of type %s", describe(left)), "cannot assign type %s to %s", right, left)
	}
	return Void
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

	if len(implMethod.TypeParams) != len(traitMethod.TypeParams) {
		c.errorfWithSuggestion(
			implMethod.Token(),
			ErrSignatureMismatch,
			fmt.Sprintf("Declare it as %s%s to match trait '%s'", implMethod.Name, typeParamList(traitMethod.TypeParams), traitName),
			"method '%s' has %d type parameters, trait requires %d",
			implMethod.Name,
			len(implMethod.TypeParams),
			len(traitMethod.TypeParams),
		)
		return
	}

	// The names of type parameters are placeholders: fn convert<U>(v: U)
	// implements fn convert<T>(v: T). Both signatures are therefore
	// resolved with the trait's names, the impl's parameters standing for
	// the trait's in the same position.
	signature := func(method *ast.FnDecl, typeParams map[string]Type) ([]Type, Type) {
		defer c.bind(typeParams)()
		var params []Type
		for _, param := range method.Params {
			params = append(params, c.resolveType(param.Type))
		}
		return params, c.resolveType(method.ReturnType)
	}

	traitScope := make(map[string]Type)
	implScope := make(map[string]Type)
	for i, name := range traitMethod.TypeParams {
		traitScope[name] = NewTypeParam(name)
		implScope[implMethod.TypeParams[i]] = NewTypeParam(name)
	}
	traitParams, traitReturnType := signature(traitMethod, traitScope)
	implParams, implReturnType := signature(implMethod, implScope)

	// Check each parameter type
	for i := range implParams {
		if !implParams[i].Equals(traitParams[i]) {
			c.errorfWithSuggestion(
				implMethod.Params[i].Token(),
				ErrSignatureMismatch,
				fmt.Sprintf("Parameter type should be %s to match trait '%s'", describe(traitParams[i]), traitName),
				"parameter '%s' has type %s, trait requires %s",
				implMethod.Params[i].Name,
				implParams[i],
				traitParams[i],
			)
		}
	}

	// Check return type
	if !implReturnType.Equals(traitReturnType) {
		c.errorfWithSuggestion(
			implMethod.Token(),
			ErrSignatureMismatch,
			fmt.Sprintf("Return type should be %s to match trait '%s'", describe(traitReturnType), traitName),
			"method '%s' returns %s, trait requires %s",
			implMethod.Name,
			implReturnType,
			traitReturnType,
		)
	}
}

// typeParamList renders type parameters as written: "<T, U>", or "".
func typeParamList(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "<" + strings.Join(names, ", ") + ">"
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
	// What a block learns about variables ends with the block
	state := c.flowSnapshot()
	defer c.flowRestore(state)

	warned := false
	for i, stmt := range s.Stmts {
		c.checkStmt(stmt)

		// Nothing after a statement that always leaves the block can run.
		// Reported once per block; the rest is still checked.
		if !warned && i+1 < len(s.Stmts) && (exits(stmt) || alwaysReturns(stmt)) {
			warned = true
			c.errs.ReportWarningFromToken(ErrUnreachableCode,
				"unreachable code: the statement before it always leaves this block",
				s.Stmts[i+1].Token(), c.sourceLines)
		}
	}
}

func (c *Checker) checkLetStmt(s *ast.LetStmt) {
	if s.Type == nil {
		c.checkExpr(s.Value)

		// Nothing says what an empty literal holds, and a variable of
		// unknown element type would accept and yield anything
		if lit, ok := s.Value.(*ast.ArrayLiteralExpr); ok && len(lit.Elements) == 0 {
			c.errorfWithSuggestion(s.Value.Token(), ErrUntypedEmptyArray,
				fmt.Sprintf("Give the variable a type: let %s: [Int] = [];", s.Name),
				"cannot infer the element type of '[]'")
		}
		return
	}

	declType := c.resolveType(s.Type)
	valType := c.checkExprAs(s.Value, declType)
	if valType != nil && !c.assignable(declType, valType) {
		c.errorfWithSuggestion(s.Value.Token(), ErrTypeMismatch, fmt.Sprintf("Ensure the value is of type %s", describe(declType)), "cannot assign type %s to %s", valType, declType)
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
		valType := c.checkExprAs(s.Value, returnType)
		if valType != nil {
			if !c.assignable(returnType, valType) {
				c.errorfWithSuggestion(s.Value.Token(), ErrReturnMismatch, fmt.Sprintf("Ensure the return value is of type %s", describe(returnType)), "cannot return type %s from function returning %s", valType, returnType)
			}
		}
	} else {
		if !returnType.Equals(Void) {
			c.errorfWithSuggestion(s.Token(), ErrMissingReturn, fmt.Sprintf("Add a return statement with a value of type %s", describe(returnType)), "missing return value in function returning %s", returnType)
		}
	}
}

func (c *Checker) checkIfStmt(s *ast.IfStmt) {
	condType := c.checkExpr(s.Cond)
	if condType != nil && !condType.Equals(Bool) && !isUnknown(condType) {
		c.errorfWithSuggestion(s.Cond.Token(), ErrInvalidCondition, "Condition must evaluate to a boolean", "condition must be bool, got %s", condType)
	}

	whenTrue := c.factsFrom(s.Cond, true)
	whenFalse := c.factsFrom(s.Cond, false)
	before := c.flowSnapshot()

	c.withNarrowing(whenTrue, func() { c.checkBlockStmt(s.Then) })
	if s.Else != nil {
		c.withNarrowing(whenFalse, func() {
			if elseBlock, ok := s.Else.(*ast.BlockStmt); ok {
				c.checkBlockStmt(elseBlock)
			} else if elseIf, ok := s.Else.(*ast.IfStmt); ok {
				c.checkIfStmt(elseIf)
			}
		})
	}

	// When one branch never continues, the code after the if is only
	// reached through the other one: `if x == null { return; }` leaves x
	// non-null from here on.
	thenExits := exits(s.Then)
	elseExits := s.Else != nil && exits(s.Else)
	if thenExits && !elseExits {
		c.narrowAfter(whenFalse, before)
	} else if elseExits && !thenExits {
		c.narrowAfter(whenTrue, before)
	}
}

func (c *Checker) checkWhileStmt(s *ast.WhileStmt) {
	condType := c.checkExpr(s.Cond)
	if condType != nil && !condType.Equals(Bool) && !isUnknown(condType) {
		c.errorfWithSuggestion(s.Cond.Token(), ErrInvalidCondition, "Condition must evaluate to a boolean", "condition must be bool, got %s", condType)
	}

	// The body runs again after its own assignments, so only the condition,
	// which is tested before every iteration, can be relied on
	c.forgetAssignedIn(s.Body)
	c.withNarrowing(c.factsFrom(s.Cond, true), func() { c.checkBlockStmt(s.Body) })
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
			// Only String is iterable among primitives
			if !t.Equals(String) && !isUnknown(t) {
				c.errorfWithSuggestion(s.Iterable.Token(), ErrNotIterable, "Use an array or string in for loops", "cannot iterate over type %s", iterType)
			}
		default:
			c.errorfWithSuggestion(s.Iterable.Token(), ErrNotIterable, "Use an array or string in for loops", "cannot iterate over type %s", iterType)
		}
	}
	c.forgetAssignedIn(s.Body)
	c.checkBlockStmt(s.Body)
}

func (c *Checker) checkMatchStmt(s *ast.MatchStmt) {
	condType := c.checkExpr(s.Cond)

	for _, matchCase := range s.Cases {
		known := newFacts()

		switch pattern := matchCase.Pattern.(type) {
		case *ast.Type:
			// `case Int:` asks what the value is; inside the case it is one
			target := c.resolveType(pattern)
			if condType != nil && !isUnknown(condType) && !isUnknown(target) &&
				!c.assignable(target, condType) && !c.assignable(condType, target) {
				c.errorf(pattern.Token(), ErrInvalidCast, "%s can never be %s", condType, target)
			} else if _, ok := c.castDescriptor(target); !ok {
				c.errorf(pattern.Token(), ErrInvalidCast, "a test for %s cannot be made", target)
			} else {
				c.learn(s.Cond, target, known)
			}
		case ast.Expr:
			patternType := c.checkExpr(pattern)
			if condType != nil && patternType != nil && !c.comparable(condType, patternType) {
				c.errorf(pattern.Token(), ErrTypeMismatch, "pattern of type %s can never match a value of type %s", patternType, condType)
			}
		}

		// Check the body - can be either BlockStmt or Expr
		c.withNarrowing(known, func() {
			switch body := matchCase.Body.(type) {
			case *ast.BlockStmt:
				c.checkBlockStmt(body)
			case ast.Expr:
				c.checkExpr(body)
			}
		})
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
	case *ast.CastExpr:
		t = c.checkCastExpr(e)
	case *ast.TypeTestExpr:
		t = c.checkTypeTest(e)
	default:
		t = Unknown
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
		return Null
	}
	return Unknown
}

func (c *Checker) checkIdent(id *ast.Ident) Type {
	if sym := c.table.Defs[id]; sym != nil {
		if narrowed, ok := c.narrowed[sym]; ok {
			return narrowed
		}
	}
	return c.declaredIdentType(id)
}

// declaredIdentType returns the type a name was declared with, ignoring
// anything flow typing knows about it. This is the type an assignment to
// the name is checked against.
func (c *Checker) declaredIdentType(id *ast.Ident) Type {
	sym := c.table.Defs[id]
	if sym == nil {
		// The resolver already reported the undefined name
		return Unknown
	}

	switch sym.Kind {
	case resolve.SymbolVar:
		// Find the declaration
		switch decl := sym.Decl.(type) {
		case *ast.LetStmt:
			if decl.Type != nil {
				return c.resolveType(decl.Type)
			}
			if t, ok := c.info.Types[decl.Value]; ok {
				return t
			}
		case *ast.Param:
			return c.resolveType(decl.Type)
		case *ast.ForStmt:
			// Loop variable: element type of the iterable
			switch iter := c.info.Types[decl.Iterable].(type) {
			case *ArrayType:
				return iter.ElementType
			case *PrimitiveType:
				if iter.Equals(String) {
					return String
				}
			}
		case *ast.ImplDecl:
			// Implicit `self` of an impl method
			return c.implSelfType(decl)
		}
	case resolve.SymbolFunc:
		if fn, ok := sym.Decl.(*ast.FnDecl); ok {
			return c.functionType(fn, id)
		}
	}
	return Unknown
}

// functionType returns the type of a function named by an identifier. For a
// generic function the identifier's type arguments are substituted for the
// type parameters, so `first<Int>` has type fn([int]) -> int.
func (c *Checker) functionType(fn *ast.FnDecl, id *ast.Ident) Type {
	if len(fn.TypeParams) == 0 {
		return c.resolveFnType(fn)
	}

	bindings := make(map[string]Type, len(fn.TypeParams))
	for i, name := range fn.TypeParams {
		if len(id.TypeArgs) == len(fn.TypeParams) {
			bindings[name] = c.resolveType(id.TypeArgs[i])
		} else {
			// Missing or mismatched type arguments are reported by
			// monomorphization; do not add errors about T on top
			bindings[name] = Unknown
		}
	}
	defer c.bind(bindings)()
	return c.resolveFnType(fn)
}

func (c *Checker) checkBinaryExpr(e *ast.BinaryExpr) Type {
	if e.Op == "=" {
		return c.checkAssignment(e)
	}

	left := c.checkExpr(e.Left)
	var right Type
	switch e.Op {
	case "&&":
		// The right side only runs when the left one was true
		c.withNarrowing(c.factsFrom(e.Left, true), func() { right = c.checkExpr(e.Right) })
	case "||":
		c.withNarrowing(c.factsFrom(e.Left, false), func() { right = c.checkExpr(e.Right) })
	default:
		right = c.checkExpr(e.Right)
	}

	if left == nil || right == nil {
		return Unknown
	}

	switch e.Op {

	case "??":
		// `a ?? b` is `a` unless it is null, so the result is a's type
		// without null, and b has to fit it.
		value := withoutNull(left)
		if value == nil || isUnknown(value) {
			return right
		}
		if !c.assignable(value, right) {
			c.errorf(e.Right.Token(), ErrTypeMismatch, "fallback of type %s does not match %s", right, value)
		}
		return value

	case "==", "!=":
		if !c.comparable(left, right) {
			c.errorf(e.Token(), ErrInvalidOperation, "operator %s not defined for types %s and %s", e.Op, left, right)
		}
		return Bool
	}

	// One side could not be typed; its error is already reported
	if isUnknown(left) || isUnknown(right) {
		switch e.Op {
		case "<", "<=", ">", ">=", "&&", "||":
			return Bool
		}
		return Unknown
	}

	// Basic arithmetic
	if left.Equals(Int) && right.Equals(Int) {
		switch e.Op {
		case "+", "-", "*", "/", "%":
			return Int
		case "<", "<=", ">", ">=":
			return Bool
		}
	}

	if left.Equals(Float) && right.Equals(Float) {
		switch e.Op {
		case "+", "-", "*", "/":
			return Float
		case "<", "<=", ">", ">=":
			return Bool
		}
	}

	if left.Equals(String) && right.Equals(String) {
		switch e.Op {
		case "+":
			return String
		case "<", "<=", ">", ">=":
			return Bool
		}
	}

	if left.Equals(Bool) && right.Equals(Bool) {
		if e.Op == "&&" || e.Op == "||" {
			return Bool
		}
	}

	for _, side := range []struct {
		typ  Type
		expr ast.Expr
	}{{left, e.Left}, {right, e.Right}} {
		if u, ok := side.typ.(*UnionType); ok && hasNull(u) {
			c.errorfWithSuggestion(side.expr.Token(), ErrPossiblyNull,
				"Test it against null first, or provide a fallback with '??'",
				"operator %s cannot be applied to %s: the value may be null", e.Op, side.typ)
			return Unknown
		}
	}

	c.errorf(e.Token(), ErrInvalidOperation, "operator %s not defined for types %s and %s", e.Op, left, right)
	return Unknown
}

func (c *Checker) checkUnaryExpr(e *ast.UnaryExpr) Type {
	operand := c.checkExpr(e.Expr)
	if operand == nil || isUnknown(operand) {
		return Unknown
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
	// The expected type is for this call only, not for calls among its
	// arguments
	expected := c.expected
	c.expected = nil

	var result Type
	if member, ok := e.Func.(*ast.MemberExpr); ok {
		result = c.checkMethodCall(e, member, expected)
	} else {
		c.inferFunctionTypeArgs(e, expected)
		result = c.callValue(e, c.checkExpr(e.Func))
		if c.isBuiltinCall(e, "push") {
			c.checkPush(e)
		}
	}

	// The callee may have changed a field it can reach
	if c.callMayWriteFields(e) {
		c.forgetFields()
	}
	return result
}

func (c *Checker) checkStructInitExpr(e *ast.StructInitExpr) Type {
	var typeArgs []Type
	for _, arg := range e.TypeArgs {
		typeArgs = append(typeArgs, c.resolveType(arg))
	}
	result := &StructType{Name: e.Name, BaseName: e.Name, TypeArgs: typeArgs}

	// Field initializers are checked in name order, for stable diagnostics
	names := make([]string, 0, len(e.Fields))
	for name := range e.Fields {
		names = append(names, name)
	}
	sort.Strings(names)

	decl := c.structs[e.Name]
	if decl == nil {
		for _, name := range names {
			c.checkExpr(e.Fields[name])
		}
		if c.traits[e.Name] != nil {
			c.errorf(e.Token(), ErrUndefinedStruct, "'%s' is a trait and cannot be instantiated", e.Name)
		} else {
			c.errorf(e.Token(), ErrUndefinedStruct, "undefined struct '%s'", e.Name)
		}
		return result
	}

	for _, name := range names {
		value := e.Fields[name]
		fieldType, ok := c.fieldType(result, name)
		if !ok {
			c.checkExpr(value)
			c.errorf(value.Token(), ErrUnknownField, "struct %s has no field '%s'", e.Name, name)
			continue
		}
		valType := c.checkExprAs(value, fieldType)
		if valType != nil && !c.assignable(fieldType, valType) {
			c.errorf(value.Token(), ErrTypeMismatch, "cannot use type %s for field '%s' of type %s", valType, name, fieldType)
		}
	}

	var missing []string
	for _, field := range decl.Fields {
		if _, given := e.Fields[field.Name]; !given {
			missing = append(missing, "'"+field.Name+"'")
		}
	}
	if len(missing) > 0 {
		c.errorfWithSuggestion(e.Token(), ErrMissingField, "Every field needs a value; use null for an optional field",
			"struct literal of %s is missing %s", e.Name, strings.Join(missing, ", "))
	}
	return result
}

// checkMemberExpr types `expr.name` where it is not being called: the
// length of an array or string, or a struct field.
func (c *Checker) checkMemberExpr(e *ast.MemberExpr) Type {
	recv := c.checkExpr(e.Expr)

	// A field tested against null is known to hold a value
	if key, _, ok := c.fieldPath(e); ok {
		if fact, narrowed := c.narrowedPaths[key]; narrowed {
			return fact.typ
		}
	}

	switch t := recv.(type) {
	case nil:
		return Unknown

	case *ArrayType:
		if e.Name == "length" {
			return Int
		}

	case *PrimitiveType:
		if isUnknown(t) {
			return Unknown
		}
		if t.Equals(String) && e.Name == "length" {
			return Int
		}

	case *StructType:
		if c.structs[t.Name] == nil {
			return Unknown
		}
		if fieldType, ok := c.fieldType(t, e.Name); ok {
			return fieldType
		}
		if c.methodDecl(t, e.Name) != nil {
			c.errorfWithSuggestion(e.Token(), ErrUnknownField, fmt.Sprintf("Call it: %s()", e.Name),
				"'%s' is a method of %s, not a field", e.Name, t)
			return Unknown
		}
		if similar := c.similarMember(t, e.Name); similar != "" {
			c.errorfWithSuggestion(e.Token(), ErrUnknownField, "Did you mean '"+similar+"'?",
				"%s has no field '%s'", t, e.Name)
			return Unknown
		}

	case *UnionType:
		if hasNull(t) {
			c.errorfWithSuggestion(e.Token(), ErrPossiblyNull, "Test it against null first, or provide a fallback with '??'",
				"cannot read '%s' of %s: the value may be null", e.Name, t)
			return Unknown
		}
	}

	c.errorf(e.Token(), ErrUnknownField, "%s has no field '%s'", recv, e.Name)
	return Unknown
}

// checkIndexExpr types `expr[index]`.
func (c *Checker) checkIndexExpr(e *ast.IndexExpr) Type {
	recv := c.checkExpr(e.Expr)
	index := c.checkExpr(e.Index)
	if index != nil && !index.Equals(Int) && !isUnknown(index) {
		c.errorfWithSuggestion(e.Index.Token(), ErrTypeMismatch, "Use an Int as index", "index must be int, got %s", index)
	}

	switch t := recv.(type) {
	case nil:
		return Unknown
	case *ArrayType:
		return t.ElementType
	case *PrimitiveType:
		if t.Equals(String) {
			return String
		}
		if isUnknown(t) {
			return Unknown
		}
	case *UnionType:
		if hasNull(t) {
			c.errorfWithSuggestion(e.Token(), ErrPossiblyNull, "Test it against null first, or provide a fallback with '??'",
				"cannot index %s: the value may be null", t)
			return Unknown
		}
	}

	c.errorf(e.Token(), ErrInvalidOperation, "type %s cannot be indexed", recv)
	return Unknown
}

// checkFnExpr types an anonymous function and checks its body.
func (c *Checker) checkFnExpr(e *ast.FnExpr) Type {
	// Reuse the FnDecl machinery so `return` is checked against the lambda
	fn := &ast.FnDecl{BaseNode: e.BaseNode, Params: e.Params, ReturnType: e.ReturnType, Body: e.Body}
	c.checkFnDecl(fn)
	c.recordSignature(e, fn)
	return c.resolveFnType(fn)
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
		// Nothing constrains the element type yet
		return NewArray(Unknown)
	}

	// Check the first element to determine the array type
	firstType := c.checkExpr(e.Elements[0])
	if firstType == nil {
		return NewArray(Unknown)
	}

	// Check that all other elements have compatible types
	for i := 1; i < len(e.Elements); i++ {
		elemType := c.checkExpr(e.Elements[i])
		if elemType != nil && !c.assignable(firstType, elemType) {
			c.errorfWithSuggestion(
				e.Elements[i].Token(),
				ErrTypeMismatch,
				fmt.Sprintf("All array elements should be of type %s", describe(firstType)),
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
		if bound, inScope := c.typeParams[n.Name]; inScope {
			if bound != nil {
				return bound
			}
			return NewTypeParam(n.Name)
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
		case "Null", "null":
			return Null
		case "unknown":
			return Unknown
		case "Self":
			if c.selfType != nil {
				return c.selfType
			}
			return Self
		}

		if _, isTrait := c.traits[n.Name]; isTrait {
			return NewBehavior(n.Name, nil)
		}

		var typeArgs []Type
		for _, arg := range n.Generics {
			typeArgs = append(typeArgs, c.resolveType(arg))
		}
		return &StructType{Name: n.Name, BaseName: n.Name, TypeArgs: typeArgs}

	case ast.TypeArray, ast.TypeVariadic:
		// A variadic parameter is seen as an array inside the function
		elem := c.resolveType(n.ElemType)
		return NewArray(elem)
	case ast.TypeOption:
		return optional(c.resolveType(n.ElemType))
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
	return Unknown
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

// bindTypeParams brings the type parameters of a declaration into scope as
// opaque types, unless a use site already bound them to concrete types. It
// returns a function that restores the previous scope.
func (c *Checker) bindTypeParams(names []string) func() {
	bindings := make(map[string]Type)
	for _, name := range names {
		if _, inScope := c.typeParams[name]; !inScope {
			bindings[name] = nil
		}
	}
	return c.bind(bindings)
}

func (c *Checker) errorf(tok token.Token, code string, format string, args ...interface{}) {
	c.errs.ReportErrorFromToken(code, fmt.Sprintf(format, describeAll(args)...), tok, c.sourceLines)
}

func (c *Checker) errorfWithSuggestion(tok token.Token, code string, suggestion string, format string, args ...interface{}) {
	c.errs.ReportErrorWithSuggestion(code, fmt.Sprintf(format, describeAll(args)...), suggestion, tok, c.sourceLines)
}

// describeAll replaces types among message arguments by their source form.
func describeAll(args []interface{}) []interface{} {
	out := make([]interface{}, len(args))
	for i, arg := range args {
		if t, ok := arg.(Type); ok && t != nil {
			out[i] = describe(t)
		} else {
			out[i] = arg
		}
	}
	return out
}

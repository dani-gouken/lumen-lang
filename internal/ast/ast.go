package ast

import (
	"fmt"
	"strings"

	"github.com/dani-gouken/lumen/internal/token"
)

// Node is the base interface for all AST nodes.
type Node interface {
	String() string
	Position() token.Position
	Token() token.Token
}

// BaseNode provides common functionality for all AST nodes.
type BaseNode struct {
	Pos token.Position
	Tok token.Token // Original token (for rich diagnostics)
}

func (n *BaseNode) Position() token.Position {
	return n.Pos
}

func (n *BaseNode) Token() token.Token {
	return n.Tok
}

// Expr is the interface for all expression nodes.
type Expr interface {
	Node
	exprNode()
}

// Stmt is the interface for all statement nodes.
type Stmt interface {
	Node
	stmtNode()
}

// Decl is the interface for all declaration nodes.
type Decl interface {
	Node
	declNode()
}

// Program represents a full Lumen source file.
type Program struct {
	BaseNode
	Imports []*ImportDecl
	Decls   []Decl
}

func (p *Program) String() string {
	var sb strings.Builder
	for _, imp := range p.Imports {
		sb.WriteString(imp.String() + "\n")
	}
	for _, decl := range p.Decls {
		sb.WriteString(decl.String() + "\n\n")
	}
	return sb.String()
}

// ImportDecl represents an import statement.
type ImportDecl struct {
	BaseNode
	Path  string
	Alias string
}

func (d *ImportDecl) String() string {
	if d.Alias != "" {
		return fmt.Sprintf("import %q as %s;", d.Path, d.Alias)
	}
	return fmt.Sprintf("import %q;", d.Path)
}

// TypeKind distinguishes different kinds of types.
type TypeKind int

const (
	TypeBasic TypeKind = iota
	TypeArray
	TypeOption
	TypeUnion
	TypeFunction
	TypeVariadic
)

// Type represents a type annotation.
type Type struct {
	BaseNode
	Kind TypeKind

	// For Basic (e.g. Int, MyStruct<T>)
	Name     string
	Generics []*Type

	// For Array, Option, Variadic
	ElemType *Type

	// For Union
	UnionTypes []*Type

	// For Function
	ParamTypes []*Type
	ReturnType *Type
}

func (t *Type) String() string {
	switch t.Kind {
	case TypeArray:
		return "[" + t.ElemType.String() + "]"
	case TypeOption:
		return t.ElemType.String() + "?"
	case TypeVariadic:
		return "..." + t.ElemType.String()
	case TypeUnion:
		var parts []string
		for _, u := range t.UnionTypes {
			parts = append(parts, u.String())
		}
		return strings.Join(parts, " | ")
	case TypeFunction:
		var params []string
		for _, p := range t.ParamTypes {
			params = append(params, p.String())
		}
		ret := "Void"
		if t.ReturnType != nil {
			ret = t.ReturnType.String()
		}
		return fmt.Sprintf("fn(%s) -> %s", strings.Join(params, ", "), ret)
	case TypeBasic:
		s := t.Name
		if len(t.Generics) > 0 {
			var args []string
			for _, g := range t.Generics {
				args = append(args, g.String())
			}
			s += "<" + strings.Join(args, ", ") + ">"
		}
		return s
	default:
		return "unknown_type"
	}
}

// --- Declarations ---
type FnDecl struct {
	BaseNode
	Name          string
	TypeParams    []string
	Params        []*Param
	ReturnType    *Type
	Body          *BlockStmt
	IsTraitMethod bool
}

func (d *FnDecl) declNode() {}
func (d *FnDecl) String() string {
	var sb strings.Builder
	sb.WriteString("fn " + d.Name)
	if len(d.TypeParams) > 0 {
		sb.WriteString("<" + strings.Join(d.TypeParams, ", ") + ">")
	}
	sb.WriteString("(")
	var params []string
	for _, p := range d.Params {
		params = append(params, p.String())
	}
	sb.WriteString(strings.Join(params, ", "))
	sb.WriteString(")")
	if d.ReturnType != nil {
		sb.WriteString(" -> " + d.ReturnType.String())
	}
	if d.Body != nil {
		sb.WriteString(" " + d.Body.String())
	} else {
		sb.WriteString(";")
	}
	return sb.String()
}

type Param struct {
	BaseNode
	Name string
	Type *Type
}

func (p *Param) String() string {
	return fmt.Sprintf("%s: %s", p.Name, p.Type.String())
}

type StructDecl struct {
	BaseNode
	Name       string
	TypeParams []string
	Fields     []*Field
}

func (d *StructDecl) declNode() {}
func (d *StructDecl) String() string {
	var sb strings.Builder
	sb.WriteString("struct " + d.Name)
	if len(d.TypeParams) > 0 {
		sb.WriteString("<" + strings.Join(d.TypeParams, ", ") + ">")
	}
	sb.WriteString(" { ")
	var fields []string
	for _, f := range d.Fields {
		fields = append(fields, f.String())
	}
	sb.WriteString(strings.Join(fields, ", "))
	sb.WriteString(" }")
	return sb.String()
}

type EnumDecl struct {
	BaseNode
	Name       string
	TypeParams []string
	Variants   []*EnumVariant
}

func (d *EnumDecl) declNode() {}
func (d *EnumDecl) String() string {
	var sb strings.Builder
	sb.WriteString("enum " + d.Name)
	if len(d.TypeParams) > 0 {
		sb.WriteString("<" + strings.Join(d.TypeParams, ", ") + ">")
	}
	sb.WriteString(" { ")
	var variants []string
	for _, v := range d.Variants {
		variants = append(variants, v.String())
	}
	sb.WriteString(strings.Join(variants, ", "))
	sb.WriteString(" }")
	return sb.String()
}

type EnumVariant struct {
	BaseNode
	Name   string
	Values []*Type
}

func (v *EnumVariant) String() string {
	if len(v.Values) == 0 {
		return v.Name
	}
	var types []string
	for _, t := range v.Values {
		types = append(types, t.String())
	}
	return fmt.Sprintf("%s(%s)", v.Name, strings.Join(types, ", "))
}

type Field struct {
	BaseNode
	Name string
	Type *Type
}

func (f *Field) String() string {
	return fmt.Sprintf("%s: %s", f.Name, f.Type.String())
}

type TraitDecl struct {
	BaseNode
	Name    string
	Methods []*FnDecl
}

func (d *TraitDecl) declNode() {}
func (d *TraitDecl) String() string {
	var sb strings.Builder
	sb.WriteString("trait " + d.Name + " { ")
	for _, m := range d.Methods {
		sb.WriteString(m.String() + " ")
	}
	sb.WriteString("}")
	return sb.String()
}

type ImplDecl struct {
	BaseNode
	TypeParams []string // impl<T> Box<T>: the struct's type parameters, by position
	StructName string
	TraitName  string
	Methods    []*FnDecl
}

func (d *ImplDecl) declNode() {}
func (d *ImplDecl) String() string {
	var sb strings.Builder
	sb.WriteString("impl")
	generics := ""
	if len(d.TypeParams) > 0 {
		generics = "<" + strings.Join(d.TypeParams, ", ") + ">"
		sb.WriteString(generics)
	}
	if d.TraitName != "" {
		sb.WriteString(" " + d.TraitName + " for")
	}
	sb.WriteString(" " + d.StructName + generics + " { ")
	for _, m := range d.Methods {
		sb.WriteString(m.String() + " ")
	}
	sb.WriteString("}")
	return sb.String()
}

// --- Statements ---

type BlockStmt struct {
	BaseNode
	Stmts []Stmt
}

func (s *BlockStmt) stmtNode() {}
func (s *BlockStmt) String() string {
	var sb strings.Builder
	sb.WriteString("{ ")
	for _, stmt := range s.Stmts {
		sb.WriteString(stmt.String() + " ")
	}
	sb.WriteString("}")
	return sb.String()
}

type LetStmt struct {
	BaseNode
	Name    string
	Type    *Type
	Value   Expr
	IsConst bool
}

func (s *LetStmt) stmtNode() {}
func (s *LetStmt) String() string {
	kind := "let"
	if s.IsConst {
		kind = "const"
	}
	t := ""
	if s.Type != nil {
		t = ": " + s.Type.String()
	}
	return fmt.Sprintf("%s %s%s = %s;", kind, s.Name, t, s.Value.String())
}

type ReturnStmt struct {
	BaseNode
	Value Expr
}

func (s *ReturnStmt) stmtNode() {}
func (s *ReturnStmt) String() string {
	if s.Value != nil {
		return fmt.Sprintf("return %s;", s.Value.String())
	}
	return "return;"
}

type IfStmt struct {
	BaseNode
	Cond Expr
	Then *BlockStmt
	Else Stmt // Can be BlockStmt or IfStmt
}

func (s *IfStmt) stmtNode() {}
func (s *IfStmt) String() string {
	str := fmt.Sprintf("if %s %s", s.Cond.String(), s.Then.String())
	if s.Else != nil {
		str += " else " + s.Else.String()
	}
	return str
}

type WhileStmt struct {
	BaseNode
	Cond Expr
	Body *BlockStmt
}

func (s *WhileStmt) stmtNode() {}
func (s *WhileStmt) String() string {
	return fmt.Sprintf("while %s %s", s.Cond.String(), s.Body.String())
}

type ForStmt struct {
	BaseNode
	Var      string
	Index    *ForIndex // `for i, item in items`: the position variable, or nil
	Iterable Expr
	Body     *BlockStmt
}

func (s *ForStmt) stmtNode() {}
func (s *ForStmt) String() string {
	vars := s.Var
	if s.Index != nil {
		vars = s.Index.Name + ", " + s.Var
	}
	return fmt.Sprintf("for %s in %s %s", vars, s.Iterable.String(), s.Body.String())
}

// ForIndex is the position variable of a for loop. It is a node of its own
// so that a use of the variable can be told from a use of the element.
type ForIndex struct {
	BaseNode
	Name string
}

func (i *ForIndex) String() string { return i.Name }

type BreakStmt struct{ BaseNode }

func (s *BreakStmt) stmtNode()      {}
func (s *BreakStmt) String() string { return "break;" }

type ContinueStmt struct{ BaseNode }

func (s *ContinueStmt) stmtNode()      {}
func (s *ContinueStmt) String() string { return "continue;" }

type MatchStmt struct {
	BaseNode
	Cond  Expr
	Cases []*MatchCase
}

func (s *MatchStmt) stmtNode() {}
func (s *MatchStmt) String() string {
	var sb strings.Builder
	sb.WriteString("match " + s.Cond.String() + " { ")
	for _, c := range s.Cases {
		sb.WriteString(c.String() + " ")
	}
	sb.WriteString("}")
	return sb.String()
}

type MatchCase struct {
	BaseNode
	Pattern Node // Can be Expr (value) or Type (type check)
	Body    Node // Can be *BlockStmt or Expr
}

func (c *MatchCase) String() string {
	return fmt.Sprintf("case %s: %s", c.Pattern.String(), c.Body.String())
}

type ExprStmt struct {
	BaseNode
	Expr Expr
}

func (s *ExprStmt) stmtNode() {}
func (s *ExprStmt) String() string {
	return s.Expr.String() + ";"
}

// --- Expressions ---

// BinaryExpr is a binary operation. Assignments are binary expressions too:
// Op is "=" for `target = value`, and "+=", "-=", "*=", "/=", "%=" for the
// compound forms (`i++` is `i += 1`).
type BinaryExpr struct {
	BaseNode
	Left  Expr
	Op    string
	Right Expr
}

// CompoundOp returns the arithmetic operator of a compound assignment
// ("+" for "+="), and whether op is one.
func CompoundOp(op string) (string, bool) {
	switch op {
	case "+=", "-=", "*=", "/=", "%=":
		return op[:1], true
	}
	return "", false
}

// IsAssignment reports whether op assigns to its left operand.
func IsAssignment(op string) bool {
	_, compound := CompoundOp(op)
	return op == "=" || compound
}

func (e *BinaryExpr) exprNode() {}
func (e *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", e.Left.String(), e.Op, e.Right.String())
}

type UnaryExpr struct {
	BaseNode
	Op   string
	Expr Expr
}

func (e *UnaryExpr) exprNode() {}
func (e *UnaryExpr) String() string {
	return fmt.Sprintf("(%s%s)", e.Op, e.Expr.String())
}

// CastExpr converts a value to a type, checked when the program runs:
// `value as Int`.
type CastExpr struct {
	BaseNode
	Expr Expr
	Type *Type
}

func (e *CastExpr) exprNode() {}
func (e *CastExpr) String() string {
	return fmt.Sprintf("(%s as %s)", e.Expr.String(), e.Type.String())
}

// TypeTestExpr asks whether a value is of a type when the program runs:
// `value is Int`.
type TypeTestExpr struct {
	BaseNode
	Expr Expr
	Type *Type
}

func (e *TypeTestExpr) exprNode() {}
func (e *TypeTestExpr) String() string {
	return fmt.Sprintf("(%s is %s)", e.Expr.String(), e.Type.String())
}

type CallExpr struct {
	BaseNode
	Func Expr
	Args []Expr
}

func (e *CallExpr) exprNode() {}
func (e *CallExpr) String() string {
	var args []string
	for _, a := range e.Args {
		args = append(args, a.String())
	}
	return fmt.Sprintf("%s(%s)", e.Func.String(), strings.Join(args, ", "))
}

type MemberExpr struct {
	BaseNode
	Expr Expr
	Name string

	// Type arguments of a call to a generic method: stack.map<String>(f)
	TypeArgs []*Type
}

func (e *MemberExpr) exprNode() {}
func (e *MemberExpr) String() string {
	s := fmt.Sprintf("%s.%s", e.Expr.String(), e.Name)
	if len(e.TypeArgs) > 0 {
		var args []string
		for _, t := range e.TypeArgs {
			args = append(args, t.String())
		}
		s += "<" + strings.Join(args, ", ") + ">"
	}
	return s
}

type IndexExpr struct {
	BaseNode
	Expr  Expr
	Index Expr
}

func (e *IndexExpr) exprNode() {}
func (e *IndexExpr) String() string {
	return fmt.Sprintf("%s[%s]", e.Expr.String(), e.Index.String())
}

type Literal struct {
	BaseNode
	Value string
	Kind  string // "int", "float", "string", "bool", "null"
}

func (e *Literal) exprNode() {}
func (e *Literal) String() string {
	if e.Kind == "string" {
		return fmt.Sprintf("%q", e.Value)
	}
	return e.Value
}

type Ident struct {
	BaseNode
	Name     string
	TypeArgs []*Type
}

func (e *Ident) exprNode() {}
func (e *Ident) String() string {
	s := e.Name
	if len(e.TypeArgs) > 0 {
		var args []string
		for _, t := range e.TypeArgs {
			args = append(args, t.String())
		}
		s += "<" + strings.Join(args, ", ") + ">"
	}
	return s
}

type StructInitExpr struct {
	BaseNode
	Name     string
	TypeArgs []*Type
	Fields   map[string]Expr
}

func (e *StructInitExpr) exprNode() {}
func (e *StructInitExpr) String() string {
	name := e.Name
	if len(e.TypeArgs) > 0 {
		var args []string
		for _, t := range e.TypeArgs {
			args = append(args, t.String())
		}
		name += "<" + strings.Join(args, ", ") + ">"
	}

	var fields []string
	for k, v := range e.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", k, v.String()))
	}
	return fmt.Sprintf("%s { %s }", name, strings.Join(fields, ", "))
}

type ArrayLiteralExpr struct {
	BaseNode
	Elements []Expr
}

func (e *ArrayLiteralExpr) exprNode() {}
func (e *ArrayLiteralExpr) String() string {
	var elems []string
	for _, elem := range e.Elements {
		elems = append(elems, elem.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
}

// FnExpr represents an anonymous function expression (lambda/closure)
type FnExpr struct {
	BaseNode
	Params     []*Param
	ReturnType *Type
	Body       *BlockStmt
}

func (e *FnExpr) exprNode() {}
func (e *FnExpr) String() string {
	var sb strings.Builder
	sb.WriteString("fn(")
	var params []string
	for _, p := range e.Params {
		params = append(params, p.String())
	}
	sb.WriteString(strings.Join(params, ", "))
	sb.WriteString(")")
	if e.ReturnType != nil {
		sb.WriteString(" -> " + e.ReturnType.String())
	}
	if e.Body != nil {
		sb.WriteString(" " + e.Body.String())
	}
	return sb.String()
}

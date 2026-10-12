package desugar

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/token"
	"github.com/dani-gouken/lumen/internal/types"
)

// DesugarInfo holds metadata about the desugaring process
type DesugarInfo struct {
	// Maps original AST nodes to their desugared equivalents
	Mapping map[ast.Node]ast.Node

	// Maps generated temporary variables to their purposes (for debugging)
	TmpVars map[string]string
}

// Desugarer transforms complex syntactic constructs into simpler, more primitive forms
type Desugarer struct {
	prog         *ast.Program
	typeInfo     *types.Info
	resolveTable *resolve.Table
	errs         *errors.Handler

	// Counter for generating unique temporary variable names
	tmpCounter int

	// Info collected during desugaring
	info *DesugarInfo

	// Source lines for error reporting (map of line number to line content)
	sourceLines map[int]string
}

// NewDesugarer creates a new desugarer instance
func NewDesugarer(
	prog *ast.Program,
	typeInfo *types.Info,
	resolveTable *resolve.Table,
	errs *errors.Handler,
	sourceLines map[int]string,
) *Desugarer {
	return &Desugarer{
		prog:         prog,
		typeInfo:     typeInfo,
		resolveTable: resolveTable,
		errs:         errs,
		tmpCounter:   0,
		sourceLines:  sourceLines,
		info: &DesugarInfo{
			Mapping: make(map[ast.Node]ast.Node),
			TmpVars: make(map[string]string),
		},
	}
}

// Desugar performs the complete desugaring transformation on the program
func (d *Desugarer) Desugar() (*ast.Program, *DesugarInfo) {
	// Create a new program with desugared declarations
	newDecls := make([]ast.Decl, 0, len(d.prog.Decls))

	for _, decl := range d.prog.Decls {
		newDecl := d.desugarDecl(decl)
		newDecls = append(newDecls, newDecl)
	}

	newProg := &ast.Program{
		BaseNode: d.prog.BaseNode,
		Imports:  d.prog.Imports,
		Decls:    newDecls,
	}

	return newProg, d.info
}

// desugarDecl desugars a declaration
func (d *Desugarer) desugarDecl(decl ast.Decl) ast.Decl {
	switch decl := decl.(type) {
	case *ast.FnDecl:
		return d.desugarFnDecl(decl)
	case *ast.StructDecl:
		return decl // Structs don't need desugaring
	case *ast.EnumDecl:
		return decl // Enums don't need desugaring
	case *ast.TraitDecl:
		return d.desugarTraitDecl(decl)
	case *ast.ImplDecl:
		return d.desugarImplDecl(decl)
	default:
		return decl
	}
}

// desugarFnDecl desugars a function declaration
func (d *Desugarer) desugarFnDecl(fn *ast.FnDecl) *ast.FnDecl {
	if fn.Body == nil {
		return fn
	}

	newBody := d.desugarStmt(fn.Body).(*ast.BlockStmt)

	return &ast.FnDecl{
		BaseNode:      fn.BaseNode,
		Name:          fn.Name,
		TypeParams:    fn.TypeParams,
		Params:        fn.Params,
		ReturnType:    fn.ReturnType,
		Body:          newBody,
		IsTraitMethod: fn.IsTraitMethod,
	}
}

// desugarTraitDecl desugars a trait declaration
func (d *Desugarer) desugarTraitDecl(trait *ast.TraitDecl) *ast.TraitDecl {
	newMethods := make([]*ast.FnDecl, len(trait.Methods))
	for i, method := range trait.Methods {
		newMethods[i] = d.desugarFnDecl(method)
	}

	return &ast.TraitDecl{
		BaseNode: trait.BaseNode,
		Name:     trait.Name,
		Methods:  newMethods,
	}
}

// desugarImplDecl desugars an impl declaration
func (d *Desugarer) desugarImplDecl(impl *ast.ImplDecl) *ast.ImplDecl {
	newMethods := make([]*ast.FnDecl, len(impl.Methods))
	for i, method := range impl.Methods {
		newMethods[i] = d.desugarFnDecl(method)
	}

	return &ast.ImplDecl{
		BaseNode:   impl.BaseNode,
		StructName: impl.StructName,
		TraitName:  impl.TraitName,
		Methods:    newMethods,
	}
}

// desugarStmt desugars a statement
func (d *Desugarer) desugarStmt(stmt ast.Stmt) ast.Stmt {
	switch stmt := stmt.(type) {
	case *ast.BlockStmt:
		return d.desugarBlockStmt(stmt)
	case *ast.LetStmt:
		return d.desugarLetStmt(stmt)
	case *ast.ReturnStmt:
		return d.desugarReturnStmt(stmt)
	case *ast.IfStmt:
		return d.desugarIfStmt(stmt)
	case *ast.WhileStmt:
		return d.desugarWhileStmt(stmt)
	case *ast.ForStmt:
		return d.desugarForStmt(stmt)
	case *ast.MatchStmt:
		return d.desugarMatchStmt(stmt)
	case *ast.ExprStmt:
		return d.desugarExprStmt(stmt)
	case *ast.BreakStmt, *ast.ContinueStmt:
		return stmt // No desugaring needed
	default:
		return stmt
	}
}

// desugarBlockStmt desugars a block statement
func (d *Desugarer) desugarBlockStmt(block *ast.BlockStmt) *ast.BlockStmt {
	newStmts := make([]ast.Stmt, 0, len(block.Stmts))

	for _, stmt := range block.Stmts {
		// A let may need helper statements in front of it. They go into
		// this block, next to the let: wrapping them in a block of their
		// own would end the variable's scope right after its declaration.
		if let, ok := stmt.(*ast.LetStmt); ok {
			newStmts = append(newStmts, d.desugarLetStmts(let)...)
			continue
		}

		newStmt := d.desugarStmt(stmt)
		newStmts = append(newStmts, newStmt)
	}

	return &ast.BlockStmt{
		BaseNode: block.BaseNode,
		Stmts:    newStmts,
	}
}

// desugarLetStmt desugars a let statement that is not directly inside a
// block. Helper statements, if any, are wrapped together with it.
func (d *Desugarer) desugarLetStmt(let *ast.LetStmt) ast.Stmt {
	stmts := d.desugarLetStmts(let)
	if len(stmts) == 1 {
		return stmts[0]
	}
	return d.wrapInBlock(stmts, let.Position())
}

// desugarLetStmts desugars a let statement into the statements that replace
// it: the helper statements computing a short-circuit value, then the let.
func (d *Desugarer) desugarLetStmts(let *ast.LetStmt) []ast.Stmt {
	if let.Value == nil {
		return []ast.Stmt{let}
	}

	// Check if the value contains short-circuit operators
	if d.containsShortCircuit(let.Value) {
		preStmts, newValue := d.desugarShortCircuitInExpression(let.Value)

		newLetStmt := &ast.LetStmt{
			BaseNode: let.BaseNode,
			Name:     let.Name,
			Type:     let.Type,
			Value:    newValue,
			IsConst:  let.IsConst,
		}

		return append(preStmts, newLetStmt)
	}

	newValue := d.desugarExpr(let.Value)

	return []ast.Stmt{&ast.LetStmt{
		BaseNode: let.BaseNode,
		Name:     let.Name,
		Type:     let.Type,
		Value:    newValue,
		IsConst:  let.IsConst,
	}}
}

// desugarReturnStmt desugars a return statement
func (d *Desugarer) desugarReturnStmt(ret *ast.ReturnStmt) *ast.ReturnStmt {
	if ret.Value == nil {
		return ret
	}

	newValue := d.desugarExpr(ret.Value)

	return &ast.ReturnStmt{
		BaseNode: ret.BaseNode,
		Value:    newValue,
	}
}

// desugarIfStmt desugars an if statement
func (d *Desugarer) desugarIfStmt(ifStmt *ast.IfStmt) ast.Stmt {
	// Check if condition contains short-circuit operators
	preStmts, newCond := d.desugarShortCircuitInCondition(ifStmt.Cond)
	newThen := d.desugarStmt(ifStmt.Then).(*ast.BlockStmt)

	var newElse ast.Stmt
	if ifStmt.Else != nil {
		newElse = d.desugarStmt(ifStmt.Else)
	}

	newIfStmt := &ast.IfStmt{
		BaseNode: ifStmt.BaseNode,
		Cond:     newCond,
		Then:     newThen,
		Else:     newElse,
	}

	// If we have pre-statements, wrap everything in a block
	if len(preStmts) > 0 {
		stmts := append(preStmts, newIfStmt)
		return d.wrapInBlock(stmts, ifStmt.Position())
	}

	return newIfStmt
}

// desugarWhileStmt desugars a while statement
func (d *Desugarer) desugarWhileStmt(while *ast.WhileStmt) ast.Stmt {
	// Check if condition contains short-circuit operators
	preStmts, newCond := d.desugarShortCircuitInCondition(while.Cond)
	newBody := d.desugarStmt(while.Body).(*ast.BlockStmt)

	// The pre-statements compute the condition, so they must run before every
	// iteration, including the ones reached through `continue`:
	//
	//	while true {
	//	  <pre-statements>
	//	  if !cond { break; }
	//	  body
	//	}
	if len(preStmts) > 0 {
		pos := while.Position()

		notCond := &ast.UnaryExpr{BaseNode: ast.BaseNode{Pos: pos}, Op: "!", Expr: newCond}
		d.typeInfo.Types[notCond] = types.Bool

		exit := &ast.IfStmt{
			BaseNode: ast.BaseNode{Pos: pos},
			Cond:     notCond,
			Then:     d.wrapInBlock([]ast.Stmt{&ast.BreakStmt{BaseNode: ast.BaseNode{Pos: pos}}}, pos),
		}

		bodyStmts := make([]ast.Stmt, 0, len(preStmts)+len(newBody.Stmts)+1)
		bodyStmts = append(bodyStmts, preStmts...)
		bodyStmts = append(bodyStmts, exit)
		bodyStmts = append(bodyStmts, newBody.Stmts...)

		return &ast.WhileStmt{
			BaseNode: while.BaseNode,
			Cond:     d.createBoolLiteral(true, pos),
			Body:     &ast.BlockStmt{BaseNode: newBody.BaseNode, Stmts: bodyStmts},
		}
	}

	return &ast.WhileStmt{
		BaseNode: while.BaseNode,
		Cond:     newCond,
		Body:     newBody,
	}
}

// desugarExprStmt desugars an expression statement
func (d *Desugarer) desugarExprStmt(expr *ast.ExprStmt) *ast.ExprStmt {
	newExpr := d.desugarExpr(expr.Expr)

	return &ast.ExprStmt{
		BaseNode: expr.BaseNode,
		Expr:     newExpr,
	}
}

// desugarExpr desugars an expression
func (d *Desugarer) desugarExpr(expr ast.Expr) ast.Expr {
	switch expr := expr.(type) {
	case *ast.BinaryExpr:
		return d.desugarBinaryExpr(expr)
	case *ast.UnaryExpr:
		return d.desugarUnaryExpr(expr)
	case *ast.CallExpr:
		return d.desugarCallExpr(expr)
	case *ast.MemberExpr:
		return d.desugarMemberExpr(expr)
	case *ast.IndexExpr:
		return d.desugarIndexExpr(expr)
	case *ast.StructInitExpr:
		return d.desugarStructInitExpr(expr)
	case *ast.ArrayLiteralExpr:
		return d.desugarArrayLiteralExpr(expr)
	case *ast.FnExpr:
		return d.desugarFnExpr(expr)
	case *ast.Literal, *ast.Ident:
		return expr // No desugaring needed
	default:
		return expr
	}
}

// desugarFnExpr desugars the body of an anonymous function
func (d *Desugarer) desugarFnExpr(fn *ast.FnExpr) ast.Expr {
	if fn.Body == nil {
		return fn
	}

	return &ast.FnExpr{
		BaseNode:   fn.BaseNode,
		Params:     fn.Params,
		ReturnType: fn.ReturnType,
		Body:       d.desugarBlockStmt(fn.Body),
	}
}

// desugarUnaryExpr desugars a unary expression
func (d *Desugarer) desugarUnaryExpr(unary *ast.UnaryExpr) ast.Expr {
	newExpr := d.desugarExpr(unary.Expr)

	return &ast.UnaryExpr{
		BaseNode: unary.BaseNode,
		Op:       unary.Op,
		Expr:     newExpr,
	}
}

// desugarCallExpr desugars a call expression
func (d *Desugarer) desugarCallExpr(call *ast.CallExpr) ast.Expr {
	newFunc := d.desugarExpr(call.Func)
	newArgs := make([]ast.Expr, len(call.Args))

	for i, arg := range call.Args {
		newArgs[i] = d.desugarExpr(arg)
	}

	return &ast.CallExpr{
		BaseNode: call.BaseNode,
		Func:     newFunc,
		Args:     newArgs,
	}
}

// desugarMemberExpr desugars a member expression
func (d *Desugarer) desugarMemberExpr(member *ast.MemberExpr) ast.Expr {
	newExpr := d.desugarExpr(member.Expr)

	return &ast.MemberExpr{
		BaseNode: member.BaseNode,
		Expr:     newExpr,
		Name:     member.Name,
	}
}

// desugarIndexExpr desugars an index expression
func (d *Desugarer) desugarIndexExpr(index *ast.IndexExpr) ast.Expr {
	newExpr := d.desugarExpr(index.Expr)
	newIndex := d.desugarExpr(index.Index)

	return &ast.IndexExpr{
		BaseNode: index.BaseNode,
		Expr:     newExpr,
		Index:    newIndex,
	}
}

// desugarStructInitExpr desugars a struct initialization expression
func (d *Desugarer) desugarStructInitExpr(structInit *ast.StructInitExpr) ast.Expr {
	newFields := make(map[string]ast.Expr)

	for name, expr := range structInit.Fields {
		newFields[name] = d.desugarExpr(expr)
	}

	return &ast.StructInitExpr{
		BaseNode: structInit.BaseNode,
		Name:     structInit.Name,
		TypeArgs: structInit.TypeArgs,
		Fields:   newFields,
	}
}

// desugarArrayLiteralExpr desugars an array literal expression
func (d *Desugarer) desugarArrayLiteralExpr(array *ast.ArrayLiteralExpr) ast.Expr {
	newElements := make([]ast.Expr, len(array.Elements))

	for i, elem := range array.Elements {
		newElements[i] = d.desugarExpr(elem)
	}

	return &ast.ArrayLiteralExpr{
		BaseNode: array.BaseNode,
		Elements: newElements,
	}
}

// generateTmpVar generates a unique temporary variable name
func (d *Desugarer) generateTmpVar() string {
	name := fmt.Sprintf("__tmp_%d", d.tmpCounter)
	d.tmpCounter++
	d.info.TmpVars[name] = "temporary"
	return name
}

// generateIterVar generates a unique iterator variable name
func (d *Desugarer) generateIterVar() string {
	name := fmt.Sprintf("__iter_%d", d.tmpCounter)
	d.tmpCounter++
	d.info.TmpVars[name] = "iterator"
	return name
}

// generateLenVar generates a unique length variable name
func (d *Desugarer) generateLenVar() string {
	name := fmt.Sprintf("__len_%d", d.tmpCounter)
	d.tmpCounter++
	d.info.TmpVars[name] = "length"
	return name
}

// generateMatchVar generates a unique match temporary variable name
func (d *Desugarer) generateMatchVar() string {
	name := fmt.Sprintf("__match_tmp_%d", d.tmpCounter)
	d.tmpCounter++
	d.info.TmpVars[name] = "match"
	return name
}

// createIntLiteral creates an integer literal with the given value
func (d *Desugarer) createIntLiteral(value string, pos token.Position) *ast.Literal {
	return &ast.Literal{
		BaseNode: ast.BaseNode{
			Pos: pos,
			Tok: token.Token{Type: token.INT, Literal: value, Pos: pos},
		},
		Value: value,
		Kind:  "int",
	}
}

// createBoolLiteral creates a boolean literal with the given value
func (d *Desugarer) createBoolLiteral(value bool, pos token.Position) *ast.Literal {
	valueStr := "false"
	if value {
		valueStr = "true"
	}
	return &ast.Literal{
		BaseNode: ast.BaseNode{
			Pos: pos,
			Tok: token.Token{Type: token.TRUE, Literal: valueStr, Pos: pos},
		},
		Value: valueStr,
		Kind:  "bool",
	}
}

// createIdent creates an identifier with the given name
func (d *Desugarer) createIdent(name string, pos token.Position) *ast.Ident {
	return &ast.Ident{
		BaseNode: ast.BaseNode{
			Pos: pos,
			Tok: token.Token{Type: token.IDENT, Literal: name, Pos: pos},
		},
		Name: name,
	}
}

// createBinaryExpr creates a binary expression
func (d *Desugarer) createBinaryExpr(left ast.Expr, op string, right ast.Expr, pos token.Position) *ast.BinaryExpr {
	return &ast.BinaryExpr{
		BaseNode: ast.BaseNode{Pos: pos},
		Left:     left,
		Op:       op,
		Right:    right,
	}
}

// createMemberExpr creates a member access expression
func (d *Desugarer) createMemberExpr(expr ast.Expr, name string, pos token.Position) *ast.MemberExpr {
	return &ast.MemberExpr{
		BaseNode: ast.BaseNode{Pos: pos},
		Expr:     expr,
		Name:     name,
	}
}

// createIndexExpr creates an index expression
func (d *Desugarer) createIndexExpr(expr ast.Expr, index ast.Expr, pos token.Position) *ast.IndexExpr {
	return &ast.IndexExpr{
		BaseNode: ast.BaseNode{Pos: pos},
		Expr:     expr,
		Index:    index,
	}
}

// wrapInBlock wraps statements in a block statement
func (d *Desugarer) wrapInBlock(stmts []ast.Stmt, pos token.Position) *ast.BlockStmt {
	return &ast.BlockStmt{
		BaseNode: ast.BaseNode{Pos: pos},
		Stmts:    stmts,
	}
}

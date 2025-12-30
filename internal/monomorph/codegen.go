package monomorph

import (
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/types"
)

// generateInstance generates a monomorphized instance for the given key.
func (m *Monomorphizer) generateInstance(key InstantiationKey) {
	// Check if already generated
	if _, exists := m.generated[key]; exists {
		return
	}

	// Check for infinite instantiation
	if m.generating[key] {
		return // Already being generated (cycle detected)
	}

	m.generating[key] = true
	defer func() { m.generating[key] = false }()

	inst := m.instantiations[key]

	switch decl := inst.Decl.(type) {
	case *ast.FnDecl:
		newDecl := m.generateFnInstance(decl, key)
		m.generated[key] = newDecl
		m.prog.Decls = append(m.prog.Decls, newDecl)
	case *ast.StructDecl:
		newDecl := m.generateStructInstance(decl, key)
		m.generated[key] = newDecl
		m.prog.Decls = append(m.prog.Decls, newDecl)
	}
}

// generateFnInstance generates a monomorphized function instance.
func (m *Monomorphizer) generateFnInstance(original *ast.FnDecl, key InstantiationKey) *ast.FnDecl {
	inst := m.instantiations[key]
	// Create type substitution map
	subst := m.createSubstitution(original.TypeParams, inst.TypeArgs)

	// Clone and substitute the function
	newFn := &ast.FnDecl{
		BaseNode:      original.BaseNode,
		Name:          m.mangleName(key),
		TypeParams:    nil, // No type parameters in monomorphized version
		Params:        m.cloneAndSubstituteParams(original.Params, subst),
		ReturnType:    m.substituteASTType(original.ReturnType, subst),
		Body:          m.cloneAndSubstituteBlock(original.Body, subst),
		IsTraitMethod: original.IsTraitMethod,
	}

	return newFn
}

// generateStructInstance generates a monomorphized struct instance.
func (m *Monomorphizer) generateStructInstance(original *ast.StructDecl, key InstantiationKey) *ast.StructDecl {
	inst := m.instantiations[key]
	// Create type substitution map
	subst := m.createSubstitution(original.TypeParams, inst.TypeArgs)

	// Clone and substitute the struct
	newStruct := &ast.StructDecl{
		BaseNode:   original.BaseNode,
		Name:       m.mangleName(key),
		TypeParams: nil, // No type parameters in monomorphized version
		Fields:     m.cloneAndSubstituteFields(original.Fields, subst),
	}

	return newStruct
}

// TypeSubstitution maps type parameter names to concrete types.
type TypeSubstitution map[string]types.Type

// createSubstitution creates a type substitution map.
func (m *Monomorphizer) createSubstitution(typeParams []string, typeArgs []types.Type) TypeSubstitution {
	subst := make(TypeSubstitution)
	for i, param := range typeParams {
		if i < len(typeArgs) {
			subst[param] = typeArgs[i]
		}
	}
	return subst
}

// cloneAndSubstituteParams clones and substitutes parameters.
func (m *Monomorphizer) cloneAndSubstituteParams(params []*ast.Param, subst TypeSubstitution) []*ast.Param {
	result := make([]*ast.Param, len(params))
	for i, param := range params {
		result[i] = &ast.Param{
			BaseNode: param.BaseNode,
			Name:     param.Name,
			Type:     m.substituteASTType(param.Type, subst),
		}
	}
	return result
}

// cloneAndSubstituteFields clones and substitutes struct fields.
func (m *Monomorphizer) cloneAndSubstituteFields(fields []*ast.Field, subst TypeSubstitution) []*ast.Field {
	result := make([]*ast.Field, len(fields))
	for i, field := range fields {
		result[i] = &ast.Field{
			BaseNode: field.BaseNode,
			Name:     field.Name,
			Type:     m.substituteASTType(field.Type, subst),
		}
	}
	return result
}

// cloneAndSubstituteBlock clones and substitutes a block statement.
func (m *Monomorphizer) cloneAndSubstituteBlock(block *ast.BlockStmt, subst TypeSubstitution) *ast.BlockStmt {
	if block == nil {
		return nil
	}

	newStmts := make([]ast.Stmt, len(block.Stmts))
	for i, stmt := range block.Stmts {
		newStmts[i] = m.cloneAndSubstituteStmt(stmt, subst)
	}

	return &ast.BlockStmt{
		BaseNode: block.BaseNode,
		Stmts:    newStmts,
	}
}

// cloneAndSubstituteStmt clones and substitutes a statement.
func (m *Monomorphizer) cloneAndSubstituteStmt(stmt ast.Stmt, subst TypeSubstitution) ast.Stmt {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		return m.cloneAndSubstituteBlock(s, subst)
	case *ast.LetStmt:
		return &ast.LetStmt{
			BaseNode: s.BaseNode,
			Name:     s.Name,
			Type:     m.substituteASTType(s.Type, subst),
			Value:    m.cloneAndSubstituteExpr(s.Value, subst),
			IsConst:  s.IsConst,
		}
	case *ast.ReturnStmt:
		return &ast.ReturnStmt{
			BaseNode: s.BaseNode,
			Value:    m.cloneAndSubstituteExpr(s.Value, subst),
		}
	case *ast.IfStmt:
		return &ast.IfStmt{
			BaseNode: s.BaseNode,
			Cond:     m.cloneAndSubstituteExpr(s.Cond, subst),
			Then:     m.cloneAndSubstituteBlock(s.Then, subst),
			Else:     m.cloneAndSubstituteStmt(s.Else, subst),
		}
	case *ast.WhileStmt:
		return &ast.WhileStmt{
			BaseNode: s.BaseNode,
			Cond:     m.cloneAndSubstituteExpr(s.Cond, subst),
			Body:     m.cloneAndSubstituteBlock(s.Body, subst),
		}
	case *ast.ForStmt:
		return &ast.ForStmt{
			BaseNode: s.BaseNode,
			Var:      s.Var,
			Iterable: m.cloneAndSubstituteExpr(s.Iterable, subst),
			Body:     m.cloneAndSubstituteBlock(s.Body, subst),
		}
	case *ast.ExprStmt:
		return &ast.ExprStmt{
			BaseNode: s.BaseNode,
			Expr:     m.cloneAndSubstituteExpr(s.Expr, subst),
		}
	case *ast.BreakStmt:
		return &ast.BreakStmt{BaseNode: s.BaseNode}
	case *ast.ContinueStmt:
		return &ast.ContinueStmt{BaseNode: s.BaseNode}
	default:
		return stmt
	}
}

// cloneAndSubstituteExpr clones and substitutes an expression.
func (m *Monomorphizer) cloneAndSubstituteExpr(expr ast.Expr, subst TypeSubstitution) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			BaseNode: e.BaseNode,
			Left:     m.cloneAndSubstituteExpr(e.Left, subst),
			Op:       e.Op,
			Right:    m.cloneAndSubstituteExpr(e.Right, subst),
		}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{
			BaseNode: e.BaseNode,
			Op:       e.Op,
			Expr:     m.cloneAndSubstituteExpr(e.Expr, subst),
		}
	case *ast.CallExpr:
		return &ast.CallExpr{
			BaseNode: e.BaseNode,
			Func:     m.cloneAndSubstituteExpr(e.Func, subst),
			Args:     m.cloneAndSubstituteExprs(e.Args, subst),
		}
	case *ast.MemberExpr:
		return &ast.MemberExpr{
			BaseNode: e.BaseNode,
			Expr:     m.cloneAndSubstituteExpr(e.Expr, subst),
			Name:     e.Name,
		}
	case *ast.IndexExpr:
		return &ast.IndexExpr{
			BaseNode: e.BaseNode,
			Expr:     m.cloneAndSubstituteExpr(e.Expr, subst),
			Index:    m.cloneAndSubstituteExpr(e.Index, subst),
		}
	case *ast.Literal:
		return &ast.Literal{
			BaseNode: e.BaseNode,
			Value:    e.Value,
			Kind:     e.Kind,
		}
	case *ast.Ident:
		// Clone the type arguments and substitute any type parameters
		var newTypeArgs []*ast.Type
		if len(e.TypeArgs) > 0 {
			newTypeArgs = make([]*ast.Type, len(e.TypeArgs))
			for i, ta := range e.TypeArgs {
				newTypeArgs[i] = m.substituteASTType(ta, subst)
			}
		}
		return &ast.Ident{
			BaseNode: e.BaseNode,
			Name:     e.Name,
			TypeArgs: newTypeArgs, // Keep type arguments but substitute type parameters
		}
	case *ast.StructInitExpr:
		newFields := make(map[string]ast.Expr)
		for k, v := range e.Fields {
			newFields[k] = m.cloneAndSubstituteExpr(v, subst)
		}
		// Clone type arguments and substitute any type parameters
		var newTypeArgs []*ast.Type
		if len(e.TypeArgs) > 0 {
			newTypeArgs = make([]*ast.Type, len(e.TypeArgs))
			for i, ta := range e.TypeArgs {
				newTypeArgs[i] = m.substituteASTType(ta, subst)
			}
		}
		return &ast.StructInitExpr{
			BaseNode: e.BaseNode,
			Name:     e.Name,
			TypeArgs: newTypeArgs, // Keep type arguments but substitute type parameters
			Fields:   newFields,
		}
	case *ast.ArrayLiteralExpr:
		return &ast.ArrayLiteralExpr{
			BaseNode: e.BaseNode,
			Elements: m.cloneAndSubstituteExprs(e.Elements, subst),
		}
	default:
		return expr
	}
}

// cloneAndSubstituteExprs clones and substitutes a slice of expressions.
func (m *Monomorphizer) cloneAndSubstituteExprs(exprs []ast.Expr, subst TypeSubstitution) []ast.Expr {
	result := make([]ast.Expr, len(exprs))
	for i, expr := range exprs {
		result[i] = m.cloneAndSubstituteExpr(expr, subst)
	}
	return result
}

// substituteASTType applies type substitution to an AST type.
func (m *Monomorphizer) substituteASTType(astType *ast.Type, subst TypeSubstitution) *ast.Type {
	if astType == nil {
		return nil
	}

	switch astType.Kind {
	case ast.TypeBasic:
		// Check if this is a type parameter
		if concreteType, ok := subst[astType.Name]; ok {
			// Convert the concrete type back to AST type
			return m.typeToASTType(concreteType)
		}
		// Otherwise, keep the type as-is but substitute generics
		newGenerics := make([]*ast.Type, len(astType.Generics))
		for i, g := range astType.Generics {
			newGenerics[i] = m.substituteASTType(g, subst)
		}
		return &ast.Type{
			BaseNode: astType.BaseNode,
			Kind:     astType.Kind,
			Name:     astType.Name,
			Generics: newGenerics,
		}
	case ast.TypeArray:
		return &ast.Type{
			BaseNode: astType.BaseNode,
			Kind:     astType.Kind,
			ElemType: m.substituteASTType(astType.ElemType, subst),
		}
	case ast.TypeFunction:
		newParams := make([]*ast.Type, len(astType.ParamTypes))
		for i, p := range astType.ParamTypes {
			newParams[i] = m.substituteASTType(p, subst)
		}
		return &ast.Type{
			BaseNode:   astType.BaseNode,
			Kind:       astType.Kind,
			ParamTypes: newParams,
			ReturnType: m.substituteASTType(astType.ReturnType, subst),
		}
	default:
		return astType
	}
}

// typeToASTType converts a types.Type to an AST type.
func (m *Monomorphizer) typeToASTType(t types.Type) *ast.Type {
	switch typ := t.(type) {
	case *types.PrimitiveType:
		return &ast.Type{
			Kind: ast.TypeBasic,
			Name: string(typ.Name),
		}
	case *types.StructType:
		// If the struct has type arguments, include them
		var generics []*ast.Type
		for _, arg := range typ.TypeArgs {
			generics = append(generics, m.typeToASTType(arg))
		}
		return &ast.Type{
			Kind:     ast.TypeBasic,
			Name:     typ.Name,
			Generics: generics,
		}
	case *types.ArrayType:
		return &ast.Type{
			Kind:     ast.TypeArray,
			ElemType: m.typeToASTType(typ.ElementType),
		}
	case *types.FunctionType:
		params := make([]*ast.Type, len(typ.Params))
		for i, p := range typ.Params {
			params[i] = m.typeToASTType(p)
		}
		return &ast.Type{
			Kind:       ast.TypeFunction,
			ParamTypes: params,
			ReturnType: m.typeToASTType(typ.ReturnType),
		}
	default:
		return &ast.Type{
			Kind: ast.TypeBasic,
			Name: "unknown",
		}
	}
}

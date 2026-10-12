package monomorph

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/types"
)

// resolveDependencies performs topological sort of instantiations to handle
// transitive dependencies and detect infinite instantiation cycles.
// Returns the instantiation keys in the order they should be generated.
func (m *Monomorphizer) resolveDependencies() []InstantiationKey {
	// Build dependency graph
	deps := make(map[InstantiationKey][]InstantiationKey)

	for key, inst := range m.instantiations {
		deps[key] = m.findDependencies(inst)
	}

	// Perform topological sort with cycle detection
	var result []InstantiationKey
	visited := make(map[InstantiationKey]bool)
	inProgress := make(map[InstantiationKey]bool)

	var visit func(key InstantiationKey) bool
	visit = func(key InstantiationKey) bool {
		if visited[key] {
			return true
		}
		if inProgress[key] {
			// Cycle detected
			inst := m.instantiations[key]
			if inst != nil && len(inst.UseSites) > 0 {
				useSite := inst.UseSites[0]
				msg := fmt.Sprintf("Generic function '%s' causes infinite instantiation. Detected cycle in type argument expansion.", inst.BaseName)
				suggestion := "Break the cycle by using a concrete type instead of recursively expanding the type parameter, or add a termination condition"
				m.errs.ReportErrorWithSuggestion(ErrInfiniteInstantiation, msg, suggestion, useSite.Token(), m.sourceLines)
			}
			return false
		}

		inProgress[key] = true

		// Visit dependencies first
		for _, dep := range deps[key] {
			if !visit(dep) {
				return false
			}
		}

		inProgress[key] = false
		visited[key] = true
		result = append(result, key)
		return true
	}

	for key := range m.instantiations {
		if !visit(key) {
			// Cycle detected, continue to report all errors
			continue
		}
	}

	return result
}

// findDependencies finds all instantiations that a given instantiation depends on.
// This occurs when a generic function calls another generic function with type parameters.
func (m *Monomorphizer) findDependencies(inst *Instantiation) []InstantiationKey {
	var deps []InstantiationKey

	// Get the declaration of this instantiation
	switch decl := inst.Decl.(type) {
	case *ast.FnDecl:
		if decl.Body != nil {
			deps = m.findDepsInStmt(decl.Body, inst.TypeArgs, decl.TypeParams)
		}
	case *ast.StructDecl:
		// Struct fields might reference generic types
		for _, field := range decl.Fields {
			if field.Type != nil {
				deps = append(deps, m.findDepsInType(field.Type, inst.TypeArgs, decl.TypeParams)...)
			}
		}
	}

	return deps
}

// findDepsInStmt finds dependencies in a statement by looking for calls to generic functions.
func (m *Monomorphizer) findDepsInStmt(stmt ast.Stmt, typeArgs []types.Type, typeParams []string) []InstantiationKey {
	var deps []InstantiationKey

	switch s := stmt.(type) {
	case *ast.BlockStmt:
		for _, st := range s.Stmts {
			deps = append(deps, m.findDepsInStmt(st, typeArgs, typeParams)...)
		}
	case *ast.LetStmt:
		if s.Value != nil {
			deps = append(deps, m.findDepsInExpr(s.Value, typeArgs, typeParams)...)
		}
	case *ast.ReturnStmt:
		if s.Value != nil {
			deps = append(deps, m.findDepsInExpr(s.Value, typeArgs, typeParams)...)
		}
	case *ast.IfStmt:
		deps = append(deps, m.findDepsInExpr(s.Cond, typeArgs, typeParams)...)
		deps = append(deps, m.findDepsInStmt(s.Then, typeArgs, typeParams)...)
		if s.Else != nil {
			deps = append(deps, m.findDepsInStmt(s.Else, typeArgs, typeParams)...)
		}
	case *ast.WhileStmt:
		deps = append(deps, m.findDepsInExpr(s.Cond, typeArgs, typeParams)...)
		deps = append(deps, m.findDepsInStmt(s.Body, typeArgs, typeParams)...)
	case *ast.ForStmt:
		deps = append(deps, m.findDepsInExpr(s.Iterable, typeArgs, typeParams)...)
		deps = append(deps, m.findDepsInStmt(s.Body, typeArgs, typeParams)...)
	case *ast.ExprStmt:
		deps = append(deps, m.findDepsInExpr(s.Expr, typeArgs, typeParams)...)
	}

	return deps
}

// findDepsInExpr finds dependencies in an expression.
func (m *Monomorphizer) findDepsInExpr(expr ast.Expr, typeArgs []types.Type, typeParams []string) []InstantiationKey {
	var deps []InstantiationKey

	switch e := expr.(type) {
	case *ast.CallExpr:
		if ident, ok := e.Func.(*ast.Ident); ok {
			if _, isGeneric := m.generics[ident.Name]; isGeneric {
				if len(ident.TypeArgs) > 0 {
					// Substitute type parameters with concrete types
					concreteTypeArgs := m.substituteTypeParams(ident.TypeArgs, typeArgs, typeParams)
					key := MakeInstantiationKey(ident.Name, concreteTypeArgs)
					deps = append(deps, key)

					// Make sure this instantiation is recorded
					if _, exists := m.instantiations[key]; !exists {
						m.recordInstantiation(key, ident.Name, concreteTypeArgs, m.generics[ident.Name], e)
					}
				}
			}
		}

		deps = append(deps, m.findDepsInExpr(e.Func, typeArgs, typeParams)...)
		for _, arg := range e.Args {
			deps = append(deps, m.findDepsInExpr(arg, typeArgs, typeParams)...)
		}

	case *ast.BinaryExpr:
		deps = append(deps, m.findDepsInExpr(e.Left, typeArgs, typeParams)...)
		deps = append(deps, m.findDepsInExpr(e.Right, typeArgs, typeParams)...)
	case *ast.UnaryExpr:
		deps = append(deps, m.findDepsInExpr(e.Expr, typeArgs, typeParams)...)
	case *ast.CastExpr:
		deps = append(deps, m.findDepsInExpr(e.Expr, typeArgs, typeParams)...)
	case *ast.TypeTestExpr:
		deps = append(deps, m.findDepsInExpr(e.Expr, typeArgs, typeParams)...)
	case *ast.MemberExpr:
		deps = append(deps, m.findDepsInExpr(e.Expr, typeArgs, typeParams)...)
	case *ast.IndexExpr:
		deps = append(deps, m.findDepsInExpr(e.Expr, typeArgs, typeParams)...)
		deps = append(deps, m.findDepsInExpr(e.Index, typeArgs, typeParams)...)
	case *ast.ArrayLiteralExpr:
		for _, elem := range e.Elements {
			deps = append(deps, m.findDepsInExpr(elem, typeArgs, typeParams)...)
		}
	case *ast.StructInitExpr:
		for _, fieldExpr := range e.Fields {
			deps = append(deps, m.findDepsInExpr(fieldExpr, typeArgs, typeParams)...)
		}
	}

	return deps
}

// findDepsInType finds dependencies in a type annotation.
func (m *Monomorphizer) findDepsInType(astType *ast.Type, typeArgs []types.Type, typeParams []string) []InstantiationKey {
	var deps []InstantiationKey

	switch astType.Kind {
	case ast.TypeBasic:
		// Check if this is a generic struct instantiation
		if len(astType.Generics) > 0 {
			if _, isGeneric := m.generics[astType.Name]; isGeneric {
				concreteTypeArgs := m.substituteTypeParams(astType.Generics, typeArgs, typeParams)
				key := MakeInstantiationKey(astType.Name, concreteTypeArgs)
				deps = append(deps, key)

				// Make sure this instantiation is recorded
				if _, exists := m.instantiations[key]; !exists {
					m.recordInstantiation(key, astType.Name, concreteTypeArgs, m.generics[astType.Name], astType)
				}
			}
		}
	case ast.TypeArray:
		deps = append(deps, m.findDepsInType(astType.ElemType, typeArgs, typeParams)...)
	case ast.TypeFunction:
		for _, param := range astType.ParamTypes {
			deps = append(deps, m.findDepsInType(param, typeArgs, typeParams)...)
		}
		if astType.ReturnType != nil {
			deps = append(deps, m.findDepsInType(astType.ReturnType, typeArgs, typeParams)...)
		}
	}

	return deps
}

// substituteTypeParams substitutes type parameters with concrete types.
func (m *Monomorphizer) substituteTypeParams(astTypes []*ast.Type, typeArgs []types.Type, typeParams []string) []types.Type {
	result := make([]types.Type, len(astTypes))

	for i, astType := range astTypes {
		result[i] = m.substituteTypeParam(astType, typeArgs, typeParams)
	}

	return result
}

// substituteTypeParam substitutes type parameters in a single type.
func (m *Monomorphizer) substituteTypeParam(astType *ast.Type, typeArgs []types.Type, typeParams []string) types.Type {
	// Check if this is a type parameter reference
	if astType.Kind == ast.TypeBasic {
		for i, param := range typeParams {
			if astType.Name == param && i < len(typeArgs) {
				return typeArgs[i]
			}
		}
	}

	// Otherwise, convert normally
	return m.astTypeToType(astType)
}

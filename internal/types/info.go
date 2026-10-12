package types

import "github.com/dani-gouken/lumen/internal/ast"

// Info holds the result of type checking.
type Info struct {
	// Types maps expressions to their computed types.
	Types map[ast.Expr]Type

	// Defs maps type names to their definitions (structs, enums, etc.)
	// This is populated during type checking for use by monomorphization
	Defs map[string]Type

	// Casts maps each cast that has to be verified at runtime to the
	// description of what the value must be. Casts that always succeed are
	// absent.
	Casts map[*ast.CastExpr]string

	// Tests maps each type test to the description of the type it tests
	// for, in the same form as Casts, and TestedTypes to that type.
	Tests       map[*ast.TypeTestExpr]string
	TestedTypes map[*ast.TypeTestExpr]Type

	// Signatures maps each function declaration and function expression to
	// the description of its type, in the same form as Casts.
	Signatures map[ast.Node]string
}

func NewInfo() *Info {
	return &Info{
		Types: make(map[ast.Expr]Type),
		Defs:  make(map[string]Type),
		Casts: make(map[*ast.CastExpr]string),

		Tests:       make(map[*ast.TypeTestExpr]string),
		TestedTypes: make(map[*ast.TypeTestExpr]Type),
		Signatures:  make(map[ast.Node]string),
	}
}

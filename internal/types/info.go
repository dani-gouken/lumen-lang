package types

import "github.com/yourname/lumen/internal/ast"

// Info holds the result of type checking.
type Info struct {
	// Types maps expressions to their computed types.
	Types map[ast.Expr]Type

	// Defs maps type names to their definitions (structs, enums, etc.)
	// This is populated during type checking for use by monomorphization
	Defs map[string]Type
}

func NewInfo() *Info {
	return &Info{
		Types: make(map[ast.Expr]Type),
		Defs:  make(map[string]Type),
	}
}

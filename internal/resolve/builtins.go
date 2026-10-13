package resolve

import "github.com/dani-gouken/lumen/internal/ast"

// Builtins returns declarations for the host-provided functions that every
// module can call without importing anything. They have no body; the VM
// supplies the implementation as extern functions of the same name.
func Builtins() []*ast.FnDecl {
	basic := func(name string) *ast.Type {
		return &ast.Type{Kind: ast.TypeBasic, Name: name}
	}
	variadicAny := func(name string) []*ast.Param {
		return []*ast.Param{{
			Name: name,
			Type: &ast.Type{Kind: ast.TypeVariadic, ElemType: basic("Any")},
		}}
	}

	return []*ast.FnDecl{
		{Name: "print", Params: variadicAny("args"), ReturnType: basic("Void")},
		{Name: "println", Params: variadicAny("args"), ReturnType: basic("Void")},
		{
			Name:       "toString",
			Params:     []*ast.Param{{Name: "value", Type: basic("Any")}},
			ReturnType: basic("String"),
		},
		// What `${expr}` in a string calls: toString under a name no
		// program writes
		{
			Name:       "__text",
			Params:     []*ast.Param{{Name: "value", Type: basic("Any")}},
			ReturnType: basic("String"),
		},
		// Stops the program with a message. It never returns.
		{Name: "panic", Params: variadicAny("message"), ReturnType: basic("Void")},
		{
			Name: "push",
			Params: []*ast.Param{
				{Name: "array", Type: basic("Any")},
				{Name: "value", Type: basic("Any")},
			},
			ReturnType: basic("Void"),
		},
	}
}

// IsBuiltin reports whether name is a host-provided function.
func IsBuiltin(name string) bool {
	for _, fn := range Builtins() {
		if fn.Name == name {
			return true
		}
	}
	return false
}

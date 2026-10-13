package repl

import (
	"strings"
	"unicode/utf8"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/types"
)

// Writing the type of a variable.
//
// A variable is declared again in every program, so its type has to be
// written out, including one that was never written: `let lines =
// io.readFile(path);` makes a Result<String, Error> of two modules the
// session may not have imported under any name. The checker knows such a
// type by the name it has in the whole program, std.result.Result, which
// is not something a program can write either. What can be written is
// the name through an import made for it:
//
//	import std.result as __repl_std_result;
//	let lines: __repl_std_result.Result<String, __repl_std_io.Error> = ...

// moduleAlias is the name a module is imported under to write the types
// of variables.
func moduleAlias(module string) string {
	return "__repl_" + strings.ReplaceAll(module, ".", "_")
}

// splitName takes a program-wide name apart: the module, "" for the
// session itself, and the name in it.
func splitName(name string) (module, short string) {
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		return name[:dot], name[dot+1:]
	}
	return "", name
}

// typeSource writes a type as a program of the session can: names of
// other modules through the imports of moduleAlias, which are added to
// modules.
func typeSource(t *ast.Type, modules map[string]bool) string {
	return writeType(t, func(module, short string) string {
		if module == "" {
			return short
		}
		modules[module] = true
		return moduleAlias(module) + "." + short
	})
}

// typeDisplay writes a type for someone to read: a name of another
// module as the module itself would be named, list.List.
func typeDisplay(t *ast.Type) string {
	return writeType(t, func(module, short string) string {
		if module == "" {
			return short
		}
		_, last := splitName(module)
		return last + "." + short
	})
}

func writeType(t *ast.Type, name func(module, short string) string) string {
	if t == nil {
		return "Void"
	}
	list := func(items []*ast.Type) string {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, writeType(item, name))
		}
		return strings.Join(parts, ", ")
	}

	switch t.Kind {
	case ast.TypeArray:
		return "[" + writeType(t.ElemType, name) + "]"
	case ast.TypeOption:
		inner := writeType(t.ElemType, name)
		if t.ElemType != nil && t.ElemType.Kind == ast.TypeFunction {
			inner = "(" + inner + ")"
		}
		return inner + "?"
	case ast.TypeVariadic:
		return "..." + writeType(t.ElemType, name)
	case ast.TypeUnion:
		parts := make([]string, 0, len(t.UnionTypes))
		for _, sub := range t.UnionTypes {
			parts = append(parts, writeType(sub, name))
		}
		return strings.Join(parts, " | ")
	case ast.TypeFunction:
		text := "fn(" + list(t.ParamTypes) + ")"
		if t.ReturnType != nil && !(t.ReturnType.Kind == ast.TypeBasic && t.ReturnType.Name == "Void") {
			text += " -> " + writeType(t.ReturnType, name)
		}
		return text
	}

	if ast.IsTupleName(t.Name) {
		return "(" + list(t.Generics) + ")"
	}
	text := t.Name
	if !types.IsBuiltinTypeName(t.Name) {
		text = name(splitName(t.Name))
	}
	if len(t.Generics) > 0 {
		text += "<" + list(t.Generics) + ">"
	}
	return text
}

// writable reports whether a type is one a variable can be declared
// with: every part of it has a name.
func writable(t *ast.Type) bool {
	if t == nil {
		return true
	}
	switch t.Kind {
	case ast.TypeBasic:
		if t.Name == "" || t.Name == "Void" || t.Name == "null" || t.Name == "unknown" {
			return false
		}
	case ast.TypeUnion:
		return false
	}
	for _, group := range [][]*ast.Type{{t.ElemType, t.ReturnType}, t.Generics, t.ParamTypes} {
		for _, part := range group {
			if part == nil {
				continue
			}
			if part == t.ReturnType && part.Kind == ast.TypeBasic && part.Name == "Void" {
				continue
			}
			if !writable(part) {
				return false
			}
		}
	}
	return true
}

// mentions reports whether a type names a struct, enum or trait of the
// session.
func mentions(t *ast.Type, name string) bool {
	if t == nil {
		return false
	}
	if t.Kind == ast.TypeBasic && t.Name == name {
		return true
	}
	for _, group := range [][]*ast.Type{{t.ElemType, t.ReturnType}, t.Generics, t.ParamTypes, t.UnionTypes} {
		for _, part := range group {
			if mentions(part, name) {
				return true
			}
		}
	}
	return false
}

func decodeRune(s string) (rune, int) {
	return utf8.DecodeRuneInString(s)
}

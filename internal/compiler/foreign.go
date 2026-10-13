package compiler

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/vm"
)

// The functions of C libraries a program declares:
//
//	extern struct Color { r: UInt8; g: UInt8; b: UInt8; a: UInt8; }
//	extern "raylib" fn ClearBackground(color: Color);
//
// To every stage they are extern functions and structs like the others,
// their C types read as the types of the program they are values of (see
// ast.ForeignTypes). What the library needs to be called is gathered
// here: the C type of each parameter, and of each field of the structs
// that go by value. It reaches the virtual machine as the name the chunk
// has for the function (see vm.Foreign).

// foreignFunctions describes the functions of C libraries of a program,
// by the name the program has for each, and reports the declarations no
// C function could have.
func foreignFunctions(prog *ast.Program, errs *errors.Handler, lines map[int]string) map[string]string {
	structs := make(map[string]*ast.StructDecl)
	for _, decl := range prog.Decls {
		if d, ok := decl.(*ast.StructDecl); ok {
			structs[d.Name] = d
		}
	}
	f := &foreignTypes{structs: structs, errs: errs, lines: lines, known: make(map[string]*vm.CType)}

	// Every struct of a C library is checked, used or not: what is wrong
	// with one is wrong where it is declared
	for _, decl := range prog.Decls {
		if d, ok := decl.(*ast.StructDecl); ok && d.Extern {
			f.structType(d)
		}
	}

	var described map[string]string
	for _, fn := range prog.Externs {
		if fn.Library == "" {
			continue
		}
		foreign := &vm.Foreign{Name: fn.Name, Library: fn.Library, Symbol: fn.Symbol, Result: vm.CType{Kind: vm.CVoid}}
		for _, param := range fn.Params {
			paramType, _ := f.typeOf(param.Type, "a parameter")
			foreign.Params = append(foreign.Params, vm.CField{Name: param.Name, Type: paramType})
		}
		if fn.ReturnType != nil {
			foreign.Result, _ = f.typeOf(fn.ReturnType, "the result")
		}
		if described == nil {
			described = make(map[string]string)
		}
		described[fn.Name] = foreign.ExternName()
	}
	return described
}

type foreignTypes struct {
	structs map[string]*ast.StructDecl
	errs    *errors.Handler
	lines   map[int]string

	known   map[string]*vm.CType // The structs already described
	inside  []string             // Those being described
	refused map[*ast.Type]bool
}

// typeOf is the C type a type was written as. what says where it is, for
// the message when there is none: it is then reported, here or where the
// struct it names is declared.
func (f *foreignTypes) typeOf(t *ast.Type, what string) (vm.CType, bool) {
	if t.Kind == ast.TypeBasic && len(t.Generics) == 0 {
		switch {
		case t.C != "":
			return vm.CType{Kind: vm.CTypeNames[t.C]}, true
		case t.Name == "Bool":
			return vm.CType{Kind: vm.CBool}, true
		}
		if decl := f.structs[t.Name]; decl != nil && decl.Extern {
			if described := f.structType(decl); described != nil {
				return *described, true
			}
			return vm.CType{Kind: vm.CPointer}, false
		}
	}

	suggestion := "Say which type of C it is: Int8, UInt8, Int16, UInt16, Int32, UInt32, Int64, UInt64, Float32, Float64, Bool, CString, Pointer, or a struct declared with 'extern struct'"
	switch {
	case t.Kind == ast.TypeBasic && t.Name == "Int":
		suggestion = "C has whole numbers of several sizes. Say which: Int32 for an int, Int64 for a long, UInt8 for an unsigned char, ..."
	case t.Kind == ast.TypeBasic && t.Name == "Float":
		suggestion = "Say which: Float32 for a float, Float64 for a double"
	case t.Kind == ast.TypeBasic && t.Name == "String":
		suggestion = "Write CString for a char * that holds a text"
	case t.Kind == ast.TypeBasic && f.structs[t.Name] != nil:
		suggestion = fmt.Sprintf("Declare it for C, with the C type of each field: extern struct %s { ... }", t.Name)
	case t.Kind == ast.TypeArray || t.Kind == ast.TypeOption:
		suggestion = "C takes the address of the first value: a Pointer. See std.ffi for memory a C function can fill"
	}
	if !f.refused[t] {
		if f.refused == nil {
			f.refused = make(map[*ast.Type]bool)
		}
		f.refused[t] = true
		f.errs.ReportErrorWithSuggestion(ErrForeignType,
			fmt.Sprintf("'%s' is not a type of C: it cannot be %s of a function of a C library", t.String(), what),
			suggestion, t.Token(), f.lines)
	}
	return vm.CType{Kind: vm.CPointer}, false
}

// structType describes a struct of a C library, or returns nil for one
// that cannot be: it holds itself, or a text.
func (f *foreignTypes) structType(decl *ast.StructDecl) *vm.CType {
	if described, ok := f.known[decl.Name]; ok {
		return described
	}
	for _, name := range f.inside {
		if name == decl.Name {
			f.errs.ReportErrorWithSuggestion(ErrForeignType,
				fmt.Sprintf("the struct '%s' holds itself: in C it would have no size", decl.Name),
				"A struct of C that refers to another of its kind holds its address: a Pointer", decl.Token(), f.lines)
			return nil
		}
	}
	f.inside = append(f.inside, decl.Name)
	defer func() { f.inside = f.inside[:len(f.inside)-1] }()

	described := &vm.CType{Kind: vm.CStruct, Name: decl.Name}
	ok := true
	if len(decl.Fields) == 0 {
		f.errs.ReportErrorWithSuggestion(ErrForeignType,
			fmt.Sprintf("the struct '%s' has no field: C has no value of no size", decl.Name),
			"A value a library only gives the address of is a Pointer", decl.Token(), f.lines)
		ok = false
	}
	for _, field := range decl.Fields {
		if field.Type.C == "CString" {
			f.errs.ReportErrorWithSuggestion(ErrForeignType,
				fmt.Sprintf("the field '%s' of a struct of C cannot be a CString", field.Name),
				"A text given to C only lasts for the call. Make the field a Pointer: std.ffi.cstring makes a text that stays, std.ffi.text reads one",
				field.Type.Token(), f.lines)
			ok = false
			continue
		}
		fieldType, fine := f.typeOf(field.Type, "a field")
		ok = ok && fine
		described.Fields = append(described.Fields, vm.CField{Name: field.Name, Type: fieldType})
	}
	if !ok {
		described = nil
	}
	f.known[decl.Name] = described
	return described
}

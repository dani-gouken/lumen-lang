package vm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Functions of C libraries.
//
// A program declares one with the library it is in and its C types:
//
//	extern "raylib" fn InitWindow(width: Int32, height: Int32, title: CString);
//
// Nothing of it is known to lumen beforehand, so what the chunk names is
// not a function the VM has but the whole declaration: ForeignPrefix
// followed by a Foreign as JSON. The VM finds the library and the function
// the first time the program uses it (see externObject), and from then on
// it is called like any other extern function.
//
// The bytecode format knows nothing of this: an extern is a name there,
// and this is a name that says more than the others.

// ForeignPrefix starts the extern name of a function of a C library.
const ForeignPrefix = "ffi:"

// The kinds of C values a function takes and returns.
const (
	CInt8    = "i8"
	CUInt8   = "u8"
	CInt16   = "i16"
	CUInt16  = "u16"
	CInt32   = "i32"
	CUInt32  = "u32"
	CInt64   = "i64"
	CUInt64  = "u64"
	CFloat32 = "f32"
	CFloat64 = "f64"
	CBool    = "bool"
	CString  = "str"    // char *, read as text up to its 0
	CPointer = "ptr"    // Any other pointer: an address, as an Int
	CVoid    = "void"   // Nothing: only what a function returns
	CStruct  = "struct" // By value
)

// CTypeNames are the names the C types have in a declaration.
var CTypeNames = map[string]string{
	"Int8": CInt8, "UInt8": CUInt8, "Int16": CInt16, "UInt16": CUInt16,
	"Int32": CInt32, "UInt32": CUInt32, "Int64": CInt64, "UInt64": CUInt64,
	"Float32": CFloat32, "Float64": CFloat64, "CString": CString, "Pointer": CPointer,
}

// CType is the type of a C value.
type CType struct {
	Kind string `json:"k"`
	// Of a struct: the name its values have in the program, and its
	// fields in the order C has them
	Name   string   `json:"n,omitempty"`
	Fields []CField `json:"f,omitempty"`
}

// CField is a field of a struct, or a parameter of a function.
type CField struct {
	Name string `json:"n"`
	Type CType  `json:"t"`
}

// Foreign is a function of a C library, as a program declares it.
type Foreign struct {
	Name    string   `json:"name"` // What the program calls it
	Library string   `json:"lib"`
	Symbol  string   `json:"sym"` // What the library calls it
	Params  []CField `json:"args,omitempty"`
	Result  CType    `json:"ret"`
}

// ExternName is the name a chunk has for the function.
func (f *Foreign) ExternName() string {
	text, err := json.Marshal(f)
	if err != nil {
		// A Foreign is made of texts and lists of them
		panic(err)
	}
	return ForeignPrefix + string(text)
}

// IsForeign reports whether an extern name is that of a function of a C
// library.
func IsForeign(name string) bool {
	return strings.HasPrefix(name, ForeignPrefix)
}

// ParseForeign reads the extern name of a function of a C library. The
// name comes from a chunk, which may have been written by anything: what
// it describes is checked before anything is made of it.
func ParseForeign(name string) (*Foreign, error) {
	if !IsForeign(name) {
		return nil, fmt.Errorf("not a function of a C library: %s", name)
	}
	f := &Foreign{}
	if err := json.Unmarshal([]byte(name[len(ForeignPrefix):]), f); err != nil {
		return nil, fmt.Errorf("a function of a C library is not described properly: %v", err)
	}
	if f.Library == "" || f.Symbol == "" {
		return nil, fmt.Errorf("a function of a C library needs a library and a name")
	}
	if f.Name == "" {
		f.Name = f.Symbol
	}
	if len(f.Params) > maxForeignParams {
		return nil, fmt.Errorf("%s: more than %d parameters", f.Name, maxForeignParams)
	}
	for i := range f.Params {
		if err := f.Params[i].Type.check(false, 0); err != nil {
			return nil, fmt.Errorf("%s: parameter %d: %v", f.Name, i+1, err)
		}
	}
	if err := f.Result.check(true, 0); err != nil {
		return nil, fmt.Errorf("%s: result: %v", f.Name, err)
	}
	return f, nil
}

const (
	maxForeignParams = 32
	maxStructDepth   = 8
	maxStructLeaves  = 256
)

func (t *CType) check(result bool, depth int) error {
	switch t.Kind {
	case CInt8, CUInt8, CInt16, CUInt16, CInt32, CUInt32, CInt64, CUInt64,
		CFloat32, CFloat64, CBool, CPointer:
		return nil
	case CString:
		if depth > 0 {
			return fmt.Errorf("a struct cannot hold a CString: use a Pointer")
		}
		return nil
	case CVoid:
		if !result || depth > 0 {
			return fmt.Errorf("only the result of a function can be nothing")
		}
		return nil
	case CStruct:
		if depth >= maxStructDepth {
			return fmt.Errorf("structs nested more than %d deep", maxStructDepth)
		}
		if t.Name == "" || len(t.Fields) == 0 {
			return fmt.Errorf("a struct needs a name and at least one field")
		}
		for i := range t.Fields {
			if t.Fields[i].Name == "" {
				return fmt.Errorf("struct %s: a field has no name", t.Name)
			}
			if err := t.Fields[i].Type.check(false, depth+1); err != nil {
				return fmt.Errorf("struct %s: field %s: %v", t.Name, t.Fields[i].Name, err)
			}
		}
		if len(t.leaves(0, nil)) > maxStructLeaves {
			return fmt.Errorf("struct %s: more than %d values", t.Name, maxStructLeaves)
		}
		return nil
	}
	return fmt.Errorf("unknown C type %q", t.Kind)
}

// --- How C lays a struct out ---

// cLeaf is one value of a struct that is not itself a struct, and where C
// puts it.
type cLeaf struct {
	kind   string
	offset uintptr
}

// sizeAndAlign is the room a C value takes, and the multiple its address
// has to be of. They are those of every 64-bit system lumen runs on.
func (t *CType) sizeAndAlign() (size, align uintptr) {
	switch t.Kind {
	case CInt8, CUInt8, CBool:
		return 1, 1
	case CInt16, CUInt16:
		return 2, 2
	case CInt32, CUInt32, CFloat32:
		return 4, 4
	case CInt64, CUInt64, CFloat64, CPointer, CString:
		return 8, 8
	case CStruct:
		align = 1
		for i := range t.Fields {
			fieldSize, fieldAlign := t.Fields[i].Type.sizeAndAlign()
			size = alignUp(size, fieldAlign) + fieldSize
			if fieldAlign > align {
				align = fieldAlign
			}
		}
		return alignUp(size, align), align
	}
	return 0, 1
}

func alignUp(n, align uintptr) uintptr {
	return (n + align - 1) / align * align
}

// leaves lists the values of a struct in the order they are in memory,
// those of the structs inside it included.
func (t *CType) leaves(base uintptr, into []cLeaf) []cLeaf {
	offset := base
	for i := range t.Fields {
		field := &t.Fields[i].Type
		size, align := field.sizeAndAlign()
		offset = alignUp(offset, align)
		if field.Kind == CStruct {
			into = field.leaves(offset, into)
		} else {
			into = append(into, cLeaf{field.Kind, offset})
		}
		offset += size
	}
	return into
}

// --- Between the values of a program and those of C ---

// inRange checks that a whole number fits the C type it is given to.
func cIntRange(kind string) (low, high int64) {
	switch kind {
	case CInt8:
		return -1 << 7, 1<<7 - 1
	case CUInt8:
		return 0, 1<<8 - 1
	case CInt16:
		return -1 << 15, 1<<15 - 1
	case CUInt16:
		return 0, 1<<16 - 1
	case CInt32:
		return -1 << 31, 1<<31 - 1
	case CUInt32:
		return 0, 1<<32 - 1
	case CUInt64, CPointer:
		return 0, MaxInt
	}
	return MinInt, MaxInt
}

// cTypeName is how a C type is written in a program.
func cTypeName(t *CType) string {
	if t.Kind == CStruct {
		return t.Name
	}
	if t.Kind == CBool {
		return "Bool"
	}
	for name, kind := range CTypeNames {
		if kind == t.Kind {
			return name
		}
	}
	return t.Kind
}

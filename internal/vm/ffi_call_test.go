//go:build (darwin || linux) && (arm64 || amd64)

package vm

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// The Go struct given to purego for a struct of C is the same memory:
// every value where C has it, the gaps Go would not leave filled in.
func TestForeignStructsAreLaidOutAsInC(t *testing.T) {
	scalar := func(kind string) CType { return CType{Kind: kind} }
	inner := CType{Kind: CStruct, Name: "Inner", Fields: []CField{{"weight", scalar(CFloat64)}, {"tag", scalar(CUInt8)}}}
	types := []CType{
		{Kind: CStruct, Name: "Color", Fields: []CField{{"r", scalar(CUInt8)}, {"g", scalar(CUInt8)}, {"b", scalar(CUInt8)}, {"a", scalar(CUInt8)}}},
		{Kind: CStruct, Name: "Padded", Fields: []CField{{"inner", inner}, {"flag", scalar(CUInt8)}, {"count", scalar(CInt32)}}},
		{Kind: CStruct, Name: "Twice", Fields: []CField{{"first", inner}, {"second", inner}, {"last", scalar(CBool)}}},
		{Kind: CStruct, Name: "Mixed", Fields: []CField{
			{"a", scalar(CInt8)}, {"b", scalar(CInt16)}, {"c", scalar(CInt32)}, {"d", scalar(CInt64)},
			{"e", scalar(CUInt8)}, {"f", scalar(CUInt16)}, {"g", scalar(CUInt32)}, {"h", scalar(CUInt64)},
			{"i", scalar(CFloat32)}, {"j", scalar(CFloat64)}, {"k", scalar(CBool)}, {"p", scalar(CPointer)},
		}},
	}
	for _, ctype := range types {
		value, err := newCValue(&ctype)
		if err != nil {
			t.Errorf("%s: %v", ctype.Name, err)
			continue
		}
		size, align := ctype.sizeAndAlign()
		if value.goType.Size() != size || uintptr(value.goType.Align()) != align {
			t.Errorf("%s: expected %d bytes aligned on %d, got %d on %d", ctype.Name, size, align, value.goType.Size(), value.goType.Align())
		}
		for i, leaf := range ctype.leaves(0, nil) {
			field := value.goType.Field(value.fields[i])
			if field.Offset != leaf.offset || field.Type != goTypes[leaf.kind] {
				t.Errorf("%s: value %d: expected a %s at %d, got a %s at %d", ctype.Name, i, goTypes[leaf.kind], leaf.offset, field.Type, field.Offset)
			}
		}
	}
}

// How each struct is given on arm64: floats, words, or its address; and
// the whole function the slow way once the registers run out.
func TestForeignStructsInRegisters(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("structs are only given as numbers on arm64")
	}
	scalar := func(kind string) CType { return CType{Kind: kind} }
	structOf := func(name string, kinds ...string) CType {
		t := CType{Kind: CStruct, Name: name}
		for i, kind := range kinds {
			t.Fields = append(t.Fields, CField{Name: string(rune('a' + i)), Type: scalar(kind)})
		}
		return t
	}
	color := structOf("Color", CUInt8, CUInt8, CUInt8, CUInt8)
	vec := structOf("Vec2", CFloat32, CFloat32)
	rect := structOf("Rect", CFloat32, CFloat32, CFloat32, CFloat32)
	point := structOf("Point", CFloat64, CFloat64)
	pair := structOf("Pair", CFloat32, CInt32)
	image := structOf("Image", CPointer, CInt32, CInt32, CInt32)
	five := structOf("Five", CFloat32, CFloat32, CFloat32, CFloat32, CFloat32)
	two := structOf("Two", CInt64, CInt64)
	mixed := structOf("Mixed", CFloat32, CFloat64)

	word, address := goTypes[CUInt64], goTypes[CPointer]
	f32, f64, i32 := goTypes[CFloat32], goTypes[CFloat64], goTypes[CInt32]

	cases := []struct {
		name    string
		params  []CType
		lowered bool
		in      []reflect.Type
	}{
		{"four bytes: a word", []CType{color}, true, []reflect.Type{word}},
		{"two floats", []CType{vec}, true, []reflect.Type{f32, f32}},
		{"four floats", []CType{rect}, true, []reflect.Type{f32, f32, f32, f32}},
		{"two doubles", []CType{point}, true, []reflect.Type{f64, f64}},
		{"a float and a whole number: a word", []CType{pair}, true, []reflect.Type{word}},
		{"a float and a double: two words", []CType{mixed}, true, []reflect.Type{word, word}},
		{"sixteen bytes: two words", []CType{two}, true, []reflect.Type{word, word}},
		{"twenty-four bytes: its address", []CType{image}, true, []reflect.Type{address}},
		{"five floats: its address", []CType{five}, true, []reflect.Type{address}},
		{"between numbers", []CType{scalar(CInt32), color, scalar(CFloat32), vec, scalar(CInt32)}, true,
			[]reflect.Type{i32, word, f32, f32, f32, i32}},
		{"the float registers just last", []CType{rect, rect}, true,
			[]reflect.Type{f32, f32, f32, f32, f32, f32, f32, f32}},
		{"the float registers run out", []CType{rect, rect, vec}, false, nil},
		{"the float registers run out on a number", []CType{rect, rect, scalar(CFloat32)}, true,
			[]reflect.Type{f32, f32, f32, f32, f32, f32, f32, f32, f32}},
		{"the word registers just last", []CType{two, two, two, two}, true,
			[]reflect.Type{word, word, word, word, word, word, word, word}},
		{"the word registers run out", []CType{two, two, two, two, color}, false, nil},
		{"a struct after the registers ran out", []CType{
			scalar(CInt32), scalar(CInt32), scalar(CInt32), scalar(CInt32),
			scalar(CInt32), scalar(CInt32), scalar(CInt32), scalar(CInt32), color}, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &Foreign{Name: "f", Library: "c", Symbol: "f", Result: CType{Kind: CVoid}}
			for i, param := range c.params {
				f.Params = append(f.Params, CField{Name: string(rune('a' + i)), Type: param})
			}
			params, in, err := lowerParams(f)
			if err != nil {
				t.Fatal(err)
			}
			lowered := false
			for _, p := range params {
				lowered = lowered || p.lowered
			}
			if lowered != c.lowered {
				t.Fatalf("expected given as numbers: %v, got %v", c.lowered, lowered)
			}
			if !c.lowered {
				// One argument for each parameter, the structs as structs
				if len(in) != len(c.params) {
					t.Errorf("expected %d arguments, got %d", len(c.params), len(in))
				}
				return
			}
			if !reflect.DeepEqual(in, c.in) {
				t.Errorf("expected %v, got %v", c.in, in)
			}
		})
	}
}

func TestLibraryFiles(t *testing.T) {
	t.Setenv(LibraryPathVariable, "/first/place"+string(os.PathListSeparator)+"/second/place")

	// A path is that file and no other
	for _, path := range []string{"/opt/lib/libfoo.dylib", "./libfoo.so", "vendor/foo"} {
		if files := libraryFiles(path); len(files) != 1 || files[0] != path {
			t.Errorf("%s: expected that file alone, got %v", path, files)
		}
	}

	files := libraryFiles("raylib")
	extension := ".so"
	if runtime.GOOS == "darwin" {
		extension = ".dylib"
	}
	// By its name alone first, for the system to look where it looks
	if files[0] != "libraylib"+extension {
		t.Errorf("expected the name alone first, got %s", files[0])
	}
	first, second := -1, -1
	for i, file := range files {
		switch file {
		case "/first/place/libraylib" + extension:
			first = i
		case "/second/place/libraylib" + extension:
			second = i
		}
	}
	if first < 0 || second < first {
		t.Errorf("expected the folders of %s in order, got %v", LibraryPathVariable, files)
	}
	usual := "/usr/local/lib/libraylib" + extension
	if at := strings.Index(strings.Join(files, "\n"), usual); at < 0 {
		t.Errorf("expected %s among %v", usual, files)
	}

	// A name that says what kind of file it is is not given another
	for _, file := range libraryFiles("libfoo.so.3") {
		if !strings.HasSuffix(file, "libfoo.so.3") {
			t.Errorf("expected libfoo.so.3 somewhere, got %s", file)
		}
	}

	// The C library is one the system has, under a name of its own
	for _, name := range []string{"c", "m"} {
		if files := libraryFiles(name); len(files) != 1 {
			t.Errorf("%s: expected one file, got %v", name, files)
		} else if _, err := openLibrary(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

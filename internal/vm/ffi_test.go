package vm

import (
	"strings"
	"testing"
)

// What a chunk says of a function of a C library is read back as it was
// written.
func TestForeignNameRoundTrip(t *testing.T) {
	vec := CType{Kind: CStruct, Name: "gfx.Vec2", Fields: []CField{{"x", CType{Kind: CFloat32}}, {"y", CType{Kind: CFloat32}}}}
	written := &Foreign{
		Name: "gfx.DrawLine", Library: "raylib", Symbol: "DrawLineV",
		Params: []CField{
			{"from", vec}, {"to", vec},
			{"camera", CType{Kind: CStruct, Name: "gfx.Camera", Fields: []CField{{"offset", vec}, {"zoom", CType{Kind: CFloat32}}}}},
			{"title", CType{Kind: CString}}, {"data", CType{Kind: CPointer}}, {"on", CType{Kind: CBool}},
		},
		Result: CType{Kind: CVoid},
	}
	name := written.ExternName()
	if !IsForeign(name) || IsForeign("std.math.floor") || IsForeign("") {
		t.Fatalf("expected %q to be the name of a function of C, and no other", name)
	}
	read, err := ParseForeign(name)
	if err != nil {
		t.Fatal(err)
	}
	if read.ExternName() != name {
		t.Errorf("read back differently:\n  %s\n  %s", name, read.ExternName())
	}
	if read.Name != "gfx.DrawLine" || read.Symbol != "DrawLineV" || len(read.Params) != 6 ||
		read.Params[2].Type.Fields[0].Type.Name != "gfx.Vec2" || read.Result.Kind != CVoid {
		t.Errorf("unexpected: %+v", read)
	}
}

// A chunk can be written by anything: what it says of a function of C is
// checked before a library is opened for it.
func TestForeignNamesRefused(t *testing.T) {
	deep := `{"k":"i32"}`
	for i := 0; i < 12; i++ {
		deep = `{"k":"struct","n":"S","f":[{"n":"x","t":` + deep + `}]}`
	}
	var wide strings.Builder
	for i := 0; i < 300; i++ {
		if i > 0 {
			wide.WriteString(",")
		}
		wide.WriteString(`{"n":"f","t":{"k":"u8"}}`)
	}
	var many strings.Builder
	for i := 0; i < 40; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"n":"a","t":{"k":"i32"}}`)
	}

	cases := []struct{ name, described, want string }{
		{"not a function of C", `std.math.floor`, "not a function of a C library"},
		{"nothing", `ffi:`, "not described properly"},
		{"not JSON", `ffi:InitWindow`, "not described properly"},
		{"cut short", `ffi:{"lib":"c","sym":"abs","ret":{"k":"i3`, "not described properly"},
		{"no library", `ffi:{"sym":"abs","ret":{"k":"i32"}}`, "needs a library and a name"},
		{"no function", `ffi:{"lib":"c","ret":{"k":"i32"}}`, "needs a library and a name"},
		{"no result", `ffi:{"lib":"c","sym":"abs"}`, `unknown C type ""`},
		{"a kind that does not exist", `ffi:{"lib":"c","sym":"abs","ret":{"k":"i128"}}`, `unknown C type "i128"`},
		{"a parameter that is nothing", `ffi:{"lib":"c","sym":"f","args":[{"n":"a","t":{"k":"void"}}],"ret":{"k":"void"}}`, "only the result of a function can be nothing"},
		{"a struct without fields", `ffi:{"lib":"c","sym":"f","args":[{"n":"a","t":{"k":"struct","n":"S"}}],"ret":{"k":"void"}}`, "needs a name and at least one field"},
		{"a struct without a name", `ffi:{"lib":"c","sym":"f","ret":{"k":"struct","f":[{"n":"x","t":{"k":"u8"}}]}}`, "needs a name and at least one field"},
		{"a field without a name", `ffi:{"lib":"c","sym":"f","ret":{"k":"struct","n":"S","f":[{"t":{"k":"u8"}}]}}`, "a field has no name"},
		{"a field that is nothing", `ffi:{"lib":"c","sym":"f","ret":{"k":"struct","n":"S","f":[{"n":"x","t":{"k":"void"}}]}}`, "only the result of a function can be nothing"},
		{"a text in a struct", `ffi:{"lib":"c","sym":"f","ret":{"k":"struct","n":"S","f":[{"n":"x","t":{"k":"str"}}]}}`, "a struct cannot hold a CString"},
		{"structs too deep", `ffi:{"lib":"c","sym":"f","ret":` + deep + `}`, "nested more than"},
		{"a struct too wide", `ffi:{"lib":"c","sym":"f","ret":{"k":"struct","n":"S","f":[` + wide.String() + `]}}`, "more than 256 values"},
		{"too many parameters", `ffi:{"lib":"c","sym":"f","args":[` + many.String() + `],"ret":{"k":"void"}}`, "more than 32 parameters"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseForeign(c.described)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("expected to be refused with %q, got %v", c.want, err)
			}
		})
	}
}

// A chunk that names a function of C badly fails where the function is
// used, as a failure of the program: nothing is opened, nothing crashes.
func TestForeignNameRefusedWhenUsed(t *testing.T) {
	for _, name := range []string{
		`ffi:`, `ffi:{"lib":"c","sym":"abs","ret":{"k":"i128"}}`,
		`ffi:{"lib":"/no/such/library.dylib","sym":"abs","ret":{"k":"i32"}}`,
	} {
		chunk := NewChunk()
		chunk.AddExtern(name)
		chunk.Code = []Instr{
			NewInstr(OpNEWEXFUNC, 0, 0, 0),
			NewInstr(OpRET, 0, 0, 0),
		}
		addMainProto(chunk, 2, 0)
		_, err := NewVM().Exec(chunk)
		failure, ok := err.(*RuntimeError)
		if !ok || failure.Kind != KindLibrary {
			t.Errorf("%s: expected a failure of the library, got %v", name, err)
		}
	}
}

// Where C puts the values of a struct.
func TestStructLayout(t *testing.T) {
	scalar := func(kind string) CType { return CType{Kind: kind} }
	structOf := func(name string, fields ...CType) CType {
		t := CType{Kind: CStruct, Name: name}
		for i, field := range fields {
			t.Fields = append(t.Fields, CField{Name: string(rune('a' + i)), Type: field})
		}
		return t
	}
	vec := structOf("Vec2", scalar(CFloat32), scalar(CFloat32))
	inner := structOf("Inner", scalar(CFloat64), scalar(CUInt8))

	cases := []struct {
		name    string
		ctype   CType
		size    uintptr
		align   uintptr
		offsets []uintptr
	}{
		{"four bytes", structOf("Color", scalar(CUInt8), scalar(CUInt8), scalar(CUInt8), scalar(CUInt8)), 4, 1, []uintptr{0, 1, 2, 3}},
		{"two floats", vec, 8, 4, []uintptr{0, 4}},
		{"a byte before a word", structOf("S", scalar(CUInt8), scalar(CInt64)), 16, 8, []uintptr{0, 8}},
		{"a word before a byte", inner, 16, 8, []uintptr{0, 8}},
		{"sizes going up", structOf("S", scalar(CInt8), scalar(CInt16), scalar(CInt32), scalar(CInt64)), 16, 8, []uintptr{0, 2, 4, 8}},
		{"sizes going down", structOf("S", scalar(CInt64), scalar(CInt32), scalar(CInt16), scalar(CInt8)), 16, 8, []uintptr{0, 8, 12, 14}},
		{"a byte between shorts", structOf("S", scalar(CInt16), scalar(CUInt8), scalar(CInt16)), 6, 2, []uintptr{0, 2, 4}},
		{"an address among numbers", structOf("Image", scalar(CPointer), scalar(CInt32), scalar(CInt32), scalar(CInt32)), 24, 8, []uintptr{0, 8, 12, 16}},
		{"structs in a struct", structOf("Camera", vec, vec, scalar(CFloat32), scalar(CFloat32)), 24, 4, []uintptr{0, 4, 8, 12, 16, 20}},
		// The struct inside ends at 9 and takes 16: what follows is at 16
		{"after a struct rounded up", structOf("Padded", inner, scalar(CUInt8), scalar(CInt32)), 24, 8, []uintptr{0, 8, 16, 20}},
		{"a struct after a byte", structOf("S", scalar(CUInt8), inner), 24, 8, []uintptr{0, 8, 16}},
		{"a struct of bytes after a byte", structOf("S", scalar(CBool), structOf("C", scalar(CUInt8), scalar(CUInt8))), 3, 1, []uintptr{0, 1, 2}},
		{"three deep", structOf("S", scalar(CUInt8), structOf("M", scalar(CUInt8), structOf("I", scalar(CInt16), scalar(CFloat64)))), 32, 8, []uintptr{0, 8, 16, 24}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			size, align := c.ctype.sizeAndAlign()
			if size != c.size || align != c.align {
				t.Errorf("expected %d bytes aligned on %d, got %d on %d", c.size, c.align, size, align)
			}
			leaves := c.ctype.leaves(0, nil)
			if len(leaves) != len(c.offsets) {
				t.Fatalf("expected %d values, got %d", len(c.offsets), len(leaves))
			}
			for i, leaf := range leaves {
				if leaf.offset != c.offsets[i] {
					t.Errorf("value %d: expected at %d, got %d", i, c.offsets[i], leaf.offset)
				}
			}
		})
	}
}

func TestCIntRanges(t *testing.T) {
	cases := []struct {
		kind      string
		low, high int64
	}{
		{CInt8, -128, 127}, {CUInt8, 0, 255}, {CInt16, -32768, 32767}, {CUInt16, 0, 65535},
		{CInt32, -2147483648, 2147483647}, {CUInt32, 0, 4294967295},
		{CInt64, MinInt, MaxInt}, {CUInt64, 0, MaxInt}, {CPointer, 0, MaxInt},
	}
	for _, c := range cases {
		if low, high := cIntRange(c.kind); low != c.low || high != c.high {
			t.Errorf("%s: expected %d to %d, got %d to %d", c.kind, c.low, c.high, low, high)
		}
	}
	// Every C type has one name in a declaration
	for name, kind := range CTypeNames {
		if got := cTypeName(&CType{Kind: kind}); got != name {
			t.Errorf("%s: expected to be written %s, got %s", kind, name, got)
		}
	}
}

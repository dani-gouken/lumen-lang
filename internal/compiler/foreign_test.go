package compiler

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/ffitest"
	"github.com/dani-gouken/lumen/internal/vm"
)

// The declarations of functions and structs of C libraries: what is
// refused before a program runs, and what the chunk of a program says of
// the ones it uses.

func TestForeignDeclarationsRefused(t *testing.T) {
	cases := []struct{ name, src, msg string }{
		{"an Int as parameter",
			`extern "c" fn abs(n: Int) -> Int32;`,
			"'Int' is not a type of C: it cannot be a parameter of a function of a C library"},
		{"a Float as result",
			`extern "m" fn sqrt(x: Float64) -> Float;`,
			"'Float' is not a type of C: it cannot be the result of a function of a C library"},
		{"a String as parameter",
			`extern "c" fn strlen(text: String) -> UInt64;`,
			"'String' is not a type of C"},
		{"an array as parameter",
			`extern "c" fn sum(values: [Int32]) -> Int32;`,
			"'[Int32]' is not a type of C"},
		{"an optional result",
			`extern "c" fn getenv(name: CString) -> CString?;`,
			"is not a type of C: it cannot be the result"},
		{"a struct of the program",
			`struct Point { x: Float; y: Float; }
			extern "c" fn length(p: Point) -> Float64;`,
			"'Point' is not a type of C: it cannot be a parameter"},
		{"an enum",
			`enum Mode { On, Off }
			extern "c" fn set(mode: Mode);`,
			"'Mode' is not a type of C"},
		{"a function as parameter",
			`extern "c" fn each(callback: fn(Int) -> Int);`,
			"is not a type of C"},
		{"a type that does not exist",
			`extern "c" fn make() -> Widget;`,
			"'Widget' is not a type of C"},
		{"a field that is not of C",
			`extern struct Point { x: Float32; y: Float; }`,
			"'Float' is not a type of C: it cannot be a field"},
		{"a field that is a text",
			`extern struct Named { name: CString; size: Int32; }`,
			"the field 'name' of a struct of C cannot be a CString"},
		{"a field that is a struct of the program",
			`struct Point { x: Float; }
			extern struct Line { from: Point; }`,
			"'Point' is not a type of C: it cannot be a field"},
		{"a struct that holds itself",
			`extern struct Node { value: Int32; next: Node; }`,
			"the struct 'Node' holds itself"},
		{"structs that hold each other",
			`extern struct A { b: B; }
			extern struct B { a: A; }`,
			"holds itself"},
		{"a struct with no field",
			`extern struct Nothing { }`,
			"the struct 'Nothing' has no field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectModulesError(t, sources{"main.lum": c.src + "\nfn main() -> Int { return 0; }"},
				ErrForeignType, "main.lum", c.msg)
		})
	}

	// Reported in the module it is written in, used or not
	expectModulesError(t, sources{
		"main.lum":     "import gfx.draw;\nfn main() -> Int { return 0; }",
		"gfx/draw.lum": "\npub extern \"gfx\" fn line(width: Int);\n",
	}, ErrForeignType, "gfx/draw.lum", "'Int' is not a type of C")
}

// What is wrong with a struct is said once, where it is declared, however
// many functions take it.
func TestForeignStructReportedOnce(t *testing.T) {
	_, errs := compileModules(sources{"main.lum": `
		extern struct Bad { x: Float; }
		extern "c" fn one(b: Bad);
		extern "c" fn two(b: Bad) -> Bad;
		fn main() -> Int { return 0; }
	`})
	if len(errs.Diagnostics) != 1 {
		for _, d := range errs.Diagnostics {
			t.Logf("%s", d.String())
		}
		t.Fatalf("expected one diagnostic, got %d", len(errs.Diagnostics))
	}
	if d := errs.Diagnostics[0]; d.Code != ErrForeignType || d.Pos.Line != 2 {
		t.Errorf("expected %s on line 2, got %s on line %d", ErrForeignType, d.Code, d.Pos.Line)
	}
}

// The types of C are the program's types to everything else: what is
// given to a function of C is checked like any other argument.
func TestForeignCallsAreTypeChecked(t *testing.T) {
	cases := []struct{ name, body, msg string }{
		{"a text for a number", `add("1", 2);`, "cannot use type string as argument of type int"},
		{"a float for a whole number", `add(1.5, 2);`, "cannot use type float as argument of type int"},
		{"a number for a text", `greet(3);`, "as argument of type string"},
		{"too few arguments", `add(1);`, "argument"},
		{"a struct of another kind", `pack(Size { w: 1.0, h: 2.0 });`, "argument"},
		{"a result used as something else", `let s: String = add(1, 2);`, "int"},
		{"a field that the struct does not have", `let c = unpack(1); println(c.alpha);`, "alpha"},
		{"a field given the wrong type", `pack(Color { r: "red", g: 0, b: 0, a: 0 });`, "string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errs := compileModules(sources{"main.lum": `
				extern struct Color { r: UInt8; g: UInt8; b: UInt8; a: UInt8; }
				extern struct Size { w: Float32; h: Float32; }
				extern "probe" fn add(a: Int32, b: Int32) -> Int32;
				extern "probe" fn greet(name: CString) -> CString;
				extern "probe" fn pack(c: Color) -> UInt32;
				extern "probe" fn unpack(packed: UInt32) -> Color;
				fn main() { ` + c.body + ` }
			`})
			if !errs.HasErrors() {
				t.Fatal("expected a diagnostic, got none")
			}
			if d := errs.Diagnostics[0]; !strings.Contains(d.Message, c.msg) {
				t.Errorf("expected a message containing %q, got %s: %s", c.msg, d.Code, d.Message)
			}
		})
	}
}

// A binding is a module like any other: its functions and structs are
// used by their names in the module, the structs have methods, and the
// library still gets the names it knows.
func foreignModules(library string) sources {
	return sources{
		"main.lum": `
			import probe;
			import probe.{Color, Vec2};
			import std.arrays;

			// A struct of C with methods of the program
			impl Vec2 {
				fn plus(other: Vec2) -> Vec2 { return probe.probe_vec_add(self, other); }
			}

			fn main() {
				let colors = [Color { r: 1, g: 2, b: 3 }, probe.opaque(9, 8, 7), probe.probe_color_unpack(16909060)];
				println(arrays.map(colors, c -> probe.probe_color_pack(c)));
				let at = Vec2 { x: 1.0, y: 1.0 }.plus(Vec2 { x: 0.5, y: 2.0 }).plus(probe.unit());
				println("${at.x} ${at.y}");
				println(probe.sum([1, 2, 3, 4]));
				println(colors[2] is Color);
			}
		`,
		"probe.lum": `
			import std.arrays;
			import std.ffi;

			pub extern struct Color { r: UInt8; g: UInt8; b: UInt8; a: UInt8 = 255; }
			pub extern struct Vec2 { x: Float32; y: Float32; }

			pub extern "` + library + `" fn probe_color_pack(c: Color) -> UInt32;
			pub extern "` + library + `" fn probe_color_unpack(packed: UInt32) -> Color;
			pub extern "` + library + `" fn probe_vec_add(a: Vec2, b: Vec2) -> Vec2;
			extern "` + library + `" fn probe_sum(values: Pointer, count: Int32) -> Int64;
			// Declared, never called: the chunk does not hear of it
			pub extern "` + library + `" fn probe_never_called(n: Int32) -> Int32;

			pub fn opaque(r: Int, g: Int, b: Int) -> Color { return Color { r: r, g: g, b: b }; }
			pub fn unit() -> Vec2 { return Vec2 { x: 1.0, y: 1.0 }; }

			// What a binding is for: a function of the program around one of C
			pub fn sum(values: [Int]) -> Int {
				let address = ffi.int32s(values);
				defer ffi.free(address);
				return probe_sum(address, arrays.len(values));
			}
		`,
	}
}

func TestForeignModules(t *testing.T) {
	library := ffitest.Library(t)
	expectModulesOutput(t, foreignModules(library), "[16909311, 151521279, 16909060]\n2.5 4\n10\ntrue\n")
}

// The chunk names a function of C by all there is to know of it, and only
// the functions the program can reach.
func TestForeignFunctionsInTheChunk(t *testing.T) {
	library := ffitest.Library(t)
	result, errs := compileModules(foreignModules(library))
	if errs.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", errs.Diagnostics)
	}

	described := map[string]*vm.Foreign{}
	for _, name := range result.Chunk.ExternNames {
		if !vm.IsForeign(name) {
			continue
		}
		foreign, err := vm.ParseForeign(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		described[foreign.Symbol] = foreign
	}
	for _, symbol := range []string{"probe_color_pack", "probe_color_unpack", "probe_vec_add", "probe_sum"} {
		if described[symbol] == nil {
			t.Errorf("expected the chunk to describe %s", symbol)
		}
	}
	if described["probe_never_called"] != nil {
		t.Errorf("expected nothing of a function that is never called")
	}
	if t.Failed() {
		t.FailNow()
	}

	pack := described["probe_color_pack"]
	if pack.Name != "probe.probe_color_pack" || pack.Library != library || pack.Result.Kind != vm.CUInt32 {
		t.Errorf("unexpected description: %+v", pack)
	}
	color := pack.Params[0]
	if color.Name != "c" || color.Type.Kind != vm.CStruct || color.Type.Name != "probe.Color" || len(color.Type.Fields) != 4 {
		t.Fatalf("unexpected parameter: %+v", color)
	}
	for i, name := range []string{"r", "g", "b", "a"} {
		if field := color.Type.Fields[i]; field.Name != name || field.Type.Kind != vm.CUInt8 {
			t.Errorf("field %d: expected %s as a UInt8, got %+v", i, name, field)
		}
	}
	if sum := described["probe_sum"]; sum.Params[0].Type.Kind != vm.CPointer || sum.Params[1].Type.Kind != vm.CInt32 || sum.Result.Kind != vm.CInt64 {
		t.Errorf("unexpected description: %+v", sum)
	}
}

// A program compiled to a file calls the same functions when the file is
// run: nothing of them is outside the chunk.
func TestForeignFunctionsSurviveTheBytecodeFile(t *testing.T) {
	library := ffitest.Library(t)
	result, errs := compileModules(foreignModules(library))
	if errs.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", errs.Diagnostics)
	}
	for name, chunk := range map[string]*vm.Chunk{"as compiled": result.Chunk, "without debug information": result.Chunk.Stripped()} {
		data, err := vm.SerializeChunk(chunk)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		read, err := vm.DeserializeChunk(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var out bytes.Buffer
		machine := vm.NewVM()
		machine.SetOutput(&out)
		if _, err := machine.Exec(read); err != nil {
			t.Fatalf("%s: runtime error: %v", name, err)
		}
		if want := "[16909311, 151521279, 16909060]\n2.5 4\n10\ntrue\n"; out.String() != want {
			t.Errorf("%s: unexpected output\n--- want ---\n%s--- got ---\n%s", name, want, out.String())
		}
	}
}

// The declarations of raylib that come with lumen, against the raylib of
// the machine: every function is in the library, and can be called with
// the types it is declared with.
func TestRaylibDeclarationsAreInTheLibrary(t *testing.T) {
	ffitest.Installed(t, "raylib")
	source, err := os.ReadFile("../../examples/raylib/raylib.lum")
	if err != nil {
		t.Fatal(err)
	}
	errs := errors.NewHandler()
	result := Compile(string(source)+"\nfn main() {}\n", "raylib.lum", errs)
	if errs.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", errs.Diagnostics[0].String())
	}
	described := foreignFunctions(result.AST, errs, nil)
	if len(described) < 500 {
		t.Fatalf("expected the functions of raylib, got %d", len(described))
	}
	machine := vm.NewVM()
	for name, extern := range described {
		if err := machine.BindForeign(extern); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

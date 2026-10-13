package stdlib_test

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/ffitest"
	"github.com/dani-gouken/lumen/internal/sandbox"
	"github.com/dani-gouken/lumen/internal/vm"
)

// The functions of C libraries, seen from programs: every kind of value on
// its way to C and back, structs by value in every way the processor
// takes them, memory a function fills, and what is refused.
//
// They call a library made for them (internal/ffitest/testdata/probe.c),
// compiled when the tests run.

// probeDeclarations is what a program says of the library of the tests.
const probeDeclarations = `
extern struct Color { r: UInt8; g: UInt8; b: UInt8; a: UInt8 = 255; }
extern struct Vec2 { x: Float32; y: Float32; }
extern struct Point { x: Float64; y: Float64; }
extern struct Rect { x: Float32; y: Float32; width: Float32; height: Float32; }
extern struct Camera { offset: Vec2; target: Vec2; rotation: Float32; zoom: Float32; }
extern struct Inner { weight: Float64; tag: UInt8; }
extern struct Padded { inner: Inner; flag: UInt8; count: Int32; }
extern struct Image { data: Pointer; width: Int32; height: Int32; format: Int32; }
extern struct Pair { ratio: Float32; count: Int32; }
extern struct Mixed {
    a: Int8; b: Int16; c: Int32; d: Int64;
    e: UInt8; f: UInt16; g: UInt32; h: UInt64;
    i: Float32; j: Float64; k: Bool;
}

extern "probe" fn probe_add(a: Int32, b: Int32) -> Int32;
extern "probe" fn probe_not(value: Bool) -> Bool;
extern "probe" fn probe_scalars(a: Int8, b: Int16, c: Int32, d: Int64, e: UInt8, f: UInt16, g: UInt32, h: UInt64, i: Float32, j: Float64, k: Bool) -> Float64;
extern "probe" fn probe_i8(n: Int8) -> Int8;
extern "probe" fn probe_u8(n: UInt8) -> UInt8;
extern "probe" fn probe_i16(n: Int16) -> Int16;
extern "probe" fn probe_u16(n: UInt16) -> UInt16;
extern "probe" fn probe_u32(n: UInt32) -> UInt32;
extern "probe" fn probe_i64(n: Int64) -> Int64;
extern "probe" fn probe_half(n: Float32) -> Float32;
extern "probe" fn probe_ten(a: Int32, b: Int32, c: Int32, d: Int32, e: Int32, f: Int32, g: Int32, h: Int32, i: Int32, j: Int32) -> Int64;
extern "probe" fn probe_ten_floats(a: Float64, b: Float64, c: Float64, d: Float64, e: Float64, f: Float64, g: Float64, h: Float64, i: Float64, j: Float64) -> Float64;

extern "probe" fn probe_color_pack(c: Color) -> UInt32;
extern "probe" fn probe_color_unpack(packed: UInt32) -> Color;
extern "probe" fn probe_vec_add(a: Vec2, b: Vec2) -> Vec2;
extern "probe" fn probe_point_scale(p: Point, by: Float64) -> Point;
extern "probe" fn probe_rect_holds(r: Rect, p: Vec2) -> Bool;
extern "probe" fn probe_rect_grow(r: Rect, by: Float32) -> Rect;
extern "probe" fn probe_camera_zoom(c: Camera, by: Float32) -> Camera;
extern "probe" fn probe_camera_sum(c: Camera) -> Float32;
extern "probe" fn probe_padded_bump(p: Padded) -> Padded;
extern "probe" fn probe_padded_sum(p: Padded) -> Float64;
extern "probe" fn probe_pair_swap(p: Pair) -> Pair;
extern "probe" fn probe_pair_sum(p: Pair) -> Float32;
extern "probe" fn probe_mixed_bump(m: Mixed) -> Mixed;
extern "probe" fn probe_crowd(n: Int32, a: Color, b: Vec2, c: Rect, d: Float64, e: Color, f: Image, g: Pair, h: Vec2, i: Rect) -> Float64;
extern "probe" fn probe_image(width: Int32, height: Int32) -> Image;
extern "probe" fn probe_image_pixel(image: Image, at: Int32) -> Int32;

extern "probe" fn probe_greet(name: CString) -> CString;
extern "probe" fn probe_bytes(text: CString) -> Int32;
extern "probe" fn probe_nothing() -> CString;
extern "probe" fn probe_squares(out: Pointer, count: Int32);
extern "probe" fn probe_sum(values: Pointer, count: Int32) -> Int64;
extern "probe" fn probe_mean(values: Pointer, count: Int32) -> Float32;
extern "probe" fn probe_origin(out: Pointer, r: Rect);
extern "probe" fn probe_count();
extern "probe" fn probe_reset();
extern "probe" fn probe_counted() -> Int32;
extern "probe" fn probe_keep(text: Pointer);
extern "probe" fn probe_kept() -> CString;
extern "probe" fn probe_huge() -> Int64;
extern "probe" fn probe_huge_unsigned() -> UInt64;
`

// probeProgram is a program that can call the library of the tests.
func probeProgram(t *testing.T, src string) string {
	t.Helper()
	library := ffitest.Library(t)
	return strings.ReplaceAll(probeDeclarations, `"probe"`, `"`+library+`"`) + src
}

func expectProbe(t *testing.T, src string, want ...string) {
	t.Helper()
	out, err := runWith(t, 64<<20, probeProgram(t, src), nil)
	if err != nil {
		t.Fatalf("runtime error: %v\nafter printing:\n%s", err, out)
	}
	expectLines(t, out, want...)
}

// expectProbeFailure runs a program that has to fail, and returns what it
// failed with.
func expectProbeFailure(t *testing.T, src, want string) *vm.RuntimeError {
	t.Helper()
	out, err := runWith(t, 64<<20, probeProgram(t, src), nil)
	if err == nil {
		t.Fatalf("expected a failure with %q, the program printed:\n%s", want, out)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected a failure with %q, got: %v", want, err)
	}
	failure, _ := err.(*vm.RuntimeError)
	return failure
}

func TestForeignNumbers(t *testing.T) {
	expectProbe(t, `
		fn main() {
			println(probe_add(40, 2));
			println(probe_add(-2147483648, 2147483647));
			println(probe_not(true));
			println(probe_not(false));
			// 1 + 2 + 3 + 4 + 5 + 6 + 7 + 8 + 0.5 + 0.25 + 1000
			println(probe_scalars(1, 2, 3, 4, 5, 6, 7, 8, 0.5, 0.25, true));
			println(probe_scalars(-1, -2, -3, -4, 0, 0, 0, 0, 0.0, 0.0, false));

			// The ends of each size come back as they went
			println("${probe_i8(-128)} ${probe_i8(127)}");
			println("${probe_u8(0)} ${probe_u8(255)}");
			println("${probe_i16(-32768)} ${probe_i16(32767)}");
			println("${probe_u16(65535)}");
			println("${probe_u32(4294967295)}");
			println("${probe_i64(-140737488355328)} ${probe_i64(140737488355327)}");
			println(probe_half(5.0));

			// The last two of ten do not fit in registers
			println(probe_ten(1, 1, 1, 1, 1, 1, 1, 1, 1, 1));
			println(probe_ten(0, 0, 0, 0, 0, 0, 0, 0, 7, -3));
			println(probe_ten_floats(0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.5, 2.0, 0.25));
		}
	`,
		"42", "-1", "false", "true", "1036.75", "-10",
		"-128 127", "0 255", "-32768 32767", "65535", "4294967295",
		"-140737488355328 140737488355327", "2.5",
		"55", "33", "24.5")
}

func TestForeignStructs(t *testing.T) {
	expectProbe(t, `
		fn show(c: Color) -> String { return "${c.r},${c.g},${c.b},${c.a}"; }

		fn main() {
			// Four bytes: one word. 0x10203040 is 270544960
			println(probe_color_pack(Color { r: 16, g: 32, b: 48, a: 64 }));
			println(show(probe_color_unpack(270544960)));
			// The field left out has what the declaration says
			println(probe_color_pack(Color { r: 0, g: 0, b: 1 }));

			// Two floats, then two doubles: each in a register
			let v = probe_vec_add(Vec2 { x: 1.5, y: 2.0 }, Vec2 { x: 0.25, y: -4.0 });
			println("${v.x} ${v.y}");
			let p = probe_point_scale(Point { x: 1.5, y: -2.0 }, 4.0);
			println("${p.x} ${p.y}");

			// Four floats
			let r = Rect { x: 10.0, y: 20.0, width: 30.0, height: 40.0 };
			println(probe_rect_holds(r, Vec2 { x: 39.5, y: 20.0 }));
			println(probe_rect_holds(r, Vec2 { x: 40.0, y: 20.0 }));
			let grown = probe_rect_grow(r, 2.5);
			println("${grown.x} ${grown.y} ${grown.width} ${grown.height}");

			// Structs in a struct, 24 bytes: by its address
			let camera = Camera { offset: Vec2 { x: 1.0, y: 2.0 }, target: Vec2 { x: 3.0, y: 4.0 }, rotation: 5.0, zoom: 6.0 };
			println(probe_camera_sum(camera));
			let zoomed = probe_camera_zoom(camera, 0.5);
			println("${zoomed.offset.x} ${zoomed.offset.y} ${zoomed.target.x} ${zoomed.target.y} ${zoomed.rotation} ${zoomed.zoom}");
			// What was given is what it was: C had a copy
			println("${camera.offset.y} ${camera.target.x} ${camera.zoom}");

			// The end of the struct inside is rounded up: flag is at 16
			let padded = Padded { inner: Inner { weight: 1.25, tag: 3 }, flag: 4, count: 50 };
			println(probe_padded_sum(padded));
			let bumped = probe_padded_bump(padded);
			println("${bumped.inner.weight} ${bumped.inner.tag} ${bumped.flag} ${bumped.count}");

			// A float and a whole number in one word
			println(probe_pair_sum(Pair { ratio: 0.5, count: 7 }));
			let swapped = probe_pair_swap(Pair { ratio: 9.0, count: 4 });
			println("${swapped.ratio} ${swapped.count}");

			// One of each
			let m = probe_mixed_bump(Mixed { a: -5, b: -300, c: 70000, d: 5000000000, e: 200, f: 60000, g: 4000000000, h: 9, i: 0.5, j: 0.125, k: false });
			println("${m.a} ${m.b} ${m.c} ${m.d} ${m.e} ${m.f} ${m.g} ${m.h} ${m.i} ${m.j} ${m.k}");

			// What comes back is a value of the struct like any other
			println(grown is Rect);
			println(zoomed.target is Vec2);
			grown.x = 99.0;
			println(probe_rect_grow(grown, 1.0).x);
		}
	`,
		"270544960", "16,32,48,64", "511",
		"1.75 -2", "6 -8",
		"true", "false", "7.5 17.5 35 45",
		"21", "1 1 4 4 5 3", "2 3 6",
		"58.25", "1.75 4 6 100",
		"7.5", "4 9",
		"-4 -299 70001 5000000001 201 60001 4000000001 10 1.5 1.125 true",
		"true", "true", "98")
}

// Structs between numbers, more of them than there are registers: the
// call has to go the slow way, and still be right.
func TestForeignStructsBeyondTheRegisters(t *testing.T) {
	expectProbe(t, `
		fn main() {
			let image = probe_image(3, 4);
			println("${image.width} ${image.height} ${image.format} ${image.data != 0}");
			println(probe_image_pixel(image, 0));
			println(probe_image_pixel(image, 15));

			// 1 + 2 + 0.5 + 0.25 + 8 + 16 + 32 + 3 + 4 + 5 + 0.5 + 1 + 2 + 64 + 128 + 20
			println(probe_crowd(1,
				Color { r: 2, g: 0, b: 0, a: 0 },
				Vec2 { x: 0.5, y: 0.25 },
				Rect { x: 0.0, y: 0.0, width: 8.0, height: 0.0 },
				16.0,
				Color { r: 0, g: 0, b: 0, a: 32 },
				image,
				Pair { ratio: 0.5, count: 5 },
				Vec2 { x: 1.0, y: 2.0 },
				Rect { x: 64.0, y: 0.0, width: 0.0, height: 128.0 }));
		}
	`, "3 4 7 true", "10", "160", "287.25")
}

func TestForeignTexts(t *testing.T) {
	expectProbe(t, `
		import std.ffi;

		fn main() {
			println(probe_greet("lumen"));
			// Counted in bytes by C: é is two, 日 is three
			println(probe_bytes("été"));
			println(probe_bytes("日本"));
			println(probe_greet("日本 ✓"));
			println(probe_bytes(""));
			// No text at all comes back as an empty one
			println("[${probe_nothing()}]");

			// A text C keeps has to stay where it is
			let kept = ffi.cstring("kept by C");
			probe_keep(kept);
			println(probe_greet("someone else"));
			println(probe_kept());
			println(ffi.text(kept));
			ffi.free(kept);
		}
	`, "hello, lumen!", "5", "6", "hello, 日本 ✓!", "0", "[]",
		"hello, someone else!", "kept by C", "kept by C")
}

func TestForeignMemory(t *testing.T) {
	expectProbe(t, `
		import std.arrays;
		import std.ffi;

		fn main() {
			// Memory C fills
			let squares = ffi.alloc(6 * 4);
			defer ffi.free(squares);
			println(ffi.readInt32s(squares, 6));
			probe_squares(squares, 6);
			println(ffi.readInt32s(squares, 6));
			println(probe_sum(squares, 6));

			// Numbers of the program, given to C
			let values = ffi.int32s([10, -20, 30, 2000000000]);
			println(probe_sum(values, 4));
			ffi.free(values);
			let floats = ffi.float32s([1.0, 2.0, 4.5]);
			println(probe_mean(floats, 3));
			println(ffi.readFloat32s(floats, 3));
			ffi.free(floats);

			// A struct filled through its address
			let origin = ffi.alloc(8);
			probe_origin(origin, Rect { x: 10.0, y: 20.0, width: 5.0, height: 9.0 });
			println("${ffi.readFloat32(origin, 0)} ${ffi.readFloat32(origin, 4)}");
			ffi.free(origin);

			// Every size, written and read back
			let m = ffi.alloc(64);
			ffi.writeInt8(m, 0, -7);
			ffi.writeUInt8(m, 1, 250);
			ffi.writeInt16(m, 2, -3000);
			ffi.writeUInt16(m, 4, 65000);
			ffi.writeInt32(m, 8, -100000);
			ffi.writeUInt32(m, 12, 4000000000);
			ffi.writeInt64(m, 16, -5000000000);
			ffi.writeFloat32(m, 24, 0.75);
			ffi.writeFloat64(m, 32, 0.1);
			ffi.writePointer(m, 40, m);
			println("${ffi.readInt8(m, 0)} ${ffi.readUInt8(m, 1)} ${ffi.readInt16(m, 2)} ${ffi.readUInt16(m, 4)}");
			println("${ffi.readInt32(m, 8)} ${ffi.readUInt32(m, 12)} ${ffi.readInt64(m, 16)}");
			println("${ffi.readFloat32(m, 24)} ${ffi.readFloat64(m, 32)} ${ffi.readPointer(m, 40) == m}");
			// The same bytes, read as something else: 250 is -6 with a sign
			println(ffi.readInt8(m, 1));
			// What was not written is 0
			println(ffi.readInt64(m, 56));
			ffi.free(m);
			ffi.free(0);

			// Functions that take and return nothing
			probe_reset();
			for i in 0..5 { probe_count(); }
			println(probe_counted());
		}
	`,
		"[0, 0, 0, 0, 0, 0]", "[0, 1, 4, 9, 16, 25]", "55",
		"2000000020", "2.5", "[1, 2, 4.5]",
		"12.5 24.5",
		"-7 250 -3000 65000", "-100000 4000000000 -5000000000", "0.75 0.1 true",
		"-6", "0", "5")
}

// What comes back from C is made on the heap of the program: structs and
// texts by the hundred thousand in a small heap have to be given back.
func TestForeignResultsAreCollected(t *testing.T) {
	out, err := runWith(t, 4<<20, probeProgram(t, `
		fn main() {
			let total = 0.0;
			let length = 0;
			let camera = Camera { offset: Vec2 { x: 0.0, y: 0.0 }, target: Vec2 { x: 0.0, y: 0.0 }, rotation: 0.0, zoom: 1.0 };
			let i = 0;
			while i < 100000 {
				camera = probe_camera_zoom(camera, 1.0);
				total += probe_rect_grow(Rect { x: 0.0, y: 0.0, width: 1.0, height: 1.0 }, 1.0).width;
				length += strings.len(probe_greet("again"));
				i++;
			}
			println(total);
			println(length);
			println("${camera.target.x} ${camera.offset.y} ${camera.zoom}");
		}
	`)+"\nimport std.strings;\n", nil)
	if err != nil {
		t.Fatalf("runtime error: %v\nafter printing:\n%s", err, out)
	}
	expectLines(t, out, "300000", "1300000", "100000 -100000 1")
}

func TestForeignRefusals(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"too large for Int8", `probe_i8(128);`, "probe_i8: 'n' is 128, which does not fit Int8"},
		{"too small for Int8", `probe_i8(-129);`, "'n' is -129, which does not fit Int8"},
		{"negative for UInt8", `probe_u8(-1);`, "'n' is -1, which does not fit UInt8"},
		{"too large for UInt16", `probe_u16(65536);`, "does not fit UInt16"},
		{"too large for Int32", `probe_add(2147483648, 0);`, "probe_add: 'a' is 2147483648, which does not fit Int32"},
		{"too large for UInt32", `probe_u32(4294967296);`, "does not fit UInt32"},
		{"a field too large", `probe_color_pack(Color { r: 1, g: 256, b: 3 });`, "probe_color_pack: 'c'.g is 256, which does not fit UInt8"},
		{"a field of a struct inside", `probe_padded_sum(Padded { inner: Inner { weight: 1.0, tag: 300 }, flag: 0, count: 0 });`, "'p'.inner.tag is 300, which does not fit UInt8"},
		{"a field on the slow way", `let image = probe_image(1, 1); image.width = 3000000000; probe_crowd(1, Color { r: 1, g: 1, b: 1 }, Vec2 { x: 0.0, y: 0.0 }, Rect { x: 0.0, y: 0.0, width: 0.0, height: 0.0 }, 0.0, Color { r: 1, g: 1, b: 1 }, image, Pair { ratio: 0.0, count: 0 }, Vec2 { x: 0.0, y: 0.0 }, Rect { x: 0.0, y: 0.0, width: 0.0, height: 0.0 });`, "'f'.width is 3000000000, which does not fit Int32"},
		{"a negative address", `probe_sum(-8, 1);`, "'values' is -8, which does not fit Pointer"},
		{"a text with a 0 in it", `probe_bytes("a\0b");`, "probe_bytes: 'text' holds the character 0"},
		{"a result too large for an Int", `probe_huge();`, "probe_huge: 9223372036854775807 does not fit an Int"},
		{"an unsigned result too large", `probe_huge_unsigned();`, "probe_huge_unsigned: 18446744073709551615 does not fit an Int"},
		{"reading at 0", `ffi.readInt32(0, 0);`, "std.ffi.readInt32: the address is 0"},
		{"writing at 0", `ffi.writeFloat64(0, 8, 1.0);`, "std.ffi.writeFloat64: the address is 0"},
		{"writing what does not fit", `let m = ffi.alloc(8); ffi.writeUInt8(m, 0, 256);`, "std.ffi.writeUInt8: 256 does not fit UInt8"},
		{"a text at 0", `ffi.text(0);`, "std.ffi.text: 0 is not the address of a text"},
		{"too much memory", `ffi.alloc(-1);`, "std.ffi.alloc: -1 bytes cannot be asked for"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			failure := expectProbeFailure(t, "import std.ffi;\nfn main() { "+c.body+" }", c.want)
			if failure == nil || failure.Kind != vm.KindLibrary {
				t.Errorf("expected a failure of the library, got %#v", failure)
			}
		})
	}
}

// A failure of a function of C is one a program survives like the others,
// and the function works again afterwards.
func TestForeignFailuresAreSurvived(t *testing.T) {
	expectProbe(t, `
		import std.result;
		import std.result.{Result};

		fn main() {
			match result.attempt(() -> probe_i8(1000)) {
				case Result.Ok(n): println("got ${n}");
				case Result.Err(failure): println(failure.message);
			}
			println(probe_i8(100));
		}
	`, "probe_i8: 'n' is 1000, which does not fit Int8", "100")
}

func TestForeignLibrariesAndFunctionsNotFound(t *testing.T) {
	library := ffitest.Library(t)

	out, err := runWith(t, 16<<20, `
		extern "no-such-library-anywhere" fn nothing() -> Int32;
		fn main() { println("before"); nothing(); }
	`, nil)
	if err == nil || !strings.Contains(err.Error(), `nothing: the library "no-such-library-anywhere" was not found`) ||
		!strings.Contains(err.Error(), vm.LibraryPathVariable) {
		t.Errorf("a library that does not exist: %v", err)
	}
	// Only looked for when the function is used
	if out != "before\n" {
		t.Errorf("printed %q before failing", out)
	}

	_, err = runWith(t, 16<<20, `
		extern "`+library+`" fn probe_no_such_function() -> Int32;
		fn main() { probe_no_such_function(); }
	`, nil)
	if err == nil || !strings.Contains(err.Error(), "has no function 'probe_no_such_function'") {
		t.Errorf("a function that does not exist: %v", err)
	}

	// A function that is declared and never called asks for nothing
	out, err = runWith(t, 16<<20, `
		extern "no-such-library-anywhere" fn nothing() -> Int32;
		fn main() { println("fine"); }
	`, nil)
	if err != nil || out != "fine\n" {
		t.Errorf("a function never called: %q, %v", out, err)
	}
}

// The C library itself, and the mathematical one, are there by name on
// every system.
func TestForeignSystemLibraries(t *testing.T) {
	if !vm.ForeignSupported {
		t.Skip("this build of lumen cannot call the functions of C libraries")
	}
	out, err := runWith(t, 16<<20, `
		extern "c" fn abs(n: Int32) -> Int32;
		extern "c" fn strlen(text: CString) -> UInt64;
		extern "c" fn toupper(c: Int32) -> Int32;
		extern "c" fn atoi(text: CString) -> Int32;
		extern "c" fn getenv(name: CString) -> CString;
		extern "m" fn pow(x: Float64, y: Float64) -> Float64;
		extern "m" fn sqrtf(x: Float32) -> Float32;
		extern "m" fn hypot(x: Float64, y: Float64) -> Float64;

		fn main() {
			println(abs(-42));
			println(strlen("hello, C"));
			println(toupper(97));
			println(atoi("  1234xyz"));
			println("[${getenv("LUMEN_NO_SUCH_VARIABLE")}]");
			println(pow(2.0, 10.0));
			println(sqrtf(6.25));
			println(hypot(3.0, 4.0));
		}
	`, nil)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	expectLines(t, out, "42", "8", "65", "1234", "[]", "1024", "2.5", "5")
}

// A window belongs to the thread that made it, so the functions of C are
// main's to call: a task is told so.
func TestForeignFunctionsAreNotForTasks(t *testing.T) {
	expectProbe(t, `
		import std.task;
		import std.result.{Result};

		fn main() {
			println(probe_add(1, 2));
			let adding = task.start(() -> probe_add(3, 4));
			match adding.wait() {
				case Result.Ok(sum): println("a task got ${sum}");
				case Result.Err(failure): println(failure.message);
			}
			// main still can
			println(probe_add(5, 6));
		}
	`, "3", "probe_add: a function of a C library can only be called from main, not from a task", "11")
}

// A program that is not permitted everything is not permitted C: a
// function of a C library can do anything at all.
func TestForeignFunctionsAreNotForTheSandbox(t *testing.T) {
	library := ffitest.Library(t)
	for name, src := range map[string]string{
		"a function of a library": `
			extern "` + library + `" fn probe_add(a: Int32, b: Int32) -> Int32;
			fn main() { println(probe_add(1, 2)); }`,
		"the C library": `
			extern "c" fn system(command: CString) -> Int32;
			fn main() { println(system("true")); }`,
		"memory": `
			import std.ffi;
			fn main() { println(ffi.alloc(8) != 0); }`,
	} {
		t.Run(name, func(t *testing.T) {
			out, err := runWith(t, 16<<20, src, func(machine *vm.VM) { machine.PermitNatives(sandbox.Permitted) })
			if err == nil || !strings.Contains(err.Error(), "not permitted") {
				t.Errorf("expected to be refused, got %q, %v", out, err)
			}
		})
	}
}

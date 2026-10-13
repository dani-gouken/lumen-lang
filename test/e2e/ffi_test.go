package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/ffitest"
)

// The functions of C libraries, from the command line: a program run from
// its source and from its bytecode, libraries found by name, and the real
// libraries a machine has. No test opens a window.

// writeProgram writes the files of a program in a folder of its own.
func writeProgram(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func expectOutput(t *testing.T, res result, want ...string) {
	t.Helper()
	if res.exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}
	if got := strings.TrimSuffix(res.stdout, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("unexpected output\n--- want ---\n%s\n--- got ---\n%s", strings.Join(want, "\n"), got)
	}
}

const probeProgram = `
import std.ffi;

extern struct Vec2 { x: Float32; y: Float32; }
extern struct Camera { offset: Vec2; target: Vec2; rotation: Float32; zoom: Float32; }

extern "probe" fn probe_add(a: Int32, b: Int32) -> Int32;
extern "probe" fn probe_greet(name: CString) -> CString;
extern "probe" fn probe_camera_zoom(c: Camera, by: Float32) -> Camera;
extern "probe" fn probe_squares(out: Pointer, count: Int32);
extern "probe" fn probe_i8(n: Int8) -> Int8;

fn main() -> Int {
    println(probe_add(40, 2));
    println(probe_greet("the command line"));
    let camera = probe_camera_zoom(Camera { offset: Vec2 { x: 1.0, y: 2.0 }, target: Vec2 { x: 3.0, y: 4.0 }, rotation: 0.0, zoom: 2.0 }, 4.0);
    println("${camera.offset.y} ${camera.target.x} ${camera.zoom}");
    let squares = ffi.alloc(20);
    defer ffi.free(squares);
    probe_squares(squares, 5);
    println(ffi.readInt32s(squares, 5));
    return probe_i8(7);
}
`

var probeOutput = []string{"42", "hello, the command line!", "1 4 8", "[0, 1, 4, 9, 16]"}

// A library named in a program is found in the folders of
// LUMEN_LIBRARY_PATH, from the source and from the bytecode alike.
func TestForeignLibraryByName(t *testing.T) {
	library := ffitest.Library(t)
	dir := writeProgram(t, map[string]string{"main.lum": probeProgram})
	t.Setenv("LUMEN_LIBRARY_PATH", "/no/such/folder"+string(os.PathListSeparator)+filepath.Dir(library))

	res := runLumenIn(t, dir, "", "run", "main.lum")
	if res.exit != 7 {
		t.Errorf("expected what main returned, 7, as exit code, got %d\n%s", res.exit, res.stderr)
	}
	res.exit = 0
	expectOutput(t, res, probeOutput...)

	for _, flags := range [][]string{nil, {"--strip"}} {
		build := append(append([]string{"build"}, flags...), "main.lum", "main.lc")
		if res := runLumenIn(t, dir, "", build...); res.exit != 0 {
			t.Fatalf("lumen %v: exit %d\n%s", build, res.exit, res.stderr)
		}
		res := runLumenIn(t, dir, "", "run", "main.lc")
		if res.exit != 7 {
			t.Errorf("%v: expected exit code 7, got %d\n%s", flags, res.exit, res.stderr)
		}
		res.exit = 0
		expectOutput(t, res, probeOutput...)
	}
}

// A library that is nowhere is said so, with the place of the call, when
// the program gets there and not before.
func TestForeignLibraryNotFound(t *testing.T) {
	ffitest.Library(t)
	dir := writeProgram(t, map[string]string{"main.lum": probeProgram})
	t.Setenv("LUMEN_LIBRARY_PATH", "")

	res := runLumenIn(t, dir, "", "run", "main.lum")
	if res.exit == 0 || res.stdout != "" {
		t.Errorf("expected a failure before anything is printed, got exit %d and %q", res.exit, res.stdout)
	}
	for _, want := range []string{`probe_add: the library "probe" was not found`, "LUMEN_LIBRARY_PATH", "main.lum:14"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("expected %q in:\n%s", want, res.stderr)
		}
	}

	// It compiles all the same: the library is only needed to run
	if res := runLumenIn(t, dir, "", "build", "main.lum", "main.lc"); res.exit != 0 {
		t.Errorf("build: exit %d\n%s", res.exit, res.stderr)
	}
}

// What a declaration cannot say is refused when the program is compiled.
func TestForeignDeclarationRefused(t *testing.T) {
	dir := writeProgram(t, map[string]string{"main.lum": `
struct Point { x: Float; y: Float; }
extern "c" fn abs(n: Int) -> Int32;
extern "c" fn length(p: Point) -> Float64;
fn main() { println(abs(-1)); }
`})
	res := runLumenIn(t, dir, "", "run", "main.lum")
	if res.exit == 0 {
		t.Fatalf("expected to be refused, printed %q", res.stdout)
	}
	for _, want := range []string{"E7003", "'Int' is not a type of C", "Int32 for an int", "'Point' is not a type of C", "extern struct Point", "main.lum:3", "main.lum:4"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("expected %q in:\n%s", want, res.stderr)
		}
	}
}

// raylib, through the declarations that come with lumen: the functions
// that compute, which need no window.
func TestRaylib(t *testing.T) {
	ffitest.Installed(t, "raylib")
	binding, err := os.ReadFile(filepath.Join(repoRoot, "examples/raylib/raylib.lum"))
	if err != nil {
		t.Fatal(err)
	}
	dir := writeProgram(t, map[string]string{
		"raylib.lum": string(binding),
		"main.lum": `
import raylib;
import raylib.{Camera2D, Color, Rectangle, Vector2};

fn show(c: Color) -> String { return "${c.r} ${c.g} ${c.b} ${c.a}"; }

fn main() {
    raylib.SetTraceLogLevel(raylib.LOG_NONE);

    // Colors: four bytes in a word, in and out
    println(raylib.ColorToInt(raylib.red()));
    println(show(raylib.GetColor(4278190335)));
    println(show(raylib.Fade(raylib.red(), 0.5)));
    println(show(raylib.ColorFromHSV(120.0, 1.0, 1.0)));
    let hsv = raylib.ColorToHSV(Color { r: 255, g: 0, b: 0, a: 255 });
    println("${hsv.x} ${hsv.y} ${hsv.z}");
    println(raylib.ColorIsEqual(raylib.blue(), Color { r: 0, g: 121, b: 241, a: 255 }));

    // Rectangles and points: floats in registers
    let a = Rectangle { x: 0.0, y: 0.0, width: 10.0, height: 10.0 };
    let b = Rectangle { x: 5.0, y: 5.0, width: 10.0, height: 10.0 };
    println(raylib.CheckCollisionRecs(a, b));
    let both = raylib.GetCollisionRec(a, b);
    println("${both.x} ${both.y} ${both.width} ${both.height}");
    println(raylib.CheckCollisionPointRec(Vector2 { x: 12.0, y: 3.0 }, a));
    println(raylib.CheckCollisionCircles(Vector2 { x: 0.0, y: 0.0 }, 5.0, Vector2 { x: 8.0, y: 0.0 }, 4.0));
    println(raylib.CheckCollisionCircleRec(Vector2 { x: 20.0, y: 20.0 }, 2.0, a));

    // A camera: structs in a struct, given by its address
    let camera = Camera2D { offset: Vector2 { x: 0.0, y: 0.0 }, target: Vector2 { x: 100.0, y: 100.0 }, rotation: 0.0, zoom: 1.0 };
    let world = raylib.GetScreenToWorld2D(Vector2 { x: 10.0, y: 20.0 }, camera);
    println("${world.x} ${world.y}");
    camera.zoom = 2.0;
    let screen = raylib.GetWorldToScreen2D(Vector2 { x: 110.0, y: 120.0 }, camera);
    println("${screen.x} ${screen.y}");

    // An image: an address among numbers, made and freed by raylib
    let image = raylib.GenImageColor(4, 2, raylib.skyBlue());
    println("${image.width}x${image.height} ${image.mipmaps} ${image.format == raylib.PIXELFORMAT_UNCOMPRESSED_R8G8B8A8}");
    println(show(raylib.GetImageColor(image, 3, 1)));
    println(raylib.IsImageValid(image));
    raylib.UnloadImage(image);

    // Texts, there and back
    println(raylib.TextToUpper("lumen"));
    println(raylib.TextSubtext("hello from lumen", 6, 4));
    println(raylib.TextLength("été"));
    println(raylib.TextIsEqual("a", "a"));

    println(raylib.KEY_SPACE + raylib.MOUSE_BUTTON_RIGHT + raylib.FLAG_VSYNC_HINT);
}
`})
	expectOutput(t, runLumenIn(t, dir, "", "run", "main.lum"),
		"-433506305", "255 0 0 255", "230 41 55 127", "0 255 0 255", "0 1 1", "true",
		"true", "5 5 5 5", "false", "true", "false",
		"110 120", "20 40",
		"4x2 1 true", "102 191 255 255", "true",
		"LUMEN", "from", "5", "true",
		"97")
}

// SDL: a function that fills what it is given the address of, which is
// how most of SDL talks.
func TestSDL2(t *testing.T) {
	library := ffitest.Installed(t, "SDL2")
	dir := writeProgram(t, map[string]string{"main.lum": `
import std.ffi;

extern "` + library + `" fn SDL_GetVersion(version: Pointer);
extern "` + library + `" fn SDL_GetRevision() -> CString;
extern "` + library + `" fn SDL_GetPlatform() -> CString;
extern "` + library + `" fn SDL_GetCPUCount() -> Int32;

fn main() {
    // An SDL_version: three bytes
    let version = ffi.alloc(3);
    defer ffi.free(version);
    SDL_GetVersion(version);
    println(ffi.readUInt8(version, 0));
    println(SDL_GetRevision() != "");
    println(SDL_GetPlatform() != "");
    println(SDL_GetCPUCount() > 0);
}
`})
	expectOutput(t, runLumenIn(t, dir, "", "run", "main.lum"), "2", "true", "true", "true")
}

func TestSDL3(t *testing.T) {
	library := ffitest.Installed(t, "SDL3")
	dir := writeProgram(t, map[string]string{"main.lum": `
extern "` + library + `" fn SDL_GetVersion() -> Int32;
extern "` + library + `" fn SDL_GetPlatform() -> CString;
extern "` + library + `" fn SDL_GetNumLogicalCPUCores() -> Int32;

fn main() {
    println(SDL_GetVersion() / 1000000);
    println(SDL_GetPlatform() != "");
    println(SDL_GetNumLogicalCPUCores() > 0);
}
`})
	expectOutput(t, runLumenIn(t, dir, "", "run", "main.lum"), "3", "true", "true")
}

// The examples that open a window are not run, but they compile: the
// library is only needed to run them.
func TestWindowedExamplesCompile(t *testing.T) {
	for name := range opensAWindow {
		bytecode := filepath.Join(t.TempDir(), name+".lc")
		if res := runLumen(t, "build", filepath.Join("examples", name), bytecode); res.exit != 0 || res.stderr != "" {
			t.Errorf("%s: exit %d\n%s", name, res.exit, res.stderr)
		}
	}
}

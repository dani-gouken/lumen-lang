# Functions of C Libraries

A program can call the functions of a C library: raylib, SDL, SQLite, the C library itself. Nothing is compiled and nothing is installed with lumen: the program says which library a function is in and what its C types are, and the library is found and opened when the program first calls it.

```lumen
extern struct Color { r: UInt8; g: UInt8; b: UInt8; a: UInt8 = 255; }

extern "raylib" fn InitWindow(width: Int32, height: Int32, title: CString);
extern "raylib" fn WindowShouldClose() -> Bool;
extern "raylib" fn BeginDrawing();
extern "raylib" fn ClearBackground(color: Color);
extern "raylib" fn DrawText(text: CString, x: Int32, y: Int32, size: Int32, color: Color);
extern "raylib" fn EndDrawing();
extern "raylib" fn CloseWindow();

fn main() {
    InitWindow(800, 450, "hello");
    while !WindowShouldClose() {
        BeginDrawing();
        ClearBackground(Color { r: 245, g: 245, b: 245 });
        DrawText("hello from Lumen", 190, 200, 20, Color { r: 80, g: 80, b: 80 });
        EndDrawing();
    }
    CloseWindow();
}
```

All of raylib is declared this way in [`examples/raylib/raylib.lum`](../examples/raylib/raylib.lum), with a game that uses it: `lumen run examples/raylib`.

It works on macOS and Linux, on arm64 and amd64. Elsewhere a program that declares such functions compiles, and fails when it calls one.

## Declaring a Function

```lumen
extern "library" fn name(parameter: CType, ...) -> CType;
```

The name is the one the library has for the function. In a module it is used like any other function of the module, `pub` or not: `raylib.InitWindow(...)`.

The types are those of C, which say how many bits go to the library. To the program each is one of its own types:

| In a declaration | In C | To the program |
|------------------|------|----------------|
| `Int8`, `Int16`, `Int32`, `Int64` | `signed char`, `short`, `int`, `long long` | `Int` |
| `UInt8`, `UInt16`, `UInt32`, `UInt64` | the same, `unsigned` | `Int` |
| `Float32`, `Float64` | `float`, `double` | `Float` |
| `Bool` | `bool` | `Bool` |
| `CString` | `const char *` holding a text | `String` |
| `Pointer` | any other pointer | `Int`: an address |
| a struct declared with `extern struct` | that struct, by value | that struct |
| no result | `void` | nothing |

So `InitWindow(800, 450, "hello")` is checked like any call: two `Int` and a `String`. These names mean something only in a declaration made for a C library; `Int` and `Float` are refused there (`E7003`), because C has several of each and the wrong size reads the wrong bits.

A whole number that does not fit the C type it is given to is a failure, not a number cut short: `probe_i8: 'n' is 128, which does not fit Int8`. So is a result that does not fit an `Int`, which has 48 bits: every address does, an `Int64` or `UInt64` larger than that does not.

A `CString` is copied on its way in and on its way out. The copy given to C lasts for the call: a function that keeps the text needs [`ffi.cstring`](#memory). A `CString` that comes back as no text at all (`NULL`) is `""`.

## Structs

```lumen
extern struct Vector2 { x: Float32; y: Float32; }
extern struct Camera2D { offset: Vector2; target: Vector2; rotation: Float32; zoom: Float32; }
```

The fields are those of the C struct, in its order, with their C types: other structs of C, or anything of the table above but `CString`. To the program it is a struct like any other: made with a literal, read and changed by its fields, given methods with `impl`, tested with `is`. A field can say what it holds when a literal leaves it out (`a: UInt8 = 255`).

A function takes and returns it by value, as C does: C gets a copy, laid out as the C compiler lays it out, and what comes back is a new value of the program. An array inside a C struct (`float params[4]`) is declared as that many fields in a row (`params0` to `params3`): the memory is the same.

A function that wants the *address* of a struct takes a `Pointer`: see below.

## Memory

Some functions fill memory they are given the address of, or read numbers in a row, or keep a text. [`std.ffi`](stdlib.md#stdffi) is for those: memory outside the program, read and written by address.

```lumen
import std.ffi;

extern "SDL2" fn SDL_PollEvent(event: Pointer) -> Int32;

let event = ffi.alloc(56);              // An SDL_Event
defer ffi.free(event);
while SDL_PollEvent(event) != 0 {
    if ffi.readUInt32(event, 0) == 256 { running = false; }    // SDL_QUIT
}
```

`ffi.int32s([1, 2, 3])` and `ffi.float32s(...)` copy an array where C can read it; `ffi.readInt32s(address, count)` reads one back. `ffi.cstring(text)` makes a text that stays, `ffi.text(address)` reads one. What `alloc`, `cstring` and the two copies return is the program's to `free`.

An address is a number, and nothing checks one. Reading where there is nothing ends lumen at once, as it would a C program: this module, and a C function given a wrong address, are the only ways a Lumen program can do that.

## Finding the Library

`extern "raylib"` is the file `libraylib.dylib` on macOS and `libraylib.so` on Linux. It is looked for:

1.  where the system looks by itself;
2.  in the folders of the environment variable `LUMEN_LIBRARY_PATH` (separated by `:`);
3.  in `/opt/homebrew/lib`, `/usr/local/lib` and the other usual places.

A name with a `/` in it is that file and no other: `extern "./build/libgame.so"`. A name that says what kind of file it is is taken as written: `extern "libfoo.so.3"`. `"c"` and `"m"` are the C library and its mathematical functions, wherever the system keeps them.

A library is only looked for when one of its functions is called. A program that never gets there never needs it; one that does and does not find it fails there, with the place of the call:

```
[ERROR] R0007: InitWindow: the library "raylib" was not found: there is no libraylib.dylib where libraries are. Give its path, or list its folder in LUMEN_LIBRARY_PATH
  --> main.lum:12:5 in function 'main'
```

A library that is there and cannot be opened (it is for another processor, or needs a library that is missing) fails with what the system says of it.

A program compiled to bytecode (`lumen compile`) carries its declarations with it, and finds the libraries the same way where it is run.

## What Is Not Checked

The declaration is all lumen knows of a function. If it says `Int32` where the library has a `double`, or leaves a field out of a struct, the call goes through and the library reads the wrong thing: a wrong result at best. Take declarations from the header of the library, or write them with a program as [`examples/raylib/generate.py`](../examples/raylib/generate.py) does. That one also compares the size of every struct with the one the C compiler gives it.

## Limits

*   **Only `main` calls C.** A window belongs to the thread that made it, and only the program's first task always runs on the same one. A function of a C library called from a [task](tasks.md) fails, as a failure the task survives.
*   **No functions given to C.** A C function that takes a function to call back (`qsort`, an audio callback) cannot be declared.
*   **No functions that take any number of arguments** (`printf`).
*   **A program that is not permitted everything is not permitted C.** The [sandbox](testing.md) refuses every function of a C library and all of `std.ffi`: a C function can do anything at all.
*   **Speed.** A call costs about half a microsecond, ten times a call between functions of the program: thousands of calls for each picture are fine, a million are not. A function that returns a struct costs about twice that.

## How It Is Done

Nothing in the bytecode is particular to C. A chunk names its extern functions; a function of a C library is named by everything there is to know of it: the library, the name, the C type of each parameter and of each field of the structs. The virtual machine reads that the first time the function is used, opens the library, and calls through [purego](https://github.com/ebitengine/purego), which puts the arguments where the C calling convention of the processor wants them, without a C compiler having been near lumen.

On arm64 a struct argument is written once as C lays it out and given as what it comes to there: up to four floats in registers, one or two words, or its address. `lumen` stays on the thread it started on, which is the one macOS wants every call about a window made from.

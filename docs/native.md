# Native executables (`lumen native`)

`lumen native` makes an executable of a program: a file that runs on its own, without `lumen`, with the program's functions compiled to machine code.

```bash
lumen native app.lum             # ./app
lumen native app.lum -o bin/app
lumen native                     # in a project: the executable has the project's name
lumen native --strip app.lum     # smaller; a failure names the instruction, not the line
```

The executable does what `lumen run` of the source does: the same output, the same failures with the same messages and stack traces, the same exit status. What follows it on the command line is the program's (`std.os.args`), all of it. The limits are those `lumen run` would use, set when it is made: `--heap-mb` and `--max-depth` on the command line of `lumen native`, or `heap_mb` and `max_depth` of the project.

The compiler's warnings are said when the executable is made. A program that does not compile makes no executable.

## How it works

A program is compiled as for `lumen run`, to the bytecode `lumen compile` writes. Then each function of it is written as a Go function: the instructions one after the other, a jump as a `goto`, a sum of two Ints as a sum, a call of a function of the program as a call of its Go function. Constants are written into the code, with a way through for the type they have.

What the instruction loop does besides stays as it is. Every call goes in and out of a call frame as it would, and the registers are on the stack of the virtual machine: the collector, [tasks](tasks.md), stack traces, `defer` and `attempt`, and the limits work as they do for bytecode. The executable is the virtual machine with the program's bytecode in it, whose functions run as Go instead of in the loop.

The Go of the functions and a `main` are built with the source of Lumen's runtime, which `lumen` carries: it is unpacked once into the cache (`LUMEN_CACHE`, or `~/.cache/lumen`), and built there with `go build`. Nothing is left behind but the executable.

## Go

Building needs the `go` command. The one on the `PATH` is used if there is one. Otherwise `lumen native` downloads the version of Go `lumen` itself was built with from go.dev, the first time, checks it against the checksum go.dev gives, and keeps it in the cache under `toolchain/`. It says so when it does:

```
Downloading Go 1.26.1 to build with (once; it is kept in ~/.cache/lumen/toolchain)
```

The first build also fetches the one Go module the runtime needs, for calling C libraries ([ffi.md](ffi.md)), through the Go module proxy.

`GOOS` and `GOARCH` are passed on to `go build`: `GOOS=linux GOARCH=amd64 lumen native app.lum` makes an executable for Linux.

## How much faster

About twice as fast as `lumen run` on loops of numbers and on calls (`fib(35)`: 0.47 s against 0.86 s; a loop of 100 million sums: 0.92 s against 1.94 s). Less on programs that spend their time in the library, making texts and dictionaries, which is Go already. Building takes a second or two.

## Tests

`test/native` compiles every example, every program of `test/e2e/cases` and of the benchmarks, and the Lox interpreter, into one executable, runs each the two ways and compares what they print, how they fail and how they end. It also covers the command: names, `-o`, a project's name and limits, `--strip`, `std.compiler`, and what is refused.

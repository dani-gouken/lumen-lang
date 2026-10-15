<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/lum-wordmark-white.svg">
    <img src="assets/lum-wordmark.svg" height="96" alt="lum">
  </picture>
</p>

# Lumen Programming Language

Lumen is a statically typed language that is small enough to learn in an afternoon and complete enough to write real programs: a web service, a build tool, its own language server. This repository holds all of it: the compiler, the virtual machine, the standard library, and the editor support.

```rust
import std.http;
import std.task;

enum Health { Up(Int), Down(String) }

fn check(url: String) -> Health {
    match http.get(url) {
        case Result.Ok(response): { return Health.Up(response.status); }
        case Result.Err(error): { return Health.Down(error.describe()); }
    }
}

fn main() -> Int {
    let sites = ["https://example.com", "https://example.org", "https://example.net"];

    // One task per site: all three are asked at the same moment
    let results = task.map(sites, check).expect("the checks");

    for i, health in results {
        match health {
            case Health.Up(status): { println(sites[i], "answered", status); }
            case Health.Down(reason): { println(sites[i], "is down:", reason); }
        }
    }
    return 0;
}
```

```bash
curl -fsSL https://raw.githubusercontent.com/dani-gouken/lumen-lang/main/install.sh | sh
lumen run program.lum
```

The script installs the binary of the latest release in `~/.local/bin`, after checking it against its checksum. From the source: `make build`, then `./lumen`.

## What you get

### A language that catches mistakes before the program runs

- **Static types that stay out of the way.** Types are worked out where they can be: `let total = 0;`, `names.map(n -> n.len())`.
- **No null surprises.** A value that may be missing has a type that says so (`String?`), and the compiler makes you look before you use it. `?.`, `??` and `?` keep that short.
- **Enums that hold values, and `match` checked for completeness.** Forget a case and the program does not compile. A `match` gives a value where one is wanted, and a case can take several patterns, a range or a condition: `case Shape.Circle(r) if r > 10.0:`.
- **Failures as values.** A function that can fail returns a `Result`; `?` passes a failure on. A bug that stops the program can still be survived with `attempt`, where it matters, and `defer` cleans up whichever way a block is left.
- **Generics and traits.** `fn largest<T: Ordered>(items: [T])`, traits with type parameters and default methods, and operators, `for` loops and brackets on your own types.
- **Closures, tuples, ranges (`for i in 0..10`), string interpolation, modules** with `import` and `pub`, and an optional project file.

A tour of all of it: [docs/guide.md](docs/guide.md).

### Doing several things at once

Tasks run functions at the same time, and channels carry values between them. There are no locks and no `async` to spread through the code: one task runs at a time and gives way only where it waits, so two tasks never see a value half changed.

```rust
let lines = task.channel<String>(16);
task.run(fn() {
    for name in files { lines.send(io.readFile(name).orElse("")); }
    lines.close();
});
lines.each(fn(text: String) { println(text.len()); });
```

- A task that fails does not take the program down: whoever waits for it gets the failure as a `Result`.
- Tasks that all wait for each other are reported as a deadlock, with the line, instead of hanging.
- The HTTP server answers each request in a task of its own.

More in [docs/tasks.md](docs/tasks.md).

### A standard library with the everyday things in it

Built into the `lumen` binary, nothing to install: text, arrays, lists, dictionaries and sets, files and folders, dates, JSON, CSV, regular expressions, encodings and hashes, an HTTP client and a server with routes, random numbers, program arguments, running other programs, a test module, tasks and channels, reflection on values, and the compiler itself as a module.

```rust
let app = http.router();
app.get("/hello/:name", fn(request: Request) -> Response {
    return http.text("hello, ${request.params.get("name") ?? "you"}");
});
http.serve(8080, app.handler()).expect("a free port");
```

All of it: [docs/stdlib.md](docs/stdlib.md).

### An editor that understands the program

`lumen lsp` is a language server, and it is written in Lumen. With the VS Code extension in [editors/vscode](editors/vscode), or any editor that speaks the protocol:

- **Mistakes underlined as you type**, with what usually fixes them
- **Hover**: how a name is declared and the comment above it, for variables, functions, methods, fields, types and modules
- **Go to definition**, into your own files and into the standard library
- **Suggestions**: the methods of whatever stands before the dot, the names of a module, the modules on an `import` line
- **An outline** of what a file declares, and colours for `.lum` files
- **Format Document**, and on save: the file written the way `lumen fmt` writes it

Setting it up: [docs/editor.md](docs/editor.md).

### Errors written for people

The same form when compiling and when running: what went wrong, where, what usually fixes it, and the calls that led there.

```text
[ERROR] E1001: undefined identifier 'cuont'
  --> main.lum:3:12
   |
 3 |     return cuont + 1;
   |            ^^^^^
   |
   = Suggestion: Did you mean 'count'?
```

### C libraries, without a C compiler

A function of a C library is declared in Lumen and found when the program runs: no bindings to compile, nothing to install with `lumen`.

```lumen
extern struct Color { r: UInt8; g: UInt8; b: UInt8; a: UInt8 = 255; }
extern "raylib" fn InitWindow(width: Int32, height: Int32, title: CString);
extern "raylib" fn ClearBackground(color: Color);
```

All of raylib is declared in [examples/raylib](examples/raylib), with a Breakout that runs at sixty pictures a second: `lumen run examples/raylib`. More in [docs/ffi.md](docs/ffi.md).

### Programs that look at programs

- **Reflection** ([`std.reflect`](docs/stdlib.md#stdreflect)): a value asked at run time for its type, its fields by name, the variant of its enum. Enough to write a serializer that was never told about your structs.
- **The compiler as a module** ([`std.compiler`](docs/stdlib.md#stdcompiler)): the mistakes of a program, what it declares, what a name is and where it comes from. The language server is 350 lines on top of it.

### Scripts that don't break

A file that starts with `#!/usr/bin/env lumen` runs on its own, and [`std.sh`](docs/stdlib.md#stdsh) runs the commands it is made of: what they write shows as it comes, and one that fails stops the script, saying which and why.

```rust
#!/usr/bin/env lumen
import std.sh;

fn main() -> Int {
    sh.run("composer install --no-interaction");
    let branch = sh.output("git rev-parse --abbrev-ref HEAD");

    match sh.attempt("composer pint-test") {
        case Result.Ok(_): { println("✓ lint on ${branch}"); }
        case Result.Err(failed): { println("✗ lint, status", failed.status); }
    }
    sh.run("git tag release-${branch} -m 'Released from ${branch}'");   // a value never splits
    return 0;
}
```

A command is written as it is typed, and what `${...}` puts in it stays one argument: no shell reads it, so a value cannot become another command. A misspelt name is found before anything runs, and `?` in `main` ends the script with the reason.

### The whole toolchain, in one binary

`lumen run`, `build` to bytecode, `test`, `fmt`, `check` (`--json` for a CI step or an agent: every mistake with its code, its place and what usually fixes it), a REPL, a bytecode assembler and disassembler, a debugger that shows source lines, a look at the intermediate representation, limits on memory and call depth, and `lumen lsp`.

```text
$ lumen test
shop/prices_test.lum
  ok    a total adds up
  FAIL  an empty basket costs nothing
        expected 0, got 1
        at shop/prices_test.lum:9
1 passed, 1 failed
```

A test is a function `fn testATotalAddsUp()` in a file called `*_test.lum`: [docs/stdlib.md](docs/stdlib.md#test-files-lumen-test). `lumen fmt` writes every file the one way, with room in it: a block is never on the line of its brace, and what takes several lines stands apart. It is a Lumen program too: [docs/formatting.md](docs/formatting.md).

```text
$ lumen repl
[1]> import std.arrays;
[2]> let prices = [12.5, 3.0, 8.25]
[3]> prices.fold(0.0, (sum, p) -> sum + p)
23.75
```

Variables, functions and types stay from one line to the next, a declaration typed again replaces the one before, and Tab completes: [docs/repl.md](docs/repl.md).

## Where to read more

- **Getting started and language tour**: [docs/guide.md](docs/guide.md)
- **The standard library**: [docs/stdlib.md](docs/stdlib.md)
- **Tasks and channels**: [docs/tasks.md](docs/tasks.md)
- **Calling C libraries**: [docs/ffi.md](docs/ffi.md)
- **Editor support**: [docs/editor.md](docs/editor.md)
- **The formatter**: [docs/formatting.md](docs/formatting.md)
- **The REPL**: [docs/repl.md](docs/repl.md)
- **Syntax and semantics**: [docs/language-spec.md](docs/language-spec.md) (draft, with an implementation status table)
- **How the compiler works**: [docs/compilation/pipeline.md](docs/compilation/pipeline.md)
- **What is not there yet**: [docs/missing.md](docs/missing.md)
- **For an AI writing Lumen**: [llms.txt](llms.txt), the language on one page

## What is luvm?

luvm (Lumen Virtual Machine) is the bytecode interpreter for Lumen. It features:

- Register and stack hybrid execution model
- NaN-boxed value representation for efficient type handling
- Mark-sweep garbage collector
- Compact bytecode format

See [docs/luvm.md](docs/luvm.md) for detailed VM specification.

## Current Status

This is a prototype implementation. A Lumen program goes through the whole chain, from source text to execution:

- ✅ Lexer and parser, with error recovery
- ✅ Name resolution and static type checking (methods, traits, optionals, generics with inferred type arguments, casts and type tests, flow-sensitive narrowing)
- ✅ Generic functions and structs (monomorphization)
- ✅ Desugaring (`for`, `match`, short-circuit operators)
- ✅ Closure analysis
- ✅ IR generation
- ✅ Bytecode generation with register allocation
- ✅ luvm bytecode virtual machine with mark-sweep GC, bounded memory and call depth (`--heap-mb`, `--max-depth`)
- ✅ Binary bytecode format (.lc files), assembler and disassembler
- ✅ Interactive debugger, showing Lumen source lines
- ✅ Tasks scheduled by the virtual machine, one goroutine each, taking turns
- ✅ Language server (`lumen lsp`) and VS Code extension
- ✅ REPL (`lumen repl`): variables, declarations and closures kept from one input to the next
- ✅ Formatter (`lumen fmt`), written in Lumen, and a test runner (`lumen test`)
- ✅ Package manager in the manner of Composer (`lumen require`, `install`, `update`, `remove`, `show`), written in Lumen ([docs/packages.md](docs/packages.md))
- ✅ Executables (`lumen native`): a program's functions compiled to machine code through Go, about twice as fast as the interpreter ([docs/native.md](docs/native.md))
- ✅ CLI: `run`, `compile`, `native`, `assemble`, `dump`, `debug`, `test`, `check`, `fmt`, `repl`, `install`, `update`, `require`, `remove`, `show`, `lsp`

Language features that work: integers, floats, booleans, strings, arrays, structs, traits and methods, functions and recursion, closures, generics with requirements (`<T: Ordered>`), traits with type parameters and default methods, operators on structs, variadic functions, optionals, casts and type tests, `if`/`while`/`for`/`match` (on values, ranges and types, as a statement or for its value, with conditions and several patterns per case), loops over ranges (`for i in 0..10`), `defer`, short-circuit operators, `?.` and `?` for values that may be missing, tuples, string interpolation (`"total: ${n}"`) and texts over several lines, enums with pattern matching checked for completeness ([docs/enums.md](docs/enums.md)), constants, failures as values and recovery from runtime errors ([docs/errors.md](docs/errors.md)), programs in several files (`import`, `pub`; see [docs/modules.md](docs/modules.md)), described by an optional project file ([docs/project.md](docs/project.md)), and a built-in standard library ([docs/stdlib.md](docs/stdlib.md)): numbers, text, arrays, lists, dictionaries, sets, files and folders, dates, program arguments, JSON, CSV, regular expressions, encodings and hashes, an HTTP client and server, tests, tasks with channels for doing several things at once ([docs/tasks.md](docs/tasks.md)), and reflection. An editor gets mistakes as you type, hover, go to definition, an outline and suggestions from `lumen lsp`, a language server written in Lumen ([docs/editor.md](docs/editor.md)).

Not there yet:

- ⚠️ Packages are installed from git repositories with versions and a lock file (`lumen require`, `install`, `update`), but there is no registry yet; native code from shared libraries
- ⚠️ Several processors at once: tasks take turns on one
- ⚠️ Renaming and finding uses in the editor

What works is listed in the [implementation status table](docs/language-spec.md#13-implementation-status); what does not, with the way it fails today, in [docs/missing.md](docs/missing.md).

## Repository Structure

```
lumen/
  cmd/lumen/       - CLI entry point
  docs/            - Documentation
    compilation/   - One document per compiler stage
  editors/vscode/  - VS Code extension: colours and the language client
  examples/        - Example programs (.lum) and assembly files (.lasm)
  internal/
    token/         - Tokens and source positions
    ast/           - AST definitions
    errors/        - Diagnostics shared by all stages
    parser/        - Lexer and parser
    resolve/       - Name resolution, builtin functions
    types/         - Type system and type checker
    monomorph/     - Generics instantiation
    desugar/       - AST simplification
    analysis/      - Closure analysis
    ir/            - Intermediate representation
    irgen/         - AST -> IR
    codegen/       - IR -> bytecode
    compiler/      - Pipeline driver
    modules/       - Loading the files of a program
    project/       - The project file (lumen.json)
    stdlib/        - The standard library, in Lumen (lib/*.lum)
    inspect/       - Questions about a program's source: std.compiler
    lsp/           - The language server, a Lumen program
    formatter/     - lumen fmt, a Lumen program
    testrun/       - lumen test: finding the tests, writing the main that runs them
    repl/          - The REPL: sessions, the line editor
    vm/            - Virtual machine, tasks, native functions
    asm/           - Assembler and disassembler
    debugger/      - Debugger support
  test/
    e2e/           - End-to-end tests on .lum programs
    integration/   - Toolchain tests on .lasm files
```

## Building and Running

```bash
# Initialize dependencies
go mod tidy

# Build the project
go build ./...

# Run the CLI
go run ./cmd/lumen
```

## Working with Lumen Programs

```bash
# Compile and run a source file
go run ./cmd/lumen run examples/hello.lum

# Compile to bytecode, run it later: it does what the source does
go run ./cmd/lumen compile examples/fibonacci.lum -o fib.lc
go run ./cmd/lumen run fib.lc

# Make an executable of it, which runs without lumen
go run ./cmd/lumen native examples/fibonacci.lum -o fib

# Look at what the compiler produced
go run ./cmd/lumen dump --ir examples/hello.lum   # intermediate representation
go run ./cmd/lumen dump examples/hello.lum        # bytecode listing

# Step through a program
go run ./cmd/lumen debug examples/fibonacci.lum

# What follows the file is for the program (std.os.args)
go run ./cmd/lumen run examples/todo add "Buy milk" --due=2026-10-20
```

`run` exits with the `Int` returned by `main`, with the status given to `os.exit`, or with 1 after a compile or runtime error. Errors are reported with their source location:

```text
[ERROR] E1001: undefined identifier 'cuont'
  --> main.lum:3:12
   |
 3 |     return cuont + 1;
   |            ^^^^^
   |
   = Suggestion: Did you mean 'count'?
```

Example programs in [examples/](examples/):

- **hello.lum** - The smallest program
- **fibonacci.lum** - Functions, recursion, loops, arrays
- **shapes.lum** - Structs, traits, methods
- **closures.lum** - Anonymous functions and captured variables
- **generics.lum** - Generic and variadic functions, `match`
- **stack.lum** - A generic struct with methods, optionals, casts
- **dynamic.lum** - `Any`, `is`, `match` on types, null tests on fields
- **life.lum** - Conway's Game of Life: arrays of arrays, nested loops
- **sorting.lum** - Merge sort, quicksort and binary search for any type, the order passed as a function
- **tree.lum** - A generic binary search tree: a struct that refers to itself, optional fields
- **derivative.lum** - Symbolic differentiation: an enum that holds itself, the rules of calculus as `match` cases with nested patterns
- **tournament.lum** - Traits in full: operators, loops and brackets on structs, traits with type parameters and default methods, requirements on type parameters (`<T: Ordered>`), field defaults, methods as values
- **build.lum** - Tasks and channels: a build that works on several targets at once, with a fixed number of workers given back by `defer`
- **failures.lum** - `Result` for failures with a reason, an error type with a trace, `panic`, and `attempt` to survive what would stop the program
- **words.lum** - Word counts of a text with the standard library: `std.strings`, `std.dict`, `std.arrays`, `std.list`, `std.math`
- **modules.lum** - A program in several files: `import`, `pub`, with its modules in `zoo/`
- **project/** - A program with a project file (`lumen.json`): entry, module root, a package. Run it with `lumen run examples/project`
- **invoices/** - An invoice from a CSV file, with two packages required in `lumen.json` and locked in `lumen.lock`, one requiring the other. `lumen install` in its folder, then `lumen run examples/invoices`
- **calculator/** - A small language implementation in three modules: lexer, recursive-descent parser, and a tree of trait objects that evaluates itself. `lumen run examples/calculator`, and `lumen test examples/calculator` for its tests

Whole programs on the standard library, each a project to run by its folder:

- **todo/** - A to-do list kept in a JSON file: the command line read into an enum, errors that keep the error they came from, dates. `lumen run examples/todo add "Buy milk" --due=2026-10-20`, then `list`, `done 1`
- **logs/** - A report on a web server's access log: each line read with a regular expression into a struct or into the reason it could not be, then grouped, counted and charted. `lumen run examples/logs [access.log]`
- **sales/** - A sales report from a CSV file that is not quite clean: rows checked as they are read, totals by category, customer and week, a summary written as JSON. `lumen run examples/sales [orders.csv out.json]`
- **notes/** - A web service with its tests: the notes and their rules in one module, the routes in another, and tests that call the routes as the server would. `lumen run examples/notes` runs the tests, `lumen run examples/notes serve` listens on port 8080

## Working with Bytecode

### Assembly Format

Lumen assembly (.lasm) uses a simple text format:

```assembly
.constants 2
  0: 10.0        ; floating point number
  1: 5i          ; integer (48-bit)

.func main 3 0
  LOADK r0 0     ; load constant into register
  LOADK r1 1
  ADD r2 r0 r1   ; r2 = r0 + r1
  RET r2         ; return result
.endfunc
```

### Value Types

- **Numbers**: `42.5`, `3.14159`
- **Integers**: `42i`, `-100i` (48-bit signed)
- **Booleans**: `true`, `false`
- **Null**: `null`
- **Strings**: `"text"` (used with `NEWSTR` and as field names)

### Available Commands

```bash
# Assemble .lasm to .lc bytecode
go run ./cmd/lumen assemble input.lasm -o output.lc

# Print .lc bytecode as .lasm text
go run ./cmd/lumen dump input.lc > output.lasm

# Run bytecode, or assembly directly; --result prints what main returned
go run ./cmd/lumen run program.lc --result

# Example workflow
go run ./cmd/lumen assemble examples/add.lasm -o add.lc
go run ./cmd/lumen run add.lc --result
# Output: Result: Number(30)
```

### Examples

The full instruction reference is in [docs/assembly.md](docs/assembly.md). See the [examples/](examples/) directory for sample assembly programs:

- **add.lasm** - Simple addition
- **complex.lasm** - Multiple arithmetic operations
- **types.lasm** - Various value types

## Testing

Lumen has a three-level testing approach. See [docs/testing.md](docs/testing.md) for philosophy and details.

```bash
# Run everything
make test

# One level at a time
make test-unit          # Go unit tests of every internal package
make test-e2e           # the lumen binary on real .lum programs
make test-integration   # assembler / disassembler / VM commands

# Or directly with go
go test ./...
go test ./internal/compiler -run TestClosures -v
```

End-to-end expectations live next to the programs in `test/e2e/cases/` and `examples/expected/`; regenerate them after an intended change with `go test ./test/e2e -update`.

Toolchain test output is written to `test/output/` (gitignored).

## Requirements

- Go 1.24 or later

## License

MIT

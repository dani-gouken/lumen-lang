# Lumen Programming Language

Lumen is a prototype programming language with a focus on simplicity and efficiency.

## What is Lumen?

Lumen is a statically-typed programming language currently in early development. This repository contains the compiler, which turns `.lum` source files into bytecode, and the virtual machine that runs it.

```lumen
struct Point { x: Int; y: Int; }

impl Point {
    fn area() -> Int { return self.x * self.y; }
}

fn main() -> Int {
    let points = [Point { x: 2, y: 3 }, Point { x: 4, y: 5 }];
    let total = 0;
    for p in points {
        total = total + p.area();
    }
    println("total area:", total);
    return 0;
}
```

```bash
go run ./cmd/lumen run program.lum
# total area: 26
```

- **Getting started and language tour**: [docs/guide.md](docs/guide.md)
- **Syntax and semantics**: [docs/language-spec.md](docs/language-spec.md) (draft, with an implementation status table)
- **How the compiler works**: [docs/compilation/pipeline.md](docs/compilation/pipeline.md)
- **What is not there yet**: [docs/missing.md](docs/missing.md)

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
- ✅ CLI: `run`, `build`, `ir`, `asm`, `disasm`, `exec`, `runasm`, `lumdbg`

Language features that work: integers, floats, booleans, strings, arrays, structs, traits and methods, functions and recursion, closures, generics, variadic functions, optionals, casts and type tests, `if`/`while`/`for`/`match` (on values and on types), short-circuit operators, string interpolation (`"total: ${n}"`), enums with pattern matching checked for completeness ([docs/enums.md](docs/enums.md)), constants, failures as values and recovery from runtime errors ([docs/errors.md](docs/errors.md)), programs in several files (`import`, `pub`; see [docs/modules.md](docs/modules.md)), described by an optional project file ([docs/project.md](docs/project.md)), and a built-in standard library ([docs/stdlib.md](docs/stdlib.md)): numbers, text, arrays, lists, dictionaries, sets, files and folders, dates, program arguments, JSON, CSV, regular expressions, encodings and hashes, an HTTP client and server, and tests.

Not there yet:

- ⚠️ A way to install packages (`lumen.json` can point at folders of modules, nothing fetches them); native code from shared libraries
- ⚠️ REPL (stub only)

What works is listed in the [implementation status table](docs/language-spec.md#13-implementation-status); what does not, with the way it fails today, in [docs/missing.md](docs/missing.md).

## Repository Structure

```
lumen/
  cmd/lumen/       - CLI entry point
  docs/            - Documentation
    compilation/   - One document per compiler stage
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
    vm/            - Virtual machine implementation
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

# Compile to bytecode, run it later
go run ./cmd/lumen build examples/fibonacci.lum fib.lc
go run ./cmd/lumen exec fib.lc

# Look at what the compiler produced
go run ./cmd/lumen ir examples/hello.lum                 # intermediate representation
go run ./cmd/lumen build examples/hello.lum hello.lasm   # bytecode listing

# Step through a program
go run ./cmd/lumen lumdbg examples/fibonacci.lum

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
- **failures.lum** - `Result` for failures with a reason, an error type with a trace, `panic`, and `attempt` to survive what would stop the program
- **words.lum** - Word counts of a text with the standard library: `std.strings`, `std.dict`, `std.arrays`, `std.list`, `std.math`
- **modules.lum** - A program in several files: `import`, `pub`, with its modules in `zoo/`
- **project/** - A program with a project file (`lumen.json`): entry, module root, a package. Run it with `lumen run examples/project`
- **calculator/** - A small language implementation in three modules: lexer, recursive-descent parser, and a tree of trait objects that evaluates itself. `lumen run examples/calculator`

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
go run ./cmd/lumen asm input.lasm output.lc

# Disassemble .lc bytecode to .lasm text
go run ./cmd/lumen disasm input.lc output.lasm

# Execute .lc bytecode
go run ./cmd/lumen exec program.lc

# Example workflow
go run ./cmd/lumen asm examples/add.lasm add.lc
go run ./cmd/lumen exec add.lc
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

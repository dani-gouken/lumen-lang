# Lumen Testing Philosophy

## Overview

Lumen uses a pragmatic, three-level testing approach focused on **concise, high-value tests** rather than exhaustive coverage. We don't need an army of tests—we need the right tests.

| Level | What it exercises | Where | Run with |
|-------|-------------------|-------|----------|
| 1. Unit | One package at a time | `internal/*/..._test.go` | `make test-unit` |
| 2. Toolchain | The assembler/disassembler/VM commands on `.lasm` files | `test/integration/test.sh` | `make test-integration` |
| 3. End-to-end | `lumen run`/`build`/`ir` on real `.lum` programs | `test/e2e/` | `make test-e2e` |

## Testing Levels

### Level 1: Internal Tests (Go Unit Tests)

**What**: Test internal packages directly using Go's testing framework.

**Why**: Fast feedback, catches logic errors early, validates core functionality.

**Scope**:
- Individual functions and methods
- Core algorithms and data structures
- Edge cases and error conditions

**Philosophy**:
- Test the **contract**, not the implementation
- Focus on **critical paths** and **error handling**
- Keep tests **simple and readable**
- Avoid testing trivial getters/setters
- One test file per package (e.g., `vm_test.go`, `assembler_test.go`)

### Level 2: Toolchain Tests (Integration)

**What**: Test the compiled binary or `go run` end-to-end.

**Why**: Validates the complete workflow users experience.

**Scope**:
- Assemble → Execute → Verify result
- Disassemble → Round-trip verification
- Error handling in CLI commands

**Philosophy**:
- Test **user-facing workflows**
- Use **real example files**
- Verify **expected outcomes**
- Keep test cases **minimal but representative**

### Level 3: End-to-End Tests (Lumen programs)

**What**: Build the `lumen` binary and run it on `.lum` source files, checking stdout, stderr and the exit code.

**Why**: It is the only level that covers the whole chain a user depends on: CLI argument handling, every compiler stage, the VM, and how results and errors are presented.

**How it is organized** (`test/e2e/e2e_test.go`):

- `test/e2e/programs_test.go` runs the example projects as a person would: the to-do list one command per process with its file kept between them, the two reports on files of the test's own, and the notes service started as a server and asked over HTTP. It also checks which command line arguments reach a program, and the exit status of `os.exit`.
- `test/e2e/cases/NAME.lum` is run with `lumen run`. Optional files next to it state what to expect:
  - `NAME.out` — exact stdout (absent: no output)
  - `NAME.err` — lines that must each appear in stderr (absent: stderr must be empty)
  - `NAME.exit` — exit code (absent: 0)
- Every `examples/*.lum` is run too and compared with `examples/expected/NAME.out`, so the examples in the repository are guaranteed to work.
- A few Go tests cover the other commands: `build` then `exec`, `build` to a `.lasm` listing, `ir`, a missing file, and usage errors.
- A run that takes longer than 20 seconds fails the test. `limits_memory.lum` builds an array of a million and a half elements; it takes a fraction of a second, and took minutes when appending was quadratic. Performance that regresses that far is caught here.
- The `modules_*.lum` cases import the modules in `cases/game/`: a working program, private names and methods, missing modules, a runtime error inside a module. Files in subfolders of `cases/` are not run on their own, so that is where modules go. `TestModulesThroughCommands` takes a multi-file program through `build`, `exec`, `ir` and `lumdbg`; `TestModulesAreFoundNextToTheEntryFile` runs one from another directory by absolute path.
- `control.lum` runs a parcel depot on `match` that gives a value, cases with conditions and several patterns, ranges in loops and in cases, and `defer` around a dock that must be given back, also when the loading fails. `control_errors.lum` gets each wrong. `defer_failure.lum` stops on a failure nothing catches: the defers of every call in progress have printed their lines before the error, which is the same as it would be without them.
- `shortcuts.lum` reads the stock of a shop from text with tuples, `?`, `?.`, texts over several lines and tests that reach into function expressions; `shortcuts_errors.lum` gets each of them wrong and checks what is said.
- `traits.lum` works out the standings of a tournament with traits that give structs operators, loops and brackets, traits with type parameters and default methods, requirements on type parameters, field defaults and methods as values; `traits_errors.lum` gets each wrong.
- `lambdas.lum` works over a reading list with functions written without types: as arguments of generic functions, as fields, returned, nested, giving nothing back.
- `interpolation.lum` prints a receipt with expressions in strings: calls, fields, methods, constants, nesting, and what is not an expression (`$5`, `{a, b}`, `\${x}`).
- `panic.lum` stops itself: the output so far, the error with its call stack, exit code 1. The runtime-error cases (`runtime_error`, `cast_error`, `stdlib_error`, `modules_runtime_error`, `program_matrix`) check the code, the suggestion and the calls.
- `enums_json.lum` is a JSON document as an enum that holds arrays of itself: built, printed two ways, searched by path and summed. `constants.lum` draws a board from constants. `enums_errors.lum` and `constants_errors.lum` check the diagnostics, suggestions included.
- `TestStandardLibrary` runs programs that use the library with nothing but the binary: input from standard input, the end of the input, files written by one run and read by the next, bytecode built and executed, and a project replacing `std` with its own. The `stdlib_*.lum` and `extern_unknown.lum` cases check the messages for a failing library call, for misuse caught by the type checker, and for an `extern fn` the runtime does not have.
- `TestExampleProjects` runs every folder of `examples/` that has a `lumen.json` and compares its output with `examples/expected/<folder>.out`, as `TestExamples` does for the single files.
- `TestExampleProject` runs `examples/project` every way a project can be run: its folder, its entry file, no argument from inside it, through `build`, `ir` and `lumdbg`. `TestProjectFile` writes small projects to a temporary folder to check `root`, `packages`, the limits and their override from the command line, every mistake in `lumen.json` with its message, and that nothing changes without one.
- `TestLargeProgram` generates a program of 500 structs, each with a trait implementation, and runs it from source and from a bytecode file. `TestDebuggerSession` drives `lumdbg` with commands on standard input: stepping, registers, state, continue.
- The `program_*.lum` cases are complete programs of 150 to 250 lines (a bank, matrix arithmetic, a calculator with tokenizer and parser). Each combines most of the language in one place: traits and generics, closures, optionals and narrowing, casts and `match` on types, loops with indexes. They catch interactions that one-feature tests cannot.

To add a case, drop a `.lum` file in `cases/` and generate its expected output:

```bash
go test ./test/e2e -update   # rewrites the .out files from actual output
git diff test/e2e examples   # review what changed before committing
```

`-update` only writes `.out` files. `.err` and `.exit` are written by hand, because they state intent: which diagnostics matter and how the program should end.

**Philosophy**:
- `.err` files list the stable, meaningful lines (error code, message, location), not the whole rendered diagnostic, so a cosmetic change to the layout does not break every case
- One program per behavior that only shows at this level (exit codes, error reporting); `features.lum` is the single broad program that touches everything

## What We Test

### 1. Assembler (`internal/asm/assembler_test.go`)

**Critical tests**:
- ✅ Parse valid assembly syntax
- ✅ Handle all value types (number, int, bool, null)
- ✅ Parse all instruction formats
- ✅ Error on invalid syntax with line numbers
- ✅ Error on out-of-range values

**Non-goals**:
- ❌ Exhaustive combinations of every instruction
- ❌ Testing lexer tokens individually
- ❌ Unicode edge cases (not a priority)

### 2. Disassembler (`internal/asm/disassembler_test.go`)

**Critical tests**:
- ✅ Format all instruction types correctly
- ✅ Format all value types correctly
- ✅ Round-trip: asm → binary → disasm → asm produces identical bytecode

**Non-goals**:
- ❌ Pretty-printing edge cases
- ❌ Comment formatting

### 3. Virtual Machine (`internal/vm/vm_test.go`)

**Critical tests**:
- ✅ Execute basic instructions (LOADK, MOV, RET)
- ✅ Arithmetic operations (ADD, SUB, MUL, DIV)
- ✅ Type checking (int + int, number + number)
- ✅ Error on type mismatch
- ✅ Error on invalid opcodes

**Non-goals**:
- ❌ Performance benchmarks (premature)
- ❌ Stress tests with thousands of instructions
- ❌ Memory leak detection (future concern)

### 4. Bytecode Serialization (`internal/vm/bytecode_test.go`)

**Critical tests**:
- ✅ Serialize and deserialize chunks
- ✅ Preserve all value types
- ✅ Handle empty chunks
- ✅ Error on corrupted bytecode
- ✅ Error on version mismatch

**Non-goals**:
- ❌ Testing every possible corruption scenario
- ❌ Backwards compatibility (only one version exists)

**Also in the VM package**: `errors_test.go` raises each kind of runtime error from instructions and checks its kind, code, suggestion and call stack, the exact text it is shown as, how long and repetitive stacks are shortened, and what is shown without debug information.

**Also in the VM package**: `variant_test.go` builds values of an enum from instructions: how they print, when two are equal (variant, values, values inside values, and what stays compared by identity), how type tests see them, and a long chain of them kept alive across collections.

**Also in the VM package**: `native_test.go` calls each native function of the standard library directly: results, the argument checks that protect against hand-written bytecode, files in a temporary folder, input, seeded random numbers per VM. `TestNativeResultsSurviveCollection` sizes the heap so that it is collected while a native is building its result, and spoils whatever is freed; it fails if the native does not pin what it holds. `native_more_test.go` does the same for the natives of the later modules: paths, folders in a temporary one, running programs, regular expressions, the tokens and error positions of JSON, CSV, encodings, hashes against published values, dates, and both halves of HTTP (the client against a Go test server, the server asked by a Go client while the test plays the program). `native_arrays_test.go` is for the natives that make an array shorter or put an item in its middle: every position including the ones an array does not have, that the capacity stays and no object is left in the part of it that is no longer the array's, that what is taken out is freed by the next collection and what is put in is kept, that the heap's count stays exactly what the array's capacity makes it and returns to where it started, and running out of memory while putting an item in.

**Also in the VM package**: `wide_operands_test.go` runs chunks whose registers, constants, functions and field names have indexes past 255, jumps across more instructions than an operand can address, and round-trips such a chunk through the file format. Operands were one byte once; an index that silently wrapped would read the wrong register or field.

**Also in the VM package**: `limits_test.go` covers what bounds a running program and what keeps it fast: reuse of function objects, accounting of array and object growth, the number of collections for a large array, running out of memory, detaching closures from a register, and call depth.

**Also in the VM package**: `runtime_test.go` covers string operations, field names from the string pool, runtime type and bounds errors, the host functions and value formatting; `cast_test.go` covers the cast descriptor parser and the matcher that verifies `value as Type` and `value is Type`, case by case (scalars, optionals, arrays, structs, generic structs, values containing themselves), the subtyping used for function types, and that signatures survive a round trip through a bytecode file.

### 5. Compiler front end

Tests for the stages that turn source into IR are driven **from source text**: a small helper parses (and resolves, type checks, ...) a snippet and the test asserts on the result. This keeps them short and independent of how AST nodes are built by hand.

| Package | File | Critical tests |
|---------|------|----------------|
| `internal/parser` | `lexer_test.go`, `parser_test.go` | String interpolation as the tokens it becomes: every position in the string, nesting, braces and strings inside the expression, each mistake, and the positions of real and made-up tokens. Tokens and positions; operator precedence; `a < b` vs `f<T>(x)`; struct literal vs block; every error code; recovery after a broken statement |
| `internal/modules` | `modules_test.go`, `errors_test.go` | Programs of several files held in a map, checked by printing the merged program back as source: names reached through each form of import; what a local variable or type parameter hides; match patterns on imported types; chains of re-exports; modules importing each other; each file read once; every error code with its message and suggestion; diagnostics quoting the line of the right file |
| `internal/stdlib` | `stdlib_test.go` | The library, used from Lumen programs: every function of every module with ordinary and edge inputs, checked against the expected output; the runtime errors a misuse gives; that each `extern fn` has a native and each native a declaration; that every module compiles |
| `internal/stdlib` | `arrays_change_test.go` | `arrays.pop`, `removeAt`, `insertAt` and `clear`: every index, in and out of the array; `insertAt` against `inserted` for each of them; arrays of every kind of item, through generic functions and generic structs; an array of optionals, where `null` is an item; the array seen changed by everything that holds it; removing while going through by index, and the failure inside `for ... in`; a program with a stack of values, a stack of calls, a queue and a history of closures; `std.list` on top of them; a stack and a queue filled and emptied many times on a small heap |
| `internal/stdlib` | `more_test.go` | The later modules, the same way. A Lumen program serving HTTP is run next to a Go client (`serve`); `TestExit` checks that `os.exit` gets past `attempt`; `TestLibraryUnderMemoryPressure` runs JSON, CSV and regex with a heap of 3 MB, collected many times while they work |
| `internal/project` | `project_test.go` | `lumen.json`: every key; each malformed file with its message and line; paths resolved from the file's folder; the nearest file found from any depth |
| `internal/resolve` | `source_test.go` | Builtins in scope; lambda parameters and bodies; `self` only in methods |
| `internal/types` | `source_test.go` | Inferred types of member/index/lambda/generic expressions; programs that must be accepted; programs that must be rejected, with the message. Covers methods and their type arguments, traits, `Self`, optionals and narrowing of variables and fields, type tests and `match` on types, `Any`, generics, type argument inference from arguments and from the expected type (including the arguments written into the AST), which calls end what is known about a field, casts (including the runtime descriptor each one produces), recorded function signatures, `const`, return paths and unreachable-code warnings |
| `internal/types` | `enums_test.go` | Enums and constants: programs accepted and rejected with the message; the suggestion each diagnostic carries; completeness of a match through nested patterns, with the value named as missing; cases reported as never reached; type arguments of generic enums from values, from the expected type and through nested values; the runtime description of each cast to an enum |
| `internal/desugar` | `enums_test.go` | Constants replaced by their value at each use, positioned there; variants lowered to values and to functions; patterns lowered to tests and names in the right order; the last case of a complete match as the final `else` |
| `internal/analysis` | `source_test.go` | Which variables each lambda captures, including ones passed through an intermediate lambda |
| `internal/desugar` | `source_test.go` | `continue` cannot skip a for-loop increment; the iterable is evaluated once; short-circuit operators are kept; type patterns become type tests on the variable itself |
| `internal/irgen` | `source_test.go` | `let` copies instead of aliasing; block scopes; short-circuit branches; struct fields by name; direct call on a known struct vs lookup through a trait; variadic packing; capture operands |

### 6. Bytecode generation (`internal/codegen/codegen_test.go`)

Each test builds a small IR function with `ir.Builder`, generates bytecode and **executes it**, asserting on the returned value. Testing behavior rather than the emitted instructions keeps the tests valid when the register allocator changes.

**Critical tests**:
- ✅ Every constant kind loads correctly
- ✅ Arguments survive calls (the VM returns results in `r0`)
- ✅ Closures share captured variables by reference
- ✅ Temporaries share registers (a 400-step expression needs a handful)
- ✅ Each limit and each unsupported construct reports its error code

### 7. Whole pipeline (`internal/compiler/compiler_test.go`)

Feature tests: one test per language feature, each a short program whose printed output is compared with the expected text. They run in-process (no binary), so they are fast and failures show the diagnostics directly.

**Critical tests**:
- ✅ One test per feature area: arithmetic, strings, scopes, control flow, short-circuit evaluation, functions, arrays, structs and methods, closures, generics and variadics, match
- ✅ The garbage collector keeps live values while the heap is recycled many times
- ✅ Runtime errors name the source line and function
- ✅ One compile error per stage, checked by error code
- ✅ The pipeline stops at the first failing stage
- ✅ Failures (`failures_test.go`): `panic` with its message, place and stack; how it ends a path for the type checker; `attempt` on every kind of failure; what is left behind after one (data, loops, closures, call depth, memory); an attempt inside an attempt; the fields of a `Failure`; errors as values with a trace of their own; `Result` and its methods
- ✅ Enums and constants (`enums_test.go`): a simplifier of formulas written as cases with nested patterns; `Option`, `Result` and a linked list as generic enums with generic methods; enums with traits, in structs, arrays and closures; `match` inside loops with `break` and `continue`; constants in every position; all of it again across modules
- ✅ Programs of several files (`modules_test.go`): a shop spread over six modules using generics, traits, closures and optionals across files; structs of the same name in different modules told apart by `is`, `as` and `match`; type errors across modules; method visibility, with accepted and rejected calls; compile and runtime errors naming the module's file, line and function

**When a program misbehaves**, add the smallest reproduction here first; move down to the package that owns the bug once it is located.

**When a feature is added**, it gets tests at every level in the same change: accept/reject cases in the package that owns the rule, a feature test here that also combines it with existing features, and, if it changes how programs are written, a use in one of the `program_*.lum` end-to-end cases.

## Test Organization

```
lumen/
  internal/
    vm/
      vm.go
      vm_test.go           # Unit tests for VM execution
      bytecode.go
      bytecode_test.go     # Unit tests for serialization
    asm/
      assembler.go
      assembler_test.go    # Unit tests for assembler
      disassembler.go
      disassembler_test.go # Unit tests for disassembler
    parser/ resolve/ types/ analysis/ desugar/ irgen/
      source_test.go       # Stage tests driven from source text
    codegen/
      codegen_test.go      # IR -> bytecode, verified by execution
    modules/
      modules_test.go      # Multi-file programs: loading and renaming
      errors_test.go       # Every way an import can be wrong
    compiler/
      compiler_test.go     # Feature tests through the whole pipeline
      modules_test.go      # The same for programs of several files
  examples/
    *.lum                  # Example programs, run by the e2e tests
    expected/*.out         # Their expected output
  test/
    e2e/
      e2e_test.go          # Runs the lumen binary on .lum files
      cases/               # Programs with expected .out/.err/.exit
        game/              # Modules imported by the modules_*.lum cases
    integration/           # Toolchain integration tests
      test.sh             # Main test script
    output/               # Test artifacts (gitignored)
```

## Running Tests

### Everything
```bash
make test             # unit + end-to-end + toolchain
```

### Under the race detector, and measured

```bash
make test-race   # the packages that run programs, with Go's race detector
make bench       # the virtual machine: calls, fields
```

### Inputs made by a machine (`test/fuzz`)

Three tests look for what nobody thought to write a test for:

*   `FuzzSource` gives the compiler any text, and runs what compiles;
*   `FuzzBytecode` gives the bytecode loader any bytes, and runs what loads;
*   `TestRandomProgramsMeanWhatTheySay` makes programs at random (arithmetic, loops, arrays, a struct, functions, closures) and compares what each prints with what a few lines of Go say it means. This is the one that can catch a program compiled into something that runs and prints the wrong number.

Everything runs in `internal/sandbox`: bounded steps, memory, depth and output, and no native function that reaches outside the program. As ordinary tests the two fuzzers run on the programs of the repository; `make fuzz` lets them search for new inputs, a minute each (`FUZZ_TIME=10m make fuzz` for longer). An input that fails is saved under `test/fuzz/testdata` and runs with the tests from then on: commit it with the fix.

### A real program (`examples/lox`, `test/e2e/lox`)

`examples/lox` is an interpreter for Lox, the language of *Crafting Interpreters*, written in Lumen: a scanner, a parser, a resolver and a tree-walking interpreter with closures, classes and inheritance, some 1,400 lines in six modules. `TestLox` builds it and runs it on the 241 test programs of the book that apply to such an interpreter (`test/e2e/lox`, copied unchanged, under their own licence), comparing what each prints, how it fails and the status it ends with.

It is the widest test there is of Lumen doing what it is for. Writing it found three defects no other test had: Floats compared by their bits (`0.0 == -0.0` was false), a method stored on a value overwriting a field of the same name, and a built program not ending with the status its source ends with.

### Whole programs, measured (`test/bench`)

`test/bench/programs` holds one program per part of the language (calls, arrays, structs, traits, closures, texts, enums), each with the output it must print. `make bench` times them, and the compiler on them. `BenchmarkLox` runs the Lox interpreter on two Lox programs: a program of real size, doing everything at once. Before a change meant to make Lumen faster or smaller, save its output; after, compare (`benchstat`). `TestBytecodeSize` logs how large each compiles to.

### Limits on the tests themselves

Every `make` target runs the tests with a time limit, and the packages that run programs (`internal/vm`, `internal/stdlib`, `internal/compiler`, `internal/inspect`) end themselves if the test binary comes to hold more than 4 GB (`internal/memguard`, from a `TestMain` in each). A test that goes wrong, by a loop that never ends or a value that grows without end, fails; it does not take the machine down. When running `go test` by hand, pass `-timeout`.

### One Level
```bash
make test-unit        # Go unit tests of every internal package
make test-e2e         # lumen binary on .lum programs
make test-integration # assembler/disassembler/VM commands
```

### One Package or Test
```bash
go test ./internal/parser
go test ./internal/compiler -run TestClosures -v
go test ./test/e2e -run TestCases/features -v
```

## Test Naming Conventions

**Go unit tests**:
```go
func TestAssembleSimpleAdd(t *testing.T)         // Happy path
func TestAssembleInvalidSyntax(t *testing.T)     // Error case
func TestVMExecuteArithmetic(t *testing.T)       // Feature test
```

**Integration test names**:
- Descriptive, reflects user workflow
- e.g., "Assemble and execute add.lasm"

## When to Add Tests

**Always add tests for**:
- ✅ New opcodes or instructions
- ✅ New value types
- ✅ Bug fixes (regression tests)
- ✅ Error handling paths

**Don't add tests for**:
- ❌ Refactoring without behavior change
- ❌ Formatting or cosmetic changes
- ❌ Trivial functions (e.g., getters)

## Test Data

**Use real examples**:
- Prefer `examples/*.lasm` files for integration tests
- Create minimal inline data for unit tests
- Avoid large generated test fixtures

## Assertions

**Keep assertions simple**:
```go
// Good
if result != expected {
    t.Errorf("got %v, want %v", result, expected)
}

// Avoid
assert.NotNil(t, result)
assert.Equal(t, expected, result)
assert.True(t, result.IsValid())
```

Use standard library, avoid heavy testing frameworks.

## Coverage Goals

**We don't chase 100% coverage.**

**Target**:
- Critical paths: ~90%
- Error handling: ~80%
- Total: ~70%

**Why**:
- Diminishing returns beyond this
- Code churn makes high coverage expensive
- Focus on **correctness over coverage**

## Example Test Structure

```go
func TestVMExecuteAdd(t *testing.T) {
    // Setup
    chunk := vm.NewChunk()
    chunk.AddConstant(vm.NewInt(10))
    chunk.AddConstant(vm.NewInt(20))
    chunk.AddInstruction(vm.NewInstr(vm.OpLOADK, 0, 0, 0))
    chunk.AddInstruction(vm.NewInstr(vm.OpLOADK, 1, 1, 0))
    chunk.AddInstruction(vm.NewInstr(vm.OpADD, 2, 0, 1))
    chunk.AddInstruction(vm.NewInstr(vm.OpRET, 2, 0, 0))
    chunk.RegisterCount = 3

    // Execute
    v := vm.NewVM()
    result, err := v.Exec(chunk)

    // Assert
    if err != nil {
        t.Fatalf("execution failed: %v", err)
    }
    if !result.IsInt() {
        t.Fatalf("expected int, got %s", result.String())
    }
    if result.AsInt() != 30 {
        t.Errorf("expected 30, got %d", result.AsInt())
    }
}
```

## Philosophy Summary

1. **Pragmatic over dogmatic** - Test what matters
2. **Quality over quantity** - Few good tests beat many bad ones
3. **Fast feedback** - Tests should run in seconds
4. **Maintainable** - Simple tests that won't break on refactoring
5. **Real-world** - Use actual examples, not contrived data

**Remember**: We're building a prototype. Perfect is the enemy of good enough.

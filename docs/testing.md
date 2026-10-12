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

- `test/e2e/cases/NAME.lum` is run with `lumen run`. Optional files next to it state what to expect:
  - `NAME.out` — exact stdout (absent: no output)
  - `NAME.err` — lines that must each appear in stderr (absent: stderr must be empty)
  - `NAME.exit` — exit code (absent: 0)
- Every `examples/*.lum` is run too and compared with `examples/expected/NAME.out`, so the examples in the repository are guaranteed to work.
- A few Go tests cover the other commands: `build` then `exec`, `build` to a `.lasm` listing, `ir`, a missing file, and usage errors.
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

**Also in the VM package**: `runtime_test.go` covers string operations, field names from the string pool, runtime type and bounds errors, the host functions and value formatting; `cast_test.go` covers the cast descriptor parser and the matcher that verifies `value as Type` and `value is Type`, case by case (scalars, optionals, arrays, structs, generic structs, values containing themselves), the subtyping used for function types, and that signatures survive a round trip through a bytecode file.

### 5. Compiler front end

Tests for the stages that turn source into IR are driven **from source text**: a small helper parses (and resolves, type checks, ...) a snippet and the test asserts on the result. This keeps them short and independent of how AST nodes are built by hand.

| Package | File | Critical tests |
|---------|------|----------------|
| `internal/parser` | `lexer_test.go`, `parser_test.go` | Tokens and positions; operator precedence; `a < b` vs `f<T>(x)`; struct literal vs block; every error code; recovery after a broken statement |
| `internal/resolve` | `source_test.go` | Builtins in scope; lambda parameters and bodies; `self` only in methods |
| `internal/types` | `source_test.go` | Inferred types of member/index/lambda/generic expressions; programs that must be accepted; programs that must be rejected, with the message. Covers methods and their type arguments, traits, `Self`, optionals and narrowing of variables and fields, type tests and `match` on types, `Any`, generics, type argument inference from arguments and from the expected type (including the arguments written into the AST), which calls end what is known about a field, casts (including the runtime descriptor each one produces), recorded function signatures, `const`, return paths and unreachable-code warnings |
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
    compiler/
      compiler_test.go     # Feature tests through the whole pipeline
  examples/
    *.lum                  # Example programs, run by the e2e tests
    expected/*.out         # Their expected output
  test/
    e2e/
      e2e_test.go          # Runs the lumen binary on .lum files
      cases/               # Programs with expected .out/.err/.exit
    integration/           # Toolchain integration tests
      test.sh             # Main test script
    output/               # Test artifacts (gitignored)
```

## Running Tests

### Everything
```bash
make test             # unit + end-to-end + toolchain
```

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

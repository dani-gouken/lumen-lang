# Lumen Testing Philosophy

## Overview

Lumen uses a pragmatic, two-level testing approach focused on **concise, high-value tests** rather than exhaustive coverage. We don't need an army of tests—we need the right tests.

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
  test/
    integration/           # Toolchain integration tests
      test.sh             # Main test script
    output/               # Test artifacts (gitignored)
```

## Running Tests

### Quick (Unit Tests Only)
```bash
make test
```

### Full (Unit + Integration)
```bash
make test-all
```

### Individual Components
```bash
make test-vm          # Test VM only
make test-asm         # Test assembler/disassembler only
make test-integration # Test toolchain only
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

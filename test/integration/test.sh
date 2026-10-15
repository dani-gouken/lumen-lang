#!/bin/bash
set -e

# Integration tests for Lumen toolchain
# Tests assembler, disassembler, and VM execution

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT_DIR="$PROJECT_ROOT/test/output"
EXAMPLES_DIR="$PROJECT_ROOT/examples"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counter
TESTS_RUN=0
TESTS_PASSED=0

# Determine lumen command
if [ -n "$USE_BINARY" ]; then
    LUMEN="$PROJECT_ROOT/lumen"
    if [ ! -f "$LUMEN" ]; then
        echo -e "${RED}ERROR: Binary not found at $LUMEN${NC}"
        echo "Run 'make build' first or unset USE_BINARY"
        exit 1
    fi
else
    # The go the Makefile uses, else the one on the PATH, else where the
    # official installer puts it
    GO_BIN="${GO:-$(command -v go || echo /usr/local/go/bin/go)}"
    LUMEN="$GO_BIN run $PROJECT_ROOT/cmd/lumen"
fi

echo "Using lumen command: $LUMEN"
echo

# Setup
mkdir -p "$OUTPUT_DIR"
rm -f "$OUTPUT_DIR"/*

# Test helper functions
test_name() {
    echo -e "${YELLOW}TEST: $1${NC}"
    TESTS_RUN=$((TESTS_RUN + 1))
}

pass() {
    echo -e "${GREEN}✓ PASS${NC}"
    echo
    TESTS_PASSED=$((TESTS_PASSED + 1))
}

fail() {
    echo -e "${RED}✗ FAIL: $1${NC}"
    echo
    exit 1
}

# Test 1: Assemble simple program
test_name "Assemble add.lasm"
$LUMEN assemble "$EXAMPLES_DIR/add.lasm" -o "$OUTPUT_DIR/add.lc" > /dev/null 2>&1 || fail "assembly failed"
[ -f "$OUTPUT_DIR/add.lc" ] || fail "bytecode file not created"
[ -s "$OUTPUT_DIR/add.lc" ] || fail "bytecode file is empty"
pass

# Test 2: Execute bytecode
test_name "Execute add.lc"
result=$($LUMEN run --result "$OUTPUT_DIR/add.lc" 2>&1)
echo "$result" | grep -q "Result: Number(30)" || fail "unexpected result: $result"
pass

# Test 3: Disassemble bytecode
test_name "Disassemble add.lc"
$LUMEN dump "$OUTPUT_DIR/add.lc" > "$OUTPUT_DIR/add_disasm.lasm" 2>/dev/null || fail "disassembly failed"
[ -f "$OUTPUT_DIR/add_disasm.lasm" ] || fail "disassembled file not created"
grep -q "LOADK r0 0" "$OUTPUT_DIR/add_disasm.lasm" || fail "missing expected instruction"
grep -q "ADD r2 r0 r1" "$OUTPUT_DIR/add_disasm.lasm" || fail "missing ADD instruction"
pass

# Test 4: Round-trip verification
test_name "Round-trip: assemble → bytecode → dump → assemble"
$LUMEN assemble "$OUTPUT_DIR/add_disasm.lasm" -o "$OUTPUT_DIR/add_roundtrip.lc" > /dev/null 2>&1 || fail "re-assembly failed"
result1=$($LUMEN run --result "$OUTPUT_DIR/add.lc" 2>&1)
result2=$($LUMEN run --result "$OUTPUT_DIR/add_roundtrip.lc" 2>&1)
[ "$result1" = "$result2" ] || fail "round-trip produced different results"
pass

# Test 5: Execute complex example
test_name "Execute complex.lasm"
$LUMEN assemble "$EXAMPLES_DIR/complex.lasm" -o "$OUTPUT_DIR/complex.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/complex.lc" 2>&1)
echo "$result" | grep -q "Result: Int(23)" || fail "unexpected result: $result"
pass

# Test 6: Execute types example
test_name "Execute types.lasm"
$LUMEN assemble "$EXAMPLES_DIR/types.lasm" -o "$OUTPUT_DIR/types.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/types.lc" 2>&1)
echo "$result" | grep -q "Result: Number(42.5)" || fail "unexpected result: $result"
pass

# Test 7: Error handling - invalid bytecode
test_name "Error on invalid bytecode"
echo "invalid" > "$OUTPUT_DIR/invalid.lc"
$LUMEN run --result "$OUTPUT_DIR/invalid.lc" 2>&1 | grep -qi "error" || fail "should error on invalid bytecode"
pass

# Test 8: Error handling - invalid assembly
test_name "Error on invalid assembly syntax"
echo "INVALID SYNTAX" > "$OUTPUT_DIR/invalid.lasm"
$LUMEN assemble "$OUTPUT_DIR/invalid.lasm" -o "$OUTPUT_DIR/out.lc" 2>&1 | grep -q "error" || fail "should error on invalid syntax"
pass

# Test 9: Disassemble preserves value types
test_name "Disassemble preserves value types"
$LUMEN dump "$OUTPUT_DIR/complex.lc" > "$OUTPUT_DIR/complex_disasm.lasm" 2>/dev/null || fail "disassembly failed"
grep -q "5i" "$OUTPUT_DIR/complex_disasm.lasm" || fail "integer suffix not preserved"
grep -q "MUL r2 r0 r1" "$OUTPUT_DIR/complex_disasm.lasm" || fail "MUL instruction missing"
pass

# Test 10: Array operations
test_name "Execute array_example.lasm"
$LUMEN assemble "$EXAMPLES_DIR/array_example.lasm" -o "$OUTPUT_DIR/array.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/array.lc" 2>&1)
echo "$result" | grep -q "Result: Int(20)" || fail "unexpected result: $result"
pass

# Test 11: Disassemble array bytecode
test_name "Disassemble array bytecode with memory opcodes"
$LUMEN dump "$OUTPUT_DIR/array.lc" > "$OUTPUT_DIR/array_disasm.lasm" 2>/dev/null || fail "disassembly failed"
grep -q "NEWARRAY r0 5" "$OUTPUT_DIR/array_disasm.lasm" || fail "missing NEWARRAY instruction"
grep -q "SETINDEX" "$OUTPUT_DIR/array_disasm.lasm" || fail "missing SETINDEX instruction"
grep -q "GETINDEX" "$OUTPUT_DIR/array_disasm.lasm" || fail "missing GETINDEX instruction"
grep -q "ARRLEN" "$OUTPUT_DIR/array_disasm.lasm" || fail "missing ARRLEN instruction"
pass

# Test 12: Object operations
test_name "Execute object_example.lasm"
$LUMEN assemble "$EXAMPLES_DIR/object_example.lasm" -o "$OUTPUT_DIR/object.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/object.lc" 2>&1)
echo "$result" | grep -q "Result: Int(25)" || fail "unexpected result: $result"
pass

# Test 13: Nested structures
test_name "Execute nested_structures.lasm"
$LUMEN assemble "$EXAMPLES_DIR/nested_structures.lasm" -o "$OUTPUT_DIR/nested.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/nested.lc" 2>&1)
echo "$result" | grep -q "Result: Int(100)" || fail "unexpected result: $result"
pass

# Test 14: Garbage collection
test_name "Execute gc_demo.lasm"
$LUMEN assemble "$EXAMPLES_DIR/gc_demo.lasm" -o "$OUTPUT_DIR/gc.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/gc.lc" 2>&1)
echo "$result" | grep -q "Result: Int(42)" || fail "unexpected result: $result"
pass

# Test 15: Disassemble GC bytecode
test_name "Disassemble GC bytecode"
$LUMEN dump "$OUTPUT_DIR/gc.lc" > "$OUTPUT_DIR/gc_disasm.lasm" 2>/dev/null || fail "disassembly failed"
grep -q "NEWARRAY" "$OUTPUT_DIR/gc_disasm.lasm" || fail "missing NEWARRAY instruction"
grep -q "NEWOBJ" "$OUTPUT_DIR/gc_disasm.lasm" || fail "missing NEWOBJ instruction"
grep -q "GC" "$OUTPUT_DIR/gc_disasm.lasm" || fail "missing GC instruction"
pass

# Test 16: Memory round-trip
test_name "Round-trip memory opcodes: assemble → bytecode → dump → assemble"
$LUMEN dump "$OUTPUT_DIR/nested.lc" > "$OUTPUT_DIR/nested_disasm.lasm" 2>/dev/null || fail "disassembly failed"
$LUMEN assemble "$OUTPUT_DIR/nested_disasm.lasm" -o "$OUTPUT_DIR/nested_roundtrip.lc" > /dev/null 2>&1 || fail "re-assembly failed"
result1=$($LUMEN run --result "$OUTPUT_DIR/nested.lc" 2>&1)
result2=$($LUMEN run --result "$OUTPUT_DIR/nested_roundtrip.lc" 2>&1)
[ "$result1" = "$result2" ] || fail "round-trip produced different results"
pass

# Test 17: Function calls
test_name "Execute function_call.lasm"
$LUMEN assemble "$EXAMPLES_DIR/function_call.lasm" -o "$OUTPUT_DIR/function_call.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/function_call.lc" 2>&1)
echo "$result" | grep -q "Result: Int(15)" || fail "unexpected result: $result"
pass

# Test 18: Recursion (factorial)
test_name "Execute recursion.lasm (factorial)"
$LUMEN assemble "$EXAMPLES_DIR/recursion.lasm" -o "$OUTPUT_DIR/recursion.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/recursion.lc" 2>&1)
echo "$result" | grep -q "Result: Int(120)" || fail "unexpected result: $result (expected 120)"
pass

# Test 19: External function calls
test_name "Execute extern_call.lasm"
$LUMEN assemble "$EXAMPLES_DIR/extern_call.lasm" -o "$OUTPUT_DIR/extern_call.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/extern_call.lc" 2>&1)
# println prints values the way a program shows them, not the debug form
echo "$result" | grep -qx "42" || fail "println output not found"
echo "$result" | grep -qx "100" || fail "println output not found"
pass

# Test 20: Comprehensive functions
test_name "Execute comprehensive_functions.lasm"
$LUMEN assemble "$EXAMPLES_DIR/comprehensive_functions.lasm" -o "$OUTPUT_DIR/comprehensive.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/comprehensive.lc" 2>&1)
echo "$result" | grep -qx "6" || fail "factorial(3) output not found"
echo "$result" | grep -qx "4" || fail "multiply(2,2) output not found"
pass

# Test 21: Disassemble function call bytecode
test_name "Disassemble function call bytecode with labels"
$LUMEN dump "$OUTPUT_DIR/function_call.lc" > "$OUTPUT_DIR/function_disasm.lasm" 2>/dev/null || fail "disassembly failed"
grep -q "CALL" "$OUTPUT_DIR/function_disasm.lasm" || fail "missing CALL instruction"
grep -q "RET" "$OUTPUT_DIR/function_disasm.lasm" || fail "missing RET instruction"
pass

# Test 22: Round-trip function bytecode
test_name "Round-trip function bytecode with extern names"
$LUMEN dump "$OUTPUT_DIR/extern_call.lc" > "$OUTPUT_DIR/extern_disasm.lasm" 2>/dev/null || fail "disassembly failed"
$LUMEN assemble "$OUTPUT_DIR/extern_disasm.lasm" -o "$OUTPUT_DIR/extern_roundtrip.lc" > /dev/null 2>&1 || fail "re-assembly failed"
result1=$($LUMEN run --result "$OUTPUT_DIR/extern_call.lc" 2>&1 | tail -1)
result2=$($LUMEN run --result "$OUTPUT_DIR/extern_roundtrip.lc" 2>&1 | tail -1)
[ "$result1" = "$result2" ] || fail "round-trip produced different results"
pass

# Test 23: Long jump instructions
test_name "Execute long_jump.lasm with LONG prefix instructions"
$LUMEN assemble "$EXAMPLES_DIR/long_jump.lasm" -o "$OUTPUT_DIR/long_jump.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/long_jump.lc" 2>&1)
echo "$result" | grep -q "Result: Int(30)" || fail "unexpected result: $result (expected 30)"
pass

# Test 24: Disassemble long jump bytecode
test_name "Disassemble long jump bytecode shows LONG prefix"
$LUMEN dump "$OUTPUT_DIR/long_jump.lc" > "$OUTPUT_DIR/long_jump_disasm.lasm" 2>/dev/null || fail "disassembly failed"
grep -q "LONG" "$OUTPUT_DIR/long_jump_disasm.lasm" || fail "missing LONG prefix in disassembly"
grep -q "JMP32" "$OUTPUT_DIR/long_jump_disasm.lasm" || fail "missing JMP32 instruction"
grep -qE "JMPIF32|JMPIFNOT32" "$OUTPUT_DIR/long_jump_disasm.lasm" || fail "missing JMPIF32/JMPIFNOT32 instruction"
grep -q "NEWFUNC" "$OUTPUT_DIR/long_jump_disasm.lasm" || fail "missing NEWFUNC instruction"
grep -q "target:" "$OUTPUT_DIR/long_jump_disasm.lasm" || fail "missing target information"
grep -q "offset:" "$OUTPUT_DIR/long_jump_disasm.lasm" || fail "missing offset information"
pass

# Test 25: Short jump out of range error
test_name "Error when short jump target exceeds 65535 instructions"
cat > "$OUTPUT_DIR/short_jump_overflow.lasm" <<'EOF'
.constants 1
  0: 1i

.func main 1 0
  JMP far_away
EOF
# Add enough NOPs to exceed the range of a 16-bit operand
yes "  NOP" | head -n 65600 >> "$OUTPUT_DIR/short_jump_overflow.lasm"
cat >> "$OUTPUT_DIR/short_jump_overflow.lasm" <<'EOF'
far_away:
  LOADK r0 0
  RET r0
.endfunc
EOF
$LUMEN assemble "$OUTPUT_DIR/short_jump_overflow.lasm" -o "$OUTPUT_DIR/overflow.lc" 2>&1 | grep -q "out of range" || fail "should error on short jump overflow"
pass

# Test 26: Execute new integer and bitwise ops
test_name "Execute new integer/bitwise ops"
cat > "$OUTPUT_DIR/new_int_ops.lasm" <<'EOF'
.constants 1
  0: 4.9

.func main 14 0
  LOADI r0 10
  LOADI r1 3
  MOD r2 r0 r1
  BAND r3 r0 r1
  BOR  r4 r0 r1
  BXOR r5 r0 r1
  SHL  r6 r1 r1
  SHR  r7 r6 r1
  NEG  r8 r1
  LOADBOOL r9 true
  NOT  r10 r9
  LOADK r11 0
  TOINT r12 r11
  TONUM r13 r12
  RET r2
.endfunc
EOF
$LUMEN assemble "$OUTPUT_DIR/new_int_ops.lasm" -o "$OUTPUT_DIR/new_int_ops.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/new_int_ops.lc" 2>&1)
echo "$result" | grep -q "Result: Int(1)" || fail "unexpected result from int ops: $result"
pass

# Test 27: Disassemble new integer/bitwise ops
test_name "Disassemble new integer/bitwise ops"
$LUMEN dump "$OUTPUT_DIR/new_int_ops.lc" > "$OUTPUT_DIR/new_int_ops_disasm.lasm" 2>/dev/null || fail "disassembly failed"
grep -q "LOADI" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing LOADI"
grep -q "MOD" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing MOD"
grep -q "BAND" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing BAND"
grep -q "BOR" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing BOR"
grep -q "BXOR" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing BXOR"
grep -q "SHL" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing SHL"
grep -q "SHR" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing SHR"
grep -q "NEG" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing NEG"
grep -q "NOT" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing NOT"
grep -q "TOINT" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing TOINT"
grep -q "TONUM" "$OUTPUT_DIR/new_int_ops_disasm.lasm" || fail "missing TONUM"
pass

# Test 29: Execute simple_closure.lasm
test_name "Execute simple_closure.lasm (closure support)"
$LUMEN assemble "$EXAMPLES_DIR/simple_closure.lasm" -o "$OUTPUT_DIR/simple_closure.lc" > /dev/null 2>&1 || fail "assembly failed"
[ -f "$OUTPUT_DIR/simple_closure.lc" ] || fail "bytecode file not created"
[ -s "$OUTPUT_DIR/simple_closure.lc" ] || fail "bytecode file is empty"
result=$($LUMEN run --result "$OUTPUT_DIR/simple_closure.lc" 2>&1)
echo "$result"
echo "$result" | grep -q "Result:" || fail "unexpected result: $result"
pass
test_name "Execute TOINT/TONUM conversion"
cat > "$OUTPUT_DIR/conversion_ops.lasm" <<'EOF'
.constants 1
  0: 4.9

.func main 3 0
  LOADK r0 0
  TOINT r1 r0
  TONUM r2 r1
  RET r2
.endfunc
EOF
$LUMEN assemble "$OUTPUT_DIR/conversion_ops.lasm" -o "$OUTPUT_DIR/conversion_ops.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN run --result "$OUTPUT_DIR/conversion_ops.lc" 2>&1)
echo "$result" | grep -q "Result: Number(4)" || fail "unexpected conversion result: $result"
pass

# Compile and run a Lumen source file
test_name "Run hello.lum from source"
result=$($LUMEN run "$EXAMPLES_DIR/hello.lum" 2>&1) || fail "run failed: $result"
[ "$result" = "Hello, Lumen!" ] || fail "unexpected output: $result"
pass

# Compile to bytecode, then execute it
test_name "Compile fibonacci.lum to bytecode and run it"
$LUMEN compile "$EXAMPLES_DIR/fibonacci.lum" -o "$OUTPUT_DIR/fibonacci.lc" > /dev/null 2>&1 || fail "compile failed"
result=$($LUMEN run --result "$OUTPUT_DIR/fibonacci.lc" 2>&1)
echo "$result" | grep -qx "fib(20) = 6765" || fail "unexpected output: $result"
pass

# Compile errors stop the run
test_name "Report a compile error and exit non-zero"
cat > "$OUTPUT_DIR/broken.lum" << 'EOF'
fn main() -> Int {
    return missing;
}
EOF
if result=$($LUMEN run "$OUTPUT_DIR/broken.lum" 2>&1); then
    fail "expected a non-zero exit code"
fi
echo "$result" | grep -q "E1001: undefined identifier 'missing'" || fail "unexpected diagnostic: $result"
pass

# Summary
echo "========================================"
echo -e "${GREEN}Tests passed: $TESTS_PASSED/$TESTS_RUN${NC}"
echo "========================================"

if [ $TESTS_PASSED -eq $TESTS_RUN ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some tests failed${NC}"
    exit 1
fi

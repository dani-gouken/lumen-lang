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
    GO_BIN="/usr/local/go/bin/go"
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
$LUMEN asm "$EXAMPLES_DIR/add.lasm" "$OUTPUT_DIR/add.lc" > /dev/null 2>&1 || fail "assembly failed"
[ -f "$OUTPUT_DIR/add.lc" ] || fail "bytecode file not created"
[ -s "$OUTPUT_DIR/add.lc" ] || fail "bytecode file is empty"
pass

# Test 2: Execute bytecode
test_name "Execute add.lc"
result=$($LUMEN exec "$OUTPUT_DIR/add.lc" 2>&1)
echo "$result" | grep -q "Result: Number(30)" || fail "unexpected result: $result"
pass

# Test 3: Disassemble bytecode
test_name "Disassemble add.lc"
$LUMEN disasm "$OUTPUT_DIR/add.lc" "$OUTPUT_DIR/add_disasm.lasm" > /dev/null 2>&1 || fail "disassembly failed"
[ -f "$OUTPUT_DIR/add_disasm.lasm" ] || fail "disassembled file not created"
grep -q "LOADK r0 0" "$OUTPUT_DIR/add_disasm.lasm" || fail "missing expected instruction"
grep -q "ADD r2 r0 r1" "$OUTPUT_DIR/add_disasm.lasm" || fail "missing ADD instruction"
pass

# Test 4: Round-trip verification
test_name "Round-trip: asm → binary → disasm → asm"
$LUMEN asm "$OUTPUT_DIR/add_disasm.lasm" "$OUTPUT_DIR/add_roundtrip.lc" > /dev/null 2>&1 || fail "re-assembly failed"
result1=$($LUMEN exec "$OUTPUT_DIR/add.lc" 2>&1)
result2=$($LUMEN exec "$OUTPUT_DIR/add_roundtrip.lc" 2>&1)
[ "$result1" = "$result2" ] || fail "round-trip produced different results"
pass

# Test 5: Execute complex example
test_name "Execute complex.lasm"
$LUMEN asm "$EXAMPLES_DIR/complex.lasm" "$OUTPUT_DIR/complex.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN exec "$OUTPUT_DIR/complex.lc" 2>&1)
echo "$result" | grep -q "Result: Int(23)" || fail "unexpected result: $result"
pass

# Test 6: Execute types example
test_name "Execute types.lasm"
$LUMEN asm "$EXAMPLES_DIR/types.lasm" "$OUTPUT_DIR/types.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN exec "$OUTPUT_DIR/types.lc" 2>&1)
echo "$result" | grep -q "Result: Number(42.5)" || fail "unexpected result: $result"
pass

# Test 7: Error handling - invalid bytecode
test_name "Error on invalid bytecode"
echo "invalid" > "$OUTPUT_DIR/invalid.lc"
$LUMEN exec "$OUTPUT_DIR/invalid.lc" 2>&1 | grep -q "error" || fail "should error on invalid bytecode"
pass

# Test 8: Error handling - invalid assembly
test_name "Error on invalid assembly syntax"
echo "INVALID SYNTAX" > "$OUTPUT_DIR/invalid.lasm"
$LUMEN asm "$OUTPUT_DIR/invalid.lasm" "$OUTPUT_DIR/out.lc" 2>&1 | grep -q "error" || fail "should error on invalid syntax"
pass

# Test 9: Disassemble preserves value types
test_name "Disassemble preserves value types"
$LUMEN disasm "$OUTPUT_DIR/complex.lc" "$OUTPUT_DIR/complex_disasm.lasm" > /dev/null 2>&1 || fail "disassembly failed"
grep -q "5i" "$OUTPUT_DIR/complex_disasm.lasm" || fail "integer suffix not preserved"
grep -q "MUL r2 r0 r1" "$OUTPUT_DIR/complex_disasm.lasm" || fail "MUL instruction missing"
pass

# Test 10: Array operations
test_name "Execute array_example.lasm"
$LUMEN asm "$EXAMPLES_DIR/array_example.lasm" "$OUTPUT_DIR/array.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN exec "$OUTPUT_DIR/array.lc" 2>&1)
echo "$result" | grep -q "Result: Int(20)" || fail "unexpected result: $result"
pass

# Test 11: Disassemble array bytecode
test_name "Disassemble array bytecode with memory opcodes"
$LUMEN disasm "$OUTPUT_DIR/array.lc" "$OUTPUT_DIR/array_disasm.lasm" > /dev/null 2>&1 || fail "disassembly failed"
grep -q "NEWARRAY r0 5" "$OUTPUT_DIR/array_disasm.lasm" || fail "missing NEWARRAY instruction"
grep -q "SETINDEX" "$OUTPUT_DIR/array_disasm.lasm" || fail "missing SETINDEX instruction"
grep -q "GETINDEX" "$OUTPUT_DIR/array_disasm.lasm" || fail "missing GETINDEX instruction"
grep -q "ARRLEN" "$OUTPUT_DIR/array_disasm.lasm" || fail "missing ARRLEN instruction"
pass

# Test 12: Object operations
test_name "Execute object_example.lasm"
$LUMEN asm "$EXAMPLES_DIR/object_example.lasm" "$OUTPUT_DIR/object.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN exec "$OUTPUT_DIR/object.lc" 2>&1)
echo "$result" | grep -q "Result: Int(25)" || fail "unexpected result: $result"
pass

# Test 13: Nested structures
test_name "Execute nested_structures.lasm"
$LUMEN asm "$EXAMPLES_DIR/nested_structures.lasm" "$OUTPUT_DIR/nested.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN exec "$OUTPUT_DIR/nested.lc" 2>&1)
echo "$result" | grep -q "Result: Int(100)" || fail "unexpected result: $result"
pass

# Test 14: Garbage collection
test_name "Execute gc_demo.lasm"
$LUMEN asm "$EXAMPLES_DIR/gc_demo.lasm" "$OUTPUT_DIR/gc.lc" > /dev/null 2>&1 || fail "assembly failed"
result=$($LUMEN exec "$OUTPUT_DIR/gc.lc" 2>&1)
echo "$result" | grep -q "Result: Int(42)" || fail "unexpected result: $result"
pass

# Test 15: Disassemble GC bytecode
test_name "Disassemble GC bytecode"
$LUMEN disasm "$OUTPUT_DIR/gc.lc" "$OUTPUT_DIR/gc_disasm.lasm" > /dev/null 2>&1 || fail "disassembly failed"
grep -q "NEWARRAY" "$OUTPUT_DIR/gc_disasm.lasm" || fail "missing NEWARRAY instruction"
grep -q "NEWOBJ" "$OUTPUT_DIR/gc_disasm.lasm" || fail "missing NEWOBJ instruction"
grep -q ": GC$" "$OUTPUT_DIR/gc_disasm.lasm" || fail "missing GC instruction"
pass

# Test 16: Memory round-trip
test_name "Round-trip memory opcodes: asm → binary → disasm → asm"
$LUMEN disasm "$OUTPUT_DIR/nested.lc" "$OUTPUT_DIR/nested_disasm.lasm" > /dev/null 2>&1 || fail "disassembly failed"
$LUMEN asm "$OUTPUT_DIR/nested_disasm.lasm" "$OUTPUT_DIR/nested_roundtrip.lc" > /dev/null 2>&1 || fail "re-assembly failed"
result1=$($LUMEN exec "$OUTPUT_DIR/nested.lc" 2>&1)
result2=$($LUMEN exec "$OUTPUT_DIR/nested_roundtrip.lc" 2>&1)
[ "$result1" = "$result2" ] || fail "round-trip produced different results"
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

package asm

import (
	"testing"

	"github.com/yourname/lumen/internal/vm"
)

func TestAssembleFunctionCall(t *testing.T) {
	source := `.constants 2
  0: 5i
  1: 10i

.code
  LOADK r0 0
  LOADK r1 1
  CALL add_func 2
  RET r0

add_func:
  ADD r2 r0 r1
  RET r2

.metadata
  registers: 3
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Execute the code
	vm := vm.NewVM()
	result, err := vm.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if !result.IsInt() {
		t.Fatalf("expected int result, got %s", result.String())
	}

	expected := int64(15)
	if result.AsInt() != expected {
		t.Errorf("expected %d, got %d", expected, result.AsInt())
	}
}

func TestAssembleRecursion(t *testing.T) {
	source := `.constants 3
  0: 5i
  1: 1i
  2: 0i

.code
  LOADK r0 0
  CALL factorial 1
  RET r0

factorial:
  LOADK r1 2
  LE r3 r0 r1
  JMPIF r3 base_case

  MOV r4 r0
  LOADK r1 1
  SUB r0 r4 r1
  CALL factorial 1
  MUL r2 r4 r0
  RET r2

base_case:
  LOADK r2 1
  RET r2

.metadata
  registers: 5
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Execute the code
	vm := vm.NewVM()
	result, err := vm.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if !result.IsInt() {
		t.Fatalf("expected int result, got %s", result.String())
	}

	// factorial(5) = 120
	expected := int64(120)
	if result.AsInt() != expected {
		t.Errorf("expected %d, got %d", expected, result.AsInt())
	}
}

func TestAssembleExternCall(t *testing.T) {
	source := `.constants 1
  0: 42i

.code
  LOADK r0 0
  CALLEXTERN println 1
  LOADK r0 0
  RET r0

.metadata
  registers: 1
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Execute the code (this will print to stdout)
	vm := vm.NewVM()
	result, err := vm.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if !result.IsInt() {
		t.Fatalf("expected int result, got %s", result.String())
	}

	expected := int64(42)
	if result.AsInt() != expected {
		t.Errorf("expected %d, got %d", expected, result.AsInt())
	}
}

func TestAssembleLabels(t *testing.T) {
	source := `.constants 2
  0: 1i
  1: 2i

.code
  LOADK r0 0
  JMP skip_this

  LOADK r0 1

skip_this:
  RET r0

.metadata
  registers: 1
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Execute the code
	vm := vm.NewVM()
	result, err := vm.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if !result.IsInt() {
		t.Fatalf("expected int result, got %s", result.String())
	}

	// Should be 1, not 2, because we jumped over the second LOADK
	expected := int64(1)
	if result.AsInt() != expected {
		t.Errorf("expected %d, got %d", expected, result.AsInt())
	}
}

func TestAssembleNestedCalls(t *testing.T) {
	source := `.constants 3
  0: 2i
  1: 3i
  2: 4i

.code
  LOADK r0 0
  CALL double 1
  CALL double 1
  RET r0

double:
  ADD r1 r0 r0
  MOV r0 r1
  RET r0

.metadata
  registers: 2
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Execute the code
	vm := vm.NewVM()
	result, err := vm.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if !result.IsInt() {
		t.Fatalf("expected int result, got %s", result.String())
	}

	// double(double(2)) = double(4) = 8
	expected := int64(8)
	if result.AsInt() != expected {
		t.Errorf("expected %d, got %d", expected, result.AsInt())
	}
}

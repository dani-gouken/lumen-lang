package asm

import (
	"testing"

	"github.com/yourname/lumen/internal/vm"
)

func TestAssembleFunctionCall(t *testing.T) {
	source := `.constants 2
  0: 5i
  1: 10i

.func add_func 3 2
  ADD r2 r0 r1
  RET r2
.endfunc

.func main 4 0
  NEWFUNC r0 add_func
  LOADK r1 0
  LOADK r2 1
  CALL r0 2
  RET r0
.endfunc
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Execute the code
	v := vm.NewVM()
	result, err := v.Exec(chunk)
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

.func factorial 5 1
  LOADK r1 2
  LE r3 r0 r1
  JMPIF r3 base_case

  MOV r4 r0
  LOADK r1 1
  SUB r1 r4 r1
  NEWFUNC r0 factorial
  CALL r0 1
  MUL r2 r4 r0
  RET r2

base_case:
  LOADK r2 1
  RET r2
.endfunc

.func main 2 0
  NEWFUNC r0 factorial
  LOADK r1 0
  CALL r0 1
  RET r0
.endfunc
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Execute the code
	v := vm.NewVM()
	result, err := v.Exec(chunk)
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

.func main 2 0
  NEWEXFUNC r0 println
  LOADK r1 0
  CALL r0 1
  LOADK r0 0
  RET r0

.endfunc
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

.func main 1 0
  LOADK r0 0
  JMP skip_this

  LOADK r0 1

skip_this:
  RET r0

.endfunc
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

.func double 2 1
  ADD r1 r0 r0
  MOV r0 r1
  RET r0
.endfunc

.func main 3 0
  NEWFUNC r0 double
  LOADK r1 0
  CALL r0 1
  NEWFUNC r1 double
  MOV r2 r0
  CALL r1 1
  RET r0
.endfunc
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Execute the code
	v := vm.NewVM()
	result, err := v.Exec(chunk)
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

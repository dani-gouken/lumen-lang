package asm

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/vm"
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

// A handler left with TRY.L takes over when something fails in the call
// or in what it calls; RETHROW passes the failure on, ENDTRY takes the
// handler back.
func TestAssembleHandlers(t *testing.T) {
	source := `.constants 3
  0: 0i
  1: 1i
  2: 10i

.func fails 3 0
  LOADK r0 1
  LOADK r1 0
  DIV r2 r0 r1
  RET r2
.endfunc

.func cleans 3 0
  LOADK r1 0
  TRY.L on_failure
  NEWFUNC r0 fails
  CALL r0 0
  ENDTRY
  RET r0
on_failure:
  LOADK r1 2
  RETHROW
.endfunc

.func main 3 0
  LOADK r1 1
  TRY.L recovered
  LOADK r2 2
  ENDTRY
  ADD r1 r1 r2
  RET r1
recovered:
  RETHROW
.endfunc
`
	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}
	result, err := vm.NewVM().Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 11 {
		t.Errorf("expected 11 from the path that does not fail, got %s", result.String())
	}

	// The same program calling the function that fails: its caller's
	// handler runs, then the failure leaves the program as it would have
	failing := strings.Replace(source, "  LOADK r2 2\n  ENDTRY", "  NEWFUNC r0 cleans\n  CALL r0 0\n  ENDTRY", 1)
	chunk, err = Assemble(failing)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}
	_, err = vm.NewVM().Exec(chunk)
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("expected the division by zero to be passed on, got %v", err)
	}

	listing := Disassemble(chunk)
	for _, want := range []string{"TRY32", "ENDTRY", "RETHROW"} {
		if !strings.Contains(listing, want) {
			t.Errorf("expected %s in the listing:\n%s", want, listing)
		}
	}

	for _, bad := range []string{"TRY on_failure", "TRY.L nowhere", "ENDTRY r1"} {
		broken := strings.Replace(source, "  ENDTRY\n  RET r0", "  "+bad+"\n  RET r0", 1)
		if _, err := Assemble(broken); err == nil {
			t.Errorf("expected %q to be refused", bad)
		}
	}
}

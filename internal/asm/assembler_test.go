package asm

import (
	"strings"
	"testing"

	"github.com/yourname/lumen/internal/vm"
)

func TestAssembleSimpleProgram(t *testing.T) {
	source := `.constants 2
  0: 10i
  1: 20i

.code
  LOADK r0 0
  LOADK r1 1
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

	if len(chunk.Constants) != 2 {
		t.Errorf("expected 2 constants, got %d", len(chunk.Constants))
	}
	if len(chunk.Code) != 4 {
		t.Errorf("expected 4 instructions, got %d", len(chunk.Code))
	}
	if chunk.RegisterCount != 3 {
		t.Errorf("expected 3 registers, got %d", chunk.RegisterCount)
	}
	if chunk.ArgCount != 0 {
		t.Errorf("expected 0 args, got %d", chunk.ArgCount)
	}
}

func TestAssembleValueTypes(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		check    func(vm.Value) bool
	}{
		{"number", "42.5", func(v vm.Value) bool { return v.IsNumber() && v.AsNumber() == 42.5 }},
		{"integer", "100i", func(v vm.Value) bool { return v.IsInt() && v.AsInt() == 100 }},
		{"bool true", "true", func(v vm.Value) bool { return v.IsBool() && v.AsBool() }},
		{"bool false", "false", func(v vm.Value) bool { return v.IsBool() && !v.AsBool() }},
		{"null", "null", func(v vm.Value) bool { return v.IsNull() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := `.constants 1
  0: ` + tt.constant + `

.code
  NOP

.metadata
  registers: 0
  args: 0
`
			chunk, err := Assemble(source)
			if err != nil {
				t.Fatalf("assembly failed: %v", err)
			}

			if len(chunk.Constants) != 1 {
				t.Fatalf("expected 1 constant, got %d", len(chunk.Constants))
			}

			if !tt.check(chunk.Constants[0]) {
				t.Errorf("constant check failed for %s: got %s", tt.name, chunk.Constants[0].String())
			}
		})
	}
}

func TestAssembleAllInstructions(t *testing.T) {
	source := `.constants 2
  0: 10i
  1: 5i

.code
  NOP
  MOV r1 r0
  LOADK r0 0
  LOADNULL r2
  LOADBOOL r3 true
  ADD r4 r0 r1
  SUB r5 r0 r1
  MUL r6 r0 r1
  DIV r7 r0 r1
  EQ r8 r0 r1
  NE r9 r0 r1
  LT r10 r0 r1
  LE r11 r0 r1
  GT r12 r0 r1
  GE r13 r0 r1
  JMP 10
  JMPIF r0 5
  JMPIFNOT r0 5
  CALL r0 2
  RET r0

.metadata
  registers: 14
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	expectedOps := []vm.OpCode{
		vm.OpNOP, vm.OpMOV, vm.OpLOADK, vm.OpLOADNULL, vm.OpLOADBOOL,
		vm.OpADD, vm.OpSUB, vm.OpMUL, vm.OpDIV,
		vm.OpEQ, vm.OpNE, vm.OpLT, vm.OpLE, vm.OpGT, vm.OpGE,
		vm.OpJMP, vm.OpJMPIF, vm.OpJMPIFNOT, vm.OpCALL, vm.OpRET,
	}

	if len(chunk.Code) != len(expectedOps) {
		t.Fatalf("expected %d instructions, got %d", len(expectedOps), len(chunk.Code))
	}

	for i, expectedOp := range expectedOps {
		if chunk.Code[i].Op != expectedOp {
			t.Errorf("instruction %d: expected %v, got %v", i, expectedOp, chunk.Code[i].Op)
		}
	}
}

func TestAssembleWithComments(t *testing.T) {
	source := `.constants 1
  0: 42i  ; the answer

.code
  LOADK r0 0  ; load the answer
  RET r0      ; return it

.metadata
  registers: 1  ; only need one
  args: 0       ; no arguments
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly with comments failed: %v", err)
	}

	if len(chunk.Code) != 2 {
		t.Errorf("expected 2 instructions, got %d", len(chunk.Code))
	}
}

func TestAssembleEmptyLines(t *testing.T) {
	source := `.constants 1
  0: 1i


.code

  LOADK r0 0

  RET r0


.metadata
  registers: 1
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly with empty lines failed: %v", err)
	}

	if len(chunk.Code) != 2 {
		t.Errorf("expected 2 instructions, got %d", len(chunk.Code))
	}
}

func TestAssembleErrorInvalidInstruction(t *testing.T) {
	source := `.constants 0

.code
  INVALID r0 r1

.metadata
  registers: 1
  args: 0
`

	_, err := Assemble(source)
	if err == nil {
		t.Fatal("expected error for invalid instruction")
	}
	if !strings.Contains(err.Error(), "unknown instruction") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAssembleErrorInvalidRegister(t *testing.T) {
	source := `.constants 0

.code
  LOADNULL x0

.metadata
  registers: 1
  args: 0
`

	_, err := Assemble(source)
	if err == nil {
		t.Fatal("expected error for invalid register")
	}
}

func TestAssembleErrorRegisterOutOfRange(t *testing.T) {
	source := `.constants 0

.code
  LOADNULL r256

.metadata
  registers: 1
  args: 0
`

	_, err := Assemble(source)
	if err == nil {
		t.Fatal("expected error for register out of range")
	}
}

func TestAssembleErrorInvalidValue(t *testing.T) {
	source := `.constants 1
  0: not_a_value

.code
  NOP

.metadata
  registers: 0
  args: 0
`

	_, err := Assemble(source)
	if err == nil {
		t.Fatal("expected error for invalid value")
	}
}

func TestAssembleErrorWrongArgumentCount(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"MOV too few", "MOV r0"},
		{"ADD too few", "ADD r0 r1"},
		{"RET too many", "RET r0 r1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := `.constants 0

.code
  ` + tt.source + `

.metadata
  registers: 3
  args: 0
`
			_, err := Assemble(source)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

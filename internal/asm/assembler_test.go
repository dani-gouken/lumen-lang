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

.func main 3 0
  LOADK r0 0
  LOADK r1 1
  ADD r2 r0 r1
  RET r2
.endfunc
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
	if len(chunk.Prototypes) != 1 {
		t.Fatalf("expected 1 prototype, got %d", len(chunk.Prototypes))
	}
	proto := chunk.Prototypes[0]
	if proto.RegisterCount != 3 {
		t.Errorf("expected 3 registers, got %d", proto.RegisterCount)
	}
	if proto.ArgCount != 0 {
		t.Errorf("expected 0 args, got %d", proto.ArgCount)
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

.func main 0 0
  NOP
.endfunc
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

func TestAssembleStringLiterals(t *testing.T) {
	tests := []struct {
		name           string
		constant       string
		expectedString string
	}{
		{"simple string", `"Hello, World!"`, "Hello, World!"},
		{"empty string", `""`, ""},
		{"string with newline", `"Line1\nLine2"`, "Line1\nLine2"},
		{"string with tab", `"Tab\there"`, "Tab\there"},
		{"string with escape", `"Quote: \"Hello\""`, `Quote: "Hello"`},
		{"string with backslash", `"Path\\to\\file"`, `Path\to\file`},
		{"string with all escapes", `"Test:\n\t\r\\\""`, "Test:\n\t\r\\\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := `.constants 1
  0: ` + tt.constant + `

.func main 1 0
  NEWSTR r0 0
  RET r0
.endfunc
`
			chunk, err := Assemble(source)
			if err != nil {
				t.Fatalf("assembly failed: %v", err)
			}

			// Check that the constant is an integer (string pool index)
			if !chunk.Constants[0].IsInt() {
				t.Fatalf("expected constant to be Int (string pool index), got %s", chunk.Constants[0].String())
			}

			// Check that the string was added to the string pool
			if len(chunk.StringPool) != 1 {
				t.Fatalf("expected 1 string in pool, got %d", len(chunk.StringPool))
			}

			if chunk.StringPool[0] != tt.expectedString {
				t.Errorf("expected string %q, got %q", tt.expectedString, chunk.StringPool[0])
			}

			// Check that constant points to correct string pool index
			strIdx := int(chunk.Constants[0].AsInt())
			if strIdx != 0 {
				t.Errorf("expected string pool index 0, got %d", strIdx)
			}
		})
	}
}

func TestAssembleMultipleStrings(t *testing.T) {
	source := `.constants 3
  0: "First"
  1: "Second"
  2: "Third"

.func main 3 0
  NEWSTR r0 0
  NEWSTR r1 1
  NEWSTR r2 2
  RET r0
.endfunc
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Should have 3 strings in the pool
	if len(chunk.StringPool) != 3 {
		t.Fatalf("expected 3 strings in pool, got %d", len(chunk.StringPool))
	}

	expectedStrings := []string{"First", "Second", "Third"}
	for i, expected := range expectedStrings {
		if chunk.StringPool[i] != expected {
			t.Errorf("string %d: expected %q, got %q", i, expected, chunk.StringPool[i])
		}
	}

	// Check that each constant points to the correct index
	for i := 0; i < 3; i++ {
		if !chunk.Constants[i].IsInt() {
			t.Errorf("constant %d: expected Int, got %s", i, chunk.Constants[i].String())
			continue
		}
		strIdx := int(chunk.Constants[i].AsInt())
		if strIdx != i {
			t.Errorf("constant %d: expected string pool index %d, got %d", i, i, strIdx)
		}
	}
}

func TestAssembleAllInstructions(t *testing.T) {
	source := `.constants 2
  0: 10i
  1: 5i

.func main 18 0
  NOP
  MOV r1 r0
  LOADK r0 0
  LOADNULL r2
  LOADBOOL r3 true
  LOADI r4 1
  ADD r4 r0 r1
  SUB r5 r0 r1
  MUL r6 r0 r1
  DIV r7 r0 r1
  MOD r8 r0 r1
  NEG r9 r0
  NOT r10 r3
  TOINT r11 r0
  TONUM r12 r0
  BAND r13 r0 r1
  BOR r14 r0 r1
  BXOR r15 r0 r1
  SHL r16 r0 r4
  SHR r17 r0 r4
  EQ r8 r0 r1
  NE r9 r0 r1
  LT r10 r0 r1
  LE r11 r0 r1
  GT r12 r0 r1
  GE r13 r0 r1
  ARRPUSH r0 r4
  HASFIELD r1 r0 0
  DELFIELD r2 r0 1
  JMP 10
  JMPIF r0 5
  JMPIFNOT r0 5
  CALL r0 2
  RET r0
.endfunc
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	expectedOps := []vm.OpCode{
		vm.OpNOP, vm.OpMOV, vm.OpLOADK, vm.OpLOADNULL, vm.OpLOADBOOL, vm.OpLOADI,
		vm.OpADD, vm.OpSUB, vm.OpMUL, vm.OpDIV, vm.OpMOD, vm.OpNEG, vm.OpNOT, vm.OpTOINT, vm.OpTONUM,
		vm.OpBAND, vm.OpBOR, vm.OpBXOR, vm.OpSHL, vm.OpSHR,
		vm.OpEQ, vm.OpNE, vm.OpLT, vm.OpLE, vm.OpGT, vm.OpGE,
		vm.OpARRPUSH, vm.OpHASFIELD, vm.OpDELFIELD,
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

.func main 1 0  ; main function
  LOADK r0 0  ; load the answer
  RET r0      ; return it
.endfunc
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


.func main 1 0
  LOADK r0 0

  RET r0
.endfunc
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

.func main 1 0
  INVALID r0 r1
.endfunc
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

.func main 1 0
  LOADNULL x0
.endfunc
`

	_, err := Assemble(source)
	if err == nil {
		t.Fatal("expected error for invalid register")
	}
}

func TestAssembleErrorRegisterOutOfRange(t *testing.T) {
	source := `.constants 0

.func main 1 0
  LOADNULL r256
.endfunc
`

	_, err := Assemble(source)
	if err == nil {
		t.Fatal("expected error for register out of range")
	}
}

func TestAssembleErrorInvalidValue(t *testing.T) {
	source := `.constants 1
  0: not_a_value

.func main 0 0
  NOP
.endfunc
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

.func main 3 0
  ` + tt.source + `
.endfunc
`
			_, err := Assemble(source)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

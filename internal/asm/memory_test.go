package asm

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/vm"
)

func TestAssembleMemoryOpcodes(t *testing.T) {
	tests := []struct {
		name string
		code string
		want vm.OpCode
	}{
		{"NEWSTR", "NEWSTR r0 1", vm.OpNEWSTR},
		{"NEWARRAY", "NEWARRAY r1 10", vm.OpNEWARRAY},
		{"NEWOBJ", "NEWOBJ r2", vm.OpNEWOBJ},
		{"GETINDEX", "GETINDEX r0 r1 r2", vm.OpGETINDEX},
		{"SETINDEX", "SETINDEX r0 r1 r2", vm.OpSETINDEX},
		{"GETFIELD", "GETFIELD r0 r1 2", vm.OpGETFIELD},
		{"SETFIELD", "SETFIELD r0 3 r1", vm.OpSETFIELD},
		{"ARRLEN", "ARRLEN r0 r1", vm.OpARRLEN},
		{"GC", "GC", vm.OpGC},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := ".func main 1 0\n" + tt.code + "\n.endfunc"
			chunk, err := Assemble(source)
			if err != nil {
				t.Fatalf("Assemble failed: %v", err)
			}

			if len(chunk.Code) != 1 {
				t.Fatalf("Expected 1 instruction, got %d", len(chunk.Code))
			}

			if chunk.Code[0].Op != tt.want {
				t.Errorf("Expected opcode %v, got %v", tt.want, chunk.Code[0].Op)
			}
		})
	}
}

func TestAssembleArrayProgram(t *testing.T) {
	source := `
.constants 3
0: 10i
1: 0i
2: 20i

.func main 4 0
NEWARRAY r0 5
LOADK r1 0
LOADK r2 1
SETINDEX r0 r2 r1
LOADK r1 2
GETINDEX r3 r0 r2
RET r3
.endfunc
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	// Check constants
	if len(chunk.Constants) != 3 {
		t.Errorf("Expected 3 constants, got %d", len(chunk.Constants))
	}

	// Check code
	expected := []vm.OpCode{
		vm.OpNEWARRAY,
		vm.OpLOADK,
		vm.OpLOADK,
		vm.OpSETINDEX,
		vm.OpLOADK,
		vm.OpGETINDEX,
		vm.OpRET,
	}

	if len(chunk.Code) != len(expected) {
		t.Fatalf("Expected %d instructions, got %d", len(expected), len(chunk.Code))
	}

	for i, op := range expected {
		if chunk.Code[i].Op != op {
			t.Errorf("Instruction %d: expected %v, got %v", i, op, chunk.Code[i].Op)
		}
	}

	// Check metadata
	if len(chunk.Prototypes) == 0 || chunk.Prototypes[0].RegisterCount != 4 {
		regCount := 0
		if len(chunk.Prototypes) > 0 {
			regCount = chunk.Prototypes[0].RegisterCount
		}
		t.Errorf("Expected 4 registers, got %d", regCount)
	}
}

func TestAssembleObjectProgram(t *testing.T) {
	source := `
.func main 3 0
NEWOBJ r0
LOADK r1 0
SETFIELD r0 1 r1
GETFIELD r2 r0 1
RET r2
.endfunc
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	expected := []vm.OpCode{
		vm.OpNEWOBJ,
		vm.OpLOADK,
		vm.OpSETFIELD,
		vm.OpGETFIELD,
		vm.OpRET,
	}

	if len(chunk.Code) != len(expected) {
		t.Fatalf("Expected %d instructions, got %d", len(expected), len(chunk.Code))
	}

	for i, op := range expected {
		if chunk.Code[i].Op != op {
			t.Errorf("Instruction %d: expected %v, got %v", i, op, chunk.Code[i].Op)
		}
	}
}

func TestDisassembleMemoryOpcodes(t *testing.T) {
	chunk := vm.NewChunk()
	// Add memory opcodes
	codeStart := len(chunk.Code)
	chunk.AddInstruction(vm.NewInstr(vm.OpNEWSTR, 0, 1, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpNEWARRAY, 1, 10, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpNEWOBJ, 2, 0, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpGETINDEX, 0, 1, 2))
	chunk.AddInstruction(vm.NewInstr(vm.OpSETINDEX, 0, 1, 2))
	chunk.AddInstruction(vm.NewInstr(vm.OpGETFIELD, 0, 1, 2))
	chunk.AddInstruction(vm.NewInstr(vm.OpSETFIELD, 0, 3, 1))
	chunk.AddInstruction(vm.NewInstr(vm.OpARRLEN, 0, 1, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpGC, 0, 0, 0))
	codeSize := len(chunk.Code) - codeStart

	mainProto := vm.FunctionPrototype{
		Name:          "main",
		CodeStart:     codeStart,
		CodeSize:      codeSize,
		RegisterCount: 3,
		ArgCount:      0,
	}
	chunk.Prototypes = append(chunk.Prototypes, mainProto)

	output := Disassemble(chunk)

	expectedOpcodes := []string{
		"NEWSTR r0 1",
		"NEWARRAY r1 10",
		"NEWOBJ r2",
		"GETINDEX r0 r1 r2",
		"SETINDEX r0 r1 r2",
		"GETFIELD r0 r1 2",
		"SETFIELD r0 3 r1",
		"ARRLEN r0 r1",
		"GC",
	}

	for _, expected := range expectedOpcodes {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain '%s', got:\n%s", expected, output)
		}
	}
}

func TestRoundTripMemoryProgram(t *testing.T) {
	source := `.constants 2
0: 100i
1: 0i

.func main 5 0
  NEWARRAY r0 5
  LOADK r1 0
  LOADK r2 1
  SETINDEX r0 r2 r1
  GETINDEX r3 r0 r2
  ARRLEN r4 r0
  GC
  RET r3
.endfunc
`

	// Assemble
	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	// Disassemble
	output := Disassemble(chunk)

	// Reassemble
	chunk2, err := Assemble(output)
	if err != nil {
		t.Fatalf("Reassemble failed: %v", err)
	}

	// Compare
	if len(chunk.Code) != len(chunk2.Code) {
		t.Fatalf("Code length mismatch: %d != %d", len(chunk.Code), len(chunk2.Code))
	}

	for i := range chunk.Code {
		if chunk.Code[i] != chunk2.Code[i] {
			t.Errorf("Instruction %d mismatch: %v != %v", i, chunk.Code[i], chunk2.Code[i])
		}
	}

	if len(chunk.Prototypes) != len(chunk2.Prototypes) {
		t.Errorf("Prototypes length mismatch: %d != %d", len(chunk.Prototypes), len(chunk2.Prototypes))
	} else if len(chunk.Prototypes) > 0 {
		if chunk.Prototypes[0].RegisterCount != chunk2.Prototypes[0].RegisterCount {
			t.Errorf("RegisterCount mismatch: %d != %d", chunk.Prototypes[0].RegisterCount, chunk2.Prototypes[0].RegisterCount)
		}
	}
}

func TestAssembleInvalidMemoryOpcodes(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"NEWSTR wrong args", ".func main 1 0\nNEWSTR r0\n.endfunc"},
		{"NEWARRAY wrong args", ".func main 1 0\nNEWARRAY r0\n.endfunc"},
		{"NEWOBJ wrong args", ".func main 2 0\nNEWOBJ r0 r1\n.endfunc"},
		{"GETINDEX wrong args", ".func main 2 0\nGETINDEX r0 r1\n.endfunc"},
		{"SETINDEX wrong args", ".func main 1 0\nSETINDEX r0\n.endfunc"},
		{"GETFIELD wrong args", ".func main 1 0\nGETFIELD r0\n.endfunc"},
		{"SETFIELD wrong args", ".func main 2 0\nSETFIELD r0 r1\n.endfunc"},
		{"ARRLEN wrong args", ".func main 1 0\nARRLEN r0\n.endfunc"},
		{"GC wrong args", ".func main 1 0\nGC r0\n.endfunc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Assemble(tt.source)
			if err == nil {
				t.Error("Expected error for invalid instruction, got nil")
			}
		})
	}
}

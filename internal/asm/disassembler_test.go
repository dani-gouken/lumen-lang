package asm

import (
	"strings"
	"testing"

	"github.com/yourname/lumen/internal/vm"
)

func TestDisassembleSimpleChunk(t *testing.T) {
	chunk := vm.NewChunk()
	chunk.AddConstant(vm.NewInt(42))
	chunk.AddInstruction(vm.NewInstr(vm.OpLOADK, 0, 0, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpRET, 0, 0, 0))
	chunk.RegisterCount = 1
	chunk.ArgCount = 0

	output := Disassemble(chunk)

	// Check for expected sections
	if !strings.Contains(output, ".constants") {
		t.Error("output missing .constants section")
	}
	if !strings.Contains(output, ".code") {
		t.Error("output missing .code section")
	}
	if !strings.Contains(output, ".metadata") {
		t.Error("output missing .metadata section")
	}

	// Check for instructions
	if !strings.Contains(output, "LOADK r0 0") {
		t.Error("output missing LOADK instruction")
	}
	if !strings.Contains(output, "RET r0") {
		t.Error("output missing RET instruction")
	}

	// Check metadata
	if !strings.Contains(output, "registers: 1") {
		t.Error("output missing register count")
	}
	if !strings.Contains(output, "args: 0") {
		t.Error("output missing arg count")
	}
}

func TestDisassembleAllValueTypes(t *testing.T) {
	chunk := vm.NewChunk()
	chunk.AddConstant(vm.NewNumber(42.5))
	chunk.AddConstant(vm.NewInt(100))
	chunk.AddConstant(vm.NewBool(true))
	chunk.AddConstant(vm.NewBool(false))
	chunk.AddConstant(vm.NewNull())
	chunk.RegisterCount = 0
	chunk.ArgCount = 0

	output := Disassemble(chunk)

	// Check value formatting
	if !strings.Contains(output, "42.5") {
		t.Error("missing number value")
	}
	if !strings.Contains(output, "100i") {
		t.Error("missing integer value")
	}
	if !strings.Contains(output, "true") {
		t.Error("missing true value")
	}
	if !strings.Contains(output, "false") {
		t.Error("missing false value")
	}
	if !strings.Contains(output, "null") {
		t.Error("missing null value")
	}
}

func TestDisassembleAllInstructions(t *testing.T) {
	chunk := vm.NewChunk()
	chunk.AddConstant(vm.NewInt(1))

	instructions := []vm.Instr{
		vm.NewInstr(vm.OpNOP, 0, 0, 0),
		vm.NewInstr(vm.OpMOV, 1, 0, 0),
		vm.NewInstr(vm.OpLOADK, 0, 0, 0),
		vm.NewInstr(vm.OpLOADNULL, 0, 0, 0),
		vm.NewInstr(vm.OpLOADBOOL, 0, 1, 0),
		vm.NewInstr(vm.OpLOADI, 0, 1, 0),
		vm.NewInstr(vm.OpADD, 2, 0, 1),
		vm.NewInstr(vm.OpSUB, 2, 0, 1),
		vm.NewInstr(vm.OpMUL, 2, 0, 1),
		vm.NewInstr(vm.OpDIV, 2, 0, 1),
		vm.NewInstr(vm.OpMOD, 2, 0, 1),
		vm.NewInstr(vm.OpNEG, 2, 0, 0),
		vm.NewInstr(vm.OpNOT, 2, 0, 0),
		vm.NewInstr(vm.OpTOINT, 2, 0, 0),
		vm.NewInstr(vm.OpTONUM, 2, 0, 0),
		vm.NewInstr(vm.OpBAND, 2, 0, 1),
		vm.NewInstr(vm.OpBOR, 2, 0, 1),
		vm.NewInstr(vm.OpBXOR, 2, 0, 1),
		vm.NewInstr(vm.OpSHL, 2, 0, 1),
		vm.NewInstr(vm.OpSHR, 2, 0, 1),
		vm.NewInstr(vm.OpEQ, 2, 0, 1),
		vm.NewInstr(vm.OpNE, 2, 0, 1),
		vm.NewInstr(vm.OpLT, 2, 0, 1),
		vm.NewInstr(vm.OpLE, 2, 0, 1),
		vm.NewInstr(vm.OpGT, 2, 0, 1),
		vm.NewInstr(vm.OpGE, 2, 0, 1),
		vm.NewInstr(vm.OpARRPUSH, 0, 1, 0),
		vm.NewInstr(vm.OpHASFIELD, 0, 1, 0),
		vm.NewInstr(vm.OpDELFIELD, 0, 1, 0),
		vm.NewInstr(vm.OpJMP, 5, 0, 0),
		vm.NewInstr(vm.OpJMPIF, 0, 5, 0),
		vm.NewInstr(vm.OpJMPIFNOT, 0, 5, 0),
		vm.NewInstr(vm.OpCALL, 0, 2, 0),
		vm.NewInstr(vm.OpRET, 0, 0, 0),
	}

	for _, instr := range instructions {
		chunk.AddInstruction(instr)
	}

	chunk.RegisterCount = 3
	chunk.ArgCount = 0

	output := Disassemble(chunk)

	expected := []string{
		"NOP", "MOV", "LOADK", "LOADNULL", "LOADBOOL", "LOADI",
		"ADD", "SUB", "MUL", "DIV", "MOD", "NEG", "NOT", "TOINT", "TONUM",
		"BAND", "BOR", "BXOR", "SHL", "SHR",
		"EQ", "NE", "LT", "LE", "GT", "GE",
		"ARRPUSH", "HASFIELD", "DELFIELD",
		"JMP", "JMPIF", "JMPIFNOT", "CALL", "RET",
	}

	for _, mnemonic := range expected {
		if !strings.Contains(output, mnemonic) {
			t.Errorf("output missing instruction: %s", mnemonic)
		}
	}
}

func TestDisassembleEmptyChunk(t *testing.T) {
	chunk := vm.NewChunk()
	chunk.RegisterCount = 0
	chunk.ArgCount = 0

	output := Disassemble(chunk)

	if !strings.Contains(output, ".constants 0") {
		t.Error("output should show 0 constants")
	}
	if !strings.Contains(output, ".code") {
		t.Error("output missing .code section")
	}
	if !strings.Contains(output, "registers: 0") {
		t.Error("output should show 0 registers")
	}
}

func TestRoundTripAssembleDisassemble(t *testing.T) {
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

	// Assemble
	chunk1, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Disassemble
	disassembled := Disassemble(chunk1)

	// Re-assemble
	chunk2, err := Assemble(disassembled)
	if err != nil {
		t.Fatalf("re-assembly failed: %v", err)
	}

	// Compare chunks
	if len(chunk1.Constants) != len(chunk2.Constants) {
		t.Errorf("constant count mismatch: %d vs %d", len(chunk1.Constants), len(chunk2.Constants))
	}
	if len(chunk1.Code) != len(chunk2.Code) {
		t.Errorf("code count mismatch: %d vs %d", len(chunk1.Code), len(chunk2.Code))
	}
	if chunk1.RegisterCount != chunk2.RegisterCount {
		t.Errorf("register count mismatch: %d vs %d", chunk1.RegisterCount, chunk2.RegisterCount)
	}
	if chunk1.ArgCount != chunk2.ArgCount {
		t.Errorf("arg count mismatch: %d vs %d", chunk1.ArgCount, chunk2.ArgCount)
	}

	// Compare instructions
	for i := range chunk1.Code {
		if chunk1.Code[i] != chunk2.Code[i] {
			t.Errorf("instruction %d mismatch: %+v vs %+v", i, chunk1.Code[i], chunk2.Code[i])
		}
	}
}

func TestDisassembleLineNumbers(t *testing.T) {
	chunk := vm.NewChunk()
	chunk.AddInstruction(vm.NewInstr(vm.OpNOP, 0, 0, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpNOP, 0, 0, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpNOP, 0, 0, 0))
	chunk.RegisterCount = 0
	chunk.ArgCount = 0

	output := Disassemble(chunk)

	// Check for line numbers
	if !strings.Contains(output, "0000:") {
		t.Error("missing line number 0000")
	}
	if !strings.Contains(output, "0001:") {
		t.Error("missing line number 0001")
	}
	if !strings.Contains(output, "0002:") {
		t.Error("missing line number 0002")
	}
}

func TestDisassembleStringLiterals(t *testing.T) {
	chunk := vm.NewChunk()

	// Add strings to string pool
	idx0 := chunk.AddString("Hello, World!")
	idx1 := chunk.AddString("Test\nNewline")
	idx2 := chunk.AddString("Quote: \"test\"")

	// Add constants that reference string pool
	chunk.AddConstant(vm.NewInt(int64(idx0)))
	chunk.AddConstant(vm.NewInt(int64(idx1)))
	chunk.AddConstant(vm.NewInt(int64(idx2)))

	// Add NEWSTR instructions
	chunk.AddInstruction(vm.NewInstr(vm.OpNEWSTR, 0, 0, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpNEWSTR, 1, 1, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpNEWSTR, 2, 2, 0))
	chunk.AddInstruction(vm.NewInstr(vm.OpRET, 0, 0, 0))

	chunk.RegisterCount = 3
	chunk.ArgCount = 0

	output := Disassemble(chunk)

	// Check that strings appear in constants section with proper escaping
	if !strings.Contains(output, `"Hello, World!"`) {
		t.Error("missing simple string literal")
	}
	if !strings.Contains(output, `"Test\nNewline"`) {
		t.Error("missing string with newline escape")
	}
	if !strings.Contains(output, `"Quote: \"test\""`) {
		t.Error("missing string with quote escape")
	}

	// Check NEWSTR instructions
	if !strings.Contains(output, "NEWSTR r0 0") {
		t.Error("missing NEWSTR instruction for r0")
	}
	if !strings.Contains(output, "NEWSTR r1 1") {
		t.Error("missing NEWSTR instruction for r1")
	}
	if !strings.Contains(output, "NEWSTR r2 2") {
		t.Error("missing NEWSTR instruction for r2")
	}
}

func TestRoundTripStringLiterals(t *testing.T) {
	source := `.constants 2
  0: "Hello, World!"
  1: "Escape:\n\t\"\\"

.code
  NEWSTR r0 0
  NEWSTR r1 1
  RET r0

.metadata
  registers: 2
  args: 0
`

	// Assemble
	chunk1, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Disassemble
	disassembled := Disassemble(chunk1)

	// Re-assemble
	chunk2, err := Assemble(disassembled)
	if err != nil {
		t.Fatalf("re-assembly failed: %v", err)
	}

	// Compare string pools
	if len(chunk1.StringPool) != len(chunk2.StringPool) {
		t.Fatalf("string pool size mismatch: %d vs %d", len(chunk1.StringPool), len(chunk2.StringPool))
	}

	for i := range chunk1.StringPool {
		if chunk1.StringPool[i] != chunk2.StringPool[i] {
			t.Errorf("string %d mismatch: %q vs %q", i, chunk1.StringPool[i], chunk2.StringPool[i])
		}
	}

	// Compare code
	if len(chunk1.Code) != len(chunk2.Code) {
		t.Fatalf("code count mismatch: %d vs %d", len(chunk1.Code), len(chunk2.Code))
	}

	for i := range chunk1.Code {
		if chunk1.Code[i] != chunk2.Code[i] {
			t.Errorf("instruction %d mismatch: %+v vs %+v", i, chunk1.Code[i], chunk2.Code[i])
		}
	}
}

package asm

import (
	"strings"
	"testing"

	"github.com/yourname/lumen/internal/vm"
)

func TestAssembleLongJump(t *testing.T) {
	source := `.constants 1
  0: 42i

.code
  LOADK r0 0
  JMP.L far_label
  NOP
  NOP
  NOP
far_label:
  RET r0

.metadata
  registers: 1
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Check that JMP.L was assembled as LONG + JMP32 + offset (3 instructions)
	// Expected: LOADK (0), LONG (1), JMP32 (2), offset (3), NOP (4), NOP (5), NOP (6), RET (7)
	if len(chunk.Code) != 8 {
		t.Errorf("expected 8 instructions, got %d", len(chunk.Code))
	}

	// Verify LONG prefix
	if chunk.Code[1].Op != vm.OpLONG {
		t.Errorf("expected OpLONG at position 1, got %v", chunk.Code[1].Op)
	}

	// Verify JMP32 opcode
	if chunk.Code[2].Op != vm.OpJMP32 {
		t.Errorf("expected OpJMP32 at position 2, got %v", chunk.Code[2].Op)
	}

	// Verify offset word exists
	offsetInstr := chunk.Code[3]
	offset := vm.DecodeOffset32(offsetInstr)

	// Target is instruction 7 (far_label), current position after LONG block is 4
	// Offset should be 7 - 4 = 3
	expectedOffset := int32(3)
	if offset != expectedOffset {
		t.Errorf("expected offset %d, got %d", expectedOffset, offset)
	}

	// Execute and verify
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 42 {
		t.Errorf("expected Int(42), got %s", result.String())
	}
}

func TestAssembleLongJumpIf(t *testing.T) {
	source := `.constants 2
  0: 100i
  1: 200i

.code
  LOADBOOL r0 true
  LOADK r1 0
  JMPIF.L r0 then_label
  LOADK r1 1
then_label:
  RET r1

.metadata
  registers: 2
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Verify LONG prefix for JMPIF
	foundLong := false
	foundJmpIf32 := false
	for i, instr := range chunk.Code {
		if instr.Op == vm.OpLONG && i+1 < len(chunk.Code) && chunk.Code[i+1].Op == vm.OpJMPIF32 {
			foundLong = true
			foundJmpIf32 = true
			break
		}
	}

	if !foundLong {
		t.Error("expected LONG prefix in code")
	}
	if !foundJmpIf32 {
		t.Error("expected OpJMPIF32 in code")
	}

	// Execute and verify - should jump and return 100
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 100 {
		t.Errorf("expected Int(100), got %s", result.String())
	}
}

func TestAssembleLongJumpIfNot(t *testing.T) {
	source := `.constants 2
  0: 100i
  1: 200i

.code
  LOADBOOL r0 false
  LOADK r1 0
  JMPIFNOT.L r0 else_label
  LOADK r1 1
else_label:
  RET r1

.metadata
  registers: 2
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Verify LONG prefix for JMPIFNOT
	foundJmpIfNot32 := false
	for i, instr := range chunk.Code {
		if instr.Op == vm.OpLONG && i+1 < len(chunk.Code) && chunk.Code[i+1].Op == vm.OpJMPIFNOT32 {
			foundJmpIfNot32 = true
			break
		}
	}

	if !foundJmpIfNot32 {
		t.Error("expected OpJMPIFNOT32 in code")
	}

	// Execute and verify - should jump and return 100
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 100 {
		t.Errorf("expected Int(100), got %s", result.String())
	}
}

func TestAssembleLongCall(t *testing.T) {
	source := `.constants 2
  0: 5i
  1: 10i

.code
  LOADK r0 0
  LOADK r1 1
  CALL.L add_func 2
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

	// Verify LONG prefix for CALL
	foundCall32 := false
	for i, instr := range chunk.Code {
		if instr.Op == vm.OpLONG && i+1 < len(chunk.Code) && chunk.Code[i+1].Op == vm.OpCALL32 {
			foundCall32 = true
			// Verify nargs is stored in A field
			if chunk.Code[i+1].A != 2 {
				t.Errorf("expected nargs=2 in CALL32, got %d", chunk.Code[i+1].A)
			}
			break
		}
	}

	if !foundCall32 {
		t.Error("expected OpCALL32 in code")
	}

	// Execute and verify
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 15 {
		t.Errorf("expected Int(15), got %s", result.String())
	}
}

func TestLongInstructionBackwardJump(t *testing.T) {
	source := `.constants 2
  0: 0i
  1: 5i

.code
  LOADK r0 0
loop_start:
  LOADK r1 1
  ADD r0 r0 r1
  LT r2 r0 r1
  JMPIF.L r2 loop_start
  RET r0

.metadata
  registers: 3
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Verify backward jump has negative offset
	for i := 0; i < len(chunk.Code)-2; i++ {
		if chunk.Code[i].Op == vm.OpLONG && chunk.Code[i+1].Op == vm.OpJMPIF32 {
			offset := vm.DecodeOffset32(chunk.Code[i+2])
			// Backward jump should have negative offset
			if offset >= 0 {
				t.Errorf("expected negative offset for backward jump, got %d", offset)
			}
			break
		}
	}

	// Execute
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 5 {
		t.Errorf("expected Int(5), got %s", result.String())
	}
}

func TestLongInstructionDisassembly(t *testing.T) {
	source := `.constants 1
  0: 99i

.code
  LOADK r0 0
  JMP.L end_label
  LOADK r0 0
  NOP
end_label:
  RET r0

.metadata
  registers: 1
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Disassemble
	disasm := Disassemble(chunk)

	// Should contain LONG and target information
	if !strings.Contains(disasm, "LONG") {
		t.Error("disassembly missing LONG prefix")
	}
	if !strings.Contains(disasm, "JMP32") {
		t.Error("disassembly missing JMP32 opcode")
	}
	if !strings.Contains(disasm, "target:") {
		t.Error("disassembly missing target information")
	}
	if !strings.Contains(disasm, "offset:") {
		t.Error("disassembly missing offset information")
	}

	// Verify execution works
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 99 {
		t.Errorf("expected Int(99), got %s", result.String())
	}
}

func TestShortJumpOutOfRangeError(t *testing.T) {
	// Create a program with a jump target beyond 255 instructions
	var sb strings.Builder
	sb.WriteString(`.constants 1
  0: 1i

.code
  JMP far_label
`)

	// Add many NOP instructions to push the label beyond 255
	for i := 0; i < 300; i++ {
		sb.WriteString("  NOP\n")
	}

	sb.WriteString(`far_label:
  LOADK r0 0
  RET r0

.metadata
  registers: 1
  args: 0
`)

	_, err := Assemble(sb.String())
	if err == nil {
		t.Fatal("expected error for short jump out of range")
	}
	if !strings.Contains(err.Error(), "out of range") && !strings.Contains(err.Error(), "JMP.L") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLongJumpLargeOffset(t *testing.T) {
	// Create a program with a large jump offset (but within 32-bit range)
	var sb strings.Builder
	sb.WriteString(`.constants 1
  0: 42i

.code
  LOADK r0 0
  JMP.L far_label
`)

	// Add 1000 NOP instructions
	for i := 0; i < 1000; i++ {
		sb.WriteString("  NOP\n")
	}

	sb.WriteString(`far_label:
  RET r0

.metadata
  registers: 1
  args: 0
`)

	chunk, err := Assemble(sb.String())
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}

	// Verify LONG instruction is present
	if chunk.Code[1].Op != vm.OpLONG {
		t.Error("expected OpLONG for large offset jump")
	}

	// Execute and verify
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 42 {
		t.Errorf("expected Int(42), got %s", result.String())
	}
}

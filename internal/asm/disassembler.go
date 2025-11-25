package asm

import (
	"fmt"
	"strings"

	"github.com/yourname/lumen/internal/vm"
)

// Disassemble converts a Chunk into human-readable assembly text.
func Disassemble(chunk *vm.Chunk) string {
	var sb strings.Builder

	// Write constants section
	sb.WriteString(".constants ")
	sb.WriteString(fmt.Sprintf("%d\n", len(chunk.Constants)))
	for i, val := range chunk.Constants {
		sb.WriteString(fmt.Sprintf("  %d: %s\n", i, formatValue(val)))
	}
	sb.WriteString("\n")

	// Write code section
	sb.WriteString(".code\n")
	for i, instr := range chunk.Code {
		sb.WriteString(fmt.Sprintf("  %04d: %s\n", i, formatInstruction(instr, chunk)))
	}
	sb.WriteString("\n")

	// Write metadata section
	sb.WriteString(".metadata\n")
	sb.WriteString(fmt.Sprintf("  registers: %d\n", chunk.RegisterCount))
	sb.WriteString(fmt.Sprintf("  args: %d\n", chunk.ArgCount))

	return sb.String()
}

// formatValue converts a Value to its assembly representation.
func formatValue(val vm.Value) string {
	switch {
	case val.IsNumber():
		return fmt.Sprintf("%v", val.AsNumber())
	case val.IsInt():
		return fmt.Sprintf("%di", val.AsInt())
	case val.IsBool():
		if val.AsBool() {
			return "true"
		}
		return "false"
	case val.IsNull():
		return "null"
	case val.IsHeapPtr():
		return fmt.Sprintf("ptr(0x%x)", val.AsHeapPtr())
	default:
		return fmt.Sprintf("unknown(0x%x)", uint64(val))
	}
}

// formatInstruction converts an instruction to assembly syntax.
func formatInstruction(instr vm.Instr, chunk *vm.Chunk) string {
	switch instr.Op {
	case vm.OpNOP:
		return "NOP"

	case vm.OpMOV:
		return fmt.Sprintf("MOV r%d r%d", instr.A, instr.B)

	case vm.OpLOADK:
		return fmt.Sprintf("LOADK r%d %d", instr.A, instr.B)

	case vm.OpLOADNULL:
		return fmt.Sprintf("LOADNULL r%d", instr.A)

	case vm.OpLOADBOOL:
		val := "false"
		if instr.B != 0 {
			val = "true"
		}
		return fmt.Sprintf("LOADBOOL r%d %s", instr.A, val)

	case vm.OpADD:
		return fmt.Sprintf("ADD r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpSUB:
		return fmt.Sprintf("SUB r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpMUL:
		return fmt.Sprintf("MUL r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpDIV:
		return fmt.Sprintf("DIV r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpEQ:
		return fmt.Sprintf("EQ r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpNE:
		return fmt.Sprintf("NE r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpLT:
		return fmt.Sprintf("LT r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpLE:
		return fmt.Sprintf("LE r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpGT:
		return fmt.Sprintf("GT r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpGE:
		return fmt.Sprintf("GE r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpJMP:
		return fmt.Sprintf("JMP %d", instr.A)

	case vm.OpJMPIF:
		return fmt.Sprintf("JMPIF r%d %d", instr.A, instr.B)

	case vm.OpJMPIFNOT:
		return fmt.Sprintf("JMPIFNOT r%d %d", instr.A, instr.B)

	case vm.OpCALL:
		return fmt.Sprintf("CALL r%d %d", instr.A, instr.B)

	case vm.OpRET:
		return fmt.Sprintf("RET r%d", instr.A)

	case vm.OpCALLEXTERN:
		externName := "unknown"
		if int(instr.A) < len(chunk.ExternNames) {
			externName = chunk.ExternNames[instr.A]
		}
		return fmt.Sprintf("CALLEXTERN %s %d", externName, instr.B)

	case vm.OpNEWSTR:
		return fmt.Sprintf("NEWSTR r%d %d", instr.A, instr.B)

	case vm.OpNEWARRAY:
		return fmt.Sprintf("NEWARRAY r%d %d", instr.A, instr.B)

	case vm.OpNEWOBJ:
		return fmt.Sprintf("NEWOBJ r%d", instr.A)

	case vm.OpGETINDEX:
		return fmt.Sprintf("GETINDEX r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpSETINDEX:
		return fmt.Sprintf("SETINDEX r%d r%d r%d", instr.A, instr.B, instr.C)

	case vm.OpGETFIELD:
		return fmt.Sprintf("GETFIELD r%d r%d %d", instr.A, instr.B, instr.C)

	case vm.OpSETFIELD:
		return fmt.Sprintf("SETFIELD r%d %d r%d", instr.A, instr.B, instr.C)

	case vm.OpARRLEN:
		return fmt.Sprintf("ARRLEN r%d r%d", instr.A, instr.B)

	case vm.OpGC:
		return "GC"

	default:
		return fmt.Sprintf("UNKNOWN(%d) %d %d %d", instr.Op, instr.A, instr.B, instr.C)
	}
}

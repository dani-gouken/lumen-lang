package vm

// OpCode represents a bytecode instruction type.
type OpCode byte

const (
	OpNOP OpCode = iota

	// Data movement
	OpMOV
	OpLOADK
	OpLOADNULL
	OpLOADBOOL
	OpLOADI

	// Arithmetic
	OpADD
	OpSUB
	OpMUL
	OpDIV
	OpMOD
	OpNEG
	OpNOT
	OpTOINT
	OpTONUM
	OpBAND
	OpBOR
	OpBXOR
	OpSHL
	OpSHR

	// Comparison
	OpEQ
	OpNE
	OpLT
	OpLE
	OpGT
	OpGE

	// Control flow
	OpJMP
	OpJMPIF
	OpJMPIFNOT

	// Long instruction prefix
	OpLONG // Indicates next instruction uses 32-bit offset in following word

	// 32-bit control flow (must follow OpLONG prefix)
	OpJMP32      // A = unused, followed by 32-bit offset word
	OpJMPIF32    // A = condition register, followed by 32-bit offset word
	OpJMPIFNOT32 // A = condition register, followed by 32-bit offset word
	OpCALL32     // A = nargs, followed by 32-bit offset word

	// Function calls
	OpCALL
	OpRET
	OpCALLEXTERN // A = extern func index, B = argc

	// Heap allocation
	OpNEWSTR   // A = dest, B = const index (string constant)
	OpNEWARRAY // A = dest, B = capacity
	OpNEWOBJ   // A = dest

	// Array/Object access
	OpGETINDEX // A = dest, B = array, C = index
	OpSETINDEX // A = array, B = index, C = value
	OpGETFIELD // A = dest, B = object, C = const index (field name)
	OpSETFIELD // A = object, B = const index (field name), C = value
	OpARRLEN   // A = dest, B = array
	OpARRPUSH  // A = array, B = value
	OpHASFIELD // A = dest bool, B = object, C = const index
	OpDELFIELD // A = dest bool, B = object, C = const index

	// Garbage collection
	OpGC // Trigger GC manually
)

// Instr represents a single bytecode instruction.
// Layout: [Op:8][A:8][B:8][C:8]
type Instr struct {
	Op OpCode
	A  byte
	B  byte
	C  byte
}

// NewInstr creates a new instruction.
func NewInstr(op OpCode, a, b, c byte) Instr {
	return Instr{Op: op, A: a, B: b, C: c}
}

// String returns a debug representation of the instruction.
func (i Instr) String() string {
	switch i.Op {
	case OpNOP:
		return "NOP"
	case OpMOV:
		return formatInstr("MOV", i.A, i.B)
	case OpLOADK:
		return formatInstr("LOADK", i.A, i.B)
	case OpLOADNULL:
		return formatInstr("LOADNULL", i.A)
	case OpLOADBOOL:
		return formatInstr("LOADBOOL", i.A, i.B)
	case OpLOADI:
		return formatInstr("LOADI", i.A, i.B)
	case OpADD:
		return formatInstr("ADD", i.A, i.B, i.C)
	case OpSUB:
		return formatInstr("SUB", i.A, i.B, i.C)
	case OpMUL:
		return formatInstr("MUL", i.A, i.B, i.C)
	case OpDIV:
		return formatInstr("DIV", i.A, i.B, i.C)
	case OpMOD:
		return formatInstr("MOD", i.A, i.B, i.C)
	case OpNEG:
		return formatInstr("NEG", i.A, i.B)
	case OpNOT:
		return formatInstr("NOT", i.A, i.B)
	case OpTOINT:
		return formatInstr("TOINT", i.A, i.B)
	case OpTONUM:
		return formatInstr("TONUM", i.A, i.B)
	case OpBAND:
		return formatInstr("BAND", i.A, i.B, i.C)
	case OpBOR:
		return formatInstr("BOR", i.A, i.B, i.C)
	case OpBXOR:
		return formatInstr("BXOR", i.A, i.B, i.C)
	case OpSHL:
		return formatInstr("SHL", i.A, i.B, i.C)
	case OpSHR:
		return formatInstr("SHR", i.A, i.B, i.C)
	case OpEQ:
		return formatInstr("EQ", i.A, i.B, i.C)
	case OpNE:
		return formatInstr("NE", i.A, i.B, i.C)
	case OpLT:
		return formatInstr("LT", i.A, i.B, i.C)
	case OpLE:
		return formatInstr("LE", i.A, i.B, i.C)
	case OpGT:
		return formatInstr("GT", i.A, i.B, i.C)
	case OpGE:
		return formatInstr("GE", i.A, i.B, i.C)
	case OpJMP:
		return formatInstr("JMP", i.A)
	case OpJMPIF:
		return formatInstr("JMPIF", i.A, i.B)
	case OpJMPIFNOT:
		return formatInstr("JMPIFNOT", i.A, i.B)
	case OpLONG:
		return "LONG"
	case OpJMP32:
		return "JMP32"
	case OpJMPIF32:
		return formatInstr("JMPIF32", i.A)
	case OpJMPIFNOT32:
		return formatInstr("JMPIFNOT32", i.A)
	case OpCALL32:
		return formatInstr("CALL32", i.A)
	case OpCALL:
		return formatInstr("CALL", i.A, i.B)
	case OpRET:
		return formatInstr("RET", i.A)
	case OpCALLEXTERN:
		return formatInstr("CALLEXTERN", i.A, i.B)
	case OpNEWSTR:
		return formatInstr("NEWSTR", i.A, i.B)
	case OpNEWARRAY:
		return formatInstr("NEWARRAY", i.A, i.B)
	case OpNEWOBJ:
		return formatInstr("NEWOBJ", i.A)
	case OpGETINDEX:
		return formatInstr("GETINDEX", i.A, i.B, i.C)
	case OpSETINDEX:
		return formatInstr("SETINDEX", i.A, i.B, i.C)
	case OpGETFIELD:
		return formatInstr("GETFIELD", i.A, i.B, i.C)
	case OpSETFIELD:
		return formatInstr("SETFIELD", i.A, i.B, i.C)
	case OpARRLEN:
		return formatInstr("ARRLEN", i.A, i.B)
	case OpARRPUSH:
		return formatInstr("ARRPUSH", i.A, i.B)
	case OpHASFIELD:
		return formatInstr("HASFIELD", i.A, i.B, i.C)
	case OpDELFIELD:
		return formatInstr("DELFIELD", i.A, i.B, i.C)
	case OpGC:
		return "GC"
	default:
		return "UNKNOWN"
	}
}

func formatInstr(name string, args ...byte) string {
	s := name
	for _, arg := range args {
		s += " r" + string(rune('0'+arg))
	}
	return s
}

// EncodeOffset32 encodes a 32-bit signed offset as an Instr.
// The offset is stored across all 4 bytes of the instruction.
func EncodeOffset32(offset int32) Instr {
	return Instr{
		Op: OpCode(byte(offset >> 24)),
		A:  byte(offset >> 16),
		B:  byte(offset >> 8),
		C:  byte(offset),
	}
}

// DecodeOffset32 decodes a 32-bit signed offset from an Instr.
func DecodeOffset32(instr Instr) int32 {
	return int32(instr.Op)<<24 | int32(instr.A)<<16 | int32(instr.B)<<8 | int32(instr.C)
}

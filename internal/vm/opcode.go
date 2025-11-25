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

	// Arithmetic
	OpADD
	OpSUB
	OpMUL
	OpDIV

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

	// Function calls
	OpCALL
	OpRET
	OpCALLEXTERN  // A = extern func index, B = argc

	// Heap allocation
	OpNEWSTR    // A = dest, B = const index (string constant)
	OpNEWARRAY  // A = dest, B = capacity
	OpNEWOBJ    // A = dest

	// Array/Object access
	OpGETINDEX  // A = dest, B = array, C = index
	OpSETINDEX  // A = array, B = index, C = value
	OpGETFIELD  // A = dest, B = object, C = const index (field name)
	OpSETFIELD  // A = object, B = const index (field name), C = value
	OpARRLEN    // A = dest, B = array

	// Garbage collection
	OpGC        // Trigger GC manually
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
	case OpADD:
		return formatInstr("ADD", i.A, i.B, i.C)
	case OpSUB:
		return formatInstr("SUB", i.A, i.B, i.C)
	case OpMUL:
		return formatInstr("MUL", i.A, i.B, i.C)
	case OpDIV:
		return formatInstr("DIV", i.A, i.B, i.C)
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

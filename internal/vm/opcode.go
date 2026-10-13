package vm

import "strconv"

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

	// Function calls
	OpNEWFUNC   // A = dest_reg, B = proto_idx, C = unused
	OpNEWEXFUNC // A = dest_reg, B = extern_idx, C = unused
	OpCALL      // A = func_reg, B = nargs, C = unused (handles both user and extern functions)
	OpRET       // A = return register

	// Closure initialization
	OpCAPLOCAL // A = dest_func_reg, B = upvalue_idx, C = local_reg_idx
	OpCAPUPVAL // A = dest_func_reg, B = upvalue_idx, C = parent_upval_idx

	// Upvalue operations (Access)
	OpLDCAP // A = dest_reg, B = upvalue_idx, C = unused (load from upvalue)
	OpSTCAP // A = upvalue_idx, B = src_reg, C = unused (store to upvalue)

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

	// Checked element write (added after OpGC to keep earlier opcodes stable)
	OpSETELEM // A = array, B = index, C = value; index must be below the length

	// Detach closures from a register: A = register. Closures that captured
	// it keep the value it has now and no longer follow the register.
	OpCLOSEUP

	// Cleanup (defer). A handler is where a call goes on when something
	// fails in it, or in what it calls, instead of being abandoned at once.
	OpTRY32   // After OpLONG, followed by an offset word: from here a failure in this call continues at the offset
	OpENDTRY  // The innermost handler of this call no longer applies
	OpRETHROW // Pass on the failure the handler was run for
)

// MaxOperand is the largest value an instruction operand can hold. It bounds
// what one operand can index: registers per function, constants, functions
// and host functions per program.
const MaxOperand = 1<<16 - 1

// Instr represents a single bytecode instruction.
// Layout: [Op:8][A:16][B:16][C:16]
type Instr struct {
	Op OpCode
	A  uint16
	B  uint16
	C  uint16
}

// NewInstr creates a new instruction.
func NewInstr(op OpCode, a, b, c uint16) Instr {
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
	case OpNEWFUNC:
		return formatInstr("NEWFUNC", i.A, i.B)
	case OpNEWEXFUNC:
		return formatInstr("NEWEXFUNC", i.A, i.B)
	case OpCALL:
		return formatInstr("CALL", i.A, i.B)
	case OpRET:
		return formatInstr("RET", i.A)
	case OpCAPLOCAL:
		return formatInstr("CAPLOCAL", i.A, i.B, i.C)
	case OpCAPUPVAL:
		return formatInstr("CAPUPVAL", i.A, i.B, i.C)
	case OpLDCAP:
		return formatInstr("LDCAP", i.A, i.B)
	case OpSTCAP:
		return formatInstr("STCAP", i.A, i.B)
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
	case OpSETELEM:
		return formatInstr("SETELEM", i.A, i.B, i.C)
	case OpCLOSEUP:
		return formatInstr("CLOSEUP", i.A)
	case OpTRY32:
		return "TRY32"
	case OpENDTRY:
		return "ENDTRY"
	case OpRETHROW:
		return "RETHROW"
	case OpGC:
		return "GC"
	default:
		return "UNKNOWN"
	}
}

func formatInstr(name string, args ...uint16) string {
	s := name
	for _, arg := range args {
		s += " r" + strconv.Itoa(int(arg))
	}
	return s
}

// EncodeOffset32 encodes a 32-bit signed offset as an Instr.
// The high half is stored in A and the low half in B; Op and C are zero.
func EncodeOffset32(offset int32) Instr {
	return Instr{
		A: uint16(uint32(offset) >> 16),
		B: uint16(uint32(offset)),
	}
}

// DecodeOffset32 decodes a 32-bit signed offset from an Instr.
func DecodeOffset32(instr Instr) int32 {
	return int32(uint32(instr.A)<<16 | uint32(instr.B))
}

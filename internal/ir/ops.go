package ir

// OpType defines the type of IR operation
type OpType int

const (
	OpNop OpType = iota

	// Arithmetic & Logic
	OpAdd // Dest = Src1 + Src2
	OpSub // Dest = Src1 - Src2
	OpMul // Dest = Src1 * Src2
	OpDiv // Dest = Src1 / Src2
	OpMod // Dest = Src1 % Src2
	OpNeg // Dest = -Src1
	OpNot // Dest = !Src1
	OpEq  // Dest = Src1 == Src2
	OpNeq // Dest = Src1 != Src2
	OpLt  // Dest = Src1 < Src2
	OpLte // Dest = Src1 <= Src2
	OpGt  // Dest = Src1 > Src2
	OpGte // Dest = Src1 >= Src2
	OpAnd // Dest = Src1 && Src2
	OpOr  // Dest = Src1 || Src2

	// Control Flow
	OpJmp       // Jump to Block
	OpBr        // Branch if Src1 then Block1 else Block2
	OpRet       // Return Src1 (optional)
	OpCall      // Dest = Call Func(Args...)
	OpCallTrait // Dest = CallTrait Receiver, TraitName, MethodName, Args...

	// Memory & Variables
	OpMov // Dest = Src1

	// Structures
	OpNewStruct // Dest = NewStruct TypeName, Fields...
	OpGetField  // Dest = Src1.Field
	OpSetField  // Src1.Field = Src2

	// Arrays
	OpNewArray // Dest = NewArray Type, Size
	OpGetIndex // Dest = Src1[Src2]
	OpSetIndex // Src1[Src2] = Src3

	// Closures
	OpMakeClosure // Dest = MakeClosure Func, Captures...
)

func (op OpType) String() string {
	switch op {
	case OpNop:
		return "NOP"
	case OpAdd:
		return "ADD"
	case OpSub:
		return "SUB"
	case OpMul:
		return "MUL"
	case OpDiv:
		return "DIV"
	case OpMod:
		return "MOD"
	case OpNeg:
		return "NEG"
	case OpNot:
		return "NOT"
	case OpEq:
		return "EQ"
	case OpNeq:
		return "NEQ"
	case OpLt:
		return "LT"
	case OpLte:
		return "LTE"
	case OpGt:
		return "GT"
	case OpGte:
		return "GTE"
	case OpAnd:
		return "AND"
	case OpOr:
		return "OR"
	case OpJmp:
		return "JMP"
	case OpBr:
		return "BR"
	case OpRet:
		return "RET"
	case OpCall:
		return "CALL"
	case OpCallTrait:
		return "CALL_TRAIT"
	case OpMov:
		return "MOV"
	case OpNewStruct:
		return "NEW_STRUCT"
	case OpGetField:
		return "GET_FIELD"
	case OpSetField:
		return "SET_FIELD"
	case OpNewArray:
		return "NEW_ARRAY"
	case OpGetIndex:
		return "GET_INDEX"
	case OpSetIndex:
		return "SET_INDEX"
	case OpMakeClosure:
		return "MAKE_CLOSURE"
	default:
		return "UNKNOWN"
	}
}

package vm

import "fmt"

const (
	stackSize = 4096
)

// VM represents the Lumen virtual machine.
type VM struct {
	stack [stackSize]Value
	fp    int // frame pointer
	sp    int // stack pointer
	ip    int // instruction pointer
	regs  []Value
}

// NewVM creates a new VM instance.
func NewVM() *VM {
	return &VM{
		fp:   0,
		sp:   0,
		ip:   0,
		regs: make([]Value, 0),
	}
}

// Exec executes a chunk of bytecode.
func (vm *VM) Exec(chunk *Chunk) (Value, error) {
	if chunk == nil {
		return NewNull(), fmt.Errorf("cannot execute nil chunk")
	}

	// Allocate registers for this chunk
	vm.regs = make([]Value, chunk.RegisterCount)
	for i := range vm.regs {
		vm.regs[i] = NewNull()
	}

	vm.ip = 0

	for vm.ip < len(chunk.Code) {
		instr := chunk.Code[vm.ip]

		switch instr.Op {
		case OpNOP:
			// Do nothing

		case OpMOV:
			vm.regs[instr.A] = vm.regs[instr.B]

		case OpLOADK:
			if int(instr.B) >= len(chunk.Constants) {
				return NewNull(), fmt.Errorf("constant index out of bounds: %d", instr.B)
			}
			vm.regs[instr.A] = chunk.Constants[instr.B]

		case OpLOADNULL:
			vm.regs[instr.A] = NewNull()

		case OpLOADBOOL:
			vm.regs[instr.A] = NewBool(instr.B != 0)

		case OpADD:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() + b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() + b.AsInt())
			} else {
				return NewNull(), fmt.Errorf("ADD requires numeric operands")
			}

		case OpSUB:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() - b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() - b.AsInt())
			} else {
				return NewNull(), fmt.Errorf("SUB requires numeric operands")
			}

		case OpMUL:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() * b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() * b.AsInt())
			} else {
				return NewNull(), fmt.Errorf("MUL requires numeric operands")
			}

		case OpDIV:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() / b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() / b.AsInt())
			} else {
				return NewNull(), fmt.Errorf("DIV requires numeric operands")
			}

		case OpRET:
			return vm.regs[instr.A], nil

		default:
			return NewNull(), fmt.Errorf("unimplemented opcode: %v", instr.Op)
		}

		vm.ip++
	}

	return NewNull(), nil
}

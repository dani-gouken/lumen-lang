package vm

import (
	"fmt"
	"unsafe"
)

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
	heap  *Heap
}

// NewVM creates a new VM instance.
func NewVM() *VM {
	return &VM{
		fp:   0,
		sp:   0,
		ip:   0,
		regs: make([]Value, 0),
		heap: NewHeap(DefaultHeapSize),
	}
}

// NewVMWithHeapSize creates a new VM with a custom heap size.
func NewVMWithHeapSize(heapSize uint64) *VM {
	return &VM{
		fp:   0,
		sp:   0,
		ip:   0,
		regs: make([]Value, 0),
		heap: NewHeap(heapSize),
	}
}

// GetHeap returns the VM's heap.
func (vm *VM) GetHeap() *Heap {
	return vm.heap
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

		case OpNEWSTR:
			// Create a string from constant pool
			if int(instr.B) >= len(chunk.Constants) {
				return NewNull(), fmt.Errorf("constant index out of bounds: %d", instr.B)
			}
			constVal := chunk.Constants[instr.B]
			if !constVal.IsHeapPtr() {
				return NewNull(), fmt.Errorf("NEWSTR requires string constant")
			}
			vm.regs[instr.A] = constVal

		case OpNEWARRAY:
			// Allocate a new array
			capacity := int(instr.B)
			obj, shouldGC, err := vm.heap.AllocArray(capacity)
			if err != nil {
				return NewNull(), fmt.Errorf("array allocation failed: %v", err)
			}
			vm.regs[instr.A] = NewHeapPtr(uintptr(unsafe.Pointer(obj)))
			if shouldGC {
				vm.CollectGarbage()
			}

		case OpNEWOBJ:
			// Allocate a new object
			obj, shouldGC, err := vm.heap.AllocObject()
			if err != nil {
				return NewNull(), fmt.Errorf("object allocation failed: %v", err)
			}
			vm.regs[instr.A] = NewHeapPtr(uintptr(unsafe.Pointer(obj)))
			if shouldGC {
				vm.CollectGarbage()
			}

		case OpGETINDEX:
			// Get array element: A = dest, B = array, C = index
			arrayVal := vm.regs[instr.B]
			indexVal := vm.regs[instr.C]

			if !arrayVal.IsHeapPtr() {
				return NewNull(), fmt.Errorf("GETINDEX requires array")
			}
			if !indexVal.IsInt() {
				return NewNull(), fmt.Errorf("GETINDEX requires integer index")
			}

			arrayObj := (*ArrayObj)(unsafe.Pointer(arrayVal.AsHeapPtr()))
			index := int(indexVal.AsInt())
			vm.regs[instr.A] = arrayObj.Get(index)

		case OpSETINDEX:
			// Set array element: A = array, B = index, C = value
			arrayVal := vm.regs[instr.A]
			indexVal := vm.regs[instr.B]
			value := vm.regs[instr.C]

			if !arrayVal.IsHeapPtr() {
				return NewNull(), fmt.Errorf("SETINDEX requires array")
			}
			if !indexVal.IsInt() {
				return NewNull(), fmt.Errorf("SETINDEX requires integer index")
			}

			arrayObj := (*ArrayObj)(unsafe.Pointer(arrayVal.AsHeapPtr()))
			index := int(indexVal.AsInt())
			arrayObj.Set(index, value)

		case OpGETFIELD:
			// Get object field: A = dest, B = object, C = const index (field name)
			objVal := vm.regs[instr.B]
			if !objVal.IsHeapPtr() {
				return NewNull(), fmt.Errorf("GETFIELD requires object")
			}

			if int(instr.C) >= len(chunk.Constants) {
				return NewNull(), fmt.Errorf("constant index out of bounds: %d", instr.C)
			}

			// Get field name from constant
			fieldNameVal := chunk.Constants[instr.C]
			if !fieldNameVal.IsHeapPtr() {
				return NewNull(), fmt.Errorf("GETFIELD requires string constant for field name")
			}

			fieldNameObj := (*StringObj)(unsafe.Pointer(fieldNameVal.AsHeapPtr()))
			fieldName := fieldNameObj.Data()

			objObj := (*ObjectObj)(unsafe.Pointer(objVal.AsHeapPtr()))
			if value, ok := objObj.Get(fieldName); ok {
				vm.regs[instr.A] = value
			} else {
				vm.regs[instr.A] = NewNull()
			}

		case OpSETFIELD:
			// Set object field: A = object, B = const index (field name), C = value
			objVal := vm.regs[instr.A]
			if !objVal.IsHeapPtr() {
				return NewNull(), fmt.Errorf("SETFIELD requires object")
			}

			if int(instr.B) >= len(chunk.Constants) {
				return NewNull(), fmt.Errorf("constant index out of bounds: %d", instr.B)
			}

			// Get field name from constant
			fieldNameVal := chunk.Constants[instr.B]
			if !fieldNameVal.IsHeapPtr() {
				return NewNull(), fmt.Errorf("SETFIELD requires string constant for field name")
			}

			fieldNameObj := (*StringObj)(unsafe.Pointer(fieldNameVal.AsHeapPtr()))
			fieldName := fieldNameObj.Data()

			value := vm.regs[instr.C]

			objObj := (*ObjectObj)(unsafe.Pointer(objVal.AsHeapPtr()))
			objObj.Set(fieldName, value)

		case OpARRLEN:
			// Get array length: A = dest, B = array
			arrayVal := vm.regs[instr.B]
			if !arrayVal.IsHeapPtr() {
				return NewNull(), fmt.Errorf("ARRLEN requires array")
			}

			arrayObj := (*ArrayObj)(unsafe.Pointer(arrayVal.AsHeapPtr()))
			vm.regs[instr.A] = NewInt(int64(arrayObj.Len()))

		case OpGC:
			// Manually trigger garbage collection
			vm.CollectGarbage()

		default:
			return NewNull(), fmt.Errorf("unimplemented opcode: %v", instr.Op)
		}

		vm.ip++
	}

	return NewNull(), nil
}

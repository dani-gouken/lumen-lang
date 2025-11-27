package vm

import (
	"fmt"
	"unsafe"
)

const (
	stackSize    = 4096
	maxCallDepth = 256
)

// CallFrame represents a function call frame.
type CallFrame struct {
	chunk *Chunk  // The function's bytecode
	ip    int     // Return instruction pointer
	fp    int     // Frame pointer (base of this frame in stack)
	regs  []Value // Saved registers from caller
}

// ExternFunc is a function callable from bytecode.
type ExternFunc func(*VM, []Value) (Value, error)

// DebugHook is a function called before each instruction execution.
// It receives a snapshot of the VM state and returns true if execution should pause.
type DebugHook func(snapshot interface{}) bool

// VM represents the Lumen virtual machine.
type VM struct {
	stack       [stackSize]Value
	frames      [maxCallDepth]CallFrame
	frameCount  int     // Number of active frames
	fp          int     // frame pointer
	sp          int     // stack pointer
	ip          int     // instruction pointer
	regs        []Value // Current registers
	chunk       *Chunk  // Current chunk
	heap        *Heap
	externFuncs map[string]ExternFunc // External functions
	debugHook   DebugHook             // Optional debug hook
}

// NewVM creates a new VM instance.
func NewVM() *VM {
	vm := &VM{
		frameCount:  0,
		fp:          0,
		sp:          0,
		ip:          0,
		regs:        make([]Value, 0),
		chunk:       nil,
		heap:        NewHeap(DefaultHeapSize),
		externFuncs: make(map[string]ExternFunc),
	}
	vm.registerBuiltins()
	return vm
}

// NewVMWithHeapSize creates a new VM with a custom heap size.
func NewVMWithHeapSize(heapSize uint64) *VM {
	vm := &VM{
		frameCount:  0,
		fp:          0,
		sp:          0,
		ip:          0,
		regs:        make([]Value, 0),
		chunk:       nil,
		heap:        NewHeap(heapSize),
		externFuncs: make(map[string]ExternFunc),
	}
	vm.registerBuiltins()
	return vm
}

// RegisterExtern registers an external function.
func (vm *VM) RegisterExtern(name string, fn ExternFunc) {
	vm.externFuncs[name] = fn
}

// registerBuiltins registers built-in external functions.
func (vm *VM) registerBuiltins() {
	// print function
	vm.RegisterExtern("print", func(vm *VM, args []Value) (Value, error) {
		for i, arg := range args {
			if i > 0 {
				fmt.Print(" ")
			}
			fmt.Print(arg.String())
		}
		return NewNull(), nil
	})

	// println function
	vm.RegisterExtern("println", func(vm *VM, args []Value) (Value, error) {
		for i, arg := range args {
			if i > 0 {
				fmt.Print(" ")
			}
			fmt.Print(arg.String())
		}
		fmt.Println()
		return NewNull(), nil
	})
}

// GetHeap returns the VM's heap.
func (vm *VM) GetHeap() *Heap {
	return vm.heap
}

// SetDebugHook sets the debug hook function.
func (vm *VM) SetDebugHook(hook DebugHook) {
	vm.debugHook = hook
}

// Snapshot returns a snapshot of the current VM state.
// The snapshot interface{} is actually a struct containing all VM state.
func (vm *VM) Snapshot() interface{} {
	// Create a copy of registers
	regsCopy := make([]Value, len(vm.regs))
	copy(regsCopy, vm.regs)

	// Create snapshot struct (defined in debugger package to avoid circular dependency)
	snapshot := struct {
		IP            int
		Instr         Instr
		DebugInfo     *DebugInfo
		Registers     []Value
		Constants     []Value
		Code          []Instr
		StackPointer  int
		FramePointer  int
		FrameCount    int
		HeapAllocated uint64
		HeapMax       uint64
		RegisterCount int
		ArgCount      int
	}{
		IP:            vm.ip,
		Instr:         Instr{},
		DebugInfo:     nil,
		Registers:     regsCopy,
		Constants:     vm.chunk.Constants,
		Code:          vm.chunk.Code,
		StackPointer:  vm.sp,
		FramePointer:  vm.fp,
		FrameCount:    vm.frameCount,
		HeapAllocated: vm.heap.bytesAllocated,
		HeapMax:       vm.heap.maxBytes,
		RegisterCount: vm.chunk.RegisterCount,
		ArgCount:      vm.chunk.ArgCount,
	}

	// Add current instruction if in valid range
	if vm.ip < len(vm.chunk.Code) {
		snapshot.Instr = vm.chunk.Code[vm.ip]
		snapshot.DebugInfo = vm.chunk.GetDebugInfo(vm.ip)
	}

	return snapshot
}

// runtimeError creates a formatted error message with debug info.
func (vm *VM) runtimeError(message string) error {
	debugInfo := vm.chunk.GetDebugInfo(vm.ip)
	if debugInfo != nil {
		return fmt.Errorf("%s", debugInfo.FormatError(message))
	}
	return fmt.Errorf("runtime error: %s (at instruction %d)", message, vm.ip)
}

// Exec executes a chunk of bytecode.
func (vm *VM) Exec(chunk *Chunk) (Value, error) {
	if chunk == nil {
		return NewNull(), fmt.Errorf("cannot execute nil chunk")
	}

	// Set current chunk
	vm.chunk = chunk

	// Allocate registers for this chunk
	vm.regs = make([]Value, chunk.RegisterCount)
	for i := range vm.regs {
		vm.regs[i] = NewNull()
	}

	vm.ip = 0
	vm.frameCount = 0

	for vm.ip < len(vm.chunk.Code) {
		// Call debug hook if set
		if vm.debugHook != nil {
			snapshot := vm.Snapshot()
			if vm.debugHook(snapshot) {
				// Debug hook requested pause - this would be handled by debugger
				// For now, we just continue
			}
		}

		instr := vm.chunk.Code[vm.ip]

		switch instr.Op {
		case OpNOP:
			// Do nothing

		case OpMOV:
			vm.regs[instr.A] = vm.regs[instr.B]

		case OpLOADK:
			if int(instr.B) >= len(vm.chunk.Constants) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("constant index out of bounds: %d", instr.B))
			}
			vm.regs[instr.A] = vm.chunk.Constants[instr.B]

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
				return NewNull(), vm.runtimeError(fmt.Sprintf("ADD requires numeric operands, got: %s + %s", a.String(), b.String()))
			}

		case OpSUB:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() - b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() - b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("SUB requires numeric operands, got: %s - %s", a.String(), b.String()))
			}

		case OpMUL:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() * b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() * b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("MUL requires numeric operands, got: %s * %s", a.String(), b.String()))
			}

		case OpDIV:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() / b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() / b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("DIV requires numeric operands, got: %s / %s", a.String(), b.String()))
			}

		case OpEQ:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			vm.regs[instr.A] = NewBool(a == b)

		case OpNE:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			vm.regs[instr.A] = NewBool(a != b)

		case OpLT:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewBool(a.AsNumber() < b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewBool(a.AsInt() < b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("LT requires numeric operands, got: %s < %s", a.String(), b.String()))
			}

		case OpLE:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewBool(a.AsNumber() <= b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewBool(a.AsInt() <= b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("LE requires numeric operands, got: %s <= %s", a.String(), b.String()))
			}

		case OpGT:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewBool(a.AsNumber() > b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewBool(a.AsInt() > b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("GT requires numeric operands, got: %s > %s", a.String(), b.String()))
			}

		case OpGE:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewBool(a.AsNumber() >= b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewBool(a.AsInt() >= b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("GE requires numeric operands, got: %s >= %s", a.String(), b.String()))
			}

		case OpJMP:
			// JMP: A = target instruction address
			vm.ip = int(instr.A)
			continue

		case OpJMPIF:
			// JMPIF: A = condition register, B = target address
			cond := vm.regs[instr.A]
			if cond.IsBool() && cond.AsBool() {
				vm.ip = int(instr.B)
				continue
			}

		case OpJMPIFNOT:
			// JMPIFNOT: A = condition register, B = target address
			cond := vm.regs[instr.A]
			if cond.IsBool() && !cond.AsBool() {
				vm.ip = int(instr.B)
				continue
			}

		case OpCALL:
			// CALL: A = function address (instruction pointer), B = argc
			// Arguments are in r0, r1, r2, ... r(argc-1)
			funcAddr := int(instr.A)
			argc := int(instr.B)

			// Check call depth
			if vm.frameCount >= maxCallDepth {
				return NewNull(), vm.runtimeError(fmt.Sprintf("stack overflow: max call depth %d exceeded", maxCallDepth))
			}

			// Save current frame
			vm.frames[vm.frameCount] = CallFrame{
				chunk: vm.chunk,
				ip:    vm.ip,
				fp:    vm.fp,
				regs:  vm.regs,
			}
			vm.frameCount++

			// Preserve arguments in new register set
			newRegs := make([]Value, vm.chunk.RegisterCount)
			for i := range newRegs {
				newRegs[i] = NewNull()
			}
			// Copy arguments
			for i := 0; i < argc && i < len(vm.regs); i++ {
				newRegs[i] = vm.regs[i]
			}
			vm.regs = newRegs

			// Jump to function
			vm.ip = funcAddr
			continue

		case OpRET:
			// RET: A = register containing return value
			retVal := vm.regs[instr.A]

			// Check if we're in a nested call
			if vm.frameCount == 0 {
				// Top-level return
				return retVal, nil
			}

			// Restore previous frame
			vm.frameCount--
			frame := vm.frames[vm.frameCount]
			vm.chunk = frame.chunk
			vm.ip = frame.ip
			vm.fp = frame.fp

			// Restore registers and put return value in r0
			vm.regs = frame.regs
			vm.regs[0] = retVal

		case OpCALLEXTERN:
			// CALLEXTERN: A = extern func index, B = argc
			externIdx := int(instr.A)
			argc := int(instr.B)

			// Get extern function name
			if externIdx >= len(vm.chunk.ExternNames) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("extern function index out of bounds: %d", externIdx))
			}
			externName := vm.chunk.ExternNames[externIdx]

			// Look up the extern function
			externFunc, ok := vm.externFuncs[externName]
			if !ok {
				return NewNull(), vm.runtimeError(fmt.Sprintf("unknown extern function: %s", externName))
			}

			// Collect arguments from registers
			args := make([]Value, argc)
			for i := 0; i < argc && i < len(vm.regs); i++ {
				args[i] = vm.regs[i]
			}

			// Call the extern function
			retVal, err := externFunc(vm, args)
			if err != nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("extern function %s failed: %v", externName, err))
			}

			// Store return value in r0
			vm.regs[0] = retVal

		case OpNEWSTR:
			// Create a string from string pool
			if int(instr.B) >= len(vm.chunk.Constants) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("constant index out of bounds: %d", instr.B))
			}
			constVal := vm.chunk.Constants[instr.B]
			if !constVal.IsInt() {
				return NewNull(), vm.runtimeError("NEWSTR requires string pool index")
			}
			strIdx := int(constVal.AsInt())
			if strIdx < 0 || strIdx >= len(vm.chunk.StringPool) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("string pool index out of bounds: %d", strIdx))
			}
			// Allocate string on heap
			strObj, shouldGC, err := vm.heap.AllocString(vm.chunk.StringPool[strIdx])
			if err != nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("string allocation failed: %v", err))
			}
			vm.regs[instr.A] = NewHeapPtr(uintptr(unsafe.Pointer(strObj)))
			if shouldGC {
				vm.CollectGarbage()
			}

		case OpNEWARRAY:
			// Allocate a new array
			capacity := int(instr.B)
			obj, shouldGC, err := vm.heap.AllocArray(capacity)
			if err != nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("array allocation failed: %v", err))
			}
			vm.regs[instr.A] = NewHeapPtr(uintptr(unsafe.Pointer(obj)))
			if shouldGC {
				vm.CollectGarbage()
			}

		case OpNEWOBJ:
			// Allocate a new object
			obj, shouldGC, err := vm.heap.AllocObject()
			if err != nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("object allocation failed: %v", err))
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
				return NewNull(), vm.runtimeError("GETINDEX requires array")
			}
			if !indexVal.IsInt() {
				return NewNull(), vm.runtimeError("GETINDEX requires integer index")
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
				return NewNull(), vm.runtimeError("SETINDEX requires array")
			}
			if !indexVal.IsInt() {
				return NewNull(), vm.runtimeError("SETINDEX requires integer index")
			}

			arrayObj := (*ArrayObj)(unsafe.Pointer(arrayVal.AsHeapPtr()))
			index := int(indexVal.AsInt())
			arrayObj.Set(index, value)

		case OpGETFIELD:
			// Get object field: A = dest, B = object, C = const index (field name)
			objVal := vm.regs[instr.B]
			if !objVal.IsHeapPtr() {
				return NewNull(), vm.runtimeError("GETFIELD requires object")
			}

			if int(instr.C) >= len(vm.chunk.Constants) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("constant index out of bounds: %d", instr.C))
			}

			// Get field name from constant
			fieldNameVal := vm.chunk.Constants[instr.C]
			if !fieldNameVal.IsHeapPtr() {
				return NewNull(), vm.runtimeError("GETFIELD requires string constant for field name")
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
				return NewNull(), vm.runtimeError("SETFIELD requires object")
			}

			if int(instr.B) >= len(vm.chunk.Constants) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("constant index out of bounds: %d", instr.B))
			}

			// Get field name from constant
			fieldNameVal := vm.chunk.Constants[instr.B]
			if !fieldNameVal.IsHeapPtr() {
				return NewNull(), vm.runtimeError("SETFIELD requires string constant for field name")
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
				return NewNull(), vm.runtimeError("ARRLEN requires array")
			}

			arrayObj := (*ArrayObj)(unsafe.Pointer(arrayVal.AsHeapPtr()))
			vm.regs[instr.A] = NewInt(int64(arrayObj.Len()))

		case OpGC:
			// Manually trigger garbage collection
			vm.CollectGarbage()

		default:
			return NewNull(), vm.runtimeError(fmt.Sprintf("unimplemented opcode: %v", instr.Op))
		}

		vm.ip++
	}

	return NewNull(), nil
}

package vm

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"unsafe"
)

const (
	stackSize = 4096

	// DefaultMaxCallDepth bounds how deep calls may nest, so that runaway
	// recursion ends with an error instead of exhausting memory.
	DefaultMaxCallDepth = 10000
)

// CallFrame represents a function call frame.
type CallFrame struct {
	function *FunctionObj // NEW: Current executing function (for GC and captures)
	chunk    *Chunk       // The function's bytecode (kept during transition)
	ip       int          // Return instruction pointer
	fp       int          // Frame pointer (base of this frame in stack)
	regs     []Value      // Saved registers from caller
}

// ExternFunc is a function callable from bytecode.
type ExternFunc func(*VM, []Value) (Value, error)

// DebugHook is a function called before each instruction execution.
// It receives a snapshot of the VM state and returns true if execution should pause.
type DebugHook func(snapshot interface{}) bool

// VM represents the Lumen virtual machine.
type VM struct {
	stack           [stackSize]Value
	frames          []CallFrame
	frameCount      int          // Number of active frames
	fp              int          // frame pointer
	sp              int          // stack pointer
	ip              int          // instruction pointer
	regs            []Value      // Current registers
	currentFunction *FunctionObj // NEW: Current executing function (for GC root and LDCAP)
	chunk           *Chunk       // Current chunk
	heap            *Heap
	externFuncs     map[string]ExternFunc // External functions
	debugHook       DebugHook             // Optional debug hook
	openUpvalues    []*UpvalueObj         // Linked list of open upvalues (for closure support)
	out             io.Writer             // Destination of print/println
	castTypes       map[string]*castType  // Parsed cast descriptors, by text

	externSignatures map[string]string // Declared types of external functions

	maxCallDepth int

	// Function objects that never differ between uses are created once:
	// a function without captured variables (by prototype index) and a host
	// function (by name). Without this every call of a function by its name
	// would allocate an object just to call it.
	funcObjs   map[int]Value
	externObjs map[string]Value

	// State of the native functions (see native.go)
	outcome     Value                 // What the last protected call returned (see protect.go)
	failure     *RuntimeError         // ...or how it failed
	pinned      []Value               // Values a running native keeps alive
	problem     string                // Why the last file operation failed (std.io)...
	args        []string              // What the program was started with (std.os.args)
	program     string                // The file it was run from (std.os.program)
	problemKind string                // ...and of which kind
	servers     map[int64]*httpServer // The servers of std.http, by number
	lastServer  int64
	in          *bufio.Reader // Input of std.io.readLine
	rng         *rand.Rand    // Generator of std.random
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
		out:         os.Stdout,
		castTypes:   make(map[string]*castType),

		externSignatures: make(map[string]string),
		maxCallDepth:     DefaultMaxCallDepth,
		funcObjs:         make(map[int]Value),
		externObjs:       make(map[string]Value),
	}
	vm.heap.collect = vm.CollectGarbage
	vm.registerBuiltins()
	vm.registerNativeFunctions()
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
		out:         os.Stdout,
		castTypes:   make(map[string]*castType),

		externSignatures: make(map[string]string),
		maxCallDepth:     DefaultMaxCallDepth,
		funcObjs:         make(map[int]Value),
		externObjs:       make(map[string]Value),
	}
	vm.heap.collect = vm.CollectGarbage
	vm.registerBuiltins()
	vm.registerNativeFunctions()
	return vm
}

// RegisterExtern registers an external function.
func (vm *VM) RegisterExtern(name string, fn ExternFunc) {
	vm.externFuncs[name] = fn
}

// RegisterTypedExtern registers an external function together with its
// type, written as a cast descriptor ("fn(any)->string"). A function with a
// declared type can be cast to a function type; one without cannot.
func (vm *VM) RegisterTypedExtern(name, signature string, fn ExternFunc) {
	vm.externFuncs[name] = fn
	vm.externSignatures[name] = signature
}

// SetMaxCallDepth sets how deep calls may nest (default DefaultMaxCallDepth).
func (vm *VM) SetMaxCallDepth(depth int) {
	if depth > 0 {
		vm.maxCallDepth = depth
	}
}

// SetOutput redirects the output of print/println (default: os.Stdout).
func (vm *VM) SetOutput(w io.Writer) {
	vm.out = w
}

// registerBuiltins registers built-in external functions.
func (vm *VM) registerBuiltins() {
	write := func(args []Value, end string) {
		for i, arg := range args {
			if i > 0 {
				fmt.Fprint(vm.out, " ")
			}
			fmt.Fprint(vm.out, FormatValue(arg))
		}
		fmt.Fprint(vm.out, end)
	}

	// print function
	vm.RegisterTypedExtern("print", "fn(...any)->void", func(vm *VM, args []Value) (Value, error) {
		write(args, "")
		return NewNull(), nil
	})

	// println function
	vm.RegisterTypedExtern("println", "fn(...any)->void", func(vm *VM, args []Value) (Value, error) {
		write(args, "\n")
		return NewNull(), nil
	})

	// panic function: stops the program. The message is written like the
	// arguments of print. The VM turns the error into a failure at the
	// place of the call.
	vm.RegisterTypedExtern("panic", "fn(...any)->void", func(vm *VM, args []Value) (Value, error) {
		parts := make([]string, len(args))
		for i, arg := range args {
			parts[i] = FormatValue(arg)
		}
		return NewNull(), &panicError{message: strings.Join(parts, " ")}
	})

	// toString function
	vm.RegisterTypedExtern("toString", "fn(any)->string", func(vm *VM, args []Value) (Value, error) {
		if len(args) != 1 {
			return NewNull(), fmt.Errorf("toString expects 1 argument, got %d", len(args))
		}
		return vm.newString(FormatValue(args[0]))
	})

	// __text intrinsic: an expression inside a string, "${value}"
	vm.RegisterTypedExtern("__text", "fn(any)->string", func(vm *VM, args []Value) (Value, error) {
		if len(args) != 1 {
			return NewNull(), fmt.Errorf("__text expects 1 argument, got %d", len(args))
		}
		return vm.newString(FormatValue(args[0]))
	})

	// __cast intrinsic: `value as Type`. Returns the value if it is what
	// the descriptor says, fails otherwise.
	vm.RegisterExtern("__cast", func(vm *VM, args []Value) (Value, error) {
		if len(args) != 2 {
			return NewNull(), fmt.Errorf("__cast expects 2 arguments, got %d", len(args))
		}
		descriptor, ok := stringValue(args[1])
		if !ok {
			return NewNull(), fmt.Errorf("__cast expects a type descriptor")
		}
		return vm.cast(args[0], descriptor)
	})

	// __is intrinsic: `value is Type`. True if the value is what the
	// descriptor says.
	vm.RegisterExtern("__is", func(vm *VM, args []Value) (Value, error) {
		if len(args) != 2 {
			return NewNull(), fmt.Errorf("__is expects 2 arguments, got %d", len(args))
		}
		descriptor, ok := stringValue(args[1])
		if !ok {
			return NewNull(), fmt.Errorf("__is expects a type descriptor")
		}
		typ, err := vm.castTypeFor(descriptor)
		if err != nil {
			return NewNull(), err
		}
		return NewBool(typ.mismatch(args[0]) == ""), nil
	})

	// push function: appends a value to an array
	vm.RegisterTypedExtern("push", "fn(any,any)->void", func(vm *VM, args []Value) (Value, error) {
		if len(args) != 2 {
			return NewNull(), fmt.Errorf("push expects 2 arguments, got %d", len(args))
		}
		if t, ok := heapType(args[0]); !ok || t != ObjTypeArray {
			return NewNull(), fmt.Errorf("push expects an array, got %s", typeName(args[0]))
		}
		// The caller collects garbage after the call if it is due
		return NewNull(), vm.appendToArray((*ArrayObj)(unsafe.Pointer(args[0].AsHeapPtr())), args[1])
	})
}

// Ints are 48-bit signed.
const (
	MaxInt = int64(1)<<47 - 1
	MinInt = -(int64(1) << 47)
)

// intResult makes an Int of the result of an integer operation, or fails
// if it does not fit: an Int silently wrapping around would turn a large
// positive number into a negative one.
func (vm *VM) intResult(n int64) (Value, error) {
	if n < MinInt || n > MaxInt {
		return NewNull(), vm.runtimeError("integer overflow")
	}
	return NewInt(n), nil
}

// newString allocates a string on the VM heap. It does not collect garbage:
// the caller must store the value in a register first.
func (vm *VM) newString(s string) (Value, error) {
	strObj, _, err := vm.heap.AllocString(s)
	if err != nil {
		return NewNull(), err
	}
	return NewHeapPtr(uintptr(unsafe.Pointer(strObj))), nil
}

// objectFieldSize is what one field is counted as in the heap's accounting.
const objectFieldSize = 32

// appendToArray appends a value and accounts for the memory when the array
// has to grow. Counting growth is what lets the collector's threshold
// follow the size of the data; an array that grows unseen keeps the
// threshold low, and every collection then walks the whole array again.
func (vm *VM) appendToArray(arr *ArrayObj, v Value) error {
	before := cap(arr.elements)
	arr.Append(v)
	if after := cap(arr.elements); after > before {
		_, err := vm.heap.grow(uint64(after-before) * uint64(unsafe.Sizeof(Value(0))))
		return err
	}
	return nil
}

// collectIfNeeded runs the GC when the heap has passed its threshold. Call
// it only when every live value is reachable from a register.
func (vm *VM) collectIfNeeded() {
	if vm.heap.ShouldCollect() {
		vm.CollectGarbage()
	}
}

// fieldName resolves the field name operand of GETFIELD/SETFIELD/HASFIELD/
// DELFIELD. The constant is either an index into the string pool (what the
// assembler and compiler emit) or a pre-allocated heap string.
func (vm *VM) fieldName(op string, constIdx uint16) (string, error) {
	if int(constIdx) >= len(vm.chunk.Constants) {
		return "", vm.runtimeError(fmt.Sprintf("constant index out of bounds: %d", constIdx))
	}

	constVal := vm.chunk.Constants[constIdx]
	if constVal.IsInt() {
		strIdx := int(constVal.AsInt())
		if strIdx < 0 || strIdx >= len(vm.chunk.StringPool) {
			return "", vm.runtimeError(fmt.Sprintf("string pool index out of bounds: %d", strIdx))
		}
		return vm.chunk.StringPool[strIdx], nil
	}
	if name, ok := stringValue(constVal); ok {
		return name, nil
	}
	return "", vm.runtimeError(op + " requires string constant for field name")
}

// objectOperand returns the object a field instruction operates on.
func (vm *VM) objectOperand(op string, v Value) (*ObjectObj, error) {
	if t, ok := heapType(v); !ok || t != ObjTypeObject {
		return nil, vm.runtimeError(fmt.Sprintf("%s requires object, got %s", op, typeName(v)))
	}
	return (*ObjectObj)(unsafe.Pointer(v.AsHeapPtr())), nil
}

// arrayOperand returns the array an array instruction operates on.
func (vm *VM) arrayOperand(op string, v Value) (*ArrayObj, error) {
	if t, ok := heapType(v); !ok || t != ObjTypeArray {
		return nil, vm.runtimeError(fmt.Sprintf("%s requires array, got %s", op, typeName(v)))
	}
	return (*ArrayObj)(unsafe.Pointer(v.AsHeapPtr())), nil
}

// compareStrings applies an ordering operator to two strings.
func compareStrings(op OpCode, a, b string) bool {
	switch op {
	case OpLT:
		return a < b
	case OpLE:
		return a <= b
	case OpGT:
		return a > b
	default:
		return a >= b
	}
}

// stringPair returns both operands as strings when both are strings.
func stringPair(a, b Value) (string, string, bool) {
	sa, ok := stringValue(a)
	if !ok {
		return "", "", false
	}
	sb, ok := stringValue(b)
	return sa, sb, ok
}

// valuesEqual implements EQ: strings compare by content, everything else by
// identity.
func valuesEqual(a, b Value) bool {
	if a == b {
		return true
	}
	if sa, ok := stringValue(a); ok {
		if sb, ok := stringValue(b); ok {
			return sa == sb
		}
	}
	// Values of an enum are equal when they are the same variant holding
	// equal values. Structs and arrays are only equal to themselves.
	if va, ok := variantObject(a); ok {
		if vb, ok := variantObject(b); ok {
			return variantsEqual(va, vb)
		}
	}
	return false
}

// variantObject returns the object behind a value of an enum.
func variantObject(v Value) (*ObjectObj, bool) {
	if t, ok := heapType(v); !ok || t != ObjTypeObject {
		return nil, false
	}
	obj := (*ObjectObj)(unsafe.Pointer(v.AsHeapPtr()))
	return obj, obj.variant
}

func variantsEqual(a, b *ObjectObj) bool {
	if a.typeName != b.typeName {
		return false
	}
	for i := 0; ; i++ {
		va, okA := a.fields[strconv.Itoa(i)]
		vb, okB := b.fields[strconv.Itoa(i)]
		if !okA || !okB {
			return okA == okB
		}
		if !valuesEqual(va, vb) {
			return false
		}
	}
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
		RegisterCount: len(vm.regs),
		ArgCount:      0,
	}

	// Add current instruction if in valid range
	if vm.ip < len(vm.chunk.Code) {
		snapshot.Instr = vm.chunk.Code[vm.ip]
		snapshot.DebugInfo = vm.chunk.GetDebugInfo(vm.ip)
	}

	return snapshot
}

// runtimeError creates a formatted error message with debug info.

// Exec executes a chunk of bytecode.
func (vm *VM) Exec(chunk *Chunk) (Value, error) {
	if chunk == nil {
		return NewNull(), fmt.Errorf("cannot execute nil chunk")
	}

	// Set current chunk
	vm.chunk = chunk

	mainIdx := -1
	for i, proto := range chunk.Prototypes {
		if proto.Name == "main" {
			mainIdx = i
			break
		}
	}

	if mainIdx == -1 {
		return NewNull(), fmt.Errorf("entry function 'main' not found")
	}

	mainProto := chunk.Prototypes[mainIdx]

	// Main function doesn't capture any upvalues (upvalueCount = 0)
	mainFunc, shouldGC, err := vm.heap.AllocFunction(chunk, mainIdx, 0)
	if err != nil {
		return NewNull(), fmt.Errorf("failed to allocate main function: %v", err)
	}
	if shouldGC {
		vm.CollectGarbage()
	}

	// Allocate registers for main function
	vm.regs = make([]Value, mainProto.RegisterCount)
	for i := range vm.regs {
		vm.regs[i] = NewNull()
	}

	// Set instruction pointer to main's code start
	vm.ip = mainProto.CodeStart
	vm.currentFunction = mainFunc
	vm.frameCount = 0
	vm.frames = vm.frames[:0]

	// Cached function objects belong to the chunk being run
	vm.funcObjs = make(map[int]Value)

	return vm.run(0)
}

// run executes instructions until the function that was running when it
// was called returns, and gives back what it returned. base is the number
// of frames at that moment: 0 for main, more for a function called from a
// native function (see callFunction).
func (vm *VM) run(base int) (Value, error) {
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

		case OpLOADI:
			vm.regs[instr.A] = NewInt(int64(instr.B))

		case OpADD:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() + b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				sum, err := vm.intResult(a.AsInt() + b.AsInt())
				if err != nil {
					return NewNull(), err
				}
				vm.regs[instr.A] = sum
			} else if sa, ok := stringValue(a); ok {
				// String concatenation
				sb, ok := stringValue(b)
				if !ok {
					return NewNull(), vm.runtimeError(fmt.Sprintf("ADD cannot concatenate String and %s", typeName(b)))
				}
				str, err := vm.newString(sa + sb)
				if err != nil {
					return NewNull(), vm.runtimeError(fmt.Sprintf("string allocation failed: %v", err))
				}
				vm.regs[instr.A] = str
				vm.collectIfNeeded()
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("ADD requires numeric operands, got: %s + %s", a.String(), b.String()))
			}

		case OpSUB:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() - b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				diff, err := vm.intResult(a.AsInt() - b.AsInt())
				if err != nil {
					return NewNull(), err
				}
				vm.regs[instr.A] = diff
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("SUB requires numeric operands, got: %s - %s", a.String(), b.String()))
			}

		case OpMUL:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() * b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				// Both operands fit in 48 bits, so their product can exceed
				// 64; dividing back detects that as well
				x, y := a.AsInt(), b.AsInt()
				product := x * y
				if x != 0 && product/x != y {
					return NewNull(), vm.runtimeError("integer overflow")
				}
				result, err := vm.intResult(product)
				if err != nil {
					return NewNull(), err
				}
				vm.regs[instr.A] = result
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("MUL requires numeric operands, got: %s * %s", a.String(), b.String()))
			}

		case OpDIV:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(a.AsNumber() / b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				if b.AsInt() == 0 {
					return NewNull(), vm.runtimeError("division by zero")
				}
				// The smallest Int divided by -1 has no Int result
				quotient, err := vm.intResult(a.AsInt() / b.AsInt())
				if err != nil {
					return NewNull(), err
				}
				vm.regs[instr.A] = quotient
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("DIV requires numeric operands, got: %s / %s", a.String(), b.String()))
			}

		case OpMOD:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewNumber(math.Mod(a.AsNumber(), b.AsNumber()))
			} else if a.IsInt() && b.IsInt() {
				if b.AsInt() == 0 {
					return NewNull(), vm.runtimeError("division by zero")
				}
				vm.regs[instr.A] = NewInt(a.AsInt() % b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("MOD requires numeric operands, got: %s %% %s", a.String(), b.String()))
			}

		case OpNEG:
			a := vm.regs[instr.B]
			switch {
			case a.IsNumber():
				vm.regs[instr.A] = NewNumber(-a.AsNumber())
			case a.IsInt():
				negated, err := vm.intResult(-a.AsInt())
				if err != nil {
					return NewNull(), err
				}
				vm.regs[instr.A] = negated
			default:
				return NewNull(), vm.runtimeError(fmt.Sprintf("NEG requires numeric operand, got: %s", a.String()))
			}

		case OpNOT:
			a := vm.regs[instr.B]
			if !a.IsBool() {
				return NewNull(), vm.runtimeError(fmt.Sprintf("NOT requires boolean operand, got: %s", a.String()))
			}
			vm.regs[instr.A] = NewBool(!a.AsBool())

		case OpTOINT:
			a := vm.regs[instr.B]
			switch {
			case a.IsInt():
				vm.regs[instr.A] = a
			case a.IsNumber():
				vm.regs[instr.A] = NewInt(int64(math.Trunc(a.AsNumber())))
			default:
				return NewNull(), vm.runtimeError(fmt.Sprintf("TOINT requires number or int, got: %s", a.String()))
			}

		case OpTONUM:
			a := vm.regs[instr.B]
			switch {
			case a.IsNumber():
				vm.regs[instr.A] = a
			case a.IsInt():
				vm.regs[instr.A] = NewNumber(float64(a.AsInt()))
			default:
				return NewNull(), vm.runtimeError(fmt.Sprintf("TONUM requires int or number, got: %s", a.String()))
			}

		case OpBAND:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() & b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("BAND requires integer operands, got: %s & %s", a.String(), b.String()))
			}

		case OpBOR:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() | b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("BOR requires integer operands, got: %s | %s", a.String(), b.String()))
			}

		case OpBXOR:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewInt(a.AsInt() ^ b.AsInt())
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("BXOR requires integer operands, got: %s ^ %s", a.String(), b.String()))
			}

		case OpSHL:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsInt() && b.IsInt() {
				shift := b.AsInt()
				if shift < 0 {
					return NewNull(), vm.runtimeError("SHL shift count must be non-negative")
				}
				vm.regs[instr.A] = NewInt(a.AsInt() << shift)
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("SHL requires integer operands, got: %s << %s", a.String(), b.String()))
			}

		case OpSHR:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsInt() && b.IsInt() {
				shift := b.AsInt()
				if shift < 0 {
					return NewNull(), vm.runtimeError("SHR shift count must be non-negative")
				}
				vm.regs[instr.A] = NewInt(a.AsInt() >> shift)
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("SHR requires integer operands, got: %s >> %s", a.String(), b.String()))
			}

		case OpEQ:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			vm.regs[instr.A] = NewBool(valuesEqual(a, b))

		case OpNE:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			vm.regs[instr.A] = NewBool(!valuesEqual(a, b))

		case OpLT:
			a := vm.regs[instr.B]
			b := vm.regs[instr.C]
			if a.IsNumber() && b.IsNumber() {
				vm.regs[instr.A] = NewBool(a.AsNumber() < b.AsNumber())
			} else if a.IsInt() && b.IsInt() {
				vm.regs[instr.A] = NewBool(a.AsInt() < b.AsInt())
			} else if sa, sb, ok := stringPair(a, b); ok {
				vm.regs[instr.A] = NewBool(compareStrings(OpLT, sa, sb))
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
			} else if sa, sb, ok := stringPair(a, b); ok {
				vm.regs[instr.A] = NewBool(compareStrings(OpLE, sa, sb))
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
			} else if sa, sb, ok := stringPair(a, b); ok {
				vm.regs[instr.A] = NewBool(compareStrings(OpGT, sa, sb))
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
			} else if sa, sb, ok := stringPair(a, b); ok {
				vm.regs[instr.A] = NewBool(compareStrings(OpGE, sa, sb))
			} else {
				return NewNull(), vm.runtimeError(fmt.Sprintf("GE requires numeric operands, got: %s >= %s", a.String(), b.String()))
			}

		case OpLONG:
			// LONG prefix: next instruction uses 32-bit offset
			// Read the opcode instruction
			vm.ip++
			if vm.ip >= len(vm.chunk.Code) {
				return NewNull(), vm.runtimeError("LONG: unexpected end of code")
			}
			opcodeInstr := vm.chunk.Code[vm.ip]

			// Read the offset instruction
			vm.ip++
			if vm.ip >= len(vm.chunk.Code) {
				return NewNull(), vm.runtimeError("LONG: missing offset word")
			}
			offsetInstr := vm.chunk.Code[vm.ip]
			offset := DecodeOffset32(offsetInstr)

			// Calculate target: current position (after the 3-word block) + offset
			targetIP := vm.ip + 1 + int(offset)

			switch opcodeInstr.Op {
			case OpJMP32:
				vm.ip = targetIP
				continue

			case OpJMPIF32:
				cond := vm.regs[opcodeInstr.A]
				if cond.IsBool() && cond.AsBool() {
					vm.ip = targetIP
					continue
				}

			case OpJMPIFNOT32:
				cond := vm.regs[opcodeInstr.A]
				if cond.IsBool() && !cond.AsBool() {
					vm.ip = targetIP
					continue
				}

			default:
				return NewNull(), vm.runtimeError(fmt.Sprintf("LONG: invalid opcode %v", opcodeInstr.Op))
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
			// CALL: A = func_reg (register containing FunctionObj or ExternFunctionObj), B = nargs
			// Arguments are in r(func_reg+1), r(func_reg+2), ... r(func_reg+nargs)
			funcReg := int(instr.A)
			nargs := int(instr.B)

			// Get function object from register
			funcVal := vm.regs[funcReg]
			if !funcVal.IsHeapPtr() {
				return NewNull(), vm.runtimeError(fmt.Sprintf("CALL requires function object in register r%d, got %v", funcReg, funcVal))
			}

			// Get object header to determine type
			objHeader := (*ObjHeader)(unsafe.Pointer(uintptr(funcVal.AsHeapPtr())))

			switch objHeader.Type {
			case ObjTypeFunction:
				// User-defined function
				funcObj := (*FunctionObj)(unsafe.Pointer(uintptr(funcVal.AsHeapPtr())))

				// Get function prototype
				proto := funcObj.Proto()

				// STRICT validation: argument count must match exactly
				if nargs != proto.ArgCount {
					return NewNull(), vm.runtimeError(fmt.Sprintf("function %s expects %d arguments, got %d", proto.Name, proto.ArgCount, nargs))
				}

				// Check call depth
				if vm.frameCount >= vm.maxCallDepth {
					return NewNull(), vm.runtimeError(fmt.Sprintf("stack overflow: max call depth %d exceeded", vm.maxCallDepth))
				}

				// Save current frame
				vm.frames = append(vm.frames[:vm.frameCount], CallFrame{
					function: vm.currentFunction,
					chunk:    vm.chunk,
					ip:       vm.ip,
					fp:       vm.fp,
					regs:     vm.regs,
				})
				vm.frameCount++

				// Allocate new register set for called function
				newRegs := make([]Value, proto.RegisterCount)
				for i := range newRegs {
					newRegs[i] = NewNull()
				}

				// Copy arguments to new register set
				// Arguments are in caller's registers: r(funcReg+1) through r(funcReg+nargs)
				for i := 0; i < nargs; i++ {
					newRegs[i] = vm.regs[funcReg+1+i]
				}

				// Set up VM state for called function
				vm.currentFunction = funcObj
				vm.chunk = funcObj.chunk
				vm.regs = newRegs
				vm.ip = proto.CodeStart
				continue

			case ObjTypeExternFunction:
				// External (host) function
				externObj := (*ExternFunctionObj)(unsafe.Pointer(uintptr(funcVal.AsHeapPtr())))

				// Optional argument count validation (if argCount >= 0)
				if externObj.ArgCount() >= 0 && nargs != externObj.ArgCount() {
					return NewNull(), vm.runtimeError(fmt.Sprintf("extern function %s expects %d arguments, got %d", externObj.Name(), externObj.ArgCount(), nargs))
				}

				// Collect arguments from registers
				args := make([]Value, nargs)
				for i := 0; i < nargs; i++ {
					args[i] = vm.regs[funcReg+1+i]
				}

				// Call the extern function. It may pin values; a native that
				// calls back into the program (see callFunction) runs other
				// natives meanwhile, whose pins come and go above its own.
				pinned := len(vm.pinned)
				retVal, err := externObj.Function()(vm, args)
				if err != nil {
					vm.pinned = vm.pinned[:pinned]
					if failure, ok := err.(*RuntimeError); ok {
						// Raised by the program the function called back
						// into: already complete, with its own place
						return NewNull(), failure
					}
					if _, leaving := err.(*exitError); leaving {
						// The program asked to end: not a failure
						return NewNull(), err
					}
					if stop, ok := err.(*panicError); ok {
						return NewNull(), vm.runtimeErrorOf(KindPanic, stop.message)
					}
					if IsNative(externObj.Name()) && !strings.HasPrefix(err.Error(), "out of memory") {
						// A function of the standard library: named as
						// the program names it
						return NewNull(), vm.runtimeError(externObj.Name() + ": " + err.Error())
					}
					if strings.HasPrefix(externObj.Name(), "__") || strings.HasPrefix(err.Error(), "out of memory") {
						// Compiler intrinsic: the user wrote an operator,
						// not a call, so the function name means nothing.
						// Running out of memory is not the function's
						// doing either.
						return NewNull(), vm.runtimeError(err.Error())
					}
					return NewNull(), vm.runtimeError(fmt.Sprintf("extern function %s failed: %v", externObj.Name(), err))
				}

				// Store return value in r0. From here it is a root, and
				// whatever the function kept alive no longer needs to be
				vm.regs[0] = retVal
				vm.pinned = vm.pinned[:pinned]

				// Extern functions may allocate; collect now that the result is rooted
				vm.collectIfNeeded()

			default:
				return NewNull(), vm.runtimeError(fmt.Sprintf("CALL requires function object, got %v", objHeader.Type))
			}

		case OpRET:
			// RET: A = register containing return value
			retVal := vm.regs[instr.A]

			// Close any upvalues that were pointing to this frame's registers
			// This moves captured variables from stack to heap
			vm.closeUpvalues(0)

			// The function this run was started for has returned
			if vm.frameCount == base {
				return retVal, nil
			}

			// Restore previous frame
			vm.frameCount--
			frame := vm.frames[vm.frameCount]
			vm.currentFunction = frame.function
			vm.chunk = frame.chunk
			vm.ip = frame.ip
			vm.fp = frame.fp

			// Restore registers and put return value in r0
			vm.regs = frame.regs
			vm.regs[0] = retVal

		case OpNEWFUNC:
			// NEWFUNC: A = dest_reg, B = proto_idx
			// Creates a function object. If the prototype specifies upvalues,
			// they are allocated but not initialized.
			protoIdx := int(instr.B)

			// Validate proto index
			if protoIdx < 0 || protoIdx >= len(vm.chunk.Prototypes) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("invalid prototype index: %d (chunk has %d prototypes)", protoIdx, len(vm.chunk.Prototypes)))
			}

			proto := vm.chunk.Prototypes[protoIdx]

			// A function that captures nothing is the same object every
			// time; only closures need one of their own
			if cached, ok := vm.funcObjs[protoIdx]; ok && proto.UpvalueCount == 0 {
				vm.regs[instr.A] = cached
				break
			}

			// Allocate function object on heap
			funcObj, shouldGC, err := vm.heap.AllocFunction(vm.chunk, protoIdx, proto.UpvalueCount)
			if err != nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("function allocation failed: %v", err))
			}

			// Store function object pointer in destination register
			vm.regs[instr.A] = NewHeapPtr(uintptr(unsafe.Pointer(funcObj)))
			if proto.UpvalueCount == 0 {
				vm.funcObjs[protoIdx] = vm.regs[instr.A]
			}

			// Trigger GC if needed
			if shouldGC {
				vm.CollectGarbage()
			}

		case OpCAPLOCAL:
			// CAPLOCAL: A = dest_func_reg, B = upvalue_idx, C = local_reg_idx
			destReg := int(instr.A)
			upvalIdx := int(instr.B)
			localReg := int(instr.C)

			// Get destination function
			destVal := vm.regs[destReg]
			if !destVal.IsHeapPtr() {
				return NewNull(), vm.runtimeError(fmt.Sprintf("CAPLOCAL requires function object in r%d", destReg))
			}

			// We assume it's a function object because the assembler/compiler should ensure it.
			// But for safety we could check type.
			funcObj := (*FunctionObj)(unsafe.Pointer(uintptr(destVal.AsHeapPtr())))

			if upvalIdx >= len(funcObj.Upvalues()) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("upvalue index out of bounds: %d", upvalIdx))
			}

			// Capture local variable
			upval := vm.findOrCreateUpvalue(&vm.regs[localReg])
			funcObj.SetUpvalue(upvalIdx, upval)

		case OpCAPUPVAL:
			// CAPUPVAL: A = dest_func_reg, B = upvalue_idx, C = parent_upval_idx
			destReg := int(instr.A)
			upvalIdx := int(instr.B)
			parentUpvalIdx := int(instr.C)

			// Get destination function
			destVal := vm.regs[destReg]
			if !destVal.IsHeapPtr() {
				return NewNull(), vm.runtimeError(fmt.Sprintf("CAPUPVAL requires function object in r%d", destReg))
			}

			funcObj := (*FunctionObj)(unsafe.Pointer(uintptr(destVal.AsHeapPtr())))

			if upvalIdx >= len(funcObj.Upvalues()) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("upvalue index out of bounds: %d", upvalIdx))
			}

			// Capture from parent upvalues
			parentFunc := vm.currentFunction
			if parentUpvalIdx >= len(parentFunc.Upvalues()) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("parent upvalue index out of bounds: %d", parentUpvalIdx))
			}

			funcObj.SetUpvalue(upvalIdx, parentFunc.Upvalues()[parentUpvalIdx])

		case OpNEWEXFUNC:
			// NEWEXFUNC: A = dest_reg, B = extern_idx
			externIdx := int(instr.B)

			// Get extern function name
			if externIdx >= len(vm.chunk.ExternNames) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("extern function index out of bounds: %d", externIdx))
			}
			externName := vm.chunk.ExternNames[externIdx]

			// One object per host function is enough
			if cached, ok := vm.externObjs[externName]; ok {
				vm.regs[instr.A] = cached
				break
			}

			// Look up the extern function
			externFunc, ok := vm.externFuncs[externName]
			if !ok {
				return NewNull(), vm.runtimeError(fmt.Sprintf("unknown extern function: %s", externName))
			}

			// Allocate external function object on heap
			// Note: argCount is set to -1 to indicate variadic (no strict check)
			// In the future, we could add metadata to specify exact arg count
			funcObj, shouldGC, err := vm.heap.AllocExternFunction(externName, externFunc, -1)
			if err != nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("extern function allocation failed: %v", err))
			}

			funcObj.signature = vm.externSignatures[externName]

			// Store function object pointer in destination register
			vm.regs[instr.A] = NewHeapPtr(uintptr(unsafe.Pointer(funcObj)))
			vm.externObjs[externName] = vm.regs[instr.A]

			// Trigger GC if needed
			if shouldGC {
				vm.CollectGarbage()
			}

		case OpCLOSEUP:
			// CLOSEUP: A = register. Closures that captured this register
			// get their own copy of its value and stop following it. Used
			// at the end of a loop iteration, so that each iteration's
			// closures keep that iteration's variables.
			vm.closeUpvalueAt(&vm.regs[instr.A])

		case OpLDCAP:
			// LDCAP: A = dest_reg, B = upvalue_idx
			// Load value from captured upvalue
			upvalIdx := int(instr.B)

			currentFunc := vm.currentFunction
			if upvalIdx >= len(currentFunc.Upvalues()) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("LDCAP: upvalue index out of bounds: %d", upvalIdx))
			}

			upval := currentFunc.Upvalues()[upvalIdx]
			if upval == nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("LDCAP: uninitialized upvalue at index %d", upvalIdx))
			}

			vm.regs[instr.A] = upval.Get()

		case OpSTCAP:
			// STCAP: A = upvalue_idx, B = src_reg
			// Store value to captured upvalue
			upvalIdx := int(instr.A)
			srcReg := int(instr.B)

			currentFunc := vm.currentFunction
			if upvalIdx >= len(currentFunc.Upvalues()) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("STCAP: upvalue index out of bounds: %d", upvalIdx))
			}

			upval := currentFunc.Upvalues()[upvalIdx]
			if upval == nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("STCAP: uninitialized upvalue at index %d", upvalIdx))
			}

			upval.Set(vm.regs[srcReg])

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
			// Allocate a new object: A = dest. With C = 1, B is the constant
			// index of the name of the struct the object is a value of.
			obj, shouldGC, err := vm.heap.AllocObject()
			if err != nil {
				return NewNull(), vm.runtimeError(fmt.Sprintf("object allocation failed: %v", err))
			}
			if instr.C == ObjectIsNamed || instr.C == ObjectIsVariant {
				name, err := vm.fieldName("NEWOBJ", instr.B)
				if err != nil {
					return NewNull(), err
				}
				obj.typeName = name
				obj.variant = instr.C == ObjectIsVariant
			}
			vm.regs[instr.A] = NewHeapPtr(uintptr(unsafe.Pointer(obj)))
			if shouldGC {
				vm.CollectGarbage()
			}

		case OpGETINDEX:
			// Get element: A = dest, B = array or string, C = index
			indexVal := vm.regs[instr.C]
			if !indexVal.IsInt() {
				return NewNull(), vm.runtimeError("GETINDEX requires integer index")
			}
			index := int(indexVal.AsInt())

			if str, ok := stringValue(vm.regs[instr.B]); ok {
				// Indexing a string yields the character as a string
				chars := []rune(str)
				if index < 0 || index >= len(chars) {
					return NewNull(), vm.runtimeError(fmt.Sprintf("index out of bounds: %d (length %d)", index, len(chars)))
				}
				char, err := vm.newString(string(chars[index]))
				if err != nil {
					return NewNull(), vm.runtimeError(fmt.Sprintf("string allocation failed: %v", err))
				}
				vm.regs[instr.A] = char
				vm.collectIfNeeded()
				break
			}

			arrayObj, err := vm.arrayOperand("GETINDEX", vm.regs[instr.B])
			if err != nil {
				return NewNull(), err
			}
			if index < 0 || index >= arrayObj.Len() {
				return NewNull(), vm.runtimeError(fmt.Sprintf("index out of bounds: %d (length %d)", index, arrayObj.Len()))
			}
			vm.regs[instr.A] = arrayObj.Get(index)

		case OpSETINDEX:
			// Set array element: A = array, B = index, C = value
			indexVal := vm.regs[instr.B]
			if !indexVal.IsInt() {
				return NewNull(), vm.runtimeError("SETINDEX requires integer index")
			}
			index := int(indexVal.AsInt())

			arrayObj, err := vm.arrayOperand("SETINDEX", vm.regs[instr.A])
			if err != nil {
				return NewNull(), err
			}
			// Writing past the length grows the array, up to its capacity
			// (which was accounted for when the array was created)
			if index < 0 || index >= cap(arrayObj.elements) {
				return NewNull(), vm.runtimeError(fmt.Sprintf("index out of bounds: %d (length %d)", index, arrayObj.Len()))
			}
			arrayObj.Set(index, vm.regs[instr.C])

		case OpSETELEM:
			// Replace an array element: A = array, B = index, C = value.
			// Unlike SETINDEX it never grows the array: the index has to
			// name an existing element. This is what `xs[i] = v` compiles to.
			indexVal := vm.regs[instr.B]
			if !indexVal.IsInt() {
				return NewNull(), vm.runtimeError("SETELEM requires integer index")
			}
			index := int(indexVal.AsInt())

			arrayObj, err := vm.arrayOperand("SETELEM", vm.regs[instr.A])
			if err != nil {
				return NewNull(), err
			}
			if index < 0 || index >= arrayObj.Len() {
				return NewNull(), vm.runtimeError(fmt.Sprintf("index out of bounds: %d (length %d)", index, arrayObj.Len()))
			}
			arrayObj.Set(index, vm.regs[instr.C])

		case OpGETFIELD:
			// Get object field: A = dest, B = object, C = const index (field name)
			objObj, err := vm.objectOperand("GETFIELD", vm.regs[instr.B])
			if err != nil {
				return NewNull(), err
			}
			fieldName, err := vm.fieldName("GETFIELD", instr.C)
			if err != nil {
				return NewNull(), err
			}

			if value, ok := objObj.Get(fieldName); ok {
				vm.regs[instr.A] = value
			} else {
				vm.regs[instr.A] = NewNull()
			}

		case OpSETFIELD:
			// Set object field: A = object, B = const index (field name), C = value
			objObj, err := vm.objectOperand("SETFIELD", vm.regs[instr.A])
			if err != nil {
				return NewNull(), err
			}
			fieldName, err := vm.fieldName("SETFIELD", instr.B)
			if err != nil {
				return NewNull(), err
			}
			if _, exists := objObj.Get(fieldName); !exists {
				// A new field makes the object larger
				if _, err := vm.heap.grow(objectFieldSize); err != nil {
					return NewNull(), vm.runtimeError(err.Error())
				}
			}
			objObj.Set(fieldName, vm.regs[instr.C])
			vm.collectIfNeeded()

		case OpARRLEN:
			// Get length: A = dest, B = array or string.
			// On an object it reads the field named "length".
			val := vm.regs[instr.B]
			if str, ok := stringValue(val); ok {
				vm.regs[instr.A] = NewInt(int64(len([]rune(str))))
				break
			}
			if t, ok := heapType(val); ok && t == ObjTypeObject {
				field, _ := (*ObjectObj)(unsafe.Pointer(val.AsHeapPtr())).Get("length")
				vm.regs[instr.A] = field
				break
			}

			arrayObj, err := vm.arrayOperand("ARRLEN", val)
			if err != nil {
				return NewNull(), err
			}
			vm.regs[instr.A] = NewInt(int64(arrayObj.Len()))

		case OpARRPUSH:
			// Push value into array: A = array, B = value
			arrayObj, err := vm.arrayOperand("ARRPUSH", vm.regs[instr.A])
			if err != nil {
				return NewNull(), err
			}
			if err := vm.appendToArray(arrayObj, vm.regs[instr.B]); err != nil {
				return NewNull(), vm.runtimeError(err.Error())
			}
			vm.collectIfNeeded()

		case OpHASFIELD:
			// Check if object has field: A = dest bool, B = object, C = const index
			objObj, err := vm.objectOperand("HASFIELD", vm.regs[instr.B])
			if err != nil {
				return NewNull(), err
			}
			fieldName, err := vm.fieldName("HASFIELD", instr.C)
			if err != nil {
				return NewNull(), err
			}

			_, ok := objObj.Get(fieldName)
			vm.regs[instr.A] = NewBool(ok)

		case OpDELFIELD:
			// Delete object field: A = dest bool, B = object, C = const index
			objObj, err := vm.objectOperand("DELFIELD", vm.regs[instr.B])
			if err != nil {
				return NewNull(), err
			}
			fieldName, err := vm.fieldName("DELFIELD", instr.C)
			if err != nil {
				return NewNull(), err
			}

			_, existed := objObj.fields[fieldName]
			delete(objObj.fields, fieldName)
			vm.regs[instr.A] = NewBool(existed)

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

// currentFrame returns the current call frame, or nil if there are no active frames.
func (vm *VM) currentFrame() *CallFrame {
	if vm.frameCount == 0 {
		return nil
	}
	return &vm.frames[vm.frameCount-1]
}

// findOrCreateUpvalue finds an existing open upvalue for the given stack location,
// or creates a new one if it doesn't exist.
// This ensures that multiple closures capturing the same variable share the same upvalue.
func (vm *VM) findOrCreateUpvalue(location *Value) *UpvalueObj {
	// Search for existing open upvalue for this location
	for _, upval := range vm.openUpvalues {
		if upval.location == location {
			return upval
		}
	}

	// Create new upvalue
	upval, shouldGC, err := vm.heap.AllocUpvalue(location)
	if err != nil {
		// If allocation fails, we can't recover gracefully here
		// In a real implementation, this would need better error handling
		panic(fmt.Sprintf("failed to allocate upvalue: %v", err))
	}

	// Add to open upvalues list
	vm.openUpvalues = append(vm.openUpvalues, upval)

	if shouldGC {
		vm.CollectGarbage()
	}

	return upval
}

// closeUpvalueAt closes the open upvalue for one register, if there is one.
func (vm *VM) closeUpvalueAt(location *Value) {
	for i, upval := range vm.openUpvalues {
		if upval.location == location {
			upval.Close()
			vm.openUpvalues = append(vm.openUpvalues[:i], vm.openUpvalues[i+1:]...)
			return
		}
	}
}

// closeUpvalues closes all upvalues that point to stack slots at or above the given index.
// This is called when a function returns to move captured variables from stack to heap.
func (vm *VM) closeUpvalues(lastSlot int) {
	i := 0
	for i < len(vm.openUpvalues) {
		upval := vm.openUpvalues[i]
		if upval.IsOpen() {
			// Check if this upvalue points to a register that's being closed
			// We need to compare pointers to determine if it's in the range being closed
			regIndex := -1
			for idx := range vm.regs {
				if &vm.regs[idx] == upval.location {
					regIndex = idx
					break
				}
			}

			if regIndex >= lastSlot {
				// Close this upvalue
				upval.Close()
				// Remove from open list
				vm.openUpvalues = append(vm.openUpvalues[:i], vm.openUpvalues[i+1:]...)
				continue
			}
		}
		i++
	}
}

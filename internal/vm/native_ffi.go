package vm

import (
	"fmt"
	"math"
	"unsafe"
)

// The natives of std.ffi: memory outside the program, for the functions of
// C libraries that fill what they are given the address of (see ffi.go).
//
// An address is an Int. Nothing here can tell a good one from a bad one:
// reading where there is nothing ends lumen as it would end a C program.
// It is the one module of the library that can do that, and a program
// that is not permitted everything is not permitted it.

// foreignError is a failure of a function of a C library, named as the
// program names it.
func (vm *VM) foreignError(f *Foreign, message string) error {
	return vm.runtimeErrorOf(KindLibrary, f.Name+": "+message)
}

// onMainTask reports whether the task that is running is main.
func (vm *VM) onMainTask() bool {
	vm.sched.mu.Lock()
	defer vm.sched.mu.Unlock()
	return vm.sched.running == vm.sched.main
}

// foreignObject is externObject for a function of a C library: found in
// its library the first time the program uses it.
func (vm *VM) foreignObject(name string) (Value, error) {
	described, err := ParseForeign(name)
	if err != nil {
		return NewNull(), vm.runtimeErrorOf(KindLibrary, err.Error())
	}
	if vm.permitted != nil && !vm.permitted(name) {
		return NewNull(), vm.foreignError(described, "this program is not permitted to use the functions of C libraries")
	}
	function, err := vm.foreignFunction(described)
	if err != nil {
		return NewNull(), vm.foreignError(described, err.Error())
	}
	funcObj, _, err := vm.heap.AllocExternFunction(described.Name, function, -1)
	if err != nil {
		return NewNull(), vm.runtimeError(fmt.Sprintf("extern function allocation failed: %v", err))
	}
	value := valueOf(&funcObj.header)
	vm.externObjs[name] = value
	return value, nil
}

// at is the memory at an address. Written this way because an address
// that comes from C is not one Go knows anything about.
func at(address, offset int64) unsafe.Pointer {
	p := uintptr(address + offset)
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// argAddress reads the address and the offset every read and write starts
// with.
func argAddress(args []Value) (address, offset int64, err error) {
	if address, err = argInt(args, 0); err != nil {
		return 0, 0, err
	}
	if offset, err = argInt(args, 1); err != nil {
		return 0, 0, err
	}
	if address == 0 {
		return 0, 0, fmt.Errorf("the address is 0: there is nothing there")
	}
	if address < 0 || address+offset <= 0 {
		return 0, 0, fmt.Errorf("%d + %d is not an address", address, offset)
	}
	return address, offset, nil
}

// memoryKind is one kind of value memory holds: how it is read, and how
// it is written.
type memoryKind struct {
	name  string
	kind  string
	read  func(p unsafe.Pointer) Value
	write func(p unsafe.Pointer, v Value)
}

var memoryKinds = []memoryKind{
	{"Int8", CInt8, func(p unsafe.Pointer) Value { return NewInt(int64(*(*int8)(p))) }, func(p unsafe.Pointer, v Value) { *(*int8)(p) = int8(v.AsInt()) }},
	{"UInt8", CUInt8, func(p unsafe.Pointer) Value { return NewInt(int64(*(*uint8)(p))) }, func(p unsafe.Pointer, v Value) { *(*uint8)(p) = uint8(v.AsInt()) }},
	{"Int16", CInt16, func(p unsafe.Pointer) Value { return NewInt(int64(*(*int16)(p))) }, func(p unsafe.Pointer, v Value) { *(*int16)(p) = int16(v.AsInt()) }},
	{"UInt16", CUInt16, func(p unsafe.Pointer) Value { return NewInt(int64(*(*uint16)(p))) }, func(p unsafe.Pointer, v Value) { *(*uint16)(p) = uint16(v.AsInt()) }},
	{"Int32", CInt32, func(p unsafe.Pointer) Value { return NewInt(int64(*(*int32)(p))) }, func(p unsafe.Pointer, v Value) { *(*int32)(p) = int32(v.AsInt()) }},
	{"UInt32", CUInt32, func(p unsafe.Pointer) Value { return NewInt(int64(*(*uint32)(p))) }, func(p unsafe.Pointer, v Value) { *(*uint32)(p) = uint32(v.AsInt()) }},
	{"Int64", CInt64, nil, func(p unsafe.Pointer, v Value) { *(*int64)(p) = v.AsInt() }},
	{"Pointer", CPointer, nil, func(p unsafe.Pointer, v Value) { *(*uintptr)(p) = uintptr(v.AsInt()) }},
}

func init() {
	list := []native{
		// The address of a number of bytes, all 0, that are the program's
		// until it frees them.
		{"alloc", 1, func(vm *VM, args []Value) (Value, error) {
			bytes, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if bytes < 0 || bytes > 1<<32 {
				return NewNull(), fmt.Errorf("%d bytes cannot be asked for", bytes)
			}
			address, err := cAlloc(uintptr(bytes))
			if err != nil {
				return NewNull(), err
			}
			return vm.intResult(int64(address))
		}},
		// Gives back what alloc and cstring returned. Freeing 0 does nothing.
		{"free", 1, func(vm *VM, args []Value) (Value, error) {
			address, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if address < 0 {
				return NewNull(), fmt.Errorf("%d is not an address", address)
			}
			if address == 0 {
				return NewNull(), nil
			}
			return NewNull(), cFree(uintptr(address))
		}},
		// A copy of a text as C wants it, with a 0 at its end: for a
		// function that keeps the text it is given. To be freed.
		{"cstring", 1, func(vm *VM, args []Value) (Value, error) {
			text, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			address, err := cAlloc(uintptr(len(text)) + 1)
			if err != nil {
				return NewNull(), err
			}
			copy(unsafe.Slice((*byte)(at(int64(address), 0)), len(text)), text)
			return vm.intResult(int64(address))
		}},
		// The text of C at an address: what is there up to the first 0.
		{"text", 1, func(vm *VM, args []Value) (Value, error) {
			address, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if address <= 0 {
				return NewNull(), fmt.Errorf("%d is not the address of a text", address)
			}
			length, err := cLength(uintptr(address))
			if err != nil {
				return NewNull(), err
			}
			if length > math.MaxInt32 {
				return NewNull(), fmt.Errorf("the text at %d has no end", address)
			}
			return vm.newString(string(unsafe.Slice((*byte)(at(address, 0)), int(length))))
		}},
		{"readFloat32", 2, func(vm *VM, args []Value) (Value, error) {
			address, offset, err := argAddress(args)
			if err != nil {
				return NewNull(), err
			}
			return NewNumber(float64(*(*float32)(at(address, offset)))), nil
		}},
		{"readFloat64", 2, func(vm *VM, args []Value) (Value, error) {
			address, offset, err := argAddress(args)
			if err != nil {
				return NewNull(), err
			}
			return NewNumber(*(*float64)(at(address, offset))), nil
		}},
		{"writeFloat32", 3, func(vm *VM, args []Value) (Value, error) {
			address, offset, err := argAddress(args)
			if err != nil {
				return NewNull(), err
			}
			value, err := argFloat(args, 2)
			if err != nil {
				return NewNull(), err
			}
			*(*float32)(at(address, offset)) = float32(value)
			return NewNull(), nil
		}},
		{"writeFloat64", 3, func(vm *VM, args []Value) (Value, error) {
			address, offset, err := argAddress(args)
			if err != nil {
				return NewNull(), err
			}
			value, err := argFloat(args, 2)
			if err != nil {
				return NewNull(), err
			}
			*(*float64)(at(address, offset)) = value
			return NewNull(), nil
		}},
		// The two that may not fit an Int
		{"readInt64", 2, func(vm *VM, args []Value) (Value, error) {
			address, offset, err := argAddress(args)
			if err != nil {
				return NewNull(), err
			}
			return vm.intResult(*(*int64)(at(address, offset)))
		}},
		{"readPointer", 2, func(vm *VM, args []Value) (Value, error) {
			address, offset, err := argAddress(args)
			if err != nil {
				return NewNull(), err
			}
			found := *(*uintptr)(at(address, offset))
			if uint64(found) > uint64(MaxInt) {
				return NewNull(), fmt.Errorf("%d does not fit an Int", found)
			}
			return NewInt(int64(found)), nil
		}},
	}

	for _, kind := range memoryKinds {
		kind := kind
		if kind.read != nil {
			list = append(list, native{"read" + kind.name, 2, func(vm *VM, args []Value) (Value, error) {
				address, offset, err := argAddress(args)
				if err != nil {
					return NewNull(), err
				}
				return kind.read(at(address, offset)), nil
			}})
		}
		list = append(list, native{"write" + kind.name, 3, func(vm *VM, args []Value) (Value, error) {
			address, offset, err := argAddress(args)
			if err != nil {
				return NewNull(), err
			}
			value, err := argInt(args, 2)
			if err != nil {
				return NewNull(), err
			}
			if low, high := cIntRange(kind.kind); value < low || value > high {
				return NewNull(), fmt.Errorf("%d does not fit %s", value, kind.name)
			}
			kind.write(at(address, offset), NewInt(value))
			return NewNull(), nil
		}})
	}
	registerNatives("std.ffi", list...)
}

// BindForeign finds a function of a C library as the program would when it
// first calls it, without calling it: for the tools and tests that check
// what a binding declares against the library it is for.
func (vm *VM) BindForeign(name string) error {
	described, err := ParseForeign(name)
	if err != nil {
		return err
	}
	_, err = vm.foreignFunction(described)
	return err
}

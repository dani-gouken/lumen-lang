package vm

import "unsafe"

// CollectGarbage runs a mark-and-sweep garbage collection cycle.
func (vm *VM) CollectGarbage() {
	// Mark phase: mark all reachable objects
	vm.markRoots()

	// Sweep phase: free unmarked objects
	vm.heap.sweep()
}

// markRoots marks all objects reachable from GC roots.
func (vm *VM) markRoots() {
	// Mark current function
	if vm.currentFunction != nil {
		vm.markFunction(vm.currentFunction)
	}

	// Mark objects in registers
	for i := 0; i < len(vm.regs); i++ {
		vm.markValue(vm.regs[i])
	}

	// Mark objects on the stack
	for i := 0; i < vm.sp; i++ {
		vm.markValue(vm.stack[i])
	}

	// Mark saved registers and functions in call frames
	for i := 0; i < vm.frameCount; i++ {
		frame := &vm.frames[i]
		if frame.function != nil {
			vm.markFunction(frame.function)
		}
		for j := 0; j < len(frame.regs); j++ {
			vm.markValue(frame.regs[j])
		}
	}

	// Function objects kept for reuse (see functionObject, NEWEXFUNC)
	for _, cached := range vm.funcObjs {
		vm.markValue(cached)
	}
	for _, cached := range vm.externObjs {
		vm.markValue(cached)
	}

	// Mark open upvalues (they are roots because they point to stack)
	for _, upval := range vm.openUpvalues {
		if upval != nil && !upval.header.Marked {
			upval.header.Marked = true
			// Open upvalues point to stack, which is marked by stack scan.
		}
	}
}

// markValue marks a value and recursively marks its children.
func (vm *VM) markValue(v Value) {
	if !v.IsHeapPtr() {
		return
	}

	ptr := v.AsHeapPtr()
	if ptr == 0 {
		return
	}

	// Get the object header
	obj := (*ObjHeader)(unsafe.Pointer(ptr))

	// Already marked?
	if obj.Marked {
		return
	}

	// Mark this object
	obj.Marked = true

	// Recursively mark children based on type
	switch obj.Type {
	case ObjTypeString:
		// Strings have no children

	case ObjTypeArray:
		// Mark all elements in the array
		arrayObj := (*ArrayObj)(unsafe.Pointer(ptr))
		for i := 0; i < len(arrayObj.elements); i++ {
			vm.markValue(arrayObj.elements[i])
		}

	case ObjTypeObject:
		// Mark all values in the object
		objObj := (*ObjectObj)(unsafe.Pointer(ptr))
		for _, value := range objObj.fields {
			vm.markValue(value)
		}

	case ObjTypeFunction:
		// Mark function object
		funcObj := (*FunctionObj)(unsafe.Pointer(ptr))
		vm.markFunction(funcObj)

	case ObjTypeExternFunction:
		// Extern functions have no children

	case ObjTypeUpvalue:
		// Mark upvalue object
		upval := (*UpvalueObj)(unsafe.Pointer(ptr))
		// If closed, mark the closed value
		if upval.location == nil {
			vm.markValue(upval.closed)
		}
	}
}

// markFunction marks a function object.
func (vm *VM) markFunction(funcObj *FunctionObj) {
	if funcObj == nil {
		return
	}

	header := funcObj.Header()
	if header.Marked {
		return
	}

	// Mark the function object
	header.Marked = true

	// Mark upvalues
	for _, upval := range funcObj.upvalues {
		if upval != nil {
			// Mark the upvalue object itself
			if !upval.header.Marked {
				upval.header.Marked = true
				// If closed, mark the closed value
				if upval.location == nil {
					vm.markValue(upval.closed)
				}
			}
		}
	}
}

// sweep walks the object list and frees unmarked objects.
func (h *Heap) sweep() {
	var prev *ObjHeader
	current := h.objects

	for current != nil {
		if current.Marked {
			// Object is reachable, unmark for next GC cycle
			current.Marked = false
			prev = current
			current = current.Next
		} else {
			// Object is unreachable, free it
			unreached := current
			current = current.Next

			if prev != nil {
				prev.Next = current
			} else {
				h.objects = current
			}

			// Free the object (estimate size for stats)
			h.freeObject(unreached)
		}
	}

	// Collect again when twice as much is in use as survived this time, and
	// never before minGC. A threshold tied to what survives keeps the total
	// work proportional to what the program allocates; one that stays small
	// while the live data grows makes every collection re-walk that data.
	h.collections++
	h.nextGC = h.bytesAllocated * 2
	if h.nextGC < h.minGC {
		h.nextGC = h.minGC
	}
	if h.nextGC > h.maxBytes {
		h.nextGC = h.maxBytes
	}
}

// freeObject frees an individual object and updates stats.
func (h *Heap) freeObject(header *ObjHeader) {
	var size uint64

	switch header.Type {
	case ObjTypeString:
		obj := (*StringObj)(unsafe.Pointer(header))
		size = uint64(unsafe.Sizeof(StringObj{})) + uint64(len(obj.data))

	case ObjTypeArray:
		obj := (*ArrayObj)(unsafe.Pointer(header))
		size = uint64(unsafe.Sizeof(ArrayObj{})) + uint64(cap(obj.elements))*uint64(unsafe.Sizeof(Value(0)))

	case ObjTypeObject:
		obj := (*ObjectObj)(unsafe.Pointer(header))
		size = uint64(unsafe.Sizeof(ObjectObj{}))
		// What SETFIELD counted when each field was added
		size += uint64(len(obj.fields)) * objectFieldSize

	case ObjTypeFunction:
		// Phase A: Just the function object size (no upvalue array yet)
		size = uint64(unsafe.Sizeof(FunctionObj{}))
		// Phase B will add: upvalue array size

	case ObjTypeExternFunction:
		obj := (*ExternFunctionObj)(unsafe.Pointer(header))
		size = uint64(unsafe.Sizeof(ExternFunctionObj{})) + uint64(len(obj.name))

	default:
		size = uint64(unsafe.Sizeof(ObjHeader{}))
	}

	if h.bytesAllocated >= size {
		h.bytesAllocated -= size
	} else {
		h.bytesAllocated = 0
	}

	// Note: In Go, we don't manually free memory - GC will handle it
	// We just remove the object from our tracking list
}

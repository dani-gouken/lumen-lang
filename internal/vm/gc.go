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
	// Mark objects in registers
	for i := 0; i < len(vm.regs); i++ {
		vm.markValue(vm.regs[i])
	}

	// Mark objects on the stack
	for i := 0; i < vm.sp; i++ {
		vm.markValue(vm.stack[i])
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

	case ObjTypeClosure, ObjTypeUpvalue:
		// TODO: Implement when closures and upvalues are added
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

	// Update GC threshold
	h.nextGC = h.bytesAllocated + (h.bytesAllocated / 2)
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
		// Approximate map overhead
		size += uint64(len(obj.fields)) * 32

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

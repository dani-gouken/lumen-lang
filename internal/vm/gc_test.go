package vm

import (
	"strings"
	"testing"
	"unsafe"
)

func TestGarbageCollection(t *testing.T) {
	vm := NewVMWithHeapSize(10 * 1024) // 10KB heap

	// Allocate some objects and keep references
	str1, _, _ := vm.heap.AllocString("keep this")
	arr1, _, _ := vm.heap.AllocArray(5)

	// Store in registers (GC roots)
	vm.regs = make([]Value, 2)
	vm.regs[0] = NewHeapPtr(uintptr(unsafe.Pointer(str1)))
	vm.regs[1] = NewHeapPtr(uintptr(unsafe.Pointer(arr1)))

	// Allocate garbage (no references)
	for i := 0; i < 10; i++ {
		vm.heap.AllocString("garbage")
	}

	// Count objects before GC
	countBefore := countObjects(vm.heap)
	bytesBefore := vm.heap.BytesAllocated()

	// Run GC
	vm.CollectGarbage()

	// Count objects after GC
	countAfter := countObjects(vm.heap)
	bytesAfter := vm.heap.BytesAllocated()

	// Should have collected garbage
	if countAfter >= countBefore {
		t.Errorf("GC did not collect objects: before=%d, after=%d", countBefore, countAfter)
	}

	if bytesAfter >= bytesBefore {
		t.Errorf("GC did not free memory: before=%d, after=%d", bytesBefore, bytesAfter)
	}

	// Should keep exactly 2 objects (str1 and arr1)
	if countAfter != 2 {
		t.Errorf("Expected 2 objects after GC, got %d", countAfter)
	}
}

func TestGCMarksReachableObjects(t *testing.T) {
	vm := NewVMWithHeapSize(10 * 1024)

	// Create object graph: array -> object -> string
	str, _, _ := vm.heap.AllocString("nested")
	obj, _, _ := vm.heap.AllocObject()
	arr, _, _ := vm.heap.AllocArray(2)

	obj.Set("data", NewHeapPtr(uintptr(unsafe.Pointer(str))))
	arr.Append(NewHeapPtr(uintptr(unsafe.Pointer(obj))))

	// Only keep reference to array
	vm.regs = make([]Value, 1)
	vm.regs[0] = NewHeapPtr(uintptr(unsafe.Pointer(arr)))

	// Allocate garbage
	for i := 0; i < 5; i++ {
		vm.heap.AllocString("garbage")
		vm.heap.AllocObject()
	}

	countBefore := countObjects(vm.heap)

	// Run GC
	vm.CollectGarbage()

	countAfter := countObjects(vm.heap)

	// Should keep all 3 objects in the graph
	if countAfter != 3 {
		t.Errorf("Expected 3 reachable objects after GC, got %d", countAfter)
	}

	// Should have collected garbage
	if countAfter >= countBefore {
		t.Errorf("GC did not collect anything: before=%d, after=%d", countBefore, countAfter)
	}
}

func TestGCFromStack(t *testing.T) {
	vm := NewVMWithHeapSize(10 * 1024)

	// Allocate object and put on stack
	str, _, _ := vm.heap.AllocString("on stack")
	vm.stack[0] = NewHeapPtr(uintptr(unsafe.Pointer(str)))
	vm.sp = 1

	// Allocate garbage
	for i := 0; i < 5; i++ {
		vm.heap.AllocString("garbage")
	}

	// Run GC
	vm.CollectGarbage()

	// Should keep the object on the stack
	count := countObjects(vm.heap)
	if count != 1 {
		t.Errorf("Expected 1 object after GC (on stack), got %d", count)
	}
}

func TestGCClearsMarkedFlag(t *testing.T) {
	vm := NewVMWithHeapSize(10 * 1024)

	// Allocate and keep reference
	str, _, _ := vm.heap.AllocString("test")
	vm.regs = make([]Value, 1)
	vm.regs[0] = NewHeapPtr(uintptr(unsafe.Pointer(str)))

	// Run GC - this will mark and then unmark during sweep
	vm.CollectGarbage()

	// After GC, marked flag should be cleared by sweep
	if str.Header().Marked {
		t.Error("Object marked flag should be cleared after GC sweep")
	}

	// Run another GC cycle
	vm.CollectGarbage()

	// Should still work and keep the object
	count := countObjects(vm.heap)
	if count != 1 {
		t.Errorf("Expected 1 object after second GC, got %d", count)
	}
}

func TestGCUpdateThreshold(t *testing.T) {
	// The next collection is due when twice as much is in use as survived
	// this one, and never before the heap's starting threshold.
	newVM := func() *VM { return NewVMWithHeapSize(1024 * 1024) }
	keep := func(vm *VM, bytes int) {
		str, _, _ := vm.heap.AllocString(strings.Repeat("x", bytes))
		vm.regs = append(vm.regs, NewHeapPtr(uintptr(unsafe.Pointer(str))))
	}

	// Little survives: the threshold stays where it started. Dropping it to
	// just above the live size would make the collector run constantly.
	vm := newVM()
	start := vm.heap.nextGC
	keep(vm, 100)
	vm.heap.AllocString(strings.Repeat("garbage", 1000))
	vm.CollectGarbage()
	if vm.heap.nextGC != start {
		t.Errorf("expected the threshold to stay at %d with little live data, got %d", start, vm.heap.nextGC)
	}

	// Much survives: the threshold follows it
	vm = newVM()
	keep(vm, 500*1024)
	vm.CollectGarbage()
	live := vm.heap.BytesAllocated()
	if want := live * 2; vm.heap.nextGC != want {
		t.Errorf("expected the threshold at twice the live size (%d), got %d", want, vm.heap.nextGC)
	}

	// ...but never past the limit of the heap
	vm = newVM()
	keep(vm, 700*1024)
	vm.CollectGarbage()
	if vm.heap.nextGC != vm.heap.MaxBytes() {
		t.Errorf("expected the threshold capped at the heap limit %d, got %d", vm.heap.MaxBytes(), vm.heap.nextGC)
	}
}

func TestGCWithEmptyHeap(t *testing.T) {
	vm := NewVM()

	// Run GC on empty heap (should not crash)
	vm.CollectGarbage()

	count := countObjects(vm.heap)
	if count != 0 {
		t.Errorf("Expected 0 objects, got %d", count)
	}
}

func TestGCWithAllGarbage(t *testing.T) {
	vm := NewVMWithHeapSize(10 * 1024)

	// Allocate garbage with no references
	for i := 0; i < 10; i++ {
		vm.heap.AllocString("garbage")
	}

	vm.CollectGarbage()

	count := countObjects(vm.heap)
	if count != 0 {
		t.Errorf("Expected all garbage to be collected, got %d objects", count)
	}

	if vm.heap.BytesAllocated() != 0 {
		t.Errorf("Expected 0 bytes after collecting all garbage, got %d", vm.heap.BytesAllocated())
	}
}

func TestAutoGCTrigger(t *testing.T) {
	vm := NewVMWithHeapSize(2 * 1024) // Very small heap

	// Keep reference to first object
	first, _, _ := vm.heap.AllocString("keep")
	vm.regs = make([]Value, 1)
	vm.regs[0] = NewHeapPtr(uintptr(unsafe.Pointer(first)))

	// Allocate until GC is triggered
	triggered := false
	for i := 0; i < 100; i++ {
		_, shouldGC, err := vm.heap.AllocString("test string with some content")
		if err != nil {
			break
		}
		if shouldGC {
			triggered = true
			break
		}
	}

	if !triggered {
		t.Error("GC should have been triggered")
	}
}

// Helper function to count objects in heap
func countObjects(h *Heap) int {
	count := 0
	current := h.objects
	for current != nil {
		count++
		current = current.Next
	}
	return count
}

package vm

import (
	"fmt"
	"unsafe"
)

const (
	// DefaultHeapSize is 1MB
	DefaultHeapSize = 1024 * 1024
	// GC triggers when heap is 80% full
	DefaultGCThreshold = 0.8
)

// Heap manages heap-allocated objects.
type Heap struct {
	// Head of the linked list of all objects
	objects *ObjHeader

	// Memory statistics
	bytesAllocated uint64
	nextGC         uint64

	// Configuration
	maxBytes uint64
}

// NewHeap creates a new heap with the given size limit.
func NewHeap(maxBytes uint64) *Heap {
	if maxBytes == 0 {
		maxBytes = DefaultHeapSize
	}

	return &Heap{
		objects:        nil,
		bytesAllocated: 0,
		nextGC:         uint64(float64(maxBytes) * DefaultGCThreshold),
		maxBytes:       maxBytes,
	}
}

// allocate allocates memory for an object and tracks it.
func (h *Heap) allocate(obj Obj, size uint64) error {
	// Check if we need to run GC
	if h.bytesAllocated+size > h.maxBytes {
		return fmt.Errorf("heap exhausted: %d bytes allocated, %d max", h.bytesAllocated, h.maxBytes)
	}

	// Add to object list
	header := obj.Header()
	header.Next = h.objects
	h.objects = header

	// Track allocation
	h.bytesAllocated += size

	return nil
}

// AllocString allocates a string object.
// Returns the object and a boolean indicating if GC should run.
func (h *Heap) AllocString(s string) (*StringObj, bool, error) {
	obj := &StringObj{
		header: ObjHeader{
			Type:   ObjTypeString,
			Marked: false,
			Next:   nil,
		},
		data: s,
	}

	size := uint64(unsafe.Sizeof(StringObj{})) + uint64(len(s))
	if err := h.allocate(obj, size); err != nil {
		return nil, false, err
	}

	return obj, h.ShouldCollect(), nil
}

// AllocArray allocates an array object.
// Returns the object and a boolean indicating if GC should run.
func (h *Heap) AllocArray(capacity int) (*ArrayObj, bool, error) {
	obj := &ArrayObj{
		header: ObjHeader{
			Type:   ObjTypeArray,
			Marked: false,
			Next:   nil,
		},
		elements: make([]Value, 0, capacity),
	}

	size := uint64(unsafe.Sizeof(ArrayObj{})) + uint64(capacity)*uint64(unsafe.Sizeof(Value(0)))
	if err := h.allocate(obj, size); err != nil {
		return nil, false, err
	}

	return obj, h.ShouldCollect(), nil
}

// AllocObject allocates a map/object.
// Returns the object and a boolean indicating if GC should run.
func (h *Heap) AllocObject() (*ObjectObj, bool, error) {
	obj := &ObjectObj{
		header: ObjHeader{
			Type:   ObjTypeObject,
			Marked: false,
			Next:   nil,
		},
		fields: make(map[string]Value),
	}

	size := uint64(unsafe.Sizeof(ObjectObj{}))
	if err := h.allocate(obj, size); err != nil {
		return nil, false, err
	}

	return obj, h.ShouldCollect(), nil
}

// ShouldCollect returns true if GC should run.
func (h *Heap) ShouldCollect() bool {
	return h.bytesAllocated >= h.nextGC
}

// BytesAllocated returns current heap usage.
func (h *Heap) BytesAllocated() uint64 {
	return h.bytesAllocated
}

// MaxBytes returns heap size limit.
func (h *Heap) MaxBytes() uint64 {
	return h.maxBytes
}

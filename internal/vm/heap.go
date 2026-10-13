package vm

import (
	"fmt"
	"unsafe"
)

const (
	// DefaultHeapSize is the most a program may hold at once: 256MB
	DefaultHeapSize = 256 * 1024 * 1024
	// DefaultGCStart is how much is allocated before the first collection
	DefaultGCStart = 1024 * 1024
	// A heap smaller than DefaultGCStart collects when it is 80% full
	DefaultGCThreshold = 0.8
)

// Heap manages heap-allocated objects.
type Heap struct {
	// Head of the linked list of all objects
	objects *ObjHeader

	// Memory statistics
	bytesAllocated uint64
	nextGC         uint64
	collections    int // number of collections run so far

	// Configuration
	maxBytes uint64
	minGC    uint64 // the collection threshold never drops below this

	// Runs a collection. Set by the VM, which knows the roots; called when
	// an allocation would exceed maxBytes, before giving up.
	collect func()

	// onFree, when set, is told of every object the collector frees. Tests
	// use it to spoil freed objects, so that using one shows.
	onFree func(*ObjHeader)
}

// NewHeap creates a new heap with the given size limit.
func NewHeap(maxBytes uint64) *Heap {
	if maxBytes == 0 {
		maxBytes = DefaultHeapSize
	}

	// Collect for the first time at DefaultGCStart, or when a smaller heap
	// is nearly full. After each collection the threshold is set to twice
	// what survived (see sweep), so the cost of collecting stays
	// proportional to what is allocated.
	start := uint64(float64(maxBytes) * DefaultGCThreshold)
	if start > DefaultGCStart {
		start = DefaultGCStart
	}

	return &Heap{
		objects:        nil,
		bytesAllocated: 0,
		nextGC:         start,
		maxBytes:       maxBytes,
		minGC:          start,
	}
}

// reserve makes sure size more bytes fit under the limit, collecting
// garbage first if they do not.
func (h *Heap) reserve(size uint64) error {
	if h.bytesAllocated+size > h.maxBytes && h.collect != nil {
		h.collect()
	}
	if h.bytesAllocated+size > h.maxBytes {
		return fmt.Errorf("out of memory: %d bytes in use, %d more requested, limit %d", h.bytesAllocated, size, h.maxBytes)
	}
	return nil
}

// grow accounts for an existing object getting larger: an array gaining
// capacity, an object gaining a field. It reports whether a collection is
// due.
func (h *Heap) grow(size uint64) (bool, error) {
	if err := h.reserve(size); err != nil {
		return false, err
	}
	h.bytesAllocated += size
	return h.ShouldCollect(), nil
}

// Collections returns how many garbage collections have run.
func (h *Heap) Collections() int {
	return h.collections
}

// allocate allocates memory for an object and tracks it.
func (h *Heap) allocate(obj Obj, size uint64) error {
	if err := h.reserve(size); err != nil {
		return err
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

// AllocFunction allocates a function object.
// Returns the object and a boolean indicating if GC should run.
func (h *Heap) AllocFunction(chunk *Chunk, protoIdx int, upvalueCount int) (*FunctionObj, bool, error) {
	// Validate proto index
	if protoIdx < 0 || protoIdx >= len(chunk.Prototypes) {
		return nil, false, fmt.Errorf("invalid prototype index: %d (chunk has %d prototypes)", protoIdx, len(chunk.Prototypes))
	}

	obj := &FunctionObj{
		header: ObjHeader{
			Type:   ObjTypeFunction,
			Marked: false,
			Next:   nil,
		},
		chunk:    chunk,
		protoIdx: protoIdx,
		upvalues: make([]*UpvalueObj, upvalueCount),
	}

	size := uint64(unsafe.Sizeof(FunctionObj{})) + uint64(upvalueCount)*uint64(unsafe.Sizeof(&UpvalueObj{}))
	if err := h.allocate(obj, size); err != nil {
		return nil, false, err
	}

	return obj, h.ShouldCollect(), nil
}

// AllocExternFunction allocates an external function object.
// Returns the object and a boolean indicating if GC should run.
func (h *Heap) AllocExternFunction(name string, function ExternFunc, argCount int) (*ExternFunctionObj, bool, error) {
	obj := &ExternFunctionObj{
		header: ObjHeader{
			Type:   ObjTypeExternFunction,
			Marked: false,
			Next:   nil,
		},
		name:     name,
		function: function,
		argCount: argCount,
	}

	size := uint64(unsafe.Sizeof(ExternFunctionObj{})) + uint64(len(name))
	if err := h.allocate(obj, size); err != nil {
		return nil, false, err
	}

	return obj, h.ShouldCollect(), nil
}

// AllocUpvalue allocates an upvalue object.
// Returns the object and a boolean indicating if GC should run.
// The upvalue starts in the "open" state, pointing to a stack location.
func (h *Heap) AllocUpvalue(location *Value) (*UpvalueObj, bool, error) {
	obj := &UpvalueObj{
		header: ObjHeader{
			Type:   ObjTypeUpvalue,
			Marked: false,
			Next:   nil,
		},
		location: location,
		closed:   NewNull(),
	}

	size := uint64(unsafe.Sizeof(UpvalueObj{}))
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

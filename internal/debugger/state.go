package debugger

import (
	"github.com/dani-gouken/lumen/internal/vm"
)

const (
	// MaxHistorySize is the maximum number of snapshots to keep in history
	MaxHistorySize = 1000
)

// VMSnapshot captures the complete state of the VM at a specific instruction.
type VMSnapshot struct {
	// Execution state
	IP        int           // Instruction pointer
	Instr     vm.Instr      // Current instruction
	DebugInfo *vm.DebugInfo // Debug info for current instruction

	// Register state
	Registers []vm.Value // Copy of all registers

	// Constants and code
	Constants []vm.Value // Reference to constants (read-only)
	Code      []vm.Instr // Reference to code (read-only)

	// Stack state
	StackPointer int // Stack pointer
	FramePointer int // Frame pointer
	FrameCount   int // Number of active frames

	// Heap state
	HeapAllocated uint64 // Bytes allocated on heap
	HeapMax       uint64 // Maximum heap size

	// Metadata
	RegisterCount int // Total number of registers
	ArgCount      int // Number of function arguments
}

// ExecutionHistory maintains a circular buffer of VM snapshots.
type ExecutionHistory struct {
	snapshots [MaxHistorySize]*VMSnapshot
	head      int // Current position in buffer
	size      int // Number of snapshots stored
}

// NewExecutionHistory creates a new execution history tracker.
func NewExecutionHistory() *ExecutionHistory {
	return &ExecutionHistory{
		snapshots: [MaxHistorySize]*VMSnapshot{},
		head:      -1,
		size:      0,
	}
}

// Add adds a snapshot to the history.
func (h *ExecutionHistory) Add(snapshot *VMSnapshot) {
	h.head = (h.head + 1) % MaxHistorySize
	h.snapshots[h.head] = snapshot
	if h.size < MaxHistorySize {
		h.size++
	}
}

// Current returns the current snapshot (most recent).
func (h *ExecutionHistory) Current() *VMSnapshot {
	if h.size == 0 {
		return nil
	}
	return h.snapshots[h.head]
}

// Get returns the snapshot at the given index (0 = oldest, size-1 = newest).
func (h *ExecutionHistory) Get(index int) *VMSnapshot {
	if index < 0 || index >= h.size {
		return nil
	}

	// Calculate actual position in circular buffer
	actualPos := (h.head - (h.size - 1 - index) + MaxHistorySize) % MaxHistorySize
	return h.snapshots[actualPos]
}

// Size returns the number of snapshots in history.
func (h *ExecutionHistory) Size() int {
	return h.size
}

// GetLast returns the last N snapshots (newest first).
func (h *ExecutionHistory) GetLast(n int) []*VMSnapshot {
	if n > h.size {
		n = h.size
	}

	result := make([]*VMSnapshot, n)
	for i := 0; i < n; i++ {
		result[i] = h.Get(h.size - 1 - i)
	}
	return result
}

// Clear clears the history.
func (h *ExecutionHistory) Clear() {
	h.head = -1
	h.size = 0
}

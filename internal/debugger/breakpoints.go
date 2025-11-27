package debugger

import (
	"fmt"
	"sort"

	"github.com/yourname/lumen/internal/vm"
)

// BreakpointManager manages breakpoints for the debugger.
type BreakpointManager struct {
	breakpoints map[int]bool   // IP -> enabled
	labels      map[string]int // label -> IP (from debug info)
	chunk       *vm.Chunk      // Reference to chunk for label resolution
}

// NewBreakpointManager creates a new breakpoint manager.
func NewBreakpointManager(chunk *vm.Chunk) *BreakpointManager {
	bm := &BreakpointManager{
		breakpoints: make(map[int]bool),
		labels:      make(map[string]int),
		chunk:       chunk,
	}
	bm.buildLabelMap()
	return bm
}

// buildLabelMap builds a mapping from labels to instruction pointers.
func (bm *BreakpointManager) buildLabelMap() {
	for ip, debug := range bm.chunk.DebugInfo {
		if debug != nil && debug.FuncName != "" {
			if _, exists := bm.labels[debug.FuncName]; !exists {
				bm.labels[debug.FuncName] = ip
			}
		}
	}
}

// AddBreakpoint adds a breakpoint at the given instruction pointer.
func (bm *BreakpointManager) AddBreakpoint(ip int) error {
	if ip < 0 || ip >= len(bm.chunk.Code) {
		return fmt.Errorf("invalid IP %d (code size: %d)", ip, len(bm.chunk.Code))
	}
	bm.breakpoints[ip] = true
	return nil
}

// AddBreakpointByLabel adds a breakpoint at a label.
func (bm *BreakpointManager) AddBreakpointByLabel(label string) error {
	ip, exists := bm.labels[label]
	if !exists {
		return fmt.Errorf("label %q not found", label)
	}
	return bm.AddBreakpoint(ip)
}

// RemoveBreakpoint removes a breakpoint at the given instruction pointer.
func (bm *BreakpointManager) RemoveBreakpoint(ip int) {
	delete(bm.breakpoints, ip)
}

// RemoveAll removes all breakpoints.
func (bm *BreakpointManager) RemoveAll() {
	clear(bm.breakpoints)
}

// ShouldBreak returns true if there's a breakpoint at the given IP.
func (bm *BreakpointManager) ShouldBreak(ip int) bool {
	return bm.breakpoints[ip]
}

// List returns all breakpoints sorted by IP.
func (bm *BreakpointManager) List() []int {
	ips := make([]int, 0, len(bm.breakpoints))
	for ip := range bm.breakpoints {
		ips = append(ips, ip)
	}
	sort.Ints(ips)
	return ips
}

// ListLabels returns all available labels sorted alphabetically.
func (bm *BreakpointManager) ListLabels() []string {
	labels := make([]string, 0, len(bm.labels))
	for label := range bm.labels {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// GetLabelIP returns the IP for a given label.
func (bm *BreakpointManager) GetLabelIP(label string) (int, bool) {
	ip, exists := bm.labels[label]
	return ip, exists
}

// Count returns the number of breakpoints.
func (bm *BreakpointManager) Count() int {
	return len(bm.breakpoints)
}

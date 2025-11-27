package vm

// Chunk represents a compiled function containing bytecode and metadata.
type Chunk struct {
	Constants     []Value
	Code          []Instr
	RegisterCount int
	ArgCount      int
	ExternNames   []string            // Names of external functions
	StringPool    []string            // Pool of string literals
	DebugInfo     map[int]*DebugInfo  // Map from instruction index to debug info
	Breakpoints   []int               // Instruction pointers marked with ;BREAK in source
}

// NewChunk creates a new empty chunk.
func NewChunk() *Chunk {
	return &Chunk{
		Constants:     make([]Value, 0),
		Code:          make([]Instr, 0),
		RegisterCount: 0,
		ArgCount:      0,
		ExternNames:   make([]string, 0),
		StringPool:    make([]string, 0),
		DebugInfo:     make(map[int]*DebugInfo),
		Breakpoints:   make([]int, 0),
	}
}

// AddBreakpoint marks an instruction pointer as having a breakpoint.
func (c *Chunk) AddBreakpoint(ip int) {
	c.Breakpoints = append(c.Breakpoints, ip)
}

// AddConstant adds a constant to the constant pool and returns its index.
func (c *Chunk) AddConstant(val Value) int {
	c.Constants = append(c.Constants, val)
	return len(c.Constants) - 1
}

// AddInstruction adds an instruction to the code array.
func (c *Chunk) AddInstruction(instr Instr) {
	c.Code = append(c.Code, instr)
}

// AddInstructionWithDebug adds an instruction with debug info to the code array.
func (c *Chunk) AddInstructionWithDebug(instr Instr, debug *DebugInfo) {
	index := len(c.Code)
	c.Code = append(c.Code, instr)
	if debug != nil {
		c.DebugInfo[index] = debug
	}
}

// GetDebugInfo returns the debug info for an instruction at the given index.
func (c *Chunk) GetDebugInfo(index int) *DebugInfo {
	return c.DebugInfo[index]
}

// AddExtern adds an external function name and returns its index.
func (c *Chunk) AddExtern(name string) int {
	c.ExternNames = append(c.ExternNames, name)
	return len(c.ExternNames) - 1
}

// AddString adds a string literal to the string pool and returns its index.
func (c *Chunk) AddString(str string) int {
	c.StringPool = append(c.StringPool, str)
	return len(c.StringPool) - 1
}

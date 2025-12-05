package vm

// FunctionPrototype represents metadata for a compiled function.
type FunctionPrototype struct {
	Name          string // Function name for debugging and entry point lookup
	CodeStart     int    // Instruction index where function begins
	CodeSize      int    // Number of instructions in function body
	RegisterCount int    // Number of registers needed by this function
	ArgCount      int    // Number of parameters this function accepts
	UpvalueCount  int    // Number of upvalues this function captures
}

// Chunk represents a compiled module containing bytecode and metadata.
type Chunk struct {
	Constants   []Value
	Code        []Instr                 // Flat instruction array (all functions concatenated)
	Prototypes  []FunctionPrototype     // Function metadata table
	ExternNames []string                // Names of external functions
	StringPool  []string                // Pool of string literals
	DebugInfo   map[int]*DebugInfo      // Map from instruction index to debug info
	Breakpoints []int                   // Instruction pointers marked with ;BREAK in source
	// NOTE: RegisterCount and ArgCount removed - now per-function in Prototypes
}

// NewChunk creates a new empty chunk.
func NewChunk() *Chunk {
	return &Chunk{
		Constants:   make([]Value, 0),
		Code:        make([]Instr, 0),
		Prototypes:  make([]FunctionPrototype, 0),
		ExternNames: make([]string, 0),
		StringPool:  make([]string, 0),
		DebugInfo:   make(map[int]*DebugInfo),
		Breakpoints: make([]int, 0),
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

// FindPrototype finds a function prototype by name and returns its index.
// Returns -1 if not found.
func (c *Chunk) FindPrototype(name string) int {
	for i, proto := range c.Prototypes {
		if proto.Name == name {
			return i
		}
	}
	return -1
}

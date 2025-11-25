package vm

// Chunk represents a compiled function containing bytecode and metadata.
type Chunk struct {
	Constants     []Value
	Code          []Instr
	RegisterCount int
	ArgCount      int
	ExternNames   []string // Names of external functions
}

// NewChunk creates a new empty chunk.
func NewChunk() *Chunk {
	return &Chunk{
		Constants:     make([]Value, 0),
		Code:          make([]Instr, 0),
		RegisterCount: 0,
		ArgCount:      0,
		ExternNames:   make([]string, 0),
	}
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

// AddExtern adds an external function name and returns its index.
func (c *Chunk) AddExtern(name string) int {
	c.ExternNames = append(c.ExternNames, name)
	return len(c.ExternNames) - 1
}

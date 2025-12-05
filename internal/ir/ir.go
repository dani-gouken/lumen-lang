package ir

import "fmt"

// TypeRef is a reference to a type (just the name/reference).
type TypeRef string

// Value is an interface for operands (Registers, Constants, etc.)
type Value interface {
	String() string
}

// Register represents a virtual register (%v1, %v2...)
type Register struct {
	ID int
}

func (r Register) String() string {
	return fmt.Sprintf("%%v%d", r.ID)
}

// Constant represents a literal value
type Constant struct {
	Val  interface{}
	Type TypeRef
}

func (c Constant) String() string {
	if s, ok := c.Val.(string); ok {
		return fmt.Sprintf("\"%s\"", s)
	}
	return fmt.Sprintf("%v", c.Val)
}

// Instruction represents a single IR operation
type Instruction struct {
	Op       OpType
	Dest     *Register // The result goes here (can be nil)
	Operands []Value   // Variable number of operands
}

// BasicBlock is a sequence of instructions without internal control flow
type BasicBlock struct {
	ID           int
	Instructions []*Instruction
	Predecessors []*BasicBlock
	Successors   []*BasicBlock
}

func (bb *BasicBlock) String() string {
	return fmt.Sprintf("b%d", bb.ID)
}

// Function represents a compiled function in IR
type Function struct {
	Name       string
	Params     []*Register
	Blocks     []*BasicBlock
	EntryBlock *BasicBlock
	RegCount   int // Counter for generating unique registers
}

// Program represents the complete compilation unit
type Program struct {
	Functions []*Function
}

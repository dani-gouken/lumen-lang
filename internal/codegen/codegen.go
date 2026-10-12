// Package codegen lowers Lumen IR to luvm bytecode.
package codegen

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/ir"
	"github.com/dani-gouken/lumen/internal/token"
	"github.com/dani-gouken/lumen/internal/vm"
)

// Error codes for bytecode generation
const (
	ErrTooManyRegisters = "E6001" // Function needs more registers than an instruction can address
	ErrTooManyConstants = "E6002" // Constant pool is full
	ErrTooManyFunctions = "E6003" // Function table is full
	ErrUnsupportedIR    = "E6004" // IR construct with no bytecode equivalent
	ErrUndefinedFunc    = "E6005" // Reference to a function that does not exist
	ErrMissingMain      = "E6006" // Program has no usable entry point
	ErrCallTooLarge     = "E6007" // Call has more arguments than an instruction can encode
)

// Operands are 16 bits wide, which bounds everything they index.
const (
	maxRegisters = vm.MaxOperand + 1
	maxConstants = vm.MaxOperand + 1
	maxFunctions = vm.MaxOperand + 1
)

// EntryPoint is the function the VM starts executing.
const EntryPoint = "main"

// Generator turns an IR program into a bytecode chunk.
type Generator struct {
	prog        *ir.Program
	errs        *errors.Handler
	sourceLines map[int]string

	chunk *vm.Chunk

	// Names of host-provided functions that may be referenced
	externs map[string]bool

	protoIndex  map[string]int   // Function name -> prototype index
	externIndex map[string]int   // Extern name -> index in chunk.ExternNames
	constIndex  map[vm.Value]int // Constant value -> index in chunk.Constants
	stringIndex map[string]int   // String -> index in chunk.StringPool
}

// NewGenerator creates a bytecode generator. externs lists the host
// functions the program is allowed to reference.
func NewGenerator(prog *ir.Program, externs []string, errs *errors.Handler) *Generator {
	g := &Generator{
		prog:        prog,
		errs:        errs,
		sourceLines: make(map[int]string),
		chunk:       vm.NewChunk(),
		externs:     make(map[string]bool),
		protoIndex:  make(map[string]int),
		externIndex: make(map[string]int),
		constIndex:  make(map[vm.Value]int),
		stringIndex: make(map[string]int),
	}
	for _, name := range externs {
		g.externs[name] = true
	}
	return g
}

// SetSourceLines sets source lines for error reporting and debug info.
func (g *Generator) SetSourceLines(lines map[int]string) {
	g.sourceLines = lines
}

// Generate produces the chunk. Problems are reported to the error handler;
// the chunk must not be executed if it recorded any error.
func (g *Generator) Generate() *vm.Chunk {
	if len(g.prog.Functions) > maxFunctions {
		g.report(token.Position{}, ErrTooManyFunctions,
			fmt.Sprintf("program has %d functions, the limit is %d", len(g.prog.Functions), maxFunctions),
			"Split the program or remove unused functions")
		return g.chunk
	}

	// Prototype indices are assigned up front so that functions can refer
	// to ones defined later.
	for i, fn := range g.prog.Functions {
		g.protoIndex[fn.Name] = i
	}
	g.checkEntryPoint()

	for _, fn := range g.prog.Functions {
		newFuncGen(g, fn).generate()
	}
	return g.chunk
}

func (g *Generator) checkEntryPoint() {
	idx, ok := g.protoIndex[EntryPoint]
	if !ok {
		g.report(token.Position{}, ErrMissingMain, "program has no 'main' function",
			"Add an entry point: fn main() -> Int { return 0; }")
		return
	}
	if n := len(g.prog.Functions[idx].Params); n != 0 {
		g.report(token.Position{}, ErrMissingMain,
			fmt.Sprintf("'main' must not take parameters, it has %d", n),
			"Declare it as: fn main() -> Int { ... }")
	}
}

// report records an error at a source position.
func (g *Generator) report(pos token.Position, code, msg, suggestion string) {
	tok := token.Token{Pos: pos, EndPos: pos}
	tok.EndPos.Col++

	if suggestion != "" {
		g.errs.ReportErrorWithSuggestion(code, msg, suggestion, tok, g.sourceLines)
		return
	}
	g.errs.ReportErrorFromToken(code, msg, tok, g.sourceLines)
}

// constant returns the pool index of a constant, adding it if needed.
func (g *Generator) constant(val vm.Value, pos token.Position) uint16 {
	if idx, ok := g.constIndex[val]; ok {
		return uint16(idx)
	}
	if len(g.chunk.Constants) >= maxConstants {
		g.report(pos, ErrTooManyConstants,
			fmt.Sprintf("program uses more than %d distinct constants", maxConstants),
			"Reduce the number of distinct literals and field names")
		return 0
	}
	idx := g.chunk.AddConstant(val)
	g.constIndex[val] = idx
	return uint16(idx)
}

// stringConstant returns the pool index of a constant referring to a string
// in the string pool, as NEWSTR and the field instructions expect.
func (g *Generator) stringConstant(s string, pos token.Position) uint16 {
	idx, ok := g.stringIndex[s]
	if !ok {
		idx = g.chunk.AddString(s)
		g.stringIndex[s] = idx
	}
	return g.constant(vm.NewInt(int64(idx)), pos)
}

// extern returns the index of a host function name.
func (g *Generator) extern(name string) uint16 {
	idx, ok := g.externIndex[name]
	if !ok {
		idx = g.chunk.AddExtern(name)
		g.externIndex[name] = idx
	}
	return uint16(idx)
}

// debugInfo builds the debug record for an instruction.
func (g *Generator) debugInfo(pos token.Position, funcName string) *vm.DebugInfo {
	if pos.Line == 0 {
		return nil
	}
	return vm.NewDebugInfo(pos.File, pos.Line, pos.Col).
		WithSourceLine(g.sourceLines[pos.Line]).
		WithFuncName(funcName)
}

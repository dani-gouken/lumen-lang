package codegen

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/ir"
	"github.com/dani-gouken/lumen/internal/token"
	"github.com/dani-gouken/lumen/internal/vm"
)

// returnReg is where the VM leaves the result of every call. It is never
// used to hold a variable.
const returnReg = 0

// funcGen generates the bytecode of one function.
//
// Register layout of a frame:
//
//	r0            call results (and the first argument on entry)
//	r1..rN-1      remaining arguments, as passed by the caller
//	permanents    variables and values that live across basic blocks
//	temporaries   values used within one block, slots are reused
//	call window   callee followed by its arguments, set up before each CALL
type funcGen struct {
	g      *Generator
	fn     *ir.Function
	blocks []*ir.BasicBlock // Reachable blocks in layout order

	phys     map[int]int // Virtual register ID -> physical register
	regCount int         // Registers used so far (before the call window)
	callBase int         // First register of the call window
	maxArgs  int         // Largest argument count of any call

	blockStart map[*ir.BasicBlock]int // Block -> index of its first instruction
	fixups     []fixup
	pos        token.Position // Position of the IR instruction being lowered
	failed     bool
}

// fixup is a jump whose offset word is patched once all blocks are placed.
type fixup struct {
	offsetIdx int // Index of the offset word in chunk.Code
	target    *ir.BasicBlock
}

func newFuncGen(g *Generator, fn *ir.Function) *funcGen {
	return &funcGen{
		g:          g,
		fn:         fn,
		phys:       make(map[int]int),
		blockStart: make(map[*ir.BasicBlock]int),
		pos:        firstPos(fn),
	}
}

func firstPos(fn *ir.Function) token.Position {
	for _, bb := range fn.Blocks {
		for _, instr := range bb.Instructions {
			if instr.Pos.Line > 0 {
				return instr.Pos
			}
		}
	}
	return token.Position{}
}

func (f *funcGen) generate() {
	f.blocks = reachableBlocks(f.fn)
	f.allocateRegisters()

	codeStart := len(f.g.chunk.Code)

	// The first argument arrives in r0, which every call overwrites
	if len(f.fn.Params) > 0 {
		if home, ok := f.phys[f.fn.Params[0].ID]; ok && home != returnReg {
			f.emit(vm.OpMOV, uint16(home), returnReg, 0)
		}
	}

	for i, bb := range f.blocks {
		f.blockStart[bb] = len(f.g.chunk.Code)

		var next *ir.BasicBlock
		if i+1 < len(f.blocks) {
			next = f.blocks[i+1]
		}
		f.generateBlock(bb, next)
	}

	for _, fx := range f.fixups {
		offset := f.blockStart[fx.target] - (fx.offsetIdx + 1)
		f.g.chunk.Code[fx.offsetIdx] = vm.EncodeOffset32(int32(offset))
	}

	total := f.callBase + 1 + f.maxArgs
	if total > maxRegisters && !f.failed {
		f.fail(ErrTooManyRegisters,
			fmt.Sprintf("function '%s' needs %d registers, the limit is %d", f.fn.Name, total, maxRegisters),
			"Split the function into smaller ones")
	}

	f.g.chunk.Prototypes = append(f.g.chunk.Prototypes, vm.FunctionPrototype{
		Name:          f.fn.Name,
		CodeStart:     codeStart,
		CodeSize:      len(f.g.chunk.Code) - codeStart,
		RegisterCount: total,
		ArgCount:      len(f.fn.Params),
		UpvalueCount:  f.fn.UpvalueCount,
		Signature:     f.fn.Signature,
	})
}

// --- Control flow graph ---

// body returns the instructions of a block up to and including its first
// terminator; anything after it can never run.
func body(bb *ir.BasicBlock) []*ir.Instruction {
	for i, instr := range bb.Instructions {
		if isTerminator(instr.Op) {
			return bb.Instructions[:i+1]
		}
	}
	return bb.Instructions
}

func isTerminator(op ir.OpType) bool {
	return op == ir.OpRet || op == ir.OpJmp || op == ir.OpBr
}

// successors returns the blocks a block can jump to.
func successors(bb *ir.BasicBlock) []*ir.BasicBlock {
	instrs := body(bb)
	if len(instrs) == 0 {
		return nil
	}

	var targets []*ir.BasicBlock
	for _, operand := range instrs[len(instrs)-1].Operands {
		if target, ok := operand.(*ir.BasicBlock); ok {
			targets = append(targets, target)
		}
	}
	return targets
}

// reachableBlocks returns the blocks reachable from the entry, in their
// original order. Blocks left behind by code after return/break are dropped.
func reachableBlocks(fn *ir.Function) []*ir.BasicBlock {
	if fn.EntryBlock == nil {
		return nil
	}

	seen := map[*ir.BasicBlock]bool{fn.EntryBlock: true}
	work := []*ir.BasicBlock{fn.EntryBlock}
	for len(work) > 0 {
		bb := work[len(work)-1]
		work = work[:len(work)-1]
		for _, succ := range successors(bb) {
			if !seen[succ] {
				seen[succ] = true
				work = append(work, succ)
			}
		}
	}

	var blocks []*ir.BasicBlock
	for _, bb := range fn.Blocks {
		if seen[bb] {
			blocks = append(blocks, bb)
		}
	}
	return blocks
}

// --- Register allocation ---

// regID extracts the virtual register ID from an operand.
func regID(v ir.Value) (int, bool) {
	switch r := v.(type) {
	case *ir.Register:
		if r != nil {
			return r.ID, true
		}
	case ir.Register:
		return r.ID, true
	}
	return 0, false
}

// usage summarizes how a virtual register is used.
type usage struct {
	defs     int
	defBlock *ir.BasicBlock
	defIdx   int
	lastUse  int  // Index of the last use inside defBlock
	escapes  bool // Used outside defBlock, before its definition, or captured
}

// allocateRegisters maps every virtual register to a physical one.
//
// A register that is written once and only read later in the same block is
// a temporary: its slot is released after the last read and reused. All
// other registers are permanent and keep a slot for the whole function,
// which is also what closures need, since they capture slots by reference.
func (f *funcGen) allocateRegisters() {
	uses := make(map[int]*usage)
	var order []int // First appearance, for a deterministic layout
	get := func(id int) *usage {
		u, ok := uses[id]
		if !ok {
			u = &usage{}
			uses[id] = u
			order = append(order, id)
		}
		return u
	}

	for _, bb := range f.blocks {
		for idx, instr := range body(bb) {
			for _, operand := range instr.Operands {
				id, ok := regID(operand)
				if !ok {
					continue
				}
				u := get(id)
				if u.defs == 0 || u.defBlock != bb || idx <= u.defIdx || instr.Op == ir.OpMakeClosure {
					u.escapes = true
				}
				u.lastUse = idx
			}
			if instr.Dest != nil {
				u := get(instr.Dest.ID)
				u.defs++
				if u.defs == 1 {
					u.defBlock, u.defIdx, u.lastUse = bb, idx, idx
				}
			}
			if instr.Op == ir.OpCall && len(instr.Operands)-1 > f.maxArgs {
				f.maxArgs = len(instr.Operands) - 1
			}
		}
	}

	// Arguments: all but the first stay where the caller put them
	next := len(f.fn.Params)
	if next == 0 {
		next = 1 // r0 is reserved even without arguments
	}
	isParam := make(map[int]bool)
	for i, param := range f.fn.Params {
		isParam[param.ID] = true
		if i > 0 {
			f.phys[param.ID] = i
		} else if _, used := uses[param.ID]; used {
			f.phys[param.ID] = next
			next++
		}
	}

	// Permanents
	isTemp := func(id int) bool {
		u := uses[id]
		return u.defs == 1 && !u.escapes && !isParam[id]
	}
	for _, id := range order {
		if isParam[id] || isTemp(id) {
			continue
		}
		f.phys[id] = next
		next++
	}

	// Temporaries: walk each block, reusing slots as values die
	high := next
	var free []int
	for _, bb := range f.blocks {
		for idx, instr := range body(bb) {
			// Operands are read before the result is written, so a slot
			// that dies here can already hold this instruction's result.
			for _, operand := range instr.Operands {
				id, ok := regID(operand)
				if !ok || !isTemp(id) || uses[id].lastUse != idx {
					continue
				}
				if slot, live := f.phys[id]; live {
					free = append(free, slot)
					uses[id].lastUse = -1 // Released; guards against double free
				}
			}

			if instr.Dest == nil || !isTemp(instr.Dest.ID) {
				continue
			}
			var slot int
			if len(free) > 0 {
				slot = free[len(free)-1]
				free = free[:len(free)-1]
			} else {
				slot = high
				high++
			}
			f.phys[instr.Dest.ID] = slot
			if uses[instr.Dest.ID].lastUse == idx {
				free = append(free, slot) // Result is never read
			}
		}
	}

	f.regCount = high
	f.callBase = high
}

// --- Emission ---

func (f *funcGen) emit(op vm.OpCode, a, b, c uint16) {
	f.g.chunk.AddInstructionWithDebug(vm.NewInstr(op, a, b, c), f.g.debugInfo(f.pos, f.fn.Name))
}

func (f *funcGen) fail(code, msg, suggestion string) {
	f.failed = true
	f.g.report(f.pos, code, msg, suggestion)
}

// reg returns the physical register of a register operand.
func (f *funcGen) reg(v ir.Value) uint16 {
	id, ok := regID(v)
	if !ok {
		f.fail(ErrUnsupportedIR, fmt.Sprintf("expected a register operand, found %v", v), "")
		return 0
	}
	slot, ok := f.phys[id]
	if !ok || slot >= maxRegisters {
		// Too many registers; reported once the function is complete
		return 0
	}
	return uint16(slot)
}

// dest returns the physical register an instruction writes to.
func (f *funcGen) dest(instr *ir.Instruction) uint16 {
	if instr.Dest == nil {
		f.fail(ErrUnsupportedIR, fmt.Sprintf("%s has no destination register", instr.Op), "")
		return 0
	}
	return f.reg(instr.Dest)
}

// emitJump emits an unconditional or conditional 32-bit relative jump.
func (f *funcGen) emitJump(op vm.OpCode, cond uint16, target *ir.BasicBlock) {
	f.emit(vm.OpLONG, 0, 0, 0)
	f.emit(op, cond, 0, 0)
	f.fixups = append(f.fixups, fixup{offsetIdx: len(f.g.chunk.Code), target: target})
	f.emit(vm.OpNOP, 0, 0, 0) // Offset word, patched later
}

func (f *funcGen) generateBlock(bb, next *ir.BasicBlock) {
	instrs := body(bb)
	for _, instr := range instrs {
		if instr.Pos.Line > 0 {
			f.pos = instr.Pos
		}
		f.generateInstr(instr, next)
	}

	// A block must not fall into whatever happens to be placed after it
	if len(instrs) == 0 || !isTerminator(instrs[len(instrs)-1].Op) {
		f.emit(vm.OpLOADNULL, returnReg, 0, 0)
		f.emit(vm.OpRET, returnReg, 0, 0)
	}
}

var binaryOps = map[ir.OpType]vm.OpCode{
	ir.OpAdd: vm.OpADD,
	ir.OpSub: vm.OpSUB,
	ir.OpMul: vm.OpMUL,
	ir.OpDiv: vm.OpDIV,
	ir.OpMod: vm.OpMOD,
	ir.OpEq:  vm.OpEQ,
	ir.OpNeq: vm.OpNE,
	ir.OpLt:  vm.OpLT,
	ir.OpLte: vm.OpLE,
	ir.OpGt:  vm.OpGT,
	ir.OpGte: vm.OpGE,
}

func (f *funcGen) generateInstr(instr *ir.Instruction, next *ir.BasicBlock) {
	if op, ok := binaryOps[instr.Op]; ok {
		if !f.arity(instr, 2) {
			return
		}
		f.emit(op, f.dest(instr), f.reg(instr.Operands[0]), f.reg(instr.Operands[1]))
		return
	}

	switch instr.Op {
	case ir.OpNop:

	case ir.OpMov:
		if f.arity(instr, 1) {
			f.generateMov(instr)
		}

	case ir.OpNeg:
		if f.arity(instr, 1) {
			f.emit(vm.OpNEG, f.dest(instr), f.reg(instr.Operands[0]), 0)
		}
	case ir.OpNot:
		if f.arity(instr, 1) {
			f.emit(vm.OpNOT, f.dest(instr), f.reg(instr.Operands[0]), 0)
		}

	case ir.OpJmp:
		if target, ok := f.blockOperand(instr, 0); ok && target != next {
			f.emitJump(vm.OpJMP32, 0, target)
		}

	case ir.OpBr:
		thenBlock, ok1 := f.blockOperand(instr, 1)
		elseBlock, ok2 := f.blockOperand(instr, 2)
		if !ok1 || !ok2 {
			return
		}
		cond := f.reg(instr.Operands[0])
		f.emitJump(vm.OpJMPIFNOT32, cond, elseBlock)
		if thenBlock != next {
			f.emitJump(vm.OpJMP32, 0, thenBlock)
		}

	case ir.OpRet:
		if len(instr.Operands) == 0 {
			f.emit(vm.OpLOADNULL, returnReg, 0, 0)
			f.emit(vm.OpRET, returnReg, 0, 0)
			return
		}
		f.emit(vm.OpRET, f.reg(instr.Operands[0]), 0, 0)

	case ir.OpCall:
		f.generateCall(instr)

	case ir.OpNewStruct:
		// The struct's name is recorded on the value; casts check it
		if name, ok := f.nameOperand(instr, 0); ok {
			f.emit(vm.OpNEWOBJ, f.dest(instr), f.g.stringConstant(name, f.pos), 1)
		}

	case ir.OpGetField:
		if name, ok := f.nameOperand(instr, 1); ok {
			f.emit(vm.OpGETFIELD, f.dest(instr), f.reg(instr.Operands[0]), f.g.stringConstant(name, f.pos))
		}
	case ir.OpSetField:
		if name, ok := f.nameOperand(instr, 1); ok && f.arity(instr, 3) {
			f.emit(vm.OpSETFIELD, f.reg(instr.Operands[0]), f.g.stringConstant(name, f.pos), f.reg(instr.Operands[2]))
		}

	case ir.OpNewArray:
		capacity, _ := f.intOperand(instr, 0)
		if capacity > vm.MaxOperand {
			capacity = vm.MaxOperand // Only a hint; arrays grow on push
		}
		f.emit(vm.OpNEWARRAY, f.dest(instr), uint16(capacity), 0)
	case ir.OpGetIndex:
		if f.arity(instr, 2) {
			f.emit(vm.OpGETINDEX, f.dest(instr), f.reg(instr.Operands[0]), f.reg(instr.Operands[1]))
		}
	case ir.OpSetIndex:
		// SETELEM, not SETINDEX: assigning to an element must not grow
		// the array
		if f.arity(instr, 3) {
			f.emit(vm.OpSETELEM, f.reg(instr.Operands[0]), f.reg(instr.Operands[1]), f.reg(instr.Operands[2]))
		}
	case ir.OpLen:
		if f.arity(instr, 1) {
			f.emit(vm.OpARRLEN, f.dest(instr), f.reg(instr.Operands[0]), 0)
		}
	case ir.OpPush:
		if f.arity(instr, 2) {
			f.emit(vm.OpARRPUSH, f.reg(instr.Operands[0]), f.reg(instr.Operands[1]), 0)
		}

	case ir.OpMakeClosure:
		f.generateClosure(instr)
	case ir.OpCloseUpvalues:
		for _, operand := range instr.Operands {
			f.emit(vm.OpCLOSEUP, f.reg(operand), 0, 0)
		}
	case ir.OpLoadUpvalue:
		if idx, ok := f.intOperand(instr, 0); ok {
			f.emit(vm.OpLDCAP, f.dest(instr), uint16(idx), 0)
		}
	case ir.OpStoreUpvalue:
		if idx, ok := f.intOperand(instr, 0); ok && f.arity(instr, 2) {
			f.emit(vm.OpSTCAP, uint16(idx), f.reg(instr.Operands[1]), 0)
		}

	default:
		f.fail(ErrUnsupportedIR, fmt.Sprintf("IR operation %s cannot be compiled to bytecode yet", instr.Op), "")
	}
}

// generateMov lowers a move from a register or a constant.
func (f *funcGen) generateMov(instr *ir.Instruction) {
	dest := f.dest(instr)

	if _, ok := regID(instr.Operands[0]); ok {
		if src := f.reg(instr.Operands[0]); src != dest {
			f.emit(vm.OpMOV, dest, src, 0)
		}
		return
	}

	c, ok := instr.Operands[0].(ir.Constant)
	if !ok {
		f.fail(ErrUnsupportedIR, fmt.Sprintf("cannot move %v into a register", instr.Operands[0]), "")
		return
	}

	switch c.Type {
	case "Null":
		f.emit(vm.OpLOADNULL, dest, 0, 0)

	case "Bool":
		var b uint16
		if v, _ := c.Val.(bool); v {
			b = 1
		}
		f.emit(vm.OpLOADBOOL, dest, b, 0)

	case "Int":
		n, ok := toInt64(c.Val)
		if !ok {
			f.fail(ErrUnsupportedIR, fmt.Sprintf("invalid integer constant %v", c.Val), "")
			return
		}
		if n >= 0 && n <= vm.MaxOperand {
			f.emit(vm.OpLOADI, dest, uint16(n), 0)
			return
		}
		f.emit(vm.OpLOADK, dest, f.g.constant(vm.NewInt(n), f.pos), 0)

	case "Float":
		v, _ := c.Val.(float64)
		f.emit(vm.OpLOADK, dest, f.g.constant(vm.NewNumber(v), f.pos), 0)

	case "String":
		s, _ := c.Val.(string)
		f.emit(vm.OpNEWSTR, dest, f.g.stringConstant(s, f.pos), 0)

	case "FuncRef":
		name, _ := c.Val.(string)
		f.loadFunction(dest, name)

	default:
		f.fail(ErrUnsupportedIR, fmt.Sprintf("constant of type %s cannot be loaded", c.Type), "")
	}
}

// loadFunction puts a function object for the named function in dest.
func (f *funcGen) loadFunction(dest uint16, name string) {
	if idx, ok := f.g.protoIndex[name]; ok {
		f.emit(vm.OpNEWFUNC, dest, uint16(idx), 0)
		return
	}
	if f.g.externs[name] {
		f.emit(vm.OpNEWEXFUNC, dest, f.g.extern(name), 0)
		return
	}
	f.fail(ErrUndefinedFunc, fmt.Sprintf("undefined function '%s'", name), "")
}

// generateCall moves the callee and its arguments into the call window,
// calls, and copies the result out of r0.
func (f *funcGen) generateCall(instr *ir.Instruction) {
	if len(instr.Operands) == 0 {
		f.fail(ErrUnsupportedIR, "call without a callee", "")
		return
	}

	nargs := len(instr.Operands) - 1
	if f.callBase+nargs >= maxRegisters {
		f.fail(ErrCallTooLarge,
			fmt.Sprintf("call with %d arguments does not fit in the register window of '%s'", nargs, f.fn.Name),
			"Pass fewer arguments or split the function")
		return
	}

	for i, operand := range instr.Operands {
		f.emit(vm.OpMOV, uint16(f.callBase+i), f.reg(operand), 0)
	}
	f.emit(vm.OpCALL, uint16(f.callBase), uint16(nargs), 0)

	if instr.Dest != nil {
		if dest := f.reg(instr.Dest); dest != returnReg {
			f.emit(vm.OpMOV, dest, returnReg, 0)
		}
	}
}

// generateClosure creates a function object and binds its upvalues.
func (f *funcGen) generateClosure(instr *ir.Instruction) {
	if len(instr.Operands) == 0 {
		f.fail(ErrUnsupportedIR, "closure without a function", "")
		return
	}
	ref, ok := instr.Operands[0].(ir.Constant)
	name, isName := ref.Val.(string)
	if !ok || !isName {
		f.fail(ErrUnsupportedIR, "closure target must be a function reference", "")
		return
	}
	idx, ok := f.g.protoIndex[name]
	if !ok {
		f.fail(ErrUndefinedFunc, fmt.Sprintf("undefined function '%s'", name), "")
		return
	}

	dest := f.dest(instr)
	f.emit(vm.OpNEWFUNC, dest, uint16(idx), 0)

	for i, capture := range instr.Operands[1:] {
		if _, isReg := regID(capture); isReg {
			// A local of this function, shared by reference
			f.emit(vm.OpCAPLOCAL, dest, uint16(i), f.reg(capture))
			continue
		}
		c, isConst := capture.(ir.Constant)
		upvalIdx, isInt := toInt64(c.Val)
		if !isConst || c.Type != "Upvalue" || !isInt {
			f.fail(ErrUnsupportedIR, fmt.Sprintf("cannot capture %v", capture), "")
			continue
		}
		// A variable this function captured itself, passed on
		f.emit(vm.OpCAPUPVAL, dest, uint16(i), uint16(upvalIdx))
	}
}

// --- Operand helpers ---

func (f *funcGen) arity(instr *ir.Instruction, n int) bool {
	if len(instr.Operands) < n {
		f.fail(ErrUnsupportedIR, fmt.Sprintf("%s expects %d operands, found %d", instr.Op, n, len(instr.Operands)), "")
		return false
	}
	return true
}

func (f *funcGen) blockOperand(instr *ir.Instruction, i int) (*ir.BasicBlock, bool) {
	if i < len(instr.Operands) {
		if bb, ok := instr.Operands[i].(*ir.BasicBlock); ok {
			return bb, true
		}
	}
	f.fail(ErrUnsupportedIR, fmt.Sprintf("%s expects a block as operand %d", instr.Op, i), "")
	return nil, false
}

func (f *funcGen) nameOperand(instr *ir.Instruction, i int) (string, bool) {
	if i < len(instr.Operands) {
		if c, ok := instr.Operands[i].(ir.Constant); ok {
			if name, ok := c.Val.(string); ok {
				return name, true
			}
		}
	}
	f.fail(ErrUnsupportedIR, fmt.Sprintf("%s expects a name as operand %d", instr.Op, i), "")
	return "", false
}

func (f *funcGen) intOperand(instr *ir.Instruction, i int) (int64, bool) {
	if i < len(instr.Operands) {
		if c, ok := instr.Operands[i].(ir.Constant); ok {
			if n, ok := toInt64(c.Val); ok {
				return n, true
			}
		}
	}
	f.fail(ErrUnsupportedIR, fmt.Sprintf("%s expects an integer as operand %d", instr.Op, i), "")
	return 0, false
}

func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	}
	return 0, false
}

package native

import (
	"fmt"
	"strings"

	"github.com/dani-gouken/lumen/internal/vm"
)

// A frameless function is one whose every register holds an Int or a
// Float, which takes and gives Ints and Floats, and calls nothing but
// functions like it: written a second time, as a Go function of Go
// numbers, it is called without a frame of the VM (see
// internal/vm/compiled.go).
type frameless struct {
	params  []kind
	returns kind
	kinds   []kind
}

func numericKind(k kind) bool { return k == intKind || k == floatKind }

// framelessFunctions finds the functions of a chunk that can be frameless.
func framelessFunctions(chunk *vm.Chunk, compiled []bool) []*frameless {
	returns := make([]kind, len(chunk.Prototypes))
	for i, p := range chunk.Prototypes {
		_, returns[i] = signature(p.Signature, p.ArgCount)
	}
	found := make([]*frameless, len(chunk.Prototypes))
	for i, p := range chunk.Prototypes {
		if !compiled[i] || !numericKind(returns[i]) {
			continue
		}
		params, _ := signature(p.Signature, p.ArgCount)
		if !allNumeric(params) {
			continue
		}
		code := chunk.Code[p.CodeStart : p.CodeStart+p.CodeSize]
		kinds, _ := registerKinds(code, p.RegisterCount, params, chunk.Constants, returns)
		if framelessCode(code, kinds, chunk.Constants, returns[i]) {
			found[i] = &frameless{params: params, returns: returns[i], kinds: kinds}
		}
	}

	// A function that calls one that is not frameless is not either; nor
	// is one that gives a call what the callee does not take
	for changed := true; changed; {
		changed = false
		for i, f := range found {
			if f == nil {
				continue
			}
			p := chunk.Prototypes[i]
			for _, instr := range chunk.Code[p.CodeStart : p.CodeStart+p.CodeSize] {
				if instr.Op != vm.OpCALLK {
					continue
				}
				callee := int(instr.C)
				if callee >= len(found) || found[callee] == nil || !argumentsFit(f.kinds, instr, found[callee].params) {
					found[i] = nil
					changed = true
					break
				}
			}
		}
	}
	return found
}

func allNumeric(kinds []kind) bool {
	for _, k := range kinds {
		if !numericKind(k) {
			return false
		}
	}
	return true
}

// argumentsFit reports whether the arguments of a call are of the kinds
// the callee takes.
func argumentsFit(kinds []kind, call vm.Instr, params []kind) bool {
	if int(call.B) != len(params) {
		return false
	}
	for i, want := range params {
		k := int(call.A) + 1 + i
		if k >= len(kinds) || kinds[k] != want {
			return false
		}
	}
	return true
}

// framelessCode reports whether every instruction of a function can be
// written on Go numbers alone.
func framelessCode(code []vm.Instr, kinds []kind, consts []vm.Value, returns kind) bool {
	numeric := func(k int) bool { return k < len(kinds) && numericKind(kinds[k]) }
	for i := 0; i < len(code); i++ {
		in := code[i]
		next := vm.Instr{}
		if i+1 < len(code) {
			next = code[i+1]
		}
		for _, k := range reads(in, next) {
			if !numeric(k) {
				return false
			}
		}
		if dest, ok := writes(in); ok && !numeric(dest) {
			return false
		}
		switch in.Op {
		case vm.OpNOP, vm.OpLOADI, vm.OpMOV, vm.OpJMP, vm.OpCALLK,
			vm.OpADD, vm.OpSUB, vm.OpMUL, vm.OpDIV, vm.OpMOD, vm.OpADDI, vm.OpSUBI,
			vm.OpNEG, vm.OpTONUM, vm.OpJNLT, vm.OpJNLE, vm.OpJNEQ, vm.OpJNNE,
			vm.OpJNLTI, vm.OpJNLEI, vm.OpJNGTI, vm.OpJNGEI, vm.OpJNEQI, vm.OpJNNEI:
		case vm.OpLOADK:
			if !numericKind(constantKind(consts[in.B])) {
				return false
			}
		case vm.OpADDK, vm.OpSUBK, vm.OpMULK, vm.OpDIVK, vm.OpMODK:
			if !numericKind(constantKind(consts[in.C])) {
				return false
			}
		case vm.OpJNLTK, vm.OpJNLEK, vm.OpJNGTK, vm.OpJNGEK, vm.OpJNEQK, vm.OpJNNEK:
			if !numericKind(constantKind(consts[in.B])) {
				return false
			}
		case vm.OpTOINT:
			if kinds[in.B] != intKind {
				return false
			}
		case vm.OpRET:
			// What it gives is what the signature says, which callers count on
			if kinds[in.A] != returns {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// goType is the Go type of a kind.
func goType(k kind) string {
	if k == floatKind {
		return "float64"
	}
	return "int64"
}

// writeFrameless writes the frameless form of a function.
func writeFrameless(out *strings.Builder, chunk *vm.Chunk, index int, all []*frameless, prefix string) {
	p := chunk.Prototypes[index]
	f := all[index]
	code := chunk.Code[p.CodeStart : p.CodeStart+p.CodeSize]
	jumpedTo, _ := targets(code, p.CodeStart)

	var params []string
	for i, k := range f.params {
		params = append(params, fmt.Sprintf("a%d %s", i, goType(k)))
	}
	fmt.Fprintf(out, "// %sL%d is %s without a frame.\n", prefix, index, p.Name)
	fmt.Fprintf(out, "func %sL%d(vm *VM%s) (%s, error) {\n", prefix, index, strings.Join(append([]string{""}, params...), ", "), goType(f.returns))
	local := make([]bool, len(f.kinds))
	for k, kind := range f.kinds {
		if !numericKind(kind) {
			continue
		}
		local[k] = true
		fmt.Fprintf(out, "\tvar %s %s\n\t_ = %s\n", localName(k, kind), goType(kind), localName(k, kind))
	}
	for i := range f.params {
		fmt.Fprintf(out, "\t%s = a%d\n", localName(i, f.kinds[i]), i)
	}
	w := &writer{out: out, consts: chunk.Constants, registers: p.RegisterCount, code: code, start: p.CodeStart,
		prefix: prefix, kinds: f.kinds, local: local, frameless: true, proto: index, light: all}
	for i := 0; i < len(code); i++ {
		pc := p.CodeStart + i
		if jumpedTo[pc] {
			fmt.Fprintf(out, "L%d:\n", pc)
		}
		w.pc, w.instr = pc, code[i]
		if !w.typed() {
			panic(fmt.Sprintf("native: %v of %s is not written without a frame", code[i].Op, p.Name))
		}
	}
	out.WriteString("\treturn 0, nil\n}\n\n")
}

func localName(k int, kind kind) string {
	if kind == floatKind {
		return fmt.Sprintf("f%d", k)
	}
	return fmt.Sprintf("i%d", k)
}

// framelessCall writes a call of a frameless function, from code with or
// without a frame, if the arguments are known to be what it takes, and
// reports whether it did. The result is left in r0.
func (w *writer) framelessCall() bool {
	in := w.instr
	callee := int(in.C)
	if callee >= len(w.light) || w.light[callee] == nil {
		return false
	}
	f := w.light[callee]
	var args []string
	for i, want := range f.params {
		arg := w.register(int(in.A) + 1 + i)
		if arg.kind != want {
			return false
		}
		args = append(args, arg.expr)
	}
	call := fmt.Sprintf("%sL%d(vm%s)", w.prefix, callee, strings.Join(append([]string{""}, args...), ", "))
	w.line("{")
	if w.frameless {
		w.line("if vm.interrupted.Load() || vm.outOfSteps() { return 0, vm.framelessStopped(%d, %d) }", w.proto, w.pc)
		w.line("if vm.frameCount+vm.framelessCalls >= vm.maxCallDepth { return 0, vm.framelessTooDeep(%d, %d) }", w.proto, w.pc)
		w.line("vm.framelessCalls++")
		w.line("result, err := %s", call)
		w.line("vm.framelessCalls--")
		w.line("if err != nil { return 0, vm.framelessCalled(err, %d, %d) }", w.proto, w.pc)
		w.store(0, f.returns, "result")
	} else {
		w.at()
		w.line("if vm.interrupted.Load() || vm.outOfSteps() { return NewNull(), vm.stopped() }")
		w.line("if vm.frameCount+vm.framelessCalls >= vm.maxCallDepth { return NewNull(), vm.tooDeep() }")
		w.line("vm.framelessCalls++")
		w.line("result, err := %s", call)
		w.line("vm.framelessCalls--")
		w.line("if err != nil { return NewNull(), vm.framelessArrived(err) }")
		w.store(0, f.returns, "result")
	}
	w.line("}")
	return true
}

package native

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/vm"
)

// The instructions on registers known to hold Ints or Floats, written on
// Go's int64 and float64: no tag to look at, and a register that is a Go
// variable stays one. Each does what its instruction does for values of
// those kinds, failures included.

// name is the Go variable of a register that is one.
func (w *writer) name(k int) string {
	if w.kinds[k] == floatKind {
		return fmt.Sprintf("f%d", k)
	}
	return fmt.Sprintf("i%d", k)
}

// operand is how a typed instruction reads something: a register, a
// constant or a number written in the instruction.
type operand struct {
	kind kind
	expr string
}

func (w *writer) register(k int) operand {
	switch {
	case w.local[k]:
		return operand{w.kinds[k], w.name(k)}
	case w.kinds[k] == intKind:
		return operand{intKind, fmt.Sprintf("intOf(r[%d])", k)}
	case w.kinds[k] == floatKind:
		return operand{floatKind, fmt.Sprintf("floatOf(r[%d])", k)}
	}
	return operand{kind: other}
}

func (w *writer) constant(index int) operand {
	if n, ok := w.intConstant(index); ok {
		return operand{intKind, fmt.Sprintf("int64(%d)", n)}
	}
	if x, ok := w.floatConstant(index); ok {
		return operand{floatKind, x}
	}
	return operand{kind: other}
}

func immediate(n int) operand {
	return operand{intKind, fmt.Sprintf("int64(%d)", n)}
}

// asFloat reads an operand as a Float, as arithmetic with one does.
func asFloat(o operand) string {
	if o.kind == intKind {
		return "float64(" + o.expr + ")"
	}
	return o.expr
}

// store writes a result of a kind into a register.
func (w *writer) store(k int, of kind, expr string) {
	switch {
	case w.local[k]:
		w.line("%s = %s", w.name(k), expr)
	case of == intKind:
		w.line("r[%d] = NewInt(%s)", k, expr)
	default:
		w.line("r[%d] = NewNumber(%s)", k, expr)
	}
}

// boxed is a register's value as the VM holds it.
func (w *writer) boxed(k int) string {
	if !w.local[k] {
		return fmt.Sprintf("r[%d]", k)
	}
	if w.kinds[k] == floatKind {
		return fmt.Sprintf("NewNumber(f%d)", k)
	}
	return fmt.Sprintf("NewInt(i%d)", k)
}

func known(operands ...operand) bool {
	for _, o := range operands {
		if o.kind != intKind && o.kind != floatKind {
			return false
		}
	}
	return true
}

// overflow is what fails when an Int does not fit.
func (w *writer) overflow() string {
	return w.failure("integer overflow")
}

// failure fails the instruction with a message: where it is, for a
// function with a frame; with where it is, for one without.
func (w *writer) failure(message string) string {
	if w.frameless {
		return fmt.Sprintf("{ return 0, vm.framelessFailed(%d, %d, %q) }", w.proto, w.pc, message)
	}
	return fmt.Sprintf("{ vm.ip = %d; return NewNull(), vm.runtimeError(%q) }", w.pc, message)
}

// typed writes the instruction on Ints and Floats, if what it works on is
// known to be of those kinds, and reports whether it did.
func (w *writer) typed() bool {
	in := w.instr
	A, B, C := int(in.A), int(in.B), int(in.C)

	switch in.Op {
	case vm.OpLOADI:
		if !w.local[A] {
			return false
		}
		w.store(A, intKind, fmt.Sprint(B))
		return true

	case vm.OpLOADK:
		k := w.constant(B)
		if !w.local[A] || !known(k) {
			return false
		}
		w.store(A, k.kind, k.expr)
		return true

	case vm.OpMOV:
		if !w.local[A] && !w.local[B] {
			return false
		}
		if w.local[A] {
			src := w.register(B)
			if !known(src) {
				return false
			}
			w.store(A, src.kind, src.expr)
		} else {
			w.line("r[%d] = %s", A, w.boxed(B))
		}
		return true

	case vm.OpADD, vm.OpSUB, vm.OpMUL, vm.OpDIV, vm.OpMOD:
		return w.arithmetic(A, w.register(B), w.register(C))
	case vm.OpADDK, vm.OpSUBK, vm.OpMULK, vm.OpDIVK, vm.OpMODK:
		return w.arithmetic(A, w.register(B), w.constant(C))
	case vm.OpADDI:
		return w.arithmetic(A, w.register(B), immediate(C))
	case vm.OpSUBI:
		return w.arithmetic(A, w.register(B), immediate(C))

	case vm.OpNEG:
		x := w.register(B)
		if !known(x) {
			return false
		}
		if x.kind == intKind {
			w.line("if n := -%s; n < MinInt || n > MaxInt %s else {", x.expr, w.overflow())
			w.store(A, intKind, "n")
			w.line("}")
		} else {
			w.store(A, floatKind, "-"+x.expr)
		}
		return true

	case vm.OpTONUM:
		x := w.register(B)
		if !known(x) {
			return false
		}
		w.store(A, floatKind, asFloat(x))
		return true

	case vm.OpTOINT:
		x := w.register(B)
		if x.kind != intKind {
			return false
		}
		w.store(A, intKind, x.expr)
		return true

	case vm.OpLT, vm.OpLE, vm.OpGT, vm.OpGE, vm.OpEQ, vm.OpNE:
		x, y := w.register(B), w.register(C)
		if !known(x, y) {
			return false
		}
		w.line("r[%d] = NewBool(%s)", A, comparison(in.Op, x, y))
		return true

	case vm.OpJNLT, vm.OpJNLE, vm.OpJNEQ, vm.OpJNNE:
		x, y := w.register(A), w.register(B)
		if !known(x, y) {
			return false
		}
		test := map[vm.OpCode]vm.OpCode{vm.OpJNLT: vm.OpLT, vm.OpJNLE: vm.OpLE, vm.OpJNEQ: vm.OpEQ, vm.OpJNNE: vm.OpNE}[in.Op]
		w.line("if !(%s) %s", comparison(test, x, y), w.jump(C))
		return true

	case vm.OpJNLTI, vm.OpJNLEI, vm.OpJNGTI, vm.OpJNGEI, vm.OpJNEQI, vm.OpJNNEI:
		x := w.register(A)
		if !known(x) {
			return false
		}
		test := map[vm.OpCode]vm.OpCode{vm.OpJNLTI: vm.OpLT, vm.OpJNLEI: vm.OpLE, vm.OpJNGTI: vm.OpGT, vm.OpJNGEI: vm.OpGE, vm.OpJNEQI: vm.OpEQ, vm.OpJNNEI: vm.OpNE}[in.Op]
		w.line("if !(%s) %s", comparison(test, x, immediate(B)), w.jump(C))
		return true

	case vm.OpJNLTK, vm.OpJNLEK, vm.OpJNGTK, vm.OpJNGEK, vm.OpJNEQK, vm.OpJNNEK:
		x, k := w.register(A), w.constant(B)
		if !known(x, k) {
			return false
		}
		test := map[vm.OpCode]vm.OpCode{vm.OpJNLTK: vm.OpLT, vm.OpJNLEK: vm.OpLE, vm.OpJNGTK: vm.OpGT, vm.OpJNGEK: vm.OpGE, vm.OpJNEQK: vm.OpEQ, vm.OpJNNEK: vm.OpNE}[in.Op]
		w.line("if !(%s) %s", comparison(test, x, k), w.jump(C))
		return true

	case vm.OpNOP:
		return w.frameless

	case vm.OpJMP:
		if !w.frameless {
			return false
		}
		w.line("%s", w.jump(A))
		return true

	case vm.OpCALLK:
		return w.framelessCall()

	case vm.OpRET:
		if w.frameless {
			w.line("return %s, nil", w.name(A))
			return true
		}
		if !w.local[A] {
			return false
		}
		w.line("{")
		w.line("result := %s", w.boxed(A))
		w.line("if len(vm.stk.open) > 0 { vm.closeUpvalues(vm.base) }")
		w.line("return result, nil")
		w.line("}")
		return true
	}
	return false
}

// comparison is a test of two numbers: as Ints when both are, as Floats
// otherwise, which is how two values that are numbers compare.
func comparison(op vm.OpCode, x, y operand) string {
	symbol := map[vm.OpCode]string{vm.OpLT: "<", vm.OpLE: "<=", vm.OpGT: ">", vm.OpGE: ">=", vm.OpEQ: "==", vm.OpNE: "!="}[op]
	if x.kind == intKind && y.kind == intKind {
		return fmt.Sprintf("%s %s %s", x.expr, symbol, y.expr)
	}
	return fmt.Sprintf("%s %s %s", asFloat(x), symbol, asFloat(y))
}

// arithmetic writes + - * / % of two numbers into a register, if both
// are known to be numbers.
func (w *writer) arithmetic(dest int, x, y operand) bool {
	if !known(x, y) {
		return false
	}
	op := w.instr.Op
	switch op {
	case vm.OpADDK, vm.OpADDI:
		op = vm.OpADD
	case vm.OpSUBK, vm.OpSUBI:
		op = vm.OpSUB
	case vm.OpMULK:
		op = vm.OpMUL
	case vm.OpDIVK:
		op = vm.OpDIV
	case vm.OpMODK:
		op = vm.OpMOD
	}

	if x.kind == intKind && y.kind == intKind {
		w.line("{")
		w.line("x, y := %s, %s", x.expr, y.expr)
		switch op {
		case vm.OpADD:
			w.line("n := x + y")
			w.line("if n < MinInt || n > MaxInt %s", w.overflow())
		case vm.OpSUB:
			w.line("n := x - y")
			w.line("if n < MinInt || n > MaxInt %s", w.overflow())
		case vm.OpMUL:
			w.line("n, fits := mulInts(x, y)")
			w.line("if !fits %s", w.overflow())
		case vm.OpDIV:
			w.line("if y == 0 %s", w.failure("division by zero"))
			w.line("n := x / y")
			w.line("if n < MinInt || n > MaxInt %s", w.overflow())
		case vm.OpMOD:
			w.line("if y == 0 %s", w.failure("division by zero"))
			w.line("n := x %% y")
		}
		w.store(dest, intKind, "n")
		w.line("}")
		return true
	}

	a, b := asFloat(x), asFloat(y)
	switch op {
	case vm.OpADD:
		w.store(dest, floatKind, a+" + "+b)
	case vm.OpSUB:
		w.store(dest, floatKind, a+" - "+b)
	case vm.OpMUL:
		w.store(dest, floatKind, a+" * "+b)
	case vm.OpDIV:
		w.store(dest, floatKind, a+" / "+b)
	case vm.OpMOD:
		w.store(dest, floatKind, "math.Mod("+a+", "+b+")")
	}
	return true
}

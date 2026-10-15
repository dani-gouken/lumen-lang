package native

import (
	"strings"

	"github.com/dani-gouken/lumen/internal/vm"
)

// What a register of a function holds, as far as its bytecode shows: an
// Int whatever path was taken, a Float, or anything (other). A register
// whose every write puts an Int in it holds an Int; its value can then be
// a Go variable instead of a register of the stack.
type kind byte

const (
	unset kind = iota // Nothing writes it, or nothing yet worked out
	intKind
	floatKind
	other
)

// join is what a register holds that two writes put a value in.
func join(a, b kind) kind {
	switch {
	case a == unset:
		return b
	case b == unset:
		return a
	case a == b:
		return a
	}
	return other
}

// numeric is what arithmetic on two values gives: an Int of two Ints, a
// Float of two numbers otherwise. unset while an operand is.
func numeric(a, b kind) kind {
	switch {
	case a == unset || b == unset:
		return unset
	case a == other || b == other:
		return other
	case a == intKind && b == intKind:
		return intKind
	}
	return floatKind
}

// constantKind is what a constant of the chunk is.
func constantKind(k vm.Value) kind {
	if k.IsInt() {
		return intKind
	}
	if k.IsNumber() {
		return floatKind
	}
	return other
}

// writes returns the register an instruction writes, if it writes one.
// A call writes r0, where its result is left.
func writes(instr vm.Instr) (int, bool) {
	switch instr.Op {
	case vm.OpMOV, vm.OpLOADK, vm.OpLOADNULL, vm.OpLOADBOOL, vm.OpLOADI,
		vm.OpADD, vm.OpSUB, vm.OpMUL, vm.OpDIV, vm.OpMOD,
		vm.OpADDK, vm.OpSUBK, vm.OpMULK, vm.OpDIVK, vm.OpMODK, vm.OpADDI, vm.OpSUBI,
		vm.OpNEG, vm.OpNOT, vm.OpTOINT, vm.OpTONUM,
		vm.OpBAND, vm.OpBOR, vm.OpBXOR, vm.OpSHL, vm.OpSHR,
		vm.OpEQ, vm.OpNE, vm.OpLT, vm.OpLE, vm.OpGT, vm.OpGE,
		vm.OpNEWFUNC, vm.OpNEWEXFUNC, vm.OpLDCAP, vm.OpNEWSTR, vm.OpNEWARRAY, vm.OpNEWOBJ,
		vm.OpGETINDEX, vm.OpGETFIELD, vm.OpARRLEN, vm.OpHASFIELD, vm.OpDELFIELD,
		vm.OpONCE, vm.OpISTYPE, vm.OpGETMETHOD:
		return int(instr.A), true
	case vm.OpCALL, vm.OpCALLK, vm.OpCALLX, vm.OpCALLM:
		return 0, true
	}
	return 0, false
}

// reads returns the registers an instruction reads. next is the word after
// it, which a LONG prefix is followed by.
func reads(instr vm.Instr, next vm.Instr) []int {
	A, B, C := int(instr.A), int(instr.B), int(instr.C)
	window := func(from, count int) []int {
		var found []int
		for i := from; i < from+count; i++ {
			found = append(found, i)
		}
		return found
	}
	switch instr.Op {
	case vm.OpMOV, vm.OpADDI, vm.OpSUBI, vm.OpNEG, vm.OpNOT, vm.OpTOINT, vm.OpTONUM,
		vm.OpADDK, vm.OpSUBK, vm.OpMULK, vm.OpDIVK, vm.OpMODK,
		vm.OpGETFIELD, vm.OpARRLEN, vm.OpHASFIELD, vm.OpDELFIELD, vm.OpISTYPE, vm.OpGETMETHOD:
		return []int{B}
	case vm.OpADD, vm.OpSUB, vm.OpMUL, vm.OpDIV, vm.OpMOD,
		vm.OpBAND, vm.OpBOR, vm.OpBXOR, vm.OpSHL, vm.OpSHR,
		vm.OpEQ, vm.OpNE, vm.OpLT, vm.OpLE, vm.OpGT, vm.OpGE, vm.OpGETINDEX:
		return []int{B, C}
	case vm.OpJMPIF, vm.OpJMPIFNOT, vm.OpRET, vm.OpCLOSEUP, vm.OpCAPUPVAL, vm.OpSWITCH,
		vm.OpJNLTI, vm.OpJNLEI, vm.OpJNGTI, vm.OpJNGEI, vm.OpJNEQI, vm.OpJNNEI,
		vm.OpJNLTK, vm.OpJNLEK, vm.OpJNGTK, vm.OpJNGEK, vm.OpJNEQK, vm.OpJNNEK, vm.OpJNTYPE:
		return []int{A}
	case vm.OpJNLT, vm.OpJNLE, vm.OpJNEQ, vm.OpJNNE:
		return []int{A, B}
	case vm.OpCAPLOCAL:
		return []int{A, C}
	case vm.OpSTCAP:
		return []int{B}
	case vm.OpSETFIELD:
		return []int{A, C}
	case vm.OpSETELEM, vm.OpSETINDEX:
		return []int{A, B, C}
	case vm.OpARRPUSH:
		return []int{A, B}
	case vm.OpCALLK, vm.OpCALLX, vm.OpCALLM:
		return window(A+1, B)
	case vm.OpCALL:
		return window(A, B+1)
	case vm.OpLONG:
		if next.Op == vm.OpJMPIF32 || next.Op == vm.OpJMPIFNOT32 {
			return []int{int(next.A)}
		}
	}
	return nil
}

// signature reads what a function takes and gives from its signature,
// "fn(int,[string])->float": an Int or a Float the type checker made sure
// of, anything else other. A function without one (a method, assembly)
// takes and gives what is not known.
func signature(sig string, args int) ([]kind, kind) {
	params := make([]kind, args)
	for i := range params {
		params[i] = other
	}
	if !strings.HasPrefix(sig, "fn(") {
		return params, other
	}
	// The parameters end where the parenthesis that opened them closes
	// "->" is an arrow, not the end of a pair of brackets
	closes := func(i int) bool {
		switch sig[i] {
		case ')', ']', '}':
			return true
		case '>':
			return sig[i-1] != '-'
		}
		return false
	}
	depth, end := 0, -1
	for i := 2; i < len(sig) && end < 0; i++ {
		switch {
		case strings.IndexByte("([{<", sig[i]) >= 0:
			depth++
		case closes(i):
			depth--
			if depth == 0 {
				end = i
			}
		}
	}
	if end < 0 {
		return params, other
	}
	var types []string
	depth, from := 0, 3
	for i := 3; i < end; i++ {
		switch {
		case strings.IndexByte("([{<", sig[i]) >= 0:
			depth++
		case closes(i):
			depth--
		case sig[i] == ',':
			if depth == 0 {
				types = append(types, sig[from:i])
				from = i + 1
			}
		}
	}
	if from < end {
		types = append(types, sig[from:end])
	}
	of := func(t string) kind {
		switch t {
		case "int":
			return intKind
		case "float":
			return floatKind
		}
		return other
	}
	if len(types) == args {
		for i, t := range types {
			params[i] = of(t)
		}
	}
	return params, of(strings.TrimPrefix(sig[end+1:], "->"))
}

// result is what an instruction writes, given what its registers hold.
// returns is what each function of the chunk gives.
func result(instr vm.Instr, kinds []kind, consts []vm.Value, returns []kind) kind {
	B, C := int(instr.B), int(instr.C)
	switch instr.Op {
	case vm.OpCALLK:
		if C < len(returns) {
			return returns[C]
		}
		return other
	case vm.OpLOADI:
		return intKind
	case vm.OpLOADK:
		return constantKind(consts[B])
	case vm.OpMOV:
		return kinds[B]
	case vm.OpADD, vm.OpSUB, vm.OpMUL, vm.OpDIV, vm.OpMOD:
		return numeric(kinds[B], kinds[C])
	case vm.OpADDK, vm.OpSUBK, vm.OpMULK, vm.OpDIVK, vm.OpMODK:
		return numeric(kinds[B], constantKind(consts[C]))
	case vm.OpADDI, vm.OpSUBI:
		return numeric(kinds[B], intKind)
	case vm.OpNEG:
		return numeric(kinds[B], kinds[B])
	case vm.OpTONUM:
		// A Float, or a failure
		return floatKind
	case vm.OpTOINT, vm.OpBAND, vm.OpBOR, vm.OpBXOR, vm.OpSHL, vm.OpSHR, vm.OpARRLEN:
		// An Int, or a failure
		return intKind
	}
	return other
}

// registerKinds works out what each register of a function holds, and
// which can be Go variables: those that hold an Int or a Float on every
// path, and that no closure captures (it follows the register itself).
func registerKinds(code []vm.Instr, registers int, params []kind, consts []vm.Value, returns []kind) ([]kind, []bool) {
	kinds := make([]kind, registers)
	// What the caller gave is what the function's signature says
	for i := 0; i < len(params) && i < registers; i++ {
		kinds[i] = params[i]
	}
	// Nor is what a closure that captured a register writes into it
	captured := make([]bool, registers)
	for i := 0; i < len(code); i += step(code, i) {
		switch code[i].Op {
		case vm.OpCAPLOCAL:
			captured[code[i].C] = true
		case vm.OpCLOSEUP:
			captured[code[i].A] = true
		}
	}
	for i, is := range captured {
		if is {
			kinds[i] = other
		}
	}
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(code); i += step(code, i) {
			dest, ok := writes(code[i])
			if !ok || dest >= registers || captured[dest] {
				continue
			}
			joined := join(kinds[dest], result(code[i], kinds, consts, returns))
			if joined != kinds[dest] {
				kinds[dest] = joined
				changed = true
			}
		}
	}

	local := make([]bool, registers)
	for i, k := range kinds {
		local[i] = k == intKind || k == floatKind
	}
	// r0 is where calls leave their results, and the first argument
	if registers > 0 {
		local[0] = false
	}
	return kinds, local
}

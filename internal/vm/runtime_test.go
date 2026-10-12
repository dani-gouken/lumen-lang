package vm

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"
)

// execMain runs instructions as a main function with 8 registers. String
// constants are given as pool indices, the way the assembler and the
// compiler emit them.
func execMain(t *testing.T, pool []string, constants []Value, code ...Instr) (Value, string, error) {
	t.Helper()

	chunk := NewChunk()
	chunk.StringPool = pool
	chunk.Constants = constants
	chunk.Code = code
	chunk.ExternNames = []string{"print", "println", "toString", "push", "__cast"}
	chunk.Prototypes = []FunctionPrototype{{Name: "main", CodeSize: len(code), RegisterCount: 8}}

	var out bytes.Buffer
	machine := NewVM()
	machine.SetOutput(&out)
	result, err := machine.Exec(chunk)
	return result, out.String(), err
}

func TestStringOperations(t *testing.T) {
	pool := []string{"foo", "bar", "foobar"}
	consts := []Value{NewInt(0), NewInt(1), NewInt(2)}
	load := []Instr{
		NewInstr(OpNEWSTR, 1, 0, 0), // r1 = "foo"
		NewInstr(OpNEWSTR, 2, 1, 0), // r2 = "bar"
		NewInstr(OpNEWSTR, 3, 2, 0), // r3 = "foobar"
		NewInstr(OpADD, 4, 1, 2),    // r4 = "foo" + "bar"
	}

	tests := []struct {
		name string
		op   Instr
		want string
	}{
		{"concatenation", NewInstr(OpMOV, 5, 4, 0), "foobar"},
		{"equal by content", NewInstr(OpEQ, 5, 4, 3), "true"},
		{"not equal", NewInstr(OpNE, 5, 4, 3), "false"},
		{"different strings", NewInstr(OpEQ, 5, 1, 2), "false"},
		{"less than", NewInstr(OpLT, 5, 2, 1), "true"},
		{"greater or equal", NewInstr(OpGE, 5, 2, 1), "false"},
		{"length", NewInstr(OpARRLEN, 5, 4, 0), "6"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := append(append([]Instr{}, load...), tt.op, NewInstr(OpRET, 5, 0, 0))
			result, _, err := execMain(t, pool, consts, code...)
			if err != nil {
				t.Fatalf("execution failed: %v", err)
			}
			if got := FormatValue(result); got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestStringIndexing(t *testing.T) {
	result, _, err := execMain(t, []string{"héllo"}, []Value{NewInt(0)},
		NewInstr(OpNEWSTR, 1, 0, 0),
		NewInstr(OpLOADI, 2, 1, 0),
		NewInstr(OpGETINDEX, 3, 1, 2), // Characters, not bytes
		NewInstr(OpRET, 3, 0, 0),
	)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if got := FormatValue(result); got != "é" {
		t.Errorf("expected é, got %q", got)
	}
}

func TestFieldNamesFromStringPool(t *testing.T) {
	// Field names are constants holding a string pool index
	result, _, err := execMain(t, []string{"age"}, []Value{NewInt(0)},
		NewInstr(OpNEWOBJ, 1, 0, 0),
		NewInstr(OpLOADI, 2, 30, 0),
		NewInstr(OpSETFIELD, 1, 0, 2),
		NewInstr(OpHASFIELD, 4, 1, 0),
		NewInstr(OpGETFIELD, 3, 1, 0),
		NewInstr(OpJMPIFNOT, 4, 8, 0),
		NewInstr(OpRET, 3, 0, 0),
		NewInstr(OpNOP, 0, 0, 0),
		NewInstr(OpLOADNULL, 3, 0, 0),
		NewInstr(OpRET, 3, 0, 0),
	)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() || result.AsInt() != 30 {
		t.Errorf("expected 30, got %s", FormatValue(result))
	}
}

func TestRuntimeTypeAndBoundsErrors(t *testing.T) {
	pool := []string{"field", "text"}
	consts := []Value{NewInt(0), NewInt(1)}

	tests := []struct {
		name string
		code []Instr
		want string
	}{
		{"read past the end", []Instr{
			NewInstr(OpNEWARRAY, 1, 2, 0),
			NewInstr(OpLOADI, 2, 0, 0),
			NewInstr(OpGETINDEX, 3, 1, 2),
		}, "index out of bounds: 0 (length 0)"},
		{"write past the capacity", []Instr{
			NewInstr(OpNEWARRAY, 1, 2, 0),
			NewInstr(OpLOADI, 2, 5, 0),
			NewInstr(OpSETINDEX, 1, 2, 2),
		}, "index out of bounds: 5"},
		{"index a number", []Instr{
			NewInstr(OpLOADI, 1, 7, 0),
			NewInstr(OpLOADI, 2, 0, 0),
			NewInstr(OpGETINDEX, 3, 1, 2),
		}, "GETINDEX requires array, got Int"},
		{"field of an array", []Instr{
			NewInstr(OpNEWARRAY, 1, 0, 0),
			NewInstr(OpGETFIELD, 2, 1, 0),
		}, "GETFIELD requires object, got array"},
		{"field of a string", []Instr{
			NewInstr(OpNEWSTR, 1, 1, 0),
			NewInstr(OpLOADI, 2, 1, 0),
			NewInstr(OpSETFIELD, 1, 0, 2),
		}, "SETFIELD requires object, got String"},
		{"push to an object", []Instr{
			NewInstr(OpNEWOBJ, 1, 0, 0),
			NewInstr(OpARRPUSH, 1, 1, 0),
		}, "ARRPUSH requires array, got struct"},
		{"length of a bool", []Instr{
			NewInstr(OpLOADBOOL, 1, 1, 0),
			NewInstr(OpARRLEN, 2, 1, 0),
		}, "ARRLEN requires array, got Bool"},
		{"string plus number", []Instr{
			NewInstr(OpNEWSTR, 1, 1, 0),
			NewInstr(OpLOADI, 2, 1, 0),
			NewInstr(OpADD, 3, 1, 2),
		}, "ADD cannot concatenate String and Int"},
		{"integer division by zero", []Instr{
			NewInstr(OpLOADI, 1, 1, 0),
			NewInstr(OpLOADI, 2, 0, 0),
			NewInstr(OpDIV, 3, 1, 2),
		}, "division by zero"},
		{"integer modulo by zero", []Instr{
			NewInstr(OpLOADI, 1, 1, 0),
			NewInstr(OpLOADI, 2, 0, 0),
			NewInstr(OpMOD, 3, 1, 2),
		}, "division by zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := append(tt.code, NewInstr(OpRET, 0, 0, 0))
			_, _, err := execMain(t, pool, consts, code...)
			if err == nil {
				t.Fatal("expected a runtime error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestBuiltinFunctions(t *testing.T) {
	// print("n =", 42); println(toString(true)); push(arr, 7); return arr
	result, out, err := execMain(t, []string{"n ="}, []Value{NewInt(0)},
		NewInstr(OpNEWEXFUNC, 1, 0, 0), // print
		NewInstr(OpNEWSTR, 2, 0, 0),
		NewInstr(OpLOADI, 3, 42, 0),
		NewInstr(OpCALL, 1, 2, 0),

		NewInstr(OpNEWEXFUNC, 1, 2, 0), // toString
		NewInstr(OpLOADBOOL, 2, 1, 0),
		NewInstr(OpCALL, 1, 1, 0),
		NewInstr(OpMOV, 2, 0, 0),
		NewInstr(OpNEWEXFUNC, 1, 1, 0), // println
		NewInstr(OpCALL, 1, 1, 0),

		NewInstr(OpNEWARRAY, 5, 0, 0),
		NewInstr(OpNEWEXFUNC, 1, 3, 0), // push
		NewInstr(OpMOV, 2, 5, 0),
		NewInstr(OpLOADI, 3, 7, 0),
		NewInstr(OpCALL, 1, 2, 0),
		NewInstr(OpRET, 5, 0, 0),
	)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if want := "n = 42true\n"; out != want {
		t.Errorf("expected output %q, got %q", want, out)
	}
	if got := FormatValue(result); got != "[7]" {
		t.Errorf("expected [7], got %s", got)
	}
}

func TestFormatValue(t *testing.T) {
	heap := NewHeap(0)
	ptr := func(obj Obj) Value { return NewHeapPtr(uintptr(unsafe.Pointer(obj.Header()))) }

	str, _, _ := heap.AllocString("text")
	arr, _, _ := heap.AllocArray(2)
	arr.Append(NewInt(1))
	arr.Append(ptr(str))
	obj, _, _ := heap.AllocObject()
	obj.Set("b", NewBool(true))
	obj.Set("a", ptr(arr))

	// A structure that contains itself must still format
	loop, _, _ := heap.AllocArray(1)
	loop.Append(ptr(loop))

	tests := []struct {
		name  string
		value Value
		want  string
	}{
		{"int", NewInt(-12), "-12"},
		{"float", NewNumber(2.5), "2.5"},
		{"whole float", NewNumber(3), "3"},
		{"bool", NewBool(false), "false"},
		{"null", NewNull(), "null"},
		{"string", ptr(str), "text"},
		{"array quotes nested strings", ptr(arr), `[1, "text"]`},
		{"object sorts its fields", ptr(obj), `{a: [1, "text"], b: true}`},
		{"self reference", ptr(loop), "[[[[[[[[[...]]]]]]]]]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatValue(tt.value); got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

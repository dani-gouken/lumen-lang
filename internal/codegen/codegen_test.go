package codegen

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/ir"
	"github.com/dani-gouken/lumen/internal/vm"
)

func intConst(n int64) ir.Constant    { return ir.Constant{Val: n, Type: "Int"} }
func funcRef(name string) ir.Constant { return ir.Constant{Val: name, Type: "FuncRef"} }

// generate lowers the functions to bytecode and fails the test on any error.
func generate(t *testing.T, fns ...*ir.Function) *vm.Chunk {
	t.Helper()
	errs := errors.NewHandler()
	chunk := NewGenerator(&ir.Program{Functions: fns}, []string{"print", "toString"}, errs).Generate()
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic: %s: %s", d.Code, d.Message)
	}
	if t.Failed() {
		t.FailNow()
	}
	return chunk
}

// run executes a chunk and returns what main returned.
func run(t *testing.T, chunk *vm.Chunk) vm.Value {
	t.Helper()
	result, err := vm.NewVM().Exec(chunk)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	return result
}

func expectInt(t *testing.T, got vm.Value, want int64) {
	t.Helper()
	if !got.IsInt() || got.AsInt() != want {
		t.Errorf("expected Int %d, got %s", want, vm.FormatValue(got))
	}
}

func proto(t *testing.T, chunk *vm.Chunk, name string) vm.FunctionPrototype {
	t.Helper()
	idx := chunk.FindPrototype(name)
	if idx < 0 {
		t.Fatalf("function %s not found in chunk", name)
	}
	return chunk.Prototypes[idx]
}

func TestGenerateConstants(t *testing.T) {
	tests := []struct {
		name  string
		value ir.Constant
		check func(t *testing.T, v vm.Value)
	}{
		{"small int", intConst(7), func(t *testing.T, v vm.Value) { expectInt(t, v, 7) }},
		{"large int", intConst(100000), func(t *testing.T, v vm.Value) { expectInt(t, v, 100000) }},
		{"negative int", intConst(-3), func(t *testing.T, v vm.Value) { expectInt(t, v, -3) }},
		{"float", ir.Constant{Val: 2.5, Type: "Float"}, func(t *testing.T, v vm.Value) {
			if !v.IsNumber() || v.AsNumber() != 2.5 {
				t.Errorf("expected 2.5, got %s", vm.FormatValue(v))
			}
		}},
		{"bool", ir.Constant{Val: true, Type: "Bool"}, func(t *testing.T, v vm.Value) {
			if !v.IsBool() || !v.AsBool() {
				t.Errorf("expected true, got %s", vm.FormatValue(v))
			}
		}},
		{"null", ir.Constant{Val: nil, Type: "Null"}, func(t *testing.T, v vm.Value) {
			if !v.IsNull() {
				t.Errorf("expected null, got %s", vm.FormatValue(v))
			}
		}},
		{"string", ir.Constant{Val: "hi", Type: "String"}, func(t *testing.T, v vm.Value) {
			if got := vm.FormatValue(v); got != "hi" {
				t.Errorf("expected hi, got %s", got)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := ir.NewBuilder("main")
			b.CreateRet(b.CreateMov(tt.value))
			tt.check(t, run(t, generate(t, b.Func)))
		})
	}
}

func TestGenerateConstantsAreShared(t *testing.T) {
	b := ir.NewBuilder("main")
	b.CreateMov(intConst(100000))
	b.CreateMov(intConst(100000))
	b.CreateMov(ir.Constant{Val: "name", Type: "String"})
	obj := b.CreateNewStruct("T")
	b.CreateRet(b.CreateGetField(obj, "name"))

	chunk := generate(t, b.Func)
	// One slot for the int, one for the string used as value and field name
	if len(chunk.Constants) != 2 || len(chunk.StringPool) != 1 {
		t.Errorf("expected 2 constants and 1 pooled string, got %d and %d", len(chunk.Constants), len(chunk.StringPool))
	}
}

func TestGenerateBranchesAndLoops(t *testing.T) {
	// sum = 0; i = 0; while i < 5 { sum = sum + i; i = i + 1 }; return sum
	b := ir.NewBuilder("main")
	sum := b.CreateMov(intConst(0))
	i := b.CreateMov(intConst(0))
	cond, loop, exit := b.NewBlock(), b.NewBlock(), b.NewBlock()
	b.CreateJmp(cond)

	b.SetBlock(cond)
	inRange := b.NewRegister()
	b.Emit(ir.OpLt, inRange, i, b.CreateMov(intConst(5)))
	b.CreateBr(inRange, loop, exit)

	b.SetBlock(loop)
	b.Emit(ir.OpAdd, sum, sum, i)
	b.Emit(ir.OpAdd, i, i, b.CreateMov(intConst(1)))
	b.CreateJmp(cond)

	b.SetBlock(exit)
	b.CreateRet(sum)

	expectInt(t, run(t, generate(t, b.Func)), 10)
}

func TestGenerateCallKeepsArgumentsAcrossCalls(t *testing.T) {
	// The VM returns every call result in r0, which is also where the first
	// argument arrives. sub(a, b) calls id twice and must still see a and b.
	id := ir.NewBuilder("id")
	id.CreateRet(id.AddParam())

	sub := ir.NewBuilder("sub")
	a, bParam := sub.AddParam(), sub.AddParam()
	x := sub.CreateCall(sub.CreateMov(funcRef("id")), a)
	y := sub.CreateCall(sub.CreateMov(funcRef("id")), bParam)
	sub.CreateRet(sub.CreateSub(sub.CreateSub(x, y), sub.CreateSub(a, a)))

	main := ir.NewBuilder("main")
	main.CreateRet(main.CreateCall(main.CreateMov(funcRef("sub")), main.CreateMov(intConst(9)), main.CreateMov(intConst(4))))

	chunk := generate(t, id.Func, sub.Func, main.Func)
	expectInt(t, run(t, chunk), 5)

	if got := proto(t, chunk, "sub").ArgCount; got != 2 {
		t.Errorf("expected sub to take 2 arguments, got %d", got)
	}
}

func TestGenerateExternCall(t *testing.T) {
	b := ir.NewBuilder("main")
	text := b.CreateCall(b.CreateMov(funcRef("toString")), b.CreateMov(intConst(42)))
	b.CreateRet(text)

	chunk := generate(t, b.Func)
	if got := vm.FormatValue(run(t, chunk)); got != "42" {
		t.Errorf("expected \"42\", got %q", got)
	}
	if len(chunk.ExternNames) != 1 || chunk.ExternNames[0] != "toString" {
		t.Errorf("expected extern table [toString], got %v", chunk.ExternNames)
	}
}

func TestGenerateClosureCapturesByReference(t *testing.T) {
	// inc() { upvalue0 = upvalue0 + 1; return upvalue0 }
	inc := ir.NewBuilder("inc")
	inc.Func.UpvalueCount = 1
	next := inc.CreateAdd(inc.CreateLoadUpvalue(0), inc.CreateMov(intConst(1)))
	inc.CreateStoreUpvalue(0, next)
	inc.CreateRet(next)

	// main: count = 10; f = closure(inc, count); f(); f(); return count
	main := ir.NewBuilder("main")
	count := main.CreateMov(intConst(10))
	f := main.CreateMakeClosure(funcRef("inc"), count)
	main.CreateCall(f)
	main.CreateCall(f)
	main.CreateRet(count)

	chunk := generate(t, inc.Func, main.Func)
	expectInt(t, run(t, chunk), 12)

	if got := proto(t, chunk, "inc").UpvalueCount; got != 1 {
		t.Errorf("expected inc to have 1 upvalue, got %d", got)
	}
}

func TestGenerateArraysAndStructs(t *testing.T) {
	b := ir.NewBuilder("main")
	arr := b.NewRegister()
	b.Emit(ir.OpNewArray, arr, ir.Constant{Val: 2, Type: "Int"})
	b.Emit(ir.OpPush, nil, arr, b.CreateMov(intConst(5)))
	b.Emit(ir.OpPush, nil, arr, b.CreateMov(intConst(6)))
	b.Emit(ir.OpSetIndex, nil, arr, b.CreateMov(intConst(0)), b.CreateMov(intConst(30)))

	obj := b.CreateNewStruct("Point")
	b.Emit(ir.OpSetField, nil, obj, ir.Constant{Val: "x", Type: "String"}, b.CreateMov(intConst(4)))

	first := b.NewRegister()
	b.Emit(ir.OpGetIndex, first, arr, b.CreateMov(intConst(0)))
	length := b.NewRegister()
	b.Emit(ir.OpLen, length, arr)

	// 30 + 2 + 4
	b.CreateRet(b.CreateAdd(b.CreateAdd(first, length), b.CreateGetField(obj, "x")))

	expectInt(t, run(t, generate(t, b.Func)), 36)
}

func TestGenerateReusesTemporaryRegisters(t *testing.T) {
	// A long chain of single-use values must not need a register each
	b := ir.NewBuilder("main")
	acc := b.CreateMov(intConst(0))
	for i := 0; i < 400; i++ {
		acc = b.CreateAdd(acc, b.CreateMov(intConst(1)))
	}
	b.CreateRet(acc)

	chunk := generate(t, b.Func)
	expectInt(t, run(t, chunk), 400)

	if got := proto(t, chunk, "main").RegisterCount; got > 8 {
		t.Errorf("expected temporaries to share registers, function uses %d", got)
	}
}

func TestGenerateSkipsUnreachableBlocks(t *testing.T) {
	b := ir.NewBuilder("main")
	b.CreateRet(b.CreateMov(intConst(1)))
	// Code after a return lands in a block nothing jumps to
	b.SetBlock(b.NewBlock())
	b.CreateRet(b.CreateMov(intConst(2)))

	chunk := generate(t, b.Func)
	expectInt(t, run(t, chunk), 1)
	if got := proto(t, chunk, "main").CodeSize; got != 2 {
		t.Errorf("expected 2 instructions (load, return), got %d", got)
	}
}

func TestGenerateDebugInfo(t *testing.T) {
	b := ir.NewBuilder("main")
	b.Pos.File, b.Pos.Line, b.Pos.Col = "prog.lum", 3, 5
	b.CreateRet(b.CreateMov(intConst(1)))

	errs := errors.NewHandler()
	gen := NewGenerator(&ir.Program{Functions: []*ir.Function{b.Func}}, nil, errs)
	gen.SetSourceLines(map[int]string{3: "    return 1;"})
	chunk := gen.Generate()

	info := chunk.GetDebugInfo(0)
	if info == nil {
		t.Fatal("expected debug info on the first instruction")
	}
	if info.FilePath != "prog.lum" || info.Line != 3 || info.Column != 5 {
		t.Errorf("unexpected location: %s:%d:%d", info.FilePath, info.Line, info.Column)
	}
	if info.SourceLine != "    return 1;" || info.FuncName != "main" {
		t.Errorf("unexpected context: %q in %q", info.SourceLine, info.FuncName)
	}
}

func TestGenerateErrors(t *testing.T) {
	ret := func(name string, params int) *ir.Function {
		b := ir.NewBuilder(name)
		for i := 0; i < params; i++ {
			b.AddParam()
		}
		b.CreateRet(nil)
		return b.Func
	}

	tooManyRegisters := ir.NewBuilder("main")
	var live []*ir.Register
	for i := 0; i < 300; i++ {
		live = append(live, tooManyRegisters.CreateMov(intConst(1)))
	}
	// Reading every value in another block keeps all 300 alive at once
	tail := tooManyRegisters.NewBlock()
	tooManyRegisters.CreateJmp(tail)
	tooManyRegisters.SetBlock(tail)
	sum := live[0]
	for _, reg := range live[1:] {
		sum = tooManyRegisters.CreateAdd(sum, reg)
	}
	tooManyRegisters.CreateRet(sum)

	tooManyConstants := ir.NewBuilder("main")
	for i := 0; i < 300; i++ {
		tooManyConstants.CreateMov(intConst(int64(1000 + i)))
	}
	tooManyConstants.CreateRet(nil)

	undefined := ir.NewBuilder("main")
	undefined.CreateRet(undefined.CreateMov(funcRef("nowhere")))

	unsupported := ir.NewBuilder("main")
	unsupported.CreateRet(unsupported.CreateCallTrait(unsupported.CreateMov(intConst(1)), "T", "m"))

	tests := []struct {
		name string
		fns  []*ir.Function
		code string
		msg  string
	}{
		{"no main", []*ir.Function{ret("helper", 0)}, ErrMissingMain, "no 'main' function"},
		{"main with parameters", []*ir.Function{ret("main", 1)}, ErrMissingMain, "must not take parameters"},
		{"too many registers", []*ir.Function{tooManyRegisters.Func}, ErrTooManyRegisters, "registers"},
		{"too many constants", []*ir.Function{tooManyConstants.Func}, ErrTooManyConstants, "distinct constants"},
		{"undefined function", []*ir.Function{undefined.Func}, ErrUndefinedFunc, "undefined function 'nowhere'"},
		{"unsupported operation", []*ir.Function{unsupported.Func}, ErrUnsupportedIR, "CALL_TRAIT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := errors.NewHandler()
			NewGenerator(&ir.Program{Functions: tt.fns}, nil, errs).Generate()

			if !errs.HasErrors() {
				t.Fatal("expected an error, got none")
			}
			d := errs.Diagnostics[0]
			if d.Code != tt.code {
				t.Errorf("expected code %s, got %s (%s)", tt.code, d.Code, d.Message)
			}
			if !strings.Contains(d.Message, tt.msg) {
				t.Errorf("expected message containing %q, got %q", tt.msg, d.Message)
			}
		})
	}
}

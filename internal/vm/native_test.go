package vm

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// callNative calls a native function the way bytecode would, with Go
// values as arguments: int, float64, string, bool, nil, []string.
func callNative(t *testing.T, machine *VM, name string, args ...interface{}) (Value, error) {
	t.Helper()
	fn, ok := machine.externFuncs[name]
	if !ok {
		t.Fatalf("no native function %s", name)
	}

	values := make([]Value, len(args))
	for i, arg := range args {
		switch a := arg.(type) {
		case int:
			values[i] = NewInt(int64(a))
		case float64:
			values[i] = NewNumber(a)
		case bool:
			values[i] = NewBool(a)
		case nil:
			values[i] = NewNull()
		case string:
			v, err := machine.newString(a)
			if err != nil {
				t.Fatal(err)
			}
			values[i] = v
		case []string:
			v, err := machine.newStringArray(a)
			if err != nil {
				t.Fatal(err)
			}
			values[i] = v
		default:
			t.Fatalf("unsupported argument %T", arg)
		}
	}
	return fn(machine, values)
}

// expectNative checks the result of a native call, printed as Lumen
// prints values.
func expectNative(t *testing.T, want, name string, args ...interface{}) {
	t.Helper()
	got, err := callNative(t, NewVM(), name, args...)
	if err != nil {
		t.Errorf("%s%v: unexpected error: %v", name, args, err)
		return
	}
	if text := quoted(got); text != want {
		t.Errorf("%s%v: expected %s, got %s", name, args, want, text)
	}
}

// quoted prints a value as FormatValue does, with a text in quotes so that
// "" and " " can be told apart.
func quoted(v Value) string {
	if s, ok := stringValue(v); ok {
		return strconv.Quote(s)
	}
	return FormatValue(v)
}

// expectNativeError checks that a native call fails with a message.
func expectNativeError(t *testing.T, want, name string, args ...interface{}) {
	t.Helper()
	_, err := callNative(t, NewVM(), name, args...)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("%s%v: expected an error containing %q, got %v", name, args, want, err)
	}
}

func TestNativeRegistry(t *testing.T) {
	for _, name := range []string{"std.math.sqrt", "std.strings.split", "std.io.readLine", "std.time.now", "std.random.below"} {
		if !IsNative(name) {
			t.Errorf("expected %s to be a native function", name)
		}
	}
	for _, name := range []string{"sqrt", "std.math.nothing", "print", "__cast", ""} {
		if IsNative(name) {
			t.Errorf("did not expect %q to be a native function", name)
		}
	}

	names := NativeNames()
	if len(names) < 40 {
		t.Errorf("expected the natives of five modules, got %d", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("expected the names sorted and distinct, got %s before %s", names[i-1], names[i])
		}
	}
	// Every one of them is callable in a new VM
	machine := NewVM()
	for _, name := range names {
		if _, ok := machine.externFuncs[name]; !ok {
			t.Errorf("%s is not registered in a new VM", name)
		}
	}
}

func TestNativesCheckTheirArguments(t *testing.T) {
	// The type checker verifies calls from Lumen; bytecode written by hand
	// can pass anything
	expectNativeError(t, "expects 1 arguments, got 0", "std.math.sqrt")
	expectNativeError(t, "expects 1 arguments, got 2", "std.math.sqrt", 1.0, 2.0)
	expectNativeError(t, "argument 1 must be a Float, got Int", "std.math.sqrt", 4)
	expectNativeError(t, "argument 1 must be an Int, got Float", "std.math.toFloat", 1.5)
	expectNativeError(t, "argument 2 must be a Float, got String", "std.math.powf", 1.0, "x")
	expectNativeError(t, "argument 1 must be a String, got Int", "std.strings.upper", 1)
	expectNativeError(t, "argument 2 must be an Int, got String", "std.strings.substring", "abc", "0", 1)
	expectNativeError(t, "argument 3 must be an Int, got null", "std.strings.substring", "abc", 0, nil)
	expectNativeError(t, "argument 1 must be an array, got String", "std.strings.join", "abc", ",")
	expectNativeError(t, "argument 2 must be a String, got Bool", "std.strings.split", "abc", true)
	expectNativeError(t, "argument 1 must be a String, got Int", "std.io.readFile", 1)
	expectNativeError(t, "argument 1 must be an Int, got String", "std.random.below", "ten")

	// An array of something else than strings
	machine := NewVM()
	arr, _, _ := machine.heap.AllocArray(2)
	arr.Append(NewInt(1))
	sep, _ := machine.newString(",")
	_, err := machine.externFuncs["std.strings.join"](machine, []Value{NewHeapPtr(uintptr(unsafe.Pointer(arr))), sep})
	if err == nil || !strings.Contains(err.Error(), "must be an array of Strings, element 0 is Int") {
		t.Errorf("expected join to refuse an array of numbers, got %v", err)
	}
}

func TestNativeMath(t *testing.T) {
	expectNative(t, "3", "std.math.toFloat", 3)
	expectNative(t, "-7", "std.math.toFloat", -7)

	// Each way of losing the fraction, on both sides of zero and at a half
	rounding := []struct {
		x                         float64
		floor, ceil, round, trunc string
	}{
		{2.7, "2", "3", "3", "2"},
		{2.1, "2", "3", "2", "2"},
		{2.5, "2", "3", "3", "2"},
		{-2.7, "-3", "-2", "-3", "-2"},
		{-2.5, "-3", "-2", "-3", "-2"},
		{4.0, "4", "4", "4", "4"},
		{-0.2, "-1", "0", "0", "0"},
	}
	for _, r := range rounding {
		expectNative(t, r.floor, "std.math.floor", r.x)
		expectNative(t, r.ceil, "std.math.ceil", r.x)
		expectNative(t, r.round, "std.math.round", r.x)
		expectNative(t, r.trunc, "std.math.trunc", r.x)
	}
	// A Float too large for an Int, or not a number at all
	for _, name := range []string{"std.math.floor", "std.math.ceil", "std.math.round", "std.math.trunc"} {
		expectNativeError(t, "does not fit in an Int", name, 1e300)
		expectNativeError(t, "does not fit in an Int", name, -1e300)
		got, err := callNative(t, NewVM(), "std.math.powf", -8.0, 0.5) // NaN
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewVM().externFuncs[name](NewVM(), []Value{got}); err == nil {
			t.Errorf("%s: expected NaN to be refused", name)
		}
	}

	expectNative(t, "4", "std.math.sqrt", 16.0)
	expectNative(t, "0", "std.math.sqrt", 0.0)
	expectNativeError(t, "the square root of -1 is not a number", "std.math.sqrt", -1.0)
	expectNative(t, "0", "std.math.log", 1.0)
	expectNativeError(t, "the logarithm of 0 is not a number", "std.math.log", 0.0)
	expectNativeError(t, "the logarithm of -2 is not a number", "std.math.log", -2.0)
	expectNative(t, "1", "std.math.exp", 0.0)
	expectNative(t, "8", "std.math.powf", 2.0, 3.0)
	expectNative(t, "0.5", "std.math.powf", 2.0, -1.0)
	expectNative(t, "0", "std.math.sin", 0.0)
	expectNative(t, "1", "std.math.cos", 0.0)
	expectNative(t, "0", "std.math.tan", 0.0)
	expectNative(t, "0", "std.math.atan2", 0.0, 1.0)
}

func TestNativeStrings(t *testing.T) {
	// Positions are in characters, whatever the bytes
	expectNative(t, `"um"`, "std.strings.substring", "lumen", 1, 3)
	expectNative(t, `""`, "std.strings.substring", "lumen", 2, 2)
	expectNative(t, `"lumen"`, "std.strings.substring", "lumen", 0, 5)
	expectNative(t, `"éλ"`, "std.strings.substring", "héλlo", 1, 3)
	expectNativeError(t, "range 2..9 is outside a text of 3 characters", "std.strings.substring", "abc", 2, 9)
	expectNativeError(t, "range -1..2 is outside", "std.strings.substring", "abc", -1, 2)
	expectNativeError(t, "range 2..1 is outside", "std.strings.substring", "abc", 2, 1)

	expectNative(t, "2", "std.strings.indexOf", "banana", "na")
	expectNative(t, "4", "std.strings.lastIndexOf", "banana", "na")
	expectNative(t, "null", "std.strings.indexOf", "banana", "x")
	expectNative(t, "null", "std.strings.lastIndexOf", "banana", "x")
	expectNative(t, "0", "std.strings.indexOf", "banana", "")
	expectNative(t, "3", "std.strings.indexOf", "héλlo", "l") // 5 in bytes

	expectNative(t, `"ÉCOLE"`, "std.strings.upper", "école")
	expectNative(t, `"école"`, "std.strings.lower", "ÉCOLE")
	expectNative(t, `"a b"`, "std.strings.trim", " \t a b\r\n ")
	expectNative(t, `""`, "std.strings.trim", "   ")

	expectNative(t, `["a", "b", "", "c"]`, "std.strings.split", "a,b,,c", ",")
	expectNative(t, `["abc"]`, "std.strings.split", "abc", ",")
	expectNative(t, `["h", "é", "λ"]`, "std.strings.split", "héλ", "")
	expectNative(t, `[""]`, "std.strings.split", "", ",")
	expectNative(t, `["a", "b"]`, "std.strings.split", "a<>b", "<>")
	expectNative(t, `"a, b, c"`, "std.strings.join", []string{"a", "b", "c"}, ", ")
	expectNative(t, `"a"`, "std.strings.join", []string{"a"}, ", ")
	expectNative(t, `""`, "std.strings.join", []string{}, ", ")

	expectNative(t, `"a+b+c"`, "std.strings.replace", "a-b-c", "-", "+")
	expectNative(t, `"abc"`, "std.strings.replace", "abc", "x", "y")
	expectNative(t, `"ac"`, "std.strings.replace", "abc", "b", "")
	expectNativeError(t, "the text to replace is empty", "std.strings.replace", "abc", "", "y")
	expectNative(t, `"ababab"`, "std.strings.repeat", "ab", 3)
	expectNative(t, `""`, "std.strings.repeat", "ab", 0)
	expectNativeError(t, "cannot repeat a text -1 times", "std.strings.repeat", "ab", -1)
	expectNativeError(t, "out of memory", "std.strings.repeat", "abcdefgh", 1<<40)

	expectNative(t, "42", "std.strings.parseInt", "42")
	expectNative(t, "-7", "std.strings.parseInt", " -7 ")
	expectNative(t, "140737488355327", "std.strings.parseInt", "140737488355327")
	for _, bad := range []string{"", "4x", "x4", "1.5", "1 2", "140737488355328", "99999999999999999999"} {
		expectNative(t, "null", "std.strings.parseInt", bad)
	}
	expectNative(t, "2.5", "std.strings.parseFloat", "2.5")
	expectNative(t, "-3", "std.strings.parseFloat", "-3")
	expectNative(t, "1000", "std.strings.parseFloat", "1e3")
	expectNative(t, "null", "std.strings.parseFloat", "two")
	expectNative(t, "null", "std.strings.parseFloat", "")

	expectNative(t, "65", "std.strings.code", "A")
	expectNative(t, "955", "std.strings.code", "λx")
	expectNativeError(t, "an empty text has no first character", "std.strings.code", "")
	expectNative(t, `"A"`, "std.strings.fromCode", 65)
	expectNative(t, `"λ"`, "std.strings.fromCode", 955)
	expectNativeError(t, "-1 is not the code of a character", "std.strings.fromCode", -1)
	expectNativeError(t, "is not the code of a character", "std.strings.fromCode", 0xD800)
	expectNativeError(t, "is not the code of a character", "std.strings.fromCode", 0x110000)

	// A hash is stable, never negative, and tells texts apart
	hash := func(s string) int64 {
		v, err := callNative(t, NewVM(), "std.strings.hash", s)
		if err != nil || !v.IsInt() || v.AsInt() < 0 {
			t.Fatalf("hash(%q): expected a non-negative Int, got %s, %v", s, FormatValue(v), err)
		}
		return v.AsInt()
	}
	if hash("ada") != hash("ada") {
		t.Error("expected equal texts to have equal hashes")
	}
	seen := make(map[int64]string)
	for _, s := range []string{"", "a", "b", "ab", "ba", "ada", "Ada", "key1", "key2", "key10"} {
		h := hash(s)
		if other, dup := seen[h]; dup {
			t.Errorf("%q and %q have the same hash", s, other)
		}
		seen[h] = s
	}
}

func TestNativeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	missing := filepath.Join(dir, "no", "such", "file.txt")

	expectNative(t, "false", "std.io.exists", path)
	expectNative(t, "null", "std.io.readFile", path)
	expectNative(t, "true", "std.io.writeFile", path, "one\n")
	expectNative(t, "true", "std.io.exists", path)
	expectNative(t, `"one\n"`, "std.io.readFile", path)
	expectNative(t, "true", "std.io.appendFile", path, "two\n")
	expectNative(t, `"one\ntwo\n"`, "std.io.readFile", path)
	// Writing replaces, appending to a new file creates it
	expectNative(t, "true", "std.io.writeFile", path, "")
	expectNative(t, `""`, "std.io.readFile", path)
	other := filepath.Join(dir, "new.txt")
	expectNative(t, "true", "std.io.appendFile", other, "x")
	expectNative(t, `"x"`, "std.io.readFile", other)

	// What cannot be done is an answer, not an error
	expectNative(t, "false", "std.io.writeFile", missing, "x")
	expectNative(t, "false", "std.io.appendFile", missing, "x")
	expectNative(t, "null", "std.io.readFile", dir) // A folder
	expectNative(t, "true", "std.io.remove", path)
	expectNative(t, "false", "std.io.remove", path)
	expectNative(t, "false", "std.io.exists", path)

	t.Setenv("LUMEN_TEST_VARIABLE", "set")
	expectNative(t, `"set"`, "std.io.env", "LUMEN_TEST_VARIABLE")
	t.Setenv("LUMEN_TEST_VARIABLE", "")
	expectNative(t, `""`, "std.io.env", "LUMEN_TEST_VARIABLE")
	os.Unsetenv("LUMEN_TEST_VARIABLE")
	expectNative(t, "null", "std.io.env", "LUMEN_TEST_VARIABLE")
}

func TestNativeReadLine(t *testing.T) {
	read := func(input string) []string {
		machine := NewVM()
		machine.SetInput(strings.NewReader(input))
		var lines []string
		for i := 0; i < 10; i++ {
			v, err := callNative(t, machine, "std.io.readLine")
			if err != nil {
				t.Fatal(err)
			}
			lines = append(lines, quoted(v))
			if v.IsNull() {
				break
			}
		}
		return lines
	}

	tests := []struct{ input, want string }{
		{"", "null"},
		{"one\n", `"one" null`},
		{"one", `"one" null`}, // A last line without a line end counts
		{"one\ntwo\n", `"one" "two" null`},
		{"one\r\ntwo\r\n", `"one" "two" null`},
		{"\n\nx\n", `"" "" "x" null`}, // Empty lines are lines
		{"a b\tc\n", `"a b\tc" null`},
	}
	for _, tt := range tests {
		if got := strings.Join(read(tt.input), " "); got != tt.want {
			t.Errorf("input %q: expected %s, got %s", tt.input, tt.want, got)
		}
	}
}

func TestNativeTime(t *testing.T) {
	machine := NewVM()
	now, _ := callNative(t, machine, "std.time.now")
	if !now.IsInt() || now.AsInt() < 1_700_000_000_000 {
		t.Errorf("expected milliseconds since 1970, got %s", FormatValue(now))
	}

	before, _ := callNative(t, machine, "std.time.ticks")
	if _, err := callNative(t, machine, "std.time.sleep", 3); err != nil {
		t.Fatal(err)
	}
	after, _ := callNative(t, machine, "std.time.ticks")
	if elapsed := after.AsInt() - before.AsInt(); elapsed < 3000 {
		t.Errorf("expected at least 3000 microseconds to pass, got %d", elapsed)
	}
	// Not waiting is fine
	expectNative(t, "null", "std.time.sleep", 0)
	expectNative(t, "null", "std.time.sleep", -5)
}

func TestNativeRandom(t *testing.T) {
	sequence := func(machine *VM) []int64 {
		var out []int64
		for i := 0; i < 20; i++ {
			v, err := callNative(t, machine, "std.random.below", 1000)
			if err != nil || !v.IsInt() || v.AsInt() < 0 || v.AsInt() >= 1000 {
				t.Fatalf("expected a number below 1000, got %s, %v", FormatValue(v), err)
			}
			out = append(out, v.AsInt())
		}
		return out
	}
	same := func(a, b []int64) bool {
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	// The same seed gives the same numbers, in any VM
	a, b := NewVM(), NewVM()
	callNative(t, a, "std.random.seed", 42)
	callNative(t, b, "std.random.seed", 42)
	first := sequence(a)
	if !same(first, sequence(b)) {
		t.Error("expected two VMs with the same seed to agree")
	}
	// Seeding again starts over; another seed gives other numbers
	callNative(t, a, "std.random.seed", 42)
	if !same(first, sequence(a)) {
		t.Error("expected the same seed to repeat the numbers")
	}
	callNative(t, a, "std.random.seed", 43)
	if same(first, sequence(a)) {
		t.Error("expected another seed to give other numbers")
	}
	// One VM's seed does not touch another's
	callNative(t, a, "std.random.seed", 42)
	callNative(t, b, "std.random.seed", 42)
	sequence(a)
	if !same(first, sequence(b)) {
		t.Error("expected each VM to have its own generator")
	}

	expectNative(t, "0", "std.random.below", 1)
	expectNativeError(t, "the limit must be at least 1, got 0", "std.random.below", 0)
	expectNativeError(t, "the limit must be at least 1, got -3", "std.random.below", -3)

	machine := NewVM()
	for i := 0; i < 100; i++ {
		v, _ := callNative(t, machine, "std.random.float")
		if !v.IsNumber() || v.AsNumber() < 0 || v.AsNumber() >= 1 {
			t.Fatalf("expected a number in [0, 1), got %s", FormatValue(v))
		}
	}
}

// TestNativeResultsSurviveCollection makes a native run out of room half
// way through building its result, so that the heap is collected while it
// runs. What it has built so far is held only in Go variables; none of it
// may be freed.
func TestNativeResultsSurviveCollection(t *testing.T) {
	text := strings.Repeat("word ", 4000)

	// Splits the text `rounds` times, dropping every result but the last,
	// then joins the last one back together and returns it.
	program := func(rounds int) *Chunk {
		var code []Instr
		for i := 0; i < rounds; i++ {
			code = append(code,
				NewInstr(OpLOADNULL, 0, 0, 0), // The previous result is garbage now
				NewInstr(OpNEWEXFUNC, 3, 0, 0),
				NewInstr(OpNEWSTR, 4, 0, 0),
				NewInstr(OpNEWSTR, 5, 1, 0),
				NewInstr(OpCALL, 3, 2, 0),
			)
		}
		code = append(code,
			NewInstr(OpMOV, 6, 0, 0),
			NewInstr(OpNEWEXFUNC, 3, 1, 0),
			NewInstr(OpMOV, 4, 6, 0),
			NewInstr(OpNEWSTR, 5, 1, 0),
			NewInstr(OpCALL, 3, 2, 0),
			NewInstr(OpRET, 0, 0, 0),
		)
		chunk := chunkOf([]string{"std.strings.split", "std.strings.join"}, []Value{NewInt(0), NewInt(1)},
			fnDef{FunctionPrototype{Name: "main", RegisterCount: 8}, code})
		chunk.StringPool = []string{text, " "}
		return chunk
	}

	// How much one round takes
	measure := NewVM()
	if _, err := measure.Exec(program(1)); err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	oneRound := measure.heap.bytesAllocated

	// Room for one result and half of another: every later round has to
	// collect the round before it while it is building its own
	machine := NewVMWithHeapSize(oneRound * 3 / 2)
	// A freed object stays readable until Go reuses its memory, which
	// would hide a mistake here; spoil what is freed instead
	machine.heap.onFree = func(header *ObjHeader) {
		if header.Type == ObjTypeString {
			(*StringObj)(unsafe.Pointer(header)).data = "<freed>"
		}
	}
	result, err := machine.Exec(program(6))
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if got, ok := stringValue(result); !ok || got != text {
		t.Errorf("expected the text back after splitting and joining, got %d characters", len(got))
	}
	if machine.heap.Collections() < 3 {
		t.Errorf("expected the heap to be collected while the natives ran, got %d collections", machine.heap.Collections())
	}
	if len(machine.pinned) != 0 {
		t.Errorf("expected nothing to stay pinned after the calls, got %d values", len(machine.pinned))
	}
}

func TestNativeErrorsNameTheFunction(t *testing.T) {
	chunk := chunkOf([]string{"std.strings.substring"}, []Value{NewInt(0)},
		fnDef{FunctionPrototype{Name: "main", RegisterCount: 8}, []Instr{
			NewInstr(OpNEWEXFUNC, 3, 0, 0),
			NewInstr(OpNEWSTR, 4, 0, 0),
			NewInstr(OpLOADI, 5, 2, 0),
			NewInstr(OpLOADI, 6, 9, 0),
			NewInstr(OpCALL, 3, 3, 0),
			NewInstr(OpRET, 0, 0, 0),
		}})
	chunk.StringPool = []string{"abc"}

	_, err := NewVM().Exec(chunk)
	if err == nil {
		t.Fatal("expected an error")
	}
	// Named as the program names it, without the wording used for host functions
	if !strings.Contains(err.Error(), "std.strings.substring: range 2..9 is outside a text of 3 characters") {
		t.Errorf("unexpected message: %v", err)
	}
	if strings.Contains(err.Error(), "extern function") {
		t.Errorf("did not expect the message to mention extern functions: %v", err)
	}
}

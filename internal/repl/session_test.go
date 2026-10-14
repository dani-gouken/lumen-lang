package repl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/vm"
)

// talk is a session whose output and diagnostics are kept apart, for a
// test to read.
type talk struct {
	t   *testing.T
	s   *Session
	out bytes.Buffer
	err bytes.Buffer
}

func newTalk(t *testing.T) *talk {
	t.Helper()
	k := &talk{t: t}
	k.s = New(Options{Out: &k.out, Err: &k.err, In: strings.NewReader("")})
	return k
}

// say enters an input that has to go well, and returns what it wrote.
func (k *talk) say(input string) string {
	k.t.Helper()
	k.out.Reset()
	k.err.Reset()
	var outcome Outcome
	if IsCommand(input) {
		_, outcome = k.s.Command(input)
	} else {
		outcome = k.s.Eval(input)
	}
	if outcome.Failed || outcome.Exit != nil {
		k.t.Fatalf("input failed: %s\n%s", input, k.err.String())
	}
	return strings.TrimRight(k.out.String(), "\n")
}

// expect enters an input and compares what it wrote.
func (k *talk) expect(input, want string) {
	k.t.Helper()
	if got := k.say(input); got != want {
		k.t.Fatalf("%s\n  wrote: %q\n  want:  %q\n%s", input, got, want, k.err.String())
	}
}

// refuse enters an input that has to fail, and returns the diagnostics.
func (k *talk) refuse(input string, mentions ...string) string {
	k.t.Helper()
	k.out.Reset()
	k.err.Reset()
	var outcome Outcome
	if IsCommand(input) {
		_, outcome = k.s.Command(input)
	} else {
		outcome = k.s.Eval(input)
	}
	if !outcome.Failed {
		k.t.Fatalf("input should have failed: %s\nwrote: %s", input, k.out.String())
	}
	said := k.err.String()
	for _, part := range mentions {
		if !strings.Contains(said, part) {
			k.t.Fatalf("%s\n  diagnostics do not mention %q:\n%s", input, part, said)
		}
	}
	return said
}

func TestExpressionsAreShown(t *testing.T) {
	k := newTalk(t)
	k.expect("1 + 2", "3")
	k.expect("1 + 2;", "3")
	k.expect(`"a" + "b"`, `"ab"`)
	k.expect("(1 + 2) * 3", "9")
	k.expect("-(4)", "-4")
	k.expect("[1, 2, 3]", "[1, 2, 3]")
	k.expect(`(1, "x")`, `(1, "x")`)
	k.expect("1.5 * 2.0 // a comment ends the input", "3")
	k.expect("true && !false", "true")
	k.expect(`println("said")`, "said")
	k.expect("let quiet = 5", "")
	k.expect("quiet = 6", "")
	k.expect("quiet", "6")
	k.expect("print(1); print(2); quiet + 1", "127")
	k.expect("if quiet > 5 { println(\"big\"); }", "big")
	k.expect("null", "")
}

func TestVariablesLiveOn(t *testing.T) {
	k := newTalk(t)
	k.say("let count = 0")
	k.say("count += 5")
	k.say("count++")
	k.expect("count", "6")

	// Declared again, with another type
	k.say(`let count = "six"`)
	k.expect("count", `"six"`)

	// From its own earlier value
	k.say("let n = 2")
	k.say("let n = n * 10")
	k.expect("n", "20")

	// Several at once, and a tuple taken apart
	k.say("let a = 1; let b = a + 1; let (q, r) = (17 / 5, 17 % 5);")
	k.expect("[a, b, q, r]", "[1, 2, 3, 2]")

	// One declared in a block is the block's
	k.say("{ let inner = 1; }")
	k.refuse("inner", "E1001", "inner")
}

func TestClosuresShareVariablesAcrossInputs(t *testing.T) {
	k := newTalk(t)
	k.say("let count = 0")
	k.say("let bump = fn() -> Int { count += 1; return count; };")
	k.expect("bump(); bump()", "2")

	// The closure counted in the variable itself
	k.expect("count", "2")

	// And sees what is assigned here
	k.say("count = 100")
	k.expect("bump()", "101")
	k.expect("count", "101")

	// Two closures made in different inputs, one variable
	k.say("let reset = fn() { count = 0; };")
	k.say("reset()")
	k.expect("bump() + bump()", "3")

	// A closure made by a function keeps its own
	k.say("fn counter() -> fn() -> Int { let n = 0; return fn() -> Int { n += 1; return n; }; }")
	k.say("let first = counter(); let second = counter();")
	k.expect("[first(), first(), second()]", "[1, 2, 1]")
	k.expect("first()", "3")

	// Declared again, the variable is a new one: the closure keeps the old
	k.say("let count = 7")
	k.expect("bump()", "3")
	k.expect("count", "7")
}

func TestDeclarationsAndTheirReplacement(t *testing.T) {
	k := newTalk(t)
	k.say("fn twice(n: Int) -> Int { return n * 2; }")
	k.expect("twice(4)", "8")
	k.say("let old = twice")

	k.say("fn twice(n: Int) -> Int { return n * 20; }")
	k.expect("twice(4)", "80")
	// A value made of the old function is still the old function
	k.expect("old(4)", "8")

	// Functions that call each other, in any order
	k.say("fn even(n: Int) -> Bool { if n == 0 { return true; } return odd(n - 1); }\nfn odd(n: Int) -> Bool { if n == 0 { return false; } return even(n - 1); }")
	k.expect("[even(10), odd(10)]", "[true, false]")

	k.say("const LIMIT = 3;")
	k.say("const DOUBLE = LIMIT * 2;")
	k.expect("DOUBLE", "6")
	k.say("const LIMIT = 10;")
	k.expect("DOUBLE", "20")

	k.refuse("fn main() -> Int { return 0; }", "no main")
	k.refuse("fn broken() -> Int { return \"text\"; }", "E2")
	// What was refused is not there
	k.refuse("broken()", "E1001")

	if got := strings.Join(k.s.Declarations(), "|"); got != "fn twice(n: Int) -> Int|fn even(n: Int) -> Bool|fn odd(n: Int) -> Bool|const DOUBLE = LIMIT * 2|const LIMIT = 10" {
		t.Fatalf("declarations: %s", got)
	}
}

func TestTypesTraitsAndGenerics(t *testing.T) {
	k := newTalk(t)
	k.say(`import std.strings;
import std.arrays;
import std.list;
import std.result.{Result};

trait Shape { fn area() -> Float; fn name() -> String { return "shape"; } }
struct Circle { r: Float; }
struct Rect { w: Float; h: Float; }
impl Shape for Circle { fn area() -> Float { return 3.0 * self.r * self.r; } fn name() -> String { return "circle"; } }
impl Shape for Rect { fn area() -> Float { return self.w * self.h; } }

enum Token { Num(Int), Word(String), End }

struct Stack<T> { items: [T]; }
impl<T> Stack<T> {
    fn push(item: T) -> Void { self.items.push(item); }
    fn top() -> T? {
        if self.items.len() == 0 { return null; }
        return self.items[self.items.len() - 1];
    }
}

fn parse(word: String) -> Result<Token, String> {
    if word == "" { return Result.Err("nothing to read"); }
    let n = word.parseInt();
    if n != null { return Result.Ok(Token.Num(n)); }
    return Result.Ok(Token.Word(word));
}`)

	// A variable of each kind of type, used in a later input
	k.say("let shapes: [Shape] = [Circle { r: 1.0 }, Rect { w: 2.0, h: 3.0 }]")
	k.expect("shapes.map(s -> s.name())", `["circle", "shape"]`)
	k.expect("shapes.fold(0.0, (sum, s) -> sum + s.area())", "9")

	k.say("let stack = Stack<Int> { items: [] }")
	k.say("stack.push(4); stack.push(9);")
	k.expect("stack.top()", "9")
	k.say("let top = stack.top()")
	k.expect("if top != null { println(top + 1); }", "10")
	k.expect("top ?? 0", "9")

	k.say(`let tokens = ["12", "ab", ""].map(parse)`)
	k.expect(`match tokens[0] { case Result.Ok(Token.Num(n)): { println("number", n); } default: { println("other"); } }`, "number 12")
	k.expect("tokens[2]", `Err("nothing to read")`)

	k.say("let pair = (stack, Token.End)")
	k.say("pair.0.push(1)")
	k.expect("stack.top()", "1")

	k.say("let names = list.of([\"b\", \"a\"])")
	k.say("names.add(\"c\")")
	k.expect("names.size()", "3")

	k.say("let describe: fn(Shape) -> String = s -> \"${s.name()} of ${s.area()}\"")
	k.expect("describe(shapes[1])", `"shape of 6"`)

	want := []string{
		"shapes: [Shape]", "stack: Stack<Int>", "top: Int?", "tokens: [result.Result<Token, String>]",
		"pair: (Stack<Int>, Token)", "names: list.List<String>", "describe: fn(Shape) -> String",
	}
	listed := k.say(":vars")
	for _, line := range want {
		if !strings.Contains(listed, line) {
			t.Fatalf(":vars does not list %q:\n%s", line, listed)
		}
	}

	k.expect(":type tokens", "[result.Result<Token, String>]")
	k.expect(":type parse", "fn(String) -> result.Result<Token, String>")
	k.expect(":type stack.top() ?? 0", "Int")
	// Asking for a type runs nothing
	k.expect(":type stack.push(77)", "Void")
	k.expect("stack.top()", "1")
}

func TestRedeclaringATypeForgetsItsValues(t *testing.T) {
	k := newTalk(t)
	k.say("struct Point { x: Int; y: Int; }")
	k.say("impl Point { fn sum() -> Int { return self.x + self.y; } }")
	k.say("let p = Point { x: 1, y: 2 }; let points = [p]; let n = p.sum();")

	// The method still reads a field that would be gone: nothing changes
	said := k.refuse("struct Point { x: Int; }", "E2014", "[2]:1", "nothing was changed")
	if strings.Contains(said, "repl.lum") {
		t.Fatalf("a position is shown by the file no one wrote:\n%s", said)
	}
	k.expect("p.sum()", "3")

	// Both declared again in one input
	k.out.Reset()
	k.err.Reset()
	if outcome := k.s.Eval("struct Point { x: Int; z: Int = 9; }\nimpl Point { fn sum() -> Int { return self.x + self.z; } }"); outcome.Failed {
		t.Fatalf("redeclaring failed:\n%s", k.err.String())
	}
	notes := k.err.String()
	if !strings.Contains(notes, "'p' is forgotten") || !strings.Contains(notes, "'points' is forgotten") {
		t.Fatalf("the values of the old type are not said to be forgotten:\n%s", notes)
	}
	k.refuse("p", "E1001")
	k.expect("n", "3")
	k.expect("(Point { x: 1 }).sum()", "10")
}

func TestFailuresLeaveTheSessionStanding(t *testing.T) {
	k := newTalk(t)
	k.say("let xs = [1, 2, 3]")
	k.say("let total = 0")
	k.say("fn at(items: [Int], i: Int) -> Int {\n    return items[i];\n}")

	// What ran before the failure has happened; what the input declared is not there
	said := k.refuse("total = 5; let late = 1; at(xs, 9)", "R0002", "[3]:2:17", "in function 'at'", "called from [4]:1:26")
	if strings.Contains(said, "__repl") {
		t.Fatalf("the failure shows what the REPL wrote around the input:\n%s", said)
	}
	k.expect("total", "5")
	k.refuse("late", "E1001")

	// A value shown: columns are those of what was typed
	said = k.refuse("1 + xs[9]", "[6]:1:7", "1 | 1 + xs[9]")
	if strings.Contains(said, "__repl") {
		t.Fatalf("the failure shows what the REPL wrote around the input:\n%s", said)
	}

	// A closure that fails long after the input it was typed in says so
	k.say("let risky = fn(i: Int) -> Int { return xs[i]; };")
	k.expect("risky(1)", "2")
	k.refuse("risky(5)", "R0002", "[7]:1:42", "called from [9]:1:1")

	k.refuse(`panic("stop", total)`, "R0001", "stop 5")
	k.refuse("let y: Int = \"text\"", "E2")
	k.refuse("let = 3", "E0")
	k.refuse("1 / (total - 5)", "R0004")
	k.expect("xs", "[1, 2, 3]")

	// Recovering from one inside the program still works
	k.say("import std.result.{Result, attempt};")
	k.expect(`match attempt(fn() -> Int { return at(xs, 7); }) { case Result.Ok(n): { println(n); } case Result.Err(f): { println("survived"); } }`, "survived")
}

func TestWarningsAreGivenOnce(t *testing.T) {
	k := newTalk(t)
	k.out.Reset()
	k.err.Reset()
	if outcome := k.s.Eval("fn early() -> Int {\n    return 1;\n    println(\"never\");\n}"); outcome.Failed {
		t.Fatalf("a warning refused the input:\n%s", k.err.String())
	}
	if said := k.err.String(); !strings.Contains(said, "WARNING") || !strings.Contains(said, "[1]:3") {
		t.Fatalf("no warning where it was typed:\n%s", said)
	}
	k.expect("early()", "1")
	if said := k.err.String(); said != "" {
		t.Fatalf("the warning is given again:\n%s", said)
	}
	k.say("fn other() -> Int { return early(); }")
	if said := k.err.String(); said != "" {
		t.Fatalf("the warning is given again:\n%s", said)
	}
}

// What a session's programs are given is not there for any other program.
func TestSessionFunctionsAreTheSessionsOnly(t *testing.T) {
	errs := errors.NewHandler()
	src := "extern fn __repl_show(value: Any);\nfn main() -> Int { __repl_show(1); return 0; }\n"
	compiler.Compile(src, "main.lum", errs)
	if !errs.HasErrors() || errs.Diagnostics[0].Code != compiler.ErrUnknownExtern {
		t.Fatalf("an ordinary program declared a function of the REPL: %v", errs.Diagnostics)
	}
	for name, want := range map[string]bool{
		"__repl_keep": true, "__repl_link": true, "__repl_show": true, "__repl_get_count": true,
		"__repl_get_": false, "__repl_other": false, "println": false,
	} {
		if got := vm.IsSessionFunction(name); got != want {
			t.Errorf("IsSessionFunction(%q) = %v", name, got)
		}
	}
}

func TestExitEndsTheSession(t *testing.T) {
	k := newTalk(t)
	k.say("import std.os;")
	outcome := k.s.Eval("os.exit(7)")
	if outcome.Exit == nil || *outcome.Exit != 7 {
		t.Fatalf("exit was not passed on: %+v", outcome)
	}
}

func TestTasksAndChannelsAcrossInputs(t *testing.T) {
	k := newTalk(t)
	k.say("import std.task;")
	k.say("let numbers = task.channel<Int>(8)")
	k.say("let seen: [Int] = []")
	k.say("task.run(fn() { for i in 0..5 { numbers.send(i * i); } numbers.close(); });")
	k.say("numbers.each(fn(n: Int) { seen.push(n); });")
	k.expect("seen", "[0, 1, 4, 9, 16]")
}

func TestVariablesSurviveGarbageCollection(t *testing.T) {
	var out, errs bytes.Buffer
	s := New(Options{Out: &out, Err: &errs, Machine: func() *vm.VM { return vm.NewVMWithHeapSize(2 << 20) }})
	run := func(input string) {
		t.Helper()
		if outcome := s.Eval(input); outcome.Failed {
			t.Fatalf("%s failed:\n%s", input, errs.String())
		}
	}
	run("import std.strings;")
	run(`let kept = ["first"]`)
	run("let bump = fn() -> Int { kept.push(\"more\"); return 0; };")
	// Far more than the heap holds, all of it garbage but the variables
	for i := 0; i < 20; i++ {
		run(`for i in 0..2000 { let waste = "x".repeat(500); } bump();`)
	}
	out.Reset()
	run(`kept[0] + toString(bump())`)
	if got := strings.TrimSpace(out.String()); got != `"first0"` {
		t.Fatalf("the variable did not survive: %s\n%s", got, errs.String())
	}
}

func TestForgetLoadAndReset(t *testing.T) {
	k := newTalk(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "shapes.lum")
	source := `import std.math;

struct Square { side: Int; }

impl Square {
    fn area() -> Int { return self.side * self.side; }
}

fn main() -> Int {
    println(biggest(3, 4));
    return 0;
}

fn biggest(a: Int, b: Int) -> Int { return a.max(b); }
`
	if err := os.WriteFile(file, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	k.out.Reset()
	k.err.Reset()
	if _, outcome := k.s.Command(":load " + file); outcome.Failed {
		t.Fatalf("load failed:\n%s", k.err.String())
	}
	if !strings.Contains(k.err.String(), "main of shapes.lum is left out") {
		t.Fatalf("nothing said about main:\n%s", k.err.String())
	}
	k.expect("biggest((Square { side: 3 }).area(), 4)", "9")
	// A failure in the file says the line of the file's text
	k.say("fn fail() -> Int { let xs: [Int] = []; return xs[0]; }")

	k.say("let s = Square { side: 2 }")
	k.say(":forget s")
	k.refuse("s", "E1001")
	k.refuse(":forget nothing", "nothing is called")
	k.say(":forget fail")
	k.refuse("fail()", "E1001")
	// Still used: it stays
	k.say("fn user() -> Int { return biggest(1, 2); }")
	k.refuse(":forget biggest", "still used")
	k.expect("user()", "2")

	// A type goes with its methods and its values
	k.say("let square = Square { side: 5 }")
	k.say(":forget Square")
	k.refuse("square", "E1001")
	k.refuse("Square { side: 5 }", "Square")
	if decls := strings.Join(k.s.Declarations(), "|"); strings.Contains(decls, "Square") {
		t.Fatalf("declarations after forgetting the type: %s", decls)
	}

	k.refuse(":load "+filepath.Join(dir, "missing.lum"), "cannot read")
	k.refuse(":nonsense", "no command")
	k.refuse(":type", "Usage")

	k.say(":reset")
	k.refuse("biggest(1, 2)", "E1001")
	if k.s.Number() != 1 {
		t.Fatalf("after a reset the next input is number %d", k.s.Number())
	}
	if help := k.say(":help"); !strings.Contains(help, ":vars") || !strings.Contains(help, ":load") {
		t.Fatalf("help does not list the commands:\n%s", help)
	}
}

func TestImportsFromTheFolder(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "util"), 0755)
	os.WriteFile(filepath.Join(dir, "util", "text.lum"), []byte(`pub struct Label { text: String; }
pub fn label(text: String) -> Label { return Label { text: text + "!" }; }
`), 0644)
	var out, errs bytes.Buffer
	s := New(Options{Dir: dir, Out: &out, Err: &errs})
	for _, input := range []string{"import util.text;", `let made = text.label("open")`, "made.text"} {
		if outcome := s.Eval(input); outcome.Failed {
			t.Fatalf("%s failed:\n%s", input, errs.String())
		}
	}
	if got := strings.TrimSpace(out.String()); got != `"open!"` {
		t.Fatalf("wrote %s", got)
	}
	// The type of the variable is one the session never named
	if vars := s.Variables(); len(vars) != 1 || vars[0].Type != "text.Label" {
		t.Fatalf("variables: %+v", vars)
	}
	errs.Reset()
	if outcome := s.Eval("import util.missing;"); !outcome.Failed || !strings.Contains(errs.String(), "[4]:1") {
		t.Fatalf("a module that is not there:\n%s", errs.String())
	}
}

func TestIncomplete(t *testing.T) {
	for text, want := range map[string]bool{
		"1 + 2":                            false,
		"fn f() {":                         true,
		"fn f() {\n    return 1;\n}":       false,
		"let xs = [1,":                     true,
		"foo(1, 2":                         true,
		"let s = \"a { b\"":                false,
		"let s = \"\"\"\n  text":           true,
		"let s = \"\"\"\n  text\n  \"\"\"": false,
		"/* a comment":                     true,
		"}":                                false,
		"println(\"${x}\")":                false,
	} {
		if got := Incomplete(text); got != want {
			t.Errorf("Incomplete(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestComplete(t *testing.T) {
	k := newTalk(t)
	k.say("import std.strings;\nimport std.math as m;")
	k.say("struct Point { x: Int; y: Int; }\nimpl Point { fn sum() -> Int { return self.x + self.y; } }\nenum Colour { Red, Green }")
	k.say("let point = Point { x: 1, y: 2 }; let pointer: Point? = null;")

	check := func(before string, replaced int, want ...string) {
		t.Helper()
		got, n := k.s.Complete(before)
		for _, name := range want {
			if !contains(got, name) {
				t.Fatalf("Complete(%q) lacks %q: %v", before, name, got)
			}
		}
		if n != replaced {
			t.Fatalf("Complete(%q) replaces %d characters, want %d", before, n, replaced)
		}
	}
	check("poi", 3, "point", "pointer")
	check("let z = pri", 3, "print", "println")
	check("strings.fr", 2, "fromCode")
	check("1 + m.p", 1, "pi")
	check("point.", 0, "x", "y", "sum")
	check("pointer.s", 1, "sum")
	check("Colour.", 0, "Red", "Green")
	check("import std.st", 2, "strings")
	check("import ", 0, "std")
	check(":t", 2, ":type")
	check("Po", 2, "Point")
	check("st", 2, "strings", "struct")

	if got, _ := k.s.Complete("strings.zzz"); len(got) != 0 {
		t.Fatalf("candidates for nothing: %v", got)
	}
}

package compiler

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/vm"
)

// runProgram compiles and executes source, returning what it printed, the
// value main returned and the runtime error, if any.
func runProgram(t *testing.T, src string) (string, vm.Value, error) {
	t.Helper()

	errs := errors.NewHandler()
	result := Compile(src, "test.lum", errs)
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic:\n%s", d.String())
	}
	if t.Failed() {
		t.FailNow()
	}
	if result.Chunk == nil {
		t.Fatal("compilation produced no bytecode")
	}

	var out bytes.Buffer
	machine := vm.NewVM()
	machine.SetOutput(&out)
	value, err := machine.Exec(result.Chunk)
	return out.String(), value, err
}

// expectOutput runs the body of a main function and compares its output.
// decls holds any declarations the body needs.
func expectOutput(t *testing.T, decls, body, want string) {
	t.Helper()

	src := decls + "\nfn main() -> Int {\n" + body + "\nreturn 0;\n}\n"
	out, _, err := runProgram(t, src)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if out != want {
		t.Errorf("unexpected output\n--- want ---\n%s--- got ---\n%s", want, out)
	}
}

func TestMainReturnValue(t *testing.T) {
	_, value, err := runProgram(t, "fn main() -> Int { return 6 * 7; }")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if !value.IsInt() || value.AsInt() != 42 {
		t.Errorf("expected main to return 42, got %s", vm.FormatValue(value))
	}
}

func TestArithmeticAndComparison(t *testing.T) {
	expectOutput(t, "", `
		println(1 + 2 * 3, (1 + 2) * 3, 7 / 2, 7 % 3, -4 + 1);
		println(1.5 * 2.0, 10.0 / 4.0, -2.5);
		println(1 < 2, 2 <= 2, 3 > 4, 4 >= 5, 1 == 1, 1 != 1);
		println(!true, !(1 > 2));
		println(100000 * 3, 0 - 1000000);
	`, "7 9 3 1 -3\n3 2.5 -2.5\ntrue true false false true false\nfalse true\n300000 -1000000\n")
}

func TestStrings(t *testing.T) {
	expectOutput(t, "", `
		let s = "foo" + "bar";
		println(s, s.length, s[0], s[5]);
		println(s == "foobar", s != "foobar", "a" < "b", "b" <= "a");
		println("tab\there", "quote\"d", toString(42) + toString(true) + toString(null));
		for c in "héy" { print(c, ""); }
		println("");
	`, "foobar 6 f r\ntrue false true false\ntab\there quote\"d 42truenull\nh é y \n")
}

func TestVariablesAndScopes(t *testing.T) {
	expectOutput(t, "", `
		let x = 1;
		let y = x;
		y = 5;
		println(x, y);
		{
			let x = 10;
			x = x + 1;
			println(x);
		}
		println(x);
		const limit: Int = 3;
		println(limit);
	`, "1 5\n11\n1\n3\n")
}

func TestControlFlow(t *testing.T) {
	expectOutput(t, "", `
		let i = 0;
		let evens = 0;
		while true {
			i = i + 1;
			if i > 10 { break; }
			if i % 2 == 1 { continue; }
			evens = evens + i;
		}
		println(evens);

		let n = 5;
		if n < 3 { println("small"); } else if n < 10 { println("medium"); } else { println("large"); }

		let total = 0;
		for v in [1, 2, 3, 4, 5] {
			if v == 2 { continue; }
			if v == 5 { break; }
			total = total + v;
		}
		println(total);

		for row in [[1, 2], [3]] {
			for cell in row { print(cell, ""); }
		}
		println("");
	`, "30\nmedium\n8\n1 2 3 \n")
}

func TestShortCircuit(t *testing.T) {
	decls := `
		fn loud(label: String, value: Bool) -> Bool {
			print(label, "");
			return value;
		}
	`
	expectOutput(t, decls, `
		println(loud("a", false) && loud("b", true));
		println(loud("c", true) || loud("d", false));
		println(loud("e", true) && loud("f", false) || loud("g", true));
		let missing = null;
		println(missing ?? "fallback", "kept" ?? "unused");

		let i = 0;
		while i < 3 && loud("w", true) { i = i + 1; }
		println(i);

		// A variable initialized with && or || stays usable afterwards
		let both = loud("x", true) && loud("y", false);
		let either = both || loud("z", true);
		println(both, either);
		if both || (either && loud("n", true)) { println("nested"); }
	`, "a false\nc true\ne f g true\nfallback kept\nw w w 3\nx y z false true\nn nested\n")
}

func TestFunctionsAndRecursion(t *testing.T) {
	decls := `
		fn fact(n: Int) -> Int {
			if n <= 1 { return 1; }
			return n * fact(n - 1);
		}
		fn isEven(n: Int) -> Bool { if n == 0 { return true; } return isOdd(n - 1); }
		fn isOdd(n: Int) -> Bool { if n == 0 { return false; } return isEven(n - 1); }
		fn greet(name: String) -> Void { println("hi", name); }
		fn twice(f: fn(Int) -> Int, v: Int) -> Int { return f(f(v)); }
		fn addOne(v: Int) -> Int { return v + 1; }
	`
	expectOutput(t, decls, `
		println(fact(10), isEven(10), isOdd(7));
		greet("lumen");
		println(twice(addOne, 5));
		let f = fact;
		println(f(5));
	`, "3628800 true true\nhi lumen\n7\n120\n")
}

func TestArrays(t *testing.T) {
	expectOutput(t, "", `
		let xs = [10, 20, 30];
		xs[1] = 25;
		push(xs, 40);
		println(xs, xs.length, xs[3]);
		let empty = [];
		println(empty, empty.length);
		let alias = xs;
		alias[0] = 0;
		println(xs[0]);
		println([[1], [2, 3]], ["a", "b"]);
	`, "[10, 25, 30, 40] 4 40\n[] 0\n0\n[[1], [2, 3]] [\"a\", \"b\"]\n")
}

func TestStructsAndMethods(t *testing.T) {
	decls := `
		struct Point { x: Int; y: Int; }
		struct Line { from: Point; to: Point; }

		trait Shape { fn area() -> Int; }

		impl Shape for Point {
			fn area() -> Int { return self.x * self.y; }
		}
		impl Point {
			fn moved(dx: Int, dy: Int) -> Point { return Point { x: self.x + dx, y: self.y + dy }; }
			fn grow() -> Void { self.x = self.x + 1; }
		}

		fn describe(s: Shape) -> Int { return s.area(); }
	`
	expectOutput(t, decls, `
		let p = Point { x: 2, y: 3 };
		p.y = 5;
		println(p, p.x, p.area());
		p.grow();
		let q = p.moved(1, 1);
		println(p.x, q, describe(q), describe(p));
		let line = Line { from: p, to: q };
		line.to.x = 99;
		println(line.to.x, q.x);
	`, "{x: 2, y: 5} 2 10\n3 {x: 4, y: 6} 24 15\n99 99\n")
}

func TestClosures(t *testing.T) {
	decls := `
		fn makeCounter() -> fn() -> Int {
			let count = 0;
			return fn() -> Int {
				count = count + 1;
				return count;
			};
		}
		fn makeAdder(n: Int) -> fn(Int) -> Int {
			return fn(x: Int) -> Int { return x + n; };
		}
		fn nested(a: Int) -> fn(Int) -> fn(Int) -> Int {
			let b = 10;
			return fn(c: Int) -> fn(Int) -> Int {
				return fn(d: Int) -> Int { return a + b + c + d; };
			};
		}
	`
	expectOutput(t, decls, `
		let first = makeCounter();
		let second = makeCounter();
		first();
		first();
		println(first(), second());
		println(makeAdder(5)(10), nested(1)(2)(3));

		// A closure sees later writes to the variable it captured
		let total = 0;
		let add = fn(v: Int) -> Void { total = total + v; };
		add(4);
		total = total + 1;
		add(5);
		println(total);
	`, "3 1\n15 16\n10\n")
}

func TestGenericsAndVariadics(t *testing.T) {
	decls := `
		fn identity<T>(v: T) -> T { return v; }
		fn first<T>(xs: [T]) -> T { return xs[0]; }
		fn collect<T>(items: ...T) -> [T] { return items; }
		struct Box<T> { value: T; }
		fn unwrap<T>(box: Box<T>) -> T { return box.value; }
		fn sum(label: String, nums: ...Int) -> Int {
			let total = 0;
			for n in nums { total = total + n; }
			println(label, total, nums.length);
			return total;
		}
	`
	expectOutput(t, decls, `
		println(identity<Int>(42), identity<String>("id"), first<Int>([7, 8]));
		println(collect<Int>(1, 2, 3), collect<String>());
		sum("three:", 1, 2, 3);
		sum("none:");
		let box = Box<String> { value: "boxed" };
		println(box, unwrap<String>(box));
	`, "42 id 7\n[1, 2, 3] []\nthree: 6 3\nnone: 0 0\n{value: \"boxed\"} boxed\n")
}

func TestMatch(t *testing.T) {
	decls := `
		fn name(n: Int) -> String {
			match n {
				case 0: { return "zero"; }
				case -1: { return "minus one"; }
				default: { return "other"; }
			}
			return "unreachable";
		}
	`
	expectOutput(t, decls, `
		println(name(0), name(-1), name(7));
		match "b" {
			case "a": { println("is a"); }
			case "b": { println("is b"); }
		}
	`, "zero minus one other\nis b\n")
}

func TestGarbageCollectionKeepsLiveValues(t *testing.T) {
	// Allocates far more than the heap holds, so the collector must run and
	// must not free anything still reachable.
	decls := `
		struct Node { value: Int; next: Node?; }
	`
	expectOutput(t, decls, `
		let head = Node { value: 0, next: null };
		let i = 1;
		while i < 2000 {
			head = Node { value: i, next: head };
			let scratch = "temporary " + toString(i);
			i = i + 1;
		}
		let total = 0;
		let node = head;
		let steps = 0;
		while steps < 2000 {
			total = total + node.value;
			node = node.next ?? node;
			steps = steps + 1;
		}
		println(total);
	`, "1999000\n")
}

func TestRuntimeErrorsPointAtSource(t *testing.T) {
	tests := []struct {
		name string
		body string
		msg  string
		line int
	}{
		{"index out of bounds", "let xs = [1];\nreturn xs[3];", "index out of bounds: 3 (length 1)", 4},
		{"division by zero", "let zero = 0;\nreturn 1 / zero;", "division by zero", 4},
		{"call of null", "let p = Empty {};\nreturn p.missing(1);", "CALL requires function object", 4},
		{"field of non-struct", "let n = 5;\nreturn n.field;", "GETFIELD requires object, got Int", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "struct Empty {}\nfn main() -> Int {\n" + tt.body + "\n}\n"
			_, _, err := runProgram(t, src)
			if err == nil {
				t.Fatal("expected a runtime error")
			}
			if !strings.Contains(err.Error(), tt.msg) {
				t.Errorf("expected error containing %q, got:\n%v", tt.msg, err)
			}
			if want := "test.lum:" + string(rune('0'+tt.line)) + ":"; !strings.Contains(err.Error(), want) {
				t.Errorf("expected error to point at %s, got:\n%v", want, err)
			}
			if !strings.Contains(err.Error(), "in function 'main'") {
				t.Errorf("expected error to name the function, got:\n%v", err)
			}
		})
	}
}

func TestCompileErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
		msg  string
	}{
		{"syntax error", "fn main() -> Int { return 1 }", "E0003", "expected ';'"},
		{"undefined variable", "fn main() -> Int { return missing; }", "E1001", "undefined identifier 'missing'"},
		{"redeclaration", "fn main() -> Int { let a = 1; let a = 2; return a; }", "E1002", "already declared"},
		{"type mismatch", `fn main() -> Int { let a: Int = "text"; return a; }`, "E2001", "cannot assign type string to int"},
		{"bad operands", `fn main() -> Int { return 1 + "a"; }`, "E2006", "operator + not defined"},
		{"wrong return type", `fn main() -> Int { return "a"; }`, "E2003", "cannot return type string"},
		{"wrong argument count", "fn f(a: Int) -> Int { return a; }\nfn main() -> Int { return f(1, 2); }", "E2009", "expected 1 arguments, got 2"},
		{"non-bool condition", "fn main() -> Int { if 1 { return 1; } return 0; }", "E2005", "condition must be bool"},
		{"bad assignment", "fn main() -> Int { let a = 1; a = true; return a; }", "E2001", "cannot assign type bool to int"},
		{"mixed array", `fn main() -> Int { let a = [1, "two"]; return 0; }`, "E2001", "array element has type string, expected int"},
		{"unknown struct field", "struct P { x: Int; }\nfn main() -> Int { let p = P { z: 1 }; return 0; }", "E2014", "no field 'z'"},
		{"assign to function", "fn f() -> Int { return 1; }\nfn main() -> Int { f = f; return 0; }", "E5003", "cannot assign to 'f'"},
		{"break outside loop", "fn main() -> Int { break; return 0; }", "E5005", "'break' outside of a loop"},
		{"missing main", "fn helper() -> Int { return 1; }", "E6006", "no 'main' function"},
		{"main with parameters", "fn main(a: Int) -> Int { return a; }", "E6006", "must not take parameters"},
		{"imports", `import "std/io"; fn main() -> Int { return 0; }`, "E7001", "imports are not supported yet"},
		{"uninferred generic", "fn id<T>(v: T) -> T { return v; }\nfn main() -> Int { return id(1); }", "E3001", "Cannot infer type arguments"},
		{"enums", "enum Color { Red, Green }\nfn main() -> Int { return 0; }", "E7001", "enums are not supported yet"},
		{"type pattern", "fn main() -> Int { match 1 { case Int: { return 1; } } return 0; }", "E7001", "type patterns in match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := errors.NewHandler()
			result := Compile(tt.src, "test.lum", errs)

			if result.Chunk != nil {
				t.Error("expected no bytecode for a program with errors")
			}
			if len(errs.Diagnostics) == 0 {
				t.Fatal("expected a diagnostic, got none")
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

func TestCompileStopsAtFirstFailingStage(t *testing.T) {
	// A program that cannot be resolved must not reach type checking, which
	// would report the same mistake a second time.
	errs := errors.NewHandler()
	result := Compile("fn main() -> Int { return a + b; }", "test.lum", errs)

	if len(errs.Diagnostics) != 2 {
		t.Errorf("expected exactly the 2 resolution errors, got %d", len(errs.Diagnostics))
	}
	if result.AST == nil {
		t.Error("expected the AST of the parsed program")
	}
	if result.IR != nil || result.Chunk != nil {
		t.Error("expected later stages not to run")
	}
}

func TestCompileExposesStageResults(t *testing.T) {
	errs := errors.NewHandler()
	result := Compile("fn main() -> Int { for v in [1] { print(v); } return 0; }", "test.lum", errs)
	if errs.HasErrors() {
		t.Fatalf("unexpected errors: %v", errs.Diagnostics)
	}

	if !strings.Contains(result.AST.String(), "for v in") {
		t.Error("expected the AST to keep the for loop as written")
	}
	if lowered := result.Lowered.String(); strings.Contains(lowered, "for v in") || !strings.Contains(lowered, "while") {
		t.Errorf("expected the lowered program to use a while loop, got:\n%s", lowered)
	}
	if !strings.Contains(result.IR.Dump(), "fn main()") {
		t.Error("expected IR for main")
	}
	if result.Chunk.FindPrototype("main") < 0 {
		t.Error("expected bytecode for main")
	}
}

func TestCompileFileMissing(t *testing.T) {
	if _, err := CompileFile("does/not/exist.lum", errors.NewHandler()); err == nil {
		t.Error("expected an error for a missing file")
	}
}

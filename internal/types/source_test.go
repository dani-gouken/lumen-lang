package types

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/parser"
	"github.com/dani-gouken/lumen/internal/resolve"
)

// checkSource type checks source text and returns the diagnostics' messages
// together with the type information.
func checkSource(t *testing.T, src string) ([]string, *Info, *ast.Program) {
	t.Helper()

	errs := errors.NewHandler()
	prog := parser.Parse(src, "test.lum", errs)
	table := resolve.NewResolver(errs).Resolve(prog)
	if errs.HasErrors() {
		t.Fatalf("test source is invalid: %s", errs.Diagnostics[0].Message)
	}

	info := NewChecker(prog, table, errs).Check()
	var msgs []string
	for _, d := range errs.Diagnostics {
		msgs = append(msgs, d.Message)
	}
	return msgs, info, prog
}

// letType returns the type inferred for the initializer of `let name` in main.
func letType(t *testing.T, info *Info, prog *ast.Program, name string) string {
	t.Helper()
	for _, decl := range prog.Decls {
		fn, ok := decl.(*ast.FnDecl)
		if !ok || fn.Name != "main" {
			continue
		}
		for _, stmt := range fn.Body.Stmts {
			if let, ok := stmt.(*ast.LetStmt); ok && let.Name == name {
				if typ, ok := info.Types[let.Value]; ok {
					return typ.String()
				}
				return "<untyped>"
			}
		}
	}
	t.Fatalf("no `let %s` in main", name)
	return ""
}

func TestCheckInfersExpressionTypes(t *testing.T) {
	msgs, info, prog := checkSource(t, `
		struct Point { x: Int; label: String; }
		fn id<T>(v: T) -> T { return v; }
		fn total(nums: ...Int) -> Int { return nums.length; }

		fn main() -> Int {
			let p = Point { x: 1, label: "a" };
			let field = p.x;
			let text = p.label;
			let xs = [1.5, 2.5];
			let elem = xs[0];
			let count = xs.length;
			let char = text[0];
			let size = text.length;
			let lambda = fn(a: Int) -> Bool { return a > 0; };
			let called = lambda(1);
			let generic = id<Int>(1);
			let variadic = total(1, 2, 3);
			let cmp = text < "b";
			return 0;
		}
	`)
	if len(msgs) != 0 {
		t.Fatalf("expected no errors, got %v", msgs)
	}

	want := map[string]string{
		"field":    "int",
		"text":     "string",
		"elem":     "float",
		"count":    "int",
		"char":     "string",
		"size":     "int",
		"lambda":   "fn(int) -> bool",
		"called":   "bool",
		"generic":  "any", // Type parameters are bound by monomorphization
		"variadic": "int",
		"cmp":      "bool",
	}
	for name, typ := range want {
		if got := letType(t, info, prog, name); got != typ {
			t.Errorf("let %s: expected %s, got %s", name, typ, got)
		}
	}
}

func TestCheckAcceptsValidPrograms(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"assignment", `fn main() -> Int { let a = 1; a = a + 1; return a; }`},
		{"empty array fits any array", `fn f() -> [Int] { let out = []; return out; }`},
		{"optional accepts null", `fn main() -> Int { let a: Int? = null; return 0; }`},
		{"loop variable", `fn main() -> Int { let t = 0; for v in [1, 2] { t = t + v; } return t; }`},
		{"string loop variable", `fn main() -> Int { for c in "ab" { let s: String = c; } return 0; }`},
		{"variadic with no extras", `fn f(a: Int, rest: ...Int) -> Int { return a; } fn main() -> Int { return f(1); }`},
		{"lambda as argument", `fn ap(f: fn(Int) -> Int) -> Int { return f(1); }
			fn main() -> Int { return ap(fn(v: Int) -> Int { return v; }); }`},
		{"struct passed as trait", `struct P { x: Int; } trait T { fn m() -> Int; }
			impl T for P { fn m() -> Int { return self.x; } }
			fn use(t: T) -> Int { return t.m(); }
			fn main() -> Int { return use(P { x: 1 }); }`},
		{"self in method", `struct P { x: Int; } impl P { fn twice() -> Int { return self.x + self.x; } }`},
		{"null coalescing", `fn main() -> Int { let a: Int? = null; let b: Int = a ?? 3; return b; }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if msgs, _, _ := checkSource(t, tt.src); len(msgs) != 0 {
				t.Errorf("expected no errors, got %v", msgs)
			}
		})
	}
}

func TestCheckRejectsInvalidPrograms(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"assign wrong type", `fn main() -> Int { let a = 1; a = "s"; return a; }`, "cannot assign type string to int"},
		{"assign to field", `struct P { x: Int; } fn main() -> Int { let p = P { x: 1 }; p.x = true; return 0; }`, "cannot assign type bool to int"},
		{"assign to element", `fn main() -> Int { let xs = [1]; xs[0] = "s"; return 0; }`, "cannot assign type string to int"},
		{"field has wrong type", `struct P { x: Int; } fn main() -> Int { let p = P { x: "s" }; return 0; }`, "cannot use type string for field 'x'"},
		{"unknown field in literal", `struct P { x: Int; } fn main() -> Int { let p = P { y: 1 }; return 0; }`, "struct P has no field 'y'"},
		{"non-int index", `fn main() -> Int { let xs = [1]; return xs["a"]; }`, "index must be int, got string"},
		{"field type flows on", `struct P { x: Int; } fn main() -> Int { let p = P { x: 1 }; let s: String = p.x; return 0; }`, "cannot assign type int to string"},
		{"lambda return", `fn main() -> Int { let f = fn() -> Int { return "s"; }; return 0; }`, "cannot return type string"},
		{"lambda argument", `fn main() -> Int { let f = fn(a: Int) -> Int { return a; }; return f("s"); }`, "cannot use type string as argument of type int"},
		{"variadic element", `fn f(rest: ...Int) -> Int { return 0; } fn main() -> Int { return f(1, "s"); }`, "cannot use type string as argument of type int"},
		{"array of wrong element", `fn f() -> [Int] { return ["s"]; }`, "cannot return type [string]"},
		{"mixed arithmetic", `fn main() -> Int { return 1 + 2.5; }`, "operator + not defined for types int and float"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, _, _ := checkSource(t, tt.src)
			if len(msgs) == 0 {
				t.Fatal("expected an error, got none")
			}
			if !strings.Contains(msgs[0], tt.want) {
				t.Errorf("expected error containing %q, got %q", tt.want, msgs[0])
			}
		})
	}
}

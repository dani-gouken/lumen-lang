package resolve

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/parser"
)

// resolveSource parses and resolves source text, returning the messages of
// the diagnostics it produced.
func resolveSource(t *testing.T, src string) []string {
	t.Helper()

	errs := errors.NewHandler()
	prog := parser.Parse(src, "test.lum", errs)
	if errs.HasErrors() {
		t.Fatalf("test source does not parse: %s", errs.Diagnostics[0].Message)
	}

	NewResolver(errs).Resolve(prog)
	var msgs []string
	for _, d := range errs.Diagnostics {
		msgs = append(msgs, d.Message)
	}
	return msgs
}

func TestResolveBuiltinsAreInScope(t *testing.T) {
	msgs := resolveSource(t, `
		fn main() -> Int {
			let xs: [Int] = [];
			push(xs, 1);
			print("a");
			println(toString(xs));
			return 0;
		}
	`)
	if len(msgs) != 0 {
		t.Errorf("expected builtins to resolve, got %v", msgs)
	}

	for _, name := range []string{"print", "println", "toString", "push"} {
		if !IsBuiltin(name) {
			t.Errorf("expected %s to be a builtin", name)
		}
	}
	if IsBuiltin("main") {
		t.Error("main must not be a builtin")
	}
}

func TestResolveBuiltinCannotBeRedeclared(t *testing.T) {
	msgs := resolveSource(t, `fn print(s: String) -> Void { return; }`)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "'print' is already declared") {
		t.Errorf("expected a redeclaration error, got %v", msgs)
	}
}

func TestResolveLambdasAndArrayLiterals(t *testing.T) {
	// Lambda bodies see their parameters and the enclosing variables
	msgs := resolveSource(t, `
		fn main() -> Int {
			let base = 1;
			let f = fn(x: Int) -> Int { return x + base; };
			let xs = [base, f(2)];
			return 0;
		}
	`)
	if len(msgs) != 0 {
		t.Errorf("expected no errors, got %v", msgs)
	}

	// Names inside lambdas and array literals are still checked
	msgs = resolveSource(t, `
		fn main() -> Int {
			let f = fn(x: Int) -> Int { return x + missing; };
			let xs = [alsoMissing];
			return 0;
		}
	`)
	if len(msgs) != 2 ||
		!strings.Contains(msgs[0], "undefined identifier 'missing'") ||
		!strings.Contains(msgs[1], "undefined identifier 'alsoMissing'") {
		t.Errorf("expected 2 undefined identifier errors, got %v", msgs)
	}

	// A lambda parameter is not visible outside the lambda
	msgs = resolveSource(t, `
		fn main() -> Int {
			let f = fn(x: Int) -> Int { return x; };
			return x;
		}
	`)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "undefined identifier 'x'") {
		t.Errorf("expected lambda parameter to be out of scope, got %v", msgs)
	}
}

func TestResolveSelfOnlyInMethods(t *testing.T) {
	msgs := resolveSource(t, `
		struct Point { x: Int; }
		impl Point { fn get() -> Int { return self.x; } }
		fn free() -> Int { return self.x; }
	`)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "undefined identifier 'self'") {
		t.Errorf("expected self to be undefined only in the free function, got %v", msgs)
	}
}

func TestResolveNestedTypes(t *testing.T) {
	msgs := resolveSource(t, `
		fn f(a: [Missing], b: Unknown?, c: Int | Gone, d: Self) -> Void { return; }
	`)
	if len(msgs) != 3 {
		t.Errorf("expected 3 undefined type errors, got %v", msgs)
	}
}

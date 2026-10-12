package desugar

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/parser"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/types"
)

// desugarBody desugars the statements of a main function and returns the
// printed result.
func desugarBody(t *testing.T, body string) string {
	t.Helper()

	src := "fn check(v: Bool) -> Bool { return v; }\nfn items() -> [Int] { return [1]; }\nfn main() -> Void {\n" + body + "\n}"
	errs := errors.NewHandler()
	prog := parser.Parse(src, "test.lum", errs)
	table := resolve.NewResolver(errs).Resolve(prog)
	typeInfo := types.NewChecker(prog, table, errs).Check()
	if errs.HasErrors() {
		t.Fatalf("test source is invalid: %s", errs.Diagnostics[0].Message)
	}

	lowered, _ := NewDesugarer(prog, typeInfo, table, errs, parser.SourceLines(src)).Desugar()
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic: %s", d.Message)
	}
	return lowered.Decls[2].(*ast.FnDecl).Body.String()
}

// before reports whether a appears before b in s (and both appear).
func before(s, a, b string) bool {
	i, j := strings.Index(s, a), strings.Index(s, b)
	return i >= 0 && j >= 0 && i < j
}

func TestForLoopIncrementsBeforeBody(t *testing.T) {
	out := desugarBody(t, `let xs = [1, 2]; for v in xs { continue; }`)

	if strings.Contains(out, "for v in") {
		t.Fatalf("expected the for loop to be rewritten, got: %s", out)
	}
	// `continue` jumps back to the condition; the increment must already
	// have happened or the loop would never advance.
	if !before(out, "(__iter_0 = (__iter_0 + 1));", "continue;") {
		t.Errorf("expected the increment before the body, got: %s", out)
	}
	if !before(out, "let v: int = xs[__iter_0];", "(__iter_0 = (__iter_0 + 1));") {
		t.Errorf("expected the element to be read before the increment, got: %s", out)
	}
}

func TestForLoopEvaluatesIterableOnce(t *testing.T) {
	out := desugarBody(t, `for v in items() { print(v); }`)

	if n := strings.Count(out, "items()"); n != 1 {
		t.Errorf("expected items() to be called once, found %d calls in: %s", n, out)
	}

	// A plain variable needs no temporary
	out = desugarBody(t, `let xs = [1]; for v in xs { print(v); }`)
	if strings.Contains(out, "__tmp_") {
		t.Errorf("expected no temporary for a variable iterable, got: %s", out)
	}
}

func TestWhileShortCircuitReevaluatesCondition(t *testing.T) {
	out := desugarBody(t, `let i = 0; while i < 3 && check(true) { i = i + 1; continue; }`)

	// The condition is computed by statements, so they have to run at the
	// top of every iteration; a guard then leaves the loop.
	if !strings.Contains(out, "while true {") {
		t.Fatalf("expected a `while true` loop with an explicit guard, got: %s", out)
	}
	if !before(out, "while true {", "check(true)") {
		t.Errorf("expected the condition to be evaluated inside the loop, got: %s", out)
	}
	if !before(out, "{ break; }", "(i = (i + 1));") {
		t.Errorf("expected the exit guard before the body, got: %s", out)
	}
	if n := strings.Count(out, "check(true)"); n != 1 {
		t.Errorf("expected the condition to appear once, found %d in: %s", n, out)
	}
}

func TestLetWithShortCircuitStaysInScope(t *testing.T) {
	out := desugarBody(t, `let ok = check(true) && check(false); print(ok);`)

	// The let must be a direct statement of the function body, so that
	// `print(ok)` after it can still see the variable.
	if !strings.Contains(out, "let ok = __tmp_") {
		t.Fatalf("expected the let to read a temporary, got: %s", out)
	}
	if strings.LastIndex(out, "{ let __tmp_") != 0 {
		t.Errorf("expected the helpers directly in the function body, got: %s", out)
	}
	if !before(out, "let ok = ", "print(ok);") {
		t.Errorf("expected the use after the declaration, got: %s", out)
	}
}

func TestMatchDefaultAndNegativeLiterals(t *testing.T) {
	out := desugarBody(t, `
		let n = 1;
		match n {
			case -1: { print("neg"); }
			case 2: { print("two"); }
			default: { print("other"); }
		}
	`)

	if strings.Contains(out, "match") {
		t.Fatalf("expected the match to be rewritten, got: %s", out)
	}
	for _, want := range []string{`if (n == (-1))`, `else if (n == 2)`, `else if true { print("other"); }`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in: %s", want, out)
		}
	}
}

func TestLambdaBodiesAreDesugared(t *testing.T) {
	out := desugarBody(t, `let f = fn(xs: [Int]) -> Void { for v in xs { print(v); } };`)

	if strings.Contains(out, "for v in") || !strings.Contains(out, "while") {
		t.Errorf("expected the loop inside the lambda to be rewritten, got: %s", out)
	}
}

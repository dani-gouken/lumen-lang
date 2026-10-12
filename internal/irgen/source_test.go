package irgen

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/analysis"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/ir"
	"github.com/dani-gouken/lumen/internal/parser"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/types"
)

// generateSource runs the front end and IR generation on source text.
func generateSource(t *testing.T, src string) (*ir.Program, *errors.Handler) {
	t.Helper()

	errs := errors.NewHandler()
	prog := parser.Parse(src, "test.lum", errs)
	table := resolve.NewResolver(errs).Resolve(prog)
	typeInfo := types.NewChecker(prog, table, errs).Check()
	if errs.HasErrors() {
		t.Fatalf("test source is invalid: %s", errs.Diagnostics[0].Message)
	}
	captures := analysis.NewAnalyzer(prog, table, typeInfo, errs).Analyze()
	return NewIRGenerator(prog, typeInfo, table, captures, errs).Generate(), errs
}

// dumpFunction returns the IR of one function, failing on any diagnostic.
func dumpFunction(t *testing.T, src, name string) string {
	t.Helper()

	prog, errs := generateSource(t, src)
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic: %s", d.Message)
	}
	for _, fn := range prog.Functions {
		if fn.Name == name {
			return fn.Dump()
		}
	}
	t.Fatalf("function %s not generated", name)
	return ""
}

func expectLines(t *testing.T, dump string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(dump, line) {
			t.Errorf("expected %q in:\n%s", line, dump)
		}
	}
}

func TestLetCopiesVariablesButAdoptsTemporaries(t *testing.T) {
	dump := dumpFunction(t, `fn main() -> Int { let a = 1; let b = a; b = 2; return a; }`, "main")

	// `let b = a` must not alias a's register, or `b = 2` would change a
	expectLines(t, dump,
		"%v1 = MOV, 1",
		"%v2 = MOV, %v1",
		"%v2 = MOV, %v3",
		"RET %v1",
	)
}

func TestBlocksScopeTheirVariables(t *testing.T) {
	dump := dumpFunction(t, `fn main() -> Int { let x = 1; { let x = 2; } return x; }`, "main")
	expectLines(t, dump, "%v1 = MOV, 1", "%v2 = MOV, 2", "RET %v1")
}

func TestAssignmentTargets(t *testing.T) {
	dump := dumpFunction(t, `
		struct P { x: Int; }
		fn main() -> Int {
			let xs = [1];
			let p = P { x: 1 };
			xs[0] = 5;
			p.x = 6;
			return 0;
		}`, "main")

	expectLines(t, dump, "SET_INDEX %v", `SET_FIELD %v`, `, "x", %v`)
}

func TestShortCircuitBranchesAroundRightOperand(t *testing.T) {
	src := `
		fn side() -> Bool { return true; }
		fn main() -> Bool { let a = false; return a %s side(); }`

	for op, branch := range map[string]string{"&&": "BR %v2, b1, b2", "||": "BR %v2, b2, b1"} {
		dump := dumpFunction(t, strings.Replace(src, "%s", op, 1), "main")
		// The call lives in its own block, reached only when needed
		expectLines(t, dump, branch, "b1:\n\t%v3 = MOV, \"side\"\n\t%v4 = CALL, %v3\n\t%v2 = MOV, %v4\n\tJMP b2")
	}

	dump := dumpFunction(t, `fn main() -> Int { let a = null; return a ?? 1; }`, "main")
	expectLines(t, dump, "EQ, %v2, %v3", "BR %v4, b1, b2")
}

func TestStructInitSetsFieldsByNameAndAttachesMethods(t *testing.T) {
	dump := dumpFunction(t, `
		struct P { x: Int; y: Int; }
		impl P { fn sum() -> Int { return self.x + self.y; } }
		fn main() -> Int { let p = P { y: 2, x: 1 }; return p.sum(); }`, "main")

	expectLines(t, dump,
		`NEW_STRUCT, "P"`,
		`, "x", %v`,
		`, "y", %v`,
		`MOV, "P.sum"`,
		`, "sum", %v`,
		// The method is read from the value and receives it as self
		`%v5 = GET_FIELD, %v3, "sum"`,
		`%v6 = CALL, %v5, %v3`,
	)
}

func TestMethodsTakeSelfAsFirstParameter(t *testing.T) {
	dump := dumpFunction(t, `
		struct P { x: Int; }
		impl P { fn add(n: Int) -> Int { return self.x + n; } }`, "P.add")

	expectLines(t, dump, "fn P.add(%v1, %v2)", `GET_FIELD, %v1, "x"`)
}

func TestVariadicArgumentsArePacked(t *testing.T) {
	dump := dumpFunction(t, `
		fn sum(first: Int, rest: ...Int) -> Int { return first; }
		fn main() -> Int { return sum(1, 2, 3); }`, "main")

	// One fixed argument, then the rest in an array
	expectLines(t, dump, "NEW_ARRAY, 2", "PUSH %v", "= CALL, %v1, %v2, %v5")

	// Builtins take their arguments as they are
	dump = dumpFunction(t, `fn main() -> Int { print(1, 2); return 0; }`, "main")
	if strings.Contains(dump, "NEW_ARRAY") {
		t.Errorf("expected no array for a builtin call, got:\n%s", dump)
	}
}

func TestLengthAndForLoop(t *testing.T) {
	dump := dumpFunction(t, `fn main() -> Int { let xs = [1]; return xs.length; }`, "main")
	expectLines(t, dump, "LEN, %v")

	// A for loop that was not desugared is still compiled as a real loop
	dump = dumpFunction(t, `fn main() -> Int { let t = 0; for v in [1, 2] { t = t + v; } return t; }`, "main")
	expectLines(t, dump, "LEN, %v", "LT, %v", "GET_INDEX, %v", "BR %v")
}

func TestClosureCaptureOperands(t *testing.T) {
	src := `
		fn main() -> Int {
			let a = 1;
			let outer = fn() -> Int {
				let inner = fn() -> Int { a = a + 1; return a; };
				return inner();
			};
			return outer();
		}`
	prog, errs := generateSource(t, src)
	if errs.HasErrors() {
		t.Fatalf("unexpected errors: %v", errs.Diagnostics)
	}

	closures := make(map[string][]ir.Value)
	upvalues := make(map[string]int)
	for _, fn := range prog.Functions {
		upvalues[fn.Name] = fn.UpvalueCount
		for _, bb := range fn.Blocks {
			for _, instr := range bb.Instructions {
				if instr.Op == ir.OpMakeClosure {
					closures[fn.Name] = instr.Operands[1:]
				}
			}
		}
	}

	// main hands its local to the outer lambda by register...
	if caps := closures["main"]; len(caps) != 1 {
		t.Fatalf("expected main to capture 1 variable, got %v", caps)
	} else if _, ok := caps[0].(*ir.Register); !ok {
		t.Errorf("expected a register capture in main, got %v", caps[0])
	}
	// ...which passes it on by upvalue index
	if caps := closures["lambda_0"]; len(caps) != 1 {
		t.Fatalf("expected the outer lambda to capture 1 variable, got %v", caps)
	} else if c, ok := caps[0].(ir.Constant); !ok || c.Type != "Upvalue" {
		t.Errorf("expected an upvalue capture in the outer lambda, got %v", caps[0])
	}

	for _, name := range []string{"lambda_0", "lambda_1"} {
		if upvalues[name] != 1 {
			t.Errorf("expected %s to have 1 upvalue, got %d", name, upvalues[name])
		}
	}
	if inner := dumpFunction(t, src, "lambda_1"); !strings.Contains(inner, "STORE_UPVALUE 0, %v") {
		t.Errorf("expected the inner lambda to write through its upvalue:\n%s", inner)
	}
}

func TestInstructionsCarrySourcePositions(t *testing.T) {
	prog, _ := generateSource(t, "fn main() -> Int {\n  let a = 1;\n  return a + 2;\n}")

	lines := make(map[ir.OpType]int)
	for _, instr := range prog.Functions[0].EntryBlock.Instructions {
		lines[instr.Op] = instr.Pos.Line
	}
	if lines[ir.OpAdd] != 3 || lines[ir.OpRet] != 3 {
		t.Errorf("expected ADD and RET on line 3, got %d and %d", lines[ir.OpAdd], lines[ir.OpRet])
	}
}

func TestGenerationErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
	}{
		{"struct name as value", `struct P { x: Int; } fn main() -> Int { let p = P; return 0; }`, ErrNotAValue},
		{"assign to function", `fn f() -> Int { return 1; } fn main() -> Int { f = f; return 0; }`, ErrInvalidAssignment},
		{"break outside loop", `fn main() -> Int { break; return 0; }`, ErrLoopControl},
		{"continue outside loop", `fn main() -> Int { continue; return 0; }`, ErrLoopControl},
		{"field and method share a name", `
			struct P { size: Int; }
			struct Q { n: Int; }
			impl Q { fn size() -> Int { return self.n; } }
			fn main() -> Int { let q = Q { n: 1 }; return q.size(); }`, ErrAmbiguousMember},
		{"match that was not desugared", `fn main() -> Int { match 1 { case 1: { return 1; } } return 0; }`, ErrUnsupportedConstruct},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := generateSource(t, tt.src)
			if len(errs.Diagnostics) == 0 {
				t.Fatal("expected a diagnostic, got none")
			}
			if got := errs.Diagnostics[0].Code; got != tt.code {
				t.Errorf("expected code %s, got %s (%s)", tt.code, got, errs.Diagnostics[0].Message)
			}
		})
	}
}

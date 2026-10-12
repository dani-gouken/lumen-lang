package parser

import (
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
)

// parseOK parses source that is expected to be valid.
func parseOK(t *testing.T, src string) *ast.Program {
	t.Helper()
	errs := errors.NewHandler()
	prog := Parse(src, "test.lum", errs)
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic: %s: %s (%s)", d.Code, d.Message, d.Pos)
	}
	if t.Failed() {
		t.FailNow()
	}
	return prog
}

// parseBody parses statements wrapped in a function and returns them.
func parseBody(t *testing.T, body string) []ast.Stmt {
	t.Helper()
	prog := parseOK(t, "fn main() -> Void {\n"+body+"\n}")
	return prog.Decls[0].(*ast.FnDecl).Body.Stmts
}

// parseExprString parses one expression and returns its printed form, which
// makes the grouping chosen by the parser explicit.
func parseExprString(t *testing.T, expr string) string {
	t.Helper()
	stmts := parseBody(t, "let v = "+expr+";")
	return stmts[0].(*ast.LetStmt).Value.String()
}

func TestParseExpressionPrecedence(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"1 + 2 * 3", "(1 + (2 * 3))"},
		{"1 - 2 - 3", "((1 - 2) - 3)"},
		{"(1 + 2) * 3", "((1 + 2) * 3)"},
		{"a < b == c > d", "((a < b) == (c > d))"},
		{"a || b && c", "(a || (b && c))"},
		{"a ?? b == c", "(a ?? (b == c))"},
		{"-a * !b", "((-a) * (!b))"},
		{"a.b[0](1, 2).c", "a.b[0](1, 2).c"},
		{"f(x)(y)", "f(x)(y)"},
		{"[1, [2, 3], f()]", "[1, [2, 3], f()]"},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			if got := parseExprString(t, tt.src); got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestParseGenericCallVersusComparison(t *testing.T) {
	// `<` after an identifier starts type arguments only when the closing
	// `>` is followed by a call or a struct literal.
	call := parseBody(t, "let v = collect<Int, [String]>(1, 2);")[0].(*ast.LetStmt).Value.(*ast.CallExpr)
	ident := call.Func.(*ast.Ident)
	if ident.Name != "collect" || len(ident.TypeArgs) != 2 {
		t.Fatalf("expected collect with 2 type args, got %s", ident)
	}
	if ident.TypeArgs[1].Kind != ast.TypeArray {
		t.Errorf("expected second type arg to be an array, got %s", ident.TypeArgs[1])
	}

	if got := parseExprString(t, "a < b"); got != "(a < b)" {
		t.Errorf("expected comparison, got %s", got)
	}
	if got := parseExprString(t, "a < b && c > (d)"); got != "((a < b) && (c > d))" {
		t.Errorf("expected two comparisons, got %s", got)
	}
}

func TestParseStructLiteralVersusBlock(t *testing.T) {
	stmts := parseBody(t, `
		let p = Point { x: 1, y: 2 };
		let e = Empty {};
		if p { return; }
		while ready { step(); }
		if (Point { x: 1 }) { return; }
	`)

	init, ok := stmts[0].(*ast.LetStmt).Value.(*ast.StructInitExpr)
	if !ok || init.Name != "Point" || len(init.Fields) != 2 {
		t.Fatalf("expected Point struct literal with 2 fields, got %s", stmts[0])
	}
	if _, ok := stmts[1].(*ast.LetStmt).Value.(*ast.StructInitExpr); !ok {
		t.Errorf("expected empty struct literal, got %s", stmts[1])
	}

	// In a statement header `name {` opens the body, not a struct literal
	if cond, ok := stmts[2].(*ast.IfStmt).Cond.(*ast.Ident); !ok || cond.Name != "p" {
		t.Errorf("expected `if p` to have an identifier condition, got %s", stmts[2])
	}
	if _, ok := stmts[3].(*ast.WhileStmt).Cond.(*ast.Ident); !ok {
		t.Errorf("expected `while ready` to have an identifier condition, got %s", stmts[3])
	}
	// Parentheses make the struct literal unambiguous again
	if _, ok := stmts[4].(*ast.IfStmt).Cond.(*ast.StructInitExpr); !ok {
		t.Errorf("expected parenthesized struct literal condition, got %s", stmts[4])
	}
}

func TestParseStatements(t *testing.T) {
	stmts := parseBody(t, `
		const limit: Int = 10;
		x = 1;
		xs[0] = 2;
		p.x = 3;
		for item in items { continue; }
		while true { break; }
		if a { } else if b { } else { }
		return fn(n: Int) -> Int { return n; };
	`)

	let := stmts[0].(*ast.LetStmt)
	if !let.IsConst || let.Type == nil || let.Type.Name != "Int" {
		t.Errorf("expected const with Int type, got %s", let)
	}

	// Assignments are binary expressions with the "=" operator
	targets := []string{"x", "xs[0]", "p.x"}
	for i, want := range targets {
		assign, ok := stmts[i+1].(*ast.ExprStmt).Expr.(*ast.BinaryExpr)
		if !ok || assign.Op != "=" || assign.Left.String() != want {
			t.Errorf("statement %d: expected assignment to %s, got %s", i+1, want, stmts[i+1])
		}
	}

	if loop := stmts[4].(*ast.ForStmt); loop.Var != "item" || loop.Iterable.String() != "items" {
		t.Errorf("unexpected for loop: %s", loop)
	}
	if _, ok := stmts[5].(*ast.WhileStmt).Body.Stmts[0].(*ast.BreakStmt); !ok {
		t.Errorf("expected break in while body")
	}

	ifStmt := stmts[6].(*ast.IfStmt)
	elseIf, ok := ifStmt.Else.(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected else-if chain, got %T", ifStmt.Else)
	}
	if _, ok := elseIf.Else.(*ast.BlockStmt); !ok {
		t.Errorf("expected final else block, got %T", elseIf.Else)
	}

	if _, ok := stmts[7].(*ast.ReturnStmt).Value.(*ast.FnExpr); !ok {
		t.Errorf("expected lambda as return value")
	}
}

func TestParseMatch(t *testing.T) {
	stmts := parseBody(t, `
		match n {
			case 0: { return; }
			case -1: f();
			case String: { return; }
			default: { return; }
		}
	`)

	cases := stmts[0].(*ast.MatchStmt).Cases
	if len(cases) != 4 {
		t.Fatalf("expected 4 cases, got %d", len(cases))
	}
	if _, ok := cases[0].Pattern.(*ast.Literal); !ok {
		t.Errorf("case 0: expected literal pattern, got %T", cases[0].Pattern)
	}
	if _, ok := cases[1].Body.(*ast.CallExpr); !ok {
		t.Errorf("case 1: expected expression body, got %T", cases[1].Body)
	}
	if typ, ok := cases[2].Pattern.(*ast.Type); !ok || typ.Name != "String" {
		t.Errorf("case 2: expected type pattern String, got %T", cases[2].Pattern)
	}
	if cases[3].Pattern != nil {
		t.Errorf("default: expected nil pattern, got %T", cases[3].Pattern)
	}
}

func TestParseDeclarations(t *testing.T) {
	prog := parseOK(t, `
		import "std/io" as io;

		struct Pair<A, B> { first: A; second: B, }
		enum Shape { Circle(Float), Square(Float, Float), Empty }
		trait Named { fn name() -> String; }
		impl Named for Point { fn name() -> String { return "p"; } }
		impl Point { fn zero() -> Bool { return true; } }
		fn map<T, U>(xs: [T], f: fn(T) -> U, rest: ...Int) -> [U]? | Int { return xs; }
	`)

	if len(prog.Imports) != 1 || prog.Imports[0].Path != "std/io" || prog.Imports[0].Alias != "io" {
		t.Errorf("unexpected imports: %v", prog.Imports)
	}
	if len(prog.Decls) != 6 {
		t.Fatalf("expected 6 declarations, got %d", len(prog.Decls))
	}

	pair := prog.Decls[0].(*ast.StructDecl)
	if len(pair.TypeParams) != 2 || len(pair.Fields) != 2 {
		t.Errorf("unexpected struct: %s", pair)
	}

	shape := prog.Decls[1].(*ast.EnumDecl)
	if len(shape.Variants) != 3 || len(shape.Variants[1].Values) != 2 {
		t.Errorf("unexpected enum: %s", shape)
	}

	trait := prog.Decls[2].(*ast.TraitDecl)
	if m := trait.Methods[0]; m.Body != nil || !m.IsTraitMethod {
		t.Errorf("expected abstract trait method, got %s", m)
	}

	traitImpl := prog.Decls[3].(*ast.ImplDecl)
	if traitImpl.TraitName != "Named" || traitImpl.StructName != "Point" {
		t.Errorf("unexpected impl: %s", traitImpl)
	}
	inherent := prog.Decls[4].(*ast.ImplDecl)
	if inherent.TraitName != "" || inherent.StructName != "Point" {
		t.Errorf("unexpected inherent impl: trait=%q struct=%q", inherent.TraitName, inherent.StructName)
	}

	fn := prog.Decls[5].(*ast.FnDecl)
	if len(fn.TypeParams) != 2 || len(fn.Params) != 3 {
		t.Fatalf("unexpected function: %s", fn)
	}
	if fn.Params[1].Type.Kind != ast.TypeFunction {
		t.Errorf("expected function type parameter, got %s", fn.Params[1].Type)
	}
	if fn.Params[2].Type.Kind != ast.TypeVariadic {
		t.Errorf("expected variadic parameter, got %s", fn.Params[2].Type)
	}
	ret := fn.ReturnType
	if ret.Kind != ast.TypeUnion || ret.UnionTypes[0].Kind != ast.TypeOption {
		t.Errorf("expected union with optional first member, got %s", ret)
	}
}

func TestParseNodesCarryPositions(t *testing.T) {
	stmts := parseBody(t, "let total = a + b;")
	sum := stmts[0].(*ast.LetStmt).Value.(*ast.BinaryExpr)

	// Diagnostics underline the token stored on the node
	if tok := sum.Token(); tok.Literal != "+" || tok.Pos.Line != 2 || tok.Pos.Col != 15 {
		t.Errorf("expected '+' at 2:15, got %q at %d:%d", tok.Literal, tok.Pos.Line, tok.Pos.Col)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
		msg  string
		line int
	}{
		{"missing semicolon", "fn main() -> Int {\n  let x = 1\n  return x;\n}", ErrMissingSemicolon, "expected ';'", 2},
		{"missing expression", "fn main() -> Int {\n  let x = ;\n}", ErrUnexpectedToken, "expected expression", 2},
		{"bad declaration", "let x = 1;", ErrUnexpectedToken, "expected declaration", 1},
		{"unclosed block", "fn main() -> Int {\n  return 1;", ErrUnexpectedToken, "expected '}'", 2},
		{"invalid assignment target", "fn main() -> Int {\n  1 + 2 = 3;\n}", ErrInvalidAssign, "invalid assignment target", 2},
		{"duplicate field", "fn main() -> Int {\n  let p = P { x: 1, x: 2 };\n}", ErrDuplicateField, "duplicate field 'x'", 2},
		{"unterminated string", "fn main() -> Int {\n  let s = \"abc;\n}", ErrLexical, "unterminated string literal", 2},
		{"unknown character", "fn main() -> Int {\n  let s = 1 # 2;\n}", ErrLexical, "unexpected character '#'", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := errors.NewHandler()
			Parse(tt.src, "test.lum", errs)

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
			if d.Pos.Line != tt.line {
				t.Errorf("expected line %d, got %d", tt.line, d.Pos.Line)
			}
			if d.Context == "" {
				t.Error("expected the diagnostic to carry the source line")
			}
		})
	}
}

func TestParseTypeBeforeParameterName(t *testing.T) {
	// The `fn(T)` type inside the broken parameter list must not be taken
	// for the start of the next declaration.
	src := "fn map<T, U>([T] values, fn(T) -> U mapper) -> Void { return; }\nfn main() -> Int { return 0; }"
	errs := errors.NewHandler()
	prog := Parse(src, "test.lum", errs)

	if len(errs.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(errs.Diagnostics), errs.Diagnostics)
	}
	d := errs.Diagnostics[0]
	if d.Code != ErrUnexpectedToken || !strings.Contains(d.Suggestion, "name: Type") {
		t.Errorf("expected a hint about parameter syntax, got %s: %s (%s)", d.Code, d.Message, d.Suggestion)
	}
	if len(prog.Decls) != 1 || prog.Decls[0].(*ast.FnDecl).Name != "main" {
		t.Errorf("expected main to be parsed after the broken declaration")
	}
}

func TestParseErrorRecovery(t *testing.T) {
	// One error per broken statement or declaration; everything else is
	// still parsed.
	src := `
fn first() -> Int {
    let a = ;
    let b = 2
    let c = 3;
    return c;
}

struct Broken { x Int }

fn last() -> Int {
    return 1;
}
`
	errs := errors.NewHandler()
	prog := Parse(src, "test.lum", errs)

	var codes []string
	for _, d := range errs.Diagnostics {
		codes = append(codes, d.Code)
	}
	want := []string{ErrUnexpectedToken, ErrMissingSemicolon, ErrUnexpectedToken}
	if strings.Join(codes, ",") != strings.Join(want, ",") {
		t.Fatalf("expected diagnostics %v, got %v", want, codes)
	}

	var names []string
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			names = append(names, fn.Name)
		}
	}
	if strings.Join(names, ",") != "first,last" {
		t.Errorf("expected functions first and last to survive, got %v", names)
	}

	// `let a = ;` is dropped, the statements after it are kept
	first := prog.Decls[0].(*ast.FnDecl)
	if len(first.Body.Stmts) != 3 {
		t.Errorf("expected 3 statements in first(), got %d", len(first.Body.Stmts))
	}
}

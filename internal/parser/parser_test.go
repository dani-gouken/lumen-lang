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
		{"a - -b", "(a - (-b))"},
		{"a + -b * -c", "(a + ((-b) * (-c)))"},
		{"a.b[0](1, 2).c", "a.b[0](1, 2).c"},
		{"f(x)(y)", "f(x)(y)"},
		{"[1, [2, 3], f()]", "[1, [2, 3], f()]"},
		// `as` binds tighter than binary operators, looser than postfix
		{"a as Int + 1", "((a as Int) + 1)"},
		{"a.b[0] as String? == null", "((a.b[0] as String?) == null)"},
		{"-a as Int", "(-(a as Int))"},
		{"a as Any as Int", "((a as Any) as Int)"},
		// `is` sits at the same level as `as`
		{"a is Int && b", "((a is Int) && b)"},
		{"!a is [String]", "(!(a is [String]))"},
		{"a.b is Point? == true", "((a.b is Point?) == true)"},
		{"a as Any is Int", "((a as Any) is Int)"},
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

func TestParseMethodTypeArguments(t *testing.T) {
	// The same rule as for function names: `<` after a member name starts
	// type arguments only when a call follows the closing `>`.
	call := parseBody(t, "let v = stack.map<String, [Int]>(f);")[0].(*ast.LetStmt).Value.(*ast.CallExpr)
	member := call.Func.(*ast.MemberExpr)
	if member.Name != "map" || len(member.TypeArgs) != 2 || member.TypeArgs[1].Kind != ast.TypeArray {
		t.Fatalf("expected map with 2 type arguments, got %s", member)
	}
	if got := call.String(); got != "stack.map<String, [Int]>(f)" {
		t.Errorf("expected the type arguments to print back, got %s", got)
	}

	tests := []struct {
		src  string
		want string
	}{
		{"a.b < c", "(a.b < c)"},
		{"a.b < c.d", "(a.b < c.d)"},
		{"a.b < c && d > (e)", "((a.b < c) && (d > e))"},
		{"a.b<T>(x).c<U>(y)", "a.b<T>(x).c<U>(y)"},
		{"a.b(x)", "a.b(x)"},
	}
	for _, tt := range tests {
		if got := parseExprString(t, tt.src); got != tt.want {
			t.Errorf("%s: expected %s, got %s", tt.src, tt.want, got)
		}
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

func TestParseCompoundAssignment(t *testing.T) {
	stmts := parseBody(t, `
		x += 1;
		xs[i] -= 2;
		p.count *= 3;
		a.b[0].c /= 4;
		x %= 5;
		x++;
		xs[i]--;
		p.count++;
	`)

	want := []struct{ target, op, value string }{
		{"x", "+=", "1"},
		{"xs[i]", "-=", "2"},
		{"p.count", "*=", "3"},
		{"a.b[0].c", "/=", "4"},
		{"x", "%=", "5"},
		// ++ and -- are += 1 and -= 1
		{"x", "+=", "1"},
		{"xs[i]", "-=", "1"},
		{"p.count", "+=", "1"},
	}
	for i, w := range want {
		assign, ok := stmts[i].(*ast.ExprStmt).Expr.(*ast.BinaryExpr)
		if !ok {
			t.Errorf("statement %d: expected an assignment, got %s", i, stmts[i])
			continue
		}
		if assign.Left.String() != w.target || assign.Op != w.op || assign.Right.String() != w.value {
			t.Errorf("statement %d: expected %s %s %s, got %s %s %s",
				i, w.target, w.op, w.value, assign.Left, assign.Op, assign.Right)
		}
		if base, compound := ast.CompoundOp(assign.Op); !compound || base != w.op[:1] {
			t.Errorf("statement %d: expected %s to be a compound assignment of %s", i, assign.Op, w.op[:1])
		}
	}

	// The right side is a full expression
	assign := parseBody(t, "total += a * b + f(c);")[0].(*ast.ExprStmt).Expr.(*ast.BinaryExpr)
	if got := assign.Right.String(); got != "((a * b) + f(c))" {
		t.Errorf("expected the whole expression on the right, got %s", got)
	}

	// They are statements: none has a value, and the target must be assignable
	for _, src := range []string{
		"let y = x++;",
		"f(x += 1);",
		"1 += 2;",
		"f()++;",
		"(a + b) -= 1;",
		"x++ + 1;",
	} {
		errs := errors.NewHandler()
		Parse("fn main() -> Void {\n"+src+"\n}", "test.lum", errs)
		if !errs.HasErrors() {
			t.Errorf("%q: expected a syntax error", src)
		}
	}
}

func TestParseForLoopWithIndex(t *testing.T) {
	stmts := parseBody(t, `
		for item in items { }
		for i, item in items { }
		for row, cells in grid[0].rows { for col, cell in cells { } }
	`)

	plain := stmts[0].(*ast.ForStmt)
	if plain.Index != nil || plain.Var != "item" {
		t.Errorf("expected a loop without index, got %s", plain)
	}

	indexed := stmts[1].(*ast.ForStmt)
	if indexed.Index == nil || indexed.Index.Name != "i" || indexed.Var != "item" {
		t.Fatalf("expected index i and variable item, got %s", indexed)
	}
	// The index points at its own name, for diagnostics
	if tok := indexed.Index.Token(); tok.Literal != "i" || tok.Pos.Line != 4 {
		t.Errorf("expected the index to carry its token, got %q on line %d", tok.Literal, tok.Pos.Line)
	}
	if got := indexed.String(); !strings.HasPrefix(got, "for i, item in items") {
		t.Errorf("expected the loop to print with its index, got %s", got)
	}

	outer := stmts[2].(*ast.ForStmt)
	inner := outer.Body.Stmts[0].(*ast.ForStmt)
	if outer.Index.Name != "row" || inner.Index.Name != "col" || outer.Iterable.String() != "grid[0].rows" {
		t.Errorf("unexpected nested loops: %s", outer)
	}

	for _, src := range []string{"for i, in items { }", "for i, j, k in items { }", "for , item in items { }"} {
		errs := errors.NewHandler()
		Parse("fn main() -> Void {\n"+src+"\n}", "test.lum", errs)
		if !errs.HasErrors() {
			t.Errorf("%q: expected a syntax error", src)
		}
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

func TestParseTypePatterns(t *testing.T) {
	prog := parseOK(t, `
		struct Point { x: Int; }
		trait Shape { fn area() -> Int; }
		struct Box<T> { value: T; }
		fn f(v: Any, limit: Int) -> Void {
			match v {
				case Int: { return; }
				case [String]: { return; }
				case Int?: { return; }
				case fn(Int) -> Bool: { return; }
				case Box<Int>: { return; }
				case Point: { return; }
				case Shape: { return; }
				case limit: { return; }
				case 3: { return; }
				case -1: { return; }
				default: { return; }
			}
		}
	`)

	var cases []*ast.MatchCase
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			cases = fn.Body.Stmts[0].(*ast.MatchStmt).Cases
		}
	}

	// A pattern is a type when it can only be one, or when it is the name
	// of a struct or trait declared in the file
	wantTypes := []string{"Int", "[String]", "Int?", "fn(Int) -> Bool", "Box<Int>", "Point", "Shape"}
	for i, want := range wantTypes {
		typ, ok := cases[i].Pattern.(*ast.Type)
		if !ok {
			t.Errorf("case %d: expected the type pattern %s, got %T", i, want, cases[i].Pattern)
			continue
		}
		if typ.String() != want {
			t.Errorf("case %d: expected %s, got %s", i, want, typ)
		}
	}

	// Any other name stays an expression, as do literals
	if _, ok := cases[7].Pattern.(*ast.Ident); !ok {
		t.Errorf("expected `limit` to stay an identifier, got %T", cases[7].Pattern)
	}
	if _, ok := cases[8].Pattern.(*ast.Literal); !ok {
		t.Errorf("expected a literal pattern, got %T", cases[8].Pattern)
	}
	if _, ok := cases[9].Pattern.(*ast.UnaryExpr); !ok {
		t.Errorf("expected a negative literal pattern, got %T", cases[9].Pattern)
	}
	if cases[10].Pattern != nil {
		t.Errorf("expected default to have no pattern, got %T", cases[10].Pattern)
	}
}

func TestParseDeclarations(t *testing.T) {
	prog := parseOK(t, `
		import std.io as io;

		struct Pair<A, B> { first: A; second: B, }
		enum Shape { Circle(Float), Square(Float, Float), Empty }
		trait Named { fn name() -> String; }
		impl Named for Point { fn name() -> String { return "p"; } }
		impl Point { fn zero() -> Bool { return true; } }
		fn map<T, U>(xs: [T], f: fn(T) -> U, rest: ...Int) -> [U]? | Int { return xs; }
	`)

	if len(prog.Imports) != 1 || prog.Imports[0].Path != "std.io" || prog.Imports[0].Alias != "io" {
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

func TestParseGenericImpl(t *testing.T) {
	prog := parseOK(t, `
		impl<T> Box<T> { fn get() -> T { return self.value; } }
		impl<K, V> Show for Pair<K, V> { fn show() -> String { return ""; } }
	`)

	box := prog.Decls[0].(*ast.ImplDecl)
	if box.StructName != "Box" || box.TraitName != "" || strings.Join(box.TypeParams, ",") != "T" {
		t.Errorf("unexpected impl: %s", box)
	}
	pair := prog.Decls[1].(*ast.ImplDecl)
	if pair.StructName != "Pair" || pair.TraitName != "Show" || strings.Join(pair.TypeParams, ",") != "K,V" {
		t.Errorf("unexpected trait impl: %s", pair)
	}

	// The struct has to repeat the parameters, in order
	for _, src := range []string{
		"impl<T> Box { fn get() -> T { return self.value; } }",
		"impl<K, V> Pair<V, K> { fn show() -> String { return \"\"; } }",
	} {
		errs := errors.NewHandler()
		Parse(src, "test.lum", errs)
		if len(errs.Diagnostics) != 1 || !strings.Contains(errs.Diagnostics[0].Suggestion, "Write: impl<") {
			t.Errorf("expected one error showing the right form for %q, got %v", src, errs.Diagnostics)
		}
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
		{"import of a string", "import \"std/io\";", ErrUnexpectedToken, "expected module path", 1},
		{"import without a path", "import ;", ErrUnexpectedToken, "expected module path", 1},
		{"import ending in a dot", "import a.;", ErrUnexpectedToken, "expected module name", 1},
		{"empty import list", "import a.b.{};", ErrUnexpectedToken, "expected a name to import", 1},
		{"alias after a list", "import a.b.{c} as d;", ErrUnexpectedToken, "'as' cannot follow a list", 1},
		{"import without semicolon", "import a.b\nfn main() -> Int { return 0; }", ErrMissingSemicolon, "expected ';'", 1},
		{"pub on an impl", "pub impl Point {\n}", ErrUnexpectedToken, "'pub' cannot be applied to an impl block", 1},
		{"constant without a value", "const LIMIT;", ErrUnexpectedToken, "expected '=', found ';'", 1},
		{"constant without a name", "const = 3;", ErrUnexpectedToken, "expected constant name", 1},
		{"constant without semicolon", "const A = 1\nconst B = 2;", ErrMissingSemicolon, "expected ';'", 1},
		{"variable at the top level", "let total = 0;", ErrUnexpectedToken, "expected declaration, found 'let'", 1},
		{"pattern that is an operation", "fn f(s: S) -> Int {\n  match s {\n    case S.A(x + 1): { return 1; }\n  }\n}", ErrUnexpectedToken, "this cannot be matched against a value of a variant", 3},
		{"pattern that is an array", "fn f(s: S) -> Int {\n  match s {\n    case S.A([1]): { return 1; }\n  }\n}", ErrUnexpectedToken, "this cannot be matched against a value of a variant", 3},
		{"pattern that is a generic call", "fn f(s: S) -> Int {\n  match s {\n    case S.A(g<Int>()): { return 1; }\n  }\n}", ErrUnexpectedToken, "this cannot be matched against a value of a variant", 3},
		{"is with a number", "fn f(n: Int) -> Bool {\n  return n is 3;\n}", ErrUnexpectedToken, "'is' takes a type or the pattern of a variant, found '3'", 2},
		{"is with null", "fn f(n: Int?) -> Bool {\n  return n is null;\n}", ErrUnexpectedToken, "'is' takes a type or the pattern of a variant", 2},
		{"is with a text", "fn f(s: String) -> Bool {\n  return s is \"a\";\n}", ErrUnexpectedToken, "'is' takes a type or the pattern of a variant", 2},
		{"is with a variant without its enum", "fn f(s: Shape) -> Bool {\n  if s is Circle(r) { return true; }\n  return false;\n}", ErrUnexpectedToken, "'Circle' alone is a type, and a type holds no values to name", 2},
		{"is with a call", "fn f(s: Shape) -> Bool {\n  return s is shapes.make().kind;\n}", ErrUnexpectedToken, "'is' takes a type or the pattern of a variant, and this is neither", 2},
		{"is with an index", "fn f(s: Shape) -> Bool {\n  return s is all.shapes[0];\n}", ErrUnexpectedToken, "'is' takes a type or the pattern of a variant, and this is neither", 2},
		{"is pattern with an operation", "fn f(s: S) -> Bool {\n  return s is S.A(x + 1);\n}", ErrUnexpectedToken, "this cannot be matched against a value of a variant", 2},
		{"is with an unfinished type", "fn f(s: S) -> Bool {\n  return s is Box<Int;\n}", ErrUnexpectedToken, "expected '>'", 2},
		{"enum without a name", "enum { A }", ErrUnexpectedToken, "expected enum name", 1},
		{"variant that is a number", "enum E { 1 }", ErrUnexpectedToken, "expected variant name", 1},
		{"unclosed variant", "enum E { A(Int }", ErrUnexpectedToken, "expected ')'", 1},
		{"extern with a body", "extern fn f() -> Int { return 1; }", ErrUnexpectedToken, "an extern function has no body", 1},
		{"extern with type parameters", "extern fn f<T>(v: T) -> T;", ErrUnexpectedToken, "an extern function cannot have type parameters", 1},
		{"extern without fn", "extern struct P { x: Int; }", ErrUnexpectedToken, "expected 'fn' after 'extern'", 1},
		{"extern without semicolon", "extern fn f() -> Int\nfn main() -> Int { return 0; }", ErrMissingSemicolon, "expected ';'", 1},
		{"extern in an impl", "impl P {\n  extern fn f();\n}", ErrUnexpectedToken, "expected 'fn'", 2},
		{"pub on nothing", "pub let x = 1;", ErrUnexpectedToken, "expected declaration", 1},
		{"pub in a trait", "trait T {\n  pub fn m();\n}", ErrUnexpectedToken, "expected 'fn'", 2},
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

func TestParseImports(t *testing.T) {
	prog := parseOK(t, `
		import player;
		import physics.body;
		import physics.body as pb;
		import physics.collide.{overlaps, Shape};
		import util.{clamp,};
		pub import physics.body.{Body};

		fn main() -> Int { return 0; }
	`)

	type want struct {
		path, alias, names string
		pub                bool
	}
	wants := []want{
		{"player", "", "", false},
		{"physics.body", "", "", false},
		{"physics.body", "pb", "", false},
		{"physics.collide", "", "overlaps,Shape", false},
		{"util", "", "clamp", false},
		{"physics.body", "", "Body", true},
	}
	if len(prog.Imports) != len(wants) {
		t.Fatalf("expected %d imports, got %d", len(wants), len(prog.Imports))
	}
	for i, w := range wants {
		imp := prog.Imports[i]
		var names []string
		for _, n := range imp.Names {
			names = append(names, n.Name)
		}
		if imp.Path != w.path || imp.Alias != w.alias || strings.Join(names, ",") != w.names || imp.Pub != w.pub {
			t.Errorf("import %d: expected %+v, got %s", i, w, imp)
		}
	}

	// The import is positioned at its path, all of it
	body := prog.Imports[1]
	if tok := body.Token(); tok.Pos.Col != 10 || tok.EndPos.Col-tok.Pos.Col != len("physics.body") {
		t.Errorf("expected the token to span the path, got columns %d-%d", tok.Pos.Col, tok.EndPos.Col)
	}
	// Each imported name has its own position
	if name := prog.Imports[3].Names[1]; name.Token().Literal != "Shape" || name.Position().Line != 5 {
		t.Errorf("unexpected position of an imported name: %v", name.Token())
	}
	// Printed back in the syntax it was written in
	if got := prog.Imports[5].String(); got != "pub import physics.body.{Body};" {
		t.Errorf("unexpected rendering: %s", got)
	}
	if got := prog.Imports[2].String(); got != "import physics.body as pb;" {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestParsePub(t *testing.T) {
	prog := parseOK(t, `
		pub struct Point { x: Int; }
		struct Hidden { x: Int; }
		pub trait Named { fn name() -> String; }
		pub enum Color { Red, Green }
		pub fn origin() -> Point { return Point { x: 0 }; }
		fn helper() -> Int { return 1; }
		impl Point {
			pub fn shown() -> Int { return self.hidden(); }
			fn hidden() -> Int { return 1; }
		}
	`)

	if !prog.Decls[0].(*ast.StructDecl).Pub || prog.Decls[1].(*ast.StructDecl).Pub {
		t.Error("expected only Point to be a pub struct")
	}
	if !prog.Decls[2].(*ast.TraitDecl).Pub || !prog.Decls[3].(*ast.EnumDecl).Pub {
		t.Error("expected the trait and the enum to be pub")
	}
	if !prog.Decls[4].(*ast.FnDecl).Pub || prog.Decls[5].(*ast.FnDecl).Pub {
		t.Error("expected only origin to be a pub function")
	}
	methods := prog.Decls[6].(*ast.ImplDecl).Methods
	if !methods[0].Pub || methods[1].Pub {
		t.Error("expected only shown to be a pub method")
	}
}

func TestParseQualifiedNames(t *testing.T) {
	prog := parseOK(t, `
		impl body.Shape for Wall { fn left() -> Int { return 0; } }
		impl Shape for body.Body { fn left() -> Int { return 0; } }
		impl<T> box.Box<T> { fn get() -> T { return self.value; } }

		fn f(a: body.Body, b: [body.Body]?, c: box.Box<body.Body>, d: fn(body.Body) -> geo.Point) -> body.Body | Int {
			let made = body.Body { x: 1 };
			let boxed = box.Box<Int> { value: 1 };
			let empty = body.Empty {};
			let called = body.new(1);
			let generic = body.first<Int>([1]);
			let test = a is body.Body;
			let cast = a as body.Body;
			if a.visible { return a; }
			while a.next.ready { return a; }
			match a {
				case body.Body: { return a; }
				default: { return 0; }
			}
		}
	`)

	first, second, third := prog.Decls[0].(*ast.ImplDecl), prog.Decls[1].(*ast.ImplDecl), prog.Decls[2].(*ast.ImplDecl)
	if first.TraitName != "body.Shape" || first.StructName != "Wall" {
		t.Errorf("unexpected impl: %s for %s", first.TraitName, first.StructName)
	}
	if second.TraitName != "Shape" || second.StructName != "body.Body" {
		t.Errorf("unexpected impl: %s for %s", second.TraitName, second.StructName)
	}
	if third.StructName != "box.Box" || len(third.TypeParams) != 1 {
		t.Errorf("unexpected generic impl: %s", third)
	}

	fn := prog.Decls[3].(*ast.FnDecl)
	types := []string{"body.Body", "[body.Body]?", "box.Box<body.Body>", "fn(body.Body) -> geo.Point"}
	for i, want := range types {
		if got := fn.Params[i].Type.String(); got != want {
			t.Errorf("parameter %d: expected %s, got %s", i, want, got)
		}
	}
	if got := fn.ReturnType.String(); got != "body.Body | Int" {
		t.Errorf("unexpected return type %s", got)
	}
	// A qualified type is underlined whole
	if tok := fn.Params[0].Type.Token(); tok.Literal != "body.Body" || tok.EndPos.Col-tok.Pos.Col != len("body.Body") {
		t.Errorf("unexpected token of a qualified type: %+v", tok)
	}

	value := func(i int) ast.Expr { return fn.Body.Stmts[i].(*ast.LetStmt).Value }
	made, ok := value(0).(*ast.StructInitExpr)
	if !ok || made.Name != "body.Body" || len(made.Fields) != 1 {
		t.Errorf("expected a struct literal of body.Body, got %s", value(0))
	}
	if tok := made.Token(); tok.Literal != "body.Body" || tok.EndPos.Col-tok.Pos.Col != len("body.Body") {
		t.Errorf("unexpected token of a qualified struct literal: %+v", tok)
	}
	boxed, ok := value(1).(*ast.StructInitExpr)
	if !ok || boxed.Name != "box.Box" || len(boxed.TypeArgs) != 1 {
		t.Errorf("expected a generic struct literal of box.Box, got %s", value(1))
	}
	if empty, ok := value(2).(*ast.StructInitExpr); !ok || empty.Name != "body.Empty" {
		t.Errorf("expected an empty struct literal, got %s", value(2))
	}
	// Calls stay member accesses: only the module pass knows `body` is a module
	if call, ok := value(3).(*ast.CallExpr); !ok || call.Func.String() != "body.new" {
		t.Errorf("expected a call of body.new, got %s", value(3))
	}
	if call, ok := value(4).(*ast.CallExpr); !ok || call.Func.String() != "body.first<Int>" {
		t.Errorf("expected a generic call, got %s", value(4))
	}
	if got := value(5).String(); got != "(a is body.Body)" {
		t.Errorf("unexpected type test %s", got)
	}
	if got := value(6).String(); got != "(a as body.Body)" {
		t.Errorf("unexpected cast %s", got)
	}

	// `a.visible {` in a condition is a field followed by a block
	if cond, ok := fn.Body.Stmts[7].(*ast.IfStmt).Cond.(*ast.MemberExpr); !ok || cond.Name != "visible" {
		t.Errorf("expected the condition to be a field access")
	}
	if _, ok := fn.Body.Stmts[8].(*ast.WhileStmt).Cond.(*ast.MemberExpr); !ok {
		t.Errorf("expected the loop condition to be a field access")
	}
}

func TestParseExtern(t *testing.T) {
	prog := parseOK(t, `
		extern fn now() -> Int;
		pub extern fn substring(text: String, from: Int, to: Int) -> String;
		extern fn log(message: String);
		pub extern fn find(text: String, part: String) -> Int?;
		extern fn total(numbers: ...Int) -> Int;
		fn ordinary() -> Int { return now(); }
	`)

	type want struct {
		name   string
		params int
		ret    string
		pub    bool
	}
	wants := []want{
		{"now", 0, "Int", false},
		{"substring", 3, "String", true},
		{"log", 1, "", false},
		{"find", 2, "Int?", true},
		{"total", 1, "Int", false},
	}
	for i, w := range wants {
		fn := prog.Decls[i].(*ast.FnDecl)
		ret := ""
		if fn.ReturnType != nil {
			ret = fn.ReturnType.String()
		}
		if !fn.Extern || fn.Body != nil || fn.Name != w.name || len(fn.Params) != w.params || ret != w.ret || fn.Pub != w.pub {
			t.Errorf("extern %d: expected %+v, got %s (pub %v)", i, w, fn, fn.Pub)
		}
	}

	// Positioned at its name, and printed back as written
	sub := prog.Decls[1].(*ast.FnDecl)
	if sub.Token().Literal != "substring" || sub.Position().Line != 3 {
		t.Errorf("unexpected position of an extern function: %v", sub.Token())
	}
	if got := prog.Decls[0].String(); got != "extern fn now() -> Int;" {
		t.Errorf("unexpected rendering: %s", got)
	}
	if ordinary := prog.Decls[5].(*ast.FnDecl); ordinary.Extern || ordinary.Body == nil {
		t.Error("expected an ordinary function after the extern ones")
	}
}

func TestParseConstants(t *testing.T) {
	prog := parseOK(t, `
		const LIMIT = 100;
		pub const NAME: String = "lumen";
		const RATIO: Float = 1.5 * (2.0 + LIMIT);
		const NEGATIVE = -LIMIT;

		fn main() -> Int {
			const local = 1;
			return LIMIT + local;
		}
	`)

	type want struct {
		name, typ, value string
		pub              bool
	}
	wants := []want{
		{"LIMIT", "", "100", false},
		{"NAME", "String", `"lumen"`, true},
		{"RATIO", "Float", "(1.5 * (2.0 + LIMIT))", false},
		{"NEGATIVE", "", "(-LIMIT)", false},
	}
	for i, w := range wants {
		decl, ok := prog.Decls[i].(*ast.ConstDecl)
		if !ok {
			t.Fatalf("declaration %d: expected a constant, got %T", i, prog.Decls[i])
		}
		typ := ""
		if decl.Type != nil {
			typ = decl.Type.String()
		}
		if decl.Name != w.name || typ != w.typ || decl.Value.String() != w.value || decl.Pub != w.pub {
			t.Errorf("constant %d: expected %+v, got %s (pub %v)", i, w, decl, decl.Pub)
		}
	}
	// Positioned at its name, printed back as written
	if limit := prog.Decls[0].(*ast.ConstDecl); limit.Token().Literal != "LIMIT" || limit.String() != "const LIMIT = 100;" {
		t.Errorf("unexpected constant: %s at %v", limit, limit.Token())
	}
	// A constant inside a function is still a statement
	main := prog.Decls[4].(*ast.FnDecl)
	if local, ok := main.Body.Stmts[0].(*ast.LetStmt); !ok || !local.IsConst {
		t.Errorf("expected a local constant, got %T", main.Body.Stmts[0])
	}
}

func TestParseEnums(t *testing.T) {
	prog := parseOK(t, `
		enum Shape { Circle(Float), Rect(Float, Float), Empty }
		pub enum Option<T> { Some(T), None, }
		enum Json { Null, Bool(Bool), String(String), Int(Int), Arr([Json]), Fn(fn(Int) -> Json?) }
		enum Empty {}
	`)

	shape := prog.Decls[0].(*ast.EnumDecl)
	if shape.Name != "Shape" || len(shape.Variants) != 3 || shape.Pub {
		t.Fatalf("unexpected enum: %s", shape)
	}
	variants := []string{"Circle(Float)", "Rect(Float, Float)", "Empty"}
	for i, want := range variants {
		if got := shape.Variants[i].String(); got != want {
			t.Errorf("variant %d: expected %s, got %s", i, want, got)
		}
	}
	if pos := shape.Variants[1].Position(); pos.Line != 2 {
		t.Errorf("expected the variant to carry its position, got %v", pos)
	}

	option := prog.Decls[1].(*ast.EnumDecl)
	if !option.Pub || len(option.TypeParams) != 1 || option.TypeParams[0] != "T" || len(option.Variants) != 2 {
		t.Errorf("unexpected generic enum: %s", option)
	}

	// A variant may have the name of a builtin type; its values may be of any type
	json := prog.Decls[2].(*ast.EnumDecl)
	names := []string{"Null", "Bool", "String", "Int", "Arr", "Fn"}
	for i, want := range names {
		if json.Variants[i].Name != want {
			t.Errorf("variant %d: expected %s, got %s", i, want, json.Variants[i].Name)
		}
	}
	if got := json.Variants[5].Values[0].String(); got != "fn(Int) -> Json?" {
		t.Errorf("unexpected type of a value: %s", got)
	}
	if empty := prog.Decls[3].(*ast.EnumDecl); len(empty.Variants) != 0 {
		t.Errorf("expected an enum without variants, got %s", empty)
	}
}

func TestParseVariantExpressions(t *testing.T) {
	stmts := parseBody(t, `
		let a = Shape.Circle(2.0);
		let b = Shape.Empty;
		let c = Option<Int>.None;
		let d = Option<[String]>.Some([]);
		let e = Json.Bool(true);
		let f = geo.Shape.Rect(1.0, 2.0);
		let g = a < b;
		let h = Pair<Int, String>.Of(1, "x");
	`)
	value := func(i int) ast.Expr { return stmts[i].(*ast.LetStmt).Value }

	// Until the compiler knows what Shape is, these are calls and member accesses
	if call, ok := value(0).(*ast.CallExpr); !ok || call.Func.String() != "Shape.Circle" {
		t.Errorf("expected a call of Shape.Circle, got %s", value(0))
	}
	if member, ok := value(1).(*ast.MemberExpr); !ok || member.Name != "Empty" {
		t.Errorf("expected a member access, got %s", value(1))
	}
	// Type arguments before a dot belong to the name
	none, ok := value(2).(*ast.MemberExpr)
	if !ok || none.Name != "None" {
		t.Fatalf("expected a member access, got %s", value(2))
	}
	if enum, ok := none.Expr.(*ast.Ident); !ok || enum.Name != "Option" || len(enum.TypeArgs) != 1 || enum.TypeArgs[0].String() != "Int" {
		t.Errorf("expected Option<Int>, got %s", none.Expr)
	}
	if got := value(3).String(); got != "Option<[String]>.Some([])" {
		t.Errorf("unexpected rendering: %s", got)
	}
	if got := value(4).String(); got != "Json.Bool(true)" {
		t.Errorf("expected a variant named like a type to parse, got %s", got)
	}
	if got := value(5).String(); got != "geo.Shape.Rect(1.0, 2.0)" {
		t.Errorf("unexpected rendering: %s", got)
	}
	// A comparison is still a comparison
	if cmp, ok := value(6).(*ast.BinaryExpr); !ok || cmp.Op != "<" {
		t.Errorf("expected a comparison, got %s", value(6))
	}
	if got := value(7).String(); got != `Pair<Int, String>.Of(1, "x")` {
		t.Errorf("unexpected rendering: %s", got)
	}
}

func TestParseVariantPatterns(t *testing.T) {
	stmts := parseBody(t, `
		match value {
			case Shape.Empty: { }
			case Shape.Circle(r): { }
			case Shape.Rect(w, _): { }
			case geo.Shape.Rect(1.5, -2): { }
			case Expr.Add(Expr.Num(0), Expr.Neg(inner)): { }
			case Json.Str("x"): { }
			case Json.Bool(true): { }
			case Option<Int>.Some(n): { }
			case Shape.Circle(): { }
			case 3: { }
			case Point: { }
			default: { }
		}
	`)
	cases := stmts[0].(*ast.MatchStmt).Cases

	rendered := []string{
		"Shape.Empty", "Shape.Circle(r)", "Shape.Rect(w, _)", "geo.Shape.Rect(1.5, (-2))",
		"Expr.Add(Expr.Num(0), Expr.Neg(inner))", `Json.Str("x")`, "Json.Bool(true)", "Option<Int>.Some(n)", "Shape.Circle()",
	}
	for i, want := range rendered {
		pattern, ok := cases[i].Pattern.(*ast.VariantPattern)
		if !ok {
			t.Errorf("case %d: expected a variant pattern, got %T", i, cases[i].Pattern)
			continue
		}
		if pattern.String() != want {
			t.Errorf("case %d: expected %s, got %s", i, want, pattern)
		}
	}

	// With and without parentheses are told apart
	if empty := cases[0].Pattern.(*ast.VariantPattern); empty.HasArgs || len(empty.Args) != 0 {
		t.Error("expected Shape.Empty to have no parentheses")
	}
	if circle := cases[8].Pattern.(*ast.VariantPattern); !circle.HasArgs || len(circle.Args) != 0 {
		t.Error("expected Shape.Circle() to have empty parentheses")
	}

	// What each argument is
	rect := cases[2].Pattern.(*ast.VariantPattern)
	w, isBinding := rect.Args[0].(*ast.BindingPattern)
	ignored, isIgnored := rect.Args[1].(*ast.BindingPattern)
	if !isBinding || w.Name != "w" || w.Ignored() || !isIgnored || !ignored.Ignored() {
		t.Errorf("expected a name and an ignored value, got %v", rect.Args)
	}
	literals := cases[3].Pattern.(*ast.VariantPattern)
	if _, ok := literals.Args[0].(*ast.Literal); !ok {
		t.Errorf("expected a literal, got %T", literals.Args[0])
	}
	if _, ok := literals.Args[1].(*ast.UnaryExpr); !ok {
		t.Errorf("expected a negative literal, got %T", literals.Args[1])
	}
	nested := cases[4].Pattern.(*ast.VariantPattern)
	inner, ok := nested.Args[1].(*ast.VariantPattern)
	if !ok || len(inner.Args) != 1 {
		t.Fatalf("expected a pattern inside the pattern, got %T", nested.Args[1])
	}
	if name, ok := inner.Args[0].(*ast.BindingPattern); !ok || name.Name != "inner" {
		t.Errorf("expected a name in the inner pattern, got %v", inner.Args[0])
	}

	// A pattern spans its whole path, for diagnostics
	if tok := cases[3].Pattern.(*ast.VariantPattern).Token(); tok.Literal != "geo.Shape.Rect" || tok.EndPos.Col-tok.Pos.Col != len("geo.Shape.Rect") {
		t.Errorf("unexpected token of a pattern: %+v", tok)
	}
	if name := rect.Args[0].(*ast.BindingPattern); name.Position().Line != rect.Position().Line || name.Position().Col <= rect.Position().Col || name.Token().Literal != "w" {
		t.Errorf("expected the name to carry its position, got %v", name.Token())
	}

	// The other kinds of pattern are untouched
	if _, ok := cases[9].Pattern.(*ast.Literal); !ok {
		t.Errorf("expected a literal pattern, got %T", cases[9].Pattern)
	}
	if _, ok := cases[10].Pattern.(*ast.Ident); !ok {
		t.Errorf("expected a bare name to stay a name, got %T", cases[10].Pattern)
	}
	if cases[11].Pattern != nil {
		t.Errorf("expected default to have no pattern")
	}
}

func TestParseMatchValueGuardsAndAlternatives(t *testing.T) {
	// Where a value is expected, a match is an expression; its cases end
	// with ';' or ',' and the last with either or nothing
	for _, src := range []string{
		`match n { case 1: "one"; default: "many"; }`,
		`match n { case 1: "one", default: "many", }`,
		`match n { case 1: "one", default: "many" }`,
	} {
		if got, want := parseExprString(t, src), `(match n { case 1: "one" default: "many" })`; got != want {
			t.Errorf("%s: got %s, want %s", src, got, want)
		}
	}

	stmts := parseBody(t, `match shape {
		case Shape.Circle(r) if r > 10.0: { }
		case Light.Red | Light.Amber: { }
		case Int | String?: { }
		case 0 | 1..5 | 9..=12 if ok: { }
		default: { }
	}`)
	match := stmts[0].(*ast.MatchStmt)
	want := []string{
		"case Shape.Circle(r) if (r > 10.0): { }",
		"case Light.Red | Light.Amber: { }",
		"case Int | String?: { }",
		"case 0 | (1..5) | (9..=12) if ok: { }",
		"default: { }",
	}
	for i, c := range match.Cases {
		if c.String() != want[i] {
			t.Errorf("case %d: got %s, want %s", i, c.String(), want[i])
		}
	}
	// Types are told from values one pattern at a time
	if _, isType := match.Cases[2].Alternatives[0].(*ast.Type); !isType {
		t.Errorf("expected String? to be a type pattern, got %T", match.Cases[2].Alternatives[0])
	}
}

func TestParseRangesAndDefer(t *testing.T) {
	// Looser than arithmetic and comparison on both sides
	if got, want := parseExprString(t, "a + 1..b * 2"), "((a + 1)..(b * 2))"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got, want := parseExprString(t, "0..=n - 1"), "(0..=(n - 1))"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// A number before the dots is not the start of a float, and a float
	// keeps its own dot
	if got, want := parseExprString(t, "1..2.5"), "(1..2.5)"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	stmts := parseBody(t, `
		for i in 0..count { }
		defer file.close();
		defer { count++; }
	`)
	loop := stmts[0].(*ast.ForStmt)
	if _, ok := loop.Iterable.(*ast.RangeExpr); !ok {
		t.Errorf("expected a range to loop over, got %T", loop.Iterable)
	}
	for i, want := range []string{"defer { file.close(); }", "defer { (count += 1); }"} {
		if got := stmts[i+1].String(); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestParseIsPatterns(t *testing.T) {
	// What follows `is` is a pattern when it names a variant, and a type
	// when it can only be one
	patterns := []struct {
		src  string
		want string
	}{
		{"s is Shape.Empty", "(s is Shape.Empty)"},
		{"s is Shape.Circle(r)", "(s is Shape.Circle(r))"},
		{"s is Shape.Rect(w, _)", "(s is Shape.Rect(w, _))"},
		{"s is geo.Shape.Rect(1.5, -2)", "(s is geo.Shape.Rect(1.5, (-2)))"},
		{"e is Expr.Add(Expr.Num(0), Expr.Neg(inner))", "(e is Expr.Add(Expr.Num(0), Expr.Neg(inner)))"},
		{"o is Option<Int>.Some(n)", "(o is Option<Int>.Some(n))"},
		{"r is Result.Ok(_)", "(r is Result.Ok(_))"},
		{"pair is (a, b)", "(pair is __Tuple2.Of(a, b))"},
		{"pair is (true, Shape.Circle(r))", "(pair is __Tuple2.Of(true, Shape.Circle(r)))"},
		{"triple is (1, \"a\", _)", "(triple is __Tuple3.Of(1, \"a\", _))"},
		{"next() is Option.Some(v)", "(next() is Option.Some(v))"},
		{"self.items[0].shape is Shape.Empty", "(self.items[0].shape is Shape.Empty)"},
		{"load()? is Flow.Return(v)", "((load()?) is Flow.Return(v))"},
		// A type of a module reads the same; the module loader tells them apart
		{"s is geo.Shape", "(s is geo.Shape)"},
	}
	for _, tt := range patterns {
		stmts := parseBody(t, "let v = "+tt.src+";")
		value := stmts[0].(*ast.LetStmt).Value
		test, ok := value.(*ast.PatternTestExpr)
		if !ok {
			t.Errorf("%s: expected a pattern test, got %T", tt.src, value)
			continue
		}
		if got := test.String(); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.src, got, tt.want)
		}
	}

	typeTests := []struct {
		src  string
		want string
	}{
		{"v is Int", "(v is Int)"},
		{"v is Point", "(v is Point)"},
		{"v is Point?", "(v is Point?)"},
		{"v is [Shape]", "(v is [Shape])"},
		{"v is Box<Int>", "(v is Box<Int>)"},
		{"v is geo.Box<Int>", "(v is geo.Box<Int>)"},
		{"v is geo.Shape?", "(v is geo.Shape?)"},
		{"v is fn(Int) -> Int", "(v is fn(Int) -> Int)"},
		{"v is (Int, String)", "(v is __Tuple2<Int, String>)"},
		{"v is (Point, [Int])", "(v is __Tuple2<Point, [Int]>)"},
		{"v is (Point)", "(v is Point)"},
	}
	for _, tt := range typeTests {
		stmts := parseBody(t, "let v = "+tt.src+";")
		value := stmts[0].(*ast.LetStmt).Value
		test, ok := value.(*ast.TypeTestExpr)
		if !ok {
			t.Errorf("%s: expected a type test, got %T", tt.src, value)
			continue
		}
		if got := test.String(); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.src, got, tt.want)
		}
	}

	// It binds as `is` always did: tighter than any operator with two sides
	grouping := []struct {
		src  string
		want string
	}{
		{"a is S.A(x) && x > 1", "((a is S.A(x)) && (x > 1))"},
		{"ready && a is S.A(x) && b is S.B(y)", "((ready && (a is S.A(x))) && (b is S.B(y)))"},
		{"a is S.A || b is S.B(_)", "((a is S.A) || (b is S.B(_)))"},
		{"!a is S.A", "(!(a is S.A))"},
		{"a is S.A == b is S.B", "((a is S.A) == (b is S.B))"},
		{"a is S.A(_) ?? false", "((a is S.A(_)) ?? false)"},
	}
	for _, tt := range grouping {
		if got := parseExprString(t, tt.src); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.src, got, tt.want)
		}
	}
}

func TestParseIsPatternsInHeaders(t *testing.T) {
	// The brace after a pattern opens the block: a variant without values
	// is not the start of a struct literal, though `Name { }` is one elsewhere
	stmts := parseBody(t, `
		if shape is Shape.Empty { }
		if (shape is Shape.Empty) { }
		if shape is geo.Shape.Empty { } else if shape is Shape.Circle(r) && r > 1.0 { } else { }
		while queue.take() is Option.Some(job) { }
		while node is Node.Next(_, rest) && depth < 10 { }
		for i in 0..10 { if grid[i] is Cell.Wall { continue; } }
		match shape { case Shape.Circle(r) if r is Size.Big(_): { } default: { } }
		let round = shape is Shape.Circle;
		return shape is Shape.Circle(_);
	`)

	conditions := []string{
		"(shape is Shape.Empty)",
		"(shape is Shape.Empty)",
		"(shape is geo.Shape.Empty)",
	}
	for i, want := range conditions {
		if got := stmts[i].(*ast.IfStmt).Cond.String(); got != want {
			t.Errorf("statement %d: got %s, want %s", i, got, want)
		}
	}
	for i := 0; i < 3; i++ {
		if body := stmts[i].(*ast.IfStmt).Then; body == nil || len(body.Stmts) != 0 {
			t.Errorf("statement %d: expected an empty block, got %v", i, body)
		}
	}

	chain := stmts[2].(*ast.IfStmt).Else.(*ast.IfStmt)
	if got, want := chain.Cond.String(), "((shape is Shape.Circle(r)) && (r > 1.0))"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if _, ok := chain.Else.(*ast.BlockStmt); !ok {
		t.Errorf("expected a final else block, got %T", chain.Else)
	}

	loops := []string{"(queue.take() is Option.Some(job))", "((node is Node.Next(_, rest)) && (depth < 10))"}
	for i, want := range loops {
		if got := stmts[3+i].(*ast.WhileStmt).Cond.String(); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}

	guard := stmts[6].(*ast.MatchStmt).Cases[0].Guard
	if _, ok := guard.(*ast.PatternTestExpr); !ok {
		t.Errorf("expected a pattern test in the condition of the case, got %T", guard)
	}
	if _, ok := stmts[8].(*ast.ReturnStmt).Value.(*ast.PatternTestExpr); !ok {
		t.Errorf("expected a pattern test to return, got %T", stmts[8].(*ast.ReturnStmt).Value)
	}

	// The names a test would give: those of the patterns inside it too
	test := parseBody(t, "let v = e is Expr.Add(Expr.Num(a), Expr.Mul(_, Expr.Num(b)));")[0].(*ast.LetStmt).Value.(*ast.PatternTestExpr)
	var names []string
	for _, binding := range test.Pattern.Bindings() {
		names = append(names, binding.Name)
	}
	if got := strings.Join(names, " "); got != "a b" {
		t.Errorf("expected the names a b, got %s", got)
	}

	// The tests whose names reach the block: those every way to the
	// block goes through
	cond := parseBody(t, "if a is S.A(x) && (b is S.B(y) || c) && d is S.D(z) { }")[0].(*ast.IfStmt).Cond
	if got := len(ast.PatternTests(cond)); got != 2 {
		t.Errorf("expected 2 tests to reach the block, got %d", got)
	}
	if got := len(ast.Conjuncts(cond)); got != 3 {
		t.Errorf("expected a chain of 3, got %d", got)
	}
}

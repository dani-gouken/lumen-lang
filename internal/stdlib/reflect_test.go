package stdlib_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReflect(t *testing.T) {
	expectProgram(t, `
		import std.reflect;
		import std.reflect.{Kind};
		import std.list;
		import std.dict;
		import std.arrays;
		import std.json;
		import std.json.{Json};

		struct Address { city: String; zip: Int; }
		struct User {
			name: String;
			age: Int;
			home: Address;
			tags: [String];
			nick: String? = null;
			greet: fn(String) -> String;
		}
		impl User { fn older() -> Int { return self.age + 1; } }
		enum Shape { Circle(Float), Rect(Float, Float), Dot }
		struct Empty { }

		// What reflection is for: code that handles a struct it was never told about
		fn toJson(value: Any) -> Json {
			match reflect.kind(value) {
				case Kind.Null: { return Json.Null; }
				case Kind.Bool: { return Json.Bool(value as Bool); }
				case Kind.Int: { return json.int(value as Int); }
				case Kind.Float: { return Json.Number(value as Float); }
				case Kind.String: { return Json.String(value as String); }
				case Kind.Array: { return Json.Array(arrays.map(reflect.items(value), toJson)); }
				case Kind.Struct: {
					let out = json.object();
					for name in reflect.fields(value) { out.set(name, toJson(reflect.get(value, name))); }
					return out.done();
				}
				case Kind.Variant: {
					return json.object()
						.text("variant", reflect.variant(value) ?? "")
						.set("values", Json.Array(arrays.map(reflect.values(value), toJson)))
						.done();
				}
				default: { return Json.String(toString(value)); }
			}
		}

		fn main() -> Int {
			let user = User { name: "ada", age: 36, home: Address { city: "Paris", zip: 75 }, tags: ["a", "b"], greet: n -> "hi " + n };

			// What a struct value is: its fields are data, not its methods nor the functions it holds
			println(reflect.typeName(user), reflect.kind(user), reflect.fields(user));
			println(reflect.hasField(user, "age"), reflect.hasField(user, "older"), reflect.hasField(user, "greet"), reflect.hasField(3, "age"));
			println(reflect.get(user, "age"), reflect.get(user, "nope"), reflect.get(user, "nick"), reflect.get("text", "age"));

			// A value read this way has any type until it is tested
			let age = reflect.get(user, "age");
			if age is Int { println(age + 1); }
			let home = reflect.get(user, "home");
			println(reflect.typeName(home), reflect.get(home, "city"), reflect.toDict(home).keys());

			// Changing a field changes the value itself
			reflect.set(user, "age", 37);
			reflect.set(user, "nick", "countess");
			reflect.set(home, "zip", 13);
			println(user.age, user.nick, user.older(), user.home.zip);
			reflect.set(user, "nick", null);
			println(user.nick);

			// Enums, tuples, and everything else
			for shape in [Shape.Circle(1.5), Shape.Rect(2.0, 3.5), Shape.Dot] {
				println(reflect.typeName(shape), reflect.variant(shape), reflect.values(shape), reflect.kind(shape), reflect.fields(shape));
			}
			println(reflect.kind(1), reflect.kind(1.5), reflect.kind("x"), reflect.kind([1]), reflect.kind(null), reflect.kind(true), reflect.kind((1, "a")), reflect.kind(main));
			println(reflect.typeName(3), reflect.typeName(2.5), reflect.typeName("x"), reflect.typeName([1]), reflect.typeName(null), reflect.typeName(list.of([1])));
			println(reflect.values((1, "a", true)), reflect.variant(3), reflect.variant(user), reflect.values(user), reflect.fields(Empty { }));
			println(reflect.items([1, 2]), arrays.len(reflect.items(["a"])));

			println(toJson(user).stringify());
			println(toJson([Shape.Rect(1.5, 2.5), Shape.Dot]).stringify());

			println(reflect.dump(user));
			println(reflect.dump([Shape.Rect(1.5, 2.5), Shape.Dot]), reflect.dump((1, "a")), reflect.dump(Empty { }), reflect.dump([1]));
			return 0;
		}`,
		`User Struct ["age", "home", "name", "nick", "tags"]`,
		"true false false false",
		"36 null null null",
		"37",
		`Address Paris ["city", "zip"]`,
		"37 countess 38 13",
		"null",
		"Shape Circle [1.5] Variant []",
		"Shape Rect [2, 3.5] Variant []",
		"Shape Dot [] Variant []",
		"Int Float String Array Null Bool Tuple Function",
		"Int Float String array null std.list.List",
		`[1, "a", true] null null [] []`,
		"[1, 2] 1",
		`{"age":37,"home":{"city":"Paris","zip":13},"name":"ada","nick":null,"tags":["a","b"]}`,
		`[{"variant":"Rect","values":[1.5,2.5]},{"variant":"Dot","values":[]}]`,
		"User {",
		"  age: 37",
		"  home: Address {",
		`    city: "Paris"`,
		"    zip: 13",
		"  }",
		`  name: "ada"`,
		"  nick: null",
		"  tags: [",
		`    "a",`,
		`    "b"`,
		"  ]",
		"}",
		"[",
		"  Shape.Rect(1.5, 2.5),",
		"  Shape.Dot",
		`] (1, "a") Empty { } [`,
		"  1",
		"]",
	)

	prelude := "import std.reflect;\nstruct Point { x: Int; label: String; }"
	expectFailure(t, prelude, `reflect.set(Point { x: 1, label: "a" }, "x", "one");`, "the field 'x' of Point holds an Int, it cannot be given a String")
	expectFailure(t, prelude, `reflect.set(Point { x: 1, label: "a" }, "y", 2);`, "Point has no field 'y'")
	expectFailure(t, prelude, `reflect.set(3, "x", 2);`, "expected a value of a struct or of an enum, got Int")
	expectFailure(t, prelude, `reflect.items(3);`, "must be an array")
}

func TestCompilerModule(t *testing.T) {
	// The library's files are written out under the home of whoever asks
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	dir := t.TempDir()
	path := filepath.Join(dir, "shop.lum")
	source := `import std.math;

// What something costs.
struct Price { cents: Int; }

impl Price {
    fn double() -> Price { return Price { cents: self.cents * 2 }; }
}

enum Size { Small, Large(Int) }

// The price with tax.
fn taxed(price: Price, rate: Int) -> Int {
    let extra = price.cents * rate / 100;
    return math.abs(price.cents + extra);
}

fn main() -> Int {
    return taxed(Price { cents: 100 }, 20);
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, `
		import std.compiler;
		import std.compiler.{Problem, Symbol, SymbolKind, Severity};
		import std.io;
		import std.arrays;
		import std.strings;`, `
		let path = `+quote(path)+`;
		let source = io.readFile(path).expect("the file");

		// A program that is fine
		println(arrays.len(compiler.check(path, source)));

		// What it declares
		for symbol in compiler.symbols(path, source) {
			let name = symbol.name;
			if symbol.container != "" { name = "${symbol.container}.${name}"; }
			println(symbol.kind, name, "|", symbol.detail, "|", symbol.line, symbol.column);
		}

		// What a name is, and where it comes from
		let taxed = compiler.describe(path, source, 19, 12);
		println(taxed?.signature, "|", taxed?.doc, "|", taxed?.declared?.line, taxed?.declared?.column, taxed?.startColumn, taxed?.endColumn);
		println(strings.endsWith(taxed?.declared?.file ?? "", "shop.lum"), taxed?.declared?.describe() == "${path}:13:4");
		let field = compiler.describe(path, source, 14, 23);
		println(field?.signature, "|", field?.doc);
		let library = compiler.describe(path, source, 15, 14);
		// What the library declares is given a file that can be opened
		let file = library?.declared?.file ?? "";
		println(library?.signature, strings.endsWith(file, "/lumen/std/math.lum"), io.readFile(file).map(text -> strings.contains(text, "pub fn abs(")));
		println(compiler.describe(path, source, 19, 5), compiler.describe(path, source, 900, 1));

		// What could be written: the line being typed need not make sense
		let typing = strings.replace(source, "    return math.abs(price.cents + extra);", "    return price.");
		println(arrays.map(compiler.complete(path, typing, 15, 18), s -> "${s.label}:${s.kind}"));
		let plain = compiler.complete(path, source, 15, 5);
		println(arrays.map(arrays.filter(plain, s -> s.detail == "Int" || s.detail == "Price"), s -> "${s.label}: ${s.detail}"));

		// What is wrong with it
		let broken = strings.replace(source, "price.cents + extra", "price.cent + extras");
		for problem in compiler.check(path, broken) {
			println(strings.replace(problem.describe(), path, "shop.lum"), "|", problem.endColumn, problem.isError(), "|", problem.suggestion);
		}
		let unused = strings.replace(source, "    return taxed(", "    return 0;\n    taxed(");
		for problem in compiler.check(path, unused) { println(problem.severity, problem.code, problem.at.line, problem.isError()); }
		println(arrays.map(compiler.check(path, "fn main( {"), p -> "${p.severity.name()} ${p.at.line}"), arrays.len(compiler.symbols(path, "fn main( {")));

		// A file being edited is the one imported
		compiler.open(`+quote(filepath.Join(dir, "math2.lum"))+`, "pub fn triple(n: Int) -> Int { return n * 3; }");
		let user = "import math2;\nfn main() -> Int { return math2.triple(2); }\n";
		println(arrays.len(compiler.check(path, user)), arrays.map(compiler.complete(path, "import math2;\nfn main() -> Int {\n    return math2.\n}", 3, 18), s -> s.label));
		compiler.close(`+quote(filepath.Join(dir, "math2.lum"))+`);
		println(arrays.map(compiler.check(path, user), p -> p.code));
	`, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	want := `0
Struct Price |  | 4 8
Field Price.cents | Int | 4 16
Method Price.double | fn double() -> Price | 7 8
Enum Size |  | 10 6
Variant Size.Small | Small | 10 13
Variant Size.Large | Large(Int) | 10 20
Function taxed | fn taxed(price: Price, rate: Int) -> Int | 13 4
Function main | fn main() -> Int | 18 4
fn taxed(price: Price, rate: Int) -> Int | The price with tax. | 13 4 12 17
true true
(field) Price.cents: Int | What something costs.
fn math.abs(n: Int) -> Int true Ok(true)
null null
["cents:Field", "double:Method"]
["extra: Int", "price: Price", "rate: Int"]
shop.lum:15:34: error E1001: undefined identifier 'extras' | 40 true | Did you mean 'extra'?
Warning E2023 20 false
["error 1"] 0
0 ["triple"]
["E8001"]
`
	if out != want {
		t.Errorf("unexpected output:\n%s\nwant:\n%s", out, want)
	}
}

func TestReadingBytes(t *testing.T) {
	// What a protocol sends: a line saying how long, then that many
	// bytes, which is not that many characters
	out, err := run(t, "import std.io;\nimport std.strings;", `
		println(strings.byteLength("héllo"), strings.len("héllo"), strings.byteLength(""), strings.byteLength("日本"));
		let size = strings.parseInt(io.readLine() ?? "") ?? 0;
		let body = io.readBytes(size) ?? "nothing";
		println(body, strings.len(body), io.readLine(), io.readBytes(0), io.readBytes(3), io.readBytes(50), io.readBytes(1));
	`, "7\nhé\nlo!rest of the line\nabcdef")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if want := "6 5 0 6\nhé\nlo! 6 rest of the line  abc null null\n"; out != want {
		t.Errorf("unexpected output:\n%q\nwant:\n%q", out, want)
	}
	expectFailure(t, "import std.io;", `io.readBytes(-1);`, "cannot read -1 bytes at once")
}

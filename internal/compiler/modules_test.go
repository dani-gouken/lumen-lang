package compiler

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/vm"
)

// sources is a program of several files held in memory. The entry file is
// main.lum.
type sources map[string]string

func (s sources) read(path string) (string, error) {
	src, ok := s[path]
	if !ok {
		return "", fmt.Errorf("no such file: %s", path)
	}
	return src, nil
}

// compileModules compiles a program of several files.
func compileModules(fs sources) (*Result, *errors.Handler) {
	errs := errors.NewHandler()
	return CompileWith(fs["main.lum"], "main.lum", fs.read, errs), errs
}

// runModules compiles and runs a program of several files, returning what
// it printed and the runtime error, if any.
func runModules(t *testing.T, fs sources) (string, error) {
	t.Helper()

	result, errs := compileModules(fs)
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic:\n%s", d.String())
	}
	if t.Failed() {
		t.FailNow()
	}

	var out bytes.Buffer
	machine := vm.NewVM()
	machine.SetOutput(&out)
	_, err := machine.Exec(result.Chunk)
	return out.String(), err
}

func expectModulesOutput(t *testing.T, fs sources, want string) {
	t.Helper()
	out, err := runModules(t, fs)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if out != want {
		t.Errorf("unexpected output\n--- want ---\n%s--- got ---\n%s", want, out)
	}
}

// expectModulesError compiles a program that must be rejected and checks
// the first diagnostic.
func expectModulesError(t *testing.T, fs sources, code, file, msg string) {
	t.Helper()
	result, errs := compileModules(fs)
	if !errs.HasErrors() {
		t.Fatal("expected a diagnostic, got none")
	}
	if result.Chunk != nil {
		t.Error("expected no bytecode for a rejected program")
	}
	d := errs.Diagnostics[0]
	if d.Code != code || d.Pos.File != file || !strings.Contains(d.Message, msg) {
		t.Errorf("expected %s in %s containing %q, got %s in %s: %s", code, file, msg, d.Code, d.Pos.File, d.Message)
	}
	if d.Context == "" || !strings.Contains(fs[d.Pos.File], d.Context) {
		t.Errorf("expected the diagnostic to quote a line of %s, got %q", d.Pos.File, d.Context)
	}
}

// A small shop: stock, a cart built on a generic list, pricing rules given
// as traits, and a report. Each file uses the others through a different
// form of import.
var shop = sources{
	"main.lum": `
		import shop.stock;
		import shop.cart.{Cart, newCart};
		import shop.pricing as rules;
		import shop.report;

		// A rule of the program's own, implementing a trait of a module
		struct Loyalty { points: Int; }
		impl rules.Rule for Loyalty {
			fn apply(total: Int) -> Int { return total - self.points; }
			fn label() -> String { return "loyalty"; }
		}

		fn main() -> Int {
			let store = stock.open();
			store.add("apple", 30, 10);
			store.add("pear", 45, 2);
			store.add("fig", 120, 0);

			let cart: Cart = newCart(store);
			println(cart.take("apple", 4), cart.take("pear", 3), cart.take("fig", 1), cart.take("kiwi", 1));

			let applied: [rules.Rule] = [rules.percent(10), Loyalty { points: 7 }, rules.minimum(50)];
			println(report.summary(cart, applied));
			println(report.lines(cart));
			println(store.left("apple"), store.left("pear"));
			return 0;
		}
	`,
	"shop/stock.lum": `
		import util.list.{List, newList};

		pub struct Item { name: String; price: Int; count: Int; }
		pub struct Stock { items: List<Item>; }

		pub fn open() -> Stock { return Stock { items: newList<Item>() }; }

		impl Stock {
			pub fn add(name: String, price: Int, count: Int) {
				self.items.add(Item { name: name, price: price, count: count });
			}
			pub fn find(name: String) -> Item? {
				return self.items.first(fn(item: Item) -> Bool { return item.name == name; });
			}
			pub fn left(name: String) -> Int {
				let item = self.find(name);
				if item == null { return 0; }
				return item.count;
			}
		}
	`,
	"shop/cart.lum": `
		import shop.stock;
		import util.list;

		pub struct Line { item: stock.Item; count: Int; }
		pub struct Cart { store: stock.Stock; lines: list.List<Line>; }

		pub fn newCart(store: stock.Stock) -> Cart {
			return Cart { store: store, lines: list.newList<Line>() };
		}

		impl Cart {
			// Takes what the stock has; returns how many were taken
			pub fn take(name: String, wanted: Int) -> Int {
				let item = self.store.find(name);
				if item == null { return 0; }
				let count = wanted;
				if item.count < count { count = item.count; }
				if count == 0 { return 0; }
				item.count -= count;
				self.lines.add(Line { item: item, count: count });
				return count;
			}
			pub fn total() -> Int {
				return self.lines.fold<Int>(0, fn(sum: Int, line: Line) -> Int {
					return sum + line.item.price * line.count;
				});
			}
		}
	`,
	"shop/pricing.lum": `
		pub trait Rule {
			fn apply(total: Int) -> Int;
			fn label() -> String;
		}

		struct Percent { off: Int; }
		impl Rule for Percent {
			fn apply(total: Int) -> Int { return total - total * self.off / 100; }
			fn label() -> String { return toString(self.off) + "% off"; }
		}

		struct Minimum { floor: Int; }
		impl Rule for Minimum {
			fn apply(total: Int) -> Int { if total < self.floor { return self.floor; } return total; }
			fn label() -> String { return "at least " + toString(self.floor); }
		}

		// The structs stay private; only the trait leaves the module
		pub fn percent(off: Int) -> Rule { return Percent { off: off }; }
		pub fn minimum(floor: Int) -> Rule { return Minimum { floor: floor }; }
	`,
	"shop/report.lum": `
		import shop.cart.{Cart, Line};
		import shop.pricing.{Rule};
		import util.list.{List};

		pub fn summary(cart: Cart, rules: [Rule]) -> String {
			let total = cart.total();
			let text = toString(total);
			for rule in rules {
				total = rule.apply(total);
				text += " -> " + rule.label() + ": " + toString(total);
			}
			return text;
		}

		pub fn lines(cart: Cart) -> [String] {
			let names: List<String> = cart.lines.map<String>(describe);
			return names.items;
		}

		fn describe(line: Line) -> String {
			return toString(line.count) + " x " + line.item.name;
		}
	`,
	"util/list.lum": `
		pub struct List<T> { items: [T]; }

		pub fn newList<T>() -> List<T> { return List<T> { items: [] }; }

		impl<T> List<T> {
			pub fn add(value: T) { push(self.items, value); }
			pub fn size() -> Int { return self.items.length; }
			pub fn first(wanted: fn(T) -> Bool) -> T? {
				for item in self.items {
					if wanted(item) { return item; }
				}
				return null;
			}
			pub fn map<U>(f: fn(T) -> U) -> List<U> {
				let out = newList<U>();
				for item in self.items { out.add(f(item)); }
				return out;
			}
			pub fn fold<A>(start: A, f: fn(A, T) -> A) -> A {
				let acc = start;
				for item in self.items { acc = f(acc, item); }
				return acc;
			}
		}
	`,
}

func TestModulesProgram(t *testing.T) {
	// apple 4 x 30 + pear 2 x 45 = 210; 10% off = 189; -7 = 182; floor 50
	expectModulesOutput(t, shop, `4 2 0 0
210 -> 10% off: 189 -> loyalty: 182 -> at least 50: 182
["4 x apple", "2 x pear"]
6 0
`)
}

func TestModulesKeepSameNamesApart(t *testing.T) {
	// Three structs called Node, three functions called make. Which is
	// which must hold at compile time and when the program tests a value.
	expectModulesOutput(t, sources{
		"main.lum": `
			import a;
			import b;

			struct Node { own: Int; }
			fn make() -> Node { return Node { own: 3 }; }

			fn which(value: Any) -> String {
				match value {
					case a.Node: { return "a:" + toString(value.value); }
					case b.Node: { return "b:" + toString(value.size); }
					case Node: { return "main:" + toString(value.own); }
					default: { return "other"; }
				}
			}

			fn main() -> Int {
				let values: [Any] = [a.make(), b.make(), make(), 4];
				for value in values { print(which(value), ""); }
				println();

				let any: Any = b.make();
				println(any is a.Node, any is b.Node, any is Node);
				println((any as b.Node).size, a.describe(a.make()), b.describe(any));
				return 0;
			}
		`,
		"a.lum": `
			pub struct Node { value: Int; }
			pub fn make() -> Node { return Node { value: 1 }; }
			pub fn describe(value: Any) -> String { if value is Node { return "a node"; } return "not a node of a"; }
		`,
		"b.lum": `
			pub struct Node { size: Int; }
			pub fn make() -> Node { return Node { size: 2 }; }
			pub fn describe(value: Any) -> String { if value is Node { return "b node"; } return "not a node of b"; }
		`,
	}, "a:1 b:2 main:3 other \nfalse true false\n2 a node b node\n")

	// A cast to the wrong Node fails, naming both
	_, err := runModules(t, sources{
		"main.lum": `
			import a;
			import b;
			fn main() -> Int { let any: Any = b.make(); return (any as a.Node).value; }
		`,
		"a.lum": `pub struct Node { value: Int; }`,
		"b.lum": `pub struct Node { size: Int; } pub fn make() -> Node { return Node { size: 2 }; }`,
	})
	if err == nil || !strings.Contains(err.Error(), "a.Node") || !strings.Contains(err.Error(), "b.Node") {
		t.Errorf("expected a failed cast naming a.Node and b.Node, got %v", err)
	}
}

func TestModulesTypesAreChecked(t *testing.T) {
	lib := `
		pub struct Point { x: Int; }
		pub trait Shape { fn area() -> Int; }
		pub fn make(x: Int) -> Point { return Point { x: x }; }
		pub fn first<T>(items: [T]) -> T { return items[0]; }
	`
	other := `pub struct Point { y: Int; } pub fn make() -> Point { return Point { y: 1 }; }`
	entry := func(body string) sources {
		return sources{
			"main.lum":  "import lib;\nimport other;\nfn main() -> Int {\n" + body + "\nreturn 0;\n}\n",
			"lib.lum":   lib,
			"other.lum": other,
		}
	}

	tests := []struct {
		name string
		body string
		code string
		msg  string
	}{
		{"argument type", `let p = lib.make("one");`, "E2010", "cannot use type string as argument of type int"},
		{"argument count", `let p = lib.make(1, 2);`, "E2009", "expected 1 arguments, got 2"},
		{"structs of two modules", `let p: lib.Point = other.make();`, "E2001", "other.Point"},
		{"unknown field", `let p = lib.make(1); println(p.y);`, "E2014", "lib.Point has no field 'y'"},
		{"field type", `let p = lib.Point { x: "one" };`, "E2001", "string"},
		{"missing field", `let p = lib.Point {};`, "E2018", "struct literal of lib.Point is missing 'x'"},
		{"trait not implemented", `let s: lib.Shape = lib.make(1);`, "E2001", "lib.Shape"},
		{"generic result", `let s: String = lib.first([1, 2]);`, "E2001", "string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := compileModules(entry(tt.body))
			if !errs.HasErrors() {
				t.Fatal("expected a diagnostic, got none")
			}
			d := errs.Diagnostics[0]
			if d.Code != tt.code || !strings.Contains(d.Message, tt.msg) {
				t.Errorf("expected %s containing %q, got %s: %s", tt.code, tt.msg, d.Code, d.Message)
			}
		})
	}

	// The accepted counterparts
	expectModulesOutput(t, entry(`
		let p: lib.Point = lib.make(1);
		let q: other.Point = other.make();
		let r = lib.Point { x: lib.first([5, 6]) };
		println(p.x, q.y, r.x, lib.first<String>(["a"]));
	`), "1 1 5 a\n")
}

func TestModulesMethodVisibility(t *testing.T) {
	account := `
		pub struct Account { balance: Int; }
		pub trait Printable { fn text() -> String; }
		pub fn open() -> Account { return Account { balance: 0 }; }

		impl Account {
			pub fn deposit(amount: Int) -> Int {
				self.check(amount);
				self.balance += amount;
				return self.balance;
			}
			fn check(amount: Int) { if amount < 0 { self.balance = 0; } }
		}
		impl Printable for Account {
			fn text() -> String { return "balance " + toString(self.balance); }
		}
		// A private method is usable anywhere in its module, lambdas included
		pub fn audit(a: Account) -> Int {
			let run = fn() -> Int { a.check(1); return a.balance; };
			return run();
		}
	`
	entry := func(body string) sources {
		return sources{
			"main.lum": `
				import bank.account;
				import bank.account.{Account};
				impl Account {
					fn doubled() -> Int { return self.balance * 2; }
				}
				fn main() -> Int {` + body + `
					return 0;
				}`,
			"bank/account.lum": account,
			"bank/teller.lum": `
				import bank.account.{Account};
				pub fn peek(a: Account) -> Int { return a.doubled(); }
			`,
		}
	}

	// pub methods, trait methods, and the entry file's own method on a
	// struct of another module
	expectModulesOutput(t, entry(`
		let a = account.open();
		println(a.deposit(5), a.text(), a.doubled(), account.audit(a));
	`), "5 balance 5 10 5\n")

	// A method without pub stays in its module
	expectModulesError(t, entry(`let a = account.open(); a.check(1);`),
		"E2027", "main.lum", "method 'check' of bank.account.Account is private")

	// The same holds for a method the entry file added: another module
	// cannot use it
	fs := entry("")
	fs["main.lum"] = "import bank.teller;\n" + fs["main.lum"]
	expectModulesError(t, fs, "E2027", "bank/teller.lum", "method 'doubled' of bank.account.Account is private")
}

func TestModulesDiagnosticsNameTheirFile(t *testing.T) {
	// A type error deep in a module is reported in that file, with its line
	fs := sources{
		"main.lum": "import geo.area;\n\n\nfn main() -> Int { return area.square(2); }\n",
		"geo/area.lum": "import geo.units;\n" +
			"pub fn square(side: Int) -> Int {\n" +
			"    let label: Int = units.name();\n" +
			"    return side * side;\n" +
			"}\n",
		"geo/units.lum": "pub fn name() -> String { return \"cm\"; }\n",
	}
	_, errs := compileModules(fs)
	if len(errs.Diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %v", errs.Diagnostics)
	}
	d := errs.Diagnostics[0]
	if d.Code != "E2001" || d.Pos.File != "geo/area.lum" || d.Pos.Line != 3 {
		t.Errorf("expected E2001 at geo/area.lum:3, got %s at %s", d.Code, d.Pos)
	}
	if d.Context != "    let label: Int = units.name();" {
		t.Errorf("expected the line of geo/area.lum, got %q", d.Context)
	}

	// Name resolution, in another file
	expectModulesError(t, sources{
		"main.lum": "import a;\nfn main() -> Int { return a.f(); }",
		"a.lum":    "\n\npub fn f() -> Int { return undefinedThing; }",
	}, "E1001", "a.lum", "undefinedThing")

	// A missing return, in another file
	expectModulesError(t, sources{
		"main.lum": "import a;\nfn main() -> Int { return a.f(1); }",
		"a.lum":    "pub fn f(n: Int) -> Int {\n    if n > 0 { return 1; }\n}",
	}, "E2004", "a.lum", "function 'a.f' can reach its end without returning a value")
}

func TestModulesRuntimeErrorsNameTheirFile(t *testing.T) {
	_, err := runModules(t, sources{
		"main.lum": `
			import data.table;
			fn main() -> Int {
				let t = table.of([1, 2, 3]);
				println(t.at(1));
				return t.at(7);
			}
		`,
		"data/table.lum": "pub struct Table { cells: [Int]; }\n" +
			"pub fn of(cells: [Int]) -> Table { return Table { cells: cells }; }\n" +
			"impl Table {\n" +
			"    pub fn at(i: Int) -> Int {\n" +
			"        return self.cells[i];\n" +
			"    }\n" +
			"}\n",
	})
	if err == nil {
		t.Fatal("expected an out-of-bounds error")
	}
	for _, want := range []string{"index out of bounds: 7 (length 3)", "data/table.lum:5", "return self.cells[i];", "data.table.Table.at"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to contain %q, got:\n%v", want, err)
		}
	}
}

func TestModulesEntryPoint(t *testing.T) {
	// A module may have a function called main; the program starts in the
	// entry file's
	expectModulesOutput(t, sources{
		"main.lum": `
			import tool;
			fn main() -> Int { println("entry", tool.main()); return 0; }
		`,
		"tool.lum": `pub fn main() -> Int { return 7; }`,
	}, "entry 7\n")

	// ...and a module's main does not make up for a missing one
	_, errs := compileModules(sources{
		"main.lum": "import tool;\nfn helper() -> Int { return tool.main(); }",
		"tool.lum": `pub fn main() -> Int { return 7; }`,
	})
	if len(errs.Diagnostics) != 1 || errs.Diagnostics[0].Code != "E6006" {
		t.Errorf("expected the program to lack an entry point, got %v", errs.Diagnostics)
	}
}

func TestModulesFunctionsAreNamedInBytecode(t *testing.T) {
	result, errs := compileModules(sources{
		"main.lum":     "import geo.area;\nfn main() -> Int { return area.square(3); }",
		"geo/area.lum": "pub fn square(side: Int) -> Int { return side * side; }",
	})
	if errs.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", errs.Diagnostics)
	}

	idx := result.Chunk.FindPrototype("geo.area.square")
	if idx < 0 {
		t.Fatal("expected the function to be named geo.area.square in the bytecode")
	}
	info := result.Chunk.GetDebugInfo(result.Chunk.Prototypes[idx].CodeStart)
	if info == nil || info.FilePath != "geo/area.lum" || !strings.Contains(info.SourceLine, "side * side") {
		t.Errorf("expected debug info pointing into geo/area.lum, got %+v", info)
	}

	value, err := vm.NewVM().Exec(result.Chunk)
	if err != nil || !value.IsInt() || value.AsInt() != 9 {
		t.Errorf("expected 9, got %s, %v", vm.FormatValue(value), err)
	}
}

func TestModulesUnusedModuleStillChecked(t *testing.T) {
	// A module that is imported is compiled whole, used or not
	expectModulesError(t, sources{
		"main.lum": "import a;\nfn main() -> Int { return 0; }",
		"a.lum":    "pub fn f() -> Int { return \"text\"; }",
	}, "E2003", "a.lum", "cannot return type string")
}

func TestModulesImplementationsAreGlobal(t *testing.T) {
	lib := `
		pub struct Point { x: Int; }
		pub trait Named { fn name() -> String; }
		impl Point { pub fn double() -> Int { return self.x * 2; } }
		impl Named for Point { fn name() -> String { return "point"; } }
		pub fn make() -> Point { return Point { x: 2 }; }
	`
	// A second method of the same name, from another module
	_, errs := compileModules(sources{
		"main.lum": "import lib;\nimpl lib.Point { fn double() -> Int { return 0; } }\nfn main() -> Int { return 0; }",
		"lib.lum":  lib,
	})
	if !errs.HasErrors() || errs.Diagnostics[0].Code != "E2019" {
		t.Errorf("expected a duplicate method error, got %v", errs.Diagnostics)
	}

	// A method added by another module, and a trait of the program
	// implemented for a struct of a module
	expectModulesOutput(t, sources{
		"main.lum": `
			import lib;
			trait Sized { fn size() -> Int; }
			impl Sized for lib.Point { fn size() -> Int { return self.x + self.triple(); } }
			impl lib.Point { fn triple() -> Int { return self.x * 3; } }
			fn main() -> Int {
				let p = lib.make();
				let s: Sized = p;
				let n: lib.Named = p;
				println(p.double(), p.triple(), s.size(), n.name());
				return 0;
			}
		`,
		"lib.lum": lib,
	}, "4 6 8 point\n")
}

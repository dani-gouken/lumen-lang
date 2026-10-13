package stdlib_test

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/parser"
	"github.com/dani-gouken/lumen/internal/stdlib"
	"github.com/dani-gouken/lumen/internal/vm"
)

// run compiles and runs a program that uses the library. imports are the
// import lines, body the content of main. input is what readLine reads.
func run(t *testing.T, imports, body, input string) (string, error) {
	t.Helper()
	src := imports + "\nfn main() -> Int {\n" + body + "\nreturn 0;\n}\n"

	errs := errors.NewHandler()
	result := compiler.Compile(src, "test.lum", errs)
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic:\n%s", d.String())
	}
	if t.Failed() {
		t.FailNow()
	}

	var out bytes.Buffer
	machine := vm.NewVM()
	machine.SetOutput(&out)
	machine.SetInput(strings.NewReader(input))
	_, err := machine.Exec(result.Chunk)
	return out.String(), err
}

// expect runs a program and compares what it prints, line by line.
func expect(t *testing.T, imports, body string, want ...string) {
	t.Helper()
	out, err := run(t, imports, body, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	for i := 0; i < len(got) || i < len(want); i++ {
		g, w := "<nothing>", "<nothing>"
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if g != w {
			t.Errorf("line %d:\n  want %s\n   got %s", i+1, w, g)
		}
	}
}

// expectFailure runs a program that must stop with a runtime error.
func expectFailure(t *testing.T, imports, body, want string) {
	t.Helper()
	_, err := run(t, imports, body, "")
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("expected a runtime error containing %q, got %v", want, err)
	}
}

func TestModulesAreEmbedded(t *testing.T) {
	want := []string{"std.arrays", "std.io", "std.list", "std.map", "std.math", "std.option", "std.random", "std.strings", "std.time"}
	if got := stdlib.Modules(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("expected the modules %v, got %v", want, got)
	}

	src, err := stdlib.Read(stdlib.Root + "/math.lum")
	if err != nil || !strings.Contains(src, "pub fn abs(") {
		t.Errorf("expected the source of std.math, got %v", err)
	}
	if _, err := stdlib.Read(stdlib.Root + "/nothing.lum"); err == nil {
		t.Error("expected an error for a module that does not exist")
	}
	// Nothing outside the library can be read through it
	for _, path := range []string{stdlib.Root + "/../stdlib.go", stdlib.Root + "/sub/math.lum", "math.lum"} {
		if _, err := stdlib.Read(path); err == nil {
			t.Errorf("expected %s not to be readable", path)
		}
	}
	if !stdlib.IsPath(stdlib.Root+"/math.lum") || stdlib.IsPath("std/math.lum") || stdlib.IsPath("math.lum") {
		t.Error("IsPath must recognize the library's files and nothing else")
	}
}

// TestExternsMatchNatives checks the two halves of the library against
// each other: every `extern fn` of a module needs a native of that name in
// the virtual machine, and a native nobody declares is dead code.
func TestExternsMatchNatives(t *testing.T) {
	declared := make(map[string]*ast.FnDecl)
	for _, module := range stdlib.Modules() {
		file := stdlib.Root + "/" + strings.TrimPrefix(module, "std.") + ".lum"
		src, err := stdlib.Read(file)
		if err != nil {
			t.Fatal(err)
		}
		errs := errors.NewHandler()
		prog := parser.Parse(src, file, errs)
		for _, d := range errs.Diagnostics {
			t.Errorf("%s does not parse: %s", file, d.Message)
		}
		for _, decl := range prog.Decls {
			if fn, ok := decl.(*ast.FnDecl); ok && fn.Extern {
				declared[module+"."+fn.Name] = fn
			}
		}
	}

	for name, fn := range declared {
		if !vm.IsNative(name) {
			t.Errorf("%s is declared with extern fn but the virtual machine has no such native", name)
		}
		if !fn.Pub {
			t.Errorf("%s is an extern fn without pub: nothing can call it", name)
		}
	}
	for _, name := range vm.NativeNames() {
		if declared[name] == nil {
			t.Errorf("the native %s is not declared by any module of the library", name)
		}
	}
}

// TestEveryModuleCompiles imports the whole library into one program. A
// module is compiled whole when it is imported, so this type checks every
// function of the library, called by a test below or not.
func TestEveryModuleCompiles(t *testing.T) {
	var imports []string
	for _, module := range stdlib.Modules() {
		imports = append(imports, "import "+module+";")
	}
	sort.Strings(imports)
	expect(t, strings.Join(imports, "\n"), `println("compiled");`, "compiled")
}

func TestMath(t *testing.T) {
	expect(t, "import std.math;", `
		println(math.abs(-4), math.abs(4), math.abs(0));
		println(math.min(3, 9), math.min(9, 3), math.max(3, 9), math.max(-1, -2));
		println(math.clamp(15, 0, 10), math.clamp(-5, 0, 10), math.clamp(5, 0, 10));
		println(math.sign(-9), math.sign(0), math.sign(9));
		println(math.pow(2, 10), math.pow(-3, 3), math.pow(7, 0), math.pow(0, 0), math.pow(10, 14));
		println(math.pow(2, -1), math.pow(1, -5), math.pow(-1, -3), math.pow(-1, -4));
		println(math.gcd(12, 18), math.gcd(-12, 18), math.gcd(7, 0), math.gcd(0, 0), math.gcd(17, 5));
		println(math.lcm(4, 6), math.lcm(-4, 6), math.lcm(0, 6), math.lcm(7, 7));
		println(math.isEven(4), math.isEven(-3), math.isOdd(-3), math.isOdd(0));

		println(math.toFloat(3) / 2.0, math.floor(2.7), math.ceil(2.1), math.round(2.5), math.trunc(-2.7));
		println(math.sqrt(16.0), math.powf(2.0, 0.5), math.exp(0.0), math.log(math.e));
		println(math.fabs(-1.5), math.fmin(1.5, 2.5), math.fmax(1.5, 2.5));
		println(math.round(math.sin(math.pi / 2.0) * 1000.0), math.round(math.cos(math.pi) * 1000.0));
		println(math.degrees(math.pi), math.round(math.radians(180.0) * 1000.0), math.round(math.atan2(1.0, 1.0) * 4000.0));

		// The hypotenuse of a 3-4-5 triangle, through Int and back
		let a = 3;
		let b = 4;
		println(math.round(math.sqrt(math.toFloat(a * a + b * b))));
	`,
		"4 4 0",
		"3 3 9 -1",
		"10 0 5",
		"-1 0 1",
		"1024 -27 1 1 100000000000000",
		"0 1 -1 1",
		"6 6 7 0 1",
		"12 12 0 7",
		"true false true false",
		"1.5 2 3 3 -2",
		"4 1.4142135623730951 1 1",
		"1.5 1.5 2.5",
		"1000 -1000",
		"180 3142 3142",
		"5",
	)

	expectFailure(t, "import std.math;", `println(math.sqrt(-4.0));`, "std.math.sqrt: the square root of -4 is not a number")
	expectFailure(t, "import std.math;", `println(math.log(0.0));`, "std.math.log: the logarithm of 0 is not a number")
	expectFailure(t, "import std.math;", `println(math.floor(1.0e300));`, "std.math.floor: 1e+300 does not fit in an Int")
	// Arithmetic in the library overflows like arithmetic anywhere
	expectFailure(t, "import std.math;", `println(math.pow(10, 30));`, "integer overflow")
}

func TestStrings(t *testing.T) {
	expect(t, "import std.strings;", `
		println(strings.substring("lumen", 1, 3), strings.from("lumen", 2), strings.first("lumen", 2), strings.last("lumen", 2));
		println("[" + strings.first("ab", 9) + "]", "[" + strings.last("ab", 9) + "]", "[" + strings.first("ab", -1) + "]", "[" + strings.from("ab", 2) + "]");
		println(strings.split("a,b,,c", ","), strings.chars("héλ"), strings.split("", ","));
		println(strings.lines("x\ny\n"), strings.lines("x\r\ny"), strings.lines(""), strings.lines("\n\n"));
		println(strings.words("  a  b\tc\nd "), strings.words(""), strings.words("   "));
		println(strings.join(["a", "b", "c"], "-"), "[" + strings.join([], "-") + "]", strings.repeat("ab", 3), "[" + strings.repeat("ab", 0) + "]");
		println("[" + strings.padLeft("7", 3) + "]", "[" + strings.padRight("7", 3) + "]", "[" + strings.padLeft("1234", 3) + "]", "[" + strings.padRight("", 2) + "]");
		println(strings.reverse("abc"), strings.reverse("héλ"), "[" + strings.reverse("") + "]");
		println(strings.indexOf("banana", "na"), strings.lastIndexOf("banana", "na"), strings.indexOf("banana", "x"), strings.indexOf("héλlo", "l"));
		println(strings.contains("banana", "nan"), strings.contains("banana", "nab"), strings.contains("banana", ""));
		println(strings.startsWith("banana", "ban"), strings.startsWith("ban", "banana"), strings.startsWith("banana", ""), strings.startsWith("banana", "an"));
		println(strings.endsWith("banana", "na"), strings.endsWith("na", "banana"), strings.endsWith("banana", ""), strings.endsWith("banana", "an"));
		println(strings.count("banana", "a"), strings.count("banana", "an"), strings.count("aaaa", "aa"), strings.count("banana", "x"), strings.count("banana", ""));
		println(strings.replace("a-b-c", "-", "+"), strings.upper("école"), strings.lower("ÉCOLE"), "[" + strings.trim(" \t x y \n") + "]");
		println(strings.capitalize("ada"), strings.capitalize("élan"), "[" + strings.capitalize("") + "]", strings.capitalize("A"));
		println(strings.isEmpty(""), strings.isEmpty(" "), strings.isBlank(" \t\n"), strings.isBlank(" x "));
		println(strings.isDigits("123"), strings.isDigits("12a"), strings.isDigits(""), strings.isDigits("-1"));
		println(strings.parseInt("42"), strings.parseInt(" -7 "), strings.parseInt("4x"), strings.parseInt(""), strings.parseFloat("2.5"), strings.parseFloat("x"));
		println(strings.code("A"), strings.fromCode(955), strings.fromCode(strings.code("z") - 25));
		println(strings.hash("ada") == strings.hash("ada"), strings.hash("ada") == strings.hash("adb"), strings.hash("") >= 0);

		// What a parse gives is an optional: it has to be tested before use
		let total = 0;
		for text in ["10", "x", "32", ""] {
			let n = strings.parseInt(text);
			if n != null { total += n; }
		}
		println(total, strings.parseInt("oops") ?? -1);

		// The position found by a search, used to cut the text
		let line = "name=ada lovelace";
		let at = strings.indexOf(line, "=");
		if at != null {
			println(strings.first(line, at), "/", strings.from(line, at + 1));
		}
	`,
		"um men lu en",
		"[ab] [ab] [] []",
		`["a", "b", "", "c"] ["h", "é", "λ"] [""]`,
		`["x", "y"] ["x", "y"] [] ["", ""]`,
		`["a", "b", "c", "d"] [] []`,
		"a-b-c [] ababab []",
		"[  7] [7  ] [1234] [  ]",
		"cba λéh []",
		"2 4 null 3",
		"true false true",
		"true false true false",
		"true false true false",
		"3 2 2 0 0",
		"a+b+c ÉCOLE école [x y]",
		"Ada Élan [] A",
		"true false true false",
		"true false false false",
		"42 -7 null null 2.5 null",
		"65 λ a",
		"true false true",
		"42 -1",
		"name / ada lovelace",
	)

	expectFailure(t, "import std.strings;", `println(strings.substring("abc", 2, 9));`, "std.strings.substring: range 2..9 is outside a text of 3 characters")
	expectFailure(t, "import std.strings;", `println(strings.from("abc", 4));`, "range 4..3 is outside")
	expectFailure(t, "import std.strings;", `println(strings.replace("abc", "", "x"));`, "the text to replace is empty")
	expectFailure(t, "import std.strings;", `println(strings.repeat("ab", -1));`, "cannot repeat a text -1 times")
	expectFailure(t, "import std.strings;", `println(strings.code(""));`, "an empty text has no first character")
}

func TestArrays(t *testing.T) {
	expect(t, "import std.arrays;\nstruct City { name: String; people: Int; }", `
		let n = arrays.range(1, 8);
		println(n, arrays.range(3, 3), arrays.range(5, 2), arrays.range(-2, 1));
		println(arrays.fill(3, "x"), arrays.fill(0, 1), arrays.fill(2, [1]));
		println(arrays.slice(n, 2, 4), arrays.slice(n, -3, 2), arrays.slice(n, 5, 99), arrays.slice(n, 4, 2));
		println(arrays.concat([1], [2, 3]), arrays.concat<Int>([], []), arrays.reversed(n), arrays.reversed<Int>([]));
		println(arrays.without(n, 0), arrays.without(n, 6), arrays.without([1, 2], 9), arrays.without([1, 2], -1));
		println(arrays.inserted([1, 3], 1, 2), arrays.inserted([1, 2], 0, 0), arrays.inserted([1, 2], 2, 3), arrays.inserted([1, 2], 9, 3), arrays.inserted<Int>([], 0, 1));
		println(arrays.first(n), arrays.last(n), arrays.first<Int>([]), arrays.last<Int>([]), arrays.at(n, 2), arrays.at(n, 7), arrays.at(n, -1));
		println(arrays.isEmpty(n), arrays.isEmpty<String>([]));

		let even = fn(x: Int) -> Bool { return x % 2 == 0; };
		let big = fn(x: Int) -> Bool { return x > 100; };
		println(arrays.filter(n, even), arrays.filter(n, big), arrays.count(n, even), arrays.count(n, big));
		println(arrays.any(n, even), arrays.any(n, big), arrays.all(n, even), arrays.all(n, fn(x: Int) -> Bool { return x > 0; }), arrays.all<Int>([], big));
		println(arrays.find(n, even), arrays.find(n, big), arrays.findIndex(n, even), arrays.findIndex(n, big));
		println(arrays.map(n, fn(x: Int) -> String { return "#" + toString(x); }), arrays.map(["a", "bcd"], fn(s: String) -> Int { return s.length; }));
		println(arrays.fold(n, 0, fn(a: Int, x: Int) -> Int { return a + x; }), arrays.fold(["a", "b"], "", fn(a: String, s: String) -> String { return s + a; }), arrays.fold<Int, Int>([], 7, fn(a: Int, x: Int) -> Int { return a + x; }));

		// each calls a function for its effect
		let seen: [Int] = [];
		arrays.each(n, fn(x: Int) -> Void { push(seen, x * x); });
		println(seen);

		println(arrays.sum(n), arrays.sum([]), arrays.smallest([3, 1, 2]), arrays.largest([3, 1, 2]), arrays.smallest([]), arrays.largest([]));
		println(arrays.sortedNumbers([3, 1, 2, 1, -5]), arrays.sortedTexts(["pear", "Apple", "fig"]), arrays.sortedNumbers([]));
		println(arrays.contains(n, 3), arrays.contains(n, 30), arrays.contains(["a"], "a"), arrays.contains(["a"], "b"), arrays.contains([true], false), arrays.contains<Int>([], 1));
		println(arrays.indexOf(n, 3), arrays.indexOf(n, 30), arrays.indexOf(["a", "b", "b"], "b"), arrays.indexOf([1.5, 2.5], 2.5));
		// An array or a struct is only equal to itself
		let row = [1, 2];
		let rows = [[1, 2], row];
		println(arrays.indexOf(rows, row), arrays.contains(rows, [1, 2]));

		// Sorting structs, by one field then stably by another
		let cities = [City { name: "Lagos", people: 15 }, City { name: "Oslo", people: 1 }, City { name: "Lima", people: 10 }, City { name: "Bern", people: 1 }];
		let name = fn(c: City) -> String { return c.name; };
		let bySize = arrays.sorted(cities, fn(a: City, b: City) -> Bool { return a.people < b.people; });
		println(arrays.map(bySize, name), arrays.map(arrays.sorted(cities, fn(a: City, b: City) -> Bool { return a.name < b.name; }), name));
		// The original is untouched by everything above
		println(arrays.map(cities, name), n);

		// swap and copy: one changes its array, the other protects it
		let original = [1, 2, 3];
		let duplicate = arrays.copy(original);
		arrays.swap(duplicate, 0, 2);
		println(original, duplicate);

		// A sort of many items, checked against the sum and the order
		let many: [Int] = [];
		let i = 0;
		while i < 2000 { push(many, (i * 7919) % 2003); i++; }
		let ordered = arrays.sortedNumbers(many);
		let inOrder = true;
		for j, v in ordered {
			if j > 0 && ordered[j - 1] > v { inOrder = false; }
		}
		println(ordered.length, inOrder, arrays.sum(ordered) == arrays.sum(many), ordered[0], ordered[1999]);
	`,
		"[1, 2, 3, 4, 5, 6, 7] [] [] [-2, -1, 0]",
		`["x", "x", "x"] [] [[1], [1]]`,
		"[3, 4] [1, 2] [6, 7] []",
		"[1, 2, 3] [] [7, 6, 5, 4, 3, 2, 1] []",
		"[2, 3, 4, 5, 6, 7] [1, 2, 3, 4, 5, 6] [1, 2] [1, 2]",
		"[1, 2, 3] [0, 1, 2] [1, 2, 3] [1, 2, 3] [1]",
		"1 7 null null 3 null null",
		"false true",
		"[2, 4, 6] [] 3 0",
		"true false false true true",
		"2 null 1 null",
		`["#1", "#2", "#3", "#4", "#5", "#6", "#7"] [1, 3]`,
		"28 ba 7",
		"[1, 4, 9, 16, 25, 36, 49]",
		"28 0 1 3 null null",
		`[-5, 1, 1, 2, 3] ["Apple", "fig", "pear"] []`,
		"true false true false false false",
		"2 null 1 1",
		"1 false",
		`["Oslo", "Bern", "Lima", "Lagos"] ["Bern", "Lagos", "Lima", "Oslo"]`,
		`["Lagos", "Oslo", "Lima", "Bern"] [1, 2, 3, 4, 5, 6, 7]`,
		"[1, 2, 3] [3, 2, 1]",
		"2000 true true 0 2002",
	)

	expectFailure(t, "import std.arrays;", `arrays.swap([1, 2], 0, 5);`, "index out of bounds: 5 (length 2)")
}

func TestList(t *testing.T) {
	expect(t, "import std.list;\nimport std.list.{List};\nstruct City { name: String; people: Int; }", `
		let names = list.empty<String>();
		println(names.size(), names.isEmpty(), names.first(), names.last(), names.pop(), names.get(0));

		names.add("x");
		names.addAll(["y", "z"]);
		names.insert(1, "w");
		println(names.toArray(), names.size(), names.isEmpty(), names.first(), names.last(), names.get(1), names.get(9), names.get(-1));
		names.set(0, "a");
		println(names.pop(), names.removeAt(0), names.removeAt(7), names.toArray());
		names.insert(99, "end");
		names.insert(0, "start");
		println(names.toArray());
		names.clear();
		println(names.toArray(), names.isEmpty());

		// A list has its own items: the array it came from is not shared
		let source = [3, 1, 2];
		let numbers = list.of(source);
		numbers.add(0);
		let exported = numbers.toArray();
		push(exported, 99);
		println(source, numbers.toArray(), exported);

		// Calls that return a list can be chained
		let even = fn(n: Int) -> Bool { return n % 2 == 0; };
		let up = fn(a: Int, b: Int) -> Bool { return a < b; };
		println(numbers.sorted(up).toArray(), numbers.reversed().toArray(), numbers.slice(1, 3).toArray(), numbers.filter(even).toArray());
		println(numbers.filter(even).map<String>(fn(n: Int) -> String { return "<" + toString(n) + ">"; }).reversed().toArray());
		println(numbers.fold<Int>(0, fn(a: Int, n: Int) -> Int { return a + n; }), numbers.count(even), numbers.any(even), numbers.all(even));
		println(numbers.find(even), numbers.findIndex(even), numbers.find(fn(n: Int) -> Bool { return n > 9; }));
		println(numbers.contains(2), numbers.contains(7), numbers.indexOf(1), numbers.indexOf(7));
		// Type arguments of map and fold can be left to be worked out
		println(numbers.map(fn(n: Int) -> String { return toString(n) + "!"; }).toArray(), numbers.fold(0, fn(a: Int, n: Int) -> Int { return a + n; }));
		// ...and none of them changed the list; sort does
		println(numbers.toArray());
		numbers.sort(up);
		println(numbers.toArray());

		let total = 0;
		numbers.each(fn(n: Int) -> Void { total += n; });
		println(total);

		// Structs, and a list as a parameter and a field type
		let cities: List<City> = list.of([City { name: "Lagos", people: 15 }, City { name: "Oslo", people: 1 }]);
		cities.add(City { name: "Lima", people: 10 });
		cities.sort(fn(a: City, b: City) -> Bool { return a.people > b.people; });
		let largest = cities.first();
		if largest != null { println(largest.name, cities.map<String>(fn(c: City) -> String { return c.name; }).toArray()); }
	`,
		"0 true null null null null",
		`["x", "w", "y", "z"] 4 false x z w null null`,
		`z a null ["w", "y"]`,
		`["start", "w", "y", "end"]`,
		"[] true",
		"[3, 1, 2] [3, 1, 2, 0] [3, 1, 2, 0, 99]",
		"[0, 1, 2, 3] [0, 2, 1, 3] [1, 2] [2, 0]",
		`["<0>", "<2>"]`,
		"6 2 true false",
		"2 2 null",
		"true false 1 null",
		`["3!", "1!", "2!", "0!"] 6`,
		"[3, 1, 2, 0]",
		"[0, 1, 2, 3]",
		"6",
		`Lagos ["Lagos", "Lima", "Oslo"]`,
	)

	expectFailure(t, "import std.list;", `let l = list.of([1]); l.set(3, 9);`, "index out of bounds")
}

func TestMap(t *testing.T) {
	expect(t, "import std.map;\nimport std.map.{Map};\nstruct Point { x: Int; y: Int; }", `
		let ages = map.empty<Int>();
		println(ages.size(), ages.isEmpty(), ages.get("ada"), ages.has("ada"), ages.remove("ada"), ages.keys(), ages.values());

		ages.set("ada", 36);
		ages.set("linus", 21);
		ages.set("grace", 85);
		ages.set("ada", 37);          // Replaces: still three keys
		println(ages.size(), ages.isEmpty(), ages.get("ada"), ages.get("zed"), ages.has("linus"), ages.has("Linus"));
		println(ages.keys(), ages.values());
		println(ages.get("zed") ?? -1, (ages.get("ada") ?? 0) + 1);

		println(ages.remove("linus"), ages.remove("linus"), ages.size(), ages.keys(), ages.get("linus"));
		// A key set again after being removed goes to the end
		ages.set("linus", 22);
		println(ages.keys(), ages.values());

		// Keys that are alike are different keys; "" is a key too
		let odd = map.empty<String>();
		odd.set("", "empty");
		odd.set(" ", "space");
		odd.set("a", "lower");
		odd.set("A", "upper");
		println(odd.size(), odd.get(""), odd.get(" "), odd.get("a"), odd.get("A"));

		// Many keys: the table grows several times, removed keys drop out
		let big = map.empty<Int>();
		let i = 0;
		while i < 3000 { big.set("key" + toString(i), i * 2); i++; }
		i = 0;
		while i < 3000 { if i % 3 == 0 { big.remove("key" + toString(i)); } i++; }
		while i < 3500 { big.set("key" + toString(i), i * 2); i++; }
		println(big.size(), big.get("key0"), big.get("key1"), big.get("key2999"), big.get("key3499"), big.keys().length, big.values().length);
		let sum = 0;
		big.each(fn(key: String, value: Int) -> Void { sum += value; });
		let expected = 0;
		i = 0;
		while i < 3500 { if i >= 3000 || i % 3 != 0 { expected += i * 2; } i++; }
		println(sum == expected, big.keys()[0], big.keys()[1]);

		// Counting with a map
		let counts = map.empty<Int>();
		for word in ["to", "be", "or", "not", "to", "be"] {
			counts.set(word, (counts.get(word) ?? 0) + 1);
		}
		println(counts.keys(), counts.values());

		// Values of any type: structs, arrays, other maps
		let points: Map<Point> = map.empty<Point>();
		points.set("origin", Point { x: 0, y: 0 });
		points.set("far", Point { x: 9, y: 4 });
		let far = points.get("far");
		if far != null { println(far.x + far.y); }
		let groups = map.empty<[String]>();
		groups.set("fruit", ["fig"]);
		let fruit = groups.get("fruit");
		if fruit != null { push(fruit, "pear"); }
		println(groups.get("fruit"), groups.get("meat"));
		let nested = map.empty<Map<Int>>();
		nested.set("inner", counts);
		let inner = nested.get("inner");
		if inner != null { println(inner.get("to")); }
	`,
		"0 true null false false [] []",
		"3 false 37 null true false",
		`["ada", "linus", "grace"] [37, 21, 85]`,
		"-1 38",
		`true false 2 ["ada", "grace"] null`,
		`["ada", "grace", "linus"] [37, 85, 22]`,
		"4 empty space lower upper",
		"2500 null 2 5998 6998 2500 2500",
		"true key1 key2",
		`["to", "be", "or", "not"] [2, 2, 1, 1]`,
		"13",
		`["fig", "pear"] null`,
		"2",
	)
}

func TestOption(t *testing.T) {
	expect(t, `import std.option;
import std.strings;
import std.arrays;
import std.map;

struct User { name: String; manager: String?; }

fn half(n: Int) -> Int? { if n % 2 != 0 { return null; } return n / 2; }
fn double(n: Int) -> Int { return n * 2; }`, `
		// map: a step that always has an answer
		println(option.map(strings.parseInt("21"), double), option.map(strings.parseInt("x"), double), option.map(half(8), toString));
		// then: a step that may have none; nothing at any point is nothing at the end
		println(option.then(strings.parseInt("84"), half), option.then(strings.parseInt("7"), half), option.then(strings.parseInt("x"), half));
		println(option.then(option.then(strings.parseInt("84"), half), half), option.then(option.then(strings.parseInt("6"), half), half));
		// A step changes the type
		println(option.map(option.then(strings.parseInt("84"), half), fn(n: Int) -> String { return "<" + toString(n) + ">"; }));

		let big = fn(n: Int) -> Bool { return n > 3; };
		println(option.filter(half(10), big), option.filter(half(4), big), option.filter(half(3), big));
		println(option.mapOr(half(10), "none", toString), option.mapOr(half(3), "none", toString), option.mapOr(half(8), 0, double), option.mapOr(half(7), 0, double));
		println(option.or(half(3), half(4)), option.or(half(2), half(4)), option.or(half(3), half(5)), option.or(half(3), 9));

		// orElse only works out the fallback when it is needed
		let calls = 0;
		let fallback = fn() -> Int { calls++; return -1; };
		let present = option.orElse(half(4), fallback);
		let before = calls;
		let absent = option.orElse(half(5), fallback);
		println(present, before, absent, calls);

		let seen: [Int] = [];
		option.each(half(6), fn(n: Int) -> Void { push(seen, n); });
		option.each(half(7), fn(n: Int) -> Void { push(seen, n); });
		println(seen, option.isSome(half(2)), option.isSome(half(1)), option.isNone(half(1)), option.isNone(half(2)));

		println(option.values([half(2), half(3), half(4)]), option.values(arrays.map(["1", "x", "3"], strings.parseInt)), option.values<Int>([]));

		// What comes out is an ordinary optional: ?? and null tests apply
		let n: Int = option.map(strings.parseInt("5"), double) ?? 0;
		let m = option.then(strings.parseInt("9"), half);
		if m == null { println(n + 1, "no half"); }

		// Looking something up through two maps and a field that may be null
		let users = map.empty<User>();
		users.set("ada", User { name: "Ada", manager: "grace" });
		users.set("grace", User { name: "Grace", manager: null });
		users.set("bob", User { name: "Bob", manager: "gone" });
		let managerOf = fn(key: String) -> String? {
			let manager = option.then(users.get(key), fn(u: User) -> String? { return u.manager; });
			let found = option.then(manager, fn(k: String) -> User? { return users.get(k); });
			return option.map(found, fn(u: User) -> String { return u.name; });
		};
		println(managerOf("ada"), managerOf("grace"), managerOf("bob"), managerOf("nobody"), managerOf("ada") ?? "no manager");

		// With structs and arrays as the values
		let first = option.map(arrays.first([User { name: "Zed", manager: null }]), fn(u: User) -> String { return u.name; });
		let none = option.map(arrays.first<User>([]), fn(u: User) -> String { return u.name; });
		println(first, none, option.mapOr(arrays.last([[1, 2], [3]]), 0, fn(row: [Int]) -> Int { return row.length; }));
	`,
		"42 null 4",
		"42 null null",
		"21 null",
		"<42>",
		"5 null null",
		"5 none 8 0",
		"2 1 null 9",
		"2 0 -1 1",
		"[3] true false true false",
		"[1, 2] [1, 3] []",
		"11 no half",
		"Grace null null null Grace",
		"Zed null 1",
	)

	// The result of a step is checked like any optional
	errs := errors.NewHandler()
	compiler.Compile(`import std.option;
import std.strings;
fn main() -> Int {
    let n: Int = option.map(strings.parseInt("5"), fn(n: Int) -> Int { return n; });
    let s = option.then(strings.parseInt("5"), fn(n: String) -> Int? { return 1; });
    return 0;
}`, "test.lum", errs)
	if len(errs.Diagnostics) != 2 || errs.Diagnostics[0].Code != "E2001" || errs.Diagnostics[1].Code != "E2010" {
		t.Errorf("expected an optional result and a mismatched step to be rejected, got %v", errs.Diagnostics)
	}
}

func TestRandom(t *testing.T) {
	expect(t, "import std.random;\nimport std.arrays;", `
		// The same seed gives the same numbers
		random.seed(7);
		let first = [random.below(1000), random.below(1000), random.between(1, 6)];
		let fraction = random.float();
		random.seed(7);
		println(first[0] == random.below(1000), first[1] == random.below(1000), first[2] == random.between(1, 6), fraction == random.float());

		// Everything stays within its limits
		let ok = true;
		let i = 0;
		let seen = arrays.fill(6, false);
		while i < 500 {
			let die = random.between(1, 6);
			if die < 1 || die > 6 { ok = false; }
			seen[die - 1] = true;
			let b = random.below(3);
			if b < 0 || b > 2 { ok = false; }
			let f = random.float();
			if f < 0.0 || f >= 1.0 { ok = false; }
			i++;
		}
		println(ok, arrays.all(seen, fn(s: Bool) -> Bool { return s; }), random.between(4, 4), random.below(1));
		println(random.chance(1.0), random.chance(0.0));

		// pick and shuffle keep what they are given
		println(random.pick([5]), random.pick<Int>([]), arrays.contains([1, 2, 3], random.pick([1, 2, 3]) ?? 0));
		let deck = arrays.range(0, 52);
		random.shuffle(deck);
		println(deck.length, arrays.sum(deck), arrays.sortedNumbers(deck)[0], arrays.sortedNumbers(deck)[51], arrays.sortedNumbers(deck)[30]);
		let empty: [Int] = [];
		random.shuffle(empty);
		println(empty);
	`,
		"true true true true",
		"true true 4 0",
		"true false",
		"5 null true",
		"52 1326 0 51 30",
		"[]",
	)

	expectFailure(t, "import std.random;", `println(random.below(0));`, "std.random.below: the limit must be at least 1, got 0")
	expectFailure(t, "import std.random;", `println(random.between(5, 1));`, "the limit must be at least 1")
}

func TestTime(t *testing.T) {
	expect(t, "import std.time;", `
		println(time.now() > 1700000000000, time.seconds() > 1700000000, time.seconds() <= time.now() / 1000 + 1);
		let watch = time.stopwatch();
		let before = time.ticks();
		time.sleep(3);
		println(time.ticks() - before >= 3000, watch.microseconds() >= 3000, watch.milliseconds() >= 3);
		watch.restart();
		println(watch.milliseconds() < 3);
		time.sleep(0);
	`,
		"true true true",
		"true true true",
		"true",
	)
}

func TestIO(t *testing.T) {
	path := t.TempDir() + "/notes.txt"
	body := strings.ReplaceAll(`
		let path = "FILE";
		println(io.exists(path), io.readFile(path));
		println(io.writeFile(path, "one\n"), io.appendFile(path, "two\n"), io.exists(path));
		println(strings.lines(io.readFile(path) ?? ""));
		println(io.remove(path), io.remove(path), io.exists(path));
		println(io.writeFile(path + "/nowhere/file", "x"), io.env("LUMEN_NO_SUCH_VARIABLE"), io.env("PATH") != null);

		// Input: a question answered, then the rest line by line
		let name = io.ask("name? ");
		println(name ?? "nobody");
		let rest = io.readLines();
		println(rest, io.readLine(), io.ask("more? "));
	`, "FILE", path)

	out, err := run(t, "import std.io;\nimport std.strings;", body, "ada\nfirst\n\nlast")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	// The arguments of a call are worked out before it prints anything,
	// so the second question comes before the line it is part of
	want := "false null\ntrue true true\n[\"one\", \"two\"]\ntrue false false\nfalse null true\nname? ada\nmore? [\"first\", \"\", \"last\"] null null\n"
	if out != want {
		t.Errorf("unexpected output\n--- want ---\n%s--- got ---\n%s", want, out)
	}
}

// TestLibraryErrorsPointAtTheCaller checks what a programmer sees when a
// call into the library fails: the function by the name they used, and
// the line of their own program.
func TestLibraryErrorsPointAtTheCaller(t *testing.T) {
	_, err := run(t, "import std.strings;", `
		let text = "abc";
		println(strings.substring(text, 2, 9));
	`, "")
	if err == nil {
		t.Fatal("expected a runtime error")
	}
	for _, want := range []string{"std.strings.substring: range 2..9", "test.lum:5:", "in function 'main'", "strings.substring(text, 2, 9)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to contain %q, got:\n%v", want, err)
		}
	}

	// A failure inside a function of the library written in Lumen names
	// the library's file
	_, err = run(t, "import std.arrays;", `arrays.swap([1], 0, 3);`, "")
	if err == nil {
		t.Fatal("expected a runtime error")
	}
	for _, want := range []string{"index out of bounds: 3 (length 1)", "<std>/arrays.lum:", "std.arrays.swap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to contain %q, got:\n%v", want, err)
		}
	}
}

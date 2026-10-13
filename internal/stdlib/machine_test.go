package stdlib_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/vm"
)

// What the machine has to hold up under, seen from programs: calls that go
// deep, memory that is given back, texts read by position, values laid out
// alike, failures that are survived, and a program stopped from outside.

// runWith compiles a whole program and runs it on a machine with a heap
// of the given size, set up by prepare before it starts.
func runWith(t *testing.T, heapBytes uint64, src string, prepare func(*vm.VM)) (string, error) {
	t.Helper()
	errs := errors.NewHandler()
	result := compiler.Compile(src, "test.lum", errs)
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic:\n%s", d.String())
	}
	if t.Failed() {
		t.FailNow()
	}

	var out bytes.Buffer
	machine := vm.NewVMWithHeapSize(heapBytes)
	machine.SetOutput(&out)
	if prepare != nil {
		prepare(machine)
	}
	_, err := machine.Exec(result.Chunk)
	return out.String(), err
}

func expectLines(t *testing.T, out string, want ...string) {
	t.Helper()
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

// Closures, structs and upvalues made by the million in a heap of a few
// megabytes: what is freed is counted as freed, exactly, or the count
// creeps up until the program is refused memory it has.
func TestMemoryIsGivenBackWhateverWasMade(t *testing.T) {
	out, err := runWith(t, 6<<20, `
		import std.arrays;
		import std.dict;

		struct Account { owner: String; balance: Int; history: [Int]; }
		enum Event { Deposit(Int), Transfer(String, Int), Closed }

		fn apply(account: Account, event: Event) -> Int {
			match event {
				case Event.Deposit(amount): { account.balance += amount; push(account.history, amount); }
				case Event.Transfer(to, amount): { account.balance -= amount; }
				case Event.Closed: { account.balance = 0; }
			}
			return account.balance;
		}

		fn main() -> Int {
			let total = 0;
			let kept: [fn(Int) -> Int] = [];
			for round in 0..400000 {
				// A closure and the variable it captures
				let step = round % 7;
				let add = fn(n: Int) -> Int { return n + step; };
				total = (total + add(1)) % 1000003;
				if round % 50000 == 0 { push(kept, add); }

				// A struct, an array in it, values of an enum, a text
				let account = Account { owner: "owner-" + toString(round % 10), balance: 10, history: [] };
				apply(account, Event.Deposit(round % 5));
				apply(account, Event.Transfer(account.owner, 3));
				if round % 100000 == 99999 { apply(account, Event.Closed); }
				total = (total + account.balance + arrays.len(account.history)) % 1000003;
			}
			// A dictionary that grows and is dropped, over and over
			for round in 0..200 {
				let seen = dict.empty<Int>();
				for i in 0..300 { seen.set("key" + toString(i), i * round); }
				total = (total + seen.size()) % 1000003;
			}
			let fromKept = 0;
			for add in kept { fromKept += add(100); }
			println(total, arrays.len(kept), fromKept);
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	// step is 0, 6, 5, 4, 3, 2, 1, 0 for the rounds kept: 21 + 800
	expectLines(t, out, "659938 8 821")
}

// A program that really keeps more than it may is told so, as a failure it
// can survive: what the abandoned calls held is let go, and there is room
// again.
func TestRunningOutOfMemoryIsAFailureLikeAnother(t *testing.T) {
	out, err := runWith(t, 4<<20, `
		import std.arrays;
		import std.result.{Result, Kind, attempt};

		struct Row { id: Int; label: String; }

		fn hoard(seen: [Int]) -> Int {
			let rows: [Row] = [];
			let i = 0;
			while i >= 0 {
				push(rows, Row { id: i, label: "row number " + toString(i) });
				if i % 1000 == 0 { push(seen, i); }
				i += 1;
			}
			return i;
		}

		fn main() -> Int {
			let seen: [Int] = [];
			for round in 0..3 {
				match attempt(fn() -> Int { return hoard(seen); }) {
					case Result.Ok(n): { println("held", n); }
					case Result.Err(failure): {
						match failure.kind {
							case Kind.Memory: { println("memory", failure.at.function); }
							default: { println("other:", failure.message); }
						}
					}
				}
			}
			// What was done before each failure stays done
			println(arrays.len(seen) > 30, seen[1]);

			let again: [Row] = [];
			for i in 0..1000 { push(again, Row { id: i, label: "again" }); }
			println(arrays.len(again), again[999].id);
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	expectLines(t, out, "memory hoard", "memory hoard", "memory hoard", "true 1000", "1000 999")
}

// Calls nested thousands deep, each with a closure over one of its
// variables: the registers move as the stack grows, and every closure
// still finds its own.
func TestDeepCallsKeepTheirVariables(t *testing.T) {
	out, err := runWith(t, 64<<20, `
		import std.arrays;
		import std.result.{Result, Kind, attempt};

		struct Counter { bump: fn() -> Int; read: fn() -> Int; }

		fn counter(start: Int) -> Counter {
			let n = start;
			return Counter { bump: fn() -> Int { n += 1; return n; }, read: fn() -> Int { return n; } };
		}

		// Each level makes a closure over its own depth, calls further down,
		// then changes the variable the closure follows
		fn descend(depth: Int, limit: Int, readers: [fn() -> Int]) -> Int {
			let mine = depth;
			push(readers, fn() -> Int { return mine; });
			let below = 0;
			if depth < limit { below = descend(depth + 1, limit, readers); }
			mine = mine * 2;
			return below + 1;
		}

		fn ackermannish(m: Int, n: Int) -> Int {
			if m == 0 { return n + 1; }
			if n == 0 { return ackermannish(m - 1, 1); }
			return ackermannish(m - 1, ackermannish(m, n - 1));
		}

		fn forever(n: Int) -> Int { return forever(n + 1) + 1; }

		fn main() -> Int {
			let readers: [fn() -> Int] = [];
			println(descend(0, 6000, readers), arrays.len(readers));
			// Every closure sees what its call left in the variable
			let wrong = 0;
			for i, read in readers { if read() != i * 2 { wrong += 1; } }
			println(wrong, readers[0](), readers[6000]());

			let c = counter(10);
			c.bump(); c.bump();
			println(c.read(), ackermannish(2, 300));

			// Deeper than the limit: a failure, and the stack is whole after
			match attempt(fn() -> Int { return forever(0); }) {
				case Result.Ok(n): { println("returned", n); }
				case Result.Err(failure): {
					match failure.kind {
						case Kind.Depth: { println("too deep"); }
						default: { println("other:", failure.message); }
					}
				}
			}
			println(c.bump(), descend(0, 100, readers), readers[3000]());
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	expectLines(t, out, "6001 6001", "0 0 12000", "12 603", "too deep", "13 101 6000")
}

// A field is found where the last value of the same struct had it. Values
// of different structs going through the same code, fields assigned and
// methods called through a trait: each still gets its own.
func TestFieldsOfManyStructsThroughTheSameCode(t *testing.T) {
	out, err := runWith(t, 64<<20, `
		import std.arrays;

		trait Shape { fn area() -> Int; fn grow(by: Int) -> Void; }

		struct Rect { width: Int; height: Int; }
		struct Square { side: Int; }
		struct Tower { height: Int; width: Int; floors: Int; }

		impl Shape for Rect {
			fn area() -> Int { return self.width * self.height; }
			fn grow(by: Int) -> Void { self.width += by; }
		}
		impl Shape for Square {
			fn area() -> Int { return self.side * self.side; }
			fn grow(by: Int) -> Void { self.side += by; }
		}
		// The same field names as Rect, in another order
		impl Shape for Tower {
			fn area() -> Int { return self.width * self.height * self.floors; }
			fn grow(by: Int) -> Void { self.floors += by; }
		}

		fn total(shapes: [Shape]) -> Int {
			let sum = 0;
			for shape in shapes { sum += shape.area(); }
			return sum;
		}

		fn main() -> Int {
			let shapes: [Shape] = [];
			for i in 0..3000 {
				if i % 3 == 0 { push(shapes, Rect { width: i, height: 2 }); }
				if i % 3 == 1 { push(shapes, Square { side: i % 10 }); }
				if i % 3 == 2 { push(shapes, Tower { height: 3, width: i % 4, floors: 2 }); }
			}
			let before = total(shapes);
			for shape in shapes { shape.grow(1); }
			println(before, total(shapes));

			// The same line reads width off a Rect and off a Tower
			let rect = Rect { width: 7, height: 1 };
			let tower = Tower { height: 9, width: 4, floors: 1 };
			let widths = 0;
			for i in 0..1000 {
				if i % 2 == 0 { widths += rect.width; } else { widths += tower.width; }
				rect.width += 1;
			}
			println(widths, rect.width, tower.height, rect, tower);
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	expectLines(t, out, "3034500 3051000", "255000 1007 9 {height: 1, width: 1007} {floors: 1, height: 9, width: 4}")
}

// Positions in a text are characters whatever the text is made of, and
// reading a long text position by position takes time in proportion to
// its length: this one would take minutes if each position were found by
// going through the text from its start.
func TestReadingALongTextByPosition(t *testing.T) {
	started := time.Now()
	out, err := runWith(t, 64<<20, `
		import std.strings;

		fn count(text: String, wanted: String) -> Int {
			let found = 0;
			for i in 0..strings.len(text) { if text[i] == wanted { found += 1; } }
			return found;
		}

		fn main() -> Int {
			let plain = strings.repeat("abcdefghij", 30000);
			let mixed = strings.repeat("aé日🙂-", 30000);
			println(strings.len(plain), count(plain, "a"), count(plain, "j"));
			println(strings.len(mixed), count(mixed, "é"), count(mixed, "🙂"), count(mixed, "x"));
			println(mixed[0], mixed[1], mixed[2], mixed[3], mixed[149999], plain[299999]);

			// Pieces taken from anywhere, by position
			let pieces = 0;
			for i in 0..30000 {
				if strings.substring(mixed, i * 5, i * 5 + 4) == "aé日🙂" { pieces += 1; }
			}
			println(pieces, strings.substring(mixed, 149995, 150000), strings.substring(plain, 0, 300000) == plain);
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	expectLines(t, out,
		"300000 30000 30000",
		"150000 30000 30000 0",
		"a é 日 🙂 - j",
		"30000 aé日🙂- true")
	if took := time.Since(started); took > 20*time.Second {
		t.Errorf("took %v: positions are being found from the start of the text", took)
	}
}

// Values that hold themselves, or each other many times over, are printed
// in a bounded time and to a bounded length; long chains are compared
// without a call per link.
func TestValuesThatHoldThemselves(t *testing.T) {
	out, err := runWith(t, 256<<20, `
		import std.strings;

		struct Node { name: String; next: Node?; }
		enum List { Nil, Cons(Int, List) }

		fn chain(length: Int) -> List {
			let list = List.Nil;
			for i in 0..length { list = List.Cons(i, list); }
			return list;
		}

		fn main() -> Int {
			let ring = Node { name: "ring", next: null };
			ring.next = ring;
			println(strings.len(toString(ring)) < 500);

			// Fifty times itself, eight levels deep: more text than there
			// are atoms, were it written out
			let nest: [Any] = [];
			for i in 0..50 { push(nest, nest); }
			let text = toString(nest);
			println(strings.len(text) > 1000, strings.len(text) < 3000000, strings.endsWith(text, "...]"));

			let a = chain(300000);
			let b = chain(300000);
			println(a == b, a == chain(299999), a == List.Cons(-1, b));
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	expectLines(t, out, "true", "true true true", "true false false")
}

// A program stopped from outside ends where it is, whatever it was doing:
// computing, sleeping, or waiting in several tasks. Nothing in it catches
// that, and no cleanup of its runs.
func TestAProgramIsStoppedFromOutside(t *testing.T) {
	programs := map[string]string{
		"computing": `
			fn main() -> Int {
				let n = 0;
				while n >= 0 { n = (n + 1) % 1000; }
				return n;
			}`,
		"inside attempt": `
			import std.result.{Result, attempt, guard};
			fn spin() -> Int { let n = 0; while n >= 0 { n = (n + 1) % 1000; } return n; }
			fn main() -> Int {
				match attempt(spin) {
					case Result.Ok(n): { println("returned"); }
					case Result.Err(failure): { println("caught"); }
				}
				println("went on");
				return 0;
			}`,
		"with cleanup pending": `
			fn spin() -> Int {
				defer println("cleanup");
				let n = 0;
				while n >= 0 { n = (n + 1) % 1000; }
				return n;
			}
			fn main() -> Int { return spin(); }`,
		"sleeping": `
			import std.time;
			fn main() -> Int { time.sleep(600000); println("woke"); return 0; }`,
		"tasks": `
			import std.task;
			import std.time;
			fn main() -> Int {
				let sleeper = task.start(fn() -> Int { time.sleep(600000); return 1; });
				let spinner = task.start(fn() -> Int { let n = 0; while n >= 0 { n = (n + 1) % 1000; task.pause(); } return n; });
				println(sleeper.wait().isOk(), spinner.wait().isOk());
				return 0;
			}`,
	}
	for name, src := range programs {
		t.Run(name, func(t *testing.T) {
			started := time.Now()
			out, err := runWith(t, 64<<20, src, func(machine *vm.VM) {
				time.AfterFunc(100*time.Millisecond, machine.Interrupt)
			})
			if !vm.IsInterrupted(err) {
				t.Fatalf("expected the program to be interrupted, got %v (it wrote %q)", err, out)
			}
			if out != "" {
				t.Errorf("the program went on to write %q", out)
			}
			if took := time.Since(started); took > 10*time.Second {
				t.Errorf("it took %v to stop", took)
			}
			failure := err.(*vm.RuntimeError)
			if len(failure.Stack) == 0 || failure.Stack[0].File == "" {
				t.Errorf("an interruption says where the program was: %+v", failure.Stack)
			}
		})
	}
}

// Machines side by side, one per thread, each with tasks of its own.
func TestMachinesSideBySide(t *testing.T) {
	src := `
		import std.task;
		import std.arrays;

		struct Job { id: Int; weight: Int; }

		fn work(job: Job) -> Int {
			let sum = 0;
			for i in 0..job.weight { sum = (sum + i * job.id) % 10007; task.pause(); }
			return sum;
		}

		fn main() -> Int {
			let jobs: [Job] = [];
			for i in 0..20 { push(jobs, Job { id: i + 1, weight: 50 + i }); }
			let results = task.map(jobs, work).expect("the jobs");
			println(arrays.sum(results));
			return 0;
		}`
	errs := errors.NewHandler()
	compiled := compiler.Compile(src, "test.lum", errs)
	if errs.HasErrors() {
		t.Fatalf("does not compile: %v", errs.Diagnostics)
	}

	const threads = 6
	outputs := make(chan string, threads)
	for i := 0; i < threads; i++ {
		go func() {
			// The same compiled program, a machine each
			var out bytes.Buffer
			machine := vm.NewVMWithHeapSize(8 << 20)
			machine.SetOutput(&out)
			if _, err := machine.Exec(compiled.Chunk); err != nil {
				outputs <- "error: " + err.Error()
				return
			}
			outputs <- out.String()
		}()
	}
	first := <-outputs
	if strings.HasPrefix(first, "error") || strings.TrimSpace(first) == "" {
		t.Fatalf("a machine failed: %s", first)
	}
	for i := 1; i < threads; i++ {
		if got := <-outputs; got != first {
			t.Errorf("two machines disagree: %q and %q", first, got)
		}
	}
}

// Values of enums and structs through everything that looks at what they
// are: matches with and without values held, tests of a type kept as a
// value or branched on, equality, values that hold nothing shared between
// places, structs of many fields, traits, tasks.
func TestValuesKnowWhatTheyAre(t *testing.T) {
	out, err := runWith(t, 64<<20, `
		import std.arrays;
		import std.dict;
		import std.task;

		enum Light { Red, Amber, Green, Flashing(Int), Broken(String, Int) }
		enum Tree { Leaf, Node(Tree, Int, Tree) }

		trait Describe { fn describe() -> String; }

		struct Wide {
			a: Int; b: Int; c: Int; d: Int; e: Int; f: Int; g: Int; h: Int; i: Int; j: Int;
			k: Int; l: Int; m: Int; n: Int; o: Int; p: Int; q: Int; r: Int; s: Int; t: Int;
		}
		struct Empty { }
		struct Junction { name: String; lights: [Light]; }

		impl Describe for Junction {
			fn describe() -> String { return self.name + " with " + toString(arrays.len(self.lights)) + " lights"; }
		}
		impl Describe for Empty {
			fn describe() -> String { return "nothing"; }
		}

		fn next(light: Light) -> Light {
			match light {
				case Light.Red: { return Light.Green; }
				case Light.Green: { return Light.Amber; }
				case Light.Amber: { return Light.Red; }
				case Light.Flashing(times) if times > 1: { return Light.Flashing(times - 1); }
				case Light.Flashing(times): { return Light.Red; }
				case Light.Broken(why, since): { return light; }
			}
		}

		fn waits(light: Light) -> Int {
			match light {
				case Light.Red | Light.Amber: { return 1; }
				case Light.Flashing(times): { return times; }
				case Light.Broken(why, since): { return since; }
				default: { return 0; }
			}
		}

		fn insert(tree: Tree, value: Int) -> Tree {
			match tree {
				case Tree.Leaf: { return Tree.Node(Tree.Leaf, value, Tree.Leaf); }
				case Tree.Node(left, here, right): {
					if value < here { return Tree.Node(insert(left, value), here, right); }
					return Tree.Node(left, here, insert(right, value));
				}
			}
		}

		fn total(tree: Tree) -> Int {
			match tree {
				case Tree.Leaf: { return 0; }
				case Tree.Node(left, here, right): { return total(left) + here + total(right); }
			}
		}

		fn depth(tree: Tree) -> Int {
			match tree {
				case Tree.Leaf: { return 0; }
				case Tree.Node(left, here, right): {
					let l = depth(left);
					let r = depth(right);
					if l > r { return l + 1; }
					return r + 1;
				}
			}
		}

		fn kind(value: Any) -> String {
			if value is Light { return "light"; }
			if value is Tree { return "tree"; }
			if value is Describe { return "described"; }
			if value is Wide { return "wide"; }
			if value is Int { return "int"; }
			return "other";
		}

		fn main() -> Int {
			// A cycle of lights, many times round: the values that hold
			// nothing are the same wherever they are made
			let light = Light.Red;
			let waited = 0;
			for step in 0..3000 { waited += waits(light); light = next(light); }
			println(light == Light.Red, waited, next(Light.Flashing(3)) == Light.Flashing(2), next(Light.Flashing(1)));
			println(Light.Red == Light.Red, Light.Red == Light.Amber, Light.Flashing(2) == Light.Flashing(2), Light.Broken("fuse", 3) == Light.Broken("fuse", 4));

			// Kept in arrays, in a dictionary, in a struct
			let seen = dict.empty<Light>();
			let lights: [Light] = [];
			for i in 0..9 {
				let made = Light.Green;
				if i % 3 == 1 { made = Light.Amber; }
				if i % 3 == 2 { made = Light.Flashing(i); }
				push(lights, made);
				seen.set("light" + toString(i), made);
			}
			let junction = Junction { name: "north", lights: lights };
			let green = arrays.len(arrays.filter(junction.lights, l -> l == Light.Green));
			println(green, seen.getOr("light5", Light.Red), seen.getOr("light4", Light.Red) == Light.Amber, junction.describe());

			// A tree of values that hold values
			let tree = Tree.Leaf;
			for i in 0..500 { tree = insert(tree, (i * 7919) % 1009); }
			println(total(tree), depth(tree) > 8, tree == Tree.Leaf, insert(Tree.Leaf, 1) == insert(Tree.Leaf, 1));

			// A struct of more fields than a value comes with room for
			let wide = Wide { a: 1, b: 2, c: 3, d: 4, e: 5, f: 6, g: 7, h: 8, i: 9, j: 10,
				k: 11, l: 12, m: 13, n: 14, o: 15, p: 16, q: 17, r: 18, s: 19, t: 20 };
			wide.t += wide.a + wide.j;
			println(wide.a + wide.h + wide.p + wide.q + wide.t, Empty { }.describe());

			// What a value is, asked as a value and branched on
			let things: [Any] = [Light.Red, tree, junction, wide, 42, "text", Empty { }, Light.Broken("fuse", 1)];
			let kinds: [String] = [];
			for thing in things { push(kinds, kind(thing)); }
			println(kinds);
			let isLight = things[0] is Light;
			let isTree = things[0] is Tree;
			println(isLight, isTree, things[7] is Light, things[2] is Describe, things[3] is Describe);

			// Made in other tasks, the same values
			let made = task.map([1, 2, 3, 4], n -> next(Light.Flashing(n))).expect("the lights");
			println(made, made[0] == Light.Red, made[3] == Light.Flashing(3));
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	// 500 numbers (i * 7919) % 1009 for i below 500
	sum := 0
	for i := 0; i < 500; i++ {
		sum += (i * 7919) % 1009
	}
	expectLines(t, out,
		"true 2000 true Red",
		"true false true false",
		"3 Flashing(5) true north with 9 lights",
		fmt.Sprint(sum)+" true false true",
		"73 nothing",
		`["light", "tree", "described", "wide", "int", "other", "described", "light"]`,
		"true false true true false",
		"[Red, Flashing(1), Flashing(2), Flashing(3)] true true")
}

// A struct may have a field and a method of the same name. The method is
// kept on the value when a trait can reach it, or when another struct has
// a method of that name too; the field must still be the field.
func TestAFieldAndAMethodOfTheSameName(t *testing.T) {
	out, err := runWith(t, 64<<20, `
		import std.arrays;

		enum Mode { Off, On, Dimmed(Int) }

		trait Countable { fn count() -> Int; }

		// "mode" and "count" are fields, and methods other structs have too
		struct Lamp { mode: Mode = Mode.Off; count: Int = 0; name: String; }
		struct Dial { level: Int; }
		struct Shelf { items: [String]; }

		impl Lamp {
			fn mode(next: Mode) { self.mode = next; self.count += 1; }
			fn isOff() -> Bool { return self.mode == Mode.Off; }
			fn name() -> String { return "lamp " + self.name; }
		}
		impl Countable for Lamp {
			fn count() -> Int { return self.count * 100; }
		}
		impl Dial {
			fn mode(next: Mode) { self.level += 1; }
			fn name() -> String { return "dial"; }
		}
		impl Countable for Shelf {
			fn count() -> Int { return arrays.len(self.items); }
		}

		fn total(things: [Countable]) -> Int {
			let sum = 0;
			for thing in things { sum += thing.count(); }
			return sum;
		}

		fn main() -> Int {
			let lamp = Lamp { name: "desk" };
			println(lamp.isOff(), lamp.mode == Mode.Off, lamp.mode, lamp.count, lamp.count(), lamp.name, lamp.name());

			lamp.mode(Mode.Dimmed(3));
			lamp.mode(Mode.On);
			println(lamp.isOff(), lamp.mode, lamp.count, lamp.count());

			lamp.count = 7;
			lamp.name = "hall";
			let dial = Dial { level: 0 };
			dial.mode(Mode.On);
			println(lamp.count, lamp.count(), lamp.name(), dial.name(), dial.level);
			println(total([lamp, Shelf { items: ["a", "b"] }]), lamp);
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	expectLines(t, out,
		"true true Off 0 0 desk lamp desk",
		"false On 2 200",
		"7 700 lamp hall dial 1",
		`702 {count: 7, mode: On, name: "hall"}`)
}

// A dictionary through its whole life: a few keys, then many, keys taken
// away and set again, in the order they were first set throughout. What it
// holds is checked against two arrays kept alongside.
func TestADictionaryFromAFewKeysToMany(t *testing.T) {
	out, err := runWith(t, 64<<20, `
		import std.arrays;
		import std.dict;
		import std.dict.{Dict};
		import std.strings;

		struct Stock { item: String; count: Int; }

		// Whether a dictionary holds exactly these keys with these values,
		// in this order, by every way of asking
		fn same(held: Dict<Int>, keys: [String], values: [Int]) -> Bool {
			if held.size() != arrays.len(keys) || held.isEmpty() != (arrays.len(keys) == 0) { return false; }
			let listed = held.keys();
			let worth = held.values();
			if arrays.len(listed) != arrays.len(keys) || arrays.len(worth) != arrays.len(keys) { return false; }
			for i, key in keys {
				if listed[i] != key || worth[i] != values[i] { return false; }
				if !held.has(key) || held.get(key) != values[i] || held.getOr(key, -1) != values[i] { return false; }
				if held[key] != values[i] { return false; }
			}
			let at = 0;
			for (key, value) in held.entries() {
				if key != keys[at] || value != values[at] { return false; }
				at++;
			}
			let seen = 0;
			held.each((key, value) -> { if key == keys[seen] && value == values[seen] { seen++; } });
			let looped = 0;
			for key in held { if key == keys[looped] { looped++; } }
			return seen == arrays.len(keys) && looped == arrays.len(keys)
				&& !held.has("never set") && held.get("never set") == null && held.getOr("never set", -7) == -7;
		}

		fn main() -> Int {
			let held = dict.empty<Int>();
			let keys: [String] = [];
			let values: [Int] = [];
			println(same(held, keys, values), held.remove("nothing"));

			// One key at a time, past the number that is looked through
			let ok = true;
			for i in 0..40 {
				let key = "key" + toString(i * 3 % 41);
				held.set(key, i);
				push(keys, key);
				push(values, i);
				if !same(held, keys, values) { ok = false; }
			}
			println(ok, held.size());

			// Set again: the value changes, the place does not
			for i in 0..40 {
				if i % 4 == 0 {
					held.set(keys[i], values[i] * 100);
					values[i] = values[i] * 100;
				}
			}
			held["key0"] = -5;
			values[arrays.indexOf(keys, "key0") ?? 0] = -5;
			held.update("key3", 0, n -> n + 1000);
			values[arrays.indexOf(keys, "key3") ?? 0] += 1000;
			println(same(held, keys, values));

			// Taken away one by one, down to none: enough of them for the
			// empty places to be taken out several times on the way
			let removed = true;
			for round in 0..40 {
				let at = (round * 7) % arrays.len(keys);
				if !held.remove(keys[at]) || held.remove(keys[at]) { removed = false; }
				keys = arrays.without(keys, at);
				values = arrays.without(values, at);
				if !same(held, keys, values) { removed = false; }
				// A key set again after it was taken away goes to the end
				if round % 9 == 4 {
					held.set("back" + toString(round), round);
					push(keys, "back" + toString(round));
					push(values, round);
				}
			}
			println(removed, held.size(), held.keys());

			// Few keys again, and the same ones a second time
			for key in held.keys() { held.remove(key); }
			held.set("a", 1);
			held.set("b", 2);
			held.remove("a");
			held.set("a", 3);
			println(held.keys(), held.values(), held.sortedKeys(), held.size());

			// Many keys, counted and grouped
			let stock = dict.empty<Stock>();
			for i in 0..3000 {
				let name = "item" + toString(i % 700);
				let entry = stock.get(name);
				if entry == null {
					stock.set(name, Stock { item: name, count: 1 });
				} else {
					entry.count++;
				}
			}
			let total = 0;
			for name in stock { total += stock.getOr(name, Stock { item: "", count: 0 }).count; }
			let words = strings.split("the quick fox and the lazy dog and the cat", " ");
			let counts = arrays.countBy(words, w -> w);
			println(stock.size(), total, stock.getOr("item699", Stock { item: "", count: 0 }).count, counts.keys(), counts.values());
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	expectLines(t, out,
		"true false",
		"true 40",
		"true",
		// Checked at every step against the two arrays: what is left is what they hold
		`true 4 ["key1", "key13", "key37", "back31"]`,
		`["b", "a"] [2, 3] ["a", "b"] 2`,
		`700 3000 4 ["the", "quick", "fox", "and", "lazy", "dog", "cat"] [3, 1, 1, 2, 1, 1, 1]`)
}

// Conditions made of several tests, in every place a condition goes, with
// tests that leave a trace when they are made: only those whose turn
// comes are, in order; and texts built piece by piece from texts written
// in the program, in loops.
func TestConditionsAndTextsBuiltInLoops(t *testing.T) {
	out, err := runWith(t, 64<<20, `
		import std.arrays;
		import std.strings;

		struct Trace { asked: [String]; }

		impl Trace {
			// A test that says it was made
			fn is(name: String, answer: Bool) -> Bool {
				push(self.asked, name);
				return answer;
			}
			fn took() -> String {
				let said = strings.join(self.asked, "");
				arrays.clear(self.asked);
				return said;
			}
		}

		fn classify(c: String) -> String {
			if (c >= "a" && c <= "z") || (c >= "A" && c <= "Z") { return "letter"; }
			if c >= "0" && c <= "9" { return "digit"; }
			if c == " " || c == "\t" || c == "\n" { return "space"; }
			if !(c == "." || c == "," || c == "!") { return "other"; }
			return "mark";
		}

		fn main() -> Int {
			let t = Trace { asked: [] };

			// Which tests are made, for every way two can go
			let seen: [String] = [];
			for a in [true, false] {
				for b in [true, false] {
					let line = "";
					if t.is("a", a) && t.is("b", b) { line = line + "and "; }
					line = line + t.took() + " ";
					if t.is("a", a) || t.is("b", b) { line = line + "or "; }
					line = line + t.took() + " ";
					if !(t.is("a", a) && !t.is("b", b)) { line = line + "not "; }
					line = line + t.took() + " ";
					if (t.is("a", a) || t.is("b", b)) && (t.is("c", !a) || t.is("d", b)) { line = line + "both "; }
					line = line + t.took();
					push(seen, line);
				}
			}
			for line in seen { println(line); }

			// The same as a value, and as what a loop goes by
			let kept = t.is("x", false) && t.is("y", true) || t.is("z", true);
			println(kept, t.took());
			let n = 0;
			while t.is("w", n < 3) && (n != 1 || t.is("v", true)) { n += 1; }
			println(n, t.took());

			// A text taken apart and put together again, a character at a time
			let source = "Héllo, wörld! 12 日本語.";
			let kinds = "";
			let letters = "";
			let rest = "";
			for i in 0..strings.len(source) {
				let c = source[i];
				let kind = classify(c);
				kinds = kinds + strings.substring(kind, 0, 1);
				if kind == "letter" || kind == "other" && c != "!" { letters = letters + c; } else { rest = rest + c; }
			}
			println(kinds);
			println(letters, "|", rest, strings.len(letters) + strings.len(rest) == strings.len(source));

			// The same text written twice is the same text; built two ways too
			let dot = ".";
			let built = "";
			for i in 0..40 { built = built + dot + "-"; }
			println(dot == ".", built == strings.repeat(".-", 40), strings.len(built), built[78] + built[79]);
			return 0;
		}`, nil)
	if err != nil {
		t.Fatalf("the program was stopped: %v", err)
	}
	expectLines(t, out,
		// a and b true; then a alone; then b alone; then neither
		"and ab or a not ab both acd",
		"ab or a ab acd",
		"a or ab not a both abc",
		"a ab not a ab",
		"true xz",
		"3 wwvww",
		// é and ö are not between a and z: they are "other", like the three of 日本語
		"lolllmslolllmsddsooom",
		"Héllowörld日本語 | , ! 12 . true",
		"true true 80 .-")
}

// Arrays and texts have no members, so `length` is a name like any other:
// a field of a struct, a method, a variable. The length of a collection is
// asked with a function, alone or given to another.
func TestLengthIsOnlyAName(t *testing.T) {
	out, err := runWith(t, 16<<20, `
		import std.arrays;
		import std.strings;

		struct Rope { length: Int; pieces: [String]; }
		impl Rope {
			fn add(piece: String) {
				push(self.pieces, piece);
				self.length += strings.len(piece);
			}
		}
		struct Queue { items: [Int]; }
		impl Queue {
			fn length() -> Int { return arrays.len(self.items); }
		}

		fn main() {
			let rope = Rope { length: 0, pieces: [] };
			rope.add("été");
			rope.add("日本");
			println(rope.length, arrays.len(rope.pieces));
			rope.length = 100;
			println(rope.length);

			let length = Queue { items: [1, 2, 3] }.length();
			println(length);

			// Given to another function, and over what a loop walks
			println(arrays.map(["a", "bcd", ""], strings.len));
			println(arrays.map([[1], [], [1, 2]], xs -> arrays.len(xs)));
			let seen = 0;
			for piece in rope.pieces { for c in piece { seen++; } }
			println(seen);
		}
	`, nil)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	expectLines(t, out, "5 2", "100", "3", "[1, 3, 0]", "[1, 0, 2]", "5")
}

// The pipe, in programs: chains over the library, generic functions that
// take their types from what is piped, failures that leave by ?, and the
// mistakes of a piped call said as those of the call it is.
func TestPipe(t *testing.T) {
	out, err := runWith(t, 16<<20, `
		import std.arrays;
		import std.math;
		import std.strings;
		import std.dict;
		import std.dict.{Dict};
		import std.result.{Result};

		struct Order { customer: String; total: Int; paid: Bool; }

		enum Shape { Circle(Int), Square(Int) }
		impl Shape {
			fn area() -> Int {
				return match self {
					case Shape.Circle(r): 3 * r * r;
					case Shape.Square(side): side * side;
				};
			}
		}

		fn orders() -> [Order] {
			return [
				Order { customer: "ada", total: 120, paid: true },
				Order { customer: "bob", total: 80, paid: false },
				Order { customer: "ada", total: 30, paid: true },
				Order { customer: "cy", total: 200, paid: true },
			];
		}

		fn parse(text: String) -> Result<Int, String> {
			let n = strings.parseInt(text);
			if n == null { return Result.Err("not a number: '${text}'"); }
			return Result.Ok(n);
		}

		// Leaves with the first failure
		fn sum(line: String) -> Result<Int, String> {
			let total = 0;
			for part in line |> strings.split(",") {
				total += part |> strings.trim() |> parse()?;
			}
			return Result.Ok(total);
		}

		fn twice<T>(value: T, f: fn(T) -> T) -> T { return value |> f |> f; }
		fn area(shape: Shape) -> Int { return shape.area(); }

		fn main() {
			// A chain over the library, each step with what the one before gave
			let paid = orders()
				|> arrays.filter(o -> o.paid)
				|> arrays.map(o -> o.total)
				|> arrays.fold(0, (sum, n) -> sum + n);
			println(paid);

			let names = orders()
				|> arrays.map(o -> o.customer)
				|> arrays.unique()
				|> arrays.map(strings.upper)
				|> strings.join(", ");
			println(names);

			// Looser than arithmetic, tighter than comparisons
			println(2 + 3 |> math.abs);
			println(0 - 7 |> math.abs |> math.max(3) * 2);
			if orders() |> arrays.len == 4 && paid |> math.min(1000) > 300 { println("four orders"); }

			// Failures
			println("1, 2, 39" |> sum);
			println("1, two, 3" |> sum);

			// Generic functions, functions kept in variables, methods
			let shout = fn(text: String) -> String { return text + "!"; };
			println("hey" |> twice(shout));
			println(3 |> twice(n -> n * n));
			println("go" |> shout |> shout);
			println([Shape.Circle(2), Shape.Square(3)] |> arrays.map(area) |> arrays.sum());

			let seen: Dict<Int> = dict.empty();
			for order in orders() { order.customer |> seen.set((seen.get(order.customer) ?? 0) + order.total); }
			println(seen.get("ada"), seen.get("cy"), seen.get("nobody"));
		}
	`, nil)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	expectLines(t, out,
		"350", "ADA, BOB, CY",
		"5", "14", "four orders",
		"Ok(42)", "Err(\"not a number: 'two'\")",
		"hey!!", "81", "go!!", "21",
		"150 200 null")
}

// Ranges kept as values: made, passed around, asked about, stepped through
// both ways, and looped over without an array being made.
func TestRangesAsValues(t *testing.T) {
	out, err := runWith(t, 16<<20, `
		import std.arrays;
		import std.ops.{Range};
		import std.strings;

		struct Booking { guest: String; nights: Range; }
		impl Booking {
			fn overlaps(other: Booking) -> Bool {
				return arrays.any(self.nights.toArray(), n -> other.nights.contains(n));
			}
		}

		fn evens(numbers: Range) -> [Int] { return numbers.filter(n -> n % 2 == 0); }
		fn total(numbers: Range) -> Int {
			let sum = 0;
			for n in numbers { sum += n; }
			return sum;
		}
		fn span(low: Int, high: Int) -> Range { return low..high; }
		fn describe(r: Range) -> String { return "${r} holds ${r.len()}: ${r.toArray()}"; }

		fn main() {
			// Kept, passed, returned
			let teens = 13..20;
			println(teens.len(), teens.contains(13), teens.contains(19), teens.contains(20), teens.contains(12));
			println(evens(0..=10));
			println(total(1..=100), total(span(5, 8)));
			println(teens.from, teens.to, teens.inclusive, teens.by);
			println(teens is Range);

			// Steps, up and down
			println(describe((0..10).step(3)));
			println(describe((0..=10).step(5)));
			println(describe((10..0).step(-2)));
			println(describe((10..=0).step(-5)));
			println(describe((0..10).reversed()));
			println(describe((0..10).step(3).reversed()));
			println(describe((10..0).step(-2).reversed()));
			println((0..100).step(7).first(), (0..100).step(7).last());
			println((0..100).step(7).contains(49), (0..100).step(7).contains(50), (10..0).step(-2).contains(4), (10..0).step(-2).contains(5), (10..0).step(-2).contains(0));

			// Ranges that hold nothing
			println(describe(5..5));
			println(describe(5..=5));
			println(describe(10..0));
			println(describe((0..10).step(-1)));
			println(describe((5..5).reversed()));
			println((5..5).first(), (5..5).last(), (5..5).isEmpty(), (5..=5).isEmpty());

			// Loops: with a position, left early, with a step skipped
			let seen: [String] = [];
			for at, n in (20..0).step(-4) {
				if n == 12 { continue; }
				if n == 4 { break; }
				push(seen, "${at}:${n}");
			}
			println(seen |> strings.join(" "));

			// The range is read once: what the body does to it changes nothing
			let window = 0..3;
			let turns = 0;
			for n in window { window = 0..1000; turns++; }
			println(turns, window.len());

			// Loops inside loops, each with its own count
			let pairs = 0;
			for a in (0..6).step(2) { for b in (a..6).step(2) { pairs += a * b; } }
			println(pairs);

			// With the rest of the language
			println((1..=5).map(n -> n * n) |> arrays.sum());
			println((1..=3).map(n -> strings.repeat("*", n)) |> strings.join(" "));
			let stays = [Booking { guest: "ada", nights: 3..6 }, Booking { guest: "bob", nights: 5..8 }, Booking { guest: "cy", nights: 6..7 }];
			println(stays[0].overlaps(stays[1]), stays[0].overlaps(stays[2]), stays[1].overlaps(stays[2]));
			let ranges = [0..2, 10..=11, (5..0).step(-2)];
			println(arrays.map(ranges, r -> r.len()), arrays.map(ranges, total));

			// A loop that counts needs none of this, and still counts
			let plain = 0;
			for i in 0..5 { plain += i; }
			match plain { case 0..10: println("few"); case 10..=20: println("some"); default: println("many"); }
		}
	`, nil)
	if err != nil {
		t.Fatalf("runtime error: %v\nafter printing:\n%s", err, out)
	}
	expectLines(t, out,
		"7 true true false false",
		"[0, 2, 4, 6, 8, 10]",
		"5050 18",
		"13 20 false 1",
		"true",
		"(0..10).step(3) holds 4: [0, 3, 6, 9]",
		"(0..=10).step(5) holds 3: [0, 5, 10]",
		"(10..0).step(-2) holds 5: [10, 8, 6, 4, 2]",
		"(10..=0).step(-5) holds 3: [10, 5, 0]",
		"(9..=0).step(-1) holds 10: [9, 8, 7, 6, 5, 4, 3, 2, 1, 0]",
		"(9..=0).step(-3) holds 4: [9, 6, 3, 0]",
		"(2..=10).step(2) holds 5: [2, 4, 6, 8, 10]",
		"0 98",
		"true false true false false",
		"5..5 holds 0: []",
		"5..=5 holds 1: [5]",
		"10..0 holds 0: []",
		"(0..10).step(-1) holds 0: []",
		"(5..5).step(-1) holds 0: []",
		"null null true false",
		"0:20 1:16 3:8",
		"3 1000",
		"28",
		"55",
		"* ** ***",
		"true false true",
		"[2, 2, 3] [1, 21, 9]",
		"some")
}

// A loop over a range that was kept counts: a hundred thousand numbers
// in a heap that could not hold them as an array.
func TestLoopOverAKeptRangeMakesNoArray(t *testing.T) {
	out, err := runWith(t, 1<<20, `
		fn main() {
			let numbers = (0..2000000).step(2);
			let sum = 0;
			for n in numbers { sum += n % 7; }
			println(numbers.len(), sum);
			let down = 0;
			for n in (1000000..0).step(-1) { down += n % 3; }
			println(down);
		}
	`, nil)
	if err != nil {
		t.Fatalf("runtime error: %v\nafter printing:\n%s", err, out)
	}
	expectLines(t, out, "1000000 2999997", "1000000")
}

func TestRangeRefusals(t *testing.T) {
	_, err := runWith(t, 16<<20, `fn main() { let r = (0..10).step(0); println(r); }`, nil)
	if err == nil || !strings.Contains(err.Error(), "a range cannot go by steps of 0") {
		t.Errorf("a step of 0: %v", err)
	}
}

// The operators on bits, in programs: flags, masks, packed values and
// what is refused.
func TestBitwiseOperators(t *testing.T) {
	out, err := runWith(t, 16<<20, `
		import std.arrays;

		const READ = 1;
		const WRITE = 1 << 1;
		const EXEC = 1 << 2;
		const ALL = READ | WRITE | EXEC;
		const NONE = ALL & ~ALL;

		struct Color { r: Int; g: Int; b: Int; a: Int; }

		fn pack(c: Color) -> Int { return c.r << 24 | c.g << 16 | c.b << 8 | c.a; }
		fn unpack(n: Int) -> Color {
			return Color { r: n >> 24 & 0xFF, g: n >> 16 & 0xFF, b: n >> 8 & 0xFF, a: n & 0xFF };
		}
		fn has(flags: Int, flag: Int) -> Bool { return flags & flag != 0; }
		fn ones(n: Int) -> Int {
			let count = 0;
			while n != 0 { count += n & 1; n >>= 1; }
			return count;
		}
		fn names(flags: Int) -> [String] {
			let out: [String] = [];
			if has(flags, READ) { push(out, "read"); }
			if has(flags, WRITE) { push(out, "write"); }
			if has(flags, EXEC) { push(out, "exec"); }
			return out;
		}

		fn main() {
			println(ALL, NONE, names(READ | EXEC), names(ALL & ~WRITE), names(NONE));

			let flags = READ;
			flags |= EXEC;
			flags ^= READ | WRITE;
			println(flags, names(flags));
			flags &= ~EXEC;
			flags <<= 3;
			flags >>= 4;
			println(flags);

			let packed = pack(Color { r: 0x12, g: 0x34, b: 0x56, a: 0x78 });
			let color = unpack(packed);
			println(packed, packed == 0x12345678, color.r, color.g, color.b, color.a);

			println(ones(0), ones(0b1011), ones(0xFFFF), ones(1 << 40));
			println(6 & 3, 6 | 3, 6 ^ 3, ~6, 1 << 46, -1 >> 10, -256 >> 4, 7 >> 100);
			// With signs: the bits of a negative number go on to the left for ever
			println(-1 & 0xFF, -2 | 1, -1 ^ 0xF, ~-1, (-8) >> 1);
			// Each binds tighter than a comparison
			println(5 & 4 == 4, 1 | 2 == 3, 8 >> 1 > 3, [1, 2, 3, 4] |> arrays.filter(n -> n & 1 == 0));

			match flags | WRITE {
				case 1 | 2: println("one of the first two");
				case 3: println("both");
				default: println("more");
			}
		}
	`, nil)
	if err != nil {
		t.Fatalf("runtime error: %v\nafter printing:\n%s", err, out)
	}
	expectLines(t, out,
		`7 0 ["read", "exec"] ["read", "exec"] []`,
		`6 ["write", "exec"]`,
		"1",
		"305419896 true 18 52 86 120",
		"0 3 16 1",
		"2 7 5 -7 70368744177664 -1 -16 0",
		"255 -1 -16 0 -4",
		"true true true [2, 4]",
		"both")

	// A shift that would lose bits off the top, or by less than nothing, stops the program
	for body, want := range map[string]string{
		`println(1 << 47);`:            "overflow",
		`let n = -1; println(1 << n);`: "non-negative",
		`let n = -3; println(8 >> n);`: "non-negative",
	} {
		_, err := runWith(t, 16<<20, "fn main() { "+body+" }", nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: expected a failure with %q, got %v", body, want, err)
		}
	}
}

// An if where a value is expected, in programs: chosen once, with what a
// test established, with generics, enums, closures and branches that leave.
func TestIfAsAValue(t *testing.T) {
	out, err := runWith(t, 16<<20, `
		import std.arrays;
		import std.strings;
		import std.result.{Result};

		enum Shape { Circle(Int), Square(Int) }
		struct User { name: String; manager: User?; }
		struct Box<T> { value: T; }

		fn wrap<T>(value: T) -> Box<T> { return Box<T> { value: value }; }
		fn pick<T>(first: Bool, a: T, b: T) -> T { return if first { a } else { b }; }
		fn size(n: Int) -> String { return if n > 100 { "large" } else if n > 10 { "medium" } else { "small" }; }
		fn sign(n: Int) -> Int { return if n > 0 { 1 } else if n < 0 { -1 } else { 0 }; }
		fn bossOf(user: User) -> String { return if user.manager != null { user.manager.name } else { "nobody" }; }
		fn first(xs: [Int]) -> Int? { return if arrays.isEmpty(xs) { null } else { xs[0] }; }
		fn area(shape: Shape) -> Int { return if shape is Shape.Circle(r) { 3 * r * r } else if shape is Shape.Square(side) { side * side } else { 0 }; }

		// A branch that leaves gives nothing, and need not
		fn half(n: Int) -> Result<Int, String> {
			let whole = if n % 2 == 0 { n / 2 } else { return Result.Err("${n} is odd"); };
			return Result.Ok(whole);
		}
		fn sumUntilNegative(xs: [Int]) -> Int {
			let sum = 0;
			for x in xs { sum += if x >= 0 { x } else { break; }; }
			return sum;
		}

		// Only the branch chosen is worked out
		fn loud(calls: [String], name: String, value: Int) -> Int { push(calls, name); return value; }

		fn main() {
			println(size(5), size(50), size(500), sign(9), sign(-9), sign(0));
			let ada = User { name: "ada", manager: null };
			println(bossOf(ada), bossOf(User { name: "bob", manager: ada }));
			println(first([]), first([7, 8]), area(Shape.Circle(2)), area(Shape.Square(5)));
			println(half(10), half(7), sumUntilNegative([1, 2, 3, -1, 100]));

			let calls: [String] = [];
			let chosen = if arrays.len(calls) == 0 { loud(calls, "then", 1) } else { loud(calls, "else", 2) };
			println(chosen, calls);

			// Statements before the value; names of a branch stay in it
			let n = 4;
			let label = if n > 2 {
				let twice = n * 2;
				let parts = arrays.map([n, twice], toString);
				strings.join(parts, "<")
			} else {
				"small"
			};
			println(label);

			// Anywhere a value goes
			println(1 + if n == 4 { 10 } else { 20 } * 2);
			println(wrap(if n > 0 { "positive" } else { "negative" }).value, pick(n > 9, "yes", "no"));
			println([1, 2, 3, 4] |> arrays.map(x -> if x % 2 == 0 { "even" } else { "odd" }) |> strings.join(","));
			println("${if n > 3 { "more" } else { "less" }} than three");
			let either: Int | String = if n > 9 { 1 } else { "one" };
			let none: [String] = if n > 9 { ["some"] } else { [] };
			println(either, none);
			let counter = fn(up: Bool) -> fn(Int) -> Int { return if up { x -> x + 1 } else { x -> x - 1 }; };
			println(counter(true)(10), counter(false)(10));
			let matched = match n { case 4: if label == "small" { "s" } else { "b" }; default: "?"; };
			println(matched, if (match n { case 4: true; default: false; }) { "four" } else { "other" });
		}
	`, nil)
	if err != nil {
		t.Fatalf("runtime error: %v\nafter printing:\n%s", err, out)
	}
	expectLines(t, out,
		"small medium large 1 -1 0",
		"nobody ada",
		"null 7 12 25",
		`Ok(5) Err("7 is odd") 6`,
		`1 ["then"]`,
		"4<8",
		"21",
		"positive no",
		"odd,even,odd,even",
		"more than three",
		"one []",
		"11 9",
		"b four")
}

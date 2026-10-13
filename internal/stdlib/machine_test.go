package stdlib_test

import (
	"bytes"
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

package stdlib_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/vm"
)

// program compiles and runs a whole program, on a heap of the given size
// (0 for the usual one). It gives what the program printed, how it ended
// and the machine it ran on.
func program(t *testing.T, src string, heap uint64) (string, error, *vm.VM) {
	t.Helper()
	errs := errors.NewHandler()
	result := compiler.Compile(src, "tasks.lum", errs)
	for _, d := range errs.Diagnostics {
		t.Errorf("unexpected diagnostic:\n%s", d.String())
	}
	if t.Failed() {
		t.FailNow()
	}

	var out bytes.Buffer
	machine := vm.NewVM()
	if heap > 0 {
		machine = vm.NewVMWithHeapSize(heap)
	}
	machine.SetOutput(&out)
	_, err := machine.Exec(result.Chunk)
	return out.String(), err, machine
}

// expectProgram runs a whole program and compares what it prints.
func expectProgram(t *testing.T, src string, want ...string) {
	t.Helper()
	out, err, _ := program(t, src, 0)
	if err != nil {
		t.Fatalf("runtime error: %v\nafter printing:\n%s", err, out)
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

const taskImports = `
	import std.task;
	import std.task.{Task, Channel};
	import std.result;
	import std.result.{Result, Failure, Kind};
	import std.arrays;
	import std.strings;
	import std.time;`

func TestTasksTakeTurns(t *testing.T) {
	// A task starts when the one that made it waits, runs until it waits
	// itself, and the turn goes round in the order the tasks came to it
	expect(t, taskImports, `
		let log: [String] = [];
		let a = task.start(fn() -> Int {
			log.push("a starts as ${task.current()}");
			task.pause();
			log.push("a goes on");
			return 1;
		});
		let b = task.start(fn() -> String {
			log.push("b starts as ${task.current()}");
			return "two";
		});
		log.push("main is ${task.current()}");
		println(a.isDone(), b.isDone());
		println(a.wait(), b.wait());
		println(a.isDone(), b.isDone());
		for line in log { println(line); }

		// Waiting again gives the same, and pausing alone changes nothing
		task.pause();
		println(a.wait().orElse(0) + a.wait().orElse(0), a.expect("a value"));
	`,
		"false false",
		`Ok(1) Ok("two")`,
		"true true",
		"main is 0",
		"a starts as 1",
		"b starts as 2",
		"a goes on",
		"2 1",
	)
}

func TestTaskValues(t *testing.T) {
	expectProgram(t, taskImports+`
		struct Point { x: Int; y: Int; }
		enum Shape { Circle(Float), Square(Float) }

		fn area(shape: Shape) -> Float {
			match shape {
				case Shape.Circle(r): { return 3.0 * r * r; }
				case Shape.Square(side): { return side * side; }
			}
		}

		fn main() -> Int {
			// What a task returns is whatever its function does
			let point = task.start(fn() -> Point { return Point { x: 3, y: 4 }; });
			let nothing = task.start(fn() -> Int? { return null; });
			let pair = task.start(fn() -> (String, [Int]) { return ("squares", [1, 2, 3].map(n -> n * n)); });
			let areas = task.map([Shape.Circle(1.0), Shape.Square(2.0)], area);
			println(point.wait().map(p -> p.x * p.y), nothing.wait(), pair.wait(), areas);

			// Tasks share what they close over
			let total = 0;
			let adders: [Task<Bool>] = [];
			for step in [1, 10, 100] {
				adders.push(task.run(fn() { total += step; task.pause(); total += step; }));
			}
			println(task.all(adders), total);

			// A task for what it does
			let seen: [Int] = [];
			let job = task.run(fn() { seen.push(task.current()); });
			println(job.wait(), seen.len(), task.all([job, job]));

			// Tasks that start tasks
			let nested = task.start(fn() -> Int {
				let inner = task.map([1, 2, 3], n -> task.map([n, n], m -> m * 10).expect("inner"));
				return arrays.flatten(inner.expect("outer")).sum();
			});
			println(nested.wait());
			return 0;
		}`,
		`Ok(12) Ok(null) Ok(("squares", [1, 4, 9])) Ok([3, 4])`,
		"Ok([true, true, true]) 222",
		"Ok(true) 1 Ok([true, true])",
		"Ok(120)",
	)
}

func TestTaskFailures(t *testing.T) {
	expectProgram(t, taskImports+`
		fn third(items: [Int]) -> Int { return items[2]; }

		fn main() -> Int {
			// A task that stops does not stop the others
			let bad = task.start(fn() -> Int { return third([1, 2]); });
			let good = task.start(fn() -> Int { return third([1, 2, 3]); });
			let angry = task.start(fn() -> Int { if task.current() > 0 { panic("no", "thanks"); } return 0; });
			println(good.wait());
			match bad.wait() {
				case Result.Ok(n): { println("unexpected", n); }
				case Result.Err(failure): {
					println(failure.kind == Kind.Bounds, failure.message, failure.at.function, failure.at.line, failure.stack.len());
				}
			}
			// Asked again, and by several: the same failure
			let again = task.start(fn() -> String { return bad.wait().reason()?.message ?? "none"; });
			println(again.wait(), bad.wait().isErr(), bad.isDone());
			println(angry.wait().reason()?.kind == Kind.Panic, angry.wait().reason()?.message);

			// The first failure of several, after all have ended
			let done: [Int] = [];
			let mixed = task.map([1, 0, 2, 0], fn(n: Int) -> Int { done.push(n); return 10 / n; });
			println(mixed.reason()?.message, mixed.reason()?.kind == Kind.Arithmetic, done);

			// A failure caught inside the task is the task's own business,
			// even when the task waited in the middle of the attempt
			let gate = task.channel<Int>(0);
			let careful = task.start(fn() -> String {
				let tried = result.attempt(fn() -> Int { return 100 / (gate.receive() ?? 1); });
				return tried.reason()?.message ?? "fine";
			});
			gate.send(0);
			println(careful.wait());

			// A failing task keeps the closures it made usable
			let counters: [fn() -> Int] = [];
			let maker = task.run(fn() {
				let count = 40;
				counters.push(fn() -> Int { count++; return count; });
				third([]);
			});
			println(maker.wait().isErr(), counters[0](), counters[0]());
			return 0;
		}`,
		"Ok(3)",
		"true index out of bounds: 2 (length 2) third 9 2",
		`Ok("index out of bounds: 2 (length 2)") true true`,
		"true no thanks",
		"division by zero true [1, 0, 2, 0]",
		`Ok("division by zero")`,
		"true 41 42",
	)
}

func TestChannels(t *testing.T) {
	expectProgram(t, taskImports+`
		fn main() -> Int {
			// Without room: each send waits for its receive
			let log: [String] = [];
			let hand = task.channel<Int>(0);
			let sender = task.run(fn() {
				for n in [1, 2, 3] { log.push("send ${n}"); hand.send(n); log.push("sent ${n}"); }
				hand.close();
			});
			hand.each(fn(n: Int) { log.push("got ${n}"); });
			println(log.join(", "));
			println(sender.wait(), hand.receive(), hand.isClosed(), hand.length());

			// With room for two: the sender runs ahead by two, then waits
			let trace: [String] = [];
			let queue = task.channel<String>(2);
			task.run(fn() {
				for word in ["a", "b", "c", "d"] { queue.send(word); trace.push("+${word}"); }
				queue.close();
			});
			task.pause();
			trace.push("len ${queue.length()}");
			queue.each(fn(word: String) { trace.push("-${word}"); });
			println(trace.join(" "));

			// What was sent before closing is still received
			let last = task.channel<Int>(3);
			last.send(7);
			last.send(8);
			last.close();
			last.close();
			println(last.receive(), last.drain(), last.receive());

			// A value may be null: each and drain tell it from the end
			let maybe = task.channel<Int?>(4);
			maybe.send(1);
			maybe.send(null);
			maybe.send(3);
			maybe.close();
			println(maybe.drain());

			// Sending once closed is a mistake, for whoever does it
			let shut = task.channel<Int>(1);
			shut.close();
			let late = task.run(fn() { shut.send(1); });
			println(late.wait().reason()?.message);
			let waiting = task.channel<Int>(0);
			let stuck = task.run(fn() { waiting.send(1); });
			task.pause();
			waiting.close();
			println(stuck.wait(), waiting.receive(), waiting.receive());

			// Several senders, several receivers: nothing lost, nothing twice
			let jobs = task.channel<Int>(4);
			let results = task.channel<Int>(0);
			let workers: [Task<Bool>] = [];
			for worker in [1, 2, 3] {
				workers.push(task.run(fn() {
					jobs.each(fn(n: Int) { task.pause(); results.send(n * n); });
				}));
			}
			let feeders = [
				task.run(fn() { for n in arrays.range(0, 50) { jobs.send(n); } }),
				task.run(fn() { for n in arrays.range(50, 100) { jobs.send(n); } }),
			];
			task.run(fn() {
				task.all(feeders);
				jobs.close();
				task.all(workers);
				results.close();
			});
			let squares = results.drain();
			println(squares.len(), squares.sum(), arrays.range(0, 100).map(n -> n * n).sum());

			// A long queue, emptied as it fills
			let stream = task.channel<Int>(1000);
			task.run(fn() { for n in arrays.range(0, 5000) { stream.send(n); } stream.close(); });
			let count = 0;
			let inOrder = true;
			stream.each(fn(n: Int) { if n != count { inOrder = false; } count++; });
			println(count, inOrder, stream.length());
			return 0;
		}`,
		"send 1, got 1, sent 1, send 2, got 2, sent 2, send 3, got 3, sent 3",
		"Ok(true) null true 0",
		"+a +b len 2 -a -b +c +d -c -d",
		"7 [8] null",
		"[1, null, 3]",
		"send on a closed channel",
		"Ok(true) 1 null",
		"100 328350 328350",
		"5000 true 0",
	)
	expectFailure(t, taskImports, `task.channel<Int>(-1);`, "a channel cannot hold -1 values")
}

func TestTasksWaitForTheClock(t *testing.T) {
	// Tasks that sleep do so at the same time, and wake in the order of
	// their clocks whatever the order they were started in
	started := time.Now()
	expect(t, taskImports, `
		let order: [Int] = [];
		let naps: [Task<Bool>] = [];
		for ms in [120, 40, 80] {
			naps.push(task.run(fn() { time.sleep(ms); order.push(ms); }));
		}
		println(task.all(naps).isOk(), order);

		// main sleeps too, while a task works
		let ticks = 0;
		let ticker = task.run(fn() { for i in arrays.range(0, 5) { time.sleep(5); ticks++; } });
		time.sleep(100);
		println(ticks, ticker.isDone());
	`,
		"true [40, 80, 120]",
		"5 true",
	)
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("expected the sleeps to overlap, the program took %v", took)
	}
}

func TestProgramEndsWithItsTasks(t *testing.T) {
	// main has returned: the tasks still going reach their end
	expect(t, taskImports, `
		let lines = task.channel<String>(0);
		task.run(fn() { lines.each(fn(line: String) { println("printed", line); }); println("printer done"); });
		task.run(fn() { time.sleep(10); lines.send("late"); lines.close(); });
		lines.send("early");
		println("main done");
	`,
		"printed early",
		"main done",
		"printed late",
		"printer done",
	)

	// A failure nobody waited for is the program's
	out, err := run(t, taskImports, `
		task.run(fn() { println("working"); let items = [1]; println(items[9]); });
		task.run(fn() { time.sleep(10); println("the others finish"); });
		println("main done");
	`, "")
	if out != "main done\nworking\nthe others finish\n" {
		t.Errorf("unexpected output: %q", out)
	}
	if failure, ok := err.(*vm.RuntimeError); !ok || failure.Kind != vm.KindBounds || !strings.Contains(failure.Message, "index out of bounds: 9") {
		t.Errorf("expected the task's failure to end the program, got %v", err)
	}

	// ...but not one that was waited for
	expect(t, taskImports, `
		let failing = task.run(fn() { panic("handled"); });
		println(failing.wait().reason()?.message);
	`, "handled")

	// main failing ends everything at once
	out, err = run(t, taskImports, `
		task.run(fn() { time.sleep(5000); println("never"); });
		task.run(fn() { println("once"); task.channel<Int>(0).receive(); println("never"); });
		task.pause();
		if task.current() == 0 { panic("main gives up"); }
	`, "")
	if out != "once\n" || err == nil || !strings.Contains(err.Error(), "main gives up") {
		t.Errorf("expected main's failure to end the program, got %q and %v", out, err)
	}

	// A task can end the program, whatever the others are doing
	started := time.Now()
	out, err = run(t, taskImports+"\nimport std.os;", `
		task.run(fn() { time.sleep(5000); println("never"); });
		task.run(fn() { time.sleep(20); println("leaving"); os.exit(7); });
		let never = task.channel<Int>(0);
		result.attempt(fn() -> Int? { return never.receive(); });
		println("never");
	`, "")
	if code, leaving := vm.ExitCode(err); out != "leaving\n" || !leaving || code != 7 {
		t.Errorf("expected the program to exit with 7, got %q and %v", out, err)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("expected the exit not to wait for the sleeping task, it took %v", took)
	}
}

func TestDeadlocks(t *testing.T) {
	deadlock := func(body string, message string, line int) {
		t.Helper()
		out, err := run(t, taskImports, body, "")
		failure, ok := err.(*vm.RuntimeError)
		if !ok || failure.Kind != vm.KindDeadlock || failure.Code() != "R0009" {
			t.Fatalf("expected a deadlock, got %v after printing %q", err, out)
		}
		if failure.Message != message {
			t.Errorf("expected the message %q, got %q", message, failure.Message)
		}
		// The place is in the program, not in the library. The body of
		// main comes after the imports and the line that opens main
		want := strings.Count(taskImports, "\n") + 2 + line
		if at := failure.Stack[0]; at.File != "test.lum" || at.Line != want {
			t.Errorf("expected line %d of the program, got %s:%d", want, at.File, at.Line)
		}
		if failure.Suggestion() == "" {
			t.Error("expected a suggestion")
		}
	}

	// Alone, waiting for a value nobody can send
	deadlock(`
		let empty = task.channel<Int>(1);
		empty.receive();`,
		"deadlock: the task waits for something no other task is left to do", 3)

	// Two tasks waiting for each other; nothing attempt can catch. The
	// place is where the last of them went to wait
	deadlock(`
		let there = task.channel<Int>(0);
		let back = task.channel<Int>(0);
		let other = task.start(fn() -> Int { return back.receive() ?? 0; });
		println(result.attempt(fn() -> Int? { return there.receive(); }));`,
		"deadlock: 2 tasks wait for each other, and none can go on", 4)

	// Found when the last task that could have helped ends
	deadlock(`
		let never = task.channel<Int>(0);
		let quitter = task.run(fn() { task.pause(); });
		never.send(1);`,
		"deadlock: the task waits for something no other task is left to do", 4)

	// After main has returned: a task left waiting
	deadlock(`
		let forgotten = task.channel<Int>(0);
		task.run(fn() {
			forgotten.each(fn(n: Int) { println(n); });
		});
		forgotten.send(1);`,
		"deadlock: the task waits for something no other task is left to do", 4)

	// A task that sleeps is not waiting for another: no deadlock yet
	expect(t, taskImports, `
		let slow = task.channel<Int>(0);
		task.run(fn() { time.sleep(30); slow.send(5); });
		println(slow.receive());
	`, "5")
}

func TestTasksUnderMemoryPressure(t *testing.T) {
	// Tasks that wait hold values nothing else does: in their variables,
	// in channels, in what they are about to return. The collector runs
	// many times while they do.
	out, err, machine := program(t, taskImports+`
		struct Batch { name: String; rows: [String]; }

		fn build(worker: Int, round: Int) -> Batch {
			let rows: [String] = [];
			for i in arrays.range(0, 200) { rows.push("worker ${worker} round ${round} row ${i}"); }
			return Batch { name: "batch ${worker}.${round}", rows: rows };
		}

		fn main() -> Int {
			let batches = task.channel<Batch>(2);
			let workers: [Task<Int>] = [];
			for worker in arrays.range(0, 6) {
				workers.push(task.start(fn() -> Int {
					let made = 0;
					for round in arrays.range(0, 40) {
						let mine = build(worker, round);
						task.pause();
						if mine.rows[199] != "worker ${worker} round ${round} row 199" { panic("${mine.name} was spoiled"); }
						batches.send(mine);
						made++;
					}
					return made;
				}));
			}
			task.run(fn() { task.all(workers); batches.close(); });

			let rows = 0;
			let names = 0;
			batches.each(fn(batch: Batch) {
				if !batch.rows[0].startsWith("worker ") || !batch.name.startsWith("batch ") { panic("a batch was spoiled"); }
				rows += batch.rows.len();
				names++;
			});
			println(rows, names, task.all(workers).map(made -> made.sum()));
			return 0;
		}`, 4<<20)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if out != "48000 240 Ok(240)\n" {
		t.Errorf("unexpected output: %q", out)
	}
	if machine.Collections() < 5 {
		t.Errorf("expected the heap to be collected many times, got %d", machine.Collections())
	}
}

func TestManyTasks(t *testing.T) {
	expectProgram(t, taskImports+`
		// A task that passes on what it gets, plus one
		fn passOn(from: Channel<Int>, to: Channel<Int>) {
			task.run(fn() { to.send((from.receive() ?? 0) + 1); });
		}

		fn main() -> Int {
			let ring: [Channel<Int>] = [];
			for i in arrays.range(0, 300) { ring.push(task.channel<Int>(0)); }
			for i in arrays.range(0, 299) { passOn(ring[i], ring[i + 1]); }
			ring[0].send(0);
			println(ring[299].receive());

			println(task.map(arrays.range(0, 2000), n -> n % 7).map(all -> all.sum()));
			return 0;
		}`,
		"299",
		"Ok(5995)",
	)
}

func TestServerAnswersSeveralRequestsAtOnce(t *testing.T) {
	const requests = 8
	var answers [requests]string
	started := time.Now()
	printed := serve(t, `
		import std.http;
		import std.http.{Request, Response};
		import std.task;
		import std.time;
		import std.strings;
	`, `
		let inProgress = 0;
		let most = 0;
		let app = http.router();
		app.get("/slow/:n", fn(request: Request) -> Response {
			inProgress++;
			if inProgress > most { most = inProgress; }
			time.sleep(150);
			inProgress--;
			return http.text("slow ${request.params.get("n") ?? "?"}");
		})
			.get("/boom", fn(request: Request) -> Response { let items = [1]; return http.text(toString(items[5])); })
			.get("/stop", fn(request: Request) -> Response { return http.text("stopping"); });

		let server = http.listen(0).expect("a free port");
		println(server.port);
		let serving = task.run(fn() { server.handleAll(app.handler()); });

		// Meanwhile the program does something else
		let beats = 0;
		while most < 8 || inProgress > 0 { time.sleep(10); beats++; }
		server.close();
		println(serving.wait(), most, beats > 5);
	`, func(base string) {
		var wait sync.WaitGroup
		for i := 0; i < requests; i++ {
			wait.Add(1)
			go func(i int) {
				defer wait.Done()
				response, err := http.Get(fmt.Sprintf("%s/slow/%d", base, i))
				if err != nil {
					answers[i] = err.Error()
					return
				}
				defer response.Body.Close()
				body, _ := io.ReadAll(response.Body)
				answers[i] = fmt.Sprintf("%d %s", response.StatusCode, body)
			}(i)
		}
		// A handler that fails, in the middle of the others
		if response, err := http.Get(base + "/boom"); err != nil || response.StatusCode != 500 {
			t.Errorf("expected 500 from the handler that fails, got %v %v", response, err)
		}
		wait.Wait()
	})

	for i, answer := range answers {
		if want := fmt.Sprintf("200 slow %d", i); answer != want {
			t.Errorf("request %d: want %q, got %q", i, want, answer)
		}
	}
	if !strings.HasSuffix(printed, "Ok(true) 8 true\n") {
		t.Errorf("expected the requests to be in progress together, got %q", printed)
	}
	// Eight requests of 150 ms each, one after the other, take 1.2 s
	if took := time.Since(started); took > 900*time.Millisecond {
		t.Errorf("expected the requests to overlap, they took %v", took)
	}
}

func TestClientAsksSeveralServersAtOnce(t *testing.T) {
	origin := http.NewServeMux()
	origin.HandleFunc("/wait/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		fmt.Fprintf(w, "page %s", strings.TrimPrefix(r.URL.Path, "/wait/"))
	})
	base := newLocalServer(t, origin)

	started := time.Now()
	expect(t, taskImports+"\nimport std.http;", `
		let base = `+quote(base)+`;
		let pages = task.map(arrays.range(0, 6), n -> http.get("${base}/wait/${n}").map(response -> response.body).orElse("no answer"));
		println(pages);

		// One that gets no answer does not hold the others up
		let dead = task.start(fn() -> Bool { return http.get("http://127.0.0.1:1/").isErr(); });
		let alive = task.start(fn() -> Int { return http.get(base + "/wait/x").map(response -> response.status).orElse(0); });
		println(dead.wait(), alive.wait());
	`,
		`Ok(["page 0", "page 1", "page 2", "page 3", "page 4", "page 5"])`,
		"Ok(true) Ok(200)",
	)
	if took := time.Since(started); took > 800*time.Millisecond {
		t.Errorf("expected the requests to overlap, they took %v", took)
	}
}

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuiltProgramsAreSmallAndSayTheSame builds programs to bytecode and
// checks the two things that shrinking a file could cost: that the program
// does what it did, and that when it fails it says where exactly as it
// does when run from source.
func TestBuiltProgramsAreSmallAndSayTheSame(t *testing.T) {
	dir := project(t, map[string]string{
		// Brings in most of the library and uses a little of it
		"report.lum": `
			import std.arrays;
			import std.dict;
			import std.json;
			import std.strings;
			import std.regex;
			import std.csv;
			import std.time;

			struct Sale { region: String; amount: Int; }

			fn main() -> Int {
				let sales = [Sale { region: "north", amount: 120 }, Sale { region: "south", amount: 80 }, Sale { region: "north", amount: 45 }];
				let totals = dict.empty<Int>();
				for sale in sales { totals.update(sale.region, 0, n -> n + sale.amount); }
				for region in totals.sortedKeys() {
					println(strings.padRight(region, 8), totals.getOr(region, 0));
				}
				println(arrays.sumOf(sales, sale -> sale.amount));
				return 0;
			}`,
		// Fails three calls down: in a closure, called by the library,
		// with a cleanup on the way out
		"failing.lum": `
			import std.arrays;

			struct Reading { sensor: String; values: [Int]; }

			fn latest(reading: Reading) -> Int {
				defer println("checked", reading.sensor);
				return reading.values[arrays.len(reading.values)];
			}

			fn main() -> Int {
				let readings = [Reading { sensor: "a", values: [1, 2] }];
				let pick = fn(reading: Reading) -> Int { return latest(reading) * 2; };
				println(arrays.map(readings, pick));
				return 0;
			}`,
	})

	build := func(source string, options ...string) (string, int) {
		t.Helper()
		out := filepath.Join(t.TempDir(), "program.lc")
		args := append([]string{"build"}, options...)
		if res := runLumen(t, append(args, filepath.Join(dir, source), out)...); res.exit != 0 {
			t.Fatalf("build of %s failed:\n%s", source, res.stderr)
		}
		info, err := os.Stat(out)
		if err != nil {
			t.Fatal(err)
		}
		return out, int(info.Size())
	}

	t.Run("only what is used is in the file", func(t *testing.T) {
		built, size := build("report.lum")
		// The whole library is some hundreds of kilobytes compiled
		if size > 40_000 {
			t.Errorf("a program using a little of seven modules compiles to %d bytes", size)
		}
		fromSource := runLumen(t, "run", filepath.Join(dir, "report.lum"))
		fromFile := runLumen(t, "exec", built)
		want := "north    165\nsouth    80\n245\n"
		if fromSource.stdout != want || !strings.HasPrefix(fromFile.stdout, want) || fromSource.exit != 0 || fromFile.exit != 0 {
			t.Errorf("from source: %q (exit %d)\nfrom the file: %q (exit %d)\n%s%s", fromSource.stdout, fromSource.exit, fromFile.stdout, fromFile.exit, fromSource.stderr, fromFile.stderr)
		}

		data, _ := os.ReadFile(built)
		// A function of a module that was imported and never called
		for _, absent := range []string{"std.regex.", "std.csv.", "std.json.", "std.time."} {
			if strings.Contains(string(data), absent) {
				t.Errorf("the file names %s, which the program never uses", absent)
			}
		}
		if !strings.Contains(string(data), "sortedKeys") {
			t.Error("the file does not hold a function the program calls")
		}
	})

	t.Run("a failure says the same from a file", func(t *testing.T) {
		built, _ := build("failing.lum")
		fromSource := runLumen(t, "run", filepath.Join(dir, "failing.lum"))
		fromFile := runLumen(t, "exec", built)

		// exec says first that it was executing; the rest is the message
		message := strings.TrimSpace(strings.TrimPrefix(fromFile.stderr, "Execution error: "))
		if message != strings.TrimSpace(fromSource.stderr) {
			t.Errorf("from source:\n%s\nfrom the file:\n%s", fromSource.stderr, fromFile.stderr)
		}
		if fromSource.stdout != "checked a\n" || fromFile.stdout != "checked a\n" || fromSource.exit != 1 || fromFile.exit != 1 {
			t.Errorf("from source: %q (exit %d), from the file: %q (exit %d)", fromSource.stdout, fromSource.exit, fromFile.stdout, fromFile.exit)
		}
		for _, want := range []string{
			"R0002", "index out of bounds: 2 (length 2)",
			"failing.lum:8:26 in function 'latest'",
			"return reading.values[arrays.len(reading.values)];",
			"called from", "in function 'lambda_0'",
			"<std>/arrays.lum", "in function 'main'",
		} {
			if !strings.Contains(message, want) {
				t.Errorf("expected %q in:\n%s", want, message)
			}
		}
	})

	t.Run("stripped", func(t *testing.T) {
		_, whole := build("failing.lum")
		built, size := build("failing.lum", "--strip")
		if size >= whole {
			t.Errorf("stripped: %d bytes, with debug information: %d", size, whole)
		}
		res := runLumen(t, "exec", built)
		// The same program: the cleanup runs, the same failure ends it
		if res.stdout != "checked a\n" || res.exit != 1 {
			t.Errorf("wrote %q, exit %d", res.stdout, res.exit)
		}
		if !strings.Contains(res.stderr, "R0002") || !strings.Contains(res.stderr, "index out of bounds: 2 (length 2)") || !strings.Contains(res.stderr, "at instruction") {
			t.Errorf("expected what failed and at which instruction:\n%s", res.stderr)
		}
		if strings.Contains(res.stderr, "failing.lum") || strings.Contains(res.stderr, "reading.values") {
			t.Errorf("a stripped program still knows its source:\n%s", res.stderr)
		}
		data, _ := os.ReadFile(built)
		if strings.Contains(string(data), "return reading.values") {
			t.Error("the stripped file holds a line of the source")
		}

		// The option is lumen's wherever it is written, and run ignores it
		after := filepath.Join(t.TempDir(), "after.lc")
		if res := runLumen(t, "build", filepath.Join(dir, "failing.lum"), after, "--strip"); res.exit != 0 {
			t.Fatalf("build with the option last failed:\n%s", res.stderr)
		}
		if info, _ := os.Stat(after); info == nil || int(info.Size()) != size {
			t.Errorf("the option written last gave another file")
		}
	})
}

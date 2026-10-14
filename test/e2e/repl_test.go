package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The REPL fed from a file is the REPL typed at, without the prompt: what
// these sessions check is what someone at a terminal gets.

func TestReplSession(t *testing.T) {
	session := `import std.arrays;
import std.strings;
import std.result.{Result};

enum Movement { In(Int), Out(Int) }

struct Item {
    name: String;
    stock: Int = 0;
    history: [Movement] = [];
}

impl Item {
    fn apply(move: Movement) -> Result<Int, String> {
        match move {
            case Movement.In(n): { self.stock += n; }
            case Movement.Out(n): {
                if n > self.stock { return Result.Err("only ${self.stock} ${self.name} left"); }
                self.stock -= n;
            }
        }
        self.history.push(move);
        return Result.Ok(self.stock);
    }
}

let bolts = Item { name: "bolts" }
bolts.apply(Movement.In(40))
bolts.apply(Movement.Out(15))
bolts.apply(Movement.Out(100))
let shelf = [bolts, Item { name: "nuts", stock: 7 }]
let low = fn(limit: Int) -> [String] {
    return shelf.filter(item -> item.stock < limit).map(item -> item.name);
};
low(10)
shelf[1].apply(Movement.In(50)); low(10)
bolts.stock = 3
low(10)
:type low
:vars
bolts.history.map(m -> toString(m)).join(" then ")
`
	res := runLumenWithInput(t, session, "repl")
	want := `Ok(40)
Ok(25)
Err("only 25 bolts left")
["nuts"]
[]
["bolts"]
fn(Int) -> [String]
bolts: Item = {history: [In(40), Out(15)], name: "bolts", stock: 3}
shelf: [Item] = [{history: [In(40), Out(15)], name: "bolts", stock: 3}, {history: [In(50)], n...
low: fn(Int) -> [String] = <fn>
"In(40) then Out(15)"
`
	if res.exit != 0 || res.stderr != "" {
		t.Fatalf("exit %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if res.stdout != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", res.stdout, want)
	}
}

func TestReplFailuresAndStatus(t *testing.T) {
	// A session with a mistake goes on, and says so when it ends
	res := runLumenWithInput(t, "let xs = [1, 2]\nxs[5]\nundefined\nxs[1]\n", "repl")
	if res.exit != 1 {
		t.Fatalf("exit %d, want 1", res.exit)
	}
	if res.stdout != "2\n" {
		t.Fatalf("stdout: %q", res.stdout)
	}
	for _, part := range []string{"R0002", "--> [2]:1:3", "E1001", "--> [3]:1:1"} {
		if !strings.Contains(res.stderr, part) {
			t.Fatalf("stderr does not mention %q:\n%s", part, res.stderr)
		}
	}
	if strings.Contains(res.stderr, "__repl") || strings.Contains(res.stderr, "repl.lum") {
		t.Fatalf("what the REPL writes around an input shows:\n%s", res.stderr)
	}

	// The program's own exit is the session's, and what follows is not run
	res = runLumenWithInput(t, "import std.os;\nprintln(\"before\")\nos.exit(42)\nprintln(\"after\")\n", "repl")
	if res.exit != 42 || res.stdout != "before\n" {
		t.Fatalf("exit %d, stdout %q", res.exit, res.stdout)
	}

	res = runLumenWithInput(t, "1 + 1\n:quit\n2 + 2\n", "repl")
	if res.exit != 0 || res.stdout != "2\n" {
		t.Fatalf("exit %d, stdout %q", res.exit, res.stdout)
	}

	// An input left open when the session ends is reported, not dropped
	res = runLumenWithInput(t, "fn open() {\n    println(1);\n", "repl")
	if res.exit != 1 || !strings.Contains(res.stderr, "E0") {
		t.Fatalf("exit %d\n%s", res.exit, res.stderr)
	}
}

func TestReplProgramReadsTheSameInput(t *testing.T) {
	session := "import std.io;\nlet name = io.readLine() ?? \"nobody\"\nAda Lovelace\n\"hello, ${name}\"\n"
	res := runLumenWithInput(t, session, "repl")
	if res.exit != 0 || res.stdout != "\"hello, Ada Lovelace\"\n" {
		t.Fatalf("exit %d, stdout %q\n%s", res.exit, res.stdout, res.stderr)
	}
}

func TestReplLimits(t *testing.T) {
	res := runLumenWithInput(t, "fn down(n: Int) -> Int { return down(n + 1); }\ndown(0)\n\"still here\"\n", "repl", "--max-depth=50")
	if !strings.Contains(res.stderr, "R0005") || !strings.Contains(res.stderr, "50") {
		t.Fatalf("stderr:\n%s", res.stderr)
	}
	if res.stdout != "\"still here\"\n" {
		t.Fatalf("stdout: %q", res.stdout)
	}

	res = runLumenWithInput(t, "let all: [[Int]] = []\nimport std.arrays;\nwhile true { all.push(arrays.fill(100000, 1)); }\nall.len() > 0\n", "repl", "--heap-mb=8")
	if !strings.Contains(res.stderr, "R0006") {
		t.Fatalf("stderr:\n%s", res.stderr)
	}
	if res.stdout != "true\n" {
		t.Fatalf("stdout: %q", res.stdout)
	}
}

func TestReplInAProject(t *testing.T) {
	dir := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("app/lumen.json", `{"root": "src", "packages": {"kit": "../libs/kit"}}`)
	write("app/src/stock.lum", "pub struct Crate { count: Int; }\npub fn open() -> Crate { return Crate { count: 12 }; }\n")
	write("libs/kit/text.lum", "pub fn shout(s: String) -> String { return s + \"!\"; }\n")
	write("app/src/tools/loose.lum", "import kit.text;\n\nfn label(n: Int) -> String { return text.shout(toString(n)); }\n\nfn main() -> Int { println(label(1)); return 0; }\n")

	session := "import stock;\nimport kit.text;\nlet crate = stock.open()\ntext.shout(toString(crate.count))\n:load src/tools/loose.lum\nlabel(crate.count + 1)\n:vars\n"
	res := runLumenIn(t, filepath.Join(dir, "app"), session, "repl")
	want := "\"12!\"\n\"13!\"\ncrate: stock.Crate = {count: 12}\n"
	if res.exit != 0 || res.stdout != want {
		t.Fatalf("exit %d, stdout %q\n%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "main of loose.lum is left out") {
		t.Fatalf("stderr:\n%s", res.stderr)
	}
}

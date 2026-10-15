// Package native checks lumen native against lumen run: every program the
// other tests run, compiled to Go, must print the same, fail the same way
// and end with the same status as its source run by the interpreter.
//
// All of them are compiled into one executable, which takes the name of
// the program to run: one go build for all.
package native

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/memguard"
	"github.com/dani-gouken/lumen/internal/native"
	"github.com/dani-gouken/lumen/internal/vm"
)

const repoRoot = "../.."

// runTimeout bounds one run of a program.
const runTimeout = 30 * time.Second

func TestMain(m *testing.M) {
	memguard.EnforceInTests()
	// Programs are named from the root, as lumen run is given them there
	if err := os.Chdir(repoRoot); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// program is one program to run both ways.
type program struct {
	name  string // A Go identifier
	path  string // From the root
	args  []string
	chunk *vm.Chunk
}

// windowed are the examples that open a window and wait.
var windowed = map[string]bool{"examples/raylib.lum": true}

// collect compiles the programs the other tests run, and those of the
// benchmarks. One that does not compile is left out: there is nothing to
// compare.
func collect(t *testing.T) []program {
	var paths []string
	for _, pattern := range []string{"examples/*.lum", "test/e2e/cases/*.lum", "test/bench/programs/*.lum"} {
		found, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, found...)
	}
	sort.Strings(paths)

	var programs []program
	for _, path := range paths {
		if windowed[path] {
			continue
		}
		errs := errors.NewHandler()
		result, err := compiler.CompileFile(path, errs)
		if err != nil || errs.HasErrors() {
			continue
		}
		programs = append(programs, program{name: fmt.Sprintf("P%d", len(programs)), path: path, chunk: result.Chunk})
	}

	// The Lox interpreter, a program of its own size, on a program of its
	// language
	errs := errors.NewHandler()
	lox, err := compiler.CompileFile("examples/lox/main.lum", errs)
	if err != nil || errs.HasErrors() {
		t.Fatalf("the Lox interpreter does not compile: %v %v", err, errs.Diagnostics)
	}
	for _, script := range []string{"test/bench/lox/fib.lox", "test/bench/lox/shapes.lox"} {
		programs = append(programs, program{name: fmt.Sprintf("P%d", len(programs)), path: "examples/lox/main.lum", args: []string{script}, chunk: lox.Chunk})
	}
	return programs
}

// harness writes the main of the executable: the program named by the
// first argument, run as lumen run runs its source.
func harness(programs []program) (string, error) {
	var out strings.Builder
	out.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"runtime\"\n\n")
	out.WriteString("\t\"github.com/dani-gouken/lumen/internal/runner\"\n\t\"github.com/dani-gouken/lumen/internal/vm\"\n")
	out.WriteString("\t_ \"github.com/dani-gouken/lumen/internal/inspect\"\n)\n\n")
	out.WriteString("func init() { runtime.LockOSThread() }\n\n")
	out.WriteString("type program struct {\n\tpath     string\n\tbytecode []byte\n\tcompiled []vm.CompiledFunc\n}\n\n")
	out.WriteString("var programs = map[string]program{\n")
	for _, p := range programs {
		bytecode, err := vm.SerializeChunk(p.chunk)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "\t%q: {%q, []byte(%q), vm.%s},\n", p.name, p.path, bytecode, native.Table(p.name))
	}
	out.WriteString("}\n\n")
	out.WriteString(`func main() {
	p := programs[os.Args[1]]
	chunk, err := vm.DeserializeChunk(p.bytecode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	chunk.Compiled = p.compiled
	machine := runner.Machine(runner.Limits{}, os.Args[2:], p.path, os.Exit)
	result, err := machine.Exec(chunk)
	runner.Finish(result, err, os.Exit)
}
`)
	return out.String(), nil
}

type outcome struct {
	stdout, stderr string
	exit           int
}

func run(t *testing.T, name string, args ...string) outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %v took longer than %v", name, args, runTimeout)
	}
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return outcome{stdout.String(), stderr.String(), code}
}

func TestNativeDoesWhatTheInterpreterDoes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds every program twice")
	}
	dir := t.TempDir()
	lumen := filepath.Join(dir, "lumen")
	if out, err := exec.Command("go", "build", "-o", lumen, "./cmd/lumen").CombinedOutput(); err != nil {
		t.Fatalf("cannot build lumen: %v\n%s", err, out)
	}

	programs := collect(t)
	var named []native.Program
	for _, p := range programs {
		named = append(named, native.Program{Name: p.name, Chunk: p.chunk})
	}
	generated := native.Generate(named...)
	t.Logf("%d programs, %d of %d functions compiled to Go", len(programs), generated.Compiled, generated.Total)
	if generated.Compiled != generated.Total {
		t.Errorf("%d functions stayed bytecode", generated.Total-generated.Compiled)
	}
	main, err := harness(programs)
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "programs")
	if err := (native.Source{Root: ".", Go: "go"}).Build(generated.Source, main, executable); err != nil {
		t.Fatal(err)
	}

	for _, p := range programs {
		t.Run(strings.TrimSuffix(p.path, ".lum")+strings.Join(p.args, "_"), func(t *testing.T) {
			interpreted := run(t, lumen, append([]string{"run", p.path}, p.args...)...)
			// The compiler's warnings come before the program runs: lumen
			// native says them when it builds
			interpreted.stderr = withoutWarnings(interpreted.stderr)
			compiled := run(t, executable, append([]string{p.name}, p.args...)...)
			if compiled != interpreted {
				t.Errorf("lumen run: exit %d\n%s%s\n--- native: exit %d\n%s%s",
					interpreted.exit, interpreted.stdout, interpreted.stderr, compiled.exit, compiled.stdout, compiled.stderr)
			}
		})
	}
}

// withoutWarnings leaves out the warnings at the start of what lumen run
// wrote on stderr: each a block that ends with an empty line.
func withoutWarnings(stderr string) string {
	for strings.HasPrefix(stderr, "[WARNING]") {
		end := strings.Index(stderr, "\n\n")
		if end < 0 {
			return ""
		}
		stderr = strings.TrimLeft(stderr[end+2:], "\n")
	}
	return stderr
}

// buildLumen builds the lumen command for a test.
func buildLumen(t *testing.T) string {
	t.Helper()
	lumen := filepath.Join(t.TempDir(), "lumen")
	if out, err := exec.Command("go", "build", "-o", lumen, "./cmd/lumen").CombinedOutput(); err != nil {
		t.Fatalf("cannot build lumen: %v\n%s", err, out)
	}
	return lumen
}

// runIn runs a command in a folder.
func runIn(t *testing.T, dir, name string, args ...string) outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %v took too long", name, args)
	}
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return outcome{stdout.String(), stderr.String(), code}
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestNativeCommand covers lumen native: what it names the executable,
// what the executable takes from the command line and the project, how it
// fails, and what is refused.
func TestNativeCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("builds executables")
	}
	lumen, _ := filepath.Abs(buildLumen(t))
	t.Setenv("LUMEN_CACHE", t.TempDir())
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"greet.lum": `import std.os;

fn main() -> Int {
    let names = os.args();

    for name in names {
        println("hello", name);
    }

    if names.isEmpty() {
        os.exit(4);
    }

    return names.len();
}
`,
		"broken.lum": `fn main() -> Int {
    let items = [1, 2, 3];
    println("before");
    return items[7];
}
`,
		"wrong.lum": `fn main() -> Int {
    return "text";
}
`,
		"deep/lumen.json": `{ "name": "climber", "entry": "main.lum", "max_depth": 30000 }`,
		"deep/main.lum": `fn climb(n: Int) -> Int {
    if n == 0 {
        return 0;
    }

    return 1 + climb(n - 1);
}

fn main() -> Int {
    println(climb(20000));
    return 0;
}
`,
		"checker.lum": `import std.compiler;

fn main() -> Int {
    let problems = compiler.check("x.lum", "fn main() -> Int { return true; }");
    println(problems.len() > 0, problems[0].message.contains("returning int"));
    return 0;
}
`,
	})

	// The executable is named after the file, in the current folder
	built := runIn(t, dir, lumen, "native", "greet.lum")
	if built.exit != 0 || !strings.Contains(built.stdout, "-> greet (") {
		t.Fatalf("native: exit %d\n%s%s", built.exit, built.stdout, built.stderr)
	}
	executable := filepath.Join(dir, "greet")
	res := runIn(t, dir, executable, "ada", "--bob")
	if res.exit != 2 || res.stdout != "hello ada\nhello --bob\n" || res.stderr != "" {
		t.Errorf("greet ada --bob: exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}
	// std.os.exit ends it with that status
	if res := runIn(t, dir, executable); res.exit != 4 || res.stdout != "" {
		t.Errorf("greet: exit %d, %q", res.exit, res.stdout)
	}

	// -o names it; a failure says what it says under lumen run
	if built := runIn(t, dir, lumen, "native", "broken.lum", "-o", "out/broken-app"); built.exit != 0 {
		t.Fatalf("native -o: exit %d\n%s", built.exit, built.stderr)
	}
	interpreted := runIn(t, dir, lumen, "run", "broken.lum")
	compiled := runIn(t, dir, filepath.Join(dir, "out", "broken-app"))
	if compiled != interpreted || compiled.exit != 1 || !strings.Contains(compiled.stderr, "index out of bounds: 7 (length 3)") || !strings.Contains(compiled.stderr, "broken.lum:4:") {
		t.Errorf("expected the failure lumen run gives\n--- run: exit %d\n%s%s--- native: exit %d\n%s%s", interpreted.exit, interpreted.stdout, interpreted.stderr, compiled.exit, compiled.stdout, compiled.stderr)
	}

	// A project: the executable has its name and its limits
	if built := runIn(t, dir, lumen, "native", "deep"); built.exit != 0 || !strings.Contains(built.stdout, "-> climber (") {
		t.Fatalf("native of a project: exit %d\n%s%s", built.exit, built.stdout, built.stderr)
	}
	if res := runIn(t, dir, filepath.Join(dir, "climber")); res.exit != 0 || res.stdout != "20000\n" {
		t.Errorf("climber, deeper than the default depth: exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}
	// ...and the command line wins over the project, as for run
	if built := runIn(t, dir, lumen, "native", "deep", "-o", "shallow", "--max-depth=100"); built.exit != 0 {
		t.Fatalf("native --max-depth: exit %d\n%s", built.exit, built.stderr)
	}
	if res := runIn(t, dir, filepath.Join(dir, "shallow")); res.exit != 1 || !strings.Contains(res.stderr, "max call depth 100 exceeded") {
		t.Errorf("expected the depth the command line set, got exit %d\n%s", res.exit, res.stderr)
	}

	// A program that uses std.compiler carries the compiler
	if built := runIn(t, dir, lumen, "native", "checker.lum"); built.exit != 0 {
		t.Fatalf("native of std.compiler: exit %d\n%s", built.exit, built.stderr)
	}
	if res := runIn(t, dir, filepath.Join(dir, "checker")); res.exit != 0 || res.stdout != "true true\n" {
		t.Errorf("checker: exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}

	// --strip: the failure names the instruction, not the line
	if built := runIn(t, dir, lumen, "native", "broken.lum", "-o", "stripped", "--strip"); built.exit != 0 {
		t.Fatalf("native --strip: exit %d\n%s", built.exit, built.stderr)
	}
	if res := runIn(t, dir, filepath.Join(dir, "stripped")); res.exit != 1 || !strings.Contains(res.stderr, "at instruction") || strings.Contains(res.stderr, "broken.lum") {
		t.Errorf("stripped: exit %d\n%s", res.exit, res.stderr)
	}

	// A program that does not compile makes nothing
	if res := runIn(t, dir, lumen, "native", "wrong.lum"); res.exit != 1 || !strings.Contains(res.stderr, "E") {
		t.Errorf("expected the compile error, got exit %d\n%s%s", res.exit, res.stdout, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "wrong")); err == nil {
		t.Error("an executable was made of a program that does not compile")
	}
}

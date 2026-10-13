// Package e2e tests the lumen binary the way a user runs it: it builds the
// CLI once, then feeds it real .lum files and checks stdout, stderr and the
// exit code.
//
// Each program in cases/ may have, next to it:
//
//	name.out   exact expected stdout (missing = no output expected)
//	name.err   lines that must each appear in stderr (missing = stderr empty)
//	name.exit  expected exit code (missing = 0)
//
// Every program in examples/ is run as well and compared with
// examples/expected/name.out, so the examples cannot rot.
//
// Regenerate the .out files after an intended change with:
//
//	go test ./test/e2e -update
package e2e

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the expected .out files")

// lumen is the path of the binary built for this test run.
var lumen string

const repoRoot = "../.."

// runTimeout bounds one run of the binary.
const runTimeout = 20 * time.Second

func TestMain(m *testing.M) {
	flag.Parse()

	dir, err := os.MkdirTemp("", "lumen-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	lumen = filepath.Join(dir, "lumen")
	build := exec.Command("go", "build", "-o", lumen, "./cmd/lumen")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot build lumen: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	stdout string
	stderr string
	exit   int
}

// runLumen runs the binary from the repository root, so the paths printed
// in diagnostics do not depend on where the tests are started from.
func runLumen(t *testing.T, args ...string) result {
	t.Helper()
	return runLumenWithInput(t, "", args...)
}

// runLumenWithInput is runLumen with text supplied on standard input.
func runLumenWithInput(t *testing.T, input string, args ...string) result {
	t.Helper()
	return runLumenIn(t, repoRoot, input, args...)
}

// runLumenIn runs the binary with dir as the current directory.
func runLumenIn(t *testing.T, dir, input string, args ...string) result {
	t.Helper()

	// No test program needs more than a moment. A run that takes this long
	// has stopped making progress, or has become slow enough to count as
	// broken: fail instead of waiting for it.
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, lumen, args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	res := result{}
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("lumen %v did not finish within %s", args, runTimeout)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("cannot run lumen %v: %v", args, err)
		}
		res.exit = exitErr.ExitCode()
	}
	res.stdout, res.stderr = stdout.String(), stderr.String()
	return res
}

// sources returns the .lum files of a directory given relative to the root.
func sources(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, dir, "*.lum"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no .lum files found in %s", dir)
	}
	return files
}

func readOptional(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}

// checkStdout compares stdout with the golden file, or rewrites it.
func checkStdout(t *testing.T, got, goldenPath string) {
	t.Helper()

	if *update {
		if got == "" {
			os.Remove(goldenPath)
			return
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, _ := readOptional(t, goldenPath)
	if got != want {
		t.Errorf("unexpected stdout\n--- want ---\n%s--- got ---\n%s", want, got)
	}
}

func TestCases(t *testing.T) {
	for _, path := range sources(t, "test/e2e/cases") {
		name := strings.TrimSuffix(filepath.Base(path), ".lum")
		base := strings.TrimSuffix(path, ".lum")

		t.Run(name, func(t *testing.T) {
			res := runLumen(t, "run", filepath.Join("test/e2e/cases", name+".lum"))

			wantExit := 0
			if text, ok := readOptional(t, base+".exit"); ok {
				code, err := strconv.Atoi(strings.TrimSpace(text))
				if err != nil {
					t.Fatalf("invalid %s.exit: %v", name, err)
				}
				wantExit = code
			}
			if res.exit != wantExit {
				t.Errorf("expected exit code %d, got %d\nstderr:\n%s", wantExit, res.exit, res.stderr)
			}

			checkStdout(t, res.stdout, base+".out")

			wantErr, hasErr := readOptional(t, base+".err")
			if !hasErr && res.stderr != "" {
				t.Errorf("expected nothing on stderr, got:\n%s", res.stderr)
			}
			for _, line := range strings.Split(wantErr, "\n") {
				if line = strings.TrimSpace(line); line != "" && !strings.Contains(res.stderr, line) {
					t.Errorf("expected stderr to contain %q, got:\n%s", line, res.stderr)
				}
			}
		})
	}
}

func TestExamples(t *testing.T) {
	for _, path := range sources(t, "examples") {
		name := strings.TrimSuffix(filepath.Base(path), ".lum")

		t.Run(name, func(t *testing.T) {
			res := runLumen(t, "run", filepath.Join("examples", name+".lum"))
			if res.exit != 0 || res.stderr != "" {
				t.Fatalf("expected a clean run, got exit code %d\nstderr:\n%s", res.exit, res.stderr)
			}
			checkStdout(t, res.stdout, filepath.Join(repoRoot, "examples", "expected", name+".out"))
		})
	}
}

// opensAWindow lists the examples that are not run by the tests: they open
// a window and wait for somebody to play.
var opensAWindow = map[string]bool{"raylib": true}

// TestExampleProjects runs every example that is a folder with a project
// file, and compares its output with examples/expected/<folder>.out.
func TestExampleProjects(t *testing.T) {
	configs, err := filepath.Glob(filepath.Join(repoRoot, "examples", "*", "lumen.json"))
	if err != nil || len(configs) == 0 {
		t.Fatal("no example projects found")
	}
	for _, config := range configs {
		name := filepath.Base(filepath.Dir(config))
		if opensAWindow[name] {
			// Compiled by TestWindowedExamplesCompile, and not run
			continue
		}

		t.Run(name, func(t *testing.T) {
			res := runLumen(t, "run", filepath.Join("examples", name))
			if res.exit != 0 || res.stderr != "" {
				t.Fatalf("expected a clean run, got exit code %d\nstderr:\n%s", res.exit, res.stderr)
			}
			checkStdout(t, res.stdout, filepath.Join(repoRoot, "examples", "expected", name+".out"))
		})
	}
}

// TestBuildThenExec checks that compiled bytecode behaves like `lumen run`.
func TestBuildThenExec(t *testing.T) {
	bytecode := filepath.Join(t.TempDir(), "features.lc")
	source := "test/e2e/cases/features.lum"

	build := runLumen(t, "build", source, bytecode)
	if build.exit != 0 {
		t.Fatalf("build failed with exit code %d:\n%s", build.exit, build.stderr)
	}
	if info, err := os.Stat(bytecode); err != nil || info.Size() == 0 {
		t.Fatalf("expected a bytecode file at %s", bytecode)
	}

	direct := runLumen(t, "run", source)
	executed := runLumen(t, "exec", bytecode)
	if executed.exit != 0 {
		t.Fatalf("exec failed with exit code %d:\n%s", executed.exit, executed.stderr)
	}

	// exec appends a line with main's result to the program's own output
	if !strings.HasPrefix(executed.stdout, direct.stdout) {
		t.Errorf("expected exec to print what run prints\n--- run ---\n%s--- exec ---\n%s", direct.stdout, executed.stdout)
	}
	if !strings.HasSuffix(executed.stdout, "Result: Int(9)\n") {
		t.Errorf("expected exec to report main's result, got:\n%s", executed.stdout)
	}
}

func TestBuildListing(t *testing.T) {
	listing := filepath.Join(t.TempDir(), "hello.lasm")

	if res := runLumen(t, "build", "examples/hello.lum", listing); res.exit != 0 {
		t.Fatalf("build failed with exit code %d:\n%s", res.exit, res.stderr)
	}
	text, err := os.ReadFile(listing)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".func main", "CALLX", "println", `"Hello, Lumen!"`, "RET"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("expected the listing to contain %q, got:\n%s", want, text)
		}
	}
}

func TestBuildFailureWritesNothing(t *testing.T) {
	bytecode := filepath.Join(t.TempDir(), "broken.lc")

	res := runLumen(t, "build", "test/e2e/cases/type_error.lum", bytecode)
	if res.exit != 1 || !strings.Contains(res.stderr, "E2001") {
		t.Errorf("expected exit code 1 with a type error, got %d:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(bytecode); !os.IsNotExist(err) {
		t.Error("expected no bytecode file for a program that does not compile")
	}
}

func TestIRCommand(t *testing.T) {
	res := runLumen(t, "ir", "examples/hello.lum")
	if res.exit != 0 {
		t.Fatalf("ir failed with exit code %d:\n%s", res.exit, res.stderr)
	}
	for _, want := range []string{"fn main() {", "b0:", `MOV, "println"`, "CALL", "RET"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("expected the IR to contain %q, got:\n%s", want, res.stdout)
		}
	}
}

// TestMachineLimits covers the options that bound what a program may use.
func TestMachineLimits(t *testing.T) {
	deep := "test/e2e/cases/limits_recursion.lum"
	big := "test/e2e/cases/limits_memory.lum"

	// Within the defaults both programs run
	if res := runLumen(t, "run", deep); res.exit != 0 || res.stdout != "12497500 5000\n" {
		t.Errorf("expected the recursion to finish, got exit %d, %q, %s", res.exit, res.stdout, res.stderr)
	}
	if res := runLumen(t, "run", big); res.exit != 0 || res.stdout != "1500000 1124999250000\n" {
		t.Errorf("expected the large array to be built, got exit %d, %q, %s", res.exit, res.stdout, res.stderr)
	}

	// --max-depth lowers how deep calls may nest
	res := runLumen(t, "run", "--max-depth=1000", deep)
	if res.exit != 1 || !strings.Contains(res.stderr, "stack overflow: max call depth 1000 exceeded") {
		t.Errorf("expected a stack overflow at depth 1000, got exit %d:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "limits_recursion.lum") || !strings.Contains(res.stderr, "in function 'sum'") {
		t.Errorf("expected the overflow to name where it happened, got:\n%s", res.stderr)
	}

	// --heap-mb lowers how much memory it may hold; the option may follow the file
	res = runLumen(t, "run", big, "--heap-mb=2")
	if res.exit != 1 || !strings.Contains(res.stderr, "out of memory") || !strings.Contains(res.stderr, "limit 2097152") {
		t.Errorf("expected the program to run out of 2MB, got exit %d:\n%s", res.exit, res.stderr)
	}
	// ...and raising it is fine
	if res := runLumen(t, "run", "--heap-mb=512", "--max-depth=20000", big); res.exit != 0 {
		t.Errorf("expected generous limits to work, got exit %d:\n%s", res.exit, res.stderr)
	}

	// The same options apply to compiled bytecode
	bytecode := filepath.Join(t.TempDir(), "deep.lc")
	if res := runLumen(t, "build", deep, bytecode); res.exit != 0 {
		t.Fatalf("build failed:\n%s", res.stderr)
	}
	res = runLumen(t, "exec", bytecode, "--max-depth=1000")
	if res.exit != 1 || !strings.Contains(res.stderr, "max call depth 1000 exceeded") {
		t.Errorf("expected exec to honour --max-depth, got exit %d:\n%s", res.exit, res.stderr)
	}

	for _, bad := range [][]string{
		{"run", "--heap-mb=lots", big},
		{"run", "--heap-mb=0", big},
		{"run", "--max-depth", deep},
		{"run", "--speed=9", big},
	} {
		res := runLumen(t, bad...)
		if res.exit != 1 || !strings.Contains(res.stderr, "Error:") || res.stdout != "" {
			t.Errorf("%v: expected an option error, got exit %d, %q, %q", bad, res.exit, res.stdout, res.stderr)
		}
	}
}

func TestMissingFile(t *testing.T) {
	res := runLumen(t, "run", "does/not/exist.lum")
	if res.exit != 1 || !strings.Contains(res.stderr, "cannot read does/not/exist.lum") {
		t.Errorf("expected exit code 1 and a read error, got %d:\n%s", res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("expected nothing on stdout, got:\n%s", res.stdout)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"run"}, {"build", "only-input.lum"}, {"ir"}, {"no-such-command"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := runLumen(t, args...)
			if res.exit != 1 {
				t.Errorf("expected exit code 1, got %d", res.exit)
			}
			if !strings.Contains(res.stdout, "Usage") {
				t.Errorf("expected usage help, got:\n%s", res.stdout)
			}
		})
	}
}

// TestLargeProgram runs a program well past what the bytecode format once
// allowed: 256 functions and 256 constants per program, 256 registers per
// function. It is generated, since it is two thousand lines of repetition.
func TestLargeProgram(t *testing.T) {
	const count = 500

	var sb strings.Builder
	sb.WriteString("import std.arrays;\n\ntrait Step {\n    fn apply(n: Int) -> Int;\n    fn label() -> String;\n}\n\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&sb, "struct Step%d { amount%d: Int; }\n", i, i)
		fmt.Fprintf(&sb, "impl Step for Step%d {\n", i)
		fmt.Fprintf(&sb, "    fn apply(n: Int) -> Int { return n + self.amount%d + %d; }\n", i, 70000+i)
		fmt.Fprintf(&sb, "    fn label() -> String { return \"step %d\"; }\n}\n", i)
		fmt.Fprintf(&sb, "fn step%d() -> Step { return Step%d { amount%d: %d }; }\n\n", i, i, i, i)
	}
	sb.WriteString("fn main() -> Int {\n    let steps: [Step] = [];\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&sb, "    let s%d = step%d();\n    push(steps, s%d);\n", i, i, i)
	}
	fmt.Fprintf(&sb, `    let value = 0;
    let last = "";
    for step in steps {
        value = step.apply(value);
        last = step.label();
    }
    println(arrays.len(steps), value, last, s%d.label());
    return 7;
}
`, count-1)

	value := 0
	for i := 0; i < count; i++ {
		value += i + 70000 + i
	}
	want := fmt.Sprintf("%d %d step %d step %d\n", count, value, count-1, count-1)

	dir := t.TempDir()
	source := filepath.Join(dir, "large.lum")
	if err := os.WriteFile(source, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}

	res := runLumen(t, "run", source)
	if res.exit != 7 || res.stdout != want || res.stderr != "" {
		t.Fatalf("run: expected exit 7 and %q, got exit %d, %q\n%s", want, res.exit, res.stdout, res.stderr)
	}

	// The same through a bytecode file
	bytecode := filepath.Join(dir, "large.lc")
	if res := runLumen(t, "build", source, bytecode); res.exit != 0 {
		t.Fatalf("build failed:\n%s", res.stderr)
	}
	res = runLumen(t, "exec", bytecode)
	if !strings.HasPrefix(res.stdout, want) || res.stderr != "" {
		t.Errorf("exec: expected output starting with %q, got %q\n%s", want, res.stdout, res.stderr)
	}

	// The listing shows the operands that did not fit before
	listing := filepath.Join(dir, "large.lasm")
	if res := runLumen(t, "build", source, listing); res.exit != 0 {
		t.Fatalf("build of the listing failed:\n%s", res.stderr)
	}
	text, err := os.ReadFile(listing)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), fmt.Sprintf(".func step%d ", count-1)) {
		t.Errorf("expected the listing to define step%d", count-1)
	}
	if !regexp.MustCompile(fmt.Sprintf(`CALLK r\d+ \d+ step%d\n`, count-1)).Match(text) {
		t.Errorf("expected the listing to refer to step%d by name", count-1)
	}
	// Constant indexes past 255 are printed in full
	if !regexp.MustCompile(`LOADK r\d+ \d{4}\b`).Match(text) {
		t.Error("expected the listing to load a constant with a four-digit index")
	}
}

// TestDebuggerSession drives lumdbg through its commands on a compiled
// program that uses closures, loops and calls.
func TestDebuggerSession(t *testing.T) {
	program := "test/e2e/cases/closures_in_loops.lum"

	// Step a few instructions, look at registers and state, then run on
	res := runLumenWithInput(t, "s\ns\ns\nr\ni\nc\n", "lumdbg", program)
	if res.exit != 0 {
		t.Fatalf("debugger exited with %d:\n%s\n%s", res.exit, res.stdout, res.stderr)
	}
	for _, want := range []string{
		"NEWSTR r7 6  [main]",      // The instruction, and the function it is in
		"closures_in_loops.lum:26", // Source position of the instruction
		`r7 = String("build")`,     // Registers after three steps
		"Function: main",
		"Execution complete: Int(0)",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("expected the session to contain %q, got:\n%s", want, res.stdout)
		}
	}

	// The program prints the same under the debugger as without it
	plain := runLumen(t, "run", program)
	for _, line := range strings.Split(strings.TrimSpace(plain.stdout), "\n") {
		if !strings.Contains(res.stdout, line) {
			t.Errorf("expected the debugged program to print %q", line)
		}
	}

	// Limits apply under the debugger too, and errors name their place
	res = runLumenWithInput(t, "c\n", "lumdbg", "--max-depth=50", "test/e2e/cases/limits_recursion.lum")
	if res.exit != 1 || !strings.Contains(res.stderr, "stack overflow: max call depth 50 exceeded") {
		t.Errorf("expected a stack overflow at depth 50, got exit %d:\n%s", res.exit, res.stderr)
	}
}

// TestModulesThroughCommands checks that every command that takes a source
// file loads the modules it imports.
func TestModulesThroughCommands(t *testing.T) {
	program := "examples/modules.lum"
	want, err := os.ReadFile(filepath.Join(repoRoot, "examples/expected/modules.out"))
	if err != nil {
		t.Fatal(err)
	}

	// A bytecode file holds the whole program: no source is needed to run it
	bytecode := filepath.Join(t.TempDir(), "modules.lc")
	if res := runLumen(t, "build", program, bytecode); res.exit != 0 {
		t.Fatalf("build failed:\n%s", res.stderr)
	}
	if res := runLumen(t, "exec", bytecode); !strings.HasPrefix(res.stdout, string(want)) {
		t.Errorf("exec: expected output starting with %q, got %q\n%s", want, res.stdout, res.stderr)
	}

	// Functions of a module are named with its path, in the IR and in the listing
	ir := runLumen(t, "ir", program)
	for _, name := range []string{"fn main()", "fn zoo.animals.new(", "fn zoo.keeper.Keeper.feed(", "fn zoo.feed.portion("} {
		if !strings.Contains(ir.stdout, name) {
			t.Errorf("expected the IR to contain %q, got:\n%s", name, ir.stdout)
		}
	}
	listing := filepath.Join(t.TempDir(), "modules.lasm")
	if res := runLumen(t, "build", program, listing); res.exit != 0 {
		t.Fatalf("build of the listing failed:\n%s", res.stderr)
	}
	text, _ := os.ReadFile(listing)
	for _, name := range []string{".func zoo.animals.heaviest ", "CALLK r9 2 zoo.animals.new"} {
		if !strings.Contains(string(text), name) {
			t.Errorf("expected the listing to contain %q", name)
		}
	}

	// The debugger steps into a module and shows the module's source
	session := runLumenWithInput(t, "s\ns\ns\ns\ns\ns\ns\ns\nc\n", "lumdbg", program)
	if session.exit != 0 || !strings.Contains(session.stdout, "[zoo.animals.new]") || !strings.Contains(session.stdout, "examples/zoo/animals.lum:") {
		t.Errorf("expected the session to step into zoo.animals.new, got exit %d:\n%s", session.exit, session.stdout)
	}
}

// TestModulesAreFoundNextToTheEntryFile checks that modules are looked up
// from the folder of the entry file, whatever the current directory and
// however the file is named on the command line.
func TestModulesAreFoundNextToTheEntryFile(t *testing.T) {
	dir := t.TempDir()
	write := func(path, src string) {
		t.Helper()
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("app/start.lum", "import lib.text;\nimport peer;\nfn main() -> Int { println(text.shout(peer.word())); return 0; }\n")
	write("app/lib/text.lum", "pub fn shout(s: String) -> String { return s + \"!\"; }\n")
	write("app/peer.lum", "import lib.text;\npub fn word() -> String { return text.shout(\"hey\"); }\n")
	// A file outside the program's folder is not reachable
	write("text.lum", "pub fn shout(s: String) -> String { return \"wrong file\"; }\n")

	// An absolute path; the tests run from the repository root
	res := runLumen(t, "run", filepath.Join(dir, "app", "start.lum"))
	if res.exit != 0 || res.stdout != "hey!!\n" {
		t.Errorf("expected hey!!, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}

	// A module is not found through a parent folder
	write("app/start.lum", "import text;\nfn main() -> Int { return 0; }\n")
	res = runLumen(t, "run", filepath.Join(dir, "app", "start.lum"))
	if res.exit != 1 || !strings.Contains(res.stderr, "E8001") || !strings.Contains(res.stderr, filepath.Join(dir, "app", "text.lum")) {
		t.Errorf("expected the module to be looked for in app/, got exit %d:\n%s", res.exit, res.stderr)
	}

	// An error in a module shows the module's file and line
	write("app/start.lum", "import lib.text;\nfn main() -> Int { println(text.shout(1)); return 0; }\n")
	write("app/lib/text.lum", "pub fn shout(s: String) -> String {\n    let n: Int = s;\n    return s;\n}\n")
	res = runLumen(t, "run", filepath.Join(dir, "app", "start.lum"))
	for _, want := range []string{"E2001", filepath.Join(dir, "app", "lib", "text.lum") + ":2:", "let n: Int = s;", "E2010", "start.lum:2:"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("expected stderr to contain %q, got:\n%s", want, res.stderr)
		}
	}
}

// project writes the files of a program into a new folder and returns it.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestExampleProject runs the example that has a project file, the ways a
// project can be run.
func TestExampleProject(t *testing.T) {
	want := "  1 * ada *\n  2 * grace *\n  3 * linus *\n------------\n    3 guests\n"
	example := filepath.Join(repoRoot, "examples", "project")

	runs := []struct {
		name string
		dir  string
		args []string
	}{
		{"the project folder", repoRoot, []string{"run", "examples/project"}},
		{"its entry file", repoRoot, []string{"run", "examples/project/src/main.lum"}},
		{"no argument, in the folder", example, []string{"run"}},
		{"no argument, deep inside it", filepath.Join(example, "src", "app"), []string{"run"}},
		{"the entry file, from inside", filepath.Join(example, "src"), []string{"run", "main.lum"}},
		{"a dot", example, []string{"run", "."}},
	}
	for _, r := range runs {
		t.Run(r.name, func(t *testing.T) {
			res := runLumenIn(t, r.dir, "", r.args...)
			if res.exit != 0 || res.stdout != want || res.stderr != "" {
				t.Errorf("expected the guest list, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
			}
		})
	}

	// The other commands take a project the same way
	if res := runLumenIn(t, example, "", "ir"); !strings.Contains(res.stdout, "fn textkit.format.pad.left(") || !strings.Contains(res.stdout, "fn app.guests.invited(") {
		t.Errorf("expected the IR of the project, got:\n%s%s", res.stdout, res.stderr)
	}
	bytecode := filepath.Join(t.TempDir(), "greeter.lc")
	if res := runLumenIn(t, example, "", "build", bytecode); res.exit != 0 {
		t.Fatalf("build failed:\n%s", res.stderr)
	}
	if res := runLumen(t, "exec", bytecode); !strings.HasPrefix(res.stdout, want) {
		t.Errorf("expected the built project to run, got %q\n%s", res.stdout, res.stderr)
	}
	if res := runLumen(t, "build", "examples/project", bytecode); res.exit != 0 {
		t.Errorf("expected build to take a project folder:\n%s", res.stderr)
	}
	if res := runLumenWithInput(t, "c\n", "lumdbg", "examples/project"); res.exit != 0 || !strings.Contains(res.stdout, "3 guests") {
		t.Errorf("expected the debugger to run the project, got exit %d:\n%s%s", res.exit, res.stdout, res.stderr)
	}

	// The project sets max_depth to 2000; the command line wins over it
	res := runLumen(t, "run", "--max-depth=5", "examples/project")
	if res.exit != 1 || !strings.Contains(res.stderr, "max call depth 5 exceeded") || !strings.Contains(res.stderr, "examples/project/packages/textkit/format/pad.lum") {
		t.Errorf("expected a stack overflow in the package, got exit %d:\n%s", res.exit, res.stderr)
	}
}

func TestProjectFile(t *testing.T) {
	deep := "fn down(n: Int) -> Int { if n == 0 { return 0; } return 1 + down(n - 1); }\n"

	t.Run("limits", func(t *testing.T) {
		dir := project(t, map[string]string{
			"lumen.json": `{"entry": "main.lum", "max_depth": 50, "heap_mb": 1}`,
			"main.lum":   deep + "fn main() -> Int { println(down(30)); println(down(100)); return 0; }\n",
			"big.lum":    "fn main() -> Int { let xs: [Int] = []; let i = 0; while i < 2000000 { push(xs, i); i++; } return 0; }\n",
		})
		res := runLumenIn(t, dir, "", "run")
		if res.exit != 1 || res.stdout != "30\n" || !strings.Contains(res.stderr, "max call depth 50 exceeded") {
			t.Errorf("expected the project's depth limit, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
		}
		// Limits apply to any file of the project, not only the entry
		res = runLumenIn(t, dir, "", "run", "big.lum")
		if res.exit != 1 || !strings.Contains(res.stderr, "out of memory") || !strings.Contains(res.stderr, "limit 1048576") {
			t.Errorf("expected the project's memory limit, got exit %d:\n%s", res.exit, res.stderr)
		}
		// Options on the command line come first
		res = runLumenIn(t, dir, "", "run", "--max-depth=500", "--heap-mb=128", "big.lum")
		if res.exit != 0 {
			t.Errorf("expected the command line to lift the limits, got exit %d:\n%s", res.exit, res.stderr)
		}
		if res := runLumenIn(t, dir, "", "run", "--max-depth=500"); res.exit != 0 || res.stdout != "30\n100\n" {
			t.Errorf("expected the command line to lift the depth limit, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("root", func(t *testing.T) {
		files := map[string]string{
			"lumen.json":             `{"entry": "code/bin/start.lum", "root": "code"}`,
			"code/bin/start.lum":     "import lib.greet;\nfn main() -> Int { println(greet.hello()); return 0; }\n",
			"code/lib/greet.lum":     "pub fn hello() -> String { return \"from the root\"; }\n",
			"code/bin/lib/greet.lum": "pub fn hello() -> String { return \"from next to the entry\"; }\n",
		}
		dir := project(t, files)
		if res := runLumenIn(t, dir, "", "run"); res.exit != 0 || res.stdout != "from the root\n" {
			t.Errorf("expected module paths to start at the root, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
		}

		// Without "root" they start at the folder of the project file
		files["lumen.json"] = `{"entry": "code/bin/start.lum"}`
		files["code/bin/start.lum"] = "import code.lib.greet;\nfn main() -> Int { println(greet.hello()); return 0; }\n"
		dir = project(t, files)
		if res := runLumenIn(t, dir, "", "run"); res.exit != 0 || res.stdout != "from the root\n" {
			t.Errorf("expected module paths to start at the project file, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
		}

		// Without a project file they start next to the file that is run
		delete(files, "lumen.json")
		files["code/bin/start.lum"] = "import lib.greet;\nfn main() -> Int { println(greet.hello()); return 0; }\n"
		dir = project(t, files)
		if res := runLumenIn(t, dir, "", "run", "code/bin/start.lum"); res.exit != 0 || res.stdout != "from next to the entry\n" {
			t.Errorf("expected module paths to start at the entry file, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("packages", func(t *testing.T) {
		dir := project(t, map[string]string{
			"app/lumen.json":      `{"entry": "main.lum", "packages": {"kit": "../shared/kit"}}`,
			"app/main.lum":        "import kit;\nimport kit.math.{double};\nfn main() -> Int { println(kit.name(), double(21)); return 0; }\n",
			"shared/kit.lum":      "import kit.math;\npub fn name() -> String { return \"kit\" + toString(math.double(1)); }\n",
			"shared/kit/math.lum": "pub fn double(n: Int) -> Int { return n * 2; }\n",
		})
		app := filepath.Join(dir, "app")
		if res := runLumenIn(t, app, "", "run"); res.exit != 0 || res.stdout != "kit2 42\n" {
			t.Errorf("expected the package to be found, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
		}

		// A module the package does not have: the message names its folder
		if err := os.WriteFile(filepath.Join(app, "main.lum"), []byte("import kit.text;\nfn main() -> Int { return 0; }\n"), 0644); err != nil {
			t.Fatal(err)
		}
		res := runLumenIn(t, app, "", "run")
		if res.exit != 1 || !strings.Contains(res.stderr, "E8001") || !strings.Contains(res.stderr, "'kit' is a package in the folder") {
			t.Errorf("expected a missing module of the package, got exit %d:\n%s", res.exit, res.stderr)
		}
	})

	t.Run("mistakes", func(t *testing.T) {
		tests := []struct {
			name   string
			config string
			args   []string
			want   string
		}{
			{"unknown key", `{"main": "main.lum"}`, []string{"run"}, `lumen.json: unknown key "main"; the keys are name, entry, root, packages, heap_mb, max_depth`},
			{"not JSON", "{\n  entry: \"main.lum\"\n}", []string{"run"}, "lumen.json: line 2, column 3: not valid JSON"},
			{"no entry", `{"name": "x"}`, []string{"run"}, `does not say where the program starts; add "entry": "main.lum"`},
			{"no entry, for build", `{}`, []string{"build", "out.lc"}, `does not say where the program starts`},
			{"entry that does not exist", `{"entry": "start.lum"}`, []string{"run"}, "cannot read start.lum"},
			// A broken project file is reported even when a file is named
			{"broken, with a file", `{"heap_mb": "lots"}`, []string{"run", "main.lum"}, `"heap_mb" must be a whole number`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dir := project(t, map[string]string{
					"lumen.json": tt.config,
					"main.lum":   "fn main() -> Int { return 0; }\n",
				})
				res := runLumenIn(t, dir, "", tt.args...)
				if res.exit != 1 || !strings.Contains(res.stderr, tt.want) {
					t.Errorf("expected exit 1 and %q, got exit %d:\n%s%s", tt.want, res.exit, res.stdout, res.stderr)
				}
				if res.stdout != "" {
					t.Errorf("expected nothing on stdout, got %q", res.stdout)
				}
			})
		}
	})

	t.Run("no project", func(t *testing.T) {
		dir := project(t, map[string]string{"sub/main.lum": "fn main() -> Int { println(\"plain\"); return 0; }\n"})

		// Nothing to run: the usage, as before
		if res := runLumenIn(t, dir, "", "run"); res.exit != 1 || !strings.Contains(res.stdout, "Usage: lumen run") {
			t.Errorf("expected the usage, got exit %d: %q %q", res.exit, res.stdout, res.stderr)
		}
		// A folder that is not a project
		if res := runLumenIn(t, dir, "", "run", "sub"); res.exit != 1 || !strings.Contains(res.stderr, "sub is a folder without a lumen.json") {
			t.Errorf("expected a message about the folder, got exit %d: %q", res.exit, res.stderr)
		}
		// A file runs without one
		if res := runLumenIn(t, dir, "", "run", "sub/main.lum"); res.exit != 0 || res.stdout != "plain\n" {
			t.Errorf("expected a single file to run, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
		}
	})
}

// TestStandardLibrary runs programs that use the library built into the
// binary: nothing but the binary and the program's own file is needed.
func TestStandardLibrary(t *testing.T) {
	// A program in a folder with nothing else in it, run from elsewhere
	dir := project(t, map[string]string{
		"greet.lum": `
import std.io;
import std.strings;
import std.arrays;

fn main() -> Int {
    let name = io.ask("Who are you? ");
    if name == null {
        println("nobody there");
        return 1;
    }
    println("Hello, " + strings.capitalize(strings.trim(name)) + "!");

    let numbers: [Int] = [];
    for line in io.readLines() {
        let n = strings.parseInt(line);
        if n != null { push(numbers, n); }
    }
    println(arrays.len(numbers), "numbers, sum", arrays.sum(numbers), "largest", arrays.largest(numbers) ?? 0);
    return 0;
}
`,
	})
	program := filepath.Join(dir, "greet.lum")

	// Input comes from standard input
	res := runLumenWithInput(t, "  ada \n4\nx\n10\n\n-3\n", "run", program)
	if res.exit != 0 || res.stdout != "Who are you? Hello, Ada!\n3 numbers, sum 11 largest 10\n" || res.stderr != "" {
		t.Errorf("unexpected run: exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}
	// No input at all: readLine gives null at once
	res = runLumenIn(t, dir, "", "run", "greet.lum")
	if res.exit != 1 || res.stdout != "Who are you? nobody there\n" {
		t.Errorf("expected the program to see the end of the input, got exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}

	// Compiled bytecode carries the library's functions with it
	bytecode := filepath.Join(dir, "greet.lc")
	if res := runLumen(t, "build", program, bytecode); res.exit != 0 {
		t.Fatalf("build failed:\n%s", res.stderr)
	}
	res = runLumenWithInput(t, "bob\n1\n2\n", "exec", bytecode)
	if !strings.HasPrefix(res.stdout, "Who are you? Hello, Bob!\n2 numbers, sum 3 largest 2\n") {
		t.Errorf("expected the bytecode to run with the library, got %q\n%s", res.stdout, res.stderr)
	}

	// The library's functions appear under their module's name
	ir := runLumen(t, "ir", program)
	for _, name := range []string{"fn std.strings.capitalize(", "fn std.arrays.sum(", "fn std.io.ask("} {
		if !strings.Contains(ir.stdout, name) {
			t.Errorf("expected the IR to contain %q", name)
		}
	}
	// ...and the natives are host functions, not compiled code
	if strings.Contains(ir.stdout, "fn std.strings.trim(") || strings.Contains(ir.stdout, "fn std.io.readLine(") {
		t.Error("did not expect native functions among the compiled ones")
	}
	listing := filepath.Join(dir, "greet.lasm")
	if res := runLumen(t, "build", program, listing); res.exit != 0 {
		t.Fatalf("build of the listing failed:\n%s", res.stderr)
	}
	text, _ := os.ReadFile(listing)
	if !regexp.MustCompile(`CALLX r\d+ \d+ std\.io\.readLine`).Match(text) {
		t.Error("expected the listing to call std.io.readLine as a host function")
	}

	// Files: written by one run, read by the next
	notes := filepath.Join(dir, "notes.txt")
	files := project(t, map[string]string{
		"save.lum": "import std.io;\nfn main() -> Int {\n    if io.appendFile(\"" + notes + "\", \"a line\\n\").isOk() { return 0; }\n    return 1;\n}\n",
		"load.lum": "import std.io;\nimport std.strings;\nimport std.arrays;\nfn main() -> Int {\n    println(arrays.len(io.readFileLines(\"" + notes + "\").orElse([])));\n    return 0;\n}\n",
	})
	for i := 0; i < 3; i++ {
		if res := runLumenIn(t, files, "", "run", "save.lum"); res.exit != 0 {
			t.Fatalf("expected the file to be written, got exit %d:\n%s", res.exit, res.stderr)
		}
	}
	if res := runLumenIn(t, files, "", "run", "load.lum"); res.stdout != "3\n" {
		t.Errorf("expected three lines in the file, got %q\n%s", res.stdout, res.stderr)
	}

	// A project may replace the library with a package of its own
	own := project(t, map[string]string{
		"lumen.json":     `{"entry": "main.lum", "packages": {"std": "mystd"}}`,
		"main.lum":       "import std.math;\nfn main() -> Int { println(math.abs(-5)); return 0; }\n",
		"mystd/math.lum": "pub fn abs(n: Int) -> Int { return 0 - 1; }\n",
	})
	if res := runLumenIn(t, own, "", "run"); res.stdout != "-1\n" {
		t.Errorf("expected the project's own std to be used, got %q\n%s", res.stdout, res.stderr)
	}
}

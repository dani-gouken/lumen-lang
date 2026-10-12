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

	// No test program needs more than a moment. A run that takes this long
	// has stopped making progress, or has become slow enough to count as
	// broken: fail instead of waiting for it.
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, lumen, args...)
	cmd.Dir = repoRoot
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
	for _, want := range []string{".func main", "NEWEXFUNC", "println", `"Hello, Lumen!"`, "CALL", "RET"} {
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
	sb.WriteString("trait Step {\n    fn apply(n: Int) -> Int;\n    fn label() -> String;\n}\n\n")
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
    println(steps.length, value, last, s%d.label());
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
	if !regexp.MustCompile(fmt.Sprintf(`NEWFUNC r\d+ step%d\n`, count-1)).Match(text) {
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
		"NEWFUNC r7 schedule  [main]", // Functions by name, not by register
		"closures_in_loops.lum:17",    // Source position of the instruction
		`r8 = String("build")`,        // Registers after three steps
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

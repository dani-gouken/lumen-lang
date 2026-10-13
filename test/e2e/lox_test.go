package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The Lox interpreter of examples/lox is a real program written in Lumen:
// some 1,400 lines in six modules, a scanner, a parser, a resolver and a
// tree-walking interpreter with closures, classes and inheritance. It is
// run here on the tests of the book it comes from, which say for each
// program what it prints or how it fails. Lumen running it right is what
// they check: every part of the language and of the machine is under it.

// loxExpectation is what a test program says of itself, in its comments.
type loxExpectation struct {
	output       []string // // expect: line
	errors       []string // // Error at ...: said before the program runs
	runtimeError string   // // expect runtime error: message
	runtimeLine  int
}

var (
	loxExpectOutput  = regexp.MustCompile(`// expect: ?(.*)`)
	loxExpectError   = regexp.MustCompile(`// (Error.*)`)
	loxErrorAtLine   = regexp.MustCompile(`// \[((java|c) )?line (\d+)\] (Error.*)`)
	loxExpectRuntime = regexp.MustCompile(`// expect runtime error: (.+)`)
	loxStackLine     = regexp.MustCompile(`\[line (\d+)\]`)
)

// loxExpected reads the expectations of a test. The interpreter here is
// the one the book writes in Java: what a comment says of the one in C
// only is not for it.
func loxExpected(source string) loxExpectation {
	var want loxExpectation
	for i, line := range strings.Split(source, "\n") {
		number := i + 1
		if m := loxErrorAtLine.FindStringSubmatch(line); m != nil {
			if m[2] != "c" {
				want.errors = append(want.errors, fmt.Sprintf("[line %s] %s", m[3], m[4]))
			}
			continue
		}
		if m := loxExpectOutput.FindStringSubmatch(line); m != nil {
			want.output = append(want.output, m[1])
			continue
		}
		if m := loxExpectError.FindStringSubmatch(line); m != nil {
			want.errors = append(want.errors, fmt.Sprintf("[line %d] %s", number, m[1]))
			continue
		}
		if m := loxExpectRuntime.FindStringSubmatch(line); m != nil {
			want.runtimeError, want.runtimeLine = m[1], number
		}
	}
	return want
}

// loxSkipped is the tests that are not for this interpreter, as the book
// lists them for its own in Java, with the reason.
var loxSkipped = map[string]string{
	"scanning":                     "tests of the scanner alone, for an earlier chapter",
	"expressions":                  "tests of expressions alone, for an earlier chapter",
	"benchmark":                    "measured, not checked: see TestLoxBenchmarks",
	"limit/loop_too_large.lox":     "a limit of the interpreter in C",
	"limit/no_reuse_constants.lox": "a limit of the interpreter in C",
	"limit/too_many_constants.lox": "a limit of the interpreter in C",
	"limit/too_many_locals.lox":    "a limit of the interpreter in C",
	"limit/too_many_upvalues.lox":  "a limit of the interpreter in C",
}

func loxTests(t *testing.T) []string {
	t.Helper()
	var files []string
	root, err := filepath.Abs(filepath.Join(repoRoot, "test/e2e/lox"))
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(path, ".lox") {
			files = append(files, path)
		}
		return err
	})
	if err != nil || len(files) < 200 {
		t.Fatalf("expected the tests of Lox, found %d: %v", len(files), err)
	}
	return files
}

// buildLox compiles the interpreter once. Every test then runs the
// compiled file, which is also what a program shipped to someone would be.
func buildLox(t *testing.T) string {
	t.Helper()
	compiled := filepath.Join(t.TempDir(), "lox.lc")
	if res := runLumen(t, "build", "examples/lox", compiled); res.exit != 0 {
		t.Fatalf("the interpreter does not compile:\n%s", res.stderr)
	}
	return compiled
}

func TestLox(t *testing.T) {
	compiled := buildLox(t)
	root, _ := filepath.Abs(filepath.Join(repoRoot, "test/e2e/lox"))

	ran := 0
	for _, file := range loxTests(t) {
		name, _ := filepath.Rel(root, file)
		name = filepath.ToSlash(name)
		if _, skip := loxSkipped[name]; skip {
			continue
		}
		if _, skip := loxSkipped[strings.Split(name, "/")[0]]; skip {
			continue
		}
		ran++

		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want := loxExpected(string(source))
			// The interpreter nests several of its own calls for each call
			// of the program it runs
			res := runLumen(t, "run", "--max-depth=40000", compiled, file)

			wantExit := 0
			switch {
			case len(want.errors) > 0:
				wantExit = 65
			case want.runtimeError != "":
				wantExit = 70
			}
			if res.exit != wantExit {
				t.Errorf("exit %d, expected %d\n%s", res.exit, wantExit, res.stderr)
			}

			// What a program printed before it failed counts too
			got := strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n")
			if res.stdout == "" {
				got = nil
			}
			if strings.Join(got, "\n") != strings.Join(want.output, "\n") {
				t.Errorf("printed\n%s\nexpected\n%s", strings.Join(got, "\n"), strings.Join(want.output, "\n"))
			}

			said := strings.Split(strings.TrimSuffix(res.stderr, "\n"), "\n")
			switch {
			case len(want.errors) > 0:
				if strings.Join(said, "\n") != strings.Join(want.errors, "\n") {
					t.Errorf("said\n%s\nexpected\n%s", res.stderr, strings.Join(want.errors, "\n"))
				}
			case want.runtimeError != "":
				if said[0] != want.runtimeError {
					t.Errorf("failed with %q, expected %q", said[0], want.runtimeError)
				}
				m := loxStackLine.FindStringSubmatch(res.stderr)
				if m == nil {
					t.Errorf("the failure does not say its line:\n%s", res.stderr)
				} else if line, _ := strconv.Atoi(m[1]); line != want.runtimeLine {
					t.Errorf("failed at line %d, expected %d", line, want.runtimeLine)
				}
			default:
				if res.stderr != "" {
					t.Errorf("said, with nothing wrong:\n%s", res.stderr)
				}
			}
		})
	}
	if ran < 230 {
		t.Errorf("only %d tests ran", ran)
	}
}

package vm

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// std.sh runs programs for a script: what they write goes straight to
// where the script's output goes, and one that fails stops the script with
// the command and its status (a library failure, which `attempt` catches).
func init() {
	registerNatives("std.sh",
		native{"run", 1, func(vm *VM, args []Value) (Value, error) {
			_, err := vm.shell(args, nil, true)
			return NewNull(), err
		}},
		native{"output", 1, func(vm *VM, args []Value) (Value, error) {
			var out strings.Builder
			if _, err := vm.shell(args, &out, true); err != nil {
				return NewNull(), err
			}
			return vm.newString(strings.TrimSuffix(strings.TrimSuffix(out.String(), "\n"), "\r"))
		}},
		native{"status", 1, func(vm *VM, args []Value) (Value, error) {
			status, err := vm.shell(args, nil, false)
			return NewInt(int64(status)), err
		}},
		// Runs a program as run does, and instead of stopping the script
		// gives the status (-1 for a program that could not be started),
		// the command, the end of what it wrote as errors and why it
		// failed, "" when it did not
		native{"attempted", 1, func(vm *VM, args []Value) (Value, error) {
			var written tail
			status, err := vm.shellTo(args, nil, &written, true)
			if err != nil {
				if failure, ok := err.(*commandFailure); ok {
					return vm.newStringArray([]string{fmt.Sprint(failure.status), failure.command, written.String(), failure.Error()})
				}
				return NewNull(), err
			}
			return vm.newStringArray([]string{fmt.Sprint(status), "", "", ""})
		}},
	)
}

// commandFailure is a program that could not be started, or ended with a
// status other than 0 where that was not wanted.
type commandFailure struct {
	command string
	status  int // -1 when there is none
	message string
}

func (f *commandFailure) Error() string { return f.message }

// tail keeps the end of what is written to it: the lines that say why a
// program failed come last.
type tail struct{ text []byte }

const tailBytes = 8 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.text = append(t.text, p...)
	if over := len(t.text) - tailBytes; over > 0 {
		t.text = t.text[over:]
	}
	return len(p), nil
}

// String gives the last lines kept, without the line break at the end.
func (t *tail) String() string {
	lines := strings.Split(strings.TrimRight(string(t.text), "\n"), "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return strings.Join(lines, "\n")
}

// shell runs the program and arguments in args and gives its status. What
// it writes goes to out, or with the script's output when out is nil; what
// it writes as errors goes to the standard error. It fails when the program
// cannot be started, and, if strict, when its status is not 0.
func (vm *VM) shell(args []Value, out *strings.Builder, strict bool) (int, error) {
	return vm.shellTo(args, out, nil, strict)
}

// shellTo is shell, keeping in errs, when it is not nil, what the program
// writes as errors as well as showing it. Its failures are commandFailures.
func (vm *VM) shellTo(args []Value, out *strings.Builder, errs io.Writer, strict bool) (int, error) {
	words, err := commandWords(args)
	if err != nil {
		return 0, err
	}
	name, arguments := words[0], words[1:]

	cmd := exec.Command(name, arguments...)
	// The terminal itself where there is one, so that a program that
	// colours its output or asks a question behaves as it does in a shell
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, vm.out, os.Stderr
	if out != nil {
		cmd.Stdout = out
	}
	if errs != nil {
		cmd.Stderr = io.MultiWriter(os.Stderr, errs)
	}

	// Other tasks go on while the program runs
	var failed error
	if gone := vm.blocking(func() { failed = cmd.Run() }); gone != nil {
		return 0, gone
	}

	command := commandLine(name, arguments)
	fail := func(status int, format string, values ...any) (int, error) {
		return status, &commandFailure{command: command, status: status, message: fmt.Sprintf(format, values...)}
	}
	var exit *exec.ExitError
	switch {
	case failed == nil:
		return 0, nil
	case errors.Is(failed, exec.ErrNotFound):
		return fail(-1, "there is no program called '%s'", name)
	case !errors.As(failed, &exit):
		return fail(-1, "could not start %s: %s", command, reasonOf(failed))
	case exit.ExitCode() < 0:
		// Ended by a signal: there is no status to give
		return fail(-1, "%s was stopped: %s", command, exit.String())
	case strict:
		return fail(exit.ExitCode(), "%s ended with status %d", command, exit.ExitCode())
	}
	return exit.ExitCode(), nil
}

// commandWords reads the command a std.ops.Template stands for: the
// parts written in the program are split into words where there is
// space, '...' and "..." keeping one together, and a value stays whole in
// the word it is written in, whatever it holds.
func commandWords(args []Value) ([]string, error) {
	obj, ok := objectOf(args[0])
	if !ok {
		return nil, fmt.Errorf("expected a command, got %s", FormatValue(args[0]))
	}
	partsValue, _ := obj.Get("parts")
	valuesValue, _ := obj.Get("values")
	parts, err := argStrings([]Value{partsValue}, 0)
	if err != nil {
		return nil, err
	}
	values, err := argStrings([]Value{valuesValue}, 0)
	if err != nil {
		return nil, err
	}

	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	for i, part := range parts {
		for _, ch := range part {
			switch {
			case quote != 0 && ch == quote:
				quote = 0
			case quote != 0:
				word.WriteRune(ch)
			case ch == '\'' || ch == '"':
				quote, inWord = ch, true
			case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
				if inWord {
					words = append(words, word.String())
					word.Reset()
					inWord = false
				}
			default:
				word.WriteRune(ch)
				inWord = true
			}
		}
		if i < len(values) {
			word.WriteString(values[i])
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("the command has a %c that is not closed", quote)
	}
	if inWord {
		words = append(words, word.String())
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("the command is empty")
	}
	return words, nil
}

// commandLine writes a command as it would be typed, quoting the
// arguments that would otherwise read differently.
func commandLine(name string, arguments []string) string {
	parts := []string{name}
	for _, arg := range arguments {
		if arg == "" || strings.ContainsAny(arg, " \t\n'\"\\$`;&|<>*?()[]{}#~") {
			arg = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

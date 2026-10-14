package vm

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// std.sh runs programs for a script: what they write goes straight to
// where the script's output goes, and one that fails stops the script with
// the command and its status (a library failure, which `attempt` catches).
func init() {
	registerNatives("std.sh",
		native{"run", 2, func(vm *VM, args []Value) (Value, error) {
			_, err := vm.shell(args, nil, true)
			return NewNull(), err
		}},
		native{"output", 2, func(vm *VM, args []Value) (Value, error) {
			var out strings.Builder
			if _, err := vm.shell(args, &out, true); err != nil {
				return NewNull(), err
			}
			return vm.newString(strings.TrimSuffix(strings.TrimSuffix(out.String(), "\n"), "\r"))
		}},
		native{"status", 2, func(vm *VM, args []Value) (Value, error) {
			status, err := vm.shell(args, nil, false)
			return NewInt(int64(status)), err
		}},
	)
}

// shell runs the program and arguments in args and gives its status. What
// it writes goes to out, or with the script's output when out is nil; what
// it writes as errors goes to the standard error. It fails when the program
// cannot be started, and, if strict, when its status is not 0.
func (vm *VM) shell(args []Value, out *strings.Builder, strict bool) (int, error) {
	name, err := argString(args, 0)
	if err != nil {
		return 0, err
	}
	arguments, err := argStrings(args, 1)
	if err != nil {
		return 0, err
	}

	cmd := exec.Command(name, arguments...)
	// The terminal itself where there is one, so that a program that
	// colours its output or asks a question behaves as it does in a shell
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, vm.out, os.Stderr
	if out != nil {
		cmd.Stdout = out
	}

	// Other tasks go on while the program runs
	var failed error
	if gone := vm.blocking(func() { failed = cmd.Run() }); gone != nil {
		return 0, gone
	}

	command := commandLine(name, arguments)
	var exit *exec.ExitError
	switch {
	case failed == nil:
		return 0, nil
	case errors.Is(failed, exec.ErrNotFound):
		return 0, fmt.Errorf("there is no program called '%s'", name)
	case !errors.As(failed, &exit):
		return 0, fmt.Errorf("could not start %s: %s", command, reasonOf(failed))
	case exit.ExitCode() < 0:
		// Ended by a signal: there is no status to give
		return 0, fmt.Errorf("%s was stopped: %s", command, exit.String())
	case strict:
		return exit.ExitCode(), fmt.Errorf("%s ended with status %d", command, exit.ExitCode())
	}
	return exit.ExitCode(), nil
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

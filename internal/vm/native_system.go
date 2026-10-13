package vm

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"
)

// start is when the process started; std.time.ticks counts from it.
var start = time.Now()

func init() {
	// write wraps writing a text to a file
	write := func(name string, flags int) native {
		return native{name, 2, func(vm *VM, args []Value) (Value, error) {
			path, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			text, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			file, err := os.OpenFile(path, flags, 0644)
			if err != nil {
				return NewBool(false), nil
			}
			_, writeErr := file.WriteString(text)
			closeErr := file.Close()
			return NewBool(writeErr == nil && closeErr == nil), nil
		}}
	}

	registerNatives("std.io",
		// A line of input without its end of line, or null at the end
		native{"readLine", 0, func(vm *VM, args []Value) (Value, error) {
			line, err := vm.input().ReadString('\n')
			if err != nil && (err != io.EOF || line == "") {
				return NewNull(), nil
			}
			return vm.newString(strings.TrimRight(line, "\r\n"))
		}},
		// The content of a file, or null if it cannot be read
		native{"readFile", 1, func(vm *VM, args []Value) (Value, error) {
			path, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return NewNull(), nil
			}
			return vm.newString(string(data))
		}},
		write("writeFile", os.O_WRONLY|os.O_CREATE|os.O_TRUNC),
		write("appendFile", os.O_WRONLY|os.O_CREATE|os.O_APPEND),
		native{"exists", 1, func(vm *VM, args []Value) (Value, error) {
			path, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			_, statErr := os.Stat(path)
			return NewBool(statErr == nil), nil
		}},
		native{"remove", 1, func(vm *VM, args []Value) (Value, error) {
			path, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			return NewBool(os.Remove(path) == nil), nil
		}},
		// Writes to the error output, which print and println do not reach
		native{"printError", 1, func(vm *VM, args []Value) (Value, error) {
			text, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			fmt.Fprint(os.Stderr, text)
			return NewNull(), nil
		}},
		// The value of an environment variable, or null if it is not set
		native{"env", 1, func(vm *VM, args []Value) (Value, error) {
			name, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			value, ok := os.LookupEnv(name)
			if !ok {
				return NewNull(), nil
			}
			return vm.newString(value)
		}},
	)

	registerNatives("std.time",
		// Milliseconds since 1 January 1970
		native{"now", 0, func(vm *VM, args []Value) (Value, error) {
			return NewInt(time.Now().UnixMilli()), nil
		}},
		// Microseconds since the program started, for measuring durations:
		// unlike now it never jumps when the clock is set
		native{"ticks", 0, func(vm *VM, args []Value) (Value, error) {
			return NewInt(time.Since(start).Microseconds()), nil
		}},
		native{"sleep", 1, func(vm *VM, args []Value) (Value, error) {
			ms, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if ms > 0 {
				time.Sleep(time.Duration(ms) * time.Millisecond)
			}
			return NewNull(), nil
		}},
	)

	registerNatives("std.random",
		native{"seed", 1, func(vm *VM, args []Value) (Value, error) {
			n, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			vm.rng = rand.New(rand.NewSource(n))
			return NewNull(), nil
		}},
		// A whole number from 0 up to, not including, the limit
		native{"below", 1, func(vm *VM, args []Value) (Value, error) {
			limit, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if limit <= 0 {
				return NewNull(), fmt.Errorf("the limit must be at least 1, got %d", limit)
			}
			return NewInt(vm.random().Int63n(limit)), nil
		}},
		// A number from 0 up to, not including, 1
		native{"float", 0, func(vm *VM, args []Value) (Value, error) {
			return NewNumber(vm.random().Float64()), nil
		}},
	)
}

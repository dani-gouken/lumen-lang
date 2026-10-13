package vm

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Positions in a string count characters, not bytes, as `text[i]` and
// `text.length` do.

func init() {
	// transform wraps a function from a string to a string
	transform := func(name string, f func(string) string) native {
		return native{name, 1, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			return vm.newString(f(s))
		}}
	}
	// find wraps a search: the position in characters, or null
	find := func(name string, f func(s, part string) int) native {
		return native{name, 2, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			part, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			at := f(s, part)
			if at < 0 {
				return NewNull(), nil
			}
			return NewInt(int64(utf8.RuneCountInString(s[:at]))), nil
		}}
	}

	registerNatives("std.strings",
		native{"substring", 3, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			from, err := argInt(args, 1)
			if err != nil {
				return NewNull(), err
			}
			to, err := argInt(args, 2)
			if err != nil {
				return NewNull(), err
			}
			runes := []rune(s)
			if from < 0 || to > int64(len(runes)) || from > to {
				return NewNull(), fmt.Errorf("range %d..%d is outside a text of %d characters", from, to, len(runes))
			}
			return vm.newString(string(runes[from:to]))
		}},
		find("indexOf", strings.Index),
		find("lastIndexOf", strings.LastIndex),
		transform("upper", strings.ToUpper),
		transform("lower", strings.ToLower),
		transform("trim", strings.TrimSpace),
		native{"split", 2, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			sep, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			// An empty separator splits into characters, as strings.Split does
			return vm.newStringArray(strings.Split(s, sep))
		}},
		native{"join", 2, func(vm *VM, args []Value) (Value, error) {
			parts, err := argStrings(args, 0)
			if err != nil {
				return NewNull(), err
			}
			sep, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			return vm.newString(strings.Join(parts, sep))
		}},
		native{"replace", 3, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			old, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			with, err := argString(args, 2)
			if err != nil {
				return NewNull(), err
			}
			if old == "" {
				return NewNull(), fmt.Errorf("the text to replace is empty")
			}
			return vm.newString(strings.ReplaceAll(s, old, with))
		}},
		native{"repeat", 2, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			n, err := argInt(args, 1)
			if err != nil {
				return NewNull(), err
			}
			if n < 0 {
				return NewNull(), fmt.Errorf("cannot repeat a text %d times", n)
			}
			// Checked before building it: the heap would only refuse the
			// result once Go had already allocated it
			if size := uint64(len(s)) * uint64(n); size > vm.heap.maxBytes {
				return NewNull(), fmt.Errorf("out of memory: a text of %d bytes was requested, limit %d", size, vm.heap.maxBytes)
			}
			return vm.newString(strings.Repeat(s, int(n)))
		}},
		native{"parseInt", 1, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil || n < MinInt || n > MaxInt {
				return NewNull(), nil
			}
			return NewInt(n), nil
		}},
		native{"parseFloat", 1, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return NewNull(), nil
			}
			return NewNumber(f), nil
		}},
		native{"code", 1, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if s == "" {
				return NewNull(), fmt.Errorf("an empty text has no first character")
			}
			r, _ := utf8.DecodeRuneInString(s)
			return NewInt(int64(r)), nil
		}},
		native{"fromCode", 1, func(vm *VM, args []Value) (Value, error) {
			n, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if n < 0 || n > utf8.MaxRune || !utf8.ValidRune(rune(n)) {
				return NewNull(), fmt.Errorf("%d is not the code of a character", n)
			}
			return vm.newString(string(rune(n)))
		}},
		native{"hash", 1, func(vm *VM, args []Value) (Value, error) {
			s, err := argString(args, 0)
			if err != nil {
				return NewNull(), err
			}
			h := fnv.New64a()
			h.Write([]byte(s))
			// Kept non-negative and within an Int
			return NewInt(int64(h.Sum64() & uint64(MaxInt))), nil
		}},
	)
}

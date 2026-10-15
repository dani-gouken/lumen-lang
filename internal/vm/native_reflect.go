package vm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

// Reflection: a program asking what a value is, at the time it runs.
//
// The machine knows less than the compiler did. A struct value carries the
// name of its struct and its fields by name; a value of an enum carries
// "Enum.Variant" and what the variant holds by position; an array knows
// nothing of what its items are meant to be. std.reflect tells what is
// there, no more.

// objectOf is the struct or enum value behind v, if it is one.
func objectOf(v Value) (*ObjectObj, bool) {
	if t, ok := heapType(v); ok && t == ObjTypeObject {
		return (*ObjectObj)(unsafe.Pointer(v.AsHeapPtr())), true
	}
	return nil, false
}

// isVariant reports whether an object is a value of an enum: its name is
// then "Enum.Variant" and what it holds is by position.
func isVariant(obj *ObjectObj) bool { return obj.shape.variant }

// kindName sorts a value into one of the kinds std.reflect knows.
func kindName(v Value) string {
	switch {
	case v.IsNull():
		return "null"
	case v.IsInt():
		return "int"
	case v.IsBool():
		return "bool"
	case v.IsNumber():
		return "float"
	}
	switch t, _ := heapType(v); t {
	case ObjTypeString:
		return "string"
	case ObjTypeArray:
		return "array"
	case ObjTypeFunction, ObjTypeExternFunction:
		return "function"
	case ObjTypeObject:
		obj, _ := objectOf(v)
		switch {
		case strings.HasPrefix(obj.shape.typeName, tuplePrefix):
			return "tuple"
		case isVariant(obj):
			return "variant"
		}
		return "struct"
	}
	return "other"
}

// fieldNames lists the data fields of a struct value, sorted: what it was
// made with, not the methods attached to it.
func fieldNames(obj *ObjectObj) []string {
	var names []string
	for i, key := range obj.shape.keys {
		if !isFunction(obj.slots[i]) {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	return names
}

// article puts "a" or "an" before the name of a type.
func article(name string) string {
	if strings.ContainsAny(name[:1], "AEIOUaeiou") {
		return "an " + name
	}
	return "a " + name
}

func init() {
	structArg := func(args []Value) (*ObjectObj, error) {
		obj, ok := objectOf(args[0])
		if !ok {
			return nil, fmt.Errorf("expected a value of a struct or of an enum, got %s", typeName(args[0]))
		}
		return obj, nil
	}

	registerNatives("std.reflect",
		// Which kind of value this is: "int", "struct", "variant", ...
		native{"kindName", 1, func(vm *VM, args []Value) (Value, error) {
			return vm.newString(kindName(args[0]))
		}},
		// The name of its type, as the program wrote it for a struct or
		// an enum: "Point", "geometry.Shape"
		native{"typeName", 1, func(vm *VM, args []Value) (Value, error) {
			obj, ok := objectOf(args[0])
			if !ok {
				return vm.newString(typeName(args[0]))
			}
			name := obj.shape.typeName
			if isVariant(obj) {
				name = name[:strings.LastIndex(name, ".")]
			}
			return vm.newString(name)
		}},
		// For a value of an enum, the name of its variant; else ""
		native{"variantName", 1, func(vm *VM, args []Value) (Value, error) {
			if obj, ok := objectOf(args[0]); ok && isVariant(obj) {
				return vm.newString(obj.shape.typeName[strings.LastIndex(obj.shape.typeName, ".")+1:])
			}
			return vm.newString("")
		}},
		// The names of the fields of a struct value, sorted; the positions
		// of what a variant or a tuple holds; nothing for anything else
		native{"fieldNames", 1, func(vm *VM, args []Value) (Value, error) {
			obj, ok := objectOf(args[0])
			if !ok {
				return vm.newStringArray(nil)
			}
			names := fieldNames(obj)
			if isVariant(obj) {
				// Positions in order: 0, 1, ..., 10, not as texts sort
				sort.Slice(names, func(i, j int) bool {
					a, _ := strconv.Atoi(names[i])
					b, _ := strconv.Atoi(names[j])
					return a < b
				})
			}
			return vm.newStringArray(names)
		}},
		// Whether two values are the very same one: for an array, a
		// struct or a value of an enum, the same one made once, which
		// == does not ask. Anything else is identical when it is equal.
		native{"identical", 2, func(vm *VM, args []Value) (Value, error) {
			if holdsValues(args[0]) || holdsValues(args[1]) {
				return NewBool(args[0] == args[1]), nil
			}
			return NewBool(valuesEqual(args[0], args[1])), nil
		}},
		native{"hasField", 2, func(vm *VM, args []Value) (Value, error) {
			name, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			obj, ok := objectOf(args[0])
			if !ok {
				return NewBool(false), nil
			}
			value, found := obj.Get(name)
			return NewBool(found && !isFunction(value)), nil
		}},
		// The value of a field. Asking for one that is not there is a
		// mistake: hasField says whether it is
		native{"field", 2, func(vm *VM, args []Value) (Value, error) {
			obj, err := structArg(args)
			if err != nil {
				return NewNull(), err
			}
			name, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			value, found := obj.Get(name)
			if !found || isFunction(value) {
				return NewNull(), fmt.Errorf("%s has no field '%s'", obj.shape.typeName, name)
			}
			return value, nil
		}},
		// Changes a field that is there. The new value has to be of the
		// kind the old one was: the machine does not know the field's
		// type, only what it holds, and a field that holds null may be
		// given anything
		native{"setField", 3, func(vm *VM, args []Value) (Value, error) {
			obj, err := structArg(args)
			if err != nil {
				return NewNull(), err
			}
			name, err := argString(args, 1)
			if err != nil {
				return NewNull(), err
			}
			old, found := obj.Get(name)
			if !found || isFunction(old) {
				return NewNull(), fmt.Errorf("%s has no field '%s'", obj.shape.typeName, name)
			}
			was, is := kindName(old), kindName(args[2])
			if was != "null" && is != "null" && was != is {
				return NewNull(), fmt.Errorf("the field '%s' of %s holds %s, it cannot be given %s", name, obj.shape.typeName, article(typeName(old)), article(typeName(args[2])))
			}
			if obj.header.Frozen {
				return NewNull(), fmt.Errorf(errFrozen)
			}
			obj.Set(name, args[2])
			return NewNull(), nil
		}},
		// The items of an array as values of any type, in a new array
		native{"items", 1, func(vm *VM, args []Value) (Value, error) {
			arr, err := argArray(args, 0)
			if err != nil {
				return NewNull(), err
			}
			out, _, err := vm.heap.AllocArray(arr.Len())
			if err != nil {
				return NewNull(), err
			}
			result := vm.pin(NewHeapPtr(uintptr(unsafe.Pointer(out))))
			for i := 0; i < arr.Len(); i++ {
				if err := vm.appendToArray(out, arr.Get(i)); err != nil {
					return NewNull(), err
				}
			}
			return result, nil
		}},
	)
}

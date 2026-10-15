package vm

import "strings"

// ShowMethod is the method the compiler gives every type that implements
// std.ops.Show, which calls its show. Programs cannot write the name. A
// value of such a type found inside an array, a struct or an enum is
// written by it when printed: [v2], not [{n: 2}].
const ShowMethod = "__shown"

// format writes a value as print and toString do, calling the show of the
// values in it whose type implements Show. A show that fails stops the
// printing with its failure.
func (vm *VM) format(v Value) (string, error) {
	f := formatter{room: maxFormatLength}
	var failed error
	f.show = func(obj *ObjectObj, v Value) (string, bool) {
		if failed != nil {
			return "", false
		}
		fn, ok := vm.showMethodOf(obj)
		if !ok {
			return "", false
		}
		result, err := vm.callFunction(fn, []Value{v})
		if err != nil {
			failed = err
			return "", false
		}
		text, ok := stringValue(result)
		return text, ok
	}
	text := f.formatValue(v, 0)
	return text, failed
}

// showMethodOf is the function that shows values of the type of obj, if
// the type implements Show. It is looked for in the program running, as
// the methods a call asks for are (see chunkCache.method).
func (vm *VM) showMethodOf(obj *ObjectObj) (Value, bool) {
	chunk := vm.chunk
	if chunk == nil {
		return NewNull(), false
	}
	kept := vm.cacheFor(chunk)
	if kept.byName == nil {
		kept.byName = make(map[string]int, len(chunk.Prototypes))
		kept.methodNames = make(map[uint16]int32)
		for i := range chunk.Prototypes {
			kept.byName[chunk.Prototypes[i].Name] = i
		}
	}

	owner := obj.shape.root.typeName
	if obj.shape.root.variant {
		// "Enum.Variant": the methods are the enum's
		if dot := strings.LastIndex(owner, "."); dot >= 0 {
			owner = owner[:dot]
		}
	}
	found, ok := kept.byName[owner+"."+ShowMethod]
	if !ok || owner == "" || chunk.Prototypes[found].UpvalueCount != 0 {
		return NewNull(), false
	}
	if fn := kept.funcs[found]; fn != 0 {
		return fn, true
	}
	fn, err := vm.functionObject(chunk, kept.funcs, found)
	return fn, err == nil
}

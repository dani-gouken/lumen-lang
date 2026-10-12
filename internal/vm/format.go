package vm

import (
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

// maxFormatDepth bounds nesting when formatting, so that a structure that
// contains itself cannot recurse forever.
const maxFormatDepth = 8

// FormatValue returns the user-facing text of a value, as printed by
// print/println and returned by toString. Unlike Value.String, which is a
// debug representation, it follows heap pointers.
func FormatValue(v Value) string {
	return formatValue(v, 0)
}

func formatValue(v Value, depth int) string {
	switch {
	case v.IsInt():
		return strconv.FormatInt(v.AsInt(), 10)
	case v.IsBool():
		return strconv.FormatBool(v.AsBool())
	case v.IsNull():
		return "null"
	case v.IsNumber():
		return strconv.FormatFloat(v.AsNumber(), 'g', -1, 64)
	case !v.IsHeapPtr() || v.AsHeapPtr() == 0:
		return v.String()
	}

	ptr := unsafe.Pointer(v.AsHeapPtr())
	switch (*ObjHeader)(ptr).Type {
	case ObjTypeString:
		return (*StringObj)(ptr).Data()

	case ObjTypeArray:
		if depth >= maxFormatDepth {
			return "[...]"
		}
		arr := (*ArrayObj)(ptr)
		parts := make([]string, arr.Len())
		for i := range parts {
			parts[i] = formatNested(arr.Get(i), depth+1)
		}
		return "[" + strings.Join(parts, ", ") + "]"

	case ObjTypeObject:
		if depth >= maxFormatDepth {
			return "{...}"
		}
		obj := (*ObjectObj)(ptr)
		keys := obj.Keys()
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			value, _ := obj.Get(key)
			if isFunction(value) {
				continue // Methods attached to a struct value are not data
			}
			parts = append(parts, key+": "+formatNested(value, depth+1))
		}
		return "{" + strings.Join(parts, ", ") + "}"

	case ObjTypeFunction:
		return "<fn " + (*FunctionObj)(ptr).Proto().Name + ">"
	case ObjTypeExternFunction:
		return "<fn " + (*ExternFunctionObj)(ptr).Name() + ">"
	}
	return v.String()
}

// formatNested formats a value inside an array or object, where strings are
// quoted to keep them apart from the surrounding syntax.
func formatNested(v Value, depth int) string {
	if s, ok := stringValue(v); ok {
		return strconv.Quote(s)
	}
	return formatValue(v, depth)
}

// heapType returns the object type of a heap value.
func heapType(v Value) (ObjType, bool) {
	if !v.IsHeapPtr() || v.AsHeapPtr() == 0 {
		return 0, false
	}
	return (*ObjHeader)(unsafe.Pointer(v.AsHeapPtr())).Type, true
}

// stringValue returns the Go string of a heap string value.
func stringValue(v Value) (string, bool) {
	if t, ok := heapType(v); !ok || t != ObjTypeString {
		return "", false
	}
	return (*StringObj)(unsafe.Pointer(v.AsHeapPtr())).Data(), true
}

func isFunction(v Value) bool {
	t, ok := heapType(v)
	return ok && (t == ObjTypeFunction || t == ObjTypeExternFunction)
}

// typeName describes a value's runtime type for error messages.
func typeName(v Value) string {
	switch {
	case v.IsInt():
		return "Int"
	case v.IsBool():
		return "Bool"
	case v.IsNull():
		return "null"
	case v.IsNumber():
		return "Float"
	}
	switch t, _ := heapType(v); t {
	case ObjTypeString:
		return "String"
	case ObjTypeArray:
		return "array"
	case ObjTypeObject:
		return "struct"
	case ObjTypeFunction, ObjTypeExternFunction:
		return "function"
	}
	return "unknown"
}

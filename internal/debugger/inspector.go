package debugger

import (
	"fmt"
	"sort"
	"strings"
	"unsafe"

	"github.com/dani-gouken/lumen/internal/vm"
)

// InspectValue returns a detailed string representation of a value.
func InspectValue(val vm.Value) string {
	switch {
	case val.IsNumber():
		return fmt.Sprintf("Number(%v)", val.AsNumber())
	case val.IsInt():
		return fmt.Sprintf("Int(%d)", val.AsInt())
	case val.IsBool():
		return fmt.Sprintf("Bool(%v)", val.AsBool())
	case val.IsNull():
		return "Null"
	case val.IsHeapPtr():
		return InspectHeapObject(val.AsHeapPtr())
	default:
		return fmt.Sprintf("Unknown(0x%x)", uint64(val))
	}
}

// InspectHeapObject inspects a heap-allocated object.
func InspectHeapObject(ptr uintptr) string {
	if ptr == 0 {
		return "HeapPtr(null)"
	}

	header := (*vm.ObjHeader)(unsafe.Pointer(ptr))
	if header == nil {
		return fmt.Sprintf("HeapPtr(0x%x, invalid)", ptr)
	}

	switch header.Type {
	case vm.ObjTypeString:
		return InspectString(ptr)
	case vm.ObjTypeArray:
		return InspectArray(ptr)
	case vm.ObjTypeObject:
		return InspectObject(ptr)
	default:
		return fmt.Sprintf("HeapPtr(0x%x, type=%d)", ptr, header.Type)
	}
}

// InspectString inspects a string object.
func InspectString(ptr uintptr) string {
	strObj := (*vm.StringObj)(unsafe.Pointer(ptr))
	if strObj == nil {
		return "String(null)"
	}

	str := strObj.String()
	if len(str) > 50 {
		return fmt.Sprintf("String(%q... [%d chars])", str[:50], len(str))
	}
	return fmt.Sprintf("String(%q)", str)
}

// InspectArray inspects an array object.
func InspectArray(ptr uintptr) string {
	arrayObj := (*vm.ArrayObj)(unsafe.Pointer(ptr))
	if arrayObj == nil {
		return "Array(null)"
	}

	length := arrayObj.Len()
	if length == 0 {
		return "Array([])"
	}

	var sb strings.Builder
	sb.WriteString("Array([")
	maxShow := 5
	if length < maxShow {
		maxShow = length
	}

	for i := 0; i < maxShow; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		val := arrayObj.Get(i)
		sb.WriteString(formatValueBrief(val))
	}

	if length > maxShow {
		sb.WriteString(fmt.Sprintf(", ... +%d more", length-maxShow))
	}

	sb.WriteString(fmt.Sprintf("] len=%d)", length))
	return sb.String()
}

// InspectObject inspects an object.
func InspectObject(ptr uintptr) string {
	objObj := (*vm.ObjectObj)(unsafe.Pointer(ptr))
	if objObj == nil {
		return "Object(null)"
	}

	fields := objObj.Fields()
	if len(fields) == 0 {
		return "Object({})"
	}

	var sb strings.Builder
	sb.WriteString("Object({")

	count := 0
	maxShow := 5
	for key, val := range fields {
		if count > 0 {
			sb.WriteString(", ")
		}
		if count >= maxShow {
			sb.WriteString(fmt.Sprintf("... +%d more", len(fields)-maxShow))
			break
		}
		sb.WriteString(fmt.Sprintf("%q: %s", key, formatValueBrief(val)))
		count++
	}

	sb.WriteString("})")
	return sb.String()
}

// formatValueBrief returns a brief representation of a value (for nested display).
func formatValueBrief(val vm.Value) string {
	switch {
	case val.IsNumber():
		return fmt.Sprintf("%v", val.AsNumber())
	case val.IsInt():
		return fmt.Sprintf("%di", val.AsInt())
	case val.IsBool():
		return fmt.Sprintf("%v", val.AsBool())
	case val.IsNull():
		return "null"
	case val.IsHeapPtr():
		header := (*vm.ObjHeader)(unsafe.Pointer(val.AsHeapPtr()))
		if header == nil {
			return "ptr(null)"
		}
		switch header.Type {
		case vm.ObjTypeString:
			strObj := (*vm.StringObj)(unsafe.Pointer(val.AsHeapPtr()))
			str := strObj.String()
			if len(str) > 20 {
				return fmt.Sprintf("%q...", str[:20])
			}
			return fmt.Sprintf("%q", str)
		case vm.ObjTypeArray:
			arrayObj := (*vm.ArrayObj)(unsafe.Pointer(val.AsHeapPtr()))
			return fmt.Sprintf("Array[%d]", arrayObj.Len())
		case vm.ObjTypeObject:
			return "Object{...}"
		default:
			return "ptr(...)"
		}
	default:
		return fmt.Sprintf("0x%x", uint64(val))
	}
}

// InspectArrayDetailed returns detailed information about an array.
func InspectArrayDetailed(ptr uintptr) string {
	arrayObj := (*vm.ArrayObj)(unsafe.Pointer(ptr))
	if arrayObj == nil {
		return "Array is null"
	}

	var sb strings.Builder
	length := arrayObj.Len()
	sb.WriteString(fmt.Sprintf("Array (length=%d)\n", length))

	for i := 0; i < length; i++ {
		val := arrayObj.Get(i)
		sb.WriteString(fmt.Sprintf("  [%d] = %s\n", i, InspectValue(val)))
	}

	return sb.String()
}

// InspectObjectDetailed returns detailed information about an object.
func InspectObjectDetailed(ptr uintptr) string {
	objObj := (*vm.ObjectObj)(unsafe.Pointer(ptr))
	if objObj == nil {
		return "Object is null"
	}

	var sb strings.Builder
	fields := objObj.Fields()
	sb.WriteString(fmt.Sprintf("Object (fields=%d)\n", len(fields)))

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		val := fields[key]
		sb.WriteString(fmt.Sprintf("  %q: %s\n", key, InspectValue(val)))
	}

	return sb.String()
}

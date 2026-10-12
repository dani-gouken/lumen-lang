package vm

import (
	"fmt"
	"math"
)

// Value represents a NaN-boxed 64-bit value.
// Layout: [sign:1][exp:11][tag:4][payload:48]
//
// A value whose exponent bits are not all ones is an ordinary float. With
// all ones, the tag says what it is. Tag 0 is left to the floats that have
// that exponent themselves, infinity and NaN, so that they stay numbers;
// every other kind of value has a non-zero tag.
type Value uint64

const (
	expMask      uint64 = 0x7FF0000000000000
	mantMask     uint64 = 0x000FFFFFFFFFFFFF
	tagMask      uint64 = 0x000F000000000000
	payloadMask  uint64 = 0x0000FFFFFFFFFFFF // 48 bits for payload (no tag bits)
	fullPayload  uint64 = 0x000FFFFFFFFFFFFF // 52 bits (tag + payload)

	tagFloat   uint64 = 0x0000000000000000 // infinity and NaN
	tagBool    uint64 = 0x0001000000000000
	tagInt     uint64 = 0x0002000000000000
	tagHeapPtr uint64 = 0x0003000000000000
	tagNull    uint64 = 0x0004000000000000

	nanBase uint64 = 0x7FF0000000000000

	// The one bit pattern used for a float NaN. Go's NaNs set mantissa bits
	// that would read as a tag.
	canonicalNaN uint64 = nanBase | tagFloat | 1
)

// Constructors

// NewNumber creates a Number value from a float64.
func NewNumber(f float64) Value {
	if f != f {
		return Value(canonicalNaN)
	}
	return Value(math.Float64bits(f))
}

// NewInt creates an Int value from an int64.
// Only 48 bits are available, so values must fit in that range.
func NewInt(i int64) Value {
	return Value(nanBase | tagInt | (uint64(i) & payloadMask))
}

// NewBool creates a Bool value.
func NewBool(b bool) Value {
	if b {
		return Value(nanBase | tagBool | 1)
	}
	return Value(nanBase | tagBool)
}

// NewNull creates a Null value.
func NewNull() Value {
	return Value(nanBase | tagNull)
}

// NewHeapPtr creates a HeapPtr value from a pointer.
func NewHeapPtr(ptr uintptr) Value {
	return Value(nanBase | tagHeapPtr | (uint64(ptr) & payloadMask))
}

// Predicates

// IsNumber returns true if v is a Number, including infinity and NaN.
func (v Value) IsNumber() bool {
	return (uint64(v)&expMask) != expMask || (uint64(v)&tagMask) == tagFloat
}

// IsNaN returns true if v is a NaN-boxed value, that is, anything but a
// Number.
func (v Value) IsNaN() bool {
	return !v.IsNumber()
}

// IsNull returns true if v is Null.
func (v Value) IsNull() bool {
	return v.IsNaN() && (uint64(v)&tagMask) == tagNull
}

// IsBool returns true if v is a Bool.
func (v Value) IsBool() bool {
	return v.IsNaN() && (uint64(v)&tagMask) == tagBool
}

// IsInt returns true if v is an Int.
func (v Value) IsInt() bool {
	return v.IsNaN() && (uint64(v)&tagMask) == tagInt
}

// IsHeapPtr returns true if v is a HeapPtr.
func (v Value) IsHeapPtr() bool {
	return v.IsNaN() && (uint64(v)&tagMask) == tagHeapPtr
}

// Accessors

// AsNumber returns the Number value.
// Panics if v is not a Number.
func (v Value) AsNumber() float64 {
	if !v.IsNumber() {
		panic("value is not a Number")
	}
	return math.Float64frombits(uint64(v))
}

// AsInt returns the Int value.
// Panics if v is not an Int.
func (v Value) AsInt() int64 {
	if !v.IsInt() {
		panic("value is not an Int")
	}
	payload := uint64(v) & payloadMask
	// Sign-extend from 48 bits
	if payload&(1<<47) != 0 {
		payload |= 0xFFFF000000000000
	}
	return int64(payload)
}

// AsBool returns the Bool value.
// Panics if v is not a Bool.
func (v Value) AsBool() bool {
	if !v.IsBool() {
		panic("value is not a Bool")
	}
	return (uint64(v) & payloadMask) != 0
}

// AsHeapPtr returns the HeapPtr value.
// Panics if v is not a HeapPtr.
func (v Value) AsHeapPtr() uintptr {
	if !v.IsHeapPtr() {
		panic("value is not a HeapPtr")
	}
	return uintptr(uint64(v) & payloadMask)
}

// String returns a debug representation of the value.
func (v Value) String() string {
	switch {
	case v.IsNumber():
		return fmt.Sprintf("Number(%v)", v.AsNumber())
	case v.IsInt():
		return fmt.Sprintf("Int(%d)", v.AsInt())
	case v.IsBool():
		return fmt.Sprintf("Bool(%v)", v.AsBool())
	case v.IsNull():
		return "Null"
	case v.IsHeapPtr():
		return fmt.Sprintf("HeapPtr(0x%x)", v.AsHeapPtr())
	default:
		return fmt.Sprintf("Unknown(0x%x)", uint64(v))
	}
}

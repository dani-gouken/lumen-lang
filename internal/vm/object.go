package vm

import "unicode/utf8"

// ObjType represents the type of a heap-allocated object.
type ObjType byte

const (
	ObjTypeString ObjType = iota
	ObjTypeArray
	ObjTypeObject
	ObjTypeFunction       // Phase A: Basic functions (Phase B will add upvalues)
	ObjTypeExternFunction // External (host) functions
	ObjTypeUpvalue        // Phase B: For closure support
)

// ObjHeader is the header for all heap-allocated objects.
type ObjHeader struct {
	Type   ObjType
	Marked bool
	Next   *ObjHeader
}

// Obj is the interface for all heap-allocated objects.
type Obj interface {
	Header() *ObjHeader
	String() string
}

// StringObj represents a heap-allocated string.
type StringObj struct {
	header ObjHeader
	data   string

	// Positions are counted in characters, the bytes of the text are
	// UTF-8. What makes one into the other is worked out the first time it
	// is asked for and kept: the number of characters, and for a text that
	// is not plain ASCII where every runeStride-th character starts.
	measured bool
	ascii    bool
	runes    int
	starts   []uint32
}

// runeStride is how far apart the recorded character positions are: the
// most that is decoded to reach any one character.
const runeStride = 32

func (s *StringObj) Header() *ObjHeader {
	return &s.header
}

func (s *StringObj) String() string {
	return s.data
}

func (s *StringObj) Data() string {
	return s.data
}

func (s *StringObj) measure() {
	s.measured = true
	s.ascii = true
	for i := 0; i < len(s.data); i++ {
		if s.data[i] >= utf8.RuneSelf {
			s.ascii = false
			break
		}
	}
	if s.ascii {
		s.runes = len(s.data)
		return
	}
	n := 0
	for offset := range s.data {
		if n%runeStride == 0 {
			s.starts = append(s.starts, uint32(offset))
		}
		n++
	}
	s.runes = n
}

// RuneLen is the length of the text in characters.
func (s *StringObj) RuneLen() int {
	if !s.measured {
		s.measure()
	}
	return s.runes
}

// offsetOf is where the character at a position starts among the bytes of
// the text. The position may be the length: the end of the text.
func (s *StringObj) offsetOf(i int) int {
	if !s.measured {
		s.measure()
	}
	if s.ascii {
		return i
	}
	if i >= s.runes {
		return len(s.data)
	}
	offset := int(s.starts[i/runeStride])
	for skip := i % runeStride; skip > 0; skip-- {
		_, size := utf8.DecodeRuneInString(s.data[offset:])
		offset += size
	}
	return offset
}

// RuneAt returns the character at a position, as the bytes of the text
// that hold it. The position must be within the text.
func (s *StringObj) RuneAt(i int) string {
	offset := s.offsetOf(i)
	if s.ascii {
		return s.data[offset : offset+1]
	}
	_, size := utf8.DecodeRuneInString(s.data[offset:])
	return s.data[offset : offset+size]
}

// Slice returns the characters from one position up to another. Both must
// be within the text or at its end, and in order.
func (s *StringObj) Slice(from, to int) string {
	return s.data[s.offsetOf(from):s.offsetOf(to)]
}

// ArrayObj represents a heap-allocated array.
type ArrayObj struct {
	header   ObjHeader
	elements []Value
}

func (a *ArrayObj) Header() *ObjHeader {
	return &a.header
}

func (a *ArrayObj) String() string {
	return "[array]"
}

func (a *ArrayObj) Len() int {
	return len(a.elements)
}

func (a *ArrayObj) Get(i int) Value {
	if i < 0 || i >= len(a.elements) {
		return NewNull()
	}
	return a.elements[i]
}

func (a *ArrayObj) Set(i int, v Value) {
	if i >= 0 && i < cap(a.elements) {
		// Grow if needed
		for len(a.elements) <= i {
			a.elements = append(a.elements, NewNull())
		}
		a.elements[i] = v
	}
}

func (a *ArrayObj) Append(v Value) {
	a.elements = append(a.elements, v)
}

// Shape is what the objects with the same fields, added in the same
// order, have in common: which field is in which slot. An object holds its
// shape and the values; the names are kept once per shape.
//
// Shapes form a tree. Adding a field to an object moves it to the shape's
// child for that name, made the first time and found again afterwards, so
// that every value of a struct ends up with the very same shape. That is
// what lets an instruction remember where a field is (see fieldCache).
type Shape struct {
	keys  []string
	index map[string]int
	next  map[string]*Shape
}

func newShape() *Shape {
	return &Shape{index: map[string]int{}}
}

// with returns the shape of an object of this shape given one more field.
func (s *Shape) with(key string) *Shape {
	if child, ok := s.next[key]; ok {
		return child
	}
	child := &Shape{
		keys:  append(s.keys[:len(s.keys):len(s.keys)], key),
		index: make(map[string]int, len(s.keys)+1),
	}
	for name, slot := range s.index {
		child.index[name] = slot
	}
	child.index[key] = len(s.keys)
	if s.next == nil {
		s.next = map[string]*Shape{}
	}
	s.next[key] = child
	return child
}

// ObjectObj represents a heap-allocated object: a value of a struct or of
// an enum, or a plain object made from assembly.
type ObjectObj struct {
	header ObjHeader
	shape  *Shape
	slots  []Value

	// root is the shape of an object without fields, of the heap this
	// object is on: where taking a field away starts again from.
	root *Shape

	// Name of the struct this object is a value of; empty for a plain
	// object created from assembly. Used by casts.
	typeName string

	// variant marks a value of an enum. typeName is then "Enum.Variant"
	// and the values it holds are the fields "0", "1", ... Such a value is
	// compared and printed by what it holds.
	variant bool
}

// Third operand of NEWOBJ: what the object is.
const (
	ObjectIsNamed   = 1 // A value of a struct; B is the constant of its name
	ObjectIsVariant = 2 // A value of an enum; B is the constant of "Enum.Variant"
)

// TypeName returns the name of the struct the object is a value of.
func (o *ObjectObj) TypeName() string {
	return o.typeName
}

func (o *ObjectObj) Header() *ObjHeader {
	return &o.header
}

func (o *ObjectObj) String() string {
	return "[object]"
}

func (o *ObjectObj) Get(key string) (Value, bool) {
	if slot, ok := o.shape.index[key]; ok {
		return o.slots[slot], true
	}
	return NewNull(), false
}

// Set gives a field a value, adding the field if the object has none of
// that name. It does not account for the memory a new field takes: inside
// the VM, use setField.
func (o *ObjectObj) Set(key string, v Value) {
	if slot, ok := o.shape.index[key]; ok {
		o.slots[slot] = v
		return
	}
	o.shape = o.shape.with(key)
	o.slots = append(o.slots, v)
}

// Delete takes a field away, and reports whether there was one. The fields
// left keep their order.
func (o *ObjectObj) Delete(key string) bool {
	at, ok := o.shape.index[key]
	if !ok {
		return false
	}
	shape := o.root
	for i, name := range o.shape.keys {
		if i != at {
			shape = shape.with(name)
		}
	}
	o.slots = append(o.slots[:at], o.slots[at+1:]...)
	o.shape = shape
	return true
}

// Len is the number of fields.
func (o *ObjectObj) Len() int {
	return len(o.slots)
}

// Keys lists the names of the fields, in the order they were added.
func (o *ObjectObj) Keys() []string {
	return append([]string(nil), o.shape.keys...)
}

// Fields returns a copy of all fields (for debugging/inspection).
func (o *ObjectObj) Fields() map[string]Value {
	fieldsCopy := make(map[string]Value, len(o.slots))
	for i, key := range o.shape.keys {
		fieldsCopy[key] = o.slots[i]
	}
	return fieldsCopy
}

// FunctionObj represents a heap-allocated first-class function.
// Supports closures with upvalues for variable capture.
type FunctionObj struct {
	header   ObjHeader
	chunk    *Chunk             // Owning chunk containing prototypes and code
	protoIdx int                // Index into chunk.Prototypes
	proto    *FunctionPrototype // The prototype itself
	upvalues []*UpvalueObj      // Captured variables (can be empty for non-closures)
}

func (f *FunctionObj) Header() *ObjHeader {
	return &f.header
}

func (f *FunctionObj) String() string {
	return "[function]"
}

// Proto returns the function prototype metadata.
func (f *FunctionObj) Proto() *FunctionPrototype {
	return f.proto
}

// Upvalues returns the captured upvalues for this function.
func (f *FunctionObj) Upvalues() []*UpvalueObj {
	return f.upvalues
}

// SetUpvalue sets an upvalue at the given index.
func (f *FunctionObj) SetUpvalue(idx int, upval *UpvalueObj) {
	if idx >= 0 && idx < len(f.upvalues) {
		f.upvalues[idx] = upval
	}
}

// ExternFunctionObj represents a heap-allocated external (host) function.
// External functions are implemented in Go and called from bytecode.
type ExternFunctionObj struct {
	header   ObjHeader
	name     string     // Function name
	function ExternFunc // Go function pointer
	argCount int        // Expected argument count

	// Type of the function as a cast descriptor; empty if not declared
	signature string
}

func (ef *ExternFunctionObj) Header() *ObjHeader {
	return &ef.header
}

func (ef *ExternFunctionObj) String() string {
	return "[extern:" + ef.name + "]"
}

func (ef *ExternFunctionObj) Name() string {
	return ef.name
}

func (ef *ExternFunctionObj) Function() ExternFunc {
	return ef.function
}

func (ef *ExternFunctionObj) ArgCount() int {
	return ef.argCount
}

// UpvalueObj represents a captured variable in a closure.
// Upvalues can be in two states:
// 1. Open: the variable is still a register of a call in progress
// 2. Closed: the call has returned, and the upvalue holds the value itself
//
// An open upvalue names its register by the stack it is on and its place
// there, not by its address: a stack that grows moves.
type UpvalueObj struct {
	header ObjHeader
	stack  *regStack // The stack the variable is on (nil when closed)
	slot   int       // Its place on that stack
	closed Value     // Closed-over value (used when stack == nil)
}

func (u *UpvalueObj) Header() *ObjHeader {
	return &u.header
}

func (u *UpvalueObj) String() string {
	return "[upvalue]"
}

// IsOpen returns true if the upvalue is still pointing to a stack location.
func (u *UpvalueObj) IsOpen() bool {
	return u.stack != nil
}

// Get returns the current value of the upvalue.
func (u *UpvalueObj) Get() Value {
	if u.stack != nil {
		return u.stack.vals[u.slot]
	}
	return u.closed
}

// Set updates the value of the upvalue.
func (u *UpvalueObj) Set(v Value) {
	if u.stack != nil {
		u.stack.vals[u.slot] = v
	} else {
		u.closed = v
	}
}

// Close moves the upvalue from stack to heap.
// This is called when the stack frame containing the variable is about to be destroyed.
func (u *UpvalueObj) Close() {
	if u.stack != nil {
		u.closed = u.stack.vals[u.slot]
		u.stack = nil
	}
}

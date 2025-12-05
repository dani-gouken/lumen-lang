package vm

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
}

func (s *StringObj) Header() *ObjHeader {
	return &s.header
}

func (s *StringObj) String() string {
	return s.data
}

func (s *StringObj) Data() string {
	return s.data
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

// ObjectObj represents a heap-allocated object/map.
type ObjectObj struct {
	header ObjHeader
	fields map[string]Value
}

func (o *ObjectObj) Header() *ObjHeader {
	return &o.header
}

func (o *ObjectObj) String() string {
	return "[object]"
}

func (o *ObjectObj) Get(key string) (Value, bool) {
	v, ok := o.fields[key]
	return v, ok
}

func (o *ObjectObj) Set(key string, v Value) {
	o.fields[key] = v
}

func (o *ObjectObj) Keys() []string {
	keys := make([]string, 0, len(o.fields))
	for k := range o.fields {
		keys = append(keys, k)
	}
	return keys
}

// Fields returns a copy of all fields (for debugging/inspection).
func (o *ObjectObj) Fields() map[string]Value {
	fieldsCopy := make(map[string]Value, len(o.fields))
	for k, v := range o.fields {
		fieldsCopy[k] = v
	}
	return fieldsCopy
}

// FunctionObj represents a heap-allocated first-class function.
// Supports closures with upvalues for variable capture.
type FunctionObj struct {
	header   ObjHeader
	chunk    *Chunk           // Owning chunk containing prototypes and code
	protoIdx int              // Index into chunk.Prototypes
	upvalues []*UpvalueObj    // Captured variables (can be empty for non-closures)
}

func (f *FunctionObj) Header() *ObjHeader {
	return &f.header
}

func (f *FunctionObj) String() string {
	return "[function]"
}

// Proto returns the function prototype metadata.
func (f *FunctionObj) Proto() *FunctionPrototype {
	return &f.chunk.Prototypes[f.protoIdx]
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
	name     string      // Function name
	function ExternFunc // Go function pointer
	argCount int         // Expected argument count
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
// 1. Open: pointing to a stack slot (when the variable is still on the stack)
// 2. Closed: containing a copy of the value (when the stack frame has returned)
type UpvalueObj struct {
	header   ObjHeader
	location *Value // Pointer to stack slot (nil when closed)
	closed   Value  // Closed-over value (used when location == nil)
}

func (u *UpvalueObj) Header() *ObjHeader {
	return &u.header
}

func (u *UpvalueObj) String() string {
	return "[upvalue]"
}

// IsOpen returns true if the upvalue is still pointing to a stack location.
func (u *UpvalueObj) IsOpen() bool {
	return u.location != nil
}

// Get returns the current value of the upvalue.
func (u *UpvalueObj) Get() Value {
	if u.location != nil {
		return *u.location
	}
	return u.closed
}

// Set updates the value of the upvalue.
func (u *UpvalueObj) Set(v Value) {
	if u.location != nil {
		*u.location = v
	} else {
		u.closed = v
	}
}

// Close moves the upvalue from stack to heap.
// This is called when the stack frame containing the variable is about to be destroyed.
func (u *UpvalueObj) Close() {
	if u.location != nil {
		u.closed = *u.location
		u.location = nil
	}
}

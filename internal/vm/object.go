package vm

// ObjType represents the type of a heap-allocated object.
type ObjType byte

const (
	ObjTypeString ObjType = iota
	ObjTypeArray
	ObjTypeObject
	ObjTypeClosure
	ObjTypeUpvalue
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

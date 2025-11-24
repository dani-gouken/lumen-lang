package vm

// ObjType represents the type of a heap-allocated object.
type ObjType byte

const (
	ObjTypeString ObjType = iota
	ObjTypeArray
	ObjTypeClosure
	ObjTypeUpvalue
)

// ObjHeader is the header for all heap-allocated objects.
type ObjHeader struct {
	Type   ObjType
	Marked bool
	Next   *Obj
}

// Obj is the interface for all heap-allocated objects.
type Obj interface {
	Header() *ObjHeader
	String() string
}

// Example: StringObj (commented for reference)
/*
type StringObj struct {
	header ObjHeader
	data   string
}

func NewStringObj(s string) *StringObj {
	return &StringObj{
		header: ObjHeader{Type: ObjTypeString},
		data:   s,
	}
}

func (s *StringObj) Header() *ObjHeader {
	return &s.header
}

func (s *StringObj) String() string {
	return s.data
}
*/

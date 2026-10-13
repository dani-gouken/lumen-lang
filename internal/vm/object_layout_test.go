package vm

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

// How struct and enum values are laid out: what the values of a type have
// in common is kept once, a value is made in one piece with room for its
// fields, a value that holds nothing is made once, and asking a value for
// its type is asking for a name it already carries.

func TestValuesOfATypeShareWhatTheyHaveInCommon(t *testing.T) {
	heap := NewHeap(0)
	point := heap.RootShape("Point", false)
	if heap.RootShape("Point", false) != point {
		t.Fatal("a type has two shapes to start from")
	}
	if heap.RootShape("Point", true) == point || heap.RootShape("Vector", false) == point || heap.rootShape == point {
		t.Fatal("two types start from the same shape")
	}

	a, _, _ := heap.AllocObjectOf(point, 2)
	b, _, _ := heap.AllocObjectOf(point, 2)
	for _, obj := range []*ObjectObj{a, b} {
		obj.Set("x", NewInt(1))
		obj.Set("y", NewInt(2))
	}
	if a.shape != b.shape || a.shape == point {
		t.Error("two values given the same fields do not share a shape")
	}
	if a.TypeName() != "Point" || typeName(valueOf(&a.header)) != "Point" || a.shape.variant {
		t.Errorf("the value says it is %q", a.TypeName())
	}

	// Taking a field away leaves a value of the same type
	a.Delete("x")
	if a.TypeName() != "Point" || a.shape.root != point || a.Len() != 1 {
		t.Errorf("after losing a field: %q, %d fields", a.TypeName(), a.Len())
	}

	// A plain object is of no type, and says so
	plain, _, _ := heap.AllocObject()
	if plain.TypeName() != "" || typeName(valueOf(&plain.header)) != "struct" {
		t.Errorf("a plain object says it is %q", plain.TypeName())
	}

	// An object is its shape and its values, and nothing per value besides
	if size := unsafe.Sizeof(ObjectObj{}); size > 48 {
		t.Errorf("an object takes %d bytes before its values", size)
	}
}

// A value made with the number of its fields known is one allocation, and
// its fields are where they should be whatever that number was. One given
// more than was said still holds them all.
func TestObjectsComeWithRoomForTheirFields(t *testing.T) {
	heap := NewHeap(0)
	shape := heap.RootShape("Record", false)

	for _, fields := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 13, 16, 17, 40} {
		obj, _, err := heap.AllocObjectOf(shape, fields)
		if err != nil {
			t.Fatal(err)
		}
		if cap(obj.slots) < fields || len(obj.slots) != 0 {
			t.Fatalf("%d fields: room for %d, %d there already", fields, cap(obj.slots), len(obj.slots))
		}
		if cap(obj.slots) != roomFor(fields) {
			t.Errorf("%d fields: room for %d, counted as %d", fields, cap(obj.slots), roomFor(fields))
		}
		room := cap(obj.slots)

		// Twice as many as said, each checked after every one added
		for i := 0; i < 2*fields+3; i++ {
			obj.Set(fmt.Sprint("f", i), NewInt(int64(i*7)))
			for j := 0; j <= i; j++ {
				if v, ok := obj.Get(fmt.Sprint("f", j)); !ok || v.AsInt() != int64(j*7) {
					t.Fatalf("%d fields: after adding %d, field %d is %v", fields, i+1, j, v)
				}
			}
			if i < fields && cap(obj.slots) != room {
				t.Errorf("%d fields: the values moved after only %d", fields, i+1)
			}
		}
		if obj.header.Type != ObjTypeObject || obj.TypeName() != "Record" {
			t.Errorf("%d fields: not an object of the type any more", fields)
		}
	}

	// One allocation for the object and its values, when the number is
	// one an object can come with
	for _, fields := range []int{1, 2, 4, 8, 16} {
		allocations := testing.AllocsPerRun(100, func() {
			obj := newObject(shape, fields)
			obj.slots = append(obj.slots, NewInt(1))
		})
		if allocations != 1 {
			t.Errorf("%d fields: %v allocations", fields, allocations)
		}
	}
}

// What an object is counted for when it is made is what is taken off when
// it is freed, with its room and whatever it grew to since.
func TestRoomIsCountedInAndOut(t *testing.T) {
	machine := NewVM()
	heap := machine.heap
	shape := heap.RootShape("Record", false)
	machine.CollectGarbage()
	before := heap.BytesAllocated()

	for fields := 0; fields < 30; fields++ {
		obj, _, _ := heap.AllocObjectOf(shape, fields)
		if counted := heap.BytesAllocated() - before; counted < uint64(fields)*valueSize {
			t.Fatalf("%d fields: %d bytes counted", fields, counted)
		}
		// Past its room, and back under it
		for i := 0; i < fields+5; i++ {
			if err := machine.setField(obj, fmt.Sprint("f", i), NewInt(1)); err != nil {
				t.Fatal(err)
			}
		}
		obj.Delete("f0")
		machine.CollectGarbage()
		if left := heap.BytesAllocated() - before; left != 0 {
			t.Fatalf("%d fields: %d bytes counted for an object that is gone", fields, left)
		}
	}
}

// variants is a program that makes the same value of an enum in a loop,
// one that holds nothing and one that holds a number, and returns whether
// the last two of each were the very same object.
func variants(constant uint16) *Chunk {
	chunk := NewChunk()
	chunk.StringPool = []string{"Light.Red", "Light.Dim", "0"}
	chunk.Constants = []Value{NewInt(0), NewInt(1), NewInt(2)}
	chunk.Code = []Instr{
		NewInstr(OpLOADI, 5, 200, 0),                                   // 0: turns left
		NewInstr(OpMOV, 2, 1, 0),                                       // 1: the one before
		NewInstr(OpNEWOBJ, 1, 0, ObjectIsVariant|constant),             // 2: Light.Red
		NewInstr(OpNEWOBJ, 3, 1, ObjectIsVariant|1<<ObjectFieldsShift), // 3: Light.Dim(turns)
		NewInstr(OpSETFIELD, 3, 2, 5),                                  // 4
		NewInstr(OpGC, 0, 0, 0),                                        // 5
		NewInstr(OpSUBI, 5, 5, 1),                                      // 6
		NewInstr(OpJNEQI, 5, 0, 1),                                     // 7: round again unless done
		NewInstr(OpNEWARRAY, 0, 2, 0),                                  // 8
		NewInstr(OpARRPUSH, 0, 1, 0),                                   // 9
		NewInstr(OpARRPUSH, 0, 2, 0),                                   // 10
		NewInstr(OpRET, 0, 0, 0),                                       // 11
	}
	chunk.Prototypes = []FunctionPrototype{{Name: "main", CodeSize: len(chunk.Code), RegisterCount: 6}}
	return chunk
}

func TestAValueThatHoldsNothingIsMadeOnce(t *testing.T) {
	last := func(machine *VM, chunk *Chunk) (Value, Value) {
		t.Helper()
		result, err := machine.Exec(chunk)
		if err != nil {
			t.Fatal(err)
		}
		pair := (*ArrayObj)(unsafe.Pointer(header(result)))
		return pair.Get(0), pair.Get(1)
	}

	// Marked as never given anything: the same object each time, through
	// two hundred collections
	machine := NewVM()
	a, b := last(machine, variants(ObjectIsConstant))
	if a != b {
		t.Error("two values that hold nothing are two objects")
	}
	if FormatValue(a) != "Red" || !valuesEqual(a, b) {
		t.Errorf("the value is %s", FormatValue(a))
	}
	// With the program over, only what it returned is left of it
	machine.regs = []Value{a}
	machine.caches = map[*Chunk]*chunkCache{}
	machine.CollectGarbage()
	if FormatValue(a) != "Red" {
		t.Error("the value did not survive its program")
	}

	// Not marked: a new one each time, equal all the same
	c, d := last(NewVM(), variants(0))
	if c == d || !valuesEqual(c, d) {
		t.Errorf("unmarked values: the same object %v, equal %v", c == d, valuesEqual(c, d))
	}

	// What is kept is kept for its chunk: another program's value of the
	// same variant is another object, and equal
	other := NewVM()
	first, _ := last(other, variants(ObjectIsConstant))
	other.regs = []Value{first}
	second, _ := last(other, variants(ObjectIsConstant))
	if !valuesEqual(first, second) {
		t.Error("the same variant from two programs is not equal")
	}
}

// Asking a value for its type by the instruction answers what asking the
// host function with the descriptor "{Name}" answers, for every value.
func TestAskingForATypeIsAskingForTheName(t *testing.T) {
	machine := NewVM()
	heap := machine.heap
	object := func(name string, variant bool) Value {
		obj, _, _ := heap.AllocObjectOf(heap.RootShape(name, variant), 1)
		obj.Set("0", NewInt(1))
		return valueOf(&obj.header)
	}
	text, _ := machine.newString("Shape.Circle")
	arr, _, _ := heap.AllocArray(0)
	plain, _, _ := heap.AllocObject()
	values := []Value{
		object("Shape.Circle", true), object("Shape.Square", true), object("Shape", false), object("Circle", false),
		object("Shape.Circle", false), // A struct that happens to be named like the variant
		valueOf(&plain.header), text, valueOf(&arr.header),
		NewInt(3), NewNumber(1.5), NewBool(true), NewNull(),
	}
	machine.regs = values // Kept through the allocations below

	for _, name := range []string{"Shape.Circle", "Shape.Square", "Shape", "Circle", "Nothing"} {
		typ, err := machine.castTypeFor("{" + name + "}")
		if err != nil {
			t.Fatal(err)
		}
		var known fieldCache
		// Twice round: what the instruction remembers must not change its answers
		for round := 0; round < 2; round++ {
			for _, v := range values {
				want := typ.mismatch(v) == ""
				if got := isOfType(v, &known, name); got != want {
					t.Errorf("round %d: is %s a %s? the instruction says %v, the function %v", round, FormatValue(v), name, got, want)
				}
			}
		}
	}
}

func TestTypeTestsRun(t *testing.T) {
	// r1 = a Shape.Circle; r2 = whether it is one; then jump on a type it
	// is not, and on the one it is
	chunk := NewChunk()
	chunk.StringPool = []string{"Shape.Circle", "Shape.Square"}
	chunk.Constants = []Value{NewInt(0), NewInt(1)}
	chunk.Code = []Instr{
		NewInstr(OpNEWOBJ, 1, 0, ObjectIsVariant), // 0
		NewInstr(OpISTYPE, 2, 1, 0),               // 1: true
		NewInstr(OpISTYPE, 3, 1, 1),               // 2: false
		NewInstr(OpJNTYPE, 1, 1, 6),               // 3: not a Square: go to 6
		NewInstr(OpLOADI, 0, 99, 0),               // 4
		NewInstr(OpRET, 0, 0, 0),                  // 5
		NewInstr(OpJNTYPE, 1, 0, 4),               // 6: a Circle: go on
		NewInstr(OpNEWARRAY, 0, 2, 0),             // 7
		NewInstr(OpARRPUSH, 0, 2, 0),              // 8
		NewInstr(OpARRPUSH, 0, 3, 0),              // 9
		NewInstr(OpRET, 0, 0, 0),                  // 10
	}
	chunk.Prototypes = []FunctionPrototype{{Name: "main", CodeSize: len(chunk.Code), RegisterCount: 4}}
	result, err := NewVM().Exec(chunk)
	if err != nil || FormatValue(result) != "[true, false]" {
		t.Fatalf("%s, %v", FormatValue(result), err)
	}

	for name, change := range map[string]func(*Chunk){
		"a name that is a number":      func(c *Chunk) { c.Constants[0] = NewNumber(1.5) },
		"a name outside the pool":      func(c *Chunk) { c.Constants[1] = NewInt(9) },
		"a jump out of the function":   func(c *Chunk) { c.Code[3].C = 40 },
		"a register that is not there": func(c *Chunk) { c.Code[1].A = 7 },
	} {
		damaged := *chunk
		damaged.Code = append([]Instr(nil), chunk.Code...)
		damaged.Constants = append([]Value(nil), chunk.Constants...)
		change(&damaged)
		if err := Verify(&damaged); err == nil || !strings.Contains(err.Error(), "invalid bytecode") {
			t.Errorf("%s: expected the chunk to be refused, got %v", name, err)
		}
	}
}

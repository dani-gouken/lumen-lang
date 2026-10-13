//go:build (darwin || linux) && (arm64 || amd64)

package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Calling the functions of C libraries, without a C compiler: the library
// is opened and the function found when the program first uses it, and the
// call is made by purego, which puts the arguments where the C calling
// convention of the processor wants them.
//
// A signature is only known when the program runs, so the Go function
// purego fills in is made by reflection, and so are the structs passed by
// value. A struct is given to purego flat, the values of the structs inside
// it in a row with the gaps C leaves between them: that is the same
// memory, and purego's handling of a struct inside a struct made by
// reflection cannot be relied on.

// ForeignSupported reports whether this build of lumen can call the
// functions of C libraries.
const ForeignSupported = true

// --- Libraries ---

var libraries = struct {
	sync.Mutex
	open map[string]uintptr
}{open: make(map[string]uintptr)}

// LibraryPathVariable names the environment variable that lists folders
// to look for libraries in, before the usual ones.
const LibraryPathVariable = "LUMEN_LIBRARY_PATH"

// libraryFolders are where libraries are looked for by name, after what
// the system itself does with the name.
func libraryFolders() []string {
	folders := filepath.SplitList(os.Getenv(LibraryPathVariable))
	if runtime.GOOS == "darwin" {
		return append(folders, "/opt/homebrew/lib", "/usr/local/lib", "/opt/local/lib", "/usr/lib")
	}
	return append(folders, "/usr/local/lib",
		"/usr/lib/"+linuxArch()+"-linux-gnu", "/lib/"+linuxArch()+"-linux-gnu",
		"/usr/lib64", "/usr/lib", "/lib64", "/lib")
}

func linuxArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x86_64"
}

// libraryFiles are the files a library may be, from the name a program
// gives it: "raylib" is libraylib.dylib or libraylib.so somewhere, a name
// with a '/' is that file and no other.
func libraryFiles(name string) []string {
	if strings.Contains(name, "/") {
		return []string{name}
	}
	if runtime.GOOS == "darwin" {
		// The C library and the mathematical functions are one library
		// there, which is not a file any more
		if name == "c" || name == "m" {
			return []string{"/usr/lib/libSystem.B.dylib"}
		}
	} else if name == "c" {
		return []string{"libc.so.6"}
	} else if name == "m" {
		return []string{"libm.so.6"}
	}

	var bases []string
	switch {
	case strings.Contains(name, ".dylib") || strings.Contains(name, ".so"):
		bases = []string{name}
	case runtime.GOOS == "darwin":
		bases = []string{"lib" + name + ".dylib", name + ".dylib"}
	default:
		bases = []string{"lib" + name + ".so", name + ".so"}
	}

	// By the name alone first: the system has its own places and rules
	files := append([]string(nil), bases...)
	for _, folder := range libraryFolders() {
		for _, base := range bases {
			files = append(files, filepath.Join(folder, base))
		}
		if runtime.GOOS != "darwin" {
			// libraylib.so.5.5.0, when the plain name is only installed
			// with what is needed to compile against the library
			versions, _ := filepath.Glob(filepath.Join(folder, "lib"+name+".so.*"))
			files = append(files, versions...)
		}
	}
	return files
}

func openLibrary(name string) (uintptr, error) {
	libraries.Lock()
	defer libraries.Unlock()
	if handle, ok := libraries.open[name]; ok {
		return handle, nil
	}
	// Why a file that is there could not be opened is worth saying; that
	// the others are not there is not
	var found error
	files := libraryFiles(name)
	for _, file := range files {
		handle, err := purego.Dlopen(file, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			libraries.open[name] = handle
			return handle, nil
		}
		if _, missing := os.Stat(file); missing == nil && found == nil {
			found = fmt.Errorf("%s could not be opened: %v", file, err)
		}
	}
	if found != nil {
		return 0, fmt.Errorf("the library \"%s\" was not loaded: %v", name, found)
	}
	return 0, fmt.Errorf("the library \"%s\" was not found: there is no %s where libraries are. Give its path, or list its folder in %s",
		name, filepath.Base(files[0]), LibraryPathVariable)
}

// --- Types ---

var goTypes = map[string]reflect.Type{
	CInt8: reflect.TypeOf(int8(0)), CUInt8: reflect.TypeOf(uint8(0)),
	CInt16: reflect.TypeOf(int16(0)), CUInt16: reflect.TypeOf(uint16(0)),
	CInt32: reflect.TypeOf(int32(0)), CUInt32: reflect.TypeOf(uint32(0)),
	CInt64: reflect.TypeOf(int64(0)), CUInt64: reflect.TypeOf(uint64(0)),
	CFloat32: reflect.TypeOf(float32(0)), CFloat64: reflect.TypeOf(float64(0)),
	CBool: reflect.TypeOf(false), CString: reflect.TypeOf(""), CPointer: reflect.TypeOf(uintptr(0)),
}

// cValue is how one parameter, or the result, goes between the program
// and C.
type cValue struct {
	ctype  *CType
	goType reflect.Type
	// Of a struct: the field of the flat Go struct each of its values is
	// in, the fields in between being the gaps
	fields []int
}

func newCValue(t *CType) (*cValue, error) {
	if t.Kind != CStruct {
		return &cValue{ctype: t, goType: goTypes[t.Kind]}, nil
	}

	c := &cValue{ctype: t}
	var fields []reflect.StructField
	add := func(goType reflect.Type) {
		fields = append(fields, reflect.StructField{Name: fmt.Sprintf("F%d", len(fields)), Type: goType})
	}
	at := uintptr(0)
	for _, leaf := range t.leaves(0, nil) {
		goType := goTypes[leaf.kind]
		at = alignUp(at, uintptr(goType.Align()))
		// A gap Go would not leave by itself: the end of a struct inside
		// this one, rounded up to its own alignment
		for ; at < leaf.offset; at++ {
			add(goTypes[CUInt8])
		}
		if at != leaf.offset {
			return nil, fmt.Errorf("struct %s cannot be laid out", t.Name)
		}
		c.fields = append(c.fields, len(fields))
		add(goType)
		at += goType.Size()
	}
	c.goType = reflect.StructOf(fields)
	if size, _ := t.sizeAndAlign(); c.goType.Size() != size {
		return nil, fmt.Errorf("struct %s cannot be laid out: %d bytes for %d", t.Name, c.goType.Size(), size)
	}
	return c, nil
}

// --- From the program to C ---

// give writes a value of the program where C will read it.
func (c *cValue) give(v Value, into reflect.Value, what string) error {
	if c.ctype.Kind != CStruct {
		return giveScalar(c.ctype.Kind, v, into, what)
	}
	next := 0
	return c.giveStruct(c.ctype, v, into, &next, what)
}

func (c *cValue) giveStruct(t *CType, v Value, into reflect.Value, next *int, what string) error {
	if kind, ok := heapType(v); !ok || kind != ObjTypeObject {
		return fmt.Errorf("%s must be a %s, got %s", what, t.Name, typeName(v))
	}
	obj := (*ObjectObj)(unsafe.Pointer(v.AsHeapPtr()))
	for i := range t.Fields {
		field := &t.Fields[i]
		value, ok := obj.Get(field.Name)
		if !ok {
			return fmt.Errorf("%s must be a %s: it has no field '%s'", what, t.Name, field.Name)
		}
		var err error
		if field.Type.Kind == CStruct {
			err = c.giveStruct(&field.Type, value, into, next, what+"."+field.Name)
		} else {
			err = giveScalar(field.Type.Kind, value, into.Field(c.fields[*next]), what+"."+field.Name)
			*next++
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func giveScalar(kind string, v Value, into reflect.Value, what string) error {
	switch kind {
	case CFloat32, CFloat64:
		switch {
		case v.IsInt():
			into.SetFloat(float64(v.AsInt()))
		case v.IsNumber():
			into.SetFloat(v.AsNumber())
		default:
			return fmt.Errorf("%s must be a Float, got %s", what, typeName(v))
		}
	case CBool:
		if !v.IsBool() {
			return fmt.Errorf("%s must be a Bool, got %s", what, typeName(v))
		}
		into.SetBool(v.AsBool())
	case CString:
		text, ok := stringValue(v)
		if !ok {
			return fmt.Errorf("%s must be a String, got %s", what, typeName(v))
		}
		if strings.IndexByte(text, 0) >= 0 {
			return fmt.Errorf("%s holds the character 0, where a text of C ends", what)
		}
		into.SetString(text)
	default:
		if !v.IsInt() {
			return fmt.Errorf("%s must be an Int, got %s", what, typeName(v))
		}
		n := v.AsInt()
		if low, high := cIntRange(kind); n < low || n > high {
			return fmt.Errorf("%s is %d, which does not fit %s", what, n, cTypeName(&CType{Kind: kind}))
		}
		switch kind {
		case CUInt8, CUInt16, CUInt32, CUInt64, CPointer:
			into.SetUint(uint64(n))
		default:
			into.SetInt(n)
		}
	}
	return nil
}

// --- Structs given in registers ---
//
// purego takes a struct apart by reflection on every call, which costs
// several times the call itself. On arm64 a struct that is an argument
// comes to something simple: up to four floats in a row go each in a
// register of their own, anything else of 16 bytes or less goes as one or
// two words, and what is larger is copied somewhere and its address
// given. So the struct is written once, as C lays it out, in memory kept
// for the parameter, and the function is given plain numbers: as many
// words or floats as the struct comes to, or the address.
//
// This only holds while the registers last: a struct that would not fit
// in those left goes to the stack whole, which plain numbers would not
// do. A function like that is called the slow way, as are all of them on
// other processors and every struct that is returned.

// cParam is one parameter of a function.
type cParam struct {
	ctype *CType
	first int // Its first argument among those the Go function takes

	// For a parameter that is one argument: a number, a text, or a struct
	// purego is left to deal with
	value *cValue

	// For a struct given as numbers
	lowered bool
	leaves  []cLeaf
	memory  []uint64
	floats  int // Given as that many floats, each of the kind of the first leaf
	words   int // Or as that many words; neither: by its address
}

// lowerParams works out how each parameter is given, and the types of the
// arguments of the Go function that makes the call.
func lowerParams(f *Foreign) ([]cParam, []reflect.Type, error) {
	params := make([]cParam, len(f.Params))
	var in []reflect.Type
	lower := runtime.GOARCH == "arm64"

	// The registers used so far: 8 for whole numbers and addresses, 8
	// for floats
	whole, floating := 0, 0
	for i := range f.Params {
		t := &f.Params[i].Type
		p := &params[i]
		p.ctype, p.first = t, len(in)

		if t.Kind != CStruct {
			if t.Kind == CFloat32 || t.Kind == CFloat64 {
				floating++
			} else {
				whole++
			}
			p.value = &cValue{ctype: t, goType: goTypes[t.Kind]}
			in = append(in, p.value.goType)
			continue
		}
		if !lower {
			continue
		}

		p.lowered = true
		p.leaves = t.leaves(0, nil)
		size, _ := t.sizeAndAlign()
		p.memory = make([]uint64, (size+7)/8)
		switch {
		case sameFloats(p.leaves):
			p.floats = len(p.leaves)
			if floating += p.floats; floating > 8 {
				lower = false
			}
			for range p.leaves {
				in = append(in, goTypes[p.leaves[0].kind])
			}
		case size <= 16:
			p.words = len(p.memory)
			if whole += p.words; whole > 8 {
				lower = false
			}
			for range p.memory {
				in = append(in, goTypes[CUInt64])
			}
		default:
			whole++
			in = append(in, goTypes[CPointer])
		}
	}
	if lower {
		return params, in, nil
	}

	// The slow way: each struct is one argument, of a Go type laid out
	// like it
	in = in[:0]
	for i := range f.Params {
		p := &params[i]
		value, err := newCValue(&f.Params[i].Type)
		if err != nil {
			return nil, nil, err
		}
		*p = cParam{ctype: p.ctype, first: i, value: value}
		in = append(in, value.goType)
	}
	return params, in, nil
}

// sameFloats reports whether a struct is one to four floats of one kind.
func sameFloats(leaves []cLeaf) bool {
	if len(leaves) > 4 || (leaves[0].kind != CFloat32 && leaves[0].kind != CFloat64) {
		return false
	}
	for _, leaf := range leaves {
		if leaf.kind != leaves[0].kind {
			return false
		}
	}
	return true
}

// give writes an argument of the program where the call will take it.
func (p *cParam) give(v Value, args []reflect.Value, what string) error {
	if !p.lowered {
		return p.value.give(v, args[p.first], what)
	}
	memory := unsafe.Pointer(&p.memory[0])
	next := 0
	if err := storeStruct(p.ctype, v, p.leaves, &next, memory, what); err != nil {
		return err
	}
	for i := 0; i < p.floats; i++ {
		at := unsafe.Add(memory, p.leaves[i].offset)
		if p.leaves[i].kind == CFloat32 {
			args[p.first+i].SetFloat(float64(*(*float32)(at)))
		} else {
			args[p.first+i].SetFloat(*(*float64)(at))
		}
	}
	for i := 0; i < p.words; i++ {
		args[p.first+i].SetUint(p.memory[i])
	}
	return nil
}

// storeStruct writes a struct of the program in memory, as C lays it out.
func storeStruct(t *CType, v Value, leaves []cLeaf, next *int, memory unsafe.Pointer, what string) error {
	if kind, ok := heapType(v); !ok || kind != ObjTypeObject {
		return fmt.Errorf("%s must be a %s, got %s", what, t.Name, typeName(v))
	}
	obj := (*ObjectObj)(unsafe.Pointer(v.AsHeapPtr()))
	for i := range t.Fields {
		field := &t.Fields[i]
		value, ok := obj.Get(field.Name)
		if !ok {
			return fmt.Errorf("%s must be a %s: it has no field '%s'", what, t.Name, field.Name)
		}
		if field.Type.Kind == CStruct {
			if err := storeStruct(&field.Type, value, leaves, next, memory, what+"."+field.Name); err != nil {
				return err
			}
			continue
		}
		leaf := leaves[*next]
		*next++
		if err := storeScalar(leaf.kind, value, unsafe.Add(memory, leaf.offset)); err != nil {
			return fmt.Errorf("%s.%s %v", what, field.Name, err)
		}
	}
	return nil
}

// storeScalar writes a number in memory. Its error says what is wrong
// with the value, for the caller to say which value it is.
func storeScalar(kind string, v Value, at unsafe.Pointer) error {
	switch kind {
	case CFloat32, CFloat64:
		var f float64
		switch {
		case v.IsInt():
			f = float64(v.AsInt())
		case v.IsNumber():
			f = v.AsNumber()
		default:
			return fmt.Errorf("must be a Float, got %s", typeName(v))
		}
		if kind == CFloat32 {
			*(*float32)(at) = float32(f)
		} else {
			*(*float64)(at) = f
		}
		return nil
	case CBool:
		if !v.IsBool() {
			return fmt.Errorf("must be a Bool, got %s", typeName(v))
		}
		*(*bool)(at) = v.AsBool()
		return nil
	}
	if !v.IsInt() {
		return fmt.Errorf("must be an Int, got %s", typeName(v))
	}
	n := v.AsInt()
	if low, high := cIntRange(kind); n < low || n > high {
		return fmt.Errorf("is %d, which does not fit %s", n, cTypeName(&CType{Kind: kind}))
	}
	switch kind {
	case CInt8, CUInt8:
		*(*uint8)(at) = uint8(n)
	case CInt16, CUInt16:
		*(*uint16)(at) = uint16(n)
	case CInt32, CUInt32:
		*(*uint32)(at) = uint32(n)
	default:
		*(*uint64)(at) = uint64(n)
	}
	return nil
}

// --- From C to the program ---

// take makes a value of the program of what C returned.
func (c *cValue) take(vm *VM, from reflect.Value) (Value, error) {
	if c.ctype.Kind != CStruct {
		return takeScalar(vm, c.ctype.Kind, from)
	}
	next := 0
	return c.takeStruct(vm, c.ctype, from, &next)
}

func (c *cValue) takeStruct(vm *VM, t *CType, from reflect.Value, next *int) (Value, error) {
	obj, _, err := vm.heap.AllocObjectOf(vm.heap.RootShape(t.Name, false), len(t.Fields))
	if err != nil {
		return NewNull(), err
	}
	// Kept until the function returns: the structs and texts inside it are
	// made after it
	result := vm.pin(valueOf(&obj.header))
	for i := range t.Fields {
		field := &t.Fields[i]
		var value Value
		if field.Type.Kind == CStruct {
			value, err = c.takeStruct(vm, &field.Type, from, next)
		} else {
			value, err = takeScalar(vm, field.Type.Kind, from.Field(c.fields[*next]))
			*next++
		}
		if err != nil {
			return NewNull(), fmt.Errorf("field %s of the %s returned: %v", field.Name, t.Name, err)
		}
		if err := vm.setField(obj, field.Name, value); err != nil {
			return NewNull(), err
		}
	}
	return result, nil
}

func takeScalar(vm *VM, kind string, from reflect.Value) (Value, error) {
	switch kind {
	case CFloat32, CFloat64:
		return NewNumber(from.Float()), nil
	case CBool:
		return NewBool(from.Bool()), nil
	case CString:
		return vm.newString(from.String())
	case CUInt8, CUInt16, CUInt32, CUInt64, CPointer:
		n := from.Uint()
		if n > uint64(MaxInt) {
			return NewNull(), fmt.Errorf("%d does not fit an Int", n)
		}
		return NewInt(int64(n)), nil
	}
	n := from.Int()
	if n < MinInt || n > MaxInt {
		return NewNull(), fmt.Errorf("%d does not fit an Int", n)
	}
	return NewInt(n), nil
}

// --- Functions ---

// foreignFunction finds a function of a C library and makes of it a
// function the VM can call.
func (vm *VM) foreignFunction(f *Foreign) (fn ExternFunc, err error) {
	handle, err := openLibrary(f.Library)
	if err != nil {
		return nil, err
	}
	address, err := purego.Dlsym(handle, f.Symbol)
	if err != nil || address == 0 {
		return nil, fmt.Errorf("the library \"%s\" has no function '%s'", f.Library, f.Symbol)
	}

	params, in, err := lowerParams(f)
	if err != nil {
		return nil, err
	}
	var result *cValue
	var out []reflect.Type
	if f.Result.Kind != CVoid {
		if result, err = newCValue(&f.Result); err != nil {
			return nil, err
		}
		out = []reflect.Type{result.goType}
	}

	// What purego cannot call, it says by panicking
	defer func() {
		if r := recover(); r != nil {
			fn, err = nil, fmt.Errorf("it cannot be called on this system: %v", r)
		}
	}()
	target := reflect.New(reflect.FuncOf(in, out, false))
	purego.RegisterFunc(target.Interface(), address)
	call := target.Elem()

	// The arguments of a call are written in the same places every time:
	// a function of C does not call back into the program, so two calls
	// of it are never in progress at once
	args := make([]reflect.Value, len(in))
	for i := range args {
		args[i] = reflect.New(in[i]).Elem()
	}
	what := make([]string, len(params))
	for i := range what {
		what[i] = "'" + f.Params[i].Name + "'"
		if f.Params[i].Name == "" {
			what[i] = fmt.Sprintf("argument %d", i+1)
		}
		// A struct given by its address is always at the same one
		if p := &params[i]; p.lowered && p.floats == 0 && p.words == 0 {
			args[p.first].SetUint(uint64(uintptr(unsafe.Pointer(&p.memory[0]))))
		}
	}

	return func(vm *VM, values []Value) (Value, error) {
		if len(values) != len(params) {
			return NewNull(), vm.foreignError(f, fmt.Sprintf("expects %d arguments, got %d", len(params), len(values)))
		}
		if !vm.onMainTask() {
			// A window, and much else, belongs to the thread that made
			// it; only main is always on the same one
			return NewNull(), vm.foreignError(f, "a function of a C library can only be called from main, not from a task")
		}
		for i, value := range values {
			if err := params[i].give(value, args, what[i]); err != nil {
				return NewNull(), vm.foreignError(f, err.Error())
			}
		}
		returned := call.Call(args)
		if result == nil {
			return NewNull(), nil
		}
		value, err := result.take(vm, returned[0])
		if err != nil {
			return NewNull(), vm.foreignError(f, err.Error())
		}
		return value, nil
	}, nil
}

// --- Memory of C ---

var cMemory struct {
	once   sync.Once
	err    error
	calloc func(count, size uintptr) uintptr
	free   func(address uintptr)
	strlen func(address uintptr) uintptr
}

func cLibrary() error {
	cMemory.once.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				cMemory.err = fmt.Errorf("the C library cannot be used: %v", r)
			}
		}()
		handle, err := openLibrary("c")
		if err != nil {
			cMemory.err = err
			return
		}
		purego.RegisterLibFunc(&cMemory.calloc, handle, "calloc")
		purego.RegisterLibFunc(&cMemory.free, handle, "free")
		purego.RegisterLibFunc(&cMemory.strlen, handle, "strlen")
	})
	return cMemory.err
}

// cAlloc returns the address of a number of bytes, all 0, that stay where
// they are until cFree.
func cAlloc(bytes uintptr) (uintptr, error) {
	if err := cLibrary(); err != nil {
		return 0, err
	}
	if bytes == 0 {
		bytes = 1
	}
	address := cMemory.calloc(1, bytes)
	if address == 0 {
		return 0, fmt.Errorf("out of memory: no %d bytes left outside the program", bytes)
	}
	return address, nil
}

func cFree(address uintptr) error {
	if err := cLibrary(); err != nil {
		return err
	}
	cMemory.free(address)
	return nil
}

func cLength(address uintptr) (uintptr, error) {
	if err := cLibrary(); err != nil {
		return 0, err
	}
	return cMemory.strlen(address), nil
}

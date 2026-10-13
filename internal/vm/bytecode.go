package vm

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	// Magic number "LUMN" in ASCII
	bytecodeFileMagic uint32 = 0x4C554D4E
	// Current bytecode format version
	// 2: value tags moved to bits 48-51; prototypes carry a signature
	// 3: null has its own tag, so float infinity is no longer read as null
	// 4: instruction operands are 16 bits wide
	bytecodeVersion uint32 = 4

	// Bytes per instruction in a file: opcode, then three 16-bit operands
	instrSize = 1 + 3*2
)

// SerializeChunk converts a Chunk to binary bytecode format.
// Format: [Magic:4][Version:4][ConstCount:4][Constants...][CodeCount:4][Code...][ProtoCount:4][Prototypes...][ExternCount:4][ExternNames...][StringPoolCount:4][StringPool...][DebugInfoCount:4][DebugInfo...]
func SerializeChunk(chunk *Chunk) ([]byte, error) {
	if chunk == nil {
		return nil, fmt.Errorf("cannot serialize nil chunk")
	}

	// Calculate total size
	headerSize := 4 + 4 // magic + version
	constSize := 4 + (len(chunk.Constants) * 8)
	codeSize := 4 + (len(chunk.Code) * instrSize)

	// Calculate prototypes size
	protoSize := 4 // count
	for _, proto := range chunk.Prototypes {
		// Each prototype: NameLen + Name + CodeStart + CodeSize + RegisterCount + ArgCount + UpvalueCount
		// + SignatureLen + Signature
		protoSize += 4 + len(proto.Name) + 4 + 4 + 4 + 4 + 4 + 4 + len(proto.Signature)
	}

	// Calculate extern names size
	externSize := 4 // count
	for _, name := range chunk.ExternNames {
		externSize += 4 + len(name) // length + string data
	}

	// Calculate string pool size
	stringPoolSize := 4 // count
	for _, str := range chunk.StringPool {
		stringPoolSize += 4 + len(str) // length + string data
	}

	// Calculate debug info size
	debugSize := 4 // count
	for _, debug := range chunk.DebugInfo {
		if debug != nil {
			// Each debug info: 4 (instr index) + 4 (file path len) + file path + 4 (line) + 4 (column) +
			// 4 (source line len) + source line + 4 (func name len) + func name +
			// 4 (description len) + description + 4 (notes len) + notes
			debugSize += 4 + 4 + len(debug.FilePath) + 4 + 4 +
				4 + len(debug.SourceLine) + 4 + len(debug.FuncName) +
				4 + len(debug.Description) + 4 + len(debug.Notes)
		}
	}

	totalSize := headerSize + constSize + codeSize + protoSize + externSize + stringPoolSize + debugSize

	buf := make([]byte, totalSize)
	offset := 0

	// Write magic number
	binary.LittleEndian.PutUint32(buf[offset:], bytecodeFileMagic)
	offset += 4

	// Write version
	binary.LittleEndian.PutUint32(buf[offset:], bytecodeVersion)
	offset += 4

	// Write constant pool
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(chunk.Constants)))
	offset += 4
	for _, val := range chunk.Constants {
		binary.LittleEndian.PutUint64(buf[offset:], uint64(val))
		offset += 8
	}

	// Write code
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(chunk.Code)))
	offset += 4
	for _, instr := range chunk.Code {
		buf[offset] = byte(instr.Op)
		binary.LittleEndian.PutUint16(buf[offset+1:], instr.A)
		binary.LittleEndian.PutUint16(buf[offset+3:], instr.B)
		binary.LittleEndian.PutUint16(buf[offset+5:], instr.C)
		offset += instrSize
	}

	// Write function prototypes
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(chunk.Prototypes)))
	offset += 4
	for _, proto := range chunk.Prototypes {
		// Write function name
		binary.LittleEndian.PutUint32(buf[offset:], uint32(len(proto.Name)))
		offset += 4
		copy(buf[offset:], []byte(proto.Name))
		offset += len(proto.Name)

		// Write metadata
		binary.LittleEndian.PutUint32(buf[offset:], uint32(proto.CodeStart))
		offset += 4
		binary.LittleEndian.PutUint32(buf[offset:], uint32(proto.CodeSize))
		offset += 4
		binary.LittleEndian.PutUint32(buf[offset:], uint32(proto.RegisterCount))
		offset += 4
		binary.LittleEndian.PutUint32(buf[offset:], uint32(proto.ArgCount))
		offset += 4
		binary.LittleEndian.PutUint32(buf[offset:], uint32(proto.UpvalueCount))
		offset += 4

		// Write signature
		binary.LittleEndian.PutUint32(buf[offset:], uint32(len(proto.Signature)))
		offset += 4
		copy(buf[offset:], []byte(proto.Signature))
		offset += len(proto.Signature)
	}

	// Write extern names
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(chunk.ExternNames)))
	offset += 4
	for _, name := range chunk.ExternNames {
		binary.LittleEndian.PutUint32(buf[offset:], uint32(len(name)))
		offset += 4
		copy(buf[offset:], []byte(name))
		offset += len(name)
	}

	// Write string pool
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(chunk.StringPool)))
	offset += 4
	for _, str := range chunk.StringPool {
		binary.LittleEndian.PutUint32(buf[offset:], uint32(len(str)))
		offset += 4
		copy(buf[offset:], []byte(str))
		offset += len(str)
	}

	// Write debug info
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(chunk.DebugInfo)))
	offset += 4
	for instrIdx, debug := range chunk.DebugInfo {
		if debug != nil {
			// Write instruction index
			binary.LittleEndian.PutUint32(buf[offset:], uint32(instrIdx))
			offset += 4

			// Write file path
			binary.LittleEndian.PutUint32(buf[offset:], uint32(len(debug.FilePath)))
			offset += 4
			copy(buf[offset:], []byte(debug.FilePath))
			offset += len(debug.FilePath)

			// Write line and column
			binary.LittleEndian.PutUint32(buf[offset:], uint32(debug.Line))
			offset += 4
			binary.LittleEndian.PutUint32(buf[offset:], uint32(debug.Column))
			offset += 4

			// Write source line
			binary.LittleEndian.PutUint32(buf[offset:], uint32(len(debug.SourceLine)))
			offset += 4
			copy(buf[offset:], []byte(debug.SourceLine))
			offset += len(debug.SourceLine)

			// Write function name
			binary.LittleEndian.PutUint32(buf[offset:], uint32(len(debug.FuncName)))
			offset += 4
			copy(buf[offset:], []byte(debug.FuncName))
			offset += len(debug.FuncName)

			// Write description
			binary.LittleEndian.PutUint32(buf[offset:], uint32(len(debug.Description)))
			offset += 4
			copy(buf[offset:], []byte(debug.Description))
			offset += len(debug.Description)

			// Write notes
			binary.LittleEndian.PutUint32(buf[offset:], uint32(len(debug.Notes)))
			offset += 4
			copy(buf[offset:], []byte(debug.Notes))
			offset += len(debug.Notes)
		}
	}

	return buf, nil
}

// byteReader reads the parts of a bytecode file in order. Nothing in a
// file is trusted: every length is checked against what is left of the
// file before anything is read or made to hold it, so that a damaged file
// is refused with a message instead of asking for memory it names but
// does not have.
type byteReader struct {
	data   []byte
	offset int
}

func (r *byteReader) left() int {
	return len(r.data) - r.offset
}

func (r *byteReader) uint32(what string) (uint32, error) {
	if r.left() < 4 {
		return 0, fmt.Errorf("bytecode truncated: cannot read %s", what)
	}
	v := binary.LittleEndian.Uint32(r.data[r.offset:])
	r.offset += 4
	return v, nil
}

// count reads a number of items that each take at least size bytes.
func (r *byteReader) count(what string, size int) (int, error) {
	n, err := r.uint32(what + " count")
	if err != nil {
		return 0, err
	}
	if int64(n)*int64(size) > int64(r.left()) {
		return 0, fmt.Errorf("bytecode truncated: cannot read %d %s", n, what)
	}
	return int(n), nil
}

func (r *byteReader) string(what string) (string, error) {
	n, err := r.uint32(what + " length")
	if err != nil {
		return "", err
	}
	if int64(n) > int64(r.left()) {
		return "", fmt.Errorf("bytecode truncated: cannot read %s", what)
	}
	s := string(r.data[r.offset : r.offset+int(n)])
	r.offset += int(n)
	return s, nil
}

// DeserializeChunk converts binary bytecode to a Chunk.
//
// It checks that the file is whole and well formed, not that the program
// in it makes sense: that is Verify, which Exec applies to every chunk.
func DeserializeChunk(data []byte) (*Chunk, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("bytecode too short: minimum 8 bytes required")
	}
	r := &byteReader{data: data}

	if magic, _ := r.uint32("magic number"); magic != bytecodeFileMagic {
		return nil, fmt.Errorf("invalid magic number: expected 0x%X, got 0x%X", bytecodeFileMagic, magic)
	}
	if version, _ := r.uint32("version"); version != bytecodeVersion {
		return nil, fmt.Errorf("unsupported bytecode version: expected %d, got %d", bytecodeVersion, version)
	}

	chunk := NewChunk()

	// Constant pool
	constCount, err := r.count("constants", 8)
	if err != nil {
		return nil, err
	}
	chunk.Constants = make([]Value, constCount)
	for i := range chunk.Constants {
		constant := Value(binary.LittleEndian.Uint64(r.data[r.offset:]))
		r.offset += 8
		if constant.IsHeapPtr() {
			// An address means something in the program that wrote it,
			// and nothing here
			return nil, fmt.Errorf("invalid bytecode: constant %d is a pointer", i)
		}
		chunk.Constants[i] = constant
	}

	// Code
	codeCount, err := r.count("instructions", instrSize)
	if err != nil {
		return nil, err
	}
	chunk.Code = make([]Instr, codeCount)
	for i := range chunk.Code {
		at := r.data[r.offset:]
		chunk.Code[i] = Instr{
			Op: OpCode(at[0]),
			A:  binary.LittleEndian.Uint16(at[1:]),
			B:  binary.LittleEndian.Uint16(at[3:]),
			C:  binary.LittleEndian.Uint16(at[5:]),
		}
		r.offset += instrSize
	}

	// Function prototypes: a name, five numbers and a signature each
	protoCount, err := r.count("prototypes", 4+20+4)
	if err != nil {
		return nil, err
	}
	chunk.Prototypes = make([]FunctionPrototype, protoCount)
	for i := range chunk.Prototypes {
		proto := &chunk.Prototypes[i]
		if proto.Name, err = r.string("function name"); err != nil {
			return nil, err
		}
		if r.left() < 20 {
			return nil, fmt.Errorf("bytecode truncated: cannot read function metadata")
		}
		for _, field := range []*int{&proto.CodeStart, &proto.CodeSize, &proto.RegisterCount, &proto.ArgCount, &proto.UpvalueCount} {
			n, _ := r.uint32("function metadata")
			if n > math.MaxInt32 {
				return nil, fmt.Errorf("invalid bytecode: function '%s' has a size of %d", proto.Name, n)
			}
			*field = int(n)
		}
		if proto.Signature, err = r.string("function signature"); err != nil {
			return nil, err
		}
	}

	// Extern names
	externCount, err := r.count("extern names", 4)
	if err != nil {
		return nil, err
	}
	chunk.ExternNames = make([]string, externCount)
	for i := range chunk.ExternNames {
		if chunk.ExternNames[i], err = r.string("extern name"); err != nil {
			return nil, err
		}
	}

	// String pool
	stringCount, err := r.count("strings", 4)
	if err != nil {
		return nil, err
	}
	chunk.StringPool = make([]string, stringCount)
	for i := range chunk.StringPool {
		if chunk.StringPool[i], err = r.string("string"); err != nil {
			return nil, err
		}
	}

	// Debug info: an instruction, a file, a line, a column, four texts
	debugCount, err := r.count("debug info entries", 4+4+8+4*4)
	if err != nil {
		return nil, err
	}
	for i := 0; i < debugCount; i++ {
		debug := &DebugInfo{}
		instrIdx, err := r.uint32("debug info instruction index")
		if err != nil {
			return nil, err
		}
		if debug.FilePath, err = r.string("file path"); err != nil {
			return nil, err
		}
		line, err := r.uint32("line")
		if err != nil {
			return nil, err
		}
		column, err := r.uint32("column")
		if err != nil {
			return nil, err
		}
		debug.Line, debug.Column = int(line), int(column)
		if debug.SourceLine, err = r.string("source line"); err != nil {
			return nil, err
		}
		if debug.FuncName, err = r.string("function name"); err != nil {
			return nil, err
		}
		if debug.Description, err = r.string("description"); err != nil {
			return nil, err
		}
		if debug.Notes, err = r.string("notes"); err != nil {
			return nil, err
		}
		if int(instrIdx) >= len(chunk.Code) {
			return nil, fmt.Errorf("invalid bytecode: debug info for instruction %d of %d", instrIdx, len(chunk.Code))
		}
		chunk.DebugInfo[int(instrIdx)] = debug
	}

	return chunk, nil
}

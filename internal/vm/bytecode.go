package vm

import (
	"encoding/binary"
	"fmt"
)

const (
	// Magic number "LUMN" in ASCII
	bytecodeFileMagic uint32 = 0x4C554D4E
	// Current bytecode format version
	// 2: value tags moved to bits 48-51; prototypes carry a signature
	bytecodeVersion uint32 = 2
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
	codeSize := 4 + (len(chunk.Code) * 4)

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
		buf[offset+1] = instr.A
		buf[offset+2] = instr.B
		buf[offset+3] = instr.C
		offset += 4
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

// DeserializeChunk converts binary bytecode to a Chunk.
func DeserializeChunk(data []byte) (*Chunk, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("bytecode too short: minimum 8 bytes required")
	}

	offset := 0

	// Read and verify magic number
	magic := binary.LittleEndian.Uint32(data[offset:])
	offset += 4
	if magic != bytecodeFileMagic {
		return nil, fmt.Errorf("invalid magic number: expected 0x%X, got 0x%X", bytecodeFileMagic, magic)
	}

	// Read and verify version
	version := binary.LittleEndian.Uint32(data[offset:])
	offset += 4
	if version != bytecodeVersion {
		return nil, fmt.Errorf("unsupported bytecode version: expected %d, got %d", bytecodeVersion, version)
	}

	chunk := NewChunk()

	// Read constant pool
	if offset+4 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read constant count")
	}
	constCount := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	if offset+int(constCount)*8 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read %d constants", constCount)
	}
	chunk.Constants = make([]Value, constCount)
	for i := uint32(0); i < constCount; i++ {
		val := binary.LittleEndian.Uint64(data[offset:])
		chunk.Constants[i] = Value(val)
		offset += 8
	}

	// Read code
	if offset+4 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read code count")
	}
	codeCount := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	if offset+int(codeCount)*4 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read %d instructions", codeCount)
	}
	chunk.Code = make([]Instr, codeCount)
	for i := uint32(0); i < codeCount; i++ {
		chunk.Code[i] = Instr{
			Op: OpCode(data[offset]),
			A:  data[offset+1],
			B:  data[offset+2],
			C:  data[offset+3],
		}
		offset += 4
	}

	// Read function prototypes
	if offset+4 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read prototype count")
	}
	protoCount := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	chunk.Prototypes = make([]FunctionPrototype, protoCount)
	for i := uint32(0); i < protoCount; i++ {
		// Read function name
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read function name length")
		}
		nameLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4

		if offset+int(nameLen) > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read function name")
		}
		name := string(data[offset : offset+int(nameLen)])
		offset += int(nameLen)

		// Read metadata
		if offset+20 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read function metadata")
		}
		codeStart := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		codeSize := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		registerCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		argCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		upvalueCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		// Read signature
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read function signature length")
		}
		signatureLen := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		if signatureLen < 0 || offset+signatureLen > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read function signature")
		}
		signature := string(data[offset : offset+signatureLen])
		offset += signatureLen

		chunk.Prototypes[i] = FunctionPrototype{
			Name:          name,
			CodeStart:     codeStart,
			CodeSize:      codeSize,
			RegisterCount: registerCount,
			ArgCount:      argCount,
			UpvalueCount:  upvalueCount,
			Signature:     signature,
		}
	}

	// Read extern names
	if offset+4 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read extern count")
	}
	externCount := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	chunk.ExternNames = make([]string, externCount)
	for i := uint32(0); i < externCount; i++ {
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read extern name length")
		}
		nameLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4

		if offset+int(nameLen) > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read extern name")
		}
		chunk.ExternNames[i] = string(data[offset : offset+int(nameLen)])
		offset += int(nameLen)
	}

	// Read string pool
	if offset+4 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read string pool count")
	}
	stringPoolCount := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	chunk.StringPool = make([]string, stringPoolCount)
	for i := uint32(0); i < stringPoolCount; i++ {
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read string length")
		}
		strLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4

		if offset+int(strLen) > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read string")
		}
		chunk.StringPool[i] = string(data[offset : offset+int(strLen)])
		offset += int(strLen)
	}

	// Read debug info
	if offset+4 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read debug info count")
	}
	debugCount := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	for i := uint32(0); i < debugCount; i++ {
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read debug info instruction index")
		}
		instrIdx := binary.LittleEndian.Uint32(data[offset:])
		offset += 4

		// Read file path
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read file path length")
		}
		filePathLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		if offset+int(filePathLen) > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read file path")
		}
		filePath := string(data[offset : offset+int(filePathLen)])
		offset += int(filePathLen)

		// Read line and column
		if offset+8 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read line/column")
		}
		line := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		column := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		// Read source line
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read source line length")
		}
		sourceLineLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		if offset+int(sourceLineLen) > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read source line")
		}
		sourceLine := string(data[offset : offset+int(sourceLineLen)])
		offset += int(sourceLineLen)

		// Read function name
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read function name length")
		}
		funcNameLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		if offset+int(funcNameLen) > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read function name")
		}
		funcName := string(data[offset : offset+int(funcNameLen)])
		offset += int(funcNameLen)

		// Read description
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read description length")
		}
		descLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		if offset+int(descLen) > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read description")
		}
		description := string(data[offset : offset+int(descLen)])
		offset += int(descLen)

		// Read notes
		if offset+4 > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read notes length")
		}
		notesLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		if offset+int(notesLen) > len(data) {
			return nil, fmt.Errorf("bytecode truncated: cannot read notes")
		}
		notes := string(data[offset : offset+int(notesLen)])
		offset += int(notesLen)

		// Create debug info
		debug := &DebugInfo{
			FilePath:    filePath,
			Line:        line,
			Column:      column,
			SourceLine:  sourceLine,
			FuncName:    funcName,
			Description: description,
			Notes:       notes,
		}
		chunk.DebugInfo[int(instrIdx)] = debug
	}

	return chunk, nil
}

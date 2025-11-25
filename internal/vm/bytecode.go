package vm

import (
	"encoding/binary"
	"fmt"
)

const (
	// Magic number "LUMN" in ASCII
	bytecodeFileMagic uint32 = 0x4C554D4E
	// Current bytecode format version
	bytecodeVersion uint32 = 1
)

// SerializeChunk converts a Chunk to binary bytecode format.
// Format: [Magic:4][Version:4][ConstCount:4][Constants...][CodeCount:4][Code...][RegCount:4][ArgCount:4][ExternCount:4][ExternNames...]
func SerializeChunk(chunk *Chunk) ([]byte, error) {
	if chunk == nil {
		return nil, fmt.Errorf("cannot serialize nil chunk")
	}

	// Calculate total size
	headerSize := 4 + 4 // magic + version
	constSize := 4 + (len(chunk.Constants) * 8)
	codeSize := 4 + (len(chunk.Code) * 4)
	metaSize := 4 + 4 // registerCount + argCount

	// Calculate extern names size
	externSize := 4 // count
	for _, name := range chunk.ExternNames {
		externSize += 4 + len(name) // length + string data
	}

	totalSize := headerSize + constSize + codeSize + metaSize + externSize

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

	// Write metadata
	binary.LittleEndian.PutUint32(buf[offset:], uint32(chunk.RegisterCount))
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], uint32(chunk.ArgCount))
	offset += 4

	// Write extern names
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(chunk.ExternNames)))
	offset += 4
	for _, name := range chunk.ExternNames {
		binary.LittleEndian.PutUint32(buf[offset:], uint32(len(name)))
		offset += 4
		copy(buf[offset:], []byte(name))
		offset += len(name)
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

	// Read metadata
	if offset+8 > len(data) {
		return nil, fmt.Errorf("bytecode truncated: cannot read metadata")
	}
	chunk.RegisterCount = int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	chunk.ArgCount = int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	// Read extern names (if present in this version)
	if offset+4 <= len(data) {
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
	}

	return chunk, nil
}

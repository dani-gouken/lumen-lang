package asm

import (
	"testing"
	"unsafe"

	"github.com/yourname/lumen/internal/vm"
)

// TestStringIntegrationEndToEnd tests the complete flow of string literals:
// assembly -> bytecode serialization -> deserialization -> execution
func TestStringIntegrationEndToEnd(t *testing.T) {
	source := `.constants 1
  0: "Hello, Integration Test!"

.code
  NEWSTR r0 0
  RET r0

.metadata
  registers: 1
  args: 0
`

	// Step 1: Assemble
	chunk1, err := Assemble(source)
	if err != nil {
		t.Fatalf("Assembly failed: %v", err)
	}

	// Verify string pool
	if len(chunk1.StringPool) != 1 {
		t.Fatalf("Expected 1 string in pool, got %d", len(chunk1.StringPool))
	}
	if chunk1.StringPool[0] != "Hello, Integration Test!" {
		t.Errorf("Unexpected string: %q", chunk1.StringPool[0])
	}

	// Step 2: Serialize
	bytecode, err := vm.SerializeChunk(chunk1)
	if err != nil {
		t.Fatalf("Serialization failed: %v", err)
	}

	// Step 3: Deserialize
	chunk2, err := vm.DeserializeChunk(bytecode)
	if err != nil {
		t.Fatalf("Deserialization failed: %v", err)
	}

	// Verify deserialized string pool
	if len(chunk2.StringPool) != 1 {
		t.Fatalf("Expected 1 string in deserialized pool, got %d", len(chunk2.StringPool))
	}
	if chunk2.StringPool[0] != "Hello, Integration Test!" {
		t.Errorf("Deserialized string mismatch: %q", chunk2.StringPool[0])
	}

	// Step 4: Execute
	v := vm.NewVM()
	result, err := v.Exec(chunk2)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	// Verify result is a heap pointer
	if !result.IsHeapPtr() {
		t.Fatalf("Expected heap pointer, got %s", result.String())
	}

	// Verify string content
	ptr := result.AsHeapPtr()
	strObj := (*vm.StringObj)(unsafe.Pointer(ptr))
	if strObj.String() != "Hello, Integration Test!" {
		t.Errorf("String content mismatch: %q", strObj.String())
	}
}

// TestStringIntegrationEscapeSequences tests escape sequences through the full pipeline
func TestStringIntegrationEscapeSequences(t *testing.T) {
	source := `.constants 3
  0: "Line1\nLine2"
  1: "Tab\there"
  2: "Quote: \"test\""

.code
  NEWSTR r0 0
  NEWSTR r1 1
  NEWSTR r2 2
  RET r0

.metadata
  registers: 3
  args: 0
`

	// Assemble
	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("Assembly failed: %v", err)
	}

	// Check string pool
	expectedStrings := []string{
		"Line1\nLine2",
		"Tab\there",
		"Quote: \"test\"",
	}

	for i, expected := range expectedStrings {
		if chunk.StringPool[i] != expected {
			t.Errorf("String %d: expected %q, got %q", i, expected, chunk.StringPool[i])
		}
	}

	// Execute
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	// Verify first string
	ptr := result.AsHeapPtr()
	strObj := (*vm.StringObj)(unsafe.Pointer(ptr))
	if strObj.String() != "Line1\nLine2" {
		t.Errorf("Expected %q, got %q", "Line1\nLine2", strObj.String())
	}
}

// TestStringIntegrationRoundTrip tests disassembly and re-assembly with strings
func TestStringIntegrationRoundTrip(t *testing.T) {
	source := `.constants 2
  0: "First String"
  1: "Second String with\nnewline"

.code
  NEWSTR r0 0
  NEWSTR r1 1
  RET r0

.metadata
  registers: 2
  args: 0
`

	// Assemble
	chunk1, err := Assemble(source)
	if err != nil {
		t.Fatalf("Assembly failed: %v", err)
	}

	// Serialize
	bytecode, err := vm.SerializeChunk(chunk1)
	if err != nil {
		t.Fatalf("Serialization failed: %v", err)
	}

	// Deserialize
	chunk2, err := vm.DeserializeChunk(bytecode)
	if err != nil {
		t.Fatalf("Deserialization failed: %v", err)
	}

	// Disassemble
	disassembled := Disassemble(chunk2)

	// Re-assemble
	chunk3, err := Assemble(disassembled)
	if err != nil {
		t.Fatalf("Re-assembly failed: %v", err)
	}

	// Compare string pools
	if len(chunk1.StringPool) != len(chunk3.StringPool) {
		t.Fatalf("String pool size mismatch: %d vs %d", len(chunk1.StringPool), len(chunk3.StringPool))
	}

	for i := range chunk1.StringPool {
		if chunk1.StringPool[i] != chunk3.StringPool[i] {
			t.Errorf("String %d mismatch: %q vs %q", i, chunk1.StringPool[i], chunk3.StringPool[i])
		}
	}

	// Execute both and compare results
	v1 := vm.NewVM()
	result1, err := v1.Exec(chunk1)
	if err != nil {
		t.Fatalf("Execution 1 failed: %v", err)
	}

	v2 := vm.NewVM()
	result2, err := v2.Exec(chunk3)
	if err != nil {
		t.Fatalf("Execution 2 failed: %v", err)
	}

	// Both should return heap pointers to strings
	if !result1.IsHeapPtr() || !result2.IsHeapPtr() {
		t.Fatal("Expected heap pointers")
	}

	str1 := (*vm.StringObj)(unsafe.Pointer(result1.AsHeapPtr())).String()
	str2 := (*vm.StringObj)(unsafe.Pointer(result2.AsHeapPtr())).String()

	if str1 != str2 {
		t.Errorf("String mismatch: %q vs %q", str1, str2)
	}
}

// TestStringIntegrationMultipleStrings tests multiple string allocations
func TestStringIntegrationMultipleStrings(t *testing.T) {
	source := `.constants 3
  0: "Alpha"
  1: "Beta"
  2: "Gamma"

.code
  NEWSTR r0 0
  NEWSTR r1 1
  NEWSTR r2 2
  RET r2

.metadata
  registers: 3
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("Assembly failed: %v", err)
	}

	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	// Should return "Gamma"
	ptr := result.AsHeapPtr()
	strObj := (*vm.StringObj)(unsafe.Pointer(ptr))
	if strObj.String() != "Gamma" {
		t.Errorf("Expected %q, got %q", "Gamma", strObj.String())
	}
}

// TestStringIntegrationEmptyString tests empty string handling
func TestStringIntegrationEmptyString(t *testing.T) {
	source := `.constants 1
  0: ""

.code
  NEWSTR r0 0
  RET r0

.metadata
  registers: 1
  args: 0
`

	chunk, err := Assemble(source)
	if err != nil {
		t.Fatalf("Assembly failed: %v", err)
	}

	// Check string pool
	if len(chunk.StringPool) != 1 || chunk.StringPool[0] != "" {
		t.Fatalf("Expected empty string in pool, got %q", chunk.StringPool[0])
	}

	// Execute
	v := vm.NewVM()
	result, err := v.Exec(chunk)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	ptr := result.AsHeapPtr()
	strObj := (*vm.StringObj)(unsafe.Pointer(ptr))
	if strObj.String() != "" {
		t.Errorf("Expected empty string, got %q", strObj.String())
	}
}

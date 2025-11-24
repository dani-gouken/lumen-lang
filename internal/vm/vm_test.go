package vm

import "testing"

func TestVMExecuteLoadAndReturn(t *testing.T) {
	chunk := NewChunk()
	chunk.AddConstant(NewInt(42))
	chunk.AddInstruction(NewInstr(OpLOADK, 0, 0, 0))
	chunk.AddInstruction(NewInstr(OpRET, 0, 0, 0))
	chunk.RegisterCount = 1

	v := NewVM()
	result, err := v.Exec(chunk)

	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() {
		t.Fatalf("expected int, got %s", result.String())
	}
	if result.AsInt() != 42 {
		t.Errorf("expected 42, got %d", result.AsInt())
	}
}

func TestVMExecuteAddIntegers(t *testing.T) {
	chunk := NewChunk()
	chunk.AddConstant(NewInt(10))
	chunk.AddConstant(NewInt(20))
	chunk.AddInstruction(NewInstr(OpLOADK, 0, 0, 0))
	chunk.AddInstruction(NewInstr(OpLOADK, 1, 1, 0))
	chunk.AddInstruction(NewInstr(OpADD, 2, 0, 1))
	chunk.AddInstruction(NewInstr(OpRET, 2, 0, 0))
	chunk.RegisterCount = 3

	v := NewVM()
	result, err := v.Exec(chunk)

	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsInt() {
		t.Fatalf("expected int, got %s", result.String())
	}
	if result.AsInt() != 30 {
		t.Errorf("expected 30, got %d", result.AsInt())
	}
}

func TestVMExecuteAddNumbers(t *testing.T) {
	chunk := NewChunk()
	chunk.AddConstant(NewNumber(10.5))
	chunk.AddConstant(NewNumber(20.5))
	chunk.AddInstruction(NewInstr(OpLOADK, 0, 0, 0))
	chunk.AddInstruction(NewInstr(OpLOADK, 1, 1, 0))
	chunk.AddInstruction(NewInstr(OpADD, 2, 0, 1))
	chunk.AddInstruction(NewInstr(OpRET, 2, 0, 0))
	chunk.RegisterCount = 3

	v := NewVM()
	result, err := v.Exec(chunk)

	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsNumber() {
		t.Fatalf("expected number, got %s", result.String())
	}
	if result.AsNumber() != 31.0 {
		t.Errorf("expected 31.0, got %v", result.AsNumber())
	}
}

func TestVMExecuteArithmetic(t *testing.T) {
	tests := []struct {
		name     string
		op       OpCode
		a, b     int64
		expected int64
	}{
		{"sub", OpSUB, 10, 3, 7},
		{"mul", OpMUL, 5, 6, 30},
		{"div", OpDIV, 20, 4, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunk := NewChunk()
			chunk.AddConstant(NewInt(tt.a))
			chunk.AddConstant(NewInt(tt.b))
			chunk.AddInstruction(NewInstr(OpLOADK, 0, 0, 0))
			chunk.AddInstruction(NewInstr(OpLOADK, 1, 1, 0))
			chunk.AddInstruction(NewInstr(tt.op, 2, 0, 1))
			chunk.AddInstruction(NewInstr(OpRET, 2, 0, 0))
			chunk.RegisterCount = 3

			v := NewVM()
			result, err := v.Exec(chunk)

			if err != nil {
				t.Fatalf("execution failed: %v", err)
			}
			if result.AsInt() != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result.AsInt())
			}
		})
	}
}

func TestVMExecuteMOV(t *testing.T) {
	chunk := NewChunk()
	chunk.AddConstant(NewInt(99))
	chunk.AddInstruction(NewInstr(OpLOADK, 0, 0, 0))
	chunk.AddInstruction(NewInstr(OpMOV, 1, 0, 0))
	chunk.AddInstruction(NewInstr(OpRET, 1, 0, 0))
	chunk.RegisterCount = 2

	v := NewVM()
	result, err := v.Exec(chunk)

	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if result.AsInt() != 99 {
		t.Errorf("expected 99, got %d", result.AsInt())
	}
}

func TestVMExecuteLOADNULL(t *testing.T) {
	chunk := NewChunk()
	chunk.AddInstruction(NewInstr(OpLOADNULL, 0, 0, 0))
	chunk.AddInstruction(NewInstr(OpRET, 0, 0, 0))
	chunk.RegisterCount = 1

	v := NewVM()
	result, err := v.Exec(chunk)

	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.IsNull() {
		t.Errorf("expected null, got %s", result.String())
	}
}

func TestVMExecuteLOADBOOL(t *testing.T) {
	tests := []struct {
		value    byte
		expected bool
	}{
		{1, true},
		{0, false},
	}

	for _, tt := range tests {
		chunk := NewChunk()
		chunk.AddInstruction(NewInstr(OpLOADBOOL, 0, tt.value, 0))
		chunk.AddInstruction(NewInstr(OpRET, 0, 0, 0))
		chunk.RegisterCount = 1

		v := NewVM()
		result, err := v.Exec(chunk)

		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}
		if !result.IsBool() {
			t.Fatalf("expected bool, got %s", result.String())
		}
		if result.AsBool() != tt.expected {
			t.Errorf("expected %v, got %v", tt.expected, result.AsBool())
		}
	}
}

func TestVMErrorOnUnimplementedOpcode(t *testing.T) {
	chunk := NewChunk()
	chunk.AddInstruction(NewInstr(OpJMP, 0, 0, 0))
	chunk.RegisterCount = 0

	v := NewVM()
	_, err := v.Exec(chunk)

	if err == nil {
		t.Fatal("expected error for unimplemented opcode")
	}
}

func TestVMErrorOnConstantOutOfBounds(t *testing.T) {
	chunk := NewChunk()
	chunk.AddInstruction(NewInstr(OpLOADK, 0, 99, 0))
	chunk.AddInstruction(NewInstr(OpRET, 0, 0, 0))
	chunk.RegisterCount = 1

	v := NewVM()
	_, err := v.Exec(chunk)

	if err == nil {
		t.Fatal("expected error for out of bounds constant")
	}
}

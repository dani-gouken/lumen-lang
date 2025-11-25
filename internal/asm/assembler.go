package asm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yourname/lumen/internal/vm"
)

// Assemble converts assembly text into a Chunk.
func Assemble(source string) (*vm.Chunk, error) {
	chunk := vm.NewChunk()
	lines := strings.Split(source, "\n")

	var mode string // "constants", "code", "metadata"

	for lineNum, line := range lines {
		// Remove comments
		if idx := strings.Index(line, ";"); idx != -1 {
			line = line[:idx]
		}

		// Trim whitespace
		line = strings.TrimSpace(line)

		// Skip empty lines
		if line == "" {
			continue
		}

		// Check for section directives
		if strings.HasPrefix(line, ".") {
			if strings.HasPrefix(line, ".constants") {
				mode = "constants"
				continue
			} else if line == ".code" {
				mode = "code"
				continue
			} else if line == ".metadata" {
				mode = "metadata"
				continue
			}
		}

		// Process line based on current mode
		switch mode {
		case "constants":
			if err := parseConstant(chunk, line); err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNum+1, err)
			}
		case "code":
			if err := parseInstruction(chunk, line); err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNum+1, err)
			}
		case "metadata":
			if err := parseMetadata(chunk, line); err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNum+1, err)
			}
		default:
			return nil, fmt.Errorf("line %d: no section specified", lineNum+1)
		}
	}

	return chunk, nil
}

func parseConstant(chunk *vm.Chunk, line string) error {
	// Format: "index: value"
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid constant format: %s", line)
	}

	indexStr := strings.TrimSpace(parts[0])
	valueStr := strings.TrimSpace(parts[1])

	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return fmt.Errorf("invalid constant index: %s", indexStr)
	}

	value, err := parseValue(valueStr)
	if err != nil {
		return err
	}

	// Ensure constants array is large enough
	for len(chunk.Constants) <= index {
		chunk.Constants = append(chunk.Constants, vm.NewNull())
	}

	chunk.Constants[index] = value
	return nil
}

func parseValue(s string) (vm.Value, error) {
	s = strings.TrimSpace(s)

	switch {
	case s == "null":
		return vm.NewNull(), nil
	case s == "true":
		return vm.NewBool(true), nil
	case s == "false":
		return vm.NewBool(false), nil
	case strings.HasSuffix(s, "i"):
		// Integer value
		intStr := strings.TrimSuffix(s, "i")
		val, err := strconv.ParseInt(intStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid integer: %s", s)
		}
		return vm.NewInt(val), nil
	default:
		// Try to parse as float
		val, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid value: %s", s)
		}
		return vm.NewNumber(val), nil
	}
}

func parseInstruction(chunk *vm.Chunk, line string) error {
	// Skip line numbers (format: "0001: INSTRUCTION")
	if strings.Contains(line, ":") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			line = strings.TrimSpace(parts[1])
		}
	}

	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}

	mnemonic := strings.ToUpper(fields[0])
	args := fields[1:]

	var instr vm.Instr

	switch mnemonic {
	case "NOP":
		instr = vm.NewInstr(vm.OpNOP, 0, 0, 0)

	case "MOV":
		if len(args) != 2 {
			return fmt.Errorf("MOV requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseReg(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpMOV, a, b, 0)

	case "LOADK":
		if len(args) != 2 {
			return fmt.Errorf("LOADK requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseImm(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpLOADK, a, b, 0)

	case "LOADNULL":
		if len(args) != 1 {
			return fmt.Errorf("LOADNULL requires 1 argument, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpLOADNULL, a, 0, 0)

	case "LOADBOOL":
		if len(args) != 2 {
			return fmt.Errorf("LOADBOOL requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		var b byte
		if args[1] == "true" {
			b = 1
		} else if args[1] == "false" {
			b = 0
		} else {
			return fmt.Errorf("LOADBOOL value must be 'true' or 'false', got %s", args[1])
		}
		instr = vm.NewInstr(vm.OpLOADBOOL, a, b, 0)

	case "ADD", "SUB", "MUL", "DIV", "EQ", "NE", "LT", "LE", "GT", "GE":
		if len(args) != 3 {
			return fmt.Errorf("%s requires 3 arguments, got %d", mnemonic, len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseReg(args[1])
		if err != nil {
			return err
		}
		c, err := parseReg(args[2])
		if err != nil {
			return err
		}

		var op vm.OpCode
		switch mnemonic {
		case "ADD":
			op = vm.OpADD
		case "SUB":
			op = vm.OpSUB
		case "MUL":
			op = vm.OpMUL
		case "DIV":
			op = vm.OpDIV
		case "EQ":
			op = vm.OpEQ
		case "NE":
			op = vm.OpNE
		case "LT":
			op = vm.OpLT
		case "LE":
			op = vm.OpLE
		case "GT":
			op = vm.OpGT
		case "GE":
			op = vm.OpGE
		}
		instr = vm.NewInstr(op, a, b, c)

	case "JMP":
		if len(args) != 1 {
			return fmt.Errorf("JMP requires 1 argument, got %d", len(args))
		}
		a, err := parseImm(args[0])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpJMP, a, 0, 0)

	case "JMPIF", "JMPIFNOT":
		if len(args) != 2 {
			return fmt.Errorf("%s requires 2 arguments, got %d", mnemonic, len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseImm(args[1])
		if err != nil {
			return err
		}

		op := vm.OpJMPIF
		if mnemonic == "JMPIFNOT" {
			op = vm.OpJMPIFNOT
		}
		instr = vm.NewInstr(op, a, b, 0)

	case "CALL":
		if len(args) != 2 {
			return fmt.Errorf("CALL requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseImm(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpCALL, a, b, 0)

	case "RET":
		if len(args) != 1 {
			return fmt.Errorf("RET requires 1 argument, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpRET, a, 0, 0)

	case "NEWSTR":
		if len(args) != 2 {
			return fmt.Errorf("NEWSTR requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseImm(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpNEWSTR, a, b, 0)

	case "NEWARRAY":
		if len(args) != 2 {
			return fmt.Errorf("NEWARRAY requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseImm(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpNEWARRAY, a, b, 0)

	case "NEWOBJ":
		if len(args) != 1 {
			return fmt.Errorf("NEWOBJ requires 1 argument, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpNEWOBJ, a, 0, 0)

	case "GETINDEX":
		if len(args) != 3 {
			return fmt.Errorf("GETINDEX requires 3 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseReg(args[1])
		if err != nil {
			return err
		}
		c, err := parseReg(args[2])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpGETINDEX, a, b, c)

	case "SETINDEX":
		if len(args) != 3 {
			return fmt.Errorf("SETINDEX requires 3 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseReg(args[1])
		if err != nil {
			return err
		}
		c, err := parseReg(args[2])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpSETINDEX, a, b, c)

	case "GETFIELD":
		if len(args) != 3 {
			return fmt.Errorf("GETFIELD requires 3 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseReg(args[1])
		if err != nil {
			return err
		}
		c, err := parseImm(args[2])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpGETFIELD, a, b, c)

	case "SETFIELD":
		if len(args) != 3 {
			return fmt.Errorf("SETFIELD requires 3 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseImm(args[1])
		if err != nil {
			return err
		}
		c, err := parseReg(args[2])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpSETFIELD, a, b, c)

	case "ARRLEN":
		if len(args) != 2 {
			return fmt.Errorf("ARRLEN requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseReg(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpARRLEN, a, b, 0)

	case "GC":
		if len(args) != 0 {
			return fmt.Errorf("GC requires 0 arguments, got %d", len(args))
		}
		instr = vm.NewInstr(vm.OpGC, 0, 0, 0)

	default:
		return fmt.Errorf("unknown instruction: %s", mnemonic)
	}

	chunk.AddInstruction(instr)
	return nil
}

func parseReg(s string) (byte, error) {
	if !strings.HasPrefix(s, "r") {
		return 0, fmt.Errorf("register must start with 'r', got %s", s)
	}

	regNum, err := strconv.Atoi(s[1:])
	if err != nil {
		return 0, fmt.Errorf("invalid register number: %s", s)
	}

	if regNum < 0 || regNum > 255 {
		return 0, fmt.Errorf("register number out of range [0-255]: %d", regNum)
	}

	return byte(regNum), nil
}

func parseImm(s string) (byte, error) {
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid immediate value: %s", s)
	}

	if val < 0 || val > 255 {
		return 0, fmt.Errorf("immediate value out of range [0-255]: %d", val)
	}

	return byte(val), nil
}

func parseMetadata(chunk *vm.Chunk, line string) error {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid metadata format: %s", line)
	}

	key := strings.TrimSpace(parts[0])
	valueStr := strings.TrimSpace(parts[1])

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return fmt.Errorf("invalid metadata value: %s", valueStr)
	}

	switch key {
	case "registers":
		chunk.RegisterCount = value
	case "args":
		chunk.ArgCount = value
	default:
		return fmt.Errorf("unknown metadata key: %s", key)
	}

	return nil
}

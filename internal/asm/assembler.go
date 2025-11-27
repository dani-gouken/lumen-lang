package asm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yourname/lumen/internal/vm"
)

// AssembleFromFile converts assembly text into a Chunk with debug info.
func AssembleFromFile(source, filePath string) (*vm.Chunk, error) {
	return assembleInternal(source, filePath)
}

// Assemble converts assembly text into a Chunk.
func Assemble(source string) (*vm.Chunk, error) {
	return assembleInternal(source, "")
}

func assembleInternal(source, filePath string) (*vm.Chunk, error) {
	chunk := vm.NewChunk()
	lines := strings.Split(source, "\n")

	var mode string                // "constants", "code", "metadata"
	labels := make(map[string]int) // label name -> instruction index
	var currentFunc string         // Track current function name

	// First pass: collect labels
	codeLineNum := 0
	for _, line := range lines {
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
			if line == ".code" {
				mode = "code"
			} else {
				mode = ""
			}
			continue
		}

		// In code section, collect labels
		if mode == "code" {
			// Check if this is a label (ends with :)
			if strings.HasSuffix(line, ":") {
				labelName := strings.TrimSuffix(line, ":")
				labels[labelName] = codeLineNum
			} else {
				// It's an instruction - check if it's a long instruction
				fields := strings.Fields(line)
				if len(fields) > 0 {
					mnemonic := strings.ToUpper(fields[0])
					// Long instructions (with .L suffix) take 3 instruction words
					if strings.HasSuffix(mnemonic, ".L") {
						codeLineNum += 3
					} else {
						codeLineNum++
					}
				}
			}
		}
	}

	// Second pass: assemble code
	mode = ""
	currentFunc = ""
	nextInstrHasBreakpoint := false
	for lineNum, line := range lines {
		originalLine := line // Keep original line for debug info

		// Remove comments but keep them for debug info
		var comment string
		if idx := strings.Index(line, ";"); idx != -1 {
			comment = strings.TrimSpace(line[idx+1:])
			line = line[:idx]
		}

		// Trim whitespace
		line = strings.TrimSpace(line)

		// Check if comment contains BREAK directive
		if comment != "" && (strings.ToUpper(comment) == "BREAK" || strings.HasPrefix(strings.ToUpper(comment), "BREAK ")) {
			nextInstrHasBreakpoint = true
		}

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
				currentFunc = "" // Reset function context when entering code section
				continue
			} else if line == ".metadata" {
				mode = "metadata"
				continue
			}
		}

		// Check for labels in code section (these define functions)
		if mode == "code" && strings.HasSuffix(line, ":") {
			labelName := strings.TrimSuffix(line, ":")
			currentFunc = labelName
			continue
		}

		// Process line based on current mode
		switch mode {
		case "constants":
			if err := parseConstant(chunk, line); err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNum+1, err)
			}
		case "code":
			// Create debug info for this instruction
			debugInfo := vm.NewDebugInfo(filePath, lineNum+1, 1)
			debugInfo.WithSourceLine(strings.TrimSpace(originalLine))
			debugInfo.WithFuncName(currentFunc)
			if comment != "" {
				debugInfo.WithDescription(comment)
			}

			// Record current instruction index before adding
			currentIP := len(chunk.Code)

			if err := parseInstructionWithDebug(chunk, line, labels, debugInfo); err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNum+1, err)
			}

			// If this instruction should have a breakpoint, mark it
			if nextInstrHasBreakpoint {
				chunk.AddBreakpoint(currentIP)
				nextInstrHasBreakpoint = false
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

	value, err := parseValueWithChunk(valueStr, chunk)
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
	return parseValueWithChunk(s, nil)
}

func parseValueWithChunk(s string, chunk *vm.Chunk) (vm.Value, error) {
	s = strings.TrimSpace(s)

	switch {
	case s == "null":
		return vm.NewNull(), nil
	case s == "true":
		return vm.NewBool(true), nil
	case s == "false":
		return vm.NewBool(false), nil
	case strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\""):
		// String literal
		if chunk == nil {
			return 0, fmt.Errorf("string literals require chunk context")
		}
		// Remove quotes and handle escape sequences
		strValue := s[1 : len(s)-1]
		strValue = unescapeString(strValue)

		// Add to string pool and return the index as an Int
		idx := chunk.AddString(strValue)
		return vm.NewInt(int64(idx)), nil
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

func unescapeString(s string) string {
	// Handle common escape sequences in the correct order
	// We must process \\ first, but we can't use ReplaceAll because
	// it would interfere with other escapes. Instead, we'll process
	// character by character.
	result := strings.Builder{}
	result.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				result.WriteByte('\n')
				i++
			case 't':
				result.WriteByte('\t')
				i++
			case 'r':
				result.WriteByte('\r')
				i++
			case '\\':
				result.WriteByte('\\')
				i++
			case '"':
				result.WriteByte('"')
				i++
			default:
				// Unknown escape, keep as-is
				result.WriteByte(s[i])
			}
		} else {
			result.WriteByte(s[i])
		}
	}

	return result.String()
}

func parseInstruction(chunk *vm.Chunk, line string, labels map[string]int) error {
	return parseInstructionWithDebug(chunk, line, labels, nil)
}

func parseInstructionWithDebug(chunk *vm.Chunk, line string, labels map[string]int, debugInfo *vm.DebugInfo) error {
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

	// Check for .L suffix (long instruction)
	isLong := strings.HasSuffix(mnemonic, ".L")
	if isLong {
		mnemonic = strings.TrimSuffix(mnemonic, ".L")
	}

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

	case "LOADI":
		if len(args) != 2 {
			return fmt.Errorf("LOADI requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseImm(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpLOADI, a, b, 0)

	case "ADD", "SUB", "MUL", "DIV", "MOD", "BAND", "BOR", "BXOR", "SHL", "SHR", "EQ", "NE", "LT", "LE", "GT", "GE":
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
		case "MOD":
			op = vm.OpMOD
		case "BAND":
			op = vm.OpBAND
		case "BOR":
			op = vm.OpBOR
		case "BXOR":
			op = vm.OpBXOR
		case "SHL":
			op = vm.OpSHL
		case "SHR":
			op = vm.OpSHR
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

	case "NEG", "NOT", "TOINT", "TONUM":
		if len(args) != 2 {
			return fmt.Errorf("%s requires 2 arguments, got %d", mnemonic, len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseReg(args[1])
		if err != nil {
			return err
		}

		var op vm.OpCode
		switch mnemonic {
		case "NEG":
			op = vm.OpNEG
		case "NOT":
			op = vm.OpNOT
		case "TOINT":
			op = vm.OpTOINT
		case "TONUM":
			op = vm.OpTONUM
		}
		instr = vm.NewInstr(op, a, b, 0)

	case "JMP":
		if len(args) != 1 {
			return fmt.Errorf("JMP requires 1 argument, got %d", len(args))
		}

		if isLong {
			// Long JMP: emit LONG prefix + JMP32 + 32-bit offset
			if addr, ok := labels[args[0]]; ok {
				// Calculate offset: target - (current_position + 3)
				currentPos := len(chunk.Code)
				offset := int32(addr - (currentPos + 3))

				// Emit LONG prefix
				chunk.AddInstructionWithDebug(vm.NewInstr(vm.OpLONG, 0, 0, 0), debugInfo)
				// Emit JMP32 opcode
				chunk.AddInstruction(vm.NewInstr(vm.OpJMP32, 0, 0, 0))
				// Emit 32-bit offset
				chunk.AddInstruction(vm.EncodeOffset32(offset))
				return nil
			} else {
				return fmt.Errorf("JMP.L requires a label, got: %s", args[0])
			}
		}

		// Short JMP
		if addr, ok := labels[args[0]]; ok {
			if addr > 255 {
				return fmt.Errorf("jump address out of range: %d (use JMP.L for long jumps)", addr)
			}
			instr = vm.NewInstr(vm.OpJMP, byte(addr), 0, 0)
		} else {
			a, err := parseImm(args[0])
			if err != nil {
				return err
			}
			instr = vm.NewInstr(vm.OpJMP, a, 0, 0)
		}

	case "JMPIF", "JMPIFNOT":
		if len(args) != 2 {
			return fmt.Errorf("%s requires 2 arguments, got %d", mnemonic, len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}

		if isLong {
			// Long JMPIF/JMPIFNOT: emit LONG prefix + JMPIF32/JMPIFNOT32 + 32-bit offset
			if addr, ok := labels[args[1]]; ok {
				// Calculate offset: target - (current_position + 3)
				currentPos := len(chunk.Code)
				offset := int32(addr - (currentPos + 3))

				op := vm.OpJMPIF32
				if mnemonic == "JMPIFNOT" {
					op = vm.OpJMPIFNOT32
				}

				// Emit LONG prefix
				chunk.AddInstructionWithDebug(vm.NewInstr(vm.OpLONG, 0, 0, 0), debugInfo)
				// Emit JMPIF32/JMPIFNOT32 opcode with condition register
				chunk.AddInstruction(vm.NewInstr(op, a, 0, 0))
				// Emit 32-bit offset
				chunk.AddInstruction(vm.EncodeOffset32(offset))
				return nil
			} else {
				return fmt.Errorf("%s.L requires a label, got: %s", mnemonic, args[1])
			}
		}

		// Short JMPIF/JMPIFNOT
		var b byte
		if addr, ok := labels[args[1]]; ok {
			if addr > 255 {
				return fmt.Errorf("jump address out of range: %d (use %s.L for long jumps)", addr, mnemonic)
			}
			b = byte(addr)
		} else {
			b, err = parseImm(args[1])
			if err != nil {
				return err
			}
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

		if isLong {
			// Long CALL: emit LONG prefix + CALL32 + 32-bit offset
			if addr, ok := labels[args[0]]; ok {
				// Calculate offset: target - (current_position + 3)
				currentPos := len(chunk.Code)
				offset := int32(addr - (currentPos + 3))

				// Parse nargs
				nargs, err := parseImm(args[1])
				if err != nil {
					return err
				}

				// Emit LONG prefix
				chunk.AddInstructionWithDebug(vm.NewInstr(vm.OpLONG, 0, 0, 0), debugInfo)
				// Emit CALL32 opcode with nargs
				chunk.AddInstruction(vm.NewInstr(vm.OpCALL32, nargs, 0, 0))
				// Emit 32-bit offset
				chunk.AddInstruction(vm.EncodeOffset32(offset))
				return nil
			} else {
				return fmt.Errorf("CALL.L requires a label, got: %s", args[0])
			}
		}

		// Short CALL
		var a byte
		if addr, ok := labels[args[0]]; ok {
			if addr > 255 {
				return fmt.Errorf("call address out of range: %d (use CALL.L for long calls)", addr)
			}
			a = byte(addr)
		} else {
			// Try as register
			var err error
			a, err = parseReg(args[0])
			if err != nil {
				return fmt.Errorf("CALL target '%s' is not a valid label or register", args[0])
			}
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

	case "CALLEXTERN":
		if len(args) != 2 {
			return fmt.Errorf("CALLEXTERN requires 2 arguments, got %d", len(args))
		}
		// First arg is the extern function name
		externName := args[0]
		// Add to extern list and get index
		externIdx := chunk.AddExtern(externName)
		if externIdx > 255 {
			return fmt.Errorf("too many extern functions")
		}

		// Second arg is argc
		b, err := parseImm(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpCALLEXTERN, byte(externIdx), b, 0)

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

	case "ARRPUSH":
		if len(args) != 2 {
			return fmt.Errorf("ARRPUSH requires 2 arguments, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		b, err := parseReg(args[1])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpARRPUSH, a, b, 0)

	case "HASFIELD":
		if len(args) != 3 {
			return fmt.Errorf("HASFIELD requires 3 arguments, got %d", len(args))
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
		instr = vm.NewInstr(vm.OpHASFIELD, a, b, c)

	case "DELFIELD":
		if len(args) != 3 {
			return fmt.Errorf("DELFIELD requires 3 arguments, got %d", len(args))
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
		instr = vm.NewInstr(vm.OpDELFIELD, a, b, c)

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

	chunk.AddInstructionWithDebug(instr, debugInfo)
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

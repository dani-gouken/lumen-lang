package asm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dani-gouken/lumen/internal/vm"
)

// AssembleFromFile converts assembly text into a Chunk with debug info.
func AssembleFromFile(source, filePath string) (*vm.Chunk, error) {
	return assembleInternal(source, filePath)
}

// Assemble converts assembly text into a Chunk.
func Assemble(source string) (*vm.Chunk, error) {
	return assembleInternal(source, "")
}

// funcBuilder holds parsed function information during assembly
type funcBuilder struct {
	name          string
	registerCount int
	argCount      int
	upvalueCount  int
	instructions  []instructionLine
	startLine     int
}

type instructionLine struct {
	line       string
	lineNum    int
	sourceText string
	comment    string
	hasBreak   bool
}

func assembleInternal(source, filePath string) (*vm.Chunk, error) {
	chunk := vm.NewChunk()
	lines := strings.Split(source, "\n")

	// Parse constants first
	var mode string
	for lineNum, line := range lines {
		// Remove comments
		if idx := strings.Index(line, ";"); idx != -1 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, ".") {
			if strings.HasPrefix(line, ".constants") {
				mode = "constants"
			} else {
				mode = ""
			}
			continue
		}

		if mode == "constants" {
			if err := parseConstant(chunk, line); err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNum+1, err)
			}
		}
	}

	// First pass: Parse all .func blocks
	var functions []funcBuilder
	funcNameToIndex := make(map[string]int)
	var currentFunc *funcBuilder

	inConstantsBlock := false

	for lineNum, line := range lines {
		originalLine := line

		// Extract comment
		var comment string
		if idx := strings.Index(line, ";"); idx != -1 {
			comment = strings.TrimSpace(line[idx+1:])
			line = line[:idx]
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip constants section, it's already parsed
		if strings.HasPrefix(line, ".constants") {
			inConstantsBlock = true
			continue
		}
		if inConstantsBlock {
			if strings.HasPrefix(line, ".") {
				inConstantsBlock = false // End of constants section
			} else {
				continue // Inside constants section
			}
		}

		// Check for .func directive
		if strings.HasPrefix(line, ".func ") {
			if currentFunc != nil {
				return nil, fmt.Errorf("line %d: nested .func blocks not allowed", lineNum+1)
			}

			// Parse: .func <name> <reg_count> <arg_count> [upvalue_count]
			fields := strings.Fields(line)
			if len(fields) < 4 || len(fields) > 5 {
				return nil, fmt.Errorf("line %d: .func requires 3 or 4 arguments: name reg_count arg_count [upvalue_count]", lineNum+1)
			}

			name := fields[1]
			regCount, err := strconv.Atoi(fields[2])
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid register count: %v", lineNum+1, err)
			}
			argCount, err := strconv.Atoi(fields[3])
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid arg count: %v", lineNum+1, err)
			}

			upvalueCount := 0
			if len(fields) == 5 {
				upvalueCount, err = strconv.Atoi(fields[4])
				if err != nil {
					return nil, fmt.Errorf("line %d: invalid upvalue count: %v", lineNum+1, err)
				}
			}

			currentFunc = &funcBuilder{
				name:          name,
				registerCount: regCount,
				argCount:      argCount,
				upvalueCount:  upvalueCount,
				instructions:  make([]instructionLine, 0),
				startLine:     lineNum + 1,
			}
			continue
		}

		// Check for .endfunc directive
		if line == ".endfunc" {
			if currentFunc == nil {
				return nil, fmt.Errorf("line %d: .endfunc without matching .func", lineNum+1)
			}

			// Save function
			funcNameToIndex[currentFunc.name] = len(functions)
			functions = append(functions, *currentFunc)
			currentFunc = nil
			continue
		}

		// If inside a function, collect instructions
		if currentFunc != nil {
			hasBreak := comment != "" && (strings.ToUpper(comment) == "BREAK" || strings.HasPrefix(strings.ToUpper(comment), "BREAK "))
			currentFunc.instructions = append(currentFunc.instructions, instructionLine{
				line:       line,
				lineNum:    lineNum + 1,
				sourceText: strings.TrimSpace(originalLine),
				comment:    comment,
				hasBreak:   hasBreak,
			})
		} else {
			return nil, fmt.Errorf("line %d: instruction '%s' found outside of a .func block", lineNum+1, line)
		}
	}

	if currentFunc != nil {
		return nil, fmt.Errorf("line %d: .func %s missing .endfunc", currentFunc.startLine, currentFunc.name)
	}

	// Second pass: Assemble all functions and build prototypes
	labels := make(map[string]int) // For local labels within functions

	for funcIdx, fn := range functions {
		// Create prototype
		proto := vm.FunctionPrototype{
			Name:          fn.name,
			CodeStart:     len(chunk.Code),
			RegisterCount: fn.registerCount,
			ArgCount:      fn.argCount,
			UpvalueCount:  fn.upvalueCount,
		}

		// Clear labels for this function (labels are local to each function)
		labels = make(map[string]int)

		// First sub-pass: collect labels
		currentIP := len(chunk.Code)
		for _, instrLine := range fn.instructions {
			// Check if this is a label (ends with ':')
			if strings.HasSuffix(instrLine.line, ":") {
				labelName := strings.TrimSuffix(instrLine.line, ":")
				labels[labelName] = currentIP
			} else {
				// Count how many instructions this line will emit
				instrCount := countInstructionsForLine(instrLine.line)
				currentIP += instrCount
			}
		}

		// Second sub-pass: Assemble instructions
		for _, instrLine := range fn.instructions {
			// Skip label-only lines
			if strings.HasSuffix(instrLine.line, ":") {
				continue
			}

			currentIP := len(chunk.Code)

			// Create debug info
			debugInfo := vm.NewDebugInfo(filePath, instrLine.lineNum, 1)
			debugInfo.WithSourceLine(instrLine.sourceText)
			debugInfo.WithFuncName(fn.name)
			if instrLine.comment != "" {
				debugInfo.WithDescription(instrLine.comment)
			}

			// Parse and add instruction (pass funcNameToIndex for NEWFUNC resolution)
			if err := parseInstructionWithFuncTable(chunk, instrLine.line, labels, funcNameToIndex, debugInfo); err != nil {
				return nil, fmt.Errorf("line %d: %v", instrLine.lineNum, err)
			}

			// Add breakpoint if needed
			if instrLine.hasBreak {
				chunk.AddBreakpoint(currentIP)
			}
		}

		proto.CodeSize = len(chunk.Code) - proto.CodeStart
		chunk.Prototypes = append(chunk.Prototypes, proto)
		_ = funcIdx // Keep for debugging if needed
	}

	return chunk, nil
}

// countInstructionsForLine returns how many VM instructions a source line will emit
// This is used during the label collection pass to accurately compute label positions
func countInstructionsForLine(line string) int {
	// Skip line numbers if present
	if strings.Contains(line, ":") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && !strings.HasSuffix(line, ":") {
			line = strings.TrimSpace(parts[1])
		}
	}

	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0
	}

	mnemonic := strings.ToUpper(fields[0])

	// Check for .L suffix (long instructions)
	isLong := strings.HasSuffix(mnemonic, ".L")
	if isLong {
		// Long instructions emit: LONG prefix + 32-bit opcode + 32-bit offset = 3 instructions
		return 3
	}

	// All other instructions emit 1 VM instruction
	return 1
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

// parseInstructionWithFuncTable parses an instruction with function name resolution for NEWFUNC
func parseInstructionWithFuncTable(chunk *vm.Chunk, line string, labels map[string]int, funcTable map[string]int, debugInfo *vm.DebugInfo) error {
	return parseInstructionWithDebugAndFuncTable(chunk, line, labels, funcTable, debugInfo)
}

func parseInstructionWithDebug(chunk *vm.Chunk, line string, labels map[string]int, debugInfo *vm.DebugInfo) error {
	return parseInstructionWithDebugAndFuncTable(chunk, line, labels, nil, debugInfo)
}

func parseInstructionWithDebugAndFuncTable(chunk *vm.Chunk, line string, labels map[string]int, funcTable map[string]int, debugInfo *vm.DebugInfo) error {
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

	case "NEWFUNC":
		// NEWFUNC r_dest func_name
		if len(args) != 2 {
			return fmt.Errorf("NEWFUNC requires 2 arguments (dest_reg func_name), got %d", len(args))
		}

		// Parse destination register
		destReg, err := parseReg(args[0])
		if err != nil {
			return fmt.Errorf("NEWFUNC first argument must be a register: %v", err)
		}

		// Resolve function name to prototype index
		funcName := args[1]
		protoIdx, ok := funcTable[funcName]
		if !ok {
			return fmt.Errorf("unknown function: %s", funcName)
		}

		// Validate proto index fits in 8 bits
		if protoIdx > 255 {
			return fmt.Errorf("function prototype index %d out of range (max 255)", protoIdx)
		}

		instr = vm.NewInstr(vm.OpNEWFUNC, destReg, byte(protoIdx), 0)

	case "NEWEXFUNC":
		// NEWEXFUNC r_dest extern_name
		if len(args) != 2 {
			return fmt.Errorf("NEWEXFUNC requires 2 arguments (dest_reg extern_name), got %d", len(args))
		}

		// Parse destination register
		destReg, err := parseReg(args[0])
		if err != nil {
			return fmt.Errorf("NEWEXFUNC first argument must be a register: %v", err)
		}

		// Get extern function name
		externName := args[1]
		// Add to extern list and get index
		externIdx := chunk.AddExtern(externName)
		if externIdx > 255 {
			return fmt.Errorf("too many extern functions")
		}

		instr = vm.NewInstr(vm.OpNEWEXFUNC, destReg, byte(externIdx), 0)

	case "CALL":
		// New CALL: register-based only (CALL r_func nargs)
		if len(args) != 2 {
			return fmt.Errorf("CALL requires 2 arguments (func_reg nargs), got %d", len(args))
		}

		// Parse function register
		funcReg, err := parseReg(args[0])
		if err != nil {
			// Check if it looks like a label (old syntax)
			if !strings.HasPrefix(args[0], "r") {
				return fmt.Errorf("CALL no longer accepts labels; use NEWFUNC + CALL: %v", err)
			}
			return fmt.Errorf("CALL first argument must be a register: %v", err)
		}

		// Parse nargs
		nargs, err := parseImm(args[1])
		if err != nil {
			return fmt.Errorf("CALL second argument must be argument count: %v", err)
		}

		instr = vm.NewInstr(vm.OpCALL, funcReg, nargs, 0)

	case "RET":
		if len(args) != 1 {
			return fmt.Errorf("RET requires 1 argument, got %d", len(args))
		}
		a, err := parseReg(args[0])
		if err != nil {
			return err
		}
		instr = vm.NewInstr(vm.OpRET, a, 0, 0)

	case "CAPLOCAL":
		// CAPLOCAL r_dest_func upvalue_idx local_reg
		if len(args) != 3 {
			return fmt.Errorf("CAPLOCAL requires 3 arguments (dest_func_reg upvalue_idx local_reg), got %d", len(args))
		}

		destReg, err := parseReg(args[0])
		if err != nil {
			return err
		}

		upvalIdx, err := parseImm(args[1])
		if err != nil {
			return fmt.Errorf("CAPLOCAL second argument must be upvalue index: %v", err)
		}

		localReg, err := parseReg(args[2])
		if err != nil {
			return fmt.Errorf("CAPLOCAL third argument must be local register: %v", err)
		}

		instr = vm.NewInstr(vm.OpCAPLOCAL, destReg, upvalIdx, localReg)

	case "CAPUPVAL":
		// CAPUPVAL r_dest_func upvalue_idx parent_upval_idx
		if len(args) != 3 {
			return fmt.Errorf("CAPUPVAL requires 3 arguments (dest_func_reg upvalue_idx parent_upval_idx), got %d", len(args))
		}

		destReg, err := parseReg(args[0])
		if err != nil {
			return err
		}

		upvalIdx, err := parseImm(args[1])
		if err != nil {
			return fmt.Errorf("CAPUPVAL second argument must be upvalue index: %v", err)
		}

		parentUpvalIdx, err := parseImm(args[2])
		if err != nil {
			return fmt.Errorf("CAPUPVAL third argument must be parent upvalue index: %v", err)
		}

		instr = vm.NewInstr(vm.OpCAPUPVAL, destReg, upvalIdx, parentUpvalIdx)

	case "LDCAP":
		// LDCAP r_dest upvalue_idx
		if len(args) != 2 {
			return fmt.Errorf("LDCAP requires 2 arguments (dest_reg upvalue_idx), got %d", len(args))
		}

		destReg, err := parseReg(args[0])
		if err != nil {
			return err
		}

		upvalIdx, err := parseImm(args[1])
		if err != nil {
			return fmt.Errorf("LDCAP second argument must be upvalue index: %v", err)
		}

		instr = vm.NewInstr(vm.OpLDCAP, destReg, upvalIdx, 0)

	case "STCAP":
		// STCAP upvalue_idx r_src
		if len(args) != 2 {
			return fmt.Errorf("STCAP requires 2 arguments (upvalue_idx src_reg), got %d", len(args))
		}

		upvalIdx, err := parseImm(args[0])
		if err != nil {
			return fmt.Errorf("STCAP first argument must be upvalue index: %v", err)
		}

		srcReg, err := parseReg(args[1])
		if err != nil {
			return err
		}

		instr = vm.NewInstr(vm.OpSTCAP, upvalIdx, srcReg, 0)

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

	case "SETELEM":
		if len(args) != 3 {
			return fmt.Errorf("SETELEM requires 3 arguments, got %d", len(args))
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
		instr = vm.NewInstr(vm.OpSETELEM, a, b, c)

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

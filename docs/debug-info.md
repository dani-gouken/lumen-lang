# Debug Information System

This document describes the debug information system in Lumen bytecode, which provides rich error messages and stack traces during runtime.

## Overview

The Lumen VM includes a comprehensive debug information system that captures source-level context for every instruction. This enables:

- **Accurate error messages** with line numbers and source context
- **Readable stack traces** showing the call chain
- **Source-level debugging** with function names and file paths
- **Enhanced developer experience** with clear error reporting

## Debug Info Structure

Each instruction in the bytecode can have associated debug information stored in the `DebugInfo` structure:

```go
type DebugInfo struct {
    FilePath    string   // Source file path
    Line        int      // Line number in source file
    Column      int      // Column number in source file
    SourceLine  string   // Original source line text
    FuncName    string   // Function name (if in a function)
    Description string   // Human-readable description
    Notes       string   // Additional notes
}
```

## Fields

### FilePath
The path to the source file where the instruction originated.

**Example:**
```
test.lasm
/path/to/program.lasm
```

**Usage:**
- Shown in error messages
- Used for IDE integration
- Helps locate source when debugging

### Line and Column
The line and column numbers in the source file.

**Example:**
```
Line: 42
Column: 1
```

**Usage:**
- Precise error location
- Source code navigation
- Editor integration

### SourceLine
The original source code line as text. This allows error messages to show the exact line that caused the error.

**Example:**
```
SourceLine: "  ADD r2 r0 r1"
```

**Usage:**
- Displayed in error messages with visual indicators
- Helps understand context without opening source file
- Shows actual code that failed

### FuncName
The name of the function containing this instruction (if any).

**Example:**
```
FuncName: "factorial"
FuncName: "add"
FuncName: ""  // Top-level code
```

**Usage:**
- Stack traces show function names
- Helps identify location in nested calls
- Improves error context

### Description
A human-readable description of what the instruction does. Often derived from comments in the source.

**Example:**
```
Description: "Calculate factorial"
Description: "Load first argument"
```

**Usage:**
- Provides context in error messages
- Documents intent
- Aids in understanding complex operations

### Notes
Additional notes or metadata about the instruction.

**Example:**
```
Notes: "Optimized version"
Notes: "May trigger GC"
```

## Bytecode Serialization

Most of what the instructions carry is the same from one to the next: a statement is several instructions, a line several statements. In a `.lc` file it is therefore written as three tables, each thing once:

```
[TextCount:4]              The texts: file paths, source lines, function names
For each text:
  [Length:4][Bytes...]

[PlaceCount:4]             The places instructions come from
For each place, seven numbers of 4 bytes:
  [FilePath]               A text, by its number
  [Line]
  [Column]
  [SourceLine]             A text
  [FuncName]               A text
  [Description]            A text
  [Notes]                  A text

[RunCount:4]               Which instructions come from which place
For each run, three numbers of 4 bytes:
  [First]                  The first instruction (0-based)
  [Length]                 How many instructions in a row
  [Place]                  The place they all come from, by its number
```

Read back, every instruction has the record it had. A source line is in the file once, however many instructions were compiled from it; a program of a few hundred instructions carries a few kilobytes of debug information, where it carried a copy of the line and the file path per instruction before (format version 4).

Instructions from the same place share one record in memory too: a record got from `GetDebugInfo` is not to be changed.

`lumen build --strip` writes a file without any of this. The program is the same; an error then names the instruction instead of the line:

```
[ERROR] R0002: index out of bounds: 2 (length 2) (at instruction 14)
```

All multi-byte integers are little-endian. Strings are length-prefixed UTF-8.

## Assembly Integration

The assembler automatically generates debug information when using `AssembleFromFile`:

```go
chunk, err := asm.AssembleFromFile(source, "program.lasm")
```

**Automatically captured:**
- File path from second parameter
- Line numbers from source
- Source line text
- Function names from labels
- Comments as descriptions

**Example Assembly:**
```assembly
.code
  LOADK r0 0      ; Load the first value
add:
  ADD r2 r0 r1    ; Sum two numbers
  RET r2
```

**Generated Debug Info:**
```
Instruction 0:
  FilePath: "program.lasm"
  Line: 2
  Column: 1
  SourceLine: "  LOADK r0 0      ; Load the first value"
  FuncName: ""
  Description: "Load the first value"

Instruction 1:
  FilePath: "program.lasm"
  Line: 4
  Column: 1
  SourceLine: "  ADD r2 r0 r1    ; Sum two numbers"
  FuncName: "add"
  Description: "Sum two numbers"
```

## Runtime Error Messages

> The examples in this section show an older, plainer form. A runtime error is now shown as a diagnostic: `[ERROR] R0004: division by zero`, then `--> file:line:column in function 'name'`, the source line underlined, a suggestion, and the calls that led there. See [errors.md](errors.md#what-a-runtime-error-looks-like). What the debug information holds, described here, is unchanged.

When a runtime error occurs, the VM uses debug information to generate detailed error messages:

**Example Error:**
```
Runtime error: ADD requires numeric operands
  at program.lasm:8:1
    ADD r2 r0 r1
    ^
```

The error message includes:
1. **Error type and description**
2. **File path, line, and column**
3. **Source line with error indicator**

### Error Message Format

```
Runtime error: <error message>
  at <file>:<line>:<column>
    <source line>
    <position indicator>
```

**Components:**
- `<error message>`: What went wrong
- `<file>:<line>:<column>`: Where it happened
- `<source line>`: The actual code
- `<position indicator>`: Visual pointer (^)

## Stack Traces

When functions are involved, the VM generates stack traces showing the call chain:

**Example:**
```
Runtime error: division by zero
  at math.lasm:15:1
    DIV r2 r0 r1
    ^
  in function: divide
  called from: calculate (math.lasm:25)
  called from: main (main.lasm:10)
```

Stack traces show:
- **Primary error location** with source context
- **Function name** where error occurred
- **Call chain** with file locations

## API Usage

### Creating Debug Info

```go
// Create debug info for an instruction
debugInfo := vm.NewDebugInfo(filePath, line, column)
debugInfo.WithSourceLine("  ADD r2 r0 r1")
debugInfo.WithFuncName("factorial")
debugInfo.WithDescription("Sum two numbers")
debugInfo.WithNotes("May overflow for large values")
```

### Adding to Chunk

```go
chunk := vm.NewChunk()
instr := vm.NewInstr(vm.OpADD, 2, 0, 1)

debugInfo := vm.NewDebugInfo("program.lasm", 42, 1)
debugInfo.WithSourceLine("  ADD r2 r0 r1")

chunk.AddInstructionWithDebug(instr, debugInfo)
```

### Retrieving Debug Info

```go
// Get debug info for instruction at index
debugInfo := chunk.GetDebugInfo(instrIndex)
if debugInfo != nil {
    fmt.Printf("At %s:%d\n", debugInfo.FilePath, debugInfo.Line)
    fmt.Printf("  %s\n", debugInfo.SourceLine)
}
```

## Helper Methods

The `DebugInfo` type provides fluent helper methods:

```go
// NewDebugInfo creates basic debug info
debugInfo := vm.NewDebugInfo(filePath, line, column)

// WithSourceLine adds the source line text
debugInfo.WithSourceLine("  ADD r2 r0 r1")

// WithFuncName adds the function name
debugInfo.WithFuncName("factorial")

// WithDescription adds a description
debugInfo.WithDescription("Calculate sum")

// WithNotes adds additional notes
debugInfo.WithNotes("Optimized version")
```

All `With*` methods return the `DebugInfo` pointer for chaining:

```go
debugInfo := vm.NewDebugInfo("test.lasm", 10, 1).
    WithSourceLine("  ADD r2 r0 r1").
    WithFuncName("add").
    WithDescription("Sum two numbers")
```

## Performance Considerations

### Storage Overhead

Debug information increases bytecode size:
- **Without debug info**: ~290 bytes for simple program
- **With debug info**: ~450 bytes for simple program

**Overhead breakdown:**
- File path: ~20-50 bytes per unique file
- Source lines: ~30-100 bytes per instruction
- Function names: ~10-30 bytes per function
- Descriptions: ~20-50 bytes per comment

### Runtime Performance

- **No execution overhead**: Debug info is not accessed during normal execution
- **Only on error**: Debug info is read when errors occur
- **Minimal memory**: Debug info stored in map, indexed by instruction

### Optimization

For production builds, debug info can be stripped:

```go
// Strip debug info from chunk
chunk.DebugInfo = make(map[int]*vm.DebugInfo)
```

This reduces bytecode size by ~40-60% depending on comment density.

## Best Practices

### 1. Always Use AssembleFromFile
Prefer `AssembleFromFile` over `Assemble` to capture file path:

```go
// Good: captures file path
chunk, err := asm.AssembleFromFile(source, "program.lasm")

// Less useful: no file path
chunk, err := asm.Assemble(source)
```

### 2. Add Meaningful Comments
Comments in assembly become descriptions:

```go
// Good: helpful description
ADD r2 r0 r1    ; Sum two operands

// Less useful: states the obvious
ADD r2 r0 r1    ; Add
```

### 3. Use Function Labels
Labels mark function boundaries for better stack traces:

```go
factorial:
  ; Function body
  RET r0
```

### 4. Preserve Debug Info Through Transformations
When modifying chunks, preserve debug info:

```go
// Copy instruction with debug info
newChunk.AddInstructionWithDebug(
    instr,
    oldChunk.GetDebugInfo(oldIndex),
)
```

## Examples

### Example 1: Basic Debug Info

```assembly
; File: math.lasm
.constants 2
  0: 10i
  1: 20i

.code
  LOADK r0 0      ; Load first operand
  LOADK r1 1      ; Load second operand
  ADD r2 r0 r1    ; Calculate sum
  RET r2          ; Return result

.metadata
  registers: 3
  args: 0
```

**Generated Debug Info:**
- Line 6: "LOADK r0 0" → Description: "Load first operand"
- Line 7: "LOADK r1 1" → Description: "Load second operand"
- Line 8: "ADD r2 r0 r1" → Description: "Calculate sum"
- Line 9: "RET r2" → Description: "Return result"

### Example 2: Function with Debug Info

```assembly
.code
  LOADK r0 0
  CALL factorial 1
  RET r0

factorial:            ; Function entry
  LOADK r1 0
  LE r3 r0 r1        ; Check base case
  JMPIF r3 base
  ; ... recursive case ...
base:
  LOADK r2 1
  RET r2
```

**Stack Trace on Error:**
```
Runtime error: stack overflow
  at factorial.lasm:10:1
    CALL factorial 1
    ^
  in function: factorial
  called from: factorial (factorial.lasm:10)  [recursive]
  called from: main (factorial.lasm:3)
```

### Example 3: Error with Context

```assembly
; File: divide.lasm
.code
  LOADK r0 0        ; numerator
  LOADK r1 1        ; denominator
  DIV r2 r0 r1      ; Divide values
  RET r2
```

**Error Output:**
```
Runtime error: division by zero
  at divide.lasm:5:1
    DIV r2 r0 r1      ; Divide values
    ^
```

## Current Version

**Version 1** includes all features:
- Constants pool
- Code section
- Metadata
- Extern names
- String pool
- Debug information

## See Also

- [Assembly Language](assembly.md) - Assembly language reference
- [VM Architecture](luvm.md) - Virtual machine overview
- [Memory Management](memory-management.md) - Heap and GC details

# Lumen Assembly Language

This document describes the Lumen assembly language (.lasm) format and the assembler/disassembler tools.

## Overview

Lumen assembly is a human-readable text format for writing bytecode programs that run on the luvm virtual machine. The assembler converts `.lasm` files to binary `.lc` (Lumen Compiled) bytecode, and the disassembler performs the reverse operation.

## File Format

A Lumen assembly file consists of three sections:

### 1. Constants Section

```assembly
.constants <count>
  <index>: <value>
  ...
```

Defines the constant pool used by LOADK instructions.

**Example:**
```assembly
.constants 4
  0: 10.0        ; floating-point number
  1: 42i         ; 48-bit signed integer
  2: true        ; boolean
  3: null        ; null value
```

### 2. Code Section

```assembly
.code
  <instruction> <operands>...
  ...
```

Contains the bytecode instructions. Each instruction consists of an opcode mnemonic followed by operands.

**Example:**
```assembly
.code
  LOADK r0 0
  LOADK r1 1
  ADD r2 r0 r1
  RET r2
```

### 3. Metadata Section

```assembly
.metadata
  registers: <count>
  args: <count>
```

Specifies runtime metadata:
- `registers`: Number of registers needed by this function
- `args`: Number of function arguments

**Example:**
```assembly
.metadata
  registers: 3
  args: 0
```

## Value Types

### Numbers
Floating-point values using IEEE 754 double precision:
```assembly
0: 3.14159
1: -42.0
2: 1.0e10
```

### Integers
48-bit signed integers (suffix with `i`):
```assembly
0: 42i
1: -100i
2: 0i
```

Range: -140,737,488,355,328 to 140,737,488,355,327

### Booleans
```assembly
0: true
1: false
```

### Null
```assembly
0: null
```

### Strings
String literals enclosed in double quotes:
```assembly
0: "Hello, World!"
1: "This is a string"
2: ""  ; empty string
```

**Escape Sequences:**
Strings support standard escape sequences:
- `\n` - newline
- `\t` - tab
- `\r` - carriage return
- `\\` - backslash
- `\"` - double quote

**Example:**
```assembly
0: "Line 1\nLine 2"
1: "Tab\there"
2: "Quote: \"Hello\""
```

**Usage with NEWSTR:**
String constants are used with the `NEWSTR` instruction to create string objects on the heap:
```assembly
.constants 1
  0: "Hello, World!"

.code
  NEWSTR r0 0    ; Create string from constant 0
  RET r0
```

## Instruction Set

### Data Movement

#### MOV - Move Register to Register
```assembly
MOV r_dest r_src
```
Copy value from `r_src` to `r_dest`.

**Example:**
```assembly
MOV r1 r0      ; r1 = r0
```

#### LOADK - Load Constant
```assembly
LOADK r_dest const_index
```
Load constant at `const_index` into `r_dest`.

**Example:**
```assembly
LOADK r0 0     ; r0 = constants[0]
```

#### LOADNULL - Load Null
```assembly
LOADNULL r_dest
```
Load null value into `r_dest`.

**Example:**
```assembly
LOADNULL r0    ; r0 = null
```

#### LOADBOOL - Load Boolean
```assembly
LOADBOOL r_dest value
```
Load boolean `value` (true/false) into `r_dest`.

**Example:**
```assembly
LOADBOOL r0 true   ; r0 = true
LOADBOOL r1 false  ; r1 = false
```

#### LOADI - Load Small Integer
```assembly
LOADI r_dest imm8
```
Load an unsigned 8-bit immediate as an Int into `r_dest`.

**Example:**
```assembly
LOADI r0 42   ; r0 = 42i
```

### Arithmetic Operations

All arithmetic operations follow the pattern:
```assembly
OP r_dest r_src1 r_src2
```
Result is stored in `r_dest`, operands are in `r_src1` and `r_src2`.

#### ADD - Addition
```assembly
ADD r2 r0 r1   ; r2 = r0 + r1
```

#### SUB - Subtraction
```assembly
SUB r2 r0 r1   ; r2 = r0 - r1
```

#### MUL - Multiplication
```assembly
MUL r2 r0 r1   ; r2 = r0 * r1
```

#### DIV - Division
```assembly
DIV r2 r0 r1   ; r2 = r0 / r1
```

#### MOD - Remainder
```assembly
MOD r2 r0 r1   ; r2 = r0 % r1
```

#### NEG - Negation
```assembly
NEG r1 r0      ; r1 = -r0
```
Works for Number and Int, preserving the input type.

**Type Rules:**
- Number + Number → Number
- Int + Int → Int
- Mixing types is an error

### Conversions

#### TOINT - Truncate to Integer
```assembly
TOINT r_dest r_src   ; r_dest = trunc(r_src)
```
Accepts Number (truncated toward zero) or Int (passthrough). Other types error.

#### TONUM - Convert to Number
```assembly
TONUM r_dest r_src   ; r_dest = float(r_src)
```
Accepts Int (converted to Number) or Number (passthrough). Other types error.

### Logical

#### NOT - Boolean Negation
```assembly
NOT r_dest r_src   ; r_dest = !r_src
```
Accepts Bool only.

### Bitwise Operations (Int only)
```assembly
BAND r2 r0 r1   ; r2 = r0 & r1
BOR  r2 r0 r1   ; r2 = r0 | r1
BXOR r2 r0 r1   ; r2 = r0 ^ r1
SHL  r2 r0 r1   ; r2 = r0 << r1
SHR  r2 r0 r1   ; r2 = r0 >> r1
```
Shift counts must be non-negative. Mixing non-Int operands is an error.

### Comparison Operations

All comparison operations follow the pattern:
```assembly
OP r_dest r_src1 r_src2
```
Result (boolean) is stored in `r_dest`.

#### EQ - Equal
```assembly
EQ r2 r0 r1    ; r2 = (r0 == r1)
```

#### NE - Not Equal
```assembly
NE r2 r0 r1    ; r2 = (r0 != r1)
```

#### LT - Less Than
```assembly
LT r2 r0 r1    ; r2 = (r0 < r1)
```

#### LE - Less or Equal
```assembly
LE r2 r0 r1    ; r2 = (r0 <= r1)
```

#### GT - Greater Than
```assembly
GT r2 r0 r1    ; r2 = (r0 > r1)
```

#### GE - Greater or Equal
```assembly
GE r2 r0 r1    ; r2 = (r0 >= r1)
```

### Heap Objects

#### NEWSTR - Create String
```assembly
NEWSTR r_dest const_index
```
Create a heap-allocated string object from a string constant.

**Arguments:**
- `r_dest`: destination register for the string object
- `const_index`: index of string constant in the constant pool

**Behavior:**
- Retrieves string literal from constant pool
- Allocates string object on the heap
- Stores heap pointer in `r_dest`
- May trigger garbage collection if heap is full

**Example:**
```assembly
.constants 1
  0: "Hello, World!"

.code
  NEWSTR r0 0       ; r0 = pointer to "Hello, World!" string
  RET r0
```

**Memory Management:**
- String objects are garbage collected when no longer referenced
- Strings are immutable
- String data is stored on the heap

#### NEWARRAY - Create Array
```assembly
NEWARRAY r_dest capacity
```
Create a heap-allocated array with the specified capacity.

**Arguments:**
- `r_dest`: destination register for the array object
- `capacity`: initial capacity of the array (0-255)

**Behavior:**
- Allocates array object on the heap
- Stores heap pointer in `r_dest`
- Array is initially empty (length 0)
- May trigger garbage collection if heap is full

**Example:**
```assembly
NEWARRAY r0 10    ; Create array with capacity 10
LOADK r1 0        ; r1 = index 0
LOADK r2 1        ; r2 = value to store
SETINDEX r0 r1 r2 ; array[0] = value
```

**Memory Management:**
- Array objects are garbage collected when no longer referenced
- Arrays can grow dynamically beyond initial capacity
- Elements are initialized to null

#### NEWOBJ - Create Object
```assembly
NEWOBJ r_dest
```
Create a heap-allocated object (map/dictionary).

**Arguments:**
- `r_dest`: destination register for the object

**Behavior:**
- Allocates object on the heap
- Stores heap pointer in `r_dest`
- Object starts empty (no fields)
- May trigger garbage collection if heap is full

**Example:**
```assembly
NEWOBJ r0         ; Create empty object
NEWSTR r1 0       ; Create field name "key"
LOADK r2 1        ; r2 = value 42
SETFIELD r0 0 r2  ; obj["key"] = 42
```

**Memory Management:**
- Object fields are stored in a hash map
- Objects are garbage collected when no longer referenced
- Fields can be added dynamically

### Array Operations

#### GETINDEX - Get Array Element
```assembly
GETINDEX r_dest r_array r_index
```
Get element at index from array.

**Arguments:**
- `r_dest`: destination register for the value
- `r_array`: register containing array heap pointer
- `r_index`: register containing integer index

**Behavior:**
- Retrieves element at `r_index` from array in `r_array`
- Stores result in `r_dest`
- Returns null if index is out of bounds

**Example:**
```assembly
NEWARRAY r0 5     ; Create array with capacity 5
LOADK r1 0        ; r1 = index 0
LOADK r2 1        ; r2 = value 42
SETINDEX r0 r1 r2 ; array[0] = 42

LOADK r3 0        ; r3 = index 0
GETINDEX r4 r0 r3 ; r4 = array[0] (42)
```

**Error Conditions:**
- Runtime error if `r_array` is not a heap pointer
- Runtime error if `r_index` is not an integer
- Returns null for out-of-bounds access

#### SETINDEX - Set Array Element
```assembly
SETINDEX r_array r_index r_value
```
Set element at index in array.

**Arguments:**
- `r_array`: register containing array heap pointer
- `r_index`: register containing integer index
- `r_value`: register containing value to store

**Behavior:**
- Stores `r_value` at index `r_index` in array
- Array grows automatically if needed
- Elements between current length and index filled with null

**Example:**
```assembly
NEWARRAY r0 3     ; Create array
LOADK r1 0        ; r1 = index 0
LOADK r2 1        ; r2 = value 10
SETINDEX r0 r1 r2 ; array[0] = 10

LOADK r1 2        ; r1 = index 2
LOADK r2 3        ; r2 = value 30
SETINDEX r0 r1 r2 ; array[2] = 30 (array[1] becomes null)
```

**Error Conditions:**
- Runtime error if `r_array` is not a heap pointer
- Runtime error if `r_index` is not an integer
- Runtime error if index is negative

#### ARRPUSH - Append to Array
```assembly
ARRPUSH r_array r_value
```
Append `r_value` to the end of `r_array`.

**Error Conditions:**
- Runtime error if `r_array` is not a heap pointer to an array

#### ARRLEN - Get Array Length
```assembly
ARRLEN r_dest r_array
```
Get the length of an array.

**Arguments:**
- `r_dest`: destination register for the length (as integer)
- `r_array`: register containing array heap pointer

**Behavior:**
- Returns the number of elements in the array
- Stores result as integer in `r_dest`

**Example:**
```assembly
NEWARRAY r0 5     ; Create array
LOADK r1 0
LOADK r2 1
SETINDEX r0 r1 r2 ; Add one element

ARRLEN r3 r0      ; r3 = 1 (length)
```

**Error Conditions:**
- Runtime error if `r_array` is not a heap pointer to an array

### Object Operations

#### GETFIELD - Get Object Field
```assembly
GETFIELD r_dest r_object const_index
```
Get field value from object.

**Arguments:**
- `r_dest`: destination register for the value
- `r_object`: register containing object heap pointer
- `const_index`: constant pool index of field name (string)

**Behavior:**
- Retrieves field named by constant at `const_index`
- Stores result in `r_dest`
- Returns null if field doesn't exist

**Example:**
```assembly
.constants 2
  0: "name"
  1: "age"

.code
  NEWOBJ r0         ; Create object
  NEWSTR r1 0       ; r1 = "name"
  LOADK r2 1        ; r2 = "John"
  SETFIELD r0 0 r2  ; obj["name"] = "John"
  
  GETFIELD r3 r0 0  ; r3 = obj["name"] ("John")
```

**Error Conditions:**
- Runtime error if `r_object` is not a heap pointer to an object
- Runtime error if constant at `const_index` is not a string

#### SETFIELD - Set Object Field
```assembly
SETFIELD r_object const_index r_value
```
Set field value in object.

**Arguments:**
- `r_object`: register containing object heap pointer
- `const_index`: constant pool index of field name (string)
- `r_value`: register containing value to store

**Behavior:**
- Sets field named by constant at `const_index` to `r_value`
- Creates field if it doesn't exist
- Updates field if it exists

**Example:**
```assembly
.constants 3
  0: "x"
  1: "y"
  2: 100i

.code
  NEWOBJ r0         ; Create object
  LOADK r1 2        ; r1 = 100
  SETFIELD r0 0 r1  ; obj["x"] = 100
  
  LOADK r2 2        ; r2 = 100
  SETFIELD r0 1 r2  ; obj["y"] = 100
```

**Error Conditions:**
- Runtime error if `r_object` is not a heap pointer to an object
- Runtime error if constant at `const_index` is not a string

#### HASFIELD - Check Object Field Presence
```assembly
HASFIELD r_dest r_object const_index
```
Store a boolean indicating whether the object has a field with the given name.

**Error Conditions:**
- Runtime error if `r_object` is not a heap pointer to an object
- Runtime error if constant at `const_index` is not a string

#### DELFIELD - Delete Object Field
```assembly
DELFIELD r_dest r_object const_index
```
Delete a field from an object, storing `true` in `r_dest` if it existed, `false` otherwise.

**Error Conditions:**
- Runtime error if `r_object` is not a heap pointer to an object
- Runtime error if constant at `const_index` is not a string

### Garbage Collection

#### GC - Trigger Garbage Collection
```assembly
GC
```
Manually trigger a garbage collection cycle.

**Behavior:**
- Performs mark-and-sweep garbage collection
- Marks all reachable objects from registers and stack
- Sweeps and frees unmarked objects
- Updates heap statistics

**Example:**
```assembly
; Create some temporary objects
NEWARRAY r0 100
NEWARRAY r1 100
NEWARRAY r2 100

; Only keep r2
LOADNULL r0
LOADNULL r1

; Force GC to clean up r0 and r1
GC

; r2 is still valid, r0 and r1 objects are freed
```

**When to Use:**
- Rarely needed - GC triggers automatically when heap is 80% full
- Useful for testing or freeing memory before large allocations
- Can help with predictable timing in real-time applications

**Performance:**
- GC is a stop-the-world operation
- Time proportional to number of live objects
- Automatic GC is preferred in most cases

### Control Flow

#### Labels
Labels mark code locations for jumps and function calls:

```assembly
label_name:
  ; code here
```

Labels can be used with JMP, JMPIF, JMPIFNOT, and CALL instructions.

**Example:**
```assembly
.code
  JMP skip_this
  LOADK r0 0
skip_this:
  LOADK r1 1
```

#### JMP - Unconditional Jump
```assembly
JMP label       ; jump to label
JMP offset      ; jump to instruction offset
```
Jump to a label or instruction offset.

**Examples:**
```assembly
JMP loop_start  ; jump to label
JMP 5           ; jump forward 5 instructions
```

#### JMPIF - Jump If True
```assembly
JMPIF r_cond label
JMPIF r_cond offset
```
Jump to label/offset if `r_cond` is true.

**Examples:**
```assembly
JMPIF r0 then_branch    ; if r0 is true, jump to label
JMPIF r0 3              ; if r0 is true, jump forward 3 instructions
```

#### JMPIFNOT - Jump If False
```assembly
JMPIFNOT r_cond label
JMPIFNOT r_cond offset
```
Jump to label/offset if `r_cond` is false.

**Examples:**
```assembly
JMPIFNOT r0 else_branch ; if r0 is false, jump to label
JMPIFNOT r0 3           ; if r0 is false, jump forward 3 instructions
```

#### Long Instructions (32-bit Offsets)

For large programs where jump targets exceed 255 instructions, long instruction variants are available. These use a `.L` suffix and support 32-bit signed offsets, allowing jumps to any location in the code.

**Long Instruction Format:**
Long instructions are encoded as 3 consecutive instruction words:
1. `LONG` prefix (indicates the next instruction uses a 32-bit offset)
2. Opcode instruction (JMP32, JMPIF32, JMPIFNOT32, or CALL32)
3. 32-bit offset word (signed, relative to position after the 3-word block)

**Supported Long Instructions:**
```assembly
JMP.L label              ; Long unconditional jump
JMPIF.L r_cond label     ; Long conditional jump if true
JMPIFNOT.L r_cond label  ; Long conditional jump if false
CALL.L label nargs       ; Long function call
```

**When to Use Long Instructions:**
- When the target label is more than 255 instructions away
- The assembler will report an error if a short jump/call is out of range
- Long instructions take 3× the space of short instructions (12 bytes vs 4 bytes)

**Example:**
```assembly
.code
  LOADK r0 0
  JMP.L far_away_label  ; Assembled as: LONG + JMP32 + offset

  ; ... many instructions (256+) ...

far_away_label:
  LOADK r1 1
  JMP.L back_to_start   ; Can jump backwards too

back_to_start:
  RET r0
```

**Implementation Details:**
- Offset calculation: `target_address - (current_position + 3)`
- Offsets are signed 32-bit integers (range: -2,147,483,648 to 2,147,483,647)
- The VM automatically handles the 3-word sequence when encountering a LONG prefix

### Function Calls

#### CALL - Call Function
```assembly
CALL label nargs     ; Short call (label must be within 255 instructions)
CALL r_func nargs    ; Indirect call via register
CALL.L label nargs   ; Long call (supports 32-bit offsets for distant functions)
```
Call function at `label` or in `r_func` with `nargs` arguments.

**Calling Convention:**
- Arguments passed in registers r0, r1, r2, ..., r(nargs-1)
- Return value placed in r0
- Callee gets fresh register set
- Caller's registers restored after return

**Example:**
```assembly
.code
  LOADK r0 0        ; r0 = 5 (first arg)
  LOADK r1 1        ; r1 = 10 (second arg)
  CALL add 2        ; call add(5, 10)
  ; Result is now in r0
  RET r0

add:
  ADD r2 r0 r1      ; r2 = r0 + r1
  RET r2            ; return r2
```

For functions that are more than 255 instructions away, use `CALL.L`:
```assembly
.code
  LOADK r0 0
  LOADK r1 1
  CALL.L distant_function 2  ; Long call to distant function
  RET r0

  ; ... many instructions (256+) ...

distant_function:
  ADD r2 r0 r1
  RET r2
```

#### CALLEXTERN - Call External Function
```assembly
CALLEXTERN name nargs
```
Call external (host) function `name` with `nargs` arguments.

**Built-in External Functions:**
- `print` - Print values without newline
- `println` - Print values with newline

**Arguments:**
- Passed in registers r0, r1, r2, ..., r(nargs-1)
- Return value placed in r0

**Example:**
```assembly
.code
  LOADK r0 0        ; r0 = 42
  CALLEXTERN println 1   ; println(42)
```

#### RET - Return
```assembly
RET r_value
```
Return `r_value` from the current function.

**Behavior:**
- If in nested call: restores caller's frame and places return value in caller's r0
- If at top level: exits execution with return value

**Example:**
```assembly
RET r0         ; return value in r0
```

### NOP - No Operation
```assembly
NOP
```
Does nothing. Used for padding or debugging.

## Comments

Comments start with `;` and continue to end of line:

```assembly
LOADK r0 0     ; load constant 0
ADD r1 r0 r0   ; double the value
```

## Register Naming

Registers are named `r0` through `r255`:
- `r0`, `r1`, `r2`, etc.
- Maximum 256 registers per function

By convention:
- `r0-r7`: commonly used for local variables
- Higher registers: temporaries and spills

## Binary Format (.lc)

The assembler produces compact binary `.lc` files with this structure:

```
[Magic:4]              0x4C554D4E ("LUMN")
[Version:4]            Format version (currently 1)
[ConstCount:4]         Number of constants
[Constants...]         Constants (8 bytes each, NaN-boxed)
[CodeCount:4]          Number of instructions
[Code...]              Instructions (4 bytes each)
[RegisterCount:4]      Number of registers
[ArgCount:4]           Number of arguments
[ExternCount:4]        Number of extern functions
[ExternNames...]       Extern function names (length-prefixed strings)
[StringPoolCount:4]    Number of string literals
[StringPool...]        String literals (length-prefixed strings)
[DebugInfoCount:4]     Number of debug info entries
[DebugInfo...]         Debug information for error messages
```

**String Format:**
Each string (extern names, string pool, debug strings) is stored as:
```
[Length:4]             Length of string in bytes
[Bytes...]             UTF-8 encoded string data
```

**Debug Info Format:**
Each debug info entry is stored as:
```
[InstrIndex:4]         Instruction index
[FilePathLen:4]        Length of file path
[FilePath...]          Source file path
[Line:4]               Line number in source
[Column:4]             Column number in source
[SourceLineLen:4]      Length of source line text
[SourceLine...]        Original source line
[FuncNameLen:4]        Length of function name
[FuncName...]          Function name (if in function)
[DescriptionLen:4]     Length of description
[Description...]       Human-readable description
[NotesLen:4]           Length of notes
[Notes...]             Additional notes
```

All multi-byte integers are little-endian.

## Command-Line Tools

### Assembler

Convert assembly text to binary bytecode:

```bash
lumen asm <input.lasm> <output.lc>
```

**Example:**
```bash
lumen asm program.lasm program.lc
```

**Error Handling:**
- Syntax errors show line numbers
- Invalid opcodes are rejected
- Register/immediate values must be in range [0-255]

### Disassembler

Convert binary bytecode to assembly text:

```bash
lumen disasm <input.lc> <output.lasm>
```

**Example:**
```bash
lumen disasm program.lc program.lasm
```

The disassembler output includes:
- Line numbers for each instruction (e.g., `0000:`)
- All constants with their types
- Metadata section

### Executor

Execute binary bytecode:

```bash
lumen exec <program.lc>
```

**Example:**
```bash
lumen exec program.lc
# Output: Result: Number(42)
```

## Complete Examples

### Example 1: Simple Arithmetic

**File: `arithmetic.lasm`**
```assembly
; Calculate (10 + 5) * 2

.constants 3
  0: 10i
  1: 5i
  2: 2i

.code
  LOADK r0 0      ; r0 = 10
  LOADK r1 1      ; r1 = 5
  ADD r2 r0 r1    ; r2 = 10 + 5 = 15
  LOADK r3 2      ; r3 = 2
  MUL r4 r2 r3    ; r4 = 15 * 2 = 30
  RET r4          ; return 30

.metadata
  registers: 5
  args: 0
```

**Usage:**
```bash
# Assemble
lumen asm arithmetic.lasm arithmetic.lc
# Output: Assembled arithmetic.lasm -> arithmetic.lc (68 bytes)

# Execute
lumen exec arithmetic.lc
# Output: Result: Int(30)

# Disassemble
lumen disasm arithmetic.lc output.lasm
# Creates readable assembly with line numbers
```

### Example 2: Function Calls

**File: `functions.lasm`**
```assembly
; Function call example

.constants 2
  0: 5i
  1: 10i

.code
  ; Main code
  LOADK r0 0        ; r0 = 5
  LOADK r1 1        ; r1 = 10
  CALL add 2        ; Call add(5, 10)
  RET r0            ; Return result (15)

add:
  ; add(a, b) returns a + b
  ; Arguments: r0 = a, r1 = b
  ADD r2 r0 r1      ; r2 = r0 + r1
  RET r2            ; Return r2

.metadata
  registers: 3
  args: 0
```

### Example 3: Recursion

**File: `factorial.lasm`**
```assembly
; Factorial using recursion
; Computes factorial(5) = 120

.constants 3
  0: 5i             ; Input number
  1: 1i             ; Base case and decrement
  2: 0i             ; For comparison

.code
  LOADK r0 0        ; r0 = 5
  CALL factorial 1  ; Call factorial(5)
  RET r0            ; Return result

factorial:
  ; factorial(n) in r0
  LOADK r1 2        ; r1 = 0
  LE r3 r0 r1       ; r3 = (r0 <= 0)
  JMPIF r3 base_case

  ; Recursive case: n * factorial(n-1)
  MOV r4 r0         ; Save n
  LOADK r1 1        ; r1 = 1
  SUB r0 r4 r1      ; r0 = n - 1
  CALL factorial 1  ; factorial(n-1)
  MUL r2 r4 r0      ; r2 = n * factorial(n-1)
  RET r2

base_case:
  LOADK r2 1        ; Return 1
  RET r2

.metadata
  registers: 5
  args: 0
```

### Example 4: String Literals

**File: `strings.lasm`**
```assembly
; Working with string literals

.constants 3
  0: "Hello, World!"
  1: "Lumen"
  2: "Escape sequences:\n\tTab\n\tNewline"

.code
  ; Create first string
  NEWSTR r0 0       ; r0 = "Hello, World!"

  ; Create second string
  NEWSTR r1 1       ; r1 = "Lumen"

  ; Create string with escape sequences
  NEWSTR r2 2       ; r2 = string with tabs and newlines

  RET r0

.metadata
  registers: 3
  args: 0
```

**Usage:**
```bash
lumen asm strings.lasm strings.lc
lumen exec strings.lc
# Output: Result: String("Hello, World!")
```

### Example 5: External Functions

**File: `extern.lasm`**
```assembly
; Using external functions

.constants 2
  0: 42i
  1: 100i

.code
  LOADK r0 0        ; r0 = 42
  CALLEXTERN println 1  ; println(42)

  LOADK r0 1        ; r0 = 100
  CALLEXTERN println 1  ; println(100)

  LOADNULL r0
  RET r0

.metadata
  registers: 1
  args: 0
```

### Example 6: Array Operations

**File: `arrays.lasm`**
```assembly
; Working with arrays

.constants 5
  0: 10i
  1: 20i
  2: 30i
  3: 0i         ; index 0
  4: 1i         ; increment

.code
  ; Create array with capacity 5
  NEWARRAY r0 5
  
  ; Store values in array
  LOADK r1 3        ; r1 = index 0
  LOADK r2 0        ; r2 = value 10
  SETINDEX r0 r1 r2 ; array[0] = 10
  
  LOADK r1 4        ; r1 = 1 (for increment)
  LOADK r3 3        ; r3 = index 0
  ADD r3 r3 r1      ; r3 = 1
  LOADK r2 1        ; r2 = value 20
  SETINDEX r0 r3 r2 ; array[1] = 20
  
  ADD r3 r3 r1      ; r3 = 2
  LOADK r2 2        ; r2 = value 30
  SETINDEX r0 r3 r2 ; array[2] = 30
  
  ; Get array length
  ARRLEN r4 r0      ; r4 = 3
  
  ; Read from array
  LOADK r1 3        ; r1 = index 0
  GETINDEX r5 r0 r1 ; r5 = array[0] (10)
  
  RET r5

.metadata
  registers: 6
  args: 0
```

### Example 7: Object Fields

**File: `objects.lasm`**
```assembly
; Working with objects

.constants 4
  0: "name"
  1: "age"
  2: "city"
  3: 25i

.code
  ; Create object
  NEWOBJ r0
  
  ; Set fields using string constants
  NEWSTR r1 0       ; r1 = "name" (for display)
  NEWSTR r2 0       ; r2 = string value "name"
  SETFIELD r0 0 r2  ; obj["name"] = "name"
  
  LOADK r3 3        ; r3 = 25
  SETFIELD r0 1 r3  ; obj["age"] = 25
  
  NEWSTR r4 2       ; r4 = "city"
  SETFIELD r0 2 r4  ; obj["city"] = "city"
  
  ; Get field values
  GETFIELD r5 r0 0  ; r5 = obj["name"]
  GETFIELD r6 r0 1  ; r6 = obj["age"] (25)
  
  RET r6

.metadata
  registers: 7
  args: 0
```

### Example 8: Nested Structures

**File: `nested.lasm`**
```assembly
; Nested arrays and objects

.constants 5
  0: "data"
  1: "count"
  2: 10i
  3: 20i
  4: 0i

.code
  ; Create outer object
  NEWOBJ r0
  
  ; Create inner array
  NEWARRAY r1 2
  LOADK r2 4        ; r2 = index 0
  LOADK r3 2        ; r3 = value 10
  SETINDEX r1 r2 r3 ; array[0] = 10
  
  LOADK r2 4
  LOADK r4 1
  ADD r2 r2 r4      ; r2 = index 1
  LOADK r3 3        ; r3 = value 20
  SETINDEX r1 r2 r3 ; array[1] = 20
  
  ; Store array in object
  SETFIELD r0 0 r1  ; obj["data"] = array
  
  ; Store count
  ARRLEN r5 r1      ; r5 = array length (2)
  SETFIELD r0 1 r5  ; obj["count"] = 2
  
  ; Retrieve nested data
  GETFIELD r6 r0 0  ; r6 = obj["data"] (array)
  LOADK r7 4        ; r7 = index 0
  GETINDEX r8 r6 r7 ; r8 = array[0] (10)
  
  RET r8

.metadata
  registers: 9
  args: 0
```

### Example 9: Garbage Collection

**File: `gc_demo.lasm`**
```assembly
; Demonstrating garbage collection

.constants 2
  0: 1000i      ; Large array size
  1: 42i        ; Final value

.code
  ; Allocate several large arrays
  LOADK r1 0
  NEWARRAY r0 r1    ; Create array[1000]
  NEWARRAY r2 r1    ; Create array[1000]
  NEWARRAY r3 r1    ; Create array[1000]
  
  ; Drop references to r0 and r2
  LOADNULL r0
  LOADNULL r2
  
  ; Manually trigger GC to free r0 and r2
  GC
  
  ; r3 is still valid
  LOADK r4 1        ; r4 = 42
  RET r4

.metadata
  registers: 5
  args: 0
```

### Example 10: Array Iteration

**File: `array_sum.lasm`**
```assembly
; Sum elements of an array

.constants 6
  0: 10i
  1: 20i
  2: 30i
  3: 40i
  4: 50i
  5: 0i         ; index/sum

.code
  ; Create and populate array
  NEWARRAY r0 5
  
  LOADK r1 5        ; r1 = index 0
  LOADK r2 0        ; r2 = 10
  SETINDEX r0 r1 r2
  
  LOADK r3 1        ; r3 = 1 (increment)
  ADD r1 r1 r3      ; r1 = 1
  LOADK r2 1        ; r2 = 20
  SETINDEX r0 r1 r2
  
  ADD r1 r1 r3      ; r1 = 2
  LOADK r2 2        ; r2 = 30
  SETINDEX r0 r1 r2
  
  ADD r1 r1 r3      ; r1 = 3
  LOADK r2 3        ; r2 = 40
  SETINDEX r0 r1 r2
  
  ADD r1 r1 r3      ; r1 = 4
  LOADK r2 4        ; r2 = 50
  SETINDEX r0 r1 r2
  
  ; Sum array elements
  LOADK r4 5        ; r4 = sum (0)
  LOADK r1 5        ; r1 = index (0)
  ARRLEN r5 r0      ; r5 = length (5)
  
loop:
  LT r6 r1 r5       ; r6 = (index < length)
  JMPIFNOT r6 done
  
  GETINDEX r7 r0 r1 ; r7 = array[index]
  ADD r4 r4 r7      ; sum += array[index]
  ADD r1 r1 r3      ; index++
  
  JMP loop
  
done:
  RET r4            ; Return sum (150)

.metadata
  registers: 8
  args: 0
```

## Best Practices

1. **Use meaningful register allocations**: Keep related values in nearby registers
2. **Comment your code**: Explain non-obvious operations
3. **Minimize register count**: Use only as many registers as needed
4. **Type consistency**: Keep operations on same types (int vs number)
5. **Constant pool**: Reuse constants when possible to reduce file size

## Limitations

- **No macros**: Each instruction must be written explicitly
- **No symbolic constants**: All values must be literals in constant pool
- **Single chunk per file**: Each file represents one chunk with multiple functions
- **48-bit integers**: Larger values require using floats
- **255 instruction limit**: JMP/CALL targets limited to byte range (can be extended with indirect jumps)
- **256 register limit**: Maximum 256 registers per function
- **256 call depth**: Maximum recursion/call nesting depth

## Future Enhancements

Planned features:
- Symbolic constant definitions
- Multi-chunk files (separate compilation units)
- Macro system
- Include directive for code reuse
- Extended addressing modes for large programs
- Optimization passes in assembler

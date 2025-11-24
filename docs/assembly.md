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

**Type Rules:**
- Number + Number → Number
- Int + Int → Int
- Mixing types is an error

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

### Control Flow

#### JMP - Unconditional Jump
```assembly
JMP offset
```
Jump to instruction at `offset` (relative to current position).

**Example:**
```assembly
JMP 5          ; jump forward 5 instructions
```

#### JMPIF - Jump If True
```assembly
JMPIF r_cond offset
```
Jump to `offset` if `r_cond` is true.

**Example:**
```assembly
JMPIF r0 3     ; if r0 is true, jump forward 3 instructions
```

#### JMPIFNOT - Jump If False
```assembly
JMPIFNOT r_cond offset
```
Jump to `offset` if `r_cond` is false.

**Example:**
```assembly
JMPIFNOT r0 3  ; if r0 is false, jump forward 3 instructions
```

### Function Calls

#### CALL - Call Function
```assembly
CALL r_func nargs
```
Call function in `r_func` with `nargs` arguments.

**Note:** Currently not fully implemented.

#### RET - Return
```assembly
RET r_value
```
Return `r_value` from the current function.

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

## Complete Example

**File: `example.lasm`**
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
lumen asm example.lasm example.lc
# Output: Assembled example.lasm -> example.lc (68 bytes)

# Execute
lumen exec example.lc
# Output: Result: Int(30)

# Disassemble
lumen disasm example.lc output.lasm
# Creates readable assembly with line numbers
```

## Best Practices

1. **Use meaningful register allocations**: Keep related values in nearby registers
2. **Comment your code**: Explain non-obvious operations
3. **Minimize register count**: Use only as many registers as needed
4. **Type consistency**: Keep operations on same types (int vs number)
5. **Constant pool**: Reuse constants when possible to reduce file size

## Limitations

- **No labels**: Jump offsets must be calculated manually
- **No macros**: Each instruction must be written explicitly
- **No symbolic constants**: All values must be literals
- **Single chunk**: Each file represents one function/chunk
- **48-bit integers**: Larger values require using floats

## Future Enhancements

Planned features:
- Label support for jumps
- Symbolic constant definitions
- Multi-chunk files (multiple functions)
- Macro system
- Include directive for code reuse

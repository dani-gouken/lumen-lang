# Lumen Assembly Language

This document describes the Lumen assembly language (.lasm) format and the assembler/disassembler tools.

## Overview

Lumen assembly is a human-readable text format for writing bytecode programs that run on the luvm virtual machine. The assembler converts `.lasm` files to binary `.lc` (Lumen Compiled) bytecode, and the disassembler performs the reverse operation.

## File Format

A Lumen assembly file consists of a constants section followed by one or more function definitions:

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

### 2. Function Definitions

```assembly
.func <name> <registers> <args> [upvalue_count]
  <instruction> <operands>...
  ...
.endfunc
```

Defines a function with its bytecode instructions.

**Parameters:**
- `<name>`: Function identifier (e.g., `main`, `add`, `factorial`)
- `<registers>`: Number of registers needed by this function
- `<args>`: Number of function arguments
- `[upvalue_count]`: (Optional) Number of upvalues (captured variables) this function uses. Defaults to 0.

**Example:**
```assembly
.func main 3 0
  LOADK r0 0
  LOADK r1 1
  ADD r2 r0 r1
  RET r2
.endfunc
```

**Closures:**
Functions that capture variables from their enclosing scope must specify the `upvalue_count`.

```assembly
.func inner 2 0 1    ; 1 upvalue
  LDCAP r0 0         ; Load upvalue 0
  RET r0
.endfunc
```

**Multiple Functions:**
A file can contain multiple function definitions. The `main` function is the entry point:

```assembly
.func main 2 0
  NEWFUNC r0 helper
  LOADK r1 0
  CALL r0 1
  RET r0
.endfunc

.func helper 2 1
  ADD r1 r0 r0
  RET r1
.endfunc
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

**How string constants are stored:**
The text of a string literal goes into the chunk's string pool; the constant
itself holds the pool index. A string constant is therefore not a string value
and cannot be loaded with `LOADK`. It is consumed by the instructions that
expect one: `NEWSTR` (create a string object) and `GETFIELD`, `SETFIELD`,
`HASFIELD`, `DELFIELD` (field name).

**Usage with NEWSTR:**
String constants are used with the `NEWSTR` instruction to create string objects on the heap:
```assembly
.constants 1
  0: "Hello, World!"


.func main 1 0
  NEWSTR r0 0    ; Create string from constant 0
  RET r0
.endfunc
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
LOADI r_dest imm16
```
Load an unsigned 16-bit immediate (0-65535) as an Int into `r_dest`.

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
Both operands must be Int, both Number, or both String. Two strings are
concatenated into a new string object:
```assembly
NEWSTR r0 0    ; "foo"
NEWSTR r1 1    ; "bar"
ADD r2 r0 r1   ; r2 = "foobar"
```
Mixing kinds (Int + Number, String + Int, ...) is a runtime error.

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
Int division truncates. Dividing an Int by zero is a runtime error
(`division by zero`); Number division follows IEEE 754 and yields infinity.

#### MOD - Remainder
```assembly
MOD r2 r0 r1   ; r2 = r0 % r1
```
Int remainder by zero is a runtime error (`division by zero`).

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
Strings are equal when their contents are equal. Arrays, objects and functions
are equal only to themselves (identity). Values of different kinds are never
equal, so `1i` and `1.0` differ.

#### NE - Not Equal
```assembly
NE r2 r0 r1    ; r2 = (r0 != r1)
```

#### LT - Less Than
```assembly
LT r2 r0 r1    ; r2 = (r0 < r1)
```
`LT`, `LE`, `GT` and `GE` accept two Ints, two Numbers or two Strings. Strings
are ordered byte by byte (`"a" < "b"`, `"Z" < "a"`).

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


.func main 1 0
  NEWSTR r0 0       ; r0 = pointer to "Hello, World!" string
  RET r0
.endfunc
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
- `capacity`: initial capacity of the array (0-65535)

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

**Struct names:** the instruction has a second form used by the compiler, in
which operand B is a string constant naming the struct the object is a value
of and operand C is 1. The name is what the `__cast` host function checks for
`value as Point`. The assembly syntax above always creates an unnamed object,
and the disassembler does not show the name.

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
Get the element at an index of an array, or the character at an index of a
string.

**Arguments:**
- `r_dest`: destination register for the value
- `r_array`: register containing an array or a string
- `r_index`: register containing integer index

**Behavior:**
- Array: stores the element at `r_index` in `r_dest`
- String: stores a new one-character string in `r_dest`. Indices count
  characters (Unicode code points), not bytes

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
- Runtime error if `r_array` is neither an array nor a string
- Runtime error if `r_index` is not an integer
- Runtime error if the index is negative or not below the length
  (`index out of bounds: 3 (length 2)`)

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
- An index between the current length and the capacity given to `NEWARRAY`
  grows the array; skipped elements are filled with null
- To grow an array beyond its capacity, use `ARRPUSH`

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
- Runtime error if `r_array` is not an array
- Runtime error if `r_index` is not an integer
- Runtime error if the index is negative or not below the capacity

#### SETELEM - Replace Array Element
```assembly
SETELEM r_array r_index r_value
```
Store `r_value` at an existing index. Unlike `SETINDEX` it never grows the
array: the index must be below the current **length**, whatever the capacity.
The compiler uses it for `xs[i] = v`, so that writing past the end is an error
instead of silently extending the array.

**Error Conditions:**
- Runtime error if `r_array` is not an array
- Runtime error if `r_index` is not an integer
- Runtime error if the index is negative or not below the length

#### CLOSEUP - Detach Closures from a Register
```assembly
CLOSEUP r
```
Close the open upvalue for register `r`, if a closure captured it: the closure
keeps the value the register has now and no longer follows it. Has no effect
on a register nothing captured. The compiler emits it at the end of each loop
iteration for the body's captured variables; `RET` does the same for all
registers of the returning function.

```assembly
LOADI r1 10
NEWFUNC r2 reader
CAPLOCAL r2 0 r1    ; reader follows r1
CLOSEUP r1          ; reader keeps 10
LOADI r1 20         ; not seen by reader
```

#### TRY.L, ENDTRY, RETHROW - Cleanup When Something Fails
```assembly
TRY.L handler
ENDTRY
RETHROW
```
What `defer` is compiled to. `TRY.L label` leaves the label with the VM and
goes on with the next instruction: from here, if anything fails in this call
or in a call it makes, the calls made since are dropped and this one
continues at the label, with the failure put aside. `ENDTRY` takes the last
handler of the call back: the code it covered is over. `RETHROW` ends a
handler by passing the failure on, to the next handler or out of the program.

`TRY` is always written long (`LONG`, `TRY32`, offset word), like `JMP.L`.

```assembly
.func careful 3 0
  TRY.L on_failure
  NEWFUNC r0 risky
  CALL r0 0
  ENDTRY                ; nothing failed: the handler is not needed
  ; ... the cleanup ...
  RET r0
on_failure:
  ; ... the same cleanup ...
  RETHROW
.endfunc
```

**Error Conditions:**
- Runtime error for `ENDTRY` when the call has no handler left
- Runtime error for `RETHROW` when no failure is being handled

#### ARRPUSH - Append to Array
```assembly
ARRPUSH r_array r_value
```
Append `r_value` to the end of `r_array`. The array grows as needed; this is
the only way to extend an array past its initial capacity.

**Error Conditions:**
- Runtime error if `r_array` is not an array

#### ARRLEN - Get Length
```assembly
ARRLEN r_dest r_value
```
Get the length of an array or a string.

**Arguments:**
- `r_dest`: destination register for the length (as integer)
- `r_value`: register containing an array, a string or an object

**Behavior:**
- Array: the number of elements
- String: the number of characters (Unicode code points)
- Object: the value of its field named `length`, or null if it has none. The
  compiler does not rely on this: it emits `ARRLEN` only for calls of
  `arrays.len` and `strings.len`, and reads a struct's field named `length`
  as it reads any field

**Example:**
```assembly
NEWARRAY r0 5     ; Create array
LOADK r1 0
LOADK r2 1
SETINDEX r0 r1 r2 ; Add one element

ARRLEN r3 r0      ; r3 = 1 (length)
```

**Error Conditions:**
- Runtime error if `r_value` is not an array, a string or an object

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
  1: 30i


.func main 4 0
  NEWOBJ r0         ; Create object
  LOADK r2 1        ; r2 = 30
  SETFIELD r0 0 r2  ; obj["name"] = 30
  
  GETFIELD r3 r0 0  ; r3 = obj["name"] (30)
.endfunc
```

**Error Conditions:**
- Runtime error if `r_object` is not an object (`GETFIELD requires object, got array`)
- Runtime error if constant at `const_index` is not a string constant

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


.func main 3 0
  NEWOBJ r0         ; Create object
  LOADK r1 2        ; r1 = 100
  SETFIELD r0 0 r1  ; obj["x"] = 100
  
  LOADK r2 2        ; r2 = 100
  SETFIELD r0 1 r2  ; obj["y"] = 100
.endfunc
```

**Error Conditions:**
- Runtime error if `r_object` is not an object
- Runtime error if constant at `const_index` is not a string constant

#### HASFIELD - Check Object Field Presence
```assembly
HASFIELD r_dest r_object const_index
```
Store a boolean indicating whether the object has a field with the given name.

**Error Conditions:**
- Runtime error if `r_object` is not an object
- Runtime error if constant at `const_index` is not a string constant

#### DELFIELD - Delete Object Field
```assembly
DELFIELD r_dest r_object const_index
```
Delete a field from an object, storing `true` in `r_dest` if it existed, `false` otherwise.

**Error Conditions:**
- Runtime error if `r_object` is not an object
- Runtime error if constant at `const_index` is not a string constant

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

A short jump holds its target in one operand, so it reaches the first 65536 instructions of a program. For targets beyond that, long instruction variants are available. These use a `.L` suffix and support 32-bit signed offsets, allowing jumps to any location in the code.

**Long Instruction Format:**
Long instructions are encoded as 3 consecutive instruction words:
1. `LONG` prefix (indicates the next instruction uses a 32-bit offset)
2. Opcode instruction (JMP32, JMPIF32, JMPIFNOT32)
3. 32-bit offset word (signed, relative to position after the 3-word block)

**Supported Long Instructions:**
```assembly
JMP.L label              ; Long unconditional jump
JMPIF.L r_cond label     ; Long conditional jump if true
JMPIFNOT.L r_cond label  ; Long conditional jump if false
```

**When to Use Long Instructions:**
- When the target label is past instruction 65535
- The assembler will report an error if a short jump is out of range
- Long instructions take 3× the space of short instructions (three instruction words instead of one)

**Example:**
```assembly
.func main 2 0
  LOADK r0 0
  JMP.L far_away_label  ; Assembled as: LONG + JMP32 + offset

  ; ... many instructions ...

far_away_label:
  LOADK r1 1
  JMP.L back_to_start   ; Can jump backwards too

back_to_start:
  RET r0
.endfunc
```

**Implementation Details:**
- Offset calculation: `target_address - (current_position + 3)`
- Offsets are signed 32-bit integers (range: -2,147,483,648 to 2,147,483,647)
- The VM automatically handles the 3-word sequence when encountering a LONG prefix

## Instructions Only the Compiler Writes

A listing made from a Lumen program (`lumen build program.lum program.lasm`) shows instructions the assembler does not read. Each says in one step what several of the instructions above say, and is written by the compiler where it can ([bytecode_generation.md](compilation/bytecode_generation.md#one-instruction-for-several)):

| Instruction | Does |
|---|---|
| `CALLK rW n function` | Calls a function of the program by its name, with the `n` arguments in `rW+1`...; no function value is made |
| `CALLX rW n function` | The same for a host function |
| `JNLT ra rb target`, `JNLE`, `JNEQ`, `JNNE` | Goes on at `target` unless `ra < rb` (`<=`, `==`, `!=`) |
| `JNLTI ra k target`, `JNLEI`, `JNGTI`, `JNGEI`, `JNEQI`, `JNNEI` | The same against the number `k` (0 to 65535) |
| `ADDI rd ra k`, `SUBI rd ra k` | `rd = ra + k`, `rd = ra - k` |
| `CALLM rW n name`, `GETMETHOD rd ra name` | Calls the method `name` of the value in `rW+1`, found by the type the value carries; the second gives the method as a value |
| `SWITCH ra n else`, then `n` lines `CASE name target` | Goes on at the target of the case naming the type of the value in `ra`, or at `else` |
| `ONCE rd function` | The value of a constant that is a struct, an array or a value of an enum: what `function` returns. It is run the first time and never again; what it returned is frozen, with all it holds, and every write to a frozen value is refused |
| `ISTYPE rd ra name`, `JNTYPE ra name target` | Whether the value in `ra` is of the struct or variant `name` (a constant); the second goes on at `target` unless it is |

In assembly, write the instructions they stand for: `NEWFUNC` and `CALL`, a comparison and `JMPIFNOT`, `LOADI` and `ADD`, a call of `__is`.

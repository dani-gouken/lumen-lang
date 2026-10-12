# luvm - Lumen Virtual Machine Specification

## Goals

luvm is designed to be:

- **Fast**: Register-based execution with minimal overhead
- **Compact**: NaN-boxed values reduce memory footprint
- **Simple**: Clear execution model, easy to debug and optimize
- **Safe**: Automatic memory management via garbage collection

## Value Representation

luvm uses 64-bit NaN-boxing to represent all values uniformly.

### NaN-Boxing Layout

A 64-bit IEEE 754 double has this structure:

```
[sign:1][exponent:11][mantissa:52]
```

When all exponent bits are set (0x7FF), the value is NaN. We exploit this:

- If the value is a normal double (not NaN), it represents a **Number**
- If it's a NaN, we use the mantissa bits to encode type and payload:

```
[0][11111111111][tttt pppppppppppppppppppppppppppppppppppppppppppppp]
 ^       ^        ^                       ^
 sign  exp=0x7FF  tag (4 bits)          payload (48 bits)
```

### Tag Encoding

| Tag | Type     | Payload                              |
|-----|----------|--------------------------------------|
| 0x0 | (Number) | infinity and NaN: floats that have this exponent themselves |
| 0x1 | Bool     | 0 = false, 1 = true                  |
| 0x2 | Int      | 48-bit signed integer                |
| 0x3 | HeapPtr  | 48-bit pointer to heap object        |
| 0x4 | Null     | (unused)                             |

Tag 0 is deliberately not given to a boxed kind. `+Inf` is the bit pattern
`0x7FF0000000000000`, exponent all ones and everything else zero; if null had
tag 0 it would be the same pattern, and a float overflowing to infinity would
turn into null. NaN is stored as one canonical pattern with tag 0.

### Bit Masks

```go
expMask     = 0x7FF0000000000000  // exponent bits
mantMask    = 0x000FFFFFFFFFFFFF  // mantissa bits
tagMask     = 0x000F000000000000  // tag nibble (high 4 bits of mantissa)
payloadMask = 0x0000FFFFFFFFFFFF  // payload (low 48 bits)
```

The tag occupies bits 48-51 and the payload bits 0-47, so they never overlap.
A full 48-bit payload matters for heap pointers: the Go runtime may place its
heap anywhere in the 48-bit address space, and a narrower payload would
truncate the pointer.

## VM Execution Model

### Registers and Stack

luvm is a **register and stack hybrid**:

- **Registers**: Local variables and temporaries stored in a register array
- **Stack**: Call frames pushed onto a stack for function invocation
- **FP (Frame Pointer)**: Points to the base of the current call frame
- **SP (Stack Pointer)**: Points to the top of the stack
- **IP (Instruction Pointer)**: Points to the next instruction to execute

Each call frame contains:

```
[saved FP][saved IP][register array...]
```

### Execution Loop

1. Fetch instruction at IP
2. Decode opcode and operands (A, B, C)
3. Execute operation
4. Advance IP
5. Repeat

## Chunk Structure

A **Chunk** represents a compiled function:

```go
type Chunk struct {
    Constants     []Value  // constant pool
    Code          []Instr  // bytecode instructions
    RegisterCount int      // number of registers needed
    ArgCount      int      // number of arguments
}
```

## Opcode Families

### Data Movement

- `MOV r_dest r_src` - Copy value between registers
- `LOADK r_dest const_idx` - Load constant to register
- `LOADNULL r_dest` - Load null
- `LOADBOOL r_dest value` - Load boolean

### Arithmetic

- `ADD r_dest r_a r_b` - Addition, or concatenation of two strings
- `SUB r_dest r_a r_b` - Subtraction
- `MUL r_dest r_a r_b` - Multiplication
- `DIV r_dest r_a r_b` - Division
- `MOD r_dest r_a r_b` - Remainder
- `NEG r_dest r_a` - Negation
- `NOT r_dest r_a` - Boolean negation

Both operands must be of the same kind (two Ints or two Numbers); there is no
implicit conversion. Int division and remainder by zero raise a runtime error,
and so does an Int result outside the 48-bit range (`integer overflow`): Ints
never wrap around. Number arithmetic follows IEEE 754, including infinity and
NaN.

### Comparison

- `EQ r_dest r_a r_b` - Equal
- `NE r_dest r_a r_b` - Not equal
- `LT r_dest r_a r_b` - Less than
- `LE r_dest r_a r_b` - Less than or equal
- `GT r_dest r_a r_b` - Greater than
- `GE r_dest r_a r_b` - Greater than or equal

`EQ`/`NE` compare strings by content and every other heap object by identity.
The ordering instructions accept two Ints, two Numbers or two Strings.

### Heap Objects

- `NEWSTR r_dest const_idx` - Create a string from a string constant
- `NEWARRAY r_dest capacity` - Create an empty array
- `NEWOBJ r_dest [const_idx 1]` - Create an empty object (used for struct values). The compiler passes the struct's name, which is recorded on the object
- `GETINDEX r_dest r_arr r_idx` / `SETINDEX r_arr r_idx r_val` - Element access
- `SETELEM r_arr r_idx r_val` - Replace an existing element; never grows the array. What the compiler emits for `xs[i] = v`
- `ARRPUSH r_arr r_val` - Append to an array
- `ARRLEN r_dest r_val` - Length of an array or string
- `GETFIELD r_dest r_obj const_idx` / `SETFIELD r_obj const_idx r_val` - Field access
- `HASFIELD` / `DELFIELD` - Field presence and removal

These instructions check the type of their operand and report a runtime error
instead of reinterpreting memory: indexing a number, reading a field of an
array, or going past the end of an array all stop the program with a message.
See [assembly.md](assembly.md) for the exact rules of each instruction.

### Control Flow

- `JMP offset` - Unconditional jump
- `JMPIF r_cond offset` - Jump if true
- `JMPIFNOT r_cond offset` - Jump if false
- `LONG` + `JMP32` / `JMPIF32` / `JMPIFNOT32` + offset word - The same jumps
  with a 32-bit relative offset, for targets a one-byte address cannot reach.
  The compiler always emits this form.

### Function Calls

### Function Calls & Closures

- `NEWFUNC r_dest func_name` - Create a function object (or closure) from a prototype.
- `NEWEXFUNC r_dest extern_name` - Create an external function object.
- `CALL r_func nargs` - Call a function object in `r_func` with `nargs` arguments.
- `RET r_val` - Return from function with value.
- `CAPLOCAL r_func upval_idx local_reg` - Capture a local variable into a closure's upvalue slot.
- `CAPUPVAL r_func upval_idx parent_upval_idx` - Capture an existing upvalue into a closure's upvalue slot.
- `LDCAP r_dest upval_idx` - Load value from an upvalue.
- `STCAP upval_idx r_src` - Store value to an upvalue.

## Host Functions

A chunk lists the host functions it uses by name (`ExternNames`). `NEWEXFUNC`
turns a name into a callable object, and `CALL` invokes it like any other
function. The VM registers these by default:

| Name       | Arguments        | Result  | Description                                   |
|------------|------------------|---------|-----------------------------------------------|
| `print`    | any number       | null    | Writes the values separated by spaces         |
| `println`  | any number       | null    | Same, followed by a newline                   |
| `toString` | 1                | String  | The text `print` would write for the value    |
| `push`     | array, value     | null    | Appends the value to the array                |
| `__is`     | value, descriptor | Bool | Implements `value is Type`: whether the value matches the descriptor |
| `__cast`   | value, descriptor | the value | Implements `value as Type`: fails unless the value matches the descriptor (`int`, `[int]`, `?string`, `{Circle|Square}`, `{Box:value=int}`, ...; the grammar is at the top of `internal/vm/cast.go`). Arrays are checked element by element. Emitted by the compiler; not callable from Lumen source |

Values are written the way a program shows them: `42`, `2.5`, `true`, `null`,
the text of a string, `[1, "a"]` for arrays and `{x: 1, y: 2}` for objects
(fields sorted by name, function-valued fields omitted). This is
`vm.FormatValue`; `Value.String()` remains the debug form (`Int(42)`).

Output goes to standard output unless redirected with `VM.SetOutput`, which is
how tests capture it. More functions can be added with `VM.RegisterExtern`, or
with `VM.RegisterTypedExtern` to give the function a type
(`"fn(any)->string"`); only a typed function can be cast to a function type.

### Function signatures

Each function prototype carries a `Signature`: the function's type as a cast
descriptor, e.g. `fn(int,string)->bool`. The compiler fills it in; functions
written in assembly have none. It is the only type information kept at
runtime, and exists so that `value as fn(Int) -> Int` and
`value is fn(Int) -> Int` can be answered.

### Bytecode file version

`.lc` files start with a format version, currently **3** (1 predates the
48-bit value payload, 2 the tag of null). A file of another version
is refused with `unsupported bytecode version` rather than misread; rebuild it
from source.

## Operand Limits

Every operand is one byte, which bounds what a single function or chunk can
address:

| What                              | Limit | Reported by the compiler as |
|-----------------------------------|-------|-----------------------------|
| Registers per function            | 256   | `E6001`                     |
| Distinct constants per chunk      | 256   | `E6002`                     |
| Functions per chunk               | 256   | `E6003`                     |
| Arguments per call                | bounded by the register window | `E6007` |

Strings have no length limit of their own: a string constant stores an index
into the string pool, and bytecode files encode string lengths in 32 bits.
What counts against the constant limit is the number of *distinct* strings,
field names, floats and integers outside 0-255.

## Instruction Encoding

Instructions are fixed-width 32-bit values:

```
[opcode:8][A:8][B:8][C:8]
```

- **opcode**: Operation code
- **A**: First operand (usually destination register)
- **B**: Second operand
- **C**: Third operand

## Memory Management

### Garbage Collection

luvm uses **mark-sweep** garbage collection:

1. **Mark Phase**: Starting from roots (registers, stack), recursively mark all reachable objects
2. **Sweep Phase**: Iterate through heap, free unmarked objects

GC triggers when heap allocation exceeds a threshold.

### Heap Object Header

Every heap-allocated object begins with:

```go
type ObjHeader struct {
    Type   ObjType  // object type (string, array, closure, etc.)
    Marked bool     // GC mark bit
    Next   *Obj     // linked list for GC
}
```

Object types:

- `StringObj` - Immutable string
- `ArrayObj` - Dynamic array
- `ObjectObj` - Field map (struct values)
- `FunctionObj` - Function closure
- `ExternFunctionObj` - Host function
- `UpvalueObj` - Captured variable

## Example Execution

Given this bytecode:

```
LOADK r0 0      ; load constant 0 (value: 10) into r0
LOADK r1 1      ; load constant 1 (value: 20) into r1
ADD r2 r0 r1    ; r2 = r0 + r1
RET r2          ; return r2
```

Execution:

1. r0 ← 10
2. r1 ← 20
3. r2 ← 10 + 20 = 30
4. Return 30

## Design Decisions

- **Register-based**: Reduces instruction count vs stack-based VMs
- **NaN-boxing**: Efficient value representation without heap allocation
- **Simple ISA**: Easy to implement, optimize, and debug
- **Fixed-width instructions**: Fast decode, predictable performance

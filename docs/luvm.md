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
| 0x0 | Null     | (unused)                             |
| 0x1 | Bool     | 0 = false, 1 = true                  |
| 0x2 | Int      | 48-bit signed integer                |
| 0x3 | HeapPtr  | 48-bit pointer to heap object        |

### Bit Masks

```go
expMask     = 0x7FF0000000000000  // exponent bits
mantMask    = 0x000FFFFFFFFFFFFF  // mantissa bits
tagMask     = 0x0000F00000000000  // tag nibble (high 4 bits of mantissa)
payloadMask = 0x0000FFFFFFFFFFFF  // payload (low 48 bits)
```

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

- `ADD r_dest r_a r_b` - Addition
- `SUB r_dest r_a r_b` - Subtraction
- `MUL r_dest r_a r_b` - Multiplication
- `DIV r_dest r_a r_b` - Division

### Comparison

- `EQ r_dest r_a r_b` - Equal
- `NE r_dest r_a r_b` - Not equal
- `LT r_dest r_a r_b` - Less than
- `LE r_dest r_a r_b` - Less than or equal
- `GT r_dest r_a r_b` - Greater than
- `GE r_dest r_a r_b` - Greater than or equal

### Control Flow

- `JMP offset` - Unconditional jump
- `JMPIF r_cond offset` - Jump if true
- `JMPIFNOT r_cond offset` - Jump if false

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
- `FunctionObj` - Function closure
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

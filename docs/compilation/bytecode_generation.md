# Bytecode Generation Specification

This document describes the last stage of the [compilation pipeline](pipeline.md): lowering [Lumen IR](ir_generation.md) to a luvm chunk that the VM can execute or that can be written to a `.lc` file.

The implementation is in `internal/codegen`. The instruction set it targets is specified in [luvm.md](../luvm.md) and [assembly.md](../assembly.md).

## Objective

*   Map the IR's unlimited virtual registers onto the VM's registers.
*   Lay out basic blocks and turn block references into jumps.
*   Build the chunk's tables: constants, strings, functions, host functions.
*   Record where each instruction came from, for runtime errors and the debugger.
*   Fail with a clear error, never with a corrupt chunk, when a program exceeds what the bytecode format can express.

## Usage

```go
gen := codegen.NewGenerator(irProgram, []string{"print", "println", "toString", "push"}, errs)
gen.SetSourceLines(lines)
chunk := gen.Generate()

if errs.HasErrors() {
    // chunk must not be executed
}
```

The second argument lists the host functions the program may reference. A function reference that is neither defined in the IR nor in this list is an error (`E6005`), so a typo cannot turn into a failure at runtime.

## Constraints of the Target

Three properties of the VM shape most of this stage.

1.  **Operands are one byte.** A function can address 256 registers, a chunk 256 constants and 256 functions.
2.  **`CALL` uses consecutive registers.** The callee must be in some register `rN` and its arguments in `rN+1`, `rN+2`, ...
3.  **Every call returns its result in `r0` of the caller**, and the callee receives its arguments in `r0`, `r1`, ... of a fresh frame.

Point 3 means `r0` is overwritten by every call, including calls to host functions. Nothing that must survive a call may live there, and that includes the function's own first argument.

## Register Allocation

### Frame layout

```
r0              result of the last call (and the first argument, on entry)
r1 .. rN-1      the remaining arguments, where the caller put them
permanents      values that live across basic blocks
temporaries     values used within one block; slots are reused
call window     callee and arguments of the call being made
```

*   **The first argument is moved out of `r0`** by a `MOV` at function entry, into a permanent register. If the function never uses it, the move is omitted.
*   **The other arguments stay** in `r1..rN-1`; those are their registers for the whole function.
*   **A function without parameters** still leaves `r0` unused.

### Permanent or temporary?

A virtual register is a **temporary** when all of these hold:

*   it is written exactly once;
*   every read is in the same basic block as the write, and after it;
*   it is not captured by a closure;
*   it is not a parameter.

Everything else is **permanent** and keeps one physical register for the whole function. That covers variables that are reassigned, anything read in another block (loop counters, values used after an `if`), and captured variables.

This classification needs no dataflow analysis: a block has no jumps into its middle, so a value that is written and read within one block is dead at the block's end by construction.

### Reusing temporaries

Blocks are walked in order with a free list of temporary slots. At each instruction:

1.  operands that are temporaries and are read here for the last time release their slot;
2.  if the result is a temporary, it takes a slot from the free list, or a new one if the list is empty.

Because operands are released before the result is placed, an instruction can write its result into a register it just read (`ADD r3 r3 r4`). The VM reads all operands before it writes.

Expression-heavy code needs very few registers this way: a chain of 400 additions compiles into a handful (covered by `TestGenerateReusesTemporaryRegisters`).

**Captured variables are never reused.** `CAPLOCAL` makes the closure point at the register itself (see [closure-spec.md](../closure-spec.md)), so that register must keep holding that variable. Marking every operand of `MAKE_CLOSURE` as permanent guarantees it.

### The call window

One block of registers above all others is reserved for calls. For `CALL fn, a, b`:

```
MOV  rW   fn        ; callee
MOV  rW+1 a         ; arguments
MOV  rW+2 b
CALL rW   2
MOV  dest r0        ; fetch the result
```

Calls never nest at this level — arguments are already computed into registers before the sequence starts — so a single window is enough. Its size is one more than the largest argument count in the function.

The function's `RegisterCount` is `permanents + temporaries + window`. If it exceeds 256 the function is rejected with `E6001`.

## Control Flow

### Reachability

Only blocks reachable from the entry block are emitted. IR generation leaves empty or dead blocks after `return`, `break` and `continue`; dropping them here keeps that stage simple.

Within a block, instructions after the first terminator are ignored for the same reason.

### Jumps

Blocks are emitted in their IR order. A jump is always the three-word long form with a 32-bit relative offset:

```
LONG
JMP32 | JMPIFNOT32 r_cond
<offset word>
```

The short `JMP`/`JMPIF` instructions hold an absolute address in one byte, which only reaches the first 256 instructions of a chunk; using the long form everywhere avoids choosing per jump. Offsets are patched once all blocks of the function are placed.

| IR | Bytecode |
|----|----------|
| `JMP target` | long jump, **omitted** when `target` is the next block |
| `BR cond, then, else` | `JMPIFNOT32 cond → else`, then a jump to `then` unless it is the next block |
| `RET value` | `RET r` |
| `RET` | `LOADNULL r0`, `RET r0` |

A reachable block that does not end in a terminator gets `LOADNULL r0; RET r0`, so execution can never run into whatever follows it.

## Instruction Selection

| IR | Bytecode |
|----|----------|
| `MOV` from a register | `MOV` (nothing if source and destination coincide) |
| `MOV` of `Null` / `Bool` | `LOADNULL` / `LOADBOOL` |
| `MOV` of an `Int` in 0..255 | `LOADI` (no constant slot used) |
| `MOV` of any other `Int`, or a `Float` | `LOADK` from the constant pool |
| `MOV` of a `String` | `NEWSTR` (allocates a string object) |
| `MOV` of a `FuncRef` | `NEWFUNC` for a compiled function, `NEWEXFUNC` for a host function |
| `ADD` `SUB` `MUL` `DIV` `MOD` `NEG` `NOT` | the instruction of the same name |
| `EQ` `NEQ` `LT` `LTE` `GT` `GTE` | `EQ` `NE` `LT` `LE` `GT` `GE` |
| `NEW_STRUCT` | `NEWOBJ` with the struct's name as a string constant, which casts check |
| `GET_FIELD` / `SET_FIELD` | `GETFIELD` / `SETFIELD` with the field name as a string constant |
| `NEW_ARRAY` | `NEWARRAY` (capacity capped at 255; it is only a hint) |
| `GET_INDEX` / `LEN` / `PUSH` | `GETINDEX` / `ARRLEN` / `ARRPUSH` |
| `SET_INDEX` | `SETELEM`, which refuses an index at or past the length (`SETINDEX` would grow the array up to its capacity) |
| `MAKE_CLOSURE` | `NEWFUNC`, then one `CAPLOCAL` (register capture) or `CAPUPVAL` (upvalue capture) per captured variable |
| `LOAD_UPVALUE` / `STORE_UPVALUE` | `LDCAP` / `STCAP` |
| `CLOSE_UPVALUES` | one `CLOSEUP` per register |
| `AND` `OR` `CALL_TRAIT` | none: `E6004` |

## Chunk Tables

*   **Function table.** Prototype indices are assigned to all IR functions before any code is generated, so a function can refer to one defined later (mutual recursion, a lambda defined after its use). Each prototype records its name, code range, register count, argument count, upvalue count and its signature (the function's type as a cast descriptor, used when a function value is cast or tested).
*   **Constant pool.** Deduplicated by value: a constant used a hundred times takes one slot.
*   **String pool.** Deduplicated by text. A string constant in the constant pool holds the *index* of its text in the string pool; this is the representation `NEWSTR` and the field instructions expect, and it survives serialization (a heap pointer would not). A string literal and a field name with the same text share one entry.
*   **Host functions.** Names are added to `ExternNames` on first use.

### Limits

| Limit | Error | What counts |
|-------|-------|-------------|
| 256 registers per function | `E6001` | permanents + temporaries + call window |
| 256 constants per program | `E6002` | distinct floats, ints outside 0..255, strings and field names |
| 256 functions per program | `E6003` | functions, methods, lambdas and generic instances |
| call window | `E6007` | a call whose arguments would pass register 255 |

There is **no limit on the length of a string**: the constant stores an index, and `.lc` files encode string lengths in 32 bits. What is limited is how many *different* constants a program has.

These are properties of the bytecode format, not of this stage. Lifting them means widening operands in the VM (or adding extended instruction forms), after which only the `max...` constants here change.

## Debug Information

Each emitted instruction gets a `vm.DebugInfo` built from the IR instruction's position: file, line, column, the text of the source line and the function name. It is stored in the chunk and serialized with it.

Two consumers use it:

*   the VM, to say where a runtime error happened:
    ```text
    Runtime error: index out of bounds: 7 (length 3)
      at main.lum:3:17 in function 'pick'
            return items[index];
                        ^
    ```
*   the debugger, to show the source line of the instruction being stepped.

Instructions the generator adds on its own (the entry `MOV`, the call sequence) inherit the position of the IR instruction they belong to.

## Entry Point

The VM starts at the function named `main`. Generation reports `E6006` if there is none or if it takes parameters, since the VM calls it without arguments.

## Error Handling

Errors use the `E6xxx` range and go through the shared handler like every other stage; see [diagnosis.md](diagnosis.md). IR has no AST nodes, so positions come from the IR instruction being lowered.

`E6004` and `E6005` indicate a mismatch between this stage and the one before it (an IR operation or function reference it cannot handle) rather than a mistake in the user's program. They are still reported as diagnostics, with the source position, instead of panicking.

## Testing Strategy

`internal/codegen/codegen_test.go` builds small IR functions with `ir.Builder`, generates bytecode and **runs it**, asserting on the returned value. Asserting on behavior instead of on the instruction listing means the tests keep passing when the allocator or the instruction selection is improved.

They cover: each kind of constant; sharing of constants; branches and loops; arguments surviving calls that clobber `r0`; host function calls; closures sharing a variable by reference; arrays and structs; register reuse; dropped unreachable blocks; debug information; and one case per error code.

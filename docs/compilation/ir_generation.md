# IR Generation Specification

This document describes how the desugared AST is translated into Lumen IR, the last stage that works on the AST. It follows desugaring and closure analysis in the [compilation pipeline](pipeline.md); its output is consumed by [bytecode generation](bytecode_generation.md).

The IR data structures are in `internal/ir`, the translation in `internal/irgen`.

## The IR

An `ir.Program` is a list of functions. A function is a list of **basic blocks**; a block is a list of instructions that ends with a terminator (`RET`, `JMP` or `BR`).

```go
type Instruction struct {
    Op       OpType
    Dest     *Register        // result, or nil
    Operands []Value          // Register, Constant or *BasicBlock
    Pos      token.Position   // source position, for errors and debugging
}
```

*   **Registers are virtual and unlimited.** `%v1`, `%v2`, ... are numbered per function. Mapping them to the VM's registers is the backend's job.
*   **Registers are not single-assignment.** A variable keeps one register for its whole life and assignments write to it again. There are no phi nodes. This keeps generation direct; the backend recovers the lifetime information it needs.
*   **Constants** carry a type tag that tells the backend how to load them: `Int`, `Float`, `Bool`, `Null`, `String`, `FuncRef` (the name of a function), `Upvalue` (an upvalue index, only as a closure capture).

`lumen ir file.lum` prints it:

```
fn fib(%v1) {
b0:
	%v2 = MOV, 2
	%v3 = LT, %v1, %v2
	BR %v3, b1, b2
b1:
	RET %v1
b2:
	%v4 = MOV, "fib"
	...
```

### Operations

| Group | Operations |
|-------|------------|
| Values | `MOV` (from a register or a constant) |
| Arithmetic | `ADD` `SUB` `MUL` `DIV` `MOD` `NEG` |
| Logic and comparison | `NOT` `EQ` `NEQ` `LT` `LTE` `GT` `GTE` |
| Control flow | `JMP block`, `BR cond, then, else`, `RET [value]`, `CALL fn, args...` |
| Structs | `NEW_STRUCT name`, `GET_FIELD obj, name`, `SET_FIELD obj, name, value` |
| Arrays | `NEW_ARRAY capacity`, `GET_INDEX arr, i`, `SET_INDEX arr, i, value`, `LEN value`, `PUSH arr, value` |
| Closures | `MAKE_CLOSURE fn, captures...`, `LOAD_UPVALUE i`, `STORE_UPVALUE i, value` |

`AND`, `OR` and `CALL_TRAIT` exist in the instruction set but are not produced: short-circuit operators become branches and method calls become a field read plus a call. The backend rejects them (`E6004`).

## Functions

One IR function is generated for each:

| Source | IR name | Parameters |
|--------|---------|------------|
| Top-level `fn name` | `name` | as declared |
| Method `fn m` in `impl ... for S` or `impl S` | `S.m` | `self`, then as declared |
| Function expression | `lambda_N`, numbered in source order | as declared |

A lambda is emitted **before** the function that contains it. Generic functions that were instantiated appear under their mangled names (see [monomorphization.md](monomorphization.md)).

Every function ends with a `RET`; one without a value is added if control can fall off the end.

## Variables and Scopes

Each function compiler keeps a stack of scopes mapping names to registers; a block pushes one and pops it at its end. Lookup goes from the innermost scope outwards, so shadowing works and a name declared in a block is gone after it.

**`let` gives a variable its own register.** The initializer is compiled first. If its result is a temporary (the register was created by this expression), the variable simply adopts it. If the result is *another variable's* register, it is copied:

```lumen
let a = 1;      // %v1 = MOV, 1          a is %v1
let b = a;      // %v2 = MOV, %v1        b gets a copy, not a's register
b = 2;          // %v2 = MOV, 2          a is unaffected
```

Without the copy `b = 2` would overwrite `a`. The compiler tracks which registers belong to variables (`varRegs`) to tell the two cases apart.

**Identifiers** are looked up in this order:

1.  a local variable → its register;
2.  a captured variable → `LOAD_UPVALUE`;
3.  a top-level function or builtin → `MOV` of a `FuncRef`;
4.  otherwise `E5002`. Name resolution has already rejected unknown names, so this only catches names that are not values, such as a struct name used as an expression.

## Assignment

Assignments arrive as `BinaryExpr` with operator `"="`:

| Target | Generated |
|--------|-----------|
| local variable | `MOV` into the variable's register |
| captured variable | `STORE_UPVALUE` |
| `arr[i]` | `SET_INDEX arr, i, value` |
| `obj.field` | `SET_FIELD obj, "field", value` |
| anything else | `E5003` (for example assigning to a function) |

## Control Flow

`if`, `while` and `for` produce the usual block structure:

```
while cond { body }          for v in xs { body }

    JMP cond                     index = 0; length = LEN xs
cond:                            JMP cond
    BR c, body, exit         cond:
body:                            BR index < length, body, exit
    ...                      body:
    JMP cond                     v = GET_INDEX xs, index
exit:                            index = index + 1
                                 ...
                                 JMP cond
                             exit:
```

`for` loops are normally rewritten by desugaring before they get here; IR generation still compiles any that reach it as a real loop, with the increment before the body so `continue` cannot skip it.

`break` and `continue` jump to the exit and condition blocks of the innermost loop, tracked on a loop stack. Outside a loop they are `E5005`.

**Code after a terminator.** After `return`, `break` and `continue` the generator switches to a fresh block. Statements that follow (unreachable code) land there instead of after a terminator, which keeps every block well formed. Nothing jumps to these blocks and the backend drops them.

A `match` that reaches IR generation was not desugared, meaning its patterns are not supported; it is reported as `E5001` rather than compiled incorrectly.

## Short-Circuit Operators

`&&`, `||` and `??` evaluate their right operand only when needed. The result lives in one register that is written on both paths:

```
a && b                       a ?? b

    result = a                   result = a
    BR result, rhs, end          isNull = EQ result, null
rhs:                             BR isNull, rhs, end
    result = b               rhs:
    JMP end                      result = b
end:                             JMP end
                             end:
```

`||` is the same as `&&` with the branch targets swapped. Desugaring handles the operators in `if`/`while` conditions and `let` initializers itself; this lowering covers every other position (return values, arguments, assignments, nested operands), so the semantics are the same everywhere.

## Structs and Methods

A struct literal becomes an object with its fields set **by name**:

```lumen
Point { x: 1, y: 2 }
```
```
%v3 = NEW_STRUCT, "Point"
SET_FIELD %v3, "x", %v1
SET_FIELD %v3, "y", %v2
%v4 = MOV, "Point.sum"          ; one pair per method of Point
SET_FIELD %v3, "sum", %v4
```

Field values are evaluated in alphabetical order of the field names, which makes the output deterministic (the AST stores fields in a map).

**Methods are stored on the value.** After the fields, every method implemented for the struct is attached as a function-valued field under the method's name. A method call then needs no type information:

```lumen
p.sum()
```
```
%v5 = GET_FIELD, %v3, "sum"     ; look the method up on the receiver
%v6 = CALL, %v5, %v3            ; pass the receiver as self
```

`x.name(args)` is compiled as a method call when `name` is a method of *any* struct in the program, and as "read field, call the function in it" otherwise. To keep that decision unambiguous, a name may not be both a method and a struct field (`E5004`).

This gives dynamic dispatch for free: a function taking a trait-typed parameter works with any struct that has the method. Its costs are stated plainly:

*   every struct value carries one function object per method, created when the value is created;
*   calling a method the value does not have is found at runtime (`CALL requires function object`), not at compile time.

A method table shared by all values of a struct would remove the first cost; that needs VM support and is left for later.

## Calls and Variadic Functions

A call compiles the callee and the arguments to registers and emits `CALL fn, args...`.

When the callee is the **name of a user function whose last parameter is variadic**, the arguments beyond the fixed ones are packed into an array:

```lumen
fn sum(first: Int, rest: ...Int) -> Int { ... }
sum(1, 2, 3)
```
```
%v5 = NEW_ARRAY, 2
PUSH %v5, %v3
PUSH %v5, %v4
%v6 = CALL, %v1, %v2, %v5        ; sum(1, [2, 3])
```

A call with no extra arguments passes an empty array. Packing depends on knowing the callee's declaration, so it happens only for direct calls by name; a variadic function called through a variable receives its arguments as written. The builtins (`print`, `println`) are variadic in the VM itself and are never packed.

## Closures

Closure analysis has determined, for each function, the ordered list of variables it captures. That order is the function's upvalue numbering.

Inside the function, a captured variable is read with `LOAD_UPVALUE i` and written with `STORE_UPVALUE i`.

Where the function expression appears, the enclosing function builds the closure:

```
%v3 = MAKE_CLOSURE, "lambda_0", %v1, Upvalue(0)
```

Each capture operand says where the enclosing function finds the variable:

*   a **register**: it is one of the enclosing function's own locals;
*   an **`Upvalue` constant**: the enclosing function captured it itself and passes it on by index.

A lambda that captures nothing is just a `FuncRef`. Captures are by reference; the runtime mechanics are in [closure-spec.md](../closure-spec.md).

## Source Positions

Before compiling each statement and expression the generator sets the builder's current position, and every instruction emitted is stamped with it. Positions of operations (the `+` of an addition, the `[` of an index) are restored after their operands are compiled, so a runtime error points at the operation that failed rather than at its last operand.

## Error Handling

Errors use the `E5xxx` range; see [diagnosis.md](diagnosis.md).

| Code | Raised when |
|------|-------------|
| `E5001` | A construct cannot be lowered (an undesugared `match`, an unknown operator or node) |
| `E5002` | A name is used as a value but is not a variable or function |
| `E5003` | The target of an assignment cannot be written |
| `E5004` | A name is both a struct field and a method |
| `E5005` | `break` or `continue` outside of a loop |

## Testing Strategy

`internal/irgen/source_test.go` compiles snippets and checks the printed IR for the instructions that carry each rule above: the copy on `let b = a`, the branch around a short-circuit operand, fields set by name, the receiver passed as `self`, the packed variadic array, the capture operands of nested closures. The older `irgen_test.go` and `lambda_test.go` build ASTs by hand and check structure (function count, block count).

Behavior is verified end to end by the feature tests in `internal/compiler`, which run the programs.

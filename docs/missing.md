# What's Missing

An inventory of what Lumen does not do yet: language features, checks the compiler skips, runtime limits and known defects. It complements the [implementation status table](language-spec.md#13-implementation-status), which lists what works.

Every entry was checked against the current implementation. "Today" shows what actually happens when you try, because the failure mode matters: a clear compile error is a gap, a silently wrong result is a defect.

Sizes are rough: **S** is an afternoon in one or two packages, **M** touches several pipeline stages, **L** needs design work first.

## Summary

| Area | Most useful next steps |
|------|------------------------|
| Everyday syntax | `+=` / `++`, index in `for`, ranges, sized arrays |
| Correctness | array bounds on write, unknown struct fields, `const`, missing returns |
| Types | stricter checking where `Any` is used today, type argument inference |
| Runtime | heap size and accounting, recursion depth, integer overflow |
| Program structure | imports, enums, global constants |

---

## 1. Syntax and Language Features

### Compound assignment and increment — S

`i += 1`, `i -= 1`, `*=`, `/=`, `%=`, `i++`, `i--`.

**Today**: syntax error (`E0001: expected expression, found '='`). Write `i = i + 1;`.

**Notes**: Needs new tokens, then one of two designs. Rewriting to `i = i + 1` in the parser is the smallest change but evaluates the target twice, which is wrong for `xs[next()] += 1`. Keeping the operator through to IR generation and evaluating the target once is the correct version. Making `++` a statement rather than an expression (as in Go) avoids the pre/post-increment question.

### Index in `for` loops — S

```lumen
for i, item in items { ... }
```

**Today**: only `for item in items`. Keep a counter by hand, and increment it *before* any `continue`.

**Notes**: The desugarer already maintains a hidden index (`__iter_N`) for every loop; this only gives it a name. Touches the parser, `ast.ForStmt`, the resolver, the checker and the desugarer.

### Ranges — M

```lumen
for i in 0..10 { ... }
```

**Today**: not parsed. Use `while` with a counter.

### Arrays with a size — S / L

Creating an array of `n` elements, and fixed-size array types.

**Today**: arrays are created from a literal and grow with `push`. There is no way to write "ten zeros" except a loop.

**Notes**: A builtin such as `array(n, value)` is small (S). A real fixed-size type `[Int; 10]` checked at compile time is a type system feature (L) and is of limited value until the checker is stricter.

### More builtins — S each

There is no standard library beyond `print`, `println`, `toString` and `push`. Commonly needed and absent:

| Missing | Purpose |
|---------|---------|
| `pop`, `insert`, `remove`, `slice`, `contains` | Arrays |
| `substring`, `split`, `join`, `indexOf`, `toUpper`, ... | Strings |
| `toInt`, `toFloat`, `parseInt` | Conversions. The VM has `TOINT`/`TONUM`; the language does not expose them, so an `Int` and a `Float` cannot be combined at all |
| `abs`, `min`, `max`, `sqrt`, ... | Math |
| reading input, files, time | I/O |

Adding one means a host function in `internal/vm/vm.go` and a declaration in `internal/resolve/builtins.go`.

### Imports and multi-file programs — L

**Today**: `import` parses and is rejected with `E7001`. A program is one file.

**Notes**: Needs module resolution, a notion of exported names, the `module::name` syntax from the spec, and linking several files into one chunk.

### Enums — L

**Today**: `enum` parses and is rejected with `E7001`.

**Notes**: Needs a runtime representation for variants, construction syntax, and pattern matching with destructuring (`case Some(x):`).

### `match` beyond literals — M

**Today**: literal patterns and `default` work. Type patterns (`case Int:`) are rejected with `E7001`; the desugarer lowers them to an `__is_type` intrinsic that has no implementation. There are no ranges, no guards, no bindings, and `match` is a statement, not an expression.

### Global constants and variables — M

```lumen
const LIMIT = 100;      // at the top level
```

**Today**: syntax error; only `fn`, `struct`, `enum`, `trait`, `impl` are allowed at the top level.

### Nested and recursive local functions — S / M

*   `fn inner() { ... }` inside a function body is a syntax error. Use `let inner = fn() { ... };`.
*   A lambda cannot call itself: in `let f = fn(n: Int) -> Int { ... f(n - 1) ... };` the name `f` is not in scope inside the body (`E1001`). Use a top-level function for recursion.

### Trait default methods — M

**Today**: a trait method may have a body, but an `impl` that does not provide the method is rejected (`E2012`), so the default is never used.

### Bitwise operators — S

`&`, `|`, `^`, `<<`, `>>`. The tokens `&` and `|` are lexed and the VM has `BAND`, `BOR`, `BXOR`, `SHL`, `SHR`; the parser and the stages in between do not handle them.

### Smaller gaps

| Missing | Today |
|---------|-------|
| `%` on floats | `E2006`, although the VM implements it |
| Maps / dictionaries | No syntax; the VM's object type is only used for structs |
| String interpolation, multi-line strings, `\u` escapes | Not parsed |
| Optional chaining `a?.b` | Not parsed |
| Type aliases | Not parsed |
| Methods on builtin types (`"abc".upper()`) | `impl Trait for Int` parses, but calls are not dispatched |
| Struct field defaults | Not parsed |
| Tuples, destructuring `let (a, b) = ...` | Not parsed |

---

## 2. Checks the Compiler Does Not Make

These programs compile today and should not, or compile and misbehave.

### Unknown or missing struct fields — S

```lumen
struct P { x: Int; y: Int; }
let p = P { x: 1 };     // y is never set
println(p.y);           // null
println(p.nope);        // null
```

**Today**: no error in either case. A struct literal may omit fields, and reading a field that does not exist yields `null`. (A literal that names an *unknown* field is rejected, `E2014`.)

**Notes**: `checkStructInitExpr` and `checkMemberExpr` in `internal/types/checker.go` have the struct declaration at hand; both checks are short.

### `const` is not enforced — S

```lumen
const c = 1;
c = 2;                  // accepted
```

### Missing `return` — M

A function declared `-> Int` whose body falls off the end compiles and returns `null`. There is no "all paths return" analysis, and no warning for unreachable code after `return`.

### Partial static typing — L

Where the checker lacks information it uses `Any`, which is accepted everywhere and checked only when the program runs. This affects:

*   the result of every method call (`p.area()` is `Any`);
*   values whose type is a type parameter, and fields of generic structs;
*   optional types: `T?` is treated as `Any`, so `null` safety is not checked and `??` is not required;
*   values typed with a trait: whether the struct implements the trait is not verified at the call site.

A mistake in these places surfaces as a runtime error with a source location. Closing the gap is mostly work inside `internal/types`: method signatures per struct, real optional and trait types, and substituting type arguments instead of erasing them.

### Type arguments are never inferred — M

`first([1, 2])` is rejected (`E3001`); it must be written `first<Int>([1, 2])`. The same error is currently reported twice.

### Other unchecked things

| Not checked | Effect |
|-------------|--------|
| Unused variables, parameters, functions | No warning |
| A method name that is also a field of a *different* struct | Rejected (`E5004`) even though it would be unambiguous with type information |
| Calling a method the value does not have | Runtime error (`CALL requires function object`) |
| Variadic function called through a variable | Arguments are not packed into an array; only direct calls by name are |
| Shadowing a function with a local | Allowed silently |

---

## 3. Runtime and VM

### Writes past the end of an array can succeed — S (defect)

```lumen
let xs = [1, 2, 3];
push(xs, 4);
xs[5] = 9;              // no error
println(xs);            // [1, 2, 3, 4, null, 9]
```

**Why**: `SETINDEX` accepts any index below the array's *capacity*, and capacity doubles on append. Reads are checked against the length; writes are not.

**Notes**: The capacity rule is intentional for hand-written assembly (`NEWARRAY 5` then `SETINDEX` at 0..4) and VM tests rely on it. The fix is to have compiled code check against the length, either with a separate instruction or by emitting the check.

### Heap size and accounting — M

*   The heap is fixed at 1 MB and cannot be changed from the command line.
*   **Array growth is not counted.** An array's size is recorded when it is created; elements added with `push` do not count toward the limit or the GC trigger. A program can use far more memory than the limit suggests.
*   Allocation fails as soon as the limit would be exceeded, without first trying a collection.
*   The error names whichever allocation happened to hit the limit, e.g. `extern function allocation failed: heap exhausted`, which reads like a problem with the function rather than with memory.

### Recursion depth — S

The call stack is limited to 256 frames (`maxCallDepth`), so `down(1000)` fails with `stack overflow: max call depth 256 exceeded`. There is no tail-call optimization.

### Integer overflow wraps silently — S

Ints are 48-bit. Exceeding the range wraps around without an error:

```lumen
let big = 140737488355327;
println(big + 1);       // -140737488355328
```

Integer literals too large for 48 bits are truncated the same way.

### Method dispatch cost — M

Every struct value carries one function object per method, created when the value is created (see [ir_generation.md](compilation/ir_generation.md#structs-and-methods)). Correct, but wasteful for structs with many methods or programs creating many values. A method table shared per struct type needs VM support.

### Each call of a builtin allocates — S

`println(x)` compiles to `NEWEXFUNC` + `CALL`, creating a host function object every time it runs. In a hot loop this is the main source of garbage. Caching the object per function, or a dedicated call instruction, would remove it.

### Closures created in a loop share variables — M

A variable declared in a loop body has one register for all iterations, so closures created in different iterations all see its last value. Passing the value through a function parameter is the workaround.

### Bytecode format limits — L

Operands are one byte: 256 registers per function, 256 distinct constants and 256 functions per program. These are reported as compile errors (`E6001`–`E6003`), never as corruption, but a large program will reach the constant limit. Lifting them means wider operands or extended instruction forms in the VM.

### Smaller items

| Item | Detail |
|------|--------|
| `.lc` compatibility | Bytecode files carry a version but nothing bumps it; files built before a VM change may load and misbehave |
| GC roots and Go's GC | Heap objects are held as integers in registers and stay alive only through the VM's object list; correct today, fragile if Go ever moves objects |
| Equality | Arrays and structs compare by identity; there is no structural `==` |
| Float formatting | `println(3.0)` prints `3`, indistinguishable from an Int |

---

## 4. Tooling

| Missing | Today |
|---------|-------|
| REPL | `lumen repl` is a stub |
| Round-trip of compiled listings | `lumen build x.lum x.lasm` produces a listing that `lumen asm` cannot read, because long jumps are printed in an annotated form |
| Errors from several stages in one run | The pipeline stops at the first stage with errors, so type errors appear only after syntax errors are fixed |
| Machine-readable diagnostics | No `--format=json` |
| Source-level debugging | `lumdbg` steps bytecode instructions and shows registers by number; it has no statement stepping and no variable names |
| Compiler flags | No heap size, no optimization level, no warnings-as-errors |
| Formatter, language server, syntax highlighting | None |
| Optimization | None: no constant folding, no dead code elimination beyond unreachable blocks, every jump uses the 3-word long form |
| `Instr.String()` | Still renders every operand as a register; the debugger and disassembler use `asm.FormatInstruction` instead |

---

## 5. Suggested Order

A sequence that keeps each step small and makes the language noticeably more comfortable early:

1.  **Close the silent failures**: array writes past the length, unknown and missing struct fields, `const`. These make existing programs fail loudly instead of misbehaving.
2.  **Everyday syntax**: `+=` and `++`, index in `for`, `array(n, value)`, a first batch of string and array builtins, `toInt`/`toFloat`.
3.  **Runtime headroom**: count array growth, collect before failing an allocation, a clearer out-of-memory message, a configurable heap and call depth.
4.  **Stricter types**: method signatures, missing-return analysis, then optionals and trait types.
5.  **Program structure**: global constants, then imports, then enums with pattern matching.
6.  **Format limits**: wider operands, once programs are large enough to hit them.

When an item is done, remove it here and update the status table in the [language spec](language-spec.md#13-implementation-status).

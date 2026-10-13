# What's Missing

An inventory of what Lumen does not do yet: language features, checks the compiler skips, runtime limits and known defects. It complements the [implementation status table](language-spec.md#13-implementation-status), which lists what works.

Every entry was checked against the current implementation. "Today" shows what actually happens when you try, because the failure mode matters: a clear compile error is a gap, a silently wrong result is a defect.

Sizes are rough: **S** is an afternoon in one or two packages, **M** touches several pipeline stages, **L** needs design work first.

## Summary

| Area | Most useful next steps |
|------|------------------------|
| Everyday syntax | ranges, sized arrays, more builtins |
| Correctness | unused variables |
| Types | inference from the expected type, richer `match` patterns, array literals of mixed structs |
| Runtime | register allocation per call, method dispatch cost |
| Program structure | a standard library, a way to install packages, enums, global constants |

---

## 1. Syntax and Language Features

### Ranges — M

```lumen
for i in 0..10 { ... }
```

**Today**: not parsed. Use `while` with a counter.

### Arrays with a size — S / L

Creating an array of `n` elements, and fixed-size array types.

**Today**: arrays are created from a literal and grow with `push`. There is no way to write "ten zeros" except a loop.

**Notes**: A builtin such as `array(n, value)` is small (S). A real fixed-size type `[Int; 10]` checked at compile time is a type system feature (L).

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

### A standard library, and getting packages — M / L

**Today**: a program can be split into modules ([modules.md](modules.md)), and [`lumen.json`](project.md) can name folders of modules kept elsewhere (`"packages"`). That is all: no library ships with `lumen`, nothing downloads or versions a package, and a package cannot name its own dependencies; the program lists them all.

**Notes**: A standard library is a package whose folder `lumen` knows without being told (M, once there is something to put in it). Installing and versioning is a tool of its own (L).

### Gaps in modules — S each

| Missing | Today |
|---------|-------|
| Unused import warning | Silent |
| Per-field privacy | A `pub struct` exposes every field |
| `pub` signatures checked for private types | `pub fn f() -> Secret` compiles; the caller holds a value whose type it cannot name |
| Dead code removal | Every imported module ends up in the bytecode whole, used or not |
| Separate compilation | All modules are parsed and compiled on every run |
| Native functions declared in source (`extern fn`) | Host functions are a fixed list in the compiler |

### Enums — L

**Today**: `enum` parses and is rejected with `E7001`.

**Notes**: Needs a runtime representation for variants, construction syntax, and pattern matching with destructuring (`case Some(x):`).

### `match` beyond literals and types — M

**Today**: literal patterns, type patterns (`case Int:`, `case Point:`, `case [String]:`) and `default` work. There are no ranges, no guards, no bindings (`case x:`), no destructuring, and `match` is a statement, not an expression. A name that is not a type is rejected with `E7001`.

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

### Loop variables are read-only — by design

`for i, item in items` does not allow `i = ...` or `item = ...` (`E2026`). Neither would do what it looks like: the loop keeps its own position, and `item` is a copy of the element. Change the collection with `items[i] = ...`, and use `while` to control the position.

### `++` and `--` are statements only — by design

`i++;` and `i--;` are statements, short for `i += 1;` and `i -= 1;`. They have no value, so `let j = i++;` and `xs[i++]` are syntax errors, and there is no prefix form (`++i`). This avoids the pre/post-increment distinction.

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

### What flow typing does not cover — M

A test narrows a **local variable, parameter or field path** (`node.next`) inside the guarded region: `if x != null { ... }`, after `if x == null { return; }`, in a `while x != null` body, across `&&` and `||`, and for `x is T` and `match` on types. Some things are deliberately left out:

*   **a variable that a lambda assigns** is never narrowed: the lambda can run at any moment;
*   **inside a lambda**, nothing known about the enclosing function's variables carries over: the lambda may run later;
*   **fields are forgotten after anything that may assign a field**: a field assignment, or a call of a function that assigns one, directly or through what it calls. Calls that cannot (builtins, functions that only read) change nothing. A call whose target is not known counts as one that may: a function value, a function stored in a field, and a method if *any* method of that name assigns a field (the receiver's type is not used). Copy the field into a local to keep it across such calls;
*   **elements are not narrowed**: `if xs[0] != null` says nothing about the next `xs[0]`;
*   **the false branch of a type test** learns nothing: after `if v is Int { ... } else { ... }`, `v` is not "anything but Int";
*   **after a loop**: `while x == null { ... }` does not make `x` non-null afterwards;
*   **a variable assigned in a loop** is not narrowed inside that loop by a test made before it, even when everything assigned to it is non-null. Walking a linked structure needs a second variable of the non-optional type: `let node: Node = root;` after `root` was tested.

These are conservative choices, not holes. The third could be made more precise by tracking *which* fields a function assigns and by using the receiver's type for method calls.

### Inference has limits — M

Type arguments are inferred from the arguments of a call (`first([1, 2])`, `stack.map(toString)`) and, for what those leave open, from the type the result is expected to have (`let xs: [Int] = none();`, `return none();`, `size(none())`). Not covered:

*   a call whose result goes nowhere typed: `let xs = none();` and `none().length` still need `none<Int>()`;
*   the first argument that mentions a parameter decides it; there is no search for a type common to several arguments (`same(1, 2.5)` is an error, not `T = Any`);
*   inference does not look further than one call: in `first(none())`, nothing determines the inner call.

### Function casts cannot see through generics — inherent

A cast to a function type is checked against the signature recorded when the function was compiled. A generic function records its type parameters as `Any`, so a `fn id<T>(v: T) -> T` stored in an `Any` casts to `fn(Any) -> Any`, not to `fn(Int) -> Int`. Functions written in assembly have no recorded signature and cannot be cast to a function type at all.

### Array literals of mixed structs need a type — S

`let shapes = [rect, square];` is rejected, because the element type is taken from the first element. Stating the type works: `let shapes: [Shape] = [rect, square];`. Finding the common trait automatically would remove the need.

### Other unchecked things

| Not checked | Effect |
|-------------|--------|
| Unused variables, parameters, functions | No warning |
| Variadic function called through a variable | Arguments are not packed into an array; only direct calls by name are |
| Shadowing a function with a local | Allowed silently |

---

## 3. Runtime and VM

### Strings are not counted when they grow — S

`a + b` and `text += part` create a new string each time, and each is counted. But building a long string in a loop makes garbage proportional to the square of its length (every step copies everything so far). There is no string builder yet.

### Method dispatch cost — M

Calls on a value whose struct is known are direct. Calls through a trait look the method up on the value, so each value of a struct that implements traits carries one function object per trait method, created with the value (see [ir_generation.md](compilation/ir_generation.md#structs-and-methods)). A method table shared per struct type would remove that; it needs VM support.

### Every call allocates its registers — M

Function objects are reused, so calling a function by name allocates nothing for the function. Each call does still allocate a fresh register array for the callee; in call-heavy code that is the main cost left. A register stack shared by all frames would remove it.

### Smaller items

| Item | Detail |
|------|--------|
| `.lc` compatibility | Bytecode files carry a version (4 today) that is raised by hand when the format changes; a new opcode alone does not raise it |
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
| Compiler flags | `--heap-mb` and `--max-depth` exist; no optimization level, no warnings-as-errors |
| Formatter, language server, syntax highlighting | None |
| Optimization | None: no constant folding, no dead code elimination beyond unreachable blocks, every jump uses the 3-word long form |
| `Instr.String()` | Still renders every operand as a register; the debugger and disassembler use `asm.FormatInstruction` instead |

---

## 5. Suggested Order

A sequence that keeps each step small and makes the language noticeably more comfortable early:

1.  **Null operators**: `x!`, `a?.b`, and rejecting a value known to be null.
2.  **Everyday syntax**: ranges, `array(n, value)`, a first batch of string and array builtins, `toInt`/`toFloat`.
3.  **Native functions declared in source** (`extern fn`), then a standard library as ordinary modules.
4.  **Types**: inference from the expected result type, `match` as an expression with bindings and guards.
5.  **Program structure**: global constants, then enums with pattern matching.

When an item is done, remove it here and update the status table in the [language spec](language-spec.md#13-implementation-status).

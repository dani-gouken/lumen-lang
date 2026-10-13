# What's Missing

An inventory of what Lumen does not do yet: language features, checks the compiler skips, runtime limits and known defects. It complements the [implementation status table](language-spec.md#13-implementation-status), which lists what works.

Every entry was checked against the current implementation. "Today" shows what actually happens when you try, because the failure mode matters: a clear compile error is a gap, a silently wrong result is a defect.

Sizes are rough: **S** is an afternoon in one or two packages, **M** touches several pipeline stages, **L** needs design work first.

## Summary

| Area | Most useful next steps |
|------|------------------------|
| Everyday syntax | a pipe operator for chaining, ranges as values |
| Failures | one shape for all errors |
| Types | patterns on structs and arrays, names in a case with several patterns, traits for arrays |
| Correctness | unused variables |
| Runtime | method dispatch cost, tasks on several processors |
| Program structure | native code from outside (shared libraries), a way to install packages, variables outside functions |
| Tooling | a `lumen test` command, a formatter, renaming and finding uses in the editor |

---

## 1. Syntax and Language Features

### Ranges as values — M

```rust
let evens = (0..10).filter(isEven);
for i in (0..10).step(2) { ... }
```

**Today**: `for i in 0..10`, `0..=10` and `case 0..10:` work, and count without making an array. Anywhere else a range is refused (`E2038`): it has no type of its own, no step, and cannot be kept in a variable or passed to a function. `arrays.range(0, 10)` makes the array.

### Chaining calls — S

```rust
numbers |> arrays.filter(isEven) |> arrays.map(double) |> arrays.sum()
```

**Today**: calls nest, and read inside out: `arrays.sum(arrays.map(arrays.filter(numbers, isEven), double))`. The same goes for steps on optionals with `std.option`.

**Notes**: `x |> f(a)` meaning `f(x, a)` is one rewrite in the desugarer. It was preferred over giving optionals and arrays methods of their own, which would make the dot mean something other than "a field or method of a struct or enum you declared".

### Members on arrays and texts — by design

Arrays and texts have no members. `items.length` is an error (`E2033`); the length is `arrays.len(items)` or `strings.len(text)`, and everything else done to them is likewise a function of the standard library. A dot reaches a field or a method of a struct or an enum, and nothing else.

The cost is an `import std.arrays;` in almost every program. Calls of the two functions are compiled to the same single instruction `.length` was, so there is none at runtime.

### More for failures — S to M each

`Result`, `panic`, `attempt`, traces and `defer` are there ([errors.md](errors.md)). Not there:

| Missing | Today |
|---------|-------|
| `attempt` on a function with arguments | Wrap the call in a lambda |
| One shape for all errors | `io.Error`, `Failure` and your own each have `describe()` by convention. A trait for it can now be written and required (`<E: Describe>`); the library does not have one yet |
| Underlining exactly what failed | A runtime error underlines the name or number that starts where the instruction's source does; the end of the expression is not recorded |
| `Result` from the functions that parse | `strings.parseInt` returns an optional: the reason is always "not a number" |

### Arrays with a fixed size — L

**Today**: `arrays.fill(n, value)` from the [standard library](stdlib.md) makes an array of `n` items. What is missing is a type for it: `[Int; 10]`, checked at compile time.

### The standard library has gaps — S to M each

There is one, built in ([stdlib.md](stdlib.md)): numbers, text, arrays, lists, dictionaries, sets, optionals, files and folders, dates, program arguments, running other programs, JSON, CSV, regular expressions, encodings and hashes, an HTTP client and server, and tests. Its own list of [what is missing](stdlib.md#what-is-missing) covers maps with keys that are not text, files read in pieces, what tasks cannot do yet and time zones by name.

Adding a function written in Lumen means editing a file in `internal/stdlib/lib/`. Adding a native one means an `extern fn` there and a Go function in `internal/vm/native_*.go`; a test fails if the two do not match.

### What writing the library showed — S to L each

The library is some 2,500 lines of Lumen. Most of it compiled as first written; these are the places where the language made the code longer or stranger than it should be, in the order of how often they came up.

Most of this list has since been added, and the library and the examples rewritten with it: tuples, `?` to pass a failure on, `?.`, texts over several lines, what a test established reaching into a function expression, requirements on type parameters and equality a type defines for itself. Calling a function held in a field, `self.key(item)`, turned out to work all along. A `match` gives a value now, which took the `return` out of every case of some forty of them.

| Missing | What it cost |
|---------|--------------|
| **`${` in a text that is not an insertion** | A regex replacement naming a group is `"\${year}"`: easy to forget, and the error is "undefined identifier 'year'" |
| **A `Result` with nothing in it** | `fs.makeDir` gives back the path it was given, for want of a `Result<Void, Error>` |
| **Keywords as names** | `for case in cases` is a syntax error; so are `match`, `in`, `is` as names of variables |

### Getting packages, and native code from outside — L

**Today**: [`lumen.json`](project.md) can name folders of modules kept elsewhere (`"packages"`). Nothing downloads or versions a package, and a package cannot name its own dependencies; the program lists them all.

`extern fn` declares a function the runtime provides, but the only functions the runtime has are the ones compiled into `lumen` for the standard library. A package cannot bring native code of its own: there is no loading of shared libraries, so no binding to a C library such as raylib or SQLite. That needs calling conventions for C types (`i32`, `f32`, structs passed by value) and a way to say which library a function is in.

### Gaps in modules — S each

| Missing | Today |
|---------|-------|
| Unused import warning | Silent |
| Per-field privacy | A `pub struct` exposes every field |
| `pub` signatures checked for private types | `pub fn f() -> Secret` compiles; the caller holds a value whose type it cannot name |
| Separate compilation | All modules are parsed and compiled on every run |

### More from `match` — S to M each

Patterns on literals, ranges, types and the variants of an enum (with names, literals and nested patterns) work, with conditions and several patterns per case; a match gives a value where one is expected, and a match on an enum is checked for completeness ([enums.md](enums.md)). Not there:

| Missing | Today |
|---------|-------|
| Names in a case with several patterns: `case A(x) \| B(x):` | `E2029`; write `_`, or a case for each |
| A pattern inside a pattern: `case Shape.Rect(1 \| 2, _):`, `Shape.Circle(0.0..1.0)` | Several patterns and ranges stand at the top of a case only |
| Patterns on structs, arrays and optionals | Bind the value to a name and test it |
| A bare name as a pattern at the top level (`case other:`) | `default` |
| Completeness for anything but enums and `Bool` inside them | A `match` on an `Int` or on types needs a `default` to count as returning |

### More from enums — S to M each

| Missing | Today |
|---------|-------|
| Named values in a variant: `Rect { width: Float, height: Float }` | By position only |
| `value is Shape.Circle` | Use a `match` |
| Variants as bare names (`Circle(2.0)`), or imported one by one | Always `Shape.Circle(2.0)` |
| Equality by content for arrays and structs inside a value | They are only equal to themselves |
| A check of what a generic enum holds when casting: `any as Option<Int>` | Only the variant is checked; a wrong payload type shows later |

### Variables outside functions, and richer constants — M

**Today**: `const LIMIT = 100;` at the top level works, for numbers, texts and booleans made of literals, operators and other constants. There are no global variables, and no constant arrays, structs or enum values: a constant is replaced by its value at each use, and for those that would make a new object every time.

**Notes**: Needs storage that lives for the whole run (global slots in the VM) and an order in which initializers run across modules.

### What traits and structs still lack — S to M each

Requirements on type parameters, traits with type parameters, methods with a body, traits for the builtin types, operators, loops and brackets on structs, fields with a value of their own and methods as values are all there ([traits.md](traits.md)). Not there:

| Missing | Today |
|---------|-------|
| Two impls of one trait for one type: `impl Source<Int>` and `impl Source<String>` for the same struct | `E2019`. A type implements a trait once |
| Types a trait leaves to its impls: `type Item;` | Type parameters of the trait: `Iterable<T>` |
| Functions of a type rather than of a value: `Point.origin()` | A function next to the struct |
| Traits for arrays, tuples, optionals and functions | Only structs, enums and the four builtin types |
| `%` and unary `-` on a struct; `a[k] += v` on a struct | Not operators a trait gives; the last has to be written out |
| Equality by content inside other values | Two tuples, enum values or arrays holding structs are compared by the machine, which does not ask the structs for `equals` |
| `Show` inside other values | `println([money])` writes the struct as any value; `ops.join` writes each with its `show` |
| `value is Trait` for a number or a text | False, though the type implements the trait: the test looks at what the value carries |
| A method with type parameters as a value: `box.map` | `E2014`; wrap the call in a function |
| A method of a computed value as a value: `load().get` | `E2014`; put the value in a variable |
| Fields hidden from other modules | A `pub struct` shows all of them |

### Functions: defaults and names for arguments — S

`fn open(path: String, mode: String = "r")` and `open(path: "a.txt")` are not parsed. Every argument is given, in order.

### Nested and recursive local functions — S / M

*   `fn inner() { ... }` inside a function body is a syntax error. Use `let inner = fn() { ... };`.
*   A lambda cannot call itself: in `let f = fn(n: Int) -> Int { ... f(n - 1) ... };` the name `f` is not in scope inside the body (`E1001`). Use a top-level function for recursion.

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
| A literal for dictionaries | `dict.empty<V>()` then `set`; there is no `{"a": 1}` |
| Numbers | `Int` is 48 bits (±140 737 488 355 327), not 64; there are no unsigned or sized integers, and no bytes: a file is read as text only |
| Characters | A character is a text of length 1; `strings.code` and `fromCode` go to and from its number |
| `\u` escapes in a text | Not parsed; write the character itself |
| Formatting inside `${...}`: a width, a number of decimals | Call a function: `"${strings.padLeft(toString(n), 5)}"` |
| `x!`, to insist a value is not null | Not parsed. `x ?? panic(...)` does not type either: test against `null`, or use `?` |
| Several processors at once | Tasks and channels are there ([tasks.md](tasks.md)), taking turns on one processor. Not there: `select`, waits that give up after some time, stopping a task from outside |
| Type aliases | Not parsed |
| Methods on builtin types without a trait (`"abc".upper()`) | By design: `impl Trait for String` gives texts the methods of a trait, and nothing else puts a method on one |
| Tuples of more than six values, or of one | A struct |
| Taking a tuple apart in a parameter (`fn(key, value)` for a function of a pair) or a struct apart at all | `let (key, value) = pair;` as the first line |
| `?.` followed by an index: `items?[0]` | `arrays.at`, or a test against `null` |
| `?` in `main`, to end the program with the failure | A `match` |
| Types on a short function: `(n: Int) -> n * 2` | The long form, `fn(n: Int) -> Int { return n * 2; }`; a short one always takes its types from outside |
| A short function where nothing is expected: `let f = x -> x + 1;` | `E2037`; give the variable a type, or use the long form |

---

## 2. Checks the Compiler Does Not Make

These programs compile today and should not, or compile and misbehave.

### What flow typing does not cover — M

A test narrows a **local variable, parameter or field path** (`node.next`) inside the guarded region: `if x != null { ... }`, after `if x == null { return; }`, in a `while x != null` body, across `&&` and `||`, and for `x is T` and `match` on types. Some things are deliberately left out:

*   **a variable that a lambda assigns** is never narrowed: the lambda can run at any moment;
*   **inside a lambda**, only what is known about variables that are never assigned after their declaration carries over. For one that is assigned anywhere in the function, before or after, nothing does: the lambda may run later. What is known about fields never carries over;
*   **fields are forgotten after anything that may assign a field**: a field assignment, or a call of a function that assigns one, directly or through what it calls. Calls that cannot (builtins, functions that only read) change nothing. A call whose target is not known counts as one that may: a function value, a function stored in a field, and a method if *any* method of that name assigns a field (the receiver's type is not used). Copy the field into a local to keep it across such calls;
*   **elements are not narrowed**: `if xs[0] != null` says nothing about the next `xs[0]`;
*   **the false branch of a type test** learns nothing: after `if v is Int { ... } else { ... }`, `v` is not "anything but Int";
*   **after a loop**: `while x == null { ... }` does not make `x` non-null afterwards;
*   **a variable assigned in a loop** is not narrowed inside that loop by a test made before it, even when everything assigned to it is non-null. Walking a linked structure needs a second variable of the non-optional type: `let node: Node = root;` after `root` was tested.

These are conservative choices, not holes. The third could be made more precise by tracking *which* fields a function assigns and by using the receiver's type for method calls.

### Inference has limits — M

Type arguments are inferred from the arguments of a call (`first([1, 2])`, `stack.map(toString)`) and, for what those leave open, from the type the result is expected to have (`let xs: [Int] = none();`, `return none();`, `size(none())`). Not covered:

*   a call whose result goes nowhere typed: `let xs = none();` still needs `none<Int>()`;
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
| A variable passed as an argument, changed by a later argument of the same call | The call receives the new value: in `f(count, bump())`, where `bump` is a closure that increments `count`, `f` sees the incremented count. Arguments that are plain variables are read when the call is made, not when their turn comes. Assign the variable to a `let` first if the order matters |

---

## 3. Runtime and VM

### Strings are not counted when they grow — S

`a + b` and `text += part` create a new string each time, and each is counted. But building a long string in a loop makes garbage proportional to the square of its length (every step copies everything so far). Collecting the pieces in an array and calling `strings.join` once avoids it; there is no string builder that does this behind `+=`.

### Method dispatch cost — M

Calls on a value whose struct is known are direct. Calls through a trait look the method up on the value, so each value of a struct that implements traits carries one function object per trait method, created with the value (see [ir_generation.md](compilation/ir_generation.md#structs-and-methods)). A method table shared per struct type would remove that; it needs VM support.

The same lookup is used when IR generation does not know the receiver's struct, which happens more than it should: the types it works from are computed again after generics are instantiated, and that second computation loses the type arguments of generic structs. For `outer.get().get()` with `outer` a `Box<Box<Int>>`, the inner receiver is only known as "some value". The call still goes to the right method, by name when one struct has it, on the value otherwise; it is slower than it needs to be, and a method name shared by several generic structs (`get` of `List` and of `Dict`) is stored on each of their values for this reason. Keeping the first type information through the later stages would fix both.

### Tasks on several processors — L

Tasks take turns on one processor ([tasks.md](tasks.md)). Several virtual machines do run side by side, one per thread, but they are separate programs: nothing is shared between them.

The collector is why tasks of one program cannot simply be given a thread each. It stops the program, marks what is reachable and frees the rest, on the assumption that nothing else touches the heap meanwhile; allocation, arrays, struct values and the caches of field positions are all written without locks on the same assumption. Two ways out, neither started:

*   **A machine per task, sharing nothing**: a task gets a heap of its own, and what goes through a channel is copied. The collector stays as it is. Closures that capture variables of another task, which work today, would not;
*   **One heap for all**: every thread stopped at a safe point to collect, allocation and every object safe to use from two threads. Much more work, and data races become something a program can have.

### Smaller items

| Item | Detail |
|------|--------|
| `.lc` compatibility | Bytecode files carry a version (5 today) that is raised by hand when the format changes; a new opcode alone does not raise it |
| GC roots and Go's GC | Heap objects are held as integers in registers and stay alive only through the VM's object list; correct today, fragile if Go ever moves objects. Every such conversion goes through one function (`header` in `value.go`) |
| Pauses | The collector stops the program for as long as it takes to mark what is alive: not incremental, not generational. Some tens of milliseconds with millions of objects alive |
| Stopping a program that waits | `VM.Interrupt` and Ctrl-C stop a program that computes or sleeps where it is. One that waits for a line of input or an answer from the network stops when that wait is over; `lumen` ends the process after two seconds |
| Jumps | Every jump the compiler writes takes three words (the long form), also when the short one would do |
| Equality | Arrays compare by identity; so do structs that do not implement `Equal` |
| Float formatting | `println(3.0)` prints `3`, indistinguishable from an Int |

---

## 4. Tooling

| Missing | Today |
|---------|-------|
| REPL | `lumen repl` is a stub |
| Round-trip of compiled listings | `lumen build x.lum x.lasm` produces a listing that `lumen asm` cannot read, because long jumps are printed in an annotated form |
| Errors from several stages in one run | The pipeline stops at the first stage with errors, so type errors appear only after syntax errors are fixed |
| Machine-readable diagnostics | No `--format=json` on the command line; a program gets them as values from [`std.compiler`](stdlib.md#stdcompiler) |
| Source-level debugging | `lumdbg` steps bytecode instructions and shows registers by number; it has no statement stepping and no variable names |
| Compiler flags | `--heap-mb` and `--max-depth` exist; no optimization level, no warnings-as-errors |
| Formatter; more from the language server | Colours and a language server are there, with a VS Code extension ([editor.md](editor.md)). Not there: a formatter, renaming, finding every use of a name, the parameters of a function while its arguments are typed |
| A way to run tests | No `lumen test`, no `assert`; a program is tested by comparing what it prints |
| Documentation from comments | None |
| Optimization | None: no constant folding, no dead code elimination beyond unreachable blocks, every jump uses the 3-word long form |
| `Instr.String()` | Still renders every operand as a register; the debugger and disassembler use `asm.FormatInstruction` instead |

---

## 5. Suggested Order

A sequence that keeps each step small and makes the language noticeably more comfortable early:

1.  **Chaining**: the pipe operator. Small, and it changes how nested calls read.
6.  **Native code from outside**: `extern "library" fn`, C types and structs by value, for bindings to C libraries.

When an item is done, remove it here and update the status table in the [language spec](language-spec.md#13-implementation-status).

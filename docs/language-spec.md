# Lumen Language – Core Spec (v0.1 draft)

Focus: a small, statically checked core that compiles cleanly to the luvm register VM. HTML/routing/macros live above this layer.

## 0. Goals
- Static checks before execution
- Familiar syntax (TS/Rust/Go vibes)
- Straightforward lowering to luvm

## 1. Lexical Structure
- Source files: UTF-8 `.lum`, one file = one module (see [Modules & Imports](#6-modules--imports)).
- Identifiers: `Letter {Letter | Digit | _}`, case-sensitive.
- Keywords: `fn let const struct enum trait impl if else match case while for in break continue defer return true false null import as pub extern`, plus the type names `Int Float Bool String Any Void Self`.
- Literals: ints (`123`), floats (`1.0`, `10e3`), strings (`"hi"`), bools, `null`. No char literal in v0. A leading `-` is the unary operator, so `-42` is the negation of `42`.
- String literals stay on one line and support the escapes `\n \t \r \0 \\ \" \$`.
- **Interpolation.** `${expr}` inside a string literal is replaced by the text of the expression's value, as `toString` gives it: `"total: ${n} items"`. The expression is any expression, including strings with interpolation of their own, and must close on the same line. The literal is then an expression of type `String`, equal to the concatenation of its parts; the expressions are evaluated left to right, each time the string is. `$` not followed by `{` and `{` not preceded by `$` are ordinary characters; `\$` writes a dollar sign that does not start an interpolation.
- Comments: `// line`, `/* block */` (non-nested).

## 2. Types

### Union Types
- Syntax: `T1 | T2 | ... | Tn` (e.g. `Int | String`)
- A union type accepts a value of any listed type.
- Usage:
  - Variable declaration: `let x: Int | String = ...`
  - Function parameter: `fn foo(val: Int | Float) -> String { ... }`
  - Function return: `fn parse(val: String) -> Int | Error { ... }`
- Compatibility:
  - A value of type `T` can be assigned to a union type variable if `T` is included in the union.
  - Operations on a union type variable require type checking (e.g. via `match` or future refinement syntax).
  - Unions can be combined with optional types: `Int | String?`.
  - Unions are not implicitly converted: a value must explicitly correspond to one of the types.
  - Unions are statically checked.
- `let` mutable, `const` immutable; must initialize at declaration. Reassigning a `const` is a static error (`E2022`).
- Assignment: `x = x + 1;`. Compound forms `+=`, `-=`, `*=`, `/=`, `%=` read the target, combine and store back; the target is evaluated once (`xs[next()] += 1` calls `next` once). `x++;` and `x--;` are `x += 1;` and `x -= 1;`. All of these are statements: they have no value.
- Blocks create scope: `{ let x = 1; }`.
- Control flow:
  - `if/else` with `Bool` condition.
  - `while` with `Bool` condition.
  - `for item in items { ... }` over arrays or strings; a string yields its characters.
  - `for i, item in items { ... }` also gives the position, from 0.
  - `for i in low..high { ... }` counts from `low` up to, not including, `high`; `low..=high` includes it. Both are `Int`, worked out once before the first iteration; nothing runs when `low` is not below `high`. There and as a `match` pattern a range is counted or compared with, and nothing is made for it. Anywhere else it is a value: a `Range` of `std.ops` (a struct: `from`, `to`, `inclusive`, `by`), which the program need not import to make. `(0..10).step(2)`, `.reversed()`, `.contains(n)`, `.len()`, `.toArray()`, `.map(f)`, `.filter(f)`; a loop over one counts without making an array. Its ends are `Int` (`E2038`).
  - The loop's variables cannot be assigned (`E2026`). Assigning to the index would not move the loop, and assigning to the element would not change the collection; both look like they would. To change the collection write `items[i] = ...`; to control the position use `while`. Changing a value *through* the element is fine: `for p in points { p.x = 0; }`.
  - `break`/`continue` only in loops.

### Enums
Full description: [enums.md](enums.md).

- `enum Shape { Circle(Float), Rect(Float, Float), Empty }`: a type whose values are one of the variants, each with the values it holds. Variants may hold any types, the enum itself included. `enum Option<T> { Some(T), None }` is generic.
- A value is written `Shape.Circle(2.0)`, `Shape.Empty`. A variant with values, not called, is the function that makes one.
- Type arguments of a generic enum come from what is written (`Option<Int>.None`), from the values (`Option.Some(1)`), then from the type expected where the value goes. If none determines one, it is an error (`E2030`).
- A value has no fields; it is taken apart by `match`, or by `value is Pattern` when one variant is asked for.
- Two values are equal (`==`) when they are the same variant holding equal values.
- Enums have methods and implement traits like structs: `impl Shape { ... }`, `impl Named for Shape { ... }`.

### Constants
- `const LIMIT = 100;` at the top level of a file, optionally with a type (`const RATIO: Float = 1.5;`) and `pub`.
- The value is made of literals, operators and other constants; nothing else (`E2032`). It is worked out when the program is compiled.
- Constants may be declared in any order and refer to each other, but not, through each other, to themselves (`E2031`).
- A constant cannot be assigned (`E2022`). A local variable or a parameter of the same name hides it.
- Only numbers, texts, booleans and `null` can be constants: there are no constant arrays or structs, and no variables outside functions.

### Behaviors (Traits)
- Definition:
  ```
  trait ErrorLike {
      fn Error() -> String;
  }
  ```
- Implementation:
  ```
  struct MyStruct {}
  impl ErrorLike for MyStruct {
      fn Error() -> String { return "error"; }
  }
  ```
- Usage: Behaviors define a set of methods that a type must implement. They are similar to interfaces in other languages.
- A trait may be implemented for a struct, an enum, or one of `Int`, `Float`, `String`, `Bool`; once per type.
- Default methods: a trait method with a body is copied into every impl that does not write it.
- Type parameters: `trait Source<T> { fn next() -> T?; }`, implemented as `impl Source<Int> for Counter` or `impl<T> Source<T> for List<T>`. `Source<Int>` and `Source<String>` are different types.
- Requirements: `<T: Ordered + Show>` after `fn name`, `struct Name`, `enum Name` or `impl`. Inside, the methods of those traits can be called on a `T`; where type arguments are given or inferred, each has to implement them (`E2036`).
- The traits of `std.ops` give meaning to syntax on types that implement them: `Equal` (`==`, `!=`), `Ordered` (`<`, `<=`, `>`, `>=`), `Add`/`Subtract`/`Multiply`/`Divide` (`+ - * /` and their assignments), `Show` (`println`, `toString`, `"${}"`), `Iterable<T>` (`for`), `Indexed<K, V>` (`a[k]`), `IndexedSet<K, V>` (`a[k] = v`). See [traits.md](traits.md).
- Struct fields may have a default: `port: Int = 8080;`. A literal may leave such a field out; the expression is evaluated for each literal.
- A method without a call, on a variable or a field path, is a function value bound to it: `ages.get`.
- Self Type: `Self` can be used in trait definitions and implementations to refer to the implementing type.
  ```
  trait Cloneable {
      fn clone() -> Self;
  }
  ```

## 4. Expressions
- Primary: literals, identifiers, grouping `(expr)`, tuples `(a, b)`.
- Text over several lines: `"""` ... `"""`. The line end after the opening quotes and the one before the closing quotes are dropped, and the indentation of the closing quotes is removed from every line. `"` needs no escape inside; escapes and `${...}` work.
- Unary: `-` on `Int`/`Float`, `!` on `Bool`.
- Binary precedence (high → low): `* / %`, `+ -`, `<< >>`, `&`, `^`, `|`, `??`, `|>`, `< <= > >=`, `== !=`, `&&`, `||`. `??` binds tighter than a comparison and than `|>`: `count ?? 0 > 3` compares the count, and `name ?? "" |> strings.trim()` trims either.
  - On bits: `& | ^ << >>` and unary `~`, on `Int` only, with `&= |= ^= <<= >>=`. They bind tighter than comparisons: `flags & MASK == 0` tests the mask. `>>` keeps the sign. A shift by a negative count, or one that pushes bits past the 48 an `Int` has, is a runtime error. In the pattern of a `case`, `|` separates patterns (`case 1 | 2:`); in brackets it is the operator.
  - Whole numbers are also written by their bits: `0xFF`, `0b1010`.
  - Arithmetic: `Int`/`Float`. Two `Int` give an `Int` (`7 / 2` is `3`), two `Float` a `Float`. An `Int` with a `Float` is worked out as two `Float`: `7 / 2.0` is `3.5`.
  - Comparison: numbers or strings → `Bool`.
  - Equality: any type → `Bool`.
  - Null-coalescing `a ?? b`: `a: T?`, `b: T|T?`, result `T`.
- Member/index: `user.name`, `items[0]` (`Int` index; OOB is runtime error). Indexing a string yields a one-character string.
- Length: `arrays.len(items)` and `strings.len(text)`, from the standard library, give the number of elements of an array and of characters of a string. Arrays and strings have no members: `items.length` is an error (`E2033`) that names the function to use. A dot only reaches a field or a method of a struct or an enum.
- Safe access `a?.b`, `a?.m(args)`: `a: T?`; null when `a` is null, otherwise the rest of the postfix chain applied to `a` as a `T` (`a?.b.c` reads `c` only when `a` is not null). The result is optional. An error (`E2035`) when `a` cannot be null.
- Try `a?` (postfix): for `a: T?`, the value as a `T`, or `return null;` from the enclosing function, which must return an optional or nothing. For `a: Result<T, E>`, the value as a `T`, or a return of the failure, from a function that returns `Result<U, E>`. Anything else is `E2034`. In a function expression the enclosing function is that expression.
- If as a value: `if cond { a } else { b }` where an expression is expected, with `else if` chained. An `else` is required. A branch is braces around an expression, after statements if any; one that always leaves (`return`, `break`, `continue`, `panic`) gives nothing. The branches are of one type, or `null` (the type is then optional), or two types the place expects both of. Narrowing and the names of `value is Pattern` in the condition hold in the first branch. Only the branch taken is evaluated. At the start of a statement `if` is the statement.
- Match as a value: `match x { case p: expr; ... }` where an expression is expected. The cases are expressions of one type (`null` among them makes it optional; the type expected at that place may take them all), or blocks that always leave. Without a `default` it must be complete over an enum (`E2028`); a case that gives nothing is `E2040`.
- Tuples: `(a, b)` of type `(A, B)`, two to six values. `pair.0`, `pair.1` read by position; `let (a, b) = pair;` and `for (a, b) in pairs` name the values, `_` skipping one. Compared by content, immutable, matched with `case (a, b):` and checked for completeness like an enum (which is what a tuple is: see [enums.md](enums.md#tuples)).
- Calls: `callee(args...)` with arity/type check. Method calls: `value.method(args...)`.
- Pipe: `value |> f(args...)` is `f(value, args...)`, and `value |> f` is `f(value)`. What follows `|>` is one call, or a function (a name, a member, a function expression); a `?` after the call applies to its result. Left to right: `a |> f |> g(x)` is `g(f(a), x)`. The parser writes the call, so every rule that applies to a call applies to it.
- Array literals: `[1, 2, 3]`, `[]`. All elements have the same type.
- Struct literals: `Point { x: 1, y: 2 }`. In the header of `if`, `while`, `for` and `match`, a `{` after a name starts the body, so a struct literal there needs parentheses: `if (Point { x: 1 }).x == 1 { ... }`.
- Short functions: `x -> x + 1`, `(a, b) -> a + b`, `() -> now()`, `x -> { ...; return y; }`. No types are written: the parameters take theirs from the function type expected where the expression stands (a parameter, a declared variable or field, a return type), and the result is that type's, or the type of the body when it is a type parameter still open. The body is one expression, one assignment, or a block. Without an expected function type it is `E2037`, except for one without parameters.
- Anonymous functions: `fn(x: Int) -> Int { return x + 1; }`. They can use the variables of the enclosing function; see Closures below.
- `&&` and `||` evaluate their right operand only if the left one does not decide the result. `a ?? b` evaluates `b` only if `a` is `null`.

## 5. Functions
- Form: `fn add(a: Int, b: Int) -> Int { return a + b; }`.
- Return type required (`Void` allowed). In `-> T`, all paths return `T`; in `-> Void`, `return;` only.
- Entry point convention: `fn main() -> Int { ... }`.
- Generics: `fn map<T, U>(xs: [T], f: fn(T) -> U) -> [U] { ... }`; instantiate with `map<Int, String>(nums, toStr)`. `name<...>` is read as type arguments only when the closing `>` is followed by `(` or `{`; otherwise `<` is the comparison operator.
- Variadic arguments: `fn printf(fmt: String, args: ...Any) -> Void { ... }`. The last parameter can be prefixed with `...` to accept zero or more arguments of that type. Inside the function the parameter is an array (`arrays.len(args)`, `for a in args`). Arguments are packed when the function is called by its name; a variadic function called through a variable receives its arguments as written.
- Generic Variadic: `fn collect<T>(items: ...T) -> [T] { ... }`.

### Methods
- `impl Trait for Type { fn ... }` implements a trait; `impl Type { fn ... }` adds methods that belong to no trait.
- For a generic struct the type parameters are introduced after `impl` and repeated on the struct: `impl<T> Stack<T> { fn peek() -> T? { ... } }`, `impl<T> Sized for Stack<T> { ... }`. On a `Stack<Int>`, `peek()` then returns `Int?`.
- A method can have type parameters of its own, given or inferred at the call like those of a generic function: `fn map<U>(f: fn(T) -> U) -> Stack<U>` is called as `stack.map<String>(toString)` or just `stack.map(toString)`.
- Inside a method, `self` is the value the method was called on. It is implicit: methods do not list it among their parameters.
- Method calls are fully checked: the method must exist on the receiver's struct or trait, the arguments must match its signature, and the call has the method's return type. `Self` in a signature is the struct the method belongs to.
- A field and a method may share a name; `value.name` reads the field and `value.name(...)` calls the method. A method cannot be used as a value without calling it.
- A parameter typed with a trait accepts exactly the structs that implement it (checked at the call), and only the trait's methods can be called on it. Which struct's method runs is decided at runtime.

### Closures
- A function expression captures the variables it uses **by reference**: it sees later assignments made by the enclosing function, and its own assignments are visible outside. A captured variable lives on after the enclosing function returns.
- Each iteration of a loop has its own variables: closures created in different iterations keep the values of their own iteration, including the loop's variables (`for i, item in items`). Within one iteration a closure and the loop body still share them. A variable declared outside the loop is shared by all of them.

## 6. Modules & Imports
Full description: [modules.md](modules.md).

- One module per `.lum` file. Its name is its path from the folder of the program's entry file, with dots: `physics/body.lum` is `physics.body`. A folder is not a module.
- Paths always start at that folder; there are no relative imports.
- `import physics.body;` then `body.new(1)`, `body.Body`. The module is used through the last part of its path, or through an alias: `import physics.body as pb;`.
- `import physics.body.{Body, new};` brings names in directly.
- A declaration is private to its module unless marked `pub` (`pub fn`, `pub struct`, `pub trait`). Fields are as visible as their struct. A method of a plain `impl` needs `pub fn` to be called from another module; methods that implement a trait are always visible.
- `pub import physics.body.{Body};` exports an imported name again.
- Modules may import each other. Nothing runs when a module is loaded.
- A local variable hides a module of the same name.
- `extern fn name(params) -> Type;` declares a function without a body, provided by the runtime under its full name (module path and name). Only the functions of the standard library exist there today.
- The package `std` is the [standard library](stdlib.md), built into the compiler: `import std.strings;`.
- The program starts in the `main` of the entry file, which cannot itself be imported.
- A project file, [`lumen.json`](project.md), can name the entry file, move the folder paths start at (`root`), and name folders of modules outside it (`packages`). It is not part of the language: a program means the same with or without it, given the same files at the same module paths.

## 7. Type Rules (grab bag)
- `if`/`while` conditions must be `Bool`.
- `for in` requires array or string.
- Constants: `const NAME = value;` at the top level. The value is made of literals, operators, other constants, and structs, arrays and enum values made of those. A constant that is a struct, an array or a value of an enum is made the first time it is used and is that one value from then on, read-only with all it holds: assigning to one of its fields or items is `E2022` where the compiler can see it, and a runtime error through any other name.
- An `Int` is taken for a `Float` where one is expected: an argument, a variable or field declared `Float`, a value returned as one, an item among `Float`s (`[1, 2.5]`), one branch of an `if` whose other is a `Float`. It is converted there, so what is kept is a `Float`. With an operator, an `Int` and a `Float` give a `Float`, and compare as numbers (`2 == 2.0`). Never the other way: a `Float` becomes an `Int` only through `math.floor`, `ceil`, `round` or `trunc`. A type parameter is not guessed: `largest([1, 2.5])` works because the array is one of `Float`, `max(1, 2.5)` does not.
- **Optionals.** `T?` is "a `T` or `null`". `null` only fits `T?` or `Any`. A `T` can be used where a `T?` is expected, not the other way round. An optional cannot be operated on, indexed, or have fields read or methods called directly (`E2017`); `value ?? fallback` yields a `T`. A test against `null` narrows a local variable, a parameter or a field path (`node.next`) to `T` where the test guarantees it: in the body of `if x != null`, after `if x == null { return; }` (or `break`/`continue`), in a `while x != null` body, and on the right of `x != null && ...` / `x == null || ...`. Narrowing ends at an assignment to the variable, and does not apply inside lambdas or to a variable that a lambda assigns (it could be set to `null` whenever the lambda runs). What is known about a field also ends at the next assignment to any field and at the next call of something that may assign one, since those could change it through another reference. Calls that cannot assign a field (builtins, functions that only read) do not affect it; a call of a function value or of a function stored in a field always does.
- **Type tests.** `value is Type` is a `Bool` computed when the program runs; where it is true, the tested variable or field has that type: `if shape is Square { shape.side }`. A test that can never be true (`1 is String`) is a compile error (`E2021`). `match` accepts types as patterns (`case Int:`, `case [String]:`, `case Point:`, `case fn(Int) -> Int:`), with the same narrowing inside each case.
- **Pattern tests.** `value is P`, where `P` is the pattern of a variant or of a tuple as a `case` takes it, is a `Bool`: whether the value fits. The value is evaluated once. The names of `P` are variables of what runs only when the test held: the operands to its right in a chain of `&&`, and, when the test is the condition of an `if` or a `while` or an operand of the `&&` chain that is, the block that statement guards. They are not in scope in an `else`. A test in any other place (under `!`, an operand of `||`, a value by itself or the last operand of a chain that is a value) may not name anything (`E1003`). `P` is checked against the type of the value as a case is (`E2029`); a test that always holds is a warning (`E2023`). Where the test held, a tested variable or field is not null, and is of the enum if it was an `Any`, a trait or a union. After `is`, `Name.Name` not followed by parentheses is a type when `Name` is a module, and a tuple of names that are all types is a tuple type.
- **Patterns on enums.** `case Shape.Rect(w, h):` matches a variant and names the values it holds; the names are variables of that case. A position may also hold `_` (any value, unnamed), a literal (only that value) or the pattern of another variant, to any depth. Without parentheses (`case Shape.Rect:`) the variant is matched whatever it holds. A `match` on an enum value without a `default` must handle every value the enum can have, looking inside nested patterns (`E2028`); a case that can never run is a warning (`E2023`). Assigning a non-null value to an optional variable narrows it too.
- **`Any`** accepts every value, and that is all it does: a value of type `Any` can be passed along, printed, converted with `toString` and compared with `==`. It cannot be used as a number, indexed, called, iterated or assigned to a more specific type without a cast.
- **Casts.** `value as Type` treats a value as a more specific type: out of `Any`, out of an optional or union, or from a trait to a struct implementing it. The cast is verified when it runs and stops the program if the value is something else. A cast that can never succeed (`1 as String`) is a compile error (`E2021`); one to a wider type is free. `as` binds tighter than the binary operators: `v as Int + 1` is `(v as Int) + 1`. The check goes into the value: every element of an array, and the fields of a generic struct that depend on its type arguments. A function type is compared with the signature of the function: it must accept at least what the type passes and return what it promises.
- **Constants.** A name declared with `const` cannot be assigned (`E2022`). The value it refers to can still be changed: `const xs = [1]; xs[0] = 2;` is fine.
- **Cases.** `case a | b:` runs for either pattern and names nothing (`E2029`). `case p if cond:` runs when `p` matches and `cond`, which sees the names of `p`, holds; it is not counted when a match is checked for completeness. `case low..high:` and `case low..=high:` match numbers (`Int` or `Float`).
- **Defer.** `defer stmt;` and `defer { ... }` run when the enclosing block is left, by any way out, a failure passing through included; the defers of a block run last first, after the value of a `return` has been worked out. The body cannot leave the defer: `return`, `?`, and `break`/`continue` of an outer loop are `E2039`.
- **Returns.** A function with a return type other than `Void` must return a value on every path (`E2004`). `while true` without a `break`, and a `match` whose cases all return and which has a `default` or handles every variant of an enum, count as returning. Statements that can never run, after a `return`, `break` or `continue`, produce a warning (`E2023`).
- **Arrays.** All elements share one type. A literal takes its element type from its first element, or from the place it is used when that place has a type: `let shapes: [Shape] = [rect, square];`. An empty literal needs such a place: `let xs: [Int] = [];` (a bare `let xs = [];` is `E2020`). `push(xs, v)` requires `v` to fit the element type.
- **Structs.** A literal gives every field a value (`E2018`), and only declared fields can be read or written (`E2014`).
- **Functions as values.** A function fits a function type if it accepts at least what that type passes and returns what it promises, so `toString` (`fn(Any) -> String`) can be used as a `fn(Int) -> String`.
- **Type parameters** are opaque inside a generic function: a `T` only fits `T`, and has no operators or members. At a call, `f<Int>` has its signature with `Int` substituted, so arguments and result are checked.
- **Type argument inference.** The type arguments of a call may be left out when the arguments determine them: `first([1, 2])` is `first<Int>([1, 2])`, `map(names, toString)` infers both parameters, `stack.map(toString)` infers the method's. A parameter the arguments leave open is taken from the type expected of the result: `let xs: [Int] = none();`, `return none();`, `size(none())`. If neither determines it, it has to be written (`E3001`, or `E2024` for methods).
- **Traits.** An impl method matches the trait's by position of its type parameters, not by their names: `fn convert<U>(v: U) -> [U]` implements `fn convert<T>(v: T) -> [T]`.
- Generic type args must be concrete types (no higher-kinded). Each instantiation gets its own monomorphized copy.

## 8. luvm Mapping (sketch)
- Values: `Int`→TAG_INT, `Float`→double, `Bool`→TAG_BOOL, `String`/`Struct`/`Array`→heap ptr, `Null`→TAG_NULL, `Void`→none or TAG_UNDEF.
- Locals/temps become registers per function chunk; params occupy first registers.
- Example lowering:
  ```
  fn add(a: Int, b: Int) -> Int {
      let c = a + b;
      return c;
  }
  // a->R0, b->R1, c->R2
  0: ADD R2, R0, R1
  1: RET R2
  ```

## 9. Errors
- Static: unknown identifier, type mismatch, bad arity, assign to `const`.
- Runtime: array index out of bounds (read or write), integer overflow (an `Int` is 48 bits and never wraps), division by zero, a failed cast. The program stops with a message naming the source line.

## 10. Minimal Stdlib (host-provided)
- `fn print(args: ...Any) -> Void;` writes its arguments separated by spaces.
- `fn println(args: ...Any) -> Void;` same, then a newline.
- `fn toString(v: Any) -> String;` the text `print` would write.
- `fn panic(values: ...Any) -> Void;` stops the program with the values as its message ([errors.md](errors.md)). It does not return: a function may end with it in place of a `return`, and what follows it is unreachable.
- `fn push(array: Any, value: Any) -> Void;` appends to an array. Assigning to `items[i]` replaces an existing element; `push` is how an array grows.

Values print as `42`, `2.5`, `true`, `null`, the text of a string, `[1, 2]` for arrays, `{x: 1, y: 2}` for structs and `Rect(2, 3)` or `Empty` for values of an enum.

## 11. Program Entry and Exit
- Execution starts at `fn main()`, which takes no parameters (`E6006` otherwise).
- When `main` returns an `Int`, `lumen run` uses it as the process exit code (low 8 bits). Any other result exits with 0.
- A runtime error prints the message with the source location and exits with 1.
- Two limits end a runaway program with an error rather than letting it take the machine down: calls may nest 10000 deep (`--max-depth=N`), and the program may hold 256 MB at once (`--heap-mb=N`).

## 12. Grammar (EBNF sketch)
```
Program      = { Import | [ "pub" ] Decl } EOF ;
Import       = [ "pub" ] "import" ModulePath [ "." "{" Ident { "," Ident } "}" | "as" Ident ] ";" ;
ModulePath   = Ident { "." Ident } ;
Decl         = FnDecl | ExternDecl | ConstDecl | StructDecl | EnumDecl | TraitDecl | ImplDecl ;   (* "pub" not allowed before ImplDecl *)
ConstDecl    = "const" Ident [ ":" Type ] "=" Expr ";" ;      (* Expr of literals, operators and constants *)
EnumDecl     = "enum" Ident [BoundedParams] "{" [ Variant { "," Variant } [ "," ] ] "}" ;
StructDecl   = "struct" Ident [BoundedParams] "{" { Ident ":" Type [ "=" Expr ] ( ";" | "," ) } "}" ;
TraitDecl    = "trait" Ident [TypeParams] "{" { FnSignature ( ";" | Block ) } "}" ;
ImplDecl     = "impl" [BoundedParams] [ TypeName [TypeArgs] "for" ] TypeName [TypeParams] "{" { [ "pub" ] FnDecl } "}" ;
BoundedParams = "<" BoundedParam { "," BoundedParam } ">" ;   (* also after the name of a function *)
BoundedParam = Ident [ ":" TraitRef { "+" TraitRef } ] ;
TraitRef     = TypeName [TypeArgs] ;
Variant      = Ident [ "(" Type { "," Type } ")" ] ;
ExternDecl   = "extern" "fn" Ident "(" [ParamList] ")" [ "->" Type ] ";" ;
TypeName     = Ident [ "." Ident ] ;                          (* a type of an imported module *)

TraitDecl    = "trait" Ident "{" { TraitMethod } "}" ;
TraitMethod  = "fn" Ident "(" [ParamList] ")" "->" Type ";" ;

ImplDecl     = "impl" [TypeParams] [ Ident "for" ] Ident [TypeParams] "{" { FnDecl } "}" ;

FnDecl       = "fn" Ident [TypeParams] "(" [ParamList] ")" "->" Type Block ;
TypeParams   = "<" TypeParam { "," TypeParam } ">" ;
TypeParam    = Ident ;
ParamList    = Param { "," Param } [ "," VariadicParam ] | VariadicParam ;
Param        = Ident ":" Type ;
VariadicParam = Ident ":" "..." Type ;

StructDecl   = "struct" Ident [TypeParams] "{" { Field [ ";" | "," ] } "}" ;
Field        = Ident ":" Type ;

Block        = "{" { Stmt } "}" ;
Stmt         = LetDecl | IfStmt | WhileStmt | ForStmt | ReturnStmt | MatchStmt
             | BreakStmt | ContinueStmt | DeferStmt | Block | AssignStmt | ExprStmt ;
DeferStmt    = "defer" ( Block | AssignStmt | ExprStmt ) ;
BreakStmt    = "break" ";" ;
ContinueStmt = "continue" ";" ;
AssignStmt   = AssignTarget AssignOp Expr ";" | AssignTarget ( "++" | "--" ) ";" ;
AssignOp     = "=" | "+=" | "-=" | "*=" | "/=" | "%=" ;
AssignTarget = Ident | PostfixExpr Index | PostfixExpr Member ;
LetDecl      = ("let" | "const") Ident [ ":" Type ] "=" Expr ";" ;
IfStmt       = "if" Expr Block [ "else" (Block | IfStmt) ] ;
WhileStmt    = "while" Expr Block ;
ForStmt      = "for" [ Ident "," ] ( Ident | TupleNames ) "in" ( Expr | Range ) Block ;
Range        = OrExpr ( ".." | "..=" ) OrExpr ;
LetTuple     = ( "let" | "const" ) TupleNames "=" Expr ";" ;
TupleNames   = "(" Ident "," Ident { "," Ident } ")" ;         (* "_" skips a value *)
ReturnStmt   = "return" [ Expr ] ";" ;
MatchStmt    = "match" Expr "{" { MatchCase } "}" ;
MatchCase    = ( "case" Pattern { "|" Pattern } [ "if" Expr ] | "default" ) ":" ( Block | Expr ";" ) ;
MatchExpr    = MatchStmt ;       (* as a primary expression; a case may end with "," and the last with nothing *)
Pattern      = Literal | [ "-" ] Literal | Range | Type | VariantPat | TuplePat ;
TuplePat     = "(" SubPattern "," SubPattern { "," SubPattern } ")" ;
VariantPat   = Ident [TypeArgs] { "." Ident } [ "(" [ SubPattern { "," SubPattern } ] ")" ] ;
SubPattern   = Ident | "_" | Literal | [ "-" ] Literal | VariantPat | TuplePat ;
ExprStmt     = Expr ";" ;

Expr         = OrExpr ;
OrExpr       = AndExpr { "||" AndExpr } ;
AndExpr      = EqExpr { "&&" EqExpr } ;
EqExpr       = RelExpr { ("==" | "!=") RelExpr } ;
RelExpr      = PipeExpr { ("<" | "<=" | ">" | ">=") PipeExpr } ;
PipeExpr     = NullCoalesceExpr { "|>" UnaryExpr } ;
NullCoalesceExpr = BitOrExpr { "??" BitOrExpr } ;
BitOrExpr    = BitXorExpr { "|" BitXorExpr } ;
BitXorExpr   = BitAndExpr { "^" BitAndExpr } ;
BitAndExpr   = ShiftExpr { "&" ShiftExpr } ;
ShiftExpr    = AddExpr { ("<<" | ">>") AddExpr } ;   (* what a pipe gives can be added to: n |> f + 1 *)
AddExpr      = MulExpr { ("+" | "-") MulExpr } ;
MulExpr      = UnaryExpr { ("*" | "/" | "%") UnaryExpr } ;
UnaryExpr    = ( "-" | "!" | "~" ) UnaryExpr | CastExpr ;
CastExpr     = PostfixExpr { "as" OptType | "is" ( OptType | VariantPat | TuplePat ) } ;
IfExpr       = "if" Expr ValueBlock "else" ( IfExpr | ValueBlock ) ;   (* a Primary *)
ValueBlock   = "{" { Stmt } [ Expr ] "}" ;
PostfixExpr  = Primary { Call | Index | Member | SafeAccess | "?" } ;
SafeAccess   = "?." ( Ident [TypeArgs] | IntLit ) { Call | Index | Member } ;
Call         = "(" [ ArgList ] ")" ;
ArgList      = Expr { "," Expr } ;
Index        = "[" Expr "]" ;
Member       = "." ( Ident [TypeArgs] | IntLit ) ;            (* a number: a value of a tuple *)
Primary      = Literal | Ident [TypeArgs] | "(" Expr ")" | TupleLit
             | ArrayLit | StructLit | FnExpr ;
TupleLit     = "(" Expr "," Expr { "," Expr } [ "," ] ")" ;
ArrayLit     = "[" [ ArgList ] "]" ;
StructLit    = Ident [TypeArgs] "{" [ FieldInit { "," FieldInit } [ "," ] ] "}" ;
FieldInit    = Ident ":" Expr ;
FnExpr       = "fn" "(" [ParamList] ")" [ "->" Type ] Block | ShortFn ;
ShortFn      = ( Ident | "(" [ Ident { "," Ident } ] ")" ) "->" ( Block | Expr | Assignment ) ;
TypeArgs     = "<" Type { "," Type } ">" ;

Type         = OptType { "|" OptType } ;
OptType      = SimpleType [ "?" ] ;
SimpleType   = BuiltinType | Ident [TypeArgs] | "[" Type "]" | "(" Type ")" | TupleType | FunctionType ;
TupleType    = "(" Type "," Type { "," Type } ")" ;
BuiltinType  = "Int" | "Float" | "Bool" | "String" | "Any" | "Void" | "Self" ;
FunctionType = "fn" [TypeParams] "(" [TypeList] ")" "->" Type ;
TypeList     = Type { "," Type } [ "," "..." Type ] | "..." Type ;

Ident        = Letter { Letter | Digit | "_" } ;
Literal      = IntLit | FloatLit | StringLit | "true" | "false" | "null" ;
```

## 13. Implementation Status

What `lumen run` compiles and executes today. The pipeline is described in
[compilation/pipeline.md](compilation/pipeline.md); gaps, missing checks and
known defects are inventoried in [missing.md](missing.md).

| Feature | Status | Notes |
|---------|--------|-------|
| `Int`, `Float`, `Bool`, `String`, `null` | Supported | Ints are 48-bit signed; overflow is a runtime error |
| Arithmetic, comparison, `!`, unary `-` | Supported | An Int with a Float is a Float; strings compare by content |
| String concatenation, indexing | Supported | Length is `strings.len(text)`; for an array, `arrays.len(items)` |
| String interpolation: `"total: ${n}"` | Supported | Any expression; `\$` for a literal dollar before a brace |
| `let`, `const`, assignment, `+=` and `++`, block scopes | Supported | `++`/`--` are statements |
| `if`/`else`, `while`, `for in`, `break`, `continue` | Supported | `for in` over arrays and strings, optionally with the index |
| `&&`, `||`, `??` | Supported | Short-circuit |
| `value?.member`, `value?` | Supported | Null when there is no value; return null or the failure of a `Result` to the caller |
| Tuples: `(1, "a")`, `pair.0`, `let (a, b) = pair;` | Supported | Two to six values; equal by content; matched like enums |
| Text over several lines: `"""..."""` | Supported | Indentation of the closing quotes is taken off |
| Functions, recursion, functions as values | Supported | |
| Anonymous functions and closures | Supported | Capture by reference |
| Short functions: `p -> p.name`, `(a, b) -> a + b` | Supported | Types come from where the function is expected |
| Arrays | Supported | Grow with `push` |
| Structs, field access and assignment | Supported | |
| Traits, `impl`, methods, `self`, `Self` | Supported | Direct call when the struct is known, runtime lookup through a trait |
| Trait methods with a body, traits with type parameters, traits for `Int`/`Float`/`String`/`Bool` | Supported | See [traits.md](traits.md) |
| Requirements on type parameters: `<T: Ordered + Show>` | Supported | On functions, structs, enums and impls |
| Operators, `for` and brackets on structs, through the traits of `std.ops` | Supported | `==`, `<`, `+ - * /`, `for x in it`, `it[k]`, `it[k] = v` |
| Struct fields with a default, methods as values | Supported | |
| Optionals `T?`, `null`, `??` | Supported | Checked statically; null tests narrow variables and fields |
| Casts `value as Type` | Supported | Verified at runtime, including array elements, generic struct fields and function signatures |
| `const` | Supported | Reassignment is rejected |
| Generic functions | Supported | Type arguments are inferred from the arguments, or written: `f<Int>(x)` |
| Variadic functions | Supported | Packed at direct calls |
| `match` on literals, `default` | Supported | |
| `match` as a value: `let x = match v { case 1: "one"; default: "many"; };` | Supported | Complete over an enum, or with a `default` |
| Conditions and several patterns: `case Shape.Circle(r) if r > 1.0:`, `case 1 \| 2:` | Supported | A case with several patterns names nothing |
| Ranges: `for i in 0..10`, `0..=10`, `case 1..10:` | Supported | Not a value: `arrays.range` makes the array |
| `defer` | Supported | Runs when its block is left, also by a failure |
| `match` on types (`case Int:`), `value is Type` | Supported | Narrows the tested variable or field |
| Enums: variants with values, generic enums, methods and traits on them | Supported | See [enums.md](enums.md) |
| `match` on enum variants, with names, literals and nested patterns | Supported | Checked for completeness |
| One variant: `if value is Shape.Circle(r) { ... }`, in `if`, `else if`, `while`, with `&&` | Supported | Names live in the guarded block and the rest of the `&&`; none under `!` or `\|\|` (`E1003`) |
| Constants at the top level | Supported | Numbers, texts and booleans, known when the program is compiled |
| Modules: `import`, `pub`, several files | Supported | Packages are folders named in `lumen.json`; no installer |
| Standard library (`std.math`, `std.strings`, `std.arrays`, `std.list`, `std.dict`, `std.option`, `std.result`, `std.io`, `std.time`, `std.random`) | Supported | Built into the compiler; see [stdlib.md](stdlib.md) |
| `extern fn` | Supported | Only for functions built into the runtime; no loading of shared libraries |
| Generic structs and their methods | Supported | `impl<T> Box<T> { ... }`; methods may add their own: `fn map<U>(...)` |
| "All paths return" check, unreachable code | Supported | Unreachable code is a warning |

**Static typing.** Method calls, trait-typed values, optionals, generic calls
and struct fields are all checked at compile time; `Any` is an explicit type
the programmer writes, not something the checker falls back on. What the
checker still does not do is listed in [missing.md](missing.md): inference
from the expected type, and the deliberate limits of flow typing.

**Size limits** come from 16-bit bytecode operands: 65536 registers per
function, 65536 distinct constants and 65536 functions per program. Exceeding
one is a compile error (`E6001`-`E6003`), never silent corruption.

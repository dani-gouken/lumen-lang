# Lumen Language – Core Spec (v0.1 draft)

Focus: a small, statically checked core that compiles cleanly to the luvm register VM. HTML/routing/macros live above this layer.

## 0. Goals
- Static checks before execution
- Familiar syntax (TS/Rust/Go vibes)
- Straightforward lowering to luvm

## 1. Lexical Structure
- Source files: UTF-8 `.lum`, one file = one module.
- Identifiers: `Letter {Letter | Digit | _}`, case-sensitive.
- Keywords: `fn let const struct enum trait impl if else match case while for in break continue return true false null import as`, plus the type names `Int Float Bool String Any Void Self`.
- Literals: ints (`123`), floats (`1.0`, `10e3`), strings (`"hi"`), bools, `null`. No char literal in v0. A leading `-` is the unary operator, so `-42` is the negation of `42`.
- String literals stay on one line and support the escapes `\n \t \r \0 \\ \"`.
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
- `let` mutable, `const` immutable; must initialize at declaration. Reassigning a `const` is a static error.
- Assignment: `x = x + 1;`.
- Blocks create scope: `{ let x = 1; }`.
- Control flow:
  - `if/else` with `Bool` condition.
  - `while` with `Bool` condition.
  - `for item in items { ... }` over arrays or strings (iteration unit TBD).
  - `break`/`continue` only in loops.

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
- Self Type: `Self` can be used in trait definitions and implementations to refer to the implementing type.
  ```
  trait Cloneable {
      fn clone() -> Self;
  }
  ```

## 4. Expressions
- Primary: literals, identifiers, grouping `(expr)`.
- Unary: `-` on `Int`/`Float`, `!` on `Bool`.
- Binary precedence (high → low): `* / %`, `+ -`, `< <= > >=`, `== !=`, `&&`, `||`, `??`.
  - Arithmetic: `Int`/`Float`; result matches operands (no implicit int↔float coercion).
  - Comparison: numbers or strings → `Bool`.
  - Equality: any type → `Bool`.
  - Null-coalescing `a ?? b`: `a: T?`, `b: T|T?`, result `T`.
- Member/index: `user.name`, `items[0]` (`Int` index; OOB is runtime error). Indexing a string yields a one-character string.
- Length: `items.length` and `text.length` give the number of elements of an array and of characters of a string.
- Calls: `callee(args...)` with arity/type check. Method calls: `value.method(args...)`.
- Array literals: `[1, 2, 3]`, `[]`. All elements have the same type.
- Struct literals: `Point { x: 1, y: 2 }`. In the header of `if`, `while`, `for` and `match`, a `{` after a name starts the body, so a struct literal there needs parentheses: `if (Point { x: 1 }).x == 1 { ... }`.
- Anonymous functions: `fn(x: Int) -> Int { return x + 1; }`. They can use the variables of the enclosing function; see Closures below.
- `&&` and `||` evaluate their right operand only if the left one does not decide the result. `a ?? b` evaluates `b` only if `a` is `null`.

## 5. Functions
- Form: `fn add(a: Int, b: Int) -> Int { return a + b; }`.
- Return type required (`Void` allowed). In `-> T`, all paths return `T`; in `-> Void`, `return;` only.
- Entry point convention: `fn main() -> Int { ... }`.
- Generics: `fn map<T, U>(xs: [T], f: fn(T) -> U) -> [U] { ... }`; instantiate with `map<Int, String>(nums, toStr)`. `name<...>` is read as type arguments only when the closing `>` is followed by `(` or `{`; otherwise `<` is the comparison operator.
- Variadic arguments: `fn printf(fmt: String, args: ...Any) -> Void { ... }`. The last parameter can be prefixed with `...` to accept zero or more arguments of that type. Inside the function the parameter is an array (`args.length`, `for a in args`). Arguments are packed when the function is called by its name; a variadic function called through a variable receives its arguments as written.
- Generic Variadic: `fn collect<T>(items: ...T) -> [T] { ... }`.

### Methods
- `impl Trait for Type { fn ... }` implements a trait; `impl Type { fn ... }` adds methods that belong to no trait.
- Inside a method, `self` is the value the method was called on. It is implicit: methods do not list it among their parameters.
- A method name must not also be the name of a struct field anywhere in the program (`E5004`).
- A parameter typed with a trait accepts any struct that implements it; the method is looked up on the value at runtime.

### Closures
- A function expression captures the variables it uses **by reference**: it sees later assignments made by the enclosing function, and its own assignments are visible outside. A captured variable lives on after the enclosing function returns.
- Variables declared inside a loop body are shared by all closures created in that loop; copy the value into a function parameter to give each closure its own.

## 6. Modules & Imports
- One module per `.lum` file; module name = path relative to a chosen root (TBD).
- Imports: `import "std/io" as io; import "app/user";` use `module::name` (or alias).
- No selective imports in v0.

## 7. Type Rules (grab bag)
- `if`/`while` conditions must be `Bool`.
- `for in` requires array or string.
- `null` only fits `T?` or `Any`.
- No implicit `Int`↔`Float` coercion.
- `Any` accepts anything; assigning `Any` to specific `T` needs an explicit cast (future work).
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
- Runtime: null deref, array OOB, type errors that escape checks, divide by zero. Runtime aborts with an error message is acceptable in v0.

## 10. Minimal Stdlib (host-provided)
- `fn print(args: ...Any) -> Void;` writes its arguments separated by spaces.
- `fn println(args: ...Any) -> Void;` same, then a newline.
- `fn toString(v: Any) -> String;` the text `print` would write.
- `fn push(array: Any, value: Any) -> Void;` appends to an array. Assigning to `items[i]` replaces an existing element; `push` is how an array grows.

Values print as `42`, `2.5`, `true`, `null`, the text of a string, `[1, 2]` for arrays and `{x: 1, y: 2}` for structs.

## 11. Program Entry and Exit
- Execution starts at `fn main()`, which takes no parameters (`E6006` otherwise).
- When `main` returns an `Int`, `lumen run` uses it as the process exit code (low 8 bits). Any other result exits with 0.
- A runtime error prints the message with the source location and exits with 1.

## 12. Grammar (EBNF sketch)
```
Program      = { Import | Decl } EOF ;
Import       = "import" StringLiteral [ "as" Ident ] ";" ;
Decl         = FnDecl | StructDecl | TraitDecl | ImplDecl ;

TraitDecl    = "trait" Ident "{" { TraitMethod } "}" ;
TraitMethod  = "fn" Ident "(" [ParamList] ")" "->" Type ";" ;

ImplDecl     = "impl" [ Ident "for" ] Ident "{" { FnDecl } "}" ;

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
             | BreakStmt | ContinueStmt | Block | AssignStmt | ExprStmt ;
BreakStmt    = "break" ";" ;
ContinueStmt = "continue" ";" ;
AssignStmt   = AssignTarget "=" Expr ";" ;
AssignTarget = Ident | PostfixExpr Index | PostfixExpr Member ;
LetDecl      = ("let" | "const") Ident [ ":" Type ] "=" Expr ";" ;
IfStmt       = "if" Expr Block [ "else" (Block | IfStmt) ] ;
WhileStmt    = "while" Expr Block ;
ForStmt      = "for" Ident "in" Expr Block ;
ReturnStmt   = "return" [ Expr ] ";" ;
MatchStmt    = "match" Expr "{" { MatchCase } "}" ;
MatchCase    = ( "case" Pattern | "default" ) ":" ( Block | Expr ";" ) ;
Pattern      = Expr | Type ;
ExprStmt     = Expr ";" ;

Expr         = OrExpr ;
OrExpr       = AndExpr { "||" AndExpr } ;
AndExpr      = NullCoalesceExpr { "&&" NullCoalesceExpr } ;
NullCoalesceExpr = EqExpr { "??" EqExpr } ;
EqExpr       = RelExpr { ("==" | "!=") RelExpr } ;
RelExpr      = AddExpr { ("<" | "<=" | ">" | ">=") AddExpr } ;
AddExpr      = MulExpr { ("+" | "-") MulExpr } ;
MulExpr      = UnaryExpr { ("*" | "/" | "%") UnaryExpr } ;
UnaryExpr    = ( "-" | "!" ) UnaryExpr | PostfixExpr ;
PostfixExpr  = Primary { Call | Index | Member } ;
Call         = "(" [ ArgList ] ")" ;
ArgList      = Expr { "," Expr } ;
Index        = "[" Expr "]" ;
Member       = "." Ident ;
Primary      = Literal | Ident [TypeArgs] | "(" Expr ")"
             | ArrayLit | StructLit | FnExpr ;
ArrayLit     = "[" [ ArgList ] "]" ;
StructLit    = Ident [TypeArgs] "{" [ FieldInit { "," FieldInit } [ "," ] ] "}" ;
FieldInit    = Ident ":" Expr ;
FnExpr       = "fn" "(" [ParamList] ")" [ "->" Type ] Block ;
TypeArgs     = "<" Type { "," Type } ">" ;

Type         = OptType { "|" OptType } ;
OptType      = SimpleType [ "?" ] ;
SimpleType   = BuiltinType | Ident [TypeArgs] | "[" Type "]" | "(" Type ")" | FunctionType ;
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
| `Int`, `Float`, `Bool`, `String`, `null` | Supported | Ints are 48-bit signed |
| Arithmetic, comparison, `!`, unary `-` | Supported | No implicit Int/Float conversion; strings compare by content |
| String concatenation, indexing, `.length` | Supported | |
| `let`, `const`, assignment, block scopes | Supported | Assigning to a `const` is not rejected yet |
| `if`/`else`, `while`, `for in`, `break`, `continue` | Supported | `for in` over arrays and strings |
| `&&`, `||`, `??` | Supported | Short-circuit |
| Functions, recursion, functions as values | Supported | |
| Anonymous functions and closures | Supported | Capture by reference |
| Arrays | Supported | Grow with `push` |
| Structs, field access and assignment | Supported | |
| Traits, `impl`, methods, `self` | Supported | Dispatch is by method name on the value |
| Generic functions | Supported | Type arguments are explicit: `f<Int>(x)`. They are not inferred (`E3001`) |
| Variadic functions | Supported | Packed at direct calls |
| `match` on literals, `default` | Supported | |
| `match` on types (`case Int:`) | Not yet | Rejected with `E7001` |
| Enums | Not yet | Parsed, rejected with `E7001` |
| Imports / multiple files | Not yet | Parsed, rejected with `E7001` |
| Generic structs | Supported | `Box<Int> { value: 1 }`; their fields are not type checked |
| "All paths return" check, unreachable code | Not yet | A function that falls off its end returns `null` |

**Static typing is partial.** The checker verifies what it can prove and treats
the rest as `Any`, which is checked when the program runs: results of method
calls, values of generic type, optional types (`T?`) and trait-typed values.
A mistake in those places surfaces as a runtime error with a source location
rather than as a compile error.

**Size limits** come from one-byte bytecode operands: 256 registers per
function, 256 distinct constants and 256 functions per program. Exceeding one
is a compile error (`E6001`-`E6003`), never silent corruption.

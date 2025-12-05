# Lumen Language – Core Spec (v0.1 draft)

Focus: a small, statically checked core that compiles cleanly to the luvm register VM. HTML/routing/macros live above this layer.

## 0. Goals
- Static checks before execution
- Familiar syntax (TS/Rust/Go vibes)
- Straightforward lowering to luvm

## 1. Lexical Structure
- Source files: UTF-8 `.lum`, one file = one module.
- Identifiers: `Letter {Letter | Digit | _}`, case-sensitive.
- Keywords: `fn let const struct enum if else match while for in break continue return true false null import as`.
- Literals: ints (`123`, `-42`), floats (`1.0`, `10e3`), strings (`"hi"`), bools, `null`. No char literal in v0.
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
- Member/index: `user.name`, `items[0]` (`Int` index; OOB is runtime error).
- Calls: `callee(args...)` with arity/type check.

## 5. Functions
- Form: `fn add(a: Int, b: Int) -> Int { return a + b; }`.
- Return type required (`Void` allowed). In `-> T`, all paths return `T`; in `-> Void`, `return;` only.
- Entry point convention: `fn main() -> Int { ... }`.
- Generics: `fn map<T, U>(xs: [T], f: fn(T) -> U) -> [U] { ... }`; instantiate with `map[Int, String](nums, toStr)`. No traits/interfaces in v0.
- Variadic arguments: `fn printf(fmt: String, args: ...Any) -> Void { ... }`. The last parameter can be prefixed with `...` to accept zero or more arguments of that type.
- Generic Variadic: `fn collect<T>(...T) -> [T] { ... }`.

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
- `fn print(msg: String) -> Void;`
- `fn println(msg: String) -> Void;`
- `fn toString(v: Any) -> String;`

## 11. Grammar (EBNF sketch)
```
Program      = { Import | Decl } EOF ;
Import       = "import" StringLiteral [ "as" Ident ] ";" ;
Decl         = FnDecl | StructDecl | TraitDecl | ImplDecl ;

TraitDecl    = "trait" Ident "{" { TraitMethod } "}" ;
TraitMethod  = "fn" Ident "(" [ParamList] ")" "->" Type ";" ;

ImplDecl     = "impl" Ident "for" Ident "{" { FnDecl } "}" ;

FnDecl       = "fn" Ident [TypeParams] "(" [ParamList] ")" "->" Type Block ;
TypeParams   = "<" TypeParam { "," TypeParam } ">" ;
TypeParam    = Ident ;
ParamList    = Param { "," Param } [ "," VariadicParam ] | VariadicParam ;
Param        = Ident ":" Type ;
VariadicParam = Ident ":" "..." Type ;

StructDecl   = "struct" Ident [TypeParams] "{" { Field [";" ] } "}" ;
Field        = Ident ":" Type ;

Block        = "{" { Stmt } "}" ;
Stmt         = LetDecl | IfStmt | WhileStmt | ForStmt | ReturnStmt | MatchStmt | ExprStmt ;
LetDecl      = ("let" | "const") Ident [ ":" Type ] "=" Expr ";" ;
IfStmt       = "if" Expr Block [ "else" (Block | IfStmt) ] ;
WhileStmt    = "while" Expr Block ;
ForStmt      = "for" Ident "in" Expr Block ;
ReturnStmt   = "return" [ Expr ] ";" ;
MatchStmt    = "match" Expr "{" { MatchCase } "}" ;
MatchCase    = "case" Pattern ":" Block ;
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
Primary      = Literal | Ident [TypeArgs] | "(" Expr ")" ;
TypeArgs     = "<" Type { "," Type } ">" ;

Type         = SimpleType [ "?" ] ;
SimpleType   = BuiltinType | Ident [TypeArgs] | "[" Type "]" | "(" Type ")" | FunctionType ;
BuiltinType  = "Int" | "Float" | "Bool" | "String" | "Any" | "Void" | "Self" ;
FunctionType = "fn" [TypeParams] "(" [TypeList] ")" "->" Type ;
TypeList     = Type { "," Type } [ "," "..." Type ] | "..." Type ;

Ident        = Letter { Letter | Digit | "_" } ;
Literal      = IntLit | FloatLit | StringLit | "true" | "false" | "null" ;
```

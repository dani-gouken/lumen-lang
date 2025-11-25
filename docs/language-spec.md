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
- Built-ins: `Int` (64-bit), `Float` (64-bit), `Bool`, `String`, `Any`, `Void`, `T?` (nullable).
- Structs: `struct User { id: Int; name: String; email: String? }` (fields separated by newline or `;`).
- Arrays: `[T]` dynamic length.
- Generics (monomorphized per instantiation): type params on structs/functions: `struct Box<T> { value: T }`, `fn id<T>(x: T) -> T { x }`; instantiate with `Box[Int]`, `id[String]("hi")`. Type arg inference for calls is allowed when unambiguous; otherwise supply explicitly.
- Minimal inference: `let x = 1;` infers `Int`; function params/returns must be annotated.

## 3. Declarations & Statements
- `let` mutable, `const` immutable; must initialize at declaration. Reassigning a `const` is a static error.
- Assignment: `x = x + 1;`.
- Blocks create scope: `{ let x = 1; }`.
- Control flow:
  - `if/else` with `Bool` condition.
  - `while` with `Bool` condition.
  - `for item in items { ... }` over arrays or strings (iteration unit TBD).
  - `break`/`continue` only in loops.

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
Decl         = FnDecl | StructDecl ;

FnDecl       = "fn" Ident [TypeParams] "(" [ParamList] ")" "->" Type Block ;
TypeParams   = "<" TypeParam { "," TypeParam } ">" ;
TypeParam    = Ident ;
ParamList    = Param { "," Param } ;
Param        = Ident ":" Type ;

StructDecl   = "struct" Ident [TypeParams] "{" { Field [";" ] } "}" ;
Field        = Ident ":" Type ;

Block        = "{" { Stmt } "}" ;
Stmt         = LetDecl | IfStmt | WhileStmt | ForStmt | ReturnStmt | ExprStmt ;
LetDecl      = ("let" | "const") Ident [ ":" Type ] "=" Expr ";" ;
IfStmt       = "if" Expr Block [ "else" (Block | IfStmt) ] ;
WhileStmt    = "while" Expr Block ;
ForStmt      = "for" Ident "in" Expr Block ;
ReturnStmt   = "return" [ Expr ] ";" ;
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
SimpleType   = BuiltinType | Ident [TypeArgs] | "[" Type "]" | "(" Type ")" ;
BuiltinType  = "Int" | "Float" | "Bool" | "String" | "Any" | "Void" ;

Ident        = Letter { Letter | Digit | "_" } ;
Literal      = IntLit | FloatLit | StringLit | "true" | "false" | "null" ;
```

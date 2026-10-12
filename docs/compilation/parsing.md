# Parsing Specification

This document describes the first stage of the [compilation pipeline](pipeline.md): turning source text into an Abstract Syntax Tree. It covers the lexer, the parser, the places where the grammar is ambiguous and how they are resolved, and error recovery.

The grammar itself is in [language-spec.md](../language-spec.md#12-grammar-ebnf-sketch). The implementation is in `internal/parser`.

## Objective

*   Produce an `*ast.Program` for every syntactically valid source file.
*   Give every node the token it came from, so later stages can point at the exact source span.
*   Report syntax errors with a precise location and, where possible, what to do about them.
*   Keep going after an error, so one run reports several.

## Usage

```go
errs := errors.NewHandler()
prog := parser.Parse(src, "main.lum", errs)

if errs.HasErrors() {
    // prog still holds every declaration that parsed cleanly
}
```

`parser.SourceLines(src)` returns the 1-based line map that the other stages pass to the error handler.

## Lexer (`lexer.go`)

The lexer turns characters into `token.Token` values. Each token records its type, its text, and its start and end position (file, line, column, both 1-based; the end is exclusive).

| Category | Rules |
|----------|-------|
| Whitespace | Spaces, tabs, carriage returns and newlines separate tokens and are otherwise ignored |
| Comments | `// to end of line` and `/* block */` (not nested). Dropped by `Tokenize` |
| Identifiers | A letter or `_`, then letters, digits and `_`. Keywords and builtin type names are recognized by `token.LookupIdent` |
| Integers | Digits: `42` |
| Floats | Digits with a fraction and/or exponent: `3.14`, `10e3`, `1.5e-2`. A `.` only starts a fraction when a digit follows, so `xs.length` and `1.x` are not floats |
| Strings | `"..."` on one line, with escapes `\n \t \r \0 \\ \"` |
| Operators | Longest match first: `...`, `->`, `==`, `!=`, `<=`, `>=`, `&&`, `||`, `??`, `+=`, `-=`, `*=`, `/=`, `%=`, `++`, `--` before the single-character ones. As a consequence `a--b` is `a`, `--`, `b`; write `a - -b` |

There is no token for negative numbers: `-1` is the unary operator applied to `1`.

**Lexical errors do not stop the lexer.** An unknown character, an unterminated string or comment, or an invalid escape produces an `ILLEGAL` token whose text describes the problem. The parser reports it (as `E0002`) when it reaches the token. This keeps all reporting in one place and lets the parser recover the same way as for any other error.

## Parser (`parser.go`)

A hand-written recursive descent parser over the full token list. Having all tokens in memory makes arbitrary lookahead and backtracking trivial, which two of the ambiguities below rely on.

### Declarations

`Program = { Import | Decl }`. A declaration is selected by its first token: `fn`, `struct`, `enum`, `trait`, `impl`.

*   **Functions**: `fn name<T>(params) -> Type { body }`. The return type may be omitted (`Void`). Trait methods end with `;` instead of a body.
*   **Structs**: fields may be separated by `;` or `,`, and the last separator is optional.
*   **Impl blocks**: `impl Trait for Type { ... }` or `impl Type { ... }`. The second form leaves `TraitName` empty. For a generic struct the parameters follow `impl` and must be repeated, in the same order, on the struct: `impl<T> Box<T>`. The repetition is what ties a method's `T` to the struct's, so a mismatch is reported (with the correct header as suggestion) without abandoning the block.

### Statements

A statement is selected by its first token; anything else is an expression statement.

**Assignment** has no node of its own. After parsing an expression statement's expression, a following `=` turns it into a `BinaryExpr` with operator `"="`:

```
x = 1;        BinaryExpr{ Left: Ident(x),        Op: "=", Right: 1 }
xs[0] = 1;    BinaryExpr{ Left: IndexExpr(...),  Op: "=", Right: 1 }
p.x = 1;      BinaryExpr{ Left: MemberExpr(...), Op: "=", Right: 1 }
```

The compound forms use the same node with their operator: `x += 1` has Op `"+="`. `x++` and `x--` are parsed as `x += 1` and `x -= 1`; they exist only as statements, so there is no prefix form and no value to define. `ast.CompoundOp` and `ast.IsAssignment` classify the operators for later stages.

The left side must be an identifier, an index expression or a member expression (`E0004` otherwise). The desugarer builds the same shape for the assignments it generates, so every later stage handles one representation.

**Match**: each case is `case Pattern:` or `default:`, followed by a block or by an expression and `;`. `default` is not a keyword; it is recognized by position. A default case has a `nil` pattern.

A pattern is either a type or an expression, and the two overlap:

*   The parser first tries to read the pattern as a **type** (`tryTypePattern`, speculative). It keeps the result when the pattern can only be a type: `Int`, `[String]`, `Int?`, `fn(Int) -> Bool`, `Box<Int>`.
*   A **bare name** is ambiguous (`case Point:` or `case limit:`), so it is parsed as an expression. When the whole file has been read, `classifyPatterns` turns those that name a struct or trait declared in the file into type patterns.
*   Everything else is an expression: literals, `-1`.

### Expressions

Binary expressions use precedence climbing. All binary operators are left-associative. From lowest to highest:

| Level | Operators |
|-------|-----------|
| 1 | `||` |
| 2 | `&&` |
| 3 | `??` |
| 4 | `==` `!=` |
| 5 | `<` `<=` `>` `>=` |
| 6 | `+` `-` |
| 7 | `*` `/` `%` |
| unary | `-` `!` |
| cast | `value as Type` |
| postfix | call `f(x)`, index `a[i]`, member `a.b` |

`value is Type` sits at the same level and produces a `TypeTestExpr`. `is` is not a reserved word: after a complete expression an identifier cannot follow, so in that position it can only be this operator, and `is` remains usable as a name elsewhere.

A cast produces a `CastExpr`. Its type is parsed without unions, so `v as Int | String` is not a cast to a union; `v as Int?` is fine. `as` is also the keyword of `import "..." as name`, where it is handled by the import rule.

Primary expressions: literals, identifiers (optionally with type arguments), parenthesized expressions, array literals `[a, b]`, struct literals `Name { field: value }` and function expressions `fn(params) -> Type { body }`.

## Ambiguities

### Struct literal or block?

```lumen
if ready { run(); }        // `ready` is the condition, `{` opens the body
let p = Point { x: 1 };    // `Point {` opens a struct literal
```

Both are an identifier followed by `{`. Two rules decide:

1.  **Context.** While parsing the header of `if`, `while`, `for` and `match`, struct literals are switched off (`noStructLit`). They are switched back on inside any delimiters, because there the `{` cannot be the body: `if (Point { x: 1 }).x == 1 { ... }` and `f(Point { x: 1 })` are fine.
2.  **Lookahead.** Elsewhere, `Name {` is a struct literal only if what follows is `}` or `field:`. This keeps `x { ... }` from being misread in the rare places a block may follow an expression.

### Type arguments or comparison?

```lumen
collect<Int>(1, 2)     // generic call
a < b                  // comparison
a < b > (c)            // two comparisons? or a<b>(c)?
```

After an identifier, `<` starts a **speculative** parse of a type argument list (`tryTypeArgs`). It is accepted only if it is well formed, closed by `>`, and immediately followed by `(` or `{` — the two things that can follow an instantiated name. Otherwise the parser rewinds to the `<` and treats it as an operator. Errors raised while speculating are not reported.

The same rule applies after a member name, for calls of generic methods: `stack.map<String>(f)` carries its type arguments on the `MemberExpr`, while `a.b < c` stays a comparison.

The price is that `a < b > (c)` is read as the generic call `a<b>(c)`. Chained comparisons like that do not type check anyway, and parentheses resolve it: `(a < b) > c`.

## Error Handling

All errors are reported through `internal/errors` with a code in the `E0xxx` range; see [diagnosis.md](diagnosis.md) for the table.

### Recovery

A syntax error unwinds to the nearest recovery point, which then skips ahead to a place where parsing can resume. There are two levels:

| Level | Recovery point | Skips to |
|-------|----------------|----------|
| Statement | `parseStmtRecovering`, called for each statement of a block | Past the next `;` at the same nesting depth, or to the `}` closing the block |
| Declaration | `parseTopLevel`, called for each import and declaration | The next `fn`, `struct`, `enum`, `trait`, `impl` or `import` |

The broken statement is dropped from the AST; the statements around it are kept. So a file with three unrelated mistakes reports three errors, and each is reported once.

**Missing semicolons are not fatal at all.** `expectSemicolon` reports `E0003` pointing just past the previous token, where the `;` belongs, and carries on as if it were present:

```text
[ERROR] E0003: expected ';' after '1', found 'let'
  --> main.lum:3:14
   |
 3 |     let a = 1
   |              ^
   |
   = Suggestion: Add ';' to end the statement
```

Since the pipeline stops after a stage that reported errors, a program with syntax errors is never resolved or type checked; the partial AST is only used to find further syntax errors.

## Testing Strategy

Tests are in `internal/parser/lexer_test.go` and `parser_test.go`.

*   **Lexer**: token types for each category, escape handling, positions across lines, each lexical error.
*   **Precedence**: expressions are parsed and printed back with `String()`, which parenthesizes every binary expression, so the expected grouping is visible in the test: `1 + 2 * 3` → `(1 + (2 * 3))`.
*   **Ambiguities**: one test each for generic call vs comparison and struct literal vs block.
*   **Errors**: one case per error code, asserting on code, message and line.
*   **Recovery**: a file with several mistakes must report exactly those and keep the valid declarations.

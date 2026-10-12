# AST Desugaring Implementation

This package implements AST desugaring for the Lumen programming language, transforming complex syntactic constructs into simpler, more primitive forms.

## Overview

The desugaring phase transforms:
- **For loops** → While loops with explicit iteration
- **Match statements** → If-else chains
- **Short-circuit operators** (`&&`, `||`) → Explicit control flow with temporary variables
- **Null coalescing** (`??`) → Conditional assignments

This simplification reduces complexity for later compilation stages (closure analysis, IR generation) while preserving program semantics.

## Key Design Decisions

### Statement-Level Desugaring for Short-Circuit Operators

Short-circuit operators (`&&`, `||`, `??`) are **fully desugared** when they appear in statement contexts:

```lumen
// Original
let result = func1() && func2();

// Desugared (the helpers are added to the enclosing block, so `result`
// stays in scope for the statements that follow)
let __tmp_0 = func1();
let __tmp_1 = false;
if __tmp_0 {
  __tmp_1 = func2();
}
let result = __tmp_1;
```

A `while` whose condition contains these operators becomes `while true` with the
helpers and an `if !cond { break; }` at the top of the body, so the condition is
recomputed before every iteration.

The key insight: **We CAN transform expressions into statements** by:
1. Detecting short-circuit operators at the statement level (`containsShortCircuit`)
2. Hoisting the desugaring logic to generate pre-statements
3. Wrapping the result in a block statement

This works for:
- **If/while conditions**: `if a() && b() { ... }`
- **Let statements**: `let x = a() || b();`
- **Return statements**: (can be extended similarly)

### Expression-Level Fallback

When short-circuit operators appear in pure expression contexts (e.g., nested in function arguments without being in a let/if/while), we:
1. Recursively desugar the operands
2. Keep the operator itself
3. Let IR generation handle the short-circuit semantics

This is rare in practice because most real-world code uses these operators in statement contexts.

## Architecture

### Main Components

- **`desugar.go`**: Core desugarer, orchestrates transformations
- **`for_loop.go`**: For-loop → while-loop transformation
- **`match.go`**: Match statement → if-else transformation
- **`binary_expr.go`**: Short-circuit operator desugaring
- **`errors.go`**: Error reporting with rich diagnostics and suggestions

### Data Structures

```go
type Desugarer struct {
    prog         *ast.Program
    typeInfo     *types.Info
    resolveTable *resolve.Table
    errs         *errors.Handler
    tmpCounter   int
    info         *DesugarInfo
}

type DesugarInfo struct {
    Mapping map[ast.Node]ast.Node  // Original → Desugared
    TmpVars map[string]string       // Variable → Purpose
}
```

## Transformations

### 1. For-Loop Desugaring

```lumen
// Before
for item in collection {
    body
}

// After
{
    let __iter_0 = 0;
    let __len_0 = collection.length;
    while __iter_0 < __len_0 {
        let item = collection[__iter_0];
        __iter_0 = __iter_0 + 1;
        body
    }
}
```

The increment precedes the body so that a `continue` in the body cannot skip it.
An iterable that is not a plain variable is first stored in a temporary, so it is
evaluated once.

**Implementation**: [for_loop.go:20](./for_loop.go#L20)

**Features**:
- Generates unique iterator (`__iter_N`) and length (`__len_N`) variables
- Preserves original loop variable name
- Updates type information for generated variables
- Handles both arrays and strings (and `Any`, checked when the loop runs)

### 2. Match Statement Desugaring

```lumen
// Before
match x {
    case 1: { action1(); }
    case 2: { action2(); }
    default: { defaultAction(); }
}

// After
if x == 1 {
    action1();
} else if x == 2 {
    action2();
} else {
    defaultAction();
}
```

**Implementation**: [match.go:51](./match.go#L51)

**Features**:
- Handles value matching (literals, expressions)
- Supports type matching with `__is_type` intrinsic
- Generates temporary variables for complex matched expressions
- Preserves pattern order and semantics

### 3. Short-Circuit Operators

#### In Conditions

```lumen
// Before
if a() && b() {
    action();
}

// After
{
    let __tmp_0 = a();
    let __tmp_1 = false;
    if __tmp_0 {
        __tmp_1 = b();
    }
    if __tmp_1 {
        action();
    }
}
```

**Implementation**: [binary_expr.go:94](./binary_expr.go#L94)

#### In Let Statements

```lumen
// Before
let result = func1() || func2();

// After
{
    let __tmp_0 = func1();
    let __tmp_1 = true;
    if !__tmp_0 {
        __tmp_1 = func2();
    }
    let result = __tmp_1;
}
```

**Implementation**: [desugar.go:182](./desugar.go#L182)

## Type Information Management

The desugarer maintains type information for generated nodes:

```go
// Iterator variables get Int type
iterIdent := d.createIdent(iterVar, pos)
d.typeInfo.Types[iterIdent] = types.Int

// Result variables inherit expression types
if exprType, ok := d.typeInfo.Types[expr]; ok {
    d.typeInfo.Types[tmpVarExpr] = exprType
}
```

This ensures type-checking passes can still work with desugared code.

## Usage

```go
import (
    "github.com/dani-gouken/lumen/internal/desugar"
    "github.com/dani-gouken/lumen/internal/ast"
    "github.com/dani-gouken/lumen/internal/types"
    "github.com/dani-gouken/lumen/internal/resolve"
    "github.com/dani-gouken/lumen/internal/errors"
)

// After monomorphization, type checking, and name resolution:
desugarer := desugar.NewDesugarer(prog, typeInfo, resolveTable, errHandler)
desugaredProg, desugarInfo := desugarer.Desugar()

// Use desugaredProg for further compilation stages
// Use desugarInfo for debugging (mapping original → desugared nodes)
```

## Error Reporting

The desugarer provides comprehensive error diagnostics with helpful context and suggestions:

### Error Codes

| Code | Description | Example |
|------|-------------|---------|
| **E4001** | Unsupported Iterable | `for x in 42 { }` - can't iterate over Int |
| **E4002** | Invalid Match Pattern | Unsupported pattern in match statement |
| **E4003** | Missing Type Info | Type information missing (internal error) |
| **E4004** | Nested Short-Circuit | Warning for deeply nested `&&`/`||` |
| **E4005** | Unsupported Match Type | Complex type patterns not supported |
| **E4006** | Enum Variant Extraction | Invalid enum variant pattern |

### Example Error Output

```
[ERROR] E4001: Cannot iterate over type 'int'
  --> src/main.lum:10:5
   10 | for x in count {
      |          ^^^^^
   = Suggestion: Only arrays and strings are iterable. Consider using a while loop or converting to an array.
```

### Validation Checks

The desugarer validates:
- **For loops**: Iterable type must be array or string
- **Match patterns**: Must be literals, identifiers, or type names
- **Type information**: All expressions must have type info from previous phases
- **Complexity**: Warns about deeply nested short-circuit operators (depth ≥ 5)

### Error Recovery

The desugarer continues processing after errors to report multiple issues:
- Invalid patterns in match statements don't stop validation of other patterns
- Type errors are collected and reported together
- Warnings don't prevent desugaring from completing

## Testing

Run tests:
```bash
go test ./internal/desugar/... -v
```

Test coverage includes:
- For loop desugaring (simple, nested, with break/continue)
- Match statement desugaring (value, type, enum matching)
- Short-circuit operators in conditions
- Short-circuit operators in let statements
- Edge cases (empty programs, complex nesting)
- **Error reporting** (invalid iterables, missing type info, invalid patterns)
- **Warnings** (deeply nested expressions)

## Integration with Compilation Pipeline

Position in pipeline:
```
Source → Parse → Resolve → TypeCheck → Monomorphize → **Desugar** → ClosureAnalysis → IRGen → Bytecode
```

**Inputs**:
- Monomorphized AST (no generic type parameters)
- Complete type information
- Name resolution table

**Outputs**:
- Simplified AST (while loops only, no match, explicit short-circuit)
- DesugarInfo for debugging

## Performance

- **Time complexity**: O(n) where n = AST size (single-pass traversal)
- **Space complexity**: O(n) for temporary node storage
- **Code size impact**: For loops expand 3-4x, match expands linearly with cases

The IR optimizer can eliminate many temporary variables introduced during desugaring.

## Future Extensions

Potential additions:
- String interpolation → concatenation
- List comprehensions → for loops
- Optional chaining (`?.`) → explicit null checks
- Pattern matching with destructuring
- Range-based for loops

## References

See [docs/compilation/desugaring.md](../../docs/compilation/desugaring.md) for the full specification.

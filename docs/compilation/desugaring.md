# AST Desugaring Specification

This document details the design and implementation of the AST Desugaring phase in the Lumen compiler.

## Objective

The Desugaring phase transforms complex syntactic constructs into simpler, more primitive forms that are easier to compile to IR. This simplification reduces the complexity of later compilation stages while preserving the semantics of the original program.

## Motivation

Desugaring provides several benefits:

1. **Simplified IR Generation**: By transforming complex constructs into simpler ones, IR generation becomes more straightforward and less error-prone
2. **Reduced Backend Complexity**: The IR generator only needs to handle a small set of primitive operations
3. **Easier Optimization**: Simpler constructs are easier to analyze and optimize
4. **Consistent Semantics**: Complex features are defined in terms of their simpler equivalents, ensuring consistent behavior

### Example

Given this high-level code:
```lumen
fn sum_evens(nums: [Int]) -> Int {
    let total = 0;
    for num in nums {
        if num % 2 == 0 {
            total = total + num;
        }
    }
    return total;
}
```

After desugaring:
```lumen
fn sum_evens(nums: [Int]) -> Int {
    let total = 0;
    let __iter_0 = 0;
    let __len_0 = nums.length;
    while __iter_0 < __len_0 {
        let num = nums[__iter_0];
        if num % 2 == 0 {
            total = total + num;
        }
        __iter_0 = __iter_0 + 1;
    }
    return total;
}
```

## Input and Output

**Input**:
- `*ast.Program` (from monomorphization)
- `*types.Info` (type information for all expressions)
- `*resolve.Table` (name resolution information)

**Output**:
- `*ast.Program` (transformed with all sugar removed)
- `*DesugarInfo` (mapping of original nodes to desugared nodes for debugging)

## Desugaring Transformations

### 1. For-In Loops → While Loops

**Goal**: Transform `for-in` loops over arrays into `while` loops with explicit index management.

**Transformation**:

```lumen
// Before
for item in collection {
    body
}

// After (for arrays)
{
    let __iter_N = 0;
    let __len_N = collection.length;
    while __iter_N < __len_N {
        let item = collection[__iter_N];
        __iter_N = __iter_N + 1;
        body
    }
}
```

The increment comes **before** the body. A `continue` in the body jumps back to
the loop condition; with the increment at the end it would be skipped and the
loop would never advance.

When the iterable is anything but a plain variable, it is stored in a temporary
first so that it is evaluated exactly once:

```lumen
// Before
for item in loadItems() {
    body
}

// After
{
    let __tmp_N = loadItems();
    let __iter_M = 0;
    let __len_M = __tmp_N.length;
    while __iter_M < __len_M {
        let item = __tmp_N[__iter_M];
        __iter_M = __iter_M + 1;
        body
    }
}
```

**For strings**:
```lumen
// Before
for char in str {
    body
}

// After
{
    let __iter_N = 0;
    let __len_N = str.length;
    while __iter_N < __len_N {
        let char = str[__iter_N];  // char extraction
        __iter_N = __iter_N + 1;
        body
    }
}
```

An iterable whose static type is `Any` (for example a value read from a generic
container) is accepted as well; whether it really is an array or a string is
checked when the loop runs.

**Implementation Details**:
- Generate unique variable names for iterator and length (`__iter_N`, `__len_N`)
- Wrap the entire transformation in a block to ensure proper scoping
- Preserve the loop variable name from the original `for` statement
- `break` and `continue` statements remain unchanged, which is why the increment precedes the body
- Update type information for generated variables

### 2. Match Statements → If-Else Chains

**Goal**: Transform `match` expressions into nested `if-else` statements.

**Simple Value Matching**:
```lumen
// Before
match x {
    case 1: { action1(); }
    case 2: { action2(); }
    case 3: { action3(); }
    default: { defaultAction(); }
}

// After
if x == 1 {
    action1();
} else if x == 2 {
    action2();
} else if x == 3 {
    action3();
} else {
    defaultAction();
}
```

**Type Matching (Union Types)**:
```lumen
// Before
match value {
    case Int: { handleInt(); }
    case String: { handleString(); }
    default: { handleOther(); }
}

// After
{
    let __match_tmp_N = value;
    if __is_type(__match_tmp_N, Int) {
        handleInt();
    } else if __is_type(__match_tmp_N, String) {
        handleString();
    } else {
        handleOther();
    }
}
```

**Enum Variant Matching**:
```lumen
// Before
match result {
    case Ok(val): { return val; }
    case Err(err): { print(err); return 0; }
}

// After
{
    let __match_tmp_N = result;
    if __is_variant(__match_tmp_N, "Ok") {
        let val = __extract_variant(__match_tmp_N, 0);
        return val;
    } else if __is_variant(__match_tmp_N, "Err") {
        let err = __extract_variant(__match_tmp_N, 0);
        print(err);
        return 0;
    }
}
```

**Implementation Details**:
- Generate a temporary variable to store the matched expression
- Convert each case into an equality or type check
- Chain cases using `if-else`
- For exhaustive matches (all cases covered), omit the final `else`
- For enum variants, introduce runtime helper functions (`__is_variant`, `__extract_variant`)
- Preserve source location information for error reporting

**Patterns accepted today**: literals (including negative numbers such as
`case -1:`) and `default:`. A `default:` case has no pattern and becomes the
final `else`. Type patterns (`case Int:`) are desugared to a call to the
`__is_type` intrinsic, which has no runtime implementation yet; the compiler
driver rejects them up front with `E7001` (see [pipeline.md](pipeline.md)).

### 3. Optional Chaining (Future)

**Goal**: Transform optional chaining into explicit null checks.

```lumen
// Before
let result = user?.address?.street;

// After
let result;
if user != null {
    let __tmp1 = user.address;
    if __tmp1 != null {
        result = __tmp1.street;
    } else {
        result = null;
    }
} else {
    result = null;
}
```

### 4. Null Coalescing (`??`)

**Goal**: Transform null coalescing into explicit null checks.

```lumen
// Before
let name = user.name ?? "Unknown";

// After
{
    let __tmp_N = user.name;
    let name;
    if __tmp_N != null {
        name = __tmp_N;
    } else {
        name = "Unknown";
    }
}
```

**Nested Coalescing**:
```lumen
// Before
let val = a ?? b ?? c;

// After
{
    let __tmp_N = a;
    let val;
    if __tmp_N != null {
        val = __tmp_N;
    } else {
        let __tmp_M = b;
        if __tmp_M != null {
            val = __tmp_M;
        } else {
            val = c;
        }
    }
}
```

### 5. Short-Circuit Logical Operators (`&&`, `||`)

**Goal**: Make short-circuit evaluation explicit using control flow.

**AND Operator**:
```lumen
// Before
if condition1() && condition2() {
    action();
}

// After
{
    let __tmp_N = condition1();
    let __result_N;
    if __tmp_N {
        __result_N = condition2();
    } else {
        __result_N = false;
    }
    if __result_N {
        action();
    }
}
```

**OR Operator**:
```lumen
// Before
if condition1() || condition2() {
    action();
}

// After
{
    let __tmp_N = condition1();
    let __result_N;
    if __tmp_N {
        __result_N = true;
    } else {
        __result_N = condition2();
    }
    if __result_N {
        action();
    }
}
```

**`let` statements**: a `let` whose initializer contains a short-circuit
operator gets helper statements in front of it. They are inserted into the
**enclosing block**, not wrapped in a block of their own, because a new block
would end the scope of the declared variable immediately:

```lumen
// Before
let result = func1() && func2();
use(result);

// After
let __tmp_N = func1();
let __tmp_M = false;
if __tmp_N {
    __tmp_M = func2();
}
let result = __tmp_M;
use(result);          // `result` is still in scope
```

**`while` conditions**: the helper statements compute the condition, so they
have to run before *every* iteration, including the ones reached through
`continue`. The loop becomes an unconditional loop with an explicit exit:

```lumen
// Before
while a() && b() {
    body
}

// After
while true {
    let __tmp_N = a();
    let __tmp_M = false;
    if __tmp_N {
        __tmp_M = b();
    }
    if !__tmp_M { break; }
    body
}
```

**Everywhere else** (return values, call arguments, assignments, nested
operands) the operators are left in place and IR generation lowers them to
branches directly; see [ir_generation.md](ir_generation.md). Both routes
evaluate the right operand only when the left one does not decide the result.

### 6. String Interpolation (Future)

**Goal**: Transform string interpolation into concatenation.

```lumen
// Before
let msg = "Hello, {name}! You are {age} years old.";

// After
let msg = "Hello, " + toString(name) + "! You are " + toString(age) + " years old.";
```

## Implementation Structure

### Package: `internal/desugar`

```go
package desugar

import (
    "lumen/internal/ast"
    "lumen/internal/types"
    "lumen/internal/resolve"
    "lumen/internal/errors"
)

type Desugarer struct {
    prog        *ast.Program
    typeInfo    *types.Info
    resolvTable *resolve.Table
    errs        *errors.Handler

    // Counter for generating unique temporary variable names
    tmpCounter  int

    // Mapping from original nodes to desugared nodes (for debugging)
    mapping     map[ast.Node]ast.Node
}

func NewDesugarer(
    prog *ast.Program,
    typeInfo *types.Info,
    resolvTable *resolve.Table,
    errs *errors.Handler,
) *Desugarer

func (d *Desugarer) Desugar() (*ast.Program, *DesugarInfo)
```

### Key Functions

```go
// Main entry point
func (d *Desugarer) Desugar() (*ast.Program, *DesugarInfo)

// Statement transformations
func (d *Desugarer) desugarStmt(stmt ast.Stmt) ast.Stmt
func (d *Desugarer) desugarForStmt(stmt *ast.ForStmt) ast.Stmt
func (d *Desugarer) desugarMatchStmt(stmt *ast.MatchStmt) ast.Stmt

// Expression transformations
func (d *Desugarer) desugarExpr(expr ast.Expr) ast.Expr
func (d *Desugarer) desugarBinaryExpr(expr *ast.BinaryExpr) ast.Expr
func (d *Desugarer) desugarNullCoalesce(expr *ast.BinaryExpr) ast.Expr
func (d *Desugarer) desugarShortCircuit(expr *ast.BinaryExpr) ast.Expr

// Utilities
func (d *Desugarer) generateTmpVar() string
func (d *Desugarer) wrapInBlock(stmts ...ast.Stmt) *ast.BlockStmt
func (d *Desugarer) createComparison(left, right ast.Expr, op string) *ast.BinaryExpr
func (d *Desugarer) cloneType(t *ast.Type) *ast.Type
```

### DesugarInfo Structure

```go
type DesugarInfo struct {
    // Maps original AST nodes to their desugared equivalents
    Mapping map[ast.Node]ast.Node

    // Maps generated temporary variables to their purposes (for debugging)
    TmpVars map[string]string
}
```

## Algorithm Overview

The desugaring process is a single-pass tree transformation:

### Pass: Tree Transformation

1. **Walk the AST**: Traverse the program tree recursively
2. **Identify Sugar**: Detect constructs that need desugaring
3. **Transform**: Replace sugared constructs with their desugared equivalents
4. **Recurse**: Continue desugaring nested expressions and statements
5. **Update Metadata**: Maintain type and resolution information for generated nodes

**Traversal Order**: Post-order (children first, then parent)
- This ensures that nested sugar is desugared from the inside out
- Example: `for x in arr { match x { ... } }` desugars the match first, then the for loop

## Type and Resolution Table Updates

When generating new AST nodes during desugaring:

1. **Type Information**: Generated variables and expressions need type information
   - Iterator variables: `Int` type
   - Length variables: `Int` type
   - Temporary variables: inherit type from their source expression

2. **Resolution Table**: Generated variables need entries in the resolution table
   - Create new symbols for generated variables
   - Add them to the appropriate scope
   - Link identifier references to their symbols

3. **Source Locations**: Preserve original source locations
   - Generated nodes should reference the original source location
   - This ensures error messages point to the right place
   - Use the `BaseNode` position from the original construct

### 7. Anonymous Functions

The body of a function expression is desugared like the body of a declaration,
so loops and matches inside lambdas are lowered too.

## Error Handling

Desugaring should generally not introduce new errors, as it operates on a well-typed AST. However, some validation may be necessary:

The codes below are defined in `internal/desugar/errors.go`.

| Code | Name | Raised when |
|------|------|-------------|
| `E4001` | Unsupported iterable | A `for-in` loop iterates over something that is neither an array, a string nor `Any` |
| `E4002` | Invalid match pattern | A match case pattern is malformed or cannot be desugared |
| `E4003` | Missing type info | The desugarer needs the type of an expression that the checker did not record |
| `E4004` | Nested short-circuit | Warning: `&&`/`||` nested five or more levels deep |
| `E4005` | Unsupported match type | A type pattern that cannot be lowered, e.g. a union type |
| `E4006` | Enum variant extraction | Destructuring an enum variant in a pattern |

### E4001: Unsupported Iterable

**Example**:
```lumen
for x in 42 {  // Cannot iterate over Int
    print(x);
}
```

**Error Message**: "Cannot iterate over type 'int'" with the suggestion "Only arrays and strings are iterable."

**Note**: The type checker reports the same mistake first as `E2011`, so this
code is only reached for programs built without it (as in the unit tests).

### E4002: Invalid Match Pattern

**Example**:
```lumen
match x {
    case 1..10: { action(); }  // Range patterns not supported yet
}
```

## Interaction with Other Phases

### Input from Monomorphization

Desugaring expects:
1. All generic types and functions have been instantiated
2. No `TypeParam` nodes remain in the tree
3. All type information is concrete and complete

### Output to Closure Analysis

After desugaring:
1. The AST contains simpler, more primitive constructs
2. Control flow is more explicit (no `for` loops, only `while`)
3. All expressions are simple (no complex operators like `&&`, `||`, `??`)
4. The closure analyzer can work with a reduced set of node types

### Impact on IR Generation

Desugaring significantly simplifies IR generation:
1. Fewer statement types to handle (`while` instead of `for`)
2. Fewer expression types to handle (no short-circuit operators)
3. More explicit control flow makes basic block construction easier
4. Temporary variables make SSA form generation straightforward

## Testing Strategy

### Unit Tests

1. **For Loop Desugaring**:
   - Simple `for-in` loop over array
   - `for-in` loop with `break`
   - `for-in` loop with `continue`
   - Nested `for` loops
   - `for-in` loop over string

2. **Match Statement Desugaring**:
   - Simple value matching
   - Type matching (union types)
   - Enum variant matching with destructuring
   - Exhaustive vs non-exhaustive matches

3. **Null Coalescing**:
   - Simple `a ?? b`
   - Nested coalescing `a ?? b ?? c`
   - Coalescing with function calls

4. **Short-Circuit Operators**:
   - `&&` in condition
   - `||` in condition
   - Nested combinations `a && b || c`
   - Short-circuit in assignments

### Integration Tests

1. **End-to-End**:
   - Source code with sugar → Parse → Resolve → Type Check → Monomorphize → Desugar → Verify AST
   - Ensure desugared code has equivalent semantics

2. **Complex Programs**:
   - Programs with multiple layers of sugar
   - Verify type information is preserved
   - Verify resolution table is updated correctly

3. **Round-Trip**:
   - Desugar code, then pretty-print it
   - Verify the output is valid Lumen code (even if more verbose)

### Golden File Tests

For each transformation, maintain "golden files" showing:
- Input: original sugared code
- Output: expected desugared AST (as string representation)

Example:
```
Input: for x in [1, 2, 3] { print(x); }

Expected Output:
{
  let __iter_0 = 0;
  let __len_0 = 3;
  while __iter_0 < __len_0 {
    let x = array[__iter_0];
    print(x);
    __iter_0 = __iter_0 + 1;
  }
}
```

## Performance Considerations

### Compilation Impact

Desugaring adds minimal overhead:
1. **Single-pass transformation**: Linear time complexity O(n) where n = AST size
2. **Node cloning**: Some memory allocation for new nodes
3. **Symbol table updates**: O(1) insertions per generated variable

### Code Size Impact

Desugaring can increase AST size:
1. **For loops**: 1 `for` → 1 `while` + 3-4 statements (initialization, condition, increment)
2. **Match statements**: 1 `match` → n `if-else` chains
3. **Short-circuit operators**: 1 expression → 3-4 statements

**Mitigation**: IR generation and optimization can often reduce this overhead.

### Runtime Impact

Desugaring should have **no runtime impact**:
1. Desugared code has identical semantics to original
2. IR generator produces the same IR whether code is sugared or not
3. Optimizations can eliminate temporary variables

## Future Extensions

### 1. Loop Desugaring → Loop + Break

Alternative desugaring that uses an infinite loop:
```lumen
// Before
for item in collection {
    body
}

// After (alternative)
{
    let __iter_N = 0;
    let __len_N = collection.length;
    loop {
        if __iter_N >= __len_N { break; }
        let item = collection[__iter_N];
        body
        __iter_N = __iter_N + 1;
    }
}
```

### 2. Pattern Matching Optimization

For dense integer ranges, use jump tables instead of `if-else` chains:
```lumen
// Before
match x {
    case 0: { a(); }
    case 1: { b(); }
    case 2: { c(); }
    case 3: { d(); }
}

// After (with jump table hint)
switch_table x {
    0 => { a(); }
    1 => { b(); }
    2 => { c(); }
    3 => { d(); }
}
```

### 3. List Comprehensions

```lumen
// Before
let evens = [x * 2 for x in nums if x % 2 == 0];

// After
{
    let evens = [];
    for x in nums {
        if x % 2 == 0 {
            evens.push(x * 2);
        }
    }
}
```

## References

- [Scala Desugaring](https://docs.scala-lang.org/scala3/reference/other-new-features/explicit-nulls.html)
- [Rust HIR (High-level IR)](https://rustc-dev-guide.rust-lang.org/hir.html) - Rust's approach to desugaring
- [Swift SIL](https://github.com/apple/swift/blob/main/docs/SIL.rst) - Swift's desugaring and lowering strategy
- [GHC Core](https://gitlab.haskell.org/ghc/ghc/-/wikis/commentary/compiler/core-syn-type) - Haskell's core language after desugaring

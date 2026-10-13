# Enums and Pattern Matching

An enum is a type whose values are one of several **variants**. Each variant may hold values of its own.

```lumen
enum Shape {
    Circle(Float),          // holds a radius
    Rect(Float, Float),     // holds a width and a height
    Empty,                  // holds nothing
}
```

A value is made by naming a variant, and taken apart by a `match`:

```lumen
fn area(shape: Shape) -> Float {
    match shape {
        case Shape.Circle(r): { return 3.14 * r * r; }
        case Shape.Rect(w, h): { return w * h; }
        case Shape.Empty: { return 0.0; }
    }
}

area(Shape.Rect(2.0, 3.0))      // 6
```

Complete programs: [`examples/derivative.lum`](../examples/derivative.lum) works out the derivative of a formula with an enum that holds itself; [`test/e2e/cases/enums_json.lum`](../test/e2e/cases/enums_json.lum) builds, searches and prints a JSON document.

## Declaring

```lumen
enum Light { Red, Amber, Green }                    // only names

enum Event { Deposit(Int), Withdraw(Int), Closed }  // some with values

enum Expr { Num(Int), Add(Expr, Expr), Neg(Expr) }  // holding itself

enum Json { Null, Bool(Bool), String(String), Array([Json]) }
```

*   Variants are separated by commas; a comma after the last one is allowed.
*   The values of a variant have types and no names. Any type will do: arrays, structs, functions, other enums, optionals, the enum itself.
*   A variant may have the name of a builtin type (`Bool`, `String`, `Int`): after a dot it cannot be taken for the type.
*   `pub enum` makes it usable from other [modules](modules.md). Its variants come with it; they are not exported one by one.

## Making Values

```lumen
let a = Shape.Circle(2.0);
let b = Shape.Empty;                // no parentheses for a variant without values
let shapes: [Shape] = [a, b, Shape.Rect(1.0, 2.0)];
```

The variant is always written with its enum: `Shape.Circle`, never `Circle` alone. From another module it is `geo.Shape.Circle`, or `Shape.Circle` after `import geo.{Shape};`.

A variant with values, not called, is the function that makes one:

```lumen
let circles = arrays.map([1.0, 2.0, 3.0], Shape.Circle);    // [Shape]
let make: fn(Float) -> Shape = Shape.Circle;
```

## Matching

A `match` on an enum tries its cases in order and runs the first that fits.

```lumen
match event {
    case Event.Deposit(amount): { balance += amount; }
    case Event.Withdraw(amount): { balance -= amount; }
    case Event.Closed: { open = false; }
}
```

**Names.** A name in a pattern becomes a variable of that case, holding the value at that position. It exists only in the case. `_` takes a value without naming it.

```lumen
case Shape.Rect(w, _): { return w; }
```

**Without parentheses** a pattern takes the variant whatever it holds: `case Shape.Rect: { ... }`.

**Literals** match only that value. Put the specific cases before the general one:

```lumen
match shape {
    case Shape.Circle(0.0): { return "a point"; }
    case Shape.Circle(r): { return "a circle"; }
    default: { return "something else"; }
}
```

**Patterns inside patterns** look into a value that is itself an enum, to any depth:

```lumen
fn simplify(e: Expr) -> Expr {
    match e {
        case Expr.Add(Expr.Num(0), right): { return right; }      // 0 + x is x
        case Expr.Neg(Expr.Neg(inner)): { return inner; }         // --x is x
        case Expr.Add(Expr.Num(a), Expr.Num(b)): { return Expr.Num(a + b); }
        default: { return e; }
    }
}
```

**`default`** takes whatever the cases above did not.

A name of a pattern is a copy of what the variant holds: assigning to it does not change the value that was matched.

### Every case must be handled

A `match` on an enum without a `default` has to cover every value the enum can have. Leaving something out is a compile error that names it:

```lumen
fn next(light: Light) -> Light {
    match light {
        case Light.Red: { return Light.Green; }
        case Light.Green: { return Light.Amber; }
    }
}
```
```
[ERROR] E2028: match on Light does not handle Light.Amber
   = Suggestion: Add a case for each, or a 'default' case: case Light.Amber: { ... }
```

This is the point of an enum over a set of numbers or strings: add a variant, and the compiler lists every `match` that has to learn about it.

The check looks inside patterns. These three cases are complete, because between them they take every `Option<Result<Int, String>>`:

```lumen
case Option.Some(Result.Ok(n)): { ... }
case Option.Some(Result.Err(message)): { ... }
case Option.None: { ... }
```

and when part of a variant is missing, the message says which part:

```
match on Report does not handle Report.Line(_, Event.Withdraw)
```

What can be listed is: the variants of an enum, and `true` and `false`. Numbers and texts cannot, so cases with literals of those never complete a match by themselves; a case with a name or `_` in that position, or a `default`, has to follow.

A function whose `match` is complete and returns in every case returns on every path: no `return` is needed after it.

A case that can never run, because a case above already takes its variant whole, is a warning (`E2023`).

### Other things a `match` can test

The patterns of an enum mix with the [other kinds](guide.md#control-flow): a type (`case Shape:` takes any `Shape`, `case Int:`) and a literal. When the value is an `Any` or a trait, a variant pattern tests for the enum as well:

```lumen
fn describe(value: Any) -> String {
    match value {
        case Shape.Circle(r): { return "circle"; }
        case Shape: { return "another shape"; }
        case Int: { return "a number"; }
        default: { return "?"; }
    }
}
```

## Generic Enums

```lumen
enum Option<T> { Some(T), None }
enum Result<T, E> { Ok(T), Err(E) }
enum List<T> { Cons(T, List<T>), Nil }
```

The type arguments are worked out, in this order, from:

1.  what is written: `Option<Int>.None`
2.  the values given: `Option.Some(1)` is an `Option<Int>`
3.  the type expected where the value goes:

```lumen
let a: Option<Int> = Option.None;
fn find() -> Option<String> { return Option.None; }
if a == Option.None { ... }                    // the other side of a comparison
let b: Option<Result<Int, String>> = Option.Some(Result.Ok(1));    // reaches inner values
```

When none of these says what a type argument is, it is an error: `let x = Option.None;` gives `nothing here says what T of Option is`, and the fix is to write it.

In a pattern the type arguments are those of the value being matched; the names it binds have the matching types:

```lumen
fn first(r: Result<[Int], String>) -> Int {
    match r {
        case Result.Ok(items): { return items[0]; }       // items: [Int]
        case Result.Err(message): { return strings.len(message); }  // message: String
    }
}
```

## Methods and Traits

An enum has methods and implements traits exactly as a struct does. `self` is the value; a `match` on it is the usual body.

```lumen
impl Shape {
    fn scaled(by: Float) -> Shape {
        match self {
            case Shape.Circle(r): { return Shape.Circle(r * by); }
            case Shape.Rect(w, h): { return Shape.Rect(w * by, h * by); }
            case Shape.Empty: { return self; }
        }
    }
}

impl Describe for Shape { fn describe() -> String { ... } }

impl<T> Option<T> {
    fn map<U>(f: fn(T) -> U) -> Option<U> {
        match self {
            case Option.Some(v): { return Option.Some(f(v)); }
            case Option.None: { return Option.None; }
        }
    }
}
```

A method cannot have the name of a variant.

## Equality and Printing

Two values of an enum are **equal** when they are the same variant holding equal values:

```lumen
Shape.Circle(1.0) == Shape.Circle(1.0)      // true
Shape.Empty == Shape.Empty                  // true
Shape.Circle(1.0) == Shape.Circle(2.0)      // false
```

This goes all the way down through enums inside enums, so two trees built the same way are equal. An array or a struct *inside* an enum value keeps its own rule and is only equal to itself.

A value **prints** the way it is made, without the enum's name: `Circle(2)`, `Empty`, `Some("x")`, `Cons(1, Cons(2, Nil))`.

## What an Enum Value Is Not

*   It has no fields: `shape.radius` is an error. A `match` is the only way in.
*   The enum is not a value: `let s = Shape;` is an error.
*   It is not a struct: `Shape { ... }` is an error.

## Errors

| Code | Meaning | Example |
|------|---------|---------|
| `E2028` | A `match` on an enum leaves values out | no case for `Light.Amber` |
| `E2029` | A pattern that cannot match: unknown variant, wrong number of values, the variant of another enum, a literal of the wrong type | `case Shape.Rect(w):` |
| `E2030` | An enum used wrongly: unknown variant, wrong number of values, a type argument nothing determines, the enum itself as a value | `Shape.Square(1.0)` |
| `E2023` (warning) | A case that is never reached | a second `case Shape.Circle(r):` |

## What Is Not There

*   **Named values** in a variant (`Rect { width: Float, height: Float }`): values are told apart by position.
*   **Conditions on a case** (`case Shape.Circle(r) if r > 10.0:`): use an `if` inside the case.
*   **Several patterns for one case** (`case Light.Red | Light.Amber:`).
*   **Patterns on structs, arrays and optionals**: only enums are taken apart; a struct inside a variant is bound to a name and used as usual.
*   **`value is Shape.Circle`**: `is` tests for a type; use a `match` to test for a variant.
*   **`match` as an expression** with a value.
*   **A variant by itself**: `Circle(2.0)` has to be written `Shape.Circle(2.0)`; there is no way to import the variants of an enum as bare names.

## How It Is Compiled

**Types.** To the type checker a value of `Shape` has a type named `Shape`, built like the type of a struct. That is what gives enums type arguments, methods and traits with no code of their own. What differs, making and matching, is in `internal/types/enums.go`.

**Exhaustiveness** is checked with the usual method for pattern matching: the cases form a table with one column, the value being matched. For each variant, the rows that accept it are kept and the patterns for what the variant holds become new columns in front; the table is complete if that is true, in turn, for every variant. A column in which no row looks inside the value is dropped without being split, which is what makes the method end for an enum that holds itself. When a table is found empty, the path taken to it is the example in the error message.

**Lowering.** The parser cannot tell `Shape.Circle(2.0)` from a method call, so it is an ordinary call until desugaring, which knows what `Shape` is. There:

| Source | Becomes |
|--------|---------|
| `Shape.Circle(2.0)`, `Shape.Empty` | a `VariantExpr`: enum, variant, values |
| `Shape.Circle` not called | a lambda that makes one |
| `case Shape.Rect(w, 0.0): body` | `if value is Shape.Rect && value.Rect#1 == 0.0 { let w = value.Rect#0; body }` |
| the last case of a complete match | the final `else`, without a test |

A value of a variant is only read after the test that the value is that variant, which `&&` guarantees.

**At runtime** a value of an enum is an object that carries the name `Enum.Variant` and holds its values as the fields `"0"`, `"1"`, ... The bytecode is `NEWOBJ` with a third operand of 2, which marks the object as a variant: the VM compares two such objects by content and prints them as they are made. The test for a variant is the same host function as `value is Type`, with the variant's name as what to look for, and a test or cast to the enum type lists the names of all its variants.

A generic enum is not duplicated per type argument: `Option<Int>` and `Option<String>` share one set of methods, as generic structs do.

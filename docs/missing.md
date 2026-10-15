# What's Missing

What Lumen does not do yet and should, most useful first in each part. It is short on purpose: a feature goes here when programs written in Lumen keep needing it, not because another language has it. What Lumen leaves out by choice is at the end, with the reason.

Sizes are rough: **S** is an afternoon, **M** touches several stages of the compiler, **L** needs design first.

## Language

| Missing | Today | Size |
|---------|-------|------|
| Names of a test that did not hold, for the code after it: `if !(value is Ok(n)) { return; }` then `n` | The names of `is` exist in the block it guards; use a `match`, or `value!` | M |
| Fields private to a module | A `pub struct` shows all its fields | M |

## Checks

| Missing | Today | Size |
|---------|-------|------|
| Type arguments found from where a value goes: `let xs = none();` | `none<Int>()` is written, or `let xs: [Int] = none();` | M |

## Runtime

| Missing | Today | Size |
|---------|-------|------|
| Tasks on several processors | Tasks take turns on one processor ([tasks.md](tasks.md)) | L |
| Waiting on several channels at once, and waits that give up after a time | A task waits on one channel until it has something | M |
| A collector that does not stop the program | Pauses of some tens of milliseconds with millions of objects alive | L |

## Tooling and packages

| Missing | Today | Size |
|---------|-------|------|
| Renaming and finding every use of a name in the editor | Hover, definition, completion and diagnostics are there ([editor.md](editor.md)) | M |
| Stepping through the source in `lumen debug` | It steps instructions and shows registers by number | M |
| A registry of packages | Packages come from git repositories ([packages.md](packages.md)) | L |
| C calling back into the program, and calling C from a task | A program calls C from `main` ([ffi.md](ffi.md)) | M |
| Errors of every stage in one run | Type errors show once the syntax errors are fixed | M |

## Left out on purpose

*   **Exceptions.** A failure is a `Result`, passed on with `?`; a bug stops the program, and `attempt` survives one where it must ([errors.md](errors.md)).
*   **Overloading.** One name, one function: `sorted(before)`, and `sortedTexts()` for texts in their own order.
*   **Assigning a loop's variables.** `for i, item in items` does not let `i` or `item` be assigned: neither would move the loop or change the array. `items[i] = ...` changes the array, `while` controls the position.
*   **`i++` as a value.** It is a statement, `i += 1`; there is no `++i` and no `xs[i++]`.
*   **Null tests on a variable outside functions.** Any function may change one between two lines, so a test against `null` says nothing about the next: use `?.`, `!`, or copy it into a local variable.
*   **Integers of other sizes.** An `Int` is 48 bits (±140 737 488 355 327) and never wraps; there are no unsigned or sized integers.

When an item is done, remove it here and update the status table in the [language spec](language-spec.md#13-implementation-status).

# When Things Go Wrong

There are two ways for something to go wrong in a program, and Lumen handles them differently.

| | What it is | What happens |
|---|---|---|
| **A failure that is part of the job** | A file that is not there, input that makes no sense, a number out of range | The function **returns** it, as a `Result`. Whoever calls it has to look; the compiler checks |
| **A failure that should never happen** | An index outside an array, a division by zero, a state the program was sure it could not reach | The program **stops**, with a message that says where. `attempt` is for the few places that must survive it |

A complete program using both: [`examples/failures.lum`](../examples/failures.lum).

## Returning a Failure: `Result`

`Result<T, E>` is an enum of the standard library: the value a function produced, or the reason there is none.

```lumen
import std.result.{Result};

enum Problem { Empty, NotANumber(String) }

fn count(text: String) -> Result<Int, Problem> {
    if text == "" { return Result.Err(Problem.Empty); }
    let n = strings.parseInt(text);
    if n == null { return Result.Err(Problem.NotANumber(text)); }
    return Result.Ok(n);
}
```

The caller takes it apart with a `match`, which has to handle both:

```lumen
match count(input) {
    case Result.Ok(n): { println("ordered", n); }
    case Result.Err(Problem.Empty): { println("nothing was typed"); }
    case Result.Err(Problem.NotANumber(text)): { println(text, "is not a number"); }
}
```

or with one of its methods:

| Method | Gives |
|--------|-------|
| `isOk()`, `isErr()` | |
| `value() -> T?` | The value, or `null` |
| `reason() -> E?` | The reason, or `null` |
| `orElse(fallback) -> T` | The value, or the fallback |
| `map<U>(convert) -> Result<U, E>` | The value turned into another; a reason passes through |
| `mapErr<F>(convert) -> Result<T, F>` | The reason turned into another; a value passes through |
| `then<U>(step) -> Result<U, E>` | A next step that can fail too: runs only if there is a value; the first reason met is the one that comes out |
| `expect(message) -> T` | The value; **stops the program** if there is none. For where a failure would be a mistake in the program |

```lumen
fn order(text: String) -> Result<Order, Problem> {
    return count(text).then(limited).map(fn(n: Int) -> Order { return Order { count: n }; });
}
```

**The reason can be any type.** A text is the least useful: a program cannot act on it. An enum lets the caller tell the cases apart; a struct can carry whatever helps.

### An optional or a `Result`?

`T?` says "there may be nothing", and that is all `strings.indexOf` or `list.first()` have to say. `Result` is for when there is a *why*: `strings.parseInt` returns an optional, `io.readFile` a `Result`.

## A Trace for an Error That Is a Value

An error that stops the program says where it happened. An error that is returned can too: `result.stack(skip)` gives the calls in progress, to keep in the error.

```lumen
import std.result;
import std.result.{Location};

struct ConfigError {
    key: String;
    message: String;
    stack: [Location];
}

fn invalid(key: String, message: String) -> ConfigError {
    // stack(1): from whoever called this function, not from here
    return ConfigError { key: key, message: message, stack: result.stack(1) };
}
```

`result.trace(message, stack)` writes it over several lines:

```
brightness should be a number, not 'x'
  at config.lum:54:27 in function 'number'
  called from config.lum:61:12 in function 'percent'
  called from config.lum:96:17 in function 'main'
```

A `Location` has `function`, `file`, `line`, `column`, and `describe()`.

## Errors of the Standard Library: `io.Error`

Reading and writing files returns a `Result` whose reason is a struct:

```lumen
pub struct Error {
    kind: ErrorKind;        // NotFound, PermissionDenied, IsDirectory, NotDirectory, AlreadyExists, Other
    operation: String;      // "read", "write" or "append"
    path: String;
    message: String;        // as the operating system puts it: "no such file or directory"
    stack: [Location];      // where the program asked for the file
}
```

The `kind` is for the program, the rest for telling a person:

```lumen
match io.readFile("settings.conf") {
    case Result.Ok(text): { load(text); }
    case Result.Err(error): {
        match error.kind {
            case ErrorKind.NotFound: { useDefaults(); }
            default: { println(error.describe()); }     // cannot read settings.conf: permission denied
        }
    }
}
```

`error.trace()` adds where it was asked for.

## Stopping: `panic`

`panic` is a builtin. It stops the program with a message, written from its arguments as `println` writes them:

```lumen
fn take(queue: Queue) -> Int {
    if arrays.len(queue.items) == 0 {
        panic("the queue is empty after", queue.taken, "items");
    }
    ...
}
```

It is for a state the program cannot do anything sensible about: a mistake to be fixed, not a case to be handled. A function that can fail in ways its caller should deal with returns a `Result` instead.

The compiler knows that nothing comes after a `panic`: a function may end with one in place of a `return`, a test against `null` followed by a `panic` leaves the value non-null afterwards, and code after one is reported as unreachable.

```lumen
fn area(shape: Shape) -> Int {
    match shape {
        case Shape.Square(side): { return side * side; }
        case Shape.Unknown: { panic("no area for an unknown shape"); }
    }
}
```

## What a Runtime Error Looks Like

A program that stops, by `panic` or by any failure the virtual machine finds, is reported the way a compile error is: a code, the message, the place with its source line underlined, a suggestion where there is a usual fix, then the calls that led there.

```
[ERROR] R0002: index out of bounds: 7 (length 3)
  --> orders.lum:12:17 in function 'pick'
      |
 12 |     return items[index];
      |                 ^
      |
      = Suggestion: Compare the index with arrays.len(...) before using it, or use arrays.at, which gives null instead of failing
  called from orders.lum:31:20 in function 'total'
  called from orders.lum:48:13 in function 'main'
```

The exit code is 1.

| Code | Kind | Raised by |
|------|------|-----------|
| `R0001` | Panic | A call of `panic` |
| `R0002` | Bounds | An index outside an array or a text |
| `R0003` | Cast | `value as Type` on a value of another type |
| `R0004` | Arithmetic | A division by zero, a result too large for an `Int` |
| `R0005` | Depth | Calls nested deeper than the limit (`--max-depth`) |
| `R0006` | Memory | More memory than the limit (`--heap-mb`) |
| `R0007` | Library | A function of the standard library given arguments it cannot work with |
| `R0008` | Other | Anything else; in practice, bytecode written by hand |

A function calling itself gives the same line many times: it is shown once, with how often. Beyond twelve different calls the rest is counted, and the outermost, where it all started, is named.

## Surviving: `attempt`

`attempt` runs a function. If the function stops, for any of the reasons above, the program does not: the failure comes back as a value.

```lumen
import std.result.{Result, Kind, attempt};

for i, job in jobs {
    match attempt(job) {
        case Result.Ok(n): { println("job", i, "gave", n); }
        case Result.Err(failure): { println("job", i, "failed:", failure.message); }
    }
}
```

The function takes no arguments; wrap what you want to run: `attempt(fn() -> Int { return risky(input); })`. For a function run for what it does rather than for a value, `guard(action)` returns the `Failure`, or `null` if it completed.

A `Failure` holds everything the error message would have shown:

| Field | |
|-------|---|
| `kind: Kind` | `Panic`, `Bounds`, `Cast`, `Arithmetic`, `Depth`, `Memory`, `Library`, `Other`: the kinds of the table above, as an enum to `match` on |
| `message: String` | |
| `at: Location` | Where it happened |
| `stack: [Location]` | The calls in progress, innermost first; `stack[0]` is `at` |

and `describe()` (one line) and `trace()` (with the calls).

### What is left behind

Everything done before the failure stays done: an array that was pushed to still has the item, a field that was assigned keeps its value. Nothing after it happened. If a function changes several things that belong together and may stop half way, what `attempt` hands back is the half-way state; make the changes on a copy, or in an order that is safe to interrupt.

Closures made inside the function that failed keep working, with the variables they captured.

The calls that were abandoned do not count against the depth limit any more, and what they allocated is collected like any other garbage.

### When to use it

Rarely. It is a boundary, not a way to handle errors:

*   a loop that must outlive the jobs it runs;
*   a tool that runs tests and reports the ones that fail;
*   the edge of code you do not control.

A failure that is expected is better returned as a `Result` by the function that meets it: the caller is then made to deal with it, by the compiler, instead of having to remember to wrap the call.

## What Is Not There

*   **`try` / `catch`.** By design: there is one way to return a failure (`Result`) and one to survive a stop (`attempt`). A second error mechanism next to `Result` would have to be chosen between at every function.
*   **A shorter way to pass a failure on.** Returning the reason of a failed step to one's own caller takes a `match` or a `then`; there is no `count(text)?`.
*   **Cleanup that runs whatever happens** (`defer`, `finally`). Nothing in the standard library keeps something open between calls yet, so nothing is leaked by stopping half way; that changes the day there are files that are opened and closed.
*   **`attempt` with arguments.** The function it runs takes none.
*   **A common shape for errors.** `io.Error`, `Failure` and an error type of your own each have a `describe()`, by convention only: traits cannot yet be required of a type parameter, so nothing can ask for "any error".

## How It Works

**The error.** A failure of a running program is a `vm.RuntimeError`: a kind, a message and the call stack, taken at the moment it is raised from the frames of the virtual machine (each frame remembers the instruction that made the call, and the debug information of an instruction gives its file, line, column, source line and function). It is turned into the same `Diagnostic` the compiler's errors are shown with, which is why both look alike.

**`panic`** is a host function that returns an error of its own type; the instruction that called it turns that into a `RuntimeError` of kind panic at the place of the call. The type checker treats a call of it as the end of a path, by name.

**`attempt`** needs two things the virtual machine did not have. The first is calling back into the program from a native function: the instruction loop (`VM.run`) can be entered again for one function and left when that function returns (`VM.callFunction`). The second is undoing a call that failed (`VM.unwind`): the frames above the one that asked are dropped, the variables of those frames that a closure still points at are moved to the closure, as they are when a function returns normally, and the registers, the function and the instruction of the frame that asked are put back.

The native behind `attempt`, `std.result.protect`, does only that and reports whether the function completed. What the function returned, or how it failed, is fetched by two more natives, and the `Result` and the `Failure` are built by Lumen code in `std/result.lum`: values of enums and structs are made by the compiler's own code, which knows what each must hold.

**`result.stack`** is the same call stack, asked for without a failure.

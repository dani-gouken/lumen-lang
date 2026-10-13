# The Standard Library

The library is built into `lumen`. There is nothing to install and no project file is needed: any program can import its modules.

```lumen
import std.strings;
import std.dict;

fn main() -> Int {
    let counts = dict.empty<Int>();
    for word in strings.words("to be or not to be") {
        counts.set(word, (counts.get(word) ?? 0) + 1);
    }
    println(counts.keys(), counts.values());      // ["to", "be", "or", "not"] [2, 2, 1, 1]
    return 0;
}
```

A longer program using most of it is [`examples/words.lum`](../examples/words.lum).

| Module | For |
|--------|-----|
| [`std.math`](#stdmath) | Numbers, conversion between `Int` and `Float` |
| [`std.strings`](#stdstrings) | Taking text apart, searching, changing, parsing numbers |
| [`std.arrays`](#stdarrays) | Functions on arrays: slice, map, filter, sort, ... |
| [`std.list`](#stdlist) | `List<T>`: an array with methods |
| [`std.dict`](#stddict) | `Dict<V>`: values found by a text key |
| [`std.option`](#stdoption) | Chaining steps on values that may be `null` |
| [`std.result`](#stdresult) | `Result<T, E>`, surviving a failure with `attempt`, traces |
| [`std.io`](#stdio) | Reading input, files, the environment |
| [`std.time`](#stdtime) | The clock, waiting, measuring |
| [`std.random`](#stdrandom) | Random numbers |

The modules are imported and used like any other ([modules.md](modules.md)): `import std.math;` then `math.abs(-3)`, or `import std.math.{abs};` then `abs(-3)`.

`print`, `println`, `toString`, `push` and `panic` are not part of the library: they are builtins, available without an import.

## Conventions

**Nothing instead of failure.** A function that may have no answer returns an optional, and `null` means "none": the position of something not found, the first item of an empty array, a text that is not a number. Where there is a reason worth knowing, as for a file that cannot be read, it returns a [`Result`](#stdresult) with the reason in it. The type checker then makes sure the program deals with it:

```lumen
let n = strings.parseInt(line);      // Int?
if n != null { total += n; }         // an Int in here
let port = strings.parseInt(text) ?? 8080;
```

**A failure is for a mistake in the program.** Asking for characters 2 to 9 of a three-letter text, the square root of a negative number, or a random number below zero stops the program, like an index out of bounds does. The message names the function as the program called it and the line that called it:

```
[ERROR] R0007: std.strings.substring: range 0..5 is outside a text of 3 characters
  --> main.lum:8:19 in function 'shorten'
     |
 8 |     return strings.substring(name, 0, 5) + "...";
     |                   ^^^^^^^^^^
  called from main.lum:13:13 in function 'main'
```

**Arrays are not changed** by the functions of `std.arrays`; they return new ones. The exceptions say so (`swap`, `random.shuffle`). A `List` is the reverse: its methods change it, unless they return a new list.

**Ranges** are given as `from, to`: `from` is included, `to` is not. Positions count from 0. In texts they count characters, not bytes, like `text[i]` and `strings.len(text)`.

---

## `std.math`

`Int` and `Float` do not mix in arithmetic (`1 + 2.5` is a type error). Convert explicitly; going to an `Int` there are four ways to lose the fraction.

| Function | Result |
|----------|--------|
| `toFloat(n: Int) -> Float` | The same number, as a `Float` |
| `floor(x: Float) -> Int` | The largest whole number not above `x`: `floor(2.7)` is 2, `floor(-2.7)` is -3 |
| `ceil(x: Float) -> Int` | The smallest whole number not below `x`: `ceil(2.1)` is 3 |
| `round(x: Float) -> Int` | The nearest whole number; halves go away from zero: `round(2.5)` is 3 |
| `trunc(x: Float) -> Int` | `x` without its fraction: `trunc(-2.7)` is -2 |

The four fail if the number does not fit in an `Int`.

**Whole numbers**

| Function | Result |
|----------|--------|
| `abs(n)` | `n` without its sign |
| `min(a, b)`, `max(a, b)` | The smaller, the larger |
| `clamp(n, low, high)` | `n`, or the nearest limit if it is outside them |
| `sign(n)` | -1, 0 or 1 |
| `pow(base, exponent)` | `base` multiplied by itself `exponent` times. A negative exponent gives 0, except for a base of 1 or -1 |
| `gcd(a, b)` | Greatest common divisor, never negative: `gcd(12, 18)` is 6 |
| `lcm(a, b)` | Least common multiple: `lcm(4, 6)` is 12 |
| `isEven(n)`, `isOdd(n)` | |

**Numbers with a fraction**

| Function | Result |
|----------|--------|
| `pi`, `e` | The constants |
| `fabs(x)`, `fmin(a, b)`, `fmax(a, b)` | As `abs`, `min`, `max`, for `Float` |
| `sqrt(x)` | Square root. Fails for a negative number |
| `powf(base, exponent)` | `base` raised to `exponent`, which need not be whole: `powf(2.0, 0.5)` |
| `exp(x)`, `log(x)` | e raised to `x`; the natural logarithm, which fails for zero and negative numbers |
| `sin(x)`, `cos(x)`, `tan(x)` | Angles in radians |
| `atan2(y, x)` | The angle of the point (x, y), from -π to π |
| `radians(degrees)`, `degrees(radians)` | Between the two units of angle |

```lumen
// The length of the diagonal of a 3 by 4 rectangle
let a = 3;
let b = 4;
let diagonal = math.sqrt(math.toFloat(a * a + b * b));    // 5.0
```

---

## `std.strings`

| Function | Result |
|----------|--------|
| `len(text) -> Int` | How many characters the text has. This is the only way to ask: a text has no `.length` |

**Taking apart**

| Function | Result |
|----------|--------|
| `substring(text, from, to) -> String` | The characters from `from` up to, not including, `to`. Fails if the range is not inside the text |
| `from(text, from)` | From a position to the end |
| `first(text, count)`, `last(text, count)` | The first or last `count` characters; the whole text if it is shorter |
| `split(text, separator) -> [String]` | The pieces between occurrences of the separator: `split("a,b", ",")` is `["a", "b"]`. An empty separator gives the characters |
| `chars(text) -> [String]` | The characters |
| `lines(text) -> [String]` | The lines, without their line ends |
| `words(text) -> [String]` | What is between spaces, tabs and line ends |

**Putting together**

| Function | Result |
|----------|--------|
| `join(parts, separator) -> String` | The parts with the separator between them |
| `repeat(text, count)` | The text `count` times over. Fails for a negative count |
| `padLeft(text, width)`, `padRight(text, width)` | The text with spaces before or after it, up to `width` characters |
| `reverse(text)` | The characters in the opposite order |

Building a long text piece by piece with `+=` copies everything so far at each step. Collect the pieces in an array and `join` them once.

**Searching**

| Function | Result |
|----------|--------|
| `indexOf(text, part) -> Int?` | Where `part` first occurs, or `null` |
| `lastIndexOf(text, part) -> Int?` | Where it last occurs, or `null` |
| `contains(text, part)`, `startsWith(text, start)`, `endsWith(text, end)` | |
| `count(text, part) -> Int` | How many times `part` occurs, not counting overlaps |

**Changing**

| Function | Result |
|----------|--------|
| `replace(text, old, with)` | Every occurrence of `old` replaced. Fails if `old` is empty |
| `upper(text)`, `lower(text)` | |
| `capitalize(text)` | The first character in upper case |
| `trim(text)` | Without the spaces, tabs and line ends at both ends |

**Asking**

| Function | Result |
|----------|--------|
| `isEmpty(text)` | No characters at all |
| `isBlank(text)` | Empty, or only spaces, tabs and line ends |
| `isDigits(text)` | Every character is a digit, and there is at least one |

**Numbers and characters**

| Function | Result |
|----------|--------|
| `parseInt(text) -> Int?` | The whole number the text spells, or `null`: `parseInt("42")`, `parseInt(" -7 ")`. Spaces around it are ignored |
| `parseFloat(text) -> Float?` | Likewise: `parseFloat("2.5")`, `parseFloat("1e3")` |
| `code(text) -> Int` | The number of the first character: `code("A")` is 65. Fails for `""` |
| `fromCode(code) -> String` | The character with that number: `fromCode(65)` is `"A"` |
| `hash(text) -> Int` | A number that is the same for equal texts and almost always different otherwise. Never negative |

Going the other way, any value becomes text with the builtin `toString`.

```lumen
let line = "name = Ada Lovelace";
let at = strings.indexOf(line, "=");
if at != null {
    let key = strings.trim(strings.first(line, at));          // "name"
    let value = strings.trim(strings.from(line, at + 1));     // "Ada Lovelace"
}
```

---

## `std.arrays`

Generic functions on plain arrays. Type arguments are inferred: `arrays.reversed([1, 2, 3])`. Where there is nothing to infer from, write them: `arrays.first<Int>([])`.

| Function | Result |
|----------|--------|
| `len<T>(items) -> Int` | How many items the array holds. This is the only way to ask: an array has no `.length` |

**Making**

| Function | Result |
|----------|--------|
| `range(from, to) -> [Int]` | The whole numbers from `from` up to, not including, `to` |
| `fill<T>(count, value) -> [T]` | `count` times the same value. This is how to get an array of a given size |
| `copy<T>(items)` | A new array with the same items |
| `slice<T>(items, from, to)` | The items from `from` up to, not including, `to`. Positions outside the array are brought back to its ends |
| `concat<T>(first, second)` | One after the other |
| `reversed<T>(items)` | |
| `without<T>(items, index)` | Without the item at `index` |
| `inserted<T>(items, index, value)` | With `value` at `index`, the rest moved up |

**One item**

| Function | Result |
|----------|--------|
| `first<T>(items) -> T?`, `last<T>(items) -> T?` | `null` for an empty array |
| `at<T>(items, index) -> T?` | The item there, or `null`. `items[index]` fails instead |
| `find<T>(items, wanted) -> T?` | The first item the function accepts |
| `findIndex<T>(items, wanted) -> Int?` | Its position |
| `indexOf<T>(items, wanted) -> Int?` | The position of the first item equal to `wanted`, or `null` |

**Asking**

| Function | Result |
|----------|--------|
| `isEmpty<T>(items)` | |
| `contains<T>(items, wanted)` | An item is equal to `wanted` |
| `any<T>(items, wanted)` | The function accepts at least one item |
| `all<T>(items, wanted)` | The function accepts every item. True for an empty array |
| `count<T>(items, wanted) -> Int` | How many it accepts |

**Every item**

| Function | Result |
|----------|--------|
| `map<T, U>(items, convert) -> [U]` | What the function makes of each item |
| `filter<T>(items, wanted) -> [T]` | The items the function accepts |
| `fold<T, A>(items, start, combine) -> A` | One value from all the items: starts with `start`, then `combine(value so far, item)` for each |
| `each<T>(items, action)` | Calls the function with each item |

**Order**

| Function | Result |
|----------|--------|
| `sorted<T>(items, before) -> [T]` | In order, where `before(a, b)` says whether `a` must come first. Items equal in that order keep the order they had |
| `swap<T>(items, a, b)` | **Changes the array**: the two items change places |

**Numbers and texts**

| Function | Result |
|----------|--------|
| `sum(numbers) -> Int` | |
| `smallest(numbers) -> Int?`, `largest(numbers) -> Int?` | `null` for an empty array |
| `sortedNumbers(numbers)`, `sortedTexts(texts)` | Smallest first; alphabetical |

**Equal**, for `indexOf` and `contains`, means what `==` means: numbers, texts and booleans are equal when they have the same value; an array or a struct is only equal to itself. To look for a struct by what is in it, use `find` with a function.

```lumen
let people = [Person { name: "Ada", age: 36 }, Person { name: "Linus", age: 21 }];

let adults = arrays.filter(people, fn(p: Person) -> Bool { return p.age >= 18; });
let names = arrays.map(adults, fn(p: Person) -> String { return p.name; });
let oldestFirst = arrays.sorted(people, fn(a: Person, b: Person) -> Bool { return a.age > b.age; });
let totalAge = arrays.fold(people, 0, fn(sum: Int, p: Person) -> Int { return sum + p.age; });
```

---

## `std.list`

`List<T>` is an array in a struct, with the functions of `std.arrays` as methods. Calls read left to right and can be chained.

```lumen
import std.list;

let names = list.of(["ada", "linus"]);
names.add("grace");
let long = names.filter(fn(n: String) -> Bool { return strings.len(n) > 3; }).sorted(alphabetical);
```

| Function | Result |
|----------|--------|
| `list.empty<T>() -> List<T>` | An empty list. The type has to be written |
| `list.of<T>(items) -> List<T>` | A list of the items of an array. The list has its own copy |

**Methods that change the list**

| Method | Does |
|--------|------|
| `add(value)`, `addAll(values)` | Adds at the end |
| `set(index, value)` | Replaces the item there. Fails if there is none |
| `insert(index, value)` | Puts `value` at `index`, moving the rest up |
| `removeAt(index) -> T?` | Takes the item out and returns it; `null` if there is none there |
| `pop() -> T?` | Takes out the last item; `null` for an empty list |
| `clear()` | |
| `sort(before)` | Puts the list itself in order |

**Methods that read**

| Method | Result |
|--------|--------|
| `size()`, `isEmpty()` | |
| `get(index) -> T?` | The item there, or `null` |
| `first() -> T?`, `last() -> T?` | |
| `find(wanted) -> T?`, `findIndex(wanted) -> Int?` | |
| `contains(value)`, `indexOf(value) -> Int?` | By equality; see [`std.arrays`](#stdarrays) |
| `any(wanted)`, `all(wanted)`, `count(wanted)` | |
| `fold<A>(start, combine) -> A` | |
| `each(action)` | |
| `toArray() -> [T]` | An array of the items; changing it does not change the list |

**Methods that return a new list**

`map<U>(convert)`, `filter(wanted)`, `sorted(before)`, `reversed()`, `slice(from, to)`.

`map` and `fold` have a type parameter of their own, for what they produce. It is worked out from the function passed, or can be written: `names.map<Int>(fn(n: String) -> Int { return strings.len(n); })`.

The field `items` is the array itself, for a `for` loop: `for name in names.items { ... }`.

---

## `std.dict`

`Dict<V>` (a dictionary) finds values by a text key. It is a hash table: finding a key takes about the same time however many there are.

```lumen
import std.dict;

let ages = dict.empty<Int>();
ages.set("ada", 36);
ages.set("linus", 21);

let age = ages.get("ada");          // Int?
if age != null { println(age + 1); }
println(ages.get("nobody") ?? 0);
```

| Function / method | Result |
|-------------------|--------|
| `dict.empty<V>() -> Dict<V>` | An empty dictionary. The type of the values has to be written |
| `set(key, value)` | Sets the value of a key, replacing the one it had |
| `get(key) -> V?` | The value, or `null` if the key has none |
| `has(key) -> Bool` | |
| `remove(key) -> Bool` | Removes a key; whether it was there |
| `size()`, `isEmpty()` | |
| `keys() -> [String]` | The keys, in the order they were first set |
| `values() -> [V]` | The values, in the order of their keys |
| `each(action)` | Calls `action(key, value)` for each |

Keys are compared exactly: `"a"` and `"A"` are two keys, and `""` is a key.

The values can be of any type, including arrays, structs and other dictionaries (`dict.empty<Dict<Int>>()`). To use something else than a text as key, make a text of it (`toString(id)`).

```lumen
// Group words by their first letter
let groups = dict.empty<[String]>();
for word in words {
    let letter = strings.first(word, 1);
    let group = groups.get(letter);
    if group == null {
        groups.set(letter, [word]);
    } else {
        push(group, word);
    }
}
```

---

## `std.option`

Functions on optionals: `T?`, a value or `null`. They carry "or nothing" through a series of steps, so that each step does not need its own null test.

```lumen
import std.option;

// Without: one test per step
let line = io.readLine();
if line != null {
    let n = strings.parseInt(line);
    if n != null {
        println(n * 2);
    }
}

// With: each step runs only if the one before gave a value
let doubled = option.map(option.then(io.readLine(), strings.parseInt), fn(n: Int) -> Int { return n * 2; });
```

| Function | Result |
|----------|--------|
| `map<T, U>(value: T?, convert: fn(T) -> U) -> U?` | The value turned into another, or `null` if there was none |
| `then<T, U>(value: T?, step: fn(T) -> U?) -> U?` | The same, for a step that may itself have no answer |
| `filter<T>(value: T?, wanted) -> T?` | The value if the function accepts it, `null` otherwise |
| `mapOr<T, U>(value: T?, fallback: U, convert) -> U` | The value turned into another, or `fallback` |
| `or<T>(first: T?, second: T?) -> T?` | The first value if there is one, otherwise the second |
| `orElse<T>(value: T?, make: fn() -> T) -> T` | The value, or what the function makes. Unlike `value ?? fallback`, the fallback is only worked out when needed |
| `each<T>(value: T?, action)` | Calls the function with the value, if there is one |
| `isSome<T>(value)`, `isNone<T>(value)` | |
| `values<T>(items: [T?]) -> [T]` | The items of an array that are not `null` |

`map` is for a step that always has an answer (`fn(Int) -> String`), `then` for one that may not (`fn(String) -> Int?`). Using `map` with the second kind would give an optional of an optional; `then` keeps it flat.

What comes out is an ordinary optional: `??` gives it a fallback, and a test against `null` makes it a plain value inside the test.

```lumen
// The name of a user's manager, through two lookups and a field that may be null
let manager = option.then(users.get(key), fn(u: User) -> String? { return u.manager; });
let found = option.then(manager, fn(k: String) -> User? { return users.get(k); });
println(option.mapOr(found, "no manager", fn(u: User) -> String { return u.name; }));
```

Type arguments are worked out from the values and functions passed. A function of the library can be passed directly (`strings.parseInt`, `toString`); a method cannot, so a call of one is wrapped: `fn(k: String) -> User? { return users.get(k); }`.

There is no separate `Option` type with `Some` and `None`: `T?` is that type, and the type checker already refuses to use one as a plain value. These functions add the chaining.

---

## `std.result`

For failures: returning one with its reason, and surviving one that would stop the program. The whole subject is in [errors.md](errors.md); this is the reference.

**`Result<T, E>`**: `enum Result<T, E> { Ok(T), Err(E) }`, the value a function produced or the reason there is none.

| Method | Gives |
|--------|-------|
| `isOk()`, `isErr()` | |
| `value() -> T?`, `reason() -> E?` | One or the other, or `null` |
| `orElse(fallback) -> T` | The value, or the fallback |
| `map<U>(convert)`, `mapErr<F>(convert)` | The value, or the reason, turned into another |
| `then<U>(step) -> Result<U, E>` | A next step that can fail too |
| `expect(message) -> T` | The value; stops the program if there is none |

**Failures that stop the program**

| Function | Does |
|----------|------|
| `attempt<T>(action: fn() -> T) -> Result<T, Failure>` | Runs the function; if it stops, the program goes on and the failure is returned |
| `guard(action: fn() -> Void) -> Failure?` | The same, for a function run for what it does |
| `stack(skip: Int) -> [Location]` | The calls in progress, innermost first, leaving out `skip` of them: for an error of your own that says where it comes from |
| `trace(message, stack) -> String` | The message and the calls, over several lines |

| Type | Holds |
|------|-------|
| `Failure` | `kind: Kind`, `message`, `at: Location`, `stack: [Location]`; `describe()`, `trace()` |
| `Kind` | `Panic`, `Bounds`, `Cast`, `Arithmetic`, `Depth`, `Memory`, `Library`, `Other` |
| `Location` | `function`, `file`, `line`, `column`; `describe()` |

The builtin `panic(values...)`, which stops the program, needs no import.

---

## `std.io`

Output goes through the builtins `print` and `println`. Reading and writing a file can fail for reasons outside the program; those functions return a [`Result`](#stdresult) and never stop the program.

| Function | Result |
|----------|--------|
| `readLine() -> String?` | A line of input, without its line end. `null` at the end of the input |
| `ask(question) -> String?` | Prints the question, then reads the answer |
| `readLines() -> [String]` | Every remaining line of the input |
| `readFile(path) -> Result<String, Error>` | The whole content of a file, or why it cannot be read |
| `readFileLines(path) -> Result<[String], Error>` | Its lines |
| `writeFile(path, text) -> Result<Int, Error>` | Writes the text, replacing what was in the file. How many characters were written |
| `appendFile(path, text) -> Result<Int, Error>` | Adds to the end of the file, creating it if needed |
| `exists(path) -> Bool` | |
| `remove(path) -> Bool` | Deletes a file. Whether it worked |
| `printError(text)` | Writes to the error output. No line end is added |
| `env(name) -> String?` | An environment variable, or `null` if it is not set |

An `io.Error` says what kind of failure it was, for the program to act on, and the rest for telling a person:

| Field | |
|-------|---|
| `kind: ErrorKind` | `NotFound`, `PermissionDenied`, `IsDirectory`, `NotDirectory`, `AlreadyExists`, `Other` |
| `operation` | `"read"`, `"write"` or `"append"` |
| `path` | As it was given |
| `message` | As the operating system puts it: `no such file or directory` |
| `stack: [Location]` | Where the program asked for the file |

`describe()` gives one sentence (`cannot read notes.txt: no such file or directory`), `trace()` adds where.

Input is what the user types, or what is piped in: `lumen run sum.lum < numbers.txt`.

```lumen
import std.io;
import std.io.{ErrorKind};
import std.result.{Result};
import std.strings;

fn main() -> Int {
    match io.readFile("notes.txt") {
        case Result.Ok(content): {
            for i, line in strings.lines(content) {
                println(strings.padLeft(toString(i + 1), 4), line);
            }
        }
        case Result.Err(error): {
            match error.kind {
                case ErrorKind.NotFound: { println("no notes yet"); }
                default: { io.printError(error.describe() + "\n"); return 1; }
            }
        }
    }
    return 0;
}
```

When the reason does not matter: `io.readFile(path).orElse("")`.

Paths are relative to the directory the program is run from, not to the program's file.

---

## `std.time`

| Function | Result |
|----------|--------|
| `now() -> Int` | The time now, in milliseconds since 1 January 1970 |
| `seconds() -> Int` | The same, in whole seconds |
| `ticks() -> Int` | Microseconds since the program started. For measuring: it never jumps when the clock is set |
| `sleep(milliseconds)` | Waits |
| `stopwatch() -> Stopwatch` | Starts measuring |

A `Stopwatch` has `microseconds()`, `milliseconds()` and `restart()`.

```lumen
let watch = time.stopwatch();
work();
println("took", watch.milliseconds(), "ms");
```

There are no dates yet: nothing turns a time into a year, month and day.

---

## `std.random`

| Function | Result |
|----------|--------|
| `seed(n)` | Makes the numbers that follow the same on every run, for the same `n` |
| `below(limit) -> Int` | From 0 up to, not including, `limit`. Fails if the limit is below 1 |
| `between(low, high) -> Int` | From `low` to `high`, both included |
| `float() -> Float` | From 0.0 up to, not including, 1.0 |
| `chance(probability) -> Bool` | True with that probability: `chance(0.25)` one time in four |
| `pick<T>(items) -> T?` | One of the items; `null` for an empty array |
| `shuffle<T>(items)` | **Changes the array**: puts its items in a random order |

Without `seed` the numbers differ on every run. With it they repeat, which is what a test or a replay needs.

These numbers are for games and simulations. They are not fit for passwords or keys.

---

## How It Is Built

The library has two halves.

**Lumen source.** Each module is a `.lum` file in `internal/stdlib/lib/`, compiled into the `lumen` binary. Most of the library is written here, in the language itself: `math.gcd`, `arrays.sorted`, all of `List` and `Dict`. These are ordinary modules; a program importing one gets its functions compiled along with its own.

**Native functions.** What cannot be written in Lumen, because it needs the machine (files, the clock) or would be too slow (searching a text), is declared in the module without a body:

```lumen
pub extern fn sqrt(x: Float) -> Float;
```

and provided by a Go function in the virtual machine (`internal/vm/native_*.go`) registered under the function's full name, `std.math.sqrt`. See [`extern fn`](modules.md#functions-the-runtime-provides).

The package `std` is found like any [package](project.md#packages-modules-from-somewhere-else); the only difference is that its folder is inside the binary. In messages its files appear as `<std>/strings.lum`. A project that lists a package named `std` in its `lumen.json` gets that one instead, whole.

A folder called `std` among a program's own modules is not reachable as `std.something`: the name belongs to the library.

## What Is Missing

*   **Dates**, and formatting of numbers (`toString(3.0)` prints `3`; there is no way to ask for two decimals).
*   **Maps with keys that are not text**, and sets.
*   **Folders**: listing one, creating one. **Reading a file in pieces**: `readFile` takes all of it.
*   **Program arguments**: a program cannot see what follows its name on the command line.
*   **Networking**, running other programs.
*   **Equality by content** for arrays and structs: `==`, and so `contains` and `indexOf`, only say whether two of them are the same object.
*   **`for` over a `List` or a `Dict`** directly: loop over `list.items`, `dict.keys()`.
*   Everything is compiled into the program that imports a module, used or not; there is no removal of unused functions yet.

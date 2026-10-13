# The Standard Library

The library is built into `lumen`. There is nothing to install and no project file is needed: any program can import its modules.

```rust
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

A longer program using the core of it is [`examples/words.lum`](../examples/words.lum). Whole programs built on the rest: a [to-do list](../examples/todo/main.lum) kept in a JSON file, a [report on a web server's log](../examples/logs/main.lum), a [sales report](../examples/sales/main.lum) from a CSV file, a [web service](../examples/notes/main.lum) with its tests, and a [build](../examples/build.lum) that works on several targets at once.

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
| [`std.time`](#stdtime) | The clock, dates, waiting, measuring |
| [`std.random`](#stdrandom) | Random numbers, identifiers |
| [`std.path`](#stdpath) | Paths taken apart and put together |
| [`std.fs`](#stdfs) | Folders: listing, walking, creating, copying |
| [`std.os`](#stdos) | Program arguments, exit, running other programs |
| [`std.format`](#stdformat) | Numbers, sizes, durations and tables written for people |
| [`std.json`](#stdjson) | Reading, searching and writing JSON |
| [`std.regex`](#stdregex) | Regular expressions |
| [`std.csv`](#stdcsv) | Tables as text |
| [`std.set`](#stdset) | `Set<T>`: values without repeats |
| [`std.encoding`, `std.hash`](#stdencoding-and-stdhash) | Base64, hex, URL encoding; SHA, MD5, HMAC |
| [`std.http`](#stdhttp) | Asking web servers, and being one |
| [`std.task`](#stdtask) | Doing several things at once: tasks and channels |
| [`std.reflect`](#stdreflect) | Asking a value what it is while the program runs: its type, its fields |
| [`std.compiler`](#stdcompiler) | Asking the compiler about a program: its mistakes, its declarations, what a name is |
| [`std.ffi`](#stdffi) | Memory for the functions of C libraries |
| [`std.test`](#stdtest) | Tests and checks |
| [`std.ops`](#stdops) | The traits behind operators, loops and brackets; `sorted`, `max`, `join` for any type that has them |

The modules are imported and used like any other ([modules.md](modules.md)): `import std.math;` then `math.abs(-3)`, or `import std.math.{abs};` then `abs(-3)`.

`print`, `println`, `toString`, `push` and `panic` are not part of the library: they are builtins, available without an import.

## Conventions

**Nothing instead of failure.** A function that may have no answer returns an optional, and `null` means "none": the position of something not found, the first item of an empty array, a text that is not a number. Where there is a reason worth knowing, as for a file that cannot be read, it returns a [`Result`](#stdresult) with the reason in it. The type checker then makes sure the program deals with it:

```rust
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

**Arrays are not changed** by the functions of `std.arrays`; they return new ones. The exceptions say so (`pop`, `removeAt`, `insertAt`, `clear`, `swap`, `random.shuffle`). A `List` is the reverse: its methods change it, unless they return a new list.

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

```rust
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
| `byteLength(text) -> Int` | How many bytes it takes in UTF-8: `"é"` is one character and two bytes |

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
| `cut(text, separator) -> (String, String)?` | What is before the first separator, and what is after it: `let (name, value) = strings.cut("port=80", "=") ?? ("", "");` |
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

```rust
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
| `without<T>(items, index)` | Without the item at `index`. An index outside the array changes nothing |
| `inserted<T>(items, index, value)` | With `value` at `index`, the rest moved up. An index at or past the end puts it at the end; a negative one changes nothing |

**Changing the array itself**

| Function | Does |
|----------|------|
| `pop<T>(items) -> T?` | Takes the last item out and returns it; `null` for an empty array. Takes the same time however long the array is |
| `removeAt<T>(items, index) -> T?` | Takes the item at `index` out and returns it, the items after it moving down. `null`, and nothing changed, if there is none there |
| `insertAt<T>(items, index, value)` | Puts `value` at `index`, the items from there on moving up. An index at or past the end puts it at the end; a negative one changes nothing, as with `inserted` |
| `clear<T>(items)` | Takes every item out |

With `push`, these make an array a stack (`push`, `pop`) or a queue (`push`, `removeAt(items, 0)`). They change the array they are given, so everything that holds it sees the change, and they make no new array: one made shorter keeps the room it had, to grow into again.

```rust
let stack = [1, 2];
push(stack, 3);
let top = arrays.pop(stack);          // 3, an Int?; stack is [1, 2]
arrays.insertAt(stack, 0, 0);         // [0, 1, 2]
let first = arrays.removeAt(stack, 0);

while !arrays.isEmpty(stack) {        // until nothing is left
    let item = arrays.pop(stack);
}
```

Two things to know:

- **`null` from an array of optionals.** In a `[Int?]` an item can itself be `null`, and `pop` then gives `null` for that item as it does for an empty array. Ask `isEmpty` (or `len`) before, as the loop above does, instead of waiting for `null`.
- **Not inside `for ... in` over the same array.** That loop counts the items once, before it starts; an array made shorter under it is read past its end, which stops the program (`index out of bounds`). Use `while` with an index that only moves on when nothing was removed, go down from the end, or loop over `arrays.copy(items)`:

```rust
let i = 0;
while i < arrays.len(tasks) {
    if tasks[i].done { arrays.removeAt(tasks, i); } else { i++; }
}
```

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

**Pieces and groups**

| Function | Result |
|----------|--------|
| `take(items, count)`, `drop(items, count)` | The first `count` items; all but those |
| `chunks(items, size) -> [[T]]` | Pieces of `size`, the last one shorter if it does not divide |
| `flatten(arrays) -> [T]`, `flatMap(items, convert)` | |
| `groupBy(items, name) -> Dict<[T]>` | The items sorted into groups by a text worked out from each |
| `countBy(items, name) -> Dict<Int>` | How many items give each text |
| `unique(texts)`, `uniqueBy(items, name)` | Without repeats; the first of each is kept |
| `partition(items, wanted) -> ([T], [T])` | The items a function accepts, and the others: `let (even, odd) = arrays.partition(numbers, math.isEven);` |
| `zip(first, second) -> [(A, B)]` | The items of two arrays side by side, as many as the shorter has |
| `unzip(pairs) -> ([A], [B])` | The other way round |
| `sortedBy(items, rank)`, `sortedByText(items, name)` | In the order of a number, or of a text, worked out from each |
| `maxBy(items, rank) -> T?`, `minBy(items, rank) -> T?` | |
| `sumOf(items, amount) -> Int` | |

**Equal**, for `indexOf` and `contains`, means what `==` means: numbers, texts and booleans are equal when they have the same value; an array or a struct is only equal to itself. To look for a struct by what is in it, use `find` with a function.

```rust
let people = [Person { name: "Ada", age: 36 }, Person { name: "Linus", age: 21 }];

let adults = arrays.filter(people, p -> p.age >= 18);
let names = arrays.map(adults, p -> p.name);
let oldestFirst = arrays.sorted(people, (a, b) -> a.age > b.age);
let totalAge = arrays.fold(people, 0, (sum, p) -> sum + p.age);
```

---

## `std.list`

`List<T>` is an array in a struct, with the functions of `std.arrays` as methods. Calls read left to right and can be chained.

```rust
import std.list;

let names = list.of(["ada", "linus"]);
names.add("grace");
let long = names.filter(n -> strings.len(n) > 3).sorted(alphabetical);

for name in names { println(name); }    // a list goes where an array does
names[0] = "Ada";                       // as with an array, a position that is not there stops the program
println(names[0]);
```

| Function | Result |
|----------|--------|
| `list.empty<T>() -> List<T>` | An empty list. The type has to be written |
| `list.of<T>(items) -> List<T>` | A list of the items of an array. The list has its own copy |

**Methods that change the list**

They change the array the list holds, as the functions of [`std.arrays`](#stdarrays) that change an array do. As there, do not take items out of a list inside a `for ... in` over that same list: the loop counted them before it started. Loop over `names.toArray()`, or use `while` with `names.size()`.

| Method | Does |
|--------|------|
| `add(value)`, `addAll(values)` | Adds at the end |
| `set(index, value)` | Replaces the item there. Fails if there is none |
| `insert(index, value)` | Puts `value` at `index`, moving the rest up. An index at or past the end adds it at the end; a negative one changes nothing |
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

`map` and `fold` have a type parameter of their own, for what they produce. It is worked out from the function passed, or can be written: `names.map<Int>(n -> strings.len(n))`.

The field `items` is the array itself, for a `for` loop: `for name in names.items { ... }`.

---

## `std.dict`

`Dict<V>` (a dictionary) finds values by a text key: finding a key takes about the same time however many there are.

Its keys and values are two arrays, in the order the keys were set. Up to eight keys are simply looked through, so a small dictionary is three objects and costs next to nothing to make; a program can have one per record, per scope, per request. Past eight the machine keeps an index of where each key is.

```rust
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
| `getOr(key, fallback) -> V` | The value, or the fallback |
| `update(key, start, change)` | Sets the key to what a function makes of its value, or of `start` if it has none. Counting: `counts.update(word, 0, n -> n + 1)` |
| `has(key) -> Bool` | |
| `remove(key) -> Bool` | Removes a key; whether it was there |
| `size()`, `isEmpty()` | |
| `keys() -> [String]` | The keys, in the order they were first set |
| `sortedKeys() -> [String]` | In alphabetical order |
| `entries() -> [(String, V)]` | Each key with its value: `for (name, age) in ages.entries() { ... }` |
| `values() -> [V]` | The values, in the order of their keys |
| `each(action)` | Calls `action(key, value)` for each |

A dictionary takes brackets and loops: `ages["ada"]` is what `get` gives, a value or `null`; `ages["ada"] = 36;` is `set`; and `for name in ages { ... }` goes through the keys. Counting reads `counts[word] = (counts[word] ?? 0) + 1;`.

Keys are compared exactly: `"a"` and `"A"` are two keys, and `""` is a key.

The values can be of any type, including arrays, structs and other dictionaries (`dict.empty<Dict<Int>>()`). To use something else than a text as key, make a text of it (`toString(id)`).

```rust
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

```rust
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
let doubled = option.map(option.then(io.readLine(), strings.parseInt), n -> n * 2);
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

```rust
// The name of a user's manager, through two lookups and a field that may be null
let manager = option.then(users.get(key), u -> u.manager);
let found = option.then(manager, k -> users.get(k));
println(option.mapOr(found, "no manager", u -> u.name));
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
| `readBytes(count) -> String?` | The next `count` bytes of the input, as a text: for input that says first how long it is. `null` if the input ends before |
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
| `operation` | `"read"`, `"write"`, `"append"`; from [`std.fs`](#stdfs) and [`std.os`](#stdos) also `"list"`, `"copy"`, `"run"`, ... |
| `path` | As it was given |
| `message` | As the operating system puts it: `no such file or directory` |
| `stack: [Location]` | Where the program asked for the file |

`describe()` gives one sentence (`cannot read notes.txt: no such file or directory`), `trace()` adds where.

Input is what the user types, or what is piped in: `lumen run sum.lum < numbers.txt`.

```rust
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

Paths are relative to the directory the program is run from, not to the program's file. For a file that comes with the program, start from [`os.program()`](#stdos).

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

```rust
let watch = time.stopwatch();
work();
println("took", watch.milliseconds(), "ms");
```

Lengths of time are constants in milliseconds: `time.second`, `minute`, `hour`, `day`, `week`, as in `time.sleep(2 * time.second)`.

### Dates

A moment is a number, which is what `now` gives. A `Date` is that moment told the way people do, in some time zone.

| Function / method | Result |
|-------------------|--------|
| `time.today() -> Date` | Now, where the program runs |
| `time.utc(moment)`, `time.local(moment)` | A moment as told in UTC, or where the program runs |
| `time.date(year, month, day)`, `dateTime(year, month, day, hour, minute, second)` | In UTC. Numbers out of range carry over: the 32nd of January is the 1st of February |
| `time.parse(text) -> Date?` | Reads `2026-10-13`, `2026-10-13 14:30`, `2026-10-13T14:30:00+02:00`, and the dates of e-mail and of web server logs |
| `timestamp() -> Int` | The moment |
| `format(pattern)`, `iso()`, `isoTime()` | See below; `2026-10-13`; `2026-10-13T14:30:00Z` |
| `plus(milliseconds)`, `plusDays(n)`, `plusMonths(n)`, `startOfDay()` | New dates |
| `daysUntil(other) -> Int` | Days of the calendar, not lengths of 24 hours |
| `isBefore(other)`, `isAfter(other)`, `isSameDay(other)` | |
| `time.daysIn(year, month)`, `isLeapYear(year)`, `monthName(month)` | |

A `Date` has the fields `year`, `month` (1 for January), `day`, `hour`, `minute`, `second`, `millisecond`, `weekday` (an enum, `Weekday.Monday` to `Weekday.Sunday`, with `name()` and `isWeekend()`), `dayOfYear` and `offset` (minutes ahead of UTC).

In a pattern for `format`:

| | | | |
|---|---|---|---|
| `YYYY` 2026 | `YY` 26 | `MMMM` October | `MMM` Oct |
| `MM` 10 | `M` 10 | `DD` 03 | `D` 3 |
| `dddd` Tuesday | `ddd` Tue | `HH` 09 | `H` 9 |
| `hh` 09 (of 12) | `h` 9 | `A` AM | `mm` 05 |
| `ss` 07 | `SSS` 042 | `Z` +02:00 | `[text]` kept as it is |

```rust
let today = time.today();
println(today.format("dddd D MMMM YYYY"));       // Tuesday 13 October 2026
println(today.plusDays(30).iso(), today.daysUntil(time.date(2027, 1, 1)));
```

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
| `uuid() -> String` | A random identifier: `3f2b8c1e-9d4a-4f6b-8a1c-0e5d7b9f2a4c` |

Without `seed` the numbers differ on every run. With it they repeat, which is what a test or a replay needs.

These numbers are for games and simulations. They are not fit for passwords or keys.

---

## `std.path`

Paths taken apart and put together. Nothing here touches the disk. The separator is that of the system the program runs on.

| Function | Result |
|----------|--------|
| `join(parts) -> String` | The parts put together and tidied: `join(["a", "b/", "../c.txt"])` is `a/c.txt` |
| `inside(folder, name) -> String` | `inside("notes", "todo.txt")` is `notes/todo.txt` |
| `base(path)`, `dir(path)` | The last part; all but the last part |
| `extension(path)` | With its dot: `.txt`; `""` if there is none |
| `stem(path)` | The last part without its extension |
| `withExtension(path, extension)` | `withExtension("a/b.md", ".html")` is `a/b.html` |
| `parts(path) -> [String]` | `"a/b/c.txt"` gives `["a", "b", "c.txt"]` |
| `clean(path)` | The shortest path that means the same |
| `isAbsolute(path)`, `absolute(path)` | |
| `matches(pattern, name) -> Bool` | `*` stands for any characters, `?` for one: `matches("*.txt", "todo.txt")` |
| `separator()` | `/`, or `\` on Windows |

---

## `std.fs`

Folders and the files in them. Reading and writing the *content* of a file is in [`std.io`](#stdio). Like there, what can fail returns a `Result` whose reason is an `io.Error`.

| Function | Result |
|----------|--------|
| `list(folder) -> Result<[Entry], Error>` | What a folder holds, sorted by name; one level |
| `walk(folder) -> Result<[String], Error>` | The path of every file under it, however deep |
| `find(folder, pattern) -> Result<[String], Error>` | Those whose name fits: `fs.find("src", "*.lum")` |
| `info(path) -> Result<Entry, Error>` | What is known about one file or folder |
| `isDir(path)`, `isFile(path)` | |
| `makeDir(folder) -> Result<String, Error>` | Creates it and the folders above it. One already there is fine |
| `removeAll(path) -> Result<String, Error>` | Deletes a file, or a folder with all it holds. Nothing there is fine |
| `rename(from, to)`, `copy(from, to)` | Give the new path |
| `currentDir()`, `tempDir()` | |
| `makeTempDir(prefix) -> Result<String, Error>` | A new, empty folder for the program's own use |

An `Entry` has `name`, `path`, `kind` (`Kind.File` or `Kind.Folder`), `size` in bytes, `modified` (a moment, as `time.now`), and `isFile()`, `isFolder()`.

```rust
match fs.list("notes") {
    case Result.Ok(entries): {
        for entry in entries { println(entry.name, format.bytes(entry.size)); }
    }
    case Result.Err(error): { println(error.describe()); }     // cannot list notes: no such file or directory
}
```

---

## `std.os`

| Function | Result |
|----------|--------|
| `args() -> [String]` | What followed the program on the command line: `lumen run notes.lum add milk` gives `["add", "milk"]` |
| `program() -> String` | The file the program was run from. Files that come with a program are found from it: `path.inside(path.dir(os.program()), "data.csv")` |
| `exit(status)` | Ends the program now. Nothing after it runs, not even an `attempt` around it |
| `platform()` | `"linux"`, `"darwin"`, `"windows"` |
| `hostname()` | |
| `environment() -> Dict<String>` | Every environment variable. One alone: `io.env` |
| `run(program, arguments) -> Result<Output, Error>` | Runs another program and waits for it |
| `runWithInput(program, arguments, input)` | The same, with a text for it to read |

An `Output` has `status`, `out`, `err`, and `succeeded()`, `lines()`. A program that ran and reports a failure is not an error: look at `output.status`. The error is for a program that could not be started.

**Arguments on the command line.** What follows the file is for the program, except the options of `lumen` itself (`--heap-mb`, `--max-depth`). After a lone `--` everything is for the program:

```
lumen run todo.lum add "Buy milk" --due=2026-10-20
lumen run --heap-mb=64 report.lum -- --heap-mb=this-one-is-mine
lumen run . list                 # a project: name its folder, then the arguments
```

---

## `std.format`

Numbers and tables written for people.

| Function | Result |
|----------|--------|
| `fixed(x, digits)` | `fixed(3.14159, 2)` is `3.14`, `fixed(1.0, 2)` is `1.00` |
| `grouped(n)`, `groupedWith(n, separator)` | `1,234,567` |
| `money(amount)` | `1,234,567.50` |
| `percent(share, digits)` | `percent(0.125, 1)` is `12.5%` |
| `padded(n, width)` | `padded(7, 3)` is `007` |
| `hex(n)`, `binary(n)`, `inBase(n, base)`, `fromBase(text, base) -> Int?` | |
| `bytes(size)` | `512 B`, `1.5 MB` |
| `duration(milliseconds)` | `350ms`, `2.5s`, `1m 05s`, `1h 02m 03s`, `3d 04h` |
| `ordinal(n)` | `1st`, `2nd`, `11th`, `22nd` |
| `plural(count, one, many)` | `1 file`, `3 files` |
| `pad(text, width, align)` | `Align.Left`, `Align.Right`, `Align.Center` |
| `table(titles, rows) -> String` | Columns as wide as their widest value; a column of numbers is set to the right |
| `tableWith(titles, aligns, rows)` | The same, saying how each column is set |
| `bar(value, largest, width)` | A bar of a chart, in `#` |

```rust
print(format.table(["name", "age"], [["ada", "36"], ["grace", "45"]]));
```
```
name   age
-----  ---
ada     36
grace   45
```

---

## `std.json`

A document is a value of an enum, so a `match` takes it apart and the compiler checks every kind is handled:

```rust
pub enum Json { Null, Bool(Bool), Number(Float), String(String), Array([Json]), Object(Dict<Json>) }
```

| Function / method | Result |
|-------------------|--------|
| `json.parse(text) -> Result<Json, ParseError>` | The error has `message`, `line`, `column`, `describe()` |
| `get(key) -> Json?`, `at(index) -> Json?` | Into an object, into an array |
| `find(path) -> Json?` | Keys and positions separated by dots: `find("user.tags.0")` |
| `text()`, `number()`, `int()`, `bool()`, `items()`, `fields()` | The value if it is of that kind, else `null` |
| `isNull()` | |
| `a == b`, `a != b` | By content: the same kind of value, equal items in the same order, equal values under the same keys |
| `stringify()`, `pretty()` | On one line; over several, indented |
| `json.int(n)`, `json.texts(items)`, `json.ints(items)`, `json.array(items, convert)` | Making documents |
| `json.object()` | An object built a key at a time: `.text(k, v)`, `.int`, `.number`, `.bool`, `.none`, `.set(k, json)`, then `.done()` |
| `json.textOf`, `intOf`, `numberOf`, `boolOf`, `itemsOf` | The same accessors as functions, to hand to `arrays.map` and the like |

```rust
let document = json.parse(text).expect("a document");
let name = document.get("name")?.text() ?? "nobody";
let firstTag = document.find("user.tags.0")?.text();

let reply = json.object().text("name", name).int("visits", 3).set("tags", json.texts(tags)).done();
println(reply.stringify());         // {"name":"ada","visits":3,"tags":["a","b"]}
```

The keys of an object stay in the order they were written. Numbers are `Float`s; `int()` gives one that is whole.

---

## `std.regex`

Regular expressions, in the syntax of RE2: no look-behind and no back-references, and a search that always takes time proportional to the text. `\w`, `\d` and `\b` know the letters and digits of English only.

| Function / method | Result |
|-------------------|--------|
| `regex.compile(pattern) -> Result<Regex, PatternError>` | Checks a pattern |
| `regex.must(pattern) -> Regex` | The same for a pattern written in the program: a wrong one stops it |
| `matches(text) -> Bool` | Whether it fits somewhere |
| `find(text) -> Match?`, `findAll(text) -> [Match]` | |
| `capture(text) -> [String]?` | What the groups of the first match took |
| `all(text) -> [String]` | The text of every match |
| `replace(text, with)` | `$1` is what the first group took |
| `replaceWith(text, convert)` | Each match replaced by what a function makes of it |
| `split(text) -> [String]` | The pieces between the matches |
| `regex.escape(text)` | A text as a pattern that matches exactly it |

A `Match` has `text`, `start`, `end` (positions in characters), `groups`, and `group(index)`, `named(name)` for a group written `(?P<name>...)`.

In a text of a program a backslash is written twice, and `${` would start a value to insert: write `"\\d+"` and `"\${name}"`.

```rust
let date = regex.must("(\\d{4})-(\\d{2})-(\\d{2})");
date.replace("on 2026-10-13", "$3/$2/$1")           // on 13/10/2026
```

---

## `std.csv`

| Function / method | Result |
|-------------------|--------|
| `csv.parse(text) -> Result<[[String]], ParseError>` | The rows, each an array of its values |
| `csv.parseWith(text, separator)` | With `";"` or `"\t"` |
| `csv.write(rows)`, `csv.writeWith(rows, separator)` | Rows as text, quoting what needs it |
| `csv.table(text) -> Result<Table, ParseError>` | The first row names the columns; every row must have a value for each |
| `table.column(name) -> [String]?`, `cell(row, name) -> String?` | |
| `table.records() -> [Dict<String>]` | Each row as a dictionary from column to value |
| `table.where(wanted) -> Table` | The rows a function accepts |
| `table.write()`, `size()`, `header`, `rows` | |

---

## `std.set`

Values without repeats. A set tells its items apart by a text worked out from each; for texts and numbers that is taken care of.

| Function / method | Result |
|-------------------|--------|
| `set.texts()`, `set.numbers()` | Empty sets |
| `set.of(texts)`, `set.ofNumbers(numbers)` | From an array |
| `set.by(key)` | Of anything, given how to tell two apart: `set.by(fn(p: Point) -> String { return "${p.x},${p.y}"; })` |
| `add(item) -> Bool`, `addAll(items)`, `remove(item) -> Bool`, `has(item)` | `add` says whether the item was new |
| `size()`, `isEmpty()`, `items()`, `copy()` | Items come in the order they were first added |
| `union(other)`, `intersection(other)`, `difference(other)`, `filter(wanted)` | New sets |
| `isSubsetOf(other)`, `a == b` | Two sets are equal when they hold the same items, in whatever order |
| `for item in set` | Goes through the items |

---

## `std.encoding` and `std.hash`

| Function | Result |
|----------|--------|
| `encoding.toBase64(text)`, `fromBase64(encoded) -> String?` | Reading back gives `null` for a text not in that encoding |
| `toBase64Url`, `fromBase64Url` | The variant that is safe in an address |
| `toHex`, `fromHex` | Two hexadecimal digits per byte |
| `toUrl`, `fromUrl` | As a value in the query of an address |
| `bytes(text) -> [Int]`, `fromBytes(bytes) -> String?` | The bytes of a text in UTF-8 |
| `hash.sha256(text)`, `sha1`, `md5` | Hexadecimal digits |
| `hash.crc32(text) -> Int` | |
| `hash.hmacSha256(key, text)` | A signature |

These hashes are made to be fast: they are for telling contents apart and for signing, not for storing passwords.

---

## `std.http`

**Asking a server.**

```rust
match http.get("https://example.com/api/users/1") {
    case Result.Ok(response): { println(response.status, response.body); }
    case Result.Err(error): { println(error.describe()); }
}

let created = http.to(Method.Post, url).header("Authorization", token).json(document).timeout(5000).send();
```

A server that answers "not found" has answered: that is a `Response` with status 404. An `Error` is for when there was no answer at all, and its `kind` says why: `Timeout`, `Refused`, `UnknownHost`, `BadUrl`, `Other`.

| Function / method | Result |
|-------------------|--------|
| `http.get(url)`, `post(url, body, contentType)`, `postJson(url, document)` | `Result<Response, Error>` |
| `http.to(method, url)` | A request built a part at a time: `.header`, `.query`, `.body`, `.json`, `.timeout`, then `.send()` |
| `response.status`, `body`, `headers` | Header names are in lower case |
| `response.ok()`, `header(name)`, `json()` | |

**Being one.** A handler is a function from a `Request` to a `Response`. A `Router` chooses one from the method and the path:

```rust
let app = http.router();
app.get("/hello/:name", fn(request: Request) -> Response {
    return http.text("hello, ${request.params.get("name") ?? "you"}");
});
app.post("/notes", createNote);
http.serve(8080, app.handler()).expect("a free port");
```

| Function / method | Result |
|-------------------|--------|
| `http.serve(port, handler)` | Answers every request, each in a [task](#stdtask) of its own, for as long as the program runs |
| `http.listen(port) -> Result<Server, Error>` | Port 0 lets the system choose: `server.port` |
| `server.handleAll(handler)` | The same on a server that is listening, until it is closed |
| `server.handle(count, handler)`, `next()`, `reply(response)`, `close()` | That many requests one after the other, or one at a time by hand |
| `server.answer(request, response)` | Answers a request `next` gave, whichever it was: several can be in progress |
| `router.get`, `post`, `put`, `patch`, `delete`, `on(method, path, handler)` | `:name` takes one part of the path into `request.params`; a last `*` takes the rest, as `"rest"` |
| `router.answer(request) -> Response` | The answer, without a server: how routes are tested |
| `http.text(body)`, `html(body)`, `sendJson(status, document)`, `respond(status, body)`, `notFound()`, `redirect(to)` | Answers |
| `response.with(name, value)` | One more header |
| `request.method`, `path`, `query`, `headers`, `body`, `params`, `sender`, `header(name)`, `json()` | |

A path nobody takes is answered with 404, one taken only for other methods with 405. A handler that stops, by a `panic` or any other failure, does not stop the server: its request is answered with 500 and the failure goes to the error output.

With `serve` and `handleAll` a handler that waits, for another server or for the clock, does not keep the other requests waiting; handlers still run one at a time between their waits ([tasks.md](tasks.md)). `handle` answers one request after the other. The server listens on this machine only (`127.0.0.1`).

A complete service with its tests: [`examples/notes`](../examples/notes/main.lum).

---

## `std.ops`

The traits that give a struct or an enum the syntax of the builtin types, and what a generic function asks of its type parameters. They are explained, with how to implement them, in [traits.md](traits.md#operators-loops-and-brackets).

| Trait | Method to write | Gives |
|-------|-----------------|-------|
| `Equal` | `equals(other: Self) -> Bool` | `==`, `!=` |
| `Ordered` | `compare(other: Self) -> Int` | `<`, `<=`, `>`, `>=`; `isBefore`, `isAfter` |
| `Add`, `Subtract`, `Multiply`, `Divide` | `plus`, `minus`, `times`, `dividedBy` | `+ - * /`, `+=` ... |
| `Show` | `show() -> String` | What `println`, `toString` and `"${...}"` write |
| `Iterable<T>` | `iterate() -> [T]` | `for item in value` |
| `Indexed<K, V>`, `IndexedSet<K, V>` | `at(key) -> V`, `put(key, value)` | `value[key]`, `value[key] = v;` |

`Int`, `Float`, `String` and `Bool` implement `Equal` and `Show`; all but `Bool` implement `Ordered`.

For any type that implements the trait named:

| Function | Result |
|----------|--------|
| `max<T: Ordered>(a, b)`, `min(a, b)` | The first when neither comes first |
| `largest<T: Ordered>(items) -> T?`, `smallest(items) -> T?` | `null` for an empty array |
| `sorted<T: Ordered>(items) -> [T]` | From first to last; equal items keep their order |
| `contains<T: Equal>(items, wanted)`, `indexOf(items, wanted) -> Int?` | |
| `same<T: Equal>(first, second) -> Bool` | Equal items in the same order |
| `join<T: Show>(items, separator) -> String` | The texts of the items |

```rust
import std.ops;
import std.ops.{Ordered};

struct Version { major: Int; minor: Int; }
impl Ordered for Version {
    fn compare(other: Version) -> Int {
        if self.major != other.major { return self.major - other.major; }
        return self.minor - other.minor;
    }
}

let newest = ops.largest(versions);
if newest != null && newest > installed { ... }
```

---

## `std.task`

Functions running at the same time, and channels between them. All of it, with how a program of several tasks ends, is in [tasks.md](tasks.md).

```rust
let pages = task.map(urls, url -> http.get(url));          // all asked at once

let lines = task.channel<String>(16);
task.run(fn() {
    for name in files { lines.send(io.readFile(name).orElse("")); }
    lines.close();
});
lines.each(fn(text: String) { println(strings.len(text)); });
```

One task runs at a time, until it waits: for a task, a channel, `time.sleep`, the network, a line of input, another program. Tasks share everything and need no locks, as long as a change does not have a wait in its middle.

| Function / method | Result |
|-------------------|--------|
| `task.start(work) -> Task<T>` | Runs `work` on its own; returns at once |
| `task.run(action) -> Task<Bool>` | The same for a function that returns nothing |
| `task.wait() -> Result<T, Failure>` | What it returned, or the failure that stopped it |
| `task.expect(message)`, `isDone()` | |
| `task.all(tasks) -> Result<[T], Failure>` | Every value in order, or the first failure |
| `task.map(items, work) -> Result<[U], Failure>` | One task per item |
| `task.pause()`, `task.current()` | Lets the others have a turn; the number of the task that asks |
| `task.channel<T>(capacity) -> Channel<T>` | Capacity 0: a send waits for its receive |
| `channel.send(value)`, `receive() -> T?`, `close()` | `receive` gives `null` once closed and empty |
| `channel.each(action)`, `drain()`, `length()`, `isClosed()` | |

A task that stops does not stop the program: whoever waits for it gets the failure. If nobody ever does, the failure ends the program once the other tasks are done. Tasks that all wait for each other end it with error `R0009`.

---

## `std.reflect`

Asking a value what it is, while the program runs: for code that prints, saves or compares a struct it was never told about.

```rust
struct User { name: String; age: Int; }
let user = User { name: "ada", age: 36 };

for name in reflect.fields(user) {
    println(name, "=", reflect.get(user, name));       // age = 36, name = ada
}
reflect.set(user, "age", 37);
println(reflect.dump(user));
```

| Function | Result |
|----------|--------|
| `reflect.kind(value) -> Kind` | `Null`, `Bool`, `Int`, `Float`, `String`, `Array`, `Tuple`, `Struct`, `Variant`, `Function` |
| `reflect.typeName(value)` | `"Int"`, `"array"`, and for a struct or an enum its name: `"User"`, `"geometry.Shape"` |
| `reflect.fields(value) -> [String]` | The fields of a struct value, in alphabetical order |
| `reflect.hasField(value, name)`, `get(value, name) -> Any` | `get` gives `null` for a field that is not there |
| `reflect.set(value, name, to)` | Changes a field; the new value has to be of the same sort as the old |
| `reflect.variant(value) -> String?` | For a value of an enum, which variant: `"Circle"` |
| `reflect.values(value) -> [Any]` | What a variant or a tuple holds, in order |
| `reflect.items(array) -> [Any]`, `toDict(value) -> Dict<Any>` | |
| `reflect.dump(value)` | Any value written out in full, over several lines |

A value read this way has the type `Any`: test it with `is` or cast it with `as` to use it.

**What the machine knows is less than what the compiler knew.** A struct value knows its struct's name and its fields; a value of an enum its variant and what it holds. A field does not know its declared type, only the value in it now: that is all `set` can check, and why a field holding `null` accepts anything. An array does not know what its items were meant to be. Methods are not listed, nor fields that hold a function. A generic struct has one name whatever it holds: `"std.list.List"`. For types as written, ask the compiler.

---

## `std.compiler`

The front of the compiler, asked about a program instead of told to run it. Every function takes the path of a file and its text: the text is what is looked at, the path says where its modules are and how places are named.

```rust
let source = io.readFile("main.lum").expect("a file");
for problem in compiler.check("main.lum", source) {
    println(problem.describe());        // main.lum:4:9: error E1001: undefined identifier 'totl'
}
```

| Function | Result |
|----------|--------|
| `compiler.check(path, source) -> [Problem]` | Errors and warnings: `severity`, `code`, `message`, `at`, `endColumn`, `suggestion` |
| `compiler.symbols(path, source) -> [Symbol]` | What the file declares: `kind`, `name`, `detail`, `container`, `line`, `column` |
| `compiler.describe(path, source, line, column) -> Description?` | The name at that place: its `signature`, the `doc` above its declaration, where it is `declared` |
| `compiler.complete(path, source, line, column) -> [Suggestion]` | What could be written there: after a dot, what the value before it has. The line being typed need not make sense |
| `compiler.open(path, text)`, `close(path)` | A file being edited: importing it gives this text, not what is on disk |

Lines and columns count from 1, columns in characters. `describe` answers `null` for a program with errors that keep it from being understood; `complete` then works from the file without the line being typed.

It is what the [language server](editor.md) is written with, and serves as well for a linter or a documentation generator: [`internal/lsp/server.lum`](../internal/lsp/server.lum) is the longest example.

---

## `std.ffi`

Memory outside the program, for the functions of C libraries that want an address ([ffi.md](ffi.md)). An address is an `Int`. Nothing checks one: reading where there is nothing ends the program at once.

| Function | What it does |
|----------|--------------|
| `alloc(bytes: Int) -> Int` | The address of that many bytes, all 0 |
| `free(address: Int)` | Gives back what `alloc`, `cstring`, `int32s` or `float32s` returned. Freeing 0 does nothing |
| `cstring(text: String) -> Int` | A copy of a text as C wants it, that stays until freed |
| `text(address: Int) -> String` | The text of C at an address |
| `readInt8`, `readUInt8`, `readInt16`, `readUInt16`, `readInt32`, `readUInt32`, `readInt64`, `readPointer` `(address, offset) -> Int` | The whole number that many bytes after the address |
| `readFloat32`, `readFloat64` `(address, offset) -> Float` | The same for a float |
| `writeInt8` ... `writeFloat64`, `writePointer` `(address, offset, value)` | Writes one. A number that does not fit is refused |
| `int32s(values: [Int]) -> Int`, `float32s(values: [Float]) -> Int` | The address of a copy of an array, as an `int *` or a `float *`. To be freed |
| `readInt32s(address, count) -> [Int]`, `readFloat32s(address, count) -> [Float]` | An array read from there |

```lumen
let squares = ffi.alloc(6 * 4);
defer ffi.free(squares);
fillSquares(squares, 6);                 // extern "mylib" fn fillSquares(out: Pointer, count: Int32);
println(ffi.readInt32s(squares, 6));     // [0, 1, 4, 9, 16, 25]
```

A program run in the sandbox is not permitted this module.

---

## `std.test`

```rust
import std.test;

fn main() -> Int {
    return test.suite("prices")
        .test("a total adds up", fn() { test.equal(total([1, 2, 3]), 6); })
        .test("an empty basket costs nothing", fn() { test.equal(total([]), 0); })
        .run().exitCode();
}
```
```
prices
  ok    a total adds up
  FAIL  an empty basket costs nothing
        expected 0, got 1
        at prices.lum:6
1 passed, 1 failed
```

A check that does not hold stops its test; the suite goes on with the next and reports at the end. So does any other failure in a test: an index out of bounds is a failed test, not the end of the run.

| Function / method | |
|-------------------|---|
| `test.suite(name)`, `.test(name, body)` | |
| `.run() -> Report` | Runs and prints. `.check()` runs without printing |
| `report.ok()`, `passed()`, `failed()`, `exitCode()`, `lines()`, `results` | |
| `equal(got, want)`, `notEqual` | As `==` has it |
| `same(got, want)` | Two arrays with equal items in the same order |
| `near(got, want, tolerance)` | For numbers with a point |
| `isTrue(condition, what)`, `isFalse` | |
| `present(value, what) -> T`, `absent(value, what)` | For optionals; `present` gives the value |
| `contains(text, part)` | |
| `fails(kind, action) -> Failure` | The function must stop with a failure of that [kind](errors.md#surviving-attempt) |
| `fail(message)` | For a check of your own |

---

## How It Is Built

The library has two halves.

**Lumen source.** Each module is a `.lum` file in `internal/stdlib/lib/`, compiled into the `lumen` binary. Most of the library is written here, in the language itself: `math.gcd`, `arrays.sorted`, all of `List` and `Dict`. These are ordinary modules; a program importing one gets its functions compiled along with its own.

**Native functions.** What cannot be written in Lumen, because it needs the machine (files, the clock) or would be too slow (searching a text), is declared in the module without a body:

```rust
pub extern fn sqrt(x: Float) -> Float;
```

and provided by a Go function in the virtual machine (`internal/vm/native_*.go`) registered under the function's full name, `std.math.sqrt`. See [`extern fn`](modules.md#functions-the-runtime-provides).

The package `std` is found like any [package](project.md#packages-modules-from-somewhere-else); the only difference is that its folder is inside the binary. In messages its files appear as `<std>/strings.lum`. A project that lists a package named `std` in its `lumen.json` gets that one instead, whole.

**Natives hand back simple things.** A native function cannot make a struct or a value of an enum: those are made by compiled code, which knows what each must hold. So a native returns a number, a text, or arrays of them, and the Lumen half of the module builds the types the program sees. `json.parse` is the clearest case: Go checks the text and lists its tokens, and thirty lines of Lumen build the tree of `Json` values from them. An operation that fails gives `null` and keeps the reason, which the Lumen half fetches to build its error value.

**The web server** is split the same way, for another reason: only one task of a program runs at a time. Go accepts connections and reads requests, each in its own goroutine, and queues them; the program takes them one at a time, works out each answer in Lumen, in a task of its own if it wants several in progress, and hands it back under the number of its request.

**Tasks** are mostly Lumen as well. The machine provides tasks by number and three things to do with them: suspend the one that asks, resume another, wait for one to end. `Task<T>` and `Channel<T>` are written on those, which is how they come to be typed and collected like any other struct ([tasks.md](tasks.md#how-it-works)).

A folder called `std` among a program's own modules is not reachable as `std.something`: the name belongs to the library.

## What Is Missing

*   **Maps with keys that are not text.** `Dict` takes texts, and `Set` asks for a function that makes a text of an item: nothing can be required of a type parameter yet, so there is no "any type that can be hashed".
*   **Reading a file in pieces**, and files that are not text: `readFile` takes all of it, as a text. The input can be read a number of bytes at a time (`io.readBytes`), a file cannot.
*   **More for tasks.** Several things can be waited for at once ([`std.task`](#stdtask)), on one processor. There is no waiting for the first of several channels, no wait that gives up after some time, and no stopping a task from outside ([tasks.md](tasks.md#what-is-not-there)).
*   **Time zones by name.** A date is told in UTC, where the program runs, or at a fixed offset; there is no "Europe/Paris".
*   **Sockets below HTTP**, HTTPS for a server of your own, and e-mail.
*   **Equality by content for arrays.** A struct says when two of its values are equal by implementing `Equal`; two arrays are still only `==` when they are the same one. `ops.same(first, second)` and `test.same` compare them item by item.
*   Everything is compiled into the program that imports a module, used or not; there is no removal of unused functions yet. A program that imports `std.http` compiles `std.json`, `std.dict` and the rest with it.

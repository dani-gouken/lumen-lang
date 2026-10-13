# The REPL

`lumen repl` is Lumen typed in a line at a time: what is typed runs at once, and what it leaves behind is there for the next line.

```text
$ lumen repl
Lumen. :help says what can be typed here, Ctrl-D leaves.
[1]> import std.strings;
[2]> let name = "ada"
[3]> strings.upper(name)
"ADA"
[4]> fn greet(who: String) -> String {
...>     return "hello, ${who}";
...> }
[5]> greet(name)
"hello, ada"
```

## What can be typed

**Statements**, as in the body of a function. They run at once. The semicolon at the end can be left out.

**An expression**. It is worked out and its value shown, written as it would be in a program: a text in quotes. An expression without a value (`println("hi")`), an assignment and `null` show nothing.

**Declarations**: `fn`, `struct`, `enum`, `trait`, `impl`, `const`, `import`. They stay for the rest of the session. Several can be typed, or pasted, in one input.

**Commands**, which start with a colon and are for the REPL itself:

| Command | What it does |
|---------|--------------|
| `:help` | The list of commands and keys |
| `:vars` | The variables, with their types and values |
| `:decls` | What is declared: functions, types, constants, imports |
| `:type <expression>` | The type of an expression, without running it |
| `:load <file.lum>` | Take the declarations of a file. Its `main` is left out |
| `:forget <name>` | Forget a variable or a declaration |
| `:reset` | Forget everything and start again |
| `:quit` | Leave; so does Ctrl-D |

An input is either declarations or statements, not both: a function cannot be declared between two statements in a program either.

## Variables

A variable declared with `let` is there in the inputs that follow, with its type:

```text
[1]> let total = 0
[2]> total += 5
[3]> total
5
[4]> total = "five"
[ERROR] E2001: cannot assign type string to int
```

Declaring it again makes a new variable, of any type, as a `let` in an inner block does:

```text
[5]> let total = "five"
```

It is one variable for everything that uses it. A function written in one input that counts in it, and the lines typed afterwards, see the same count:

```text
[1]> let count = 0
[2]> let bump = fn() -> Int { count += 1; return count; };
[3]> bump(); bump()
2
[4]> count
2
[5]> count = 100
[6]> bump()
101
```

A variable declared inside a block, a loop or a function is that block's, as always. `:vars` lists the ones that are kept.

## Declaring again

A declaration typed again replaces the one before: that is how a function is corrected.

```text
[1]> fn area(w: Int, h: Int) -> Int { return w + h; }
[2]> area(3, 4)
7
[3]> fn area(w: Int, h: Int) -> Int { return w * h; }
[4]> area(3, 4)
12
```

Everything declared is compiled again with it, so the functions that call `area` call the new one. If they no longer compile, because the new `area` takes three arguments, the input is refused and nothing changes; declare the callers again in the same input.

*   An `impl` replaces the `impl` with the same first line: `impl Point { ... }` replaces the earlier `impl Point { ... }`, with all its methods.
*   A struct, enum or trait declared again is another type under the same name. The variables that held values of the old one are forgotten, and the REPL says which.
*   A function value made before, `let old = area;` or a closure, goes on being the function it was made from.

## Keys

| Key | |
|-----|---|
| Enter | Enters the input; starts a new line instead while a bracket is open |
| Alt-Enter | Starts a new line in any case |
| Tab | Completes the name before the cursor; indents at the start of a line |
| Up, Down | Move between the lines of the input, then through the earlier inputs |
| Left, Right, Home, End, Ctrl-A, Ctrl-E | Move on the line |
| Alt-B, Alt-F, Ctrl-Left, Ctrl-Right | Move by a word |
| Ctrl-K, Ctrl-U, Ctrl-W | Delete to the end of the line, to its start, the word before |
| Ctrl-C | Abandons the input; stops a program that runs |
| Ctrl-D | Leaves, on an empty input |
| Ctrl-L | Clears the screen |

Tab completes variables, declarations and keywords, the names of a module after its dot (`strings.up`), the fields and methods of a variable (`point.`), the variants of an enum (`Colour.`), the modules of the standard library on an `import` line, and the commands.

An earlier input comes back whole, a function with all its lines. Inputs are remembered from one session to the next in `~/.lumen_history`, or in the file named by `LUMEN_HISTORY`.

## Errors

Mistakes are reported as everywhere else. The place is given by the number of the input, the one in its prompt, and the line in it:

```text
[7]> fn third(items: [Int]) -> Int {
...>     return items[2];
...> }
[8]> third([1, 2])
[ERROR] R0002: index out of bounds: 2 (length 2)
  --> [7]:2:17 in function 'third'
     |
 2 |     return items[2];
     |                 ^
     |
     = Suggestion: Compare the index with arrays.len(...) before using it, or use arrays.at, which gives null instead of failing
  called from [8]:1:1 in function 'main'
```

An input that does not compile changes nothing. One that fails while it runs has done what it did before failing: assignments it made stay made. The variables it declared are not kept.

A program that calls `std.os.exit` ends the REPL with that status.

## Imports, projects and limits

Import paths start at the folder the REPL is started in. Inside a [project](project.md) they start at the project's root, and its packages are there, so that what a program of the project can import can be imported here.

`--heap-mb=N` and `--max-depth=N`, and the limits of the project file, apply to a session as they do to a program. The memory limit is for all that the session keeps alive at once.

## Sessions from a file

When its input is not a terminal, `lumen repl` reads the same inputs without a prompt, one per line; an input goes on to the next line while a bracket is open.

```bash
lumen repl < session.txt
echo 'import std.math; math.sqrt(2.0)' | lumen repl
```

Values and what the programs print go to the output, diagnostics to the error output. The status is 0 if every input went well and 1 if one did not; the session goes on after a mistake, as it does at a terminal. Programs that read lines (`io.readLine`) read them from the same input, after the line that calls them.

## How it works

Lumen compiles whole programs, and the REPL does not change that. Each input is compiled as a program of its own: everything declared so far, and a `main` that runs what was typed. The programs run one after the other on the same virtual machine, so that what one leaves in memory is there for the next.

```rust
// [3]> count += bump();
fn bump() -> Int { return 1; }                  // declared in [2]
fn main() {
    let count: Int = __repl_get_count();        // the variable of [1]: its value...
    __repl_link("count", () -> count);          // ...and the variable itself
    {
        count += bump();
    }
}
```

*   **Variables** are what closures already keep alive: a variable captured by a closure outlives the function that declared it. The virtual machine holds on to that variable between two programs, and makes it the variable of the next program's `main` again for as long as that program runs. That is why closures made in different inputs share it. See [`internal/vm/repl.go`](../internal/vm/repl.go).
*   **Types of variables** have to be written out in each program, including those no one wrote: `let r = io.readFile(path);` is a `Result<String, Error>` of two modules the session may never have imported. They are written through imports the REPL adds for itself.
*   **Declarations** are compiled again with every input, which is what makes replacing one mean what it should. Functions that an input does not reach are left out of its program, so the cost is that of checking them.
*   **Functions of earlier programs** stay callable: a function value carries the program it was compiled in.
*   **Positions** stay those of the session: an input keeps its place in the programs that follow, blanked out once it is no longer part of them, so that `[7]:2` still means the second line of the seventh input a hundred inputs later.

## Not there

*   A statement and a declaration in one input.
*   The value of a `match` or an `if` typed on its own: they are statements there. Give it a name, `let word = match n { ... };`, then type the name.
*   Completion after an expression that is not a name (`makePoint().`); the [language server](editor.md) has it.
*   Colours for what is typed.

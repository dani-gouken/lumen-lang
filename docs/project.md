# The Project File: `lumen.json`

A single `.lum` file needs nothing but itself. A program that grows past that can describe itself in a file called `lumen.json`:

```json
{
    "name": "greeter",
    "entry": "src/main.lum",
    "root": "src",
    "packages": {
        "textkit": "packages/textkit"
    },
    "heap_mb": 512,
    "max_depth": 20000
}
```

A working example is in [`examples/project/`](../examples/project).

## What It Does

| Key | Meaning | Without it |
|-----|---------|------------|
| `name` | The program's name. Informative only | |
| `entry` | The file the program starts in | A file has to be named on the command line |
| `root` | The folder [module](modules.md) paths start at | The folder of `lumen.json` |
| `packages` | Names for folders of modules that live outside the root | No packages |
| `heap_mb` | Memory the program may hold at once, in MB | 256 |
| `max_depth` | How deep calls may nest | 10000 |

Every key is optional; `{}` is a valid project file. Paths are relative to the folder of `lumen.json`, or absolute.

A key that is not in this table is an error, so a typo (`"entri"`) is reported instead of silently ignored.

## Where It Is Looked For

In the folder of the file being compiled, then in each folder above it. The nearest one is used. This means every file of a project is compiled with the project's settings, whichever one is run:

```
greeter/
  lumen.json
  src/
    main.lum
    app/
      guests.lum
      demo.lum          lumen run src/app/demo.lum   uses greeter/lumen.json too
```

Without a `lumen.json` anywhere above, nothing changes from how a single file is run: module paths start at the folder of the file given on the command line.

## Running a Project

With an `entry`, the commands that take a source file can be given the project's folder, or nothing:

```bash
lumen run                    # the project of the current folder (or of a folder above)
lumen run path/to/greeter    # the project in that folder
lumen run .                  # the same as nothing
lumen build greeter.lc       # compile the current project to bytecode
lumen build path/to/greeter greeter.lc
lumen ir
lumen lumdbg
```

Naming a file still works and is unaffected by `entry`: `lumen run src/app/demo.lum`.

What follows the folder or the file is handed to the program, which reads it with [`os.args()`](stdlib.md#stdos): `lumen run . add "Buy milk"`. With arguments the folder has to be named, a dot at least: `lumen run add` would look for a file called `add`.

## `root`: Where Module Paths Start

Module paths start at one folder, in every file of the program (see [modules.md](modules.md)). With a project file that folder is `root`:

```json
{ "entry": "src/main.lum", "root": "src" }
```

```rust
import app.guests;       // src/app/guests.lum, from any file of the project
```

Without `root` it is the folder of `lumen.json` itself, and the same import would be written `import src.app.guests;`.

This also lets the entry file sit below the root (`"entry": "src/cmd/tool/main.lum"`), which is not possible without a project file, where the root is always the entry file's own folder.

## `packages`: Modules From Somewhere Else

A package is a folder of modules with a name. The name is the first part of the path used to import from it:

```json
{ "packages": { "textkit": "packages/textkit", "shared": "../shared/lumen" } }
```

| Import | File |
|--------|------|
| `import textkit.format.pad;` | `packages/textkit/format/pad.lum` |
| `import textkit;` | `packages/textkit.lum`, the file next to the folder |
| `import shared.geometry;` | `../shared/lumen/geometry.lum` |

Everything else about modules applies unchanged: `pub`, aliases, importing names, `pub import`. Inside a package, modules name each other through the package, like any other file of the program:

```rust
// packages/textkit.lum
import textkit.format.pad;
```

So a package does not depend on where it is installed, only on the name it is given.

Names at runtime and in messages use the package's name, not its folder: `textkit.format.pad.left`.

A package name wins over a folder of the same name under the root: with a package `std`, `import std.list;` never looks at `<root>/std/list.lum`.

A package name must be usable in an import: letters, digits and `_`, not starting with a digit.

The [standard library](stdlib.md) is a package too, named `std`, whose folder is inside `lumen` itself. It does not have to be listed. Listing a package named `std` replaces it, whole, with the folder given: the program then gets none of the built-in modules.

### What packages are not

There is no installer, no registry, no versions, and a package cannot declare packages of its own: the program's `lumen.json` names every package the program uses, including the ones its packages import. It is a way to say where code is, not a way to get it there.

## Limits

`heap_mb` and `max_depth` set what `--heap-mb` and `--max-depth` set (see [guide.md](guide.md#commands)), for every run of the project's source. An option on the command line wins over the file.

They apply to `run` and `lumdbg`, which execute source. A compiled `.lc` file does not carry them: `lumen exec program.lc` uses the defaults or the command line.

## Errors

Mistakes in `lumen.json` stop the command before anything is compiled, with the file named:

```
Error: lumen.json: unknown key "main"; the keys are name, entry, root, packages, heap_mb, max_depth
Error: lumen.json: line 3, column 3: not valid JSON: invalid character 'e' looking for beginning of object key string
Error: lumen.json: line 2: "heap_mb" must be a whole number
Error: lumen.json: package name "my-lib" is not a name that can be imported: use letters, digits and '_', not starting with a digit
Error: lumen.json does not say where the program starts; add "entry": "main.lum"
Error: sub is a folder without a lumen.json; name a .lum file, or add a project file with an "entry"
```

A broken project file is an error even when a file is named on the command line: the file is part of the project, and compiling it with the settings ignored would be worse than stopping.

The format is plain JSON: no comments, no trailing commas.

## Implementation

`internal/project` reads and checks the file (`Find`, `Load`, `Parse`) and resolves its paths. The command line (`cmd/lumen`) turns it into `compiler.Options{Root, Packages}`, which reach the module loader as `modules.Options`; see [compilation/module_loading.md](compilation/module_loading.md). The compiler itself never reads `lumen.json`, so it can be driven with any layout by code that embeds it.

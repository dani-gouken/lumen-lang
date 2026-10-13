# Lumen in an Editor

`lumen lsp` is a language server: a program an editor starts and talks to, to show what the compiler knows while the program is being written. It speaks the [Language Server Protocol](https://microsoft.github.io/language-server-protocol/), so any editor that does can use it.

| In the editor | What it does |
|---------------|--------------|
| Mistakes underlined | Errors and warnings of the compiler, as you type, with what usually fixes them; in the file and in the modules it imports |
| Pointing at a name | How it is declared and the comment above its declaration: variables, functions, methods, fields, variants, types, modules |
| Go to definition | Jumps to where a name is declared: in the file, in another file of the program, or in the standard library |
| Outline | What the file declares: functions, structs with their fields, enums with their variants, traits, methods |
| Suggestions | After `name.`: the fields and methods of a value, the variants of an enum, the public names of a module. On an `import` line: the modules there are. Elsewhere: everything in reach |

## VS Code

The extension is in [`editors/vscode`](../editors/vscode): colours for `.lum` files, and the wire to `lumen lsp`. It has no dependencies.

```sh
cd editors/vscode
npx --yes @vscode/vsce package --allow-missing-repository --skip-license
code --install-extension lumen-0.1.0.vsix
```

It looks for the `lumen` program in this order: the `lumen.path` setting, a `lumen` at the root of the workspace or in a folder above it, then the `PATH`. After rebuilding `lumen`, run **Lumen: Restart the language server** from the command palette: the server is the program that was there when it started.

## Any other editor

Tell it to run `lumen lsp` for files ending in `.lum`, talking over standard input and output. For Neovim:

```lua
vim.filetype.add({ extension = { lum = "lumen" } })
vim.api.nvim_create_autocmd("FileType", {
  pattern = "lumen",
  callback = function()
    vim.lsp.start({ name = "lumen", cmd = { "lumen", "lsp" }, root_dir = vim.fn.getcwd() })
  end,
})
```

## How it is built

The server is a Lumen program, [`internal/lsp/server.lum`](../internal/lsp/server.lum), kept inside the `lumen` binary: some 350 lines that read messages, keep the text of the open files, and answer. It is written with three parts of the standard library:

*   [`std.compiler`](stdlib.md#stdcompiler) answers the questions: it is the front of the compiler, parsing, names and types, reachable from a program.
*   [`std.task`](tasks.md) waits before checking: a file is compiled 150 ms after the last keystroke, in a task that gives up if another keystroke came meanwhile.
*   [`std.json`](stdlib.md#stdjson) and `io.readBytes` read and write the protocol.

A file being edited is known to the compiler as it is in the editor, saved or not: a program that imports it is checked against that text.

The files of the standard library live inside `lumen`. Going to a definition there needs a file to open, so the server writes the one asked for into the user's cache folder (`lumen/std/`), marked as not to be changed.

A file without a `main` is a module: it is checked like a program, since that is the only way to check it, and the missing `main` is not held against it.

## What is not there

| Missing | Today |
|---------|-------|
| Renaming, finding every use of a name | Search |
| The parameters of a function while its arguments are typed | Point at its name |
| Formatting | By hand |
| Colours from the compiler (semantic tokens) | The grammar of the extension, which goes by the shape of words |
| Understanding a file with a mistake in it | A program is understood whole or not at all: while a line does not parse, hover and definition answer nothing; suggestions still work, from the file without that line |
| Sending only what changed | The whole text is sent with every change |
| Columns in texts with characters outside the usual planes (some emoji) | The protocol counts in UTF-16, Lumen in characters: positions after one on the same line are off by one |
| The names of struct fields suggested inside `Point { ... }` | Fields are suggested after a dot |

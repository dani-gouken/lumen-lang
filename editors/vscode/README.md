# Lumen for VS Code

Colours for `.lum` files, and the language server: mistakes underlined as you type, what a name is when you point at it, jumping to where it is declared, the outline of a file, suggestions after a dot, and formatting.

**Format Document** writes the file the way `lumen fmt` does. To have it done on save:

```json
"[lumen]": { "editor.formatOnSave": true }
```

**Lumen: Run the tests** and **Lumen: Run the tests of this file** run `lumen test` in a terminal.

The extension starts `lumen lsp`, the language server kept inside the `lumen` program. It looks for `lumen` in this order: the `lumen.path` setting, a `lumen` program at the root of the workspace or in a folder above it, then the `PATH`.

## Installing it

From this folder:

```sh
npx --yes @vscode/vsce package --allow-missing-repository --skip-license
code --install-extension lumen-0.1.0.vsix
```

or, without packaging, link the folder where VS Code looks for extensions and restart it:

```sh
ln -s "$(pwd)" ~/.vscode/extensions/lumen.lumen-0.1.0
```

## Files

| File | For |
|------|-----|
| `syntaxes/lumen.tmLanguage.json` | The colours: keywords, types, texts with `${...}`, comments |
| `language-configuration.json` | Brackets, comments, indentation |
| `extension.js` | Starts the server and passes on what it says; runs the tests in a terminal. No dependencies |

`Lumen: Restart the language server` in the command palette starts it again, after rebuilding `lumen` for one.

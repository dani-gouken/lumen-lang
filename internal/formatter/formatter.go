// Package formatter holds `lumen fmt`, which is a Lumen program: fmt.lum,
// compiled into the lumen binary. It finds the files and says what became
// of them; how a program is written is decided by std.formatter, a module
// of the standard library, which the language server runs as well.
package formatter

import _ "embed"

// File is the name the program's source goes by in messages.
const File = "<fmt>/fmt.lum"

// Source is the text of the program.
//
//go:embed fmt.lum
var Source string

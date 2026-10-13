// Package lsp holds the language server of Lumen, which is a Lumen
// program: server.lum, compiled into the lumen binary and run by
// `lumen lsp`.
package lsp

import _ "embed"

// File is the name the server's source goes by in messages.
const File = "<lsp>/server.lum"

// Source is the text of the server.
//
//go:embed server.lum
var Source string

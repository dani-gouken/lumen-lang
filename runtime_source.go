// Package lumen carries the Go source of Lumen's runtime, which lumen
// native builds a program's executable from: the virtual machine, its
// native functions, and the compiler that std.compiler is.
//
// The list is every package a native program can import, and what they
// import: TestTheRuntimeSourceIsComplete in internal/native fails when a
// package is added to them and not here.
package lumen

import "embed"

//go:embed go.mod go.sum
//go:embed internal/memguard internal/token internal/errors internal/vm internal/runner
//go:embed internal/ast internal/stdlib internal/resolve internal/parser internal/modules
//go:embed internal/types internal/analysis internal/ir internal/codegen internal/desugar
//go:embed internal/irgen internal/monomorph internal/compiler internal/project internal/inspect
var RuntimeSource embed.FS

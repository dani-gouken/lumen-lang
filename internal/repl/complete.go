package repl

import (
	"sort"
	"strings"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/parser"
	"github.com/dani-gouken/lumen/internal/resolve"
	"github.com/dani-gouken/lumen/internal/token"
)

// Incomplete reports whether a text has to go on before it can be an
// input: a bracket is open, or a text over several lines, or a comment.
func Incomplete(text string) bool {
	depth := 0
	for _, tok := range parser.NewLexer(text, "").Tokenize() {
		switch tok.Type {
		case token.LPAREN, token.LBRACE, token.LBRACKET:
			depth++
		case token.RPAREN, token.RBRACE, token.RBRACKET:
			depth--
		case token.ILLEGAL:
			if strings.Contains(tok.Literal, `"""`) || strings.Contains(tok.Literal, "block comment") {
				return true
			}
		}
	}
	return depth > 0
}

// words that start statements and declarations, offered where a name is.
var keywords = []string{
	"break", "case", "const", "continue", "default", "defer", "else", "enum", "false", "fn", "for",
	"if", "impl", "import", "in", "let", "match", "null", "pub", "return", "struct", "trait", "true", "while",
}

// Complete gives what the text before the cursor could go on as: the
// candidates, whole, and how many of the characters before the cursor
// they stand for.
//
//	strings.up      the names of a module
//	import std.     the modules of the standard library
//	point.          the fields and methods of a variable
//	:ty             the commands
//	pri             everything else: variables, declarations, keywords
func (s *Session) Complete(before string) ([]string, int) {
	if strings.HasPrefix(before, ":") && !strings.ContainsAny(before, " \t") {
		return matching(commandNames(), before), len([]rune(before))
	}

	// The name being typed, and the name before the dot before it
	runes := []rune(before)
	start := len(runes)
	for start > 0 && isWordRune(runes[start-1]) {
		start--
	}
	prefix := string(runes[start:])
	holderEnd := start
	var path []string
	for holderEnd > 0 && runes[holderEnd-1] == '.' {
		from := holderEnd - 1
		for from > 0 && isWordRune(runes[from-1]) {
			from--
		}
		if from == holderEnd-1 {
			break
		}
		path = append([]string{string(runes[from : holderEnd-1])}, path...)
		holderEnd = from
	}

	line := before[strings.LastIndexByte(before, '\n')+1:]
	if fields := strings.Fields(line); len(fields) > 0 && (fields[0] == "import" || (fields[0] == "pub" && len(fields) > 1 && fields[1] == "import")) {
		return matching(s.modules(strings.Join(path, ".")), prefix), len(runes) - start
	}

	var names []string
	switch {
	case len(path) == 0:
		names = append(names, keywords...)
		for _, fn := range resolve.Builtins() {
			names = append(names, fn.Name)
		}
		for _, v := range s.vars {
			names = append(names, v.name)
		}
		prog := s.analyzed()
		for _, e := range s.entries {
			for _, it := range e.items {
				if it.kind != "import" && it.kind != "impl" {
					names = append(names, it.name)
				}
			}
		}
		for alias := range s.imports(prog) {
			names = append(names, alias)
		}
	case len(path) == 1:
		names = s.members(path[0])
	}
	return matching(names, prefix), len(runes) - start
}

// matching keeps the names that start with a prefix, sorted, each once.
func matching(names []string, prefix string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		if strings.HasPrefix(name, prefix) && name != prefix && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// analyzed is the program of the session as the compiler reads it: the
// declarations of the session and of the modules it imports.
func (s *Session) analyzed() *ast.Program {
	src, _ := s.program("", "")
	return compiler.Analyze(src, s.file, s.compilerOptions(), errors.NewHandler()).Program
}

// imports maps the names modules are imported under to the modules.
func (s *Session) imports(prog *ast.Program) map[string]string {
	out := map[string]string{}
	for _, e := range s.entries {
		for _, it := range e.items {
			if it.kind != "import" {
				continue
			}
			parsed := parser.Parse(e.text[it.from:it.to], "", errors.NewHandler())
			for _, imp := range parsed.Imports {
				if len(imp.Names) > 0 {
					continue
				}
				alias := imp.Alias
				if alias == "" {
					_, alias = splitName(imp.Path)
				}
				out[alias] = imp.Path
			}
		}
	}
	return out
}

// members lists what can follow `name.`: what a module exports, the
// fields and methods of a variable, the variants of an enum.
func (s *Session) members(name string) []string {
	prog := s.analyzed()
	if prog == nil {
		return nil
	}
	var names []string

	if module, ok := s.imports(prog)[name]; ok {
		for _, decl := range prog.Decls {
			var full string
			pub := false
			switch d := decl.(type) {
			case *ast.FnDecl:
				full, pub = d.Name, d.Pub
			case *ast.StructDecl:
				full, pub = d.Name, d.Pub
			case *ast.EnumDecl:
				full, pub = d.Name, d.Pub
			case *ast.TraitDecl:
				full, pub = d.Name, d.Pub
			case *ast.ConstDecl:
				full, pub = d.Name, d.Pub
			}
			if in, short := splitName(full); pub && in == module {
				names = append(names, short)
			}
		}
		for _, fn := range prog.Externs {
			if in, short := splitName(fn.Name); fn.Pub && in == module {
				names = append(names, short)
			}
		}
		return names
	}

	// A variable: the members of its type. Anything else: a type itself.
	owner := name
	for _, v := range s.vars {
		if v.name == name {
			t := v.typ
			if t.Kind == ast.TypeOption {
				t = t.ElemType
			}
			owner = t.Name
		}
	}
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			if d.Name == owner && owner != name {
				for _, field := range d.Fields {
					names = append(names, field.Name)
				}
			}
		case *ast.EnumDecl:
			if d.Name == owner && owner == name {
				for _, variant := range d.Variants {
					names = append(names, variant.Name)
				}
			}
		case *ast.ImplDecl:
			if d.StructName == owner && owner != name {
				for _, method := range d.Methods {
					names = append(names, method.Name)
				}
			}
		}
	}
	return names
}

// modules lists what can follow `import under.`: the modules of the
// standard library, which are the ones every session has.
func (s *Session) modules(under string) []string {
	switch under {
	case "":
		return []string{"std"}
	case "std":
		return standardModules()
	}
	return nil
}

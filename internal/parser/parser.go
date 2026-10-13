// Package parser turns Lumen source text into an AST.
package parser

import (
	"fmt"
	"strings"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/token"
)

// Error codes for parsing
const (
	ErrUnexpectedToken  = "E0001" // Unexpected token or malformed construct
	ErrLexical          = "E0002" // Invalid character, unterminated string or comment
	ErrMissingSemicolon = "E0003" // Statement not terminated by ';'
	ErrInvalidAssign    = "E0004" // Left side of '=' is not assignable
	ErrDuplicateField   = "E0005" // Field given twice in a struct literal
)

// bailout is used to unwind the parser after a syntax error.
type bailout struct{}

// Parser is a recursive descent parser for Lumen.
type Parser struct {
	toks        []token.Token
	pos         int
	errs        *errors.Handler
	sourceLines map[int]string

	// Struct literals are ambiguous with blocks in `if x {`, so they are
	// disabled while parsing the header of if/while/for/match.
	noStructLit bool

	// While speculating, syntax errors unwind without being reported.
	speculating bool

	// How many names were made up so far, for those to differ
	made int

	// The statements that follow the one just parsed, when it is short for
	// several (`let (a, b) = pair;`)
	pendingStmts []ast.Stmt
}

// makeName returns a name no program can write.
func (p *Parser) makeName(prefix string) string {
	p.made++
	return fmt.Sprintf("__%s%d", prefix, p.made)
}

// SourceLines splits source text into a 1-based line map for diagnostics.
func SourceLines(src string) map[int]string {
	lines := make(map[int]string)
	for i, line := range strings.Split(src, "\n") {
		lines[i+1] = strings.TrimRight(line, "\r")
	}
	return lines
}

// Parse parses a whole source file. Syntax errors are reported to errs; the
// returned program contains every declaration that parsed cleanly.
func Parse(src, file string, errs *errors.Handler) *ast.Program {
	p := &Parser{
		toks:        NewLexer(src, file).Tokenize(),
		errs:        errs,
		sourceLines: SourceLines(src),
	}
	return p.parseProgram()
}

// --- Token helpers ---

func (p *Parser) cur() token.Token {
	return p.toks[p.pos]
}

func (p *Parser) peekAt(offset int) token.Token {
	if p.pos+offset >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.pos+offset]
}

func (p *Parser) at(typ token.TokenType) bool {
	return p.cur().Type == typ
}

func (p *Parser) next() token.Token {
	tok := p.cur()
	if tok.Type != token.EOF {
		p.pos++
	}
	return tok
}

func (p *Parser) accept(typ token.TokenType) bool {
	if p.at(typ) {
		p.next()
		return true
	}
	return false
}

func (p *Parser) expect(typ token.TokenType, what string) token.Token {
	if !p.at(typ) {
		p.fail("expected %s, found %s", what, describe(p.cur()))
	}
	return p.next()
}

// fail reports a syntax error at the current token and unwinds to the
// nearest recovery point.
func (p *Parser) fail(format string, args ...interface{}) {
	p.failWith(ErrUnexpectedToken, "", format, args...)
}

func (p *Parser) failWith(code, suggestion, format string, args ...interface{}) {
	if !p.speculating {
		tok := p.cur()
		msg := fmt.Sprintf(format, args...)
		if tok.Type == token.ILLEGAL {
			// The lexer already described the problem in the token literal
			code, msg, suggestion = ErrLexical, tok.Literal, lexicalSuggestion(tok.Literal)
		}
		if suggestion != "" {
			p.errs.ReportErrorWithSuggestion(code, msg, suggestion, tok, p.sourceLines)
		} else {
			p.errs.ReportErrorFromToken(code, msg, tok, p.sourceLines)
		}
	}
	panic(bailout{})
}

func lexicalSuggestion(msg string) string {
	switch {
	case strings.HasPrefix(msg, "unterminated string"):
		return "Add a closing '\"' before the end of the line"
	case strings.HasPrefix(msg, "unterminated block comment"):
		return "Add a closing '*/'"
	case strings.HasPrefix(msg, "invalid escape"):
		return "Valid escapes are \\n, \\t, \\r, \\0, \\\\, \\\" and \\$"
	case strings.HasPrefix(msg, "unterminated '${'"):
		return "Close the expression with '}'; to write a dollar sign followed by a brace, escape it: \\${"
	case strings.HasPrefix(msg, "nothing between"):
		return "Put an expression between the braces: \"${name}\"; for the characters themselves, write \\${}"
	}
	return ""
}

// expectSemicolon consumes the ';' ending a statement. A missing one is
// reported but does not stop parsing: the parser carries on as if it were
// there, so the rest of the block is still checked.
func (p *Parser) expectSemicolon() {
	if p.accept(token.SEMICOLON) {
		return
	}
	if p.at(token.ILLEGAL) {
		p.fail("")
	}
	if p.speculating {
		panic(bailout{})
	}

	// Point just past the previous token, where the ';' belongs
	prev := p.toks[p.pos-1]
	at := token.Token{Type: token.SEMICOLON, Pos: prev.EndPos, EndPos: prev.EndPos}
	at.EndPos.Col++
	p.errs.ReportErrorWithSuggestion(
		ErrMissingSemicolon,
		fmt.Sprintf("expected ';' after %s, found %s", describe(prev), describe(p.cur())),
		"Add ';' to end the statement",
		at,
		p.sourceLines,
	)
}

func describe(tok token.Token) string {
	switch tok.Type {
	case token.EOF:
		return "end of file"
	case token.ILLEGAL:
		return tok.Literal
	case token.STRING:
		return fmt.Sprintf("string %q", tok.Literal)
	default:
		return "'" + tok.Literal + "'"
	}
}

func base(tok token.Token) ast.BaseNode {
	return ast.BaseNode{Pos: tok.Pos, Tok: tok}
}

// --- Program and declarations ---

func (p *Parser) parseProgram() *ast.Program {
	prog := &ast.Program{BaseNode: base(p.cur())}

	for !p.at(token.EOF) {
		p.parseTopLevel(prog)
	}
	classifyPatterns(prog)
	return prog
}

// parseTopLevel parses one import or declaration, recovering to the next
// top-level keyword on error so that several errors can be reported.
func (p *Parser) parseTopLevel(prog *ast.Program) {
	start := p.pos
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
			if p.pos == start {
				p.next()
			}
			p.synchronize()
		}
	}()

	pubTok, pub := p.cur(), p.accept(token.PUB)
	if p.at(token.IMPORT) {
		imp := p.parseImport()
		imp.Pub = pub
		prog.Imports = append(prog.Imports, imp)
		return
	}
	decl := p.parseDecl()
	if pub {
		p.markPub(decl, pubTok)
	}
	prog.Decls = append(prog.Decls, decl)
}

// markPub records that a declaration is exported from its module.
func (p *Parser) markPub(decl ast.Decl, pubTok token.Token) {
	switch d := decl.(type) {
	case *ast.FnDecl:
		d.Pub = true
	case *ast.StructDecl:
		d.Pub = true
	case *ast.EnumDecl:
		d.Pub = true
	case *ast.TraitDecl:
		d.Pub = true
	case *ast.ConstDecl:
		d.Pub = true
	case *ast.ImplDecl:
		p.errs.ReportErrorWithSuggestion(ErrUnexpectedToken,
			"'pub' cannot be applied to an impl block",
			"Mark the methods instead: pub fn name() { ... }", pubTok, p.sourceLines)
	}
}

func (p *Parser) synchronize() {
	for {
		switch p.cur().Type {
		case token.EOF, token.STRUCT, token.ENUM, token.TRAIT, token.IMPL, token.IMPORT, token.PUB, token.EXTERN:
			return
		case token.CONST:
			// A constant of a function body is indented; one that starts a
			// line is a declaration
			if p.cur().Pos.Col == 1 {
				return
			}
		case token.FN:
			// `fn name` starts a declaration; `fn(` is a function type or
			// a lambda inside the broken declaration
			if p.peekAt(1).Type == token.IDENT {
				return
			}
		}
		p.next()
	}
}

func (p *Parser) parseImport() *ast.ImportDecl {
	tok := p.expect(token.IMPORT, "'import'")
	decl := &ast.ImportDecl{BaseNode: base(tok)}
	if p.at(token.STRING) {
		p.failWith(ErrUnexpectedToken, "Name the module by its path from the program's folder: import physics.body;",
			"expected module path, found %s", describe(p.cur()))
	}

	// The token of the import is its path, so diagnostics underline it
	first := p.expect(token.IDENT, "module path")
	decl.BaseNode = base(first)
	segments := []string{first.Literal}
	for p.accept(token.DOT) {
		if p.accept(token.LBRACE) {
			for !p.at(token.RBRACE) {
				nameTok := p.expect(token.IDENT, "name to import")
				decl.Names = append(decl.Names, &ast.ImportName{BaseNode: base(nameTok), Name: nameTok.Literal})
				if !p.accept(token.COMMA) {
					break
				}
			}
			closing := p.expect(token.RBRACE, "'}'")
			if len(decl.Names) == 0 {
				p.pos--
				p.failWith(ErrUnexpectedToken, "List the names to import, or import the module itself: import "+strings.Join(segments, ".")+";",
					"expected a name to import, found %s", describe(closing))
			}
			break
		}
		last := p.expect(token.IDENT, "module name")
		segments = append(segments, last.Literal)
		decl.Tok.EndPos = last.EndPos
	}
	decl.Path = strings.Join(segments, ".")

	if p.at(token.AS) {
		if len(decl.Names) > 0 {
			p.failWith(ErrUnexpectedToken, "An alias names the module; with a list of names, use the names directly",
				"'as' cannot follow a list of imported names")
		}
		p.next()
		decl.Alias = p.expect(token.IDENT, "import alias").Literal
	}
	p.expectSemicolon()
	return decl
}

func (p *Parser) parseDecl() ast.Decl {
	switch p.cur().Type {
	case token.CONST:
		return p.parseConstDecl()
	case token.EXTERN:
		return p.parseExternDecl()
	case token.FN:
		return p.parseFnDecl(false)
	case token.STRUCT:
		return p.parseStructDecl()
	case token.ENUM:
		return p.parseEnumDecl()
	case token.TRAIT:
		return p.parseTraitDecl()
	case token.IMPL:
		return p.parseImplDecl()
	}
	if p.at(token.LET) {
		p.failWith(ErrUnexpectedToken, "Outside a function only constants can be declared: const name = value;",
			"expected declaration, found 'let'")
	}
	p.fail("expected declaration (fn, struct, enum, trait, impl, const, import), found %s", describe(p.cur()))
	return nil
}

// parseConstDecl parses a constant at the top level: `const NAME = value;`
// with an optional type after the name.
func (p *Parser) parseConstDecl() *ast.ConstDecl {
	p.expect(token.CONST, "'const'")
	nameTok := p.expect(token.IDENT, "constant name")
	decl := &ast.ConstDecl{BaseNode: base(nameTok), Name: nameTok.Literal}
	if p.accept(token.COLON) {
		decl.Type = p.parseType()
	}
	if !p.at(token.ASSIGN) {
		p.failWith(ErrUnexpectedToken, "A constant needs a value: const "+decl.Name+" = 1;",
			"expected '=', found %s", describe(p.cur()))
	}
	p.next()
	decl.Value = p.parseExpr()
	p.expectSemicolon()
	return decl
}

// parseExternDecl parses what a program declares without writing it:
//
//	extern fn name(params) -> Type;          a function the runtime provides
//	extern "library" fn name(params) -> Type; a function of a C library
//	extern struct Name { field: Type; }       a struct of a C library
//
// In the last two the types are those of C (see ast.ForeignTypes).
func (p *Parser) parseExternDecl() ast.Decl {
	p.expect(token.EXTERN, "'extern'")
	if p.at(token.STRUCT) {
		decl := p.parseStructDecl()
		decl.Extern = true
		if len(decl.TypeParams) > 0 {
			p.errs.ReportErrorWithSuggestion(ErrUnexpectedToken,
				"a struct of a C library cannot have type parameters",
				"Give each field the C type it has: Int32, Float32, Pointer, ...", decl.Token(), p.sourceLines)
		}
		for _, field := range decl.Fields {
			field.Type.Foreign()
		}
		return decl
	}

	library, foreign := "", false
	if p.at(token.STRING) {
		library, foreign = p.cur().Literal, true
		if library == "" {
			p.failWith(ErrUnexpectedToken, "Name the library the function is in: extern \"raylib\" fn ...",
				"the name of the library is empty")
		}
		p.next()
	}
	if !p.at(token.FN) {
		p.failWith(ErrUnexpectedToken, "extern fn name(...);  extern \"library\" fn name(...);  extern struct Name { ... }",
			"expected 'fn', 'struct' or the name of a library after 'extern', found %s", describe(p.cur()))
	}
	tok := p.expect(token.FN, "'fn' after 'extern'")
	decl := &ast.FnDecl{BaseNode: base(tok), Extern: true, Library: library}

	nameTok := p.expect(token.IDENT, "function name")
	decl.Name = nameTok.Literal
	decl.BaseNode = base(nameTok)
	if foreign {
		decl.Symbol = decl.Name
	}
	if p.at(token.LT) {
		p.failWith(ErrUnexpectedToken, "Give the parameters concrete types, or 'Any'",
			"an extern function cannot have type parameters")
	}
	decl.Params = p.parseParams()
	if p.accept(token.ARROW) {
		decl.ReturnType = p.parseType()
	}
	if foreign {
		for _, param := range decl.Params {
			param.Type.Foreign()
		}
		decl.ReturnType.Foreign()
	}
	if p.at(token.LBRACE) {
		p.failWith(ErrUnexpectedToken, "Remove 'extern' to write the function here, or end the declaration with ';'",
			"an extern function has no body: the runtime provides it")
	}
	p.expectSemicolon()
	return decl
}

func (p *Parser) parseTypeParams() []string {
	params, _ := p.parseBoundedTypeParams(false)
	return params
}

// parseBoundedTypeParams parses `<T, U: Ordered + Show>`: type parameters,
// each with the traits it is required to implement, if any. Where bounds
// make no sense (the parameters of a trait, the ones repeated after the
// struct name of an impl) they are refused.
func (p *Parser) parseBoundedTypeParams(bounded bool) ([]string, map[string][]*ast.Type) {
	var params []string
	var bounds map[string][]*ast.Type
	if !p.accept(token.LT) {
		return nil, nil
	}
	for {
		name := p.expect(token.IDENT, "type parameter name").Literal
		params = append(params, name)
		if p.at(token.COLON) {
			if !bounded {
				p.failWith(ErrUnexpectedToken, "Say what a type parameter has to implement where it is introduced: after 'fn name', 'struct Name', 'enum Name' or 'impl'",
					"a type parameter cannot be given a requirement here")
			}
			p.next()
			if bounds == nil {
				bounds = make(map[string][]*ast.Type)
			}
			for {
				bounds[name] = append(bounds[name], p.parseTraitName())
				if !p.accept(token.PLUS) {
					break
				}
			}
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.GT, "'>'")
	return params, bounds
}

// parseTraitName parses the name of a trait, with its type arguments if it
// has any: `Ordered`, `shapes.Drawable`, `Source<Int>`.
func (p *Parser) parseTraitName() *ast.Type {
	tok := p.cur()
	name := p.expectTypeName("trait name")
	tok.Literal = name
	t := &ast.Type{BaseNode: base(tok), Kind: ast.TypeBasic, Name: name}
	if p.accept(token.LT) {
		t.Generics = p.parseTypeList(token.GT)
		p.expect(token.GT, "'>'")
	}
	return t
}

// parseFnDecl parses a function declaration. Trait methods have no body.
func (p *Parser) parseFnDecl(traitMethod bool) *ast.FnDecl {
	tok := p.expect(token.FN, "'fn'")
	decl := &ast.FnDecl{BaseNode: base(tok), IsTraitMethod: traitMethod}
	decl.Name = p.expect(token.IDENT, "function name").Literal
	decl.TypeParams, decl.Bounds = p.parseBoundedTypeParams(true)
	decl.Params = p.parseParams()
	if p.accept(token.ARROW) {
		decl.ReturnType = p.parseType()
	}

	if traitMethod && p.accept(token.SEMICOLON) {
		return decl
	}
	decl.Body = p.parseBlock()
	return decl
}

func (p *Parser) parseParams() []*ast.Param {
	var params []*ast.Param
	p.expect(token.LPAREN, "'('")
	for !p.at(token.RPAREN) {
		if t := p.cur().Type; t == token.LBRACKET || t == token.FN || t == token.ELLIPSIS || isBuiltinTypeToken(t) {
			p.failWith(ErrUnexpectedToken, "Parameters are written as 'name: Type'",
				"expected parameter name, found %s", describe(p.cur()))
		}
		tok := p.expect(token.IDENT, "parameter name")
		param := &ast.Param{BaseNode: base(tok), Name: tok.Literal}
		p.expect(token.COLON, "':'")
		param.Type = p.parseType()
		params = append(params, param)
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN, "')'")
	return params
}

func (p *Parser) parseStructDecl() *ast.StructDecl {
	tok := p.expect(token.STRUCT, "'struct'")
	decl := &ast.StructDecl{BaseNode: base(tok)}
	decl.Name = p.expect(token.IDENT, "struct name").Literal
	decl.TypeParams, decl.Bounds = p.parseBoundedTypeParams(true)

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		nameTok := p.expect(token.IDENT, "field name")
		field := &ast.Field{BaseNode: base(nameTok), Name: nameTok.Literal}
		p.expect(token.COLON, "':'")
		field.Type = p.parseType()
		if p.accept(token.ASSIGN) {
			// port: Int = 8080; what the field holds when a literal does
			// not say
			field.Default = p.parseExpr()
		}
		decl.Fields = append(decl.Fields, field)
		if !p.accept(token.SEMICOLON) && !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RBRACE, "'}'")
	return decl
}

func (p *Parser) parseEnumDecl() *ast.EnumDecl {
	tok := p.expect(token.ENUM, "'enum'")
	decl := &ast.EnumDecl{BaseNode: base(tok)}
	decl.Name = p.expect(token.IDENT, "enum name").Literal
	decl.TypeParams, decl.Bounds = p.parseBoundedTypeParams(true)

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		nameTok := p.expectMemberName("variant name")
		variant := &ast.EnumVariant{BaseNode: base(nameTok), Name: nameTok.Literal}
		if p.accept(token.LPAREN) {
			for !p.at(token.RPAREN) {
				variant.Values = append(variant.Values, p.parseType())
				if !p.accept(token.COMMA) {
					break
				}
			}
			p.expect(token.RPAREN, "')'")
		}
		decl.Variants = append(decl.Variants, variant)
		if !p.accept(token.COMMA) && !p.accept(token.SEMICOLON) {
			break
		}
	}
	p.expect(token.RBRACE, "'}'")
	return decl
}

func (p *Parser) parseTraitDecl() *ast.TraitDecl {
	tok := p.expect(token.TRAIT, "'trait'")
	decl := &ast.TraitDecl{BaseNode: base(tok)}
	decl.Name = p.expect(token.IDENT, "trait name").Literal
	decl.TypeParams = p.parseTypeParams()

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		decl.Methods = append(decl.Methods, p.parseFnDecl(true))
	}
	p.expect(token.RBRACE, "'}'")
	return decl
}

// parseImplDecl parses `impl Trait for Struct { ... }` or `impl Struct { ... }`.
// For a generic struct the type parameters are introduced after `impl` and
// repeated on the struct: `impl<T> Box<T> { ... }`.
func (p *Parser) parseImplDecl() *ast.ImplDecl {
	tok := p.expect(token.IMPL, "'impl'")
	decl := &ast.ImplDecl{BaseNode: base(tok)}
	decl.TypeParams, decl.Bounds = p.parseBoundedTypeParams(true)

	first := p.expectTypeName("trait or struct name")
	// impl Source<Int> for Counter: the type arguments of a trait are
	// types, where those of the struct can only repeat the parameters of
	// the impl. Which of the two follows is known once they are read.
	if p.at(token.LT) {
		start := p.pos
		if args := p.tryTraitArgs(); args != nil {
			decl.TraitArgs = args
		} else {
			p.pos = start
		}
	}
	if p.accept(token.FOR) {
		decl.TraitName = first
		decl.StructName = p.expectTypeName("struct name")
	} else {
		decl.StructName = first
	}
	p.expectImplTypeArgs(decl)

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		pub := p.accept(token.PUB)
		method := p.parseFnDecl(false)
		method.Pub = pub
		decl.Methods = append(decl.Methods, method)
	}
	p.expect(token.RBRACE, "'}'")
	return decl
}

// tryTraitArgs parses `<Int, [T]>` when `for` follows it, which makes it
// the type arguments of the trait of an impl. Otherwise it returns nil.
func (p *Parser) tryTraitArgs() (args []*ast.Type) {
	wasSpeculating := p.speculating
	p.speculating = true
	defer func() {
		p.speculating = wasSpeculating
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
			args = nil
		}
	}()

	p.expect(token.LT, "'<'")
	list := p.parseTypeList(token.GT)
	p.expect(token.GT, "'>'")
	if !p.at(token.FOR) {
		return nil
	}
	return list
}

// expectImplTypeArgs parses the `<T, U>` after the struct name of an impl
// and checks that it repeats the impl's type parameters in order, since
// that is how a method's T is tied to the struct's. A mismatch is reported
// without abandoning the impl, so its methods are still parsed.
func (p *Parser) expectImplTypeArgs(decl *ast.ImplDecl) {
	params := strings.Join(decl.TypeParams, ", ")
	correct := fmt.Sprintf("Write: impl<%s> %s<%s>", params, decl.StructName, params)

	if !p.at(token.LT) {
		if len(decl.TypeParams) > 0 {
			p.errs.ReportErrorWithSuggestion(ErrUnexpectedToken,
				fmt.Sprintf("expected the type parameters of '%s' after its name", decl.StructName),
				correct, p.cur(), p.sourceLines)
		}
		return
	}

	start := p.cur()
	if args := p.parseTypeParams(); strings.Join(args, ", ") != params {
		if len(decl.TypeParams) == 0 {
			correct = fmt.Sprintf("Write: impl<%s> %s<%s>", strings.Join(args, ", "), decl.StructName, strings.Join(args, ", "))
		}
		p.errs.ReportErrorWithSuggestion(ErrUnexpectedToken,
			fmt.Sprintf("type arguments of '%s' must repeat the parameters declared after 'impl'", decl.StructName),
			correct, start, p.sourceLines)
	}
}

// expectMemberName reads a name that follows a dot, or names a variant of
// an enum. The names of the builtin types are allowed there, where they
// cannot be taken for a type: a variant may be called Bool or String, as
// in `Json.Bool(true)`.
func (p *Parser) expectMemberName(what string) token.Token {
	if isBuiltinTypeToken(p.cur().Type) {
		return p.next()
	}
	return p.expect(token.IDENT, what)
}

func (p *Parser) expectTypeName(what string) string {
	if isBuiltinTypeToken(p.cur().Type) {
		return p.next().Literal
	}
	name := p.expect(token.IDENT, what).Literal
	// A type of another module: module.Name
	if p.at(token.DOT) && p.peekAt(1).Type == token.IDENT {
		p.next()
		name += "." + p.next().Literal
	}
	return name
}

// --- Types ---

func isBuiltinTypeToken(typ token.TokenType) bool {
	switch typ {
	case token.INT_TYPE, token.FLOAT_TYPE, token.BOOL_TYPE, token.STRING_TYPE,
		token.ANY_TYPE, token.VOID_TYPE, token.SELF_TYPE:
		return true
	}
	return false
}

// parseType parses a type, including unions: T1 | T2.
func (p *Parser) parseType() *ast.Type {
	first := p.parsePostfixType()
	if !p.at(token.PIPE) {
		return first
	}

	union := &ast.Type{BaseNode: first.BaseNode, Kind: ast.TypeUnion, UnionTypes: []*ast.Type{first}}
	for p.accept(token.PIPE) {
		union.UnionTypes = append(union.UnionTypes, p.parsePostfixType())
	}
	return union
}

func (p *Parser) parsePostfixType() *ast.Type {
	t := p.parsePrimaryType()
	for p.accept(token.QUESTION) {
		t = &ast.Type{BaseNode: t.BaseNode, Kind: ast.TypeOption, ElemType: t}
	}
	return t
}

func (p *Parser) parsePrimaryType() *ast.Type {
	tok := p.cur()

	switch {
	case isBuiltinTypeToken(tok.Type):
		p.next()
		return &ast.Type{BaseNode: base(tok), Kind: ast.TypeBasic, Name: tok.Literal}

	case tok.Type == token.IDENT:
		p.next()
		t := &ast.Type{BaseNode: base(tok), Kind: ast.TypeBasic, Name: tok.Literal}
		// A type of another module: module.Name
		if p.at(token.DOT) && p.peekAt(1).Type == token.IDENT {
			p.next()
			member := p.next()
			t.Name += "." + member.Literal
			t.Tok.Literal = t.Name
			t.Tok.EndPos = member.EndPos
		}
		if p.accept(token.LT) {
			t.Generics = p.parseTypeList(token.GT)
			p.expect(token.GT, "'>'")
		}
		return t

	case tok.Type == token.LBRACKET:
		p.next()
		elem := p.parseType()
		p.expect(token.RBRACKET, "']'")
		return &ast.Type{BaseNode: base(tok), Kind: ast.TypeArray, ElemType: elem}

	case tok.Type == token.LPAREN:
		p.next()
		t := p.parseType()
		if !p.at(token.COMMA) {
			p.expect(token.RPAREN, "')'")
			return t
		}
		// (Int, String): a tuple
		elems := []*ast.Type{t}
		for p.accept(token.COMMA) && !p.at(token.RPAREN) {
			elems = append(elems, p.parseType())
		}
		p.expect(token.RPAREN, "')'")
		p.checkTupleSize(tok, len(elems))
		return &ast.Type{BaseNode: base(tok), Kind: ast.TypeBasic, Name: ast.TupleName(len(elems)), Generics: elems}

	case tok.Type == token.ELLIPSIS:
		p.next()
		return &ast.Type{BaseNode: base(tok), Kind: ast.TypeVariadic, ElemType: p.parseType()}

	case tok.Type == token.FN:
		p.next()
		t := &ast.Type{BaseNode: base(tok), Kind: ast.TypeFunction}
		p.expect(token.LPAREN, "'('")
		t.ParamTypes = p.parseTypeList(token.RPAREN)
		p.expect(token.RPAREN, "')'")
		if p.accept(token.ARROW) {
			t.ReturnType = p.parseType()
		}
		return t
	}

	p.fail("expected type, found %s", describe(tok))
	return nil
}

func (p *Parser) parseTypeList(end token.TokenType) []*ast.Type {
	var list []*ast.Type
	for !p.at(end) {
		list = append(list, p.parseType())
		if !p.accept(token.COMMA) {
			break
		}
	}
	return list
}

// --- Statements ---

func (p *Parser) parseBlock() *ast.BlockStmt {
	tok := p.expect(token.LBRACE, "'{'")
	block := &ast.BlockStmt{BaseNode: base(tok)}

	// Struct literals are fine again inside a block body.
	saved := p.noStructLit
	p.noStructLit = false
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		if stmt := p.parseStmtRecovering(); stmt != nil {
			block.Stmts = append(block.Stmts, stmt)
		}
		// What a statement that stands for several left behind
		block.Stmts = append(block.Stmts, p.pendingStmts...)
		p.pendingStmts = nil
	}
	p.noStructLit = saved

	p.expect(token.RBRACE, "'}'")
	return block
}

// parseStmtRecovering parses one statement. On a syntax error it skips to
// the end of the broken statement and returns nil, so the following
// statements are still parsed and checked.
func (p *Parser) parseStmtRecovering() (stmt ast.Stmt) {
	if p.speculating {
		return p.parseStmt()
	}

	start := p.pos
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
			stmt = nil
			if p.pos == start {
				p.next()
			}
			p.skipToStmtEnd()
		}
	}()
	return p.parseStmt()
}

// skipToStmtEnd advances past the next ';' at the current nesting level, or
// stops before the '}' that closes the enclosing block.
func (p *Parser) skipToStmtEnd() {
	depth := 0
	for !p.at(token.EOF) {
		switch p.cur().Type {
		case token.LBRACE:
			depth++
		case token.RBRACE:
			if depth == 0 {
				return
			}
			depth--
			if depth == 0 {
				p.next()
				return
			}
		case token.SEMICOLON:
			if depth == 0 {
				p.next()
				return
			}
		}
		p.next()
	}
}

func (p *Parser) parseStmt() ast.Stmt {
	tok := p.cur()

	switch tok.Type {
	case token.LET, token.CONST:
		if p.peekAt(1).Type == token.LPAREN {
			return p.parseLetTuple()
		}
		return p.parseLetStmt()
	case token.IF:
		return p.parseIfStmt()
	case token.WHILE:
		p.next()
		cond := p.parseHeaderExpr()
		return &ast.WhileStmt{BaseNode: base(tok), Cond: cond, Body: p.parseBlock()}
	case token.FOR:
		p.next()
		// for item in items, or for index, item in items
		stmt := &ast.ForStmt{BaseNode: base(tok)}
		// for (key, value) in pairs: each item is a tuple, taken apart
		var apart []token.Token
		loopVar := func() string {
			if p.at(token.LPAREN) {
				apart = p.parseTupleNames()
				return p.makeName("tuple")
			}
			return p.expect(token.IDENT, "loop variable name").Literal
		}
		nameTok := p.cur()
		stmt.Var = loopVar()
		if p.accept(token.COMMA) {
			if apart != nil {
				p.fail("expected 'in': the position comes first, as in for i, (key, value) in pairs")
			}
			stmt.Index = &ast.ForIndex{BaseNode: base(nameTok), Name: nameTok.Literal}
			stmt.Var = loopVar()
		}
		p.expect(token.IN, "'in'")
		stmt.Iterable = p.parseHeaderExpr()
		stmt.Body = p.parseBlock()
		if apart != nil {
			item := &ast.Ident{BaseNode: base(nameTok), Name: stmt.Var}
			stmt.Body.Stmts = append(p.tupleLets(apart, item, false), stmt.Body.Stmts...)
		}
		return stmt
	case token.MATCH:
		return p.parseMatchStmt()
	case token.DEFER:
		// defer call(); or defer { ... }: what to do when the block is left
		p.next()
		stmt := &ast.DeferStmt{BaseNode: base(tok)}
		if p.at(token.LBRACE) {
			stmt.Body = p.parseBlock()
		} else {
			stmt.Body = &ast.BlockStmt{BaseNode: base(tok), Stmts: []ast.Stmt{p.parseExprStmt()}}
		}
		return stmt
	case token.RETURN:
		p.next()
		stmt := &ast.ReturnStmt{BaseNode: base(tok)}
		if !p.at(token.SEMICOLON) {
			stmt.Value = p.parseExpr()
		}
		p.expectSemicolon()
		return stmt
	case token.BREAK:
		p.next()
		p.expectSemicolon()
		return &ast.BreakStmt{BaseNode: base(tok)}
	case token.CONTINUE:
		p.next()
		p.expectSemicolon()
		return &ast.ContinueStmt{BaseNode: base(tok)}
	case token.LBRACE:
		return p.parseBlock()
	}

	return p.parseExprStmt()
}

func (p *Parser) parseLetStmt() *ast.LetStmt {
	tok := p.next()
	stmt := &ast.LetStmt{BaseNode: base(tok), IsConst: tok.Type == token.CONST}
	stmt.Name = p.expect(token.IDENT, "variable name").Literal
	if p.accept(token.COLON) {
		stmt.Type = p.parseType()
	}
	p.expect(token.ASSIGN, "'='")
	stmt.Value = p.parseExpr()
	p.expectSemicolon()
	return stmt
}

// parseLetTuple parses `let (a, b) = pair;`, which names the values of a
// tuple. It is short for
//
//	let __tuple1 = pair;
//	let a = __tuple1.0;
//	let b = __tuple1.1;
//
// and is returned as the first of these, the others being kept for the
// block to take (see parseBlock). A `_` takes a value without naming it.
func (p *Parser) parseLetTuple() ast.Stmt {
	tok := p.next()
	names := p.parseTupleNames()
	p.expect(token.ASSIGN, "'='")
	whole := &ast.LetStmt{BaseNode: base(tok), Name: p.makeName("tuple"), Value: p.parseExpr()}
	p.expectSemicolon()

	item := &ast.Ident{BaseNode: base(tok), Name: whole.Name}
	p.pendingStmts = append(p.pendingStmts, p.tupleLets(names, item, tok.Type == token.CONST)...)
	return whole
}

// parseTupleNames parses the `(a, b)` of a tuple being taken apart.
func (p *Parser) parseTupleNames() []token.Token {
	open := p.expect(token.LPAREN, "'('")
	var names []token.Token
	for !p.at(token.RPAREN) {
		names = append(names, p.expect(token.IDENT, "a name for a value of the tuple"))
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN, "')'")
	if len(names) < ast.MinTupleSize {
		p.failWith(ErrUnexpectedToken, "Name each value of the tuple: let (first, second) = pair;",
			"a tuple has at least two values, and %d %s named here", len(names), map[bool]string{true: "is", false: "are"}[len(names) == 1])
	}
	p.checkTupleSize(open, len(names))
	return names
}

// tupleLets declares a variable for each named value of a tuple.
func (p *Parser) tupleLets(names []token.Token, tuple *ast.Ident, isConst bool) []ast.Stmt {
	var lets []ast.Stmt
	for i, name := range names {
		if name.Literal == "_" {
			continue
		}
		copy := *tuple
		lets = append(lets, &ast.LetStmt{
			BaseNode: base(name),
			Name:     name.Literal,
			IsConst:  isConst,
			Value:    &ast.MemberExpr{BaseNode: base(name), Expr: &copy, Name: fmt.Sprint(i), TupleSize: len(names)},
		})
	}
	return lets
}

// tuple builds the value `(a, b)`: the variant of the enum behind tuples
// of that size.
func (p *Parser) tuple(open token.Token, elems []ast.Expr) ast.Expr {
	p.checkTupleSize(open, len(elems))
	enum := &ast.Ident{BaseNode: base(open), Name: ast.TupleName(len(elems))}
	variant := &ast.MemberExpr{BaseNode: base(open), Expr: enum, Name: ast.TupleVariant}
	return &ast.CallExpr{BaseNode: base(open), Func: variant, Args: elems}
}

func (p *Parser) checkTupleSize(open token.Token, size int) {
	if size <= ast.MaxTupleSize {
		return
	}
	if !p.speculating {
		p.errs.ReportErrorWithSuggestion(ErrUnexpectedToken,
			fmt.Sprintf("a tuple holds %d values at most, and this one has %d", ast.MaxTupleSize, size),
			"Use a struct: with that many values, each deserves a name", open, p.sourceLines)
	}
	panic(bailout{})
}

func (p *Parser) parseIfStmt() *ast.IfStmt {
	tok := p.expect(token.IF, "'if'")
	stmt := &ast.IfStmt{BaseNode: base(tok)}
	stmt.Cond = p.parseHeaderExpr()
	stmt.Then = p.parseBlock()
	if p.accept(token.ELSE) {
		if p.at(token.IF) {
			stmt.Else = p.parseIfStmt()
		} else {
			stmt.Else = p.parseBlock()
		}
	}
	return stmt
}

// parseMatchStmt parses a match statement. A `default:` case is stored with a
// nil pattern.
func (p *Parser) parseMatchStmt() *ast.MatchStmt {
	return p.parseMatch(false)
}

// parseMatch parses a match: the statement, or the one that gives a value
// (asValue), which only differ in what ends a case. In a statement a case
// that is an expression ends with ';'. Where a value is expected it may
// end with ',' as well, and the last one with nothing.
//
//	case 1: ...                          a value
//	case 1..10: ...                      a range of numbers
//	case Int: ...                        a type
//	case Shape.Rect(w, h): ...           a variant, its values named
//	case Light.Red | Light.Amber: ...    any of several patterns
//	case Shape.Circle(r) if r > 10.0:    a pattern and a condition
func (p *Parser) parseMatch(asValue bool) *ast.MatchStmt {
	tok := p.expect(token.MATCH, "'match'")
	stmt := &ast.MatchStmt{BaseNode: base(tok)}
	stmt.Cond = p.parseHeaderExpr()

	// Struct literals are fine again between the braces of the match
	saved := p.noStructLit
	p.noStructLit = false
	defer func() { p.noStructLit = saved }()

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		caseTok := p.cur()
		matchCase := &ast.MatchCase{BaseNode: base(caseTok)}

		if caseTok.Type == token.IDENT && caseTok.Literal == "default" && p.peekAt(1).Type == token.COLON {
			p.next()
		} else {
			p.expect(token.CASE, "'case' or 'default'")
			matchCase.Pattern = p.parseCasePattern()
			for p.accept(token.PIPE) {
				matchCase.Alternatives = append(matchCase.Alternatives, p.parseCasePattern())
			}
			if p.accept(token.IF) {
				matchCase.Guard = p.parseHeaderExpr()
			}
		}
		p.expect(token.COLON, "':'")

		switch {
		case p.at(token.LBRACE):
			matchCase.Body = p.parseBlock()
			if asValue {
				p.accept(token.COMMA)
			}
		case asValue:
			matchCase.Body = p.parseExpr()
			if !p.accept(token.COMMA) && !p.accept(token.SEMICOLON) && !p.at(token.RBRACE) {
				p.fail("expected ';' after the value of a case, found %s", describe(p.cur()))
			}
		default:
			matchCase.Body = p.parseExpr()
			p.expectSemicolon()
		}
		stmt.Cases = append(stmt.Cases, matchCase)
	}
	p.expect(token.RBRACE, "'}'")
	return stmt
}

// parseCasePattern parses one pattern of a case.
func (p *Parser) parseCasePattern() ast.Node {
	if typ := p.tryTypePattern(); typ != nil {
		return typ
	}
	return p.variantPattern(p.parseHeaderExpr(), true)
}

// tryTypePattern parses the pattern of a match case as a type when it can
// only be one: `Int`, `[Int]`, `String?`, `fn(Int) -> Int`, `Box<Int>`.
// A bare name is left alone, since `case Point:` and `case limit:` look the
// same here; the compiler decides once it knows what the name refers to.
func (p *Parser) tryTypePattern() (typ *ast.Type) {
	start := p.pos
	wasSpeculating := p.speculating
	p.speculating = true
	defer func() {
		p.speculating = wasSpeculating
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
			p.pos = start
			typ = nil
		}
	}()

	// (a, b) is a tuple of patterns, whose names are new variables, and
	// never the tuple type of types called a and b
	if p.at(token.LPAREN) {
		p.fail("not a type pattern")
	}

	// One type: `Int | String` is two patterns, each read on its own
	parsed := p.parsePostfixType()
	bareName := parsed.Kind == ast.TypeBasic && len(parsed.Generics) == 0 && !isBuiltinTypeToken(parsed.Tok.Type)
	if bareName || !(p.at(token.COLON) || p.at(token.PIPE) || p.at(token.IF)) {
		p.fail("not a type pattern")
	}
	return parsed
}

// parseExprStmt parses an expression statement or an assignment. Assignments
// are represented as a BinaryExpr with Op "=", or "+=" and the like for the
// compound forms. `i++` and `i--` are `i += 1` and `i -= 1`; they are
// statements, so there is no value of `i++` to define.
func (p *Parser) parseExprStmt() *ast.ExprStmt {
	tok := p.cur()
	expr, _ := p.parseExprOrAssignment()
	p.expectSemicolon()
	return &ast.ExprStmt{BaseNode: base(tok), Expr: expr}
}

// parseExprOrAssignment parses what an expression statement is made of:
// an expression, or an assignment to one (`x = 1`, `x += 1`, `x++`). It
// reports whether it was an assignment, which has no value.
func (p *Parser) parseExprOrAssignment() (ast.Expr, bool) {
	expr := p.parseExpr()

	opTok := p.cur()
	switch opTok.Type {
	case token.ASSIGN, token.PLUS_ASSIGN, token.MINUS_ASSIGN, token.STAR_ASSIGN, token.SLASH_ASSIGN, token.PERCENT_ASSIGN:
		p.checkAssignable(expr)
		p.next()
		return &ast.BinaryExpr{BaseNode: base(opTok), Left: expr, Op: opTok.Literal, Right: p.parseExpr()}, true

	case token.INC, token.DEC:
		p.checkAssignable(expr)
		p.next()
		one := &ast.Literal{BaseNode: base(opTok), Value: "1", Kind: "int"}
		return &ast.BinaryExpr{BaseNode: base(opTok), Left: expr, Op: opTok.Literal[:1] + "=", Right: one}, true
	}
	return expr, false
}

// checkAssignable fails unless expr can stand on the left of an assignment.
func (p *Parser) checkAssignable(expr ast.Expr) {
	switch expr.(type) {
	case *ast.Ident, *ast.IndexExpr, *ast.MemberExpr:
	default:
		p.failWith(ErrInvalidAssign, "Only variables, fields and array elements can be assigned to",
			"invalid assignment target")
	}
}

// --- Expressions ---

// parseHeaderExpr parses the expression in an if/while/for/match header,
// where `name {` starts the body rather than a struct literal.
func (p *Parser) parseHeaderExpr() ast.Expr {
	saved := p.noStructLit
	p.noStructLit = true
	expr := p.parseExpr()
	p.noStructLit = saved
	return expr
}

// parseNestedExpr parses an expression inside delimiters, where struct
// literals are unambiguous again.
func (p *Parser) parseNestedExpr() ast.Expr {
	saved := p.noStructLit
	p.noStructLit = false
	expr := p.parseExpr()
	p.noStructLit = saved
	return expr
}

func (p *Parser) parseExpr() ast.Expr {
	left := p.parseBinary(1)

	// low..high and low..=high: looser than every operator, so that
	// 0..count - 1 is the range up to count - 1
	if tok := p.cur(); tok.Type == token.DOTDOT || tok.Type == token.DOTDOT_EQ {
		p.next()
		return &ast.RangeExpr{BaseNode: base(tok), Low: left, High: p.parseBinary(1), Inclusive: tok.Type == token.DOTDOT_EQ}
	}
	return left
}

func binaryPrecedence(typ token.TokenType) int {
	switch typ {
	case token.OR, token.AND, token.QUESTION_QUESTION,
		token.EQ, token.NEQ, token.LT, token.LTE, token.GT, token.GTE,
		token.PLUS, token.MINUS, token.STAR, token.SLASH, token.PERCENT:
		return typ.Precedence()
	}
	return 0
}

func (p *Parser) parseBinary(minPrec int) ast.Expr {
	left := p.parseUnary()
	for {
		tok := p.cur()
		prec := binaryPrecedence(tok.Type)
		if prec == 0 || prec < minPrec {
			return left
		}
		p.next()
		right := p.parseBinary(prec + 1)
		left = &ast.BinaryExpr{BaseNode: base(tok), Left: left, Op: tok.Literal, Right: right}
	}
}

func (p *Parser) parseUnary() ast.Expr {
	tok := p.cur()
	if tok.Type == token.MINUS || tok.Type == token.NOT {
		p.next()
		return &ast.UnaryExpr{BaseNode: base(tok), Op: tok.Literal, Expr: p.parseUnary()}
	}

	// `value as Type`, `value is Type` and `value is Pattern` bind tighter
	// than any binary operator. `is` is not a reserved word: after a
	// complete expression an identifier cannot follow, so there it can only
	// be this operator.
	expr := p.parsePostfix()
	for {
		tok := p.cur()
		switch {
		case tok.Type == token.AS:
			p.next()
			expr = &ast.CastExpr{BaseNode: base(tok), Expr: expr, Type: p.parsePostfixType()}
		case tok.Type == token.IDENT && tok.Literal == "is":
			p.next()
			expr = p.parseIsTest(tok, expr)
		default:
			return expr
		}
	}
}

func (p *Parser) parsePostfix() ast.Expr {
	expr := p.parsePrimary()
	for {
		if next, ok := p.parsePostfixOp(expr); ok {
			expr = next
			continue
		}

		tok := p.cur()
		switch tok.Type {
		case token.QUESTION_DOT:
			// value?.member: what follows is done with the value, by a name
			// that stands for it, and only if it is not null. That goes for
			// all that follows: in user?.address.city the city is read
			// when there is a user, and nothing is when there is none.
			p.next()
			var access ast.Expr = &ast.Ident{BaseNode: base(expr.Token()), Name: p.makeName("safe")}
			if p.at(token.INT) || p.at(token.FLOAT) {
				access = p.parseTuplePositions(access) // pair?.0
			} else {
				nameTok := p.expectMemberName("member name")
				access = &ast.MemberExpr{BaseNode: base(nameTok), Expr: access, Name: nameTok.Literal, TypeArgs: p.tryTypeArgs()}
			}
			for {
				next, ok := p.parsePostfixOp(access)
				if !ok {
					break
				}
				access = next
			}
			expr = &ast.BinaryExpr{BaseNode: base(tok), Left: expr, Op: ast.SafeAccessOp, Right: access}
		case token.QUESTION:
			// value?: the value, or leave the function with the failure
			p.next()
			expr = &ast.UnaryExpr{BaseNode: base(tok), Op: ast.TryOp, Expr: expr}
		default:
			return expr
		}
	}
}

// parseTuplePositions parses the number after a dot, which reads a value
// of a tuple by its position. Two positions in a row, pair.0.1, come from
// the lexer as one token: the number 0.1.
func (p *Parser) parseTuplePositions(expr ast.Expr) ast.Expr {
	numTok := p.next()
	for _, part := range strings.Split(numTok.Literal, ".") {
		if _, ok := ast.TupleIndex(part); !ok {
			p.pos--
			p.fail("expected the position of a value in a tuple, as in pair.0, found %s", describe(numTok))
		}
		expr = &ast.MemberExpr{BaseNode: base(numTok), Expr: expr, Name: part}
	}
	return expr
}

// parsePostfixOp parses one thing done to an expression, if one follows:
// a call, an index or a member. It reports whether there was one.
func (p *Parser) parsePostfixOp(expr ast.Expr) (ast.Expr, bool) {
	tok := p.cur()
	switch tok.Type {
	case token.LPAREN:
		p.next()
		// Anchor the call at its callee so diagnostics underline the
		// name rather than the parenthesis
		call := &ast.CallExpr{BaseNode: base(expr.Token()), Func: expr}
		call.Args = p.parseExprList(token.RPAREN)
		p.expect(token.RPAREN, "')'")
		return call, true
	case token.LBRACKET:
		p.next()
		index := p.parseNestedExpr()
		p.expect(token.RBRACKET, "']'")
		return &ast.IndexExpr{BaseNode: base(tok), Expr: expr, Index: index}, true
	case token.DOT:
		p.next()
		// pair.0: a value of a tuple, by its position. Two in a row,
		// pair.0.1, reach the lexer looking like the number 0.1
		if p.at(token.INT) || p.at(token.FLOAT) {
			return p.parseTuplePositions(expr), true
		}
		nameTok := p.expectMemberName("member name")
		name := nameTok.Literal
		// Like a generic function name, a method name may carry type
		// arguments when a call follows: stack.map<String>(f)
		typeArgs := p.tryTypeArgs()

		// A struct of another module: module.Name { ... }
		if owner, ok := expr.(*ast.Ident); ok && len(owner.TypeArgs) == 0 && !strings.HasPrefix(owner.Name, "__") && p.at(token.LBRACE) && p.structLitFollows() {
			qualified := owner.Tok
			qualified.Literal = owner.Name + "." + name
			qualified.EndPos = nameTok.EndPos
			return p.parseStructInit(qualified, typeArgs), true
		}
		return &ast.MemberExpr{BaseNode: base(tok), Expr: expr, Name: name, TypeArgs: typeArgs}, true
	}
	return expr, false
}

func (p *Parser) parseExprList(end token.TokenType) []ast.Expr {
	var list []ast.Expr
	for !p.at(end) {
		list = append(list, p.parseNestedExpr())
		if !p.accept(token.COMMA) {
			break
		}
	}
	return list
}

func (p *Parser) parsePrimary() ast.Expr {
	tok := p.cur()

	switch tok.Type {
	case token.INT:
		p.next()
		return &ast.Literal{BaseNode: base(tok), Value: tok.Literal, Kind: "int"}
	case token.FLOAT:
		p.next()
		return &ast.Literal{BaseNode: base(tok), Value: tok.Literal, Kind: "float"}
	case token.STRING:
		p.next()
		return &ast.Literal{BaseNode: base(tok), Value: tok.Literal, Kind: "string"}
	case token.TRUE, token.FALSE:
		p.next()
		return &ast.Literal{BaseNode: base(tok), Value: tok.Literal, Kind: "bool"}
	case token.NULL:
		p.next()
		return &ast.Literal{BaseNode: base(tok), Value: tok.Literal, Kind: "null"}

	case token.MATCH:
		// A match where a value is expected gives the value of its case
		return &ast.MatchExpr{BaseNode: base(tok), Match: p.parseMatch(true)}

	case token.IDENT:
		if isShortFnArrow(p.peekAt(1)) {
			// item -> item.name
			p.next()
			return p.parseShortFn(tok, []ast.Expr{&ast.Ident{BaseNode: base(tok), Name: tok.Literal}})
		}
		return p.parseIdentExpr()

	case token.LPAREN:
		p.next()
		if p.at(token.RPAREN) && isShortFnArrow(p.peekAt(1)) {
			// () -> now()
			p.next()
			return p.parseShortFn(tok, nil)
		}
		expr := p.parseNestedExpr()
		elems := []ast.Expr{expr}
		for p.accept(token.COMMA) && !p.at(token.RPAREN) {
			elems = append(elems, p.parseNestedExpr())
		}
		p.expect(token.RPAREN, "')'")
		if isShortFnArrow(p.cur()) {
			// (a, b) -> a + b: what was read is its parameters
			return p.parseShortFn(tok, elems)
		}
		if len(elems) == 1 {
			return expr
		}
		// (1, "a"): a tuple
		return p.tuple(tok, elems)

	case token.LBRACKET:
		p.next()
		arr := &ast.ArrayLiteralExpr{BaseNode: base(tok)}
		arr.Elements = p.parseExprList(token.RBRACKET)
		p.expect(token.RBRACKET, "']'")
		return arr

	case token.FN:
		p.next()
		fn := &ast.FnExpr{BaseNode: base(tok)}
		fn.Params = p.parseParams()
		if p.accept(token.ARROW) {
			fn.ReturnType = p.parseType()
		}
		fn.Body = p.parseBlock()
		return fn
	}

	p.fail("expected expression, found %s", describe(tok))
	return nil
}

// isShortFnArrow reports whether a token is the arrow of a function
// written without types, or the `=>` other languages write there, which
// is reported with what to write instead.
func isShortFnArrow(tok token.Token) bool {
	return tok.Type == token.ARROW || tok.Type == token.FAT_ARROW
}

// parseShortFn parses a function written without types, from its arrow:
//
//	item -> item.name
//	(a, b) -> a + b
//	() -> now()
//	line -> { count++; return strings.trim(line); }
//
// The arrow is the one of `fn(item: Person) -> String`: what is on its
// left gives what is on its right. names are the parameters as they were
// read, as expressions. Their types and that of the result are those of
// the function type expected where the function stands, which the type
// checker fills in.
func (p *Parser) parseShortFn(start token.Token, names []ast.Expr) ast.Expr {
	fn := &ast.FnExpr{BaseNode: base(start), Short: true}
	for _, name := range names {
		ident, ok := name.(*ast.Ident)
		if !ok || len(ident.TypeArgs) > 0 {
			if !p.speculating {
				p.errs.ReportErrorWithSuggestion(ErrUnexpectedToken,
					"the parameters of a function written with '->' are names",
					"Write them as (first, second) -> ..., or use fn(first: Type) -> Type { ... } to give their types",
					name.Token(), p.sourceLines)
			}
			panic(bailout{})
		}
		fn.Params = append(fn.Params, &ast.Param{BaseNode: ident.BaseNode, Name: ident.Name})
	}

	if p.at(token.FAT_ARROW) {
		p.failWith(ErrUnexpectedToken, "Write '->', as in: item -> item.name", "a function is written with '->', not '=>'")
	}
	arrow := p.expect(token.ARROW, "'->'")
	if p.at(token.LBRACE) {
		fn.Body = p.parseBlock()
		return fn
	}
	// One expression: what the function gives. Or one assignment, which
	// gives nothing: () -> count++
	saved := p.noStructLit
	p.noStructLit = false
	value, isAssignment := p.parseExprOrAssignment()
	p.noStructLit = saved
	if isAssignment {
		fn.Body = &ast.BlockStmt{BaseNode: base(arrow), Stmts: []ast.Stmt{&ast.ExprStmt{BaseNode: base(arrow), Expr: value}}}
		return fn
	}
	fn.ExprBody = true
	fn.Body = &ast.BlockStmt{BaseNode: base(arrow), Stmts: []ast.Stmt{&ast.ReturnStmt{BaseNode: base(arrow), Value: value}}}
	return fn
}

// parseIdentExpr parses an identifier, optionally with type arguments, and
// the struct literal that may follow it.
func (p *Parser) parseIdentExpr() ast.Expr {
	tok := p.next()
	typeArgs := p.tryTypeArgs()

	if p.at(token.LBRACE) && p.structLitFollows() {
		return p.parseStructInit(tok, typeArgs)
	}
	return &ast.Ident{BaseNode: base(tok), Name: tok.Literal, TypeArgs: typeArgs}
}

// tryTypeArgs speculatively parses `<T, U>` after an identifier. It only
// commits when the closing '>' is followed by '(' or '{', which separates
// generic instantiation from the comparison `a < b`.
func (p *Parser) tryTypeArgs() (args []*ast.Type) {
	if !p.at(token.LT) {
		return nil
	}

	start := p.pos
	wasSpeculating := p.speculating
	p.speculating = true
	defer func() {
		p.speculating = wasSpeculating
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
			p.pos = start
			args = nil
		}
	}()

	p.next()
	list := p.parseTypeList(token.GT)
	p.expect(token.GT, "'>'")
	// `(` and `{` follow a generic call and a generic struct literal, `.`
	// the name of a generic enum: Option<Int>.None
	if len(list) == 0 || (!p.at(token.LPAREN) && !p.at(token.LBRACE) && !p.at(token.DOT)) {
		p.fail("not type arguments")
	}
	return list
}

// structLitFollows reports whether the '{' at the cursor opens a struct
// literal: `{}` or `{ name: ...`.
func (p *Parser) structLitFollows() bool {
	if p.noStructLit {
		return false
	}
	if p.peekAt(1).Type == token.RBRACE {
		return true
	}
	return p.peekAt(1).Type == token.IDENT && p.peekAt(2).Type == token.COLON
}

func (p *Parser) parseStructInit(nameTok token.Token, typeArgs []*ast.Type) *ast.StructInitExpr {
	init := &ast.StructInitExpr{
		BaseNode: base(nameTok),
		Name:     nameTok.Literal,
		TypeArgs: typeArgs,
		Fields:   make(map[string]ast.Expr),
	}

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		fieldTok := p.expect(token.IDENT, "field name")
		if _, dup := init.Fields[fieldTok.Literal]; dup {
			p.pos--
			p.failWith(ErrDuplicateField, "Remove one of the two initializers",
				"duplicate field '%s' in struct literal", fieldTok.Literal)
		}
		p.expect(token.COLON, "':'")
		init.Fields[fieldTok.Literal] = p.parseNestedExpr()
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RBRACE, "'}'")
	return init
}

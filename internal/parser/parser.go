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
		return "Valid escapes are \\n, \\t, \\r, \\0, \\\\ and \\\""
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
	p.fail("expected declaration (fn, struct, enum, trait, impl, import), found %s", describe(p.cur()))
	return nil
}

// parseExternDecl parses `extern fn name(params) -> Type;`: a function the
// runtime provides. It has a signature and no body.
func (p *Parser) parseExternDecl() *ast.FnDecl {
	p.expect(token.EXTERN, "'extern'")
	tok := p.expect(token.FN, "'fn' after 'extern'")
	decl := &ast.FnDecl{BaseNode: base(tok), Extern: true}

	nameTok := p.expect(token.IDENT, "function name")
	decl.Name = nameTok.Literal
	decl.BaseNode = base(nameTok)
	if p.at(token.LT) {
		p.failWith(ErrUnexpectedToken, "Give the parameters concrete types, or 'Any'",
			"an extern function cannot have type parameters")
	}
	decl.Params = p.parseParams()
	if p.accept(token.ARROW) {
		decl.ReturnType = p.parseType()
	}
	if p.at(token.LBRACE) {
		p.failWith(ErrUnexpectedToken, "Remove 'extern' to write the function here, or end the declaration with ';'",
			"an extern function has no body: the runtime provides it")
	}
	p.expectSemicolon()
	return decl
}

func (p *Parser) parseTypeParams() []string {
	var params []string
	if !p.accept(token.LT) {
		return nil
	}
	for {
		params = append(params, p.expect(token.IDENT, "type parameter name").Literal)
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.GT, "'>'")
	return params
}

// parseFnDecl parses a function declaration. Trait methods have no body.
func (p *Parser) parseFnDecl(traitMethod bool) *ast.FnDecl {
	tok := p.expect(token.FN, "'fn'")
	decl := &ast.FnDecl{BaseNode: base(tok), IsTraitMethod: traitMethod}
	decl.Name = p.expect(token.IDENT, "function name").Literal
	decl.TypeParams = p.parseTypeParams()
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
	decl.TypeParams = p.parseTypeParams()

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		nameTok := p.expect(token.IDENT, "field name")
		field := &ast.Field{BaseNode: base(nameTok), Name: nameTok.Literal}
		p.expect(token.COLON, "':'")
		field.Type = p.parseType()
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
	decl.TypeParams = p.parseTypeParams()

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		nameTok := p.expect(token.IDENT, "variant name")
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
	decl.TypeParams = p.parseTypeParams()

	first := p.expectTypeName("trait or struct name")
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
		p.expect(token.RPAREN, "')'")
		return t

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
		nameTok := p.expect(token.IDENT, "loop variable name")
		stmt.Var = nameTok.Literal
		if p.accept(token.COMMA) {
			stmt.Index = &ast.ForIndex{BaseNode: base(nameTok), Name: nameTok.Literal}
			stmt.Var = p.expect(token.IDENT, "loop variable name").Literal
		}
		p.expect(token.IN, "'in'")
		stmt.Iterable = p.parseHeaderExpr()
		stmt.Body = p.parseBlock()
		return stmt
	case token.MATCH:
		return p.parseMatchStmt()
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
	tok := p.expect(token.MATCH, "'match'")
	stmt := &ast.MatchStmt{BaseNode: base(tok)}
	stmt.Cond = p.parseHeaderExpr()

	p.expect(token.LBRACE, "'{'")
	for !p.at(token.RBRACE) {
		caseTok := p.cur()
		matchCase := &ast.MatchCase{BaseNode: base(caseTok)}

		if caseTok.Type == token.IDENT && caseTok.Literal == "default" && p.peekAt(1).Type == token.COLON {
			p.next()
		} else {
			p.expect(token.CASE, "'case' or 'default'")
			if typ := p.tryTypePattern(); typ != nil {
				matchCase.Pattern = typ
			} else {
				matchCase.Pattern = p.parseHeaderExpr()
			}
		}
		p.expect(token.COLON, "':'")

		if p.at(token.LBRACE) {
			matchCase.Body = p.parseBlock()
		} else {
			matchCase.Body = p.parseExpr()
			p.expectSemicolon()
		}
		stmt.Cases = append(stmt.Cases, matchCase)
	}
	p.expect(token.RBRACE, "'}'")
	return stmt
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

	parsed := p.parseType()
	bareName := parsed.Kind == ast.TypeBasic && len(parsed.Generics) == 0 && !isBuiltinTypeToken(parsed.Tok.Type)
	if bareName || !p.at(token.COLON) {
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
	expr := p.parseExpr()

	opTok := p.cur()
	switch opTok.Type {
	case token.ASSIGN, token.PLUS_ASSIGN, token.MINUS_ASSIGN, token.STAR_ASSIGN, token.SLASH_ASSIGN, token.PERCENT_ASSIGN:
		p.checkAssignable(expr)
		p.next()
		expr = &ast.BinaryExpr{BaseNode: base(opTok), Left: expr, Op: opTok.Literal, Right: p.parseExpr()}

	case token.INC, token.DEC:
		p.checkAssignable(expr)
		p.next()
		one := &ast.Literal{BaseNode: base(opTok), Value: "1", Kind: "int"}
		expr = &ast.BinaryExpr{BaseNode: base(opTok), Left: expr, Op: opTok.Literal[:1] + "=", Right: one}
	}

	p.expectSemicolon()
	return &ast.ExprStmt{BaseNode: base(tok), Expr: expr}
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
	return p.parseBinary(1)
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

	// `value as Type` and `value is Type` bind tighter than any binary
	// operator. `is` is not a reserved word: after a complete expression an
	// identifier cannot follow, so there it can only be this operator.
	expr := p.parsePostfix()
	for {
		tok := p.cur()
		switch {
		case tok.Type == token.AS:
			p.next()
			expr = &ast.CastExpr{BaseNode: base(tok), Expr: expr, Type: p.parsePostfixType()}
		case tok.Type == token.IDENT && tok.Literal == "is":
			p.next()
			expr = &ast.TypeTestExpr{BaseNode: base(tok), Expr: expr, Type: p.parsePostfixType()}
		default:
			return expr
		}
	}
}

func (p *Parser) parsePostfix() ast.Expr {
	expr := p.parsePrimary()
	for {
		tok := p.cur()
		switch tok.Type {
		case token.LPAREN:
			p.next()
			// Anchor the call at its callee so diagnostics underline the
			// name rather than the parenthesis
			call := &ast.CallExpr{BaseNode: base(expr.Token()), Func: expr}
			call.Args = p.parseExprList(token.RPAREN)
			p.expect(token.RPAREN, "')'")
			expr = call
		case token.LBRACKET:
			p.next()
			index := p.parseNestedExpr()
			p.expect(token.RBRACKET, "']'")
			expr = &ast.IndexExpr{BaseNode: base(tok), Expr: expr, Index: index}
		case token.DOT:
			p.next()
			nameTok := p.expect(token.IDENT, "member name")
			name := nameTok.Literal
			// Like a generic function name, a method name may carry type
			// arguments when a call follows: stack.map<String>(f)
			typeArgs := p.tryTypeArgs()

			// A struct of another module: module.Name { ... }
			if owner, ok := expr.(*ast.Ident); ok && len(owner.TypeArgs) == 0 && p.at(token.LBRACE) && p.structLitFollows() {
				qualified := owner.Tok
				qualified.Literal = owner.Name + "." + name
				qualified.EndPos = nameTok.EndPos
				expr = p.parseStructInit(qualified, typeArgs)
				continue
			}
			expr = &ast.MemberExpr{BaseNode: base(tok), Expr: expr, Name: name, TypeArgs: typeArgs}
		default:
			return expr
		}
	}
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

	case token.IDENT:
		return p.parseIdentExpr()

	case token.LPAREN:
		p.next()
		expr := p.parseNestedExpr()
		p.expect(token.RPAREN, "')'")
		return expr

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
	if len(list) == 0 || (!p.at(token.LPAREN) && !p.at(token.LBRACE)) {
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

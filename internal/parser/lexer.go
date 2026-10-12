package parser

import (
	"strings"

	"github.com/dani-gouken/lumen/internal/token"
)

// Lexer converts Lumen source text into tokens.
type Lexer struct {
	src  []rune
	file string
	pos  int
	line int
	col  int
}

// NewLexer creates a lexer for the given source.
func NewLexer(src, file string) *Lexer {
	return &Lexer{src: []rune(src), file: file, line: 1, col: 1}
}

// Tokenize lexes the whole input. Comments are dropped. The result always
// ends with an EOF token. Malformed input yields ILLEGAL tokens whose
// literal describes the problem.
func (l *Lexer) Tokenize() []token.Token {
	var toks []token.Token
	for {
		tok := l.Next()
		if tok.Type == token.COMMENT {
			continue
		}
		toks = append(toks, tok)
		if tok.Type == token.EOF {
			return toks
		}
	}
}

func (l *Lexer) position() token.Position {
	return token.Position{File: l.file, Line: l.line, Col: l.col}
}

func (l *Lexer) peek(offset int) rune {
	if l.pos+offset >= len(l.src) {
		return 0
	}
	return l.src[l.pos+offset]
}

func (l *Lexer) advance() rune {
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\r', '\n':
			l.advance()
		default:
			return
		}
	}
}

// Next returns the next token.
func (l *Lexer) Next() token.Token {
	l.skipWhitespace()
	start := l.position()

	if l.pos >= len(l.src) {
		return token.NewToken(token.EOF, "", start, start)
	}

	ch := l.peek(0)

	switch {
	case isLetter(ch):
		begin := l.pos
		for l.pos < len(l.src) && (isLetter(l.peek(0)) || isDigit(l.peek(0))) {
			l.advance()
		}
		lit := string(l.src[begin:l.pos])
		return token.NewToken(token.LookupIdent(lit), lit, start, l.position())
	case isDigit(ch):
		return l.lexNumber(start)
	case ch == '"':
		return l.lexString(start)
	}

	// Comments
	if ch == '/' && l.peek(1) == '/' {
		begin := l.pos
		for l.pos < len(l.src) && l.peek(0) != '\n' {
			l.advance()
		}
		return token.NewToken(token.COMMENT, string(l.src[begin:l.pos]), start, l.position())
	}
	if ch == '/' && l.peek(1) == '*' {
		begin := l.pos
		l.advance()
		l.advance()
		for l.pos < len(l.src) && !(l.peek(0) == '*' && l.peek(1) == '/') {
			l.advance()
		}
		if l.pos >= len(l.src) {
			return token.NewToken(token.ILLEGAL, "unterminated block comment", start, l.position())
		}
		l.advance()
		l.advance()
		return token.NewToken(token.COMMENT, string(l.src[begin:l.pos]), start, l.position())
	}

	// Multi-character operators, longest first
	for _, op := range multiCharOps {
		if l.hasPrefix(op.text) {
			for range op.text {
				l.advance()
			}
			return token.NewToken(op.typ, op.text, start, l.position())
		}
	}

	l.advance()
	if typ, ok := singleCharOps[ch]; ok {
		return token.NewToken(typ, string(ch), start, l.position())
	}
	return token.NewToken(token.ILLEGAL, "unexpected character '"+string(ch)+"'", start, l.position())
}

func (l *Lexer) hasPrefix(s string) bool {
	for i, r := range []rune(s) {
		if l.peek(i) != r {
			return false
		}
	}
	return true
}

func (l *Lexer) lexNumber(start token.Position) token.Token {
	begin := l.pos
	typ := token.INT
	for isDigit(l.peek(0)) {
		l.advance()
	}
	if l.peek(0) == '.' && isDigit(l.peek(1)) {
		typ = token.FLOAT
		l.advance()
		for isDigit(l.peek(0)) {
			l.advance()
		}
	}
	if l.peek(0) == 'e' || l.peek(0) == 'E' {
		signed := l.peek(1) == '+' || l.peek(1) == '-'
		if isDigit(l.peek(1)) || (signed && isDigit(l.peek(2))) {
			typ = token.FLOAT
			l.advance()
			if signed {
				l.advance()
			}
			for isDigit(l.peek(0)) {
				l.advance()
			}
		}
	}
	return token.NewToken(typ, string(l.src[begin:l.pos]), start, l.position())
}

func (l *Lexer) lexString(start token.Position) token.Token {
	l.advance() // opening quote
	var sb strings.Builder
	for {
		if l.pos >= len(l.src) || l.peek(0) == '\n' {
			return token.NewToken(token.ILLEGAL, "unterminated string literal", start, l.position())
		}
		ch := l.advance()
		if ch == '"' {
			break
		}
		if ch != '\\' {
			sb.WriteRune(ch)
			continue
		}
		if l.pos >= len(l.src) {
			return token.NewToken(token.ILLEGAL, "unterminated string literal", start, l.position())
		}
		switch esc := l.advance(); esc {
		case 'n':
			sb.WriteRune('\n')
		case 't':
			sb.WriteRune('\t')
		case 'r':
			sb.WriteRune('\r')
		case '0':
			sb.WriteRune(0)
		case '\\', '"':
			sb.WriteRune(esc)
		default:
			return token.NewToken(token.ILLEGAL, "invalid escape sequence '\\"+string(esc)+"'", start, l.position())
		}
	}
	return token.NewToken(token.STRING, sb.String(), start, l.position())
}

var multiCharOps = []struct {
	text string
	typ  token.TokenType
}{
	{"...", token.ELLIPSIS},
	{"->", token.ARROW},
	{"==", token.EQ},
	{"!=", token.NEQ},
	{"<=", token.LTE},
	{">=", token.GTE},
	{"&&", token.AND},
	{"||", token.OR},
	{"??", token.QUESTION_QUESTION},
	{"+=", token.PLUS_ASSIGN},
	{"-=", token.MINUS_ASSIGN},
	{"*=", token.STAR_ASSIGN},
	{"/=", token.SLASH_ASSIGN},
	{"%=", token.PERCENT_ASSIGN},
	{"++", token.INC},
	{"--", token.DEC},
}

var singleCharOps = map[rune]token.TokenType{
	'+': token.PLUS,
	'-': token.MINUS,
	'*': token.STAR,
	'/': token.SLASH,
	'%': token.PERCENT,
	'<': token.LT,
	'>': token.GT,
	'!': token.NOT,
	'?': token.QUESTION,
	'=': token.ASSIGN,
	'&': token.AMPERSAND,
	'|': token.PIPE,
	'(': token.LPAREN,
	')': token.RPAREN,
	'{': token.LBRACE,
	'}': token.RBRACE,
	'[': token.LBRACKET,
	']': token.RBRACKET,
	',': token.COMMA,
	';': token.SEMICOLON,
	':': token.COLON,
	'.': token.DOT,
}

func isLetter(ch rune) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

package parser

import (
	"testing"

	"github.com/dani-gouken/lumen/internal/token"
)

func lexTypes(src string) []token.TokenType {
	var types []token.TokenType
	for _, tok := range NewLexer(src, "test.lum").Tokenize() {
		types = append(types, tok.Type)
	}
	return types
}

func TestLexerTokens(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []token.TokenType
	}{
		{"keywords and idents", "fn main let x_1", []token.TokenType{token.FN, token.IDENT, token.LET, token.IDENT}},
		{"builtin types", "Int String Self", []token.TokenType{token.INT_TYPE, token.STRING_TYPE, token.SELF_TYPE}},
		{"numbers", "42 3.14 10e3 1.5e-2", []token.TokenType{token.INT, token.FLOAT, token.FLOAT, token.FLOAT}},
		{"int followed by member", "1.x", []token.TokenType{token.INT, token.DOT, token.IDENT}},
		{"two char operators", "== != <= >= && || ?? ->", []token.TokenType{
			token.EQ, token.NEQ, token.LTE, token.GTE, token.AND, token.OR, token.QUESTION_QUESTION, token.ARROW,
		}},
		{"one char operators", "= < > ! ? | &", []token.TokenType{
			token.ASSIGN, token.LT, token.GT, token.NOT, token.QUESTION, token.PIPE, token.AMPERSAND,
		}},
		{"ellipsis and dot", "... .", []token.TokenType{token.ELLIPSIS, token.DOT}},
		{"compound assignment", "+= -= *= /= %=", []token.TokenType{
			token.PLUS_ASSIGN, token.MINUS_ASSIGN, token.STAR_ASSIGN, token.SLASH_ASSIGN, token.PERCENT_ASSIGN,
		}},
		{"increment and decrement", "i++ j--", []token.TokenType{token.IDENT, token.INC, token.IDENT, token.DEC}},
		{"spaced signs stay separate", "a - -b + +c", []token.TokenType{
			token.IDENT, token.MINUS, token.MINUS, token.IDENT, token.PLUS, token.PLUS, token.IDENT,
		}},
		{"arrow is not minus and greater", "a->b a - >b", []token.TokenType{
			token.IDENT, token.ARROW, token.IDENT, token.IDENT, token.MINUS, token.GT, token.IDENT,
		}},
		{"comments are skipped", "a // line\n/* block\n */ b", []token.TokenType{token.IDENT, token.IDENT}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lexTypes(tt.src)
			want := append(tt.want, token.EOF)
			if len(got) != len(want) {
				t.Fatalf("expected %v, got %v", want, got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("token %d: expected %s, got %s", i, want[i], got[i])
				}
			}
		})
	}
}

func TestLexerStringEscapes(t *testing.T) {
	toks := NewLexer(`"a\n\t\"b\\"`, "test.lum").Tokenize()
	if toks[0].Type != token.STRING {
		t.Fatalf("expected STRING, got %s", toks[0].Type)
	}
	if want := "a\n\t\"b\\"; toks[0].Literal != want {
		t.Errorf("expected %q, got %q", want, toks[0].Literal)
	}
}

func TestLexerPositions(t *testing.T) {
	toks := NewLexer("let x\n  = 10;", "test.lum").Tokenize()

	want := []struct{ line, col, endCol int }{
		{1, 1, 4}, // let
		{1, 5, 6}, // x
		{2, 3, 4}, // =
		{2, 5, 7}, // 10
		{2, 7, 8}, // ;
	}
	for i, w := range want {
		tok := toks[i]
		if tok.Pos.Line != w.line || tok.Pos.Col != w.col || tok.EndPos.Col != w.endCol {
			t.Errorf("token %d (%s): expected %d:%d-%d, got %d:%d-%d",
				i, tok.Literal, w.line, w.col, w.endCol, tok.Pos.Line, tok.Pos.Col, tok.EndPos.Col)
		}
		if tok.Pos.File != "test.lum" {
			t.Errorf("token %d: expected file test.lum, got %q", i, tok.Pos.File)
		}
	}
}

func TestLexerErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"unterminated string", `"abc`, "unterminated string literal"},
		{"string across lines", "\"abc\ndef\"", "unterminated string literal"},
		{"bad escape", `"a\qb"`, `invalid escape sequence '\q'`},
		{"unterminated comment", "/* never closed", "unterminated block comment"},
		{"unknown character", "#", "unexpected character '#'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := NewLexer(tt.src, "test.lum").Tokenize()[0]
			if tok.Type != token.ILLEGAL {
				t.Fatalf("expected ILLEGAL, got %s", tok.Type)
			}
			if tok.Literal != tt.want {
				t.Errorf("expected %q, got %q", tt.want, tok.Literal)
			}
		})
	}
}

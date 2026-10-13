package token

// TokenType represents the type of a token.
type TokenType int

// Token categories and specific token types.
const (
	// Special tokens
	ILLEGAL TokenType = iota
	EOF
	COMMENT

	// Literals - start
	literal_beg
	IDENT  // main, x, foo
	INT    // 123, 42
	FLOAT  // 1.0, 3.14
	STRING // "hello"
	literal_end

	// Keywords - start
	keyword_beg
	// Control flow
	IF
	ELSE
	MATCH
	CASE
	WHILE
	FOR
	IN
	BREAK
	CONTINUE
	RETURN

	// Declarations
	FN
	LET
	CONST
	STRUCT
	ENUM
	TRAIT
	IMPL

	// Import
	IMPORT
	AS
	PUB

	// Literals (keywords)
	TRUE
	FALSE
	NULL

	// Types (built-in)
	INT_TYPE    // Int
	FLOAT_TYPE  // Float
	BOOL_TYPE   // Bool
	STRING_TYPE // String
	ANY_TYPE    // Any
	VOID_TYPE   // Void
	SELF_TYPE   // Self
	keyword_end

	// Operators - start
	operator_beg
	// Arithmetic
	PLUS    // +
	MINUS   // -
	STAR    // *
	SLASH   // /
	PERCENT // %

	// Comparison
	EQ  // ==
	NEQ // !=
	LT  // <
	LTE // <=
	GT  // >
	GTE // >=

	// Logical
	AND // &&
	OR  // ||
	NOT // !

	// Null coalescing
	QUESTION          // ?
	QUESTION_QUESTION // ??

	// Assignment
	ASSIGN         // =
	PLUS_ASSIGN    // +=
	MINUS_ASSIGN   // -=
	STAR_ASSIGN    // *=
	SLASH_ASSIGN   // /=
	PERCENT_ASSIGN // %=
	INC            // ++
	DEC            // --

	// Bitwise (reserved for future)
	AMPERSAND // &
	PIPE      // |
	operator_end

	// Delimiters - start
	delimiter_beg
	LPAREN   // (
	RPAREN   // )
	LBRACE   // {
	RBRACE   // }
	LBRACKET // [
	RBRACKET // ]

	COMMA     // ,
	SEMICOLON // ;
	COLON     // :
	DOT       // .
	ARROW     // ->
	ELLIPSIS  // ...
	delimiter_end
)

// Token represents a lexical token with its type, literal value, and position.
type Token struct {
	Type    TokenType // The token type
	Literal string    // The literal text of the token
	Pos     Position  // Starting position in source
	EndPos  Position  // Ending position in source (exclusive)
}

// NewToken creates a new token with the given type, literal, and positions.
func NewToken(typ TokenType, lit string, pos, endPos Position) Token {
	return Token{
		Type:    typ,
		Literal: lit,
		Pos:     pos,
		EndPos:  endPos,
	}
}

// IsLiteral returns true if the token is a literal value.
func (t Token) IsLiteral() bool {
	return t.Type > literal_beg && t.Type < literal_end
}

// IsKeyword returns true if the token is a keyword.
func (t Token) IsKeyword() bool {
	return t.Type > keyword_beg && t.Type < keyword_end
}

// IsOperator returns true if the token is an operator.
func (t Token) IsOperator() bool {
	return t.Type > operator_beg && t.Type < operator_end
}

// IsDelimiter returns true if the token is a delimiter.
func (t Token) IsDelimiter() bool {
	return t.Type > delimiter_beg && t.Type < delimiter_end
}

// String returns a string representation of the token.
func (t Token) String() string {
	if t.Literal != "" {
		return t.Type.String() + "(" + t.Literal + ")"
	}
	return t.Type.String()
}

// String returns a string representation of the token type.
func (typ TokenType) String() string {
	switch typ {
	case ILLEGAL:
		return "ILLEGAL"
	case EOF:
		return "EOF"
	case COMMENT:
		return "COMMENT"

	// Literals
	case IDENT:
		return "IDENT"
	case INT:
		return "INT"
	case FLOAT:
		return "FLOAT"
	case STRING:
		return "STRING"

	// Keywords - Control flow
	case IF:
		return "IF"
	case ELSE:
		return "ELSE"
	case MATCH:
		return "MATCH"
	case CASE:
		return "CASE"
	case WHILE:
		return "WHILE"
	case FOR:
		return "FOR"
	case IN:
		return "IN"
	case BREAK:
		return "BREAK"
	case CONTINUE:
		return "CONTINUE"
	case RETURN:
		return "RETURN"

	// Keywords - Declarations
	case FN:
		return "FN"
	case LET:
		return "LET"
	case CONST:
		return "CONST"
	case STRUCT:
		return "STRUCT"
	case ENUM:
		return "ENUM"
	case TRAIT:
		return "TRAIT"
	case IMPL:
		return "IMPL"

	// Keywords - Import
	case IMPORT:
		return "IMPORT"
	case AS:
		return "AS"
	case PUB:
		return "PUB"

	// Keywords - Literals
	case TRUE:
		return "TRUE"
	case FALSE:
		return "FALSE"
	case NULL:
		return "NULL"

	// Keywords - Types
	case INT_TYPE:
		return "INT_TYPE"
	case FLOAT_TYPE:
		return "FLOAT_TYPE"
	case BOOL_TYPE:
		return "BOOL_TYPE"
	case STRING_TYPE:
		return "STRING_TYPE"
	case ANY_TYPE:
		return "ANY_TYPE"
	case VOID_TYPE:
		return "VOID_TYPE"
	case SELF_TYPE:
		return "SELF_TYPE"

	// Operators - Arithmetic
	case PLUS:
		return "PLUS"
	case MINUS:
		return "MINUS"
	case STAR:
		return "STAR"
	case SLASH:
		return "SLASH"
	case PERCENT:
		return "PERCENT"

	// Operators - Comparison
	case EQ:
		return "EQ"
	case NEQ:
		return "NEQ"
	case LT:
		return "LT"
	case LTE:
		return "LTE"
	case GT:
		return "GT"
	case GTE:
		return "GTE"

	// Operators - Logical
	case AND:
		return "AND"
	case OR:
		return "OR"
	case NOT:
		return "NOT"

	// Operators - Null coalescing
	case QUESTION:
		return "QUESTION"
	case QUESTION_QUESTION:
		return "QUESTION_QUESTION"

	// Operators - Assignment
	case ASSIGN:
		return "ASSIGN"
	case PLUS_ASSIGN:
		return "PLUS_ASSIGN"
	case MINUS_ASSIGN:
		return "MINUS_ASSIGN"
	case STAR_ASSIGN:
		return "STAR_ASSIGN"
	case SLASH_ASSIGN:
		return "SLASH_ASSIGN"
	case PERCENT_ASSIGN:
		return "PERCENT_ASSIGN"
	case INC:
		return "INC"
	case DEC:
		return "DEC"

	// Operators - Bitwise
	case AMPERSAND:
		return "AMPERSAND"
	case PIPE:
		return "PIPE"

	// Delimiters
	case LPAREN:
		return "LPAREN"
	case RPAREN:
		return "RPAREN"
	case LBRACE:
		return "LBRACE"
	case RBRACE:
		return "RBRACE"
	case LBRACKET:
		return "LBRACKET"
	case RBRACKET:
		return "RBRACKET"
	case COMMA:
		return "COMMA"
	case SEMICOLON:
		return "SEMICOLON"
	case COLON:
		return "COLON"
	case DOT:
		return "DOT"
	case ARROW:
		return "ARROW"
	case ELLIPSIS:
		return "ELLIPSIS"

	default:
		return "UNKNOWN"
	}
}

// keywords maps keyword strings to their token types.
var keywords = map[string]TokenType{
	// Control flow
	"if":       IF,
	"else":     ELSE,
	"match":    MATCH,
	"case":     CASE,
	"while":    WHILE,
	"for":      FOR,
	"in":       IN,
	"break":    BREAK,
	"continue": CONTINUE,
	"return":   RETURN,

	// Declarations
	"fn":     FN,
	"let":    LET,
	"const":  CONST,
	"struct": STRUCT,
	"enum":   ENUM,
	"trait":  TRAIT,
	"impl":   IMPL,

	// Import
	"import": IMPORT,
	"as":     AS,
	"pub":    PUB,

	// Literals
	"true":  TRUE,
	"false": FALSE,
	"null":  NULL,

	// Types
	"Int":    INT_TYPE,
	"Float":  FLOAT_TYPE,
	"Bool":   BOOL_TYPE,
	"String": STRING_TYPE,
	"Any":    ANY_TYPE,
	"Void":   VOID_TYPE,
	"Self":   SELF_TYPE,
}

// LookupIdent checks if the identifier is a keyword and returns the appropriate token type.
func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}

// Precedence returns the operator precedence for the token type.
// Higher number means higher precedence.
func (typ TokenType) Precedence() int {
	switch typ {
	case OR:
		return 1
	case AND:
		return 2
	case QUESTION_QUESTION:
		return 3
	case EQ, NEQ:
		return 4
	case LT, LTE, GT, GTE:
		return 5
	case PLUS, MINUS:
		return 6
	case STAR, SLASH, PERCENT:
		return 7
	case PIPE:
		return 8
	case AMPERSAND:
		return 9
	}
	return 0
}

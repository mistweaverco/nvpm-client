package treesitterquery

import "fmt"

// Lex tokenizes a Tree-sitter query file without dropping or rewriting text.
func Lex(src []byte) ([]Token, error) {
	l := lexer{src: src, line: 1, col: 1}
	var tokens []Token
	for l.pos < len(l.src) {
		tok, err := l.nextToken()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
	}
	return tokens, nil
}

type lexer struct {
	src  []byte
	pos  int
	line int
	col  int
}

func (l *lexer) nextToken() (Token, error) {
	start, line, col := l.pos, l.line, l.col
	c := l.src[l.pos]
	switch {
	case c == '(':
		l.advance()
		return l.token(TokenLParen, start, line, col), nil
	case c == ')':
		l.advance()
		return l.token(TokenRParen, start, line, col), nil
	case c == '"':
		if err := l.scanString(); err != nil {
			return Token{}, err
		}
		return l.token(TokenString, start, line, col), nil
	case c == ';':
		l.scanComment()
		return l.token(TokenComment, start, line, col), nil
	case isQueryWhitespace(c):
		l.scanWhitespace()
		return l.token(TokenWhitespace, start, line, col), nil
	default:
		l.scanSymbol()
		return l.token(TokenSymbol, start, line, col), nil
	}
}

func (l *lexer) token(kind TokenKind, start, line, col int) Token {
	return Token{
		Kind:   kind,
		Raw:    string(l.src[start:l.pos]),
		Start:  start,
		End:    l.pos,
		Line:   line,
		Column: col,
	}
}

func (l *lexer) advance() {
	c := l.src[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
		return
	}
	l.col++
}

func (l *lexer) scanWhitespace() {
	for l.pos < len(l.src) && isQueryWhitespace(l.src[l.pos]) {
		l.advance()
	}
}

func (l *lexer) scanComment() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		l.advance()
		if c == '\n' {
			return
		}
	}
}

func (l *lexer) scanString() error {
	l.advance() // opening quote
	escaped := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		l.advance()
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			return nil
		}
	}
	return fmt.Errorf("unterminated query string at %d:%d", l.line, l.col)
}

func (l *lexer) scanSymbol() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '(' || c == ')' || c == '"' || c == ';' || isQueryWhitespace(c) {
			return
		}
		l.advance()
	}
}

func isQueryWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

package treesitterquery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func tokenKinds(tokens []Token) []TokenKind {
	out := make([]TokenKind, len(tokens))
	for i, t := range tokens {
		out[i] = t.Kind
	}
	return out
}

func TestLex_BasicAndTrivia(t *testing.T) {
	src := `(identifier) @foo`
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	require.Equal(t, src, reconstruct(tokens))
	require.Equal(t, []TokenKind{
		TokenLParen, TokenSymbol, TokenRParen, TokenWhitespace, TokenSymbol,
	}, tokenKinds(tokens))
	require.Equal(t, "identifier", tokens[1].Raw)
	require.Equal(t, 1, tokens[1].Line)
	require.Equal(t, 2, tokens[1].Column)
	require.Equal(t, 1, tokens[1].Start)
	require.Equal(t, 11, tokens[1].End)
}

func TestLex_PredicateAndStringEscapes(t *testing.T) {
	src := `(#match? @foo "bar")`
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	require.Equal(t, src, reconstruct(tokens))
	require.Equal(t, TokenSymbol, tokens[1].Kind)
	require.Equal(t, "#match?", tokens[1].Raw)
	require.Equal(t, TokenString, tokens[5].Kind)
	require.Equal(t, `"bar"`, tokens[5].Raw)
}

func TestLex_CommentIsNotPredicate(t *testing.T) {
	src := `; (#match? @foo "this is a comment")` + "\n"
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, TokenComment, tokens[0].Kind)
	require.Equal(t, src, tokens[0].Raw)
}

func TestLex_SemicolonInsideString(t *testing.T) {
	src := `(#match? @foo "text ; not comment")`
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	require.Equal(t, src, reconstruct(tokens))
	var str Token
	for _, tok := range tokens {
		if tok.Kind == TokenString {
			str = tok
		}
	}
	require.Equal(t, `"text ; not comment"`, str.Raw)
}

func TestLex_EscapedParenAndQuoteInString(t *testing.T) {
	src := `(#match? @foo "\\(")` + "\n" + `(#match? @foo "\"")`
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	require.Equal(t, src, reconstruct(tokens))
	var strings []string
	for _, tok := range tokens {
		if tok.Kind == TokenString {
			strings = append(strings, tok.Raw)
		}
	}
	require.Equal(t, []string{`"\\("`, `"\""`}, strings)
}

func TestLex_Locations(t *testing.T) {
	src := "a\n  b"
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	require.Equal(t, TokenSymbol, tokens[0].Kind)
	require.Equal(t, 1, tokens[0].Line)
	require.Equal(t, 1, tokens[0].Column)
	require.Equal(t, TokenSymbol, tokens[2].Kind)
	require.Equal(t, 2, tokens[2].Line)
	require.Equal(t, 3, tokens[2].Column)
}

func reconstruct(tokens []Token) string {
	out := ""
	for _, tok := range tokens {
		out += tok.Raw
	}
	return out
}

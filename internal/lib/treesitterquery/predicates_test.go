package treesitterquery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverExpressions_NestedMatch(t *testing.T) {
	src := "((identifier) @foo\n (#match? @foo \"abc\"))"
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	exprs := discoverExpressions(tokens)
	var heads []string
	for _, e := range exprs {
		heads = append(heads, e.Head)
	}
	require.Contains(t, heads, "#match?")
	var match Expression
	for _, e := range exprs {
		if e.Head == "#match?" {
			match = e
		}
	}
	require.Equal(t, "#match?", match.Head)
	require.Len(t, match.Args, 2)
	require.Equal(t, "@foo", match.Args[0].Raw)
	require.Equal(t, `"abc"`, match.Args[1].Raw)
}

func TestDiscoverExpressions_IgnoresComment(t *testing.T) {
	src := "; (#match? @foo \"abc\")\n(identifier) @foo"
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	exprs := discoverExpressions(tokens)
	for _, e := range exprs {
		require.NotEqual(t, "#match?", e.Head)
	}
}

func TestDiscoverExpressions_IgnoresString(t *testing.T) {
	src := `(#eq? @foo "#match?")`
	tokens, err := Lex([]byte(src))
	require.NoError(t, err)
	exprs := discoverExpressions(tokens)
	require.Len(t, exprs, 1)
	require.Equal(t, "#eq?", exprs[0].Head)
	require.Equal(t, `"#match?"`, exprs[0].Args[1].Raw)
}

func TestPatch_PreservesIsNotLocal(t *testing.T) {
	src := "((identifier) @variable\n (#is-not? local))\n"
	res, err := Patch([]byte(src), PatchOptions{SourceDialect: DialectTreeSitter})
	require.NoError(t, err)
	require.Equal(t, src, string(res.Content))
	require.Empty(t, res.Applied)
	require.NotEmpty(t, res.Diagnostics)
	require.Equal(t, RulePropertyIsNot, res.Diagnostics[0].Rule)
	require.Contains(t, res.Diagnostics[0].Message, "#is-not?")
}

func TestPatch_IsPredicateDiagnostic(t *testing.T) {
	src := `(#is? local)`
	res, err := Patch([]byte(src), PatchOptions{SourceDialect: DialectTreeSitter})
	require.NoError(t, err)
	require.Equal(t, src, string(res.Content))
	require.Equal(t, RulePropertyIs, res.Diagnostics[0].Rule)
}

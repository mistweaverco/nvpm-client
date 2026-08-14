package treesitterquery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatch_InheritsModeline(t *testing.T) {
	src := "(identifier) @variable\n"
	res, err := Patch([]byte(src), PatchOptions{Inherits: []string{"javascript"}})
	require.NoError(t, err)
	require.Equal(t, "; inherits: javascript\n(identifier) @variable\n", string(res.Content))
	require.Equal(t, RuleInheritsModeline, res.Applied[0].Rule)
}

func TestPatch_InheritsDedupes(t *testing.T) {
	src := "(identifier) @variable\n"
	res, err := Patch([]byte(src), PatchOptions{
		Inherits: []string{"javascript", "JavaScript", "", "typescript"},
	})
	require.NoError(t, err)
	require.Equal(t, "; inherits: javascript, typescript\n(identifier) @variable\n", string(res.Content))
}

func TestPatch_InheritsSkipsExisting(t *testing.T) {
	src := ";; inherits: javascript\n(identifier) @variable\n"
	res, err := Patch([]byte(src), PatchOptions{Inherits: []string{"typescript"}})
	require.NoError(t, err)
	require.Equal(t, src, string(res.Content))
}

func TestPatch_InheritsEmptyDoesNothing(t *testing.T) {
	src := "(identifier) @variable\n"
	res, err := Patch([]byte(src), PatchOptions{Inherits: []string{"  ", ""}})
	require.NoError(t, err)
	require.Equal(t, src, string(res.Content))
}

func TestQueryKindFromFilename(t *testing.T) {
	require.Equal(t, QueryKindHighlights, QueryKindFromFilename("queries/highlights.scm"))
	require.Equal(t, QueryKindInjections, QueryKindFromFilename("INJECTIONS.SCM"))
	require.Equal(t, QueryKindUnknown, QueryKindFromFilename("custom.scm"))
	require.Equal(t, QueryKindUnknown, QueryKindFromFilename("readme.md"))
}

package treesitterquery

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatch_RegexOnlyReplacesStringToken(t *testing.T) {
	src := "((identifier) @foo\n (#match? @foo \"abc+\"))"
	res, err := Patch([]byte(src), PatchOptions{SourceDialect: DialectTreeSitter})
	require.NoError(t, err)
	require.Contains(t, string(res.Content), `(#match? @foo "\\vabc+")`)
	require.True(t, strings.HasPrefix(string(res.Content), "((identifier) @foo\n"))
	require.NotContains(t, string(res.Content), `"abc+"`)
}

func TestPatch_MultilinePredicateFormatting(t *testing.T) {
	src := "(#match?\n    @foo\n    \"abc+\")"
	res, err := Patch([]byte(src), PatchOptions{SourceDialect: DialectTreeSitter})
	require.NoError(t, err)
	require.Equal(t, "(#match?\n    @foo\n    \"\\\\vabc+\")", string(res.Content))
}

func TestPatch_NeovimNativeSkipsRegex(t *testing.T) {
	src := `(#match? @foo "\\vfoo\\s+bar")`
	res, err := Patch([]byte(src), PatchOptions{SourceDialect: DialectNeovim})
	require.NoError(t, err)
	require.Equal(t, src, string(res.Content))
	require.Empty(t, res.Applied)
}

func TestPatch_Idempotent(t *testing.T) {
	cases := []struct {
		name string
		src  string
		opts PatchOptions
	}{
		{"inherits", "(identifier) @x\n", PatchOptions{Inherits: []string{"javascript"}}},
		{"translated regex", `(#match? @foo "abc+")`, PatchOptions{SourceDialect: DialectTreeSitter}},
		{"already v", `(#match? @foo "\\vfoo+")`, PatchOptions{SourceDialect: DialectTreeSitter}},
		{"neovim native", `(#match? @foo "\\vfoo\\s+bar")`, PatchOptions{SourceDialect: DialectNeovim}},
		{"is-not", "((identifier) @variable\n (#is-not? local))\n", PatchOptions{SourceDialect: DialectTreeSitter}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			first, err := Patch([]byte(tt.src), tt.opts)
			require.NoError(t, err)
			second, err := Patch(first.Content, tt.opts)
			require.NoError(t, err)
			require.Equal(t, first.Content, second.Content)
		})
	}
}

func TestPatch_Goldens(t *testing.T) {
	dir := filepath.Join("testdata")
	inputs, err := filepath.Glob(filepath.Join(dir, "*.input.scm"))
	require.NoError(t, err)
	require.NotEmpty(t, inputs)
	for _, inPath := range inputs {
		t.Run(filepath.Base(inPath), func(t *testing.T) {
			in, err := os.ReadFile(inPath)
			require.NoError(t, err)
			wantPath := strings.Replace(inPath, ".input.scm", ".expected.scm", 1)
			want, err := os.ReadFile(wantPath)
			require.NoError(t, err)
			opts := PatchOptions{SourceDialect: DialectTreeSitter}
			if strings.Contains(filepath.Base(inPath), "inherits") {
				opts.Inherits = []string{"javascript"}
			}
			got, err := Patch(in, opts)
			require.NoError(t, err)
			require.Equal(t, string(want), string(got.Content))
			again, err := Patch(got.Content, opts)
			require.NoError(t, err)
			require.True(t, bytes.Equal(got.Content, again.Content))
		})
	}
}

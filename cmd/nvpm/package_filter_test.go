package nvpm

import (
	"os"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
)

type emptyRegistryFileReader struct{}

func (emptyRegistryFileReader) ReadFile(string) ([]byte, error) {
	return nil, os.ErrNotExist
}

func TestMatchShowJSONExactAndGlob(t *testing.T) {
	obj := map[string]any{
		"name":       "Mistweaver",
		"package_id": "github:mistweaverco/kulala.nvim",
		"categories": []any{"Plugin", "HTTP"},
		"provider":   "github",
		"status":     "installed",
	}

	assert.True(t, MatchShowJSON(obj, []string{"categories:Plugin"}))
	assert.True(t, MatchShowJSON(obj, []string{".categories:Plugin"}))
	assert.False(t, MatchShowJSON(obj, []string{"categories:*tree*"}))
	assert.True(t, MatchShowJSON(obj, []string{"categories:*plug*"}))
	assert.True(t, MatchShowJSON(obj, []string{"package_id:*mistweaverco/*"}))
	assert.True(t, MatchShowJSON(obj, []string{"package_id:github:mistweaverco*"}))
	assert.True(t, MatchShowJSON(obj, []string{"package_id:github:mistweaverco/kulala.nvim"}))
	assert.False(t, MatchShowJSON(obj, []string{"package_id:github:other*"}))
	assert.True(t, MatchShowJSON(obj, []string{"name:mistweaver"})) // exact after lower
	assert.False(t, MatchShowJSON(obj, []string{"name:mist"}))      // no glob → exact only
	assert.True(t, MatchShowJSON(obj, []string{"name:Mist*"}))
}

func TestParseShowFilterKeepsColonsInValue(t *testing.T) {
	pathStr, value, err := parseShowFilter("package_id:github:mistweaverco*")
	assert.NoError(t, err)
	assert.Equal(t, "package_id", pathStr)
	assert.Equal(t, "github:mistweaverco*", value)
}

func TestMatchShowJSONANDAndMissing(t *testing.T) {
	obj := map[string]any{
		"provider":     "npm",
		"package_id":   "npm:eslint",
		"always_trust": true,
	}
	assert.True(t, MatchShowJSON(obj, []string{"provider:npm", "always_trust:true"}))
	assert.False(t, MatchShowJSON(obj, []string{"provider:npm", "always_trust:false"}))
	assert.False(t, MatchShowJSON(obj, []string{"missing.path:x"}))
}

func TestMatchShowJSONNestedArray(t *testing.T) {
	obj := map[string]any{
		"git_refs": []any{
			map[string]any{"ref": "main", "age": "1 day ago"},
			map[string]any{"ref": "v1.0.0", "age": "120 days ago"},
		},
	}
	assert.True(t, MatchShowJSON(obj, []string{"git_refs.age:1 day ago"}))
	assert.True(t, MatchShowJSON(obj, []string{"git_refs.ref:v1.0.0"}))
	assert.False(t, MatchShowJSON(obj, []string{"git_refs.ref:develop"}))
}

func TestMatchShowJSONTypedSlicesFromBuildPackageInfoJSON(t *testing.T) {
	// buildPackageInfoJSON / mergeGitDetailsJSON use concrete slice types, not []any.
	obj := map[string]any{
		"categories": []string{"Plugin", "Completion"},
		"git_refs": []map[string]string{
			{"ref": "main", "commit": "cba53ef", "age": "3 days ago", "kind": "branch"},
			{"ref": "v1.10.2", "commit": "78336bc", "age": "124 days ago", "kind": "tag"},
		},
		"discovery": map[string]string{
			"remote": "main (cba53ef) committed 3 days ago on remote",
			"local":  "first recorded locally 2 days ago",
		},
	}
	assert.True(t, MatchShowJSON(obj, []string{"git_refs.age:3 days ago"}))
	assert.True(t, MatchShowJSON(obj, []string{"git_refs.ref:main"}))
	assert.True(t, MatchShowJSON(obj, []string{"categories:Plugin"}))
	assert.True(t, MatchShowJSON(obj, []string{"categories:*complet*"}))
	assert.True(t, MatchShowJSON(obj, []string{"discovery.remote:*3 days ago*"}))
	assert.False(t, MatchShowJSON(obj, []string{"git_refs.age:1 day ago"}))
}

func TestFilterSourceIDsByShowFiltersSynthesized(t *testing.T) {
	// Empty registry → packageMatchesShowFilters synthesizes provider/package_id.
	prevRegistry := newRegistryParser
	newRegistryParser = func() *registry_parser.RegistryParser {
		return registry_parser.NewRegistryParser(emptyRegistryFileReader{})
	}
	defer func() { newRegistryParser = prevRegistry }()

	ids := filterSourceIDsByShowFilters(
		[]string{"npm:eslint", "pypi:black"},
		[]string{"provider:npm"},
	)
	assert.Equal(t, []string{"npm:eslint"}, ids)
}

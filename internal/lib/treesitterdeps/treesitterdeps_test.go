package treesitterdeps

import (
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/require"
)

type stubReg struct {
	items []registry_parser.RegistryItem
}

func (s stubReg) GetBySourceId(id string) registry_parser.RegistryItem {
	for _, it := range s.items {
		if it.Source.ID == id {
			return it
		}
	}
	return registry_parser.RegistryItem{}
}

func (s stubReg) GetData(bool) registry_parser.RegistryRoot {
	return s.items
}

func TestResolveExternalQueryRepoURL_FromPackage(t *testing.T) {
	reg := stubReg{items: []registry_parser.RegistryItem{
		{Source: registry_parser.RegistryItemSource{ID: "github:neovim-treesitter/nvim-treesitter-queries-typescript"}}}}
	u, err := ResolveExternalQueryRepoURL(registry_parser.RegistryItemTreeSitterExternalQueries{
		Package: "github:neovim-treesitter/nvim-treesitter-queries-typescript",
	}, reg)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/neovim-treesitter/nvim-treesitter-queries-typescript", u)
}

func TestTopoInstallOrder_Cycle(t *testing.T) {
	edges := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	_, err := TopoInstallOrder([]string{"a"}, edges)
	require.Error(t, err)
}

func TestTopoInstallOrder_Chain(t *testing.T) {
	edges := map[string][]string{
		"html_tags": {"svelte"},
	}
	order, err := TopoInstallOrder([]string{"svelte"}, edges)
	require.NoError(t, err)
	require.Equal(t, []string{"html_tags", "svelte"}, order)
}

func TestBuildParserRequireEdges(t *testing.T) {
	root := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{ID: "github:demo/svelte"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "svelte", GrammarDir: ".", Integrations: []string{"neovim"}, Requires: []string{"html_tags"}},
			},
		},
	}
	html := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:demo/html"},
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "html_tags", GrammarDir: ".", Integrations: []string{"neovim"}},
			},
		},
	}
	reg := stubReg{items: []registry_parser.RegistryItem{root, html}}
	edges, err := BuildParserRequireEdges(root, reg, func(lang string) (string, error) {
		if lang == "html_tags" {
			return "github:demo/html", nil
		}
		return "", nil
	})
	require.NoError(t, err)
	require.Contains(t, edges["html_tags"], "svelte")
}

func TestQueryPackageCandidates(t *testing.T) {
	a := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:a/nvim-q-ts"},
		Categories: []string{"Tree-sitter-queries"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "typescript", QueriesOnly: true, Integrations: []string{"neovim"}},
			},
		},
	}
	b := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:b/nvim-q-ts"},
		Categories: []string{"Tree-sitter-queries"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "typescript", QueriesOnly: true, Integrations: []string{"neovim"}},
			},
		},
	}
	reg := stubReg{items: []registry_parser.RegistryItem{a, b}}
	got := QueryPackageCandidates(reg, "typescript", "neovim")
	require.Len(t, got, 2)
}

func TestLanguageNeedsNeovimParser(t *testing.T) {
	js := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:demo/js"},
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "ecma", QueriesOnly: true, Integrations: []string{"neovim"}},
				{Language: "javascript", GrammarDir: ".", Integrations: []string{"neovim"}},
			},
		},
	}
	reg := stubReg{items: []registry_parser.RegistryItem{js}}
	require.True(t, LanguageNeedsNeovimParser(reg, "javascript"))
	require.False(t, LanguageNeedsNeovimParser(reg, "ecma"))
}

func TestBuildInjectionParserRequireEdges(t *testing.T) {
	root := registry_parser.RegistryItem{
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{
					Language:     "svelte",
					Integrations: []string{"neovim"},
					Injections:   []string{"css", "javascript"},
				},
			},
		},
	}
	css := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:demo/css"},
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "css", GrammarDir: ".", Integrations: []string{"neovim"}},
			},
		},
	}
	js := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:demo/js"},
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "javascript", GrammarDir: ".", Integrations: []string{"neovim"}, Requires: []string{"ecma"}},
			},
		},
	}
	ecma := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:demo/ecma"},
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "ecma", GrammarDir: ".", Integrations: []string{"neovim"}},
			},
		},
	}
	reg := stubReg{items: []registry_parser.RegistryItem{root, css, js, ecma}}
	resolve := func(lang string) (string, error) {
		switch lang {
		case "css":
			return "github:demo/css", nil
		case "javascript":
			return "github:demo/js", nil
		case "ecma":
			return "github:demo/ecma", nil
		default:
			return "", nil
		}
	}
	edges, langs, err := BuildInjectionParserRequireEdges(root, reg, "neovim", resolve)
	require.NoError(t, err)
	require.Equal(t, []string{"css", "javascript"}, langs)
	require.Contains(t, edges["ecma"], "javascript")
}

func TestMergeInjectionLanguagesForEditor(t *testing.T) {
	root := registry_parser.RegistryItem{
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "svelte", Integrations: []string{"neovim"}, Injections: []string{"css", "javascript"}},
				{Language: "foo", Integrations: []string{"vscode"}, Injections: []string{"lua"}},
			},
		},
	}
	got := MergeInjectionLanguagesForEditor(root, "neovim")
	require.Equal(t, []string{"css", "javascript"}, got)
}

package nvpm

import (
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

func TestCollectPackageExtraDetails_TreeSitterAndRequires(t *testing.T) {
	prev := packageIsInstalled
	defer func() { packageIsInstalled = prev }()
	packageIsInstalled = func(id string) bool {
		return id == "npm:tree-sitter-cli"
	}

	item := registry_parser.RegistryItem{
		Name:       "tree-sitter-rust",
		Categories: []string{"Tree-sitter-parser"},
		Languages:  []string{"rust"},
		Requires: &registry_parser.RegistryItemRequires{
			One: []string{"npm:tree-sitter-cli", "github:tree-sitter/tree-sitter"},
		},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{
					Language:     "rust",
					GrammarDir:   ".",
					Integrations: []string{"neovim"},
					ExternalQueries: registry_parser.TreeSitterExternalQueriesList{
						{RepoURL: "https://github.com/neovim-treesitter/nvim-treesitter-queries-rust", Semver: true},
					},
				},
			},
		},
	}

	extra := collectPackageExtraDetails(item)
	if extra.Requires == nil || len(extra.Requires.One) != 2 {
		t.Fatalf("requires: %+v", extra.Requires)
	}
	if len(extra.Requires.OneSatisfied) != 1 || extra.Requires.OneSatisfied[0] != "npm:tree-sitter-cli" {
		t.Fatalf("one satisfied: %v", extra.Requires.OneSatisfied)
	}
	if extra.TreeSitter == nil || len(extra.TreeSitter.Languages) != 1 {
		t.Fatalf("treesitter: %+v", extra.TreeSitter)
	}
	if len(extra.TreeSitter.Build[0].ExternalQueries) != 1 {
		t.Fatal("expected external queries")
	}
}

func TestAppendTreeSitterPlain(t *testing.T) {
	var b strings.Builder
	appendTreeSitterPlain(&b, &packageTreeSitterDetails{
		Languages:    []string{"ruby"},
		Integrations: []string{"neovim"},
		Build: []packageTreeSitterBuildDetails{
			{
				Language:     "ruby",
				GrammarDir:   ".",
				Integrations: []string{"neovim"},
				ExternalQueries: []packageExternalQueryDetails{
					{RepoURL: "https://example.com/queries", Semver: true},
				},
			},
		},
	})
	out := b.String()
	if !strings.Contains(out, "Languages: ruby") {
		t.Fatalf("missing languages: %q", out)
	}
	if !strings.Contains(out, "External queries") {
		t.Fatalf("missing external queries: %q", out)
	}
}

func TestRequiresDetailsJSON(t *testing.T) {
	j := requiresDetailsJSON(&packageRequiresDetails{
		All:          []string{"npm:a"},
		One:          []string{"npm:b", "github:c/d"},
		OneSatisfied: []string{"npm:b"},
		AllMissing:   []string{"npm:a"},
		OneMissing:   false,
	})
	if j["all"] == nil || j["one"] == nil {
		t.Fatalf("json: %v", j)
	}
}

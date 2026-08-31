package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

func TestInferTreeSitterBuildFromRepo_TreeSitterJSONAndParserJSON(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "tree-sitter.json"), []byte(`{
		"grammars": [{"name": "zsh", "path": "."}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "parser.json"), []byte(`{
		"lang": "zsh",
		"queries_dir": "nvim-queries",
		"test_dir": "nvim-queries/tests"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	build := inferTreeSitterBuildFromRepo(repo, "github:georgeharker/tree-sitter-zsh")
	if len(build) != 1 {
		t.Fatalf("got %d build rows: %#v", len(build), build)
	}
	if build[0].Language != "zsh" {
		t.Fatalf("language: %q", build[0].Language)
	}
	if build[0].GrammarDir != "." {
		t.Fatalf("grammar_dir: %q", build[0].GrammarDir)
	}
	if build[0].QueriesDir != "nvim-queries" {
		t.Fatalf("queries_dir: %q", build[0].QueriesDir)
	}
}

func TestInferTreeSitterBuildFromRepo_SourceIDFallback(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "parser.c"), []byte("/* parser */"), 0o644); err != nil {
		t.Fatal(err)
	}
	build := inferTreeSitterBuildFromRepo(repo, "github:georgeharker/tree-sitter-zsh")
	if len(build) != 1 || build[0].Language != "zsh" || build[0].GrammarDir != "." {
		t.Fatalf("unexpected build: %#v", build)
	}
}

func TestLanguageFromTreeSitterSourceID(t *testing.T) {
	got := languageFromTreeSitterSourceID("github:georgeharker/tree-sitter-zsh")
	if got != "zsh" {
		t.Fatalf("got %q", got)
	}
	got = languageFromTreeSitterSourceID("github:tree-sitter/tree-sitter-typescript")
	if got != "typescript" {
		t.Fatalf("got %q", got)
	}
}

func TestWithInferredTreeSitter_RegistryWins(t *testing.T) {
	prev := append([]string(nil), requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prev })

	item := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:demo/ts"},
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "demo", GrammarDir: "gram", Integrations: []string{"neovim"}},
			},
		},
	}
	got := withInferredTreeSitter(item, t.TempDir(), "github:demo/ts")
	if got.TreeSitter.Build[0].GrammarDir != "gram" {
		t.Fatalf("registry metadata should win, got %#v", got.TreeSitter.Build)
	}
}

func TestWithInferredTreeSitter_RequiresNeovimIntegration(t *testing.T) {
	prev := append([]string(nil), requestedIntegrations...)
	SetRequestedIntegrations(nil)
	t.Cleanup(func() { requestedIntegrations = prev })

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "tree-sitter.json"), []byte(`{"grammars":[{"name":"zsh","path":"."}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := withInferredTreeSitter(registry_parser.RegistryItem{}, repo, "github:georgeharker/tree-sitter-zsh")
	if got.TreeSitter != nil {
		t.Fatalf("should not infer without --integrate neovim, got %#v", got.TreeSitter)
	}
}

func TestWithInferredTreeSitter_InfersUnknownGitHubGrammar(t *testing.T) {
	prev := append([]string(nil), requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prev })

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "tree-sitter.json"), []byte(`{"grammars":[{"name":"zsh","path":"."}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "parser.json"), []byte(`{"lang":"zsh","queries_dir":"nvim-queries"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := withInferredTreeSitter(registry_parser.RegistryItem{}, repo, "github:georgeharker/tree-sitter-zsh")
	if got.Source.ID != "github:georgeharker/tree-sitter-zsh" {
		t.Fatalf("source id: %q", got.Source.ID)
	}
	if !IsTreeSitterCategory(got.Categories) {
		t.Fatalf("categories: %#v", got.Categories)
	}
	if got.TreeSitter == nil || len(got.TreeSitter.Build) != 1 || got.TreeSitter.Build[0].QueriesDir != "nvim-queries" {
		t.Fatalf("inferred build: %#v", got.TreeSitter)
	}
}

package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

func TestNestedTreeSitterExternalQueryPreflight_AllowsWhenNestedAndNeovim(t *testing.T) {
	prevIntegrations := append([]string{}, requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prevIntegrations })
	t.Cleanup(func() { nestedTreeSitterDependencyInstallDepth = 0 })

	if nestedTreeSitterExternalQueryPreflight() != nil {
		t.Fatal("expected nil preflight at depth 0")
	}

	nestedTreeSitterDependencyInstallDepth = 1
	pre := nestedTreeSitterExternalQueryPreflight()
	if pre == nil || !pre.AllowUnpinned {
		t.Fatalf("nested neovim install should auto-allow external queries, got %#v", pre)
	}

	t.Cleanup(func() { _ = SetExternalTreeSitterQueriesPolicy("ask") })
	if err := SetExternalTreeSitterQueriesPolicy("never"); err != nil {
		t.Fatal(err)
	}
	pre = nestedTreeSitterExternalQueryPreflight()
	if pre == nil || pre.AllowUnpinned {
		t.Fatalf("policy never must still skip unpinned clones, got %#v", pre)
	}
}

func TestWithNestedTreeSitterDependencyInstall_RestoresGitHubDeferred(t *testing.T) {
	parent := &githubTreeSitterDeferredState{sourceID: "github:tree-sitter/tree-sitter-html", resolvedVersion: "v1"}
	githubTreeSitterDeferred = parent
	t.Cleanup(func() { githubTreeSitterDeferred = nil; nestedTreeSitterDependencyInstallDepth = 0 })

	withNestedTreeSitterDependencyInstall(func() {
		if nestedTreeSitterDependencyInstallDepth != 1 {
			t.Fatalf("depth: %d", nestedTreeSitterDependencyInstallDepth)
		}
		githubTreeSitterDeferred = &githubTreeSitterDeferredState{sourceID: "github:tree-sitter-grammars/tree-sitter-markdown"}
	})
	if githubTreeSitterDeferred != parent {
		t.Fatalf("parent phased state must be restored, got %#v", githubTreeSitterDeferred)
	}
	if nestedTreeSitterDependencyInstallDepth != 0 {
		t.Fatalf("depth after: %d", nestedTreeSitterDependencyInstallDepth)
	}
}

func TestCacheNeovimTreeSitterQueriesForBuiltLangs_NestedInstallSkipsConfirmAndRoutesQueries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	prevIntegrations := append([]string{}, requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prevIntegrations })
	t.Cleanup(func() { nestedTreeSitterDependencyInstallDepth = 0 })

	prevConfirm := externalTreeSitterQueriesConfirmHook
	externalTreeSitterQueriesConfirmHook = func(_, _ string) (bool, error) {
		t.Fatal("nested dependency install must not prompt for external queries")
		return false, nil
	}
	t.Cleanup(func() { externalTreeSitterQueriesConfirmHook = prevConfirm })

	prevClone := cloneExternalQueriesRepoForCache
	cloneExternalQueriesRepoForCache = func(repoURL, destDir, _ string, _ bool, _, _ string) (string, error) {
		switch {
		case strings.Contains(repoURL, "markdown_inline"):
			writeFakeExternalQueryClone(t, destDir, "markdown_inline", "(code_span) @markup.raw")
		default:
			writeFakeExternalQueryClone(t, destDir, "markdown", "(atx_heading) @markup.heading")
		}
		return "abc123def456", nil
	}
	t.Cleanup(func() { cloneExternalQueriesRepoForCache = prevClone })

	sourceID := "github:tree-sitter-grammars/tree-sitter-markdown"
	version := "v1"
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "tree-sitter-markdown", "queries"), "highlights.scm", "(grammar_local)")
	writeQueryFile(t, filepath.Join(repo, "tree-sitter-markdown-inline", "queries"), "highlights.scm", "(code_span)")

	build := []registry_parser.RegistryItemTreeSitterBuild{
		{
			Language:     "markdown",
			GrammarDir:   "tree-sitter-markdown",
			Integrations: []string{"neovim"},
			ExternalQueries: registry_parser.TreeSitterExternalQueriesList{
				{RepoURL: "https://github.com/neovim-treesitter/nvim-treesitter-queries-markdown"},
				{RepoURL: "https://github.com/neovim-treesitter/nvim-treesitter-queries-markdown_inline"},
			},
		},
		{
			Language:     "markdown_inline",
			GrammarDir:   "tree-sitter-markdown-inline",
			Integrations: []string{"neovim"},
			ExternalQueries: registry_parser.TreeSitterExternalQueriesList{
				{RepoURL: "https://github.com/neovim-treesitter/nvim-treesitter-queries-markdown_inline"},
			},
		},
	}

	nestedTreeSitterDependencyInstallDepth = 1
	pre := nestedTreeSitterExternalQueryPreflight()
	pins, err := cacheNeovimTreeSitterQueriesForBuiltLangs(repo, sourceID, version, build, []string{"markdown", "markdown_inline"}, pre)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pins) == 0 {
		t.Fatal("expected external query pins from registry metadata")
	}

	md, err := os.ReadFile(filepath.Join(neovimTreeSitterQueriesCacheDir(sourceID, version, "markdown"), "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "(atx_heading)") || strings.Contains(string(md), "code_span") {
		t.Fatalf("markdown override queries lost during nested install: %q", md)
	}
	inline, err := os.ReadFile(filepath.Join(neovimTreeSitterQueriesCacheDir(sourceID, version, "markdown_inline"), "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inline), "code_span") {
		t.Fatalf("markdown_inline queries missing: %q", inline)
	}
}

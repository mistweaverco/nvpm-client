package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/treesitterquery"
)

func writeQueryFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_PrefersGrammarLocal(t *testing.T) {
	repo := t.TempDir()
	gram := filepath.Join(repo, "g")
	writeQueryFile(t, filepath.Join(gram, "queries"), "highlights.scm", "(a)")
	writeQueryFile(t, filepath.Join(repo, "queries"), "highlights.scm", "(b)")
	got := resolveNeovimTreeSitterQueriesDir(repo, gram, neovimQueryResolveOpts{Language: "demo"})
	want := filepath.Join(gram, "queries")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_PrefersNvimQueriesLangOverQueries(t *testing.T) {
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "queries"), "highlights.scm", "(generic)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "zsh"), "highlights.scm", "(nvim)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "tests"), "highlights.scm", "(test)")
	got := resolveNeovimTreeSitterQueriesDir(repo, repo, neovimQueryResolveOpts{Language: "zsh"})
	want := filepath.Join(repo, "nvim-queries", "zsh")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_HonorsParserJSONQueriesDir(t *testing.T) {
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "queries"), "highlights.scm", "(generic)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "zsh"), "highlights.scm", "(nvim)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "tests"), "highlights.scm", "(test)")
	meta := `{"lang":"zsh","queries_dir":"nvim-queries","test_dir":"nvim-queries/tests"}`
	if err := os.WriteFile(filepath.Join(repo, "parser.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveNeovimTreeSitterQueriesDir(repo, repo, neovimQueryResolveOpts{Language: "zsh"})
	want := filepath.Join(repo, "nvim-queries", "zsh")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_HonorsRegistryQueriesDir(t *testing.T) {
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "queries"), "highlights.scm", "(generic)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "zsh"), "highlights.scm", "(nvim)")
	writeQueryFile(t, filepath.Join(repo, "custom-q", "zsh"), "highlights.scm", "(registry)")
	got := resolveNeovimTreeSitterQueriesDir(repo, repo, neovimQueryResolveOpts{
		Language:   "zsh",
		QueriesDir: "custom-q",
	})
	want := filepath.Join(repo, "custom-q", "zsh")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_HonorsRegistryQueriesPath(t *testing.T) {
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "queries"), "highlights.scm", "(generic)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "zsh"), "highlights.scm", "(nvim)")
	writeQueryFile(t, filepath.Join(repo, "flat-q"), "highlights.scm", "(path)")
	got := resolveNeovimTreeSitterQueriesDir(repo, repo, neovimQueryResolveOpts{
		Language:    "zsh",
		QueriesPath: "flat-q",
	})
	want := filepath.Join(repo, "flat-q")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_UnwrapsLanguageSubdir(t *testing.T) {
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "zsh"), "highlights.scm", "(nvim)")
	got := resolveNeovimTreeSitterQueriesDir(repo, repo, neovimQueryResolveOpts{
		Language:   "zsh",
		QueriesDir: "nvim-queries",
	})
	want := filepath.Join(repo, "nvim-queries", "zsh")
	if got != want {
		t.Fatalf("got %q want %q (must unwrap language subdir)", got, want)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_DoesNotTreatNvimQueriesTestsAsQueries(t *testing.T) {
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "tests"), "highlights.scm", "(test)")
	writeQueryFile(t, filepath.Join(repo, "queries"), "highlights.scm", "(generic)")
	got := resolveNeovimTreeSitterQueriesDir(repo, repo, neovimQueryResolveOpts{Language: "zsh"})
	want := filepath.Join(repo, "queries")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_ParserJSONIgnoresTestDir(t *testing.T) {
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "tests"), "highlights.scm", "(test)")
	meta := `{"lang":"zsh","queries_dir":"nvim-queries","test_dir":"nvim-queries/tests"}`
	if err := os.WriteFile(filepath.Join(repo, "parser.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveNeovimTreeSitterQueriesDir(repo, repo, neovimQueryResolveOpts{Language: "zsh"})
	if got != "" {
		t.Fatalf("expected no query source when only tests/ exist, got %q", got)
	}
}

func TestResolveNeovimTreeSitterQueriesDir_EmptyGrammarDirUsesRepoRoot(t *testing.T) {
	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "zsh"), "highlights.scm", "(nvim)")
	got := resolveNeovimTreeSitterQueriesDir(repo, repo, neovimQueryResolveOpts{Language: "zsh"})
	want := filepath.Join(repo, "nvim-queries", "zsh")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCopyNeovimTreeSitterQueriesDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "injections.scm"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyAndPatchNeovimTreeSitterQueriesDir(src, dst, neovimTreeSitterQueryCopyOptions{
		Language:      "demo",
		SourceDialect: treesitterquery.DialectTreeSitter,
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "nested", "injections.scm"))
	if err != nil || string(b) != "y" {
		t.Fatalf("nested copy: %v %q", err, b)
	}
}

func TestCopyNeovimTreeSitterQueriesDir_InheritsModelinePreservesIsNot(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte(`(#is-not? local)`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyAndPatchNeovimTreeSitterQueriesDir(src, dst, neovimTreeSitterQueryCopyOptions{
		Language:      "demo",
		Inherits:      []string{"javascript"},
		SourceDialect: treesitterquery.DialectTreeSitter,
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, "; inherits: javascript\n") {
		t.Fatalf("want inherits modeline first, got %q", got)
	}
	if !strings.Contains(got, "#is-not?") || strings.Contains(got, "#not-eq?") {
		t.Fatalf("must preserve #is-not?: %q", got)
	}
}

func TestCopyNeovimTreeSitterQueriesDir_SkipsModelineWhenPresent(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	orig := "; inherits: ecma\n(#is-not? local)\n"
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyAndPatchNeovimTreeSitterQueriesDir(src, dst, neovimTreeSitterQueryCopyOptions{
		Language:      "demo",
		Inherits:      []string{"javascript"},
		SourceDialect: treesitterquery.DialectTreeSitter,
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Count(got, "inherits:") != 1 {
		t.Fatalf("should not duplicate inherits modeline: %q", got)
	}
	if strings.Contains(got, "#not-eq?") || !strings.Contains(got, "#is-not?") {
		t.Fatalf("must preserve #is-not?: %q", got)
	}
}

func TestCacheNeovimTreeSitterQueriesForBuiltLangs_QueriesOnlyWithoutGrammarDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	prevIntegrations := append([]string{}, requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prevIntegrations })

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "queries", "highlights.scm"), []byte("(tag_name) @tag"), 0o644); err != nil {
		t.Fatal(err)
	}
	build := []registry_parser.RegistryItemTreeSitterBuild{
		{Language: "html_tags", QueriesOnly: true, Integrations: []string{"neovim"}},
	}

	_, err := cacheNeovimTreeSitterQueriesForBuiltLangs(repo, "github:demo/html", "v1", build, []string{"html_tags"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(neovimTreeSitterQueriesCacheDir("github:demo/html", "v1", "html_tags"), "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "(tag_name) @tag" {
		t.Fatalf("unexpected cached query: %q", b)
	}
}

func TestCacheNeovimTreeSitterQueriesForBuiltLangs_EmptyGrammarDirPrefersNvimQueries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	prevIntegrations := append([]string{}, requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prevIntegrations })

	repo := t.TempDir()
	writeQueryFile(t, filepath.Join(repo, "queries"), "highlights.scm", "(generic)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "zsh"), "highlights.scm", "(nvim)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "zsh"), "injections.scm", "(inj)")
	writeQueryFile(t, filepath.Join(repo, "nvim-queries", "tests"), "highlights.scm", "(test)")
	build := []registry_parser.RegistryItemTreeSitterBuild{
		{Language: "zsh", Integrations: []string{"neovim"}},
	}

	_, err := cacheNeovimTreeSitterQueriesForBuiltLangs(repo, "github:demo/zsh", "v1", build, []string{"zsh"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cached := neovimTreeSitterQueriesCacheDir("github:demo/zsh", "v1", "zsh")
	b, err := os.ReadFile(filepath.Join(cached, "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "(nvim)" {
		t.Fatalf("expected nvim-queries content, got %q", b)
	}
	if _, err := os.Stat(filepath.Join(cached, "zsh", "highlights.scm")); !os.IsNotExist(err) {
		t.Fatalf("language subdirectory must be unwrapped, stat nested zsh/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cached, "tests")); !os.IsNotExist(err) {
		t.Fatalf("tests/ must not be copied, stat: %v", err)
	}
}

func TestInstallNeovimParsersAndQueriesFromCache_AllowsQueriesOnlyMissingParser(t *testing.T) {
	home := t.TempDir()
	dataDir := filepath.Join(home, "nvim-data")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	prevIntegrations := append([]string{}, requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prevIntegrations })

	prevShellOut := neovimShellOutCapture
	neovimShellOutCapture = func(command string, args []string, dir string, env []string) (int, string, error) {
		for _, a := range args {
			if a == "--clean" {
				// html_tags has no bundled highlights in stock Neovim.
				return 0, "0", nil
			}
		}
		return 0, dataDir, nil
	}
	t.Cleanup(func() { neovimShellOutCapture = prevShellOut })

	cacheDir := neovimTreeSitterQueriesCacheDir("github:demo/html", "v1", "html_tags")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "highlights.scm"), []byte("(tag_name) @tag"), 0o644); err != nil {
		t.Fatal(err)
	}
	allowMissing := map[string]struct{}{"html_tags": {}}

	err := installNeovimParsersAndQueriesFromCache("github:demo/html", "v1", []string{"html_tags"}, allowMissing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "site", "queries", "html_tags", "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "(tag_name) @tag" {
		t.Fatalf("unexpected installed query: %q", b)
	}
}

func TestNeovimBundledQueriesPresent_InvalidLanguage(t *testing.T) {
	_, err := neovimBundledQueriesPresent("bad;lang")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstallNeovimParsersAndQueriesFromCache_SkipsQueriesWhenBundled(t *testing.T) {
	home := t.TempDir()
	dataDir := filepath.Join(home, "nvim-data")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	prevIntegrations := append([]string{}, requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prevIntegrations })

	prevShellOut := neovimShellOutCapture
	neovimShellOutCapture = func(command string, args []string, dir string, env []string) (int, string, error) {
		for _, a := range args {
			if a == "--clean" {
				return 0, "1", nil
			}
		}
		return 0, dataDir, nil
	}
	t.Cleanup(func() { neovimShellOutCapture = prevShellOut })

	artifact := TreeSitterArtifactPath("github:x/markdown", "v1", "markdown")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("fakeparser"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := neovimTreeSitterQueriesCacheDir("github:x/markdown", "v1", "markdown")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "highlights.scm"), []byte("(would_break_if_installed)"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate a prior install that shadowed Neovim's bundled markdown queries.
	staleQueries := filepath.Join(dataDir, "site", "queries", "markdown", "highlights.scm")
	if err := os.MkdirAll(filepath.Dir(staleQueries), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleQueries, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := installNeovimParsersAndQueriesFromCache("github:x/markdown", "v1", []string{"markdown"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = os.Stat(staleQueries)
	if !os.IsNotExist(err) {
		t.Fatalf("expected stale queries removed, stat err=%v", err)
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "site", "parser", "markdown"+SharedLibExt()))
	if err != nil || string(b) != "fakeparser" {
		t.Fatalf("parser install: %v %q", err, b)
	}
}

func TestExternalQuerySourceDialect(t *testing.T) {
	if got := externalQuerySourceDialect(registry_parser.RegistryItemTreeSitterExternalQueries{}); got != treesitterquery.DialectNeovim {
		t.Fatalf("empty dialect: got %q", got)
	}
	if got := externalQuerySourceDialect(registry_parser.RegistryItemTreeSitterExternalQueries{Dialect: "neovim"}); got != treesitterquery.DialectNeovim {
		t.Fatalf("neovim: got %q", got)
	}
	if got := externalQuerySourceDialect(registry_parser.RegistryItemTreeSitterExternalQueries{Dialect: "tree-sitter"}); got != treesitterquery.DialectTreeSitter {
		t.Fatalf("tree-sitter: got %q", got)
	}
}

func TestNeovimQuerySourceDialect(t *testing.T) {
	if got := neovimQuerySourceDialect("/repo/nvim-queries/zsh", neovimQueryResolveOpts{Language: "zsh"}); got != treesitterquery.DialectNeovim {
		t.Fatalf("nvim-queries path: got %q", got)
	}
	if got := neovimQuerySourceDialect("/repo/queries/zsh", neovimQueryResolveOpts{Language: "zsh"}); got != treesitterquery.DialectTreeSitter {
		t.Fatalf("generic queries path: got %q", got)
	}
	if got := neovimQuerySourceDialect("/repo/custom-q/zsh", neovimQueryResolveOpts{Language: "zsh", QueriesDir: "custom-q"}); got != treesitterquery.DialectNeovim {
		t.Fatalf("registry QueriesDir: got %q", got)
	}
}

func TestCopyAndPatch_NeovimDialectSkipsRegex(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	orig := `(#match? @foo "\\vfoo\\s+bar")`
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyAndPatchNeovimTreeSitterQueriesDir(src, dst, neovimTreeSitterQueryCopyOptions{
		Language:      "javascript",
		SourceDialect: treesitterquery.DialectNeovim,
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != orig {
		t.Fatalf("neovim dialect must skip regex rewrite: %q", b)
	}
}

func TestCopyAndPatchThenPlainInstallCopy(t *testing.T) {
	src := t.TempDir()
	cache := t.TempDir()
	site := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte(`(#match? @foo "abc+")`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyAndPatchNeovimTreeSitterQueriesDir(src, cache, neovimTreeSitterQueryCopyOptions{
		Language:      "javascript",
		SourceDialect: treesitterquery.DialectTreeSitter,
	}); err != nil {
		t.Fatal(err)
	}
	cached, err := os.ReadFile(filepath.Join(cache, "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cached), `\\vabc+`) && !strings.Contains(string(cached), `"\\vabc+"`) {
		// cached file should contain SCM-encoded \vabc+
		if !strings.Contains(string(cached), `\vabc+`) {
			t.Fatalf("expected translated regex in cache: %q", cached)
		}
	}
	if err := copyTreeSitterQueriesDir(cache, site); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(filepath.Join(site, "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(cached) {
		t.Fatalf("install must copy cache verbatim:\ncache=%q\nsite=%q", cached, installed)
	}
}

func TestCopyAndPatch_ValidationFailurePropagates(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	parser := filepath.Join(t.TempDir(), "javascript.so")
	if err := os.WriteFile(parser, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte(`(#match? @foo "abc+")`), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := neovimShellOutCapture
	neovimShellOutCapture = func(command string, args []string, dir string, env []string) (int, string, error) {
		return 1, "query: invalid node type", fmt.Errorf("exit 1")
	}
	t.Cleanup(func() { neovimShellOutCapture = prev })

	err := copyAndPatchNeovimTreeSitterQueriesDir(src, dst, neovimTreeSitterQueryCopyOptions{
		Language:      "javascript",
		SourceDialect: treesitterquery.DialectTreeSitter,
		ParserPath:    parser,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "javascript/highlights.scm") {
		t.Fatalf("error should identify query file: %v", err)
	}
	if !strings.Contains(err.Error(), "neovim/regex-match") {
		t.Fatalf("error should list applied rules: %v", err)
	}
}

func TestCopyAndPatch_PatchFailurePropagates(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte(`(#match? @foo "unterminated)`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := copyAndPatchNeovimTreeSitterQueriesDir(src, dst, neovimTreeSitterQueryCopyOptions{
		Language:      "javascript",
		SourceDialect: treesitterquery.DialectTreeSitter,
	})
	if err == nil {
		t.Fatal("expected patch error")
	}
	if !strings.Contains(err.Error(), "javascript/highlights.scm") {
		t.Fatalf("error should identify query file: %v", err)
	}
}

func TestCopyAndPatch_ReportsWarningDetails(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte("((identifier) @variable\n (#is-not? local))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = ConsumeIntegrationReport("github:demo/js", "v1")
	if err := copyAndPatchNeovimTreeSitterQueriesDir(src, dst, neovimTreeSitterQueryCopyOptions{
		Language:      "javascript",
		SourceID:      "github:demo/js",
		Version:       "v1",
		SourceDialect: treesitterquery.DialectTreeSitter,
	}); err != nil {
		t.Fatal(err)
	}
	lines := ConsumeIntegrationReport("github:demo/js", "v1")
	var joined string
	var sawWarning bool
	for _, line := range lines {
		joined += line.Text + "\n"
		if line.Warning && strings.Contains(line.Text, "compatibility warnings") {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Fatalf("expected warning-flagged compatibility report, got %#v", lines)
	}
	if !strings.Contains(joined, "compatibility warnings for javascript") {
		t.Fatalf("expected warning report, got %q", joined)
	}
	if !strings.Contains(joined, "#is-not?") {
		t.Fatalf("expected #is-not? explanation, got %q", joined)
	}
	if !strings.Contains(joined, "highlights.scm") {
		t.Fatalf("expected query filename in warning, got %q", joined)
	}
}

func TestSummarizeNeovimQueryPatchReport_GroupsDuplicateWarnings(t *testing.T) {
	lines := summarizeNeovimQueryPatchReport("js", nil, nil, []neovimQueryDiag{
		{
			File: "highlights.scm",
			Diagnostic: treesitterquery.Diagnostic{
				Severity: treesitterquery.SeverityWarning,
				Message:  `cannot safely translate regex construct "(?=...)"; leaving regex unchanged`,
				Line:     4,
			},
		},
		{
			File: "highlights.scm",
			Diagnostic: treesitterquery.Diagnostic{
				Severity: treesitterquery.SeverityWarning,
				Message:  `cannot safely translate regex construct "(?=...)"; leaving regex unchanged`,
				Line:     9,
			},
		},
	})
	if len(lines) != 1 {
		t.Fatalf("got %d lines: %#v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "(2 times; first at highlights.scm:4)") {
		t.Fatalf("expected grouped warning with location, got %q", lines[0])
	}
}

func TestFormatNeovimQueryValidationError_IncludesCompatibilityNotes(t *testing.T) {
	err := formatNeovimQueryValidationError("javascript", "highlights.scm", fmt.Errorf("invalid node type"), treesitterquery.PatchResult{
		Diagnostics: []treesitterquery.Diagnostic{{
			Severity: treesitterquery.SeverityWarning,
			Message:  "upstream Tree-sitter predicate #is-not? has no known lossless Neovim translation; preserved unchanged",
			Line:     2,
		}},
	})
	got := err.Error()
	if !strings.Contains(got, "Compatibility notes:") {
		t.Fatalf("expected compatibility notes, got %q", got)
	}
	if !strings.Contains(got, "#is-not?") {
		t.Fatalf("expected #is-not? note, got %q", got)
	}
}

func writeFakeExternalQueryClone(t *testing.T, destDir, lang, highlights string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(destDir, "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := fmt.Sprintf("{\"lang\":%q,\"queries_dir\":\"queries\"}\n", lang)
	if err := os.WriteFile(filepath.Join(destDir, "parser.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "queries", "highlights.scm"), []byte(highlights+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExternalQueryTargetLanguage_FromParserJSON(t *testing.T) {
	dir := t.TempDir()
	writeFakeExternalQueryClone(t, dir, "markdown_inline", "(code_span)")
	got := externalQueryTargetLanguage(dir, "markdown", []string{"markdown", "markdown_inline"})
	if got != "markdown_inline" {
		t.Fatalf("got %q want markdown_inline", got)
	}
}

func TestExternalQueryTargetLanguage_QueriesLangSubdir(t *testing.T) {
	dir := t.TempDir()
	writeQueryFile(t, filepath.Join(dir, "queries", "markdown_inline"), "highlights.scm", "(code_span)")
	got := externalQueryTargetLanguage(dir, "markdown", []string{"markdown", "markdown_inline"})
	if got != "markdown_inline" {
		t.Fatalf("got %q want markdown_inline", got)
	}
}

func TestExternalQueryTargetLanguage_Fallback(t *testing.T) {
	dir := t.TempDir()
	writeQueryFile(t, filepath.Join(dir, "queries"), "highlights.scm", "(atx_heading)")
	got := externalQueryTargetLanguage(dir, "markdown", []string{"markdown", "markdown_inline"})
	if got != "markdown" {
		t.Fatalf("got %q want markdown", got)
	}
}

func TestCacheNeovimTreeSitterQueriesAfterBuild_RoutesSiblingExternalQueries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	prevIntegrations := append([]string{}, requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prevIntegrations })

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

	sourceID := "github:demo/markdown"
	version := "v1"
	if err := os.MkdirAll(filepath.Dir(TreeSitterArtifactPath(sourceID, version, "markdown")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TreeSitterArtifactPath(sourceID, version, "markdown"), []byte("fakeparser"), 0o644); err != nil {
		t.Fatal(err)
	}

	prevShellOut := neovimShellOutCapture
	neovimShellOutCapture = func(_ string, _ []string, _ string, env []string) (int, string, error) {
		lang, queryPath := "", ""
		for _, e := range env {
			if strings.HasPrefix(e, "NVPM_TS_LANG=") {
				lang = strings.TrimPrefix(e, "NVPM_TS_LANG=")
			}
			if strings.HasPrefix(e, "NVPM_TS_QUERY_PATH=") {
				queryPath = strings.TrimPrefix(e, "NVPM_TS_QUERY_PATH=")
			}
		}
		if queryPath != "" {
			b, err := os.ReadFile(queryPath)
			if err != nil {
				return 1, err.Error(), nil
			}
			if lang == "markdown" && strings.Contains(string(b), "code_span") {
				return 1, `Query error at 2:2. Invalid node type "code_span"`, nil
			}
		}
		return 0, "", nil
	}
	t.Cleanup(func() { neovimShellOutCapture = prevShellOut })

	repo := t.TempDir()
	gram := filepath.Join(repo, "tree-sitter-markdown")
	writeQueryFile(t, filepath.Join(gram, "queries"), "highlights.scm", "(grammar_local)")
	build := registry_parser.RegistryItemTreeSitterBuild{
		Language:     "markdown",
		GrammarDir:   "tree-sitter-markdown",
		Integrations: []string{"neovim"},
		ExternalQueries: registry_parser.TreeSitterExternalQueriesList{
			{RepoURL: "https://github.com/neovim-treesitter/nvim-treesitter-queries-markdown"},
			{RepoURL: "https://github.com/neovim-treesitter/nvim-treesitter-queries-markdown_inline"},
		},
	}
	allBuild := []registry_parser.RegistryItemTreeSitterBuild{
		build,
		{Language: "markdown_inline", GrammarDir: "tree-sitter-markdown-inline", Integrations: []string{"neovim"}},
	}

	pins, err := cacheNeovimTreeSitterQueriesAfterBuild(repo, gram, sourceID, version, build, allBuild, func(string, string) bool { return true })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pins) != 2 {
		t.Fatalf("got %d pins, want 2: %#v", len(pins), pins)
	}
	for _, p := range pins {
		if p.Language != "markdown" {
			t.Fatalf("lock pin language must stay the declaring build language, got %q", p.Language)
		}
	}

	md, err := os.ReadFile(filepath.Join(neovimTreeSitterQueriesCacheDir(sourceID, version, "markdown"), "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "(atx_heading)") {
		t.Fatalf("markdown cache should keep markdown override queries, got %q", md)
	}
	if strings.Contains(string(md), "code_span") {
		t.Fatalf("markdown cache must not be overwritten by markdown_inline queries, got %q", md)
	}
	inline, err := os.ReadFile(filepath.Join(neovimTreeSitterQueriesCacheDir(sourceID, version, "markdown_inline"), "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inline), "code_span") {
		t.Fatalf("markdown_inline queries should land under markdown_inline, got %q", inline)
	}
}

func TestCacheNeovimTreeSitterQueriesAfterBuild_GrammarLocalWhenOnlySiblingExternal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	prevIntegrations := append([]string{}, requestedIntegrations...)
	SetRequestedIntegrations([]string{"neovim"})
	t.Cleanup(func() { requestedIntegrations = prevIntegrations })

	prevClone := cloneExternalQueriesRepoForCache
	cloneExternalQueriesRepoForCache = func(repoURL, destDir, _ string, _ bool, _, _ string) (string, error) {
		if !strings.Contains(repoURL, "markdown_inline") {
			t.Fatalf("unexpected clone of %s", repoURL)
		}
		writeFakeExternalQueryClone(t, destDir, "markdown_inline", "(code_span) @markup.raw")
		return "abc123def456", nil
	}
	t.Cleanup(func() { cloneExternalQueriesRepoForCache = prevClone })

	sourceID := "github:demo/markdown"
	version := "v1"
	repo := t.TempDir()
	gram := filepath.Join(repo, "tree-sitter-markdown")
	writeQueryFile(t, filepath.Join(gram, "queries"), "highlights.scm", "(atx_heading) @markup.heading")
	build := registry_parser.RegistryItemTreeSitterBuild{
		Language:     "markdown",
		GrammarDir:   "tree-sitter-markdown",
		Integrations: []string{"neovim"},
		ExternalQueries: registry_parser.TreeSitterExternalQueriesList{
			{RepoURL: "https://github.com/neovim-treesitter/nvim-treesitter-queries-markdown_inline"},
		},
	}
	allBuild := []registry_parser.RegistryItemTreeSitterBuild{
		build,
		{Language: "markdown_inline", Integrations: []string{"neovim"}},
	}

	pins, err := cacheNeovimTreeSitterQueriesAfterBuild(repo, gram, sourceID, version, build, allBuild, func(string, string) bool { return true })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pins) != 1 || pins[0].Language != "markdown" {
		t.Fatalf("want declaring-language pin, got %#v", pins)
	}

	md, err := os.ReadFile(filepath.Join(neovimTreeSitterQueriesCacheDir(sourceID, version, "markdown"), "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "(atx_heading)") {
		t.Fatalf("expected grammar-local markdown queries, got %q", md)
	}
	inline, err := os.ReadFile(filepath.Join(neovimTreeSitterQueriesCacheDir(sourceID, version, "markdown_inline"), "highlights.scm"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inline), "code_span") {
		t.Fatalf("expected sibling queries under markdown_inline, got %q", inline)
	}
}

func TestAllowMissingNeovimParserLanguages_IncludesExtraQueryLangs(t *testing.T) {
	build := []registry_parser.RegistryItemTreeSitterBuild{
		{Language: "objc", Integrations: []string{"neovim"}},
		{Language: "html_tags", QueriesOnly: true, Integrations: []string{"neovim"}},
	}
	langs := []string{"objc", "html_tags", "c"}
	got := allowMissingNeovimParserLanguages(build, langs)
	if _, ok := got["html_tags"]; !ok {
		t.Fatalf("queries_only html_tags must allow missing parser: %#v", got)
	}
	if _, ok := got["c"]; !ok {
		t.Fatalf("extra cached query lang c must allow missing parser: %#v", got)
	}
	if _, ok := got["objc"]; ok {
		t.Fatalf("objc builds a parser and must require it: %#v", got)
	}
}

func TestNeovimTreeSitterInstallLanguages_UnionsCachedQueryLangs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	sourceID := "github:demo/objc"
	version := "v1"
	if err := os.MkdirAll(neovimTreeSitterQueriesCacheDir(sourceID, version, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(neovimTreeSitterQueriesCacheDir(sourceID, version, "c"), "highlights.scm"), []byte("()"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := neovimTreeSitterInstallLanguages(sourceID, version, []string{"objc"})
	wantC, wantObjc := false, false
	for _, lang := range got {
		switch lang {
		case "c":
			wantC = true
		case "objc":
			wantObjc = true
		}
	}
	if !wantC || !wantObjc {
		t.Fatalf("got %v, want objc and c", got)
	}
}

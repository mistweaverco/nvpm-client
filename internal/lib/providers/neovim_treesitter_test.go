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

func TestResolveNeovimTreeSitterQueriesDir_PrefersGrammarLocal(t *testing.T) {
	repo := t.TempDir()
	gram := filepath.Join(repo, "g")
	if err := os.MkdirAll(filepath.Join(gram, "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gram, "queries", "highlights.scm"), []byte("(a)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "queries", "highlights.scm"), []byte("(b)"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveNeovimTreeSitterQueriesDir(repo, gram)
	want := filepath.Join(gram, "queries")
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

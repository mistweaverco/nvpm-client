package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
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
	if err := copyNeovimTreeSitterQueriesDir(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "nested", "injections.scm"))
	if err != nil || string(b) != "y" {
		t.Fatalf("nested copy: %v %q", err, b)
	}
}

func TestCopyNeovimTreeSitterQueriesDir_InheritsModelineAndNotEq(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte(`(#is-not? @x "y")`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyNeovimTreeSitterQueriesDir(src, dst, []string{"javascript"}); err != nil {
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
	if !strings.Contains(got, "#not-eq?") || strings.Contains(got, "#is-not?") {
		t.Fatalf("replace #is-not?: %q", got)
	}
}

func TestCopyNeovimTreeSitterQueriesDir_SkipsModelineWhenPresent(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	orig := "; inherits: ecma\n(#is-not? @a \"b\")\n"
	if err := os.WriteFile(filepath.Join(src, "highlights.scm"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyNeovimTreeSitterQueriesDir(src, dst, []string{"javascript"}); err != nil {
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
	if !strings.Contains(got, "#not-eq?") {
		t.Fatal("expected #is-not? rewritten")
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

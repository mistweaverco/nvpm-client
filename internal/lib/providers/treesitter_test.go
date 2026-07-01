package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

func TestSafeArtifactPackageID(t *testing.T) {
	got := SafeArtifactPackageID("github:tree-sitter/tree-sitter-typescript")
	if strings.ContainsAny(got, ":/\\") {
		t.Fatalf("expected sanitized id, got %q", got)
	}
	if got == "" {
		t.Fatalf("expected non-empty")
	}
}

func TestBuildTreeSitterParsersToCache_UsesShellOut(t *testing.T) {
	// Arrange
	oldHas := treeSitterHasCommand
	oldShell := treeSitterShellOut
	oldShellCapture := treeSitterShellOutCapture
	oldMkdir := osMkdirAll
	oldStat := treeSitterStat
	t.Cleanup(func() {
		treeSitterHasCommand = oldHas
		treeSitterShellOut = oldShell
		treeSitterShellOutCapture = oldShellCapture
		osMkdirAll = oldMkdir
		treeSitterStat = oldStat
	})

	treeSitterHasCommand = func(cmd string, args []string, env []string) bool { return true }
	treeSitterStat = func(name string) (os.FileInfo, error) { return nil, nil }

	var gotArgs []string
	treeSitterShellOut = func(command string, args []string, dir string, env []string) (int, error) {
		if command != "tree-sitter" {
			t.Fatalf("expected tree-sitter, got %q", command)
		}
		gotArgs = append([]string{}, args...)
		return 0, nil
	}
	treeSitterShellOutCapture = func(command string, args []string, dir string, env []string) (int, string, error) {
		if command != "tree-sitter" {
			t.Fatalf("expected tree-sitter, got %q", command)
		}
		gotArgs = append([]string{}, args...)
		return 0, "", nil
	}

	var mkdirPath string
	osMkdirAll = func(path string, perm os.FileMode) error {
		// This function is os.FileMode in prod; we accept any here to keep the stub simple.
		mkdirPath = path
		return nil
	}

	build := []registry_parser.RegistryItemTreeSitterBuild{
		{Language: "typescript", GrammarDir: "typescript", Integrations: []string{"neovim"}},
	}

	// Act
	langs, err := BuildTreeSitterParsersToCache("/tmp/repo", "github:tree-sitter/tree-sitter-typescript", "v0.0.1", build)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert
	if len(langs) != 1 || langs[0] != "typescript" {
		t.Fatalf("unexpected languages: %#v", langs)
	}
	if len(gotArgs) < 4 || gotArgs[0] != "build" || gotArgs[1] != "-o" {
		t.Fatalf("unexpected args: %#v", gotArgs)
	}
	if gotArgs[3] != filepath.Join("/tmp/repo", "typescript") {
		t.Fatalf("unexpected grammar dir arg: %q", gotArgs[3])
	}
	if mkdirPath == "" {
		t.Fatalf("expected mkdir to be called")
	}
}

func TestBuildTreeSitterParsersToCache_FailsWithoutCLI(t *testing.T) {
	oldHas := treeSitterHasCommand
	t.Cleanup(func() { treeSitterHasCommand = oldHas })
	treeSitterHasCommand = func(cmd string, args []string, env []string) bool { return false }

	_, err := BuildTreeSitterParsersToCache("/tmp/repo", "github:x/y", "v1", []registry_parser.RegistryItemTreeSitterBuild{
		{Language: "x", GrammarDir: "x", Integrations: []string{"neovim"}},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildTreeSitterParsersToCache_QueriesOnlySkipsCLIAndBuild(t *testing.T) {
	oldHas := treeSitterHasCommand
	oldShellCapture := treeSitterShellOutCapture
	t.Cleanup(func() {
		treeSitterHasCommand = oldHas
		treeSitterShellOutCapture = oldShellCapture
	})
	treeSitterHasCommand = func(cmd string, args []string, env []string) bool { return false }
	treeSitterShellOutCapture = func(command string, args []string, dir string, env []string) (int, string, error) {
		t.Fatalf("queries-only build should not call %s %#v", command, args)
		return 1, "", nil
	}

	langs, err := BuildTreeSitterParsersToCache("/tmp/repo", "github:x/y", "v1", []registry_parser.RegistryItemTreeSitterBuild{
		{Language: "html_tags", QueriesOnly: true, Integrations: []string{"neovim"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(langs) != 1 || langs[0] != "html_tags" {
		t.Fatalf("unexpected languages: %#v", langs)
	}
}

package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
	"github.com/mistweaverco/nvpm-client/internal/lib/treesitterdeps"
)

// IsTreeSitterParserCategory is true for curated tree-sitter grammar packages.
func IsTreeSitterParserCategory(categories []string) bool {
	return treesitterdeps.IsTreeSitterParserPackage(categories)
}

// IsTreeSitterQueriesCategory is true for registry packages that primarily ship editor queries.
func IsTreeSitterQueriesCategory(categories []string) bool {
	return treesitterdeps.IsTreeSitterQueriesPackage(categories)
}

// IsTreeSitterCategory is true for any tree-sitter-related registry package handled by the client.
func IsTreeSitterCategory(categories []string) bool {
	return IsTreeSitterParserCategory(categories) || IsTreeSitterQueriesCategory(categories)
}

func SharedLibExt() string {
	switch runtime.GOOS {
	case "darwin":
		return ".dylib"
	case "windows":
		return ".dll"
	default:
		return ".so"
	}
}

// injectable for tests
var (
	treeSitterHasCommand      = shell_out.HasCommand
	treeSitterShellOut        = shell_out.ShellOut
	treeSitterShellOutCapture = shell_out.ShellOutCapture
	osMkdirAll                = func(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
	treeSitterStat            = os.Stat
)

func HasTreeSitterCLI() bool {
	return treeSitterHasCommand("tree-sitter", []string{"--version"}, nil)
}

func SafeArtifactPackageID(sourceID string) string {
	// sourceID is like "github:tree-sitter/tree-sitter-typescript"
	s := strings.TrimSpace(sourceID)
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

// TreeSitterArtifactVersionDir is the per-version directory where built parser artifacts are stored.
func TreeSitterArtifactVersionDir(sourceID, version string) string {
	return filepath.Join(files.GetAppDataSharePath(), "artifacts", "treesitter", SafeArtifactPackageID(sourceID), version)
}

func TreeSitterArtifactPath(sourceID, version, language string) string {
	return filepath.Join(TreeSitterArtifactVersionDir(sourceID, version), language+SharedLibExt())
}

// buildTreeSitterParsersToCache builds tree-sitter parser shared libraries from
// upstream source into NVPM's artifact cache and returns the list of languages
// that were built.
func BuildTreeSitterParsersToCache(
	repoPath string,
	sourceID string,
	version string,
	build []registry_parser.RegistryItemTreeSitterBuild,
) ([]string, error) {
	if len(build) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(version) == "" {
		version = "unknown"
	}
	needsParserBuild := false
	for _, b := range build {
		if !b.QueriesOnly {
			needsParserBuild = true
			break
		}
	}
	if needsParserBuild && !HasTreeSitterCLI() {
		return nil, fmt.Errorf("tree-sitter CLI not found in PATH (required to build parsers from source)")
	}

	var built []string
	for _, b := range build {
		lang := strings.TrimSpace(b.Language)
		if lang == "" {
			continue
		}
		if b.QueriesOnly {
			built = append(built, lang)
			continue
		}
		grammarDir := strings.TrimSpace(b.GrammarDir)
		if grammarDir == "" {
			continue
		}

		outPath := TreeSitterArtifactPath(sourceID, version, lang)
		if err := osMkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, fmt.Errorf("create artifact dir: %w", err)
		}

		fullGrammarDir := filepath.Join(repoPath, filepath.FromSlash(grammarDir))

		// Some upstream grammars (including tree-sitter-sql v0.3.11) do not ship generated
		// sources in `src/` (parser.c/grammar.json) and require `tree-sitter generate` first.
		// Newer tree-sitter versions also expect `src/grammar.json` to exist during build.
		parserC := filepath.Join(fullGrammarDir, "src", "parser.c")
		grammarJSON := filepath.Join(fullGrammarDir, "src", "grammar.json")
		needsGenerate := false
		if _, err := treeSitterStat(parserC); err != nil {
			needsGenerate = true
		}
		if _, err := treeSitterStat(grammarJSON); err != nil {
			needsGenerate = true
		}
		if needsGenerate {
			code, output, err := treeSitterShellOutCapture("tree-sitter", []string{"generate"}, fullGrammarDir, nil)
			if err != nil || code != 0 {
				if strings.TrimSpace(output) == "" {
					return nil, fmt.Errorf("tree-sitter generate failed for %s in %s", lang, grammarDir)
				}
				return nil, fmt.Errorf("tree-sitter generate failed for %s in %s: %s", lang, grammarDir, strings.TrimSpace(output))
			}
		}

		code, output, err := treeSitterShellOutCapture("tree-sitter", []string{"build", "-o", outPath, fullGrammarDir}, "", nil)
		if err != nil || code != 0 {
			if strings.TrimSpace(output) == "" {
				return nil, fmt.Errorf("tree-sitter build failed for %s in %s", lang, grammarDir)
			}
			return nil, fmt.Errorf("tree-sitter build failed for %s in %s: %s", lang, grammarDir, strings.TrimSpace(output))
		}

		built = append(built, lang)
	}

	return built, nil
}

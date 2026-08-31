package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

const treeSitterParserCategory = "Tree-sitter-parser"

type treeSitterJSONFile struct {
	Grammars []treeSitterJSONGrammar `json:"grammars"`
}

type treeSitterJSONGrammar struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type treeSitterParserJSONFile struct {
	Lang        string `json:"lang"`
	QueriesDir  string `json:"queries_dir"`
	QueriesPath string `json:"queries_path"`
}

// integrateGitHostedTreeSitter builds parsers and runs Neovim integration for a git checkout.
// Registry packages use their treesitter.build metadata. Unknown GitHub/GitLab/Codeberg grammars
// are inferred from tree-sitter.json / parser.json / src/parser.c when --integrate neovim is set.
func integrateGitHostedTreeSitter(sourceID, version, repoPath string, registryItem registry_parser.RegistryItem) ([]local_packages_parser.TreeSitterExternalQueryPin, error) {
	item := withInferredTreeSitter(registryItem, repoPath, sourceID)
	if integrationEnabled("neovim") && (item.TreeSitter == nil || len(item.TreeSitter.Build) == 0) {
		AddIntegrationReportWarning(sourceID, version,
			"Neovim integration requested, but this package is not a registry Tree-sitter parser and no grammar was found in the checkout (tree-sitter.json or src/parser.c)")
		return nil, nil
	}
	var opts *buildTreeSitterOpts
	if pre := nestedTreeSitterExternalQueryPreflight(); pre != nil {
		opts = &buildTreeSitterOpts{ExternalQueryPreflight: pre}
	}
	return buildAndMaybeIntegrateTreeSitter(repoPath, item, version, opts)
}

// withInferredTreeSitter fills missing treesitter.build from the checkout when Neovim
// integration is requested. Registry metadata always wins.
func withInferredTreeSitter(item registry_parser.RegistryItem, repoPath, sourceID string) registry_parser.RegistryItem {
	if item.TreeSitter != nil && len(item.TreeSitter.Build) > 0 {
		if strings.TrimSpace(item.Source.ID) == "" {
			item.Source.ID = sourceID
		}
		return item
	}
	if !integrationEnabled("neovim") {
		return item
	}
	build := inferTreeSitterBuildFromRepo(repoPath, sourceID)
	if len(build) == 0 {
		return item
	}
	if strings.TrimSpace(item.Source.ID) == "" {
		item.Source.ID = sourceID
	}
	if !IsTreeSitterCategory(item.Categories) {
		item.Categories = append(append([]string(nil), item.Categories...), treeSitterParserCategory)
	}
	item.TreeSitter = &registry_parser.RegistryItemTreeSitter{Build: build}
	return item
}

func inferTreeSitterBuildFromRepo(repoPath, sourceID string) []registry_parser.RegistryItemTreeSitterBuild {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return nil
	}
	var build []registry_parser.RegistryItemTreeSitterBuild
	if grammars := readTreeSitterJSONGrammars(repoPath); len(grammars) > 0 {
		for _, g := range grammars {
			lang := strings.TrimSpace(g.Name)
			if lang == "" {
				continue
			}
			grammarDir := strings.TrimSpace(g.Path)
			if grammarDir == "" {
				grammarDir = "."
			}
			build = append(build, registry_parser.RegistryItemTreeSitterBuild{
				Language:     lang,
				GrammarDir:   grammarDir,
				Integrations: []string{"neovim"},
			})
		}
	}
	if len(build) == 0 && looksLikeTreeSitterGrammarRoot(repoPath) {
		lang := languageFromParserJSON(repoPath)
		if lang == "" {
			lang = languageFromTreeSitterSourceID(sourceID)
		}
		if lang != "" {
			build = append(build, registry_parser.RegistryItemTreeSitterBuild{
				Language:     lang,
				GrammarDir:   ".",
				Integrations: []string{"neovim"},
			})
		}
	}
	for i := range build {
		applyParserJSONQueries(repoPath, &build[i])
	}
	return build
}

func readTreeSitterJSONGrammars(repoPath string) []treeSitterJSONGrammar {
	b, err := os.ReadFile(filepath.Join(repoPath, "tree-sitter.json"))
	if err != nil {
		return nil
	}
	var meta treeSitterJSONFile
	if json.Unmarshal(b, &meta) != nil {
		return nil
	}
	return meta.Grammars
}

func looksLikeTreeSitterGrammarRoot(repoPath string) bool {
	for _, rel := range []string{
		filepath.Join("src", "parser.c"),
		"grammar.js",
		"grammar.json",
	} {
		if st, err := os.Stat(filepath.Join(repoPath, rel)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func readParserJSON(dir string) (treeSitterParserJSONFile, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "parser.json"))
	if err != nil {
		return treeSitterParserJSONFile{}, false
	}
	var meta treeSitterParserJSONFile
	if json.Unmarshal(b, &meta) != nil {
		return treeSitterParserJSONFile{}, false
	}
	return meta, true
}

func languageFromParserJSON(repoPath string) string {
	meta, ok := readParserJSON(repoPath)
	if !ok {
		return ""
	}
	return strings.TrimSpace(meta.Lang)
}

func applyParserJSONQueries(repoPath string, row *registry_parser.RegistryItemTreeSitterBuild) {
	if row == nil {
		return
	}
	lang := strings.TrimSpace(row.Language)
	dirs := []string{repoPath}
	if gd := strings.TrimSpace(row.GrammarDir); gd != "" && gd != "." {
		dirs = append([]string{filepath.Join(repoPath, filepath.FromSlash(gd))}, dirs...)
	}
	for _, dir := range dirs {
		meta, ok := readParserJSON(dir)
		if !ok {
			continue
		}
		metaLang := strings.TrimSpace(meta.Lang)
		if metaLang != "" && lang != "" && !strings.EqualFold(metaLang, lang) {
			continue
		}
		if q := strings.TrimSpace(meta.QueriesDir); q != "" && strings.TrimSpace(row.QueriesDir) == "" {
			row.QueriesDir = q
		}
		if q := strings.TrimSpace(meta.QueriesPath); q != "" && strings.TrimSpace(row.QueriesPath) == "" {
			row.QueriesPath = q
		}
		return
	}
}

func languageFromTreeSitterSourceID(sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	_, rest, found := strings.Cut(sourceID, ":")
	if found {
		sourceID = rest
	}
	if i := strings.LastIndex(sourceID, "/"); i >= 0 {
		sourceID = sourceID[i+1:]
	}
	sourceID = strings.TrimSuffix(sourceID, ".git")
	name := sourceID
	for _, prefix := range []string{"tree-sitter-", "tree_sitter_"} {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			name = name[len(prefix):]
			break
		}
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "tree-sitter" || name == "tree_sitter" {
		return ""
	}
	return name
}

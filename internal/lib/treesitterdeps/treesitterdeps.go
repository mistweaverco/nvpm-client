// Package treesitterdeps implements layered tree-sitter dependency planning:
// parser requires (compile-order), query inheritance, and external query URL resolution.
package treesitterdeps

import (
	"fmt"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

// Layer identifies which dependency slice an edge belongs to (for diagnostics / future expansion).
type Layer int

const (
	LayerParser Layer = iota
	LayerQueryInherit
	LayerEditor
	LayerInjection
)

// IsTreeSitterParserPackage reports whether categories include the curated parser grammar role.
func IsTreeSitterParserPackage(categories []string) bool {
	for _, c := range categories {
		if strings.EqualFold(strings.TrimSpace(c), "Tree-sitter-parser") {
			return true
		}
	}
	return false
}

// IsTreeSitterQueriesPackage reports whether categories include query-only registry packages.
func IsTreeSitterQueriesPackage(categories []string) bool {
	for _, c := range categories {
		if strings.EqualFold(strings.TrimSpace(c), "Tree-sitter-queries") {
			return true
		}
	}
	return false
}

func normLang(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// RegistryIndex is the minimal registry surface for resolution (implemented by *registry_parser.RegistryParser).
type RegistryIndex interface {
	GetBySourceId(sourceID string) registry_parser.RegistryItem
	GetData(force bool) registry_parser.RegistryRoot
}

// LanguageNeedsNeovimParser reports whether any registry parser package builds a native grammar
// artifact (non queries_only with grammar_dir) for lang.
func LanguageNeedsNeovimParser(reg RegistryIndex, lang string) bool {
	want := normLang(lang)
	if want == "" {
		return false
	}
	for _, id := range ParserCandidates(reg, want) {
		item := reg.GetBySourceId(id)
		if item.TreeSitter == nil {
			continue
		}
		for _, b := range item.TreeSitter.Build {
			if normLang(b.Language) == want && !b.QueriesOnly && strings.TrimSpace(b.GrammarDir) != "" {
				return true
			}
		}
	}
	return false
}

// ParserCandidates returns registry source ids for packages that provide a parser build row
// for the given tree-sitter language (Tree-sitter-parser category only).
func ParserCandidates(reg RegistryIndex, lang string) []string {
	want := normLang(lang)
	if want == "" {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, item := range reg.GetData(false) {
		if item.Source.ID == "" || !IsTreeSitterParserPackage(item.Categories) {
			continue
		}
		ok := false
		for _, l := range item.Languages {
			if normLang(l) == want {
				ok = true
				break
			}
		}
		if !ok && item.TreeSitter != nil {
			for _, b := range item.TreeSitter.Build {
				if normLang(b.Language) == want && !b.QueriesOnly {
					ok = true
					break
				}
			}
		}
		if ok {
			if _, dup := seen[item.Source.ID]; dup {
				continue
			}
			seen[item.Source.ID] = struct{}{}
			out = append(out, item.Source.ID)
		}
	}
	return out
}

// QueryPackageCandidates returns registry source ids for Tree-sitter-queries packages
// that ship Neovim queries for lang and declare the given editor integration on a matching build row.
func QueryPackageCandidates(reg RegistryIndex, lang, editor string) []string {
	want := normLang(lang)
	ed := strings.ToLower(strings.TrimSpace(editor))
	if want == "" || ed == "" {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, item := range reg.GetData(false) {
		if item.Source.ID == "" || !IsTreeSitterQueriesPackage(item.Categories) {
			continue
		}
		if item.TreeSitter == nil {
			continue
		}
		for _, b := range item.TreeSitter.Build {
			if normLang(b.Language) != want {
				continue
			}
			if !buildDeclaresEditor(b, ed) {
				continue
			}
			if _, dup := seen[item.Source.ID]; dup {
				continue
			}
			seen[item.Source.ID] = struct{}{}
			out = append(out, item.Source.ID)
		}
	}
	return out
}

func buildDeclaresEditor(b registry_parser.RegistryItemTreeSitterBuild, editor string) bool {
	if strings.TrimSpace(editor) == "" {
		return true
	}
	for _, i := range b.Integrations {
		if strings.EqualFold(strings.TrimSpace(i), editor) {
			return true
		}
	}
	return false
}

// BuildParserRequireEdges walks parser build rows (non queries_only) starting from root and follows
// requires chains across registry packages. resolveLang maps a required language name to a concrete
// registry source id (lockfile, prompt, or default). Edges are depLang -> dependentLang (dep builds first).
func BuildParserRequireEdges(
	root registry_parser.RegistryItem,
	reg RegistryIndex,
	resolveLang func(requiredLang string) (sourceID string, err error),
) (map[string][]string, error) {
	edges := map[string][]string{} // dep -> dependents (dep installs first)
	addEdge := func(dep, dependent string) {
		dep, dependent = normLang(dep), normLang(dependent)
		if dep == "" || dependent == "" || dep == dependent {
			return
		}
		list := edges[dep]
		for _, x := range list {
			if x == dependent {
				return
			}
		}
		edges[dep] = append(list, dependent)
	}

	seenPkg := map[string]struct{}{}
	var visitPkg func(item registry_parser.RegistryItem) error
	visitRow := func(item registry_parser.RegistryItem, b registry_parser.RegistryItemTreeSitterBuild) error {
		if b.QueriesOnly {
			return nil
		}
		if strings.TrimSpace(b.GrammarDir) == "" {
			return nil
		}
		lang := normLang(b.Language)
		if lang == "" {
			return nil
		}
		for _, r := range b.Requires {
			r = normLang(r)
			if r == "" {
				continue
			}
			addEdge(r, lang)
			sourceID, err := resolveLang(r)
			if err != nil {
				return err
			}
			sourceID = strings.TrimSpace(sourceID)
			if sourceID == "" {
				return fmt.Errorf("parser layer: empty source id resolving language %q", r)
			}
			depItem := reg.GetBySourceId(sourceID)
			if depItem.Source.ID == "" {
				return fmt.Errorf("parser layer: unknown registry package %q for language %q", sourceID, r)
			}
			if err := visitPkg(depItem); err != nil {
				return err
			}
		}
		return nil
	}
	visitPkg = func(item registry_parser.RegistryItem) error {
		if item.Source.ID == "" {
			return nil
		}
		if _, ok := seenPkg[item.Source.ID]; ok {
			return nil
		}
		seenPkg[item.Source.ID] = struct{}{}
		if item.TreeSitter == nil {
			return nil
		}
		for _, b := range item.TreeSitter.Build {
			if err := visitRow(item, b); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visitPkg(root); err != nil {
		return nil, err
	}
	return edges, nil
}

// TopoInstallOrder returns languages in an order where every dependent appears after its parser requires.
func TopoInstallOrder(rootLangs []string, edges map[string][]string) ([]string, error) {
	nodes := map[string]struct{}{}
	for _, l := range rootLangs {
		if s := normLang(l); s != "" {
			nodes[s] = struct{}{}
		}
	}
	for dep := range edges {
		nodes[dep] = struct{}{}
		for _, d := range edges[dep] {
			nodes[d] = struct{}{}
		}
	}
	indeg := map[string]int{}
	for n := range nodes {
		indeg[n] = 0
	}
	for _, outs := range edges {
		for _, out := range outs {
			if _, ok := indeg[out]; ok {
				indeg[out]++
			}
		}
	}
	var q []string
	for n := range nodes {
		if indeg[n] == 0 {
			q = append(q, n)
		}
	}
	sortStringsStable(q)
	var order []string
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		order = append(order, n)
		for _, out := range edges[n] {
			if _, ok := indeg[out]; !ok {
				continue
			}
			indeg[out]--
			if indeg[out] == 0 {
				q = append(q, out)
				sortStringsStable(q)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("parser layer: cycle detected in requires graph involving %v", sortedKeys(nodes))
	}
	return order, nil
}

func sortStringsStable(s []string) {
	// tiny insertion sort for stable small slices
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && s[j-1] > s[j] {
			s[j-1], s[j] = s[j], s[j-1]
			j--
		}
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStringsStable(out)
	return out
}

// RootParserLanguages returns languages for non queries_only build rows that declare a grammar_dir.
func RootParserLanguages(root registry_parser.RegistryItem) []string {
	if root.TreeSitter == nil {
		return nil
	}
	var out []string
	for _, b := range root.TreeSitter.Build {
		if b.QueriesOnly {
			continue
		}
		if strings.TrimSpace(b.GrammarDir) == "" {
			continue
		}
		if s := normLang(b.Language); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// CollectQueryInheritEdges returns dep -> dependents for Neovim query inheritance (inherits:).
func CollectQueryInheritEdges(root registry_parser.RegistryItem, editor string) map[string][]string {
	edges := map[string][]string{}
	if root.TreeSitter == nil {
		return edges
	}
	ed := strings.ToLower(strings.TrimSpace(editor))
	addEdge := func(dep, dependent string) {
		dep, dependent = normLang(dep), normLang(dependent)
		if dep == "" || dependent == "" || dep == dependent {
			return
		}
		list := edges[dep]
		for _, x := range list {
			if x == dependent {
				return
			}
		}
		edges[dep] = append(list, dependent)
	}
	for _, b := range root.TreeSitter.Build {
		if !buildDeclaresEditor(b, ed) {
			continue
		}
		lang := normLang(b.Language)
		if lang == "" {
			continue
		}
		for _, in := range b.Inherits {
			addEdge(normLang(in), lang)
		}
	}
	return edges
}

func mergeParserRequireEdges(dst, src map[string][]string) {
	for dep, dependents := range src {
		list := dst[dep]
		for _, d := range dependents {
			d = normLang(d)
			if d == "" {
				continue
			}
			dup := false
			for _, x := range list {
				if x == d {
					dup = true
					break
				}
			}
			if !dup {
				list = append(list, d)
			}
		}
		dst[dep] = list
	}
}

// BuildInjectionParserRequireEdges merges parser-layer requires edges from each injection host
// language's registry package (treesitter.build[].requires on those grammars).
func BuildInjectionParserRequireEdges(
	root registry_parser.RegistryItem,
	reg RegistryIndex,
	editor string,
	resolveLang func(requiredLang string) (sourceID string, err error),
) (edges map[string][]string, injectionLangs []string, err error) {
	injectionLangs = MergeInjectionLanguagesForEditor(root, editor)
	edges = map[string][]string{}
	if len(injectionLangs) == 0 {
		return edges, nil, nil
	}
	for _, lang := range injectionLangs {
		sourceID, err := resolveLang(lang)
		if err != nil {
			return nil, nil, err
		}
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			continue
		}
		depItem := reg.GetBySourceId(sourceID)
		if depItem.Source.ID == "" {
			continue
		}
		sub, err := BuildParserRequireEdges(depItem, reg, resolveLang)
		if err != nil {
			return nil, nil, err
		}
		mergeParserRequireEdges(edges, sub)
	}
	return edges, injectionLangs, nil
}

// MergeInjectionLanguages returns a deduplicated list of injection host languages declared on build rows.
func MergeInjectionLanguages(root registry_parser.RegistryItem) []string {
	return MergeInjectionLanguagesForEditor(root, "")
}

// MergeInjectionLanguagesForEditor returns injection host languages from build rows that declare
// the given editor integration. When editor is empty, all build rows are considered.
func MergeInjectionLanguagesForEditor(root registry_parser.RegistryItem, editor string) []string {
	if root.TreeSitter == nil {
		return nil
	}
	ed := strings.ToLower(strings.TrimSpace(editor))
	seen := map[string]struct{}{}
	var out []string
	for _, b := range root.TreeSitter.Build {
		if ed != "" && !buildDeclaresEditor(b, ed) {
			continue
		}
		for _, inj := range b.Injections {
			s := normLang(inj)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sortStringsStable(out)
	return out
}

// ResolveExternalQueryRepoURL returns a git HTTPS URL for an external_queries spec.
// Exactly one of spec.RepoURL or spec.Package must be set.
func ResolveExternalQueryRepoURL(spec registry_parser.RegistryItemTreeSitterExternalQueries, reg RegistryIndex) (string, error) {
	repo := strings.TrimSpace(spec.RepoURL)
	pkg := strings.TrimSpace(spec.Package)
	if repo != "" && pkg != "" {
		return "", fmt.Errorf("external_queries: set only one of repo_url or package")
	}
	if repo != "" {
		return repo, nil
	}
	if pkg == "" {
		return "", fmt.Errorf("external_queries: repo_url or package is required")
	}
	item := reg.GetBySourceId(pkg)
	if item.Source.ID == "" {
		return "", fmt.Errorf("external_queries: unknown registry package %q", pkg)
	}
	if u := SourceIDToHTTPSCloneURL(item.Source.ID); u != "" {
		return u, nil
	}
	hp := strings.TrimSpace(item.Homepage)
	if strings.HasPrefix(strings.ToLower(hp), "http") {
		return strings.TrimSuffix(hp, "/"), nil
	}
	return "", fmt.Errorf("external_queries: cannot derive clone URL for package %q", pkg)
}

// SourceIDToHTTPSCloneURL maps a registry source id to an https git remote when possible.
func SourceIDToHTTPSCloneURL(sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	if strings.HasPrefix(sourceID, "github:") {
		rest := strings.TrimPrefix(sourceID, "github:")
		rest = strings.Trim(rest, "/")
		if rest == "" {
			return ""
		}
		return "https://github.com/" + rest
	}
	if strings.HasPrefix(sourceID, "gitlab:") {
		rest := strings.Trim(strings.TrimPrefix(sourceID, "gitlab:"), "/")
		if rest == "" {
			return ""
		}
		return "https://gitlab.com/" + rest
	}
	if strings.HasPrefix(sourceID, "codeberg:") {
		rest := strings.Trim(strings.TrimPrefix(sourceID, "codeberg:"), "/")
		if rest == "" {
			return ""
		}
		return "https://codeberg.org/" + rest
	}
	return ""
}

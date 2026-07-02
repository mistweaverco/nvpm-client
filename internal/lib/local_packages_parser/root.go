package local_packages_parser

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// marshalIndent is a package-level variable to allow injection during tests
var marshalIndent = json.MarshalIndent

const lockSchemaURL = "https://nvpm.dev/nvpm-lock.schema.json"

type LocalPackageItem struct {
	SourceID string `json:"sourceId"`
	Version  string `json:"version"`
	// Commit is the resolved full git commit SHA for git-hosted packages (github/gitlab/codeberg).
	Commit string         `json:"commit,omitempty"`
	Extras *PackageExtras `json:"extras,omitempty"`
}

type PackageExtras struct {
	Integrations []string `json:"integrations,omitempty"`
	// TreeSitterParserChoices pins which registry Tree-sitter-parser package to use for a language
	// when several registry entries provide the same language (requires / inherit resolution).
	TreeSitterParserChoices []TreeSitterParserChoice `json:"treesitter_parser_choices,omitempty"`
	// TreeSitterQueryChoices pins which Tree-sitter-queries registry package to use for a language
	// when several entries provide the same language for an editor integration.
	TreeSitterQueryChoices []TreeSitterQueryChoice `json:"treesitter_query_choices,omitempty"`
	// TreeSitterExternalQueries pins optional external query-only git deps (commit SHA per repo URL)
	// so nvpm sync can reproduce the same query trees without re-resolving semver. Multiple rows may
	// share the same language when several query-only repositories apply.
	TreeSitterExternalQueries []TreeSitterExternalQueryPin `json:"treesitter_external_queries,omitempty"`
}

// TreeSitterParserChoice records a disambiguated parser package for a tree-sitter language name.
type TreeSitterParserChoice struct {
	Language string `json:"language"`
	SourceID string `json:"sourceId"`
}

// TreeSitterQueryChoice records a disambiguated Tree-sitter-queries package for a language + integration.
type TreeSitterQueryChoice struct {
	Language    string `json:"language"`
	Integration string `json:"integration"`
	SourceID    string `json:"sourceId"`
}

// TreeSitterExternalQueryPin records the resolved git revision for an external_queries repo.
// Multiple pins may share the same language when the registry lists several query-only repositories.
type TreeSitterExternalQueryPin struct {
	Language string `json:"language"`
	RepoURL  string `json:"repo_url"`
	Ref      string `json:"ref"` // full commit SHA from git rev-parse HEAD after clone
}

type LocalPackageRoot struct {
	Packages []LocalPackageItem `json:"packages"`
	Schema   string             `json:"$schema,omitempty"`
}

// LocalPackagesParser implements LocalPackagesManager
type LocalPackagesParser struct {
	fileManager FileManager
}

// New creates a new LocalPackagesParser with the default file manager
func New() *LocalPackagesParser {
	return &LocalPackagesParser{
		fileManager: &DefaultFileManager{},
	}
}

// NewWithFileManager creates a new LocalPackagesParser with a custom file manager
func NewWithFileManager(fileManager FileManager) *LocalPackagesParser {
	return &LocalPackagesParser{
		fileManager: fileManager,
	}
}

// normalizePackageID converts a package ID from legacy format (pkg:provider/pkg)
// to the new format (provider:pkg), or returns it unchanged if already in new format.
// This ensures backward compatibility when reading nvpm-lock.json files.
func normalizePackageID(sourceID string) string {
	if strings.HasPrefix(sourceID, "pkg:") {
		rest := strings.TrimPrefix(sourceID, "pkg:")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 2 {
			return parts[0] + ":" + parts[1]
		}
	}
	return sourceID
}

// GetData returns the local packages data from the local packages file.
// The force flag is ignored; data is always read from disk to avoid caching.
// Package IDs are normalized from legacy format (pkg:provider/pkg) to new format (provider:pkg)
// for backward compatibility.
func (lpp *LocalPackagesParser) GetData(force bool) LocalPackageRoot {
	localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
	var localPackageRoot LocalPackageRoot

	if !lpp.fileManager.FileExists(localPackagesFile) {
		return LocalPackageRoot{Packages: []LocalPackageItem{}}
	}

	byteValue, err := lpp.fileManager.ReadFile(localPackagesFile)
	if err != nil {
		fmt.Printf("Warning: failed to read local packages file: %v\n", err)
		return LocalPackageRoot{Packages: []LocalPackageItem{}}
	}

	if err := json.Unmarshal(byteValue, &localPackageRoot); err != nil {
		fmt.Printf("Warning: failed to parse local packages file: %v\n", err)
		return LocalPackageRoot{Packages: []LocalPackageItem{}}
	}

	// Normalize all package IDs from legacy format to new format
	for i := range localPackageRoot.Packages {
		localPackageRoot.Packages[i].SourceID = normalizePackageID(localPackageRoot.Packages[i].SourceID)
	}

	return localPackageRoot
}

func normalizeIntegrations(integrations []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(integrations))
	for _, i := range integrations {
		i = strings.ToLower(strings.TrimSpace(i))
		if i == "" {
			continue
		}
		if _, ok := seen[i]; ok {
			continue
		}
		seen[i] = struct{}{}
		out = append(out, i)
	}
	return out
}

func (lpp *LocalPackagesParser) MergePackageIntegrations(sourceID string, integrations []string) error {
	integrations = normalizeIntegrations(integrations)
	if len(integrations) == 0 {
		return nil
	}

	sourceID = normalizePackageID(sourceID)
	if strings.TrimSpace(sourceID) == "" {
		return nil
	}

	root := lpp.GetData(false)
	for i := range root.Packages {
		if root.Packages[i].SourceID != sourceID {
			continue
		}
		if root.Packages[i].Extras == nil {
			root.Packages[i].Extras = &PackageExtras{}
		}
		root.Packages[i].Extras.Integrations = normalizeIntegrations(
			append(root.Packages[i].Extras.Integrations, integrations...),
		)
		goto write
	}
	// Package not found in lockfile (shouldn't happen if caller updated it first).
	return nil

write:
	root.Schema = lockSchemaURL
	localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
	jsonData, err := marshalIndent(root, "", "  ")
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return err
	}
	if err := lpp.fileManager.WriteFile(localPackagesFile, jsonData, 0644); err != nil {
		fmt.Println("Error writing to file:", err)
		return err
	}
	return nil
}

func normalizeExternalQueryRepoURLForPin(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(strings.TrimSuffix(u, ".git"), "/")
	return strings.ToLower(u)
}

func treeSitterExternalQueryPinKey(lang, repoURL string) string {
	return strings.ToLower(strings.TrimSpace(lang)) + "\x00" + normalizeExternalQueryRepoURLForPin(repoURL)
}

// MergePackageTreeSitterExternalQueryPins upserts pins for optional external query git clones
// (commit SHA + repo URL). The lock row must already exist for sourceID. Multiple repos per
// language are keyed by language and repo_url together.
func (lpp *LocalPackagesParser) MergePackageTreeSitterExternalQueryPins(sourceID string, pins []TreeSitterExternalQueryPin) error {
	sourceID = normalizePackageID(sourceID)
	if strings.TrimSpace(sourceID) == "" || len(pins) == 0 {
		return nil
	}

	root := lpp.GetData(false)
	for i := range root.Packages {
		if root.Packages[i].SourceID != sourceID {
			continue
		}
		if root.Packages[i].Extras == nil {
			root.Packages[i].Extras = &PackageExtras{}
		}
		byKey := map[string]TreeSitterExternalQueryPin{}
		for _, p := range root.Packages[i].Extras.TreeSitterExternalQueries {
			l := strings.TrimSpace(p.Language)
			r := strings.TrimSpace(p.RepoURL)
			if l == "" || r == "" {
				continue
			}
			byKey[treeSitterExternalQueryPinKey(l, r)] = p
		}
		for _, p := range pins {
			l := strings.TrimSpace(p.Language)
			r := strings.TrimSpace(p.RepoURL)
			if l == "" || r == "" || strings.TrimSpace(p.Ref) == "" {
				continue
			}
			k := treeSitterExternalQueryPinKey(l, r)
			byKey[k] = TreeSitterExternalQueryPin{
				Language: l,
				RepoURL:  r,
				Ref:      strings.TrimSpace(p.Ref),
			}
		}
		merged := make([]TreeSitterExternalQueryPin, 0, len(byKey))
		for _, p := range byKey {
			merged = append(merged, p)
		}
		sort.Slice(merged, func(a, b int) bool {
			la := strings.ToLower(merged[a].Language)
			lb := strings.ToLower(merged[b].Language)
			if la != lb {
				return la < lb
			}
			return strings.ToLower(merged[a].RepoURL) < strings.ToLower(merged[b].RepoURL)
		})
		root.Packages[i].Extras.TreeSitterExternalQueries = merged

		root.Schema = lockSchemaURL
		localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
		jsonData, err := marshalIndent(root, "", "  ")
		if err != nil {
			fmt.Println("Error marshaling JSON:", err)
			return err
		}
		if err := lpp.fileManager.WriteFile(localPackagesFile, jsonData, 0644); err != nil {
			fmt.Println("Error writing to file:", err)
			return err
		}
		return nil
	}
	return nil
}

// GetTreeSitterParserLockChoice returns the pinned parser source id for a language on a consumer package row.
func GetTreeSitterParserLockChoice(consumerSourceID, language string) (sourceID string, ok bool) {
	item := GetBySourceId(consumerSourceID)
	if item.SourceID == "" || item.Extras == nil {
		return "", false
	}
	want := strings.ToLower(strings.TrimSpace(language))
	for _, c := range item.Extras.TreeSitterParserChoices {
		if strings.ToLower(strings.TrimSpace(c.Language)) == want && strings.TrimSpace(c.SourceID) != "" {
			return strings.TrimSpace(c.SourceID), true
		}
	}
	return "", false
}

// MergePackageTreeSitterParserChoice records which registry parser package to use for a language.
// consumerVersion is used to create a new lock row when the consumer package is not yet recorded.
func (lpp *LocalPackagesParser) MergePackageTreeSitterParserChoice(consumerSourceID, language, chosenSourceID, consumerVersion string) error {
	consumerSourceID = normalizePackageID(consumerSourceID)
	language = strings.TrimSpace(language)
	chosenSourceID = strings.TrimSpace(chosenSourceID)
	if consumerSourceID == "" || language == "" || chosenSourceID == "" {
		return fmt.Errorf("merge parser choice: missing consumer, language, or source id")
	}

	root := lpp.GetData(false)
	idx := -1
	for i := range root.Packages {
		if root.Packages[i].SourceID == consumerSourceID {
			idx = i
			break
		}
	}
	if idx < 0 {
		v := strings.TrimSpace(consumerVersion)
		if v == "" {
			return fmt.Errorf("merge parser choice: no lock row for %s", consumerSourceID)
		}
		root.Packages = append(root.Packages, LocalPackageItem{
			SourceID: consumerSourceID,
			Version:  v,
			Extras: &PackageExtras{
				TreeSitterParserChoices: []TreeSitterParserChoice{{Language: language, SourceID: chosenSourceID}},
			},
		})
		root.Schema = lockSchemaURL
		localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
		jsonData, err := marshalIndent(root, "", "  ")
		if err != nil {
			return err
		}
		return lpp.fileManager.WriteFile(localPackagesFile, jsonData, 0644)
	}

	if root.Packages[idx].Extras == nil {
		root.Packages[idx].Extras = &PackageExtras{}
	}
	byLang := map[string]TreeSitterParserChoice{}
	for _, c := range root.Packages[idx].Extras.TreeSitterParserChoices {
		l := strings.ToLower(strings.TrimSpace(c.Language))
		if l == "" || strings.TrimSpace(c.SourceID) == "" {
			continue
		}
		byLang[l] = c
	}
	byLang[strings.ToLower(language)] = TreeSitterParserChoice{Language: language, SourceID: chosenSourceID}
	merged := make([]TreeSitterParserChoice, 0, len(byLang))
	for _, c := range byLang {
		merged = append(merged, c)
	}
	sort.Slice(merged, func(a, b int) bool {
		return strings.ToLower(merged[a].Language) < strings.ToLower(merged[b].Language)
	})
	root.Packages[idx].Extras.TreeSitterParserChoices = merged
	root.Schema = lockSchemaURL
	localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
	jsonData, err := marshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return lpp.fileManager.WriteFile(localPackagesFile, jsonData, 0644)
}

func MergePackageTreeSitterParserChoice(consumerSourceID, language, chosenSourceID, consumerVersion string) error {
	return globalParser.MergePackageTreeSitterParserChoice(consumerSourceID, language, chosenSourceID, consumerVersion)
}

func queryLockKey(language, integration string) string {
	return strings.ToLower(strings.TrimSpace(language)) + "\x00" + strings.ToLower(strings.TrimSpace(integration))
}

// GetTreeSitterQueryLockChoice returns the pinned Tree-sitter-queries source id for language+integration.
func GetTreeSitterQueryLockChoice(consumerSourceID, language, integration string) (sourceID string, ok bool) {
	item := GetBySourceId(consumerSourceID)
	if item.SourceID == "" || item.Extras == nil {
		return "", false
	}
	want := queryLockKey(language, integration)
	for _, c := range item.Extras.TreeSitterQueryChoices {
		if queryLockKey(c.Language, c.Integration) == want && strings.TrimSpace(c.SourceID) != "" {
			return strings.TrimSpace(c.SourceID), true
		}
	}
	return "", false
}

// MergePackageTreeSitterQueryChoice records which Tree-sitter-queries registry package to use.
func (lpp *LocalPackagesParser) MergePackageTreeSitterQueryChoice(
	consumerSourceID, language, integration, chosenSourceID, consumerVersion string,
) error {
	consumerSourceID = normalizePackageID(consumerSourceID)
	language = strings.TrimSpace(language)
	integration = strings.TrimSpace(integration)
	chosenSourceID = strings.TrimSpace(chosenSourceID)
	if consumerSourceID == "" || language == "" || integration == "" || chosenSourceID == "" {
		return fmt.Errorf("merge query choice: missing consumer, language, integration, or source id")
	}

	root := lpp.GetData(false)
	idx := -1
	for i := range root.Packages {
		if root.Packages[i].SourceID == consumerSourceID {
			idx = i
			break
		}
	}
	if idx < 0 {
		v := strings.TrimSpace(consumerVersion)
		if v == "" {
			return fmt.Errorf("merge query choice: no lock row for %s", consumerSourceID)
		}
		root.Packages = append(root.Packages, LocalPackageItem{
			SourceID: consumerSourceID,
			Version:  v,
			Extras: &PackageExtras{
				TreeSitterQueryChoices: []TreeSitterQueryChoice{
					{Language: language, Integration: integration, SourceID: chosenSourceID},
				},
			},
		})
		root.Schema = lockSchemaURL
		localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
		jsonData, err := marshalIndent(root, "", "  ")
		if err != nil {
			return err
		}
		return lpp.fileManager.WriteFile(localPackagesFile, jsonData, 0644)
	}

	if root.Packages[idx].Extras == nil {
		root.Packages[idx].Extras = &PackageExtras{}
	}
	byKey := map[string]TreeSitterQueryChoice{}
	for _, c := range root.Packages[idx].Extras.TreeSitterQueryChoices {
		if strings.TrimSpace(c.Language) == "" || strings.TrimSpace(c.Integration) == "" || strings.TrimSpace(c.SourceID) == "" {
			continue
		}
		byKey[queryLockKey(c.Language, c.Integration)] = c
	}
	byKey[queryLockKey(language, integration)] = TreeSitterQueryChoice{
		Language: language, Integration: integration, SourceID: chosenSourceID,
	}
	merged := make([]TreeSitterQueryChoice, 0, len(byKey))
	for _, c := range byKey {
		merged = append(merged, c)
	}
	sort.Slice(merged, func(a, b int) bool {
		ka := queryLockKey(merged[a].Language, merged[a].Integration)
		kb := queryLockKey(merged[b].Language, merged[b].Integration)
		return ka < kb
	})
	root.Packages[idx].Extras.TreeSitterQueryChoices = merged
	root.Schema = lockSchemaURL
	localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
	jsonData, err := marshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return lpp.fileManager.WriteFile(localPackagesFile, jsonData, 0644)
}

func MergePackageTreeSitterQueryChoice(
	consumerSourceID, language, integration, chosenSourceID, consumerVersion string,
) error {
	return globalParser.MergePackageTreeSitterQueryChoice(consumerSourceID, language, integration, chosenSourceID, consumerVersion)
}

// GetDataForProvider returns the local packages data
// for a specific provider. Supports both legacy (pkg:provider/pkg) and new (provider:pkg) formats.
func (lpp *LocalPackagesParser) GetDataForProvider(provider string) LocalPackageRoot {
	localPackageRoot := lpp.GetData(false)
	filteredPackages := []LocalPackageItem{}

	for _, item := range localPackageRoot.Packages {
		// Check for new format: provider:pkg
		if strings.HasPrefix(item.SourceID, provider+":") {
			filteredPackages = append(filteredPackages, item)
		}
		// Also check legacy format for backward compatibility (though GetData normalizes)
		if strings.HasPrefix(item.SourceID, "pkg:"+provider+"/") {
			filteredPackages = append(filteredPackages, item)
		}
	}

	return LocalPackageRoot{Packages: filteredPackages}
}

func (lpp *LocalPackagesParser) AddLocalPackage(sourceId string, version string) error {
	return lpp.AddLocalPackageWithCommit(sourceId, version, "")
}

func (lpp *LocalPackagesParser) AddLocalPackageWithCommit(sourceId, version, commit string) error {
	// Normalize the source ID to new format before storing
	normalizedID := normalizePackageID(sourceId)
	localPackageRoot := lpp.GetData(false)
	packageExists := false
	commit = strings.TrimSpace(commit)

	// Check if the package is already installed (compare normalized IDs)
	for i, pkg := range localPackageRoot.Packages {
		if pkg.SourceID == normalizedID {
			if pkg.Version != version && localPackageRoot.Packages[i].Extras != nil {
				localPackageRoot.Packages[i].Extras.TreeSitterExternalQueries = nil
				localPackageRoot.Packages[i].Extras.TreeSitterParserChoices = nil
				localPackageRoot.Packages[i].Extras.TreeSitterQueryChoices = nil
			}
			// Update the existing package with the new version
			localPackageRoot.Packages[i].Version = version
			if commit != "" {
				localPackageRoot.Packages[i].Commit = commit
			} else if pkg.Version != version {
				localPackageRoot.Packages[i].Commit = ""
			}
			packageExists = true
			break
		}
	}

	// If not found, add the new package with normalized ID
	if !packageExists {
		item := LocalPackageItem{
			SourceID: normalizedID,
			Version:  version,
		}
		if commit != "" {
			item.Commit = commit
		}
		localPackageRoot.Packages = append(localPackageRoot.Packages, item)
	}

	localPackageRoot.Schema = lockSchemaURL
	localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
	jsonData, err := marshalIndent(localPackageRoot, "", "  ")
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return err
	}

	if err := lpp.fileManager.WriteFile(localPackagesFile, jsonData, 0644); err != nil {
		fmt.Println("Error writing to file:", err)
		return err
	}
	return nil
}

func (lpp *LocalPackagesParser) RemoveLocalPackage(sourceId string) error {
	// Normalize the source ID to new format before looking up
	normalizedID := normalizePackageID(sourceId)
	localPackageRoot := lpp.GetData(false)
	for i, pkg := range localPackageRoot.Packages {
		if pkg.SourceID == normalizedID {
			localPackageRoot.Packages = append(localPackageRoot.Packages[:i], localPackageRoot.Packages[i+1:]...)
			break
		}
	}

	localPackageRoot.Schema = lockSchemaURL
	localPackagesFile := lpp.fileManager.GetAppLocalPackagesFilePath()
	jsonData, err := marshalIndent(localPackageRoot, "", "  ")
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return err
	}

	if err := lpp.fileManager.WriteFile(localPackagesFile, jsonData, 0644); err != nil {
		fmt.Println("Error writing to file:", err)
		return err
	}
	return nil
}

func (lpp *LocalPackagesParser) GetBySourceId(sourceId string) LocalPackageItem {
	// Normalize the source ID to new format before looking up
	normalizedID := normalizePackageID(sourceId)
	localPackageRoot := lpp.GetData(false)
	for _, item := range localPackageRoot.Packages {
		if item.SourceID == normalizedID {
			return item
		}
	}
	return LocalPackageItem{}
}

func (lpp *LocalPackagesParser) IsPackageInstalled(sourceId string) bool {
	// Normalize the source ID to new format before looking up
	normalizedID := normalizePackageID(sourceId)
	localPackageRoot := lpp.GetData(false)
	for _, item := range localPackageRoot.Packages {
		if item.SourceID == normalizedID {
			return true
		}
	}
	return false
}

// Global instance for backward compatibility
var globalParser *LocalPackagesParser

func init() {
	globalParser = New()
}

// Legacy functions for backward compatibility
func GetData(force bool) LocalPackageRoot {
	return globalParser.GetData(force)
}

func GetDataForProvider(provider string) LocalPackageRoot {
	return globalParser.GetDataForProvider(provider)
}

func AddLocalPackage(sourceId string, version string) error {
	return globalParser.AddLocalPackage(sourceId, version)
}

func AddLocalPackageWithCommit(sourceId, version, commit string) error {
	return globalParser.AddLocalPackageWithCommit(sourceId, version, commit)
}

func RemoveLocalPackage(sourceId string) error {
	return globalParser.RemoveLocalPackage(sourceId)
}

func MergePackageIntegrations(sourceId string, integrations []string) error {
	return globalParser.MergePackageIntegrations(sourceId, integrations)
}

func MergePackageTreeSitterExternalQueryPins(sourceId string, pins []TreeSitterExternalQueryPin) error {
	return globalParser.MergePackageTreeSitterExternalQueryPins(sourceId, pins)
}

func GetBySourceId(sourceId string) LocalPackageItem {
	return globalParser.GetBySourceId(sourceId)
}

func IsPackageInstalled(sourceId string) bool {
	return globalParser.IsPackageInstalled(sourceId)
}

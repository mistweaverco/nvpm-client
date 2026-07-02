package registry_parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
)

// FileReader interface for dependency injection in tests
type FileReader interface {
	ReadFile(filename string) ([]byte, error)
}

// defaultFileReader implements FileReader using the standard library
type defaultFileReader struct{}

func (d *defaultFileReader) ReadFile(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

// RegistryParser handles parsing of registry data
type RegistryParser struct {
	fileReader FileReader
	data       RegistryRoot
	hasData    bool
	bySourceID map[string]RegistryItem
}

// NewRegistryParser creates a new RegistryParser instance
func NewRegistryParser(fileReader FileReader) *RegistryParser {
	return &RegistryParser{
		fileReader: fileReader,
		data:       RegistryRoot{},
		hasData:    false,
	}
}

// NewDefaultRegistryParser creates a RegistryParser with default dependencies
func NewDefaultRegistryParser() *RegistryParser {
	return NewRegistryParser(&defaultFileReader{})
}

// RegistryItemSourceAssetFile can be a string or an array of strings
type RegistryItemSourceAssetFile struct {
	value interface{}
}

func (f *RegistryItemSourceAssetFile) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		f.value = str
		return nil
	}
	// Try array
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		f.value = arr
		return nil
	}
	return fmt.Errorf("cannot unmarshal file: expected string or array")
}

func (f *RegistryItemSourceAssetFile) String() string {
	if str, ok := f.value.(string); ok {
		return str
	}
	if arr, ok := f.value.([]string); ok && len(arr) > 0 {
		return arr[0] // Return first file if it's an array
	}
	return ""
}

func (f *RegistryItemSourceAssetFile) IsArray() bool {
	_, ok := f.value.([]string)
	return ok
}

func (f *RegistryItemSourceAssetFile) GetArray() []string {
	if arr, ok := f.value.([]string); ok {
		return arr
	}
	if str, ok := f.value.(string); ok {
		return []string{str}
	}
	return nil
}

type RegistryItemSourceAsset struct {
	Target interface{}                 `json:"target"` // Can be string or []string
	File   RegistryItemSourceAssetFile `json:"file"`
	Bin    interface{}                 `json:"bin,omitempty"` // Can be string or map[string]string
}

// RegistryItemSourceAssetList is a custom type that can unmarshal both a single object and an array
type RegistryItemSourceAssetList []RegistryItemSourceAsset

// UnmarshalJSON implements custom JSON unmarshaling to handle both single object and array formats
func (a *RegistryItemSourceAssetList) UnmarshalJSON(data []byte) error {
	// Handle null or empty values
	if len(data) == 0 || string(data) == "null" {
		*a = nil
		return nil
	}

	// Try to unmarshal as array first
	var arr []RegistryItemSourceAsset
	if err := json.Unmarshal(data, &arr); err == nil {
		*a = arr
		return nil
	}

	// If that fails, try as a single object
	var obj RegistryItemSourceAsset
	if err := json.Unmarshal(data, &obj); err == nil {
		*a = []RegistryItemSourceAsset{obj}
		return nil
	}

	return fmt.Errorf("cannot unmarshal asset: expected array or object, got: %s", string(data))
}

type RegistryItemSourceDownloadFile struct {
	Target interface{}       `json:"target"` // Can be string or []string
	Files  map[string]string `json:"files"`  // Map of filename -> URL
	Bin    string            `json:"bin,omitempty"`
}

type RegistryItemSourceDownloadList []RegistryItemSourceDownloadFile

// UnmarshalJSON implements custom JSON unmarshaling to handle both single object and array formats
func (d *RegistryItemSourceDownloadList) UnmarshalJSON(data []byte) error {
	// Handle null or empty values
	if len(data) == 0 || string(data) == "null" {
		*d = nil
		return nil
	}

	// Try to unmarshal as array first
	var arr []RegistryItemSourceDownloadFile
	if err := json.Unmarshal(data, &arr); err == nil {
		*d = arr
		return nil
	}

	// If that fails, try as a single object
	var obj RegistryItemSourceDownloadFile
	if err := json.Unmarshal(data, &obj); err == nil {
		*d = []RegistryItemSourceDownloadFile{obj}
		return nil
	}

	return fmt.Errorf("cannot unmarshal download: expected array or object, got: %s", string(data))
}

type RegistryItemSource struct {
	ID       string                         `json:"id"`
	Asset    RegistryItemSourceAssetList    `json:"asset,omitempty"`
	Download RegistryItemSourceDownloadList `json:"download,omitempty"`
}

// RegistryItemTreeSitterExternalQueries points at a separate repository that only
// ships Neovim tree-sitter query files (.scm), mirroring neovim-treesitter's
// nvim-treesitter-queries-* layout (typically a top-level queries/ directory).
type RegistryItemTreeSitterExternalQueries struct {
	RepoURL string `json:"repo_url,omitempty"`
	// Package is an optional registry source id (e.g. github:org/repo) naming a Tree-sitter-queries
	// (or other) package whose git URL is derived for Neovim external query clones.
	Package string `json:"package,omitempty"`
	// Ref is a branch name, tag, or commit to check out after clone. If empty and
	// Semver is false, the remote default branch is used.
	Ref string `json:"ref,omitempty"`
	// Semver, when true and Ref is empty, selects the greatest valid semver tag
	// from the remote (via git ls-remote), matching queries_semver in the
	// neovim-treesitter parser registry.
	Semver bool `json:"semver,omitempty"`
}

// TreeSitterExternalQueriesList unmarshals external_queries as either one object or an array
// (matching package YAML / compiled registry JSON).
type TreeSitterExternalQueriesList []RegistryItemTreeSitterExternalQueries

func (l *TreeSitterExternalQueriesList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*l = nil
		return nil
	}
	if data[0] == '[' {
		var sl []RegistryItemTreeSitterExternalQueries
		if err := json.Unmarshal(data, &sl); err != nil {
			return err
		}
		*l = sl
		return nil
	}
	var one RegistryItemTreeSitterExternalQueries
	if err := json.Unmarshal(data, &one); err != nil {
		return err
	}
	*l = []RegistryItemTreeSitterExternalQueries{one}
	return nil
}

type RegistryItemTreeSitterBuild struct {
	Language     string   `json:"language"`
	GrammarDir   string   `json:"grammar_dir"`
	Integrations []string `json:"integrations"`
	// Requires lists other tree-sitter language names needed before this grammar can build.
	Requires []string `json:"requires,omitempty"`
	Inherits []string `json:"inherits,omitempty"`
	// Injections lists host language names used at runtime/editor for injected regions (metadata).
	Injections      []string                      `json:"injections,omitempty"`
	QueriesOnly     bool                          `json:"queries_only,omitempty"`
	ExternalQueries TreeSitterExternalQueriesList `json:"external_queries,omitempty"`
}

type RegistryItemTreeSitter struct {
	Build []RegistryItemTreeSitterBuild `json:"build,omitempty"`
}

// RegistryItemRequires declares package dependencies that must be satisfied before install.
type RegistryItemRequires struct {
	All []string `json:"all,omitempty"`
	One []string `json:"one,omitempty"`
}

func (r *RegistryItemRequires) IsEmpty() bool {
	return r == nil || (len(r.All) == 0 && len(r.One) == 0)
}

type RegistryItem struct {
	Name              string                  `json:"name"`
	Version           string                  `json:"version"`
	PrereleaseVersion string                  `json:"prerelease_version,omitempty"`
	Description       string                  `json:"description"`
	Homepage          string                  `json:"homepage"`
	Licenses          []string                `json:"licenses"`
	Languages         []string                `json:"languages"`
	Categories        []string                `json:"categories"`
	Aliases           []string                `json:"aliases,omitempty"`
	Source            RegistryItemSource      `json:"source"`
	Bin               map[string]string       `json:"bin"`
	TreeSitter        *RegistryItemTreeSitter `json:"treesitter,omitempty"`
	Requires          *RegistryItemRequires   `json:"requires,omitempty"`
}

type RegistryRoot []RegistryItem

func normalizeSourceID(id string) string {
	// New format: "<provider>:<id>"
	if strings.Contains(id, ":") && !strings.HasPrefix(id, "pkg:") {
		return id
	}
	// Legacy format: "pkg:<provider>/<id>"
	if strings.HasPrefix(id, "pkg:") {
		withoutPrefix := strings.TrimPrefix(id, "pkg:")
		parts := strings.SplitN(withoutPrefix, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return parts[0] + ":" + parts[1]
		}
	}
	return id
}

// GetData retrieves registry data, optionally forcing a refresh
func (rp *RegistryParser) GetData(force bool) RegistryRoot {
	if rp.hasData && !force {
		return rp.data
	}

	rp.bySourceID = nil

	// Try to load from the default registry file path
	// This maintains backward compatibility with the old implementation
	registryFile := files.GetAppRegistryFilePath()
	if err := rp.LoadFromFile(registryFile); err != nil {
		// If file loading fails, return empty data
		rp.data = RegistryRoot{}
		rp.hasData = true
		rp.rebuildSourceIDIndex()
	}

	return rp.data
}

func (rp *RegistryParser) rebuildSourceIDIndex() {
	if len(rp.data) == 0 {
		rp.bySourceID = map[string]RegistryItem{}
		return
	}
	rp.bySourceID = make(map[string]RegistryItem, len(rp.data))
	for _, item := range rp.data {
		id := normalizeSourceID(strings.TrimSpace(item.Source.ID))
		if id == "" {
			continue
		}
		rp.bySourceID[id] = item
	}
}

// GetBySourceId finds a registry item by its source ID
func (rp *RegistryParser) GetBySourceId(sourceId string) RegistryItem {
	registryRoot := rp.GetData(false)
	want := normalizeSourceID(sourceId)
	if rp.bySourceID != nil {
		if item, ok := rp.bySourceID[want]; ok {
			return item
		}
	}
	for _, item := range registryRoot {
		if item.Source.ID == sourceId || normalizeSourceID(item.Source.ID) == want {
			return item
		}
	}
	return RegistryItem{}
}

// GetLatestVersion gets the latest version for a given source ID
func (rp *RegistryParser) GetLatestVersion(sourceId string) string {
	item := rp.GetBySourceId(sourceId)
	return item.Version
}

// GetLatestVersions gets the latest stable and prerelease versions for a given source ID.
// The stable version is always returned as the first value (which may be empty),
// and the prerelease version (if any) as the second.
func (rp *RegistryParser) GetLatestVersions(sourceId string) (string, string) {
	item := rp.GetBySourceId(sourceId)
	return item.Version, item.PrereleaseVersion
}

// GetByNameOrAlias finds a registry item by its name or any of its aliases.
// It prioritizes exact name matches over alias matches.
func (rp *RegistryParser) GetByNameOrAlias(name string) RegistryItem {
	registryRoot := rp.GetData(false)

	// First pass: check for exact name matches (prioritize these)
	for _, item := range registryRoot {
		if item.Name == name {
			return item
		}
	}

	// Second pass: check for alias matches only if no name match was found
	for _, item := range registryRoot {
		for _, alias := range item.Aliases {
			if alias == name {
				return item
			}
		}
	}

	return RegistryItem{}
}

// LoadFromBytes loads registry data from JSON bytes
func (rp *RegistryParser) LoadFromBytes(data []byte) error {
	var registry RegistryRoot
	if err := json.Unmarshal(data, &registry); err != nil {
		return fmt.Errorf("failed to parse registry data: %w", err)
	}

	// Sort the registry by name
	sort.Slice(registry, func(i, j int) bool {
		return registry[i].Name < registry[j].Name
	})

	rp.data = registry
	rp.hasData = true
	rp.rebuildSourceIDIndex()
	return nil
}

// LoadFromFile loads registry data from a file
func (rp *RegistryParser) LoadFromFile(filename string) error {
	data, err := rp.fileReader.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read registry file: %w", err)
	}

	return rp.LoadFromBytes(data)
}

// GetDataForTesting returns the current data (useful for testing)
func (rp *RegistryParser) GetDataForTesting() RegistryRoot {
	return rp.data
}

// HasDataForTesting returns whether data has been loaded (useful for testing)
func (rp *RegistryParser) HasDataForTesting() bool {
	return rp.hasData
}

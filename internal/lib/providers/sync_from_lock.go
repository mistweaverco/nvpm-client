package providers

import (
	"fmt"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

func languagesFromTreeSitterBuild(build []registry_parser.RegistryItemTreeSitterBuild) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(build))
	for _, b := range build {
		lang := strings.TrimSpace(b.Language)
		if lang == "" {
			continue
		}
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	return out
}

// SyncAllFromLock syncs packages and replays lockfile-configured integrations.
func SyncAllFromLock() error {
	if err := EnsureLockfilePackageRequires(true); err != nil {
		return err
	}
	lock := local_packages_parser.GetData(false)

	// Keep existing behavior for ensuring packages exist.
	// Integrations are configured per-package and replayed below.
	SetRequestedIntegrations(nil)
	syncAllProviders()

	// Re-apply integrations even when packages are already installed.
	registry := registry_parser.NewDefaultRegistryParser()
	var firstErr error

	for _, pkg := range lock.Packages {
		sourceID := strings.TrimSpace(pkg.SourceID)
		version := strings.TrimSpace(pkg.Version)
		if sourceID == "" || version == "" {
			continue
		}

		var integrations []string
		if pkg.Extras != nil {
			integrations = pkg.Extras.Integrations
		}
		SetRequestedIntegrations(integrations)

		item := registry.GetBySourceId(sourceID)
		if item.Source.ID == "" {
			// Unknown to registry; nothing to integrate.
			continue
		}
		if !IsTreeSitterCategory(item.Categories) {
			continue
		}
		if item.TreeSitter == nil || len(item.TreeSitter.Build) == 0 {
			continue
		}

		langs := languagesFromTreeSitterBuild(item.TreeSitter.Build)
		if err := ensureTreeSitterParserRequirements(item, version); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("apply integrations for %s@%s: %w", sourceID, version, err)
			continue
		}
		if err := ensureNeovimTreeSitterInheritDependencies(item); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("apply integrations for %s@%s: %w", sourceID, version, err)
			continue
		}
		if err := ensureNeovimTreeSitterInjectionQueryPackages(item, version); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("apply integrations for %s@%s: %w", sourceID, version, err)
			continue
		}
		queryOnly := queryOnlyNeovimLanguagesForInstall(item.TreeSitter.Build, langs)
		if err := installNeovimParsersAndQueriesFromCache(item.Source.ID, version, langs, queryOnly); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("apply integrations for %s@%s: %w", sourceID, version, err)
		}
	}

	return firstErr
}

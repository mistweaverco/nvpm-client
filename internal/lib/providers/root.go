package providers

import (
	"fmt"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/log"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/semver"
)

type Provider int

const (
	ProviderNPM Provider = iota
	ProviderPyPi
	ProviderGolang
	ProviderCargo
	ProviderGitHub
	ProviderGitLab
	ProviderCodeberg
	ProviderGem
	ProviderComposer
	ProviderLuaRocks
	ProviderNuGet
	ProviderOpam
	ProviderOpenVSX
	ProviderGeneric
	ProviderUnsupported
)

var Logger = log.NewLogger()

type MinReleaseAgePolicy struct {
	MinAge    time.Duration
	Force     bool
	BypassAll bool
}

var minReleaseAgePolicy = MinReleaseAgePolicy{
	MinAge: 0,
}

func SetMinReleaseAgePolicy(p MinReleaseAgePolicy) {
	minReleaseAgePolicy = p
}

func enforceMinReleaseAge(sourceID, version string) error {
	p := minReleaseAgePolicy
	if p.BypassAll || p.MinAge <= 0 {
		return nil
	}
	if version == "" || version == "latest" {
		// We only gate concrete versions.
		return nil
	}
	trust := PackageAlwaysTrust(sourceID)
	// Always resolve and record discovery - including for --force / always_trust - so
	// tag SHA mismatch detection has history. Those flags only skip the age wait below.
	discoveryVersion, err := discoveryVersionForEnforcement(sourceID, version)
	if err != nil {
		if p.Force || p.BypassAll || trust {
			Logger.Info(fmt.Sprintf("min-release-age: warning: cannot resolve discovery version for %s@%s: %v", sourceID, version, err))
			discoveryVersion = version
		} else {
			return fmt.Errorf("min-release-age: cannot resolve git commit for %s@%s: %w", sourceID, version, err)
		}
	}
	now := time.Now()
	firstSeen, err := getOrSetFirstSeen(sourceID, discoveryVersion, now)
	if err != nil {
		// If we cannot read/write the local discovery DB, fail closed unless explicitly forced.
		if p.Force || p.BypassAll || trust {
			Logger.Info(fmt.Sprintf("min-release-age: warning: cannot persist discovery time: %v", err))
			return nil
		}
		return fmt.Errorf("min-release-age: cannot read discovery database: %w", err)
	}
	if p.Force || trust {
		return nil
	}
	age := now.Sub(firstSeen)
	if age >= p.MinAge {
		return nil
	}
	remaining := p.MinAge - age
	return &MinReleaseAgeTooSoonError{
		SourceID:  sourceID,
		Version:   discoveryVersion,
		Age:       age,
		Remaining: remaining,
	}
}

// Global factory instance - can be replaced for testing
var globalFactory ProviderFactory = &DefaultProviderFactory{}

// SetProviderFactory allows setting a custom factory for testing
func SetProviderFactory(factory ProviderFactory) {
	globalFactory = factory
}

// ResetProviderFactory resets to the default factory
func ResetProviderFactory() {
	globalFactory = &DefaultProviderFactory{}
}

// Get providers from factory
func getNPMProvider() PackageManager {
	return globalFactory.CreateNPMProvider()
}

func getPyPIProvider() PackageManager {
	return globalFactory.CreatePyPIProvider()
}

func getGolangProvider() PackageManager {
	return globalFactory.CreateGolangProvider()
}

func getCargoProvider() PackageManager {
	return globalFactory.CreateCargoProvider()
}

func getGitHubProvider() PackageManager {
	return globalFactory.CreateGitHubProvider()
}

func getGitLabProvider() PackageManager {
	return globalFactory.CreateGitLabProvider()
}

func getCodebergProvider() PackageManager {
	return globalFactory.CreateCodebergProvider()
}

func getGemProvider() PackageManager {
	return globalFactory.CreateGemProvider()
}

func getComposerProvider() PackageManager {
	return globalFactory.CreateComposerProvider()
}

func getLuaRocksProvider() PackageManager {
	return globalFactory.CreateLuaRocksProvider()
}

func getNuGetProvider() PackageManager {
	return globalFactory.CreateNuGetProvider()
}

func getOpamProvider() PackageManager {
	return globalFactory.CreateOpamProvider()
}

func getOpenVSXProvider() PackageManager {
	return globalFactory.CreateOpenVSXProvider()
}

func getGenericProvider() PackageManager {
	return globalFactory.CreateGenericProvider()
}

// AvailableProviders lists all provider names supported by NVPM
var AvailableProviders = []string{
	"npm",
	"pypi",
	"golang",
	"cargo",
	"github",
	"gitlab",
	"codeberg",
	"gem",
	"composer",
	"luarocks",
	"nuget",
	"opam",
	"openvsx",
	"generic",
}

// IsSupportedProvider returns true if the given provider name is supported
func IsSupportedProvider(name string) bool {
	for _, p := range AvailableProviders {
		if p == name {
			return true
		}
	}
	return false
}

// normalizePackageID converts a package ID from legacy format (pkg:provider/pkg)
// to the new format (provider:pkg), or returns it unchanged if already in new format.
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

// extractProviderAndPackage extracts provider and package name from a source ID.
// Supports both legacy (pkg:provider/pkg) and new (provider:pkg) formats.
func extractProviderAndPackage(sourceID string) (string, string) {
	normalized := normalizePackageID(sourceID)
	parts := strings.SplitN(normalized, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func detectProvider(sourceId string) Provider {
	normalized := normalizePackageID(sourceId)
	providerName, _ := extractProviderAndPackage(normalized)
	if providerName == "" {
		return ProviderUnsupported
	}

	switch strings.ToLower(providerName) {
	case "npm":
		return ProviderNPM
	case "pypi":
		return ProviderPyPi
	case "golang":
		return ProviderGolang
	case "cargo":
		return ProviderCargo
	case "github":
		return ProviderGitHub
	case "gitlab":
		return ProviderGitLab
	case "codeberg":
		return ProviderCodeberg
	case "gem":
		return ProviderGem
	case "composer":
		return ProviderComposer
	case "luarocks":
		return ProviderLuaRocks
	case "nuget":
		return ProviderNuGet
	case "opam":
		return ProviderOpam
	case "openvsx":
		return ProviderOpenVSX
	case "generic":
		return ProviderGeneric
	default:
		return ProviderUnsupported
	}
}

// CheckIfUpdateIsAvailable checks if an update is available for a given package
// and returns a boolean indicating if an update is available and the latest version number
func CheckIfUpdateIsAvailable(localVersion string, remoteVersion string) (bool, string) {
	if semver.IsGreater(localVersion, remoteVersion) {
		return true, remoteVersion
	}
	return false, ""
}

func syncAllProviders() {
	npmProvider := getNPMProvider()
	if npm, ok := npmProvider.(*NPMProvider); ok {
		npm.Sync()
	}

	pypiProvider := getPyPIProvider()
	if pypi, ok := pypiProvider.(*PyPiProvider); ok {
		pypi.Sync()
	}

	golangProvider := getGolangProvider()
	if golang, ok := golangProvider.(*GolangProvider); ok {
		golang.Sync()
	}

	cargoProvider := getCargoProvider()
	if cargo, ok := cargoProvider.(*CargoProvider); ok {
		cargo.Sync()
	}

	githubProvider := getGitHubProvider()
	if github, ok := githubProvider.(*GitHubProvider); ok {
		github.Sync()
	}

	gitlabProvider := getGitLabProvider()
	if gitlab, ok := gitlabProvider.(*GitLabProvider); ok {
		gitlab.Sync()
	}

	codebergProvider := getCodebergProvider()
	if codeberg, ok := codebergProvider.(*CodebergProvider); ok {
		codeberg.Sync()
	}

	gemProvider := getGemProvider()
	if gem, ok := gemProvider.(*GemProvider); ok {
		gem.Sync()
	}

	composerProvider := getComposerProvider()
	if composer, ok := composerProvider.(*ComposerProvider); ok {
		composer.Sync()
	}

	luarocksProvider := getLuaRocksProvider()
	if luarocks, ok := luarocksProvider.(*LuaRocksProvider); ok {
		luarocks.Sync()
	}

	nugetProvider := getNuGetProvider()
	if nuget, ok := nugetProvider.(*NuGetProvider); ok {
		nuget.Sync()
	}

	opamProvider := getOpamProvider()
	if opam, ok := opamProvider.(*OpamProvider); ok {
		opam.Sync()
	}

	openvsxProvider := getOpenVSXProvider()
	if openvsx, ok := openvsxProvider.(*OpenVSXProvider); ok {
		openvsx.Sync()
	}

	genericProvider := getGenericProvider()
	if generic, ok := genericProvider.(*GenericProvider); ok {
		generic.Sync()
	}
}

// ResolveVersion resolves the version for a given sourceID.
// If version is empty or "latest", it will query the provider for the latest version.
// Otherwise, it returns the provided version as-is.
func ResolveVersion(sourceId string, version string) (string, error) {
	if err := CheckSourceIDPrerequisites(sourceId); err != nil {
		return version, err
	}
	if version != "" && version != "latest" {
		return version, nil
	}

	// Prefer the registry version when present for both:
	// - version == "" (user omitted a version)
	// - version == "latest" (user asked for "latest")
	//
	// This keeps installs consistent with the curated registry, instead of always
	// deferring to provider "latest" logic (e.g. GitHub release/tag lookups).
	if version == "" || version == "latest" {
		registry := registry_parser.NewDefaultRegistryParser()
		registryItem := registry.GetBySourceId(sourceId)
		if registryItem.Version != "" {
			return registryItem.Version, nil
		}

		// Non-registry git packages: prefer-branch-over-release via remote discovery.
		if IsGitHostedSourceID(sourceId) {
			if ref, err := ResolveGitLatestRef(sourceId); err == nil && strings.TrimSpace(ref) != "" {
				return strings.TrimSpace(ref), nil
			}
		}
	}

	provider := detectProvider(sourceId)
	_, packageName := extractProviderAndPackage(normalizePackageID(sourceId))
	if packageName == "" {
		return version, nil
	}

	var pkgManager PackageManager
	switch provider {
	case ProviderNPM:
		pkgManager = getNPMProvider()
	case ProviderPyPi:
		pkgManager = getPyPIProvider()
	case ProviderGolang:
		pkgManager = getGolangProvider()
	case ProviderCargo:
		pkgManager = getCargoProvider()
	case ProviderGitHub:
		pkgManager = getGitHubProvider()
	case ProviderGitLab:
		pkgManager = getGitLabProvider()
	case ProviderCodeberg:
		pkgManager = getCodebergProvider()
	case ProviderGem:
		pkgManager = getGemProvider()
	case ProviderComposer:
		pkgManager = getComposerProvider()
	case ProviderLuaRocks:
		pkgManager = getLuaRocksProvider()
	case ProviderNuGet:
		pkgManager = getNuGetProvider()
	case ProviderOpam:
		pkgManager = getOpamProvider()
	case ProviderOpenVSX:
		pkgManager = getOpenVSXProvider()
	case ProviderGeneric:
		// Generic provider gets version from registry
		registry := registry_parser.NewDefaultRegistryParser()
		registryItem := registry.GetBySourceId(sourceId)
		if registryItem.Version != "" {
			return registryItem.Version, nil
		}
		return "latest", nil
	case ProviderUnsupported:
		return version, nil
	default:
		return version, nil
	}

	if pkgManager != nil {
		resolvedVersion, err := pkgManager.getLatestVersion(packageName)
		if err != nil {
			return version, err
		}
		return resolvedVersion, nil
	}

	return version, nil
}

func Install(sourceId string, version string) bool {
	ClearLastError()
	if err := CheckSourceIDPrerequisites(sourceId); err != nil {
		logAndSetError(err.Error())
		return false
	}
	// Tag SHA checks must run before min-release-age recording. Otherwise recording the
	// live tip (especially with always_trust / --force age bypass) would hide a
	// force-moved tag from the mismatch detector on add/set/up.
	if !enforceGitTagSHAOrReject(sourceId, version) {
		return false
	}
	if err := enforceMinReleaseAge(sourceId, version); err != nil {
		if tooSoon, ok := AsMinReleaseAgeTooSoon(err); ok {
			// Still a failed install, but don't spam slog ERROR for a safety wait.
			SetLastError(tooSoon.Error())
			return false
		}
		logAndSetError(err.Error())
		return false
	}
	provider := detectProvider(sourceId)
	var ok bool
	switch provider {
	case ProviderNPM:
		ok = getNPMProvider().Install(sourceId, version)
	case ProviderPyPi:
		ok = getPyPIProvider().Install(sourceId, version)
	case ProviderGolang:
		ok = getGolangProvider().Install(sourceId, version)
	case ProviderCargo:
		ok = getCargoProvider().Install(sourceId, version)
	case ProviderGitHub:
		ok = getGitHubProvider().Install(sourceId, version)
	case ProviderGitLab:
		ok = getGitLabProvider().Install(sourceId, version)
	case ProviderCodeberg:
		ok = getCodebergProvider().Install(sourceId, version)
	case ProviderGem:
		ok = getGemProvider().Install(sourceId, version)
	case ProviderComposer:
		ok = getComposerProvider().Install(sourceId, version)
	case ProviderLuaRocks:
		ok = getLuaRocksProvider().Install(sourceId, version)
	case ProviderNuGet:
		ok = getNuGetProvider().Install(sourceId, version)
	case ProviderOpam:
		ok = getOpamProvider().Install(sourceId, version)
	case ProviderOpenVSX:
		ok = getOpenVSXProvider().Install(sourceId, version)
	case ProviderGeneric:
		ok = getGenericProvider().Install(sourceId, version)
	case ProviderUnsupported:
		return false
	}
	return finishProviderOp(sourceId, ok)
}

func Remove(sourceId string) bool {
	provider := detectProvider(sourceId)
	switch provider {
	case ProviderNPM:
		return getNPMProvider().Remove(sourceId)
	case ProviderPyPi:
		return getPyPIProvider().Remove(sourceId)
	case ProviderGolang:
		return getGolangProvider().Remove(sourceId)
	case ProviderCargo:
		return getCargoProvider().Remove(sourceId)
	case ProviderGitHub:
		return getGitHubProvider().Remove(sourceId)
	case ProviderGitLab:
		return getGitLabProvider().Remove(sourceId)
	case ProviderCodeberg:
		return getCodebergProvider().Remove(sourceId)
	case ProviderGem:
		return getGemProvider().Remove(sourceId)
	case ProviderComposer:
		return getComposerProvider().Remove(sourceId)
	case ProviderLuaRocks:
		return getLuaRocksProvider().Remove(sourceId)
	case ProviderNuGet:
		return getNuGetProvider().Remove(sourceId)
	case ProviderOpam:
		return getOpamProvider().Remove(sourceId)
	case ProviderOpenVSX:
		return getOpenVSXProvider().Remove(sourceId)
	case ProviderGeneric:
		return getGenericProvider().Remove(sourceId)
	case ProviderUnsupported:
		// Unsupported provider
	}
	return false
}

func Update(sourceId string) bool {
	ClearLastError()
	if err := CheckSourceIDPrerequisites(sourceId); err != nil {
		logAndSetError(err.Error())
		return false
	}
	// Enforce min-release-age for updates too (provider Update() implementations call into
	// provider Install() directly, so this must live in the wrapper).
	registry := registry_parser.NewDefaultRegistryParser()
	registryItem := registry.GetBySourceId(sourceId)
	if registryItem.Version != "" {
		// SHA check before age recording - same ordering requirement as Install.
		if !enforceGitTagSHAOrReject(sourceId, registryItem.Version) {
			return false
		}
		if err := enforceMinReleaseAge(sourceId, registryItem.Version); err != nil {
			if tooSoon, ok := AsMinReleaseAgeTooSoon(err); ok {
				// Safety wait: informational skip, not a hard error.
				SetLastSkip(tooSoon.Error())
				return false
			}
			logAndSetError(err.Error())
			return false
		}
	}

	provider := detectProvider(sourceId)
	var ok bool
	switch provider {
	case ProviderNPM:
		ok = getNPMProvider().Update(sourceId)
	case ProviderPyPi:
		ok = getPyPIProvider().Update(sourceId)
	case ProviderGolang:
		ok = getGolangProvider().Update(sourceId)
	case ProviderCargo:
		ok = getCargoProvider().Update(sourceId)
	case ProviderGitHub:
		ok = getGitHubProvider().Update(sourceId)
	case ProviderGitLab:
		ok = getGitLabProvider().Update(sourceId)
	case ProviderCodeberg:
		ok = getCodebergProvider().Update(sourceId)
	case ProviderGem:
		ok = getGemProvider().Update(sourceId)
	case ProviderComposer:
		ok = getComposerProvider().Update(sourceId)
	case ProviderLuaRocks:
		ok = getLuaRocksProvider().Update(sourceId)
	case ProviderNuGet:
		ok = getNuGetProvider().Update(sourceId)
	case ProviderOpam:
		ok = getOpamProvider().Update(sourceId)
	case ProviderOpenVSX:
		ok = getOpenVSXProvider().Update(sourceId)
	case ProviderGeneric:
		ok = getGenericProvider().Update(sourceId)
	case ProviderUnsupported:
		return false
	}
	return finishProviderOp(sourceId, ok)
}

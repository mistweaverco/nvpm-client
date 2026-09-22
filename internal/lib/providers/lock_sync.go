package providers

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
)

// InstalledMatchesLock reports whether pkg is already present on disk at the lockfile
// version (and git commit, when set). Used to skip redundant sync/install work.
func InstalledMatchesLock(pkg local_packages_parser.LocalPackageItem) bool {
	id := strings.TrimSpace(pkg.SourceID)
	ver := strings.TrimSpace(pkg.Version)
	if id == "" || ver == "" {
		return false
	}
	switch detectProvider(id) {
	case ProviderGitHub, ProviderGitLab, ProviderCodeberg:
		return gitInstalledMatchesLock(pkg)
	case ProviderNPM:
		p := NewProviderNPM()
		name := p.getRepo(id)
		return name != "" && p.isPackageInstalled(name, ver)
	case ProviderPyPi:
		p := NewProviderPyPi()
		name := p.getRepo(id)
		if name == "" {
			return false
		}
		installed := p.getInstalledPackagesIn(p.packageDir(name))
		return p.isDistributionInstalled(installed, name, ver)
	case ProviderCargo:
		p := NewProviderCargo()
		crate := p.getRepo(id)
		if crate == "" {
			return false
		}
		got, ok := p.getInstalledCrates()[crate]
		return ok && got == ver
	case ProviderGolang:
		p := NewProviderGolang()
		name := p.getRepo(id)
		if name == "" {
			return false
		}
		binPath := filepath.Join(p.APP_PACKAGES_DIR, "bin", filepath.Base(name))
		fi, err := goStat(binPath)
		return err == nil && !fi.IsDir()
	case ProviderGem:
		p := NewProviderGem()
		name := p.getRepo(id)
		return name != "" && p.isGemInstalled(name, ver)
	case ProviderComposer:
		p := NewProviderComposer()
		name := p.getRepo(id)
		return name != "" && p.isComposerPackageInstalled(name)
	case ProviderLuaRocks:
		p := NewProviderLuaRocks()
		name := p.getRepo(id)
		return name != "" && p.isLuaRockInstalled(name)
	case ProviderNuGet:
		p := NewProviderNuGet()
		name := p.getRepo(id)
		return name != "" && p.isNuGetToolInstalled(name)
	case ProviderOpam:
		p := NewProviderOpam()
		name := p.getRepo(id)
		return name != "" && p.isOpamPackageInstalled(name)
	case ProviderOpenVSX:
		p := NewProviderOpenVSX()
		repo := p.getRepo(id)
		parts := strings.Split(repo, "/")
		if len(parts) != 2 {
			return false
		}
		_, err := openvsxStat(p.getExtensionPath(parts[0], parts[1]))
		return err == nil
	case ProviderGeneric:
		p := NewProviderGeneric()
		name := p.getRepo(id)
		if name == "" {
			return false
		}
		_, err := genericStat(filepath.Join(p.APP_PACKAGES_DIR, name))
		return err == nil
	default:
		return false
	}
}

func gitInstalledMatchesLock(pkg local_packages_parser.LocalPackageItem) bool {
	commit := strings.TrimSpace(pkg.Commit)
	if commit == "" {
		return false
	}
	repoPath := gitInstalledRepoPath(pkg.SourceID)
	if repoPath == "" {
		return false
	}
	if _, err := os.Stat(repoPath); err != nil {
		return false
	}
	if !gitWorkTreeExists(repoPath) {
		return false
	}
	return gitHEADMatchesCommit(repoPath, commit)
}

func gitInstalledRepoPath(sourceID string) string {
	switch detectProvider(sourceID) {
	case ProviderGitHub:
		p := NewProviderGitHub()
		repo := p.getRepo(sourceID)
		if repo == "" {
			return ""
		}
		return p.getRepoPath(sourceID, repo)
	case ProviderGitLab:
		p := NewProviderGitLab()
		repo := p.getRepo(sourceID)
		if repo == "" {
			return ""
		}
		return p.getRepoPath(sourceID, repo)
	case ProviderCodeberg:
		p := NewProviderCodeberg()
		repo := p.getRepo(sourceID)
		if repo == "" {
			return ""
		}
		return p.getRepoPath(sourceID, repo)
	default:
		return ""
	}
}

// resolveNestedDependencyInstallSpec picks version and commit for a nested package install.
// Lockfile pins win; otherwise ResolveVersion is used (same as nvpm install).
func resolveNestedDependencyInstallSpec(sourceID string) (version, commit string) {
	locked := local_packages_parser.GetBySourceId(sourceID)
	if strings.TrimSpace(locked.Version) != "" {
		return strings.TrimSpace(locked.Version), strings.TrimSpace(locked.Commit)
	}
	ver, err := ResolveVersion(sourceID, "")
	if err != nil || strings.TrimSpace(ver) == "" {
		return "latest", ""
	}
	return strings.TrimSpace(ver), ""
}

func nestedDependencyAlreadyInstalled(sourceID string) bool {
	pkg := local_packages_parser.GetBySourceId(sourceID)
	if strings.TrimSpace(pkg.SourceID) == "" {
		return false
	}
	return InstalledMatchesLock(pkg)
}

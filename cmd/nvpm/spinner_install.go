package nvpm

import (
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
)

// runNvpmInstallWithTreeSitterSpinnerPhases runs GitHub + Neovim + tree-sitter installs in two
// steps: interactive prefights (clone, inherit deps, external query consent) without a spinner,
// then the install action behind spinnerutil.Run. Phased GitHub Neovim tree-sitter installs use
// separate spinners for parser build, per-language external query clones, and final registration.
func runNvpmInstallWithTreeSitterSpinnerPhases(
	title string,
	sourceID string,
	resolvedVersion string,
	registryItem registry_parser.RegistryItem,
	installFn func() bool,
) (success bool, err error) {
	// Top-level registry requires (requires.all / requires.one) must run before any
	// tree-sitter phased or provider install path (e.g. tree-sitter-cli for GitHub grammars).
	if e := providers.PreflightPackageRequires(registryItem); e != nil {
		return false, e
	}
	if providers.GitHubTreeSitterUsesPhasedInteractiveInstall(sourceID, registryItem) {
		if e := providers.GitHubTreeSitterPreflightInteractive(sourceID, resolvedVersion); e != nil {
			return false, e
		}
		buildTitle := strings.TrimSuffix(title, "...") + " (tree-sitter build)..."
		var buildOK bool
		err = spinnerutil.Run(buildTitle, func() {
			buildOK = providers.GitHubTreeSitterPhaseBuildParsers(sourceID, resolvedVersion)
		})
		if err != nil || !buildOK {
			return false, err
		}
		if !providers.GitHubTreeSitterPhaseCacheNeovimQueries(sourceID, resolvedVersion) {
			return false, nil
		}
		err = spinnerutil.Run(title, func() {
			success = providers.GitHubTreeSitterPhaseRegisterPackage(sourceID, resolvedVersion)
		})
		return success, err
	}

	if e := providers.PreflightTreeSitterParserRequirements(registryItem, resolvedVersion); e != nil {
		return false, e
	}
	if e := providers.PreflightNeovimTreeSitterInheritDeps(registryItem); e != nil {
		return false, e
	}
	if e := providers.PreflightTreeSitterInjectionQueryPackages(registryItem, resolvedVersion); e != nil {
		return false, e
	}
	err = spinnerutil.Run(title, func() {
		success = installFn()
	})
	return success, err
}

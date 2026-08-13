package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

// pendingPostInstall records preflight consent keyed by normalized source ID.
// true: run the script; false: skip it.
var pendingPostInstall = map[string]bool{}

var postInstallShellOut = shell_out.ShellOut
var postInstallConfirmHook = defaultPostInstallConfirm
var postInstallRegistryParser = registry_parser.NewDefaultRegistryParser

func postInstallKey(sourceID string) string {
	return normalizePackageID(strings.TrimSpace(sourceID))
}

func postInstallScript(item registry_parser.RegistryItem) string {
	return item.PostInstallRun()
}

// PreflightPostInstall asks to run a registry post-install script before the
// install spinner. Always-trusted packages skip the prompt and are approved.
// Returning an error aborts the install (e.g. the user cancelled the form).
func PreflightPostInstall(item registry_parser.RegistryItem) error {
	run := postInstallScript(item)
	if run == "" {
		return nil
	}
	sourceID := postInstallKey(item.Source.ID)
	if sourceID == "" {
		return nil
	}
	if PackageAlwaysTrust(sourceID) {
		pendingPostInstall[sourceID] = true
		return nil
	}
	ok, err := postInstallConfirmHook(sourceID, run)
	if err != nil {
		return err
	}
	pendingPostInstall[sourceID] = ok
	return nil
}

// PreflightPostInstallForSource loads the registry item and runs PreflightPostInstall.
func PreflightPostInstallForSource(sourceID string) error {
	item := postInstallRegistryParser().GetBySourceId(sourceID)
	return PreflightPostInstall(item)
}

// ExecutePostInstall runs the registry post-install script in the package
// install directory after a successful install/update. Consent comes from
// PreflightPostInstall when present; otherwise the same confirm/trust rules apply.
func ExecutePostInstall(sourceID string) error {
	item := postInstallRegistryParser().GetBySourceId(sourceID)
	run := postInstallScript(item)
	if run == "" {
		return nil
	}
	key := postInstallKey(sourceID)
	allowed, decided := pendingPostInstall[key]
	delete(pendingPostInstall, key)
	if !decided {
		if PackageAlwaysTrust(key) {
			allowed = true
		} else {
			ok, err := postInstallConfirmHook(key, run)
			if err != nil {
				return err
			}
			allowed = ok
		}
	}
	if !allowed {
		Logger.Info(fmt.Sprintf("post-install: skipping script for %s (not confirmed)", key))
		return nil
	}
	dir := packageInstallDir(sourceID)
	if dir == "" {
		return fmt.Errorf("post-install: cannot resolve install directory for %s", key)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("post-install: create directory %s: %w", dir, err)
	}
	Logger.Info(fmt.Sprintf("post-install: running script for %s in %s", key, dir))
	code, err := postInstallShellOut("sh", []string{"-c", run}, dir, nil)
	if err != nil || code != 0 {
		return fmt.Errorf("post-install failed for %s (exit %d): %v", key, code, err)
	}
	return nil
}

func defaultPostInstallConfirm(sourceID, script string) (bool, error) {
	title := fmt.Sprintf("Run post-install script for %s?", sourceID)
	description := fmt.Sprintf("This package wants to run the following command in its install directory:\n\n%s", script)
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return false, fmt.Errorf("%s\n%s\n\nNon-interactive session: re-run with --always-trust to allow this script", title, description)
	}
	proceed := true
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(description).
				Value(&proceed).
				Affirmative("Yes").
				Negative("No"),
		),
	)
	if err := form.Run(); err != nil {
		return false, err
	}
	return proceed, nil
}

func packageInstallDir(sourceID string) string {
	switch detectProvider(sourceID) {
	case ProviderNPM:
		p := NewProviderNPM()
		return p.packageDir(p.getRepo(sourceID))
	case ProviderPyPi:
		p := NewProviderPyPi()
		return p.packageDir(p.getRepo(sourceID))
	case ProviderGem:
		p := NewProviderGem()
		return p.packageDir(p.getRepo(sourceID))
	case ProviderComposer:
		p := NewProviderComposer()
		return p.packageDir(p.getRepo(sourceID))
	case ProviderLuaRocks:
		p := NewProviderLuaRocks()
		return p.packageDir(p.getRepo(sourceID))
	case ProviderNuGet:
		p := NewProviderNuGet()
		return p.packageDir(p.getRepo(sourceID))
	case ProviderOpam:
		p := NewProviderOpam()
		return p.packageDir(p.getRepo(sourceID))
	case ProviderGolang:
		p := NewProviderGolang()
		return p.APP_PACKAGES_DIR
	case ProviderCargo:
		p := NewProviderCargo()
		return p.APP_PACKAGES_DIR
	case ProviderGitHub:
		p := NewProviderGitHub()
		return p.getRepoPath(sourceID, p.getRepo(sourceID))
	case ProviderGitLab:
		p := NewProviderGitLab()
		return p.getRepoPath(sourceID, p.getRepo(sourceID))
	case ProviderCodeberg:
		p := NewProviderCodeberg()
		return p.getRepoPath(sourceID, p.getRepo(sourceID))
	case ProviderOpenVSX:
		p := NewProviderOpenVSX()
		repo := p.getRepo(sourceID)
		parts := strings.Split(repo, "/")
		if len(parts) != 2 {
			return filepath.Join(p.APP_PACKAGES_DIR, strings.ReplaceAll(repo, "/", "_"))
		}
		return p.getExtensionPath(parts[0], parts[1])
	case ProviderGeneric:
		p := NewProviderGeneric()
		return filepath.Join(p.APP_PACKAGES_DIR, p.getRepo(sourceID))
	default:
		return ""
	}
}

func finishProviderOp(sourceID string, ok bool) bool {
	if !ok {
		return false
	}
	if err := ExecuteRegistryInstallHooks(sourceID); err != nil {
		logAndSetError(err.Error())
		return false
	}
	return true
}

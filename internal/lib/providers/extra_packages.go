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

type extraPackage struct {
	Spec     string
	Provider string
	Name     string
	Version  string
}

// pendingExtraPackages records preflight consent keyed by normalized source ID.
// true: install extras; false: skip them.
var pendingExtraPackages = map[string]bool{}

var extraPackagesShellOut = shell_out.ShellOut
var extraPackagesConfirmHook = defaultExtraPackagesConfirm

func extraPackagesKey(sourceID string) string {
	return postInstallKey(sourceID)
}

func extraPackagesFromItem(item registry_parser.RegistryItem) []string {
	out := make([]string, 0, len(item.Source.ExtraPackages))
	for _, spec := range item.Source.ExtraPackages {
		spec = strings.TrimSpace(spec)
		if spec != "" {
			out = append(out, spec)
		}
	}
	return out
}

func parseExtraPackages(item registry_parser.RegistryItem) ([]extraPackage, error) {
	specs := extraPackagesFromItem(item)
	if len(specs) == 0 {
		return nil, nil
	}
	out := make([]extraPackage, 0, len(specs))
	for _, spec := range specs {
		provider, name, version, err := parseExtraPackageSpec(spec, item.Source.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, extraPackage{Spec: spec, Provider: provider, Name: name, Version: version})
	}
	return out, nil
}

// parseExtraPackageSpec accepts:
//   - provider:name[@version]          e.g. npm:typescript@6.0.3
//   - provider:@scope/name[@version]   e.g. npm:@astrojs/ts-plugin
//   - name[@version]                   e.g. typescript@5.4.5 (provider inferred from parent)
//   - @scope/name[@version]            e.g. @astrojs/ts-plugin
func parseExtraPackageSpec(spec, parentSourceID string) (provider, name, version string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", "", fmt.Errorf("empty extra_packages entry")
	}
	parentProvider, _ := extractProviderAndPackage(normalizePackageID(parentSourceID))
	if extraPackageHasProviderPrefix(spec) {
		sourceID, ver, parseErr := parseRequirePackageRef(spec)
		if parseErr != nil {
			return "", "", "", fmt.Errorf("invalid extra_packages entry %q: %w", spec, parseErr)
		}
		provider, name = extractProviderAndPackage(sourceID)
		if provider == "" || name == "" {
			return "", "", "", fmt.Errorf("invalid extra_packages entry %q", spec)
		}
		if parentProvider != "" && !strings.EqualFold(provider, parentProvider) {
			return "", "", "", fmt.Errorf("extra package %q uses provider %s but parent package is %s; extras must install into the parent package container", spec, provider, parentProvider)
		}
		return provider, name, ver, nil
	}
	if parentProvider == "" {
		return "", "", "", fmt.Errorf("extra package %q has no provider prefix and parent source %q has no provider", spec, parentSourceID)
	}
	name, version = splitRequirePackageVersion(spec)
	if name == "" {
		return "", "", "", fmt.Errorf("invalid extra_packages entry %q", spec)
	}
	return parentProvider, name, version, nil
}

func extraPackageHasProviderPrefix(spec string) bool {
	if spec == "" || strings.HasPrefix(spec, "@") {
		return false
	}
	provider, rest := extractProviderAndPackage(spec)
	if provider == "" || rest == "" {
		return false
	}
	return detectProvider(provider+":x") != ProviderUnsupported
}

func formatExtraPackageRef(pkg extraPackage) string {
	ref := pkg.Provider + ":" + pkg.Name
	if pkg.Version != "" {
		ref += "@" + pkg.Version
	}
	return ref
}

// PreflightRegistryInstallHooks confirms extra packages then post-install before
// the install spinner. Always-trusted packages skip prompts.
func PreflightRegistryInstallHooks(item registry_parser.RegistryItem) error {
	if err := PreflightExtraPackages(item); err != nil {
		return err
	}
	return PreflightPostInstall(item)
}

// PreflightRegistryInstallHooksForSource loads the registry item and runs
// PreflightRegistryInstallHooks.
func PreflightRegistryInstallHooksForSource(sourceID string) error {
	item := postInstallRegistryParser().GetBySourceId(sourceID)
	return PreflightRegistryInstallHooks(item)
}

// ExecuteRegistryInstallHooks installs extra packages into the parent container,
// then runs the post-install script.
func ExecuteRegistryInstallHooks(sourceID string) error {
	if err := ExecuteExtraPackages(sourceID); err != nil {
		return err
	}
	return ExecutePostInstall(sourceID)
}

// PreflightExtraPackages asks to install registry extra_packages before the
// install spinner. Always-trusted packages skip the prompt and are approved.
func PreflightExtraPackages(item registry_parser.RegistryItem) error {
	pkgs, err := parseExtraPackages(item)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return nil
	}
	sourceID := extraPackagesKey(item.Source.ID)
	if sourceID == "" {
		return nil
	}
	if PackageAlwaysTrust(sourceID) {
		pendingExtraPackages[sourceID] = true
		return nil
	}
	ok, err := extraPackagesConfirmHook(sourceID, pkgs)
	if err != nil {
		return err
	}
	pendingExtraPackages[sourceID] = ok
	return nil
}

// ExecuteExtraPackages installs source.extra_packages into the parent package
// container after a successful install/update. They are not added as separate
// nvpm lock packages. Consent comes from PreflightExtraPackages when present.
func ExecuteExtraPackages(sourceID string) error {
	item := postInstallRegistryParser().GetBySourceId(sourceID)
	pkgs, err := parseExtraPackages(item)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return nil
	}
	key := extraPackagesKey(sourceID)
	allowed, decided := pendingExtraPackages[key]
	delete(pendingExtraPackages, key)
	if !decided {
		if PackageAlwaysTrust(key) {
			allowed = true
		} else {
			ok, confirmErr := extraPackagesConfirmHook(key, pkgs)
			if confirmErr != nil {
				return confirmErr
			}
			allowed = ok
		}
	}
	if !allowed {
		Logger.Info(fmt.Sprintf("extra-packages: skipping extras for %s (not confirmed)", key))
		return nil
	}
	dir := packageInstallDir(sourceID)
	if dir == "" {
		return fmt.Errorf("extra-packages: cannot resolve install directory for %s", key)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("extra-packages: create directory %s: %w", dir, err)
	}
	for _, pkg := range pkgs {
		Logger.Info(fmt.Sprintf("extra-packages: installing %s into %s", formatExtraPackageRef(pkg), dir))
		if err := installExtraPackage(sourceID, dir, pkg); err != nil {
			return err
		}
	}
	return nil
}

func installExtraPackage(parentSourceID, dir string, pkg extraPackage) error {
	cmd, args, cwd, env, err := extraPackageInstallArgs(parentSourceID, dir, pkg)
	if err != nil {
		return err
	}
	code, err := extraPackagesShellOut(cmd, args, cwd, env)
	if err != nil || code != 0 {
		return fmt.Errorf("extra-packages: failed to install %s into %s (exit %d): %v", formatExtraPackageRef(pkg), dir, code, err)
	}
	return nil
}

func extraPackageInstallArgs(parentSourceID, dir string, pkg extraPackage) (cmd string, args []string, cwd string, env []string, err error) {
	switch strings.ToLower(pkg.Provider) {
	case "npm":
		spec := pkg.Name
		if pkg.Version != "" {
			spec = pkg.Name + "@" + pkg.Version
		}
		return "npm", []string{"install", "--no-update-notifier", spec}, dir, npmQuietEnv(), nil
	case "pypi":
		spec := pkg.Name
		if pkg.Version != "" {
			spec = pkg.Name + "==" + pkg.Version
		}
		return pipCmd, []string{"install", spec, "--prefix", dir}, dir, nil, nil
	case "gem":
		args := []string{"install", pkg.Name, "--install-dir", dir, "--no-document", "--no-user-install"}
		if pkg.Version != "" && pkg.Version != "latest" {
			args = append(args, "--version", pkg.Version)
		}
		return gemCmd, args, "", nil, nil
	case "composer":
		spec := pkg.Name
		if pkg.Version != "" && pkg.Version != "latest" {
			spec = pkg.Name + ":" + pkg.Version
		}
		return composerCmd, []string{"require", spec, "--no-interaction", "--no-plugins", "--no-scripts"}, dir, nil, nil
	case "luarocks":
		args := []string{"install", pkg.Name}
		if pkg.Version != "" && pkg.Version != "latest" {
			args = append(args, pkg.Version)
		}
		args = append(args, "--tree", dir)
		return luarocksCmd, args, "", nil, nil
	case "nuget":
		args := []string{"tool", "install", pkg.Name, "--tool-path", dir}
		if pkg.Version != "" && pkg.Version != "latest" {
			args = append(args, "--version", pkg.Version)
		}
		return nugetCmd, args, dir, nil, nil
	case "opam":
		parentName := NewProviderOpam().getRepo(parentSourceID)
		switchPath := NewProviderOpam().switchPath(parentName)
		spec := pkg.Name
		if pkg.Version != "" && pkg.Version != "latest" {
			spec = pkg.Name + "." + pkg.Version
		}
		return opamCmd, []string{"install", spec, "--switch", switchPath, "--yes", "--no-depexts"}, "", nil, nil
	case "golang":
		spec := pkg.Name + "@latest"
		if pkg.Version != "" && pkg.Version != "latest" {
			spec = pkg.Name + "@" + pkg.Version
		}
		gobin := filepath.Join(dir, "bin")
		return "go", []string{"install", spec}, os.TempDir(), []string{"GOBIN=" + gobin}, nil
	case "cargo":
		args := []string{"install", pkg.Name, "--root", dir}
		if pkg.Version != "" && pkg.Version != "latest" {
			args = append(args, "--version", pkg.Version)
		}
		return "cargo", args, dir, []string{"CARGO_HOME=" + dir}, nil
	default:
		return "", nil, "", nil, fmt.Errorf("extra-packages: provider %s does not support extra packages", pkg.Provider)
	}
}

func defaultExtraPackagesConfirm(sourceID string, pkgs []extraPackage) (bool, error) {
	title := fmt.Sprintf("Install extra packages for %s?", sourceID)
	var b strings.Builder
	b.WriteString("This package wants to install the following extra packages into its own install directory (not as separate nvpm packages):\n")
	for _, pkg := range pkgs {
		b.WriteString("\n  • ")
		b.WriteString(formatExtraPackageRef(pkg))
	}
	description := b.String()
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return false, fmt.Errorf("%s\n%s\n\nNon-interactive session: re-run with --always-trust to allow these extra packages", title, description)
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

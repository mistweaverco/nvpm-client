package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

type extraPackage struct {
	Spec     string
	Provider string
	Name     string
	Version  string
	Commit   string
}

// pendingExtraPackages records preflight consent keyed by normalized source ID.
// true: run extras; false: skip them.
var pendingExtraPackages = map[string]bool{}

// pendingExtraPackageResolved holds extras (with resolved versions) from preflight.
var pendingExtraPackageResolved = map[string][]extraPackage{}

var extraPackagesShellOut = shell_out.ShellOut
var extraPackagesConfirmHook = defaultExtraPackagesConfirm
var extraPackageResolveVersion = defaultExtraPackageResolveVersion
var extraPackagesLockItem = local_packages_parser.GetBySourceId
var extraPackagesSavePins = local_packages_parser.MergePackageExtraPackages

// extraPackagesLastInstalled is set by ExecuteExtraPackages when extras were
// actually installed (not skipped, declined, or absent).
var extraPackagesLastInstalled bool

// ConsumeExtraPackagesInstalledLastOp reports whether the last ExecuteExtraPackages
// call installed extras, then clears the flag.
func ConsumeExtraPackagesInstalledLastOp() bool {
	v := extraPackagesLastInstalled
	extraPackagesLastInstalled = false
	return v
}

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
		pkg := extraPackage{Spec: spec, Provider: provider, Name: name, Version: version}
		applyExtraPackageGitSHA(&pkg)
		out = append(out, pkg)
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

func extraPackageID(pkg extraPackage) string {
	return pkg.Provider + ":" + pkg.Name
}

func extraPackageLooksLikeGitSHA(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func extraPackageVersionsEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if strings.EqualFold(a, b) {
		return true
	}
	return strings.EqualFold(strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v"))
}

func extraPackageCommitsEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func applyExtraPackageGitSHA(pkg *extraPackage) {
	if pkg.Commit == "" && extraPackageLooksLikeGitSHA(pkg.Version) {
		pkg.Commit = pkg.Version
	}
}

func resolveExtraPackageVersions(pkgs []extraPackage) []extraPackage {
	out := make([]extraPackage, len(pkgs))
	copy(out, pkgs)
	for i := range out {
		if out[i].Version == "" || strings.EqualFold(out[i].Version, "latest") {
			if ver, err := extraPackageResolveVersion(out[i].Provider, out[i].Name); err == nil {
				ver = strings.TrimSpace(ver)
				if ver != "" && !strings.EqualFold(ver, "latest") {
					out[i].Version = ver
				}
			}
		}
		applyExtraPackageGitSHA(&out[i])
	}
	return out
}

func extraPackagesToPins(pkgs []extraPackage) []local_packages_parser.ExtraPackagePin {
	pins := make([]local_packages_parser.ExtraPackagePin, 0, len(pkgs))
	for _, pkg := range pkgs {
		pins = append(pins, local_packages_parser.ExtraPackagePin{
			ID:      extraPackageID(pkg),
			Version: pkg.Version,
			Commit:  pkg.Commit,
		})
	}
	return pins
}

func extraPackageMatchesPin(pkg extraPackage, pin local_packages_parser.ExtraPackagePin) bool {
	if normalizePackageID(extraPackageID(pkg)) != normalizePackageID(pin.ID) {
		return false
	}
	if pkg.Version != "" && strings.TrimSpace(pin.Version) != "" && !extraPackageVersionsEqual(pkg.Version, pin.Version) {
		return false
	}
	if pkg.Commit != "" && strings.TrimSpace(pin.Commit) != "" && !extraPackageCommitsEqual(pkg.Commit, pin.Commit) {
		return false
	}
	return true
}

func extraPackagesCoveredByLock(sourceID string, desired []extraPackage) bool {
	if len(desired) == 0 {
		return true
	}
	item := extraPackagesLockItem(sourceID)
	if item.Extras == nil || len(item.Extras.ExtraPackages) == 0 {
		return false
	}
	byID := make(map[string]local_packages_parser.ExtraPackagePin, len(item.Extras.ExtraPackages))
	for _, pin := range item.Extras.ExtraPackages {
		id := normalizePackageID(strings.TrimSpace(pin.ID))
		if id == "" {
			continue
		}
		byID[id] = pin
	}
	for _, pkg := range desired {
		pin, ok := byID[normalizePackageID(extraPackageID(pkg))]
		if !ok || !extraPackageMatchesPin(pkg, pin) {
			return false
		}
	}
	return true
}

func defaultExtraPackageResolveVersion(provider, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "npm":
		return NewProviderNPM().getLatestVersion(name)
	case "pypi":
		return NewProviderPyPi().getLatestVersion(name)
	case "gem":
		return NewProviderGem().getLatestVersion(name)
	case "composer":
		return NewProviderComposer().getLatestVersion(name)
	case "luarocks":
		return NewProviderLuaRocks().getLatestVersion(name)
	case "nuget":
		return NewProviderNuGet().getLatestVersion(name)
	case "opam":
		return NewProviderOpam().getLatestVersion(name)
	case "golang":
		return NewProviderGolang().getLatestVersion(name)
	case "cargo":
		return NewProviderCargo().getLatestVersion(name)
	case "github":
		return NewProviderGitHub().getLatestVersion(name)
	case "gitlab":
		return NewProviderGitLab().getLatestVersion(name)
	case "codeberg":
		return NewProviderCodeberg().getLatestVersion(name)
	default:
		return "", nil
	}
}

// RegistryHasInstallHooks reports whether the registry item for sourceID has
// extra_packages or a post-install script that should run after install/update.
func RegistryHasInstallHooks(sourceID string) bool {
	item := postInstallRegistryParser().GetBySourceId(sourceID)
	if item.PostInstallRun() != "" {
		return true
	}
	pkgs, err := parseExtraPackages(item)
	return err == nil && len(pkgs) > 0
}

// StubExtraPackageInstallForTest replaces extra-package shell-out, version
// resolution, and confirm. Call the returned restore function from t.Cleanup.
func StubExtraPackageInstallForTest(
	shellOut func(cmd string, args []string, dir string, env []string) (int, error),
	resolve func(provider, name string) (string, error),
) (restore func()) {
	oldShell := extraPackagesShellOut
	oldResolve := extraPackageResolveVersion
	oldConfirm := extraPackagesConfirmHook
	if shellOut != nil {
		extraPackagesShellOut = shellOut
	}
	if resolve != nil {
		extraPackageResolveVersion = resolve
	}
	extraPackagesConfirmHook = func(string, []extraPackage) (bool, error) {
		return true, nil
	}
	return func() {
		extraPackagesShellOut = oldShell
		extraPackageResolveVersion = oldResolve
		extraPackagesConfirmHook = oldConfirm
	}
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
// install spinner. Always-trusted packages and extras already recorded in the
// lock (same id/version/commit) skip the prompt and are approved.
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
	pkgs = resolveExtraPackageVersions(pkgs)
	pendingExtraPackageResolved[sourceID] = pkgs
	if PackageAlwaysTrust(sourceID) || extraPackagesCoveredByLock(sourceID, pkgs) {
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
// Successful installs are recorded on the parent lock extras.extra_packages.
func ExecuteExtraPackages(sourceID string) error {
	extraPackagesLastInstalled = false
	item := postInstallRegistryParser().GetBySourceId(sourceID)
	pkgs, err := parseExtraPackages(item)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return nil
	}
	key := extraPackagesKey(sourceID)
	if resolved, ok := pendingExtraPackageResolved[key]; ok {
		pkgs = resolved
		delete(pendingExtraPackageResolved, key)
	} else {
		pkgs = resolveExtraPackageVersions(pkgs)
	}
	allowed, decided := pendingExtraPackages[key]
	delete(pendingExtraPackages, key)
	if !decided {
		if PackageAlwaysTrust(key) || extraPackagesCoveredByLock(key, pkgs) {
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
		_ = extraPackagesSavePins(key, nil)
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
			_ = extraPackagesSavePins(key, nil)
			return err
		}
	}
	if err := extraPackagesSavePins(key, extraPackagesToPins(pkgs)); err != nil {
		return fmt.Errorf("extra-packages: record lock extras for %s: %w", key, err)
	}
	extraPackagesLastInstalled = true
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

package providers

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
)

// packageRequiresIsInstalled is injectable for tests.
var packageRequiresIsInstalled = local_packages_parser.IsPackageInstalled

// packageRequiresLockData is injectable for tests.
var packageRequiresLockData = local_packages_parser.GetData

// packageRequiresNewRegistry is injectable for tests.
var packageRequiresNewRegistry = registry_parser.NewDefaultRegistryParser

// packageRequiresInstallFn is injectable for tests.
var packageRequiresInstallFn = func(sourceID, version string) bool {
	return Install(sourceID, version)
}

// packageRequiresResolveVersion is injectable for tests.
var packageRequiresResolveVersion = ResolveVersion

// packageRequiresPrompt is swapped in tests to avoid interactive huh.
var packageRequiresPrompt = defaultPackageRequiresPrompt

type packageRequiresPromptAction string

const (
	packageRequiresAbort   packageRequiresPromptAction = "abort"
	packageRequiresInstall packageRequiresPromptAction = "install"
)

func defaultPackageRequiresPrompt(title, description string) (packageRequiresPromptAction, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return packageRequiresAbort, fmt.Errorf("%s\n%s", title, description)
	}
	var choice packageRequiresPromptAction = packageRequiresInstall
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[packageRequiresPromptAction]().
				Title(title).
				Description(description).
				Options(
					huh.NewOption("Install missing dependencies", packageRequiresInstall),
					huh.NewOption("Abort", packageRequiresAbort),
				).
				Value(&choice),
		),
	)
	if err := form.Run(); err != nil {
		return packageRequiresAbort, err
	}
	return choice, nil
}

// packageRequiresOnePicker selects one package when a requires.one group is unsatisfied.
var packageRequiresOnePicker = defaultPackageRequiresOnePicker

func defaultPackageRequiresOnePicker(title string, options []string) (string, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return "", fmt.Errorf("%s: choose one of: %s", title, strings.Join(options, ", "))
	}
	var choice string
	opts := make([]huh.Option[string], 0, len(options))
	for _, o := range options {
		opts = append(opts, huh.NewOption(o, o))
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Options(opts...).
				Value(&choice),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return choice, nil
}

// PreflightPackageRequires installs registry-declared dependencies for the package
// before the main install spinner runs (interactive: prompts when deps are missing).
func PreflightPackageRequires(registryItem registry_parser.RegistryItem) error {
	return ensurePackageRequires(registryItem, false)
}

// EnsureLockfilePackageRequires installs registry-declared dependencies for every
// package in nvpm-lock.json. When autoInstall is true, missing deps are installed
// without prompts (used by non-interactive sync).
func EnsureLockfilePackageRequires(autoInstall bool) error {
	lock := packageRequiresLockData(false)
	reg := packageRequiresNewRegistry()
	seen := map[string]struct{}{}
	var combined []string
	for _, pkg := range lock.Packages {
		id := normalizePackageID(strings.TrimSpace(pkg.SourceID))
		if id == "" {
			continue
		}
		order, err := requiresInstallOrder(id, reg, autoInstall)
		if err != nil {
			return err
		}
		for _, depID := range order {
			if _, ok := seen[depID]; ok {
				continue
			}
			seen[depID] = struct{}{}
			combined = append(combined, depID)
		}
	}
	if len(combined) == 0 {
		return nil
	}
	var missing []string
	for _, id := range combined {
		if !packageRequiresIsInstalled(id) {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	if !autoInstall {
		var hint strings.Builder
		for _, id := range missing {
			fmt.Fprintf(&hint, "\n• nvpm install %s", id)
		}
		title := fmt.Sprintf("Missing required package(s) for lockfile: %s", strings.Join(missing, ", "))
		desc := "These dependencies are declared in the registry and must be installed first." + hint.String()
		action, err := packageRequiresPrompt(title, desc)
		if err != nil {
			return err
		}
		if action != packageRequiresInstall {
			return fmt.Errorf("aborted: install required package(s) first%s", hint.String())
		}
	}
	return installRequiredPackages(combined, reg)
}

func ensurePackageRequires(registryItem registry_parser.RegistryItem, autoInstall bool) error {
	if registryItem.Source.ID == "" || registryItem.Requires == nil || registryItem.Requires.IsEmpty() {
		return nil
	}
	reg := packageRequiresNewRegistry()
	order, err := requiresInstallOrder(registryItem.Source.ID, reg, autoInstall)
	if err != nil {
		return err
	}
	if len(order) == 0 {
		return nil
	}
	var missing []string
	for _, id := range order {
		if !packageRequiresIsInstalled(id) {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if !autoInstall {
		sort.Strings(missing)
		var hint strings.Builder
		for _, id := range missing {
			fmt.Fprintf(&hint, "\n• nvpm install %s", id)
		}
		title := fmt.Sprintf("Missing required package(s) for %s: %s", registryItem.Name, strings.Join(missing, ", "))
		desc := "These dependencies are declared in the registry and must be installed first." + hint.String()
		action, err := packageRequiresPrompt(title, desc)
		if err != nil {
			return err
		}
		if action != packageRequiresInstall {
			return fmt.Errorf("aborted: install required package(s) first%s", hint.String())
		}
	}
	return installRequiredPackages(order, reg)
}

func installRequiredPackages(order []string, reg *registry_parser.RegistryParser) error {
	for _, id := range order {
		if packageRequiresIsInstalled(id) {
			continue
		}
		ver, err := packageRequiresResolveVersion(id, "")
		if err != nil {
			return fmt.Errorf("resolve version for required package %s: %w", id, err)
		}
		dispVer := strings.TrimSpace(ver)
		if dispVer == "" {
			dispVer = "latest"
		}
		title := fmt.Sprintf("Installing required dependency %s@%s...", id, dispVer)
		var installFailed bool
		action := func() {
			if !packageRequiresInstallFn(id, ver) {
				installFailed = true
			}
		}
		if err := spinnerutil.RunWithTTYOrPlain(title, nil, action); err != nil {
			return err
		}
		if installFailed {
			return fmt.Errorf("failed to install required package %s", id)
		}
		noteTreeSitterDependencyInstallSuccess()
		if !packageRequiresIsInstalled(id) {
			return fmt.Errorf("required package %s was not recorded in the lockfile after install", id)
		}
	}
	return nil
}

// requiresInstallOrder returns transitive registry requires for sourceID in install-first order.
func requiresInstallOrder(sourceID string, reg *registry_parser.RegistryParser, autoInstall bool) ([]string, error) {
	resolving := map[string]bool{}
	resolved := map[string]bool{}
	var order []string
	_, err := collectRequiresInstallOrder(sourceID, reg, autoInstall, resolving, resolved, &order)
	return order, err
}

func collectRequiresInstallOrder(
	sourceID string,
	reg *registry_parser.RegistryParser,
	autoInstall bool,
	resolving map[string]bool,
	resolved map[string]bool,
	order *[]string,
) (bool, error) {
	id := normalizePackageID(sourceID)
	if id == "" {
		return false, nil
	}
	if resolving[id] {
		return false, fmt.Errorf("cyclic package requires involving %s", id)
	}
	item := reg.GetBySourceId(id)
	if item.Source.ID == "" || item.Requires == nil || item.Requires.IsEmpty() {
		return false, nil
	}
	resolving[id] = true
	defer delete(resolving, id)

	deps, err := expandRegistryRequires(item.Requires, reg, autoInstall)
	if err != nil {
		return false, err
	}
	for _, depID := range deps {
		if _, err := collectRequiresInstallOrder(depID, reg, autoInstall, resolving, resolved, order); err != nil {
			return false, err
		}
		if resolved[depID] {
			continue
		}
		*order = append(*order, depID)
		resolved[depID] = true
	}
	return true, nil
}

func expandRegistryRequires(req *registry_parser.RegistryItemRequires, reg *registry_parser.RegistryParser, autoInstall bool) ([]string, error) {
	var deps []string
	seen := map[string]struct{}{}
	add := func(id string) {
		n := normalizePackageID(id)
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		deps = append(deps, n)
	}
	for _, ref := range req.All {
		id, _, err := parseRequirePackageRef(ref)
		if err != nil {
			return nil, err
		}
		add(id)
	}
	if len(req.One) > 0 {
		chosen, err := resolveRequiresOneChoice(req.One, autoInstall)
		if err != nil {
			return nil, err
		}
		if chosen != "" {
			add(chosen)
		}
	}
	sort.Strings(deps)
	return deps, nil
}

func resolveRequiresOneChoice(refs []string, autoInstall bool) (string, error) {
	if requiresOneSatisfied(refs) {
		return "", nil
	}
	var options []string
	for _, ref := range refs {
		id, _, err := parseRequirePackageRef(ref)
		if err != nil {
			return "", err
		}
		options = append(options, normalizePackageID(id))
	}
	sort.Strings(options)
	if autoInstall {
		if len(options) == 0 {
			return "", nil
		}
		return options[0], nil
	}
	title := "Choose one required package to install"
	chosen, err := packageRequiresOnePicker(title, options)
	if err != nil {
		return "", err
	}
	return normalizePackageID(chosen), nil
}

func requiresOneSatisfied(refs []string) bool {
	for _, ref := range refs {
		id, _, err := parseRequirePackageRef(ref)
		if err != nil {
			continue
		}
		if packageRequiresIsInstalled(normalizePackageID(id)) {
			return true
		}
	}
	return false
}

// parseRequirePackageRef splits a registry requires entry into source ID and optional version.
func parseRequirePackageRef(ref string) (sourceID string, version string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("empty package requires reference")
	}
	if strings.HasPrefix(ref, "pkg:") {
		base, ver := splitRequirePackageVersion(ref)
		legacy := strings.TrimPrefix(base, "pkg:")
		parts := strings.SplitN(legacy, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid legacy requires reference %q", ref)
		}
		return normalizePackageID(parts[0] + ":" + parts[1]), ver, nil
	}
	if !strings.Contains(ref, ":") {
		return "", "", fmt.Errorf("invalid requires reference %q: expected <provider>:<package-id>[@version]", ref)
	}
	base, ver := splitRequirePackageVersion(ref)
	return normalizePackageID(base), ver, nil
}

func splitRequirePackageVersion(pkgID string) (string, string) {
	parts := strings.Split(pkgID, "@")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if requireRefVersionLooksValid(last) {
			return strings.Join(parts[:len(parts)-1], "@"), last
		}
	}
	return pkgID, ""
}

func requireRefVersionLooksValid(version string) bool {
	if version == "" {
		return false
	}
	lower := strings.ToLower(version)
	if lower == "latest" || lower == "prerelease" {
		return true
	}
	for _, c := range version {
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

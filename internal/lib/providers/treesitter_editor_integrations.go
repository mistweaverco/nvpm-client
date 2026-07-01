package providers

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

// SupportedTreeSitterEditorIntegrations lists editor IDs for which this client implements
// tree-sitter-parser integration (parser install paths, optional query repos, inherit prompts).
func SupportedTreeSitterEditorIntegrations() []string {
	return []string{"neovim"}
}

func supportedTreeSitterEditorSet() map[string]struct{} {
	m := map[string]struct{}{}
	for _, s := range SupportedTreeSitterEditorIntegrations() {
		m[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}
	return m
}

func treeSitterRegistryUsesLegacyIntegrations(item registry_parser.RegistryItem) bool {
	if item.TreeSitter == nil {
		return false
	}
	for _, b := range item.TreeSitter.Build {
		if len(b.Integrations) > 0 {
			return false
		}
	}
	return len(item.TreeSitter.Build) > 0
}

// TreeSitterDeclaredEditorIntegrations returns the union of integration IDs declared on build rows.
func TreeSitterDeclaredEditorIntegrations(item registry_parser.RegistryItem) map[string]struct{} {
	out := map[string]struct{}{}
	if item.TreeSitter == nil {
		return out
	}
	if treeSitterRegistryUsesLegacyIntegrations(item) {
		out["neovim"] = struct{}{}
		return out
	}
	for _, b := range item.TreeSitter.Build {
		for _, x := range b.Integrations {
			if s := strings.ToLower(strings.TrimSpace(x)); s != "" {
				out[s] = struct{}{}
			}
		}
	}
	return out
}

// TreeSitterBuildDeclaresEditorIntegration reports whether a build row targets the given editor ID.
func TreeSitterBuildDeclaresEditorIntegration(b registry_parser.RegistryItemTreeSitterBuild, editor string) bool {
	editor = strings.ToLower(strings.TrimSpace(editor))
	if editor == "" {
		return false
	}
	if len(b.Integrations) == 0 {
		return editor == "neovim"
	}
	for _, x := range b.Integrations {
		if strings.ToLower(strings.TrimSpace(x)) == editor {
			return true
		}
	}
	return false
}

// TreeSitterBuildDeclaresNeovimIntegration is true when Neovim-specific tree-sitter integration
// may run for this build row (queries, parser copy, external query repos).
func TreeSitterBuildDeclaresNeovimIntegration(b registry_parser.RegistryItemTreeSitterBuild) bool {
	return TreeSitterBuildDeclaresEditorIntegration(b, "neovim")
}

func registryDeclaresNeovimTreeSitterIntegration(item registry_parser.RegistryItem) bool {
	if item.TreeSitter == nil {
		return false
	}
	for _, b := range item.TreeSitter.Build {
		if TreeSitterBuildDeclaresNeovimIntegration(b) {
			return true
		}
	}
	return false
}

// ApplicableTreeSitterIntegrations returns requested names that are both implemented in this client
// and declared on the package's tree-sitter build rows.
func ApplicableTreeSitterIntegrations(item registry_parser.RegistryItem, requested []string) []string {
	supported := supportedTreeSitterEditorSet()
	declared := TreeSitterDeclaredEditorIntegrations(item)
	var out []string
	seen := map[string]struct{}{}
	for _, r := range normalizeIntegrationRequestedSlice(requested) {
		if _, ok := supported[r]; !ok {
			continue
		}
		if _, ok := declared[r]; !ok {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

// RequestedTreeSitterIntegrationsNotImplementedByClient lists --integrate values this client does
// not implement for tree-sitter-parser packages (e.g. future editors).
func RequestedTreeSitterIntegrationsNotImplementedByClient(requested []string) []string {
	supported := supportedTreeSitterEditorSet()
	var out []string
	seen := map[string]struct{}{}
	for _, r := range normalizeIntegrationRequestedSlice(requested) {
		if _, ok := supported[r]; ok {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

func normalizeIntegrationRequestedSlice(requested []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, r := range requested {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "" {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

func formatDeclaredIntegrationNames(item registry_parser.RegistryItem) string {
	m := TreeSitterDeclaredEditorIntegrations(item)
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// TreeSitterIntegrateResolveOpts configures ResolveTreeSitterInstallIntegrations.
type TreeSitterIntegrateResolveOpts struct {
	MachineOutput bool
}

var treeSitterIntegrateMismatchConfirmHook = defaultTreeSitterIntegrateMismatchConfirm

// ResolveTreeSitterInstallIntegrations returns the integration slice to use for this registry item.
// Non–tree-sitter packages return requested unchanged. Tree-sitter packages may prompt when
// --integrate does not match any declared integration for this client.
func ResolveTreeSitterInstallIntegrations(item registry_parser.RegistryItem, requested []string, opts TreeSitterIntegrateResolveOpts) ([]string, error) {
	normalized := normalizeIntegrationRequestedSlice(requested)
	if !IsTreeSitterCategory(item.Categories) || item.TreeSitter == nil || len(item.TreeSitter.Build) == 0 {
		return normalized, nil
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	if bad := RequestedTreeSitterIntegrationsNotImplementedByClient(normalized); len(bad) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: tree-sitter editor integrations are not implemented for: %s (this client supports: %s).\n",
			strings.Join(bad, ", "), strings.Join(SupportedTreeSitterEditorIntegrations(), ", "))
	}
	if app := ApplicableTreeSitterIntegrations(item, normalized); len(app) > 0 {
		return app, nil
	}
	if opts.MachineOutput {
		return nil, fmt.Errorf("no declared tree-sitter integration matches --integrate for this package (declared: %s); omit --integrate or use a supported editor integration",
			formatDeclaredIntegrationNames(item))
	}
	ok, err := treeSitterIntegrateMismatchConfirmHook(item.Source.ID, formatDeclaredIntegrationNames(item), strings.Join(normalized, ", "))
	if err != nil {
		return nil, err
	}
	if ok {
		return nil, nil
	}
	return nil, fmt.Errorf("install cancelled: no matching --integrate for this tree-sitter package")
}

func defaultTreeSitterIntegrateMismatchConfirm(sourceID, declaredStr, requestedStr string) (proceedWithout bool, err error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return false, fmt.Errorf("non-interactive session: no tree-sitter integration matches --integrate=%s for %s (declared: %s). Omit --integrate or use a matching value",
			requestedStr, sourceID, declaredStr)
	}
	proceed := true
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("No matching editor integration").
				Description(fmt.Sprintf(
					"Package %s does not declare integration support that matches your --integrate selection (%s).\n\nDeclared integrations for tree-sitter builds: %s.\n\nProceed and install without editor integrations for this package?",
					sourceID, requestedStr, declaredStr,
				)).
				Value(&proceed).
				Affirmative("Proceed without integrations").
				Negative("Cancel install"),
		),
	)
	if err := form.Run(); err != nil {
		return false, err
	}
	return proceed, nil
}

// FilterLanguagesForNeovimTreeSitterIntegration keeps built language names whose registry build row
// declares Neovim integration (used before Neovim-only install/remove steps).
func FilterLanguagesForNeovimTreeSitterIntegration(build []registry_parser.RegistryItemTreeSitterBuild, langs []string) []string {
	want := map[string]struct{}{}
	for _, l := range langs {
		if s := strings.TrimSpace(l); s != "" {
			want[s] = struct{}{}
		}
	}
	if len(want) == 0 {
		return nil
	}
	var out []string
	for _, b := range build {
		lang := strings.TrimSpace(b.Language)
		if lang == "" {
			continue
		}
		if _, ok := want[lang]; !ok {
			continue
		}
		if TreeSitterBuildDeclaresNeovimIntegration(b) {
			out = append(out, lang)
			delete(want, lang)
		}
	}
	sort.Strings(out)
	return out
}

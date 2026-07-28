package providers

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

const editorPluginCategory = "Plugin"

var editorPluginKindByTarget = map[string]string{
	"neovim": KindNeovimPlugin,
}

// SupportedEditorPluginTargets lists valid values for nvpm add --plugin.
var SupportedEditorPluginTargets = []string{"neovim"}

func normalizeEditorPluginTarget(target string) string {
	return strings.ToLower(strings.TrimSpace(target))
}

func kindForEditorPluginTarget(target string) (string, bool) {
	target = normalizeEditorPluginTarget(target)
	kind, ok := editorPluginKindByTarget[target]
	return kind, ok
}

// IsEditorPluginRegistryItem reports whether registry metadata classifies a package as an editor plugin.
func IsEditorPluginRegistryItem(item registry_parser.RegistryItem) bool {
	for _, c := range item.Categories {
		if c == editorPluginCategory {
			return true
		}
	}
	return len(item.EditorIntegration) > 0
}

// ResolveInstallKind returns the lock extras.kind for an install.
// pluginTarget is the value of --plugin (e.g. "neovim"); empty when the flag is omitted.
func ResolveInstallKind(item registry_parser.RegistryItem, pluginTarget string) (string, error) {
	pluginTarget = normalizeEditorPluginTarget(pluginTarget)
	if pluginTarget != "" {
		if kind, ok := kindForEditorPluginTarget(pluginTarget); ok {
			return kind, nil
		}
		return "", fmt.Errorf(
			"unsupported --plugin %q (supported: %s)",
			pluginTarget,
			strings.Join(SupportedEditorPluginTargets, ", "),
		)
	}

	if slices.Contains(item.EditorIntegration, "neovim") || IsEditorPluginRegistryItem(item) {
		return KindNeovimPlugin, nil
	}
	return "", nil
}

// IsEditorPluginPackage reports whether sourceID is an editor plugin for install path routing.
func IsEditorPluginPackage(sourceID string) bool {
	kind := GetInstallKind()
	if kind != "" {
		return true
	}
	return local_packages_parser.IsNeovimPlugin(sourceID)
}

package providers

import (
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/require"
)

func TestIsEditorPluginRegistryItem(t *testing.T) {
	require.True(t, IsEditorPluginRegistryItem(registry_parser.RegistryItem{
		Categories: []string{"Plugin"},
	}))
	require.True(t, IsEditorPluginRegistryItem(registry_parser.RegistryItem{
		EditorIntegration: []string{"neovim"},
	}))
	require.False(t, IsEditorPluginRegistryItem(registry_parser.RegistryItem{
		Categories: []string{"LSP"},
	}))
}

func TestResolveInstallKind(t *testing.T) {
	item := registry_parser.RegistryItem{Categories: []string{"Plugin"}}
	kind, err := ResolveInstallKind(item, "")
	require.NoError(t, err)
	require.Equal(t, KindNeovimPlugin, kind)

	kind, err = ResolveInstallKind(registry_parser.RegistryItem{}, "neovim")
	require.NoError(t, err)
	require.Equal(t, KindNeovimPlugin, kind)

	kind, err = ResolveInstallKind(registry_parser.RegistryItem{}, "")
	require.NoError(t, err)
	require.Equal(t, "", kind)

	_, err = ResolveInstallKind(registry_parser.RegistryItem{}, "vscode")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported --plugin")
}

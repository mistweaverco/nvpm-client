package providers

import (
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/require"
)

func TestApplicableTreeSitterIntegrations(t *testing.T) {
	item := registry_parser.RegistryItem{
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "hcl", GrammarDir: ".", Integrations: []string{"neovim"}},
			},
		},
	}
	require.Equal(t, []string{"neovim"}, ApplicableTreeSitterIntegrations(item, []string{"neovim"}))
	require.Empty(t, ApplicableTreeSitterIntegrations(item, []string{"vscode"}))
}

func TestApplicableTreeSitterIntegrations_LegacyEmptyMeansNeovim(t *testing.T) {
	item := registry_parser.RegistryItem{
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "c", GrammarDir: ".", Integrations: nil},
			},
		},
	}
	require.Equal(t, []string{"neovim"}, ApplicableTreeSitterIntegrations(item, []string{"neovim"}))
}

func TestResolveTreeSitterInstallIntegrations_MachineOutputMismatch(t *testing.T) {
	item := registry_parser.RegistryItem{
		Source:     registry_parser.RegistryItemSource{ID: "github:x/y"},
		Categories: []string{"Tree-sitter-parser"},
		TreeSitter: &registry_parser.RegistryItemTreeSitter{
			Build: []registry_parser.RegistryItemTreeSitterBuild{
				{Language: "hcl", GrammarDir: ".", Integrations: []string{"neovim"}},
			},
		},
	}
	_, err := ResolveTreeSitterInstallIntegrations(item, []string{"vscode"}, TreeSitterIntegrateResolveOpts{MachineOutput: true})
	require.Error(t, err)
}

func TestResolveTreeSitterInstallIntegrations_NonTreeSitterPassthrough(t *testing.T) {
	item := registry_parser.RegistryItem{
		Categories: []string{"LSP"},
	}
	out, err := ResolveTreeSitterInstallIntegrations(item, []string{"vscode", "neovim"}, TreeSitterIntegrateResolveOpts{MachineOutput: true})
	require.NoError(t, err)
	require.Equal(t, []string{"vscode", "neovim"}, out)
}

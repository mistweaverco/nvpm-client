package providers

import (
	"errors"
	"os"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectProviderUnsupported(t *testing.T) {
	assert.Equal(t, ProviderUnsupported, detectProvider("invalid"))
	assert.Equal(t, ProviderUnsupported, detectProvider("pkg:unknown/pkg"))
	// Missing trailing slash segment triggers len(parts) < 2 path
	assert.Equal(t, ProviderUnsupported, detectProvider("pkg:noslash"))
	// Only prefix
	assert.Equal(t, ProviderUnsupported, detectProvider("pkg:"))
}

func TestSyncAllInvokesProviderSyncs(t *testing.T) {
	_ = withTempNvpmHome(t)
	// Make Go available and Cargo available so their Sync methods run and return fast
	oldGo := goShellOut
	oldCargoHas := cargoHasCommand
	goShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	cargoHasCommand = func(string, []string, []string) bool { return true }
	defer func() { goShellOut = oldGo; cargoHasCommand = oldCargoHas }()

	// Ensure base packages dir exists to avoid mkdir races in different providers
	_ = os.MkdirAll(files.GetAppPackagesPath(), 0755)

	// Call SyncAllFromLock; with empty desired sets, each provider's Sync should no-op/return quickly
	_ = SyncAllFromLock()
}

func TestResolveVersionUsesRegistryVersionWhenOmitted(t *testing.T) {
	_ = withTempNvpmHome(t)
	sourceID := "github:tree-sitter/tree-sitter-regex"
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name:           "tree-sitter-regex",
		Version:        "v0.25.0",
		DefaultVersion: "v0.25.0",
		Source:         registry_parser.RegistryItemSource{ID: sourceID},
	}})

	oldDiscover := discoverGitRemoteLatestFn
	t.Cleanup(func() { discoverGitRemoteLatestFn = oldDiscover })
	discoverGitRemoteLatestFn = func(string, string) (GitRemoteLatestResult, error) {
		t.Fatal("registry default must skip remote latest discovery")
		return GitRemoteLatestResult{}, errors.New("should not discover")
	}

	got, err := ResolveVersion(sourceID, "")
	require.NoError(t, err)
	assert.Equal(t, "v0.25.0", got)

	got, err = ResolveVersion(sourceID, "latest")
	require.NoError(t, err)
	assert.Equal(t, "v0.25.0", got)

	got, err = ResolveVersion(sourceID, "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", got)
}

func TestResolveVersionPrefersDefaultVersionWhenOmitted(t *testing.T) {
	_ = withTempNvpmHome(t)
	sourceID := "github:microsoft/vscode-js-debug"
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name:           "js-debug-adapter",
		Version:        "v9.9.9",
		DefaultVersion: "v1.117.0",
		Source:         registry_parser.RegistryItemSource{ID: sourceID},
	}})

	oldDiscover := discoverGitRemoteLatestFn
	t.Cleanup(func() { discoverGitRemoteLatestFn = oldDiscover })
	discoverGitRemoteLatestFn = func(string, string) (GitRemoteLatestResult, error) {
		t.Fatal("default_version must skip remote latest discovery")
		return GitRemoteLatestResult{}, errors.New("should not discover")
	}

	got, err := ResolveVersion(sourceID, "")
	require.NoError(t, err)
	assert.Equal(t, "v1.117.0", got)

	got, err = ResolveVersion(sourceID, "latest")
	require.NoError(t, err)
	assert.Equal(t, "v9.9.9", got)

	got, err = ResolveVersion(sourceID, "v1.76.1")
	require.NoError(t, err)
	assert.Equal(t, "v1.76.1", got)
}

func TestResolveVersionPrefersNestedSourceDefaultVersionWhenOmitted(t *testing.T) {
	_ = withTempNvpmHome(t)
	sourceID := "github:microsoft/vscode-js-debug"
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name:    "js-debug-adapter",
		Version: "v9.9.9",
		Source: registry_parser.RegistryItemSource{
			ID:             sourceID,
			DefaultVersion: "v1.117.0",
		},
	}})

	oldDiscover := discoverGitRemoteLatestFn
	t.Cleanup(func() { discoverGitRemoteLatestFn = oldDiscover })
	discoverGitRemoteLatestFn = func(string, string) (GitRemoteLatestResult, error) {
		t.Fatal("source.default_version must skip remote latest discovery")
		return GitRemoteLatestResult{}, errors.New("should not discover")
	}

	got, err := ResolveVersion(sourceID, "")
	require.NoError(t, err)
	assert.Equal(t, "v1.117.0", got)
}

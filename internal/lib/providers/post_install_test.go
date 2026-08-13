package providers

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryItemPostInstallUnmarshal(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		var item registry_parser.RegistryItem
		require.NoError(t, json.Unmarshal([]byte(`{"name":"x","source":{"id":"npm:x"},"post_install":"npm add typescript@6.0.3"}`), &item))
		assert.Equal(t, "npm add typescript@6.0.3", item.PostInstallRun())
	})
	t.Run("object", func(t *testing.T) {
		var item registry_parser.RegistryItem
		require.NoError(t, json.Unmarshal([]byte(`{"name":"x","source":{"id":"npm:x"},"post_install":{"run":"npm add typescript@6.0.3"}}`), &item))
		assert.Equal(t, "npm add typescript@6.0.3", item.PostInstallRun())
	})
	t.Run("missing", func(t *testing.T) {
		var item registry_parser.RegistryItem
		require.NoError(t, json.Unmarshal([]byte(`{"name":"x","source":{"id":"npm:x"}}`), &item))
		assert.Empty(t, item.PostInstallRun())
	})
}

func TestPreflightPostInstallTrustedSkipsConfirm(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldConfirm := postInstallConfirmHook
	t.Cleanup(func() { postInstallConfirmHook = oldConfirm })
	postInstallConfirmHook = func(string, string) (bool, error) {
		t.Fatal("confirm should not run for always-trusted packages")
		return false, nil
	}
	SetPendingAlwaysTrust("npm:@astrojs/language-server", true)
	t.Cleanup(func() { ClearPendingAlwaysTrust("npm:@astrojs/language-server") })

	item := registry_parser.RegistryItem{
		Source:      registry_parser.RegistryItemSource{ID: "npm:@astrojs/language-server"},
		PostInstall: &registry_parser.RegistryItemPostInstall{Run: "npm add typescript@6.0.3"},
	}
	require.NoError(t, PreflightPostInstall(item))
	assert.True(t, pendingPostInstall[postInstallKey("npm:@astrojs/language-server")])
}

func TestExecutePostInstallRunsInPackageDir(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldConfirm := postInstallConfirmHook
	oldShell := postInstallShellOut
	oldParser := postInstallRegistryParser
	t.Cleanup(func() {
		postInstallConfirmHook = oldConfirm
		postInstallShellOut = oldShell
		postInstallRegistryParser = oldParser
		delete(pendingPostInstall, postInstallKey("npm:@astrojs/language-server"))
	})

	item := registry_parser.RegistryItem{
		Name:        "astro-language-server",
		Source:      registry_parser.RegistryItemSource{ID: "npm:@astrojs/language-server"},
		PostInstall: &registry_parser.RegistryItemPostInstall{Run: "npm add typescript@6.0.3"},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}

	var gotCmd, gotDir string
	var gotArgs []string
	postInstallShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		gotCmd, gotArgs, gotDir = cmd, args, dir
		return 0, nil
	}
	pendingPostInstall[postInstallKey("npm:@astrojs/language-server")] = true

	require.NoError(t, ExecutePostInstall("npm:@astrojs/language-server"))
	assert.Equal(t, "sh", gotCmd)
	require.Len(t, gotArgs, 2)
	assert.Equal(t, "-c", gotArgs[0])
	assert.Equal(t, "npm add typescript@6.0.3", gotArgs[1])
	wantDir := NewProviderNPM().packageDir("@astrojs/language-server")
	assert.Equal(t, wantDir, gotDir)
	_, err := os.Stat(wantDir)
	assert.NoError(t, err)
}

func TestExecutePostInstallSkippedWhenDeclined(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldShell := postInstallShellOut
	oldParser := postInstallRegistryParser
	t.Cleanup(func() {
		postInstallShellOut = oldShell
		postInstallRegistryParser = oldParser
	})
	item := registry_parser.RegistryItem{
		Source:      registry_parser.RegistryItemSource{ID: "npm:pkg"},
		PostInstall: &registry_parser.RegistryItemPostInstall{Run: "true"},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}
	postInstallShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("script should not run when declined")
		return 1, nil
	}
	pendingPostInstall[postInstallKey("npm:pkg")] = false
	require.NoError(t, ExecutePostInstall("npm:pkg"))
}

func TestExecutePostInstallFailedScript(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldShell := postInstallShellOut
	oldParser := postInstallRegistryParser
	t.Cleanup(func() {
		postInstallShellOut = oldShell
		postInstallRegistryParser = oldParser
	})
	item := registry_parser.RegistryItem{
		Source:      registry_parser.RegistryItemSource{ID: "npm:pkg"},
		PostInstall: &registry_parser.RegistryItemPostInstall{Run: "false"},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}
	postInstallShellOut = func(string, []string, string, []string) (int, error) {
		return 1, errors.New("boom")
	}
	pendingPostInstall[postInstallKey("npm:pkg")] = true
	err := ExecutePostInstall("npm:pkg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "post-install failed")
}

func TestPackageInstallDirNpmScoped(t *testing.T) {
	_ = withTempNvpmHome(t)
	dir := packageInstallDir("npm:@astrojs/language-server")
	assert.Equal(t, filepath.Join(files.GetAppPackagesPath(), "npm", "@astrojs", "language-server"), dir)
}

type registryBytesReader struct {
	data []byte
}

func (r *registryBytesReader) ReadFile(string) ([]byte, error) {
	return r.data, nil
}

func mustMarshalRegistry(t *testing.T, item registry_parser.RegistryItem) []byte {
	t.Helper()
	data, err := json.Marshal([]registry_parser.RegistryItem{item})
	require.NoError(t, err)
	return data
}

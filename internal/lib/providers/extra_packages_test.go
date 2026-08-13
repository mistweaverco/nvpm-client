package providers

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExtraPackageSpec(t *testing.T) {
	t.Parallel()
	parent := "npm:@astrojs/language-server"
	cases := []struct {
		spec, provider, name, version string
	}{
		{"npm:typescript@6.0.3", "npm", "typescript", "6.0.3"},
		{"npm:@astrojs/ts-plugin", "npm", "@astrojs/ts-plugin", ""},
		{"npm:@astrojs/ts-plugin@1.2.3", "npm", "@astrojs/ts-plugin", "1.2.3"},
		{"typescript", "npm", "typescript", ""},
		{"typescript@5.4.5", "npm", "typescript", "5.4.5"},
		{"@astrojs/ts-plugin", "npm", "@astrojs/ts-plugin", ""},
		{"@commitlint/config-conventional", "npm", "@commitlint/config-conventional", ""},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			provider, name, version, err := parseExtraPackageSpec(tc.spec, parent)
			require.NoError(t, err)
			assert.Equal(t, tc.provider, provider)
			assert.Equal(t, tc.name, name)
			assert.Equal(t, tc.version, version)
		})
	}
}

func TestParseExtraPackageSpecPyPiBareName(t *testing.T) {
	t.Parallel()
	provider, name, version, err := parseExtraPackageSpec("toml", "pypi:yapf")
	require.NoError(t, err)
	assert.Equal(t, "pypi", provider)
	assert.Equal(t, "toml", name)
	assert.Empty(t, version)
}

func TestParseExtraPackageSpecProviderMismatch(t *testing.T) {
	t.Parallel()
	_, _, _, err := parseExtraPackageSpec("pypi:toml", "npm:@astrojs/language-server")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must install into the parent package container")
}

func TestPreflightExtraPackagesTrustedSkipsConfirm(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldConfirm := extraPackagesConfirmHook
	t.Cleanup(func() { extraPackagesConfirmHook = oldConfirm })
	extraPackagesConfirmHook = func(string, []extraPackage) (bool, error) {
		t.Fatal("confirm should not run for always-trusted packages")
		return false, nil
	}
	SetPendingAlwaysTrust("npm:@astrojs/language-server", true)
	t.Cleanup(func() { ClearPendingAlwaysTrust("npm:@astrojs/language-server") })

	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:@astrojs/language-server",
			ExtraPackages: []string{"npm:typescript@6.0.3", "npm:@astrojs/ts-plugin"},
		},
	}
	require.NoError(t, PreflightExtraPackages(item))
	assert.True(t, pendingExtraPackages[extraPackagesKey("npm:@astrojs/language-server")])
}

func TestExecuteExtraPackagesRunsInParentPackageDir(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldConfirm := extraPackagesConfirmHook
	oldShell := extraPackagesShellOut
	oldParser := postInstallRegistryParser
	t.Cleanup(func() {
		extraPackagesConfirmHook = oldConfirm
		extraPackagesShellOut = oldShell
		postInstallRegistryParser = oldParser
		delete(pendingExtraPackages, extraPackagesKey("npm:@astrojs/language-server"))
	})

	item := registry_parser.RegistryItem{
		Name: "astro-language-server",
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:@astrojs/language-server",
			ExtraPackages: []string{"npm:typescript@6.0.3", "npm:@astrojs/ts-plugin"},
		},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}

	type call struct {
		cmd  string
		args []string
		dir  string
	}
	var calls []call
	extraPackagesShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		copied := append([]string(nil), args...)
		calls = append(calls, call{cmd: cmd, args: copied, dir: dir})
		return 0, nil
	}
	pendingExtraPackages[extraPackagesKey("npm:@astrojs/language-server")] = true

	require.NoError(t, ExecuteExtraPackages("npm:@astrojs/language-server"))
	wantDir := NewProviderNPM().packageDir("@astrojs/language-server")
	require.Len(t, calls, 2)
	assert.Equal(t, "npm", calls[0].cmd)
	assert.Equal(t, []string{"install", "--no-update-notifier", "typescript@6.0.3"}, calls[0].args)
	assert.Equal(t, wantDir, calls[0].dir)
	assert.Equal(t, []string{"install", "--no-update-notifier", "@astrojs/ts-plugin"}, calls[1].args)
	assert.Equal(t, wantDir, calls[1].dir)
	_, err := os.Stat(wantDir)
	assert.NoError(t, err)
}

func TestExecuteExtraPackagesPyPiUsesPrefix(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldShell := extraPackagesShellOut
	oldParser := postInstallRegistryParser
	t.Cleanup(func() {
		extraPackagesShellOut = oldShell
		postInstallRegistryParser = oldParser
		delete(pendingExtraPackages, extraPackagesKey("pypi:yapf"))
	})
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "pypi:yapf",
			ExtraPackages: []string{"toml"},
		},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}
	var gotCmd, gotDir string
	var gotArgs []string
	extraPackagesShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		gotCmd, gotArgs, gotDir = cmd, append([]string(nil), args...), dir
		return 0, nil
	}
	pendingExtraPackages[extraPackagesKey("pypi:yapf")] = true
	require.NoError(t, ExecuteExtraPackages("pypi:yapf"))
	wantDir := NewProviderPyPi().packageDir("yapf")
	assert.Equal(t, pipCmd, gotCmd)
	assert.Equal(t, []string{"install", "toml", "--prefix", wantDir}, gotArgs)
	assert.Equal(t, wantDir, gotDir)
}

func TestExecuteExtraPackagesSkippedWhenDeclined(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldShell := extraPackagesShellOut
	oldParser := postInstallRegistryParser
	t.Cleanup(func() {
		extraPackagesShellOut = oldShell
		postInstallRegistryParser = oldParser
	})
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:pkg",
			ExtraPackages: []string{"typescript"},
		},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}
	extraPackagesShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("extras should not install when declined")
		return 1, nil
	}
	pendingExtraPackages[extraPackagesKey("npm:pkg")] = false
	require.NoError(t, ExecuteExtraPackages("npm:pkg"))
}

func TestExecuteExtraPackagesFailedInstall(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldShell := extraPackagesShellOut
	oldParser := postInstallRegistryParser
	t.Cleanup(func() {
		extraPackagesShellOut = oldShell
		postInstallRegistryParser = oldParser
	})
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:pkg",
			ExtraPackages: []string{"typescript@6.0.3"},
		},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}
	extraPackagesShellOut = func(string, []string, string, []string) (int, error) {
		return 1, errors.New("boom")
	}
	pendingExtraPackages[extraPackagesKey("npm:pkg")] = true
	err := ExecuteExtraPackages("npm:pkg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra-packages: failed to install")
}

func TestExecuteRegistryInstallHooksRunsExtrasThenPostInstall(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldExtraShell := extraPackagesShellOut
	oldPostShell := postInstallShellOut
	oldParser := postInstallRegistryParser
	t.Cleanup(func() {
		extraPackagesShellOut = oldExtraShell
		postInstallShellOut = oldPostShell
		postInstallRegistryParser = oldParser
		delete(pendingExtraPackages, extraPackagesKey("npm:pkg"))
		delete(pendingPostInstall, extraPackagesKey("npm:pkg"))
	})
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:pkg",
			ExtraPackages: []string{"typescript@6.0.3"},
		},
		PostInstall: &registry_parser.RegistryItemPostInstall{Run: "true"},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}
	var order []string
	extraPackagesShellOut = func(string, []string, string, []string) (int, error) {
		order = append(order, "extra")
		return 0, nil
	}
	postInstallShellOut = func(string, []string, string, []string) (int, error) {
		order = append(order, "post")
		return 0, nil
	}
	pendingExtraPackages[extraPackagesKey("npm:pkg")] = true
	pendingPostInstall[extraPackagesKey("npm:pkg")] = true
	require.NoError(t, ExecuteRegistryInstallHooks("npm:pkg"))
	assert.Equal(t, []string{"extra", "post"}, order)
}

func TestRegistryItemSourceExtraPackagesUnmarshal(t *testing.T) {
	var item registry_parser.RegistryItem
	require.NoError(t, json.Unmarshal([]byte(`{
		"name":"astro-language-server",
		"source":{
			"id":"npm:@astrojs/language-server",
			"extra_packages":["npm:typescript@6.0.3","npm:@astrojs/ts-plugin"]
		}
	}`), &item))
	assert.Equal(t, []string{"npm:typescript@6.0.3", "npm:@astrojs/ts-plugin"}, item.Source.ExtraPackages)
}

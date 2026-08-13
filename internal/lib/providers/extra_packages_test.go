package providers

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubExtraPackageResolve(t *testing.T, versions map[string]string) {
	t.Helper()
	old := extraPackageResolveVersion
	extraPackageResolveVersion = func(provider, name string) (string, error) {
		if versions == nil {
			return "", nil
		}
		if v, ok := versions[provider+":"+name]; ok {
			return v, nil
		}
		return "", nil
	}
	t.Cleanup(func() { extraPackageResolveVersion = old })
}

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
	stubExtraPackageResolve(t, nil)
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
	stubExtraPackageResolve(t, nil)
	require.NoError(t, local_packages_parser.AddLocalPackage("npm:@astrojs/language-server", "2.0.0"))
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
	assert.True(t, ConsumeExtraPackagesInstalledLastOp())
	assert.False(t, ConsumeExtraPackagesInstalledLastOp())
	wantDir := NewProviderNPM().packageDir("@astrojs/language-server")
	require.Len(t, calls, 2)
	assert.Equal(t, "npm", calls[0].cmd)
	assert.Equal(t, []string{"install", "--no-update-notifier", "typescript@6.0.3"}, calls[0].args)
	assert.Equal(t, wantDir, calls[0].dir)
	assert.Equal(t, []string{"install", "--no-update-notifier", "@astrojs/ts-plugin"}, calls[1].args)
	assert.Equal(t, wantDir, calls[1].dir)
	_, err := os.Stat(wantDir)
	assert.NoError(t, err)
	lock := local_packages_parser.GetBySourceId("npm:@astrojs/language-server")
	require.NotNil(t, lock.Extras)
	assert.Equal(t, []local_packages_parser.ExtraPackagePin{
		{ID: "npm:@astrojs/ts-plugin"},
		{ID: "npm:typescript", Version: "6.0.3"},
	}, lock.Extras.ExtraPackages)
}

func TestExecuteExtraPackagesPyPiUsesPrefix(t *testing.T) {
	_ = withTempNvpmHome(t)
	stubExtraPackageResolve(t, nil)
	require.NoError(t, local_packages_parser.AddLocalPackage("pypi:yapf", "0.40.0"))
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
	stubExtraPackageResolve(t, nil)
	require.NoError(t, local_packages_parser.AddLocalPackage("npm:pkg", "1.0.0"))
	require.NoError(t, local_packages_parser.MergePackageExtraPackages("npm:pkg", []local_packages_parser.ExtraPackagePin{
		{ID: "npm:typescript", Version: "6.0.3"},
	}))
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
	assert.False(t, ConsumeExtraPackagesInstalledLastOp())
	lock := local_packages_parser.GetBySourceId("npm:pkg")
	if lock.Extras != nil {
		assert.Empty(t, lock.Extras.ExtraPackages)
	}
}

func TestExecuteExtraPackagesFailedInstall(t *testing.T) {
	_ = withTempNvpmHome(t)
	stubExtraPackageResolve(t, nil)
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
	stubExtraPackageResolve(t, nil)
	require.NoError(t, local_packages_parser.AddLocalPackage("npm:pkg", "1.0.0"))
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

func TestExtraPackagesCoveredByLockVersionAndCommit(t *testing.T) {
	_ = withTempNvpmHome(t)
	require.NoError(t, local_packages_parser.AddLocalPackage("npm:pkg", "1.0.0"))
	require.NoError(t, local_packages_parser.MergePackageExtraPackages("npm:pkg", []local_packages_parser.ExtraPackagePin{
		{ID: "npm:typescript", Version: "6.0.3"},
		{ID: "npm:plugin", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"},
	}))

	assert.True(t, extraPackagesCoveredByLock("npm:pkg", []extraPackage{
		{Provider: "npm", Name: "typescript", Version: "6.0.3"},
		{Provider: "npm", Name: "plugin", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"},
	}))
	assert.False(t, extraPackagesCoveredByLock("npm:pkg", []extraPackage{
		{Provider: "npm", Name: "typescript", Version: "6.0.4"},
	}))
	assert.False(t, extraPackagesCoveredByLock("npm:pkg", []extraPackage{
		{Provider: "npm", Name: "plugin", Commit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb2"},
	}))
	assert.False(t, extraPackagesCoveredByLock("npm:pkg", []extraPackage{
		{Provider: "npm", Name: "typescript", Version: "6.0.3"},
		{Provider: "npm", Name: "new-extra", Version: "1.0.0"},
	}))
}

func TestPreflightExtraPackagesSkipsConfirmWhenLockMatches(t *testing.T) {
	_ = withTempNvpmHome(t)
	stubExtraPackageResolve(t, map[string]string{"npm:@astrojs/ts-plugin": "1.2.3"})
	require.NoError(t, local_packages_parser.AddLocalPackage("npm:@astrojs/language-server", "2.0.0"))
	require.NoError(t, local_packages_parser.MergePackageExtraPackages("npm:@astrojs/language-server", []local_packages_parser.ExtraPackagePin{
		{ID: "npm:typescript", Version: "6.0.3"},
		{ID: "npm:@astrojs/ts-plugin", Version: "1.2.3"},
	}))
	oldConfirm := extraPackagesConfirmHook
	t.Cleanup(func() {
		extraPackagesConfirmHook = oldConfirm
		delete(pendingExtraPackages, extraPackagesKey("npm:@astrojs/language-server"))
		delete(pendingExtraPackageResolved, extraPackagesKey("npm:@astrojs/language-server"))
	})
	extraPackagesConfirmHook = func(string, []extraPackage) (bool, error) {
		t.Fatal("confirm should not run when lock extras match")
		return false, nil
	}
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:@astrojs/language-server",
			ExtraPackages: []string{"npm:typescript@6.0.3", "npm:@astrojs/ts-plugin"},
		},
	}
	require.NoError(t, PreflightExtraPackages(item))
	assert.True(t, pendingExtraPackages[extraPackagesKey("npm:@astrojs/language-server")])
}

func TestPreflightExtraPackagesPromptsWhenVersionChanges(t *testing.T) {
	_ = withTempNvpmHome(t)
	stubExtraPackageResolve(t, nil)
	require.NoError(t, local_packages_parser.AddLocalPackage("npm:pkg", "1.0.0"))
	require.NoError(t, local_packages_parser.MergePackageExtraPackages("npm:pkg", []local_packages_parser.ExtraPackagePin{
		{ID: "npm:typescript", Version: "6.0.3"},
	}))
	oldConfirm := extraPackagesConfirmHook
	t.Cleanup(func() {
		extraPackagesConfirmHook = oldConfirm
		delete(pendingExtraPackages, extraPackagesKey("npm:pkg"))
		delete(pendingExtraPackageResolved, extraPackagesKey("npm:pkg"))
	})
	var prompted []extraPackage
	extraPackagesConfirmHook = func(_ string, pkgs []extraPackage) (bool, error) {
		prompted = pkgs
		return true, nil
	}
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:pkg",
			ExtraPackages: []string{"npm:typescript@6.0.4"},
		},
	}
	require.NoError(t, PreflightExtraPackages(item))
	require.Len(t, prompted, 1)
	assert.Equal(t, "6.0.4", prompted[0].Version)
	assert.True(t, pendingExtraPackages[extraPackagesKey("npm:pkg")])
}

func TestPreflightExtraPackagesPromptsWhenResolvedVersionChanges(t *testing.T) {
	_ = withTempNvpmHome(t)
	stubExtraPackageResolve(t, map[string]string{"npm:@astrojs/ts-plugin": "1.3.0"})
	require.NoError(t, local_packages_parser.AddLocalPackage("npm:@astrojs/language-server", "2.0.0"))
	require.NoError(t, local_packages_parser.MergePackageExtraPackages("npm:@astrojs/language-server", []local_packages_parser.ExtraPackagePin{
		{ID: "npm:@astrojs/ts-plugin", Version: "1.2.3"},
	}))
	oldConfirm := extraPackagesConfirmHook
	t.Cleanup(func() {
		extraPackagesConfirmHook = oldConfirm
		delete(pendingExtraPackages, extraPackagesKey("npm:@astrojs/language-server"))
		delete(pendingExtraPackageResolved, extraPackagesKey("npm:@astrojs/language-server"))
	})
	var prompted []extraPackage
	extraPackagesConfirmHook = func(_ string, pkgs []extraPackage) (bool, error) {
		prompted = pkgs
		return true, nil
	}
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:@astrojs/language-server",
			ExtraPackages: []string{"npm:@astrojs/ts-plugin"},
		},
	}
	require.NoError(t, PreflightExtraPackages(item))
	require.Len(t, prompted, 1)
	assert.Equal(t, "1.3.0", prompted[0].Version)
}

func TestPreflightExtraPackagesPromptsWhenGitSHAChanges(t *testing.T) {
	_ = withTempNvpmHome(t)
	stubExtraPackageResolve(t, nil)
	oldSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
	newSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb2"
	require.NoError(t, local_packages_parser.AddLocalPackage("github:org/pkg", "main"))
	require.NoError(t, local_packages_parser.MergePackageExtraPackages("github:org/pkg", []local_packages_parser.ExtraPackagePin{
		{ID: "github:org/extra", Version: oldSHA, Commit: oldSHA},
	}))
	oldConfirm := extraPackagesConfirmHook
	t.Cleanup(func() {
		extraPackagesConfirmHook = oldConfirm
		delete(pendingExtraPackages, extraPackagesKey("github:org/pkg"))
		delete(pendingExtraPackageResolved, extraPackagesKey("github:org/pkg"))
	})
	var prompted []extraPackage
	extraPackagesConfirmHook = func(_ string, pkgs []extraPackage) (bool, error) {
		prompted = pkgs
		return true, nil
	}
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "github:org/pkg",
			ExtraPackages: []string{"github:org/extra@" + newSHA},
		},
	}
	require.NoError(t, PreflightExtraPackages(item))
	require.Len(t, prompted, 1)
	assert.Equal(t, newSHA, prompted[0].Commit)
}

func TestRegistryHasInstallHooks(t *testing.T) {
	_ = withTempNvpmHome(t)
	oldParser := postInstallRegistryParser
	t.Cleanup(func() { postInstallRegistryParser = oldParser })

	t.Run("extra packages", func(t *testing.T) {
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
		assert.True(t, RegistryHasInstallHooks("npm:pkg"))
	})

	t.Run("post install only", func(t *testing.T) {
		item := registry_parser.RegistryItem{
			Source:      registry_parser.RegistryItemSource{ID: "npm:pkg"},
			PostInstall: &registry_parser.RegistryItemPostInstall{Run: "true"},
		}
		postInstallRegistryParser = func() *registry_parser.RegistryParser {
			p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
			_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
			return p
		}
		assert.True(t, RegistryHasInstallHooks("npm:pkg"))
	})

	t.Run("none", func(t *testing.T) {
		item := registry_parser.RegistryItem{
			Source: registry_parser.RegistryItemSource{ID: "npm:pkg"},
		}
		postInstallRegistryParser = func() *registry_parser.RegistryParser {
			p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
			_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
			return p
		}
		assert.False(t, RegistryHasInstallHooks("npm:pkg"))
		assert.False(t, RegistryHasInstallHooks("npm:other"))
	})
}

func TestExecuteExtraPackagesRecordsUnversionedExtra(t *testing.T) {
	_ = withTempNvpmHome(t)
	stubExtraPackageResolve(t, map[string]string{"npm:typescript-svelte-plugin": "0.3.52"})
	require.NoError(t, local_packages_parser.AddLocalPackage("npm:svelte-language-server", "0.18.3"))
	oldShell := extraPackagesShellOut
	oldParser := postInstallRegistryParser
	oldConfirm := extraPackagesConfirmHook
	t.Cleanup(func() {
		extraPackagesShellOut = oldShell
		postInstallRegistryParser = oldParser
		extraPackagesConfirmHook = oldConfirm
		delete(pendingExtraPackages, extraPackagesKey("npm:svelte-language-server"))
		delete(pendingExtraPackageResolved, extraPackagesKey("npm:svelte-language-server"))
	})
	item := registry_parser.RegistryItem{
		Source: registry_parser.RegistryItemSource{
			ID:            "npm:svelte-language-server",
			ExtraPackages: []string{"npm:typescript-svelte-plugin"},
		},
	}
	postInstallRegistryParser = func() *registry_parser.RegistryParser {
		p := registry_parser.NewRegistryParser(&registryBytesReader{data: mustMarshalRegistry(t, item)})
		_ = p.LoadFromBytes(mustMarshalRegistry(t, item))
		return p
	}
	extraPackagesConfirmHook = func(string, []extraPackage) (bool, error) { return true, nil }
	extraPackagesShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	require.NoError(t, PreflightExtraPackages(item))
	require.NoError(t, ExecuteExtraPackages("npm:svelte-language-server"))
	assert.True(t, ConsumeExtraPackagesInstalledLastOp())
	lock := local_packages_parser.GetBySourceId("npm:svelte-language-server")
	require.NotNil(t, lock.Extras)
	assert.Equal(t, []local_packages_parser.ExtraPackagePin{
		{ID: "npm:typescript-svelte-plugin", Version: "0.3.52"},
	}, lock.Extras.ExtraPackages)
}

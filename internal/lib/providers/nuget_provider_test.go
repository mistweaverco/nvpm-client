package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
)

func plantNuGetInstalled(t *testing.T, p *NuGetProvider, name, binName string) {
	t.Helper()
	dir := p.packageDir(name)
	assert.NoError(t, os.MkdirAll(dir, 0755))
	if binName == "" {
		binName = name
	}
	assert.NoError(t, os.WriteFile(filepath.Join(dir, binName), []byte("#!/bin/sh\n"), 0755))
}

func TestNuGetPerPackageIsolationAndWrappers(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNuGet()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppNugetAdd("nuget:csharpier", "0.28.0")
	_ = lppNugetAdd("nuget:dotnet-ef", "8.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{
		{Name: "csharpier", Version: "0.28.0", Source: registry_parser.RegistryItemSource{ID: "nuget:csharpier"}, Bin: map[string]string{"csharpier": "csharpier"}},
		{Name: "dotnet-ef", Version: "8.0.0", Source: registry_parser.RegistryItemSource{ID: "nuget:dotnet-ef"}, Bin: map[string]string{"dotnet-ef": "dotnet-ef"}},
	})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantNuGetInstalled(t, p, "csharpier", "csharpier")
	plantNuGetInstalled(t, p, "dotnet-ef", "dotnet-ef")

	oldHas := nugetHasCommand
	oldOut := nugetShellOut
	nugetHasCommand = func(string, []string, []string) bool { return true }
	nugetShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("Sync should skip already-installed nuget tools")
		return 1, nil
	}
	assert.True(t, p.Sync())
	nugetHasCommand = oldHas
	nugetShellOut = oldOut

	csharpier, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "csharpier"))
	assert.NoError(t, err)
	assert.Contains(t, string(csharpier), filepath.Join("packages", "nuget", "csharpier"))
	assert.NotContains(t, string(csharpier), filepath.Join("packages", "nuget", "dotnet-ef"))
	ef, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "dotnet-ef"))
	assert.NoError(t, err)
	assert.Contains(t, string(ef), filepath.Join("packages", "nuget", "dotnet-ef"))
}

func TestNuGetSyncMigratesLegacySharedToolPath(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNuGet()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppNugetAdd("nuget:csharpier", "0.28.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "csharpier", Version: "0.28.0", Source: registry_parser.RegistryItemSource{ID: "nuget:csharpier"},
		Bin: map[string]string{"csharpier": "csharpier"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "nvpm-tools.csproj"), []byte("<Project />"), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "csharpier"), []byte("#!/bin/sh\n"), 0755))
	assert.NoError(t, os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, ".store"), 0755))

	oldHas := nugetHasCommand
	oldOut := nugetShellOut
	nugetHasCommand = func(string, []string, []string) bool { return true }
	nugetShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		plantNuGetInstalled(t, p, "csharpier", "csharpier")
		return 0, nil
	}
	assert.True(t, p.Sync())
	nugetHasCommand = oldHas
	nugetShellOut = oldOut

	_, err := os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "nvpm-tools.csproj"))
	assert.Error(t, err)
	fi, err := os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "csharpier"))
	assert.NoError(t, err)
	assert.True(t, fi.IsDir(), "legacy shim file should be replaced by a package container")
	_, err = os.Stat(filepath.Join(p.APP_PACKAGES_DIR, ".store"))
	assert.Error(t, err)
	_, err = os.Stat(p.packageDir("csharpier"))
	assert.NoError(t, err)
	wrapper, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "csharpier"))
	assert.NoError(t, err)
	assert.Contains(t, string(wrapper), filepath.Join("packages", "nuget", "csharpier"))
}

func TestNuGetRemoveDeletesContainerAndWrapper(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNuGet()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppNugetAdd("nuget:csharpier", "0.28.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "csharpier", Version: "0.28.0", Source: registry_parser.RegistryItemSource{ID: "nuget:csharpier"},
		Bin: map[string]string{"csharpier": "csharpier"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantNuGetInstalled(t, p, "csharpier", "csharpier")
	assert.NoError(t, os.WriteFile(filepath.Join(files.GetAppBinPath(), "csharpier"), []byte("wrapper"), 0755))

	oldHas := nugetHasCommand
	oldOut := nugetShellOut
	nugetHasCommand = func(string, []string, []string) bool { return true }
	nugetShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Remove("nuget:csharpier"))
	nugetHasCommand = oldHas
	nugetShellOut = oldOut

	_, err := os.Stat(p.packageDir("csharpier"))
	assert.Error(t, err)
	_, err = os.Lstat(filepath.Join(files.GetAppBinPath(), "csharpier"))
	assert.Error(t, err)
}

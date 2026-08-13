package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
)

func plantLuaRockInstalled(t *testing.T, p *LuaRocksProvider, name, binName string) {
	t.Helper()
	binDir := filepath.Join(p.packageDir(name), "bin")
	assert.NoError(t, os.MkdirAll(binDir, 0755))
	if binName != "" {
		assert.NoError(t, os.WriteFile(filepath.Join(binDir, binName), []byte("#!/bin/sh\n"), 0755))
	}
}

func TestLuaRocksPerPackageIsolationAndWrappers(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderLuaRocks()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppLuarocksAdd("luarocks:luacheck", "1.1.0")
	_ = lppLuarocksAdd("luarocks:ldoc", "1.5.0")
	writeRegistry(t, []registry_parser.RegistryItem{
		{Name: "luacheck", Version: "1.1.0", Source: registry_parser.RegistryItemSource{ID: "luarocks:luacheck"}, Bin: map[string]string{"luacheck": "luacheck"}},
		{Name: "ldoc", Version: "1.5.0", Source: registry_parser.RegistryItemSource{ID: "luarocks:ldoc"}, Bin: map[string]string{"ldoc": "ldoc"}},
	})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantLuaRockInstalled(t, p, "luacheck", "luacheck")
	plantLuaRockInstalled(t, p, "ldoc", "ldoc")

	oldHas := luarocksHasCommand
	oldOut := luarocksShellOut
	luarocksHasCommand = func(string, []string, []string) bool { return true }
	luarocksShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("Sync should skip already-installed rocks")
		return 1, nil
	}
	assert.True(t, p.Sync())
	luarocksHasCommand = oldHas
	luarocksShellOut = oldOut

	luacheck, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "luacheck"))
	assert.NoError(t, err)
	assert.Contains(t, string(luacheck), filepath.Join("packages", "luarocks", "luacheck"))
	assert.NotContains(t, string(luacheck), filepath.Join("packages", "luarocks", "ldoc"))
	ldoc, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "ldoc"))
	assert.NoError(t, err)
	assert.Contains(t, string(ldoc), filepath.Join("packages", "luarocks", "ldoc"))
}

func TestLuaRocksSyncMigratesLegacySharedTree(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderLuaRocks()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppLuarocksAdd("luarocks:luacheck", "1.1.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "luacheck", Version: "1.1.0", Source: registry_parser.RegistryItemSource{ID: "luarocks:luacheck"},
		Bin: map[string]string{"luacheck": "luacheck"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	assert.NoError(t, os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "bin"), 0755))
	assert.NoError(t, os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "lib"), 0755))
	assert.NoError(t, os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "share"), 0755))

	oldHas := luarocksHasCommand
	oldOut := luarocksShellOut
	luarocksHasCommand = func(string, []string, []string) bool { return true }
	luarocksShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		plantLuaRockInstalled(t, p, "luacheck", "luacheck")
		return 0, nil
	}
	assert.True(t, p.Sync())
	luarocksHasCommand = oldHas
	luarocksShellOut = oldOut

	_, err := os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "bin"))
	assert.Error(t, err)
	_, err = os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "lib"))
	assert.Error(t, err)
	_, err = os.Stat(p.packageDir("luacheck"))
	assert.NoError(t, err)
	wrapper, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "luacheck"))
	assert.NoError(t, err)
	assert.Contains(t, string(wrapper), filepath.Join("packages", "luarocks", "luacheck"))
}

func TestLuaRocksRemoveDeletesContainerAndWrapper(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderLuaRocks()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppLuarocksAdd("luarocks:luacheck", "1.1.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "luacheck", Version: "1.1.0", Source: registry_parser.RegistryItemSource{ID: "luarocks:luacheck"},
		Bin: map[string]string{"luacheck": "luacheck"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantLuaRockInstalled(t, p, "luacheck", "luacheck")
	assert.NoError(t, os.WriteFile(filepath.Join(files.GetAppBinPath(), "luacheck"), []byte("wrapper"), 0755))

	oldHas := luarocksHasCommand
	oldOut := luarocksShellOut
	luarocksHasCommand = func(string, []string, []string) bool { return true }
	luarocksShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Remove("luarocks:luacheck"))
	luarocksHasCommand = oldHas
	luarocksShellOut = oldOut

	_, err := os.Stat(p.packageDir("luacheck"))
	assert.Error(t, err)
	_, err = os.Lstat(filepath.Join(files.GetAppBinPath(), "luacheck"))
	assert.Error(t, err)
}

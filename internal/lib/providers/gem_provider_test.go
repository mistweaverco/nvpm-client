package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
)

func plantGemInstalled(t *testing.T, prefix, name, version string) {
	t.Helper()
	specDir := filepath.Join(prefix, "specifications")
	binDir := filepath.Join(prefix, "bin")
	assert.NoError(t, os.MkdirAll(specDir, 0755))
	assert.NoError(t, os.MkdirAll(binDir, 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(specDir, name+"-"+version+".gemspec"), []byte("# spec"), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\necho "+name+"\n"), 0755))
}

func TestGemProvider_packageDir(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGem()
	got := p.packageDir("solargraph")
	want := filepath.Join(p.APP_PACKAGES_DIR, "solargraph")
	assert.Equal(t, want, got)
}

func TestGemPerPackageIsolationAndWrappers(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGem()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	_ = lppGemAdd("gem:solargraph", "0.50.0")
	_ = lppGemAdd("gem:rubocop", "1.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{
		{Name: "solargraph", Version: "0.50.0", Source: registry_parser.RegistryItemSource{ID: "gem:solargraph"}, Bin: map[string]string{"solargraph": "solargraph"}},
		{Name: "rubocop", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "gem:rubocop"}, Bin: map[string]string{"rubocop": "rubocop"}},
	})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)

	plantGemInstalled(t, p.packageDir("solargraph"), "solargraph", "0.50.0")
	plantGemInstalled(t, p.packageDir("rubocop"), "rubocop", "1.0.0")

	oldHas := gemHasCommand
	oldOut := gemShellOut
	gemHasCommand = func(string, []string, []string) bool { return true }
	gemShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("Sync should skip already-installed gems")
		return 1, nil
	}
	assert.True(t, p.Sync())
	gemHasCommand = oldHas
	gemShellOut = oldOut

	sg, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "solargraph"))
	assert.NoError(t, err)
	assert.Contains(t, string(sg), filepath.Join("packages", "gem", "solargraph"))
	assert.NotContains(t, string(sg), filepath.Join("packages", "gem", "rubocop"))
	rc, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "rubocop"))
	assert.NoError(t, err)
	assert.Contains(t, string(rc), filepath.Join("packages", "gem", "rubocop"))
	assert.NotContains(t, string(rc), filepath.Join("packages", "gem", "solargraph"))
}

func TestGemSyncMigratesLegacySharedTree(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGem()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppGemAdd("gem:solargraph", "0.50.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "solargraph", Version: "0.50.0", Source: registry_parser.RegistryItemSource{ID: "gem:solargraph"},
		Bin: map[string]string{"solargraph": "solargraph"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)

	plantGemInstalled(t, p.APP_PACKAGES_DIR, "solargraph", "0.50.0")
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "Gemfile"), []byte("gem 'solargraph'\n"), 0644))

	oldHas := gemHasCommand
	oldOut := gemShellOut
	gemHasCommand = func(string, []string, []string) bool { return true }
	gemShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if cmd == gemCmd && len(args) >= 2 && args[0] == "install" {
			plantGemInstalled(t, p.packageDir("solargraph"), "solargraph", "0.50.0")
			return 0, nil
		}
		t.Fatalf("unexpected command %s %v", cmd, args)
		return 1, nil
	}
	assert.True(t, p.Sync())
	gemHasCommand = oldHas
	gemShellOut = oldOut

	_, err := os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "Gemfile"))
	assert.Error(t, err)
	_, err = os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "bin"))
	assert.Error(t, err)
	_, err = os.Stat(p.packageDir("solargraph"))
	assert.NoError(t, err)

	wrapper, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "solargraph"))
	assert.NoError(t, err)
	assert.Contains(t, string(wrapper), filepath.Join("packages", "gem", "solargraph"))
	assert.True(t, strings.Contains(string(wrapper), "GEM_HOME"))
}

func TestGemInstallMigratesPackageIntoContainer(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGem()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "solargraph", Version: "0.50.0", Source: registry_parser.RegistryItemSource{ID: "gem:solargraph"},
		Bin: map[string]string{"solargraph": "solargraph"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantGemInstalled(t, p.APP_PACKAGES_DIR, "solargraph", "0.49.0")
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "Gemfile"), []byte("gem 'solargraph'\n"), 0644))

	oldHas := gemHasCommand
	oldOut := gemShellOut
	oldCap := gemShellOutCapture
	gemHasCommand = func(string, []string, []string) bool { return true }
	gemShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if cmd == gemCmd && len(args) >= 2 && args[0] == "install" {
			assert.Equal(t, p.packageDir("solargraph"), args[3])
			plantGemInstalled(t, p.packageDir("solargraph"), "solargraph", "0.50.0")
			return 0, nil
		}
		return 0, nil
	}
	gemShellOutCapture = func(string, []string, string, []string) (int, string, error) {
		return 0, "solargraph (0.50.0)\n", nil
	}
	assert.True(t, p.Install("gem:solargraph", "0.50.0"))
	gemHasCommand = oldHas
	gemShellOut = oldOut
	gemShellOutCapture = oldCap

	_, err := os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "bin"))
	assert.Error(t, err)
	_, err = os.Stat(p.packageDir("solargraph"))
	assert.NoError(t, err)
	wrapper, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "solargraph"))
	assert.NoError(t, err)
	assert.Contains(t, string(wrapper), filepath.Join("packages", "gem", "solargraph"))
}

func TestGemRemoveDeletesContainerAndWrapper(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGem()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppGemAdd("gem:solargraph", "0.50.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "solargraph", Version: "0.50.0", Source: registry_parser.RegistryItemSource{ID: "gem:solargraph"},
		Bin: map[string]string{"solargraph": "solargraph"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantGemInstalled(t, p.packageDir("solargraph"), "solargraph", "0.50.0")
	assert.NoError(t, os.WriteFile(filepath.Join(files.GetAppBinPath(), "solargraph"), []byte("wrapper"), 0755))

	oldHas := gemHasCommand
	oldOut := gemShellOut
	gemHasCommand = func(string, []string, []string) bool { return true }
	gemShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Remove("gem:solargraph"))
	gemHasCommand = oldHas
	gemShellOut = oldOut

	_, err := os.Stat(p.packageDir("solargraph"))
	assert.Error(t, err)
	_, err = os.Lstat(filepath.Join(files.GetAppBinPath(), "solargraph"))
	assert.Error(t, err)
}

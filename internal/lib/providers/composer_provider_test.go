package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
)

func plantComposerInstalled(t *testing.T, p *ComposerProvider, name, binName string) {
	t.Helper()
	dir := p.packageDir(name)
	vendorPkg := filepath.Join(dir, "vendor", name)
	binDir := filepath.Join(dir, "vendor", "bin")
	assert.NoError(t, os.MkdirAll(vendorPkg, 0755))
	assert.NoError(t, os.MkdirAll(binDir, 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(vendorPkg, "composer.json"), []byte(`{"name":"`+name+`"}`), 0644))
	if binName != "" {
		assert.NoError(t, os.WriteFile(filepath.Join(binDir, binName), []byte("#!/bin/sh\n"), 0755))
	}
}

func TestComposerPerPackageIsolationAndWrappers(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderComposer()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppComposerAdd("composer:phpstan/phpstan", "1.0.0")
	_ = lppComposerAdd("composer:squizlabs/php_codesniffer", "3.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{
		{Name: "phpstan", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "composer:phpstan/phpstan"}, Bin: map[string]string{"phpstan": "phpstan"}},
		{Name: "phpcs", Version: "3.0.0", Source: registry_parser.RegistryItemSource{ID: "composer:squizlabs/php_codesniffer"}, Bin: map[string]string{"phpcs": "phpcs"}},
	})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantComposerInstalled(t, p, "phpstan/phpstan", "phpstan")
	plantComposerInstalled(t, p, "squizlabs/php_codesniffer", "phpcs")

	oldHas := composerHasCommand
	oldOut := composerShellOut
	composerHasCommand = func(string, []string, []string) bool { return true }
	composerShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("Sync should skip already-installed composer packages")
		return 1, nil
	}
	assert.True(t, p.Sync())
	composerHasCommand = oldHas
	composerShellOut = oldOut

	phpstan, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "phpstan"))
	assert.NoError(t, err)
	assert.Contains(t, string(phpstan), filepath.Join("packages", "composer", "phpstan", "phpstan"))
	assert.NotContains(t, string(phpstan), filepath.Join("packages", "composer", "squizlabs"))
	phpcs, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "phpcs"))
	assert.NoError(t, err)
	assert.Contains(t, string(phpcs), filepath.Join("packages", "composer", "squizlabs", "php_codesniffer"))
}

func TestComposerSyncMigratesLegacySharedTree(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderComposer()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppComposerAdd("composer:phpstan/phpstan", "1.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "phpstan", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "composer:phpstan/phpstan"},
		Bin: map[string]string{"phpstan": "phpstan"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "composer.json"), []byte(`{"require":{"phpstan/phpstan":"^1.0.0"}}`), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "composer.lock"), []byte(`{}`), 0644))
	legacyVendor := filepath.Join(p.APP_PACKAGES_DIR, "vendor")
	assert.NoError(t, os.MkdirAll(filepath.Join(legacyVendor, "bin"), 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(legacyVendor, "autoload.php"), []byte("<?php"), 0644))

	oldHas := composerHasCommand
	oldOut := composerShellOut
	composerHasCommand = func(string, []string, []string) bool { return true }
	composerShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		plantComposerInstalled(t, p, "phpstan/phpstan", "phpstan")
		return 0, nil
	}
	assert.True(t, p.Sync())
	composerHasCommand = oldHas
	composerShellOut = oldOut

	_, err := os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "composer.json"))
	assert.Error(t, err)
	_, err = os.Stat(legacyVendor)
	assert.Error(t, err)
	_, err = os.Stat(p.packageDir("phpstan/phpstan"))
	assert.NoError(t, err)
	wrapper, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "phpstan"))
	assert.NoError(t, err)
	assert.Contains(t, string(wrapper), filepath.Join("packages", "composer", "phpstan", "phpstan"))
}

func TestComposerRemoveDeletesContainerAndWrapper(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderComposer()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppComposerAdd("composer:phpstan/phpstan", "1.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "phpstan", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "composer:phpstan/phpstan"},
		Bin: map[string]string{"phpstan": "phpstan"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantComposerInstalled(t, p, "phpstan/phpstan", "phpstan")
	assert.NoError(t, os.WriteFile(filepath.Join(files.GetAppBinPath(), "phpstan"), []byte("wrapper"), 0755))

	oldHas := composerHasCommand
	oldOut := composerShellOut
	composerHasCommand = func(string, []string, []string) bool { return true }
	composerShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Remove("composer:phpstan/phpstan"))
	composerHasCommand = oldHas
	composerShellOut = oldOut

	_, err := os.Stat(p.packageDir("phpstan/phpstan"))
	assert.Error(t, err)
	_, err = os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "phpstan"))
	assert.Error(t, err)
	_, err = os.Lstat(filepath.Join(files.GetAppBinPath(), "phpstan"))
	assert.Error(t, err)
}

package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
)

func plantOpamInstalled(t *testing.T, p *OpamProvider, name, binName string) {
	t.Helper()
	binDir := filepath.Join(p.switchPath(name), "bin")
	assert.NoError(t, os.MkdirAll(binDir, 0755))
	if binName != "" {
		assert.NoError(t, os.WriteFile(filepath.Join(binDir, binName), []byte("#!/bin/sh\n"), 0755))
	}
}

func TestOpamPerPackageIsolationAndWrappers(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderOpam()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppOpamAdd("opam:ocamlformat", "0.26.0")
	_ = lppOpamAdd("opam:merlin", "4.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{
		{Name: "ocamlformat", Version: "0.26.0", Source: registry_parser.RegistryItemSource{ID: "opam:ocamlformat"}, Bin: map[string]string{"ocamlformat": "ocamlformat"}},
		{Name: "merlin", Version: "4.0.0", Source: registry_parser.RegistryItemSource{ID: "opam:merlin"}, Bin: map[string]string{"ocamlmerlin": "ocamlmerlin"}},
	})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantOpamInstalled(t, p, "ocamlformat", "ocamlformat")
	plantOpamInstalled(t, p, "merlin", "ocamlmerlin")

	oldHas := opamHasCommand
	oldOut := opamShellOut
	opamHasCommand = func(string, []string, []string) bool { return true }
	opamShellOut = func(string, []string, string, []string) (int, error) {
		t.Fatal("Sync should skip already-installed opam packages")
		return 1, nil
	}
	assert.True(t, p.Sync())
	opamHasCommand = oldHas
	opamShellOut = oldOut

	ocamlformat, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "ocamlformat"))
	assert.NoError(t, err)
	assert.Contains(t, string(ocamlformat), filepath.Join("packages", "opam", "ocamlformat", "switch"))
	assert.NotContains(t, string(ocamlformat), filepath.Join("packages", "opam", "merlin"))
	merlin, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "ocamlmerlin"))
	assert.NoError(t, err)
	assert.Contains(t, string(merlin), filepath.Join("packages", "opam", "merlin", "switch"))
}

func TestOpamSyncMigratesLegacySharedSwitch(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderOpam()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppOpamAdd("opam:ocamlformat", "0.26.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "ocamlformat", Version: "0.26.0", Source: registry_parser.RegistryItemSource{ID: "opam:ocamlformat"},
		Bin: map[string]string{"ocamlformat": "ocamlformat"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	legacySwitch := filepath.Join(p.APP_PACKAGES_DIR, "switch")
	assert.NoError(t, os.MkdirAll(filepath.Join(legacySwitch, "bin"), 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(legacySwitch, "bin", "ocamlformat"), []byte("#!/bin/sh\n"), 0755))

	oldHas := opamHasCommand
	oldOut := opamShellOut
	opamHasCommand = func(string, []string, []string) bool { return true }
	opamShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) >= 1 && args[0] == "switch" {
			assert.NoError(t, os.MkdirAll(p.switchPath("ocamlformat"), 0755))
			return 0, nil
		}
		plantOpamInstalled(t, p, "ocamlformat", "ocamlformat")
		return 0, nil
	}
	assert.True(t, p.Sync())
	opamHasCommand = oldHas
	opamShellOut = oldOut

	_, err := os.Stat(legacySwitch)
	assert.Error(t, err)
	_, err = os.Stat(p.packageDir("ocamlformat"))
	assert.NoError(t, err)
	wrapper, err := os.ReadFile(filepath.Join(files.GetAppBinPath(), "ocamlformat"))
	assert.NoError(t, err)
	assert.Contains(t, string(wrapper), filepath.Join("packages", "opam", "ocamlformat", "switch"))
}

func TestOpamRemoveDeletesContainerAndWrapper(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderOpam()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppOpamAdd("opam:ocamlformat", "0.26.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "ocamlformat", Version: "0.26.0", Source: registry_parser.RegistryItemSource{ID: "opam:ocamlformat"},
		Bin: map[string]string{"ocamlformat": "ocamlformat"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	plantOpamInstalled(t, p, "ocamlformat", "ocamlformat")
	assert.NoError(t, os.WriteFile(filepath.Join(files.GetAppBinPath(), "ocamlformat"), []byte("wrapper"), 0755))

	oldHas := opamHasCommand
	oldOut := opamShellOut
	opamHasCommand = func(string, []string, []string) bool { return true }
	opamShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Remove("opam:ocamlformat"))
	opamHasCommand = oldHas
	opamShellOut = oldOut

	_, err := os.Stat(p.packageDir("ocamlformat"))
	assert.Error(t, err)
	_, err = os.Lstat(filepath.Join(files.GetAppBinPath(), "ocamlformat"))
	assert.Error(t, err)
}

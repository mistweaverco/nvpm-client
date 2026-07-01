package providers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
)

func TestGolangGeneratePackageJSON_CloseWarningAndEncodeError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Ensure there is at least one package so found == true
	_ = lppGoAdd("pkg:golang/github.com/acme/tool", "v1.0.0")

	// 1) Trigger close warning path
	oldCreate := goCreate
	oldClose := goClose
	goCreate = os.Create
	goClose = func(f *os.File) error { return errors.New("close") }
	assert.True(t, p.generatePackageJSON())
	goClose = oldClose

	// 2) Trigger encode error path by returning a closed file handle
	goCreate = func(path string) (*os.File, error) {
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		_ = f.Close()
		return f, nil
	}
	assert.False(t, p.generatePackageJSON())
	goCreate = oldCreate
}

func TestGolangCreateSymlink_RemovesExistingAndSymlinkError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// registry with bin
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "tool", Version: "v1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/acme/tool"},
		Bin: map[string]string{"tool": "tool"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)

	// ensure golang bin contains the binary
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(gobin, "tool"), []byte(""), 0755))

	// Prepare an existing symlink in nvpm bin to exercise removal path
	zbin := files.GetAppBinPath()
	_ = os.MkdirAll(zbin, 0755)
	pre := filepath.Join(zbin, "tool")
	_ = os.Symlink(filepath.Join(gobin, "tool"), pre)

	// Stub lstat/remove to simulate existing symlink detection and removal
	oldLs := goLstat
	oldRm := goRemove
	oldSym := goSymlink
	calledRemove := 0
	goLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	goRemove = func(path string) error { calledRemove++; return os.Remove(path) }
	goSymlink = os.Symlink
	assert.NoError(t, p.createSymlink("pkg:golang/github.com/acme/tool"))
	assert.GreaterOrEqual(t, calledRemove, 1)

	// Now force symlink creation error
	goSymlink = func(string, string) error { return errors.New("sym") }
	err := p.createSymlink("pkg:golang/github.com/acme/tool")
	assert.Error(t, err)

	goLstat, goRemove, goSymlink = oldLs, oldRm, oldSym
}

func TestGolangRemoveBin_ErrorOnRemove(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// registry with bin
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "tool", Version: "v1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/acme/tool"},
		Bin: map[string]string{"tool": "tool"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)

	// create binary file so goStat finds it
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin, 0755)
	binPath := filepath.Join(gobin, "tool")
	assert.NoError(t, os.WriteFile(binPath, []byte(""), 0755))

	oldRm := goRemove
	goRemove = func(string) error { return errors.New("rm") }
	defer func() { goRemove = oldRm }()

	assert.Error(t, p.removeBin("pkg:golang/github.com/acme/tool"))
}

func TestGolangClean_BinaryRemoveErrorAndLocalRemoveError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Add a desired package
	_ = lppGoAdd("pkg:golang/github.com/acme/tool", "v1.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "tool", Version: "v1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/acme/tool"},
		Bin: map[string]string{"tool": "tool"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)

	// create a binary so Clean tries to remove it and emit error
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(gobin, "tool"), []byte(""), 0755))

	oldRm := goRemove
	goRemove = func(string) error { return errors.New("rm") }
	// Clean should continue despite binary removal error (logs internally)
	assert.True(t, p.Clean())
	goRemove = oldRm

	// Now force local package removal to fail via injectable and expect Clean to return false
	oldLppRemove := lppGoRemove
	lppGoRemove = func(string) error { return errors.New("lppRemove") }
	defer func() { lppGoRemove = oldLppRemove }()
	// Re-add package so Clean iterates and hits lppGoRemove error
	_ = lppGoAdd("pkg:golang/github.com/acme/tool", "v1.0.0")
	assert.False(t, p.Clean())
}

func TestGolangSync_DirMkdirErrorAndModInitError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()

	// 1) Directory creation error
	oldStat := goStat
	oldMkdir := goMkdir
	goStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	goMkdir = func(string, os.FileMode) error { return errors.New("mkdir") }
	assert.False(t, p.Sync())
	goMkdir = oldMkdir
	goStat = oldStat

	// 2) go.mod init error
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// Add a package so packagesFound == true and we reach mod init path
	_ = lppGoAdd("pkg:golang/github.com/acme/tool", "v1.0.0")
	oldStat = goStat
	oldOut := goShellOut
	// Ensure go.mod is missing
	goStat = func(name string) (os.FileInfo, error) {
		if filepath.Base(name) == "go.mod" {
			return nil, os.ErrNotExist
		}
		return oldStat(name)
	}
	// Make go unavailable to force init path, but return error for init call specifically
	goShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) >= 2 && args[0] == "mod" && args[1] == "init" {
			return 1, errors.New("init")
		}
		return 0, nil
	}
	assert.False(t, p.Sync())
	goShellOut = oldOut
	goStat = oldStat
}

func TestGolangInstall_LatestFetchError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	oldCap := goShellOutCapture
	goShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 1, "", errors.New("err") }
	defer func() { goShellOutCapture = oldCap }()

	assert.False(t, p.Install("pkg:golang/github.com/acme/tool", "latest"))
}

func TestGolangSync_InstallErrorSetsAllOkFalse(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// ensure go.mod exists to skip mod init
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "go.mod"), []byte("module nvpm"), 0644))
	// add desired package
	_ = lppGoAdd("pkg:golang/github.com/acme/tool", "v1.0.0")
	// stub goShellOut: go available ok, install fails
	oldOut := goShellOut
	goShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) == 1 && args[0] == "version" {
			return 0, nil
		}
		if len(args) >= 1 && args[0] == "install" {
			return 1, errors.New("install")
		}
		return 0, nil
	}
	defer func() { goShellOut = oldOut }()

	assert.False(t, p.Sync())
}
func TestGolangErrorBranches(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	// registry item with no bin
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "nobin", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/nobin"},
		Bin: map[string]string{},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	assert.Error(t, p.createSymlink("pkg:golang/nobin"))
	assert.Error(t, p.removeSymlink("pkg:golang/nobin"))
	assert.Error(t, p.removeBin("pkg:golang/nobin"))

	// missing binary
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "tool", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/tool"},
		Bin: map[string]string{"tool": "tool"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	assert.Error(t, p.createSymlink("pkg:golang/tool"))

	// Sync go unavailable
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldGo := goShellOut
	goShellOut = func(string, []string, string, []string) (int, error) { return 1, errors.New("x") }
	assert.False(t, p.Sync())
	goShellOut = oldGo
}

func TestMoreBranchesGolang(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Install: add fails -> false
	oldAdd := lppGoAdd
	lppGoAdd = func(string, string) error { return errors.New("add") }
	assert.False(t, p.Install("pkg:golang/mod", "1.0.0"))
	lppGoAdd = oldAdd

	// Update: latest fetch fails -> false
	oldCap := goShellOutCapture
	goShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 1, "", errors.New("err") }
	assert.False(t, p.Update("pkg:golang/mod"))
	goShellOutCapture = oldCap

	// Clean loop path: setup a package and registry with bin; induce remove symlink error
	_ = local_packages_parser.AddLocalPackage("pkg:golang/github.com/acme/tool", "1.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "tool", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/acme/tool"},
		Bin: map[string]string{"tool": "tool"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	oldLs := goLstat
	oldRm := goRemove
	goLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	goRemove = func(string) error { return errors.New("rm") }
	assert.True(t, p.Clean())
	goLstat, goRemove = oldLs, oldRm

	// Sync: go mod init path and install error path
	gm := filepath.Join(p.APP_PACKAGES_DIR, "go.mod")
	_ = os.Remove(gm)
	oldOut := goShellOut
	goShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) >= 2 && args[0] == "mod" && args[1] == "init" {
			return 0, nil
		}
		return 1, errors.New("install")
	}
	// Add desired package
	_ = local_packages_parser.AddLocalPackage("pkg:golang/github.com/a/b", "1.0.0")
	assert.False(t, p.Sync())
	goShellOut = oldOut
}

func TestGolangSyncInstallSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired package
	_ = lppGoAdd("pkg:golang/github.com/x/y", "1.0.0")
	// registry with bin
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "y", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/x/y"},
		Bin: map[string]string{"y": "y"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	// ensure go.mod missing -> mod init path
	_ = os.Remove(filepath.Join(p.APP_PACKAGES_DIR, "go.mod"))
	// pre-create installed binary so createSymlink can find it
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin, 0755)
	_ = os.WriteFile(filepath.Join(gobin, "y"), []byte(""), 0755)
	// go commands succeed
	oldOut := goShellOut
	goShellOut = func(cmd string, args []string, dir string, env []string) (int, error) { return 0, nil }
	assert.True(t, p.Sync())
	goShellOut = oldOut
}

func TestGolangMorePermutations(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// generatePackageJSON: false then true
	assert.False(t, p.generatePackageJSON())
	_ = local_packages_parser.AddLocalPackage("pkg:golang/github.com/x/y", "v1.0.0")
	assert.True(t, p.generatePackageJSON())

	// Sync skip path when installed
	_ = lppGoAdd("pkg:golang/github.com/x/skip", "v1.0.0")
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin, 0755)
	_ = os.WriteFile(filepath.Join(gobin, "skip"), []byte(""), 0755)
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "skip", Version: "v1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/x/skip"},
		Bin: map[string]string{"skip": "skip"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	oldGo := goShellOut
	goShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Sync())
	goShellOut = oldGo

	// getLatestVersion invalid output
	oldCap := goShellOutCapture
	goShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 0, "onlyone", nil }
	_, err := p.getLatestVersion("mod")
	assert.Error(t, err)
	goShellOutCapture = oldCap
}

func TestGolangCreateSymlinkSuccessAndGeneratePackageJSONCreateErrorAndUpdateInvalid(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// createSymlink success
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "tool", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/tool"},
		Bin: map[string]string{"tool": "tool"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(gobin, "tool"), []byte(""), 0755))
	assert.NoError(t, p.createSymlink("pkg:golang/tool"))
	// symlink exists in nvpm bin
	_, err := os.Lstat(filepath.Join(files.GetAppBinPath(), "tool"))
	assert.NoError(t, err)

	// generatePackageJSON create error
	oldCreate := goCreate
	goCreate = func(string) (*os.File, error) { return nil, errors.New("create") }
	assert.False(t, p.generatePackageJSON())
	goCreate = oldCreate

	// Update invalid repo
	assert.False(t, p.Update("pkg:golang/"))
}

func TestGolangCleanMultipleAndInstallLatestSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderGolang()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// add two packages
	_ = lppGoAdd("pkg:golang/github.com/a/one", "v1.0.0")
	_ = lppGoAdd("pkg:golang/github.com/a/two", "v1.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "one", Version: "v1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/a/one"},
		Bin: map[string]string{"one": "one"},
	}, {
		Name: "two", Version: "v1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/a/two"},
		Bin: map[string]string{"two": "two"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin, 0755)
	_ = os.WriteFile(filepath.Join(gobin, "one"), []byte(""), 0755)
	_ = os.WriteFile(filepath.Join(gobin, "two"), []byte(""), 0755)
	assert.True(t, p.Clean())

	// Install latest success
	_ = lppGoAdd("pkg:golang/github.com/a/inst", "latest")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "inst", Version: "v2.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/a/inst"},
		Bin: map[string]string{"inst": "inst"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)
	// pre-create binary so createSymlink can succeed
	_ = os.WriteFile(filepath.Join(gobin, "inst"), []byte(""), 0755)
	oldCap := goShellOutCapture
	oldOut := goShellOut
	goShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 0, "mod v1.0.0 v2.0.0", nil }
	goShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Install("pkg:golang/github.com/a/inst", "latest"))
	goShellOut = oldOut
	goShellOutCapture = oldCap
}
func TestGolangProviderBasicFlows(t *testing.T) {
	_ = withTempNvpmHome(t)

	oldOut := goShellOut
	oldCap := goShellOutCapture
	oldCreate := goCreate
	goShellOut = func(cmd string, args []string, dir string, env []string) (int, error) { return 0, nil }
	goShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		return 0, "module x v1.0.0 v2.0.0", nil
	}
	goCreate = func(name string) (*os.File, error) {
		_ = os.MkdirAll(filepath.Dir(name), 0755)
		return os.Create(name)
	}
	t.Cleanup(func() { goShellOut = oldOut; goShellOutCapture = oldCap; goCreate = oldCreate })

	p := NewProviderGolang()
	assert.Equal(t, "golang", p.PROVIDER_NAME)
	assert.Equal(t, "golang:", p.PREFIX)

	// add local package and registry mapping
	_ = local_packages_parser.AddLocalPackage("pkg:golang/github.com/acme/tool", "1.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name: "tool", Version: "1.0.0", Source: registry_parser.RegistryItemSource{ID: "pkg:golang/github.com/acme/tool"},
		Bin: map[string]string{"tool": "tool"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)

	// precreate binary to allow createSymlink to succeed
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(gobin, "tool"), []byte("#!/bin/sh\n"), 0755))

	// Sync should run and create symlink
	ok := p.Sync()
	assert.True(t, ok)

	// Update path
	ok = p.Update("pkg:golang/github.com/acme/tool")
	assert.True(t, ok)

	// Install latest
	ok = p.Install("pkg:golang/github.com/acme/tool", "latest")
	assert.True(t, ok)

	// Remove
	ok = p.Remove("pkg:golang/github.com/acme/tool")
	assert.True(t, ok)

	// Clean
	ok = p.Clean()
	assert.True(t, ok)

	// create and remove symlink explicitly: ensure binary exists first
	gobin2 := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(gobin2, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(gobin2, "tool"), []byte(""), 0755))
	assert.NoError(t, p.createSymlink("pkg:golang/github.com/acme/tool"))
	// verify symlink exists
	if _, e := os.Lstat(filepath.Join(files.GetAppBinPath(), "tool")); e != nil {
		t.Fatalf("expected symlink to exist: %v", e)
	}
	assert.NoError(t, p.removeSymlink("pkg:golang/github.com/acme/tool"))
}

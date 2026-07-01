package providers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/stretchr/testify/assert"
)

type fakeEntry struct {
	name string
	dir  bool
}

func (f fakeEntry) Name() string               { return f.name }
func (f fakeEntry) IsDir() bool                { return f.dir }
func (f fakeEntry) Type() os.FileMode          { return 0 }
func (f fakeEntry) Info() (os.FileInfo, error) { return fileInfoNow(nil), nil }

func TestMoreBranchesCargo(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Update: latest fetch error
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 1, "", errors.New("err") }
	assert.False(t, p.Update("pkg:cargo/x"))
	cargoShellOutCapture = oldCap

	// getInstalledCrates error path
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 1, "", errors.New("err") }
	m := p.getInstalledCrates()
	assert.Len(t, m, 0)
	cargoShellOutCapture = oldCap
}

func TestCargoLatestResolutionAndUninstallFail(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired with latest
	_ = lppCargoAdd("pkg:cargo/cr", "latest")
	// search returns version
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if len(args) > 0 && args[0] == "search" {
			return 0, "cr = \"1.2.3\"", nil
		}
		if len(args) > 0 && args[0] == "install" {
			return 0, "", nil
		}
		return 0, "", nil
	}
	// uninstall fails logs but proceed
	oldOut := cargoShellOut
	cargoShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) > 0 && args[0] == "uninstall" {
			return 1, errors.New("uninstall")
		}
		return 0, nil
	}
	// create cargo bin dir
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "bin"), 0755)
	assert.True(t, p.Sync())
	assert.True(t, p.Remove("pkg:cargo/cr"))
	cargoShellOut = oldOut
	cargoShellOutCapture = oldCap
}

func TestCargoErrorBranches(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// createSymlinks removal/chmod error
	cargoBin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(cargoBin, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(cargoBin, "mybin"), []byte(""), 0755))
	oldLs, oldRm, oldCh, oldSym := cargoLstat, cargoRemove, cargoChmod, cargoSymlink
	cargoLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	cargoRemove = func(string) error { return errors.New("rm") }
	cargoChmod = func(string, os.FileMode) error { return errors.New("chmod") }
	cargoSymlink = func(string, string) error { return errors.New("sym") }
	assert.NoError(t, os.MkdirAll(files.GetAppBinPath(), 0755))
	assert.NoError(t, p.createSymlinks())
	cargoLstat, cargoRemove, cargoChmod, cargoSymlink = oldLs, oldRm, oldCh, oldSym

	// removeAllSymlinks over real symlink (already covered by other tests), add readlink error by pointing at non-cargo
	zbin := files.GetAppBinPath()
	sl := filepath.Join(zbin, "notcargo")
	_ = os.Symlink(filepath.Join(t.TempDir(), "x"), sl)
	assert.NoError(t, p.removeAllSymlinks())
}

func TestCargoSync_CreateDirErrorAndUnavailable(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	// mkdir error
	oldStat, oldMkdir := cargoStat, cargoMkdir
	cargoStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	cargoMkdir = func(string, os.FileMode) error { return errors.New("mkdir") }
	assert.False(t, p.Sync())
	cargoStat, cargoMkdir = oldStat, oldMkdir

	// cargo unavailable
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return false }
	assert.False(t, p.Sync())
	cargoHasCommand = oldHas
}

func TestCargoSync_InstallErrorSetsAllOkFalse(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired crate not installed
	_ = lppCargoAdd("pkg:cargo/tool", "1.2.3")
	// installed empty map: make ShellOutCapture return no list
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 0, "", nil }
	// install fails
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 1, errors.New("install") }
	assert.False(t, p.Sync())
	cargoShellOut = oldOut
	cargoShellOutCapture = oldCap
}

func TestCargoClean_RemoveAllErrorReturnsFalse(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// make removeAll fail
	oldRemoveAll := cargoRemoveAll
	cargoRemoveAll = func(string) error { return errors.New("rmall") }
	assert.False(t, p.Clean())
	cargoRemoveAll = oldRemoveAll
}

func TestCargoCreateSymlinks_SkipDirAndRemovalSuccessAndSymlinkSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	// prepare cargo bin with a directory and a file
	cbin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(filepath.Join(cbin, "subdir"), 0755)
	_ = os.MkdirAll(cbin, 0755)
	// create a dummy binary file
	bin := filepath.Join(cbin, "mybin")
	_ = os.WriteFile(bin, []byte(""), 0755)
	// ensure nvpm bin has an existing symlink to remove successfully
	zbin := files.GetAppBinPath()
	_ = os.MkdirAll(zbin, 0755)
	sl := filepath.Join(zbin, "mybin")
	_ = os.Symlink(bin, sl)
	// make removal succeed and chmod succeed
	oldRm, oldCh := cargoRemove, cargoChmod
	cargoRemove = func(string) error { return nil }
	cargoChmod = func(string, os.FileMode) error { return nil }
	assert.NoError(t, p.createSymlinks())
	cargoRemove, cargoChmod = oldRm, oldCh
}

func TestCargoCreateSymlinks_ReadDirError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	cbin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(cbin, 0755)
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	assert.Error(t, p.createSymlinks())
	cargoReadDir = oldRD
}

func TestCargoCreateSymlinks_ChmodErrorAfterSymlinkSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	cbin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(cbin, 0755)
	// create a binary
	bin := filepath.Join(cbin, "b1")
	_ = os.WriteFile(bin, []byte(""), 0755)
	// ensure no pre-existing symlink
	_ = os.MkdirAll(files.GetAppBinPath(), 0755)
	oldSym := cargoSymlink
	oldCh := cargoChmod
	cargoSymlink = func(string, string) error { return nil }
	cargoChmod = func(string, os.FileMode) error { return errors.New("chmod") }
	assert.NoError(t, p.createSymlinks())
	cargoChmod = oldCh
	cargoSymlink = oldSym
}

func TestCargoCreateSymlinks_RemoveExistingSymlinkWarning(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	cbin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(cbin, 0755)
	// simulate cargo bin entries via injectable readDir
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return []os.DirEntry{fakeEntry{name: "warnbin", dir: false}}, nil }
	// nvpm bin existing symlink to trigger removal warning
	zbin := files.GetAppBinPath()
	_ = os.MkdirAll(zbin, 0755)
	sl := filepath.Join(zbin, "warnbin")
	_ = os.Symlink(filepath.Join(cbin, "warnbin"), sl)
	oldLs := cargoLstat
	oldRm := cargoRemove
	oldSym := cargoSymlink
	oldCh := cargoChmod
	cargoLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	cargoRemove = func(string) error { return errors.New("rm") }
	cargoSymlink = func(string, string) error { return nil }
	cargoChmod = func(string, os.FileMode) error { return nil }
	assert.NoError(t, p.createSymlinks())
	cargoReadDir = oldRD
	cargoChmod = oldCh
	cargoSymlink = oldSym
	cargoRemove = oldRm
	cargoLstat = oldLs
}

func TestCargoCreateSymlinks_NoBinDirReturnsNil(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	// Do not create bin dir
	assert.NoError(t, p.createSymlinks())
}

func TestCargoRemoveAllSymlinks_NonSymlinkAndReadlinkError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// create a regular file in nvpm bin (non-symlink)
	zbin := files.GetAppBinPath()
	_ = os.MkdirAll(zbin, 0755)
	f := filepath.Join(zbin, "regular")
	_ = os.WriteFile(f, []byte(""), 0644)
	// create a symlink that will cause readlink error
	sl := filepath.Join(zbin, "badlink")
	_ = os.Symlink(filepath.Join(p.APP_PACKAGES_DIR, "bin", "missing"), sl)
	oldRl := cargoReadlink
	cargoReadlink = func(string) (string, error) { return "", errors.New("readlink") }
	assert.NoError(t, p.removeAllSymlinks())
	cargoReadlink = oldRl
}

func TestCargoRemoveAllSymlinks_RemoveWarning(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	// prepare cargo bin and nvpm bin symlink pointing into cargo bin
	cbin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(cbin, 0755)
	zbin := files.GetAppBinPath()
	_ = os.MkdirAll(zbin, 0755)
	sl := filepath.Join(zbin, "s1")
	_ = os.Symlink(filepath.Join(cbin, "s1"), sl)
	oldLs := cargoLstat
	oldRl := cargoReadlink
	oldRm := cargoRemove
	// Use real Lstat so ModeSymlink is detected on the actual symlink we created
	cargoLstat = os.Lstat
	cargoReadlink = func(string) (string, error) { return filepath.Join(cbin, "s1"), nil }
	cargoRemove = func(string) error { return errors.New("rm") }
	assert.NoError(t, p.removeAllSymlinks())
	cargoRemove = oldRm
	cargoReadlink = oldRl
	cargoLstat = oldLs
}

func TestCargoSync_SkipInstalledWithLockUpdateWarningAndCreateSymlinksError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired latest
	_ = lppCargoAdd("pkg:cargo/tool", "latest")
	// installed shows same version 1.2.3 and also ensure getInstalledCrates path is executed first
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if len(args) > 0 && args[0] == "install" && len(args) > 1 && args[1] == "--list" {
			return 0, "tool v1.2.3:", nil
		}
		if len(args) > 0 && args[0] == "search" {
			return 0, "tool = \"1.2.3\"", nil
		}
		return 0, "", nil
	}
	// updating lockfile fails to hit warning path (both when desired was latest and also after successful install)
	oldAdd := lppCargoAdd
	lppCargoAdd = func(string, string) error { return errors.New("update-lock") }
	// createSymlinks error after loop
	call := 0
	oldRD := cargoReadDir
	cargoReadDir = func(path string) ([]os.DirEntry, error) {
		call++
		if call == 1 {
			return []os.DirEntry{}, nil // for createSymlinks in removeAll path elsewhere, just safe
		}
		return nil, errors.New("readdir") // trigger error log in createSymlinks
	}
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	assert.True(t, p.Sync())
	// restore
	cargoHasCommand = oldHas
	cargoReadDir = oldRD
	lppCargoAdd = oldAdd
	cargoShellOutCapture = oldCap
}

func TestCargoSync_LatestResolutionErrorSetsAllOkFalse(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppCargoAdd("pkg:cargo/tool", "latest")
	// search fails
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 1, "", errors.New("search") }
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	// no-op symlinks
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return []os.DirEntry{}, nil }
	assert.False(t, p.Sync())
	cargoReadDir = oldRD
	cargoHasCommand = oldHas
	cargoShellOutCapture = oldCap
}

func TestCargoSync_CreateSymlinksErrorLoggedWhenNoDesired(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	// empty desired
	oldGet := lppCargoGetDataForProvider
	lppCargoGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	// force createSymlinks to error via readDir
	oldRD := cargoReadDir
	// ensure cargo bin dir exists so createSymlinks does not short-circuit
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "bin"), 0755)
	cargoReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	assert.True(t, p.Sync())
	cargoReadDir = oldRD
	lppCargoGetDataForProvider = oldGet
	cargoHasCommand = oldHas
}

func TestCargoInstall_LatestResolveErrorReturnsFalse(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 1, "", errors.New("search") }
	assert.False(t, p.Install("pkg:cargo/tool", "latest"))
	cargoShellOutCapture = oldCap
}

func TestCargoRemove_RemoveAllSymlinksErrorLogs(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	oldLocalRemove := lppCargoRemove
	lppCargoRemove = func(string) error { return nil }
	oldRD := cargoReadDir
	// ensure cargo bin dir exists so later createSymlinks attempts and errors
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "bin"), 0755)
	cargoReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	// ensure Sync afterwards returns true
	oldGet := lppCargoGetDataForProvider
	lppCargoGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	assert.True(t, p.Remove("pkg:cargo/tool"))
	cargoHasCommand = oldHas
	lppCargoGetDataForProvider = oldGet
	cargoReadDir = oldRD
	lppCargoRemove = oldLocalRemove
	cargoShellOut = oldOut
}

func TestCargoSync_InstallLatestSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppCargoAdd("pkg:cargo/tool", "latest")
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if len(args) > 1 && args[0] == "install" && args[1] == "--list" {
			return 0, "", nil
		}
		if len(args) > 0 && args[0] == "search" {
			return 0, "tool = \"1.2.3\"", nil
		}
		return 0, "", nil
	}
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	// make createSymlinks no-op
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return []os.DirEntry{}, nil }
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	assert.True(t, p.Sync())
	cargoHasCommand = oldHas
	cargoReadDir = oldRD
	cargoShellOut = oldOut
	cargoShellOutCapture = oldCap
}

func TestCargoSync_InstallLatestLockUpdateWarning(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired latest so pkg.Version != desiredVersion after resolution
	_ = lppCargoAdd("pkg:cargo/tool", "latest")
	// resolve latest and show not installed
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if len(args) > 1 && args[0] == "install" && args[1] == "--list" {
			return 0, "", nil
		}
		if len(args) > 0 && args[0] == "search" {
			return 0, "tool = \"1.2.3\"", nil
		}
		return 0, "", nil
	}
	// install succeeds
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	// cause post-install lock update to warn
	oldAdd := lppCargoAdd
	lppCargoAdd = func(sourceId, version string) error { return errors.New("update-lock") }
	// make symlink creation no-op
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return []os.DirEntry{}, nil }
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	assert.True(t, p.Sync())
	// restore
	cargoHasCommand = oldHas
	cargoReadDir = oldRD
	lppCargoAdd = oldAdd
	cargoShellOut = oldOut
	cargoShellOutCapture = oldCap
}

func TestCargoInstall_SpecificVersionSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldAdd := lppCargoAdd
	lppCargoAdd = func(string, string) error { return nil }
	oldGet := lppCargoGetDataForProvider
	lppCargoGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	assert.True(t, p.Install("pkg:cargo/tool", "1.2.3"))
	cargoHasCommand = oldHas
	lppCargoGetDataForProvider = oldGet
	lppCargoAdd = oldAdd
}

func TestCargoRemove_LocalRemoveError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	oldLocalRemove := lppCargoRemove
	lppCargoRemove = func(string) error { return errors.New("rm-local") }
	assert.False(t, p.Remove("pkg:cargo/tool"))
	lppCargoRemove = oldLocalRemove
	cargoShellOut = oldOut
}

func TestCargoInstall_InvalidAndLatestAddError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// invalid source id
	assert.False(t, p.Install("pkg:cargo/", "1.0.0"))
	// latest resolves but add fails
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 0, "tool = \"1.2.3\"", nil }
	oldAdd := lppCargoAdd
	lppCargoAdd = func(string, string) error { return errors.New("add") }
	assert.False(t, p.Install("pkg:cargo/tool", "latest"))
	lppCargoAdd = oldAdd
	cargoShellOutCapture = oldCap
}

func TestCargoRemove_InvalidAndCreateSymlinksError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// invalid source id
	assert.False(t, p.Remove("pkg:cargo/"))
	// happy uninstall and local remove, but createSymlinks error path
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	oldLocalRemove := lppCargoRemove
	lppCargoRemove = func(string) error { return nil }
	// readDir stub: first call (removeAllSymlinks) returns empty, second (createSymlinks) errors
	count := 0
	oldRD := cargoReadDir
	cargoReadDir = func(path string) ([]os.DirEntry, error) {
		count++
		if count == 1 {
			return []os.DirEntry{}, nil
		}
		return nil, errors.New("readdir")
	}
	// ensure Sync returns quickly after Remove
	oldGet := lppCargoGetDataForProvider
	lppCargoGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	assert.True(t, p.Remove("pkg:cargo/tool"))
	// restore
	cargoHasCommand = oldHas
	lppCargoGetDataForProvider = oldGet
	cargoReadDir = oldRD
	lppCargoRemove = oldLocalRemove
	cargoShellOut = oldOut
}

func TestCargoRemove_UninstallErrorProceeds(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 1, errors.New("uninstall") }
	oldLocalRemove := lppCargoRemove
	lppCargoRemove = func(string) error { return nil }
	// ensure removeAll and createSymlinks harmless
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return []os.DirEntry{}, nil }
	// ensure Sync returns true
	oldGet := lppCargoGetDataForProvider
	lppCargoGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	assert.True(t, p.Remove("pkg:cargo/tool"))
	cargoHasCommand = oldHas
	lppCargoGetDataForProvider = oldGet
	cargoReadDir = oldRD
	lppCargoRemove = oldLocalRemove
	cargoShellOut = oldOut
}

func TestCargoSync_SkipInvalidRepoEntryAndSpecificInstallSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// add invalid and valid entries
	_ = lppCargoAdd("pkg:cargo/", "1.0.0")
	_ = lppCargoAdd("pkg:cargo/tool", "1.2.3")
	// installed shows old version; force go through install
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if len(args) > 1 && args[0] == "install" && args[1] == "--list" {
			return 0, "tool v1.0.0:", nil
		}
		return 0, "", nil
	}
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	// no-op symlink creation
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return []os.DirEntry{}, nil }
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	assert.True(t, p.Sync())
	cargoHasCommand = oldHas
	cargoReadDir = oldRD
	cargoShellOut = oldOut
	cargoShellOutCapture = oldCap
}

func TestCargoInstall_EmptyVersionResolvesLatestSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 0, "tool = \"1.2.3\"", nil }
	oldAdd := lppCargoAdd
	lppCargoAdd = func(string, string) error { return nil }
	oldGet := lppCargoGetDataForProvider
	lppCargoGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	oldHas := cargoHasCommand
	cargoHasCommand = func(string, []string, []string) bool { return true }
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return []os.DirEntry{}, nil }
	assert.True(t, p.Install("pkg:cargo/tool", ""))
	cargoReadDir = oldRD
	cargoHasCommand = oldHas
	lppCargoGetDataForProvider = oldGet
	lppCargoAdd = oldAdd
	cargoShellOutCapture = oldCap
}

func TestCargoMorePermutations(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Clean success when desired empty and Sync runs
	assert.True(t, p.Clean())

	// getLatestVersion not found
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 0, "no matches", nil }
	_, err := p.getLatestVersion("crate")
	assert.Error(t, err)
	cargoShellOutCapture = oldCap

	// Sync skip installed path
	_ = lppCargoAdd("pkg:cargo/myc", "1.0.0")
	cargoShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 0, "myc v1.0.0: desc", nil }
	assert.True(t, p.Sync())
	cargoShellOutCapture = oldCap

	// Install add failure -> false
	oldAdd := lppCargoAdd
	lppCargoAdd = func(string, string) error { return errors.New("add") }
	assert.False(t, p.Install("pkg:cargo/myc", "1.0.0"))
	lppCargoAdd = oldAdd
}

func TestCargoRepoAndCreateSymlinksSuccessAndCleanHappyAndInvalidUpdateRemove(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// createSymlinks success
	cargoBin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(cargoBin, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(cargoBin, "mybin"), []byte(""), 0755))
	_ = os.MkdirAll(files.GetAppBinPath(), 0755)
	assert.NoError(t, p.createSymlinks())

	// Clean happy path (make Sync return true quickly)
	oldHas := cargoHasCommand
	oldGet := lppCargoGetDataForProvider
	cargoHasCommand = func(string, []string, []string) bool { return true }
	lppCargoGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Clean())
	cargoHasCommand = oldHas
	lppCargoGetDataForProvider = oldGet

	// Invalid repo branches
	assert.False(t, p.Remove("pkg:cargo/"))
	assert.False(t, p.Update("pkg:cargo/"))
}

func TestCargoRemoveAllSymlinksReadDirErrorAndCreateSymlinksNoBinDir(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	// readDir error -> propagate
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	assert.Error(t, p.removeAllSymlinks())
	cargoReadDir = oldRD

	// createSymlinks when cargo bin dir missing -> nil
	assert.NoError(t, p.createSymlinks())
}

func TestCargoCleanWithRemoveAllSymlinksErrorStillProceeds(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// force removeAllSymlinks to error
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	// ensure RemoveAll OK via injectable
	oldRemoveAll := cargoRemoveAll
	cargoRemoveAll = func(string) error { return nil }
	// ensure Sync returns true (cargo available and desired empty)
	oldHas := cargoHasCommand
	oldGet := lppCargoGetDataForProvider
	cargoHasCommand = func(string, []string, []string) bool { return true }
	lppCargoGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Clean())
	// restore
	cargoReadDir = oldRD
	cargoRemoveAll = oldRemoveAll
	cargoHasCommand = oldHas
	lppCargoGetDataForProvider = oldGet
}

func TestCargoSyncMixedDesiredInstalledAndRemoveAllSymlinksSuccessAndWarning(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderCargo()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired alpha fixed, beta latest
	_ = lppCargoAdd("pkg:cargo/alpha", "1.0.0")
	_ = lppCargoAdd("pkg:cargo/beta", "latest")
	// getInstalledCrates shows alpha installed
	oldCap := cargoShellOutCapture
	cargoShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if len(args) > 0 && args[0] == "install" && len(args) > 1 && args[1] == "--list" { // not used
			return 0, "", nil
		}
		if len(args) > 0 && args[0] == "install" {
			return 0, "", nil
		}
		if len(args) > 0 && args[0] == "search" {
			return 0, "beta = \"2.3.4\"", nil
		}
		// default for getInstalledCrates
		return 0, "alpha v1.0.0:", nil
	}
	oldOut := cargoShellOut
	cargoShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	// ensure cargo bin exists with some binary for symlink creation call after Sync
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "bin"), 0755)
	_ = os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "bin", "dummy"), []byte(""), 0755)
	assert.True(t, p.Sync())
	cargoShellOut = oldOut
	cargoShellOutCapture = oldCap

	// removeAllSymlinks success and warning paths
	zbin := files.GetAppBinPath()
	cbin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(zbin, 0755)
	_ = os.MkdirAll(cbin, 0755)
	// one symlink removable successfully
	sl1 := filepath.Join(zbin, "s1")
	_ = os.Symlink(filepath.Join(cbin, "s1"), sl1)
	// one symlink remove warning
	sl2 := filepath.Join(zbin, "s2")
	_ = os.Symlink(filepath.Join(cbin, "s2"), sl2)
	oldLs := cargoLstat
	oldRl := cargoReadlink
	oldRm := cargoRemove
	cargoLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	cargoReadlink = func(path string) (string, error) {
		if path == sl1 {
			return filepath.Join(cbin, "s1"), nil
		}
		return filepath.Join(cbin, "s2"), nil
	}
	cargoRemove = func(path string) error {
		if path == sl2 {
			return errors.New("rm")
		}
		return nil
	}
	assert.NoError(t, p.removeAllSymlinks())
	cargoLstat, cargoReadlink, cargoRemove = oldLs, oldRl, oldRm
}

func TestCargoRemoveAllSymlinksHandlesErrors(t *testing.T) {
	_ = withTempNvpmHome(t)
	// Cargo: removeAllSymlinks handles Lstat err via fake readDir; and Remove error when target under cargo
	cp := NewProviderCargo()
	_ = os.MkdirAll(cp.APP_PACKAGES_DIR, 0755)
	oldRD := cargoReadDir
	cargoReadDir = func(string) ([]os.DirEntry, error) { return []os.DirEntry{fakeEntry{name: "ghost", dir: false}}, nil }
	oldLs := cargoLstat
	oldRl := cargoReadlink
	oldRm := cargoRemove
	cargoLstat = func(string) (os.FileInfo, error) { return nil, errors.New("lstat") }
	assert.NoError(t, cp.removeAllSymlinks())
	// simulate symlink pointing into cargo and remove failing
	cargoReadDir = oldRD
	// create cargo bin and a symlink to it under nvpm bin
	cbin := filepath.Join(cp.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(cbin, 0755)
	zbin := files.GetAppBinPath()
	sl := filepath.Join(zbin, "sym")
	_ = os.Symlink(filepath.Join(cbin, "sym"), sl)
	cargoLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	cargoReadlink = func(string) (string, error) { return filepath.Join(cbin, "sym"), nil }
	cargoRemove = func(string) error { return errors.New("rm") }
	assert.NoError(t, cp.removeAllSymlinks())
	cargoLstat, cargoReadlink, cargoRemove = oldLs, oldRl, oldRm
}
func TestCargoProviderBasicFlows(t *testing.T) {
	_ = withTempNvpmHome(t)

	oldOut := cargoShellOut
	oldCap := cargoShellOutCapture
	oldHas := cargoHasCommand
	cargoShellOut = func(cmd string, args []string, dir string, env []string) (int, error) { return 0, nil }
	cargoShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		if len(args) > 0 && args[0] == "search" {
			return 0, "mycrate = \"1.2.3\"", nil
		}
		return 0, "mycrate v1.0.0: installed binary\n", nil
	}
	cargoHasCommand = func(command string, args []string, env []string) bool { return true }
	t.Cleanup(func() { cargoShellOut = oldOut; cargoShellOutCapture = oldCap; cargoHasCommand = oldHas })

	p := NewProviderCargo()
	assert.Equal(t, "cargo", p.PROVIDER_NAME)
	assert.Equal(t, "cargo:", p.PREFIX)

	// ensure provider packages dir exists
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// add local package
	_ = local_packages_parser.AddLocalPackage("pkg:cargo/mycrate", "latest")

	// create cargo bin dir with a binary for symlink creation
	cargoBin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	_ = os.MkdirAll(cargoBin, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(cargoBin, "mycrate"), []byte(""), 0755))

	// create a symlink in nvpm bin that points to cargo bin for removeAllSymlinks
	zbin := files.GetAppBinPath()
	sl := filepath.Join(zbin, "mycrate")
	_ = os.Symlink(filepath.Join(cargoBin, "mycrate"), sl)

	// getInstalledCrates
	crates := p.getInstalledCrates()
	assert.Equal(t, "1.0.0", crates["mycrate"]) // from stubbed capture

	// Sync
	ok := p.Sync()
	assert.True(t, ok)

	// Install latest
	ok = p.Install("pkg:cargo/mycrate", "latest")
	assert.True(t, ok)

	// Update
	ok = p.Update("pkg:cargo/mycrate")
	assert.True(t, ok)

	// Remove
	ok = p.Remove("pkg:cargo/mycrate")
	assert.True(t, ok)

	// Clean
	ok = p.Clean()
	assert.True(t, ok)

	// Remove again on missing to hit uninstall non-critical path
	ok = p.Remove("pkg:cargo/mycrate")
	assert.True(t, ok)
}

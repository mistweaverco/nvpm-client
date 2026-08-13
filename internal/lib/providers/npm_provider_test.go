package providers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
)

func plantNpmInstalled(t *testing.T, p *NPMProvider, name, version, binName string) {
	t.Helper()
	nm := filepath.Join(p.packageDir(name), "node_modules")
	pkgDir := filepath.Join(nm, name)
	assert.NoError(t, os.MkdirAll(filepath.Join(nm, ".bin"), 0755))
	assert.NoError(t, os.MkdirAll(pkgDir, 0755))
	pkgJSON := `{"name":"` + name + `","version":"` + version + `"}`
	if binName != "" {
		pkgJSON = `{"name":"` + name + `","version":"` + version + `","bin":{"` + binName + `":"./bin/` + binName + `.js"}}`
		assert.NoError(t, os.WriteFile(filepath.Join(nm, ".bin", binName), []byte(""), 0755))
	}
	assert.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(pkgJSON), 0644))
}

func TestNPMErrorBranches(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// readPackageJSON error
	oldRF := npmReadFile
	npmReadFile = func(string) ([]byte, error) { return nil, errors.New("boom") }
	_, err := p.readPackageJSON(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "x"))
	assert.Error(t, err)
	npmReadFile = oldRF

	// hasPackageJSONChanged branches
	oldStat := npmStat
	// package.json missing
	npmStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	assert.True(t, p.hasPackageJSONChanged())
	// lock missing
	npmStat = func(name string) (os.FileInfo, error) {
		if filepath.Base(name) == "package.json" {
			return fileInfoNow(t), nil
		}
		return nil, os.ErrNotExist
	}
	assert.True(t, p.hasPackageJSONChanged())
	// pkgStat error
	npmStat = func(name string) (os.FileInfo, error) {
		if filepath.Base(name) == "package.json" {
			return nil, errors.New("err")
		}
		return fileInfoNow(t), nil
	}
	assert.True(t, p.hasPackageJSONChanged())
	// lockStat error
	npmStat = func(name string) (os.FileInfo, error) {
		if filepath.Base(name) == "package.json" {
			return fileInfoNow(t), nil
		}
		return nil, errors.New("err")
	}
	assert.True(t, p.hasPackageJSONChanged())
	npmStat = oldStat

	// tryNpmCi no lock
	oldStat2 := npmStat
	npmStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	assert.False(t, p.tryNpmCi())
	npmStat = oldStat2

	// createPackageSymlinks symlink error and chmod error branches
	plantNpmInstalled(t, p, "pkg", "1.0.0", "tool")

	// existing symlink removal error
	oldL := npmLstat
	oldRm := npmRemove
	oldSym := npmSymlink
	oldCh := npmChmod
	npmLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	npmRemove = func(string) error { return errors.New("rmerr") }
	npmSymlink = func(oldname, newname string) error { return errors.New("symerr") }
	_ = p.createPackageSymlinks("pkg")

	// symlink ok, chmod error
	npmSymlink = func(oldname, newname string) error { return nil }
	npmChmod = func(string, os.FileMode) error { return errors.New("chmoderr") }
	err = p.createPackageSymlinks("pkg")
	assert.NoError(t, err)
	npmLstat, npmRemove, npmSymlink, npmChmod = oldL, oldRm, oldSym, oldCh

	// removePackageSymlinks removal error
	oldL = npmLstat
	oldRm = npmRemove
	npmLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	npmRemove = func(string) error { return errors.New("rmerr") }
	assert.NoError(t, p.removePackageSymlinks("pkg"))
	npmLstat, npmRemove = oldL, oldRm

	// removeAllSymlinks readDir error
	oldRD := npmReadDir
	npmReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	assert.Error(t, p.removeAllSymlinks())
	npmReadDir = oldRD

	// Clean dir remove error -> false
	oldRA := npmRemoveAll
	npmRemoveAll = func(string) error { return errors.New("rmalldir") }
	assert.False(t, p.Clean())
	npmRemoveAll = oldRA
}

func TestPushingRemainingBranchesTo100(t *testing.T) {
	_ = withTempNvpmHome(t)

	// NPM: Sync fast path (lock newer, all installed, no changes) and needsUpdate path with ci success
	np := NewProviderNPM()
	_ = os.MkdirAll(np.APP_PACKAGES_DIR, 0755)
	_ = local_packages_parser.AddLocalPackage("pkg:npm/a", "1.0.0")
	plantNpmInstalled(t, np, "a", "1.0.0", "a")
	assert.True(t, np.Sync())

	// needsUpdate: change desired version and simulate ci success
	_ = local_packages_parser.AddLocalPackage("pkg:npm/a", "2.0.0")
	oldOut := npmShellOut
	npmShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, np.Sync())
	npmShellOut = oldOut

	// NPM: Remove returns false when local RemoveLocalPackage fails
	oldRemoveLocal := lppRemove
	lppRemove = func(string) error { return errors.New("x") }
	assert.False(t, np.Remove("pkg:npm/a"))
	lppRemove = oldRemoveLocal

	// PyPI: Clean removeAll error and then success
	pp := NewProviderPyPi()
	_ = os.MkdirAll(pp.APP_PACKAGES_DIR, 0755)
	oldRA := pipRemoveAll
	pipRemoveAll = func(string) error { return errors.New("x") }
	assert.False(t, pp.Clean())
	pipRemoveAll = oldRA

	// Golang: Remove returns false when local RemoveLocalPackage fails
	gp := NewProviderGolang()
	_ = os.MkdirAll(gp.APP_PACKAGES_DIR, 0755)
	oldRemoveLocal2 := lppGoRemove
	lppGoRemove = func(string) error { return errors.New("x") }
	assert.False(t, gp.Remove("pkg:golang/x"))
	lppGoRemove = oldRemoveLocal2

	// Cargo: Remove returns false when local RemoveLocalPackage fails
	cp := NewProviderCargo()
	_ = os.MkdirAll(cp.APP_PACKAGES_DIR, 0755)
	oldRemoveLocal3 := lppCargoRemove
	lppCargoRemove = func(string) error { return errors.New("x") }
	assert.False(t, cp.Remove("pkg:cargo/x"))
	lppCargoRemove = oldRemoveLocal3
}

func TestMoreBranchesNPM(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Install: latest version fetch fails -> false
	oldCap := npmShellOutCapture
	npmShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 1, "", errors.New("err") }
	assert.False(t, p.Install("pkg:npm/pkg", "latest"))
	npmShellOutCapture = oldCap

	// Install: add local package fails -> false
	oldAdd := lppAdd
	lppAdd = func(string, string) error { return errors.New("add") }
	assert.False(t, p.Install("pkg:npm/pkg", "1.0.0"))
	lppAdd = oldAdd

	// Update: repo empty -> false
	assert.False(t, p.Update("pkg:npm/"))

	// removeAllSymlinks success path
	_ = os.MkdirAll(files.GetAppBinPath(), 0755)
	assert.NoError(t, p.removeAllSymlinks())
}

func TestNPMNeedsUpdateCiFailThenInstallIndividually(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "2.0.0")
	dir := p.packageDir("a")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"dependencies":{"a":{"version":"1.0.0"}}}`), 0644)
	oldOut := npmShellOut
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) > 0 && args[0] == "ci" {
			return 1, errors.New("ci")
		}
		return 0, nil
	}
	assert.True(t, p.Sync())
	npmShellOut = oldOut
}

func TestNPMAllConditionalsToggle(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Case 1: no packages -> Sync returns true
	oldGet := lppGetDataForProvider
	lppGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Sync())
	lppGetDataForProvider = oldGet

	// Setup desired package a@2.0.0 not yet installed, with a lockfile so ci is attempted
	_ = lppAdd("pkg:npm/a", "2.0.0")
	dir := p.packageDir("a")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"dependencies":{"a":{"version":"1.0.0"}}}`), 0644)

	// Case 2: ci success -> returns true
	oldOut := npmShellOut
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) > 0 && args[0] == "ci" {
			return 0, nil
		}
		return 0, nil
	}
	assert.True(t, p.Sync())
	npmShellOut = oldOut

	// Case 3: Installing individually path with install failure -> returns false
	_ = os.Remove(filepath.Join(dir, "package-lock.json"))
	_ = lppAdd("pkg:npm/b", "1.0.0")
	plantNpmInstalled(t, p, "b", "0.9.0", "b")
	oldOut2 := npmShellOut
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		return 1, errors.New("install")
	}
	assert.False(t, p.Sync())
	npmShellOut = oldOut2
}

func TestNPMGeneratePackageJSONSkipsNonNpmAndCloseErrorAndEncodeError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Add a non-npm package and an npm package; ensure skip happens and found==true
	_ = local_packages_parser.AddLocalPackage("pkg:pypi/black", "1.0.0")
	_ = local_packages_parser.AddLocalPackage("pkg:npm/pkg", "1.2.3")
	assert.True(t, p.generatePackageJSON())

	// Encode error path: replace file with a directory so encoder.Encode fails to write
	oldCreate := npmCreate
	npmCreate = func(path string) (*os.File, error) {
		// create a directory at the path to cause encode error when opening file descriptor invalid
		_ = os.MkdirAll(path, 0755)
		return nil, errors.New("open as dir")
	}
	assert.False(t, p.generatePackageJSON())
	npmCreate = oldCreate

	// Close error path via injectable close
	filePath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	f, err := os.Create(filePath)
	assert.NoError(t, err)
	_ = f.Close()
	oldClose := npmClose
	npmClose = func(*os.File) error { return errors.New("close") }
	// generate writes and then triggers close warning; still returns true since found
	assert.True(t, p.generatePackageJSON())
	npmClose = oldClose

	// Encode error path: return a closed file so encoder writes fail
	oldCreate2 := npmCreate
	npmCreate = func(path string) (*os.File, error) {
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		_ = f.Close()
		return f, nil
	}
	assert.False(t, p.generatePackageJSON())
	npmCreate = oldCreate2
}

func TestNPMRemoveAllSymlinksWarnOnRemove(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(files.GetAppBinPath(), 0755)
	// create a dummy entry in bin
	f := filepath.Join(files.GetAppBinPath(), "dummy")
	assert.NoError(t, os.WriteFile(f, []byte(""), 0644))
	oldLs, oldRm := npmLstat, npmRemove
	npmLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	npmRemove = func(string) error { return errors.New("rm") }
	assert.NoError(t, p.removeAllSymlinks())
	npmLstat, npmRemove = oldLs, oldRm
}

func TestNPMCleanLogsErrorOnRemoveSymlinks(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	oldRD, oldRA, oldStat, oldMkdirAll, oldGet := npmReadDir, npmRemoveAll, npmStat, npmMkdirAll, lppGetDataForProvider
	// make removeAllSymlinks error
	npmReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	// allow directory remove to succeed
	npmRemoveAll = func(string) error { return nil }
	// Sync path: make directory creation succeed and no packages found
	npmStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	npmMkdirAll = func(string, os.FileMode) error { return nil }
	lppGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Clean())
	// restore
	npmReadDir, npmRemoveAll, npmStat, npmMkdirAll, lppGetDataForProvider = oldRD, oldRA, oldStat, oldMkdirAll, oldGet
}

func TestNPMSyncCreateDirError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	oldStat, oldMkdirAll := npmStat, npmMkdirAll
	npmStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	npmMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir") }
	assert.False(t, p.Sync())
	npmStat, npmMkdirAll = oldStat, oldMkdirAll
}

func TestNPMFastPathLogsCreateSymlinkErrors(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "1.0.0")
	plantNpmInstalled(t, p, "a", "1.0.0", "a")
	oldSym := npmSymlink
	npmSymlink = func(string, string) error { return errors.New("sym") }
	assert.True(t, p.Sync())
	npmSymlink = oldSym
}

func TestNPMInstallAndRemovePermutations(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// getInstalledPackagesFromLock cases
	assert.Equal(t, 0, len(p.getInstalledPackagesFromLock(filepath.Join(p.APP_PACKAGES_DIR, "missing.json"))))
	bad := filepath.Join(p.APP_PACKAGES_DIR, "bad.json")
	_ = os.WriteFile(bad, []byte("not-json"), 0644)
	assert.Equal(t, 0, len(p.getInstalledPackagesFromLock(bad)))
	good := filepath.Join(p.APP_PACKAGES_DIR, "good.json")
	_ = os.WriteFile(good, []byte(`{"dependencies":{"x":{"version":"1.2.3"}}}`), 0644)
	mp := p.getInstalledPackagesFromLock(good)
	assert.Equal(t, "1.2.3", mp["x"])

	// isPackageInstalled false when dir missing
	assert.False(t, p.isPackageInstalled("none", "1.0.0"))
	// false when readPackageJSON fails
	dir := filepath.Join(p.packageDir("broken"), "node_modules", "broken")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte("{"), 0644)
	assert.False(t, p.isPackageInstalled("broken", "1.0.0"))
	// true when versions match
	plantNpmInstalled(t, p, "ok", "1.0.0", "")
	assert.True(t, p.isPackageInstalled("ok", "1.0.0"))

	// hasPackageJSONChanged false when lock newer
	pkgPath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(pkgPath, []byte("{}"), 0644)
	_ = os.WriteFile(lock, []byte("{}"), 0644)
	now := time.Now()
	_ = os.Chtimes(lock, now.Add(2*time.Hour), now.Add(2*time.Hour))
	assert.False(t, p.hasPackageJSONChanged())

	// createPackageSymlinks chmod success
	plantNpmInstalled(t, p, "pkg", "1.0.0", "cli")
	oldCh := npmChmod
	npmChmod = func(string, os.FileMode) error { return nil }
	assert.NoError(t, p.createPackageSymlinks("pkg"))
	npmChmod = oldCh

	// removePackageSymlinks removal success
	oldLs := npmLstat
	oldRm := npmRemove
	npmLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	npmRemove = func(string) error { return nil }
	assert.NoError(t, p.removePackageSymlinks("pkg"))
	npmLstat, npmRemove = oldLs, oldRm

	// Install success returns true even if createPackageSymlinks fails afterwards
	oldGet := lppGetDataForProvider
	oldOut := npmShellOut
	lppGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	npmShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Install("pkg:npm/pkg", "1.0.0"))
	lppGetDataForProvider = oldGet
	npmShellOut = oldOut

	// Remove success (lppRemove ok) with Sync returning true from empty desired
	assert.True(t, p.Remove("pkg:npm/pkg"))
}

func TestGetRepoAllProviders(t *testing.T) {
	_ = withTempNvpmHome(t)

	np := NewProviderNPM()
	assert.Equal(t, "pkg", np.getRepo("pkg:npm/pkg"))
	assert.Equal(t, "", np.getRepo("invalid"))

	pp := NewProviderPyPi()
	assert.Equal(t, "black", pp.getRepo("pkg:pypi/black"))
	assert.Equal(t, "", pp.getRepo("pkg:pypi/"))

	gp := NewProviderGolang()
	assert.Equal(t, "github.com/x/y", gp.getRepo("pkg:golang/github.com/x/y"))
	assert.Equal(t, "", gp.getRepo("pkg:golang/"))
	// non-matching prefix (missing trailing slash) exercises the else-branch returning empty
	assert.Equal(t, "", gp.getRepo("pkg:golang"))

	cp := NewProviderCargo()
	assert.Equal(t, "crate", cp.getRepo("pkg:cargo/crate"))
	assert.Equal(t, "", cp.getRepo("pkg:cargo/"))
	assert.Equal(t, "", cp.getRepo("invalid"))
}

func TestNPMGeneratePackageJSONCreateErrorAndCleanHappy(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// generatePackageJSON create error
	oldCreate := npmCreate
	npmCreate = func(string) (*os.File, error) { return nil, errors.New("create") }
	assert.False(t, p.generatePackageJSON())
	npmCreate = oldCreate

	// Clean happy path: removeAll ok, remove dir ok, Sync returns true because no packages
	oldRmAll := npmRemoveAll
	oldStat := npmStat
	oldMkdirAll := npmMkdirAll
	oldGet := lppGetDataForProvider
	npmRemoveAll = func(string) error { return nil }
	npmStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	npmMkdirAll = func(string, os.FileMode) error { return nil }
	lppGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Clean())
	// restore
	npmRemoveAll = oldRmAll
	npmStat = oldStat
	npmMkdirAll = oldMkdirAll
	lppGetDataForProvider = oldGet
}

func TestNPMFastPathMultiPackageSymlinkLoop(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	plantNpmInstalled(t, p, "a", "1.0.0", "a")
	plantNpmInstalled(t, p, "b", "2.0.0", "b")
	assert.True(t, p.Sync())
}

func TestNPMFastPathMultiPackageSymlinkLoopWithSymlinkErrors(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	plantNpmInstalled(t, p, "a", "1.0.0", "a")
	plantNpmInstalled(t, p, "b", "2.0.0", "b")
	oldSym := npmSymlink
	npmSymlink = func(string, string) error { return errors.New("sym") }
	assert.True(t, p.Sync())
	npmSymlink = oldSym
}

func TestNPMFastPathMultiPackageSymlinkLoopSuccess(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	plantNpmInstalled(t, p, "a", "1.0.0", "a")
	plantNpmInstalled(t, p, "b", "2.0.0", "b")
	oldCh := npmChmod
	npmChmod = func(string, os.FileMode) error { return nil }
	assert.True(t, p.Sync())
	npmChmod = oldCh
}

func TestNPMFastPathSecondLoopAllInstalled(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	plantNpmInstalled(t, p, "a", "1.0.0", "a")
	plantNpmInstalled(t, p, "b", "2.0.0", "b")
	oldCh := npmChmod
	npmChmod = func(string, os.FileMode) error { return nil }
	assert.True(t, p.Sync())
	npmChmod = oldCh
}

func TestNPMFastPathAllInstalledCallsSymlinkForEachPackage(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	plantNpmInstalled(t, p, "a", "1.0.0", "a")
	plantNpmInstalled(t, p, "b", "2.0.0", "b")

	called := map[string]int{}
	oldSym := npmSymlink
	npmSym := func(oldname, newname string) error {
		base := filepath.Base(newname)
		called[base]++
		return nil
	}
	npmSymlink = npmSym
	oldCh := npmChmod
	npmChmod = func(string, os.FileMode) error { return nil }

	assert.True(t, p.Sync())
	assert.GreaterOrEqual(t, called["a"], 1)
	assert.GreaterOrEqual(t, called["b"], 1)

	npmChmod = oldCh
	npmSymlink = oldSym
}

func TestNPMFastPathAllInstalledMixedSymlinkSuccessAndError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	plantNpmInstalled(t, p, "a", "1.0.0", "a")
	bn := filepath.Join(p.packageDir("b"), "node_modules", "b")
	_ = os.MkdirAll(bn, 0755)
	_ = os.WriteFile(filepath.Join(bn, "package.json"), []byte("{"), 0644)
	oldCh := npmChmod
	npmChmod = func(string, os.FileMode) error { return nil }
	oldOut := npmShellOut
	npmShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Sync())
	npmShellOut = oldOut
	npmChmod = oldCh
}

func TestNPMUpdateLatestFetchFail(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	oldCap := npmShellOutCapture
	npmShellOutCapture = func(string, []string, string, []string) (int, string, error) { return 1, "", errors.New("err") }
	assert.False(t, p.Update("pkg:npm/x"))
	npmShellOutCapture = oldCap
}

func TestNPMSkipPathSymlinkError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "1.0.0")
	plantNpmInstalled(t, p, "a", "1.0.0", "a")
	oldSym := npmSymlink
	npmSymlink = func(string, string) error { return errors.New("sym") }
	assert.True(t, p.Sync())
	npmSymlink = oldSym
}

func TestNPMInstallPostSyncSymlinkError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// Ensure Sync will return true quickly by faking no packages
	oldGetProv := lppGetDataForProvider
	lppGetDataForProvider = func(string) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	// Force symlink creation error in Install's post-sync step
	oldSym := npmSymlink
	npmSymlink = func(string, string) error { return errors.New("sym") }
	// Call Install with a specific package
	assert.True(t, p.Install("pkg:npm/post", "1.0.0"))
	// restore
	npmSymlink = oldSym
	lppGetDataForProvider = oldGetProv
}

func TestNPMRemoveLogsSymlinkRemovalError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	plantNpmInstalled(t, p, "pkg", "1.0.0", "cli")
	oldLs, oldRm := npmLstat, npmRemove
	npmLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	npmRemove = func(string) error { return errors.New("rm") }
	oldLR := lppRemove
	lppRemove = func(string) error { return nil }
	assert.True(t, p.Remove("pkg:npm/pkg"))
	lppRemove = oldLR
	npmLstat, npmRemove = oldLs, oldRm
}

func TestNPMInstallIndividualPathSymlinkError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/d", "1.0.0")
	oldOut := npmShellOut
	npmShellOut = func(string, []string, string, []string) (int, error) { return 0, nil }
	assert.True(t, p.Sync())
	npmShellOut = oldOut
}

func TestNPMProviderBasicFlows(t *testing.T) {
	_ = withTempNvpmHome(t)

	// stub shell helpers
	oldOut := npmShellOut
	oldCap := npmShellOutCapture
	oldCreate := npmCreate
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) { return 0, nil }
	npmShellOutCapture = func(cmd string, args []string, dir string, env []string) (int, string, error) {
		return 0, "1.2.3\n", nil
	}
	npmCreate = func(name string) (*os.File, error) {
		_ = os.MkdirAll(filepath.Dir(name), 0755)
		return os.Create(name)
	}
	t.Cleanup(func() { npmShellOut = oldOut; npmShellOutCapture = oldCap; npmCreate = oldCreate })

	p := NewProviderNPM()
	assert.Equal(t, "npm", p.PROVIDER_NAME)
	assert.Equal(t, "npm:", p.PREFIX)

	// ensure provider packages dir exists
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// getRepo
	assert.Equal(t, "eslint", p.getRepo("pkg:npm/eslint"))

	// generatePackageJSON with no packages
	ok := p.generatePackageJSON()
	assert.False(t, ok)

	// add a local npm package and generate again
	_ = local_packages_parser.AddLocalPackage("pkg:npm/eslint", "1.0.0")
	ok = p.generatePackageJSON()
	assert.True(t, ok)

	// create node_modules package.json for bin
	plantNpmInstalled(t, p, "eslint", "1.0.0", "eslint")

	// isPackageInstalled true/false
	assert.True(t, p.isPackageInstalled("eslint", "1.0.0"))
	assert.False(t, p.isPackageInstalled("eslint", "2.0.0"))

	// createPackageSymlinks
	assert.NoError(t, p.createPackageSymlinks("eslint"))

	// removePackageSymlinks
	assert.NoError(t, p.removePackageSymlinks("eslint"))

	// getInstalledPackagesFromLock
	lock := filepath.Join(p.packageDir("eslint"), "package-lock.json")
	lockData := `{"dependencies":{"eslint":{"version":"1.0.0"}}}`
	assert.NoError(t, os.WriteFile(lock, []byte(lockData), 0644))
	inst := p.getInstalledPackagesFromLock(lock)
	assert.Equal(t, "1.0.0", inst["eslint"])
	pkgPath := filepath.Join(p.packageDir("eslint"), "package.json")
	assert.NoError(t, os.WriteFile(pkgPath, []byte("{}"), 0644))
	now := time.Now()
	_ = os.Chtimes(lock, now.Add(1*time.Hour), now.Add(1*time.Hour))

	assert.True(t, p.tryNpmCiIn(p.packageDir("eslint")))
	// failure path
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) { return 1, nil }
	assert.False(t, p.tryNpmCi())
	// reset to success before Sync and later flows
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) { return 0, nil }

	// Sync reaches install individually path (node_modules has correct content, installs ok)
	ok = p.Sync()
	assert.True(t, ok)

	// Install with latest
	ok = p.Install("pkg:npm/eslint", "latest")
	assert.True(t, ok)

	// Update
	ok = p.Update("pkg:npm/eslint")
	assert.True(t, ok)

	// Clean
	ok = p.Clean()
	assert.True(t, ok)

	// Remove
	ok = p.Remove("pkg:npm/eslint")
	assert.True(t, ok)

	// hasPackageJSONChanged scenarios after flows
	// now make package newer to toggle path
	_ = os.Chtimes(pkgPath, now.Add(2*time.Hour), now.Add(2*time.Hour))
	assert.True(t, p.hasPackageJSONChanged())
}

func TestNPMCustomBinFieldUnmarshal(t *testing.T) {
	var cbf CustomBinField
	// string case
	err := cbf.UnmarshalJSON([]byte(`"./bin/cli.js"`))
	assert.NoError(t, err)
	// map case
	err = cbf.UnmarshalJSON([]byte(`{"foo":"./bin/foo.js"}`))
	assert.NoError(t, err)
	// invalid type
	err = cbf.UnmarshalJSON([]byte(`123`))
	assert.Error(t, err)
}

func TestFirstNonNoticeLine(t *testing.T) {
	assert.Equal(t, "1.2.3", firstNonNoticeLine("1.2.3\n"))
	assert.Equal(t, "1.2.3", firstNonNoticeLine("npm notice New version\n1.2.3\nnpm notice\n"))
	assert.Equal(t, "", firstNonNoticeLine("npm notice only\n"))
	assert.Equal(t, "", firstNonNoticeLine(""))
}

func TestNPMWritePackageJSONIncludesLockExtras(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	dir := p.packageDir("svelte-language-server")
	assert.NoError(t, os.MkdirAll(dir, 0755))
	extras := &local_packages_parser.PackageExtras{
		ExtraPackages: []local_packages_parser.ExtraPackagePin{
			{ID: "npm:typescript-svelte-plugin", Version: "0.3.52"},
			{ID: "npm:@astrojs/ts-plugin"},
		},
	}
	assert.True(t, p.writePackageJSON(dir, "svelte-language-server", "0.18.3", extras))
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	assert.NoError(t, err)
	body := string(data)
	assert.Contains(t, body, `"svelte-language-server": "0.18.3"`)
	assert.Contains(t, body, `"typescript-svelte-plugin": "0.3.52"`)
	assert.Contains(t, body, `"@astrojs/ts-plugin": "*"`)
}

func TestNPMPerPackageIsolationAndBinTargets(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = os.MkdirAll(files.GetAppBinPath(), 0755)

	_ = lppAdd("npm:svelte-language-server", "0.16.0")
	_ = lppAdd("npm:typescript", "7.0.0")
	plantNpmInstalled(t, p, "svelte-language-server", "0.16.0", "svelteserver")
	plantNpmInstalled(t, p, "typescript", "7.0.0", "tsc")
	// Conflicting peer: svelte-language-server keeps typescript@6 in its own tree.
	peer := filepath.Join(p.packageDir("svelte-language-server"), "node_modules", "typescript")
	_ = os.MkdirAll(peer, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(peer, "package.json"), []byte(`{"name":"typescript","version":"6.0.0"}`), 0644))

	assert.True(t, p.Sync())

	svelteTS, err := os.ReadFile(filepath.Join(peer, "package.json"))
	assert.NoError(t, err)
	assert.Contains(t, string(svelteTS), `"6.0.0"`)
	topTS, err := os.ReadFile(filepath.Join(p.packageDir("typescript"), "node_modules", "typescript", "package.json"))
	assert.NoError(t, err)
	assert.Contains(t, string(topTS), `"7.0.0"`)

	svelteserver, err := os.Readlink(filepath.Join(files.GetAppBinPath(), "svelteserver"))
	assert.NoError(t, err)
	assert.Contains(t, svelteserver, filepath.Join("packages", "npm", "svelte-language-server", "node_modules", ".bin", "svelteserver"))
	tsc, err := os.Readlink(filepath.Join(files.GetAppBinPath(), "tsc"))
	assert.NoError(t, err)
	assert.Contains(t, tsc, filepath.Join("packages", "npm", "typescript", "node_modules", ".bin", "tsc"))
}

func TestNPMScopedPackageContainer(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	name := "@vue/language-server"
	_ = lppAdd("npm:"+name, "2.0.0")
	plantNpmInstalled(t, p, name, "2.0.0", "vue-language-server")
	assert.True(t, p.Sync())
	_, err := os.Stat(p.packageDir(name))
	assert.NoError(t, err)
	link, err := os.Readlink(filepath.Join(files.GetAppBinPath(), "vue-language-server"))
	assert.NoError(t, err)
	assert.Contains(t, link, filepath.Join("packages", "npm", "@vue", "language-server"))
}

func TestNPMSyncMigratesLegacySharedTree(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = os.MkdirAll(files.GetAppBinPath(), 0755)
	_ = lppAdd("npm:typescript", "7.0.0")

	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package.json"), []byte(`{"dependencies":{"typescript":"7.0.0"}}`), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json"), []byte(`{"dependencies":{"typescript":{"version":"7.0.0"}}}`), 0644))
	legacyNM := filepath.Join(p.APP_PACKAGES_DIR, "node_modules")
	_ = os.MkdirAll(filepath.Join(legacyNM, "typescript"), 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(legacyNM, "typescript", "package.json"), []byte(`{"name":"typescript","version":"7.0.0","bin":{"tsc":"./bin/tsc"}}`), 0644))
	_ = os.MkdirAll(filepath.Join(legacyNM, ".bin"), 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(legacyNM, ".bin", "tsc"), []byte(""), 0755))
	legacyLink := filepath.Join(files.GetAppBinPath(), "tsc")
	assert.NoError(t, os.Symlink(filepath.Join(legacyNM, ".bin", "tsc"), legacyLink))

	oldOut := npmShellOut
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		plantNpmInstalled(t, p, "typescript", "7.0.0", "tsc")
		return 0, nil
	}
	assert.True(t, p.Sync())
	npmShellOut = oldOut

	_, err := os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "package.json"))
	assert.Error(t, err)
	_, err = os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json"))
	assert.Error(t, err)
	_, err = os.Stat(legacyNM)
	assert.Error(t, err)
	_, err = os.Stat(p.packageDir("typescript"))
	assert.NoError(t, err)

	target, err := os.Readlink(legacyLink)
	assert.NoError(t, err)
	assert.Contains(t, target, filepath.Join("packages", "npm", "typescript", "node_modules", ".bin", "tsc"))
}

func TestNPMInstallMigratesPackageIntoContainer(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = os.MkdirAll(files.GetAppBinPath(), 0755)

	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package.json"), []byte(`{}`), 0644))
	legacyNM := filepath.Join(p.APP_PACKAGES_DIR, "node_modules")
	_ = os.MkdirAll(filepath.Join(legacyNM, "typescript"), 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(legacyNM, "typescript", "package.json"), []byte(`{"name":"typescript","version":"5.0.0"}`), 0644))

	oldOut := npmShellOut
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		plantNpmInstalled(t, p, "typescript", "7.0.0", "tsc")
		return 0, nil
	}
	assert.True(t, p.Install("npm:typescript", "7.0.0"))
	npmShellOut = oldOut

	_, err := os.Stat(legacyNM)
	assert.Error(t, err)
	_, err = os.Stat(p.packageDir("typescript"))
	assert.NoError(t, err)
	target, err := os.Readlink(filepath.Join(files.GetAppBinPath(), "tsc"))
	assert.NoError(t, err)
	assert.Contains(t, target, filepath.Join("packages", "npm", "typescript"))
}

func TestNPMRemoveDeletesContainerAndBinLink(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = os.MkdirAll(files.GetAppBinPath(), 0755)
	_ = lppAdd("npm:eslint", "1.0.0")
	plantNpmInstalled(t, p, "eslint", "1.0.0", "eslint")
	assert.True(t, p.Sync())
	_, err := os.Lstat(filepath.Join(files.GetAppBinPath(), "eslint"))
	assert.NoError(t, err)

	assert.True(t, p.Remove("npm:eslint"))
	_, err = os.Stat(p.packageDir("eslint"))
	assert.Error(t, err)
	_, err = os.Lstat(filepath.Join(files.GetAppBinPath(), "eslint"))
	assert.Error(t, err)
}

func TestNPMRemovePrunesEmptyScopeDir(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	name := "@scope/pkg"
	_ = lppAdd("npm:"+name, "1.0.0")
	plantNpmInstalled(t, p, name, "1.0.0", "pkg")
	assert.True(t, p.Remove("npm:"+name))
	_, err := os.Stat(filepath.Join(p.APP_PACKAGES_DIR, "@scope"))
	assert.Error(t, err)
}

func TestNPMRemoveAllSymlinksLeavesNonNpmBins(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	binDir := files.GetAppBinPath()
	_ = os.MkdirAll(binDir, 0755)
	other := filepath.Join(binDir, "gopls")
	assert.NoError(t, os.WriteFile(other, []byte("#!/bin/sh\n"), 0755))
	_ = lppAdd("npm:eslint", "1.0.0")
	plantNpmInstalled(t, p, "eslint", "1.0.0", "eslint")
	assert.NoError(t, p.createPackageSymlinks("eslint"))
	assert.NoError(t, p.removeAllSymlinks())
	_, err := os.Stat(other)
	assert.NoError(t, err)
	_, err = os.Lstat(filepath.Join(binDir, "eslint"))
	assert.Error(t, err)
}

func TestNPMCreatePackageSymlinksUsesRegistryBinAliases(t *testing.T) {
	_ = withTempNvpmHome(t)

	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = local_packages_parser.AddLocalPackage("npm:typescript", "5.0.0")

	writeRegistry(t, []registry_parser.RegistryItem{{
		Name:    "typescript",
		Version: "5.0.0",
		Source:  registry_parser.RegistryItemSource{ID: "npm:typescript"},
		Bin: map[string]string{
			"tsc":  "npm:tsc",
			"tsgo": "npm:tsc",
		},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)

	plantNpmInstalled(t, p, "typescript", "5.0.0", "tsc")
	containerBin := filepath.Join(p.nodeModulesDir("typescript"), ".bin")

	assert.NoError(t, p.createPackageSymlinks("typescript"))

	appBin := files.GetAppBinPath()
	tscLink := filepath.Join(appBin, "tsc")
	tsgoLink := filepath.Join(appBin, "tsgo")
	tscTarget, err := os.Readlink(tscLink)
	assert.NoError(t, err)
	tsgoTarget, err := os.Readlink(tsgoLink)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(containerBin, "tsc"), tscTarget)
	assert.Equal(t, filepath.Join(containerBin, "tsc"), tsgoTarget)

	assert.NoError(t, p.removePackageSymlinks("typescript"))
	_, err = os.Lstat(tscLink)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Lstat(tsgoLink)
	assert.True(t, os.IsNotExist(err))
}

func TestNPMCreatePackageSymlinksIgnoresPackageJSONBinsWhenRegistryPresent(t *testing.T) {
	_ = withTempNvpmHome(t)

	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("npm:typescript", "5.0.0")
	writeRegistry(t, []registry_parser.RegistryItem{{
		Name:    "typescript",
		Version: "5.0.0",
		Source:  registry_parser.RegistryItemSource{ID: "npm:typescript"},
		Bin:     map[string]string{"tsc": "npm:tsc"},
	}})
	_ = registry_parser.NewDefaultRegistryParser().GetData(true)

	plantNpmInstalled(t, p, "typescript", "5.0.0", "tsc")
	nm := p.nodeModulesDir("typescript")
	pkgJSON := `{"name":"typescript","version":"5.0.0","bin":{"tsc":"./bin/tsc","tsserver":"./bin/tsserver"}}`
	assert.NoError(t, os.WriteFile(filepath.Join(nm, "typescript", "package.json"), []byte(pkgJSON), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(nm, ".bin", "tsserver"), []byte("#!/bin/sh\n"), 0755))

	assert.NoError(t, p.createPackageSymlinks("typescript"))

	appBin := files.GetAppBinPath()
	_, err := os.Lstat(filepath.Join(appBin, "tsc"))
	assert.NoError(t, err)
	_, err = os.Lstat(filepath.Join(appBin, "tsserver"))
	assert.True(t, os.IsNotExist(err))
}

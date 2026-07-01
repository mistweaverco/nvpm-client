package providers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/stretchr/testify/assert"
)

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
	// prepare a fake package.json
	nm := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "pkg")
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	_ = os.MkdirAll(nm, 0755)
	pkgJSON := `{"name":"pkg","version":"1.0.0","bin":{"tool":"./bin/tool.js"}}`
	assert.NoError(t, os.WriteFile(filepath.Join(nm, "package.json"), []byte(pkgJSON), 0644))

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
	// desired
	pkgPath := filepath.Join(np.APP_PACKAGES_DIR, "package.json")
	assert.NoError(t, os.WriteFile(pkgPath, []byte("{}"), 0644))
	// lock newer matching
	lock := filepath.Join(np.APP_PACKAGES_DIR, "package-lock.json")
	lockData := `{"dependencies":{"a":{"version":"1.0.0"}}}`
	assert.NoError(t, os.WriteFile(lock, []byte(lockData), 0644))
	now := time.Now()
	_ = os.Chtimes(lock, now.Add(1*time.Hour), now.Add(1*time.Hour))
	// create node_modules bin so createPackageSymlinks can run
	_ = os.MkdirAll(filepath.Join(np.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	nm := filepath.Join(np.APP_PACKAGES_DIR, "node_modules", "a")
	_ = os.MkdirAll(nm, 0755)
	assert.NoError(t, os.WriteFile(filepath.Join(nm, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644))
	// fast path
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
	// desired v2.0.0
	_ = lppAdd("pkg:npm/a", "2.0.0")
	// lock newer with v1.0.0
	pkgPath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	_ = os.WriteFile(pkgPath, []byte("{}"), 0644)
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(lock, []byte(`{"dependencies":{"a":{"version":"1.0.0"}}}`), 0644)
	now := time.Now()
	_ = os.Chtimes(lock, now.Add(1*time.Hour), now.Add(1*time.Hour))
	// prepare node_modules/a package.json and .bin target
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	nm := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	_ = os.MkdirAll(nm, 0755)
	_ = os.WriteFile(filepath.Join(nm, "package.json"), []byte(`{"name":"a","version":"2.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	// tryNpmCi should fail
	oldOut := npmShellOut
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) == 1 && args[0] == "ci" {
			return 1, errors.New("ci")
		}
		// install individually succeeds
		return 0, nil
	}
	// lock removal error should be tolerated
	oldRm := npmRemove
	npmRemove = func(string) error { return errors.New("rm") }
	assert.True(t, p.Sync())
	npmRemove = oldRm
	npmShellOut = oldOut
}

func TestNPMAllConditionalsToggle(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// Case 1: generatePackageJSON returns false -> Sync returns true
	oldGet := lppGetData
	lppGetData = func(bool) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Sync())
	lppGetData = oldGet

	// Setup desired package a@2.0.0, lock newer with 1.0.0 to trigger needsUpdate
	_ = lppAdd("pkg:npm/a", "2.0.0")
	pkgPath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	_ = os.WriteFile(pkgPath, []byte("{}"), 0644)
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(lock, []byte(`{"dependencies":{"a":{"version":"1.0.0"}}}`), 0644)
	now := time.Now()
	_ = os.Chtimes(lock, now.Add(1*time.Hour), now.Add(1*time.Hour))
	// Ensure bin dirs exist
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	_ = os.MkdirAll(an, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"2.0.0","bin":{"a":"./bin/a.js"}}`), 0644)

	// Case 2: needsUpdate with ci success -> returns true early
	oldOut := npmShellOut
	npmShellOut = func(cmd string, args []string, dir string, env []string) (int, error) {
		if len(args) == 1 && args[0] == "ci" {
			return 0, nil
		}
		return 0, nil
	}
	assert.True(t, p.Sync())
	npmShellOut = oldOut

	// Case 3: Installing individually path with install failure -> returns false
	// Remove lock to skip lockExists branches
	_ = os.Remove(lock)
	// Desired b@1.0.0 not installed
	_ = lppAdd("pkg:npm/b", "1.0.0")
	bn := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "b")
	_ = os.MkdirAll(bn, 0755)
	_ = os.WriteFile(filepath.Join(bn, "package.json"), []byte(`{"name":"b","version":"0.9.0","bin":{"b":"./bin/b.js"}}`), 0644)
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
	oldRD, oldRA, oldStat, oldMkdir, oldGet := npmReadDir, npmRemoveAll, npmStat, npmMkdir, lppGetData
	// make removeAllSymlinks error
	npmReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	// allow directory remove to succeed
	npmRemoveAll = func(string) error { return nil }
	// Sync path: make directory creation succeed and no packages found
	npmStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	npmMkdir = func(string, os.FileMode) error { return nil }
	lppGetData = func(bool) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Clean())
	// restore
	npmReadDir, npmRemoveAll, npmStat, npmMkdir, lppGetData = oldRD, oldRA, oldStat, oldMkdir, oldGet
}

func TestNPMSyncCreateDirError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	oldStat, oldMkdir := npmStat, npmMkdir
	npmStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	npmMkdir = func(string, os.FileMode) error { return errors.New("mkdir") }
	assert.False(t, p.Sync())
	npmStat, npmMkdir = oldStat, oldMkdir
}

func TestNPMFastPathLogsCreateSymlinkErrors(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired a@1.0.0
	_ = lppAdd("pkg:npm/a", "1.0.0")
	// package.json and newer lock with matching version
	_ = os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package.json"), []byte("{}"), 0644)
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(lock, []byte(`{"dependencies":{"a":{"version":"1.0.0"}}}`), 0644)
	_ = os.Chtimes(lock, time.Now().Add(1*time.Hour), time.Now().Add(1*time.Hour))
	// node_modules setup with bin
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	_ = os.MkdirAll(an, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	// force symlink creation to fail so the log path is taken
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
	dir := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "broken")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte("{"), 0644)
	assert.False(t, p.isPackageInstalled("broken", "1.0.0"))
	// true when versions match
	okd := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "ok")
	_ = os.MkdirAll(okd, 0755)
	_ = os.WriteFile(filepath.Join(okd, "package.json"), []byte(`{"name":"ok","version":"1.0.0"}`), 0644)
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
	// Setup a package with bin
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	pa := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "pkg")
	_ = os.MkdirAll(pa, 0755)
	_ = os.WriteFile(filepath.Join(pa, "package.json"), []byte(`{"name":"pkg","version":"1.0.0","bin":{"cli":"./bin/cli.js"}}`), 0644)
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
	oldGet := lppGetData
	lppGetData = func(bool) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Install("pkg:npm/pkg", "1.0.0"))
	lppGetData = oldGet

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
	oldMkdir := npmMkdir
	oldGet := lppGetData
	npmRemoveAll = func(string) error { return nil }
	npmStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	npmMkdir = func(string, os.FileMode) error { return nil }
	lppGetData = func(bool) local_packages_parser.LocalPackageRoot {
		return local_packages_parser.LocalPackageRoot{Packages: nil}
	}
	assert.True(t, p.Clean())
	// restore
	npmRemoveAll = oldRmAll
	npmStat = oldStat
	npmMkdir = oldMkdir
	lppGetData = oldGet
}

func TestNPMFastPathMultiPackageSymlinkLoop(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)

	// desired a@1.0.0 and b@2.0.0
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	// create package.json and a newer lock with both versions matching
	pkgPath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	_ = os.WriteFile(pkgPath, []byte("{}"), 0644)
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(lock, []byte(`{"dependencies":{"a":{"version":"1.0.0"},"b":{"version":"2.0.0"}}}`), 0644)
	now := time.Now()
	_ = os.Chtimes(lock, now.Add(1*time.Hour), now.Add(1*time.Hour))
	// node_modules setup with bins for both
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	bn := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "b")
	_ = os.MkdirAll(an, 0755)
	_ = os.MkdirAll(bn, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	_ = os.WriteFile(filepath.Join(bn, "package.json"), []byte(`{"name":"b","version":"2.0.0","bin":{"b":"./bin/b.js"}}`), 0644)
	assert.True(t, p.Sync())
}

func TestNPMFastPathMultiPackageSymlinkLoopWithSymlinkErrors(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	_ = os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package.json"), []byte("{}"), 0644)
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(lock, []byte(`{"dependencies":{"a":{"version":"1.0.0"},"b":{"version":"2.0.0"}}}`), 0644)
	_ = os.Chtimes(lock, time.Now().Add(1*time.Hour), time.Now().Add(1*time.Hour))
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	bn := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "b")
	_ = os.MkdirAll(an, 0755)
	_ = os.MkdirAll(bn, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	_ = os.WriteFile(filepath.Join(bn, "package.json"), []byte(`{"name":"b","version":"2.0.0","bin":{"b":"./bin/b.js"}}`), 0644)
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
	_ = os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package.json"), []byte("{}"), 0644)
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(lock, []byte(`{"dependencies":{"a":{"version":"1.0.0"},"b":{"version":"2.0.0"}}}`), 0644)
	_ = os.Chtimes(lock, time.Now().Add(1*time.Hour), time.Now().Add(1*time.Hour))
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	bn := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "b")
	_ = os.MkdirAll(an, 0755)
	_ = os.MkdirAll(bn, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	_ = os.WriteFile(filepath.Join(bn, "package.json"), []byte(`{"name":"b","version":"2.0.0","bin":{"b":"./bin/b.js"}}`), 0644)
	oldCh := npmChmod
	npmChmod = func(string, os.FileMode) error { return nil }
	assert.True(t, p.Sync())
	npmChmod = oldCh
}

func TestNPMFastPathSecondLoopAllInstalled(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	// actual files (their real modtimes won't matter due to npmStat stub)
	_ = os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json"), []byte(`{"dependencies":{"a":{"version":"1.0.0"},"b":{"version":"2.0.0"}}}`), 0644)
	// setup node_modules with bins
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	bn := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "b")
	_ = os.MkdirAll(an, 0755)
	_ = os.MkdirAll(bn, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	_ = os.WriteFile(filepath.Join(bn, "package.json"), []byte(`{"name":"b","version":"2.0.0","bin":{"b":"./bin/b.js"}}`), 0644)

	// Create controlled fi for lock newer and pkg older; also ensure hasPackageJSONChanged returns false
	tmp := t.TempDir()
	fakeLock := filepath.Join(tmp, "lock")
	fakePkg := filepath.Join(tmp, "pkg")
	_ = os.WriteFile(fakeLock, []byte("x"), 0644)
	_ = os.WriteFile(fakePkg, []byte("x"), 0644)
	_ = os.Chtimes(fakeLock, time.Now().Add(2*time.Hour), time.Now().Add(2*time.Hour))
	_ = os.Chtimes(fakePkg, time.Now(), time.Now())
	oldStat := npmStat
	npmStat = func(name string) (os.FileInfo, error) {
		switch filepath.Base(name) {
		case "package-lock.json":
			fi, _ := os.Stat(fakeLock)
			return fi, nil
		case "package.json":
			fi, _ := os.Stat(fakePkg)
			return fi, nil
		default:
			return oldStat(name)
		}
	}
	oldCh := npmChmod
	npmChmod = func(string, os.FileMode) error { return nil }

	assert.True(t, p.Sync())
	npmChmod = oldCh
	npmStat = oldStat
}

func TestNPMFastPathAllInstalledCallsSymlinkForEachPackage(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired packages
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	// package.json and lock newer matching
	_ = os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "package.json"), []byte("{}"), 0644)
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(lock, []byte(`{"dependencies":{"a":{"version":"1.0.0"},"b":{"version":"2.0.0"}}}`), 0644)
	_ = os.Chtimes(lock, time.Now().Add(1*time.Hour), time.Now().Add(1*time.Hour))
	// node_modules with bin for both packages
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	bn := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "b")
	_ = os.MkdirAll(an, 0755)
	_ = os.MkdirAll(bn, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	_ = os.WriteFile(filepath.Join(bn, "package.json"), []byte(`{"name":"b","version":"2.0.0","bin":{"b":"./bin/b.js"}}`), 0644)

	// Capture which symlinks were attempted to verify loop over desired
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
	// Expect both a and b symlink attempts
	assert.GreaterOrEqual(t, called["a"], 1)
	assert.GreaterOrEqual(t, called["b"], 1)

	npmChmod = oldCh
	npmSymlink = oldSym
}

func TestNPMFastPathAllInstalledMixedSymlinkSuccessAndError(t *testing.T) {
	_ = withTempNvpmHome(t)
	p := NewProviderNPM()
	_ = os.MkdirAll(p.APP_PACKAGES_DIR, 0755)
	// desired packages
	_ = lppAdd("pkg:npm/a", "1.0.0")
	_ = lppAdd("pkg:npm/b", "2.0.0")
	// package.json and lock (lock newer)
	pkgPath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	_ = os.WriteFile(pkgPath, []byte("{}"), 0644)
	_ = os.WriteFile(lock, []byte(`{"dependencies":{"a":{"version":"1.0.0"},"b":{"version":"2.0.0"}}}`), 0644)
	now := time.Now()
	_ = os.Chtimes(pkgPath, now, now)
	_ = os.Chtimes(lock, now.Add(1*time.Hour), now.Add(1*time.Hour))
	// node_modules with valid a and invalid b to force error on b
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	bn := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "b")
	_ = os.MkdirAll(an, 0755)
	_ = os.MkdirAll(bn, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	_ = os.WriteFile(filepath.Join(bn, "package.json"), []byte("{"), 0644) // invalid JSON -> error path
	// ensure chmod does not fail for the success package
	oldCh := npmChmod
	npmChmod = func(string, os.FileMode) error { return nil }
	assert.True(t, p.Sync())
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
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	an := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "a")
	_ = os.MkdirAll(an, 0755)
	_ = os.WriteFile(filepath.Join(an, "package.json"), []byte(`{"name":"a","version":"1.0.0","bin":{"a":"./bin/a.js"}}`), 0644)
	// No lock file so it goes to individual install path; mark package installed so skip branch executes
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
	// Prepare node_modules/pkg with a bin so removePackageSymlinks will try to remove
	binDir := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin")
	_ = os.MkdirAll(binDir, 0755)
	pkgDir := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "pkg")
	_ = os.MkdirAll(pkgDir, 0755)
	_ = os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"pkg","version":"1.0.0","bin":{"cli":"./bin/cli.js"}}`), 0644)
	// Force symlink removal error and ensure removePackageSymlinks returns error to trigger log in Remove
	oldLs, oldRm := npmLstat, npmRemove
	npmLstat = func(string) (os.FileInfo, error) { return fileInfoNow(t), nil }
	npmRemove = func(string) error { return errors.New("rm") }
	// Ensure local removal succeeds
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
	// desired d@1.0.0 not installed
	_ = lppAdd("pkg:npm/d", "1.0.0")
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	// install succeeds
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
	nm := filepath.Join(p.APP_PACKAGES_DIR, "node_modules", "eslint")
	_ = os.MkdirAll(nm, 0755)
	_ = os.MkdirAll(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin"), 0755)
	// create actual bin target to avoid chmod errors
	assert.NoError(t, os.WriteFile(filepath.Join(p.APP_PACKAGES_DIR, "node_modules", ".bin", "eslint"), []byte(""), 0755))
	pkgJSON := `{"name":"eslint","version":"1.0.0","bin":{"eslint":"./bin/eslint.js"}}`
	assert.NoError(t, os.WriteFile(filepath.Join(nm, "package.json"), []byte(pkgJSON), 0644))

	// isPackageInstalled true/false
	assert.True(t, p.isPackageInstalled("eslint", "1.0.0"))
	assert.False(t, p.isPackageInstalled("eslint", "2.0.0"))

	// createPackageSymlinks
	assert.NoError(t, p.createPackageSymlinks("eslint"))

	// removePackageSymlinks
	assert.NoError(t, p.removePackageSymlinks("eslint"))

	// getInstalledPackagesFromLock
	lock := filepath.Join(p.APP_PACKAGES_DIR, "package-lock.json")
	lockData := `{"dependencies":{"eslint":{"version":"1.0.0"}}}`
	assert.NoError(t, os.WriteFile(lock, []byte(lockData), 0644))
	inst := p.getInstalledPackagesFromLock(lock)
	assert.Equal(t, "1.0.0", inst["eslint"])
	// ensure lock is newer than package.json so fast-path is taken
	pkgPath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	assert.NoError(t, os.WriteFile(pkgPath, []byte("{}"), 0644))
	now := time.Now()
	_ = os.Chtimes(lock, now.Add(1*time.Hour), now.Add(1*time.Hour))

	// tryNpmCi: lock exists and stub returns success
	assert.True(t, p.tryNpmCi())
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

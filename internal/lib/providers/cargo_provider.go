package providers

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

type CargoProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

// Injectable shell and OS helpers for tests
var cargoShellOut = shell_out.ShellOut
var cargoShellOutCapture = shell_out.ShellOutCapture
var cargoReadDir = os.ReadDir
var cargoLstat = os.Lstat
var cargoRemove = os.Remove
var cargoChmod = os.Chmod
var cargoStat = os.Stat
var cargoMkdir = os.Mkdir
var cargoReadlink = os.Readlink
var cargoRemoveAll = os.RemoveAll
var cargoSymlink = os.Symlink

// Injectable local packages helpers for tests
var lppCargoAdd = local_packages_parser.AddLocalPackage
var lppCargoRemove = local_packages_parser.RemoveLocalPackage
var lppCargoGetDataForProvider = local_packages_parser.GetDataForProvider
var cargoHasCommand = shell_out.HasCommand

func NewProviderCargo() *CargoProvider {
	p := &CargoProvider{}
	p.PROVIDER_NAME = "cargo"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	return p
}

func (p *CargoProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:cargo/pkg) and new (cargo:pkg) formats
	normalized := normalizePackageID(sourceID)
	if strings.HasPrefix(normalized, p.PREFIX) {
		return strings.TrimPrefix(normalized, p.PREFIX)
	}
	// Fallback for legacy format
	re := regexp.MustCompile("^pkg:" + p.PROVIDER_NAME + "/(.*)")
	matches := re.FindStringSubmatch(sourceID)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (p *CargoProvider) createSymlinks() error {
	cargoBinDir := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	nvpmBinDir := files.GetAppBinPath()
	if _, err := cargoStat(cargoBinDir); os.IsNotExist(err) {
		return nil
	}
	entries, err := cargoReadDir(cargoBinDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		binaryName := entry.Name()
		binaryPath := filepath.Join(cargoBinDir, binaryName)
		symlinkPath := filepath.Join(nvpmBinDir, binaryName)
		if _, err := cargoLstat(symlinkPath); err == nil {
			if err := cargoRemove(symlinkPath); err != nil {
				log.Printf("Warning: failed to remove existing symlink %s: %v", symlinkPath, err)
			}
		}
		if err := cargoSymlink(binaryPath, symlinkPath); err != nil {
			log.Printf("Error creating symlink for %s: %v", binaryName, err)
			continue
		}
		if err := cargoChmod(symlinkPath, 0755); err != nil {
			log.Printf("Error setting executable permissions for %s: %v", binaryName, err)
		}
	}
	return nil
}

func (p *CargoProvider) removeAllSymlinks() error {
	nvpmBinDir := files.GetAppBinPath()
	cargoBinDir := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	entries, err := cargoReadDir(nvpmBinDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		symlinkPath := filepath.Join(nvpmBinDir, entry.Name())
		fi, err := cargoLstat(symlinkPath)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := cargoReadlink(symlinkPath)
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, cargoBinDir) {
			if err := cargoRemove(symlinkPath); err != nil {
				log.Printf("Warning: failed to remove symlink %s: %v", symlinkPath, err)
			}
		}
	}
	return nil
}

func (p *CargoProvider) Clean() bool {
	if err := p.removeAllSymlinks(); err != nil {
		log.Printf("Error removing symlinks: %v", err)
	}
	if err := cargoRemoveAll(p.APP_PACKAGES_DIR); err != nil {
		log.Println("Error removing directory:", err)
		return false
	}
	return p.Sync()
}

func (p *CargoProvider) checkCargoAvailable() bool {
	return cargoHasCommand("cargo", []string{"--version"}, nil)
}

func (p *CargoProvider) getInstalledCrates() map[string]string {
	installed := map[string]string{}
	_, output, err := cargoShellOutCapture("cargo", []string{"install", "--list"}, p.APP_PACKAGES_DIR, []string{"CARGO_HOME=" + p.APP_PACKAGES_DIR})
	if err != nil {
		return installed
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "(") || strings.HasPrefix(line, "\t") {
			continue
		}
		re := regexp.MustCompile(`^([A-Za-z0-9_-]+) v([^:]+):`)
		m := re.FindStringSubmatch(line)
		if len(m) == 3 {
			installed[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return installed
}

func (p *CargoProvider) Sync() bool {
	if _, err := cargoStat(p.APP_PACKAGES_DIR); os.IsNotExist(err) {
		if err := cargoMkdir(p.APP_PACKAGES_DIR, 0755); err != nil {
			fmt.Println("Error creating directory:", err)
			return false
		}
	}
	if !p.checkCargoAvailable() {
		log.Println("Error: Cargo is not available. Please install Rust and ensure cargo is in your PATH.")
		return false
	}
	desired := lppCargoGetDataForProvider("cargo").Packages
	installed := p.getInstalledCrates()
	allOk := true
	installedCount := 0
	skippedCount := 0
	for _, pkg := range desired {
		crate := p.getRepo(pkg.SourceID)
		if crate == "" {
			continue
		}
		// Resolve desired version: if "latest" (or empty), query the actual latest version
		desiredVersion := pkg.Version
		if desiredVersion == "" || desiredVersion == "latest" {
			latestVersion, err := p.getLatestVersion(crate)
			if err != nil {
				log.Printf("Error resolving latest version for %s: %v", crate, err)
				allOk = false
				continue
			}
			desiredVersion = latestVersion
		}

		if v, ok := installed[crate]; ok && v == desiredVersion {
			log.Printf("Cargo Sync: Package %s@%s already installed, skipping", crate, desiredVersion)
			// If lockfile still has "latest", update it to the resolved version
			if pkg.Version != desiredVersion {
				if err := lppCargoAdd(pkg.SourceID, desiredVersion); err != nil {
					log.Printf("Warning: failed to update nvpm-lock.json for %s: %v", crate, err)
				}
			}
			skippedCount++
			continue
		}

		log.Printf("Cargo Sync: Installing package %s@%s", crate, desiredVersion)
		args := []string{"install", crate, "--force"}
		if desiredVersion != "" {
			args = append(args, "--version", desiredVersion)
		}
		args = append(args, "--locked")
		code, err := cargoShellOut("cargo", args, p.APP_PACKAGES_DIR, []string{"CARGO_HOME=" + p.APP_PACKAGES_DIR})
		if err != nil || code != 0 {
			log.Printf("Error installing %s@%s: %v", crate, desiredVersion, err)
			allOk = false
			continue
		}
		// Persist resolved version to lockfile (covers cases where requested was "latest")
		if pkg.Version != desiredVersion {
			if err := lppCargoAdd(pkg.SourceID, desiredVersion); err != nil {
				log.Printf("Warning: failed to update nvpm-lock.json for %s: %v", crate, err)
			}
		}
		installedCount++
	}
	if err := p.createSymlinks(); err != nil {
		log.Printf("Error creating symlinks for Cargo binaries: %v", err)
	}
	log.Printf("Cargo Sync: Completed - %d packages installed, %d packages skipped", installedCount, skippedCount)
	return allOk
}

func (p *CargoProvider) Install(sourceID, version string) bool {
	crate := p.getRepo(sourceID)
	if crate == "" {
		return false
	}
	// Resolve version if "latest" or empty
	resolvedVersion := version
	if resolvedVersion == "" || resolvedVersion == "latest" {
		latestVersion, err := p.getLatestVersion(crate)
		if err != nil {
			log.Printf("Error resolving latest version for %s: %v", crate, err)
			return false
		}
		resolvedVersion = latestVersion
	}
	if err := lppCargoAdd(sourceID, resolvedVersion); err != nil {
		return false
	}
	return p.Sync()
}

func (p *CargoProvider) Remove(sourceID string) bool {
	crate := p.getRepo(sourceID)
	if crate == "" {
		return false
	}
	log.Printf("Cargo Remove: Removing package %s", crate)
	code, err := cargoShellOut("cargo", []string{"uninstall", crate}, p.APP_PACKAGES_DIR, []string{"CARGO_HOME=" + p.APP_PACKAGES_DIR})
	if err != nil || code != 0 {
		log.Printf("Error uninstalling %s: %v", crate, err)
	}
	if err := lppCargoRemove(sourceID); err != nil {
		log.Printf("Error removing package %s from local packages: %v", crate, err)
		return false
	}
	if err := p.removeAllSymlinks(); err != nil {
		log.Printf("Error removing symlinks: %v", err)
	}
	if err := p.createSymlinks(); err != nil {
		log.Printf("Error creating symlinks: %v", err)
	}
	log.Printf("Cargo Remove: Package %s removed successfully", crate)
	return p.Sync()
}

func (p *CargoProvider) Update(sourceID string) bool {
	crate := p.getRepo(sourceID)
	if crate == "" {
		log.Printf("Invalid source ID format for Cargo provider")
		return false
	}
	latestVersion, err := p.getLatestVersion(crate)
	if err != nil {
		log.Printf("Error getting latest version for %s: %v", crate, err)
		return false
	}
	log.Printf("Cargo Update: Updating %s to version %s", crate, latestVersion)
	return p.Install(sourceID, latestVersion)
}

func (p *CargoProvider) getLatestVersion(crate string) (string, error) {
	_, output, err := cargoShellOutCapture("cargo", []string{"search", crate, "-q"}, "", nil)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, crate+" ") {
			continue
		}
		re := regexp.MustCompile(`^` + regexp.QuoteMeta(crate) + `\s*=\s*"([^"]+)"`)
		m := re.FindStringSubmatch(line)
		if len(m) == 2 {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("latest version not found for %s", crate)
}

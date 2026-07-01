package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/shell_out"
)

type GolangProvider struct {
	APP_PACKAGES_DIR string
	PREFIX           string
	PROVIDER_NAME    string
}

// Injectable shell and OS helpers for tests
var goShellOut = shell_out.ShellOut
var goShellOutCapture = shell_out.ShellOutCapture
var goCreate = os.Create
var goStat = os.Stat
var goMkdir = os.Mkdir
var goLstat = os.Lstat
var goRemove = os.Remove
var goSymlink = os.Symlink
var goClose = func(f *os.File) error { return f.Close() }

// Injectable local packages helpers for tests
var lppGoAdd = local_packages_parser.AddLocalPackage
var lppGoRemove = local_packages_parser.RemoveLocalPackage
var lppGoGetDataForProvider = local_packages_parser.GetDataForProvider

func NewProviderGolang() *GolangProvider {
	p := &GolangProvider{}
	p.PROVIDER_NAME = "golang"
	p.APP_PACKAGES_DIR = filepath.Join(files.GetAppPackagesPath(), p.PROVIDER_NAME)
	p.PREFIX = p.PROVIDER_NAME + ":"
	return p
}

func (p *GolangProvider) getRepo(sourceID string) string {
	// Support both legacy (pkg:golang/pkg) and new (golang:pkg) formats
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

func (p *GolangProvider) generatePackageJSON() bool {
	found := false
	packageJSON := struct {
		Dependencies map[string]string `json:"dependencies"`
	}{
		Dependencies: make(map[string]string),
	}
	localPackages := local_packages_parser.GetDataForProvider("golang").Packages
	for _, pkg := range localPackages {
		packageJSON.Dependencies[p.getRepo(pkg.SourceID)] = pkg.Version
		found = true
	}
	filePath := filepath.Join(p.APP_PACKAGES_DIR, "package.json")
	file, err := goCreate(filePath)
	if err != nil {
		fmt.Println("Error creating package.json:", err)
		return false
	}
	defer func() {
		if closeErr := goClose(file); closeErr != nil {
			fmt.Printf("Warning: failed to close package.json file: %v\n", closeErr)
		}
	}()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(packageJSON); err != nil {
		fmt.Println("error encoding package.json:", err)
		return false
	}
	return found
}

func (p *GolangProvider) createSymlink(sourceID string) error {
	parser := registry_parser.NewDefaultRegistryParser()
	registryItem := parser.GetBySourceId(sourceID)
	golangBinDir := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	nvpmBinDir := files.GetAppBinPath()

	if len(registryItem.Bin) == 0 {
		return fmt.Errorf("error: no binary name found for package %s", sourceID)
	}

	for binName := range registryItem.Bin {
		symlink := filepath.Join(nvpmBinDir, binName)
		// Remove any existing symlink with the same name to avoid conflicts
		if _, err := goLstat(symlink); err == nil {
			_ = goRemove(symlink)
		}
		binaryPath := filepath.Join(golangBinDir, binName)
		if _, err := goStat(binaryPath); os.IsNotExist(err) {
			return fmt.Errorf("error: binary %s does not exist in %s", binName, golangBinDir)
		}
		if err := goSymlink(binaryPath, symlink); err != nil {
			return fmt.Errorf("error creating symlink %s -> %s: %v", symlink, binaryPath, err)
		}
	}

	return nil
}

func (p *GolangProvider) removeBin(sourceID string) error {
	parser := registry_parser.NewDefaultRegistryParser()
	registryItem := parser.GetBySourceId(sourceID)
	golangBinDir := filepath.Join(p.APP_PACKAGES_DIR, "bin")

	if len(registryItem.Bin) == 0 {
		return fmt.Errorf("error: no binary name found for package %s", sourceID)
	}

	for binName := range registryItem.Bin {
		binPath := filepath.Join(golangBinDir, binName)
		if fi, err := goStat(binPath); err == nil && !fi.IsDir() {
			if err := goRemove(binPath); err != nil {
				return fmt.Errorf("error removing binary %s: %v", binPath, err)
			}
		}
	}
	return nil
}

func (p *GolangProvider) removeSymlink(sourceID string) error {
	parser := registry_parser.NewDefaultRegistryParser()
	registryItem := parser.GetBySourceId(sourceID)
	nvpmBinDir := files.GetAppBinPath()

	if len(registryItem.Bin) == 0 {
		return fmt.Errorf("error: no binary name found for package %s", sourceID)
	}

	for binName := range registryItem.Bin {
		symlink := filepath.Join(nvpmBinDir, binName)
		if _, err := goLstat(symlink); err == nil {
			if err := goRemove(symlink); err != nil {
				return fmt.Errorf("error removing symlink %s: %v", symlink, err)
			}
		}
	}
	return nil
}

func (p *GolangProvider) Clean() bool {
	data := lppGoGetDataForProvider("golang")
	if len(data.Packages) == 0 {
		Logger.Debug("Golang Clean: No packages to clean")
		return true
	}
	Logger.Debug("Golang Clean: Cleaning up packages")
	for _, pkg := range data.Packages {
		name := p.getRepo(pkg.SourceID)
		Logger.Debug(fmt.Sprintf("Golang Clean: Removing package %s", name))
		if err := p.removeSymlink(pkg.SourceID); err != nil {
			Logger.Error(fmt.Sprintf("Error removing symlink for package %s: %v", name, err))
		}
		parser := registry_parser.NewDefaultRegistryParser()
		for bin := range parser.GetBySourceId(pkg.SourceID).Bin {
			binPath := filepath.Join(p.APP_PACKAGES_DIR, "bin", bin)
			if fi, err := goStat(binPath); err == nil && !fi.IsDir() {
				if err := goRemove(binPath); err != nil {
					Logger.Error(fmt.Sprintf("Error removing binary %s: %v", binPath, err))
				}
			}
		}
		if err := lppGoRemove(pkg.SourceID); err != nil {
			Logger.Error(fmt.Sprintf("Error removing package %s from local packages: %v", name, err))
			return false
		}
		Logger.Debug(fmt.Sprintf("Golang Clean: Package %s removed from local packages", name))
	}
	return true
}

func (p *GolangProvider) checkGoAvailable() bool {
	checkCode, err := goShellOut("go", []string{"version"}, p.APP_PACKAGES_DIR, nil)
	return err == nil && checkCode == 0
}

func (p *GolangProvider) Sync() bool {
	if _, err := goStat(p.APP_PACKAGES_DIR); os.IsNotExist(err) {
		if err := goMkdir(p.APP_PACKAGES_DIR, 0755); err != nil {
			fmt.Println("Error creating directory:", err)
			return false
		}
	}
	if !p.checkGoAvailable() {
		Logger.Error("Golang Sync: Go is not available. Please install Go and ensure it's in your PATH.")
		return false
	}
	packagesFound := p.generatePackageJSON()
	if !packagesFound {
		return true
	}
	Logger.Info("Golang Sync: Generating package.json")
	desired := local_packages_parser.GetDataForProvider("golang").Packages
	goModPath := filepath.Join(p.APP_PACKAGES_DIR, "go.mod")
	if _, err := goStat(goModPath); os.IsNotExist(err) {
		initCode, err := goShellOut("go", []string{"mod", "init", "nvpm-golang-packages"}, p.APP_PACKAGES_DIR, nil)
		if err != nil || initCode != 0 {
			Logger.Error(fmt.Sprintf("Error initializing Go module: %v", err))
			return false
		}
	}
	gobin := filepath.Join(p.APP_PACKAGES_DIR, "bin")
	allOk := true
	installedCount := 0
	skippedCount := 0
	for _, pkg := range desired {
		name := p.getRepo(pkg.SourceID)
		binPath := filepath.Join(gobin, filepath.Base(name))
		installed := false
		if fi, err := goStat(binPath); err == nil && !fi.IsDir() {
			installed = true
		}
		if !installed {
			Logger.Info(fmt.Sprintf("Golang Sync: Package %s@%s not installed, installing...", name, pkg.Version))
			installCode, err := goShellOut("go", []string{"install", name + "@" + pkg.Version}, p.APP_PACKAGES_DIR, []string{"GOBIN=" + gobin})
			if err != nil || installCode != 0 {
				Logger.Error(fmt.Sprintf("Error installing %s@%s: %v", name, pkg.Version, err))
				allOk = false
			} else {
				installedCount++
				if err := p.createSymlink(pkg.SourceID); err != nil {
					Logger.Error(fmt.Sprintf("Error creating symlinks for %s: %v", name, err))
				}
			}
		} else {
			Logger.Info(fmt.Sprintf("Golang Sync: Package %s@%s already installed, skipping", name, pkg.Version))
			skippedCount++
		}
	}
	Logger.Debug(fmt.Sprintf("Golang Sync: %d packages installed, %d packages skipped", installedCount, skippedCount))
	return allOk
}

func (p *GolangProvider) Install(sourceID, version string) bool {
	var err error
	if version == "latest" {
		version, err = p.getLatestVersion(p.getRepo(sourceID))
		if err != nil {
			Logger.Error("Error getting latest version for package %s: %v", sourceID, err)
			return false
		}
	}
	if err := lppGoAdd(sourceID, version); err != nil {
		return false
	}
	return p.Sync()
}

func (p *GolangProvider) Remove(sourceID string) bool {
	packageName := p.getRepo(sourceID)
	Logger.Debug(fmt.Sprintf("Golang Remove: Removing package %s", packageName))
	if err := p.removeSymlink(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Error removing symlinks for package %s: %v", packageName, err))
	}
	if err := p.removeBin(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Error removing binaries for package %s: %v", packageName, err))
	}
	if err := lppGoRemove(sourceID); err != nil {
		Logger.Error(fmt.Sprintf("Error removing package %s from local packages: %v", packageName, err))
		return false
	}
	Logger.Debug(fmt.Sprintf("Golang Remove: Package %s removed from local packages", packageName))
	return p.Sync()
}

func (p *GolangProvider) Update(sourceID string) bool {
	repo := p.getRepo(sourceID)
	if repo == "" {
		Logger.Error("Golang Update: Invalid source ID format")
		return false
	}
	latestVersion, err := p.getLatestVersion(repo)
	if err != nil {
		Logger.Error(fmt.Sprintf("Error getting latest version for package %s: %v", repo, err))
		return false
	}
	Logger.Debug(fmt.Sprintf("Golang Update: Latest version for %s is %s", repo, latestVersion))
	return p.Install(sourceID, latestVersion)
}

func (p *GolangProvider) getLatestVersion(packageName string) (string, error) {
	_, output, err := goShellOutCapture("go", []string{"list", "-m", "-versions", packageName}, "", nil)
	if err != nil {
		return "", err
	}
	parts := strings.Fields(output)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid output format from go list")
	}
	return parts[len(parts)-1], nil
}

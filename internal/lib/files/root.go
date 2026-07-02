package files

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/spinnerutil"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

// FileSystem interface for filesystem operations
type FileSystem interface {
	Create(name string) (afero.File, error)
	MkdirAll(path string, perm os.FileMode) error
	OpenFile(name string, flag int, perm os.FileMode) (afero.File, error)
	Stat(name string) (os.FileInfo, error)
	UserConfigDir() (string, error)
	UserHomeDir() (string, error)
	TempDir() string
	Getenv(key string) string
	WriteString(file afero.File, s string) (int, error)
	Close(file afero.File) error
}

// HTTPClient interface for HTTP operations
type HTTPClient interface {
	Get(url string) (*http.Response, error)
}

// ZipArchive is an interface that abstracts the functionality
// of a *zip.ReadCloser.
type ZipArchive interface {
	File() []*zip.File
	Close() error
}

// ZipFileOpener is the interface for opening a zip file.
type ZipFileOpener interface {
	Open(name string) (ZipArchive, error)
}

// defaultFileSystem implements FileSystem using Afero
type defaultFileSystem struct {
	fs afero.Fs
}

func (d *defaultFileSystem) Create(name string) (afero.File, error) {
	return d.fs.Create(name)
}

func (d *defaultFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return d.fs.MkdirAll(path, perm)
}

func (d *defaultFileSystem) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	return d.fs.OpenFile(name, flag, perm)
}

func (d *defaultFileSystem) Stat(name string) (os.FileInfo, error) {
	return d.fs.Stat(name)
}

func (d *defaultFileSystem) UserConfigDir() (string, error) {
	return os.UserConfigDir()
}

func (d *defaultFileSystem) UserHomeDir() (string, error) {
	return os.UserHomeDir()
}

func (d *defaultFileSystem) TempDir() string {
	return os.TempDir()
}

func (d *defaultFileSystem) Getenv(key string) string {
	return os.Getenv(key)
}

func (d *defaultFileSystem) WriteString(file afero.File, s string) (int, error) {
	return file.WriteString(s)
}

func (d *defaultFileSystem) Close(file afero.File) error {
	return file.Close()
}

// defaultHTTPClient implements HTTPClient
type defaultHTTPClient struct{}

func (d *defaultHTTPClient) Get(url string) (*http.Response, error) {
	return http.Get(url)
}

// RealZipArchive is a wrapper for a real *zip.ReadCloser
type RealZipArchive struct {
	*zip.ReadCloser
}

// File provides access to the embedded zip.ReadCloser's File field
func (r *RealZipArchive) File() []*zip.File {
	return r.ReadCloser.File
}

// Close closes the underlying zip.ReadCloser
func (r *RealZipArchive) Close() error {
	return r.ReadCloser.Close()
}

// RealZipFileOpener implements the ZipFileOpener interface using the real zip package.
type RealZipFileOpener struct{}

func (o *RealZipFileOpener) Open(name string) (ZipArchive, error) {
	rc, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}
	return &RealZipArchive{rc}, nil
}

// Global variables for dependency injection
var (
	fileSystem    FileSystem    = &defaultFileSystem{fs: afero.NewOsFs()}
	httpClient    HTTPClient    = &defaultHTTPClient{}
	zipFileOpener ZipFileOpener = &RealZipFileOpener{}
)

// SetFileSystem sets the file system implementation
func SetFileSystem(fs FileSystem) {
	fileSystem = fs
}

// SetHTTPClient sets the HTTP client implementation
func SetHTTPClient(client HTTPClient) {
	httpClient = client
}

// SetZipFileOpener sets the zip file opener implementation
func SetZipFileOpener(zfo ZipFileOpener) {
	zipFileOpener = zfo
}

// ResetDependencies resets all dependencies to their default implementations
func ResetDependencies() {
	fileSystem = &defaultFileSystem{fs: afero.NewOsFs()}
	httpClient = &defaultHTTPClient{}
	zipFileOpener = &RealZipFileOpener{}
}

func Download(url string, dest string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			// Log the error but don't fail the function
			fmt.Printf("Warning: failed to close response body: %v\n", closeErr)
		}
	}()

	out, err := fileSystem.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := fileSystem.Close(out); closeErr != nil {
			// Log the error but don't fail the function
			fmt.Printf("Warning: failed to close output file: %v\n", closeErr)
		}
	}()

	_, err = io.Copy(out, resp.Body)
	return err
}

// GetAppLocalPackagesFilePath returns the path to the local packages file
// e.g. /home/user/.config/nvpm/nvpm-lock.json
func GetAppLocalPackagesFilePath() string {
	return GetAppDataPath() + string(os.PathSeparator) + "nvpm-lock.json"
}

func FileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err :=
		fileSystem.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return err == nil
}

// GetAppDataPath returns the path to the app data directory
// If the NVPM_HOME environment variable is set, it will use that path
// otherwise it will use the user's config directory
// e.g. /home/user/.config/nvpm
func GetAppDataPath() string {
	if nvpmHome := fileSystem.Getenv("NVPM_HOME"); nvpmHome != "" {
		return EnsureDirExists(nvpmHome)
	}
	userConfigDir, err := fileSystem.UserConfigDir()
	if err != nil {
		panic(err)
	}
	return EnsureDirExists(userConfigDir + string(os.PathSeparator) + "nvpm")
}

// GetTempPath returns the path to the temp directory
// e.g. /tmp
func GetTempPath() string {
	return fileSystem.TempDir()
}

// GetAppRegistryFilePath returns the path to the registry file
// e.g. /home/user/.cache/nvpm/nvpm-registry.json
func GetAppRegistryFilePath() string {
	return GetCachePath() + string(os.PathSeparator) + "nvpm-registry.json"
}

// GetAppPackagesPath returns the path to the packages directory
// Otherwise:
//   - Linux: ~/.local/share/nvpm/packages
//   - macOS: ~/Library/Application Support/nvpm/packages
//   - Windows: %APPDATA%\nvpm\packages
func GetAppPackagesPath() string {
	return EnsureDirExists(GetAppDataSharePath() + string(os.PathSeparator) + "packages")
}

// GetAppDataSharePath returns the path to the app data share directory
// This is separate from the config directory and follows XDG Base Directory spec
// Otherwise:
//   - Linux: ~/.local/share/nvpm
//   - macOS: ~/Library/Application Support/nvpm (same as config)
//   - Windows: %APPDATA%\nvpm (same as config)
func GetAppDataSharePath() string {
	// On Linux, use ~/.local/share, otherwise use config dir (macOS/Windows)
	userConfigDir, err := fileSystem.UserConfigDir()
	if err != nil {
		panic(err)
	}

	// Check if we're on Linux by checking if config dir ends with .config
	// On Linux: ~/.config, on macOS: ~/Library/Application Support, on Windows: %APPDATA%
	if strings.Contains(userConfigDir, ".config") {
		// Linux: use ~/.local/share instead of ~/.config
		userHomeDir, err := fileSystem.UserHomeDir()
		if err != nil {
			panic(err)
		}
		return EnsureDirExists(userHomeDir + string(os.PathSeparator) + ".local" + string(os.PathSeparator) + "share" + string(os.PathSeparator) + "nvpm")
	}

	// macOS and Windows: use config directory (same location)
	return EnsureDirExists(userConfigDir + string(os.PathSeparator) + "nvpm")
}

// GetAppBinPath returns the path to the bin directory
// Otherwise:
//   - Linux: ~/.local/share/nvpm/bin
//   - macOS: ~/Library/Application Support/nvpm/bin
//   - Windows: %APPDATA%\nvpm\bin
//
// e.g. /home/user/.local/share/nvpm/bin
func GetAppBinPath() string {
	return EnsureDirExists(GetAppDataSharePath() + string(os.PathSeparator) + "bin")
}

func EnsureDirExists(path string) string {
	if _, err := fileSystem.Stat(path); os.IsNotExist(err) {
		if err := fileSystem.MkdirAll(path, 0755); err != nil {
			// Log the error but don't fail the function
			fmt.Printf("Warning: failed to create directory %s: %v\n", path, err)
		}
	}
	return path
}

func Unzip(src, dest string) error {
	r, err := zipFileOpener.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	if err := fileSystem.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			if err := fileSystem.MkdirAll(path, f.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", path, err)
			}
		} else {
			if err := fileSystem.MkdirAll(filepath.Dir(path), f.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(path), err)
			}
			f, err := fileSystem.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := fileSystem.Close(f); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File() {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetCachePath returns the path to the cache directory
// If NVPM_CACHE is set, it will use that path
// Otherwise:
//   - Linux: ~/.cache/nvpm
//   - macOS: ~/Library/Caches/nvpm
//   - Windows: %LOCALAPPDATA%\nvpm\cache
func GetCachePath() string {
	if nvpmCache := fileSystem.Getenv("NVPM_CACHE"); nvpmCache != "" {
		return EnsureDirExists(nvpmCache)
	}

	if cfg, ok := readNvpmConfigFile(); ok {
		if raw := strings.TrimSpace(cfg.Paths.CacheDir); raw != "" {
			return EnsureDirExists(expandUserAndRelativePath(raw))
		}
	}

	userHomeDir, err := fileSystem.UserHomeDir()
	if err != nil {
		panic(err)
	}

	var cacheDir string
	switch runtime.GOOS {
	case "linux":
		// Linux: ~/.cache/nvpm (XDG Base Directory spec)
		cacheDir = userHomeDir + string(os.PathSeparator) + ".cache" + string(os.PathSeparator) + "nvpm"
	case "darwin":
		// macOS: ~/Library/Caches/nvpm
		cacheDir = userHomeDir + string(os.PathSeparator) + "Library" + string(os.PathSeparator) + "Caches" + string(os.PathSeparator) + "nvpm"
	case "windows":
		// Windows: %LOCALAPPDATA%\nvpm\cache
		// Try LOCALAPPDATA first, fallback to APPDATA if not set
		localAppData := fileSystem.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			appData := fileSystem.Getenv("APPDATA")
			if appData != "" {
				cacheDir = appData + string(os.PathSeparator) + "nvpm" + string(os.PathSeparator) + "cache"
			} else {
				// Fallback to user home
				cacheDir = userHomeDir + string(os.PathSeparator) + ".nvpm" + string(os.PathSeparator) + "cache"
			}
		} else {
			cacheDir = localAppData + string(os.PathSeparator) + "nvpm" + string(os.PathSeparator) + "cache"
		}
	default:
		// Default: use user home with .cache/nvpm
		cacheDir = userHomeDir + string(os.PathSeparator) + ".cache" + string(os.PathSeparator) + "nvpm"
	}

	return EnsureDirExists(cacheDir)
}

// GetRegistryCachePath returns the path to the registry cache file
// e.g. /home/user/.cache/nvpm/registry-cache.json.zip
func GetRegistryCachePath() string {
	return GetCachePath() + string(os.PathSeparator) + "registry-cache.json.zip"
}

// IsCacheValid checks if the cache file exists and is newer than the specified duration
func IsCacheValid(cachePath string, maxAge time.Duration) bool {
	fileInfo, err := fileSystem.Stat(cachePath)
	if err != nil {
		return false // Cache file doesn't exist
	}

	// Check if the file is older than maxAge
	return time.Since(fileInfo.ModTime()) < maxAge
}

// DownloadWithCache downloads a file with caching support
func DownloadWithCache(url string, cachePath string, maxAge time.Duration) error {
	// Check if cache is valid
	if IsCacheValid(cachePath, maxAge) {
		return nil // Cache is valid, no need to download
	}

	// Download the file
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			// Log the error but don't fail the function
			fmt.Printf("Warning: failed to close response body: %v\n", closeErr)
		}
	}()

	// Create the cache file
	out, err := fileSystem.Create(cachePath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := fileSystem.Close(out); closeErr != nil {
			// Log the error but don't fail the function
			fmt.Printf("Warning: failed to close cache file: %v\n", closeErr)
		}
	}()

	// Copy the response to the cache file
	_, err = io.Copy(out, resp.Body)
	return err
}

type nvpmConfigFile struct {
	Registry struct {
		URLs        []string `yaml:"urls"`
		CacheMaxAge string   `yaml:"cacheMaxAge"`
	} `yaml:"registry"`

	Paths struct {
		CacheDir string `yaml:"cacheDir"`
	} `yaml:"paths"`
}

func expandUserAndRelativePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}

	// Expand "~" to the user's home directory.
	if p == "~" || strings.HasPrefix(p, "~"+string(os.PathSeparator)) {
		home, err := fileSystem.UserHomeDir()
		if err == nil && home != "" {
			if p == "~" {
				return home
			}
			return filepath.Join(home, strings.TrimPrefix(p, "~"+string(os.PathSeparator)))
		}
	}

	// If relative, make it relative to the user's home directory (safer than cwd).
	if !filepath.IsAbs(p) {
		home, err := fileSystem.UserHomeDir()
		if err == nil && home != "" {
			return filepath.Join(home, p)
		}
	}

	return p
}

func getConfigFilePath() string {
	return GetAppDataPath() + string(os.PathSeparator) + "config.yaml"
}

func readNvpmConfigFile() (nvpmConfigFile, bool) {
	path := getConfigFilePath()
	f, err := fileSystem.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nvpmConfigFile{}, false
	}
	defer func() { _ = fileSystem.Close(f) }()

	b, err := io.ReadAll(f)
	if err != nil {
		return nvpmConfigFile{}, true
	}

	var cfg nvpmConfigFile
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nvpmConfigFile{}, true
	}
	return cfg, true
}

func getRegistryCacheMaxAge() time.Duration {
	// Default is intentionally short to reduce the chance of users seeing stale registry data
	// without having to manually `nvpm sync registry`.
	maxAge := 6 * time.Hour

	if cfg, ok := readNvpmConfigFile(); ok {
		if raw := strings.TrimSpace(cfg.Registry.CacheMaxAge); raw != "" {
			if parsed, err := time.ParseDuration(raw); err == nil {
				if parsed < 0 {
					return 0
				}
				return parsed
			}
		}
	}

	return maxAge
}

func defaultRegistryURL() string {
	return "https://github.com/mistweaverco/nvpm-registry/releases/latest/download/nvpm-registry.json.zip"
}

func splitRegistryURLs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	// Accept comma-separated (common in env vars), but also tolerate whitespace/newlines.
	raw = strings.ReplaceAll(raw, "\n", ",")
	raw = strings.ReplaceAll(raw, "\t", ",")
	raw = strings.ReplaceAll(raw, " ", ",")

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		u := strings.TrimSpace(p)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

// ResolveRegistryURLs returns registry URLs in priority order:
// 1) NVPM_REGISTRY_URLS (comma/space-separated list)
// 2) config.yaml registry.urls (array)
// 3) built-in default
func ResolveRegistryURLs() []string {
	if override := splitRegistryURLs(fileSystem.Getenv("NVPM_REGISTRY_URLS")); len(override) > 0 {
		return override
	}
	if cfg, ok := readNvpmConfigFile(); ok {
		if len(cfg.Registry.URLs) > 0 {
			urls := make([]string, 0, len(cfg.Registry.URLs))
			for _, u := range cfg.Registry.URLs {
				if s := strings.TrimSpace(u); s != "" {
					urls = append(urls, s)
				}
			}
			if len(urls) > 0 {
				return urls
			}
		}
	}
	return []string{defaultRegistryURL()}
}

func downloadWithCacheFromURLs(urls []string, cachePath string, maxAge time.Duration) error {
	// Check if cache is valid once
	if IsCacheValid(cachePath, maxAge) {
		return nil
	}

	if len(urls) == 0 {
		urls = []string{defaultRegistryURL()}
	}

	var lastErr error
	for _, url := range urls {
		resp, err := httpClient.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		func() {
			defer func() { _ = resp.Body.Close() }()
			out, err := fileSystem.Create(cachePath)
			if err != nil {
				lastErr = err
				return
			}
			defer func() { _ = fileSystem.Close(out) }()

			if _, err := io.Copy(out, resp.Body); err != nil {
				lastErr = err
				return
			}

			lastErr = nil
		}()

		if lastErr == nil {
			return nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no registry urls configured")
	}
	return lastErr
}

func registryCachePathForURL(url string, index int) string {
	// Keep the historical cache filename for the first registry to avoid breaking
	// external assumptions/tests. Additional registries get deterministic hashed names.
	if index == 0 {
		return GetRegistryCachePath()
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(url))
	return filepath.Join(GetCachePath(), fmt.Sprintf("registry-cache-%08x.json.zip", h.Sum32()))
}

// DownloadRegistryZipWithCache downloads all configured registry zips into cache,
// one zip per registry URL.
//
// This is useful for UI flows that want to separate "download" and "unzip" steps.
func DownloadRegistryZipWithCache(maxAge time.Duration) error {
	urls := ResolveRegistryURLs()
	if len(urls) == 0 {
		urls = []string{defaultRegistryURL()}
	}
	for i, u := range urls {
		cachePath := registryCachePathForURL(u, i)
		if err := DownloadWithCache(u, cachePath, maxAge); err != nil {
			return err
		}
	}
	return nil
}

type registryItemKey struct {
	Name   string `json:"name"`
	Source struct {
		ID string `json:"id"`
	} `json:"source"`
}

func normalizeRegistrySourceID(id string) string {
	// New format: "<provider>:<id>"
	if strings.Contains(id, ":") && !strings.HasPrefix(id, "pkg:") {
		return id
	}
	// Legacy format: "pkg:<provider>/<id>"
	if strings.HasPrefix(id, "pkg:") {
		withoutPrefix := strings.TrimPrefix(id, "pkg:")
		parts := strings.SplitN(withoutPrefix, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return parts[0] + ":" + parts[1]
		}
	}
	return id
}

func mergeRegistryJSONArrays(registryJSONs [][]byte) ([]byte, error) {
	type entry struct {
		key string
		raw json.RawMessage
	}

	merged := map[string]json.RawMessage{}
	order := make([]string, 0, 4096)
	seen := map[string]struct{}{}

	for _, b := range registryJSONs {
		var items []json.RawMessage
		if err := json.Unmarshal(b, &items); err != nil {
			return nil, fmt.Errorf("failed to parse registry json array: %w", err)
		}

		for _, raw := range items {
			var k registryItemKey
			_ = json.Unmarshal(raw, &k) // best-effort; key fallback below

			key := normalizeRegistrySourceID(strings.TrimSpace(k.Source.ID))
			if key == "" {
				key = strings.TrimSpace(k.Name)
			}
			if key == "" {
				// Skip items that can't be identified deterministically.
				continue
			}

			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				order = append(order, key)
			}
			// Later registries override earlier ones.
			merged[key] = raw
		}
	}

	out := make([]json.RawMessage, 0, len(order))
	for _, k := range order {
		if raw, ok := merged[k]; ok {
			out = append(out, raw)
		}
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("failed to encode merged registry: %w", err)
	}
	return encoded, nil
}

// DownloadAndUnzipRegistry downloads the registry from the default URL and unzips it
// This is used to ensure the registry is available for commands that need it
func DownloadAndUnzipRegistry() error {
	return downloadAndUnzipRegistry(getRegistryCacheMaxAge(), true)
}

// DownloadAndUnzipRegistryQuiet is like DownloadAndUnzipRegistry but does not show its own
// download spinner. Use this when the caller already wraps the operation in a spinner.
func DownloadAndUnzipRegistryQuiet() error {
	return downloadAndUnzipRegistry(getRegistryCacheMaxAge(), false)
}

func downloadAndUnzipRegistry(cacheMaxAge time.Duration, showSpinner bool) error {
	registryURLs := ResolveRegistryURLs()
	registryJSONPath := GetAppRegistryFilePath()

	if len(registryURLs) == 0 {
		registryURLs = []string{defaultRegistryURL()}
	}

	// Determine which registry zips need downloading.
	cachePaths := make([]string, 0, len(registryURLs))
	cacheInfos := make([]os.FileInfo, 0, len(registryURLs))
	needsDownload := false
	for i, u := range registryURLs {
		p := registryCachePathForURL(u, i)
		cachePaths = append(cachePaths, p)
		if !IsCacheValid(p, cacheMaxAge) {
			needsDownload = true
		}
		if info, err := fileSystem.Stat(p); err == nil {
			cacheInfos = append(cacheInfos, info)
		}
	}

	// If zips are all fresh, and merged JSON exists and is newer than all zip files, we can skip.
	if !needsDownload {
		if jsonInfo, err := fileSystem.Stat(registryJSONPath); err == nil {
			isNewerThanAll := true
			for _, ci := range cacheInfos {
				if ci != nil && jsonInfo.ModTime().Before(ci.ModTime()) {
					isNewerThanAll = false
					break
				}
			}
			if isNewerThanAll {
				return nil
			}
		}
	}

	// Download all zips (only those that need it).
	var downloadErr error
	action := func() {
		for i, u := range registryURLs {
			p := cachePaths[i]
			if err := DownloadWithCache(u, p, cacheMaxAge); err != nil {
				downloadErr = err
				return
			}
		}
	}

	if needsDownload {
		if showSpinner {
			if err := spinnerutil.Run("Downloading registry...", action); err != nil {
				return err
			}
		} else {
			action()
		}

		if downloadErr != nil {
			return fmt.Errorf("failed to download registry: %w", downloadErr)
		}
	}

	// Unzip each registry and merge them into a single nvpm-registry.json for consumers.
	registryJSONName := filepath.Base(GetAppRegistryFilePath())
	registryJSONs := make([][]byte, 0, len(cachePaths))
	for i, cachePath := range cachePaths {
		unzipDir := filepath.Join(GetCachePath(), fmt.Sprintf("registry-unzipped-%d", i))
		if err := Unzip(cachePath, unzipDir); err != nil {
			return fmt.Errorf("failed to unzip registry: %w", err)
		}

		f, err := fileSystem.OpenFile(filepath.Join(unzipDir, registryJSONName), os.O_RDONLY, 0)
		if err != nil {
			return fmt.Errorf("failed to read registry json: %w", err)
		}
		b, err := io.ReadAll(f)
		_ = fileSystem.Close(f)
		if err != nil {
			return fmt.Errorf("failed to read registry json: %w", err)
		}
		registryJSONs = append(registryJSONs, b)
	}

	merged, err := mergeRegistryJSONArrays(registryJSONs)
	if err != nil {
		return err
	}

	out, err := fileSystem.Create(registryJSONPath)
	if err != nil {
		return fmt.Errorf("failed to write merged registry json: %w", err)
	}
	if _, err := out.Write(merged); err != nil {
		_ = fileSystem.Close(out)
		return fmt.Errorf("failed to write merged registry json: %w", err)
	}
	if err := fileSystem.Close(out); err != nil {
		return fmt.Errorf("failed to write merged registry json: %w", err)
	}

	return nil
}

// DownloadAndUnzipRegistryForced is like DownloadAndUnzipRegistry, but always forces a fresh download.
// It still respects registry URL resolution (NVPM_REGISTRY_URLS > config.yaml > default).
func DownloadAndUnzipRegistryForced() error {
	registryURLs := ResolveRegistryURLs()
	if len(registryURLs) == 0 {
		registryURLs = []string{defaultRegistryURL()}
	}

	// Force download by using 0 duration (cache is never valid) with spinner
	var downloadErr error
	action := func() {
		for i, u := range registryURLs {
			p := registryCachePathForURL(u, i)
			if err := DownloadWithCache(u, p, 0); err != nil {
				downloadErr = err
				return
			}
		}
	}

	if err := spinnerutil.Run("Downloading registry...", action); err != nil {
		return err
	}

	if downloadErr != nil {
		return fmt.Errorf("failed to download registry: %w", downloadErr)
	}

	// Reuse the non-forced merge/unzip path now that zips are fresh.
	// (It will merge and write the final JSON.)
	return DownloadAndUnzipRegistryQuiet()
}

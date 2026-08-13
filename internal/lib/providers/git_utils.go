package providers

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
)

// IsGitCommitHash checks if a string is a valid Git commit hash (40 hexadecimal characters)
func IsGitCommitHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// resolveReleaseUpdateVersion picks the GitHub/GitLab/Codeberg *release* tag to install
// on `nvpm up`. Prefers /releases/latest (stable; skips pre-releases). Git branch
// aliases and channel names like nightly must not be used as the automatic latest
// when the Releases API is unavailable - those are explicit install pins, not updates.
func resolveReleaseUpdateVersion(sourceID, registryVersion string, latestReleaseTag func() (string, error)) (string, error) {
	if latestReleaseTag != nil {
		if tag, err := latestReleaseTag(); err == nil {
			if v := strings.TrimSpace(tag); v != "" && !strings.EqualFold(v, "latest") {
				return v, nil
			}
		}
	}
	if v := strings.TrimSpace(registryVersion); v != "" && !strings.EqualFold(v, "latest") && !isNonReleaseGitRef(v) {
		return v, nil
	}
	latest, err := ResolveGitLatestRef(sourceID)
	if err == nil {
		if v := strings.TrimSpace(latest); v != "" && !strings.EqualFold(v, "latest") && !isNonReleaseGitRef(v) {
			return v, nil
		}
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("could not determine latest release for %s", sourceID)
}

func isNonReleaseGitRef(version string) bool {
	switch strings.ToLower(strings.TrimSpace(version)) {
	case "main", "master", "trunk", "nightly", "latest", "head":
		return true
	default:
		return false
	}
}

// LatestReleaseTagForSource returns the host's latest GitHub/GitLab/Codeberg release tag.
func LatestReleaseTagForSource(sourceID string) (string, error) {
	id := normalizePackageID(strings.TrimSpace(sourceID))
	switch detectProvider(id) {
	case ProviderGitHub:
		return NewProviderGitHub().getLatestReleaseTag(strings.TrimPrefix(id, "github:"))
	case ProviderGitLab:
		return NewProviderGitLab().getLatestReleaseTag(strings.TrimPrefix(id, "gitlab:"))
	case ProviderCodeberg:
		return NewProviderCodeberg().getLatestReleaseTag(strings.TrimPrefix(id, "codeberg:"))
	default:
		return "", fmt.Errorf("no release API for %s", sourceID)
	}
}

// gitWorkTreeExists reports whether path looks like a git working tree (has .git).
// Release-asset installs are plain directories; sync must not treat them as clones.
func gitWorkTreeExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// DetectRegistryTarget detects the current platform and returns the registry target string
// Registry targets: darwin_arm64, darwin_x64, linux_x64, linux_arm64, linux_arm, win_x64, etc.
func DetectRegistryTarget() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	var osPart string
	var archPart string

	switch goos {
	case "darwin":
		osPart = "darwin"
	case "linux":
		osPart = "linux"
	case "windows":
		osPart = "win"
	default:
		osPart = strings.ToLower(goos)
	}

	switch goarch {
	case "amd64":
		archPart = "x64"
	case "386":
		archPart = "x86"
	case "arm64":
		archPart = "arm64"
	case "arm":
		archPart = "arm"
	default:
		archPart = strings.ToLower(goarch)
	}

	// Handle special cases
	if goos == "linux" && goarch == "arm" {
		archPart = "arm" // Could be armv6hf, armv7, etc. - registry may specify more precisely
	}

	return fmt.Sprintf("%s_%s", osPart, archPart)
}

// IsUntargetedAsset reports whether an asset/download entry applies to all platforms.
func IsUntargetedAsset(target interface{}) bool {
	if target == nil {
		return true
	}
	if str, ok := target.(string); ok {
		return strings.TrimSpace(str) == ""
	}
	return false
}

// MatchesTarget checks if a registry target matches the current platform
// target can be a string like "linux_x64" or an array like ["darwin_x64", "darwin_arm64"]
func MatchesTarget(target interface{}, currentTarget string) bool {
	switch v := target.(type) {
	case string:
		return v == currentTarget
	case []interface{}:
		for _, t := range v {
			if str, ok := t.(string); ok && str == currentTarget {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// FindMatchingAsset finds the asset entry that matches the current platform.
// Assets without a target apply to all platforms and are used as a last resort.
func FindMatchingAsset(assets registry_parser.RegistryItemSourceAssetList) *registry_parser.RegistryItemSourceAsset {
	currentTarget := DetectRegistryTarget()

	var untargeted *registry_parser.RegistryItemSourceAsset
	for i := range assets {
		if IsUntargetedAsset(assets[i].Target) {
			if untargeted == nil {
				untargeted = &assets[i]
			}
			continue
		}
		if MatchesTarget(assets[i].Target, currentTarget) {
			return &assets[i]
		}
	}

	// Try fallback: check for linux_x64_gnu if linux_x64 not found
	if strings.HasPrefix(currentTarget, "linux_") {
		fallbackTarget := currentTarget + "_gnu"
		for i := range assets {
			if MatchesTarget(assets[i].Target, fallbackTarget) {
				return &assets[i]
			}
		}
	}

	return untargeted
}

// ResolveTemplate resolves template variables in strings
// Currently supports: {{version}}
func ResolveTemplate(template string, version string) string {
	result := template
	result = strings.ReplaceAll(result, "{{version}}", version)
	result = strings.ReplaceAll(result, "{{ version }}", version)

	// Handle strip_prefix filter: {{ version | strip_prefix "v" }}
	// Simple implementation: if version starts with "v", remove it
	if strings.HasPrefix(version, "v") {
		result = strings.ReplaceAll(result, "{{ version | strip_prefix \"v\" }}", strings.TrimPrefix(version, "v"))
		result = strings.ReplaceAll(result, "{{version | strip_prefix \"v\"}}", strings.TrimPrefix(version, "v"))
	}

	return result
}

// extractBinFromAsset extracts binary name(s) from asset bin field
// bin can be a string (single binary) or a map[string]string (multiple binaries)
func extractBinFromAsset(bin interface{}, binName string) string {
	switch v := bin.(type) {
	case string:
		return v
	case map[string]interface{}:
		if val, ok := v[binName].(string); ok {
			return val
		}
		return ""
	default:
		return ""
	}
}

// ResolveBinPath resolves the binary path from registry bin template
// Examples: "{{source.asset.bin}}" -> "shellcheck"
//
//	"{{source.bin}}" -> "node:js-debug/src/dapDebugServer.js"
//	"{{source.asset.file}}" -> "latexindent-macos"
//	"{{source.asset.bin.protolint}}" -> "protolint"
func ResolveBinPath(binTemplate string, asset *registry_parser.RegistryItemSourceAsset, sourceBin, binName string) string {
	result := binTemplate

	if strings.Contains(result, "{{source.bin}}") {
		result = strings.ReplaceAll(result, "{{source.bin}}", sourceBin)
	}

	// Handle {{source.asset.bin}}
	if strings.Contains(result, "{{source.asset.bin}}") {
		binValue := extractBinFromAsset(asset.Bin, binName)
		if binValue == "" {
			// If bin is a string, use it directly
			if str, ok := asset.Bin.(string); ok {
				binValue = str
			}
		}
		result = strings.ReplaceAll(result, "{{source.asset.bin}}", binValue)
	}

	// Handle {{source.asset.bin.<name>}}
	if strings.Contains(result, "{{source.asset.bin.") {
		// Extract bin name from template
		start := strings.Index(result, "{{source.asset.bin.")
		end := strings.Index(result[start:], "}}")
		if end > 0 {
			binKey := result[start+len("{{source.asset.bin.") : start+end]
			binValue := extractBinFromAsset(asset.Bin, binKey)
			result = strings.ReplaceAll(result, "{{source.asset.bin."+binKey+"}}", binValue)
		}
	}

	// Handle {{source.asset.file}}
	if strings.Contains(result, "{{source.asset.file}}") {
		result = strings.ReplaceAll(result, "{{source.asset.file}}", asset.File.String())
	}

	return result
}

// SplitAssetFileSpec splits a registry asset file spec into the archive file name
// and an optional install subdirectory.
//
// Example: "lua-language-server-{{version}}-linux-x64.tar.gz:libexec/" ->
// ("lua-language-server-{{version}}-linux-x64.tar.gz", "libexec")
func SplitAssetFileSpec(spec string) (fileName, installSubdir string) {
	if idx := strings.Index(spec, ":"); idx > 0 {
		return spec[:idx], strings.Trim(strings.TrimSpace(spec[idx+1:]), "/")
	}
	return spec, ""
}

// AssetArchiveFileName resolves the downloadable archive name from a registry asset file spec.
func AssetArchiveFileName(file registry_parser.RegistryItemSourceAssetFile, version string) string {
	fileName, _ := SplitAssetFileSpec(file.String())
	return ResolveTemplate(fileName, version)
}

// ParseBinSpec parses a registry bin path. Paths prefixed with "exec:" are executed
// via a wrapper script; paths prefixed with "node:" run via `node`; other paths are symlinked.
func ParseBinSpec(binPath string) (wrapper string, relPath string) {
	if strings.HasPrefix(binPath, "exec:") {
		return "exec", strings.TrimPrefix(binPath, "exec:")
	}
	if strings.HasPrefix(binPath, "node:") {
		return "node", strings.TrimPrefix(binPath, "node:")
	}
	return "", binPath
}

// InstallReleaseAssetContents copies extracted release contents into the package directory.
// When the asset file spec includes ":subdir/", contents are installed under that subdir.
func InstallReleaseAssetContents(extractDir, repoPath string, asset *registry_parser.RegistryItemSourceAsset) error {
	_, installSubdir := SplitAssetFileSpec(asset.File.String())
	destDir := repoPath
	if installSubdir != "" {
		destDir = filepath.Join(repoPath, filepath.Clean(installSubdir))
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create install directory: %w", err)
	}
	return copyDirContents(extractDir, destDir)
}

// LinkReleaseBins exposes installed release binaries in the nvpm bin directory.
func LinkReleaseBins(repoPath string, asset *registry_parser.RegistryItemSourceAsset, registryItem registry_parser.RegistryItem) error {
	nvpmBinDir := files.GetAppBinPath()

	for binName, binTemplate := range registryItem.Bin {
		binPath := ResolveBinPath(binTemplate, asset, registryItem.Source.Bin, binName)
		if binPath == "" {
			continue
		}

		wrapper, relPath := ParseBinSpec(binPath)
		targetPath := filepath.Join(repoPath, filepath.Clean(relPath))
		if _, err := os.Stat(targetPath); err != nil {
			if found := findBinaryInTree(repoPath, filepath.Base(relPath)); found != "" {
				targetPath = found
			} else {
				Logger.Info(fmt.Sprintf("Release install: binary not found for %s at %s", binName, targetPath))
				continue
			}
		}

		linkPath := filepath.Join(nvpmBinDir, binName)
		if _, err := os.Lstat(linkPath); err == nil {
			_ = os.Remove(linkPath)
		}

		switch wrapper {
		case "exec":
			script := fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", targetPath)
			if err := os.WriteFile(linkPath, []byte(script), 0755); err != nil {
				Logger.Info(fmt.Sprintf("Release install: warning creating wrapper %s: %v", linkPath, err))
				continue
			}
			Logger.Info(fmt.Sprintf("Release install: Created wrapper %s -> %s", linkPath, targetPath))
		case "node":
			script := fmt.Sprintf("#!/bin/sh\nexec node %q \"$@\"\n", targetPath)
			if err := os.WriteFile(linkPath, []byte(script), 0755); err != nil {
				Logger.Info(fmt.Sprintf("Release install: warning creating wrapper %s: %v", linkPath, err))
				continue
			}
			Logger.Info(fmt.Sprintf("Release install: Created node wrapper %s -> %s", linkPath, targetPath))
		default:
			relLink, err := filepath.Rel(nvpmBinDir, targetPath)
			if err != nil {
				relLink = targetPath
			}
			if err := os.Symlink(relLink, linkPath); err != nil {
				Logger.Info(fmt.Sprintf("Release install: warning creating symlink %s -> %s: %v", linkPath, relLink, err))
				continue
			}
			Logger.Info(fmt.Sprintf("Release install: Created symlink %s -> %s", linkPath, relLink))
		}
	}

	return nil
}

func copyDirContents(src, dest string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dest, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
			if err := copyDirContents(srcPath, destPath); err != nil {
				return err
			}
			continue
		}
		if err := copyReleaseFile(srcPath, destPath); err != nil {
			return err
		}
	}
	return nil
}

func copyReleaseFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	if info, err := os.Stat(src); err == nil {
		_ = os.Chmod(dest, info.Mode().Perm())
	}
	return nil
}

func findBinaryInTree(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if found := findBinaryInTree(path, name); found != "" {
				return found
			}
			continue
		}
		if entry.Name() == name {
			return path
		}
	}
	return ""
}
